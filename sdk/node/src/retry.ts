// The retry/backoff contract shared with the Go SDK (#36). See
// docs/proposals/2026/06/2026-06-19-node-typescript-sdk.md § "Retry / error
// contract". Behaviour here MUST stay in lockstep with that document.

const RETRYABLE_STATUS = new Set([408, 409, 429])

const BASE_DELAY_MS = 200
const MAX_DELAY_MS = 2_000
const JITTER_RATIO = 0.25
const MAX_RETRY_AFTER_MS = 60_000

/**
 * Retry on transient failures only: HTTP 408 / 409 / 429 and any 5xx.
 * Deterministic client errors (400, 401, 403, 404, 422, …) are never retried —
 * retrying wastes calls and, for the non-idempotent ingest POST, risks
 * duplicate rows.
 */
export function isRetryableStatus(status: number): boolean {
  return RETRYABLE_STATUS.has(status) || status >= 500
}

/**
 * Exponential backoff with ±{@link JITTER_RATIO} jitter, for a 0-based retry
 * attempt index. base 200ms, capped at 2s.
 */
export function backoffDelay(attempt: number, random: () => number = Math.random): number {
  const exp = Math.min(MAX_DELAY_MS, BASE_DELAY_MS * 2 ** attempt)
  const jitter = exp * JITTER_RATIO * (random() * 2 - 1)
  return Math.max(0, Math.round(exp + jitter))
}

/**
 * Parse a `Retry-After` header (delta-seconds or an HTTP-date) into a delay in
 * ms, clamped to {@link MAX_RETRY_AFTER_MS}. Returns undefined when absent or
 * unparseable, so the caller falls back to {@link backoffDelay}.
 */
export function parseRetryAfter(headers: Headers, now: number = Date.now()): number | undefined {
  const raw = headers.get('retry-after')
  if (!raw) return undefined

  const seconds = Number(raw)
  if (Number.isFinite(seconds)) {
    if (seconds < 0) return undefined
    return Math.min(MAX_RETRY_AFTER_MS, Math.round(seconds * 1000))
  }

  const dateMs = Date.parse(raw)
  if (Number.isNaN(dateMs)) return undefined
  return Math.min(MAX_RETRY_AFTER_MS, Math.max(0, dateMs - now))
}
