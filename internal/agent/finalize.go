package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/common"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/videoevidence"
)

func finalAnswerImageRequirement(hasRetrievedImage bool) string {
	if !hasRetrievedImage {
		return ""
	}
	return `
5. Retrieved tool results contain Markdown images. Unless the user explicitly requested text-only output or every image is clearly unrelated, the final answer MUST include at least one relevant Markdown image copied verbatim from the tool results. Preserve its complete URL exactly. Use ASCII half-width parentheses exactly as ![alt](url) and never use full-width （ or ）. Place the image immediately after the paragraph it supports. When multiple images support different sections, distribute them across those sections instead of stopping after the first image.
6. Before finishing, silently verify that the answer contains a Markdown image when requirement 5 applies.`
}

func finalAnswerComparisonRequirement(query string) string {
	normalized := strings.TrimSpace(query)
	if normalized == "" ||
		(!strings.Contains(normalized, "比较") &&
			!strings.Contains(normalized, "对比") &&
			!strings.Contains(normalized, "分别") &&
			!strings.Contains(normalized, "二者")) {
		return ""
	}
	return `
12. This is a comparison question. Use at least one "comparison" block for each explicitly requested video or concept, then include a summary/evidence block that states the key difference and the scope boundary.
13. Do not claim that one video directly defines a concept unless the retrieved evidence supports that exact claim. If a requested video or claim is not evidenced, use coverage "partial" and say what remains unverified.
14. Keep every factual comparison claim tied to one or more citation handles from the current tool results; never use a handle from another turn or invent a video identity.`
}

