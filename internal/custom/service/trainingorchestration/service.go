package trainingorchestration

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/Tencent/WeKnora/internal/custom/client/weknora"
	"github.com/Tencent/WeKnora/internal/custom/model"
)

const (
	JobQueued    = "queued"
	JobRunning   = "running"
	JobSucceeded = "succeeded"
	JobFailed    = "failed"
)

type InputCollector interface {
	Collect(context.Context) (InputPackage, error)
}
type ProjectionGenerator interface {
	Generate(context.Context, InputPackage) (ProjectionDocument, error)
}
type ProjectionWiki interface {
	EnsurePage(context.Context, string, weknora.WikiPageWrite) (*weknora.WikiPage, error)
	GetPageByID(context.Context, string, string) (*weknora.WikiPage, error)
}

type Service struct {
	DB              *gorm.DB
	Collector       InputCollector
	Generator       ProjectionGenerator
	Wiki            ProjectionWiki
	KnowledgeBaseID string
	OwnerScopeID    string
	Model           string
	PromptVersion   string
	RunTimeout      time.Duration
	mu              sync.Mutex
	running         map[string]struct{}
}

func (s *Service) Start(ctx context.Context) (model.TrainingOrchestrationJob, error) {
	if err := s.validate(); err != nil {
		return model.TrainingOrchestrationJob{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var active model.TrainingOrchestrationJob
	err := s.DB.WithContext(ctx).Where("owner_scope_id = ? AND status IN ?", s.OwnerScopeID, []string{JobQueued, JobRunning}).Order("created_at DESC").First(&active).Error
	if err == nil {
		if _, owned := s.running[active.ID]; owned {
			return active, nil
		}
		now := time.Now().UTC()
		if updateErr := s.DB.WithContext(ctx).Model(&model.TrainingOrchestrationJob{}).Where("id = ?", active.ID).Updates(map[string]any{"status": JobFailed, "error_code": "process_interrupted", "error_message": "服务进程已中断，请重新生成", "finished_at": now, "updated_at": now}).Error; updateErr != nil {
			return model.TrainingOrchestrationJob{}, fmt.Errorf("recover interrupted training orchestration job: %w", updateErr)
		}
		err = gorm.ErrRecordNotFound
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.TrainingOrchestrationJob{}, fmt.Errorf("find active training orchestration job: %w", err)
	}
	job := model.TrainingOrchestrationJob{ID: uuid.NewString(), OwnerScopeID: s.OwnerScopeID, Status: JobQueued, Progress: 0, Model: s.Model, PromptVersion: s.PromptVersion}
	if err := s.DB.WithContext(ctx).Create(&job).Error; err != nil {
		return model.TrainingOrchestrationJob{}, fmt.Errorf("create training orchestration job: %w", err)
	}
	if s.running == nil {
		s.running = make(map[string]struct{})
	}
	s.running[job.ID] = struct{}{}
	go s.run(job.ID)
	return job, nil
}

func (s *Service) GetJob(ctx context.Context, id string) (model.TrainingOrchestrationJob, error) {
	var job model.TrainingOrchestrationJob
	if err := s.DB.WithContext(ctx).Where("id = ? AND owner_scope_id = ?", strings.TrimSpace(id), s.OwnerScopeID).First(&job).Error; err != nil {
		return job, err
	}
	return job, nil
}

func (s *Service) GetCurrent(ctx context.Context) (*ProjectionDocument, *model.TrainingOrchestrationCurrent, error) {
	if err := s.validate(); err != nil {
		return nil, nil, err
	}
	var current model.TrainingOrchestrationCurrent
	if err := s.DB.WithContext(ctx).Where("owner_scope_id = ?", s.OwnerScopeID).First(&current).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read current training orchestration: %w", err)
	}
	page, err := s.Wiki.GetPageByID(ctx, s.KnowledgeBaseID, current.ResultWikiPageID)
	if err != nil {
		return nil, &current, fmt.Errorf("read training orchestration Wiki: %w", err)
	}
	if page == nil {
		return nil, &current, fmt.Errorf("current training orchestration Wiki does not exist")
	}
	var doc ProjectionDocument
	if err := json.Unmarshal([]byte(page.Content), &doc); err != nil {
		return nil, &current, fmt.Errorf("decode current training orchestration Wiki: %w", err)
	}
	if doc.TrainingPathProjection.OwnerScopeID != s.OwnerScopeID || doc.TrainingPathProjection.SourceFingerprint != current.SourceFingerprint || doc.TrainingPathProjection.SchemaVersion != SchemaVersion {
		return nil, &current, fmt.Errorf("current training orchestration identity is invalid")
	}
	normalizeProjectionKnowledgeFields(&doc.TrainingPathProjection)
	if err := validateKnowledgeCompatibilityFields(doc); err != nil {
		return nil, &current, fmt.Errorf("current training orchestration content is invalid: %w", err)
	}
	return &doc, &current, nil
}

func (s *Service) run(jobID string) {
	defer func() {
		s.mu.Lock()
		delete(s.running, jobID)
		s.mu.Unlock()
	}()
	timeout := s.RunTimeout
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	now := time.Now().UTC()
	if err := s.DB.WithContext(ctx).Model(&model.TrainingOrchestrationJob{}).Where("id = ? AND status = ?", jobID, JobQueued).Updates(map[string]any{"status": JobRunning, "progress": 5, "started_at": now, "updated_at": now}).Error; err != nil {
		return
	}
	input, err := s.Collector.Collect(ctx)
	if err != nil {
		s.fail(jobID, "input_collection_failed", err)
		return
	}
	fingerprint, err := SourceFingerprint(input, s.Model, s.PromptVersion)
	if err != nil {
		s.fail(jobID, "fingerprint_failed", err)
		return
	}
	inputRefs, _ := json.Marshal(inputReferenceSnapshot(input))
	_ = s.DB.WithContext(ctx).Model(&model.TrainingOrchestrationJob{}).Where("id = ?", jobID).Updates(map[string]any{"progress": 25, "source_fingerprint": fingerprint, "input_references": string(inputRefs)}).Error
	var current model.TrainingOrchestrationCurrent
	if err := s.DB.WithContext(ctx).Where("owner_scope_id = ? AND source_fingerprint = ?", s.OwnerScopeID, fingerprint).First(&current).Error; err == nil {
		if err := s.succeed(ctx, jobID, current.ResultWikiPageID, true); err != nil {
			s.fail(jobID, "job_update_failed", err)
		}
		return
	}
	doc, err := s.Generator.Generate(ctx, input)
	if err != nil {
		s.fail(jobID, "generation_failed", err)
		return
	}
	if doc.TrainingPathProjection.SourceFingerprint != fingerprint {
		s.fail(jobID, "generation_validation_failed", fmt.Errorf("training orchestration generator fingerprint does not match the job input"))
		return
	}
	normalizeProjectionKnowledgeFields(&doc.TrainingPathProjection)
	if err := ValidateProjection(doc, input); err != nil {
		s.fail(jobID, "generation_validation_failed", err)
		return
	}
	s.DB.WithContext(ctx).Model(&model.TrainingOrchestrationJob{}).Where("id = ?", jobID).Update("progress", 75)
	latest, err := s.Collector.Collect(ctx)
	if err != nil {
		s.fail(jobID, "source_recheck_failed", err)
		return
	}
	latestFingerprint, err := SourceFingerprint(latest, s.Model, s.PromptVersion)
	if err != nil || latestFingerprint != fingerprint {
		s.fail(jobID, "source_changed", fmt.Errorf("training orchestration sources changed while generation was running"))
		return
	}
	content, err := json.Marshal(doc)
	if err != nil {
		s.fail(jobID, "encode_result_failed", err)
		return
	}
	page, err := s.Wiki.EnsurePage(ctx, s.KnowledgeBaseID, weknora.WikiPageWrite{Slug: projectionSlug(s.OwnerScopeID, fingerprint), Title: "培训学习路径 " + time.Now().Format("2006-01-02 15:04"), PageType: "index", Status: "published", Content: string(content), Summary: "由当前视频、正式总结或规范化转写与字幕证据生成的培训学习路径", SourceRefs: projectionSourceRefs(input), ChunkRefs: projectionChunkRefs(input)})
	if err != nil {
		s.fail(jobID, "wiki_publish_failed", err)
		return
	}
	if page == nil {
		s.fail(jobID, "wiki_publish_failed", fmt.Errorf("training orchestration Wiki writer returned no page"))
		return
	}
	var published ProjectionDocument
	if err := json.Unmarshal([]byte(page.Content), &published); err != nil {
		s.fail(jobID, "wiki_publish_validation_failed", err)
		return
	}
	normalizeProjectionKnowledgeFields(&published.TrainingPathProjection)
	if err := ValidateProjection(published, input); err != nil || published.TrainingPathProjection.SourceFingerprint != fingerprint {
		if err == nil {
			err = fmt.Errorf("published training orchestration fingerprint does not match the job input")
		}
		s.fail(jobID, "wiki_publish_validation_failed", err)
		return
	}
	if err := s.publishCurrent(ctx, jobID, page.ID, fingerprint); err != nil {
		s.fail(jobID, "current_switch_failed", err)
		return
	}
}

func (s *Service) publishCurrent(ctx context.Context, jobID, pageID, fingerprint string) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job model.TrainingOrchestrationJob
		if err := tx.Where("id = ? AND owner_scope_id = ? AND status = ?", jobID, s.OwnerScopeID, JobRunning).First(&job).Error; err != nil {
			return err
		}
		now := time.Now().UTC()
		current := model.TrainingOrchestrationCurrent{OwnerScopeID: s.OwnerScopeID, JobID: jobID, ResultWikiPageID: pageID, SourceFingerprint: fingerprint, UpdatedAt: now}
		if err := tx.Save(&current).Error; err != nil {
			return err
		}
		return tx.Model(&model.TrainingOrchestrationJob{}).Where("id = ?", jobID).Updates(map[string]any{"status": JobSucceeded, "progress": 100, "result_wiki_page_id": pageID, "reused": false, "finished_at": now, "updated_at": now}).Error
	})
}

