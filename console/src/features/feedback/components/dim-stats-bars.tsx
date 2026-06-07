import { useTranslation } from 'react-i18next'
import { useDisplayName } from '@/lib/i18n-resolve'
import type { Dimension } from '@/proto/attune/v1/common'
import type { DimStats, ValueCount } from '@/proto/attune/v1/ingest'

// DimStatsBars renders the per-dim top-values distribution returned
// by GET /feedback/stats. One small horizontal bar block per dim,
// powered by stable Value → DisplayName resolution from the tenant's
// dim set so the same chart works after taxonomy edits.
//
// Replaces the pre-E3 single-axis "kind donut" with a metadata-driven
// surface that grows organically as operators add dims.
export function DimStatsBars({
  dims,
  stats,
  total,
}: {
  dims: Dimension[]
  stats: DimStats[]
  total: number
}) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  if (total === 0 || stats.length === 0) return null

  return (
    <div className="grid grid-cols-1 gap-6 md:grid-cols-2 lg:grid-cols-3">
      {stats.map((ds) => {
        const dim = dims.find((d) => d.name === ds.dim)
        if (!dim || ds.top.length === 0) return null
        const label = displayOf(dim.displayName) || ds.dim
        return (
          <div key={ds.dim} className="space-y-2">
            <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
              {label}
            </div>
            <ul className="space-y-1.5">
              {ds.top.map((vc) => (
                <ValueBar key={vc.value} dim={dim} vc={vc} max={maxCount(ds.top)} />
              ))}
            </ul>
          </div>
        )
      })}
      <div className="space-y-2">
        <div className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t('feedback.stats.summary')}
        </div>
        <ul className="space-y-1 text-sm">
          <li>
            <span className="text-muted-foreground">{t('feedback.stats.total')}: </span>
            <span className="font-medium tabular-nums">{total}</span>
          </li>
        </ul>
      </div>
    </div>
  )
}

function ValueBar({ dim, vc, max }: { dim: Dimension; vc: ValueCount; max: number }) {
  const displayOf = useDisplayName()
  const tax = dim.taxonomy.find((t) => t.value === vc.value)
  const label = (tax && displayOf(tax.displayName)) || vc.value
  const count = Number(vc.count)
  const pct = max === 0 ? 0 : (count / max) * 100
  return (
    <li className="text-sm">
      <div className="flex items-center justify-between text-xs">
        <span className="truncate">{label}</span>
        <span className="tabular-nums text-muted-foreground">{count}</span>
      </div>
      <div className="mt-0.5 h-1.5 w-full overflow-hidden rounded-full bg-muted">
        <div className="h-full rounded-full bg-foreground/60" style={{ width: `${pct}%` }} />
      </div>
    </li>
  )
}

function maxCount(vs: ValueCount[]): number {
  let m = 0
  for (const v of vs) {
    const n = Number(v.count)
    if (n > m) m = n
  }
  return m
}
