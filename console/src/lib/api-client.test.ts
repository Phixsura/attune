import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { type ApiError, api, getCsrfToken, setCsrfToken } from '@/lib/api-client'
import { server } from '@/testing/mocks/server'

describe('api()', () => {
  // CSRF state is module-level singleton — reset it between tests so
  // ordering doesn't matter.
  beforeEach(() => {
    setCsrfToken(null)
  })

  describe('CSRF header injection', () => {
    it('GET omits X-CSRF-Token even when token is set', async () => {
      setCsrfToken('csrf-abc')
      let observedHeader: string | null = 'INIT'
      server.use(
        http.get('/fb/v1/csrf-get', ({ request }) => {
          observedHeader = request.headers.get('X-CSRF-Token')
          return HttpResponse.json({ ok: true })
        }),
      )
      await api('/fb/v1/csrf-get')
      expect(observedHeader).toBeNull()
    })

    it('POST includes X-CSRF-Token when token is set', async () => {
      setCsrfToken('csrf-abc')
      let observedHeader: string | null = 'INIT'
      server.use(
        http.post('/fb/v1/csrf-post', ({ request }) => {
          observedHeader = request.headers.get('X-CSRF-Token')
          return HttpResponse.json({ ok: true })
        }),
      )
      await api('/fb/v1/csrf-post', { method: 'POST', body: {} })
      expect(observedHeader).toBe('csrf-abc')
    })

    it('POST omits X-CSRF-Token when token is null', async () => {
      // token left as null (beforeEach reset)
      let observedHeader: string | null = 'INIT'
      server.use(
        http.post('/fb/v1/csrf-empty', ({ request }) => {
          observedHeader = request.headers.get('X-CSRF-Token')
          return HttpResponse.json({ ok: true })
        }),
      )
      await api('/fb/v1/csrf-empty', { method: 'POST', body: {} })
      expect(observedHeader).toBeNull()
    })

    it.each([
      'PUT',
      'PATCH',
      'DELETE',
    ] as const)('%s includes X-CSRF-Token when set', async (method) => {
      setCsrfToken('csrf-xyz')
      let observedHeader: string | null = 'INIT'
      server.use(
        http[method.toLowerCase() as 'put' | 'patch' | 'delete'](
          '/fb/v1/csrf-other',
          ({ request }) => {
            observedHeader = request.headers.get('X-CSRF-Token')
            return HttpResponse.json({ ok: true })
          },
        ),
      )
      await api('/fb/v1/csrf-other', { method })
      expect(observedHeader).toBe('csrf-xyz')
    })

    it('setCsrfToken(null) clears the token', () => {
      setCsrfToken('present')
      expect(getCsrfToken()).toBe('present')
      setCsrfToken(null)
      expect(getCsrfToken()).toBeNull()
    })
  })

  describe('response body handling', () => {
    it('returns undefined for 204', async () => {
      server.use(http.delete('/fb/v1/no-content', () => new HttpResponse(null, { status: 204 })))
      const result = await api('/fb/v1/no-content', { method: 'DELETE' })
      expect(result).toBeUndefined()
    })

    it('parses 2xx JSON body as typed payload', async () => {
      interface Foo {
        id: string
        n: number
      }
      server.use(http.get('/fb/v1/foo', () => HttpResponse.json({ id: 'x', n: 7 })))
      const result = await api<Foo>('/fb/v1/foo')
      expect(result).toEqual({ id: 'x', n: 7 })
    })

    it('returns null when 2xx body is empty / non-JSON', async () => {
      // parsed: unknown = null per api-client.ts:63
      server.use(http.get('/fb/v1/empty', () => new HttpResponse('', { status: 200 })))
      const result = await api('/fb/v1/empty')
      expect(result).toBeNull()
    })
  })

  describe('error envelope', () => {
    it('4xx with {code,message,requestId} envelope throws ApiError', async () => {
      server.use(
        http.get('/fb/v1/bad', () =>
          HttpResponse.json(
            { code: 'invalid_label', message: 'label too short', requestId: 'req-42' },
            { status: 400 },
          ),
        ),
      )
      try {
        await api('/fb/v1/bad')
        throw new Error('expected throw')
      } catch (err) {
        const e = err as ApiError
        expect(e.status).toBe(400)
        expect(e.code).toBe('invalid_label')
        expect(e.message).toBe('label too short')
        expect(e.requestId).toBe('req-42')
      }
    })

    it.each([
      500, 502, 503,
    ])('5xx (%i) without envelope throws fallback ApiError', async (status) => {
      server.use(
        http.get(`/fb/v1/no-envelope-${status}`, () => new HttpResponse('boom', { status })),
      )
      try {
        await api(`/fb/v1/no-envelope-${status}`)
        throw new Error('expected throw')
      } catch (err) {
        const e = err as ApiError
        expect(e.status).toBe(status)
        expect(e.code).toBe('unknown')
        expect(e.message).toBe(`HTTP ${status}`)
      }
    })

    it('4xx with JSON but missing envelope fields gets defaults', async () => {
      server.use(
        http.get('/fb/v1/partial', () =>
          HttpResponse.json({ unexpected: 'shape' }, { status: 422 }),
        ),
      )
      try {
        await api('/fb/v1/partial')
        throw new Error('expected throw')
      } catch (err) {
        const e = err as ApiError
        expect(e.status).toBe(422)
        expect(e.code).toBe('unknown')
        expect(e.message).toBe('HTTP 422')
        expect(e.requestId).toBeUndefined()
      }
    })
  })

  describe('network failures', () => {
    it('fetch rejection propagates as the raw error', async () => {
      const originalFetch = globalThis.fetch
      const boom = new TypeError('Failed to fetch')
      vi.stubGlobal('fetch', vi.fn().mockRejectedValue(boom))
      try {
        await api('/fb/v1/anywhere')
        throw new Error('expected throw')
      } catch (err) {
        expect(err).toBe(boom)
      } finally {
        vi.stubGlobal('fetch', originalFetch)
      }
    })

    it('AbortSignal propagates — aborted signal causes AbortError', async () => {
      const controller = new AbortController()
      controller.abort()
      server.use(http.get('/fb/v1/never', () => HttpResponse.json({})))
      try {
        await api('/fb/v1/never', { signal: controller.signal })
        throw new Error('expected throw')
      } catch (err) {
        // Either DOMException or AbortError — both have name === 'AbortError'.
        expect((err as Error).name).toBe('AbortError')
      }
    })
  })
})
