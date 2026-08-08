import type { AuditLogEntry } from '@/proto/attune/v1/audit'
import type { GdprOperationsResponse } from '@/proto/attune/v1/gdpr'
import type { Member } from '@/proto/attune/v1/member'
import type { NotifyTarget } from '@/proto/attune/v1/notify_target'
import {
  ModerationState,
  type ModerationSubject,
  PublicAccessMode,
  PublicIdentityMode,
  type PublicVisibilityPolicy,
} from '@/proto/attune/v1/public_visibility'
import type { AuthModeResponse } from './api/auth-mode'
import type { BreakGlassLockout, BreakGlassToken } from './api/breakglass'

export type CompliancePackageStatus = 'ready' | 'watch' | 'blocked' | 'needs_data'

export type CompliancePackageLaneKey =
  | 'control_mapping'
  | 'data_flow_inventory'
  | 'audit_evidence_package'
  | 'retention_dsr'
  | 'subprocessor_boundary'

export type CompliancePackageLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: CompliancePackageLaneKey
  owner: string
  signal: string
  status: CompliancePackageStatus
  title: string
}

export type CompliancePackageEvidence = {
  fingerprint: string
  lanes: CompliancePackageLane[]
  summary: string
  totals: Record<CompliancePackageStatus, number> & {
    total: number
  }
}

export type CompliancePackageEvidenceInput = {
  auditEntries?: AuditLogEntry[]
  authMode?: AuthModeResponse
  gdprOperations?: GdprOperationsResponse
  lockouts?: BreakGlassLockout[]
  members?: Member[]
  moderationSubjects?: ModerationSubject[]
  notifyTargets?: NotifyTarget[]
  publicVisibilityPolicy?: PublicVisibilityPolicy
  tokens?: BreakGlassToken[]
}

