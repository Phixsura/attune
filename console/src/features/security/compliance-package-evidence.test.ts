import { describe, expect, it } from 'vitest'
import type { GdprOperationsResponse } from '@/proto/attune/v1/gdpr'
import type { NotifyTarget } from '@/proto/attune/v1/notify_target'
import {
  ModerationState,
  type ModerationSubject,
  PublicAccessMode,
  PublicIdentityMode,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
} from '@/proto/attune/v1/public_visibility'
import { buildCompliancePackageEvidence } from './compliance-package-evidence'

describe('buildCompliancePackageEvidence', () => {
  it('joins access, data-flow, audit, retention, and subprocessor evidence', () => {
    const evidence = buildCompliancePackageEvidence({
      auditEntries: [audit('member.update_role'), audit('moderation.hide')],
      authMode: { mode: 'hybrid' },
      gdprOperations: gdprOperations(),
      lockouts: [],
      members: [
        member('admin-1', 'admin'),
        member('admin-2', 'admin'),
        member('member-1', 'member'),
        member('viewer-1', 'viewer'),
      ],
      moderationSubjects: [
        moderationSubject('pending', ModerationState.MODERATION_STATE_PENDING),
        moderationSubject('approved', ModerationState.MODERATION_STATE_APPROVED),
      ],
      notifyTargets: [notifyTarget()],
      publicVisibilityPolicy: publicPolicy(),
      tokens: [breakglassToken()],
    })

    expect(evidence.fingerprint).toBe(
      'hybrid / 4 active members / 4 public surfaces / 1 outbound targets / 2 audit events',
    )
    expect(evidence.summary).toBe('1 compliance package checks are blocked')
    expect(evidence.totals).toMatchObject({
      blocked: 1,
      needs_data: 0,
      ready: 2,
      total: 5,
      watch: 2,
    })
    expect(evidence.lanes.find((lane) => lane.key === 'control_mapping')).toMatchObject({
      signal: 'hybrid / 2 admins / 3 roles / 1 break-glass',
      status: 'watch',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'data_flow_inventory')).toMatchObject({
      signal: '4 public surfaces / identity display_name / moderation 2 subjects',
      status: 'watch',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'audit_evidence_package')).toMatchObject({
      signal: '2 audit events / 1 actors / 2 action types',
      status: 'ready',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'retention_dsr')).toMatchObject({
      signal: 'audit 30d / legal hold off / 1 scheduled deletes',
      status: 'blocked',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'subprocessor_boundary')).toMatchObject({
      signal: '1 enabled outbound / 0 failing / 1 HTTPS',
      status: 'ready',
    })
  })

  it('blocks non-HTTPS outbound targets and missing admin continuity', () => {
    const evidence = buildCompliancePackageEvidence({
      auditEntries: [],
      authMode: { mode: 'sso_only' },
      gdprOperations: gdprOperations({
        backupsMayRetainUntilRotation: false,
        legalHoldSupported: true,
        readyExportCount: 0,
        scheduledDeleteCount: 0,
      }),
      lockouts: [],
      members: [member('member-1', 'member')],
      moderationSubjects: [],
      notifyTargets: [notifyTarget({ url: 'http://example.com/hook' })],
      publicVisibilityPolicy: publicPolicy({
        changelogEnabled: false,
        commentsEnabled: false,
        hidePublicTimestamps: true,
        requestsEnabled: false,
        roadmapEnabled: false,
        showCommentCount: false,
        showSubmitterDisplay: false,
        showVoteCount: false,
      }),
      tokens: [],
    })

    expect(evidence.summary).toBe('2 compliance package checks are blocked')
    expect(evidence.totals).toMatchObject({ blocked: 2, ready: 2, watch: 1 })
    expect(evidence.lanes.find((lane) => lane.key === 'control_mapping')).toMatchObject({
      status: 'blocked',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'subprocessor_boundary')).toMatchObject({
      status: 'blocked',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'audit_evidence_package')).toMatchObject({
      status: 'watch',
    })
  })

  it('keeps missing compliance package inputs explicit', () => {
    const evidence = buildCompliancePackageEvidence({})

    expect(evidence.fingerprint).toBe(
      'auth unknown / 0 active members / 0 public surfaces / 0 outbound targets / 0 audit events',
    )
    expect(evidence.summary).toBe('5 compliance package checks need evidence')
    expect(evidence.totals).toMatchObject({
      blocked: 0,
      needs_data: 5,
      ready: 0,
      total: 5,
      watch: 0,
    })
  })

  it('verifies a private SSO-only compliance package with complete evidence', () => {
    const evidence = buildCompliancePackageEvidence({
      auditEntries: [audit('member.invite'), audit('data.export')],
      authMode: { mode: 'sso_only' },
      gdprOperations: gdprOperations({
        backupsMayRetainUntilRotation: false,
        legalHoldSupported: true,
        readyExportCount: 0,
        scheduledDeleteCount: 0,
      }),
      lockouts: [],
      members: [
        member('admin-1', 'admin'),
        member('admin-2', 'admin'),
        member('member-1', 'member'),
        member('viewer-1', 'viewer'),
      ],
      moderationSubjects: [],
      notifyTargets: [notifyTarget()],
      publicVisibilityPolicy: publicPolicy({
        changelogEnabled: false,
        commentsEnabled: false,
        hidePublicTimestamps: true,
        requestsEnabled: false,
        roadmapEnabled: false,
        showCommentCount: false,
        showSubmitterDisplay: false,
        showVoteCount: false,
        submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_ORGANIZATION,
      }),
      tokens: [breakglassToken()],
    })

    expect(evidence.summary).toBe('compliance package evidence is verified')
    expect(evidence.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 5, watch: 0 })
    expect(evidence.lanes.find((lane) => lane.key === 'data_flow_inventory')).toMatchObject({
      signal: '0 public surfaces / identity organization / moderation 0 subjects',
      status: 'ready',
    })
  })

  it('blocks public auto-approval data flows even when the rest of the package is present', () => {
    const evidence = buildCompliancePackageEvidence({
      auditEntries: [audit('member.update_role')],
      authMode: { mode: 'sso_only' },
      gdprOperations: gdprOperations({
        backupsMayRetainUntilRotation: false,
        legalHoldSupported: true,
        readyExportCount: 0,
        scheduledDeleteCount: 0,
      }),
      lockouts: [],
      members: [
        member('admin-1', 'admin'),
        member('admin-2', 'admin'),
        member('member-1', 'member'),
        member('viewer-1', 'viewer'),
      ],
      moderationSubjects: [],
      notifyTargets: [notifyTarget()],
      publicVisibilityPolicy: publicPolicy({
        defaultCommentState: ModerationState.MODERATION_STATE_APPROVED,
        defaultRequestState: ModerationState.MODERATION_STATE_APPROVED,
      }),
      tokens: [breakglassToken()],
    })

    expect(evidence.summary).toBe('1 compliance package checks are blocked')
    expect(evidence.lanes.find((lane) => lane.key === 'data_flow_inventory')).toMatchObject({
      status: 'blocked',
    })
  })

  it('watches recoverable package gaps across access, audit, retention, and outbound targets', () => {
    const evidence = buildCompliancePackageEvidence({
      auditEntries: [],
      authMode: { mode: 'hybrid' },
      gdprOperations: gdprOperations({
        deleteGraceWindowSeconds: 120,
        exportTtlSeconds: 90,
        legalHoldSupported: false,
        readyExportCount: 2,
        scheduledDeleteCount: 0,
      }),
      lockouts: [
        {
          attempts: 5,
          ip: '203.0.113.10',
          locked_until: '2026-07-01T00:30:00Z',
          remaining_mins: 10,
        },
      ],
      members: [member('admin-1', 'admin'), member('viewer-1', 'viewer')],
      moderationSubjects: [],
      notifyTargets: [notifyTarget({ lastError: '500' })],
      publicVisibilityPolicy: publicPolicy({
        changelogEnabled: false,
        commentsEnabled: false,
        hidePublicTimestamps: true,
        requestsEnabled: false,
        roadmapEnabled: false,
        showCommentCount: false,
        showSubmitterDisplay: false,
        showVoteCount: false,
        submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_ANONYMOUS,
      }),
      tokens: [{ ...breakglassToken(), revoked_at: '2026-07-02T00:00:00Z' }],
    })

    expect(evidence.summary).toBe('4 compliance package checks need attention')
    expect(evidence.lanes.find((lane) => lane.key === 'control_mapping')).toMatchObject({
      signal: 'hybrid / 1 admins / 2 roles / 0 break-glass',
      status: 'watch',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'audit_evidence_package')).toMatchObject({
      status: 'watch',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'retention_dsr')).toMatchObject({
      evidence: 'export TTL 90s / audit 30d / delete grace 2m / hashed audit on',
      status: 'watch',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'subprocessor_boundary')).toMatchObject({
      status: 'watch',
    })
  })

  it('keeps disabled outbound inventories and unknown identity modes visible', () => {
    const evidence = buildCompliancePackageEvidence({
      auditEntries: [audit('member.remove')],
      authMode: { mode: 'sso_only' },
      gdprOperations: gdprOperations({
        auditRetentionDays: 0,
        deleteGraceWindowSeconds: 0,
        exportTtlSeconds: 0,
        hashedAuditResidue: false,
      }),
      lockouts: [],
      members: [
        member('admin-1', 'admin'),
        member('admin-2', 'admin'),
        member('member-1', 'member'),
        member('viewer-1', 'viewer'),
      ],
      moderationSubjects: [],
      notifyTargets: [notifyTarget({ disabled: true })],
      publicVisibilityPolicy: publicPolicy({
        changelogEnabled: false,
        commentsEnabled: false,
        hidePublicTimestamps: true,
        requestsEnabled: false,
        roadmapEnabled: false,
        showCommentCount: false,
        showSubmitterDisplay: false,
        showVoteCount: false,
        submitterIdentityMode: PublicIdentityMode.UNRECOGNIZED,
      }),
      tokens: [breakglassToken()],
    })

    expect(evidence.summary).toBe('1 compliance package checks are blocked')
    expect(evidence.lanes.find((lane) => lane.key === 'data_flow_inventory')).toMatchObject({
      signal: '0 public surfaces / identity unknown / moderation 0 subjects',
      status: 'ready',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'retention_dsr')).toMatchObject({
      evidence: 'export TTL missing / audit missing / delete grace missing / hashed audit off',
      status: 'blocked',
    })
    expect(evidence.lanes.find((lane) => lane.key === 'subprocessor_boundary')).toMatchObject({
      signal: '0 enabled outbound / 0 failing / 0 HTTPS',
      status: 'watch',
    })
  })

  it('watches private data-flow inventory while moderation review is still pending', () => {
    const evidence = buildCompliancePackageEvidence({
      auditEntries: [audit('moderation.update')],
      authMode: { mode: 'sso_only' },
      gdprOperations: gdprOperations({
        backupsMayRetainUntilRotation: false,
        legalHoldSupported: true,
        readyExportCount: 0,
        scheduledDeleteCount: 0,
      }),
      lockouts: [],
      members: [
        member('admin-1', 'admin'),
        member('admin-2', 'admin'),
        member('member-1', 'member'),
        member('viewer-1', 'viewer'),
      ],
      moderationSubjects: [moderationSubject('pending', ModerationState.MODERATION_STATE_PENDING)],
      notifyTargets: [notifyTarget()],
      publicVisibilityPolicy: publicPolicy({
        changelogEnabled: false,
        commentsEnabled: false,
        hidePublicTimestamps: true,
        requestsEnabled: false,
        roadmapEnabled: false,
        showCommentCount: false,
        showSubmitterDisplay: false,
        showVoteCount: false,
      }),
      tokens: [breakglassToken()],
    })

    expect(evidence.summary).toBe('1 compliance package checks need attention')
    expect(evidence.lanes.find((lane) => lane.key === 'data_flow_inventory')).toMatchObject({
      signal: '0 public surfaces / identity display_name / moderation 1 subjects',
      status: 'watch',
    })
  })

  it('blocks SSO-only control mapping without break-glass and watches narrow role breadth', () => {
    const privatePolicy = publicPolicy({
      changelogEnabled: false,
      commentsEnabled: false,
      hidePublicTimestamps: true,
      requestsEnabled: false,
      roadmapEnabled: false,
      showCommentCount: false,
      showSubmitterDisplay: false,
      showVoteCount: false,
    })
    const blocked = buildCompliancePackageEvidence({
      auditEntries: [
        { ...audit('member.update_role'), actorId: '', actorEmail: 'fallback@example.com' },
      ],
      authMode: { mode: 'sso_only' },
      gdprOperations: gdprOperations({
        backupsMayRetainUntilRotation: false,
        legalHoldSupported: true,
        readyExportCount: 0,
        scheduledDeleteCount: 0,
      }),
      lockouts: [],
      members: [
        member('admin-1', 'admin'),
        member('admin-2', 'admin'),
        member('viewer-1', 'viewer'),
      ],
      moderationSubjects: [],
      notifyTargets: [notifyTarget()],
      publicVisibilityPolicy: privatePolicy,
      tokens: [],
    })
    const watched = buildCompliancePackageEvidence({
      auditEntries: [
        { ...audit('member.update_role'), actorId: '', actorEmail: 'fallback@example.com' },
      ],
      authMode: { mode: 'sso_only' },
      gdprOperations: gdprOperations({
        backupsMayRetainUntilRotation: false,
        legalHoldSupported: true,
        readyExportCount: 0,
        scheduledDeleteCount: 0,
      }),
      lockouts: [],
      members: [
        member('admin-1', 'admin'),
        member('admin-2', 'admin'),
        member('viewer-1', 'viewer'),
      ],
      moderationSubjects: [],
      notifyTargets: [notifyTarget()],
      publicVisibilityPolicy: privatePolicy,
      tokens: [breakglassToken()],
    })

    expect(blocked.summary).toBe('1 compliance package checks are blocked')
    expect(blocked.lanes.find((lane) => lane.key === 'control_mapping')).toMatchObject({
      signal: 'sso_only / 2 admins / 2 roles / 0 break-glass',
      status: 'blocked',
    })
    expect(blocked.lanes.find((lane) => lane.key === 'audit_evidence_package')).toMatchObject({
      signal: '1 audit events / 1 actors / 1 action types',
      status: 'ready',
    })
    expect(watched.summary).toBe('1 compliance package checks need attention')
    expect(watched.lanes.find((lane) => lane.key === 'control_mapping')).toMatchObject({
      signal: 'sso_only / 2 admins / 2 roles / 1 break-glass',
      status: 'watch',
    })
  })
})

