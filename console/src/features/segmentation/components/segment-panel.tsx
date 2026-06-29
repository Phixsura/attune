import { Users } from 'lucide-react'
import { useTranslation } from 'react-i18next'

export interface SegmentItem {
  id: string
  name: string
  tier: string
  customerCount: number
  feedbackCount: number
  revenueWeight: number
}

export function SegmentPanel({
  segments,
  onSelect,
}: {
  segments: SegmentItem[]
  onSelect?: (id: string) => void
}) {
  const { t } = useTranslation()

  if (segments.length === 0) {
    return <p className="text-center text-muted-foreground py-4">{t('segmentation.empty')}</p>
  }

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <Users className="size-4 text-muted-foreground" />
        <h3 className="text-sm font-semibold">{t('segmentation.title')}</h3>
      </div>
      <div className="divide-y rounded-md border">
        {segments.map((seg) => (
          <button
            key={seg.id}
            type="button"
            className="flex w-full items-center justify-between p-3 text-left hover:bg-muted/50"
            onClick={() => onSelect?.(seg.id)}
          >
            <div>
              <p className="text-sm font-medium">{seg.name}</p>
              <p className="text-xs text-muted-foreground">
                {seg.tier} · {seg.customerCount} {t('segmentation.customers')}
              </p>
            </div>
            <div className="text-right">
              <p className="text-sm">{seg.feedbackCount}</p>
              <p className="text-xs text-muted-foreground">{seg.revenueWeight.toFixed(0)}%</p>
            </div>
          </button>
        ))}
      </div>
    </div>
  )
}
