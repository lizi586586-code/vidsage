<template>
  <div class="assistant-shell">
    <div class="assistant-frame" :class="{ 'assistant-frame--expanded': expanded }">
      <section v-if="expanded" class="assistant-drawer">
        <header>
          <div>
            <strong>AI Assistant</strong>
            <span>{{ globalMode ? '全局视频问答' : `围绕《${currentVideo.title}》提问` }}</span>
          </div>
          <t-button variant="text" shape="square" aria-label="收起" @click="expanded = false">
            <t-icon name="chevron-down" />
          </t-button>
        </header>
        <div ref="messageArea" class="assistant-messages">
          <div v-for="(message, messageIndex) in messages" :key="message.id" :class="['assistant-message', `assistant-message--${message.sender}`]">
            <AgentStreamDisplay
              v-if="shouldUseNativeAgentDisplay(message)"
              :session="toNativeAgentSession(message)"
              :session-id="activeSession?.id"
              :user-query="questionForMessage(messageIndex) || lastUserQuery"
              :show-video-title="shouldShowVideoTitle(message, messageIndex)"
              :hydrate-protected-images="false"
              @click="handleRenderedAnswerClick"
              @video-navigate="handleVideoNavigate"
            />
            <div v-else-if="message.sender === 'assistant' && message.text" class="assistant-rendered-answer markdown-content" v-html="renderAssistantAnswer(message.text, message.knowledge_references, questionForMessage(messageIndex))" @click="handleRenderedAnswerClick" @keydown="handleRenderedAnswerKeydown"></div>
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
            :disabled="isGenerating"
            :placeholder="globalMode ? '向 AI 提问知识库全部视频内容' : '向 AI 提问当前视频内容'"
            @focus="expanded = true"
            @keydown.enter.exact.prevent="send(input)"
          />
          <div class="assistant-composer__tools">
            <t-button class="chat-tool" variant="text" type="button" aria-label="添加附件" @click="showToolMessage('添加附件')">
              <t-icon name="attach" /><span>添加附件</span>
            </t-button>
            <t-button class="chat-tool" variant="text" type="button" aria-label="语音输入" @click="showToolMessage('语音输入')">
              <t-icon name="microphone" /><span>语音输入</span>
            </t-button>
            <VideohubAgentPicker appearance="tool" tool-label="自动路由" />
          </div>
        </div>
        <t-button class="chat-send" type="submit" shape="square" :disabled="isGenerating || !input.trim()">
          <t-icon name="arrow-up" />
        </t-button>
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
import { createChatTurn } from '@/api/videohub/chat'
import type { StreamingChatMessage } from '@/api/videohub/chat'
import type { ChatKnowledgeReference, ChatMessage, ChatSession, VideoData } from '@/types/videohub'
import AgentStreamDisplay from '@/views/chat/components/AgentStreamDisplay.vue'
import VideohubAgentPicker from '@/components/videohub/VideohubAgentPicker.vue'
import { useSettingsStore } from '@/stores/settings'
import { sanitizeMarkdownHTML } from '@/utils/security'
import { configureMarkedForChatMarkdown, renderChatMarkdown } from '@/utils/chatMarkdownRenderer'
import { shouldShowVideoCitationTitle } from '@/utils/citationMarkdown'
import { shouldAutoRoute } from '@/api/videohub/chatRequest'

type TimestampPart = { text: string; seconds?: number; videoId?: string }

const props = withDefaults(defineProps<{ currentVideo: VideoData; currentTime?: number; externalQuery?: string; globalMode?: boolean }>(), { currentTime: 0, externalQuery: '', globalMode: false })
const emit = defineEmits<{ seek: [seconds: number]; navigate: [videoId: string, seconds: number] }>()
const settingsStore = useSettingsStore()
const expanded = ref(false), input = ref(''), isGenerating = ref(false), messageArea = ref<HTMLElement | null>(null)
const messages = ref<StreamingChatMessage[]>([])
const activeSession = ref<ChatSession | null>(null)
const lastUserQuery = ref('')
const globalSuggestions = ['帮我总结一下全部视频的核心观点', '最近上传了哪些重要视频？', '帮我找关于培训内容的视频']
const singleSuggestions = ['总结这段视频的核心观点', '有哪些值得记录的知识点？', '给出三个可执行建议']
const suggestions = props.globalMode ? globalSuggestions : singleSuggestions
let consumedExternalQuery = ''
const streamingAssistantId = ref('')

