import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { runInNewContext } from 'node:vm'
import { compileScript, parse } from '@vue/compiler-sfc'
import ts from 'typescript'
import * as vue from 'vue'
import * as materials from '../api/videohub/reviewMaterials.ts'

const require = createRequire(import.meta.url)
const renderer = vue.createRenderer({
  createElement: () => ({}), createText: () => ({}), createComment: () => ({}),
  setText() {}, setElementText() {}, patchProp() {}, insert() {}, remove() {},
  parentNode: () => null, nextSibling: () => null,
})
const flush = async () => { for (let i = 0; i < 8; i++) await vue.nextTick() }
const deferred = () => { let resolve; const promise = new Promise(r => { resolve = r }); return { promise, resolve } }
const task = (status = 'running', id = 'request-1') => ({ request_id: id, generation_status: status, classroom_id: '', classroom_url: '', status: 'unknown' })

function load(path, mocks, sfc = false, globals = {}) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8')
  const content = sfc ? compileScript(parse(source).descriptor, { id: 'review-pause-test' }).content : source
  const compiled = ts.transpileModule(content, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  const module = { exports: {} }
  runInNewContext(compiled, { module, exports: module.exports, require: id => mocks[id] || require(id), ...globals })
  return module.exports
}

function courseHarness(api = {}, persistedId = null) {
  const errors = []
  const contextKey = vue.ref('conversation-1')
  const storageKey = vue.ref('review:user')
  let state
  const { useReviewCourse } = load('./useReviewCourse.ts', {
    vue, 'tdesign-vue-next': { MessagePlugin: { error: message => errors.push(message) } },
    '@/api/videohub': {}, '@/api/videohub/reviewMaterials': materials,
    '@/api/videohub/recentlyPlayedCourses': {},
    '@/api/videohub/openmaicClassroom': api,
  }, false, { localStorage: { getItem: () => persistedId } })
  const app = renderer.createApp({ setup() {
    state = useReviewCourse({ userId: vue.ref('user'), storageKey, contextKey, busy: vue.ref(false), onStart: async () => {} })
    return () => null
  } })
  app.mount({})
  state.classroomResult.value = task()
  return { state, app, errors, contextKey }
}

test('transport failures retry twice; success is terminal against stale status', async () => {
  let calls = 0
  const h = courseHarness({ cancelOpenMAICClassroom: async () => {
    calls++; if (calls < 3) throw { message: 'offline' }; return task('cancelled')
  } })
  try {
    assert.equal(await h.state.cancelReview('request-1'), true)
    assert.equal(calls, 3)
    h.state.updateStatus(task('succeeded'))
    assert.equal(h.state.classroomResult.value.generation_status, 'cancelled')
    assert.equal(h.state.classroomGenerating.value, false)
    assert.equal(h.errors.length, 0)
  } finally { h.app.unmount() }
})

test('HTTP failures keep true state; transport exhaustion leaves retry possible', async () => {
  for (const failure of [{ status: 500 }, { message: 'offline' }]) {
    let calls = 0
    const h = courseHarness({ cancelOpenMAICClassroom: async () => { calls++; throw failure } })
    try {
      assert.equal(await h.state.cancelReview('request-1'), false)
      assert.equal(calls, failure.status ? 1 : 3)
      assert.equal(h.state.classroomResult.value.generation_status, 'running')
      assert.equal(h.state.cancellingClassroom.value, false)
      assert.deepEqual(h.errors, ['复习材料暂停失败，请重试'])
    } finally { h.app.unmount() }
  }
})

test('completion conflict fetches actual status instead of claiming pause', async () => {
  let fetches = 0
  const h = courseHarness({
    cancelOpenMAICClassroom: async () => { throw { status: 409, code: 'task_not_cancellable' } },
    fetchOpenMAICClassroomStatus: async () => { fetches++; return task('succeeded') },
  })
  try {
    assert.equal(await h.state.cancelReview('request-1'), true)
    assert.equal(fetches, 1)
    assert.equal(h.state.classroomResult.value.generation_status, 'succeeded')
  } finally { h.app.unmount() }
})

