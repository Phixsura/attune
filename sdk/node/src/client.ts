import { AttuneError, TransportErrorCode } from './errors'
import type {
  CreateAuditEvidenceExportRequest,
  CreateAuditEvidenceExportResponse,
  GetAuditEvidenceExportResponse,
  ListAuditLogRequest,
  ListAuditLogResponse,
} from './proto/attune/v1/audit'
import type { ErrorResponse } from './proto/attune/v1/common'
import type {
  CancelGdprRequestResponse,
  DeleteGdprSubjectRequest,
  DeleteGdprSubjectResponse,
  ExportGdprSubjectRequest,
  ExportGdprSubjectResponse,
  GdprExportStatusResponse,
  GdprOperationsResponse,
  GdprRequestSummary,
  ListGdprRequestsRequest,
  ListGdprRequestsResponse,
  RevokeGdprExportResponse,
} from './proto/attune/v1/gdpr'
import type { IngestRequest, IngestResponse } from './proto/attune/v1/ingest'
import type {
  CreateMCPClientRequest,
  CreateMCPClientResponse,
  GetMCPClientResponse,
  ListMCPClientsResponse,
  MCPToolPolicyInput,
  ReplaceMCPClientToolPoliciesRequest,
  ReplaceMCPClientToolPoliciesResponse,
  RevokeMCPClientResponse,
  RevokeMCPRefreshGrantResponse,
  RevokeMCPSessionResponse,
  UpdateMCPClientRequest,
  UpdateMCPClientResponse,
} from './proto/attune/v1/mcp_client'
import type {
  ListDeliveriesResponse,
  OutboxDelivery,
  RetryDeliveryResponse,
} from './proto/attune/v1/outbox'
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

export interface BinaryResponse {
  contentType: string
  data: Uint8Array
  filename?: string
}

const DEFAULT_TIMEOUT_MS = 30_000
const DEFAULT_MAX_RETRIES = 2
const API_KEY_HEADER = 'X-API-Key'
const MAX_INT32 = 2_147_483_647
const MAX_INT64_DECIMAL = '9223372036854775807'
const GDPR_REQUEST_TYPES = new Set(['export', 'delete'])
const OUTBOX_STATUSES = new Set(['pending', 'delivered', 'failed', 'dead'])
const MCP_SCOPES = new Set(['mcp:read', 'mcp:write', 'mcp:ingest'])
const MCP_TOOL_POLICY_MODES = new Set(['legacy_allow_all', 'allow_list'])
const MCP_TOOL_POLICY_EFFECTS = new Set(['allow', 'deny'])
const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i
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

function setQueryValue(
  query: URLSearchParams,
  key: string,
  value: string | number | undefined,
): void {
  if (value === undefined || value === '') return
  query.append(key, String(value))
}

function isObjectRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

function validateObjectRequest<T extends object>(
  value: T | null | undefined,
  message: string,
): T | AttuneError {
  if (!isObjectRecord(value)) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

function validateOptionalObjectRequest<T extends object>(
  value: T | null | undefined,
  message: string,
): T | AttuneError {
  if (value === undefined) return {} as T
  return validateObjectRequest(value, message)
}

function validateOptionalOptionsObject<T extends object>(
  value: T | null | undefined,
  message: string,
): T | AttuneError | undefined {
  if (value === undefined) return undefined
  return validateObjectRequest(value, message)
}

function validateOptionalNumber(value: unknown, message: string): number | AttuneError | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

function validateOptionalPositiveInt32(
  value: unknown,
  message: string,
): number | AttuneError | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'number' || !Number.isInteger(value) || value <= 0 || value > MAX_INT32) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

function validateOptionalString(value: unknown, message: string): string | AttuneError | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'string') {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

function validateOptionalGdprRequestType(
  value: unknown,
  message: string,
): string | AttuneError | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'string' || !GDPR_REQUEST_TYPES.has(value)) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

