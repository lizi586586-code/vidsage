<template>
  <section class="related-wiki">
    <t-alert v-if="processingFailure" theme="error" message="关联知识生成失败">
      <template #operation><t-button size="small" variant="outline" :loading="props.isRetrying" @click="emit('retry')">重试</t-button></template>
    </t-alert>
    <div v-else-if="showSkeleton" class="state page-skeleton" aria-busy="true" aria-label="正在获取关联知识">
      <t-skeleton animation="gradient" :row-col="skeletonRows" />
    </div>
    <t-alert v-else-if="error" theme="error" :message="error">
      <template #operation><t-button size="small" variant="outline" @click="emit('reload')">刷新</t-button></template>
    </t-alert>
    <t-empty v-else-if="!context.center" :description="notGenerated ? '视频摘要尚未生成' : '暂无关联知识'" />
    <template v-else>
      <header class="header">
        <h3 class="overview">{{ overviewTitle }}</h3>
        <div class="switch" role="group" aria-label="关联知识视图">
          <button :class="{ active: viewMode === 'list' }" :aria-pressed="viewMode === 'list'" @click="viewMode = 'list'">列表</button>
          <button :class="{ active: viewMode === 'graph' }" :aria-pressed="viewMode === 'graph'" @click="showGraph">图谱</button>
        </div>
      </header>
      <div v-if="graphLoading" class="state page-skeleton" aria-busy="true" aria-label="正在获取知识节点">
        <t-skeleton animation="gradient" :row-col="skeletonRows" />
      </div>
      <t-alert v-else-if="graphError" theme="error" :message="graphError">
        <template #operation><t-button size="small" variant="outline" @click="loadGraph">刷新</t-button></template>
      </t-alert>
      <t-empty v-else-if="!relatedNodes.length" description="该摘要页暂未链接其他 Wiki 页面" />
      <div v-else-if="viewMode === 'list'" class="list">
        <article v-for="node in relatedNodes" :key="node.slug" class="item">
          <div class="item-head"><i /><strong>{{ node.title || node.slug }}</strong><t-tag size="small" variant="light-outline">{{ pageTypeLabel(node.page_type) }}</t-tag></div>
          <div v-if="relationsLoading" class="relation-skeleton" aria-label="正在生成关联说明"><t-skeleton animation="gradient" :row-col="[{ width: '82%', height: '13px' }]" /></div>
          <p v-else>{{ relationFor(node.slug) }}</p>
          <button class="view" type="button" @click="openWikiPage(node.slug)">查看 <t-icon name="chevron-right" size="14px" /></button>
        </article>
      </div>
      <div v-else class="graph-frame">
        <t-tooltip content="适应视图"><button class="fit" aria-label="适应图谱视图" :disabled="!chart" @click="fitGraph"><t-icon name="focus" /></button></t-tooltip>
        <div ref="chartElement" class="canvas" aria-label="WeKnora 原生 Wiki 关联图谱" />
        <span class="hint">点击节点查看 Wiki 页面</span>
      </div>
    </template>
    <t-drawer v-model:visible="drawerOpen" placement="right" size="560px" drawer-class-name="related-wiki-drawer" :footer="false" :destroy-on-close="false">
      <template #header>
        <div class="drawer-header">
          <t-button class="drawer-back" variant="text" shape="square" aria-label="返回关联知识" @click="drawerOpen = false">
            <t-icon name="chevron-left" />
          </t-button>
          <div class="drawer-title">
            <t-tag v-if="selectedPage" size="small" variant="light-outline">{{ pageTypeLabel(selectedPage.page_type) }}</t-tag>
            <strong>{{ selectedPage?.title || 'Wiki 页面' }}</strong>
          </div>
        </div>
      </template>
      <div v-if="pageLoading" class="reader-state"><t-loading text="正在加载 Wiki 页面" /></div>
      <t-alert v-else-if="pageError" theme="error" :message="pageError" />
      <article v-else class="reader" v-html="renderedPageContent" />
    </t-drawer>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import * as echarts from 'echarts'
import { marked } from 'marked'
import { getWikiPage, type WikiGraphData, type WikiPage } from '@/api/wiki'
import type { ContentState } from '@/types/videohub'
import type { RelatedKnowledgeContent } from '@/api/videohub/contentState'
import { fetchNativeWikiGraph, fetchNativeWikiRelations } from '@/api/videohub/relatedKnowledge'
import { sanitizeMarkdownHTML } from '@/utils/security'

