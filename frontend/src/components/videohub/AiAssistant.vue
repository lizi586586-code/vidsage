<template>
  <div class="assistant-shell">
    <div class="assistant-frame" :class="{ 'assistant-frame--expanded': expanded }">
      <section v-if="expanded" class="assistant-drawer">
        <header>
          <div>
            <span v-if="firstQuestion">{{ firstQuestion }}</span>
          </div>
          <t-button variant="text" shape="square" aria-label="收起" @click="expanded = false">
            <t-icon name="chevron-down" />
          </t-button>
        </header>
        <div ref="messageArea" class="assistant-messages">
          <div v-for="(message, messageIndex) in messages" :key="message.id" :class="['assistant-message', `assistant-message--${message.sender}`, { 'assistant-message--welcome': message.id.startsWith('welcome-') }]">
            <AgentStreamDisplay
              v-if="shouldUseNativeAgentDisplay(message)"
              :session="toNativeAgentSession(message)"
              :session-id="activeSession?.id"
              :user-query="questionForMessage(messageIndex) || lastUserQuery"
              :show-video-title="shouldShowVideoTitle(message, messageIndex)"
              :show-request-info="false"
              :hydrate-protected-images="false"
              @click="handleRenderedAnswerClick"
              @video-navigate="handleVideoNavigate"
            />
            <div v-else-if="message.sender === 'assistant' && message.text" class="assistant-rendered-answer markdown-content" v-html="renderAssistantAnswer(message.text, message.knowledge_references, questionForMessage(messageIndex))" @click="handleRenderedAnswerClick" @keydown="handleRenderedAnswerKeydown"></div>
            <template v-else-if="message.sender === 'user' && message.text">
              <form v-if="editingMessageId === message.id" class="assistant-message-edit" @submit.prevent="resendEditedMessage">
                <textarea v-model="editingMessageText" rows="3" aria-label="编辑已发送指令" @keydown.esc.prevent="cancelMessageEdit" />
                <div class="assistant-message-edit__actions">
                  <t-button size="small" variant="text" type="button" @click="cancelMessageEdit">取消</t-button>
                  <t-button size="small" variant="text" theme="primary" type="submit" :disabled="!editingMessageText.trim()">重新发送</t-button>
                </div>
              </form>
              <template v-else>
                <div class="assistant-user-content">
                <p
                  :class="{ 'assistant-user-content__text--editable': canEditMessage(message, messageIndex) }"
                  :role="canEditMessage(message, messageIndex) ? 'button' : undefined"
                  :tabindex="canEditMessage(message, messageIndex) ? 0 : undefined"
                  @click="canEditMessage(message, messageIndex) && startMessageEdit(message)"
                  @keydown.enter.prevent="canEditMessage(message, messageIndex) && startMessageEdit(message)"
                >{{ message.text }}</p>
                </div>
              </template>
            </template>
            <p v-else-if="message.text"><template v-for="(part, index) in splitTimestamps(message.text, message.evidenceLinks)" :key="index"><button v-if="part.seconds !== undefined" class="timestamp" type="button" @click="selectTimestamp(part)">{{ part.text }}</button><template v-else>{{ part.text }}</template></template></p>
            <small v-else-if="message.activityText" class="assistant-activity">{{ message.activityText }}</small>
          </div>
          <div v-if="isGenerating && !streamingAssistantVisible" class="assistant-loading"><t-loading size="small" /> 正在整合视频知识...</div>
        </div>
        <div class="assistant-suggestions"><button v-for="item in suggestions" :key="item" type="button" @click="send(item)">{{ item }}</button></div>
      </section>
      <form class="assistant-composer" @submit.prevent="send(input)">
        <div class="assistant-composer__body">
          <textarea
            v-model="input"
            rows="2"
            :placeholder="globalMode ? '向 AI 提问知识库全部视频内容' : '向 AI 提问当前视频内容'"
            @focus="expanded = true"
            @keydown.enter.exact.prevent="send(input)"
          />
          <div class="assistant-composer__tools">
            <VideohubAgentPicker appearance="tool" />
          </div>
        </div>
        <div class="chat-actions">
          <t-tooltip content="语音输入" placement="top">
            <t-button class="chat-voice" type="button" shape="square" aria-label="语音输入" @click="showToolMessage('语音输入')">
              <svg class="chat-voice__icon" viewBox="0 0 1024 1024" aria-hidden="true" focusable="false">
                <path d="M486.4 972.8v-128.9728A332.8 332.8 0 0 1 179.2 512a25.6 25.6 0 0 1 51.2 0 281.6 281.6 0 0 0 563.2 0 25.6 25.6 0 1 1 51.2 0 332.8 332.8 0 0 1-307.2 331.8272V972.8h153.6a25.6 25.6 0 1 1 0 51.2h-358.4a25.6 25.6 0 1 1 0-51.2h153.6zM512 51.2a153.6 153.6 0 0 0-153.6 153.6v307.2a153.6 153.6 0 0 0 307.2 0V204.8a153.6 153.6 0 0 0-153.6-153.6z m0-51.2a204.8 204.8 0 0 1 204.8 204.8v307.2a204.8 204.8 0 1 1-409.6 0V204.8a204.8 204.8 0 0 1 204.8-204.8z" fill="currentColor" />
              </svg>
            </t-button>
          </t-tooltip>
          <t-tooltip v-if="isGenerating" content="暂停生成" placement="top">
            <t-button class="chat-send chat-pause" type="button" shape="square" aria-label="暂停生成" @click="pauseGeneration">
              <t-icon name="pause-circle" />
            </t-button>
          </t-tooltip>
          <t-button v-else class="chat-send" type="submit" shape="square" aria-label="发送" :disabled="!input.trim()">
            <t-icon name="arrow-up" />
          </t-button>
        </div>
      </form>
    </div>
  </div>
