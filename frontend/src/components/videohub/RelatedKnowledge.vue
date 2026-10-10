<template>
  <section class="native-wiki-graph">
    <t-alert v-if="processingFailure" theme="error" message="关联知识生成失败">
      <template #operation><t-button size="small" variant="outline" :loading="props.isRetrying" @click="emit('retry')">重试</t-button></template>
    </t-alert>
    <div v-else-if="showSkeleton" class="native-wiki-graph__skeleton" aria-busy="true" aria-label="正在加载关联知识">
      <t-skeleton animation="gradient" :row-col="[{ width: '38%', height: '18px' }, { width: '92%', height: '14px' }, { width: '68%', height: '14px' }]" />
    </div>
    <t-alert v-else-if="error" theme="error" :message="error">
      <template #operation><t-button size="small" variant="outline" @click="emit('reload')">刷新</t-button></template>
    </t-alert>
    <t-empty v-else-if="!context.center" :description="notGenerated ? '视频摘要尚未生成' : '暂无关联知识'">
      <template #action><t-button size="small" variant="outline" @click="emit('reload')">刷新</t-button></template>
    </t-empty>
    <template v-else>
      <p class="native-wiki-graph__overview">{{ overview }}</p>
      <div v-if="graphLoading" class="native-wiki-graph__canvas-state"><t-loading text="正在加载原生 Wiki 图谱" /></div>
      <t-alert v-else-if="graphError" theme="error" :message="graphError">
        <template #operation><t-button size="small" variant="outline" @click="loadGraph">刷新</t-button></template>
      </t-alert>
      <t-empty v-else-if="!graph || graph.nodes.length <= 1" description="该摘要页暂未链接其他 Wiki 页面" />
      <div v-else ref="chartElement" class="native-wiki-graph__canvas" aria-label="原生 Wiki 关联图谱" />
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import * as echarts from 'echarts'
import type { WikiGraphData } from '@/api/wiki'
import type { ContentState } from '@/types/videohub'
import type { RelatedKnowledgeContent } from '@/api/videohub/contentState'
import { fetchNativeWikiGraph } from '@/api/videohub/relatedKnowledge'

const props = withDefaults(defineProps<{
  contentState: ContentState<RelatedKnowledgeContent>
  isGenerating?: boolean
  isProcessingFailed?: boolean
  isRetrying?: boolean
}>(), { isGenerating: false, isProcessingFailed: false, isRetrying: false })
const emit = defineEmits<{ reload: []; retry: [] }>()
const router = useRouter()
const chartElement = ref<HTMLElement | null>(null)
const graph = ref<WikiGraphData | null>(null)
const graphLoading = ref(false)
const graphError = ref('')
let chart: echarts.ECharts | null = null
let resizeObserver: ResizeObserver | null = null

const context = computed(() => props.contentState.data)
const processingFailure = computed(() => props.isProcessingFailed)
const notGenerated = computed(() => props.contentState.status === 'not_generated')
const error = computed(() => props.contentState.status === 'error' ? props.contentState.error || '关联知识加载失败' : '')
const showSkeleton = computed(() => !processingFailure.value && !context.value.center && (props.contentState.status === 'loading' || props.isGenerating))
const relatedNodes = computed(() => graph.value?.nodes.filter(node => node.slug !== context.value.center?.slug) || [])
const overview = computed(() => {
  const centerTitle = context.value.center?.title || '该视频摘要'
  if (!relatedNodes.value.length) return `${centerTitle} 暂未关联其他 Wiki 页面。`
  const titles = relatedNodes.value.slice(0, 3).map(node => node.title).filter(Boolean)
  const suffix = relatedNodes.value.length > titles.length ? '等' : ''
  return `${centerTitle} 已关联 ${relatedNodes.value.length} 个 Wiki 页面：${titles.join('、')}${suffix}。`
})

function disposeChart() {
  resizeObserver?.disconnect()
  resizeObserver = null
  chart?.dispose()
  chart = null
}

