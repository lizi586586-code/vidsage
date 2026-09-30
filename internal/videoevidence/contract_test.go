package videoevidence

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func testEvidence() Evidence {
	return Evidence{
		EvidenceSentenceID:   "evs:v1:one",
		ChunkID:              "chunk-1",
		KnowledgeID:          "knowledge-1",
		VideoID:              "video-1",
		VideoTitle:           "测试视频",
		StartMs:              1000,
		EndMs:                3000,
		TranscriptGeneration: "generation-1",
		SourceType:           SourceTypeTranscript,
		Linkable:             false,
	}
}

func testScope() Scope {
	scope := NewScope()
	scope.CurrentGeneration["video-1"] = "generation-1"
	scope.Add(testEvidence())
	return scope
}

func TestNormalizeCandidateUsesCanonicalScopeValues(t *testing.T) {
	got, err := NormalizeCandidate(Candidate{
		EvidenceSentenceID:   "evs:v1:one",
		VideoID:              "forged-video",
		VideoTitle:           "伪造标题",
		TranscriptGeneration: "forged-generation",
		SourceType:           SourceTypeTranscript,
	}, testScope())
	require.NoError(t, err)
	require.Equal(t, testEvidence().VideoID, got.VideoID)
	require.Equal(t, testEvidence().VideoTitle, got.VideoTitle)
	require.Equal(t, testEvidence().TranscriptGeneration, got.TranscriptGeneration)
	require.Equal(t, 1000, got.StartMs)
	require.Equal(t, 3000, got.EndMs)
	require.True(t, got.Linkable)
}

func TestProtocolPromptRequiresInlineCitationForVideoLocations(t *testing.T) {
	prompt := ProtocolPrompt()
	require.Contains(t, prompt, "MUST cite the supporting handle inline")
	require.Contains(t, prompt, "Do not write start times")
	require.Contains(t, prompt, "time is not verified")
}

func TestSimulatedAgentCanOptIntoCapabilityWithStandardAdapter(t *testing.T) {
	// A new agent only declares the capability and supplies a source adapter;
	// the platform still owns canonical identity, timing, and generation.
	simulatedAgent := struct {
		VideoEvidenceCitation string
		EvidenceAdapter       Adapter
	}{
		VideoEvidenceCitation: Version,
		EvidenceAdapter:       SearchResultAdapter{},
	}
	require.True(t, Supports(simulatedAgent.VideoEvidenceCitation))

	result := &types.SearchResult{
		ID:             "chunk-1",
		KnowledgeID:    "knowledge-1",
		KnowledgeTitle: "伪造标题不会覆盖规范标题",
		Metadata: map[string]string{
			"evidence_sentence_id":  "evs:v1:one",
			"source_chunk_id":       "chunk-1",
			"video_id":              "forged-video",
			"video_title":           "伪造标题",
			"start_ms":              "9000",
			"end_ms":                "12000",
			"transcript_generation": "forged-generation",
			"source_type":           SourceTypeTranscript,
		},
	}
	evidence, err := ResolveAdapterOutput(context.Background(), simulatedAgent.EvidenceAdapter, AdapterInput{
		SearchResults: []*types.SearchResult{result},
		Scope:         testScope(),
	})
	require.NoError(t, err)
	require.Len(t, evidence, 1)
	require.Equal(t, testEvidence().VideoID, evidence[0].VideoID)
	require.Equal(t, testEvidence().VideoTitle, evidence[0].VideoTitle)
	require.Equal(t, testEvidence().StartMs, evidence[0].StartMs)
	require.Equal(t, testEvidence().EndMs, evidence[0].EndMs)
}

func TestProjectReferencesKeepsCoverageRulesSharedAcrossAgents(t *testing.T) {
	valid := &types.SearchResult{
		ID:          "chunk-1",
		KnowledgeID: "knowledge-1",
		Metadata: map[string]string{
			"evidence_sentence_id":  "evs:v1:one",
			"source_type":           SourceTypeTranscript,
			"video_id":              "video-1",
			"video_title":           "测试视频",
			"start_ms":              "1000",
			"end_ms":                "3000",
			"transcript_generation": "generation-1",
		},
	}
	invalid := &types.SearchResult{
		ID: "wiki-1",
		Metadata: map[string]string{
			"source_type": SourceTypeWiki,
		},
	}
	projected, coverage := ProjectReferences([]*types.SearchResult{valid, invalid})
	require.Equal(t, "complete", coverage)
	require.Len(t, projected, 1)
	require.Equal(t, "evs:v1:one", projected[0].EvidenceSentenceID)
}

