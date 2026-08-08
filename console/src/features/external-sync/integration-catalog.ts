import type {
  ExternalConnection,
  ExternalSyncEvent,
  ExternalSyncHealthResponse,
} from '@/features/external-sync/api/external-sync'

export type IntegrationCatalogStatus = 'verified' | 'watch' | 'blocked' | 'needs_data'

export type IntegrationCatalogLaneKey =
  | 'catalog_cards'
  | 'install_status'
  | 'permission_scope'
  | 'health_badge'
  | 'sample_replay'
  | 'upgrade_path'

export type IntegrationCatalogInstallStatus = 'installed' | 'available' | 'beta' | 'requires_setup'

export type IntegrationCatalogHealthBadge = 'healthy' | 'ready' | 'watch' | 'degraded'

export type IntegrationCatalogLane = {
  actionLabel: string
  detail: string
  evidence: string
  key: IntegrationCatalogLaneKey
  owner: string
  signal: string
  status: IntegrationCatalogStatus
  title: string
}

export type IntegrationCatalogConnectorDefinition = {
  authTypes: string[]
  category: string
  dataClasses: string[]
  description: string
  displayName: string
  docsHref: string
  healthBadge: IntegrationCatalogHealthBadge
  healthSignals: string[]
  id: string
  installStatus: IntegrationCatalogInstallStatus
  owner: string
  scopes: string[]
  setupChecks: string[]
  supportTier: string
  replayEvent: string
  replayFixture: string
  replayNormalizedType: string
  replayAction: string
  upgradeCompatibility: 'non_breaking' | 'requires_migration'
  upgradePath: string
  upgradeRollback: boolean
  version: string
}

export type IntegrationCatalogConnector = IntegrationCatalogConnectorDefinition & {
  liveConnectionName?: string
  runtimeHealthBadge: IntegrationCatalogHealthBadge
  runtimeInstallStatus: IntegrationCatalogInstallStatus
}

export type IntegrationCatalog = {
  connectors: IntegrationCatalogConnector[]
  fingerprint: string
  lanes: IntegrationCatalogLane[]
  summary: string
  totals: Record<IntegrationCatalogStatus, number> & {
    total: number
  }
}

export type IntegrationCatalogArtifacts = {
  catalogVerifier: boolean
  connectors: IntegrationCatalogConnectorDefinition[]
  expectedConnectorIds: string[]
}

export type IntegrationCatalogInput = {
  artifacts?: IntegrationCatalogArtifacts
  connections?: ExternalConnection[]
  events?: ExternalSyncEvent[]
  health?: ExternalSyncHealthResponse
}

export const requiredIntegrationCatalogConnectorIds = [
  'jira',
  'github',
  'intercom',
  'zendesk',
  'salesforce',
  'hubspot',
  'custom-webhook',
  'csv',
]

