import type { ReactNode } from 'react'
import { cn } from '@/lib/utils'

export function PageHero({
  eyebrow,
  title,
  subtitle,
  actions,
  metrics,
  className,
}: {
  eyebrow?: string
  title: string
  subtitle?: string
  actions?: ReactNode
  metrics?: ReactNode
  className?: string
}) {
  return (
    <section className={cn('space-y-4', className)}>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="min-w-0">
          {eyebrow && <div className="text-xs font-medium text-muted-foreground">{eyebrow}</div>}
          <h1 className="mt-1 text-2xl font-semibold tracking-tight text-foreground">{title}</h1>
          {subtitle && <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{subtitle}</p>}
        </div>
        {actions && <div className="flex items-center gap-2 shrink-0">{actions}</div>}
      </div>
      {metrics && (
        <div className="flex flex-wrap items-center gap-x-5 gap-y-2 border-t border-border/50 pt-3 text-sm">
          {metrics}
        </div>
      )}
    </section>
  )
}

export function PageHeroMetric({
  label,
  value,
  hint,
  tone = 'default',
}: {
  label: string
  value: string
  hint?: string
  tone?: 'default' | 'urgent' | 'active'
}) {
  const valueClass =
    tone === 'urgent'
      ? 'text-destructive'
      : tone === 'active'
        ? 'text-amber-800 dark:text-amber-300'
        : 'text-foreground'
  return (
    <span className="inline-flex items-center gap-1.5" title={hint}>
      <span className="text-muted-foreground">{label}</span>
      <span className={cn('font-semibold tabular-nums', valueClass)}>{value}</span>
    </span>
  )
}
