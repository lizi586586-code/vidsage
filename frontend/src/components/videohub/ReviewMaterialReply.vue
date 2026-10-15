<template>
  <div class="review-material-reply">
    <p class="review-material-reply__copy">{{ reviewAssistantText }}</p>
    <component :is="presentation.href ? 'a' : 'div'" class="review-card" :data-state="presentation.state"
      :href="presentation.href || undefined" :target="presentation.href ? '_blank' : undefined"
      :rel="presentation.href ? 'noopener noreferrer' : undefined" :aria-busy="!inaccessible && (isReviewPending(task) || !task)">
      <div class="review-card__head">
        <div class="review-card__text">
          <h2 role="status" aria-live="polite" aria-atomic="true">{{ presentation.title }}</h2>
          <p class="review-card__course">{{ videoTitle || '正在获取课程名称' }}</p>
        </div>
        <div class="review-card__document" aria-hidden="true">
          <svg viewBox="0 0 24 24"><path d="M14 3H6a1 1 0 0 0-1 1v16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V8Z" /><path d="M14 3v5h5M9 12h6M9 16h6" /></svg>
        </div>
      </div>
    </component>
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { fetchVideoDetail } from '@/api/videohub'
import { fetchOpenMAICClassroomStatus, type OpenMAICClassroomStatus } from '@/api/videohub/openmaicClassroom'
import { isReviewPending, reviewAssistantText, reviewCardPresentation, reviewSourceKey } from '@/api/videohub/reviewMaterials'
import type { VideoData } from '@/types/videohub'

const props = defineProps<{ requestId: string; userId: string; initialTask?: OpenMAICClassroomStatus | null; video?: VideoData | null }>()
const emit = defineEmits<{ status: [task: OpenMAICClassroomStatus] }>()
const task = ref<OpenMAICClassroomStatus | null>(null)
const videoTitle = ref('')
const inaccessible = ref(false)
const presentation = computed(() => inaccessible.value
  ? { state: 'failed', title: '复习材料暂不可访问', href: '' }
  : reviewCardPresentation(task.value))
let timer: ReturnType<typeof setTimeout> | undefined
let revision = 0
let pollRevision = 0
let disposed = false
onUnmounted(() => { disposed = true; revision += 1; if (timer) clearTimeout(timer) })

watch(() => [props.requestId, props.userId], async () => {
  const turn = ++revision
  if (timer) clearTimeout(timer)
  task.value = props.initialTask?.request_id === props.requestId ? props.initialTask : null
  inaccessible.value = false
  videoTitle.value = ''
  if (!props.userId || !props.requestId) return
  let videoId = ''
  try { videoId = localStorage.getItem(reviewSourceKey(props.userId, props.requestId)) || '' } catch { /* No storage. */ }
  if (!videoId && props.video?.id !== '__global__') videoId = props.video?.id || ''
  if (props.video?.id === videoId) videoTitle.value = props.video.title
  if (videoId) void fetchVideoDetail(videoId).then(video => {
    if (!disposed && turn === revision) videoTitle.value = video.title
  }).catch(() => {
    if (!disposed && turn === revision && !videoTitle.value) videoTitle.value = '课程名称暂不可用'
  })
  else videoTitle.value = '课程名称暂不可用'
  if (!task.value || isReviewPending(task.value)) await refresh(turn)
}, { immediate: true })

watch(() => props.initialTask, status => {
  if (!status || status.request_id !== props.requestId || task.value?.generation_status === 'cancelled') return
  task.value = status
  if (!isReviewPending(status)) {
    pollRevision += 1 // Invalidate an older in-flight poll without dropping the course title.
    if (timer) clearTimeout(timer)
  }
}, { flush: 'sync' })

async function refresh(turn: number) {
  const poll = pollRevision
  try {
    const status = await fetchOpenMAICClassroomStatus(props.requestId)
    if (disposed || turn !== revision || poll !== pollRevision) return
    task.value = status
    emit('status', status)
  } catch (error) {
    if (disposed || turn !== revision || poll !== pollRevision) return
    const status = (error as { status?: number; response?: { status?: number } })?.status
      || (error as { response?: { status?: number } })?.response?.status
    if (status === 401 || status === 403 || status === 404) inaccessible.value = true
    // A transient network error does not replace a verified result.
  }
  if (!disposed && turn === revision && poll === pollRevision && !inaccessible.value && (!task.value || isReviewPending(task.value))) {
    timer = setTimeout(() => void refresh(turn), 5000)
  }
}
</script>

<style scoped>
.review-material-reply { width: 100%; min-width: 0; }
.review-material-reply__copy { margin: 0 0 16px; font: var(--td-font-body-medium); line-height: 24px; color: var(--td-text-color-primary); }
.review-card {
  display: block; position: relative; isolation: isolate; overflow: hidden;
  border: 1px solid var(--td-bg-color-container); border-radius: var(--td-radius-extraLarge);
  background: linear-gradient(115deg, var(--td-brand-color-light), color-mix(in srgb, var(--td-bg-color-container) 74%, var(--td-brand-color-light)));
  text-decoration: none; color: var(--td-text-color-primary);
}
.review-card__head { display: flex; align-items: center; justify-content: space-between; gap: 16px; box-sizing: border-box; padding: 20px; min-height: 98px; }
.review-card__text { min-width: 0; }
.review-card h2 { font: var(--td-font-title-medium); font-weight: 600; margin: 0 0 4px; overflow-wrap: anywhere; }
.review-card__course { font: var(--td-font-body-small); color: var(--td-text-color-secondary); margin: 0; overflow-wrap: anywhere; }
.review-card__document { color: var(--td-brand-color); width: 64px; height: 56px; display: grid; place-items: center; flex-shrink: 0; position: relative; }
.review-card__document::before { content: ''; position: absolute; inset: 4px 8px; transform: rotate(10deg); background: color-mix(in srgb, var(--td-brand-color) 10%, transparent); border-radius: var(--td-radius-large); }
.review-card__document svg { position: relative; width: 48px; height: 48px; fill: none; stroke: currentColor; stroke-width: 1.3; stroke-linecap: round; stroke-linejoin: round; }
.review-card[data-state=running]::after, .review-card[data-state=unknown]::after { content: ''; position: absolute; inset: 0; pointer-events: none; background: linear-gradient(100deg, transparent 15%, color-mix(in srgb, var(--td-bg-color-container) 72%, transparent) 50%, transparent 85%); animation: review-shimmer 1.8s ease-in-out infinite; transform: translateX(-100%); }
.review-card[data-state=failed], .review-card[data-state=cancelled] { background: var(--td-bg-color-secondarycontainer); }
.review-card[data-state=failed] .review-card__document, .review-card[data-state=cancelled] .review-card__document { color: var(--td-text-color-placeholder); }
.review-card[data-state=failed] .review-card__document::before, .review-card[data-state=cancelled] .review-card__document::before { background: color-mix(in srgb, var(--td-component-stroke) 60%, transparent); }
a.review-card:hover, a.review-card:focus-visible { border-color: var(--td-brand-color); outline-color: var(--td-brand-color); }
@keyframes review-shimmer { to { transform: translateX(100%); } }
@media (max-width: 440px) { .review-card__head { padding: 16px; } .review-card__document { width: 48px; } }
@media (prefers-reduced-motion: reduce) { .review-card::after { animation: none !important; display: none; } }
</style>
