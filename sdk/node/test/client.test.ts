import { describe, expect, it, vi } from 'vitest'
import { AttuneError, Client, type FetchLike } from '../src/index'

const BASE = 'https://attune.example.test'
const KEY = 'ak_test_key'

/** A fetch stub that returns queued responses in order and records calls. */
function stubFetch(responses: Array<() => Response | Promise<Response>>): {
  fetch: FetchLike
  calls: Array<{ url: string; init: RequestInit }>
} {
  const calls: Array<{ url: string; init: RequestInit }> = []
  let i = 0
  const fetch: FetchLike = (url, init) => {
    calls.push({ url, init })
    const next = responses[Math.min(i, responses.length - 1)]
    i += 1
    if (!next) throw new Error('no stubbed response')
    return Promise.resolve(next())
  }
  return { fetch, calls }
}

function json(status: number, body: unknown, headers?: Record<string, string>): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  })
}

// no-op sleep so retry tests do not wait on real timers
const noSleep = () => Promise.resolve()

function newClient(fetch: FetchLike, overrides = {}): Client {
  return new Client({ baseURL: BASE, apiKey: KEY, fetch, sleep: noSleep, ...overrides })
}

describe('constructor', () => {
  it('requires baseURL and apiKey', () => {
    expect(() => new Client({ baseURL: '', apiKey: KEY })).toThrow(AttuneError)
    expect(() => new Client({ baseURL: BASE, apiKey: '' })).toThrow(AttuneError)
  })

  it('throws when no fetch is available and none injected', () => {
    const saved = globalThis.fetch
    // @ts-expect-error force-remove for the test
    globalThis.fetch = undefined
    try {
      expect(() => new Client({ baseURL: BASE, apiKey: KEY })).toThrow(/no global fetch/)
    } finally {
      globalThis.fetch = saved
    }
  })
})

describe('adversarial inputs / config edges', () => {
  it('wraps a non-JSON-serializable body as AttuneError, never a raw TypeError', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    const client = newClient(fetch)
    // biome-ignore lint/suspicious/noExplicitAny: deliberately invalid payload
    const circular: any = {}
    circular.self = circular
    await expect(client.ingest({ content: 'x', sourceMeta: circular })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
    })
    // biome-ignore lint/suspicious/noExplicitAny: BigInt is not serializable
    await expect(
      client.ingest({ content: 'x', sourceMeta: { big: 1n } as any }),
    ).rejects.toBeInstanceOf(AttuneError)
    expect(calls).toHaveLength(0) // never hit the network
  })

  it('clamps a negative maxRetries to 0 (still makes one real attempt)', async () => {
    const { fetch, calls } = stubFetch([() => json(503, { code: 'BAD_GATEWAY', message: 'down' })])
    const client = newClient(fetch, { maxRetries: -1 })
    await expect(client.ingest({ content: 'x' })).rejects.toMatchObject({ status: 503 })
    expect(calls).toHaveLength(1) // one attempt, not zero
  })

  it('treats timeout <= 0 as no per-attempt deadline (does not instant-abort)', async () => {
    const { fetch } = stubFetch([() => json(200, { id: '7', enrichmentStatus: 'pending' })])
    const client = new Client({ baseURL: BASE, apiKey: KEY, fetch, sleep: noSleep, timeout: 0 })
    const res = await client.ingest({ content: 'x' })
    expect(res.id).toBe('7')
  })
})

describe('ingest — happy path', () => {
  it('posts to /v1/feedback/ingest with the X-API-Key header and returns the body', async () => {
    const { fetch, calls } = stubFetch([
      () => json(200, { id: '4242', enrichmentStatus: 'pending' }),
    ])
    const client = newClient(fetch)

    const res = await client.ingest({ content: 'love it', source: 'api' })

    expect(res).toEqual({ id: '4242', enrichmentStatus: 'pending' })
    expect(calls).toHaveLength(1)
    expect(calls[0]?.url).toBe(`${BASE}/v1/feedback/ingest`)
    expect(calls[0]?.init.method).toBe('POST')
    const headers = new Headers(calls[0]?.init.headers)
    expect(headers.get('x-api-key')).toBe(KEY)
    expect(calls[0]?.init.body).toBe(JSON.stringify({ content: 'love it', source: 'api' }))
  })

  it('strips a trailing slash from baseURL', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    const client = new Client({ baseURL: `${BASE}/`, apiKey: KEY, fetch, sleep: noSleep })
    await client.ingest({ content: 'x' })
    expect(calls[0]?.url).toBe(`${BASE}/v1/feedback/ingest`)
  })
})

