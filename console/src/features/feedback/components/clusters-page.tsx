import { useInfiniteQuery } from '@tanstack/react-query'
import { formatDistanceToNow } from 'date-fns'
import { enUS, type Locale, zhCN } from 'date-fns/locale'
import {
  AlertCircle,
  ArrowRight,
  ChevronRight,
  Clock3,
  Inbox,
  Layers,
  Loader2,
  Search,
  Sparkles,
  Target,
} from 'lucide-react'
import { type CSSProperties, useCallback, useDeferredValue, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { List } from 'react-window'
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
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { clusterMembersInfiniteQuery } from '@/features/feedback/api/get-cluster-members'
import { type ClustersFilters, clustersInfiniteQuery } from '@/features/feedback/api/list-clusters'
import { cn } from '@/lib/utils'
import type { ClusterMember, ClusterSummary } from '@/proto/attune/v1/clusters'

const DEFAULT_FILTERS: ClustersFilters = {
  recencyDays: 30,
  minCount: 2,
  sort: 'latest_at',
}

const ROW_HEIGHT = 108

export function ClustersPage() {
  const { i18n } = useTranslation()
  const dateLocale = getDateLocale(i18n.language)

  const [filters, setFilters] = useState<ClustersFilters>(DEFAULT_FILTERS)
  const [qInput, setQInput] = useState('')
  const [selectedCluster, setSelectedCluster] = useState<ClusterSummary | null>(null)
  const qDeferred = useDeferredValue(qInput)

  const queryFilters = useMemo(
    () => ({ ...filters, q: qDeferred.trim() || undefined }),
    [filters, qDeferred],
  )

  const list = useInfiniteQuery(clustersInfiniteQuery(queryFilters))

  const items = useMemo(
    () => list.data?.pages.flatMap((page) => page.items) ?? [],
    [list.data?.pages],
  )
  const totalCount = list.data?.pages[0]?.totalCount ?? 0
  const clusteringEnabled = list.data?.pages[0]?.clusteringEnabled ?? false
  const visibleFeedbackCount = useMemo(
    () => items.reduce((sum, item) => sum + item.count, 0),
    [items],
  )
  const leadCluster = useMemo(
    () =>
      items.reduce<ClusterSummary | null>(
        (best, item) => (!best || item.count > best.count ? item : best),
        null,
      ),
    [items],
  )
  const latestClusterAt = useMemo(
    () =>
      items.reduce<string | null>((latest, item) => {
        if (!latest) return item.latestAt
        return new Date(item.latestAt).getTime() > new Date(latest).getTime()
          ? item.latestAt
          : latest
      }, null),
    [items],
  )
  const currentSort = filters.sort ?? 'latest_at'
  const currentWindow = filters.recencyDays ?? 30
  const currentThreshold = filters.minCount ?? 2
  const activeWorkbenchCount =
    (qInput.trim() ? 1 : 0) +
    (currentWindow !== (DEFAULT_FILTERS.recencyDays ?? 30) ? 1 : 0) +
    (currentThreshold !== (DEFAULT_FILTERS.minCount ?? 2) ? 1 : 0) +
    (currentSort !== (DEFAULT_FILTERS.sort ?? 'latest_at') ? 1 : 0)

  const handleSelect = useCallback((cluster: ClusterSummary) => setSelectedCluster(cluster), [])
  const handleClose = useCallback(() => setSelectedCluster(null), [])
  const handleLoadMore = useCallback(() => void list.fetchNextPage(), [list])
  const handleRetry = useCallback(() => void list.refetch(), [list])

  if (list.isPending) {
    return <ClustersLoadingState />
  }

  return (
    <div className="space-y-5">
      <ClustersHero
        totalCount={totalCount}
        visibleFeedbackCount={visibleFeedbackCount}
        recencyDays={currentWindow}
        minCount={currentThreshold}
        sort={currentSort}
        clusteringEnabled={clusteringEnabled}
        selectedCluster={selectedCluster}
        latestClusterAt={latestClusterAt}
        q={qInput.trim()}
        dateLocale={dateLocale}
      />

      {list.isError ? (
        <ClustersErrorState message={list.error?.message} onRetry={handleRetry} />
      ) : !clusteringEnabled ? (
        <ClustersDisabledState recencyDays={currentWindow} minCount={currentThreshold} />
      ) : (
        <div className="grid gap-5 2xl:grid-cols-[minmax(0,1fr)_21rem]">
          <div className="space-y-5">
            <ClustersWorkbench
              filters={filters}
              q={qInput}
              activeCount={activeWorkbenchCount}
              onFiltersChange={setFilters}
              onQ={setQInput}
            />

            <ClustersCollectionCard
              items={items}
              totalCount={totalCount}
              leadCluster={leadCluster}
              onSelect={handleSelect}
              hasNextPage={list.hasNextPage}
              isFetchingNextPage={list.isFetchingNextPage}
              onLoadMore={handleLoadMore}
              dateLocale={dateLocale}
            />
          </div>

          <aside className="space-y-5">
            <ClustersSpotlightCard
              cluster={selectedCluster ?? leadCluster}
              isSelected={selectedCluster != null}
              onSelect={handleSelect}
              dateLocale={dateLocale}
            />
            <ClustersPlaybookCard />
          </aside>
        </div>
      )}

      <ClusterMembersSheet
        cluster={selectedCluster}
        onClose={handleClose}
        dateLocale={dateLocale}
      />
    </div>
  )
}

function ClustersHero({
  totalCount,
  visibleFeedbackCount,
  recencyDays,
  minCount,
  sort,
  clusteringEnabled,
  selectedCluster,
  latestClusterAt,
  q,
  dateLocale,
}: {
  totalCount: number
  visibleFeedbackCount: number
  recencyDays: number
  minCount: number
  sort: 'latest_at' | 'count'
  clusteringEnabled: boolean
  selectedCluster: ClusterSummary | null
  latestClusterAt: string | null
  q: string
  dateLocale: Locale
}) {
  const { t } = useTranslation()

  return (
    <section className="rounded-[1.45rem] border border-border/70 bg-[linear-gradient(180deg,rgba(255,252,248,0.9),rgba(255,255,255,0.995)_24%,rgba(249,250,251,0.99))] p-2 shadow-[0_24px_70px_-58px_rgba(15,23,42,0.2)]">
      <div className="relative overflow-hidden rounded-[1.28rem] border border-border/55 bg-[radial-gradient(circle_at_top_left,rgba(255,247,237,0.96),rgba(255,255,255,0.985)_38%),linear-gradient(135deg,rgba(255,255,255,0.985),rgba(248,250,252,0.98))] px-5 py-5 sm:px-6 sm:py-5.5">
        <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />
        <div className="pointer-events-none absolute top-0 right-0 h-40 w-40 rounded-full bg-primary/[0.06] blur-3xl" />

        <div className="flex flex-col gap-4 xl:flex-row xl:items-end xl:justify-between">
          <div className="min-w-0 max-w-3xl">
            <div className="text-[11px] font-semibold tracking-[0.18em] text-muted-foreground uppercase">
              {t('feedback.eyebrow')}
            </div>
            <h1 className="mt-2 text-[2rem] font-semibold tracking-tight text-foreground text-balance sm:text-[2.25rem]">
              {t('nav.clusters')}
            </h1>
            <p className="mt-2.5 max-w-2xl text-[13.5px] leading-[1.6rem] text-muted-foreground text-pretty">
              {t('feedback.clusters.page_subtitle')}
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-2 lg:justify-end">
            <WorkbenchFact
              label={t('feedback.focus_items.search')}
              value={q || t('feedback.focus_items.search_idle')}
            />
            <WorkbenchFact
              label={t('feedback.clusters.latest')}
              value={
                latestClusterAt
                  ? formatDistanceToNow(new Date(latestClusterAt), {
                      addSuffix: true,
                      locale: dateLocale,
                    })
                  : '—'
              }
            />
            <WorkbenchFact
              label={t('feedback.clusters.current_focus')}
              value={
                selectedCluster
                  ? clusterDisplayTitle(selectedCluster)
                  : t('feedback.clusters.current_focus_idle')
              }
            />
          </div>
        </div>

        <div className="mt-4 grid gap-2 rounded-[1.15rem] border border-border/60 bg-background/82 p-2.5 shadow-[inset_0_1px_0_rgba(255,255,255,0.65)] sm:grid-cols-2 xl:grid-cols-5">
          <ClusterHeroMetric
            label={t('feedback.clusters.summary.total')}
            value={String(totalCount)}
            hint={t('feedback.clusters.summary.total_hint')}
          />
          <ClusterHeroMetric
            label={t('feedback.clusters.summary.coverage')}
            value={String(visibleFeedbackCount)}
            hint={t('feedback.clusters.summary.coverage_hint')}
            tone={visibleFeedbackCount > 0 ? 'active' : 'default'}
          />
          <ClusterHeroMetric
            label={t('feedback.clusters.summary.window')}
            value={String(recencyDays)}
            hint={t('feedback.clusters.summary.window_hint')}
          />
          <ClusterHeroMetric
            label={t('feedback.clusters.summary.threshold')}
            value={String(minCount)}
            hint={t('feedback.clusters.summary.threshold_hint')}
          />
          {clusteringEnabled ? (
            <ClusterHeroMetric
              label={t('feedback.clusters.summary.sort')}
              value={sortLabel(sort, t)}
              hint={t('feedback.clusters.summary.sort_hint')}
            />
          ) : (
            <ClusterHeroMetric
              label={t('feedback.clusters.summary.status')}
              value={t('feedback.clusters.summary.status_disabled')}
              hint={t('feedback.clusters.summary.status_hint')}
              tone="muted"
            />
          )}
        </div>
      </div>
    </section>
  )
}

function ClustersWorkbench({
  filters,
  q,
  activeCount,
  onFiltersChange,
  onQ,
}: {
  filters: ClustersFilters
  q: string
  activeCount: number
  onFiltersChange: (filters: ClustersFilters) => void
  onQ: (value: string) => void
}) {
  const { t } = useTranslation()
  const currentSort = filters.sort ?? 'latest_at'
  const currentWindow = filters.recencyDays ?? 30
  const currentThreshold = filters.minCount ?? 2

  return (
    <Card className="overflow-hidden rounded-[1.32rem] border-border/65 py-0 shadow-none">
      <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(255,250,246,0.92),rgba(255,255,255,0.98))] px-5 py-4 sm:px-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
          <div className="min-w-0">
            <CardTitle className="text-base">{t('feedback.filter_title')}</CardTitle>
            <CardDescription className="mt-1 max-w-2xl text-[13px] leading-6">
              {t('feedback.clusters.surface_description')}
            </CardDescription>
          </div>
          <div className="inline-flex items-center rounded-full border border-border/60 bg-background/88 px-3 py-1.5 text-xs font-medium text-muted-foreground">
            {activeCount > 0
              ? t('feedback.workbench_filters_active', { count: activeCount })
              : t('feedback.workbench_filters_idle')}
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 px-5 py-4 sm:px-6">
        <div className="grid gap-3 lg:grid-cols-[minmax(0,1.45fr)_repeat(3,minmax(9rem,0.58fr))]">
          <WorkbenchField label={t('feedback.focus_items.search')}>
            <div className="relative min-w-0">
              <Search className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                type="search"
                placeholder={t('feedback.clusters.filter.search_placeholder')}
                value={q}
                onChange={(event) => onQ(event.target.value)}
                className="h-10 pl-9"
                aria-label={t('feedback.clusters.filter.search_placeholder')}
              />
            </div>
          </WorkbenchField>

          <WorkbenchField label={t('feedback.clusters.summary.sort')}>
            <Select
              value={currentSort}
              onValueChange={(value) =>
                onFiltersChange({ ...filters, sort: value as 'latest_at' | 'count' })
              }
            >
              <SelectTrigger className="h-10 w-full bg-background">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="latest_at">{t('feedback.clusters.sort.latest_at')}</SelectItem>
                <SelectItem value="count">{t('feedback.clusters.sort.count')}</SelectItem>
              </SelectContent>
            </Select>
          </WorkbenchField>

          <WorkbenchField label={t('feedback.clusters.summary.window')}>
            <Select
              value={String(currentWindow)}
              onValueChange={(value) => onFiltersChange({ ...filters, recencyDays: Number(value) })}
            >
              <SelectTrigger className="h-10 w-full bg-background">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="7">
                  {t('feedback.clusters.filter.recency', { days: 7 })}
                </SelectItem>
                <SelectItem value="30">
                  {t('feedback.clusters.filter.recency', { days: 30 })}
                </SelectItem>
                <SelectItem value="90">
                  {t('feedback.clusters.filter.recency', { days: 90 })}
                </SelectItem>
              </SelectContent>
            </Select>
          </WorkbenchField>

          <WorkbenchField label={t('feedback.clusters.summary.threshold')}>
            <Select
              value={String(currentThreshold)}
              onValueChange={(value) => onFiltersChange({ ...filters, minCount: Number(value) })}
            >
              <SelectTrigger className="h-10 w-full bg-background">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="2">
                  {t('feedback.clusters.filter.min_count', { count: 2 })}
                </SelectItem>
                <SelectItem value="3">
                  {t('feedback.clusters.filter.min_count', { count: 3 })}
                </SelectItem>
                <SelectItem value="5">
                  {t('feedback.clusters.filter.min_count', { count: 5 })}
                </SelectItem>
              </SelectContent>
            </Select>
          </WorkbenchField>
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <SurfacePill
            label={t('feedback.focus_items.search')}
            value={q.trim() || t('feedback.focus_items.search_idle')}
            tone={q.trim() ? 'active' : 'default'}
          />
          <SurfacePill
            label={t('feedback.clusters.summary.window')}
            value={String(currentWindow)}
          />
          <SurfacePill
            label={t('feedback.clusters.summary.threshold')}
            value={String(currentThreshold)}
          />
          <SurfacePill
            label={t('feedback.clusters.summary.sort')}
            value={sortLabel(currentSort, t)}
            tone={currentSort === 'count' ? 'active' : 'default'}
          />
        </div>
      </CardContent>
    </Card>
  )
}

