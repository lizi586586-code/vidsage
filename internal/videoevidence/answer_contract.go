package videoevidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

const AnswerContractVersion = "answer_contract/v1"

type AnswerMode string

const (
	AnswerModeQuick     AnswerMode = "quick"
	AnswerModeReasoning AnswerMode = "reasoning"
)

type AnswerCoverage string

const (
	AnswerCoverageComplete AnswerCoverage = "complete"
	AnswerCoveragePartial  AnswerCoverage = "partial"
	AnswerCoverageNone     AnswerCoverage = "none"
)

type AnswerBlock struct {
	Type         string   `json:"type"`
	Title        string   `json:"title"`
	TextMarkdown string   `json:"text_markdown"`
	EvidenceRefs []string `json:"evidence_refs"`
}

type AnswerContract struct {
	SchemaVersion   string         `json:"schema_version"`
	Mode            AnswerMode     `json:"mode"`
	Coverage        AnswerCoverage `json:"coverage"`
	ContentMarkdown string         `json:"content_markdown"`
	Blocks          []AnswerBlock  `json:"blocks"`
}

// AnswerProjection is the server-owned representation used by stream and
// persistence layers. Evidence is resolved from the request-local whitelist;
// no model supplied title, time, or video identity is carried forward.
type AnswerProjection struct {
	ContentMarkdown  string
	RenderedMarkdown string
	Mode             AnswerMode
	Coverage         AnswerCoverage
	Blocks           []AnswerBlock
	Evidence         []Evidence
}

type AnswerContractError struct {
	Code AnswerErrorCode
}

type AnswerErrorCode string

const (
	AnswerErrorInvalidJSON      AnswerErrorCode = "invalid_json"
	AnswerErrorUnknownField     AnswerErrorCode = "unknown_field"
	AnswerErrorTrailingContent  AnswerErrorCode = "trailing_content"
	AnswerErrorTruncatedOutput  AnswerErrorCode = "truncated_output"
	AnswerErrorInvalidContract  AnswerErrorCode = "invalid_contract"
	AnswerErrorEvidenceOutScope AnswerErrorCode = "evidence_out_of_scope"
)

func (e *AnswerContractError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code)
}

var answerHandlePattern = regexp.MustCompile(`^c[1-9][0-9]*$`)
var answerCitationPattern = regexp.MustCompile(`<ref id="(c[1-9][0-9]*)"\s*/>`)

// unwrapJSONCodeFence removes one transport-only Markdown fence when it wraps
// the entire response. The JSON contract remains strict after unwrapping:
// unknown fields, truncation, and trailing content are still rejected.
func unwrapJSONCodeFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[len(lines)-1]) != "```" {
		return trimmed
	}
	language := strings.TrimSpace(strings.TrimPrefix(lines[0], "```"))
	if language != "" && !strings.EqualFold(language, "json") {
		return trimmed
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}

// ParseAnswerContract strictly decodes one answer_contract/v1 object. A
// single Markdown JSON fence is tolerated as a transport wrapper, but all
// contract and trailing-content checks remain strict.
func ParseAnswerContract(raw string) (AnswerContract, error) {
	trimmed := unwrapJSONCodeFence(raw)
	if trimmed == "" {
		return AnswerContract{}, &AnswerContractError{Code: AnswerErrorInvalidJSON}
	}

	decoder := json.NewDecoder(bytes.NewReader([]byte(trimmed)))
	decoder.DisallowUnknownFields()
	var wire struct {
		SchemaVersion   string          `json:"schema_version"`
		Mode            AnswerMode      `json:"mode"`
		Coverage        AnswerCoverage  `json:"coverage"`
		ContentMarkdown string          `json:"content_markdown"`
		Blocks          json.RawMessage `json:"blocks"`
	}
	if err := decoder.Decode(&wire); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return AnswerContract{}, &AnswerContractError{Code: AnswerErrorTruncatedOutput}
		}
		if strings.Contains(err.Error(), "unknown field") {
			return AnswerContract{}, &AnswerContractError{Code: AnswerErrorUnknownField}
		}
		return AnswerContract{}, &AnswerContractError{Code: AnswerErrorInvalidJSON}
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return AnswerContract{}, &AnswerContractError{Code: AnswerErrorTrailingContent}
	}
	if len(wire.Blocks) == 0 || bytes.Equal(bytes.TrimSpace(wire.Blocks), []byte("null")) {
		return AnswerContract{}, &AnswerContractError{Code: AnswerErrorInvalidContract}
	}

	blocks, err := decodeAnswerBlocks(wire.Blocks)
	if err != nil {
		return AnswerContract{}, err
	}
	contract := AnswerContract{
		SchemaVersion:   normalizeAnswerSchemaVersion(wire.SchemaVersion),
		Mode:            wire.Mode,
		Coverage:        wire.Coverage,
		ContentMarkdown: wire.ContentMarkdown,
		Blocks:          blocks,
	}
	normalizeDeclaredBlockCitations(&contract)
	if err := validateAnswerContractShape(contract); err != nil {
		return AnswerContract{}, err
	}
	return contract, nil
}

