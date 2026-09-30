<template>
  <main class="chat-page">
    <section v-if="conversationLoading" class="conversation conversation--loading" aria-busy="true" aria-label="正在加载对话">
      <header class="conversation__loading-header">
        <t-skeleton animation="gradient" :row-col="[{ width: '64px', height: '24px', type: 'rect' }, { width: '36%', height: '20px' }]" />
      </header>
      <div class="conversation__messages conversation__messages--loading">
        <div v-for="item in conversationSkeletonRows" :key="item.id" :class="['message-skeleton', `message-skeleton--${item.align}`]">
          <t-skeleton animation="gradient" :row-col="item.rows" />
        </div>
      </div>
      <div class="conversation-composer conversation-composer--loading">
        <t-skeleton animation="gradient" :row-col="[{ width: '42%', height: '16px' }, { width: '24%', height: '14px' }]" />
        <t-skeleton animation="gradient" :row-col="[{ width: '36px', height: '36px', type: 'circle' }]" />
      </div>
    </section>
    <section v-else-if="!activeSession" class="chat-landing">
      <div class="chat-landing__inner">
        <h1>Learn with Vidsage</h1>
        <form class="chat-composer" @submit.prevent="startSession">
          <textarea v-model="question" rows="4" placeholder="问问你的视频知识库，例如：帮我总结最近看过的 AI 提示词视频" @keydown.enter.exact.prevent="startSession" />
          <footer>
            <div class="chat-composer__tools">
              <VideohubAgentPicker appearance="tool" />
            </div>
            <div class="chat-actions">
              <t-tooltip content="语音输入" placement="top">
                <t-button class="chat-voice" type="button" shape="square" aria-label="语音输入" @click="showToolMessage('语音输入')">
                  <svg class="chat-voice__icon" viewBox="0 0 1024 1024" aria-hidden="true" focusable="false">
                    <path d="M486.4 972.8v-128.9728A332.8 332.8 0 0 1 179.2 512a25.6 25.6 0 0 1 51.2 0 281.6 281.6 0 0 0 563.2 0 25.6 25.6 0 1 1 51.2 0 332.8 332.8 0 0 1-307.2 331.8272V972.8h153.6a25.6 25.6 0 1 1 0 51.2h-358.4a25.6 25.6 0 1 1 0-51.2h153.6zM512 51.2a153.6 153.6 0 0 0-153.6 153.6v307.2a153.6 153.6 0 0 0 307.2 0V204.8a153.6 153.6 0 0 0-153.6-153.6z m0-51.2a204.8 204.8 0 0 1 204.8 204.8v307.2a204.8 204.8 0 1 1-409.6 0V204.8a204.8 204.8 0 0 1 204.8-204.8z" fill="currentColor" />
                  </svg>
                </t-button>
              </t-tooltip>
              <t-button class="chat-send" type="submit" shape="square" aria-label="发送" :disabled="!question.trim()">
                <t-icon name="arrow-up" />
              </t-button>
            </div>
          </footer>
        </form>
        <section class="suggestions" aria-label="推荐任务">
          <div class="suggestions__heading">
            <span class="suggestions__title"><span class="suggestions__spark">✦</span> 根据最近对话推荐</span>
            <span class="suggestions__note">点击即可开始</span>
          </div>
          <div class="suggestions__grid">
            <button
              v-for="task in suggestedTasks"
              :key="task.id"
              class="suggestion-card"
              type="button"
              @click="beginSuggestedSession(task.prompt)"
            >
              <span class="suggestion-card__icon">✦</span>
              <span class="suggestion-card__content">
                <span class="suggestion-card__text">{{ task.label }}</span>
                <span class="suggestion-card__meta">{{ task.meta }}</span>
              </span>
            </button>
          </div>
        </section>
      </div>
    </section>
    <section v-else class="conversation">
      <header><div><strong>{{ activeSession.title }}</strong></div></header>
      <div ref="messageArea" class="conversation__messages">
        <article v-for="(message, messageIndex) in activeSession.messages" :key="message.id" :class="['message', `message--${message.sender}`]">
          <div class="message__bubble">
            <AgentStreamDisplay
              v-if="message.sender === 'assistant'"
              :session="toNativeAgentSession(message)"
              :session-id="activeSession.id"
              :user-query="questionForMessage(messageIndex) || lastUserQuery"
              :show-video-title="shouldShowVideoTitle(messageIndex)"
              :show-request-info="false"
              :hydrate-protected-images="false"
              @video-navigate="navigateToEvidence"
            />
            <template v-else>
  <form v-if="editingMessageId === message.id" class="message-edit" @submit.prevent="resendEditedMessage">
    <textarea v-model="editingMessageText" rows="3" aria-label="编辑已发送指令" @keydown.esc.prevent="cancelMessageEdit" />
    <div class="message-edit__actions">
      <t-button size="small" variant="text" type="button" @click="cancelMessageEdit">取消</t-button>
      <t-button size="small" variant="text" theme="primary" type="submit" :disabled="!editingMessageText.trim()">重新发送</t-button>
    </div>
  </form>
  <div v-else class="message-user-content">
    <p
      :class="{ 'message-user-content__text--editable': canEditMessage(message, messageIndex) }"
      :role="canEditMessage(message, messageIndex) ? 'button' : undefined"
      :tabindex="canEditMessage(message, messageIndex) ? 0 : undefined"
      @click="canEditMessage(message, messageIndex) && startMessageEdit(message)"
      @keydown.enter.prevent="canEditMessage(message, messageIndex) && startMessageEdit(message)"
    >{{ message.text }}</p>
    <button
      v-if="canEditMessage(message, messageIndex)"
      class="message-user-content__edit"
      type="button"
      aria-label="编辑指令"
      title="编辑指令"
      @click="startMessageEdit(message)"
    >
      <svg class="message-user-content__edit-icon" viewBox="0 0 1024 1024" aria-hidden="true" focusable="false">
        <path d="M690.816 171.84l90.944 93.824a52.544 52.544 0 0 1-0.768 73.92l-409.6 405.888h-124.48a38.848 38.848 0 0 1-38.848-38.912V586.368l427.904-415.36a38.848 38.848 0 0 1 54.912 0.832zM267.52 611.52v74.496h79.36l387.456-383.872-71.872-74.112L267.392 611.52z m-29.824 259.328c-16.384 0-29.696-14.336-29.696-32s13.312-32 29.696-32h548.608c16.384 0 29.696 14.336 29.696 32s-13.312 32-29.696 32H237.696z" fill="currentColor" />
      </svg>
    </button>
  </div>
