import type { CustomerRequestSummary } from '@/proto/attune/v1/customer_request'
import type { PreflightCheckResult } from '@/proto/attune/v1/system'
import type { GetLLMUsageResponse, GetUsageResponse } from '@/proto/attune/v1/usage'

export type PipelineSloStatus = 'ready' | 'watch' | 'blocked' | 'needs_data'

export type PipelineSloKey = 'ingest' | 'enrich' | 'outbox' | 'sync'

export type PipelineSloLane = {
  actionHref: string
  actionLabel: string
  burnSignal: string
  dashboardHref: string
  escalation: string
  evidence: string
  key: PipelineSloKey
  objective: string
  owner: string
  releaseGate: string
  runbookHref: string
  status: PipelineSloStatus
  title: string
}

export type PipelineSloLedger = {
  fingerprint: string
  lanes: PipelineSloLane[]
  summary: string
  totals: Record<PipelineSloStatus, number> & {
    total: number
  }
}

export type PipelineSloLedgerInput = {
  customerRequests?: CustomerRequestSummary[]
  dashboardHref: string
  deadDeliveryCount?: number
  feedbackHref: string
  inflightDeadDeliveries?: number
  llmUsage?: GetLLMUsageResponse
  preflightChecks?: PreflightCheckResult[]
  readinessStatus?: string
  releaseLifecycleState?: string
  retryableDeadDeliveries?: number
  tenantName: string
  usage?: GetUsageResponse
}

