import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as chatMessageTime from '../../utils/chatMessageTime.ts'
import * as reviewMaterials from '../../api/videohub/reviewMaterials.ts'

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

async function harness({ delayedStop = false, realReviewCourse = false } = {}) {
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
  const cancellations = []
  const preparedCourses = [], createdReviewSessions = [], submittedQuestions = [], errors = []
  const classroomResult = vue.ref(null)
  const createChatTurn = async (_question, turnOptions) => {
    submittedQuestions.push(_question)
    options = turnOptions
    if (!delayedStop) turnOptions.onStopReady(async () => { stops += 1 })
    if (realReviewCourse) {
      const session = { id: 'session-1', title: '巩固学习', messages: [{ id: 'user-1', sender: 'user', text: _question }], scope: 'global' }
      turnOptions.onSessionCreated(session)
      return session
    }
    return new Promise(resolve => { finish = resolve })
  }
  const mocks = {
    vue,
    '@/utils/chatMessageTime': chatMessageTime,
    'vue-router': { useRoute: () => route, useRouter: () => router },
    'tdesign-vue-next': { MessagePlugin: { error: message => errors.push(message), info() {} } },
    '@/api/videohub/chat': { createChatTurn, fetchSessions: async () => [], loadChatSession: async session => session,
      createReviewChatSession: async () => {
        createdReviewSessions.push('empty-review-session')
        return { id: 'empty-review-session', title: '巩固学习', messages: [], scope: 'global' }
      },
    },
    '@/stores/settings': { useSettingsStore: () => ({ selectedAgentId: '', isAgentEnabled: true, settings: {} }) },
    '@/stores/auth': { useAuthStore: () => ({ user: { id: 'test-user' } }) },
    '@/composables/useReviewCourse': { useReviewCourse: () => ({
      choosingCourse: vue.ref(false), loadingCourses: vue.ref(false), courseError: vue.ref(''),
      courses: vue.ref([]), selectedCourseId: vue.ref(''), preparingClassroom: vue.ref(false),
      classroomResult, classroomVideo: vue.ref(null), recentCourseIds: vue.ref([]),
      cancellingClassroom: vue.ref(false),
      async cancelReview(id) { cancellations.push(id); classroomResult.value = { request_id: id, generation_status: 'cancelled' }; return true },
      askCourse() {}, loadCourses() {}, cancelCourse() {}, confirmCourse() {}, updateStatus() {},
    }) },
    '@/api/videohub/reviewMaterials': {
      isFirstReviewRequest: () => false, isReviewIntent: () => false, needsReviewCourseChoice: () => false,
      reviewRequestId: value => value.startsWith('review:') ? value.slice(7) : '', visibleReviewQuestion: value => value,
    },
    '@/api/agent': { VIDSAGE_REASONING_AGENT_ID: 'reasoning' },
    '@/api/videohub/chatRequest': { shouldAutoRoute: () => true },
    '@/api/videohub/chatRecovery': {
      CHAT_PAGE_RECOVERY_KEY: 'chat', getChatRecoveryStorage: () => null,
      readChatRecovery: () => null, writeChatRecovery() {}, clearChatRecovery() {},
    },
  }
  if (realReviewCourse) {
    const storage = new Map([['openmaic:test-user:chat', 'historical-review']])
    const localStorage = {
      getItem: key => storage.get(key) ?? null,
      setItem: (key, value) => storage.set(key, value),
      removeItem: key => storage.delete(key),
    }
    Object.assign(mocks, {
      '@/api/videohub/reviewMaterials': reviewMaterials,
      '@/api/videohub': {
        fetchVideoList: async () => [{ id: 'course-1', title: '测试课程', status: 'completed', video_url: 'video.mp4', initiallyAvailable: true }],
        isVideoInitiallyAvailable: () => true,
      },
      '@/api/videohub/recentlyPlayedCourses': { readRecentlyPlayedCourseIds: () => [] },
      '@/api/videohub/openmaicClassroom': {
        fetchOpenMAICClassroomStatus: async id => ({ request_id: id, generation_status: id === 'historical-review' ? 'succeeded' : 'queued' }),
        prepareOpenMAICClassroom: async request => {
          preparedCourses.push(request.video_id)
          return { request_id: 'new-review', generation_status: 'queued' }
        },
      },
    })
    const courseSource = readFileSync(new URL('../../composables/useReviewCourse.ts', import.meta.url), 'utf8')
    const courseModule = { exports: {} }
    runInNewContext(ts.transpileModule(courseSource, {
      compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
    }).outputText, {
      module: courseModule, exports: courseModule.exports, localStorage,
      require: id => mocks[id] || require(id),
    })
    mocks['@/composables/useReviewCourse'] = courseModule.exports
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
    state, route, router, app, replacements, cancellations, classroomResult,
    preparedCourses, createdReviewSessions, submittedQuestions, errors,
    stopReady() { options.onStopReady(async () => { stops += 1 }) },
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

test('landing review asks for a course without creating a session or restoring a historical card into a new conversation', async () => {
  const h = await harness({ realReviewCourse: true })
  try {
    assert.equal(h.state.classroomResult.generation_status, 'succeeded', 'reproduce an existing completed review')
    h.state.question = '尚未发送的问题'
    await h.state.openReviewCourse(); await flush()
    assert.equal(h.state.activeSession, null)
    assert.equal(h.state.choosingCourse, true)
    assert.equal(h.state.selectedCourseId, '')
    assert.equal(h.state.courses.length, 1)
    assert.equal(h.replacements.length, 0)
    assert.equal(h.createdReviewSessions.length, 0)
    await h.state.confirmCourse()
    assert.equal(h.preparedCourses.length, 0, 'confirmation requires an explicit course')
    h.state.cancelCourse()
    assert.equal(h.state.choosingCourse, false)
    assert.equal(h.state.question, '尚未发送的问题')
    assert.equal(h.submittedQuestions.length, 0)
    assert.deepEqual(h.errors, [])
  } finally { h.app.unmount() }
})

test('typed generic review also asks first; only confirming a selected course prepares and opens the new review', async () => {
  const h = await harness({ realReviewCourse: true })
  try {
    h.state.question = '巩固学习'
    await h.state.startSession(); await flush()
    assert.equal(h.state.activeSession, null)
    assert.equal(h.state.choosingCourse, true)
    assert.equal(h.preparedCourses.length, 0)
    assert.equal(h.submittedQuestions.length, 0)
    h.state.selectedCourseId = 'course-1'
    await h.state.confirmCourse(); await flush()
    assert.deepEqual(h.preparedCourses, ['course-1'])
    assert.equal(h.submittedQuestions.length, 1)
    assert.equal(reviewMaterials.reviewRequestId(h.submittedQuestions[0]), 'new-review')
    assert.equal(h.state.activeSession.id, 'session-1')
    assert.equal(h.route.query.session, 'session-1')
    assert.equal(h.state.classroomResult.request_id, 'new-review')
    assert.deepEqual(h.errors, [])
  } finally { h.app.unmount() }
})

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

test('pause targets current review command, including before stop callback is ready', async () => {
  const h = await harness({ delayedStop: true })
  try {
    h.state.question = 'review:current-review'
    h.classroomResult.value = { request_id: 'old-review', generation_status: 'running' }
    const pending = h.state.startSession(); await flush()
    await h.state.pauseGeneration()
    assert.deepEqual(h.cancellations, ['current-review'])
    h.stopReady(); await flush()
    assert.equal(h.stops, 1)
    h.complete(); await pending
    h.state.followUp = '普通追问'
    const next = h.state.continueSession(); await flush()
    h.stopReady(); await h.state.pauseGeneration()
    assert.deepEqual(h.cancellations, ['current-review'], 'normal follow-up must not cancel historical review')
    h.complete(); await next
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
