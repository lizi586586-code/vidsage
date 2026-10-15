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
        <t-skeleton animation="gradient" :row-col="[{ width: '28px', height: '28px', type: 'circle' }]" />
      </div>
    </section>
    <section v-else-if="!activeSession" class="chat-landing">
      <div class="chat-landing__inner">
        <h1>Learn with Vidsage</h1>
        <ReviewCourseChooser v-if="choosingCourse" class="chat-review-chooser" v-model="selectedCourseId" :courses="courses" :recent-course-ids="recentCourseIds" :loading="loadingCourses"
          :error="courseError" :busy="preparingClassroom" @confirm="confirmCourse" @cancel="cancelCourse" @retry="loadCourses" />
        <form class="chat-composer" @submit.prevent="startSession">
          <textarea v-model="question" rows="4" placeholder="请输入你的问题" @keydown.enter.exact.prevent="startSession" />
          <footer class="chat-composer__controls">
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
        <button v-if="!choosingCourse" class="review-entry" type="button" :disabled="isGenerating || preparingClassroom" @click="openReviewCourse()">
          <ReviewLearningIcon class="review-entry__icon" />
          <span class="review-entry__copy"><strong>巩固学习</strong><small>根据学习特性，生成专属复习内容</small></span>
        </button>
      </div>
    </section>
    <section v-else class="conversation">
      <header><div><strong>{{ activeSession.title }}</strong></div></header>
      <div ref="messageArea" class="conversation__messages">
        <article v-if="reviewFallbackInsertIndex === 0" class="message message--assistant">
          <AssistantReplyShell>
          <ReviewMaterialReply :request-id="classroomResult?.request_id || ''" :user-id="authStore.user?.id || ''"
            :initial-task="classroomResult" :video="classroomVideo" @status="updateStatus" />
          </AssistantReplyShell>
        </article>
        <template v-for="(message, messageIndex) in activeSession.messages" :key="message.id">
        <article v-if="!isReviewReply(message, messageIndex)" :class="['message', `message--${message.sender}`]">
          <div class="message__bubble">
            <component :is="message.sender === 'assistant' ? AssistantReplyShell : 'div'" :timestamp="message.sender === 'assistant' ? formatChatMessageTime(message.timestamp) : undefined">
            <AgentStreamDisplay
              v-if="message.sender === 'assistant' && (message.id !== streamingAssistantId || streamingAssistantVisible)"
              :session="toNativeAgentSession(message)"
              :session-id="activeSession.id"
              :user-query="questionForMessage(messageIndex) || lastUserQuery"
              :show-video-title="shouldShowVideoTitle(messageIndex)"
              :show-request-info="false"
              :hydrate-protected-images="false"
              @video-navigate="navigateToEvidence"
            />
            <div v-else-if="message.sender === 'assistant'" class="generating"><t-loading size="small" /> AI 正在整合视频知识回答中...</div>
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
    >{{ visibleReviewQuestion(message.text) }}</p>
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
            </component>
          </div>
        </article>
        <article v-if="isFirstReviewRequest(activeSession.messages, messageIndex)" class="message message--assistant">
          <AssistantReplyShell :timestamp="reviewReplyTime(activeSession.messages, messageIndex)">
          <ReviewMaterialReply :request-id="reviewRequestId(message.text)" :user-id="authStore.user?.id || ''"
            :initial-task="classroomResult" :video="classroomResult?.request_id === reviewRequestId(message.text) ? classroomVideo : null" @status="updateStatus" />
          </AssistantReplyShell>
        </article>
        <article v-if="reviewFallbackInsertIndex === messageIndex + 1" class="message message--assistant">
          <AssistantReplyShell>
          <ReviewMaterialReply :request-id="classroomResult?.request_id || ''" :user-id="authStore.user?.id || ''"
            :initial-task="classroomResult" :video="classroomVideo" @status="updateStatus" />
          </AssistantReplyShell>
        </article>
        </template>
        <ReviewCourseChooser v-if="choosingCourse" v-model="selectedCourseId" :courses="courses" :recent-course-ids="recentCourseIds" :loading="loadingCourses"
          :error="courseError" :busy="preparingClassroom" @confirm="confirmCourse" @cancel="cancelCourse" @retry="loadCourses" />
      </div>
      <button class="review-entry review-entry--conversation" type="button" :disabled="isGenerating || preparingClassroom" @click="openReviewCourseInConversation()">
        <span class="review-entry__copy"><strong>巩固知识</strong></span>
      </button>
      <form class="conversation-composer" @submit.prevent="continueSession">
        <div class="conversation-composer__body">
          <textarea
            ref="followUpInput"
            v-model="followUp"
            rows="1"
            placeholder="请输入你的问题"
            @input="resizeFollowUpInput"
            @keydown.enter.exact.prevent="continueSession"
          />
        </div>
        <div class="conversation-composer__controls">
          <div class="conversation-composer__tools">
            <VideohubAgentPicker appearance="tool" />
          </div>
          <div class="chat-actions">
            <t-tooltip content="语音输入" placement="top">
              <t-button class="chat-voice" type="button" shape="square" aria-label="语音输入" @click="showToolMessage('语音输入')">
                <svg class="chat-voice__icon" viewBox="0 0 1024 1024" aria-hidden="true" focusable="false">
                  <path d="M486.4 972.8v-128.9728A332.8 332.8 0 0 1 179.2 512a25.6 25.6 0 0 1 51.2 0 281.6 281.6 0 0 0 563.2 0 25.6 25.6 0 1 1 51.2 0 332.8 332.8 0 0 1-307.2 331.8272V972.8h153.6a25.6 25.6 0 1 1 0-51.2h-358.4a25.6 25.6 0 1 1 0-51.2h153.6zM512 51.2a153.6 153.6 0 0 0-153.6 153.6v307.2a153.6 153.6 0 0 0 307.2 0V204.8a153.6 153.6 0 0 0-153.6-153.6z m0-51.2a204.8 204.8 0 0 1 204.8 204.8v307.2a204.8 204.8 0 1 1-409.6 0V204.8a204.8 204.8 0 0 1 204.8-204.8z" fill="currentColor" />
                </svg>
              </t-button>
            </t-tooltip>
            <t-tooltip v-if="isGenerating || canPauseReview" content="暂停生成" placement="top">
              <t-button class="chat-send chat-pause" type="button" shape="square" :disabled="cancellingClassroom" aria-label="暂停生成" @click="pauseGeneration">
                <t-icon name="pause-circle" />
              </t-button>
            </t-tooltip>
            <t-button v-else class="chat-send" type="submit" shape="square" aria-label="发送" :disabled="!followUp.trim()">
              <t-icon name="arrow-up" />
            </t-button>
          </div>
        </div>
      </form>
    </section>
  </main>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { MessagePlugin } from 'tdesign-vue-next'
