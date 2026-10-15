import assert from 'node:assert/strict'
import { mkdir } from 'node:fs/promises'
import path from 'node:path'
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { chromium } from 'playwright'

const root = path.resolve(import.meta.dirname, '..')
const evidence = path.resolve(root, '../../output/acceptance/assistant-reply-meta')
await mkdir(evidence, { recursive: true })
const server = await createServer({
  root, configFile: false, plugins: [vue(), {
    name: 'reply-meta-visual-check',
    resolveId(id) { if (id === '/reply-meta-entry.js') return id },
    load(id) {
      if (id === '/reply-meta-entry.js') return `
        import {createApp} from 'vue';
        import 'tdesign-vue-next/es/style/index.css';
        import '/src/assets/theme/theme.css';
        import App from '/scripts/fixtures/assistant-reply-meta-harness.vue';
        createApp(App).mount('#app');`
    },
    configureServer(server) {
      server.middlewares.use('/reply-meta-check', async (_req, res) => {
        res.setHeader('Content-Type', 'text/html')
        res.end(await server.transformIndexHtml('/reply-meta-check', '<div id="app"></div><script type="module" src="/reply-meta-entry.js"></script>'))
      })
    },
  }], resolve: { alias: { '@': path.join(root, 'src') } },
  server: { host: '127.0.0.1', port: 0, hmr: false },
})
await server.listen()
const browser = await chromium.launch({ headless: true })
const page = await browser.newPage()
const errors = []
page.on('pageerror', error => errors.push(error.message))
try {
  // This is a read-only check of the required real entry, not a deployment.
  const response = await page.goto('http://127.0.0.1/platform/videos')
  assert.equal(response.status(), 200)
  console.log('Fixed entry reachable:', page.url())
  const upstream = `http://127.0.0.1:${server.httpServer.address().port}/reply-meta-check`
  await page.goto(upstream)
  await page.locator('.assistant-reply-shell__avatar').first().waitFor()
  for (const width of [1024, 390, 320]) {
    await page.setViewportSize({ width, height: 1000 })
    const result = await page.locator('.assistant-reply-shell').evaluateAll(shells => shells.map(shell => {
      const avatar = shell.querySelector('img')
      const body = shell.querySelector('.assistant-reply-shell__body')
      const a = avatar.getBoundingClientRect(), b = body.getBoundingClientRect()
      return { width: a.width, height: a.height, gap: b.left - a.right,
        bodyWidth: b.width, radius: getComputedStyle(avatar).borderRadius,
        loaded: avatar.complete && avatar.naturalWidth > 0 }
    }))
    for (const item of result) {
      assert.equal(item.width, width > 640 ? 32 : 28)
      assert.equal(item.height, item.width)
      assert.equal(item.gap, width > 640 ? 10 : 8)
      assert.equal(item.loaded, true)
      assert.equal(item.radius, '50%')
      assert.ok(item.bodyWidth > 0)
    }
    assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true)
    assert.equal(await page.locator('[data-case="missing"] time').count(), 0)
    assert.equal(await page.locator('time').count(), 3)
    for (const text of await page.locator('time').allTextContents()) assert.match(text, /^\d{2}月\d{2}日 \d{2}:\d{2}$/u)
    await page.screenshot({ path: path.join(evidence, `reply-meta-${width}.png`), fullPage: true })
  }
  assert.deepEqual(errors, [])
  console.log('PASS: actual shared Vue component; 1024/390/320px avatar, gap, image load, missing time, strict format and overflow checks.')
} finally { await browser.close(); await server.close() }
