import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const componentSource = readFileSync(resolve(import.meta.dirname, 'ChatView.vue'), 'utf8')

test('chat page disables protected image hydration in native agent display', () => {
  assert.match(componentSource, /:hydrate-protected-images="false"/)
})

test('chat page lets the conversation message list scroll inside the fixed chat viewport', () => {
  assert.match(componentSource, /\.conversation\s*\{[^}]*height:\s*calc\(100% - 4px\)[^}]*min-height:\s*0[^}]*overflow:\s*hidden/s)
  assert.match(componentSource, /\.conversation__messages\s*\{[^}]*min-height:\s*0[^}]*overflow-y:\s*auto[^}]*overscroll-behavior:\s*contain/s)
})

test('chat page renders assistant answers without a separate identity column', () => {
  assert.match(componentSource, /\.message--assistant\s*\{[^}]*display:\s*block/s)
  assert.match(componentSource, /\.message--assistant \.message__bubble\s*\{[^}]*width:\s*100%/s)
})

test('chat page materializes pending sessions as soon as the backend creates them', () => {
  assert.match(componentSource, /function materializePendingSession\(pendingSession: ChatSession, createdSession: ChatSession\)/)
  assert.match(componentSource, /onSessionCreated: createdSession => materializePendingSession\(session, createdSession\)/)
})