</template>

<script lang="ts">
const assistantSessionCache = new Map<string, unknown>()
</script>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { marked } from 'marked'
import { MessagePlugin } from 'tdesign-vue-next'
import { createChatTurn, loadChatSession } from '@/api/videohub/chat'
import type { StreamingChatMessage } from '@/api/videohub/chat'
import type { ChatKnowledgeReference, ChatMessage, ChatSession, VideoData } from '@/types/videohub'
import AgentStreamDisplay from '@/views/chat/components/AgentStreamDisplay.vue'
import VideohubAgentPicker from '@/components/videohub/VideohubAgentPicker.vue'
import { useSettingsStore } from '@/stores/settings'
import { sanitizeMarkdownHTML } from '@/utils/security'
import { configureMarkedForChatMarkdown, renderChatMarkdown } from '@/utils/chatMarkdownRenderer'
import { shouldAutoRoute } from '@/api/videohub/chatRequest'
import { learningQuotes, nextLearningQuote } from '@/utils/learningQuotes'
import {
  assistantRecoveryKey,
  clearChatRecovery,
  getChatRecoveryStorage,
  readChatRecovery,
  writeChatRecovery,
} from '@/api/videohub/chatRecovery'

type TimestampPart = { text: string; seconds?: number; videoId?: string }

const props = withDefaults(defineProps<{ currentVideo: VideoData; currentTime?: number; externalQuery?: string; globalMode?: boolean }>(), { currentTime: 0, externalQuery: '', globalMode: false })
const emit = defineEmits<{ seek: [seconds: number]; navigate: [videoId: string, seconds: number] }>()
const settingsStore = useSettingsStore()
const expanded = ref(false), input = ref(''), isGenerating = ref(false), messageArea = ref<HTMLElement | null>(null)
const messages = ref<StreamingChatMessage[]>([])
const activeSession = ref<ChatSession | null>(null)
const lastUserQuery = ref('')
const editingMessageId = ref('')
const editingMessageText = ref('')
const globalSuggestions = ['帮我总结一下全部视频的核心观点', '最近上传了哪些重要视频？', '帮我找关于培训内容的视频']
const singleSuggestions = ['总结这段视频的核心观点', '有哪些值得记录的知识点？', '给出三个可执行建议']
const suggestions = props.globalMode ? globalSuggestions : singleSuggestions
let consumedExternalQuery = ''
const streamingAssistantId = ref('')
const stopCurrentTurn = ref<null | (() => Promise<void>)>(null)
const recoveryStorage = getChatRecoveryStorage()
const welcomeQuote = ref<string>(learningQuotes[0])

