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

  it('does NOT retry a 422 validation error', async () => {
    const { fetch, calls } = stubFetch([
      () => json(422, { code: 'VALIDATION', message: 'content required' }),
    ])
    const client = newClient(fetch, { maxRetries: 2 })

    await expect(client.ingest({ content: '' })).rejects.toMatchObject({ code: 'VALIDATION' })
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
