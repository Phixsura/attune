import { Copy, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import type { ApiKey, NewApiKey } from '@/api/queries'
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

export function CreateKeyDialog({
  open,
  onOpenChange,
  onSubmit,
  pending,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  onSubmit: (label: string) => Promise<unknown>
  pending: boolean
}) {
  const { t } = useTranslation()
  const [label, setLabel] = useState('')
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            if (label.trim()) void onSubmit(label.trim()).then(() => setLabel(''))
          }}
        >
          <DialogHeader>
            <DialogTitle>{t('api_keys.create_dialog.title')}</DialogTitle>
            <DialogDescription>{t('api_keys.create_dialog.label_help')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-2 py-4">
            <Label htmlFor="api-key-label">{t('api_keys.create_dialog.label_field')}</Label>
            <Input
              id="api-key-label"
              value={label}
              onChange={(e) => setLabel(e.target.value)}
              placeholder={t('api_keys.create_dialog.label_placeholder')}
              maxLength={200}
              autoFocus
              disabled={pending}
            />
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
            <Button type="submit" disabled={pending || !label.trim()}>
              {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
              {t('common.create')}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

export function SecretKeyDialog({
  issued,
  onClose,
}: {
  issued: NewApiKey | null
  onClose: () => void
}) {
  const { t } = useTranslation()
  const onCopy = () => {
    if (!issued) return
    void navigator.clipboard.writeText(issued.secret).then(() => {
      toast.success(t('api_keys.secret_dialog.copy_hint'))
    })
  }
  return (
    <Dialog open={issued !== null} onOpenChange={(v) => !v && onClose()}>
      <DialogContent onPointerDownOutside={(e) => e.preventDefault()}>
        <DialogHeader>
          <DialogTitle>{t('api_keys.secret_dialog.title')}</DialogTitle>
          <DialogDescription>{t('api_keys.secret_dialog.body')}</DialogDescription>
        </DialogHeader>
        {issued && (
          <div className="my-4 flex items-center gap-2 rounded-md border bg-muted/40 px-3 py-2">
            <code className="flex-1 break-all font-mono text-xs">{issued.secret}</code>
            <Button size="sm" variant="ghost" onClick={onCopy}>
              <Copy className="h-3.5 w-3.5" />
            </Button>
          </div>
        )}
        <DialogFooter>
          <Button onClick={onClose}>{t('common.close')}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

export function RevokeKeyDialog({
  target,
  onCancel,
  onConfirm,
  pending,
}: {
  target: ApiKey | null
  onCancel: () => void
  onConfirm: () => void
  pending: boolean
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={target !== null} onOpenChange={(v) => !v && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('api_keys.revoke_confirm_title')}</DialogTitle>
          <DialogDescription>{t('api_keys.revoke_confirm_body')}</DialogDescription>
        </DialogHeader>
        {target && (
          <p className="my-2 font-mono text-sm text-muted-foreground">
            {target.key_prefix}… &nbsp; · &nbsp; {target.label || '—'}
          </p>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={onCancel} disabled={pending}>
            {t('common.cancel')}
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={pending}>
            {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
            {t('api_keys.revoke_button')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
