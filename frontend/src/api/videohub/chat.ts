import { fetchEventSource } from '@microsoft/fetch-event-source'
import { get, post } from '@/utils/request'
import { getApiBaseUrl } from '@/utils/api-base'
import type {
  ChatKnowledgeReference,
  ChatMessage,
  ChatSession,
  EvidenceLink,
  VideoData,
  VideoEvidence,
  WikiFallback,
} from '@/types/videohub'
import {
  displayQuestionFromStoredContent,
  dedupeNativeAnswerEvents,
  failureMessageForReason,
  isChatFailureReason,
  mergeLocalTurnWithStoredMessages,
  parseWeKnoraStreamChunk,
  shouldAbortStream,
} from './chatStream'
import { buildChatRequest, normalizeChatError, normalizeTenantId, resolveChatScopeAgent, type ChatRequestScope } from './chatRequest'
import { recordChatSourceAudit, recordDashboardQuestion } from './dashboard'

interface ScopeResponse extends ChatRequestScope {
  video_id?: string
  video_title?: string
  video_cover_url?: string
  execution_scope?: 'current_video' | 'global_videos'
  scope_resolution?: string
  session_meta: Record<string, string>
}

interface WeKnoraSession {
  id: string
  title?: string
  description?: string
  created_at?: string
  updated_at?: string
}

interface WeKnoraMessage {
  id: string
  content: string
  role: 'user' | 'assistant' | 'system'
  created_at?: string
  updated_at?: string
  knowledge_references?: KnowledgeReference[]
  is_completed?: boolean
}

interface KnowledgeReference extends ChatKnowledgeReference {}

interface EvidenceLookupItem {
  knowledge_id: string
  video_id: string
  video_title: string
  video_cover_url: string
  start_ms: number
  end_ms: number
  start_seconds: number
  end_seconds: number
  seconds: number
  timestamp: string
  evidence_sentence_id: string
  transcript_generation: string
  source_type: string
  linkable: boolean
  wiki_page_id?: string
  wiki_page_slug?: string
  wiki_page_title?: string
  wiki_knowledge_base_id?: string
  wiki_linkable?: boolean
}

interface ApiEnvelope<T> {
  data?: T
  success?: boolean
  total?: number
}

interface SendOptions {
  currentVideo?: VideoData
  currentTime?: number
  globalMode?: boolean
  agentId?: string
  agentEnabled?: boolean
  autoRoute?: boolean
  agentSourceTenantId?: string | null
  onMessage?: (message: ChatMessage) => void
  onStreamMessage?: (message: StreamingChatMessage) => void
  onSessionCreated?: (session: ChatSession) => void
}

interface TurnOptions extends SendOptions {
  session?: ChatSession
}

interface StreamAnswerOptions {
  onChunk?: (message: StreamingChatMessage) => void
}

export interface StreamingChatMessage extends ChatMessage {
  thinkingText?: string
  activityText?: string
  role?: 'assistant' | 'user'
  content?: string
  isAgentMode?: boolean
  is_completed?: boolean
  request_id?: string
  assistant_message_id?: string
  failure_reason?: import('./chatStream').ChatFailureReason
  agentEventStream?: Record<string, unknown>[]
  knowledge_references?: KnowledgeReference[]
}

interface StreamingAnswerResult {
  answer: string
  streamMessage: StreamingChatMessage
}

interface WeKnoraStreamPayload {
  response_type?: string
  type?: string
  id?: string
  assistant_message_id?: string
  content?: string
  data?: Record<string, unknown>
  done?: boolean
}

const VIDEOHUB_META_PREFIX = 'videohub:'
const KB_TAG_RE = /<kb\b([^>]*?)\s*\/?>/gi
const TAG_ATTR_RE = /([\w-]+)\s*=\s*"([^"]*)"/g

function messageId() {
  return `message-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`
}

function questionEventID() {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  return messageId()
}

function nowLabel() {
  return new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
}

function formatTime(seconds: number) {
  const safe = Math.max(0, Math.floor(seconds))
  return `${String(Math.floor(safe / 60)).padStart(2, '0')}:${String(safe % 60).padStart(2, '0')}`
}

function formatTimeRange(startSeconds: number, endSeconds: number) {
  const start = Math.max(0, Math.floor(startSeconds))
  const end = Math.max(start, Math.floor(endSeconds))
  return `${formatTime(start)}–${formatTime(end)}`
}

function referenceLookupKeys(reference: KnowledgeReference): string[] {
  const metadata = reference.metadata || {}
  return [
    reference.knowledge_id,
    reference.id,
    ...(Array.isArray(reference.chunk_ids) ? reference.chunk_ids : []),
    metadata.source_chunk_id,
    metadata.chunk_id,
    metadata.evidence_sentence_id,
    metadata.source_evidence_id,
    metadata.evidence_id,
  ].map(value => String(value || '').trim()).filter(Boolean)
}

function parseCitationAttributes(attrString: string): Record<string, string> {
  const attrs: Record<string, string> = {}
  TAG_ATTR_RE.lastIndex = 0
  let match: RegExpExecArray | null
  while ((match = TAG_ATTR_RE.exec(attrString)) !== null) {
    attrs[match[1]] = match[2]
  }
  return attrs
}

