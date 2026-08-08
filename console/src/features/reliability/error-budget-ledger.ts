import { type ReliabilityCatalogEntryValue, reliabilityCatalog } from './reliability-catalog'

export type ErrorBudgetLedgerStatus = 'monitored' | 'attention' | 'blocked' | 'needs_data'

export type ErrorBudgetLedgerEntry = {
  alertName: string
  budgetAllowanceLabel: string
  burnRateQuery: string
  currentSignal: string
  dashboardHref: string
  escalation: string
  exceptionEvidence: string
  exceptionPolicy: string
  fastBurnThreshold: string
  incidentEvidence: string
  key: ReliabilityCatalogEntryValue['key']
  objectiveLabel: string
  owner: string
  ratioQuery: string
  remainingBudgetQuery: string
  runbookHref: string
  scopeLabel: string
  slowBurnThreshold: string
  status: ErrorBudgetLedgerStatus
  title: string
}

export type ErrorBudgetLedger = {
  entries: ErrorBudgetLedgerEntry[]
  totals: Record<ErrorBudgetLedgerStatus, number> & {
    total: number
  }
}

export type ErrorBudgetLedgerInput = {
  activeApiKeys?: number
  activeGdpr?: number
  activeMcpClients?: number
  authMode?: string
  dashboardHref: string
  deadDeliveryCount?: number
  inflightDeadDeliveries?: number
  queuedGdpr?: number
  readinessStatus?: string
  recoveryStatus?: string
  releaseLifecycleState?: string
  retryableDeadDeliveries?: number
  scheduledDeletes?: number
  tenantName: string
  totalApiKeys?: number
  totalMcpClients?: number
}

const fastBurnThreshold = '14.4x on 5m + 1h'
const slowBurnThreshold = '6x on 30m + 6h'

export function buildErrorBudgetLedger(input: ErrorBudgetLedgerInput): ErrorBudgetLedger {
  const entries = reliabilityCatalog.map((entry) => buildErrorBudgetLedgerEntry(entry, input))
  return {
    entries,
    totals: {
      attention: entries.filter((entry) => entry.status === 'attention').length,
      blocked: entries.filter((entry) => entry.status === 'blocked').length,
      monitored: entries.filter((entry) => entry.status === 'monitored').length,
      needs_data: entries.filter((entry) => entry.status === 'needs_data').length,
      total: entries.length,
    },
  }
}

function buildErrorBudgetLedgerEntry(
  entry: ReliabilityCatalogEntryValue,
  input: ErrorBudgetLedgerInput,
): ErrorBudgetLedgerEntry {
  return {
    alertName: entry.alertName,
    budgetAllowanceLabel: formatBudgetAllowance(entry.objective),
    burnRateQuery: burnRateQuery(entry, '5m'),
    currentSignal: currentSignal(entry.key, input),
    dashboardHref: input.dashboardHref,
    escalation: entry.escalation,
    exceptionEvidence: entry.budgetExceptionNote,
    exceptionPolicy: entry.budgetExceptionPolicy,
    fastBurnThreshold,
    incidentEvidence: `${entry.alertName}, ${slowBurnAlertName(entry.alertName)}, runbook, and ${entry.recordedRatioBase}:ratio30m/:ratio6h`,
    key: entry.key,
    objectiveLabel: formatObjective(entry.objective),
    owner: entry.owner,
    ratioQuery: `${entry.recordedRatioBase}:ratio5m`,
    remainingBudgetQuery: remainingBudgetQuery(entry),
    runbookHref: reliabilityRunbookHref(entry.alertName),
    scopeLabel: scopeLabel(entry.scope),
    slowBurnThreshold,
    status: errorBudgetStatus(entry.key, input),
    title: entry.title,
  }
}

