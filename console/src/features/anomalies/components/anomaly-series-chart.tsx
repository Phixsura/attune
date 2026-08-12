import type { SeriesPoint } from '@/features/anomalies/api/anomalies'

interface AnomalySeriesChartProps {
  points: SeriesPoint[]
  height?: number
}

const WIDTH = 720
const PAD = { bottom: 20, left: 36, right: 8, top: 8 }

/**
 * Hand-rolled SVG series chart: count line over the detector's expected
 * band, red dots on anomalous days. The band comes from the server-side
 * replay of the same Detect function the worker runs, so chart and alerts
 * can never disagree.
 */
export function AnomalySeriesChart({ points, height = 220 }: AnomalySeriesChartProps) {
  if (points.length === 0) {
    return null
  }
  const innerW = WIDTH - PAD.left - PAD.right
  const innerH = height - PAD.top - PAD.bottom
  const maxY = Math.max(1, ...points.map((p) => Math.max(Number(p.count), p.expectedHigh)))
  const x = (i: number) =>
    PAD.left + (points.length === 1 ? innerW / 2 : (i / (points.length - 1)) * innerW)
  const y = (v: number) => PAD.top + innerH - (v / maxY) * innerH

  const banded = points.map((p, i) => ({ i, p })).filter(({ p }) => !p.insufficient)
  const bandPath =
    banded.length > 1
      ? `M ${banded.map(({ i, p }) => `${x(i)} ${y(p.expectedHigh)}`).join(' L ')} L ${banded
          .slice()
          .reverse()
          .map(({ i, p }) => `${x(i)} ${y(p.expectedLow)}`)
          .join(' L ')} Z`
      : ''
  const linePath = points
    .map((p, i) => `${i === 0 ? 'M' : 'L'} ${x(i)} ${y(Number(p.count))}`)
    .join(' ')

  const gridLines = [0.25, 0.5, 0.75]
  return (
    <svg
      aria-label="anomaly series chart"
      className="w-full"
      role="img"
      viewBox={`0 0 ${WIDTH} ${height}`}
    >
      {gridLines.map((f) => (
        <line
          key={f}
          className="stroke-border"
          strokeDasharray="2 4"
          x1={PAD.left}
          x2={WIDTH - PAD.right}
          y1={PAD.top + innerH * f}
          y2={PAD.top + innerH * f}
        />
      ))}
      {bandPath ? (
        <path className="fill-primary/10" d={bandPath} data-testid="expected-band" />
      ) : null}
      <path
        className="stroke-primary fill-none"
        d={linePath}
        data-testid="count-line"
        strokeWidth={1.5}
      />
      {points.map((p, i) =>
        p.isAnomalous ? (
          <circle
            className="fill-red-500"
            cx={x(i)}
            cy={y(Number(p.count))}
            data-testid="anomaly-dot"
            key={p.date}
            r={3.5}
          />
        ) : null,
      )}
      <text className="fill-muted-foreground text-[10px]" x={PAD.left} y={height - 6}>
        {points[0]?.date}
      </text>
      <text
        className="fill-muted-foreground text-[10px]"
        textAnchor="end"
        x={WIDTH - PAD.right}
        y={height - 6}
      >
        {points[points.length - 1]?.date}
      </text>
      <text className="fill-muted-foreground text-[10px]" x={4} y={PAD.top + 10}>
        {maxY}
      </text>
    </svg>
  )
}
