import { get } from '@/utils/request'
import type { WikiGraphData } from '@/api/wiki'

export interface RelatedKnowledgePayload {
  videoId: string
  knowledgeBaseId: string
  center: { slug: string; title: string; pageType: string } | null
}

export async function fetchRelatedKnowledge(videoId: string): Promise<RelatedKnowledgePayload> {
  const response: any = await get(`/api/custom/videos/${videoId}/wiki-graph-context`)
  const payload = response?.data || response
  return {
    videoId: payload.video_id || videoId,
    knowledgeBaseId: payload.knowledge_base_id || '',
    center: payload.center ? {
      slug: payload.center.slug,
      title: payload.center.title,
      pageType: payload.center.page_type,
    } : null,
  }
}

// The custom endpoint validates that the graph center is the requesting
// video's summary Wiki page, then forwards WeKnora's native Wiki graph.
export async function fetchNativeWikiGraph(videoId: string): Promise<WikiGraphData> {
  const response: any = await get(`/api/custom/videos/${videoId}/wiki-graph`)
  return response?.data || response
}
