import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'

// Lean SVG bar chart. We don't need recharts/visx for a single page
// showing daily ingest counts — viewBox + rect[] is 30 lines and ships
// 0 KB. Phase 5 周报 dashboard may justify a chart lib; not now.

interface UsageBucket {
  bucket: string // RFC3339
  value: number
}

const WIDTH = 600
const HEIGHT = 120
const PAD_X = 20
const PAD_Y = 12

export function UsageBarChart({ series }: { series: UsageBucket[] }) {
  if (series.length === 0) return null
  const max = Math.max(...series.map((b) => b.value), 1)
  const barAreaW = WIDTH - PAD_X * 2
  const barAreaH = HEIGHT - PAD_Y * 2
  const gap = 2
  const barW = Math.max((barAreaW - gap * (series.length - 1)) / series.length, 1)
  return (
    <svg
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      className="h-32 w-full"
      role="img"
      aria-label="Daily ingest counts"
    >
      <line
        x1={PAD_X}
        x2={WIDTH - PAD_X}
        y1={HEIGHT - PAD_Y}
        y2={HEIGHT - PAD_Y}
        className="stroke-border"
        strokeWidth={1}
      />
      {series.map((b, i) => {
        const h = (b.value / max) * barAreaH
        const x = PAD_X + i * (barW + gap)
        const y = HEIGHT - PAD_Y - h
        return (
          <rect key={b.bucket} x={x} y={y} width={barW} height={h} className="fill-primary" rx={1}>
            <title>
              {format(new Date(b.bucket), 'M月d日', { locale: zhCN })} · {b.value} 条
            </title>
          </rect>
        )
      })}
    </svg>
  )
}