function ClustersCollectionCard({
  items,
  totalCount,
  leadCluster,
  onSelect,
  hasNextPage,
  isFetchingNextPage,
  onLoadMore,
  dateLocale,
}: ClusterRowProps & { totalCount: number; leadCluster: ClusterSummary | null }) {
  const { t } = useTranslation()

  return (
    <Card className="overflow-hidden rounded-[1.32rem] border-border/65 py-0 shadow-none">
      <CardHeader className="border-b border-border/55 bg-background px-5 py-4 sm:px-6">
        <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
          <div className="min-w-0">
            <CardTitle className="text-base">{t('feedback.clusters.title')}</CardTitle>
            <CardDescription className="mt-1 max-w-2xl text-[13px] leading-6">
              {t('feedback.clusters.subtitle')}
            </CardDescription>
          </div>
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <SurfacePill label={t('feedback.clusters.summary.total')} value={String(totalCount)} />
            {leadCluster ? (
              <SurfacePill
                label={t('feedback.clusters.lead_cluster')}
                value={clusterDisplayTitle(leadCluster)}
                tone="active"
              />
            ) : null}
          </div>
        </div>
      </CardHeader>

      <CardContent className="p-0">
        {leadCluster ? (
          <div className="border-b border-border/50 bg-[linear-gradient(180deg,rgba(255,248,240,0.55),rgba(255,255,255,0.95))] px-5 py-4 sm:px-6">
            <button
              type="button"
              onClick={() => onSelect(leadCluster)}
              className="group flex w-full items-start gap-4 rounded-[1rem] border border-border/55 bg-background/82 px-4 py-4 text-left transition-all hover:-translate-y-0.5 hover:border-primary/25 hover:shadow-[0_16px_36px_-32px_rgba(234,88,12,0.45)]"
            >
              <div className="flex h-11 w-11 shrink-0 items-center justify-center rounded-[0.95rem] bg-primary/10 text-primary">
                <Sparkles className="h-5 w-5" />
              </div>
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="text-sm font-semibold text-foreground">
                    {t('feedback.clusters.lead_cluster')}
                  </span>
                  <span className="rounded-full border border-border/60 bg-muted/30 px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
                    {t('feedback.clusters.count', { count: leadCluster.count })}
                  </span>
                </div>
                <div className="mt-1.5 text-base font-semibold tracking-tight text-foreground">
                  {clusterDisplayTitle(leadCluster)}
                </div>
                {leadCluster.sampleTitle &&
                leadCluster.sampleTitle !== clusterDisplayTitle(leadCluster) ? (
                  <p className="mt-1 line-clamp-2 max-w-3xl text-[13px] leading-[1.45rem] text-muted-foreground">
                    {leadCluster.sampleTitle}
                  </p>
                ) : null}
              </div>
              <div className="hidden shrink-0 text-right lg:block">
                <div className="text-[11px] font-medium tracking-[0.16em] text-muted-foreground uppercase">
                  {t('feedback.clusters.latest')}
                </div>
                <div className="mt-1 text-xs text-muted-foreground">
                  {formatDistanceToNow(new Date(leadCluster.latestAt), {
                    addSuffix: true,
                    locale: dateLocale,
                  })}
                </div>
              </div>
              <ChevronRight className="mt-1 h-4 w-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" />
            </button>
          </div>
        ) : null}

        {items.length > 0 ? (
          <ClusterList
            items={items}
            onSelect={onSelect}
            hasNextPage={hasNextPage}
            isFetchingNextPage={isFetchingNextPage}
            onLoadMore={onLoadMore}
            dateLocale={dateLocale}
          />
        ) : (
          <EmptyState
            icon={Layers}
            title={t('feedback.clusters.empty_title')}
            description={t('feedback.clusters.empty_body')}
            className="px-6 py-14"
          />
        )}
      </CardContent>
    </Card>
  )
}

