export const learningQuotes = [
  '学而时习之，不亦说乎？ —— 孔子《论语》',
  '学而不思则罔，思而不学则殆。 —— 孔子《论语》',
  '知之为知之，不知为不知，是知也。 —— 孔子《论语》',
  '不积跬步，无以至千里。 —— 荀子《劝学》',
  '纸上得来终觉浅，绝知此事要躬行。 —— 陆游《冬夜读书示子聿》',
  '问渠那得清如许？为有源头活水来。 —— 朱熹《观书有感》',
] as const

type QuoteStorage = Pick<Storage, 'getItem' | 'setItem'>
const storageKey = 'vidsage:learning-quote-index'

export function createLearningQuoteRotator(getStorage: () => QuoteStorage | undefined) {
  let nextIndex = 0
  let storageUnavailable = false
  return () => {
    let storage: QuoteStorage | undefined
    try {
      storage = storageUnavailable ? undefined : getStorage()
      const saved = storage?.getItem(storageKey)
      if (saved && /^\d+$/.test(saved)) {
        const index = Number(saved)
        if (Number.isInteger(index) && index < learningQuotes.length) nextIndex = index
      }
    } catch {
      // Keep rotating in memory when browser storage is unavailable.
      storageUnavailable = true
    }
    const quote = learningQuotes[nextIndex]!
    nextIndex = (nextIndex + 1) % learningQuotes.length
    try {
      storage?.setItem(storageKey, String(nextIndex))
    } catch {
      storageUnavailable = true
    }
    return quote
  }
}

export const nextLearningQuote = createLearningQuoteRotator(() =>
  typeof window === 'undefined' ? undefined : window.sessionStorage,
)
