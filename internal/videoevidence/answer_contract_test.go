package videoevidence

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func answerContractJSON() string {
	return `{"schema_version":"answer_contract/v1","mode":"quick","coverage":"complete","content_markdown":"定位见 <ref id=\"c1\"/>","blocks":[{"type":"evidence","title":"定位","text_markdown":"原文","evidence_refs":["c1"]}]}`
}

func answerHandleScope() map[string]Evidence {
	return map[string]Evidence{
		"c1": {
			EvidenceSentenceID:   "evs:1",
			VideoID:              "video-1",
			VideoTitle:           "视频一",
			StartMs:              1000,
			EndMs:                3000,
			TranscriptGeneration: "generation-1",
			SourceType:           SourceTypeTranscript,
			Linkable:             true,
		},
	}
}

func TestParseAnswerContractRejectsProtocolDrift(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		code AnswerErrorCode
	}{
		{name: "unknown field", raw: `{"schema_version":"answer_contract/v1","mode":"quick","coverage":"complete","content_markdown":"x","blocks":[],"extra":1}`, code: AnswerErrorUnknownField},
		{name: "trailing content", raw: answerContractJSON() + " trailing", code: AnswerErrorTrailingContent},
		{name: "truncated", raw: `{"schema_version":"answer_contract/v1","mode":"quick","coverage":"complete","content_markdown":"x","blocks":[`, code: AnswerErrorTruncatedOutput},
		{name: "null blocks", raw: `{"schema_version":"answer_contract/v1","mode":"quick","coverage":"complete","content_markdown":"x","blocks":null}`, code: AnswerErrorInvalidContract},
		{name: "citation block mismatch", raw: `{"schema_version":"answer_contract/v1","mode":"quick","coverage":"complete","content_markdown":"定位见 <ref id=\"c1\"/>","blocks":[{"type":"evidence","title":"定位","text_markdown":"原文","evidence_refs":[]}]}`, code: AnswerErrorInvalidContract},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseAnswerContract(test.raw)
			require.Equal(t, test.code, answerErrorCode(err))
		})
	}
}

func TestParseAnswerContractAcceptsSingleJSONCodeFenceWrapper(t *testing.T) {
	raw := "```json\n" + answerContractJSON() + "\n```"

	contract, err := ParseAnswerContract(raw)
	require.NoError(t, err)
	require.Equal(t, AnswerContractVersion, contract.SchemaVersion)
}

func TestParseAnswerContractAddsDeclaredBlockCitationsWhenModelOmitsInlineTags(t *testing.T) {
	raw := `{"schema_version":"answer_contract/v1","mode":"reasoning","coverage":"complete","content_markdown":"结论。","blocks":[{"type":"topic","title":"主题","text_markdown":"正文没有内联引用。","evidence_refs":["c1"]}]}`

	contract, err := ParseAnswerContract(raw)
	require.NoError(t, err)
	require.Contains(t, contract.Blocks[0].TextMarkdown, `<ref id="c1"/>`)
}

func TestParseAnswerContractNormalizesNumericSchemaVersion(t *testing.T) {
	raw := strings.Replace(answerContractJSON(), `"schema_version":"answer_contract/v1"`, `"schema_version":"1"`, 1)
	contract, err := ParseAnswerContract(raw)
	require.NoError(t, err)
	require.Equal(t, AnswerContractVersion, contract.SchemaVersion)
}

func TestParseAnswerContractAllowsRepeatedCitationHandles(t *testing.T) {
	raw := strings.Replace(answerContractJSON(), `定位见 <ref id="c1"/>`, `定位见 <ref id="c1"/>，结论再次由 <ref id="c1"/> 支持`, 1)
	contract, err := ParseAnswerContract(raw)
	require.NoError(t, err)
	require.Equal(t, []string{"c1"}, contract.Blocks[0].EvidenceRefs)
}

func TestProjectAnswerContractRecomputesCoverageFromValidatedEvidence(t *testing.T) {
	contract, err := ParseAnswerContract(answerContractJSON())
	require.NoError(t, err)
	projection, err := ProjectAnswerContract(contract, answerHandleScope())
	require.NoError(t, err)
	require.Equal(t, AnswerCoverageComplete, projection.Coverage)
	require.Len(t, projection.Evidence, 1)
	require.Equal(t, "video-1", projection.Evidence[0].VideoID)

	contract.Blocks[0].EvidenceRefs = []string{"c1", "c99"}
	contract.ContentMarkdown = `定位见 <ref id="c1"/> <ref id="c99"/>`
	projection, err = ProjectAnswerContract(contract, answerHandleScope())
	require.Equal(t, AnswerErrorEvidenceOutScope, answerErrorCode(err))
	require.Empty(t, projection.Evidence)
}

func TestProjectAnswerContractPreservesBlockAnswerForRendering(t *testing.T) {
	contract, err := ParseAnswerContract(answerContractJSON())
	require.NoError(t, err)

	projection, err := ProjectAnswerContract(contract, answerHandleScope())
	require.NoError(t, err)
	require.Contains(t, projection.ContentMarkdown, "定位见 <ref id=\"c1\"/>")
	require.Contains(t, projection.RenderedMarkdown, "原文")
	require.Contains(t, projection.RenderedMarkdown, "### 定位")
}

func TestRenderAnswerContractPreservesBlockCitations(t *testing.T) {
	raw := `{"schema_version":"answer_contract/v1","mode":"reasoning","coverage":"complete","content_markdown":"结论如下。","blocks":[{"type":"summary","title":"视频总结","text_markdown":"总结原文见 <ref id=\"c1\"/>","evidence_refs":["c1"]}]}`
	contract, err := ParseAnswerContract(raw)
	require.NoError(t, err)
	rendered := RenderAnswerContract(contract)
	require.Contains(t, rendered, "总结原文见 <ref id=\"c1\"/>")
}

func TestProjectAnswerContractRejectsWikiAsVideoEvidence(t *testing.T) {
	contract, err := ParseAnswerContract(answerContractJSON())
	require.NoError(t, err)
	scope := answerHandleScope()
	scope["c1"] = Evidence{EvidenceSentenceID: "wiki-1", SourceType: SourceTypeWiki, Linkable: true}
	projection, err := ProjectAnswerContract(contract, scope)
	require.Equal(t, AnswerErrorEvidenceOutScope, answerErrorCode(err))
	require.Empty(t, projection.Evidence)
}

func answerErrorCode(err error) AnswerErrorCode {
	if typed, ok := err.(*AnswerContractError); ok && typed != nil {
		return typed.Code
	}
	return ""
}
