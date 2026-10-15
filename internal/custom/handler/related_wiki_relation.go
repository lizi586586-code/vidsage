package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode/utf8"

	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/custom/client/weknora"
	"github.com/Tencent/WeKnora/internal/custom/model"
)

const relatedWikiUnavailable = "暂无足够内容生成可靠的关联说明。"

// RelatedWikiLLM is intentionally narrow so handlers can be tested without a
// concrete model client. The output is validated before it is published.
type RelatedWikiLLM interface {
	CompleteJSON(context.Context, string) (string, error)
}

type relatedWikiOutput struct {
	WikiSlug      string `json:"wiki_slug"`
	Relation      string `json:"relation"`
	EvidenceQuote string `json:"evidence_quote"`
}

func decodeRelatedWikiOutput(raw, targetSlug, evidenceSentence string) (relatedWikiOutput, error) {
	var output relatedWikiOutput
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&output); err != nil {
		return output, fmt.Errorf("invalid_json: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return output, fmt.Errorf("contract_invalid: trailing content")
	}
	output.WikiSlug = strings.TrimSpace(output.WikiSlug)
	output.Relation = strings.TrimSpace(output.Relation)
	output.EvidenceQuote = strings.TrimSpace(output.EvidenceQuote)
	if output.WikiSlug != targetSlug {
		return output, fmt.Errorf("reference_out_of_scope")
	}
	if output.Relation == "" && output.EvidenceQuote == "" {
		return output, nil
	}
	if output.Relation == "" || output.EvidenceQuote == "" {
		return output, fmt.Errorf("contract_invalid: incomplete empty result")
	}
	length := utf8.RuneCountInString(output.Relation)
	if length < 15 || length > 100 || strings.ContainsAny(output.Relation, "\r\n`") {
		return output, fmt.Errorf("contract_invalid: relation length or format")
	}
	if !strings.Contains(evidenceSentence, output.EvidenceQuote) {
		return output, fmt.Errorf("reference_out_of_scope: evidence quote")
	}
	return output, nil
}

var wikiLinkPattern = regexp.MustCompile(`\[\[([^\]|]+)(?:\|[^\]]+)?\]\]`)

func linkedSentence(content, targetSlug string) (string, bool) {
	parts := strings.FieldsFunc(content, func(r rune) bool {
		return r == '。' || r == '！' || r == '？' || r == '\n' || r == '\r'
	})
	for _, part := range parts {
		part = strings.TrimSpace(part)
		matches := wikiLinkPattern.FindAllStringSubmatch(part, -1)
		if len(matches) != 1 || strings.TrimSpace(matches[0][1]) != targetSlug {
			continue
		}
		plain := strings.TrimSpace(wikiLinkPattern.ReplaceAllString(part, "$1"))
		if plain == targetSlug || utf8.RuneCountInString(plain) < utf8.RuneCountInString(targetSlug)+8 {
			return "", false
		}
		return part, true
	}
	return "", false
}

func relatedWikiPrompt(summaryTitle, targetTitle, targetSlug, summaryContent, targetContent string) string {
	if utf8.RuneCountInString(summaryContent) > 6000 {
		runes := []rune(summaryContent)
		summaryContent = string(runes[:6000])
	}
	if utf8.RuneCountInString(targetContent) > 3000 {
		runes := []rune(targetContent)
		targetContent = string(runes[:3000])
	}
	return fmt.Sprintf(`你只负责概括已给证据，不得补充、推测来源或因果。
输出严格单个 JSON 对象，且只能包含 wiki_slug、relation、evidence_quote 三个字符串字段；禁止 Markdown、代码围栏和尾随文本。
relation 必须为 15-100 个中文字符，只描述视频摘要与目标 Wiki 正文共同支持的关系。
evidence_quote 必须逐字复制视频摘要中的连续文本；没有可靠依据时 relation 返回空字符串，evidence_quote 返回空字符串。

视频摘要标题：%s
目标 Wiki：%s
wiki_slug：%s
视频摘要正文：%s
目标 Wiki 正文：%s`, summaryTitle, targetTitle, targetSlug, summaryContent, targetContent)
}

func directNeighborSlugs(center string, graph *weknora.WikiGraphData) []string {
	seen := map[string]bool{}
	for _, edge := range graph.Edges {
		if edge.Source == center && edge.Target != center {
			seen[edge.Target] = true
		}
		if edge.Target == center && edge.Source != center {
			seen[edge.Source] = true
		}
	}
	result := make([]string, 0, len(seen))
	for _, node := range graph.Nodes {
		if seen[node.Slug] {
			result = append(result, node.Slug)
		}
	}
	return result
}

func (h *ContentHandler) nativeWikiRelations(ctx context.Context, videoID string, summary *weknora.WikiPage, graph *weknora.WikiGraphData) map[string]string {
	result := map[string]string{}
	if summary == nil || graph == nil {
		return result
	}
	for _, slug := range directNeighborSlugs(summary.Slug, graph) {
		result[slug] = relatedWikiUnavailable
		target, err := h.Wiki.GetPage(ctx, h.KBID, slug)
		if err != nil || target == nil {
			continue
		}
		var cached model.VideoWikiRelation
		cacheErr := h.DB.WithContext(ctx).First(&cached, "video_id = ? AND summary_slug = ? AND target_slug = ? AND summary_version = ? AND target_version = ?", videoID, summary.Slug, slug, summary.Version, target.Version).Error
		if cacheErr == nil {
			result[slug] = cached.Relation
			continue
		}
		if h.LLM == nil {
			continue
		}
		prompt := relatedWikiPrompt(summary.Title, target.Title, slug, summary.Content, target.Content)
		var output relatedWikiOutput
		for attempt := 0; attempt < 2; attempt++ {
			raw, callErr := h.LLM.CompleteJSON(ctx, prompt)
			if callErr != nil {
				break
			}
			output, err = decodeRelatedWikiOutput(raw, slug, summary.Content)
			if err == nil {
				break
			}
			prompt += "\n上次输出未通过协议校验。仅按上述协议重新输出一个完整 JSON 对象。"
		}
		if err != nil || output.Relation == "" {
			continue
		}
		record := model.VideoWikiRelation{VideoID: videoID, SummarySlug: summary.Slug, TargetSlug: slug, SummaryVersion: summary.Version, TargetVersion: target.Version, Relation: output.Relation, EvidenceQuote: output.EvidenceQuote}
		if err := h.DB.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "video_id"}, {Name: "summary_slug"}, {Name: "target_slug"}}, DoUpdates: clause.AssignmentColumns([]string{"summary_version", "target_version", "relation", "evidence_quote", "updated_at"})}).Create(&record).Error; err == nil {
			result[slug] = output.Relation
		}
	}
	return result
}
