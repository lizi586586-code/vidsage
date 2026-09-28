package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

func TestBuildAutoRoutePromptIncludesConversationContext(t *testing.T) {
	prompt := buildAutoRoutePrompt("有哪些值得记录的知识点？", "当前视频：产品培训")
	if want := "当前视频：产品培训"; !strings.Contains(prompt, want) {
		t.Fatalf("auto route prompt = %q, want it to include context %q", prompt, want)
	}
}

func TestAutoRoutePromptUsesEvidenceComplexityPrinciple(t *testing.T) {
	for _, want := range []string{"直接证据", "综合结论", "知识点", "视频数量", "video-chat-route/v1", "required_capabilities"} {
		if !strings.Contains(autoRouteSystemPrompt, want) {
			t.Fatalf("system prompt = %q, want principle %q", autoRouteSystemPrompt, want)
		}
	}
}

func TestParseAutoRouteDecision(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "quick", raw: `{"schema_version":"video-chat-route/v1","mode":"quick","intent":"location","scope_hint":"current_video","required_capabilities":["video_evidence"],"reason_code":"explicit_location_question"}`, want: routeModeQuick},
		{name: "reasoning", raw: `{"schema_version":"video-chat-route/v1","mode":"reasoning","intent":"comparison","scope_hint":"global_videos","required_capabilities":["video_evidence"],"reason_code":"comparison_request"}`, want: routeModeReasoning},
		{name: "unknown mode", raw: `{"schema_version":"video-chat-route/v1","mode":"normal","intent":"location","scope_hint":"current_video","required_capabilities":["video_evidence"],"reason_code":"explicit_location_question"}`, wantErr: true},
		{name: "unknown field", raw: `{"schema_version":"video-chat-route/v1","mode":"quick","intent":"location","scope_hint":"current_video","required_capabilities":["video_evidence"],"reason_code":"explicit_location_question","extra":"x"}`, wantErr: true},
		{name: "trailing text", raw: `{"schema_version":"video-chat-route/v1","mode":"quick","intent":"location","scope_hint":"current_video","required_capabilities":["video_evidence"],"reason_code":"explicit_location_question"}解释`, wantErr: true},
		{name: "markdown fence", raw: "```json\n{\"schema_version\":\"video-chat-route/v1\",\"mode\":\"quick\",\"intent\":\"location\",\"scope_hint\":\"current_video\",\"required_capabilities\":[\"video_evidence\"],\"reason_code\":\"explicit_location_question\"}\n```", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseAutoRouteDecision(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseAutoRouteDecision() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got.Mode != tt.want {
				t.Fatalf("parseAutoRouteDecision() = %q, want %q", got.Mode, tt.want)
			}
		})
	}
}

func TestRouteParseErrorCodeClassifiesProtocolFailures(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{`{"schema_version":"video-chat-route/v1","extra":1}`, "unknown_field"},
		{`{"schema_version":"video-chat-route/v1"} trailing`, "trailing_content"},
		{`not-json`, "invalid_json"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			_, err := parseAutoRouteDecision(tt.raw)
			if got := routeParseErrorCode(err); got != tt.want {
				t.Fatalf("routeParseErrorCode() = %q, want %q (err=%v)", got, tt.want, err)
			}
		})
	}
}

func TestAutoRouteChatOptionsDisableThinking(t *testing.T) {
	options := autoRouteChatOptions()
	if options == nil || options.Thinking == nil {
		t.Fatal("auto route options must explicitly set thinking")
	}
	if *options.Thinking {
		t.Fatal("auto route classification must disable model thinking")
	}
	if options.MaxTokens != 512 {
		t.Fatalf("auto route max tokens = %d, want 512", options.MaxTokens)
	}
}

func TestBuildAutoRoutePromptLabelsExecutionBoundary(t *testing.T) {
	prompt := buildAutoRoutePrompt("两个视频分别讲了什么？", "", "global_videos")
	if !strings.Contains(prompt, "可信执行边界") || !strings.Contains(prompt, "不是目标视频集合") || !strings.Contains(prompt, "global_videos") {
		t.Fatalf("prompt does not distinguish execution scope from semantic targets: %q", prompt)
	}
}

func TestTrustedExecutionScopeNeverTreatsTitlesAsVideoSelection(t *testing.T) {
	tests := []struct {
		name       string
		requested  string
		kbIDs      []string
		knowledge  []string
		wantScope  string
		wantStatus string
	}{
		{"global boundary", "global_videos", []string{"kb-1"}, nil, "global_videos", "computed"},
		{"current page boundary", "current_video", []string{"kb-1"}, []string{"chunk-1"}, "current_video", "computed"},
		{"mismatched global request narrows", "global_videos", []string{"kb-1"}, []string{"chunk-1"}, "selected_knowledge", "computed"},
		{"unknown boundary", "", nil, nil, "", "not_exposed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotScope, gotStatus := trustedExecutionScope(tt.requested, tt.kbIDs, tt.knowledge)
			if gotScope != tt.wantScope || gotStatus != tt.wantStatus {
				t.Fatalf("trustedExecutionScope() = (%q, %q), want (%q, %q)", gotScope, gotStatus, tt.wantScope, tt.wantStatus)
			}
		})
	}
}