</template>
          </div>
        </article>
        <div v-if="isGenerating && !streamingAssistantVisible" class="generating"><t-loading size="small" /> AI 正在整合视频知识回答中...</div>
      </div>
      <form class="conversation-composer" @submit.prevent="continueSession">
        <div class="conversation-composer__body">
          <textarea v-model="followUp" rows="2" placeholder="继续追问" @keydown.enter.exact.prevent="continueSession" />
          <div class="conversation-composer__tools">
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
          <t-button v-else class="chat-send" type="submit" shape="square" aria-label="发送" :disabled="!followUp.trim()">
            <t-icon name="arrow-up" />
          </t-button>
        </div>
      </form>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { createChatTurn, fetchSessions, loadChatSession } from '@/api/videohub/chat'
import type { StreamingChatMessage } from '@/api/videohub/chat'
import type { ChatMessage, ChatSession } from '@/types/videohub'
import AgentStreamDisplay from '@/views/chat/components/AgentStreamDisplay.vue'
import VideohubAgentPicker from '@/components/videohub/VideohubAgentPicker.vue'
import { useSettingsStore } from '@/stores/settings'
import { shouldAutoRoute } from '@/api/videohub/chatRequest'
import {
  CHAT_PAGE_RECOVERY_KEY,
  clearChatRecovery,
  getChatRecoveryStorage,
  readChatRecovery,
  writeChatRecovery,
} from '@/api/videohub/chatRecovery'

