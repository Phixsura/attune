import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { feedbackDetailQuery } from '@/features/feedback/api/get-feedback-detail'
import { server } from '@/testing/mocks/server'

describe('feedbackDetailQuery', () => {
  it('composes URL with the id and returns the typed body', async () => {
    let observedURL: string | null = null
    server.use(
      http.get('/fb/v1/console/feedback/:id', ({ request, params }) => {
        observedURL = request.url
        return HttpResponse.json({ id: params.id, content: 'payload' })
      }),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const data = (await qc.fetchQuery(feedbackDetailQuery('f-42'))) as {
      id: string
      content: string
    }
    expect(observedURL).toContain('/fb/v1/console/feedback/f-42')
    expect(data.id).toBe('f-42')
    expect(data.content).toBe('payload')
  })

  it('404 surfaces the error envelope', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({ code: 'not_found', message: 'no such feedback' }, { status: 404 }),
      ),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    await expect(qc.fetchQuery(feedbackDetailQuery('missing'))).rejects.toMatchObject({
      status: 404,
      code: 'not_found',
    })
  })
})
