import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('related knowledge uses the native Wiki ego graph and reads Wiki pages in the video detail panel', () => {
  const relatedKnowledge = readFileSync(new URL('./RelatedKnowledge.vue', import.meta.url), 'utf8')

  assert.match(relatedKnowledge, /fetchNativeWikiGraph\(context\.value\.videoId\)/)
  assert.doesNotMatch(relatedKnowledge, /getWikiGraph\(/)
  assert.match(relatedKnowledge, /getWikiPage\(context\.value\.knowledgeBaseId, slug\)/)
  assert.match(relatedKnowledge, /selectedPage/)
  assert.match(relatedKnowledge, /pageHistory/)
  assert.match(relatedKnowledge, /returnToGraph/)
  assert.match(relatedKnowledge, /在知识库中打开/)
  assert.match(relatedKnowledge, /已关联 \$\{relatedNodes\.value\.length\} 个 Wiki 页面/)
  assert.doesNotMatch(relatedKnowledge, /KnowledgeAnchorCard|RelationOverviewCard|GraphCanvas|crossVideo/)
})
