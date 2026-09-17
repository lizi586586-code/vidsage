<template>
  <div ref="rootRef" class="videohub-agent-picker">
    <button
      type="button"
      class="picker-chip"
      :aria-label="t('videohub.agentPicker.title')"
      @click.stop="toggleDropdown"
    >
      <t-icon name="precise-search" />
      <span class="picker-chip__label">{{ currentAgentName }}</span>
      <t-icon name="chevron-down" :class="{ 'picker-chip__icon--open': dropdownVisible }" />
    </button>
    <Teleport to="body">
      <div
        v-if="dropdownVisible"
        class="picker-dropdown"
        :style="dropdownStyle"
        role="listbox"
        @click.stop
      >
        <button
          v-for="agent in displayAgents"
          :key="agent.id"
          type="button"
          :class="['picker-item', { 'picker-item--active': agent.id === currentAgentId }]"
          @click.stop="onSelect(agent)"
        >
          <span class="picker-item__name">{{ agent.name }}</span>
          <t-icon v-if="agent.id === currentAgentId" name="check" class="picker-item__check" />
        </button>
      </div>
    </Teleport>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, nextTick } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsStore } from '@/stores/settings'
import { useChatResourcesStore } from '@/stores/chatResources'
import { BUILTIN_QUICK_ANSWER_ID } from '@/api/agent'

const CUSTOM_AGENT_ID = '6f3691c2-8d15-48f8-b1f4-dfadd222ca53'
const ALLOWED_IDS = [BUILTIN_QUICK_ANSWER_ID, CUSTOM_AGENT_ID] as const

interface DisplayAgent {
  id: string
  name: string
  agentMode?: string
  sourceTenantId?: string | null
}

const { t } = useI18n()
const settingsStore = useSettingsStore()
const chatResources = useChatResourcesStore()

const dropdownVisible = ref(false)
const rootRef = ref<HTMLElement | null>(null)
const dropdownStyle = ref<Record<string, string>>({})

const currentAgentId = computed(() => settingsStore.selectedAgentId || BUILTIN_QUICK_ANSWER_ID)

const displayAgents = computed<DisplayAgent[]>(() => {
  return ALLOWED_IDS.map(id => {
    if (id === BUILTIN_QUICK_ANSWER_ID) {
      return { id, name: t('videohub.agentPicker.quickAnswer'), agentMode: 'quick-answer' }
    }
    const apiAgent = chatResources.agents.find(a => a.id === id)
    if (apiAgent) {
      return {
        id,
        name: apiAgent.name,
        agentMode: apiAgent.config?.agent_mode,
        sourceTenantId: apiAgent.tenant_id ? String(apiAgent.tenant_id) : null,
      }
    }
    return { id, name: t('videohub.agentPicker.customAgent'), sourceTenantId: null }
  })
})

const currentAgentName = computed(() => {
  const current = displayAgents.value.find(a => a.id === currentAgentId.value)
  return current?.name || t('videohub.agentPicker.quickAnswer')
})

const emit = defineEmits<{ select: [agentId: string] }>()

async function ensureAgentMetadata() {
  try {
    if (chatResources.agents.length === 0) {
      await chatResources.ensureAgents()
    }
  } catch {
    // best-effort: 兜底名称已在 displayAgents 里处理
  }
}

function computeDropdownStyle() {
  if (!rootRef.value) return
  const rect = rootRef.value.getBoundingClientRect()
  dropdownStyle.value = {
    position: 'fixed',
    bottom: `${window.innerHeight - rect.top + 4}px`,
    left: `${rect.left}px`,
    zIndex: '6000',
  }
}

async function toggleDropdown() {
  if (dropdownVisible.value) {
    dropdownVisible.value = false
    return
  }
  await nextTick()
  computeDropdownStyle()
  dropdownVisible.value = true
}

function onDocumentClick(e: MouseEvent) {
  if (!dropdownVisible.value) return
  const target = e.target as Node
  if (rootRef.value?.contains(target)) return
  // Teleport 到 body 的下拉也需要检查
  const dropdown = document.querySelector('.picker-dropdown')
  if (dropdown?.contains(target)) return
  dropdownVisible.value = false
}

onMounted(() => {
  void ensureAgentMetadata()
  document.addEventListener('click', onDocumentClick, true)
  window.addEventListener('resize', () => { if (dropdownVisible.value) computeDropdownStyle() })
})

onBeforeUnmount(() => {
  document.removeEventListener('click', onDocumentClick, true)
})

function onSelect(agent: DisplayAgent) {
  settingsStore.selectAgent(agent.id, agent.sourceTenantId ?? null, agent.agentMode)
  dropdownVisible.value = false
  emit('select', agent.id)
}
</script>

<style scoped>
.videohub-agent-picker {
  display: inline-flex;
  flex-shrink: 0;
  position: relative;
}

.picker-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 6px 12px;
  background: var(--td-bg-color-secondarycontainer);
  backdrop-filter: blur(20px) saturate(180%);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-round);
  color: var(--td-text-color-primary);
  font-size: var(--td-font-body-small, 13px);
  cursor: pointer;
  transition: background 0.2s ease, border-color 0.2s ease;
}

.picker-chip:hover {
  background: var(--td-bg-color-secondarycontainer-hover);
}

.picker-chip__label {
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.picker-chip__icon--open {
  transform: rotate(180deg);
  transition: transform 0.2s ease;
}
</style>

<style>
.picker-dropdown {
  min-width: 160px;
  padding: 4px;
  background: var(--td-bg-color-container);
  backdrop-filter: blur(20px) saturate(180%);
  border: 1px solid var(--td-component-stroke);
  border-radius: var(--td-radius-large);
  box-shadow: var(--td-shadow-2);
}

.picker-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: 100%;
  padding: 8px 12px;
  background: transparent;
  border: none;
  border-radius: var(--td-radius-medium);
  color: var(--td-text-color-primary);
  font-size: var(--td-font-body-medium, 14px);
  cursor: pointer;
  transition: background 0.15s ease;
}

.picker-item:hover {
  background: var(--td-bg-color-container-hover);
}

.picker-item--active {
  color: var(--td-brand-color);
}

.picker-item__check {
  color: var(--td-brand-color);
}
</style>
