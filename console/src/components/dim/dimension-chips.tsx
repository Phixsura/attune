import { Flame, Frown, Minus, Smile } from 'lucide-react'
import { useDisplayName } from '@/lib/i18n-resolve'
import { cn } from '@/lib/utils'
import type { Dimension, RendererValue, Taxonomy } from '@/proto/attune/v1/common'

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
    return <DimensionValue dim={dim} value={value} emptyDash={emptyDash} className={className} />
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

export function DimensionValue({
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
  if (typeof value !== 'string' || value === '') {
    return emptyDash ? <span className="text-muted-foreground">—</span> : null
  }
  const label = displayForTaxonomy(dim.taxonomy, value, displayOf)
  const renderValue = dim.renderer?.values?.[value]
  if (dim.renderer?.kind === 'enum_badge' && renderValue) {
    return (
      <EnumBadge value={renderValue} className={className}>
        {label}
      </EnumBadge>
    )
  }
  return <Badge className={className}>{label}</Badge>
}

export function rendererToneBarClass(tone: string | undefined): string {
  switch (tone) {
    case 'success':
      return 'bg-emerald-500/70'
    case 'warning':
      return 'bg-amber-500/70'
    case 'danger':
      return 'bg-destructive/75'
    case 'muted':
      return 'bg-muted-foreground/40'
    default:
      return 'bg-foreground/55'
  }
}

export function displayForTaxonomy(
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
        'inline-flex max-w-full items-center rounded-md border border-border bg-muted px-1.5 py-0.5 text-xs font-medium whitespace-nowrap',
        className,
      )}
    >
      {children}
    </span>
  )
}

function EnumBadge({
  value,
  children,
  className,
}: {
  value: RendererValue
  children: React.ReactNode
  className?: string
}) {
  const Icon = iconFor(value.icon)
  return (
    <span
      className={cn(
        'inline-flex max-w-full items-center gap-1 rounded-md border px-1.5 py-0.5 text-xs font-medium whitespace-nowrap',
        toneClass(value.tone),
        className,
      )}
    >
      {Icon && <Icon className="h-3 w-3" aria-hidden="true" />}
      {children}
    </span>
  )
}

function iconFor(icon: string) {
  switch (icon) {
    case 'smile':
      return Smile
    case 'frown':
      return Frown
    case 'flame':
      return Flame
    case 'minus':
      return Minus
    default:
      return null
  }
}

function toneClass(tone: string): string {
  switch (tone) {
    case 'success':
      return 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300'
    case 'warning':
      return 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300'
    case 'danger':
      return 'border-destructive/30 bg-destructive/10 text-destructive'
    case 'muted':
      return 'border-border bg-muted text-muted-foreground'
    default:
      return 'border-border bg-muted'
  }
}

function Chip({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-flex max-w-full items-center rounded-full border border-border bg-muted px-2 py-0.5 text-xs whitespace-nowrap">
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
