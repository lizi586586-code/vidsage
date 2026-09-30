package agent

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/modelcontext"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/videoevidence"
	"github.com/stretchr/testify/require"
)

func TestProjectAnswerContractUsesRequestLocalVideoEvidenceWhitelist(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	require.Equal(t, "c1", engine.modelContext.RegisterChunk(modelcontext.ChunkReference{
		ChunkID: "chunk-1",
	}))

	ref := &types.SearchResult{
		ID:        "chunk-1",
		ChunkType: types.ChunkTypeText,
		Metadata: map[string]string{
			"source_type":           videoevidence.SourceTypeTranscript,
			"evidence_sentence_id":  "evs:1",
			"video_id":              "video-1",
			"video_title":           "AI提示词",
			"start_ms":              "1000",
			"end_ms":                "3000",
			"transcript_generation": "generation-1",
		},
	}
	contract, err := videoevidence.ParseAnswerContract(
		`{"schema_version":"answer_contract/v1","mode":"reasoning","coverage":"complete","content_markdown":"定位见 <ref id=\"c1\"/>","blocks":[{"type":"evidence","title":"定位","text_markdown":"原文 <ref id=\"c1\"/>","evidence_refs":["c1"]}]}`,
	)
	require.NoError(t, err)

	projection, err := engine.projectAnswerContract(contract, []*types.SearchResult{ref})
	require.NoError(t, err)
	require.Equal(t, videoevidence.AnswerCoverageComplete, projection.Coverage)
	require.Len(t, projection.Evidence, 1)
	require.Equal(t, "video-1", projection.Evidence[0].VideoID)
}

func TestProjectAnswerContractRejectsSummaryReference(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	require.Equal(t, "c1", engine.modelContext.RegisterChunk(modelcontext.ChunkReference{
		ChunkID: "summary-1",
	}))

	ref := &types.SearchResult{
		ID:        "summary-1",
		ChunkType: types.ChunkTypeSummary,
		Metadata: map[string]string{
			"source_type":           videoevidence.SourceTypeTranscript,
			"evidence_sentence_id":  "evs:summary",
			"video_id":              "video-1",
			"video_title":           "AI提示词",
			"start_ms":              "1000",
			"end_ms":                "3000",
			"transcript_generation": "generation-1",
		},
	}
	contract, err := videoevidence.ParseAnswerContract(
		`{"schema_version":"answer_contract/v1","mode":"reasoning","coverage":"complete","content_markdown":"总结 <ref id=\"c1\"/>","blocks":[{"type":"summary","title":"总结","text_markdown":"总结 <ref id=\"c1\"/>","evidence_refs":["c1"]}]}`,
	)
	require.NoError(t, err)

	projection, err := engine.projectAnswerContract(contract, []*types.SearchResult{ref})
	require.NoError(t, err)
	require.Empty(t, projection.Evidence)
}

func TestProjectAnswerContractKeepsWikiCitationOutsideVideoEvidence(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	videoRef := &types.SearchResult{
		ID:        "transcript-1",
		ChunkType: types.ChunkTypeText,
		Metadata: map[string]string{
			"source_type":           videoevidence.SourceTypeTranscript,
			"evidence_sentence_id":  "evs:1",
			"video_id":              "video-1",
			"video_title":           "AI提示词",
			"start_ms":              "1000",
			"end_ms":                "3000",
			"transcript_generation": "generation-1",
		},
	}
	wikiRef := &types.SearchResult{
		ID:              "wiki-1",
		ChunkType:       "wiki",
		KnowledgeID:     "wiki-object-1",
		KnowledgeBaseID: "kb-wiki",
		KnowledgeTitle:  "AI上下文概念",
		Content:         "Wiki 对概念作了补充说明。",
		Metadata: map[string]string{
			"source_type": videoevidence.SourceTypeWiki,
		},
	}
	refs := []*types.SearchResult{videoRef, wikiRef}
	engine.modelContext.RegisterSearchResults(refs)
	videoHandle := engine.modelContext.ChunkHandle(videoRef.ID)
	wikiHandle := engine.modelContext.ChunkHandle(wikiRef.ID)
	require.Equal(t, "c1", videoHandle)
	require.Equal(t, "c2", wikiHandle)

	contract, err := videoevidence.ParseAnswerContract(
		`{"schema_version":"answer_contract/v1","mode":"reasoning","coverage":"complete","content_markdown":"视频定位 <ref id=\"c1\"/>；概念解释 <ref id=\"c2\"/>","blocks":[{"type":"evidence","title":"混合来源","text_markdown":"视频定位 <ref id=\"c1\"/>；概念解释 <ref id=\"c2\"/>","evidence_refs":["c1","c2"]}]}`,
	)
	require.NoError(t, err)

	projection, err := engine.projectAnswerContract(contract, refs)
	require.NoError(t, err)
	require.Len(t, projection.Evidence, 1)
	require.Equal(t, "video-1", projection.Evidence[0].VideoID)
	require.Contains(t, engine.modelContext.DecodeOutputText(projection.RenderedMarkdown), `chunk_id="wiki-1"`)
}

func TestAnswerReferencesFromToolCallsKeepsOrdinaryWikiRows(t *testing.T) {
	refs := answerReferencesFromToolCalls([]types.ToolCall{{
		Name: "knowledge_search",
		Result: &types.ToolResult{
			Success: true,
			Data: map[string]interface{}{
				"display_type": "search_results",
				"results": []map[string]interface{}{
					{
						"chunk_id":          "wiki-1",
						"chunk_type":        "wiki",
						"knowledge_id":      "wiki-object-1",
						"knowledge_base_id": "kb-wiki",
						"knowledge_title":   "AI上下文概念",
						"metadata": map[string]string{
							"source_type": videoevidence.SourceTypeWiki,
						},
						"content": "Wiki 对概念作了补充说明。",
					},
				},
			},
		},
	}})

	require.Len(t, refs, 1)
	require.Equal(t, "wiki-1", refs[0].ID)
	require.Equal(t, videoevidence.SourceTypeWiki, refs[0].Metadata["source_type"])
}
