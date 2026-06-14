import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { createFileRoute } from '@tanstack/react-router'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import { Inbox, Loader2 } from 'lucide-react'
import { useDeferredValue, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DimensionChips, UrgentDot } from '@/components/dim/dimension-chips'
import { EmptyState } from '@/components/empty-state'
import { Loading } from '@/components/loading'
import { TagBadgeTooltip } from '@/components/tag/tag-badge-tooltip'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { WorkflowStateBadge } from '@/components/workflow/workflow-state-badge'
import { useBatchUpdateTags } from '@/features/feedback/api/batch-update-tags'
import { feedbackStatsQuery } from '@/features/feedback/api/get-feedback-stats'
import {
  type AttrFilterEntry,
  type Feedback,
  type FeedbackListFilters,
  feedbackListInfiniteQuery,
} from '@/features/feedback/api/list-feedback-infinite'
import { ClustersCard } from '@/features/feedback/components/clusters-card'
import { ConfidenceIndicator } from '@/features/feedback/components/confidence-indicator'
import { FeedbackDetailSheet } from '@/features/feedback/components/detail-sheet'
import { DimStatsBars } from '@/features/feedback/components/dim-stats-bars'
import { LanguageBadge } from '@/features/feedback/components/language-badge'
import { SelectionActionBar } from '@/features/feedback/components/selection-action-bar'
import { useRowSelection } from '@/features/feedback/hooks/use-row-selection'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { tagsQuery } from '@/features/tags/api/list-tags'
import { workflowStatesQuery } from '@/features/workflow/api/list-states'
import { useBatchTransitionFeedback } from '@/features/workflow/api/transition-feedback'
import { useDisplayName } from '@/lib/i18n-resolve'
import type { Dimension } from '@/proto/attune/v1/common'
import type { Tag } from '@/proto/attune/v1/tag'

export const Route = createFileRoute('/_authed/feedback')({
  component: FeedbackPage,
})