const streamingAssistantVisible = computed(() => messages.value.some(message =>
  message.id === streamingAssistantId.value && Boolean(message.text || message.thinkingText || message.activityText),
))
const firstQuestion = computed(() => messages.value.find(message => message.sender === 'user')?.text || '')

const answerRenderer = new marked.Renderer()
configureMarkedForChatMarkdown()

const sessionCacheKey = computed(() => props.globalMode ? 'global' : `video:${props.currentVideo.id}`)

function welcome(video: VideoData): StreamingChatMessage {
  return {
    id: `welcome-${video.id}`,
    sender: 'assistant',
    text: welcomeQuote.value,
    timestamp: ''
  }
}
function rotateWelcomeQuote() {
  welcomeQuote.value = nextLearningQuote()
  for (const message of messages.value) {
    if (message.id.startsWith('welcome-')) message.text = welcomeQuote.value
  }
}
watch(expanded, (open, wasOpen) => {
  if (open && !wasOpen) rotateWelcomeQuote()
}, { flush: 'sync' })
restoreCachedSession()
function splitTimestamps(text: string, evidenceLinks: ChatMessage['evidenceLinks'] = []): TimestampPart[] {
  return text.split(/(\[\d{2}:\d{2}(?:[–-]\d{2}:\d{2})?\])/g).filter(Boolean).map(part => {
    const match = part.match(/^\[(\d{2}:\d{2})(?:[–-](\d{2}:\d{2}))?\]$/)
    if (!match) return { text: part }
    const [startMinutes, startSeconds] = match[1].split(':').map(Number)
    const seconds = startMinutes * 60 + startSeconds
    const evidence = evidenceLinks.find(item => item.seconds === seconds || item.timestamp === part.slice(1, -1))
    return { text: part, seconds, videoId: evidence?.videoId }
  })
}

function shouldUseNativeAgentDisplay(message: StreamingChatMessage) {
  return message.sender === 'assistant' && Boolean(message.isAgentMode && message.agentEventStream?.length)
}

function toNativeAgentSession(message: StreamingChatMessage) {
  return {
    id: message.id,
    assistant_message_id: message.assistant_message_id || message.id,
    request_id: message.request_id || message.id,
    role: 'assistant',
    content: message.content || message.text,
    isAgentMode: true,
    is_completed: message.is_completed ?? false,
    agentEventStream: message.agentEventStream || [],
    knowledge_references: message.knowledge_references || [],
    debugRequest: message.debugRequest,
  }
}

function questionForMessage(index: number): string {
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    const message = messages.value[cursor]
    if (message?.sender === 'user') return message.text
  }
  return props.externalQuery || ''
}
function hasAnswerOutputAfter(index: number): boolean {
  const nextAssistant = messages.value.slice(index + 1).find(message => message.sender === 'assistant')
  if (!nextAssistant) return false
  if (nextAssistant.text?.trim() || nextAssistant.content?.trim()) return true
  return Boolean(nextAssistant.agentEventStream?.some(event => {
    const item = event as { type?: string; content?: string }
    return (item.type === 'answer' || item.type === 'complete') && Boolean(item.content?.trim())
  }))
}
function canEditMessage(message: StreamingChatMessage, index: number): boolean {
  return message.sender === 'user' && !isGenerating.value && !hasAnswerOutputAfter(index)
}

function shouldShowVideoTitle(message: StreamingChatMessage, index: number): boolean {
  return false
}

function renderAssistantAnswer(text: string, references: ChatKnowledgeReference[] = [], question = '') {
  const html = renderChatMarkdown(text, {
    renderer: answerRenderer,
    escapeMarkdown: markdown => markdown,
    sanitizeHtml: sanitizeMarkdownHTML,
    streaming: false,
    knowledgeReferences: references,
    showVideoTitle: false,
    hideUnavailableCitations: true,
  })
  return html.replace(/\[(\d{2}:\d{2}(?:[–-]\d{2}:\d{2})?)\]/g, '<button type="button" class="timestamp">[$1]</button>')
}

