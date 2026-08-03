import type { ApiKey } from '@/proto/attune/v1/api_key'
import type { GdprOperationsResponse } from '@/proto/attune/v1/gdpr'
import type { GetLLMUsageResponse, GetUsageResponse } from '@/proto/attune/v1/usage'

export type TenantQuotaMcpClient = {
  created_at?: string
  created_by?: string
  id?: string
  name?: string
  redirect_uris?: string[]
  rate_limit_burst?: number | null
  rate_limit_rpm?: number | null
  revoked_at?: string
  scopes?: string[]
  tool_policy_mode?: string
}

export type TenantQuotaSaturationStatus = 'healthy' | 'watch' | 'saturated' | 'needs_data'

export type TenantQuotaLaneKey = 'ingest' | 'enrichment' | 'mcp' | 'gdpr' | 'outbox'

export type TenantQuotaLane = {
  actionHref: string
  actionLabel: string
  capacityLabel: string
  consumptionLabel: string
  evidence: string
  guardrail: string
  key: TenantQuotaLaneKey
  owner: string
  saturationPct: number | null
  signal: string
  status: TenantQuotaSaturationStatus
  title: string
}

export type TenantQuotaSaturation = {
  fingerprint: string
  lanes: TenantQuotaLane[]
  summary: string
  totals: Record<TenantQuotaSaturationStatus, number> & {
    total: number
  }
  windowLabel: string
  worstLaneKey: TenantQuotaLaneKey | null
  worstSaturationPct: number | null
}

export type TenantQuotaSaturationInput = {
  apiKeys?: ApiKey[]
  dashboardHref: string
  deadDeliveryCount?: number
  gdprOperations?: GdprOperationsResponse
  inflightDeadDeliveries?: number
  llmUsage?: GetLLMUsageResponse
  mcpClients?: TenantQuotaMcpClient[]
  retryableDeadDeliveries?: number
  tenantName: string
  usage?: GetUsageResponse
}

const gdprWorkloadLimit = 10
const outboxDeadDeliveryLimit = 5

export function buildTenantQuotaSaturation(
  input: TenantQuotaSaturationInput,
): TenantQuotaSaturation {
  const lanes = [
    ingestQuotaLane(input),
    enrichmentQuotaLane(input),
    mcpQuotaLane(input),
    gdprQuotaLane(input),
    outboxQuotaLane(input),
  ]
  const totals = {
    healthy: lanes.filter((lane) => lane.status === 'healthy').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    saturated: lanes.filter((lane) => lane.status === 'saturated').length,
    total: lanes.length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }
  const worst = lanes
    .filter((lane) => lane.saturationPct !== null)
    .sort((a, b) => Number(b.saturationPct) - Number(a.saturationPct))[0]
  const windowLabel = quotaWindowLabel(input)

  return {
    fingerprint: `${input.tenantName || 'tenant unknown'} / ${windowLabel} / ${summaryStatus(
      totals,
    )}`,
    lanes,
    summary: quotaSummary(totals),
    totals,
    windowLabel,
    worstLaneKey: worst?.key ?? null,
    worstSaturationPct: worst?.saturationPct ?? null,
  }
}

function ingestQuotaLane(input: TenantQuotaSaturationInput): TenantQuotaLane {
  const usage = input.usage
  const total = parseCount(usage?.total)
  const quota = parseCount(usage?.quota)
  const activeKeys = input.apiKeys?.filter((key) => key.isActive)
  const limitedKeys = activeKeys?.filter((key) => numberOrZero(key.rateLimitRpm) > 0)
  const saturationPct = total !== undefined && quota && quota > 0 ? pct(total, quota) : null
  const unboundedKeys =
    activeKeys && limitedKeys ? Math.max(activeKeys.length - limitedKeys.length, 0) : undefined

  return {
    actionHref: '/analytics/usage',
    actionLabel: 'Open ingest usage',
    capacityLabel: quota
      ? `${formatNumber(quota)} monthly ingest quota`
      : 'monthly ingest quota unset',
    /* v8 ignore next -- @preserve: malformed ingest counters are covered by ingestStatus needs-data tests. */
    consumptionLabel:
      total !== undefined
        ? `${formatNumber(total)} ingested this period`
        : 'ingest consumption unknown',
    /* v8 ignore next -- @preserve: usage window and API-key count fallbacks guard legacy snapshots. */
    evidence: usage
      ? `${usage.periodStart || 'unknown'} -> ${usage.periodEnd || 'unknown'} / ${
          usage.series.length
        } usage buckets / ${limitedKeys?.length ?? 0}/${activeKeys?.length ?? 0} active keys rate-limited`
      : 'ingest usage endpoint is missing',
    guardrail:
      'Tenant monthly quota and per-key RPM must both be explicit before high-volume intake.',
    key: 'ingest',
    owner: 'Ingest',
    saturationPct,
    signal:
      unboundedKeys !== undefined
        ? `${formatPct(saturationPct)} used / ${unboundedKeys} unbounded active API keys`
        : `${formatPct(saturationPct)} used / API-key limit evidence missing`,
    status: ingestStatus(total, quota, unboundedKeys),
    title: 'Ingest quota',
  }
}

