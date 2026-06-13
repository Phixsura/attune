import { Loader2 } from 'lucide-react'
import { useState } from 'react'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { NotifyTargetCreate } from '@/features/notify-targets/api/create-notify-target'
import type { NotifyTarget } from '@/features/notify-targets/api/list-notify-targets'

// raw-webhook is the only outbound destination shipped in v0.3. #34
// will reintroduce a typed select when the outbound-adapter SDK lands.
const FIXED_DEST_TYPE = 'raw-webhook' as const

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
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [timeout, setTimeoutSec] = useState(10)
  const [audience, setAudience] = useState<'pool' | 'radar' | 'all' | 'digest'>('all')

  const reset = () => {
    setUrl('')
    setSecret('')
    setTimeoutSec(10)
    setAudience('all')
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
            // Pass server-side defaults explicitly so the wire body is
            // self-describing — the API also fills these if omitted.
            const body: NotifyTargetCreate = {
              destinationType: FIXED_DEST_TYPE,
              url: url.trim(),
              audience,
              timeoutSeconds: timeout,
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
                {t('notify_targets.create_dialog.url_help_raw')}
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
                {t('notify_targets.create_dialog.secret_help_raw')}
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

            <div className="space-y-2">
              <Label htmlFor="nt-audience">
                {t('notify_targets.create_dialog.audience_field')}
              </Label>
              <Select
                value={audience}
                onValueChange={(v) => setAudience(v as 'pool' | 'radar' | 'all' | 'digest')}
                disabled={pending}
              >
                <SelectTrigger id="nt-audience">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">all</SelectItem>
                  <SelectItem value="pool">pool</SelectItem>
                  <SelectItem value="radar">radar</SelectItem>
                  <SelectItem value="digest">digest</SelectItem>
                </SelectContent>
              </Select>
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
