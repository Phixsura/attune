import { Tags, Trash2, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { TagCombobox } from '@/components/tag/tag-combobox'
import { Button } from '@/components/ui/button'
import type { Tag } from '@/proto/attune/v1/tag'

export function SelectionActionBar({
  count,
  availableTags,
  removableTags,
  onBatchAdd,
  onBatchRemove,
  onCancel,
}: {
  count: number
  availableTags: Tag[]
  removableTags: Tag[]
  onBatchAdd: (tagId: string) => void
  onBatchRemove: (tagId: string) => void
  onCancel: () => void
}) {
  const { t } = useTranslation()

  if (count === 0) return null

  return (
    <div className="flex items-center gap-3 rounded-lg bg-primary px-4 py-2 text-primary-foreground">
      <span className="text-sm font-medium">{t('feedback.batch.selected', { count })}</span>
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
      <button
        type="button"
        onClick={onCancel}
        className="ml-auto rounded-full p-1 text-primary-foreground/70 transition-colors hover:text-primary-foreground"
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  )
}
