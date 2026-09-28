package videoevidence

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	metadataEvidenceID = "evidence_sentence_id"
	metadataChunkID    = "source_chunk_id"
	metadataVideoID    = "video_id"
	metadataVideoTitle = "video_title"
	metadataStartMs    = "start_ms"
	metadataEndMs      = "end_ms"
	metadataGeneration = "transcript_generation"
	metadataSourceType = "source_type"
	metadataLinkable   = "linkable"
	metadataContract   = "video_evidence_contract"
)

// NormalizeCandidate resolves a candidate only through the supplied scope.
// Candidate-provided video identity, time, and generation are intentionally
// ignored after the whitelist lookup.
func NormalizeCandidate(candidate Candidate, scope Scope) (Evidence, error) {
	evidenceID := strings.TrimSpace(candidate.EvidenceSentenceID)
	if evidenceID == "" {
		if strings.EqualFold(strings.TrimSpace(candidate.SourceType), SourceTypeWiki) {
			return Evidence{}, &ValidationError{Code: ErrorWikiOnlySource, Err: errors.New("wiki source has no transcript evidence")}
		}
		return Evidence{}, &ValidationError{Code: ErrorInvalidHandle, Err: errors.New("evidence sentence ID is required")}
	}

	evidence, ok := scope.AllowedEvidenceIDs[evidenceID]
	if !ok {
		if chunkID := strings.TrimSpace(candidate.ChunkID); chunkID != "" {
			evidence, ok = scope.AllowedChunkIDs[chunkID]
		}
	}
	if !ok {
		if knowledgeID := strings.TrimSpace(candidate.KnowledgeID); knowledgeID != "" {
			evidence, ok = scope.AllowedKnowledgeIDs[knowledgeID]
		}
	}
	if !ok || strings.TrimSpace(evidence.EvidenceSentenceID) != evidenceID {
		return Evidence{}, &ValidationError{Code: ErrorEvidenceOutOfScope, Err: fmt.Errorf("evidence %q is not in the request scope", evidenceID)}
	}
	if strings.EqualFold(strings.TrimSpace(evidence.SourceType), SourceTypeWiki) {
		return Evidence{}, &ValidationError{Code: ErrorWikiOnlySource, Err: errors.New("wiki source cannot become video evidence")}
	}
	if strings.TrimSpace(evidence.VideoID) == "" {
		return Evidence{}, &ValidationError{Code: ErrorMissingVideo, Err: errors.New("video identity is missing")}
	}
	if evidence.StartMs < 0 || evidence.EndMs <= evidence.StartMs {
		return Evidence{}, &ValidationError{Code: ErrorMissingTimeRange, Err: errors.New("video time range is missing or invalid")}
	}
	if expected := strings.TrimSpace(scope.CurrentGeneration[evidence.VideoID]); expected != "" &&
		expected != strings.TrimSpace(evidence.TranscriptGeneration) {
		return Evidence{}, &ValidationError{Code: ErrorGenerationMismatch, Err: errors.New("transcript generation is stale")}
	}
	evidence.Linkable = true
	return evidence, nil
}

// CandidateFromSearchResult reads only backend-controlled structured metadata.
// It never searches arbitrary snippets or answer text for an evidence ID.
func CandidateFromSearchResult(result *types.SearchResult) (Candidate, bool) {
	if result == nil || result.Metadata == nil {
		return Candidate{}, false
	}
	if isNonTimelineChunkType(result.ChunkType) {
		return Candidate{}, false
	}
	metadata := result.Metadata
	sourceType := strings.TrimSpace(metadata[metadataSourceType])
	if sourceType == "" || strings.EqualFold(sourceType, SourceTypeWiki) || strings.EqualFold(sourceType, "summary") {
		return Candidate{}, false
	}
	evidenceID := strings.TrimSpace(metadata[metadataEvidenceID])
	if evidenceID == "" {
		return Candidate{}, false
	}
	candidate := Candidate{
		EvidenceSentenceID:   evidenceID,
		ChunkID:              firstNonEmpty(metadata[metadataChunkID], result.ID),
		KnowledgeID:          result.KnowledgeID,
		VideoID:              metadata[metadataVideoID],
		VideoTitle:           firstNonEmpty(metadata[metadataVideoTitle], result.KnowledgeTitle),
		TranscriptGeneration: metadata[metadataGeneration],
		SourceType:           sourceType,
	}
	if value, ok := parseIntMetadata(metadata[metadataStartMs]); ok {
		candidate.StartMs = &value
	}
	if value, ok := parseIntMetadata(metadata[metadataEndMs]); ok {
		candidate.EndMs = &value
	}
	return candidate, true
}

