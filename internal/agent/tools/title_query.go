package tools

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var likelyTitleSuffix = regexp.MustCompile(
	`(?i)[\s\p{P}\p{S}]*(?:是什么|是什麼|在哪(?:里|裡)?|哪里|哪一部分|哪部分|讲了什么|講了什麼|提到什么|提到什麼|总结|總結|概述|内容|內容|定义|定義|概念|原理|介绍|介紹)[\s\p{P}\p{S}]*$`,
)

// ExtractLikelyTitleCandidate keeps a short phrase before a common title
// question suffix. It is intentionally conservative: semantic questions
// without one of these suffixes remain untouched.
func ExtractLikelyTitleCandidate(query string) (string, bool) {
	query = strings.TrimSpace(query)
	if query == "" || strings.ContainsAny(query, "|[](){}^$*+") {
		return "", false
	}
	candidate := query
	for i := 0; i < 4; i++ {
		next := strings.TrimSpace(likelyTitleSuffix.ReplaceAllString(candidate, ""))
		if next == candidate {
			break
		}
		candidate = next
	}
	candidate = strings.Trim(candidate, " \t\r\n:：-—？?，,。.!！")
	if candidate == "" || candidate == query {
		return "", false
	}
	switch candidate {
	case "这段", "這段", "这段内容", "這段內容", "这个内容", "這個內容", "内容", "內容", "这个", "這個":
		return "", false
	}
	if utf8.RuneCountInString(candidate) < 2 || utf8.RuneCountInString(candidate) > 80 {
		return "", false
	}
	// A full sentence is not a title candidate. This guard prevents the
	// fallback from turning ordinary explanatory questions into title searches.
	if strings.ContainsAny(candidate, "请帮请问如何为什么為什麼怎么怎麼") {
		return "", false
	}
	return candidate, true
}

func titleAwareGrepQueries(query string) []string {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	queries := []string{query}
	if candidate, ok := ExtractLikelyTitleCandidate(query); ok {
		quoted := regexp.QuoteMeta(candidate)
		if quoted != query {
			queries = append(queries, quoted)
		}
	}
	return queries
}
