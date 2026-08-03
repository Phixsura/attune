import { format, formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import {
  AlertCircle,
  ArrowRight,
  CheckCircle2,
  Clock3,
  RotateCcw,
  ShieldCheck,
  TriangleAlert,
  UsersRound,
} from 'lucide-react'
import { type ReactNode, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Loading } from '@/components/loading'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader } from '@/components/ui/card'
import type { FeedbackTriageCommandCenterData } from '@/features/feedback/api/get-feedback-triage-command-center'
import { cn } from '@/lib/utils'
import type { FeedbackTriageLane } from '@/proto/attune/v1/ingest'

interface TriageCommandCenterPanelProps {
  data?: FeedbackTriageCommandCenterData
  isLoading: boolean
  isError: boolean
  errorMessage?: string
  onRetry: () => void
  onOpenFeedback: (id: string, restoreFocusTo?: HTMLElement) => void
  onApplyLane?: (lane: FeedbackTriageLane) => void
}

export function TriageCommandCenterPanel({
  data,
  isLoading,
  isError,
  errorMessage,
  onRetry,
  onOpenFeedback,
  onApplyLane,
}: TriageCommandCenterPanelProps) {
  const { t } = useTranslation()
  const lanes = data?.lanes ?? []
  const priorityLane = useMemo(
    () => lanes.find((lane) => toNumber(lane.count) > 0) ?? lanes[0],
    [lanes],
  )

  if (isLoading) {
    return (
      <CommandCenterShell>
        <Loading className="py-0" />
      </CommandCenterShell>
    )
  }

  if (isError) {
    return (
      <CommandCenterShell tone="error">
        <div className="flex items-start gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-2xl border border-destructive/20 bg-destructive/10 text-destructive">
            <AlertCircle className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-base font-semibold text-foreground">
              {t('feedback.triage_command_center.error_title')}
            </h2>
            <p className="mt-1 text-sm leading-6 text-muted-foreground text-pretty">
              {errorMessage || t('feedback.triage_command_center.error_body')}
            </p>
            <div className="mt-4">
              <Button type="button" size="sm" variant="outline" onClick={onRetry}>
                <RotateCcw className="size-3.5" />
                {t('common.retry')}
              </Button>
            </div>
          </div>
        </div>
      </CommandCenterShell>
    )
  }

  if (!data) {
    return null
  }

  return (
    <CommandCenterShell>
      <CardHeader className="gap-0 px-5 py-5 sm:px-6">
        <div className="flex flex-col gap-5 xl:flex-row xl:items-end xl:justify-between">
          <div className="min-w-0 max-w-3xl">
            <div className="inline-flex items-center gap-2 rounded-full border border-sky-200 bg-sky-50 px-3 py-1 text-[11px] font-semibold tracking-[0.16em] text-sky-700 uppercase">
              <ShieldCheck className="size-3.5" />
              {t('feedback.triage_command_center.eyebrow')}
            </div>
            <h2 className="mt-3 text-[1.7rem] font-semibold tracking-tight text-foreground text-balance sm:text-[2rem]">
              {t('feedback.triage_command_center.title')}
            </h2>
            <CardDescription className="mt-2.5 max-w-2xl text-[13.5px] leading-[1.65rem] text-pretty">
              {t('feedback.triage_command_center.description')}
            </CardDescription>
          </div>
          <div className="grid min-w-0 gap-2 sm:grid-cols-2 xl:w-[22rem]">
            <CommandMetric
              icon={<TriangleAlert className="size-3.5" />}
              label={t('feedback.triage_command_center.urgent_open_count')}
              value={data.urgentOpenCount}
              tone="danger"
            />
            <CommandMetric
              icon={<Clock3 className="size-3.5" />}
              label={t('feedback.triage_command_center.overdue_count')}
              value={data.overdueCount}
              tone={toNumber(data.overdueCount) > 0 ? 'danger' : 'success'}
            />
          </div>
        </div>
      </CardHeader>

      <CardContent className="space-y-5 px-5 pb-5 sm:px-6">
        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-5">
          <CommandMetric
            icon={<UsersRound className="size-3.5" />}
            label={t('feedback.triage_command_center.open_count')}
            value={data.openCount}
          />
          <CommandMetric
            icon={<ArrowRight className="size-3.5" />}
            label={t('feedback.triage_command_center.active_count')}
            value={data.activeCount}
            tone="active"
          />
          <CommandMetric
            icon={<CheckCircle2 className="size-3.5" />}
            label={t('feedback.triage_command_center.closed_count')}
            value={data.closedCount}
            tone="success"
          />
          <CommandMetric
            icon={<TriangleAlert className="size-3.5" />}
            label={t('feedback.triage_command_center.terminal_failure_count')}
            value={data.terminalFailureCount}
            tone={toNumber(data.terminalFailureCount) > 0 ? 'danger' : 'neutral'}
          />
          <CommandMetric
            icon={<ShieldCheck className="size-3.5" />}
            label={t('feedback.triage_command_center.identity_debt_count')}
            value={data.identityDebtCount}
            tone={toNumber(data.identityDebtCount) > 0 ? 'warning' : 'success'}
          />
        </div>

        {priorityLane ? (
          <div className="rounded-[1rem] border border-sky-200 bg-[linear-gradient(135deg,rgba(240,249,255,0.96),rgba(255,255,255,0.98))] p-4 shadow-[0_20px_48px_-42px_rgba(2,132,199,0.7)]">
            <div className="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
              <div className="min-w-0 space-y-2">
                <div className="flex flex-wrap items-center gap-2 text-[11px] font-semibold tracking-[0.14em] text-sky-700 uppercase">
                  <Clock3 className="size-3.5" />
                  {t('feedback.triage_command_center.next_action')}
                </div>
                <div className="text-base font-semibold text-foreground">{priorityLane.label}</div>
                <p className="max-w-3xl text-sm leading-6 text-muted-foreground text-pretty">
                  {priorityLane.recommendedAction ||
                    t('feedback.triage_command_center.default_recommended_action')}
                </p>
                <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
                  <TriagePill>{priorityLane.ownerLane}</TriagePill>
                  <TriagePill>
                    {t('feedback.triage_command_center.sla_hours', {
                      count: priorityLane.slaHours,
                    })}
                  </TriagePill>
                  <TriagePill>
                    {t('feedback.triage_command_center.next_deadline', {
                      value: formatDateTime(priorityLane.nextDeadlineAt),
                    })}
                  </TriagePill>
                </div>
              </div>
              <div className="flex flex-wrap items-center gap-2 lg:justify-end">
                {onApplyLane && priorityLane.filterQuery ? (
                  <Button type="button" size="sm" onClick={() => onApplyLane(priorityLane)}>
                    <ArrowRight className="size-3.5" />
                    {t('feedback.triage_command_center.focus_lane')}
                  </Button>
                ) : null}
                {priorityLane.sampleFeedbackIds.slice(0, 3).map((id) => (
                  <Button
                    key={id}
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={(event) => onOpenFeedback(id, event.currentTarget)}
                    aria-label={t('feedback.triage_command_center.open_sample_with_id', { id })}
                  >
                    #{id}
                  </Button>
                ))}
              </div>
            </div>
          </div>
        ) : null}

        <div className="space-y-3">
          <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between">
            <div>
              <div className="text-sm font-semibold text-foreground">
                {t('feedback.triage_command_center.lanes_title')}
              </div>
              <p className="text-xs leading-5 text-muted-foreground">
                {t('feedback.triage_command_center.generated_at', {
                  value: formatDateTime(data.generatedAt),
                })}
              </p>
            </div>
            <p className="text-xs text-muted-foreground">
              {t('feedback.triage_command_center.due_soon_count', {
                count: toNumber(data.dueSoonCount),
              })}
            </p>
          </div>

          {lanes.length > 0 ? (
            <div className="divide-y divide-border/60 rounded-[1rem] border border-border/70 bg-background/85">
              {lanes.map((lane) => (
                <TriageLaneRow
                  key={lane.key}
                  lane={lane}
                  onOpenFeedback={onOpenFeedback}
                  onApplyLane={onApplyLane}
                />
              ))}
            </div>
          ) : (
            <div className="rounded-[1rem] border border-dashed border-border bg-muted/25 px-4 py-8 text-center text-sm text-muted-foreground">
              {t('feedback.triage_command_center.no_lanes')}
            </div>
          )}
        </div>
      </CardContent>
    </CommandCenterShell>
  )
}

