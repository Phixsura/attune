import { useQuery } from '@tanstack/react-query'
import { format, formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { ArrowRight, Clock3, Loader2, MessageSquareText, UserCircle2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { type AuditEntry, auditQuery } from '@/features/workflow/api/list-audit'

export function AuditTimeline({ feedbackId }: { feedbackId: string }) {
  const { t } = useTranslation()
  const audit = useQuery(auditQuery(feedbackId))

  if (audit.isPending) {
    return (
      <div className="rounded-xl border border-dashed border-border/70 bg-muted/[0.08] px-4 py-4">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <Loader2 className="h-3.5 w-3.5 animate-spin" />
          {t('app.loading')}
        </div>
      </div>
    )
  }

  const entries = audit.data ?? []
  if (entries.length === 0) {
    return (
      <div className="rounded-xl border border-dashed border-border/70 bg-background px-4 py-4">
        <p className="text-sm font-medium">{t('workflow.audit.empty')}</p>
        <p className="mt-1 text-xs leading-6 text-muted-foreground">
          {t('workflow.audit.empty_hint')}
        </p>
      </div>
    )
  }

  return (
    <ol className="space-y-3" aria-label={t('feedback.detail.audit_log')}>
      {entries.map((entry) => (
        <AuditItem key={entry.id} entry={entry} />
      ))}
    </ol>
  )
}

function AuditItem({ entry }: { entry: AuditEntry }) {
  const { t } = useTranslation()
  const fieldLabel = formatAuditFieldName(entry.fieldName, t)
  const ago =
    entry.createdAt && !Number.isNaN(new Date(entry.createdAt).getTime())
      ? formatDistanceToNow(new Date(entry.createdAt), { addSuffix: true, locale: zhCN })
      : ''
  const stamp =
    entry.createdAt && !Number.isNaN(new Date(entry.createdAt).getTime())
      ? format(new Date(entry.createdAt), 'PPP HH:mm', { locale: zhCN })
      : ''

  return (
    <li className="rounded-xl border border-border/60 bg-background px-4 py-4">
      <div className="flex gap-3">
        <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <Clock3 className="h-4 w-4" />
        </div>
        <div className="min-w-0 flex-1 space-y-3">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0">
              <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                {fieldLabel}
              </p>
              <div className="mt-2 flex flex-wrap items-center gap-2 text-sm">
                {entry.oldValue ? (
                  <ValuePill tone="from" value={entry.oldValue} />
                ) : (
                  <ValuePill tone="empty" value={t('workflow.audit.initial_value')} />
                )}
                <ArrowRight className="h-3.5 w-3.5 text-muted-foreground" />
                <ValuePill tone="to" value={entry.newValue || t('workflow.audit.empty_value')} />
              </div>
            </div>
            {stamp ? (
              <div className="shrink-0 text-right text-[11px] leading-5 text-muted-foreground">
                <div>{stamp}</div>
                <div>{ago}</div>
              </div>
            ) : null}
          </div>

          {entry.comment ? (
            <div className="rounded-lg border border-border/50 bg-muted/[0.08] px-3 py-2">
              <p className="inline-flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
                <MessageSquareText className="h-3.5 w-3.5" />
                {t('workflow.audit.comment')}
              </p>
              <p className="mt-1 text-sm leading-6">{entry.comment}</p>
            </div>
          ) : null}

          <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
            <span className="inline-flex items-center gap-1.5 rounded-full bg-muted px-2.5 py-1">
              <UserCircle2 className="h-3.5 w-3.5" />
              {entry.changedBy || t('workflow.audit.unknown_actor')}
            </span>
            {!stamp && ago ? <span>{ago}</span> : null}
          </div>
        </div>
      </div>
    </li>
  )
}

function ValuePill({ value, tone }: { value: string; tone: 'from' | 'to' | 'empty' }) {
  const toneClass =
    tone === 'to'
      ? 'border-emerald-200/80 bg-emerald-50/70 text-emerald-700'
      : tone === 'from'
        ? 'border-border/60 bg-muted/50 text-foreground'
        : 'border-dashed border-border/60 bg-background text-muted-foreground'

  return (
    <span
      className={`inline-flex min-h-8 max-w-full items-center rounded-full border px-3 py-1 text-sm ${toneClass}`}
    >
      <span className="truncate">{value}</span>
    </span>
  )
}

function formatAuditFieldName(fieldName: string, t: (key: string) => string) {
  const preset: Record<string, string> = {
    workflow_state_id: t('workflow.audit.field.workflow_state'),
    tag_ids: t('workflow.audit.field.tags'),
    tags: t('workflow.audit.field.tags'),
  }

  return preset[fieldName] ?? fieldName
}
