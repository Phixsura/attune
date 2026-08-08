import {
  ArrowRightLeft,
  ClipboardCheck,
  DatabaseZap,
  FileArchive,
  FileSpreadsheet,
  FileType2,
  GitCompareArrows,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  ShieldPlus,
  SlidersHorizontal,
  TableProperties,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  DeveloperImportExportDryRunRow,
  DeveloperImportExportLane,
  DeveloperImportExportLaneKey,
  DeveloperImportExportMappingRow,
  DeveloperImportExportStatus,
  DeveloperImportExportTemplate,
  DeveloperImportExportWorkbench,
} from '../developer-import-export-workbench'

export function DeveloperImportExportWorkbenchCard({
  workbench,
}: {
  workbench: DeveloperImportExportWorkbench
}) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="developer-import-export-workbench"
      className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-background py-0 shadow-none"
    >
      <CardHeader className="border-b border-border/55 bg-muted/20 px-5 py-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-[1.05rem] tracking-tight">
              <FileArchive className="size-4 text-muted-foreground" />
              {t('api_keys.import_export.title', 'Developer import/export workbench')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-2xl text-sm leading-[1.35rem]">
              {t(
                'api_keys.import_export.description',
                'CSV and JSON templates, schema preview, field mapping, dry-run diff, recovery, permissions, and audit evidence in one operator surface.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-4">
            <DeveloperImportExportTotal
              label={t('api_keys.import_export.verified', 'Verified')}
              tone="verified"
              value={workbench.totals.verified}
            />
            <DeveloperImportExportTotal
              label={t('api_keys.import_export.watch', 'Watch')}
              tone="watch"
              value={workbench.totals.watch}
            />
            <DeveloperImportExportTotal
              label={t('api_keys.import_export.blocked', 'Blocked')}
              tone="blocked"
              value={workbench.totals.blocked}
            />
            <DeveloperImportExportTotal
              label={t('api_keys.import_export.needs_data', 'Needs data')}
              tone="needs_data"
              value={workbench.totals.needs_data}
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 p-5">
        <div className="grid gap-3 rounded-[1rem] border border-border/60 bg-muted/10 px-3 py-3">
          <DeveloperImportExportFact
            label={t('api_keys.import_export.fingerprint', 'Workbench fingerprint')}
            value={workbench.fingerprint}
          />
          <DeveloperImportExportFact
            label={t('api_keys.import_export.summary', 'Workbench decision')}
            value={workbench.summary}
          />
        </div>
        <div className="grid gap-3">
          {workbench.lanes.map((lane) => (
            <DeveloperImportExportLaneCard key={lane.key} lane={lane} />
          ))}
        </div>
        <div className="grid gap-3 xl:grid-cols-2">
          <DeveloperImportExportTemplateList templates={workbench.templates} />
          <DeveloperImportExportDryRunList rows={workbench.dryRunRows} />
        </div>
        <DeveloperImportExportMappingMatrix rows={workbench.mappingRows} />
      </CardContent>
    </Card>
  )
}

function DeveloperImportExportTotal({
  label,
  tone,
  value,
}: {
  label: string
  tone: DeveloperImportExportStatus
  value: number
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', developerImportExportSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function DeveloperImportExportLaneCard({ lane }: { lane: DeveloperImportExportLane }) {
  const { t } = useTranslation()
  const Icon = developerImportExportIcon(lane.key)
  return (
    <div
      data-testid={`developer-import-export-workbench-${lane.key}`}
      className="flex min-w-0 flex-col rounded-[0.95rem] border border-border/60 bg-background px-3 py-3"
    >
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Icon className="size-4 shrink-0 text-muted-foreground" />
            <div className="truncate text-sm font-semibold text-foreground">
              {t(`api_keys.import_export.lanes.${lane.key}.title`, lane.title)}
            </div>
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {t(`api_keys.import_export.lanes.${lane.key}.owner`, lane.owner)}
          </div>
        </div>
        <DeveloperImportExportStatusBadge status={lane.status} />
      </div>
      <div className="mt-3 space-y-2">
        <DeveloperImportExportFact
          label={t('api_keys.import_export.signal', 'Signal')}
          value={lane.signal}
        />
        <DeveloperImportExportFact
          label={t('api_keys.import_export.evidence', 'Evidence')}
          value={lane.evidence}
        />
        <DeveloperImportExportFact
          label={t('api_keys.import_export.guardrail', 'Guardrail')}
          value={lane.guardrail}
        />
      </div>
      <div className="mt-3 inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground">
        <span className="min-w-0 truncate">
          {t(`api_keys.import_export.lanes.${lane.key}.action`, lane.actionLabel)}
        </span>
        <ArrowRightLeft className="size-3.5 shrink-0 text-muted-foreground" />
      </div>
    </div>
  )
}

function DeveloperImportExportTemplateList({
  templates,
}: {
  templates: DeveloperImportExportTemplate[]
}) {
  const { t } = useTranslation()
  return (
    <div className="overflow-hidden rounded-[1rem] border border-border/60">
      <div className="border-b border-border/60 bg-muted/20 px-3 py-2 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {t('api_keys.import_export.templates', 'Template catalog')}
      </div>
      <div className="divide-y divide-border/50">
        {templates.map((template) => (
          <div
            key={template.id}
            data-testid={`developer-import-export-template-${template.id}`}
            className="grid gap-2 px-3 py-3 text-sm sm:grid-cols-[minmax(0,1fr)_auto]"
          >
            <div className="min-w-0">
              <div className="truncate font-mono text-foreground">{template.id}</div>
              <div className="mt-1 text-xs text-muted-foreground">
                {template.direction} / {template.object}
              </div>
            </div>
            <span
              className={cn(
                'inline-flex w-fit items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
                developerImportExportTemplateClass(template.status),
              )}
            >
              {template.format.toUpperCase()}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

function DeveloperImportExportDryRunList({ rows }: { rows: DeveloperImportExportDryRunRow[] }) {
  const { t } = useTranslation()
  return (
    <div className="overflow-hidden rounded-[1rem] border border-border/60">
      <div className="border-b border-border/60 bg-muted/20 px-3 py-2 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {t('api_keys.import_export.dry_run_rows', 'Dry-run sample')}
      </div>
      <div className="divide-y divide-border/50">
        {rows.map((row) => (
          <div
            key={`${row.row}-${row.action}`}
            data-testid={`developer-import-export-dry-run-row-${row.row}`}
            className="grid gap-2 px-3 py-3 text-sm sm:grid-cols-[auto_minmax(0,1fr)_auto]"
          >
            <div className="font-mono text-xs text-muted-foreground">#{row.row}</div>
            <div className="min-w-0">
              <div className="truncate text-foreground">{row.evidence}</div>
              <div className="mt-1 text-xs text-muted-foreground">
                {t('api_keys.import_export.recovery_action', {
                  defaultValue: 'Recovery {{value}}',
                  value: row.recoveryAction,
                })}
              </div>
            </div>
            <span
              className={cn(
                'inline-flex w-fit rounded-full border px-2 py-0.5 text-xs font-medium',
                developerImportExportTemplateClass(row.status),
              )}
            >
              {row.action}
            </span>
          </div>
        ))}
      </div>
    </div>
  )
}

function DeveloperImportExportMappingMatrix({ rows }: { rows: DeveloperImportExportMappingRow[] }) {
  const { t } = useTranslation()
  return (
    <div className="overflow-hidden rounded-[1rem] border border-border/60">
      <div className="border-b border-border/60 bg-muted/20 px-3 py-2 text-xs font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {t('api_keys.import_export.mapping_matrix', 'Field mapping matrix')}
      </div>
      <div className="divide-y divide-border/50">
        {rows.map((row) => (
          <div
            key={row.localField}
            data-testid={`developer-import-export-mapping-${row.localField}`}
            className="grid gap-3 px-3 py-3 text-sm md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]"
          >
            <div className="min-w-0">
              <div className="text-xs font-medium text-muted-foreground">
                {t('api_keys.import_export.local_field', 'Local field')}
              </div>
              <div className="mt-1 truncate font-mono text-foreground">{row.localField}</div>
            </div>
            <div className="min-w-0">
              <div className="text-xs font-medium text-muted-foreground">
                {t('api_keys.import_export.provider_field', 'Template field')}
              </div>
              <div className="mt-1 truncate font-mono text-foreground">{row.providerField}</div>
            </div>
            <div className="flex items-start justify-between gap-2 md:justify-end">
              <DeveloperImportExportRowBadge required={row.required} status={row.status} />
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function DeveloperImportExportFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function DeveloperImportExportStatusBadge({ status }: { status: DeveloperImportExportStatus }) {
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
        developerImportExportBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`api_keys.import_export.status.${status}`, status)}
    </span>
  )
}

function DeveloperImportExportRowBadge({
  required,
  status,
}: {
  required: boolean
  status: DeveloperImportExportMappingRow['status']
}) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'inline-flex shrink-0 rounded-full border px-2 py-0.5 text-xs font-medium',
        developerImportExportMappingClass(status),
      )}
    >
      {t(`api_keys.import_export.row_status.${status}`, status)}
      {required ? ` / ${t('api_keys.import_export.required', 'required')}` : ''}
    </span>
  )
}

function developerImportExportIcon(key: DeveloperImportExportLaneKey) {
  switch (key) {
    case 'template_catalog':
      return FileSpreadsheet
    case 'schema_preview':
      return TableProperties
    case 'field_mapping':
      return GitCompareArrows
    case 'dry_run_diff':
      return DatabaseZap
    case 'error_recovery':
      return RotateCcw
    case 'governance_audit':
      return ShieldPlus
    default:
      return FileType2
  }
}

function developerImportExportBadgeClass(status: DeveloperImportExportStatus) {
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

function developerImportExportSurfaceClass(status: DeveloperImportExportStatus) {
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

function developerImportExportTemplateClass(status: 'ready' | 'watch' | 'blocked') {
  switch (status) {
    case 'ready':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'watch':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    case 'blocked':
      return 'border-red-200 bg-red-50 text-red-700'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}

function developerImportExportMappingClass(status: DeveloperImportExportMappingRow['status']) {
  switch (status) {
    case 'mapped':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'suggested':
      return 'border-sky-200 bg-sky-50 text-sky-800'
    case 'missing':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    case 'drift':
      return 'border-red-200 bg-red-50 text-red-700'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}
