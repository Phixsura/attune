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
const PAD_X = 4
const PAD_Y = 4

export function UsageSparkline({ series }: { series: UsageBucket[] }) {
  const { t } = useTranslation()
  if (series.length === 0) return null
  const max = Math.max(...series.map((b) => b.value), 1)
  const barAreaW = WIDTH - PAD_X * 2
  const barAreaH = HEIGHT - PAD_Y * 2
  const gap = 2
  const maxBarW = 10
  const computedBarW = Math.max((barAreaW - gap * (series.length - 1)) / series.length, 2)
  const barW = Math.min(computedBarW, maxBarW)
  const usedW = barW * series.length + gap * Math.max(series.length - 1, 0)
  const offsetX = PAD_X + Math.max((barAreaW - usedW) / 2, 0)
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      className="h-9 w-[220px]"
      role="img"
      aria-label="Daily ingest sparkline"
    >
      <line
        x1={PAD_X}
        x2={WIDTH - PAD_X}
        y1={HEIGHT - PAD_Y}
        y2={HEIGHT - PAD_Y}
        className="stroke-border/70"
        strokeWidth={1}
      />
      {series.map((b, i) => {
        const h = (b.value / max) * barAreaH
        const x = offsetX + i * (barW + gap)
        const y = HEIGHT - PAD_Y - h
        return (
          <rect
            key={b.bucket}
            x={x}
            y={y}
            width={barW}
            height={h}
            className="fill-primary/75"
            rx={1.5}
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
