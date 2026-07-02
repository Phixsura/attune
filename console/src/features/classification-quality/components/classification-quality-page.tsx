import { useQuery } from '@tanstack/react-query'
import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Activity, AlertTriangle, BarChart3, Loader2, Search, TrendingUp, X } from 'lucide-react'
import type { ReactNode } from 'react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  type ClassificationQuality,
  type ClassificationQualityBucket,
  type ClassificationQualityFilters,
  type ClassificationQualityRange,
  type ClassificationQualitySeverity,
  classificationQualityBucketForRange,
  classificationQualityQuery,
  defaultClassificationQualityFilters,
} from '@/features/classification-quality/api/get-classification-quality'
import { consolePath } from '@/lib/console-path'
import type {
  ClassificationDimensionDrift,
  ClassificationQualitySample,
  ClassificationQualityTimeBucket,
  ClassificationQualityWarning,
  ClassificationValueDrift,
} from '@/proto/attune/v1/classification_quality'

type QualityUiFilters = Required<
  Pick<ClassificationQualityFilters, 'range' | 'bucketWidth' | 'severity'>
> &
  ClassificationQualityFilters

export function ClassificationQualityPage() {
  const { t } = useTranslation()
  const [filters, setFilters] = useState<QualityUiFilters>(defaultClassificationQualityFilters)
  const quality = useQuery(classificationQualityQuery(filters))
  const summary = quality.data?.summary
  const warningCount = quality.data?.warnings.length ?? 0

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.analytics')}
        title={t('nav.classification_quality')}
        subtitle={t('classification_quality.subtitle')}
        actions={<QualityFilters filters={filters} onChange={setFilters} />}
        metrics={
          <>
            <PageHeroMetric
              label={t('classification_quality.summary.events')}
              value={formatInt(summary?.classificationEvents)}
              hint={t('classification_quality.summary.events_hint')}
            />
            <PageHeroMetric
              label={t('classification_quality.summary.low_confidence')}
              value={formatRate(summary?.lowConfidenceRate)}
              tone={rateTone(summary?.lowConfidenceRate, 0.1, 0.05)}
            />
            <PageHeroMetric
              label={t('classification_quality.summary.off_list')}
              value={formatRate(summary?.offListRate)}
              tone={rateTone(summary?.offListRate, 0.05, 0.02)}
            />
            <PageHeroMetric
              label={t('classification_quality.summary.warnings')}
              value={formatInt(warningCount)}
              tone={warningCount > 0 ? 'urgent' : 'default'}
            />
          </>
        }
      />

      {quality.isPending ? (
        <div className="flex items-center justify-center py-12 text-muted-foreground">
          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          {t('app.loading')}
        </div>
      ) : quality.data ? (
        <ClassificationQualityBody data={quality.data} />
      ) : null}
    </div>
  )
}

