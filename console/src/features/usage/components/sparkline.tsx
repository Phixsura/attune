import { format } from 'date-fns'
import { zhCN } from 'date-fns/locale'

// Sparkline — same data as UsageBarChart, narrower form for embedding
// in a stat card. Phase 1.5 Layer 2: 让"本月已接收 X 条"那个大数字
// 旁边有一条小趋势线，一眼看出"涨/平/降"。

interface UsageBucket {
  bucket: string
  value: number
}

const WIDTH = 220
const HEIGHT = 36
const PAD_X = 2
const PAD_Y = 4

export function UsageSparkline({ series }: { series: UsageBucket[] }) {
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
              {format(new Date(b.bucket), 'M月d日', { locale: zhCN })} · {b.value} 条
            </title>
          </rect>
        )
      })}
    </svg>
  )
}
