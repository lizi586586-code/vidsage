<template>
  <main>
    <ReviewSuggestions :disabled="preparingClassroom" @select="askCourse" />
    <ReviewCourseChooser v-if="choosingCourse" v-model="selectedCourseId" :courses="courses" :loading="loadingCourses"
      :error="courseError" :busy="preparingClassroom" @confirm="confirmCourse" @cancel="cancelCourse" @retry="loadCourses" />
    <section v-for="(message, index) in messages" :key="index">
      <p class="user-copy">{{ visibleReviewQuestion(message.instruction) }}</p>
      <ReviewMaterialReply v-if="isFirstReviewRequest(messages, index)" :request-id="message.id" user-id="test-user" :video="message.video" @status="updateStatus" />
    </section>
    <section v-if="gallery" class="gallery">
      <ReviewMaterialReply v-for="id in ['running', 'unknown', 'failed', 'ready']" :key="id" :request-id="id" user-id="test-user" :video="galleryVideo" />
    </section>
  </main>
</template>
<script setup lang="ts">
import { ref } from 'vue'
import ReviewSuggestions from '../../src/components/videohub/ReviewSuggestions.vue'
import ReviewCourseChooser from '../../src/components/videohub/ReviewCourseChooser.vue'
import ReviewMaterialReply from '../../src/components/videohub/ReviewMaterialReply.vue'
import { useReviewCourse } from '../../src/composables/useReviewCourse'
import { isFirstReviewRequest, visibleReviewQuestion } from '../../src/api/videohub/reviewMaterials'
import type { VideoData } from '../../src/types/videohub'
const messages = ref<{ id: string; sender: string; text: string; instruction: string; video: VideoData }[]>([])
const gallery = new URLSearchParams(location.search).has('gallery')
const galleryVideo = { id: 'course-a', title: 'AI认知——什么是MCP和Skills' } as VideoData
const contextKey = ref('chat-a')
const review = useReviewCourse({
  userId: ref('test-user'), storageKey: ref('test-review-latest'), contextKey, busy: ref(false),
  onStart: async (instruction, video) => {
    const id = instruction.match(/复制为 ([A-Za-z0-9_-]+)/)![1]!
    messages.value.push({ id, sender: 'user', text: instruction, instruction, video })
    window.dispatchEvent(new CustomEvent('review-test-start', { detail: { instruction, videoId: video.id } }))
  },
})
const { preparingClassroom, choosingCourse, selectedCourseId, courses, loadingCourses, courseError,
  askCourse, confirmCourse, cancelCourse, loadCourses, updateStatus } = review
Object.assign(window, { reviewTest: { ...review, contextKey, messages } })
</script>
<style>
body { margin: 0; background: var(--td-bg-color-page); font-family: -apple-system, 'PingFang SC', sans-serif; }
main { max-width: 760px; margin: 40px auto; padding: 16px; }
section { margin-top: 24px; }
.user-copy { text-align: right; }
.gallery { display: grid; gap: 24px; }
</style>
