import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { NotifyTarget, NotifyTargetCreate } from '@/api/queries'
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

type DestType = 'lark-bot' | 'raw-webhook'

export function CreateNotifyDialog({
  open,
  onOpenChange,
  onSubmit,
  pending,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onSubmit: (body: NotifyTargetCreate) => Promise<unknown>
  pending: boolean
}) {
  const { t } = useTranslation()
  const [type, setType] = useState<DestType>('lark-bot')
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [timeout, setTimeoutSec] = useState(10)

  const reset = () => {
    setType('lark-bot')
    setUrl('')
    setSecret('')
    setTimeoutSec(10)
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) reset()
      }}
    >
      <DialogContent className="sm:max-w-lg">
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (!url.trim()) return
            // openapi-typescript treats fields with `default:` as required
            // even though the server fills them — pass explicitly.
            const body: NotifyTargetCreate = {
              destination_type: type,
              url: url.trim(),
              audience: 'all',
              timeout_seconds: timeout,
              disabled: false,
            }
            if (secret.trim()) body.secret = secret.trim()
            void onSubmit(body).then(() => reset())
          }}
        >
          <DialogHeader>
            <DialogTitle>{t('notify_targets.create_dialog.title')}</DialogTitle>
          </DialogHeader>

          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="nt-type">{t('notify_targets.create_dialog.type_field')}</Label>
              <Select value={type} onValueChange={(v) => setType(v as DestType)} disabled={pending}>
                <SelectTrigger id="nt-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="lark-bot">
                    {t('notify_targets.create_dialog.type_lark')}
                  </SelectItem>
                  <SelectItem value="raw-webhook">
                    {t('notify_targets.create_dialog.type_raw')}
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="nt-url">{t('notify_targets.create_dialog.url_field')}</Label>
              <Input
                id="nt-url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://…"
                disabled={pending}
                required
              />
              <p className="text-xs text-muted-foreground">
                {type === 'lark-bot'
                  ? t('notify_targets.create_dialog.url_help_lark')
                  : t('notify_targets.create_dialog.url_help_raw')}
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="nt-secret">{t('notify_targets.create_dialog.secret_field')}</Label>
              <Input
                id="nt-secret"
                type="password"
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                disabled={pending}
              />
              <p className="text-xs text-muted-foreground">
                {type === 'lark-bot'
                  ? t('notify_targets.create_dialog.secret_help_lark')
                  : t('notify_targets.create_dialog.secret_help_raw')}
              </p>
            </div>

            <div className="space-y-2">
              <Label htmlFor="nt-timeout">{t('notify_targets.create_dialog.timeout_field')}</Label>
              <Input
                id="nt-timeout"
                type="number"
                min={1}
                max={60}
                value={timeout}
                onChange={(e) => setTimeoutSec(Number(e.target.value) || 10)}
                disabled={pending}
              />
            </div>
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="ghost"
              onClick={() => onOpenChange(false)}
              disabled={pending}
            >
              {t('common.cancel')}
            </Button>
            <Button type="submit" disabled={pending || !url.trim()}>
              {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
              {t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function DeleteNotifyDialog({
  target,
  onCancel,
  onConfirm,
  pending,
}: {
  target: NotifyTarget | null
  onCancel: () => void
  onConfirm: () => void
  pending: boolean
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={target !== null} onOpenChange={(v) => !v && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('notify_targets.delete_confirm_title')}</DialogTitle>
          <DialogDescription>{t('notify_targets.delete_confirm_body')}</DialogDescription>
        </DialogHeader>
        {target && (
          <p className="my-2 break-all font-mono text-xs text-muted-foreground">{target.url}</p>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={onCancel} disabled={pending}>
            {t('common.cancel')}
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={pending}>
            {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
            {t('notify_targets.delete_button')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
