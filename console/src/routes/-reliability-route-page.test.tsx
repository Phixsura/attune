import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { reliabilityCatalog } from '@/features/reliability/reliability-catalog'
import { ReliabilityRoutePage } from '@/routes/-reliability-route-page'
import { expectNoA11yViolations } from '@/testing/a11y'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

const preflightReport = {
  status: 'warn' as const,
  elapsed: '42ms',
  checks: [
    { name: 'config:parse', category: 'config' as const, status: 'pass' as const, message: 'OK' },
    {
      name: 'database:connectivity',
      category: 'database' as const,
      status: 'warn' as const,
      message: 'Slow but reachable',
    },
    {
      name: 'worker:enricher',
      category: 'worker' as const,
      status: 'pass' as const,
      message: '2 workers',
    },
  ],
}

function renderReliabilityPage() {
  const rootRoute = createRootRoute()
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: '/',
    component: ReliabilityRoutePage,
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: ['/'] }),
  })

  return renderWithProviders(<RouterProvider router={router} />)
}

describe('ReliabilityRoutePage', () => {
  it('renders the reliability summary and dashboard shortcut', async () => {
    server.use(
      http.get('/fb/v1/console/me', () =>
        HttpResponse.json({
          tenant: {
            id: 'tenant-1',
            slug: 'tenant-1',
            name: 'Tenant One',
            locale: 'zh-CN',
            timezone: 'Asia/Singapore',
          },
          user: { openId: 'user-1', name: 'Alice', role: 'admin' },
          csrfToken: 'csrf',
        }),
      ),
      http.get('/fb/v1/console/auth/sso/mode', () => HttpResponse.json({ mode: 'hybrid' })),
      http.get('/fb/v1/console/system/preflight', () => HttpResponse.json(preflightReport)),
      http.get('/fb/v1/console/api-keys', () =>
        HttpResponse.json({
          items: [
            {
              id: 'key-1',
              keyPrefix: 'ak_live_1',
              label: 'main',
              isActive: true,
              createdAt: '2026-07-01T00:00:00Z',
              scopes: ['feedback:read'],
              allowedCidrs: [],
              usageCount: '12',
              environment: 'production',
            },
            {
              id: 'key-2',
              keyPrefix: 'ak_live_2',
              label: 'rotated',
              isActive: false,
              createdAt: '2026-06-01T00:00:00Z',
              revokedAt: '2026-06-15T00:00:00Z',
              scopes: ['feedback:read'],
              allowedCidrs: [],
              usageCount: '3',
              environment: 'production',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/mcp/clients', () =>
        HttpResponse.json({
          clients: [
            {
              id: 'client-1',
              name: 'ops-bot',
              redirect_uris: ['http://localhost/cb'],
              scopes: ['mcp:*'],
              tool_policy_mode: 'allow_list',
              created_at: '2026-07-01T00:00:00Z',
              created_by: 'user-1',
            },
            {
              id: 'client-2',
              name: 'legacy',
              redirect_uris: ['http://localhost/cb'],
              scopes: ['mcp:*'],
              tool_policy_mode: 'legacy_allow_all',
              created_at: '2026-06-01T00:00:00Z',
              created_by: 'user-1',
              revoked_at: '2026-06-15T00:00:00Z',
            },
          ],
        }),
      ),
      http.get('/fb/v1/console/gdpr/operations', () =>
        HttpResponse.json({
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
          queuedRequestCount: 1,
          activeRequestCount: 2,
          readyExportCount: 1,
          nextExportExpiryAt: '2026-07-01T02:00:00Z',
          hashedAuditResidue: true,
          backupsMayRetainUntilRotation: true,
          legalHoldSupported: true,
          deleteGraceWindowSeconds: 1800,
          scheduledDeleteCount: 1,
        }),
      ),
      http.get('/fb/v1/console/outbox/deliveries', ({ request }) => {
        const url = new URL(request.url)
        if (url.searchParams.get('status') !== 'dead') {
          return HttpResponse.json({ deliveries: [] })
        }
        return HttpResponse.json({
          deliveries: [
            {
              id: 'delivery-1',
              feedbackId: 'feedback-1',
              destinationType: 'raw-webhook',
              destinationTarget: 'https://example.com/hook',
              audience: 'all',
              status: 'dead',
              attempts: 3,
              failureKind: 'http_5xx',
              httpStatus: 503,
              lastError: 'upstream returned 503',
              deadReason: 'max attempts exhausted',
              traceId: 'trace-1',
              nextRetryAt: '',
              createdAt: '2026-07-01T00:00:00Z',
              deliveredAt: '',
              lastManualRetryAt: '',
              retriedBy: '',
              manualRetryCount: 0,
              inFlight: false,
            },
            {
              id: 'delivery-2',
              feedbackId: 'feedback-2',
              destinationType: 'slack',
              destinationTarget: 'https://example.com/slack',
              audience: 'all',
              status: 'dead',
              attempts: 5,
              failureKind: 'http_5xx',
              httpStatus: 429,
              lastError: 'upstream returned 429',
              deadReason: 'max attempts exhausted',
              traceId: 'trace-2',
              nextRetryAt: '',
              createdAt: '2026-07-01T01:00:00Z',
              deliveredAt: '',
              lastManualRetryAt: '',
              retriedBy: '',
              manualRetryCount: 0,
              inFlight: true,
            },
          ],
        })
      }),
    )

    const { container } = renderReliabilityPage()

    await waitFor(() => {
      expect(screen.getByText('1 个活跃 · 1 个非活跃，共 2 个。')).toBeInTheDocument()
    })

    expect(screen.getByText('可靠性总览')).toBeInTheDocument()
    expect(screen.getByText('SLO 目录')).toBeInTheDocument()
    expect(screen.getByText('打开 tenant impact dashboard')).toBeInTheDocument()
    expect(screen.getByText('运行快照')).toBeInTheDocument()
    expect(screen.getByText('快速入口')).toBeInTheDocument()
    expect(screen.getAllByText('预算例外')).toHaveLength(reliabilityCatalog.length)
    expect(screen.getByText('1 个活跃 · 1 个非活跃，共 2 个。')).toBeInTheDocument()
    expect(
      screen.getByText(/1 个排队 · 2 个处理中 · 1 个已就绪导出 · 1 个待执行删除。/),
    ).toBeInTheDocument()
    expect(screen.getByText(/1 个可重试 · 1 个处理中。/)).toBeInTheDocument()

    const dashboardLink = screen.getByRole('link', { name: '打开 tenant impact dashboard' })
    expect(dashboardLink).toHaveAttribute(
      'href',
      '/d/attune-tenant-impact/attune-tenant-impact?var-tenant=tenant-1',
    )
    expect(screen.getByRole('link', { name: /^策略参考/ })).toHaveAttribute(
      'href',
      'https://github.com/Phixsura/attune/blob/main/observability/reports/attune-slo-policy-reference.md',
    )
    expect(screen.getByRole('link', { name: /^回放报告/ })).toHaveAttribute(
      'href',
      'https://github.com/Phixsura/attune/blob/main/observability/reports/attune-slo-replay-template.md',
    )
    expect(screen.getByRole('link', { name: /^OpenSLO 导出/ })).toHaveAttribute(
      'href',
      'https://github.com/Phixsura/attune/blob/main/observability/openslo/attune-slo.yaml',
    )
    const replayDownloadLink = screen.getByRole('link', { name: /^下载 replay 工作表/ })
    expect(replayDownloadLink).toHaveAttribute('download', 'attune-slo-replay-template.md')
    const replayDownloadHref = replayDownloadLink.getAttribute('href')
    expect(replayDownloadHref).toContain('data:text/markdown;charset=utf-8,')
    expect(decodeURIComponent(replayDownloadHref?.split(',', 2)[1] ?? '')).toContain('Tenant One')
    expect(screen.getByText('Replay 工作区')).toBeInTheDocument()

    await expectNoA11yViolations(container)
  })

  it('surfaces partial query failures without hiding the rest of the page', async () => {
    server.use(
      http.get('/fb/v1/console/me', () =>
        HttpResponse.json({
          tenant: {
            id: 'tenant-1',
            slug: 'tenant-1',
            name: 'Tenant One',
            locale: 'zh-CN',
            timezone: 'Asia/Singapore',
          },
          user: { openId: 'user-1', name: 'Alice', role: 'admin' },
          csrfToken: 'csrf',
        }),
      ),
      http.get('/fb/v1/console/auth/sso/mode', () => HttpResponse.json({ mode: 'hybrid' })),
      http.get('/fb/v1/console/system/preflight', () => HttpResponse.json(preflightReport)),
      http.get('/fb/v1/console/api-keys', () =>
        HttpResponse.json({ code: 'internal', message: 'boom' }, { status: 500 }),
      ),
      http.get('/fb/v1/console/mcp/clients', () => HttpResponse.json({ clients: [] })),
      http.get('/fb/v1/console/gdpr/operations', () =>
        HttpResponse.json({
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
        }),
      ),
      http.get('/fb/v1/console/outbox/deliveries', () => HttpResponse.json({ deliveries: [] })),
    )

    renderReliabilityPage()

    await waitFor(() => {
      expect(screen.getByText('部分可靠性数据加载失败')).toBeInTheDocument()
    })
    expect(screen.getByText('运行快照')).toBeInTheDocument()
    expect(screen.getByText('打开 tenant impact dashboard')).toBeInTheDocument()
  })
})
