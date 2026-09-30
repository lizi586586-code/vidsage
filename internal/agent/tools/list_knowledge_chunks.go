package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/Tencent/WeKnora/internal/searchutil"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/Tencent/WeKnora/internal/videoevidence"
)

// listKnowledgeChunksMaxLimit is the hard per-page ceiling enforced by the
// tool schema (maximum: 100) and re-applied defensively inside Execute so any
// caller that bypasses the JSON Schema validation layer (e.g. direct API or
// sandbox) still cannot request unbounded chunk pages in a single call.
const listKnowledgeChunksMaxLimit = 100

var listKnowledgeChunksTool = BaseTool{
	name: ToolListKnowledgeChunks,
	description: `Retrieve full chunk content for a document or a single FAQ entry.

## Use After grep_chunks or knowledge_search:
- **FAQ hit** (type faq): list_knowledge_chunks(faq_id="cN") — reads that one FAQ chunk with answers from metadata.
- **Document hit**: list_knowledge_chunks(knowledge_id="dN") — pages through all chunks.

## Parameters (provide exactly one id target):
- faq_id (optional): Short cN ID for an FAQ chunk from grep_chunks / knowledge_search.
- chunk_id (optional): Short cN ID for a single non-FAQ chunk.
- knowledge_id (optional): Short dN document ID to page through all chunks.
- limit / offset: Only for knowledge_id paging (default limit 20, max 100).

## Output:
Full chunk content. FAQ entries include <faq> with <answer> from metadata.`,
	schema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "faq_id": {
      "type": "string",
      "description": "Short cN FAQ chunk ID. Use for FAQ hits instead of the parent dN document ID."
    },
    "chunk_id": {
      "type": "string",
      "description": "Short cN ID for one non-FAQ chunk"
    },
    "knowledge_id": {
      "type": "string",
      "description": "Short dN document ID to list all chunks"
    },
    "limit": {
      "type": "integer",
      "description": "Chunks per page when using knowledge_id (default 20, max 100)",
      "default": 20,
      "minimum": 1,
      "maximum": 100
    },
    "offset": {
      "type": "integer",
      "description": "Start position when using knowledge_id (default 0)",
      "default": 0,
      "minimum": 0
    }
  }
}`),
}

// ListKnowledgeChunksInput defines the input parameters for list knowledge chunks tool
type ListKnowledgeChunksInput struct {
	KnowledgeID string `json:"knowledge_id,omitempty"`
	FAQID       string `json:"faq_id,omitempty"`
	ChunkID     string `json:"chunk_id,omitempty"`
	Limit       int    `json:"limit"`
	Offset      int    `json:"offset"`
}

// ListKnowledgeChunksTool retrieves chunk snapshots for a specific knowledge document.
type ListKnowledgeChunksTool struct {
	BaseTool
	chunkService     interfaces.ChunkService
	knowledgeService interfaces.KnowledgeService
	searchTargets    types.SearchTargets // Pre-computed unified search targets with KB-tenant mapping
}

// NewListKnowledgeChunksTool creates a new tool instance.
func NewListKnowledgeChunksTool(
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
	searchTargets types.SearchTargets,
) *ListKnowledgeChunksTool {
	return &ListKnowledgeChunksTool{
		BaseTool:         listKnowledgeChunksTool,
		chunkService:     chunkService,
		knowledgeService: knowledgeService,
		searchTargets:    searchTargets,
	}
}

// Execute performs the chunk fetch against the chunk service.
func (t *ListKnowledgeChunksTool) Execute(ctx context.Context, args json.RawMessage) (*types.ToolResult, error) {
	// Parse args from json.RawMessage
	var input ListKnowledgeChunksInput
	if err := json.Unmarshal(args, &input); err != nil {
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Failed to parse args: %v", err),
		}, err
	}

	chunkID := strings.TrimSpace(input.FAQID)
	if chunkID == "" {
		chunkID = strings.TrimSpace(input.ChunkID)
	}
	if chunkID != "" {
		return t.executeByChunkID(ctx, chunkID)
	}

	knowledgeID := strings.TrimSpace(input.KnowledgeID)
	if knowledgeID == "" {
		return &types.ToolResult{
			Success: false,
			Error:   "one of faq_id, chunk_id, or knowledge_id is required",
		}, fmt.Errorf("missing id parameter")
	}

	knowledge, err := authorizeKnowledgeInSearchTargets(ctx, t.searchTargets, knowledgeID, t.knowledgeService)
	if err != nil {
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("Knowledge is not accessible: %v", err),
		}, err
	}

	// Use the knowledge's actual tenant_id for chunk query (supports cross-tenant shared KB)
	effectiveTenantID := knowledge.TenantID

	chunkLimit := 20
	switch {
	case input.Limit <= 0:
		// keep default
	case input.Limit > listKnowledgeChunksMaxLimit:
		slog.InfoContext(ctx, "list_knowledge_chunks clamped limit",
			"requested", input.Limit, "capped", listKnowledgeChunksMaxLimit)
		chunkLimit = listKnowledgeChunksMaxLimit
	default:
		chunkLimit = input.Limit
	}
	offset := 0
	if input.Offset > 0 {
		offset = input.Offset
	}
	if offset < 0 {
		offset = 0
	}

	pagination := &types.Pagination{
		Page:     offset/chunkLimit + 1,
		PageSize: chunkLimit,
	}

	enabled := true
	chunks, total, err := t.chunkService.GetRepository().ListPagedChunksByKnowledgeID(ctx,
		effectiveTenantID, knowledgeID, pagination, []types.ChunkType{types.ChunkTypeText, types.ChunkTypeFAQ}, nil, "", "", "", "", &enabled)
	if err != nil {
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("failed to list chunks: %v", err),
		}, err
	}
	if chunks == nil {
		return &types.ToolResult{
			Success: false,
			Error:   "chunk query returned no data",
		}, fmt.Errorf("chunk query returned no data")
	}

	totalChunks := total
	fetched := len(chunks)

	// When the caller paged past the end (offset >= total with total > 0),
	// recover the final page locally. Returning a paging error here can make a
	// valid document look unavailable and truncate the model's final answer.
	if fetched == 0 && totalChunks > 0 && int64(offset) >= totalChunks {
		// Models occasionally request the next page after a document's final
		// page. Recover locally so a valid document is not reported as a failed
		// retrieval and the final answer is not truncated by a paging error.
		pagination.Page = int((totalChunks-1)/int64(chunkLimit)) + 1
		chunks, total, err = t.chunkService.GetRepository().ListPagedChunksByKnowledgeID(ctx,
			effectiveTenantID, knowledgeID, pagination, []types.ChunkType{types.ChunkTypeText, types.ChunkTypeFAQ}, nil, "", "", "", "", &enabled)
		if err != nil {
			return &types.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("failed to list chunks: %v", err),
			}, err
		}
		if chunks == nil {
			return &types.ToolResult{
				Success: false,
				Error:   "chunk query returned no data",
			}, fmt.Errorf("chunk query returned no data")
		}
		totalChunks = total
		fetched = len(chunks)
	}

	// Enrich image info from child image chunks (lazy loading)
	if fetched > 0 {
		chunkIDs := make([]string, 0, fetched)
		for _, c := range chunks {
			chunkIDs = append(chunkIDs, c.ID)
		}
		infoMap := searchutil.CollectImageInfoByChunkIDs(ctx, t.chunkService.GetRepository(), effectiveTenantID, chunkIDs)
		for _, c := range chunks {
			if c.ImageInfo == "" {
				if merged, ok := infoMap[c.ID]; ok {
					c.ImageInfo = merged
				}
			}
		}
	}

	knowledgeTitle := strings.TrimSpace(knowledge.Title)
	knowledgeMetadata := knowledge.GetMetadata()

	output := t.buildOutput(knowledgeID, knowledgeTitle, totalChunks, fetched, chunks)

	formattedChunks := make([]map[string]interface{}, 0, len(chunks))
	for idx, c := range chunks {
		metadataRef := videoevidence.EnrichSearchResult(&types.SearchResult{
			ID: c.ID, Content: c.Content, ChunkType: c.ChunkType,
			KnowledgeID: c.KnowledgeID, KnowledgeTitle: knowledgeTitle,
			Metadata: cloneStringMetadata(knowledgeMetadata), ChunkMetadata: c.Metadata,
		})
		chunkData := map[string]interface{}{
			"seq":             idx + 1,
			"chunk_id":        c.ID,
			"chunk_index":     c.ChunkIndex,
			"content":         c.Content,
			"chunk_type":      c.ChunkType,
			"knowledge_id":    c.KnowledgeID,
			"knowledge_base":  c.KnowledgeBaseID,
			"start_at":        c.StartAt,
			"end_at":          c.EndAt,
			"parent_chunk_id": c.ParentChunkID,
			"metadata":        metadataRef.Metadata,
		}

		appendFAQChunkData(chunkData, c)
		normalizeFAQChunkDataMap(chunkData, c)

		// 添加图片信息
		if c.ImageInfo != "" {
			var imageInfos []types.ImageInfo
			if err := json.Unmarshal([]byte(c.ImageInfo), &imageInfos); err == nil && len(imageInfos) > 0 {
				imageList := make([]map[string]string, 0, len(imageInfos))
				for _, img := range imageInfos {
					imgData := make(map[string]string)
					if img.URL != "" {
						imgData["url"] = img.URL
					}
					if img.Caption != "" {
						imgData["caption"] = img.Caption
					}
					if img.OCRText != "" {
						imgData["ocr_text"] = img.OCRText
					}
					if len(imgData) > 0 {
						imageList = append(imageList, imgData)
					}
				}
				if len(imageList) > 0 {
					chunkData["images"] = imageList
				}
			}
		}

		formattedChunks = append(formattedChunks, chunkData)
	}

	return &types.ToolResult{
		Success: true,
		Output:  output,
		Data: map[string]interface{}{
			"display_type":    "knowledge_chunks_list",
			"knowledge_id":    knowledgeID,
			"knowledge_title": knowledgeTitle,
			"total_chunks":    totalChunks,
			"fetched_chunks":  fetched,
			"page":            pagination.Page,
			"page_size":       pagination.PageSize,
			"chunks":          formattedChunks,
		},
	}, nil
}

// executeByChunkID loads one chunk by faq_id / chunk_id (FAQ entry or any chunk).
func (t *ListKnowledgeChunksTool) executeByChunkID(ctx context.Context, chunkID string) (*types.ToolResult, error) {
	chunk, err := authorizeChunkInSearchTargets(
		ctx, t.searchTargets, chunkID, t.chunkService, t.knowledgeService,
	)
	if err != nil {
		return &types.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("chunk is not accessible: %v", err),
		}, err
	}

	chunks := []*types.Chunk{chunk}
	if chunk.ImageInfo == "" {
		effectiveTenantID := t.searchTargets.GetTenantIDForKB(chunk.KnowledgeBaseID)
		if effectiveTenantID > 0 {
			infoMap := searchutil.CollectImageInfoByChunkIDs(ctx, t.chunkService.GetRepository(), effectiveTenantID, []string{chunk.ID})
			if merged, ok := infoMap[chunk.ID]; ok {
				chunk.ImageInfo = merged
			}
		}
	}

	knowledgeTitle, knowledgeMetadata := t.lookupKnowledgeInfo(ctx, chunk.KnowledgeID)
	output := t.buildOutput(chunk.KnowledgeID, knowledgeTitle, 1, 1, chunks)

	formattedChunks := []map[string]interface{}{
		{
			"seq":            1,
			"chunk_id":       chunk.ID,
			"chunk_index":    chunk.ChunkIndex,
			"content":        chunk.Content,
			"chunk_type":     chunk.ChunkType,
			"knowledge_id":   chunk.KnowledgeID,
			"knowledge_base": chunk.KnowledgeBaseID,
		},
	}
	metadataRef := videoevidence.EnrichSearchResult(&types.SearchResult{
		ID: chunk.ID, Content: chunk.Content, ChunkType: chunk.ChunkType,
		KnowledgeID: chunk.KnowledgeID, KnowledgeTitle: knowledgeTitle,
		Metadata: cloneStringMetadata(knowledgeMetadata), ChunkMetadata: chunk.Metadata,
	})
	formattedChunks[0]["metadata"] = metadataRef.Metadata
	appendFAQChunkData(formattedChunks[0], chunk)
	normalizeFAQChunkDataMap(formattedChunks[0], chunk)

	data := map[string]interface{}{
		"display_type":    "knowledge_chunks_list",
		"knowledge_id":    chunk.KnowledgeID,
		"knowledge_title": knowledgeTitle,
		"total_chunks":    int64(1),
		"fetched_chunks":  1,
		"page":            1,
		"page_size":       1,
		"chunks":          formattedChunks,
		"faq_id":          chunk.ID,
		"single_chunk":    true,
	}
	if q := faqStandardQuestion(chunk); q != "" {
		data["faq_question"] = q
	}

	return &types.ToolResult{
		Success: true,
		Output:  output,
		Data:    data,
	}, nil
}

// lookupKnowledgeInfo looks up the title and backend-owned metadata of a
// knowledge document.
// Uses GetKnowledgeByIDOnly to support cross-tenant shared KB
func (t *ListKnowledgeChunksTool) lookupKnowledgeInfo(ctx context.Context, knowledgeID string) (string, map[string]string) {
	if t.knowledgeService == nil {
		return "", nil
	}
	knowledge, err := t.knowledgeService.GetKnowledgeByIDOnly(ctx, knowledgeID)
	if err != nil || knowledge == nil {
		return "", nil
	}
	return strings.TrimSpace(knowledge.Title), knowledge.GetMetadata()
}

func cloneStringMetadata(source map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

// buildOutput builds the output as XML for the list knowledge chunks tool
func (t *ListKnowledgeChunksTool) buildOutput(
	knowledgeID string,
	knowledgeTitle string,
	total int64,
	fetched int,
	chunks []*types.Chunk,
) string {
	var b strings.Builder

	titleAttr := ""
	if knowledgeTitle != "" {
		titleAttr = fmt.Sprintf(" title=\"%s\"", knowledgeTitle)
	}
	fmt.Fprintf(&b, "<knowledge_chunks knowledge_id=\"%s\"%s total=\"%d\" fetched=\"%d\">\n",
		knowledgeID, titleAttr, total, fetched)

	if fetched == 0 {
		b.WriteString("</knowledge_chunks>")
		return b.String()
	}

	for _, c := range chunks {
		if c.ChunkType == types.ChunkTypeFAQ {
			writeFAQEntryXML(&b, c)
			writeChunkImagesMarkdown(&b, c)
			continue
		}

		if q := faqStandardQuestion(c); q != "" {
			fmt.Fprintf(&b, "<chunk chunk_id=\"%s\" chunk_index=\"%d\" type=\"%s\" question=\"%s\">\n",
				c.ID, c.ChunkIndex, c.ChunkType, xmlEscape(q))
		} else {
			fmt.Fprintf(&b, "<chunk chunk_id=\"%s\" chunk_index=\"%d\" type=\"%s\">\n",
				c.ID, c.ChunkIndex, c.ChunkType)
		}
		fmt.Fprintf(&b, "<content>%s</content>\n", summarizeContent(c.Content))
		writeChunkImagesMarkdown(&b, c)
		b.WriteString("</chunk>\n")
	}

	if int64(fetched) < total {
		fmt.Fprintf(&b, "<pagination remaining=\"%d\" />\n", int64(total)-int64(fetched))
	}

	b.WriteString("</knowledge_chunks>")
	return b.String()
}

func writeChunkImagesMarkdown(b *strings.Builder, c *types.Chunk) {
	if c == nil || c.ImageInfo == "" {
		return
	}
	var imageInfos []types.ImageInfo
	if err := json.Unmarshal([]byte(c.ImageInfo), &imageInfos); err != nil || len(imageInfos) == 0 {
		return
	}
	for _, img := range imageInfos {
		if imageMarkdown := searchutil.BuildImageInfoMarkdownWithURL(img.URL, &img); imageMarkdown != "" {
			b.WriteString(imageMarkdown)
			b.WriteString("\n")
		}
	}
}

// faqStandardQuestion returns the FAQ standard question for an FAQ-type chunk,
// or "" for non-FAQ chunks (or when metadata is missing/unparseable). All FAQ
// entries inside one knowledge share the same knowledge title, so surfacing the
// standard question gives each entry a distinct, human-readable identity in
// tool output that would otherwise look like duplicate same-titled chunks.
func faqStandardQuestion(c *types.Chunk) string {
	if c == nil || c.ChunkType != types.ChunkTypeFAQ {
		return ""
	}
	meta, err := c.FAQMetadata()
	if err != nil || meta == nil {
		return ""
	}
	return strings.TrimSpace(meta.StandardQuestion)
}

// summarizeContent summarizes the content of a chunk
func summarizeContent(content string) string {
	cleaned := strings.TrimSpace(content)
	if cleaned == "" {
		return "(empty)"
	}

	return strings.TrimSpace(string(cleaned))
}
