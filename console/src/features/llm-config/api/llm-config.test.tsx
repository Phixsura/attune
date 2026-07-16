import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import {
  llmAbilitiesQuery,
  llmChannelModelsQuery,
  llmChannelsQuery,
  llmRoutesQuery,
  useCreateLLMChannel,
  useDeleteLLMAbility,
  useDeleteLLMChannel,
  useDeleteLLMRoute,
  useTestLLMChannel,
  useUpdateLLMChannel,
  useUpsertLLMAbility,
  useUpsertLLMRoute,
} from '@/features/llm-config/api/llm-config'
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

function seedLLMQueries(queryClient: QueryClient) {
  queryClient.setQueryData(['console', 'llm', 'channels'], [])
  queryClient.setQueryData(['console', 'llm', 'channels', 'ch-1', 'abilities'], [])
  queryClient.setQueryData(['console', 'llm', 'channels', 'ch-1', 'models'], [])
  queryClient.setQueryData(['console', 'llm', 'routes'], [])
}

function renderMutation<T>(hook: () => T) {
  const queryClient = makeQueryClient()
  seedLLMQueries(queryClient)
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
  return { queryClient, ...renderHook(hook, { wrapper }) }
}

describe('llm config query options', () => {
  it('maps list responses to item arrays and exposes enabled guards', async () => {
    server.use(
      http.get('/fb/v1/console/llm/channels', () =>
        HttpResponse.json({ items: [{ id: 'ch-1', name: 'OpenAI' }] }),
      ),
      http.get('/fb/v1/console/llm/channels/ch-1/abilities', () =>
        HttpResponse.json({ items: [{ id: 'ab-1', logicalModel: 'reply-draft' }] }),
      ),
      http.get('/fb/v1/console/llm/channels/ch-1/models', () =>
        HttpResponse.json({ items: [{ id: 'gpt-4.1-mini', displayName: 'GPT 4.1 mini' }] }),
      ),
      http.get('/fb/v1/console/llm/routes', () =>
        HttpResponse.json({ items: [{ id: 'route-1', purpose: 'reply_draft' }] }),
      ),
    )
    const queryClient = makeQueryClient()

    await expect(queryClient.fetchQuery(llmChannelsQuery())).resolves.toEqual([
      { id: 'ch-1', name: 'OpenAI' },
    ])
    await expect(queryClient.fetchQuery(llmAbilitiesQuery('ch-1'))).resolves.toEqual([
      { id: 'ab-1', logicalModel: 'reply-draft' },
    ])
    await expect(queryClient.fetchQuery(llmChannelModelsQuery('ch-1'))).resolves.toEqual([
      { id: 'gpt-4.1-mini', displayName: 'GPT 4.1 mini' },
    ])
    await expect(queryClient.fetchQuery(llmRoutesQuery())).resolves.toEqual([
      { id: 'route-1', purpose: 'reply_draft' },
    ])
    expect(llmAbilitiesQuery('').enabled).toBe(false)
    expect(llmChannelModelsQuery('', true).enabled).toBe(false)
    expect(llmChannelModelsQuery('ch-1', false).enabled).toBe(false)
  })
})

describe('llm channel mutations', () => {
  it('creates a channel and invalidates llm queries', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.post('/fb/v1/console/llm/channels', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toMatchObject({
          name: 'OpenAI',
          protocol: 'openai-responses',
          baseUrl: 'https://api.openai.com/v1',
          authMode: 'bearer',
          status: 'enabled',
          priority: 10,
        })
        return HttpResponse.json({ id: 'ch-1', name: 'OpenAI' })
      }),
    )
    const { queryClient, result } = renderMutation(() => useCreateLLMChannel())

    result.current.mutate({
      name: 'OpenAI',
      protocol: 'openai-responses',
      baseUrl: 'https://api.openai.com/v1',
      authMode: 'bearer',
      apiKey: 'sk-test',
      status: 'enabled',
      priority: 10,
      weight: 1,
      timeoutSeconds: 60,
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(result.current.data).toMatchObject({ id: 'ch-1' })
    expect(queryClient.getQueryState(['console', 'llm', 'channels'])?.isInvalidated).toBe(true)
    expect(queryClient.getQueryState(['console', 'llm', 'routes'])?.isInvalidated).toBe(true)
  })

  it('updates a channel and invalidates channel, ability, and model caches', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.patch('/fb/v1/console/llm/channels/ch-1', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toMatchObject({
          name: 'Primary',
          status: 'draining',
        })
        return HttpResponse.json({ id: 'ch-1', name: 'Primary', status: 'draining' })
      }),
    )
    const { queryClient, result } = renderMutation(() => useUpdateLLMChannel())

    result.current.mutate({
      id: 'ch-1',
      body: {
        name: 'Primary',
        protocol: 'openai-responses',
        baseUrl: 'https://api.openai.com/v1',
        authMode: 'bearer',
        status: 'draining',
        priority: 5,
        weight: 2,
        timeoutSeconds: 45,
      },
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(queryClient.getQueryState(['console', 'llm', 'channels'])?.isInvalidated).toBe(true)
    expect(
      queryClient.getQueryState(['console', 'llm', 'channels', 'ch-1', 'abilities'])?.isInvalidated,
    ).toBe(true)
    expect(
      queryClient.getQueryState(['console', 'llm', 'channels', 'ch-1', 'models'])?.isInvalidated,
    ).toBe(true)
  })

  it('deletes a channel and runs a channel test with settled invalidation', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.delete('/fb/v1/console/llm/channels/ch-1', ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        return new HttpResponse(null, { status: 204 })
      }),
      http.post('/fb/v1/console/llm/channels/ch-1/test', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toEqual({
          providerModel: 'gpt-4.1-mini',
          prompt: 'attune-ok',
        })
        return HttpResponse.json({ ok: true, providerModel: 'gpt-4.1-mini', text: 'ok' })
      }),
    )
    const remove = renderMutation(() => useDeleteLLMChannel())
    const testChannel = renderMutation(() => useTestLLMChannel())

    remove.result.current.mutate('ch-1')
    await waitFor(() => expect(remove.result.current.isSuccess).toBe(true))
    testChannel.result.current.mutate({
      id: 'ch-1',
      body: { providerModel: 'gpt-4.1-mini', prompt: 'attune-ok' },
    })
    await waitFor(() => expect(testChannel.result.current.isSuccess).toBe(true))

    expect(remove.queryClient.getQueryState(['console', 'llm', 'channels'])?.isInvalidated).toBe(
      true,
    )
    expect(testChannel.result.current.data).toMatchObject({ ok: true })
    expect(
      testChannel.queryClient.getQueryState(['console', 'llm', 'channels', 'ch-1', 'abilities'])
        ?.isInvalidated,
    ).toBe(true)
  })
})

