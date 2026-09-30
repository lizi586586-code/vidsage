import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const here = dirname(fileURLToPath(import.meta.url))
const source = readFileSync(join(here, 'AgentStreamDisplay.vue'), 'utf8')

test('collapsed active thinking pulses in gray even on hover and focus, with reduced-motion support', () => {
  const style = source.slice(source.indexOf('/* Collapsed in-progress thinking:'))
  assert.match(style, /\.action-card\.thinking-collapsed-active:hover \.action-name/)
  assert.match(style, /\.action-card\.thinking-collapsed-active:focus-within \.action-summary/)
  assert.match(style, /color:\s*var\(--td-text-color-secondary\)/)
  assert.match(style, /background:\s*none/)
  assert.match(style, /-webkit-text-fill-color:\s*currentColor/)
  assert.match(style, /animation:\s*thinking-collapsed-status 2.2s ease-in-out infinite/)
  assert.doesNotMatch(style.split('@media')[0], /td-text-color-primary|linear-gradient|thinking-collapsed-shimmer/)
  assert.match(style, /@media \(prefers-reduced-motion: reduce\)[\s\S]*animation:\s*none/)
})

test('answer actions retain copy and artifact download but remove bookmarking', () => {
  const template = source.split('<script')[0]
  assert.doesNotMatch(template, /bookmark-add|handleAddToKnowledge|agent\.addToKnowledgeBase/)
  assert.match(template, /@click\.stop="handleCopyAnswer\(event\)"/)
  assert.match(template, /v-if="hasArtifacts"/)
  assert.match(template, /@click\.stop="openArtifactDrawer"/)
})

test('all thinking tool title overlays use 12px regardless of popup teleport', () => {
  assert.match(source, /const thinkingTooltipStyle = \{ fontSize: '12px', lineHeight: '20px' \}/)
  assert.equal((source.match(/:overlay-inner-style="thinkingTooltipStyle"/g) || []).length, 4)
  const titleTooltips = source.match(/<t-tooltip[^>]*:content="(?:t\('agent.updatePlan'\)|getToolTitle\(event\))"[^>]*>/g) || []
  assert.equal(titleTooltips.length, 4)
  for (const tooltip of titleTooltips) assert.match(tooltip, /:overlay-inner-style="thinkingTooltipStyle"/)
})

test('wider thinking rows keep tools and memory on the same axis', () => {
  assert.match(source, /--agent-step-indent:\s*30px/)
  assert.match(source, /\.tree-child\s*\{[^}]*padding-left:\s*var\(--agent-step-indent\)/s)
  assert.match(source, /\.tree-child-content\s*\{[^}]*min-width:\s*0[^}]*width:\s*100%/s)
  assert.match(source, /\.tree-child \.action-title-icon\s*\{[^}]*left:\s*calc\(-1 \* var\(--agent-step-indent\)\)/s)
  const memory = readFileSync(join(here, 'ChatMemoryStep.vue'), 'utf8')
  assert.match(memory, /left:\s*calc\(-1 \* var\(--agent-step-indent, 42px\)\)/)
})

