import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it } from 'vitest'
import { useArchiveTag } from '@/features/tags/api/archive-tag'
import { useCreateTag } from '@/features/tags/api/create-tag'
import { tagsQueryKey } from '@/features/tags/api/list-tags'
import { useUpdateTag } from '@/features/tags/api/update-tag'
import { setCsrfToken } from '@/lib/api-client'
import { server } from '@/testing/mocks/server'
import { renderHook, waitFor } from '@/testing/test-utils'

function renderMutation<T>(hook: () => T) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
  queryClient.setQueryData(tagsQueryKey, [{ id: 'tag-1', name: 'Existing' }])
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, ...renderHook(hook, { wrapper }) }
}

describe('tag mutation hooks', () => {
  beforeEach(() => setCsrfToken('csrf-token'))

  it('creates a tag and invalidates the tag list', async () => {
    server.use(
      http.post('/fb/v1/console/tags', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toMatchObject({ name: 'Escalation' })
        return HttpResponse.json({ id: 'tag-2', name: 'Escalation' })
      }),
    )
    const { queryClient, result } = renderMutation(() => useCreateTag())

    result.current.mutate({ name: 'Escalation' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data).toMatchObject({ id: 'tag-2' })
    expect(queryClient.getQueryState(tagsQueryKey)?.isInvalidated).toBe(true)
  })

  it('updates a tag and invalidates the tag list', async () => {
    server.use(
      http.patch('/fb/v1/console/tags/tag-1', async ({ request }) => {
        await expect(request.json()).resolves.toMatchObject({ id: 'tag-1', name: 'VIP' })
        return HttpResponse.json({ id: 'tag-1', name: 'VIP' })
      }),
    )
    const { queryClient, result } = renderMutation(() => useUpdateTag())

    result.current.mutate({ id: 'tag-1', name: 'VIP' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data).toMatchObject({ name: 'VIP' })
    expect(queryClient.getQueryState(tagsQueryKey)?.isInvalidated).toBe(true)
  })

  it('archives a tag and invalidates the tag list', async () => {
    server.use(
      http.delete('/fb/v1/console/tags/tag-1', () => new HttpResponse(null, { status: 204 })),
    )
    const { queryClient, result } = renderMutation(() => useArchiveTag())

    result.current.mutate('tag-1')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(queryClient.getQueryState(tagsQueryKey)?.isInvalidated).toBe(true)
  })
})
