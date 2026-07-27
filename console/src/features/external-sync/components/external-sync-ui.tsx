import type { ReactNode } from 'react'
import type { ExternalSyncEvent, ExternalSyncRun } from '@/features/external-sync/api/external-sync'
import { cn } from '@/lib/utils'

export const activeRunStatuses = [
  'running',
  'queued',
  'EXTERNAL_SYNC_RUN_STATUS_RUNNING',
  'EXTERNAL_SYNC_RUN_STATUS_QUEUED',
]

export function DetailSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h2 className="text-xs font-semibold tracking-[0.12em] text-muted-foreground uppercase">
        {title}
      </h2>
      {children}
    </section>
  )
}

export function JsonBlock({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-lg border border-border/60 bg-muted/25 px-3 py-2">
      <div className="text-[11px] font-semibold text-muted-foreground">{label}</div>
      <pre className="mt-1 max-h-24 overflow-auto text-xs whitespace-pre-wrap">
        {prettyJSON(value)}
      </pre>
    </div>
  )
}

export type DiagnosticRow = {
  label: string
  value: string
}

export function DiagnosticRows({ rows }: { rows: DiagnosticRow[] }) {
  const visibleRows = rows.filter((row) => row.value.trim())
  if (visibleRows.length === 0) return null

  return (
    <dl className="mt-2 grid gap-x-3 gap-y-1 text-[11px] sm:grid-cols-2">
      {visibleRows.map((row) => (
        <div key={`${row.label}-${row.value}`} className="min-w-0">
          <dt className="text-muted-foreground">{row.label}</dt>
          <dd className="truncate font-mono">{row.value}</dd>
        </div>
      ))}
    </dl>
  )
}

export function MutedLine({ children }: { children: ReactNode }) {
  return <div className="text-sm text-muted-foreground">{children}</div>
}

export function StatusPill({ value }: { value: string }) {
  const urgent = [
    'failed',
    'dead',
    'deleted',
    'EXTERNAL_SYNC_RUN_STATUS_FAILED',
    'EXTERNAL_SYNC_RUN_STATUS_DEAD',
  ]
  const active = [...activeRunStatuses, 'received']
  return (
    <span
      className={cn(
        'inline-flex max-w-36 items-center rounded-full border px-2 py-0.5 text-[11px] font-medium',
        urgent.includes(value)
          ? 'border-destructive/20 bg-destructive/10 text-destructive'
          : active.includes(value)
            ? 'border-amber-300/40 bg-amber-100/60 text-amber-800 dark:bg-amber-900/20 dark:text-amber-200'
            : 'border-border bg-muted/50 text-muted-foreground',
      )}
    >
      <span className="truncate">{value}</span>
    </span>
  )
}

export function isActiveRun(run: ExternalSyncRun) {
  return run.inFlight || activeRunStatuses.includes(run.status)
}

export function statusLabel(value: string) {
  return value.replace('EXTERNAL_SYNC_RUN_STATUS_', '').toLowerCase()
}

export function eventStatusLabel(value: string) {
  return value.replace('EXTERNAL_SYNC_EVENT_STATUS_', '').toLowerCase()
}

export function eventSignatureLabel(value: string) {
  return value.replace('EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_', '').toLowerCase()
}

export function isRetryableRunStatus(value: string) {
  const status = statusLabel(value)
  return status === 'failed' || status === 'dead'
}

export function isReplayableEvent(event: ExternalSyncEvent) {
  const status = eventStatusLabel(event.status)
  const signature = eventSignatureLabel(event.signatureStatus)
  return status === 'received' && (signature === 'verified' || signature === 'not_required')
}

export function directionLabel(value: string) {
  return value.replace('EXTERNAL_SYNC_DIRECTION_', '').toLowerCase()
}

export function formatDate(value: string) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export function capabilityGrade(profileJson: string) {
  const raw = profileJson.trim()
  if (!raw) return ''
  try {
    const parsed: unknown = JSON.parse(raw)
    if (!isRecord(parsed) || typeof parsed.grade !== 'string') return ''
    return parsed.grade
  } catch {
    return ''
  }
}

export function prettyJSON(value: string) {
  if (!value) return '{}'
  try {
    return JSON.stringify(JSON.parse(value), null, 2)
  } catch {
    return value
  }
}

export function shortID(value: string) {
  return value.slice(0, 8)
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}
