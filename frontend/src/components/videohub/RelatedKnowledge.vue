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
      <header class="native-wiki-graph__header">
        <div>
          <div class="native-wiki-graph__eyebrow"><t-icon name="chart-bubble" size="15px" />原生 Wiki 关系</div>
          <p class="native-wiki-graph__overview">{{ overview }}</p>
        </div>
        <t-tooltip content="适应视图" placement="top">
          <button class="native-wiki-graph__fit" type="button" aria-label="适应图谱视图" :disabled="!chart" @click="fitGraph">
            <t-icon name="focus" size="16px" />
          </button>
        </t-tooltip>
      </header>
      <div v-if="graphLoading" class="native-wiki-graph__canvas-state"><t-loading text="正在加载原生 Wiki 图谱" /></div>
      <t-alert v-else-if="graphError" theme="error" :message="graphError">
        <template #operation><t-button size="small" variant="outline" @click="loadGraph">刷新</t-button></template>
      </t-alert>
      <t-empty v-else-if="!graph || graph.nodes.length <= 1" description="该摘要页暂未链接其他 Wiki 页面" />
      <div v-else class="native-wiki-graph__workspace">
      <div v-show="!readerOpen" class="native-wiki-graph__frame">
        <div ref="chartElement" class="native-wiki-graph__canvas" aria-label="原生 Wiki 关联图谱" />
        <div class="native-wiki-graph__legend" aria-label="Wiki 页面类型图例">
          <span v-for="item in visibleLegendItems" :key="item.type" class="native-wiki-graph__legend-item">
            <i :style="{ backgroundColor: item.color }" />{{ item.label }}
          </span>
        </div>
        <span class="native-wiki-graph__hint">点击节点查看 Wiki 页面</span>
      </div>
      <section v-if="readerOpen" class="native-wiki-graph__reader" aria-label="Wiki 页面阅读器">
        <header class="native-wiki-graph__reader-header">
          <t-tooltip :content="backTooltip" placement="top"><t-button variant="text" shape="square" :aria-label="backTooltip" @click="goBack"><t-icon name="chevron-left" /></t-button></t-tooltip>
          <t-tag v-if="selectedPage" size="small" variant="light-outline">{{ pageTypeLabel(selectedPage.page_type) }}</t-tag>
          <span class="native-wiki-graph__reader-title">{{ selectedPage?.title || 'Wiki 页面' }}</span>
          <t-button size="small" variant="text" :disabled="!selectedPage" @click="openInKnowledgeBase">在知识库中打开</t-button>
          <t-tooltip content="返回关联知识图谱" placement="top"><t-button variant="text" shape="square" aria-label="返回关联知识图谱" @click="returnToGraph"><t-icon name="chart-bubble" /></t-button></t-tooltip>
        </header>
        <div v-if="pageLoading" class="native-wiki-graph__reader-state"><t-loading text="正在加载 Wiki 页面" /></div>
        <t-alert v-else-if="pageError" theme="error" :message="pageError"><template #operation><t-button size="small" variant="outline" @click="goBack">返回</t-button></template></t-alert>
        <article v-else class="native-wiki-graph__reader-body markdown-content" v-html="renderedPageContent" @click="handleReaderClick" />
      </section>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import * as echarts from 'echarts'
import { marked } from 'marked'
import { getWikiPage, type WikiGraphData, type WikiPage } from '@/api/wiki'
import type { ContentState } from '@/types/videohub'
import type { RelatedKnowledgeContent } from '@/api/videohub/contentState'
import { fetchNativeWikiGraph } from '@/api/videohub/relatedKnowledge'
import { sanitizeMarkdownHTML } from '@/utils/security'

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
const selectedPage = ref<WikiPage | null>(null)
const pageLoading = ref(false)
const pageError = ref('')
const pageHistory = ref<string[]>([])
const readerOpen = ref(false)
let chart: echarts.ECharts | null = null
let resizeObserver: ResizeObserver | null = null

