import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { server } from '@/testing/mocks/server'
import { renderHook, waitFor } from '@/testing/test-utils'
import {
  useApproveReplyDraft,
  useRegenerateReplyDraft,
  useRejectReplyDraft,
  useSendReplyDraft,
  useUpdateReplyDraft,
} from './regenerate-reply-draft'

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function wrapperFor(queryClient: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

describe('reply draft workflow API hooks', () => {
  it('regenerate invalidates feedback caches when the mutation settles', async () => {
    server.use(
      http.post('/fb/v1/console/feedback/:id/reply-draft/regenerate', () =>
        HttpResponse.json({
          replyDraft: 'fresh reply',
          replyDraftGeneratedAt: '2026-07-03T06:00:00Z',
        }),
      ),
    )
    const qc = makeQueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(() => useRegenerateReplyDraft('fb-1'), {
      wrapper: wrapperFor(qc),
    })

    result.current.mutate()
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['console', 'feedback'] })
  })

  it('update posts the edited content and invalidates feedback caches', async () => {
    let body: unknown
    server.use(
      http.post('/fb/v1/console/feedback/:id/reply-draft/edit', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({ workflow: { draftId: 'draft-1', revision: '12' } })
      }),
    )
    const qc = makeQueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(() => useUpdateReplyDraft('fb-2'), { wrapper: wrapperFor(qc) })

    result.current.mutate({ content: 'human-edited answer', expectedRevision: '11' })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(body).toEqual({ content: 'human-edited answer', expectedRevision: '11' })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['console', 'feedback'] })
  })

  it('approve and reject post the expected revision and invalidate feedback caches', async () => {
    const requests: Array<{ path: string; body: unknown }> = []
    server.use(
      http.post('/fb/v1/console/feedback/:id/reply-draft/approve', async ({ request }) => {
        requests.push({ path: new URL(request.url).pathname, body: await request.json() })
        return HttpResponse.json({ workflow: { draftId: 'draft-1', revision: '12' } })
      }),
      http.post('/fb/v1/console/feedback/:id/reply-draft/reject', async ({ request }) => {
        requests.push({ path: new URL(request.url).pathname, body: await request.json() })
        return HttpResponse.json({ workflow: { draftId: 'draft-1', revision: '13' } })
      }),
    )
    const qc = makeQueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result: approve } = renderHook(() => useApproveReplyDraft('fb-3'), {
      wrapper: wrapperFor(qc),
    })
    const { result: reject } = renderHook(() => useRejectReplyDraft('fb-3'), {
      wrapper: wrapperFor(qc),
    })

    approve.current.mutate('11')
    await waitFor(() => expect(approve.current.isSuccess).toBe(true))
    reject.current.mutate('12')
    await waitFor(() => expect(reject.current.isSuccess).toBe(true))

    expect(requests).toEqual([
      {
        path: '/fb/v1/console/feedback/fb-3/reply-draft/approve',
        body: { expectedRevision: '11' },
      },
      {
        path: '/fb/v1/console/feedback/fb-3/reply-draft/reject',
        body: { expectedRevision: '12' },
      },
    ])
    expect(invalidate).toHaveBeenCalledTimes(2)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['console', 'feedback'] })
  })

  it('send includes an idempotency key and refreshes feedback plus delivery observability', async () => {
    const key = '00000000-0000-4000-8000-000000000001'
    const randomUUID = vi.spyOn(crypto, 'randomUUID').mockReturnValue(key)
    let body: unknown
    let idempotencyKey = ''
    server.use(
      http.post('/fb/v1/console/feedback/:id/reply-draft/send', async ({ request }) => {
        body = await request.json()
        idempotencyKey = request.headers.get('Idempotency-Key') ?? ''
        return HttpResponse.json({
          workflow: { draftId: 'draft-1', revision: '12' },
          fromCache: false,
        })
      }),
    )
    const qc = makeQueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(() => useSendReplyDraft('fb-4'), { wrapper: wrapperFor(qc) })

    result.current.mutate('11')
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(randomUUID).toHaveBeenCalled()
    expect(body).toEqual({ expectedRevision: '11' })
    expect(idempotencyKey).toBe(key)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['console', 'feedback'] })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ['console', 'reply-send-hook', 'deliveries'],
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ['console', 'reply-send-hook', 'health'],
    })
    randomUUID.mockRestore()
  })

  it('send refreshes feedback and delivery observability even when the send endpoint fails', async () => {
    server.use(
      http.post('/fb/v1/console/feedback/:id/reply-draft/send', () =>
        HttpResponse.json({ code: 'DELIVERY_FAILED', message: 'hook failed' }, { status: 502 }),
      ),
    )
    const qc = makeQueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(() => useSendReplyDraft('fb-5'), { wrapper: wrapperFor(qc) })

    result.current.mutate('11')
    await waitFor(() => expect(result.current.isError).toBe(true))

    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['console', 'feedback'] })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ['console', 'reply-send-hook', 'deliveries'],
    })
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ['console', 'reply-send-hook', 'health'],
    })
  })
})
