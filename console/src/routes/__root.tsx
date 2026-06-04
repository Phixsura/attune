import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { createRootRouteWithContext, Outlet } from '@tanstack/react-router'
import { TanStackRouterDevtools } from '@tanstack/react-router-devtools'
import { lazy, Suspense } from 'react'
import { Toaster } from 'sonner'

// QueryClient is owned by the root so loaders + components share one cache.
// Keep config minimal until we hit a real edge case (CLAUDE.md 律 6).
const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Refetch on tab focus is great for an admin console where data may
      // change in another browser tab or via the API itself.
      refetchOnWindowFocus: true,
      // Network errors retry once; 401/403/404 don't (handled per-query).
      retry: 1,
      staleTime: 30_000,
    },
  },
})

interface RouterContext {
  queryClient: QueryClient
}

// Devtools only loaded outside of production builds. Lazy import keeps the
// production bundle clean.
const DevtoolsBundle = import.meta.env.PROD
  ? () => null
  : lazy(() =>
      Promise.resolve({
        default: () => (
          <>
            <TanStackRouterDevtools position="bottom-left" />
            <ReactQueryDevtools buttonPosition="bottom-right" />
          </>
        ),
      }),
    )

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootComponent,
})

function RootComponent() {
  return (
    <QueryClientProvider client={queryClient}>
      <Outlet />
      <Toaster richColors closeButton position="top-right" />
      <Suspense>
        <DevtoolsBundle />
      </Suspense>
    </QueryClientProvider>
  )
}

export { queryClient }