const router = useRouter()
const route = useRoute()
const settingsStore = useSettingsStore()
const question = ref(''), followUp = ref(''), sessions = ref<ChatSession[]>([]), activeSession = ref<ChatSession | null>(null)
const loadingSessions = ref(true), isGenerating = ref(false), messageArea = ref<HTMLElement | null>(null)
const conversationLoading = ref(Boolean(route.query.session))
const streamingAssistantId = ref('')
const stopCurrentTurn = ref<null | (() => Promise<void>)>(null)
const lastUserQuery = ref('')
const editingMessageId = ref('')
const editingMessageText = ref('')
const sessionListReady = ref(false)
const recoveryStorage = getChatRecoveryStorage()
const conversationSkeletonRows = [
  { id: 'user-1', align: 'user', rows: [{ width: '100%', height: '36px', type: 'rect' }] },
  { id: 'assistant-1', align: 'assistant', rows: [{ width: '78%', height: '16px' }, { width: '100%', height: '16px' }, { width: '56%', height: '16px' }] },
  { id: 'user-2', align: 'user', rows: [{ width: '100%', height: '36px', type: 'rect' }] },
  { id: 'assistant-2', align: 'assistant', rows: [{ width: '68%', height: '16px' }, { width: '90%', height: '16px' }] },
]
interface SuggestedTask {
  id: string
  label: string
  prompt: string
  meta: string
}
const fallbackSuggestedTasks: SuggestedTask[] = [
  {
    id: 'suggestion-prompt-formula',
    label: '提炼最近视频里的 AI 提示词公式',
    prompt: '请提炼最近看过的视频里的 AI 提示词公式，并整理成可以直接复用的模板。',
    meta: '从最近学习内容开始',
  },
  {
    id: 'suggestion-summary',
    label: '总结最近看过的视频重点',
    prompt: '请总结我最近看过的视频，列出每个视频的核心观点和关键时间点。',
    meta: '跨视频整理',
  },
  {
    id: 'suggestion-follow-up',
    label: '继续追问上次对话中的关键概念',
    prompt: '请结合上次对话，继续解释其中最重要的概念，并给出一个实际应用例子。',
    meta: '延续最近对话',
  },
  {
    id: 'suggestion-action',
    label: '把学习内容转成下一步行动',
    prompt: '请根据最近学习的视频内容，整理一份今天可以执行的行动清单。',
    meta: '把知识变成行动',
  },
]
type RenderableChatMessage = ChatMessage & Partial<StreamingChatMessage>
const shorten = (value: string, length = 26) => {
  const normalized = value.replace(/\s+/g, ' ').trim()
  return normalized.length > length ? `${normalized.slice(0, length)}…` : normalized
}
const suggestedTasks = computed<SuggestedTask[]>(() => {
  const derived: SuggestedTask[] = []
  const seen = new Set<string>()
  for (const session of sessions.value) {
    const latestQuestion = [...session.messages].reverse().find(message => message.sender === 'user')?.text?.trim()
    const source = latestQuestion || session.title?.trim()
    if (!source || source === '未命名会话' || seen.has(source)) continue
    seen.add(source)
    derived.push({
      id: `suggestion-${session.id}`,
      label: `继续探索：${shorten(source)}`,
      prompt: source,
      meta: session.videoTitle ? `围绕视频「${shorten(session.videoTitle, 22)}」` : `来自${session.time || '最近'}的对话`,
    })
    if (derived.length >= 4) break
  }
  return derived.length > 0 ? derived : fallbackSuggestedTasks
})
const streamingAssistantVisible = computed(() => Boolean(streamingAssistantId.value && activeSession.value?.messages.some(message => {
  const item = message as RenderableChatMessage
  return item.id === streamingAssistantId.value && Boolean(item.text || item.activityText || item.agentEventStream?.length)
})))

async function scrollBottom() { await nextTick(); if (messageArea.value) messageArea.value.scrollTop = messageArea.value.scrollHeight }
const sessionQueryId = computed(() => {
  const value = route.query.session
  return typeof value === 'string' ? value : ''
})
function toNativeAgentSession(message: ChatMessage) {
  const item = message as RenderableChatMessage
  return {
    id: item.id,
    assistant_message_id: item.assistant_message_id || item.id,
    request_id: item.request_id || item.id,
    role: 'assistant',
    content: item.content || item.text,
    isAgentMode: true,
    is_completed: item.is_completed ?? true,
    agentEventStream: item.agentEventStream?.length
      ? item.agentEventStream
      : [{ type: 'answer', content: item.text, done: true }, { type: 'agent_complete', total_duration_ms: 0, total_steps: 0 }],
    knowledge_references: item.knowledge_references || [],
    debugRequest: item.debugRequest,
  }
}
function questionForMessage(index: number): string {
  if (!activeSession.value) return lastUserQuery.value
  for (let cursor = index - 1; cursor >= 0; cursor -= 1) {
    const message = activeSession.value.messages[cursor]
    if (message?.sender === 'user') return message.text
  }
  return lastUserQuery.value
}
function hasAnswerOutputAfter(index: number): boolean {
  if (!activeSession.value) return false
  const nextAssistant = activeSession.value.messages.slice(index + 1).find(message => message.sender === 'assistant') as RenderableChatMessage | undefined
  if (!nextAssistant) return false
  if (nextAssistant.text?.trim() || nextAssistant.content?.trim()) return true
  return Boolean(nextAssistant.agentEventStream?.some(event => {
    const item = event as { type?: string; content?: string }
    return (item.type === 'answer' || item.type === 'complete') && Boolean(item.content?.trim())
  }))
}
function canEditMessage(message: ChatMessage, index: number): boolean {
  return message.sender === 'user' && !isGenerating.value && !hasAnswerOutputAfter(index)
}
function shouldShowVideoTitle(index: number): boolean {
  return false
}
function updateStreamingMessage(messageId: string, message: StreamingChatMessage) {
  if (!activeSession.value) return
  const target = activeSession.value.messages.find(item => item.id === messageId) as RenderableChatMessage | undefined
  if (!target) return
  Object.assign(target, message, { id: messageId })
  void scrollBottom()
}
function pushStreamingPlaceholder(target: ChatSession) {
  const assistantId = `assistant-${Date.now()}`
  streamingAssistantId.value = assistantId
  target.messages.push({ id: assistantId, sender: 'assistant', text: '', timestamp: '刚刚' } as StreamingChatMessage)
  return assistantId
}

