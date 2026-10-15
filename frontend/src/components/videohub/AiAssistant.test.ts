import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const componentSource = readFileSync(resolve(import.meta.dirname, 'AiAssistant.vue'), 'utf8')
const suggestionsSource = readFileSync(resolve(import.meta.dirname, 'ReviewSuggestions.vue'), 'utf8')
const replySource = readFileSync(resolve(import.meta.dirname, 'ReviewMaterialReply.vue'), 'utf8')
const courseSource = readFileSync(resolve(import.meta.dirname, '../../composables/useReviewCourse.ts'), 'utf8')
const reviewSource = readFileSync(resolve(import.meta.dirname, '../../api/videohub/reviewMaterials.ts'), 'utf8')

test('video assistant suggestion chips trigger chat as plain buttons', () => {
  assert.match(
    suggestionsSource,
    /<button\s+v-for="item in items"[^>]*type="button"[^>]*@click="\$emit\('select', item\)"/,
  )
})

test('video assistant keeps suggestions focus-scoped and auto-grows the composer up to five lines', () => {
  const template = componentSource.split('<script')[0]
  const composer = template.match(/<div[^>]*class="assistant-composer-area"[\s\S]*?<\/div>\s*<\/div>/)?.[0] || ''
  const textarea = template.match(/<textarea\s+ref="composerInput"[\s\S]*?\/>/)?.[0] || ''
  assert.match(composer, /ref="composerArea"/)
  assert.match(composer, /@focusin="handleComposerFocusin"/)
  assert.match(composer, /@focusout="handleComposerFocusout"/)
  assert.match(composer, /<ReviewSuggestions v-if="composerFocused"/)
  assert.match(textarea, /rows="1"/)
  assert.match(textarea, /@input="resizeComposerInput"/)
  assert.match(componentSource, /const composerMaxHeight = 124/)
  assert.match(componentSource, /Math\.min\(textarea\.scrollHeight, composerMaxHeight\)/)
  assert.match(componentSource, /textarea\.scrollHeight > composerMaxHeight \? 'auto' : 'hidden'/)
  assert.match(componentSource, /max-height:\s*124px/)
  const textareaStyle = componentSource.match(/\.assistant-composer textarea\s*\{([^}]*)}/)?.[1] || ''
  assert.match(textareaStyle, /font:\s*var\(--td-font-body-medium\)/)
  assert.doesNotMatch(textareaStyle, /(?:^|;)\s*(?:height|min-height):\s*36px/)
  const focusout = componentSource.match(/function handleComposerFocusout\(event: FocusEvent\)[\s\S]*?\n}/)?.[0] || ''
  assert.match(focusout, /area\.contains\(nextTarget\)/)
  assert.match(focusout, /nextTarget\.closest\('\.picker-dropdown'\)/)
  assert.match(focusout, /composerFocused\.value = false\s*resizeComposerInput\(\)/)
  const resize = componentSource.match(/function resizeComposerInput\(\)[\s\S]*?\n}/)?.[0] || ''
  assert.match(resize, /if \(!composerFocused\.value\)/)
  assert.match(resize, /textarea\.style\.height = ''/)
  assert.match(componentSource, /document\.addEventListener\('pointerdown', handleDocumentPointerdown, true\)/)
  assert.match(componentSource, /document\.removeEventListener\('pointerdown', handleDocumentPointerdown, true\)/)
  const outsidePointer = componentSource.match(/function handleDocumentPointerdown\(event: PointerEvent\)[\s\S]*?\n}/)?.[0] || ''
  assert.match(template, /ref="assistantFrame"/)
  assert.match(outsidePointer, /composerArea\.value\?\.contains\(target\)/)
  assert.match(outsidePointer, /target\.closest\('\.picker-dropdown'\)/)
  assert.match(outsidePointer, /composerFocused\.value = false\s*resizeComposerInput\(\)/)
  assert.match(outsidePointer, /if \(!assistantFrame\.value\?\.contains\(target\)\) expanded\.value = false/)
})