func (s *Service) succeed(ctx context.Context, jobID, pageID string, reused bool) error {
	now := time.Now().UTC()
	return s.DB.WithContext(ctx).Model(&model.TrainingOrchestrationJob{}).Where("id = ?", jobID).Updates(map[string]any{"status": JobSucceeded, "progress": 100, "result_wiki_page_id": pageID, "reused": reused, "finished_at": now, "updated_at": now}).Error
}
func (s *Service) fail(jobID, code string, err error) {
	now := time.Now().UTC()
	message := ""
	if err != nil {
		message = err.Error()
	}
	s.DB.Model(&model.TrainingOrchestrationJob{}).Where("id = ?", jobID).Updates(map[string]any{"status": JobFailed, "error_code": code, "error_message": message, "finished_at": now, "updated_at": now})
}
func (s *Service) validate() error {
	if s == nil || s.DB == nil || s.Collector == nil || s.Generator == nil || s.Wiki == nil || strings.TrimSpace(s.KnowledgeBaseID) == "" || strings.TrimSpace(s.OwnerScopeID) == "" {
		return fmt.Errorf("training orchestration service dependencies are not configured")
	}
	return nil
}

type inputReference struct {
	VideoID, TranscriptGeneration, TopicSource, SummaryWikiPageID, TranscriptKnowledgeID, KnowledgeIndexWikiPageID string
	KnowledgeWikiPageIDs, EvidenceIDs                                                                              []string
}

