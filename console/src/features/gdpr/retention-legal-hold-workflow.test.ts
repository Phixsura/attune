import { describe, expect, it } from 'vitest'
import {
  type GdprOperationsResponse,
  GdprRequestStatus,
  type GdprRequestSummary,
  GdprRequestType,
} from '@/proto/attune/v1/gdpr'
import { buildRetentionLegalHoldWorkflow } from './retention-legal-hold-workflow'

describe('buildRetentionLegalHoldWorkflow', () => {
  it('joins retention policy, legal hold, delete grace, export residue, and backup residue', () => {
    const workflow = buildRetentionLegalHoldWorkflow({
      operations: operations(),
      requests: [
        request(
          'export-ready',
          GdprRequestType.GDPR_REQUEST_TYPE_EXPORT,
          GdprRequestStatus.GDPR_REQUEST_STATUS_READY,
          {
            expiresAt: '2026-06-25T09:00:00Z',
          },
        ),
        request(
          'delete-scheduled',
          GdprRequestType.GDPR_REQUEST_TYPE_DELETE,
          GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED,
          { executeAfter: '2026-06-24T10:30:00Z' },
        ),
      ],
    })

    expect(workflow.fingerprint).toBe(
      '30d audit / 1h delete grace / legal hold off / 2 request records',
    )
    expect(workflow.summary).toBe('1 retention and legal-hold checks are blocked')
    expect(workflow.totals).toMatchObject({
      blocked: 1,
      needs_data: 0,
      ready: 1,
      total: 5,
      watch: 3,
    })
    expect(workflow.lanes.find((lane) => lane.key === 'retention_policy')).toMatchObject({
      signal: 'audit 30d / export 1d / prune 1h',
      status: 'ready',
    })
    expect(workflow.lanes.find((lane) => lane.key === 'legal_hold_gate')).toMatchObject({
      signal: 'legal hold off / 1 scheduled deletes',
      status: 'blocked',
    })
    expect(workflow.lanes.find((lane) => lane.key === 'delete_grace_window')).toMatchObject({
      signal: 'grace 1h / 1 scheduled deletes / 1 visible',
      status: 'watch',
    })
    expect(workflow.lanes.find((lane) => lane.key === 'export_residue')).toMatchObject({
      signal: '1 ready exports / expires 2026-06-25T09:00:00Z / TTL 1d',
      status: 'watch',
    })
    expect(workflow.lanes.find((lane) => lane.key === 'backup_retention')).toMatchObject({
      signal: 'hashed audit on / backup residue on / audit 30d',
      status: 'watch',
    })
  })

  it('blocks missing retention windows and unbounded export residue', () => {
    const workflow = buildRetentionLegalHoldWorkflow({
      operations: operations({
        auditPruneIntervalSeconds: 0,
        auditRetentionDays: 0,
        deleteGraceWindowSeconds: 0,
        exportTtlSeconds: 0,
        hashedAuditResidue: false,
        legalHoldSupported: true,
        nextExportExpiryAt: undefined,
        readyExportCount: 1,
        scheduledDeleteCount: 1,
      }),
    })

    expect(workflow.summary).toBe('4 retention and legal-hold checks are blocked')
    expect(workflow.totals).toMatchObject({ blocked: 4, ready: 1, watch: 0 })
    expect(workflow.lanes.find((lane) => lane.key === 'retention_policy')).toMatchObject({
      status: 'blocked',
    })
    expect(workflow.lanes.find((lane) => lane.key === 'legal_hold_gate')).toMatchObject({
      status: 'ready',
    })
    expect(workflow.lanes.find((lane) => lane.key === 'delete_grace_window')).toMatchObject({
      status: 'blocked',
    })
    expect(workflow.lanes.find((lane) => lane.key === 'export_residue')).toMatchObject({
      status: 'blocked',
    })
    expect(workflow.lanes.find((lane) => lane.key === 'backup_retention')).toMatchObject({
      status: 'blocked',
    })
  })

  it('keeps missing operations evidence explicit', () => {
    const workflow = buildRetentionLegalHoldWorkflow({})

    expect(workflow.fingerprint).toBe('retention operations evidence missing')
    expect(workflow.summary).toBe('5 retention and legal-hold checks need evidence')
    expect(workflow.totals).toMatchObject({
      blocked: 0,
      needs_data: 5,
      ready: 0,
      total: 5,
      watch: 0,
    })
  })

  it('marks retention and legal-hold evidence verified when residue is cleared', () => {
    const workflow = buildRetentionLegalHoldWorkflow({
      operations: operations({
        backupsMayRetainUntilRotation: false,
        legalHoldSupported: true,
        nextExportExpiryAt: '',
        queuedRequestCount: 0,
        readyExportCount: 0,
        scheduledDeleteCount: 0,
      }),
      requests: [],
    })

    expect(workflow.summary).toBe('retention and legal-hold evidence is verified')
    expect(workflow.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 5, watch: 0 })
    expect(workflow.lanes.map((lane) => lane.status)).toEqual([
      'ready',
      'ready',
      'ready',
      'ready',
      'ready',
    ])
    expect(workflow.lanes.find((lane) => lane.key === 'delete_grace_window')).toMatchObject({
      evidence: '0 visible scheduled records / next execution none',
      status: 'ready',
    })
  })

  it('watches thin retention controls without blocking empty delete queues', () => {
    const workflow = buildRetentionLegalHoldWorkflow({
      operations: operations({
        auditRetentionDays: 14,
        backupsMayRetainUntilRotation: false,
        deleteGraceWindowSeconds: 0,
        legalHoldSupported: false,
        nextExportExpiryAt: '',
        readyExportCount: 0,
        scheduledDeleteCount: 0,
        stepUp: {
          method: 'password',
          passwordAllowed: true,
          satisfied: false,
          ttlSeconds: 45,
        },
      }),
      requests: [],
    })

    expect(workflow.fingerprint).toBe(
      '14d audit / missing delete grace / legal hold off / 0 request records',
    )
    expect(workflow.summary).toBe('3 retention and legal-hold checks need attention')
    expect(workflow.lanes.find((lane) => lane.key === 'retention_policy')).toMatchObject({
      evidence: 'export TTL 1d / audit prune 1h / step-up 45s',
      status: 'watch',
    })
    expect(workflow.lanes.find((lane) => lane.key === 'legal_hold_gate')).toMatchObject({
      status: 'watch',
    })
    expect(workflow.lanes.find((lane) => lane.key === 'delete_grace_window')).toMatchObject({
      signal: 'grace missing / 0 scheduled deletes / 0 visible',
      status: 'watch',
    })
  })

  it('blocks ready export residue when the expiry timestamp is absent', () => {
    const workflow = buildRetentionLegalHoldWorkflow({
      operations: operations({
        backupsMayRetainUntilRotation: false,
        legalHoldSupported: true,
        nextExportExpiryAt: '',
        readyExportCount: 1,
        scheduledDeleteCount: 0,
      }),
    })

    expect(workflow.summary).toBe('1 retention and legal-hold checks are blocked')
    expect(workflow.lanes.find((lane) => lane.key === 'export_residue')).toMatchObject({
      signal: '1 ready exports / expires none / TTL 1d',
      status: 'blocked',
    })
  })
})