function enrichmentQuotaLane(input: TenantQuotaSaturationInput): TenantQuotaLane {
  const usage = input.llmUsage
  const calls = parseCount(usage?.calls)
  const errors = parseCount(usage?.errors)
  const errorPct =
    calls !== undefined && errors !== undefined ? pct(errors, Math.max(calls, 1)) : null
  const promptTokens = parseCount(usage?.promptTokens)
  const completionTokens = parseCount(usage?.completionTokens)

  return {
    actionHref: '/analytics/llm-usage',
    actionLabel: 'Open LLM usage',
    capacityLabel: 'provider error saturation proxy',
    consumptionLabel:
      calls !== undefined && errors !== undefined
        ? `${formatNumber(calls)} calls / ${formatNumber(errors)} errors`
        : 'LLM calls unknown',
    evidence: usage
      ? `${usage.periodStart || 'unknown'} -> ${usage.periodEnd || 'unknown'} / ${formatNumber(
          numberOrZero(promptTokens) + numberOrZero(completionTokens),
        )} tokens / $${usage.costUsd.toFixed(2)}`
      : 'LLM usage endpoint is missing',
    guardrail:
      'Provider errors should stay visible as enrichment saturation until route limits exist.',
    key: 'enrichment',
    owner: 'AI Pipeline',
    saturationPct: errorPct,
    signal: `${formatPct(errorPct)} provider-error saturation`,
    status: providerErrorStatus(calls, errors),
    title: 'Enrichment capacity',
  }
}

function mcpQuotaLane(input: TenantQuotaSaturationInput): TenantQuotaLane {
  const clients = input.mcpClients
  const activeClients = clients?.filter((client) => !client.revoked_at)
  const limitedClients = activeClients?.filter(
    (client) =>
      numberOrZero(client.rate_limit_rpm) > 0 && numberOrZero(client.rate_limit_burst) > 0,
  )
  const unboundedClients =
    activeClients && limitedClients
      ? Math.max(activeClients.length - limitedClients.length, 0)
      : undefined
  const saturationPct =
    activeClients && unboundedClients !== undefined && activeClients.length > 0
      ? pct(unboundedClients, activeClients.length)
      : null

  return {
    actionHref: '/mcp-clients',
    actionLabel: 'Open MCP limits',
    capacityLabel:
      activeClients && limitedClients
        ? `${limitedClients.length}/${activeClients.length} active clients have rpm+burst`
        : 'MCP client limit coverage unknown',
    consumptionLabel:
      activeClients && unboundedClients !== undefined
        ? `${unboundedClients} unbounded / ${activeClients.length} active MCP clients`
        : 'MCP saturation unknown',
    /* v8 ignore next -- @preserve: MCP active/limited counters default to zero for legacy inventory snapshots. */
    evidence: clients
      ? `${clients.length} total clients / ${activeClients?.length ?? 0} active / ${
          limitedClients?.length ?? 0
        } limited`
      : 'MCP client inventory is missing',
    guardrail: 'Every active MCP client should carry explicit rpm and burst limits.',
    key: 'mcp',
    owner: 'MCP',
    saturationPct,
    signal:
      unboundedClients !== undefined
        ? /* v8 ignore next -- @preserve: active MCP client count is present whenever unboundedClients is computed. */
          `${unboundedClients} unbounded / ${activeClients?.length ?? 0} active MCP clients`
        : 'MCP limit evidence missing',
    status: mcpLimitStatus(activeClients?.length, unboundedClients),
    title: 'MCP client rate limits',
  }
}

function gdprQuotaLane(input: TenantQuotaSaturationInput): TenantQuotaLane {
  const ops = input.gdprOperations
  const pressure = ops
    ? ops.queuedRequestCount + ops.activeRequestCount + ops.scheduledDeleteCount
    : undefined
  const saturationPct = pressure !== undefined ? pct(pressure, gdprWorkloadLimit) : null

  return {
    actionHref: '/administration/gdpr',
    actionLabel: 'Open GDPR operations',
    capacityLabel: `${gdprWorkloadLimit} privacy jobs operational guardrail`,
    consumptionLabel:
      pressure !== undefined
        ? `${pressure} queued, active, or scheduled jobs`
        : 'GDPR workload unknown',
    evidence: ops
      ? `${ops.queuedRequestCount} queued / ${ops.activeRequestCount} active / ${ops.scheduledDeleteCount} scheduled delete`
      : 'GDPR operations endpoint is missing',
    guardrail: 'Privacy exports and deletes should stay below the manual review guardrail.',
    key: 'gdpr',
    owner: 'Privacy',
    saturationPct,
    signal: `${formatPct(saturationPct)} GDPR workload saturation`,
    status: operationalGuardrailStatus(pressure, gdprWorkloadLimit),
    title: 'GDPR workload',
  }
}

