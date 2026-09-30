import {
  sanitizeStreamRequestBody,
  type ChatRequestDebugInfo,
} from '../../utils/chatRequestDebug'

export interface StoredChatMessage {
  id: string
  request_id?: string
  role: 'user' | 'assistant' | 'system'
  content: string
  created_at?: string
  updated_at?: string
  agent_id?: string
  channel?: string
  request_context?: Record<string, unknown>
}

const requestContextKeys = [
  'execution_scope',
  'knowledge_base_ids',
  'knowledge_ids',
  'agent_enabled',
  'auto_route',
  'agent_id',
  'agent_source_tenant_id',
  'web_search_enabled',
  'summary_model_id',
  'mcp_service_ids',
  'skill_names',
  'tag_ids',
  'disable_title',
] as const

function persistedRequestBody(
  assistant: StoredChatMessage,
  user?: StoredChatMessage,
): Record<string, unknown> {
  const context = assistant.request_context || {}
  const body: Record<string, unknown> = { query: user?.content || '' }
  for (const key of requestContextKeys) {
    if (context[key] !== undefined) body[key] = context[key]
  }
  body.channel = assistant.channel || 'web'
  return sanitizeStreamRequestBody(body)
}

export function buildPersistedChatRequestDebug(input: {
  sessionId: string
  assistant: StoredChatMessage
  user?: StoredChatMessage
}): ChatRequestDebugInfo {
  const context = input.assistant.request_context || {}
  const usesAgentEndpoint = context.auto_route === true
    || Boolean(context.agent_id)
    || Boolean(input.assistant.agent_id)
  return {
    requestId: input.assistant.request_id,
    messageId: input.assistant.id,
    sessionId: input.sessionId,
    url: `/api/v1/${usesAgentEndpoint ? 'agent-chat' : 'knowledge-chat'}/${input.sessionId}`,
    method: 'POST',
    body: persistedRequestBody(input.assistant, input.user),
    sentAt: Date.parse(input.user?.created_at || input.assistant.created_at || input.assistant.updated_at || '') || undefined,
  }
}

export function buildStreamingChatRequestDebug(input: {
  sessionId: string
  requestId: string
  endpoint: string
  body: Record<string, unknown>
  sentAt: number
}): ChatRequestDebugInfo {
  return {
    requestId: input.requestId,
    sessionId: input.sessionId,
    url: `/api/v1/${input.endpoint}/${input.sessionId}`,
    method: 'POST',
    body: sanitizeStreamRequestBody(input.body),
    sentAt: input.sentAt,
  }
}
