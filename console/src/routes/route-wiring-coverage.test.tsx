import { describe, expect, it, vi } from 'vitest'
import { Route as AuditLogRoute } from './_authed.administration.audit-log'
import { Route as DeadDeliveriesRoute } from './_authed.administration.dead-deliveries'
import { Route as GDPRRoute } from './_authed.administration.gdpr'
import { Route as GuardPoliciesRoute } from './_authed.administration.guard-policies'
import { Route as MembersRoute } from './_authed.administration.members'
import { Route as ReliabilityRoute } from './_authed.administration.reliability'
import { Route as SecurityRoute } from './_authed.administration.security'
import { Route as SystemReadinessRoute } from './_authed.administration.system-readiness'
import { Route as ClassificationQualityRoute } from './_authed.analytics.classification-quality'
import { Route as LLMUsageRoute } from './_authed.analytics.llm-usage'
import { Route as SearchQualityRoute } from './_authed.analytics.search-quality'
import { Route as UsageRoute } from './_authed.analytics.usage'
import { Route as LegacyAPIKeysRoute } from './_authed.api-keys'
import { Route as LegacyClassificationQualityRoute } from './_authed.classification-quality'
import { Route as LegacyClustersRoute } from './_authed.clusters'
import { Route as ClassificationRoute } from './_authed.configuration.classification'
import { Route as EnrichmentRuntimeRoute } from './_authed.configuration.enrichment-runtime'
import { Route as LLMConfigurationRoute } from './_authed.configuration.llm'
import { Route as TagsRoute } from './_authed.configuration.tags'
import { Route as WorkflowRoute } from './_authed.configuration.workflow'
import { Route as ControlTowerRoute } from './_authed.control-tower'
import { Route as CustomerRequestsRoute } from './_authed.feedback.customer-requests'
import { Route as LegacyGuardPoliciesRoute } from './_authed.guard-policies'
import { Route as LegacyInboundSourcesRoute } from './_authed.inbound-sources'
import { Route as AuthedIndexRoute } from './_authed.index'
import { Route as APIKeysIntegrationRoute } from './_authed.integrations.api-keys'
import { Route as DigestRoute } from './_authed.integrations.digests'
import { Route as ExternalSyncRoute } from './_authed.integrations.external-sync'
import { Route as InboundSourcesRoute } from './_authed.integrations.inbound-sources'
import { Route as NotifyTargetsRoute } from './_authed.integrations.notify-targets'
import { Route as PublicVisibilityRoute } from './_authed.integrations.public-visibility'
import { Route as ReplySendHookRoute } from './_authed.integrations.reply-send-hook'
import { Route as RequestNotificationsRoute } from './_authed.integrations.request-notifications'
import { Route as LegacyLLMConfigRoute } from './_authed.llm-config'
import { Route as LegacyLLMUsageRoute } from './_authed.llm-usage'
import { Route as MCPClientsRoute } from './_authed.mcp-clients'

const requireRouteAccessMock = vi.hoisted(() => vi.fn())

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
}))

type RouteLike = {
  options: {
    beforeLoad?: (args: { context: unknown }) => unknown
    loader?: (args: {
      context: { queryClient: { ensureQueryData: ReturnType<typeof vi.fn> } }
    }) => Promise<unknown> | unknown
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
  ])('runs route access guard for %s', (route, access) => {
    const context = { queryClient: {} }

    routeOptions(route).beforeLoad?.({ context })

    expect(requireRouteAccessMock).toHaveBeenLastCalledWith(context, access)
  })

  it.each([
    [GuardPoliciesRoute, 1],
    [MembersRoute, 1],
    [DeadDeliveriesRoute, 1],
    [ReliabilityRoute, 9],
    [ClassificationQualityRoute, 1],
    [LLMUsageRoute, 1],
    [SearchQualityRoute, 1],
    [UsageRoute, 1],
    [WorkflowRoute, 1],
    [TagsRoute, 1],
    [LLMConfigurationRoute, 2],
    [EnrichmentRuntimeRoute, 1],
    [ControlTowerRoute, 3],
    [APIKeysIntegrationRoute, 1],
    [DigestRoute, 1],
    [ExternalSyncRoute, 2],
    [InboundSourcesRoute, 1],
    [NotifyTargetsRoute, 1],
    [PublicVisibilityRoute, 1],
    [ReplySendHookRoute, 1],
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

    expect(ensureQueryData).toHaveBeenCalledTimes(4)
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
  ])('redirects legacy route %s to %s', (route, to) => {
    expect(() => routeOptions(route).beforeLoad?.({ context: {} })).toThrow(
      expect.objectContaining({ redirect: { to } }),
    )
  })
})
