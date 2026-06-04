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
        <div className="rounded-full bg-primary/10 p-3 text-primary">
          <Icon className="h-6 w-6" />
        </div>
      )}
      <div className="space-y-1">
        <p className="text-sm font-medium text-foreground">{title}</p>
        {description && <p className="max-w-md text-sm text-muted-foreground">{description}</p>}
      </div>
      {action && (
        <Button onClick={action.onClick} className="mt-2">
          {action.label}
        </Button>
      )}
    </div>
  )
}
