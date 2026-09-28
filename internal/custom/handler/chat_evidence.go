package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/custom/client/weknora"
	"github.com/Tencent/WeKnora/internal/custom/model"
	transcriptservice "github.com/Tencent/WeKnora/internal/custom/service/transcript"
	"github.com/Tencent/WeKnora/internal/videoevidence"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type ChatEvidenceHandler struct {
	db                  *gorm.DB
	sourceChunkResolver chatEvidenceChunkResolver
	wikiResolver        chatEvidenceWikiResolver
	wikiKBID            string
}

type chatEvidenceChunkResolver interface {
	GetChunkByID(ctx context.Context, chunkID string) (weknora.KnowledgeChunk, error)
	GetKnowledge(ctx context.Context, knowledgeID string) (weknora.ManualKnowledgeResult, error)
}

type chatEvidenceWikiResolver interface {
	GetPageByID(ctx context.Context, kbID, pageID string) (*weknora.WikiPage, error)
}

type ChatEvidenceItem struct {
	KnowledgeID          string `json:"knowledge_id"`
	VideoID              string `json:"video_id"`
	VideoTitle           string `json:"video_title"`
	VideoCover           string `json:"video_cover_url"`
	StartMs              int    `json:"start_ms"`
	EndMs                int    `json:"end_ms"`
	StartSeconds         int    `json:"start_seconds"`
	EndSeconds           int    `json:"end_seconds"`
	Seconds              int    `json:"seconds"` // legacy alias for start_seconds
	Timestamp            string `json:"timestamp"`
	EvidenceSentenceID   string `json:"evidence_sentence_id"`
	TranscriptGeneration string `json:"transcript_generation"`
	SourceType           string `json:"source_type"`
	Linkable             bool   `json:"linkable"`
	WikiPageID           string `json:"wiki_page_id,omitempty"`
	WikiPageSlug         string `json:"wiki_page_slug,omitempty"`
	WikiPageTitle        string `json:"wiki_page_title,omitempty"`
	WikiKnowledgeBaseID  string `json:"wiki_knowledge_base_id,omitempty"`
	WikiLinkable         bool   `json:"wiki_linkable"`
	ErrorCode            string `json:"error_code,omitempty"`
}

type chatEvidenceRequest struct {
	KnowledgeIDs []string `json:"knowledge_ids"`
}

func NewChatEvidenceHandler(db *gorm.DB, resolvers ...chatEvidenceChunkResolver) *ChatEvidenceHandler {
	handler := &ChatEvidenceHandler{db: db}
	if len(resolvers) > 0 {
		handler.sourceChunkResolver = resolvers[0]
	}
	return handler
}

func NewChatEvidenceHandlerWithWiki(
	db *gorm.DB,
	chunkResolver chatEvidenceChunkResolver,
	wikiResolver chatEvidenceWikiResolver,
	wikiKBID string,
) *ChatEvidenceHandler {
	return &ChatEvidenceHandler{
		db:                  db,
		sourceChunkResolver: chunkResolver,
		wikiResolver:        wikiResolver,
		wikiKBID:            strings.TrimSpace(wikiKBID),
	}
}