function handleRenderedAnswerClick(event: MouseEvent) {
  const target = event.target as HTMLElement
  const videoCitation = target.closest?.('.video-citation') as HTMLElement | null
  if (videoCitation) {
    const videoId = String(videoCitation.getAttribute('data-video-id') || '').trim()
    const seconds = Number(videoCitation.getAttribute('data-video-seconds'))
    if (videoId && Number.isFinite(seconds) && seconds >= 0) {
      handleVideoNavigate(videoId, seconds)
    }
    return
  }
  const timestamp = target.closest?.('.timestamp')
  if (!timestamp) return
  const text = timestamp.textContent || ''
  const [part] = splitTimestamps(text)
  selectTimestamp(part)
}

function handleRenderedAnswerKeydown(event: KeyboardEvent) {
  if (event.key !== 'Enter' && event.key !== ' ') return
  const target = event.target as HTMLElement
  const videoCitation = target.closest?.('.video-citation') as HTMLElement | null
  if (!videoCitation || videoCitation.tagName === 'BUTTON') return
  event.preventDefault()
  handleRenderedAnswerClick(event as unknown as MouseEvent)
}

function handleVideoNavigate(videoId: string, seconds: number) {
  if (!videoId) return
  emit('navigate', videoId, seconds)
}

function showToolMessage(label: string) {
  MessagePlugin.info(`${label}入口暂未接入`)
}

function restoreCachedSession() {
  const cached = assistantSessionCache.get(sessionCacheKey.value) as ChatSession | undefined
  if (cached) {
    activeSession.value = cached
    messages.value = [welcome(props.currentVideo), ...cached.messages]
    return
  }
  const pending = readChatRecovery(recoveryStorage, assistantRecoveryKey(props.globalMode ? 'global' : 'video', props.currentVideo.id))
  activeSession.value = null
  messages.value = [welcome(props.currentVideo)]
  if (!pending) return
  if (!pending.sessionId) {
    input.value = pending.question
    expanded.value = true
    return
  }
  void restorePersistedSession(pending)
}

function cacheSession(session: ChatSession) {
  assistantSessionCache.set(sessionCacheKey.value, session)
}

function materializeActiveSession(session: ChatSession) {
  const currentMessages = messages.value.filter(message => !message.id.startsWith('welcome-'))
  activeSession.value = { ...session, messages: currentMessages }
  cacheSession(activeSession.value)
  writeChatRecovery(recoveryStorage, assistantRecoveryKey(props.globalMode ? 'global' : 'video', props.currentVideo.id), {
    scope: props.globalMode ? 'global' : 'video',
    question: lastUserQuery.value,
    sessionId: session.id,
    videoId: props.globalMode ? undefined : props.currentVideo.id,
    videoTitle: props.globalMode ? undefined : props.currentVideo.title,
    title: session.title,
    updatedAt: Date.now(),
  })
}

function recoveredSessionFromPending(sessionId: string, pendingQuestion: string): ChatSession {
  return {
    id: sessionId,
    title: pendingQuestion.slice(0, 24),
    type: props.globalMode ? 'chat' : 'video',
    time: '刚刚',
    messages: [],
    scope: props.globalMode ? 'global' : 'video',
    videoId: props.globalMode ? undefined : props.currentVideo.id,
    videoTitle: props.globalMode ? undefined : props.currentVideo.title,
  }
}

async function restorePersistedSession(pending: NonNullable<ReturnType<typeof readChatRecovery>>) {
  const key = assistantRecoveryKey(props.globalMode ? 'global' : 'video', props.currentVideo.id)
  try {
    const loaded = await loadChatSession(recoveredSessionFromPending(pending.sessionId!, pending.question))
    if (!loaded.messages.some(message => message.sender === 'user' && message.text.trim() === pending.question.trim())) {
      loaded.messages = [
        ...loaded.messages,
        { id: `recovered-user-${Date.now()}`, sender: 'user', text: pending.question, timestamp: '刚刚' },
      ]
    }
    activeSession.value = loaded
    cacheSession(loaded)
    messages.value = [welcome(props.currentVideo), ...loaded.messages]
    clearChatRecovery(recoveryStorage, key)
    void scrollBottom()
  } catch {
    input.value = pending.question
    expanded.value = true
  }
}