function referencesFromCitationTags(content: string): KnowledgeReference[] {
  const refs: KnowledgeReference[] = []
  KB_TAG_RE.lastIndex = 0
  let match: RegExpExecArray | null
  while ((match = KB_TAG_RE.exec(content || '')) !== null) {
    const attrs = parseCitationAttributes(match[1] || '')
    const chunkID = String(attrs.chunk_id || attrs.chunkId || '').trim()
    if (!chunkID) continue
    refs.push({
      id: chunkID,
      knowledge_id: attrs.knowledge_id || undefined,
      knowledge_title: attrs.doc || undefined,
      knowledge_base_id: attrs.kb_id || attrs.kbId || undefined,
      metadata: {
        ...(attrs.doc ? { title: attrs.doc } : {}),
        ...(attrs.kb_id || attrs.kbId ? { knowledge_base_id: attrs.kb_id || attrs.kbId } : {}),
        chunk_id: chunkID,
      },
    })
  }
  return refs
}

function messageReferences(message: WeKnoraMessage): KnowledgeReference[] {
  const stored = message.knowledge_references || []
  if (stored.length) return stored
  return referencesFromCitationTags(message.content || '')
}

function referenceIdentity(reference: KnowledgeReference) {
  return referenceLookupKeys(reference)[0] || reference.id || reference.knowledge_id || reference.knowledge_title || ''
}

function mergeKnowledgeReferences(existing: KnowledgeReference[], incoming: KnowledgeReference[]) {
  if (!existing.length) return incoming
  if (!incoming.length) return existing
  const merged = [...existing]
  const seen = new Set(existing.map(referenceIdentity).filter(Boolean))
  for (const reference of incoming) {
    const key = referenceIdentity(reference)
    if (key && seen.has(key)) continue
    if (key) seen.add(key)
    merged.push(reference)
  }
  return merged
}

function videoEvidenceFromLookup(item?: EvidenceLookupItem): VideoEvidence | undefined {
  if (!item || !item.linkable || !item.video_id || !item.evidence_sentence_id || !item.transcript_generation) {
    return undefined
  }
  return {
    knowledgeId: item.knowledge_id,
    videoId: item.video_id,
    videoTitle: item.video_title,
    videoCoverUrl: item.video_cover_url || undefined,
    startMs: item.start_ms,
    endMs: item.end_ms,
    startSeconds: item.start_seconds,
    endSeconds: item.end_seconds,
    startTimestamp: formatTime(item.start_seconds),
    endTimestamp: formatTime(item.end_seconds),
    evidenceSentenceId: item.evidence_sentence_id,
    transcriptGeneration: item.transcript_generation,
    sourceType: 'transcript',
    linkable: true,
  }
}

function wikiFallbackFromLookup(item?: EvidenceLookupItem): WikiFallback | undefined {
  if (!item || !item.wiki_linkable || !item.wiki_page_id || !item.wiki_page_slug || !item.wiki_knowledge_base_id) {
    return undefined
  }
  return {
    pageId: item.wiki_page_id,
    slug: item.wiki_page_slug,
    title: item.wiki_page_title || 'Wiki 页面',
    knowledgeBaseId: item.wiki_knowledge_base_id,
    linkable: true,
  }
}

function evidenceLookupForReference(
  reference: KnowledgeReference,
  evidenceByKey: Map<string, EvidenceLookupItem>,
): EvidenceLookupItem | undefined {
  return referenceLookupKeys(reference).map(key => evidenceByKey.get(key)).find(Boolean)
}

function evidenceForReference(
  reference: KnowledgeReference,
  evidenceByKey: Map<string, EvidenceLookupItem>,
): VideoEvidence | undefined {
  const evidence = videoEvidenceFromLookup(evidenceLookupForReference(reference, evidenceByKey))
  if (evidence) return evidence
  return reference.video_evidence?.linkable ? reference.video_evidence : undefined
}

function hydrateKnowledgeReferences(
  references: KnowledgeReference[],
  evidenceByKey: Map<string, EvidenceLookupItem>,
): KnowledgeReference[] {
  return references.map(reference => {
    const videoEvidence = evidenceForReference(reference, evidenceByKey)
    const lookup = evidenceLookupForReference(reference, evidenceByKey)
    const wikiFallback = wikiFallbackFromLookup(lookup)
    const videoEvidenceUnavailable = Boolean(
      lookup && lookup.source_type === 'transcript' && !lookup.linkable,
    )
    return {
      ...reference,
      ...(videoEvidence ? { video_evidence: videoEvidence } : {}),
      ...(wikiFallback ? { wiki_fallback: wikiFallback } : {}),
      ...(videoEvidenceUnavailable ? { video_evidence_unavailable: true } : {}),
    }
  })
}

