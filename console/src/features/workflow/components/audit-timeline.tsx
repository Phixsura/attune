import { useQuery } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Clock, Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { type AuditEntry, auditQuery } from '@/features/workflow/api/list-audit'

export function AuditTimeline({ feedbackId }: { feedbackId: number }) {
  const { t } = useTranslation()
  const audit = useQuery(auditQuery(feedbackId))

  if (audit.isPending) {
    return (
      <div className="flex items-center gap-2 py-4 text-xs text-muted-foreground">
        <Loader2 className="h-3.5 w-3.5 animate-spin" />
        {t('app.loading')}
      </div>
    )
  }

  const entries = audit.data ?? []
  if (entries.length === 0) {
    return <p className="py-2 text-xs text-muted-foreground">{t('workflow.audit.empty')}</p>
  }

  return (
    <ol className="space-y-3">
      {entries.map((entry) => (
        <AuditItem key={entry.id} entry={entry} />
      ))}
    </ol>
  )
}

function AuditItem({ entry }: { entry: AuditEntry }) {
  const ago = entry.createdAt
    ? formatDistanceToNow(new Date(entry.createdAt), { addSuffix: true, locale: zhCN })
    : ''

  return (
    <li className="flex gap-3 text-xs">
      <div className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-muted">
        <Clock className="h-3 w-3 text-muted-foreground" />
      </div>
      <div className="min-w-0 flex-1">
        <p>
          <span className="font-medium">{entry.fieldName}</span>
          {entry.oldValue && (
            <>
              {' '}
              <span className="text-muted-foreground">{entry.oldValue}</span>
            </>
          )}
          {' → '}
          <span className="font-medium">{entry.newValue}</span>
        </p>
        {entry.comment && <p className="mt-0.5 text-muted-foreground">"{entry.comment}"</p>}
        <p className="mt-0.5 text-muted-foreground">
          {entry.changedBy} · {ago}
        </p>
      </div>
    </li>
  )
}