test('video assistant input unlocks after chat turn finalizes', () => {
  assert.match(componentSource, /finally\s*\{[^}]*isGenerating\.value\s*=\s*false/s)
  assert.match(componentSource, /finally\s*\{[^}]*streamingAssistantId\.value\s*=\s*''/s)
})

test('video assistant clears the sent draft while keeping the composer editable during generation', () => {
  const input = componentSource.match(/<textarea\b[^>]*v-model="input"[^>]*>/)?.[0] || ''
  assert.ok(input)
  assert.doesNotMatch(input, /disabled|readonly/)
  const send = componentSource.match(/async function send\(value: string, reinforcement = false\) \{[\s\S]*?\n}/)?.[0] || ''
  assert.match(send, /if \(!question \|\| isGenerating\.value\) return/)
  assert.ok(send.indexOf('isGenerating.value) return') < send.indexOf("input.value = ''"))
  assert.match(send, /expanded\.value = true;\s*input\.value = ''/)
  assert.doesNotMatch(send.slice(send.indexOf('try {')), /input\.value\s*=/)
})

test('video assistant hides attachments and places the custom microphone before send and pause', () => {
  const template = componentSource.split('<script')[0]
  assert.doesNotMatch(template, /添加附件|name="attach"|tool-label="自动路由"/)
  assert.match(template, /<VideohubAgentPicker appearance="tool" \/>/)
  assert.match(template, /class="chat-actions"[\s\S]*?<t-tooltip content="语音输入"[\s\S]*?class="chat-voice"[\s\S]*?<svg class="chat-voice__icon" viewBox="0 0 1024 1024"[\s\S]*?<t-tooltip v-if="isGenerating \|\| canPauseReview" content="暂停生成"[\s\S]*?class="chat-send chat-pause"/)
  assert.match(template, /class="chat-actions"[\s\S]*?<t-tooltip content="语音输入"[\s\S]*?class="chat-voice"[\s\S]*?<svg class="chat-voice__icon" viewBox="0 0 1024 1024"[\s\S]*?class="chat-send"[^>]*type="submit"/)
  assert.match(template, /class="assistant-composer__controls"[\s\S]*?<VideohubAgentPicker appearance="tool" \/>[\s\S]*?class="chat-actions"/)
  assert.match(componentSource, /\.assistant-composer\s*\{[^}]*grid-template-columns:\s*minmax\(0, 1fr\) auto/s)
  assert.match(componentSource, /\.chat-actions\s*\{[^}]*display:\s*flex[^}]*gap:\s*4px/s)
  assert.match(componentSource, /\.chat-send,\s*\.chat-voice\s*\{[^}]*width:\s*28px[^}]*height:\s*28px[^}]*flex:\s*0 0 28px[^}]*border:\s*0/s)
  assert.match(componentSource, /\.chat-voice\s*\{[^}]*background:\s*transparent[^}]*box-shadow:\s*none/s)
  assert.match(componentSource, /\.chat-actions :deep\(svg\)\s*\{[^}]*width:\s*16px[^}]*height:\s*16px/s)
  assert.match(componentSource, /\.chat-send:not\(:disabled\):hover,\s*\.chat-send:not\(:disabled\):focus-visible\s*\{[^}]*border:\s*0[^}]*background:\s*var\(--td-brand-color-active\)[^}]*box-shadow:[^}]*transform:\s*translateY\(-1px\)/s)
  assert.match(componentSource, /\.chat-voice:hover,\s*\.chat-voice:focus-visible\s*\{[^}]*border:\s*0[^}]*background:\s*var\(--td-brand-color-light\)[^}]*box-shadow:[^}]*color:\s*var\(--td-brand-color\)[^}]*transform:\s*translateY\(-1px\)/s)
})

