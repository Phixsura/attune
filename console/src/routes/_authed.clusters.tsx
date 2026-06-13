import { useInfiniteQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { formatDistanceToNow } from 'date-fns'
import { enUS, type Locale, zhCN } from 'date-fns/locale'
import { AlertCircle, ChevronRight, Inbox, Layers, Loader2, Search } from 'lucide-react'
import { useCallback, useDeferredValue, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { List } from 'react-window'
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
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { clusterMembersInfiniteQuery } from '@/features/feedback/api/get-cluster-members'
import { type ClustersFilters, clustersInfiniteQuery } from '@/features/feedback/api/list-clusters'
import type { ClusterSummary } from '@/proto/attune/v1/clusters'

export const Route = createFileRoute('/_authed/clusters')({
  component: ClustersPage,
})

function getDateLocale(lang: string) {
  return lang.startsWith('zh') ? zhCN : enUS
}

function ClustersPage() {
  const { t, i18n } = useTranslation()
  const dateLocale = getDateLocale(i18n.language)

  const [filters, setFilters] = useState<ClustersFilters>({
    recencyDays: 30,
    minCount: 2,
    sort: 'latest_at',
  })
  const [qInput, setQInput] = useState('')
  const qDeferred = useDeferredValue(qInput)

  const queryFilters = useMemo(
    () => ({ ...filters, q: qDeferred.trim() || undefined }),
    [filters, qDeferred],
  )

  const list = useInfiniteQuery(clustersInfiniteQuery(queryFilters))

  const items = useMemo(() => list.data?.pages.flatMap((p) => p.items) ?? [], [list.data?.pages])
  const totalCount = list.data?.pages[0]?.totalCount ?? 0
  const clusteringEnabled = list.data?.pages[0]?.clusteringEnabled ?? false

  const [selectedCluster, setSelectedCluster] = useState<ClusterSummary | null>(null)

  const handleSelect = useCallback((c: ClusterSummary) => setSelectedCluster(c), [])
  const handleClose = useCallback(() => setSelectedCluster(null), [])
  const handleLoadMore = useCallback(() => void list.fetchNextPage(), [list])

  if (list.isPending) {
    return <Loading />
  }

  if (list.isError) {
    return (
      <div className="space-y-6">
        <PageHeader title={t('nav.clusters')} subtitle={t('feedback.clusters.page_subtitle')} />
        <Card>
          <CardContent className="py-12">
            <EmptyState
              icon={AlertCircle}
              title={t('common.error')}
              description={list.error?.message}
            />
          </CardContent>
        </Card>
      </div>
    )
  }

  if (!clusteringEnabled) {
    return (
      <div className="space-y-6">
        <PageHeader title={t('nav.clusters')} subtitle={t('feedback.clusters.page_subtitle')} />
        <Card className="border-dashed">
          <CardContent className="py-12">
            <EmptyState
              icon={Layers}
              title={t('feedback.clusters.disabled_title')}
              description={t('feedback.clusters.disabled_body')}
            />
          </CardContent>
        </Card>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <PageHeader title={t('nav.clusters')} subtitle={t('feedback.clusters.page_subtitle')} />

      <FilterBar filters={filters} q={qInput} onFiltersChange={setFilters} onQ={setQInput} />

      <Card className="gap-0 overflow-hidden py-0 shadow-none">
        <CardHeader className="border-b border-border/60 bg-muted/20 px-4 py-3">
          <CardTitle className="text-base">{t('feedback.clusters.title')}</CardTitle>
          <CardDescription>{t('feedback.clusters.total', { total: totalCount })}</CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {items.length > 0 ? (
            <ClusterList
              items={items}
              onSelect={handleSelect}
              hasNextPage={list.hasNextPage}
              isFetchingNextPage={list.isFetchingNextPage}
              onLoadMore={handleLoadMore}
              dateLocale={dateLocale}
            />
          ) : (
            <EmptyState
              icon={Layers}
              title={t('feedback.clusters.empty_title')}
              description={t('feedback.clusters.empty_body')}
              className="py-12"
            />
          )}
        </CardContent>
      </Card>

      <ClusterMembersSheet
        cluster={selectedCluster}
        onClose={handleClose}
        dateLocale={dateLocale}
      />
    </div>
  )
}

function PageHeader({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div>
      <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
      <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{subtitle}</p>
    </div>
  )
}

function FilterBar({
  filters,
  q,
  onFiltersChange,
  onQ,
}: {
  filters: ClustersFilters
  q: string
  onFiltersChange: (f: ClustersFilters) => void
  onQ: (v: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select
        value={filters.sort ?? 'latest_at'}
        onValueChange={(v) => onFiltersChange({ ...filters, sort: v as 'latest_at' | 'count' })}
      >
        <SelectTrigger className="w-[140px]">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="latest_at">{t('feedback.clusters.sort.latest_at')}</SelectItem>
          <SelectItem value="count">{t('feedback.clusters.sort.count')}</SelectItem>
        </SelectContent>
      </Select>

      <Select
        value={String(filters.recencyDays ?? 30)}
        onValueChange={(v) => onFiltersChange({ ...filters, recencyDays: Number(v) })}
      >
        <SelectTrigger className="w-[120px]">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="7">{t('feedback.clusters.filter.recency', { days: 7 })}</SelectItem>
          <SelectItem value="30">{t('feedback.clusters.filter.recency', { days: 30 })}</SelectItem>
          <SelectItem value="90">{t('feedback.clusters.filter.recency', { days: 90 })}</SelectItem>
        </SelectContent>
      </Select>

      <Select
        value={String(filters.minCount ?? 2)}
        onValueChange={(v) => onFiltersChange({ ...filters, minCount: Number(v) })}
      >
        <SelectTrigger className="w-[120px]">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="2">{t('feedback.clusters.filter.min_count', { count: 2 })}</SelectItem>
          <SelectItem value="3">{t('feedback.clusters.filter.min_count', { count: 3 })}</SelectItem>
          <SelectItem value="5">{t('feedback.clusters.filter.min_count', { count: 5 })}</SelectItem>
        </SelectContent>
      </Select>

      <div className="relative min-w-[14rem] flex-1 sm:max-w-[280px]">
        <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          type="search"
          placeholder={t('feedback.clusters.filter.search_placeholder')}
          value={q}
          onChange={(e) => onQ(e.target.value)}
          className="pl-8"
          aria-label={t('feedback.clusters.filter.search_placeholder')}
        />
      </div>
    </div>
  )
}

const ROW_HEIGHT = 64

interface ClusterRowProps {
  items: ClusterSummary[]
  onSelect: (c: ClusterSummary) => void
  hasNextPage: boolean
  isFetchingNextPage: boolean
  onLoadMore: () => void
  dateLocale: Locale
}

function ClusterRow({
  index,
  style,
  ariaAttributes,
  items,
  onSelect,
  isFetchingNextPage,
  onLoadMore,
  dateLocale,
}: {
  index: number
  style: React.CSSProperties
  ariaAttributes: { 'aria-posinset': number; 'aria-setsize': number; role: 'listitem' }
} & ClusterRowProps) {
  const { t } = useTranslation()

  if (index >= items.length) {
    return (
      <div style={style} className="flex items-center justify-center border-t border-border/40">
        {isFetchingNextPage ? (
          <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
        ) : (
          <Button variant="ghost" size="sm" onClick={onLoadMore}>
            {t('feedback.clusters.load_more')}
          </Button>
        )}
      </div>
    )
  }

  const cluster = items[index]

  return (
    <div style={style} {...ariaAttributes}>
      <button
        type="button"
        onClick={() => onSelect(cluster)}
        aria-label={`${cluster.label || cluster.sampleTitle}, ${cluster.count} ${t('feedback.clusters.count', { count: cluster.count })}`}
        className={`group flex h-full w-full items-center gap-4 px-4 text-left transition-colors hover:bg-muted/40 ${
          index > 0 ? 'border-t border-border/40' : ''
        }`}
      >
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="truncate text-sm font-medium text-foreground">
              {cluster.label || cluster.sampleTitle}
            </span>
            <span className="shrink-0 rounded bg-muted/80 px-1.5 py-0.5 text-[11px] tabular-nums text-muted-foreground">
              {cluster.count}
            </span>
          </div>
          {cluster.label && cluster.sampleTitle !== cluster.label && (
            <p className="mt-0.5 truncate text-xs text-muted-foreground">{cluster.sampleTitle}</p>
          )}
        </div>
        <span className="shrink-0 text-xs tabular-nums text-muted-foreground/70">
          {formatDistanceToNow(new Date(cluster.latestAt), { addSuffix: true, locale: dateLocale })}
        </span>
        <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground/50 transition-transform group-hover:translate-x-0.5 group-hover:text-muted-foreground" />
      </button>
    </div>
  )
}

function ClusterList({
  items,
  onSelect,
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
  dateLocale,
}: ClusterRowProps) {
  const itemCount = items.length + (hasNextPage ? 1 : 0)
  const listHeight = useMemo(() => Math.min(itemCount * ROW_HEIGHT, 560), [itemCount])

  return (
    <List
      style={{ height: listHeight }}
      rowCount={itemCount}
      rowHeight={ROW_HEIGHT}
      rowComponent={ClusterRow}
      rowProps={{ items, onSelect, hasNextPage, isFetchingNextPage, onLoadMore, dateLocale }}
    />
  )
}

function ClusterMembersSheet({
  cluster,
  onClose,
  dateLocale,
}: {
  cluster: ClusterSummary | null
  onClose: () => void
  dateLocale: Locale
}) {
  const { t } = useTranslation()
  const members = useInfiniteQuery(clusterMembersInfiniteQuery(cluster?.clusterId ?? ''))

  const items = useMemo(
    () => members.data?.pages.flatMap((p) => p.items) ?? [],
    [members.data?.pages],
  )
  const totalCount = members.data?.pages[0]?.totalCount ?? 0
  const loadedCount = items.length
  const hasMore = members.hasNextPage

  const handleLoadMore = useCallback(() => void members.fetchNextPage(), [members])

  return (
    <Sheet open={!!cluster} onOpenChange={(v) => !v && onClose()}>
      <SheetContent className="flex w-full flex-col gap-0 overflow-hidden p-0 sm:max-w-md">
        <SheetHeader className="shrink-0 space-y-3 border-b border-border/50 bg-muted/30 px-5 py-4">
          <div>
            <SheetTitle className="text-base font-semibold">
              {cluster?.label || t('feedback.clusters.members_title')}
            </SheetTitle>
            <SheetDescription className="mt-0.5 text-xs">
              {cluster?.sampleTitle !== cluster?.label && cluster?.sampleTitle}
            </SheetDescription>
          </div>
          {totalCount > 0 && (
            <div className="flex items-center justify-between rounded-md bg-background/80 px-3 py-2">
              <span className="text-xs text-muted-foreground">
                {t('feedback.clusters.loaded_of_total', { loaded: loadedCount, total: totalCount })}
              </span>
              <div className="flex items-center gap-2">
                <div className="h-1.5 w-24 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-primary/70 transition-all duration-300"
                    style={{ width: `${Math.min((loadedCount / totalCount) * 100, 100)}%` }}
                  />
                </div>
                <span className="min-w-[3ch] text-right text-xs tabular-nums text-muted-foreground">
                  {Math.round((loadedCount / totalCount) * 100)}%
                </span>
              </div>
            </div>
          )}
        </SheetHeader>

        <div className="flex-1 overflow-y-auto">
          {members.isError ? (
            <EmptyState
              icon={AlertCircle}
              title={t('common.error')}
              description={members.error?.message}
              className="py-12"
            />
          ) : members.isPending ? (
            <div className="flex items-center justify-center py-12 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
              {t('app.loading')}
            </div>
          ) : items.length === 0 ? (
            <EmptyState
              icon={Inbox}
              title={t('feedback.clusters.members_empty')}
              className="py-12"
            />
          ) : (
            <div className="space-y-2 p-4">
              {items.map((member, idx) => (
                <div
                  key={member.id}
                  className="group rounded-lg border border-border/50 bg-card p-3 transition-colors hover:border-border hover:bg-muted/20"
                >
                  <div className="flex items-start gap-3">
                    <div className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-muted/60 text-xs font-medium tabular-nums text-muted-foreground">
                      {idx + 1}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-start justify-between gap-2">
                        <p className="text-sm font-medium leading-snug text-foreground">
                          {member.enrichedTitle || `#${member.id}`}
                        </p>
                        {member.similarity > 0.01 && (
                          <span className="shrink-0 rounded bg-emerald-500/10 px-1.5 py-0.5 text-[11px] tabular-nums font-medium text-emerald-600 dark:text-emerald-400">
                            {Math.round(member.similarity * 100)}%
                          </span>
                        )}
                      </div>
                      <p className="mt-1.5 text-xs leading-relaxed text-muted-foreground line-clamp-2">
                        {member.content}
                      </p>
                      <div className="mt-2 flex items-center gap-2 text-[11px] text-muted-foreground/70">
                        <span className="rounded bg-muted/80 px-1.5 py-0.5">{member.source}</span>
                        <span>·</span>
                        <span className="tabular-nums">
                          {formatDistanceToNow(new Date(member.createdAt), {
                            addSuffix: true,
                            locale: dateLocale,
                          })}
                        </span>
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {hasMore && (
          <div className="shrink-0 border-t border-border/50 bg-muted/20 px-4 py-3">
            <Button
              variant="outline"
              size="sm"
              className="w-full"
              onClick={handleLoadMore}
              disabled={members.isFetchingNextPage}
            >
              {members.isFetchingNextPage && <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />}
              {t('feedback.clusters.load_more')}
            </Button>
          </div>
        )}
      </SheetContent>
    </Sheet>
  )
}
