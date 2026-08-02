import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { Route as SecurityRoute } from '@/routes/_authed.administration.security'
import { server } from '@/testing/mocks/server'

describe('_authed.administration.security route', () => {
  it('preloads governance and field-level permission evidence from the route loader', async () => {
    const seenPaths = new Set<string>()
    let auditURL: URL | undefined
    let fieldAuditURL: URL | undefined
    let moderationURL: URL | undefined

    server.use(
      http.get('/fb/v1/console/auth/sso/mode', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ mode: 'hybrid' })
      }),
      http.get('/fb/v1/console/members', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ members: [] })
      }),
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        const url = new URL(request.url)
        if (url.searchParams.get('targetType') === 'public_moderation_subject') {
          fieldAuditURL = url
        } else {
          auditURL = url
        }
        seenPaths.add(url.pathname)
        return HttpResponse.json({ items: [] })
      }),
      http.get('/fb/v1/console/public-visibility/policy', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          changelogEnabled: true,
          commentWriteMode: 'PUBLIC_WRITE_MODE_DISABLED',
          commentsEnabled: true,
          createdAt: '2026-07-01T00:00:00Z',
          defaultCommentState: 'MODERATION_STATE_PENDING',
          defaultRequestState: 'MODERATION_STATE_PENDING',
          hidePublicTimestamps: true,
          portalAccessMode: 'PUBLIC_ACCESS_MODE_INVITE_ONLY',
          requestsEnabled: true,
          roadmapEnabled: true,
          roadmapStatusMapping: [],
          searchIndexingEnabled: false,
          showCommentCount: false,
          showSubmitterDisplay: false,
          showVoteCount: false,
          submissionWriteMode: 'PUBLIC_WRITE_MODE_IDENTIFIED',
          submitterIdentityMode: 'PUBLIC_IDENTITY_MODE_ORGANIZATION',
          tenantId: 'tenant-1',
          updatedAt: '2026-07-01T00:00:00Z',
          updatedBy: 'admin-1',
          voteWriteMode: 'PUBLIC_WRITE_MODE_IDENTIFIED',
        })
      }),
      http.get('/fb/v1/console/public-visibility/moderation', ({ request }) => {
        moderationURL = new URL(request.url)
        seenPaths.add(moderationURL.pathname)
        return HttpResponse.json({ subjects: [] })
      }),
      http.get('/fb/v1/console/gdpr/operations', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          activeRequestCount: 0,
          auditPruneIntervalSeconds: 3600,
          auditRetentionDays: 30,
          backupsMayRetainUntilRotation: false,
          deleteGraceWindowSeconds: 3600,
          exportTtlSeconds: 86400,
          hashedAuditResidue: true,
          legalHoldSupported: true,
          queuedRequestCount: 0,
          readyExportCount: 0,
          scheduledDeleteCount: 0,
          stepUp: {
            method: 'password',
            passwordAllowed: true,
            satisfied: false,
            ttlSeconds: 900,
          },
        })
      }),
      http.get('/fb/v1/console/notify-targets', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
      http.get('/fb/v1/console/api-keys', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
      http.get('/fb/v1/console/inbound/sources', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
      http.get('/fb/v1/console/llm/channels', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
      http.get('/fb/v1/console/reply-send-hook', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          createdAt: '2026-07-01T00:00:00Z',
          enabled: true,
          id: 'reply-hook-route',
          name: 'Reply hook',
          updatedAt: '2026-07-01T00:00:00Z',
          urlFingerprint: 'sha256:route',
          urlHost: 'hooks.example.com',
        })
      }),
      http.get('/fb/v1/console/reply-send-hook/health', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          accepted: '0',
          dead: '0',
          failed: '0',
          pending: '0',
          retryable: '0',
          total: '0',
        })
      }),
      http.get('/fb/v1/console/system/preflight', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          checks: [],
          elapsed: '1ms',
          status: 'pass',
        })
      }),
      http.get('/fb/v1/console/request-notifications/settings', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          contactDailySendLimit: 10,
          createdAt: '2026-07-16T00:00:00Z',
          defaultConsentMode: 'explicit_opt_in',
          emailEnabled: true,
          enabledEventTypes: { 'request.status_changed': true },
          maxRecipientsWithoutConfirm: 100,
          requirePublicUpdateForStatus: true,
          statusPolicy: { shipped: true },
          tenantHourlySendLimit: 1000,
          tenantId: 'tenant-1',
          updatedAt: '2026-07-16T00:00:00Z',
          updatedBy: 'admin-1',
          webhookEnabled: true,
        })
      }),
      http.get('/fb/v1/console/request-notifications/webhook-targets', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          targets: [
            {
              createdAt: '2026-07-16T00:00:00Z',
              id: 'rn-target-route',
              includeRecipientIdentity: true,
              name: 'Route target',
              signatureVersion: 'v1',
              status: 'active',
              updatedAt: '2026-07-16T00:00:00Z',
              url: 'https://hooks.example.test/request-notifications',
              urlHost: 'hooks.example.test',
            },
          ],
        })
      }),
      http.get('/fb/v1/console/request-notifications/deliveries', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ deliveries: [] })
      }),
      http.get('/fb/v1/console/external-sync/connections', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ connections: [] })
      }),
      http.get('/fb/v1/console/external-sync/events', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ events: [], nextBeforeId: '' })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = SecurityRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toHaveLength(19)
    expect(SecurityRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(
      new Set([
        '/fb/v1/console/auth/sso/mode',
        '/fb/v1/console/members',
        '/fb/v1/console/audit-log',
        '/fb/v1/console/public-visibility/policy',
        '/fb/v1/console/public-visibility/moderation',
        '/fb/v1/console/gdpr/operations',
        '/fb/v1/console/notify-targets',
        '/fb/v1/console/api-keys',
        '/fb/v1/console/inbound/sources',
        '/fb/v1/console/llm/channels',
        '/fb/v1/console/reply-send-hook',
        '/fb/v1/console/reply-send-hook/health',
        '/fb/v1/console/system/preflight',
        '/fb/v1/console/request-notifications/settings',
        '/fb/v1/console/request-notifications/webhook-targets',
        '/fb/v1/console/request-notifications/deliveries',
        '/fb/v1/console/external-sync/connections',
        '/fb/v1/console/external-sync/events',
      ]),
    )
    expect(auditURL?.searchParams.getAll('action')).toEqual([
      'member.invite',
      'member.remove',
      'member.update_role',
    ])
    expect(auditURL?.searchParams.get('targetType')).toBe('member')
    expect(auditURL?.searchParams.get('limit')).toBe('20')
    expect(moderationURL?.searchParams.get('limit')).toBe('50')
    expect(fieldAuditURL?.searchParams.getAll('action')).toEqual([
      'moderation.approve',
      'moderation.reject',
      'moderation.hide',
      'moderation.mark_spam',
      'moderation.restore',
    ])
    expect(fieldAuditURL?.searchParams.get('targetType')).toBe('public_moderation_subject')
    expect(fieldAuditURL?.searchParams.get('limit')).toBe('20')
  })
})
