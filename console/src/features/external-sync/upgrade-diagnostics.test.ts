import { describe, expect, it } from 'vitest'
import type {
  ExternalConnection,
  ExternalObjectMapping,
  ExternalObjectSchema,
  ExternalSyncEvent,
  ExternalSyncHealthResponse,
  ExternalSyncRun,
} from '@/features/external-sync/api/external-sync'
import { buildConnectorConformanceGate } from '@/features/external-sync/connector-conformance-gate'
import { buildFieldMappingWorkbench } from '@/features/external-sync/field-mapping-workbench'
import { buildIntegrationCatalog } from '@/features/external-sync/integration-catalog'
import {
  buildUpgradeDiagnostics,
  defaultUpgradeDiagnosticsArtifacts,
  type UpgradeDiagnosticsArtifacts,
} from '@/features/external-sync/upgrade-diagnostics'
import {
  ExternalSyncDirection,
  ExternalSyncEventSignatureStatus,
  ExternalSyncEventStatus,
  ExternalSyncRunStatus,
  ExternalSyncRunTrigger,
} from '@/proto/attune/v1/external_sync'

describe('buildUpgradeDiagnostics', () => {
  it('joins catalog, conformance, field mapping, and health evidence into upgrade diagnostics', () => {
    const diagnostics = buildUpgradeDiagnostics(upgradeInput())

    expect(diagnostics.fingerprint).toBe(
      '6/6 checks / verifier on / playbook available / compatibility available / fixtures 3/3',
    )
    expect(diagnostics.summary).toBe('1 upgrade diagnostics lanes need hardening')
    expect(diagnostics.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      total: 6,
      verified: 5,
      watch: 1,
    })
    expect(diagnostics.lanes.map((lane) => [lane.key, lane.status, lane.signal])).toEqual([
      ['install_health', 'watch', '1 installed / 1 degraded / 1 quarantined / 1 retryable'],
      [
        'permission_boundary',
        'verified',
        '2 scoped connections / 8 permission maps / 0 blank scopes',
      ],
      ['schema_drift', 'verified', '5 provider fields / 0 drift risks / mapping v3'],
      ['webhook_readiness', 'verified', '1 verified / 0 failed / 1 configured secrets'],
      [
        'fixture_replay',
        'verified',
        '8 catalog replays / fixture lane verified / 1 received events',
      ],
      ['version_compatibility', 'verified', '8 connectors / rollback 8/8 / fixtures 3/3'],
    ])
    expect(diagnostics.rows.find((row) => row.id === 'install_health')).toMatchObject({
      action: 'Resume quarantined connector or rerun credential test',
      status: 'watch',
    })
  })

  it('verifies upgrade diagnostics when live connector evidence is healthy', () => {
    const diagnostics = buildUpgradeDiagnostics(
      upgradeInput({
        connections: [connections()[0]],
        health: healthyHealth(),
      }),
    )

    expect(diagnostics.summary).toBe('upgrade diagnostics are verified')
    expect(diagnostics.totals).toMatchObject({
      blocked: 0,
      needs_data: 0,
      verified: 6,
      watch: 0,
    })
  })

  it('blocks all lanes when the diagnostics verifier is missing', () => {
    const artifacts: UpgradeDiagnosticsArtifacts = {
      ...defaultUpgradeDiagnosticsArtifacts,
      diagnosticsVerifier: false,
    }

    const diagnostics = buildUpgradeDiagnostics(upgradeInput({ artifacts }))

    expect(diagnostics.summary).toBe('6 upgrade diagnostics lanes are blocked')
    expect(diagnostics.totals.blocked).toBe(6)
  })

  it('requires live data before install health can be trusted', () => {
    const diagnostics = buildUpgradeDiagnostics(upgradeInput({ connections: [] }))

    expect(diagnostics.summary).toBe('2 upgrade diagnostics lanes need data')
    expect(diagnostics.lanes.find((lane) => lane.key === 'install_health')).toMatchObject({
      status: 'needs_data',
    })
  })

  it('blocks fixture replay when migration fixtures are incomplete', () => {
    const artifacts: UpgradeDiagnosticsArtifacts = {
      ...defaultUpgradeDiagnosticsArtifacts,
      replayMigrationFixtures: 2,
    }

    const diagnostics = buildUpgradeDiagnostics(upgradeInput({ artifacts }))

    expect(diagnostics.summary).toBe('1 upgrade diagnostics lanes are blocked')
    expect(diagnostics.lanes.find((lane) => lane.key === 'fixture_replay')).toMatchObject({
      signal: '8 catalog replays / fixture lane verified / 1 received events',
      status: 'blocked',
    })
  })

  it('blocks version compatibility when rollback evidence is missing', () => {
    const catalog = buildIntegrationCatalog({
      connections: connections(),
      artifacts: {
        catalogVerifier: true,
        connectors: buildIntegrationCatalog().connectors.map((connector) =>
          connector.id === 'github' ? { ...connector, upgradeRollback: false } : connector,
        ),
        expectedConnectorIds: buildIntegrationCatalog().connectors.map((connector) => connector.id),
      },
    })

    const diagnostics = buildUpgradeDiagnostics(upgradeInput({ catalog }))

    expect(diagnostics.summary).toBe('1 upgrade diagnostics lanes are blocked')
    expect(diagnostics.lanes.find((lane) => lane.key === 'version_compatibility')).toMatchObject({
      signal: '8 connectors / rollback 7/8 / fixtures 3/3',
      status: 'blocked',
    })
  })

  it('blocks permission diagnostics when the catalog permission lane is blocked', () => {
    const catalog = buildIntegrationCatalog({
      connections: connections(),
      artifacts: {
        catalogVerifier: true,
        connectors: buildIntegrationCatalog().connectors.map((connector) =>
          connector.id === 'github' ? { ...connector, scopes: [] } : connector,
        ),
        expectedConnectorIds: buildIntegrationCatalog().connectors.map((connector) => connector.id),
      },
    })

    const diagnostics = buildUpgradeDiagnostics(upgradeInput({ catalog }))

    expect(diagnostics.summary).toBe('1 upgrade diagnostics lanes are blocked')
    expect(diagnostics.lanes.find((lane) => lane.key === 'permission_boundary')).toMatchObject({
      signal: '2 scoped connections / 8 permission maps / 0 blank scopes',
      status: 'blocked',
    })
  })

  it('watches permission diagnostics when live connector scopes are blank', () => {
    const diagnostics = buildUpgradeDiagnostics(
      upgradeInput({
        connections: [
          {
            ...connections()[0],
            scopes: [],
          },
        ],
        health: healthyHealth(),
      }),
    )

    expect(diagnostics.summary).toBe('1 upgrade diagnostics lanes need hardening')
    expect(diagnostics.lanes.find((lane) => lane.key === 'permission_boundary')).toMatchObject({
      signal: '0 scoped connections / 8 permission maps / 1 blank scopes',
      status: 'watch',
    })
  })

  it('falls back to live connection quarantine state when health summary is absent', () => {
    const input = upgradeInput()
    const diagnostics = buildUpgradeDiagnostics({
      ...input,
      health: undefined,
    })

    expect(diagnostics.summary).toBe('1 upgrade diagnostics lanes need hardening')
    expect(diagnostics.lanes.find((lane) => lane.key === 'install_health')).toMatchObject({
      signal: '1 installed / 1 degraded / 1 quarantined / 0 retryable',
      status: 'watch',
    })
  })

  it('requires linked catalog, conformance, and field mapping lanes before upgrade trust', () => {
    const diagnostics = buildUpgradeDiagnostics({
      connections: [connections()[0]],
      health: healthyHealth(),
    })

    expect(diagnostics.summary).toBe('5 upgrade diagnostics lanes need data')
    expect(diagnostics.lanes.find((lane) => lane.key === 'install_health')).toMatchObject({
      status: 'verified',
    })
    expect(diagnostics.lanes.filter((lane) => lane.status === 'needs_data')).toHaveLength(5)
  })

  it('blocks fixture replay when the catalog has no replay fixtures', () => {
    const catalog = buildIntegrationCatalog({
      connections: connections(),
      artifacts: {
        catalogVerifier: true,
        connectors: buildIntegrationCatalog().connectors.map((connector) => ({
          ...connector,
          replayFixture: '',
        })),
        expectedConnectorIds: buildIntegrationCatalog().connectors.map((connector) => connector.id),
      },
    })

    const diagnostics = buildUpgradeDiagnostics(upgradeInput({ catalog }))

    expect(diagnostics.summary).toBe('1 upgrade diagnostics lanes are blocked')
    expect(diagnostics.lanes.find((lane) => lane.key === 'fixture_replay')).toMatchObject({
      signal: '0 catalog replays / fixture lane verified / 1 received events',
      status: 'blocked',
    })
  })
})

