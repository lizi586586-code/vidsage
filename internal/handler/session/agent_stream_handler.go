package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/agent/skills"
	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/videoevidence"
)

// AgentStreamHandler handles agent events for SSE streaming
// It uses a dedicated EventBus per request to avoid SessionID filtering
// Events are appended to StreamManager without accumulation
type AgentStreamHandler struct {
	ctx                context.Context
	sessionID          string
	tenantID           uint64 // Tenant that owns this session; used when persisting skill artifacts.
	assistantMessageID string
	requestID          string
	receivedAt         time.Time // Handler entry timestamp, used for TTFB logging
	ttfbLogged         bool      // Guards one-shot TTFB log on first answer chunk
	assistantMessage   *types.Message
	streamManager      interfaces.StreamManager

	eventBus *event.EventBus

	// artifactCollector drains skill-generated files from the session
	// sandbox after the agent completes. Nil when the sandbox backend
	// doesn't support artifact collection or WeKnora was built without it.
	artifactCollector *service.ArtifactCollector
	route             routeMetadata

	// State tracking
	knowledgeRefs   []*types.SearchResult
	evidenceScope   videoevidence.Scope
	finalAnswer     string
	answerSegments  []*answerSegment     // Per-answer-event-ID accumulation, so superseded preambles can be dropped
	eventStartTimes map[string]time.Time // Track start time for duration calculation
	mu              sync.Mutex
}

// answerSegment accumulates the streamed content of a single final-answer event
// ID. A non-terminal round may stream a preamble ("let me search…") under its
// own answer ID and then be marked superseded once the round turns out to call
// tools; tracking segments separately lets us exclude that preamble from the
// persisted assistant message instead of leaking it into the final answer.
type answerSegment struct {
	id         string
	content    string
	superseded bool
}

// findAnswerSegment returns the segment for an answer event ID, or nil.
// Callers must hold h.mu.
func (h *AgentStreamHandler) findAnswerSegment(id string) *answerSegment {
	for _, seg := range h.answerSegments {
		if seg.id == id {
			return seg
		}
	}
	return nil
}

// composeFinalAnswer rebuilds the persisted answer from all non-superseded
// segments in arrival order. Callers must hold h.mu.
func (h *AgentStreamHandler) composeFinalAnswer() string {
	var b strings.Builder
	for _, seg := range h.answerSegments {
		if !seg.superseded {
			b.WriteString(seg.content)
		}
	}
	return b.String()
}

func mapSlice(value interface{}) []map[string]interface{} {
	if value == nil {
		return nil
	}
	var result []map[string]interface{}
	bytes, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(bytes, &result); err != nil {
		return nil
	}
	return result
}

