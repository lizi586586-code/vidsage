package videoevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// JSONAdapter decodes a strict, source-specific candidate payload. A repair
// function, when supplied, is invoked at most once and receives only the raw
// payload. It may repair syntax, but it cannot invent a candidate.
type JSONAdapter struct {
	Repair func(context.Context, string) (string, error)
}

func (a JSONAdapter) Name() string { return "json" }

func (a JSONAdapter) Normalize(ctx context.Context, input AdapterInput) ([]Candidate, error) {
	raw := strings.TrimSpace(string(input.RawJSON))
	if raw == "" {
		return nil, &ValidationError{Code: ErrorInvalidJSON, Err: errors.New("empty candidate payload")}
	}
	candidates, err := decodeCandidates([]byte(raw))
	if err != nil && a.Repair != nil {
		repaired, repairErr := a.Repair(ctx, raw)
		if repairErr != nil {
			return nil, &ValidationError{Code: ErrorCorrectionFailed, Err: errors.New("candidate correction failed")}
		}
		candidates, err = decodeCandidates([]byte(strings.TrimSpace(repaired)))
		if err != nil {
			return nil, &ValidationError{Code: ErrorCorrectionFailed, Err: errors.New("corrected candidate payload is invalid")}
		}
	}
	if err != nil {
		return nil, err
	}
	normalized := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if _, err := NormalizeCandidate(candidate, input.Scope); err != nil {
			return nil, err
		}
		normalized = append(normalized, candidate)
	}
	return normalized, nil
}

func decodeCandidates(raw []byte) ([]Candidate, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()

	var value json.RawMessage
	if err := decoder.Decode(&value); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, &ValidationError{Code: ErrorTruncatedOutput, Err: errors.New("candidate payload is truncated")}
		}
		return nil, &ValidationError{Code: ErrorInvalidJSON, Err: errors.New("candidate payload is not valid JSON")}
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, &ValidationError{Code: ErrorTrailingContent, Err: errors.New("candidate payload has trailing content")}
		}
		return nil, &ValidationError{Code: ErrorInvalidJSON, Err: errors.New("candidate payload has invalid trailing content")}
	}

	switch firstNonByte(value) {
	case '[':
		return decodeStrictArray(value)
	case '{':
		return decodeStrictEnvelope(value)
	default:
		return nil, &ValidationError{Code: ErrorInvalidJSON, Err: fmt.Errorf("candidate payload must be an object or array")}
	}
}

func decodeStrictArray(raw []byte) ([]Candidate, error) {
	var candidates []Candidate
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&candidates); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return nil, &ValidationError{Code: ErrorUnknownField, Err: errors.New("candidate payload contains an unknown field")}
		}
		return nil, &ValidationError{Code: ErrorInvalidJSON, Err: errors.New("candidate array is invalid")}
	}
	return candidates, nil
}

func decodeStrictEnvelope(raw []byte) ([]Candidate, error) {
	var envelope struct {
		References []Candidate `json:"references"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return nil, &ValidationError{Code: ErrorUnknownField, Err: errors.New("candidate envelope contains an unknown field")}
		}
		return nil, &ValidationError{Code: ErrorInvalidJSON, Err: errors.New("candidate envelope is invalid")}
	}
	return envelope.References, nil
}

func firstNonByte(raw []byte) byte {
	for _, value := range raw {
		if value > ' ' {
			return value
		}
	}
	return 0
}