func (h *ChatEvidenceHandler) Lookup(c *gin.Context) {
	ids, err := chatEvidenceIDs(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid evidence lookup request"})
		return
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "knowledge_ids is required"})
		return
	}

	var chunks []model.VideoTranscriptChunk
	if err := h.db.WithContext(c.Request.Context()).
		Where("knowledge_id IN ? OR evidence_sentence_id IN ?", ids, ids).
		Find(&chunks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	directMatches := make(map[string]struct{}, len(chunks)*2)
	for _, chunk := range chunks {
		directMatches[strings.TrimSpace(chunk.KnowledgeID)] = struct{}{}
		directMatches[strings.TrimSpace(chunk.EvidenceSentenceID)] = struct{}{}
	}
	unresolvedIDs := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, matched := directMatches[strings.TrimSpace(id)]; !matched {
			unresolvedIDs = append(unresolvedIDs, id)
		}
	}
	lookupKnowledgeIDs, sourceChunkLookup, sourceChunks := h.lookupKnowledgeIDsForSourceChunks(c, unresolvedIDs)
	if len(lookupKnowledgeIDs) > 0 {
		var mappedChunks []model.VideoTranscriptChunk
		if err := h.db.WithContext(c.Request.Context()).Where("knowledge_id IN ?", lookupKnowledgeIDs).Find(&mappedChunks).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		chunks = append(chunks, mappedChunks...)
	}
	resolvedKnowledgeIDs := make(map[string]struct{}, len(chunks))
	for _, chunk := range chunks {
		resolvedKnowledgeIDs[strings.TrimSpace(chunk.KnowledgeID)] = struct{}{}
	}
	for lookupID, sourceChunk := range sourceChunks {
		if _, matched := resolvedKnowledgeIDs[strings.TrimSpace(sourceChunkLookup[lookupID])]; matched {
			continue
		}
		chunk, ok := h.resolveTranscriptSourceChunk(c.Request.Context(), sourceChunk)
		if !ok {
			continue
		}
		chunks = append(chunks, chunk)
		resolvedKnowledgeIDs[strings.TrimSpace(chunk.KnowledgeID)] = struct{}{}
		sourceChunkLookup[lookupID] = chunk.KnowledgeID
	}
	if len(chunks) == 0 {
		c.JSON(http.StatusOK, gin.H{"data": []ChatEvidenceItem{}})
		return
	}

	videoIDs := make([]string, 0, len(chunks))
	seenVideoIDs := map[string]struct{}{}
	for _, chunk := range chunks {
		if chunk.VideoID == "" {
			continue
		}
		if _, exists := seenVideoIDs[chunk.VideoID]; exists {
			continue
		}
		seenVideoIDs[chunk.VideoID] = struct{}{}
		videoIDs = append(videoIDs, chunk.VideoID)
	}
	var videos []model.Video
	if len(videoIDs) > 0 {
		if err := h.db.WithContext(c.Request.Context()).Where("id IN ?", videoIDs).Find(&videos).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	videoByID := make(map[string]model.Video, len(videos))
	for _, video := range videos {
		videoByID[video.ID] = video
	}
	wikiByVideoID := h.resolveWikiFallbacks(c.Request.Context(), videos)

	chunkByLookupID := make(map[string]model.VideoTranscriptChunk, len(chunks)*2)
	for _, chunk := range chunks {
		if id := strings.TrimSpace(chunk.KnowledgeID); id != "" {
			chunkByLookupID[id] = chunk
		}
		if id := strings.TrimSpace(chunk.EvidenceSentenceID); id != "" {
			chunkByLookupID[id] = chunk
		}
	}
	for lookupID, knowledgeID := range sourceChunkLookup {
		if chunk, ok := chunkByLookupID[knowledgeID]; ok {
			chunkByLookupID[lookupID] = chunk
		}
	}

	scope := videoevidence.NewScope()
	for _, video := range videos {
		scope.CurrentGeneration[video.ID] = strings.TrimSpace(video.TranscriptGeneration)
	}
	for _, chunk := range chunks {
		video := videoByID[chunk.VideoID]
		scope.Add(videoevidence.Evidence{
			EvidenceSentenceID:   chunk.EvidenceSentenceID,
			ChunkID:              chunk.KnowledgeID,
			KnowledgeID:          chunk.KnowledgeID,
			VideoID:              video.ID,
			VideoTitle:           video.Title,
			StartMs:              chunk.StartMs,
			EndMs:                chunk.EndMs,
			TranscriptGeneration: chunk.Generation,
			SourceType:           videoevidence.SourceTypeTranscript,
		})
	}

	// Preserve the order supplied by the citation list. This keeps the
	// evidence projection stable when the retrieval service returns rows in an
	// unspecified order.
	items := make([]ChatEvidenceItem, 0, len(ids))
	for _, requestedID := range ids {
		id := strings.TrimSpace(requestedID)
		chunk, ok := chunkByLookupID[id]
		if !ok {
			continue
		}
		video := videoByID[chunk.VideoID]
		startSeconds := nonNegativeMilliseconds(chunk.StartMs) / 1000
		endSeconds := nonNegativeMilliseconds(chunk.EndMs) / 1000
		if endSeconds < startSeconds {
			endSeconds = startSeconds
		}
		candidate := videoevidence.Candidate{
			EvidenceSentenceID:   strings.TrimSpace(chunk.EvidenceSentenceID),
			ChunkID:              strings.TrimSpace(chunk.KnowledgeID),
			KnowledgeID:          strings.TrimSpace(chunk.KnowledgeID),
			VideoID:              strings.TrimSpace(video.ID),
			TranscriptGeneration: strings.TrimSpace(chunk.Generation),
			SourceType:           videoevidence.SourceTypeTranscript,
		}
		evidence, validationErr := videoevidence.NormalizeCandidate(candidate, scope)
		errorCode := videoevidence.CodeOf(validationErr)
		if validationErr == nil && chunk.Status != "completed" {
			errorCode = videoevidence.ErrorUnpublished
		}
		linkable := validationErr == nil && chunk.Status == "completed"
		if validationErr == nil && chunk.Status == "completed" {
			candidate.VideoTitle = evidence.VideoTitle
		}
		items = append(items, ChatEvidenceItem{
			KnowledgeID:          id,
			VideoID:              chunk.VideoID,
			VideoTitle:           video.Title,
			VideoCover:           video.ThumbnailURL,
			StartMs:              chunk.StartMs,
			EndMs:                chunk.EndMs,
			StartSeconds:         startSeconds,
			EndSeconds:           endSeconds,
			Seconds:              startSeconds,
			Timestamp:            formatRange(startSeconds, endSeconds),
			EvidenceSentenceID:   chunk.EvidenceSentenceID,
			TranscriptGeneration: chunk.Generation,
			SourceType:           "transcript",
			Linkable:             linkable,
			WikiPageID:           wikiByVideoID[chunk.VideoID].PageID,
			WikiPageSlug:         wikiByVideoID[chunk.VideoID].Slug,
			WikiPageTitle:        wikiByVideoID[chunk.VideoID].Title,
			WikiKnowledgeBaseID:  wikiByVideoID[chunk.VideoID].KnowledgeBaseID,
			WikiLinkable:         wikiByVideoID[chunk.VideoID].Linkable,
			ErrorCode:            errorCodeString(errorCode),
		})
	}
	c.JSON(http.StatusOK, gin.H{"data": items})
}