func stringMapValue(values map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(fmt.Sprint(values[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}

func videoEvidenceReferencesFromToolData(toolName string, data map[string]interface{}) []*types.SearchResult {
	if data == nil {
		return nil
	}
	displayType := stringMapValue(data, "display_type")
	if displayType != "grep_results" && displayType != "search_results" &&
		displayType != "knowledge_chunks_list" &&
		toolName != agenttools.ToolGrepChunks && toolName != agenttools.ToolKnowledgeSearch &&
		toolName != agenttools.ToolListKnowledgeChunks {
		return nil
	}

	// list_knowledge_chunks returns the authoritative transcript rows under
	// `chunks`; grep_chunks uses `chunk_results`. Both carry the same
	// backend-owned evidence metadata, so collect all structured row forms
	// before projecting video evidence.
	rows := append(mapSlice(data["chunk_results"]), mapSlice(data["chunks"])...)
	rows = append(rows, mapSlice(data["knowledge_results"])...)
	if len(rows) == 0 {
		rows = mapSlice(data["results"])
	}
	if len(rows) == 0 {
		return nil
	}

	refs := make([]*types.SearchResult, 0)
	seen := make(map[string]struct{})
	for _, row := range rows {
		ref := searchResultFromMap(row)
		if ref == nil {
			continue
		}
		if ref.ID == "" {
			for _, key := range []string{"id", "chunk_id", "faq_id"} {
				if ref.ID = getString(row, key); ref.ID != "" {
					break
				}
			}
		}
		// Some list_knowledge_chunks rows carry the audited locator in the
		// canonical transcript body instead of a separate metadata map.
		// Enrich before validating so both representations follow the same
		// backend-owned parsing path.
		videoevidence.EnrichSearchResult(ref)
		candidate, ok := videoevidence.CandidateFromSearchResult(ref)
		if !ok {
			continue
		}
		if _, exists := seen[candidate.EvidenceSentenceID]; exists {
			continue
		}
		seen[candidate.EvidenceSentenceID] = struct{}{}
		if ref.ID == "" {
			ref.ID = candidate.ChunkID
		}
		refs = append(refs, ref)
		if len(refs) >= 20 {
			return refs
		}
	}
	return refs
}

func agentRouteMode(agentID string) string {
	if strings.TrimSpace(agentID) == types.BuiltinQuickAnswerID {
		return "quick"
	}
	return "reasoning"
}

func projectVideoEvidence(refs []*types.SearchResult) ([]videoevidence.Evidence, string) {
	return videoevidence.ProjectReferences(refs)
}

func (h *AgentStreamHandler) appendToolEvidenceReferences(toolName string, data map[string]interface{}) {
	refs := videoEvidenceReferencesFromToolData(toolName, data)
	if len(refs) == 0 {
		return
	}

	h.mu.Lock()
	videoevidence.MergeScopes(&h.evidenceScope, videoevidence.ScopeFromReferences(refs))
	refs = videoevidence.NormalizeReferences(h.evidenceScope, refs)
	h.knowledgeRefs = videoevidence.MergeReferences(h.knowledgeRefs, refs)
	if len(h.knowledgeRefs) > 0 {
		h.assistantMessage.KnowledgeReferences = h.knowledgeRefs
	}
	snapshot := append([]*types.SearchResult(nil), h.knowledgeRefs...)
	h.mu.Unlock()

	if len(snapshot) == 0 {
		return
	}
	projected, coverage := projectVideoEvidence(snapshot)
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        fmt.Sprintf("references-%d", time.Now().UnixNano()),
		Type:      types.ResponseTypeReferences,
		Content:   "",
		Done:      false,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"references":     types.References(snapshot),
			"route_mode":     agentRouteMode(h.assistantMessage.AgentID),
			"coverage":       coverage,
			"video_evidence": projected,
		},
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append tool evidence references event failed", "error", err)
	}
}

// NewAgentStreamHandler creates a new handler for agent SSE streaming
func NewAgentStreamHandler(
	ctx context.Context,
	sessionID, assistantMessageID, requestID string,
	tenantID uint64,
	receivedAt time.Time,
	assistantMessage *types.Message,
	streamManager interfaces.StreamManager,
	eventBus *event.EventBus,
	artifactCollector *service.ArtifactCollector,
) *AgentStreamHandler {
	return &AgentStreamHandler{
		ctx:                ctx,
		sessionID:          sessionID,
		tenantID:           tenantID,
		assistantMessageID: assistantMessageID,
		requestID:          requestID,
		receivedAt:         receivedAt,
		assistantMessage:   assistantMessage,
		streamManager:      streamManager,
		eventBus:           eventBus,
		artifactCollector:  artifactCollector,
		knowledgeRefs:      make([]*types.SearchResult, 0),
		evidenceScope:      videoevidence.NewScope(),
		eventStartTimes:    make(map[string]time.Time),
	}
}

// SetRouteMetadata attaches backend-resolved routing state to the stream
// boundary. It is called before any agent event can be emitted.
func (h *AgentStreamHandler) SetRouteMetadata(route routeMetadata) {
	if h == nil {
		return
	}
	h.route = route
}

// Subscribe subscribes to all agent streaming events on the dedicated EventBus
// No SessionID filtering needed since we have a dedicated EventBus per request
func (h *AgentStreamHandler) Subscribe() {
	// Subscribe to all agent streaming events on the dedicated EventBus
	h.eventBus.On(event.EventAgentThought, h.handleThought)
	h.eventBus.On(event.EventAgentToolCall, h.handleToolCall)
	h.eventBus.On(event.EventAgentToolResult, h.handleToolResult)
	h.eventBus.On(event.EventAgentReferences, h.handleReferences)
	h.eventBus.On(event.EventMemoryRecalled, h.handleMemoryRecalled)
	h.eventBus.On(event.EventAgentFinalAnswer, h.handleFinalAnswer)
	h.eventBus.On(event.EventAgentReflection, h.handleReflection)
	h.eventBus.On(event.EventError, h.handleError)
	h.eventBus.On(event.EventSessionTitle, h.handleSessionTitle)
	h.eventBus.On(event.EventAgentComplete, h.handleComplete)
	h.eventBus.On(event.EventToolApprovalRequired, h.handleToolApprovalRequired)
	h.eventBus.On(event.EventToolApprovalResolved, h.handleToolApprovalResolved)
	h.eventBus.On(event.EventMCPOAuthRequired, h.handleMCPOAuthRequired)
	h.eventBus.On(event.EventMCPOAuthResolved, h.handleMCPOAuthResolved)
}

// handleThought handles agent thought events
func (h *AgentStreamHandler) handleThought(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.AgentThoughtData)
	if !ok {
		return nil
	}

	h.mu.Lock()

	// Track start time on first chunk
	if _, exists := h.eventStartTimes[evt.ID]; !exists {
		h.eventStartTimes[evt.ID] = time.Now()
	}

	// Calculate duration if done
	var metadata map[string]interface{}
	if data.Done {
		startTime := h.eventStartTimes[evt.ID]
		duration := time.Since(startTime)
		metadata = map[string]interface{}{
			"event_id":     evt.ID,
			"duration_ms":  duration.Milliseconds(),
			"completed_at": time.Now().Unix(),
		}
		delete(h.eventStartTimes, evt.ID)
	} else {
		metadata = map[string]interface{}{
			"event_id": evt.ID,
		}
	}

	h.mu.Unlock()

	// Append this chunk to stream (no accumulation - frontend will accumulate)
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeThinking,
		Content:   data.Content, // Just this chunk
		Done:      data.Done,
		Timestamp: time.Now(),
		Data:      metadata,
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append thought event to stream failed", "error", err)
	}

	return nil
}