test('collapsing the video assistant also restores the composer to one line', () => {
  const template = componentSource.split('<script')[0]
  assert.match(template, /class="assistant-collapse"[^>]*aria-label="收起"[^>]*@pointerdown\.prevent\.stop="collapseAssistant"[^>]*@click="collapseAssistant"/)
  const collapse = componentSource.match(/function collapseAssistant\(\)[\s\S]*?\n}/)?.[0] || ''
  assert.match(collapse, /expanded\.value = false/)
  assert.match(collapse, /composerFocused\.value = false/)
  assert.match(collapse, /composerInput\.value\?\.blur\(\)/)
  assert.match(collapse, /resizeComposerInput\(\)/)
  const outsidePointer = componentSource.match(/function handleDocumentPointerdown\(event: PointerEvent\)[\s\S]*?\n}/)?.[0] || ''
  assert.match(outsidePointer, /target\.closest\('\.assistant-collapse'\)\) return/)
  assert.ok(outsidePointer.indexOf("target.closest('.assistant-collapse')") < outsidePointer.indexOf('composerFocused.value = false'))
})

test('video assistant swaps the send action for an icon-only pause control while generating', () => {
  assert.match(componentSource, /<t-tooltip v-if="isGenerating \|\| canPauseReview" content="暂停生成"/)
  assert.match(componentSource, /aria-label="暂停生成" @click="pauseGeneration"/)
  assert.match(componentSource, /<t-icon name="pause-circle" \/>/)
  assert.match(componentSource, /onStopReady: stop => \{ if \(isCurrentScope\(\) && !pauseRequested\) stopCurrentTurn\.value = stop/)
})

test('video assistant exposes a knowledge reinforcement action that routes through the classroom skill', () => {
  const template = componentSource.split('<script')[0]
  assert.doesNotMatch(template, /chat-reinforce/)
  assert.match(componentSource, /const singleSuggestions = \['巩固学习', '总结这段视频的核心观点', '给出三个可执行建议'\]/)
  assert.doesNotMatch(componentSource, /有哪些值得记录的知识点/)
  assert.match(reviewSource, /请使用 openmaic-classroom Skill/)
  assert.match(reviewSource, /互动问答/)
  assert.match(reviewSource, /互动白板/)
  assert.match(componentSource, /async function startKnowledgeReinforcement\(\)/)
  assert.match(courseSource, /prepareOpenMAICClassroom\(\{ video_id: video.id/)
  assert.match(componentSource, /skillNames:\s*reviewTurn \? \['openmaic-classroom'\]/)
  assert.match(template, /<ReviewMaterialReply/)
  assert.doesNotMatch(componentSource, /OPENMAIC_CLASSROOM_RESULT|parseClassroomResult/)
  assert.match(replySource, /setTimeout\(\(\) => void refresh\(turn\), 5000\)/)
  assert.match(replySource, /:is="presentation.href \? 'a' : 'div'"/)
  assert.match(replySource, /:target="presentation.href \? '_blank' : undefined"/)
  assert.match(template, /visibleQuestion\(message.text\)/)
  assert.match(template, /!isReinforcementReply\(message, messageIndex\)/)
  assert.match(reviewSource, /按照课程材料，课程要求以及你的学习偏好，为你定制复习材料/)
  assert.match(reviewSource, /复习材料生成中，进度\$\{progress\}%/)
  assert.match(replySource, /animation: review-shimmer/)
  assert.match(replySource, /fetchOpenMAICClassroomStatus/)
})

test('video assistant keeps a restored review task ahead of later chat turns', () => {
  assert.match(componentSource, /saveReviewTaskAnchor\(recoveryStorage,[\s\S]*?messages\.value\.length\)/)
  assert.match(componentSource, /const reviewFallbackInsertIndex = computed\([\s\S]*?reviewTaskFallbackIndex\(messages\.value, requestId, anchor\)/)
  assert.match(componentSource, /v-if="reviewFallbackInsertIndex === messageIndex \+ 1" class="assistant-review-reply"/)
})

test('video assistant disables protected image hydration in native agent display', () => {
  assert.match(componentSource, /:hydrate-protected-images="false"/)
})

test('video assistant wraps learning quotations and reduces right-side whitespace', () => {
  assert.match(componentSource, /:show-request-info="false"/)
  assert.match(componentSource, /assistant-message--welcome/)
  assert.match(componentSource, /padding:\s*4px 8px 84px 16px/)
  assert.match(componentSource, /\.assistant-message--welcome \.assistant-rendered-answer\s*\{[^}]*white-space:\s*normal[^}]*overflow-wrap:\s*anywhere/s)
  assert.match(componentSource, /text: welcomeQuote\.value/)
  assert.doesNotMatch(componentSource, /你好，很高兴陪你一起/)
})

test('video assistant rotates only on entry and keeps quotations outside saved conversations', () => {
  assert.match(componentSource, /watch\(expanded, \(open, wasOpen\) => \{\s*if \(open && !wasOpen\) rotateWelcomeQuote\(\)/)
  assert.match(componentSource, /if \(expanded\.value\) rotateWelcomeQuote\(\)/)
  const rotation = componentSource.match(/function rotateWelcomeQuote\(\)[\s\S]*?\n}/)?.[0] || ''
  assert.match(rotation, /if \(message\.id\.startsWith\('welcome-'\)\) message\.text = welcomeQuote\.value/)
  assert.doesNotMatch(rotation, /messages\.value\s*=/)
  assert.match(componentSource, /messages\.value\.filter\(message => !message\.id\.startsWith\('welcome-'\)\)/)
  const welcome = componentSource.match(/function welcome\([\s\S]*?\n}/)?.[0] || ''
  assert.doesNotMatch(welcome, /nextLearningQuote/)
})

test('video assistant gives welcoming text and compact suggestion buttons without clipping', () => {
  const welcomeStyle = componentSource.match(/\.assistant-message--welcome \.assistant-rendered-answer\s*\{([^}]*)}/)?.[1] || ''
  assert.match(welcomeStyle, /display:\s*flex/)
  assert.match(welcomeStyle, /align-items:\s*flex-start/)
  assert.match(welcomeStyle, /min-height:\s*80px/)
  assert.doesNotMatch(welcomeStyle, /(?:^|;)\s*height:|overflow:\s*hidden/)
  const buttonStyle = suggestionsSource.match(/\.review-suggestions button\s*\{([^}]*)}/)?.[1] || ''
  assert.match(buttonStyle, /min-height:\s*20px/)
  assert.match(buttonStyle, /box-sizing:\s*border-box/)
  assert.match(buttonStyle, /flex:\s*0 0 auto/)
  assert.match(buttonStyle, /align-items:\s*center/)
})

test('video assistant supports keyboard activation for rendered video citations', () => {
  assert.match(componentSource, /@keydown="handleRenderedAnswerKeydown"/)
  assert.match(componentSource, /function handleRenderedAnswerKeydown\(event: KeyboardEvent\)/)
  assert.match(componentSource, /target\.closest\?\.\('\.video-citation'\)/)
  assert.match(componentSource, /videoCitation\.tagName === 'BUTTON'/)
  assert.match(componentSource, /event\.preventDefault\(\)/)
})

test('video assistant supports hovering an already sent question to edit and resend it', () => {
  assert.match(componentSource, /assistant-user-content__text--editable/)
  assert.match(componentSource, /@click="canEditMessage\(message, messageIndex\) && startMessageEdit\(message\)"/)
  assert.doesNotMatch(componentSource, /name="edit"|> 编辑</)
  assert.match(componentSource, /@submit\.prevent="resendEditedMessage"/)
  assert.match(componentSource, /async function resendEditedMessage\(\)[\s\S]*?await send\(value\)/)
})

test('video assistant keeps sent questions as text highlights without hover blocks', () => {
  assert.match(componentSource, /\.assistant-user-content__text--editable:hover,[\s\S]*?background:\s*transparent[\s\S]*?color:\s*var\(--td-brand-color\)/)
  assert.match(componentSource, /\.assistant-messages\s*\{[^}]*padding:\s*4px 8px 84px 16px/s)
})

test('sent video questions use borderless gray bubbles up to 90% of the conversation width', () => {
  const wrapper = componentSource.match(/\.assistant-user-content\s*\{([^}]*)}/)?.[1] || ''
  assert.match(wrapper, /display:\s*flex/)
  assert.match(wrapper, /align-items:\s*flex-end/)
  assert.match(wrapper, /width:\s*100%/)
  const bubble = componentSource.match(/\.assistant-user-content > p\s*\{([^}]*)}/)?.[1] || ''
  assert.match(bubble, /box-sizing:\s*border-box/)
  assert.match(bubble, /max-width:\s*90%/)
  assert.match(bubble, /border:\s*0/)
  assert.match(bubble, /background:\s*var\(--td-bg-color-secondarycontainer\)/)
  assert.match(bubble, /overflow-wrap:\s*anywhere/)
})