function QualityFilters({
  filters,
  onChange,
}: {
  filters: QualityUiFilters
  onChange: (filters: QualityUiFilters) => void
}) {
  const { t } = useTranslation()
  const hasAdvancedFilters =
    !!filters.dimensionName ||
    !!filters.source ||
    !!filters.logicalModel ||
    !!filters.providerModel ||
    !!filters.channelId
  return (
    <div className="flex max-w-4xl flex-wrap items-center justify-end gap-2">
      <Select
        value={filters.range}
        onValueChange={(range) => {
          const nextRange = range as ClassificationQualityRange
          onChange({
            ...filters,
            range: nextRange,
            bucketWidth: classificationQualityBucketForRange(nextRange, filters.bucketWidth),
          })
        }}
      >
        <SelectTrigger className="w-28 bg-background/90">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="7d">{t('classification_quality.range.7d')}</SelectItem>
          <SelectItem value="30d">{t('classification_quality.range.30d')}</SelectItem>
          <SelectItem value="90d">{t('classification_quality.range.90d')}</SelectItem>
        </SelectContent>
      </Select>
      <Select
        value={filters.bucketWidth}
        onValueChange={(bucketWidth) =>
          onChange({
            ...filters,
            bucketWidth: classificationQualityBucketForRange(
              filters.range,
              bucketWidth as ClassificationQualityBucket,
            ),
          })
        }
      >
        <SelectTrigger className="w-28 bg-background/90">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="day">{t('classification_quality.bucket.day')}</SelectItem>
          <SelectItem value="hour" disabled={filters.range !== '7d'}>
            {t('classification_quality.bucket.hour')}
          </SelectItem>
        </SelectContent>
      </Select>
      <Select
        value={filters.severity}
        onValueChange={(severity) =>
          onChange({ ...filters, severity: severity as ClassificationQualitySeverity })
        }
      >
        <SelectTrigger className="w-36 bg-background/90">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{t('classification_quality.severity.all')}</SelectItem>
          <SelectItem value="alert">{t('classification_quality.severity.alert')}</SelectItem>
          <SelectItem value="watch">{t('classification_quality.severity.watch')}</SelectItem>
          <SelectItem value="normal">{t('classification_quality.severity.normal')}</SelectItem>
        </SelectContent>
      </Select>
      <QualityTextFilter
        value={filters.dimensionName}
        label={t('classification_quality.filters.dimension')}
        onChange={(dimensionName) => onChange({ ...filters, dimensionName })}
      />
      <QualityTextFilter
        value={filters.source}
        label={t('classification_quality.filters.source')}
        onChange={(source) => onChange({ ...filters, source })}
      />
      <QualityTextFilter
        value={filters.logicalModel}
        label={t('classification_quality.filters.logical_model')}
        onChange={(logicalModel) => onChange({ ...filters, logicalModel })}
      />
      <QualityTextFilter
        value={filters.providerModel}
        label={t('classification_quality.filters.provider_model')}
        onChange={(providerModel) => onChange({ ...filters, providerModel })}
      />
      <QualityTextFilter
        value={filters.channelId}
        label={t('classification_quality.filters.channel_id')}
        onChange={(channelId) => onChange({ ...filters, channelId })}
      />
      {hasAdvancedFilters ? (
        <Button
          type="button"
          variant="ghost"
          size="icon-sm"
          title={t('classification_quality.filters.clear')}
          aria-label={t('classification_quality.filters.clear')}
          onClick={() =>
            onChange({
              range: filters.range,
              bucketWidth: filters.bucketWidth,
              severity: filters.severity,
            })
          }
        >
          <X className="size-4" />
        </Button>
      ) : null}
    </div>
  )
}

function QualityTextFilter({
  value,
  label,
  onChange,
}: {
  value: string | undefined
  label: string
  onChange: (value: string | undefined) => void
}) {
  return (
    <Input
      value={value ?? ''}
      aria-label={label}
      placeholder={label}
      className="h-8 w-28 bg-background/90 text-sm md:w-32"
      onChange={(event) => onChange(event.target.value || undefined)}
    />
  )
}

function ClassificationQualityBody({ data }: { data: ClassificationQuality }) {
  const { t } = useTranslation()
  const period = t('classification_quality.period', {
    start: formatDate(data.currentFrom),
    end: formatDate(data.currentTo),
  })

  return (
    <div className="space-y-6">
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.15fr)_minmax(20rem,0.85fr)]">
        <Card className="overflow-hidden rounded-lg border-border/70 shadow-none">
          <CardHeader className="border-b border-border/60 bg-muted/15">
            <div className="flex items-start justify-between gap-3">
              <div>
                <CardTitle>{t('classification_quality.trend.title')}</CardTitle>
                <CardDescription>{period}</CardDescription>
              </div>
              <Badge tone={severityTone(data.summary?.worstSeverity)}>
                {severityLabel(data.summary?.worstSeverity, t)}
              </Badge>
            </div>
          </CardHeader>
          <CardContent className="pt-5">
            <QualityTrend series={data.series} />
          </CardContent>
        </Card>

        <WarningsCard warnings={data.warnings} />
      </div>

      <DimensionDriftCard dimensions={data.dimensions} />
      <SamplesCard samples={data.samples} />
    </div>
  )
}