// handleToolCall handles tool call events
func (h *AgentStreamHandler) handleToolCall(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.AgentToolCallData)
	if !ok {
		return nil
	}

	h.mu.Lock()
	// Track start time for this tool call (use tool_call_id as key)
	h.eventStartTimes[data.ToolCallID] = time.Now()
	// Any answer text streamed before this tool call was a non-terminal round's
	// preamble, not the final answer (the agent only ends by stopping naturally
	// with plain text and no tool calls). Drop those segments from the persisted
	// answer so the preamble never leaks into Message.Content.
	supersededAny := false
	for _, seg := range h.answerSegments {
		if !seg.superseded && seg.content != "" {
			seg.superseded = true
			supersededAny = true
		}
	}
	if supersededAny {
		h.finalAnswer = h.composeFinalAnswer()
	}
	h.mu.Unlock()

	metadata := map[string]interface{}{
		"tool_name":    data.ToolName,
		"arguments":    data.Arguments,
		"tool_call_id": data.ToolCallID,
	}

	// Append event to stream
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeToolCall,
		Content:   fmt.Sprintf("Calling tool: %s", data.ToolName),
		Done:      false,
		Timestamp: time.Now(),
		Data:      metadata,
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append tool call event to stream failed", "error", err)
	}

	return nil
}

