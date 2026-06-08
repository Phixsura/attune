import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Inbox, Loader2 } from 'lucide-react'
import { useDeferredValue, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { DimensionChips, UrgentDot } from '@/components/dim/dimension-chips'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
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
  type AttrFilterEntry,
  type Feedback,
  type FeedbackListFilters,
  feedbackListInfiniteQuery,
} from '@/features/feedback/api/list-feedback-infinite'
import { FeedbackDetailSheet } from '@/features/feedback/components/detail-sheet'
import { DimStatsBars } from '@/features/feedback/components/dim-stats-bars'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { useDisplayName } from '@/lib/i18n-resolve'
import type { Dimension } from '@/proto/attune/v1/common'

export const Route = createFileRoute('/_authed/feedback')({
  component: FeedbackPage,
})

function FeedbackPage() {
  const { t } = useTranslation()
  const config = useQuery(enrichConfigQuery())
  const dims = config.data?.dimensions ?? []

  const [attrFilters, setAttrFilters] = useState<Record<string, string>>({})
  const [qInput, setQInput] = useState('')
  const qDeferred = useDeferredValue(qInput)
  const [detailId, setDetailId] = useState<string | null>(null)

  const filters: FeedbackListFilters = useMemo(() => {
    const attrs: AttrFilterEntry[] = Object.entries(attrFilters)
      .filter(([, v]) => v && v !== '__all')
      .map(([dim, value]) => ({ dim, value }))
    return { attrs, q: qDeferred.trim() }
  }, [attrFilters, qDeferred])

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
            <CardTitle className="text-base">{t('feedback.stats.title')}</CardTitle>
          </CardHeader>
          <CardContent>
            <DimStatsBars dims={dims} stats={stats.data.dims} total={Number(stats.data.total)} />
          </CardContent>
        </Card>
      )}

      <FilterBar
        dims={dims}
        attrFilters={attrFilters}
        q={qInput}
        onAttrChange={(dim, value) => setAttrFilters((m) => ({ ...m, [dim]: value }))}
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
            <FeedbackTable items={items} dims={dims} onRowClick={setDetailId} />
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

      <FeedbackDetailSheet
        id={detailId}
        dims={dims}
        onOpenChange={(v) => !v && setDetailId(null)}
      />
    </div>
  )
}

// FilterBar renders one Select per single-kind dim with a non-empty
// taxonomy (the only shape where a dropdown filter makes sense). Multi
// dims and freeform dims are skipped — they'd need a chip-picker that
// isn't worth the complexity until customer feedback asks for it.
function FilterBar({
  dims,
  attrFilters,
  q,
  onAttrChange,
  onQ,
}: {
  dims: Dimension[]
  attrFilters: Record<string, string>
  q: string
  onAttrChange: (dim: string, value: string) => void
  onQ: (v: string) => void
}) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  return (
    <div className="mt-6 flex flex-wrap items-center gap-2">
      {dims
        .filter((d) => d.kind === 'single' && d.taxonomy.length > 0)
        .map((d) => (
          <Select
            key={d.name}
            value={attrFilters[d.name] || '__all'}
            onValueChange={(v) => onAttrChange(d.name, v)}
          >
            <SelectTrigger className="w-[180px]">
              <SelectValue placeholder={displayOf(d.displayName) || d.name} />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="__all">
                {t('feedback.filter.all', { dim: displayOf(d.displayName) || d.name })}
              </SelectItem>
              {d.taxonomy.map((tax) => (
                <SelectItem key={tax.value} value={tax.value}>
                  {displayOf(tax.displayName) || tax.value}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        ))}
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
  dims,
  onRowClick,
}: {
  items: Feedback[]
  dims: Dimension[]
  onRowClick: (id: string) => void
}) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('feedback.table.title')}</TableHead>
          {dims.map((d) => (
            <TableHead key={d.name} className="w-[140px]">
              {displayOf(d.displayName) || d.name}
            </TableHead>
          ))}
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
              <div className="flex items-center gap-1 truncate font-medium">
                <UrgentDot urgent={f.isUrgent} />
                {f.enrichedTitle || `#${f.id}`}
              </div>
              <div className="truncate text-xs text-muted-foreground">{f.content}</div>
            </TableCell>
            {dims.map((d) => (
              <TableCell key={d.name}>
                <DimensionChips
                  dim={d}
                  value={(f.enrichedAttrs as Record<string, unknown> | undefined)?.[d.name]}
                />
              </TableCell>
            ))}
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
