import {
  AlertTriangle,
  ArrowUpRight,
  BellRing,
  GitBranch,
  HeartPulse,
  MessageSquareWarning,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  ReleaseHealthLedger,
  ReleaseHealthLedgerEntry,
  ReleaseHealthStatus,
} from '../release-health-ledger'

export function ReleaseHealthLedgerCard({ ledger }: { ledger: ReleaseHealthLedger }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="reliability-release-health-ledger"
      className="border-border/60 bg-background/95 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <HeartPulse className="size-4 text-muted-foreground" />
              {t('reliability.release_health_ledger.title', 'Release health correlation ledger')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-3xl">
              {t(
                'reliability.release_health_ledger.description',
                'Runtime version, lifecycle, restore drill, feedback pressure, and notification failures stay tied to the same release-health decision.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <LedgerTotal
              label={t('reliability.release_health_ledger.ready', 'Ready')}
              value={ledger.totals.ready}
              tone="ready"
            />
            <LedgerTotal
              label={t('reliability.release_health_ledger.attention', 'Attention')}
              value={ledger.totals.attention}
              tone="attention"
            />
            <LedgerTotal
              label={t('reliability.release_health_ledger.blocked', 'Blocked')}
              value={ledger.totals.blocked}
              tone="blocked"
            />
            <LedgerTotal
              label={t('reliability.release_health_ledger.needs_data', 'Needs data')}
              value={ledger.totals.needs_data}
              tone="needs_data"
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-4">
        <div className="grid gap-3 rounded-md border border-border/60 bg-muted/10 px-3 py-3 md:grid-cols-[minmax(0,0.8fr)_minmax(0,1fr)]">
          <ReleaseHealthFact
            label={t('reliability.release_health_ledger.fingerprint', 'Release fingerprint')}
            value={ledger.releaseFingerprint}
          />
          <ReleaseHealthFact
            label={t('reliability.release_health_ledger.summary', 'Release decision')}
            value={ledger.summary}
          />
        </div>
        <div className="grid gap-3 xl:grid-cols-5">
          {ledger.entries.map((entry) => (
            <ReleaseHealthLedgerLane key={entry.key} entry={entry} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function LedgerTotal({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: ReleaseHealthStatus
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', releaseHealthSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function ReleaseHealthLedgerLane({ entry }: { entry: ReleaseHealthLedgerEntry }) {
  const { t } = useTranslation()
  const Icon = releaseHealthEntryIcon(entry.key)
  return (
    <div
      data-testid={`reliability-release-health-${entry.key}`}
      className="flex min-w-0 flex-col rounded-md border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="truncate text-sm font-semibold text-foreground">{entry.title}</div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">{entry.owner}</div>
        </div>
        <ReleaseHealthStatusBadge status={entry.status} />
      </div>

      <div className="mt-3 space-y-2">
        <ReleaseHealthFact
          label={t('reliability.release_health_ledger.signal', 'Signal')}
          value={entry.signal}
        />
        <ReleaseHealthFact
          label={t('reliability.release_health_ledger.evidence', 'Evidence')}
          value={entry.evidence}
        />
      </div>

      <a
        href={entry.actionHref}
        target={entry.actionHref.startsWith('http') ? '_blank' : undefined}
        rel={entry.actionHref.startsWith('http') ? 'noreferrer' : undefined}
        className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <span className="min-w-0 truncate">{entry.actionLabel}</span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function ReleaseHealthFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function ReleaseHealthStatusBadge({ status }: { status: ReleaseHealthStatus }) {
  const { t } = useTranslation()
  const Icon =
    status === 'blocked'
      ? ShieldAlert
      : status === 'attention'
        ? AlertTriangle
        : status === 'needs_data'
          ? BellRing
          : ShieldCheck
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        releaseHealthBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {releaseHealthStatusLabel(status, t)}
    </span>
  )
}

function releaseHealthEntryIcon(entryKey: ReleaseHealthLedgerEntry['key']) {
  switch (entryKey) {
    case 'runtime_version':
      return GitBranch
    case 'lifecycle_gate':
      return ShieldCheck
    case 'restore_drill':
      return RotateCcw
    case 'feedback_pressure':
      return MessageSquareWarning
    case 'notification_failures':
      return BellRing
    default:
      return HeartPulse
  }
}

function releaseHealthStatusLabel(status: ReleaseHealthStatus, t: (key: string) => string) {
  switch (status) {
    case 'ready':
      return t('reliability.release_health_ledger.status.ready')
    case 'attention':
      return t('reliability.release_health_ledger.status.attention')
    case 'blocked':
      return t('reliability.release_health_ledger.status.blocked')
    case 'needs_data':
      return t('reliability.release_health_ledger.status.needs_data')
    default:
      return status
  }
}

function releaseHealthBadgeClass(status: ReleaseHealthStatus) {
  switch (status) {
    case 'ready':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'attention':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    case 'blocked':
      return 'border-red-200 bg-red-50 text-red-700'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50 text-sky-800'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}

function releaseHealthSurfaceClass(status: ReleaseHealthStatus) {
  switch (status) {
    case 'ready':
      return 'border-emerald-200 bg-emerald-50/70'
    case 'attention':
      return 'border-amber-200 bg-amber-50/70'
    case 'blocked':
      return 'border-red-200 bg-red-50/70'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50/70'
    default:
      return 'border-border bg-muted/20'
  }
}
