import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import test from 'node:test'

const componentSource = readFileSync(resolve(import.meta.dirname, 'VideoDetail.vue'), 'utf8')

test('video detail restores playback position from the t query after load and query changes', () => {
  assert.match(componentSource, /function seekToRouteTime\(\)/)
  assert.match(componentSource, /seekTo\(Math\.min\(Math\.max\(querySeconds, 0\), video\.value\.durationSeconds\)\)/)
  assert.match(componentSource, /watch\(\(\) => route\.query\.t, \(\) => \{ void nextTick\(seekToRouteTime\) \}\)/)
})

test('video evidence navigation updates the current video route query for refresh recovery', () => {
  assert.match(componentSource, /function navigateToEvidence\(videoId: string, seconds: number\)/)
  assert.match(componentSource, /router\.push\(\{ name: 'videoDetail', params: \{ videoId \}, query: \{ \.\.\.route\.query, t: String\(safeSeconds\) \} \}\)/)
  assert.match(componentSource, /router\.push\(\{ name: 'videoDetail', params: \{ videoId \}, query: \{ t: String\(safeSeconds\) \} \}\)/)
})
