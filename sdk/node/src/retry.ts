// The retry/backoff contract shared with the Go SDK (#36). See
// docs/proposals/2026/06/2026-06-19-node-typescript-sdk.md § "Retry / error
// contract". Behaviour here MUST stay in lockstep with that document.

const BASE_DELAY_MS = 200
const MAX_DELAY_MS = 2_000
const JITTER_RATIO = 0.25
const MAX_RETRY_AFTER_MS = 60_000

/**
 * Whether a non-2xx response should be retried (with a fresh attempt under the
 * same idempotency key, so a retry can never duplicate a row).
 *
 * Retried: 408 (request timeout), 429 (rate limited), any 5xx.
 *
 * 409 is split by error `code`, because ingest uses it for two opposite
 * idempotency outcomes:
 *   - REQUEST_IN_PROGRESS — a concurrent request with the same key is still
 *     in flight; backing off and retrying lets it finish and returns the cached
 *     result. Retried.
 *   - IDEMPOTENCY_CONFLICT — the same key was reused with a different body; this
 *     is permanent, retrying always 409s. NOT retried.
 *
 * Everything else (400, 401, 403, 404, 422, …) is a deterministic client error
 * and is never retried.
 */
export function isRetryable(status: number, code?: string): boolean {
  if (status === 408 || status === 429 || status >= 500) return true
  if (status === 409) return code === 'REQUEST_IN_PROGRESS'
  return false
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