import { createChatTurn, fetchSessions, loadChatSession } from '@/api/videohub/chat'
import type { StreamingChatMessage } from '@/api/videohub/chat'
import type { ChatMessage, ChatSession } from '@/types/videohub'
import AgentStreamDisplay from '@/views/chat/components/AgentStreamDisplay.vue'
import VideohubAgentPicker from '@/components/videohub/VideohubAgentPicker.vue'
import AssistantReplyShell from '@/components/videohub/AssistantReplyShell.vue'
import { currentChatMessageTime, formatChatMessageTime, reviewReplyTime } from '@/utils/chatMessageTime'
import { useSettingsStore } from '@/stores/settings'
import { useAuthStore } from '@/stores/auth'
import { useReviewCourse } from '@/composables/useReviewCourse'
import { isFirstReviewRequest, isReviewIntent, needsReviewCourseChoice, readReviewTaskAnchor, reviewRequestId, reviewTaskFallbackIndex, saveReviewTaskAnchor, visibleReviewQuestion } from '@/api/videohub/reviewMaterials'
import { VIDSAGE_REASONING_AGENT_ID } from '@/api/agent'
import ReviewMaterialReply from '@/components/videohub/ReviewMaterialReply.vue'
import ReviewCourseChooser from '@/components/videohub/ReviewCourseChooser.vue'
import ReviewLearningIcon from '@/components/videohub/ReviewLearningIcon.vue'
import { shouldAutoRoute } from '@/api/videohub/chatRequest'
import {
  CHAT_PAGE_RECOVERY_KEY,
  clearChatRecovery,
  getChatRecoveryStorage,
  readChatRecovery,
  writeChatRecovery,
} from '@/api/videohub/chatRecovery'

