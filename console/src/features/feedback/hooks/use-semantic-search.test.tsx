import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useSemanticSearch } from '@/features/feedback/hooks/use-semantic-search'
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

describe('useSemanticSearch', () => {
  it('posts search request and returns results', async () => {
    server.use(
      http.post('/fb/v1/console/feedback/search', () =>
        HttpResponse.json({
          results: [
            { id: 'fb-1', score: 0.95, snippet: 'matching text' },
            { id: 'fb-2', score: 0.82, snippet: 'another match' },
          ],
          total: 2,
        }),
      ),
    )

    const { result } = renderHook(() => useSemanticSearch(), {
      wrapper: createWrapper(),
    })

    result.current.mutate({ query: 'test search', limit: 10 })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data?.results).toHaveLength(2)
    expect(result.current.data?.results?.[0].id).toBe('fb-1')
  })

  it('handles error responses', async () => {
    server.use(
      http.post('/fb/v1/console/feedback/search', () =>
        HttpResponse.json({ code: 'BAD_REQUEST', message: 'Invalid query' }, { status: 400 }),
      ),
    )

    const { result } = renderHook(() => useSemanticSearch(), {
      wrapper: createWrapper(),
    })

    result.current.mutate({ query: '', limit: 10 })

    await waitFor(() => {
      expect(result.current.isError).toBe(true)
    })
  })
})