function validateOptionalBoolean(
  value: unknown,
  message: string,
): boolean | AttuneError | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'boolean') {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

function validateOptionalStringArray(
  value: unknown,
  message: string,
): string[] | AttuneError | undefined {
  if (value === undefined) return undefined
  if (!Array.isArray(value) || value.some((item) => typeof item !== 'string')) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

function validateOptionalOutboxStatuses(
  value: unknown,
  typeMessage: string,
  valueMessage: string,
): string[] | AttuneError | undefined {
  const statuses = validateOptionalStringArray(value, typeMessage)
  if (statuses instanceof AttuneError || statuses === undefined) return statuses
  const out: string[] = []
  const seen = new Set<string>()
  for (const status of statuses) {
    const normalized = status.trim()
    if (!OUTBOX_STATUSES.has(normalized)) {
      return new AttuneError({ code: 'BAD_REQUEST', message: valueMessage })
    }
    if (!seen.has(normalized)) {
      seen.add(normalized)
      out.push(normalized)
    }
  }
  return out
}

function validateOptionalMCPScopes(
  value: unknown,
  typeMessage: string,
  valueMessage: string,
): string[] | AttuneError | undefined {
  const scopes = validateOptionalStringArray(value, typeMessage)
  if (scopes instanceof AttuneError || scopes === undefined) return scopes
  for (const scope of scopes) {
    if (!MCP_SCOPES.has(scope)) {
      return new AttuneError({ code: 'BAD_REQUEST', message: valueMessage })
    }
  }
  return scopes
}

function validateRequiredMCPClientName(value: unknown): string | AttuneError {
  if (typeof value !== 'string') {
    return new AttuneError({ code: 'BAD_REQUEST', message: 'mcp client name is required' })
  }
  const normalized = value.trim()
  if (!normalized) {
    return new AttuneError({ code: 'BAD_REQUEST', message: 'mcp client name is required' })
  }
  if (Array.from(normalized).length > 128) {
    return new AttuneError({
      code: 'BAD_REQUEST',
      message: 'mcp client name must be at most 128 characters',
    })
  }
  return normalized
}

function isLoopbackHostname(hostname: string): boolean {
  const lower = hostname.toLowerCase()
  return lower === 'localhost' || lower === '127.0.0.1' || lower === '::1' || lower === '[::1]'
}

function validateMCPRedirectUris(value: unknown): string[] | AttuneError {
  const uris = validateOptionalStringArray(value, 'mcp redirectUris must be an array of strings')
  if (uris instanceof AttuneError || uris === undefined) {
    return (
      uris ??
      new AttuneError({
        code: 'BAD_REQUEST',
        message: 'redirectUris must contain at least one URI',
      })
    )
  }
  if (uris.length === 0) {
    return new AttuneError({
      code: 'BAD_REQUEST',
      message: 'redirectUris must contain at least one URI',
    })
  }
  for (const uri of uris) {
    if (!/^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//.test(uri)) {
      return new AttuneError({
        code: 'BAD_REQUEST',
        message: 'redirect URI must be an absolute URI with a host',
      })
    }
    let parsed: URL
    try {
      parsed = new URL(uri)
    } catch {
      return new AttuneError({ code: 'BAD_REQUEST', message: `invalid redirect URI: ${uri}` })
    }
    const scheme = parsed.protocol.slice(0, -1).toLowerCase()
    if (parsed.hash) {
      return new AttuneError({
        code: 'BAD_REQUEST',
        message: 'fragment not allowed in redirect URI',
      })
    }
    if (
      scheme === 'javascript' ||
      scheme === 'data' ||
      scheme === 'file' ||
      scheme === 'vbscript'
    ) {
      return new AttuneError({
        code: 'BAD_REQUEST',
        message: 'dangerous URI scheme not allowed',
      })
    }
    if (!parsed.host || !parsed.hostname) {
      return new AttuneError({
        code: 'BAD_REQUEST',
        message: 'redirect URI must be an absolute URI with a host',
      })
    }
    if (scheme !== 'https' && !isLoopbackHostname(parsed.hostname)) {
      return new AttuneError({
        code: 'BAD_REQUEST',
        message: 'redirect URI must use HTTPS (except for localhost)',
      })
    }
  }
  return uris
}

function validateOptionalMCPToolPolicyMode(
  value: unknown,
  message: string,
): string | AttuneError | undefined {
  if (value === undefined) {
    return undefined
  }
  if (typeof value !== 'string') {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  const normalized = value.trim()
  if (!MCP_TOOL_POLICY_MODES.has(normalized)) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return normalized
}

function validateRequiredMCPToolPolicies(value: unknown): MCPToolPolicyInput[] | AttuneError {
  if (value === undefined) {
    return new AttuneError({
      code: 'BAD_REQUEST',
      message: 'mcp policies must be provided; use [] to clear all policies',
    })
  }
  if (!Array.isArray(value)) {
    return new AttuneError({
      code: 'BAD_REQUEST',
      message: 'mcp policies must be an array of objects',
    })
  }
  const out: MCPToolPolicyInput[] = []
  const seen = new Set<string>()
  for (const item of value) {
    if (!isObjectRecord(item)) {
      return new AttuneError({
        code: 'BAD_REQUEST',
        message: 'mcp policies must be an array of objects',
      })
    }
    const toolName = typeof item.toolName === 'string' ? item.toolName.trim() : ''
    if (!toolName) {
      return new AttuneError({
        code: 'BAD_REQUEST',
        message: 'mcp tool policy toolName is required',
      })
    }
    if (seen.has(toolName)) {
      return new AttuneError({
        code: 'BAD_REQUEST',
        message: `duplicate mcp tool policy toolName: ${toolName}`,
      })
    }
    seen.add(toolName)
    const effect = typeof item.effect === 'string' ? item.effect.trim() : ''
    if (!MCP_TOOL_POLICY_EFFECTS.has(effect)) {
      return new AttuneError({
        code: 'BAD_REQUEST',
        message: 'mcp tool policy effect must be "allow" or "deny"',
      })
    }
    const rateLimitRpm = validateOptionalPositiveInt32(
      item.rateLimitRpm,
      'mcp tool policy rateLimitRpm must be a positive integer',
    )
    if (rateLimitRpm instanceof AttuneError) return rateLimitRpm
    const rateLimitBurst = validateOptionalPositiveInt32(
      item.rateLimitBurst,
      'mcp tool policy rateLimitBurst must be a positive integer',
    )
    if (rateLimitBurst instanceof AttuneError) return rateLimitBurst
    out.push({
      ...item,
      toolName,
      effect,
      ...(rateLimitRpm !== undefined ? { rateLimitRpm } : {}),
      ...(rateLimitBurst !== undefined ? { rateLimitBurst } : {}),
    })
  }
  return out
}

function validateOptionalStringRecord(
  value: unknown,
  message: string,
): Record<string, string> | AttuneError | undefined {
  if (value === undefined) return undefined
  if (!isObjectRecord(value)) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  const out: Record<string, string> = {}
  for (const [k, v] of Object.entries(value)) {
    if (typeof v !== 'string') {
      return new AttuneError({ code: 'BAD_REQUEST', message })
    }
    out[k] = v
  }
  return out
}

function validateOptionalFunction<T extends (...args: never) => unknown>(
  value: unknown,
  message: string,
): T | AttuneError | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'function') {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value as T
}

function validateOptionalAbortSignal(
  value: unknown,
  message: string,
): AbortSignal | AttuneError | undefined {
  if (value === undefined) return undefined
  if (typeof AbortSignal !== 'undefined' && value instanceof AbortSignal) {
    return value
  }
  if (
    value === null ||
    typeof value !== 'object' ||
    Object.prototype.toString.call(value) !== '[object AbortSignal]' ||
    typeof (value as { aborted?: unknown }).aborted !== 'boolean' ||
    typeof (value as { addEventListener?: unknown }).addEventListener !== 'function' ||
    typeof (value as { removeEventListener?: unknown }).removeEventListener !== 'function'
  ) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value as AbortSignal
}

function validateSignalOptions(
  value: { signal?: AbortSignal } | null | undefined,
  message: string,
): { signal?: AbortSignal } | AttuneError | undefined {
  const validated = validateOptionalOptionsObject(value, message)
  if (validated instanceof AttuneError) return validated
  const signal = validateOptionalAbortSignal(validated?.signal, 'signal must be an AbortSignal')
  if (signal instanceof AttuneError) return signal
  return signal === undefined ? undefined : { signal }
}

function validateRequestOptions(
  value: RequestOptions | null | undefined,
  message: string,
): RequestOptions | AttuneError | undefined {
  const validated = validateOptionalOptionsObject(value, message)
  if (validated instanceof AttuneError) return validated
  const signal = validateOptionalAbortSignal(validated?.signal, 'signal must be an AbortSignal')
  if (signal instanceof AttuneError) return signal
  const idempotencyKey = validateOptionalString(
    validated?.idempotencyKey,
    'idempotencyKey must be a string',
  )
  if (idempotencyKey instanceof AttuneError) return idempotencyKey
  if (idempotencyKey !== undefined && hasHeaderControlChar(idempotencyKey)) {
    return new AttuneError({
      code: 'BAD_REQUEST',
      message: 'idempotencyKey contains invalid characters',
    })
  }
  return {
    ...(signal !== undefined ? { signal } : {}),
    ...(idempotencyKey !== undefined ? { idempotencyKey } : {}),
  }
}

function resolveIdempotencyKey(value: string | undefined): string {
  return value && value.length > 0 ? value : randomIdempotencyKey()
}

function validateObjectRequestWithPathId<T extends { id?: string }>(
  value: T | null | undefined,
  objectMessage: string,
  idMessage: string,
): { req: T; id: string } | AttuneError {
  const req = validateObjectRequest(value, objectMessage)
  if (req instanceof AttuneError) return req
  const id = typeof req.id === 'string' ? req.id : ''
  if (!isValidPathSegment(id)) {
    return new AttuneError({ code: 'BAD_REQUEST', message: idMessage })
  }
  return { req, id }
}

function validatePathSegment(value: unknown, message: string): string | AttuneError {
  if (typeof value !== 'string' || !isValidPathSegment(value)) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

function validateUUIDPathSegment(value: unknown, message: string): string | AttuneError {
  if (typeof value !== 'string' || !UUID_PATTERN.test(value)) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

function isNonNegativeInt64String(value: string): boolean {
  if (!/^\d+$/.test(value)) return false
  const normalized = value.replace(/^0+/, '') || '0'
  return (
    normalized.length < MAX_INT64_DECIMAL.length ||
    (normalized.length === MAX_INT64_DECIMAL.length && normalized <= MAX_INT64_DECIMAL)
  )
}

function validateOptionalNonNegativeInt64String(
  value: unknown,
  message: string,
): string | AttuneError | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'string' || !isNonNegativeInt64String(value)) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

function validatePositiveInt64String(value: unknown, message: string): string | AttuneError {
  if (typeof value !== 'string' || !isNonNegativeInt64String(value)) {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  if ((value.replace(/^0+/, '') || '0') === '0') {
    return new AttuneError({ code: 'BAD_REQUEST', message })
  }
  return value
}

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
    if (typeof options.baseURL !== 'string') {
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'baseURL must be a string' })
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
    if (typeof options.apiKey !== 'string') {
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'apiKey must be a string' })
    }
    if (!options.apiKey)
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'apiKey is required' })
    if (hasHeaderControlChar(options.apiKey))
      throw new AttuneError({ code: 'BAD_REQUEST', message: 'apiKey contains invalid characters' })

    const fetchOverride = validateOptionalFunction<FetchLike>(
      options.fetch,
      'fetch must be a function',
    )
    if (fetchOverride instanceof AttuneError) throw fetchOverride
    const fetchImpl = options.fetch ?? (globalThis.fetch as FetchLike | undefined)
    if (!fetchImpl) {
      throw new AttuneError({
        code: 'BAD_REQUEST',
        message: 'no global fetch available; pass a `fetch` implementation in ClientOptions',
      })
    }

    const timeout = validateOptionalNumber(options.timeout, 'timeout must be a finite number')
    if (timeout instanceof AttuneError) throw timeout
    const maxRetries = validateOptionalNumber(
      options.maxRetries,
      'maxRetries must be a finite number',
    )
    if (maxRetries instanceof AttuneError) throw maxRetries
    const defaultHeaders = validateOptionalStringRecord(
      options.defaultHeaders,
      'defaultHeaders must be an object of string values',
    )
    if (defaultHeaders instanceof AttuneError) throw defaultHeaders
    const sleep = validateOptionalFunction<(ms: number) => Promise<void>>(
      options.sleep,
      'sleep must be a function',
    )
    if (sleep instanceof AttuneError) throw sleep

    this.#baseURL = stripTrailingSlashes(options.baseURL)
    this.#apiKey = options.apiKey
    this.#fetch = fetchImpl
    // timeout <= 0 disables the per-attempt timeout (treat as "no limit") rather
    // than aborting instantly; negative maxRetries clamps to 0 (one attempt).
    this.#timeout = timeout ?? DEFAULT_TIMEOUT_MS
    this.#maxRetries = Math.max(0, maxRetries ?? DEFAULT_MAX_RETRIES)
    // Drop any reserved header (case-insensitively) so a consumer's casing
    // variant can't slip past and get concatenated by `Headers` at fetch time.
    this.#defaultHeaders = {}
    for (const [k, v] of Object.entries(defaultHeaders ?? {})) {
      if (!RESERVED_HEADERS.has(k.toLowerCase())) this.#defaultHeaders[k] = v
    }
    this.#sleep = sleep ?? defaultSleep
  }

  /** Ingest one feedback item. Resolves with the stored row id, or throws `AttuneError`. */
  ingest(input: IngestInput, options?: RequestOptions): Promise<IngestResponse> {
    const validated = validateObjectRequest(input, 'ingest input must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedOptions = validateRequestOptions(options, 'request options must be an object')
    if (validatedOptions instanceof AttuneError) return Promise.reject(validatedOptions)
    // One key per call, reused across this call's retries — that is what makes
    // a retry safe against the non-idempotent ingest POST.
    const idempotencyKey = resolveIdempotencyKey(validatedOptions?.idempotencyKey)
    return this.#request<IngestResponse>(
      'POST',
      INGEST_PATH,
      validated,
      idempotencyKey,
      validatedOptions?.signal,
    )
  }

  // ── Tags (needs a key with the tags:read / tags:write scope) ──────────────

  /** List the tenant's tags. */
  listTags(opts?: { includeArchived?: boolean; signal?: AbortSignal }): Promise<ListTagsResponse> {
    const validated = validateOptionalOptionsObject(opts, 'tag list options must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const signal = validateOptionalAbortSignal(validated?.signal, 'signal must be an AbortSignal')
    if (signal instanceof AttuneError) return Promise.reject(signal)
    const includeArchived = validateOptionalBoolean(
      validated?.includeArchived,
      'includeArchived must be a boolean',
    )
    if (includeArchived instanceof AttuneError) return Promise.reject(includeArchived)
    const q = includeArchived ? '?include_archived=true' : ''
    return this.#request<ListTagsResponse>('GET', `/v1/tags${q}`, undefined, '', signal)
  }

  /** Create a tag. */
  createTag(req: CreateTagRequest, opts?: RequestOptions): Promise<Tag> {
    const validated = validateObjectRequest(req, 'tag request must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedOpts = validateRequestOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<Tag>(
      'POST',
      '/v1/tags',
      validated,
      resolveIdempotencyKey(validatedOpts?.idempotencyKey),
      validatedOpts?.signal,
    )
  }

  /**
   * Update a tag by id. **Replace-semantics**: send the full desired state —
   * any writable field you omit (name, color, description) is overwritten, not
   * preserved, so a partial update silently clears the omitted fields.
   */
  updateTag(req: UpdateTagRequest, opts?: { signal?: AbortSignal }): Promise<Tag> {
    const validated = validateObjectRequestWithPathId(
      req,
      'tag request must be an object',
      'tag id is invalid',
    )
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<Tag>(
      'PATCH',
      `/v1/tags/${encodeURIComponent(validated.id)}`,
      validated.req,
      '',
      validatedOpts?.signal,
    )
  }

  /** Archive a tag by id. */
  archiveTag(id: string, opts?: { signal?: AbortSignal }): Promise<ArchiveTagResponse> {
    const validatedId = validatePathSegment(id, 'tag id is invalid')
    if (validatedId instanceof AttuneError) return Promise.reject(validatedId)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<ArchiveTagResponse>(
      'DELETE',
      `/v1/tags/${encodeURIComponent(validatedId)}`,
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  // ── Workflow config (needs the workflow:read / workflow:write scope) ──────

  /** List the tenant's workflow states. */
  listWorkflowStates(opts?: {
    includeArchived?: boolean
    signal?: AbortSignal
  }): Promise<ListStatesResponse> {
    const validated = validateOptionalOptionsObject(
      opts,
      'workflow state list options must be an object',
    )
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const signal = validateOptionalAbortSignal(validated?.signal, 'signal must be an AbortSignal')
    if (signal instanceof AttuneError) return Promise.reject(signal)
    const includeArchived = validateOptionalBoolean(
      validated?.includeArchived,
      'includeArchived must be a boolean',
    )
    if (includeArchived instanceof AttuneError) return Promise.reject(includeArchived)
    const q = includeArchived ? '?include_archived=true' : ''
    return this.#request<ListStatesResponse>(
      'GET',
      `/v1/workflow/states${q}`,
      undefined,
      '',
      signal,
    )
  }

  /** Create a workflow state. */
  createWorkflowState(req: CreateStateRequest, opts?: RequestOptions): Promise<CreateStateResponse> {
    const validated = validateObjectRequest(req, 'state request must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedOpts = validateRequestOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<CreateStateResponse>(
      'POST',
      '/v1/workflow/states',
      validated,
      resolveIdempotencyKey(validatedOpts?.idempotencyKey),
      validatedOpts?.signal,
    )
  }

  /** Update a workflow state by id (replace-semantics). */
  updateWorkflowState(
    req: UpdateStateRequest,
    opts?: { signal?: AbortSignal },
  ): Promise<UpdateStateResponse> {
    const validated = validateObjectRequestWithPathId(
      req,
      'state request must be an object',
      'state id is invalid',
    )
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<UpdateStateResponse>(
      'PATCH',
      `/v1/workflow/states/${encodeURIComponent(validated.id)}`,
      validated.req,
      '',
      validatedOpts?.signal,
    )
  }

  /** Archive a workflow state by id. */
  archiveWorkflowState(id: string, opts?: { signal?: AbortSignal }): Promise<ArchiveStateResponse> {
    const validatedId = validatePathSegment(id, 'state id is invalid')
    if (validatedId instanceof AttuneError) return Promise.reject(validatedId)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<ArchiveStateResponse>(
      'DELETE',
      `/v1/workflow/states/${encodeURIComponent(validatedId)}`,
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  /** List the allowed workflow transitions. */
  listWorkflowTransitions(opts?: { signal?: AbortSignal }): Promise<ListTransitionsResponse> {
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<ListTransitionsResponse>(
      'GET',
      '/v1/workflow/transitions',
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  /** Replace the workflow transition set. */
  replaceWorkflowTransitions(
    req: ReplaceTransitionsRequest,
    opts?: { signal?: AbortSignal },
  ): Promise<ReplaceTransitionsResponse> {
    const validated = validateObjectRequest(req, 'workflow transition request must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<ReplaceTransitionsResponse>(
      'PUT',
      '/v1/workflow/transitions',
      validated,
      '',
      validatedOpts?.signal,
    )
  }

  /** Seed the default workflow states/transitions. */
  seedWorkflowDefaults(opts?: RequestOptions): Promise<SeedDefaultsResponse> {
    const validatedOpts = validateRequestOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<SeedDefaultsResponse>(
      'POST',
      '/v1/workflow/seed',
      {},
      resolveIdempotencyKey(validatedOpts?.idempotencyKey),
      validatedOpts?.signal,
    )
  }

  // ── Audit log (needs the audit:read scope) ───────────────────────────────

  /** List audit log entries. */
  listAuditLog(
    req: Partial<ListAuditLogRequest> & { signal?: AbortSignal } = {},
  ): Promise<ListAuditLogResponse> {
    const validated = this.#validateAuditLogQuery(req)
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const encoded = validated.query.toString()
    const suffix = encoded ? `?${encoded}` : ''
    return this.#request<ListAuditLogResponse>(
      'GET',
      `/v1/audit-log${suffix}`,
      undefined,
      '',
      validated.signal,
    )
  }

  /** Download the current audit-log query as CSV. */
  exportAuditLogCSV(
    req: Partial<ListAuditLogRequest> & { signal?: AbortSignal } = {},
  ): Promise<BinaryResponse> {
    const validated = this.#validateAuditLogQuery(req)
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const encoded = validated.query.toString()
    const suffix = encoded ? `?${encoded}` : ''
    return this.#requestBinary('GET', `/v1/audit-log/export.csv${suffix}`, '', validated.signal)
  }

  /** Create an asynchronous audit evidence export job. */
  createAuditEvidenceExport(
    req: CreateAuditEvidenceExportRequest,
    opts?: RequestOptions,
  ): Promise<CreateAuditEvidenceExportResponse> {
    const validated = validateObjectRequest(req, 'audit evidence export request must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedOpts = validateRequestOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<CreateAuditEvidenceExportResponse>(
      'POST',
      '/v1/audit-log/evidence',
      validated,
      resolveIdempotencyKey(validatedOpts?.idempotencyKey),
      validatedOpts?.signal,
    )
  }

  /** Get one audit evidence export job by id. */
  getAuditEvidenceExport(
    jobId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<GetAuditEvidenceExportResponse> {
    const validatedJobId = validatePathSegment(jobId, 'audit evidence job id is invalid')
    if (validatedJobId instanceof AttuneError) return Promise.reject(validatedJobId)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<GetAuditEvidenceExportResponse>(
      'GET',
      `/v1/audit-log/evidence/${encodeURIComponent(validatedJobId)}`,
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  /** Download one audit evidence export archive by id. */
  downloadAuditEvidenceExport(
    jobId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<BinaryResponse> {
    const validatedJobId = validatePathSegment(jobId, 'audit evidence job id is invalid')
    if (validatedJobId instanceof AttuneError) return Promise.reject(validatedJobId)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#requestBinary(
      'GET',
      `/v1/audit-log/evidence/${encodeURIComponent(validatedJobId)}/download`,
      '',
      validatedOpts?.signal,
    )
  }

  /** Iterate across all audit log pages. */
  async *iterateAuditLog(
    req: Partial<ListAuditLogRequest> & { signal?: AbortSignal } = {},
  ): AsyncGenerator<ListAuditLogResponse['items'][number], void, void> {
    let cursor = req.cursor
    while (true) {
      const page = await this.listAuditLog({ ...req, cursor })
      for (const item of page.items ?? []) {
        yield item
      }
      if (!page.nextCursor) {
        return
      }
      cursor = page.nextCursor
    }
  }

  // ── GDPR jobs (needs gdpr:read / gdpr:export / gdpr:delete) ──────────────

  /** Start a GDPR export job. */
  exportGdprSubject(req: ExportGdprSubjectRequest, opts?: RequestOptions): Promise<ExportGdprSubjectResponse> {
    const validated = validateObjectRequest(req, 'gdpr export request must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedOpts = validateRequestOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<ExportGdprSubjectResponse>(
      'POST',
      '/v1/gdpr/export',
      validated,
      resolveIdempotencyKey(validatedOpts?.idempotencyKey),
      validatedOpts?.signal,
    )
  }

  /** Get one GDPR export job by id. */
  getGdprExport(jobId: string, opts?: { signal?: AbortSignal }): Promise<GdprExportStatusResponse> {
    const validatedJobId = validatePathSegment(jobId, 'gdpr export job id is invalid')
    if (validatedJobId instanceof AttuneError) return Promise.reject(validatedJobId)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<GdprExportStatusResponse>(
      'GET',
      `/v1/gdpr/exports/${encodeURIComponent(validatedJobId)}`,
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  /** Download one GDPR export archive by id. */
  downloadGdprExport(jobId: string, opts?: { signal?: AbortSignal }): Promise<BinaryResponse> {
    const validatedJobId = validatePathSegment(jobId, 'gdpr export job id is invalid')
    if (validatedJobId instanceof AttuneError) return Promise.reject(validatedJobId)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#requestBinary(
      'GET',
      `/v1/gdpr/exports/${encodeURIComponent(validatedJobId)}/download`,
      '',
      validatedOpts?.signal,
    )
  }

  /** Revoke a GDPR export job by id. */
  revokeGdprExport(jobId: string, opts?: RequestOptions): Promise<RevokeGdprExportResponse> {
    const validatedJobId = validatePathSegment(jobId, 'gdpr export job id is invalid')
    if (validatedJobId instanceof AttuneError) return Promise.reject(validatedJobId)
    const validatedOpts = validateRequestOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<RevokeGdprExportResponse>(
      'POST',
      `/v1/gdpr/exports/${encodeURIComponent(validatedJobId)}/revoke`,
      {},
      resolveIdempotencyKey(validatedOpts?.idempotencyKey),
      validatedOpts?.signal,
    )
  }

  /** Start a GDPR delete request for one subject. */
  deleteGdprSubject(req: DeleteGdprSubjectRequest, opts?: RequestOptions): Promise<DeleteGdprSubjectResponse> {
    const validated = validateObjectRequest(req, 'gdpr delete request must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedOpts = validateRequestOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<DeleteGdprSubjectResponse>(
      'POST',
      '/v1/gdpr/delete',
      validated,
      resolveIdempotencyKey(validatedOpts?.idempotencyKey),
      validatedOpts?.signal,
    )
  }

  /** Cancel one GDPR request by id. */
  cancelGdprRequest(requestId: string, opts?: RequestOptions): Promise<CancelGdprRequestResponse> {
    const validatedRequestId = validatePathSegment(requestId, 'gdpr request id is invalid')
    if (validatedRequestId instanceof AttuneError) return Promise.reject(validatedRequestId)
    const validatedOpts = validateRequestOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<CancelGdprRequestResponse>(
      'POST',
      `/v1/gdpr/requests/${encodeURIComponent(validatedRequestId)}/cancel`,
      {},
      resolveIdempotencyKey(validatedOpts?.idempotencyKey),
      validatedOpts?.signal,
    )
  }

  /** List GDPR requests. */
  listGdprRequests(
    req: Partial<ListGdprRequestsRequest> & { signal?: AbortSignal } = {},
  ): Promise<ListGdprRequestsResponse> {
    const validated = validateOptionalObjectRequest(req, 'gdpr request query must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const signal = validateOptionalAbortSignal(validated.signal, 'signal must be an AbortSignal')
    if (signal instanceof AttuneError) return Promise.reject(signal)
    const cursor = validateOptionalString(validated.cursor, 'gdpr request cursor must be a string')
    if (cursor instanceof AttuneError) return Promise.reject(cursor)
    const limit = validateOptionalPositiveInt32(
      validated.limit,
      'gdpr request limit must be a positive integer',
    )
    if (limit instanceof AttuneError) return Promise.reject(limit)
    const requestType = validateOptionalGdprRequestType(
      validated.requestType,
      'gdpr request type must be "export" or "delete"',
    )
    if (requestType instanceof AttuneError) return Promise.reject(requestType)
    const query = new URLSearchParams()
    setQueryValue(query, 'cursor', cursor)
    if (limit !== undefined) setQueryValue(query, 'limit', limit)
    setQueryValue(query, 'request_type', requestType)
    const encoded = query.toString()
    const suffix = encoded ? `?${encoded}` : ''
    return this.#request<ListGdprRequestsResponse>(
      'GET',
      `/v1/gdpr/requests${suffix}`,
      undefined,
      '',
      signal,
    )
  }

  /** Iterate across all GDPR request pages. */
  async *iterateGdprRequests(
    req: Partial<ListGdprRequestsRequest> & { signal?: AbortSignal } = {},
  ): AsyncGenerator<GdprRequestSummary, void, void> {
    let cursor = req.cursor
    while (true) {
      const page = await this.listGdprRequests({ ...req, cursor })
      for (const item of page.items ?? []) {
        yield item
      }
      if (!page.nextCursor) {
        return
      }
      cursor = page.nextCursor
    }
  }

  /** Get GDPR operational status and retention metadata. */
  getGdprOperations(opts?: { signal?: AbortSignal }): Promise<GdprOperationsResponse> {
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<GdprOperationsResponse>(
      'GET',
      '/v1/gdpr/operations',
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  // ── Outbox (needs notify:read / notify:write) ────────────────────────────

  /** List outbox deliveries. */
  listOutboxDeliveries(opts?: {
    status?: string[]
    limit?: number
    beforeId?: string
    signal?: AbortSignal
  }): Promise<ListDeliveriesResponse> {
    const validated = validateOptionalOptionsObject(
      opts,
      'outbox delivery list options must be an object',
    )
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const signal = validateOptionalAbortSignal(validated?.signal, 'signal must be an AbortSignal')
    if (signal instanceof AttuneError) return Promise.reject(signal)
    const statusList = validateOptionalOutboxStatuses(
      validated?.status,
      'outbox statuses must be an array of strings',
      'outbox status must be one of "pending", "delivered", "failed", or "dead"',
    )
    if (statusList instanceof AttuneError) return Promise.reject(statusList)
    const limit = validateOptionalPositiveInt32(
      validated?.limit,
      'outbox delivery limit must be a positive integer',
    )
    if (limit instanceof AttuneError) return Promise.reject(limit)
    const beforeId = validateOptionalNonNegativeInt64String(
      validated?.beforeId,
      'beforeId must be a non-negative int64 string',
    )
    if (beforeId instanceof AttuneError) return Promise.reject(beforeId)
    const query = new URLSearchParams()
    for (const status of statusList ?? []) {
      if (status) query.append('status', status)
    }
    if (limit !== undefined) setQueryValue(query, 'limit', limit)
    setQueryValue(query, 'before_id', beforeId)
    const encoded = query.toString()
    const suffix = encoded ? `?${encoded}` : ''
    return this.#request<ListDeliveriesResponse>(
      'GET',
      `/v1/outbox/deliveries${suffix}`,
      undefined,
      '',
      signal,
    )
  }

  /** Retry one outbox delivery by id. */
  retryOutboxDelivery(id: string, opts?: RequestOptions): Promise<RetryDeliveryResponse> {
    const validatedId = validatePositiveInt64String(id, 'delivery id is invalid')
    if (validatedId instanceof AttuneError) return Promise.reject(validatedId)
    const validatedOpts = validateRequestOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<RetryDeliveryResponse>(
      'POST',
      `/v1/outbox/${encodeURIComponent(validatedId)}/retry`,
      {},
      resolveIdempotencyKey(validatedOpts?.idempotencyKey),
      validatedOpts?.signal,
    )
  }

  /** Iterate across all outbox delivery pages. */
  async *iterateOutboxDeliveries(opts?: {
    status?: string[]
    limit?: number
    beforeId?: string
    signal?: AbortSignal
  }): AsyncGenerator<OutboxDelivery, void, void> {
    let beforeId = opts?.beforeId
    while (true) {
      const page = await this.listOutboxDeliveries({ ...opts, beforeId })
      for (const item of page.deliveries ?? []) {
        yield item
      }
      if (!page.nextBeforeId || page.nextBeforeId === '0') {
        return
      }
      beforeId = page.nextBeforeId
    }
  }

  // ── MCP client governance (needs mcpclient:admin) ────────────────────────

  /** List MCP OAuth clients. */
  listMCPClients(opts?: { signal?: AbortSignal }): Promise<ListMCPClientsResponse> {
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<ListMCPClientsResponse>(
      'GET',
      '/v1/mcp/clients',
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  /** Create one MCP OAuth client. */
  createMCPClient(req: CreateMCPClientRequest, opts?: RequestOptions): Promise<CreateMCPClientResponse> {
    const validated = validateObjectRequest(req, 'mcp client request must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const name = validateRequiredMCPClientName(validated.name)
    if (name instanceof AttuneError) return Promise.reject(name)
    const redirectUris = validateMCPRedirectUris(validated.redirectUris)
    if (redirectUris instanceof AttuneError) return Promise.reject(redirectUris)
    const scopes = validateOptionalMCPScopes(
      validated.scopes,
      'mcp scopes must be an array of strings',
      'mcp scope must be one of "mcp:read", "mcp:write", or "mcp:ingest"',
    )
    if (scopes instanceof AttuneError) return Promise.reject(scopes)
    const validatedOpts = validateRequestOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<CreateMCPClientResponse>(
      'POST',
      '/v1/mcp/clients',
      {
        ...validated,
        name,
        redirectUris,
        ...(scopes !== undefined ? { scopes } : {}),
      },
      resolveIdempotencyKey(validatedOpts?.idempotencyKey),
      validatedOpts?.signal,
    )
  }

  /** Get one MCP OAuth client by id. */
  getMCPClient(id: string, opts?: { signal?: AbortSignal }): Promise<GetMCPClientResponse> {
    const validatedId = validateUUIDPathSegment(id, 'mcp client id is invalid')
    if (validatedId instanceof AttuneError) return Promise.reject(validatedId)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<GetMCPClientResponse>(
      'GET',
      `/v1/mcp/clients/${encodeURIComponent(validatedId)}`,
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  /** Revoke one MCP OAuth client by id. */
  revokeMCPClient(id: string, opts?: { signal?: AbortSignal }): Promise<RevokeMCPClientResponse> {
    const validatedId = validateUUIDPathSegment(id, 'mcp client id is invalid')
    if (validatedId instanceof AttuneError) return Promise.reject(validatedId)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<RevokeMCPClientResponse>(
      'DELETE',
      `/v1/mcp/clients/${encodeURIComponent(validatedId)}`,
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  /** Update one MCP OAuth client. */
  updateMCPClient(
    req: UpdateMCPClientRequest,
    opts?: { signal?: AbortSignal },
  ): Promise<UpdateMCPClientResponse> {
    const validated = validateObjectRequest(req, 'mcp client request must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedId = validateUUIDPathSegment(validated.id, 'mcp client id is invalid')
    if (validatedId instanceof AttuneError) return Promise.reject(validatedId)
    const toolPolicyMode = validateOptionalMCPToolPolicyMode(
      validated.toolPolicyMode,
      'mcp toolPolicyMode must be "legacy_allow_all" or "allow_list"',
    )
    if (toolPolicyMode instanceof AttuneError) return Promise.reject(toolPolicyMode)
    const rateLimitRpm = validateOptionalPositiveInt32(
      validated.rateLimitRpm,
      'mcp rateLimitRpm must be a positive integer',
    )
    if (rateLimitRpm instanceof AttuneError) return Promise.reject(rateLimitRpm)
    const rateLimitBurst = validateOptionalPositiveInt32(
      validated.rateLimitBurst,
      'mcp rateLimitBurst must be a positive integer',
    )
    if (rateLimitBurst instanceof AttuneError) return Promise.reject(rateLimitBurst)
    if (
      toolPolicyMode === undefined &&
      rateLimitRpm === undefined &&
      rateLimitBurst === undefined
    ) {
      return Promise.reject(
        new AttuneError({
          code: 'BAD_REQUEST',
          message:
            'mcp update request must include at least one of toolPolicyMode, rateLimitRpm, or rateLimitBurst',
        }),
      )
    }
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<UpdateMCPClientResponse>(
      'PATCH',
      `/v1/mcp/clients/${encodeURIComponent(validatedId)}`,
      {
        ...validated,
        id: validatedId,
        ...(toolPolicyMode !== undefined ? { toolPolicyMode } : {}),
        ...(rateLimitRpm !== undefined ? { rateLimitRpm } : {}),
        ...(rateLimitBurst !== undefined ? { rateLimitBurst } : {}),
      },
      '',
      validatedOpts?.signal,
    )
  }

  /** Replace explicit MCP tool policy overrides for one client. */
  replaceMCPClientToolPolicies(
    req: ReplaceMCPClientToolPoliciesRequest,
    opts?: { signal?: AbortSignal },
  ): Promise<ReplaceMCPClientToolPoliciesResponse> {
    const validated = validateObjectRequest(req, 'mcp tool policy request must be an object')
    if (validated instanceof AttuneError) return Promise.reject(validated)
    const validatedId = validateUUIDPathSegment(validated.id, 'mcp client id is invalid')
    if (validatedId instanceof AttuneError) return Promise.reject(validatedId)
    const policies = validateRequiredMCPToolPolicies(validated.policies)
    if (policies instanceof AttuneError) return Promise.reject(policies)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<ReplaceMCPClientToolPoliciesResponse>(
      'PUT',
      `/v1/mcp/clients/${encodeURIComponent(validatedId)}/tool-policies`,
      {
        ...validated,
        id: validatedId,
        policies,
      },
      '',
      validatedOpts?.signal,
    )
  }

  /** Revoke one MCP refresh grant by id. */
  revokeMCPRefreshGrant(
    clientId: string,
    grantId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<RevokeMCPRefreshGrantResponse> {
    const validatedClientId = validateUUIDPathSegment(clientId, 'mcp client id is invalid')
    if (validatedClientId instanceof AttuneError) return Promise.reject(validatedClientId)
    const validatedGrantId = validateUUIDPathSegment(grantId, 'mcp refresh grant id is invalid')
    if (validatedGrantId instanceof AttuneError) return Promise.reject(validatedGrantId)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<RevokeMCPRefreshGrantResponse>(
      'DELETE',
      `/v1/mcp/clients/${encodeURIComponent(validatedClientId)}/grants/${encodeURIComponent(validatedGrantId)}`,
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  /** Revoke one MCP session by id. */
  revokeMCPSession(
    clientId: string,
    sessionId: string,
    opts?: { signal?: AbortSignal },
  ): Promise<RevokeMCPSessionResponse> {
    const validatedClientId = validateUUIDPathSegment(clientId, 'mcp client id is invalid')
    if (validatedClientId instanceof AttuneError) return Promise.reject(validatedClientId)
    const validatedSessionId = validateUUIDPathSegment(sessionId, 'mcp session id is invalid')
    if (validatedSessionId instanceof AttuneError) return Promise.reject(validatedSessionId)
    const validatedOpts = validateSignalOptions(opts, 'request options must be an object')
    if (validatedOpts instanceof AttuneError) return Promise.reject(validatedOpts)
    return this.#request<RevokeMCPSessionResponse>(
      'DELETE',
      `/v1/mcp/clients/${encodeURIComponent(validatedClientId)}/sessions/${encodeURIComponent(validatedSessionId)}`,
      undefined,
      '',
      validatedOpts?.signal,
    )
  }

  #validateAuditLogQuery(
    req: Partial<ListAuditLogRequest> & { signal?: AbortSignal },
  ): { query: URLSearchParams; signal?: AbortSignal } | AttuneError {
    const validated = validateOptionalObjectRequest(req, 'audit log query must be an object')
    if (validated instanceof AttuneError) return validated
    const signal = validateOptionalAbortSignal(validated.signal, 'signal must be an AbortSignal')
    if (signal instanceof AttuneError) return signal
    const actions = validateOptionalStringArray(
      validated.actions,
      'audit log actions must be an array of strings',
    )
    if (actions instanceof AttuneError) return actions
    const action = validateOptionalString(validated.action, 'audit log action must be a string')
    if (action instanceof AttuneError) return action
    const actorType = validateOptionalString(
      validated.actorType,
      'audit log actorType must be a string',
    )
    if (actorType instanceof AttuneError) return actorType
    const actorId = validateOptionalString(validated.actorId, 'audit log actorId must be a string')
    if (actorId instanceof AttuneError) return actorId
    const targetType = validateOptionalString(
      validated.targetType,
      'audit log targetType must be a string',
    )
    if (targetType instanceof AttuneError) return targetType
    const targetId = validateOptionalString(
      validated.targetId,
      'audit log targetId must be a string',
    )
    if (targetId instanceof AttuneError) return targetId
    const from = validateOptionalString(validated.from, 'audit log from must be a string')
    if (from instanceof AttuneError) return from
    const to = validateOptionalString(validated.to, 'audit log to must be a string')
    if (to instanceof AttuneError) return to
    const limit = validateOptionalPositiveInt32(
      validated.limit,
      'audit log limit must be a positive integer',
    )
    if (limit instanceof AttuneError) return limit
    const cursor = validateOptionalString(validated.cursor, 'audit log cursor must be a string')
    if (cursor instanceof AttuneError) return cursor

    const query = new URLSearchParams()
    for (const item of actions ?? []) {
      if (item) query.append('actions', item)
    }
    setQueryValue(query, 'action', action)
    setQueryValue(query, 'actor_type', actorType)
    setQueryValue(query, 'actor_id', actorId)
    setQueryValue(query, 'target_type', targetType)
    setQueryValue(query, 'target_id', targetId)
    setQueryValue(query, 'from', from)
    setQueryValue(query, 'to', to)
    if (limit !== undefined) setQueryValue(query, 'limit', limit)
    setQueryValue(query, 'cursor', cursor)
    return signal === undefined ? { query } : { query, signal }
  }

  async #requestBinary(
    method: string,
    path: string,
    idempotencyKey: string,
    userSignal?: AbortSignal,
  ): Promise<BinaryResponse> {
    const url = this.#baseURL + path
    const retrySafe = method !== 'POST' || idempotencyKey !== ''
    let lastError: AttuneError | undefined

    for (let attempt = 0; attempt <= this.#maxRetries; attempt++) {
      if (userSignal?.aborted) {
        throw new AttuneError({ code: TransportErrorCode.Aborted, message: 'request aborted' })
      }

      let result: { status: number; headers: Headers; data: Uint8Array }
      try {
        result = await this.#fetchBinaryOnce(method, url, idempotencyKey, userSignal)
      } catch (err) {
        const transportError = err as AttuneError
        if (transportError.code === TransportErrorCode.Aborted) throw transportError
        lastError = transportError
        if (retrySafe && attempt < this.#maxRetries) {
          await this.#sleep(backoffDelay(attempt))
          continue
        }
        throw transportError
      }

      const { status, headers, data } = result
      if (status >= 200 && status < 300) {
        const filename = parseContentDispositionFilename(headers)
        return {
          contentType: headers.get('content-type') ?? 'application/octet-stream',
          data,
          ...(filename !== undefined ? { filename } : {}),
        }
      }

      const text = new TextDecoder().decode(data)
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
    // POST retries are only safe when the server honors a stable
    // Idempotency-Key; the management writes that use this helper now auto-
    // generate one per call. GET/PUT/PATCH/DELETE remain retry-safe by method.
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
        if (status === 204) {
          return {} as T
        }
        if (text.trim() === '') {
          throw new AttuneError({
            code: 'INTERNAL',
            status,
            headers,
            message: 'empty response body for non-204 success response',
          })
        }
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

  async #fetchBinaryOnce(
    method: string,
    url: string,
    idempotencyKey: string,
    userSignal?: AbortSignal,
  ): Promise<{ status: number; headers: Headers; data: Uint8Array }> {
    const controller = new AbortController()
    let timedOut = false
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
      if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey
      const response = await this.#fetch(url, {
        method,
        headers,
        signal: controller.signal,
        redirect: 'manual',
      })
      const data = await readCappedBytes(response)
      return { status: response.status, headers: response.headers, data }
    } catch (cause) {
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

async function readCappedBytes(response: Response): Promise<Uint8Array> {
  const body = response.body
  if (!body || typeof body.getReader !== 'function') {
    const buf = new Uint8Array(await response.arrayBuffer())
    if (buf.byteLength > MAX_RESPONSE_BYTES) {
      throw new AttuneError({
        code: 'INTERNAL',
        status: response.status,
        message: `response body exceeds the ${MAX_RESPONSE_BYTES}-byte cap`,
      })
    }
    return buf
  }
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
  const out = new Uint8Array(total)
  let offset = 0
  for (const chunk of chunks) {
    out.set(chunk, offset)
    offset += chunk.byteLength
  }
  return out
}

// CR/LF in a header value enables header injection; fetch would reject it as an
// opaque (retryable-looking) network error, so reject it up front as a clear
// non-retryable client error.
function hasHeaderControlChar(s: string): boolean {
  return s.includes('\r') || s.includes('\n')
}

function parseContentDispositionFilename(headers: Headers): string | undefined {
  const contentDisposition = headers.get('content-disposition')
  if (!contentDisposition) return undefined
  const encoded = contentDisposition.match(/filename\*=UTF-8''([^;]+)/i)
  if (encoded?.[1]) {
    try {
      return decodeURIComponent(encoded[1])
    } catch {
      return encoded[1]
    }
  }
  const quoted = contentDisposition.match(/filename="([^"]+)"/i)
  if (quoted?.[1]) return quoted[1]
  const bare = contentDisposition.match(/filename=([^;]+)/i)
  return bare?.[1]?.trim()
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
