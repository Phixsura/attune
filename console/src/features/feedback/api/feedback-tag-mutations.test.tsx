import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useAddFeedbackTag } from '@/features/feedback/api/add-feedback-tag'
import { useBatchUpdateTags } from '@/features/feedback/api/batch-update-tags'
import { useRemoveFeedbackTag } from '@/features/feedback/api/remove-feedback-tag'
import { setCsrfToken } from '@/lib/api-client'
import { server } from '@/testing/mocks/server'

function renderMutation<T>(hook: () => T) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  queryClient.setQueryData(['console', 'feedback'], { items: [] })
  queryClient.setQueryData(['console', 'tags'], { tags: [] })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, ...renderHook(hook, { wrapper }) }
}

describe('feedback tag mutation hooks', () => {
  it('adds a tag to feedback and invalidates feedback and tag caches', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post('/fb/v1/console/feedback/fb-1/tags', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toEqual({
          feedbackId: 'fb-1',
          tagId: 'tag-1',
        })
        return HttpResponse.json({
          tag: { id: 'tag-1', name: 'Escalation', color: '#ef4444' },
        })
      }),
    )
    const { queryClient, result } = renderMutation(() => useAddFeedbackTag('fb-1'))

    result.current.mutate({ tagId: 'tag-1' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.tag?.id).toBe('tag-1')
    expect(queryClient.getQueryState(['console', 'feedback'])?.isInvalidated).toBe(true)
    expect(queryClient.getQueryState(['console', 'tags'])?.isInvalidated).toBe(true)
  })

  it('removes a tag from feedback and invalidates feedback and tag caches', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.delete('/fb/v1/console/feedback/fb-1/tags/tag-1', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.text()).resolves.toBe('')
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const { queryClient, result } = renderMutation(() => useRemoveFeedbackTag('fb-1'))

    result.current.mutate('tag-1')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(queryClient.getQueryState(['console', 'feedback'])?.isInvalidated).toBe(true)
    expect(queryClient.getQueryState(['console', 'tags'])?.isInvalidated).toBe(true)
  })

  it('batch updates feedback tags through the shared batch endpoint', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post('/fb/v1/console/feedback/batch', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toEqual({
          feedbackIds: ['101', '102'],
          dryRun: false,
          operation: {
            tag: {
              addTagIds: ['tag-add'],
              removeTagIds: ['tag-remove'],
            },
          },
        })
        return HttpResponse.json({ succeeded: 2, failed: [] })
      }),
    )
    const { queryClient, result } = renderMutation(() => useBatchUpdateTags())

    result.current.mutate({
      feedbackIds: ['101', '102'],
      addTagIds: ['tag-add'],
      removeTagIds: ['tag-remove'],
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data).toEqual({ affected: 2 })
    expect(queryClient.getQueryState(['console', 'feedback'])?.isInvalidated).toBe(true)
    expect(queryClient.getQueryState(['console', 'tags'])?.isInvalidated).toBe(true)
  })
})
