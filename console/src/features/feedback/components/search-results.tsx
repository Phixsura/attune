import { useTranslation } from 'react-i18next'
import { Card } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type { SemanticSearchHit } from '@/proto/attune/v1/search'

interface SearchResultsProps {
  results: SemanticSearchHit[]
  usedKeywordFallback?: boolean
  onSelectFeedback?: (feedbackId: string) => void
}

export function SearchResults({
  results,
  usedKeywordFallback,
  onSelectFeedback,
}: SearchResultsProps) {
  const { t } = useTranslation()

  if (results.length === 0) {
    return (
      <div className="py-8 text-center text-muted-foreground">
        {t('feedback.search.no_results')}
      </div>
    )
  }

  return (
    <div className="space-y-2">
      {usedKeywordFallback && (
        <div className="mb-2 inline-flex items-center rounded-md border border-border bg-muted/40 px-2 py-0.5 text-xs font-medium text-muted-foreground">
          {t('feedback.search.keyword_fallback')}
        </div>
      )}

      {results.map((hit) => (
        <Card
          key={hit.feedback?.id}
          className="cursor-pointer gap-0 p-3 hover:bg-accent"
          onClick={() => onSelectFeedback?.(hit.feedback?.id ?? '')}
        >
          <div className="flex items-start justify-between">
            <div className="flex-1">
              <p className="font-medium">{hit.feedback?.enrichedTitle || t('feedback.untitled')}</p>
              <p className="line-clamp-2 text-sm text-muted-foreground">{hit.feedback?.content}</p>
            </div>
            <SimilarityBadge similarity={hit.similarity} matchType={hit.matchType} />
          </div>
        </Card>
      ))}
    </div>
  )
}

function SimilarityBadge({ similarity, matchType }: { similarity: number; matchType?: string }) {
  const { t } = useTranslation()
  const percentage = Math.round(similarity * 100)
  const variant = percentage >= 80 ? 'high' : percentage >= 60 ? 'medium' : 'low'

  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-md border px-2 py-0.5 text-xs font-medium',
        variant === 'high' &&
          'border-green-200 bg-green-50 text-green-700 dark:border-green-800 dark:bg-green-950 dark:text-green-300',
        variant === 'medium' &&
          'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-300',
        variant === 'low' && 'border-border bg-muted/40 text-muted-foreground',
      )}
      title={matchType ? t(`feedback.search.match_type.${matchType}`) : undefined}
    >
      {percentage}%
    </span>
  )
}
