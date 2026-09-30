package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/videoevidence"
	"github.com/stretchr/testify/require"
)

type candidateSearchTool struct {
	agenttools.BaseTool
	calls   []string
	missing string
	failing string
}

func (t *candidateSearchTool) Execute(_ context.Context, raw json.RawMessage) (*types.ToolResult, error) {
	var input agenttools.KnowledgeSearchInput
	if err := json.Unmarshal(raw, &input); err != nil || len(input.KnowledgeIDs) != 1 {
		return &types.ToolResult{Success: false}, fmt.Errorf("expected one document ID")
	}
	id := input.KnowledgeIDs[0]
	t.calls = append(t.calls, id)
	if id == t.failing {
		return &types.ToolResult{Success: false}, fmt.Errorf("retrieval failed")
	}
	if id == t.missing {
		return &types.ToolResult{Success: true, Data: map[string]interface{}{"display_type": "search_results", "results": []interface{}{}}}, nil
	}
	return &types.ToolResult{Success: true, Data: map[string]interface{}{
		"display_type": "search_results", "results": []interface{}{candidateRow(id)},
	}}, nil
}

func candidateRow(id string) map[string]interface{} {
	return map[string]interface{}{
		"id": "chunk-" + id, "knowledge_id": id, "knowledge_base_id": "kb",
		"knowledge_title": "视频" + id, "chunk_type": "text", "content": "提示词介绍",
		"metadata": map[string]string{
			"source_type": "transcript", "evidence_sentence_id": "ev-" + id,
			"video_id": "video-" + id, "video_title": "视频" + id,
			"start_ms": "1000", "end_ms": "2000", "transcript_generation": "g1",
		},
	}
}

func candidateFixture(t *testing.T) (*AgentEngine, *types.AgentState, *candidateSearchTool) {
	t.Helper()
	engine := newTestEngine(t, &mockChat{})
	engine.config.AnswerContractEnabled = true
	tool := &candidateSearchTool{BaseTool: agenttools.NewBaseTool(agenttools.ToolKnowledgeSearch, "test", json.RawMessage(`{"type":"object"}`))}
	engine.toolRegistry = agenttools.NewToolRegistry()
	engine.toolRegistry.RegisterTool(tool)
	state := &types.AgentState{}
	for _, id := range []string{"1", "2", "3"} {
		ref := searchResultFromMap(candidateRow(id))
		state.KnowledgeRefs = append(state.KnowledgeRefs, ref)
	}
	return engine, state, tool
}

const threeCandidates = `{"version": "1", "task_type": "multi_video_location", "topic": "提示词", "videos": [{"title":"视频1","knowledge_base_id":"kb"},{"title":"视频2","knowledge_base_id":"kb"},{"title":"视频3","knowledge_base_id":"kb"}]}`

func TestCandidateRetrievalThreeVideosPerIDAndCoverage(t *testing.T) {
	engine, state, tool := candidateFixture(t)
	messages := emptyMessages()
	outcome, err := engine.resolveCandidateManifest(t.Context(), state, &messages, types.AgentStep{}, threeCandidates, "sess", "msg", "三个视频分别在哪里讲提示词")
	require.NoError(t, err)
	require.Equal(t, iterOutcomeNext, outcome)
	require.Equal(t, []string{"1", "2", "3"}, tool.calls)
	for _, id := range tool.calls {
		require.Equal(t, string(videoevidence.RetrievalEvidenceFound), state.CandidateSearchStatus[id])
	}
	require.Equal(t, []string{"1", "2", "3"}, state.RequiredKnowledgeIDs)
	require.Len(t, engine.videoEvidenceHandles(state.KnowledgeRefs), 3)
	require.NotContains(t, fmt.Sprint(messages), `"task_type"`)
}

func TestCandidateRetrievalFailureAndNoEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, missing, failing, status string
		want                           iterOutcome
	}{
		{"no evidence", "2", "", string(videoevidence.RetrievalSearchedNoResult), iterOutcomeNext},
		{"tool failed", "", "2", string(videoevidence.RetrievalFailed), iterOutcomeBreak},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, state, tool := candidateFixture(t)
			tool.missing, tool.failing = tc.missing, tc.failing
			messages := []chat.Message{{Role: "user", Content: "test"}}
			outcome, err := engine.resolveCandidateManifest(t.Context(), state, &messages, types.AgentStep{}, threeCandidates, "sess", "msg", "三个视频分别在哪里讲提示词")
			require.NoError(t, err)
			require.Equal(t, tc.want, outcome)
			require.Equal(t, tc.status, state.CandidateSearchStatus["2"])
			require.Len(t, tool.calls, 3)
		})
	}
}

