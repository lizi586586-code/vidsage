package session

import (
	"context"
	"testing"
	"time"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/videoevidence"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingStreamManager struct {
	events []interfaces.StreamEvent
}

func (m *recordingStreamManager) AppendEvent(_ context.Context, _, _ string, event interfaces.StreamEvent) error {
	m.events = append(m.events, event)
	return nil
}

func (m *recordingStreamManager) GetEvents(_ context.Context, _, _ string, fromOffset int) ([]interfaces.StreamEvent, int, error) {
	if fromOffset >= len(m.events) {
		return nil, len(m.events), nil
	}
	return m.events[fromOffset:], len(m.events), nil
}

func TestProjectVideoEvidenceComputesCoverageFromBackendMetadata(t *testing.T) {
	refs := []*types.SearchResult{
		{
			ID: "chunk-1",
			Metadata: map[string]string{
				"source_type":           "transcript",
				"evidence_sentence_id":  "evs:1",
				"video_id":              "video-1",
				"video_title":           "视频一",
				"start_ms":              "1000",
				"end_ms":                "3000",
				"transcript_generation": "generation-1",
			},
		},
		{
			ID: "chunk-2",
			Metadata: map[string]string{
				"source_type":           "transcript",
				"evidence_sentence_id":  "evs:2",
				"video_id":              "video-2",
				"video_title":           "视频二",
				"start_ms":              "4000",
				"end_ms":                "6000",
				"transcript_generation": "generation-2",
			},
		},
	}

	projected, coverage := projectVideoEvidence(refs)
	require.Equal(t, "complete", coverage)
	require.Len(t, projected, 2)
	require.Equal(t, "video-1", projected[0].VideoID)
}

func TestVideoEvidenceReferencesFromToolDataUsesStructuredMetadata(t *testing.T) {
	refs := videoEvidenceReferencesFromToolData(agenttools.ToolGrepChunks, map[string]interface{}{
		"display_type": "grep_results",
		"chunk_results": []map[string]interface{}{
			{
				"knowledge_id":      "knowledge-1",
				"knowledge_title":   "AI提示词课程",
				"knowledge_base_id": "kb-1",
				"chunk_id":          "chunk-1",
				"metadata": map[string]string{
					"source_type":           "transcript",
					"evidence_sentence_id":  "evs:v1:prompt",
					"video_id":              "video-1",
					"start_ms":              "1000",
					"end_ms":                "3000",
					"transcript_generation": "generation-1",
				},
			},
			{
				"knowledge_id":  "knowledge-2",
				"match_snippet": `普通文本，没有证据 ID`,
			},
		},
	})

	require.Len(t, refs, 1)
	assert.Equal(t, "chunk-1", refs[0].ID)
	assert.Equal(t, "knowledge-1", refs[0].KnowledgeID)
	assert.Equal(t, "AI提示词课程", refs[0].KnowledgeTitle)
	assert.Equal(t, "transcript", refs[0].Metadata["source_type"])
	assert.Equal(t, "evs:v1:prompt", refs[0].Metadata["evidence_sentence_id"])
	assert.Equal(t, "video-1", refs[0].Metadata["video_id"])
}

func TestVideoEvidenceReferencesFromToolDataReadsListedChunks(t *testing.T) {
	refs := videoEvidenceReferencesFromToolData(agenttools.ToolListKnowledgeChunks, map[string]interface{}{
		"display_type": "knowledge_chunks_list",
		"chunks": []map[string]interface{}{
			{
				"chunk_id":        "chunk-1",
				"knowledge_id":    "knowledge-1",
				"knowledge_title": "AI提示词课程",
				"metadata": map[string]string{
					"source_type":           "transcript",
					"evidence_sentence_id":  "evs:v1:prompt",
					"video_id":              "video-1",
					"start_ms":              "180901",
					"end_ms":                "196741",
					"transcript_generation": "generation-1",
				},
			},
		},
	})

	require.Len(t, refs, 1)
	assert.Equal(t, "chunk-1", refs[0].ID)
	assert.Equal(t, "evs:v1:prompt", refs[0].Metadata["evidence_sentence_id"])
}

func TestVideoEvidenceReferencesFromToolDataEnrichesTranscriptBody(t *testing.T) {
	content := "## 视频定位信息\n```json\n" +
		`{"evidence_sentence_id":"evs:v1:prompt","video_id":"video-1","video_title":"AI提示词课程","start_ms":180901,"end_ms":196741,"transcript_generation":"generation-1"}` +
		"\n```\n总结一下"
	refs := videoEvidenceReferencesFromToolData(agenttools.ToolListKnowledgeChunks, map[string]interface{}{
		"display_type": "knowledge_chunks_list",
		"chunks": []map[string]interface{}{
			{
				"chunk_id":     "chunk-1",
				"knowledge_id": "knowledge-1",
				"content":      content,
			},
		},
	})

	require.Len(t, refs, 1)
	assert.Equal(t, "video-1", refs[0].Metadata["video_id"])
	assert.Equal(t, "180901", refs[0].Metadata["start_ms"])
	assert.Equal(t, "196741", refs[0].Metadata["end_ms"])
}

