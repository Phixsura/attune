import { X } from 'lucide-react'
import { cn } from '@/lib/utils'

export function TagBadge({
  name,
  color,
  onRemove,
  className,
}: {
  name: string
  color: string
  onRemove?: () => void
  className?: string
}) {
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        className,
      )}
    >
      <span
        className="size-2 shrink-0 rounded-full"
        style={{ backgroundColor: color }}
        aria-hidden
      />
      {name}
      {onRemove ? (
        <button
          type="button"
          className="ml-0.5 rounded-full p-0.5 text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
          onClick={(e) => {
            e.stopPropagation()
            onRemove()
          }}
        >
          <X className="size-3" />
        </button>
      ) : null}
    </span>
  )
}