function ClustersSpotlightCard({
  cluster,
  isSelected,
  onSelect,
  dateLocale,
}: {
  cluster: ClusterSummary | null
  isSelected: boolean
  onSelect: (cluster: ClusterSummary) => void
  dateLocale: Locale
}) {
  const { t } = useTranslation()

  return (
    <Card className="overflow-hidden rounded-[1.32rem] border-border/65 py-0 shadow-none">
      <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(255,250,246,0.92),rgba(255,255,255,0.98))] px-5 py-4">
        <CardTitle className="text-base">{t('feedback.focus_title')}</CardTitle>
        <CardDescription className="mt-1 text-[13px] leading-6">
          {t('feedback.focus_description')}
        </CardDescription>
      </CardHeader>
      <CardContent className="px-5 py-4">
        {cluster ? (
          <div className="space-y-4">
            <div className="rounded-[1rem] border border-border/60 bg-background/82 p-4">
              <div className="flex items-start gap-3">
                <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-[0.95rem] bg-primary/10 text-primary">
                  <Layers className="h-4.5 w-4.5" />
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-xs font-medium text-muted-foreground">
                      {isSelected
                        ? t('feedback.clusters.current_focus')
                        : t('feedback.clusters.lead_cluster')}
                    </span>
                    <span className="rounded-full border border-border/60 bg-muted/25 px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
                      {t('feedback.clusters.count', { count: cluster.count })}
                    </span>
                  </div>
                  <div className="mt-1.5 text-base font-semibold tracking-tight text-foreground">
                    {clusterDisplayTitle(cluster)}
                  </div>
                  {cluster.sampleTitle && cluster.sampleTitle !== clusterDisplayTitle(cluster) ? (
                    <p className="mt-1 line-clamp-3 text-[13px] leading-[1.45rem] text-muted-foreground">
                      {cluster.sampleTitle}
                    </p>
                  ) : null}
                </div>
              </div>
              <div className="mt-4 grid gap-2 sm:grid-cols-2">
                <MiniStat
                  label={t('feedback.clusters.latest')}
                  value={formatDistanceToNow(new Date(cluster.latestAt), {
                    addSuffix: true,
                    locale: dateLocale,
                  })}
                />
                <MiniStat
                  label={t('feedback.clusters.summary.coverage')}
                  value={String(cluster.count)}
                />
              </div>
            </div>

            {!isSelected ? (
              <Button className="w-full justify-between" onClick={() => onSelect(cluster)}>
                {t('feedback.clusters.open_members')}
                <ArrowRight className="h-4 w-4" />
              </Button>
            ) : (
              <div className="rounded-[0.95rem] border border-primary/15 bg-primary/[0.06] px-3.5 py-3 text-sm leading-6 text-muted-foreground">
                {t('feedback.clusters.sheet_hint')}
              </div>
            )}
          </div>
        ) : (
          <EmptyState
            icon={Target}
            title={t('feedback.clusters.spotlight_empty_title')}
            description={t('feedback.clusters.spotlight_empty_body')}
            className="py-10"
          />
        )}
      </CardContent>
    </Card>
  )
}

