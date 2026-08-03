import {
  AlertTriangle,
  ArchiveRestore,
  ArrowUpRight,
  BookOpenCheck,
  DatabaseZap,
  GitBranch,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  Wrench,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  BackupRestoreDrill,
  BackupRestoreDrillStatus,
  BackupRestoreLane,
  BackupRestoreLaneKey,
} from '../backup-restore-drill'

export function BackupRestoreDrillCard({ drill }: { drill: BackupRestoreDrill }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="reliability-backup-restore-drill"
      className="border-border/60 bg-background/95 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <ArchiveRestore className="size-4 text-muted-foreground" />
              {t('reliability.backup_restore.title', 'Backup / restore drill evidence')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-3xl">
              {t(
                'reliability.backup_restore.description',
                'Backup freshness, restore execution, migration readiness, ownership, and remediation evidence stay in one recovery proof.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <BackupRestoreTotal
              label={t('reliability.backup_restore.verified', 'Verified')}
              value={drill.totals.verified}
              tone="verified"
            />
            <BackupRestoreTotal
              label={t('reliability.backup_restore.watch', 'Watch')}
              value={drill.totals.watch}
              tone="watch"
            />
            <BackupRestoreTotal
              label={t('reliability.backup_restore.blocked', 'Blocked')}
              value={drill.totals.blocked}
              tone="blocked"
            />
            <BackupRestoreTotal
              label={t('reliability.backup_restore.needs_data', 'Needs data')}
              value={drill.totals.needs_data}
              tone="needs_data"
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-4">
        <div className="grid gap-3 rounded-md border border-border/60 bg-muted/10 px-3 py-3 md:grid-cols-2">
          <BackupRestoreFact
            label={t('reliability.backup_restore.fingerprint', 'Recovery fingerprint')}
            value={drill.fingerprint}
          />
          <BackupRestoreFact
            label={t('reliability.backup_restore.summary', 'Recovery decision')}
            value={drill.summary}
          />
        </div>
        <div className="grid gap-3 xl:grid-cols-5">
          {drill.lanes.map((lane) => (
            <BackupRestoreLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function BackupRestoreTotal({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: BackupRestoreDrillStatus
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', backupRestoreSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function BackupRestoreLaneCard({ lane }: { lane: BackupRestoreLane }) {
  const { t } = useTranslation()
  const Icon = backupRestoreIcon(lane.key)
  return (
    <div
      data-testid={`reliability-backup-restore-${lane.key}`}
      className="flex min-w-0 flex-col rounded-md border border-border/60 bg-background/80 px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="truncate text-sm font-semibold text-foreground">{lane.title}</div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">{lane.owner}</div>
        </div>
        <BackupRestoreStatusBadge status={lane.status} />
      </div>

      <div className="mt-3 space-y-2">
        <BackupRestoreFact
          label={t('reliability.backup_restore.signal', 'Signal')}
          value={lane.signal}
        />
        <BackupRestoreFact
          label={t('reliability.backup_restore.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <BackupRestoreFact
          label={t('reliability.backup_restore.guardrail', 'Guardrail')}
          value={lane.guardrail}
        />
      </div>

      <a
        href={lane.actionHref}
        className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <span className="min-w-0 truncate">{lane.actionLabel}</span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function BackupRestoreFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function BackupRestoreStatusBadge({ status }: { status: BackupRestoreDrillStatus }) {
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
        backupRestoreBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`reliability.backup_restore.status.${status}`, status)}
    </span>
  )
}

function backupRestoreIcon(key: BackupRestoreLaneKey) {
  switch (key) {
    case 'backup_freshness':
      return ArchiveRestore
    case 'restore_execution':
      return RotateCcw
    case 'migration_readiness':
      return GitBranch
    case 'runbook_ownership':
      return BookOpenCheck
    case 'remediation_path':
      return Wrench
    default:
      return ArchiveRestore
  }
}

function backupRestoreBadgeClass(status: BackupRestoreDrillStatus) {
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

function backupRestoreSurfaceClass(status: BackupRestoreDrillStatus) {
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
