import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { similarFeedbackQuery } from '@/features/feedback/api/similar-feedback'
import { server } from '@/testing/mocks/server'

describe('similarFeedbackQuery', () => {
  it('GETs /feedback/{id}/similar with stable query key and returns the body', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/29/similar', () =>
        HttpResponse.json({
          items: [
            {
              id: 12,
              title: 'PDF export blocked on Team plan',
              source: 'intercom',
              similarity: 0.99,
              created_at: '2026-07-26T10:00:00Z',
              linked_requests: [{ id: 'cr-2', cr_no: 2, title: 'PDF export', status: 'open' }],
            },
          ],
          anchor_linked_requests: [],
        }),
      ),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const data = await qc.fetchQuery(similarFeedbackQuery('29'))
    expect(data.items).toHaveLength(1)
    expect(data.items[0]?.linked_requests?.[0]?.cr_no).toBe(2)
    expect(data.anchor_linked_requests).toEqual([])
    expect(similarFeedbackQuery('29').queryKey).toEqual(['console', 'feedback', 'similar', '29'])
  })

  it('is disabled for an empty feedback id', () => {
    expect(similarFeedbackQuery('').enabled).toBe(false)
    expect(similarFeedbackQuery('29').enabled).toBe(true)
  })
})
