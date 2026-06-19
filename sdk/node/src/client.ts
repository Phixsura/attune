import { AttuneError, TransportErrorCode } from './errors'
import type { ErrorResponse } from './proto/attune/v1/common'
import type { IngestRequest, IngestResponse } from './proto/attune/v1/ingest'
import { backoffDelay, isRetryable, parseRetryAfter } from './retry'

/**
 * Caller-facing ingest payload. Derived from the proto-generated
 * {@link IngestRequest} (no hand-written fields): `content` is required, every
 * other wire field is optional — the server fills defaults (e.g. `source` →
 * "api"). Sent verbatim as the JSON body.
 */
export type IngestInput = Pick<IngestRequest, 'content'> & Partial<Omit<IngestRequest, 'content'>>

/** A `fetch`-compatible function. Defaults to the runtime's global `fetch`. */
export type FetchLike = (input: string, init: RequestInit) => Promise<Response>

export interface ClientOptions {
  /** Base URL of the attune deployment, e.g. `https://attune.example.com`. */
  baseURL: string
  /**
   * API key with the `ingest:write` scope. Sent as the `X-API-Key` header.
   * `ingest:write` keys are publishable (browser-safe) — see the package README.
   */
  apiKey: string
  /** Inject a custom `fetch` (older runtimes, tests). Defaults to `globalThis.fetch`. */
  fetch?: FetchLike
  /** Per-attempt timeout in milliseconds. Default 30000. A timeout is retryable. */
  timeout?: number
  /** Max retries on transient failures (1 initial try + N retries). Default 2. */
  maxRetries?: number
  /** @internal Override the inter-attempt sleep (tests). */
  sleep?: (ms: number) => Promise<void>
}

export interface RequestOptions {
  /** Caller cancellation. An aborted request throws `AttuneError` code "ABORTED". */
  signal?: AbortSignal
  /**
   * Dedup token sent as the `Idempotency-Key` header. Defaults to a fresh UUID
   * per call, held stable across that call's retries so a retried at-least-once
   * delivery cannot create a duplicate feedback row. Pass your own to make an
   * ingest idempotent across separate `ingest()` calls.
   */
  idempotencyKey?: string
}

const DEFAULT_TIMEOUT_MS = 30_000
const DEFAULT_MAX_RETRIES = 2
const API_KEY_HEADER = 'X-API-Key'
const INGEST_PATH = '/v1/feedback/ingest'

const defaultSleep = (ms: number): Promise<void> =>
  new Promise((resolve) => setTimeout(resolve, ms))

/**
 * Client for the attune ingest API.
 *
 * ```ts
 * const client = new Client({ baseURL, apiKey });
 * const { id } = await client.ingest({ content: "love it" });
 * ```
 */
export class Client {
  readonly #baseURL: string
  readonly #apiKey: string
  readonly #fetch: FetchLike
  readonly #timeout: number
  readonly #maxRetries: number
  readonly #sleep: (ms: number) => Promise<void>

