import { describe, expect, it, vi } from 'vitest'
import type { AuditLogEntry } from '@/proto/attune/v1/audit'
import {
  ModerationState,
  type ModerationSubject,
  PublicAccessMode,
  PublicIdentityMode,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
} from '@/proto/attune/v1/public_visibility'
import { buildFieldLevelPermissionsLedger } from './field-level-permissions-ledger'

describe('buildFieldLevelPermissionsLedger', () => {
  it('joins role, public projection, write policy, moderation, and audit evidence', () => {
    const ledger = buildFieldLevelPermissionsLedger({
      auditEntries: [moderationAuditEntry()],
      moderationSubjects: [
        moderationSubject('pending', ModerationState.MODERATION_STATE_PENDING),
        moderationSubject('approved', ModerationState.MODERATION_STATE_APPROVED),
      ],
      policy: publicPolicy(),
    })

    expect(ledger.fingerprint).toBe(
      '4 roles / public / 2 moderation subjects / 1 moderation audit events',
    )
    expect(ledger.summary).toBe('3 field-level permission checks need attention')
    expect(ledger.totals).toMatchObject({
      blocked: 0,
      needs_data: 0,
      ready: 2,
      total: 5,
      watch: 3,
    })
    expect(ledger.lanes.find((lane) => lane.key === 'role_matrix')).toMatchObject({
      signal: '4 roles / 2 policy editors / viewer 3 grants',
      status: 'ready',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'public_projection')).toMatchObject({
      signal: 'public / search off / request pending / comment pending',
      status: 'watch',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'write_identity_policy')).toMatchObject({
      signal: 'submission identified / comments disabled / votes anonymous / identity display_name',
      status: 'watch',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'moderation_redaction')).toMatchObject({
      signal: '1 pending / 1 approved / 0 blocked / 2 subjects',
      status: 'watch',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'audit_exports')).toMatchObject({
      signal: '1 moderation audit events / 1 actors',
      status: 'ready',
    })
  })

  it('blocks anonymous auto-public projections', () => {
    const ledger = buildFieldLevelPermissionsLedger({
      auditEntries: [],
      moderationSubjects: [],
      policy: publicPolicy({
        defaultCommentState: ModerationState.MODERATION_STATE_APPROVED,
        defaultRequestState: ModerationState.MODERATION_STATE_APPROVED,
        searchIndexingEnabled: true,
        submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
      }),
    })

    expect(ledger.summary).toBe('2 field-level permission checks are blocked')
    expect(ledger.totals).toMatchObject({ blocked: 2, ready: 2, watch: 1 })
    expect(ledger.lanes.find((lane) => lane.key === 'public_projection')).toMatchObject({
      signal: 'public / search on / request approved / comment approved',
      status: 'blocked',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'write_identity_policy')).toMatchObject({
      signal: 'submission anonymous / comments disabled / votes anonymous / identity display_name',
      status: 'blocked',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'audit_exports')).toMatchObject({
      signal: '0 moderation audit events / 0 actors',
      status: 'watch',
    })
  })

  it('keeps absent public-surface evidence visible as data gaps', () => {
    const ledger = buildFieldLevelPermissionsLedger({})

    expect(ledger.fingerprint).toBe(
      '4 roles / public policy unknown / 0 moderation subjects / 0 moderation audit events',
    )
    expect(ledger.summary).toBe('4 field-level permission checks need evidence')
    expect(ledger.totals).toMatchObject({
      blocked: 0,
      needs_data: 4,
      ready: 1,
      total: 5,
      watch: 0,
    })
  })

  it('watches public projections when search indexing is enabled without auto approval', () => {
    const ledger = buildFieldLevelPermissionsLedger({
      auditEntries: [],
      moderationSubjects: [],
      policy: publicPolicy({
        defaultCommentState: ModerationState.MODERATION_STATE_PENDING,
        defaultRequestState: ModerationState.MODERATION_STATE_PENDING,
        hidePublicTimestamps: true,
        searchIndexingEnabled: true,
        showSubmitterDisplay: false,
      }),
    })

    expect(ledger.lanes.find((lane) => lane.key === 'public_projection')).toMatchObject({
      signal: 'public / search on / request pending / comment pending',
      status: 'watch',
    })
  })

  it('verifies locked-down authenticated surfaces with complete moderation audit evidence', () => {
    const ledger = buildFieldLevelPermissionsLedger({
      auditEntries: [
        moderationAuditEntry({
          action: 'policy.update',
          actorEmail: 'security@example.com',
          actorId: '',
          targetType: 'public_moderation_subject',
        }),
      ],
      moderationSubjects: [
        moderationSubject('rejected', ModerationState.MODERATION_STATE_REJECTED, 'sensitive_data'),
        moderationSubject('hidden', ModerationState.MODERATION_STATE_HIDDEN, 'private_note'),
        moderationSubject('spam', ModerationState.MODERATION_STATE_SPAM, 'spam'),
      ],
      policy: publicPolicy({
        commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
        hidePublicTimestamps: true,
        portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_AUTHENTICATED,
        searchIndexingEnabled: false,
        showSubmitterDisplay: false,
        submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_ORGANIZATION,
        voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
      }),
    })

    expect(ledger.fingerprint).toBe(
      '4 roles / authenticated / 3 moderation subjects / 1 moderation audit events',
    )
    expect(ledger.summary).toBe('field-level permission evidence is verified')
    expect(ledger.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 5, watch: 0 })
    expect(ledger.lanes.find((lane) => lane.key === 'write_identity_policy')).toMatchObject({
      signal:
        'submission identified / comments identified / votes disabled / identity organization',
      status: 'ready',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'moderation_redaction')).toMatchObject({
      evidence: '1 hidden / 1 rejected / 1 spam / 2 sensitive reasons',
      signal: '0 pending / 0 approved / 3 blocked / 3 subjects',
      status: 'ready',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'audit_exports')).toMatchObject({
      signal: '1 moderation audit events / 1 actors',
      status: 'ready',
    })
  })

  it.each([
    [PublicAccessMode.PUBLIC_ACCESS_MODE_INVITE_ONLY, 'invite_only'],
    [PublicAccessMode.PUBLIC_ACCESS_MODE_DISABLED, 'disabled'],
    [PublicAccessMode.UNRECOGNIZED, 'unknown'],
  ])('labels %s projection mode in evidence', (portalAccessMode, label) => {
    const ledger = buildFieldLevelPermissionsLedger({
      auditEntries: [],
      moderationSubjects: [],
      policy: publicPolicy({
        defaultCommentState: ModerationState.MODERATION_STATE_REJECTED,
        defaultRequestState: ModerationState.MODERATION_STATE_HIDDEN,
        portalAccessMode,
        submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_ANONYMOUS,
        voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
      }),
    })

    expect(ledger.fingerprint).toContain(`4 roles / ${label}`)
    expect(ledger.lanes.find((lane) => lane.key === 'public_projection')).toMatchObject({
      signal: `${label} / search off / request hidden / comment rejected`,
    })
    expect(ledger.lanes.find((lane) => lane.key === 'write_identity_policy')).toMatchObject({
      signal: 'submission identified / comments disabled / votes identified / identity anonymous',
    })
  })

  it('keeps unrecognized write, identity, and moderation enum values visible as unknown', () => {
    const ledger = buildFieldLevelPermissionsLedger({
      auditEntries: [
        { ...moderationAuditEntry(), action: 'ticket.update', targetType: 'customer_request' },
      ],
      moderationSubjects: [moderationSubject('unknown', ModerationState.UNRECOGNIZED)],
      policy: publicPolicy({
        commentWriteMode: PublicWriteMode.UNRECOGNIZED,
        defaultCommentState: ModerationState.UNRECOGNIZED,
        defaultRequestState: ModerationState.MODERATION_STATE_SPAM,
        submissionWriteMode: PublicWriteMode.UNRECOGNIZED,
        submitterIdentityMode: PublicIdentityMode.UNRECOGNIZED,
        voteWriteMode: PublicWriteMode.UNRECOGNIZED,
      }),
    })

    expect(ledger.summary).toBe('3 field-level permission checks need attention')
    expect(ledger.lanes.find((lane) => lane.key === 'public_projection')).toMatchObject({
      signal: 'public / search off / request spam / comment unknown',
      status: 'watch',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'write_identity_policy')).toMatchObject({
      signal: 'submission unknown / comments unknown / votes unknown / identity unknown',
      status: 'ready',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'moderation_redaction')).toMatchObject({
      signal: '0 pending / 0 approved / 0 blocked / 1 subjects',
      status: 'watch',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'audit_exports')).toMatchObject({
      signal: '0 moderation audit events / 0 actors',
      status: 'watch',
    })
  })

  it.each([
    [
      'blocks viewer edit grants',
      {
        admin: ['public_policy:edit', 'moderation:enforce'],
        delegated_admin: [],
        member: [],
        viewer: ['feedback:edit'],
      },
      'blocked',
    ],
    [
      'blocks missing public policy editors',
      {
        admin: ['moderation:enforce'],
        delegated_admin: [],
        member: [],
        viewer: ['feedback:view'],
      },
      'blocked',
    ],
    [
      'watches missing moderation enforcement grants',
      {
        admin: ['public_policy:edit'],
        delegated_admin: [],
        member: [],
        viewer: ['feedback:view'],
      },
      'watch',
    ],
  ])('%s in the role matrix', async (_name, permissionsByRole, status) => {
    const ledger = await buildLedgerWithPermissions(permissionsByRole)

    expect(ledger.lanes.find((lane) => lane.key === 'role_matrix')).toMatchObject({ status })
  })
})