function beginSuggestedSession(promptText: string) {
  if (isGenerating.value) return
  question.value = promptText
  void startSession()
}

function showToolMessage(label: string) {
  MessagePlugin.info(`${label}入口暂未接入`)
}

function pauseGeneration() {
  const stop = stopCurrentTurn.value
  if (!stop) return
  stopCurrentTurn.value = null
  void stop()
}

function startMessageEdit(message: ChatMessage) {
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
  followUp.value = value
  await continueSession()
}

function materializePendingSession(pendingSession: ChatSession, createdSession: ChatSession) {
  const materialized = { ...createdSession, messages: pendingSession.messages }
  sessions.value = sessions.value.map(item => item.id === pendingSession.id ? materialized : item)
  if (activeSession.value?.id === pendingSession.id) activeSession.value = materialized
  writeChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY, {
    scope: 'global',
    question: lastUserQuery.value,
    sessionId: createdSession.id,
    title: createdSession.title,
    updatedAt: Date.now(),
  })
  void router.replace({ path: '/platform/ai-chat', query: { session: createdSession.id } })
  return materialized
}
function persistPendingQuestion(questionText: string, session?: ChatSession) {
  writeChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY, {
    scope: 'global',
    question: questionText,
    ...(session?.id && !session.id.startsWith('pending-') ? { sessionId: session.id } : {}),
    ...(session?.title ? { title: session.title } : {}),
    updatedAt: Date.now(),
  })
}
function appendRecoveredQuestion(session: ChatSession, questionText: string): ChatSession {
  if (session.messages.some(message => message.sender === 'user' && message.text.trim() === questionText.trim())) return session
  return {
    ...session,
    messages: [
      ...session.messages,
      { id: `recovered-user-${Date.now()}`, sender: 'user', text: questionText, timestamp: '刚刚' },
    ],
  }
}
async function startSession() {
  const value = question.value.trim(); if (!value || isGenerating.value) return
  lastUserQuery.value = value
  persistPendingQuestion(value)
  question.value = ''
  followUp.value = ''
  const session: ChatSession = { id: `pending-${Date.now()}`, title: value.slice(0, 24), type: 'chat', time: '刚刚', messages: [{ id: `user-${Date.now()}`, sender: 'user', text: value, timestamp: '刚刚' }], scope: 'global' }
  const assistantId = pushStreamingPlaceholder(session)
  sessions.value.unshift(session); activeSession.value = session; isGenerating.value = true; await scrollBottom()
  try {
    const created = await createChatTurn(value, {
      globalMode: true,
      agentId: settingsStore.selectedAgentId,
      agentEnabled: settingsStore.isAgentEnabled,
      autoRoute: shouldAutoRoute(settingsStore.selectedAgentId, settingsStore.settings.selectedAgentExplicit),
      agentSourceTenantId: settingsStore.selectedAgentSourceTenantId,
      onSessionCreated: createdSession => materializePendingSession(session, createdSession),
      onStreamMessage: message => updateStreamingMessage(assistantId, message),
      onStopReady: stop => { stopCurrentTurn.value = stop },
    })
    sessions.value = sessions.value.map(item => item.id === session.id || item.id === created.id ? created : item)
    activeSession.value = created
    clearChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY)
    window.dispatchEvent(new CustomEvent('weknora:session-refresh'))
    await router.replace({ path: '/platform/ai-chat', query: { session: created.id } })
  } catch (error) {
    session.messages.push({ id: `error-${Date.now()}`, sender: 'assistant', text: error instanceof Error ? error.message : '问答生成失败，请稍后重试', timestamp: '刚刚' })
  } finally { isGenerating.value = false; streamingAssistantId.value = ''; stopCurrentTurn.value = null; await scrollBottom() }
}
async function continueSession() {
  const value = followUp.value.trim(); if (!value || isGenerating.value || !activeSession.value) return
  const target = activeSession.value
  lastUserQuery.value = value
  persistPendingQuestion(value, target)
  followUp.value = ''
  target.messages.push({ id: `user-${Date.now()}`, sender: 'user', text: value, timestamp: '刚刚' })
  const assistantId = pushStreamingPlaceholder(target)
  isGenerating.value = true; await scrollBottom()
  try {
    const updated = await createChatTurn(value, {
      globalMode: target.scope !== 'video',
      agentId: settingsStore.selectedAgentId,
      agentEnabled: settingsStore.isAgentEnabled,
      autoRoute: shouldAutoRoute(settingsStore.selectedAgentId, settingsStore.settings.selectedAgentExplicit),
      agentSourceTenantId: settingsStore.selectedAgentSourceTenantId,
      currentVideo: target.videoId ? { id: target.videoId, title: target.videoTitle || '指定视频', category: 'general', categoryName: '通用分享', duration: '', durationSeconds: 0, created_at: '', video_url: '', poster_url: target.videoCoverUrl, overview: '', chapters: [], subtitles: [] } : undefined,
      session: target,
      onStreamMessage: message => updateStreamingMessage(assistantId, message),
      onStopReady: stop => { stopCurrentTurn.value = stop },
    })
    activeSession.value = updated
    sessions.value = [updated, ...sessions.value.filter(item => item.id !== updated.id)]
    clearChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY)
  } catch (error) {
    target.messages.push({ id: `error-${Date.now()}`, sender: 'assistant', text: error instanceof Error ? error.message : '问答生成失败，请稍后重试', timestamp: '刚刚' })
  } finally { isGenerating.value = false; streamingAssistantId.value = ''; stopCurrentTurn.value = null; await scrollBottom() }
}
async function openSession(session: ChatSession) {
  cancelMessageEdit()
  let resolvedSession = session
  if (!session.messages.length) {
    try { resolvedSession = await loadChatSession(session) }
    catch (error) {
      MessagePlugin.error(error instanceof Error ? error.message : '会话加载失败')
    }
  }
  activeSession.value = resolvedSession
  void scrollBottom()
}
async function openSessionFromRoute(sessionId: string) {
  if (!sessionId) return
  const existing = sessions.value.find(item => item.id === sessionId)
  if (existing) {
    await openSession(existing)
    return
  }
  try {
    const refreshed = await fetchSessions()
    sessions.value = refreshed
    const target = refreshed.find(item => item.id === sessionId)
    if (target) await openSession(target)
  } catch (error) {
    MessagePlugin.error(error instanceof Error ? error.message : '会话加载失败')
  }
}
async function restorePendingChat() {
  const pending = readChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY)
  if (!pending) return
  if (!pending.sessionId) {
    question.value = pending.question
    return
  }
  try {
    await router.replace({ path: '/platform/ai-chat', query: { session: pending.sessionId } })
    const refreshed = await fetchSessions()
    sessions.value = refreshed
    const target = refreshed.find(item => item.id === pending.sessionId)
    if (!target) {
      question.value = pending.question
      return
    }
    const loaded = await loadChatSession(target)
    const recovered = appendRecoveredQuestion(loaded, pending.question)
    activeSession.value = recovered
    sessions.value = sessions.value.map(item => item.id === recovered.id ? recovered : item)
    clearChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY)
    void scrollBottom()
  } catch {
    question.value = pending.question
  }
}
function back() {
  cancelMessageEdit()
  activeSession.value = null
  followUp.value = ''
  isGenerating.value = false
  stopCurrentTurn.value = null
  void router.replace('/platform/ai-chat')
}
function navigateToEvidence(videoId: string, seconds: number) {
  if (!videoId || !Number.isFinite(seconds) || seconds < 0) return
  router.push(`/platform/videos/${videoId}?t=${Math.floor(seconds)}`)
}
watch(sessionQueryId, async (sessionId) => {
  if (!sessionListReady.value) return
  if (!sessionId) {
    conversationLoading.value = false
    activeSession.value = null
    return
  }
  if (activeSession.value?.id !== sessionId) {
    conversationLoading.value = true
    try { await openSessionFromRoute(sessionId) }
    finally { conversationLoading.value = false }
  }
})