// handleToolResult handles tool result events
func (h *AgentStreamHandler) handleToolResult(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.AgentToolResultData)
	if !ok {
		return nil
	}

	h.mu.Lock()
	// Calculate duration from start time if available, otherwise use provided duration
	var durationMs int64
	if startTime, exists := h.eventStartTimes[data.ToolCallID]; exists {
		durationMs = time.Since(startTime).Milliseconds()
		delete(h.eventStartTimes, data.ToolCallID)
	} else if data.Duration > 0 {
		// Fallback to provided duration if start time not tracked
		durationMs = data.Duration
	}
	h.mu.Unlock()

	// Send SSE response (both success and failure)
	responseType := types.ResponseTypeToolResult
	content := agenttools.StreamContentForToolResult(data.ToolName, data.Success, data.Error, data.Data)
	if !data.Success {
		responseType = types.ResponseTypeError
		if content == "" && data.Error != "" {
			content = data.Error
		}
	}

	// Build metadata including tool result data for rich frontend rendering
	metadata := map[string]interface{}{
		"tool_name":    data.ToolName,
		"success":      data.Success,
		"error":        data.Error,
		"duration_ms":  durationMs,
		"tool_call_id": data.ToolCallID,
	}

	clientData := agenttools.SanitizeToolResultForClient(data.ToolName, &types.ToolResult{
		Success: data.Success,
		Output:  data.Output,
		Error:   data.Error,
		Data:    data.Data,
	})
	for k, v := range clientData {
		metadata[k] = v
	}

	h.appendToolEvidenceReferences(data.ToolName, data.Data)

	// Append event to stream
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      responseType,
		Content:   content,
		Done:      false,
		Timestamp: time.Now(),
		Data:      metadata,
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append tool result event to stream failed", "error", err)
	}

	return nil
}

func toolApprovalDataToMap(v interface{}) map[string]interface{} {
	b, err := json.Marshal(v)
	if err != nil {
		return map[string]interface{}{}
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]interface{}{}
	}
	return m
}

// handleToolApprovalRequired persists MCP tool human-approval prompts for SSE / replay (issue #1173).
func (h *AgentStreamHandler) handleToolApprovalRequired(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.ToolApprovalRequiredData)
	if !ok {
		return nil
	}
	meta := toolApprovalDataToMap(data)
	meta["pending_id"] = data.PendingID
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeToolApprovalRequired,
		Content:   "MCP tool requires human approval",
		Done:      true,
		Timestamp: time.Now(),
		Data:      meta,
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append tool approval required event failed", "error", err)
	}
	return nil
}

// handleToolApprovalResolved persists the outcome of a tool approval (issue #1173).
func (h *AgentStreamHandler) handleToolApprovalResolved(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.ToolApprovalResolvedData)
	if !ok {
		return nil
	}
	meta := toolApprovalDataToMap(data)
	meta["pending_id"] = data.PendingID
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeToolApprovalResolved,
		Content:   "MCP tool approval resolved",
		Done:      true,
		Timestamp: time.Now(),
		Data:      meta,
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append tool approval resolved event failed", "error", err)
	}
	return nil
}

// handleMCPOAuthRequired forwards an in-conversation "authorize this MCP
// service" prompt to the SSE stream so the UI can render an Authorize card.
func (h *AgentStreamHandler) handleMCPOAuthRequired(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.MCPOAuthRequiredData)
	if !ok {
		return nil
	}
	meta := toolApprovalDataToMap(data)
	meta["pending_id"] = data.PendingID
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeMCPOAuthRequired,
		Content:   "MCP service requires OAuth authorization",
		Done:      true,
		Timestamp: time.Now(),
		Data:      meta,
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append mcp oauth required event failed", "error", err)
	}
	return nil
}

