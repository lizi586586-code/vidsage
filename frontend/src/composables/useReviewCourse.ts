import { computed, onUnmounted, ref, watch, type Ref } from 'vue'
import { MessagePlugin } from 'tdesign-vue-next'
import { fetchVideoList, isVideoInitiallyAvailable } from '@/api/videohub'
import { fetchOpenMAICClassroomStatus, prepareOpenMAICClassroom, type OpenMAICClassroomStatus } from '@/api/videohub/openmaicClassroom'
import { isReviewPending, reviewInstruction, reviewRequirement, reviewSourceKey } from '@/api/videohub/reviewMaterials'
import { readRecentlyPlayedCourseIds } from '@/api/videohub/recentlyPlayedCourses'
import type { VideoData } from '@/types/videohub'

export function useReviewCourse(options: {
  userId: Ref<string>
  storageKey: Ref<string>
  contextKey?: Ref<string>
  busy: Ref<boolean>
  onStart: (instruction: string, video: VideoData) => Promise<void>
}) {
  const choosingCourse = ref(false)
  const loadingCourses = ref(false)
  const courseError = ref('')
  const courses = ref<VideoData[]>([])
  const recentCourseIds = ref<string[]>([])
  const selectedCourseId = ref('')
  const preparingClassroom = ref(false)
  const classroomResult = ref<OpenMAICClassroomStatus | null>(null)
  const classroomVideo = ref<VideoData | null>(null)
  const classroomGenerating = computed(() => isReviewPending(classroomResult.value))
  let revision = 0
  let disposed = false
  function isMissingTask(error: unknown) {
    const response = error as { status?: number; response?: { status?: number } }
    return (response?.status || response?.response?.status) === 404
  }
  function forgetMissingTask(key: string, requestId: string) {
    try {
      if (localStorage.getItem(key) === requestId) localStorage.removeItem(key)
    } catch { /* No storage. */ }
  }
  onUnmounted(() => { disposed = true; revision += 1 })

  watch(() => [options.storageKey.value, options.contextKey?.value], async ([key]) => {
    const turn = ++revision
    choosingCourse.value = false
    loadingCourses.value = false
    preparingClassroom.value = false
    selectedCourseId.value = ''
    classroomResult.value = null
    classroomVideo.value = null
    if (!options.userId.value) return
    let requestId = ''
    try { requestId = localStorage.getItem(key!) || '' } catch { return }
    if (!requestId) return
    preparingClassroom.value = true
    try {
      const task = await fetchOpenMAICClassroomStatus(requestId)
      if (!disposed && revision === turn) classroomResult.value = task
    } catch (error) {
      if (!disposed && revision === turn && isMissingTask(error)) forgetMissingTask(key!, requestId)
      // Restore only authorized backend results, never cached URLs.
    }
    finally { if (!disposed && revision === turn) preparingClassroom.value = false }
  }, { immediate: true })

  async function askCourse() {
    if (options.busy.value || preparingClassroom.value) return
    choosingCourse.value = true
    selectedCourseId.value = ''
    recentCourseIds.value = readRecentlyPlayedCourseIds(options.userId.value)
    await loadCourses()
  }

  async function loadCourses() {
    if (loadingCourses.value) return
    const turn = revision
    loadingCourses.value = true
    courseError.value = ''
    courses.value = []
    try {
      const videos = await fetchVideoList()
      if (disposed || turn !== revision) return
      courses.value = videos.filter(video => isVideoInitiallyAvailable({
        status: video.status, file_url: video.video_url,
        thumbnail_url: video.poster_url, initially_available: video.initiallyAvailable,
      }))
    } catch {
      if (turn === revision) courseError.value = '课程列表暂时无法加载，请重试'
    } finally { if (turn === revision) loadingCourses.value = false }
  }

  function cancelCourse() {
    if (preparingClassroom.value) return
    choosingCourse.value = false
    selectedCourseId.value = ''
  }

  async function startForVideo(video: VideoData) {
    if (disposed || options.busy.value || preparingClassroom.value || !video.id || video.id === '__global__') return
    preparingClassroom.value = true
    const turn = revision
    const key = options.storageKey.value
    try {
      // Verify persisted identity even if restoration failed, before preparing another task.
      let existingRequestId = isReviewPending(classroomResult.value) ? classroomResult.value!.request_id : ''
      if (!classroomResult.value) {
        try { existingRequestId = localStorage.getItem(key) || '' } catch { /* No storage. */ }
      }
      if (existingRequestId) {
        let status: OpenMAICClassroomStatus | null
        try { status = await fetchOpenMAICClassroomStatus(existingRequestId) }
        catch (error) {
          if (!isMissingTask(error)) throw error
          if (disposed || turn !== revision) return
          forgetMissingTask(key, existingRequestId)
          status = null
        }
        if (disposed || turn !== revision) return
        classroomResult.value = status
        if (status && isReviewPending(status)) {
          let sourceId = classroomVideo.value?.id || ''
          try { sourceId ||= localStorage.getItem(reviewSourceKey(options.userId.value, status.request_id)) || '' } catch { /* No storage. */ }
          // A queued request has not been claimed by the tool. Reuse its identity on retry.
          if (status.generation_status === 'queued' && sourceId === video.id) {
            classroomVideo.value = video
            choosingCourse.value = false
            await options.onStart(reviewInstruction(status.request_id), video)
            return
          }
          MessagePlugin.info('复习材料正在生成，请等待进度更新')
          return
        }
      }
      const task = await prepareOpenMAICClassroom({ video_id: video.id, requirement: reviewRequirement })
      if (disposed || turn !== revision) return
      classroomResult.value = task
      classroomVideo.value = video
      choosingCourse.value = false
      try {
        localStorage.setItem(key, task.request_id)
        localStorage.setItem(reviewSourceKey(options.userId.value, task.request_id), video.id)
      } catch { /* The active view works without browser persistence. */ }
      await options.onStart(reviewInstruction(task.request_id), video)
    } catch {
      if (turn === revision) MessagePlugin.error('复习材料任务准备失败，请检查课程资料后重试')
    } finally { if (turn === revision) preparingClassroom.value = false }
  }

  async function confirmCourse() {
    if (!choosingCourse.value || loadingCourses.value || courseError.value) return
    const video = courses.value.find(course => course.id === selectedCourseId.value)
    if (video) await startForVideo(video)
  }

  function updateStatus(status: OpenMAICClassroomStatus) {
    if (classroomResult.value?.request_id === status.request_id) classroomResult.value = status
  }

  return { choosingCourse, loadingCourses, courseError, courses, recentCourseIds, selectedCourseId, preparingClassroom,
    classroomResult, classroomVideo, classroomGenerating, askCourse, loadCourses, cancelCourse, confirmCourse, startForVideo, updateStatus }
}