function outboxQuotaLane(input: TenantQuotaSaturationInput): TenantQuotaLane {
  const dead = input.deadDeliveryCount
  const retryable = input.retryableDeadDeliveries
  const inflight = input.inflightDeadDeliveries
  const saturationPct = dead !== undefined ? pct(dead, outboxDeadDeliveryLimit) : null

  return {
    actionHref: '/administration/dead-deliveries',
    actionLabel: 'Open dead deliveries',
    capacityLabel: `${outboxDeadDeliveryLimit} dead deliveries before saturation`,
    consumptionLabel:
      dead !== undefined ? `${dead} dead deliveries` : 'outbox dead-letter count unknown',
    evidence:
      dead !== undefined && retryable !== undefined && inflight !== undefined
        ? `${retryable} retryable / ${inflight} in-flight / ${dead} dead`
        : 'outbox dead-delivery endpoint is missing',
    guardrail:
      'Dead-letter pressure must be cleared before it hides customer notification failures.',
    key: 'outbox',
    owner: 'Delivery',
    saturationPct,
    signal: `${formatPct(saturationPct)} dead-letter saturation`,
    status: outboxStatus(dead),
    title: 'Outbox dead-letter capacity',
  }
}

function ingestStatus(
  total: number | undefined,
  quota: number | undefined,
  unboundedKeys: number | undefined,
): TenantQuotaSaturationStatus {
  if (total === undefined || quota === undefined || quota <= 0 || unboundedKeys === undefined) {
    return 'needs_data'
  }
  const usagePct = pct(total, quota)
  if (usagePct >= 100) return 'saturated'
  if (usagePct >= 80 || unboundedKeys > 0) return 'watch'
  return 'healthy'
}

function providerErrorStatus(
  calls: number | undefined,
  errors: number | undefined,
): TenantQuotaSaturationStatus {
  if (calls === undefined || errors === undefined) return 'needs_data'
  if (calls === 0) return errors > 0 ? 'saturated' : 'healthy'
  const errorPct = pct(errors, calls)
  if (errorPct >= 10) return 'saturated'
  return errors > 0 ? 'watch' : 'healthy'
}

function mcpLimitStatus(
  activeClients: number | undefined,
  unboundedClients: number | undefined,
): TenantQuotaSaturationStatus {
  if (activeClients === undefined || unboundedClients === undefined) return 'needs_data'
  if (activeClients === 0) return 'needs_data'
  if (unboundedClients === activeClients) return 'saturated'
  return unboundedClients > 0 ? 'watch' : 'healthy'
}

function operationalGuardrailStatus(
  pressure: number | undefined,
  limit: number,
): TenantQuotaSaturationStatus {
  if (pressure === undefined) return 'needs_data'
  const saturationPct = pct(pressure, limit)
  if (saturationPct >= 100) return 'saturated'
  if (saturationPct >= 80) return 'watch'
  return 'healthy'
}

function outboxStatus(dead: number | undefined): TenantQuotaSaturationStatus {
  if (dead === undefined) return 'needs_data'
  if (dead >= outboxDeadDeliveryLimit) return 'saturated'
  return dead > 0 ? 'watch' : 'healthy'
}

function quotaWindowLabel(input: TenantQuotaSaturationInput) {
  if (input.usage) {
    /* v8 ignore next -- @preserve: usage window labels are optional display evidence. */
    return `${input.usage.periodStart || 'unknown'} -> ${input.usage.periodEnd || 'unknown'}`
  }
  if (input.llmUsage) {
    /* v8 ignore next -- @preserve: LLM usage window labels are optional display evidence. */
    return `${input.llmUsage.periodStart || 'unknown'} -> ${input.llmUsage.periodEnd || 'unknown'}`
  }
  return 'current quota window'
}

function quotaSummary(totals: TenantQuotaSaturation['totals']) {
  if (totals.saturated > 0) return `${totals.saturated} quota lanes are saturated`
  if (totals.needs_data > 0) return `${totals.needs_data} quota lanes need data`
  if (totals.watch > 0) return `${totals.watch} quota lanes need attention`
  return 'tenant quota boundaries are healthy'
}

function summaryStatus(totals: TenantQuotaSaturation['totals']) {
  if (totals.saturated > 0) return 'saturated'
  if (totals.needs_data > 0) return 'needs data'
  if (totals.watch > 0) return 'watch'
  return 'healthy'
}

function pct(value: number, total: number) {
  /* v8 ignore next -- @preserve: quota callers pass positive denominators or withhold saturation when capacity is unset. */
  if (total <= 0) return 0
  return Math.round((value / total) * 1000) / 10
}

function formatPct(value: number | null) {
  if (value === null) return 'unknown'
  return `${trimTrailingZeros(value)}%`
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value)
}

function parseCount(value: string | undefined) {
  if (value === undefined || value.trim() === '') return undefined
  const parsed = Number.parseInt(value, 10)
  return Number.isFinite(parsed) ? parsed : undefined
}

function numberOrZero(value: number | null | undefined) {
  return value ?? 0
}

function trimTrailingZeros(value: number) {
  return value.toFixed(1).replace(/\.0$/, '')
}
