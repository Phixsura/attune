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

  it('rejects malformed JS option types up front', () => {
    // @ts-expect-error deliberate JS-style misuse
    expect(() => new Client({ baseURL: 123, apiKey: KEY })).toThrow(/baseURL must be a string/)
    // @ts-expect-error deliberate JS-style misuse
    expect(() => new Client({ baseURL: BASE, apiKey: 123 })).toThrow(/apiKey must be a string/)
    // @ts-expect-error deliberate JS-style misuse
    expect(() => new Client({ baseURL: BASE, apiKey: KEY, fetch: 123 })).toThrow(
      /fetch must be a function/,
    )
    // @ts-expect-error deliberate JS-style misuse
    expect(() => new Client({ baseURL: BASE, apiKey: KEY, timeout: '30' })).toThrow(
      /timeout must be a finite number/,
    )
    // @ts-expect-error deliberate JS-style misuse
    expect(() => new Client({ baseURL: BASE, apiKey: KEY, maxRetries: '2' })).toThrow(
      /maxRetries must be a finite number/,
    )
    // @ts-expect-error deliberate JS-style misuse
    expect(() => new Client({ baseURL: BASE, apiKey: KEY, defaultHeaders: 'bad' })).toThrow(
      /defaultHeaders must be an object of string values/,
    )
    expect(
      () =>
        new Client({
          baseURL: BASE,
          apiKey: KEY,
          defaultHeaders: { 'x-trace-id': 1 as unknown as string },
        }),
    ).toThrow(/defaultHeaders must be an object of string values/)
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

  it('rejects a non-http(s) or malformed baseURL at construction (parity with Go)', () => {
    expect(() => new Client({ baseURL: 'ftp://attune.example.test', apiKey: KEY })).toThrow(
      /baseURL must be http or https/,
    )
    expect(() => new Client({ baseURL: 'not a url', apiKey: KEY })).toThrow(/invalid baseURL/)
    // host-less inputs that `new URL` tolerates but Go's url.Parse rejects.
    expect(() => new Client({ baseURL: 'http:foo', apiKey: KEY })).toThrow(/invalid baseURL/)
    expect(() => new Client({ baseURL: 'http:///v1/path', apiKey: KEY })).toThrow(/invalid baseURL/)
    expect(
      () => new Client({ baseURL: 'https://user:pass@attune.example.test', apiKey: KEY }),
    ).toThrow(/baseURL must not include credentials/)
    expect(() => new Client({ baseURL: 'https://attune.example.test?x=1', apiKey: KEY })).toThrow(
      /baseURL must not include query or fragment/,
    )
    expect(() => new Client({ baseURL: 'https://attune.example.test#frag', apiKey: KEY })).toThrow(
      /baseURL must not include query or fragment/,
    )
    // http and https both accepted.
    expect(() => new Client({ baseURL: 'http://localhost:8080', apiKey: KEY })).not.toThrow()
  })
})

describe('reserved headers cannot be overridden by defaultHeaders', () => {
  it('drops case-variant reserved keys so they are not concatenated by Headers', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    const client = new Client({
      baseURL: BASE,
      apiKey: KEY,
      fetch,
      sleep: noSleep,
      defaultHeaders: {
        // Case variants of reserved headers — must NOT survive into the request.
        'x-api-key': 'attacker-key',
        'Content-Type': 'text/evil',
        'IDEMPOTENCY-KEY': 'spoofed',
        'User-Agent': 'spoofed-ua',
        'x-attune-api-version': '1900-01-01',
        // A genuinely custom header is preserved.
        'x-trace-id': 'trace-123',
      },
    })
    await client.ingest({ content: 'x' }, { idempotencyKey: 'real-idem-key' })
    const h = new Headers(calls[0]?.init.headers)
    expect(h.get('x-api-key')).toBe(KEY) // not "KEY, attacker-key"
    expect(h.get('content-type')).toBe('application/json') // not "application/json, text/evil"
    expect(h.get('idempotency-key')).toBe('real-idem-key')
    expect(h.get('user-agent')).toMatch(/^attune-node\//) // SDK UA, not "spoofed-ua"
    expect(h.get('user-agent')).not.toContain('spoofed-ua')
    expect(h.get('x-attune-api-version')).toBe('2026-06-28')
    expect(h.get('x-trace-id')).toBe('trace-123') // custom header preserved
  })
})

