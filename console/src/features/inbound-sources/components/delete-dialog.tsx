import { Loader2 } from 'lucide-react'
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

// DeleteInboundSourceDialog — mirrors the notify-targets delete shape.
// The destructive button copy is intentionally explicit since deleting
// a webhook source invalidates the public URL immediately.
export function DeleteInboundSourceDialog({
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
          <DialogTitle>{t('inbound_sources.delete.title')}</DialogTitle>
          <DialogDescription>{t('inbound_sources.delete.body')}</DialogDescription>
        </DialogHeader>
        {source && (
          <p className="my-2 break-all font-mono text-xs text-muted-foreground">{source.name}</p>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={onCancel} disabled={pending}>
            {t('common.cancel')}
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={pending}>
            {pending && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
            {t('inbound_sources.delete.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
