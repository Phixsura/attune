import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { llmUsageQuery } from '@/features/usage/api/get-llm-usage'
import { server } from '@/testing/mocks/server'

function makeQc() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

describe('llmUsageQuery', () => {
  it('builds the llm usage query string', async () => {
    let seen: URL | undefined
    server.use(
      http.get('/fb/v1/console/llm-usage', ({ request }) => {
        seen = new URL(request.url)
        return HttpResponse.json({ series: [] })
      }),
    )

    await makeQc().fetchQuery(llmUsageQuery({ granularity: 'month', range: 'now-90d' }))

    expect(seen?.searchParams.get('granularity')).toBe('month')
    expect(seen?.searchParams.get('range')).toBe('now-90d')
  })
})
