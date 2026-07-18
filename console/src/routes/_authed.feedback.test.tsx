import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Route as FeedbackShellRoute } from '@/routes/_authed.feedback'
import { Route as FeedbackClustersRoute } from '@/routes/_authed.feedback.clusters'
import { Route as FeedbackIndexRoute } from '@/routes/_authed.feedback.index'
import { Route as PortalInboxRoute } from '@/routes/_authed.feedback.portal'
import { Route as TerminalFailuresRoute } from '@/routes/_authed.feedback.terminal-failures'
import { FeedbackRoutePage } from '@/routes/-feedback-route-page'
import { server } from '@/testing/mocks/server'
import { fireEvent, renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

const navigateMock = vi.hoisted(() => vi.fn())

vi.mock('@tanstack/react-router', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-router')>('@tanstack/react-router')

  return {
    ...actual,
    Link: ({ children, to, ...props }: { children: ReactNode; to?: string }) => (
      <a href={to ?? '#'} {...props}>
        {children}
      </a>
    ),
    useNavigate: () => navigateMock,
  }
})

vi.mock('@/features/session/hooks/use-permissions', () => ({
  usePermissions: () => ({
    can: () => true,
  }),
}))

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn(), warning: vi.fn() } }))

// Route-level smoke test for the feedback page. The unit tests cover
// individual hooks + components in isolation; this test covers the
// composition layer where users actually live — the wiring between
// enrichConfigQuery → dims, feedbackListInfiniteQuery → table,
// row click → detail sheet open, detail query → sheet content.
//
// Without this, a regression like "FeedbackRoutePage forgot to pass dims
// to DimensionChips" would land green: the components are tested
// but their integration on the page is not.

const dimsFixture = [
  {
    name: 'severity',
    displayName: { entries: { default: 'Severity' } },
    kind: 'single',
    taxonomy: [
      { value: 'P0', displayName: { entries: { default: 'P0' } } },
      { value: 'P1', displayName: { entries: { default: 'P1' } } },
    ],
    urgentSet: ['P0'],
    required: false,
    examples: [],
    extractionHint: '',
  },
]

const itemFixture = {
  id: '101',
  content: 'login is broken when password has unicode',
  enrichedTitle: 'Login fails on unicode password',
  enrichedDisplayTitle: 'Unicode 密码登录失败',
  enrichedDisplayLocale: 'zh',
  enrichedAttrs: { severity: 'P0' },
  isUrgent: true,
  language: 'en',
  source: 'web',
  userId: 'user-7',
  createdAt: '2026-06-07T08:30:00Z',
  type: 'bug',
  enrichmentStatus: 'done',
  tags: [],
}

const terminalItemFixture = {
  ...itemFixture,
  id: '201',
  content: 'terminal failure during enrichment',
  enrichedTitle: 'Terminal enrichment failure',
  enrichedDisplayTitle: '终态失败样本',
  enrichmentStatus: 'failed',
  enrichmentAttempts: 5,
  enrichmentNextRetryAt: '',
  isUrgent: false,
  createdAt: '2026-06-07T09:00:00Z',
}

const detailFixture = {
  id: '101',
  content: 'login is broken when password has unicode',
  source: 'web',
  type: 'bug',
  userId: 'user-7',
  pageUrl: '',
  enrichedTitle: 'Login fails on unicode password',
  enrichedDisplayTitle: 'Unicode 密码登录失败',
  enrichedRationale: 'Unicode normalization bug',
  enrichedDisplayRationale: 'Unicode 规范化问题',
  enrichedDisplayLocale: 'zh',
  enrichedAttrs: { severity: 'P0' },
  isUrgent: true,
  language: 'en',
  enrichmentStatus: 'done',
  createdAt: '2026-06-07T08:30:00Z',
  attachments: [],
  enrichmentError: '',
  enrichedAt: '2026-06-07T08:31:00Z',
}

const terminalDetailFixture = {
  ...detailFixture,
  id: '201',
  content: 'terminal failure during enrichment',
  enrichedTitle: 'Terminal enrichment failure',
  enrichedDisplayTitle: '终态失败样本',
  enrichedDisplayRationale: '终态失败的聚类样本',
  enrichedRationale: 'terminal failure sample',
  enrichmentStatus: 'failed',
  enrichmentAttempts: 5,
  enrichmentNextRetryAt: '',
  enrichmentError: 'llm: upstream exhausted',
  enrichmentFailureReasonClass: 'llm_err',
  enrichmentFailureModel: 'gpt-4.1',
  enrichmentFailureChannelName: 'Primary',
  enrichmentFailureChannelId: 'chan-primary',
  enrichmentFailureConfigFingerprint: 'sha256:abc123',
  enrichmentFailurePromptVersion: 'v1',
}

