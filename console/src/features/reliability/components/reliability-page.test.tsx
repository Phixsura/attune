import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { describe, expect, it, vi } from 'vitest'
import { buildBackupRestoreDrill } from '@/features/reliability/backup-restore-drill'
import {
  ReliabilityPage,
  type ReliabilityPageProps,
} from '@/features/reliability/components/reliability-page'
import { buildConsistencyChecks } from '@/features/reliability/consistency-checks'
import { buildErrorBudgetLedger } from '@/features/reliability/error-budget-ledger'
import { buildIncidentTimeline } from '@/features/reliability/incident-timeline'
import { buildPipelineSloLedger } from '@/features/reliability/pipeline-slo-ledger'
import { buildReleaseHealthLedger } from '@/features/reliability/release-health-ledger'
import { buildReplayDrill } from '@/features/reliability/replay-drill'
import { buildTenantQuotaSaturation } from '@/features/reliability/tenant-quota-saturation'
import {
  CustomerRequestDeliveryHealth,
  CustomerRequestPriority,
  CustomerRequestStatus,
} from '@/proto/attune/v1/customer_request'
import { expectNoA11yViolations } from '@/testing/a11y'
import { renderWithProviders, screen } from '@/testing/test-utils'

function renderReliabilityPage(props: ReliabilityPageProps) {
  const rootRoute = createRootRoute()
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/',
    component: () => <ReliabilityPage {...props} />,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })

  return renderWithProviders(<RouterProvider router={router} />)
}

