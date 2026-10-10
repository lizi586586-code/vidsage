const RECENT_COURSES_KEY = 'vidsage_recently_played_courses'
const MAX_RECENT_COURSES = 50

type RecentCourse = { id: string; playedAt: number }

function storageKey(userId: string) {
  return `${RECENT_COURSES_KEY}:${encodeURIComponent(userId || 'anonymous')}`
}

function readRecentlyPlayedCourses(userId: string): RecentCourse[] {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(storageKey(userId)) || '[]')
    if (!Array.isArray(value)) return []
    return (value as RecentCourse[])
      .filter(item => item && typeof item.id === 'string' && Number.isFinite(item.playedAt))
      .sort((left, right) => right.playedAt - left.playedAt)
  } catch {
    return []
  }
}

export function readRecentlyPlayedCourseIds(userId: string): string[] {
  return [...new Set(readRecentlyPlayedCourses(userId).map(item => item.id))]
}

export function recordRecentlyPlayedCourse(userId: string, courseId: string, playedAt = Date.now()): void {
  if (!courseId || courseId === '__global__') return
  try {
    const records = readRecentlyPlayedCourses(userId)
      .filter(item => item.id !== courseId)
    records.unshift({ id: courseId, playedAt })
    localStorage.setItem(storageKey(userId), JSON.stringify(records.slice(0, MAX_RECENT_COURSES)))
  } catch {
    // Playback remains available when browser storage is disabled.
  }
}
