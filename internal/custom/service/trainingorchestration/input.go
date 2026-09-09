// Package trainingorchestration assembles the verified, in-memory inputs used
// by the cross-video training path workflow.
package trainingorchestration

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/custom/client/weknora"
	"github.com/Tencent/WeKnora/internal/custom/model"
	"github.com/Tencent/WeKnora/internal/custom/service/knowledge"
	"github.com/Tencent/WeKnora/internal/custom/service/summary"
	"github.com/Tencent/WeKnora/internal/custom/service/transcript"
)

const (
	SchemaVersion      = "training-orchestration/v1"
	MaxCandidateVideos = 100
)

type TopicSource string

const (
	TopicSourceFinalSummary         TopicSource = "final_summary"
	TopicSourceNormalizedTranscript TopicSource = "normalized_transcript"
)

type SkipReason string

const (
	SkipProcessing       SkipReason = "processing"
	SkipProcessingFailed SkipReason = "processing_failed"
	// Wire values stay stable for v1 clients; they now describe formal content,
	// not knowledge-object readiness.
	SkipFormalContentNotReady         SkipReason = "knowledge_not_ready"
	SkipFormalContentValidationFailed SkipReason = "knowledge_audit_failed"
	SkipEvidenceMissing               SkipReason = "evidence_missing"
	SkipInaccessible                  SkipReason = "inaccessible"
)

type CapacityError struct {
	CandidateVideos int
}

func (e *CapacityError) Error() string {
	return fmt.Sprintf("training orchestration candidate videos exceed %d: got %d", MaxCandidateVideos, e.CandidateVideos)
}

type WikiReader interface {
	ListAllPages(context.Context, string, string) ([]weknora.WikiPage, error)
}

type KnowledgeReader interface {
	GetKnowledge(context.Context, string) (weknora.ManualKnowledgeResult, error)
}

type TranscriptReader interface {
	Read(context.Context, string, string) ([]transcript.Chunk, error)
}

type VideoAccessReader interface {
	CheckAccessible(context.Context, []model.Video) (map[string]bool, error)
}

type Collector struct {
	DB               *gorm.DB
	Wiki             WikiReader
	SourceReader     KnowledgeReader
	TranscriptReader TranscriptReader
	VideoAccess      VideoAccessReader
	KnowledgeBaseID  string
	OwnerScopeID     string
}

type InputPackage struct {
	SchemaVersion     string              `json:"schema_version"`
	OwnerScopeID      string              `json:"owner_scope_id"`
	ScannedVideos     int                 `json:"scanned_videos"`
	QualifiedVideos   []VideoTopicProfile `json:"qualified_videos"`
	SkippedVideos     []SkippedVideo      `json:"skipped_videos"`
	SkipReasonCounts  map[SkipReason]int  `json:"skip_reason_counts"`
	TopicSourceCounts map[TopicSource]int `json:"topic_source_counts"`
}

type SkippedVideo struct {
	VideoID string     `json:"video_id"`
	Reason  SkipReason `json:"reason"`
}

type VideoTopicProfile struct {
	VideoID              string            `json:"video_id"`
	Title                string            `json:"title"`
	VideoType            string            `json:"video_type"`
	DurationSeconds      int               `json:"duration_seconds"`
	TranscriptGeneration string            `json:"transcript_generation"`
	TopicSource          TopicSource       `json:"topic_source"`
	Summary              *SummaryInput     `json:"summary,omitempty"`
	Transcript           *TranscriptInput  `json:"transcript,omitempty"`
	KnowledgeIndex       WikiReference     `json:"knowledge_index"`
	KnowledgeSignals     []KnowledgeSignal `json:"knowledge_signals"`
	EvidenceSignals      []EvidenceSignal  `json:"evidence_signals"`
}

type WikiReference struct {
	WikiPageID string `json:"wiki_page_id"`
	Version    int    `json:"version"`
}

type SummaryInput struct {
	WikiReference
	Signals []SummarySignal `json:"signals"`
}