function ClustersPlaybookCard() {
  const { t } = useTranslation()

  return (
    <Card className="rounded-[1.32rem] border-border/65 shadow-none">
      <CardHeader className="pb-3">
        <CardTitle className="text-base">{t('feedback.clusters.playbook_title')}</CardTitle>
        <CardDescription className="mt-1 text-[13px] leading-6">
          {t('feedback.clusters.playbook_description')}
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <PlaybookStep
          icon={Clock3}
          title={t('feedback.clusters.playbook.scale_title')}
          body={t('feedback.clusters.playbook.scale_body')}
        />
        <PlaybookStep
          icon={Layers}
          title={t('feedback.clusters.playbook.members_title')}
          body={t('feedback.clusters.playbook.members_body')}
        />
        <PlaybookStep
          icon={Target}
          title={t('feedback.clusters.playbook.action_title')}
          body={t('feedback.clusters.playbook.action_body')}
        />
      </CardContent>
    </Card>
  )
}

function ClustersErrorState({ message, onRetry }: { message?: string; onRetry: () => void }) {
  const { t } = useTranslation()

  return (
    <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_20rem]">
      <Card className="overflow-hidden rounded-[1.32rem] border-border/65 py-0 shadow-none">
        <CardContent className="px-6 py-14">
          <EmptyState
            icon={AlertCircle}
            title={t('common.error')}
            description={message}
            action={{ label: t('common.retry'), onClick: onRetry }}
          />
        </CardContent>
      </Card>
      <Card className="rounded-[1.32rem] border-border/65 shadow-none">
        <CardHeader className="pb-3">
          <CardTitle className="text-base">{t('feedback.clusters.playbook_title')}</CardTitle>
          <CardDescription className="mt-1 text-[13px] leading-6">
            {t('feedback.clusters.error_hint')}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <PlaybookStep
            icon={Clock3}
            title={t('feedback.clusters.error_steps.retry_title')}
            body={t('feedback.clusters.error_steps.retry_body')}
          />
          <PlaybookStep
            icon={Layers}
            title={t('feedback.clusters.error_steps.scope_title')}
            body={t('feedback.clusters.error_steps.scope_body')}
          />
        </CardContent>
      </Card>
    </div>
  )
}