function escapeCitationAttribute(value: string) {
  return String(value || '')
    .replace(/&/g, '&amp;')
    .replace(/"/g, '&quot;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
}

function fallbackAnswerFromVideoEvidence(references: KnowledgeReference[]) {
  const usable = references
    .filter(reference => reference.video_evidence?.linkable && reference.video_evidence.videoId)
    .slice(0, 8)
  if (!usable.length) return ''

  const lines = usable.map(reference => {
    const evidence = reference.video_evidence!
    const citationID = reference.id || reference.knowledge_id || evidence.evidenceSentenceId
    const doc = escapeCitationAttribute(evidence.videoTitle || reference.knowledge_title || '相关视频')
    const chunkID = escapeCitationAttribute(citationID)
    return `- <kb doc="${doc}" chunk_id="${chunkID}" /> 找到相关内容。`
  })
  return `已找到相关视频定位：\n\n${lines.join('\n')}`
}

function formatRelativeTime(input?: string) {
  if (!input) return '最近'
  const value = new Date(input).getTime()
  if (!Number.isFinite(value)) return '最近'
  const diff = Date.now() - value
  if (diff < 60_000) return '刚刚'
  if (diff < 3_600_000) return `${Math.max(1, Math.floor(diff / 60_000))} 分钟前`
  if (diff < 86_400_000) return `${Math.max(1, Math.floor(diff / 3_600_000))} 小时前`
  if (diff < 172_800_000) return '昨天'
  return new Date(input).toLocaleDateString('zh-CN')
}

function sessionDescription(meta: Record<string, string>) {
  return `${VIDEOHUB_META_PREFIX}${JSON.stringify(meta)}`
}

function parseSessionMeta(description?: string): Record<string, string> {
  const value = (description || '').trim()
  if (!value.startsWith(VIDEOHUB_META_PREFIX)) return {}
  try {
    const parsed = JSON.parse(value.slice(VIDEOHUB_META_PREFIX.length))
    return parsed && typeof parsed === 'object' ? parsed : {}
  } catch {
    return {}
  }
}

function isVideohubSession(session: WeKnoraSession) {
  return (session.description || '').trim().startsWith(VIDEOHUB_META_PREFIX)
}

function unwrapData<T>(response: ApiEnvelope<T> | T): T {
  if (response && typeof response === 'object' && 'data' in response) {
    return (response as ApiEnvelope<T>).data as T
  }
  return response as T
}

function mapMessage(message: WeKnoraMessage, evidenceByKnowledgeID = new Map<string, EvidenceLookupItem>()): ChatMessage {
  const references = hydrateKnowledgeReferences(messageReferences(message), evidenceByKnowledgeID)
  const evidenceLinks = evidenceLinksFromReferences(references, evidenceByKnowledgeID)
  const firstEvidence = evidenceLinks.find(item => item.videoId)
  const rawContent = message.content || ''
  return {
    id: message.id || messageId(),
    sender: message.role === 'user' ? 'user' : 'assistant',
    text: message.role === 'user' ? displayQuestionFromStoredContent(rawContent) : cleanAnswer(rawContent),
    timestamp: formatRelativeTime(message.created_at || message.updated_at),
    relatedVideoId: firstEvidence?.videoId,
    relatedVideoTitle: firstEvidence?.videoTitle,
    relatedTime: firstEvidence?.seconds,
    evidenceLinks,
    knowledge_references: references,
  }
}

function evidenceLinksFromReferences(references: KnowledgeReference[], evidenceByKnowledgeID: Map<string, EvidenceLookupItem>): EvidenceLink[] {
  const result: EvidenceLink[] = []
  const seen = new Set<string>()
  for (const reference of references) {
    const knowledgeID = reference.knowledge_id || ''
    const evidence = evidenceForReference(reference, evidenceByKnowledgeID)
    const lookup = referenceLookupKeys(reference).map(key => evidenceByKnowledgeID.get(key)).find(Boolean)
    const metadata = reference.metadata || {}
    const metadataStartMs = Number(metadata.start_ms)
    const metadataEndMs = Number(metadata.end_ms)
    const hasMetadataStart = Number.isFinite(metadataStartMs) && metadataStartMs >= 0
    const seconds = hasMetadataStart
      ? Math.floor(metadataStartMs / 1000)
      : evidence?.startSeconds ?? lookup?.start_seconds ?? 0
    const endSeconds = Number.isFinite(metadataEndMs) && metadataEndMs >= metadataStartMs
      ? Math.floor(metadataEndMs / 1000)
      : evidence?.endSeconds ?? lookup?.end_seconds ?? seconds
    const label = evidence?.videoTitle || lookup?.video_title || reference.knowledge_title || metadata.source_filename || '知识来源'
    const timestamp = evidence
      ? formatTimeRange(evidence.startSeconds, evidence.endSeconds)
      : lookup?.timestamp || formatTimeRange(seconds, endSeconds)
    const key = `${reference.knowledge_id || reference.id || knowledgeID}:${seconds}:${endSeconds}:${label}`
    if (seen.has(key)) continue
    seen.add(key)
    result.push({
      label,
      timestamp,
      seconds,
      endSeconds,
      startMs: evidence?.startMs ?? lookup?.start_ms,
      endMs: evidence?.endMs ?? lookup?.end_ms,
      videoId: evidence?.videoId || lookup?.video_id || metadata.video_id,
      videoTitle: evidence?.videoTitle || lookup?.video_title || label,
      videoEvidence: evidence,
    })
  }
  return result
}

function questionForScope(question: string, scope: ScopeResponse) {
  if (scope.scope !== 'video') return question
  return [
    `用户正在视频详情页围绕《${scope.video_title || '当前视频'}》提问。当前视频 ID：${scope.video_id || 'unknown'}。`,
    '必须先使用 Wiki 搜索读取当前视频已审计通过的知识对象页面，优先引用页面中的可读正文和结构化字段。',
    '只有 Wiki 页面不存在、检索不到或无法回答时，才使用当前视频同一转写代次的句级分块作为证据回溯；不得把未审计页面当作事实来源。',
    '回答必须区分知识对象结论与转写原文证据；引用来源时保留 Wiki 页面或转写分块的真实来源。',
    `用户问题：${question}`,
  ].join('\n')
}

async function getScope(options?: SendOptions): Promise<ScopeResponse> {
  if (options?.currentVideo && !options.globalMode && options.currentVideo.id !== '__global__') {
    const res = await get<ApiEnvelope<ScopeResponse>>(`/api/custom/videos/${options.currentVideo.id}/chat-scope`)
    return unwrapData(res)
  }
  const res = await get<ApiEnvelope<ScopeResponse>>('/api/custom/chat/scope/global')
  return unwrapData(res)
}

async function createSession(question: string, scope: ScopeResponse): Promise<WeKnoraSession> {
  const titlePrefix = scope.scope === 'video' && scope.video_title ? `《${scope.video_title}》` : '全局视频问答'
  const title = `${titlePrefix}：${question}`.slice(0, 80)
  const res = await post<ApiEnvelope<WeKnoraSession>>('/api/v1/sessions', {
    title,
    description: sessionDescription(scope.session_meta || { scope: scope.scope }),
  }, tenantRequestConfig(scope.tenant_id))
  return unwrapData(res)
}

function recordQuestionBestEffort(
  question: string,
  scope: ScopeResponse,
  sessionID: string,
  currentVideo?: VideoData,
  currentTime?: number,
) {
  const eventID = questionEventID()
  void recordDashboardQuestion({
    event_id: eventID,
    session_id: sessionID,
    ...((scope.video_id || currentVideo?.id) ? { video_id: scope.video_id || currentVideo?.id } : {}),
    video_seconds: Math.max(0, Math.floor(currentTime || 0)),
    question,
  }).catch(error => {
    console.warn('record dashboard question failed', error)
  })
  return eventID
}

async function recordSourceAuditBestEffort(
  eventID: string,
  sessionID: string,
  scope: ScopeResponse,
  messages: WeKnoraMessage[],
  outcome?: StreamingChatMessage,
) {
  const assistant = [...messages].reverse().find(message => message.role === 'assistant' && (message.knowledge_references || []).length)
  const references = assistant?.knowledge_references || []
  const knowledgeIDs = references.map(reference => reference.knowledge_id || '').filter(Boolean)
  const evidenceByKnowledgeID = await lookupEvidence(knowledgeIDs)
  const wikiPageIDs = new Set<string>()
  const knowledgeObjectIDs = new Set<string>()
  const transcriptChunkIDs = new Set<string>()
  for (const reference of references) {
    const knowledgeID = reference.knowledge_id || ''
    const metadata = reference.metadata || {}
    const sourceType = String(metadata.source_type || metadata.source_kind || metadata.source || '').toLowerCase()
    const wikiPageID = String(metadata.wiki_page_id || metadata.page_id || '').trim()
    const knowledgeObjectID = String(metadata.knowledge_object_id || metadata.object_id || '').trim()
    if (wikiPageID || sourceType.includes('wiki') || sourceType.includes('page')) {
      if (wikiPageID) wikiPageIDs.add(wikiPageID)
      if (knowledgeObjectID) knowledgeObjectIDs.add(knowledgeObjectID)
      continue
    }
    if (knowledgeID && (sourceType.includes('chunk') || sourceType.includes('transcript') || evidenceByKnowledgeID.has(knowledgeID))) {
      transcriptChunkIDs.add(knowledgeID)
    }
  }
  const hasWiki = wikiPageIDs.size > 0
  const hasChunk = transcriptChunkIDs.size > 0
  const sourceMode = hasWiki && hasChunk ? 'wiki_and_chunk' : hasWiki ? 'wiki' : hasChunk ? 'chunk' : 'none'
  const linkableEvidence = outcome?.video_evidence?.filter(item => item.linkable).length || 0
  const invalidReferences = references.filter(reference => reference.video_evidence_unavailable).length
  const coverage = outcome?.coverage
  const degradationCode = outcome?.failure_reason
    || (invalidReferences > 0 ? 'invalid_evidence' : coverage === 'partial' ? 'partial_coverage' : coverage === 'none' ? 'no_evidence' : undefined)
  await recordChatSourceAudit({
    event_id: eventID,
    session_id: sessionID,
    scope: scope.scope,
    ...(scope.video_id ? { video_id: scope.video_id } : {}),
    ...(coverage || outcome?.route_mode ? { capability_version: 'v1' } : {}),
    ...(outcome?.route_mode ? { route_mode: outcome.route_mode } : {}),
    ...(coverage ? { coverage } : {}),
    source_mode: sourceMode,
    fallback_used: !hasWiki && hasChunk,
    references_found: references.length,
    linkable_evidence: linkableEvidence,
    invalid_references: invalidReferences,
    ...(degradationCode ? { degradation_code: degradationCode } : {}),
    wiki_page_ids: [...wikiPageIDs],
    knowledge_object_ids: [...knowledgeObjectIDs],
    transcript_chunk_ids: [...transcriptChunkIDs],
  })
}

function tenantRequestConfig(tenantID?: string | number | null) {
  const normalized = normalizeTenantId(tenantID)
  return normalized ? { headers: { 'X-Tenant-ID': normalized } } : undefined
}

async function loadSessionMessages(sessionID: string, tenantID?: string | number | null): Promise<WeKnoraMessage[]> {
  const res = await get<ApiEnvelope<WeKnoraMessage[]>>(
    `/api/v1/messages/${sessionID}/load?limit=100`,
    tenantRequestConfig(tenantID),
  )
  return unwrapData(res) || []
}

async function lookupEvidence(knowledgeIDs: string[]) {
  const ids = [...new Set(knowledgeIDs.filter(Boolean))]
  if (!ids.length) return new Map<string, EvidenceLookupItem>()
  const res = await post<ApiEnvelope<EvidenceLookupItem[]>>('/api/custom/chat/evidence', { knowledge_ids: ids })
  const evidenceByKey = new Map<string, EvidenceLookupItem>()
  for (const item of unwrapData(res) || []) {
    for (const key of [item.knowledge_id, item.evidence_sentence_id]) {
      const normalized = String(key || '').trim()
      if (normalized) evidenceByKey.set(normalized, item)
    }
  }
  return evidenceByKey
}

async function mapMessages(messages: WeKnoraMessage[]) {
  const knowledgeIDs = messages.flatMap(message => messageReferences(message).flatMap(referenceLookupKeys))
  const evidence = await lookupEvidence(knowledgeIDs)
  return messages.filter(message => message.role === 'user' || message.role === 'assistant').map(message => mapMessage(message, evidence))
}

function cloneAgentEventStream(events: Record<string, unknown>[]) {
  return events.map(event => ({ ...event }))
}

function parseStreamPayload(raw: string): WeKnoraStreamPayload | null {
  if (!raw || raw === '[DONE]') return null
  try {
    const parsed = JSON.parse(raw)
    return parsed && typeof parsed === 'object' ? parsed : null
  } catch {
    return null
  }
}

function streamType(payload: WeKnoraStreamPayload) {
  return String(payload.response_type || payload.type || '').trim()
}

function referencesFromPayload(payload: WeKnoraStreamPayload): KnowledgeReference[] {
  const data = payload.data || {}
  const raw = data.references || data.knowledge_references
  if (!Array.isArray(raw)) return []
  return raw.filter(item => item && typeof item === 'object') as KnowledgeReference[]
}

function videoEvidenceFromPayload(value: unknown): VideoEvidence[] {
  if (!Array.isArray(value)) return []
  return value.flatMap(item => {
    if (!item || typeof item !== 'object') return []
    const raw = item as Record<string, unknown>
    const videoId = String(raw.video_id || '').trim()
    const evidenceSentenceId = String(raw.evidence_sentence_id || '').trim()
    const transcriptGeneration = String(raw.transcript_generation || '').trim()
    const startMs = Number(raw.start_ms)
    const endMs = Number(raw.end_ms)
    if (!videoId || !evidenceSentenceId || !transcriptGeneration || !Number.isFinite(startMs) || !Number.isFinite(endMs) || endMs <= startMs) return []
    const startSeconds = Math.max(0, Math.floor(startMs / 1000))
    const endSeconds = Math.max(startSeconds, Math.floor(endMs / 1000))
    return [{
      knowledgeId: String(raw.knowledge_id || '').trim(),
      videoId,
      videoTitle: String(raw.video_title || '').trim(),
      startMs,
      endMs,
      startSeconds,
      endSeconds,
      startTimestamp: formatTime(startSeconds),
      endTimestamp: formatTime(endSeconds),
      evidenceSentenceId,
      transcriptGeneration,
      sourceType: 'transcript',
      linkable: raw.linkable === true,
    }]
  })
}

function appendNativeAgentEvent(
  events: Record<string, unknown>[],
  eventMap: Map<string, Record<string, unknown>>,
  pendingToolCalls: Map<string, Record<string, unknown>>,
  payload: WeKnoraStreamPayload,
  answer: string,
) {
  const type = streamType(payload)
  const data = payload.data || {}
  if (type === 'thinking' || type === 'reflection') {
    const eventID = String(data.event_id || payload.id || 'thinking')
    let event = eventMap.get(eventID)
    if (!event) {
      event = { type: 'thinking', event_id: eventID, content: '', done: false, startTime: Date.now(), thinking: true }
      events.push(event)
      eventMap.set(eventID, event)
    }
    if (payload.content && !payload.done) event.content = String(event.content || '') + payload.content
    if (payload.done) {
      event.done = true
      event.thinking = false
      event.duration_ms = data.duration_ms || Date.now() - Number(event.startTime || Date.now())
      event.completed_at = data.completed_at || Date.now()
    }
    return
  }

  if (type === 'tool_call') {
    const toolName = String(data.tool_name || data.name || '')
    if (toolName === 'final_answer') return
    const toolCallID = String(data.tool_call_id || (toolName ? `${toolName}-${events.length}` : payload.id || `tool-${events.length}`))
    let event = pendingToolCalls.get(toolCallID) || events.find(item => item.type === 'tool_call' && item.tool_call_id === toolCallID)
    if (!event) {
      event = { type: 'tool_call', tool_call_id: toolCallID, timestamp: Date.now(), pending: true }
      events.push(event)
    }
    event.tool_name = toolName || event.tool_name
    event.arguments = data.arguments || event.arguments
    event.pending = true
    event.tool_data = data
    pendingToolCalls.set(toolCallID, event)
    return
  }

  if (type === 'tool_result' || (type === 'error' && (data.tool_call_id || data.tool_name))) {
    const toolCallID = String(data.tool_call_id || '')
    const toolName = String(data.tool_name || data.name || '')
    let event = toolCallID ? pendingToolCalls.get(toolCallID) : undefined
    if (!event && toolName) {
      event = [...pendingToolCalls.values()].find(item => item.tool_name === toolName)
    }
    if (!event) {
      event = { type: 'tool_call', tool_call_id: toolCallID || `${toolName || 'tool'}-${events.length}`, tool_name: toolName, timestamp: Date.now() }
      events.push(event)
    }
    event.pending = false
    event.success = type !== 'error' && data.success !== false
    event.output = event.success ? data.output || payload.content : undefined
    event.error = event.success ? undefined : data.error || payload.content
    event.duration = data.duration_ms || data.duration
    event.duration_ms = data.duration_ms || data.duration
    event.display_type = data.display_type
    event.tool_data = data
    if (toolCallID) pendingToolCalls.delete(toolCallID)
    return
  }

  if (type === 'answer') {
    const eventID = String(data.event_id || '')
    const eventKey = eventID || 'answer'
    let event = eventMap.get(eventKey)
    if (!event) {
      event = { type: 'answer', event_id: eventID || undefined, content: '', done: false }
      events.push(event)
      eventMap.set(eventKey, event)
    }
    event.content = cleanAnswer(answer)
    if (payload.done) event.done = true
    if (data.is_fallback) event.is_fallback = true
    return
  }

  if (type === 'complete' || (!type && payload.done)) {
    let answerEvent = eventMap.get('answer')
    const cleanedAnswer = cleanAnswer(answer)
    if (!answerEvent && cleanedAnswer) {
      answerEvent = { type: 'answer', content: cleanedAnswer, done: false }
      events.push(answerEvent)
      eventMap.set('answer', answerEvent)
    }
    if (answerEvent) {
      answerEvent.content = cleanedAnswer || answerEvent.content || ''
      answerEvent.done = true
    }
    if (!events.some(event => event.type === 'agent_complete')) {
      events.push({
        type: 'agent_complete',
        total_duration_ms: data.total_duration_ms || 0,
        total_steps: data.total_steps || events.filter(event => event.type !== 'answer').length,
      })
    }
  }
}

function finalizeNativeAgentStream(
  events: Record<string, unknown>[],
  eventMap: Map<string, Record<string, unknown>>,
  answer: string,
) {
  const cleanedAnswer = cleanAnswer(answer)
  if (cleanedAnswer) {
    let answerEvent = eventMap.get('answer')
    if (!answerEvent) {
      answerEvent = { type: 'answer', content: cleanedAnswer, done: false }
      events.push(answerEvent)
      eventMap.set('answer', answerEvent)
    }
    answerEvent.content = cleanedAnswer
    answerEvent.done = true
  }
  if (!events.some(event => event.type === 'agent_complete')) {
    events.push({
      type: 'agent_complete',
      total_duration_ms: 0,
      total_steps: events.filter(event => event.type !== 'answer').length,
    })
  }
  const deduped = dedupeNativeAnswerEvents(events)
  if (deduped !== events) {
    events.splice(0, events.length, ...deduped)
  }
}

function findLastAssistantAnswerIndex(messages: ChatMessage[]) {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index]
    if (message.sender === 'assistant' && message.text.trim()) return index
  }
  return -1
}

