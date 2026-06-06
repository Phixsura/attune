import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { AlertTriangle, CheckCircle2, Loader2, Pencil, Trash2, XCircle, Zap } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { NotifyTarget } from '@/features/notify-targets/api/list-notify-targets'
import type { NotifyTestResult } from '@/features/notify-targets/api/test-notify-target'

// Extracted from _authed.notify-targets.tsx to keep that route ≤300 lines
// (CLAUDE.md 律 2). Pure presentation — no queries, no state; parent
// owns lastTest + mutation pending flags and passes them in.

export interface TestState {
  id: string
  result: NotifyTestResult | { ok: false; statusCode?: number; message?: string }
}

export function TargetTable({
  targets,
  lastTest,
  testingId,
  onTest,
  onEdit,
  onDelete,
}: {
  targets: NotifyTarget[]
  lastTest: TestState | null
  testingId: string | undefined
  onTest: (t: NotifyTarget) => void
  onEdit: (t: NotifyTarget) => void
  onDelete: (t: NotifyTarget) => void
}) {
  const { t } = useTranslation()
  const typeLabel: Record<string, string> = {
    'lark-bot': t('notify_targets.type_label.lark-bot'),
    'raw-webhook': t('notify_targets.type_label.raw-webhook'),
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('notify_targets.table.type')}</TableHead>
          <TableHead>{t('notify_targets.table.url')}</TableHead>
          <TableHead>{t('notify_targets.table.status')}</TableHead>
          <TableHead>{t('notify_targets.table.timeout')}</TableHead>
          <TableHead className="text-right">{t('notify_targets.table.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {targets.map((tgt) => (
          <TableRow key={tgt.id} className={tgt.disabled ? 'opacity-50' : ''}>
            <TableCell>{typeLabel[tgt.destinationType] ?? tgt.destinationType}</TableCell>
            <TableCell className="max-w-[24rem] truncate font-mono text-xs" title={tgt.url}>
              {tgt.url}
              {lastTest?.id === tgt.id && (
                <span className="ml-2">
                  {lastTest.result.ok ? (
                    <CheckCircle2 className="inline h-3.5 w-3.5 text-green-600" />
                  ) : (
                    <XCircle className="inline h-3.5 w-3.5 text-destructive" />
                  )}
                </span>
              )}
            </TableCell>
            <TableCell>
              <FailureBadge target={tgt} />
            </TableCell>
            <TableCell className="text-muted-foreground">{tgt.timeoutSeconds}s</TableCell>
            <TableCell className="text-right">
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onTest(tgt)}
                disabled={testingId === tgt.id}
                title={t('notify_targets.test_button')}
              >
                {testingId === tgt.id ? (
                  <Loader2 className="h-3.5 w-3.5 animate-spin" />
                ) : (
                  <Zap className="h-3.5 w-3.5" />
                )}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onEdit(tgt)}
                title={t('notify_targets.edit_button')}
              >
                <Pencil className="h-3.5 w-3.5" />
              </Button>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => onDelete(tgt)}
                title={t('notify_targets.delete_button')}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function FailureBadge({ target }: { target: NotifyTarget }) {
  const { t } = useTranslation()
  if (!target.lastFailureAt) {
    return <span className="text-xs text-green-600">{t('notify_targets.status_ok')}</span>
  }
  const ago = formatDistanceToNow(new Date(target.lastFailureAt), {
    addSuffix: false,
    locale: zhCN,
  })
  const tooltip = t('notify_targets.failure_tooltip', {
    at: target.lastFailureAt,
    reason: target.lastError || '—',
  })
  return (
    <span
      className="inline-flex items-center gap-1 rounded-md border border-destructive/30 bg-destructive/10 px-1.5 py-0.5 text-xs text-destructive"
      title={tooltip}
    >
      <AlertTriangle className="h-3 w-3" />
      {t('notify_targets.failure_badge', { ago })}
    </span>
  )
}
