import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { terminalFailureWorkbenchQueryKey } from '@/features/feedback/api/get-terminal-failure-workbench'
import { useRetryEnrichment } from '@/features/feedback/api/retry-enrichment'
import { server } from '@/testing/mocks/server'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, wrapper }
}

describe('useRetryEnrichment', () => {
  it('posts to the retry endpoint and invalidates queries on success', async () => {
    let requestReceived = false
    server.use(
      http.post('/fb/v1/console/feedback/:id/retry-enrichment', ({ params }) => {
        requestReceived = true
        return HttpResponse.json({
          id: params.id,
          enrichmentStatus: 'pending',
          enrichmentAttempts: 0,
        })
      }),
    )

    const { queryClient, wrapper } = createWrapper()
    queryClient.setQueryData(terminalFailureWorkbenchQueryKey, {
      periodStart: '2026-06-01T00:00:00Z',
      periodEnd: '2026-06-30T00:00:00Z',
      totalTerminalFailures: '1',
      oldestCreatedAt: '2026-06-01T00:00:00Z',
      reasonClassClusters: [],
      modelChannelClusters: [],
      configFingerprintClusters: [],
      ageBucketClusters: [],
    })

    const { result } = renderHook(() => useRetryEnrichment('f-123'), { wrapper })

    result.current.mutate()

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(requestReceived).toBe(true)
    expect(queryClient.getQueryState(terminalFailureWorkbenchQueryKey)?.isInvalidated).toBe(true)
  })

  it('handles error responses', async () => {
    server.use(
      http.post('/fb/v1/console/feedback/:id/retry-enrichment', () =>
        HttpResponse.json(
          { code: 'INVALID_STATE', message: 'not in terminal state' },
          { status: 400 },
        ),
      ),
    )

    const { wrapper } = createWrapper()
    const { result } = renderHook(() => useRetryEnrichment('f-456'), { wrapper })

    result.current.mutate()

    await waitFor(() => {
      expect(result.current.isError).toBe(true)
    })
  })
})
