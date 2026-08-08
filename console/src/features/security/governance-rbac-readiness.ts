import type { AuthModeResponse } from '@/features/security/api/auth-mode'
import type { BreakGlassLockout, BreakGlassToken } from '@/features/security/api/breakglass'
import type { AuditLogEntry } from '@/proto/attune/v1/audit'
import type { Member } from '@/proto/attune/v1/member'

export type GovernanceRbacStatus = 'ready' | 'watch' | 'blocked' | 'needs_data'

export type GovernanceRbacLaneKey =
  | 'sso_breakglass'
  | 'scim_idp'
  | 'rbac_roles'
  | 'last_admin_guard'
  | 'access_review'

export type GovernanceRbacLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: GovernanceRbacLaneKey
  owner: string
  signal: string
  status: GovernanceRbacStatus
  title: string
}

export type GovernanceRbacReadiness = {
  fingerprint: string
  lanes: GovernanceRbacLane[]
  summary: string
  totals: Record<GovernanceRbacStatus, number> & {
    total: number
  }
}

export type GovernanceRbacReadinessInput = {
  auditEntries?: AuditLogEntry[]
  authMode?: AuthModeResponse
  lockouts?: BreakGlassLockout[]
  members?: Member[]
  tokens?: BreakGlassToken[]
}

const memberAuditActions = new Set(['member.invite', 'member.remove', 'member.update_role'])