export const defaultIntegrationCatalogConnectors: IntegrationCatalogConnectorDefinition[] = [
  catalogConnector({
    authTypes: ['oauth2', 'api_token'],
    category: 'Issue tracker',
    dataClasses: ['request', 'status', 'comment reference'],
    description: 'Sync product feedback into Jira issues with project and status context.',
    displayName: 'Jira',
    docsHref: 'https://github.com/Phixsura/attune/tree/main/integrations/integration-catalog',
    healthSignals: ['credential test', 'webhook delivery', 'backfill cursor'],
    id: 'jira',
    scopes: ['read:jira-work', 'write:jira-work', 'read:jira-user'],
    setupChecks: ['credential test', 'project key', 'webhook delivery'],
    upgradePath: 'stable-v1-to-managed-v2',
  }),
  catalogConnector({
    authTypes: ['github_app', 'token'],
    category: 'Issue tracker',
    dataClasses: ['request', 'label', 'issue reference'],
    description: 'Sync customer requests to GitHub Issues with replayable webhook events.',
    displayName: 'GitHub',
    docsHref: 'https://github.com/Phixsura/attune/tree/main/integrations/connector-conformance',
    healthSignals: ['credential test', 'signature verification', 'replay fixture'],
    id: 'github',
    scopes: ['issues:read', 'issues:write', 'metadata:read', 'webhooks:write'],
    setupChecks: ['credential test', 'repository access', 'webhook secret'],
    upgradePath: 'token-to-github-app',
  }),
  catalogConnector({
    authTypes: ['oauth2'],
    category: 'Support',
    dataClasses: ['conversation reference', 'contact', 'support ticket'],
    description: 'Import support conversations and contact context with privacy boundaries.',
    displayName: 'Intercom',
    docsHref: 'https://github.com/Phixsura/attune/tree/main/integrations/integration-catalog',
    healthSignals: ['credential test', 'conversation cursor', 'redaction policy'],
    id: 'intercom',
    replayEvent: 'conversation.user.replied',
    replayNormalizedType: 'feedback',
    scopes: ['conversations:read', 'contacts:read', 'tickets:write'],
    setupChecks: ['credential test', 'workspace access', 'conversation scope'],
    upgradePath: 'conversation-v1-to-ticket-context',
  }),
  catalogConnector({
    authTypes: ['oauth2', 'api_token'],
    category: 'Support',
    dataClasses: ['support ticket', 'status', 'contact'],
    description: 'Map Zendesk tickets and requester context into request evidence.',
    displayName: 'Zendesk',
    docsHref: 'https://github.com/Phixsura/attune/tree/main/integrations/integration-catalog',
    healthSignals: ['credential test', 'ticket cursor', 'webhook delivery'],
    id: 'zendesk',
    replayEvent: 'ticket.updated',
    replayNormalizedType: 'feedback',
    scopes: ['tickets:read', 'tickets:write', 'users:read'],
    setupChecks: ['credential test', 'subdomain', 'ticket webhook'],
    upgradePath: 'ticket-v1-to-audit-events',
  }),
  catalogConnector({
    authTypes: ['oauth2'],
    category: 'CRM',
    dataClasses: ['account', 'contact', 'commercial context'],
    description: 'Bring account and opportunity context into prioritization evidence.',
    displayName: 'Salesforce',
    docsHref: 'https://github.com/Phixsura/attune/tree/main/integrations/integration-catalog',
    healthSignals: ['credential test', 'object schema', 'sync cursor'],
    id: 'salesforce',
    replayEvent: 'account.updated',
    replayNormalizedType: 'account_context',
    scopes: ['api', 'refresh_token', 'offline_access'],
    setupChecks: ['credential test', 'object access', 'field mapping'],
    supportTier: 'Enterprise',
    upgradePath: 'account-context-v1-to-subject-graph',
  }),
  catalogConnector({
    authTypes: ['oauth2', 'private_app_token'],
    category: 'CRM',
    dataClasses: ['company', 'contact', 'support ticket'],
    description: 'Attach contact, company, and ticket context to request evidence.',
    displayName: 'HubSpot',
    docsHref: 'https://github.com/Phixsura/attune/tree/main/integrations/integration-catalog',
    healthSignals: ['credential test', 'association cursor', 'sync cursor'],
    id: 'hubspot',
    replayEvent: 'ticket.propertyChange',
    replayNormalizedType: 'account_context',
    scopes: ['crm.objects.contacts.read', 'crm.objects.companies.read', 'tickets'],
    setupChecks: ['credential test', 'object access', 'association mapping'],
    supportTier: 'Enterprise',
    upgradePath: 'private-app-token-to-oauth',
  }),
  catalogConnector({
    authTypes: ['hmac', 'bearer_token'],
    category: 'Custom',
    dataClasses: ['raw event reference', 'normalized feedback'],
    description: 'Receive signed source events from customer systems with replay evidence.',
    displayName: 'Custom webhook',
    docsHref: 'https://github.com/Phixsura/attune/tree/main/integrations/integration-catalog',
    healthSignals: ['signature verification', 'payload schema', 'dead-letter rate'],
    id: 'custom-webhook',
    replayEvent: 'attune.feedback.created',
    replayNormalizedType: 'feedback',
    scopes: ['webhook:receive', 'webhook:replay'],
    setupChecks: ['endpoint secret', 'signature test', 'sample payload'],
    upgradePath: 'unsigned-to-hmac',
  }),
  catalogConnector({
    authTypes: ['file_upload'],
    category: 'File',
    dataClasses: ['feedback', 'request', 'audit reference'],
    description: 'Import or export feedback and request records with dry-run checks.',
    displayName: 'CSV',
    docsHref: 'https://github.com/Phixsura/attune/tree/main/integrations/import-export-workbench',
    healthSignals: ['template version', 'dry-run result', 'quarantine count'],
    id: 'csv',
    replayEvent: 'csv.row.imported',
    replayNormalizedType: 'feedback',
    scopes: ['feedback:write', 'audit:read'],
    setupChecks: ['schema preview', 'required mapping', 'dry run'],
    upgradePath: 'template-v1-to-workbench-v2',
  }),
]

