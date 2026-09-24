// Package videoevidence owns the request-local contract for video citations.
//
// The package deliberately separates candidate data from canonical evidence:
// candidates may come from an adapter, while canonical video identity,
// timing, and transcript generation are accepted only from a validated scope.
package videoevidence

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	Capability = "video_evidence_citation"
	Version    = "v1"

	SourceTypeTranscript = "transcript"
	SourceTypeWiki       = "wiki"
)

// Supports reports whether an agent explicitly opts into the current public
// video evidence contract. Keeping this check here prevents each runtime
// entrypoint from gradually accepting a different version or spelling.
func Supports(version string) bool {
	return strings.TrimSpace(version) == Version
}

type ErrorCode string

const (
	ErrorInvalidHandle      ErrorCode = "invalid_handle"
	ErrorEvidenceOutOfScope ErrorCode = "evidence_out_of_scope"
	ErrorMissingVideo       ErrorCode = "missing_video"
	ErrorMissingTimeRange   ErrorCode = "missing_time_range"
	ErrorGenerationMismatch ErrorCode = "generation_mismatch"
	ErrorWikiOnlySource     ErrorCode = "wiki_only_source"
	ErrorInvalidJSON        ErrorCode = "invalid_json"
	ErrorUnknownField       ErrorCode = "unknown_field"
	ErrorTrailingContent    ErrorCode = "trailing_content"
	ErrorTruncatedOutput    ErrorCode = "truncated_output"
	ErrorCorrectionFailed   ErrorCode = "correction_failed"
	ErrorUnpublished        ErrorCode = "unpublished_evidence"
)

// ValidationError is the stable, safe-to-log failure shape exposed by this
// package. The wrapped error is for server diagnostics only.
type ValidationError struct {
	Code ErrorCode
	Err  error
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return string(e.Code)
}