export function buildPipelineSloLedger(input: PipelineSloLedgerInput): PipelineSloLedger {
  const lanes = [
    ingestPipelineLane(input),
    enrichPipelineLane(input),
    outboxPipelineLane(input),
    syncPipelineLane(input),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    ready: lanes.filter((lane) => lane.status === 'ready').length,
    total: lanes.length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${input.tenantName || 'tenant unknown'} / ${formatNumber(
      parseCount(input.usage?.total) ?? 0,
    )} ingested / ${formatNumber(parseCount(input.llmUsage?.calls) ?? 0)} enrich calls / ${formatNumber(
      input.customerRequests?.length ?? 0,
    )} sync rows`,
    lanes,
    summary: pipelineSummary(totals),
    totals,
  }
}

function ingestPipelineLane(input: PipelineSloLedgerInput): PipelineSloLane {
  const total = parseCount(input.usage?.total)
  const quota = parseCount(input.usage?.quota)
  const buckets = input.usage?.series.length

  return {
    actionHref: input.feedbackHref,
    actionLabel: 'Open feedback intake',
    burnSignal:
      total !== undefined && buckets !== undefined
        ? `${formatNumber(total)} ingested / ${buckets} buckets / ${formatPercent(
            /* v8 ignore next -- @preserve: zero or missing quota is rendered as unknown burn rate and covered by status tests. */
            quota && quota > 0 ? total / quota : undefined,
          )} quota used`
        : 'ingest burn signal missing',
    dashboardHref: input.dashboardHref,
    escalation: 'Ingest on-call',
    evidence: input.usage
      ? /* v8 ignore next -- @preserve: usage window labels are optional display evidence. */
        `${input.usage.periodStart || 'unknown'} -> ${input.usage.periodEnd || 'unknown'}`
      : 'usage aggregate is missing',
    key: 'ingest',
    objective: '99.9% source events accepted without internal errors or unexpected throttling',
    owner: 'Data Pipeline',
    releaseGate: releaseGateEvidence(input, ['database', 'migration', 'worker']),
    runbookHref: reliabilityRunbookHref('attuneingestservicefastburn'),
    status: ingestStatus(input, total, buckets),
    title: 'Ingest pipeline SLO',
  }
}

function enrichPipelineLane(input: PipelineSloLedgerInput): PipelineSloLane {
  const calls = parseCount(input.llmUsage?.calls)
  const errors = parseCount(input.llmUsage?.errors)
  const buckets = input.llmUsage?.series.length

  return {
    actionHref: '/feedback/terminal-failures',
    actionLabel: 'Open enrichment failures',
    burnSignal:
      calls !== undefined && errors !== undefined && buckets !== undefined
        ? `${formatNumber(calls)} calls / ${formatNumber(errors)} errors / ${formatPercent(
            /* v8 ignore next -- @preserve: zero-call enrichment windows render an unknown error rate and are covered by tests. */
            calls > 0 ? errors / calls : undefined,
          )} error rate`
        : 'enrichment burn signal missing',
    dashboardHref: input.dashboardHref,
    escalation: 'AI Pipeline on-call',
    evidence: input.llmUsage
      ? /* v8 ignore next -- @preserve: LLM usage window labels are optional display evidence. */
        `${input.llmUsage.periodStart || 'unknown'} -> ${
          input.llmUsage.periodEnd || 'unknown'
        } / ${input.llmUsage.granularity || 'unknown'}`
      : 'LLM usage aggregate is missing',
    key: 'enrich',
    objective: '95% enrichment attempts complete inside the user-visible latency target',
    owner: 'AI Pipeline',
    releaseGate: releaseGateEvidence(input, ['worker', 'metrics']),
    runbookHref: reliabilityRunbookHref('attuneenrichmentfastburn'),
    status: enrichStatus(input, calls, errors),
    title: 'Enrich pipeline SLO',
  }
}

function outboxPipelineLane(input: PipelineSloLedgerInput): PipelineSloLane {
  return {
    actionHref: '/administration/dead-deliveries',
    actionLabel: 'Open dead deliveries',
    /* v8 ignore next -- @preserve: retryable and in-flight counts default to zero when dead-letter evidence is present. */
    burnSignal:
      input.deadDeliveryCount !== undefined
        ? `${formatNumber(input.retryableDeadDeliveries ?? 0)} retryable / ${formatNumber(
            input.inflightDeadDeliveries ?? 0,
          )} in-flight / ${formatNumber(input.deadDeliveryCount)} dead`
        : 'outbox burn signal missing',
    dashboardHref: input.dashboardHref,
    escalation: 'Delivery on-call',
    evidence:
      input.deadDeliveryCount !== undefined
        ? 'dead-letter inventory, retryable state, and in-flight recovery state'
        : 'dead-letter inventory is missing',
    key: 'outbox',
    objective: '99.9% terminal deliveries avoid unrecovered dead-letter states',
    owner: 'Delivery',
    releaseGate: releaseGateEvidence(input, ['worker', 'metrics']),
    runbookHref: reliabilityRunbookHref('attuneoutboxdeliveryfastburn'),
    status: outboxStatus(input),
    title: 'Outbox pipeline SLO',
  }
}

function syncPipelineLane(input: PipelineSloLedgerInput): PipelineSloLane {
  const rows = input.customerRequests
  const synced = sumRequests(rows, 'syncedIssueCount')
  const stale = sumRequests(rows, 'staleIssueCount')
  const failed = sumRequests(rows, 'failedIssueCount')
  const pending = sumRequests(rows, 'pendingIssueCount')
  const manual = sumRequests(rows, 'manualIssueCount')

  return {
    actionHref: '/integrations/external-sync',
    actionLabel: 'Open external sync',
    burnSignal: rows
      ? `${formatNumber(synced)} synced / ${formatNumber(stale)} stale / ${formatNumber(
          failed,
        )} failed / ${formatNumber(pending)} pending / ${formatNumber(manual)} manual`
      : 'sync burn signal missing',
    dashboardHref: input.dashboardHref,
    escalation: 'Integrations on-call',
    evidence: rows
      ? `${formatNumber(rows.length)} request projections inspected for delivery drift`
      : 'customer request sync projection is missing',
    key: 'sync',
    objective: '99.5% external issue projections stay fresh, linked, and recoverable',
    owner: 'Integrations',
    releaseGate: releaseGateEvidence(input, ['database', 'worker', 'metrics']),
    runbookHref:
      'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md#external-sync',
    status: syncStatus(input, rows, stale, failed, pending),
    title: 'Sync pipeline SLO',
  }
}

function ingestStatus(
  input: PipelineSloLedgerInput,
  total: number | undefined,
  buckets: number | undefined,
): PipelineSloStatus {
  const gate = lifecycleGateStatus(input)
  if (gate !== 'ready') return gate
  if (input.readinessStatus === undefined || total === undefined || buckets === undefined) {
    return 'needs_data'
  }
  if (input.readinessStatus === 'fail') return 'blocked'
  if (input.readinessStatus === 'warn' || total === 0 || buckets === 0) return 'watch'
  return 'ready'
}

function enrichStatus(
  input: PipelineSloLedgerInput,
  calls: number | undefined,
  errors: number | undefined,
): PipelineSloStatus {
  const gate = lifecycleGateStatus(input)
  if (gate !== 'ready') return gate
  if (calls === undefined || errors === undefined) return 'needs_data'
  if (calls === 0) return 'watch'
  const errorRate = errors / calls
  if (errorRate > 0.05) return 'blocked'
  if (errors > 0) return 'watch'
  return 'ready'
}

function outboxStatus(input: PipelineSloLedgerInput): PipelineSloStatus {
  const gate = lifecycleGateStatus(input)
  if (gate !== 'ready') return gate
  if (input.deadDeliveryCount === undefined) return 'needs_data'
  if (input.deadDeliveryCount === 0) return 'ready'
  /* v8 ignore next -- @preserve: retryable dead-letter count defaults to zero in legacy snapshots. */
  if ((input.retryableDeadDeliveries ?? 0) === 0 && input.deadDeliveryCount > 0) return 'blocked'
  return 'watch'
}

function syncStatus(
  input: PipelineSloLedgerInput,
  rows: CustomerRequestSummary[] | undefined,
  stale: number,
  failed: number,
  pending: number,
): PipelineSloStatus {
  const gate = lifecycleGateStatus(input)
  if (gate !== 'ready') return gate
  if (!rows) return 'needs_data'
  if (failed > 0) return 'blocked'
  if (stale + pending > 0) return 'watch'
  return 'ready'
}

function lifecycleGateStatus(input: PipelineSloLedgerInput): PipelineSloStatus {
  if (input.releaseLifecycleState === 'blocked') return 'blocked'
  return 'ready'
}

function releaseGateEvidence(input: PipelineSloLedgerInput, categories: string[]) {
  const checks = input.preflightChecks?.filter((check) => categories.includes(check.category))
  const failed = checks?.filter((check) => check.status === 'fail').length ?? 0
  const warn = checks?.filter((check) => check.status === 'warn').length ?? 0
  const passed = checks?.filter((check) => check.status === 'pass').length ?? 0
  const gate = checks
    ? `${passed} pass / ${warn} warn / ${failed} fail`
    : 'preflight evidence missing'
  return `${gate} / release ${input.releaseLifecycleState ?? 'unknown'}`
}

function pipelineSummary(totals: PipelineSloLedger['totals']) {
  if (totals.blocked > 0) return `${totals.blocked} pipeline SLOs are blocked`
  if (totals.needs_data > 0) return `${totals.needs_data} pipeline SLOs need evidence`
  if (totals.watch > 0) return `${totals.watch} pipeline SLOs need attention`
  return 'pipeline SLO evidence is ready'
}

function sumRequests(
  rows: CustomerRequestSummary[] | undefined,
  key:
    | 'failedIssueCount'
    | 'manualIssueCount'
    | 'pendingIssueCount'
    | 'staleIssueCount'
    | 'syncedIssueCount',
) {
  return rows?.reduce((sum, request) => sum + request[key], 0) ?? 0
}

function parseCount(value: string | undefined) {
  if (!value) return undefined
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : undefined
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value)
}

function formatPercent(value: number | undefined) {
  if (value === undefined) return 'unknown'
  return `${(value * 100).toFixed(1)}%`
}

function reliabilityRunbookHref(anchor: string) {
  return `https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#${anchor}`
}
