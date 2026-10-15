const FORMATTED_CHAT_TIME = /^(\d{2})月(\d{2})日 (\d{2}):(\d{2})$/u
const SOURCE_CHAT_TIME = /^(\d{4})-(\d{2})-(\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2})(?:\.\d+)?)?(?:Z|[+-]\d{2}:\d{2})?)?$/u

function pad(value: number) {
  return String(value).padStart(2, '0')
}

function isValidCalendarDay(year: number, month: number, day: number) {
  const leapYear = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
  const days = [31, leapYear ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  return month >= 1 && month <= 12 && day >= 1 && day <= days[month - 1]
}

function isValidFormattedChatTime(value: string) {
  const match = value.match(FORMATTED_CHAT_TIME)
  if (!match) return false
  const [, month, day, hour, minute] = match.map(Number)
  return isValidCalendarDay(2000, month, day)
    && hour >= 0 && hour <= 23
    && minute >= 0 && minute <= 59
}

export function formatChatMessageTime(value?: string | number | Date | null) {
  if (value === undefined || value === null || value === '') return ''
  if (typeof value === 'string') {
    if (isValidFormattedChatTime(value)) return value
    const match = value.match(SOURCE_CHAT_TIME)
    if (!match) return ''
    const [, year, month, day, hour = '0', minute = '0', second = '0'] = match
    if (!isValidCalendarDay(Number(year), Number(month), Number(day))
      || Number(hour) > 23 || Number(minute) > 59 || Number(second) > 59) return ''
  }
  const date = value instanceof Date ? value : new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return `${pad(date.getMonth() + 1)}月${pad(date.getDate())}日 ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function currentChatMessageTime(now: number | Date = Date.now()) {
  return formatChatMessageTime(now)
}

// Structured review cards replace the assistant message following the request.
export function reviewReplyTime(messages: readonly { sender: string; timestamp: string }[], requestIndex: number) {
  for (const message of messages.slice(requestIndex + 1)) {
    if (message.sender === 'user') break
    if (message.sender === 'assistant') return formatChatMessageTime(message.timestamp)
  }
  return ''
}
