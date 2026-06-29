import { useTranslation } from 'react-i18next'

interface SentimentEntry {
  value: string
  count: number
}

const SENTIMENT_COLORS: Record<string, string> = {
  positive: 'fill-emerald-500',
  neutral: 'fill-slate-400',
  negative: 'fill-rose-500',
  mixed: 'fill-amber-400',
}

const SENTIMENT_ORDER = ['positive', 'neutral', 'mixed', 'negative']

const BAR_HEIGHT = 28
const LABEL_W = 80
const COUNT_W = 50
const BAR_AREA_W = 280
const SVG_W = LABEL_W + BAR_AREA_W + COUNT_W + 8

export function SentimentChart({ data }: { data: SentimentEntry[] }) {
  const { t } = useTranslation()

  const sorted = [...data].sort(
    (a, b) => SENTIMENT_ORDER.indexOf(a.value) - SENTIMENT_ORDER.indexOf(b.value),
  )

  if (sorted.length === 0) return null

  const max = Math.max(...sorted.map((e) => e.count), 1)
  const svgH = sorted.length * (BAR_HEIGHT + 6) + 4

  return (
    <svg
      viewBox={`0 0 ${SVG_W} ${svgH}`}
      className="w-full"
      role="img"
      aria-label={t('analytics.sentiment_distribution')}
    >
      {sorted.map((entry, i) => {
        const y = i * (BAR_HEIGHT + 6) + 2
        const barW = (entry.count / max) * BAR_AREA_W
        const colorClass = SENTIMENT_COLORS[entry.value] ?? 'fill-primary/60'
        return (
          <g key={entry.value}>
            <text
              x={LABEL_W - 8}
              y={y + BAR_HEIGHT / 2 + 4}
              textAnchor="end"
              className="fill-foreground text-[11px]"
            >
              {entry.value}
            </text>
            <rect
              x={LABEL_W}
              y={y}
              width={Math.max(barW, 2)}
              height={BAR_HEIGHT}
              className={colorClass}
              rx={4}
            />
            <text
              x={LABEL_W + BAR_AREA_W + 8}
              y={y + BAR_HEIGHT / 2 + 4}
              className="fill-muted-foreground text-[11px]"
            >
              {entry.count}
            </text>
          </g>
        )
      })}
    </svg>
  )
}