// handleMCPOAuthResolved forwards the outcome of an in-conversation OAuth prompt.
func (h *AgentStreamHandler) handleMCPOAuthResolved(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.MCPOAuthResolvedData)
	if !ok {
		return nil
	}
	meta := toolApprovalDataToMap(data)
	meta["pending_id"] = data.PendingID
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeMCPOAuthResolved,
		Content:   "MCP OAuth authorization resolved",
		Done:      true,
		Timestamp: time.Now(),
		Data:      meta,
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append mcp oauth resolved event failed", "error", err)
	}
	return nil
}

// handleReferences handles knowledge references events
func (h *AgentStreamHandler) handleReferences(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.AgentReferencesData)
	if !ok {
		return nil
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Extract knowledge references
	// Try to cast directly to []*types.SearchResult first
	var incoming []*types.SearchResult
	if searchResults, ok := data.References.([]*types.SearchResult); ok {
		incoming = append(incoming, searchResults...)
	} else if refs, ok := data.References.([]interface{}); ok {
		// Fallback: convert from []interface{}
		for _, ref := range refs {
			if sr, ok := ref.(*types.SearchResult); ok {
				incoming = append(incoming, sr)
			} else if refMap, ok := ref.(map[string]interface{}); ok {
				// Parse from map if needed
				incoming = append(incoming, searchResultFromMap(refMap))
			}
		}
	}
	incoming = videoevidence.EnrichReferences(incoming)
	videoevidence.MergeScopes(&h.evidenceScope, videoevidence.ScopeFromReferences(incoming))
	incoming = videoevidence.NormalizeReferences(h.evidenceScope, incoming)
	h.knowledgeRefs = videoevidence.MergeReferences(h.knowledgeRefs, incoming)
	projected, coverage := projectVideoEvidence(h.knowledgeRefs)

	// Update assistant message references
	h.assistantMessage.KnowledgeReferences = h.knowledgeRefs

	// Append references event to stream
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeReferences,
		Content:   "",
		Done:      false,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"references":     types.References(h.knowledgeRefs),
			"route_mode":     agentRouteMode(h.assistantMessage.AgentID),
			"coverage":       coverage,
			"video_evidence": projected,
		},
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append references event to stream failed", "error", err)
	}

	return nil
}

// handleMemoryRecalled records the long-term memories injected into this turn.
// The list is both persisted on the assistant message and streamed, so the
// panel is present live and after a reload.
func (h *AgentStreamHandler) handleMemoryRecalled(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.MemoryRecalledData)
	if !ok {
		return nil
	}
	used, ok := data.Memories.(types.UsedMemories)
	if !ok || len(used) == 0 {
		return nil
	}

	h.mu.Lock()
	h.assistantMessage.UsedMemories = used
	h.mu.Unlock()

	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeMemoryRecalled,
		Done:      false,
		Timestamp: time.Now(),
		Data:      map[string]interface{}{"memories": used},
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append memory recalled event to stream failed", "error", err)
	}
	return nil
}

