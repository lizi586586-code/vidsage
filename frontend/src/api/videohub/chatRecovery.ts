export type ChatRecoveryScope = 'global' | 'video'

export interface ChatRecoveryRecord {
  scope: ChatRecoveryScope
  question: string
  sessionId?: string
  videoId?: string
  videoTitle?: string
  title?: string
  updatedAt: number
}

export const CHAT_PAGE_RECOVERY_KEY = 'videohub:chat-page'

export function assistantRecoveryKey(scope: ChatRecoveryScope, videoId?: string) {
  return scope === 'global' ? 'videohub:assistant:global' : `videohub:assistant:video:${videoId || 'unknown'}`
}

export function getChatRecoveryStorage(): Storage | null {
  if (typeof window === 'undefined') return null
  try {
    return window.sessionStorage
  } catch {
    return null
  }
}

export function readChatRecovery(storage: Storage | null, key: string): ChatRecoveryRecord | null {
  if (!storage) return null
  try {
    const raw = storage.getItem(key)
    if (!raw) return null
    const value = JSON.parse(raw) as Partial<ChatRecoveryRecord>
    if (
      (value.scope !== 'global' && value.scope !== 'video')
      || typeof value.question !== 'string'
      || !value.question.trim()
      || typeof value.updatedAt !== 'number'
    ) {
      return null
    }
    return {
      scope: value.scope,
      question: value.question,
      ...(typeof value.sessionId === 'string' && value.sessionId ? { sessionId: value.sessionId } : {}),
      ...(typeof value.videoId === 'string' && value.videoId ? { videoId: value.videoId } : {}),
      ...(typeof value.videoTitle === 'string' && value.videoTitle ? { videoTitle: value.videoTitle } : {}),
      ...(typeof value.title === 'string' && value.title ? { title: value.title } : {}),
      updatedAt: value.updatedAt,
    }
  } catch {
    return null
  }
}

export function writeChatRecovery(storage: Storage | null, key: string, record: ChatRecoveryRecord) {
  if (!storage) return
  try {
    storage.setItem(key, JSON.stringify({
      ...record,
      question: record.question.trim(),
      updatedAt: Number.isFinite(record.updatedAt) ? record.updatedAt : Date.now(),
    }))
  } catch {
    // Storage can be unavailable in private browsing or when its quota is full.
  }
}

export function clearChatRecovery(storage: Storage | null, key: string) {
  if (!storage) return
  try {
    storage.removeItem(key)
  } catch {
    // Storage failures must not interrupt an otherwise completed chat turn.
  }
}