onMounted(async () => {
  try {
    conversationLoading.value = Boolean(sessionQueryId.value)
    sessions.value = await fetchSessions()
    if (sessionQueryId.value) {
      await openSessionFromRoute(sessionQueryId.value)
      const pending = readChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY)
      if (pending?.sessionId === sessionQueryId.value && activeSession.value?.id === sessionQueryId.value) {
        const recovered = appendRecoveredQuestion(activeSession.value, pending.question)
        activeSession.value = recovered
        sessions.value = sessions.value.map(item => item.id === recovered.id ? recovered : item)
        clearChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY)
      } else if (sessions.value.some(session => session.id === sessionQueryId.value) && !pending?.sessionId) {
        clearChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY)
      }
    }
    else await restorePendingChat()
  } finally {
    conversationLoading.value = false
    loadingSessions.value = false
    sessionListReady.value = true
  }
})
</script>

<style scoped>
.chat-page {
  position: relative;
  isolation: isolate;
  display: flex;
  box-sizing: border-box;
  min-height: 100%;
  height: 100%;
  overflow: hidden;
  scrollbar-width: none;
  padding: 28px 48px 38px;
  color: var(--td-text-color-primary);
  background:
    linear-gradient(120deg, rgba(232, 237, 234, .96), rgba(248, 249, 248, .94) 46%, rgba(235, 240, 237, .96));
}

