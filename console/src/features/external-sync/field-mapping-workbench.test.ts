import { describe, expect, it } from 'vitest'
import { ExternalSyncDirection } from '@/features/external-sync/api/external-sync'
import {
  buildFieldMappingWorkbench,
  defaultFieldMappingWorkbenchArtifacts,
} from '@/features/external-sync/field-mapping-workbench'
import { ExternalSyncRunStatus, ExternalSyncRunTrigger } from '@/proto/attune/v1/external_sync'

const connection = {
  id: 'conn-1',
  tenantId: 'tenant-1',
  providerInstallationId: 'provider-installation-github',
  provider: 'github',
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
}

const schema = {
  type: 'issue',
  fields: ['number', 'title', 'state', 'labels', 'updated_at'],
  requiredFields: ['title'],
  writableFields: ['title', 'state', 'labels'],
}

describe('buildFieldMappingWorkbench', () => {
  it('verifies required fields, status mapping, preview safety, and recovery policy', () => {
    const workbench = buildFieldMappingWorkbench({
      connection,
      health: {
        activeRuns: 0,
        deadRuns: 0,
        degradedConnections: 0,
        delayedRetryRuns: 0,
        disabledConnections: 0,
        enabledConnections: 1,
        failingConnections: 0,
        newestRetryAfter: '',
        newestSuccessfulRunAt: '2026-07-08T02:05:00Z',
        openConflicts: 0,
        providerUnavailableRuns: 0,
        quarantinedConnections: 0,
        retryableRuns: 0,
        staleConnections: 0,
        throttledRuns: 0,
        unauthorizedRuns: 0,
      },
      mapping: {
        id: 'mapping-1',
        tenantId: 'tenant-1',
        connectionId: 'conn-1',
        localObjectType: 'customer_request',
        externalObjectType: 'issue',
        direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL,
        fieldMappingJson: '{"title":"title","status":"state","tags":"labels"}',
        statusMappingJson: '{"open":"open","done":"closed"}',
        conflictPolicy: 'manual',
        tombstonePolicy: 'mark_stale',
        enabled: true,
        mappingVersion: 3,
        createdAt: '2026-07-08T01:00:00Z',
        updatedAt: '2026-07-08T02:00:00Z',
      },
      schemas: [schema],
    })

    expect(workbench.summary).toBe('field mapping workbench is verified')
    expect(workbench.fingerprint).toBe(
      'GitHub Issues / mapping-1 / 2/2 required fields / 5 provider fields / 0 drift risks',
    )
    expect(workbench.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      total: 5,
      verified: 5,
      watch: 0,
    })
    expect(
      workbench.mappingRows.map((row) => [row.localField, row.providerField, row.status]),
    ).toEqual([
      ['title', 'title', 'mapped'],
      ['status', 'state', 'mapped'],
      ['description', 'body', 'missing'],
      ['tags', 'labels', 'mapped'],
      ['external_key', 'number', 'suggested'],
    ])
  })

  it('blocks invalid JSON, missing required mappings, status gaps, and schema drift', () => {
    const workbench = buildFieldMappingWorkbench({
      connection,
      mapping: {
        id: 'mapping-2',
        tenantId: 'tenant-1',
        connectionId: 'conn-1',
        localObjectType: 'customer_request',
        externalObjectType: 'issue',
        direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL,
        fieldMappingJson: '{"title":"headline"}',
        statusMappingJson: '{}',
        conflictPolicy: '',
        tombstonePolicy: '',
        enabled: true,
        mappingVersion: 1,
        createdAt: '2026-07-08T01:00:00Z',
        updatedAt: '2026-07-08T02:00:00Z',
      },
      schemas: [schema],
    })

    expect(workbench.summary).toBe('4 field mapping lanes are blocked')
    expect(workbench.fingerprint).toBe(
      'GitHub Issues / mapping-2 / 0/2 required fields / 5 provider fields / 1 drift risks',
    )
    expect(workbench.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['schema_diff', 'verified'],
      ['required_mapping', 'blocked'],
      ['status_mapping', 'blocked'],
      ['preview_backfill', 'blocked'],
      ['rollback_recovery', 'blocked'],
    ])
    expect(workbench.mappingRows.find((row) => row.localField === 'title')).toMatchObject({
      providerField: 'headline',
      status: 'drift',
    })
  })

  it('keeps missing live mapping evidence visible without blocking schema discovery', () => {
    const workbench = buildFieldMappingWorkbench({
      schemas: [schema],
    })

    expect(workbench.summary).toBe('4 field mapping lanes need live mapping evidence')
    expect(workbench.fingerprint).toBe(
      'No connection / no mapping / 0/2 required fields / 5 provider fields / 0 drift risks',
    )
    expect(workbench.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['schema_diff', 'verified'],
      ['required_mapping', 'needs_data'],
      ['status_mapping', 'needs_data'],
      ['preview_backfill', 'needs_data'],
      ['rollback_recovery', 'needs_data'],
    ])
  })

  it('blocks lanes when required workbench safety controls are absent', () => {
    const workbench = buildFieldMappingWorkbench({
      artifacts: {
        ...defaultFieldMappingWorkbenchArtifacts,
        conflictPolicyControl: false,
        resetCursorRecovery: false,
        samplePreview: false,
        schemaDiffDetector: false,
        tombstonePolicyControl: false,
      },
      connection,
      mapping: objectMapping(),
      schemas: [schema],
    })

    expect(workbench.summary).toBe('3 field mapping lanes are blocked')
    expect(workbench.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['schema_diff', 'blocked'],
      ['required_mapping', 'verified'],
      ['status_mapping', 'verified'],
      ['preview_backfill', 'blocked'],
      ['rollback_recovery', 'blocked'],
    ])
  })

  it('watches disabled push mappings and rollback pressure after failures', () => {
    const workbench = buildFieldMappingWorkbench({
      connection,
      health: { ...health(), openConflicts: 2 },
      mapping: objectMapping({
        direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PUSH,
        enabled: false,
      }),
      runs: [
        {
          id: 'run-1',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          mappingId: 'mapping-1',
          direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PUSH,
          trigger: ExternalSyncRunTrigger.EXTERNAL_SYNC_RUN_TRIGGER_MANUAL,
          status: ExternalSyncRunStatus.EXTERNAL_SYNC_RUN_STATUS_FAILED,
          attempts: 1,
          nextRetryAt: '',
          startedAt: '2026-07-08T02:00:00Z',
          finishedAt: '2026-07-08T02:01:00Z',
          cursorBeforeJson: '{}',
          cursorAfterJson: '{}',
          recordsSeen: 3,
          recordsChanged: 1,
          recordsFailed: 1,
          conflictsCreated: 5,
          errorKind: 'validation',
          errorMessage: 'bad mapping',
          actorId: 'admin',
          createdAt: '2026-07-08T02:00:00Z',
          updatedAt: '2026-07-08T02:01:00Z',
          inFlight: false,
          inputMetadataJson: '{}',
        },
      ],
      schemas: [schema],
    })

    expect(workbench.summary).toBe('2 field mapping lanes need hardening')
    expect(workbench.lanes.find((lane) => lane.key === 'preview_backfill')).toMatchObject({
      evidence: 'preview available / impact available / reset available / backfill not available',
      status: 'watch',
    })
    expect(workbench.lanes.find((lane) => lane.key === 'rollback_recovery')).toMatchObject({
      signal: 'mapping v3 / 1 failed / 2 conflicts',
      status: 'watch',
    })
  })

  it('falls back to run-created conflicts when health conflict totals are unavailable', () => {
    const workbench = buildFieldMappingWorkbench({
      connection,
      mapping: objectMapping(),
      runs: [
        {
          id: 'run-1',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          mappingId: 'mapping-1',
          direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL,
          trigger: ExternalSyncRunTrigger.EXTERNAL_SYNC_RUN_TRIGGER_MANUAL,
          status: ExternalSyncRunStatus.EXTERNAL_SYNC_RUN_STATUS_SUCCEEDED,
          attempts: 1,
          nextRetryAt: '',
          startedAt: '2026-07-08T02:00:00Z',
          finishedAt: '2026-07-08T02:01:00Z',
          cursorBeforeJson: '{}',
          cursorAfterJson: '{}',
          recordsSeen: 3,
          recordsChanged: 1,
          recordsFailed: 0,
          conflictsCreated: 3,
          errorKind: '',
          errorMessage: '',
          actorId: 'admin',
          createdAt: '2026-07-08T02:00:00Z',
          updatedAt: '2026-07-08T02:01:00Z',
          inFlight: false,
          inputMetadataJson: '{}',
        },
      ],
      schemas: [schema],
    })

    expect(workbench.summary).toBe('1 field mapping lanes need hardening')
    expect(workbench.lanes.find((lane) => lane.key === 'rollback_recovery')).toMatchObject({
      signal: 'mapping v3 / 0 failed / 3 conflicts',
      status: 'watch',
    })
  })

  it('blocks array and malformed JSON records before preview or status operations', () => {
    const arrayWorkbench = buildFieldMappingWorkbench({
      connection,
      mapping: objectMapping({ statusMappingJson: '[]' }),
      schemas: [schema],
    })
    expect(arrayWorkbench.lanes.find((lane) => lane.key === 'status_mapping')).toMatchObject({
      evidence: '0/2 required statuses / JSON invalid / conflict manual',
      status: 'blocked',
    })

    const malformedWorkbench = buildFieldMappingWorkbench({
      connection,
      mapping: objectMapping({ fieldMappingJson: '{"title":' }),
      schemas: [schema],
    })
    expect(malformedWorkbench.summary).toBe('2 field mapping lanes are blocked')
    expect(malformedWorkbench.lanes.find((lane) => lane.key === 'required_mapping')).toMatchObject({
      evidence: '0/2 required mapped / 2 suggested / 0 drifted / JSON invalid',
      status: 'blocked',
    })
    expect(malformedWorkbench.lanes.find((lane) => lane.key === 'preview_backfill')).toMatchObject({
      status: 'blocked',
    })
  })

  it('surfaces suggestions when provider schema evidence is not loaded yet', () => {
    const workbench = buildFieldMappingWorkbench({
      connection,
      mapping: objectMapping({ fieldMappingJson: '' }),
    })

    expect(workbench.summary).toBe('1 field mapping lanes are blocked')
    expect(workbench.fingerprint).toBe(
      'GitHub Issues / mapping-1 / 0/2 required fields / 0 provider fields / 0 drift risks',
    )
    expect(workbench.mappingRows.find((row) => row.localField === 'title')).toMatchObject({
      evidence: 'suggestion is available from schema',
      providerField: 'title',
      status: 'suggested',
    })
    expect(workbench.lanes.find((lane) => lane.key === 'schema_diff')).toMatchObject({
      status: 'needs_data',
    })
  })

  it('marks optional local fields as unmapped when no suggestion exists', () => {
    const workbench = buildFieldMappingWorkbench({
      artifacts: {
        ...defaultFieldMappingWorkbenchArtifacts,
        recommendedLocalFields: ['notes'],
        requiredLocalFields: ['title'],
        requiredProviderFields: [],
        requiredStatusValues: [],
        suggestions: {},
      },
      connection,
      mapping: objectMapping({ fieldMappingJson: '{}', statusMappingJson: '{}' }),
      schemas: [
        {
          ...schema,
          fields: [],
          requiredFields: [],
          writableFields: [],
        },
      ],
    })

    expect(workbench.mappingRows.find((row) => row.localField === 'notes')).toMatchObject({
      evidence: 'optional field is not mapped',
      providerField: 'unmapped',
      status: 'missing',
      suggestion: 'none',
    })
  })

  it('blocks schema diff when required provider fields disappear', () => {
    const workbench = buildFieldMappingWorkbench({
      connection,
      mapping: objectMapping(),
      schemas: [
        {
          ...schema,
          fields: ['title', 'labels'],
        },
      ],
    })

    expect(workbench.summary).toBe('3 field mapping lanes are blocked')
    expect(workbench.lanes.find((lane) => lane.key === 'schema_diff')).toMatchObject({
      evidence: 'schema diff available / 2 fields / 3 writable / 1 required missing',
      status: 'blocked',
    })
  })

  it('blocks optional drift even when required rows remain mapped', () => {
    const workbench = buildFieldMappingWorkbench({
      connection,
      mapping: objectMapping({
        fieldMappingJson: '{"title":"title","status":"state","tags":"missing_labels"}',
      }),
      schemas: [schema],
    })

    expect(workbench.summary).toBe('1 field mapping lanes are blocked')
    expect(workbench.lanes.find((lane) => lane.key === 'required_mapping')).toMatchObject({
      evidence: '2/2 required mapped / 0 suggested / 1 drifted / JSON valid',
      status: 'blocked',
    })
    expect(workbench.mappingRows.find((row) => row.localField === 'tags')).toMatchObject({
      evidence: 'saved mapping references a missing provider field',
      providerField: 'missing_labels',
      status: 'drift',
    })
  })

  it('keeps schema fallback empty when neither mapping nor schema evidence exists', () => {
    const workbench = buildFieldMappingWorkbench({})

    expect(workbench.fingerprint).toBe(
      'No connection / no mapping / 0/2 required fields / 0 provider fields / 0 drift risks',
    )
    expect(workbench.lanes.find((lane) => lane.key === 'schema_diff')).toMatchObject({
      status: 'needs_data',
    })
  })
})