async function streamAnswer(sessionID: string, question: string, scope: ScopeResponse, options: StreamAnswerOptions = {}): Promise<StreamingAnswerResult> {
  const token = localStorage.getItem('weknora_token')
  if (!token) throw new Error('登录已失效，请重新登录后再提问')
  const apiBase = getApiBaseUrl()
  const request = buildChatRequest(
    scope,
    questionForScope(question, scope),
    token,
    localStorage.getItem('weknora_selected_tenant_id'),
  )
  let answer = ''
  let thinkingText = ''
  let activityText = ''
  let completed = false
  let knowledgeReferences: KnowledgeReference[] = []
  let routeMode: 'quick' | 'reasoning' | undefined
  let coverage: 'complete' | 'partial' | 'none' | undefined
  let failureReason: StreamingChatMessage['failure_reason']
  let videoEvidence: VideoEvidence[] = []
  const agentEventStream: Record<string, unknown>[] = []
  const eventMap = new Map<string, Record<string, unknown>>()
  const pendingToolCalls = new Map<string, Record<string, unknown>>()
  const streamController = new AbortController()
  const emitChunk = () => options.onChunk?.({
    id: `stream-${sessionID}`,
    sender: 'assistant',
    text: cleanAnswer(answer),
    role: 'assistant',
    content: cleanAnswer(answer),
    isAgentMode: Boolean(scope.agent_id && scope.agent_id !== 'builtin-quick-answer') || agentEventStream.length > 0,
    is_completed: completed,
    request_id: sessionID,
    agentEventStream: cloneAgentEventStream(completed ? dedupeNativeAnswerEvents(agentEventStream) : agentEventStream),
    knowledge_references: knowledgeReferences,
    route_mode: routeMode,
    coverage,
    failure_reason: failureReason,
    video_evidence: videoEvidence,
    thinkingText: thinkingText.trim(),
    activityText,
    timestamp: nowLabel(),
  })
  const endpoint = scope.auto_route || scope.agent_id ? 'agent-chat' : 'knowledge-chat'
  await fetchEventSource(`${apiBase}/api/v1/${endpoint}/${sessionID}`, {
    method: 'POST',
    signal: streamController.signal,
    headers: request.headers,
    body: JSON.stringify(request.body),
    openWhenHidden: true,
    onopen: async response => {
      if (!response.ok) {
        let detail = ''
        try {
          const payload = await response.clone().json() as {
            error?: string | { message?: string }
            message?: string
          }
          detail = typeof payload.error === 'string'
            ? payload.error
            : payload.error?.message || payload.message || ''
        } catch {
          // Keep the stable HTTP error when the response is not JSON.
        }
        const suffix = detail.trim() ? `：${detail.trim()}` : ''
        const error = new Error(`问答请求失败：HTTP ${response.status}${suffix}`) as Error & { status?: number }
        error.status = response.status
        throw error
      }
    },
    onmessage: event => {
      const payload = parseStreamPayload(event.data)
      const chunk = parseWeKnoraStreamChunk(event.data)
      if (chunk.kind === 'answer') {
        answer += chunk.content || ''
        activityText = ''
      }
      if (chunk.kind === 'thinking') {
        thinkingText += chunk.content || ''
        activityText = '正在思考'
      }
      if (chunk.kind === 'activity') {
        activityText = chunk.content || ''
      }
      if (chunk.kind === 'complete' && !answer && chunk.content) {
        answer = chunk.content
        activityText = ''
      }
      if (payload) {
        const route = String(payload.data?.route_mode || '').trim()
        if (route === 'quick' || route === 'reasoning') routeMode = route
        const reportedCoverage = String(payload.data?.coverage || '').trim()
        if (reportedCoverage === 'complete' || reportedCoverage === 'partial' || reportedCoverage === 'none') coverage = reportedCoverage
        const reportedFailure = String(payload.data?.failure_reason || '').trim()
        if (isChatFailureReason(reportedFailure)) {
          failureReason = reportedFailure
        }
        if (Array.isArray(payload.data?.video_evidence)) videoEvidence = videoEvidenceFromPayload(payload.data.video_evidence)
        const references = referencesFromPayload(payload)
        if (references.length) {
          knowledgeReferences = mergeKnowledgeReferences(knowledgeReferences, references)
        }
        appendNativeAgentEvent(agentEventStream, eventMap, pendingToolCalls, payload, answer)
        if (streamType(payload) === 'complete' || (!streamType(payload) && payload.done)) completed = true
      }
      if (chunk.kind === 'error') {
        throw new Error(chunk.content || '问答生成失败')
      }
      if (chunk.kind !== 'ignore' || payload) emitChunk()
      if (shouldAbortStream(event.data)) streamController.abort()
    },
    onerror: error => {
      throw error
    },
  })
  completed = true
  knowledgeReferences = mergeKnowledgeReferences(knowledgeReferences, referencesFromCitationTags(answer))
  knowledgeReferences = hydrateKnowledgeReferences(
    knowledgeReferences,
    await lookupEvidence(knowledgeReferences.flatMap(referenceLookupKeys)),
  )
  if (!cleanAnswer(answer)) {
    answer = failureMessageForReason(failureReason) || fallbackAnswerFromVideoEvidence(knowledgeReferences)
  }
  finalizeNativeAgentStream(agentEventStream, eventMap, answer)
  emitChunk()
  const streamMessage: StreamingChatMessage = {
    id: `stream-${sessionID}`,
    sender: 'assistant',
    text: cleanAnswer(answer),
    role: 'assistant',
    content: cleanAnswer(answer),
    isAgentMode: Boolean(scope.agent_id && scope.agent_id !== 'builtin-quick-answer') || agentEventStream.length > 0,
    is_completed: true,
    request_id: sessionID,
    agentEventStream: cloneAgentEventStream(agentEventStream),
    knowledge_references: knowledgeReferences,
    route_mode: routeMode,
    coverage,
    failure_reason: failureReason,
    video_evidence: videoEvidence,
    thinkingText: thinkingText.trim(),
    activityText: '',
    timestamp: nowLabel(),
  }
  return { answer: cleanAnswer(answer), streamMessage }
}