// Some compatible models emit the protocol's short numeric version even
// after receiving the canonical contract name. Normalize only that known
// alias; all other versions remain rejected by validateAnswerContractShape.
func normalizeAnswerSchemaVersion(version string) string {
	if strings.TrimSpace(version) == "1" {
		return AnswerContractVersion
	}
	return version
}

func decodeAnswerBlocks(raw json.RawMessage) ([]AnswerBlock, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var blocks []AnswerBlock
	if err := decoder.Decode(&blocks); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return nil, &AnswerContractError{Code: AnswerErrorUnknownField}
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, &AnswerContractError{Code: AnswerErrorTruncatedOutput}
		}
		return nil, &AnswerContractError{Code: AnswerErrorInvalidContract}
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, &AnswerContractError{Code: AnswerErrorInvalidContract}
	}
	if blocks == nil {
		return nil, &AnswerContractError{Code: AnswerErrorInvalidContract}
	}
	return blocks, nil
}

// normalizeDeclaredBlockCitations repairs a common model formatting error:
// the model declares evidence_refs correctly but omits the matching inline
// tags from the block text. Only handles already declared by that same block
// are added; scope validation still happens later in ProjectAnswerContract.
func normalizeDeclaredBlockCitations(contract *AnswerContract) {
	if contract == nil {
		return
	}
	for index := range contract.Blocks {
		block := &contract.Blocks[index]
		if len(block.EvidenceRefs) == 0 {
			continue
		}
		present := make(map[string]struct{})
		for _, match := range answerCitationPattern.FindAllStringSubmatch(block.TextMarkdown, -1) {
			present[match[1]] = struct{}{}
		}
		text := strings.TrimRight(block.TextMarkdown, " \t\r\n")
		for _, rawHandle := range block.EvidenceRefs {
			handle := strings.TrimSpace(rawHandle)
			if handle == "" {
				continue
			}
			if _, ok := present[handle]; ok {
				continue
			}
			if text != "" {
				text += " "
			}
			text += `<ref id="` + handle + `"/>`
			present[handle] = struct{}{}
		}
		block.TextMarkdown = text
	}
}

func validateAnswerContractShape(contract AnswerContract) error {
	if contract.SchemaVersion != AnswerContractVersion {
		return &AnswerContractError{Code: AnswerErrorInvalidContract}
	}
	if contract.Mode != AnswerModeQuick && contract.Mode != AnswerModeReasoning {
		return &AnswerContractError{Code: AnswerErrorInvalidContract}
	}
	if contract.Coverage != AnswerCoverageComplete && contract.Coverage != AnswerCoveragePartial && contract.Coverage != AnswerCoverageNone {
		return &AnswerContractError{Code: AnswerErrorInvalidContract}
	}
	for _, block := range contract.Blocks {
		switch block.Type {
		case "summary", "topic", "comparison", "steps", "evidence":
		default:
			return &AnswerContractError{Code: AnswerErrorInvalidContract}
		}
		for _, handle := range block.EvidenceRefs {
			if !answerHandlePattern.MatchString(strings.TrimSpace(handle)) {
				return &AnswerContractError{Code: AnswerErrorInvalidContract}
			}
		}
	}
	var renderedText strings.Builder
	renderedText.WriteString(contract.ContentMarkdown)
	for _, block := range contract.Blocks {
		renderedText.WriteString("\n")
		renderedText.WriteString(block.TextMarkdown)
	}
	rendered := renderedText.String()
	contentHandles := answerCitationPattern.FindAllStringSubmatch(rendered, -1)
	if strings.Contains(rendered, "<ref") && len(contentHandles) == 0 {
		return &AnswerContractError{Code: AnswerErrorInvalidContract}
	}
	contentSet := make(map[string]struct{}, len(contentHandles))
	for _, match := range contentHandles {
		contentSet[match[1]] = struct{}{}
	}
	blockSet := make(map[string]struct{})
	for _, block := range contract.Blocks {
		for _, handle := range block.EvidenceRefs {
			blockSet[strings.TrimSpace(handle)] = struct{}{}
		}
	}
	if len(contentSet) != len(blockSet) {
		return &AnswerContractError{Code: AnswerErrorInvalidContract}
	}
	for handle := range contentSet {
		if _, ok := blockSet[handle]; !ok {
			return &AnswerContractError{Code: AnswerErrorInvalidContract}
		}
	}
	for handle := range blockSet {
		if _, ok := contentSet[handle]; !ok {
			return &AnswerContractError{Code: AnswerErrorInvalidContract}
		}
	}
	return nil
}