const streamingAssistantVisible = computed(() => messages.value.some(message =>
  message.id === streamingAssistantId.value && Boolean(message.text || message.thinkingText || message.activityText),
))

const answerRenderer = new marked.Renderer()
configureMarkedForChatMarkdown()

const sessionCacheKey = computed(() => props.globalMode ? 'global' : `video:${props.currentVideo.id}`)

function welcome(video: VideoData, global: boolean): StreamingChatMessage {
  return {
    id: `welcome-${video.id}`,
    sender: 'assistant',
    text: global ? '你好，我可以基于知识库全部视频回答问题，回答中会标注来自哪条视频的哪个时间点。' : `你好，我可以基于《${video.title}》回答问题。`,
    timestamp: ''
  }
}
messages.value = [welcome(props.currentVideo, props.globalMode)]
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
  }
}

function questionForMessage(index: number): string {
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    const message = messages.value[cursor]
    if (message?.sender === 'user') return message.text
  }
  return props.externalQuery || ''
}

function shouldShowVideoTitle(message: StreamingChatMessage, index: number): boolean {
  if (message.sender !== 'assistant' || !props.globalMode) return false
  return shouldShowVideoCitationTitle(questionForMessage(index), message.knowledge_references)
}

function renderAssistantAnswer(text: string, references: ChatKnowledgeReference[] = [], question = '') {
  const html = renderChatMarkdown(text, {
    renderer: answerRenderer,
    escapeMarkdown: markdown => markdown,
    sanitizeHtml: sanitizeMarkdownHTML,
    streaming: false,
    knowledgeReferences: references,
    showVideoTitle: props.globalMode && shouldShowVideoCitationTitle(question, references),
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
  activeSession.value = cached || null
  messages.value = cached ? [welcome(props.currentVideo, props.globalMode), ...cached.messages] : [welcome(props.currentVideo, props.globalMode)]
}

function cacheSession(session: ChatSession) {
  assistantSessionCache.set(sessionCacheKey.value, session)
}

function materializeActiveSession(session: ChatSession) {
  const currentMessages = messages.value.filter(message => !message.id.startsWith('welcome-'))
  activeSession.value = { ...session, messages: currentMessages }
  cacheSession(activeSession.value)
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
async function send(value: string) {
  const question = value.trim(); if (!question || isGenerating.value) return
  lastUserQuery.value = question
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
    })
    activeSession.value = session
    cacheSession(session)
    messages.value = [welcome(props.currentVideo, props.globalMode), ...session.messages]
  }
  catch (error) {
    const text = error instanceof Error ? error.message : '问答生成失败，请稍后重试'
    messages.value.push({ id: `error-${Date.now()}`, sender: 'assistant', text, timestamp: '' })
    MessagePlugin.error(text)
  } finally { isGenerating.value = false; streamingAssistantId.value = ''; await scrollBottom() }
}
watch([() => props.currentVideo.id, () => props.globalMode], () => { input.value = ''; isGenerating.value = false; streamingAssistantId.value = ''; restoreCachedSession() })
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
  width: min(608px, calc(100% - 32px));
  margin: 0 auto 16px;
  box-sizing: border-box;
  overflow: hidden;
  border: 1px solid rgba(255, 255, 255, .84);
  border-radius: var(--td-radius-extraLarge);
  background: rgba(255, 255, 255, .62);
  box-shadow: 0 12px 32px rgba(27, 37, 31, .08);
  backdrop-filter: blur(24px) saturate(112%);
  -webkit-backdrop-filter: blur(24px) saturate(112%);
  pointer-events: auto;
}

