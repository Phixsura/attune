import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useCreateInboundSource } from '@/features/inbound-sources/api/create-inbound-source'
import { useDeleteInboundSource } from '@/features/inbound-sources/api/delete-inbound-source'
import { useDiscoverSlackChannels } from '@/features/inbound-sources/api/discover-slack-channels'
import { inboundSourceQuery } from '@/features/inbound-sources/api/get-inbound-source'
import { inboundSourcesQuery } from '@/features/inbound-sources/api/list-inbound-sources'
import { usePauseInboundSource } from '@/features/inbound-sources/api/pause-inbound-source'
import { useResumeInboundSource } from '@/features/inbound-sources/api/resume-inbound-source'
import { useRotateInboundSource } from '@/features/inbound-sources/api/rotate-inbound-source'
import { useSyncNow } from '@/features/inbound-sources/api/sync-now'
import { useTestInboundSourceConnection } from '@/features/inbound-sources/api/test-connection'
import { useUpdateInboundSource } from '@/features/inbound-sources/api/update-inbound-source'
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
  queryClient.setQueryData(['console', 'inbound-sources'], [])
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, ...renderHook(hook, { wrapper }) }
}

describe('inbound source API hooks', () => {
  it('lists inbound source rows and fetches one detail row', async () => {
    server.use(
      http.get('/fb/v1/console/inbound/sources', () =>
        HttpResponse.json({ items: [{ id: 'src-1', name: 'Webhook' }] }),
      ),
      http.get('/fb/v1/console/inbound/sources/src-1', () =>
        HttpResponse.json({ id: 'src-1', name: 'Webhook', enabled: true }),
      ),
    )
    const queryClient = makeQueryClient()

    await expect(queryClient.fetchQuery(inboundSourcesQuery())).resolves.toEqual([
      { id: 'src-1', name: 'Webhook' },
    ])
    await expect(queryClient.fetchQuery(inboundSourceQuery('src-1'))).resolves.toMatchObject({
      id: 'src-1',
      enabled: true,
    })
  })

  it('creates a source and invalidates the source list', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post('/fb/v1/console/inbound/sources', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toMatchObject({
          name: 'Webhook',
          channel: 'webhook',
          webhookConfig: {},
        })
        return HttpResponse.json({
          source: { id: 'src-1', name: 'Webhook' },
          webhookSecretReveal: { secretHex: 'secret-once' },
        })
      }),
    )
    const { queryClient, result } = renderMutation(() => useCreateInboundSource())

    result.current.mutate({ name: 'Webhook', channel: 'webhook', webhookConfig: {} })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.source?.id).toBe('src-1')
    expect(queryClient.getQueryState(['console', 'inbound-sources'])?.isInvalidated).toBe(true)
  })

  it('PATCHes settings in place and invalidates the source list', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.patch('/fb/v1/console/inbound/sources/src-1', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toMatchObject({
          id: 'src-1',
          name: 'Renamed',
          intercomConfig: { accessToken: '', filterTags: ['bug'] },
        })
        return HttpResponse.json({ id: 'src-1', name: 'Renamed' })
      }),
    )
    const { queryClient, result } = renderMutation(() => useUpdateInboundSource())

    result.current.mutate({
      id: 'src-1',
      name: 'Renamed',
      intercomConfig: {
        region: '',
        accessToken: '',
        filterTags: ['bug'],
        filterExcludeTags: [],
        filterStates: [],
      },
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data?.name).toBe('Renamed')
    expect(queryClient.getQueryState(['console', 'inbound-sources'])?.isInvalidated).toBe(true)
  })

  it('pauses, resumes, rotates, and deletes by source id', async () => {
    setCsrfToken('csrf-token')
    const calls: string[] = []
    server.use(
      http.post('/fb/v1/console/inbound/sources/src-1/pause', ({ request }) => {
        calls.push(new URL(request.url).pathname)
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        return HttpResponse.json({ id: 'src-1', enabled: false })
      }),
      http.post('/fb/v1/console/inbound/sources/src-1/resume', ({ request }) => {
        calls.push(new URL(request.url).pathname)
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        return HttpResponse.json({ id: 'src-1', enabled: true })
      }),
      http.post('/fb/v1/console/inbound/sources/src-1/rotate-secret', ({ request }) => {
        calls.push(new URL(request.url).pathname)
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        return HttpResponse.json({ secretHex: 'rotated-secret' })
      }),
      http.delete('/fb/v1/console/inbound/sources/src-1', ({ request }) => {
        calls.push(new URL(request.url).pathname)
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const pause = renderMutation(() => usePauseInboundSource())
    const resume = renderMutation(() => useResumeInboundSource())
    const rotate = renderMutation(() => useRotateInboundSource())
    const remove = renderMutation(() => useDeleteInboundSource())

    pause.result.current.mutate('src-1')
    await waitFor(() => expect(pause.result.current.isSuccess).toBe(true))
    resume.result.current.mutate('src-1')
    await waitFor(() => expect(resume.result.current.isSuccess).toBe(true))
    rotate.result.current.mutate('src-1')
    await waitFor(() => expect(rotate.result.current.isSuccess).toBe(true))
    remove.result.current.mutate('src-1')
    await waitFor(() => expect(remove.result.current.isSuccess).toBe(true))

    expect(calls).toEqual([
      '/fb/v1/console/inbound/sources/src-1/pause',
      '/fb/v1/console/inbound/sources/src-1/resume',
      '/fb/v1/console/inbound/sources/src-1/rotate-secret',
      '/fb/v1/console/inbound/sources/src-1',
    ])
    expect(pause.queryClient.getQueryState(['console', 'inbound-sources'])?.isInvalidated).toBe(
      true,
    )
    expect(resume.queryClient.getQueryState(['console', 'inbound-sources'])?.isInvalidated).toBe(
      true,
    )
    expect(rotate.result.current.data).toMatchObject({ secretHex: 'rotated-secret' })
    expect(rotate.queryClient.getQueryState(['console', 'inbound-sources'])?.isInvalidated).toBe(
      true,
    )
    expect(remove.queryClient.getQueryState(['console', 'inbound-sources'])?.isInvalidated).toBe(
      true,
    )
  })

  it('sync-now POSTs by source id and invalidates the source list', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post('/fb/v1/console/inbound/sources/src-1/sync-now', ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        return HttpResponse.json({ id: 'src-1' })
      }),
    )
    const syncNow = renderMutation(() => useSyncNow())

    syncNow.result.current.mutate('src-1')
    await waitFor(() => expect(syncNow.result.current.isSuccess).toBe(true))

    expect(syncNow.result.current.data).toEqual({ id: 'src-1' })
    expect(syncNow.queryClient.getQueryState(['console', 'inbound-sources'])?.isInvalidated).toBe(
      true,
    )
  })

  it('discovers Slack channels and tests a draft connection', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post('/fb/v1/console/inbound/sources/slack/discover', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toEqual({
          slackConfig: { botToken: 'xoxb-token', channelId: '' },
        })
        return HttpResponse.json({ channels: [{ id: 'C1', name: 'support' }] })
      }),
      http.post('/fb/v1/console/inbound/sources/test-connection', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toMatchObject({ channel: 'slack' })
        return HttpResponse.json({ ok: true, message: 'connected' })
      }),
    )
    const discover = renderMutation(() => useDiscoverSlackChannels())
    const testConnection = renderMutation(() => useTestInboundSourceConnection())

    discover.result.current.mutate({ slackConfig: { botToken: 'xoxb-token', channelId: '' } })
    await waitFor(() => expect(discover.result.current.isSuccess).toBe(true))
    testConnection.result.current.mutate({
      channel: 'slack',
      slackConfig: { botToken: 'xoxb-token', channelId: 'C1' },
    })
    await waitFor(() => expect(testConnection.result.current.isSuccess).toBe(true))

    expect(discover.result.current.data?.channels?.[0]?.name).toBe('support')
    expect(testConnection.result.current.data).toMatchObject({ ok: true })
  })
})