export function buildGovernanceRbacReadiness(
  input: GovernanceRbacReadinessInput,
): GovernanceRbacReadiness {
  const lanes = [
    ssoBreakglassLane(input),
    scimIdpLane(input),
    rbacRolesLane(input),
    lastAdminGuardLane(input),
    accessReviewLane(input),
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
    )} active members / ${formatNumber(activeAdminCount(input.members))} admins / ${formatNumber(
      memberAuditEntries(input.auditEntries).length,
    )} member audit events`,
    lanes,
    summary: governanceSummary(totals),
    totals,
  }
}

function ssoBreakglassLane(input: GovernanceRbacReadinessInput): GovernanceRbacLane {
  const activeTokens = activeBreakglassTokens(input.tokens).length
  const expiringTokens = expiringBreakglassTokens(input.tokens).length
  const lockoutCount = input.lockouts?.length

  return {
    actionHref: '/administration/security',
    actionLabel: 'Open security settings',
    evidence:
      input.authMode && input.tokens && input.lockouts
        ? `${input.authMode.mode} mode / ${activeTokens} active tokens / ${expiringTokens} expiring / ${lockoutCount} lockouts`
        : 'auth mode, break-glass token, or lockout evidence is missing',
    guardrail:
      'SSO-only deployments need a tested break-glass path, active emergency access inventory, and no unresolved lockouts.',
    key: 'sso_breakglass',
    owner: 'Security',
    signal:
      input.authMode && input.tokens && input.lockouts
        ? `${input.authMode.mode} / ${activeTokens} active break-glass / ${lockoutCount} lockouts`
        : 'SSO and break-glass evidence missing',
    status: ssoBreakglassStatus(input.authMode, input.tokens, input.lockouts),
    title: 'SSO / break-glass readiness',
  }
}

function scimIdpLane(input: GovernanceRbacReadinessInput): GovernanceRbacLane {
  const members = input.members
  const active = activeMembers(members)
  const idpManaged = active.filter((member) => member.roleSource === 'idp').length
  const manual = active.filter((member) => member.roleSource === 'manual').length
  const bootstrap = active.filter((member) => member.roleSource === 'bootstrap').length
  const pending = pendingInvites(members).length

  return {
    actionHref: '/administration/members',
    actionLabel: 'Open member directory',
    evidence: members
      ? `${idpManaged} IdP-managed / ${manual} manual / ${bootstrap} bootstrap / ${pending} pending invites`
      : 'member source evidence is missing',
    guardrail:
      'SCIM or IdP-managed memberships should be visible separately from manual and bootstrap role assignments.',
    key: 'scim_idp',
    owner: 'Security + IT',
    signal: members
      ? `${idpManaged} IdP-managed / ${formatNumber(active.length)} active members / ${pending} pending invites`
      : 'SCIM and IdP evidence missing',
    status: scimIdpStatus(members),
    title: 'SCIM / IdP coverage',
  }
}

function rbacRolesLane(input: GovernanceRbacReadinessInput): GovernanceRbacLane {
  const members = input.members
  const active = activeMembers(members)
  const roleCounts = countRoles(active)
  const roleBreadth = Object.values(roleCounts).filter((count) => count > 0).length

  return {
    actionHref: '/administration/members',
    actionLabel: 'Review roles',
    evidence: members
      ? `${roleCounts.admin} admin / ${roleCounts.delegated_admin} delegated admin / ${roleCounts.member} member / ${roleCounts.viewer} viewer`
      : 'role inventory evidence is missing',
    guardrail:
      'Fine-grained RBAC needs visible role breadth, least-privilege member distribution, and privileged role separation.',
    key: 'rbac_roles',
    owner: 'Security + Product Ops',
    signal: members
      ? `${roleBreadth} roles represented / ${formatNumber(active.length)} active members`
      : 'role inventory evidence missing',
    status: rbacRolesStatus(members),
    title: 'Fine-grained RBAC coverage',
  }
}

function lastAdminGuardLane(input: GovernanceRbacReadinessInput): GovernanceRbacLane {
  const admins = activeAdminCount(input.members)

  return {
    actionHref: '/administration/members',
    actionLabel: 'Review admin continuity',
    evidence: input.members
      ? `${admins} active admins / ${pendingInvites(input.members).length} pending invites`
      : 'admin continuity evidence is missing',
    guardrail:
      'At least two active admins must remain available before role changes, removals, or SSO-only cutover can be trusted.',
    key: 'last_admin_guard',
    owner: 'Security',
    signal: input.members ? `${admins} active admins` : 'last-admin guard evidence missing',
    status: lastAdminGuardStatus(input.members),
    title: 'Last-admin guard',
  }
}

function accessReviewLane(input: GovernanceRbacReadinessInput): GovernanceRbacLane {
  const entries = memberAuditEntries(input.auditEntries)
  /* v8 ignore next -- @preserve: audit rows normally include actor id; actor email fallback guards imported evidence. */
  const actors = new Set(entries.map((entry) => entry.actorId || entry.actorEmail).filter(Boolean))

  return {
    actionHref: '/administration/audit-log?targetType=member',
    actionLabel: 'Open member audit log',
    evidence: input.auditEntries
      ? `${entries.length} member events / ${actors.size} actors / CSV export available`
      : 'member audit log evidence is missing',
    guardrail:
      'Access reviews need recent member invite, removal, and role-change events plus an exportable audit trail.',
    key: 'access_review',
    owner: 'Security + Compliance',
    signal: input.auditEntries
      ? `${entries.length} member audit events / ${actors.size} actors`
      : 'access review evidence missing',
    status: accessReviewStatus(input.auditEntries),
    title: 'Access review audit trail',
  }
}

function ssoBreakglassStatus(
  authMode: AuthModeResponse | undefined,
  tokens: BreakGlassToken[] | undefined,
  lockouts: BreakGlassLockout[] | undefined,
): GovernanceRbacStatus {
  if (!authMode || !tokens || !lockouts) return 'needs_data'
  const activeTokens =
    activeBreakglassTokens(tokens).length + expiringBreakglassTokens(tokens).length
  if (authMode.mode === 'sso_only' && activeTokens === 0) return 'blocked'
  if (lockouts.length > 0 || authMode.mode === 'hybrid' || activeTokens === 0) return 'watch'
  return 'ready'
}

function scimIdpStatus(members: Member[] | undefined): GovernanceRbacStatus {
  if (!members) return 'needs_data'
  const active = activeMembers(members)
  if (active.length === 0) return 'blocked'
  const idpManaged = active.filter((member) => member.roleSource === 'idp').length
  const pending = pendingInvites(members).length
  if (idpManaged === 0 || pending > 0) return 'watch'
  return 'ready'
}

function rbacRolesStatus(members: Member[] | undefined): GovernanceRbacStatus {
  if (!members) return 'needs_data'
  const active = activeMembers(members)
  if (active.length === 0) return 'blocked'
  const rolesRepresented = Object.values(countRoles(active)).filter((count) => count > 0).length
  return rolesRepresented >= 3 ? 'ready' : 'watch'
}

function lastAdminGuardStatus(members: Member[] | undefined): GovernanceRbacStatus {
  if (!members) return 'needs_data'
  return activeAdminCount(members) >= 2 ? 'ready' : 'blocked'
}

function accessReviewStatus(entries: AuditLogEntry[] | undefined): GovernanceRbacStatus {
  if (!entries) return 'needs_data'
  const memberEvents = memberAuditEntries(entries)
  if (memberEvents.length === 0) return 'watch'
  const actions = new Set(memberEvents.map((entry) => entry.action))
  return actions.has('member.update_role') || actions.has('member.remove') ? 'ready' : 'watch'
}

function governanceSummary(totals: GovernanceRbacReadiness['totals']) {
  if (totals.blocked > 0) return `${totals.blocked} governance readiness checks are blocked`
  if (totals.needs_data > 0) return `${totals.needs_data} governance checks need evidence`
  if (totals.watch > 0) return `${totals.watch} governance checks need attention`
  return 'governance readiness evidence is verified'
}

function activeMembers(members: Member[] | undefined) {
  return members?.filter((member) => member.memberType !== 'invite') ?? []
}

function pendingInvites(members: Member[] | undefined) {
  return members?.filter((member) => member.memberType === 'invite') ?? []
}

function activeAdminCount(members: Member[] | undefined) {
  return activeMembers(members).filter((member) => member.role === 'admin').length
}

function countRoles(members: Member[]) {
  return members.reduce(
    (counts, member) => {
      const role = member.role in counts ? (member.role as keyof typeof counts) : 'member'
      counts[role] += 1
      return counts
    },
    {
      admin: 0,
      delegated_admin: 0,
      member: 0,
      viewer: 0,
    },
  )
}

function memberAuditEntries(entries: AuditLogEntry[] | undefined) {
  return (
    entries?.filter(
      (entry) => entry.targetType === 'member' || memberAuditActions.has(entry.action),
    ) ?? []
  )
}

function activeBreakglassTokens(tokens: BreakGlassToken[] | undefined) {
  return tokens?.filter((token) => tokenUsableStatus(token) === 'active') ?? []
}

function expiringBreakglassTokens(tokens: BreakGlassToken[] | undefined) {
  return tokens?.filter((token) => tokenUsableStatus(token) === 'expiring') ?? []
}

function tokenUsableStatus(token: BreakGlassToken): 'active' | 'expiring' | 'inactive' {
  if (token.revoked_at || token.used_at) return 'inactive'
  const expiresAt = new Date(token.expires_at).getTime()
  if (!Number.isFinite(expiresAt) || expiresAt <= Date.now()) return 'inactive'
  const hoursUntilExpiry = (expiresAt - Date.now()) / (1000 * 60 * 60)
  return hoursUntilExpiry <= 1 ? 'expiring' : 'active'
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value)
}
