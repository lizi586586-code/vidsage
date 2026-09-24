package session

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
)

const (
	routeModeQuick     = "quick"
	routeModeReasoning = "reasoning"

	// Routing happens before the SSE stream is established. Keep history
	// lookup independent so a slow database read cannot consume the model's
	// entire classification budget.
)

var (
	autoRouteHistoryTimeout        = 1 * time.Second
	autoRouteClassificationTimeout = 5 * time.Second
)

var errRouteModelUnavailable = errors.New("route model unavailable")

const autoRouteSystemPrompt = `你是 VideoHub 问题路由器。你的唯一任务是把用户本轮问题路由为 quick 或 reasoning，不回答问题，不检索资料。

判定原则：
1. quick：检索到少量直接证据后即可回答，不需要重新组织多个证据或推导综合结论。
2. reasoning：需要分析、归纳、综合多个证据，解释原因，形成总结、对比、应用方案、学习方案或回答多个子问题。
3. “知识点”“核心观点”“讲了什么”等词本身不能决定路径；如果需要通读、归纳或提炼整段内容，属于 reasoning。
4. 用户明确要求“分别列出”“共同点和差异”“如何应用”“给步骤/示例”时通常属于 reasoning，但仍以本轮问题和上下文的实际复杂度判断。
5. 视频数量、是否在单视频页面、知识库范围都只是上下文信息，不是 quick/reasoning 的直接判据。
6. 如果仅凭本轮问题无法判断，结合必要的会话上下文；仍无法确定时选择 reasoning。

只能使用输入中的上下文判断，不得创建视频名称、视频 ID、证据 ID 或时间。
只输出一个严格 JSON 对象，不要 Markdown、代码围栏、解释或尾随文本。`

type autoRouteDecision struct {
	SchemaVersion        string   `json:"schema_version"`
	Mode                 string   `json:"mode"`
	Intent               string   `json:"intent"`
	ScopeHint            string   `json:"scope_hint"`
	RequiredCapabilities []string `json:"required_capabilities"`
	ReasonCode           string   `json:"reason_code"`
}

func buildAutoRoutePrompt(query, contextText string) string {
	prompt := "当前问题：\n" + strings.TrimSpace(query)
	if strings.TrimSpace(contextText) != "" {
		prompt += "\n\n最近会话上下文（仅用于判断问题是否依赖上下文）：\n" + contextText
	}
	return prompt
}

var allowedRouteIntents = map[string]bool{
	"quick_fact": true, "location": true, "summary": true, "explanation": true,
	"comparison": true, "application": true, "learning_plan": true,
}

var allowedRouteReasons = map[string]bool{
	"explicit_location_question": true, "explicit_fact_question": true,
	"multi_video_synthesis": true, "comparison_request": true,
	"application_request": true, "context_dependent_followup": true,
	"uncertain": true,
}

func fallbackAutoRouteDecision() autoRouteDecision {
	return autoRouteDecision{
		SchemaVersion:        "video-chat-route/v1",
		Mode:                 routeModeReasoning,
		Intent:               "explanation",
		ScopeHint:            "global_videos",
		RequiredCapabilities: []string{"video_evidence"},
		ReasonCode:           "uncertain",
	}
}

func parseAutoRouteDecision(raw string) (autoRouteDecision, error) {
	var decision autoRouteDecision
	decoder := json.NewDecoder(bytes.NewReader([]byte(strings.TrimSpace(raw))))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decision); err != nil {
		return autoRouteDecision{}, fmt.Errorf("route output is not valid JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return autoRouteDecision{}, fmt.Errorf("route output has trailing content")
	}
	if decision.SchemaVersion != "video-chat-route/v1" {
		return autoRouteDecision{}, fmt.Errorf("unsupported schema_version %q", decision.SchemaVersion)
	}
	if decision.Mode != routeModeQuick && decision.Mode != routeModeReasoning {
		return autoRouteDecision{}, fmt.Errorf("unsupported mode %q", decision.Mode)
	}
	if !allowedRouteIntents[decision.Intent] ||
		(decision.ScopeHint != "current_video" && decision.ScopeHint != "global_videos") ||
		len(decision.RequiredCapabilities) != 1 || decision.RequiredCapabilities[0] != "video_evidence" ||
		!allowedRouteReasons[decision.ReasonCode] {
		return autoRouteDecision{}, fmt.Errorf("route output violates protocol")
	}
	return decision, nil
}

func autoRouteChatOptions() *chat.ChatOptions {
	thinking := false
	return &chat.ChatOptions{
		Temperature: 0,
		MaxTokens:   128,
		Thinking:    &thinking,
	}
}

