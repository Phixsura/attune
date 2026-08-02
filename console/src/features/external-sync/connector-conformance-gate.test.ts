import { describe, expect, it } from 'vitest'
import { ExternalSyncDirection } from '@/features/external-sync/api/external-sync'
import {
  buildConnectorConformanceGate,
  defaultConnectorConformanceArtifacts,
} from '@/features/external-sync/connector-conformance-gate'
import {
  ExternalSyncEventSignatureStatus,
  ExternalSyncEventStatus,
  ExternalSyncRunStatus,
  ExternalSyncRunTrigger,
} from '@/proto/attune/v1/external_sync'

describe('buildConnectorConformanceGate', () => {
  it('verifies connector SDK, replay, signature, mapping, and recovery lanes', () => {
    const gate = buildConnectorConformanceGate({
      connections: [
        {
          id: 'conn-1',
          tenantId: 'tenant-1',
          provider: 'github',
          providerInstallationId: 'provider-installation-github',
          name: 'GitHub Issues',
          enabled: true,
          status: 'active',
          authType: 'token',
          baseUrl: '',
          providerConfigJson: '{}',
          scopes: ['issues'],
          lastTestedAt: '2026-07-08T02:00:00Z',
          lastTestStatus: 'ok',
          lastError: '',
          createdBy: 'admin',
          updatedBy: 'admin',
          createdAt: '2026-07-08T01:00:00Z',
          updatedAt: '2026-07-08T02:00:00Z',
          webhookSecretConfigured: true,
        },
      ],
      events: [
        {
          id: 'event-1',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          mappingId: 'mapping-1',
          provider: 'github',
          eventType: 'issues.opened',
          externalEventId: 'delivery-1',
          dedupeKey: 'github:issues:delivery-1',
          signatureStatus:
            ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED,
          status: ExternalSyncEventStatus.EXTERNAL_SYNC_EVENT_STATUS_REPLAYED,
          payloadDigest: 'sha256:abc',
          normalizedPayloadJson: '{}',
          receivedAt: '2026-07-08T02:02:00Z',
          replayedAt: '2026-07-08T02:03:00Z',
          replayedBy: 'admin',
          runId: 'run-1',
          failureReason: '',
          createdAt: '2026-07-08T02:02:00Z',
          updatedAt: '2026-07-08T02:03:00Z',
        },
      ],
      health: {
        activeRuns: 0,
        deadRuns: 0,
        degradedConnections: 0,
        delayedRetryRuns: 1,
        disabledConnections: 0,
        enabledConnections: 1,
        failingConnections: 0,
        newestRetryAfter: '2026-07-08T02:30:00Z',
        newestSuccessfulRunAt: '2026-07-08T02:05:00Z',
        openConflicts: 0,
        providerUnavailableRuns: 1,
        quarantinedConnections: 0,
        retryableRuns: 1,
        staleConnections: 0,
        throttledRuns: 1,
        unauthorizedRuns: 0,
      },
      mappings: [
        {
          id: 'mapping-1',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          localObjectType: 'customer_request',
          externalObjectType: 'issue',
          direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL,
          fieldMappingJson: '{"title":"title","status":"state","labels":"labels"}',
          statusMappingJson: '{"open":"open","done":"closed"}',
          conflictPolicy: 'manual',
          tombstonePolicy: 'mark_stale',
          enabled: true,
          mappingVersion: 2,
          createdAt: '2026-07-08T01:00:00Z',
          updatedAt: '2026-07-08T02:00:00Z',
        },
      ],
      schemas: [
        {
          type: 'issue',
          fields: ['number', 'title', 'state', 'labels', 'updated_at'],
          requiredFields: ['title'],
          writableFields: ['title', 'state', 'labels'],
        },
      ],
    })

    expect(gate.summary).toBe('connector conformance is verified')
    expect(gate.fingerprint).toBe(
      '1/1 providers / 3/3 fixtures / 6/6 hooks / 1 live connectors / 1 verified signatures',
    )
    expect(gate.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      total: 5,
      verified: 5,
      watch: 0,
    })
  })

  it('blocks missing artifact contracts and live schema drift', () => {
    const gate = buildConnectorConformanceGate({
      artifacts: {
        ...defaultConnectorConformanceArtifacts,
        fixtureReplaySuite: false,
        signatureVerifier: false,
      },
      connections: [
        {
          id: 'conn-1',
          tenantId: 'tenant-1',
          provider: 'github',
          providerInstallationId: 'provider-installation-github',
          name: 'GitHub Issues',
          enabled: true,
          status: 'active',
          authType: 'token',
          baseUrl: '',
          providerConfigJson: '{}',
          scopes: ['issues'],
          lastTestedAt: '',
          lastTestStatus: '',
          lastError: '',
          createdBy: 'admin',
          updatedBy: 'admin',
          createdAt: '2026-07-08T01:00:00Z',
          updatedAt: '2026-07-08T02:00:00Z',
          webhookSecretConfigured: true,
        },
      ],
      mappings: [
        {
          id: 'mapping-1',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          localObjectType: 'customer_request',
          externalObjectType: 'issue',
          direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL,
          fieldMappingJson: '{"title":"headline"}',
          statusMappingJson: '{}',
          conflictPolicy: 'manual',
          tombstonePolicy: 'mark_stale',
          enabled: true,
          mappingVersion: 1,
          createdAt: '2026-07-08T01:00:00Z',
          updatedAt: '2026-07-08T02:00:00Z',
        },
      ],
      schemas: [
        {
          type: 'issue',
          fields: ['number', 'title', 'state'],
          requiredFields: ['title'],
          writableFields: ['title'],
        },
      ],
    })

    expect(gate.summary).toBe('3 connector conformance lanes are blocked')
    expect(gate.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['connector_manifest', 'verified'],
      ['fixture_replay', 'blocked'],
      ['webhook_signature', 'blocked'],
      ['field_mapping', 'blocked'],
      ['error_recovery', 'verified'],
    ])
  })

  it('keeps missing live tenant evidence visible after static artifacts pass', () => {
    const gate = buildConnectorConformanceGate({})

    expect(gate.summary).toBe('3 connector conformance lanes need live tenant evidence')
    expect(gate.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['connector_manifest', 'verified'],
      ['fixture_replay', 'verified'],
      ['webhook_signature', 'needs_data'],
      ['field_mapping', 'needs_data'],
      ['error_recovery', 'needs_data'],
    ])
  })

  it('blocks connector lanes when required static artifacts are missing', () => {
    const gate = buildConnectorConformanceGate({
      artifacts: {
        ...defaultConnectorConformanceArtifacts,
        conformanceVerifier: false,
        connectorSdk: false,
        expectedProviders: 2,
        expectedRequiredHooks: 8,
        fieldMappingContract: false,
        fixtures: 1,
        manifest: false,
        providers: 1,
        recoveryMatrix: false,
        requiredHooks: 6,
        signatureVerifier: false,
      },
    })

    expect(gate.summary).toBe('5 connector conformance lanes are blocked')
    expect(gate.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['connector_manifest', 'blocked'],
      ['fixture_replay', 'blocked'],
      ['webhook_signature', 'blocked'],
      ['field_mapping', 'blocked'],
      ['error_recovery', 'blocked'],
    ])
  })

  it('blocks manifests that undershoot provider or hook coverage even when files exist', () => {
    const gate = buildConnectorConformanceGate({
      artifacts: {
        ...defaultConnectorConformanceArtifacts,
        expectedProviders: 2,
        expectedRequiredHooks: 8,
        providers: 1,
        requiredHooks: 7,
      },
      connections: [connection()],
      events: [event()],
      mappings: [mapping()],
      runs: [],
      schemas: [schema()],
    })

    expect(gate.summary).toBe('1 connector conformance lanes are blocked')
    expect(gate.lanes.find((lane) => lane.key === 'connector_manifest')).toMatchObject({
      evidence: 'SDK available / manifest available / 1/2 providers / 7/8 hooks',
      status: 'blocked',
    })
  })

  it('watches thin live evidence before marking connectors verified', () => {
    const gate = buildConnectorConformanceGate({
      connections: [connection({ webhookSecretConfigured: false })],
      events: [],
      health: {
        activeRuns: 0,
        deadRuns: 2,
        degradedConnections: 0,
        delayedRetryRuns: 0,
        disabledConnections: 0,
        enabledConnections: 1,
        failingConnections: 0,
        newestRetryAfter: '',
        newestSuccessfulRunAt: '',
        openConflicts: 0,
        providerUnavailableRuns: 0,
        quarantinedConnections: 0,
        retryableRuns: 0,
        staleConnections: 0,
        throttledRuns: 0,
        unauthorizedRuns: 0,
      },
      mappings: [
        mapping({
          enabled: false,
          fieldMappingJson: '{"title":"title"}',
          statusMappingJson: '{"open":"open","done":"closed"}',
        }),
      ],
      schemas: [schema()],
    })

    expect(gate.summary).toBe('3 connector conformance lanes need hardening')
    expect(gate.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['connector_manifest', 'verified'],
      ['fixture_replay', 'verified'],
      ['webhook_signature', 'watch'],
      ['field_mapping', 'watch'],
      ['error_recovery', 'watch'],
    ])
  })

  it('blocks undersized fixture replay and watches unsigned events plus unretryable dead runs', () => {
    const gate = buildConnectorConformanceGate({
      artifacts: {
        ...defaultConnectorConformanceArtifacts,
        fixtures: 2,
      },
      connections: [connection()],
      events: [event({ signatureStatus: 'pending' })],
      health: {
        activeRuns: 0,
        deadRuns: 1,
        degradedConnections: 0,
        delayedRetryRuns: 0,
        disabledConnections: 0,
        enabledConnections: 1,
        failingConnections: 0,
        newestRetryAfter: '',
        newestSuccessfulRunAt: '',
        openConflicts: 0,
        providerUnavailableRuns: 0,
        quarantinedConnections: 0,
        retryableRuns: 0,
        staleConnections: 0,
        throttledRuns: 0,
        unauthorizedRuns: 0,
      },
      mappings: [mapping()],
      schemas: [schema()],
    })

    expect(gate.summary).toBe('1 connector conformance lanes are blocked')
    expect(gate.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['connector_manifest', 'verified'],
      ['fixture_replay', 'blocked'],
      ['webhook_signature', 'watch'],
      ['field_mapping', 'verified'],
      ['error_recovery', 'watch'],
    ])
  })

  it('blocks failed signatures and invalid mapping payloads', () => {
    const gate = buildConnectorConformanceGate({
      connections: [connection()],
      events: [
        event({
          replayedAt: '',
          runId: '',
          signatureStatus: 'invalid',
        }),
      ],
      mappings: [mapping({ fieldMappingJson: '{"title":', statusMappingJson: '[]' })],
      schemas: [schema()],
    })

    expect(gate.summary).toBe('2 connector conformance lanes are blocked')
    expect(gate.lanes.find((lane) => lane.key === 'webhook_signature')).toMatchObject({
      signal: '0 verified / 1 failed / 1 configured secrets',
      status: 'blocked',
    })
    expect(gate.lanes.find((lane) => lane.key === 'field_mapping')).toMatchObject({
      evidence:
        'mapping contract available / 1 enabled mappings / 1 provider schemas / 1 schema problems',
      status: 'blocked',
    })
    expect(gate.lanes.find((lane) => lane.key === 'fixture_replay')).toMatchObject({
      signal: '1 received events / 0 replayed in tenant ledger',
      status: 'verified',
    })
  })

  it('derives recovery signals from runs and quarantined connections when health is absent', () => {
    const gate = buildConnectorConformanceGate({
      connections: [connection({ status: 'quarantined' })],
      events: [event()],
      mappings: [mapping()],
      runs: [
        run({
          errorKind: 'provider_unavailable',
          nextRetryAt: '2026-07-08T03:00:00Z',
        }),
        run({ errorKind: 'unauthorized' }),
        run({ errorKind: 'provider_throttled' }),
      ],
      schemas: [schema()],
    })

    expect(gate.summary).toBe('connector conformance is verified')
    expect(gate.lanes.find((lane) => lane.key === 'error_recovery')).toMatchObject({
      evidence:
        'recovery matrix available / 1 retryable signals / 1 quarantined connectors / 1 provider outages',
      signal: '1 retryable / 1 quarantined / 1 unauthorized / 1 throttled',
      status: 'verified',
    })
  })

  it('accepts nested and array field mapping candidates when provider schema covers them', () => {
    const gate = buildConnectorConformanceGate({
      connections: [connection()],
      events: [event()],
      mappings: [
        mapping({
          fieldMappingJson:
            '{"title":["title",{"fallback":"updated_at"}],"status":{"provider":"state"},"labels":["labels"]}',
        }),
      ],
      runs: [],
      schemas: [schema()],
    })

    expect(gate.summary).toBe('connector conformance is verified')
    expect(gate.lanes.find((lane) => lane.key === 'field_mapping')).toMatchObject({
      signal: '4 mapped fields / 5 provider fields / 0 problems',
      status: 'verified',
    })
  })

  it('keeps empty, array, and non-string mapping payloads visible as mapping risks', () => {
    const emptyGate = buildConnectorConformanceGate({
      connections: [connection()],
      events: [event()],
      mappings: [mapping({ fieldMappingJson: '' })],
      runs: [],
      schemas: [schema()],
    })
    expect(emptyGate.lanes.find((lane) => lane.key === 'field_mapping')).toMatchObject({
      signal: '0 mapped fields / 5 provider fields / 0 problems',
      status: 'watch',
    })

    const arrayGate = buildConnectorConformanceGate({
      connections: [connection()],
      events: [event()],
      mappings: [mapping({ fieldMappingJson: '[]' })],
      runs: [],
      schemas: [schema()],
    })
    expect(arrayGate.lanes.find((lane) => lane.key === 'field_mapping')).toMatchObject({
      evidence:
        'mapping contract available / 1 enabled mappings / 1 provider schemas / 1 schema problems',
      status: 'blocked',
    })

    const objectGate = buildConnectorConformanceGate({
      connections: [connection()],
      events: [event()],
      mappings: [
        mapping({
          fieldMappingJson: '{"title":null,"status":123,"labels":{"primary":"labels"}}',
        }),
      ],
      runs: [],
      schemas: [schema()],
    })
    expect(objectGate.lanes.find((lane) => lane.key === 'field_mapping')).toMatchObject({
      signal: '1 mapped fields / 5 provider fields / 0 problems',
      status: 'watch',
    })
  })
})

