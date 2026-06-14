import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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

export function TagFormDialog({
  open,
  tag,
  pending,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  tag?: Tag
  pending: boolean
  onOpenChange: (v: boolean) => void
  onSubmit: (data: {
    name: string
    color: string
    description: string
    exclusiveScope: string
  }) => void
}) {
  const { t } = useTranslation()
  const isEdit = !!tag
  const [name, setName] = useState('')
  const [color, setColor] = useState(PALETTE[0])
  const [description, setDescription] = useState('')
  const [exclusiveScope, setExclusiveScope] = useState('')

  useEffect(() => {
    if (open) {
      setName(tag?.name ?? '')
      setColor(tag?.color ?? PALETTE[0])
      setDescription(tag?.description ?? '')
      setExclusiveScope(tag?.exclusiveScope ?? '')
    }
  }, [open, tag])

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    onSubmit({
      name: name.trim(),
      color,
      description: description.trim(),
      exclusiveScope: exclusiveScope.trim(),
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>
              {isEdit ? t('tags.edit_dialog.title') : t('tags.create_dialog.title')}
            </DialogTitle>
            <DialogDescription>
              {isEdit ? t('tags.edit_dialog.description') : t('tags.create_dialog.description')}
            </DialogDescription>
          </DialogHeader>

          <div className="mt-4 space-y-4">
            <div className="space-y-2">
              <Label htmlFor="tag-name">{t('tags.form.name')}</Label>
              <Input
                id="tag-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                maxLength={48}
                required
                autoFocus
              />
            </div>

            <div className="space-y-2">
              <Label>{t('tags.form.color')}</Label>
              <div className="flex flex-wrap gap-1.5">
                {PALETTE.map((c) => (
                  <button
                    key={c}
                    type="button"
                    className={`size-6 rounded-full border-2 transition-transform ${
                      color === c
                        ? 'scale-110 border-foreground'
                        : 'border-transparent hover:scale-105'
                    }`}
                    style={{ backgroundColor: c }}
                    onClick={() => setColor(c)}
                  />
                ))}
              </div>
            </div>

            <div className="space-y-2">
              <Label htmlFor="tag-desc">{t('tags.form.description')}</Label>
              <Input
                id="tag-desc"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                maxLength={200}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="tag-scope">{t('tags.form.exclusive_scope')}</Label>
              <Input
                id="tag-scope"
                value={exclusiveScope}
                onChange={(e) => setExclusiveScope(e.target.value)}
                maxLength={32}
                placeholder={t('tags.form.exclusive_scope_placeholder')}
              />
              <p className="text-xs text-muted-foreground">{t('tags.form.exclusive_scope_help')}</p>
            </div>
          </div>

          <DialogFooter className="mt-6">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={pending || !name.trim()}>
              {pending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              {isEdit ? t('common.save') : t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
