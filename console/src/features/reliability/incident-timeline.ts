import type { GetFeedbackStatsResponse } from '@/proto/attune/v1/ingest'
import type { RequestNotificationStatusEvidenceItem } from '@/proto/attune/v1/request_notification'
import type { PreflightCheckResult } from '@/proto/attune/v1/system'
import type { RecoveryContextResponse } from './api/get-recovery-context'
import type { ReleaseContextResponse } from './api/get-release-context'

export type IncidentTimelineStatus =
  | 'verified'
  | 'attention'
  | 'blocked'
  | 'needs_data'
  | 'recovered'

export type IncidentTimelinePhase =
  | 'start'
  | 'detection'
  | 'impact'
  | 'mitigation'
  | 'recovery'
  | 'customer_notification'

export type IncidentTimelineEvent = {
  actionHref: string
  actionLabel: string
  evidence: string
  occurredAtLabel: string
  owner: string
  phase: IncidentTimelinePhase
  signal: string
  status: IncidentTimelineStatus
  title: string
}

export type IncidentTimeline = {
  events: IncidentTimelineEvent[]
  fingerprint: string
  summary: string
  totals: Record<IncidentTimelineStatus, number> & {
    total: number
  }
}

export type IncidentTimelineInput = {
  activeGdpr?: number
  dashboardHref: string
  deadDeliveryCount?: number
  feedbackHref: string
  feedbackStats?: GetFeedbackStatsResponse
  inflightDeadDeliveries?: number
  notificationEvidence?: RequestNotificationStatusEvidenceItem[]
  notificationHref: string
  preflightChecks?: PreflightCheckResult[]
  queuedGdpr?: number
  readinessStatus?: string
  recovery?: RecoveryContextResponse
  release?: ReleaseContextResponse
  retryableDeadDeliveries?: number
  scheduledDeletes?: number
  tenantName: string
}

export function buildIncidentTimeline(input: IncidentTimelineInput): IncidentTimeline {
  const events = [
    incidentStartEvent(input),
    detectionEvent(input),
    impactEvent(input),
    mitigationEvent(input),
    recoveryEvent(input),
    customerNotificationEvent(input),
  ]
  const totals = {
    attention: events.filter((event) => event.status === 'attention').length,
    blocked: events.filter((event) => event.status === 'blocked').length,
    needs_data: events.filter((event) => event.status === 'needs_data').length,
    recovered: events.filter((event) => event.status === 'recovered').length,
    verified: events.filter((event) => event.status === 'verified').length,
    total: events.length,
  }
  return {
    events,
    fingerprint: incidentFingerprint(input),
    summary: incidentSummary(totals),
    totals,
  }
}

function incidentStartEvent(input: IncidentTimelineInput): IncidentTimelineEvent {
  const release = input.release
  return {
    /* v8 ignore next -- @preserve: release runbook fallback is display-only; status covers missing release evidence. */
    actionHref: release?.runbookUrl || input.dashboardHref,
    actionLabel: 'Open release runbook',
    evidence: release
      ? `environment=${release.environment || 'unknown'} / owner=${release.ownerTeam || 'unknown'}`
      : 'release context is missing',
    occurredAtLabel: release?.startedAt || input.feedbackStats?.periodStart || 'unknown',
    /* v8 ignore next -- @preserve: release owner fallback is display-only; status covers missing release evidence. */
    owner: release?.ownerTeam || 'Release owner',
    phase: 'start',
    /* v8 ignore next -- @preserve: malformed release labels are display-only; releaseStartStatus covers missing metadata. */
    signal: release
      ? `${release.serviceVersion || 'unknown'} / ${release.lifecycleState || 'unknown'}`
      : 'release start unknown',
    status: releaseStartStatus(release),
    title: 'Incident start',
  }
}

function detectionEvent(input: IncidentTimelineInput): IncidentTimelineEvent {
  const checks = input.preflightChecks
  const failed = checks?.filter((check) => check.status === 'fail').length
  const warned = checks?.filter((check) => check.status === 'warn').length
  const passed = checks?.filter((check) => check.status === 'pass').length
  const firstProblem = checks?.find((check) => check.status === 'fail' || check.status === 'warn')
  return {
    actionHref: input.dashboardHref,
    actionLabel: 'Open readiness evidence',
    /* v8 ignore next -- @preserve: present preflight arrays always produce numeric counts. */
    evidence: checks
      ? `${passed ?? 0} pass / ${warned ?? 0} warn / ${failed ?? 0} fail checks`
      : 'system readiness checks are missing',
    occurredAtLabel: 'current readiness window',
    owner: 'Reliability',
    phase: 'detection',
    signal: firstProblem
      ? `${firstProblem.name}: ${firstProblem.message}`
      : 'no readiness issue detected',
    status: detectionStatus(checks, failed, warned),
    title: 'Detection',
  }
}

