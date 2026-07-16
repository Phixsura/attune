import { QueryClient } from '@tanstack/react-query'
import { isRedirect } from '@tanstack/react-router'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { Route as AdministrationRoute } from '@/routes/_authed.administration'
import { Route as AuditLogRoute } from '@/routes/_authed.administration.audit-log'
import { Route as DeadDeliveriesRoute } from '@/routes/_authed.administration.dead-deliveries'
import { Route as GDPRRoute } from '@/routes/_authed.administration.gdpr'
import { Route as GuardPoliciesAdminRoute } from '@/routes/_authed.administration.guard-policies'
import { Route as MembersRoute } from '@/routes/_authed.administration.members'
import { Route as ReliabilityRoute } from '@/routes/_authed.administration.reliability'
import { Route as SecurityRoute } from '@/routes/_authed.administration.security'
import { Route as SystemReadinessRoute } from '@/routes/_authed.administration.system-readiness'
import { Route as AnalyticsRoute } from '@/routes/_authed.analytics'
import { Route as AnalyticsClassificationQualityRoute } from '@/routes/_authed.analytics.classification-quality'
import { Route as AnalyticsLLMUsageRoute } from '@/routes/_authed.analytics.llm-usage'
import { Route as AnalyticsSearchQualityRoute } from '@/routes/_authed.analytics.search-quality'
import { Route as AnalyticsUsageRoute } from '@/routes/_authed.analytics.usage'
import { Route as LegacyApiKeysRoute } from '@/routes/_authed.api-keys'
import { Route as LegacyChangePasswordRoute } from '@/routes/_authed.change-password'
import { Route as ConfigurationRoute } from '@/routes/_authed.configuration'
import { Route as EnrichmentRuntimeRoute } from '@/routes/_authed.configuration.enrichment-runtime'
import { Route as LLMConfigRoute } from '@/routes/_authed.configuration.llm'
import { Route as TagsRoute } from '@/routes/_authed.configuration.tags'
import { Route as WorkflowRoute } from '@/routes/_authed.configuration.workflow'
import { Route as ControlTowerRoute } from '@/routes/_authed.control-tower'
import { Route as FeedbackRootRoute } from '@/routes/_authed.feedback'
import { Route as FeedbackClustersRoute } from '@/routes/_authed.feedback.clusters'
import { Route as CustomerRequestsRoute } from '@/routes/_authed.feedback.customer-requests'
import { Route as FeedbackIndexRoute } from '@/routes/_authed.feedback.index'
import { Route as FeedbackPortalRoute } from '@/routes/_authed.feedback.portal'
import { Route as TerminalFailuresRoute } from '@/routes/_authed.feedback.terminal-failures'
import { Route as LegacyGuardPoliciesRoute } from '@/routes/_authed.guard-policies'
import { Route as InboundSourcesRoute } from '@/routes/_authed.inbound-sources'
import { Route as AuthedIndexRoute } from '@/routes/_authed.index'
import { Route as IntegrationsRoute } from '@/routes/_authed.integrations'
import { Route as ApiKeysRoute } from '@/routes/_authed.integrations.api-keys'
import { Route as DigestRoute } from '@/routes/_authed.integrations.digests'
import { Route as ExternalSyncRoute } from '@/routes/_authed.integrations.external-sync'
import { Route as IntegrationsInboundSourcesRoute } from '@/routes/_authed.integrations.inbound-sources'
import { Route as NotifyTargetsRoute } from '@/routes/_authed.integrations.notify-targets'
import { Route as PublicVisibilityRoute } from '@/routes/_authed.integrations.public-visibility'
import { Route as ReplySendHookRoute } from '@/routes/_authed.integrations.reply-send-hook'
import { Route as RequestNotificationsRoute } from '@/routes/_authed.integrations.request-notifications'
import { Route as LegacyLLMConfigRoute } from '@/routes/_authed.llm-config'
import { Route as MCPClientsRoute } from '@/routes/_authed.mcp-clients'
import { Route as LegacyNotifyTargetsRoute } from '@/routes/_authed.notify-targets'
import { Route as LegacyOutboxDeadRoute } from '@/routes/_authed.outbox-dead'
import { Route as LegacySearchQualityRoute } from '@/routes/_authed.search-quality'
import { Route as SettingsRoute } from '@/routes/_authed.settings'
import { server } from '@/testing/mocks/server'