function ClustersDisabledState({
  recencyDays,
  minCount,
}: {
  recencyDays: number
  minCount: number
}) {
  const { t } = useTranslation()

  return (
    <div className="grid gap-5 xl:grid-cols-[minmax(0,1fr)_20rem]">
      <Card className="overflow-hidden rounded-[1.32rem] border-border/65 py-0 shadow-none">
        <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(255,250,246,0.92),rgba(255,255,255,0.98))] px-5 py-4 sm:px-6">
          <CardTitle className="text-base">{t('feedback.clusters.disabled_title')}</CardTitle>
          <CardDescription className="mt-1 max-w-2xl text-[13px] leading-6">
            {t('feedback.clusters.disabled_body')}
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-5 px-6 py-6 xl:grid-cols-[minmax(0,1fr)_15rem]">
          <div className="space-y-4">
            <div className="rounded-[1rem] border border-border/60 bg-background/84 px-5 py-5">
              <div className="flex flex-col items-start gap-4">
                <div className="rounded-[1rem] border border-primary/10 bg-[linear-gradient(180deg,rgba(255,247,237,0.95),rgba(255,255,255,0.92))] p-3.5 text-primary shadow-[0_18px_36px_-28px_rgba(234,88,12,0.45)]">
                  <Layers className="h-6 w-6" />
                </div>
                <div>
                  <div className="text-base font-semibold tracking-tight text-foreground">
                    {t('feedback.clusters.disabled_surface_title')}
                  </div>
                  <p className="mt-1.5 max-w-2xl text-sm leading-6 text-muted-foreground">
                    {t('feedback.clusters.disabled_surface_body')}
                  </p>
                </div>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <Button asChild className="h-9" data-testid="clusters-enable-link">
                <a href="/integrations/digests">{t('feedback.clusters.enable_action')}</a>
              </Button>
              <p className="text-sm leading-6 text-muted-foreground">
                {t('feedback.clusters.enable_action_hint')}
              </p>
            </div>
          </div>

          <div className="grid gap-3 content-start sm:grid-cols-3 xl:grid-cols-1">
            <MiniStat label={t('feedback.clusters.summary.window')} value={String(recencyDays)} />
            <MiniStat label={t('feedback.clusters.summary.threshold')} value={String(minCount)} />
            <MiniStat
              label={t('feedback.clusters.summary.status')}
              value={t('feedback.clusters.summary.status_disabled')}
            />
          </div>
        </CardContent>
      </Card>

      <Card className="rounded-[1.32rem] border-border/65 shadow-none">
        <CardHeader className="pb-3">
          <CardTitle className="text-base">
            {t('feedback.clusters.disabled_playbook_title')}
          </CardTitle>
          <CardDescription className="mt-1 text-[13px] leading-6">
            {t('feedback.clusters.disabled_playbook_description')}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3">
          <PlaybookStep
            icon={Sparkles}
            title={t('feedback.clusters.disabled_steps.signal_title')}
            body={t('feedback.clusters.disabled_steps.signal_body')}
          />
          <PlaybookStep
            icon={Clock3}
            title={t('feedback.clusters.disabled_steps.window_title')}
            body={t('feedback.clusters.disabled_steps.window_body')}
          />
          <PlaybookStep
            icon={Target}
            title={t('feedback.clusters.disabled_steps.action_title')}
            body={t('feedback.clusters.disabled_steps.action_body')}
          />
        </CardContent>
      </Card>
    </div>
  )
}

