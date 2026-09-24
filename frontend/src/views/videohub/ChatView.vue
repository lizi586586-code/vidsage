<template>
  <main class="chat-page">
    <section v-if="!activeSession" class="chat-landing">
      <div class="chat-landing__inner">
        <h1>Learn with Vidsage</h1>
        <form class="chat-composer" @submit.prevent="startSession">
          <textarea v-model="question" rows="4" placeholder="问问你的视频知识库，例如：帮我总结最近看过的 AI 提示词视频" @keydown.enter.exact.prevent="startSession" />
          <footer>
            <div class="chat-composer__tools">
              <t-button class="chat-tool" variant="text" type="button" aria-label="添加附件" @click="showToolMessage('添加附件')">
                <t-icon name="attach" /><span>添加附件</span>
              </t-button>
              <t-button class="chat-tool" variant="text" type="button" aria-label="语音输入" @click="showToolMessage('语音输入')">
                <t-icon name="microphone" /><span>语音输入</span>
              </t-button>
              <VideohubAgentPicker appearance="tool" tool-label="自动路由" />
            </div>
            <t-button class="chat-send" type="submit" shape="square" :disabled="!question.trim()">
              <t-icon name="arrow-up" />
            </t-button>
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
      <header><t-button variant="text" @click="back"><t-icon name="chevron-left" /> 返回</t-button><div><strong>{{ activeSession.title }}</strong><span>{{ activeSession.scope === 'video' ? activeSession.videoTitle || '指定视频问答' : '全局视频问答' }}</span></div></header>
      <div ref="messageArea" class="conversation__messages">
        <article v-for="(message, messageIndex) in activeSession.messages" :key="message.id" :class="['message', `message--${message.sender}`]">
          <div class="message__bubble">
            <AgentStreamDisplay
              v-if="message.sender === 'assistant'"
              :session="toNativeAgentSession(message)"
              :session-id="activeSession.id"
              :user-query="questionForMessage(messageIndex) || lastUserQuery"
              :show-video-title="shouldShowVideoTitle(messageIndex)"
              :hydrate-protected-images="false"
              @video-navigate="navigateToEvidence"
            />
            <p v-else>{{ message.text }}</p>
          </div>
        </article>
        <div v-if="isGenerating && !streamingAssistantVisible" class="generating"><t-loading size="small" /> AI 正在整合视频知识回答中...</div>
      </div>
      <form class="conversation-composer" @submit.prevent="continueSession">
        <div class="conversation-composer__body">
          <textarea v-model="followUp" rows="2" placeholder="继续追问" :disabled="isGenerating" @keydown.enter.exact.prevent="continueSession" />
          <div class="conversation-composer__tools">
            <t-button class="chat-tool" variant="text" type="button" aria-label="添加附件" @click="showToolMessage('添加附件')">
              <t-icon name="attach" /><span>添加附件</span>
            </t-button>
            <t-button class="chat-tool" variant="text" type="button" aria-label="语音输入" @click="showToolMessage('语音输入')">
              <t-icon name="microphone" /><span>语音输入</span>
            </t-button>
            <VideohubAgentPicker appearance="tool" tool-label="自动路由" />
          </div>
        </div>
        <t-button class="chat-send" type="submit" shape="square" :disabled="isGenerating || !followUp.trim()">
          <t-icon name="arrow-up" />
        </t-button>
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
import { shouldShowVideoCitationTitle } from '@/utils/citationMarkdown'
import { shouldAutoRoute } from '@/api/videohub/chatRequest'

