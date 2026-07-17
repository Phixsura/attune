import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { I18nInput } from '@/components/dim/i18n-input'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { I18nString } from '@/lib/i18n-resolve'
import { cn } from '@/lib/utils'
import type { WorkflowState } from '@/proto/attune/v1/workflow'

const PALETTE = [
  '#3b82f6',
  '#06b6d4',
  '#22c55e',
  '#f59e0b',
  '#f97316',
  '#ef4444',
  '#8b5cf6',
  '#ec4899',
  '#6b7280',
]

const CATEGORIES = ['open', 'active', 'closed'] as const

// SLUG mirrors the proto-side WorkflowState.name contract (and
// Dimension.name): a stable machine key, lowercase + digits +
// underscore, ≤31 chars. It is required and editable only on CREATE;
// immutable on edit (relabel via displayName instead).
const SLUG = /^[a-z][a-z0-9_]{0,30}$/

// StateFormData is what the dialog reports up. `displayName` is the raw
// per-locale map (the I18nInput shape); the page wraps it into the proto
// `{ entries }` envelope when building the request.
export type StateFormData = {
  name: string
  displayName: I18nString
  color: string
  category: string
}

export function StateFormDialog({
  open,
  state,
  pending,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  state?: WorkflowState
  pending: boolean
  onOpenChange: (v: boolean) => void
  onSubmit: (data: StateFormData) => void
}) {
  const { t } = useTranslation()
  const isEdit = !!state
  const [name, setName] = useState('')
  const [displayName, setDisplayName] = useState<I18nString>({ default: '' })
  const [color, setColor] = useState(PALETTE[0])
  const [category, setCategory] = useState<string>(CATEGORIES[0])

  useEffect(() => {
    if (open) {
      setName(state?.name ?? '')
      setDisplayName((state?.displayName?.entries ?? { default: '' }) as I18nString)
      setColor(state?.color ?? PALETTE[0])
      setCategory(state?.category ?? CATEGORIES[0])
    }
  }, [open, state])

  // The key is the only blocker for submit on create; on edit it is
  // locked, so the form is always submittable (display/color edits).
  const keyValid = isEdit || SLUG.test(name.trim())

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    /* v8 ignore next -- @preserve: submit is disabled until the state key is valid. */
    if (!keyValid) return
    onSubmit({ name: name.trim(), displayName, color, category })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>
              {isEdit
                ? t('workflow.state_dialog.edit_title')
                : t('workflow.state_dialog.create_title')}
            </DialogTitle>
            <DialogDescription>
              {isEdit
                ? t('workflow.state_dialog.edit_description')
                : t('workflow.state_dialog.create_description')}
            </DialogDescription>
          </DialogHeader>

          <div className="mt-4 space-y-4">
            <div className="space-y-2">
              <Label htmlFor="state-key">{t('workflow.state_dialog.key')}</Label>
              <Input
                id="state-key"
                value={name}
                readOnly={isEdit}
                onChange={(e) => setName(e.target.value)}
                placeholder="lowercase_only"
                maxLength={31}
                required
                autoFocus={!isEdit}
                className={cn('font-mono text-sm', isEdit && 'opacity-70')}
              />
              <p className="text-xs text-muted-foreground">
                {isEdit
                  ? t('workflow.state_dialog.key_locked')
                  : t('workflow.state_dialog.key_help')}
              </p>
            </div>

            <div className="space-y-2">
              <Label>{t('workflow.state_dialog.display_name')}</Label>
              <I18nInput value={displayName} onChange={setDisplayName} placeholder={name} />
              <p className="text-xs text-muted-foreground">
                {t('workflow.state_dialog.display_name_help')}
              </p>
            </div>

            <div className="space-y-2">
              <Label>{t('workflow.state_dialog.color')}</Label>
              <div className="flex flex-wrap gap-1.5">
                {PALETTE.map((c) => (
                  <button
                    key={c}
                    type="button"
                    aria-label={c}
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
              <Label htmlFor="state-category">{t('workflow.state_dialog.category')}</Label>
              <Select value={category} onValueChange={setCategory} disabled={isEdit}>
                <SelectTrigger id="state-category">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {CATEGORIES.map((c) => (
                    <SelectItem key={c} value={c}>
                      {t(`workflow.categories.${c}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {isEdit && (
                <p className="text-xs text-muted-foreground">
                  {t('workflow.state_dialog.category_locked')}
                </p>
              )}
            </div>
          </div>

          <DialogFooter className="mt-6">
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={pending || !keyValid}>
              {pending ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              {isEdit ? t('common.save') : t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