function ClustersLoadingState() {
  return (
    <div className="space-y-5">
      <section className="rounded-[1.45rem] border border-border/70 bg-[linear-gradient(180deg,rgba(255,252,248,0.9),rgba(255,255,255,0.995)_24%,rgba(249,250,251,0.99))] p-2 shadow-[0_24px_70px_-58px_rgba(15,23,42,0.2)]">
        <div className="rounded-[1.28rem] border border-border/55 bg-background/95 px-5 py-5 sm:px-6">
          <Skeleton className="h-3 w-24" />
          <Skeleton className="mt-4 h-10 w-52" />
          <Skeleton className="mt-3 h-5 w-full max-w-2xl" />
          <div className="mt-5 grid gap-2 sm:grid-cols-2 xl:grid-cols-5">
            {['hero-1', 'hero-2', 'hero-3', 'hero-4', 'hero-5'].map((key) => (
              <Skeleton key={key} className="h-28 rounded-[1.1rem]" />
            ))}
          </div>
        </div>
      </section>

      <div className="grid gap-5 2xl:grid-cols-[minmax(0,1fr)_21rem]">
        <div className="space-y-5">
          <Card className="overflow-hidden rounded-[1.32rem] border-border/65 py-0 shadow-none">
            <CardContent className="space-y-4 px-6 py-6">
              <Skeleton className="h-5 w-28" />
              <div className="grid gap-3 lg:grid-cols-[minmax(0,1.45fr)_repeat(3,minmax(9rem,0.58fr))]">
                <Skeleton className="h-10" />
                <Skeleton className="h-10" />
                <Skeleton className="h-10" />
                <Skeleton className="h-10" />
              </div>
              <div className="flex flex-wrap gap-2">
                {['pill-1', 'pill-2', 'pill-3', 'pill-4'].map((key) => (
                  <Skeleton key={key} className="h-9 w-28 rounded-full" />
                ))}
              </div>
            </CardContent>
          </Card>

          <Card className="overflow-hidden rounded-[1.32rem] border-border/65 py-0 shadow-none">
            <CardContent className="space-y-3 px-6 py-6">
              <Skeleton className="h-24 rounded-[1rem]" />
              {['row-1', 'row-2', 'row-3', 'row-4', 'row-5'].map((key) => (
                <Skeleton key={key} className="h-24 rounded-[1rem]" />
              ))}
            </CardContent>
          </Card>
        </div>

        <div className="space-y-5">
          <Card className="rounded-[1.32rem] border-border/65 shadow-none">
            <CardContent className="space-y-3 px-5 py-5">
              <Skeleton className="h-5 w-24" />
              <Skeleton className="h-32 rounded-[1rem]" />
              <Skeleton className="h-10" />
            </CardContent>
          </Card>
          <Card className="rounded-[1.32rem] border-border/65 shadow-none">
            <CardContent className="space-y-3 px-5 py-5">
              <Skeleton className="h-5 w-24" />
              <Skeleton className="h-16 rounded-[1rem]" />
              <Skeleton className="h-16 rounded-[1rem]" />
              <Skeleton className="h-16 rounded-[1rem]" />
            </CardContent>
          </Card>
        </div>
      </div>
    </div>
  )
}

