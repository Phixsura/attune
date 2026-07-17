import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { createElement, type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { usePromoteSuggestedValue } from '@/features/settings/api/promote-suggested-value'
import { server } from '@/testing/mocks/server'

function wrap() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  })
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children)
  return { qc, wrapper }
}

describe('usePromoteSuggestedValue', () => {
  it('keeps an empty eval-suggestions cache empty while invalidating enrich config', async () => {
    server.use(http.post('/fb/v1/console/enrich-config/promote', () => HttpResponse.json({})))
    const { qc, wrapper } = wrap()
    const { result } = renderHook(() => usePromoteSuggestedValue(), { wrapper })

    result.current.mutate({ dimensionName: 'topic', value: 'billing' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(qc.getQueryData(['console', 'eval-suggestions'])).toBeUndefined()
  })

  it('removes the promoted candidate and recommendation from cached suggestions', async () => {
    let body: unknown
    server.use(
      http.post('/fb/v1/console/enrich-config/promote', async ({ request }) => {
        body = await request.json()
        return HttpResponse.json({})
      }),
    )
    const { qc, wrapper } = wrap()
    qc.setQueryData(['console', 'eval-suggestions'], {
      candidates: [
        { dim: 'topic', value: 'billing' },
        { dim: 'topic', value: 'login' },
        { dim: 'severity', value: 'billing' },
      ],
      recommendations: [
        { dim: 'topic', value: 'billing' },
        { dim: 'topic', value: 'login' },
      ],
    })
    const { result } = renderHook(() => usePromoteSuggestedValue(), { wrapper })

    result.current.mutate({ dimensionName: 'topic', value: 'billing' })

    await waitFor(() => expect(result.current.isSuccess).toBe(true))
    expect(body).toEqual({ dimensionName: 'topic', value: 'billing' })
    expect(qc.getQueryData(['console', 'eval-suggestions'])).toEqual({
      candidates: [
        { dim: 'topic', value: 'login' },
        { dim: 'severity', value: 'billing' },
      ],
      recommendations: [{ dim: 'topic', value: 'login' }],
    })
  })
})