func TestCandidateManifestInvalidAndCancelledDoNotSearch(t *testing.T) {
	for _, raw := range []string{`{"version":"1","task_type":"multi_video_location"`, `{"version":"1","task_type":"multi_video_location","topic":"t","videos":[{"title":"视频1"},{"title":"未知视频"}]}`} {
		engine, state, tool := candidateFixture(t)
		messages := emptyMessages()
		outcome, err := engine.resolveCandidateManifest(t.Context(), state, &messages, types.AgentStep{}, raw, "sess", "msg", "三个视频分别在哪里讲提示词")
		require.NoError(t, err)
		// First invalid attempt gets one retry (iterOutcomeContinue); only
		// the second attempt with the same failure stops.
		if state.CandidateManifestRetries == 1 {
			require.Equal(t, iterOutcomeContinue, outcome)
			require.Empty(t, tool.calls)
			// Simulate a second failure with the same raw input
			outcome2, err2 := engine.resolveCandidateManifest(t.Context(), state, &messages, types.AgentStep{}, raw, "sess", "msg", "三个视频分别在哪里讲提示词")
			require.NoError(t, err2)
			require.Equal(t, iterOutcomeBreak, outcome2)
			require.Empty(t, tool.calls)
			require.Equal(t, "failed", state.CompletionStatus)
		} else {
			require.Equal(t, iterOutcomeBreak, outcome)
			require.Empty(t, tool.calls)
			require.Equal(t, "failed", state.CompletionStatus)
		}
	}
	engine, state, tool := candidateFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	messages := emptyMessages()
	_, err := engine.resolveCandidateManifest(ctx, state, &messages, types.AgentStep{}, threeCandidates, "sess", "msg", "三个视频分别在哪里讲提示词")
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, tool.calls)
}

func TestCandidateAnswerCompleteCannotUseOldEvidenceAfterSearchMiss(t *testing.T) {
	engine, state, tool := candidateFixture(t)
	tool.missing = "3"
	messages := emptyMessages()
	_, err := engine.resolveCandidateManifest(t.Context(), state, &messages, types.AgentStep{}, threeCandidates, "sess", "msg", "三个视频分别在哪里讲提示词")
	require.NoError(t, err)
	require.Error(t, candidateAnswerCoverageError(state, videoevidence.AnswerCoverageComplete))
	require.NoError(t, candidateAnswerCoverageError(state, videoevidence.AnswerCoveragePartial))
}

func TestExecuteLoopCandidateJSONWithWhitespaceSearchesAllThree(t *testing.T) {
	completeAnswer := `{"schema_version":"answer_contract/v1","mode":"reasoning","coverage":"complete","content_markdown":"三个视频都讲了提示词。","blocks":[{"type":"evidence","title":"定位","text_markdown":"视频一见 <ref id=\"c1\"/>。","evidence_refs":["c1"]},{"type":"evidence","title":"定位","text_markdown":"视频二见 <ref id=\"c2\"/>。","evidence_refs":["c2"]},{"type":"evidence","title":"定位","text_markdown":"视频三见 <ref id=\"c3\"/>。","evidence_refs":["c3"]}]}`
	model := &mockChat{responses: []mockResponse{
		{chunks: []types.StreamResponse{{Content: threeCandidates, Done: true, FinishReason: "stop"}}},
		{chunks: []types.StreamResponse{{Content: `{"schema_version":"answer_contract/v1","mode":"reasoning","coverage":"partial","content_markdown":"检索已完成，但目前仍需进一步核对。","blocks":[{"type":"summary","title":"状态","text_markdown":"三个视频已经逐条检索。","evidence_refs":[]}]}`, Done: true, FinishReason: "stop"}}},
		{chunks: []types.StreamResponse{{Content: completeAnswer, Done: true, FinishReason: "stop"}}},
	}}
	engine, state, tool := candidateFixture(t)
	engine.chatModel = model
	_, err := engine.executeLoop(t.Context(), state, "三个视频分别在哪里讲提示词", emptyMessages(), emptyTools(), "sess", "msg")
	require.NoError(t, err)
	require.Equal(t, []string{"1", "2", "3"}, tool.calls)
	require.NotEqual(t, "candidate_json_invalid", state.CompletionFailureReason)
}

func TestThreeVideoFinalAnswerMustCiteEachCandidate(t *testing.T) {
	engine, state, _ := candidateFixture(t)
	messages := emptyMessages()
	_, err := engine.resolveCandidateManifest(t.Context(), state, &messages, types.AgentStep{}, threeCandidates, "sess", "msg", "三个视频分别在哪里讲提示词")
	require.NoError(t, err)
	handles := engine.videoEvidenceHandles(state.KnowledgeRefs)
	keys := make([]string, 0, len(handles))
	for handle := range handles {
		keys = append(keys, handle)
	}
	sort.Strings(keys)
	require.Len(t, keys, 3)
	makeContract := func(refs []string) videoevidence.AnswerContract {
		blocks := make([]videoevidence.AnswerBlock, 0, len(refs))
		for _, handle := range refs {
			blocks = append(blocks, videoevidence.AnswerBlock{
				Type: "evidence", Title: "定位", TextMarkdown: "该视频的说明见 <ref id=\"" + handle + "\"/>。", EvidenceRefs: []string{handle},
			})
		}
		return videoevidence.AnswerContract{
			SchemaVersion: videoevidence.AnswerContractVersion, Mode: videoevidence.AnswerModeReasoning,
			Coverage: videoevidence.AnswerCoverageComplete, ContentMarkdown: "逐条定位。", Blocks: blocks,
		}
	}
	for _, tc := range []struct {
		name  string
		refs  []string
		valid bool
	}{
		{"all three", keys, true}, {"third omitted", keys[:2], false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contract := makeContract(tc.refs)
			projection, err := engine.projectAnswerContract(contract, state.KnowledgeRefs)
			require.NoError(t, err)
			err = videoevidence.ValidateComparisonAnswerForKnowledgeIDs("三个视频分别在哪里讲提示词", contract, projection, state.RequiredKnowledgeIDs)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.True(t, strings.Contains(err.Error(), "missing_video_coverage"))
			}
		})
	}
}
