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

interface UnsavedChangesDialogProps {
  open: boolean
  onConfirmLeave: () => void
  onCancelLeave: () => void
  onSaveAndLeave?: () => void
  saving?: boolean
  changeCount?: number
}

export function UnsavedChangesDialog({
  open,
  onConfirmLeave,
  onCancelLeave,
  onSaveAndLeave,
  saving = false,
  changeCount,
}: UnsavedChangesDialogProps) {
  const { t } = useTranslation()
  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        if (!v && !saving) onCancelLeave()
      }}
    >
      <DialogContent showCloseButton={false} role="alertdialog">
        <DialogHeader>
          <DialogTitle>{t('draft.unsaved_changes_title')}</DialogTitle>
          <DialogDescription>
            {changeCount
              ? t('draft.unsaved_changes_body_count', { count: changeCount })
              : t('draft.unsaved_changes_body')}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onCancelLeave} disabled={saving}>
            {t('draft.stay')}
          </Button>
          {onSaveAndLeave && (
            <Button type="button" onClick={onSaveAndLeave} disabled={saving} aria-busy={saving}>
              {saving ? (
                <>
                  <Loader2 className="mr-2 size-4 animate-spin" />
                  {t('draft.status_saving')}
                </>
              ) : (
                t('draft.save_and_leave')
              )}
            </Button>
          )}
          <Button type="button" variant="destructive" onClick={onConfirmLeave} disabled={saving}>
            {t('draft.discard_and_leave')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