// handleFinalAnswer handles final answer events
func (h *AgentStreamHandler) handleFinalAnswer(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.AgentFinalAnswerData)
	if !ok {
		return nil
	}

	h.mu.Lock()

	// Track start time on first chunk
	if _, exists := h.eventStartTimes[evt.ID]; !exists {
		h.eventStartTimes[evt.ID] = time.Now()
	}

	// Emit a one-shot TTFB log the first time *any* answer chunk reaches
	// the stream handler. This lets us compare the backend's "request in →
	// first token out" timing against the frontend-observed TTFB and pin
	// down where latency lives (network vs server vs LLM).
	if !h.ttfbLogged && !h.receivedAt.IsZero() {
		h.ttfbLogged = true
		ttfb := time.Since(h.receivedAt)
		logger.GetLogger(h.ctx).Infof("TTFB:first_answer_chunk request_id=%s, session_id=%s, ttfb_ms=%d",
			h.requestID, h.sessionID, ttfb.Milliseconds())
	}

	// Accumulate final answer locally for assistant message (database). Track
	// per event ID so a later supersede can subtract this segment's content.
	if data.Content != "" {
		seg := h.findAnswerSegment(evt.ID)
		if seg == nil {
			seg = &answerSegment{id: evt.ID}
			h.answerSegments = append(h.answerSegments, seg)
		}
		seg.content += data.Content
		h.finalAnswer = h.composeFinalAnswer()
	}
	if data.IsFallback {
		h.assistantMessage.IsFallback = true
	}

	// Calculate duration if done
	var metadata map[string]interface{}
	if data.Done {
		startTime := h.eventStartTimes[evt.ID]
		duration := time.Since(startTime)
		metadata = map[string]interface{}{
			"event_id":     evt.ID,
			"duration_ms":  duration.Milliseconds(),
			"completed_at": time.Now().Unix(),
		}
		delete(h.eventStartTimes, evt.ID)
	} else {
		metadata = map[string]interface{}{
			"event_id": evt.ID,
		}
	}
	if data.IsFallback {
		metadata["is_fallback"] = true
	}
	h.mu.Unlock()

	// Append this chunk to stream (frontend will accumulate by event ID)
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeAnswer,
		Content:   data.Content, // Just this chunk
		Done:      data.Done,
		Timestamp: time.Now(),
		Data:      metadata,
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append answer event to stream failed", "error", err)
	}

	return nil
}

// handleReflection handles agent reflection events
func (h *AgentStreamHandler) handleReflection(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.AgentReflectionData)
	if !ok {
		return nil
	}

	// Append this chunk to stream (frontend will accumulate by event ID)
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeReflection,
		Content:   data.Content, // Just this chunk
		Done:      data.Done,
		Timestamp: time.Now(),
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append reflection event to stream failed", "error", err)
	}

	return nil
}

// handleError handles error events
func (h *AgentStreamHandler) handleError(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.ErrorData)
	if !ok {
		return nil
	}

	// Build error metadata
	metadata := map[string]interface{}{
		"stage": data.Stage,
		"error": data.Error,
	}

	// Append error event to stream
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeError,
		Content:   data.Error,
		Done:      true,
		Timestamp: time.Now(),
		Data:      metadata,
	}); err != nil {
		logger.GetLogger(h.ctx).Error("Append error event to stream failed", "error", err)
	}

	return nil
}

// handleSessionTitle handles session title update events
func (h *AgentStreamHandler) handleSessionTitle(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.SessionTitleData)
	if !ok {
		return nil
	}

	// Use background context for title event since it may arrive after stream completion
	bgCtx := context.Background()

	// Append title event to stream
	if err := h.streamManager.AppendEvent(bgCtx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeSessionTitle,
		Content:   data.Title,
		Done:      true,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"session_id": data.SessionID,
			"title":      data.Title,
		},
	}); err != nil {
		logger.GetLogger(h.ctx).Warn("Append session title event to stream failed (stream may have ended)", "error", err)
	}

	return nil
}