test('duplicate clicks send once; switching context or unmount ignores old response', async () => {
  for (const unmount of [false, true]) {
    const result = deferred(); let calls = 0
    const h = courseHarness({ cancelOpenMAICClassroom: () => { calls++; return result.promise } })
    const pending = h.state.cancelReview('request-1')
    assert.equal(await h.state.cancelReview('request-1'), false)
    assert.equal(calls, 1)
    if (unmount) h.app.unmount()
    else { h.contextKey.value = 'conversation-2'; await flush(); h.state.classroomResult.value = task('running', 'request-2') }
    result.resolve(task('cancelled'))
    assert.equal(await pending, false)
    assert.notEqual(h.state.classroomResult.value?.generation_status, 'cancelled')
    if (!unmount) h.app.unmount()
  }
})

test('session materialization cannot interrupt cancellation retry or restore stale running state', async () => {
  const restore = deferred(), firstCancel = deferred(); let calls = 0
  const h = courseHarness({
    fetchOpenMAICClassroomStatus: () => restore.promise,
    cancelOpenMAICClassroom: async () => { calls++; if (calls === 1) { await firstCancel.promise; throw { message: 'offline' } }; return task('cancelled') },
  }, 'request-1')
  try {
    const pending = h.state.cancelReview('request-1')
    h.contextKey.value = 'real-session'; await flush()
    firstCancel.resolve(); await pending
    assert.equal(calls, 2, 'already authorized cancellation survives pending-to-real session change')
    restore.resolve(task()); await flush()
    assert.equal(h.state.classroomResult.value.generation_status, 'cancelled')
  } finally { h.app.unmount() }
})

test('starting a course cannot replace confirmed cancellation with an older status lookup', async () => {
  const lookup = deferred(); let prepares = 0
  const h = courseHarness({
    fetchOpenMAICClassroomStatus: () => lookup.promise,
    cancelOpenMAICClassroom: async () => task('cancelled'),
    prepareOpenMAICClassroom: async () => { prepares++; return task('queued', 'new-task') },
  })
  try {
    const starting = h.state.startForVideo({ id: 'video-1', title: '原课程' })
    await h.state.cancelReview('request-1')
    lookup.resolve(task()); await starting
    assert.equal(h.state.classroomResult.value.generation_status, 'cancelled')
    assert.equal(prepares, 0)
  } finally { h.app.unmount() }
})

for (const globalMode of [true, false]) {
  test(`${globalMode ? 'video list' : 'video detail'} pauses the current review even before stop is ready; ordinary follow-up keeps history`, async () => {
    const source = readFileSync(new URL('../components/videohub/AiAssistant.vue', import.meta.url), 'utf8')
    const send = source.match(/async function send\([^)]*\) \{[\s\S]*?\n}/)?.[0]
    const pause = source.match(/async function pauseGeneration\([^)]*\) \{[\s\S]*?\n}/)?.[0]
    assert.ok(send && pause)
    const cancellations = []; let stops = 0; let options; let turn = deferred()
    const session = { id: 'session', messages: [] }
    const context = {
      input: vue.ref(''), expanded: vue.ref(false), isGenerating: vue.ref(false),
      preparingClassroom: vue.ref(false), cancellingClassroom: vue.ref(false),
      activeReviewRequestId: vue.ref(''), pauseRequested: false, scopeRevision: 0,
      sessionCacheKey: vue.ref('scope'), lastUserQuery: vue.ref(''),
      streamingAssistantId: vue.ref(''), stopCurrentTurn: vue.ref(null),
      activeSession: vue.ref(session), messages: vue.ref([]),
      props: { globalMode, currentVideo: { id: 'video-1', title: '原课程' } },
      needsReviewCourseChoice: () => false, isReviewIntent: materials.isReviewIntent,
      reviewRequestId: materials.reviewRequestId, VIDSAGE_REASONING_AGENT_ID: 'reasoning',
      settingsStore: { settings: {} }, shouldAutoRoute: () => false,
      recoveryStorage: null, assistantRecoveryKey: () => 'key', writeChatRecovery() {}, clearChatRecovery() {},
      currentChatMessageTime: () => '12:00', scrollBottom: async () => {},
      createChatTurn: async (_, value) => { options = value; return turn.promise },
      cancelReview: async id => { cancellations.push(id); return true },
      cacheSession() {}, welcome: () => ({}), MessagePlugin: { error() {} },
    }
    const compiled = ts.transpileModule(`${send}\n${pause}`, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText
    const methods = runInNewContext(`${compiled}\n({send,pauseGeneration})`, context)
    const first = methods.send(materials.reviewInstruction('request-1')); await flush()
    await methods.pauseGeneration()
    assert.deepEqual(cancellations, ['request-1'])
    options.onStopReady(async () => { stops++ })
    assert.equal(stops, 1, 'late chat stop callback is invoked immediately')
    turn.resolve(session); await first
    turn = deferred()
    const followup = methods.send('请再解释一下'); await flush()
    options.onStopReady(async () => { stops++ })
    await methods.pauseGeneration()
    assert.deepEqual(cancellations, ['request-1'], 'ordinary turn must not cancel historical review')
    assert.equal(stops, 2)
    turn.resolve(session); await followup
  })
}

