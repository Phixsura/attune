import { cn } from '@/lib/utils'

interface SimilarityRingProps {
  similarity: number // 0-1
  size?: number // diameter in pixels
  strokeWidth?: number
  className?: string
}

export function SimilarityRing({
  similarity,
  size = 32,
  strokeWidth = 3,
  className,
}: SimilarityRingProps) {
  const clamped = Math.max(0, Math.min(1, similarity))
  const percentage = Math.round(clamped * 100)
  const radius = (size - strokeWidth) / 2
  const circumference = radius * 2 * Math.PI
  const offset = circumference - clamped * circumference

  const getColor = () => {
    if (percentage >= 80) return 'stroke-emerald-500'
    if (percentage >= 60) return 'stroke-amber-500'
    if (percentage >= 40) return 'stroke-orange-500'
    return 'stroke-red-500'
  }

  return (
    <div className={cn('relative inline-flex items-center justify-center', className)}>
      <svg
        width={size}
        height={size}
        className="-rotate-90"
        role="img"
        aria-label={`${percentage}% similarity`}
      >
        {/* Background circle */}
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          strokeWidth={strokeWidth}
          className="stroke-muted"
        />
        {/* Progress circle */}
        <circle
          cx={size / 2}
          cy={size / 2}
          r={radius}
          fill="none"
          strokeWidth={strokeWidth}
          strokeDasharray={circumference}
          strokeDashoffset={offset}
          strokeLinecap="round"
          className={cn('transition-all duration-300', getColor())}
        />
      </svg>
      <span className="absolute font-medium text-xs">{percentage}</span>
    </div>
  )
}
