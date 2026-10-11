import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { chromium } from 'playwright'

const root = path.resolve(import.meta.dirname, '..')
const evidence = path.resolve(root, '../../output/acceptance/review-material-entry')
await mkdir(evidence, { recursive: true })
const harness = '/scripts/fixtures/review-materials-harness.vue'
const server = await createServer({ root, configFile: false, plugins: [vue(), {
  name: 'isolated-review-entry',
  resolveId(id) { if (id === '/review-entry.js') return id },
  load(id) { if (id === '/review-entry.js') return `import {createApp} from 'vue'; import TDesign from 'tdesign-vue-next'; import 'tdesign-vue-next/es/style/index.css'; import '/src/assets/theme/theme.css'; import App from '${harness}'; createApp(App).use(TDesign).mount('#app')` },
}], optimizeDeps: { entries: [harness.slice(1)], include: ['vue', 'tdesign-vue-next'] },
  resolve: { alias: [
    { find: /^@\/api\/videohub$/, replacement: path.join(root, 'scripts/fixtures/review-videos.ts') },
    { find: /^@\/utils\/request$/, replacement: path.join(root, 'scripts/fixtures/review-request.ts') },
    { find: '@', replacement: path.join(root, 'src') },
  ] }, server: { host: '127.0.0.1', port: 0, hmr: false },
})
await server.listen()
const upstream = `http://127.0.0.1:${server.httpServer.address().port}`
const browser = await chromium.launch({ headless: true })
const page = await browser.newPage({ viewport: { width: 1024, height: 1000 } })
const errors = []
page.on('pageerror', error => errors.push(error.message))
const courses = ['a', 'b'].map(suffix => ({ id: `course-${suffix}`, title: suffix === 'a' ? 'AI认知——什么是MCP和Skills' : '第二门课程：提示词设计', status: 'completed', initiallyAvailable: true, video_url: '/course.mp4' }))
const task = (id, status = 'running', progress) => ({ request_id: id, generation_status: status, progress,
  classroom_id: 'stage-test', classroom_url: status === 'succeeded' ? 'https://open.maic.chat/classroom/stage-test' : '', status: 'not_started' })