func inputReferenceSnapshot(input InputPackage) []inputReference {
	out := make([]inputReference, 0, len(input.QualifiedVideos))
	for _, v := range input.QualifiedVideos {
		item := inputReference{VideoID: v.VideoID, TranscriptGeneration: v.TranscriptGeneration, TopicSource: string(v.TopicSource)}
		if v.Summary != nil {
			item.SummaryWikiPageID = v.Summary.WikiPageID
		}
		if v.Transcript != nil {
			item.TranscriptKnowledgeID = v.Transcript.KnowledgeID
		}
		for _, e := range v.EvidenceSignals {
			item.EvidenceIDs = append(item.EvidenceIDs, e.EvidenceID)
		}
		out = append(out, item)
	}
	return out
}
func projectionSlug(owner, fingerprint string) string {
	clean := strings.NewReplacer(":", "-", "/", "-").Replace(fingerprint)
	if len(clean) > 28 {
		clean = clean[:28]
	}
	ownerHash := sha256String(owner)
	return "training-orchestration/" + ownerHash[:12] + "/" + clean
}
func sha256String(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum[:])
}
func projectionSourceRefs(input InputPackage) []string {
	seen := map[string]struct{}{}
	out := []string{}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for _, v := range input.QualifiedVideos {
		if v.Summary != nil {
			add(v.Summary.WikiPageID)
		}
		if v.Transcript != nil {
			add(v.Transcript.KnowledgeID)
		}
	}
	return out
}
func projectionChunkRefs(input InputPackage) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, v := range input.QualifiedVideos {
		for _, e := range v.EvidenceSignals {
			if _, ok := seen[e.ChunkKnowledgeID]; !ok {
				seen[e.ChunkKnowledgeID] = struct{}{}
				out = append(out, e.ChunkKnowledgeID)
			}
		}
	}
	return out
}
