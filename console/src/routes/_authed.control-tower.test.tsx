import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  isRedirect,
  RouterProvider,
} from '@tanstack/react-router'
import { render } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { I18nextProvider } from 'react-i18next'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { TooltipProvider } from '@/components/ui/tooltip'
import i18n from '@/i18n'
import { Route as ControlTowerRoute } from '@/routes/_authed.control-tower'
import { Route as AuthedIndexRoute } from '@/routes/_authed.index'
import { ControlTowerPage } from '@/routes/-control-tower-page'
import { server } from '@/testing/mocks/server'
import { screen, waitFor } from '@/testing/test-utils'

beforeAll(() => {
  window.scrollTo = vi.fn()
})

const classificationFixture = {
  generatedAt: '2026-07-03T00:00:00Z',
  dataThrough: '2026-07-03T00:00:00Z',
  rollupLagSeconds: '0',
  currentFrom: '2026-06-26T00:00:00Z',
  currentTo: '2026-07-03T00:00:00Z',
  baselineFrom: '2026-05-29T00:00:00Z',
  baselineTo: '2026-06-26T00:00:00Z',
  bucketWidth: 'day',
  summary: {
    classificationEvents: '128',
    failedAttempts: '0',
    averageConfidence: 0.84,
    lowConfidenceRate: 0.12,
    offListRate: 0.03,
    unknownDimensionRate: 0,
    parseFailureRate: 0,
    terminalFailureRate: 0,
    worstSeverity: 'alert',
  },
  series: [],
  dimensions: [],
  warnings: [{}],
  samples: [],
}

const searchFixture = {
  generatedAt: '2026-07-03T00:00:00Z',
  currentFrom: '2026-06-26T00:00:00Z',
  currentTo: '2026-07-03T00:00:00Z',
  bucketWidth: 'day',
  summary: {
    queryCount: '44',
    zeroResultCount: '9',
    zeroResultRate: 0.21,
    fallbackCount: '5',
    fallbackRate: 0.11,
    clickCount: '18',
    clickThroughRate: 0.41,
    averageResultCount: 6.8,
    p95LatencyMs: '3200',
    worstSeverity: 'watch',
  },
  series: [],
  queries: [],
  zeroResultQueries: [
    {
      queryHash: 'a'.repeat(64),
      queryPreview: 'enterprise export webhook',
      queryCount: '5',
      zeroResultCount: '5',
      zeroResultRate: 1,
      fallbackCount: '0',
      clickCount: '0',
      clickThroughRate: 0,
      averageResultCount: 0,
      p95LatencyMs: '500',
      lastSeenAt: '2026-07-02T00:00:00Z',
    },
  ],
  fallbackBreakdown: [],
  indexHealth: {
    totalLiveFeedback: 100,
    totalWithEmbeddings: 82,
    coverageRatio: 0.82,
    embeddingModel: 'text-embedding-3-small',
    missingFeedbackCount: '18',
  },
  rankingVersions: [
    {
      rankingVersion: 'rrf.pgfts.v1.k60',
      status: 'active',
      trafficPercent: 100,
      notes: 'current',
      updatedAt: '2026-07-02T00:00:00Z',
    },
  ],
}

interface ThrownRedirect {
  options: { to: string; statusCode?: number }
}

describe('_authed.control-tower route', () => {
  it('preloads classification and search quality from the route loader', async () => {
    const seen = new Set<string>()
    server.use(
      http.get('/fb/v1/console/classification-quality', ({ request }) => {
        seen.add(new URL(request.url).pathname)
        return HttpResponse.json(classificationFixture)
      }),
      http.get('/fb/v1/console/feedback/search/quality', ({ request }) => {
        seen.add(new URL(request.url).pathname)
        return HttpResponse.json(searchFixture)
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = ControlTowerRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toHaveLength(2)
    expect(seen).toEqual(
      new Set(['/fb/v1/console/classification-quality', '/fb/v1/console/feedback/search/quality']),
    )
  })

  it('redirects the authenticated index to the control tower', () => {
    const beforeLoad = AuthedIndexRoute.options.beforeLoad as () => void
    let thrown: unknown

    try {
      beforeLoad()
    } catch (err) {
      thrown = err
    }

    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/control-tower')
  })

  it('renders the operating risks and proof trail', async () => {
    server.use(
      http.get('/fb/v1/console/classification-quality', () =>
        HttpResponse.json(classificationFixture),
      ),
      http.get('/fb/v1/console/feedback/search/quality', () => HttpResponse.json(searchFixture)),
    )

    renderControlTower()

    await waitFor(() => {
      expect(screen.getByText('控制塔')).toBeInTheDocument()
      expect(screen.getByText('低置信度升高')).toBeInTheDocument()
    })
    expect(screen.getByText('搜索零结果偏高')).toBeInTheDocument()
    expect(screen.getByText('enterprise export webhook')).toBeInTheDocument()
    expect(screen.getByText('rrf.pgfts.v1.k60')).toBeInTheDocument()
  })

  it('shows an explicit unavailable state when quality APIs fail', async () => {
    server.use(
      http.get(
        '/fb/v1/console/classification-quality',
        () => new HttpResponse(null, { status: 500 }),
      ),
      http.get(
        '/fb/v1/console/feedback/search/quality',
        () => new HttpResponse(null, { status: 500 }),
      ),
    )

    renderControlTower()

    await waitFor(() => {
      expect(screen.getByText('质量信号暂不可用')).toBeInTheDocument()
    })
  })
})

function renderControlTower() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Number.POSITIVE_INFINITY },
      mutations: { retry: false },
    },
  })
  const rootRoute = createRootRoute()
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/',
    component: ControlTowerPage,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })

  return render(
    <I18nextProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <RouterProvider router={router} />
        </TooltipProvider>
      </QueryClientProvider>
    </I18nextProvider>,
  )
}