func chatEvidenceIDs(c *gin.Context) ([]string, error) {
	if c.Request.Method != http.MethodPost {
		return splitQueryValues(c.Query("knowledge_ids")), nil
	}

	var request chatEvidenceRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		return nil, err
	}
	return splitQueryValues(strings.Join(request.KnowledgeIDs, ",")), nil
}

type chatEvidenceWikiFallback struct {
	PageID          string
	Slug            string
	Title           string
	KnowledgeBaseID string
	Linkable        bool
}

func (h *ChatEvidenceHandler) resolveWikiFallbacks(ctx context.Context, videos []model.Video) map[string]chatEvidenceWikiFallback {
	fallbacks := make(map[string]chatEvidenceWikiFallback, len(videos))
	if h == nil || h.wikiResolver == nil || strings.TrimSpace(h.wikiKBID) == "" {
		return fallbacks
	}
	for _, video := range videos {
		pageID := strings.TrimSpace(video.TranscriptPageWikiPageID)
		if pageID == "" {
			continue
		}
		page, err := h.wikiResolver.GetPageByID(ctx, h.wikiKBID, pageID)
		if err != nil || page == nil ||
			strings.TrimSpace(page.ID) != pageID ||
			strings.TrimSpace(page.Slug) == "" ||
			!strings.EqualFold(strings.TrimSpace(page.Status), "published") {
			continue
		}
		fallbacks[video.ID] = chatEvidenceWikiFallback{
			PageID:          page.ID,
			Slug:            page.Slug,
			Title:           page.Title,
			KnowledgeBaseID: h.wikiKBID,
			Linkable:        true,
		}
	}
	return fallbacks
}

