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
import { ControlTowerPage, controlTowerPageTestables } from '@/routes/-control-tower-page'
import { server } from '@/testing/mocks/server'
import { screen, userEvent, waitFor } from '@/testing/test-utils'

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

const qualityActionsFixture = {
  actions: [
    {
      actionId: '11111111-1111-1111-1111-111111111111',
      actionKey: 'control_tower.zero_result',
      signal: 'zero-result',
      status: 'acknowledged',
      severity: 'alert',
      targetPath: '/analytics/search-quality',
      metricLabel: '搜索零结果偏高',
      metricValue: '21%',
      recommendationKey: 'control_tower.action.zero_result',
      evidenceJson: '{"metric":"21%"}',
      createdAt: '2026-07-03T00:00:00Z',
      lastSeenAt: '2026-07-03T00:00:00Z',
      acknowledgedAt: '2026-07-03T00:00:00Z',
      resolvedAt: '',
      dismissedAt: '',
      updatedAt: '2026-07-03T00:00:00Z',
      updatedBy: 'user-1',
    },
  ],
}

interface ThrownRedirect {
  options: { to: string; statusCode?: number }
}

describe('_authed.control-tower route', () => {
  it('normalizes helper edge cases for dashboard signals', () => {
    expect(controlTowerPageTestables.normalizeSeverity('alert', 'normal')).toBe('alert')
    expect(controlTowerPageTestables.normalizeSeverity('unknown', 'watch')).toBe('watch')
    expect(controlTowerPageTestables.worstSeverity(['normal', 'watch'])).toBe('watch')
    expect(controlTowerPageTestables.worstSeverity(['normal', 'insufficient_data'])).toBe(
      'insufficient_data',
    )
    expect(controlTowerPageTestables.metricTone('alert')).toBe('urgent')
    expect(controlTowerPageTestables.metricTone('insufficient_data')).toBe('active')
    expect(controlTowerPageTestables.metricTone('normal')).toBe('default')
    expect(controlTowerPageTestables.toNumber(Number.NaN, 7)).toBe(7)
    expect(controlTowerPageTestables.toNumber('42')).toBe(42)
    expect(controlTowerPageTestables.toNumber('not-a-number', 3)).toBe(3)
    expect(controlTowerPageTestables.toNumber(undefined, 5)).toBe(5)
    expect(controlTowerPageTestables.clampUnit(Number.NaN)).toBe(0)
    expect(controlTowerPageTestables.clampUnit(-0.5)).toBe(0)
    expect(controlTowerPageTestables.clampUnit(1.5)).toBe(1)
    expect(controlTowerPageTestables.formatLatency(999.4)).toBe('999ms')
    expect(controlTowerPageTestables.formatLatency(1200)).toBe('1.2s')
  })

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
      http.get('/fb/v1/console/quality-actions', ({ request }) => {
        seen.add(new URL(request.url).pathname)
        return HttpResponse.json(qualityActionsFixture)
      }),
      http.get('/fb/v1/console/cohort-sync/health', ({ request }) => {
        seen.add(new URL(request.url).pathname)
        return HttpResponse.json({
          sourceCount: 0,
          activeSources: 0,
          errorSources: 0,
          cohortCount: 0,
          totalActiveMembers: 0,
        })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = ControlTowerRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toHaveLength(4)
    expect(seen).toContain('/fb/v1/console/classification-quality')
    expect(seen).toContain('/fb/v1/console/feedback/search/quality')
    expect(seen).toContain('/fb/v1/console/quality-actions')
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
      http.get('/fb/v1/console/quality-actions', () => HttpResponse.json(qualityActionsFixture)),
    )

    renderControlTower()

    await waitFor(() => {
      expect(screen.getByText('控制塔')).toBeInTheDocument()
      expect(screen.getByText('低置信度升高')).toBeInTheDocument()
    })
    expect(screen.getByText('搜索零结果偏高')).toBeInTheDocument()
    expect(screen.getByText('处理中')).toBeInTheDocument()
    expect(screen.getByText('enterprise export webhook')).toBeInTheDocument()
    expect(screen.getByText('rrf.pgfts.v1.k60')).toBeInTheDocument()
  })

  it('updates quality actions from the risk queue and keeps failed actions retryable', async () => {
    const payloads: Record<string, unknown>[] = []
    let updateCalls = 0
    server.use(
      http.get('/fb/v1/console/classification-quality', () =>
        HttpResponse.json(classificationFixture),
      ),
      http.get('/fb/v1/console/feedback/search/quality', () => HttpResponse.json(searchFixture)),
      http.get('/fb/v1/console/quality-actions', () => HttpResponse.json({ actions: [] })),
      http.post('/fb/v1/console/quality-actions/update', async ({ request }) => {
        updateCalls += 1
        payloads.push((await request.json()) as Record<string, unknown>)
        if (updateCalls === 1) {
          return HttpResponse.json({ message: 'queue unavailable' }, { status: 500 })
        }
        return HttpResponse.json({})
      }),
    )

    const { user } = renderControlTower()

    await screen.findByText('低置信度升高')
    await user.click(screen.getAllByRole('button', { name: '开始处理' })[0])
    await waitFor(() => expect(payloads).toHaveLength(1))
    expect(payloads[0]).toMatchObject({
      actionKey: 'control_tower.classification_warnings',
      signal: 'classification-warnings',
      status: 'acknowledged',
      targetPath: '/analytics/classification-quality',
    })

    await user.click(screen.getAllByRole('button', { name: '标记已验证' })[0])
    await waitFor(() => expect(payloads).toHaveLength(2))
    expect(payloads[1]).toMatchObject({
      actionKey: 'control_tower.classification_warnings',
      signal: 'classification-warnings',
      status: 'resolved',
    })
  })

  it('renders empty and insufficient-data states when all signals are within target', async () => {
    server.use(
      http.get('/fb/v1/console/classification-quality', () =>
        HttpResponse.json({
          ...classificationFixture,
          summary: {
            ...classificationFixture.summary,
            classificationEvents: '0',
            lowConfidenceRate: 0,
            offListRate: 0,
            worstSeverity: 'normal',
          },
          warnings: [],
        }),
      ),
      http.get('/fb/v1/console/feedback/search/quality', () =>
        HttpResponse.json({
          ...searchFixture,
          summary: {
            ...searchFixture.summary,
            queryCount: '0',
            zeroResultRate: 0,
            fallbackRate: 0,
            p95LatencyMs: '200',
            worstSeverity: 'normal',
          },
          zeroResultQueries: [],
          indexHealth: {
            ...searchFixture.indexHealth,
            coverageRatio: 1,
          },
          rankingVersions: [],
        }),
      ),
      http.get('/fb/v1/console/quality-actions', () => HttpResponse.json({ actions: [] })),
    )

    renderControlTower()

    expect(await screen.findByText('暂无待处理动作')).toBeInTheDocument()
    expect(screen.getByText('数据不足')).toBeInTheDocument()
    expect(screen.getByText('暂无零结果 query')).toBeInTheDocument()
    expect(screen.getByText('暂无排序版本')).toBeInTheDocument()
  })

  it('keeps quality evidence visible when action status cannot be loaded', async () => {
    server.use(
      http.get('/fb/v1/console/classification-quality', () =>
        HttpResponse.json({
          ...classificationFixture,
          summary: {
            ...classificationFixture.summary,
            lowConfidenceRate: 0,
            offListRate: 0,
            worstSeverity: 'normal',
          },
        }),
      ),
      http.get('/fb/v1/console/feedback/search/quality', () =>
        HttpResponse.json({
          ...searchFixture,
          summary: {
            ...searchFixture.summary,
            zeroResultRate: 0,
            fallbackRate: 0,
            p95LatencyMs: '100',
            worstSeverity: 'normal',
          },
          indexHealth: {
            ...searchFixture.indexHealth,
            coverageRatio: 1.2,
          },
        }),
      ),
      http.get('/fb/v1/console/quality-actions', () =>
        HttpResponse.json({ message: 'actions failed' }, { status: 500 }),
      ),
    )

    renderControlTower()

    expect(await screen.findByText('分类质量出现告警')).toBeInTheDocument()
    expect(screen.getByText('动作状态暂不可用，仍可查看质量证据。')).toBeInTheDocument()
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
      http.get('/fb/v1/console/quality-actions', () => HttpResponse.json({ actions: [] })),
    )

    renderControlTower()

    await waitFor(() => {
      expect(screen.getByText('质量信号暂不可用')).toBeInTheDocument()
    })
  })
})

function renderControlTower() {
  const user = userEvent.setup({ delay: null })
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

  return {
    user,
    ...render(
      <I18nextProvider i18n={i18n}>
        <QueryClientProvider client={queryClient}>
          <TooltipProvider>
            <RouterProvider router={router} />
          </TooltipProvider>
        </QueryClientProvider>
      </I18nextProvider>,
    ),
  }
}