.assistant-frame:focus-within {
  border-color: color-mix(in srgb, var(--td-brand-color) 42%, transparent);
  box-shadow: 0 18px 44px rgba(27, 37, 31, .12), 0 0 0 3px color-mix(in srgb, var(--td-brand-color) 12%, transparent);
}

.assistant-drawer {
  width: 100%;
  box-sizing: border-box;
  overflow: hidden;
  max-height: 600px;
  border: 0;
  border-radius: 0;
  background: transparent;
  pointer-events: auto;
}

.assistant-drawer header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid rgba(0, 0, 0, .08);
}

.assistant-drawer header div {
  display: grid;
  gap: 2px;
}

.assistant-drawer header strong {
  color: var(--td-text-color-primary);
  font: var(--td-font-title-medium);
}

.assistant-drawer header span {
  color: var(--td-text-color-secondary);
  font: var(--td-font-body-small);
}

.assistant-messages {
  width: 100%;
  box-sizing: border-box;
  overflow-y: auto;
  max-height: 500px;
  padding: 20px 16px 14px;
  color: var(--td-text-color-primary);
  font: var(--td-font-body-medium);
  scrollbar-width: thin;
  scrollbar-color: color-mix(in srgb, var(--td-text-color-placeholder) 38%, transparent) transparent;
}

.assistant-messages::-webkit-scrollbar {
  width: 6px;
  height: 6px;
  border: 0;
  background: transparent;
}

.assistant-messages::-webkit-scrollbar-track,
.assistant-messages::-webkit-scrollbar-track-piece,
.assistant-messages::-webkit-scrollbar-corner {
  border: 0;
  outline: 0;
  background: transparent;
  box-shadow: none;
}

.assistant-messages::-webkit-scrollbar-thumb {
  min-height: 36px;
  border: 0;
  border-radius: var(--td-radius-round);
  background: color-mix(in srgb, var(--td-text-color-placeholder) 38%, transparent);
  box-shadow: none;
}

.assistant-messages::-webkit-scrollbar-thumb:hover {
  background: color-mix(in srgb, var(--td-text-color-secondary) 48%, transparent);
}

.assistant-message {
  display: flex;
  margin-bottom: 20px;
}

.assistant-message--user {
  justify-content: flex-end;
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
  background: rgba(255, 255, 255, .54);
  color: var(--td-text-color-primary);
  font: var(--td-font-body-medium);
  line-height: 1.7;
  white-space: pre-wrap;
  text-align: left;
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
  display: flex;
  gap: 6px;
  overflow-x: auto;
  padding: 0 16px 12px;
}

.assistant-suggestions button {
  padding: 6px 10px;
  border: 1px solid rgba(255, 255, 255, .84);
  border-radius: var(--td-radius-round);
  background: rgba(255, 255, 255, .48);
  color: var(--td-text-color-secondary);
  cursor: pointer;
  font: var(--td-font-body-small);
  white-space: nowrap;
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
}

.assistant-suggestions button:hover {
  border-color: color-mix(in srgb, var(--td-brand-color) 30%, transparent);
  background: rgba(255, 255, 255, .76);
  color: var(--td-brand-color);
}

.assistant-composer {
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
}

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

.chat-tool {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 8px;
  border-radius: var(--td-radius-medium);
  color: var(--td-text-color-secondary);
  font-size: 12px;
}

.chat-tool:hover {
  color: var(--td-text-color-primary);
  background: rgba(0, 0, 0, .05);
}

.chat-tool :deep(.t-icon) {
  font-size: 15px;
}

.chat-send {
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  border-radius: var(--td-radius-large);
  color: #fff;
  background: var(--td-brand-color);
  box-shadow: 0 6px 14px rgba(7, 192, 95, .24);
}

.chat-send:hover {
  background: var(--td-brand-color-active);
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

  .chat-tool span {
    display: none;
  }
}
</style>