// handleComplete handles agent complete events
func (h *AgentStreamHandler) handleComplete(ctx context.Context, evt event.Event) error {
	data, ok := evt.Data.(event.AgentCompleteData)
	if !ok {
		return nil
	}
	// A terminal failure must never be persisted as a completed empty
	// assistant message. The engine normally supplies a user-facing fallback;
	// keep this boundary defensive for other failure paths and older agents.
	if strings.TrimSpace(data.FinalAnswer) == "" && (data.Outcome == "failed" || data.FailureReason != "") {
		if data.FailureReason == "answer_contract_truncated" {
			data.FinalAnswer = "回答生成不完整，请重试。"
		} else if data.FailureReason == "missing_video_coverage" {
			data.FinalAnswer = "已找到部分视频证据，但尚未覆盖问题涉及的全部视频，请重试。"
		} else if data.FailureReason == "answer_contract_invalid" {
			data.FinalAnswer = "回答格式校验失败，请重试。"
		} else {
			data.FinalAnswer = "回答生成失败，请重试。"
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// Update assistant message with final data
	if data.MessageID == h.assistantMessageID {
		// h.assistantMessage.Content = data.FinalAnswer
		h.assistantMessage.IsCompleted = true
		h.assistantMessage.AgentDurationMs = data.TotalDurationMs

		// Update knowledge references if provided
		if len(data.KnowledgeRefs) > 0 {
			knowledgeRefs := make([]*types.SearchResult, 0, len(data.KnowledgeRefs))
			for _, ref := range data.KnowledgeRefs {
				if sr, ok := ref.(*types.SearchResult); ok {
					knowledgeRefs = append(knowledgeRefs, sr)
				} else if refMap, ok := ref.(map[string]interface{}); ok {
					knowledgeRefs = append(knowledgeRefs, searchResultFromMap(refMap))
				}
			}
			knowledgeRefs = videoevidence.EnrichReferences(knowledgeRefs)
			videoevidence.MergeScopes(&h.evidenceScope, videoevidence.ScopeFromReferences(knowledgeRefs))
			knowledgeRefs = videoevidence.NormalizeReferences(h.evidenceScope, knowledgeRefs)
			h.knowledgeRefs = videoevidence.MergeReferences(h.knowledgeRefs, knowledgeRefs)
			h.assistantMessage.KnowledgeReferences = h.knowledgeRefs
		}

		h.assistantMessage.Content += data.FinalAnswer

		// Update agent steps if provided
		if data.AgentSteps != nil {
			if steps, ok := data.AgentSteps.([]types.AgentStep); ok {
				h.assistantMessage.AgentSteps = agenttools.SanitizeAgentStepsForStorage(steps)
			}
		}

		// Drain skill-generated files from the sandbox into persistent
		// storage. Best-effort: any failure is logged and the turn is
		// persisted without artifacts. Collect is a no-op when either the
		// collector wasn't wired in, no sandbox is bound, or no files were
		// produced — those cases must not disturb the completion path.
		if h.artifactCollector != nil {
			collectCtx := context.WithoutCancel(h.ctx)
			artifacts, err := h.artifactCollector.Collect(
				collectCtx,
				h.sessionID,
				h.assistantMessageID,
				h.tenantID,
				skills.ArtifactOutputDir(),
			)
			if err != nil {
				logger.GetLogger(h.ctx).Warnf(
					"artifact collect failed session=%s message=%s: %v",
					h.sessionID, h.assistantMessageID, err,
				)
			} else if len(artifacts) > 0 {
				h.assistantMessage.Artifacts = artifacts
				logger.GetLogger(h.ctx).Infof(
					"artifact collect attached %d file(s) to message=%s session=%s",
					len(artifacts), h.assistantMessageID, h.sessionID,
				)
			}
		}
	}

	// Fallback: if no answer events were streamed but we have a final answer,
	// emit it as answer events so the frontend can render it properly.
	// This guards against edge cases where the LLM stops without calling final_answer.
	if h.finalAnswer == "" && data.FinalAnswer != "" {
		logger.GetLogger(h.ctx).Warnf(
			"No answer events were streamed, emitting fallback answer (len=%d). "+
				"This typically happens when: (1) model stopped naturally and content was sent as thought events, "+
				"or (2) Ollama model returned tool calls non-incrementally. "+
				"total_steps=%d, total_duration_ms=%d",
			len(data.FinalAnswer), data.TotalSteps, data.TotalDurationMs,
		)
		fallbackID := fmt.Sprintf("answer-fallback-%d", time.Now().UnixMilli())
		if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
			ID:        fallbackID,
			Type:      types.ResponseTypeAnswer,
			Content:   data.FinalAnswer,
			Done:      false,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"event_id":    fallbackID,
				"is_fallback": true,
			},
		}); err != nil {
			logger.GetLogger(h.ctx).Errorf("Append fallback answer event failed: %v", err)
		}
		if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
			ID:        fallbackID,
			Type:      types.ResponseTypeAnswer,
			Content:   "",
			Done:      true,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"event_id":    fallbackID,
				"is_fallback": true,
			},
		}); err != nil {
			logger.GetLogger(h.ctx).Errorf("Append fallback answer done event failed: %v", err)
		}
	}

	// Send completion event to stream manager so SSE can detect completion
	completeData := map[string]interface{}{
		"total_steps":       data.TotalSteps,
		"total_duration_ms": data.TotalDurationMs,
		"outcome":           data.Outcome,
		"route_mode":        agentRouteMode(h.assistantMessage.AgentID),
	}
	if h.route.AutoRoute {
		completeData["route_intent_status"] = h.route.IntentStatus
		completeData["route_intent"] = nil
		if h.route.Intent != "" && h.route.IntentStatus == "exposed" {
			completeData["route_intent"] = h.route.Intent
		}
		completeData["scope_hint"] = h.route.ScopeHint
		completeData["execution_scope"] = h.route.ExecutionScope
		completeData["execution_scope"] = h.route.ExecutionScope
		completeData["required_capabilities"] = h.route.RequiredCapabilities
		completeData["effective_scope_status"] = h.route.EffectiveScopeStatus
		completeData["effective_scope"] = nil
		if h.route.EffectiveScope != "" {
			completeData["effective_scope"] = h.route.EffectiveScope
		}
		completeData["route_schema_valid"] = h.route.SchemaValid
		if h.route.ErrorCode != "" {
			completeData["route_error_code"] = h.route.ErrorCode
		}
	}
	completeData["agent_id"] = h.assistantMessage.AgentID
	projected, coverage := projectVideoEvidence(h.knowledgeRefs)
	if data.Outcome == "failed" || data.FailureReason != "" {
		if len(projected) > 0 {
			coverage = "partial"
		} else {
			coverage = "none"
		}
	}
	completeData["coverage"] = coverage
	completeData["video_evidence"] = projected
	if data.FailureReason != "" {
		completeData["failure_reason"] = data.FailureReason
	}
	// Attach the freshly-collected artifacts so the frontend can render the
	// download button without waiting for a page refresh. We strip the
	// storage URL and any other server-only fields via publicArtifactViews
	// — clients only ever download through /artifacts/:index which enforces
	// tenant ownership.
	if len(h.assistantMessage.Artifacts) > 0 {
		completeData["artifacts"] = publicArtifactViews(h.assistantMessage.Artifacts)
	}
	if err := h.streamManager.AppendEvent(h.ctx, h.sessionID, h.assistantMessageID, interfaces.StreamEvent{
		ID:        evt.ID,
		Type:      types.ResponseTypeComplete,
		Content:   "",
		Done:      true,
		Timestamp: time.Now(),
		Data:      completeData,
	}); err != nil {
		logger.GetLogger(h.ctx).Errorf("Append complete event to stream failed: %v", err)
	}

	return nil
}

// publicArtifactViews returns a redacted view of the artifact list suitable
// for direct serialization onto the SSE stream. The storage URL and any
// other server-only fields are stripped; the frontend uses (index, name,
// size, source_path, mod_time, created_at) to render the download drawer
// and calls /artifacts/:index/download to fetch the bytes.
func publicArtifactViews(list types.MessageArtifacts) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(list))
	for i, a := range list {
		out = append(out, map[string]interface{}{
			"index":       i,
			"file_name":   a.FileName,
			"file_type":   a.FileType,
			"file_size":   a.FileSize,
			"source_path": a.SourcePath,
			"mod_time":    a.ModTime,
			"created_at":  a.CreatedAt,
		})
	}
	return out
}
