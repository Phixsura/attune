import {
  Activity,
  AlertTriangle,
  ArrowUpRight,
  DatabaseZap,
  Gauge,
  GitBranch,
  RadioTower,
  ShieldAlert,
  ShieldCheck,
  Truck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  PipelineSloKey,
  PipelineSloLane,
  PipelineSloLedger,
  PipelineSloStatus,
} from '../pipeline-slo-ledger'

export function PipelineSloLedgerCard({ ledger }: { ledger: PipelineSloLedger }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="reliability-pipeline-slo-ledger"
      className="border-border/60 bg-background/95 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <Gauge className="size-4 text-muted-foreground" />
              {t('reliability.pipeline_slo.title', 'Pipeline SLO ledger')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-3xl">
              {t(
                'reliability.pipeline_slo.description',
                'Ingest, enrich, outbox, and sync pipelines carry objective, burn signal, owner, runbook, and release-gate evidence.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <PipelineTotal
              label={t('reliability.pipeline_slo.ready', 'Ready')}
              value={ledger.totals.ready}
              tone="ready"
            />
            <PipelineTotal
              label={t('reliability.pipeline_slo.watch', 'Watch')}
              value={ledger.totals.watch}
              tone="watch"
            />
            <PipelineTotal
              label={t('reliability.pipeline_slo.blocked', 'Blocked')}
              value={ledger.totals.blocked}
              tone="blocked"
            />
            <PipelineTotal
              label={t('reliability.pipeline_slo.needs_data', 'Needs data')}
              value={ledger.totals.needs_data}
              tone="needs_data"
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-4">
        <div className="grid gap-3 rounded-md border border-border/60 bg-muted/10 px-3 py-3 md:grid-cols-2">
          <PipelineFact
            label={t('reliability.pipeline_slo.fingerprint', 'Pipeline fingerprint')}
            value={ledger.fingerprint}
          />
          <PipelineFact
            label={t('reliability.pipeline_slo.summary', 'Pipeline decision')}
            value={ledger.summary}
          />
        </div>
        <div className="grid gap-3 xl:grid-cols-4">
          {ledger.lanes.map((lane) => (
            <PipelineLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function PipelineTotal({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: PipelineSloStatus
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', pipelineSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function PipelineLaneCard({ lane }: { lane: PipelineSloLane }) {
  const { t } = useTranslation()
  const Icon = pipelineIcon(lane.key)
  return (
    <div
      data-testid={`reliability-pipeline-slo-${lane.key}`}
      className="flex min-w-0 flex-col rounded-md border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="truncate text-sm font-semibold text-foreground">{lane.title}</div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {lane.owner} / {lane.escalation}
          </div>
        </div>
        <PipelineStatusBadge status={lane.status} />
      </div>

      <div className="mt-3 space-y-2">
        <PipelineFact
          label={t('reliability.pipeline_slo.objective', 'Objective')}
          value={lane.objective}
        />
        <PipelineFact
          label={t('reliability.pipeline_slo.burn_signal', 'Burn signal')}
          value={lane.burnSignal}
        />
        <PipelineFact
          label={t('reliability.pipeline_slo.release_gate', 'Release gate')}
          value={lane.releaseGate}
        />
        <PipelineFact
          label={t('reliability.pipeline_slo.evidence', 'Evidence')}
          value={lane.evidence}
        />
      </div>

      <div className="mt-3 grid gap-2">
        <a
          href={lane.actionHref}
          className="inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
        >
          <span className="min-w-0 truncate">{lane.actionLabel}</span>
          <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
        </a>
        <a
          href={lane.runbookHref}
          target="_blank"
          rel="noreferrer"
          className="inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
        >
          <span className="min-w-0 truncate">
            {t('reliability.pipeline_slo.open_runbook', 'Open runbook')}
          </span>
          <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
        </a>
      </div>
    </div>
  )
}

function PipelineFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function PipelineStatusBadge({ status }: { status: PipelineSloStatus }) {
  const { t } = useTranslation()
  const Icon =
    status === 'blocked'
      ? ShieldAlert
      : status === 'watch'
        ? AlertTriangle
        : status === 'needs_data'
          ? DatabaseZap
          : ShieldCheck
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        pipelineBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`reliability.pipeline_slo.status.${status}`, status)}
    </span>
  )
}

function pipelineIcon(key: PipelineSloKey) {
  switch (key) {
    case 'ingest':
      return RadioTower
    case 'enrich':
      return Activity
    case 'outbox':
      return Truck
    case 'sync':
      return GitBranch
    default:
      return Gauge
  }
}

function pipelineBadgeClass(status: PipelineSloStatus) {
  switch (status) {
    case 'ready':
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

function pipelineSurfaceClass(status: PipelineSloStatus) {
  switch (status) {
    case 'ready':
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
