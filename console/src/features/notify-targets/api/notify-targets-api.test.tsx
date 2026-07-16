import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useCreateNotifyTarget } from '@/features/notify-targets/api/create-notify-target'
import { useDeleteNotifyTarget } from '@/features/notify-targets/api/delete-notify-target'
import { notifyTargetsQuery } from '@/features/notify-targets/api/list-notify-targets'
import { useTestNotifyTarget } from '@/features/notify-targets/api/test-notify-target'
import { useUpdateNotifyTarget } from '@/features/notify-targets/api/update-notify-target'
import { setCsrfToken } from '@/lib/api-client'
import { server } from '@/testing/mocks/server'

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function renderMutation<T>(hook: () => T) {
  const queryClient = makeQueryClient()
  queryClient.setQueryData(['console', 'notify-targets'], [])
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, ...renderHook(hook, { wrapper }) }
}

describe('notify target API hooks', () => {
  it('lists target rows from the response items array', async () => {
    server.use(
      http.get('/fb/v1/console/notify-targets', ({ request }) => {
        expect(new URL(request.url).search).toBe('')
        return HttpResponse.json({ items: [{ id: 'target-1', name: 'Pager' }] })
      }),
    )

    await expect(makeQueryClient().fetchQuery(notifyTargetsQuery())).resolves.toEqual([
      { id: 'target-1', name: 'Pager' },
    ])
  })

  it('creates a target and invalidates the target list', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post('/fb/v1/console/notify-targets', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toMatchObject({
          destinationType: 'raw-webhook',
          audience: 'all',
        })
        return HttpResponse.json({ id: 'target-1', name: 'Escalation' })
      }),
    )
    const { queryClient, result } = renderMutation(() => useCreateNotifyTarget())

    result.current.mutate({
      destinationType: 'raw-webhook',
      audience: 'all',
      url: 'https://hooks.test/x',
      timeoutSeconds: 10,
      disabled: false,
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data).toMatchObject({ id: 'target-1' })
    expect(queryClient.getQueryState(['console', 'notify-targets'])?.isInvalidated).toBe(true)
  })

  it('updates a target with a sparse patch and invalidates the target list', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.patch('/fb/v1/console/notify-targets/target-1', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toEqual({ disabled: true })
        return HttpResponse.json({ id: 'target-1', disabled: true })
      }),
    )
    const { queryClient, result } = renderMutation(() => useUpdateNotifyTarget())

    result.current.mutate({ id: 'target-1', patch: { disabled: true } })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data).toMatchObject({ disabled: true })
    expect(queryClient.getQueryState(['console', 'notify-targets'])?.isInvalidated).toBe(true)
  })

  it('deletes a target and invalidates the target list', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.delete('/fb/v1/console/notify-targets/target-1', ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const { queryClient, result } = renderMutation(() => useDeleteNotifyTarget())

    result.current.mutate('target-1')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(queryClient.getQueryState(['console', 'notify-targets'])?.isInvalidated).toBe(true)
  })

  it('runs a target test request without invalidating the list', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post('/fb/v1/console/notify-targets/target-1/test', ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        return HttpResponse.json({ ok: true, statusCode: 202 })
      }),
    )
    const { queryClient, result } = renderMutation(() => useTestNotifyTarget())

    result.current.mutate('target-1')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data).toMatchObject({ ok: true, statusCode: 202 })
    expect(queryClient.getQueryState(['console', 'notify-targets'])?.isInvalidated).toBe(false)
  })
})