describe('adversarial inputs / config edges', () => {
  it('rejects a non-object ingest payload before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    const client = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(client.ingest(null)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'ingest input must be an object',
    })
    expect(calls).toHaveLength(0)
  })

  it('rejects malformed ingest options before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    const client = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(client.ingest({ content: 'x' }, 'bad')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'request options must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(client.ingest({ content: 'x' }, { idempotencyKey: 123 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'idempotencyKey must be a string',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(client.ingest({ content: 'x' }, { signal: 'bad' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'signal must be an AbortSignal',
    })
    await expect(
      client.ingest({ content: 'x' }, { signal: { aborted: false } as unknown as AbortSignal }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'signal must be an AbortSignal',
    })
    await expect(
      client.ingest(
        { content: 'x' },
        {
          signal: {
            aborted: false,
            addEventListener() {},
            removeEventListener() {},
          } as unknown as AbortSignal,
        },
      ),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'signal must be an AbortSignal',
    })
    expect(calls).toHaveLength(0)
  })

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
    const bad = { content: 'x', sourceMeta: { big: 1n } as any }
    await expect(client.ingest(bad)).rejects.toBeInstanceOf(AttuneError)
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

  it('maps an empty non-204 success body to AttuneError INTERNAL', async () => {
    const { fetch } = stubFetch([() => new Response('', { status: 200 })])
    await expect(
      newClient(fetch, { maxRetries: 0 }).ingest({ content: 'x' }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'INTERNAL',
      status: 200,
      message: 'empty response body for non-204 success response',
    })
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

  it('enforces the 1 MiB cap even when fetch only exposes a non-stream body fallback', async () => {
    const huge = JSON.stringify({
      id: '1',
      enrichmentStatus: 'pending',
      pad: 'x'.repeat(1024 * 1024),
    })
    const bytes = new TextEncoder().encode(huge)
    const fetch: FetchLike = () =>
      Promise.resolve({
        status: 200,
        headers: new Headers({ 'content-type': 'application/json' }),
        body: null,
        text: () => Promise.resolve(huge),
        arrayBuffer: () => Promise.resolve(bytes.buffer),
      } as unknown as Response)
    await expect(
      newClient(fetch, { maxRetries: 0 }).ingest({ content: 'x' }),
    ).rejects.toMatchObject({
      code: 'INTERNAL',
      message: 'response body exceeds the 1048576-byte cap',
    })
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

  it('rejects an oversized declared content-length before reading a non-stream body', async () => {
    let called = false
    const { fetch } = stubFetch([
      () =>
        ({
          status: 200,
          headers: new Headers({
            'content-type': 'application/json',
            'content-length': String(1024 * 1024 + 1),
          }),
          body: null,
          arrayBuffer: async () => {
            called = true
            return new TextEncoder().encode(
              JSON.stringify({ id: '1', enrichmentStatus: 'pending' }),
            ).buffer
          },
          text: async () => {
            called = true
            return JSON.stringify({ id: '1', enrichmentStatus: 'pending' })
          },
        }) as Response,
    ])
    await expect(
      newClient(fetch, { maxRetries: 0 }).ingest({ content: 'x' }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'INTERNAL',
    })
    expect(called).toBe(false)
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

describe('headers: User-Agent + defaultHeaders', () => {
  it('sends a versioned User-Agent header', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    await newClient(fetch).ingest({ content: 'x' })
    const ua = new Headers(calls[0]?.init.headers).get('user-agent')
    expect(ua).toMatch(/^attune-node\/\d+\.\d+\.\d+/)
  })

  it('omits User-Agent in browser-like runtimes while still pinning the API version', async () => {
    const browserGlobals = globalThis as { window?: Window; document?: Document }
    const savedWindow = browserGlobals.window
    const savedDocument = browserGlobals.document
    const originalProcess = globalThis.process
    // Simulate a browser global so the SDK does not try to force a forbidden
    // User-Agent header on window.fetch.
    browserGlobals.window = {} as Window
    browserGlobals.document = {} as Document
    Object.defineProperty(globalThis, 'process', { value: undefined, configurable: true })
    try {
      const { fetch, calls } = stubFetch([
        () => json(200, { id: '1', enrichmentStatus: 'pending' }),
      ])
      await newClient(fetch).ingest({ content: 'x' })
      const headers = new Headers(calls[0]?.init.headers)
      expect(headers.has('user-agent')).toBe(false)
      expect(headers.get('x-attune-api-version')).toBe('2026-06-28')
    } finally {
      if (savedWindow === undefined) {
        delete browserGlobals.window
      } else {
        browserGlobals.window = savedWindow
      }
      if (savedDocument === undefined) {
        delete browserGlobals.document
      } else {
        browserGlobals.document = savedDocument
      }
      Object.defineProperty(globalThis, 'process', {
        value: originalProcess,
        configurable: true,
        writable: true,
      })
    }
  })

  it('omits User-Agent in worker-like runtimes without a real Node process', async () => {
    const original = globalThis.process
    Object.defineProperty(globalThis, 'process', { value: undefined, configurable: true })
    try {
      const { fetch, calls } = stubFetch([
        () => json(200, { id: '1', enrichmentStatus: 'pending' }),
      ])
      await newClient(fetch).ingest({ content: 'x' })
      const headers = new Headers(calls[0]?.init.headers)
      expect(headers.has('user-agent')).toBe(false)
      expect(headers.get('x-attune-api-version')).toBe('2026-06-28')
    } finally {
      Object.defineProperty(globalThis, 'process', {
        value: original,
        configurable: true,
        writable: true,
      })
    }
  })

  it('pins the current public API version header', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    await newClient(fetch).ingest({ content: 'x' })
    const version = new Headers(calls[0]?.init.headers).get('x-attune-api-version')
    expect(version).toBe('2026-06-28')
  })

  it('merges defaultHeaders but never lets them override reserved headers', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: '1', enrichmentStatus: 'pending' })])
    const client = new Client({
      baseURL: BASE,
      apiKey: KEY,
      fetch,
      sleep: noSleep,
      defaultHeaders: {
        'x-trace': 'abc',
        'X-API-Key': 'HACKED',
        'user-agent': 'evil',
        'X-Attune-Api-Version': '1900-01-01',
      },
    })
    await client.ingest({ content: 'x' })
    const h = new Headers(calls[0]?.init.headers)
    expect(h.get('x-trace')).toBe('abc') // custom header added
    expect(h.get('x-api-key')).toBe(KEY) // reserved: not overridden
    expect(h.get('user-agent')).toMatch(/^attune-node\//) // reserved: not overridden
    expect(h.get('x-attune-api-version')).toBe('2026-06-28')
  })
})

describe('management binary downloads', () => {
  it('prefers and decodes RFC 5987 filename* values for audit exports', async () => {
    const body = 'id,action\n1,tag.create\n'
    const { fetch } = stubFetch([
      () =>
        new Response(body, {
          status: 200,
          headers: {
            'content-type': 'text/csv; charset=utf-8',
            'content-disposition': `attachment; filename="fallback.csv"; filename*=UTF-8'en'audit-%E2%82%AC.csv`,
          },
        }),
    ])
    const res = await newClient(fetch).exportAuditLogCSV({ actions: ['tag.create'] })
    expect(res.filename).toBe('audit-€.csv')
    expect(new TextDecoder().decode(res.data)).toBe(body)
  })

  it('preserves quoted fallback filenames containing semicolons', async () => {
    const { fetch } = stubFetch([
      () =>
        new Response('zip', {
          status: 200,
          headers: {
            'content-type': 'application/zip',
            'content-disposition': `attachment; filename="audit; q2.zip"`,
          },
        }),
    ])
    const res = await newClient(fetch).downloadAuditEvidenceExport('job_123')
    expect(res.filename).toBe('audit; q2.zip')
  })

  it('keeps the raw RFC 5987 suffix when percent-decoding fails', async () => {
    const { fetch } = stubFetch([
      () =>
        new Response('zip', {
          status: 200,
          headers: {
            'content-type': 'application/zip',
            'content-disposition': `attachment; filename*=UTF-8''gdpr-%ZZ.zip`,
          },
        }),
    ])
    const res = await newClient(fetch).downloadGdprExport('job_123')
    expect(res.filename).toBe('gdpr-%ZZ.zip')
  })

  it('falls back to plain filename when filename* is malformed', async () => {
    const { fetch } = stubFetch([
      () =>
        new Response('zip', {
          status: 200,
          headers: {
            'content-type': 'application/zip',
            'content-disposition': `attachment; filename="fallback.zip"; filename*=UTF-8''bad-%ZZ.zip`,
          },
        }),
    ])
    const res = await newClient(fetch).downloadGdprExport('job_123')
    expect(res.filename).toBe('fallback.zip')
  })
})
