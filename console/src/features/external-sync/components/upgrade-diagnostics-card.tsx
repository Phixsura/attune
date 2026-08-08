import {
  ArrowUpRight,
  ClipboardCheck,
  FileJson,
  GitCompareArrows,
  HeartPulse,
  KeyRound,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  Stethoscope,
  Webhook,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  UpgradeDiagnostics,
  UpgradeDiagnosticsLane,
  UpgradeDiagnosticsLaneKey,
  UpgradeDiagnosticsRow,
  UpgradeDiagnosticsStatus,
} from '../upgrade-diagnostics'

export function UpgradeDiagnosticsCard({ diagnostics }: { diagnostics: UpgradeDiagnostics }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="external-sync-upgrade-diagnostics"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(248,250,252,0.985))] py-0 shadow-none"
    >
      <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(248,250,252,0.86),rgba(255,255,255,0.94))] px-5 py-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.05rem] tracking-tight">
              <Stethoscope className="size-4 text-muted-foreground" />
              {t('external_sync.upgrade_diagnostics.title', 'Upgrade diagnostics')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-2xl text-sm leading-[1.35rem]">
              {t(
                'external_sync.upgrade_diagnostics.description',
                'Install, permission, schema drift, webhook, replay, and version compatibility evidence become one executable upgrade diagnosis.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <UpgradeDiagnosticsTotal
              label={t('external_sync.upgrade_diagnostics.verified', 'Verified')}
              tone="verified"
              value={diagnostics.totals.verified}
            />
            <UpgradeDiagnosticsTotal
              label={t('external_sync.upgrade_diagnostics.watch', 'Watch')}
              tone="watch"
              value={diagnostics.totals.watch}
            />
            <UpgradeDiagnosticsTotal
              label={t('external_sync.upgrade_diagnostics.blocked', 'Blocked')}
              tone="blocked"
              value={diagnostics.totals.blocked}
            />
            <UpgradeDiagnosticsTotal
              label={t('external_sync.upgrade_diagnostics.needs_data', 'Needs data')}
              tone="needs_data"
              value={diagnostics.totals.needs_data}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-background/80 px-3 py-3 md:grid-cols-2">
          <UpgradeDiagnosticsFact
            label={t('external_sync.upgrade_diagnostics.fingerprint', 'Diagnostics fingerprint')}
            value={diagnostics.fingerprint}
          />
          <UpgradeDiagnosticsFact
            label={t('external_sync.upgrade_diagnostics.summary', 'Diagnostics decision')}
            value={diagnostics.summary}
          />
        </div>
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-6">
          {diagnostics.lanes.map((lane) => (
            <UpgradeDiagnosticsLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
        <div className="space-y-3">
          <div className="flex items-center gap-2 text-sm font-semibold text-foreground">
            <ClipboardCheck className="size-4 text-muted-foreground" />
            {t('external_sync.upgrade_diagnostics.diagnostic_matrix', 'Diagnostic matrix')}
          </div>
          <div className="grid gap-3 lg:grid-cols-3">
            {diagnostics.rows.map((row) => (
              <UpgradeDiagnosticsRowCard key={row.id} row={row} />
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function UpgradeDiagnosticsTotal({
  label,
  tone,
  value,
}: {
  label: string
  tone: UpgradeDiagnosticsStatus
  value: number
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', upgradeDiagnosticsSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function UpgradeDiagnosticsLaneCard({ lane }: { lane: UpgradeDiagnosticsLane }) {
  const { t } = useTranslation()
  const Icon = upgradeDiagnosticsIcon(lane.key)
  return (
    <div
      data-testid={`external-sync-upgrade-diagnostics-${lane.key}`}
      className="flex min-w-0 flex-col rounded-[0.95rem] border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="text-sm font-semibold leading-5 text-foreground">
              {t(`external_sync.upgrade_diagnostics.lanes.${lane.key}.title`, lane.title)}
            </div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {t(`external_sync.upgrade_diagnostics.lanes.${lane.key}.owner`, lane.owner)}
          </div>
        </div>
        <UpgradeDiagnosticsStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <UpgradeDiagnosticsFact
          label={t('external_sync.upgrade_diagnostics.signal', 'Signal')}
          value={lane.signal}
        />
        <UpgradeDiagnosticsFact
          label={t('external_sync.upgrade_diagnostics.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <UpgradeDiagnosticsFact
          label={t('external_sync.upgrade_diagnostics.detail', 'Detail')}
          value={lane.detail}
        />
      </div>
      <a
        href="https://github.com/Phixsura/attune/tree/main/integrations/upgrade-diagnostics"
        className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <span className="min-w-0 truncate">
          {t(`external_sync.upgrade_diagnostics.lanes.${lane.key}.action`, lane.actionLabel)}
        </span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function UpgradeDiagnosticsRowCard({ row }: { row: UpgradeDiagnosticsRow }) {
  const { t } = useTranslation()
  return (
    <div
      data-testid={`external-sync-upgrade-diagnostics-row-${row.id}`}
      className="min-w-0 rounded-[0.95rem] border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="text-sm font-semibold leading-5 text-foreground">
            {t(`external_sync.upgrade_diagnostics.lanes.${row.id}.title`, row.title)}
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {t(`external_sync.upgrade_diagnostics.lanes.${row.id}.owner`, row.owner)}
          </div>
        </div>
        <UpgradeDiagnosticsStatusBadge status={row.status} />
      </div>
      <div className="mt-3 grid gap-2">
        <UpgradeDiagnosticsFact
          label={t('external_sync.upgrade_diagnostics.signal', 'Signal')}
          value={row.signal}
        />
        <UpgradeDiagnosticsFact
          label={t('external_sync.upgrade_diagnostics.evidence', 'Evidence')}
          value={row.evidence}
        />
        <UpgradeDiagnosticsFact
          label={t('external_sync.upgrade_diagnostics.next_action', 'Next action')}
          value={t(`external_sync.upgrade_diagnostics.lanes.${row.id}.action`, row.action)}
        />
      </div>
    </div>
  )
}

function UpgradeDiagnosticsFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function UpgradeDiagnosticsStatusBadge({ status }: { status: UpgradeDiagnosticsStatus }) {
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
        upgradeDiagnosticsBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`external_sync.upgrade_diagnostics.status.${status}`, status)}
    </span>
  )
}

function upgradeDiagnosticsIcon(key: UpgradeDiagnosticsLaneKey) {
  switch (key) {
    case 'install_health':
      return HeartPulse
    case 'permission_boundary':
      return KeyRound
    case 'schema_drift':
      return GitCompareArrows
    case 'webhook_readiness':
      return Webhook
    case 'fixture_replay':
      return FileJson
    case 'version_compatibility':
      return RotateCcw
    default:
      return Stethoscope
  }
}

function upgradeDiagnosticsBadgeClass(status: UpgradeDiagnosticsStatus) {
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

function upgradeDiagnosticsSurfaceClass(status: UpgradeDiagnosticsStatus) {
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
