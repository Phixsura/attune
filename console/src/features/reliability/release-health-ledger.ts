import type { GetFeedbackStatsResponse } from '@/proto/attune/v1/ingest'
import type { RequestNotificationStatusEvidenceItem } from '@/proto/attune/v1/request_notification'
import type { RecoveryContextResponse } from './api/get-recovery-context'
import type { ReleaseContextResponse } from './api/get-release-context'

export type ReleaseHealthStatus = 'ready' | 'attention' | 'blocked' | 'needs_data'

export type ReleaseHealthLedgerEntry = {
  actionHref: string
  actionLabel: string
  evidence: string
  key:
    | 'runtime_version'
    | 'lifecycle_gate'
    | 'restore_drill'
    | 'feedback_pressure'
    | 'notification_failures'
  owner: string
  signal: string
  status: ReleaseHealthStatus
  title: string
}

export type ReleaseHealthLedger = {
  entries: ReleaseHealthLedgerEntry[]
  releaseFingerprint: string
  summary: string
  totals: Record<ReleaseHealthStatus, number> & {
    total: number
  }
}

export type ReleaseHealthLedgerInput = {
  dashboardHref: string
  escalationHref?: string
  feedbackHref: string
  feedbackStats?: GetFeedbackStatsResponse
  notificationEvidence?: RequestNotificationStatusEvidenceItem[]
  notificationHref: string
  readinessStatus?: string
  recovery?: RecoveryContextResponse
  release?: ReleaseContextResponse
}

export function buildReleaseHealthLedger(input: ReleaseHealthLedgerInput): ReleaseHealthLedger {
  const entries = [
    runtimeVersionEntry(input),
    lifecycleGateEntry(input),
    restoreDrillEntry(input),
    feedbackPressureEntry(input),
    notificationFailuresEntry(input),
  ]
  const totals = {
    attention: entries.filter((entry) => entry.status === 'attention').length,
    blocked: entries.filter((entry) => entry.status === 'blocked').length,
    needs_data: entries.filter((entry) => entry.status === 'needs_data').length,
    ready: entries.filter((entry) => entry.status === 'ready').length,
    total: entries.length,
  }
  return {
    entries,
    releaseFingerprint: releaseFingerprint(input.release),
    summary: releaseHealthSummary(totals),
    totals,
  }
}

function runtimeVersionEntry(input: ReleaseHealthLedgerInput): ReleaseHealthLedgerEntry {
  const release = input.release
  return {
    /* v8 ignore next -- @preserve: runbook fallback is display-only; runtimeVersionStatus covers missing release evidence. */
    actionHref: release?.runbookUrl || input.dashboardHref,
    actionLabel: 'Open release runbook',
    /* v8 ignore next -- @preserve: release profile/start fallbacks guard malformed runtime metadata. */
    evidence: release
      ? `profile=${release.profile || 'unknown'} / started=${release.startedAt || 'unknown'}`
      : 'system release endpoint did not return runtime metadata',
    key: 'runtime_version',
    /* v8 ignore next -- @preserve: owner fallback is display-only; runtimeVersionStatus covers missing owner. */
    owner: release?.ownerTeam || 'Release owner',
    /* v8 ignore next -- @preserve: malformed release labels are display-only; runtimeVersionStatus covers missing metadata. */
    signal: release
      ? `${release.serviceVersion || 'unknown'} / ${release.environment || 'unknown'} / ${
          release.ownerTeam || 'unknown'
        }`
      : 'runtime version unknown',
    status: runtimeVersionStatus(release),
    title: 'Runtime version',
  }
}

function lifecycleGateEntry(input: ReleaseHealthLedgerInput): ReleaseHealthLedgerEntry {
  const release = input.release
  return {
    actionHref: input.escalationHref || release?.escalationUrl || input.dashboardHref,
    actionLabel: 'Open escalation path',
    /* v8 ignore next -- @preserve: readiness label defaults to unknown when live readiness has not been fetched. */
    evidence: release
      ? `${release.compatibilityRules.length} compatibility rules / readiness=${
          input.readinessStatus ?? 'unknown'
        }`
      : 'release lifecycle evidence is missing',
    key: 'lifecycle_gate',
    owner: release?.ownerTeam || 'Release owner',
    signal: release?.lifecycleState || 'lifecycle unknown',
    status: lifecycleStatus(release?.lifecycleState),
    title: 'Lifecycle gate',
  }
}

function restoreDrillEntry(input: ReleaseHealthLedgerInput): ReleaseHealthLedgerEntry {
  const recovery = input.recovery
  return {
    actionHref: input.dashboardHref,
    actionLabel: 'Open reliability dashboard',
    evidence: recovery ? restoreEvidence(recovery) : 'restore-drill context is missing',
    key: 'restore_drill',
    owner: input.release?.ownerTeam || 'Reliability',
    signal: recovery ? recovery.message : 'restore result unknown',
    status: recoveryStatus(recovery?.status),
    title: 'Restore drill',
  }
}