type SummarySignal struct {
	Section       string              `json:"section"`
	Text          string              `json:"text"`
	KnowledgeRefs []string            `json:"knowledge_refs"`
	EvidenceRefs  []EvidenceReference `json:"evidence_refs"`
}

type TranscriptInput struct {
	KnowledgeID     string             `json:"knowledge_id"`
	KnowledgeBaseID string             `json:"knowledge_base_id"`
	ContentHash     string             `json:"content_hash"`
	Signals         []TranscriptSignal `json:"signals"`
}

type TranscriptSignal struct {
	Chapter      string              `json:"chapter"`
	Text         string              `json:"text"`
	StartMs      int                 `json:"start_ms"`
	EndMs        int                 `json:"end_ms"`
	EvidenceRefs []EvidenceReference `json:"evidence_refs"`
}

type KnowledgeSignal struct {
	KnowledgeObjectID string                  `json:"knowledge_object_id"`
	WikiPageID        string                  `json:"wiki_page_id"`
	WikiPageVersion   int                     `json:"wiki_page_version"`
	KnowledgeType     knowledge.KnowledgeType `json:"knowledge_type"`
	EntitySubType     string                  `json:"entity_sub_type,omitempty"`
	Title             string                  `json:"title"`
	CoreContent       string                  `json:"core_content"`
	StructureFields   map[string]string       `json:"structure_fields"`
	EvidenceIDs       []string                `json:"evidence_ids"`
}

type EvidenceReference struct {
	EvidenceID       string `json:"evidence_id"`
	ChunkKnowledgeID string `json:"chunk_knowledge_id"`
	StartMs          int    `json:"start_ms"`
	EndMs            int    `json:"end_ms"`
}

type EvidenceSignal struct {
	VideoID              string `json:"video_id"`
	TranscriptGeneration string `json:"transcript_generation"`
	EvidenceID           string `json:"evidence_id"`
	ChunkKnowledgeID     string `json:"chunk_knowledge_id"`
	StartMs              int    `json:"start_ms"`
	EndMs                int    `json:"end_ms"`
	TranscriptSnippet    string `json:"transcript_snippet"`
}

type wikiSnapshot struct {
	byID map[string]weknora.WikiPage
}

func newWikiSnapshot(pages []weknora.WikiPage) (wikiSnapshot, error) {
	snapshot := wikiSnapshot{
		byID: make(map[string]weknora.WikiPage, len(pages)),
	}
	for _, page := range pages {
		id := strings.TrimSpace(page.ID)
		if id == "" {
			continue
		}
		if _, duplicate := snapshot.byID[id]; duplicate {
			return wikiSnapshot{}, fmt.Errorf("training orchestration Wiki snapshot contains duplicate page id %s", id)
		}
		snapshot.byID[id] = page
	}
	return snapshot, nil
}

func (s wikiSnapshot) pageByID(id string) *weknora.WikiPage {
	page, ok := s.byID[strings.TrimSpace(id)]
	if !ok {
		return nil
	}
	copy := page
	return &copy
}