function upgradeInput(
  patch: Partial<{
    artifacts: UpgradeDiagnosticsArtifacts
    catalog: ReturnType<typeof buildIntegrationCatalog>
    connections: ExternalConnection[]
    health: ExternalSyncHealthResponse
  }> = {},
) {
  const conn = patch.connections ?? connections()
  const health = patch.health ?? externalHealth()
  const mappings = [mapping()]
  const schemas = [schema()]
  const runs = [run()]
  const events = [event()]
  const catalog = patch.catalog ?? buildIntegrationCatalog({ connections: conn, health })
  const conformance = buildConnectorConformanceGate({
    connections: conn,
    events,
    health,
    mappings,
    runs,
    schemas,
  })
  const fieldMapping = buildFieldMappingWorkbench({
    connection: conn[0],
    health,
    mapping: mappings[0],
    mappings,
    runs: [],
    schemas,
  })
  return {
    artifacts: patch.artifacts,
    catalog,
    conformance,
    connections: conn,
    events,
    fieldMapping,
    health,
    mappings,
    runs,
    schemas,
  }
}

function connections(): ExternalConnection[] {
  return [
    {
      authType: 'token',
      baseUrl: '',
      createdAt: '2026-07-08T01:00:00Z',
      createdBy: 'admin',
      enabled: true,
      id: 'conn-github',
      lastError: '',
      lastTestStatus: 'ok',
      lastTestedAt: '2026-07-08T02:00:00Z',
      name: 'GitHub Prod',
      provider: 'github',
      providerInstallationId: 'provider-installation-github',
      providerConfigJson: '{}',
      scopes: ['issues'],
      status: 'active',
      tenantId: 'tenant-1',
      updatedAt: '2026-07-08T02:00:00Z',
      updatedBy: 'admin',
      webhookSecretConfigured: true,
    },
    {
      authType: 'token',
      baseUrl: '',
      createdAt: '2026-07-08T01:30:00Z',
      createdBy: 'admin',
      enabled: false,
      id: 'conn-github-quarantined',
      lastError: 'provider throttled',
      lastTestStatus: 'failed',
      lastTestedAt: '2026-07-08T03:00:00Z',
      name: 'GitHub Quarantined',
      provider: 'github',
      providerInstallationId: 'provider-installation-github-quarantined',
      providerConfigJson: '{}',
      scopes: ['issues'],
      status: 'quarantined',
      tenantId: 'tenant-1',
      updatedAt: '2026-07-08T03:00:00Z',
      updatedBy: 'admin',
      webhookSecretConfigured: false,
    },
  ]
}