const context = computed(() => props.contentState.data)
const processingFailure = computed(() => props.isProcessingFailed)
const notGenerated = computed(() => props.contentState.status === 'not_generated')
const error = computed(() => props.contentState.status === 'error' ? props.contentState.error || '关联知识加载失败' : '')
const showSkeleton = computed(() => !processingFailure.value && !context.value.center && (props.contentState.status === 'loading' || props.isGenerating))
const relatedNodes = computed(() => graph.value?.nodes.filter(node => node.slug !== context.value.center?.slug) || [])
const PAGE_TYPE_STYLES: Record<string, { label: string; color: string }> = {
  summary: { label: '摘要', color: '#0052d9' },
  entity: { label: '实体', color: '#2ba471' },
  concept: { label: '概念', color: '#e37318' },
  synthesis: { label: '综合', color: '#0594fa' },
  comparison: { label: '对比', color: '#d54941' },
}
const DEFAULT_PAGE_TYPE_STYLE = { label: 'Wiki 页面', color: '#5f6b7a' }
const visibleLegendItems = computed(() => {
  const presentTypes = new Set(graph.value?.nodes.map(node => node.page_type) || [])
  return [...presentTypes].map(type => ({ type, ...(PAGE_TYPE_STYLES[type] || DEFAULT_PAGE_TYPE_STYLE) }))
})
const overview = computed(() => {
  const centerTitle = context.value.center?.title || '该视频摘要'
  if (!relatedNodes.value.length) return `${centerTitle} 暂未关联其他 Wiki 页面。`
  const titles = relatedNodes.value.slice(0, 3).map(node => node.title).filter(Boolean)
  const suffix = relatedNodes.value.length > titles.length ? '等' : ''
  return `${centerTitle} 已关联 ${relatedNodes.value.length} 个 Wiki 页面：${titles.join('、')}${suffix}。`
})
const backTooltip = computed(() => pageHistory.value.length ? '返回上一页' : '返回关联知识图谱')
const renderedPageContent = computed(() => {
  if (!selectedPage.value) return ''
  const content = stripDuplicateLeadingTitle(selectedPage.value.content || '', selectedPage.value.title)
  const linkedContent = content.replace(/\[\[([^\]]+)\]\]/g, (_, inner: string) => {
    const separator = inner.indexOf('|')
    const slug = (separator > 0 ? inner.slice(0, separator) : inner).trim()
    const label = (separator > 0 ? inner.slice(separator + 1) : slug).trim()
    return `<a href="#" class="native-wiki-graph__content-link" data-wiki-slug="${encodeURIComponent(slug)}">${label}</a>`
  })
  return sanitizeMarkdownHTML(marked.parse(linkedContent, { breaks: true, async: false }) as string)
})

function disposeChart() {
  resizeObserver?.disconnect()
  resizeObserver = null
  chart?.dispose()
  chart = null
}

