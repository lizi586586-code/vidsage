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