function TriageLaneRow({
  lane,
  onOpenFeedback,
  onApplyLane,
}: {
  lane: FeedbackTriageLane
  onOpenFeedback: (id: string, restoreFocusTo?: HTMLElement) => void
  onApplyLane?: (lane: FeedbackTriageLane) => void
}) {
  const { t } = useTranslation()
  const overdue = toNumber(lane.overdueCount)
  const dueSoon = toNumber(lane.dueSoonCount)
  const count = toNumber(lane.count)

  return (
    <div className="grid gap-3 px-4 py-4 lg:grid-cols-[minmax(0,1.2fr)_minmax(16rem,0.85fr)_auto] lg:items-center">
      <div className="min-w-0 space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span
            className={cn('size-2 rounded-full', laneSeverityClass(lane.severity))}
            aria-hidden="true"
          />
          <div className="text-sm font-semibold text-foreground">{lane.label}</div>
          <span className="rounded-full border border-border/70 bg-muted/45 px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
            {t('feedback.triage_command_center.count', { count })}
          </span>
        </div>
        <p className="text-sm leading-6 text-muted-foreground text-pretty">
          {lane.recommendedAction || t('feedback.triage_command_center.default_recommended_action')}
        </p>
        <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
          <TriagePill>{lane.ownerLane}</TriagePill>
          <TriagePill>
            {t('feedback.triage_command_center.sla_hours', { count: lane.slaHours })}
          </TriagePill>
          {lane.filterQuery ? <TriagePill>{lane.filterQuery}</TriagePill> : null}
        </div>
      </div>

      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2">
        <TriageStatus
          label={t('feedback.triage_command_center.oldest')}
          value={formatDateDistance(lane.oldestCreatedAt)}
        />
        <TriageStatus
          label={t('feedback.triage_command_center.deadline')}
          value={formatDateDistance(lane.nextDeadlineAt)}
          tone={overdue > 0 ? 'danger' : dueSoon > 0 ? 'warning' : 'success'}
        />
      </div>

      <div className="flex flex-wrap items-center gap-2 lg:justify-end">
        <TriageStateBadge overdue={overdue} dueSoon={dueSoon} count={count} />
        {onApplyLane && lane.filterQuery ? (
          <Button type="button" size="sm" variant="outline" onClick={() => onApplyLane(lane)}>
            <ArrowRight className="size-3.5" />
            {t('feedback.triage_command_center.focus_lane')}
          </Button>
        ) : null}
        {lane.sampleFeedbackIds.slice(0, 3).map((id) => (
          <Button
            key={id}
            type="button"
            size="sm"
            variant="ghost"
            onClick={(event) => onOpenFeedback(id, event.currentTarget)}
            aria-label={t('feedback.triage_command_center.open_sample_with_id', { id })}
          >
            #{id}
          </Button>
        ))}
      </div>
    </div>
  )
}