describe('ingest — error mapping', () => {
  it('throws AttuneError carrying code/status/requestId from the envelope', async () => {
    const { fetch } = stubFetch([
      () => json(401, { code: 'UNAUTHORIZED', message: 'invalid api key', requestId: 'req-1' }),
    ])
    const client = newClient(fetch)

    await expect(client.ingest({ content: 'x' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'UNAUTHORIZED',
      status: 401,
      requestId: 'req-1',
    })
  })

  it('synthesizes a code when the body is not JSON', async () => {
    const { fetch } = stubFetch([
      () => new Response('nope', { status: 500, headers: { 'x-request-id': 'req-2' } }),
    ])
    const client = newClient(fetch, { maxRetries: 0 })

    await expect(client.ingest({ content: 'x' })).rejects.toMatchObject({
      code: 'INTERNAL',
      status: 500,
      requestId: 'req-2',
    })
  })
})

describe('ingest — retry policy', () => {
  it('retries 429 then succeeds, honoring Retry-After', async () => {
    const sleep = vi.fn(() => Promise.resolve())
    const { fetch, calls } = stubFetch([
      () => json(429, { code: 'RATE_LIMITED', message: 'slow down' }, { 'retry-after': '0' }),
      () => json(200, { id: '7', enrichmentStatus: 'pending' }),
    ])
    const client = new Client({ baseURL: BASE, apiKey: KEY, fetch, sleep })

    const res = await client.ingest({ content: 'x' })

    expect(res.id).toBe('7')
    expect(calls).toHaveLength(2)
    expect(sleep).toHaveBeenCalledWith(0) // Retry-After: 0
  })

  it('retries 5xx up to maxRetries then throws the last error', async () => {
    const { fetch, calls } = stubFetch([() => json(503, { code: 'BAD_GATEWAY', message: 'down' })])
    const client = newClient(fetch, { maxRetries: 2 })

    await expect(client.ingest({ content: 'x' })).rejects.toMatchObject({ status: 503 })
    expect(calls).toHaveLength(3) // 1 initial + 2 retries
  })

  it('does NOT retry a validation error (server sends 400 + VALIDATION)', async () => {
    const { fetch, calls } = stubFetch([
      () => json(400, { code: 'VALIDATION', message: 'content required' }),
    ])
    const client = newClient(fetch, { maxRetries: 2 })

    await expect(client.ingest({ content: '' })).rejects.toMatchObject({
      status: 400,
      code: 'VALIDATION',
    })
    expect(calls).toHaveLength(1)
  })

  it('does NOT retry a 409 IDEMPOTENCY_CONFLICT (permanent)', async () => {
    const { fetch, calls } = stubFetch([
      () => json(409, { code: 'IDEMPOTENCY_CONFLICT', message: 'different body' }),
    ])
    const client = newClient(fetch, { maxRetries: 2 })

    await expect(client.ingest({ content: 'x' })).rejects.toMatchObject({
      code: 'IDEMPOTENCY_CONFLICT',
      status: 409,
    })
    expect(calls).toHaveLength(1)
  })

  it('retries a network error then throws code NETWORK', async () => {
    let n = 0
    const fetch: FetchLike = () => {
      n += 1
      return Promise.reject(new TypeError('connection refused'))
    }
    const client = newClient(fetch, { maxRetries: 1 })

    await expect(client.ingest({ content: 'x' })).rejects.toMatchObject({ code: 'NETWORK' })
    expect(n).toBe(2) // 1 initial + 1 retry
  })
})

describe('ingest — idempotency', () => {
  const keyOf = (call?: { init: RequestInit }) =>
    new Headers(call?.init.headers).get('idempotency-key')

  it('sends a UUID Idempotency-Key header by default', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    await newClient(fetch).ingest({ content: 'x' })
    expect(keyOf(calls[0])).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i,
    )
  })

  it('reuses the SAME key across retries of one call (safe retry)', async () => {
    const { fetch, calls } = stubFetch([
      () => json(503, { code: 'BAD_GATEWAY', message: 'down' }),
      () => json(200, { id: '1', enrichmentStatus: 'pending' }),
    ])
    await newClient(fetch, { maxRetries: 1 }).ingest({ content: 'x' })
    expect(calls).toHaveLength(2)
    expect(keyOf(calls[0])).toBeTruthy()
    expect(keyOf(calls[0])).toBe(keyOf(calls[1]))
  })

  it('honors a caller-supplied idempotencyKey', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    await newClient(fetch).ingest({ content: 'x' }, { idempotencyKey: 'my-fixed-key-123' })
    expect(keyOf(calls[0])).toBe('my-fixed-key-123')
  })
})

