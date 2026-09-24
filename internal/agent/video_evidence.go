package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/videoevidence"
)

func collectVideoEvidenceReferencesFromToolCalls(toolCalls []types.ToolCall) []*types.SearchResult {
	refs := make([]*types.SearchResult, 0)
	seen := make(map[string]struct{})
	for _, toolCall := range toolCalls {
		for _, ref := range videoEvidenceReferencesFromToolData(toolCall.Name, toolCall.Result) {
			if ref == nil {
				continue
			}
			ref = videoevidence.EnrichSearchResult(ref)
			candidate, ok := videoevidence.CandidateFromSearchResult(ref)
			if !ok || strings.TrimSpace(candidate.EvidenceSentenceID) == "" {
				continue
			}
			if _, exists := seen[candidate.EvidenceSentenceID]; exists {
				continue
			}
			seen[candidate.EvidenceSentenceID] = struct{}{}
			refs = append(refs, ref)
		}
	}
	return refs
}

func videoEvidenceReferencesFromToolData(toolName string, result *types.ToolResult) []*types.SearchResult {
	if result == nil || result.Data == nil {
		return nil
	}
	displayType := stringValue(result.Data, "display_type")
	if displayType != "grep_results" && displayType != "search_results" &&
		displayType != "knowledge_chunks_list" &&
		toolName != agenttools.ToolGrepChunks && toolName != agenttools.ToolKnowledgeSearch &&
		toolName != agenttools.ToolListKnowledgeChunks {
		return nil
	}

	rows := append(mapSlice(result.Data["chunk_results"]), mapSlice(result.Data["chunks"])...)
	rows = append(rows, mapSlice(result.Data["knowledge_results"])...)
	if len(rows) == 0 {
		rows = mapSlice(result.Data["results"])
	}
	if len(rows) == 0 {
		return nil
	}

	refs := make([]*types.SearchResult, 0, len(rows))
	for _, row := range rows {
		ref := searchResultFromMap(row)
		if ref == nil {
			continue
		}
		videoevidence.EnrichSearchResult(ref)
		if _, ok := videoevidence.CandidateFromSearchResult(ref); !ok {
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

// answerReferencesFromToolCalls keeps every request-local knowledge reference
// available to the final answer contract. Video evidence validation still
// happens separately through CandidateFromSearchResult; ordinary Wiki/KB
// references are retained only as normal citations.
func answerReferencesFromToolCalls(toolCalls []types.ToolCall) []*types.SearchResult {
	refs := make([]*types.SearchResult, 0)
	for _, toolCall := range toolCalls {
		result := toolCall.Result
		if result == nil || result.Data == nil {
			continue
		}
		displayType := stringValue(result.Data, "display_type")
		if displayType != "grep_results" && displayType != "search_results" &&
			displayType != "knowledge_chunks_list" &&
			toolCall.Name != agenttools.ToolGrepChunks && toolCall.Name != agenttools.ToolKnowledgeSearch &&
			toolCall.Name != agenttools.ToolListKnowledgeChunks {
			continue
		}
		rows := append(mapSlice(result.Data["chunk_results"]), mapSlice(result.Data["chunks"])...)
		rows = append(rows, mapSlice(result.Data["knowledge_results"])...)
		if len(rows) == 0 {
			rows = mapSlice(result.Data["results"])
		}
		for _, row := range rows {
			ref := searchResultFromMap(row)
			if ref == nil || strings.TrimSpace(ref.ID) == "" {
				continue
			}
			if strings.EqualFold(ref.ChunkType, string(types.ChunkTypeWebSearch)) ||
				strings.EqualFold(ref.KnowledgeSource, "web_search") {
				continue
			}
			sourceType := ""
			if ref.Metadata != nil {
				sourceType = ref.Metadata["source_type"]
			}
			videoevidence.EnrichSearchResult(ref)
			if sourceType != "" && ref.Metadata != nil && ref.Metadata["source_type"] == "" {
				ref.Metadata["source_type"] = sourceType
			}
			refs = append(refs, ref)
		}
	}
	return refs
}

func searchResultFromMap(row map[string]interface{}) *types.SearchResult {
	if row == nil {
		return nil
	}
	id := firstNonEmptyString(
		stringValue(row, "id"),
		stringValue(row, "chunk_id"),
		stringValue(row, "faq_id"),
	)
	return &types.SearchResult{
		ID:              id,
		Content:         stringValue(row, "content"),
		KnowledgeID:     stringValue(row, "knowledge_id"),
		KnowledgeTitle:  firstNonEmptyString(stringValue(row, "knowledge_title"), stringValue(row, "title")),
		KnowledgeBaseID: firstNonEmptyString(stringValue(row, "knowledge_base_id"), stringValue(row, "knowledge_base")),
		ChunkType:       stringValue(row, "chunk_type"),
		ChunkIndex:      intValue(row, "chunk_index"),
		Metadata:        stringMap(row["metadata"]),
	}
}

func (e *AgentEngine) projectAnswerContract(
	contract videoevidence.AnswerContract,
	refs []*types.SearchResult,
) (videoevidence.AnswerProjection, error) {
	handles := e.videoEvidenceHandles(refs)
	ordinaryHandles := e.ordinaryAnswerHandles(refs)
	return videoevidence.ProjectAnswerContractWithOrdinaryHandles(contract, handles, ordinaryHandles)
}

func (e *AgentEngine) videoEvidenceHandles(refs []*types.SearchResult) map[string]videoevidence.Evidence {
	handles := make(map[string]videoevidence.Evidence)
	if e == nil || e.modelContext == nil || len(refs) == 0 {
		return handles
	}
	e.modelContext.RegisterSearchResults(refs)
	scope := videoevidence.ScopeFromReferences(refs)
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		candidate, ok := videoevidence.CandidateFromSearchResult(ref)
		if !ok {
			continue
		}
		evidence, err := videoevidence.NormalizeCandidate(candidate, scope)
		if err != nil || !evidence.Linkable {
			continue
		}
		handle := strings.TrimSpace(e.modelContext.ChunkHandle(ref.ID))
		if handle == "" && candidate.ChunkID != "" {
			handle = strings.TrimSpace(e.modelContext.ChunkHandle(candidate.ChunkID))
		}
		if handle == "" {
			continue
		}
		handles[handle] = evidence
	}
	return handles
}

// ordinaryAnswerHandles returns request-local handles that may remain normal
// knowledge/Wiki citations in a mixed answer. Transcript-shaped references
// are deliberately excluded even when malformed or stale: they must either
// pass the video evidence whitelist or fail closed.
func (e *AgentEngine) ordinaryAnswerHandles(refs []*types.SearchResult) map[string]struct{} {
	handles := make(map[string]struct{})
	if e == nil || e.modelContext == nil || len(refs) == 0 {
		return handles
	}
	e.modelContext.RegisterSearchResults(refs)
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		sourceType := strings.ToLower(strings.TrimSpace(ref.Metadata["source_type"]))
		if sourceType == videoevidence.SourceTypeTranscript ||
			strings.EqualFold(strings.TrimSpace(ref.ChunkType), videoevidence.SourceTypeTranscript) ||
			strings.EqualFold(strings.TrimSpace(ref.ChunkType), "summary") {
			continue
		}
		if handle := strings.TrimSpace(e.modelContext.ChunkHandle(ref.ID)); handle != "" {
			handles[handle] = struct{}{}
		}
	}
	return handles
}

func (e *AgentEngine) emitValidatedFinalAnswer(ctx context.Context, sessionID, answer string) {
	if e == nil || e.eventBus == nil {
		return
	}
	answerID := generateEventID("answer")
	if strings.TrimSpace(answer) != "" {
		e.eventBus.Emit(ctx, event.Event{
			ID:        answerID,
			Type:      event.EventAgentFinalAnswer,
			SessionID: sessionID,
			Data: event.AgentFinalAnswerData{
				Content: answer,
				Done:    false,
			},
		})
	}
	e.eventBus.Emit(ctx, event.Event{
		ID:        answerID,
		Type:      event.EventAgentFinalAnswer,
		SessionID: sessionID,
		Data: event.AgentFinalAnswerData{
			Content: "",
			Done:    true,
		},
	})
}

func mapSlice(value interface{}) []map[string]interface{} {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(encoded, &rows); err != nil {
		return nil
	}
	return rows
}

func stringMap(value interface{}) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		return typed
	case map[string]interface{}:
		result := make(map[string]string, len(typed))
		for key, raw := range typed {
			if raw != nil {
				result[key] = fmt.Sprint(raw)
			}
		}
		return result
	default:
		return nil
	}
}

func stringValue(values map[string]interface{}, key string) string {
	if values == nil {
		return ""
	}
	switch value := values[key].(type) {
	case string:
		return strings.TrimSpace(value)
	case fmt.Stringer:
		return strings.TrimSpace(value.String())
	case json.Number:
		return strings.TrimSpace(value.String())
	default:
		return ""
	}
}

func intValue(values map[string]interface{}, key string) int {
	if values == nil {
		return 0
	}
	switch value := values[key].(type) {
	case int:
		return value
	case int32:
		return int(value)
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		parsed, _ := value.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
