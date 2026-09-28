package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	customweknora "github.com/Tencent/WeKnora/internal/custom/client/weknora"
	"github.com/Tencent/WeKnora/internal/custom/model"
	transcriptservice "github.com/Tencent/WeKnora/internal/custom/service/transcript"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type fakeChatEvidenceChunkResolver struct {
	chunks     map[string]customweknora.KnowledgeChunk
	knowledges map[string]customweknora.ManualKnowledgeResult
}

type fakeChatEvidenceWikiResolver struct {
	pages map[string]*customweknora.WikiPage
}

func (f fakeChatEvidenceWikiResolver) GetPageByID(_ context.Context, _ string, pageID string) (*customweknora.WikiPage, error) {
	page, ok := f.pages[pageID]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return page, nil
}

func (f fakeChatEvidenceChunkResolver) GetChunkByID(_ context.Context, chunkID string) (customweknora.KnowledgeChunk, error) {
	chunk, ok := f.chunks[chunkID]
	if !ok {
		return customweknora.KnowledgeChunk{}, gorm.ErrRecordNotFound
	}
	return chunk, nil
}

func (f fakeChatEvidenceChunkResolver) GetKnowledge(_ context.Context, knowledgeID string) (customweknora.ManualKnowledgeResult, error) {
	knowledge, ok := f.knowledges[knowledgeID]
	if !ok {
		return customweknora.ManualKnowledgeResult{}, gorm.ErrRecordNotFound
	}
	return knowledge, nil
}

