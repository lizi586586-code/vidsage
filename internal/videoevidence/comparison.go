package videoevidence

import (
	"regexp"
	"strings"
)

var videoLocationTableHeaderRE = regexp.MustCompile(`(?mi)^\s*\|?\s*(?:定位\s*\|\s*核心主旨|时间位置\s*\|\s*内容)\s*\|?\s*$`)

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
// the model already cited in the answer. Handles that cite the same block
// are merged into a single row so the table stays a compact location index:
// the 时间位置 cell keeps the clickable citations (the client renders the
// validated time ranges) and 内容 carries the model-written gist.
func ensureVideoLocationTable(contract AnswerContract, handles map[string]Evidence) AnswerContract {
	if hasVideoLocationTable(RenderAnswerContract(contract)) {
		return contract
	}

	type locationRow struct {
		handles []string
		summary string
	}
	rows := make([]locationRow, 0)
	rowBySummary := make(map[string]int)
	seenEvidence := make(map[string]struct{})
	uniqueLocations := 0
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
			uniqueLocations++
			summary := locationTableSummary(block)
			if index, exists := rowBySummary[summary]; exists {
				rows[index].handles = append(rows[index].handles, handle)
				continue
			}
			rowBySummary[summary] = len(rows)
			rows = append(rows, locationRow{
				handles: []string{handle},
				summary: summary,
			})
		}
	}
	if uniqueLocations <= 3 {
		return contract
	}

	var table strings.Builder
	table.WriteString("| 时间位置 | 内容 |\n| --- | --- |")
	tableHandles := make([]string, 0, uniqueLocations)
	for _, row := range rows {
		table.WriteString("\n| ")
		for index, handle := range row.handles {
			if index > 0 {
				table.WriteString(" ")
			}
			table.WriteString("<ref id=\"")
			table.WriteString(handle)
			table.WriteString("\"/>")
		}
		table.WriteString(" | ")
		table.WriteString(row.summary)
		table.WriteString(" |")
		tableHandles = append(tableHandles, row.handles...)
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
	return ValidateComparisonAnswerForKnowledgeIDs(query, contract, projection, nil)
}

// ValidateComparisonAnswerForKnowledgeIDs applies the normal task-level
// checks and, when expectedIDs is non-empty, requires evidence from every
// server-resolved candidate document. The expected set is backend-owned; it
// is never parsed from user language or trusted from model output.
func ValidateComparisonAnswerForKnowledgeIDs(query string, contract AnswerContract, projection AnswerProjection, expectedIDs []string) error {
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
	if len(expectedIDs) > 0 && contract.Coverage == AnswerCoverageComplete {
		covered := make(map[string]struct{}, len(projection.Evidence))
		for _, evidence := range projection.Evidence {
			if id := strings.TrimSpace(evidence.KnowledgeID); id != "" {
				covered[id] = struct{}{}
			}
		}
		for _, expectedID := range expectedIDs {
			if _, ok := covered[strings.TrimSpace(expectedID)]; !ok {
				return &AnswerContractError{Code: AnswerErrorMissingVideoCoverage}
			}
		}
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