function FeedbackPage() {
  const { t } = useTranslation()
  const config = useQuery(enrichConfigQuery())
  const dims = config.data?.dimensions ?? []

  const [attrFilters, setAttrFilters] = useState<Record<string, string>>({})
  const [tagFilter, setTagFilter] = useState<string>('')
  const [workflowFilter, setWorkflowFilter] = useState<string>('')
  const [qInput, setQInput] = useState('')
  const qDeferred = useDeferredValue(qInput)
  const [detailId, setDetailId] = useState<string | null>(null)

  const filters: FeedbackListFilters = useMemo(() => {
    const attrs: AttrFilterEntry[] = Object.entries(attrFilters)
      .filter(([, v]) => v && v !== '__all')
      .map(([dim, value]) => ({ dim, value }))
    return {
      attrs,
      q: qDeferred.trim(),
      tag: tagFilter || undefined,
      workflowState: workflowFilter || undefined,
    }
  }, [attrFilters, qDeferred, tagFilter, workflowFilter])

  const list = useInfiniteQuery(feedbackListInfiniteQuery(filters))
  const items = list.data?.pages.flatMap((p) => p.items) ?? []
  const total = items.length
  const stats = useQuery(feedbackStatsQuery())
  const allTags = useQuery(tagsQuery())
  const tagList = allTags.data ?? []
  const allStates = useQuery(workflowStatesQuery())
  const stateList = allStates.data ?? []

  const itemIds = useMemo(() => items.map((i) => i.id), [items])
  const { selected, toggle, toggleAll, clear, isAllSelected } = useRowSelection(itemIds)

  const batchUpdate = useBatchUpdateTags()
  const batchTransition = useBatchTransitionFeedback()

  const removableTags = useMemo(() => {
    const selectedItems = items.filter((i) => selected.has(i.id))
    const tagMap = new Map<string, Tag>()
    for (const item of selectedItems) {
      for (const tag of item.tags) {
        if (!tagMap.has(tag.id)) tagMap.set(tag.id, tag)
      }
    }
    return Array.from(tagMap.values())
  }, [items, selected])

  const handleBatchAdd = (tagId: string) => {
    batchUpdate.mutate(
      { feedbackIds: Array.from(selected), addTagIds: [tagId], removeTagIds: [] },
      {
        onSuccess: (res) => {
          toast.success(t('feedback.batch.success', { count: res.affected }))
          clear()
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleBatchRemove = (tagId: string) => {
    batchUpdate.mutate(
      { feedbackIds: Array.from(selected), addTagIds: [], removeTagIds: [tagId] },
      {
        onSuccess: (res) => {
          toast.success(t('feedback.batch.success', { count: res.affected }))
          clear()
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const handleBatchTransition = (toStateId: string) => {
    batchTransition.mutate(
      { feedbackIds: Array.from(selected), toStateId, comment: '' },
      {
        onSuccess: (res) => {
          toast.success(t('feedback.batch.transition_success', { count: res.succeeded }))
          clear()
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  return (
    <div>
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{t('nav.feedback')}</h1>
        <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{t('feedback.subtitle')}</p>
      </div>

      {stats.data && Number(stats.data.total) > 0 && (
        <Card className="mt-6 gap-4 rounded-lg border-border/60 bg-muted/20 py-4 shadow-none">
          <CardHeader className="px-4">
            <CardTitle className="text-base">{t('feedback.stats.title')}</CardTitle>
          </CardHeader>
          <CardContent className="px-4">
            <DimStatsBars
              dims={dims}
              stats={stats.data.dims}
              total={Number(stats.data.total)}
              urgentCount={Number(stats.data.urgentCount)}
            />
          </CardContent>
        </Card>
      )}

      <ClustersCard />

      <FilterBar
        dims={dims}
        attrFilters={attrFilters}
        tagFilter={tagFilter}
        workflowFilter={workflowFilter}
        tags={tagList}
        workflowStates={stateList}
        q={qInput}
        onAttrChange={(dim, value) => setAttrFilters((m) => ({ ...m, [dim]: value }))}
        onTagChange={setTagFilter}
        onWorkflowChange={setWorkflowFilter}
        onQ={setQInput}
      />

      <Card className="mt-4 gap-0 overflow-hidden rounded-lg border-border/60 py-0 shadow-none">
        <CardHeader className="border-b border-border/60 bg-muted/20 px-4 py-3">
          <CardTitle>{t('nav.feedback')}</CardTitle>
          <CardDescription>{total}</CardDescription>
        </CardHeader>
        <CardContent className="px-0">
          {selected.size > 0 && (
            <div className="px-4 pt-3">
              <SelectionActionBar
                count={selected.size}
                availableTags={tagList}
                removableTags={removableTags}
                workflowStates={stateList}
                onBatchAdd={handleBatchAdd}
                onBatchRemove={handleBatchRemove}
                onBatchTransition={handleBatchTransition}
                onCancel={clear}
              />
            </div>
          )}

          {list.isPending ? (
            <Loading />
          ) : items.length > 0 ? (
            <FeedbackTable
              items={items}
              dims={dims}
              allTags={tagList}
              selected={selected}
              isAllSelected={isAllSelected}
              onToggle={toggle}
              onToggleAll={toggleAll}
              onRowClick={setDetailId}
            />
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
        availableTags={allTags.data ?? []}
        onOpenChange={(v) => !v && setDetailId(null)}
      />
    </div>
  )
}

function FilterBar({
  dims,
  attrFilters,
  tagFilter,
  workflowFilter,
  tags,
  workflowStates,
  q,
  onAttrChange,
  onTagChange,
  onWorkflowChange,
  onQ,
}: {
  dims: Dimension[]
  attrFilters: Record<string, string>
  tagFilter: string
  workflowFilter: string
  tags: Tag[]
  workflowStates: import('@/proto/attune/v1/workflow').WorkflowState[]
  q: string
  onAttrChange: (dim: string, value: string) => void
  onTagChange: (tagId: string) => void
  onWorkflowChange: (stateId: string) => void
  onQ: (v: string) => void
}) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  const activeTags = tags.filter((tag) => !tag.archived)
  const activeStates = workflowStates.filter((s) => !s.archived)

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
            <SelectTrigger className="min-w-[10rem] max-w-[14rem] flex-1 sm:flex-none">
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
      {activeTags.length > 0 && (
        <Select
          value={tagFilter || '__all'}
          onValueChange={(v) => onTagChange(v === '__all' ? '' : v)}
        >
          <SelectTrigger className="min-w-[10rem] max-w-[14rem] flex-1 sm:flex-none">
            <SelectValue placeholder={t('feedback.filter.all_tags')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all">{t('feedback.filter.all_tags')}</SelectItem>
            {activeTags.map((tag) => (
              <SelectItem key={tag.id} value={tag.id}>
                <span
                  className="mr-2 inline-block size-2 rounded-full"
                  style={{ backgroundColor: tag.color }}
                />
                {tag.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      {activeStates.length > 0 && (
        <Select
          value={workflowFilter || '__all'}
          onValueChange={(v) => onWorkflowChange(v === '__all' ? '' : v)}
        >
          <SelectTrigger className="min-w-[10rem] max-w-[14rem] flex-1 sm:flex-none">
            <SelectValue placeholder={t('feedback.filter.all_states')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all">{t('feedback.filter.all_states')}</SelectItem>
            {activeStates.map((s) => (
              <SelectItem key={s.id} value={s.id}>
                <span className="flex items-center gap-2">
                  <span
                    className="inline-block size-2 rounded-full"
                    style={{ backgroundColor: s.color }}
                  />
                  {s.name}
                </span>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
      <Input
        type="search"
        placeholder={t('feedback.filter.search_placeholder')}
        value={q}
        onChange={(e) => onQ(e.target.value)}
        className="min-w-[14rem] flex-1 sm:max-w-[280px]"
      />
    </div>
  )
}

function FeedbackTable({
  items,
  dims,
  allTags,
  selected,
  isAllSelected,
  onToggle,
  onToggleAll,
  onRowClick,
}: {
  items: Feedback[]
  dims: Dimension[]
  allTags: Tag[]
  selected: Set<string>
  isAllSelected: boolean
  onToggle: (id: string) => void
  onToggleAll: () => void
  onRowClick: (id: string) => void
}) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()

  const tagLookup = useMemo(() => {
    const map = new Map<string, Tag>()
    for (const tag of allTags) map.set(tag.id, tag)
    return map
  }, [allTags])

  return (
    <div>
      <div className="grid grid-cols-[2rem_minmax(0,1.2fr)_minmax(18rem,1.8fr)_5rem_5rem_7rem_7rem] gap-4 border-b border-border/60 px-4 py-2 text-[11px] font-medium text-muted-foreground max-lg:hidden">
        <div className="flex items-center justify-center">
          <Checkbox
            checked={isAllSelected}
            onCheckedChange={onToggleAll}
            aria-label={t('feedback.table.select_all')}
          />
        </div>
        <div>{t('feedback.table.title')}</div>
        <div>{dims.map((d) => displayOf(d.displayName) || d.name).join(' / ')}</div>
        <div>{t('feedback.table.confidence')}</div>
        <div>{t('feedback.table.language')}</div>
        <div>{t('feedback.table.user')}</div>
        <div>{t('feedback.table.time')}</div>
      </div>
      <div className="divide-y divide-border/60">
        {items.map((f) => {
          const title = f.enrichedDisplayTitle || f.enrichedTitle || `#${f.id}`
          const isSelected = selected.has(f.id)
          return (
            <div
              key={f.id}
              className={`grid w-full gap-4 px-4 py-4 text-left transition-colors hover:bg-muted/25 lg:grid-cols-[2rem_minmax(0,1.2fr)_minmax(18rem,1.8fr)_5rem_5rem_7rem_7rem] ${isSelected ? 'bg-muted/30' : ''}`}
            >
              <div className="flex items-start justify-center pt-0.5">
                <Checkbox
                  checked={isSelected}
                  onCheckedChange={() => onToggle(f.id)}
                  aria-label={t('feedback.table.select_row', { title })}
                />
              </div>
              <button type="button" className="min-w-0 text-left" onClick={() => onRowClick(f.id)}>
                <div className="flex min-w-0 items-center gap-2 font-medium">
                  <UrgentDot urgent={f.isUrgent} />
                  <span className="truncate">{title}</span>
                </div>
                <div className="truncate text-xs text-muted-foreground">{f.content}</div>
                {(f.tags.length > 0 || f.workflowState) && (
                  <div className="mt-1.5 flex flex-wrap gap-1">
                    {f.workflowState && (
                      <WorkflowStateBadge
                        name={f.workflowState.name}
                        color={f.workflowState.color}
                        category={f.workflowState.category}
                      />
                    )}
                    {f.tags.map((tag) => (
                      <TagBadgeTooltip key={tag.id} tag={tagLookup.get(tag.id) ?? tag} />
                    ))}
                  </div>
                )}
              </button>
              <dl className="grid min-w-0 grid-cols-[repeat(auto-fit,minmax(7.5rem,1fr))] gap-x-3 gap-y-2">
                {dims.map((d) => (
                  <div key={d.name} className="min-w-0">
                    <dt className="mb-1 truncate text-[11px] text-muted-foreground lg:hidden">
                      {displayOf(d.displayName) || d.name}
                    </dt>
                    <dd className="min-w-0">
                      <DimensionChips
                        dim={d}
                        value={(f.enrichedAttrs as Record<string, unknown> | undefined)?.[d.name]}
                      />
                    </dd>
                  </div>
                ))}
              </dl>
              <div>
                <ConfidenceIndicator confidence={f.classificationConfidence} />
              </div>
              <div>
                <LanguageBadge language={f.language} />
              </div>
              <div className="truncate font-mono text-xs text-muted-foreground">
                {f.userId || '—'}
              </div>
              <div className="text-xs text-muted-foreground">
                {formatDistanceToNow(new Date(f.createdAt), { addSuffix: true, locale: zhCN })}
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}
