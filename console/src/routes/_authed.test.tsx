import { QueryClient } from '@tanstack/react-query'
import {
  createRootRouteWithContext,
  createRoute,
  Outlet,
  RouterProvider,
  redirect,
} from '@tanstack/react-router'
import { waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { meQuery } from '@/features/session/api/get-me'
import { setCsrfToken } from '@/lib/api-client'
import { server } from '@/testing/mocks/server'
import { makeTestRouter } from '@/testing/router-utils'
import { renderWithProviders } from '@/testing/test-utils'

// Mirrors the production _authed.tsx beforeLoad guard — the contract
// the test wants to lock in. If the real route file ever diverges from
// this shape, the divergence is intentional and this test (or its
// production peer) needs an update.
function buildRouteTree() {
  const rootRoute = createRootRouteWithContext<{ queryClient: QueryClient }>()({
    component: () => <Outlet />,
  })
  const loginRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/login',
    validateSearch: (search: Record<string, unknown>) => ({
      redirect: typeof search.redirect === 'string' ? search.redirect : undefined,
    }),
    component: () => <div data-testid="login-page">login</div>,
  })
  const authedRoute = createRoute({
    getParentRoute: () => rootRoute,
    id: '_authed',
    beforeLoad: async ({ context, location }) => {
      try {
        await context.queryClient.ensureQueryData(meQuery())
      } catch {
        throw redirect({ to: '/login', search: { redirect: location.pathname } })
      }
    },
    component: () => <Outlet />,
  })
  const feedbackRoute = createRoute({
    getParentRoute: () => authedRoute,
    path: '/feedback',
    component: () => <div data-testid="feedback-page">feedback</div>,
  })
  const apiKeysRoute = createRoute({
    getParentRoute: () => authedRoute,
    path: '/api-keys',
    component: () => <div data-testid="api-keys-page">api-keys</div>,
  })
  return rootRoute.addChildren([loginRoute, authedRoute.addChildren([feedbackRoute, apiKeysRoute])])
}

describe('_authed route guard', () => {
  it('401 from /me → redirect to /login with redirect=<original path>', async () => {
    setCsrfToken(null)
    server.use(http.get('/fb/v1/console/me', () => new HttpResponse(null, { status: 401 })))
    const { router, queryClient } = makeTestRouter({
      routeTree: buildRouteTree(),
      initialEntries: ['/feedback'],
    })
    const { findByTestId } = renderWithProviders(<RouterProvider router={router} />, {
      queryClient,
    })
    await findByTestId('login-page')
    expect(router.state.location.pathname).toBe('/login')
    expect((router.state.location.search as { redirect?: string }).redirect).toBe('/feedback')
  })

  it('200 from /me → renders the authed child (no redirect)', async () => {
    server.use(
      http.get('/fb/v1/console/me', () =>
        HttpResponse.json({
          tenant: { id: 't', name: 'T', slug: 't' },
          user: { id: 'u', email: 'u@e.com', displayName: 'U' },
          csrfToken: 'tok',
        }),
      ),
    )
    const { router, queryClient } = makeTestRouter({
      routeTree: buildRouteTree(),
      initialEntries: ['/feedback'],
    })
    const { findByTestId } = renderWithProviders(<RouterProvider router={router} />, {
      queryClient,
    })
    await findByTestId('feedback-page')
    expect(router.state.location.pathname).toBe('/feedback')
  })

  it('/api-keys while unauthenticated lands at /login?redirect=/api-keys', async () => {
    setCsrfToken(null)
    server.use(http.get('/fb/v1/console/me', () => new HttpResponse(null, { status: 401 })))
    const { router, queryClient } = makeTestRouter({
      routeTree: buildRouteTree(),
      initialEntries: ['/api-keys'],
    })
    renderWithProviders(<RouterProvider router={router} />, { queryClient })
    await waitFor(() => expect(router.state.location.pathname).toBe('/login'))
    expect((router.state.location.search as { redirect?: string }).redirect).toBe('/api-keys')
  })
})