function cleanAnswer(text: string) {
  return text
    .replace(/<think\b[^>]*>[\s\S]*?<\/think>/gi, '')
    .replace(/<think\b[^>]*>[\s\S]*$/gi, '')
    .replace(/\n{3,}/g, '\n\n')
    .trim()
}

async function hydrateLastAssistant(
  session: WeKnoraSession,
  fallbackText: string,
  tenantID?: string | number | null,
): Promise<ChatMessage> {
  const messages = await loadSessionMessages(session.id, tenantID)
  const mapped = await mapMessages(messages)
  const assistant = [...mapped].reverse().find(message => message.sender === 'assistant' && message.text.trim())
  return assistant || { id: messageId(), sender: 'assistant', text: fallbackText, timestamp: nowLabel() }
}

function sessionFromScope(session: WeKnoraSession, scope: ScopeResponse, messages: ChatMessage[], fallbackTitle: string): ChatSession {
  return {
    id: session.id,
    title: session.title || fallbackTitle.slice(0, 24),
    type: scope.scope === 'video' ? 'video' : 'chat',
    time: formatRelativeTime(session.updated_at || session.created_at) || '刚刚',
    messages,
    scope: scope.scope,
    tenantId: normalizeTenantId(scope.tenant_id) || undefined,
    videoId: scope.video_id,
    videoTitle: scope.video_title,
    videoCoverUrl: scope.video_cover_url,
  }
}

