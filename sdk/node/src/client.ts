import { AttuneError, TransportErrorCode } from './errors'
import type { ErrorResponse } from './proto/attune/v1/common'
import type { IngestRequest, IngestResponse } from './proto/attune/v1/ingest'
import type {
  ArchiveTagResponse,
  CreateTagRequest,
  ListTagsResponse,
  Tag,
  UpdateTagRequest,
} from './proto/attune/v1/tag'
import type {
  ArchiveStateResponse,
  CreateStateRequest,
  CreateStateResponse,
  ListStatesResponse,
  ListTransitionsResponse,
  ReplaceTransitionsRequest,
  ReplaceTransitionsResponse,
  SeedDefaultsResponse,
  UpdateStateRequest,
  UpdateStateResponse,
} from './proto/attune/v1/workflow'
import { backoffDelay, isRetryable, parseRetryAfter } from './retry'
import { VERSION } from './version'

// Versioned client identifier, sent as User-Agent so the server can attribute
// SDK traffic by version (support, deprecation, abuse triage). Includes the
// Node version when running on Node; browsers ignore a custom User-Agent.
const USER_AGENT =
  typeof process !== 'undefined' && process.versions?.node
    ? `attune-node/${VERSION} node/${process.versions.node}`
    : `attune-node/${VERSION}`

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
  /**
   * Extra headers added to every request (e.g. a proxy token or a trace header).
   * Reserved headers (`content-type`, `X-API-Key`, `Idempotency-Key`,
   * `User-Agent`) take precedence and cannot be overridden here.
   */
  defaultHeaders?: Record<string, string>
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
// Reserved headers the client always controls. A consumer's `defaultHeaders`
// must never override these — matched case-INSENSITIVELY, since the WHATWG
// `Headers` constructor lowercases names and would otherwise *concatenate* a
// case-variant duplicate (e.g. `content-type` + `Content-Type`) into one
// malformed header. Keys are stored lowercased for comparison.
const RESERVED_HEADERS = new Set(['content-type', 'x-api-key', 'idempotency-key', 'user-agent'])