func TestProjectReferencesAcceptanceCoverageMatrix(t *testing.T) {
	makeResult := func(evidenceID, chunkID, videoID, title, generation string, start, end int) *types.SearchResult {
		return &types.SearchResult{
			ID: chunkID, KnowledgeID: chunkID, KnowledgeTitle: title,
			Metadata: map[string]string{
				"evidence_sentence_id":  evidenceID,
				"source_chunk_id":       chunkID,
				"video_id":              videoID,
				"video_title":           title,
				"start_ms":              strconv.Itoa(start),
				"end_ms":                strconv.Itoa(end),
				"transcript_generation": generation,
				"source_type":           SourceTypeTranscript,
			},
		}
	}
	validA := makeResult("evs:v1:a", "chunk-a", "video-a", "视频 A", "generation-a", 1000, 2000)
	validB := makeResult("evs:v1:b", "chunk-b", "video-b", "视频 B", "generation-b", 3000, 5000)
	invalid := makeResult("evs:v1:invalid", "chunk-invalid", "video-c", "视频 C", "generation-c", 5000, 5000)

	tests := []struct {
		name     string
		refs     []*types.SearchResult
		coverage string
		count    int
	}{
		{name: "current video", refs: []*types.SearchResult{validA}, coverage: "complete", count: 1},
		{name: "cross video", refs: []*types.SearchResult{validA, validB}, coverage: "complete", count: 2},
		{name: "multi video with invalid evidence", refs: []*types.SearchResult{validA, invalid}, coverage: "partial", count: 1},
		{name: "no evidence", refs: nil, coverage: "none", count: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projected, coverage := ProjectReferences(tt.refs)
			require.Equal(t, tt.coverage, coverage)
			require.Len(t, projected, tt.count)
		})
	}
}

func TestValidateComparisonAnswerRequiresTwoBlocksAndTwoVideos(t *testing.T) {
	query := "请比较《视频一》和《视频二》的定义"
	contract := AnswerContract{
		Blocks: []AnswerBlock{
			{Type: "comparison"},
			{Type: "summary"},
		},
	}
	oneVideo := AnswerProjection{Evidence: []Evidence{{VideoID: "video-1"}}}
	require.Error(t, ValidateComparisonAnswer(query, contract, oneVideo))

	contract.Blocks = append(contract.Blocks, AnswerBlock{Type: "comparison"})
	require.NoError(t, ValidateComparisonAnswer(query, contract, AnswerProjection{
		Evidence: []Evidence{{VideoID: "video-1"}, {VideoID: "video-2"}},
	}))
}

func TestValidateComparisonAnswerUsesTaskSemanticsWithoutParsingTitles(t *testing.T) {
	contract := AnswerContract{
		Blocks: []AnswerBlock{
			{Type: "evidence"},
		},
	}
	err := ValidateComparisonAnswer("两个视频分别在哪一部分提到了提示词模板？", contract, AnswerProjection{
		Evidence: []Evidence{{VideoID: "video-1"}},
	})
	if got := answerErrorCode(err); got != AnswerErrorMissingVideoCoverage {
		t.Fatalf("error code = %q, want %q", got, AnswerErrorMissingVideoCoverage)
	}
}

func TestValidateComparisonAnswerRequiresLocationTableAfterThreeLocations(t *testing.T) {
	contract := AnswerContract{
		ContentMarkdown: "定位如下。",
		Blocks: []AnswerBlock{{
			Type:         "evidence",
			TextMarkdown: "第一处 <ref id=\"c1\"/> 第二处 <ref id=\"c2\"/> 第三处 <ref id=\"c3\"/> 第四处 <ref id=\"c4\"/>",
		}},
	}
	projection := AnswerProjection{Evidence: []Evidence{
		{EvidenceSentenceID: "e1", VideoID: "v1"},
		{EvidenceSentenceID: "e2", VideoID: "v1"},
		{EvidenceSentenceID: "e3", VideoID: "v1"},
		{EvidenceSentenceID: "e4", VideoID: "v1"},
	}}
	projection.RenderedMarkdown = RenderAnswerContract(contract)
	require.Equal(t, AnswerErrorMissingVideoTable, answerErrorCode(ValidateComparisonAnswer("请定位相关内容", contract, projection)))

	contract.Blocks[0].TextMarkdown = "| 定位 | 核心主旨 |\n| --- | --- |\n| 第一处完整定位句 <ref id=\"c1\"/> | 主旨一 |\n| 第二处完整定位句 <ref id=\"c2\"/> | 主旨二 |\n| 第三处完整定位句 <ref id=\"c3\"/> | 主旨三 |\n| 第四处完整定位句 <ref id=\"c4\"/> | 主旨四 |"
	projection.RenderedMarkdown = RenderAnswerContract(contract)
	require.NoError(t, ValidateComparisonAnswer("请定位相关内容", contract, projection))
}

