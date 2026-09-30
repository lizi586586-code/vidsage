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

func TestParseAnswerContractAcceptsLeadingProcessNarration(t *testing.T) {
	for _, preamble := range []string{
		"已收集到足够证据。现在我有三个视频的定位信息，开始作答。",
		"I have all the evidence needed. Let me write the final answer.",
	} {
		contract, err := ParseAnswerContract(preamble + " " + answerContractJSON())
		require.NoError(t, err, "preamble: %s", preamble)
		require.Equal(t, AnswerContractVersion, contract.SchemaVersion)
	}
	// 前导铺垫被剥离后，JSON 后的尾随文本仍然必须拒绝
	_, err := ParseAnswerContract("开场白。" + answerContractJSON() + " 尾巴")
	require.Equal(t, AnswerErrorTrailingContent, answerErrorCode(err))
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

// --- trimLeadingPreamble ---

func TestTrimLeadingPreamble(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "empty", body: "", want: ""},
		{name: "whitespace only", body: "   \n\t  ", want: ""},
		{name: "no preamble", body: `{"k":"v"}`, want: `{"k":"v"}`},
		{name: "chinese preamble", body: "已收集到足够证据。现在开始作答。" + ` {"k":"v"}`, want: `{"k":"v"}`},
		{name: "english preamble", body: "I have all the evidence needed. Let me write. " + `{"k":"v"}`, want: `{"k":"v"}`},
		{name: "preamble with braces in text", body: "说明 {不是JSON} 然后 " + `{"k":"v"}`, want: `{不是JSON} 然后 {"k":"v"}`},
		{name: "no json object at all", body: "纯文本没有JSON", want: "纯文本没有JSON"},
		{name: "leading whitespace before brace", body: "  \n  " + `{"k":"v"}`, want: `{"k":"v"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, trimLeadingPreamble(test.body))
		})
	}
}

// --- jsonErrorWindow ---

func TestJsonErrorWindow(t *testing.T) {
	body := `{"key":"value","num":12345}`
	tests := []struct {
		name   string
		body   string
		offset int
		want   string
	}{
		{name: "negative offset", body: body, offset: -1, want: ""},
		{name: "offset beyond body", body: body, offset: 100, want: ""},
		{name: "offset at start", body: body, offset: 0, want: body},
		{name: "offset at end", body: body, offset: len(body), want: ""},
		{name: "middle offset", body: body, offset: 14, want: body},
		{name: "collapses whitespace", body: "{\n  \"k\":\n  \"v\"\n}", offset: 5, want: `{ "k": "v" }`},
		{name: "short body", body: `{}`, offset: 1, want: `{}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.want, jsonErrorWindow(test.body, test.offset))
		})
	}
}

// --- AnswerContractError.Detail ---

func TestParseAnswerContractInvalidJSONCarriesDetail(t *testing.T) {
	// 非法 JSON 值（不是截断）→ json.SyntaxError → invalid_json + Detail
	raw := `{"schema_version":"answer_contract/v1","mode":"quick","coverage":"complete","content_markdown":"x","blocks":[invalid]}`
	_, err := ParseAnswerContract(raw)
	require.Equal(t, AnswerErrorInvalidJSON, answerErrorCode(err))
	var contractErr *AnswerContractError
	require.ErrorAs(t, err, &contractErr)
	require.NotEmpty(t, contractErr.Detail)
	require.Contains(t, contractErr.Detail, "invalid character")
}

func TestParseAnswerContractUnknownFieldCarriesDetail(t *testing.T) {
	raw := `{"schema_version":"answer_contract/v1","mode":"quick","coverage":"complete","content_markdown":"x","blocks":[],"extra":1}`
	_, err := ParseAnswerContract(raw)
	require.Equal(t, AnswerErrorUnknownField, answerErrorCode(err))
	var contractErr *AnswerContractError
	require.ErrorAs(t, err, &contractErr)
	require.Contains(t, contractErr.Detail, "unknown field")
	require.Contains(t, contractErr.Detail, "extra")
}

func TestParseAnswerContractTruncatedHasNoDetail(t *testing.T) {
	raw := `{"schema_version":"answer_contract/v1","mode":"quick","coverage":"complete","content_markdown":"x","blocks":[`
	_, err := ParseAnswerContract(raw)
	require.Equal(t, AnswerErrorTruncatedOutput, answerErrorCode(err))
	var contractErr *AnswerContractError
	require.ErrorAs(t, err, &contractErr)
	require.Empty(t, contractErr.Detail)
}

func TestParseAnswerContractPreamblePlusFencePlusJSON(t *testing.T) {
	// 前导铺垫 + JSON（无围栏）→ 前导被剥离后合法解析
	raw := "好的，我来回答。" + answerContractJSON()
	contract, err := ParseAnswerContract(raw)
	require.NoError(t, err)
	require.Equal(t, AnswerContractVersion, contract.SchemaVersion)
	require.Equal(t, "quick", string(contract.Mode))
}

func TestParseAnswerContractPreambleWithoutJSONFails(t *testing.T) {
	// 有前导铺垫但无 JSON 对象 → 仍应失败
	_, err := ParseAnswerContract("这是一段纯文本，没有任何 JSON。")
	require.Equal(t, AnswerErrorInvalidJSON, answerErrorCode(err))
}

func TestAnswerContractErrorErrorMethod(t *testing.T) {
	require.Equal(t, "", (&AnswerContractError{}).Error())
	require.Equal(t, "invalid_json", (&AnswerContractError{Code: AnswerErrorInvalidJSON}).Error())
	require.Contains(t, (&AnswerContractError{Code: AnswerErrorInvalidJSON, Detail: "unexpected token"}).Error(), "unexpected token")
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
	require.NoError(t, err)
	require.Len(t, projection.Evidence, 1)
	require.Equal(t, "video-1", projection.Evidence[0].VideoID)
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
	require.NoError(t, err)
	require.Empty(t, projection.Evidence)
}

func answerErrorCode(err error) AnswerErrorCode {
	if typed, ok := err.(*AnswerContractError); ok && typed != nil {
		return typed.Code
	}
	return ""
}