// streamFinalAnswerToEventBus streams the final answer generation through EventBus
func (e *AgentEngine) streamFinalAnswerToEventBus(
	ctx context.Context,
	query string,
	state *types.AgentState,
	sessionID string,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	totalToolCalls := countTotalToolCalls(state.RoundSteps)
	logger.Infof(ctx, "[Agent][FinalAnswer] Synthesizing from %d steps, %d tool calls",
		len(state.RoundSteps), totalToolCalls)
	common.PipelineInfo(ctx, "Agent", "final_answer_start", map[string]interface{}{
		"session_id":   sessionID,
		"query":        query,
		"steps":        len(state.RoundSteps),
		"tool_results": totalToolCalls,
	})

	// Build messages with all context
	systemPrompt := e.buildSystemPrompt(ctx)
	userTurn := e.RenderUserTurnContent(sessionID, query)

	messages := []chat.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userTurn},
	}

	// Add all tool call results as context
	toolResultCount := 0
	hasRetrievedImage := false
	for stepIdx, step := range state.RoundSteps {
		for toolIdx, toolCall := range step.ToolCalls {
			toolResultCount++
			if searchutil.MarkdownImageRegex.MatchString(toolCall.Result.Output) {
				hasRetrievedImage = true
			}
			modelOutput := e.modelContext.ModelToolResultForTool(toolCall.Name, toolCall.Result)
			messages = append(messages, chat.Message{
				Role:    "user",
				Content: fmt.Sprintf("Tool %s returned: %s", toolCall.Name, modelOutput),
			})
			logger.Debugf(ctx, "[Agent][FinalAnswer] Added tool result [Step-%d][Tool-%d]: %s (output: %d chars)",
				stepIdx+1, toolIdx+1, toolCall.Name, len(toolCall.Result.Output))
		}
	}

	logger.Debugf(ctx, "[Agent][FinalAnswer] Built context: %d messages, %d tool results",
		len(messages), toolResultCount)

	imageRequirement := finalAnswerImageRequirement(hasRetrievedImage)
	comparisonRequirement := finalAnswerComparisonRequirement(query)
	answerContractRequirement := ""
	bufferFinalAnswer := false
	if e.config != nil && videoevidence.Supports(e.config.VideoEvidenceCitation) {
		if e.config.AnswerContractEnabled {
			bufferFinalAnswer = true
			answerContractRequirement = fmt.Sprintf(`
8. Return exactly one JSON object that conforms to %s. Do not use Markdown fences, explanations, or trailing text.
9. The object must contain: schema_version (exactly %q), mode (exactly %q for this agent), coverage (%q, %q, or %q), content_markdown (string), and blocks (array).
10. Each block must contain only type, title, text_markdown, and evidence_refs. Block type must be exactly one of "summary", "topic", "comparison", "steps", or "evidence"; never use "video_evidence" or "answer". Use evidence_refs only for citation handles that appear in the block text as <ref id="cN"/>. Ordinary knowledge/Wiki cN handles may be cited for explanation, but they remain normal sources and never become video evidence; video locations require a validated transcript handle. Do not invent video IDs, titles, timestamps, evidence IDs, or links.
11. Every video location must be written as a complete sentence around its citation. Do not output a Markdown table of video locations yourself and never hand-write times, video IDs, or evidence IDs; the system appends the validated location table and renders only the validated time range.`, videoevidence.AnswerContractVersion,
				videoevidence.AnswerContractVersion, videoevidence.AnswerModeReasoning,
				videoevidence.AnswerCoverageComplete, videoevidence.AnswerCoveragePartial, videoevidence.AnswerCoverageNone)
		}
	}
	if e.config != nil && e.config.SkillUnavailable {
		answerContractRequirement += `
11. The video evidence Skill is unavailable. Do not claim that video evidence was fully retrieved; use partial or none coverage and state that the time is not verified.`
	}

	// Add final answer prompt.
	finalPrompt := fmt.Sprintf(`Based on the above tool call results, generate a complete answer for the user's question.

User question: %s

Requirements:
1. Answer based on the actually retrieved content
2. Organize the answer in a structured format
3. If information is insufficient, honestly state so
4. Respond in the same language as the user's question
%s
%s
%s

Now generate the final answer:`, query, imageRequirement, answerContractRequirement, comparisonRequirement)

	messages = append(messages, chat.Message{
		Role:    "user",
		Content: finalPrompt,
	})

	// Generate a single ID for this entire final answer stream
	answerID := generateEventID("answer")
	logger.Debugf(ctx, "[Agent][FinalAnswer] AnswerID: %s", answerID)
	answerDoneEmitted := false

	llmResult, err := e.streamLLMToEventBus(
		ctx,
		messages,
		&chat.ChatOptions{
			Temperature:         e.config.Temperature,
			MaxCompletionTokens: e.config.MaxCompletionTokens,
		}, // Thinking disabled for final answer synthesis
		func(chunk *types.StreamResponse, fullContent string) {
			if bufferFinalAnswer {
				return
			}
			// Defensive filter: only emit answer content, skip thinking chunks
			if chunk.ResponseType == types.ResponseTypeThinking {
				return
			}
			if chunk.Content != "" {
				logger.Debugf(ctx, "[Agent][FinalAnswer] Emitting answer chunk: %d chars", len(chunk.Content))
				e.eventBus.Emit(ctx, event.Event{
					ID:        answerID,
					Type:      event.EventAgentFinalAnswer,
					SessionID: sessionID,
					Data: event.AgentFinalAnswerData{
						Content: chunk.Content,
						Done:    chunk.Done,
					},
				})
				if chunk.Done {
					answerDoneEmitted = true
				}
			}
		},
	)
	if err != nil {
		logger.Errorf(ctx, "[Agent][FinalAnswer] Final answer generation failed: %v", err)
		common.PipelineError(ctx, "Agent", "final_answer_stream_failed", map[string]interface{}{
			"session_id": sessionID,
			"error":      err.Error(),
		})
		return err
	}

	fullAnswer := agenttools.StripThinkBlocks(llmResult.Content)
	if bufferFinalAnswer {
		contract, err := videoevidence.ParseAnswerContract(fullAnswer)
		if err != nil {
			logger.Errorf(ctx, "[Agent][FinalAnswer] Answer contract validation failed: %v", err)
			common.PipelineError(ctx, "Agent", "answer_contract_invalid", map[string]interface{}{
				"session_id": sessionID,
				"error_code": err.Error(),
			})
			return err
		}
		if err := candidateAnswerCoverageError(state, contract.Coverage); err != nil {
			return err
		}
		projection, err := e.projectAnswerContract(contract, state.KnowledgeRefs)
		if err != nil {
			logger.Errorf(ctx, "[Agent][FinalAnswer] Answer evidence projection failed: %v", err)
			common.PipelineError(ctx, "Agent", "answer_contract_invalid", map[string]interface{}{
				"session_id": sessionID,
				"error_code": err.Error(),
			})
			return err
		}
		if err := videoevidence.ValidateComparisonAnswerForKnowledgeIDs(
			query, contract, projection, state.RequiredKnowledgeIDs,
		); err != nil {
			logger.Errorf(ctx, "[Agent][FinalAnswer] Comparison answer validation failed: %v", err)
			common.PipelineError(ctx, "Agent", "answer_contract_invalid", map[string]interface{}{
				"session_id": sessionID,
				"error_code": err.Error(),
			})
			return err
		}
		fullAnswer = e.modelContext.DecodeOutputText(projection.RenderedMarkdown)
		e.eventBus.Emit(ctx, event.Event{
			ID:        answerID,
			Type:      event.EventAgentFinalAnswer,
			SessionID: sessionID,
			Data: event.AgentFinalAnswerData{
				Content: fullAnswer,
				Done:    true,
			},
		})
		answerDoneEmitted = true
	}
	if !answerDoneEmitted {
		e.eventBus.Emit(ctx, event.Event{
			ID:        answerID,
			Type:      event.EventAgentFinalAnswer,
			SessionID: sessionID,
			Data: event.AgentFinalAnswerData{
				Content: "",
				Done:    true,
			},
		})
	}

	// Safety net: strip any residual <think> blocks that may have leaked through
	logger.Infof(ctx, "[Agent][FinalAnswer] Final answer generated: %d characters", len(fullAnswer))
	common.PipelineInfo(ctx, "Agent", "final_answer_done", map[string]interface{}{
		"session_id": sessionID,
		"answer_len": len(fullAnswer),
	})
	state.FinalAnswer = fullAnswer
	return nil
}