func (c *Collector) Collect(ctx context.Context) (InputPackage, error) {
	result := InputPackage{
		SchemaVersion: SchemaVersion,
		SkipReasonCounts: map[SkipReason]int{
			SkipProcessing: 0, SkipProcessingFailed: 0, SkipFormalContentNotReady: 0,
			SkipFormalContentValidationFailed: 0, SkipEvidenceMissing: 0, SkipInaccessible: 0,
		},
		TopicSourceCounts: map[TopicSource]int{
			TopicSourceFinalSummary: 0, TopicSourceNormalizedTranscript: 0,
		},
	}
	if c != nil {
		result.OwnerScopeID = strings.TrimSpace(c.OwnerScopeID)
	}
	if err := c.validate(); err != nil {
		return result, err
	}

	candidateQuery := func() *gorm.DB {
		return c.DB.WithContext(ctx).Model(&model.Video{}).
			Where("uploaded_at IS NOT NULL AND TRIM(COALESCE(file_url, '')) <> '' AND status IN ?", append(model.VideoInitiallyAvailableStatuses(), model.VideoStatusFailed))
	}
	var scanned int64
	if err := candidateQuery().Count(&scanned).Error; err != nil {
		return result, fmt.Errorf("count training orchestration candidates: %w", err)
	}
	result.ScannedVideos = int(scanned)
	if result.ScannedVideos > MaxCandidateVideos {
		return result, &CapacityError{CandidateVideos: result.ScannedVideos}
	}

	var videos []model.Video
	if err := candidateQuery().Order("id ASC").Find(&videos).Error; err != nil {
		return result, fmt.Errorf("list training orchestration candidates: %w", err)
	}
	if len(videos) == 0 {
		return result, nil
	}
	accessCandidates := make([]model.Video, 0, len(videos))
	for _, video := range videos {
		if video.Status != model.VideoStatusFailed {
			accessCandidates = append(accessCandidates, video)
		}
	}
	accessibility, err := c.VideoAccess.CheckAccessible(ctx, accessCandidates)
	if err != nil {
		return result, fmt.Errorf("check training orchestration video access: %w", err)
	}
	wiki := wikiSnapshot{byID: map[string]weknora.WikiPage{}}
	if anyVideoNeedsSummaryWiki(videos, accessibility) {
		// Typed summaries are stored as index pages; the frontmatter and the
		// persisted SummaryWikiPageID define the artifact type and identity.
		pages, err := c.Wiki.ListAllPages(ctx, c.KnowledgeBaseID, "")
		if err != nil {
			return result, fmt.Errorf("read training orchestration Wiki snapshot: %w", err)
		}
		wiki, err = newWikiSnapshot(pages)
		if err != nil {
			return result, err
		}
	}
	for _, video := range videos {
		profile, reason, err := c.collectVideo(ctx, video, accessibility[video.ID], wiki)
		if err != nil {
			return result, fmt.Errorf("collect training orchestration input for video %s: %w", video.ID, err)
		}
		if reason != "" {
			result.SkippedVideos = append(result.SkippedVideos, SkippedVideo{VideoID: video.ID, Reason: reason})
			result.SkipReasonCounts[reason]++
			continue
		}
		result.QualifiedVideos = append(result.QualifiedVideos, profile)
		result.TopicSourceCounts[profile.TopicSource]++
	}
	return result, nil
}

func anyVideoNeedsSummaryWiki(videos []model.Video, accessibility map[string]bool) bool {
	for _, video := range videos {
		if video.Status != model.VideoStatusFailed && accessibility[video.ID] &&
			strings.TrimSpace(video.TranscriptGeneration) != "" && video.TranscriptActiveRevision > 0 &&
			strings.TrimSpace(video.SummaryWikiPageID) != "" &&
			strings.EqualFold(strings.TrimSpace(video.SummaryResultStage), "final_ready") {
			return true
		}
	}
	return false
}

func (c *Collector) validate() error {
	if c == nil || c.DB == nil || c.Wiki == nil || c.SourceReader == nil || c.TranscriptReader == nil || c.VideoAccess == nil {
		return fmt.Errorf("training orchestration collector dependencies are not configured")
	}
	if strings.TrimSpace(c.KnowledgeBaseID) == "" || strings.TrimSpace(c.OwnerScopeID) == "" {
		return fmt.Errorf("training orchestration knowledge base and owner scope are required")
	}
	return nil
}