test('cancel API encodes identity, sends strict empty object, exposes conflict code', async () => {
  const requests = []
  const api = load('../api/videohub/openmaicClassroom.ts', { '@/utils/request': {
    post: async (...args) => { requests.push(args); if (requests.length === 2) throw { status: 409, error: 'task_not_cancellable' }; return { success: true, data: task('cancelled') } },
  } })
  assert.equal((await api.cancelOpenMAICClassroom('a/b')).generation_status, 'cancelled')
  assert.equal(requests[0][0], '/api/custom/learning/openmaic/a%2Fb/cancel')
  assert.equal(JSON.stringify(requests[0][1]), '{}')
  await assert.rejects(api.cancelOpenMAICClassroom('a/b'), error => error.status === 409 && error.code === 'task_not_cancellable')
})

test('card cancellation invalidates old poll, preserves subtitle and stops polling', async () => {
  const poll = deferred(), title = deferred(); const timers = new Set(); let calls = 0
  const Card = load('../components/videohub/ReviewMaterialReply.vue', {
    vue, '@/api/videohub/reviewMaterials': materials,
    '@/api/videohub': { fetchVideoDetail: () => title.promise },
    '@/api/videohub/openmaicClassroom': { fetchOpenMAICClassroomStatus: () => { calls++; return poll.promise } },
  }, true, { localStorage: { getItem: () => null }, setTimeout: fn => { timers.add(fn); return fn }, clearTimeout: fn => timers.delete(fn) }).default
  const props = vue.reactive({ requestId: 'request-1', userId: 'user', initialTask: task(), video: { id: 'video-1', title: '原课程' } })
  const instance = vue.ref()
  const Component = { ...Card, render: () => null }
  const app = renderer.createApp({ render: () => vue.h(Component, { ...props, ref: instance }) })
  app.mount({}); await flush()
  try {
    const state = instance.value.$.setupState
    props.initialTask = task('cancelled'); await flush()
    title.resolve({ title: '原课程完整名称' }); poll.resolve({ ...task('succeeded'), classroom_url: 'https://late.invalid' }); await flush()
    assert.equal(state.presentation.title, '复习材料暂停生成')
    assert.equal(state.presentation.href, '')
    assert.equal(state.videoTitle, '原课程完整名称')
    assert.equal(timers.size, 0)
    assert.equal(calls, 1)
  } finally { app.unmount() }
})

test('restored cancelled card never starts polling', async () => {
  let calls = 0
  const Card = load('../components/videohub/ReviewMaterialReply.vue', {
    vue, '@/api/videohub/reviewMaterials': materials, '@/api/videohub': {},
    '@/api/videohub/openmaicClassroom': { fetchOpenMAICClassroomStatus: async () => { calls++; return task() } },
  }, true, { localStorage: { getItem: () => null } }).default
  const app = renderer.createApp({ ...Card, render: () => null }, { requestId: 'request-1', userId: 'user', initialTask: task('cancelled') })
  app.mount({}); await flush(); assert.equal(calls, 0); app.unmount()
})
