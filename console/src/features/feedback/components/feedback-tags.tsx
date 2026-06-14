import { Plus } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { TagBadge } from '@/components/tag/tag-badge'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
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

  const handleAdd = (tag: Tag) => {
    addTag.mutate(
      { tagId: tag.id },
      {
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
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
        {unassigned.length > 0 ? (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="sm" className="h-6 px-2 text-xs">
                <Plus className="mr-1 h-3 w-3" />
                {t('tags.feedback_section.add')}
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              {unassigned.map((tag) => (
                <DropdownMenuItem key={tag.id} onClick={() => handleAdd(tag)}>
                  <span
                    className="mr-2 size-2 rounded-full"
                    style={{ backgroundColor: tag.color }}
                  />
                  {tag.name}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        ) : null}
      </div>

      {tags.length > 0 ? (
        <div className="flex flex-wrap gap-1.5">
          {tags.map((tag) => (
            <TagBadge
              key={tag.id}
              name={tag.name}
              color={tag.color}
              onRemove={() => handleRemove(tag.id)}
            />
          ))}
        </div>
      ) : (
        <p className="text-xs text-muted-foreground">{t('tags.feedback_section.empty')}</p>
      )}
    </div>
  )
}
