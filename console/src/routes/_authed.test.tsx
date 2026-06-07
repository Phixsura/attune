import { QueryClient } from '@tanstack/react-query'
import { isRedirect } from '@tanstack/react-router'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { setCsrfToken } from '@/lib/api-client'
import { Route as AuthedRoute } from '@/routes/_authed'
import { server } from '@/testing/mocks/server'

// Direct test of the production beforeLoad — we import the Route from
// _authed.tsx and call its options.beforeLoad() with a mocked context.
// This is what makes the production source actually covered in
// coverage reports (an earlier iteration of this file built a parallel
// route tree, which let _authed.tsx rot at 0% covered).

type BeforeLoadFn = NonNullable<typeof AuthedRoute.options.beforeLoad>
type BeforeLoadCtx = Parameters<BeforeLoadFn>[0]

// `redirect()` throws an object whose external shape is { options: ... }.
// The TanStack Router type for the Redirect class hides the internals;
// the test only needs to read the user-supplied to/search.
interface ThrownRedirect {
  options: { to: string; search: { redirect?: string }; statusCode?: number }
}

function makeContext(pathname: string): BeforeLoadCtx {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return {
    context: { queryClient },
    location: { pathname } as BeforeLoadCtx['location'],
  } as BeforeLoadCtx
}

async function callBeforeLoad(pathname: string): Promise<unknown> {
  const beforeLoad = AuthedRoute.options.beforeLoad as BeforeLoadFn
  try {
    await beforeLoad(makeContext(pathname))
    return null
  } catch (err) {
    return err
  }
}

describe('_authed Route.options.beforeLoad (production source)', () => {
  it('401 from /me throws redirect to /login with redirect=<pathname>', async () => {
    setCsrfToken(null)
    server.use(http.get('/fb/v1/console/me', () => new HttpResponse(null, { status: 401 })))
    const thrown = await callBeforeLoad('/feedback')
    expect(isRedirect(thrown)).toBe(true)
    const r = thrown as ThrownRedirect
    expect(r.options.to).toBe('/login')
    expect(r.options.search.redirect).toBe('/feedback')
  })

  it('200 from /me resolves without throwing (page proceeds)', async () => {
    setCsrfToken(null)
    server.use(
      http.get('/fb/v1/console/me', () =>
        HttpResponse.json({
          tenant: { id: 't', name: 'T', slug: 't', locale: 'zh-CN', timezone: 'UTC' },
          user: { openId: 'u', name: 'U', role: 'admin' },
          csrfToken: 'tok',
        }),
      ),
    )
    expect(await callBeforeLoad('/feedback')).toBeNull()
  })

  it('captures the exact requested pathname (/api-keys → redirect=/api-keys)', async () => {
    setCsrfToken(null)
    server.use(http.get('/fb/v1/console/me', () => new HttpResponse(null, { status: 401 })))
    const thrown = await callBeforeLoad('/api-keys')
    expect(isRedirect(thrown)).toBe(true)
    const r = thrown as ThrownRedirect
    expect(r.options.to).toBe('/login')
    expect(r.options.search.redirect).toBe('/api-keys')
  })
})
