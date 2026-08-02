import {
  ArrowUpRight,
  ClipboardCheck,
  FileJson,
  GitCompareArrows,
  LifeBuoy,
  Puzzle,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  Webhook,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  ConnectorConformanceGate,
  ConnectorConformanceLane,
  ConnectorConformanceLaneKey,
  ConnectorConformanceStatus,
} from '../connector-conformance-gate'

export function ConnectorConformanceGateCard({ gate }: { gate: ConnectorConformanceGate }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="external-sync-connector-conformance-gate"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(248,250,252,0.985))] py-0 shadow-none"
    >
      <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(248,250,252,0.86),rgba(255,255,255,0.94))] px-5 py-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.05rem] tracking-tight">
              <Puzzle className="size-4 text-muted-foreground" />
              {t('external_sync.connector_conformance.title', 'Connector conformance gate')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-2xl text-sm leading-[1.35rem]">
              {t(
                'external_sync.connector_conformance.description',
                'Connector install metadata, fixture replay, webhook signatures, field mappings, and recovery paths stay verifiable.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <ConnectorConformanceTotal
              label={t('external_sync.connector_conformance.verified', 'Verified')}
              tone="verified"
              value={gate.totals.verified}
            />
            <ConnectorConformanceTotal
              label={t('external_sync.connector_conformance.watch', 'Watch')}
              tone="watch"
              value={gate.totals.watch}
            />
            <ConnectorConformanceTotal
              label={t('external_sync.connector_conformance.blocked', 'Blocked')}
              tone="blocked"
              value={gate.totals.blocked}
            />
            <ConnectorConformanceTotal
              label={t('external_sync.connector_conformance.needs_data', 'Needs data')}
              tone="needs_data"
              value={gate.totals.needs_data}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-background/80 px-3 py-3 md:grid-cols-2">
          <ConnectorConformanceFact
            label={t('external_sync.connector_conformance.fingerprint', 'Conformance fingerprint')}
            value={gate.fingerprint}
          />
          <ConnectorConformanceFact
            label={t('external_sync.connector_conformance.summary', 'Conformance decision')}
            value={gate.summary}
          />
        </div>
        <div className="grid gap-3 lg:grid-cols-5">
          {gate.lanes.map((lane) => (
            <ConnectorConformanceLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function ConnectorConformanceTotal({
  label,
  tone,
  value,
}: {
  label: string
  tone: ConnectorConformanceStatus
  value: number
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', connectorConformanceSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function ConnectorConformanceLaneCard({ lane }: { lane: ConnectorConformanceLane }) {
  const { t } = useTranslation()
  const Icon = connectorConformanceIcon(lane.key)
  return (
    <div
      data-testid={`external-sync-connector-conformance-gate-${lane.key}`}
      className="flex min-w-0 flex-col rounded-[0.95rem] border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="text-sm font-semibold leading-5 text-foreground">
              {t(`external_sync.connector_conformance.lanes.${lane.key}.title`, lane.title)}
            </div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {t(`external_sync.connector_conformance.lanes.${lane.key}.owner`, lane.owner)}
          </div>
        </div>
        <ConnectorConformanceStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <ConnectorConformanceFact
          label={t('external_sync.connector_conformance.signal', 'Signal')}
          value={lane.signal}
        />
        <ConnectorConformanceFact
          label={t('external_sync.connector_conformance.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <ConnectorConformanceFact
          label={t('external_sync.connector_conformance.guardrail', 'Guardrail')}
          value={lane.guardrail}
        />
      </div>
      <a
        href={lane.actionHref}
        className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <span className="min-w-0 truncate">
          {t(`external_sync.connector_conformance.lanes.${lane.key}.action`, lane.actionLabel)}
        </span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function ConnectorConformanceFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function ConnectorConformanceStatusBadge({ status }: { status: ConnectorConformanceStatus }) {
  const { t } = useTranslation()
  const Icon =
    status === 'blocked'
      ? ShieldAlert
      : status === 'watch'
        ? SlidersHorizontal
        : status === 'needs_data'
          ? ClipboardCheck
          : ShieldCheck
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        connectorConformanceBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`external_sync.connector_conformance.status.${status}`, status)}
    </span>
  )
}

function connectorConformanceIcon(key: ConnectorConformanceLaneKey) {
  switch (key) {
    case 'connector_manifest':
      return Puzzle
    case 'fixture_replay':
      return FileJson
    case 'webhook_signature':
      return Webhook
    case 'field_mapping':
      return GitCompareArrows
    case 'error_recovery':
      return LifeBuoy
    default:
      return RefreshCw
  }
}

function connectorConformanceBadgeClass(status: ConnectorConformanceStatus) {
  switch (status) {
    case 'verified':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'watch':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    case 'blocked':
      return 'border-red-200 bg-red-50 text-red-700'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50 text-sky-800'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}

function connectorConformanceSurfaceClass(status: ConnectorConformanceStatus) {
  switch (status) {
    case 'verified':
      return 'border-emerald-200 bg-emerald-50/70'
    case 'watch':
      return 'border-amber-200 bg-amber-50/70'
    case 'blocked':
      return 'border-red-200 bg-red-50/70'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50/70'
    default:
      return 'border-border bg-muted/20'
  }
}
