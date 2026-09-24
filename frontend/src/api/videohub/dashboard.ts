import { get, post } from '@/utils/request'
import type { DashboardPayload, DashboardRequest } from '@/types/videohub'
import { validateDashboardRequest } from './dashboardValidation'

export async function fetchDashboard(req: DashboardRequest): Promise<DashboardPayload> {
  validateDashboardRequest(req)
  const response = await get<{ success: boolean; data?: DashboardPayload; error?: string }>('/api/custom/dashboard', {
    params: { range: req.range, from: req.from, to: req.to },
  })
  if (!response.success || !response.data) throw new Error(response.error || '提问看板加载失败')
  return response.data
}

export interface DashboardQuestionEventInput {
  event_id: string
  session_id: string
  video_id?: string
  video_seconds?: number
  question: string
}

export async function recordDashboardQuestion(input: DashboardQuestionEventInput): Promise<void> {
  await post('/api/custom/dashboard/questions', input)
}

export interface ChatSourceAuditInput {
  event_id: string
  session_id: string
  scope: 'global' | 'video'
  video_id?: string
  capability_version?: 'v1'
  route_mode?: 'quick' | 'reasoning'
  coverage?: 'complete' | 'partial' | 'none'
  source_mode: 'wiki' | 'chunk' | 'wiki_and_chunk' | 'none'
  fallback_used: boolean
  references_found: number
  linkable_evidence?: number
  invalid_references?: number
  degradation_code?:
    | 'no_evidence'
    | 'partial_coverage'
    | 'invalid_evidence'
    | 'skill_unavailable'
    | 'reasoning_budget_exceeded'
    | 'reasoning_timeout'
    | 'answer_contract_truncated'
    | 'answer_contract_invalid'
    | 'empty_response'
    | 'final_answer_generation_failed'
    | 'llm_call_failed_after_tool_results'
    | 'context_cancelled'
    | 'production_graph_missing_audited_wiki_write'
  wiki_page_ids: string[]
  knowledge_object_ids: string[]
  transcript_chunk_ids: string[]
}

export async function recordChatSourceAudit(input: ChatSourceAuditInput): Promise<void> {
  await post('/api/custom/chat/source-audit', input)
}
