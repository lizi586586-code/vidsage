package handler

import (
	"net/http"
	"strings"

	"github.com/Tencent/WeKnora/internal/custom/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ChatScopeHandler struct {
	db            *gorm.DB
	knowledgeKBID string
	evidenceKBID  string
	agentID       string
	tenantID      string
}

type ChatScopeResponse struct {
	Scope            string   `json:"scope"`
	VideoID          string   `json:"video_id,omitempty"`
	VideoTitle       string   `json:"video_title,omitempty"`
	VideoCoverURL    string   `json:"video_cover_url,omitempty"`
	AgentID          string   `json:"agent_id,omitempty"`
	TenantID         string   `json:"tenant_id,omitempty"`
	KnowledgeBaseIDs []string `json:"knowledge_base_ids"`
	KnowledgeIDs     []string `json:"knowledge_ids"`
	// ExecutionScope is the server-resolved access boundary. It never names
	// the videos a user may be semantically asking about; those are discovered
	// by WeKnora retrieval after routing.
	ExecutionScope  string            `json:"execution_scope"`
	ScopeResolution string            `json:"scope_resolution,omitempty"`
	SessionMeta     map[string]string `json:"session_meta"`
}

func NewChatScopeHandler(db *gorm.DB, kbID, agentID, tenantID string) *ChatScopeHandler {
	return NewChatScopeHandlerWithEvidence(db, kbID, "", agentID, tenantID)
}

// NewChatScopeHandlerWithEvidence binds both layers used by video Q&A:
// knowledge-layer documents for context and evidence-layer transcripts for
// clickable video locations.
func NewChatScopeHandlerWithEvidence(db *gorm.DB, knowledgeKBID, evidenceKBID, agentID, tenantID string) *ChatScopeHandler {
	return &ChatScopeHandler{
		db:            db,
		knowledgeKBID: strings.TrimSpace(knowledgeKBID),
		evidenceKBID:  strings.TrimSpace(evidenceKBID),
		agentID:       strings.TrimSpace(agentID),
		tenantID:      strings.TrimSpace(tenantID),
	}
}

func (h *ChatScopeHandler) knowledgeBaseIDs() []string {
	ids := make([]string, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, id := range []string{h.knowledgeKBID, h.evidenceKBID} {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func (h *ChatScopeHandler) sessionMeta(values map[string]string) map[string]string {
	if h.tenantID != "" {
		values["tenant_id"] = h.tenantID
	}
	return values
}

func (h *ChatScopeHandler) Global(c *gin.Context) {
	kbIDs := h.knowledgeBaseIDs()
	if len(kbIDs) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "global video knowledge base is not configured"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": ChatScopeResponse{
		Scope:            "global",
		AgentID:          h.agentID,
		TenantID:         h.tenantID,
		KnowledgeBaseIDs: kbIDs,
		KnowledgeIDs:     []string{},
		ExecutionScope:   "global_videos",
		ScopeResolution:  "global",
		SessionMeta: h.sessionMeta(map[string]string{
			"scope":           "global",
			"execution_scope": "global_videos",
		}),
	}})
}

// Query is retained as a compatibility endpoint for older clients. A query
// must never be used to resolve a semantic video set before routing: titles
// may be abbreviated, misspelled, or refer to multiple videos. The trusted
// boundary for a global question is therefore the same account-wide scope as
// Global; WeKnora discovers candidate videos during retrieval.
func (h *ChatScopeHandler) Query(c *gin.Context) {
	h.Global(c)
}

func (h *ChatScopeHandler) Video(c *gin.Context) {
	videoID := strings.TrimSpace(c.Param("id"))
	if videoID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "video id is required"})
		return
	}
	kbIDs := h.knowledgeBaseIDs()
	if len(kbIDs) == 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "global video knowledge base is not configured"})
		return
	}

	var video model.Video
	if err := h.db.WithContext(c.Request.Context()).First(&video, "id = ?", videoID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	knowledgeIDs, err := h.videoKnowledgeIDs(c, video)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if len(knowledgeIDs) == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "current video knowledge is not ready"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": ChatScopeResponse{
		Scope:            "video",
		VideoID:          video.ID,
		VideoTitle:       video.Title,
		VideoCoverURL:    video.ThumbnailURL,
		AgentID:          h.agentID,
		TenantID:         h.tenantID,
		KnowledgeBaseIDs: kbIDs,
		KnowledgeIDs:     knowledgeIDs,
		ExecutionScope:   "current_video",
		SessionMeta: h.sessionMeta(map[string]string{
			"scope":           "video",
			"execution_scope": "current_video",
			"video_id":        video.ID,
			"video_title":     video.Title,
			"video_cover_url": video.ThumbnailURL,
		}),
	}})
}

func (h *ChatScopeHandler) videoKnowledgeIDs(c *gin.Context, video model.Video) ([]string, error) {
	var chunks []model.VideoTranscriptChunk
	query := h.db.WithContext(c.Request.Context()).
		Where("video_id = ? AND status = ? AND TRIM(COALESCE(knowledge_id, '')) <> ''", video.ID, "completed")
	if strings.TrimSpace(video.TranscriptGeneration) != "" {
		query = query.Where("generation = ?", video.TranscriptGeneration)
	}
	if err := query.Order("chunk_index ASC").Find(&chunks).Error; err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(chunks)+1)
	ids := make([]string, 0, len(chunks)+1)
	for _, chunk := range chunks {
		id := strings.TrimSpace(chunk.KnowledgeID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		id := strings.TrimSpace(video.TranscriptKnowledgeID)
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
