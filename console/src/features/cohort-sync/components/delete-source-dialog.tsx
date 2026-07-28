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

export function DeleteSourceDialog({
  open,
  onOpenChange,
  sourceName,
  pending,
  onConfirm,
}: {
  open: boolean
  onOpenChange: (v: boolean) => void
  sourceName: string
  pending: boolean
  onConfirm: () => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('cohort_sync.source.delete')}</DialogTitle>
          <DialogDescription>
            {t('cohort_sync.source.delete_confirm', { name: sourceName })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            {t('common.cancel')}
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={pending}>
            {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t('common.delete')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