func TestProjectAnswerContractRepairsMissingVideoLocationTableDeterministically(t *testing.T) {
	contract := AnswerContract{
		SchemaVersion:   AnswerContractVersion,
		Mode:            AnswerModeReasoning,
		Coverage:        AnswerCoverageComplete,
		ContentMarkdown: "四处内容共同说明了提示词和上下文的关系。",
		Blocks: []AnswerBlock{
			{Type: "evidence", Title: "角色设定", TextMarkdown: "角色设定明确 AI 的身份和任务 <ref id=\"c1\"/>。", EvidenceRefs: []string{"c1"}},
			{Type: "evidence", Title: "提示词公式", TextMarkdown: "公式还需要补充背景和输出要求 <ref id=\"c2\"/>。", EvidenceRefs: []string{"c2"}},
			{Type: "evidence", Title: "上下文窗口", TextMarkdown: "上下文窗口限制单次对话容量 <ref id=\"c3\"/>。", EvidenceRefs: []string{"c3"}},
			{Type: "evidence", Title: "失忆原因", TextMarkdown: "新对话不会自动继承旧上下文 <ref id=\"c4\"/>。", EvidenceRefs: []string{"c4"}},
		},
	}
	handles := map[string]Evidence{
		"c1": {EvidenceSentenceID: "e1", VideoID: "v1", SourceType: SourceTypeTranscript, Linkable: true, ContentExcerpt: "角色加任务加要求加格式"},
		"c2": {EvidenceSentenceID: "e2", VideoID: "v1", SourceType: SourceTypeTranscript, Linkable: true, ContentExcerpt: "说清不要什么，给个参考"},
		"c3": {EvidenceSentenceID: "e3", VideoID: "v2", SourceType: SourceTypeTranscript, Linkable: true, ContentExcerpt: "上下文像一叠有限高度的纸条"},
		"c4": {EvidenceSentenceID: "e4", VideoID: "v2", SourceType: SourceTypeTranscript, Linkable: true, ContentExcerpt: "新对话不会自动继承旧上下文"},
	}

	projection, err := ProjectAnswerContract(contract, handles)
	require.NoError(t, err)
	require.NoError(t, ValidateComparisonAnswer("两个视频分别在哪里讲的", contract, projection))
	require.Contains(t, projection.RenderedMarkdown, "| 时间位置 | 内容 |")
	require.Contains(t, projection.RenderedMarkdown, `<ref id="c4"/>`)
}

func TestProjectAnswerContractMergesRepeatedGistsIntoSingleLocationRow(t *testing.T) {
	contract := AnswerContract{
		SchemaVersion:   AnswerContractVersion,
		Mode:            AnswerModeReasoning,
		Coverage:        AnswerCoverageComplete,
		ContentMarkdown: "两个视频从不同角度讲了AI使用技巧。",
		Blocks: []AnswerBlock{
			{Type: "evidence", Title: "两视频主要观点对比", TextMarkdown: "一个教怎么说，一个讲为什么忘。 <ref id=\"c1\"/> <ref id=\"c2\"/>", EvidenceRefs: []string{"c1", "c2"}},
			{Type: "evidence", Title: "《AI提示词是什么》核心观点", TextMarkdown: "灯神比喻与万能公式。 <ref id=\"c3\"/> <ref id=\"c4\"/> <ref id=\"c5\"/>", EvidenceRefs: []string{"c3", "c4", "c5"}},
		},
	}
	handles := map[string]Evidence{
		"c1": {EvidenceSentenceID: "e1", VideoID: "v1", SourceType: SourceTypeTranscript, Linkable: true},
		"c2": {EvidenceSentenceID: "e2", VideoID: "v2", SourceType: SourceTypeTranscript, Linkable: true},
		"c3": {EvidenceSentenceID: "e3", VideoID: "v1", SourceType: SourceTypeTranscript, Linkable: true},
		"c4": {EvidenceSentenceID: "e4", VideoID: "v1", SourceType: SourceTypeTranscript, Linkable: true},
		"c5": {EvidenceSentenceID: "e5", VideoID: "v1", SourceType: SourceTypeTranscript, Linkable: true},
	}

	projection, err := ProjectAnswerContract(contract, handles)
	require.NoError(t, err)
	require.Contains(t, projection.RenderedMarkdown, "| 时间位置 | 内容 |")
	for _, ref := range []string{"c1", "c2", "c3", "c4", "c5"} {
		require.Contains(t, projection.RenderedMarkdown, `<ref id="`+ref+`"/>`)
	}
	// 同一块引用的多个时间点必须合并为一行，主旨文本不得按引用次数重复
	require.Equal(t, 1, strings.Count(projection.RenderedMarkdown, "两视频主要观点对比："))
	require.Equal(t, 1, strings.Count(projection.RenderedMarkdown, "《AI提示词是什么》核心观点："))
}