.chat-page::-webkit-scrollbar { display: none; width: 0; height: 0; }

.chat-page::before,
.chat-page::after {
  position: absolute;
  inset: 0;
  z-index: -1;
  pointer-events: none;
  content: "";
}

.chat-page::before {
  opacity: .66;
  background:
    linear-gradient(90deg, rgba(255,255,255,.36), transparent 34%, rgba(255,255,255,.18)),
    repeating-linear-gradient(115deg, rgba(255,255,255,.1) 0 1px, transparent 1px 9px);
}

.chat-page::after {
  background: rgba(255,255,255,.15);
}

.chat-landing,
.conversation {
  width: min(920px, 100%);
  min-width: 0;
  margin: auto;
}

.chat-landing {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100%;
  transform: translateY(-4vh);
}

.chat-landing__inner {
  width: 100%;
}

.chat-landing h1 {
  margin: 0 0 36px;
  color: var(--td-text-color-primary);
  font-size: clamp(32px, 3vw, 44px);
  font-weight: 720;
  letter-spacing: -.035em;
  line-height: 1.15;
  text-align: center;
}

.chat-composer {
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  overflow: hidden;
  border: 1px solid rgba(255,255,255,.84);
  border-radius: var(--td-radius-extraLarge);
  background: rgba(255,255,255,.62);
  box-shadow: 0 16px 42px rgba(27,37,31,.1), 0 3px 12px rgba(27,37,31,.05);
  backdrop-filter: blur(26px) saturate(112%);
  -webkit-backdrop-filter: blur(26px) saturate(112%);
  transition: border-color .2s ease, box-shadow .2s ease;
}

.chat-composer:focus-within,
.conversation-composer:focus-within {
  border-color: color-mix(in srgb, var(--td-brand-color) 42%, transparent);
  box-shadow: 0 18px 44px rgba(27,37,31,.12), 0 0 0 3px color-mix(in srgb, var(--td-brand-color) 12%, transparent);
}

.chat-composer textarea {
  display: block;
  box-sizing: border-box;
  width: 100%;
  min-height: 128px;
  resize: none;
  padding: 20px 20px 10px;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--td-text-color-primary);
  font: inherit;
  font-size: 16px;
  line-height: 26px;
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.chat-composer textarea::-webkit-scrollbar,
.conversation-composer textarea::-webkit-scrollbar,
.message-edit textarea::-webkit-scrollbar { display: none; width: 0; height: 0; }

