import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const componentSource = readFileSync(resolve(import.meta.dirname, 'AssistantReplyShell.vue'), 'utf8')
const avatar = readFileSync(resolve(import.meta.dirname, '../../assets/img/vidsage-ai-assistant.png'))

test('assistant reply shell uses the confirmed IP image and hides absent timestamps', () => {
  assert.match(componentSource, /vidsage-ai-assistant\.png/)
  assert.match(componentSource, /alt="AI 助手"/)
  assert.match(componentSource, /<time v-if="timestamp"/)
  assert.ok(avatar.length > 24)
  assert.equal(avatar.readUInt32BE(16), 1254)
  assert.equal(avatar.readUInt32BE(20), 1254)
})

test('assistant reply shell keeps the approved desktop and narrow-screen geometry', () => {
  assert.match(componentSource, /grid-template-columns:\s*32px minmax\(0, 1fr\)/)
  assert.match(componentSource, /gap:\s*10px/)
  assert.match(componentSource, /width:\s*32px[\s\S]*height:\s*32px/)
  assert.match(componentSource, /border-radius:\s*var\(--td-radius-circle\)/)
  assert.match(componentSource, /@media \(max-width: 640px\)[\s\S]*grid-template-columns:\s*28px minmax\(0, 1fr\)/)
})