export const defaultIntegrationCatalogArtifacts: IntegrationCatalogArtifacts = {
  catalogVerifier: true,
  connectors: defaultIntegrationCatalogConnectors,
  expectedConnectorIds: requiredIntegrationCatalogConnectorIds,
}

export function buildIntegrationCatalog(input: IntegrationCatalogInput = {}): IntegrationCatalog {
  const artifacts = input.artifacts ?? defaultIntegrationCatalogArtifacts
  const connectors = decorateCatalogConnectors(artifacts.connectors, input.connections)
  const stats = catalogStats(connectors, artifacts, input)
  const lanes = [
    catalogCardsLane(connectors, artifacts, stats),
    installStatusLane(connectors, input, stats),
    permissionScopeLane(connectors, stats),
    healthBadgeLane(connectors, input, stats),
    sampleReplayLane(connectors, stats),
    upgradePathLane(connectors, stats),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    total: lanes.length,
    verified: lanes.filter((lane) => lane.status === 'verified').length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    connectors,
    fingerprint: `${stats.requiredPresent}/${artifacts.expectedConnectorIds.length} connectors / ${stats.installStates} install states / ${stats.permissionMaps} permission maps / ${stats.replayFixtures} sample replays / ${stats.upgradePaths} upgrade paths / verifier ${artifacts.catalogVerifier ? 'on' : 'off'}`,
    lanes,
    summary: integrationCatalogSummary(totals),
    totals,
  }
}

function catalogConnector(
  connector: Omit<
    IntegrationCatalogConnectorDefinition,
    | 'healthBadge'
    | 'installStatus'
    | 'owner'
    | 'replayAction'
    | 'replayEvent'
    | 'replayFixture'
    | 'replayNormalizedType'
    | 'supportTier'
    | 'upgradeCompatibility'
    | 'upgradeRollback'
    | 'version'
  > &
    Partial<
      Pick<
        IntegrationCatalogConnectorDefinition,
        | 'healthBadge'
        | 'installStatus'
        | 'owner'
        | 'replayAction'
        | 'replayEvent'
        | 'replayFixture'
        | 'replayNormalizedType'
        | 'supportTier'
        | 'upgradeCompatibility'
        | 'upgradeRollback'
        | 'version'
      >
    >,
): IntegrationCatalogConnectorDefinition {
  return {
    healthBadge: 'ready',
    installStatus: 'available',
    owner: 'Developer Platform + Integrations',
    replayAction: 'upsert',
    replayEvent: 'issue_updated',
    replayFixture: `fixtures/${connector.id}-sample-replay.json`,
    replayNormalizedType: 'customer_request',
    supportTier: 'Standard',
    upgradeCompatibility: 'non_breaking',
    upgradeRollback: true,
    version: '2026.07',
    ...connector,
  }
}

