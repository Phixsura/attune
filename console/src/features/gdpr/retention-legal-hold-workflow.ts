import {
  type GdprOperationsResponse,
  GdprRequestStatus,
  type GdprRequestSummary,
  GdprRequestType,
} from '@/proto/attune/v1/gdpr'

export type RetentionLegalHoldStatus = 'ready' | 'watch' | 'blocked' | 'needs_data'

export type RetentionLegalHoldLaneKey =
  | 'retention_policy'
  | 'legal_hold_gate'
  | 'delete_grace_window'
  | 'export_residue'
  | 'backup_retention'

export type RetentionLegalHoldLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: RetentionLegalHoldLaneKey
  owner: string
  signal: string
  status: RetentionLegalHoldStatus
  title: string
}

export type RetentionLegalHoldWorkflow = {
  fingerprint: string
  lanes: RetentionLegalHoldLane[]
  summary: string
  totals: Record<RetentionLegalHoldStatus, number> & {
    total: number
  }
}

export type RetentionLegalHoldWorkflowInput = {
  operations?: GdprOperationsResponse
  requests?: GdprRequestSummary[]
}

export function buildRetentionLegalHoldWorkflow(
  input: RetentionLegalHoldWorkflowInput,
): RetentionLegalHoldWorkflow {
  const lanes = [
    retentionPolicyLane(input),
    legalHoldGateLane(input),
    deleteGraceWindowLane(input),
    exportResidueLane(input),
    backupRetentionLane(input),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    ready: lanes.filter((lane) => lane.status === 'ready').length,
    total: lanes.length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: input.operations
      ? `${formatDays(input.operations.auditRetentionDays)} audit / ${formatDuration(
          input.operations.deleteGraceWindowSeconds,
        )} delete grace / legal hold ${onOff(input.operations.legalHoldSupported)} / ${formatNumber(
          requestCount(input.requests),
        )} request records`
      : 'retention operations evidence missing',
    lanes,
    summary: retentionLegalHoldSummary(totals),
    totals,
  }
}

function retentionPolicyLane(input: RetentionLegalHoldWorkflowInput): RetentionLegalHoldLane {
  const operations = input.operations
  return {
    actionHref: '/administration/gdpr',
    actionLabel: 'Review retention policy',
    evidence: operations
      ? `export TTL ${formatDuration(operations.exportTtlSeconds)} / audit prune ${formatDuration(
          operations.auditPruneIntervalSeconds,
        )} / step-up ${formatDuration(operations.stepUp?.ttlSeconds)}`
      : 'retention operations evidence is missing',
    guardrail:
      'Tenant retention policy must define export artifact lifetime, audit retention, prune cadence, and recent step-up protection.',
    key: 'retention_policy',
    owner: 'Security + Compliance',
    signal: operations
      ? `audit ${formatDays(operations.auditRetentionDays)} / export ${formatDuration(
          operations.exportTtlSeconds,
        )} / prune ${formatDuration(operations.auditPruneIntervalSeconds)}`
      : 'retention policy evidence missing',
    status: retentionPolicyStatus(operations),
    title: 'Tenant retention policy',
  }
}

function legalHoldGateLane(input: RetentionLegalHoldWorkflowInput): RetentionLegalHoldLane {
  const operations = input.operations
  const scheduled = scheduledDeleteRequests(input.requests)
  return {
    actionHref: '/administration/gdpr',
    actionLabel: 'Review legal hold gate',
    evidence: operations
      ? `${scheduled.length} visible scheduled delete records / legal hold ${onOff(
          operations.legalHoldSupported,
        )}`
      : 'legal hold gate evidence is missing',
    guardrail:
      'Scheduled deletes need a legal-hold-aware gate before irreversible online data removal.',
    key: 'legal_hold_gate',
    owner: 'Legal + Security',
    signal: operations
      ? `legal hold ${onOff(operations.legalHoldSupported)} / ${formatNumber(
          operations.scheduledDeleteCount,
        )} scheduled deletes`
      : 'legal hold evidence missing',
    status: legalHoldGateStatus(operations),
    title: 'Legal hold gate',
  }
}

function deleteGraceWindowLane(input: RetentionLegalHoldWorkflowInput): RetentionLegalHoldLane {
  const operations = input.operations
  const scheduled = scheduledDeleteRequests(input.requests)
  return {
    actionHref: '/administration/gdpr',
    actionLabel: 'Review scheduled deletes',
    evidence: operations
      ? `${scheduled.length} visible scheduled records / next execution ${nextScheduledExecution(
          scheduled,
        )}`
      : 'delete grace-window evidence is missing',
    guardrail:
      'Irreversible delete requests need a cancellable grace window and visible execution timestamp.',
    key: 'delete_grace_window',
    owner: 'Security Operations',
    signal: operations
      ? `grace ${formatDuration(operations.deleteGraceWindowSeconds)} / ${formatNumber(
          operations.scheduledDeleteCount,
        )} scheduled deletes / ${formatNumber(scheduled.length)} visible`
      : 'delete grace-window evidence missing',
    status: deleteGraceWindowStatus(operations),
    title: 'Delete grace window',
  }
}

