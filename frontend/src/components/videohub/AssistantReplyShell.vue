<template>
  <div class="assistant-reply-shell">
    <img class="assistant-reply-shell__avatar" :src="assistantAvatarUrl" alt="AI 助手" />
    <div class="assistant-reply-shell__body">
      <time v-if="timestamp" class="assistant-reply-shell__time">{{ timestamp }}</time>
      <div class="assistant-reply-shell__content"><slot /></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import assistantAvatarUrl from '@/assets/img/vidsage-ai-assistant.png'

defineProps<{ timestamp?: string }>()
</script>

<style scoped>
.assistant-reply-shell {
  display: grid;
  grid-template-columns: 32px minmax(0, 1fr);
  gap: 10px;
  width: 100%;
  min-width: 0;
}

.assistant-reply-shell__avatar {
  box-sizing: border-box;
  display: block;
  width: 32px;
  height: 32px;
  object-fit: cover;
  border: 1px solid color-mix(in srgb, var(--td-bg-color-container) 88%, transparent);
  border-radius: var(--td-radius-circle);
  background: var(--td-bg-color-secondarycontainer);
}

.assistant-reply-shell__body,
.assistant-reply-shell__content {
  min-width: 0;
}

.assistant-reply-shell__time {
  display: block;
  margin-bottom: 4px;
  color: var(--td-text-color-secondary);
  font: var(--td-font-body-small);
  line-height: 20px;
}

@media (max-width: 640px) {
  .assistant-reply-shell {
    grid-template-columns: 28px minmax(0, 1fr);
    gap: 8px;
  }

  .assistant-reply-shell__avatar {
    width: 28px;
    height: 28px;
  }
}
</style>
