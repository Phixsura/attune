import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Loader2, RotateCcw } from 'lucide-react'
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
import type { OutboxDelivery } from '@/features/outbox-dead/api/list-deliveries'
import {
  failureKindLabel,
  hasFailureKind,
  httpStatusLabel,
} from '@/features/outbox-dead/lib/failure-label'

// Pure presentation — no queries, no state. The page owns the retry
// mutation + pending id and passes them in, mirroring the notify-targets
// TargetTable split.

export function DeliveriesTable({
  deliveries,
  retryingId,
  onRetry,
}: {
  deliveries: OutboxDelivery[]
  retryingId: string | undefined
  onRetry: (d: OutboxDelivery) => void
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('outbox_dead.table.destination')}</TableHead>
          <TableHead>{t('outbox_dead.table.audience')}</TableHead>
          <TableHead>{t('outbox_dead.table.status')}</TableHead>
          <TableHead className="text-right">{t('outbox_dead.table.attempts')}</TableHead>
          <TableHead>{t('outbox_dead.table.failure')}</TableHead>
          <TableHead>{t('outbox_dead.table.reason')}</TableHead>
          <TableHead>{t('outbox_dead.table.last_attempt')}</TableHead>
          <TableHead className="text-right">{t('outbox_dead.table.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {deliveries.map((d) => (
          <DeliveryRow
            key={d.id}
            delivery={d}
            retrying={retryingId === d.id}
            onRetry={() => onRetry(d)}
          />
        ))}
      </TableBody>
    </Table>
  )
}

function DeliveryRow({
  delivery,
  retrying,
  onRetry,
}: {
  delivery: OutboxDelivery
  retrying: boolean
  onRetry: () => void
}) {
  const { t } = useTranslation()
  // last-attempt timestamp: deliveredAt is set only on success, so for a
  // dead/failed row the freshest signal is the last manual retry, then
  // the next-retry watermark, then created. Show whichever exists.
  const lastAt = delivery.lastManualRetryAt || delivery.nextRetryAt || delivery.createdAt
  const reason = delivery.deadReason || delivery.lastError
  return (
    <TableRow>
      <TableCell className="max-w-[18rem]">
        <div className="truncate font-medium" title={delivery.destinationTarget}>
          {delivery.destinationTarget || '—'}
        </div>
        <div className="text-xs text-muted-foreground">{delivery.destinationType || '—'}</div>
      </TableCell>
      <TableCell className="text-muted-foreground">{delivery.audience || '—'}</TableCell>
      <TableCell>
        <StatusBadge status={delivery.status} inFlight={delivery.inFlight} />
      </TableCell>
      <TableCell className="text-right tabular-nums">{delivery.attempts}</TableCell>
      <TableCell>
        <FailureCell delivery={delivery} />
      </TableCell>
      <TableCell className="max-w-[20rem]">
        {reason ? (
          <span className="block truncate text-xs text-muted-foreground" title={reason}>
            {reason}
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">—</span>
        )}
      </TableCell>
      <TableCell className="text-muted-foreground" title={lastAt || undefined}>
        {lastAt
          ? formatDistanceToNow(new Date(lastAt), { addSuffix: true, locale: zhCN })
          : t('common.never')}
      </TableCell>
      <TableCell className="text-right">
        <Button
          variant="ghost"
          size="sm"
          onClick={onRetry}
          disabled={retrying || delivery.inFlight}
          aria-label={t('outbox_dead.retry_button')}
          title={
            delivery.inFlight ? t('outbox_dead.retry_inflight') : t('outbox_dead.retry_button')
          }
        >
          {retrying ? (
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
          ) : (
            <RotateCcw className="h-3.5 w-3.5" />
          )}
        </Button>
      </TableCell>
    </TableRow>
  )
}

function StatusBadge({ status, inFlight }: { status: string; inFlight: boolean }) {
  const { t } = useTranslation()
  const tone =
    status === 'dead'
      ? 'border-destructive/30 bg-destructive/10 text-destructive'
      : 'border-amber-500/30 bg-amber-500/10 text-amber-600'
  return (
    <div className="flex flex-col gap-1">
      <span
        className={`inline-flex w-fit items-center rounded-md border px-1.5 py-0.5 text-xs ${tone}`}
      >
        {status || '—'}
      </span>
      {inFlight && (
        <span className="text-[11px] text-muted-foreground">{t('outbox_dead.inflight')}</span>
      )}
    </div>
  )
}

function FailureCell({ delivery }: { delivery: OutboxDelivery }) {
  const { t } = useTranslation()
  const http = httpStatusLabel(delivery.httpStatus)
  const showKind = hasFailureKind(delivery.failureKind)
  if (!http && !showKind) {
    return <span className="text-xs text-muted-foreground">—</span>
  }
  return (
    <div className="flex flex-wrap gap-1">
      {showKind && (
        <span className="inline-flex items-center rounded-md border border-border bg-muted/40 px-1.5 py-0.5 text-xs">
          {failureKindLabel(t, delivery.failureKind)}
        </span>
      )}
      {http && (
        <span className="inline-flex items-center rounded-md border border-border bg-muted/40 px-1.5 py-0.5 font-mono text-xs">
          {http}
        </span>
      )}
    </div>
  )
}
