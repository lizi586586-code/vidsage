import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import test from 'node:test'
import ts from 'typescript'
import * as time from '../../utils/chatMessageTime.ts'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./chat.ts', import.meta.url), 'utf8')
const compiled = ts.transpileModule(`${source}\nexport { streamAnswer, mapMessage }`, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

function harness() {
  let clock = new Date(2026, 9, 15, 11, 39).getTime()
  class Clock extends Date {
    constructor(...args) { super(...(args.length ? args : [clock])) }
    static now() { return clock }
  }
  const mocks = {
    '@microsoft/fetch-event-source': { fetchEventSource: async (_url, options) => {
      for (const chunk of [
        { response_type: 'thinking', content: '读取内容' },
        { response_type: 'answer', content: '第一段' },
        { response_type: 'answer', content: '第二段' },
        { response_type: 'complete', done: true },
      ]) {
        clock += 61_000
        options.onmessage({ data: JSON.stringify(chunk) })
      }
    } },
    '@/utils/request': { get: async () => ({}), post: async () => ({}) },
    '@/utils/api-base': { getApiBaseUrl: () => '' },
    '@/utils/chatMessageTime': { ...time, currentChatMessageTime: () => time.currentChatMessageTime(clock) },
    './dashboard': {},
  }
  const module = { exports: {} }
  runInNewContext(compiled, {
    module, exports: module.exports, Date: Clock, AbortController, console,
    localStorage: { getItem: () => 'test-token' },
    require: id => mocks[id] || require(id),
  })
  return module.exports
}

test('history uses creation time only; missing or invalid creation time stays hidden', () => {
  const { mapMessage } = harness()
  const created = new Date(2026, 0, 2, 0, 5).toISOString()
  const message = { id: 'a1', role: 'assistant', content: '历史回复', updated_at: new Date().toISOString() }
  assert.equal(mapMessage({ ...message, created_at: created }).timestamp, '01月02日 00:05')
  assert.equal(mapMessage(message).timestamp, '')
  assert.equal(mapMessage({ ...message, created_at: 'invalid' }).timestamp, '')
})

test('stream chunks and final reply retain generation start time while the clock advances', async () => {
  for (const provided of [undefined, '10月15日 11:38']) {
    const { streamAnswer } = harness()
    const chunks = []
    const result = await streamAnswer('session-1', '问题', {
      scope: 'global', knowledge_base_ids: [], knowledge_ids: [], session_meta: {},
    }, { assistantTimestamp: provided, onChunk: chunk => chunks.push(chunk) })
    assert.ok(chunks.length >= 4)
    assert.equal(result.answer, '第一段第二段')
    for (const chunk of [...chunks, result.streamMessage]) {
      assert.equal(chunk.timestamp, provided || '10月15日 11:39')
    }
  }
})