function openWikiPage(slug: string) {
  if (!context.value.knowledgeBaseId || !slug) return
  router.push({ path: `/platform/knowledge-bases/${context.value.knowledgeBaseId}`, query: { tab: 'wiki', slug } })
}

function renderGraph() {
  if (!chartElement.value || !graph.value || !context.value.center) return
  disposeChart()
  chart = echarts.init(chartElement.value)
  const centerSlug = context.value.center.slug
  const styles = getComputedStyle(chartElement.value)
  const brandColor = styles.getPropertyValue('--td-brand-color').trim() || '#07c05f'
  const surfaceColor = styles.getPropertyValue('--td-bg-color-container').trim() || '#ffffff'
  const strokeColor = styles.getPropertyValue('--td-component-stroke').trim() || '#e7e7e7'
  const textColor = styles.getPropertyValue('--td-text-color-primary').trim() || 'rgba(0, 0, 0, 0.9)'
  chart.setOption({
    animationDurationUpdate: 260,
    tooltip: { trigger: 'item', formatter: (params: any) => params.data?.name || '' },
    series: [{
      type: 'graph', layout: 'force', roam: true, draggable: true,
      data: graph.value.nodes.map(node => ({
        id: node.slug, name: node.title || node.slug,
        symbolSize: node.slug === centerSlug ? 54 : 34,
        itemStyle: { color: node.slug === centerSlug ? brandColor : surfaceColor, borderColor: strokeColor, borderWidth: node.slug === centerSlug ? 0 : 1 },
        label: { show: true, color: textColor, fontSize: 12, width: 96, overflow: 'truncate' },
      })),
      links: graph.value.edges.map(edge => ({ source: edge.source, target: edge.target })),
      lineStyle: { color: strokeColor, width: 1.2, curveness: 0.08 },
      force: { repulsion: 280, edgeLength: [76, 128], gravity: 0.08 },
      emphasis: { focus: 'adjacency', lineStyle: { width: 2, color: brandColor } },
    }],
  })
  chart.on('click', (params: any) => { if (params.dataType === 'node') openWikiPage(params.data.id) })
  resizeObserver = new ResizeObserver(() => chart?.resize())
  resizeObserver.observe(chartElement.value)
}

async function loadGraph() {
	const center = context.value.center
	if (!center || !context.value.videoId) return
  graphLoading.value = true
  graphError.value = ''
  graph.value = null
  disposeChart()
  try {
		graph.value = await fetchNativeWikiGraph(context.value.videoId)
  } catch (reason: any) {
    graphError.value = reason?.error_message || reason?.message || reason?.error || '原生 Wiki 图谱加载失败'
  } finally {
    graphLoading.value = false
    // The chart container is rendered only after the loading state is cleared.
    await nextTick()
    renderGraph()
  }
}

watch(() => `${context.value.knowledgeBaseId}:${context.value.center?.slug || ''}`, () => { void loadGraph() }, { immediate: true })
onBeforeUnmount(disposeChart)
</script>

<style scoped>
.native-wiki-graph { display: grid; gap: 14px; min-height: 320px; padding: 16px 4px 96px 0; }
.native-wiki-graph__overview { margin: 0; color: var(--td-text-color-secondary); font: var(--td-font-body-small); line-height: 22px; }
.native-wiki-graph__canvas { height: min(520px, calc(100vh - 260px)); min-height: 320px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-large); background: color-mix(in srgb, var(--td-bg-color-container) 58%, transparent); backdrop-filter: blur(18px) saturate(112%); }
.native-wiki-graph__canvas-state, .native-wiki-graph__skeleton, .native-wiki-graph > :deep(.t-empty) { min-height: 320px; display: grid; place-items: center; }
.native-wiki-graph__skeleton { padding: 16px 8px; }
@media (max-width: 768px) { .native-wiki-graph__canvas { height: 360px; min-height: 280px; } }
</style>
