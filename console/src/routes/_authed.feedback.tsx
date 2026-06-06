import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Inbox, Loader2 } from 'lucide-react'
import { useDeferredValue, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { feedbackStatsQuery } from '@/features/feedback/api/get-feedback-stats'
import {
  type Feedback,
  type FeedbackListFilters,
  feedbackListInfiniteQuery,
} from '@/features/feedback/api/list-feedback-infinite'
import { KindBadge, SeverityBadge } from '@/features/feedback/components/badges'
import { FeedbackDetailSheet } from '@/features/feedback/components/detail-sheet'
import { KindDonut } from '@/features/feedback/components/kind-donut'

export const Route = createFileRoute('/_authed/feedback')({
  component: FeedbackPage,
})

const KIND_OPTIONS = ['bug', 'feature', 'ops', 'question', 'other'] as const
const SEVERITY_OPTIONS = ['P0', 'P1', 'P2', 'P3'] as const

function FeedbackPage() {
  const { t } = useTranslation()
  const [kind, setKind] = useState<string>('')
  const [severity, setSeverity] = useState<string>('')
  const [qInput, setQInput] = useState('')
  const qDeferred = useDeferredValue(qInput)
  const [detailId, setDetailId] = useState<string | null>(null)

  const filters: FeedbackListFilters = useMemo(
    () => ({ kind, severity, q: qDeferred.trim() }),
    [kind, severity, qDeferred],
  )
  const list = useInfiniteQuery(feedbackListInfiniteQuery(filters))
  const items = list.data?.pages.flatMap((p) => p.items) ?? []
  const total = items.length
  const stats = useQuery(feedbackStatsQuery())

  return (
    <div>
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t('nav.feedback')}</h1>
        <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{t('feedback.subtitle')}</p>
      </div>

      {stats.data && Number(stats.data.total) > 0 && (
        <Card className="mt-6">
          <CardHeader>
            <CardTitle className="text-base">{t('feedback.donut_title')}</CardTitle>
          </CardHeader>
          <CardContent>
            <KindDonut
              byKind={Object.fromEntries(
                Object.entries(stats.data.byKind).map(([k, v]) => [k, Number(v)]),
              )}
              total={Number(stats.data.total)}
            />
          </CardContent>
        </Card>
      )}

      <FilterBar
        kind={kind}
        severity={severity}
        q={qInput}
        onKind={setKind}
        onSeverity={setSeverity}
        onQ={setQInput}
      />

      <Card className="mt-4">
        <CardHeader>
          <CardTitle>{t('nav.feedback')}</CardTitle>
          <CardDescription>{total}</CardDescription>
        </CardHeader>
        <CardContent>
          {list.isPending ? (
            <Loading />
          ) : items.length > 0 ? (
            <FeedbackTable items={items} onRowClick={setDetailId} />
          ) : (
            <EmptyState
              icon={Inbox}
              title={t('feedback.empty_title')}
              description={t('feedback.empty_body')}
            />
          )}

          {list.hasNextPage && (
            <div className="mt-4 flex justify-center">
              <Button
                variant="ghost"
                onClick={() => void list.fetchNextPage()}
                disabled={list.isFetchingNextPage}
              >
                {list.isFetchingNextPage && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
                {t('feedback.load_more')}
              </Button>
            </div>
          )}
        </CardContent>
      </Card>

      <FeedbackDetailSheet id={detailId} onOpenChange={(v) => !v && setDetailId(null)} />
    </div>
  )
}

function Loading() {
  const { t } = useTranslation()
  return (
    <div className="flex items-center justify-center py-8 text-muted-foreground">
      <Loader2 className="mr-2 h-4 w-4 animate-spin" />
      {t('app.loading')}
    </div>
  )
}

function FilterBar({
  kind,
  severity,
  q,
  onKind,
  onSeverity,
  onQ,
}: {
  kind: string
  severity: string
  q: string
  onKind: (v: string) => void
  onSeverity: (v: string) => void
  onQ: (v: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="mt-6 flex flex-wrap items-center gap-2">
      <Select value={kind || 'all'} onValueChange={(v) => onKind(v === 'all' ? '' : v)}>
        <SelectTrigger className="w-[160px]">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{t('feedback.filter.kind_all')}</SelectItem>
          {KIND_OPTIONS.map((k) => (
            <SelectItem key={k} value={k}>
              {t(`feedback.kind.${k}`)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select value={severity || 'all'} onValueChange={(v) => onSeverity(v === 'all' ? '' : v)}>
        <SelectTrigger className="w-[160px]">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{t('feedback.filter.severity_all')}</SelectItem>
          {SEVERITY_OPTIONS.map((s) => (
            <SelectItem key={s} value={s}>
              {t(`feedback.severity.${s}`)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Input
        type="search"
        placeholder={t('feedback.filter.search_placeholder')}
        value={q}
        onChange={(e) => onQ(e.target.value)}
        className="max-w-[280px]"
      />
    </div>
  )
}

function FeedbackTable({
  items,
  onRowClick,
}: {
  items: Feedback[]
  onRowClick: (id: string) => void
}) {
  const { t } = useTranslation()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('feedback.table.title')}</TableHead>
          <TableHead className="w-[100px]">{t('feedback.table.kind')}</TableHead>
          <TableHead className="w-[110px]">{t('feedback.table.severity')}</TableHead>
          <TableHead className="w-[120px]">{t('feedback.table.user')}</TableHead>
          <TableHead className="w-[120px]">{t('feedback.table.time')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {items.map((f) => (
          <TableRow
            key={f.id}
            onClick={() => onRowClick(f.id)}
            className="cursor-pointer hover:bg-muted/40"
          >
            <TableCell className="max-w-[28rem]">
              <div className="truncate font-medium">{f.enrichedTitle || `#${f.id}`}</div>
              <div className="truncate text-xs text-muted-foreground">{f.content}</div>
            </TableCell>
            <TableCell>
              <KindBadge kind={f.enrichedKind} />
            </TableCell>
            <TableCell>
              <SeverityBadge severity={f.enrichedSeverity} />
            </TableCell>
            <TableCell className="truncate font-mono text-xs text-muted-foreground">
              {f.userId || '—'}
            </TableCell>
            <TableCell className="text-xs text-muted-foreground">
              {formatDistanceToNow(new Date(f.createdAt), { addSuffix: true, locale: zhCN })}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
