// CSRF token is set by /me's response body and stored here in module
// state. The SPA NEVER writes it to a cookie — it must be JS-readable
// but not auto-attached. Each fetch() pulls it from this closure.
let csrfToken: string | null = null

export function setCsrfToken(t: string | null) {
  csrfToken = t
}
export function getCsrfToken(): string | null {
  return csrfToken
}

export interface ApiError extends Error {
  status: number
  code?: string
  requestId?: string
}

function apiError(status: number, code: string, message: string, requestId?: string): ApiError {
  const err = new Error(message) as ApiError
  err.status = status
  err.code = code
  err.requestId = requestId
  return err
}

interface FetchOpts {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'
  body?: unknown
  signal?: AbortSignal
}

// api makes a typed fetch against /fb/v1/console/*. Returns parsed JSON
// or throws ApiError for non-2xx (with code/message from the server's
// unified {code, message, requestId} envelope when present).
//
// All non-GET requests automatically include X-CSRF-Token from the
// module-level CSRF state. If the token is missing the request is sent
// anyway — the server will respond 403 csrf_invalid, which is the
// honest behavior we want during dev.
export async function api<T>(path: string, opts: FetchOpts = {}): Promise<T> {
  const method = opts.method ?? 'GET'
  const headers: Record<string, string> = {
    Accept: 'application/json',
  }
  let body: BodyInit | undefined
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }
  if (method !== 'GET' && csrfToken) {
    headers['X-CSRF-Token'] = csrfToken
  }
  const res = await fetch(path, {
    method,
    headers,
    body,
    credentials: 'include',
    signal: opts.signal,
  })
  if (res.status === 204) return undefined as T
  const text = await res.text()
  let parsed: unknown = null
  if (text) {
    try {
      parsed = JSON.parse(text) as unknown
    } catch {
      // non-JSON body; keep parsed=null
    }
  }
  if (!res.ok) {
    const envelope = parsed as { code?: string; message?: string; requestId?: string } | null
    throw apiError(
      res.status,
      envelope?.code ?? 'unknown',
      envelope?.message ?? `HTTP ${res.status}`,
      envelope?.requestId,
    )
  }
  return parsed as T
}