function QualityTrend({ series }: { series: ClassificationQualityTimeBucket[] }) {
  const { t } = useTranslation()
  const rows = series.slice(-18)
  const maxRate = Math.max(
    0.01,
    ...rows.flatMap((row) => [
      row.lowConfidenceRate,
      row.offListRate,
      row.parseFailureRate,
      row.terminalFailureRate,
    ]),
  )

  if (rows.length === 0) {
    return (
      <EmptyState
        icon={BarChart3}
        title={t('classification_quality.empty.title')}
        description={t('classification_quality.empty.body')}
      />
    )
  }

  return (
    <div className="space-y-4">
      <div className="grid h-44 grid-flow-col items-end gap-2 overflow-x-auto pb-1">
        {rows.map((row) => (
          <div
            key={row.bucket}
            className="flex min-w-12 flex-col items-center justify-end gap-1 text-[10px]"
            title={formatDate(row.bucket)}
          >
            <Bar value={row.lowConfidenceRate} max={maxRate} className="bg-amber-500" />
            <Bar value={row.offListRate} max={maxRate} className="bg-sky-500" />
            <Bar value={row.parseFailureRate} max={maxRate} className="bg-rose-500" />
            <Bar value={row.terminalFailureRate} max={maxRate} className="bg-zinc-700" />
            <span className="mt-1 w-full truncate text-center text-muted-foreground">
              {format(new Date(row.bucket), 'MM-dd', { locale: zhCN })}
            </span>
          </div>
        ))}
      </div>
      <div className="flex flex-wrap gap-3 text-xs text-muted-foreground">
        <Legend color="bg-amber-500" label={t('classification_quality.legend.low_confidence')} />
        <Legend color="bg-sky-500" label={t('classification_quality.legend.off_list')} />
        <Legend color="bg-rose-500" label={t('classification_quality.legend.parse_failure')} />
        <Legend color="bg-zinc-700" label={t('classification_quality.legend.terminal_failure')} />
      </div>
    </div>
  )
}

function Bar({ value, max, className }: { value: number; max: number; className: string }) {
  const height = Math.max(4, Math.round((value / max) * 112))
  return <div className={`w-2.5 rounded-sm ${className}`} style={{ height }} />
}

function Legend({ color, label }: { color: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={`size-2 rounded-full ${color}`} />
      {label}
    </span>
  )
}

function WarningsCard({ warnings }: { warnings: ClassificationQualityWarning[] }) {
  const { t } = useTranslation()
  return (
    <Card className="rounded-lg border-border/70 shadow-none">
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <CardTitle>{t('classification_quality.warnings.title')}</CardTitle>
        <CardDescription>
          {t('classification_quality.warnings.count', { count: warnings.length })}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3 pt-5">
        {warnings.length > 0 ? (
          warnings.slice(0, 6).map((warning) => (
            <div
              key={`${warning.reason}-${warning.dimensionName}-${warning.severity}`}
              className="rounded-md border border-border/70 bg-background p-3"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <Badge tone={severityTone(warning.severity)}>
                      {severityLabel(warning.severity, t)}
                    </Badge>
                    <span className="truncate text-sm font-medium">
                      {warningReasonLabel(warning.reason, t)}
                    </span>
                  </div>
                  <div className="mt-2 text-xs text-muted-foreground">
                    {warning.dimensionName || t('classification_quality.warnings.global')} ·{' '}
                    {formatRate(warning.value)}
                  </div>
                </div>
                <Button asChild variant="ghost" size="sm">
                  <a href={feedbackHref(warning.sampleFeedbackIds, warning.reason)}>
                    <Search className="size-3.5" />
                    {t('classification_quality.open_feedback')}
                  </a>
                </Button>
              </div>
            </div>
          ))
        ) : (
          <EmptyState
            icon={AlertTriangle}
            title={t('classification_quality.warnings.empty_title')}
            description={t('classification_quality.warnings.empty_body')}
          />
        )}
      </CardContent>
    </Card>
  )
}

function DimensionDriftCard({ dimensions }: { dimensions: ClassificationDimensionDrift[] }) {
  const { t } = useTranslation()
  return (
    <Card className="gap-0 overflow-hidden rounded-lg border-border/70 py-0 shadow-none">
      <CardHeader className="border-b border-border/60 bg-muted/15 px-4 py-3">
        <CardTitle>{t('classification_quality.dimensions.title')}</CardTitle>
        <CardDescription>
          {t('classification_quality.dimensions.count', { count: dimensions.length })}
        </CardDescription>
      </CardHeader>
      <CardContent className="px-0">
        {dimensions.length > 0 ? (
          <div className="divide-y divide-border/60">
            {dimensions.map((dimension) => (
              <DimensionRow key={dimension.dimensionName} dimension={dimension} />
            ))}
          </div>
        ) : (
          <EmptyState
            icon={Activity}
            title={t('classification_quality.dimensions.empty_title')}
            description={t('classification_quality.dimensions.empty_body')}
          />
        )}
      </CardContent>
    </Card>
  )
}

