import { Key, Loader2 } from 'lucide-react'
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
import type { InboundSource } from '@/features/inbound-sources/api/list-inbound-sources'

// RotateConfirmDialog — explicit "are you sure" before generating a new
// HMAC secret. The 24h dual-secret grace window means the old secret
// keeps working for a day after rotation — we surface that in the body
// so the operator knows their automation has time to update.
export function RotateConfirmDialog({
  source,
  onCancel,
  onConfirm,
  pending,
}: {
  source: InboundSource | null
  onCancel: () => void
  onConfirm: () => void
  pending: boolean
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={source !== null} onOpenChange={(v) => !v && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Key className="h-4 w-4" />
            {t('inbound_sources.rotate.title')}
          </DialogTitle>
          <DialogDescription>{t('inbound_sources.rotate.body')}</DialogDescription>
        </DialogHeader>
        {source && (
          <p className="my-2 break-all font-mono text-xs text-muted-foreground">{source.name}</p>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={onCancel} disabled={pending}>
            {t('common.cancel')}
          </Button>
          <Button onClick={onConfirm} disabled={pending}>
            {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
            {t('inbound_sources.rotate.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
