package trainingorchestration

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/token"
)

type CompletionClient interface {
	Complete(context.Context, string) (string, error)
	Model() string
	PromptVersion() string
}

type streamingCompletionClient interface {
	Stream(context.Context, string, func(string) error) (string, error)
}

type Generator struct {
	LLM            CompletionClient
	MaxInputTokens int
	PromptVersion  string
	Now            func() time.Time
}

type modelProjection struct {
	TopicClusters         []TopicCluster         `json:"topic_clusters"`
	TopicClusterRelations []TopicClusterRelation `json:"topic_cluster_relations"`
}

//go:embed prompts/training-orchestration-v2.txt
var trainingOrchestrationPromptV2 string

type InputCapacityError struct{ Tokens, Limit int }

func (e *InputCapacityError) Error() string {
	return fmt.Sprintf("training orchestration input exceeds configured token limit %d: got %d", e.Limit, e.Tokens)
}

func (g *Generator) Generate(ctx context.Context, input InputPackage) (ProjectionDocument, error) {
	if g == nil || g.LLM == nil {
		return ProjectionDocument{}, fmt.Errorf("training orchestration llm is not configured")
	}
	input = withoutKnowledgeObjects(input)
	fingerprint, err := SourceFingerprint(input, g.LLM.Model(), g.promptVersion())
	if err != nil {
		return ProjectionDocument{}, err
	}
	now := time.Now().UTC()
	if g.Now != nil {
		now = g.Now().UTC()
	}
	projection := Projection{SchemaVersion: SchemaVersion, OwnerScopeID: input.OwnerScopeID, SourceFingerprint: fingerprint, GeneratedAt: now.Format(time.RFC3339Nano), TopicClusters: []TopicCluster{}, TopicClusterRelations: []TopicClusterRelation{}}
	if len(input.QualifiedVideos) == 0 {
		projection.Statistics = buildStatistics(input, nil, map[string]struct{}{}, nil)
		doc := ProjectionDocument{TrainingPathProjection: projection}
		if err := ValidateProjection(doc, input); err != nil {
			return ProjectionDocument{}, err
		}
		return doc, nil
	}
	prompt, err := buildPrompt(input)
	if err != nil {
		return ProjectionDocument{}, err
	}
	if g.MaxInputTokens > 0 {
		estimator, estimatorErr := token.NewEstimator()
		if estimatorErr != nil {
			return ProjectionDocument{}, fmt.Errorf("initialize training orchestration tokenizer: %w", estimatorErr)
		}
		count := estimator.EstimateString(prompt)
		if count > g.MaxInputTokens {
			return ProjectionDocument{}, &InputCapacityError{Tokens: count, Limit: g.MaxInputTokens}
		}
	}
	var raw string
	if streaming, ok := g.LLM.(streamingCompletionClient); ok {
		raw, err = streaming.Stream(ctx, prompt, nil)
	} else {
		raw, err = g.LLM.Complete(ctx, prompt)
	}
	if err != nil {
		return ProjectionDocument{}, fmt.Errorf("generate training orchestration: %w", err)
	}
	var generated modelProjection
	decoder := json.NewDecoder(bytes.NewReader(stripJSONFence(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&generated); err != nil {
		return ProjectionDocument{}, fmt.Errorf("decode training orchestration model output: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return ProjectionDocument{}, fmt.Errorf("decode training orchestration model output: trailing content")
	}
	if generated.TopicClusters == nil || generated.TopicClusterRelations == nil {
		return ProjectionDocument{}, fmt.Errorf("decode training orchestration model output: topic arrays must be JSON arrays")
	}
	projection.TopicClusters = generated.TopicClusters
	projection.TopicClusterRelations = generated.TopicClusterRelations
	normalizeProjectionKnowledgeFields(&projection)
	selectedVideos := map[string]struct{}{}
	unitEvidence := make([]EvidenceRef, 0)
	for _, cluster := range projection.TopicClusters {
		for _, id := range cluster.SourceVideoIDs {
			selectedVideos[id] = struct{}{}
		}
		for _, stage := range cluster.Path.Stages {
			for _, unit := range stage.Units {
				unitEvidence = append(unitEvidence, unit.EvidenceRefs...)
			}
		}
	}
	projection.Statistics = buildStatistics(input, projection.TopicClusters, selectedVideos, unitEvidence)
	doc := ProjectionDocument{TrainingPathProjection: projection}
	if err := ValidateProjection(doc, input); err != nil {
		return ProjectionDocument{}, fmt.Errorf("reject training orchestration model output: %w", err)
	}
	return doc, nil
}

func (g *Generator) promptVersion() string {
	if strings.TrimSpace(g.PromptVersion) != "" {
		return strings.TrimSpace(g.PromptVersion)
	}
	return g.LLM.PromptVersion()
}

func SourceFingerprint(input InputPackage, model, promptVersion string) (string, error) {
	input = withoutKnowledgeObjects(input)
	payload := struct {
		Input         InputPackage `json:"input"`
		Model         string       `json:"model"`
		PromptVersion string       `json:"prompt_version"`
		SchemaVersion string       `json:"schema_version"`
	}{input, strings.TrimSpace(model), strings.TrimSpace(promptVersion), SchemaVersion}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode training orchestration fingerprint: %w", err)
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("sha256:%x", sum[:]), nil
}

func buildPrompt(input InputPackage) (string, error) {
	input = withoutKnowledgeObjects(input)
	raw, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("encode training orchestration input: %w", err)
	}
	return strings.TrimSpace(trainingOrchestrationPromptV2) + "\n\nINPUT:\n" + string(raw), nil
}

func stripJSONFence(raw string) []byte {
	trimmed := strings.TrimSpace(raw)
	for _, tag := range []string{"think", "analysis"} {
		opening := "<" + tag + ">"
		closing := "</" + tag + ">"
		if strings.HasPrefix(strings.ToLower(trimmed), opening) {
			end := strings.Index(strings.ToLower(trimmed), closing)
			if end < 0 {
				return []byte(trimmed)
			}
			trimmed = strings.TrimSpace(trimmed[end+len(closing):])
		}
	}
	if strings.HasPrefix(trimmed, "```") {
		lines := strings.Split(trimmed, "\n")
		if len(lines) >= 3 {
			lines = lines[1 : len(lines)-1]
			trimmed = strings.TrimSpace(strings.Join(lines, "\n"))
		}
	}
	return []byte(trimmed)
}
