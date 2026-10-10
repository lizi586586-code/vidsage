package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/custom/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func classroomTest(t *testing.T) (*gorm.DB, *OpenMAICClassroomHandler, *gin.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.OpenMAICClassroom{}, &model.Video{}, &model.VideoTranscriptChunk{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/v1/auth/me":
			w.Write([]byte(`{"success":true,"data":{"user":{"id":"learner-1"}}}`))
		case "/api/v1/memory/settings":
			w.Write([]byte(`{"success":true,"data":{"effective":true}}`))
		case "/api/v1/memory/items":
			w.Write([]byte(`{"success":true,"total":2,"data":[{"kind":"preference","status":"active","content":"使用真实案例并慢速讲解"},{"kind":"fact","status":"active","content":"不应传出的私人事实"}]}`))
		case "/api/v1/knowledge/video-chunk":
			w.Write([]byte(`{"success":true,"data":{"id":"video-chunk","file_type":"txt","content":"MCP连接模型与工具，Skills封装可复用工作流。"}}`))
		case "/api/v1/knowledge/pdf-1":
			w.Write([]byte(`{"success":true,"data":{"id":"pdf-1","file_type":"pdf"}}`))
		case "/api/v1/knowledge/pdf-1/download":
			w.Write([]byte("%PDF-1.7\noriginal bytes"))
		default:
			w.WriteHeader(403)
		}
	}))
	t.Cleanup(native.Close)
	h := NewOpenMAICClassroomHandler(db, "bridge-secret", native.URL)
	router := gin.New()
	router.POST("/prepare", h.Prepare)
	router.POST("/:requestID/claim", h.Claim)
	router.POST("/sync", h.SyncStatus)
	router.GET("/status/:classroomID", h.Status)
	router.GET("/pending", h.Pending)
	return db, h, router
}
func classroomRequest(router *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	router.ServeHTTP(rec, req)
	return rec
}
func classroomRow(t *testing.T, db *gorm.DB, request, learner string) model.OpenMAICClassroom {
	t.Helper()
	row := model.OpenMAICClassroom{ID: uuid.NewString(), RequestID: request, LearnerID: learner, GenerationStatus: "queued", Status: "unknown"}
	require.NoError(t, db.Create(&row).Error)
	return row
}
func TestOpenMAICPreparedInputsUseOriginalPDFAndOwnActivePreferences(t *testing.T) {
	db, _, router := classroomTest(t)
	prepared := classroomRequest(router, "POST", "/prepare", `{"knowledge_ids":["pdf-1"],"requirement":"互动白板"}`, "user-token")
	require.Equal(t, 200, prepared.Code, prepared.Body.String())
	var result struct {
		Data model.OpenMAICClassroom `json:"data"`
	}
	require.NoError(t, json.Unmarshal(prepared.Body.Bytes(), &result))
	require.Equal(t, "learner-1", result.Data.LearnerID)
	claim := classroomRequest(router, "POST", "/"+result.Data.RequestID+"/claim", "{}", "bridge-secret")
	require.Equal(t, 200, claim.Code)
	var input struct {
		Data classroomContext `json:"data"`
	}
	require.NoError(t, json.Unmarshal(claim.Body.Bytes(), &input))
	require.True(t, input.Data.MemoryRead)
	require.Equal(t, 1, input.Data.MemoryCount)
	require.Contains(t, input.Data.Requirement, "慢速讲解")
	require.Contains(t, input.Data.Requirement, "中文配音")
	require.Equal(t, []string{"feynman-learning"}, input.Data.Teaching.Skills)
	require.Equal(t, "not_assessed", input.Data.Teaching.Ability)
	require.Equal(t, "slow", input.Data.Teaching.Pacing)
	require.True(t, input.Data.Teaching.Narration)
	require.NotContains(t, input.Data.Requirement, "私人事实")
	require.Equal(t, []byte("%PDF-1.7\noriginal bytes"), input.Data.Materials[0].Content)
	require.Contains(t, claim.Body.String(), base64.StdEncoding.EncodeToString(input.Data.Materials[0].Content))
	require.Equal(t, 410, classroomRequest(router, "POST", "/"+result.Data.RequestID+"/claim", "{}", "bridge-secret").Code)
	var stored model.OpenMAICClassroom
	require.NoError(t, db.First(&stored).Error)
	require.Equal(t, "submitting", stored.GenerationStatus)
}
func TestOpenMAICMultiplePendingTasksRemainIndependent(t *testing.T) {
	db, _, router := classroomTest(t)
	classroomRow(t, db, "req-1", "learner-1")
	classroomRow(t, db, "req-2", "learner-2")
	sync := classroomRequest(router, "POST", "/sync", `{"request_id":"req-1","job_id":"job-1","generation_status":"running","status":"unknown"}`, "bridge-secret")
	require.Equal(t, 200, sync.Code, sync.Body.String())
	var rows []model.OpenMAICClassroom
	require.NoError(t, db.Order("request_id").Find(&rows).Error)
	require.Equal(t, "job-1", rows[0].JobID)
	require.Empty(t, rows[1].JobID)
	require.Equal(t, 404, classroomRequest(router, "GET", "/status/req-2", "", "user-token").Code)
	require.Equal(t, 401, classroomRequest(router, "GET", "/status/req-1", "", "wrong").Code)
}
func TestOpenMAICLegacyTimeoutRecovery(t *testing.T) {
	db, _, router := classroomTest(t)
	for _, code := range []string{"generation_timeout", "generation_timeout_confirmed", "generation_timeout_unverified"} {
		classroomRow(t, db, code, "learner-1")
		require.NoError(t, db.Model(&model.OpenMAICClassroom{}).Where("request_id = ?", code).Updates(map[string]interface{}{"generation_status": "failed", "last_error_code": code, "job_id": "job-" + code}).Error)
	}
	pending := classroomRequest(router, "GET", "/pending", "", "bridge-secret")
	require.Equal(t, 200, pending.Code)
	require.Contains(t, pending.Body.String(), `"request_id":"generation_timeout"`)
	require.NotContains(t, pending.Body.String(), "generation_timeout_confirmed")
	require.NotContains(t, pending.Body.String(), "generation_timeout_unverified")
	for _, code := range []string{"generation_timeout", "generation_timeout_confirmed"} {
		body := `{"request_id":"` + code + `","job_id":"job-` + code + `","generation_status":"succeeded","classroom_id":"class-1","classroom_url":"https://open.maic.chat/classroom/class-1","status":"unknown"}`
		require.Equal(t, 200, classroomRequest(router, "POST", "/sync", body, "bridge-secret").Code)
		var row model.OpenMAICClassroom
		require.NoError(t, db.Where("request_id = ?", code).First(&row).Error)
		if code == "generation_timeout" {
			require.Equal(t, "succeeded", row.GenerationStatus)
			require.NotEmpty(t, row.ClassroomURL)
		} else {
			require.Equal(t, "failed", row.GenerationStatus)
			require.Empty(t, row.ClassroomURL)
		}
	}
}