  constructor(options: ClientOptions) {
    if (!options.baseURL)
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'baseURL is required' })
    if (!options.apiKey)
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'apiKey is required' })

    const fetchImpl = options.fetch ?? (globalThis.fetch as FetchLike | undefined)
    if (!fetchImpl) {
      throw new AttuneError({
        code: 'BAD_REQUEST',
        message: 'no global fetch available; pass a `fetch` implementation in ClientOptions',
      })
    }

    this.#baseURL = options.baseURL.replace(/\/+$/, '')
    this.#apiKey = options.apiKey
    this.#fetch = fetchImpl
    this.#timeout = options.timeout ?? DEFAULT_TIMEOUT_MS
    this.#maxRetries = options.maxRetries ?? DEFAULT_MAX_RETRIES
    this.#sleep = options.sleep ?? defaultSleep
  }

  /** Ingest one feedback item. Resolves with the stored row id, or throws `AttuneError`. */
  ingest(input: IngestInput, options?: RequestOptions): Promise<IngestResponse> {
    // One key per call, reused across this call's retries — that is what makes
    // a retry safe against the non-idempotent ingest POST.
    const idempotencyKey = options?.idempotencyKey ?? randomIdempotencyKey()
    return this.#request<IngestResponse>(INGEST_PATH, input, idempotencyKey, options?.signal)
  }

  async #request<T>(
    path: string,
    body: unknown,
    idempotencyKey: string,
    userSignal?: AbortSignal,
  ): Promise<T> {
    const url = this.#baseURL + path
    const payload = JSON.stringify(body) // serialize once, reused across retries
    let lastError: AttuneError | undefined

    for (let attempt = 0; attempt <= this.#maxRetries; attempt++) {
      if (userSignal?.aborted) {
        throw new AttuneError({ code: TransportErrorCode.Aborted, message: 'request aborted' })
      }

      let response: Response
      try {
        response = await this.#fetchOnce(url, payload, idempotencyKey, userSignal)
      } catch (err) {
        const transportError = err as AttuneError
        // A caller-initiated abort is intentional — never retry it.
        if (transportError.code === TransportErrorCode.Aborted) throw transportError
        lastError = transportError
        if (attempt < this.#maxRetries) {
          await this.#sleep(backoffDelay(attempt))
          continue
        }
        throw transportError
      }

      if (response.ok) {
        return (await response.json()) as T
      }

      const error = AttuneError.fromResponse(
        response.status,
        await readErrorBody(response),
        response.headers,
      )
      if (isRetryable(response.status) && attempt < this.#maxRetries) {
        lastError = error
        await this.#sleep(parseRetryAfter(response.headers) ?? backoffDelay(attempt))
        continue
      }
      throw error
    }

    throw lastError ?? new AttuneError({ code: 'INTERNAL', message: 'request failed' })
  }

  // One fetch attempt with a per-attempt timeout and caller-abort forwarding,
  // built on AbortController only (portable across Node 20+ and every browser
  // with AbortController — avoids AbortSignal.any/.timeout, which need Node 20.3
  // and very recent browsers). Throws a typed transport AttuneError on failure.
  async #fetchOnce(
    url: string,
    payload: string,
    idempotencyKey: string,
    userSignal?: AbortSignal,
  ): Promise<Response> {
    const controller = new AbortController()
    let timedOut = false
    const timer = setTimeout(() => {
      timedOut = true
      controller.abort()
    }, this.#timeout)
    const onUserAbort = () => controller.abort()
    if (userSignal) userSignal.addEventListener('abort', onUserAbort, { once: true })

    try {
      return await this.#fetch(url, {
        method: 'POST',
        headers: {
          'content-type': 'application/json',
          [API_KEY_HEADER]: this.#apiKey,
          'Idempotency-Key': idempotencyKey,
        },
        body: payload,
        signal: controller.signal,
      })
    } catch (cause) {
      if (userSignal?.aborted) {
        throw new AttuneError({
          code: TransportErrorCode.Aborted,
          message: 'request aborted',
          cause,
        })
      }
      if (timedOut) {
        throw new AttuneError({
          code: TransportErrorCode.Timeout,
          message: 'request timed out',
          cause,
        })
      }
      throw new AttuneError({ code: TransportErrorCode.Network, message: 'network error', cause })
    } finally {
      clearTimeout(timer)
      if (userSignal) userSignal.removeEventListener('abort', onUserAbort)
    }
  }
}

/** Best-effort parse of the unified ErrorResponse envelope; undefined on any failure. */
async function readErrorBody(response: Response): Promise<ErrorResponse | undefined> {
  try {
    return (await response.json()) as ErrorResponse
  } catch {
    return undefined
  }
}

// A dedup token valid against the server's key format ([A-Za-z0-9_-]{8,64}).
// crypto.randomUUID needs a secure context (https/localhost), so a browser
// widget served over plain http would otherwise throw — fall back gracefully.
function randomIdempotencyKey(): string {
  const c: Crypto | undefined = globalThis.crypto
  if (typeof c?.randomUUID === 'function') return c.randomUUID()
  if (typeof c?.getRandomValues === 'function') {
    const b = c.getRandomValues(new Uint8Array(16))
    b[6] = ((b[6] ?? 0) & 0x0f) | 0x40 // version 4
    b[8] = ((b[8] ?? 0) & 0x3f) | 0x80 // variant
    const hex = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
    return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
  }
  // Last resort: uniqueness only (no Web Crypto at all). Idempotency keys need
  // to be unique per call, not cryptographically strong.
  return `idem-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`
}
