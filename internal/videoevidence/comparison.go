package videoevidence

import (
	"regexp"
	"strings"
)

var videoLocationTableHeaderRE = regexp.MustCompile(`(?mi)^\s*\|?\s*定位\s*\|\s*核心主旨\s*\|?\s*$`)

func hasVideoLocationTable(markdown string) bool {
	if !videoLocationTableHeaderRE.MatchString(markdown) {
		return false
	}
	lines := strings.Split(markdown, "\n")
	for index, line := range lines {
		if !videoLocationTableHeaderRE.MatchString(line) || index+1 >= len(lines) {
			continue
		}
		separator := strings.TrimSpace(lines[index+1])
		if strings.Contains(separator, "---") && strings.Contains(separator, "|") {
			return true
		}
	}
	return false
}

// ensureVideoLocationTable performs a presentation-only repair after the
// answer contract has passed shape validation. It never creates evidence:
// every row is derived from a request-local, linkable transcript handle that
// the model already cited in the answer.
func ensureVideoLocationTable(contract AnswerContract, handles map[string]Evidence) AnswerContract {
	if hasVideoLocationTable(RenderAnswerContract(contract)) {
		return contract
	}

	type locationRow struct {
		handle  string
		summary string
	}
	rows := make([]locationRow, 0)
	seenEvidence := make(map[string]struct{})
	for _, block := range contract.Blocks {
		for _, rawHandle := range block.EvidenceRefs {
			handle := strings.TrimSpace(rawHandle)
			evidence, ok := handles[handle]
			if !ok || !evidence.Linkable || evidence.SourceType == SourceTypeWiki {
				continue
			}
			identity := strings.TrimSpace(evidence.EvidenceSentenceID)
			if identity == "" {
				identity = handle
			}
			if _, exists := seenEvidence[identity]; exists {
				continue
			}
			seenEvidence[identity] = struct{}{}
			rows = append(rows, locationRow{
				handle:  handle,
				summary: locationTableSummary(block),
			})
		}
	}
	if len(rows) <= 3 {
		return contract
	}

	var table strings.Builder
	table.WriteString("| 定位 | 核心主旨 |\n| --- | --- |")
	tableHandles := make([]string, 0, len(rows))
	for _, row := range rows {
		table.WriteString("\n| 该处内容见 <ref id=\"")
		table.WriteString(row.handle)
		table.WriteString("\"/>。 | ")
		table.WriteString(row.summary)
		table.WriteString(" |")
		tableHandles = append(tableHandles, row.handle)
	}
	contract.Blocks = append(contract.Blocks, AnswerBlock{
		Type:         "evidence",
		Title:        "视频定位",
		TextMarkdown: table.String(),
		EvidenceRefs: tableHandles,
	})
	return contract
}

func locationTableSummary(block AnswerBlock) string {
	title := strings.TrimSpace(block.Title)
	text := answerCitationPattern.ReplaceAllString(block.TextMarkdown, "")
	text = strings.Join(strings.Fields(text), " ")
	text = strings.Trim(text, "#*- ")
	summary := text
	if title != "" && text != "" {
		summary = title + "：" + text
	} else if title != "" {
		summary = title
	}
	if summary == "" {
		summary = "已验证的视频证据"
	}
	summary = strings.ReplaceAll(summary, "|", `\|`)
	runes := []rune(summary)
	if len(runes) > 120 {
		summary = string(runes[:120]) + "..."
	}
	return summary
}

// ValidateComparisonAnswer enforces multi-video coverage from the task
// semantics and validated evidence. It deliberately does not parse titles:
// titles are user language, not an access-control or retrieval boundary.
func ValidateComparisonAnswer(query string, contract AnswerContract, projection AnswerProjection) error {
	if len(projection.Evidence) > 3 && !hasVideoLocationTable(projection.RenderedMarkdown) {
		return &AnswerContractError{Code: AnswerErrorMissingVideoTable}
	}

	if !requiresMultiVideoCoverage(query) {
		return nil
	}

	if strings.Contains(query, "比较") || strings.Contains(query, "对比") || strings.Contains(query, "共同点") || strings.Contains(query, "差异") {
		comparisonBlocks := 0
		for _, block := range contract.Blocks {
			if block.Type == "comparison" {
				comparisonBlocks++
			}
		}
		if comparisonBlocks < 2 {
			return &AnswerContractError{Code: AnswerErrorInvalidContract}
		}
	}

	videos := make(map[string]struct{})
	for _, evidence := range projection.Evidence {
		if id := strings.TrimSpace(evidence.VideoID); id != "" {
			videos[id] = struct{}{}
		}
	}
	if len(videos) < 2 {
		return &AnswerContractError{Code: AnswerErrorMissingVideoCoverage}
	}
	return nil
}

func requiresMultiVideoCoverage(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	for _, marker := range []string{"分别", "两个视频", "这两个视频", "多个视频", "各自", "共同点", "差异", "比较", "对比", "二者"} {
		if strings.Contains(query, marker) {
			return true
		}
	}
	return false
}