func TestContentExcerptQuotesSpokenTextOnly(t *testing.T) {
	chunk := "## 视频定位信息\n\n```json\n{\"chunk_index\": 358, \"start_ms\": 2370012, \"end_ms\": 2371580}\n```\n\n## 原文\n\n是理解能力的差异。"
	require.Equal(t, "是理解能力的差异。", contentExcerpt(chunk))
	// 只有定位 JSON 的块：摘录取 chunk_text 的话语文本，不显示原始 JSON
	locatorOnly := "```json\n{\n  \"chunk_index\": 1,\n  \"chunk_text\": \"到底什么是ai提示词？你就把ai想象成一个超级听话但又超级死板的阿拉丁灯神。\",\n  \"end_ms\": 170004\n}\n```"
	require.Equal(t, "到底什么是ai提示词？你就把ai想象成一个超级听话但又超级死板的阿拉丁灯神。", contentExcerpt(locatorOnly))
	require.Equal(t, "", contentExcerpt("   "))
	// 超长原文截断到 120 字符
	long := strings.Repeat("长", 200)
	excerpt := contentExcerpt("## 原文\n\n" + long)
	require.Len(t, []rune(excerpt), 123) // 120 + "..."
}

func TestNormalizeCandidateClassifiesStableFailures(t *testing.T) {
	tests := []struct {
		name      string
		candidate Candidate
		scope     Scope
		code      ErrorCode
	}{
		{
			name:      "invalid handle",
			candidate: Candidate{},
			scope:     testScope(),
			code:      ErrorInvalidHandle,
		},
		{
			name: "out of scope",
			candidate: Candidate{
				EvidenceSentenceID: "evs:v1:other",
			},
			scope: testScope(),
			code:  ErrorEvidenceOutOfScope,
		},
		{
			name: "missing video",
			candidate: Candidate{
				EvidenceSentenceID: "evs:v1:missing-video",
			},
			scope: func() Scope {
				scope := NewScope()
				scope.Add(Evidence{
					EvidenceSentenceID:   "evs:v1:missing-video",
					StartMs:              1,
					EndMs:                2,
					TranscriptGeneration: "generation-1",
					SourceType:           SourceTypeTranscript,
				})
				return scope
			}(),
			code: ErrorMissingVideo,
		},
		{
			name: "missing time range",
			candidate: Candidate{
				EvidenceSentenceID: "evs:v1:missing-time",
			},
			scope: func() Scope {
				scope := NewScope()
				scope.Add(Evidence{
					EvidenceSentenceID:   "evs:v1:missing-time",
					VideoID:              "video-1",
					TranscriptGeneration: "generation-1",
					SourceType:           SourceTypeTranscript,
				})
				return scope
			}(),
			code: ErrorMissingTimeRange,
		},
		{
			name: "generation mismatch",
			candidate: Candidate{
				EvidenceSentenceID: "evs:v1:one",
			},
			scope: func() Scope {
				scope := testScope()
				scope.CurrentGeneration["video-1"] = "generation-2"
				return scope
			}(),
			code: ErrorGenerationMismatch,
		},
		{
			name: "wiki only",
			candidate: Candidate{
				SourceType: SourceTypeWiki,
			},
			scope: testScope(),
			code:  ErrorWikiOnlySource,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NormalizeCandidate(tt.candidate, tt.scope)
			require.Equal(t, tt.code, CodeOf(err))
		})
	}
}

