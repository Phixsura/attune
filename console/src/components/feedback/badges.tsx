import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

// Severity color mapping — attune-specific design choice:
//   P0 阻塞 = destructive red
//   P1 严重 = amber
//   P2 中等 = blue
//   P3 一般 = muted
// Kept simple inline (no Badge component yet) to ship lean.

const severityClasses: Record<string, string> = {
  P0: 'bg-destructive/10 text-destructive border-destructive/30',
  P1: 'bg-amber-500/10 text-amber-700 border-amber-500/30 dark:text-amber-400',
  P2: 'bg-blue-500/10 text-blue-700 border-blue-500/30 dark:text-blue-400',
  P3: 'bg-muted text-muted-foreground border-border',
}

export function SeverityBadge({ severity }: { severity: string | null | undefined }) {
  const { t } = useTranslation()
  if (!severity) return <span className="text-muted-foreground">—</span>
  const label = t(`feedback.severity.${severity}`, severity)
  const cls = severityClasses[severity] ?? severityClasses.P3
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-md border px-1.5 py-0.5 text-xs font-medium',
        cls,
      )}
    >
      {label}
    </span>
  )
}

const kindClasses: Record<string, string> = {
  bug: 'bg-red-500/10 text-red-700 border-red-500/30 dark:text-red-400',
  feature: 'bg-violet-500/10 text-violet-700 border-violet-500/30 dark:text-violet-400',
  ops: 'bg-orange-500/10 text-orange-700 border-orange-500/30 dark:text-orange-400',
  question: 'bg-sky-500/10 text-sky-700 border-sky-500/30 dark:text-sky-400',
  other: 'bg-muted text-muted-foreground border-border',
}

export function KindBadge({ kind }: { kind: string | null | undefined }) {
  const { t } = useTranslation()
  if (!kind) return <span className="text-muted-foreground">—</span>
  const label = t(`feedback.kind.${kind}`, kind)
  const cls = kindClasses[kind] ?? kindClasses.other
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-md border px-1.5 py-0.5 text-xs font-medium',
        cls,
      )}
    >
      {label}
    </span>
  )
}