const router = useRouter()
const route = useRoute()
const settingsStore = useSettingsStore()
const question = ref(''), followUp = ref(''), sessions = ref<ChatSession[]>([]), activeSession = ref<ChatSession | null>(null)
const loadingSessions = ref(true), isGenerating = ref(false), messageArea = ref<HTMLElement | null>(null)
const streamingAssistantId = ref('')
const lastUserQuery = ref('')
const sessionListReady = ref(false)
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
function shouldShowVideoTitle(index: number): boolean {
  const message = activeSession.value?.messages[index]
  if (!message || message.sender !== 'assistant') return false
  // A video-scoped session already identifies the source in its header and
  // request context. Do not repeat the title beside every timestamp.
  if (activeSession.value?.scope === 'video') return false
  return shouldShowVideoCitationTitle(questionForMessage(index), message.knowledge_references)
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

function materializePendingSession(pendingSession: ChatSession, createdSession: ChatSession) {
  const materialized = { ...createdSession, messages: pendingSession.messages }
  sessions.value = sessions.value.map(item => item.id === pendingSession.id ? materialized : item)
  if (activeSession.value?.id === pendingSession.id) activeSession.value = materialized
  return materialized
}
async function startSession() {
  const value = question.value.trim(); if (!value || isGenerating.value) return
  lastUserQuery.value = value
  question.value = ''
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
    })
    sessions.value = sessions.value.map(item => item.id === session.id || item.id === created.id ? created : item)
    activeSession.value = created
    window.dispatchEvent(new CustomEvent('weknora:session-refresh'))
    await router.replace({ path: '/platform/ai-chat', query: { session: created.id } })
  } catch (error) {
    session.messages.push({ id: `error-${Date.now()}`, sender: 'assistant', text: error instanceof Error ? error.message : '问答生成失败，请稍后重试', timestamp: '刚刚' })
  } finally { isGenerating.value = false; streamingAssistantId.value = ''; await scrollBottom() }
}
async function continueSession() {
  const value = followUp.value.trim(); if (!value || isGenerating.value || !activeSession.value) return
  const target = activeSession.value
  lastUserQuery.value = value
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
    })
    activeSession.value = updated
    sessions.value = [updated, ...sessions.value.filter(item => item.id !== updated.id)]
  } catch (error) {
    target.messages.push({ id: `error-${Date.now()}`, sender: 'assistant', text: error instanceof Error ? error.message : '问答生成失败，请稍后重试', timestamp: '刚刚' })
  } finally { isGenerating.value = false; streamingAssistantId.value = ''; await scrollBottom() }
}
async function openSession(session: ChatSession) {
  activeSession.value = session
  if (!session.messages.length) {
    try { activeSession.value = await loadChatSession(session) }
    catch (error) { MessagePlugin.error(error instanceof Error ? error.message : '会话加载失败') }
  }
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
function back() {
  activeSession.value = null
  followUp.value = ''
  isGenerating.value = false
  void router.replace('/platform/ai-chat')
}
function navigateToEvidence(videoId: string, seconds: number) {
  if (!videoId || !Number.isFinite(seconds) || seconds < 0) return
  router.push(`/platform/videos/${videoId}?t=${Math.floor(seconds)}`)
}
watch(sessionQueryId, async (sessionId) => {
  if (!sessionListReady.value) return
  if (!sessionId) {
    activeSession.value = null
    return
  }
  if (activeSession.value?.id !== sessionId) await openSessionFromRoute(sessionId)
})

onMounted(async () => {
  try {
    sessions.value = await fetchSessions()
    if (sessionQueryId.value) await openSessionFromRoute(sessionQueryId.value)
  } finally {
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
  min-height: 100%;
  height: 100%;
  overflow: hidden;
  padding: 28px 48px 38px;
  color: var(--td-text-color-primary);
  background:
    linear-gradient(120deg, rgba(232, 237, 234, .96), rgba(248, 249, 248, .94) 46%, rgba(235, 240, 237, .96));
}

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
  backdrop-filter: blur(26px) saturate(118%);
  -webkit-backdrop-filter: blur(26px) saturate(118%);
}

.chat-landing,
.conversation {
  width: min(920px, 100%);
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
}

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
  background: rgba(0,0,0,.05);
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
  box-shadow: 0 6px 14px rgba(7,192,95,.24);
}

.chat-send:hover {
  background: var(--td-brand-color-active);
}

.chat-send:disabled {
  opacity: .48;
  box-shadow: none;
}

.conversation {
  display: grid;
  grid-template-rows: auto 1fr auto;
  min-height: calc(100% - 4px);
  padding-bottom: 8px;
}

.conversation > header {
  display: flex;
  align-items: center;
  gap: 12px;
  padding-bottom: 16px;
  border-bottom: 1px solid rgba(0,0,0,.08);
}

.conversation > header div {
  display: grid;
}

.conversation > header span {
  color: var(--td-text-color-secondary);
  font-size: 12px;
}

.conversation__messages {
  overflow-y: auto;
  padding: 24px 4px;
}

.conversation-composer {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: end;
  padding: 10px 12px 10px 16px;
  border: 1px solid rgba(255,255,255,.84);
  border-radius: var(--td-radius-extraLarge);
  background: rgba(255,255,255,.62);
  box-shadow: 0 12px 32px rgba(27,37,31,.08);
  backdrop-filter: blur(24px) saturate(112%);
  -webkit-backdrop-filter: blur(24px) saturate(112%);
}

.conversation-composer__body {
  min-width: 0;
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
}

.message {
  display: flex;
  margin-bottom: 20px;
}

.message--user { justify-content: flex-end; }
.message--assistant { display: block; }
.message__bubble { min-width: 0; max-width: 72%; }
.message--assistant .message__bubble { width: 100%; max-width: none; }
.message__bubble > p { margin: 0; padding: 11px 14px; border-radius: var(--td-radius-large); background: rgba(255,255,255,.54); line-height: 1.7; white-space: pre-wrap; }
.message--assistant .message__bubble > p { background: transparent; }
.message--assistant :deep(.agent-stream-display) { width: 100%; }
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
  .chat-tool span { display: none; }
  .message__bubble { max-width: 88%; }
  .message--assistant .message__bubble { max-width: none; }
}
</style>