// handleMaxIterations generates a final answer when the agent loop exhausted all iterations
// without the LLM producing a natural stop. It marks state.IsComplete = true.
func (e *AgentEngine) handleMaxIterations(
	ctx context.Context, query string, state *types.AgentState, sessionID string,
) {
	logger.Info(ctx, "Reached max iterations, generating final answer")
	common.PipelineWarn(ctx, "Agent", "max_iterations_reached", map[string]interface{}{
		"iterations": state.CurrentRound,
		"max":        e.config.MaxIterations,
	})

	// Stream final answer generation through EventBus
	if err := e.streamFinalAnswerToEventBus(ctx, query, state, sessionID); err != nil {
		logger.Errorf(ctx, "Failed to synthesize final answer: %v", err)
		common.PipelineError(ctx, "Agent", "final_answer_failed", map[string]interface{}{
			"error": err.Error(),
		})
		state.CompletionStatus = "failed"
		state.CompletionFailureReason = "final_answer_generation_failed"
		var contractErr *videoevidence.AnswerContractError
		if errors.As(err, &contractErr) {
			state.CompletionFailureReason = answerContractFailureReason(err)
		}
		if state.CompletionFailureReason == "missing_video_coverage" {
			state.FinalAnswer = answerContractFailureMessage(state.CompletionFailureReason)
		} else {
			state.FinalAnswer = "Sorry, I was unable to generate a complete answer."
		}
	} else {
		state.CompletionStatus = "succeeded"
		state.CompletionFailureReason = ""
	}
	state.IsComplete = true
}

// emitCompletionEvent emits the EventAgentComplete event with execution summary.
func (e *AgentEngine) emitCompletionEvent(
	ctx context.Context, state *types.AgentState, sessionID, messageID string, startTime time.Time,
) {
	outcome := state.CompletionStatus
	if outcome == "" {
		if state.IsComplete {
			outcome = "succeeded"
		} else {
			outcome = "failed"
		}
	}
	// Convert knowledge refs to interface{} slice for event data
	knowledgeRefsInterface := make([]interface{}, 0, len(state.KnowledgeRefs))
	for _, ref := range state.KnowledgeRefs {
		knowledgeRefsInterface = append(knowledgeRefsInterface, ref)
	}

	e.eventBus.Emit(ctx, event.Event{
		ID:        generateEventID("complete"),
		Type:      event.EventAgentComplete,
		SessionID: sessionID,
		Data: event.AgentCompleteData{
			FinalAnswer:     state.FinalAnswer,
			Outcome:         outcome,
			FailureReason:   state.CompletionFailureReason,
			KnowledgeRefs:   knowledgeRefsInterface,
			AgentSteps:      state.RoundSteps, // Include detailed execution steps for message storage
			TotalSteps:      len(state.RoundSteps),
			TotalDurationMs: time.Since(startTime).Milliseconds(),
			MessageID:       messageID, // Include message ID for proper message update
		},
	})

	logger.Infof(ctx, "Agent execution completed in %d rounds", state.CurrentRound)
}
