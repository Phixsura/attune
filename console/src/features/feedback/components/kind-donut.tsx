import { useTranslation } from 'react-i18next'

// Phase 1.5 Layer 2 — kind 分布 donut for the feedback page. Pure SVG,
// no chart lib (~80 lines vs recharts ~100KB). Reuses the KindBadge
// hue palette so colors mean the same thing across the page.
//
// Mathematics: each slice is an SVG path arc on a single radius circle,
// rendered as a thick stroked arc (stroke-dasharray + offset). Hovering
// a legend item highlights via opacity — no fancy interactivity.

interface KindDonutProps {
  byKind: Record<string, number>
  total: number
}

// Hex equivalents of the KindBadge Tailwind colors. Inline so the SVG
// stays self-contained (Tailwind utility classes can't be applied to
// stroke="currentColor" on multiple slices).
const KIND_COLOR: Record<string, string> = {
  bug: '#ef4444', // red-500
  feature: '#8b5cf6', // violet-500
  ops: '#f97316', // orange-500
  question: '#0ea5e9', // sky-500
  other: '#94a3b8', // slate-400
  unknown: '#cbd5e1', // slate-300
}

const SIZE = 160
const STROKE = 22
const RADIUS = (SIZE - STROKE) / 2
const CIRC = 2 * Math.PI * RADIUS

export function KindDonut({ byKind, total }: KindDonutProps) {
  const { t } = useTranslation()
  if (total === 0) return null

  // Sort largest first so the visual order matches the legend.
  const entries = Object.entries(byKind)
    .filter(([, n]) => n > 0)
    .sort(([, a], [, b]) => b - a)

  let offset = 0
  return (
    <div className="flex items-center gap-6">
      <svg
        viewBox={`0 0 ${SIZE} ${SIZE}`}
        width={SIZE}
        height={SIZE}
        className="shrink-0"
        role="img"
        aria-label="Feedback kind distribution"
      >
        {/* Background ring so slices < 1% still register visually */}
        <circle
          cx={SIZE / 2}
          cy={SIZE / 2}
          r={RADIUS}
          fill="none"
          stroke="var(--muted)"
          strokeWidth={STROKE}
        />
        {entries.map(([kind, n]) => {
          const len = (n / total) * CIRC
          const arc = (
            <circle
              key={kind}
              cx={SIZE / 2}
              cy={SIZE / 2}
              r={RADIUS}
              fill="none"
              stroke={KIND_COLOR[kind] ?? KIND_COLOR.other}
              strokeWidth={STROKE}
              strokeDasharray={`${len} ${CIRC}`}
              strokeDashoffset={-offset}
              transform={`rotate(-90 ${SIZE / 2} ${SIZE / 2})`}
            />
          )
          offset += len
          return arc
        })}
        {/* Total in center */}
        <text
          x={SIZE / 2}
          y={SIZE / 2 - 4}
          textAnchor="middle"
          className="fill-foreground font-semibold"
          fontSize="22"
        >
          {total}
        </text>
        <text
          x={SIZE / 2}
          y={SIZE / 2 + 14}
          textAnchor="middle"
          className="fill-muted-foreground"
          fontSize="10"
        >
          {t('feedback.donut_total_label')}
        </text>
      </svg>

      <ul className="space-y-1.5 text-sm">
        {entries.map(([kind, n]) => (
          <li key={kind} className="flex items-center gap-2">
            <span
              className="inline-block h-3 w-3 rounded-sm"
              style={{ backgroundColor: KIND_COLOR[kind] ?? KIND_COLOR.other }}
            />
            <span className="text-muted-foreground">
              {t(`feedback.kind_label.${kind}`, { defaultValue: kind })}
            </span>
            <span className="font-medium tabular-nums">{n}</span>
            <span className="text-xs text-muted-foreground">
              ({Math.round((n / total) * 100)}%)
            </span>
          </li>
        ))}
      </ul>
    </div>
  )
}
