import { useDisplayName } from '@/lib/i18n-resolve'
import { cn } from '@/lib/utils'
import type { Dimension, Taxonomy } from '@/proto/attune/v1/common'

// DimensionChips renders one Dimension's value(s) for a given row.
// Single-kind dims render as one badge; multi-kind dims render as
// chips. The DisplayName resolver maps the stable Taxonomy.Value to
// the operator's UI label for the current locale; unknown values
// (e.g. legacy rows after an operator deleted the value) fall back to
// the stable Value itself so nothing disappears silently.
//
// Stable Values pass through verbatim — that's also what filter URL
// params + webhook payloads carry.
export function DimensionChips({
  dim,
  value,
  emptyDash = true,
  className,
}: {
  dim: Dimension
  value: unknown
  emptyDash?: boolean
  className?: string
}) {
  const displayOf = useDisplayName()

  if (dim.kind === 'single') {
    if (typeof value !== 'string' || value === '') {
      return emptyDash ? <span className="text-muted-foreground">—</span> : null
    }
    const label = displayForTaxonomy(dim.taxonomy, value, displayOf)
    return <Badge className={className}>{label}</Badge>
  }

  const arr = toStringArray(value)
  if (arr.length === 0) {
    return emptyDash ? <span className="text-muted-foreground">—</span> : null
  }
  return (
    <div className={cn('flex flex-wrap gap-1', className)}>
      {arr.map((v) => (
        <Chip key={v}>{displayForTaxonomy(dim.taxonomy, v, displayOf)}</Chip>
      ))}
    </div>
  )
}

function displayForTaxonomy(
  taxonomy: Taxonomy[],
  value: string,
  displayOf: ReturnType<typeof useDisplayName>,
): string {
  const t = taxonomy.find((entry) => entry.value === value)
  if (!t) return value
  return displayOf(t.displayName) || value
}

function toStringArray(v: unknown): string[] {
  if (Array.isArray(v)) {
    return v.filter((x): x is string => typeof x === 'string' && x.length > 0)
  }
  if (typeof v === 'string' && v.length > 0) return [v]
  return []
}

function Badge({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-md border border-border bg-muted px-1.5 py-0.5 text-xs font-medium',
        className,
      )}
    >
      {children}
    </span>
  )
}

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center rounded-full border border-border bg-muted px-2 py-0.5 text-xs">
      {children}
    </span>
  )
}

// UrgentDot is a small visual flag the list/detail views can paint
// next to an urgent row's title. Centralised here so the rendering
// stays consistent across surfaces.
export function UrgentDot({ urgent }: { urgent: boolean | undefined }) {
  if (!urgent) return null
  return (
    <span
      role="img"
      aria-label="urgent"
      className="inline-block h-2 w-2 rounded-full bg-destructive align-middle"
      title="urgent"
    />
  )
}