func transcriptMetadataFromResult(result *types.SearchResult) (transcriptMetadata, bool) {
	if result == nil {
		return transcriptMetadata{}, false
	}
	// New transcript rows may carry the locator in ChunkMetadata. Keep this
	// path ahead of text parsing so structured backend metadata is authoritative.
	if len(result.ChunkMetadata) > 0 {
		var parsed transcriptMetadata
		if err := json.Unmarshal(result.ChunkMetadata, &parsed); err == nil && validTranscriptMetadata(parsed) {
			return parsed, true
		}
	}
	// Older VidSage transcript knowledge records stored the complete locator
	// document in the backend-owned knowledge metadata `content` field while
	// the indexed chunk body could be split mid-JSON.
	if result.Metadata != nil {
		if raw := strings.TrimSpace(result.Metadata["content"]); raw != "" {
			if parsed, ok := parseTranscriptMetadata(raw); ok {
				return parsed, true
			}
		}
	}
	return parseTranscriptMetadata(result.Content)
}

func validTranscriptMetadata(parsed transcriptMetadata) bool {
	return parsed.EvidenceSentenceID != "" && parsed.VideoID != "" &&
		parsed.TranscriptGeneration != "" && parsed.StartMs >= 0 && parsed.EndMs > parsed.StartMs
}

// CandidateFromMap is used for structured tool result rows. The row must
// explicitly carry metadata; match_snippet/content are never inspected.
func CandidateFromMap(row map[string]interface{}) (Candidate, bool) {
	if row == nil {
		return Candidate{}, false
	}
	metadata := stringMap(row["metadata"])
	result := &types.SearchResult{
		ID:             stringValue(row, "id", "chunk_id"),
		KnowledgeID:    stringValue(row, "knowledge_id"),
		KnowledgeTitle: stringValue(row, "knowledge_title", "title"),
		ChunkType:      stringValue(row, "chunk_type", "type"),
		Metadata:       metadata,
	}
	if raw := row["chunk_metadata"]; raw != nil {
		if encoded, err := json.Marshal(raw); err == nil {
			result.ChunkMetadata = types.JSON(encoded)
		}
	}
	if result.ID == "" {
		result.ID = stringValue(row, "faq_id")
	}
	return CandidateFromSearchResult(result)
}

// EnrichSearchResult derives structured evidence metadata from the canonical
// transcript document. It is a backend adapter, not model-output parsing.
func EnrichSearchResult(result *types.SearchResult) *types.SearchResult {
	if result == nil {
		return nil
	}
	if isNonTimelineChunkType(result.ChunkType) {
		stripVideoMetadata(result)
		return result
	}
	if result.Metadata == nil {
		result.Metadata = make(map[string]string)
	}
	if _, ok := CandidateFromSearchResult(result); !ok {
		if parsed, ok := transcriptMetadataFromResult(result); ok {
			result.Metadata[metadataEvidenceID] = parsed.EvidenceSentenceID
			result.Metadata[metadataVideoID] = parsed.VideoID
			result.Metadata[metadataVideoTitle] = firstNonEmpty(parsed.VideoTitle, result.KnowledgeTitle)
			result.Metadata[metadataStartMs] = strconv.Itoa(parsed.StartMs)
			result.Metadata[metadataEndMs] = strconv.Itoa(parsed.EndMs)
			result.Metadata[metadataGeneration] = parsed.TranscriptGeneration
			result.Metadata[metadataSourceType] = SourceTypeTranscript
			result.Metadata[metadataContract] = Version
			if result.ID != "" {
				result.Metadata[metadataChunkID] = result.ID
			}
		}
	}
	return result
}

func isNonTimelineChunkType(chunkType string) bool {
	switch strings.ToLower(strings.TrimSpace(chunkType)) {
	case "summary", "wiki", "wiki_page", "entity", "relationship", "table_summary":
		return true
	default:
		return false
	}
}

// NormalizeReferences validates all video metadata against scope while
// preserving ordinary knowledge references. Invalid video metadata is removed
// from the renderable reference but never converted into an invented locator.
func NormalizeReferences(scope Scope, refs []*types.SearchResult) []*types.SearchResult {
	normalized := make([]*types.SearchResult, 0, len(refs))
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		ref = EnrichSearchResult(ref)
		candidate, ok := CandidateFromSearchResult(ref)
		if !ok {
			normalized = append(normalized, ref)
			continue
		}
		evidence, err := NormalizeCandidate(candidate, scope)
		if err != nil {
			stripVideoMetadata(ref)
			normalized = append(normalized, ref)
			continue
		}
		applyEvidenceMetadata(ref, evidence)
		normalized = append(normalized, ref)
	}
	return normalized
}