function objectMapping(overrides: Record<string, unknown> = {}) {
  return {
    id: 'mapping-1',
    tenantId: 'tenant-1',
    connectionId: 'conn-1',
    localObjectType: 'customer_request',
    externalObjectType: 'issue',
    direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL,
    fieldMappingJson: '{"title":"title","status":"state","tags":"labels"}',
    statusMappingJson: '{"open":"open","done":"closed"}',
    conflictPolicy: 'manual',
    tombstonePolicy: 'mark_stale',
    enabled: true,
    mappingVersion: 3,
    createdAt: '2026-07-08T01:00:00Z',
    updatedAt: '2026-07-08T02:00:00Z',
    ...overrides,
  }
}

function health(overrides: Record<string, unknown> = {}) {
  return {
    activeRuns: 0,
    deadRuns: 0,
    degradedConnections: 0,
    delayedRetryRuns: 0,
    disabledConnections: 0,
    enabledConnections: 1,
    failingConnections: 0,
    newestRetryAfter: '',
    newestSuccessfulRunAt: '2026-07-08T02:05:00Z',
    openConflicts: 0,
    providerUnavailableRuns: 0,
    quarantinedConnections: 0,
    retryableRuns: 0,
    staleConnections: 0,
    throttledRuns: 0,
    unauthorizedRuns: 0,
    ...overrides,
  }
}