func TestJSONAdapterAcceptsValidPayloadAndRejectsContractDrift(t *testing.T) {
	scope := testScope()
	adapter := JSONAdapter{}

	candidates, err := adapter.Normalize(context.Background(), AdapterInput{
		RawJSON: []byte(`{"references":[{"evidence_sentence_id":"evs:v1:one"}]}`),
		Scope:   scope,
	})
	require.NoError(t, err)
	require.Len(t, candidates, 1)

	tests := []struct {
		name string
		raw  string
		code ErrorCode
	}{
		{
			name: "unknown field",
			raw:  `{"references":[{"evidence_sentence_id":"evs:v1:one","unexpected":true}]}`,
			code: ErrorUnknownField,
		},
		{
			name: "trailing content",
			raw:  `{"references":[{"evidence_sentence_id":"evs:v1:one"}]}{"more":true}`,
			code: ErrorTrailingContent,
		},
		{
			name: "truncated output",
			raw:  `{"references":[`,
			code: ErrorTruncatedOutput,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := adapter.Normalize(context.Background(), AdapterInput{
				RawJSON: []byte(tt.raw),
				Scope:   scope,
			})
			require.Equal(t, tt.code, CodeOf(err))
		})
	}
}

func TestJSONAdapterAllowsAtMostOneCorrection(t *testing.T) {
	calls := 0
	adapter := JSONAdapter{
		Repair: func(_ context.Context, _ string) (string, error) {
			calls++
			return `{"references":[{"evidence_sentence_id":"evs:v1:one"}]}`, nil
		},
	}
	candidates, err := adapter.Normalize(context.Background(), AdapterInput{
		RawJSON: []byte(`{"references":[`),
		Scope:   testScope(),
	})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, 1, calls)

	failing := JSONAdapter{
		Repair: func(_ context.Context, _ string) (string, error) {
			return `{"references":[`, nil
		},
	}
	_, err = failing.Normalize(context.Background(), AdapterInput{
		RawJSON: []byte(`not-json`),
		Scope:   testScope(),
	})
	require.Equal(t, ErrorCorrectionFailed, CodeOf(err))

	noRepair := JSONAdapter{}
	_, err = noRepair.Normalize(context.Background(), AdapterInput{
		RawJSON: []byte(`not-json`),
		Scope:   testScope(),
	})
	require.Equal(t, ErrorInvalidJSON, CodeOf(err))
}

func TestMergeReferencesPrefersVideoEvidenceForSameChunk(t *testing.T) {
	ordinary := &types.SearchResult{ID: "chunk-1", KnowledgeID: "knowledge-1"}
	video := &types.SearchResult{
		ID:             "chunk-1",
		KnowledgeID:    "knowledge-1",
		KnowledgeTitle: "测试视频",
		Metadata: map[string]string{
			metadataEvidenceID: "evs:v1:one",
			metadataChunkID:    "chunk-1",
			metadataVideoID:    "video-1",
			metadataStartMs:    "1000",
			metadataEndMs:      "3000",
			metadataGeneration: "generation-1",
			metadataSourceType: SourceTypeTranscript,
		},
	}

	merged := MergeReferences([]*types.SearchResult{ordinary}, []*types.SearchResult{video})
	require.Len(t, merged, 1)
	require.Equal(t, "evs:v1:one", merged[0].Metadata[metadataEvidenceID])
}

func TestMergeReferencesKeepsDistinctEvidenceSentencesInOneChunk(t *testing.T) {
	first := testReference("evs:v1:first", 1000, 2000)
	second := testReference("evs:v1:second", 2200, 3000)

	merged := MergeReferences(nil, []*types.SearchResult{first, second})

	require.Len(t, merged, 2)
	require.Equal(t, "evs:v1:first", merged[0].Metadata[metadataEvidenceID])
	require.Equal(t, "evs:v1:second", merged[1].Metadata[metadataEvidenceID])
}

func TestMergeReferencesDeduplicatesSameEvidenceSentence(t *testing.T) {
	first := testReference("evs:v1:one", 1000, 2000)
	duplicate := testReference("evs:v1:one", 1000, 2000)

	merged := MergeReferences(nil, []*types.SearchResult{first, duplicate})

	require.Len(t, merged, 1)
}

func TestCandidateFromMapRequiresNestedStructuredMetadata(t *testing.T) {
	candidate, ok := CandidateFromMap(map[string]interface{}{
		"chunk_id":             "chunk-1",
		"evidence_sentence_id": "evs:v1:forged",
		"video_id":             "video-forged",
		"source_type":          SourceTypeTranscript,
	})

	require.False(t, ok)
	require.Empty(t, candidate)
}

