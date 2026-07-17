import { QueryClient } from '@tanstack/react-query'
import { isRedirect } from '@tanstack/react-router'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { SearchQualityPage } from '@/features/search-quality/components/search-quality-page'
import { Route as AnalyticsSearchQualityRoute } from '@/routes/_authed.analytics.search-quality'
import { Route as LegacySearchQualityRoute } from '@/routes/_authed.search-quality'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

const searchQualityFixture = {
  generatedAt: '2026-07-02T00:00:00Z',
  currentFrom: '2026-06-25T00:00:00Z',
  currentTo: '2026-07-02T00:00:00Z',
  bucketWidth: 'day',
  summary: {
    queryCount: '42',
    zeroResultCount: '4',
    zeroResultRate: 0.095,
    fallbackCount: '3',
    fallbackRate: 0.071,
    clickCount: '18',
    clickThroughRate: 0.42,
    averageResultCount: 8.4,
    p95LatencyMs: '640',
    worstSeverity: 'watch',
  },
  series: [
    {
      bucket: '2026-07-01T00:00:00Z',
      queryCount: '18',
      zeroResultCount: '2',
      zeroResultRate: 0.11,
      fallbackCount: '1',
      fallbackRate: 0.05,
      clickCount: '8',
      clickThroughRate: 0.44,
      p95LatencyMs: '620',
    },
  ],
  queries: [
    {
      queryHash: 'a'.repeat(64),
      queryPreview: 'login failures after SSO',
      queryCount: '12',
      zeroResultCount: '1',
      zeroResultRate: 0.08,
      fallbackCount: '0',
      clickCount: '7',
      clickThroughRate: 0.58,
      averageResultCount: 9.2,
      p95LatencyMs: '610',
      lastSeenAt: '2026-07-01T12:00:00Z',
    },
  ],
  zeroResultQueries: [
    {
      queryHash: 'b'.repeat(64),
      queryPreview: 'checkout mystery state',
      queryCount: '5',
      zeroResultCount: '5',
      zeroResultRate: 1,
      fallbackCount: '0',
      clickCount: '0',
      clickThroughRate: 0,
      averageResultCount: 0,
      p95LatencyMs: '480',
      lastSeenAt: '2026-07-01T13:00:00Z',
    },
  ],
  fallbackBreakdown: [{ reason: 'no_embeddings', count: '3', share: 1 }],
  indexHealth: {
    totalLiveFeedback: 100,
    totalWithEmbeddings: 92,
    coverageRatio: 0.92,
    embeddingModel: 'text-embedding-3-small',
    oldestMissingFeedbackAt: '2026-06-20T00:00:00Z',
    missingFeedbackCount: '8',
  },
  rankingVersions: [
    {
      rankingVersion: 'rrf.pgfts.v1.k60',
      status: 'active',
      trafficPercent: 100,
      notes: 'Current production ranker',
      updatedAt: '2026-07-01T00:00:00Z',
    },
  ],
}

interface ThrownRedirect {
  options: { to: string; statusCode?: number }
}

