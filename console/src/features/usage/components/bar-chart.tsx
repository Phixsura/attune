import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { useTranslation } from 'react-i18next'

// Lean SVG bar chart. We don't need recharts/visx for a single page
// showing daily ingest counts — viewBox + rect[] is 30 lines and ships
// 0 KB. The weekly-digest dashboard may justify a real chart lib later;
// today, hand-rolled SVG is enough.

interface UsageBucket {
  bucket: string // RFC3339
  value: number
}

const WIDTH = 600
const HEIGHT = 220
const PAD_X = 28
const PAD_Y = 18

export function UsageBarChart({ series }: { series: UsageBucket[] }) {
  const { t } = useTranslation()
  if (series.length === 0) return null
  const max = Math.max(...series.map((b) => b.value), 1)
  const barAreaW = WIDTH - PAD_X * 2
  const barAreaH = HEIGHT - PAD_Y * 2
  const gap = series.length > 12 ? 4 : 8
  const maxBarW = 30
  const computedBarW = Math.max((barAreaW - gap * (series.length - 1)) / series.length, 8)
  const barW = Math.min(computedBarW, maxBarW)
  const usedW = barW * series.length + gap * Math.max(series.length - 1, 0)
  const offsetX = PAD_X + Math.max((barAreaW - usedW) / 2, 0)
  const guideValues = [0.25, 0.5, 0.75, 1]
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      className="h-56 w-full"
      role="img"
      aria-label="Daily ingest counts"
    >
      {guideValues.map((ratio) => {
        const y = HEIGHT - PAD_Y - barAreaH * ratio
        return (
          <line
            key={ratio}
            x1={PAD_X}
            x2={WIDTH - PAD_X}
            y1={y}
            y2={y}
            className="stroke-border/70"
            strokeWidth={1}
            strokeDasharray="3 5"
          />
        )
      })}
      <line
        x1={PAD_X}
        x2={WIDTH - PAD_X}
        y1={HEIGHT - PAD_Y}
        y2={HEIGHT - PAD_Y}
        className="stroke-border"
        strokeWidth={1.2}
      />
      {series.map((b, i) => {
        const h = (b.value / max) * barAreaH
        const x = offsetX + i * (barW + gap)
        const y = HEIGHT - PAD_Y - h
        const showTick = series.length <= 7 || i === 0 || i === series.length - 1
        return (
          <g key={b.bucket}>
            <rect
              x={x}
              y={y}
              width={barW}
              height={h}
              className="fill-primary/85"
              rx={barW > 10 ? 4 : 2}
            >
              <title>
                {t('usage.bar_tooltip', {
                  date: format(new Date(b.bucket), t('usage.bar_date_format'), { locale: zhCN }),
                  count: b.value,
                })}
              </title>
            </rect>
            {showTick && (
              <text
                x={x + barW / 2}
                y={HEIGHT - 4}
                textAnchor="middle"
                className="fill-muted-foreground text-[10px]"
              >
                {format(new Date(b.bucket), 'M/d', { locale: zhCN })}
              </text>
            )}
          </g>
        )
      })}
    </svg>
  )
}