// ProjectReferences converts backend-owned retrieval references into the
// public evidence projection and computes coverage from validated candidates.
// Quick answers and Agent streams share this path so they cannot drift in
// their handling of Wiki-only, stale, or malformed references.
func ProjectReferences(refs []*types.SearchResult) ([]Evidence, string) {
	scope := ScopeFromReferences(refs)
	projected := make([]Evidence, 0)
	totalVideo := 0
	for _, ref := range refs {
		candidate, ok := CandidateFromSearchResult(ref)
		if !ok || candidate.SourceType != SourceTypeTranscript {
			continue
		}
		totalVideo++
		evidence, err := NormalizeCandidate(candidate, scope)
		if err != nil || !evidence.Linkable {
			continue
		}
		projected = append(projected, evidence)
	}
	coverage := "none"
	switch {
	case len(projected) > 0 && len(projected) == totalVideo:
		coverage = "complete"
	case len(projected) > 0:
		coverage = "partial"
	}
	return projected, coverage
}

// ScopeFromReferences creates a whitelist from backend-produced references.
// It is used at request boundaries before model-generated citations are
// accepted. It does not make the references linkable by itself.
func ScopeFromReferences(refs []*types.SearchResult) Scope {
	scope := NewScope()
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		ref = EnrichSearchResult(ref)
		candidate, ok := CandidateFromSearchResult(ref)
		if !ok {
			continue
		}
		evidence, ok := EvidenceFromCandidate(candidate)
		if ok {
			scope.Add(evidence)
		}
	}
	return scope
}

// MergeScopes combines request-local whitelists without allowing one source
// to overwrite an already registered canonical mapping.
func MergeScopes(dst *Scope, src Scope) {
	if dst == nil {
		return
	}
	if dst.AllowedEvidenceIDs == nil {
		*dst = NewScope()
	}
	for _, evidence := range src.AllowedEvidenceIDs {
		if _, exists := dst.AllowedEvidenceIDs[evidence.EvidenceSentenceID]; !exists {
			dst.Add(evidence)
		}
	}
	for videoID, generation := range src.CurrentGeneration {
		if dst.CurrentGeneration[videoID] == "" {
			dst.CurrentGeneration[videoID] = generation
		}
	}
}

func EvidenceFromCandidate(candidate Candidate) (Evidence, bool) {
	if strings.TrimSpace(candidate.EvidenceSentenceID) == "" ||
		strings.TrimSpace(candidate.VideoID) == "" ||
		strings.TrimSpace(candidate.TranscriptGeneration) == "" ||
		candidate.StartMs == nil || candidate.EndMs == nil {
		return Evidence{}, false
	}
	return Evidence{
		EvidenceSentenceID:   strings.TrimSpace(candidate.EvidenceSentenceID),
		ChunkID:              strings.TrimSpace(candidate.ChunkID),
		KnowledgeID:          strings.TrimSpace(candidate.KnowledgeID),
		VideoID:              strings.TrimSpace(candidate.VideoID),
		VideoTitle:           strings.TrimSpace(candidate.VideoTitle),
		StartMs:              *candidate.StartMs,
		EndMs:                *candidate.EndMs,
		TranscriptGeneration: strings.TrimSpace(candidate.TranscriptGeneration),
		SourceType:           firstNonEmpty(candidate.SourceType, SourceTypeTranscript),
	}, true
}

func EnrichReferences(refs []*types.SearchResult) []*types.SearchResult {
	for _, ref := range refs {
		EnrichSearchResult(ref)
	}
	return refs
}

// MergeReferences keeps ordinary and video references on one transport path.
// A validated video reference replaces its ordinary counterpart for the same
// source chunk, while distinct evidence sentences remain distinct.
func MergeReferences(existing, incoming []*types.SearchResult) []*types.SearchResult {
	merged := make([]*types.SearchResult, 0, len(existing)+len(incoming))
	add := func(ref *types.SearchResult) {
		if ref == nil {
			return
		}
		ref = EnrichSearchResult(ref)
		refIsVideo := isVideoReference(ref)
		refEvidenceID, refChunkID := referenceIdentity(ref)
		for position, current := range merged {
			currentIsVideo := isVideoReference(current)
			currentEvidenceID, currentChunkID := referenceIdentity(current)

			// Evidence sentence IDs are the strongest identity. This also
			// prevents two representations of the same evidence from being
			// persisted twice when their chunk IDs differ.
			if refEvidenceID != "" && refEvidenceID == currentEvidenceID {
				return
			}

			switch {
			case refIsVideo && currentIsVideo:
				// A single transcript chunk may contain multiple evidence
				// sentences. Keep them all; chunk ID alone is not a safe
				// deduplication key for video evidence.
				continue
			case refIsVideo && !currentIsVideo:
				// A validated video reference is the richer representation
				// of an ordinary reference for the same source chunk.
				if refChunkID != "" && refChunkID == currentChunkID {
					merged[position] = ref
					return
				}
			case !refIsVideo && currentIsVideo:
				// Once a chunk has a validated video representation, an
				// ordinary duplicate must not replace or hide it.
				if refChunkID != "" && refChunkID == currentChunkID {
					return
				}
			default:
				if refChunkID != "" && refChunkID == currentChunkID {
					return
				}
			}
		}
		merged = append(merged, ref)
	}
	for _, ref := range existing {
		add(ref)
	}
	for _, ref := range incoming {
		add(ref)
	}
	return merged
}