function exportResidueLane(input: RetentionLegalHoldWorkflowInput): RetentionLegalHoldLane {
  const operations = input.operations
  const readyExports = readyExportRequests(input.requests)
  return {
    actionHref: '/administration/gdpr',
    actionLabel: 'Review export artifacts',
    evidence: operations
      ? `${readyExports.length} visible ready exports / next expiry ${operations.nextExportExpiryAt || 'none'}`
      : 'export residue evidence is missing',
    guardrail:
      'Downloadable export artifacts must have a finite expiry, revocation path, and visible residue count.',
    key: 'export_residue',
    owner: 'Security Operations',
    signal: operations
      ? `${formatNumber(operations.readyExportCount)} ready exports / expires ${
          operations.nextExportExpiryAt || 'none'
        } / TTL ${formatDuration(operations.exportTtlSeconds)}`
      : 'export residue evidence missing',
    status: exportResidueStatus(operations),
    title: 'Export artifact residue',
  }
}

function backupRetentionLane(input: RetentionLegalHoldWorkflowInput): RetentionLegalHoldLane {
  const operations = input.operations
  return {
    actionHref: '/administration/reliability',
    actionLabel: 'Review backup retention',
    evidence: operations
      ? `hashed audit ${onOff(operations.hashedAuditResidue)} / backup residue ${onOff(
          operations.backupsMayRetainUntilRotation,
        )}`
      : 'backup retention evidence is missing',
    guardrail:
      'Post-delete residue must be irreversible in audit logs and explicit when backups can retain data until rotation.',
    key: 'backup_retention',
    owner: 'Reliability + Compliance',
    signal: operations
      ? `hashed audit ${onOff(operations.hashedAuditResidue)} / backup residue ${onOff(
          operations.backupsMayRetainUntilRotation,
        )} / audit ${formatDays(operations.auditRetentionDays)}`
      : 'backup retention evidence missing',
    status: backupRetentionStatus(operations),
    title: 'Backup retention residue',
  }
}

function retentionPolicyStatus(
  operations: GdprOperationsResponse | undefined,
): RetentionLegalHoldStatus {
  if (!operations) return 'needs_data'
  /* v8 ignore next -- @preserve: GDPR operations responses include step-up evidence in console contract fixtures. */
  const stepUpTtlSeconds = operations.stepUp?.ttlSeconds ?? 0
  if (
    operations.exportTtlSeconds <= 0 ||
    operations.auditRetentionDays <= 0 ||
    operations.auditPruneIntervalSeconds <= 0 ||
    stepUpTtlSeconds <= 0
  ) {
    return 'blocked'
  }
  if (operations.auditRetentionDays < 30) return 'watch'
  return 'ready'
}

function legalHoldGateStatus(
  operations: GdprOperationsResponse | undefined,
): RetentionLegalHoldStatus {
  if (!operations) return 'needs_data'
  if (!operations.legalHoldSupported && operations.scheduledDeleteCount > 0) return 'blocked'
  if (!operations.legalHoldSupported) return 'watch'
  return 'ready'
}

function deleteGraceWindowStatus(
  operations: GdprOperationsResponse | undefined,
): RetentionLegalHoldStatus {
  if (!operations) return 'needs_data'
  if (operations.deleteGraceWindowSeconds <= 0 && operations.scheduledDeleteCount > 0) {
    return 'blocked'
  }
  if (operations.deleteGraceWindowSeconds <= 0) return 'watch'
  if (operations.scheduledDeleteCount > 0) return 'watch'
  return 'ready'
}

function exportResidueStatus(
  operations: GdprOperationsResponse | undefined,
): RetentionLegalHoldStatus {
  if (!operations) return 'needs_data'
  if (operations.exportTtlSeconds <= 0) return 'blocked'
  if (operations.readyExportCount > 0 && !operations.nextExportExpiryAt) return 'blocked'
  if (operations.readyExportCount > 0) return 'watch'
  return 'ready'
}

function backupRetentionStatus(
  operations: GdprOperationsResponse | undefined,
): RetentionLegalHoldStatus {
  if (!operations) return 'needs_data'
  if (!operations.hashedAuditResidue) return 'blocked'
  if (operations.backupsMayRetainUntilRotation) return 'watch'
  return 'ready'
}

function scheduledDeleteRequests(requests: GdprRequestSummary[] | undefined) {
  return (requests ?? []).filter(
    (request) =>
      request.requestType === GdprRequestType.GDPR_REQUEST_TYPE_DELETE &&
      request.status === GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED,
  )
}

function readyExportRequests(requests: GdprRequestSummary[] | undefined) {
  return (requests ?? []).filter(
    (request) =>
      request.requestType === GdprRequestType.GDPR_REQUEST_TYPE_EXPORT &&
      request.status === GdprRequestStatus.GDPR_REQUEST_STATUS_READY,
  )
}

function nextScheduledExecution(requests: GdprRequestSummary[]) {
  return (
    requests
      .map((request) => request.executeAfter)
      .filter(Boolean)
      .sort()[0] ?? 'none'
  )
}

function requestCount(requests: GdprRequestSummary[] | undefined) {
  return requests?.length ?? 0
}

function retentionLegalHoldSummary(totals: RetentionLegalHoldWorkflow['totals']) {
  if (totals.blocked > 0) return `${totals.blocked} retention and legal-hold checks are blocked`
  if (totals.needs_data > 0) {
    return `${totals.needs_data} retention and legal-hold checks need evidence`
  }
  if (totals.watch > 0) return `${totals.watch} retention and legal-hold checks need attention`
  return 'retention and legal-hold evidence is verified'
}

function formatDuration(seconds?: number) {
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