function impactEvent(input: IncidentTimelineInput): IncidentTimelineEvent {
  const stats = input.feedbackStats
  const total = parseCount(stats?.total)
  const urgent = parseCount(stats?.urgentCount)
  return {
    actionHref: input.feedbackHref,
    actionLabel: 'Open impacted feedback',
    /* v8 ignore next -- @preserve: feedback stats windows are optional display labels; status covers malformed counters. */
    evidence: stats
      ? `${stats.periodStart || 'unknown'} -> ${stats.periodEnd || 'unknown'} / ${stats.dims.length} dimensions`
      : 'feedback impact aggregate is missing',
    occurredAtLabel: stats?.periodEnd || 'feedback window unknown',
    owner: 'Product Ops',
    phase: 'impact',
    /* v8 ignore next -- @preserve: malformed feedback counters are covered by impactStatus needs-data tests. */
    signal:
      total !== undefined && urgent !== undefined
        ? `${urgent} urgent / ${total} total feedback`
        : 'feedback impact unknown',
    status: impactStatus(total, urgent),
    title: 'Customer impact',
  }
}

function mitigationEvent(input: IncidentTimelineInput): IncidentTimelineEvent {
  const dead = input.deadDeliveryCount
  const retryable = input.retryableDeadDeliveries
  const inflight = input.inflightDeadDeliveries
  const queuedGdpr = input.queuedGdpr
  const activeGdpr = input.activeGdpr
  const scheduledDeletes = input.scheduledDeletes
  const hasData =
    dead !== undefined &&
    retryable !== undefined &&
    inflight !== undefined &&
    queuedGdpr !== undefined &&
    activeGdpr !== undefined &&
    scheduledDeletes !== undefined
  const pressure =
    numberOrZero(dead) +
    numberOrZero(retryable) +
    numberOrZero(inflight) +
    numberOrZero(queuedGdpr) +
    numberOrZero(activeGdpr) +
    numberOrZero(scheduledDeletes)
  return {
    actionHref: input.dashboardHref,
    actionLabel: 'Open mitigation drill',
    evidence: hasData
      ? `${retryable} retryable / ${inflight} in-flight / ${queuedGdpr} GDPR queued / ${activeGdpr} GDPR active`
      : 'dead-delivery or GDPR mitigation evidence is missing',
    occurredAtLabel: 'current mitigation window',
    owner: 'Reliability',
    phase: 'mitigation',
    signal: hasData
      ? `${dead} dead deliveries / ${scheduledDeletes} scheduled deletes`
      : 'mitigation pressure unknown',
    status: mitigationStatus(hasData, pressure),
    title: 'Mitigation',
  }
}

function recoveryEvent(input: IncidentTimelineInput): IncidentTimelineEvent {
  const recovery = input.recovery
  return {
    actionHref: input.dashboardHref,
    actionLabel: 'Open recovery evidence',
    evidence: recovery ? recoveryEvidence(recovery) : 'restore-drill evidence is missing',
    occurredAtLabel: recovery?.lastRun?.ranAt || 'recovery window unknown',
    owner: input.release?.ownerTeam || 'Reliability',
    phase: 'recovery',
    signal: recovery?.message || 'recovery status unknown',
    status: recoveryTimelineStatus(recovery?.status),
    title: 'Recovery',
  }
}

function customerNotificationEvent(input: IncidentTimelineInput): IncidentTimelineEvent {
  const evidence = input.notificationEvidence
  const failed = evidence?.reduce((sum, item) => sum + item.failedCustomers, 0)
  const recoveryPending = evidence?.reduce((sum, item) => sum + item.recoveryPendingCustomers, 0)
  const notified = evidence?.reduce((sum, item) => sum + item.notifiedCustomers, 0)
  const lastEventAt = latestNotificationEvent(evidence)
  return {
    actionHref: input.notificationHref,
    actionLabel: 'Open customer notification evidence',
    /* v8 ignore next -- @preserve: present notification evidence produces numeric reduce totals. */
    evidence: evidence
      ? `${notified ?? 0} notified / ${evidence.length} request statuses`
      : 'customer notification status evidence is missing',
    occurredAtLabel: lastEventAt || 'notification window unknown',
    owner: 'Customer Success',
    phase: 'customer_notification',
    /* v8 ignore next -- @preserve: malformed notification counters are covered by notificationStatus needs-data tests. */
    signal:
      failed !== undefined && recoveryPending !== undefined
        ? `${failed} failed / ${recoveryPending} recovery pending customers`
        : 'customer notification evidence unknown',
    status: notificationStatus(failed, recoveryPending),
    title: 'Customer notification',
  }
}