export async function fetchSessions(): Promise<ChatSession[]> {
  try {
    const scope = await getScope({ globalMode: true })
    const res = await get<ApiEnvelope<WeKnoraSession[]>>(
      '/api/v1/sessions?page=1&page_size=20',
      tenantRequestConfig(scope.tenant_id),
    )
    const resourceTenantId = normalizeTenantId(scope.tenant_id) || undefined
    return (unwrapData(res) || []).filter(isVideohubSession).map(session => {
      const meta = parseSessionMeta(session.description)
      const sessionTenantId = normalizeTenantId(meta.tenant_id) || resourceTenantId
      const scopeType = meta.scope === 'video' ? 'video' : 'global'
      return {
        id: session.id,
        title: session.title || '未命名会话',
        type: scopeType === 'video' ? 'video' : 'chat',
        time: formatRelativeTime(session.updated_at || session.created_at),
        messages: [],
        scope: scopeType,
        tenantId: sessionTenantId,
        videoId: meta.video_id,
        videoTitle: meta.video_title,
        videoCoverUrl: meta.video_cover_url,
      }
    })
  } catch (error) {
    throw normalizeChatError(error)
  }
}

export async function loadChatSession(session: ChatSession): Promise<ChatSession> {
  try {
    const tenantID = normalizeTenantId(session.tenantId)
      || normalizeTenantId((await getScope({ globalMode: true })).tenant_id)
    const messages = await mapMessages(await loadSessionMessages(session.id, tenantID))
    return { ...session, tenantId: tenantID || undefined, messages }
  } catch (error) {
    throw normalizeChatError(error)
  }
}

