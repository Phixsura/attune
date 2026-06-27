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

interface UnsavedChangesDialogProps {
  open: boolean
  onConfirmLeave: () => void
  onCancelLeave: () => void
}

export function UnsavedChangesDialog({
  open,
  onConfirmLeave,
  onCancelLeave,
}: UnsavedChangesDialogProps) {
  const { t } = useTranslation()
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v) onCancelLeave()
      }}
    >
      <DialogContent showCloseButton={false} role="alertdialog">
        <DialogHeader>
          <DialogTitle>{t('draft.unsaved_changes_title')}</DialogTitle>
          <DialogDescription>{t('draft.unsaved_changes_body')}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancelLeave}>
            {t('draft.stay')}
          </Button>
          <Button type="button" variant="destructive" onClick={onConfirmLeave}>
            {t('draft.discard_and_leave')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
