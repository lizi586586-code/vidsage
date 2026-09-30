import assert from 'node:assert/strict'
import test from 'node:test'
import {
  buildPersistedChatRequestDebug,
  buildStreamingChatRequestDebug,
} from './chatRequestDebug'

test('restores a non-null request body and real request id after a page reload', () => {
  const debug = buildPersistedChatRequestDebug({
    sessionId: 'session-1',
    assistant: {
      id: 'assistant-1',
      request_id: 'request-1',
      role: 'assistant',
      content: 'answer',
      created_at: '2026-09-28T13:32:09+08:00',
      agent_id: 'builtin-quick-answer',
      request_context: {
        knowledge_base_ids: ['video-kb'],
        knowledge_ids: [],
        agent_enabled: false,
        auto_route: true,
        execution_scope: 'global_videos',
        disable_title: true,
        channel: 'web',
      },
    },
    user: {
      id: 'user-1',
      request_id: 'request-1',
      role: 'user',
      content: '在哪里提到 AI 提示词公式',
    },
  })

  assert.equal(debug.requestId, 'request-1')
  assert.equal(debug.messageId, 'assistant-1')
  assert.equal(debug.sessionId, 'session-1')
  assert.equal(debug.method, 'POST')
  assert.equal(debug.url, '/api/v1/agent-chat/session-1')
  assert.deepEqual(debug.body, {
    query: '在哪里提到 AI 提示词公式',
    knowledge_base_ids: ['video-kb'],
    knowledge_ids: [],
    agent_enabled: false,
    auto_route: true,
    execution_scope: 'global_videos',
    disable_title: true,
    channel: 'web',
  })
})

test('keeps the exact request metadata on the live streaming message', () => {
  const debug = buildStreamingChatRequestDebug({
    sessionId: 'session-1',
    requestId: 'request-1',
    endpoint: 'agent-chat',
    body: {
      query: '在哪里提到 AI 提示词公式',
      knowledge_base_ids: ['video-kb'],
      images: [{ data: 'base64-payload' }],
    },
    sentAt: 123,
  })

  assert.equal(debug.requestId, 'request-1')
  assert.equal(debug.body?.query, '在哪里提到 AI 提示词公式')
  assert.deepEqual(debug.body?.images, [{ _placeholder: 'image[0]', bytes: 14 }])
})
