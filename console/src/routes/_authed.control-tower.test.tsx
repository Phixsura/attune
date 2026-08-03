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
import { defaultInboundSources, defaultMe, defaultSurveyAnalytics } from '@/testing/mocks/handlers'
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

function closedLoopScorecardFixture(
  override: Partial<Parameters<typeof controlTowerPageTestables.closedLoopReadinessStatus>[0]> = {},
): Parameters<typeof controlTowerPageTestables.closedLoopReadinessStatus>[0] {
  return {
    available: true,
    criticalReviews: 0,
    invitations: 10,
    missingActionRecoveryQueue: 0,
    missingRootCauseRecoveryQueue: 0,
    oldestOpenLowScoreReviewDueAt: '',
    openReviews: 1,
    overdueRecoveryQueue: 0,
    overdueReviews: 0,
    ownerLoads: [],
    pendingContactRecoveryQueue: 0,
    pendingContactReviews: 0,
    readiness: 80,
    responseRate: 0.33,
    severity: 'normal',
    unassignedRecoveryQueue: 0,
    unassignedReviews: 0,
    ...override,
  }
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
    expect(controlTowerPageTestables.recoveryReadinessScore(defaultSurveyAnalytics)).toBe(54)
    expect(controlTowerPageTestables.roleCanReadSurveyAnalytics('admin')).toBe(true)
    expect(controlTowerPageTestables.roleCanReadSurveyAnalytics('delegated_admin')).toBe(true)
    expect(controlTowerPageTestables.roleCanReadSurveyAnalytics('viewer')).toBe(false)
    expect(controlTowerPageTestables.roleCanReadInboundSources('admin')).toBe(true)
    expect(controlTowerPageTestables.roleCanReadInboundSources('member')).toBe(true)
    expect(controlTowerPageTestables.roleCanReadInboundSources('viewer')).toBe(false)
    expect(controlTowerPageTestables.firstValueSourceStatus(false, undefined, 0, 0)).toBe(
      'insufficient_data',
    )
    expect(controlTowerPageTestables.firstValueSourceStatus(true, [], 0, 0)).toBe('blocked')
    expect(
      controlTowerPageTestables.firstValueSourceStatus(true, defaultInboundSources, 1, 0),
    ).toBe('pass')
    expect(
      controlTowerPageTestables.firstValueSourceStatus(true, defaultInboundSources, 1, 1),
    ).toBe('watch')
    const sourceHealthNow = new Date('2026-07-12T12:00:00Z')
    expect(controlTowerPageTestables.sourceIsFresh('2026-07-10T12:00:00Z', sourceHealthNow)).toBe(
      true,
    )
    expect(controlTowerPageTestables.sourceIsFresh('2026-07-10T11:59:59Z', sourceHealthNow)).toBe(
      false,
    )
    expect(
      controlTowerPageTestables.sourceHealthStatus(defaultInboundSources[0], sourceHealthNow),
    ).toBe('pass')
    expect(
      controlTowerPageTestables.sourceHealthStatus(
        {
          ...defaultInboundSources[0],
          lastError: 'invalid signature',
        },
        sourceHealthNow,
      ),
    ).toBe('blocked')
    expect(
      controlTowerPageTestables.sourceHealthCommandStatus({
        active: 1,
        disabled: 0,
        errors: 0,
        neverSeen: 1,
        stale: 0,
        total: 1,
      }),
    ).toBe('blocked')
    expect(
      controlTowerPageTestables.sourceHealthNextActionKey({
        active: 1,
        disabled: 0,
        errors: 0,
        neverSeen: 0,
        stale: 1,
        total: 1,
      }),
    ).toBe('control_tower.source_health.next.refresh_stale')
    const sourceHealth = controlTowerPageTestables.buildSourceHealthCommandCenter({
      canReadInboundSources: true,
      inboundSources: [
        defaultInboundSources[0],
        {
          ...defaultInboundSources[0],
          id: 'source-error',
          lastError: 'token expired',
          name: 'Broken source',
        },
        {
          ...defaultInboundSources[0],
          enabled: false,
          id: 'source-disabled',
          lastError: '',
          name: 'Paused source',
        },
      ],
      now: sourceHealthNow,
    })
    expect(sourceHealth.status).toBe('blocked')
    expect(sourceHealth.nextActionKey).toBe('control_tower.source_health.next.fix_errors')
    expect(sourceHealth.problems.map((problem) => problem.id)).toEqual(['errors', 'disabled'])
    expect(sourceHealth.sources.map((source) => source.name)).toEqual([
      'Broken source',
      'Paused source',
      'Default webhook',
    ])
    expect(controlTowerPageTestables.signalQualityReadinessStatus(0, 0, 0, 0)).toBe(
      'insufficient_data',
    )
    expect(controlTowerPageTestables.signalQualityReadinessStatus(100, 1, 0, 0)).toBe('blocked')
    expect(controlTowerPageTestables.signalQualityReadinessStatus(100, 0, 0.06, 0)).toBe('watch')
    expect(controlTowerPageTestables.semanticDiscoveryReadinessStatus(0, 1, 0, 0, 100)).toBe(
      'insufficient_data',
    )
    expect(controlTowerPageTestables.semanticDiscoveryReadinessStatus(100, 0.75, 0, 0, 100)).toBe(
      'blocked',
    )
    expect(controlTowerPageTestables.semanticDiscoveryReadinessStatus(100, 0.9, 0, 0, 100)).toBe(
      'watch',
    )
    expect(
      controlTowerPageTestables.closedLoopReadinessStatus(
        closedLoopScorecardFixture({
          overdueReviews: 1,
          readiness: 54,
          severity: 'alert',
        }),
      ),
    ).toBe('blocked')
    expect(
      controlTowerPageTestables.firstValueClosedLoopStatus(
        closedLoopScorecardFixture({
          invitations: 0,
          openReviews: 0,
          readiness: 100,
          responseRate: 0,
        }),
      ),
    ).toBe('watch')
    expect(controlTowerPageTestables.formatDateTime('not-a-date')).toBe('not-a-date')
    expect(controlTowerPageTestables.formatDateTime('2026-07-30T01:00:00Z')).toContain('2026')
    expect(
      controlTowerPageTestables.recoveryCommandStatus({
        available: false,
        evidenceDebt: 0,
        invitations: 0,
        openReviews: 0,
        overdue: 0,
        pendingContact: 0,
        unassigned: 0,
      }),
    ).toBe('insufficient_data')
    expect(
      controlTowerPageTestables.recoveryCommandStatus({
        available: true,
        evidenceDebt: 0,
        invitations: 1,
        openReviews: 0,
        overdue: 1,
        pendingContact: 0,
        unassigned: 0,
      }),
    ).toBe('blocked')
    expect(
      controlTowerPageTestables.recoveryCommandStatus({
        available: true,
        evidenceDebt: 1,
        invitations: 1,
        openReviews: 0,
        overdue: 0,
        pendingContact: 0,
        unassigned: 0,
      }),
    ).toBe('watch')
    expect(
      controlTowerPageTestables.recoveryCommandNextActionKey({
        evidenceDebt: 2,
        invitations: 1,
        openReviews: 1,
        overdue: 0,
        pendingContact: 0,
        unassigned: 0,
      }),
    ).toBe('control_tower.recovery_command.next.document_evidence')
    const recoveryCommand = controlTowerPageTestables.buildRecoveryCommandCenter(
      closedLoopScorecardFixture({
        missingActionRecoveryQueue: 1,
        overdueRecoveryQueue: 1,
        ownerLoads: [
          {
            critical: 1,
            dueSoon: 1,
            oldestDueAt: '2026-07-30T01:00:00Z',
            open: 3,
            overdue: 1,
            ownerMemberId: 'owner-a',
            pendingContact: 2,
            workload: 91,
          },
        ],
        pendingContactReviews: 1,
        unassignedReviews: 1,
      }),
    )
    expect(recoveryCommand.status).toBe('blocked')
    expect(recoveryCommand.nextActionKey).toBe(
      'control_tower.recovery_command.next.resolve_overdue',
    )
    expect(recoveryCommand.blockers.map((blocker) => blocker.id)).toEqual([
      'overdue',
      'unassigned',
      'pending-contact',
      'missing-action',
    ])
    expect(recoveryCommand.totals).toMatchObject({
      dueSoon: 1,
      evidenceDebt: 1,
      ownerCount: 1,
      overdue: 1,
      pendingContact: 1,
      unassigned: 1,
    })
    expect(
      controlTowerPageTestables.actionAccountabilityReadinessStatus([
        { actionKey: 'risk' },
      ] as Parameters<typeof controlTowerPageTestables.actionAccountabilityReadinessStatus>[0]),
    ).toBe('blocked')
    expect(
      controlTowerPageTestables.firstValueReleaseStatus({
        items: [],
        passed: 0,
        total: 0,
      }),
    ).toBe('insufficient_data')
    expect(
      controlTowerPageTestables.firstValueReleaseStatus({
        items: [],
        passed: 0,
        total: 5,
      }),
    ).toBe('blocked')
    expect(
      controlTowerPageTestables.releaseVerificationStatusBlocksRelease('insufficient_data'),
    ).toBe(true)
    expect(
      controlTowerPageTestables.releaseVerificationCommandStatus({
        blocked: 1,
        unresolvedRisks: 0,
        watch: 0,
      }),
    ).toBe('blocked')
    expect(
      controlTowerPageTestables.releaseVerificationCommandStatus({
        blocked: 0,
        unresolvedRisks: 1,
        watch: 1,
      }),
    ).toBe('watch')
    expect(
      controlTowerPageTestables.releaseVerificationNextActionKey({
        blocked: 0,
        unresolvedRisks: 0,
        watch: 0,
      }),
    ).toBe('control_tower.release_verification.next.attach_ci')
    const releaseVerification = controlTowerPageTestables.buildReleaseVerificationCommandCenter({
      firstValue: {
        items: [],
        passed: 3,
        total: 5,
      },
      readinessItems: [
        {
          evidenceKey: 'evidence',
          evidenceValues: {},
          gapKey: 'gap',
          id: 'runtime-gap',
          standardKey: 'standard',
          status: 'insufficient_data',
          titleKey: 'title',
        },
      ],
      recoveryCommand,
      risks: [
        {
          action: qualityActionsFixture.actions[0],
          actionKey: 'control_tower.zero_result',
          bodyKey: 'control_tower.risk.zero_result.body',
          href: '/analytics/search-quality',
          id: 'zero-result',
          metric: '1',
          recommendationKey: 'control_tower.action.zero_result',
          severity: 'watch',
          titleKey: 'control_tower.risk.zero_result.title',
        },
      ],
      sourceHealth,
    })
    expect(releaseVerification.status).toBe('blocked')
    expect(releaseVerification.nextActionKey).toBe(
      'control_tower.release_verification.next.clear_blockers',
    )
    expect(releaseVerification.totals).toMatchObject({
      blocked: 3,
      evidencePassed: 6,
      evidenceTotal: 6,
      unresolvedRisks: 1,
      watch: 1,
    })
    const maturityRegister = controlTowerPageTestables.buildWorldClassMaturityRegister()
    expect(maturityRegister.categories).toHaveLength(10)
    expect(maturityRegister.categories.every((category) => category.items.length === 10)).toBe(true)
    expect(maturityRegister.totals).toEqual({
      covered: 39,
      gap: 31,
      partial: 30,
      total: 100,
    })
    expect(
      maturityRegister.categories.find((category) => category.id === 'evidence')?.totals,
    ).toEqual({
      covered: 1,
      gap: 6,
      partial: 3,
      total: 10,
    })
    expect(
      maturityRegister.categories.find((category) => category.id === 'requests')?.totals,
    ).toEqual({
      covered: 1,
      gap: 6,
      partial: 3,
      total: 10,
    })
    expect(
      maturityRegister.categories.find((category) => category.id === 'identity')?.totals,
    ).toEqual({
      covered: 2,
      gap: 6,
      partial: 2,
      total: 10,
    })
    expect(
      maturityRegister.categories.find((category) => category.id === 'closed_loop')?.totals,
    ).toEqual({
      covered: 3,
      gap: 3,
      partial: 4,
      total: 10,
    })
    expect(maturityRegister.categories.find((category) => category.id === 'ai')?.totals).toEqual({
      covered: 1,
      gap: 2,
      partial: 7,
      total: 10,
    })
    expect(
      maturityRegister.categories.find((category) => category.id === 'reliability')?.totals,
    ).toEqual({
      covered: 10,
      gap: 0,
      partial: 0,
      total: 10,
    })
    expect(
      maturityRegister.categories.find((category) => category.id === 'governance')?.totals,
    ).toEqual({
      covered: 10,
      gap: 0,
      partial: 0,
      total: 10,
    })
    expect(
      maturityRegister.categories.find((category) => category.id === 'developer')?.totals,
    ).toEqual({
      covered: 9,
      gap: 0,
      partial: 1,
      total: 10,
    })
    expect(controlTowerPageTestables.worldClassMaturityExecutionDefinitions).toHaveLength(32)
    expect(maturityRegister.executionQueue).toHaveLength(1)
    expect(maturityRegister.executionQueue.map((slice) => slice.priority)).toEqual([32])
    expect(maturityRegister.executionQueue[0]).toMatchObject({
      gapId: 'developer_north_star_metrics',
      id: 'developer-north-star-metrics',
      status: 'partial',
    })
    expect(controlTowerPageTestables.worldClassMaturityTotals([{ status: 'covered' }])).toEqual({
      covered: 1,
      gap: 0,
      partial: 0,
      total: 1,
    })
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
          disabledSources: 0,
          cohortCount: 0,
          totalActiveMembers: 0,
          syncsLast24h: 0,
        })
      }),
      http.get('/fb/v1/console/feedback/stats', ({ request }) => {
        seen.add(new URL(request.url).pathname)
        return HttpResponse.json({
          dims: [],
          periodEnd: '2026-07-03T00:00:00Z',
          periodStart: '2026-06-26T00:00:00Z',
          total: '12',
          urgentCount: '2',
        })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = ControlTowerRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toHaveLength(5)
    expect(seen).toEqual(
      new Set([
        '/fb/v1/console/classification-quality',
        '/fb/v1/console/feedback/search/quality',
        '/fb/v1/console/quality-actions',
        '/fb/v1/console/cohort-sync/health',
        '/fb/v1/console/feedback/stats',
      ]),
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
      http.get('/fb/v1/console/feedback/stats', () =>
        HttpResponse.json({
          dims: [],
          periodEnd: '2026-07-03T00:00:00Z',
          periodStart: '2026-06-26T00:00:00Z',
          total: '12',
          urgentCount: '2',
        }),
      ),
      http.get('/fb/v1/console/inbound/sources', () =>
        HttpResponse.json({
          items: [{ ...defaultInboundSources[0], lastEventAt: new Date().toISOString() }],
        }),
      ),
      http.get('/fb/v1/console/quality-actions', () => HttpResponse.json(qualityActionsFixture)),
    )

    renderControlTower()

    await waitFor(() => {
      expect(screen.getByText('控制塔')).toBeInTheDocument()
      expect(screen.getByText('低置信度升高')).toBeInTheDocument()
    })
    expect(screen.getByText('搜索零结果偏高')).toBeInTheDocument()
    expect(screen.getByText('低分恢复已逾期')).toBeInTheDocument()
    expect(screen.getByText('闭环恢复健康度偏低')).toBeInTheDocument()
    expect(screen.getByText('首次价值激活')).toBeInTheDocument()
    expect(screen.getByText('3 / 5 已达标')).toBeInTheDocument()
    expect(screen.getByText('接入源已连接')).toBeInTheDocument()
    expect(screen.getByText('客户信号已进入')).toBeInTheDocument()
    expect(screen.getByText('闭环已测试')).toBeInTheDocument()
    expect(screen.getByText('信号入口健康')).toBeInTheDocument()
    expect(screen.getByText('1 / 1 个源启用')).toBeInTheDocument()
    expect(screen.getByText('1 个新鲜 · 0 个过期')).toBeInTheDocument()
    expect(screen.getByText('Default webhook')).toBeInTheDocument()
    expect(screen.getByText('入口健康，继续监控最近事件和源级错误。')).toBeInTheDocument()
    expect(screen.getByText('世界级可用性矩阵')).toBeInTheDocument()
    expect(screen.getByText('信号理解可信')).toBeInTheDocument()
    expect(screen.getByText('语义发现可靠')).toBeInTheDocument()
    expect(screen.getByText('闭环运营可执行')).toBeInTheDocument()
    expect(screen.getByText('行动问责明确')).toBeInTheDocument()
    expect(screen.getByText('发布验证可追溯')).toBeInTheDocument()
    expect(screen.getByText('发布验证证据')).toBeInTheDocument()
    expect(screen.getByText('证据合同')).toBeInTheDocument()
    expect(screen.getByText('6 / 6 项已纳入')).toBeInTheDocument()
    expect(screen.getByText('产品合同门禁')).toBeInTheDocument()
    expect(screen.getByText('100项世界级成熟度差距')).toBeInTheDocument()
    expect(screen.getByText('100 项总差距')).toBeInTheDocument()
    expect(screen.getByText('31 项仍阻塞世界级体验')).toBeInTheDocument()
    expect(screen.getByText('世界级执行队列')).toBeInTheDocument()
    expect(screen.getByText('1 个优先切片')).toBeInTheDocument()
    expect(
      screen.queryByTestId('control-tower-world-class-execution-ai-review-feedback-loop'),
    ).not.toBeInTheDocument()
    expect(
      screen.queryByTestId('control-tower-world-class-execution-end-to-end-signal-trace'),
    ).not.toBeInTheDocument()
    const northStarMetricsSlice = screen.getByTestId(
      'control-tower-world-class-execution-developer-north-star-metrics',
    )
    expect(northStarMetricsSlice).toHaveTextContent('P32')
    expect(northStarMetricsSlice).toHaveTextContent('开发者与集成平台')
    expect(northStarMetricsSlice).toHaveTextContent(
      'First value time、signal loss rate、decision coverage、closed-loop rate 等北极星指标',
    )
    expect(screen.getByText('身份与账户上下文')).toBeInTheDocument()
    expect(screen.getAllByText('Account/Company 一等公民模型').length).toBeGreaterThan(0)
    expect(screen.getByText('质量门禁、浏览器 smoke 和 bundle 证据可见')).toBeInTheDocument()
    expect(screen.getByText('闭环恢复指挥')).toBeInTheDocument()
    expect(screen.getByText('1 个逾期 · 1 个即将到期')).toBeInTheDocument()
    expect(screen.getByText('1 个未分配 · 1 个负责人')).toBeInTheDocument()
    expect(screen.getByText('逾期恢复')).toBeInTheDocument()
    expect(screen.getByText('未分配恢复')).toBeInTheDocument()
    expect(screen.getByText('待联系客户')).toBeInTheDocument()
    expect(screen.getByText('22222222-2222-2222-2222-222222222222')).toBeInTheDocument()
    expect(screen.getByText('先处理 1 个逾期恢复并更新状态。')).toBeInTheDocument()
    expect(screen.getAllByText('阻塞').length).toBeGreaterThan(0)
    expect(screen.getAllByText('达标').length).toBeGreaterThan(0)
    expect(screen.getAllByText('54/100').length).toBeGreaterThan(0)
    expect(screen.getByText('处理中')).toBeInTheDocument()
    expect(screen.getByText('enterprise export webhook')).toBeInTheDocument()
    expect(screen.getByText('1 个开放低分 · 1 个逾期 · 响应率 33.3%')).toBeInTheDocument()
    expect(screen.getByText('rrf.pgfts.v1.k60')).toBeInTheDocument()
  })

  it('does not request survey analytics when the operator cannot read survey administration', async () => {
    let surveyAnalyticsCalls = 0
    let inboundSourceCalls = 0
    server.use(
      http.get('/fb/v1/console/me', () =>
        HttpResponse.json({
          ...defaultMe,
          user: { ...defaultMe.user, role: 'viewer' },
        }),
      ),
      http.get('/fb/v1/console/classification-quality', () =>
        HttpResponse.json({
          ...classificationFixture,
          summary: {
            ...classificationFixture.summary,
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
            zeroResultRate: 0,
            fallbackRate: 0,
            p95LatencyMs: '100',
            worstSeverity: 'normal',
          },
          indexHealth: {
            ...searchFixture.indexHealth,
            coverageRatio: 1,
          },
        }),
      ),
      http.get('/fb/v1/console/quality-actions', () => HttpResponse.json({ actions: [] })),
      http.get('/fb/v1/console/surveys/analytics', () => {
        surveyAnalyticsCalls += 1
        return HttpResponse.json(defaultSurveyAnalytics)
      }),
      http.get('/fb/v1/console/inbound/sources', () => {
        inboundSourceCalls += 1
        return HttpResponse.json({ items: defaultInboundSources })
      }),
    )

    renderControlTower()

    expect(await screen.findByText('首次价值激活')).toBeInTheDocument()
    expect(screen.getByText('信号入口健康')).toBeInTheDocument()
    expect(screen.getByText('由有权限的操作员确认入站源配置和最近事件。')).toBeInTheDocument()
    expect(screen.getByText('闭环恢复指挥')).toBeInTheDocument()
    expect(screen.getByText('由管理员确认调查分析权限和恢复队列数据。')).toBeInTheDocument()
    expect(screen.getAllByText('需要管理员权限').length).toBeGreaterThan(0)
    expect(screen.getAllByText('当前角色无法读取入站源配置。').length).toBeGreaterThan(0)
    expect(surveyAnalyticsCalls).toBe(0)
    expect(inboundSourceCalls).toBe(0)
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
      http.get('/fb/v1/console/surveys/analytics', () =>
        HttpResponse.json({
          ...defaultSurveyAnalytics,
          invitationCount: 0,
          openLowScoreReviewCount: 0,
          overdueLowScoreReviewCount: 0,
          unassignedLowScoreReviewCount: 0,
          criticalLowScoreReviewCount: 0,
          pendingCustomerContactReviewCount: 0,
          missingRootCauseRecoveryQueueCount: 0,
          missingActionRecoveryQueueCount: 0,
          responseRate: 0,
        }),
      ),
    )

    renderControlTower()

    expect(await screen.findByText('暂无待处理动作')).toBeInTheDocument()
    expect(screen.getAllByText('数据不足').length).toBeGreaterThan(0)
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
    expect(screen.getAllByText('动作状态暂不可用，仍可查看质量证据。').length).toBeGreaterThan(0)
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
