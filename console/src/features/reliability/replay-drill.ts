import { type ReliabilityCatalogEntryValue, reliabilityCatalog } from './reliability-catalog'

export type ReplayDrillStatus = 'ready' | 'attention' | 'blocked'

export type ReplayDrillLane = {
  actionLabel: string
  entryHref: string
  entryLabel: string
  escalation: string
  evidenceLabel: string
  key: ReliabilityCatalogEntryValue['key']
  lens: string
  owner: string
  signal: string
  status: ReplayDrillStatus
  title: string
}

export type ReplayDrill = {
  lanes: ReplayDrillLane[]
  totals: {
    attention: number
    blocked: number
    ready: number
    total: number
  }
}

export type ReplayDrillInput = {
  activeGdpr: number
  dashboardHref: string
  deadDeliveryCount: number
  inflightDeadDeliveries: number
  queuedGdpr: number
  readinessStatus?: string
  recoveryStatus?: string
  releaseLifecycleState?: string
  retryableDeadDeliveries: number
  scheduledDeletes: number
  tenantName: string
}

type ReplayDrillBlueprint = {
  actionLabel: string
  entryHref: string
  entryLabel: string
  evidenceLabel: string
  lens: string
}

const replayDrillBlueprints: Record<ReliabilityCatalogEntryValue['key'], ReplayDrillBlueprint> = {
  apikey_access: {
    actionLabel: 'Rotate, disable, or validate the affected key set',
    entryHref: '/integrations/api-keys',
    entryLabel: 'API key inventory',
    evidenceLabel: 'access-denial query, audit log, and key state snapshot',
    lens: 'active key / denial class / tenant',
  },
  enrichment_latency: {
    actionLabel: 'Replay failed enrichment rows from the terminal workbench',
    entryHref: '/feedback/terminal-failures',
    entryLabel: 'Terminal enrichment failures',
    evidenceLabel: 'retry_enrichment audit event, model snapshot, and signal trace',
    lens: 'tenant / model / reason class / feedback id',
  },
  gdpr_job: {
    actionLabel: 'Backfill export/delete jobs from the GDPR operations queue',
    entryHref: '/administration/gdpr',
    entryLabel: 'GDPR operations',
    evidenceLabel: 'job state, export expiry, deletion plan, and audit residue',
    lens: 'tenant / request type / job state',
  },
  ingest_service: {
    actionLabel: 'Re-submit idempotent source events and compare feedback rows',
    entryHref: '/feedback',
    entryLabel: 'Feedback intake',
    evidenceLabel: 'idempotency key, source event id, signal trace, and feedback row',
    lens: 'tenant / source / result / signal trace',
  },
  mcp_tool: {
    actionLabel: 'Replay a tool call through the MCP client policy surface',
    entryHref: '/mcp-clients',
    entryLabel: 'MCP client policy',
    evidenceLabel: 'tool policy, denial class, session grant, and audit event',
    lens: 'tenant / client / tool / result',
  },
  oidc_login: {
    actionLabel: 'Run the break-glass and SSO sign-in recovery drill',
    entryHref: '/administration/security',
    entryLabel: 'Security settings',
    evidenceLabel: 'auth mode, break-glass state, and sign-in failure class',
    lens: 'auth mode / identity provider / result',
  },
  outbox_delivery: {
    actionLabel: 'Retry dead deliveries and verify destination acceptance',
    entryHref: '/administration/dead-deliveries',
    entryLabel: 'Dead deliveries',
    evidenceLabel: 'outbox.retry audit row, delivery id, destination, and response',
    lens: 'destination type / failure kind / delivery id',
  },
}

export function buildReplayDrill(input: ReplayDrillInput): ReplayDrill {
  const lanes = reliabilityCatalog.map((entry) => buildReplayDrillLane(entry, input))
  return {
    lanes,
    totals: {
      attention: lanes.filter((lane) => lane.status === 'attention').length,
      blocked: lanes.filter((lane) => lane.status === 'blocked').length,
      ready: lanes.filter((lane) => lane.status === 'ready').length,
      total: lanes.length,
    },
  }
}

function buildReplayDrillLane(
  entry: ReliabilityCatalogEntryValue,
  input: ReplayDrillInput,
): ReplayDrillLane {
  const blueprint = replayDrillBlueprints[entry.key]
  return {
    actionLabel: blueprint.actionLabel,
    entryHref:
      entry.key === 'ingest_service' ? feedbackIntakeHref(input.tenantName) : blueprint.entryHref,
    entryLabel: blueprint.entryLabel,
    escalation: entry.escalation,
    evidenceLabel: blueprint.evidenceLabel,
    key: entry.key,
    lens: blueprint.lens,
    owner: entry.owner,
    signal: replayDrillSignal(entry.key, input),
    status: replayDrillStatus(entry.key, input),
    title: entry.title,
  }
}

function replayDrillStatus(
  key: ReliabilityCatalogEntryValue['key'],
  input: ReplayDrillInput,
): ReplayDrillStatus {
  if (input.releaseLifecycleState === 'blocked') {
    return 'blocked'
  }
  if (key === 'ingest_service' || key === 'enrichment_latency') {
    if (input.readinessStatus === 'fail') return 'blocked'
    if (input.readinessStatus === 'warn') return 'attention'
  }
  if (key === 'outbox_delivery' && input.deadDeliveryCount > 0) {
    return 'attention'
  }
  if (key === 'gdpr_job') {
    if (input.recoveryStatus === 'fail') return 'blocked'
    if (input.queuedGdpr + input.activeGdpr + input.scheduledDeletes > 0) return 'attention'
  }
  return 'ready'
}

function replayDrillSignal(
  key: ReliabilityCatalogEntryValue['key'],
  input: ReplayDrillInput,
): string {
  switch (key) {
    case 'outbox_delivery':
      return `${input.retryableDeadDeliveries} retryable / ${input.inflightDeadDeliveries} in-flight / ${input.deadDeliveryCount} dead`
    case 'gdpr_job':
      return `${input.queuedGdpr} queued / ${input.activeGdpr} active / ${input.scheduledDeletes} scheduled delete`
    case 'ingest_service':
      return `${input.tenantName} intake drill / ${input.readinessStatus ?? 'unknown'} readiness`
    case 'enrichment_latency':
      return `${input.tenantName} enrichment drill / ${input.readinessStatus ?? 'unknown'} readiness`
    default:
      return `${input.tenantName} replay drill`
  }
}

function feedbackIntakeHref(tenantName: string) {
  const accountKey = tenantName
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
  return accountKey ? `/feedback?account_key=${encodeURIComponent(accountKey)}` : '/feedback'
}
