import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { server } from '@/testing/mocks/server'
import {
  feedbackIdentityReviewQuery,
  feedbackIdentitySubjectQuery,
  useMergeFeedbackIdentityReview,
  useSplitFeedbackIdentityReview,
} from './get-feedback-identity-review'

function createWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, wrapper }
}

describe('feedback identity review API', () => {
  it('fetches the identity review workbench with the stable cache key', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/identity-review', () =>
        HttpResponse.json({
          candidateCount: '1',
          generatedAt: '2026-07-30T00:00:00Z',
          subjects: [],
        }),
      ),
    )
    const { queryClient } = createWrapper()

    const result = await queryClient.fetchQuery(feedbackIdentityReviewQuery())

    expect(result).toMatchObject({ candidateCount: '1' })
    expect(queryClient.getQueryData(['console', 'feedback', 'identity-review'])).toMatchObject({
      candidateCount: '1',
    })
  })

  it('fetches encoded subject details only when a subject id is present', async () => {
    let requestedPath = ''
    server.use(
      http.get('/fb/v1/console/feedback/identity-review/subjects/:subjectId', ({ request }) => {
        requestedPath = new URL(request.url).pathname
        return HttpResponse.json({
          evidence: [],
          feedback: [],
          subjectId: 'subject/a b',
        })
      }),
    )
    const { queryClient } = createWrapper()

    const emptySubjectQuery = feedbackIdentitySubjectQuery('')
    const result = await queryClient.fetchQuery(feedbackIdentitySubjectQuery('subject/a b'))

    expect(emptySubjectQuery.enabled).toBe(false)
    expect(result).toMatchObject({ subjectId: 'subject/a b' })
    expect(requestedPath).toBe('/fb/v1/console/feedback/identity-review/subjects/subject%2Fa%20b')
  })

  it('merges identity subjects and invalidates the review cache', async () => {
    let body: unknown
    server.use(
      http.post('/fb/v1/console/feedback/identity-review/merge', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({
          mergedSubjectId: 'subject-primary',
          revision: '2',
        })
      }),
    )
    const { queryClient, wrapper } = createWrapper()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(() => useMergeFeedbackIdentityReview(), { wrapper })

    result.current.mutate({
      feedbackIds: ['fb-1', 'fb-2'],
      identityKind: 'email',
      identityValue: 'ada@example.com',
      note: 'merge duplicate submitter identities',
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(body).toEqual({
      feedbackIds: ['fb-1', 'fb-2'],
      identityKind: 'email',
      identityValue: 'ada@example.com',
      note: 'merge duplicate submitter identities',
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ['console', 'feedback', 'identity-review'],
    })
  })

  it('splits identity feedback rows and invalidates the review cache', async () => {
    let body: unknown
    server.use(
      http.post('/fb/v1/console/feedback/identity-review/split', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({
          revision: '3',
          subjectId: 'subject-primary',
        })
      }),
    )
    const { queryClient, wrapper } = createWrapper()
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries')
    const { result } = renderHook(() => useSplitFeedbackIdentityReview(), { wrapper })

    result.current.mutate({
      identityKind: 'email',
      identityValue: 'ada+split@example.com',
      note: 'split secondary email',
      subjectId: 'subject-primary',
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(body).toEqual({
      identityKind: 'email',
      identityValue: 'ada+split@example.com',
      note: 'split secondary email',
      subjectId: 'subject-primary',
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ['console', 'feedback', 'identity-review'],
    })
  })
})
