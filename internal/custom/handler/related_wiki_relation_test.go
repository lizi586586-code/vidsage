package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLinkedSentenceRequiresOneBoundWikiLink(t *testing.T) {
	sentence, ok := linkedSentence("规模之外，[[concept/data-quality|高质量数据]]决定训练上限。", "concept/data-quality")
	require.True(t, ok)
	require.Contains(t, sentence, "concept/data-quality")
	_, ok = linkedSentence("[[concept/a]]与[[concept/b]]共同影响训练。", "concept/a")
	require.False(t, ok)
	_, ok = linkedSentence("参见[[concept/a]]。", "concept/a")
	require.False(t, ok)
}

func TestDecodeRelatedWikiOutputStrictContract(t *testing.T) {
	evidence := "模型规模之外，高质量数据决定训练能力上限"
	valid := `{"wiki_slug":"concept/data-quality","relation":"补充说明模型规模之外，训练能力仍受到高质量数据的直接约束。","evidence_quote":"高质量数据决定训练能力上限"}`
	output, err := decodeRelatedWikiOutput(valid, "concept/data-quality", evidence)
	require.NoError(t, err)
	require.Equal(t, "concept/data-quality", output.WikiSlug)

	empty, err := decodeRelatedWikiOutput(`{"wiki_slug":"concept/data-quality","relation":"","evidence_quote":""}`, "concept/data-quality", evidence)
	require.NoError(t, err)
	require.Empty(t, empty.Relation)

	for _, raw := range []string{
		valid + ` trailing`,
		`{"wiki_slug":"concept/other","relation":"补充说明模型规模之外，训练能力仍受到高质量数据的直接约束。","evidence_quote":"高质量数据决定训练能力上限"}`,
		`{"wiki_slug":"concept/data-quality","relation":"补充说明模型规模之外，训练能力仍受到高质量数据的直接约束。","evidence_quote":"不存在的证据"}`,
		`{"wiki_slug":"concept/data-quality","relation":"补充说明模型规模之外，训练能力仍受到高质量数据的直接约束。","evidence_quote":"高质量数据决定训练能力上限","extra":true}`,
	} {
		_, err := decodeRelatedWikiOutput(raw, "concept/data-quality", evidence)
		require.Error(t, err)
	}
}