type BeforeLoadFn = (...args: any[]) => unknown
type LoaderFn = (args: { context: { queryClient: QueryClient } }) => Promise<unknown>

interface ThrownRedirect {
  options: { to: string; statusCode?: number }
}

function mockMe(role: 'admin' | 'delegated_admin' | 'member' | 'viewer') {
  server.use(
    http.get('/fb/v1/console/me', () =>
      HttpResponse.json({
        tenant: { id: 't', name: 'T', slug: 't', locale: 'zh-CN', timezone: 'UTC' },
        user: { openId: `${role}-user`, name: role, role },
        csrfToken: 'tok',
      }),
    ),
  )
}

async function callBeforeLoad(
  beforeLoad: BeforeLoadFn | undefined,
  extra: Record<string, unknown> = {},
): Promise<unknown> {
  if (!beforeLoad) return null
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  try {
    await beforeLoad({
      context: { queryClient },
      ...extra,
    })
    return null
  } catch (err) {
    return err
  }
}

describe('route access guards', () => {
  it('redirects viewers away from integrations api keys', async () => {
    mockMe('viewer')
    const thrown = await callBeforeLoad(ApiKeysRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')
  })

  it('redirects the legacy inbound sources entrypoint to the integrations page', async () => {
    const thrown = await callBeforeLoad(InboundSourcesRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/integrations/inbound-sources')
  })

  it('allows members into integrations api keys', async () => {
    mockMe('member')
    expect(await callBeforeLoad(ApiKeysRoute.options.beforeLoad)).toBeNull()
  })

  it('preloads only the api key registry for integrations api keys', async () => {
    mockMe('member')
    expect(await callBeforeLoad(ApiKeysRoute.options.beforeLoad)).toBeNull()

    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/api-keys', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
      http.get('/fb/v1/console/service-accounts', () => {
        throw new Error('service accounts should not be preloaded by the api keys route loader')
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = ApiKeysRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toEqual([])
    expect(seenPaths).toEqual(new Set(['/fb/v1/console/api-keys']))
  })

  it('allows members to view public visibility moderation but redirects viewers', async () => {
    mockMe('member')
    expect(await callBeforeLoad(PublicVisibilityRoute.options.beforeLoad)).toBeNull()

    mockMe('viewer')
    const thrown = await callBeforeLoad(PublicVisibilityRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')
  })

  it('preloads the public visibility moderation queue for the integration route', async () => {
    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/public-visibility/moderation', ({ request }) => {
        const url = new URL(request.url)
        seenPaths.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({ subjects: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = PublicVisibilityRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toBeUndefined()
    expect(PublicVisibilityRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(new Set(['/fb/v1/console/public-visibility/moderation?limit=50']))
  })

  it('preloads inbound sources for the integrations page and exposes a component', async () => {
    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/inbound/sources', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = IntegrationsInboundSourcesRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toEqual([])
    expect(IntegrationsInboundSourcesRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(new Set(['/fb/v1/console/inbound/sources']))
  })

  it('redirects members away from GDPR administration pages', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(GDPRRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')
  })

  it('allows delegated admins into GDPR administration pages', async () => {
    mockMe('delegated_admin')
    expect(await callBeforeLoad(GDPRRoute.options.beforeLoad)).toBeNull()
  })

  it('keeps dead deliveries admin-only', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(DeadDeliveriesRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(DeadDeliveriesRoute.options.beforeLoad)).toBeNull()
  })

  it('keeps the reliability summary admin-only', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(ReliabilityRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(ReliabilityRoute.options.beforeLoad)).toBeNull()
  })

  it('keeps the security emergency access page admin-only', async () => {
    mockMe('delegated_admin')
    const thrown = await callBeforeLoad(SecurityRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(SecurityRoute.options.beforeLoad)).toBeNull()
  })

  it('keeps reply send hook configuration admin-only', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(ReplySendHookRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(ReplySendHookRoute.options.beforeLoad)).toBeNull()
  })

  it('keeps request notifications admin-only and preloads notification settings', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(RequestNotificationsRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('admin')
    expect(await callBeforeLoad(RequestNotificationsRoute.options.beforeLoad)).toBeNull()

    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/request-notifications/settings', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          appNotificationsEnabled: true,
          emailNotificationsEnabled: false,
        })
      }),
      http.get('/fb/v1/console/request-notifications/sender', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ id: 'sender-1', fromEmail: 'updates@example.test' })
      }),
      http.get('/fb/v1/console/request-notifications/webhook-targets', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ targets: [] })
      }),
      http.get('/fb/v1/console/request-notifications/deliveries', ({ request }) => {
        const url = new URL(request.url)
        seenPaths.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({ deliveries: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = RequestNotificationsRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toBeUndefined()
    expect(RequestNotificationsRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(
      new Set([
        '/fb/v1/console/request-notifications/settings',
        '/fb/v1/console/request-notifications/sender',
        '/fb/v1/console/request-notifications/webhook-targets',
        '/fb/v1/console/request-notifications/deliveries?limit=25',
      ]),
    )
  })

  it('keeps external sync operational-only and preloads its console shell data', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(ExternalSyncRoute.options.beforeLoad)
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('delegated_admin')
    expect(await callBeforeLoad(ExternalSyncRoute.options.beforeLoad)).toBeNull()

    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/external-sync/health', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({
          enabledConnections: 1,
          failingConnections: 0,
          staleConnections: 0,
          activeRuns: 0,
          retryableRuns: 0,
          deadRuns: 0,
          openConflicts: 0,
          newestSuccessfulRunAt: '',
        })
      }),
      http.get('/fb/v1/console/external-sync/connections', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ connections: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const loader = ExternalSyncRoute.options.loader as (args: {
      context: { queryClient: QueryClient }
    }) => Promise<unknown>

    await expect(loader({ context: { queryClient } })).resolves.toBeUndefined()
    expect(ExternalSyncRoute.options.component).toBeTypeOf('function')
    expect(seenPaths).toEqual(
      new Set(['/fb/v1/console/external-sync/health', '/fb/v1/console/external-sync/connections']),
    )
  })

  it('allows admins into leaf configuration and integration routes', async () => {
    mockMe('admin')

    for (const route of [
      MembersRoute,
      TagsRoute,
      WorkflowRoute,
      LLMConfigRoute,
      EnrichmentRuntimeRoute,
      DigestRoute,
      NotifyTargetsRoute,
    ]) {
      await expect(callBeforeLoad(route.options.beforeLoad)).resolves.toBeNull()
      expect(route.options.component).toBeTypeOf('function')
    }
  })

  it('preloads simple leaf route data', async () => {
    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/members', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ members: [] })
      }),
      http.get('/fb/v1/console/tags', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ tags: [] })
      }),
      http.get('/fb/v1/console/workflow/states', ({ request }) => {
        const url = new URL(request.url)
        seenPaths.add(`${url.pathname}${url.search}`)
        return HttpResponse.json({ states: [] })
      }),
      http.get('/fb/v1/console/notify-targets', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const context = { queryClient }
    const membersLoader = MembersRoute.options.loader as LoaderFn
    const tagsLoader = TagsRoute.options.loader as LoaderFn
    const workflowLoader = WorkflowRoute.options.loader as LoaderFn
    const notifyTargetsLoader = NotifyTargetsRoute.options.loader as LoaderFn

    await expect(membersLoader({ context })).resolves.toEqual([])
    await expect(tagsLoader({ context })).resolves.toEqual([])
    await expect(workflowLoader({ context })).resolves.toEqual([])
    await expect(notifyTargetsLoader({ context })).resolves.toEqual([])
    expect(seenPaths).toEqual(
      new Set([
        '/fb/v1/console/members',
        '/fb/v1/console/tags',
        '/fb/v1/console/workflow/states?include_archived=true',
        '/fb/v1/console/notify-targets',
      ]),
    )
  })

  it('maps legacy settings deep links to the first section a member can still access', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(SettingsRoute.options.beforeLoad, {
      search: { section: 'gdpr' },
    })
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/configuration/classification')
  })

  it('keeps legacy settings deep links for allowed sections', async () => {
    mockMe('member')
    const thrown = await callBeforeLoad(SettingsRoute.options.beforeLoad, {
      search: { section: 'api-keys' },
    })
    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe('/integrations/api-keys')
  })

  it('redirects group root routes to the first page the role can access', async () => {
    mockMe('member')

    const analytics = await callBeforeLoad(AnalyticsRoute.options.beforeLoad, {
      location: { pathname: '/analytics' },
    })
    expect(isRedirect(analytics)).toBe(true)
    expect((analytics as ThrownRedirect).options.to).toBe('/analytics/usage')

    const configuration = await callBeforeLoad(ConfigurationRoute.options.beforeLoad, {
      location: { pathname: '/configuration' },
    })
    expect(isRedirect(configuration)).toBe(true)
    expect((configuration as ThrownRedirect).options.to).toBe('/configuration/classification')

    const integrations = await callBeforeLoad(IntegrationsRoute.options.beforeLoad, {
      location: { pathname: '/integrations' },
    })
    expect(isRedirect(integrations)).toBe(true)
    expect((integrations as ThrownRedirect).options.to).toBe('/integrations/inbound-sources')

    const administration = await callBeforeLoad(AdministrationRoute.options.beforeLoad, {
      location: { pathname: '/administration' },
    })
    expect(isRedirect(administration)).toBe(true)
    expect((administration as ThrownRedirect).options.to).toBe('/administration/members')
  })

  it('leaves group child routes alone', async () => {
    for (const [route, pathname] of [
      [AnalyticsRoute, '/analytics/usage'],
      [ConfigurationRoute, '/configuration/classification'],
      [IntegrationsRoute, '/integrations/inbound-sources'],
      [AdministrationRoute, '/administration/members'],
    ] as const) {
      await expect(
        callBeforeLoad(route.options.beforeLoad, {
          location: { pathname },
        }),
      ).resolves.toBeNull()
    }
  })

  it('routes delegated admins to operational settings pages', async () => {
    mockMe('delegated_admin')

    const configuration = await callBeforeLoad(ConfigurationRoute.options.beforeLoad, {
      location: { pathname: '/configuration' },
    })
    expect(isRedirect(configuration)).toBe(true)
    expect((configuration as ThrownRedirect).options.to).toBe('/configuration/classification')

    const integrations = await callBeforeLoad(IntegrationsRoute.options.beforeLoad, {
      location: { pathname: '/integrations' },
    })
    expect(isRedirect(integrations)).toBe(true)
    expect((integrations as ThrownRedirect).options.to).toBe('/integrations/inbound-sources')

    const administration = await callBeforeLoad(AdministrationRoute.options.beforeLoad, {
      location: { pathname: '/administration' },
    })
    expect(isRedirect(administration)).toBe(true)
    expect((administration as ThrownRedirect).options.to).toBe('/administration/audit-log')
  })

  it('redirects legacy top-level routes to their grouped destinations', async () => {
    const cases: Array<{ route: { options: { beforeLoad?: BeforeLoadFn } }; to: string }> = [
      { route: AuthedIndexRoute, to: '/control-tower' },
      { route: LegacyApiKeysRoute, to: '/integrations/api-keys' },
      { route: LegacyGuardPoliciesRoute, to: '/administration/guard-policies' },
      { route: LegacyLLMConfigRoute, to: '/configuration/llm' },
      { route: LegacyNotifyTargetsRoute, to: '/integrations/notify-targets' },
      { route: LegacyOutboxDeadRoute, to: '/administration/dead-deliveries' },
      { route: LegacySearchQualityRoute, to: '/analytics/search-quality' },
    ]

    for (const { route, to } of cases) {
      const thrown = await callBeforeLoad(route.options.beforeLoad)
      expect(isRedirect(thrown)).toBe(true)
      expect((thrown as ThrownRedirect).options.to).toBe(to)
    }
  })

  it('covers additional leaf route access checks and components', async () => {
    mockMe('member')
    const adminOnlyRoutes = [
      SystemReadinessRoute,
      GuardPoliciesAdminRoute,
      AnalyticsClassificationQualityRoute,
      AnalyticsSearchQualityRoute,
      AnalyticsUsageRoute,
      AnalyticsLLMUsageRoute,
      FeedbackClustersRoute,
      FeedbackPortalRoute,
      TerminalFailuresRoute,
      FeedbackRootRoute,
      FeedbackIndexRoute,
      MCPClientsRoute,
      LegacyChangePasswordRoute,
    ]

    for (const route of adminOnlyRoutes) {
      expect(route.options.component).toBeTypeOf('function')
    }

    const systemReadiness = await callBeforeLoad(SystemReadinessRoute.options.beforeLoad)
    expect(isRedirect(systemReadiness)).toBe(true)
    expect((systemReadiness as ThrownRedirect).options.to).toBe('/feedback')

    mockMe('viewer')
    for (const route of [GuardPoliciesAdminRoute, AuditLogRoute]) {
      const thrown = await callBeforeLoad(route.options.beforeLoad)
      expect(isRedirect(thrown)).toBe(true)
      expect((thrown as ThrownRedirect).options.to).toBe('/feedback')
    }

    mockMe('admin')
    for (const route of [SystemReadinessRoute, GuardPoliciesAdminRoute, AuditLogRoute]) {
      await expect(callBeforeLoad(route.options.beforeLoad)).resolves.toBeNull()
    }

    mockMe('viewer')
    await expect(callBeforeLoad(CustomerRequestsRoute.options.beforeLoad)).resolves.toBeNull()

    mockMe('member')
    await expect(callBeforeLoad(CustomerRequestsRoute.options.beforeLoad)).resolves.toBeNull()
    expect(CustomerRequestsRoute.options.component).toBeTypeOf('function')
  })

  it('preloads analytics, guard policy, control tower, and mcp route data', async () => {
    const seenPaths = new Set<string>()
    server.use(
      http.get('/fb/v1/console/classification-quality', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({})
      }),
      http.get('/fb/v1/console/feedback/search/quality', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({})
      }),
      http.get('/fb/v1/console/quality-actions', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ actions: [] })
      }),
      http.get('/fb/v1/console/usage', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({})
      }),
      http.get('/fb/v1/console/llm-usage', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({})
      }),
      http.get('/fb/v1/console/guard-policies', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ items: [] })
      }),
      http.get('/fb/v1/console/mcp/clients', ({ request }) => {
        seenPaths.add(new URL(request.url).pathname)
        return HttpResponse.json({ clients: [] })
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const context = { queryClient }
    const loaders = [
      AnalyticsClassificationQualityRoute.options.loader,
      AnalyticsSearchQualityRoute.options.loader,
      AnalyticsUsageRoute.options.loader,
      AnalyticsLLMUsageRoute.options.loader,
      GuardPoliciesAdminRoute.options.loader,
      MCPClientsRoute.options.loader,
      ControlTowerRoute.options.loader,
    ] as LoaderFn[]

    for (const loader of loaders) {
      await expect(loader({ context })).resolves.toBeDefined()
    }
    expect(seenPaths).toEqual(
      new Set([
        '/fb/v1/console/classification-quality',
        '/fb/v1/console/feedback/search/quality',
        '/fb/v1/console/quality-actions',
        '/fb/v1/console/usage',
        '/fb/v1/console/llm-usage',
        '/fb/v1/console/guard-policies',
        '/fb/v1/console/mcp/clients',
      ]),
    )
  })
})