type CatalogStats = {
  categories: number
  healthBadges: number
  installStates: number
  liveInstalls: number
  normalizedSamples: number
  permissionMaps: number
  requiredPresent: number
  replayFixtures: number
  rollbackPaths: number
  scopeCount: number
  setupBlockers: number
  unhealthyConnections: number
  upgradePaths: number
}

function catalogStats(
  connectors: IntegrationCatalogConnector[],
  artifacts: IntegrationCatalogArtifacts,
  input: IntegrationCatalogInput,
): CatalogStats {
  const required = new Set(artifacts.expectedConnectorIds)
  return {
    categories: new Set(connectors.map((connector) => connector.category)).size,
    healthBadges: connectors.filter((connector) => connector.healthBadge).length,
    installStates: connectors.filter((connector) => connector.installStatus).length,
    liveInstalls: installedProviderIds(input.connections).size,
    normalizedSamples: connectors.filter((connector) => connector.replayNormalizedType).length,
    permissionMaps: connectors.filter(
      (connector) => connector.scopes.length > 0 && connector.dataClasses.length > 0,
    ).length,
    requiredPresent: connectors.filter((connector) => required.has(connector.id)).length,
    replayFixtures: connectors.filter((connector) => connector.replayFixture).length,
    rollbackPaths: connectors.filter((connector) => connector.upgradeRollback).length,
    scopeCount: connectors.reduce((sum, connector) => sum + connector.scopes.length, 0),
    setupBlockers: setupBlockers(input.connections).length,
    unhealthyConnections: unhealthyConnections(input.connections).length,
    upgradePaths: connectors.filter((connector) => connector.upgradePath).length,
  }
}

function catalogCardsLane(
  connectors: IntegrationCatalogConnector[],
  artifacts: IntegrationCatalogArtifacts,
  stats: CatalogStats,
): IntegrationCatalogLane {
  return {
    actionLabel: 'Review catalog manifest',
    detail:
      'Every listed integration needs a catalog card with category, install metadata, docs, owner, and supported data classes before it can be presented as platform-ready.',
    evidence: `manifest verifier ${available(artifacts.catalogVerifier)} / ${stats.requiredPresent}/${artifacts.expectedConnectorIds.length} required connectors / ${stats.categories} categories`,
    key: 'catalog_cards',
    owner: 'Developer Platform + Integrations',
    signal: `${connectors.length} catalog cards / ${connectors
      .map((connector) => connector.displayName)
      .join(', ')}`,
    status: catalogCardsStatus(connectors, artifacts, stats),
    title: 'Marketplace catalog coverage',
  }
}

function installStatusLane(
  connectors: IntegrationCatalogConnector[],
  input: IntegrationCatalogInput,
  stats: CatalogStats,
): IntegrationCatalogLane {
  return {
    actionLabel: 'Review install states',
    detail:
      'Catalog cards should separate available, installed, beta, and setup-required states while reflecting tenant live installs from External Sync connections.',
    evidence: `${stats.installStates}/${connectors.length} install states / ${
      uniqueAuthTypes(connectors).length
    } auth modes / ${input.connections?.length ?? 0} tenant connections`,
    key: 'install_status',
    owner: 'Developer Platform + Console',
    signal: `${stats.liveInstalls} live installs / ${stats.installStates} catalog states / ${stats.setupBlockers} setup blockers`,
    status: installStatusStatus(connectors, stats),
    title: 'Install status and setup checks',
  }
}

function permissionScopeLane(
  connectors: IntegrationCatalogConnector[],
  stats: CatalogStats,
): IntegrationCatalogLane {
  return {
    actionLabel: 'Audit connector permissions',
    detail:
      'Each connector needs explicit scopes, data classes, and least-privilege framing so security reviewers can approve install boundaries.',
    evidence: `${stats.permissionMaps}/${connectors.length} permission maps / ${stats.scopeCount} scopes / ${connectors.length} data-class inventories`,
    key: 'permission_scope',
    owner: 'Security + Developer Platform',
    signal: `${stats.permissionMaps} permission maps / ${stats.scopeCount} scopes / least privilege ${
      stats.permissionMaps === connectors.length ? 'on' : 'incomplete'
    }`,
    status: permissionScopeStatus(connectors, stats),
    title: 'Permission scope clarity',
  }
}

