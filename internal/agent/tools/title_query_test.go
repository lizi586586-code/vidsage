package tools

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractLikelyTitleCandidatePreservesShortVideoTitle(t *testing.T) {
	tests := []struct {
		query string
		want  string
	}{
		{query: "AI提示词是什么？", want: "AI提示词"},
		{query: "AI上下文是什么", want: "AI上下文"},
		{query: "项目复盘总结在哪里", want: "项目复盘"},
		{query: "AI提示词是什么 定义 概念", want: "AI提示词"},
	}
	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			got, ok := ExtractLikelyTitleCandidate(test.query)
			require.True(t, ok)
			require.Equal(t, test.want, got)
		})
	}
}

func TestExtractLikelyTitleCandidateDoesNotRewriteOrdinaryQuestion(t *testing.T) {
	for _, query := range []string{
		"请解释一下提示词为什么重要",
		"如何设计一个好的提示词",
		"这段内容是什么",
	} {
		_, ok := ExtractLikelyTitleCandidate(query)
		require.False(t, ok, query)
	}
}

func TestTitleAwareGrepQueriesAddsLiteralTitleCandidate(t *testing.T) {
	got := titleAwareGrepQueries("AI提示词是什么？")
	require.Len(t, got, 2)
	require.Equal(t, "AI提示词是什么？", got[0])
	require.Equal(t, `AI提示词`, got[1])
}
