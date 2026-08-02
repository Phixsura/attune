import { describe, expect, it, vi } from 'vitest'
import { Route as AdministrationRoute } from './_authed.administration'
import { Route as AuditLogRoute } from './_authed.administration.audit-log'
import { Route as DeadDeliveriesRoute } from './_authed.administration.dead-deliveries'
import { Route as GDPRRoute } from './_authed.administration.gdpr'
import { Route as GuardPoliciesRoute } from './_authed.administration.guard-policies'
import { Route as MembersRoute } from './_authed.administration.members'
import { Route as ReliabilityRoute } from './_authed.administration.reliability'
import { Route as SecurityRoute } from './_authed.administration.security'
import { Route as SystemReadinessRoute } from './_authed.administration.system-readiness'
import { Route as AnalyticsRoute } from './_authed.analytics'
import { Route as ClassificationQualityRoute } from './_authed.analytics.classification-quality'
import { Route as LLMUsageRoute } from './_authed.analytics.llm-usage'
import { Route as SearchQualityRoute } from './_authed.analytics.search-quality'
import { Route as UsageRoute } from './_authed.analytics.usage'
import { Route as LegacyAPIKeysRoute } from './_authed.api-keys'
import { Route as LegacyClassificationQualityRoute } from './_authed.classification-quality'
import { Route as LegacyClustersRoute } from './_authed.clusters'
import { Route as ConfigurationRoute } from './_authed.configuration'
import { Route as ClassificationRoute } from './_authed.configuration.classification'
import { Route as EnrichmentRuntimeRoute } from './_authed.configuration.enrichment-runtime'
import { Route as LLMConfigurationRoute } from './_authed.configuration.llm'
import { Route as TagsRoute } from './_authed.configuration.tags'
import { Route as WorkflowRoute } from './_authed.configuration.workflow'
import { Route as ControlTowerRoute } from './_authed.control-tower'
import { Route as FeedbackRoute } from './_authed.feedback'
import { Route as CustomerRequestsRoute } from './_authed.feedback.customer-requests'
import { Route as FeedbackIndexRoute } from './_authed.feedback.index'
import { Route as FeedbackPortalRoute } from './_authed.feedback.portal'
import { Route as TerminalFailuresRoute } from './_authed.feedback.terminal-failures'
import { Route as LegacyGuardPoliciesRoute } from './_authed.guard-policies'
import { Route as LegacyInboundSourcesRoute } from './_authed.inbound-sources'
import { Route as AuthedIndexRoute } from './_authed.index'
import { Route as IntegrationsRoute } from './_authed.integrations'
import { Route as APIKeysIntegrationRoute } from './_authed.integrations.api-keys'
import { Route as DigestRoute } from './_authed.integrations.digests'
import { Route as ExternalSyncRoute } from './_authed.integrations.external-sync'
import { Route as InboundSourcesRoute } from './_authed.integrations.inbound-sources'
import { Route as NotifyTargetsRoute } from './_authed.integrations.notify-targets'
import { Route as PublicVisibilityRoute } from './_authed.integrations.public-visibility'
import { Route as ReplySendHookRoute } from './_authed.integrations.reply-send-hook'
import { Route as RequestNotificationsRoute } from './_authed.integrations.request-notifications'
import { Route as SurveysRoute } from './_authed.integrations.surveys'
import { Route as LegacyLLMConfigRoute } from './_authed.llm-config'
import { Route as LegacyLLMUsageRoute } from './_authed.llm-usage'
import { Route as MCPClientsRoute } from './_authed.mcp-clients'
import { Route as LegacyNotifyTargetsRoute } from './_authed.notify-targets'
import { Route as LegacyOutboxDeadRoute } from './_authed.outbox-dead'
import { Route as LegacySearchQualityRoute } from './_authed.search-quality'
import { Route as LegacySettingsRoute } from './_authed.settings'
import { Route as LoginRoute } from './login'
import { Route as LoginErrorRoute } from './login_.error'

const requireRouteAccessMock = vi.hoisted(() => vi.fn())
const resolveGroupDefaultMock = vi.hoisted(() =>
  vi.fn((group: string, role: string) => `/${group}/${role}-default`),
)
const resolveLegacySettingsRedirectMock = vi.hoisted(() =>
  vi.fn((_search: Record<string, unknown>, role: string) => `/settings/${role}-default`),
)

vi.mock('@tanstack/react-router', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-router')>('@tanstack/react-router')

  return {
    ...actual,
    createFileRoute: (path: string) => (options: Record<string, unknown>) => ({
      id: path,
      options,
    }),
    redirect: (options: Record<string, unknown>) => ({ redirect: options }),
  }
})