function healthBadgeLane(
  connectors: IntegrationCatalogConnector[],
  input: IntegrationCatalogInput,
  stats: CatalogStats,
): IntegrationCatalogLane {
  return {
    actionLabel: 'Inspect health badges',
    detail:
      'Catalog health must join static readiness badges with tenant failing, degraded, stale, and quarantined connector signals.',
    evidence: `${stats.healthBadges}/${connectors.length} health badges / ${
      input.health?.degradedConnections ?? 0
    } degraded / ${input.health?.quarantinedConnections ?? 0} quarantined`,
    key: 'health_badge',
    owner: 'Integrations + Reliability',
    signal: `${stats.healthBadges} health badges / ${stats.unhealthyConnections} unhealthy tenant ${connectorNoun(
      stats.unhealthyConnections,
    )}`,
    status: healthBadgeStatus(connectors, stats),
    title: 'Health badge and tenant risk',
  }
}

function sampleReplayLane(
  connectors: IntegrationCatalogConnector[],
  stats: CatalogStats,
): IntegrationCatalogLane {
  return {
    actionLabel: 'Open replay fixtures',
    detail:
      'Every catalog integration needs a deterministic sample replay that proves raw provider input can become a normalized Attune object.',
    evidence: `${stats.replayFixtures}/${connectors.length} fixture paths / ${stats.normalizedSamples}/${connectors.length} normalized samples`,
    key: 'sample_replay',
    owner: 'Developer Platform + QA',
    signal: `${stats.replayFixtures} replay fixtures / ${stats.normalizedSamples} normalized samples`,
    status: sampleReplayStatus(connectors, stats),
    title: 'Sample replay evidence',
  }
}

function upgradePathLane(
  connectors: IntegrationCatalogConnector[],
  stats: CatalogStats,
): IntegrationCatalogLane {
  const migrations = connectors.filter(
    (connector) => connector.upgradeCompatibility === 'requires_migration',
  )
  return {
    actionLabel: 'Review upgrade paths',
    detail:
      'Connector cards need version, compatibility, migration, and rollback evidence before teams can trust catalog upgrades.',
    evidence: `${stats.upgradePaths}/${connectors.length} upgrade paths / ${stats.rollbackPaths}/${connectors.length} rollback plans / ${migrations.length} migrations`,
    key: 'upgrade_path',
    owner: 'Developer Platform + Release',
    signal: `${stats.upgradePaths} upgrade paths / rollback ${stats.rollbackPaths}/${connectors.length}`,
    status: upgradePathStatus(connectors, stats),
    title: 'Upgrade path and rollback',
  }
}

function catalogCardsStatus(
  connectors: IntegrationCatalogConnector[],
  artifacts: IntegrationCatalogArtifacts,
  stats: CatalogStats,
): IntegrationCatalogStatus {
  if (!artifacts.catalogVerifier) return 'blocked'
  if (stats.requiredPresent < artifacts.expectedConnectorIds.length) return 'blocked'
  if (connectors.some((connector) => connector.displayName.length === 0)) return 'blocked'
  return 'verified'
}

function installStatusStatus(
  connectors: IntegrationCatalogConnector[],
  stats: CatalogStats,
): IntegrationCatalogStatus {
  if (stats.installStates < connectors.length) return 'blocked'
  if (connectors.some((connector) => connector.setupChecks.length < 3)) return 'blocked'
  if (stats.setupBlockers > 0) return 'watch'
  return 'verified'
}

function permissionScopeStatus(
  connectors: IntegrationCatalogConnector[],
  stats: CatalogStats,
): IntegrationCatalogStatus {
  if (stats.permissionMaps < connectors.length) return 'blocked'
  /* v8 ignore next -- @preserve: no-scope cards are already caught by the permission-map coverage gate above. */
  if (connectors.some((connector) => connector.scopes.length === 0)) return 'blocked'
  return 'verified'
}