function operations(overrides: Partial<GdprOperationsResponse> = {}): GdprOperationsResponse {
  return {
    activeRequestCount: 1,
    auditPruneIntervalSeconds: 3_600,
    auditRetentionDays: 30,
    backupsMayRetainUntilRotation: true,
    deleteGraceWindowSeconds: 3_600,
    exportTtlSeconds: 86_400,
    hashedAuditResidue: true,
    legalHoldSupported: false,
    nextExportExpiryAt: '2026-06-25T09:00:00Z',
    queuedRequestCount: 1,
    readyExportCount: 1,
    scheduledDeleteCount: 1,
    stepUp: {
      method: 'password',
      passwordAllowed: true,
      satisfied: false,
      ttlSeconds: 900,
    },
    ...overrides,
  }
}

function request(
  requestId: string,
  requestType: GdprRequestType,
  status: GdprRequestStatus,
  overrides: Partial<GdprRequestSummary> = {},
): GdprRequestSummary {
  return {
    ...requestBase(),
    requestId,
    requestType,
    status,
    ...overrides,
  }
}

function requestBase() {
  return {
    archiveFilename: '',
    createdAt: '2026-06-24T09:00:00Z',
    createdBy: 'admin',
    feedbackAuditCount: 1,
    feedbackCount: 1,
    llmAuditCount: 1,
    outboxCount: 0,
    subjectDisplay: 'Ada Lovelace',
    subjectKey: 'ada@example.com',
    surveyInvitationCount: 0,
    surveyLowScoreReviewCount: 0,
    surveyProviderEventCount: 0,
    surveyRecoveryNotificationCount: 0,
    surveyResponseCount: 0,
    tagAssignmentCount: 0,
  }
}
