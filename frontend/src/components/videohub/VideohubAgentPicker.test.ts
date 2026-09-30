import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const componentSource = readFileSync(resolve(import.meta.dirname, 'VideohubAgentPicker.vue'), 'utf8')

test('video assistant agent picker exposes auto-routing as the unselected state', () => {
  assert.match(componentSource, /autoRouteAgent/)
  assert.match(componentSource, /!currentAgentId/)
  assert.match(componentSource, /autoRoute:|agentPicker\.autoRoute/)
})

test('video assistant agent picker keeps quick answer as an explicit option', () => {
  assert.match(componentSource, /BUILTIN_QUICK_ANSWER_ID/)
  assert.match(componentSource, /agentPicker\.quickAnswer/)
})

test('tool appearance is text-only and uses the shared shortened auto label', () => {
  const trigger = componentSource.match(/<button[\s\S]*?<\/button>/)?.[0] || ''
  const icons = trigger.match(/<t-icon[^>]*>/g) || []
  assert.equal(icons.length, 2)
  for (const icon of icons) assert.match(icon, /v-if="props\.appearance !== 'tool'"/)
  assert.doesNotMatch(trigger, /map-route-planning/)
  assert.match(readFileSync(resolve(import.meta.dirname, '../../i18n/locales/zh-CN.ts'), 'utf8'), /autoRoute: '自动'/)
  assert.match(readFileSync(resolve(import.meta.dirname, '../../i18n/locales/en-US.ts'), 'utf8'), /autoRoute: 'Auto'/)
})
