import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { type RenderOptions, type RenderResult, render } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactElement } from 'react'
import { I18nextProvider } from 'react-i18next'
import { TooltipProvider } from '@/components/ui/tooltip'
import i18n from '@/i18n'

// renderWithProviders wraps the unit under test with the providers it
// would see in the real app: i18n + a fresh QueryClient. retry: false
// is non-negotiable (an error-path test would otherwise wait ~3s for
// the default retry storm), and a new client per test prevents cache
// state from leaking across tests (TkDodo's first rule).
export function renderWithProviders(
  ui: ReactElement,
  opts?: { queryClient?: QueryClient; renderOptions?: Omit<RenderOptions, 'wrapper'> },
): RenderResult & { queryClient: QueryClient; user: ReturnType<typeof userEvent.setup> } {
  const queryClient =
    opts?.queryClient ??
    new QueryClient({
      defaultOptions: {
        queries: { retry: false, gcTime: Number.POSITIVE_INFINITY },
        mutations: { retry: false },
      },
    })
  return {
    queryClient,
    user: userEvent.setup(),
    ...render(ui, {
      wrapper: ({ children }) => (
        <I18nextProvider i18n={i18n}>
          <QueryClientProvider client={queryClient}>
            <TooltipProvider>{children}</TooltipProvider>
          </QueryClientProvider>
        </I18nextProvider>
      ),
      ...opts?.renderOptions,
    }),
  }
}

// Convenience re-exports so a test file imports everything from one place.
export * from '@testing-library/react'
export { default as userEvent } from '@testing-library/user-event'