function audit(action: string) {
  return {
    action,
    actorEmail: 'admin@example.com',
    actorId: 'admin-1',
    actorIp: '127.0.0.1',
    actorType: 'admin',
    actorUserAgent: 'vitest',
    afterJson: '{}',
    beforeJson: '{}',
    createdAt: '2026-07-01T00:00:00Z',
    id: `audit-${action}`,
    summary: action,
    targetId: 'target-1',
    targetType: action.startsWith('member.') ? 'member' : 'public_moderation_subject',
  }
}

function gdprOperations(overrides: Partial<GdprOperationsResponse> = {}): GdprOperationsResponse {
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

function member(id: string, role: string) {
  return {
    acceptedAt: '1',
    email: `${id}@example.com`,
    id,
    invitedAt: '0',
    memberType: 'oidc_user',
    role,
    roleSource: 'idp',
    userId: id,
  }
}

function moderationSubject(id: string, state: ModerationState): ModerationSubject {
  return {
    createdAt: '2026-07-01T00:00:00Z',
    id,
    reasonCode: state === ModerationState.MODERATION_STATE_APPROVED ? 'safe' : '',
    reasonNote: '',
    reviewedBy: state === ModerationState.MODERATION_STATE_APPROVED ? 'admin-1' : '',
    state,
    subjectId: `subject-${id}`,
    submittedByDisplay: 'Ada Lovelace',
    surface: PublicSurface.PUBLIC_SURFACE_REQUEST,
    tenantId: 'tenant-1',
    updatedAt: '2026-07-01T00:00:00Z',
  }
}

function notifyTarget(overrides: Partial<NotifyTarget> = {}): NotifyTarget {
  return {
    audience: 'all',
    createdAt: '2026-07-01T00:00:00Z',
    destinationType: 'raw-webhook',
    disabled: false,
    id: 'notify-1',
    lastError: '',
    timeoutSeconds: 10,
    url: 'https://example.com/hook',
    ...overrides,
  }
}

function publicPolicy(overrides: Partial<PublicVisibilityPolicy> = {}): PublicVisibilityPolicy {
  return {
    changelogEnabled: true,
    commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
    commentsEnabled: true,
    createdAt: '2026-07-01T00:00:00Z',
    defaultCommentState: ModerationState.MODERATION_STATE_PENDING,
    defaultRequestState: ModerationState.MODERATION_STATE_PENDING,
    hidePublicTimestamps: false,
    portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC,
    portalSubmissionForm: undefined,
    requestsEnabled: true,
    roadmapEnabled: true,
    roadmapStatusMapping: [],
    searchIndexingEnabled: false,
    showCommentCount: true,
    showSubmitterDisplay: true,
    showVoteCount: true,
    submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
    submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME,
    tenantId: 'tenant-1',
    updatedAt: '2026-07-01T00:05:00Z',
    updatedBy: 'admin-1',
    voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
    ...overrides,
  }
}

function breakglassToken() {
  return {
    admin_email: 'admin@example.com',
    expires_at: '2099-07-01T00:00:00Z',
    id: 'token-1',
    issued_at: '2026-07-01T00:00:00Z',
    issued_by: 'admin-1',
    status: 'valid',
  }
}