export async function sendChatMessage(question: string, options: SendOptions = {}): Promise<ChatMessage> {
  try {
    const scope = {
      ...resolveChatScopeAgent(await getScope(options), options),
      ...(options.autoRoute !== undefined ? { auto_route: options.autoRoute } : {}),
      ...(options.agentId ? { agent_id: options.agentId } : {}),
      ...(options.agentEnabled !== undefined ? { agent_enabled: options.agentEnabled } : {}),
    }
    const session = await createSession(question, scope)
    const eventID = recordQuestionBestEffort(question, scope, session.id, options.currentVideo, options.currentTime)
    const { answer, streamMessage } = await streamAnswer(session.id, question, scope, { onChunk: options.onStreamMessage })
    const storedMessages = await loadSessionMessages(session.id, scope.tenant_id)
    const mappedMessages = await mapMessages(storedMessages)
    const assistant = [...mappedMessages].reverse().find(message => message.sender === 'assistant' && message.text.trim())
      || { id: messageId(), sender: 'assistant' as const, text: answer, timestamp: nowLabel() }
    void recordSourceAuditBestEffort(eventID, session.id, scope, storedMessages, streamMessage).catch(error => {
      console.warn('record chat source audit failed', error)
    })
    options.onMessage?.(assistant)
    return assistant
  } catch (error) {
    throw normalizeChatError(error)
  }
}