func TestSelectRouteModelIDDoesNotFallbackFromExplicitModel(t *testing.T) {
	models := []*types.Model{
		{ID: "minimax", Type: types.ModelTypeKnowledgeQA, IsDefault: true},
		{ID: "deepseek", Type: types.ModelTypeKnowledgeQA},
	}
	got, err := selectRouteModelID("missing", models)
	if !errors.Is(err, errRouteModelUnavailable) {
		t.Fatalf("selectRouteModelID() error = %v, want explicit model unavailable", err)
	}
	if got != "" {
		t.Fatalf("selectRouteModelID() = %q, want no fallback model", got)
	}
}

func TestSelectRouteModelIDUsesStableDefaultWhenUnspecified(t *testing.T) {
	models := []*types.Model{
		{ID: "z-model", Type: types.ModelTypeKnowledgeQA},
		{ID: "a-model", Type: types.ModelTypeKnowledgeQA},
	}
	got, err := selectRouteModelID("", models)
	if err != nil {
		t.Fatalf("selectRouteModelID() error = %v", err)
	}
	if got != "a-model" {
		t.Fatalf("selectRouteModelID() = %q, want stable lowest ID", got)
	}
}

func TestRouteHistoryTimeoutDoesNotCancelClassification(t *testing.T) {
	parent := context.Background()
	historyCtx, historyCancel := context.WithCancel(parent)
	historyCancel()
	classificationCtx, classificationCancel := context.WithCancel(parent)
	defer classificationCancel()

	if err := historyCtx.Err(); err == nil {
		t.Fatal("history context should be independently cancellable")
	}
	if err := classificationCtx.Err(); err != nil {
		t.Fatalf("classification context was cancelled with history: %v", err)
	}
}

type routeChatProbe struct {
	options  *chat.ChatOptions
	messages []chat.Message
	chatFn   func(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error)
}

func (p *routeChatProbe) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	p.messages = append([]chat.Message(nil), messages...)
	p.options = options
	if p.chatFn != nil {
		return p.chatFn(ctx, messages, options)
	}
	return &types.ChatResponse{Content: `{"schema_version":"video-chat-route/v1","mode":"reasoning","intent":"comparison","scope_hint":"global_videos","required_capabilities":["video_evidence"],"reason_code":"multi_video_synthesis"}`}, nil
}

func (p *routeChatProbe) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}

func (p *routeChatProbe) GetModelName() string { return "route-probe" }

func (p *routeChatProbe) GetModelID() string { return "route-probe" }

type routeMessageServiceProbe struct {
	interfaces.MessageService
	getRecent func(context.Context, string, int) ([]*types.Message, error)
}

func (p *routeMessageServiceProbe) GetRecentMessagesBySession(ctx context.Context, sessionID string, limit int) ([]*types.Message, error) {
	if p.getRecent != nil {
		return p.getRecent(ctx, sessionID, limit)
	}
	return nil, nil
}

type routeModelServiceProbe struct {
	interfaces.ModelService
	model         *types.Model
	chatModel     chat.Chat
	getModelErr   error
	getChatErr    error
	listModelsErr error
	listCalls     int
	chatCalls     []string
}

func (p *routeModelServiceProbe) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	if p.getModelErr != nil {
		return nil, p.getModelErr
	}
	if p.model == nil || p.model.ID != id {
		return nil, fmt.Errorf("model %q not found", id)
	}
	return p.model, nil
}

func (p *routeModelServiceProbe) ListModels(_ context.Context) ([]*types.Model, error) {
	p.listCalls++
	if p.listModelsErr != nil {
		return nil, p.listModelsErr
	}
	if p.model == nil {
		return nil, nil
	}
	return []*types.Model{p.model}, nil
}

func (p *routeModelServiceProbe) GetChatModel(_ context.Context, id string) (chat.Chat, error) {
	p.chatCalls = append(p.chatCalls, id)
	if p.getChatErr != nil {
		return nil, p.getChatErr
	}
	return p.chatModel, nil
}

func routeProbeHandler(messageService interfaces.MessageService, modelService interfaces.ModelService) *Handler {
	return &Handler{
		messageService: messageService,
		modelService:   modelService,
	}
}

func withShortRouteTimeouts(t *testing.T) {
	t.Helper()
	historyTimeout := autoRouteHistoryTimeout
	classificationTimeout := autoRouteClassificationTimeout
	autoRouteHistoryTimeout = 20 * time.Millisecond
	autoRouteClassificationTimeout = 30 * time.Millisecond
	t.Cleanup(func() {
		autoRouteHistoryTimeout = historyTimeout
		autoRouteClassificationTimeout = classificationTimeout
	})
}

