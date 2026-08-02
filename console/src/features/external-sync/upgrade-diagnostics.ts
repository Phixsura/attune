import type {
  ExternalConnection,
  ExternalObjectMapping,
  ExternalObjectSchema,
  ExternalSyncEvent,
  ExternalSyncHealthResponse,
  ExternalSyncRun,
} from '@/features/external-sync/api/external-sync'
import type { ConnectorConformanceGate } from '@/features/external-sync/connector-conformance-gate'
import type { FieldMappingWorkbench } from '@/features/external-sync/field-mapping-workbench'
import type { IntegrationCatalog } from '@/features/external-sync/integration-catalog'

export type UpgradeDiagnosticsStatus = 'verified' | 'watch' | 'blocked' | 'needs_data'

export type UpgradeDiagnosticsLaneKey =
  | 'install_health'
  | 'permission_boundary'
  | 'schema_drift'
  | 'webhook_readiness'
  | 'fixture_replay'
  | 'version_compatibility'

export type UpgradeDiagnosticsLane = {
  actionLabel: string
  detail: string
  evidence: string
  key: UpgradeDiagnosticsLaneKey
  owner: string
  signal: string
  status: UpgradeDiagnosticsStatus
  title: string
}

export type UpgradeDiagnosticsRow = {
  action: string
  evidence: string
  id: UpgradeDiagnosticsLaneKey
  owner: string
  signal: string
  status: UpgradeDiagnosticsStatus
  title: string
}

export type UpgradeDiagnostics = {
  fingerprint: string
  lanes: UpgradeDiagnosticsLane[]
  rows: UpgradeDiagnosticsRow[]
  summary: string
  totals: Record<UpgradeDiagnosticsStatus, number> & {
    total: number
  }
}

export type UpgradeDiagnosticsArtifacts = {
  compatibilityMatrix: boolean
  diagnosticsVerifier: boolean
  expectedChecks: UpgradeDiagnosticsLaneKey[]
  recoveryPlaybook: boolean
  replayMigrationFixtures: number
  replayMigrationFixturesExpected: number
}

export type UpgradeDiagnosticsInput = {
  artifacts?: UpgradeDiagnosticsArtifacts
  catalog?: IntegrationCatalog
  conformance?: ConnectorConformanceGate
  connections?: ExternalConnection[]
  events?: ExternalSyncEvent[]
  fieldMapping?: FieldMappingWorkbench
  health?: ExternalSyncHealthResponse
  mappings?: ExternalObjectMapping[]
  runs?: ExternalSyncRun[]
  schemas?: ExternalObjectSchema[]
}

export const defaultUpgradeDiagnosticsArtifacts: UpgradeDiagnosticsArtifacts = {
  compatibilityMatrix: true,
  diagnosticsVerifier: true,
  expectedChecks: [
    'install_health',
    'permission_boundary',
    'schema_drift',
    'webhook_readiness',
    'fixture_replay',
    'version_compatibility',
  ],
  recoveryPlaybook: true,
  replayMigrationFixtures: 3,
  replayMigrationFixturesExpected: 3,
}

export function buildUpgradeDiagnostics(input: UpgradeDiagnosticsInput = {}): UpgradeDiagnostics {
  const artifacts = input.artifacts ?? defaultUpgradeDiagnosticsArtifacts
  const lanes = [
    installHealthLane(input, artifacts),
    permissionBoundaryLane(input, artifacts),
    schemaDriftLane(input, artifacts),
    webhookReadinessLane(input, artifacts),
    fixtureReplayLane(input, artifacts),
    versionCompatibilityLane(input, artifacts),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    total: lanes.length,
    verified: lanes.filter((lane) => lane.status === 'verified').length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }
  const rows = lanes.map((lane) => ({
    action: lane.actionLabel,
    evidence: lane.evidence,
    id: lane.key,
    owner: lane.owner,
    signal: lane.signal,
    status: lane.status,
    title: lane.title,
  }))

  return {
    fingerprint: `${artifacts.expectedChecks.length}/${artifacts.expectedChecks.length} checks / verifier ${
      artifacts.diagnosticsVerifier ? 'on' : 'off'
    } / playbook ${available(artifacts.recoveryPlaybook)} / compatibility ${available(
      artifacts.compatibilityMatrix,
    )} / fixtures ${artifacts.replayMigrationFixtures}/${artifacts.replayMigrationFixturesExpected}`,
    lanes,
    rows,
    summary: upgradeDiagnosticsSummary(totals),
    totals,
  }
}

