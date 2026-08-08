import { format, formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import {
  AlertCircle,
  ArrowRight,
  CalendarClock,
  CalendarX2,
  Clock3,
  RotateCcw,
  ShieldAlert,
  UserRoundX,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Loading } from '@/components/loading'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { FeedbackAssignmentEscalationQueueData } from '@/features/feedback/api/get-feedback-assignment-escalations'
import { cn } from '@/lib/utils'
import type { FeedbackAssignmentEscalation } from '@/proto/attune/v1/ingest'

interface AssignmentEscalationQueueProps {
  data?: FeedbackAssignmentEscalationQueueData
  isLoading: boolean
  isError: boolean
  errorMessage?: string
  onRetry: () => void
  onOpenFeedback: (id: string, restoreFocusTo?: HTMLElement) => void
}

export function AssignmentEscalationQueue({
  data,
  isLoading,
  isError,
  errorMessage,
  onRetry,
  onOpenFeedback,
}: AssignmentEscalationQueueProps) {
  const { t } = useTranslation()
  const items = data?.items ?? []
  const hasEscalations =
    toNumber(data?.overdueCount) +
      toNumber(data?.dueSoonCount) +
      toNumber(data?.missingOwnerCount) +
      toNumber(data?.missingSlaCount) >
    0

  if (isLoading) {
    return (
      <EscalationShell>
        <Loading className="py-0" />
      </EscalationShell>
    )
  }

  if (isError) {
    return (
      <EscalationShell tone="error">
        <div className="flex items-start gap-3 p-5 sm:p-6">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-[0.8rem] border border-destructive/20 bg-destructive/10 text-destructive">
            <AlertCircle className="size-4" />
          </div>
          <div className="min-w-0 flex-1">
            <h2 className="text-base font-semibold text-foreground">
              {t('feedback.assignment_escalations.error_title')}
            </h2>
            <p className="mt-1 text-sm leading-6 text-muted-foreground text-pretty">
              {errorMessage || t('feedback.assignment_escalations.error_body')}
            </p>
            <Button type="button" size="sm" variant="outline" className="mt-4" onClick={onRetry}>
              <RotateCcw className="size-3.5" />
              {t('common.retry')}
            </Button>
          </div>
        </div>
      </EscalationShell>
    )
  }

  if (!data) return null

  return (
    <EscalationShell>
      <CardHeader className="gap-0 px-5 py-5 sm:px-6">
        <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
          <div className="min-w-0 max-w-3xl">
            <div className="inline-flex items-center gap-2 rounded-md border border-amber-200 bg-amber-50 px-2.5 py-1 text-[11px] font-semibold tracking-[0.14em] text-amber-800 uppercase">
              <ShieldAlert className="size-3.5" />
              {t('feedback.assignment_escalations.eyebrow')}
            </div>
            <CardTitle className="mt-3 text-[1.35rem] font-semibold tracking-tight text-foreground text-balance sm:text-[1.55rem]">
              {t('feedback.assignment_escalations.title')}
            </CardTitle>
            <p className="mt-2 max-w-2xl text-[13.5px] leading-[1.65rem] text-muted-foreground text-pretty">
              {t('feedback.assignment_escalations.description')}
            </p>
          </div>
          <p className="text-xs text-muted-foreground">
            {t('feedback.assignment_escalations.generated_at', {
              value: formatDateTime(data.generatedAt),
            })}
          </p>
        </div>
      </CardHeader>

      <CardContent className="space-y-4 px-5 pb-5 sm:px-6">
        <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
          <EscalationMetric
            icon={<Clock3 className="size-3.5" />}
            label={t('feedback.assignment_escalations.overdue_count')}
            value={data.overdueCount}
            tone={toNumber(data.overdueCount) > 0 ? 'danger' : 'success'}
          />
          <EscalationMetric
            icon={<CalendarClock className="size-3.5" />}
            label={t('feedback.assignment_escalations.due_soon_count')}
            value={data.dueSoonCount}
            tone={toNumber(data.dueSoonCount) > 0 ? 'warning' : 'success'}
          />
          <EscalationMetric
            icon={<UserRoundX className="size-3.5" />}
            label={t('feedback.assignment_escalations.missing_owner_count')}
            value={data.missingOwnerCount}
            tone={toNumber(data.missingOwnerCount) > 0 ? 'danger' : 'success'}
          />
          <EscalationMetric
            icon={<CalendarX2 className="size-3.5" />}
            label={t('feedback.assignment_escalations.missing_sla_count')}
            value={data.missingSlaCount}
            tone={toNumber(data.missingSlaCount) > 0 ? 'warning' : 'success'}
          />
        </div>

        {hasEscalations && items.length > 0 ? (
          <div className="divide-y divide-border/60 rounded-[0.95rem] border border-border/70 bg-background/90">
            {items.map((item) => (
              <EscalationRow key={item.feedbackId} item={item} onOpenFeedback={onOpenFeedback} />
            ))}
          </div>
        ) : (
          <div className="rounded-[0.95rem] border border-dashed border-border bg-muted/25 px-4 py-8 text-center">
            <div className="text-sm font-semibold text-foreground">
              {t('feedback.assignment_escalations.empty_title')}
            </div>
            <p className="mx-auto mt-1 max-w-xl text-sm leading-6 text-muted-foreground text-pretty">
              {t('feedback.assignment_escalations.empty_body')}
            </p>
          </div>
        )}
      </CardContent>
    </EscalationShell>
  )
}

function EscalationRow({
  item,
  onOpenFeedback,
}: {
  item: FeedbackAssignmentEscalation
  onOpenFeedback: (id: string, restoreFocusTo?: HTMLElement) => void
}) {
  const { t } = useTranslation()
  const ownerLabel =
    item.assignment?.owner?.email ||
    item.assignment?.owner?.userId ||
    item.assignment?.owner?.memberId
  const accountLabel = item.accountContext?.accountDisplay || item.accountContext?.accountKey || ''

  return (
    <div className="grid gap-3 px-4 py-4 lg:grid-cols-[minmax(0,1.15fr)_minmax(14rem,0.72fr)_auto] lg:items-center">
      <div className="min-w-0 space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span
            className={cn('size-2 rounded-full', priorityDotClass(item.priority))}
            aria-hidden="true"
          />
          <div className="min-w-0 truncate text-sm font-semibold text-foreground">
            {item.title || t('feedback.untitled')}
          </div>
          {item.isUrgent ? (
            <span className="rounded-full border border-destructive/20 bg-destructive/10 px-2 py-0.5 text-[11px] font-semibold text-destructive">
              urgent
            </span>
          ) : null}
          <span className="rounded-full border border-border/70 bg-muted/45 px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
            {t(`feedback.assignment_escalations.priority.${item.priority || 'low'}`)}
          </span>
        </div>
        <div className="flex flex-wrap gap-2 text-xs text-muted-foreground">
          <EscalationPill>{item.source || '-'}</EscalationPill>
          <EscalationPill>{item.type || '-'}</EscalationPill>
          {accountLabel ? (
            <EscalationPill>
              {t('feedback.assignment_escalations.account_label', { value: accountLabel })}
            </EscalationPill>
          ) : null}
          {item.escalationReasons.map((reason) => (
            <EscalationReason key={reason} reason={reason} />
          ))}
        </div>
      </div>

      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-1 xl:grid-cols-2">
        <EscalationStatus
          label={t('feedback.assignment_escalations.owner_label')}
          value={ownerLabel || t('feedback.assignment_escalations.no_owner')}
          tone={ownerLabel ? 'success' : 'danger'}
        />
        <EscalationStatus
          label={t('feedback.assignment_escalations.due_label')}
          value={formatDue(item, (hours) =>
            hours < 0
              ? t('feedback.assignment_escalations.hours_overdue', {
                  count: Math.abs(hours),
                })
              : t('feedback.assignment_escalations.hours_left', { count: hours }),
          )}
          tone={dueTone(item)}
        />
      </div>

      <div className="flex flex-wrap items-center gap-2 lg:justify-end">
        <span className="text-xs text-muted-foreground">
          {t('feedback.assignment_escalations.opened_at', {
            value: formatDateDistance(item.createdAt),
          })}
        </span>
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={(event) => onOpenFeedback(item.feedbackId, event.currentTarget)}
          aria-label={t('feedback.assignment_escalations.open_feedback_with_id', {
            id: item.feedbackId,
          })}
        >
          <ArrowRight className="size-3.5" />
          {t('feedback.assignment_escalations.open_feedback')}
        </Button>
      </div>
    </div>
  )
}

function EscalationShell({ children, tone }: { children: ReactNode; tone?: 'default' | 'error' }) {
  return (
    <Card
      className={cn(
        'gap-0 overflow-hidden rounded-[1.05rem] border-border/65 bg-card py-0 shadow-none',
        tone === 'error' && 'border-destructive/25 bg-destructive/5',
      )}
    >
      {children}
    </Card>
  )
}

function EscalationMetric({
  label,
  value,
  icon,
  tone,
}: {
  label: string
  value: string
  icon: ReactNode
  tone?: 'neutral' | 'danger' | 'success' | 'warning'
}) {
  return (
    <div className={cn('rounded-[0.85rem] border px-3 py-2.5', metricToneClass(tone))}>
      <div className="flex items-center gap-2 text-[11px] font-medium text-muted-foreground">
        <span className="flex size-6 shrink-0 items-center justify-center rounded-md bg-background/85 text-foreground">
          {icon}
        </span>
        <span className="min-w-0 truncate">{label}</span>
      </div>
      <div className="mt-2 text-2xl font-semibold tracking-tight text-foreground">{value}</div>
    </div>
  )
}

function EscalationStatus({
  label,
  value,
  tone,
}: {
  label: string
  value: string
  tone?: 'neutral' | 'danger' | 'success' | 'warning'
}) {
  return (
    <div className={cn('rounded-[0.75rem] border px-3 py-2', metricToneClass(tone))}>
      <div className="text-[11px] font-medium text-muted-foreground">{label}</div>
      <div className="mt-1 truncate text-sm font-semibold text-foreground">{value}</div>
    </div>
  )
}

function EscalationReason({ reason }: { reason: string }) {
  const { t } = useTranslation()
  return (
    <span className={cn('rounded-md border px-2 py-1 font-semibold', reasonToneClass(reason))}>
      {t(`feedback.assignment_escalations.reason.${reason}`)}
    </span>
  )
}

function EscalationPill({ children }: { children: ReactNode }) {
  return (
    <span className="rounded-md border border-border/70 bg-background/80 px-2 py-1 font-medium">
      {children}
    </span>
  )
}

function toNumber(value: string | number | undefined) {
  const parsed = Number(value ?? 0)
  return Number.isFinite(parsed) ? parsed : 0
}

function priorityDotClass(priority: string) {
  if (priority === 'critical') return 'bg-destructive'
  if (priority === 'high') return 'bg-amber-500'
  if (priority === 'medium') return 'bg-sky-500'
  return 'bg-emerald-500'
}

function metricToneClass(tone: 'neutral' | 'danger' | 'success' | 'warning' = 'neutral') {
  if (tone === 'danger') return 'border-destructive/20 bg-destructive/8'
  if (tone === 'success') return 'border-emerald-200 bg-emerald-50'
  if (tone === 'warning') return 'border-amber-200 bg-amber-50'
  return 'border-border/70 bg-background/80'
}

function reasonToneClass(reason: string) {
  if (reason === 'overdue' || reason === 'missing_owner') {
    return 'border-destructive/20 bg-destructive/10 text-destructive'
  }
  if (reason === 'missing_sla' || reason === 'due_soon') {
    return 'border-amber-200 bg-amber-50 text-amber-700'
  }
  return 'border-border/70 bg-muted/45 text-muted-foreground'
}

function dueTone(item: FeedbackAssignmentEscalation) {
  if (item.escalationReasons.includes('overdue')) return 'danger'
  if (
    item.escalationReasons.includes('missing_sla') ||
    item.escalationReasons.includes('due_soon')
  ) {
    return 'warning'
  }
  return 'success'
}

function formatDue(item: FeedbackAssignmentEscalation, formatHours: (hours: number) => string) {
  const dueAt = item.assignment?.slaDueAt
  if (!dueAt) return '-'
  const date = new Date(dueAt)
  if (Number.isNaN(date.getTime())) return '-'
  if (typeof item.hoursUntilDue === 'number') {
    return formatHours(item.hoursUntilDue)
  }
  return formatDateTime(dueAt)
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
