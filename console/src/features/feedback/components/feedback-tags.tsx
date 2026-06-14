import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { TagBadgeTooltip } from '@/components/tag/tag-badge-tooltip'
import { TagCombobox } from '@/components/tag/tag-combobox'
import { useAddFeedbackTag } from '@/features/feedback/api/add-feedback-tag'
import { useRemoveFeedbackTag } from '@/features/feedback/api/remove-feedback-tag'
import type { Tag } from '@/proto/attune/v1/tag'

export function FeedbackTagSection({
  feedbackId,
  tags,
  availableTags,
}: {
  feedbackId: string
  tags: Tag[]
  availableTags: Tag[]
}) {
  const { t } = useTranslation()
  const addTag = useAddFeedbackTag(feedbackId)
  const removeTag = useRemoveFeedbackTag(feedbackId)

  const assignedIds = new Set(tags.map((tag) => tag.id))
  const unassigned = availableTags.filter((tag) => !assignedIds.has(tag.id))

  const handleSelect = (tagId: string) => {
    addTag.mutate(
      { tagId },
      { onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')) },
    )
  }

  const handleCreate = (name: string) => {
    addTag.mutate(
      { tagName: name },
      { onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')) },
    )
  }

  const handleRemove = (tagId: string) => {
    removeTag.mutate(tagId, {
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
    })
  }

  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <h4 className="text-xs font-medium uppercase tracking-wide text-muted-foreground">
          {t('tags.feedback_section.title')}
        </h4>
        <TagCombobox availableTags={unassigned} onSelect={handleSelect} onCreate={handleCreate} />
      </div>

      {tags.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {tags.map((tag) => (
            <TagBadgeTooltip key={tag.id} tag={tag} onRemove={() => handleRemove(tag.id)} />
          ))}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{t('tags.feedback_section.empty')}</p>
      )}
    </div>
  )
}
