import { cn } from '@/lib/utils'

// Listen brand mark. Three concentric sound-wave arcs paired with the
// 听见 wordmark. Color inherits from currentColor so the same SVG works
// for primary (orange in light mode) and any accent context (light text
// in dark mode, brand color on white pages).
//
// Layer 1 visual polish — replaces the bare "听见" text in TopBar.
// Layer 2 (later) may add a custom typeface or animated arcs; for now
// the static SVG ships zero KB extra (inline) and looks intentional.
export function Logo({
  className,
  showText = true,
  size = 24,
}: {
  className?: string
  showText?: boolean
  size?: number
}) {
  return (
    <div className={cn('inline-flex items-center gap-2 leading-none', className)}>
      <svg
        viewBox="0 0 32 32"
        width={size}
        height={size}
        fill="none"
        stroke="currentColor"
        strokeWidth={2.4}
        strokeLinecap="round"
        role="img"
        aria-label="听见"
        className="shrink-0 text-primary"
      >
        {/* Three arcs opening rightward — sound emanating from a source */}
        <path d="M9 11a8 8 0 0 1 0 10" opacity="1" />
        <path d="M14 8a13 13 0 0 1 0 16" opacity="0.7" />
        <path d="M19 5a18 18 0 0 1 0 22" opacity="0.4" />
      </svg>
      {showText && <span className="text-base font-semibold tracking-tight">听见</span>}
    </div>
  )
}