defineOptions({ name: 'ChatView' })

const router = useRouter()
const route = useRoute()
const settingsStore = useSettingsStore()
const authStore = useAuthStore()
const question = ref(''), followUp = ref(''), sessions = ref<ChatSession[]>([]), activeSession = ref<ChatSession | null>(null)
const loadingSessions = ref(true), isGenerating = ref(false), messageArea = ref<HTMLElement | null>(null)
const followUpInput = ref<HTMLTextAreaElement | null>(null)
const followUpMaxHeight = 124
const conversationLoading = ref(Boolean(route.query.session))
const streamingAssistantId = ref('')
const stopCurrentTurn = ref<null | (() => Promise<void>)>(null)
const activeReviewRequestId = ref('')
let pauseRequested = false
let turnRevision = 0
function leaveCurrentTurn() {
  turnRevision += 1
  activeReviewRequestId.value = ''
  pauseRequested = false
  void stopCurrentTurn.value?.()
  stopCurrentTurn.value = null
  isGenerating.value = false
  streamingAssistantId.value = ''
}
onUnmounted(leaveCurrentTurn)
const lastUserQuery = ref('')
const editingMessageId = ref('')
const editingMessageText = ref('')
const sessionListReady = ref(false)
const recoveryStorage = getChatRecoveryStorage()
const reviewAnchorRevision = ref(0)
const conversationSkeletonRows = [
  { id: 'user-1', align: 'user', rows: [{ width: '100%', height: '36px', type: 'rect' }] },
  { id: 'assistant-1', align: 'assistant', rows: [{ width: '78%', height: '16px' }, { width: '100%', height: '16px' }, { width: '56%', height: '16px' }] },
  { id: 'user-2', align: 'user', rows: [{ width: '100%', height: '36px', type: 'rect' }] },
  { id: 'assistant-2', align: 'assistant', rows: [{ width: '68%', height: '16px' }, { width: '90%', height: '16px' }] },
]
type RenderableChatMessage = ChatMessage & Partial<StreamingChatMessage>
const streamingAssistantVisible = computed(() => Boolean(streamingAssistantId.value && activeSession.value?.messages.some(message => {
  const item = message as RenderableChatMessage
  return item.id === streamingAssistantId.value && Boolean(item.text || item.activityText || item.agentEventStream?.length)
})))

async function scrollBottom() { await nextTick(); if (messageArea.value) messageArea.value.scrollTop = messageArea.value.scrollHeight }
function resizeFollowUpInput() {
  const textarea = followUpInput.value
  if (!textarea) return
  textarea.style.height = 'auto'
  const height = Math.min(textarea.scrollHeight, followUpMaxHeight)
  textarea.style.height = `${height}px`
  textarea.style.overflowY = textarea.scrollHeight > followUpMaxHeight ? 'auto' : 'hidden'
}
watch(followUp, () => { void nextTick(resizeFollowUpInput) })
const sessionQueryId = computed(() => {
  const value = route.query.session
  return typeof value === 'string' ? value : ''
})
const { choosingCourse, loadingCourses, courseError, courses, selectedCourseId, preparingClassroom,
  classroomResult, classroomVideo, recentCourseIds, cancellingClassroom, cancelReview, askCourse, loadCourses, cancelCourse, confirmCourse, updateStatus } = useReviewCourse({
  userId: computed(() => authStore.user?.id || ''),
  storageKey: computed(() => `openmaic:${authStore.user?.id || ''}:chat`),
  contextKey: computed(() => activeSession.value?.id || ''),
  busy: isGenerating,
  onStart: async instruction => {
    const requestId = reviewRequestId(instruction)
    saveReviewTaskAnchor(recoveryStorage, authStore.user?.id || '', requestId, activeSession.value?.messages.length || 0)
    reviewAnchorRevision.value += 1
    if (activeSession.value) { followUp.value = instruction; await continueSession() }
    else { question.value = instruction; await startSession() }
  },
})
const canPauseReview = computed(() => Boolean(activeReviewRequestId.value
  && classroomResult.value?.request_id === activeReviewRequestId.value
  && ['queued', 'submitting', 'running'].includes(classroomResult.value.generation_status)))
