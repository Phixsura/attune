import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { Route as ReliabilityRoute } from '@/routes/_authed.administration.reliability'
import { server } from '@/testing/mocks/server'

describe('_authed.administration.reliability route', () => {
  it('preloads the reliability snapshot endpoints from the route loader', async () => {
    const seenPaths = new Set<string>()
    let deliveriesURL: URL | undefined

    server.use(
      http.get('/fb/v1/console/me', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          tenant: {
            id: 'tenant-1',
            slug: 'tenant-1',
            name: 'Tenant One',
            locale: 'zh-CN',
            timezone: 'Asia/Singapore',
          },
          user: { openId: 'user-1', name: 'Alice', role: 'admin' },
          csrfToken: 'csrf',
        })
      }),
      http.get('/fb/v1/console/auth/sso/mode', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ mode: 'hybrid' })
      }),
      http.get('/fb/v1/console/system/preflight', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          status: 'warn',
          elapsed: '42ms',
          checks: [],
        })
      }),
      http.get('/fb/v1/console/system/recovery', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          status: 'pass',
          message: 'Last restore drill passed (today)',
          freshnessWindowSeconds: 604800,
          ageSeconds: 0,
          lastRun: {
            ranAt: '2026-07-01T00:00:00Z',
            status: 'pass',
            backupRef: 'nightly-backup',
            durationMs: 1234,
          },
        })
      }),
      http.get('/fb/v1/console/system/release', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          serviceVersion: '5d6ea83',
          environment: 'production',
          profile: 'production',
          lifecycleState: 'supported',
          ownerTeam: 'Platform',
          compatibilityRules: [],
          glossary: [],
          runbookUrl: 'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
          escalationUrl: 'https://github.com/Phixsura/attune/issues/new/choose',
          startedAt: '2026-07-01T00:00:00Z',
        })
      }),
      http.get('/fb/v1/console/api-keys', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
      http.get('/fb/v1/console/mcp/clients', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ clients: [] })
      }),
      http.get('/fb/v1/console/gdpr/operations', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          stepUp: {
            satisfied: true,
            passwordAllowed: true,
            method: 'password',
            ttlSeconds: 900,
            verifiedAt: '2026-07-01T00:00:00Z',
            expiresAt: '2026-07-01T00:15:00Z',
          },
          exportTtlSeconds: 7200,
          auditRetentionDays: 30,
          auditPruneIntervalSeconds: 86400,
          queuedRequestCount: 0,
          activeRequestCount: 0,
          readyExportCount: 0,
          hashedAuditResidue: true,
          backupsMayRetainUntilRotation: true,
          legalHoldSupported: true,
          deleteGraceWindowSeconds: 1800,
          scheduledDeleteCount: 0,
        })
      }),
      http.get('/fb/v1/console/outbox/deliveries', ({ request }) => {
        deliveriesURL = new URL(request.url)
        seenPaths.add(deliveriesURL.pathname)
        return HttpResponse.json({ deliveries: [] })
      }),
      http.get('/fb/v1/console/feedback/stats', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          periodStart: '2026-07-01T00:00:00Z',
          periodEnd: '2026-07-31T23:59:59Z',
          total: '2',
          urgentCount: '1',
          dims: [],
        })
      }),
      http.get('/fb/v1/console/request-notifications/status-evidence', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          items: [
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
        })
      }),
      http.get('/fb/v1/console/usage', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          periodStart: '2026-07-01T00:00:00Z',
          periodEnd: '2026-07-31T23:59:59Z',
          total: '72',
          quota: '100',
          series: [{ bucket: '2026-07-01T00:00:00Z', value: '72' }],
        })
      }),
      http.get('/fb/v1/console/llm-usage', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          periodStart: '2026-07-01T00:00:00Z',
          periodEnd: '2026-07-31T23:59:59Z',
          granularity: 'week',
          series: [],
          promptTokens: '12000',
          completionTokens: '4000',
          costUsd: 2.34,
          calls: '20',
          errors: '1',
        })
      }),
      http.get('/fb/v1/console/customer-requests', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ requests: [] })
      }),
      http.get('/fb/v1/console/surveys/analytics', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          invitationCount: 0,
          deliveredCount: 0,
          suppressedCount: 0,
          completedCount: 0,
          lowScoreCount: 0,
          averageScore: 0,
          responseRate: 0,
          scoreDistribution: [],
          suppressionReasonDistribution: [],
          averageResponseSeconds: 0,
          positiveScoreCount: 0,
          positiveScoreRate: 0,
          openLowScoreReviewCount: 0,
          overdueLowScoreReviewCount: 0,
          notStartedCount: 0,
          openedCount: 0,
          expiredCount: 0,
          unassignedLowScoreReviewCount: 0,
          criticalLowScoreReviewCount: 0,
          pendingCustomerContactReviewCount: 0,
          overdueRecoveryQueueCount: 0,
          unassignedRecoveryQueueCount: 0,
          pendingContactRecoveryQueueCount: 0,
          missingRootCauseRecoveryQueueCount: 0,
          missingActionRecoveryQueueCount: 0,
          pendingDeliveryCount: 0,
          delayedDeliveryCount: 0,
          rejectedDeliveryCount: 0,
        })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = ReliabilityRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toHaveLength(15)
    expect(ReliabilityRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(
      new Set([
        '/fb/v1/console/me',
        '/fb/v1/console/auth/sso/mode',
        '/fb/v1/console/system/preflight',
        '/fb/v1/console/system/recovery',
        '/fb/v1/console/system/release',
        '/fb/v1/console/api-keys',
        '/fb/v1/console/mcp/clients',
        '/fb/v1/console/gdpr/operations',
        '/fb/v1/console/outbox/deliveries',
        '/fb/v1/console/feedback/stats',
        '/fb/v1/console/request-notifications/status-evidence',
        '/fb/v1/console/usage',
        '/fb/v1/console/llm-usage',
        '/fb/v1/console/customer-requests',
        '/fb/v1/console/surveys/analytics',
      ]),
    )
    expect(deliveriesURL?.searchParams.get('status')).toBe('dead')
    expect(deliveriesURL?.searchParams.get('limit')).toBe('50')
  })
})