function DimensionRow({ dimension }: { dimension: ClassificationDimensionDrift }) {
  const { t } = useTranslation()
  const values = dimension.values.slice(0, 3)
  return (
    <div className="grid gap-4 px-4 py-4 text-sm lg:grid-cols-[minmax(9rem,0.8fr)_minmax(18rem,1.4fr)_10rem_10rem] lg:items-center">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <Badge tone={severityTone(dimension.severity)}>
            {severityLabel(dimension.severity, t)}
          </Badge>
          <span className="truncate font-medium">{dimension.dimensionName}</span>
        </div>
        <div className="mt-1 text-xs text-muted-foreground">
          {t('classification_quality.dimensions.volume', {
            current: formatInt(dimension.currentCount),
            baseline: formatInt(dimension.baselineCount),
          })}
        </div>
      </div>
      <div className="min-w-0 space-y-2">
        {values.map((value) => (
          <ValueShift key={`${value.valueHash}-${value.valueStatus}`} value={value} />
        ))}
      </div>
      <MetricPair
        label={t('classification_quality.dimensions.js')}
        value={dimension.jsDistance.toFixed(3)}
        hint={t('classification_quality.dimensions.psi', { value: dimension.psi.toFixed(3) })}
      />
      <MetricPair
        label={t('classification_quality.dimensions.low_confidence')}
        value={formatRate(dimension.lowConfidenceRate)}
        hint={t('classification_quality.dimensions.off_list', {
          value: formatRate(dimension.offListRate),
        })}
      />
    </div>
  )
}

function ValueShift({ value }: { value: ClassificationValueDrift }) {
  const { t } = useTranslation()
  const width = Math.min(100, Math.max(4, Math.abs(value.shareDeltaPp)))
  return (
    <div>
      <div className="mb-1 flex items-center justify-between gap-3 text-xs">
        <div className="min-w-0 truncate">
          <span className="font-medium">{value.valueDisplay || value.valueHash}</span>
          <span className="ml-2 text-muted-foreground">
            {valueStatusLabel(value.valueStatus, t)}
          </span>
        </div>
        <span className="font-mono tabular-nums">{formatDelta(value.shareDeltaPp)}</span>
      </div>
      <div className="h-1.5 rounded-full bg-muted">
        <div
          className={
            value.shareDeltaPp >= 0
              ? 'h-1.5 rounded-full bg-amber-500'
              : 'h-1.5 rounded-full bg-sky-500'
          }
          style={{ width: `${width}%` }}
        />
      </div>
    </div>
  )
}

function SamplesCard({ samples }: { samples: ClassificationQualitySample[] }) {
  const { t } = useTranslation()
  return (
    <Card className="gap-0 overflow-hidden rounded-lg border-border/70 py-0 shadow-none">
      <CardHeader className="border-b border-border/60 bg-muted/15 px-4 py-3">
        <CardTitle>{t('classification_quality.samples.title')}</CardTitle>
        <CardDescription>
          {t('classification_quality.samples.count', { count: samples.length })}
        </CardDescription>
      </CardHeader>
      <CardContent className="px-0">
        {samples.length > 0 ? (
          <div className="divide-y divide-border/60">
            {samples.map((sample) => (
              <div
                key={sample.id}
                className="grid gap-3 px-4 py-3 text-sm md:grid-cols-[minmax(10rem,1fr)_8rem_8rem_8rem]"
              >
                <div className="min-w-0">
                  <div className="truncate font-medium">{sample.title || sample.id}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {formatDate(sample.createdAt)} · {sample.source || 'unknown'}
                  </div>
                </div>
                <MetricPair
                  label={t('classification_quality.samples.confidence')}
                  value={
                    sample.classificationConfidence == null
                      ? '-'
                      : formatRate(sample.classificationConfidence)
                  }
                />
                <MetricPair
                  label={t('classification_quality.samples.status')}
                  value={sample.enrichmentStatus || '-'}
                />
                <Button
                  asChild
                  variant="ghost"
                  size="sm"
                  className="justify-self-start md:justify-self-end"
                >
                  <a href={feedbackHref([sample.id], sample.signalReason)}>
                    <Search className="size-3.5" />
                    {t('classification_quality.open_feedback')}
                  </a>
                </Button>
              </div>
            ))}
          </div>
        ) : (
          <EmptyState
            icon={TrendingUp}
            title={t('classification_quality.samples.empty_title')}
            description={t('classification_quality.samples.empty_body')}
          />
        )}
      </CardContent>
    </Card>
  )
}

