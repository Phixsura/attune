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

// #34 outbound adapter framework: typed destination select with per-channel help.
const DEST_TYPES = [
  {
    value: 'raw-webhook',
    label: 'Raw Webhook',
    urlHelp: 'Your HTTPS endpoint that receives POST requests',
    secretHelp: 'HMAC secret for signature verification',
  },
  {
    value: 'github-issue',
    label: 'GitHub Issue',
    urlHelp: 'GitHub repo URL (https://github.com/owner/repo)',
    secretHelp: 'GitHub PAT with issues:write scope',
  },
  {
    value: 'lark',
    label: 'Lark (Feishu)',
    urlHelp: 'Lark custom bot webhook URL',
    secretHelp: 'Bot signature secret (optional)',
  },
  {
    value: 'slack',
    label: 'Slack',
    urlHelp: 'Slack incoming webhook URL',
    secretHelp: 'Not used — URL is the secret',
  },
  {
    value: 'discord',
    label: 'Discord',
    urlHelp: 'Discord channel webhook URL',
    secretHelp: 'Not used — URL is the secret',
  },
] as const

type DestType = (typeof DEST_TYPES)[number]['value']

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
  const [destType, setDestType] = useState<DestType>('raw-webhook')
  const [url, setUrl] = useState('')
  const [secret, setSecret] = useState('')
  const [timeout, setTimeoutSec] = useState(10)
  const [audience, setAudience] = useState<'pool' | 'radar' | 'all' | 'digest'>('all')

  const selectedDest = DEST_TYPES.find((d) => d.value === destType) ?? DEST_TYPES[0]

  const reset = () => {
    setDestType('raw-webhook')
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
              destinationType: destType,
              url: url.trim(),
              audience,
              timeoutSeconds: timeout,
              disabled: false,
            }
            if (secret.trim()) body.secret = secret.trim()
            void onSubmit(body)
              .then(() => reset())
              .catch(() => {
                // The page-level mutation owns the toast; keep the dialog open.
              })
          }}
        >
          <DialogHeader>
            <DialogTitle>{t('notify_targets.create_dialog.title')}</DialogTitle>
          </DialogHeader>

          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="nt-dest-type">
                {t('notify_targets.create_dialog.dest_type_field')}
              </Label>
              <Select
                value={destType}
                onValueChange={(v) => setDestType(v as DestType)}
                disabled={pending}
              >
                <SelectTrigger id="nt-dest-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {DEST_TYPES.map((d) => (
                    <SelectItem key={d.value} value={d.value}>
                      {d.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="space-y-2">
              <Label htmlFor="nt-url">{t('notify_targets.create_dialog.url_field')}</Label>
              <Input
                id="nt-url"
                data-testid="create-notify-url"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://…"
                disabled={pending}
                required
              />
              <p className="text-xs text-muted-foreground">{selectedDest.urlHelp}</p>
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
              <p className="text-xs text-muted-foreground">{selectedDest.secretHelp}</p>
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
            <Button
              type="submit"
              disabled={pending || !url.trim()}
              data-testid="create-notify-submit"
            >
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
          <Button
            variant="destructive"
            onClick={onConfirm}
            disabled={pending}
            data-testid="delete-notify-confirm"
          >
            {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
            {t('notify_targets.delete_button')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
