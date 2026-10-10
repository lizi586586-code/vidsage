import assert from 'node:assert/strict'
import { test } from 'node:test'
import { readRecentlyPlayedCourseIds, recordRecentlyPlayedCourse } from './recentlyPlayedCourses'

test('recent playback retains timestamps, moves replayed courses first and isolates users', () => {
  const values = new Map<string, string>()
  const original = Object.getOwnPropertyDescriptor(globalThis, 'localStorage')
  Object.defineProperty(globalThis, 'localStorage', { configurable: true, value: {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => values.set(key, value),
  } })
  try {
    recordRecentlyPlayedCourse('alice', 'first', 100)
    recordRecentlyPlayedCourse('alice', 'second', 200)
    recordRecentlyPlayedCourse('alice', 'third', 300)
    assert.deepEqual(readRecentlyPlayedCourseIds('alice'), ['third', 'second', 'first'])
    assert.deepEqual(JSON.parse(values.get('vidsage_recently_played_courses:alice')!), [
      { id: 'third', playedAt: 300 }, { id: 'second', playedAt: 200 }, { id: 'first', playedAt: 100 },
    ])
    recordRecentlyPlayedCourse('alice', 'first', 400)
    assert.deepEqual(readRecentlyPlayedCourseIds('alice'), ['first', 'third', 'second'])
    assert.deepEqual(readRecentlyPlayedCourseIds('bob'), [])
    values.set('vidsage_recently_played_courses:bob', 'invalid')
    assert.deepEqual(readRecentlyPlayedCourseIds('bob'), [])
  } finally {
    if (original) Object.defineProperty(globalThis, 'localStorage', original)
    else Reflect.deleteProperty(globalThis, 'localStorage')
  }
})
