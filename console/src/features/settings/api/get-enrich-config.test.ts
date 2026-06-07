import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { server } from '@/testing/mocks/server'

describe('enrichConfigQuery', () => {
  it('unwraps resp.config from GetEnrichConfigResponse', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: {
            modules: ['payment', 'checkout'],
            promptTemplate: 'tpl',
            dimensions: [],
          },
        }),
      ),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const cfg = await qc.fetchQuery(enrichConfigQuery())
    expect(cfg).toEqual({ modules: ['payment', 'checkout'], promptTemplate: 'tpl', dimensions: [] })
  })

  it('error envelope surfaces (e.g. 500)', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({ code: 'internal', message: 'boom' }, { status: 500 }),
      ),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    await expect(qc.fetchQuery(enrichConfigQuery())).rejects.toMatchObject({
      status: 500,
      code: 'internal',
    })
  })
})