func (e *ValidationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func CodeOf(err error) ErrorCode {
	var validationErr *ValidationError
	if errors.As(err, &validationErr) && validationErr != nil {
		return validationErr.Code
	}
	return ""
}

// Evidence is the only shape that downstream renderers may treat as a
// clickable video citation. All fields are source-owned, not model-owned.
type Evidence struct {
	EvidenceSentenceID   string `json:"evidence_sentence_id"`
	ChunkID              string `json:"chunk_id,omitempty"`
	KnowledgeID          string `json:"knowledge_id,omitempty"`
	VideoID              string `json:"video_id"`
	VideoTitle           string `json:"video_title"`
	StartMs              int    `json:"start_ms"`
	EndMs                int    `json:"end_ms"`
	TranscriptGeneration string `json:"transcript_generation"`
	SourceType           string `json:"source_type"`
	Linkable             bool   `json:"linkable"`
}

// Candidate is an adapter-facing shape. The validator may discard all
// candidate-owned identity and timing values and replace them with the
// canonical values from Scope.
type Candidate struct {
	EvidenceSentenceID   string `json:"evidence_sentence_id"`
	ChunkID              string `json:"chunk_id,omitempty"`
	KnowledgeID          string `json:"knowledge_id,omitempty"`
	VideoID              string `json:"video_id,omitempty"`
	VideoTitle           string `json:"video_title,omitempty"`
	StartMs              *int   `json:"start_ms,omitempty"`
	EndMs                *int   `json:"end_ms,omitempty"`
	TranscriptGeneration string `json:"transcript_generation,omitempty"`
	SourceType           string `json:"source_type,omitempty"`
}

// Scope is the immutable whitelist for one retrieval/request context.
type Scope struct {
	AllowedEvidenceIDs  map[string]Evidence
	AllowedChunkIDs     map[string]Evidence
	AllowedKnowledgeIDs map[string]Evidence
	CurrentGeneration   map[string]string
}

func NewScope() Scope {
	return Scope{
		AllowedEvidenceIDs:  make(map[string]Evidence),
		AllowedChunkIDs:     make(map[string]Evidence),
		AllowedKnowledgeIDs: make(map[string]Evidence),
		CurrentGeneration:   make(map[string]string),
	}
}

func (s *Scope) Add(evidence Evidence) {
	if s == nil {
		return
	}
	if s.AllowedEvidenceIDs == nil {
		*s = NewScope()
	}
	evidence.EvidenceSentenceID = strings.TrimSpace(evidence.EvidenceSentenceID)
	evidence.ChunkID = strings.TrimSpace(evidence.ChunkID)
	evidence.KnowledgeID = strings.TrimSpace(evidence.KnowledgeID)
	evidence.VideoID = strings.TrimSpace(evidence.VideoID)
	evidence.VideoTitle = strings.TrimSpace(evidence.VideoTitle)
	evidence.TranscriptGeneration = strings.TrimSpace(evidence.TranscriptGeneration)
	evidence.SourceType = strings.TrimSpace(evidence.SourceType)
	if evidence.SourceType == "" {
		evidence.SourceType = SourceTypeTranscript
	}
	if evidence.EvidenceSentenceID != "" {
		s.AllowedEvidenceIDs[evidence.EvidenceSentenceID] = evidence
	}
	if evidence.ChunkID != "" {
		s.AllowedChunkIDs[evidence.ChunkID] = evidence
	}
	if evidence.KnowledgeID != "" {
		s.AllowedKnowledgeIDs[evidence.KnowledgeID] = evidence
	}
	if evidence.VideoID != "" && evidence.TranscriptGeneration != "" {
		if s.CurrentGeneration[evidence.VideoID] == "" {
			s.CurrentGeneration[evidence.VideoID] = evidence.TranscriptGeneration
		}
	}
}

func (s Scope) Len() int {
	return len(s.AllowedEvidenceIDs)
}

// ProtocolPrompt is appended only for agents that explicitly declare v1.
func ProtocolPrompt() string {
	return `

## Video evidence citation protocol (video_evidence_citation/v1)
- Use only the source handles already supplied by the system, in the exact form <ref id="cN"/>.
- Any answer that locates, quotes, or attributes content to a video transcript MUST cite the supporting handle inline with the claim. A location answer without an inline <ref id="cN"/> is incomplete.
- Do not write start times, end times, durations, video IDs, evidence IDs, or video links yourself. The system owns those values and projects the validated time range after generation.
- Never create, transform, or infer a video ID, evidence ID, timestamp, video link, or source handle from prose, chunk indexes, or other metadata.
- Keep each citation next to the claim it supports, on the same line. If no validated transcript handle is available, state that the time is not verified instead of guessing.
- A Wiki source without a validated transcript evidence mapping is a normal source only; never turn it into a video citation.`
}

// SearchResultAdapter is the default adapter for retrieval results already
// represented by the platform's SearchResult type.
type SearchResultAdapter struct{}

func (SearchResultAdapter) Name() string { return "search_result" }

func (SearchResultAdapter) Normalize(_ context.Context, input AdapterInput) ([]Candidate, error) {
	if input.SearchResults == nil {
		return nil, &ValidationError{Code: ErrorInvalidHandle, Err: errors.New("search results are missing")}
	}
	candidates := make([]Candidate, 0, len(input.SearchResults))
	for _, result := range input.SearchResults {
		if result == nil {
			continue
		}
		candidate, ok := CandidateFromSearchResult(result)
		if !ok {
			continue
		}
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return nil, &ValidationError{Code: ErrorInvalidHandle, Err: errors.New("no video evidence candidate")}
	}
	return candidates, nil
}

type AdapterInput struct {
	RawJSON       json.RawMessage
	SearchResults []*types.SearchResult
	Scope         Scope
}

// Adapter normalizes a source-specific result into candidates. It must not
// decide whether a candidate is publishable; ResolveAdapterOutput performs
// the final scope and source validation.
type Adapter interface {
	Name() string
	Normalize(context.Context, AdapterInput) ([]Candidate, error)
}

// ResolveAdapterOutput is the shared adapter boundary for new agents. An
// adapter may normalize a source-specific payload, but only this function may
// turn those candidates into publishable evidence through the request scope.
func ResolveAdapterOutput(ctx context.Context, adapter Adapter, input AdapterInput) ([]Evidence, error) {
	if adapter == nil {
		return nil, &ValidationError{Code: ErrorInvalidHandle, Err: errors.New("video evidence adapter is missing")}
	}
	candidates, err := adapter.Normalize(ctx, input)
	if err != nil {
		return nil, err
	}
	evidence := make([]Evidence, 0, len(candidates))
	for _, candidate := range candidates {
		item, validationErr := NormalizeCandidate(candidate, input.Scope)
		if validationErr != nil {
			return nil, validationErr
		}
		evidence = append(evidence, item)
	}
	if len(evidence) == 0 {
		return nil, &ValidationError{Code: ErrorInvalidHandle, Err: errors.New("video evidence adapter returned no candidates")}
	}
	return evidence, nil
}