func TestHandleCompleteDoesNotPersistEmptyFailedAnswer(t *testing.T) {
	message := &types.Message{ID: "assistant-1", SessionID: "session-1"}
	stream := &recordingStreamManager{}
	handler := NewAgentStreamHandler(
		context.Background(), "session-1", "assistant-1", "request-1", 1, time.Now(),
		message, stream, event.NewEventBus(), nil,
	)

	err := handler.handleComplete(context.Background(), event.Event{
		Type: event.EventAgentComplete,
		Data: event.AgentCompleteData{
			MessageID:     "assistant-1",
			Outcome:       "failed",
			FailureReason: "answer_contract_truncated",
		},
	})

	require.NoError(t, err)
	require.True(t, message.IsCompleted)
	require.Equal(t, "回答生成不完整，请重试。", message.Content)
	require.NotEmpty(t, stream.events)
}

func TestHandleCompleteDoesNotPersistEmptyMissingVideoCoverageAnswer(t *testing.T) {
	message := &types.Message{ID: "assistant-1", SessionID: "session-1"}
	stream := &recordingStreamManager{}
	handler := NewAgentStreamHandler(
		context.Background(), "session-1", "assistant-1", "request-1", 1, time.Now(),
		message, stream, event.NewEventBus(), nil,
	)

	err := handler.handleComplete(context.Background(), event.Event{
		Type: event.EventAgentComplete,
		Data: event.AgentCompleteData{
			MessageID:     "assistant-1",
			Outcome:       "failed",
			FailureReason: "missing_video_coverage",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "已找到部分视频证据，但尚未覆盖问题涉及的全部视频，请重试。", message.Content)
	require.NotEmpty(t, stream.events)
}

func TestHandleCompleteDoesNotPersistEmptyMissingVideoCoverageAnswerWithoutOutcome(t *testing.T) {
	message := &types.Message{ID: "assistant-1", SessionID: "session-1"}
	stream := &recordingStreamManager{}
	handler := NewAgentStreamHandler(
		context.Background(), "session-1", "assistant-1", "request-1", 1, time.Now(),
		message, stream, event.NewEventBus(), nil,
	)

	err := handler.handleComplete(context.Background(), event.Event{
		Type: event.EventAgentComplete,
		Data: event.AgentCompleteData{
			MessageID:     "assistant-1",
			FailureReason: "missing_video_coverage",
		},
	})

	require.NoError(t, err)
	require.Equal(t, "已找到部分视频证据，但尚未覆盖问题涉及的全部视频，请重试。", message.Content)
	require.NotEmpty(t, stream.events)
}

func TestHandleCompleteMarksFailedAnswerCoverageAsPartial(t *testing.T) {
	message := &types.Message{ID: "assistant-1", SessionID: "session-1"}
	stream := &recordingStreamManager{}
	handler := NewAgentStreamHandler(
		context.Background(), "session-1", "assistant-1", "request-1", 1, time.Now(),
		message, stream, event.NewEventBus(), nil,
	)
	handler.knowledgeRefs = []*types.SearchResult{{
		ID:          "chunk-1",
		KnowledgeID: "knowledge-1",
		Metadata: map[string]string{
			"evidence_sentence_id":  "evs:v1:one",
			"source_type":           videoevidence.SourceTypeTranscript,
			"video_id":              "video-1",
			"video_title":           "视频一",
			"start_ms":              "1000",
			"end_ms":                "2000",
			"transcript_generation": "generation-1",
		},
	}}

	require.NoError(t, handler.handleComplete(context.Background(), event.Event{
		Type: event.EventAgentComplete,
		Data: event.AgentCompleteData{
			MessageID:     "assistant-1",
			Outcome:       "failed",
			FailureReason: "answer_contract_invalid",
		},
	}))

	require.NotEmpty(t, stream.events)
	data := stream.events[len(stream.events)-1].Data
	require.Equal(t, "partial", data["coverage"])
}

func TestHandleCompleteExposesBackendRouteStatus(t *testing.T) {
	message := &types.Message{ID: "assistant-1", SessionID: "session-1", AgentID: types.BuiltinSmartReasoningID}
	stream := &recordingStreamManager{}
	handler := NewAgentStreamHandler(
		context.Background(), "session-1", "assistant-1", "request-1", 1, time.Now(),
		message, stream, event.NewEventBus(), nil,
	)
	handler.SetRouteMetadata(routeMetadata{
		AutoRoute:            true,
		IntentStatus:         "not_exposed",
		ScopeHint:            "global_videos",
		ExecutionScope:       "global_videos",
		EffectiveScope:       "global_videos",
		EffectiveScopeStatus: "computed",
		SchemaValid:          false,
		ErrorCode:            "unknown_field",
	})

	require.NoError(t, handler.handleComplete(context.Background(), event.Event{
		Type: event.EventAgentComplete,
		Data: event.AgentCompleteData{MessageID: "assistant-1", Outcome: "succeeded"},
	}))

	require.NotEmpty(t, stream.events)
	data := stream.events[len(stream.events)-1].Data
	assert.Nil(t, data["route_intent"])
	assert.Equal(t, "not_exposed", data["route_intent_status"])
	assert.Equal(t, "global_videos", data["effective_scope"])
	assert.Equal(t, "global_videos", data["execution_scope"])
	assert.Equal(t, false, data["route_schema_valid"])
	assert.Equal(t, "unknown_field", data["route_error_code"])
}