function pageTypeLabel(type: string) { return (PAGE_TYPE_STYLES[type] || DEFAULT_PAGE_TYPE_STYLE).label }
function stripDuplicateLeadingTitle(content: string, title: string) {
  const lines = content.split('\n')
  const index = lines.findIndex(line => line.trim() !== '')
  const match = index < 0 ? null : lines[index].match(/^#\s+(.+?)\s*$/)
  return !match || match[1].trim().toLowerCase() !== title.trim().toLowerCase() ? content : lines.slice(index + 1).join('\n').replace(/^\s+/, '')
}
async function openWikiPage(slug: string, recordHistory = true) {
  if (!context.value.knowledgeBaseId || !slug || selectedPage.value?.slug === slug) return
  if (recordHistory && selectedPage.value) pageHistory.value.push(selectedPage.value.slug)
  readerOpen.value = true; pageLoading.value = true; pageError.value = ''; selectedPage.value = null
  try { const response = await getWikiPage(context.value.knowledgeBaseId, slug); selectedPage.value = ((response as any).data || response) as WikiPage }
  catch (reason: any) { pageError.value = reason?.error_message || reason?.message || '无法读取该 Wiki 页面' }
  finally { pageLoading.value = false }
}
function returnToGraph() { readerOpen.value = false; selectedPage.value = null; pageLoading.value = false; pageError.value = ''; pageHistory.value = []; void nextTick(() => chart?.resize()) }
function goBack() { const previousSlug = pageHistory.value.pop(); if (previousSlug) void openWikiPage(previousSlug, false); else returnToGraph() }
function openInKnowledgeBase() { if (context.value.knowledgeBaseId && selectedPage.value?.slug) router.push({ path: `/platform/knowledge-bases/${context.value.knowledgeBaseId}`, query: { tab: 'wiki', slug: selectedPage.value.slug } }) }
function handleReaderClick(event: MouseEvent) {
  const link = (event.target as HTMLElement).closest('.native-wiki-graph__content-link') as HTMLElement | null
  if (!link) return
  event.preventDefault(); const slug = decodeURIComponent(link.dataset.wikiSlug || ''); if (slug) void openWikiPage(slug)
}

function fitGraph() {
  chart?.dispatchAction({ type: 'restore' })
  chart?.resize()
}

function renderGraph() {
  if (!chartElement.value || !graph.value || !context.value.center) return
  disposeChart()
  chart = echarts.init(chartElement.value)
  const centerSlug = context.value.center.slug
  const styles = getComputedStyle(chartElement.value)
  const brandColor = styles.getPropertyValue('--td-brand-color').trim() || '#07c05f'
  const strokeColor = styles.getPropertyValue('--td-component-stroke').trim() || '#e7e7e7'
  const textColor = styles.getPropertyValue('--td-text-color-primary').trim() || 'rgba(0, 0, 0, 0.9)'
  chart.setOption({
    animationDurationUpdate: 260,
    tooltip: { trigger: 'item', formatter: (params: any) => params.data?.name || '' },
    series: [{
      type: 'graph', layout: 'force', roam: true, draggable: true,
      data: graph.value.nodes.map(node => ({
        id: node.slug, name: node.title || node.slug,
        symbolSize: node.slug === centerSlug ? 58 : 36,
        itemStyle: node.slug === centerSlug
          ? { color: brandColor, borderColor: 'rgba(255, 255, 255, 0.96)', borderWidth: 3, shadowBlur: 12, shadowColor: 'rgba(7, 192, 95, 0.22)' }
          : { color: (PAGE_TYPE_STYLES[node.page_type] || DEFAULT_PAGE_TYPE_STYLE).color, borderColor: 'rgba(255, 255, 255, 0.92)', borderWidth: 2 },
        label: { show: true, color: textColor, fontSize: 12, width: 104, overflow: 'truncate' },
      })),
      links: graph.value.edges.map(edge => ({ source: edge.source, target: edge.target })),
      lineStyle: { color: strokeColor, width: 1.2, curveness: 0.08 },
      force: { repulsion: 300, edgeLength: [82, 138], gravity: 0.08 },
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
.native-wiki-graph__header { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.native-wiki-graph__eyebrow { display: flex; align-items: center; gap: 5px; margin-bottom: 4px; color: var(--td-text-color-primary); font: var(--td-font-title-small); font-weight: 600; }
.native-wiki-graph__overview { margin: 0; color: var(--td-text-color-secondary); font: var(--td-font-body-small); line-height: 22px; }
.native-wiki-graph__fit { display: inline-grid; flex: 0 0 auto; width: 32px; height: 32px; place-items: center; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); color: var(--td-text-color-secondary); background: rgba(255, 255, 255, .58); cursor: pointer; transition: color .15s ease, border-color .15s ease, background .15s ease; }
.native-wiki-graph__fit:hover:not(:disabled) { color: var(--td-brand-color); border-color: var(--td-brand-color); background: var(--td-brand-color-light); }
.native-wiki-graph__fit:disabled { cursor: default; opacity: .45; }
.native-wiki-graph__workspace { height: min(520px, calc(100vh - 260px)); min-height: 320px; }
.native-wiki-graph__frame, .native-wiki-graph__reader { height: 100%; min-height: 320px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-large); background: color-mix(in srgb, var(--td-bg-color-container) 58%, transparent); backdrop-filter: blur(18px) saturate(112%); }
.native-wiki-graph__frame { position: relative; overflow: hidden; }
.native-wiki-graph__canvas { width: 100%; height: 100%; }
.native-wiki-graph__legend { position: absolute; top: 12px; right: 12px; display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 6px 10px; max-width: calc(100% - 24px); padding: 8px 10px; border: 1px solid rgba(255, 255, 255, .9); border-radius: var(--td-radius-medium); background: rgba(255, 255, 255, .78); box-shadow: var(--td-shadow-1); backdrop-filter: blur(12px) saturate(112%); pointer-events: none; }
.native-wiki-graph__legend-item { display: inline-flex; align-items: center; gap: 5px; color: var(--td-text-color-secondary); font: var(--td-font-body-small); line-height: 16px; white-space: nowrap; }
.native-wiki-graph__legend-item i { width: 8px; height: 8px; border-radius: var(--td-radius-circle); box-shadow: 0 0 0 1px rgba(255, 255, 255, .9); }
.native-wiki-graph__hint { position: absolute; right: 12px; bottom: 12px; color: var(--td-text-color-placeholder); font: var(--td-font-body-small); pointer-events: none; }
.native-wiki-graph__reader { display: flex; flex-direction: column; overflow: hidden; }
.native-wiki-graph__reader-header { display: flex; min-height: 48px; align-items: center; gap: 8px; padding: 8px 10px; border-bottom: 1px solid var(--td-component-stroke); background: rgba(255,255,255,.38); }
.native-wiki-graph__reader-title { min-width: 0; flex: 1; overflow: hidden; font: var(--td-font-title-small); font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.native-wiki-graph__reader-state { display: grid; min-height: 0; flex: 1; place-items: center; }
.native-wiki-graph__reader > :deep(.t-alert) { margin: 14px; }
.native-wiki-graph__reader-body { min-height: 0; flex: 1; overflow: auto; padding: 18px 20px 108px; color: var(--td-text-color-secondary); font: var(--td-font-body-small); line-height: 1.75; }
.native-wiki-graph__reader-body :deep(h1), .native-wiki-graph__reader-body :deep(h2), .native-wiki-graph__reader-body :deep(h3) { color: var(--td-text-color-primary); line-height: 1.4; }
.native-wiki-graph__reader-body :deep(blockquote) { margin: 14px 0; padding: 8px 12px; border-left: 3px solid var(--td-brand-color); background: var(--td-brand-color-light); }
.native-wiki-graph__reader-body :deep(.native-wiki-graph__content-link) { color: var(--td-brand-color); cursor: pointer; text-decoration: underline; }
.native-wiki-graph__canvas-state, .native-wiki-graph__skeleton, .native-wiki-graph > :deep(.t-empty) { min-height: 320px; display: grid; place-items: center; }
.native-wiki-graph__skeleton { padding: 16px 8px; }
@media (max-width: 768px) { .native-wiki-graph__workspace { height: 360px; min-height: 280px; } .native-wiki-graph__frame, .native-wiki-graph__reader { min-height: 280px; } .native-wiki-graph__legend { top: 8px; right: 8px; left: 8px; justify-content: flex-start; } .native-wiki-graph__hint { right: 8px; bottom: 8px; } .native-wiki-graph__reader-header > :deep(.t-button--size-small) { display: none; } }
</style>