func TestChatEvidenceAcceptsPostKnowledgeIDs(t *testing.T) {
	db := openTestVideoDB(t)
	if err := db.AutoMigrate(&model.VideoTranscriptChunk{}); err != nil {
		t.Fatalf("migrate chunks: %v", err)
	}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(
		http.MethodPost,
		"/api/custom/chat/evidence",
		bytes.NewBufferString(`{"knowledge_ids":["knowledge-1"]}`),
	)
	context.Request.Header.Set("Content-Type", "application/json")

	NewChatEvidenceHandler(db).Lookup(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestChatEvidenceMapsKnowledgeIDsToVideoSource(t *testing.T) {
	db := openTestVideoDB(t)
	if err := db.AutoMigrate(&model.VideoTranscriptChunk{}); err != nil {
		t.Fatalf("migrate chunks: %v", err)
	}
	video := model.Video{
		ID:                   uuid.NewString(),
		Title:                "来源视频",
		ThumbnailURL:         "https://cdn.example.com/cover.jpg",
		Status:               model.VideoStatusCompleted,
		TranscriptGeneration: "generation-1",
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	chunk := model.VideoTranscriptChunk{
		VideoID: video.ID, Generation: "generation-1", Revision: 1, ChunkIndex: 3,
		StartMs: 125000, EndMs: 130000, KnowledgeID: "knowledge-1", EvidenceSentenceID: "evs:1", ContentHash: "hash", Status: "completed",
	}
	if err := db.Create(&chunk).Error; err != nil {
		t.Fatalf("create chunk: %v", err)
	}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/custom/chat/evidence?knowledge_ids=knowledge-1", nil)

	NewChatEvidenceHandler(db).Lookup(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []ChatEvidenceItem `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Data) != 1 {
		t.Fatalf("data length = %d", len(payload.Data))
	}
	item := payload.Data[0]
	if item.KnowledgeID != "knowledge-1" || item.VideoID != video.ID || item.VideoTitle != video.Title || item.VideoCover == "" {
		t.Fatalf("unexpected evidence item: %#v", item)
	}
	if item.Seconds != 125 || item.StartSeconds != 125 || item.EndSeconds != 130 ||
		item.StartMs != 125000 || item.EndMs != 130000 || item.Timestamp != "02:05–02:10" {
		t.Fatalf("time = %#v, want 125/130000/02:05–02:10", item)
	}
	if item.EvidenceSentenceID != "evs:1" || item.TranscriptGeneration != "generation-1" ||
		item.SourceType != "transcript" || !item.Linkable {
		t.Fatalf("unexpected link contract: %#v", item)
	}
}

func TestChatEvidenceFallsBackToPublishedTranscriptWikiPage(t *testing.T) {
	db := openTestVideoDB(t)
	if err := db.AutoMigrate(&model.VideoTranscriptChunk{}); err != nil {
		t.Fatalf("migrate chunks: %v", err)
	}
	video := model.Video{
		ID:                       uuid.NewString(),
		Title:                    "有 Wiki 降级页的视频",
		Status:                   model.VideoStatusCompleted,
		TranscriptGeneration:     "generation-current",
		TranscriptPageWikiPageID: "wiki-transcript-1",
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	if err := db.Create(&model.VideoTranscriptChunk{
		VideoID: video.ID, Generation: "generation-old", Revision: 1, ChunkIndex: 1,
		StartMs: 1000, EndMs: 3000, KnowledgeID: "knowledge-old", EvidenceSentenceID: "evs:old",
		ContentHash: "hash-old", Status: "completed",
	}).Error; err != nil {
		t.Fatalf("create chunk: %v", err)
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/custom/chat/evidence?knowledge_ids=knowledge-old", nil)
	wiki := fakeChatEvidenceWikiResolver{pages: map[string]*customweknora.WikiPage{
		"wiki-transcript-1": {
			ID: "wiki-transcript-1", Slug: "video/example/transcript", Title: "有 Wiki 降级页的视频：转写",
			Status: "published", Content: "转写内容",
		},
	}}

	NewChatEvidenceHandlerWithWiki(db, nil, wiki, "knowledge-kb").Lookup(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []ChatEvidenceItem `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Data) != 1 {
		t.Fatalf("data length = %d, body = %s", len(payload.Data), recorder.Body.String())
	}
	item := payload.Data[0]
	if item.Linkable {
		t.Fatalf("stale video evidence must not be linkable: %#v", item)
	}
	if !item.WikiLinkable || item.WikiPageID != "wiki-transcript-1" ||
		item.WikiPageSlug != "video/example/transcript" || item.WikiPageTitle == "" ||
		item.WikiKnowledgeBaseID != "knowledge-kb" {
		t.Fatalf("published Wiki fallback missing: %#v", item)
	}
}

func TestChatEvidenceDoesNotExposeMissingWikiFallback(t *testing.T) {
	db := openTestVideoDB(t)
	if err := db.AutoMigrate(&model.VideoTranscriptChunk{}); err != nil {
		t.Fatalf("migrate chunks: %v", err)
	}
	video := model.Video{
		ID:                       uuid.NewString(),
		Title:                    "没有 Wiki 降级页的视频",
		Status:                   model.VideoStatusCompleted,
		TranscriptGeneration:     "generation-current",
		TranscriptPageWikiPageID: "missing-wiki-page",
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	if err := db.Create(&model.VideoTranscriptChunk{
		VideoID: video.ID, Generation: "generation-old", Revision: 1, ChunkIndex: 1,
		StartMs: 1000, EndMs: 3000, KnowledgeID: "knowledge-no-wiki", EvidenceSentenceID: "evs:no-wiki",
		ContentHash: "hash-no-wiki", Status: "completed",
	}).Error; err != nil {
		t.Fatalf("create chunk: %v", err)
	}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/custom/chat/evidence?knowledge_ids=knowledge-no-wiki", nil)

	NewChatEvidenceHandlerWithWiki(db, nil, fakeChatEvidenceWikiResolver{pages: map[string]*customweknora.WikiPage{}}, "knowledge-kb").Lookup(context)

	var payload struct {
		Data []ChatEvidenceItem `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].WikiLinkable {
		t.Fatalf("missing Wiki page must stay non-linkable: %#v", payload.Data)
	}
}

func TestChatEvidenceMapsSourceChunkIDsToVideoSource(t *testing.T) {
	db := openTestVideoDB(t)
	if err := db.AutoMigrate(&types.Chunk{}); err != nil {
		t.Fatalf("migrate source chunks: %v", err)
	}
	video := model.Video{
		ID:                   uuid.NewString(),
		Title:                "检索来源视频",
		Status:               model.VideoStatusCompleted,
		TranscriptGeneration: "generation-1",
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	transcriptChunk := model.VideoTranscriptChunk{
		VideoID: video.ID, Generation: "generation-1", Revision: 1, ChunkIndex: 8,
		StartMs: 76000, EndMs: 148000, KnowledgeID: "transcript-knowledge-8",
		EvidenceSentenceID: "evs:source-chunk", ContentHash: "hash-source", Status: "completed",
	}
	if err := db.Create(&transcriptChunk).Error; err != nil {
		t.Fatalf("create transcript chunk: %v", err)
	}
	sourceChunk := types.Chunk{
		ID:              "weknora-chunk-8",
		KnowledgeID:     transcriptChunk.KnowledgeID,
		KnowledgeBaseID: "evidence-kb",
		Content:         "四步万能提示词公式。",
		IsEnabled:       true,
	}
	if err := db.Create(&sourceChunk).Error; err != nil {
		t.Fatalf("create source chunk: %v", err)
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/custom/chat/evidence?knowledge_ids=weknora-chunk-8", nil)

	NewChatEvidenceHandler(db).Lookup(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []ChatEvidenceItem `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Data) != 1 {
		t.Fatalf("data length = %d, body = %s", len(payload.Data), recorder.Body.String())
	}
	item := payload.Data[0]
	if item.KnowledgeID != "weknora-chunk-8" || item.EvidenceSentenceID != "evs:source-chunk" || !item.Linkable {
		t.Fatalf("source chunk lookup did not produce linkable evidence: %#v", item)
	}
	if item.VideoID != video.ID || item.VideoTitle != video.Title || item.Timestamp != "01:16–02:28" {
		t.Fatalf("unexpected source chunk evidence item: %#v", item)
	}
}

func TestChatEvidenceMapsSourceChunkIDsAcrossDatabaseBoundary(t *testing.T) {
	db := openTestVideoDB(t)
	if err := db.AutoMigrate(&model.VideoTranscriptSource{}); err != nil {
		t.Fatalf("migrate transcript source: %v", err)
	}
	video := model.Video{
		ID:                   uuid.NewString(),
		Title:                "跨库检索来源视频",
		DurationSeconds:      61,
		Status:               model.VideoStatusCompleted,
		TranscriptGeneration: "generation-remote",
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	transcriptChunk := model.VideoTranscriptChunk{
		VideoID: video.ID, Generation: "generation-remote", Revision: 1, ChunkIndex: 3,
		StartMs: 46840, EndMs: 60840, KnowledgeID: "knowledge-remote",
		EvidenceSentenceID: "evs:remote", SourceSegmentID: "source-remote", SpeakerID: "speaker-1",
		ContentHash: "hash-remote", Status: "completed",
	}
	if err := db.Create(&transcriptChunk).Error; err != nil {
		t.Fatalf("create transcript chunk: %v", err)
	}
	sourceDocumentID := "source-document-remote"
	if err := db.Create(&model.VideoTranscriptSource{
		ID: "binding-remote", VideoID: video.ID, TranscriptGeneration: video.TranscriptGeneration,
		KnowledgeBaseID: "evidence-kb", KnowledgeID: sourceDocumentID, ContentHash: "source-hash", Status: "created",
	}).Error; err != nil {
		t.Fatalf("create transcript source: %v", err)
	}
	sourceText := "这是跨数据库来源分块中可唯一对应的完整证据原文，用来验证精确的视频时间定位。"
	document, err := transcriptservice.Build(transcriptservice.Input{
		VideoID: video.ID, TranscriptGeneration: video.TranscriptGeneration, Title: video.Title, DurationSeconds: video.DurationSeconds,
		Chapters: []transcriptservice.InputChapter{{
			Index: 0, Title: "第一章", Paragraphs: []transcriptservice.InputParagraph{{
				ParagraphID: "paragraph-1", Index: 0, SpeakerID: "speaker-1",
				Sentences: []transcriptservice.InputSentence{{
					SourceSentenceID: "source-remote", EvidenceSentenceID: "evs:remote", Text: sourceText,
					SpeakerID: "speaker-1", StartMs: 46840, EndMs: 60840,
				}},
			}},
		}},
	})
	if err != nil {
		t.Fatalf("build transcript source document: %v", err)
	}
	resolver := fakeChatEvidenceChunkResolver{
		chunks: map[string]customweknora.KnowledgeChunk{
			"source-chunk-remote": {ID: "source-chunk-remote", KnowledgeID: sourceDocumentID, Content: string([]rune(sourceText)[5:])},
		},
		knowledges: map[string]customweknora.ManualKnowledgeResult{
			sourceDocumentID: {ID: sourceDocumentID, Content: transcriptservice.SourceContent(document, "", "source-hash")},
		},
	}
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/custom/chat/evidence?knowledge_ids=source-chunk-remote", nil)

	NewChatEvidenceHandler(db, resolver).Lookup(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []ChatEvidenceItem `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].KnowledgeID != "source-chunk-remote" || !payload.Data[0].Linkable {
		t.Fatalf("cross-database source chunk lookup failed: %#v", payload.Data)
	}
	if payload.Data[0].EvidenceSentenceID != "evs:remote" || payload.Data[0].Timestamp != "00:46–01:00" {
		t.Fatalf("unexpected cross-database evidence item: %#v", payload.Data[0])
	}
}

func TestUniqueEvidenceForSourceChunkFailsClosedOnAmbiguousOrShortContent(t *testing.T) {
	document := transcriptservice.FullVideoDocument{Chapters: []transcriptservice.Chapter{{
		Paragraphs: []transcriptservice.Paragraph{{TimeMarks: []transcriptservice.TimeMark{
			{EvidenceSentenceID: "evs:1", Text: "相同的检索片段足够长，但不能唯一决定视频证据句。"},
			{EvidenceSentenceID: "evs:2", Text: "另一句也包含相同的检索片段足够长，但不能唯一决定视频证据句。"},
			{EvidenceSentenceID: "evs:3", Text: "原理靠固定的提示词来设定角色和任务，每次启动自动加载。"},
		}},
		}}}}
	if id, ok := uniqueEvidenceForSourceChunk(document, "相同的检索片段足够长，但不能唯一决定视频证据句。"); ok || id != "" {
		t.Fatalf("ambiguous source chunk resolved to %q", id)
	}
	if id, ok := uniqueEvidenceForSourceChunk(document, "片段太短"); ok || id != "" {
		t.Fatalf("short source chunk resolved to %q", id)
	}
	escapedJSONFragment := `,{\"text\":\"原理靠固定的提示词来设定角色和任务，每次启动自动加载`
	if id, ok := uniqueEvidenceForSourceChunk(document, escapedJSONFragment); !ok || id != "evs:3" {
		t.Fatalf("escaped JSON source chunk resolved to (%q, %v), want (evs:3, true)", id, ok)
	}
}

func TestMatchingEvidenceForSourceChunkKeepsContiguousEvidenceOrder(t *testing.T) {
	document := transcriptservice.FullVideoDocument{Chapters: []transcriptservice.Chapter{{
		Paragraphs: []transcriptservice.Paragraph{{TimeMarks: []transcriptservice.TimeMark{
			{EvidenceSentenceID: "evs:1", Text: "第一条连续证据句足够长，可以安全匹配。"},
			{EvidenceSentenceID: "evs:2", Text: "第二条连续证据句足够长，可以安全匹配。"},
			{EvidenceSentenceID: "evs:3", Text: "第三条不在来源分块里的证据句。"},
		}},
		}}}}
	ids := matchingEvidenceForSourceChunk(document, "第一条连续证据句足够长，可以安全匹配。第二条连续证据句足够长，可以安全匹配。")
	if len(ids) != 2 || ids[0] != "evs:1" || ids[1] != "evs:2" {
		t.Fatalf("matching evidence IDs = %#v, want [evs:1 evs:2]", ids)
	}
}

func TestChatEvidenceMapsEvidenceSentenceIDsToVideoSource(t *testing.T) {
	db := openTestVideoDB(t)
	if err := db.AutoMigrate(&model.Video{}, &model.VideoTranscriptChunk{}); err != nil {
		t.Fatalf("migrate video evidence: %v", err)
	}
	video := model.Video{
		ID:                   uuid.NewString(),
		Title:                "证据视频",
		Status:               model.VideoStatusCompleted,
		TranscriptGeneration: "generation-1",
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	chunk := model.VideoTranscriptChunk{
		VideoID: video.ID, Generation: "generation-1", Revision: 1, ChunkIndex: 4,
		StartMs: 210000, EndMs: 218000, KnowledgeID: "knowledge-4",
		EvidenceSentenceID: "evs:4", ContentHash: "hash-4", Status: "completed",
	}
	if err := db.Create(&chunk).Error; err != nil {
		t.Fatalf("create chunk: %v", err)
	}
	var stored model.VideoTranscriptChunk
	if err := db.Where("evidence_sentence_id = ?", "evs:4").First(&stored).Error; err != nil {
		t.Fatalf("lookup fixture by evidence sentence: %v", err)
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/custom/chat/evidence?knowledge_ids=evs:4", nil)

	NewChatEvidenceHandler(db).Lookup(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []ChatEvidenceItem `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].EvidenceSentenceID != "evs:4" || !payload.Data[0].Linkable {
		t.Fatalf("unexpected evidence item: %#v", payload.Data)
	}
}

func TestChatEvidenceRejectsStaleOrUnpublishedTranscriptLocation(t *testing.T) {
	db := openTestVideoDB(t)
	if err := db.AutoMigrate(&model.Video{}, &model.VideoTranscriptChunk{}); err != nil {
		t.Fatalf("migrate video evidence: %v", err)
	}
	video := model.Video{
		ID:                   uuid.NewString(),
		Title:                "当前视频",
		Status:               model.VideoStatusCompleted,
		TranscriptGeneration: "generation-current",
	}
	if err := db.Create(&video).Error; err != nil {
		t.Fatalf("create video: %v", err)
	}
	for _, chunk := range []model.VideoTranscriptChunk{
		{
			VideoID: video.ID, Generation: "generation-old", ChunkIndex: 0,
			StartMs: 1000, EndMs: 2000, KnowledgeID: "stale",
			EvidenceSentenceID: "old-evidence", ContentHash: "old", Status: "completed",
		},
		{
			VideoID: video.ID, Generation: "generation-current", ChunkIndex: 1,
			StartMs: 3000, EndMs: 5000, KnowledgeID: "pending",
			EvidenceSentenceID: "pending-evidence", ContentHash: "pending", Status: "created",
		},
	} {
		if err := db.Create(&chunk).Error; err != nil {
			t.Fatalf("create chunk %s: %v", chunk.KnowledgeID, err)
		}
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/custom/chat/evidence?knowledge_ids=stale,pending", nil)

	NewChatEvidenceHandler(db).Lookup(context)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []ChatEvidenceItem `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(payload.Data) != 2 || payload.Data[0].Linkable || payload.Data[1].Linkable {
		t.Fatalf("stale or unpublished evidence must be non-linkable: %#v", payload.Data)
	}
}
