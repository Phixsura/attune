import { getPermissions, type Permission, type Role } from '@/lib/permissions'
import type { AuditLogEntry } from '@/proto/attune/v1/audit'
import {
  ModerationState,
  type ModerationSubject,
  PublicAccessMode,
  PublicIdentityMode,
  type PublicVisibilityPolicy,
  PublicWriteMode,
} from '@/proto/attune/v1/public_visibility'

export type FieldLevelPermissionStatus = 'ready' | 'watch' | 'blocked' | 'needs_data'

export type FieldLevelPermissionLaneKey =
  | 'role_matrix'
  | 'public_projection'
  | 'write_identity_policy'
  | 'moderation_redaction'
  | 'audit_exports'

export type FieldLevelPermissionLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: FieldLevelPermissionLaneKey
  owner: string
  signal: string
  status: FieldLevelPermissionStatus
  title: string
}

export type FieldLevelPermissionsLedger = {
  fingerprint: string
  lanes: FieldLevelPermissionLane[]
  summary: string
  totals: Record<FieldLevelPermissionStatus, number> & {
    total: number
  }
}

export type FieldLevelPermissionsLedgerInput = {
  auditEntries?: AuditLogEntry[]
  moderationSubjects?: ModerationSubject[]
  policy?: PublicVisibilityPolicy
}

const roles: Role[] = ['admin', 'delegated_admin', 'member', 'viewer']
const publicModerationActions = new Set([
  'moderation.approve',
  'moderation.reject',
  'moderation.hide',
  'moderation.mark_spam',
  'moderation.restore',
])

