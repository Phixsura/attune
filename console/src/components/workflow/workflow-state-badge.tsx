import { useDisplayName } from '@/lib/i18n-resolve'
import { cn } from '@/lib/utils'
import type { WorkflowState } from '@/proto/attune/v1/workflow'

const categoryStyles: Record<string, string> = {
  open: 'border-blue-200 bg-blue-50 text-blue-700 dark:border-blue-800 dark:bg-blue-950 dark:text-blue-300',
  active:
    'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300',
  closed:
    'border-emerald-200 bg-emerald-50 text-emerald-700 dark:border-emerald-800 dark:bg-emerald-950 dark:text-emerald-300',
}

// WorkflowStateBadge takes the whole state and resolves displayName
// internally — every call site renders the same human label, so the
// resolver lives here rather than threading a `displayOf` through each
// caller. The stable machine key (state.name) is the fallback when
// displayName is empty for the active locale chain.
export function WorkflowStateBadge({
  state,
  className,
}: {
  state: Pick<WorkflowState, 'name' | 'color' | 'category' | 'displayName'>
  className?: string
}) {
  const displayOf = useDisplayName()
  const label = displayOf(state.displayName) || state.name
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium',
        categoryStyles[state.category] ?? 'border-border bg-muted text-muted-foreground',
        className,
      )}
    >
      <span
        className="size-2 shrink-0 rounded-full"
        style={{ backgroundColor: state.color }}
        aria-hidden
      />
      {label}
    </span>
  )
}