function connection(overrides: Record<string, unknown> = {}) {
  return {
    id: 'conn-1',
    tenantId: 'tenant-1',
    provider: 'github',
    providerInstallationId: 'provider-installation-github',
    name: 'GitHub Issues',
    enabled: true,
    status: 'active',
    authType: 'token',
    baseUrl: '',
    providerConfigJson: '{}',
    scopes: ['issues'],
    lastTestedAt: '2026-07-08T02:00:00Z',
    lastTestStatus: 'ok',
    lastError: '',
    createdBy: 'admin',
    updatedBy: 'admin',
    createdAt: '2026-07-08T01:00:00Z',
    updatedAt: '2026-07-08T02:00:00Z',
    webhookSecretConfigured: true,
    ...overrides,
  }
}

function event(overrides: Record<string, unknown> = {}) {
  return {
    id: 'event-1',
    tenantId: 'tenant-1',
    connectionId: 'conn-1',
    mappingId: 'mapping-1',
    provider: 'github',
    eventType: 'issues.opened',
    externalEventId: 'delivery-1',
    dedupeKey: 'github:issues:delivery-1',
    signatureStatus: ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED,
    status: ExternalSyncEventStatus.EXTERNAL_SYNC_EVENT_STATUS_REPLAYED,
    payloadDigest: 'sha256:abc',
    normalizedPayloadJson: '{}',
    receivedAt: '2026-07-08T02:02:00Z',
    replayedAt: '2026-07-08T02:03:00Z',
    replayedBy: 'admin',
    runId: 'run-1',
    failureReason: '',
    createdAt: '2026-07-08T02:02:00Z',
    updatedAt: '2026-07-08T02:03:00Z',
    ...overrides,
  }
}