function selectTimestamp(part: TimestampPart) {
  if (part.seconds === undefined) return
  if (part.videoId) {
    handleVideoNavigate(part.videoId, part.seconds)
    return
  }
  if (!props.globalMode) emit('seek', part.seconds)
}
async function scrollBottom() { await nextTick(); if (messageArea.value) messageArea.value.scrollTop = messageArea.value.scrollHeight }
function pauseGeneration() {
  const stop = stopCurrentTurn.value
  if (!stop) return
  stopCurrentTurn.value = null
  void stop()
}
function startMessageEdit(message: StreamingChatMessage) {
  if (isGenerating.value || message.sender !== 'user') return
  editingMessageId.value = message.id
  editingMessageText.value = message.text
}
function cancelMessageEdit() {
  editingMessageId.value = ''
  editingMessageText.value = ''
}
async function resendEditedMessage() {
  const value = editingMessageText.value.trim()
  if (!value || isGenerating.value) return
  cancelMessageEdit()
  await send(value)
}
async function send(value: string) {
  const question = value.trim(); if (!question || isGenerating.value) return
  lastUserQuery.value = question
  const recoveryKey = assistantRecoveryKey(props.globalMode ? 'global' : 'video', props.currentVideo.id)
  writeChatRecovery(recoveryStorage, recoveryKey, {
    scope: props.globalMode ? 'global' : 'video',
    question,
    videoId: props.globalMode ? undefined : props.currentVideo.id,
    videoTitle: props.globalMode ? undefined : props.currentVideo.title,
    updatedAt: Date.now(),
  })
  expanded.value = true; input.value = ''
  messages.value.push({ id: `user-${Date.now()}`, sender: 'user', text: question, timestamp: '' })
  const assistantId = `assistant-${Date.now()}`
  streamingAssistantId.value = assistantId
  messages.value.push({ id: assistantId, sender: 'assistant', text: '', timestamp: '' })
  isGenerating.value = true; await scrollBottom()
  try {
    const updateStreamingMessage = (message: StreamingChatMessage) => {
      const target = messages.value.find(item => item.id === assistantId)
      if (!target) return
      Object.assign(target, message, { id: assistantId })
      void scrollBottom()
    }
    const session = await createChatTurn(question, {
      currentVideo: props.currentVideo,
      currentTime: props.currentTime,
      globalMode: props.globalMode,
      agentId: settingsStore.selectedAgentId,
      agentEnabled: settingsStore.isAgentEnabled,
      autoRoute: shouldAutoRoute(settingsStore.selectedAgentId, settingsStore.settings.selectedAgentExplicit),
      agentSourceTenantId: settingsStore.selectedAgentSourceTenantId,
      session: activeSession.value || undefined,
      onSessionCreated: materializeActiveSession,
      onStreamMessage: updateStreamingMessage,
      onStopReady: stop => { stopCurrentTurn.value = stop },
    })
    activeSession.value = session
    cacheSession(session)
    messages.value = [welcome(props.currentVideo), ...session.messages]
    clearChatRecovery(recoveryStorage, recoveryKey)
  }
  catch (error) {
    const text = error instanceof Error ? error.message : '问答生成失败，请稍后重试'
    messages.value.push({ id: `error-${Date.now()}`, sender: 'assistant', text, timestamp: '' })
    MessagePlugin.error(text)
  } finally { isGenerating.value = false; streamingAssistantId.value = ''; stopCurrentTurn.value = null; await scrollBottom() }
}
watch([() => props.currentVideo.id, () => props.globalMode], () => {
  input.value = ''; isGenerating.value = false; streamingAssistantId.value = ''; stopCurrentTurn.value = null
  if (expanded.value) rotateWelcomeQuote()
  restoreCachedSession()
})
watch(() => props.externalQuery, value => { if (value && value !== consumedExternalQuery) { consumedExternalQuery = value; void send(value) } }, { immediate: true })
</script>

<style scoped>
.assistant-shell {
  position: fixed;
  z-index: 20;
  right: 0;
  bottom: 0;
  left: 260px;
  pointer-events: none;
}

.assistant-frame {
  display: flex;
  flex-direction: column;
  width: min(608px, calc(100% - 32px));
  max-height: min(720px, calc(100vh - 32px));
  margin: 0 auto 16px;
  box-sizing: border-box;
  overflow: hidden;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-extraLarge);
  background: var(--td-bg-color-container);
  box-shadow: 0 12px 32px rgba(27, 37, 31, .08);
  pointer-events: auto;
}