.chat-composer textarea::placeholder,
.conversation-composer textarea::placeholder {
  color: var(--td-text-color-placeholder);
}

.suggestions {
  margin-top: 30px;
}

.suggestions__heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: 0 2px 12px;
}

.suggestions__title {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  color: var(--td-text-color-secondary);
  font-size: 12px;
  font-weight: 600;
}

.suggestions__spark,
.suggestion-card__icon {
  display: grid;
  place-items: center;
  color: var(--td-brand-color);
}

.suggestions__spark {
  width: 17px;
  height: 17px;
  border-radius: 6px;
  background: color-mix(in srgb, var(--td-brand-color) 12%, transparent);
  font-size: 13px;
}

.suggestions__note {
  color: var(--td-text-color-placeholder);
  font-size: 11px;
}

.suggestions__grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.suggestion-card {
  display: flex;
  align-items: flex-start;
  gap: 11px;
  min-height: 78px;
  padding: 13px 15px;
  border: 1px solid rgba(255,255,255,.84);
  border-radius: var(--td-radius-large);
  color: var(--td-text-color-primary);
  background: rgba(255,255,255,.48);
  box-shadow: 0 4px 14px rgba(27,37,31,.035);
  text-align: left;
  cursor: pointer;
  backdrop-filter: blur(18px);
  -webkit-backdrop-filter: blur(18px);
  transition: border-color .18s ease, background .18s ease, transform .18s ease, box-shadow .18s ease;
}

.suggestion-card:hover {
  border-color: color-mix(in srgb, var(--td-brand-color) 30%, transparent);
  background: rgba(255,255,255,.76);
  box-shadow: 0 10px 24px rgba(27,37,31,.08);
  transform: translateY(-2px);
}

.suggestion-card__icon {
  width: 27px;
  height: 27px;
  flex: 0 0 27px;
  border-radius: 8px;
  background: color-mix(in srgb, var(--td-brand-color) 12%, transparent);
  font-size: 14px;
}

.suggestion-card__content {
  display: grid;
  min-width: 0;
  padding-top: 1px;
}

.suggestion-card__text {
  font-size: 13px;
  line-height: 20px;
}

.suggestion-card__meta {
  display: block;
  margin-top: 4px;
  overflow: hidden;
  color: var(--td-text-color-placeholder);
  font-size: 11px;
  line-height: 16px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chat-composer footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 7px 12px 12px 16px;
}

.chat-composer__tools,
.conversation-composer__tools {
  display: flex;
  align-items: center;
  gap: 3px;
  min-width: 0;
}