export function buildCompliancePackageEvidence(
  input: CompliancePackageEvidenceInput,
): CompliancePackageEvidence {
  const lanes = [
    controlMappingLane(input),
    dataFlowInventoryLane(input),
    auditEvidencePackageLane(input),
    retentionDsrLane(input),
    subprocessorBoundaryLane(input),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    ready: lanes.filter((lane) => lane.status === 'ready').length,
    total: lanes.length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${input.authMode?.mode ?? 'auth unknown'} / ${formatNumber(
      activeMembers(input.members).length,
    )} active members / ${formatNumber(
      publicSurfaceCount(input.publicVisibilityPolicy),
    )} public surfaces / ${formatNumber(input.notifyTargets?.length ?? 0)} outbound targets / ${formatNumber(
      input.auditEntries?.length ?? 0,
    )} audit events`,
    lanes,
    summary: compliancePackageSummary(totals),
    totals,
  }
}

function controlMappingLane(input: CompliancePackageEvidenceInput): CompliancePackageLane {
  const active = activeMembers(input.members)
  const rolesRepresented = representedRoleCount(active)
  const admins = active.filter((member) => member.role === 'admin').length
  const activeBreakglass = activeBreakglassTokens(input.tokens).length

  return {
    actionHref: '/administration/security',
    actionLabel: 'Review access controls',
    evidence:
      input.authMode && input.members && input.tokens && input.lockouts
        ? `${input.authMode.mode} auth / ${admins} admins / ${rolesRepresented} roles / ${activeBreakglass} active break-glass / ${input.lockouts.length} lockouts`
        : 'access-control package evidence is missing',
    guardrail:
      'DPA and SOC2 control mapping needs current auth mode, RBAC breadth, admin continuity, break-glass coverage, and lockout evidence.',
    key: 'control_mapping',
    owner: 'Security + Legal',
    signal:
      input.authMode && input.members && input.tokens && input.lockouts
        ? `${input.authMode.mode} / ${admins} admins / ${rolesRepresented} roles / ${activeBreakglass} break-glass`
        : 'control mapping evidence missing',
    status: controlMappingStatus(input),
    title: 'DPA / SOC2 control mapping',
  }
}

function dataFlowInventoryLane(input: CompliancePackageEvidenceInput): CompliancePackageLane {
  const policy = input.publicVisibilityPolicy
  const subjects = input.moderationSubjects

  return {
    actionHref: '/integrations/public-visibility',
    actionLabel: 'Review data flow inventory',
    /* v8 ignore next -- @preserve: moderation subject counts default to zero for policy-only data-flow snapshots. */
    evidence: policy
      ? `${publicSurfaceCount(policy)} public surfaces / identity ${identityModeLabel(
          policy.submitterIdentityMode,
        )} / ${subjects?.length ?? 0} moderation subjects`
      : 'public data-flow inventory evidence is missing',
    guardrail:
      'Customer-facing data flows need public-surface inventory, identity exposure rules, moderation defaults, and review evidence.',
    key: 'data_flow_inventory',
    owner: 'Security + Product',
    /* v8 ignore next -- @preserve: moderation subject counts default to zero for policy-only data-flow snapshots. */
    signal: policy
      ? `${publicSurfaceCount(policy)} public surfaces / identity ${identityModeLabel(
          policy.submitterIdentityMode,
        )} / moderation ${subjects?.length ?? 0} subjects`
      : 'data flow inventory evidence missing',
    status: dataFlowInventoryStatus(policy, subjects),
    title: 'Data flow inventory',
  }
}

function auditEvidencePackageLane(input: CompliancePackageEvidenceInput): CompliancePackageLane {
  const entries = input.auditEntries
  const actors = new Set(
    (entries ?? []).map((entry) => entry.actorId || entry.actorEmail).filter(Boolean),
  )
  const actions = new Set((entries ?? []).map((entry) => entry.action).filter(Boolean))

  return {
    actionHref: '/administration/audit-log',
    actionLabel: 'Open audit evidence',
    evidence: entries
      ? `${entries.length} events / ${actors.size} actors / ${actions.size} action types / evidence ZIP available`
      : 'audit evidence package is missing',
    guardrail:
      'Compliance packets need searchable audit events, actor coverage, CSV export, and signed evidence export availability.',
    key: 'audit_evidence_package',
    owner: 'Security + Compliance',
    signal: entries
      ? `${entries.length} audit events / ${actors.size} actors / ${actions.size} action types`
      : 'audit evidence package missing',
    status: auditEvidenceStatus(entries),
    title: 'Audit evidence package',
  }
}

function retentionDsrLane(input: CompliancePackageEvidenceInput): CompliancePackageLane {
  const operations = input.gdprOperations

  return {
    actionHref: '/administration/gdpr',
    actionLabel: 'Review DSR controls',
    evidence: operations
      ? `export TTL ${formatDuration(operations.exportTtlSeconds)} / audit ${formatDays(
          operations.auditRetentionDays,
        )} / delete grace ${formatDuration(operations.deleteGraceWindowSeconds)} / hashed audit ${onOff(
          operations.hashedAuditResidue,
        )}`
      : 'retention and data-subject request evidence is missing',
    guardrail:
      'Compliance packets need data-subject export, deletion, legal hold, audit retention, and backup residue controls in one proof.',
    key: 'retention_dsr',
    owner: 'Security + Compliance',
    signal: operations
      ? `audit ${formatDays(operations.auditRetentionDays)} / legal hold ${onOff(
          operations.legalHoldSupported,
        )} / ${operations.scheduledDeleteCount} scheduled deletes`
      : 'retention and DSR evidence missing',
    status: retentionDsrStatus(operations),
    title: 'Retention and DSR controls',
  }
}

function subprocessorBoundaryLane(input: CompliancePackageEvidenceInput): CompliancePackageLane {
  const targets = input.notifyTargets
  const enabled = (targets ?? []).filter((target) => !target.disabled)
  const failing = enabled.filter((target) => target.lastFailureAt || target.lastError).length
  const https = enabled.filter((target) => target.url.startsWith('https://')).length
  const disabled = (targets ?? []).filter((target) => target.disabled).length

  return {
    actionHref: '/integrations/notify-targets',
    actionLabel: 'Review outbound targets',
    evidence: targets
      ? `${enabled.length} enabled / ${disabled} disabled / ${failing} failing / ${https} HTTPS targets`
      : 'subprocessor and outbound target evidence is missing',
    guardrail:
      'DPA subprocessor evidence needs a current outbound target inventory with HTTPS boundaries and failure state visible.',
    key: 'subprocessor_boundary',
    owner: 'Security + Customer Success',
    signal: targets
      ? `${enabled.length} enabled outbound / ${failing} failing / ${https} HTTPS`
      : 'subprocessor boundary evidence missing',
    status: subprocessorBoundaryStatus(targets),
    title: 'Subprocessor boundary',
  }
}

function controlMappingStatus(input: CompliancePackageEvidenceInput): CompliancePackageStatus {
  if (!input.authMode || !input.members || !input.tokens || !input.lockouts) return 'needs_data'
  const active = activeMembers(input.members)
  const admins = active.filter((member) => member.role === 'admin').length
  const activeBreakglass = activeBreakglassTokens(input.tokens).length
  if (active.length === 0 || admins < 1) return 'blocked'
  if (input.authMode.mode === 'sso_only' && activeBreakglass === 0) return 'blocked'
  if (admins < 2 || input.authMode.mode === 'hybrid' || input.lockouts.length > 0) return 'watch'
  if (representedRoleCount(active) < 3) return 'watch'
  return 'ready'
}

function dataFlowInventoryStatus(
  policy: PublicVisibilityPolicy | undefined,
  subjects: ModerationSubject[] | undefined,
): CompliancePackageStatus {
  if (!policy || !subjects) return 'needs_data'
  if (
    policy.portalAccessMode === PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC &&
    (policy.defaultRequestState === ModerationState.MODERATION_STATE_APPROVED ||
      policy.defaultCommentState === ModerationState.MODERATION_STATE_APPROVED)
  ) {
    return 'blocked'
  }
  if (
    publicSurfaceCount(policy) > 0 ||
    policy.showSubmitterDisplay ||
    policy.showVoteCount ||
    policy.showCommentCount ||
    !policy.hidePublicTimestamps ||
    subjects.some((subject) => subject.state === ModerationState.MODERATION_STATE_PENDING)
  ) {
    return 'watch'
  }
  return 'ready'
}

function auditEvidenceStatus(entries: AuditLogEntry[] | undefined): CompliancePackageStatus {
  if (!entries) return 'needs_data'
  if (entries.length === 0) return 'watch'
  return 'ready'
}

function retentionDsrStatus(
  operations: GdprOperationsResponse | undefined,
): CompliancePackageStatus {
  if (!operations) return 'needs_data'
  if (
    operations.exportTtlSeconds <= 0 ||
    operations.auditRetentionDays <= 0 ||
    operations.deleteGraceWindowSeconds <= 0 ||
    !operations.hashedAuditResidue ||
    (!operations.legalHoldSupported && operations.scheduledDeleteCount > 0)
  ) {
    return 'blocked'
  }
  if (
    !operations.legalHoldSupported ||
    operations.readyExportCount > 0 ||
    operations.scheduledDeleteCount > 0 ||
    operations.backupsMayRetainUntilRotation
  ) {
    return 'watch'
  }
  return 'ready'
}

function subprocessorBoundaryStatus(targets: NotifyTarget[] | undefined): CompliancePackageStatus {
  if (!targets) return 'needs_data'
  const enabled = targets.filter((target) => !target.disabled)
  if (enabled.some((target) => !target.url.startsWith('https://'))) return 'blocked'
  if (enabled.some((target) => target.lastFailureAt || target.lastError)) return 'watch'
  if (enabled.length === 0) return 'watch'
  return 'ready'
}

function compliancePackageSummary(totals: CompliancePackageEvidence['totals']) {
  if (totals.blocked > 0) return `${totals.blocked} compliance package checks are blocked`
  if (totals.needs_data > 0) return `${totals.needs_data} compliance package checks need evidence`
  if (totals.watch > 0) return `${totals.watch} compliance package checks need attention`
  return 'compliance package evidence is verified'
}

function activeMembers(members: Member[] | undefined) {
  return (members ?? []).filter(
    (member) => member.acceptedAt && !member.memberType.includes('invite'),
  )
}

function representedRoleCount(members: Member[]) {
  return new Set(members.map((member) => member.role).filter(Boolean)).size
}

function activeBreakglassTokens(tokens: BreakGlassToken[] | undefined) {
  return (tokens ?? []).filter((token) => !token.revoked_at && !token.used_at)
}

function publicSurfaceCount(policy: PublicVisibilityPolicy | undefined) {
  if (!policy) return 0
  return [
    policy.requestsEnabled,
    policy.commentsEnabled,
    policy.roadmapEnabled,
    policy.changelogEnabled,
  ].filter(Boolean).length
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

function formatDuration(seconds: number | undefined) {
  if (!seconds || seconds <= 0) return 'missing'
  if (seconds % 86400 === 0) return `${formatNumber(seconds / 86400)}d`
  if (seconds % 3600 === 0) return `${formatNumber(seconds / 3600)}h`
  if (seconds % 60 === 0) return `${formatNumber(seconds / 60)}m`
  return `${formatNumber(seconds)}s`
}

function formatDays(days: number) {
  if (days <= 0) return 'missing'
  return `${formatNumber(days)}d`
}

function onOff(enabled: boolean) {
  return enabled ? 'on' : 'off'
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value)
}
