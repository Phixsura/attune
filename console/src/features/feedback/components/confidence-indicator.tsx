import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

export function ConfidenceIndicator({
  confidence,
  threshold = 0.5,
  className,
}: {
  confidence?: number
  threshold?: number
  className?: string
}) {
  const { t } = useTranslation()
  if (confidence == null || Number.isNaN(confidence)) {
    return (
      <span
        className={cn('text-xs text-muted-foreground', className)}
        title={t('feedback.confidence.none')}
      >
        --
      </span>
    )
  }
  const clamped = Math.max(0, Math.min(1, confidence))
  const tone = toneForConfidence(clamped, threshold, t)
  const pct = Math.round(clamped * 100)
  return (
    <span
      className={cn(
        'inline-flex h-6 min-w-14 items-center justify-center gap-1 rounded border px-1.5 font-mono text-[11px] tabular-nums',
        tone.className,
        className,
      )}
      title={tone.title}
    >
      <span className={cn('size-1.5 rounded-full', tone.dotClass)} />
      {pct}%
    </span>
  )
}

function toneForConfidence(confidence: number, threshold: number, t: (key: string) => string) {
  if (confidence < threshold) {
    return {
      title: t('feedback.confidence.low'),
      className: 'border-red-200 bg-red-50 text-red-700',
      dotClass: 'bg-red-500',
    }
  }
  if (confidence < 0.75) {
    return {
      title: t('feedback.confidence.medium'),
      className: 'border-amber-200 bg-amber-50 text-amber-700',
      dotClass: 'bg-amber-500',
    }
  }
  return {
    title: t('feedback.confidence.high'),
    className: 'border-emerald-200 bg-emerald-50 text-emerald-700',
    dotClass: 'bg-emerald-500',
  }
}
