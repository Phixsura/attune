import type { LucideIcon } from 'lucide-react'
import type { ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

// EmptyState is the one-stop component for "this list/table is empty".
// Replaces a-page-each ad-hoc <p className="text-muted-foreground">…</p>
// blocks so empty-state language stays consistent and friendly across
// the console.
//
// Layer 1 brand: empty pages were the #1 risk of "this looks like a demo"
// for first-time SMB customers (see chat audit 2026-05-15). One Icon +
// title + soft description + optional action button reads as intentional
// "ready to start" instead of "nothing here yet".
export function EmptyState({
  icon: Icon,
  title,
  description,
  action,
  className,
}: {
  icon?: LucideIcon
  title: string
  description?: ReactNode
  action?: { label: string; onClick: () => void }
  className?: string
}) {
  return (
    <div
      className={cn('flex flex-col items-center justify-center gap-3 py-12 text-center', className)}
    >
      {Icon && (
        <div className="rounded-[1.15rem] border border-primary/10 bg-[linear-gradient(180deg,rgba(255,247,237,0.95),rgba(255,255,255,0.92))] p-3.5 text-primary shadow-[0_18px_36px_-28px_rgba(234,88,12,0.45)]">
          <Icon className="h-6 w-6" />
        </div>
      )}
      <div className="space-y-1">
        <p className="text-base font-semibold tracking-tight text-foreground">{title}</p>
        {description && (
          <p className="max-w-md text-sm leading-6 text-muted-foreground">{description}</p>
        )}
      </div>
      {action && (
        <Button onClick={action.onClick} className="mt-2">
          {action.label}
        </Button>
      )}
    </div>
  )
}