// A path-segment id must be non-empty and must not be a dot-segment or contain a
// slash: encodeURIComponent leaves '.' untouched, so `archiveTag('..')` would
// otherwise build `/v1/tags/..` and resolve to a different path (parent walk /
// extra segment) once fetch normalizes the URL.
function isValidPathSegment(id: string): boolean {
  return !!id && id !== '.' && id !== '..' && !id.includes('/')
}
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
  readonly #defaultHeaders: Record<string, string>
  readonly #sleep: (ms: number) => Promise<void>

  constructor(options: ClientOptions) {
    // Guard against JS callers (no type-checking) doing `new Client()` /
    // `new Client(null)` — give a clear AttuneError, not a raw TypeError.
    if (options == null || typeof options !== 'object') {
      throw new AttuneError({
        code: 'BAD_REQUEST',
        message: 'Client requires an options object: { baseURL, apiKey }',
      })
    }
    if (!options.baseURL)
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'baseURL is required' })
    // Validate at construction (parity with the Go SDK), not lazily at fetch time.
    let parsedBase: URL
    try {
      parsedBase = new URL(options.baseURL)
    } catch {
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'invalid baseURL' })
    }
    if (parsedBase.protocol !== 'http:' && parsedBase.protocol !== 'https:')
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'baseURL must be http or https' })
    // `new URL` coerces host-less inputs ("http:foo", "http:///path") into a
    // bogus host instead of failing. Require a real `scheme://host` authority so
    // these are rejected (Go's url.Parse rejects them via Host=="") rather than
    // silently sending every request to the wrong host.
    if (!/^https?:\/\/[^/]/i.test(options.baseURL))
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'invalid baseURL' })
    if (!options.apiKey)
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'apiKey is required' })
    if (hasHeaderControlChar(options.apiKey))
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'apiKey contains invalid characters' })

    const fetchImpl = options.fetch ?? (globalThis.fetch as FetchLike | undefined)
    if (!fetchImpl) {
      throw new AttuneError({
        code: 'BAD_REQUEST',
        message: 'no global fetch available; pass a `fetch` implementation in ClientOptions',
      })
    }

    this.#baseURL = stripTrailingSlashes(options.baseURL)
    this.#apiKey = options.apiKey
    this.#fetch = fetchImpl
    // timeout <= 0 disables the per-attempt timeout (treat as "no limit") rather
    // than aborting instantly; negative maxRetries clamps to 0 (one attempt).
    this.#timeout = options.timeout ?? DEFAULT_TIMEOUT_MS
    this.#maxRetries = Math.max(0, options.maxRetries ?? DEFAULT_MAX_RETRIES)
    // Drop any reserved header (case-insensitively) so a consumer's casing
    // variant can't slip past and get concatenated by `Headers` at fetch time.
    this.#defaultHeaders = {}
    for (const [k, v] of Object.entries(options.defaultHeaders ?? {})) {
      if (!RESERVED_HEADERS.has(k.toLowerCase())) this.#defaultHeaders[k] = v
    }
    this.#sleep = options.sleep ?? defaultSleep
  }

  /** Ingest one feedback item. Resolves with the stored row id, or throws `AttuneError`. */
  ingest(input: IngestInput, options?: RequestOptions): Promise<IngestResponse> {
    // One key per call, reused across this call's retries — that is what makes
    // a retry safe against the non-idempotent ingest POST.
    const idempotencyKey = options?.idempotencyKey ?? randomIdempotencyKey()
    if (hasHeaderControlChar(idempotencyKey)) {
      return Promise.reject(
        new AttuneError({
          code: 'BAD_REQUEST',
          message: 'idempotencyKey contains invalid characters',
        }),
      )
    }
    return this.#request<IngestResponse>(
      'POST',
      INGEST_PATH,
      input,
      idempotencyKey,
      options?.signal,
    )
  }

  // ── Tags (needs a key with the tags:read / tags:write scope) ──────────────

  /** List the tenant's tags. */
  listTags(opts?: { includeArchived?: boolean; signal?: AbortSignal }): Promise<ListTagsResponse> {
    const q = opts?.includeArchived ? '?include_archived=true' : ''
    return this.#request<ListTagsResponse>('GET', `/v1/tags${q}`, undefined, '', opts?.signal)
  }

  /** Create a tag. */
  createTag(req: CreateTagRequest, opts?: { signal?: AbortSignal }): Promise<Tag> {
    return this.#request<Tag>('POST', '/v1/tags', req, '', opts?.signal)
  }

  /**
   * Update a tag by id. **Replace-semantics**: send the full desired state —
   * any writable field you omit (name, color, description) is overwritten, not
   * preserved, so a partial update silently clears the omitted fields.
   */
  updateTag(req: UpdateTagRequest, opts?: { signal?: AbortSignal }): Promise<Tag> {
    if (!isValidPathSegment(req.id))
      return Promise.reject(new AttuneError({ code: 'BAD_REQUEST', message: 'tag id is invalid' }))
    return this.#request<Tag>(
      'PATCH',
      `/v1/tags/${encodeURIComponent(req.id)}`,
      req,
      '',
      opts?.signal,
    )
  }

  /** Archive a tag by id. */
  archiveTag(id: string, opts?: { signal?: AbortSignal }): Promise<ArchiveTagResponse> {
    if (!isValidPathSegment(id))
      return Promise.reject(new AttuneError({ code: 'BAD_REQUEST', message: 'tag id is invalid' }))
    return this.#request<ArchiveTagResponse>(
      'DELETE',
      `/v1/tags/${encodeURIComponent(id)}`,
      undefined,
      '',
      opts?.signal,
    )
  }

  // ── Workflow config (needs the workflow:read / workflow:write scope) ──────

  /** List the tenant's workflow states. */
  listWorkflowStates(opts?: {
    includeArchived?: boolean
    signal?: AbortSignal
  }): Promise<ListStatesResponse> {
    const q = opts?.includeArchived ? '?include_archived=true' : ''
    return this.#request<ListStatesResponse>(
      'GET',
      `/v1/workflow/states${q}`,
      undefined,
      '',
      opts?.signal,
    )
  }

  /** Create a workflow state. */
  createWorkflowState(
    req: CreateStateRequest,
    opts?: { signal?: AbortSignal },
  ): Promise<CreateStateResponse> {
    return this.#request<CreateStateResponse>('POST', '/v1/workflow/states', req, '', opts?.signal)
  }

  /** Update a workflow state by id (replace-semantics). */
  updateWorkflowState(
    req: UpdateStateRequest,
    opts?: { signal?: AbortSignal },
  ): Promise<UpdateStateResponse> {
    if (!isValidPathSegment(req.id))
      return Promise.reject(
        new AttuneError({ code: 'BAD_REQUEST', message: 'state id is invalid' }),
      )
    return this.#request<UpdateStateResponse>(
      'PATCH',
      `/v1/workflow/states/${encodeURIComponent(req.id)}`,
      req,
      '',
      opts?.signal,
    )
  }

  /** Archive a workflow state by id. */
  archiveWorkflowState(id: string, opts?: { signal?: AbortSignal }): Promise<ArchiveStateResponse> {
    if (!isValidPathSegment(id))
      return Promise.reject(
        new AttuneError({ code: 'BAD_REQUEST', message: 'state id is invalid' }),
      )
    return this.#request<ArchiveStateResponse>(
      'DELETE',
      `/v1/workflow/states/${encodeURIComponent(id)}`,
      undefined,
      '',
      opts?.signal,
    )
  }

  /** List the allowed workflow transitions. */
  listWorkflowTransitions(opts?: { signal?: AbortSignal }): Promise<ListTransitionsResponse> {
    return this.#request<ListTransitionsResponse>(
      'GET',
      '/v1/workflow/transitions',
      undefined,
      '',
      opts?.signal,
    )
  }

  /** Replace the workflow transition set. */
  replaceWorkflowTransitions(
    req: ReplaceTransitionsRequest,
    opts?: { signal?: AbortSignal },
  ): Promise<ReplaceTransitionsResponse> {
    return this.#request<ReplaceTransitionsResponse>(
      'PUT',
      '/v1/workflow/transitions',
      req,
      '',
      opts?.signal,
    )
  }

  /** Seed the default workflow states/transitions. */
  seedWorkflowDefaults(opts?: { signal?: AbortSignal }): Promise<SeedDefaultsResponse> {
    return this.#request<SeedDefaultsResponse>('POST', '/v1/workflow/seed', {}, '', opts?.signal)
  }

  async #request<T>(
    method: string,
    path: string,
    body: unknown,
    idempotencyKey: string,
    userSignal?: AbortSignal,
  ): Promise<T> {
    const url = this.#baseURL + path
    // Serialize once, reused across retries. A non-serializable body (BigInt,
    // circular ref, …) surfaces as a typed AttuneError, not a raw TypeError.
    // No body for GET/DELETE.
    let payload: string | undefined
    if (body !== undefined) {
      try {
        payload = JSON.stringify(body)
      } catch (cause) {
        throw new AttuneError({
          code: 'BAD_REQUEST',
          message: 'request body is not JSON-serializable',
          cause,
        })
      }
    }
    // A non-idempotent POST without a server-honored idempotency key must NOT be
    // retried: a retry after a lost response could create a duplicate resource.
    // (Ingest's POST carries an Idempotency-Key; GET/PUT/PATCH/DELETE are idempotent.)
    const retrySafe = method !== 'POST' || idempotencyKey !== ''
    let lastError: AttuneError | undefined

    for (let attempt = 0; attempt <= this.#maxRetries; attempt++) {
      if (userSignal?.aborted) {
        throw new AttuneError({ code: TransportErrorCode.Aborted, message: 'request aborted' })
      }

      let result: { status: number; headers: Headers; text: string }
      try {
        result = await this.#fetchOnce(method, url, payload, idempotencyKey, userSignal)
      } catch (err) {
        const transportError = err as AttuneError
        // A caller-initiated abort is intentional — never retry it.
        if (transportError.code === TransportErrorCode.Aborted) throw transportError
        lastError = transportError
        if (retrySafe && attempt < this.#maxRetries) {
          await this.#sleep(backoffDelay(attempt))
          continue
        }
        throw transportError
      }

      const { status, headers, text } = result
      if (status >= 200 && status < 300) {
        try {
          return JSON.parse(text) as T
        } catch (cause) {
          throw new AttuneError({
            code: 'INTERNAL',
            status,
            headers,
            message: 'invalid JSON in response body',
            cause,
          })
        }
      }

      const error = AttuneError.fromResponse(status, parseErrorBody(text), headers)
      if (isRetryable(status) && retrySafe && attempt < this.#maxRetries) {
        lastError = error
        await this.#sleep(parseRetryAfter(headers) ?? backoffDelay(attempt))
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
    method: string,
    url: string,
    payload: string | undefined,
    idempotencyKey: string,
    userSignal?: AbortSignal,
  ): Promise<{ status: number; headers: Headers; text: string }> {
    const controller = new AbortController()
    let timedOut = false
    // timeout <= 0 → no per-attempt deadline (don't arm the timer).
    const timer =
      this.#timeout > 0
        ? setTimeout(() => {
            timedOut = true
            controller.abort()
          }, this.#timeout)
        : undefined
    const onUserAbort = () => controller.abort()
    if (userSignal) userSignal.addEventListener('abort', onUserAbort, { once: true })

    try {
      const headers: Record<string, string> = {
        ...this.#defaultHeaders,
        'user-agent': USER_AGENT,
        [API_KEY_HEADER]: this.#apiKey,
      }
      if (payload !== undefined) headers['content-type'] = 'application/json'
      if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey
      const response = await this.#fetch(url, {
        method,
        headers,
        body: payload,
        signal: controller.signal,
        // Never auto-follow redirects: fetch would re-send the X-API-Key header
        // to the redirect target, leaking the key if the endpoint is
        // compromised/misconfigured. A 3xx is surfaced as an error instead.
        redirect: 'manual',
      })
      // Read the body under the SAME timeout/abort scope: fetch() resolves on
      // headers, so a slow or truncated body would otherwise hang forever (the
      // timer is cleared once this method returns). Reading here keeps the whole
      // request — headers AND body — under one deadline. Capped so a hostile
      // server can't OOM the client with a huge body.
      const text = await readCappedText(response)
      return { status: response.status, headers: response.headers, text }
    } catch (cause) {
      // A typed error from the read (e.g. the size cap) passes through unchanged.
      if (cause instanceof AttuneError) throw cause
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
      if (timer) clearTimeout(timer)
      if (userSignal) userSignal.removeEventListener('abort', onUserAbort)
    }
  }
}

