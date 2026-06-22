import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { ReactQueryDevtools } from '@tanstack/react-query-devtools'
import { createRootRouteWithContext, Outlet } from '@tanstack/react-router'
import { TanStackRouterDevtools } from '@tanstack/react-router-devtools'
import { ThemeProvider } from 'next-themes'
import { lazy, Suspense } from 'react'
import { Toaster } from 'sonner'

// QueryClient is owned by the root so loaders + components share one cache.
// Keep the config minimal until we hit a real edge case.
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

// Devtools stay opt-in even in dev so browser smoke tests and demos show the
// product surface, not framework panels.
const DevtoolsBundle =
  import.meta.env.VITE_ATTUNE_DEVTOOLS === 'true'
    ? lazy(() =>
        Promise.resolve({
          default: () => (
            <>
              <TanStackRouterDevtools position="bottom-left" />
              <ReactQueryDevtools buttonPosition="bottom-right" />
            </>
          ),
        }),
      )
    : () => null

export const Route = createRootRouteWithContext<RouterContext>()({
  component: RootComponent,
})

function RootComponent() {
  return (
    <QueryClientProvider client={queryClient}>
      <ThemeProvider attribute="class" defaultTheme="system" enableSystem disableTransitionOnChange>
        <Outlet />
        <Toaster richColors closeButton position="top-right" />
        <Suspense>
          <DevtoolsBundle />
        </Suspense>
      </ThemeProvider>
    </QueryClientProvider>
  )
}

export { queryClient }
