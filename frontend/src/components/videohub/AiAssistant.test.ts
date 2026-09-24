import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const componentSource = readFileSync(resolve(import.meta.dirname, 'AiAssistant.vue'), 'utf8')

test('video assistant suggestion chips trigger chat as plain buttons', () => {
  assert.match(
    componentSource,
    /<button\s+v-for="item in suggestions"[^>]*type="button"[^>]*@click="send\(item\)"/,
  )
})

test('video assistant input unlocks after chat turn finalizes', () => {
  assert.match(componentSource, /finally\s*\{[^}]*isGenerating\.value\s*=\s*false/s)
  assert.match(componentSource, /finally\s*\{[^}]*streamingAssistantId\.value\s*=\s*''/s)
})

test('video assistant disables protected image hydration in native agent display', () => {
  assert.match(componentSource, /:hydrate-protected-images="false"/)
})

test('video assistant supports keyboard activation for rendered video citations', () => {
  assert.match(componentSource, /@keydown="handleRenderedAnswerKeydown"/)
  assert.match(componentSource, /function handleRenderedAnswerKeydown\(event: KeyboardEvent\)/)
  assert.match(componentSource, /target\.closest\?\.\('\.video-citation'\)/)
  assert.match(componentSource, /videoCitation\.tagName === 'BUTTON'/)
  assert.match(componentSource, /event\.preventDefault\(\)/)
})

test('video citations use the shared navigation path for current and cross-video targets', () => {
  const handler = componentSource.match(/function handleVideoNavigate[\s\S]*?\n}/)?.[0] || ''
  assert.match(handler, /emit\('navigate', videoId, seconds\)/)
  assert.doesNotMatch(handler, /currentVideo|emit\('seek'/)
})

test('video assistant keeps the created session before streaming finishes', () => {
  assert.match(componentSource, /function materializeActiveSession\(session: ChatSession\)/)
  assert.match(componentSource, /onSessionCreated: materializeActiveSession/)
})

test('video assistant requests automatic routing when no agent is selected', () => {
  assert.match(componentSource, /agentEnabled:\s*settingsStore\.isAgentEnabled,\s*autoRoute:\s*true/)
})