vi.mock('@/routes/-route-access', () => ({
  requireRouteAccess: requireRouteAccessMock,
  resolveGroupDefault: resolveGroupDefaultMock,
  resolveLegacySettingsRedirect: resolveLegacySettingsRedirectMock,
}))

type RouteLike = {
  options: {
    beforeLoad?: (args: {
      context: unknown
      location?: { pathname: string }
      search?: Record<string, unknown>
    }) => unknown
    loader?: (args: {
      context: { queryClient: { ensureQueryData: ReturnType<typeof vi.fn> } }
    }) => Promise<unknown> | unknown
    component?: unknown
    validateSearch?: (search: Record<string, unknown>) => unknown
  }
}

function routeOptions(route: unknown) {
  return (route as RouteLike).options
}

describe('route wiring coverage', () => {
  it.each([
    [AuditLogRoute, { permission: 'settings:audit_log:view' }],
    [ClassificationRoute, { permission: 'settings:enrich_config:view' }],
    [DeadDeliveriesRoute, { adminOnly: true }],
    [GDPRRoute, { permission: 'settings:gdpr:view' }],
    [GuardPoliciesRoute, { permission: 'settings:guard_policies:view' }],
    [MembersRoute, { permission: 'settings:members:view' }],
    [ReliabilityRoute, { adminOnly: true }],
    [SecurityRoute, { adminOnly: true }],
    [SystemReadinessRoute, { adminOnly: true }],
    [WorkflowRoute, { permission: 'settings:workflow:view' }],
    [TagsRoute, { permission: 'settings:tags:view' }],
    [LLMConfigurationRoute, { permission: 'llm_config:view' }],
    [EnrichmentRuntimeRoute, { permission: 'settings:enrichment_runtime:view' }],
    [ControlTowerRoute, { permission: 'usage:view' }],
    [CustomerRequestsRoute, { permission: 'customer_request:view' }],
    [APIKeysIntegrationRoute, { permission: 'settings:api_keys:view' }],
    [ExternalSyncRoute, { permission: 'settings:external_sync:view' }],
    [InboundSourcesRoute, { permission: 'settings:inbound:view' }],
    [NotifyTargetsRoute, { permission: 'settings:notify_targets:view' }],
    [PublicVisibilityRoute, { permission: 'moderation:view' }],
    [ReplySendHookRoute, { adminOnly: true }],
    [DigestRoute, { permission: 'settings:digest:view' }],
    [RequestNotificationsRoute, { adminOnly: true }],
    [SurveysRoute, { adminOnly: true }],
  ])('runs route access guard for %s', (route, access) => {
    const context = { queryClient: {} }

    routeOptions(route).beforeLoad?.({ context })

    expect(requireRouteAccessMock).toHaveBeenLastCalledWith(context, access)
  })

  it.each([
    [AdministrationRoute, '/administration', 'administration', { permission: 'nav:settings' }],
    [AnalyticsRoute, '/analytics', 'analytics', { permission: 'usage:view' }],
    [ConfigurationRoute, '/configuration', 'configuration', { permission: 'nav:settings' }],
    [IntegrationsRoute, '/integrations', 'integrations', { permission: 'nav:settings' }],
  ])('redirects %s group index to its role default', async (route, pathname, group, access) => {
    const context = { queryClient: {} }
    requireRouteAccessMock.mockResolvedValueOnce('admin')

    await expect(
      routeOptions(route).beforeLoad?.({ context, location: { pathname } }),
    ).rejects.toEqual({
      redirect: { to: `/${group}/admin-default` },
    })

    expect(requireRouteAccessMock).toHaveBeenLastCalledWith(context, access)
    expect(resolveGroupDefaultMock).toHaveBeenLastCalledWith(group, 'admin')
  })

  it.each([
    [AdministrationRoute, '/administration/members'],
    [AnalyticsRoute, '/analytics/usage'],
    [ConfigurationRoute, '/configuration/workflow'],
    [IntegrationsRoute, '/integrations/api-keys'],
  ])('leaves nested group route %s in place', async (route, pathname) => {
    const context = { queryClient: {} }

    await expect(
      routeOptions(route).beforeLoad?.({ context, location: { pathname } }),
    ).resolves.toBe(undefined)
  })

  it.each([
    [GuardPoliciesRoute, 1],
    [MembersRoute, 1],
    [DeadDeliveriesRoute, 1],
    [ReliabilityRoute, 15],
    [SecurityRoute, 19],
    [ClassificationQualityRoute, 1],
    [LLMUsageRoute, 1],
    [SearchQualityRoute, 1],
    [UsageRoute, 1],
    [WorkflowRoute, 1],
    [TagsRoute, 1],
    [LLMConfigurationRoute, 2],
    [EnrichmentRuntimeRoute, 1],
    [ControlTowerRoute, 5],
    [APIKeysIntegrationRoute, 1],
    [DigestRoute, 1],
    [ExternalSyncRoute, 2],
    [InboundSourcesRoute, 1],
    [NotifyTargetsRoute, 1],
    [PublicVisibilityRoute, 1],
    [ReplySendHookRoute, 1],
    [SurveysRoute, 4],
    [MCPClientsRoute, 1],
  ])('preloads route data for %s', async (route, expectedCalls) => {
    const ensureQueryData = vi.fn().mockResolvedValue(undefined)

    await routeOptions(route).loader?.({ context: { queryClient: { ensureQueryData } } })

    expect(ensureQueryData).toHaveBeenCalledTimes(expectedCalls)
  })

  it('preloads all request notification route data', async () => {
    const ensureQueryData = vi.fn().mockResolvedValue(undefined)

    await routeOptions(RequestNotificationsRoute).loader?.({
      context: { queryClient: { ensureQueryData } },
    })

    expect(ensureQueryData).toHaveBeenCalledTimes(5)
  })

  it.each([
    [AuthedIndexRoute, '/control-tower'],
    [LegacyAPIKeysRoute, '/integrations/api-keys'],
    [LegacyClassificationQualityRoute, '/analytics/classification-quality'],
    [LegacyClustersRoute, '/feedback/clusters'],
    [LegacyGuardPoliciesRoute, '/administration/guard-policies'],
    [LegacyInboundSourcesRoute, '/integrations/inbound-sources'],
    [LegacyLLMConfigRoute, '/configuration/llm'],
    [LegacyLLMUsageRoute, '/analytics/llm-usage'],
    [LegacyNotifyTargetsRoute, '/integrations/notify-targets'],
    [LegacyOutboxDeadRoute, '/administration/dead-deliveries'],
    [LegacySearchQualityRoute, '/analytics/search-quality'],
  ])('redirects legacy route %s to %s', (route, to) => {
    expect(() => routeOptions(route).beforeLoad?.({ context: {} })).toThrow(
      expect.objectContaining({ redirect: { to } }),
    )
  })

  it('exposes section route components', () => {
    expect(routeOptions(FeedbackRoute).component).toBeTypeOf('function')
    expect(routeOptions(FeedbackPortalRoute).component).toBeTypeOf('function')
    expect(routeOptions(TerminalFailuresRoute).component).toBeTypeOf('function')
  })

  it('validates feedback search params defensively', () => {
    expect(
      routeOptions(FeedbackIndexRoute).validateSearch?.({
        ids: '1,2',
        quality_signal: 42,
        confidence_lte: 0.75,
        created_from: {},
        created_to: '2026-07-01',
        enriched_from: Number.NaN,
        enriched_to: '2026-07-02',
      }),
    ).toEqual({
      ids: '1,2',
      quality_signal: 42,
      confidence_lte: 0.75,
      created_from: undefined,
      created_to: '2026-07-01',
      enriched_from: undefined,
      enriched_to: '2026-07-02',
    })
  })

  it('validates auth and legacy settings search params', () => {
    expect(routeOptions(LoginRoute).validateSearch?.({ redirect: '/feedback', other: 1 })).toEqual({
      redirect: '/feedback',
    })
    expect(routeOptions(LoginRoute).validateSearch?.({ redirect: 12 })).toEqual({
      redirect: undefined,
    })
    expect(
      routeOptions(LoginErrorRoute).validateSearch?.({
        code: 'state_expired',
        request_id: 'req-1',
        trace_id: 'trace-1',
        ignored: 1,
      }),
    ).toEqual({
      code: 'state_expired',
      request_id: 'req-1',
      trace_id: 'trace-1',
    })

    const search = { tab: 'audit', section: 'members' }
    expect(routeOptions(LegacySettingsRoute).validateSearch?.(search)).toBe(search)
  })

  it('redirects legacy settings searches through the IA resolver', async () => {
    const context = { queryClient: {} }
    const search = { section: 'workflow' }
    requireRouteAccessMock.mockResolvedValueOnce('admin')
    resolveLegacySettingsRedirectMock.mockReturnValueOnce('/configuration/workflow')

    await expect(
      routeOptions(LegacySettingsRoute).beforeLoad?.({ context, search }),
    ).rejects.toEqual({
      redirect: { to: '/configuration/workflow' },
    })

    expect(requireRouteAccessMock).toHaveBeenLastCalledWith(context, { permission: 'nav:settings' })
    expect(resolveLegacySettingsRedirectMock).toHaveBeenLastCalledWith(search, 'admin')
  })
})
