import { Plus, Search } from 'lucide-react'
import { type ReactNode, useId, useRef, useState } from 'react'
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
  allTags,
  onSelect,
  onCreate,
  disabled,
  trigger,
}: {
  availableTags: Tag[]
  allTags?: Tag[]
  onSelect: (tagId: string) => void
  onCreate?: (name: string) => void
  disabled?: boolean
  trigger?: ReactNode
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [activeIndex, setActiveIndex] = useState(-1)
  const inputRef = useRef<HTMLInputElement>(null)
  const listId = useId()

  const trimmed = query.trim().toLowerCase()
  const filtered = trimmed
    ? availableTags.filter((tag) => tag.name.toLowerCase().includes(trimmed))
    : availableTags
  const exactMatchTag = (allTags ?? availableTags).find((tag) => tag.name.toLowerCase() === trimmed)
  // Use the full known catalog for create suppression so a hidden assigned tag
  // does not surface a misleading "Create" action.
  const exactMatch = exactMatchTag != null
  const showCreate = onCreate && trimmed.length > 0 && !exactMatch
  const emptyState =
    exactMatch && filtered.length === 0
      ? t('tags.combobox.exact_match_assigned', { name: query.trim() })
      : t('tags.combobox.no_results')

  const optionCount = filtered.length + (showCreate ? 1 : 0)

  const handleSelect = (tagId: string) => {
    onSelect(tagId)
    setQuery('')
    setActiveIndex(-1)
    setOpen(false)
  }

  const handleCreate = () => {
    /* v8 ignore next -- @preserve: create option is hidden/disabled without a create handler and non-empty query. */
    if (!onCreate || !trimmed) return
    onCreate(query.trim())
    setQuery('')
    setActiveIndex(-1)
    setOpen(false)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown': {
        e.preventDefault()
        if (optionCount === 0) return
        setActiveIndex((i) => (i + 1) % optionCount)
        break
      }
      case 'ArrowUp': {
        e.preventDefault()
        if (optionCount === 0) return
        setActiveIndex((i) => (i - 1 + optionCount) % optionCount)
        break
      }
      case 'Home': {
        e.preventDefault()
        if (optionCount === 0) return
        setActiveIndex(0)
        break
      }
      case 'End': {
        e.preventDefault()
        if (optionCount === 0) return
        setActiveIndex(optionCount - 1)
        break
      }
      case 'Enter': {
        e.preventDefault()
        if (activeIndex >= 0 && activeIndex < filtered.length) {
          handleSelect(filtered[activeIndex].id)
        } else if (activeIndex === filtered.length && showCreate) {
          handleCreate()
        } else if (filtered.length === 1) {
          handleSelect(filtered[0].id)
        } else if (showCreate) {
          handleCreate()
        }
        break
      }
      case 'Escape': {
        setOpen(false)
        break
      }
    }
  }

  const activeDescendant =
    activeIndex >= 0 && activeIndex < filtered.length
      ? `${listId}-opt-${filtered[activeIndex].id}`
      : activeIndex === filtered.length && showCreate
        ? `${listId}-create`
        : undefined

  return (
    <Popover
      open={open}
      onOpenChange={(v) => {
        setOpen(v)
        if (!v) {
          setQuery('')
          setActiveIndex(-1)
        }
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
            onChange={(e) => {
              setQuery(e.target.value)
              setActiveIndex(-1)
            }}
            onKeyDown={handleKeyDown}
            placeholder={t('tags.combobox.search_placeholder')}
            className="h-9 border-0 shadow-none focus-visible:ring-0"
            role="combobox"
            aria-expanded={open}
            aria-controls={listId}
            aria-activedescendant={activeDescendant}
            aria-autocomplete="list"
          />
        </div>
        <div id={listId} role="listbox" className="max-h-48 overflow-y-auto p-1">
          {filtered.length > 0 ? (
            filtered.map((tag, i) => (
              <button
                key={tag.id}
                id={`${listId}-opt-${tag.id}`}
                type="button"
                role="option"
                aria-selected={i === activeIndex}
                className={`flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-sm outline-hidden hover:bg-accent hover:text-accent-foreground ${i === activeIndex ? 'bg-accent text-accent-foreground' : ''}`}
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
            <p className="px-2 py-4 text-center text-sm text-muted-foreground">{emptyState}</p>
          ) : null}
          {showCreate ? (
            <button
              id={`${listId}-create`}
              type="button"
              role="option"
              aria-selected={activeIndex === filtered.length}
              className={`flex w-full items-center gap-2 rounded-sm border-t px-2 py-1.5 text-sm text-primary outline-hidden hover:bg-accent ${activeIndex === filtered.length ? 'bg-accent' : ''}`}
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