function TriageStateBadge({
  overdue,
  dueSoon,
  count,
}: {
  overdue: number
  dueSoon: number
  count: number
}) {
  const { t } = useTranslation()
  if (count === 0) {
    return (
      <span className="rounded-full border border-border bg-muted/40 px-2.5 py-1 text-xs font-semibold text-muted-foreground">
        {t('feedback.triage_command_center.empty_lane')}
      </span>
    )
  }
  if (overdue > 0) {
    return (
      <span className="rounded-full border border-destructive/20 bg-destructive/10 px-2.5 py-1 text-xs font-semibold text-destructive">
        {t('feedback.triage_command_center.overdue_badge', { count: overdue })}
      </span>
    )
  }
  if (dueSoon > 0) {
    return (
      <span className="rounded-full border border-amber-200 bg-amber-50 px-2.5 py-1 text-xs font-semibold text-amber-700">
        {t('feedback.triage_command_center.due_soon_badge', { count: dueSoon })}
      </span>
    )
  }
  return (
    <span className="rounded-full border border-emerald-200 bg-emerald-50 px-2.5 py-1 text-xs font-semibold text-emerald-700">
      {t('feedback.triage_command_center.on_track')}
    </span>
  )
}

function CommandCenterShell({
  children,
  tone,
}: {
  children: ReactNode
  tone?: 'default' | 'error'
}) {
  return (
    <Card
      className={cn(
        'gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(248,250,252,0.98))] py-0 shadow-[0_28px_72px_-52px_rgba(15,23,42,0.24)]',
        tone === 'error' && 'border-destructive/25 bg-destructive/5',
      )}
    >
      {children}
    </Card>
  )
}