function healthBadgeStatus(
  connectors: IntegrationCatalogConnector[],
  stats: CatalogStats,
): IntegrationCatalogStatus {
  if (stats.healthBadges < connectors.length) return 'blocked'
  if (connectors.some((connector) => connector.healthSignals.length < 3)) return 'blocked'
  if (stats.unhealthyConnections > 0) return 'watch'
  return 'verified'
}

function sampleReplayStatus(
  connectors: IntegrationCatalogConnector[],
  stats: CatalogStats,
): IntegrationCatalogStatus {
  if (stats.replayFixtures < connectors.length) return 'blocked'
  if (stats.normalizedSamples < connectors.length) return 'blocked'
  return 'verified'
}

function upgradePathStatus(
  connectors: IntegrationCatalogConnector[],
  stats: CatalogStats,
): IntegrationCatalogStatus {
  if (stats.upgradePaths < connectors.length) return 'blocked'
  if (stats.rollbackPaths < connectors.length) return 'blocked'
  if (connectors.some((connector) => connector.upgradeCompatibility === 'requires_migration')) {
    return 'watch'
  }
  return 'verified'
}

function decorateCatalogConnectors(
  connectors: IntegrationCatalogConnectorDefinition[],
  connections: ExternalConnection[] = [],
): IntegrationCatalogConnector[] {
  return connectors.map((connector) => {
    const liveConnection = connections.find(
      (connection) => providerKey(connection.provider) === connector.id,
    )
    return {
      ...connector,
      liveConnectionName: liveConnection?.name,
      runtimeHealthBadge: liveConnection
        ? healthBadgeForConnection(liveConnection)
        : connector.healthBadge,
      runtimeInstallStatus: liveConnection?.enabled ? 'installed' : connector.installStatus,
    }
  })
}

function installedProviderIds(connections: ExternalConnection[] = []) {
  const ids = new Set<string>()
  for (const connection of connections) {
    if (connection.enabled && !isUnhealthyConnection(connection))
      ids.add(providerKey(connection.provider))
  }
  return ids
}

function setupBlockers(connections: ExternalConnection[] = []) {
  return connections.filter((connection) =>
    providerKey(connection.status).includes('requires-setup'),
  )
}

function unhealthyConnections(connections: ExternalConnection[] = []) {
  return connections.filter(isUnhealthyConnection)
}

function isUnhealthyConnection(connection: ExternalConnection): boolean {
  const status = providerKey(connection.status)
  const lastTestStatus = providerKey(connection.lastTestStatus)
  return (
    status.includes('degraded') ||
    status.includes('quarantined') ||
    status.includes('failed') ||
    lastTestStatus.includes('failed')
  )
}

function healthBadgeForConnection(connection: ExternalConnection): IntegrationCatalogHealthBadge {
  if (isUnhealthyConnection(connection)) return 'degraded'
  if (connection.enabled && providerKey(connection.lastTestStatus).includes('ok')) return 'healthy'
  return 'watch'
}

function uniqueAuthTypes(connectors: IntegrationCatalogConnector[]) {
  return Array.from(new Set(connectors.flatMap((connector) => connector.authTypes)))
}

function providerKey(value: string) {
  return value.trim().toLowerCase().replaceAll('_', '-').replaceAll(' ', '-')
}

function available(value: boolean) {
  return value ? 'available' : 'missing'
}

function connectorNoun(count: number) {
  return count === 1 ? 'connector' : 'connectors'
}

function integrationCatalogSummary(totals: IntegrationCatalog['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} integration catalog lanes are blocked`
  /* v8 ignore next -- @preserve: integration catalog lanes never emit needs_data; the status union stays shared with other evidence builders. */
  if (totals.needs_data > 0) return `${totals.needs_data} integration catalog lanes need data`
  if (totals.watch > 0) return `${totals.watch} integration catalog lanes need hardening`
  return 'integration catalog is verified'
}
