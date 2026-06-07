import { QueryClient } from '@tanstack/react-query'
import { type AnyRoute, createMemoryHistory, createRouter } from '@tanstack/react-router'

// Test helper for TanStack Router route-guard tests. Default
// pending/load timings are zeroed so awaitable behavior shows up
// immediately, and the memory history is seeded from initialEntries.
// The test file builds its own route tree (root + leaves) and passes
// it via routeTree — keeping each test focused on the guard it
// exercises, rather than coupling to the production route table.
//
// Used by routes/_authed.test.tsx to verify beforeLoad redirects to
// /login on 401 from meQuery; could be reused by any future route
// loader/guard tests.
export function makeTestRouter(opts: {
  routeTree: AnyRoute
  initialEntries?: string[]
  queryClient?: QueryClient
}) {
  const queryClient =
    opts.queryClient ??
    new QueryClient({
      defaultOptions: {
        queries: { retry: false, gcTime: Number.POSITIVE_INFINITY },
        mutations: { retry: false },
      },
    })
  const router = createRouter({
    routeTree: opts.routeTree,
    context: { queryClient },
    history: createMemoryHistory({ initialEntries: opts.initialEntries ?? ['/'] }),
    defaultPendingMinMs: 0,
    defaultPendingMs: 0,
  })
  return { router, queryClient }
}