func TestOpenMAICSyncStrictContractAndPublishingProtection(t *testing.T) {
	db, _, router := classroomTest(t)
	classroomRow(t, db, "req-1", "learner-1")
	valid := `{"request_id":"req-1","job_id":"job-1","generation_status":"succeeded","classroom_id":"class-1","classroom_url":"https://open.maic.chat/classroom/class-1","status":"unknown"}`
	cases := []struct {
		body string
		code int
	}{
		{`{`, 400}, {valid + ` {}`, 400}, {strings.TrimSuffix(valid, "}") + `,"unexpected":true}`, 400},
		{strings.Replace(valid, "class-1", "pending", -1), 400},
		{strings.Replace(valid, "https://open.maic.chat", "https://evil.example", 1), 400},
		{strings.Replace(valid, `"status":"unknown"`, `"status":"completed"`, 1), 422},
		{strings.Replace(valid, "req-1", "missing", 1), 404},
	}
	for _, test := range cases {
		rec := classroomRequest(router, "POST", "/sync", test.body, "bridge-secret")
		require.Equal(t, test.code, rec.Code, rec.Body.String())
	}
	var before model.OpenMAICClassroom
	require.NoError(t, db.First(&before).Error)
	require.Empty(t, before.ClassroomURL)
	require.Equal(t, 200, classroomRequest(router, "POST", "/sync", valid, "bridge-secret").Code)
	require.Equal(t, 200, classroomRequest(router, "POST", "/sync", valid, "bridge-secret").Code)
	require.Equal(t, 409, classroomRequest(router, "POST", "/sync", strings.Replace(valid, "job-1", "job-2", 1), "bridge-secret").Code)
	require.Equal(t, 409, classroomRequest(router, "POST", "/sync", strings.TrimSuffix(valid, "}")+`,"external_learner_id":"other"}`, "bridge-secret").Code)
	require.Equal(t, 200, classroomRequest(router, "POST", "/sync", `{"request_id":"req-1","job_id":"job-1","generation_status":"running","status":"unknown"}`, "bridge-secret").Code)
	var after model.OpenMAICClassroom
	require.NoError(t, db.First(&after).Error)
	require.Equal(t, "succeeded", after.GenerationStatus)
	require.Equal(t, "learner-1", after.LearnerID)
}
func TestOpenMAICBridgeAndSourcePermissions(t *testing.T) {
	_, h, router := classroomTest(t)
	require.Equal(t, 401, classroomRequest(router, "POST", "/sync", "{}", "wrong").Code)
	h.bridgeSecret = ""
	require.Equal(t, 503, classroomRequest(router, "POST", "/sync", "{}", "bridge-secret").Code)
	h.bridgeSecret = "bridge-secret"
	require.Equal(t, 403, classroomRequest(router, "POST", "/prepare", `{"knowledge_ids":["private"],"requirement":"course"}`, "user-token").Code)
	require.Equal(t, 400, classroomRequest(router, "POST", "/prepare", `{"knowledge_ids":["pdf-1"],"requirement":"course","learner_id":"other"}`, "user-token").Code)
}

