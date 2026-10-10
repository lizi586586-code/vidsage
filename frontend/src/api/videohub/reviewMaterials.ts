import type { OpenMAICClassroomStatus } from './openmaicClassroom'

export const reviewUserText = '帮我定制复习材料'
export const reviewAssistantText = '按照课程材料，课程要求以及你的学习偏好，为你定制复习材料'
export const reviewRequirement = [
  '为当前课程材料生成一门巩固知识的互动课程。',
  '以课程材料为依据，结合学员教学偏好调整难度、案例和互动节奏。',
  '课程必须包含：真实可迁移的巩固案例、分层互动问答，以及可操作的互动白板活动；不要只输出文字总结。',
].join('\n')

export function reviewInstruction(requestId: string): string {
  return `请使用 openmaic-classroom Skill，调用 openmaic_start_learning_course，request_id 必须逐字复制为 ${requestId}。后台已按当前学员权限准备原始课程材料、真实记忆偏好和课程要求。仅发起此任务并告知后台自动更新进度。`
}

// This recognizes our own submitted command, never a model reply.
export function reviewRequestId(text: string): string {
  return text.match(/^请使用 openmaic-classroom Skill，调用 openmaic_start_learning_course，request_id 必须逐字复制为 ([A-Za-z0-9_-]+)。/)?.[1] || ''
}

export function visibleReviewQuestion(text: string): string {
  return reviewRequestId(text) ? reviewUserText : text
}

export function isFirstReviewRequest(messages: readonly { sender: string; text: string }[], index: number): boolean {
  const message = messages[index]
  const requestId = message?.sender === 'user' ? reviewRequestId(message.text) : ''
  return Boolean(requestId) && !messages.slice(0, index).some(item => item.sender === 'user' && reviewRequestId(item.text) === requestId)
}

export function isReviewIntent(text: string): boolean {
  return ['巩固学习', reviewUserText, reviewAssistantText].includes(text.trim())
}

export function isReviewPending(task: OpenMAICClassroomStatus | null | undefined): boolean {
  return Boolean(task && !['succeeded', 'failed'].includes(task.generation_status))
}

export function reviewCardPresentation(task?: OpenMAICClassroomStatus | null) {
  if (task?.generation_status === 'failed') return { state: 'failed', title: '复习材料生成失败', href: '' }
  if (task?.generation_status === 'succeeded') {
    let href = ''
    try {
      const url = new URL(task.classroom_url)
      if (url.protocol === 'https:' && url.host === 'open.maic.chat' && !url.username && !url.password
        && url.pathname === `/classroom/${task.classroom_id}` && task.classroom_id !== 'pending'
        && /^[A-Za-z0-9_-]+$/.test(task.classroom_id) && !url.search && !url.hash) href = url.href
    } catch { /* Only server-verified classroom links can be opened. */ }
    return { state: 'ready', title: '复习材料已生成', href }
  }
  const progress = task?.progress
  const hasProgress = typeof progress === 'number' && Number.isFinite(progress) && progress >= 0 && progress <= 100
  return {
    state: hasProgress ? 'running' : 'unknown',
    title: hasProgress ? `复习材料生成中，进度${progress}%` : '复习材料生成中，正在获取进度',
    href: '',
  }
}

export function reviewSourceKey(userId: string, requestId: string): string {
  return `openmaic-source:${userId}:${requestId}`
}