test('video citations use the shared navigation path for current and cross-video targets', () => {
  const handler = componentSource.match(/function handleVideoNavigate[\s\S]*?\n}/)?.[0] || ''
  assert.match(handler, /emit\('navigate', videoId, seconds\)/)
  assert.doesNotMatch(handler, /currentVideo|emit\('seek'/)
})

test('video assistant keeps the created session before streaming finishes', () => {
  assert.match(componentSource, /function materializeActiveSession\(session: ChatSession\)/)
  assert.match(componentSource, /onSessionCreated: session => \{ if \(isCurrentScope\(\)\) materializeActiveSession\(session\)/)
})

test('video assistant persists and restores the current turn across a hard refresh', () => {
  assert.match(componentSource, /writeChatRecovery\(recoveryStorage, recoveryKey, \{/)
  assert.match(componentSource, /void restorePersistedSession\(pending\)/)
  assert.match(componentSource, /clearChatRecovery\(recoveryStorage, recoveryKey\)/)
})

test('video assistant routes reinforcement through the Vidsage reasoning agent', () => {
  assert.match(componentSource, /const reviewTurn = reinforcement \|\| isReviewIntent\(question\)/)
  assert.match(componentSource, /agentId:\s*reviewTurn \? VIDSAGE_REASONING_AGENT_ID/)
  assert.match(componentSource, /agentEnabled:\s*reviewTurn \|\| settingsStore\.isAgentEnabled/)
  assert.match(componentSource, /autoRoute:\s*reviewTurn \? false : shouldAutoRoute\(settingsStore\.selectedAgentId,\s*settingsStore\.settings\.selectedAgentExplicit\)/)
})

test('video assistant keeps long conversations scrollable inside the viewport', () => {
  assert.match(componentSource, /\.assistant-frame\s*\{[^}]*display:\s*flex[^}]*flex-direction:\s*column[^}]*max-height:\s*min\(720px,\s*calc\(100vh - 32px\)\)/s)
  assert.match(componentSource, /\.assistant-drawer\s*\{[^}]*display:\s*flex[^}]*flex:\s*1 1 auto[^}]*min-height:\s*0/s)
  assert.match(componentSource, /\.assistant-messages\s*\{[^}]*flex:\s*1 1 auto[^}]*min-height:\s*0[^}]*overflow-y:\s*auto[^}]*overscroll-behavior:\s*contain/s)
})

test('video assistant uses solid white surfaces instead of frosted backgrounds', () => {
  assert.match(componentSource, /\.assistant-frame\s*\{[^}]*background:\s*var\(--td-bg-color-container\)/s)
  assert.doesNotMatch(componentSource, /\.assistant-frame\s*\{[^}]*backdrop-filter:/s)
  assert.match(suggestionsSource, /\.review-suggestions button\s*\{[^}]*background:\s*var\(--td-bg-color-container\)/s)
})

test('video assistant expanded header shows only the first question without source labels', () => {
  assert.doesNotMatch(componentSource, /<strong>AI Assistant<\/strong>/)
  assert.match(componentSource, /const firstQuestion = computed\(\(\) => visibleQuestion\(messages\.value\.find\(message => message\.sender === 'user'\)\?\.text \|\| ''\)\)/)
  assert.match(componentSource, /<span v-if="firstQuestion">\{\{ firstQuestion \}\}<\/span>/)
  assert.doesNotMatch(componentSource, /全局视频问答|围绕《\$\{currentVideo\.title\}》提问/)
  assert.match(componentSource, /\.assistant-drawer header \{[^}]*padding:\s*4px 8px/s)
})