func (c *Collector) collectVideo(ctx context.Context, video model.Video, accessible bool, wiki wikiSnapshot) (VideoTopicProfile, SkipReason, error) {
	profile := VideoTopicProfile{
		VideoID: video.ID, Title: video.Title, VideoType: video.VideoType,
		DurationSeconds: video.DurationSeconds, TranscriptGeneration: video.TranscriptGeneration,
	}
	if video.Status == model.VideoStatusFailed {
		return profile, SkipProcessingFailed, nil
	}
	if !accessible {
		return profile, SkipInaccessible, nil
	}
	if strings.TrimSpace(video.TranscriptGeneration) == "" || video.TranscriptActiveRevision <= 0 {
		return profile, SkipProcessing, nil
	}
	chunks, err := c.TranscriptReader.Read(ctx, video.ID, video.TranscriptGeneration)
	if err != nil {
		return profile, "", fmt.Errorf("read transcript evidence for video %s generation %s: %w", video.ID, video.TranscriptGeneration, err)
	}
	if len(chunks) == 0 {
		return profile, SkipEvidenceMissing, nil
	}
	if ok, err := c.validateEvidenceManifest(ctx, video, chunks); err != nil {
		return profile, "", err
	} else if !ok {
		return profile, SkipEvidenceMissing, nil
	}
	evidence := indexEvidence(chunks)

	profile.KnowledgeSignals = []KnowledgeSignal{}
	requiredEvidence := make(map[string]transcript.Chunk)

	if strings.TrimSpace(video.SummaryWikiPageID) != "" && strings.EqualFold(strings.TrimSpace(video.SummaryResultStage), "final_ready") {
		summaryInput, summaryEvidence, ok := readSummary(wiki, video, evidence)
		if !ok {
			return profile, SkipFormalContentNotReady, nil
		}
		profile.TopicSource = TopicSourceFinalSummary
		profile.Summary = summaryInput
		for id, chunk := range summaryEvidence {
			requiredEvidence[id] = chunk
		}
	} else {
		transcriptInput, transcriptEvidence, ok, err := c.readNormalizedTranscript(ctx, video, evidence)
		if err != nil {
			return profile, "", err
		}
		if !ok {
			return profile, SkipFormalContentNotReady, nil
		}
		profile.TopicSource = TopicSourceNormalizedTranscript
		profile.Transcript = transcriptInput
		for id, chunk := range transcriptEvidence {
			requiredEvidence[id] = chunk
		}
	}

	profile.EvidenceSignals = evidenceSignals(video, requiredEvidence)
	return profile, "", nil
}

func (c *Collector) validateEvidenceManifest(ctx context.Context, video model.Video, chunks []transcript.Chunk) (bool, error) {
	var checkpoints []model.VideoTranscriptChunk
	if err := c.DB.WithContext(ctx).
		Where("video_id = ? AND generation = ?", video.ID, video.TranscriptGeneration).
		Order("chunk_index ASC").Find(&checkpoints).Error; err != nil {
		return false, fmt.Errorf("read current evidence manifest: %w", err)
	}
	if len(checkpoints) == 0 || len(checkpoints) != len(chunks) {
		return false, nil
	}
	seenEvidence := make(map[string]struct{}, len(checkpoints))
	seenKnowledge := make(map[string]struct{}, len(checkpoints))
	for index, checkpoint := range checkpoints {
		chunk := chunks[index]
		if checkpoint.ChunkIndex != index || checkpoint.Revision != video.TranscriptActiveRevision ||
			checkpoint.Status != "completed" || strings.TrimSpace(checkpoint.EvidenceSentenceID) == "" ||
			strings.TrimSpace(checkpoint.KnowledgeID) == "" || checkpoint.StartMs < 0 || checkpoint.EndMs <= checkpoint.StartMs ||
			(video.DurationSeconds > 0 && checkpoint.EndMs > video.DurationSeconds*1000) ||
			chunk.ID != checkpoint.KnowledgeID || chunk.EvidenceSentenceID != checkpoint.EvidenceSentenceID ||
			chunk.StartMs != checkpoint.StartMs || chunk.EndMs != checkpoint.EndMs {
			return false, nil
		}
		if _, duplicate := seenEvidence[checkpoint.EvidenceSentenceID]; duplicate {
			return false, nil
		}
		if _, duplicate := seenKnowledge[checkpoint.KnowledgeID]; duplicate {
			return false, nil
		}
		seenEvidence[checkpoint.EvidenceSentenceID] = struct{}{}
		seenKnowledge[checkpoint.KnowledgeID] = struct{}{}
	}
	return true, nil
}

