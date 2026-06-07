import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { meQuery } from '@/features/session/api/get-me'
import { getCsrfToken, setCsrfToken } from '@/lib/api-client'
import { server } from '@/testing/mocks/server'

describe('meQuery side-effect', () => {
  // The api-client's CSRF token is module-level. Reset it before AND
  // after each case so failures during a test don't leak token state
  // to the next.
  beforeEach(() => setCsrfToken(null))
  afterEach(() => setCsrfToken(null))

  it('200 stores response.csrfToken into the api-client', async () => {
    server.use(
      http.get('/fb/v1/console/me', () =>
        HttpResponse.json({
          tenant: { id: 't', name: 'T', slug: 't' },
          user: { id: 'u', email: 'u@e.com', displayName: 'U' },
          csrfToken: 'abc-csrf',
        }),
      ),
    )
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const data = await qc.ensureQueryData(meQuery())
    expect(data.csrfToken).toBe('abc-csrf')
    expect(getCsrfToken()).toBe('abc-csrf')
  })

  it('401 throws and leaves CSRF state untouched', async () => {
    server.use(http.get('/fb/v1/console/me', () => new HttpResponse(null, { status: 401 })))
    setCsrfToken('pre-existing')
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    await expect(qc.ensureQueryData(meQuery())).rejects.toThrow()
    // Token unchanged — the 401 path never executes setCsrfToken.
    expect(getCsrfToken()).toBe('pre-existing')
  })
})