const props = withDefaults(defineProps<{ contentState: ContentState<RelatedKnowledgeContent>; isGenerating?: boolean; isProcessingFailed?: boolean; isRetrying?: boolean }>(), { isGenerating: false, isProcessingFailed: false, isRetrying: false })
const emit = defineEmits<{ reload: []; retry: [] }>()
const chartElement = ref<HTMLElement | null>(null)
const graph = ref<WikiGraphData | null>(null)
const relations = ref<Record<string, string>>({})
const graphLoading = ref(false)
const relationsLoading = ref(false)
const graphError = ref('')
const viewMode = ref<'list' | 'graph'>('list')
const drawerOpen = ref(false)
const selectedPage = ref<WikiPage | null>(null)
const pageLoading = ref(false)
const pageError = ref('')
const skeletonRows = [{ width: '38%', height: '18px' }, { width: '92%', height: '14px' }, { width: '84%', height: '14px' }, { width: '88%', height: '14px' }]
let chart: echarts.ECharts | null = null
let resizeObserver: ResizeObserver | null = null
let loadSequence = 0

const context = computed(() => props.contentState.data)
const processingFailure = computed(() => props.isProcessingFailed)
const notGenerated = computed(() => props.contentState.status === 'not_generated')
const error = computed(() => props.contentState.status === 'error' ? props.contentState.error || '关联知识加载失败' : '')
const showSkeleton = computed(() => !processingFailure.value && !context.value.center && (props.contentState.status === 'loading' || props.isGenerating))
const relatedNodes = computed(() => (graph.value?.nodes.filter(node => node.slug !== context.value.center?.slug) || []).slice().sort((a, b) => (a.title || a.slug).localeCompare(b.title || b.slug, 'zh-CN')))
const overviewTitle = computed(() => `关联 ${relatedNodes.value.length} 篇 Wiki 页面`)
const labels: Record<string, string> = { summary: '摘要', entity: '实体', concept: '概念', synthesis: '综合', comparison: '对比', index: '索引' }
const renderedPageContent = computed(() => selectedPage.value ? sanitizeMarkdownHTML(marked.parse(stripTitle(selectedPage.value.content || '', selectedPage.value.title), { breaks: true, async: false }) as string) : '')

