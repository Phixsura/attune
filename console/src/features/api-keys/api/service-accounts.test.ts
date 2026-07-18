import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { createElement, type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { useCreateServiceAccount } from '@/features/api-keys/api/create-service-account'
import { useDeleteServiceAccount } from '@/features/api-keys/api/delete-service-account'
import {
  type ServiceAccount,
  serviceAccountsQueryKey,
} from '@/features/api-keys/api/list-service-accounts'
import { useUpdateServiceAccount } from '@/features/api-keys/api/update-service-account'
import { server } from '@/testing/mocks/server'

function wrap() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children)
  return { qc, wrapper }
}

function serviceAccount(id: string, name: string, isActive = true): ServiceAccount {
  return {
    id,
    name,
    description: `${name} description`,
    isActive,
    createdAt: '2026-01-01T00:00:00Z',
    updatedAt: '2026-01-01T00:00:00Z',
  }
}

describe('service account mutations', () => {
  it('creates a service account and keeps cached accounts sorted by name', async () => {
    let observedBody: unknown
    server.use(
      http.post('/fb/v1/console/service-accounts', async ({ request }) => {
        observedBody = await request.json()
        return HttpResponse.json({
          serviceAccount: serviceAccount('sa-2', 'Alpha'),
        })
      }),
    )
    const { qc, wrapper } = wrap()
    qc.setQueryData<ServiceAccount[]>(serviceAccountsQueryKey, [serviceAccount('sa-1', 'Zulu')])

    const { result } = renderHook(() => useCreateServiceAccount(), { wrapper })
    result.current.mutate({ name: 'Alpha', description: 'new account' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(observedBody).toEqual({ name: 'Alpha', description: 'new account' })
    expect(
      qc.getQueryData<ServiceAccount[]>(serviceAccountsQueryKey)?.map((item) => item.id),
    ).toEqual(['sa-2', 'sa-1'])
    expect(qc.getQueryState(serviceAccountsQueryKey)?.isInvalidated).toBe(true)
    qc.clear()
  })

  it('fails create mutations when the response omits serviceAccount', async () => {
    server.use(http.post('/fb/v1/console/service-accounts', () => HttpResponse.json({})))
    const { qc, wrapper } = wrap()

    const { result } = renderHook(() => useCreateServiceAccount(), { wrapper })
    result.current.mutate({ name: 'Broken', description: '' })

    await waitFor(() => expect(result.current.isError).toBe(true))
    expect(result.current.error).toEqual(
      new Error('service account response missing serviceAccount'),
    )
    qc.clear()
  })

  it('updates cached service accounts in sorted order', async () => {
    server.use(
      http.patch('/fb/v1/console/service-accounts/sa-2', () =>
        HttpResponse.json(serviceAccount('sa-2', 'Beta', false)),
      ),
    )
    const { qc, wrapper } = wrap()
    qc.setQueryData<ServiceAccount[]>(serviceAccountsQueryKey, [
      serviceAccount('sa-1', 'Alpha'),
      serviceAccount('sa-2', 'Zulu'),
    ])

    const { result } = renderHook(() => useUpdateServiceAccount(), { wrapper })
    result.current.mutate({ id: 'sa-2', isActive: false })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(qc.getQueryData<ServiceAccount[]>(serviceAccountsQueryKey)).toMatchObject([
      { id: 'sa-1', name: 'Alpha', isActive: true },
      { id: 'sa-2', name: 'Beta', isActive: false },
    ])
    expect(qc.getQueryState(serviceAccountsQueryKey)?.isInvalidated).toBe(true)
    qc.clear()
  })

  it('removes deleted service accounts from cache', async () => {
    server.use(
      http.delete(
        '/fb/v1/console/service-accounts/sa-1',
        () => new HttpResponse(null, { status: 204 }),
      ),
    )
    const { qc, wrapper } = wrap()
    qc.setQueryData<ServiceAccount[]>(serviceAccountsQueryKey, [
      serviceAccount('sa-1', 'Alpha'),
      serviceAccount('sa-2', 'Beta'),
    ])

    const { result } = renderHook(() => useDeleteServiceAccount(), { wrapper })
    result.current.mutate('sa-1')

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(
      qc.getQueryData<ServiceAccount[]>(serviceAccountsQueryKey)?.map((item) => item.id),
    ).toEqual(['sa-2'])
    expect(qc.getQueryState(serviceAccountsQueryKey)?.isInvalidated).toBe(true)
    qc.clear()
  })
})
