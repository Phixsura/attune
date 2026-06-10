import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import {
  type GuardPolicy,
  guardPoliciesQuery,
  useCreateGuardPolicy,
  useDeleteGuardPolicy,
  usePatchGuardPolicy,
  useResolveGuardPolicy,
  useUpdateGuardPolicies,
} from '@/features/guard-policies/api/guard-policies'
import { server } from '@/testing/mocks/server'
import { renderHook, waitFor } from '@/testing/test-utils'

const policyFixture: GuardPolicy = {
  id: 'gp-1',
  name: 'tenant pii',
  kind: 'default',
  enabled: true,
  priority: 100,
  scope: 'tenant',
  target: {
    channels: ['api'],
    sourceIds: ['source-1'],
    sourceTags: ['regulated'],
    purposes: ['enrich'],
    stages: ['llm_input'],
    environments: ['prod'],
  },
  rules: [
    {
      guard: 'pii',
      stage: 'llm_input',
      entities: ['email'],
      action: 'redact',
      replacement: '<PII:{entity}>',
    },
  ],
}

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

describe('guard policy API hooks', () => {
  it('guardPoliciesQuery unwraps response items', async () => {
    server.use(
      http.get('/fb/v1/console/guard-policies', () =>
        HttpResponse.json({ items: [policyFixture] }),
      ),
    )
    const qc = makeQueryClient()
    await expect(qc.fetchQuery(guardPoliciesQuery())).resolves.toEqual([policyFixture])
  })

  it('resolve mutation posts the preview request body and returns effective rules', async () => {
    let observed: unknown
    server.use(
      http.post('/fb/v1/console/guard-policies/effective', async ({ request }) => {
        observed = await request.json()
        return HttpResponse.json({
          rules: policyFixture.rules,
        })
      }),
    )
    const qc = makeQueryClient()
    const { result } = renderHook(() => useResolveGuardPolicy(), {
      wrapper: wrapperFor(qc),
    })

    result.current.mutate({
      channel: 'api',
      sourceId: 'source-1',
      sourceTags: ['regulated'],
      purpose: 'enrich',
      environment: 'prod',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(observed).toEqual({
      channel: 'api',
      sourceId: 'source-1',
      sourceTags: ['regulated'],
      purpose: 'enrich',
      environment: 'prod',
    })
    expect(result.current.data?.rules).toEqual(policyFixture.rules)
  })

  it('create, patch, delete, and bulk update invalidate the guard policy list', async () => {
    const requests: Array<{ method: string; path: string; body?: unknown }> = []
    server.use(
      http.post('/fb/v1/console/guard-policies', async ({ request }) => {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
        })
        return HttpResponse.json(policyFixture)
      }),
      http.patch('/fb/v1/console/guard-policies/:id', async ({ params, request }) => {
        requests.push({
          method: request.method,
          path: `/fb/v1/console/guard-policies/${params.id}`,
          body: await request.json(),
        })
        return HttpResponse.json({ ...policyFixture, id: params.id })
      }),
      http.delete('/fb/v1/console/guard-policies/:id', ({ params, request }) => {
        requests.push({
          method: request.method,
          path: `/fb/v1/console/guard-policies/${params.id}`,
        })
        return HttpResponse.json({ id: params.id })
      }),
      http.put('/fb/v1/console/guard-policies', async ({ request }) => {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
        })
        return HttpResponse.json({ items: [policyFixture] })
      }),
    )
    const qc = makeQueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result: create } = renderHook(() => useCreateGuardPolicy(), {
      wrapper: wrapperFor(qc),
    })
    const { result: patch } = renderHook(() => usePatchGuardPolicy(), {
      wrapper: wrapperFor(qc),
    })
    const { result: del } = renderHook(() => useDeleteGuardPolicy(), {
      wrapper: wrapperFor(qc),
    })
    const { result: update } = renderHook(() => useUpdateGuardPolicies(), {
      wrapper: wrapperFor(qc),
    })

    create.current.mutate(policyFixture)
    await waitFor(() => expect(create.current.isSuccess).toBe(true))
    patch.current.mutate({ id: 'gp-2', policy: { ...policyFixture, name: 'patched' } })
    await waitFor(() => expect(patch.current.isSuccess).toBe(true))
    del.current.mutate('gp-3')
    await waitFor(() => expect(del.current.isSuccess).toBe(true))
    update.current.mutate([policyFixture])
    await waitFor(() => expect(update.current.isSuccess).toBe(true))

    expect(requests).toEqual([
      {
        method: 'POST',
        path: '/fb/v1/console/guard-policies',
        body: { policy: policyFixture },
      },
      {
        method: 'PATCH',
        path: '/fb/v1/console/guard-policies/gp-2',
        body: { policy: { ...policyFixture, name: 'patched' } },
      },
      { method: 'DELETE', path: '/fb/v1/console/guard-policies/gp-3' },
      {
        method: 'PUT',
        path: '/fb/v1/console/guard-policies',
        body: { policies: [policyFixture] },
      },
    ])
    expect(invalidate).toHaveBeenCalledTimes(4)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['console', 'guard-policies'] })
  })
})
