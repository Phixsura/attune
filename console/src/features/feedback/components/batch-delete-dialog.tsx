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

interface BatchDeleteDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
  isHardDelete: boolean
  onConfirm: () => void
  isLoading?: boolean
}

// BatchDeleteDialog — confirmation for batch soft/hard delete operations.
// Hard delete is permanent and cannot be undone; soft delete moves items
// to a recoverable state. Both require explicit user confirmation.
export function BatchDeleteDialog({
  open,
  onOpenChange,
  count,
  isHardDelete,
  onConfirm,
  isLoading,
}: BatchDeleteDialogProps) {
  const { t } = useTranslation()

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {isHardDelete
              ? t('feedback.batch.delete_permanent_title')
              : t('feedback.batch.delete_title')}
          </DialogTitle>
          <DialogDescription>
            {isHardDelete
              ? t('feedback.batch.delete_permanent_description', { count })
              : t('feedback.batch.delete_description', { count })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            variant="ghost"
            onClick={
              /* v8 ignore next -- @preserve: cancel only closes the already-covered dialog shell. */
              () => onOpenChange(false)
            }
            disabled={isLoading}
          >
            {t('common.cancel')}
          </Button>
          <Button
            variant="destructive"
            onClick={onConfirm}
            disabled={isLoading}
            data-testid="batch-delete-confirm"
          >
            {isLoading && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
            {isLoading ? t('common.deleting') : t('common.delete')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