func (h *ChatEvidenceHandler) lookupKnowledgeIDsForSourceChunks(c *gin.Context, ids []string) ([]string, map[string]string, map[string]weknora.KnowledgeChunk) {
	lookup := make(map[string]string)
	sourceChunks := make(map[string]weknora.KnowledgeChunk)
	if h == nil || h.db == nil || len(ids) == 0 {
		return nil, lookup, sourceChunks
	}
	type row struct {
		LookupID    string `gorm:"column:lookup_id"`
		KnowledgeID string `gorm:"column:knowledge_id"`
	}
	var rows []row
	if h.db.Migrator().HasTable("chunks") {
		_ = h.db.WithContext(c.Request.Context()).
			Table("chunks").
			Select("id AS lookup_id, knowledge_id").
			Where("id IN ?", ids).
			Where("deleted_at IS NULL").
			Find(&rows).Error
	}
	for _, row := range rows {
		lookupID := strings.TrimSpace(row.LookupID)
		knowledgeID := strings.TrimSpace(row.KnowledgeID)
		if lookupID != "" && knowledgeID != "" {
			lookup[lookupID] = knowledgeID
		}
	}
	if h.sourceChunkResolver != nil {
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, exists := lookup[id]; exists {
				continue
			}
			chunk, err := h.sourceChunkResolver.GetChunkByID(c.Request.Context(), id)
			if err != nil {
				continue
			}
			sourceChunks[id] = chunk
			rows = append(rows, row{LookupID: id, KnowledgeID: chunk.KnowledgeID})
		}
	}
	knowledgeIDs := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		lookupID := strings.TrimSpace(row.LookupID)
		knowledgeID := strings.TrimSpace(row.KnowledgeID)
		if lookupID == "" || knowledgeID == "" {
			continue
		}
		lookup[lookupID] = knowledgeID
		if _, ok := seen[knowledgeID]; ok {
			continue
		}
		seen[knowledgeID] = struct{}{}
		knowledgeIDs = append(knowledgeIDs, knowledgeID)
	}
	return knowledgeIDs, lookup, sourceChunks
}

func (h *ChatEvidenceHandler) resolveTranscriptSourceChunk(ctx context.Context, sourceChunk weknora.KnowledgeChunk) (model.VideoTranscriptChunk, bool) {
	if h == nil || h.db == nil || h.sourceChunkResolver == nil {
		return model.VideoTranscriptChunk{}, false
	}
	var binding model.VideoTranscriptSource
	if err := h.db.WithContext(ctx).
		Where("knowledge_id = ?", strings.TrimSpace(sourceChunk.KnowledgeID)).
		First(&binding).Error; err != nil {
		return model.VideoTranscriptChunk{}, false
	}
	var video model.Video
	if err := h.db.WithContext(ctx).Where("id = ?", binding.VideoID).First(&video).Error; err != nil {
		return model.VideoTranscriptChunk{}, false
	}
	knowledge, err := h.sourceChunkResolver.GetKnowledge(ctx, binding.KnowledgeID)
	if err != nil {
		return model.VideoTranscriptChunk{}, false
	}
	document, err := transcriptservice.ValidateSourceContent(
		knowledge.Content,
		binding.VideoID,
		binding.TranscriptGeneration,
		video.DurationSeconds,
	)
	if err != nil {
		return model.VideoTranscriptChunk{}, false
	}
	var transcriptChunks []model.VideoTranscriptChunk
	if err := h.db.WithContext(ctx).
		Where("video_id = ? AND generation = ?", binding.VideoID, binding.TranscriptGeneration).
		Order("chunk_index ASC").
		Find(&transcriptChunks).Error; err != nil || len(transcriptChunks) == 0 {
		return model.VideoTranscriptChunk{}, false
	}
	manifest := make([]transcriptservice.EvidenceManifestItem, 0, len(transcriptChunks))
	chunkByEvidenceID := make(map[string]model.VideoTranscriptChunk, len(transcriptChunks))
	for _, chunk := range transcriptChunks {
		manifest = append(manifest, transcriptservice.EvidenceManifestItem{
			EvidenceSentenceID: chunk.EvidenceSentenceID,
			SourceSentenceID:   chunk.SourceSegmentID,
			SpeakerID:          chunk.SpeakerID,
			StartMs:            chunk.StartMs,
			EndMs:              chunk.EndMs,
		})
		chunkByEvidenceID[strings.TrimSpace(chunk.EvidenceSentenceID)] = chunk
	}
	if err := transcriptservice.ValidateSourceEvidenceManifest(document, manifest); err != nil {
		return model.VideoTranscriptChunk{}, false
	}
	evidenceIDs := matchingEvidenceForSourceChunk(document, sourceChunk.Content)
	if len(evidenceIDs) == 0 {
		return model.VideoTranscriptChunk{}, false
	}
	selected := make([]model.VideoTranscriptChunk, 0, len(evidenceIDs))
	for _, evidenceID := range evidenceIDs {
		chunk, ok := chunkByEvidenceID[evidenceID]
		if !ok || chunk.Status != "completed" {
			return model.VideoTranscriptChunk{}, false
		}
		selected = append(selected, chunk)
	}
	for index := 1; index < len(selected); index++ {
		if selected[index].ChunkIndex != selected[index-1].ChunkIndex+1 {
			return model.VideoTranscriptChunk{}, false
		}
	}
	resolved := selected[0]
	for _, chunk := range selected[1:] {
		if chunk.StartMs < resolved.StartMs {
			resolved.StartMs = chunk.StartMs
		}
		if chunk.EndMs > resolved.EndMs {
			resolved.EndMs = chunk.EndMs
		}
	}
	return resolved, true
}

