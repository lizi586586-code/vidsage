import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { runInNewContext } from 'node:vm'
import { setImmediate } from 'node:timers/promises'
import test from 'node:test'
import ts from 'typescript'

const cases = [
  { file: 'AiAssistant.vue', method: 'send', draft: 'input', initial: 'input' },
  { file: '../../views/videohub/ChatView.vue', method: 'startSession', draft: 'followUp', initial: 'question' },
  { file: '../../views/videohub/ChatView.vue', method: 'continueSession', draft: 'followUp', initial: 'followUp' },
]

for (const scenario of cases) {
  for (const outcome of ['success', 'failure']) {
    test(`${scenario.method} clears the sent instruction and preserves a new draft after ${outcome}`, async () => {
      const source = readFileSync(resolve(import.meta.dirname, scenario.file), 'utf8')
      const method = source.match(new RegExp(`async function ${scenario.method}\\([^)]*\\) \\{[\\s\\S]*?\\n}`))?.[0]
      assert.ok(method)
      let complete: (value: unknown) => void = () => {}
      let fail: (error: Error) => void = () => {}
      const pendingTurn = new Promise((res, rej) => { complete = res; fail = rej })
      let submitted = ''
      let submissions = 0
      const original = '  请总结视频\n并列出时间点  '
      const session = { id: 'existing', scope: 'global', messages: [] }
      const context = {
        input: { value: '' }, question: { value: '' }, followUp: { value: '' },
        expanded: { value: false }, isGenerating: { value: false },
        preparingClassroom: { value: false }, isReviewIntent: () => false,
        reviewRequestId: () => '', visibleReviewQuestion: (value: string) => value,
        turnRevision: 0, scopeRevision: 0, sessionCacheKey: { value: 'test-scope' },
        lastUserQuery: { value: '' }, streamingAssistantId: { value: '' }, stopCurrentTurn: { value: null },
        activeSession: { value: session }, sessions: { value: [session] }, messages: { value: [] },
        props: { globalMode: true, currentVideo: { id: 'video' } },
        settingsStore: { settings: {} }, recoveryStorage: null, CHAT_PAGE_RECOVERY_KEY: 'test',
        createChatTurn: (value: string) => { submitted = value; submissions++; return pendingTurn },
        scrollBottom: async () => {}, pushStreamingPlaceholder: () => 'assistant',
        persistPendingQuestion: () => {}, writeChatRecovery: () => {}, clearChatRecovery: () => {},
        assistantRecoveryKey: () => 'test', shouldAutoRoute: () => true,
        materializeActiveSession: () => {}, materializePendingSession: () => {},
        updateStreamingMessage: () => {}, cacheSession: () => {}, welcome: () => ({}),
        route: { name: 'aiChat' },
        router: { replace: async () => {} }, window: { dispatchEvent: () => {} },
        MessagePlugin: { error: () => {} },
        CustomEvent: class {},
      }
      context[scenario.initial as 'input' | 'question' | 'followUp'].value = original
      const compiled = ts.transpile(method, { target: ts.ScriptTarget.ES2022 })
      const run = runInNewContext(`${compiled}\n${scenario.method}`, context) as (value?: string) => Promise<void>
      const turn = run(original)
      await setImmediate()
      assert.equal(context[scenario.draft as 'input' | 'followUp'].value, '')
      assert.equal(submitted, original.trim())
      assert.equal(context.isGenerating.value, true)
      const edited = '重新编辑的下一条问题'
      context[scenario.draft as 'input' | 'followUp'].value = edited
      await run(edited)
      assert.equal(submissions, 1)
      if (outcome === 'success') complete(session)
      else fail(new Error('generation failed'))
      await turn
      assert.equal(context[scenario.draft as 'input' | 'followUp'].value, edited)
      assert.equal(context.isGenerating.value, false)
    })
  }
}

test('reviewing a different course bypasses the old video scope and preserves the conversation scope for later questions', async () => {
  const source = readFileSync(resolve(import.meta.dirname, '../../views/videohub/ChatView.vue'), 'utf8')
  const method = source.match(/async function continueSession\(\) \{[\s\S]*?\n}/)?.[0]
  assert.ok(method)
  const original = {
    id: 'old-video-session', type: 'video', scope: 'video', videoId: 'deleted-video',
    videoTitle: '旧课程', videoCoverUrl: '/old-cover', tenantId: 'tenant', messages: [],
  }
  let submittedOptions: Record<string, unknown> = {}
  const context = {
    followUp: { value: 'confirmed-new-course-request' }, isGenerating: { value: false },
    preparingClassroom: { value: false }, isReviewIntent: () => false,
    reviewRequestId: () => 'new-course-request-id', turnRevision: 0,
    activeSession: { value: original }, sessions: { value: [original] },
    lastUserQuery: { value: '' }, streamingAssistantId: { value: '' }, stopCurrentTurn: { value: null },
    settingsStore: { settings: {} }, recoveryStorage: null, CHAT_PAGE_RECOVERY_KEY: 'test',
    createChatTurn: async (_: string, options: Record<string, unknown>) => {
      submittedOptions = options
      return { id: original.id, type: 'chat', scope: 'global', tenantId: 'tenant', messages: [{ sender: 'assistant', text: 'done' }] }
    },
    persistPendingQuestion: () => {}, clearChatRecovery: () => {}, shouldAutoRoute: () => false,
    scrollBottom: async () => {}, pushStreamingPlaceholder: () => 'assistant', updateStreamingMessage: () => {},
  }
  const run = runInNewContext(`${ts.transpile(method, { target: ts.ScriptTarget.ES2022 })}\ncontinueSession`, context)
  await run()
  assert.equal(submittedOptions.globalMode, true)
  assert.equal(submittedOptions.currentVideo, undefined)
  assert.equal(submittedOptions.agentEnabled, true)
  assert.deepEqual(Array.from(submittedOptions.skillNames as string[]), ['openmaic-classroom'])
  assert.equal(context.activeSession.value.scope, 'video')
  assert.equal(context.activeSession.value.type, 'video')
  assert.equal(context.activeSession.value.videoId, 'deleted-video')
  assert.equal(context.activeSession.value.videoTitle, '旧课程')
  assert.equal(context.activeSession.value.videoCoverUrl, '/old-cover')
  assert.equal(context.activeSession.value.tenantId, 'tenant')
  assert.equal(context.sessions.value.length, 1)
  assert.equal(context.isGenerating.value, false)
})
