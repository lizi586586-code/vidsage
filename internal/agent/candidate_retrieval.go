package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/videoevidence"
)

// candidateWhitelist contains only exact titles and durable document IDs
// returned by native retrieval in this request. Multiple chunks of one
// document count once; a title shared by distinct documents is ambiguous.
func candidateWhitelist(refs []*types.SearchResult) []videoevidence.VideoCandidate {
	result := make([]videoevidence.VideoCandidate, 0)
	seen := make(map[string]struct{})
	for _, ref := range refs {
		if ref == nil || ref.KnowledgeID == "" || ref.KnowledgeTitle == "" || ref.KnowledgeBaseID == "" {
			continue
		}
		if _, ok := videoevidence.CandidateFromSearchResult(ref); !ok {
			continue
		}
		if _, ok := seen[ref.KnowledgeID]; ok {
			continue
		}
		seen[ref.KnowledgeID] = struct{}{}
		result = append(result, videoevidence.VideoCandidate{
			CandidateRef: ref.KnowledgeID, Title: ref.KnowledgeTitle,
			KnowledgeBaseID: ref.KnowledgeBaseID,
		})
	}
	return result
}

func requiresCandidateCoverage(query string) bool {
	for _, marker := range []string{"分别", "多个视频", "两个视频", "三个视频", "对比", "比较", "各自"} {
		if strings.Contains(query, marker) {
			return true
		}
	}
	return false
}

// An earlier discovery hit does not substitute for a failed per-document
// retrieval. A complete answer is only possible once every candidate's own
// native search returned valid, linkable evidence.
func candidateAnswerCoverageError(state *types.AgentState, coverage videoevidence.AnswerCoverage) error {
	if len(state.RequiredKnowledgeIDs) == 0 || coverage != videoevidence.AnswerCoverageComplete {
		return nil
	}
	for _, id := range state.RequiredKnowledgeIDs {
		if state.CandidateSearchStatus[id] != string(videoevidence.RetrievalEvidenceFound) {
			return &videoevidence.AnswerContractError{Code: videoevidence.AnswerErrorMissingVideoCoverage}
		}
	}
	return nil
}