func readSummary(
	wiki wikiSnapshot,
	video model.Video,
	evidence map[string]transcript.Chunk,
) (*SummaryInput, map[string]transcript.Chunk, bool) {
	page := wiki.pageByID(video.SummaryWikiPageID)
	if page == nil || !summaryPageMatches(page, video) {
		return nil, nil, false
	}
	document, err := summary.ParseStored(page.Content)
	if err != nil || summary.ValidateStored(document, "") != nil {
		return nil, nil, false
	}
	input := &SummaryInput{WikiReference: WikiReference{WikiPageID: page.ID, Version: page.Version}}
	required := make(map[string]transcript.Chunk)
	for _, section := range document.Sections {
		for _, block := range section.Blocks {
			refs := summaryEvidenceReferences(block)
			if strings.TrimSpace(block.Text) == "" || len(refs) == 0 {
				continue
			}
			verified := make([]EvidenceReference, 0, len(refs))
			for _, ref := range refs {
				chunk, ok := evidence[ref.EvidenceID]
				if !ok || chunk.ID != ref.ChunkKnowledgeID || chunk.StartMs != ref.StartMs || chunk.EndMs != ref.EndMs {
					return nil, nil, false
				}
				verified = append(verified, ref)
				required[ref.EvidenceID] = chunk
			}
			input.Signals = append(input.Signals, SummarySignal{
				Section: section.Title, Text: strings.TrimSpace(block.Text),
				KnowledgeRefs: []string{}, EvidenceRefs: verified,
			})
		}
	}
	if len(input.Signals) == 0 {
		return nil, nil, false
	}
	return input, required, true
}

func (c *Collector) readNormalizedTranscript(ctx context.Context, video model.Video, evidence map[string]transcript.Chunk) (*TranscriptInput, map[string]transcript.Chunk, bool, error) {
	var binding model.VideoTranscriptSource
	err := c.DB.WithContext(ctx).Where(
		"video_id = ? AND transcript_generation = ? AND knowledge_base_id = ? AND status = ?",
		video.ID, video.TranscriptGeneration, c.KnowledgeBaseID, transcript.SourceStatusCreated,
	).First(&binding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("read normalized transcript binding: %w", err)
	}
	if strings.TrimSpace(binding.KnowledgeID) == "" || strings.TrimSpace(binding.ContentHash) == "" {
		return nil, nil, false, nil
	}
	stored, err := c.SourceReader.GetKnowledge(ctx, binding.KnowledgeID)
	if err != nil {
		return nil, nil, false, fmt.Errorf("read normalized transcript source: %w", err)
	}
	if strings.TrimSpace(stored.ID) != strings.TrimSpace(binding.KnowledgeID) ||
		strings.TrimSpace(stored.KnowledgeBaseID) != strings.TrimSpace(c.KnowledgeBaseID) ||
		strings.EqualFold(strings.TrimSpace(stored.ParseStatus), "failed") {
		return nil, nil, false, nil
	}
	document, err := transcript.ValidateSourceContent(stored.Content, video.ID, video.TranscriptGeneration, video.DurationSeconds)
	if err != nil {
		return nil, nil, false, nil
	}
	documentJSON, err := document.JSON()
	if err != nil || fmt.Sprintf("%x", sha256.Sum256([]byte(documentJSON))) != strings.TrimSpace(binding.ContentHash) {
		return nil, nil, false, nil
	}
	input := &TranscriptInput{
		KnowledgeID: binding.KnowledgeID, KnowledgeBaseID: binding.KnowledgeBaseID, ContentHash: binding.ContentHash,
		Signals: make([]TranscriptSignal, 0, len(document.Chapters)),
	}
	required := make(map[string]transcript.Chunk)
	for _, chapter := range document.Chapters {
		signal := TranscriptSignal{Chapter: chapter.Title, Text: chapter.ContinuousText, StartMs: chapter.StartMs, EndMs: chapter.EndMs}
		for _, paragraph := range chapter.Paragraphs {
			for _, mark := range paragraph.TimeMarks {
				chunk, ok := evidence[mark.EvidenceSentenceID]
				if !ok || chunk.SourceSentenceID != mark.SourceSentenceID || chunk.StartMs != mark.StartMs || chunk.EndMs != mark.EndMs ||
					strings.TrimSpace(transcript.OriginalText(chunk.Content)) != strings.TrimSpace(mark.Text) {
					return nil, nil, false, nil
				}
				signal.EvidenceRefs = append(signal.EvidenceRefs, EvidenceReference{
					EvidenceID: mark.EvidenceSentenceID, ChunkKnowledgeID: chunk.ID, StartMs: mark.StartMs, EndMs: mark.EndMs,
				})
				required[mark.EvidenceSentenceID] = chunk
			}
		}
		if len(signal.EvidenceRefs) == 0 {
			return nil, nil, false, nil
		}
		input.Signals = append(input.Signals, signal)
	}
	if len(required) != len(evidence) {
		return nil, nil, false, nil
	}
	return input, required, len(input.Signals) > 0, nil
}

