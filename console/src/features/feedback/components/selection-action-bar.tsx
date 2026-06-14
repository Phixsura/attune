import { ArrowRight, Loader2, Tags, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { TagCombobox } from '@/components/tag/tag-combobox'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { Tag } from '@/proto/attune/v1/tag'
import type { WorkflowState } from '@/proto/attune/v1/workflow'

export function SelectionActionBar({
  count,
  availableTags,
  removableTags,
  workflowStates,
  onBatchAdd,
  onBatchRemove,
  onBatchTransition,
  onBatchDelete,
  onCancel,
  isLoading,
  loadingMessage,
  totalMatching,
  onSelectAll,
}: {
  count: number
  availableTags: Tag[]
  removableTags: Tag[]
  workflowStates?: WorkflowState[]
  onBatchAdd: (tagId: string) => void
  onBatchRemove: (tagId: string) => void
  onBatchTransition?: (toStateId: string) => void
  onBatchDelete?: () => void
  onCancel: () => void
  isLoading?: boolean
  loadingMessage?: string
  totalMatching?: number
  onSelectAll?: () => void
}) {
  const { t } = useTranslation()

  if (count === 0) return null

  const activeStates = workflowStates?.filter((s) => !s.archived) ?? []

  // Show loading state when a batch operation is in progress
  if (isLoading) {
    return (
      <div className="flex items-center gap-3 rounded-lg bg-primary px-4 py-2 text-primary-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        <span className="text-sm font-medium">{loadingMessage ?? t('app.loading')}</span>
        <button
          type="button"
          onClick={onCancel}
          aria-label={t('common.cancel')}
          className="ml-auto rounded-full p-1 text-primary-foreground/70 transition-colors hover:text-primary-foreground"
        >
          <X className="h-4 w-4" />
        </button>
      </div>
    )
  }

  return (
    <div className="flex flex-wrap items-center gap-3 rounded-lg bg-primary px-4 py-2 text-primary-foreground">
      <span className="text-sm font-medium">{t('feedback.batch.selected', { count })}</span>

      {/* Select all matching filter link */}
      {totalMatching !== undefined && totalMatching > count && onSelectAll && (
        <button
          type="button"
          onClick={onSelectAll}
          className="text-sm underline underline-offset-2 opacity-80 transition-opacity hover:opacity-100"
        >
          {t('feedback.batch.select_all_matching', { count: totalMatching })}
        </button>
      )}

      <TagCombobox
        availableTags={availableTags}
        onSelect={onBatchAdd}
        trigger={
          <Button variant="secondary" size="sm" className="h-7 gap-1.5 text-xs">
            <Tags className="h-3.5 w-3.5" />
            {t('feedback.batch.add_tag')}
          </Button>
        }
      />
      {removableTags.length > 0 ? (
        <TagCombobox
          availableTags={removableTags}
          onSelect={onBatchRemove}
          trigger={
            <Button variant="secondary" size="sm" className="h-7 gap-1.5 text-xs">
              <Trash2 className="h-3.5 w-3.5" />
              {t('feedback.batch.remove_tag')}
            </Button>
          }
        />
      ) : null}
      {activeStates.length > 0 && onBatchTransition ? (
        <Select onValueChange={onBatchTransition}>
          <SelectTrigger className="h-7 w-auto gap-1.5 border-none bg-secondary text-xs text-secondary-foreground">
            <ArrowRight className="h-3.5 w-3.5" />
            <SelectValue placeholder={t('feedback.batch.transition')} />
          </SelectTrigger>
          <SelectContent>
            {activeStates.map((s) => (
              <SelectItem key={s.id} value={s.id}>
                <span className="flex items-center gap-2">
                  <span
                    className="inline-block size-2 rounded-full"
                    style={{ backgroundColor: s.color }}
                  />
                  {s.name}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      ) : null}

      {/* Delete button */}
      {onBatchDelete && (
        <Button
          variant="destructive"
          size="sm"
          className="h-7 gap-1.5 text-xs"
          onClick={onBatchDelete}
        >
          <Trash2 className="h-3.5 w-3.5" />
          {t('feedback.batch.delete')}
        </Button>
      )}

      <button
        type="button"
        onClick={onCancel}
        aria-label={t('common.cancel')}
        className="ml-auto rounded-full p-1 text-primary-foreground/70 transition-colors hover:text-primary-foreground"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  )
}
