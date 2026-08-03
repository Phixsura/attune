import type { PreflightCheckResult } from '@/proto/attune/v1/system'
import type { RecoveryContextResponse } from './api/get-recovery-context'
import type { ReleaseContextResponse } from './api/get-release-context'

export type BackupRestoreDrillStatus = 'verified' | 'watch' | 'blocked' | 'needs_data'

export type BackupRestoreLaneKey =
  | 'backup_freshness'
  | 'restore_execution'
  | 'migration_readiness'
  | 'runbook_ownership'
  | 'remediation_path'

export type BackupRestoreLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: BackupRestoreLaneKey
  owner: string
  signal: string
  status: BackupRestoreDrillStatus
  title: string
}

export type BackupRestoreDrill = {
  fingerprint: string
  lanes: BackupRestoreLane[]
  summary: string
  totals: Record<BackupRestoreDrillStatus, number> & {
    total: number
  }
}

export type BackupRestoreDrillInput = {
  dashboardHref: string
  preflightChecks?: PreflightCheckResult[]
  recovery?: RecoveryContextResponse
  release?: ReleaseContextResponse
  tenantName: string
}

export function buildBackupRestoreDrill(input: BackupRestoreDrillInput): BackupRestoreDrill {
  const lanes = [
    backupFreshnessLane(input),
    restoreExecutionLane(input),
    migrationReadinessLane(input),
    runbookOwnershipLane(input),
    remediationPathLane(input),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    total: lanes.length,
    verified: lanes.filter((lane) => lane.status === 'verified').length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${input.tenantName || 'tenant unknown'} / ${
      input.recovery?.lastRun?.backupRef || 'backup unknown'
    } / ${input.release?.lifecycleState || input.recovery?.status || 'state unknown'}`,
    lanes,
    summary: backupRestoreSummary(totals),
    totals,
  }
}

function backupFreshnessLane(input: BackupRestoreDrillInput): BackupRestoreLane {
  const recovery = input.recovery
  const ageSeconds = recovery?.ageSeconds
  const freshnessWindowSeconds = recovery?.freshnessWindowSeconds
  const backupRef = recovery?.lastRun?.backupRef

  return {
    actionHref: input.dashboardHref,
    actionLabel: 'Open backup evidence',
    evidence: recovery
      ? `${recovery.message || 'no recovery message'} / status=${recovery.status}`
      : 'recovery context is missing',
    guardrail: 'A restore-safe tenant must show a named backup inside the freshness window.',
    key: 'backup_freshness',
    owner: input.release?.ownerTeam || 'Reliability',
    signal:
      backupRef && ageSeconds !== undefined && freshnessWindowSeconds
        ? `backup=${backupRef} / age=${formatDuration(ageSeconds)} / window=${formatDuration(
            freshnessWindowSeconds,
          )}`
        : 'backup freshness evidence missing',
    status: backupFreshnessStatus(recovery),
    title: 'Backup freshness',
  }
}

function restoreExecutionLane(input: BackupRestoreDrillInput): BackupRestoreLane {
  const lastRun = input.recovery?.lastRun

  return {
    actionHref: input.dashboardHref,
    actionLabel: 'Open restore drill',
    evidence: lastRun
      ? `${lastRun.backupRef || 'backup unknown'} / ran=${lastRun.ranAt || 'unknown'}`
      : 'restore drill last run is missing',
    guardrail: 'The last restore drill must pass with duration and backup reference evidence.',
    key: 'restore_execution',
    owner: input.release?.ownerTeam || 'Reliability',
    signal: lastRun
      ? `${lastRun.status} restore / ${formatMilliseconds(lastRun.durationMs)}`
      : 'restore execution unknown',
    status: restoreExecutionStatus(input.recovery),
    title: 'Restore execution',
  }
}

function migrationReadinessLane(input: BackupRestoreDrillInput): BackupRestoreLane {
  const release = input.release
  const backupOrMigrationChecks = backupOrMigrationPreflightChecks(input.preflightChecks)
  const failed = backupOrMigrationChecks.filter((check) => check.status === 'fail').length
  const warned = backupOrMigrationChecks.filter((check) => check.status === 'warn').length

  return {
    actionHref: release?.runbookUrl || input.dashboardHref,
    actionLabel: 'Open migration runbook',
    evidence: release
      ? `${release.compatibilityRules.length} compatibility rules / ${backupOrMigrationChecks.length} backup or migration checks`
      : 'release context is missing',
    guardrail:
      'Restore drills are not complete until the running release has explicit compatibility and migration evidence.',
    key: 'migration_readiness',
    owner: release?.ownerTeam || 'Release Engineering',
    signal: release
      ? `${release.lifecycleState || 'unknown'} / ${release.compatibilityRules.length} compatibility rules`
      : 'migration readiness unknown',
    status: migrationReadinessStatus(release, failed, warned),
    title: 'Migration readiness',
  }
}

function runbookOwnershipLane(input: BackupRestoreDrillInput): BackupRestoreLane {
  const release = input.release
  const hasOwner = Boolean(release?.ownerTeam)
  const hasRunbook = Boolean(release?.runbookUrl)
  const hasEscalation = Boolean(release?.escalationUrl)

  return {
    actionHref: release?.runbookUrl || input.dashboardHref,
    actionLabel: 'Open runbook',
    evidence: release
      ? `owner=${release.ownerTeam || 'missing'} / runbook=${
          hasRunbook ? 'present' : 'missing'
        } / escalation=${hasEscalation ? 'present' : 'missing'}`
      : 'release ownership context is missing',
    guardrail: 'Restore responsibility must name an owner, a runbook, and an escalation path.',
    key: 'runbook_ownership',
    owner: release?.ownerTeam || 'Release owner',
    signal: release
      ? `owner=${release.ownerTeam || 'missing'} / runbook=${
          hasRunbook ? 'present' : 'missing'
        } / escalation=${hasEscalation ? 'present' : 'missing'}`
      : 'runbook ownership unknown',
    status: runbookOwnershipStatus(Boolean(release), hasOwner, hasRunbook, hasEscalation),
    title: 'Runbook ownership',
  }
}

function remediationPathLane(input: BackupRestoreDrillInput): BackupRestoreLane {
  const checks = backupOrMigrationPreflightChecks(input.preflightChecks)
  const failed = checks.filter((check) => check.status === 'fail').length
  const warned = checks.filter((check) => check.status === 'warn').length
  const firstRemediation =
    input.recovery?.remediation ||
    checks.find((check) => check.remediation?.trim())?.remediation ||
    ''
  const hasRecovery = Boolean(input.recovery)
  const hasPreflight = Boolean(input.preflightChecks)

  return {
    actionHref: input.dashboardHref,
    actionLabel: 'Open remediation evidence',
    evidence:
      hasRecovery || hasPreflight
        ? `${failed} fail / ${warned} warn backup or migration checks`
        : 'remediation inputs are missing',
    guardrail: 'Any backup, restore, or migration warning must have a named remediation path.',
    key: 'remediation_path',
    owner: input.release?.ownerTeam || 'Reliability',
    signal:
      firstRemediation ||
      (hasRecovery || hasPreflight ? 'no backup remediation required' : 'remediation unknown'),
    status: remediationPathStatus(
      input.recovery?.status,
      hasRecovery,
      hasPreflight,
      failed,
      warned,
    ),
    title: 'Remediation path',
  }
}

function backupFreshnessStatus(
  recovery: RecoveryContextResponse | undefined,
): BackupRestoreDrillStatus {
  if (!recovery) return 'needs_data'
  if (recovery.status === 'fail') return 'blocked'
  const ageSeconds = recovery.ageSeconds
  const freshnessWindowSeconds = recovery.freshnessWindowSeconds
  if (!recovery.lastRun?.backupRef || ageSeconds === undefined || freshnessWindowSeconds <= 0) {
    return 'needs_data'
  }
  if (ageSeconds > freshnessWindowSeconds) return 'blocked'
  if (recovery.status === 'warn' || ageSeconds >= freshnessWindowSeconds * 0.75) return 'watch'
  if (recovery.status === 'pass') return 'verified'
  return 'needs_data'
}

function restoreExecutionStatus(
  recovery: RecoveryContextResponse | undefined,
): BackupRestoreDrillStatus {
  const lastRun = recovery?.lastRun
  if (!recovery || !lastRun) return 'needs_data'
  if (lastRun.status === 'fail' || recovery.status === 'fail') return 'blocked'
  if (!lastRun.backupRef || lastRun.durationMs <= 0 || !lastRun.ranAt) return 'needs_data'
  if (lastRun.status === 'warn' || recovery.status === 'warn') return 'watch'
  if (lastRun.status === 'pass') return 'verified'
  return 'needs_data'
}

function migrationReadinessStatus(
  release: ReleaseContextResponse | undefined,
  failed: number,
  warned: number,
): BackupRestoreDrillStatus {
  if (!release) return 'needs_data'
  if (release.lifecycleState === 'blocked' || failed > 0) return 'blocked'
  if (!release.lifecycleState || release.compatibilityRules.length === 0) return 'needs_data'
  if (
    warned > 0 ||
    release.lifecycleState === 'deprecated' ||
    release.lifecycleState === 'migrating' ||
    release.lifecycleState === 'recovering'
  ) {
    return 'watch'
  }
  return 'verified'
}

function runbookOwnershipStatus(
  hasRelease: boolean,
  hasOwner: boolean,
  hasRunbook: boolean,
  hasEscalation: boolean,
): BackupRestoreDrillStatus {
  if (!hasRelease || !hasOwner) return 'needs_data'
  if (!hasRunbook || !hasEscalation) return 'watch'
  return 'verified'
}

function remediationPathStatus(
  recoveryStatus: string | undefined,
  hasRecovery: boolean,
  hasPreflight: boolean,
  failed: number,
  warned: number,
): BackupRestoreDrillStatus {
  if (!hasRecovery && !hasPreflight) return 'needs_data'
  if (recoveryStatus === 'fail' || failed > 0) return 'blocked'
  if (recoveryStatus === 'warn' || warned > 0) return 'watch'
  return 'verified'
}

function backupOrMigrationPreflightChecks(checks: PreflightCheckResult[] | undefined) {
  return (checks ?? []).filter((check) => {
    const haystack = `${check.category} ${check.name} ${check.message}`.toLowerCase()
    return (
      haystack.includes('backup') || haystack.includes('restore') || haystack.includes('migration')
    )
  })
}

function backupRestoreSummary(
  totals: Record<BackupRestoreDrillStatus, number> & { total: number },
) {
  if (totals.blocked > 0) {
    return `${totals.blocked} backup / restore lanes are blocked`
  }
  if (totals.needs_data > 0) {
    return `${totals.needs_data} backup / restore lanes need evidence`
  }
  if (totals.watch > 0) {
    return `${totals.watch} backup / restore lanes need attention`
  }
  return 'backup and restore evidence is verified'
}

function formatDuration(seconds: number) {
  if (seconds < 60) return `${Math.round(seconds)}s`
  const minutes = seconds / 60
  if (minutes < 60) return `${Math.round(minutes)}m`
  const hours = minutes / 60
  if (hours < 24) return `${Math.round(hours)}h`
  return `${Math.round(hours / 24)}d`
}

function formatMilliseconds(milliseconds: number) {
  if (milliseconds < 1000) return `${milliseconds}ms`
  const seconds = milliseconds / 1000
  if (seconds < 60) return `${seconds < 10 ? seconds.toFixed(1) : Math.round(seconds)}s`
  const minutes = seconds / 60
  if (minutes < 60) return `${minutes < 10 ? minutes.toFixed(1) : Math.round(minutes)}m`
  const hours = minutes / 60
  if (hours < 24) return `${hours < 10 ? hours.toFixed(1) : Math.round(hours)}h`
  return `${Math.round(hours / 24)}d`
}