export async function createChatTurn(question: string, options: TurnOptions = {}): Promise<ChatSession> {
  try {
    const scope = resolveChatScopeAgent(await getScope(options), options)
    const tenantID = normalizeTenantId(options.session?.tenantId) || normalizeTenantId(scope.tenant_id)
    const sourceTenantID = normalizeTenantId(options.agentSourceTenantId)
    const currentTenantID = normalizeTenantId(scope.tenant_id) || tenantID
    const effectiveScope = {
      ...(tenantID && !scope.tenant_id ? { ...scope, tenant_id: tenantID } : scope),
      ...(options.agentId ? { agent_id: options.agentId } : {}),
      ...(options.agentEnabled !== undefined ? { agent_enabled: options.agentEnabled } : {}),
      ...(options.autoRoute !== undefined ? { auto_route: options.autoRoute } : {}),
      ...(sourceTenantID && sourceTenantID !== currentTenantID
        ? { agent_source_tenant_id: sourceTenantID }
        : {}),
    }
    const session = options.session?.id && !options.session.id.startsWith('pending-')
      ? {
        id: options.session.id,
        title: options.session.title,
        created_at: undefined,
        updated_at: undefined,
      }
      : await createSession(question, effectiveScope)
    if (!options.session?.id || options.session.id.startsWith('pending-')) {
      options.onSessionCreated?.(sessionFromScope(session, effectiveScope, [], question))
    }
    const eventID = recordQuestionBestEffort(question, effectiveScope, session.id, options.currentVideo, options.currentTime)
    const userMessage: ChatMessage = { id: messageId(), sender: 'user', text: question, timestamp: nowLabel() }
    options.onMessage?.(userMessage)
    const { answer, streamMessage } = await streamAnswer(session.id, question, effectiveScope, { onChunk: options.onStreamMessage })
    const storedMessages = await loadSessionMessages(session.id, tenantID)
    const messages = await mapMessages(storedMessages)
    void recordSourceAuditBestEffort(eventID, session.id, effectiveScope, storedMessages, streamMessage).catch(error => {
      console.warn('record chat source audit failed', error)
    })
    const finalMessages = mergeLocalTurnWithStoredMessages(messages, userMessage, { ...streamMessage, id: messageId(), text: answer, timestamp: nowLabel() })
    return sessionFromScope(session, effectiveScope, finalMessages, question)
  } catch (error) {
    throw normalizeChatError(error)
  }
}

export async function sendAssistantQuery(question: string, currentVideo: VideoData, currentTime: number, globalMode = false): Promise<ChatMessage> {
  return sendChatMessage(question, { currentVideo, currentTime, globalMode })
}