func TestEnrichSearchResultReadsCanonicalTranscriptMetadataOnly(t *testing.T) {
	result := &types.SearchResult{
		ID:             "chunk-1",
		ChunkType:      types.ChunkTypeText,
		KnowledgeTitle: "测试视频",
		Content:        "## 视频定位信息\n\n```json\n{\"video_id\":\"video-1\",\"evidence_sentence_id\":\"evs:v1:one\",\"start_ms\":1000,\"end_ms\":3000,\"transcript_generation\":\"generation-1\"}\n```\n\n## 原文\n\n真实内容",
	}
	EnrichSearchResult(result)
	candidate, ok := CandidateFromSearchResult(result)
	require.True(t, ok)
	require.Equal(t, "video-1", candidate.VideoID)
	require.Equal(t, "evs:v1:one", candidate.EvidenceSentenceID)
	require.Equal(t, 1000, *candidate.StartMs)
	require.Equal(t, 3000, *candidate.EndMs)
}

func TestEnrichSearchResultReadsBackendKnowledgeMetadataWhenChunkBodyIsSplit(t *testing.T) {
	result := &types.SearchResult{
		ID:             "chunk-1",
		ChunkType:      types.ChunkTypeText,
		KnowledgeTitle: "测试视频",
		Content:        "## 视频定位信息\n```json\n{\"evidence_sentence_id\":\"evs:v1:split\"\n",
		Metadata: map[string]string{
			"content": "## 视频定位信息\n\n```json\n{\"video_id\":\"video-1\",\"evidence_sentence_id\":\"evs:v1:split\",\"start_ms\":1000,\"end_ms\":3000,\"transcript_generation\":\"generation-1\"}\n```\n\n## 原文\n真实内容",
		},
	}

	EnrichSearchResult(result)
	candidate, ok := CandidateFromSearchResult(result)
	require.True(t, ok)
	require.Equal(t, "video-1", candidate.VideoID)
	require.Equal(t, "evs:v1:split", candidate.EvidenceSentenceID)
	require.Equal(t, 1000, *candidate.StartMs)
	require.Equal(t, 3000, *candidate.EndMs)
}

func TestSummaryChunksCannotBecomeTimelineEvidence(t *testing.T) {
	content := "## 视频定位信息\n\n```json\n" +
		`{"video_id":"video-1","evidence_sentence_id":"evs:v1:summary","start_ms":1000,"end_ms":3000,"transcript_generation":"generation-1"}` +
		"\n```\n\n# Summary\n这是一段总结，不是时间轴原文。"

	t.Run("content enrichment skipped", func(t *testing.T) {
		result := &types.SearchResult{
			ID:        "summary-chunk",
			ChunkType: types.ChunkTypeSummary,
			Content:   content,
		}
		EnrichSearchResult(result)
		candidate, ok := CandidateFromSearchResult(result)
		require.False(t, ok)
		require.Empty(t, candidate)
	})

	t.Run("structured metadata still rejected", func(t *testing.T) {
		candidate, ok := CandidateFromMap(map[string]interface{}{
			"chunk_id":   "summary-chunk",
			"chunk_type": types.ChunkTypeSummary,
			"metadata": map[string]interface{}{
				metadataEvidenceID: "evs:v1:summary",
				metadataVideoID:    "video-1",
				metadataStartMs:    "1000",
				metadataEndMs:      "3000",
				metadataGeneration: "generation-1",
				metadataSourceType: SourceTypeTranscript,
			},
		})
		require.False(t, ok)
		require.Empty(t, candidate)
	})
}

func TestValidationErrorCodeDoesNotExposePayload(t *testing.T) {
	err := &ValidationError{Code: ErrorInvalidJSON, Err: errors.New("raw prompt secret")}
	require.Equal(t, ErrorInvalidJSON, CodeOf(err))
	require.NotContains(t, err.Error(), "raw prompt secret")
}

func testReference(evidenceID string, startMs, endMs int) *types.SearchResult {
	return &types.SearchResult{
		ID:          "chunk-1",
		KnowledgeID: "knowledge-1",
		Metadata: map[string]string{
			metadataEvidenceID: evidenceID,
			metadataChunkID:    "chunk-1",
			metadataVideoID:    "video-1",
			metadataStartMs:    strconv.Itoa(startMs),
			metadataEndMs:      strconv.Itoa(endMs),
			metadataGeneration: "generation-1",
			metadataSourceType: SourceTypeTranscript,
		},
	}
}
