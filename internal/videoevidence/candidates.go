package videoevidence

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

var ErrCandidateJSONInvalid = errors.New("candidate_json_invalid")
var ErrCandidateTitleNotInWhitelist = errors.New("candidate_title_not_in_whitelist")
var candidateTaskTypeRE = regexp.MustCompile(`"task_type"\s*:\s*"multi_video_location"`)

// IsCandidateManifestOutput recognizes the intermediate envelope even when
// its remaining fields are invalid, so malformed candidates cannot fall
// through to the final-answer validator.
func IsCandidateManifestOutput(raw string) bool {
	return strings.HasPrefix(strings.TrimSpace(raw), "{") && candidateTaskTypeRE.MatchString(raw)
}

type VideoCandidate struct {
	CandidateRef    string `json:"-"` // server-owned ID, never accepted from model JSON
	Title           string `json:"title"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
}

type CandidateManifest struct {
	Version  string           `json:"version"`
	TaskType string           `json:"task_type"`
	Topic    string           `json:"topic"`
	Videos   []VideoCandidate `json:"videos"`
}

type CandidateTitleMatch string

const (
	CandidateTitleMatched    CandidateTitleMatch = "matched"
	CandidateTitleAmbiguous  CandidateTitleMatch = "ambiguous"
	CandidateTitleNotFound   CandidateTitleMatch = "not_found"
	CandidateTitleOutOfScope CandidateTitleMatch = "out_of_scope"
)

// ParseCandidateManifest accepts exactly one strict JSON object and no wrapping prose.
func ParseCandidateManifest(raw string) (CandidateManifest, error) {
	var manifest CandidateManifest
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return CandidateManifest{}, ErrCandidateJSONInvalid
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return CandidateManifest{}, ErrCandidateJSONInvalid
	}
	if manifest.Version != "1" || manifest.TaskType != "multi_video_location" || strings.TrimSpace(manifest.Topic) == "" || len(manifest.Videos) < 2 {
		return CandidateManifest{}, ErrCandidateJSONInvalid
	}
	seen := make(map[string]struct{}, len(manifest.Videos))
	for _, candidate := range manifest.Videos {
		if strings.TrimSpace(candidate.Title) == "" {
			return CandidateManifest{}, ErrCandidateJSONInvalid
		}
		key := candidate.KnowledgeBaseID + "\x00" + candidate.Title
		if _, ok := seen[key]; ok {
			return CandidateManifest{}, ErrCandidateJSONInvalid
		}
		seen[key] = struct{}{}
	}
	return manifest, nil
}

// MatchCandidateTitle requires an exact title match against the current native search result whitelist.
func MatchCandidateTitle(title, knowledgeBaseID string, whitelist []VideoCandidate) (VideoCandidate, CandidateTitleMatch) {
	title = strings.TrimSpace(title)
	var match VideoCandidate
	matches := 0
	for _, candidate := range whitelist {
		if candidate.Title != title {
			continue
		}
		if knowledgeBaseID != "" && candidate.KnowledgeBaseID != knowledgeBaseID {
			continue
		}
		matches++
		match = candidate
	}
	if matches == 0 {
		for _, candidate := range whitelist {
			if candidate.Title == title {
				return VideoCandidate{}, CandidateTitleOutOfScope
			}
		}
		return VideoCandidate{}, CandidateTitleNotFound
	}
	if matches > 1 {
		return VideoCandidate{}, CandidateTitleAmbiguous
	}
	return match, CandidateTitleMatched
}

type KnowledgeRetrievalStatus string

const (
	RetrievalUnresolved       KnowledgeRetrievalStatus = "unresolved"
	RetrievalEvidenceFound    KnowledgeRetrievalStatus = "evidence_found"
	RetrievalSearchedNoResult KnowledgeRetrievalStatus = "searched_no_evidence"
	RetrievalFailed           KnowledgeRetrievalStatus = "retrieval_failed"
)

type CandidateRetrieval struct {
	KnowledgeID string
	Status      KnowledgeRetrievalStatus
}

func SummarizeCandidateRetrievals(states []CandidateRetrieval) string {
	if len(states) == 0 {
		return "failed"
	}
	allTerminal := true
	allEvidence := true
	hasFailure := false
	for _, state := range states {
		if state.Status == RetrievalUnresolved || state.Status == RetrievalFailed {
			allTerminal = false
		}
		if state.Status == RetrievalFailed {
			hasFailure = true
		}
		if state.Status != RetrievalEvidenceFound {
			allEvidence = false
		}
	}
	if allEvidence {
		return "complete"
	}
	if !allTerminal || hasFailure {
		return "incomplete"
	}
	return "partial"
}
