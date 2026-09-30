import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const componentSource = readFileSync(resolve(import.meta.dirname, 'ChatView.vue'), 'utf8')

test('conversation header displays only the session title without a subtitle', () => {
  assert.match(componentSource, /<header><div><strong>\{\{ activeSession\.title \}\}<\/strong><\/div><\/header>/)
  assert.doesNotMatch(componentSource, /<t-button[^>]*>.*返回/)
  assert.doesNotMatch(componentSource, /<header>.*?<span[^>]*>\{\{ activeSession\.videoTitle/s)
})

test('chat page disables protected image hydration in native agent display', () => {
  assert.match(componentSource, /:hydrate-protected-images="false"/)
})

test('chat page lets the conversation message list scroll inside the fixed chat viewport', () => {
  assert.match(componentSource, /\.conversation\s*\{[^}]*height:\s*calc\(100% - 4px\)[^}]*min-height:\s*0[^}]*overflow:\s*hidden/s)
  assert.match(componentSource, /\.conversation__messages\s*\{[^}]*min-height:\s*0[^}]*overflow-y:\s*auto[^}]*overscroll-behavior:\s*contain/s)
})

test('chat page shows a conversation skeleton while routed session data is loading', () => {
  assert.match(componentSource, /<section v-if="conversationLoading" class="conversation conversation--loading"/)
  assert.match(componentSource, /const conversationLoading = ref\(Boolean\(route\.query\.session\)\)/)
  assert.match(componentSource, /conversationSkeletonRows/)
  assert.match(componentSource, /<t-skeleton animation="gradient"/)
  assert.match(componentSource, /\.conversation__messages--loading\s*\{[^}]*display:\s*flex[^}]*gap:\s*20px/s)
  assert.match(componentSource, /\.message-skeleton--user\s*\{[^}]*justify-content:\s*flex-end/s)
  assert.match(componentSource, /\.message-skeleton--user > :deep\(\.t-skeleton\)\s*\{[^}]*width:\s*min\(42%, 420px\)/s)
})

test('chat page avoids a full-page backdrop filter that can blank during hover repaint', () => {
  assert.doesNotMatch(componentSource, /\.chat-page::after\s*\{[^}]*backdrop-filter/s)
  assert.doesNotMatch(componentSource, /\.chat-page::after\s*\{[^}]*-webkit-backdrop-filter/s)
})

test('chat page renders assistant answers without a separate identity column', () => {
  assert.match(componentSource, /\.message--assistant\s*\{[^}]*display:\s*block/s)
  assert.match(componentSource, /\.message--assistant \.message__bubble\s*\{[^}]*width:\s*100%/s)
})

test('chat page wraps overflowing conversation content within the safe width', () => {
  assert.match(componentSource, /\.conversation__messages\s*\{[^}]*min-width:\s*0[^}]*overflow-x:\s*hidden/s)
  assert.match(componentSource, /\.message-user-content > p\s*\{[^}]*overflow-wrap:\s*anywhere[^}]*word-break:\s*break-word/s)
  assert.match(componentSource, /\.message--assistant :deep\(\.markdown-content\)[\s\S]*?overflow-wrap:\s*anywhere/s)
})

test('chat page keeps the follow-up composer inside the conversation width', () => {
  assert.match(componentSource, /\.conversation-composer\s*\{[^}]*width:\s*100%[^}]*min-width:\s*0/s)
  assert.match(componentSource, /\.conversation-composer textarea\s*\{[^}]*overflow-wrap:\s*anywhere/s)
  assert.match(componentSource, /\.conversation__messages\s*\{[^}]*padding:\s*16px 0 24px/s)
  assert.match(componentSource, /\.conversation-composer\s*\{[^}]*padding:\s*10px 0 10px 12px/s)
})

test('chat page uses text-only edit actions without hover blocks', () => {
  assert.match(componentSource, /message-user-content__text--editable/)
  assert.doesNotMatch(componentSource, /name="edit"|> 编辑</)
  assert.match(componentSource, /\.message-user-content__text--editable:hover,[\s\S]*?background:\s*transparent[\s\S]*?color:\s*var\(--td-brand-color\)/)
  assert.match(componentSource, /\.message-edit__actions :deep\(\.t-button:hover\)[\s\S]*?background:\s*transparent/)
})

