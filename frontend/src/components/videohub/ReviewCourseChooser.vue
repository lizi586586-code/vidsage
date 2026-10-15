<template>
  <section class="review-course-chooser" aria-label="确认复习课程">
    <p class="review-course-chooser__title">你想巩固学习哪一门课程？</p>
    <div v-if="loading" role="status"><t-loading size="small" /> 正在获取课程...</div>
    <div v-else-if="error" role="alert">{{ error }} <t-button size="small" variant="text" @click="$emit('retry')">重试</t-button></div>
    <p v-else-if="!courses.length" class="review-course-chooser__empty">暂无可复习的课程，请先上传课程视频。</p>
    <div v-else class="review-course-chooser__choices" role="radiogroup" aria-label="选择课程">
      <button v-for="course in recentCourses" :key="course.id" class="review-course-chooser__choice" :class="{ 'is-selected': modelValue === course.id }" type="button" role="radio" :aria-checked="modelValue === course.id" :disabled="busy" @click="selectRecent(course.id)">
        <span class="review-course-chooser__radio" aria-hidden="true"></span>
        <span class="review-course-chooser__label">{{ course.title }}</span>
      </button>
      <button class="review-course-chooser__choice" :class="{ 'is-selected': otherSelected }" type="button" role="radio" :aria-checked="otherSelected" :disabled="busy" @click="openOther">
        <span class="review-course-chooser__radio" aria-hidden="true"></span>
        <span class="review-course-chooser__label">其他</span>
      </button>
      <t-select v-if="otherSelected" class="review-course-chooser__other" :value="modelValue" :options="otherCourseOptions" filterable clearable placeholder="搜索并选择其他课程" aria-label="其他课程" :disabled="busy" @change="selectCourse" />
    </div>
    <div class="review-course-chooser__actions">
      <t-button size="small" variant="text" :disabled="busy" @click="$emit('cancel')">取消</t-button>
      <t-button size="small" theme="primary" :disabled="!modelValue || loading || !!error || busy" :loading="busy" @click="$emit('confirm')">确认并生成</t-button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SelectValue } from 'tdesign-vue-next'
import type { VideoData } from '@/types/videohub'
const props = defineProps<{ courses: VideoData[]; recentCourseIds?: string[]; modelValue: string; loading: boolean; error: string; busy: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [id: string]; confirm: []; cancel: []; retry: [] }>()
const recentCourses = computed(() => {
  const byId = new Map(props.courses.map(course => [course.id, course]))
  return (props.recentCourseIds || []).map(id => byId.get(id)).filter((course): course is VideoData => Boolean(course)).slice(0, 3)
})
const otherCourses = computed(() => props.courses.filter(course => !recentCourses.value.some(recent => recent.id === course.id)))
const choosingOther = ref(false)
const otherSelected = computed(() => choosingOther.value || (Boolean(props.modelValue) && !recentCourses.value.some(course => course.id === props.modelValue)))
const otherCourseOptions = computed(() => otherCourses.value.map(video => ({ value: video.id, label: video.title })))
function selectRecent(id: string) { choosingOther.value = false; emit('update:modelValue', id) }
function openOther() { choosingOther.value = true; if (recentCourses.value.some(course => course.id === props.modelValue)) emit('update:modelValue', '') }
function selectCourse(value: SelectValue) { emit('update:modelValue', typeof value === 'string' ? value : '') }
</script>

<style scoped>
.review-course-chooser { min-width: 0; padding: 20px 20px 16px; border: 1px solid rgba(255,255,255,.9); border-radius: var(--td-radius-extraLarge); background: rgba(255,255,255,.42); backdrop-filter: blur(26px) saturate(112%); -webkit-backdrop-filter: blur(26px) saturate(112%); font: var(--td-font-body-medium); }
.review-course-chooser__title { margin: 0 0 16px; color: var(--td-text-color-primary); font-size: 14px; font-weight: 500; line-height: 22px; }
.review-course-chooser__choices { display: grid; gap: 8px; }
.review-course-chooser__choice { display: flex; align-items: center; gap: 12px; min-height: 44px; padding: 10px 12px; border: 1px solid rgba(255,255,255,.9); border-radius: var(--td-radius-large); color: var(--td-text-color-primary); background: rgba(255,255,255,.42); font: var(--td-font-body-medium); cursor: pointer; text-align: left; transition: background .2s ease; }
.review-course-chooser__choice:hover:not(:disabled) { background: rgba(255,255,255,.58); }
.review-course-chooser__choice.is-selected { background: var(--td-brand-color-light); }
.review-course-chooser__choice:focus-visible { outline: 2px solid var(--td-brand-color-focus); outline-offset: 1px; }
.review-course-chooser__choice:disabled { color: var(--td-text-color-disabled); cursor: not-allowed; }
.review-course-chooser__radio { display: grid; width: 20px; height: 20px; flex: none; place-items: center; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-circle); background: rgba(255,255,255,.52); }
.review-course-chooser__choice.is-selected .review-course-chooser__radio { border-color: var(--td-brand-color); }
.review-course-chooser__choice.is-selected .review-course-chooser__radio::after { width: 10px; height: 10px; border-radius: var(--td-radius-circle); background: var(--td-brand-color); content: ''; }
.review-course-chooser__label { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.review-course-chooser__other { margin: 2px 0 8px 32px; max-width: calc(100% - 32px); }
.review-course-chooser__empty { color: var(--td-text-color-secondary); }
.review-course-chooser__actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 12px; padding-top: 12px; border-top: 1px solid color-mix(in srgb, var(--td-component-stroke) 72%, transparent); }
@media (max-width: 640px) { .review-course-chooser { padding: 16px; } }
</style>
