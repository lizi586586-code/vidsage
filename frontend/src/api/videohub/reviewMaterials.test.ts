import assert from 'node:assert/strict'
import test from 'node:test'
import { isFirstReviewRequest, isReviewIntent, reviewAssistantText, reviewCardPresentation, reviewInstruction, reviewRequestId, visibleReviewQuestion } from './reviewMaterials'
import type { OpenMAICClassroomStatus } from './openmaicClassroom'

const task: OpenMAICClassroomStatus = { request_id: 'request-1', generation_status: 'running', classroom_id: 'stage-test', classroom_url: 'https://open.maic.chat/classroom/stage-test', status: 'not_started' }
test('queued retries keep one card per request without hiding later independent tasks', () => {
  const messages = [
    { sender: 'user', text: reviewInstruction('request-1') },
    { sender: 'assistant', text: '已发起' },
    { sender: 'user', text: reviewInstruction('request-1') },
    { sender: 'user', text: reviewInstruction('request-2') },
    { sender: 'user', text: '普通问题' },
  ]
  assert.deepEqual(messages.map((_, index) => isFirstReviewRequest(messages, index)), [true, false, false, true, false])
})
test('only our internal command is replaced with the approved user copy', () => {
  const id = 'db623132-186c-457e-b515-1424a4fbe868'
  assert.equal(reviewRequestId(reviewInstruction(id)), id)
  assert.equal(visibleReviewQuestion(reviewInstruction(id)), '帮我定制复习材料')
  assert.equal(visibleReviewQuestion('正常课程问题'), '正常课程问题')
  assert.ok(isReviewIntent('巩固学习'))
  assert.ok(isReviewIntent('帮我定制复习材料'))
  assert.equal(isReviewIntent('请总结巩固学习的方法'), false)
  assert.equal(reviewAssistantText, '按照课程材料，课程要求以及你的学习偏好，为你定制复习材料')
})
test('real zero and fractional progress are shown; missing or invalid progress stays unknown', () => {
  for (const progress of [0, 32, 46.5, 100]) {
    assert.equal(reviewCardPresentation({ ...task, progress }).title, `复习材料生成中，进度${progress}%`)
  }
  for (const progress of [undefined, null, NaN, Infinity, -1, 101]) {
    assert.deepEqual(reviewCardPresentation({ ...task, progress }), { state: 'unknown', title: '复习材料生成中，正在获取进度', href: '' })
  }
})
test('terminal status overrides progress and only verified completed classroom URLs can open', () => {
  assert.deepEqual(reviewCardPresentation({ ...task, generation_status: 'failed', progress: 50 }), { state: 'failed', title: '复习材料生成失败', href: '' })
  assert.equal(reviewCardPresentation(task).href, '')
  assert.deepEqual(reviewCardPresentation({ ...task, generation_status: 'succeeded', progress: 100 }), { state: 'ready', title: '复习材料已生成', href: task.classroom_url })
  for (const classroom_url of ['javascript:alert(1)', 'https://evil.test/classroom/stage-test', 'https://open.maic.chat/classroom/other', 'https://open.maic.chat/classroom/stage-test?redirect=x', 'https://user@open.maic.chat/classroom/stage-test']) {
    assert.equal(reviewCardPresentation({ ...task, generation_status: 'succeeded', classroom_url }).href, '')
  }
})