function MetricPair({ label, value, hint }: { label: string; value: string; hint?: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-1 truncate font-mono text-sm font-semibold tabular-nums">{value}</div>
      {hint ? <div className="mt-1 truncate text-xs text-muted-foreground">{hint}</div> : null}
    </div>
  )
}

function Badge({
  tone,
  children,
}: {
  tone: 'default' | 'watch' | 'alert' | 'muted'
  children: ReactNode
}) {
  const toneClass =
    tone === 'alert'
      ? 'border-red-200 bg-red-50 text-red-700 dark:border-red-900 dark:bg-red-950/40 dark:text-red-300'
      : tone === 'watch'
        ? 'border-amber-200 bg-amber-50 text-amber-800 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-300'
        : tone === 'muted'
          ? 'border-border bg-muted text-muted-foreground'
          : 'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-900 dark:bg-emerald-950/40 dark:text-emerald-300'
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[11px] font-medium ${toneClass}`}
    >
      {children}
    </span>
  )
}

function severityTone(severity: string | undefined): 'default' | 'watch' | 'alert' | 'muted' {
  if (severity === 'alert') return 'alert'
  if (severity === 'watch') return 'watch'
  if (severity === 'insufficient_data') return 'muted'
  return 'default'
}

function severityLabel(severity: string | undefined, t: (key: string) => string) {
  return t(`classification_quality.severity.${severity || 'normal'}`)
}

function warningReasonLabel(reason: string, t: (key: string) => string) {
  switch (reason) {
    case 'dimension_distribution_drift':
      return t('classification_quality.warning_reason.dimension_distribution_drift')
    case 'low_confidence_rate_spike':
      return t('classification_quality.warning_reason.low_confidence_rate_spike')
    case 'off_list_rate_spike':
      return t('classification_quality.warning_reason.off_list_rate_spike')
    case 'parse_failure_rate_spike':
      return t('classification_quality.warning_reason.parse_failure_rate_spike')
    case 'terminal_failure_rate_spike':
      return t('classification_quality.warning_reason.terminal_failure_rate_spike')
    default:
      return reason
  }
}

function valueStatusLabel(status: string, t: (key: string) => string) {
  switch (status) {
    case 'configured':
      return t('classification_quality.value_status.configured')
    case 'off_list':
      return t('classification_quality.value_status.off_list')
    case 'unknown_dimension':
      return t('classification_quality.value_status.unknown_dimension')
    default:
      return status
  }
}

function feedbackHref(ids: string[], reason: string) {
  const qs = new URLSearchParams()
  const cleanIds = ids.filter(Boolean)
  if (cleanIds.length > 0) qs.set('ids', cleanIds.join(','))
  const signal = qualitySignalForReason(reason)
  if (signal) qs.set('quality_signal', signal)
  const suffix = qs.toString()
  return consolePath(`/feedback${suffix ? `?${suffix}` : ''}`)
}

function qualitySignalForReason(reason: string) {
  switch (reason) {
    case 'low_confidence_rate_spike':
      return 'low_confidence'
    case 'off_list_rate_spike':
      return 'off_list'
    case 'parse_failure_rate_spike':
      return 'parse_failure'
    case 'terminal_failure_rate_spike':
      return 'terminal_failure'
    default:
      return ''
  }
}

function rateTone(value: number | undefined, alert: number, watch: number) {
  const rate = value ?? 0
  if (rate >= alert) return 'urgent' as const
  if (rate >= watch) return 'active' as const
  return 'default' as const
}

function formatInt(value: string | number | undefined) {
  return new Intl.NumberFormat('en-US').format(Number(value ?? 0))
}

function formatRate(value: number | undefined) {
  return new Intl.NumberFormat('en-US', {
    style: 'percent',
    maximumFractionDigits: 1,
  }).format(value ?? 0)
}

function formatDelta(value: number) {
  const sign = value > 0 ? '+' : ''
  return `${sign}${value.toFixed(1)}pp`
}

function formatDate(value: string) {
  if (!value) return '-'
  return format(new Date(value), 'yyyy-MM-dd', { locale: zhCN })
}