function mapping(overrides: Record<string, unknown> = {}) {
  return {
    id: 'mapping-1',
    tenantId: 'tenant-1',
    connectionId: 'conn-1',
    localObjectType: 'customer_request',
    externalObjectType: 'issue',
    direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL,
    fieldMappingJson: '{"title":"title","status":"state","labels":"labels"}',
    statusMappingJson: '{"open":"open","done":"closed"}',
    conflictPolicy: 'manual',
    tombstonePolicy: 'mark_stale',
    enabled: true,
    mappingVersion: 2,
    createdAt: '2026-07-08T01:00:00Z',
    updatedAt: '2026-07-08T02:00:00Z',
    ...overrides,
  }
}

function schema(overrides: Record<string, unknown> = {}) {
  return {
    type: 'issue',
    fields: ['number', 'title', 'state', 'labels', 'updated_at'],
    requiredFields: ['title'],
    writableFields: ['title', 'state', 'labels'],
    ...overrides,
  }
}

function run(overrides: Record<string, unknown> = {}) {
  return {
    id: 'run-1',
    tenantId: 'tenant-1',
    connectionId: 'conn-1',
    mappingId: 'mapping-1',
    direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL,
    trigger: ExternalSyncRunTrigger.EXTERNAL_SYNC_RUN_TRIGGER_MANUAL,
    status: ExternalSyncRunStatus.EXTERNAL_SYNC_RUN_STATUS_FAILED,
    attempts: 1,
    nextRetryAt: '',
    startedAt: '2026-07-08T02:00:00Z',
    finishedAt: '2026-07-08T02:01:00Z',
    cursorBeforeJson: '{}',
    cursorAfterJson: '{}',
    recordsSeen: 1,
    recordsChanged: 0,
    recordsFailed: 1,
    conflictsCreated: 0,
    errorKind: '',
    errorMessage: '',
    actorId: 'admin',
    createdAt: '2026-07-08T02:00:00Z',
    updatedAt: '2026-07-08T02:01:00Z',
    inFlight: false,
    inputMetadataJson: '{}',
    ...overrides,
  }
}
