import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useBatchDeleteFeedback } from '@/features/feedback/api/batch-delete'
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

describe('useBatchDeleteFeedback', () => {
  it('posts to the batch endpoint with delete operation', async () => {
    let requestBody: string | null = null
    server.use(
      http.post('/fb/v1/console/feedback/batch', async ({ request }) => {
        requestBody = await request.text()
        return HttpResponse.json({ affected_count: 3 })
      }),
    )

    const { result } = renderHook(() => useBatchDeleteFeedback(), {
      wrapper: createWrapper(),
    })

    result.current.mutate(['1', '2', '3'])

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    const parsed = JSON.parse(requestBody ?? '{}')
    expect(parsed.feedback_ids).toEqual([1, 2, 3])
    expect(parsed.operation).toEqual({ delete: {} })
    expect(result.current.data).toEqual({
      succeeded: 3,
      failed: [],
    })
  })

  it('handles error responses', async () => {
    server.use(
      http.post('/fb/v1/console/feedback/batch', () =>
        HttpResponse.json({ code: 'FORBIDDEN', message: 'not authorized' }, { status: 403 }),
      ),
    )

    const { result } = renderHook(() => useBatchDeleteFeedback(), {
      wrapper: createWrapper(),
    })

    result.current.mutate(['1'])

    await waitFor(() => {
      expect(result.current.isError).toBe(true)
    })
  })
})
