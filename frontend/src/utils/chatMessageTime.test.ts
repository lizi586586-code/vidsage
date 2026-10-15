import assert from 'node:assert/strict'
import test from 'node:test'
import { currentChatMessageTime, formatChatMessageTime, reviewReplyTime } from './chatMessageTime'

test('formats chat message time with zero-padded month, day and 24-hour time', () => {
  assert.equal(formatChatMessageTime(new Date(2026, 0, 2, 0, 5)), '01月02日 00:05')
  assert.equal(formatChatMessageTime(new Date(2026, 9, 15, 12, 0)), '10月15日 12:00')
  assert.equal(formatChatMessageTime(new Date(2026, 11, 31, 23, 59)), '12月31日 23:59')
})

test('keeps a valid formatted chat time stable across repeated rendering', () => {
  const timestamp = currentChatMessageTime(new Date(2026, 9, 15, 11, 39))
  assert.equal(formatChatMessageTime(timestamp), '10月15日 11:39')
  assert.equal(formatChatMessageTime(timestamp), timestamp)
})

test('hides missing, invalid and out-of-range historical timestamps', () => {
  assert.equal(formatChatMessageTime(), '')
  assert.equal(formatChatMessageTime(null), '')
  assert.equal(formatChatMessageTime('刚刚'), '')
  assert.equal(formatChatMessageTime('13月40日 25:61'), '')
  assert.equal(formatChatMessageTime('02月31日 12:00'), '')
  assert.equal(formatChatMessageTime('04月31日 12:00'), '')
  assert.equal(formatChatMessageTime('2026-02-30T12:00:00+08:00'), '')
  assert.equal(formatChatMessageTime('2026-02-29T12:00:00+08:00'), '')
  assert.equal(formatChatMessageTime('2026-10-15T24:00:00+08:00'), '')
  assert.match(formatChatMessageTime('2024-02-29T12:00:00+08:00'), /^02月29日 \d{2}:\d{2}$/u)
})

test('review cards use their assistant creation time and never borrow another turn or user time', () => {
  const user = { sender: 'user', timestamp: '10月15日 10:00' }
  const assistant = { sender: 'assistant', timestamp: '10月15日 10:01' }
  assert.equal(reviewReplyTime([user, assistant], 0), assistant.timestamp)
  assert.equal(reviewReplyTime([user, { ...assistant, timestamp: '' }], 0), '')
  assert.equal(reviewReplyTime([user, user, assistant], 0), '')
  assert.equal(reviewReplyTime([user], 0), '')
})
