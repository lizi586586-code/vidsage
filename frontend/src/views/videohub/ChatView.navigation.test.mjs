import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as chatMessageTime from '../../utils/chatMessageTime.ts'

const require = createRequire(import.meta.url)
const source = readFileSync(new URL('./ChatView.vue', import.meta.url), 'utf8')
const platformSource = readFileSync(new URL('../platform/index.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const script = compileScript(descriptor, { id: 'chat-navigation-test' })
const compiled = ts.transpileModule(script.content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

function node(type = 'root') { return { type, children: [], parent: null } }
const renderer = vue.createRenderer({
  createElement: node,
  createText: () => node('text'),
  createComment: () => node('comment'),
  setText() {}, setElementText() {}, patchProp() {},
  parentNode: target => target.parent,
  nextSibling: target => target.parent?.children[target.parent.children.indexOf(target) + 1] || null,
  insert(target, parent, anchor) {
    if (target.parent) target.parent.children.splice(target.parent.children.indexOf(target), 1)
    target.parent = parent
    const index = anchor ? parent.children.indexOf(anchor) : -1
    parent.children.splice(index < 0 ? parent.children.length : index, 0, target)
  },
  remove(target) {
    if (target.parent) target.parent.children.splice(target.parent.children.indexOf(target), 1)
    target.parent = null
  },
})

async function flush() { for (let i = 0; i < 8; i += 1) await vue.nextTick() }

async function harness() {
  const route = vue.reactive({ path: '/platform/ai-chat', name: 'aiChat', query: {} })
  const replacements = []
  const router = {
    async replace(location) {
      replacements.push(location)
      route.path = typeof location === 'string' ? location : location.path
      route.name = route.path === '/platform/ai-chat' ? 'aiChat' : 'other'
      route.query = typeof location === 'string' ? {} : location.query || {}
    },
    async push(location) { await this.replace(location) },
  }
  let options, finish, stops = 0
  const createChatTurn = async (_question, turnOptions) => {
    options = turnOptions
    turnOptions.onStopReady(async () => { stops += 1 })
    return new Promise(resolve => { finish = resolve })
  }
  const mocks = {
    vue,
    '@/utils/chatMessageTime': chatMessageTime,
    'vue-router': { useRoute: () => route, useRouter: () => router },
    'tdesign-vue-next': { MessagePlugin: { error() {}, info() {} } },
    '@/api/videohub/chat': { createChatTurn, fetchSessions: async () => [], loadChatSession: async session => session },
    '@/stores/settings': { useSettingsStore: () => ({ selectedAgentId: '', isAgentEnabled: true, settings: {} }) },
    '@/stores/auth': { useAuthStore: () => ({ user: { id: 'test-user' } }) },
    '@/composables/useReviewCourse': { useReviewCourse: () => ({
      choosingCourse: vue.ref(false), loadingCourses: vue.ref(false), courseError: vue.ref(''),
      courses: vue.ref([]), selectedCourseId: vue.ref(''), preparingClassroom: vue.ref(false),
      classroomResult: vue.ref(null), classroomVideo: vue.ref(null), recentCourseIds: vue.ref([]),
      askCourse() {}, loadCourses() {}, cancelCourse() {}, confirmCourse() {}, updateStatus() {},
    }) },
    '@/api/videohub/reviewMaterials': {
      isFirstReviewRequest: () => false, isReviewIntent: () => false, needsReviewCourseChoice: () => false,
      reviewRequestId: () => '', visibleReviewQuestion: value => value,
    },
    '@/api/agent': { VIDSAGE_REASONING_AGENT_ID: 'reasoning' },
    '@/api/videohub/chatRequest': { shouldAutoRoute: () => true },
    '@/api/videohub/chatRecovery': {
      CHAT_PAGE_RECOVERY_KEY: 'chat', getChatRecoveryStorage: () => null,
      readChatRecovery: () => null, writeChatRecovery() {}, clearChatRecovery() {},
    },
  }
  const module = { exports: {} }
  runInNewContext(compiled, {
    module, exports: module.exports,
    require: id => mocks[id] || (id.endsWith('.vue') ? { default: {} } : require(id)),
    window: { dispatchEvent() {} }, CustomEvent: class {},
  })
  const Chat = { ...module.exports.default, render: () => null }
  const Other = { render: () => null }
  const cachedName = platformSource.match(/<KeepAlive include="([^"]+)"/)?.[1]
  const instance = vue.ref(null)
  const app = renderer.createApp({
    render() {
      const page = route.path === '/platform/ai-chat' ? vue.h(Chat, { ref: instance }) : vue.h(Other)
      return cachedName ? vue.h(vue.KeepAlive, { include: cachedName }, () => page) : page
    },
  })
  app.mount(node())
  await flush()
  const state = instance.value.$.setupState
  return {
    state, route, router, app, replacements,
    get options() { return options }, get stops() { return stops },
    async start() { state.question = '测试知识问题'; const pending = state.startSession(); await flush(); return { pending } },
    created() { options.onSessionCreated({ id: 'session-1', title: '测试知识问题', messages: [], scope: 'global' }) },
    stream(text) { options.onStreamMessage({ sender: 'assistant', text, timestamp: '刚刚' }) },
    complete() { finish({ id: 'session-1', title: '测试知识问题', scope: 'global', messages: [
      { id: 'user-1', sender: 'user', text: '测试知识问题', timestamp: '刚刚' },
      { id: 'assistant-1', sender: 'assistant', text: '完整回答', timestamp: options.assistantTimestamp },
    ] }) },
    async navigate(path, query = {}) { route.path = path; route.name = path === '/platform/ai-chat' ? 'aiChat' : 'other'; route.query = query; await flush() },
  }
}

test('reply time stays fixed through pending, repeated streaming chunks and completion', async () => {
  const h = await harness()
  try {
    const { pending } = await h.start()
    const timestamp = h.state.activeSession.messages.at(-1).timestamp
    assert.match(timestamp, /^\d{2}月\d{2}日 \d{2}:\d{2}$/u)
    assert.equal(h.options.assistantTimestamp, timestamp)
    h.created(); await flush()
    h.stream('第一段')
    assert.equal(h.state.activeSession.messages.at(-1).timestamp, timestamp)
    h.stream('第二段')
    assert.equal(h.state.activeSession.messages.at(-1).timestamp, timestamp)
    h.complete(); await pending
    assert.equal(h.state.activeSession.messages.at(-1).timestamp, timestamp)
  } finally { h.app.unmount() }
})

test('switching menus preserves a streaming turn and its draft; returning resumes it', async () => {
  const h = await harness()
  try {
    const { pending } = await h.start()
    h.created(); await flush(); h.stream('第一段回答')
    h.state.followUp = '尚未发送的追问'
    await h.navigate('/platform/videos')
    assert.equal(h.stops, 0, 'menu navigation must not call the stop endpoint')
    h.stream('第二段回答')
    await h.navigate('/platform/ai-chat', { session: 'session-1' })
    assert.equal(h.state.isGenerating, true)
    assert.equal(h.state.activeSession.messages.at(-1).text, '第二段回答')
    assert.equal(h.state.followUp, '尚未发送的追问')
    h.complete(); await pending
    assert.equal(h.state.activeSession.messages.at(-1).text, '完整回答')
  } finally { h.app.unmount() }
})

test('session creation and completion while on another menu never navigate back', async () => {
  const h = await harness()
  try {
    const { pending } = await h.start()
    await h.navigate('/platform/agents')
    h.created(); await flush(); h.stream('后台回答')
    h.complete(); await pending; await flush()
    assert.equal(h.route.path, '/platform/agents')
    assert.equal(h.replacements.length, 0)
    assert.equal(h.stops, 0)
    await h.navigate('/platform/ai-chat', { session: 'session-1' })
    assert.equal(h.state.activeSession.messages.at(-1).text, '完整回答')
    assert.equal(h.state.isGenerating, false)
  } finally { h.app.unmount() }
})

test('returning through the top-level chat menu restores the turn, including before session creation', async () => {
  const h = await harness()
  try {
    const { pending } = await h.start()
    await h.navigate('/platform/videos')
    await h.navigate('/platform/ai-chat')
    assert.equal(h.stops, 0)
    assert.equal(h.state.isGenerating, true)
    assert.equal(h.state.activeSession.messages[0].text, '测试知识问题')
    h.created(); await flush()
    await h.navigate('/platform/videos')
    await h.navigate('/platform/ai-chat')
    assert.equal(h.route.query.session, 'session-1')
    assert.equal(h.stops, 0)
    h.complete(); await pending
    await h.navigate('/platform/videos')
    await h.navigate('/platform/ai-chat')
    assert.equal(h.state.activeSession.messages.at(-1).text, '完整回答')
  } finally { h.app.unmount() }
})

test('explicitly choosing a new conversation from the chat page still stops the current turn', async () => {
  const h = await harness()
  try {
    const { pending } = await h.start()
    h.created(); await flush()
    await h.navigate('/platform/ai-chat')
    assert.equal(h.stops, 1)
    assert.equal(h.state.activeSession, null)
    h.complete(); await pending
    assert.equal(h.state.activeSession, null)
  } finally { h.app.unmount() }
})

test('follow-up turns survive menu navigation, while explicit pause and platform exit still stop', async () => {
  const h = await harness()
  try {
    const first = await h.start(); h.created(); h.complete(); await first.pending; await flush()
    h.state.followUp = '追问'
    const pending = h.state.continueSession(); await flush()
    await h.navigate('/platform/graph')
    assert.equal(h.stops, 0)
    await h.navigate('/platform/ai-chat', { session: 'session-1' })
    h.state.pauseGeneration(); await flush()
    assert.equal(h.stops, 1)
    h.complete(); await pending
    h.state.followUp = '下一轮'
    const next = h.state.continueSession(); await flush()
    h.app.unmount(); await flush()
    assert.equal(h.stops, 2)
    h.complete(); await next
  } finally { h.app.unmount() }
})
