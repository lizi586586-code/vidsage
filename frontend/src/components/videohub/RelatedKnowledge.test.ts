import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'

test('related knowledge uses the native Wiki ego graph and Wiki deep links', () => {
  const relatedKnowledge = readFileSync(new URL('./RelatedKnowledge.vue', import.meta.url), 'utf8')

  assert.match(relatedKnowledge, /fetchNativeWikiGraph\(context\.value\.videoId\)/)
  assert.doesNotMatch(relatedKnowledge, /getWikiGraph\(/)
  assert.match(relatedKnowledge, /\/platform\/knowledge-bases\/\$\{context\.value\.knowledgeBaseId\}/)
  assert.match(relatedKnowledge, /query: \{ tab: 'wiki', slug \}/)
  assert.match(relatedKnowledge, /已关联 \$\{relatedNodes\.value\.length\} 个 Wiki 页面/)
  assert.doesNotMatch(relatedKnowledge, /KnowledgeAnchorCard|RelationOverviewCard|GraphCanvas|crossVideo/)
})
