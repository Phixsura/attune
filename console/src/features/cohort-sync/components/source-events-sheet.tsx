import { useQuery } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { useTranslation } from 'react-i18next'
import { Loading } from '@/components/loading'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  type CohortSource,
  type CohortSyncEvent,
  listCohortSyncEventsQuery,
} from '../api/cohort-sync'

export function SourceEventsSheet({
  source,
  open,
  onOpenChange,
}: {
  source: CohortSource
  open: boolean
  onOpenChange: (v: boolean) => void
}) {
  const { t } = useTranslation()
  const eventsQ = useQuery(listCohortSyncEventsQuery(source.id))

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-[480px] overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{source.name}</SheetTitle>
          <SheetDescription>
            {source.provider} · {t('cohort_sync.events.title')}
          </SheetDescription>
        </SheetHeader>

        <section className="mt-4">
          {eventsQ.isLoading ? (
            <Loading />
          ) : eventsQ.isError ? (
            <p className="text-sm text-destructive">{t('common.error')}</p>
          ) : (
            <EventsTable events={eventsQ.data ?? []} />
          )}
        </section>
      </SheetContent>
    </Sheet>
  )
}

function EventsTable({ events }: { events: CohortSyncEvent[] }) {
  const { t } = useTranslation()
  if (events.length === 0) {
    return <p className="text-sm text-muted-foreground">{t('cohort_sync.events.no_events')}</p>
  }
  return (
    <div className="max-h-[70vh] overflow-y-auto rounded-md border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('cohort_sync.events.time')}</TableHead>
            <TableHead>{t('cohort_sync.events.type')}</TableHead>
            <TableHead>{t('cohort_sync.run.status_label')}</TableHead>
            <TableHead>{t('cohort_sync.cohort.members')}</TableHead>
            <TableHead>{t('cohort_sync.run.error')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {events.map((event) => (
            <TableRow key={event.id}>
              <TableCell className="text-xs text-muted-foreground">
                {event.receivedAt
                  ? formatDistanceToNow(new Date(event.receivedAt), {
                      addSuffix: true,
                      locale: zhCN,
                    })
                  : '—'}
              </TableCell>
              <TableCell className="text-xs">{event.eventType}</TableCell>
              <TableCell>
                <EventStatusBadge status={event.status} />
              </TableCell>
              <TableCell className="text-xs">{event.membersCount}</TableCell>
              <TableCell
                className="max-w-[10rem] truncate text-xs text-destructive"
                title={event.failureReason}
              >
                {event.failureReason || '—'}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function EventStatusBadge({ status }: { status: string }) {
  const map: Record<string, string> = {
    received: 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
    processed: 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-400',
    failed: 'bg-destructive/10 text-destructive',
  }
  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs ${map[status] ?? 'bg-muted text-muted-foreground'}`}
    >
      {status}
    </span>
  )
}
