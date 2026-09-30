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

test('video assistant clears the sent draft while keeping the composer editable during generation', () => {
  const input = componentSource.match(/<textarea\s+v-model="input"[^>]*>/)?.[0] || ''
  assert.ok(input)
  assert.doesNotMatch(input, /disabled|readonly/)
  const send = componentSource.match(/async function send\(value: string\) \{[\s\S]*?\n}/)?.[0] || ''
  assert.match(send, /if \(!question \|\| isGenerating\.value\) return/)
  assert.ok(send.indexOf('isGenerating.value) return') < send.indexOf("input.value = ''"))
  assert.match(send, /expanded\.value = true;\s*input\.value = ''/)
  assert.doesNotMatch(send.slice(send.indexOf('try {')), /input\.value\s*=/)
})

test('video assistant hides attachments and places the custom microphone before send and pause', () => {
  const template = componentSource.split('<script')[0]
  assert.doesNotMatch(template, /添加附件|name="attach"|tool-label="自动路由"/)
  assert.match(template, /<VideohubAgentPicker appearance="tool" \/>/)
  assert.match(template, /class="chat-actions"[\s\S]*?<t-tooltip content="语音输入"[\s\S]*?class="chat-voice"[\s\S]*?<svg class="chat-voice__icon" viewBox="0 0 1024 1024"[\s\S]*?<t-tooltip v-if="isGenerating" content="暂停生成"[\s\S]*?class="chat-send chat-pause"/)
  assert.match(template, /class="chat-actions"[\s\S]*?<t-tooltip content="语音输入"[\s\S]*?class="chat-voice"[\s\S]*?<svg class="chat-voice__icon" viewBox="0 0 1024 1024"[\s\S]*?class="chat-send"[^>]*type="submit"/)
  assert.match(componentSource, /\.chat-actions\s*\{[^}]*display:\s*flex[^}]*gap:\s*8px/s)
  assert.match(componentSource, /\.chat-send,\s*\.chat-voice\s*\{[^}]*width:\s*36px[^}]*height:\s*36px/s)
  assert.match(componentSource, /\.chat-voice\s*\{[^}]*background:\s*transparent[^}]*box-shadow:\s*none/s)
  assert.match(componentSource, /\.chat-voice__icon\s*\{[^}]*width:\s*18px[^}]*height:\s*18px/s)
})

test('video assistant swaps the send action for an icon-only pause control while generating', () => {
  assert.match(componentSource, /<t-tooltip v-if="isGenerating" content="暂停生成"/)
  assert.match(componentSource, /aria-label="暂停生成" @click="pauseGeneration"/)
  assert.match(componentSource, /<t-icon name="pause-circle" \/>/)
  assert.match(componentSource, /onStopReady: stop => \{ stopCurrentTurn\.value = stop \}/)
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
  const buttonStyle = componentSource.match(/\.assistant-suggestions button\s*\{([^}]*)}/)?.[1] || ''
  assert.match(buttonStyle, /height:\s*20px/)
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
  assert.match(componentSource, /onSessionCreated: materializeActiveSession/)
})

test('video assistant persists and restores the current turn across a hard refresh', () => {
  assert.match(componentSource, /writeChatRecovery\(recoveryStorage, recoveryKey, \{/)
  assert.match(componentSource, /void restorePersistedSession\(pending\)/)
  assert.match(componentSource, /clearChatRecovery\(recoveryStorage, recoveryKey\)/)
})

test('video assistant delegates automatic routing to the shared routing rule', () => {
  assert.match(componentSource, /agentEnabled:\s*settingsStore\.isAgentEnabled,\s*autoRoute:\s*shouldAutoRoute\(settingsStore\.selectedAgentId,\s*settingsStore\.settings\.selectedAgentExplicit\)/)
})

test('video assistant keeps long conversations scrollable inside the viewport', () => {
  assert.match(componentSource, /\.assistant-frame\s*\{[^}]*display:\s*flex[^}]*flex-direction:\s*column[^}]*max-height:\s*min\(720px,\s*calc\(100vh - 32px\)\)/s)
  assert.match(componentSource, /\.assistant-drawer\s*\{[^}]*display:\s*flex[^}]*flex:\s*1 1 auto[^}]*min-height:\s*0/s)
  assert.match(componentSource, /\.assistant-messages\s*\{[^}]*flex:\s*1 1 auto[^}]*min-height:\s*0[^}]*overflow-y:\s*auto[^}]*overscroll-behavior:\s*contain/s)
})

test('video assistant uses solid white surfaces instead of frosted backgrounds', () => {
  assert.match(componentSource, /\.assistant-frame\s*\{[^}]*background:\s*var\(--td-bg-color-container\)/s)
  assert.doesNotMatch(componentSource, /\.assistant-frame\s*\{[^}]*backdrop-filter:/s)
  assert.match(componentSource, /\.assistant-suggestions button\s*\{[^}]*background:\s*var\(--td-bg-color-container\)/s)
})

test('video assistant expanded header shows only the first question without source labels', () => {
  assert.doesNotMatch(componentSource, /<strong>AI Assistant<\/strong>/)
  assert.match(componentSource, /const firstQuestion = computed\(\(\) => messages\.value\.find\(message => message\.sender === 'user'\)\?\.text \|\| ''\)/)
  assert.match(componentSource, /<span v-if="firstQuestion">\{\{ firstQuestion \}\}<\/span>/)
  assert.doesNotMatch(componentSource, /全局视频问答|围绕《\$\{currentVideo\.title\}》提问/)
  assert.match(componentSource, /\.assistant-drawer header \{[^}]*padding:\s*4px 8px/s)
})
