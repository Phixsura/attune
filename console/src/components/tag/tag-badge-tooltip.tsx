import { useTranslation } from 'react-i18next'
import { TagBadge } from '@/components/tag/tag-badge'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import type { Tag } from '@/proto/attune/v1/tag'

export function TagBadgeTooltip({
  tag,
  onRemove,
  className,
}: {
  tag: Tag
  onRemove?: () => void
  className?: string
}) {
  const { t } = useTranslation()
  const hasTooltipContent = tag.description || tag.exclusiveScope || tag.usageCount > 0

  const badge = (
    <TagBadge name={tag.name} color={tag.color} onRemove={onRemove} className={className} />
  )

  if (!hasTooltipContent) return badge

  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger asChild>{badge}</TooltipTrigger>
        <TooltipContent className="max-w-60 space-y-1.5 p-3">
          {tag.description ? <p className="text-xs">{tag.description}</p> : null}
          <div className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-xs">
            {tag.exclusiveScope ? (
              <>
                <span className="text-muted-foreground">{t('tags.tooltip.scope')}</span>
                <span>{tag.exclusiveScope}</span>
              </>
            ) : null}
            {tag.usageCount > 0 ? (
              <>
                <span className="text-muted-foreground">{t('tags.tooltip.usage')}</span>
                <span>{t('tags.tooltip.usage_count', { count: tag.usageCount })}</span>
              </>
            ) : null}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}