func summaryPageMatches(page *weknora.WikiPage, video model.Video) bool {
	frontmatter := page.ParsedFrontmatter()
	return strings.EqualFold(frontmatterString(frontmatter, "type"), "typed_summary") &&
		frontmatterString(frontmatter, "source_video_id") == video.ID &&
		frontmatterString(frontmatter, "transcript_generation") == video.TranscriptGeneration
}

func summaryEvidenceReferences(block summary.Block) []EvidenceReference {
	if len(block.EvidenceRefs) > 0 {
		result := make([]EvidenceReference, 0, len(block.EvidenceRefs))
		for _, ref := range block.EvidenceRefs {
			result = append(result, EvidenceReference{
				EvidenceID: ref.EvidenceSentenceID, ChunkKnowledgeID: ref.ChunkID, StartMs: ref.StartMs, EndMs: ref.EndMs,
			})
		}
		return result
	}
	result := make([]EvidenceReference, 0, len(block.Evidence))
	for _, item := range block.Evidence {
		result = append(result, EvidenceReference{
			EvidenceID: item.EvidenceSentenceID, ChunkKnowledgeID: item.ChunkID,
			StartMs: int(math.Round(item.StartSeconds * 1000)), EndMs: int(math.Round(item.EndSeconds * 1000)),
		})
	}
	return result
}

func indexEvidence(chunks []transcript.Chunk) map[string]transcript.Chunk {
	result := make(map[string]transcript.Chunk, len(chunks))
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk.EvidenceSentenceID) != "" {
			result[chunk.EvidenceSentenceID] = chunk
		}
	}
	return result
}

func evidenceSignals(video model.Video, chunks map[string]transcript.Chunk) []EvidenceSignal {
	ids := make([]string, 0, len(chunks))
	for id := range chunks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]EvidenceSignal, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		chunk := chunks[id]
		canonicalID := strings.TrimSpace(chunk.EvidenceSentenceID)
		if canonicalID == "" {
			canonicalID = id
		}
		if _, duplicate := seen[canonicalID]; duplicate {
			continue
		}
		seen[canonicalID] = struct{}{}
		result = append(result, EvidenceSignal{
			VideoID: video.ID, TranscriptGeneration: video.TranscriptGeneration,
			EvidenceID: canonicalID, ChunkKnowledgeID: chunk.ID, StartMs: chunk.StartMs, EndMs: chunk.EndMs,
			TranscriptSnippet: transcript.OriginalText(chunk.Content),
		})
	}
	return result
}

func frontmatterString(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}
