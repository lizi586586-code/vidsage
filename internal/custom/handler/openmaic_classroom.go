package handler

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/Tencent/WeKnora/internal/custom/model"
)

type OpenMAICClassroomHandler struct {
	db           *gorm.DB
	bridgeSecret string
	nativeURL    string
	client       *http.Client
	mu           sync.Mutex
	contexts     map[string]classroomContext
}

type openMAICClassroomStatusRequest struct {
	RequestID        string `json:"request_id"`
	LearnerID        string `json:"external_learner_id"`
	JobID            string `json:"job_id"`
	GenerationStatus string `json:"generation_status"`
	Progress         *int   `json:"progress"`
	ClassroomID      string `json:"classroom_id"`
	ClassroomURL     string `json:"classroom_url"`
	Status           string `json:"status" binding:"required"`
	CompletedAt      string `json:"completed_at"`
	CompletionSource string `json:"completion_source"`
	EvidenceVersion  string `json:"evidence_version"`
	IdempotencyKey   string `json:"idempotency_key" binding:"required"`
	ErrorCode        string `json:"error_code"`
}

func NewOpenMAICClassroomHandler(db *gorm.DB, bridgeSecret string, nativeURL ...string) *OpenMAICClassroomHandler {
	h := &OpenMAICClassroomHandler{db: db, bridgeSecret: strings.TrimSpace(bridgeSecret), contexts: make(map[string]classroomContext), client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	if len(nativeURL) > 0 {
		h.nativeURL = strings.TrimSuffix(strings.TrimRight(nativeURL[0], "/"), "/api/v1")
	}
	return h
}

func (h *OpenMAICClassroomHandler) Status(c *gin.Context) {
	learner, ok := h.learner(c)
	if !ok {
		return
	}
	classroomID := strings.TrimSpace(c.Param("classroomID"))
	if classroomID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "classroom_id_required"})
		return
	}
	var classroom model.OpenMAICClassroom
	if err := h.db.WithContext(c.Request.Context()).Where("learner_id = ? AND (classroom_id = ? OR request_id = ?)", learner, classroomID, classroomID).First(&classroom).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "classroom_not_found"})
			return
		}
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "no such table") || strings.Contains(message, "does not exist") {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "classroom_status_unavailable"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "classroom_status_read_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": classroom})
}

// Cancel commits the terminal state before discarding inputs, under Claim's mutex.
func (h *OpenMAICClassroomHandler) Cancel(c *gin.Context) {
	learner, ok := h.learner(c)
	if !ok {
		return
	}
	var input *struct{}
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 1024))
	d.DisallowUnknownFields()
	var extra any
	if err := d.Decode(&input); err != nil || input == nil {
		c.JSON(400, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	if d.Decode(&extra) != io.EOF {
		c.JSON(400, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	requestID := strings.TrimSpace(c.Param("requestID"))
	var classroom model.OpenMAICClassroom
	h.mu.Lock()
	defer h.mu.Unlock()
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("learner_id = ? AND request_id = ?", learner, requestID).First(&classroom).Error; err != nil {
			return err
		}
		switch classroom.GenerationStatus {
		case "cancelled":
			return nil
		case "queued", "submitting", "running":
			classroom.GenerationStatus = "cancelled"
			classroom.LastErrorCode = "user_cancelled"
			classroom.ClassroomID = ""
			classroom.ClassroomURL = ""
			return tx.Save(&classroom).Error
		default:
			return errors.New("task_not_cancellable")
		}
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(404, gin.H{"success": false, "error": "classroom_not_found"})
		case err.Error() == "task_not_cancellable":
			c.JSON(409, gin.H{"success": false, "error": "task_not_cancellable"})
		default:
			c.JSON(500, gin.H{"success": false, "error": "cancel_request_failed"})
		}
		return
	}
	delete(h.contexts, requestID)
	c.JSON(200, gin.H{"success": true, "data": classroom})
}

// Pending is only for the bridge reconciliation loop. It makes a restart
// recover outstanding remote jobs without exposing them to browsers.
func (h *OpenMAICClassroomHandler) Pending(c *gin.Context) {
	if !h.authorized(c) {
		return
	}
	var classrooms []model.OpenMAICClassroom
	h.db.WithContext(c.Request.Context()).Model(&model.OpenMAICClassroom{}).Where("(generation_status = 'queued' AND updated_at < ?) OR (generation_status = 'submitting' AND updated_at < ?)", time.Now().Add(-15*time.Minute), time.Now().Add(-time.Hour)).Updates(map[string]any{"generation_status": "failed", "last_error_code": "task_context_expired"})
	if err := h.db.WithContext(c.Request.Context()).Where("generation_status = 'running' OR (generation_status = 'failed' AND last_error_code = 'generation_timeout' AND job_id <> '')").Order("updated_at ASC").Limit(100).Find(&classrooms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "classroom_status_read_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": classrooms})
}

