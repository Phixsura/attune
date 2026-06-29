import { Bookmark, Check, Plus, Trash2, X } from 'lucide-react'
import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import type { SavedView, SavedViewFilters } from '@/features/feedback/hooks/use-saved-views'

export function SavedViewsMenu({
  views,
  onSave,
  onLoad,
  onRemove,
  currentFilters,
}: {
  views: SavedView[]
  onSave: (name: string, filters: SavedViewFilters) => void
  onLoad: (view: SavedView) => void
  onRemove: (id: string) => void
  currentFilters: SavedViewFilters
}) {
  const { t } = useTranslation()
  const [isNaming, setIsNaming] = useState(false)
  const [name, setName] = useState('')

  const handleSave = useCallback(() => {
    if (name.trim()) {
      onSave(name.trim(), currentFilters)
      setName('')
      setIsNaming(false)
    }
  }, [name, currentFilters, onSave])

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="h-8 gap-1.5 text-xs">
          <Bookmark className="h-3.5 w-3.5" />
          {t('feedback.saved_views.title')}
          {views.length > 0 && (
            <span className="rounded-full bg-muted px-1.5 text-[10px] tabular-nums">
              {views.length}
            </span>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-64">
        {isNaming ? (
          <div className="flex items-center gap-1.5 px-2 py-1.5">
            <Input
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={t('feedback.saved_views.name_placeholder')}
              className="h-7 text-xs"
              autoFocus
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleSave()
                if (e.key === 'Escape') setIsNaming(false)
              }}
            />
            <Button variant="ghost" size="sm" className="h-7 w-7 shrink-0 p-0" onClick={handleSave}>
              <Check className="h-3.5 w-3.5" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="h-7 w-7 shrink-0 p-0"
              onClick={() => setIsNaming(false)}
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          </div>
        ) : (
          <DropdownMenuItem onClick={() => setIsNaming(true)}>
            <Plus className="mr-2 h-3.5 w-3.5" />
            {t('feedback.saved_views.save_current')}
          </DropdownMenuItem>
        )}
        {views.length > 0 && <DropdownMenuSeparator />}
        {views.map((view) => (
          <DropdownMenuItem
            key={view.id}
            className="group flex items-center justify-between"
            onClick={() => onLoad(view)}
          >
            <span className="truncate text-xs">{view.name}</span>
            <button
              type="button"
              className="ml-2 shrink-0 rounded p-0.5 opacity-0 transition-opacity hover:bg-destructive/10 group-hover:opacity-100"
              onClick={(e) => {
                e.stopPropagation()
                onRemove(view.id)
              }}
            >
              <Trash2 className="h-3 w-3 text-destructive" />
            </button>
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