// resolveCandidateManifest is an intermediate Agent turn, never a published
// answer. The model chooses titles; the backend only performs exact matching
// against native search results and executes the already-authorized tool.
func (e *AgentEngine) resolveCandidateManifest(
	ctx context.Context, state *types.AgentState, messagesPtr *[]chat.Message,
	step types.AgentStep, raw, sessionID, assistantMessageID, query string,
) (iterOutcome, error) {
	if err := ctx.Err(); err != nil {
		return iterOutcomeBreak, err
	}
	fail := func(code string) (iterOutcome, error) {
		logger.Warnf(ctx, "candidate retrieval stopped code=%s", code)
		state.CompletionStatus = "failed"
		state.CompletionFailureReason = code
		state.FinalAnswer = "候选视频定位或检索未通过校验，请检查视频范围后重试。"
		state.IsComplete = true
		step.Thought = ""
		state.RoundSteps = append(state.RoundSteps, step)
		return iterOutcomeBreak, nil
	}
	if len(state.RequiredKnowledgeIDs) != 0 || e.toolRegistry == nil {
		return fail("candidate_phase_invalid")
	}
	manifest, err := videoevidence.ParseCandidateManifest(raw)
	if err != nil {
		if state.CandidateManifestRetries < 1 {
			state.CandidateManifestRetries++
			logger.Warnf(ctx, "candidate manifest parse failed (%v), retrying", err)
			*messagesPtr = append(*messagesPtr, chat.Message{
				Role: "user",
				Content: fmt.Sprintf("上一轮候选视频清单 JSON 解析失败（%v）。请重新输出一个合法 JSON 对象，包含 version（值为 \"1\"）、task_type（值为 \"multi_video_location\"）、topic（字符串）和 videos（数组，每项含 title 和 knowledge_base_id）。只输出 JSON，不要输出解释、思考过程或代码围栏。", err),
			})
			state.RoundSteps = append(state.RoundSteps, step)
			return iterOutcomeContinue, nil
		}
		return fail("candidate_json_invalid")
	}
	whitelist := candidateWhitelist(state.KnowledgeRefs)
	ids := make([]string, 0, len(manifest.Videos))
	seen := make(map[string]struct{}, len(manifest.Videos))
	for _, candidate := range manifest.Videos {
		matched, status := videoevidence.MatchCandidateTitle(candidate.Title, candidate.KnowledgeBaseID, whitelist)
		if status != videoevidence.CandidateTitleMatched {
			return fail("candidate_title_" + string(status))
		}
		if _, duplicate := seen[matched.CandidateRef]; duplicate {
			return fail("candidate_duplicate_document")
		}
		seen[matched.CandidateRef] = struct{}{}
		ids = append(ids, matched.CandidateRef)
	}
	if _, err := e.toolRegistry.GetTool(agenttools.ToolKnowledgeSearch); err != nil {
		return fail("candidate_search_unavailable")
	}
	state.RequiredKnowledgeIDs = ids
	state.CandidateSearchStatus = make(map[string]string, len(ids))
	// Discovery hits identify documents; they are not final evidence. Only
	// references returned by the per-document searches can support the answer.
	state.KnowledgeRefs = nil
	// This intermediate output must not enter provider history as a final answer.
	// Each native tool call uses one exact, server-resolved knowledge ID.
	for index, id := range ids {
		if err := ctx.Err(); err != nil {
			return iterOutcomeBreak, err
		}
		args, _ := json.Marshal(agenttools.KnowledgeSearchInput{
			Queries: []string{manifest.Topic}, KnowledgeIDs: []string{id},
		})
		call := types.LLMToolCall{
			ID:       fmt.Sprintf("candidate-search-%d-%d", state.CurrentRound, index),
			Type:     "function",
			Function: types.FunctionCall{Name: agenttools.ToolKnowledgeSearch, Arguments: string(args)},
		}
		taskStep := types.AgentStep{Iteration: state.CurrentRound, Timestamp: time.Now()}
		e.executeToolCalls(ctx, &types.ChatResponse{ToolCalls: []types.LLMToolCall{call}},
			&taskStep, state.CurrentRound, sessionID, assistantMessageID)
		state.RoundSteps = append(state.RoundSteps, taskStep)
		*messagesPtr = e.appendToolResults(*messagesPtr, taskStep)
		state.CandidateSearchStatus[id] = string(videoevidence.RetrievalFailed)
		if len(taskStep.ToolCalls) == 0 || taskStep.ToolCalls[0].Result == nil || !taskStep.ToolCalls[0].Result.Success {
			continue
		}
		refs := answerReferencesFromToolCalls(taskStep.ToolCalls)
		found := false
		for _, ref := range refs {
			if ref.KnowledgeID == id {
				candidate, ok := videoevidence.CandidateFromSearchResult(ref)
				if !ok {
					logger.Warnf(ctx, "[Agent][CandidateRetrieval] id=%s ref skipped: CandidateFromSearchResult=false (chunk_type=%q source_type=%q evidence_id=%q video_id=%q gen=%q start=%q end=%q)",
						id, ref.ChunkType, ref.Metadata["source_type"], ref.Metadata["evidence_sentence_id"], ref.Metadata["video_id"], ref.Metadata["transcript_generation"], ref.Metadata["start_ms"], ref.Metadata["end_ms"])
					continue
				}
				_, err := videoevidence.NormalizeCandidate(candidate, videoevidence.ScopeFromReferences([]*types.SearchResult{ref}))
				if err != nil {
					logger.Warnf(ctx, "[Agent][CandidateRetrieval] id=%s NormalizeCandidate failed: %v", id, err)
				}
				found = found || err == nil
			}
		}
		if found {
			state.CandidateSearchStatus[id] = string(videoevidence.RetrievalEvidenceFound)
		} else {
			state.CandidateSearchStatus[id] = string(videoevidence.RetrievalSearchedNoResult)
			logger.Warnf(ctx, "[Agent][CandidateRetrieval] id=%s no linkable evidence (refs=%d, status=searched_no_evidence)", id, len(refs))
		}
		state.KnowledgeRefs = videoevidence.MergeReferences(state.KnowledgeRefs, refs)
	}
	statuses := make([]videoevidence.CandidateRetrieval, 0, len(ids))
	for _, id := range ids {
		statuses = append(statuses, videoevidence.CandidateRetrieval{
			KnowledgeID: id, Status: videoevidence.KnowledgeRetrievalStatus(state.CandidateSearchStatus[id]),
		})
	}
	logger.Infof(ctx, "[Agent][CandidateRetrieval] per-video status: %+v; summary=%s", statuses, videoevidence.SummarizeCandidateRetrievals(statuses))
	summary := videoevidence.SummarizeCandidateRetrievals(statuses)
	if summary == "incomplete" {
		return fail("candidate_retrieval_failed")
	}
	step.Thought = ""
	state.RoundSteps = append(state.RoundSteps, step)
	*messagesPtr = append(*messagesPtr, chat.Message{
		Role: "user", Content: fmt.Sprintf(
			"Native per-video retrieval is complete. Candidate status: %s. Answer the original question (%s) now as one %s JSON object. Set coverage=partial if any candidate had no evidence; name the unresolved gap without inventing a citation. For complete coverage cite validated evidence from EVERY candidate; never reuse another video's citation.",
			summary, strings.TrimSpace(query), videoevidence.AnswerContractVersion),
	})
	return iterOutcomeNext, nil
}
