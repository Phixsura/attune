import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { useTranslation } from 'react-i18next'

// Sparkline — same data as UsageBarChart, narrower form for embedding
// in a stat card. Lets the headline number ("received X this month")
// carry a tiny trend line beside it so the reader gets up/flat/down at
// a glance without reading any axis.

interface UsageBucket {
  bucket: string
  value: number
}

const WIDTH = 220
const HEIGHT = 36
const PAD_X = 2
const PAD_Y = 4

export function UsageSparkline({ series }: { series: UsageBucket[] }) {
  const { t } = useTranslation()
  if (series.length === 0) return null
  const max = Math.max(...series.map((b) => b.value), 1)
  const barAreaW = WIDTH - PAD_X * 2
  const barAreaH = HEIGHT - PAD_Y * 2
  const gap = 1
  const barW = Math.max((barAreaW - gap * (series.length - 1)) / series.length, 1)
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      className="h-9 w-[220px]"
      role="img"
      aria-label="Daily ingest sparkline"
    >
      {series.map((b, i) => {
        const h = (b.value / max) * barAreaH
        const x = PAD_X + i * (barW + gap)
        const y = HEIGHT - PAD_Y - h
        return (
          <rect
            key={b.bucket}
            x={x}
            y={y}
            width={barW}
            height={h}
            className="fill-primary/70"
            rx={0.5}
          >
            <title>
              {t('usage.bar_tooltip', {
                date: format(new Date(b.bucket), t('usage.bar_date_format'), { locale: zhCN }),
                count: b.value,
              })}
            </title>
          </rect>
        )
      })}
    </svg>
  )
}
