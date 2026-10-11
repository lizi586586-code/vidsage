import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { chromium } from 'playwright'

const root = path.resolve(import.meta.dirname, '..')
const evidence = path.resolve(root, '../../output/acceptance/invite-landing')
await mkdir(evidence, { recursive: true })
const server = await createServer({ root, configFile: false, plugins: [vue(), {
  name: 'invite-landing-check',
  resolveId(id) { if (id === '/invite-check.js') return id },
  load(id) {
    if (id !== '/invite-check.js') return
    return `import {createApp,h} from 'vue'; import {createPinia} from 'pinia'; import {createRouter,createWebHistory,RouterView} from 'vue-router'; import TDesign from 'tdesign-vue-next'; import i18n from '/src/i18n/index.ts'; import InviteLanding from '/src/views/auth/InviteLanding.vue'; import 'tdesign-vue-next/es/style/index.css'; import '/src/assets/theme/theme.css'; const router=createRouter({history:createWebHistory(),routes:[{path:'/register',component:InviteLanding},{path:'/platform/videos',component:{render:()=>h('h1','已加入空间')}}]}); window.inviteCheckRouter=router; createApp({render:()=>h(RouterView)}).use(createPinia()).use(i18n).use(TDesign).use(router).mount('#app');`
  },
}], resolve: { alias: { '@': path.join(root, 'src') } }, server: { host: '127.0.0.1', port: 0, hmr: false } })
await server.listen()
const upstream = `http://127.0.0.1:${server.httpServer.address().port}`
const browser = await chromium.launch({ headless: true })
const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } })
const errors = [], submitted = []
page.on('pageerror', error => errors.push(error.message))
page.on('console', message => { if (message.type() === 'error') console.error(message.text()) })
await page.route(`${upstream}/**`, async route => {
  const url = new URL(route.request().url())
  const json = data => route.fulfill({ contentType: 'application/json', body: JSON.stringify(data) })
  if (url.pathname === '/api/v1/auth/invitations/lookup') {
    const { token } = route.request().postDataJSON()
    if (token === 'invalid') return json({ success: false, message: '邀请已失效' })
    return json({ success: true, data: { tenant_id: 42, owner_username: token === 'second' ? '李四' : '张三', tenant_name: token === 'second' ? '第二个学习空间' : '前沿大模型与应用实战空间', tenant_description: '老师的真实空间描述', role: 'viewer' } })
  }
  if (url.pathname === '/api/v1/auth/config') return json({ registration_mode: 'invite_only' })
  if (url.pathname === '/api/v1/auth/oidc/config') return json({ success: true, enabled: false })
  if (url.pathname === '/api/v1/auth/login-by-invite') {
    submitted.push(route.request().postDataJSON())
    return json({ success: true, token: 'mock-access', user: { id: 'learner', email: 'learner@example.com', username: '学员', tenant_id: 42 }, active_tenant: { id: 42, name: '邀请空间' }, memberships: [] })
  }
  if (url.pathname === '/register' || url.pathname === '/platform/videos') {
    const html = await server.transformIndexHtml(url.pathname, '<style>body{margin:0;font-family:var(--app-font-family)}</style><script src="/tdesign-icons/0.4.1/fonts/index.js"></script><div id="app"></div><script type="module" src="/invite-check.js"></script>')
    return route.fulfill({ contentType: 'text/html', body: html })
  }
  return route.continue()
})
try {
  await page.goto(`${upstream}/register?token=first`)
  await page.getByRole('heading', { name: '前沿大模型与应用实战空间' }).waitFor()
  assert.equal(await page.locator('.invite-brand strong').textContent(), '张三老师的学习空间')
  assert.equal(await page.locator('.invite-hero p').textContent(), '老师的真实空间描述')
  assert.equal(await page.locator('.t-dialog:visible').count(), 0)
  assert.equal(submitted.length, 0)
  await page.screenshot({ path: path.join(evidence, 'desktop.png'), fullPage: true })
  for (const name of ['直接进入空间', '加入学习空间', '立即进入学习空间']) {
    await page.getByRole('button', { name, exact: false }).click()
    await page.locator('.t-dialog:visible input[type=password]').waitFor()
    await page.locator('.t-dialog:visible .t-dialog__close').click()
  }
  await page.getByRole('button', { name: '加入学习空间', exact: false }).click()
  await page.evaluate(() => window.inviteCheckRouter.push('/register?token=second'))
  await page.getByRole('heading', { name: '第二个学习空间' }).waitFor()
  assert.equal(await page.locator('.invite-brand strong').textContent(), '李四老师的学习空间')
  await page.locator('.t-dialog:visible').waitFor({ state: 'hidden' })
  assert.equal(await page.locator('.t-dialog:visible').count(), 0)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.screenshot({ path: path.join(evidence, 'mobile.png'), fullPage: true })
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth), true)
  await page.getByRole('button', { name: '加入学习空间', exact: false }).click()
  const dialog = page.locator('.t-dialog:visible')
  await dialog.locator('input[type=password]').waitFor()
  await dialog.locator('input').first().fill('learner@example.com')
  await dialog.locator('input[type=password]').fill('oldpwd')
  await page.waitForFunction(() => {
    const bounds = document.querySelector('.t-dialog').getBoundingClientRect()
    return bounds.width > 300
  })
  await page.screenshot({ path: path.join(evidence, 'mobile-login.png') })
  await dialog.getByRole('button', { name: '登录', exact: true }).click()
  await page.getByRole('heading', { name: '已加入空间' }).waitFor()
  assert.deepEqual(submitted, [{ token: 'second', email: 'learner@example.com', password: 'oldpwd' }])
  await page.goto(`${upstream}/register?token=invalid`)
  await page.getByRole('heading', { name: '邀请链接无效或已失效' }).waitFor()
  await page.goto(`${upstream}/register`)
  await page.getByText('请使用完整的邀请链接打开学习空间。').waitFor()
  assert.deepEqual(errors, [])
  console.log('PASS: dynamic metadata, desktop/mobile layout, three join buttons, token changes, login submission and invalid invitations (isolated mocked APIs).')
} catch (error) {
  console.error('Page errors:', errors, 'Body:', await page.locator('body').innerText())
  throw error
} finally {
  await browser.close()
  await server.close()
}