const reviewFallbackInsertIndex = computed(() => {
  void reviewAnchorRevision.value
  const requestId = classroomResult.value?.request_id || ''
  const messages = activeSession.value?.messages || []
  const anchor = readReviewTaskAnchor(recoveryStorage, authStore.user?.id || '', requestId, 0)
  return reviewTaskFallbackIndex(messages, requestId, anchor)
})
function isReviewReply(message: ChatMessage, index: number) {
  return message.sender === 'assistant' && !message.id.startsWith('error-') && Boolean(reviewRequestId(questionForMessage(index)))
}
async function openReviewCourse() {
  await askCourse()
}
async function openReviewCourseInConversation() {
  if (!activeSession.value || isGenerating.value || preparingClassroom.value) return
  await askCourse()
  await scrollBottom()
}
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
  return message.sender === 'user' && !reviewRequestId(message.text) && !isGenerating.value && !hasAnswerOutputAfter(index)
}
function shouldShowVideoTitle(index: number): boolean {
  return false
}
function updateStreamingMessage(messageId: string, message: StreamingChatMessage) {
  if (!activeSession.value) return
  const target = activeSession.value.messages.find(item => item.id === messageId) as RenderableChatMessage | undefined
  if (!target) return
  Object.assign(target, message, { id: messageId, timestamp: target.timestamp })
  void scrollBottom()
}
function pushStreamingPlaceholder(target: ChatSession) {
  const assistantId = `assistant-${Date.now()}`
  streamingAssistantId.value = assistantId
  target.messages.push({ id: assistantId, sender: 'assistant', text: '', timestamp: currentChatMessageTime() } as StreamingChatMessage)
  return assistantId
}

function showToolMessage(label: string) {
  MessagePlugin.info(`${label}入口暂未接入`)
}