func referenceIdentity(ref *types.SearchResult) (evidenceID, chunkID string) {
	if ref == nil {
		return "", ""
	}
	if candidate, ok := CandidateFromSearchResult(ref); ok {
		evidenceID = strings.TrimSpace(candidate.EvidenceSentenceID)
		chunkID = strings.TrimSpace(candidate.ChunkID)
	}
	if chunkID == "" {
		chunkID = strings.TrimSpace(ref.ID)
	}
	return evidenceID, chunkID
}

func isVideoReference(ref *types.SearchResult) bool {
	if ref == nil {
		return false
	}
	candidate, ok := CandidateFromSearchResult(ref)
	return ok && candidate.SourceType == SourceTypeTranscript
}

func applyEvidenceMetadata(ref *types.SearchResult, evidence Evidence) {
	if ref.Metadata == nil {
		ref.Metadata = make(map[string]string)
	}
	ref.Metadata[metadataEvidenceID] = evidence.EvidenceSentenceID
	ref.Metadata[metadataChunkID] = evidence.ChunkID
	ref.Metadata[metadataVideoID] = evidence.VideoID
	ref.Metadata[metadataVideoTitle] = evidence.VideoTitle
	ref.Metadata[metadataStartMs] = strconv.Itoa(evidence.StartMs)
	ref.Metadata[metadataEndMs] = strconv.Itoa(evidence.EndMs)
	ref.Metadata[metadataGeneration] = evidence.TranscriptGeneration
	ref.Metadata[metadataSourceType] = evidence.SourceType
	ref.Metadata[metadataContract] = Version
	ref.Metadata["linkable"] = strconv.FormatBool(evidence.Linkable)
}

func stripVideoMetadata(ref *types.SearchResult) {
	if ref == nil || ref.Metadata == nil {
		return
	}
	for _, key := range []string{
		metadataEvidenceID, metadataChunkID, metadataVideoID, metadataVideoTitle,
		metadataStartMs, metadataEndMs, metadataGeneration, metadataSourceType,
		metadataContract, "linkable",
	} {
		delete(ref.Metadata, key)
	}
}

type transcriptMetadata struct {
	EvidenceSentenceID   string `json:"evidence_sentence_id"`
	VideoID              string `json:"video_id"`
	VideoTitle           string `json:"video_title"`
	StartMs              int    `json:"start_ms"`
	EndMs                int    `json:"end_ms"`
	TranscriptGeneration string `json:"transcript_generation"`
}

func parseTranscriptMetadata(content string) (transcriptMetadata, bool) {
	const section = "## 视频定位信息"
	const fence = "```json"
	start := strings.Index(content, section)
	if start < 0 {
		return transcriptMetadata{}, false
	}
	fenceStart := strings.Index(content[start+len(section):], fence)
	if fenceStart < 0 {
		return transcriptMetadata{}, false
	}
	fenceStart += start + len(section) + len(fence)
	fenceEnd := strings.Index(content[fenceStart:], "```")
	if fenceEnd < 0 {
		return transcriptMetadata{}, false
	}
	var parsed transcriptMetadata
	if err := json.Unmarshal([]byte(strings.TrimSpace(content[fenceStart:fenceStart+fenceEnd])), &parsed); err != nil {
		return transcriptMetadata{}, false
	}
	if !validTranscriptMetadata(parsed) {
		return transcriptMetadata{}, false
	}
	return parsed, true
}

func parseIntMetadata(value string) (int, bool) {
	if strings.TrimSpace(value) == "" {
		return 0, false
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	return parsed, err == nil
}

func stringMap(value interface{}) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case map[string]interface{}:
		result := make(map[string]string, len(typed))
		for key, raw := range typed {
			switch value := raw.(type) {
			case string:
				result[key] = value
			case json.Number:
				result[key] = value.String()
			case float64:
				result[key] = strconv.FormatFloat(value, 'f', -1, 64)
			default:
				if raw != nil {
					result[key] = fmt.Sprint(raw)
				}
			}
		}
		return result
	default:
		return nil
	}
}

func stringValue(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