describe('_authed.feedback route — user flow smoke', () => {
  beforeEach(() => {
    navigateMock.mockClear()
    vi.mocked(toast.error).mockClear()
    vi.mocked(toast.success).mockClear()
    vi.mocked(toast.warning).mockClear()
  })

  it('preserves numeric drilldown search params parsed by the router', () => {
    const validateSearch = FeedbackIndexRoute.options.validateSearch as (
      search: Record<string, unknown>,
    ) => { ids?: string | number; confidence_lte?: string | number; quality_signal?: string }

    expect(
      validateSearch({
        ids: 101,
        confidence_lte: 0.55,
        quality_signal: 'low_confidence',
      }),
    ).toEqual({
      ids: 101,
      confidence_lte: 0.55,
      quality_signal: 'low_confidence',
    })
  })

  it('wires the feedback route shells to the shared pages', () => {
    expect(FeedbackShellRoute.options.component).toBeTypeOf('function')
    expect(FeedbackClustersRoute.options.component).toBeTypeOf('function')
    expect(PortalInboxRoute.options.component).toBeTypeOf('function')
    expect(TerminalFailuresRoute.options.component).toBeTypeOf('function')
  })

  it('renders title + table row from the list query, opens sheet with detail on row click', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/feedback/:id', ({ params }) =>
        HttpResponse.json({ ...detailFixture, id: params.id }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/feedback/:id/audit', ({ params }) =>
        HttpResponse.json({ entries: [], feedbackId: params.id }),
      ),
      http.get('/fb/v1/console/customer-requests', () =>
        HttpResponse.json({ items: [], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    // Wait for the list query to land + render the row title.
    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
    expect(screen.getByRole('heading', { name: '反馈' })).toBeInTheDocument()
    expect(screen.getByText('筛选视角')).toBeInTheDocument()
    expect(screen.getAllByText('反馈队列').length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('当前工作面').length).toBeGreaterThanOrEqual(1)
    expect(screen.getByText('推荐视角')).toBeInTheDocument()
    expect(screen.getByText('队列体征')).toBeInTheDocument()
    expect(screen.getByText('操作上下文')).toBeInTheDocument()
    // Dim column header rendered from enrich-config dims. Two
    // matches expected: one in the stats card title, one in the
    // table column header.
    expect(screen.getAllByText('Severity').length).toBeGreaterThanOrEqual(1)
    // Per-row dim cell rendered via DimensionChips (P0 resolved from
    // the dim's taxonomy).
    const p0Badges = screen.getAllByText('P0')
    expect(p0Badges.length).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByTitle('原文语言：英文').length).toBeGreaterThanOrEqual(1)

    // Click the row → setDetailId fires → FeedbackDetailSheet opens →
    // its useQuery(feedbackDetailQuery(id)) runs → MSW returns
    // detailFixture → sheet body renders the rationale.
    await user.click(screen.getByText('Unicode 密码登录失败'))
    await waitFor(() => {
      expect(screen.getByText('Unicode 规范化问题')).toBeInTheDocument()
    })
    expect(screen.getByText('Unicode normalization bug')).toBeInTheDocument()

    await user.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByText('Unicode normalization bug')).toBeNull()
    })
  }, 20_000) // Route-level smoke composes lazy routes, queries, and sheet rendering.

  it('updates quality drilldown filters when the same route receives new search-derived props', async () => {
    const seen: URL[] = []
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        seen.push(new URL(request.url))
        return HttpResponse.json({ items: [itemFixture], nextCursor: undefined })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { rerender } = renderWithProviders(
      <FeedbackRoutePage
        initialQualityFilters={{ ids: ['101'], qualitySignal: 'low_confidence' }}
      />,
    )

    await waitFor(() => {
      expect(seen.some((url) => url.searchParams.get('ids') === '101')).toBe(true)
    })
    rerender(
      <FeedbackRoutePage
        initialQualityFilters={{ ids: ['202'], qualitySignal: 'parse_failure' }}
      />,
    )
    await waitFor(() => {
      expect(seen.some((url) => url.searchParams.get('ids') === '202')).toBe(true)
    })
    expect(seen[seen.length - 1]?.searchParams.get('quality_signal')).toBe('parse_failure')
  }, 20_000)

  it('updates source and type scope when the same route receives new scope props', async () => {
    const seen: URL[] = []
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        seen.push(new URL(request.url))
        return HttpResponse.json({ items: [itemFixture], nextCursor: undefined })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { rerender } = renderWithProviders(
      <FeedbackRoutePage initialSourceFilter="portal" initialTypeFilter="bug" />,
    )

    await waitFor(() => {
      expect(
        seen.some(
          (url) =>
            url.searchParams.get('source') === 'portal' && url.searchParams.get('type') === 'bug',
        ),
      ).toBe(true)
    })

    rerender(<FeedbackRoutePage initialSourceFilter="web" initialTypeFilter="request" />)

    await waitFor(() => {
      expect(
        seen.some(
          (url) =>
            url.searchParams.get('source') === 'web' && url.searchParams.get('type') === 'request',
        ),
      ).toBe(true)
    })
  })

  it('queue rail primary action opens the current highest-priority feedback', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/feedback/:id', ({ params }) =>
        HttpResponse.json({ ...detailFixture, id: params.id }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/feedback/:id/audit', ({ params }) =>
        HttpResponse.json({ entries: [], feedbackId: params.id }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '打开紧急反馈' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '打开紧急反馈' }))

    await waitFor(() => {
      expect(screen.getByText('Unicode 规范化问题')).toBeInTheDocument()
    })
  })

  it('terminal queue exposes a shortcut to the dedicated workbench', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        const url = new URL(request.url)
        const terminalOnly = url.searchParams.get('terminal_failed_only') === 'true'
        return HttpResponse.json({
          items: terminalOnly ? [terminalItemFixture] : [itemFixture],
          nextCursor: undefined,
        })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    renderWithProviders(<FeedbackRoutePage initialQueueMode="terminal" />)

    await waitFor(() => {
      expect(screen.getByRole('link', { name: '打开终态失败工位' })).toBeInTheDocument()
    })
    expect(screen.getByRole('link', { name: '打开终态失败工位' })).toHaveAttribute(
      'href',
      '/feedback/terminal-failures',
    )
  })

  it('portal inbox defaults to portal submissions and surfaces the portal title', async () => {
    const seen: URL[] = []
    const portalItemFixture = {
      ...itemFixture,
      id: '301',
      content: 'portal submission from a customer',
      enrichedTitle: 'Portal submission',
      enrichedDisplayTitle: '门户提交',
      source: 'portal',
      type: 'request',
      isUrgent: false,
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        seen.push(new URL(request.url))
        return HttpResponse.json({ items: [portalItemFixture], nextCursor: undefined })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    renderWithProviders(
      <FeedbackRoutePage
        initialSourceFilter="portal"
        titleKey="nav.portal_inbox"
        subtitleKey="feedback.portal.subtitle"
      />,
    )

    await waitFor(() => {
      expect(screen.getByRole('heading', { name: '门户收件箱' })).toBeInTheDocument()
    })
    expect(await screen.findByText('门户投稿')).toBeInTheDocument()
    expect(seen.some((url) => url.searchParams.get('source') === 'portal')).toBe(true)
  }, 20_000)

  it('portal inbox shows a portal-specific empty state when no submissions exist', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '0',
          dims: [],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    renderWithProviders(
      <FeedbackRoutePage
        initialSourceFilter="portal"
        titleKey="nav.portal_inbox"
        subtitleKey="feedback.portal.subtitle"
      />,
    )

    expect(await screen.findByText('当前还没有收到门户投稿')).toBeInTheDocument()
    await waitFor(() => {
      expect(screen.getByRole('link', { name: /打开实时门户/ })).toHaveAttribute(
        'href',
        '/portal/default',
      )
      expect(screen.getByRole('link', { name: /查看公开设置/ })).toHaveAttribute(
        'href',
        '/integrations/public-visibility',
      )
    })
  }, 20_000)

  it('shows the default empty workspace when the feedback queue has no rows', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '0',
          dims: [],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    renderWithProviders(<FeedbackRoutePage />)

    expect(await screen.findByText('还没有反馈')).toBeInTheDocument()
    expect(screen.getByText('先签发 API key')).toBeInTheDocument()
    expect(screen.getByText('校准 AI 分类')).toBeInTheDocument()
    expect(screen.getByText('接通反馈入口')).toBeInTheDocument()
    expect(screen.getByText('先用真实样本校准分类')).toBeInTheDocument()
    expect(screen.getByText('补上通知和分发链路')).toBeInTheDocument()
  }, 20_000)

  it('terminal workbench priority is reflected in the queue deck', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        const url = new URL(request.url)
        const terminalOnly = url.searchParams.get('terminal_failed_only') === 'true'
        return HttpResponse.json({
          items: terminalOnly ? [terminalItemFixture] : [itemFixture],
          nextCursor: undefined,
        })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/feedback/terminal-failures', () =>
        HttpResponse.json({
          periodStart: '2026-06-01T00:00:00Z',
          periodEnd: '2026-06-30T12:00:00Z',
          totalTerminalFailures: '1',
          oldestCreatedAt: '2026-06-01T00:00:00Z',
          reasonClassClusters: [
            {
              key: 'llm_err',
              label: 'LLM error',
              count: '1',
              oldestCreatedAt: '2026-06-01T00:00:00Z',
              newestCreatedAt: '2026-06-01T00:00:00Z',
              sampleFeedbackIds: ['201'],
              remediationHint: 'Check the routed LLM channel and provider health.',
            },
          ],
          modelChannelClusters: [
            {
              key: 'gpt-4.1::primary',
              label: 'GPT-4.1 / Primary',
              count: '2',
              oldestCreatedAt: '2026-06-01T00:00:00Z',
              newestCreatedAt: '2026-06-02T00:00:00Z',
              sampleFeedbackIds: ['201'],
              remediationHint: 'Check the routed LLM channel and provider health.',
            },
          ],
          configFingerprintClusters: [],
          ageBucketClusters: [],
        }),
      ),
      http.get('/fb/v1/console/feedback/:id', ({ params }) =>
        HttpResponse.json(
          params.id === '201' ? terminalDetailFixture : { ...detailFixture, id: params.id },
        ),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/feedback/:id/audit', ({ params }) =>
        HttpResponse.json({ entries: [], feedbackId: params.id }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(
      <FeedbackRoutePage initialQueueMode="terminal" showTerminalWorkbench />,
    )

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '打开样本 #201' })).toBeInTheDocument()
    })
    expect(screen.getByRole('link', { name: '返回反馈队列' })).toHaveAttribute('href', '/feedback')
    expect(screen.getAllByRole('link', { name: '打开 LLM 配置' }).length).toBeGreaterThanOrEqual(2)

    await user.click(screen.getByRole('button', { name: '打开样本 #201' }))

    await waitFor(() => {
      expect(screen.getByText('终态失败的聚类样本')).toBeInTheDocument()
    })
  })

  it('terminal workbench error state can retry the failed summary request', async () => {
    let workbenchCalls = 0

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        const url = new URL(request.url)
        const terminalOnly = url.searchParams.get('terminal_failed_only') === 'true'
        return HttpResponse.json({
          items: terminalOnly ? [terminalItemFixture] : [itemFixture],
          nextCursor: undefined,
        })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/feedback/terminal-failures', () => {
        workbenchCalls += 1
        if (workbenchCalls === 1) {
          return HttpResponse.json({ code: 'INTERNAL', message: 'workbench down' }, { status: 500 })
        }
        return HttpResponse.json({
          periodStart: '2026-06-01T00:00:00Z',
          periodEnd: '2026-06-30T12:00:00Z',
          totalTerminalFailures: '1',
          oldestCreatedAt: '2026-06-01T00:00:00Z',
          reasonClassClusters: [
            {
              key: 'llm_err',
              label: 'LLM error',
              count: '1',
              oldestCreatedAt: '2026-06-01T00:00:00Z',
              newestCreatedAt: '2026-06-01T00:00:00Z',
              sampleFeedbackIds: ['201'],
              remediationHint: 'Check the routed LLM channel and provider health.',
            },
          ],
          modelChannelClusters: [],
          configFingerprintClusters: [],
          ageBucketClusters: [],
        })
      }),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(
      <FeedbackRoutePage initialQueueMode="terminal" showTerminalWorkbench />,
    )

    await waitFor(() => {
      expect(screen.getByText('终态失败工位暂时无法加载')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '打开样本 #201' })).toBeInTheDocument()
    })
  })

  it('recommended lane can switch into urgent scope before manual filtering', async () => {
    const calmItem = {
      ...itemFixture,
      id: '102',
      enrichedDisplayTitle: '普通反馈',
      enrichedTitle: 'Normal feedback',
      content: 'just a normal suggestion',
      isUrgent: false,
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        const url = new URL(request.url)
        const urgent = url.searchParams.get('urgent')
        return HttpResponse.json({
          items: urgent === 'true' ? [itemFixture] : [itemFixture, calmItem],
          nextCursor: undefined,
        })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('普通反馈')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '切到紧急视角' }))

    await waitFor(() => {
      expect(screen.queryByText('普通反馈')).toBeNull()
    })
    expect(screen.getByText('仅查看紧急反馈')).toBeInTheDocument()
  })

  it('recommended lane also offers a direct-open shortcut for the priority feedback', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/feedback/:id', ({ params }) =>
        HttpResponse.json({ ...detailFixture, id: params.id }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/feedback/:id/audit', ({ params }) =>
        HttpResponse.json({ entries: [], feedbackId: params.id }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByRole('button', { name: '直接打开优先反馈' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '直接打开优先反馈' }))

    await waitFor(() => {
      expect(screen.getByText('Unicode 规范化问题')).toBeInTheDocument()
    })
  })

  it('queue lane actions open the priority feedback for each explicit subqueue', async () => {
    const activeItem = {
      ...itemFixture,
      id: '102',
      enrichedDisplayTitle: '处理中反馈',
      enrichedTitle: 'Active feedback',
      isUrgent: false,
      workflowState: {
        id: 'ws-active',
        name: 'in_progress',
        displayName: { entries: { default: 'In Progress', zh: '处理中' } },
        color: '#f59e0b',
        category: 'active',
        position: 1,
        isDefault: false,
        archived: false,
        createdAt: '2026-06-07T00:00:00Z',
        updatedAt: '2026-06-07T00:00:00Z',
      },
    }
    const failedItem = {
      ...itemFixture,
      id: '103',
      enrichedDisplayTitle: '失败反馈',
      enrichedTitle: 'Failed feedback',
      enrichmentStatus: 'failed',
      enrichmentAttempts: 1,
      enrichmentNextRetryAt: '2099-06-07T10:00:00Z',
      isUrgent: false,
    }
    const readyItem = {
      ...itemFixture,
      id: '104',
      enrichedDisplayTitle: '已就绪反馈',
      enrichedTitle: 'Ready feedback',
      enrichedAttrs: { severity: 'P1' },
      isUrgent: false,
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({
          items: [itemFixture, activeItem, failedItem, readyItem],
          nextCursor: undefined,
        }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '4',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/feedback/:id', ({ params }) =>
        HttpResponse.json({ ...detailFixture, id: params.id }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/feedback/:id/audit', ({ params }) =>
        HttpResponse.json({ entries: [], feedbackId: params.id }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)
    const clickLaneAction = async (title: string, action: string) => {
      const titleNode = await screen.findByText(title)
      const lane = titleNode.parentElement?.parentElement
      expect(lane).toBeTruthy()
      await user.click(within(lane as HTMLElement).getByRole('button', { name: action }))
      await waitFor(() => {
        expect(screen.getByText('Unicode 规范化问题')).toBeInTheDocument()
      })
      await user.keyboard('{Escape}')
      await waitFor(() => {
        expect(screen.queryByText('Unicode 规范化问题')).toBeNull()
      })
    }

    await waitFor(() => {
      expect(screen.getByText('处理中反馈')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '处理中1' }))
    await clickLaneAction('先推进这批处理中反馈', '打开处理中反馈')

    await user.click(screen.getByRole('button', { name: /^富化失败\d+$/ }))
    await clickLaneAction('先处理失败条目里的优先反馈', '打开失败条目')

    await user.click(screen.getByRole('button', { name: /^AI 已就绪\d+$/ }))
    await clickLaneAction('优先消费 AI 已就绪反馈', '打开 AI 已就绪反馈')

    await user.click(screen.getByRole('button', { name: /^紧急\d+$/ }))
    await clickLaneAction('继续处理这批紧急反馈', '打开紧急反馈')
  })

  it('queue lane all-mode actions open default and failed priorities', async () => {
    const failedOnly = {
      ...itemFixture,
      id: '202',
      enrichedDisplayTitle: '失败优先反馈',
      enrichedTitle: 'Failed priority feedback',
      enrichmentStatus: 'failed',
      enrichmentAttempts: 1,
      isUrgent: false,
    }
    const readyOnly = {
      ...itemFixture,
      id: '203',
      enrichedDisplayTitle: '普通就绪反馈',
      enrichedTitle: 'Ready priority feedback',
      isUrgent: false,
    }
    let showFailure = false

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [showFailure ? failedOnly : readyOnly], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/feedback/:id', ({ params }) =>
        HttpResponse.json({ ...detailFixture, id: params.id }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/feedback/:id/audit', ({ params }) =>
        HttpResponse.json({ entries: [], feedbackId: params.id }),
      ),
      http.get('/fb/v1/console/customer-requests', () =>
        HttpResponse.json({ items: [], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const firstRender = renderWithProviders(<FeedbackRoutePage />)
    const clickLaneAction = async (
      user: ReturnType<typeof renderWithProviders>['user'],
      title: string,
      action: string,
    ) => {
      const titleNode = await screen.findByText(title)
      const lane = titleNode.parentElement?.parentElement
      expect(lane).toBeTruthy()
      await user.click(within(lane as HTMLElement).getByRole('button', { name: action }))
      await waitFor(() => {
        expect(screen.getByText('Unicode 规范化问题')).toBeInTheDocument()
      })
      await user.keyboard('{Escape}')
      await waitFor(() => {
        expect(screen.queryByText('Unicode 规范化问题')).toBeNull()
      })
    }

    await waitFor(() => {
      expect(screen.getByText('普通就绪反馈')).toBeInTheDocument()
    })
    await clickLaneAction(firstRender.user, '先打开当前优先反馈', '打开当前优先反馈')

    firstRender.unmount()
    showFailure = true
    const secondRender = renderWithProviders(<FeedbackRoutePage />)
    await waitFor(() => {
      expect(screen.getByText('失败优先反馈')).toBeInTheDocument()
    })
    await clickLaneAction(secondRender.user, '先处理失败条目里的优先反馈', '打开失败条目')
  })

  it('active filter chips can be removed individually', async () => {
    const assignedTag = {
      id: 'tag-billing',
      name: 'Billing',
      color: '#06b6d4',
      archived: false,
    }
    const workflowState = {
      id: 'ws-1',
      name: 'open',
      displayName: { entries: { default: 'Open', zh: '待处理' } },
      color: '#3b82f6',
      category: 'open',
      position: 0,
      isDefault: true,
      archived: false,
      createdAt: '2026-06-07T00:00:00Z',
      updatedAt: '2026-06-07T00:00:00Z',
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () => {
        return HttpResponse.json({
          items: [{ ...itemFixture, tags: [assignedTag] }],
          nextCursor: undefined,
        })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () =>
        HttpResponse.json({ states: [workflowState] }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [assignedTag] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(
      <FeedbackRoutePage
        initialQualityFilters={{
          ids: ['101', '102'],
          qualitySignal: 'low_confidence',
          confidenceLte: 0.5,
        }}
      />,
    )

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '关键词' }))
    await user.click(screen.getByRole('button', { name: '终态失败0' }))
    await waitFor(() => {
      expect(screen.getByText('当前范围先回到更大的工作面')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '全部' }))
    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
    await user.type(screen.getByRole('searchbox'), 'unicode')
    await user.click(screen.getByRole('button', { name: '仅看紧急' }))
    await user.click(screen.getByRole('button', { name: '全部反馈' }))
    await user.click(screen.getByRole('button', { name: '仅看紧急' }))
    await user.click(screen.getByLabelText('来源'))
    await user.click(screen.getByRole('option', { name: '网页' }))
    await user.click(screen.getByLabelText('类型'))
    await user.click(screen.getByRole('option', { name: '缺陷' }))
    await user.click(screen.getByLabelText('所有 Severity'))
    await user.click(screen.getByRole('option', { name: 'P0' }))
    await user.click(screen.getByLabelText('所有标签'))
    await user.click(screen.getByRole('option', { name: 'Billing' }))
    await user.click(screen.getByLabelText('所有状态'))
    await user.click(screen.getByRole('option', { name: '待处理' }))
    await user.click(screen.getByLabelText('AI 富化状态'))
    await user.click(screen.getByRole('option', { name: '已富化' }))
    await user.click(screen.getByLabelText('分诊顺序'))
    await user.click(screen.getByRole('option', { name: '紧急优先' }))
    await user.click(screen.getByRole('button', { name: '紧急1' }))
    await user.click(screen.getByRole('button', { name: '语义' }))

    await waitFor(() => {
      expect(screen.getByText('仅查看紧急反馈')).toBeInTheDocument()
    })

    const chipLabels = [
      'Severity P0',
      '所有标签 Billing',
      '工作流状态 待处理',
      'AI 富化状态 已富化',
      '来源 网页',
      '类型 缺陷',
      '质量样本 2 条反馈',
      '质量信号 低置信度',
      '置信度 ≤ 50%',
      '搜索模式 语义',
      '分诊顺序 紧急优先',
      '当前子队列 紧急',
    ]
    for (const label of chipLabels) {
      await user.click(screen.getByLabelText(label))
      await waitFor(() => {
        expect(screen.queryByLabelText(label)).toBeNull()
      })
    }

    await user.click(screen.getByLabelText('关键词搜索 unicode'))
    await waitFor(() => {
      expect(screen.queryByLabelText('关键词搜索 unicode')).toBeNull()
    })

    await user.click(screen.getByLabelText('紧急信号 仅查看紧急反馈'))
    await waitFor(() => {
      expect(screen.queryByLabelText('紧急信号 仅查看紧急反馈')).toBeNull()
    })
  })

  it('renders the workflow-state badge on a row via the displayName resolver', async () => {
    // A feedback row carrying a workflowState whose human label lives in
    // displayName (machine key is the "open" slug). The badge must show
    // the resolved label, not the slug.
    const workflowState = {
      id: 'ws-1',
      name: 'open',
      displayName: { entries: { default: 'Open', zh: '待处理' } },
      color: '#3b82f6',
      category: 'open',
      position: 0,
      isDefault: true,
      archived: false,
      createdAt: '2026-06-07T00:00:00Z',
      updatedAt: '2026-06-07T00:00:00Z',
    }
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: [] },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({
          items: [{ ...itemFixture, workflowState }],
          nextCursor: undefined,
        }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [],
          urgentCount: '0',
        }),
      ),
      // The filter dropdown lists active states; resolves displayName too.
      http.get('/fb/v1/console/workflow/states', () =>
        HttpResponse.json({ states: [workflowState] }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    renderWithProviders(<FeedbackRoutePage />)

    // The row badge resolves the zh display name ("待处理"), never "open".
    await waitFor(() => {
      expect(screen.getByText('待处理')).toBeInTheDocument()
    })
    expect(screen.queryByText('open')).not.toBeInTheDocument()
  })

  it('500 from /feedback renders a retryable error state instead of an empty list', async () => {
    let listCalls = 0

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: [] },
        }),
      ),
      http.get('/fb/v1/console/feedback', () => {
        listCalls += 1
        if (listCalls === 1) {
          return HttpResponse.json({ code: 'INTERNAL', message: 'boom' }, { status: 500 })
        }
        return HttpResponse.json({ items: [itemFixture], nextCursor: undefined })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '0',
          dims: [],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )
    const { user } = renderWithProviders(<FeedbackRoutePage />)
    await waitFor(() => {
      expect(screen.getByText('反馈列表暂时无法加载')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '重新加载' }))
    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
    expect(screen.queryByText('还没有反馈')).toBeNull()
  })

  it('filtered zero-state stays a search refinement state instead of onboarding', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        const url = new URL(request.url)
        const q = url.searchParams.get('q') ?? ''
        return HttpResponse.json({
          items: q ? [] : [itemFixture],
          nextCursor: undefined,
        })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })

    await user.type(screen.getByRole('searchbox'), 'no-hit')

    await waitFor(() => {
      expect(screen.getByText('当前筛选没有命中反馈')).toBeInTheDocument()
    })
    expect(screen.getAllByRole('button', { name: '清空筛选' }).length).toBeGreaterThanOrEqual(1)
    expect(screen.queryByText('还没有反馈')).toBeNull()
    expect(screen.queryByText('先签发 API key')).toBeNull()

    await user.click(screen.getAllByRole('button', { name: '清空筛选' })[0])
    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
  })

  it('quick urgent scope narrows the queue to urgent feedback only', async () => {
    const calmItem = {
      ...itemFixture,
      id: '102',
      enrichedDisplayTitle: '普通反馈',
      enrichedTitle: 'Normal feedback',
      content: 'just a normal suggestion',
      isUrgent: false,
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        const url = new URL(request.url)
        const urgent = url.searchParams.get('urgent')
        return HttpResponse.json({
          items: urgent === 'true' ? [itemFixture] : [itemFixture, calmItem],
          nextCursor: undefined,
        })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('普通反馈')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '仅看紧急' }))

    await waitFor(() => {
      expect(screen.queryByText('普通反馈')).toBeNull()
    })
    expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    expect(screen.getByText('仅查看紧急反馈')).toBeInTheDocument()
  })

  it('triage order can reprioritize the visible queue', async () => {
    const activeItem = {
      ...itemFixture,
      id: '102',
      enrichedDisplayTitle: '处理中反馈',
      enrichedTitle: 'Active feedback',
      createdAt: '2026-06-06T08:30:00Z',
      isUrgent: false,
      workflowState: {
        id: 'ws-1',
        name: 'in_progress',
        displayName: { entries: { default: 'In Progress', zh: '处理中' } },
        color: '#f59e0b',
        category: 'active',
        position: 1,
        isDefault: false,
        archived: false,
        createdAt: '2026-06-07T00:00:00Z',
        updatedAt: '2026-06-07T00:00:00Z',
      },
    }
    const newestItem = {
      ...itemFixture,
      id: '103',
      enrichedDisplayTitle: '最新普通反馈',
      enrichedTitle: 'Newest calm feedback',
      createdAt: '2026-06-08T08:30:00Z',
      isUrgent: false,
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture, activeItem, newestItem], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '3',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '3' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('最新普通反馈')).toBeInTheDocument()
    })

    const rowButtonsBefore = screen
      .getAllByRole('button')
      .filter((button) => button.textContent?.includes('#') && button.textContent?.includes('/'))
    expect(rowButtonsBefore[0]).toHaveTextContent('最新普通反馈')

    await user.click(screen.getByRole('combobox', { name: '分诊顺序' }))
    await user.click(screen.getByRole('option', { name: '紧急优先' }))

    const rowButtonsUrgent = screen
      .getAllByRole('button')
      .filter((button) => button.textContent?.includes('#') && button.textContent?.includes('/'))
    expect(rowButtonsUrgent[0]).toHaveTextContent('Unicode 密码登录失败')

    await user.click(screen.getByRole('combobox', { name: '分诊顺序' }))
    await user.click(screen.getByRole('option', { name: '处理中优先' }))

    const rowButtonsActive = screen
      .getAllByRole('button')
      .filter((button) => button.textContent?.includes('#') && button.textContent?.includes('/'))
    expect(rowButtonsActive[0]).toHaveTextContent('处理中反馈')
  })

  it('non-default triage order appears as a removable active chip', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('combobox', { name: '分诊顺序' }))
    await user.click(screen.getByRole('option', { name: '紧急优先' }))

    await waitFor(() => {
      expect(screen.getByLabelText('分诊顺序 紧急优先')).toBeInTheDocument()
    })

    await user.click(screen.getByLabelText('分诊顺序 紧急优先'))

    await waitFor(() => {
      expect(screen.queryByLabelText('分诊顺序 紧急优先')).toBeNull()
    })
  })

  it('queue mode can narrow the visible list to a local failed subqueue', async () => {
    const readyItem = {
      ...itemFixture,
      id: '102',
      enrichedDisplayTitle: '已就绪反馈',
      enrichedTitle: 'Ready feedback',
      isUrgent: false,
    }
    const failedItem = {
      ...itemFixture,
      id: '103',
      enrichedDisplayTitle: '失败反馈',
      enrichedTitle: 'Failed feedback',
      isUrgent: false,
      enrichmentStatus: 'failed',
      classificationConfidence: undefined,
      enrichedAttrs: {},
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture, readyItem, failedItem], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '3',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '2' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('已就绪反馈')).toBeInTheDocument()
      expect(screen.getByText('失败反馈')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '富化失败1' }))

    await waitFor(() => {
      expect(screen.getByText('失败反馈')).toBeInTheDocument()
    })
    expect(screen.queryByText('已就绪反馈')).toBeNull()
    expect(screen.queryByText('Unicode 密码登录失败')).toBeNull()
    expect(screen.getByLabelText('当前子队列 富化失败')).toBeInTheDocument()
    expect(
      screen.getByText('当前视角下共有 1 条反馈，支持批量处理和深入排查。'),
    ).toBeInTheDocument()
    expect(screen.getByText('当前姿态')).toBeInTheDocument()
    expect(screen.getByText('失败排障')).toBeInTheDocument()
    expect(screen.queryByText('紧急优先')).toBeNull()
  })

  it('queue posture stays aligned with the active local queue instead of failed AI status', async () => {
    const activeFailedItem = {
      ...itemFixture,
      id: '102',
      enrichedDisplayTitle: '处理中失败反馈',
      enrichedTitle: 'Active failed feedback',
      isUrgent: false,
      enrichmentStatus: 'failed',
      classificationConfidence: undefined,
      enrichedAttrs: {},
      workflowState: {
        id: 'ws-1',
        name: 'in_progress',
        displayName: { entries: { default: 'In Progress', zh: '处理中' } },
        color: '#f59e0b',
        category: 'active',
        position: 1,
        isDefault: false,
        archived: false,
        createdAt: '2026-06-07T00:00:00Z',
        updatedAt: '2026-06-07T00:00:00Z',
      },
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [activeFailedItem], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('处理中失败反馈')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '处理中1' }))

    await waitFor(() => {
      expect(screen.getByLabelText('当前子队列 处理中')).toBeInTheDocument()
    })
    expect(screen.getByText('当前姿态')).toBeInTheDocument()
    expect(screen.getByText('处理中推进')).toBeInTheDocument()
    expect(screen.queryByText('失败排障')).toBeNull()
  })

  it('queue mode active chip can be removed to restore the broader local queue', async () => {
    const failedItem = {
      ...itemFixture,
      id: '103',
      enrichedDisplayTitle: '失败反馈',
      enrichedTitle: 'Failed feedback',
      isUrgent: false,
      enrichmentStatus: 'failed',
      classificationConfidence: undefined,
      enrichedAttrs: {},
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture, failedItem], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('失败反馈')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '富化失败1' }))

    await waitFor(() => {
      expect(screen.getByLabelText('当前子队列 富化失败')).toBeInTheDocument()
      expect(screen.queryByText('Unicode 密码登录失败')).toBeNull()
    })

    await user.click(screen.getByLabelText('当前子队列 富化失败'))

    await waitFor(() => {
      expect(screen.queryByLabelText('当前子队列 富化失败')).toBeNull()
    })
    expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
  })

  it('queue mode zero-result state explains when a local subqueue has no matching rows', async () => {
    const activeItem = {
      ...itemFixture,
      id: '102',
      enrichedDisplayTitle: '处理中反馈',
      enrichedTitle: 'Active feedback',
      isUrgent: false,
      workflowState: {
        id: 'ws-1',
        name: 'in_progress',
        displayName: { entries: { default: 'In Progress', zh: '处理中' } },
        color: '#f59e0b',
        category: 'active',
        position: 1,
        isDefault: false,
        archived: false,
        createdAt: '2026-06-07T00:00:00Z',
        updatedAt: '2026-06-07T00:00:00Z',
      },
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture, activeItem], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '2' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('处理中反馈')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '富化失败0' }))

    await waitFor(() => {
      expect(screen.getByText('富化失败 子队列当前没有命中反馈')).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: '回到全部子队列' })).toBeInTheDocument()
    expect(screen.queryByText('处理中反馈')).toBeNull()

    await user.click(screen.getByRole('button', { name: '回到全部子队列' }))

    await waitFor(() => {
      expect(screen.getByText('处理中反馈')).toBeInTheDocument()
    })
  })

  it('queue mode updates recommendation copy and direct action to match the active work surface', async () => {
    const activeItem = {
      ...itemFixture,
      id: '102',
      enrichedDisplayTitle: '处理中反馈',
      enrichedTitle: 'Active feedback',
      isUrgent: false,
      workflowState: {
        id: 'ws-1',
        name: 'in_progress',
        displayName: { entries: { default: 'In Progress', zh: '处理中' } },
        color: '#f59e0b',
        category: 'active',
        position: 1,
        isDefault: false,
        archived: false,
        createdAt: '2026-06-07T00:00:00Z',
        updatedAt: '2026-06-07T00:00:00Z',
      },
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture, activeItem], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '2' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('处理中反馈')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '处理中1' }))

    await waitFor(() => {
      expect(screen.getByText('先推进这批处理中反馈')).toBeInTheDocument()
    })
    expect(screen.getAllByRole('button', { name: '打开处理中反馈' }).length).toBeGreaterThanOrEqual(
      1,
    )
    expect(screen.getByText('先推进正在流转的条目')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: '去检查富化运行时' })).toBeNull()
    expect(screen.getByText('处理中推进')).toBeInTheDocument()
    expect(screen.getByText('输出稳定')).toBeInTheDocument()
    expect(screen.queryByText('来源覆盖')).toBeNull()
  })

  it('ai ready lane only counts feedback with usable ai output', async () => {
    const readyItem = {
      ...itemFixture,
      id: '102',
      enrichedDisplayTitle: 'AI 已就绪反馈',
      enrichedTitle: 'Ready feedback',
      isUrgent: false,
    }
    const shallowItem = {
      ...itemFixture,
      id: '103',
      enrichedDisplayTitle: '',
      enrichedTitle: '',
      classificationConfidence: undefined,
      enrichedAttrs: {},
      enrichmentStatus: 'done',
      isUrgent: false,
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [readyItem, shallowItem], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('AI 已就绪反馈')).toBeInTheDocument()
    })

    expect(screen.getByRole('button', { name: 'AI 已就绪1' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'AI 已就绪1' }))

    await waitFor(() => {
      expect(screen.getByText('AI 已就绪反馈')).toBeInTheDocument()
    })
    expect(screen.queryByText('#103')).toBeNull()
  })

  it('runs semantic search with supported feedback filters and uses the returned working set', async () => {
    let semanticRequest: unknown
    let searchEventRequest: unknown
    const semanticItem = {
      ...itemFixture,
      id: '301',
      content: 'customers cannot finish checkout after choosing invoice billing',
      enrichedTitle: 'Invoice checkout failure',
      enrichedDisplayTitle: '发票结账失败',
      isUrgent: true,
      tags: [],
      allowedNextStates: [],
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.post('/fb/v1/console/feedback/search', async ({ request }) => {
        semanticRequest = await request.json()
        return HttpResponse.json({
          hits: [
            {
              feedback: semanticItem,
              similarity: 0.91,
              keywordScore: 0.3,
              matchType: 'hybrid',
              semanticRank: 1,
              lexicalRank: 2,
              fusedScore: 0.0161,
              evidence: [
                {
                  field: 'content',
                  snippet: 'refund blocker needs support review',
                  reason: 'lexical_match',
                },
              ],
              rankingSignals: ['semantic', 'lexical', 'rrf'],
            },
            {
              feedback: {},
              similarity: 0.12,
              keywordScore: 0,
              matchType: 'semantic',
              semanticRank: 2,
              lexicalRank: 0,
              fusedScore: 0.001,
              evidence: [],
              rankingSignals: ['semantic'],
            },
          ],
          runId: 'semantic-run-1',
          embeddingModel: 'text-embedding-3-small',
          totalWithEmbeddings: 12,
          usedKeywordFallback: false,
          rankingVersion: 'rrf.pgfts.v1.k60',
          coverage: {
            totalLiveFeedback: 18,
            totalWithEmbeddings: 12,
            embeddingModel: 'text-embedding-3-small',
          },
        })
      }),
      http.post('/fb/v1/console/feedback/search/events', async ({ request }) => {
        searchEventRequest = await request.json()
        return HttpResponse.json({ ok: true })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/feedback/:id', ({ params }) =>
        HttpResponse.json({ ...detailFixture, ...semanticItem, id: params.id }),
      ),
      http.get('/fb/v1/console/feedback/:id/audit', ({ params }) =>
        HttpResponse.json({ entries: [], feedbackId: params.id }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '仅看紧急' }))
    await user.click(screen.getByLabelText('所有 Severity'))
    await user.click(screen.getByRole('option', { name: 'P0' }))
    await user.click(screen.getByRole('button', { name: '语义' }))
    await user.type(screen.getByRole('searchbox', { name: '搜索反馈内容' }), 'billing checkout')
    await user.click(screen.getByRole('button', { name: '运行语义搜索' }))

    await waitFor(() => {
      expect(screen.getByText('发票结账失败')).toBeInTheDocument()
    })

    const body = semanticRequest as {
      q?: string
      limit?: number
      filter?: { attrs?: Array<{ dim: string; value: string; multi: boolean }>; urgent?: boolean }
    }
    expect(body.q).toBe('billing checkout')
    expect(body.limit).toBe(50)
    expect(body.filter?.urgent).toBe(true)
    expect(body.filter?.attrs).toEqual([{ dim: 'severity', value: 'P0', multi: false }])
    expect(screen.queryByText('Unicode 密码登录失败')).toBeNull()
    expect(
      screen.getByText('语义搜索返回 2 条结果，当前租户有 12 条反馈带向量。'),
    ).toBeInTheDocument()
    expect(screen.getByText('排序 rrf.pgfts.v1.k60')).toBeInTheDocument()
    expect(screen.getByText('匹配依据')).toBeInTheDocument()
    expect(screen.getByText('refund blocker needs support review')).toBeInTheDocument()
    expect(screen.getByTitle(/混合匹配/)).toHaveTextContent('混合匹配 91%')

    await user.click(screen.getByText('发票结账失败'))
    await waitFor(() => {
      expect(searchEventRequest).toEqual({
        runId: 'semantic-run-1',
        feedbackId: '301',
        action: 'open',
        rank: 1,
        matchType: 'hybrid',
      })
    })
  })

  it('keeps terminal failure scope when running semantic search from the terminal queue', async () => {
    let semanticRequest: unknown

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        const url = new URL(request.url)
        const terminalOnly = url.searchParams.get('terminal_failed_only') === 'true'
        return HttpResponse.json({
          items: terminalOnly ? [terminalItemFixture] : [itemFixture],
          nextCursor: undefined,
        })
      }),
      http.post('/fb/v1/console/feedback/search', async ({ request }) => {
        semanticRequest = await request.json()
        return HttpResponse.json({
          hits: [
            {
              feedback: { ...terminalItemFixture, tags: [], allowedNextStates: [] },
              similarity: 0.84,
              keywordScore: 0.1,
              matchType: 'semantic',
              semanticRank: 1,
              lexicalRank: 0,
              fusedScore: 0.0115,
              evidence: [
                {
                  field: 'content',
                  snippet: 'LLM exhausted retries for this feedback',
                  reason: 'vector_similarity',
                },
              ],
              rankingSignals: ['semantic', 'rrf'],
            },
          ],
          embeddingModel: 'text-embedding-3-small',
          totalWithEmbeddings: 4,
          usedKeywordFallback: false,
          rankingVersion: 'rrf.pgfts.v1.k60',
          coverage: {
            totalLiveFeedback: 6,
            totalWithEmbeddings: 4,
            embeddingModel: 'text-embedding-3-small',
          },
        })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage initialQueueMode="terminal" />)

    await waitFor(() => {
      expect(screen.getByText('终态失败样本')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '语义' }))
    await user.type(screen.getByRole('searchbox', { name: '搜索反馈内容' }), 'llm exhausted')
    await user.click(screen.getByRole('button', { name: '运行语义搜索' }))

    await waitFor(() => {
      expect(semanticRequest).toBeTruthy()
    })

    const body = semanticRequest as {
      q?: string
      filter?: { enrichmentStatus?: string; terminalFailedOnly?: boolean }
    }
    expect(body.q).toBe('llm exhausted')
    expect(body.filter?.enrichmentStatus).toBe('failed')
    expect(body.filter?.terminalFailedOnly).toBe(true)
  })

  it('does not submit semantic search without a query', async () => {
    let searchCalls = 0

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.post('/fb/v1/console/feedback/search', () => {
        searchCalls += 1
        return HttpResponse.json({ hits: [] })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '语义' }))

    const searchbox = screen.getByRole('searchbox', { name: '搜索反馈内容' })
    const form = searchbox.closest('form')
    expect(form).not.toBeNull()
    fireEvent.submit(form as HTMLFormElement)

    expect(searchCalls).toBe(0)
    expect(screen.getByText('输入一句自然语言问题，再运行语义搜索。')).toBeInTheDocument()
  })

  it('keeps semantic results marked stale after the query changes', async () => {
    const staleItem = {
      ...itemFixture,
      id: '303',
      content: 'customers need billing export',
      enrichedTitle: 'Billing export request',
      enrichedDisplayTitle: '账单导出需求',
      isUrgent: false,
      tags: [],
      allowedNextStates: [],
    }
    let releaseSearch: (() => void) | undefined

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.post('/fb/v1/console/feedback/search', async () => {
        await new Promise<void>((resolve) => {
          releaseSearch = resolve
        })
        return HttpResponse.json({
          hits: [
            {
              feedback: staleItem,
              similarity: 0.88,
              keywordScore: 0.2,
              matchType: 'semantic',
              semanticRank: 1,
              lexicalRank: 0,
              fusedScore: 0.0137,
              evidence: [],
              rankingSignals: ['semantic', 'rrf'],
            },
          ],
          embeddingModel: 'text-embedding-3-small',
          totalWithEmbeddings: 9,
          usedKeywordFallback: false,
          rankingVersion: 'rrf.pgfts.v1.k60',
          coverage: {
            totalLiveFeedback: 12,
            totalWithEmbeddings: 9,
            embeddingModel: 'text-embedding-3-small',
          },
        })
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '语义' }))
    await user.type(screen.getByRole('searchbox', { name: '搜索反馈内容' }), 'billing export')
    await user.click(screen.getByRole('button', { name: '运行语义搜索' }))

    expect(await screen.findByText('正在进行语义搜索...')).toBeInTheDocument()
    releaseSearch?.()

    await waitFor(() => {
      expect(screen.getByText('账单导出需求')).toBeInTheDocument()
    })

    await user.type(screen.getByRole('searchbox', { name: '搜索反馈内容' }), ' later')

    await waitFor(() => {
      expect(screen.getByText('查询或筛选已变化，请重新运行语义搜索。')).toBeInTheDocument()
    })
    expect(screen.getAllByRole('button', { name: '运行语义搜索' }).length).toBeGreaterThanOrEqual(1)
  })

  it('shows keyword fallback state when semantic search degrades', async () => {
    const fallbackItem = {
      ...itemFixture,
      id: '302',
      content: 'billing keyword fallback result',
      enrichedTitle: 'Billing keyword result',
      enrichedDisplayTitle: '账单关键词结果',
      isUrgent: false,
      tags: [],
      allowedNextStates: [],
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.post('/fb/v1/console/feedback/search', () =>
        HttpResponse.json({
          hits: [
            {
              feedback: fallbackItem,
              similarity: 0,
              keywordScore: 0.82,
              matchType: 'keyword',
              semanticRank: 0,
              lexicalRank: 1,
              fusedScore: 0.0049,
              evidence: [
                {
                  field: 'content',
                  snippet: 'billing keyword fallback result',
                  reason: 'lexical_match',
                },
              ],
              rankingSignals: ['lexical', 'rrf', 'keyword_fallback'],
            },
          ],
          embeddingModel: '',
          totalWithEmbeddings: 0,
          usedKeywordFallback: true,
          fallbackReason: 'no_embeddings',
          rankingVersion: 'rrf.pgfts.v1.k60',
          coverage: {
            totalLiveFeedback: 2,
            totalWithEmbeddings: 0,
            embeddingModel: '',
          },
        }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '语义' }))
    await user.type(screen.getByRole('searchbox', { name: '搜索反馈内容' }), 'billing')
    await user.click(screen.getByRole('button', { name: '运行语义搜索' }))

    await waitFor(() => {
      expect(screen.getByText('账单关键词结果')).toBeInTheDocument()
    })

    expect(screen.getByText('语义搜索已降级')).toBeInTheDocument()
    expect(screen.getByText('使用关键词搜索')).toBeInTheDocument()
    expect(screen.getByText('排序 rrf.pgfts.v1.k60')).toBeInTheDocument()
    expect(screen.getAllByText('当前租户还没有可用向量')).toHaveLength(2)
    expect(screen.getByTitle(/关键词匹配/)).toHaveTextContent('关键词匹配 82%')
  })

  it('loads the next feedback page from the queue footer', async () => {
    const seenCursors: Array<string | null> = []
    const secondItem = {
      ...itemFixture,
      id: '102',
      enrichedDisplayTitle: '第二页反馈',
      enrichedTitle: 'Second page feedback',
      isUrgent: false,
      createdAt: '2026-06-06T08:30:00Z',
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', ({ request }) => {
        const cursor = new URL(request.url).searchParams.get('cursor')
        seenCursors.push(cursor)
        return HttpResponse.json(
          cursor === 'cur-2'
            ? { items: [secondItem], nextCursor: null }
            : { items: [itemFixture], nextCursor: 'cur-2' },
        )
      }),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '加载更多' }))

    await waitFor(() => {
      expect(screen.getByText('第二页反馈')).toBeInTheDocument()
    })
    expect(seenCursors).toEqual([null, 'cur-2'])
  })

  it('wires selected-row tag updates and workflow transitions to batch mutations', async () => {
    const assignedTag = {
      id: 'tag-old',
      name: 'Existing',
      color: '#64748b',
      archived: false,
    }
    const newTag = {
      id: 'tag-new',
      name: 'Escalation',
      color: '#ef4444',
      archived: false,
    }
    const workflowState = {
      id: 'ws-2',
      name: 'review',
      displayName: { entries: { default: 'Review', zh: '待处理' } },
      color: '#3b82f6',
      category: 'active',
      position: 1,
      isDefault: false,
      archived: false,
      createdAt: '2026-06-07T00:00:00Z',
      updatedAt: '2026-06-07T00:00:00Z',
    }
    const batchBodies: unknown[] = []
    let transitionBody: unknown

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({
          items: [{ ...itemFixture, tags: [assignedTag] }],
          nextCursor: undefined,
        }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () =>
        HttpResponse.json({ states: [workflowState] }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [assignedTag, newTag] })),
      http.post('/fb/v1/console/feedback/batch', async ({ request }) => {
        batchBodies.push(await request.json())
        return HttpResponse.json({ succeeded: 1, failed: [] })
      }),
      http.post('/fb/v1/console/feedback/transition/batch', async ({ request }) => {
        transitionBody = await request.json()
        return HttpResponse.json({ succeeded: 1, failed: [] })
      }),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })

    await user.click(screen.getByLabelText('选择 Unicode 密码登录失败'))
    await user.click(screen.getByRole('button', { name: '添加标签' }))
    await user.click(screen.getByRole('option', { name: 'Escalation' }))
    await waitFor(() => expect(batchBodies).toHaveLength(1))

    await user.click(screen.getByLabelText('选择 Unicode 密码登录失败'))
    await user.click(screen.getByRole('button', { name: '移除标签' }))
    await user.click(screen.getByRole('option', { name: 'Existing' }))
    await waitFor(() => expect(batchBodies).toHaveLength(2))

    await user.click(screen.getByLabelText('选择 Unicode 密码登录失败'))
    const transitionTrigger = screen.getByText('流转状态').closest('[role="combobox"]')
    expect(transitionTrigger).not.toBeNull()
    await user.click(transitionTrigger as HTMLElement)
    await user.click(screen.getByRole('option', { name: '待处理' }))
    await waitFor(() => expect(transitionBody).toBeTruthy())

    await user.click(screen.getByLabelText('选择 Unicode 密码登录失败'))
    await user.click(screen.getByRole('button', { name: '从反馈提升' }))
    expect(navigateMock).toHaveBeenCalledWith({
      to: '/feedback/customer-requests',
      search: {
        request_id: undefined,
        merge_target_id: undefined,
        promote_feedback_ids: '101',
        feedback_id: undefined,
      },
    })

    expect(batchBodies).toEqual([
      {
        feedbackIds: ['101'],
        dryRun: false,
        operation: { tag: { addTagIds: ['tag-new'], removeTagIds: [] } },
      },
      {
        feedbackIds: ['101'],
        dryRun: false,
        operation: { tag: { addTagIds: [], removeTagIds: ['tag-old'] } },
      },
    ])
    expect(transitionBody).toEqual({
      feedbackIds: ['101'],
      toStateId: 'ws-2',
      comment: '',
    })
  })

  it('surfaces batch mutation errors without clearing the current selection', async () => {
    const assignedTag = {
      id: 'tag-old',
      name: 'Existing',
      color: '#64748b',
      archived: false,
    }
    const newTag = {
      id: 'tag-new',
      name: 'Escalation',
      color: '#ef4444',
      archived: false,
    }
    const workflowState = {
      id: 'ws-2',
      name: 'review',
      displayName: { entries: { default: 'Review', zh: '待处理' } },
      color: '#3b82f6',
      category: 'active',
      position: 1,
      isDefault: false,
      archived: false,
      createdAt: '2026-06-07T00:00:00Z',
      updatedAt: '2026-06-07T00:00:00Z',
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({
          items: [{ ...itemFixture, tags: [assignedTag] }],
          nextCursor: undefined,
        }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () =>
        HttpResponse.json({ states: [workflowState] }),
      ),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [assignedTag, newTag] })),
      http.post('/fb/v1/console/feedback/batch', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'batch exploded' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/feedback/transition/batch', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'transition exploded' }, { status: 500 }),
      ),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
    await user.click(screen.getByLabelText('选择 Unicode 密码登录失败'))

    await user.click(screen.getByRole('button', { name: '添加标签' }))
    await user.click(screen.getByRole('option', { name: 'Escalation' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('batch exploded'))

    await user.click(screen.getByRole('button', { name: '移除标签' }))
    await user.click(screen.getByRole('option', { name: 'Existing' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('batch exploded'))

    const transitionTrigger = screen.getByText('流转状态').closest('[role="combobox"]')
    expect(transitionTrigger).not.toBeNull()
    await user.click(transitionTrigger as HTMLElement)
    await user.click(screen.getByRole('option', { name: '待处理' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('transition exploded'))

    await user.click(screen.getByRole('button', { name: '删除' }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '删除' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('batch exploded'))
  })

  it('confirms selected feedback deletion through the page dialog', async () => {
    let deleteBody: unknown

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.post('/fb/v1/console/feedback/batch', async ({ request }) => {
        deleteBody = await request.json()
        return HttpResponse.json({ affected_count: 1 })
      }),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
    await user.click(screen.getByLabelText('选择 Unicode 密码登录失败'))
    await user.click(screen.getByRole('button', { name: '删除' }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('永久删除反馈')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull()
    })

    await user.click(screen.getByRole('button', { name: '删除' }))
    const confirmDialog = await screen.findByRole('dialog')
    await user.click(within(confirmDialog).getByRole('button', { name: '删除' }))

    await waitFor(() => {
      expect(deleteBody).toEqual({
        feedback_ids: [101],
        operation: { delete: {} },
      })
    })
  })

  it('confirms selected terminal failure retry through the page dialog', async () => {
    let retriedId = ''

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [terminalItemFixture], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.post('/fb/v1/console/feedback/:id/retry-enrichment', ({ params }) => {
        retriedId = String(params.id)
        return HttpResponse.json({
          id: params.id,
          enrichmentStatus: 'pending',
          enrichmentAttempts: 0,
        })
      }),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('终态失败样本')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '查看终态失败' }))
    await waitFor(() => {
      expect(screen.getByLabelText('当前子队列 终态失败')).toBeInTheDocument()
    })
    await user.click(screen.getByLabelText('选择 终态失败样本'))
    await user.click(screen.getByRole('button', { name: '重试富化' }))

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('重试 AI 富化')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).toBeNull()
    })

    await user.click(screen.getByRole('button', { name: '重试富化' }))
    const confirmDialog = await screen.findByRole('dialog')
    await user.click(within(confirmDialog).getByRole('button', { name: '重试' }))

    await waitFor(() => {
      expect(retriedId).toBe('201')
    })
  })

  it('reports partial batch retry failures for selected terminal failures', async () => {
    const secondTerminalItem = {
      ...terminalItemFixture,
      id: '202',
      enrichedDisplayTitle: '第二个终态失败',
      enrichedTitle: 'Second terminal failure',
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({
          items: [terminalItemFixture, secondTerminalItem],
          nextCursor: undefined,
        }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '2',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.post('/fb/v1/console/feedback/:id/retry-enrichment', ({ params }) => {
        if (params.id === '202') {
          return HttpResponse.json({ code: 'INTERNAL', message: 'retry failed' }, { status: 500 })
        }
        return HttpResponse.json({
          id: params.id,
          enrichmentStatus: 'pending',
          enrichmentAttempts: 0,
        })
      }),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('终态失败样本')).toBeInTheDocument()
      expect(screen.getByText('第二个终态失败')).toBeInTheDocument()
    })
    await user.click(screen.getByLabelText('选择 终态失败样本'))
    await user.click(screen.getByLabelText('选择 第二个终态失败'))
    await user.click(screen.getByRole('button', { name: /重试富化/ }))

    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '重试' }))

    await waitFor(() => expect(toast.warning).toHaveBeenCalledWith('1 条成功，1 条失败'))
  })

  it('reports full batch retry failure when every selected terminal retry fails', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [terminalItemFixture], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.post('/fb/v1/console/feedback/:id/retry-enrichment', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'retry failed' }, { status: 500 }),
      ),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('终态失败样本')).toBeInTheDocument()
    })
    await user.click(screen.getByLabelText('选择 终态失败样本'))
    await user.click(screen.getByRole('button', { name: '重试富化' }))

    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '重试' }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('重试失败'))
  })

  it('shows retry schedule and error preview for non-terminal enrichment failures', async () => {
    const retryingFailure = {
      ...itemFixture,
      id: '203',
      content: 'transient enrichment failure',
      enrichedTitle: 'Transient enrichment failure',
      enrichedDisplayTitle: '可自动重试失败',
      enrichmentStatus: 'failed',
      enrichmentAttempts: 2,
      enrichmentNextRetryAt: '2099-06-07T10:00:00Z',
      enrichmentError:
        'provider returned a transient timeout while generating structured feedback attributes and should be retried automatically with the same routing policy',
      enrichedAttrs: { severity: ['P0'] },
      isUrgent: false,
    }

    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [retryingFailure], nextCursor: undefined }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '0',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('可自动重试失败')).toBeInTheDocument()
    })
    expect(screen.getByTitle(/等待下次自动重试/)).toHaveAttribute(
      'title',
      expect.stringContaining('下次重试：'),
    )
    expect(screen.getByTitle(/provider returned a transient timeout/)).toHaveAttribute(
      'title',
      expect.stringContaining('...'),
    )
  })

  it('shows a retryable semantic-search error state', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.post('/fb/v1/console/feedback/search', () =>
        HttpResponse.json({ code: 'BAD_REQUEST', message: 'semantic exploded' }, { status: 400 }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '语义' }))
    await user.type(screen.getByRole('searchbox', { name: '搜索反馈内容' }), 'billing')
    await user.click(screen.getByRole('button', { name: '运行语义搜索' }))

    await waitFor(() => {
      expect(screen.getAllByText('semantic exploded').length).toBeGreaterThanOrEqual(1)
    })
    expect(screen.getByText('反馈列表暂时无法加载')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '重新加载' }).length).toBeGreaterThanOrEqual(1)
  })

  it('shows the semantic-search empty state for a successful zero-hit response', async () => {
    server.use(
      http.get('/fb/v1/console/enrich-config', () =>
        HttpResponse.json({
          config: { promptTemplate: '', defaultPromptTemplate: '', dimensions: dimsFixture },
        }),
      ),
      http.get('/fb/v1/console/feedback', () =>
        HttpResponse.json({ items: [itemFixture], nextCursor: undefined }),
      ),
      http.post('/fb/v1/console/feedback/search', () =>
        HttpResponse.json({
          hits: [],
          embeddingModel: 'text-embedding-3-small',
          totalWithEmbeddings: 12,
          usedKeywordFallback: false,
          rankingVersion: 'rrf.pgfts.v1.k60',
          coverage: {
            totalLiveFeedback: 18,
            totalWithEmbeddings: 12,
            embeddingModel: 'text-embedding-3-small',
          },
        }),
      ),
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          periodStart: '',
          periodEnd: '',
          total: '1',
          dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
          urgentCount: '1',
        }),
      ),
      http.get('/fb/v1/console/workflow/states', () => HttpResponse.json({ states: [] })),
      http.get('/fb/v1/console/tags', () => HttpResponse.json({ tags: [] })),
      http.get('/fb/v1/console/clusters', () =>
        HttpResponse.json({ items: [], clusteringEnabled: false, totalCount: 0 }),
      ),
    )

    const { user } = renderWithProviders(<FeedbackRoutePage />)

    await waitFor(() => {
      expect(screen.getByText('Unicode 密码登录失败')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '语义' }))
    await user.type(screen.getByRole('searchbox', { name: '搜索反馈内容' }), 'no semantic match')
    await user.click(screen.getByRole('button', { name: '运行语义搜索' }))

    await waitFor(() => {
      expect(screen.getByText('未找到语义匹配')).toBeInTheDocument()
    })
    expect(
      screen.getByText('语义搜索返回 0 条结果，当前租户有 12 条反馈带向量。'),
    ).toBeInTheDocument()
    expect(screen.queryByText('Unicode 密码登录失败')).toBeNull()
  })
})
