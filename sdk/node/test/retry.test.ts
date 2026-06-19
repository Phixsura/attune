import { describe, expect, it } from 'vitest'
import { backoffDelay, isRetryable, parseRetryAfter } from '../src/retry'

describe('isRetryable', () => {
  it('retries 408/429 and all 5xx', () => {
    for (const s of [408, 429, 500, 502, 503, 504]) expect(isRetryable(s)).toBe(true)
  })
  it('does not retry deterministic 4xx, including 409 (idempotency conflict)', () => {
    for (const s of [400, 401, 403, 404, 409, 422]) expect(isRetryable(s)).toBe(false)
  })
})

describe('backoffDelay', () => {
  it('grows exponentially and is capped at 2s', () => {
    const noJitter = () => 0.5 // (0.5*2-1) = 0 → no jitter
    expect(backoffDelay(0, noJitter)).toBe(200)
    expect(backoffDelay(1, noJitter)).toBe(400)
    expect(backoffDelay(2, noJitter)).toBe(800)
    expect(backoffDelay(10, noJitter)).toBe(2000) // capped
  })
  it('applies bounded jitter within ±25%', () => {
    expect(backoffDelay(2, () => 0)).toBe(600) // -25% of 800
    expect(backoffDelay(2, () => 1)).toBe(1000) // +25% of 800
  })
})

describe('parseRetryAfter', () => {
  it('parses delta-seconds to ms', () => {
    expect(parseRetryAfter(new Headers({ 'retry-after': '2' }))).toBe(2000)
    expect(parseRetryAfter(new Headers({ 'retry-after': '0' }))).toBe(0)
  })
  it('parses an HTTP-date relative to now', () => {
    const now = 1_000_000
    const future = new Date(now + 5000).toUTCString()
    expect(parseRetryAfter(new Headers({ 'retry-after': future }), now)).toBe(
      Date.parse(future) - now,
    )
  })
  it('returns undefined when absent or unparseable', () => {
    expect(parseRetryAfter(new Headers())).toBeUndefined()
    expect(parseRetryAfter(new Headers({ 'retry-after': 'soon' }))).toBeUndefined()
    expect(parseRetryAfter(new Headers({ 'retry-after': '-3' }))).toBeUndefined()
  })
})