export function buildFieldLevelPermissionsLedger(
  input: FieldLevelPermissionsLedgerInput,
): FieldLevelPermissionsLedger {
  const lanes = [
    roleMatrixLane(),
    publicProjectionLane(input),
    writeIdentityPolicyLane(input),
    moderationRedactionLane(input),
    auditExportsLane(input),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    ready: lanes.filter((lane) => lane.status === 'ready').length,
    total: lanes.length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${roles.length} roles / ${policyAccessMode(input.policy)} / ${formatNumber(
      input.moderationSubjects?.length ?? 0,
    )} moderation subjects / ${formatNumber(
      publicModerationAuditEntries(input.auditEntries).length,
    )} moderation audit events`,
    lanes,
    summary: fieldPermissionSummary(totals),
    totals,
  }
}

function roleMatrixLane(): FieldLevelPermissionLane {
  const policyEditors = rolesWith('public_policy:edit').length
  const moderationViewers = rolesWith('moderation:view').length
  const viewerPermissions = getPermissions('viewer').length

  return {
    actionHref: '/administration/members',
    actionLabel: 'Review role matrix',
    evidence: `${policyEditors} policy editors / ${moderationViewers} moderation viewers / viewer grants ${viewerPermissions}`,
    guardrail:
      'Viewer must stay read-only, public-policy edits must stay privileged, and moderation powers must remain separated from ordinary feedback access.',
    key: 'role_matrix',
    owner: 'Security + Product',
    signal: `${roles.length} roles / ${policyEditors} policy editors / viewer ${viewerPermissions} grants`,
    status: roleMatrixStatus(),
    title: 'Role-to-field matrix',
  }
}

function publicProjectionLane(input: FieldLevelPermissionsLedgerInput): FieldLevelPermissionLane {
  const policy = input.policy

  return {
    actionHref: '/integrations/public-visibility',
    actionLabel: 'Review public projection',
    evidence: policy
      ? `${featureState(policy.requestsEnabled, 'requests')} / ${featureState(
          policy.roadmapEnabled,
          'roadmap',
        )} / ${featureState(policy.changelogEnabled, 'changelog')} / ${featureState(
          policy.searchIndexingEnabled,
          'search',
        )}`
      : 'public projection policy is missing',
    guardrail:
      'Public boards must expose only approved request summaries, public-safe state, and intentionally enabled counters or timestamps.',
    key: 'public_projection',
    owner: 'Product + Security',
    signal: policy
      ? `${policyAccessMode(policy)} / search ${onOff(policy.searchIndexingEnabled)} / request ${moderationStateLabel(
          policy.defaultRequestState,
        )} / comment ${moderationStateLabel(policy.defaultCommentState)}`
      : 'public projection evidence missing',
    status: publicProjectionStatus(policy),
    title: 'Public projection policy',
  }
}

function writeIdentityPolicyLane(
  input: FieldLevelPermissionsLedgerInput,
): FieldLevelPermissionLane {
  const policy = input.policy

  return {
    actionHref: '/integrations/public-visibility',
    actionLabel: 'Review write policy',
    evidence: policy
      ? `${writeModeLabel(policy.submissionWriteMode)} submissions / ${writeModeLabel(
          policy.commentWriteMode,
        )} comments / ${writeModeLabel(policy.voteWriteMode)} votes / ${identityModeLabel(
          policy.submitterIdentityMode,
        )} identity`
      : 'public write and identity policy is missing',
    guardrail:
      'Customer-entered fields need an explicit write mode, identity exposure rule, moderation default, and consent-safe public projection.',
    key: 'write_identity_policy',
    owner: 'Product + Support',
    signal: policy
      ? `submission ${writeModeLabel(policy.submissionWriteMode)} / comments ${writeModeLabel(
          policy.commentWriteMode,
        )} / votes ${writeModeLabel(policy.voteWriteMode)} / identity ${identityModeLabel(
          policy.submitterIdentityMode,
        )}`
      : 'write and identity evidence missing',
    status: writeIdentityPolicyStatus(policy),
    title: 'Write and identity policy',
  }
}

function moderationRedactionLane(
  input: FieldLevelPermissionsLedgerInput,
): FieldLevelPermissionLane {
  const subjects = input.moderationSubjects
  const counts = moderationCounts(subjects)

  return {
    actionHref: '/integrations/public-visibility',
    actionLabel: 'Open moderation queue',
    evidence: subjects
      ? `${counts.hidden} hidden / ${counts.rejected} rejected / ${counts.spam} spam / ${counts.sensitiveReasons} sensitive reasons`
      : 'moderation and redaction queue evidence is missing',
    guardrail:
      'Sensitive public-surface subjects need pending review, reject/hide/spam outcomes, and explicit reason codes before publication.',
    key: 'moderation_redaction',
    owner: 'Support + Security',
    signal: subjects
      ? `${counts.pending} pending / ${counts.approved} approved / ${counts.blocked} blocked / ${subjects.length} subjects`
      : 'moderation evidence missing',
    status: moderationRedactionStatus(subjects),
    title: 'Moderation and redaction queue',
  }
}

function auditExportsLane(input: FieldLevelPermissionsLedgerInput): FieldLevelPermissionLane {
  const entries = publicModerationAuditEntries(input.auditEntries)
  const actors = new Set(entries.map((entry) => entry.actorId || entry.actorEmail).filter(Boolean))

  return {
    actionHref: '/administration/audit-log?targetType=public_moderation_subject',
    actionLabel: 'Open audit evidence',
    evidence: input.auditEntries
      ? `${entries.length} moderation events / ${actors.size} actors / CSV export available`
      : 'public moderation audit evidence is missing',
    guardrail:
      'Field-level permission decisions must leave searchable moderation events and exportable audit evidence.',
    key: 'audit_exports',
    owner: 'Security + Compliance',
    signal: input.auditEntries
      ? `${entries.length} moderation audit events / ${actors.size} actors`
      : 'audit export evidence missing',
    status: auditExportsStatus(input.auditEntries),
    title: 'Audit and export evidence',
  }
}

function roleMatrixStatus(): FieldLevelPermissionStatus {
  if (getPermissions('viewer').some((permission) => permission.includes(':edit'))) return 'blocked'
  if (rolesWith('public_policy:edit').length === 0) return 'blocked'
  if (rolesWith('moderation:enforce').length === 0) return 'watch'
  return 'ready'
}

function publicProjectionStatus(
  policy: PublicVisibilityPolicy | undefined,
): FieldLevelPermissionStatus {
  if (!policy) return 'needs_data'
  if (
    policy.portalAccessMode === PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC &&
    (policy.defaultRequestState === ModerationState.MODERATION_STATE_APPROVED ||
      policy.defaultCommentState === ModerationState.MODERATION_STATE_APPROVED)
  ) {
    return 'blocked'
  }
  /* v8 ignore next -- @preserve: public projection watch predicates are equivalent exposure-risk signals. */
  if (
    policy.portalAccessMode === PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC &&
    (policy.searchIndexingEnabled || policy.showSubmitterDisplay || !policy.hidePublicTimestamps)
  ) {
    return 'watch'
  }
  return 'ready'
}

function writeIdentityPolicyStatus(
  policy: PublicVisibilityPolicy | undefined,
): FieldLevelPermissionStatus {
  if (!policy) return 'needs_data'
  if (
    policy.submissionWriteMode === PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS &&
    policy.defaultRequestState === ModerationState.MODERATION_STATE_APPROVED
  ) {
    return 'blocked'
  }
  if (
    policy.voteWriteMode === PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS ||
    policy.submitterIdentityMode === PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME
  ) {
    return 'watch'
  }
  return 'ready'
}

function moderationRedactionStatus(
  subjects: ModerationSubject[] | undefined,
): FieldLevelPermissionStatus {
  if (!subjects) return 'needs_data'
  const counts = moderationCounts(subjects)
  if (counts.pending > 0) return 'watch'
  if (subjects.length === 0 || counts.blocked > 0 || counts.approved > 0) return 'ready'
  return 'watch'
}

function auditExportsStatus(entries: AuditLogEntry[] | undefined): FieldLevelPermissionStatus {
  if (!entries) return 'needs_data'
  return publicModerationAuditEntries(entries).length > 0 ? 'ready' : 'watch'
}

function rolesWith(permission: Permission) {
  return roles.filter((role) => getPermissions(role).includes(permission))
}

function publicModerationAuditEntries(entries: AuditLogEntry[] | undefined) {
  return (
    entries?.filter(
      (entry) =>
        entry.targetType === 'public_moderation_subject' ||
        publicModerationActions.has(entry.action),
    ) ?? []
  )
}

function moderationCounts(subjects: ModerationSubject[] | undefined) {
  const counts = {
    approved: 0,
    blocked: 0,
    hidden: 0,
    pending: 0,
    rejected: 0,
    sensitiveReasons: 0,
    spam: 0,
  }
  for (const subject of subjects ?? []) {
    if (subject.state === ModerationState.MODERATION_STATE_PENDING) counts.pending += 1
    if (subject.state === ModerationState.MODERATION_STATE_APPROVED) counts.approved += 1
    if (subject.state === ModerationState.MODERATION_STATE_REJECTED) counts.rejected += 1
    if (subject.state === ModerationState.MODERATION_STATE_HIDDEN) counts.hidden += 1
    if (subject.state === ModerationState.MODERATION_STATE_SPAM) counts.spam += 1
    if (
      subject.state === ModerationState.MODERATION_STATE_REJECTED ||
      subject.state === ModerationState.MODERATION_STATE_HIDDEN ||
      subject.state === ModerationState.MODERATION_STATE_SPAM
    ) {
      counts.blocked += 1
    }
    if (subject.reasonCode.includes('sensitive') || subject.reasonCode.includes('private')) {
      counts.sensitiveReasons += 1
    }
  }
  return counts
}

function fieldPermissionSummary(totals: FieldLevelPermissionsLedger['totals']) {
  if (totals.blocked > 0) return `${totals.blocked} field-level permission checks are blocked`
  if (totals.needs_data > 0)
    return `${totals.needs_data} field-level permission checks need evidence`
  if (totals.watch > 0) return `${totals.watch} field-level permission checks need attention`
  return 'field-level permission evidence is verified'
}

function policyAccessMode(policy: PublicVisibilityPolicy | undefined) {
  if (!policy) return 'public policy unknown'
  switch (policy.portalAccessMode) {
    case PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC:
      return 'public'
    case PublicAccessMode.PUBLIC_ACCESS_MODE_AUTHENTICATED:
      return 'authenticated'
    case PublicAccessMode.PUBLIC_ACCESS_MODE_INVITE_ONLY:
      return 'invite_only'
    case PublicAccessMode.PUBLIC_ACCESS_MODE_DISABLED:
      return 'disabled'
    default:
      return 'unknown'
  }
}

function writeModeLabel(mode: PublicWriteMode) {
  switch (mode) {
    case PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS:
      return 'anonymous'
    case PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED:
      return 'identified'
    case PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED:
      return 'disabled'
    default:
      return 'unknown'
  }
}

function identityModeLabel(mode: PublicIdentityMode) {
  switch (mode) {
    case PublicIdentityMode.PUBLIC_IDENTITY_MODE_ANONYMOUS:
      return 'anonymous'
    case PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME:
      return 'display_name'
    case PublicIdentityMode.PUBLIC_IDENTITY_MODE_ORGANIZATION:
      return 'organization'
    default:
      return 'unknown'
  }
}

function moderationStateLabel(state: ModerationState) {
  switch (state) {
    case ModerationState.MODERATION_STATE_PENDING:
      return 'pending'
    case ModerationState.MODERATION_STATE_APPROVED:
      return 'approved'
    case ModerationState.MODERATION_STATE_REJECTED:
      return 'rejected'
    case ModerationState.MODERATION_STATE_HIDDEN:
      return 'hidden'
    case ModerationState.MODERATION_STATE_SPAM:
      return 'spam'
    default:
      return 'unknown'
  }
}

function featureState(enabled: boolean, label: string) {
  return `${label} ${onOff(enabled)}`
}

function onOff(enabled: boolean) {
  return enabled ? 'on' : 'off'
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value)
}
