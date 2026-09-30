import assert from 'node:assert/strict'
import test from 'node:test'
import {
  clearChatRecovery,
  readChatRecovery,
  writeChatRecovery,
  type ChatRecoveryRecord,
} from './chatRecovery'

class MemoryStorage implements Storage {
  private values = new Map<string, string>()

  get length() {
    return this.values.size
  }

  clear() {
    this.values.clear()
  }

  getItem(key: string) {
    return this.values.get(key) ?? null
  }

  key(index: number) {
    return [...this.values.keys()][index] ?? null
  }

  removeItem(key: string) {
    this.values.delete(key)
  }

  setItem(key: string, value: string) {
    this.values.set(key, value)
  }
}

test('persists a question before a session exists so a hard refresh can restore it', () => {
  const storage = new MemoryStorage()
  const record: ChatRecoveryRecord = {
    scope: 'global',
    question: '刷新后仍然应该看见这条问题',
    updatedAt: 123,
  }

  writeChatRecovery(storage, 'chat-page', record)

  assert.deepEqual(readChatRecovery(storage, 'chat-page'), record)
})

test('persists the created session id and clears it after the turn is materialized', () => {
  const storage = new MemoryStorage()
  const record: ChatRecoveryRecord = {
    scope: 'video',
    question: '当前视频讲了什么',
    sessionId: 'session-1',
    videoId: 'video-1',
    videoTitle: '视频一',
    updatedAt: 123,
  }

  writeChatRecovery(storage, 'video:video-1', record)
  assert.equal(readChatRecovery(storage, 'video:video-1')?.sessionId, 'session-1')

  clearChatRecovery(storage, 'video:video-1')
  assert.equal(readChatRecovery(storage, 'video:video-1'), null)
})
