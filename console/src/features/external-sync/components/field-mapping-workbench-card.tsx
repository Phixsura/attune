import {
  ArrowUpRight,
  ClipboardCheck,
  FileSearch,
  GitCompareArrows,
  GitPullRequestArrow,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  SlidersHorizontal,
  TableProperties,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  FieldMappingWorkbench,
  FieldMappingWorkbenchLane,
  FieldMappingWorkbenchLaneKey,
  FieldMappingWorkbenchRow,
  FieldMappingWorkbenchRowStatus,
  FieldMappingWorkbenchStatus,
} from '../field-mapping-workbench'

export function FieldMappingWorkbenchCard({ workbench }: { workbench: FieldMappingWorkbench }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="external-sync-field-mapping-workbench"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-background py-0 shadow-none"
    >
      <CardHeader className="border-b border-border/55 bg-muted/20 px-5 py-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.05rem] tracking-tight">
              <TableProperties className="size-4 text-muted-foreground" />
              {t('external_sync.field_mapping_workbench.title', 'Field mapping workbench')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-2xl text-sm leading-[1.35rem]">
              {t(
                'external_sync.field_mapping_workbench.description',
                'Schema drift, required fields, status mapping, preview safety, and rollback evidence for the selected connector mapping.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <FieldMappingTotal
              label={t('external_sync.field_mapping_workbench.verified', 'Verified')}
              tone="verified"
              value={workbench.totals.verified}
            />
            <FieldMappingTotal
              label={t('external_sync.field_mapping_workbench.watch', 'Watch')}
              tone="watch"
              value={workbench.totals.watch}
            />
            <FieldMappingTotal
              label={t('external_sync.field_mapping_workbench.blocked', 'Blocked')}
              tone="blocked"
              value={workbench.totals.blocked}
            />
            <FieldMappingTotal
              label={t('external_sync.field_mapping_workbench.needs_data', 'Needs data')}
              tone="needs_data"
              value={workbench.totals.needs_data}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-muted/10 px-3 py-3 md:grid-cols-2">
          <FieldMappingFact
            label={t('external_sync.field_mapping_workbench.fingerprint', 'Mapping fingerprint')}
            value={workbench.fingerprint}
          />
          <FieldMappingFact
            label={t('external_sync.field_mapping_workbench.summary', 'Mapping decision')}
            value={workbench.summary}
          />
        </div>
        <div className="grid gap-3 lg:grid-cols-5">
          {workbench.lanes.map((lane) => (
            <FieldMappingLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
        <div className="overflow-hidden rounded-[1rem] border border-border/60">
          <div className="border-b border-border/60 bg-muted/20 px-3 py-2 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            {t('external_sync.field_mapping_workbench.matrix', 'Mapping matrix')}
          </div>
          <div className="divide-y divide-border/50">
            {workbench.mappingRows.map((row) => (
              <FieldMappingRowView key={row.localField} row={row} />
            ))}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function FieldMappingTotal({
  label,
  tone,
  value,
}: {
  label: string
  tone: FieldMappingWorkbenchStatus
  value: number
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', fieldMappingSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function FieldMappingLaneCard({ lane }: { lane: FieldMappingWorkbenchLane }) {
  const { t } = useTranslation()
  const Icon = fieldMappingLaneIcon(lane.key)
  return (
    <div
      data-testid={`external-sync-field-mapping-workbench-${lane.key}`}
      className="flex min-w-0 flex-col rounded-[0.95rem] border border-border/60 bg-background px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="text-sm font-semibold leading-5 text-foreground">
              {t(`external_sync.field_mapping_workbench.lanes.${lane.key}.title`, lane.title)}
            </div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {t(`external_sync.field_mapping_workbench.lanes.${lane.key}.owner`, lane.owner)}
          </div>
        </div>
        <FieldMappingStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <FieldMappingFact
          label={t('external_sync.field_mapping_workbench.signal', 'Signal')}
          value={lane.signal}
        />
        <FieldMappingFact
          label={t('external_sync.field_mapping_workbench.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <FieldMappingFact
          label={t('external_sync.field_mapping_workbench.detail', 'Detail')}
          value={lane.detail}
        />
      </div>
      <div className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground">
        <span className="min-w-0 truncate">
          {t(`external_sync.field_mapping_workbench.lanes.${lane.key}.action`, lane.actionLabel)}
        </span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </div>
    </div>
  )
}

function FieldMappingRowView({ row }: { row: FieldMappingWorkbenchRow }) {
  const { t } = useTranslation()
  return (
    <div
      data-testid={`external-sync-field-mapping-row-${row.localField}`}
      className="grid gap-3 px-3 py-3 text-sm md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]"
    >
      <div className="min-w-0">
        <div className="text-xs font-medium text-muted-foreground">
          {t('external_sync.field_mapping_workbench.local_field', 'Local field')}
        </div>
        <div className="mt-1 truncate font-mono text-foreground">{row.localField}</div>
      </div>
      <div className="min-w-0">
        <div className="text-xs font-medium text-muted-foreground">
          {t('external_sync.field_mapping_workbench.provider_field', 'Provider field')}
        </div>
        <div className="mt-1 truncate font-mono text-foreground">{row.providerField}</div>
        <div className="mt-1 text-xs text-muted-foreground">
          {t('external_sync.field_mapping_workbench.suggestion', {
            defaultValue: 'Suggestion {{value}}',
            value: row.suggestion,
          })}
        </div>
      </div>
      <div className="flex items-start justify-between gap-2 md:justify-end">
        <div className="min-w-0 text-xs leading-5 text-muted-foreground">{row.evidence}</div>
        <FieldMappingRowBadge required={row.required} status={row.status} />
      </div>
    </div>
  )
}

function FieldMappingFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function FieldMappingStatusBadge({ status }: { status: FieldMappingWorkbenchStatus }) {
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
        fieldMappingBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`external_sync.field_mapping_workbench.status.${status}`, status)}
    </span>
  )
}

function FieldMappingRowBadge({
  required,
  status,
}: {
  required: boolean
  status: FieldMappingWorkbenchRowStatus
}) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'inline-flex shrink-0 rounded-full border px-2 py-0.5 text-xs font-medium',
        fieldMappingRowBadgeClass(status),
      )}
    >
      {t(`external_sync.field_mapping_workbench.row_status.${status}`, status)}
      {required ? ` · ${t('external_sync.field_mapping_workbench.required', 'required')}` : ''}
    </span>
  )
}

function fieldMappingLaneIcon(key: FieldMappingWorkbenchLaneKey) {
  switch (key) {
    case 'schema_diff':
      return GitCompareArrows
    case 'required_mapping':
      return TableProperties
    case 'status_mapping':
      return GitPullRequestArrow
    case 'preview_backfill':
      return FileSearch
    case 'rollback_recovery':
      return RotateCcw
    default:
      return TableProperties
  }
}

function fieldMappingBadgeClass(status: FieldMappingWorkbenchStatus) {
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

function fieldMappingSurfaceClass(status: FieldMappingWorkbenchStatus) {
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

function fieldMappingRowBadgeClass(status: FieldMappingWorkbenchRowStatus) {
  switch (status) {
    case 'mapped':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'suggested':
      return 'border-sky-200 bg-sky-50 text-sky-800'
    case 'drift':
      return 'border-red-200 bg-red-50 text-red-700'
    case 'missing':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}
