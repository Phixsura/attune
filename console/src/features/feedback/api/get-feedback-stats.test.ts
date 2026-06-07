import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { feedbackStatsQuery } from '@/features/feedback/api/get-feedback-stats'
import { server } from '@/testing/mocks/server'

describe('feedbackStatsQuery', () => {
  it('GETs /feedback/stats with stable query key and returns the body', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({ total: 42, dims: [{ dim: 'severity', top: [] }] }),
      ),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const data = (await qc.fetchQuery(feedbackStatsQuery())) as {
      total: number
      dims: unknown[]
    }
    expect(data.total).toBe(42)
    expect(data.dims).toHaveLength(1)
    expect(feedbackStatsQuery().queryKey).toEqual(['console', 'feedback', 'stats'])
  })
})