function pageTypeLabel(type: string) { return labels[type] || 'Wiki 页面' }
function relationFor(slug: string) { return relations.value[slug] || '暂无足够内容生成可靠的关联说明。' }
function stripTitle(content: string, title: string) {
  const lines = content.split('\n')
  const index = lines.findIndex(line => line.trim())
  const match = index < 0 ? null : lines[index].match(/^#\s+(.+?)\s*$/)
  return !match || match[1].trim().toLowerCase() !== title.trim().toLowerCase() ? content : lines.slice(index + 1).join('\n').replace(/^\s+/, '')
}
function disposeChart() { resizeObserver?.disconnect(); resizeObserver = null; chart?.dispose(); chart = null }
function fitGraph() { chart?.dispatchAction({ type: 'restore' }); chart?.resize() }
function showGraph() { viewMode.value = 'graph'; void nextTick(renderGraph) }
async function openWikiPage(slug: string) {
  if (!context.value.knowledgeBaseId || !slug) return
  drawerOpen.value = true
  pageLoading.value = true
  pageError.value = ''
  selectedPage.value = null
  try {
    const response = await getWikiPage(context.value.knowledgeBaseId, slug)
    selectedPage.value = ((response as any).data || response) as WikiPage
  } catch (reason: any) {
    pageError.value = reason?.error_message || reason?.message || '无法读取该 Wiki 页面'
  } finally { pageLoading.value = false }
}
function renderGraph() {
  if (viewMode.value !== 'graph' || !chartElement.value || !graph.value || !context.value.center) return
  disposeChart()
  chart = echarts.init(chartElement.value)
  const css = getComputedStyle(chartElement.value)
  const brand = css.getPropertyValue('--td-brand-color').trim() || '#07c05f'
  const stroke = css.getPropertyValue('--td-component-stroke').trim() || '#e7e7e7'
  const text = css.getPropertyValue('--td-text-color-primary').trim() || 'rgba(0,0,0,.9)'
  const center = context.value.center.slug
  chart.setOption({ series: [{ type: 'graph', layout: 'force', roam: true, draggable: true, data: graph.value.nodes.map(node => ({ id: node.slug, name: node.title || node.slug, symbolSize: node.slug === center ? 56 : 34, itemStyle: { color: node.slug === center ? brand : '#fff', borderColor: brand, borderWidth: 2 }, label: { show: true, color: text, fontSize: 12, width: 104, overflow: 'truncate' } })), links: graph.value.edges.map(edge => ({ source: edge.source, target: edge.target })), lineStyle: { color: stroke, width: 1.2 }, force: { repulsion: 300, edgeLength: [82, 138], gravity: .08 }, emphasis: { focus: 'adjacency' } }] })
  chart.on('click', (params: any) => { if (params.dataType === 'node' && params.data.id !== center) void openWikiPage(params.data.id) })
  resizeObserver = new ResizeObserver(() => chart?.resize())
  resizeObserver.observe(chartElement.value)
}
async function loadRelations(sequence: number) {
  if (!context.value.videoId || !relatedNodes.value.length) return
  relationsLoading.value = true
  try {
    const result = await fetchNativeWikiRelations(context.value.videoId)
    if (sequence === loadSequence) relations.value = result
  } catch {
    if (sequence === loadSequence) relations.value = {}
  } finally {
    if (sequence === loadSequence) relationsLoading.value = false
  }
}
async function loadGraph() {
  if (!context.value.center || !context.value.videoId) return
  const sequence = ++loadSequence
  graphLoading.value = true
  relationsLoading.value = false
  graphError.value = ''
  graph.value = null
  relations.value = {}
  disposeChart()
  try {
    const result = await fetchNativeWikiGraph(context.value.videoId)
    if (sequence !== loadSequence) return
    graph.value = result
  } catch (reason: any) {
    if (sequence === loadSequence) graphError.value = reason?.error_message || reason?.message || reason?.error || '原生 Wiki 图谱加载失败'
  } finally {
    if (sequence !== loadSequence) return
    graphLoading.value = false
    await nextTick()
    if (viewMode.value === 'graph') renderGraph()
    void loadRelations(sequence)
  }
}

watch(() => `${context.value.knowledgeBaseId}:${context.value.center?.slug || ''}`, () => { viewMode.value = 'list'; drawerOpen.value = false; void loadGraph() }, { immediate: true })
onBeforeUnmount(() => { loadSequence++; disposeChart() })
</script>

<style scoped>
.related-wiki { display: grid; min-height: 320px; padding: 16px 4px 104px 0; }
.header { display: flex; min-height: 48px; align-items: center; justify-content: space-between; gap: 20px; padding: 8px 0 20px; }
h3 { margin: 0; font: var(--td-font-title-medium); font-weight: 600; }
.switch { display: inline-flex; padding: 3px; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); background: color-mix(in srgb, var(--td-bg-color-container) 58%, transparent); }
.switch button { min-width: 52px; padding: 5px 10px; border: 0; border-radius: 4px; color: var(--td-text-color-secondary); background: transparent; cursor: pointer; }
.switch button.active { color: var(--td-brand-color); background: var(--td-brand-color-light); font-weight: 600; }
.overview { color: var(--td-text-color-primary); font: var(--td-font-title-medium); font-weight: 600; }
.list { padding-top: 10px; border-top: 1px solid var(--td-component-stroke); }
.item { position: relative; padding: 18px 2px 20px; border-bottom: 1px solid var(--td-component-stroke); }
.item-head { display: flex; align-items: center; gap: 8px; }
.item-head i { width: 7px; height: 7px; flex: none; border-radius: var(--td-radius-circle); background: var(--td-brand-color); }
.item-head strong { min-width: 0; overflow-wrap: anywhere; font: var(--td-font-title-small); font-weight: 600; }
.item p { margin: 8px 54px 0 15px; color: var(--td-text-color-secondary); font: var(--td-font-body-small); line-height: 21px; }
.relation-skeleton { margin: 10px 54px 1px 15px; min-height: 20px; }
.view { position: absolute; right: 2px; bottom: 18px; display: inline-flex; align-items: center; border: 0; color: var(--td-brand-color); background: transparent; cursor: pointer; font: var(--td-font-body-small); font-weight: 600; }
.graph-frame { position: relative; height: min(480px, calc(100vh - 300px)); min-height: 320px; overflow: hidden; border: 1px solid rgba(255,255,255,.9); border-radius: var(--td-radius-large); background: color-mix(in srgb, var(--td-bg-color-container) 58%, transparent); backdrop-filter: blur(18px) saturate(112%); }
.canvas { width: 100%; height: 100%; }
.fit { position: absolute; z-index: 2; top: 12px; right: 12px; display: grid; width: 32px; height: 32px; place-items: center; border: 1px solid var(--td-component-stroke); border-radius: var(--td-radius-medium); color: var(--td-text-color-secondary); background: rgba(255,255,255,.72); backdrop-filter: blur(12px); cursor: pointer; }
.hint { position: absolute; right: 12px; bottom: 12px; color: var(--td-text-color-placeholder); font: var(--td-font-body-small); }
.state, .related-wiki > :deep(.t-empty) { min-height: 320px; display: grid; place-items: center; }
.page-skeleton { align-content: start; padding: 34px 8px; }
.drawer-header { display: flex; min-width: 0; align-items: center; gap: 8px; }
.drawer-back { flex: none; }
.drawer-title { display: flex; min-width: 0; align-items: center; gap: 8px; }
.drawer-title strong { min-width: 0; overflow: hidden; font: var(--td-font-title-small); font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.reader-state { display: grid; min-height: 280px; place-items: center; }
.reader { box-sizing: border-box; contain: inline-size; width: 100%; max-width: 100%; min-width: 0; margin: 0; padding: 2px 0 96px; overflow-x: clip; color: var(--td-text-color-secondary); font-size: 14px; line-height: 1.75; overflow-wrap: anywhere; word-break: break-word; }
.reader :deep(*) { box-sizing: border-box; min-width: 0; max-width: 100%; }
.reader :deep(p), .reader :deep(h1), .reader :deep(h2), .reader :deep(h3), .reader :deep(h4), .reader :deep(h5), .reader :deep(h6) { margin-right: 0; margin-left: 0; }
.reader :deep(h1), .reader :deep(h2), .reader :deep(h3), .reader :deep(h4), .reader :deep(h5), .reader :deep(h6) { color: var(--td-text-color-primary); font-size: 14px; font-weight: 600; line-height: 1.55; overflow-wrap: anywhere; word-break: break-word; }
.reader :deep(ul), .reader :deep(ol) { margin-right: 0; margin-left: 0; padding-left: 24px; }
.reader :deep(a) { color: var(--td-brand-color); text-decoration: underline; }
.reader :deep(img), .reader :deep(video), .reader :deep(svg), .reader :deep(canvas), .reader :deep(iframe) { display: block; width: auto; max-width: 100%; height: auto; }
.reader :deep(pre), .reader :deep(table) { display: block; width: 100%; max-width: 100%; overflow-x: auto; white-space: pre-wrap; overflow-wrap: anywhere; }
.reader :deep(code), .reader :deep(a), .reader :deep(td), .reader :deep(th) { max-width: 100%; overflow-wrap: anywhere; word-break: break-word; }
.reader :deep(blockquote) { margin: 14px 0; padding: 8px 12px; border-left: 3px solid var(--td-brand-color); background: var(--td-brand-color-light); }
@media (max-width: 768px) { .related-wiki { padding-right: 0; } .graph-frame { height: 360px; } .item p { margin-right: 0; padding-right: 44px; } .relation-skeleton { margin-right: 0; padding-right: 44px; } }
</style>

<style>
.related-wiki-drawer { font-family: inherit; }
.t-drawer.related-wiki-drawer .t-drawer__content-wrapper { box-sizing: border-box; width: 560px !important; max-width: 100vw; overflow: hidden; border-left: 1px solid rgba(255,255,255,.56); background: color-mix(in srgb, var(--td-bg-color-container) 58%, transparent); box-shadow: var(--td-shadow-2); -webkit-backdrop-filter: blur(20px) saturate(112%); backdrop-filter: blur(20px) saturate(112%); }
.t-drawer.related-wiki-drawer .t-drawer__header { box-sizing: border-box; width: 100%; min-width: 0; flex: 0 0 auto; padding: 16px 30px; overflow: hidden; border-bottom: 1px solid rgba(255,255,255,.56); background: transparent; font: var(--td-font-title-small); }
.t-drawer.related-wiki-drawer .t-drawer__body { box-sizing: border-box; width: 100%; max-width: 100%; min-width: 0; min-height: 0; flex: 1 1 auto; padding: 24px 30px 0; overflow-x: hidden; overflow-y: auto; background: transparent; font-size: 14px; }
.t-drawer.related-wiki-drawer .t-drawer__close-btn { background: transparent; }
.related-wiki-drawer .t-drawer__body > * { box-sizing: border-box; width: 100%; max-width: 100%; min-width: 0; }
@media (max-width: 640px) {
  .t-drawer.related-wiki-drawer .t-drawer__content-wrapper { width: 100vw !important; }
  .t-drawer.related-wiki-drawer .t-drawer__header { padding-right: 20px; padding-left: 20px; }
  .t-drawer.related-wiki-drawer .t-drawer__body { padding-right: 20px; padding-left: 20px; }
}
</style>
