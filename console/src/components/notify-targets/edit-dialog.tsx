import { Loader2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { NotifyTarget, NotifyTargetPatch } from '@/api/queries'
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

// EditNotifyDialog edits an existing notify-target.
// destination_type is locked (changing type = different identity; the
// server's PATCH schema doesn't accept it either).
//
// Sparse submit: only fields the user changed are sent. URL/audience/
// timeout/disabled compare against the original snapshot; secret is
// special — empty string sent only when the user explicitly cleared
// the masked field (we detect that via the cleared bit).
export function EditNotifyDialog({
  target,
  onClose,
  onSubmit,
  pending,
}: {
  target: NotifyTarget | null
  onClose: () => void
  onSubmit: (patch: NotifyTargetPatch) => Promise<unknown>
  pending: boolean
}) {
  const { t } = useTranslation()
  const [url, setUrl] = useState('')
  const [audience, setAudience] = useState<'pool' | 'radar' | 'all'>('all')
  const [timeout, setTimeoutSec] = useState(10)
  const [disabled, setDisabled] = useState(false)
  const [secret, setSecret] = useState('')
  const [secretCleared, setSecretCleared] = useState(false)

  // Re-seed fields whenever a different target is selected for editing.
  useEffect(() => {
    if (!target) return
    setUrl(target.url ?? '')
    setAudience(target.audience as 'pool' | 'radar' | 'all')
    setTimeoutSec(target.timeout_seconds ?? 10)
    setDisabled(target.disabled)
    setSecret('')
    setSecretCleared(false)
  }, [target])

  if (!target) return null

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    const patch: NotifyTargetPatch = {}
    if (url.trim() !== (target.url ?? '')) patch.url = url.trim()
    if (audience !== target.audience) patch.audience = audience
    if (timeout !== (target.timeout_seconds ?? 10)) patch.timeout_seconds = timeout
    if (disabled !== target.disabled) patch.disabled = disabled
    if (secret) patch.secret = secret
    else if (secretCleared) patch.secret = ''
    if (Object.keys(patch).length === 0) {
      onClose()
      return
    }
    void onSubmit(patch)
  }

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>{t('notify_targets.edit_dialog.title')}</DialogTitle>
            <DialogDescription>
              {t('notify_targets.edit_dialog.locked_type', {
                type: target.destination_type,
              })}
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="ent-url">{t('notify_targets.create_dialog.url_field')}</Label>
              <Input
                id="ent-url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                disabled={pending}
                required
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="ent-audience">
                {t('notify_targets.create_dialog.audience_field')}
              </Label>
              <Select
                value={audience}
                onValueChange={(v) => setAudience(v as 'pool' | 'radar' | 'all')}
                disabled={pending}
              >
                <SelectTrigger id="ent-audience">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">all</SelectItem>
                  <SelectItem value="pool">pool</SelectItem>
                  <SelectItem value="radar">radar</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="ent-secret">{t('notify_targets.create_dialog.secret_field')}</Label>
              <Input
                id="ent-secret"
                type="password"
                placeholder={t('notify_targets.edit_dialog.secret_placeholder')}
                value={secret}
                onChange={(e) => {
                  setSecret(e.target.value)
                  setSecretCleared(false)
                }}
                disabled={pending || secretCleared}
              />
              <label className="flex items-center gap-2 text-xs text-muted-foreground">
                <input
                  type="checkbox"
                  checked={secretCleared}
                  onChange={(e) => {
                    setSecretCleared(e.target.checked)
                    if (e.target.checked) setSecret('')
                  }}
                  disabled={pending}
                />
                {t('notify_targets.edit_dialog.secret_clear')}
              </label>
            </div>

            <div className="space-y-2">
              <Label htmlFor="ent-timeout">{t('notify_targets.create_dialog.timeout_field')}</Label>
              <Input
                id="ent-timeout"
                type="number"
                min={1}
                max={60}
                value={timeout}
                onChange={(e) => setTimeoutSec(Number(e.target.value))}
                disabled={pending}
              />
            </div>

            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={disabled}
                onChange={(e) => setDisabled(e.target.checked)}
                disabled={pending}
              />
              {t('notify_targets.edit_dialog.disabled_field')}
            </label>
          </div>

          <DialogFooter>
            <Button type="button" variant="ghost" onClick={onClose} disabled={pending}>
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={pending}>
              {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
              {t('common.save')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