function buildProps(overrides: Partial<ReliabilityPageProps> = {}): ReliabilityPageProps {
  const onRefreshAll = overrides.onRefreshAll ?? vi.fn()
  const defaults: ReliabilityPageProps = {
    tenantName: 'Tenant One',
    dashboardHref: '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
    isRefreshing: false,
    onRefreshAll,
    failedQueries: [],
    readiness: {
      status: 'warn',
      tone: 'urgent',
      heroTone: 'urgent',
      value: '告警',
      hint: '2 个通过 · 1 个告警 · 0 个失败 · 用时 42ms。',
    },
    authMode: {
      tone: 'active',
      heroTone: 'active',
      value: '混合',
      hint: '仅 SSO 时需要确保 break-glass 路径可用。',
    },
    apiKeys: {
      tone: 'active',
      heroTone: 'default',
      value: '1/2',
      hint: '1 个活跃 · 1 个非活跃，共 2 个。',
    },
    mcpClients: {
      tone: 'active',
      heroTone: 'default',
      value: '1/2',
      hint: '1 个活跃 · 1 个已撤销，共 2 个。',
    },
    gdpr: {
      tone: 'urgent',
      heroTone: 'active',
      value: '3',
      hint: '1 个排队 · 2 个处理中 · 1 个已就绪导出 · 1 个待执行删除。 · 下一次导出过期：2 小时后。',
    },
    deadDeliveries: {
      tone: 'urgent',
      heroTone: 'urgent',
      value: '2',
      hint: '1 个可重试 · 1 个处理中。',
    },
    backupRestoreDrill: buildBackupRestoreDrill({
      dashboardHref: '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
      preflightChecks: [
        {
          name: 'backup catalog',
          category: 'backup',
          status: 'pass',
          message: 'Backup catalog is reachable',
          remediation: '',
        },
        {
          name: 'migration ledger',
          category: 'migration',
          status: 'pass',
          message: 'Migration ledger is current',
          remediation: '',
        },
      ],
      recovery: {
        status: 'pass',
        message: 'Last restore drill passed (today)',
        freshnessWindowSeconds: 604800,
        ageSeconds: 3600,
        lastRun: {
          ranAt: '2026-08-01T09:00:00Z',
          status: 'pass',
          backupRef: 'nightly-backup',
          durationMs: 1234,
        },
      },
      release: {
        serviceVersion: '5d6ea83',
        environment: 'production',
        profile: 'production',
        lifecycleState: 'supported',
        ownerTeam: 'Platform',
        compatibilityRules: [{ key: 'additive', label: 'Additive', description: '' }],
        glossary: [],
        runbookUrl: 'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
        escalationUrl: 'https://github.com/Phixsura/attune/issues/new/choose',
        startedAt: '2026-08-01T09:00:00Z',
      },
      tenantName: 'Tenant One',
    }),
    consistencyChecks: buildConsistencyChecks({
      customerRequests: [
        {
          id: 'req-1',
          displayId: 'CR-1',
          displayNumber: '1',
          title: 'Keyboard focus restore',
          status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
          priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
          supportingFeedbackCount: 2,
          customerCount: 1,
          linkedIssueCount: 1,
          hiddenFeedbackCount: 0,
          firstFeedbackAt: '2026-07-01T00:00:00Z',
          latestFeedbackAt: '2026-07-02T00:00:00Z',
          createdAt: '2026-07-01T00:00:00Z',
          updatedAt: '2026-07-02T00:00:00Z',
          accountCount: 1,
          voteCount: 1,
          duplicateRequestCount: 0,
          revenueImpactCents: '100000',
          revenueCurrency: 'USD',
          decisionScore: 42,
          decisionScoreExplanation: 'feedback=2 delivery_health=synced',
          deliveryHealth: CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_SYNCED,
          syncedIssueCount: 1,
          staleIssueCount: 0,
          failedIssueCount: 0,
          pendingIssueCount: 0,
          manualIssueCount: 0,
          decisionScoreFactors: [],
        },
      ],
      dashboardHref: '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
      feedbackHref: '/feedback?account_key=tenant-1',
      feedbackStats: {
        periodStart: '2026-07-01T00:00:00Z',
        periodEnd: '2026-07-31T23:59:59Z',
        total: '2',
        urgentCount: '1',
        dims: [],
      },
      notificationEvidence: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 4,
          notifiedCustomers: 2,
          failedCustomers: 1,
          suppressedCustomers: 1,
          recoveryPendingCustomers: 1,
          eventCount: 2,
          lastEventAt: '2026-07-16T00:00:00Z',
        },
      ],
      notificationHref: '/integrations/request-notifications',
      surveyAnalytics: {
        invitationCount: 4,
        deliveredCount: 3,
        suppressedCount: 1,
        completedCount: 2,
        npsAvailable: false,
        qualityFlaggedResponseCount: 0,
        lowScoreCount: 1,
        averageScore: 3.5,
        responseRate: 0.5,
        scoreDistribution: [],
        suppressionReasonDistribution: [],
        averageResponseSeconds: 5400,
        positiveScoreCount: 1,
        positiveScoreRate: 0.5,
        openLowScoreReviewCount: 1,
        overdueLowScoreReviewCount: 1,
        notStartedCount: 1,
        openedCount: 1,
        startedCount: 2,
        expiredCount: 0,
        startRate: 0.5,
        completionRate: 1,
        unassignedLowScoreReviewCount: 1,
        criticalLowScoreReviewCount: 0,
        pendingCustomerContactReviewCount: 1,
        oldestOpenLowScoreReviewDueAt: '2026-07-29T01:00:00Z',
        overdueRecoveryQueueCount: 1,
        unassignedRecoveryQueueCount: 1,
        pendingContactRecoveryQueueCount: 0,
        missingRootCauseRecoveryQueueCount: 0,
        missingActionRecoveryQueueCount: 0,
        ownerRecoveryLoads: [],
        nps: 0,
        detractorCount: 0,
        passiveCount: 0,
        promoterCount: 0,
        redactedResponseCount: 0,
        pendingDeliveryCount: 0,
        delayedDeliveryCount: 0,
        rejectedDeliveryCount: 0,
      },
      surveyHref: '/integrations/surveys',
      tenantName: 'Tenant One',
      usage: {
        periodStart: '2026-07-01T00:00:00Z',
        periodEnd: '2026-07-31T23:59:59Z',
        total: '72',
        quota: '100',
        series: [{ bucket: '2026-07-01T00:00:00Z', value: '72' }],
      },
    }),
    pipelineSloLedger: buildPipelineSloLedger({
      customerRequests: [
        {
          id: 'req-1',
          displayId: 'CR-1',
          displayNumber: '1',
          title: 'Keyboard focus restore',
          status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
          priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
          supportingFeedbackCount: 2,
          customerCount: 1,
          linkedIssueCount: 1,
          hiddenFeedbackCount: 0,
          firstFeedbackAt: '2026-07-01T00:00:00Z',
          latestFeedbackAt: '2026-07-02T00:00:00Z',
          createdAt: '2026-07-01T00:00:00Z',
          updatedAt: '2026-07-02T00:00:00Z',
          accountCount: 1,
          voteCount: 1,
          duplicateRequestCount: 0,
          revenueImpactCents: '100000',
          revenueCurrency: 'USD',
          decisionScore: 42,
          decisionScoreExplanation: 'feedback=2 delivery_health=synced',
          deliveryHealth: CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_SYNCED,
          syncedIssueCount: 1,
          staleIssueCount: 0,
          failedIssueCount: 0,
          pendingIssueCount: 0,
          manualIssueCount: 0,
          decisionScoreFactors: [],
        },
      ],
      dashboardHref: '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
      deadDeliveryCount: 2,
      feedbackHref: '/feedback?account_key=tenant-1',
      inflightDeadDeliveries: 1,
      llmUsage: {
        periodStart: '2026-07-01T00:00:00Z',
        periodEnd: '2026-07-31T23:59:59Z',
        granularity: 'week',
        series: [
          {
            bucket: '2026-07-01T00:00:00Z',
            tenantId: 'tenant-1',
            modelId: 'gpt-5-mini',
            promptTokens: '1000',
            completionTokens: '500',
            costUsd: 1.23,
            calls: '20',
            errors: '1',
          },
        ],
        promptTokens: '1000',
        completionTokens: '500',
        costUsd: 1.23,
        calls: '20',
        errors: '1',
      },
      preflightChecks: [
        {
          name: 'database',
          category: 'database',
          status: 'pass',
          message: 'Database reachable',
          remediation: '',
        },
        {
          name: 'worker',
          category: 'worker',
          status: 'warn',
          message: 'Replay queue needs review',
          remediation: 'Run the replay drill before release.',
        },
        {
          name: 'metrics',
          category: 'metrics',
          status: 'pass',
          message: 'Metrics ready',
          remediation: '',
        },
      ],
      readinessStatus: 'warn',
      releaseLifecycleState: 'supported',
      retryableDeadDeliveries: 1,
      tenantName: 'Tenant One',
      usage: {
        periodStart: '2026-07-01T00:00:00Z',
        periodEnd: '2026-07-31T23:59:59Z',
        total: '72',
        quota: '100',
        series: [{ bucket: '2026-07-01T00:00:00Z', value: '72' }],
      },
    }),
    errorBudgetLedger: buildErrorBudgetLedger({
      activeApiKeys: 1,
      activeGdpr: 2,
      activeMcpClients: 1,
      authMode: 'hybrid',
      dashboardHref: '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
      deadDeliveryCount: 2,
      inflightDeadDeliveries: 1,
      queuedGdpr: 1,
      readinessStatus: 'warn',
      recoveryStatus: 'pass',
      releaseLifecycleState: 'supported',
      retryableDeadDeliveries: 1,
      scheduledDeletes: 1,
      tenantName: 'Tenant One',
      totalApiKeys: 2,
      totalMcpClients: 2,
    }),
    incidentTimeline: buildIncidentTimeline({
      activeGdpr: 2,
      dashboardHref: '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
      deadDeliveryCount: 2,
      feedbackHref: '/feedback?account_key=tenant-1',
      feedbackStats: {
        periodStart: '2026-07-01T00:00:00Z',
        periodEnd: '2026-07-31T23:59:59Z',
        total: '2',
        urgentCount: '1',
        dims: [],
      },
      inflightDeadDeliveries: 1,
      notificationEvidence: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 4,
          notifiedCustomers: 2,
          failedCustomers: 1,
          suppressedCustomers: 1,
          recoveryPendingCustomers: 1,
          eventCount: 2,
          lastEventAt: '2026-07-16T00:00:00Z',
        },
      ],
      notificationHref: '/integrations/request-notifications',
      preflightChecks: [
        {
          name: 'database',
          category: 'database',
          status: 'pass',
          message: 'Database reachable',
          remediation: '',
        },
        {
          name: 'worker',
          category: 'worker',
          status: 'warn',
          message: 'Replay queue needs review',
          remediation: 'Run the replay drill before release.',
        },
      ],
      queuedGdpr: 1,
      readinessStatus: 'warn',
      recovery: {
        status: 'pass',
        message: 'Last restore drill passed (today)',
        freshnessWindowSeconds: 604800,
        ageSeconds: 3600,
        lastRun: {
          ranAt: '2026-08-01T09:00:00Z',
          status: 'pass',
          backupRef: 'nightly-backup',
          durationMs: 1234,
        },
      },
      release: {
        serviceVersion: '5d6ea83',
        environment: 'production',
        profile: 'production',
        lifecycleState: 'supported',
        ownerTeam: 'Platform',
        compatibilityRules: [{ key: 'additive', label: 'Additive', description: '' }],
        glossary: [],
        runbookUrl: 'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
        escalationUrl: 'https://github.com/Phixsura/attune/issues/new/choose',
        startedAt: '2026-08-01T09:00:00Z',
      },
      retryableDeadDeliveries: 1,
      scheduledDeletes: 1,
      tenantName: 'Tenant One',
    }),
    releaseHealthLedger: buildReleaseHealthLedger({
      dashboardHref: '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
      escalationHref: 'https://github.com/Phixsura/attune/issues/new/choose',
      feedbackHref: '/feedback?account_key=tenant-1',
      feedbackStats: {
        periodStart: '2026-07-01T00:00:00Z',
        periodEnd: '2026-07-31T23:59:59Z',
        total: '2',
        urgentCount: '1',
        dims: [],
      },
      notificationEvidence: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 4,
          notifiedCustomers: 2,
          failedCustomers: 1,
          suppressedCustomers: 1,
          recoveryPendingCustomers: 1,
          eventCount: 2,
          lastEventAt: '2026-07-16T00:00:00Z',
        },
      ],
      notificationHref: '/integrations/request-notifications',
      readinessStatus: 'warn',
      recovery: {
        status: 'pass',
        message: 'Last restore drill passed (today)',
        freshnessWindowSeconds: 604800,
        ageSeconds: 3600,
        lastRun: {
          ranAt: '2026-08-01T09:00:00Z',
          status: 'pass',
          backupRef: 'nightly-backup',
          durationMs: 1234,
        },
      },
      release: {
        serviceVersion: '5d6ea83',
        environment: 'production',
        profile: 'production',
        lifecycleState: 'supported',
        ownerTeam: 'Platform',
        compatibilityRules: [{ key: 'additive', label: 'Additive', description: '' }],
        glossary: [],
        runbookUrl: 'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
        escalationUrl: 'https://github.com/Phixsura/attune/issues/new/choose',
        startedAt: '2026-08-01T09:00:00Z',
      },
    }),
    replayDrill: buildReplayDrill({
      activeGdpr: 2,
      dashboardHref: '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
      deadDeliveryCount: 2,
      inflightDeadDeliveries: 1,
      queuedGdpr: 1,
      readinessStatus: 'warn',
      recoveryStatus: 'pass',
      releaseLifecycleState: 'supported',
      retryableDeadDeliveries: 1,
      scheduledDeletes: 1,
      tenantName: 'Tenant One',
    }),
    tenantQuotaSaturation: buildTenantQuotaSaturation({
      apiKeys: [
        {
          id: 'key-1',
          keyPrefix: 'ak_1',
          label: 'limited',
          isActive: true,
          createdAt: '2026-07-01T00:00:00Z',
          scopes: ['ingest:write'],
          allowedCidrs: [],
          usageCount: '40',
          rateLimitRpm: 120,
          environment: 'production',
        },
        {
          id: 'key-2',
          keyPrefix: 'ak_2',
          label: 'unbounded',
          isActive: true,
          createdAt: '2026-07-01T00:00:00Z',
          scopes: ['ingest:write'],
          allowedCidrs: [],
          usageCount: '32',
          environment: 'production',
        },
      ],
      dashboardHref: '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
      deadDeliveryCount: 2,
      gdprOperations: {
        stepUp: {
          satisfied: true,
          passwordAllowed: true,
          method: 'password',
          ttlSeconds: 900,
        },
        exportTtlSeconds: 86400,
        auditRetentionDays: 30,
        auditPruneIntervalSeconds: 3600,
        queuedRequestCount: 1,
        activeRequestCount: 2,
        readyExportCount: 1,
        hashedAuditResidue: true,
        backupsMayRetainUntilRotation: true,
        legalHoldSupported: false,
        deleteGraceWindowSeconds: 3600,
        scheduledDeleteCount: 1,
      },
      inflightDeadDeliveries: 1,
      llmUsage: {
        periodStart: '2026-07-01T00:00:00Z',
        periodEnd: '2026-07-31T23:59:59Z',
        granularity: 'week',
        series: [],
        promptTokens: '12000',
        completionTokens: '4000',
        costUsd: 2.34,
        calls: '20',
        errors: '1',
      },
      mcpClients: [
        {
          id: 'mcp-1',
          name: 'limited-agent',
          redirect_uris: ['http://localhost/callback'],
          scopes: ['mcp:read'],
          tool_policy_mode: 'allow_list',
          rate_limit_rpm: 60,
          rate_limit_burst: 10,
          created_at: '2026-07-01T00:00:00Z',
          created_by: 'admin',
        },
        {
          id: 'mcp-2',
          name: 'unbounded-agent',
          redirect_uris: ['http://localhost/callback'],
          scopes: ['mcp:read'],
          tool_policy_mode: 'legacy_allow_all',
          rate_limit_rpm: null,
          rate_limit_burst: null,
          created_at: '2026-07-01T00:00:00Z',
          created_by: 'admin',
        },
      ],
      retryableDeadDeliveries: 1,
      tenantName: 'Tenant One',
      usage: {
        periodStart: '2026-07-01T00:00:00Z',
        periodEnd: '2026-07-31T23:59:59Z',
        total: '72',
        quota: '100',
        series: [{ bucket: '2026-07-01T00:00:00Z', value: '72' }],
      },
    }),
    releaseContext: {
      version: {
        tone: 'active',
        heroTone: 'active',
        value: '5d6ea83',
        hint: '已启动 42 分钟前。',
      },
      environment: {
        tone: 'active',
        heroTone: 'active',
        value: 'production',
        hint: 'profile=production。',
      },
      owner: {
        tone: 'active',
        heroTone: 'active',
        value: 'Platform',
        hint: 'Runbook 与升级通道应保持同步。',
      },
      lifecycle: {
        tone: 'active',
        heroTone: 'active',
        value: 'supported',
        hint: 'Current runtime contract is within the supported window.',
      },
      recovery: {
        tone: 'active',
        heroTone: 'active',
        value: '通过',
        hint: 'Last restore drill passed (today)',
      },
      compatibility: {
        tone: 'active',
        heroTone: 'active',
        value: '5 rules',
        hint: 'Additive · Breaking · Deprecated with shim · Rename with alias · Migration step',
      },
      glossary:
        'Environment · Profile · Service · Owner · Policy mode · Release state · Lifecycle state · Risk class',
      runbookHref: 'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
      escalationHref: 'https://github.com/Phixsura/attune/issues/new/choose',
    },
  }
  return {
    ...defaults,
    ...overrides,
    onRefreshAll,
  }
}

