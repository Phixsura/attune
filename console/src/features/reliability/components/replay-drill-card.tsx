import { ArrowUpRight, ClipboardCheck, RefreshCw, ShieldAlert, ShieldCheck } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { cn } from '@/lib/utils'
import type { ReplayDrill, ReplayDrillLane, ReplayDrillStatus } from '../replay-drill'

export function ReplayDrillCard({ drill }: { drill: ReplayDrill }) {
  const { t } = useTranslation()
  return (
    <Card
      data-testid="reliability-replay-drill"
      className="border-border/60 bg-background/95 shadow-none"
    >
      <CardHeader className="border-b border-border/60 bg-muted/15">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <CardTitle className="flex items-center gap-2 text-base">
              <RefreshCw className="size-4 text-muted-foreground" />
              {t('reliability.replay_drill.title', 'Replay / Backfill 演练')}
            </CardTitle>
            <CardDescription className="mt-1">
              {t(
                'reliability.replay_drill.description',
                '每条可靠性 SLO 都绑定重放入口、证据产物、负责人和升级路径。',
              )}
            </CardDescription>
          </div>
          <div className="grid grid-cols-3 gap-2 text-right text-xs">
            <ReplayDrillTotal
              label={t('reliability.replay_drill.ready', '就绪')}
              value={drill.totals.ready}
              tone="ready"
            />
            <ReplayDrillTotal
              label={t('reliability.replay_drill.attention', '需处理')}
              value={drill.totals.attention}
              tone="attention"
            />
            <ReplayDrillTotal
              label={t('reliability.replay_drill.blocked', '阻塞')}
              value={drill.totals.blocked}
              tone="blocked"
            />
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-3 pt-4">
        {drill.lanes.map((lane) => (
          <ReplayDrillLaneRow key={lane.key} lane={lane} />
        ))}
      </CardContent>
    </Card>
  )
}

function ReplayDrillTotal({
  label,
  value,
  tone,
}: {
  label: string
  value: number
  tone: ReplayDrillStatus
}) {
  return (
    <div className={cn('rounded-md border px-3 py-2', replayDrillSurfaceClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-0.5 text-lg font-semibold tabular-nums text-foreground">{value}</div>
    </div>
  )
}

function ReplayDrillLaneRow({ lane }: { lane: ReplayDrillLane }) {
  const { t } = useTranslation()
  return (
    <div className="grid gap-3 rounded-md border border-border/60 bg-muted/10 px-3 py-3 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.3fr)_12rem]">
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <ReplayDrillStatusBadge status={lane.status} />
          <div className="truncate text-sm font-semibold text-foreground">{lane.title}</div>
        </div>
        <div className="mt-1 text-xs leading-5 text-muted-foreground">
          {lane.owner} · {lane.escalation}
        </div>
        <div className="mt-2 rounded-sm border border-border/60 bg-background/75 px-2 py-1.5 font-mono text-xs text-muted-foreground">
          {lane.signal}
        </div>
      </div>

      <div className="grid gap-2 sm:grid-cols-3">
        <ReplayDrillFact
          label={t('reliability.replay_drill.lens', 'Replay lens')}
          value={lane.lens}
        />
        <ReplayDrillFact
          label={t('reliability.replay_drill.action', 'Action')}
          value={lane.actionLabel}
        />
        <ReplayDrillFact
          label={t('reliability.replay_drill.evidence', 'Evidence')}
          value={lane.evidenceLabel}
        />
      </div>

      <a
        href={lane.entryHref}
        className="inline-flex min-h-10 items-center justify-center gap-2 rounded-md border border-border/70 bg-background px-3 py-2 text-sm font-medium text-foreground transition-colors hover:bg-muted/30"
      >
        <ClipboardCheck className="size-4" />
        <span className="min-w-0 truncate">{lane.entryLabel}</span>
        <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
      </a>
    </div>
  )
}

function ReplayDrillFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 rounded-sm border border-border/50 bg-background/70 px-2.5 py-2">
      <div className="text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
        {label}
      </div>
      <div className="mt-1 text-xs leading-5 text-foreground">{value}</div>
    </div>
  )
}

function ReplayDrillStatusBadge({ status }: { status: ReplayDrillStatus }) {
  const { t } = useTranslation()
  const Icon = status === 'blocked' ? ShieldAlert : status === 'attention' ? RefreshCw : ShieldCheck
  return (
    <span
      className={cn(
        'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-medium',
        replayDrillBadgeClass(status),
      )}
    >
      <Icon className="size-3" />
      {replayDrillStatusLabel(status, t)}
    </span>
  )
}

function replayDrillStatusLabel(status: ReplayDrillStatus, t: (key: string) => string) {
  switch (status) {
    case 'ready':
      return t('reliability.replay_drill.status.ready')
    case 'attention':
      return t('reliability.replay_drill.status.attention')
    case 'blocked':
      return t('reliability.replay_drill.status.blocked')
    default:
      return status
  }
}

function replayDrillBadgeClass(status: ReplayDrillStatus) {
  switch (status) {
    case 'ready':
      return 'border-emerald-200 bg-emerald-50 text-emerald-700'
    case 'attention':
      return 'border-amber-200 bg-amber-50 text-amber-800'
    case 'blocked':
      return 'border-red-200 bg-red-50 text-red-700'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}

function replayDrillSurfaceClass(status: ReplayDrillStatus) {
  switch (status) {
    case 'ready':
      return 'border-emerald-200 bg-emerald-50/70'
    case 'attention':
      return 'border-amber-200 bg-amber-50/70'
    case 'blocked':
      return 'border-red-200 bg-red-50/70'
    default:
      return 'border-border bg-muted/20'
  }
}