.assistant-frame:focus-within {
  border-color: color-mix(in srgb, var(--td-brand-color) 42%, transparent);
  box-shadow: 0 18px 44px rgba(27, 37, 31, .12), 0 0 0 3px color-mix(in srgb, var(--td-brand-color) 12%, transparent);
}

.assistant-drawer {
  display: flex;
  flex: 1 1 auto;
  flex-direction: column;
  width: 100%;
  min-height: 0;
  box-sizing: border-box;
  overflow: hidden;
  border: 0;
  border-radius: 0;
  background: transparent;
  pointer-events: auto;
}

.assistant-drawer header {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 4px 8px;
  border-bottom: 1px solid rgba(0, 0, 0, .08);
}

.assistant-drawer header div {
  display: grid;
  gap: 2px;
}

.assistant-drawer header span {
  display: block;
  max-width: calc(100% - 40px);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--td-text-color-secondary);
  font: var(--td-font-body-small);
  line-height: 20px;
}

.assistant-messages {
  flex: 1 1 auto;
  width: 100%;
  min-height: 0;
  box-sizing: border-box;
  overflow-y: auto;
  max-height: none;
  overscroll-behavior: contain;
  padding: 4px 8px 84px 16px;
  color: var(--td-text-color-primary);
  font: var(--td-font-body-medium);
  scrollbar-width: none;
  -webkit-overflow-scrolling: touch;
}

.assistant-messages::-webkit-scrollbar { display: none; width: 0; height: 0; }

.assistant-message {
  display: flex;
  margin-bottom: 20px;
}

.assistant-message--welcome {
  margin-bottom: 8px;
}

.assistant-message--welcome .assistant-rendered-answer {
  display: flex;
  align-items: flex-start;
  min-height: 80px;
  padding-top: 2px;
  white-space: normal;
  overflow-wrap: anywhere;
}

.assistant-message--welcome .assistant-rendered-answer :deep(p) {
  margin: 0;
  white-space: normal;
}

.assistant-message--user {
  justify-content: flex-end;
  align-items: flex-start;
  gap: 4px;
  margin-bottom: 24px;
}

.assistant-user-content {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  width: 100%;
  min-width: 0;
}

.assistant-message-edit {
  display: grid;
  gap: 8px;
  width: min(520px, 72vw);
}

.assistant-message-edit textarea {
  width: 100%;
  min-height: 72px;
  box-sizing: border-box;
  resize: vertical;
  padding: 10px 12px;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-medium);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-primary);
  font: var(--td-font-body-medium);
}

.assistant-message-edit__actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.assistant-message-edit__actions :deep(.t-button) {
  border: 0;
  background: transparent;
  color: var(--td-text-color-secondary);
}

.assistant-message-edit__actions :deep(.t-button:hover),
.assistant-message-edit__actions :deep(.t-button:focus-visible) {
  background: transparent;
  color: var(--td-brand-color);
}

.assistant-message-edit__actions :deep(.t-button--theme-primary) {
  color: var(--td-brand-color);
}

.assistant-message--assistant {
  display: block;
}

.assistant-message p {
  display: inline-block;
  max-width: 72%;
  margin: 0;
  padding: 11px 14px;
  border-radius: var(--td-radius-large);
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  color: var(--td-text-color-primary);
  font: var(--td-font-body-medium);
  line-height: 1.7;
  white-space: pre-wrap;
  text-align: left;
}

.assistant-user-content > p {
  box-sizing: border-box;
  max-width: 90%;
  border: 0;
  background: var(--td-bg-color-secondarycontainer);
  overflow-wrap: anywhere;
}

.assistant-user-content__text--editable {
  cursor: text;
}

.assistant-user-content__text--editable:hover,
.assistant-user-content__text--editable:focus-visible {
  background: transparent;
  color: var(--td-brand-color);
  outline: 0;
}

.assistant-message--assistant p {
  display: block;
  width: 100%;
  max-width: none;
  padding: 0;
  background: transparent;
}

.assistant-rendered-answer {
  width: 100%;
  max-width: none;
  color: var(--td-text-color-primary);
  font: var(--td-font-body-medium);
  line-height: 1.7;
  text-align: left;
}