describe('ReliabilityPage', () => {
  it('renders the reliability summary and dashboard shortcut', async () => {
    const onRefreshAll = vi.fn()
    const props = buildProps({ onRefreshAll })

    const { container, user } = renderReliabilityPage(props)

    await screen.findByText('可靠性总览')
    expect(screen.getByText('可靠性总览')).toBeInTheDocument()
    expect(screen.getByText('打开 tenant impact dashboard')).toBeInTheDocument()
    expect(screen.getByText('Release health correlation ledger')).toBeInTheDocument()
    expect(screen.getByText('5d6ea83 / production / supported')).toBeInTheDocument()
    expect(screen.getByText('2 release-health signals need attention')).toBeInTheDocument()
    expect(screen.getByText('Incident timeline reconstruction')).toBeInTheDocument()
    expect(screen.getByText('Tenant One / 5d6ea83 / supported')).toBeInTheDocument()
    expect(screen.getByText('4 incident timeline phases need attention')).toBeInTheDocument()
    expect(screen.getByText('worker: Replay queue needs review')).toBeInTheDocument()
    expect(screen.getAllByText('1 urgent / 2 total feedback').length).toBeGreaterThanOrEqual(2)
    expect(
      screen.getAllByText('1 failed / 1 recovery pending customers').length,
    ).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('Tenant quota / saturation dashboard')).toBeInTheDocument()
    expect(screen.getByText('4 quota lanes need attention')).toBeInTheDocument()
    expect(screen.getByText('72% used / 1 unbounded active API keys')).toBeInTheDocument()
    expect(screen.getAllByText('1 unbounded / 2 active MCP clients').length).toBeGreaterThanOrEqual(
      2,
    )
    expect(screen.getByText('40% dead-letter saturation')).toBeInTheDocument()
    expect(screen.getByText('Backup / restore drill evidence')).toBeInTheDocument()
    expect(screen.getByText('Tenant One / nightly-backup / supported')).toBeInTheDocument()
    expect(screen.getByText('backup and restore evidence is verified')).toBeInTheDocument()
    expect(screen.getByText('backup=nightly-backup / age=1h / window=7d')).toBeInTheDocument()
    expect(screen.getByText('pass restore / 1.2s')).toBeInTheDocument()
    expect(screen.getByText('Data consistency checks')).toBeInTheDocument()
    expect(
      screen.getByText('Tenant One / 2 feedback / 1 requests / 2 survey completions'),
    ).toBeInTheDocument()
    expect(screen.getByText('2 consistency checks need attention')).toBeInTheDocument()
    expect(
      screen.getByText('72 ingested / 2 feedback records / 1 usage buckets'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('2 supporting feedback / 1 requests / 0 orphaned requests'),
    ).toBeInTheDocument()
    expect(screen.getByText('2 notified / 4 expected / 1 request statuses')).toBeInTheDocument()
    expect(screen.getByText('1 low-score / 1 open reviews / 1 overdue')).toBeInTheDocument()
    expect(screen.getByText('Pipeline SLO ledger')).toBeInTheDocument()
    expect(
      screen.getByText('Tenant One / 72 ingested / 20 enrich calls / 1 sync rows'),
    ).toBeInTheDocument()
    expect(screen.getByText('3 pipeline SLOs need attention')).toBeInTheDocument()
    expect(screen.getByText('72 ingested / 1 buckets / 72.0% quota used')).toBeInTheDocument()
    expect(screen.getByText('20 calls / 1 errors / 5.0% error rate')).toBeInTheDocument()
    expect(screen.getAllByText('1 retryable / 1 in-flight / 2 dead').length).toBeGreaterThanOrEqual(
      2,
    )
    expect(
      screen.getByText('1 synced / 0 stale / 0 failed / 0 pending / 0 manual'),
    ).toBeInTheDocument()
    expect(screen.getByText('Replay / Backfill 演练')).toBeInTheDocument()
    expect(screen.getByText('Error budget / burn-rate ledger')).toBeInTheDocument()
    expect(screen.getByText('Replay 工作区')).toBeInTheDocument()
    expect(screen.getAllByText('Outbox burn x').length).toBeGreaterThanOrEqual(2)
    expect(screen.getAllByText(/0.10% budget allowance/).length).toBeGreaterThan(0)
    expect(
      screen.getByText('attune:ingest_service_failure_ratio:ratio5m / 0.001'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('clamp_min(1 - (attune:ingest_service_failure_ratio:ratio6h / 0.001), 0)'),
    ).toBeInTheDocument()
    expect(screen.getAllByText('Maintenance windows only').length).toBeGreaterThan(0)
    expect(
      screen.getByText('Retry dead deliveries and verify destination acceptance'),
    ).toBeInTheDocument()
    expect(screen.getAllByText('1 retryable / 1 in-flight / 2 dead').length).toBeGreaterThanOrEqual(
      3,
    )
    expect(screen.getByRole('link', { name: /Dead deliveries/ })).toHaveAttribute(
      'href',
      '/administration/dead-deliveries',
    )
    expect(screen.getByText('运行快照')).toBeInTheDocument()
    expect(screen.getByText('发布与归属')).toBeInTheDocument()
    expect(screen.getByText('快速入口')).toBeInTheDocument()
    expect(screen.getByText('1 个活跃 · 1 个非活跃，共 2 个。')).toBeInTheDocument()
    expect(
      screen.getByText(/1 个排队 · 2 个处理中 · 1 个已就绪导出 · 1 个待执行删除。/),
    ).toBeInTheDocument()
    expect(screen.getByText(/1 个可重试 · 1 个处理中。/)).toBeInTheDocument()
    expect(screen.getByText('5d6ea83')).toBeInTheDocument()
    expect(screen.getAllByText('supported').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('恢复')).toBeInTheDocument()
    expect(screen.getByText('5 rules')).toBeInTheDocument()
    expect(screen.getByText('Canonical terms')).toBeInTheDocument()
    expect(
      screen.getByText(
        'Environment · Profile · Service · Owner · Policy mode · Release state · Lifecycle state · Risk class',
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /运行手册/ })).toHaveAttribute(
      'href',
      'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
    )
    expect(screen.getByRole('link', { name: /升级通道/ })).toHaveAttribute(
      'href',
      'https://github.com/Phixsura/attune/issues/new/choose',
    )

    const dashboardLink = screen.getByRole('link', { name: '打开 tenant impact dashboard' })
    expect(dashboardLink).toHaveAttribute(
      'href',
      '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
    )

    await user.click(screen.getByRole('button', { name: '刷新' }))
    expect(onRefreshAll).toHaveBeenCalledTimes(1)

    await expectNoA11yViolations(container)
  })

  it('surfaces partial query failures without hiding the rest of the page', async () => {
    const props = buildProps({
      failedQueries: [{ label: '系统就绪', message: 'boom' }],
    })

    const { container } = renderReliabilityPage(props)

    await screen.findByText('部分可靠性数据加载失败')
    expect(screen.getByText('部分可靠性数据加载失败')).toBeInTheDocument()
    expect(screen.getByText('boom')).toBeInTheDocument()
    expect(screen.getByText('运行快照')).toBeInTheDocument()
    expect(screen.getByText('打开 tenant impact dashboard')).toBeInTheDocument()

    await expectNoA11yViolations(container)
  })
})