// ProjectAnswerContract resolves every evidence handle against the supplied
// request scope and recomputes coverage. Invalid handles fail closed instead
// of allowing the model's coverage field to claim a complete answer.
func ProjectAnswerContract(contract AnswerContract, handles map[string]Evidence) (AnswerProjection, error) {
	return ProjectAnswerContractWithOrdinaryHandles(contract, handles, nil)
}

// ProjectAnswerContractWithOrdinaryHandles validates the strict answer
// contract against two request-local namespaces:
//   - handles: transcript evidence that may become clickable video evidence;
//   - ordinaryHandles: regular knowledge/Wiki references that may remain
//     citations, but never contribute video evidence or time ranges.
//
// Keeping these namespaces separate prevents a Wiki citation from being
// mistaken for video evidence while still allowing a mixed answer to cite the
// Wiki facts used for semantic explanation.
func ProjectAnswerContractWithOrdinaryHandles(
	contract AnswerContract,
	handles map[string]Evidence,
	ordinaryHandles map[string]struct{},
) (AnswerProjection, error) {
	if err := validateAnswerContractShape(contract); err != nil {
		return AnswerProjection{}, err
	}
	projection := AnswerProjection{
		ContentMarkdown:  contract.ContentMarkdown,
		RenderedMarkdown: RenderAnswerContract(contract),
		Mode:             contract.Mode,
		Blocks:           contract.Blocks,
		Coverage:         AnswerCoverageNone,
		Evidence:         make([]Evidence, 0),
	}
	seen := make(map[string]struct{})
	invalid := false
	for _, block := range contract.Blocks {
		for _, rawHandle := range block.EvidenceRefs {
			handle := strings.TrimSpace(rawHandle)
			evidence, videoOK := handles[handle]
			if videoOK {
				if !evidence.Linkable || evidence.SourceType == SourceTypeWiki {
					invalid = true
					continue
				}
			} else if _, ordinaryOK := ordinaryHandles[handle]; !ordinaryOK {
				invalid = true
				continue
			} else {
				continue
			}
			if _, exists := seen[evidence.EvidenceSentenceID]; exists {
				continue
			}
			seen[evidence.EvidenceSentenceID] = struct{}{}
			projection.Evidence = append(projection.Evidence, evidence)
		}
	}
	if invalid {
		return AnswerProjection{}, &AnswerContractError{Code: AnswerErrorEvidenceOutScope}
	}
	switch {
	case len(projection.Evidence) == 0:
		projection.Coverage = AnswerCoverageNone
	default:
		projection.Coverage = AnswerCoverageComplete
	}
	return projection, nil
}

// RenderAnswerContract creates the user-facing Markdown from the validated
// contract. The model's blocks are part of the answer, not validation-only
// metadata; omitting them would reduce a complete answer to its short preamble.
func RenderAnswerContract(contract AnswerContract) string {
	parts := make([]string, 0, 1+len(contract.Blocks))
	if content := strings.TrimSpace(contract.ContentMarkdown); content != "" {
		parts = append(parts, content)
	}
	for _, block := range contract.Blocks {
		text := strings.TrimSpace(block.TextMarkdown)
		if title := strings.TrimSpace(block.Title); title != "" && text != "" {
			text = "### " + title + "\n\n" + text
		} else if title != "" {
			text = "### " + title
		}
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}