test('chat page shows the supplied edit icon below a user instruction on hover', () => {
  assert.match(componentSource, /class="message-user-content__edit"[\s\S]*?aria-label="编辑指令"[\s\S]*?@click="startMessageEdit\(message\)"/)
  assert.match(componentSource, /<path d="M690\.816 171\.84[\s\S]*?fill="currentColor"/)
  assert.match(componentSource, /\.message-user-content:hover \.message-user-content__edit,[\s\S]*?\.message-user-content:focus-within \.message-user-content__edit \{ opacity: 1; \}/)
  assert.match(componentSource, /\.message-user-content__edit:hover,[\s\S]*?background:\s*transparent[\s\S]*?color:\s*var\(--td-brand-color\)/)
})

test('chat page swaps the send action for an icon-only pause control while generating', () => {
  assert.match(componentSource, /<t-tooltip v-if="isGenerating" content="暂停生成"/)
  assert.match(componentSource, /aria-label="暂停生成" @click="pauseGeneration"/)
  assert.match(componentSource, /<t-icon name="pause-circle" \/>/)
  assert.match(componentSource, /onStopReady: stop => \{ stopCurrentTurn\.value = stop \}/)
})

test('chat page clears submitted drafts while keeping the composer editable during generation', () => {
  const input = componentSource.match(/<textarea\s+v-model="followUp"[^>]*>/)?.[0] || ''
  assert.ok(input)
  assert.doesNotMatch(input, /disabled|readonly/)
  const continuation = componentSource.match(/async function continueSession\(\) \{[\s\S]*?\n}/)?.[0] || ''
  assert.match(continuation, /if \(!value \|\| isGenerating\.value \|\| !activeSession\.value\) return/)
  assert.match(continuation, /lastUserQuery\.value = value\s*\n\s*persistPendingQuestion\(value, target\)\s*\n\s*followUp\.value = ''/)
  const start = componentSource.match(/async function startSession\(\) \{[\s\S]*?\n}/)?.[0] || ''
  assert.match(start, /persistPendingQuestion\(value\)\s*\n\s*question\.value = ''\s*\n\s*followUp\.value = ''/)
  assert.doesNotMatch(start.slice(start.indexOf('try {')), /followUp\.value\s*=/)
})

test('chat page materializes pending sessions as soon as the backend creates them', () => {
  assert.match(componentSource, /function materializePendingSession\(pendingSession: ChatSession, createdSession: ChatSession\)/)
  assert.match(componentSource, /onSessionCreated: createdSession => materializePendingSession\(session, createdSession\)/)
})

test('chat page persists the pending question before clearing both composers', () => {
  assert.match(componentSource, /persistPendingQuestion\(value\)\s*\n\s*question\.value = ''\s*\n\s*followUp\.value = ''/)
  assert.match(componentSource, /void router\.replace\(\{ path: '\/platform\/ai-chat', query: \{ session: createdSession\.id \} \}\)/)
  assert.match(componentSource, /restorePendingChat\(\)/)
})

test('both chat composers hide attachments and place the custom microphone before send', () => {
  const template = componentSource.split('<script')[0]
  assert.doesNotMatch(template, /添加附件|name="attach"|tool-label="自动路由"/)
  assert.equal((template.match(/<VideohubAgentPicker appearance="tool" \/>/g) || []).length, 2)
  const actions = template.match(/<div class="chat-actions">[\s\S]*?<\/div>/g) || []
  assert.equal(actions.length, 2)
  for (const action of actions) {
    assert.ok(action.indexOf('class="chat-voice"') < action.indexOf('class="chat-send"'))
    assert.match(action, /<t-tooltip content="语音输入"[\s\S]*?class="chat-voice"[^>]*type="button"[^>]*aria-label="语音输入"[\s\S]*?<svg class="chat-voice__icon" viewBox="0 0 1024 1024"/)
  }
})

test('chat page clears the recovery pointer after a routed session is loaded', () => {
  assert.match(componentSource, /if \(sessionQueryId\.value\) \{[\s\S]*?openSessionFromRoute\(sessionQueryId\.value\)[\s\S]*?clearChatRecovery\(recoveryStorage, CHAT_PAGE_RECOVERY_KEY\)/)
  assert.match(componentSource, /pending\?\.sessionId === sessionQueryId\.value/)
})

test('chat page puts the pending question back when session recovery fails', () => {
  assert.match(componentSource, /async function restorePendingChat\(\) \{[\s\S]*?try \{[\s\S]*?loadChatSession\(target\)[\s\S]*?\} catch \{[\s\S]*?question\.value = pending\.question/)
})

test('chat page supplements a routed session when history still misses the pending question', () => {
  assert.match(componentSource, /pending\?\.sessionId === sessionQueryId\.value && activeSession\.value\?\.id === sessionQueryId\.value/)
  assert.match(componentSource, /const recovered = appendRecoveredQuestion\(activeSession\.value, pending\.question\)/)
})