func uniqueEvidenceForSourceChunk(document transcriptservice.FullVideoDocument, sourceContent string) (string, bool) {
	evidenceIDs := matchingEvidenceForSourceChunk(document, sourceContent)
	if len(evidenceIDs) != 1 {
		return "", false
	}
	return evidenceIDs[0], true
}

func matchingEvidenceForSourceChunk(document transcriptservice.FullVideoDocument, sourceContent string) []string {
	sources := normalizedEvidenceTextCandidates(sourceContent)
	matches := make(map[string]struct{})
	orderedIDs := make([]string, 0)
	for _, chapter := range document.Chapters {
		for _, paragraph := range chapter.Paragraphs {
			for _, mark := range paragraph.TimeMarks {
				text := normalizeEvidenceText(mark.Text)
				if text == "" {
					continue
				}
				for _, source := range sources {
					if utf8.RuneCountInString(source) >= 16 && (strings.Contains(text, source) || strings.Contains(source, text)) {
						if id := strings.TrimSpace(mark.EvidenceSentenceID); id != "" {
							if _, exists := matches[id]; !exists {
								matches[id] = struct{}{}
								orderedIDs = append(orderedIDs, id)
							}
						}
						break
					}
				}
			}
		}
	}
	return orderedIDs
}

func normalizedEvidenceTextCandidates(value string) []string {
	values := []string{value}
	if strings.Contains(value, `\"`) || strings.Contains(value, `\\n`) {
		values = append(values, strings.NewReplacer(`\"`, `"`, `\\n`, "\n", `\\r`, "\r", `\\t`, "\t").Replace(value))
	}
	candidates := make([]string, 0, len(values)+2)
	seen := make(map[string]struct{})
	add := func(raw string) {
		normalized := normalizeEvidenceText(raw)
		if normalized == "" {
			return
		}
		if _, exists := seen[normalized]; exists {
			return
		}
		seen[normalized] = struct{}{}
		candidates = append(candidates, normalized)
	}
	for _, raw := range values {
		add(raw)
		for offset := 0; offset < len(raw); {
			marker := strings.Index(raw[offset:], `"text":"`)
			if marker < 0 {
				break
			}
			start := offset + marker + len(`"text":"`)
			end := len(raw)
			for _, delimiter := range []string{`","start_ms"`, `","end_ms"`, `"},`} {
				if index := strings.Index(raw[start:], delimiter); index >= 0 && start+index < end {
					end = start + index
				}
			}
			add(raw[start:end])
			offset = start
		}
	}
	return candidates
}

func normalizeEvidenceText(value string) string {
	return strings.Join(strings.Fields(value), "")
}

func errorCodeString(code videoevidence.ErrorCode) string {
	return string(code)
}

func nonNegativeMilliseconds(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func formatClock(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}
	return fmt.Sprintf("%02d:%02d", seconds/60, seconds%60)
}

func formatRange(startSeconds, endSeconds int) string {
	if endSeconds < startSeconds {
		endSeconds = startSeconds
	}
	return formatClock(startSeconds) + "–" + formatClock(endSeconds)
}