async function pauseGeneration() {
  if (cancellingClassroom.value) return
  const requestId = activeReviewRequestId.value
  const revision = turnRevision
  pauseRequested = true
  const stop = stopCurrentTurn.value
  stopCurrentTurn.value = null
  if (stop) void stop().catch(() => { /* Task cancellation still proceeds if chat stop fails. */ })
  if (requestId && await cancelReview(requestId) && revision === turnRevision) activeReviewRequestId.value = ''
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
  window.dispatchEvent(new CustomEvent('weknora:session-refresh'))
  if (route.name === 'aiChat') {
    void router.replace({ path: '/platform/ai-chat', query: { session: createdSession.id } })
  }
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
  if (cancellingClassroom.value) return
  if (needsReviewCourseChoice(value)) { question.value = ''; await openReviewCourse(); return }
  const reinforcement = Boolean(reviewRequestId(value))
  const reviewTurn = reinforcement || isReviewIntent(value)
  activeReviewRequestId.value = reviewRequestId(value)
  pauseRequested = false
  if (!reviewTurn && preparingClassroom.value) return
  const revision = turnRevision
  lastUserQuery.value = value
  persistPendingQuestion(value)
  question.value = ''
  followUp.value = ''
  const session: ChatSession = { id: `pending-${Date.now()}`, title: visibleReviewQuestion(value).slice(0, 24), type: 'chat', time: '刚刚', messages: [{ id: `user-${Date.now()}`, sender: 'user', text: value, timestamp: '刚刚' }], scope: 'global' }
  const assistantId = pushStreamingPlaceholder(session)
  sessions.value.unshift(session); activeSession.value = session; isGenerating.value = true; await scrollBottom()
  try {
    const created = await createChatTurn(value, {
      assistantTimestamp: session.messages.find(message => message.id === assistantId)?.timestamp,
      globalMode: true,
      agentId: reviewTurn ? VIDSAGE_REASONING_AGENT_ID : settingsStore.selectedAgentId,
      agentEnabled: reviewTurn || settingsStore.isAgentEnabled,
      autoRoute: reviewTurn ? false : shouldAutoRoute(settingsStore.selectedAgentId, settingsStore.settings.selectedAgentExplicit),
      agentSourceTenantId: reviewTurn ? undefined : settingsStore.selectedAgentSourceTenantId,
      skillNames: reviewTurn ? ['openmaic-classroom'] : undefined,
      onSessionCreated: createdSession => { if (revision === turnRevision) materializePendingSession(session, createdSession) },
      onStreamMessage: message => { if (revision === turnRevision) updateStreamingMessage(assistantId, message) },
      onStopReady: stop => { if (revision === turnRevision && !pauseRequested) stopCurrentTurn.value = stop; else void stop() },
    })
    if (revision !== turnRevision) return
    sessions.value = sessions.value.map(item => item.id === session.id || item.id === created.id ? created : item)
    activeSession.value = created
    clearChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY)
    window.dispatchEvent(new CustomEvent('weknora:session-refresh'))
    if (route.name === 'aiChat') {
      await router.replace({ path: '/platform/ai-chat', query: { session: created.id } })
    }
  } catch (error) {
    if (revision !== turnRevision) return
    session.messages = session.messages.filter(message => message.id !== assistantId || Boolean(message.text))
    session.messages.push({ id: `error-${Date.now()}`, sender: 'assistant', text: error instanceof Error ? error.message : '问答生成失败，请稍后重试', timestamp: currentChatMessageTime() })
  } finally { if (revision === turnRevision) { isGenerating.value = false; streamingAssistantId.value = ''; stopCurrentTurn.value = null; await scrollBottom() } }
}
async function continueSession() {
  const value = followUp.value.trim(); if (!value || isGenerating.value || !activeSession.value) return
  if (cancellingClassroom.value) return
  if (needsReviewCourseChoice(value)) { followUp.value = ''; await askCourse(); await scrollBottom(); return }
  const reinforcement = Boolean(reviewRequestId(value))
  const reviewTurn = reinforcement || isReviewIntent(value)
  activeReviewRequestId.value = reviewRequestId(value)
  pauseRequested = false
  if (!reviewTurn && preparingClassroom.value) return
  const revision = turnRevision
  const target = activeSession.value
  lastUserQuery.value = value
  persistPendingQuestion(value, target)
  followUp.value = ''
  target.messages.push({ id: `user-${Date.now()}`, sender: 'user', text: value, timestamp: '刚刚' })
  const assistantId = pushStreamingPlaceholder(target)
  isGenerating.value = true; await scrollBottom()
  try {
    const updated = await createChatTurn(value, {
      assistantTimestamp: target.messages.find(message => message.id === assistantId)?.timestamp,
      globalMode: reviewTurn || target.scope !== 'video',
      agentId: reviewTurn ? VIDSAGE_REASONING_AGENT_ID : settingsStore.selectedAgentId,
      agentEnabled: reviewTurn || settingsStore.isAgentEnabled,
      autoRoute: reviewTurn ? false : shouldAutoRoute(settingsStore.selectedAgentId, settingsStore.settings.selectedAgentExplicit),
      agentSourceTenantId: reviewTurn ? undefined : settingsStore.selectedAgentSourceTenantId,
      skillNames: reviewTurn ? ['openmaic-classroom'] : undefined,
      currentVideo: !reviewTurn && target.videoId ? { id: target.videoId, title: target.videoTitle || '指定视频', category: 'general', categoryName: '通用分享', duration: '', durationSeconds: 0, created_at: '', video_url: '', poster_url: target.videoCoverUrl, overview: '', chapters: [], subtitles: [] } : undefined,
      session: target,
      onStreamMessage: message => { if (revision === turnRevision) updateStreamingMessage(assistantId, message) },
      onStopReady: stop => { if (revision === turnRevision && !pauseRequested) stopCurrentTurn.value = stop; else void stop() },
    })
    if (revision !== turnRevision) return
    const completed = reinforcement ? {
      ...updated,
      type: target.type,
      scope: target.scope,
      videoId: target.videoId,
      videoTitle: target.videoTitle,
      videoCoverUrl: target.videoCoverUrl,
    } : updated
    activeSession.value = completed
    sessions.value = [completed, ...sessions.value.filter(item => item.id !== completed.id)]
    clearChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY)
  } catch (error) {
    if (revision !== turnRevision) return
    target.messages = target.messages.filter(message => message.id !== assistantId || Boolean(message.text))
    target.messages.push({ id: `error-${Date.now()}`, sender: 'assistant', text: error instanceof Error ? error.message : '问答生成失败，请稍后重试', timestamp: currentChatMessageTime() })
  } finally { if (revision === turnRevision) { isGenerating.value = false; streamingAssistantId.value = ''; stopCurrentTurn.value = null; await scrollBottom() } }
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
    question.value = reviewRequestId(pending.question) ? '' : pending.question
    return
  }
  try {
    await router.replace({ path: '/platform/ai-chat', query: { session: pending.sessionId } })
    const refreshed = await fetchSessions()
    sessions.value = refreshed
    const target = refreshed.find(item => item.id === pending.sessionId)
    if (!target) {
      question.value = reviewRequestId(pending.question) ? '' : pending.question
      return
    }
    const loaded = await loadChatSession(target)
    const recovered = appendRecoveredQuestion(loaded, pending.question)
    activeSession.value = recovered
    sessions.value = sessions.value.map(item => item.id === recovered.id ? recovered : item)
    clearChatRecovery(recoveryStorage, CHAT_PAGE_RECOVERY_KEY)
    void scrollBottom()
  } catch {
    question.value = reviewRequestId(pending.question) ? '' : pending.question
  }
}
function back() {
  leaveCurrentTurn()
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
watch([() => route.name, sessionQueryId], async ([routeName, sessionId], [previousRouteName]) => {
  // Cached pages still observe route changes; only a chat selection can end a turn.
  if (routeName !== 'aiChat') return
  if (previousRouteName !== 'aiChat' && !sessionId && activeSession.value) {
    if (!activeSession.value.id.startsWith('pending-')) {
      await router.replace({ path: '/platform/ai-chat', query: { session: activeSession.value.id } })
    }
    return
  }
  if (sessionId !== activeSession.value?.id) leaveCurrentTurn()
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
.chat-review-chooser { margin-bottom: 16px; }
.chat-page {
  position: relative;
  isolation: isolate;
  display: flex;
  box-sizing: border-box;
  min-height: 100%;
  height: 100%;
  overflow: hidden;
  scrollbar-width: none;
  padding: 16px 48px 20px;
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
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: end;
  gap: 12px;
  box-sizing: border-box;
  width: 100%;
  min-width: 0;
  padding: 12px 14px 12px 20px;
  overflow: hidden;
  border: 1px solid rgba(255,255,255,.84);
  border-radius: var(--td-radius-extraLarge);
  background: rgba(255,255,255,.62);
  box-shadow: 0 16px 42px rgba(27,37,31,.1), 0 3px 12px rgba(27,37,31,.05);
  backdrop-filter: blur(26px) saturate(112%);
  -webkit-backdrop-filter: blur(26px) saturate(112%);
  transition: border-color .2s ease, box-shadow .2s ease;
}

.chat-composer:focus-within {
  border-color: color-mix(in srgb, var(--td-brand-color) 42%, transparent);
  box-shadow: 0 18px 44px rgba(27,37,31,.12), 0 0 0 3px color-mix(in srgb, var(--td-brand-color) 12%, transparent);
}

.chat-composer textarea {
  display: block;
  box-sizing: border-box;
  width: 100%;
  min-height: 128px;
  resize: none;
  padding: 8px 0;
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

.review-entry { display: flex; width: min(320px, 100%); min-height: 84px; margin-top: 16px; align-items: center; gap: 14px; padding: 10px 12px; border: 1px solid rgba(255,255,255,.9); border-radius: 12px; background: rgba(255,255,255,.42); backdrop-filter: blur(26px) saturate(112%); color: var(--td-text-color-primary); cursor: pointer; text-align: left; transition: background .2s ease, box-shadow .2s ease; }
.review-entry:hover:not(:disabled) { background: rgba(255,255,255,.58); box-shadow: 0 4px 16px rgba(0,0,0,.06); }
.review-entry:focus-visible { outline: 2px solid var(--td-brand-color); outline-offset: 3px; }
.review-entry__icon { width: 38px; height: 38px; flex: 0 0 auto; color: #f0b070; }
.review-entry__copy { display: flex; flex-direction: column; gap: 3px; }
.review-entry__copy strong { font-size: 16px; font-weight: 500; line-height: 24px; }
.review-entry__copy small { font-size: 12px; font-weight: 400; line-height: 20px; color: var(--td-text-color-secondary); white-space: nowrap; }
.review-entry--conversation { width: fit-content; min-width: 0; min-height: 28px; margin: 0 0 12px; padding: 3px 10px; border-color: var(--td-component-stroke); border-radius: var(--td-radius-round); background: var(--td-bg-color-container); backdrop-filter: none; }
.review-entry--conversation .review-entry__copy { gap: 0; }
.review-entry--conversation .review-entry__copy strong { font: var(--td-font-body-medium); }
.review-entry--conversation .review-entry__copy small { display: none; }
.review-entry--conversation:hover:not(:disabled), .review-entry--conversation:focus-visible { border-color: var(--td-brand-color); background: var(--td-bg-color-container); box-shadow: none; color: var(--td-brand-color); }
.review-entry:disabled { cursor: not-allowed; opacity: .5; }


.chat-composer__controls,
.conversation-composer__controls {
  display: flex;
  align-self: end;
  align-items: center;
  gap: 4px;
  min-width: 0;
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

.chat-composer__tools :deep(.picker-chip__label),
.conversation-composer__tools :deep(.picker-chip__label) {
  max-width: clamp(56px, 18vw, 120px);
}

.chat-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.chat-send,
.chat-voice {
  width: 28px;
  min-width: 28px;
  height: 28px;
  flex: 0 0 28px;
  padding: 0;
  border: 0;
  border-radius: var(--td-radius-large);
  transition: background-color 150ms ease, box-shadow 150ms ease, color 150ms ease, transform 150ms ease;
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
  background: transparent;
  box-shadow: none;
}

.chat-actions :deep(svg) {
  width: 16px;
  height: 16px;
}

.chat-voice__icon {
  display: block;
}

.chat-send:not(:disabled):hover,
.chat-send:not(:disabled):focus-visible {
  border: 0;
  background: var(--td-brand-color-active);
  box-shadow: 0 0 0 3px var(--td-brand-color-light), 0 8px 16px rgba(7,192,95,.28);
  transform: translateY(-1px);
}

.chat-voice:hover,
.chat-voice:focus-visible {
  border: 0;
  background: var(--td-brand-color-light);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--td-brand-color) 16%, transparent);
  color: var(--td-brand-color);
  transform: translateY(-1px);
}

.chat-send:disabled {
  opacity: .48;
  box-shadow: none;
}

.conversation {
  display: grid;
  grid-template-rows: auto minmax(0, 1fr) auto auto;
  box-sizing: border-box;
  height: 100%;
  min-height: 0;
  overflow: hidden;
  padding: 0;
}

.conversation > header {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
  padding-bottom: 8px;
  border-bottom: 1px solid rgba(0,0,0,.08);
}

.conversation--loading { grid-template-rows: auto minmax(0, 1fr) auto; }

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
  padding: 12px 0 16px;
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
  padding: 10px 12px;
  border: 1px solid rgba(255,255,255,.84);
  border-radius: var(--td-radius-extraLarge);
  background: rgba(255,255,255,.62);
  box-shadow: 0 4px 14px rgba(27,37,31,.06);
  backdrop-filter: blur(24px) saturate(112%);
  -webkit-backdrop-filter: blur(24px) saturate(112%);
}

.conversation-composer:focus-within {
  border-color: color-mix(in srgb, var(--td-brand-color) 42%, transparent);
  box-shadow: 0 4px 16px rgba(27,37,31,.08), 0 0 0 3px color-mix(in srgb, var(--td-brand-color) 12%, transparent);
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
  max-height: 124px;
  resize: none;
  padding: 4px 0 10px;
  border: 0;
  outline: 0;
  background: transparent;
  color: var(--td-text-color-primary);
  font: inherit;
  line-height: 1.6;
  overflow-y: hidden;
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
.message--assistant { display: block; margin-bottom: 20px; }
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
  .chat-page { padding: 16px 24px 20px; }
  .chat-landing { transform: translateY(-2vh); }
}

@media (max-width: 640px) {
  .chat-page { padding: 12px 16px 16px; }
  .chat-landing h1 { margin-bottom: 26px; font-size: 31px; }
  .message__bubble { max-width: 88%; }
  .message--assistant .message__bubble { max-width: none; }
  .conversation-composer {
    gap: 6px;
    padding: 12px;
  }
}
</style>