func (h *OpenMAICClassroomHandler) authorized(c *gin.Context) bool {
	if h.bridgeSecret == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "status_sync_unavailable"})
		return false
	}
	providedSecret := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
	if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") || len(providedSecret) != len(h.bridgeSecret) || subtle.ConstantTimeCompare([]byte(providedSecret), []byte(h.bridgeSecret)) != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized"})
		return false
	}
	return true
}

// SyncStatus accepts only a server-to-server update from the configured
// bridge. The bridge token is intentionally not accepted from the browser.
func (h *OpenMAICClassroomHandler) SyncStatus(c *gin.Context) {
	if !h.authorized(c) {
		return
	}
	var input openMAICClassroomStatusRequest
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024))
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		c.JSON(400, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	input.ClassroomID = strings.TrimSpace(input.ClassroomID)
	input.ClassroomURL = strings.TrimSpace(input.ClassroomURL)
	input.JobID = strings.TrimSpace(input.JobID)
	input.GenerationStatus = strings.TrimSpace(input.GenerationStatus)
	input.Status = strings.TrimSpace(input.Status)
	input.LearnerID = strings.TrimSpace(input.LearnerID)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	if input.IdempotencyKey == "" {
		input.IdempotencyKey = input.RequestID
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(input.IdempotencyKey) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_request"})
		return
	}
	if input.GenerationStatus != "running" && input.GenerationStatus != "succeeded" && input.GenerationStatus != "failed" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_generation_status"})
		return
	}
	if input.Progress != nil && (*input.Progress < 0 || *input.Progress > 100) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_progress"})
		return
	}
	u, urlErr := url.Parse(input.ClassroomURL)
	if input.GenerationStatus == "succeeded" && (urlErr != nil || input.ClassroomID == "pending" || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(input.ClassroomID) || u.Scheme != "https" || u.Host != "open.maic.chat" || u.User != nil || u.Path != "/classroom/"+input.ClassroomID || u.RawQuery != "" || u.Fragment != "") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "classroom_result_required"})
		return
	}
	if input.GenerationStatus == "running" && !regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(input.JobID) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "job_id_required"})
		return
	}
	if input.Status == "completed" {
		c.JSON(422, gin.H{"success": false, "error": "whole_course_evidence_unavailable"})
		return
	}
	if input.ErrorCode != "" && !regexp.MustCompile(`^[a-z0-9_]{1,64}$`).MatchString(input.ErrorCode) {
		c.JSON(400, gin.H{"success": false, "error": "invalid_error_code"})
		return
	}
	if input.Status != "not_started" && input.Status != "in_progress" && input.Status != "completed" && input.Status != "unknown" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_status"})
		return
	}
	var completedAt *time.Time
	if strings.TrimSpace(input.CompletedAt) != "" {
		parsed, err := time.Parse(time.RFC3339, input.CompletedAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "invalid_completed_at"})
			return
		}
		completedAt = &parsed
	}
	var classroom model.OpenMAICClassroom
	err := h.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		result := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("request_id = ?", input.IdempotencyKey).First(&classroom)
		if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
			return result.Error
		}
		if result.Error == gorm.ErrRecordNotFound {
			return gorm.ErrRecordNotFound
		} else {
			if classroom.GenerationStatus == "cancelled" {
				return nil
			}
			if input.LearnerID != "" && input.LearnerID != classroom.LearnerID || classroom.JobID != "" && input.JobID != "" && input.JobID != classroom.JobID || classroom.ClassroomID != "" && input.ClassroomID != "" && input.ClassroomID != classroom.ClassroomID {
				return errors.New("classroom_binding_conflict")
			}
			recoverableTimeout := classroom.GenerationStatus == "failed" && classroom.LastErrorCode == "generation_timeout" && classroom.JobID != ""
			if classroom.GenerationStatus == "failed" && !recoverableTimeout || classroom.GenerationStatus == "succeeded" && input.GenerationStatus != "succeeded" {
				return nil
			}
			if classroom.Status == "completed" && input.Status != "completed" {
				return nil
			}
			if input.JobID != "" {
				classroom.JobID = input.JobID
			}
			classroom.GenerationStatus = input.GenerationStatus
			// Missing/null progress preserves the last verified percentage.
			if input.Progress != nil {
				classroom.Progress = input.Progress
			}
			if input.GenerationStatus == "succeeded" {
				progress := 100 // Only reached after classroom URL validation.
				classroom.Progress = &progress
			}
			if input.ClassroomID != "" {
				classroom.ClassroomID = input.ClassroomID
			}
			if input.ClassroomURL != "" {
				classroom.ClassroomURL = input.ClassroomURL
			}
			classroom.Status = input.Status
			classroom.LastErrorCode = input.ErrorCode
		}
		classroom.CompletionSource = strings.TrimSpace(input.CompletionSource)
		classroom.EvidenceVersion = strings.TrimSpace(input.EvidenceVersion)
		classroom.CompletedAt = completedAt
		return tx.Save(&classroom).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(404, gin.H{"success": false, "error": "task_not_found"})
			return
		}
		if err.Error() == "classroom_binding_conflict" {
			c.JSON(409, gin.H{"success": false, "error": "classroom_binding_conflict"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "status_sync_failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": classroom})
}