function publicPolicy(overrides: Partial<PublicVisibilityPolicy> = {}): PublicVisibilityPolicy {
  return {
    changelogEnabled: true,
    commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
    commentsEnabled: true,
    createdAt: '2026-07-10T00:00:00Z',
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
    tenantId: 'tenant-a',
    updatedAt: '2026-07-10T00:05:00Z',
    updatedBy: 'admin-1',
    voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
    ...overrides,
  }
}

function moderationSubject(
  id: string,
  state: ModerationState,
  reasonCode = state === ModerationState.MODERATION_STATE_APPROVED ? 'safe' : '',
): ModerationSubject {
  return {
    createdAt: '2026-07-10T00:01:00Z',
    id,
    reasonCode,
    reasonNote: '',
    reviewedAt: state === ModerationState.MODERATION_STATE_APPROVED ? '2026-07-10T00:04:00Z' : '',
    reviewedBy: state === ModerationState.MODERATION_STATE_APPROVED ? 'admin-1' : '',
    state,
    subjectId: `subject-${id}`,
    submittedByDisplay: 'Ada Lovelace',
    surface: PublicSurface.PUBLIC_SURFACE_REQUEST,
    tenantId: 'tenant-a',
    updatedAt: '2026-07-10T00:04:00Z',
  }
}

function moderationAuditEntry(overrides: Partial<AuditLogEntry> = {}): AuditLogEntry {
  return {
    action: 'moderation.hide',
    actorEmail: 'admin@example.com',
    actorId: 'admin-1',
    actorIp: '127.0.0.1',
    actorType: 'admin',
    actorUserAgent: 'vitest',
    afterJson: '{}',
    beforeJson: '{}',
    createdAt: '2026-07-10T00:04:00Z',
    id: 'audit-moderation',
    summary: 'Changed public moderation state',
    targetId: 'subject-pending',
    targetType: 'public_moderation_subject',
    ...overrides,
  }
}

async function buildLedgerWithPermissions(permissionsByRole: Record<string, string[]>) {
  vi.resetModules()
  vi.doMock('@/lib/permissions', () => ({
    getPermissions: (role: string) => permissionsByRole[role] ?? [],
  }))
  const { buildFieldLevelPermissionsLedger: buildLedger } = await import(
    './field-level-permissions-ledger'
  )
  const ledger = buildLedger({})
  vi.doUnmock('@/lib/permissions')
  vi.resetModules()
  return ledger
}
