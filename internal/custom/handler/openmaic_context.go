package handler

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/custom/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

//go:embed prompts/openmaic-review.txt
var openMAICReviewPrompt string

func compileOpenMAICReviewPrompt(courseTitle, requirement string, preferences []string) string {
	return strings.NewReplacer(
		"{{course_title}}", courseTitle,
		"{{review_requirement}}", classroomEmailPattern.ReplaceAllString(requirement, "[已隐藏联系方式]"),
		"{{teaching_preferences}}", strings.Join(preferences, "；"),
	).Replace(openMAICReviewPrompt)
}

type classroomContext struct {
	Requirement string                `json:"requirement"`
	Materials   []classroomMaterial   `json:"materials"`
	MemoryRead  bool                  `json:"memory_read"`
	MemoryCount int                   `json:"memory_count"`
	Teaching    classroomTeachingPlan `json:"teaching_plan"`
	Expires     time.Time             `json:"-"`
}
type classroomMaterial struct {
	KnowledgeID string `json:"knowledge_id,omitempty"`
	FileName    string `json:"file_name"`
	MIMEType    string `json:"mime_type"`
	Content     []byte `json:"content_base64"`
}

var classroomReferencePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var classroomEmailPattern = regexp.MustCompile(`(?i)[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}`)

func classroomError(c *gin.Context, status int, code string) {
	c.JSON(status, gin.H{"success": false, "error": code})
}
func (h *OpenMAICClassroomHandler) native(c *gin.Context, path string, out any) error {
	if h.nativeURL == "" || !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") {
		return errors.New("unauthorized")
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), "GET", h.nativeURL+"/api/v1"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.GetHeader("Authorization"))
	req.Header.Set("X-Tenant-ID", c.GetHeader("X-Tenant-ID"))
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return errors.New("native_read_denied")
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 32*1024*1024)).Decode(out)
}
func (h *OpenMAICClassroomHandler) learner(c *gin.Context) (string, bool) {
	var me struct {
		Success bool `json:"success"`
		Data    struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"data"`
	}
	if h.native(c, "/auth/me", &me) != nil || !me.Success || me.Data.User.ID == "" {
		classroomError(c, 401, "unauthorized")
		return "", false
	}
	return me.Data.User.ID, true
}

// Prepare authorizes each source and reads the caller's active teaching preferences.
// The business database stores references only; input bytes expire in memory.
func (h *OpenMAICClassroomHandler) Prepare(c *gin.Context) {
	learner, ok := h.learner(c)
	if !ok {
		return
	}
	if h.bridgeSecret == "" {
		classroomError(c, 503, "status_sync_unavailable")
		return
	}
	var in struct {
		VideoID      string   `json:"video_id"`
		KnowledgeIDs []string `json:"knowledge_ids"`
		Requirement  string   `json:"requirement"`
	}
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&in) != nil || d.Decode(&extra) != io.EOF || len(in.Requirement) > 4000 || len(in.KnowledgeIDs) > 5 {
		classroomError(c, 400, "invalid_request")
		return
	}
	ids := append([]string(nil), in.KnowledgeIDs...)
	courseTitle := ""
	if in.VideoID != "" {
		var video model.Video
		if h.db.WithContext(c.Request.Context()).First(&video, "id = ?", in.VideoID).Error != nil {
			classroomError(c, 404, "video_not_found")
			return
		}
		courseTitle = video.Title
		var chunks []model.VideoTranscriptChunk
		if h.db.WithContext(c.Request.Context()).Where("video_id = ? AND generation = ?", video.ID, video.TranscriptGeneration).Order("chunk_index").Find(&chunks).Error != nil {
			classroomError(c, 503, "source_read_failed")
			return
		}
		for _, chunk := range chunks {
			if chunk.KnowledgeID != "" {
				ids = append(ids, chunk.KnowledgeID)
			}
		}
		if len(chunks) == 0 && video.TranscriptKnowledgeID != "" {
			ids = append(ids, video.TranscriptKnowledgeID)
		}
	}
	if len(ids) == 0 || len(ids) > 100 {
		classroomError(c, 400, "course_source_required")
		return
	}
	var settings struct {
		Success bool `json:"success"`
		Data    struct {
			Effective bool `json:"effective"`
		} `json:"data"`
	}
	if h.native(c, "/memory/settings", &settings) != nil || !settings.Success {
		classroomError(c, 503, "memory_read_failed")
		return
	}
	context := classroomContext{MemoryRead: true, Expires: time.Now().Add(15 * time.Minute)}
	preferences := []string{}
	if settings.Data.Effective {
		var memory struct {
			Success bool `json:"success"`
			Total   int  `json:"total"`
			Data    []struct {
				Kind    string `json:"kind"`
				Status  string `json:"status"`
				Content string `json:"content"`
			} `json:"data"`
		}
		if h.native(c, "/memory/items?status=active&limit=100&offset=0", &memory) != nil || !memory.Success || memory.Total > 100 {
			classroomError(c, 503, "memory_read_failed")
			return
		}
		for _, m := range memory.Data {
			if m.Status == "active" && m.Kind == "preference" {
				preferences = append(preferences, classroomEmailPattern.ReplaceAllString(m.Content, "[已隐藏联系方式]"))
				context.MemoryCount++
			}
		}
	}
	text := strings.Builder{}
	seen := map[string]bool{}
	for _, id := range ids {
		if !classroomReferencePattern.MatchString(id) {
			classroomError(c, 400, "invalid_source_reference")
			return
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		var source struct {
			Success bool `json:"success"`
			Data    struct {
				ID       string         `json:"id"`
				FileType string         `json:"file_type"`
				Content  string         `json:"content"`
				Metadata map[string]any `json:"metadata"`
			} `json:"data"`
		}
		if h.native(c, "/knowledge/"+url.PathEscape(id), &source) != nil || !source.Success || source.Data.ID != id {
			classroomError(c, 403, "source_read_denied")
			return
		}
		if strings.EqualFold(source.Data.FileType, "pdf") {
			req, _ := http.NewRequestWithContext(c.Request.Context(), "GET", h.nativeURL+"/api/v1/knowledge/"+url.PathEscape(id)+"/download", nil)
			req.Header.Set("Authorization", c.GetHeader("Authorization"))
			req.Header.Set("X-Tenant-ID", c.GetHeader("X-Tenant-ID"))
			resp, err := h.client.Do(req)
			if err != nil {
				classroomError(c, 503, "source_read_failed")
				return
			}
			data, err := io.ReadAll(io.LimitReader(resp.Body, 30*1024*1024+1))
			resp.Body.Close()
			if err != nil || resp.StatusCode != 200 || len(data) > 30*1024*1024 || !strings.HasPrefix(string(data), "%PDF-") || len(context.Materials) >= 4 {
				classroomError(c, 422, "original_material_invalid")
				return
			}
			context.Materials = append(context.Materials, classroomMaterial{KnowledgeID: id, FileName: "course.pdf", MIMEType: "application/pdf", Content: data})
		} else {
			content := source.Data.Content
			if content == "" {
				content, _ = source.Data.Metadata["content"].(string)
			}
			if strings.TrimSpace(content) == "" {
				classroomError(c, 422, "course_source_empty")
				return
			}
			text.WriteString("\n")
			text.WriteString(content)
			if text.Len() > 30*1024*1024 {
				classroomError(c, 422, "course_source_too_large")
				return
			}
		}
	}
	if text.Len() > 0 {
		context.Materials = append(context.Materials, classroomMaterial{FileName: "course.txt", MIMEType: "text/plain", Content: []byte(text.String())})
	}
	context.Teaching = recommendClassroomTeaching(in.Requirement, preferences)
	context.Requirement = compileOpenMAICReviewPrompt(courseTitle, in.Requirement, preferences)
	if len(context.Requirement) > 8000 {
		classroomError(c, 422, "preferences_too_large")
		return
	}
	row := model.OpenMAICClassroom{ID: uuid.NewString(), RequestID: uuid.NewString(), LearnerID: learner, GenerationStatus: "queued", Status: "unknown"}
	references, _ := json.Marshal(ids)
	row.SourceReferences = string(references)
	row.MemoryRead = context.MemoryRead
	row.MemoryCount = context.MemoryCount
	if h.db.WithContext(c.Request.Context()).Create(&row).Error != nil {
		classroomError(c, 503, "task_create_failed")
		return
	}
	h.mu.Lock()
	for key, value := range h.contexts {
		if time.Now().After(value.Expires) {
			delete(h.contexts, key)
		}
	}
	h.contexts[row.RequestID] = context
	h.mu.Unlock()
	c.JSON(200, gin.H{"success": true, "data": row})
}

// Claim is atomic: repeated Skill calls cannot create a second remote classroom.
func (h *OpenMAICClassroomHandler) Claim(c *gin.Context) {
	if !h.authorized(c) {
		return
	}
	id := c.Param("requestID")
	h.mu.Lock()
	defer h.mu.Unlock()
	context, ok := h.contexts[id]
	if !ok || time.Now().After(context.Expires) {
		delete(h.contexts, id)
		classroomError(c, 410, "task_context_expired")
		return
	}
	result := h.db.WithContext(c.Request.Context()).Model(&model.OpenMAICClassroom{}).Where("request_id = ? AND generation_status = 'queued'", id).Update("generation_status", "submitting")
	if result.Error != nil || result.RowsAffected != 1 {
		classroomError(c, 409, "task_already_claimed")
		return
	}
	delete(h.contexts, id)
	c.JSON(200, gin.H{"success": true, "data": context})
}
