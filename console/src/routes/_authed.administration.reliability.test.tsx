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
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = ReliabilityRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toHaveLength(7)
    expect(ReliabilityRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(
      new Set([
        '/fb/v1/console/me',
        '/fb/v1/console/auth/sso/mode',
        '/fb/v1/console/system/preflight',
        '/fb/v1/console/api-keys',
        '/fb/v1/console/mcp/clients',
        '/fb/v1/console/gdpr/operations',
        '/fb/v1/console/outbox/deliveries',
      ]),
    )
    expect(deliveriesURL?.searchParams.get('status')).toBe('dead')
    expect(deliveriesURL?.searchParams.get('limit')).toBe('50')
  })
})
