import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useBatchRetryEnrichment } from '@/features/feedback/api/batch-retry-enrichment'
import { server } from '@/testing/mocks/server'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  }
}

describe('useBatchRetryEnrichment', () => {
  it('retries multiple feedback items and returns success count', async () => {
    server.use(
      http.post('/fb/v1/console/feedback/:id/retry-enrichment', ({ params }) =>
        HttpResponse.json({
          id: params.id,
          enrichmentStatus: 'pending',
          enrichmentAttempts: 0,
        }),
      ),
    )

    const { result } = renderHook(() => useBatchRetryEnrichment(), {
      wrapper: createWrapper(),
    })

    result.current.mutate(['f-1', 'f-2', 'f-3'])

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data).toEqual({
      succeeded: 3,
      failed: [],
    })
  })

  it('handles partial failures', async () => {
    server.use(
      http.post('/fb/v1/console/feedback/:id/retry-enrichment', ({ params }) => {
        if (params.id === 'f-fail') {
          return HttpResponse.json(
            { code: 'INVALID_STATE', message: 'not retryable' },
            { status: 400 },
          )
        }
        return HttpResponse.json({ id: params.id })
      }),
    )

    const { result } = renderHook(() => useBatchRetryEnrichment(), {
      wrapper: createWrapper(),
    })

    result.current.mutate(['f-1', 'f-fail', 'f-2'])

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data?.succeeded).toBe(2)
    expect(result.current.data?.failed).toHaveLength(1)
    expect(result.current.data?.failed[0].id).toBe('f-fail')
  })

  it('handles all failures', async () => {
    server.use(
      http.post('/fb/v1/console/feedback/:id/retry-enrichment', () =>
        HttpResponse.json({ code: 'SERVER_ERROR', message: 'internal error' }, { status: 500 }),
      ),
    )

    const { result } = renderHook(() => useBatchRetryEnrichment(), {
      wrapper: createWrapper(),
    })

    result.current.mutate(['f-1', 'f-2'])

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data?.succeeded).toBe(0)
    expect(result.current.data?.failed).toHaveLength(2)
  })

  it('handles empty batch', async () => {
    const { result } = renderHook(() => useBatchRetryEnrichment(), {
      wrapper: createWrapper(),
    })

    result.current.mutate([])

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data).toEqual({
      succeeded: 0,
      failed: [],
    })
  })
})
