import type { ErrorResponse } from './proto/attune/v1/common'

/**
 * Synthetic codes the SDK assigns to transport-level failures that never reach
 * the server's {@link ErrorResponse} envelope. They sit alongside the server
 * `ErrorCode` enum values (e.g. "VALIDATION", "UNAUTHORIZED") on
 * {@link AttuneError.code} — switch on `code` for both.
 */
export const TransportErrorCode = {
  /** The request never completed (DNS, connection reset, fetch threw). */
  Network: 'NETWORK',
  /** The per-attempt timeout elapsed before a response arrived. */
  Timeout: 'TIMEOUT',
  /** The caller's AbortSignal fired. Never retried. */
  Aborted: 'ABORTED',
} as const

export interface AttuneErrorInit {
  message: string
  /** Server `ErrorResponse.code` value, or a {@link TransportErrorCode}. */
  code: string
  /** HTTP status, when the failure carried a response. */
  status?: number
  /** Request id from the error envelope / `X-Request-ID`, for support. */
  requestId?: string
  /** Response headers, when present. */
  headers?: Headers
  cause?: unknown
}

/**
 * The single error type the SDK throws. Carries the stable machine-readable
 * `code` (switch on this, never on `message`), the HTTP `status`, the
 * `requestId` for log correlation, and the response `headers` when available.
 */
export class AttuneError extends Error {
  readonly code: string
  readonly status?: number
  readonly requestId?: string
  readonly headers?: Headers

  constructor(init: AttuneErrorInit) {
    super(init.message, init.cause !== undefined ? { cause: init.cause } : undefined)
    this.name = 'AttuneError'
    this.code = init.code
    this.status = init.status
    this.requestId = init.requestId
    this.headers = init.headers
  }

  /** Build an error from a non-2xx response and its (best-effort) parsed body. */
  static fromResponse(
    status: number,
    body: ErrorResponse | undefined,
    headers: Headers,
  ): AttuneError {
    return new AttuneError({
      status,
      headers,
      code: body?.code || codeFromStatus(status),
      message: body?.message || `attune request failed with status ${status}`,
      requestId: body?.requestId || headers.get('x-request-id') || undefined,
    })
  }
}

/** Fallback mapping when the body is missing or unparseable. */
function codeFromStatus(status: number): string {
  switch (status) {
    case 400:
      return 'BAD_REQUEST'
    case 401:
      return 'UNAUTHORIZED'
    case 403:
      return 'FORBIDDEN'
    case 404:
      return 'NOT_FOUND'
    case 409:
      return 'CONFLICT'
    case 413:
      return 'BODY_TOO_LARGE'
    case 422:
      return 'VALIDATION'
    case 429:
      return 'RATE_LIMITED'
    default:
      return status >= 500 ? 'INTERNAL' : 'BAD_REQUEST'
  }
}
