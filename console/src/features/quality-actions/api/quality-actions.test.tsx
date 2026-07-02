import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { server } from '@/testing/mocks/server'
import { renderHook, waitFor } from '@/testing/test-utils'
import {
  normalizeQualityActionsFilters,
  qualityActionsQuery,
  qualityActionsSearchParams,
  useUpdateQualityAction,
} from './quality-actions'

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

describe('quality action API', () => {
  it('normalizes filters and query params', () => {
    expect(normalizeQualityActionsFilters({ limit: 500 })).toEqual({ status: 'all', limit: 200 })
    expect(qualityActionsSearchParams({ status: 'open', limit: 10 }).toString()).toBe(
      'status=open&limit=10',
    )
    expect(qualityActionsSearchParams({ status: 'all', limit: 10 }).toString()).toBe('limit=10')
  })

  it('unwraps action list responses', async () => {
    server.use(
      http.get('/fb/v1/console/quality-actions', ({ request }) => {
        expect(new URL(request.url).searchParams.get('limit')).toBe('25')
        return HttpResponse.json({
          actions: [{ actionKey: 'control_tower.zero_result', status: 'open' }],
        })
      }),
    )

    const qc = makeQueryClient()
    await expect(qc.fetchQuery(qualityActionsQuery({ limit: 25 }))).resolves.toEqual([
      { actionKey: 'control_tower.zero_result', status: 'open' },
    ])
  })

  it('posts quality action updates', async () => {
    let posted: unknown
    server.use(
      http.post('/fb/v1/console/quality-actions/update', async ({ request }) => {
        posted = await request.json()
        return HttpResponse.json({ action: { actionKey: 'control_tower.zero_result' } })
      }),
    )

    const qc = makeQueryClient()
    const { result } = renderHook(() => useUpdateQualityAction(), { wrapper: wrapperFor(qc) })
    result.current.mutate({
      actionKey: 'control_tower.zero_result',
      signal: 'zero-result',
      status: 'acknowledged',
      severity: 'alert',
      targetPath: '/analytics/search-quality',
      metricLabel: 'Zero result',
      metricValue: '21%',
      recommendationKey: 'control_tower.action.zero_result',
      evidenceJson: '{}',
    })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(posted).toMatchObject({
      actionKey: 'control_tower.zero_result',
      status: 'acknowledged',
    })
  })
})