function WorkbenchField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <div className="text-[11px] font-medium tracking-[0.16em] text-muted-foreground uppercase">
        {label}
      </div>
      {children}
    </div>
  )
}

function WorkbenchFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="inline-flex max-w-full items-center gap-2 rounded-full border border-border/60 bg-background/85 px-3 py-1.5 text-sm">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="truncate font-medium text-foreground">{value}</span>
    </div>
  )
}

function SurfacePill({
  label,
  value,
  tone = 'default',
}: {
  label: string
  value: string
  tone?: 'default' | 'active'
}) {
  return (
    <div
      className={cn(
        'inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm',
        tone === 'active'
          ? 'border-primary/20 bg-primary/[0.06] text-foreground'
          : 'border-border/60 bg-background text-foreground',
      )}
    >
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium">{value}</span>
    </div>
  )
}

function ClusterHeroMetric({
  label,
  value,
  hint,
  tone = 'default',
}: {
  label: string
  value: string
  hint: string
  tone?: 'default' | 'active' | 'muted'
}) {
  return (
    <div
      className={cn(
        'rounded-[1.05rem] border px-4 py-4',
        tone === 'active'
          ? 'border-primary/20 bg-primary/[0.06]'
          : tone === 'muted'
            ? 'border-border/60 bg-muted/20'
            : 'border-border/60 bg-background/88',
      )}
    >
      <div className="text-[10px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {label}
      </div>
      <div className="mt-1.5 text-3xl font-semibold tracking-tight text-foreground">{value}</div>
      <div className="mt-1.5 text-sm leading-5 text-muted-foreground">{hint}</div>
    </div>
  )
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-[0.95rem] border border-border/60 bg-background/86 px-3.5 py-3">
      <div className="text-[10px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {label}
      </div>
      <div className="mt-1.5 text-sm font-semibold text-foreground">{value}</div>
    </div>
  )
}

function PlaybookStep({
  icon: Icon,
  title,
  body,
}: {
  icon: typeof Sparkles
  title: string
  body: string
}) {
  return (
    <div className="rounded-[1rem] border border-border/60 bg-background/82 px-4 py-3.5">
      <div className="flex items-start gap-3">
        <div className="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-[0.85rem] bg-primary/10 text-primary">
          <Icon className="h-4.5 w-4.5" />
        </div>
        <div>
          <div className="text-sm font-semibold text-foreground">{title}</div>
          <p className="mt-1 text-[13px] leading-6 text-muted-foreground">{body}</p>
        </div>
      </div>
    </div>
  )
}

function getDateLocale(language: string) {
  return language.startsWith('zh') ? zhCN : enUS
}

function sortLabel(value: 'latest_at' | 'count', t: (key: string) => string) {
  return value === 'count'
    ? t('feedback.clusters.sort.count')
    : t('feedback.clusters.sort.latest_at')
}

function clusterDisplayTitle(cluster: ClusterSummary) {
  return cluster.label || cluster.sampleTitle
}

