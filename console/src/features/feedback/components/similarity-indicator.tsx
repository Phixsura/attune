import { useTranslation } from 'react-i18next'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

interface SimilarityIndicatorProps {
  similarity: number // 0-1
  matchType?: 'semantic' | 'keyword' | 'hybrid'
  showPercentage?: boolean
  size?: 'sm' | 'md' | 'lg'
  className?: string
}

export function SimilarityIndicator({
  similarity,
  matchType = 'semantic',
  showPercentage = true,
  size = 'md',
  className,
}: SimilarityIndicatorProps) {
  const { t } = useTranslation()
  const clamped = Math.max(0, Math.min(1, similarity))
  const percentage = Math.round(clamped * 100)

  const getColor = () => {
    if (percentage >= 80) return 'bg-emerald-500'
    if (percentage >= 60) return 'bg-amber-500'
    if (percentage >= 40) return 'bg-orange-500'
    return 'bg-red-500'
  }

  const getLabel = () => {
    if (percentage >= 80) return t('feedback.similarity.high')
    if (percentage >= 60) return t('feedback.similarity.medium')
    if (percentage >= 40) return t('feedback.similarity.low')
    return t('feedback.similarity.very_low')
  }

  const sizeClasses = {
    sm: 'h-1.5 w-12',
    md: 'h-2 w-16',
    lg: 'h-2.5 w-20',
  }

  const textSizeClasses = {
    sm: 'text-xs',
    md: 'text-sm',
    lg: 'text-base',
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <div className={cn('flex items-center gap-2', className)}>
          {/* Progress bar */}
          <div className={cn('overflow-hidden rounded-full bg-muted', sizeClasses[size])}>
            <div
              className={cn('h-full rounded-full transition-all', getColor())}
              style={{ width: `${percentage}%` }}
            />
          </div>

          {/* Percentage text */}
          {showPercentage && (
            <span className={cn('font-medium text-muted-foreground', textSizeClasses[size])}>
              {percentage}%
            </span>
          )}
        </div>
      </TooltipTrigger>
      <TooltipContent>
        <p>{getLabel()}</p>
        <p className="text-xs text-muted-foreground">
          {matchType === 'semantic' && t('feedback.similarity.semantic_match')}
          {matchType === 'keyword' && t('feedback.similarity.keyword_match')}
          {matchType === 'hybrid' && t('feedback.similarity.hybrid_match')}
        </p>
      </TooltipContent>
    </Tooltip>
  )
}
