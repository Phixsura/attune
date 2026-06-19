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
 * NOT retried: 409 and the other deterministic client errors (400, 401, 403,
 * 404, 422, …). ingest's only 409 is IDEMPOTENCY_CONFLICT (same key, different
 * body) — permanent, so retrying always 409s. There is no in-progress 409:
 * concurrent same-key retries are serialized by the server's unique index (the
 * loser blocks then reads the original id), never bounced back to the client.
 */
export function isRetryable(status: number): boolean {
  return status === 408 || status === 429 || status >= 500
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
  const raw = headers.get('retry-after')?.trim()
  if (!raw) return undefined // absent, empty, or whitespace-only (Number("") is 0)

  // Integer delta-seconds only — per RFC 9110 (delta-seconds = 1*DIGIT) and to
  // stay in lockstep with the Go SDK (strconv.Atoi rejects "1.5"). A fractional
  // or otherwise non-integer value falls through to the HTTP-date branch.
  if (/^[+-]?\d+$/.test(raw)) {
    const seconds = Number(raw)
    if (seconds < 0) return undefined
    return Math.min(MAX_RETRY_AFTER_MS, seconds * 1000)
  }

  // HTTP-date branch — every valid HTTP-date contains letters (day/month names),
  // so require one. This keeps Date.parse from leniently coercing a non-integer
  // numeric like "1.5" into a bogus past date (Go's http.ParseTime rejects it too).
  if (!/[a-zA-Z]/.test(raw)) return undefined
  const dateMs = Date.parse(raw)
  if (Number.isNaN(dateMs)) return undefined
  return Math.min(MAX_RETRY_AFTER_MS, Math.max(0, dateMs - now))
}