interface ClusterRowProps {
  items: ClusterSummary[]
  onSelect: (cluster: ClusterSummary) => void
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
  style: CSSProperties
  ariaAttributes: { 'aria-posinset': number; 'aria-setsize': number; role: 'listitem' }
} & ClusterRowProps) {
  const { t } = useTranslation()

  if (index >= items.length) {
    return (
      <div
        style={style}
        className="flex items-center justify-center border-t border-border/50 bg-background"
      >
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
  const relativeTime = formatDistanceToNow(new Date(cluster.latestAt), {
    addSuffix: true,
    locale: dateLocale,
  })

  return (
    <div style={style} {...ariaAttributes}>
      <button
        type="button"
        onClick={() => onSelect(cluster)}
        aria-label={`${clusterDisplayTitle(cluster)}, ${cluster.count} ${t('feedback.clusters.count', { count: cluster.count })}`}
        className={cn(
          'group flex h-full w-full items-center gap-4 bg-background px-5 text-left transition-colors hover:bg-muted/25 sm:px-6',
          index > 0 && 'border-t border-border/50',
        )}
      >
        <div className="hidden h-10 w-10 shrink-0 items-center justify-center rounded-[0.95rem] border border-border/55 bg-muted/15 text-xs font-semibold text-muted-foreground sm:flex">
          {String(index + 1).padStart(2, '0')}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate text-sm font-semibold text-foreground">
              {clusterDisplayTitle(cluster)}
            </span>
            <span className="rounded-full border border-border/60 bg-muted/20 px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
              {t('feedback.clusters.count', { count: cluster.count })}
            </span>
            <span className="rounded-full border border-border/60 bg-background px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
              {relativeTime}
            </span>
          </div>
          <p className="mt-1.5 line-clamp-2 max-w-3xl text-[13px] leading-[1.45rem] text-muted-foreground">
            {cluster.sampleTitle}
          </p>
        </div>
        <div className="hidden shrink-0 lg:block">
          <div className="text-[10px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
            {t('feedback.clusters.latest')}
          </div>
          <div className="mt-1 text-xs text-muted-foreground">{relativeTime}</div>
        </div>
        <ChevronRight className="h-4 w-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-foreground" />
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
  const listHeight = useMemo(() => Math.min(itemCount * ROW_HEIGHT, 640), [itemCount])

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
    () => members.data?.pages.flatMap((page) => page.items) ?? [],
    [members.data?.pages],
  )
  const totalCount = members.data?.pages[0]?.totalCount ?? 0
  const loadedCount = items.length
  const averageSimilarity = items.length
    ? Math.round((items.reduce((sum, item) => sum + item.similarity, 0) / items.length) * 100)
    : 0
  const resolvedLabel =
    members.data?.pages[0]?.clusterLabel || (cluster ? clusterDisplayTitle(cluster) : '')

  const handleLoadMore = useCallback(() => void members.fetchNextPage(), [members])

  return (
    <Sheet open={cluster != null} onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="flex w-full flex-col gap-0 overflow-hidden bg-[linear-gradient(180deg,rgba(255,250,246,0.55),rgba(255,255,255,0.98)_18%,rgba(248,250,252,0.99))] p-0 sm:max-w-xl">
        <SheetHeader className="shrink-0 border-b border-border/55 bg-background/94 px-6 pt-6 pb-5 pr-12 backdrop-blur">
          <SheetTitle className="text-left text-lg font-semibold tracking-tight">
            {resolvedLabel || t('feedback.clusters.members_title')}
          </SheetTitle>
          <SheetDescription className="mt-1 text-left text-[13px] leading-6">
            {cluster?.sampleTitle && cluster.sampleTitle !== resolvedLabel
              ? cluster.sampleTitle
              : t('feedback.clusters.members_description')}
          </SheetDescription>

          <div className="mt-4 grid gap-2 sm:grid-cols-2">
            <MiniStat label={t('feedback.clusters.members_total')} value={String(totalCount)} />
            <MiniStat
              label={t('feedback.clusters.similarity')}
              value={items.length > 0 ? `${averageSimilarity}%` : '—'}
            />
          </div>

          {totalCount > 0 ? (
            <div className="mt-4 rounded-[0.95rem] border border-border/60 bg-muted/10 px-3.5 py-3">
              <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
                <span>
                  {t('feedback.clusters.loaded_of_total', {
                    loaded: loadedCount,
                    total: totalCount,
                  })}
                </span>
                <span className="tabular-nums">
                  {Math.round((loadedCount / totalCount) * 100)}%
                </span>
              </div>
              <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-muted">
                <div
                  className="h-full rounded-full bg-primary/70 transition-all duration-300"
                  style={{ width: `${Math.min((loadedCount / totalCount) * 100, 100)}%` }}
                />
              </div>
            </div>
          ) : null}
        </SheetHeader>

        <div className="flex-1 overflow-y-auto px-6 py-5">
          {members.isError ? (
            <EmptyState
              icon={AlertCircle}
              title={t('common.error')}
              description={members.error?.message}
              className="py-12"
            />
          ) : members.isPending ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : items.length === 0 ? (
            <EmptyState
              icon={Inbox}
              title={t('feedback.clusters.members_empty_title')}
              description={t('feedback.clusters.members_empty_body')}
              className="py-12"
            />
          ) : (
            <div className="space-y-3">
              {items.map((item) => (
                <ClusterMemberCard key={item.id} item={item} dateLocale={dateLocale} />
              ))}

              {members.hasNextPage ? (
                <div className="pt-2 text-center">
                  <Button
                    variant="outline"
                    onClick={handleLoadMore}
                    disabled={members.isFetchingNextPage}
                  >
                    {members.isFetchingNextPage ? (
                      <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
                    ) : (
                      t('feedback.clusters.load_more')
                    )}
                  </Button>
                </div>
              ) : null}
            </div>
          )}
        </div>
      </SheetContent>
    </Sheet>
  )
}

function ClusterMemberCard({ item, dateLocale }: { item: ClusterMember; dateLocale: Locale }) {
  const { t } = useTranslation()

  return (
    <article className="rounded-[1rem] border border-border/60 bg-background/88 px-4 py-4 shadow-[0_18px_30px_-32px_rgba(15,23,42,0.2)]">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="text-sm font-semibold text-foreground">
            {item.enrichedTitle || `#${item.id}`}
          </div>
          <p className="mt-1.5 line-clamp-4 text-[13px] leading-[1.45rem] text-muted-foreground">
            {item.content}
          </p>
        </div>
        <div className="rounded-full border border-border/60 bg-muted/20 px-2.5 py-1 text-[11px] font-medium text-muted-foreground">
          {t('feedback.clusters.similarity')} {Math.round(item.similarity * 100)}%
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
        <span className="rounded-full border border-border/60 bg-background px-2.5 py-1">
          {item.source}
        </span>
        <span className="rounded-full border border-border/60 bg-background px-2.5 py-1">
          {formatDistanceToNow(new Date(item.createdAt), {
            addSuffix: true,
            locale: dateLocale,
          })}
        </span>
      </div>
    </article>
  )
}
