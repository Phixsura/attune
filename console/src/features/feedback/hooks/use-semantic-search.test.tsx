import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import {
  useRecordSearchEvent,
  useSemanticSearch,
} from '@/features/feedback/hooks/use-semantic-search'
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
          hits: [
            {
              feedback: { id: 'fb-1' },
              similarity: 0.95,
              keywordScore: 0.4,
              matchType: 'hybrid',
              semanticRank: 1,
              lexicalRank: 2,
              fusedScore: 0.017,
              evidence: [],
              rankingSignals: ['semantic', 'lexical', 'rrf'],
            },
            {
              feedback: { id: 'fb-2' },
              similarity: 0.82,
              keywordScore: 0,
              matchType: 'semantic',
              semanticRank: 2,
              lexicalRank: 0,
              fusedScore: 0.011,
              evidence: [],
              rankingSignals: ['semantic', 'rrf'],
            },
          ],
          embeddingModel: 'text-embedding-3-small',
          totalWithEmbeddings: 100,
          usedKeywordFallback: false,
          rankingVersion: 'rrf.pgfts.v1.k60',
          coverage: {
            totalLiveFeedback: 120,
            totalWithEmbeddings: 100,
            embeddingModel: 'text-embedding-3-small',
          },
        }),
      ),
    )

    const { result } = renderHook(() => useSemanticSearch(), {
      wrapper: createWrapper(),
    })

    result.current.mutate({ q: 'test search', limit: 10 })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(result.current.data?.hits).toHaveLength(2)
    expect(result.current.data?.hits?.[0].feedback?.id).toBe('fb-1')
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

    result.current.mutate({ q: '', limit: 10 })

    await waitFor(() => {
      expect(result.current.isError).toBe(true)
    })
  })

  it('posts search result interaction events', async () => {
    let capturedBody: unknown
    server.use(
      http.post('/fb/v1/console/feedback/search/events', async ({ request }) => {
        capturedBody = await request.json()
        return HttpResponse.json({ recorded: true })
      }),
    )

    const { result } = renderHook(() => useRecordSearchEvent(), {
      wrapper: createWrapper(),
    })

    result.current.mutate({
      feedbackId: 'fb-1',
      action: 'open_result',
      matchType: 'semantic',
      rank: 1,
      runId: 'search-1',
    })

    await waitFor(() => {
      expect(result.current.isSuccess).toBe(true)
    })

    expect(capturedBody).toEqual({
      feedbackId: 'fb-1',
      action: 'open_result',
      matchType: 'semantic',
      rank: 1,
      runId: 'search-1',
    })
  })
})