// selectRouteModelID chooses one stable model ID. An explicit request is
// authoritative: if it is unavailable, do not silently switch providers.
func selectRouteModelID(requestedID string, models []*types.Model) (string, error) {
	requestedID = strings.TrimSpace(requestedID)
	if requestedID != "" {
		for _, model := range models {
			if model != nil && model.ID == requestedID && model.Type == types.ModelTypeKnowledgeQA {
				return model.ID, nil
			}
		}
		return "", fmt.Errorf("%w: %s", errRouteModelUnavailable, requestedID)
	}

	candidates := make([]*types.Model, 0, len(models))
	for _, model := range models {
		if model != nil && model.Type == types.ModelTypeKnowledgeQA {
			candidates = append(candidates, model)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].IsDefault != candidates[j].IsDefault {
			return candidates[i].IsDefault
		}
		return candidates[i].ID < candidates[j].ID
	})
	if len(candidates) == 0 {
		return "", errRouteModelUnavailable
	}
	return candidates[0].ID, nil
}

// routeAgent performs one bounded, non-streaming classification call. Any
// failure is deliberately converted to reasoning; routing must never block a
// user's question or produce an unscoped answer.
func (h *Handler) routeAgent(ctx context.Context, query string, session *types.Session, modelID string) autoRouteDecision {
	fallback := fallbackAutoRouteDecision()
	routeStartedAt := time.Now()
	logFailure := func(phase, reason string, err error) {
		elapsed := time.Since(routeStartedAt).Round(time.Millisecond)
		if err != nil {
			logger.Warnf(ctx, "auto route failed phase=%s reason=%s elapsed=%s err=%v", phase, reason, elapsed, err)
			return
		}
		logger.Warnf(ctx, "auto route failed phase=%s reason=%s elapsed=%s", phase, reason, elapsed)
	}
	contextText := ""
	if session != nil {
		if description := strings.TrimSpace(session.Description); description != "" {
			contextText = "会话上下文：\n" + description
		}
	}
	if h.messageService != nil && session != nil {
		historyCtx, historyCancel := context.WithTimeout(ctx, autoRouteHistoryTimeout)
		history, historyErr := h.messageService.GetRecentMessagesBySession(historyCtx, session.ID, 6)
		historyCancel()
		if historyErr == nil {
			var b strings.Builder
			for _, message := range history {
				if message == nil || strings.TrimSpace(message.Content) == "" {
					continue
				}
				content := strings.TrimSpace(message.Content)
				if len(content) > 600 {
					content = content[:600]
				}
				fmt.Fprintf(&b, "%s: %s\n", message.Role, content)
			}
			if historyText := strings.TrimSpace(b.String()); historyText != "" {
				if contextText != "" {
					contextText += "\n"
				}
				contextText += historyText
			}
		} else {
			reason := "history_error"
			if errors.Is(historyErr, context.DeadlineExceeded) {
				reason = "history_timeout"
			}
			logFailure("history", reason, historyErr)
			if contextText != "" {
				contextText += "\n"
			}
			contextText += "最近会话上下文：不可用（本次路由未使用历史消息）"
		}
	}
	routeCtx, cancel := context.WithTimeout(ctx, autoRouteClassificationTimeout)
	defer cancel()
	model, err := h.resolveRouteModel(routeCtx, modelID)
	if err != nil {
		logFailure("model_resolve", "model_unavailable", err)
		return fallback
	}
	userPrompt := buildAutoRoutePrompt(query, contextText)
	response, err := model.Chat(routeCtx, []chat.Message{
		{Role: "system", Content: autoRouteSystemPrompt},
		{Role: "user", Content: userPrompt},
	}, autoRouteChatOptions())
	if err != nil || response == nil {
		if err != nil {
			reason := "model_call_error"
			if errors.Is(err, context.DeadlineExceeded) {
				reason = "model_call_timeout"
			}
			logFailure("model_call", reason, err)
		} else {
			logFailure("model_call", "empty_response", nil)
		}
		return fallback
	}
	decision, err := parseAutoRouteDecision(response.Content)
	if err != nil {
		logFailure("model_output", "invalid_json", err)
		return fallback
	}
	logger.Infof(ctx, "auto route succeeded mode=%s intent=%s scope_hint=%s reason=%s elapsed=%s",
		decision.Mode, decision.Intent, decision.ScopeHint, decision.ReasonCode, time.Since(routeStartedAt).Round(time.Millisecond))
	return decision
}

func (h *Handler) resolveRouteModel(ctx context.Context, requestedID string) (chat.Chat, error) {
	if h.modelService == nil {
		return nil, fmt.Errorf("model service is unavailable")
	}
	if id := strings.TrimSpace(requestedID); id != "" {
		model, err := h.modelService.GetModelByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errRouteModelUnavailable, err)
		}
		if model == nil || model.Type != types.ModelTypeKnowledgeQA {
			return nil, fmt.Errorf("%w: %s is not a KnowledgeQA model", errRouteModelUnavailable, id)
		}
		return h.modelService.GetChatModel(ctx, id)
	}
	models, err := h.modelService.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: list models: %v", errRouteModelUnavailable, err)
	}
	id, err := selectRouteModelID("", models)
	if err != nil {
		return nil, err
	}
	return h.modelService.GetChatModel(ctx, id)
}
