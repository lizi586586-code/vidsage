<template>
  <div class="review-suggestions" aria-label="学习建议">
    <button v-for="item in items" :key="item" type="button" :disabled="disabled" @click="$emit('select', item)">{{ item }}</button>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{ items?: string[]; disabled?: boolean }>(), {
  items: () => ['巩固学习', '总结核心观点', '给出三个可执行建议'], disabled: false,
})
defineEmits<{ select: [item: string] }>()
</script>

<style scoped>
.review-suggestions { display: flex; gap: 6px; flex-wrap: wrap; min-width: 0; }
.review-suggestions button {
  display: inline-flex; flex: 0 0 auto; align-items: center; justify-content: center;
  box-sizing: border-box; min-height: 20px; padding: 0 10px;
  border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-round);
  background: var(--td-bg-color-container); color: var(--td-text-color-secondary);
  cursor: pointer; font: var(--td-font-body-small);
}
.review-suggestions button:hover, .review-suggestions button:focus-visible {
  color: var(--td-brand-color); border-color: var(--td-brand-color); outline-color: var(--td-brand-color);
}
.review-suggestions button:disabled { cursor: default; opacity: .48; }
</style>
