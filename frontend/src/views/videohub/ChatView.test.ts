import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const componentSource = readFileSync(resolve(import.meta.dirname, 'ChatView.vue'), 'utf8')
const chooserSource = readFileSync(resolve(import.meta.dirname, '../../components/videohub/ReviewCourseChooser.vue'), 'utf8')

test('conversation header displays only the session title without a subtitle', () => {
  assert.match(componentSource, /<header><div><strong>\{\{ activeSession\.title \}\}<\/strong><\/div><\/header>/)
  assert.doesNotMatch(componentSource, /<t-button[^>]*>.*返回/)
  assert.doesNotMatch(componentSource, /<header>.*?<span[^>]*>\{\{ activeSession\.videoTitle/s)
})

test('chat detail uses the compact video-detail review pill and a 14px chooser title', () => {
  assert.match(componentSource, /\.review-entry--conversation\s*\{[^}]*min-height:\s*28px[^}]*border-radius:\s*var\(--td-radius-round\)/s)
  const conversation = componentSource.split('<section v-else class="conversation">')[1].split('</section>')[0]
  assert.doesNotMatch(conversation, /ReviewLearningIcon|review-entry__icon/)
  assert.match(componentSource, /<strong>巩固知识<\/strong>/)
  assert.match(conversation, /class="review-entry review-entry--conversation"[^>]*@click="openReviewCourseInConversation\(\)"/)
  assert.doesNotMatch(conversation, /class="review-entry review-entry--conversation"[^>]*@click="openReviewCourse\(\)"/)
  const openInConversation = componentSource.match(/async function openReviewCourseInConversation\(\)[\s\S]*?\n}/)?.[0] || ''
  assert.match(openInConversation, /if \(!activeSession\.value \|\| isGenerating\.value \|\| preparingClassroom\.value\) return/)
  assert.match(openInConversation, /await askCourse\(\)/)
  assert.match(openInConversation, /await scrollBottom\(\)/)
  assert.doesNotMatch(openInConversation, /createReviewChatSession|router\.(?:push|replace)/)
  assert.match(componentSource, /\.review-entry--conversation:hover:not\(:disabled\)[^}]*border-color:\s*var\(--td-brand-color\)[^}]*box-shadow:\s*none/s)
  assert.match(chooserSource, /\.review-course-chooser__title\s*\{[^}]*font-size:\s*14px[^}]*line-height:\s*22px/s)
})

test('chat landing review card uses the requested compact spacing', () => {
  assert.match(componentSource, /\.review-entry\s*\{[^}]*margin-top:\s*16px[^}]*align-items:\s*center[^}]*gap:\s*14px[^}]*padding:\s*10px 12px[^}]*border-radius:\s*12px/s)
  assert.match(componentSource, /\.review-entry--conversation\s*\{[^}]*padding:\s*3px 10px[^}]*border-radius:\s*var\(--td-radius-round\)/s)
})

test('chat landing renders the shared course question before entering a conversation', () => {
  const landing = componentSource.split('<section v-else-if="!activeSession" class="chat-landing">')[1].split('</section>')[0]
  assert.match(landing, /<ReviewCourseChooser v-if="choosingCourse"[\s\S]*?@confirm="confirmCourse" @cancel="cancelCourse" @retry="loadCourses"/)
  assert.match(landing, /<button v-if="!choosingCourse" class="review-entry"/)
})