// Strip trailing '/' with a single linear scan — avoids the /\/+$/ regex, which
// CodeQL flags as polynomial-ReDoS on inputs with many trailing slashes.
function stripTrailingSlashes(s: string): string {
  let end = s.length
  while (end > 0 && s.charCodeAt(end - 1) === 47 /* '/' */) end--
  return s.slice(0, end)
}

/** Best-effort parse of the unified ErrorResponse envelope; undefined on any failure. */
function parseErrorBody(text: string): ErrorResponse | undefined {
  try {
    return JSON.parse(text) as ErrorResponse
  } catch {
    return undefined
  }
}

// 1 MiB — an ingest response is < 1 KiB; the cap stops a hostile/runaway server
// from OOM-ing the client by streaming an unbounded body.
const MAX_RESPONSE_BYTES = 1024 * 1024

// Read a response body to text under a hard byte cap. Falls back to
// response.text() when the body isn't a readable stream (some test doubles).
async function readCappedText(response: Response): Promise<string> {
  const body = response.body
  if (!body || typeof body.getReader !== 'function') return response.text()
  const reader = body.getReader()
  const chunks: Uint8Array[] = []
  let total = 0
  try {
    let chunk = await reader.read()
    while (!chunk.done) {
      if (chunk.value) {
        total += chunk.value.byteLength
        if (total > MAX_RESPONSE_BYTES) {
          throw new AttuneError({
            code: 'INTERNAL',
            status: response.status,
            message: `response body exceeds the ${MAX_RESPONSE_BYTES}-byte cap`,
          })
        }
        chunks.push(chunk.value)
      }
      chunk = await reader.read()
    }
  } finally {
    await reader.cancel().catch(() => {})
  }
  const buf = new Uint8Array(total)
  let offset = 0
  for (const c of chunks) {
    buf.set(c, offset)
    offset += c.byteLength
  }
  return new TextDecoder().decode(buf)
}

// CR/LF in a header value enables header injection; fetch would reject it as an
// opaque (retryable-looking) network error, so reject it up front as a clear
// non-retryable client error.
function hasHeaderControlChar(s: string): boolean {
  return s.includes('\r') || s.includes('\n')
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