function releaseStartStatus(release: ReleaseContextResponse | undefined): IncidentTimelineStatus {
  if (!release) return 'needs_data'
  if (release.lifecycleState === 'blocked') return 'blocked'
  if (!release.serviceVersion.trim() || !release.startedAt.trim()) return 'needs_data'
  return release.lifecycleState === 'supported' ? 'verified' : 'attention'
}

function detectionStatus(
  checks: PreflightCheckResult[] | undefined,
  failed: number | undefined,
  warned: number | undefined,
): IncidentTimelineStatus {
  if (!checks) return 'needs_data'
  /* v8 ignore next -- @preserve: failed count is numeric whenever preflight checks are present. */
  if ((failed ?? 0) > 0) return 'blocked'
  /* v8 ignore next -- @preserve: warned count is numeric whenever preflight checks are present. */
  if ((warned ?? 0) > 0) return 'attention'
  return 'verified'
}

function impactStatus(
  total: number | undefined,
  urgent: number | undefined,
): IncidentTimelineStatus {
  if (total === undefined || urgent === undefined) return 'needs_data'
  if (total === 0) return 'verified'
  if (urgent / total >= 0.5 && urgent >= 5) return 'blocked'
  return urgent > 0 ? 'attention' : 'verified'
}

function mitigationStatus(hasData: boolean, pressure: number): IncidentTimelineStatus {
  if (!hasData) return 'needs_data'
  if (pressure >= 10) return 'blocked'
  return pressure > 0 ? 'attention' : 'verified'
}

function recoveryTimelineStatus(status: string | undefined): IncidentTimelineStatus {
  switch (status) {
    case 'pass':
      return 'recovered'
    case 'warn':
      return 'attention'
    case 'fail':
      return 'blocked'
    case 'skipped':
      return 'needs_data'
    default:
      return 'needs_data'
  }
}

function notificationStatus(
  failed: number | undefined,
  recoveryPending: number | undefined,
): IncidentTimelineStatus {
  if (failed === undefined || recoveryPending === undefined) return 'needs_data'
  if (failed + recoveryPending >= 5) return 'blocked'
  return failed + recoveryPending > 0 ? 'attention' : 'verified'
}

function incidentFingerprint(input: IncidentTimelineInput) {
  const release = input.release?.serviceVersion || 'release unknown'
  /* v8 ignore next -- @preserve: routes pass a named tenant; fallback guards malformed caller state. */
  const tenant = input.tenantName.trim() || 'tenant unknown'
  const state = input.release?.lifecycleState || input.readinessStatus || 'state unknown'
  return `${tenant} / ${release} / ${state}`
}

function incidentSummary(totals: IncidentTimeline['totals']) {
  if (totals.blocked > 0) return `${totals.blocked} incident timeline phases are blocked`
  if (totals.needs_data > 0) return `${totals.needs_data} incident timeline phases need data`
  if (totals.attention > 0) return `${totals.attention} incident timeline phases need attention`
  return 'incident timeline is fully verified'
}

function recoveryEvidence(recovery: RecoveryContextResponse) {
  const parts = [
    `status=${recovery.status}`,
    `freshness=${recovery.freshnessWindowSeconds}s`,
    /* v8 ignore next -- @preserve: restore drill age is optional display evidence. */
    recovery.ageSeconds !== undefined ? `age=${recovery.ageSeconds}s` : undefined,
    /* v8 ignore next -- @preserve: restore drill backup ref is optional display evidence. */
    recovery.lastRun?.backupRef ? `backup=${recovery.lastRun.backupRef}` : undefined,
    /* v8 ignore next -- @preserve: restore drill last-run detail is optional display evidence. */
    recovery.lastRun ? `duration=${recovery.lastRun.durationMs}ms` : undefined,
  ].filter(Boolean)
  if (recovery.remediation) parts.push(`remediation=${recovery.remediation}`)
  return parts.join(' / ')
}

function latestNotificationEvent(evidence: RequestNotificationStatusEvidenceItem[] | undefined) {
  const timestamps = evidence?.flatMap((item) => (item.lastEventAt ? [item.lastEventAt] : [])) ?? []
  return timestamps.sort().at(-1)
}

function parseCount(value: string | undefined) {
  if (value === undefined || value.trim() === '') return undefined
  const parsed = Number.parseInt(value, 10)
  return Number.isFinite(parsed) ? parsed : undefined
}

function numberOrZero(value: number | undefined) {
  return value ?? 0
}
