import assert from 'node:assert/strict'
import test from 'node:test'
import { createLearningQuoteRotator, learningQuotes } from './learningQuotes'

function memoryStorage(initial?: string) {
  let value = initial ?? null
  return {
    getItem: () => value,
    setItem: (_key: string, next: string) => { value = next },
  }
}

test('learning quotes cycle without adjacent repeats', () => {
  assert.ok(learningQuotes.every(quote => quote.includes('——')))
  const next = createLearningQuoteRotator(() => undefined)
  assert.deepEqual(Array.from({ length: learningQuotes.length }, next), [...learningQuotes])
  assert.equal(next(), learningQuotes[0])
  assert.equal(new Set(learningQuotes).size, learningQuotes.length)
})

test('quote rotation continues when the assistant is recreated in the same tab', () => {
  const storage = memoryStorage()
  const firstWindow = createLearningQuoteRotator(() => storage)
  assert.equal(firstWindow(), learningQuotes[0])
  assert.equal(createLearningQuoteRotator(() => storage)(), learningQuotes[1])
  assert.equal(firstWindow(), learningQuotes[2])
})

test('invalid stored indices cannot produce a missing quote', () => {
  for (const value of ['-1', '1.5', 'NaN', 'null', '999999999999999999999', String(learningQuotes.length), '']) {
    const next = createLearningQuoteRotator(() => memoryStorage(value))
    assert.equal(next(), learningQuotes[0])
  }
})

test('blocked browser storage falls back to in-memory rotation', () => {
  const next = createLearningQuoteRotator(() => { throw new Error('blocked') })
  assert.equal(next(), learningQuotes[0])
  assert.equal(next(), learningQuotes[1])
})

test('failed storage reads and writes do not repeat the last stored quotation', () => {
  for (const storage of [
    { getItem: () => { throw new Error('blocked') }, setItem: () => {} },
    { getItem: () => '2', setItem: () => { throw new Error('quota') } },
  ]) {
    const next = createLearningQuoteRotator(() => storage)
    const first = next()
    const second = next()
    assert.notEqual(second, first)
    assert.ok(learningQuotes.includes(second))
  }
})
