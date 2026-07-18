import { isRedirect } from '@tanstack/react-router'
import { describe, expect, it } from 'vitest'
import { Route as GuardPoliciesRoute } from '@/routes/_authed.administration.guard-policies'
import { Route as MembersRoute } from '@/routes/_authed.administration.members'
import { Route as LegacyApiKeysRoute } from '@/routes/_authed.api-keys'
import { Route as LegacyClustersRoute } from '@/routes/_authed.clusters'
import { Route as EnrichmentRuntimeRoute } from '@/routes/_authed.configuration.enrichment-runtime'
import { Route as LLMConfigurationRoute } from '@/routes/_authed.configuration.llm'
import { Route as TagsConfigurationRoute } from '@/routes/_authed.configuration.tags'
import { Route as WorkflowConfigurationRoute } from '@/routes/_authed.configuration.workflow'
import { Route as LegacyGuardPoliciesRoute } from '@/routes/_authed.guard-policies'
import { Route as LegacyIndexRoute } from '@/routes/_authed.index'
import { Route as DigestIntegrationRoute } from '@/routes/_authed.integrations.digests'
import { Route as NotifyTargetsRoute } from '@/routes/_authed.integrations.notify-targets'
import { Route as LegacyLLMConfigRoute } from '@/routes/_authed.llm-config'
import { Route as MCPClientsRoute } from '@/routes/_authed.mcp-clients'
import { Route as LegacyNotifyTargetsRoute } from '@/routes/_authed.notify-targets'
import { Route as LegacyOutboxDeadRoute } from '@/routes/_authed.outbox-dead'

interface ThrownRedirect {
  options: { to: string }
}

function captureRedirect(beforeLoad: (() => unknown) | undefined) {
  try {
    beforeLoad?.()
    return null
  } catch (err) {
    return err
  }
}

describe('route smoke coverage', () => {
  it.each([
    [LegacyApiKeysRoute, '/integrations/api-keys'],
    [LegacyClustersRoute, '/feedback/clusters'],
    [LegacyGuardPoliciesRoute, '/administration/guard-policies'],
    [LegacyIndexRoute, '/control-tower'],
    [LegacyLLMConfigRoute, '/configuration/llm'],
    [LegacyNotifyTargetsRoute, '/integrations/notify-targets'],
    [LegacyOutboxDeadRoute, '/administration/dead-deliveries'],
  ])('redirects legacy route to %s', (route, to) => {
    const thrown = captureRedirect(route.options.beforeLoad as (() => unknown) | undefined)

    expect(isRedirect(thrown)).toBe(true)
    expect((thrown as ThrownRedirect).options.to).toBe(to)
  })

  it.each([
    GuardPoliciesRoute,
    MembersRoute,
    EnrichmentRuntimeRoute,
    LLMConfigurationRoute,
    TagsConfigurationRoute,
    WorkflowConfigurationRoute,
    DigestIntegrationRoute,
    NotifyTargetsRoute,
    MCPClientsRoute,
  ])('registers a component for route %s', (route) => {
    expect(route.options.component).toBeTypeOf('function')
  })
})