test('agent steps use compact muted timeline styling', () => {
  assert.match(source, /--agent-step-text-size:\s*12px/)
  assert.match(source, /--agent-step-summary-size:\s*12px/)
  assert.match(source, /--agent-step-icon-color:\s*var\(--td-text-color-placeholder\)/)
  assert.match(source, /max-height:\s*none/)
  assert.match(source, /overflow-y:\s*visible/)
  assert.match(source, /\.tree-root \.action-name\s*\{[\s\S]*font-size:\s*12px/)
  assert.match(source, /\.tree-child \.action-title-icon\s*\{[\s\S]*position:\s*absolute/)
  assert.match(source, /function maskIconStyle\(src: string, size = 18\)/)
  assert.match(source, /\.icon-mask\s*\{[\s\S]*background-color:\s*var\(--agent-step-icon-color\)/)
  assert.match(source, /\.action-title \.action-title-icon\s*\{[\s\S]*width:\s*18px/)
  assert.doesNotMatch(source, /\.action-title \.action-title-icon,\s*\n\s*\.icon-mask\s*\{/)
})

test('thinking rows use one 14px text and icon size', () => {
  assert.match(source, /--agent-thinking-text-size:\s*14px/)
  assert.match(source, /--agent-thinking-icon-size:\s*18px/)
  assert.match(source, /\.thinking-event-card,[\s\S]*\.thinking-tool-card[\s\S]*font-size:\s*var\(--agent-thinking-text-size\)/)
  assert.match(source, /\.thinking-event-card,[\s\S]*\.action-title-icon\.icon-mask,[\s\S]*width:\s*var\(--agent-thinking-icon-size\)/)
  assert.match(source, /maskIconStyle\(thinkingIcon, 18\)/)
  assert.doesNotMatch(source, /event\.tool_data\.thought_number/)
})

test('expanded agent step log keeps model thinking in the tool timeline', () => {
  assert.match(source, /visibleIntermediateEvents\s*=\s*computed\(\(\) => intermediateEvents\.value\)/)
  assert.match(source, /v-for="\(event, index\) in visibleIntermediateEvents"/)
})

test('streaming log renders reasoning alongside tool calls', () => {
  assert.match(source, /if \(!isConversationDone\.value\)\s*\{\s*return result;\s*\}/)
})

test('streaming thinking stays collapsed until the user expands it', () => {
  const watcher = source.slice(
    source.indexOf('watch(eventStream'),
    source.indexOf('// Once the steps collapse'),
  )
  assert.doesNotMatch(watcher, /expandedEvents\.value\.(add|delete)/)
  assert.match(source, /const toggleEvent = \(eventId: string\) => \{[\s\S]*expandedEvents\.value\.has\(eventId\)/)
})

test('expanded model reasoning stays inline without a separate thinking title', () => {
  assert.match(source, /class="thinking-inline-content markdown-content"/)
  assert.match(source, /class="thinking-inline-markdown" v-html="renderThinkingMarkdownContent\(event\.content\)"/)
  assert.match(source, /event\.title && event\.content && isEventExpanded\(event\.event_id\)/)
  assert.match(source, /\.thinking-inline-title\s*\{[\s\S]*align-items:\s*flex-start/)
  assert.match(source, /\.thinking-inline-content\s*\{[\s\S]*margin-top:\s*0/)
  assert.doesNotMatch(source, /\.thinking-inline-title > \.action-title-icon/)
  assert.match(source, /\.tree-child \.thinking-event-card \.action-title\s*\{[\s\S]*position:\s*static/)
})

test('streaming tool log uses the same timeline structure', () => {
  assert.match(source, /'is-streaming-timeline': showStreamingTimeline/)
  assert.match(source, /'tree-child': isStreamingTimelineEvent\(event\)/)
  assert.match(source, /class="tree-child tree-child-last streaming-loading-node"/)
  assert.match(source, /chat-timeline-loading\.less/)
  assert.match(source, /lastStreamingTimelineEventIndex\s*=\s*computed/)
})

test('final done row uses an existing common translation key', () => {
  assert.match(source, /t\('common\.finish'\)/)
  assert.doesNotMatch(source, /\$t\('common\.done'\)/)
  assert.match(source, /'tree-child-last': !isConversationDone && index === visibleIntermediateEvents\.length - 1/)
})

test('tool rows use line icon names instead of legacy asset masks', () => {
  assert.match(source, /getAgentToolIconName/)
  assert.match(source, /:name="getToolIconName\(event\.tool_name\)"/)
  assert.match(source, /wiki_search: 'agentEditor\.tools\.wikiSearch'/)
  assert.match(source, /wiki_read_page: 'agentEditor\.tools\.wikiReadPage'/)
  assert.match(source, /wiki_read_source_doc: 'agentStream\.tools\.wikiReadSourceDoc'/)
  assert.match(source, /toolName === 'get_document_content' \|\| toolName === 'wiki_read_source_doc'/)
  assert.doesNotMatch(source, /getToolIcon\(event\.tool_name\)/)
})

test('rag mode delegates pre-answer loading to pipeline and adds no row after answer starts', () => {
  assert.match(source, /if \(props\.ragMode \|\| hasAnswerStarted\.value\) return false/)
  assert.match(source, /v-if="!ragMode \|\| displayEvents\.length > 0 \|\| showAgentActivityIndicator"/)
  assert.doesNotMatch(source, /ChatActivityIndicator/)
})

test('rag mode keeps model thinking out of the answer stream component', () => {
  const displayEventsBlock = source.slice(
    source.indexOf('const displayEvents = computed'),
    source.indexOf('// Get unique key for event'),
  )
  assert.match(displayEventsBlock, /if \(props\.ragMode\)\s*\{[\s\S]*e\.type === 'answer'/)
  assert.doesNotMatch(
    displayEventsBlock,
    /attachment_parsing/,
  )
  assert.doesNotMatch(
    displayEventsBlock,
    /if \(props\.ragMode\)\s*\{[\s\S]*e\.type === 'answer' \|\| e\.type === 'thinking'/,
  )
})

test('only the collapsed root summary shows an expand chevron', () => {
  assert.match(source, /tree-root-summary[\s\S]*class="action-show-icon"/)
  assert.match(source, /showIntermediateSteps \? 'chevron-down' : 'chevron-right'/)
  assert.doesNotMatch(source, /isEventExpanded\(event\.tool_call_id\) \? 'chevron/)
  assert.doesNotMatch(source, /isEventExpanded\(event\.event_id\) \? 'chevron/)
})

// Recalled memory is one more thing the turn did before answering, so it rides
// the same timeline as the steps instead of sitting in a card above them. It has
// to travel into the collapsed tree with them, and appear exactly once.
test('recalled memory rides the agent timeline as its leading row', () => {
  const template = source.split('<script')[0]
  assert.equal((template.match(/<ChatMemoryStep/g) || []).length, 2)
  assert.match(template, /<div v-if="showIntermediateSteps" class="tree-children">\s*\n\s*<ChatMemoryStep/)
  assert.match(template, /<ChatMemoryStep\s*\n\s*v-if="showMemoryRow"/)
  assert.match(template, /<ChatMemoryStep[\s\S]*?:is-last="memoryIsLast"/)
  assert.match(source, /!props\.ragMode && hasMemory\.value && !shouldShowCollapsedSteps\.value/)
  assert.match(
    source,
    /const memoryIsLast = computed\([\s\S]*lastStreamingTimelineEventIndex\.value === -1/,
  )
})

test('pending tool rows do not render an extra axis dot', () => {
  assert.doesNotMatch(source, /&\.action-pending\s*\{[\s\S]*&::after/)
})

test('agent mode shows a native placeholder before answer whenever nothing is pending', () => {
  assert.match(source, /if \(isConversationDone\.value\) return false/)
  assert.match(source, /return !hasPendingStreamingActivity\.value/)
  assert.match(source, /const hasPendingStreamingActivity = computed/)
  assert.match(source, /event\.thinking === true \|\| isThinkingActive\(event\.event_id\)/)
  assert.match(source, /event\.type === 'tool_approval_required' \|\| event\.type === 'mcp_oauth_required'/)
  assert.match(source, /class="action-card action-pending thinking-collapsed-active"/)
  assert.match(source, /t\('chat\.thinkingAlt'\)/)
  assert.match(source, /chat-timeline-loading\.less/)
})
