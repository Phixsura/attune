import {
  AlertTriangle,
  ArrowUpRight,
  BellRing,
  CheckCircle2,
  type LucideIcon,
  MessageSquareWarning,
  Radar,
  RotateCcw,
  ShieldAlert,
  ShieldCheck,
  TimerReset,
  Wrench,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type {
  IncidentTimeline,
  IncidentTimelineEvent,
  IncidentTimelinePhase,
  IncidentTimelineStatus,
} from '../incident-timeline'

export function IncidentTimelineCard({ timeline }: { timeline: IncidentTimeline }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="reliability-incident-timeline"
      className="border-border/60 bg-background/95 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <TimerReset className="size-4 text-muted-foreground" />
              {t('reliability.incident_timeline.title', 'Incident timeline reconstruction')}
            </CardTitle>
            <CardDescription className="mt-1 max-w-3xl">
              {t(
                'reliability.incident_timeline.description',
                'Start, detection, impact, mitigation, recovery, and customer notification evidence stay in one incident reconstruction.',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-2 gap-2 text-right text-xs sm:grid-cols-5">
            <IncidentTimelineTotal
              label={t('reliability.incident_timeline.verified', 'Verified')}
              value={timeline.totals.verified}
              tone="verified"
            />
            <IncidentTimelineTotal
              label={t('reliability.incident_timeline.attention', 'Attention')}
              value={timeline.totals.attention}
              tone="attention"
            />
            <IncidentTimelineTotal
              label={t('reliability.incident_timeline.blocked', 'Blocked')}
              value={timeline.totals.blocked}
              tone="blocked"
            />
            <IncidentTimelineTotal
              label={t('reliability.incident_timeline.recovered', 'Recovered')}
              value={timeline.totals.recovered}
              tone="recovered"
            />
            <IncidentTimelineTotal
              label={t('reliability.incident_timeline.needs_data', 'Needs data')}
              value={timeline.totals.needs_data}
              tone="needs_data"
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 pt-4">
        <div className="grid gap-3 rounded-md border border-border/60 bg-muted/10 px-3 py-3 md:grid-cols-[minmax(0,0.8fr)_minmax(0,1fr)]">
          <IncidentTimelineFact
            label={t('reliability.incident_timeline.fingerprint', 'Incident fingerprint')}
            value={timeline.fingerprint}
          />
          <IncidentTimelineFact
            label={t('reliability.incident_timeline.summary', 'Incident decision')}
            value={timeline.summary}
          />
        </div>
        <div className="space-y-3">
          {timeline.events.map((event) => (
            <IncidentTimelineRow key={event.phase} event={event} />
          ))}
        </div>
      </CardContent>
    </Card>
  )
}

function IncidentTimelineTotal({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: IncidentTimelineStatus
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', incidentStatusSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function IncidentTimelineRow({ event }: { event: IncidentTimelineEvent }) {
  const { t } = useTranslation()
  const Icon = incidentPhaseIcon(event.phase)
  return (
    <div
      data-testid={`reliability-incident-timeline-${event.phase}`}
      className="grid gap-3 rounded-md border border-border/60 bg-background/80 px-3 py-3 lg:grid-cols-[minmax(9rem,0.5fr)_minmax(0,1fr)_minmax(11rem,0.55fr)]"
    >
      <div className="flex min-w-0 items-start gap-3">
        <div className={cn('rounded-lg border p-2', incidentStatusSurfaceClass(event.status))}>
          <Icon className="size-4 text-foreground/75" />
        </div>
        <div className="min-w-0">
          <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            {t(`reliability.incident_timeline.phase.${event.phase}`, event.phase)}
          </div>
          <div className="mt-1 break-words text-sm font-semibold text-foreground">
            {event.title}
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">{event.owner}</div>
        </div>
      </div>

      <div className="grid min-w-0 gap-2 sm:grid-cols-2">
        <IncidentTimelineFact
          label={t('reliability.incident_timeline.signal', 'Signal')}
          value={event.signal}
        />
        <IncidentTimelineFact
          label={t('reliability.incident_timeline.evidence', 'Evidence')}
          value={event.evidence}
        />
      </div>

      <div className="flex min-w-0 flex-col justify-between gap-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <IncidentTimelineStatusBadge status={event.status} />
          <span className="break-words text-xs text-muted-foreground">{event.occurredAtLabel}</span>
        </div>
        <a
          href={event.actionHref}
          target={event.actionHref.startsWith('http') ? '_blank' : undefined}
          rel={event.actionHref.startsWith('http') ? 'noreferrer' : undefined}
          className="inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
        >
          <span className="min-w-0 truncate">{event.actionLabel}</span>
          <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
        </a>
      </div>
    </div>
  )
}

function IncidentTimelineFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 break-words text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function IncidentTimelineStatusBadge({ status }: { status: IncidentTimelineStatus }) {
  const { t } = useTranslation()
  const Icon =
    status === 'blocked'
      ? ShieldAlert
      : status === 'attention'
        ? AlertTriangle
        : status === 'needs_data'
          ? BellRing
          : status === 'recovered'
            ? RotateCcw
            : ShieldCheck
  return (
    <span
      className={cn(
        'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        incidentStatusBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {t(`reliability.incident_timeline.status.${status}`, status)}
    </span>
  )
}

function incidentPhaseIcon(phase: IncidentTimelinePhase): LucideIcon {
  switch (phase) {
    case 'start':
      return TimerReset
    case 'detection':
      return Radar
    case 'impact':
      return MessageSquareWarning
    case 'mitigation':
      return Wrench
    case 'recovery':
      return RotateCcw
    case 'customer_notification':
      return BellRing
    default:
      return CheckCircle2
  }
}

function incidentStatusBadgeClass(status: IncidentTimelineStatus) {
  switch (status) {
    case 'verified':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'attention':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    case 'blocked':
      return 'border-red-200 bg-red-50 text-red-700'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50 text-sky-800'
    case 'recovered':
      return 'border-teal-200 bg-teal-50 text-teal-700'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}

function incidentStatusSurfaceClass(status: IncidentTimelineStatus) {
  switch (status) {
    case 'verified':
      return 'border-emerald-200 bg-emerald-50/70'
    case 'attention':
      return 'border-amber-200 bg-amber-50/70'
    case 'blocked':
      return 'border-red-200 bg-red-50/70'
    case 'needs_data':
      return 'border-sky-200 bg-sky-50/70'
    case 'recovered':
      return 'border-teal-200 bg-teal-50/70'
    default:
      return 'border-border bg-muted/20'
  }
}