describe('llm ability and route mutations', () => {
  it('upserts and deletes abilities with scoped cache invalidation', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.put('/fb/v1/console/llm/channels/ch-1/abilities', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toEqual({
          logicalModel: 'reply-draft',
          providerModel: 'gpt-4.1-mini',
          enabled: true,
          priority: 1,
          weight: 1,
        })
        return HttpResponse.json({ id: 'ab-1', channelId: 'ch-1', logicalModel: 'reply-draft' })
      }),
      http.post('/fb/v1/console/llm/channels/ch-1/abilities/delete', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toEqual({ logicalModel: 'reply-draft' })
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const upsert = renderMutation(() => useUpsertLLMAbility())
    const remove = renderMutation(() => useDeleteLLMAbility())

    upsert.result.current.mutate({
      channelId: 'ch-1',
      body: {
        logicalModel: 'reply-draft',
        providerModel: 'gpt-4.1-mini',
        enabled: true,
        priority: 1,
        weight: 1,
      },
    })
    await waitFor(() => expect(upsert.result.current.isSuccess).toBe(true))
    remove.result.current.mutate({ channelId: 'ch-1', logicalModel: 'reply-draft' })
    await waitFor(() => expect(remove.result.current.isSuccess).toBe(true))

    expect(
      upsert.queryClient.getQueryState(['console', 'llm', 'channels', 'ch-1', 'abilities'])
        ?.isInvalidated,
    ).toBe(true)
    expect(upsert.queryClient.getQueryState(['console', 'llm', 'routes'])?.isInvalidated).toBe(true)
    expect(
      remove.queryClient.getQueryState(['console', 'llm', 'channels', 'ch-1', 'abilities'])
        ?.isInvalidated,
    ).toBe(true)
    expect(remove.queryClient.getQueryState(['console', 'llm', 'routes'])?.isInvalidated).toBe(true)
  })

  it('upserts and deletes routes', async () => {
    setCsrfToken('csrf-token')
    server.use(
      http.put('/fb/v1/console/llm/routes', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toEqual({
          tenantId: '',
          purpose: 'reply_draft',
          logicalModel: 'reply-draft',
          enabled: true,
        })
        return HttpResponse.json({
          id: 'route-1',
          tenantId: '',
          purpose: 'reply_draft',
          logicalModel: 'reply-draft',
          enabled: true,
        })
      }),
      http.post('/fb/v1/console/llm/routes/delete', async ({ request }) => {
        expect(request.headers.get('x-csrf-token')).toBe('csrf-token')
        await expect(request.json()).resolves.toEqual({ tenantId: '', purpose: 'reply_draft' })
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const upsert = renderMutation(() => useUpsertLLMRoute())
    const remove = renderMutation(() => useDeleteLLMRoute())

    upsert.result.current.mutate({
      tenantId: '',
      purpose: 'reply_draft',
      logicalModel: 'reply-draft',
      enabled: true,
    })
    await waitFor(() => expect(upsert.result.current.isSuccess).toBe(true))
    remove.result.current.mutate({ tenantId: '', purpose: 'reply_draft' })
    await waitFor(() => expect(remove.result.current.isSuccess).toBe(true))

    expect(upsert.result.current.data).toMatchObject({ id: 'route-1' })
    expect(upsert.queryClient.getQueryState(['console', 'llm', 'routes'])?.isInvalidated).toBe(true)
    expect(remove.queryClient.getQueryState(['console', 'llm', 'routes'])?.isInvalidated).toBe(true)
  })
})