func TestOpenMAICProgressContractPreservesVerifiedStatus(t *testing.T) {
	db, _, router := classroomTest(t)
	classroomRow(t, db, "req-progress", "learner-1")
	running := `{"request_id":"req-progress","job_id":"job-progress","generation_status":"running","status":"unknown"}`
	withProgress := func(value string) string { return strings.TrimSuffix(running, "}") + `,"progress":` + value + `}` }
	require.Equal(t, 200, classroomRequest(router, "POST", "/sync", withProgress("75"), "bridge-secret").Code)
	for _, invalid := range []string{"-1", "101", "2.5", `"80"`, "true"} {
		rec := classroomRequest(router, "POST", "/sync", withProgress(invalid), "bridge-secret")
		require.Equal(t, 400, rec.Code, rec.Body.String())
	}
	for _, body := range []string{running, withProgress("null")} {
		require.Equal(t, 200, classroomRequest(router, "POST", "/sync", body, "bridge-secret").Code)
	}
	var row model.OpenMAICClassroom
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, 75, *row.Progress)
	require.Empty(t, row.ClassroomURL)
	require.Contains(t, classroomRequest(router, "GET", "/status/req-progress", "", "user-token").Body.String(), `"progress":75`)
	succeeded := `{"request_id":"req-progress","job_id":"job-progress","generation_status":"succeeded","classroom_id":"class-progress","classroom_url":"https://open.maic.chat/classroom/class-progress","status":"unknown"}`
	require.Equal(t, 200, classroomRequest(router, "POST", "/sync", succeeded, "bridge-secret").Code)
	require.Equal(t, 200, classroomRequest(router, "POST", "/sync", withProgress("20"), "bridge-secret").Code)
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, 100, *row.Progress)
	require.Equal(t, "succeeded", row.GenerationStatus)
	require.NotEmpty(t, row.ClassroomURL)
}
func TestOpenMAICExpiredTransientContextFailsWithoutCreatingRemoteJob(t *testing.T) {
	db, h, router := classroomTest(t)
	row := classroomRow(t, db, "expired", "learner-1")
	h.contexts[row.RequestID] = classroomContext{Expires: time.Now().Add(-time.Minute)}
	require.Equal(t, 410, classroomRequest(router, "POST", "/expired/claim", "{}", "bridge-secret").Code)
	require.NoError(t, db.Model(&row).Update("updated_at", time.Now().Add(-20*time.Minute)).Error)
	require.Equal(t, 200, classroomRequest(router, "GET", "/pending", "", "bridge-secret").Code)
	require.NoError(t, db.First(&row).Error)
	require.Equal(t, "failed", row.GenerationStatus)
	require.Equal(t, "task_context_expired", row.LastErrorCode)
}

func TestOpenMAICVideoContextCarriesRealTranscriptAndTopic(t *testing.T) {
	db, _, router := classroomTest(t)
	video := model.Video{ID: "video-mcp", Title: "什么是MCP和Skills", TranscriptGeneration: "generation-2"}
	require.NoError(t, db.Create(&video).Error)
	chunk := model.VideoTranscriptChunk{VideoID: video.ID, Generation: "generation-2", KnowledgeID: "video-chunk", ChunkIndex: 0}
	require.NoError(t, db.Create(&chunk).Error)
	prepared := classroomRequest(router, "POST", "/prepare", `{"video_id":"video-mcp","requirement":"案例练习"}`, "user-token")
	require.Equal(t, 200, prepared.Code, prepared.Body.String())
	var task struct {
		Data model.OpenMAICClassroom `json:"data"`
	}
	require.NoError(t, json.Unmarshal(prepared.Body.Bytes(), &task))
	claim := classroomRequest(router, "POST", "/"+task.Data.RequestID+"/claim", "{}", "bridge-secret")
	var input struct {
		Data classroomContext `json:"data"`
	}
	require.NoError(t, json.Unmarshal(claim.Body.Bytes(), &input))
	require.Contains(t, input.Data.Requirement, "什么是MCP和Skills")
	require.Contains(t, input.Data.Requirement, "慢速讲解")
	require.Equal(t, "MCP连接模型与工具，Skills封装可复用工作流。", strings.TrimSpace(string(input.Data.Materials[0].Content)))
}
