import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { createElement, type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useCreateApiKey } from '@/features/api-keys/api/create-api-key'
import { server } from '@/testing/mocks/server'

// The hook lives in the api/ layer and needs only a QueryClientProvider.
// Each test gets a fresh client (TkDodo's "no cache leak between tests").
// createElement instead of JSX keeps this file .ts; the dialogs.test.tsx
// next door handles the rendering.
function wrap() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children)
  return { qc, wrapper }
}

describe('useCreateApiKey', () => {
  it('POSTs to /api-keys with {label} and returns the issued key', async () => {
    let observedBody: unknown
    server.use(
      http.post('/fb/v1/console/api-keys', async ({ request }) => {
        observedBody = await request.json()
        return HttpResponse.json({
          key: {
            id: 'k-42',
            label: (observedBody as { label: string }).label,
            keyPrefix: 'sk_t_',
            createdAt: '2026-06-07T00:00:00Z',
            lastUsedAt: '',
            isActive: true,
            scopes: [],
            allowedCidrs: [],
            usageCount: '0',
            environment: '',
          },
          secret: 'sk_t_supersecret',
        })
      }),
    )
    const { qc, wrapper } = wrap()
    const { result } = renderHook(() => useCreateApiKey(), { wrapper })
    result.current.mutate({ label: 'ci/automation' })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(observedBody).toEqual({ label: 'ci/automation' })
    expect(result.current.data?.key?.id).toBe('k-42')
    expect(result.current.data?.secret).toBe('sk_t_supersecret')
    qc.clear()
  })

  it('onSuccess invalidates ["console","api-keys"] so the list refetches', async () => {
    server.use(
      http.post('/fb/v1/console/api-keys', () =>
        HttpResponse.json({
          id: 'k-x',
          label: 'x',
          keyPrefix: 'sk_',
          secret: 'sk_s',
          createdAt: 'now',
        }),
      ),
    )
    const { qc, wrapper } = wrap()
    qc.setQueryData(['console', 'api-keys'], [{ id: 'old', label: 'old' }])
    const { result } = renderHook(() => useCreateApiKey(), { wrapper })
    result.current.mutate({ label: 'new' })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    // Invalidation marks the cached query stale — the next observer
    // refetches. With no active observer, isInvalidated stays true.
    const state = qc.getQueryState(['console', 'api-keys'])
    expect(state?.isInvalidated).toBe(true)
    qc.clear()
  })
})