describe('ingest — cancellation', () => {
  it('throws code ABORTED and does not retry when the caller signal is aborted', async () => {
    const { fetch, calls } = stubFetch([
      () => Promise.reject(Object.assign(new Error('aborted'), { name: 'AbortError' })),
    ])
    const client = newClient(fetch, { maxRetries: 3 })
    const controller = new AbortController()
    controller.abort()

    await expect(
      client.ingest({ content: 'x' }, { signal: controller.signal }),
    ).rejects.toMatchObject({
      code: 'ABORTED',
    })
    expect(calls).toHaveLength(0) // aborted before the first attempt
  })
})

describe('transport: slow / partial / malformed responses', () => {
  it('maps a malformed 200 body to AttuneError INTERNAL (not a raw SyntaxError)', async () => {
    const { fetch } = stubFetch([() => new Response('this is not json{', { status: 200 })])
    await expect(
      newClient(fetch, { maxRetries: 0 }).ingest({ content: 'x' }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'INTERNAL',
      status: 200,
    })
  })

  it('maps an empty 200 body to AttuneError INTERNAL', async () => {
    const { fetch } = stubFetch([() => new Response('', { status: 200 })])
    await expect(
      newClient(fetch, { maxRetries: 0 }).ingest({ content: 'x' }),
    ).rejects.toBeInstanceOf(AttuneError)
  })

  it('times out a slow/hanging response BODY (not just slow headers)', async () => {
    // fetch resolves headers immediately; the body read hangs until aborted.
    const hangFetch: FetchLike = (_url, init) =>
      Promise.resolve({
        status: 200,
        headers: new Headers(),
        text: () =>
          new Promise<string>((_resolve, reject) => {
            init.signal?.addEventListener('abort', () =>
              reject(new DOMException('aborted', 'AbortError')),
            )
          }),
      } as unknown as Response)
    const client = new Client({
      baseURL: BASE,
      apiKey: KEY,
      fetch: hangFetch,
      timeout: 50,
      maxRetries: 0,
    })
    await expect(client.ingest({ content: 'x' })).rejects.toMatchObject({ code: 'TIMEOUT' })
  })
})

describe('security: redirects are not followed (no API-key leak)', () => {
  it('surfaces a 3xx as AttuneError, sets redirect:manual, and never re-sends', async () => {
    const { fetch, calls } = stubFetch([
      () => new Response('', { status: 307, headers: { location: 'http://evil.example/x' } }),
    ])
    const client = newClient(fetch, { maxRetries: 0 })
    await expect(client.ingest({ content: 'x' })).rejects.toMatchObject({
      name: 'AttuneError',
      status: 307,
    })
    expect(calls).toHaveLength(1) // not followed to the redirect target
    expect((calls[0]?.init as RequestInit).redirect).toBe('manual')
  })
})

describe('security: header & response-size hardening', () => {
  it('rejects CR/LF in apiKey at construction (header injection)', () => {
    expect(() => new Client({ baseURL: BASE, apiKey: 'k\r\nX-Evil: 1' })).toThrow(AttuneError)
  })

  it('rejects CR/LF in a caller idempotencyKey with BAD_REQUEST, no request made', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    await expect(
      newClient(fetch).ingest({ content: 'x' }, { idempotencyKey: 'k\r\nX-Evil: 1' }),
    ).rejects.toMatchObject({ name: 'AttuneError', code: 'BAD_REQUEST' })
    expect(calls).toHaveLength(0)
  })

  it('caps an oversized response body instead of buffering it all (OOM guard)', async () => {
    const huge = 'x'.repeat(1024 * 1024 + 16) // > 1 MiB cap
    const { fetch } = stubFetch([() => new Response(huge, { status: 200 })])
    await expect(
      newClient(fetch, { maxRetries: 0 }).ingest({ content: 'x' }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'INTERNAL',
    })
  })
})

describe('constructor: JS-caller misuse (no type checking)', () => {
  it('throws AttuneError (not a raw TypeError) for missing/non-object options', () => {
    // biome-ignore lint/suspicious/noExplicitAny: simulating an untyped JS caller
    const C = Client as any
    expect(() => new C()).toThrow(AttuneError)
    expect(() => new C(null)).toThrow(AttuneError)
    expect(() => new C('nope')).toThrow(AttuneError)
    expect(() => new C(42)).toThrow(AttuneError)
  })
})