let listMode = 'ok', delayPrepare = false, delayRestore = false
let releasePrepare, releaseRestore
const prepared = [], starts = [], statuses = new Map(), statusReads = new Map()
await page.exposeFunction('recordReviewStart', value => starts.push(value))
await page.addInitScript(() => window.addEventListener('review-test-start', event => window.recordReviewStart(event.detail)))
await page.route('http://127.0.0.1/**', async route => {
  const url = new URL(route.request().url())
  const json = data => route.fulfill({ contentType: 'application/json', body: JSON.stringify(data) })
  if (url.pathname === '/api/custom/videos') {
    if (listMode === 'error') return route.fulfill({ status: 503, body: '{}' })
    return json(listMode === 'empty' ? [] : courses)
  }
  if (url.pathname.startsWith('/api/custom/videos/')) return json(courses.find(course => course.id === url.pathname.split('/').at(-1)))
  if (url.pathname.endsWith('/prepare')) {
    prepared.push(JSON.parse(route.request().postData()))
    if (delayPrepare) await new Promise(resolve => { releasePrepare = resolve })
    const id = `request-${prepared.length}`
    const result = task(id, 'queued', null)
    statuses.set(id, result)
    return json({ success: true, data: result })
  }
  if (url.pathname.endsWith('/status')) {
    const id = url.pathname.split('/').at(-2)
    statusReads.set(id, (statusReads.get(id) || 0) + 1)
    if (delayRestore) await new Promise(resolve => { releaseRestore = resolve })
    const result = statuses.get(id)
    if (result === 'denied') return route.fulfill({ status: 403, body: '{}' })
    if (result === 'offline') return route.fulfill({ status: 503, body: '{}' })
    if (result === 'missing') return route.fulfill({ status: 404, body: '{}' })
    return json({ success: true, data: result || task(id) })
  }
  if (url.pathname === '/platform/videos') {
    const html = await server.transformIndexHtml(url.pathname, `<div id="app"></div><script type="module" src="/review-entry.js"></script>`)
    return route.fulfill({ contentType: 'text/html', body: html })
  }
  const response = await route.fetch({ url: `${upstream}${url.pathname}${url.search}` })
  return route.fulfill({ response })
})
const open = async query => { await page.goto(`http://127.0.0.1/platform/videos${query || ''}`); await page.getByRole('button', { name: '巩固学习', exact: true }).waitFor() }
const ask = () => page.getByRole('button', { name: '巩固学习', exact: true }).click()
const select = async title => {
  await page.locator('.t-select').click()
  await page.locator('.t-select-option').filter({ hasText: title }).click()
  await page.waitForFunction(() => !!window.reviewTest.selectedCourseId.value)
}
try {
  await open()
  await page.evaluate(() => localStorage.clear())
  await open()
  await ask()
  await page.getByText('你想巩固学习哪一门课程？').waitFor()
  assert.equal(await page.getByRole('button', { name: '确认并生成' }).isDisabled(), true)
  assert.equal(prepared.length, 0)
  await page.getByRole('button', { name: '取消', exact: true }).click()
  assert.equal(starts.length, 0)
  await ask()
  await select(courses[1].title)
  assert.equal(prepared.length, 0)
  await page.getByRole('button', { name: '确认并生成' }).click()
  await page.getByText('帮我定制复习材料', { exact: true }).waitFor()
  assert.equal(prepared.length, 1)
  assert.deepEqual(Object.keys(prepared[0]).sort(), ['requirement', 'video_id'])
  assert.equal(prepared[0].video_id, 'course-b')
  assert.equal(starts[0].videoId, 'course-b')
  assert.match(starts[0].instruction, /request_id 必须逐字复制为 request-1。/)
  assert.equal(await page.locator('body').innerText().then(text => text.includes('openmaic_start')), false)
  await page.locator('.review-card__course').filter({ hasText: courses[1].title }).waitFor()
  // Queued kickoff retry reuses the same prepared identity.
  await ask(); await select(courses[1].title); await page.getByRole('button', { name: '确认并生成' }).click()
  await page.waitForFunction(() => window.reviewTest.preparingClassroom.value === false)
  assert.equal(prepared.length, 1)
  assert.equal(starts.length, 2)
  assert.equal(starts[1].instruction, starts[0].instruction)
  assert.equal(await page.locator('.review-card').count(), 1)
  // Pending remote generation blocks creation of a second task.
  statuses.set('request-1', task('request-1', 'running', 37))
  await ask(); await select(courses[0].title); await page.getByRole('button', { name: '确认并生成' }).click()
  await page.waitForFunction(() => !window.reviewTest.preparingClassroom.value)
  assert.equal(prepared.length, 1)
  // Restoration locks actions until backend state is known.
  delayRestore = true
  await open()
  assert.equal(await page.getByRole('button', { name: '巩固学习', exact: true }).isDisabled(), true)
  assert.equal(prepared.length, 1)
  delayRestore = false; releaseRestore()
  await page.getByRole('button', { name: '巩固学习', exact: true }).waitFor({ state: 'visible' })
  // Failed restoration cannot bypass the persisted pending identity.
  statuses.set('request-1', 'offline')
  await open(); await ask(); await select(courses[0].title)
  await page.getByRole('button', { name: '确认并生成' }).click()
  await page.waitForFunction(() => !window.reviewTest.preparingClassroom.value)
  assert.equal(prepared.length, 1)
  statuses.set('request-1', task('request-1', 'running', 37))
  // Empty/error lists and recovery do not issue generation requests.
  await page.evaluate(() => localStorage.clear()); listMode = 'empty'; await open(); await ask()
  await page.getByText('暂无可复习的课程，请先上传课程视频。').waitFor()
  assert.equal(await page.getByRole('button', { name: '确认并生成' }).isDisabled(), true)
  listMode = 'error'; await page.getByRole('button', { name: '取消', exact: true }).click(); await ask()
  await page.getByText('课程列表暂时无法加载，请重试', { exact: false }).waitFor()
  listMode = 'ok'; await page.getByRole('button', { name: '重试', exact: true }).click(); await select(courses[0].title)
  // Double confirm creates once; navigating away during preparation never invokes the skill.
  delayPrepare = true
  await page.evaluate(() => { void window.reviewTest.confirmCourse(); void window.reviewTest.confirmCourse() })
  await page.waitForFunction(() => window.reviewTest.preparingClassroom.value)
  await page.waitForTimeout(100)
  assert.equal(prepared.length, 2)
  await page.evaluate(() => { window.reviewTest.contextKey.value = 'chat-b' })
  delayPrepare = false; releasePrepare()
  await page.waitForTimeout(150)
  assert.equal(starts.length, 2)
  // A definitively deleted cached task permits a fresh confirmed request.
  await page.evaluate(() => localStorage.setItem('test-review-latest', 'deleted-request'))
  statuses.set('deleted-request', 'missing')
  await open(); await ask(); await select(courses[0].title)
  await page.getByRole('button', { name: '确认并生成' }).click()
  await page.waitForFunction(() => !window.reviewTest.preparingClassroom.value)
  assert.equal(prepared.length, 3)
  // Four states, actual title, whole-card link and reduced-motion behavior.
  statuses.set('running', task('running', 'running', 37))
  statuses.set('unknown', task('unknown', 'queued', null))
  statuses.set('failed', task('failed', 'failed', 70))
  statuses.set('ready', task('ready', 'succeeded', 100))
  await page.evaluate(() => localStorage.clear()); await open('?gallery=1')
  await page.getByRole('status').filter({ hasText: /^复习材料已生成$/ }).waitFor()
  assert.equal(await page.locator('.review-card').count(), 4)
  for (const title of ['复习材料生成中，进度37%', '复习材料生成中，正在获取进度', '复习材料生成失败']) await page.getByRole('status').filter({ hasText: title }).waitFor()
  const ready = page.locator('a.review-card')
  assert.equal(await ready.getAttribute('target'), '_blank')
  assert.equal(await ready.getAttribute('href'), 'https://open.maic.chat/classroom/stage-test')
  assert.equal(await ready.locator('button, progress, .review-card__body').count(), 0)
  assert.equal(await ready.innerText().then(text => text.includes('%')), false)
  assert.equal(await page.locator('.review-card__document').first().evaluate(el => getComputedStyle(el).color), 'rgb(7, 192, 95)')
  assert.match(await page.locator('.review-card[data-state="running"]').evaluate(el => getComputedStyle(el, '::after').animationName), /^review-shimmer(?:-|$)/)
  await page.screenshot({ path: path.join(evidence, 'review-card-desktop.png'), fullPage: true })
  for (const width of [390, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
    await page.screenshot({ path: path.join(evidence, `review-card-${width}.png`), fullPage: true })
  }
  await page.emulateMedia({ reducedMotion: 'reduce' })
  assert.equal(await page.locator('.review-card[data-state="running"]').evaluate(el => getComputedStyle(el, '::after').animationName), 'none')
  // Refresh restores course by ID even without a supplied video object.
  await page.evaluate(() => { localStorage.setItem('openmaic-source:test-user:running', 'course-b') })
  await open('?gallery=1')
  await page.locator('.review-card[data-state="running"] .review-card__course').filter({ hasText: courses[1].title }).waitFor()
  // Definitive access rejection stops polling and removes the clickable result.
  statuses.set('running', 'denied'); await open('?gallery=1')
  await page.getByRole('status').filter({ hasText: /^复习材料暂不可访问$/ }).waitFor()
  const reads = statusReads.get('running')
  await page.waitForTimeout(5200)
  assert.equal(statusReads.get('running'), reads)
  assert.deepEqual(errors, [])
  console.log('PASS: confirmation, cancellation, chosen source, queued retry, restoration lock, pending guard, errors/empty, double-confirm, context switch, four states, links, refresh, mobile, reduced motion, access denial; no real generation calls.')
} catch (error) {
  console.error('Browser errors:', errors, '\nPage:', await page.locator('body').innerText())
  throw error
} finally { await browser.close(); await server.close() }