describe('_authed.analytics.search-quality route', () => {
  it('preloads the default search quality query from the canonical route loader', async () => {
    let seen: URL | undefined
    server.use(
      http.get('/fb/v1/console/feedback/search/quality', ({ request }) => {
        seen = new URL(request.url)
        return HttpResponse.json(searchQualityFixture)
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = AnalyticsSearchQualityRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toMatchObject({
      summary: { worstSeverity: 'watch' },
    })
    expect(AnalyticsSearchQualityRoute.options.component).toBeTypeOf('function')
    expect(seen?.searchParams.get('bucket_width')).toBe('day')
    expect(seen?.searchParams.get('limit')).toBe('10')
  })

  it('redirects the legacy shortcut route to analytics search quality', () => {
    const beforeLoad = LegacySearchQualityRoute.options.beforeLoad as () => void
    let thrown: unknown

    try {
      beforeLoad()
    } catch (err) {
      thrown = err
    }

    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/analytics/search-quality')
  })

  it('renders search relevance operations data', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/search/quality', () =>
        HttpResponse.json(searchQualityFixture),
      ),
    )

    renderWithProviders(<SearchQualityPage />)

    await waitFor(() => {
      expect(screen.getByText('login failures after SSO')).toBeInTheDocument()
    })
    expect(screen.getByText('搜索质量')).toBeInTheDocument()
    expect(screen.getByText('checkout mystery state')).toBeInTheDocument()
    expect(screen.getByText('索引健康')).toBeInTheDocument()
    expect(screen.getByText('暂无 embedding')).toBeInTheDocument()
    expect(screen.getByText('rrf.pgfts.v1.k60')).toBeInTheDocument()
  })

  it('reloads search quality when range, bucket, and limit controls change', async () => {
    const urls: string[] = []
    server.use(
      http.get('/fb/v1/console/feedback/search/quality', ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({
          ...searchQualityFixture,
          summary: {
            ...searchQualityFixture.summary,
            p95LatencyMs: '3600',
          },
        })
      }),
    )

    const { user } = renderWithProviders(<SearchQualityPage />)

    await screen.findByText('login failures after SSO')

    const choose = async (index: number, optionName: string) => {
      await user.click(screen.getAllByRole('combobox')[index])
      await user.click(await screen.findByRole('option', { name: optionName }))
    }

    await choose(1, '按小时')
    await waitFor(() =>
      expect(urls.some((url) => new URL(url).searchParams.get('bucket_width') === 'hour')).toBe(
        true,
      ),
    )

    await choose(0, '90 天')
    await waitFor(() => {
      const latest = new URL(urls.at(-1) ?? '')
      expect(latest.searchParams.get('bucket_width')).toBe('day')
    })

    await choose(2, 'Top 50')
    await waitFor(() =>
      expect(urls.some((url) => new URL(url).searchParams.get('limit') === '50')).toBe(true),
    )
  })

  it('renders empty datasets, defaults, and active latency tone', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/search/quality', () =>
        HttpResponse.json({
          ...searchQualityFixture,
          summary: {
            queryCount: '0',
            zeroResultCount: '0',
            zeroResultRate: 0.2,
            fallbackCount: '1',
            fallbackRate: 0.1,
            clickCount: '0',
            clickThroughRate: undefined,
            averageResultCount: undefined,
            p95LatencyMs: '1200',
            worstSeverity: '',
          },
          series: [],
          queries: [
            {
              queryHash: 'c'.repeat(64),
              queryPreview: '',
              queryCount: '1',
              zeroResultCount: '0',
              zeroResultRate: 0,
              fallbackCount: '0',
              clickCount: '0',
              clickThroughRate: 0,
              averageResultCount: 0,
              p95LatencyMs: '0',
              lastSeenAt: '2026-07-01T14:00:00Z',
            },
          ],
          zeroResultQueries: [],
          fallbackBreakdown: [{ reason: '', count: '1', share: 0 }],
          indexHealth: {
            totalLiveFeedback: 0,
            totalWithEmbeddings: 0,
            coverageRatio: 0,
            embeddingModel: '',
            missingFeedbackCount: '0',
          },
          rankingVersions: [
            {
              rankingVersion: 'draft-ranker',
              status: '',
              trafficPercent: 5,
              notes: '',
              updatedAt: '',
            },
          ],
        }),
      ),
    )

    renderWithProviders(<SearchQualityPage />)

    expect(await screen.findByText('暂无趋势数据')).toBeInTheDocument()
    expect(screen.getByText('未知原因')).toBeInTheDocument()
    expect(screen.getByText('暂无模型')).toBeInTheDocument()
    expect(screen.getByText('draft-ranker')).toBeInTheDocument()
    expect(screen.getByText(/^hash cccccccc$/)).toBeInTheDocument()
  })
})