function errorBudgetStatus(
  key: ReliabilityCatalogEntryValue['key'],
  input: ErrorBudgetLedgerInput,
): ErrorBudgetLedgerStatus {
  if (input.releaseLifecycleState === 'blocked') {
    return 'blocked'
  }

  switch (key) {
    case 'ingest_service':
    case 'enrichment_latency':
      return readinessStatus(input.readinessStatus)
    case 'outbox_delivery':
      if (input.deadDeliveryCount === undefined) return 'needs_data'
      return input.deadDeliveryCount > 0 ? 'attention' : 'monitored'
    case 'oidc_login':
      return input.authMode ? 'monitored' : 'needs_data'
    case 'apikey_access':
      if (input.totalApiKeys === undefined || input.activeApiKeys === undefined) {
        return 'needs_data'
      }
      return input.activeApiKeys > 0 ? 'monitored' : 'attention'
    case 'mcp_tool':
      if (input.totalMcpClients === undefined || input.activeMcpClients === undefined) {
        return 'needs_data'
      }
      return input.activeMcpClients > 0 ? 'monitored' : 'attention'
    case 'gdpr_job':
      if (input.recoveryStatus === undefined) return 'needs_data'
      if (input.recoveryStatus === 'fail') return 'blocked'
      if (input.recoveryStatus === 'skipped') return 'needs_data'
      return numberOrZero(input.queuedGdpr) +
        numberOrZero(input.activeGdpr) +
        numberOrZero(input.scheduledDeletes) >
        0
        ? 'attention'
        : 'monitored'
    /* v8 ignore next -- @preserve: reliabilityCatalog keys are exhaustive for this switch. */
    default:
      return 'monitored'
  }
}

function readinessStatus(status: string | undefined): ErrorBudgetLedgerStatus {
  switch (status) {
    case 'pass':
      return 'monitored'
    case 'warn':
      return 'attention'
    case 'fail':
      return 'blocked'
    default:
      return 'needs_data'
  }
}

function currentSignal(
  key: ReliabilityCatalogEntryValue['key'],
  input: ErrorBudgetLedgerInput,
): string {
  switch (key) {
    case 'ingest_service':
      return `${input.tenantName} intake readiness: ${input.readinessStatus ?? 'unknown'}`
    case 'enrichment_latency':
      return `${input.tenantName} enrichment readiness: ${input.readinessStatus ?? 'unknown'}`
    case 'outbox_delivery':
      return `${numberOrZero(input.retryableDeadDeliveries)} retryable / ${numberOrZero(
        input.inflightDeadDeliveries,
      )} in-flight / ${numberOrZero(input.deadDeliveryCount)} dead`
    case 'oidc_login':
      return `auth mode: ${input.authMode ?? 'unknown'}`
    case 'apikey_access':
      return `${numberOrZero(input.activeApiKeys)} active / ${numberOrZero(
        input.totalApiKeys,
      )} total API keys`
    case 'mcp_tool':
      return `${numberOrZero(input.activeMcpClients)} active / ${numberOrZero(
        input.totalMcpClients,
      )} total MCP clients`
    case 'gdpr_job':
      return `${numberOrZero(input.queuedGdpr)} queued / ${numberOrZero(
        input.activeGdpr,
      )} active / ${numberOrZero(input.scheduledDeletes)} scheduled delete`
    /* v8 ignore next -- @preserve: reliabilityCatalog keys are exhaustive for this switch. */
    default:
      return `${input.tenantName} SLO evidence`
  }
}

function burnRateQuery(entry: ReliabilityCatalogEntryValue, window: '5m' | '1h' | '30m' | '6h') {
  const budgetRatio = formatBudgetRatio(entry.objective)
  return `${observedBadRatio(entry, window)} / ${budgetRatio}`
}

function remainingBudgetQuery(entry: ReliabilityCatalogEntryValue) {
  const budgetRatio = formatBudgetRatio(entry.objective)
  return `clamp_min(1 - (${observedBadRatio(entry, '6h')} / ${budgetRatio}), 0)`
}

function observedBadRatio(entry: ReliabilityCatalogEntryValue, window: '5m' | '1h' | '30m' | '6h') {
  const recordedRatio = `${entry.recordedRatioBase}:ratio${window}`
  return entry.burnKind === 'success' ? `(1 - ${recordedRatio})` : recordedRatio
}

function formatBudgetRatio(objective: number) {
  return trimTrailingZeros(1 - objective)
}

function formatObjective(objective: number) {
  return `${trimTrailingZeros(objective * 100)}%`
}

function formatBudgetAllowance(objective: number) {
  return `${((1 - objective) * 100).toFixed(2)}% budget allowance`
}

function trimTrailingZeros(value: number) {
  return value.toFixed(3).replace(/\.?0+$/, '')
}

function numberOrZero(value: number | undefined) {
  return value ?? 0
}

function scopeLabel(scope: ReliabilityCatalogEntryValue['scope']) {
  return scope.replace('_', ' ')
}

function slowBurnAlertName(alertName: string) {
  return alertName.replace(/FastBurn$/, 'SlowBurn')
}

function reliabilityRunbookHref(alertName: string) {
  return `https://github.com/Phixsura/attune/blob/main/observability/runbooks.md#${alertName.toLowerCase()}`
}