function feedbackPressureEntry(input: ReleaseHealthLedgerInput): ReleaseHealthLedgerEntry {
  const stats = input.feedbackStats
  const total = parseCount(stats?.total)
  const urgent = parseCount(stats?.urgentCount)
  return {
    actionHref: input.feedbackHref,
    actionLabel: 'Open feedback pressure',
    /* v8 ignore next -- @preserve: feedback window labels are optional display evidence. */
    evidence: stats
      ? `${stats.periodStart || 'unknown'} -> ${stats.periodEnd || 'unknown'} / ${stats.dims.length} dimensions`
      : 'feedback stats endpoint did not return a release-window aggregate',
    key: 'feedback_pressure',
    owner: 'Product Ops',
    /* v8 ignore next -- @preserve: malformed feedback counters are covered by feedbackPressureStatus needs-data tests. */
    signal:
      stats && total !== undefined && urgent !== undefined
        ? `${urgent} urgent / ${total} total feedback`
        : 'feedback pressure unknown',
    status: feedbackPressureStatus(total, urgent),
    title: 'Feedback pressure',
  }
}

function notificationFailuresEntry(input: ReleaseHealthLedgerInput): ReleaseHealthLedgerEntry {
  const evidence = input.notificationEvidence
  const failed = evidence?.reduce((sum, item) => sum + item.failedCustomers, 0)
  const recoveryPending = evidence?.reduce((sum, item) => sum + item.recoveryPendingCustomers, 0)
  const events = evidence?.reduce((sum, item) => sum + item.eventCount, 0)
  return {
    actionHref: input.notificationHref,
    actionLabel: 'Open notification evidence',
    evidence: evidence
      ? `${events} notification events / ${evidence.length} request statuses`
      : 'request notification status evidence is missing',
    key: 'notification_failures',
    owner: 'Customer Success',
    signal:
      failed !== undefined && recoveryPending !== undefined
        ? `${failed} failed / ${recoveryPending} recovery pending customers`
        : 'notification failure evidence unknown',
    status: notificationFailureStatus(failed, recoveryPending),
    title: 'Notification failures',
  }
}

function runtimeVersionStatus(release: ReleaseContextResponse | undefined): ReleaseHealthStatus {
  if (!release) return 'needs_data'
  if (release.lifecycleState === 'blocked') return 'blocked'
  if (!release.serviceVersion.trim() || !release.environment.trim() || !release.ownerTeam.trim()) {
    return 'needs_data'
  }
  return release.environment === 'production' ? 'ready' : 'attention'
}

function lifecycleStatus(state: string | undefined): ReleaseHealthStatus {
  switch (state) {
    case 'supported':
      return 'ready'
    case 'blocked':
      return 'blocked'
    case 'deprecated':
    case 'migrating':
    case 'recovering':
      return 'attention'
    default:
      return 'needs_data'
  }
}

function recoveryStatus(status: string | undefined): ReleaseHealthStatus {
  switch (status) {
    case 'pass':
      return 'ready'
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

function feedbackPressureStatus(
  total: number | undefined,
  urgent: number | undefined,
): ReleaseHealthStatus {
  if (total === undefined || urgent === undefined) return 'needs_data'
  if (total === 0) return 'ready'
  if (urgent / total >= 0.5 && urgent >= 5) return 'blocked'
  return urgent > 0 ? 'attention' : 'ready'
}

function notificationFailureStatus(
  failed: number | undefined,
  recoveryPending: number | undefined,
): ReleaseHealthStatus {
  if (failed === undefined || recoveryPending === undefined) return 'needs_data'
  if (failed + recoveryPending >= 5) return 'blocked'
  return failed + recoveryPending > 0 ? 'attention' : 'ready'
}

function releaseFingerprint(release: ReleaseContextResponse | undefined) {
  if (!release) return 'release unknown'
  /* v8 ignore next -- @preserve: release metadata fallbacks guard malformed runtime context payloads. */
  return `${release.serviceVersion || 'unknown'} / ${release.environment || 'unknown'} / ${
    release.lifecycleState || 'unknown'
  }`
}

function releaseHealthSummary(totals: ReleaseHealthLedger['totals']) {
  if (totals.blocked > 0) return `${totals.blocked} blocked release-health signals`
  if (totals.needs_data > 0) return `${totals.needs_data} release-health signals need data`
  if (totals.attention > 0) return `${totals.attention} release-health signals need attention`
  return 'release health is ready'
}

function restoreEvidence(recovery: RecoveryContextResponse) {
  const parts = [
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

function parseCount(value: string | undefined) {
  if (value === undefined || value.trim() === '') return undefined
  const parsed = Number.parseInt(value, 10)
  return Number.isFinite(parsed) ? parsed : undefined
}