function CommandMetric({
  label,
  value,
  icon,
  tone,
}: {
  label: string
  value: string
  icon: ReactNode
  tone?: 'neutral' | 'active' | 'danger' | 'success' | 'warning'
}) {
  return (
    <div className={cn('rounded-[0.9rem] border px-3 py-2.5', metricToneClass(tone))}>
      <div className="flex items-center gap-2 text-[11px] font-medium text-muted-foreground">
        <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-background/80 text-foreground">
          {icon}
        </span>
        <span className="min-w-0 truncate">{label}</span>
      </div>
      <div className="mt-2 text-2xl font-semibold tracking-tight text-foreground">{value}</div>
    </div>
  )
}

function TriageStatus({
  label,
  value,
  tone,
}: {
  label: string
  value: string
  tone?: 'neutral' | 'danger' | 'success' | 'warning'
}) {
  return (
    <div className={cn('rounded-[0.8rem] border px-3 py-2', statusToneClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-1 text-sm font-semibold text-foreground">{value}</div>
    </div>
  )
}

function TriagePill({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-full border border-border/70 bg-background/80 px-2.5 py-1 font-medium">
      {children}
    </span>
  )
}

function toNumber(value: string | number | undefined) {
  const parsed = Number(value ?? 0)
  return Number.isFinite(parsed) ? parsed : 0
}

function formatDateTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return `${format(date, 'yyyy-MM-dd HH:mm', { locale: zhCN })} UTC`
}

function formatDateDistance(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return formatDistanceToNow(date, { addSuffix: true, locale: zhCN })
}

function laneSeverityClass(severity: string) {
  if (severity === 'critical') return 'bg-destructive'
  if (severity === 'high') return 'bg-amber-500'
  if (severity === 'medium') return 'bg-sky-500'
  return 'bg-emerald-500'
}

function metricToneClass(
  tone: 'neutral' | 'active' | 'danger' | 'success' | 'warning' = 'neutral',
) {
  if (tone === 'danger') return 'border-destructive/20 bg-destructive/8'
  if (tone === 'active') return 'border-sky-200 bg-sky-50'
  if (tone === 'success') return 'border-emerald-200 bg-emerald-50'
  if (tone === 'warning') return 'border-amber-200 bg-amber-50'
  return 'border-border/70 bg-background/80'
}

function statusToneClass(tone: 'neutral' | 'danger' | 'success' | 'warning' = 'neutral') {
  if (tone === 'danger') return 'border-destructive/20 bg-destructive/8'
  if (tone === 'success') return 'border-emerald-200 bg-emerald-50'
  if (tone === 'warning') return 'border-amber-200 bg-amber-50'
  return 'border-border/70 bg-muted/25'
}