test('review instructions use the Vidsage reasoning agent without frontend course-name parsing', () => {
  assert.match(componentSource, /if \(needsReviewCourseChoice\(value\)\) \{ question\.value = ''; await openReviewCourse\(\); return \}/)
  assert.match(componentSource, /if \(needsReviewCourseChoice\(value\)\) \{ followUp\.value = ''; await askCourse\(\)/)
  assert.match(componentSource, /const reviewTurn = reinforcement \|\| isReviewIntent\(value\)/)
  assert.match(componentSource, /agentId: reviewTurn \? VIDSAGE_REASONING_AGENT_ID/)
  assert.match(componentSource, /autoRoute: reviewTurn \? false/)
  assert.doesNotMatch(componentSource, /findReviewCourseId|resolveReviewCourse/)
})

test('chat detail keeps a restored review task ahead of later chat turns', () => {
  assert.match(componentSource, /saveReviewTaskAnchor\(recoveryStorage,[\s\S]*?activeSession\.value\?\.messages\.length \|\| 0\)/)
  assert.match(componentSource, /const reviewFallbackInsertIndex = computed\([\s\S]*?reviewTaskFallbackIndex\(messages, requestId, anchor\)/)
  assert.match(componentSource, /v-if="reviewFallbackInsertIndex === messageIndex \+ 1" class="message message--assistant"/)
})

test('chat page disables protected image hydration in native agent display', () => {
  assert.match(componentSource, /:hydrate-protected-images="false"/)
})

test('chat page lets the conversation message list scroll inside the fixed chat viewport', () => {
  assert.match(componentSource, /\.chat-page\s*\{[^}]*padding:\s*16px 48px 20px/s)
  assert.match(componentSource, /\.conversation\s*\{[^}]*height:\s*100%[^}]*min-height:\s*0[^}]*overflow:\s*hidden[^}]*padding:\s*0/s)
  assert.match(componentSource, /\.conversation__messages\s*\{[^}]*min-height:\s*0[^}]*overflow-y:\s*auto[^}]*overscroll-behavior:\s*contain[^}]*padding:\s*12px 0 16px/s)
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

test('chat page uses the shared identity layout for ordinary and structured replies', () => {
  assert.match(componentSource, /\.message--assistant\s*\{[^}]*display:\s*block/s)
  assert.match(componentSource, /\.message--assistant \.message__bubble\s*\{[^}]*width:\s*100%/s)
  assert.match(componentSource, /message.sender === 'assistant' \? AssistantReplyShell : 'div'/)
  assert.match(componentSource, /<AssistantReplyShell :timestamp="reviewReplyTime\(activeSession.messages, messageIndex\)"/)
})

test('chat page wraps overflowing conversation content within the safe width', () => {
  assert.match(componentSource, /\.conversation__messages\s*\{[^}]*min-width:\s*0[^}]*overflow-x:\s*hidden/s)
  assert.match(componentSource, /\.message-user-content > p\s*\{[^}]*overflow-wrap:\s*anywhere[^}]*word-break:\s*break-word/s)
  assert.match(componentSource, /\.message--assistant :deep\(\.markdown-content\)[\s\S]*?overflow-wrap:\s*anywhere/s)
})

test('chat page keeps the follow-up composer inside the conversation width', () => {
  assert.match(componentSource, /\.conversation-composer\s*\{[^}]*width:\s*100%[^}]*min-width:\s*0/s)
  assert.match(componentSource, /\.conversation-composer textarea\s*\{[^}]*overflow-wrap:\s*anywhere/s)
  assert.match(componentSource, /\.conversation__messages\s*\{[^}]*padding:\s*12px 0 16px/s)
  assert.match(componentSource, /\.conversation-composer\s*\{[^}]*padding:\s*10px 12px/s)
  assert.match(componentSource, /\.conversation-composer\s*\{[^}]*box-shadow:\s*0 4px 14px rgba\(27,37,31,\.06\)/s)
  assert.match(componentSource, /\.conversation-composer:focus-within\s*\{[^}]*box-shadow:\s*0 4px 16px rgba\(27,37,31,\.08\),\s*0 0 0 3px/s)
  assert.doesNotMatch(componentSource, /\.conversation-composer:focus-within\s*\{[^}]*0 18px 44px/s)
})

