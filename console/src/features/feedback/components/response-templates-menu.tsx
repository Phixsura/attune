import { Copy, FileText, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Input } from '@/components/ui/input'
import type { ResponseTemplate } from '@/features/feedback/hooks/use-response-templates'

export function ResponseTemplatesMenu({
  templates,
  onInsert,
  onSave,
  onDelete,
  currentContent,
}: {
  templates: ResponseTemplate[]
  onInsert: (content: string) => void
  onSave: (name: string, content: string) => void
  onDelete: (id: string) => void
  currentContent: string
}) {
  const { t } = useTranslation()
  const [saving, setSaving] = useState(false)
  const [nameInput, setNameInput] = useState('')

  const handleSave = () => {
    const name = nameInput.trim()
    if (!name || !currentContent.trim()) return
    onSave(name, currentContent)
    setNameInput('')
    setSaving(false)
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="sm" className="gap-1.5">
          <FileText className="size-3.5" />
          {t('feedback.templates.title')}
          {templates.length > 0 && (
            <span className="rounded-full bg-muted px-1.5 text-[10px] font-medium">
              {templates.length}
            </span>
          )}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-64">
        {templates.length > 0 && (
          <>
            <DropdownMenuLabel>{t('feedback.templates.saved')}</DropdownMenuLabel>
            {templates.map((tpl) => (
              <DropdownMenuItem
                key={tpl.id}
                className="group flex items-center justify-between"
                onSelect={(e) => {
                  e.preventDefault()
                  onInsert(tpl.content)
                }}
              >
                <span className="flex items-center gap-2 truncate">
                  <Copy className="size-3 shrink-0 text-muted-foreground" />
                  <span className="truncate">{tpl.name}</span>
                </span>
                <button
                  type="button"
                  className="ml-2 hidden shrink-0 rounded p-0.5 text-muted-foreground hover:text-destructive group-hover:inline-flex"
                  onClick={(e) => {
                    e.stopPropagation()
                    onDelete(tpl.id)
                  }}
                >
                  <Trash2 className="size-3" />
                </button>
              </DropdownMenuItem>
            ))}
            <DropdownMenuSeparator />
          </>
        )}
        {saving ? (
          <div className="px-2 py-1.5">
            <Input
              autoFocus
              placeholder={t('feedback.templates.name_placeholder')}
              value={nameInput}
              onChange={(e) => setNameInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === 'Enter') handleSave()
                if (e.key === 'Escape') setSaving(false)
              }}
              className="h-7 text-sm"
            />
          </div>
        ) : (
          <DropdownMenuItem
            onSelect={(e) => {
              e.preventDefault()
              setSaving(true)
            }}
            disabled={!currentContent.trim()}
          >
            <Plus className="mr-2 size-3.5" />
            {t('feedback.templates.save_current')}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
