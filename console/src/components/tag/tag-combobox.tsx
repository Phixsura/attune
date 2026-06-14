import { Plus, Search } from 'lucide-react'
import { type ReactNode, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import type { Tag } from '@/proto/attune/v1/tag'

const PALETTE = [
  '#ef4444',
  '#f97316',
  '#f59e0b',
  '#22c55e',
  '#14b8a6',
  '#06b6d4',
  '#3b82f6',
  '#6366f1',
  '#8b5cf6',
  '#a855f7',
  '#ec4899',
  '#6b7280',
]

export function TagCombobox({
  availableTags,
  onSelect,
  onCreate,
  disabled,
  trigger,
}: {
  availableTags: Tag[]
  onSelect: (tagId: string) => void
  onCreate?: (name: string) => void
  disabled?: boolean
  trigger?: ReactNode
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  const trimmed = query.trim().toLowerCase()
  const filtered = trimmed
    ? availableTags.filter((tag) => tag.name.toLowerCase().includes(trimmed))
    : availableTags
  const exactMatch = availableTags.some((tag) => tag.name.toLowerCase() === trimmed)
  const showCreate = onCreate && trimmed.length > 0 && !exactMatch

  const handleSelect = (tagId: string) => {
    onSelect(tagId)
    setQuery('')
    setOpen(false)
  }

  const handleCreate = () => {
    if (!onCreate || !trimmed) return
    onCreate(query.trim())
    setQuery('')
    setOpen(false)
  }

  return (
    <Popover
      open={open}
      onOpenChange={(v) => {
        setOpen(v)
        if (!v) setQuery('')
      }}
    >
      <PopoverTrigger asChild disabled={disabled}>
        {trigger ?? (
          <Button variant="ghost" size="sm" className="h-6 px-2 text-xs">
            <Plus className="mr-1 h-3 w-3" />
            {t('tags.feedback_section.add')}
          </Button>
        )}
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-64 p-0"
        onOpenAutoFocus={(e) => {
          e.preventDefault()
          inputRef.current?.focus()
        }}
      >
        <div className="flex items-center border-b px-3">
          <Search className="mr-2 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <Input
            ref={inputRef}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                if (filtered.length === 1) handleSelect(filtered[0].id)
                else if (showCreate) handleCreate()
              }
            }}
            placeholder={t('tags.combobox.search_placeholder')}
            className="h-9 border-0 shadow-none focus-visible:ring-0"
          />
        </div>
        <div className="max-h-48 overflow-y-auto p-1">
          {filtered.length > 0 ? (
            filtered.map((tag) => (
              <button
                key={tag.id}
                type="button"
                className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden hover:bg-accent hover:text-accent-foreground"
                onClick={() => handleSelect(tag.id)}
              >
                <span
                  className="size-2 shrink-0 rounded-full"
                  style={{ backgroundColor: tag.color }}
                />
                <span className="truncate">{tag.name}</span>
                {tag.exclusiveScope ? (
                  <span className="ml-auto truncate text-xs text-muted-foreground">
                    {tag.exclusiveScope}
                  </span>
                ) : null}
              </button>
            ))
          ) : !showCreate ? (
            <p className="px-2 py-4 text-center text-sm text-muted-foreground">
              {t('tags.combobox.no_results')}
            </p>
          ) : null}
          {showCreate ? (
            <button
              type="button"
              className="flex w-full items-center gap-2 rounded-sm border-t px-2 py-1.5 text-sm text-primary outline-hidden hover:bg-accent"
              onClick={handleCreate}
            >
              <Plus className="h-3.5 w-3.5" />
              {t('tags.combobox.create', { name: query.trim() })}
            </button>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  )
}

export { PALETTE }