func TestRouteAgentPreservesFullCompoundQuestionAndDisablesThinking(t *testing.T) {
	probe := &routeChatProbe{}
	modelService := &routeModelServiceProbe{
		model:     &types.Model{ID: "deepseek", Type: types.ModelTypeKnowledgeQA},
		chatModel: probe,
	}
	handler := routeProbeHandler(nil, modelService)
	query := `AI提示词是什么？和AI上下文是什么？这两个视频的总结部分分别在什么位置，各自总结了哪些要点`

	decision := handler.routeAgent(context.Background(), query, &types.Session{ID: "session-1", Description: "当前页面包含两个视频"}, "deepseek")

	if decision.Mode != routeModeReasoning {
		t.Fatalf("routeAgent() mode = %q, want reasoning", decision.Mode)
	}
	if len(probe.messages) != 2 {
		t.Fatalf("routeAgent() sent %d messages, want system and user", len(probe.messages))
	}
	if !strings.Contains(probe.messages[1].Content, query) {
		t.Fatalf("routeAgent() prompt lost compound question: %q", probe.messages[1].Content)
	}
	if probe.options == nil || probe.options.Thinking == nil || *probe.options.Thinking {
		t.Fatalf("routeAgent() options = %#v, want Thinking=false", probe.options)
	}
	if len(modelService.chatCalls) != 1 || modelService.chatCalls[0] != "deepseek" {
		t.Fatalf("routeAgent() chat calls = %v, want [deepseek]", modelService.chatCalls)
	}
}

func TestRouteAgentHistoryTimeoutDoesNotCancelModelCall(t *testing.T) {
	withShortRouteTimeouts(t)
	probe := &routeChatProbe{
		chatFn: func(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("classification context unexpectedly cancelled: %w", err)
			}
			return &types.ChatResponse{Content: `{"schema_version":"video-chat-route/v1","mode":"quick","intent":"location","scope_hint":"current_video","required_capabilities":["video_evidence"],"reason_code":"explicit_location_question"}`}, nil
		},
	}
	messageService := &routeMessageServiceProbe{
		getRecent: func(ctx context.Context, _ string, _ int) ([]*types.Message, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	modelService := &routeModelServiceProbe{
		model:     &types.Model{ID: "deepseek", Type: types.ModelTypeKnowledgeQA},
		chatModel: probe,
	}
	handler := routeProbeHandler(messageService, modelService)

	decision := handler.routeAgent(context.Background(), "总结位置在哪里？", &types.Session{ID: "session-1"}, "deepseek")

	if decision.Mode != routeModeQuick {
		t.Fatalf("routeAgent() mode = %q, want quick after history timeout", decision.Mode)
	}
	if len(probe.messages) != 2 {
		t.Fatalf("routeAgent() model call count = %d, want 1", len(probe.messages))
	}
	if !strings.Contains(probe.messages[1].Content, "历史消息") || !strings.Contains(probe.messages[1].Content, "不可用") {
		t.Fatalf("routeAgent() prompt should disclose unavailable history: %q", probe.messages[1].Content)
	}
}

func TestRouteAgentModelTimeoutFallsBackToReasoning(t *testing.T) {
	withShortRouteTimeouts(t)
	probe := &routeChatProbe{
		chatFn: func(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	modelService := &routeModelServiceProbe{
		model:     &types.Model{ID: "deepseek", Type: types.ModelTypeKnowledgeQA},
		chatModel: probe,
	}
	handler := routeProbeHandler(nil, modelService)

	decision := handler.routeAgent(context.Background(), "这两个视频分别讲了什么？", nil, "deepseek")

	if decision.Mode != routeModeReasoning || decision.ScopeHint != "global_videos" {
		t.Fatalf("routeAgent() fallback = %#v, want conservative reasoning/global_videos", decision)
	}
}

func TestRouteAgentExplicitModelFailureDoesNotFallbackToAnotherProvider(t *testing.T) {
	probe := &routeChatProbe{}
	modelService := &routeModelServiceProbe{
		model:      &types.Model{ID: "deepseek", Type: types.ModelTypeKnowledgeQA},
		chatModel:  probe,
		getChatErr: errors.New("deepseek unavailable"),
	}
	handler := routeProbeHandler(nil, modelService)

	decision := handler.routeAgent(context.Background(), "问题", nil, "deepseek")

	if decision.Mode != routeModeReasoning {
		t.Fatalf("routeAgent() mode = %q, want reasoning fallback", decision.Mode)
	}
	if modelService.listCalls != 0 {
		t.Fatalf("routeAgent() listed models after explicit failure, want no provider fallback")
	}
	if len(modelService.chatCalls) != 1 || modelService.chatCalls[0] != "deepseek" {
		t.Fatalf("routeAgent() chat calls = %v, want exactly [deepseek]", modelService.chatCalls)
	}
}