test('chat page follow-up composer starts at one line and grows only to the five-line limit', () => {
  const template = componentSource.split('<script')[0]
  const textarea = template.match(/<textarea\s+ref="followUpInput"[\s\S]*?\/>/)?.[0] || ''
  assert.match(textarea, /rows="1"/)
  assert.match(textarea, /@input="resizeFollowUpInput"/)
  assert.match(componentSource, /const followUpMaxHeight = 124/)
  assert.match(componentSource, /Math\.min\(textarea\.scrollHeight, followUpMaxHeight\)/)
  assert.match(componentSource, /textarea\.scrollHeight > followUpMaxHeight \? 'auto' : 'hidden'/)
  assert.match(componentSource, /\.conversation-composer textarea\s*\{[^}]*max-height:\s*124px[^}]*overflow-y:\s*hidden/s)
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
  assert.match(componentSource, /<t-tooltip v-if="isGenerating \|\| canPauseReview" content="暂停生成"/)
  assert.match(componentSource, /aria-label="暂停生成" @click="pauseGeneration"/)
  assert.match(componentSource, /<t-icon name="pause-circle" \/>/)
  assert.match(componentSource, /onStopReady: stop => \{ if \(revision === turnRevision && !pauseRequested\) stopCurrentTurn\.value = stop/)
})

test('chat page clears submitted drafts while keeping the composer editable during generation', () => {
  const input = componentSource.match(/<textarea\b[^>]*v-model="followUp"[^>]*>/)?.[0] || ''
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
  assert.match(componentSource, /onSessionCreated: createdSession => \{ if \(revision === turnRevision\) materializePendingSession\(session, createdSession\)/)
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

test('chat inputs use the product default question placeholder and keep actions away from the border', () => {
  assert.equal((componentSource.match(/placeholder="请输入你的问题"/g) || []).length, 2)
  assert.match(componentSource, /\.conversation-composer\s*\{[^}]*padding:\s*10px 12px/s)
  assert.match(componentSource, /\.chat-composer\s*\{[^}]*grid-template-columns:\s*minmax\(0, 1fr\) auto/s)
  assert.match(componentSource, /\.conversation-composer\s*\{[^}]*grid-template-columns:\s*minmax\(0, 1fr\) max-content/s)
  assert.match(componentSource, /\.chat-composer__controls,\s*\.conversation-composer__controls\s*\{[^}]*display:\s*flex[^}]*align-self:\s*end/s)
  assert.match(componentSource, /\.chat-send,\s*\.chat-voice\s*\{[^}]*width:\s*28px[^}]*height:\s*28px[^}]*flex:\s*0 0 28px[^}]*border:\s*0/s)
  assert.match(componentSource, /\.chat-actions :deep\(svg\)\s*\{[^}]*width:\s*16px[^}]*height:\s*16px/s)
  assert.match(componentSource, /\.chat-send:not\(:disabled\):hover,\s*\.chat-send:not\(:disabled\):focus-visible\s*\{[^}]*border:\s*0[^}]*background:\s*var\(--td-brand-color-active\)[^}]*box-shadow:[^}]*transform:\s*translateY\(-1px\)/s)
  assert.match(componentSource, /\.chat-voice:hover,\s*\.chat-voice:focus-visible\s*\{[^}]*border:\s*0[^}]*background:\s*var\(--td-brand-color-light\)[^}]*box-shadow:[^}]*color:\s*var\(--td-brand-color\)[^}]*transform:\s*translateY\(-1px\)/s)
})

test('chat page clears the recovery pointer after a routed session is loaded', () => {
  assert.match(componentSource, /if \(sessionQueryId\.value\) \{[\s\S]*?openSessionFromRoute\(sessionQueryId\.value\)[\s\S]*?clearChatRecovery\(recoveryStorage, CHAT_PAGE_RECOVERY_KEY\)/)
  assert.match(componentSource, /pending\?\.sessionId === sessionQueryId\.value/)
})

test('chat page puts the pending question back when session recovery fails', () => {
  assert.match(componentSource, /async function restorePendingChat\(\) \{[\s\S]*?try \{[\s\S]*?loadChatSession\(target\)[\s\S]*?\} catch \{[\s\S]*?question\.value = reviewRequestId\(pending.question\) \? '' : pending\.question/)
})

test('chat page supplements a routed session when history still misses the pending question', () => {
  assert.match(componentSource, /pending\?\.sessionId === sessionQueryId\.value && activeSession\.value\?\.id === sessionQueryId\.value/)
  assert.match(componentSource, /const recovered = appendRecoveredQuestion\(activeSession\.value, pending\.question\)/)
})
