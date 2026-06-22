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
    <section
      className={cn(
        'rounded-[1.6rem] border border-border/65 bg-[linear-gradient(145deg,rgba(255,250,246,0.86),rgba(255,255,255,0.98))] px-6 py-7 shadow-[0_28px_80px_-58px_rgba(15,23,42,0.2)] sm:px-7 sm:py-7.5',
        className,
      )}
    >
      <div className="flex flex-col gap-7 xl:flex-row xl:items-start xl:justify-between">
        <div className="max-w-3xl">
          {eyebrow && (
            <div className="text-[11px] font-semibold tracking-[0.22em] text-muted-foreground uppercase">
              {eyebrow}
            </div>
          )}
          <h1 className="mt-3 text-3xl font-semibold tracking-tight text-foreground sm:text-[2.05rem]">
            {title}
          </h1>
          {subtitle && (
            <p className="mt-2.5 max-w-2xl text-sm leading-6 text-muted-foreground">{subtitle}</p>
          )}
        </div>
        {actions && <div className="flex items-center gap-2 self-start">{actions}</div>}
      </div>
      {metrics && <div className="mt-7 grid gap-4.5 lg:grid-cols-4">{metrics}</div>}
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
  return (
    <div className={cn('rounded-[1.2rem] border px-4 py-4', metricToneClass(tone))}>
      <div className="text-[10px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {label}
      </div>
      <div className="mt-1.5 text-3xl font-semibold tracking-tight text-foreground">{value}</div>
      {hint && <div className="mt-1.5 text-sm leading-5 text-muted-foreground">{hint}</div>}
    </div>
  )
}

function metricToneClass(tone: 'default' | 'urgent' | 'active') {
  if (tone === 'urgent') return 'border-red-200/75 bg-red-50/62'
  if (tone === 'active') return 'border-amber-200/75 bg-amber-50/56'
  return 'border-border/65 bg-background/82'
}