.chat-composer__tools :deep(.videohub-agent-picker),
.conversation-composer__tools :deep(.videohub-agent-picker) {
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
  box-shadow: 0 6px 14px rgba(7,192,95,.24);
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

.conversation {
  display: grid;
  grid-template-rows: auto 1fr auto;
  box-sizing: border-box;
  height: calc(100% - 4px);
  min-height: 0;
  overflow: hidden;
  padding: 0 0 8px;
}

.conversation > header {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  padding-bottom: 10px;
  border-bottom: 1px solid rgba(0,0,0,.08);
}

.conversation > header div {
  min-width: 0;
}

.conversation > header strong {
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.conversation--loading .conversation__loading-header :deep(.t-skeleton),
.conversation-composer--loading :deep(.t-skeleton) {
  display: grid;
  gap: 8px;
  width: 100%;
}

.conversation__messages--loading {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.message-skeleton {
  display: flex;
  min-width: 0;
}

.message-skeleton--user {
  justify-content: flex-end;
}

.message-skeleton--user > :deep(.t-skeleton) {
  width: min(42%, 420px);
  margin-left: auto;
}

.message-skeleton--assistant > :deep(.t-skeleton) {
  width: 100%;
}

.message-skeleton--assistant {
  flex-direction: column;
  gap: 8px;
  padding-left: 4px;
}

.conversation-composer--loading {
  align-items: center;
}

.conversation-composer--loading > :last-child {
  justify-self: end;
}

.conversation__messages {
  min-height: 0;
  min-width: 0;
  overflow-y: auto;
  overflow-x: hidden;
  overscroll-behavior: contain;
  padding: 16px 0 24px;
  scrollbar-width: none;
  -webkit-overflow-scrolling: touch;
}

.conversation-composer {
  display: grid;
  grid-template-columns: minmax(0, 1fr) max-content;
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  gap: 10px;
  align-items: end;
  padding: 10px 0 10px 12px;
  border: 1px solid rgba(255,255,255,.84);
  border-radius: var(--td-radius-extraLarge);
  background: rgba(255,255,255,.62);
  box-shadow: 0 12px 32px rgba(27,37,31,.08);
  backdrop-filter: blur(24px) saturate(112%);
  -webkit-backdrop-filter: blur(24px) saturate(112%);
}

.conversation-composer__body {
  width: 100%;
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
}

.conversation-composer textarea {
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
  font: inherit;
  line-height: 1.6;
  overflow-wrap: anywhere;
  scrollbar-width: none;
  -ms-overflow-style: none;
}

.message {
  display: flex;
  min-width: 0;
  margin-bottom: 14px;
}

.message--user {
  position: relative;
  justify-content: flex-end;
  padding-right: 0;
  margin-bottom: 20px;
}
.message-user-content { display: grid; justify-items: end; min-width: 0; max-width: min(100%, 920px); }
.message-user-content > p { max-width: 100%; overflow-wrap: anywhere; word-break: break-word; }
.message-user-content__text--editable { cursor: text; }
.message-user-content__text--editable:hover,
.message-user-content__text--editable:focus-visible { background: transparent; color: var(--td-brand-color); outline: 0; }
.message-user-content__edit {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 20px;
  margin-top: 2px;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--td-text-color-placeholder);
  cursor: pointer;
  opacity: 0;
  transition: color .15s ease, opacity .15s ease;
}
.message-user-content:hover .message-user-content__edit,
.message-user-content:focus-within .message-user-content__edit { opacity: 1; }
.message-user-content__edit:hover,
.message-user-content__edit:focus-visible { background: transparent; color: var(--td-brand-color); outline: 0; }
.message-user-content__edit-icon { width: 16px; height: 16px; }
.message-edit { display: grid; gap: 8px; width: min(520px, 72vw); }
.message-edit textarea {
  box-sizing: border-box;
  width: 100%;
  resize: vertical;
  padding: 10px 12px;
  border: 1px solid var(--td-component-border);
  border-radius: var(--td-radius-large);
  outline: 0;
  background: rgba(255,255,255,.72);
  color: var(--td-text-color-primary);
  font: inherit;
  line-height: 1.6;
}
.message-edit textarea:focus { border-color: var(--td-brand-color); }
.message-edit__actions { display: flex; justify-content: flex-end; gap: 8px; }
.message-edit__actions :deep(.t-button) { border: 0; background: transparent; color: var(--td-text-color-secondary); }
.message-edit__actions :deep(.t-button:hover),
.message-edit__actions :deep(.t-button:focus-visible) { background: transparent; color: var(--td-brand-color); }
.message-edit__actions :deep(.t-button--theme-primary) { color: var(--td-brand-color); }
.message--assistant { display: block; }
.message__bubble { min-width: 0; max-width: 72%; overflow-wrap: anywhere; }
.message--assistant .message__bubble { width: 100%; max-width: none; }
.message__bubble > p { margin: 0; padding: 11px 14px; border-radius: var(--td-radius-large); background: rgba(255,255,255,.54); line-height: 1.7; white-space: pre-wrap; }
.message--assistant .message__bubble > p { background: transparent; }
.message--assistant :deep(.agent-stream-display),
.message--assistant :deep(.markdown-content),
.message--assistant :deep(.answer-content) { width: 100%; max-width: 100%; min-width: 0; overflow-wrap: anywhere; }
.message--assistant :deep(.t-image-viewer__trigger--hover:empty) { display: none; }
.generating { display: flex; align-items: center; gap: 8px; color: var(--td-text-color-secondary); }

@media (max-width: 900px) {
  .chat-page { padding: 24px; }
  .chat-landing { transform: translateY(-2vh); }
}

@media (max-width: 640px) {
  .chat-page { padding: 20px 16px; }
  .chat-landing h1 { margin-bottom: 26px; font-size: 31px; }
  .suggestions__grid { grid-template-columns: 1fr; }
  .message__bubble { max-width: 88%; }
  .message--assistant .message__bubble { max-width: none; }
  .conversation-composer {
    grid-template-columns: minmax(0, 1fr);
    gap: 4px;
    padding: 8px 10px;
  }
  .conversation-composer .chat-actions {
    justify-content: flex-end;
  }
}
</style>