function externalHealth(): ExternalSyncHealthResponse {
  return {
    activeRuns: 0,
    deadRuns: 0,
    degradedConnections: 1,
    delayedRetryRuns: 1,
    disabledConnections: 1,
    enabledConnections: 1,
    failingConnections: 0,
    newestRetryAfter: '2026-07-08T03:10:00Z',
    newestSuccessfulRunAt: '2026-07-08T02:45:00Z',
    openConflicts: 0,
    providerUnavailableRuns: 0,
    quarantinedConnections: 1,
    retryableRuns: 1,
    staleConnections: 0,
    throttledRuns: 1,
    unauthorizedRuns: 0,
  }
}

function healthyHealth(): ExternalSyncHealthResponse {
  return {
    ...externalHealth(),
    degradedConnections: 0,
    delayedRetryRuns: 0,
    disabledConnections: 0,
    newestRetryAfter: '',
    openConflicts: 0,
    quarantinedConnections: 0,
    retryableRuns: 0,
    throttledRuns: 0,
  }
}

function mapping(): ExternalObjectMapping {
  return {
    connectionId: 'conn-github',
    conflictPolicy: 'manual',
    createdAt: '2026-07-08T01:05:00Z',
    direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL,
    enabled: true,
    externalObjectType: 'issue',
    fieldMappingJson: '{"title":"title","status":"state","tags":"labels"}',
    id: 'mapping-github',
    localObjectType: 'customer_request',
    mappingVersion: 3,
    statusMappingJson: '{"open":"open","done":"closed"}',
    tenantId: 'tenant-1',
    tombstonePolicy: 'mark_stale',
    updatedAt: '2026-07-08T02:05:00Z',
  }
}

function schema(): ExternalObjectSchema {
  return {
    fields: ['number', 'title', 'state', 'labels', 'updated_at'],
    requiredFields: ['title'],
    type: 'issue',
    writableFields: ['title', 'state', 'labels'],
  }
}

function run(): ExternalSyncRun {
  return {
    actorId: 'admin',
    attempts: 1,
    conflictsCreated: 0,
    connectionId: 'conn-github',
    createdAt: '2026-07-08T02:00:00Z',
    cursorAfterJson: '{}',
    cursorBeforeJson: '{}',
    direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL,
    errorKind: '',
    errorMessage: '',
    finishedAt: '2026-07-08T02:01:00Z',
    id: 'run-github',
    inFlight: false,
    inputMetadataJson: '{}',
    mappingId: 'mapping-github',
    nextRetryAt: '',
    recordsChanged: 1,
    recordsFailed: 0,
    recordsSeen: 1,
    startedAt: '2026-07-08T02:00:00Z',
    status: ExternalSyncRunStatus.EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED,
    tenantId: 'tenant-1',
    trigger: ExternalSyncRunTrigger.EXTERNAL_SYNC_RUN_TRIGGER_MANUAL,
    updatedAt: '2026-07-08T02:01:00Z',
  }
}

function event(): ExternalSyncEvent {
  return {
    connectionId: 'conn-github',
    createdAt: '2026-07-08T03:02:00Z',
    dedupeKey: 'github:event-1',
    eventType: 'issues.edited',
    externalEventId: 'event-1',
    failureReason: '',
    id: 'event-github',
    mappingId: 'mapping-github',
    normalizedPayloadJson: '{"action":"edited"}',
    payloadDigest: 'sha256:event',
    provider: 'github',
    receivedAt: '2026-07-08T03:02:00Z',
    replayedAt: '',
    replayedBy: '',
    runId: '',
    signatureStatus: ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED,
    status: ExternalSyncEventStatus.EXTERNAL_SYNC_EVENT_STATUS_RECEIVED,
    tenantId: 'tenant-1',
    updatedAt: '2026-07-08T03:02:00Z',
  }
}
