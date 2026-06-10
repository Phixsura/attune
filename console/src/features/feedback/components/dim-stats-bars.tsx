import { useTranslation } from 'react-i18next'
import { DimensionValue, rendererToneBarClass } from '@/components/dim/dimension-chips'
import { useDisplayName } from '@/lib/i18n-resolve'
import type { Dimension } from '@/proto/attune/v1/common'
import type { DimStats, ValueCount } from '@/proto/attune/v1/ingest'

// DimStatsBars renders GET /feedback/stats as a compact monthly insight strip.
// It deliberately highlights the dominant signal per dimension instead of
// dumping every value/count pair, so the surface stays useful as tenants add
// more dimensions.
export function DimStatsBars({
  dims,
  stats,
  total,
  urgentCount = 0,
}: {
  dims: Dimension[]
  stats: DimStats[]
  total: number
  urgentCount?: number
}) {
  const { t } = useTranslation()
  if (total === 0) return null

  const visibleStats = stats.filter(
    (ds) => dims.some((d) => d.name === ds.dim) && ds.top.length > 0,
  )
  const activeSignals = visibleStats.reduce((n, ds) => n + ds.top.length, 0)
  const urgentPct = Math.round((urgentCount / total) * 100)

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-3">
        <SummaryMetric
          label={t('feedback.stats.total')}
          value={total}
          hint={t('feedback.stats.total_hint')}
        />
        <SummaryMetric
          label={t('feedback.stats.urgent')}
          value={urgentCount}
          hint={
            urgentCount > 0
              ? t('feedback.stats.urgent_hint', { pct: urgentPct })
              : t('feedback.stats.urgent_empty')
          }
          tone={urgentCount > 0 ? 'danger' : 'muted'}
        />
        <SummaryMetric
          label={t('feedback.stats.signals')}
          value={activeSignals}
          hint={t('feedback.stats.signals_hint')}
        />
      </div>

      {visibleStats.length > 0 && (
        <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
          {visibleStats.map((ds) => {
            const dim = dims.find((d) => d.name === ds.dim)
            if (!dim) return null
            if (dim.kind === 'multi') {
              return <MultiInsightTile key={ds.dim} dim={dim} stats={ds.top} />
            }
            return <InsightTile key={ds.dim} dim={dim} stats={ds.top} total={total} />
          })}
        </div>
      )}
    </div>
  )
}

function SummaryMetric({
  label,
  value,
  hint,
  tone = 'default',
}: {
  label: string
  value: number
  hint: string
  tone?: 'default' | 'danger' | 'muted'
}) {
  return (
    <div className="rounded-md bg-background/80 p-3">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div className="mt-1 flex items-end gap-2">
        <span
          className={
            tone === 'danger'
              ? 'text-2xl font-semibold tabular-nums text-destructive'
              : 'text-2xl font-semibold tabular-nums'
          }
        >
          {value}
        </span>
        <span className="pb-1 text-xs text-muted-foreground">{hint}</span>
      </div>
    </div>
  )
}

function InsightTile({
  dim,
  stats,
  total,
}: {
  dim: Dimension
  stats: ValueCount[]
  total: number
}) {
  const displayOf = useDisplayName()
  const label = displayOf(dim.displayName) || dim.name
  const lead = stats[0]
  if (!lead) return null

  const leadCount = Number(lead.count)
  const leadPct = Math.round((leadCount / total) * 100)
  const renderValue = dim.renderer?.values?.[lead.value]
  const secondary = stats.slice(1, 3)

  return (
    <div className="min-w-0 rounded-md bg-background/80 p-3">
      <div className="flex items-center justify-between gap-3">
        <div className="truncate text-xs font-medium text-muted-foreground">{label}</div>
        <div className="text-xs tabular-nums text-muted-foreground">{leadPct}%</div>
      </div>
      <div className="mt-2 flex min-w-0 items-center justify-between gap-3">
        <DimensionValue
          dim={dim}
          value={lead.value}
          className="min-w-0 max-w-[13rem] overflow-hidden text-ellipsis"
        />
        <span className="text-xs tabular-nums text-muted-foreground">
          {leadCount}/{total}
        </span>
      </div>
      <div className="mt-2 h-1 w-full overflow-hidden rounded-full bg-muted">
        <div
          className={`h-full rounded-full ${rendererToneBarClass(renderValue?.tone)}`}
          style={{ width: `${leadPct}%` }}
        />
      </div>
      {secondary.length > 0 && (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {secondary.map((vc) => (
            <SecondarySignal key={vc.value} dim={dim} vc={vc} total={total} />
          ))}
        </div>
      )}
    </div>
  )
}

function MultiInsightTile({ dim, stats }: { dim: Dimension; stats: ValueCount[] }) {
  const displayOf = useDisplayName()
  const label = displayOf(dim.displayName) || dim.name
  const visible = stats.slice(0, 5)
  return (
    <div className="min-w-0 rounded-md bg-background/80 p-3">
      <div className="flex items-center justify-between gap-3">
        <div className="truncate text-xs font-medium text-muted-foreground">{label}</div>
        <div className="text-xs tabular-nums text-muted-foreground">{stats.length}</div>
      </div>
      <div className="mt-3 flex flex-wrap gap-1.5">
        {visible.map((vc) => (
          <DimensionValue key={vc.value} dim={dim} value={vc.value} emptyDash={false} />
        ))}
      </div>
    </div>
  )
}

function SecondarySignal({ dim, vc, total }: { dim: Dimension; vc: ValueCount; total: number }) {
  const count = Number(vc.count)
  const pct = Math.round((count / total) * 100)
  return (
    <span className="inline-flex min-w-0 items-center gap-1 rounded-md bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
      <DimensionValue
        dim={dim}
        value={vc.value}
        emptyDash={false}
        className="border-0 bg-transparent p-0"
      />
      <span className="tabular-nums">{pct}%</span>
    </span>
  )
}
