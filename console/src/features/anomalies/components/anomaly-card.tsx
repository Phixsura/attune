import { TrendingDown, TrendingUp } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { AnomalyEvent } from '@/features/anomalies/api/anomalies'
import { cn } from '@/lib/utils'

interface AnomalyCardProps {
  event: AnomalyEvent
  selected?: boolean
  onSelect?: (event: AnomalyEvent) => void
}

/** ongoingDays counts inclusive days between first and last bucket. */
function ongoingDays(event: AnomalyEvent): number {
  const first = new Date(`${event.firstBucketDate}T00:00:00Z`).getTime()
  const last = new Date(`${event.lastBucketDate}T00:00:00Z`).getTime()
  return Math.round((last - first) / 86_400_000) + 1
}

export function AnomalyCard({ event, onSelect, selected }: AnomalyCardProps) {
  const { t } = useTranslation()
  const spike = event.direction === 'spike'
  const days = ongoingDays(event)
  const med = Math.round(event.expectedMed * 10) / 10
  const low = Math.round(event.expectedLow * 10) / 10
  const high = Math.round(event.expectedHigh * 10) / 10
  return (
    <button
      className={cn(
        'w-full rounded-lg border p-3 text-left transition-colors hover:bg-accent',
        selected && 'border-primary bg-accent',
      )}
      data-testid="anomaly-card"
      onClick={() => onSelect?.(event)}
      type="button"
    >
      <div className="flex items-center gap-2">
        {spike ? (
          <TrendingUp aria-hidden className="h-4 w-4 text-red-500" />
        ) : (
          <TrendingDown aria-hidden className="h-4 w-4 text-amber-500" />
        )}
        <span className="truncate font-medium">{event.sliceDisplay || event.sliceKey}</span>
        {event.status === 'retracted' ? (
          <span className="ml-auto rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
            {t('anomalies.status.retracted', 'retracted after data correction')}
          </span>
        ) : days > 1 ? (
          <span className="ml-auto rounded bg-amber-500/15 px-1.5 py-0.5 text-xs text-amber-600">
            {t('anomalies.card.ongoing', 'ongoing {{days}}d', { days })}
          </span>
        ) : null}
      </div>
      <p className="mt-1 text-sm text-muted-foreground">
        {t(
          'anomalies.card.observed',
          'observed {{observed}}, expected {{med}} ({{low}}–{{high}})',
          {
            high,
            low,
            med,
            observed: Number(event.observed),
          },
        )}
      </p>
      <p className="mt-0.5 text-xs text-muted-foreground">
        {event.lastBucketDate} · z {Math.round(event.zScore * 10) / 10} · {event.sliceType}
      </p>
    </button>
  )
}