function installHealthLane(
  input: UpgradeDiagnosticsInput,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsLane {
  /* v8 ignore next -- @preserve: empty upgrade diagnostics normalize absent live connections to an empty inventory. */
  const connections = input.connections ?? []
  const unhealthy = unhealthyConnections(connections)
  const retryable = input.health?.retryableRuns ?? 0
  const quarantined =
    input.health?.quarantinedConnections ?? quarantinedConnections(connections).length
  return {
    actionLabel:
      unhealthy.length > 0 || quarantined > 0
        ? 'Resume quarantined connector or rerun credential test'
        : 'Keep connector install diagnostics green',
    detail:
      'Upgrade rollout must pause when live connectors are quarantined, degraded, or failing credential tests.',
    evidence: `${connections.length} tenant connections / ${unhealthy.length} unhealthy / ${quarantined} quarantined / ${retryable} retryable runs`,
    key: 'install_health',
    owner: 'Developer Platform + Integrations',
    signal: `${installedConnections(connections).length} installed / ${unhealthy.length} degraded / ${quarantined} quarantined / ${retryable} retryable`,
    status: installHealthStatus(connections, unhealthy, quarantined, artifacts),
    title: 'Install and connector health',
  }
}

function permissionBoundaryLane(
  input: UpgradeDiagnosticsInput,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsLane {
  /* v8 ignore next -- @preserve: empty upgrade diagnostics normalize absent live connections to an empty inventory. */
  const connections = input.connections ?? []
  const blankScopes = connections.filter((connection) => connection.scopes.length === 0).length
  const permissionLane = input.catalog?.lanes.find((lane) => lane.key === 'permission_scope')
  return {
    actionLabel:
      blankScopes > 0 ? 'Add live connector scopes before upgrade' : 'Review scopes before upgrade',
    detail:
      'Upgrade diagnostics need catalog permission maps plus live connector scope evidence before teams approve new connector capabilities.',
    evidence: `${input.catalog?.connectors.length ?? 0} catalog connectors / ${
      permissionLane?.evidence ?? 'permission lane unavailable'
    }`,
    key: 'permission_boundary',
    owner: 'Security + Developer Platform',
    signal: `${connections.length - blankScopes} scoped connections / ${
      input.catalog?.connectors.length ?? 0
    } permission maps / ${blankScopes} blank scopes`,
    status: permissionBoundaryStatus(input, blankScopes, artifacts),
    title: 'Permission boundary diagnostics',
  }
}

function schemaDriftLane(
  input: UpgradeDiagnosticsInput,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsLane {
  const schemaLane = input.fieldMapping?.lanes.find((lane) => lane.key === 'schema_diff')
  /* v8 ignore next -- @preserve: missing field-mapping workbench evidence is handled by schemaDriftStatus. */
  const driftRisks = schemaLane?.status === 'blocked' ? 1 : 0
  const mappingVersion = input.mappings?.[0]?.mappingVersion ?? 0
  return {
    actionLabel:
      schemaLane?.status === 'verified'
        ? 'Preview mapping before backfill'
        : 'Resolve schema drift before upgrade',
    detail:
      'Provider schema discovery, required-field coverage, and mapping JSON must be checked before migration or backfill.',
    evidence: `${input.schemas?.[0]?.fields.length ?? 0} provider fields / ${
      schemaLane?.evidence ?? 'schema lane unavailable'
    }`,
    key: 'schema_drift',
    owner: 'Integrations + Product Ops',
    signal: `${input.schemas?.[0]?.fields.length ?? 0} provider fields / ${driftRisks} drift risks / mapping v${mappingVersion}`,
    status: schemaDriftStatus(schemaLane?.status, artifacts),
    title: 'Schema drift and mapping safety',
  }
}

function webhookReadinessLane(
  input: UpgradeDiagnosticsInput,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsLane {
  const webhookLane = input.conformance?.lanes.find((lane) => lane.key === 'webhook_signature')
  return {
    actionLabel:
      webhookLane?.status === 'verified'
        ? 'Keep webhook signature fixture pinned'
        : 'Rotate secret and replay webhook fixture',
    detail:
      'Webhook secrets, HMAC verification, and failed-signature visibility must be ready before connector upgrade.',
    evidence: webhookLane?.evidence ?? 'webhook signature evidence unavailable',
    key: 'webhook_readiness',
    owner: 'Security + Integrations',
    signal:
      webhookLane?.signal ?? `${input.events?.length ?? 0} events / signature lane unavailable`,
    status: linkedLaneStatus(webhookLane?.status, artifacts),
    title: 'Webhook readiness diagnostics',
  }
}

function fixtureReplayLane(
  input: UpgradeDiagnosticsInput,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsLane {
  const replayLane = input.conformance?.lanes.find((lane) => lane.key === 'fixture_replay')
  const catalogReplay =
    input.catalog?.connectors.filter((connector) => connector.replayFixture).length ?? 0
  return {
    actionLabel:
      replayLane?.status === 'verified'
        ? 'Keep replay fixtures in upgrade gate'
        : 'Refresh replay fixture before upgrade',
    detail:
      'Fixture replay must prove provider payloads still normalize after connector version changes.',
    evidence: `${catalogReplay} catalog replay fixtures / ${replayLane?.evidence ?? 'replay lane unavailable'}`,
    key: 'fixture_replay',
    owner: 'Developer Platform + QA',
    signal: `${catalogReplay} catalog replays / fixture lane ${replayLane?.status ?? 'missing'} / ${
      input.events?.length ?? 0
    } received events`,
    status: fixtureReplayStatus(replayLane?.status, catalogReplay, artifacts),
    title: 'Fixture replay diagnostics',
  }
}

function versionCompatibilityLane(
  input: UpgradeDiagnosticsInput,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsLane {
  const upgradeLane = input.catalog?.lanes.find((lane) => lane.key === 'upgrade_path')
  const rollbackReady =
    input.catalog?.connectors.filter((connector) => connector.upgradeRollback).length ?? 0
  const connectorCount = input.catalog?.connectors.length ?? 0
  return {
    actionLabel:
      rollbackReady === connectorCount
        ? 'Run compatibility migration plan'
        : 'Add rollback before version upgrade',
    detail:
      'Version compatibility diagnostics need target version, compatibility class, fixture migration, and rollback evidence.',
    evidence: `${rollbackReady}/${connectorCount} rollback paths / ${
      upgradeLane?.evidence ?? 'upgrade lane unavailable'
    }`,
    key: 'version_compatibility',
    owner: 'Developer Platform + Release',
    signal: `${connectorCount} connectors / rollback ${rollbackReady}/${connectorCount} / fixtures ${artifacts.replayMigrationFixtures}/${artifacts.replayMigrationFixturesExpected}`,
    status: versionCompatibilityStatus(
      upgradeLane?.status,
      rollbackReady,
      connectorCount,
      artifacts,
    ),
    title: 'Version compatibility and rollback',
  }
}

function installHealthStatus(
  connections: ExternalConnection[],
  unhealthy: ExternalConnection[],
  quarantined: number,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsStatus {
  if (!artifacts.diagnosticsVerifier || !artifacts.recoveryPlaybook) return 'blocked'
  if (connections.length === 0) return 'needs_data'
  if (unhealthy.length > 0 || quarantined > 0) return 'watch'
  return 'verified'
}

function permissionBoundaryStatus(
  input: UpgradeDiagnosticsInput,
  blankScopes: number,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsStatus {
  if (!artifacts.diagnosticsVerifier) return 'blocked'
  if (!input.catalog) return 'needs_data'
  if (input.catalog.lanes.find((lane) => lane.key === 'permission_scope')?.status === 'blocked') {
    return 'blocked'
  }
  if (blankScopes > 0) return 'watch'
  return 'verified'
}

function schemaDriftStatus(
  status: UpgradeDiagnosticsStatus | undefined,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsStatus {
  if (!artifacts.diagnosticsVerifier) return 'blocked'
  if (!status) return 'needs_data'
  return status
}

function linkedLaneStatus(
  status: UpgradeDiagnosticsStatus | undefined,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsStatus {
  if (!artifacts.diagnosticsVerifier) return 'blocked'
  if (!status) return 'needs_data'
  return status
}

function fixtureReplayStatus(
  status: UpgradeDiagnosticsStatus | undefined,
  catalogReplay: number,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsStatus {
  if (!artifacts.diagnosticsVerifier) return 'blocked'
  if (!status) return 'needs_data'
  if (catalogReplay === 0) return 'blocked'
  if (artifacts.replayMigrationFixtures < artifacts.replayMigrationFixturesExpected)
    return 'blocked'
  return status
}

function versionCompatibilityStatus(
  status: UpgradeDiagnosticsStatus | undefined,
  rollbackReady: number,
  connectorCount: number,
  artifacts: UpgradeDiagnosticsArtifacts,
): UpgradeDiagnosticsStatus {
  if (!artifacts.diagnosticsVerifier || !artifacts.compatibilityMatrix) return 'blocked'
  if (!status || connectorCount === 0) return 'needs_data'
  if (rollbackReady < connectorCount) return 'blocked'
  return status
}

function installedConnections(connections: ExternalConnection[] = []) {
  return connections.filter(
    (connection) => connection.enabled && !isUnhealthyConnection(connection),
  )
}

function unhealthyConnections(connections: ExternalConnection[] = []) {
  return connections.filter(isUnhealthyConnection)
}

function quarantinedConnections(connections: ExternalConnection[] = []) {
  return connections.filter((connection) => normalized(connection.status).includes('quarantined'))
}

function isUnhealthyConnection(connection: ExternalConnection): boolean {
  const status = normalized(connection.status)
  const lastTestStatus = normalized(connection.lastTestStatus)
  return (
    status.includes('degraded') ||
    status.includes('quarantined') ||
    status.includes('failed') ||
    lastTestStatus.includes('failed')
  )
}

function normalized(value: string) {
  return value.trim().toLowerCase().replaceAll('_', '-').replaceAll(' ', '-')
}

function available(value: boolean) {
  /* v8 ignore next -- @preserve: artifact availability permutations are covered by lane status tests. */
  return value ? 'available' : 'missing'
}

function upgradeDiagnosticsSummary(totals: UpgradeDiagnostics['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} upgrade diagnostics lanes are blocked`
  if (totals.needs_data > 0) return `${totals.needs_data} upgrade diagnostics lanes need data`
  if (totals.watch > 0) return `${totals.watch} upgrade diagnostics lanes need hardening`
  return 'upgrade diagnostics are verified'
}
