interface WeKnoraStreamChunk {
  response_type?: string
  type?: string
  content?: string
  data?: Record<string, unknown>
  done?: boolean
}

export interface ParsedStreamChunk {
  kind: 'answer' | 'thinking' | 'activity' | 'complete' | 'error' | 'ignore'
  content?: string
  done?: boolean
}

export interface MergeableChatMessage {
  id: string
  sender: 'user' | 'assistant'
  text: string
  timestamp: string
}

/**
 * Agent backends may emit a streamed answer and then repeat the same answer
 * in the terminal event with a different event id. The UI renders every
 * answer event, so retain only the final answer for a completed turn.
 */
export function dedupeNativeAnswerEvents(events: Record<string, unknown>[]) {
  const answerIndexes = events
    .map((event, index) => ({ event, index }))
    .filter(({ event }) => event.type === 'answer' && String(event.content || '').trim())
  if (answerIndexes.length < 2) return events

  const completed = answerIndexes.filter(({ event }) => event.done === true)
  const keepIndex = (completed[completed.length - 1] || answerIndexes[answerIndexes.length - 1]).index
  return events.filter((event, index) => event.type !== 'answer' || index === keepIndex)
}

export function displayQuestionFromStoredContent(content: string) {
  const marker = '用户问题：'
  const index = content.lastIndexOf(marker)
  if (index < 0) return content
  return content.slice(index + marker.length).trim() || content
}

export function mergeLocalTurnWithStoredMessages<T extends MergeableChatMessage>(
  storedMessages: T[],
  localUserMessage: T,
  localAssistantMessage: T,
) {
  const finalMessages = [...storedMessages]
  let currentUserIndex = -1
  for (let index = finalMessages.length - 1; index >= 0; index -= 1) {
    const message = finalMessages[index]
    if (message.sender === 'user' && message.text.trim() === localUserMessage.text.trim()) {
      currentUserIndex = index
      break
    }
  }
  if (currentUserIndex < 0) {
    // 没有匹配的 user message
    const hasStoredUser = finalMessages.some(m => m.sender === 'user')
    if (!hasStoredUser) {
      // stored 没有 user message，移除所有 stored assistant（它们是当前问题的重复回答）
      for (let i = finalMessages.length - 1; i >= 0; i -= 1) {
        if (finalMessages[i].sender === 'assistant') {
          finalMessages.splice(i, 1)
        }
      }
    }
    finalMessages.push(localUserMessage)
    currentUserIndex = finalMessages.length - 1
  }

  // 移除当前 user message 之后所有 assistant message（空占位 / 重复），只保留第一条非空的 id/timestamp
  let preservedAssistant: T | null = null
  for (let i = finalMessages.length - 1; i > currentUserIndex; i -= 1) {
    const m = finalMessages[i]
    if (m.sender === 'assistant') {
      if (!preservedAssistant && m.text.trim()) {
        preservedAssistant = m
      }
      finalMessages.splice(i, 1)
    }
  }
  if (preservedAssistant) {
    finalMessages.push({ ...localAssistantMessage, id: preservedAssistant.id, timestamp: preservedAssistant.timestamp })
  } else {
    finalMessages.push(localAssistantMessage)
  }

  return finalMessages
}

function chunkType(data: WeKnoraStreamChunk) {
  return String(data.response_type || data.type || '').trim()
}

function streamErrorMessage(data: WeKnoraStreamChunk) {
  return String(data.content || data.data?.error || data.data?.message || '问答生成失败')
}

function completeAnswer(data: WeKnoraStreamChunk) {
  return typeof data.data?.final_answer === 'string' ? data.data.final_answer : ''
}

function toolDisplayName(value: unknown) {
  const name = String(value || '').trim()
  const labels: Record<string, string> = {
    search_knowledge: '检索知识库',
    knowledge_search: '检索知识库',
    list_knowledge_chunks: '读取知识片段',
    get_document_info: '读取文档信息',
    get_document_content: '读取文档内容',
    wiki_search: '检索 Wiki',
    wiki_read_page: '读取 Wiki 页面',
    web_search: '检索网页',
    thinking: '思考',
  }
  return labels[name] || '工具'
}

function activityFromChunk(type: string, data: WeKnoraStreamChunk) {
  const payload = data.data || {}
  if (type === 'tool_call') {
    const toolName = toolDisplayName(payload.tool_name || payload.name)
    return `正在调用 ${toolName}`
  }
  if (type === 'tool_result') {
    const toolName = toolDisplayName(payload.tool_name || payload.name)
    return payload.success === false ? `${toolName} 调用失败` : `${toolName} 调用完成`
  }
  if (type === 'agent_query') return '正在检索视频知识'
  if (type === 'references' || type === 'memory_recalled') return '已找到相关知识'
  return ''
}

function markDone<T extends ParsedStreamChunk>(chunk: T, data: WeKnoraStreamChunk): T {
  return data.done === true ? { ...chunk, done: true } : chunk
}

export function parseWeKnoraStreamChunk(raw: string): ParsedStreamChunk {
  if (!raw) return { kind: 'ignore' }
  if (raw === '[DONE]') return { kind: 'complete', done: true }
  let data: WeKnoraStreamChunk
  try {
    data = JSON.parse(raw)
  } catch {
    return { kind: 'ignore' }
  }

  const type = chunkType(data)
  if (type === 'error') return markDone({ kind: 'error', content: streamErrorMessage(data) }, data)
  if (type === 'answer') return markDone({ kind: 'answer', content: String(data.content || '') }, data)
  if (type === 'thinking' || type === 'reflection') return markDone({ kind: 'thinking', content: String(data.content || '') }, data)
  if (type === 'complete' || (!type && data.done)) return { kind: 'complete', content: completeAnswer(data), done: true }

  const activity = activityFromChunk(type, data)
  return activity ? markDone({ kind: 'activity', content: activity }, data) : markDone({ kind: 'ignore' }, data)
}

export function shouldAbortStream(raw: string) {
  if (raw === '[DONE]') return true
  if (!raw) return false
  try {
    const data = JSON.parse(raw) as WeKnoraStreamChunk
    const type = chunkType(data)
    return type === 'complete' || (!type && data.done === true)
  } catch {
    return false
  }
}
