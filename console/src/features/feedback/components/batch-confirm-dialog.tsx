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

type BatchOperation = 'tag' | 'workflow' | 'delete'

interface BatchConfirmDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  operation: BatchOperation
  matchedCount: number
  expectedCount?: number // For confirm_count validation
  onConfirm: () => void
  isLoading?: boolean
}

// BatchConfirmDialog — general confirmation for query-based batch operations.
// Used for tag assignment, workflow transitions, and delete operations on
// filtered result sets. Shows a warning when server-side count differs from
// the client's expected count to prevent unintended large-scale changes.
export function BatchConfirmDialog({
  open,
  onOpenChange,
  operation,
  matchedCount,
  expectedCount,
  onConfirm,
  isLoading,
}: BatchConfirmDialogProps) {
  const { t } = useTranslation()

  // Show warning if matchedCount != expectedCount
  const countMismatch = expectedCount !== undefined && matchedCount !== expectedCount

  const getTitleKey = () => {
    switch (operation) {
      case 'tag':
        return 'feedback.batch.confirm_tag_title'
      case 'workflow':
        return 'feedback.batch.confirm_workflow_title'
      case 'delete':
        return 'feedback.batch.confirm_delete_title'
    }
  }

  const getDescriptionKey = () => {
    switch (operation) {
      case 'tag':
        return 'feedback.batch.confirm_tag_description'
      case 'workflow':
        return 'feedback.batch.confirm_workflow_description'
      case 'delete':
        return 'feedback.batch.confirm_delete_description'
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t(getTitleKey())}</DialogTitle>
          <DialogDescription>{t(getDescriptionKey(), { count: matchedCount })}</DialogDescription>
        </DialogHeader>
        {countMismatch && (
          <p className="text-sm text-amber-600">
            {t('feedback.batch.count_mismatch_warning', {
              expected: expectedCount,
              actual: matchedCount,
            })}
          </p>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={isLoading}>
            {t('common.cancel')}
          </Button>
          <Button
            variant={operation === 'delete' ? 'destructive' : 'default'}
            onClick={onConfirm}
            disabled={isLoading || countMismatch}
            data-testid="batch-confirm-action"
          >
            {isLoading && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
            {isLoading ? t('common.processing') : t('common.confirm')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