.assistant-message--assistant :deep(.agent-stream-display) {
  display: block;
  width: 100%;
  max-width: 100%;
}

.assistant-message--assistant :deep(.answer-content.markdown-content) {
  display: block;
  width: 100%;
  max-width: 100%;
}

.assistant-message--assistant :deep(.t-image-viewer__trigger--hover:empty) {
  display: none;
}

.timestamp,
:deep(.timestamp) {
  padding: 1px 6px;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-round);
  background: var(--td-bg-color-container);
  color: var(--td-brand-color);
  cursor: pointer;
  font: inherit;
}

.timestamp:hover,
:deep(.timestamp:hover) {
  border-color: var(--td-brand-color);
}

.assistant-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--td-text-color-secondary);
  font: var(--td-font-body-medium);
}

.assistant-suggestions {
  flex: 0 0 auto;
  display: flex;
  gap: 6px;
  overflow-x: auto;
  padding: 0 16px 12px;
}

.assistant-suggestions button {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  box-sizing: border-box;
  height: 20px;
  min-height: 20px;
  padding: 0 10px;
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-round);
  background: var(--td-bg-color-container);
  color: var(--td-text-color-secondary);
  cursor: pointer;
  font: var(--td-font-body-small);
  white-space: nowrap;
}

.assistant-suggestions button:hover {
  border-color: color-mix(in srgb, var(--td-brand-color) 30%, transparent);
  background: var(--td-bg-color-container-hover);
  color: var(--td-brand-color);
}

.assistant-composer {
  flex: 0 0 auto;
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: end;
  width: 100%;
  box-sizing: border-box;
  padding: 10px 12px 10px 16px;
  border-top: 1px solid rgba(0, 0, 0, .08);
}

.assistant-composer__body {
  min-width: 0;
}

.assistant-composer textarea {
  display: block;
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  min-height: 56px;
  resize: none;
  padding: 4px 0 10px;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--td-text-color-primary);
  font: var(--td-font-body-medium);
  line-height: 1.6;
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.assistant-composer textarea::-webkit-scrollbar,
.assistant-message-edit textarea::-webkit-scrollbar { display: none; width: 0; height: 0; }

.assistant-composer textarea::placeholder {
  color: var(--td-text-color-placeholder);
}

.assistant-composer__tools {
  display: flex;
  align-items: center;
  gap: 3px;
  min-width: 0;
}

.assistant-composer__tools :deep(.videohub-agent-picker) {
  display: inline-flex;
  flex: 0 0 auto;
}

.chat-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.chat-send,
.chat-voice {
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  border-radius: var(--td-radius-large);
}

.chat-send {
  color: var(--td-text-color-anti);
  background: var(--td-brand-color);
  box-shadow: 0 6px 14px rgba(7, 192, 95, .24);
}

.chat-voice {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  color: var(--td-text-color-secondary);
  border: 1px solid var(--td-component-stroke);
  background: transparent;
  box-shadow: none;
}

.chat-voice__icon {
  width: 18px;
  height: 18px;
  display: block;
}

.chat-send:hover {
  background: var(--td-brand-color-active);
}

.chat-voice:hover {
  border-color: var(--td-component-stroke-hover);
  background: var(--td-bg-color-secondarycontainer);
  color: var(--td-text-color-primary);
}

.chat-send:disabled {
  opacity: .48;
  box-shadow: none;
}

.assistant-rendered-answer :deep(.video-citation) {
  display: inline-flex;
  align-items: baseline;
  gap: 3px;
  margin: 0 2px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--td-brand-color);
  cursor: pointer;
  font: inherit;
  text-decoration: underline;
  text-decoration-thickness: 1px;
  text-underline-offset: 3px;
}

.assistant-rendered-answer :deep(.video-citation:hover),
.assistant-rendered-answer :deep(.video-citation:focus-visible) {
  color: var(--td-brand-color-hover);
}

.assistant-rendered-answer :deep(.video-citation__time) {
  white-space: nowrap;
}

@media (max-width: 900px) {
  .assistant-shell {
    left: 0;
  }

  .assistant-frame {
    width: calc(100% - 24px);
  }
}

@media (max-width: 640px) {
  .assistant-frame {
    width: calc(100% - 20px);
  }

}
</style>
