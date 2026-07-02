import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import {
  AlertCircle,
  ArrowRight,
  ChevronDown,
  Clock3,
  Inbox,
  Loader2,
  RotateCcw,
  Search,
  Sparkles,
  Target,
  TriangleAlert,
  X,
} from 'lucide-react'
import {
  type ReactNode,
  useCallback,
  useDeferredValue,
  useEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { DimensionChips, UrgentDot } from '@/components/dim/dimension-chips'
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
import { useBatchDeleteFeedback } from '@/features/feedback/api/batch-delete'
import { useBatchRetryEnrichment } from '@/features/feedback/api/batch-retry-enrichment'
import { useBatchUpdateTags } from '@/features/feedback/api/batch-update-tags'
import type { FeedbackDetail } from '@/features/feedback/api/get-feedback-detail'
import { feedbackStatsQuery } from '@/features/feedback/api/get-feedback-stats'
import { terminalFailureWorkbenchQuery } from '@/features/feedback/api/get-terminal-failure-workbench'
import {
  type AttrFilterEntry,
  type Feedback,
  type FeedbackListFilters,
  feedbackListInfiniteQuery,
} from '@/features/feedback/api/list-feedback-infinite'
import { ClustersCard } from '@/features/feedback/components/clusters-card'
import { DeleteFeedbackDialog } from '@/features/feedback/components/delete-feedback-dialog'
import { FeedbackDetailSheet } from '@/features/feedback/components/detail-sheet'
import { DimStatsBars } from '@/features/feedback/components/dim-stats-bars'
import { LanguageBadge } from '@/features/feedback/components/language-badge'
import { RetryEnrichmentDialog } from '@/features/feedback/components/retry-enrichment-dialog'
import { SelectionActionBar } from '@/features/feedback/components/selection-action-bar'
import { TerminalFailureWorkbenchPanel } from '@/features/feedback/components/terminal-failure-workbench'
import { useRowSelection } from '@/features/feedback/hooks/use-row-selection'
import {
  useRecordSearchEvent,
  useSemanticSearch,
} from '@/features/feedback/hooks/use-semantic-search'
import {
  isTerminalFailure,
  MAX_ENRICHMENT_ATTEMPTS,
} from '@/features/feedback/lib/enrichment-utils'
import {
  selectTerminalFailurePriority,
  type TerminalFailureWorkbenchSectionLike,
} from '@/features/feedback/lib/terminal-failure-workbench'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { useDisplayName } from '@/lib/i18n-resolve'
import type { FeedbackFilter } from '@/proto/attune/v1/batch'
import type { Dimension } from '@/proto/attune/v1/common'
import type {
  SearchEvidence,
  SemanticSearchHit,
  SemanticSearchResponse,
} from '@/proto/attune/v1/search'
import type { Tag } from '@/proto/attune/v1/tag'
import type { WorkflowState } from '@/proto/attune/v1/workflow'

type FeedbackSortMode = 'newest' | 'urgent' | 'active'
type FeedbackQueueMode = 'all' | 'urgent' | 'active' | 'failed' | 'terminal' | 'ready'
type FeedbackSearchMode = 'keyword' | 'semantic'

interface SearchHitMeta {
  rank: number
  similarity: number
  keywordScore: number
  matchType: string
  semanticRank: number
  lexicalRank: number
  fusedScore: number
  evidence: SearchEvidence[]
  rankingSignals: string[]
}

type BatchTransitionFeedbackMutation = {
  mutate: (
    variables: { feedbackIds: string[]; toStateId: string; comment: string },
    options?: {
      onSuccess?: (result: { succeeded: number }) => void
      onError?: (error: unknown) => void
    },
  ) => void
}

interface FeedbackPageProps {
  dims: Dimension[]
  tagList: Tag[]
  stateList: WorkflowState[]
  batchTransition: BatchTransitionFeedbackMutation
  renderWorkflowTransition?: (data: FeedbackDetail) => ReactNode
  renderAuditLog?: (data: FeedbackDetail) => ReactNode
  initialQueueMode?: FeedbackQueueMode
  showTerminalWorkbench?: boolean
  initialQualityFilters?: Pick<
    FeedbackListFilters,
    | 'ids'
    | 'confidenceLte'
    | 'createdFrom'
    | 'createdTo'
    | 'enrichedFrom'
    | 'enrichedTo'
    | 'qualitySignal'
  >
}

export function FeedbackPage({
  dims,
  tagList,
  stateList,
  batchTransition,
  renderWorkflowTransition,
  renderAuditLog,
  initialQueueMode = 'all',
  showTerminalWorkbench = false,
  initialQualityFilters,
}: FeedbackPageProps) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  const { can } = usePermissions()
  const canViewLLMConfig = can('llm_config:view')
  const canViewRuntimeConfig = can('settings:enrichment_runtime:view')
  const initialQualityFilterKey = qualityFilterSyncKey(initialQualityFilters)
  const syncedQualityFilterKeyRef = useRef(initialQualityFilterKey)

  const [attrFilters, setAttrFilters] = useState<Record<string, string>>({})
  const [tagFilter, setTagFilter] = useState<string>('')
  const [workflowFilter, setWorkflowFilter] = useState<string>('')
  const [enrichmentFilter, setEnrichmentFilter] = useState<string>('')
  const [urgentOnly, setUrgentOnly] = useState(false)
  const [qInput, setQInput] = useState('')
  const [searchMode, setSearchMode] = useState<FeedbackSearchMode>('keyword')
  const [semanticResponse, setSemanticResponse] = useState<SemanticSearchResponse | null>(null)
  const [semanticQuery, setSemanticQuery] = useState('')
  const [semanticFilterKey, setSemanticFilterKey] = useState('')
  const [sortMode, setSortMode] = useState<FeedbackSortMode>('newest')
  const [queueMode, setQueueMode] = useState<FeedbackQueueMode>(initialQueueMode)
  const [qualityFilters, setQualityFilters] = useState(initialQualityFilters ?? {})
  const qDeferred = useDeferredValue(qInput)
  const [detailId, setDetailId] = useState<string | null>(null)
  const detailRestoreFocusRef = useRef<HTMLElement | null>(null)
  const openDetail = useCallback((feedbackId: string, restoreFocusTo?: HTMLElement) => {
    detailRestoreFocusRef.current = restoreFocusTo ?? null
    setDetailId(feedbackId)
  }, [])
  useEffect(() => {
    if (syncedQualityFilterKeyRef.current === initialQualityFilterKey) return
    syncedQualityFilterKeyRef.current = initialQualityFilterKey
    setQualityFilters(initialQualityFilters ?? {})
  }, [initialQualityFilterKey, initialQualityFilters])
  const terminalWorkbench = useQuery({
    ...terminalFailureWorkbenchQuery(),
    enabled: showTerminalWorkbench,
  })
  const terminalWorkbenchPriority = useMemo(() => {
    if (!showTerminalWorkbench || !terminalWorkbench.data) return null
    const workbench = terminalWorkbench.data
    const sections: TerminalFailureWorkbenchSectionLike[] = [
      {
        key: 'reason_class',
        title: t('feedback.terminal_workbench.reason_class_title'),
        clusters: workbench.reasonClassClusters,
      },
      {
        key: 'model_channel',
        title: t('feedback.terminal_workbench.model_channel_title'),
        clusters: workbench.modelChannelClusters,
        remediationPath: canViewLLMConfig ? '/configuration/llm' : undefined,
        remediationLabel: canViewLLMConfig
          ? t('feedback.terminal_workbench.remediation_llm')
          : undefined,
      },
      {
        key: 'config_fingerprint',
        title: t('feedback.terminal_workbench.config_fingerprint_title'),
        clusters: workbench.configFingerprintClusters,
        remediationPath: canViewRuntimeConfig ? '/configuration/enrichment-runtime' : undefined,
        remediationLabel: canViewRuntimeConfig
          ? t('feedback.terminal_workbench.remediation_runtime')
          : undefined,
      },
      {
        key: 'age_bucket',
        title: t('feedback.terminal_workbench.age_bucket_title'),
        clusters: workbench.ageBucketClusters,
      },
    ]
    return selectTerminalFailurePriority(sections)
  }, [canViewLLMConfig, canViewRuntimeConfig, showTerminalWorkbench, terminalWorkbench.data, t])

  const scopeFilters: FeedbackListFilters = useMemo(() => {
    const attrs: AttrFilterEntry[] = Object.entries(attrFilters)
      .filter(([, v]) => v && v !== '__all')
      .map(([dim, value]) => ({ dim, value }))
    // Enrichment status: independent filter takes precedence, then queue mode
    const effectiveEnrichmentStatus =
      enrichmentFilter ||
      (queueMode === 'failed' || queueMode === 'terminal' ? 'failed' : undefined)
    return {
      attrs,
      urgent: urgentOnly ? true : undefined,
      tag: tagFilter || undefined,
      workflowState: workflowFilter || undefined,
      enrichmentStatus: effectiveEnrichmentStatus || undefined,
      terminalFailedOnly: queueMode === 'terminal' ? true : undefined,
      ids: qualityFilters.ids,
      confidenceLte: qualityFilters.confidenceLte,
      createdFrom: qualityFilters.createdFrom,
      createdTo: qualityFilters.createdTo,
      enrichedFrom: qualityFilters.enrichedFrom,
      enrichedTo: qualityFilters.enrichedTo,
      qualitySignal: qualityFilters.qualitySignal,
    }
  }, [
    attrFilters,
    tagFilter,
    urgentOnly,
    workflowFilter,
    enrichmentFilter,
    queueMode,
    qualityFilters,
  ])

  const filters: FeedbackListFilters = useMemo(
    () => ({
      ...scopeFilters,
      q: searchMode === 'keyword' ? qDeferred.trim() : undefined,
    }),
    [qDeferred, scopeFilters, searchMode],
  )

  const semanticFilter = useMemo(
    () => buildSemanticFilter(scopeFilters, dims),
    [dims, scopeFilters],
  )
  const currentSemanticFilterKey = useMemo(
    () => semanticFilterCacheKey(semanticFilter),
    [semanticFilter],
  )

  const list = useInfiniteQuery(feedbackListInfiniteQuery(filters))
  const semanticSearch = useSemanticSearch()
  const recordSearchEvent = useRecordSearchEvent()
  const listItems = list.data?.pages.flatMap((p) => p.items) ?? []
  const isSemanticResponseCurrent =
    searchMode === 'semantic' &&
    semanticResponse != null &&
    semanticQuery === qInput.trim() &&
    semanticFilterKey === currentSemanticFilterKey
  const semanticItems = useMemo(
    () =>
      isSemanticResponseCurrent
        ? semanticResponse.hits.flatMap((hit) => {
            const row = semanticHitToFeedback(hit)
            return row ? [row] : []
          })
        : [],
    [isSemanticResponseCurrent, semanticResponse],
  )
  const searchMetaById = useMemo(() => {
    const meta = new Map<string, SearchHitMeta>()
    if (!isSemanticResponseCurrent || !semanticResponse) return meta
    semanticResponse.hits.forEach((hit, index) => {
      if (!hit.feedback?.id) return
      meta.set(String(hit.feedback.id), {
        rank: index + 1,
        similarity: hit.similarity,
        keywordScore: hit.keywordScore,
        matchType: hit.matchType,
        semanticRank: hit.semanticRank,
        lexicalRank: hit.lexicalRank,
        fusedScore: hit.fusedScore,
        evidence: hit.evidence ?? [],
        rankingSignals: hit.rankingSignals ?? [],
      })
    })
    return meta
  }, [isSemanticResponseCurrent, semanticResponse])
  const handleOpenFeedbackDetail = useCallback(
    (feedbackId: string, restoreFocusTo?: HTMLElement) => {
      if (isSemanticResponseCurrent && semanticResponse?.runId) {
        const meta = searchMetaById.get(feedbackId)
        if (meta) {
          recordSearchEvent.mutate({
            runId: semanticResponse.runId,
            feedbackId,
            action: 'open',
            rank: meta.rank,
            matchType: meta.matchType,
          })
        }
      }
      openDetail(feedbackId, restoreFocusTo)
    },
    [
      isSemanticResponseCurrent,
      openDetail,
      recordSearchEvent,
      searchMetaById,
      semanticResponse?.runId,
    ],
  )
  const items = isSemanticResponseCurrent ? semanticItems : listItems
  const stats = useQuery(feedbackStatsQuery())
  const activeFilterCount =
    Object.values(attrFilters).filter((value) => value && value !== '__all').length +
    (tagFilter ? 1 : 0) +
    (workflowFilter ? 1 : 0) +
    (enrichmentFilter ? 1 : 0) +
    (qualityFilters.ids?.length ? 1 : 0) +
    (qualityFilters.confidenceLte != null ? 1 : 0) +
    (qualityFilters.createdFrom || qualityFilters.createdTo ? 1 : 0) +
    (qualityFilters.enrichedFrom || qualityFilters.enrichedTo ? 1 : 0) +
    (qualityFilters.qualitySignal ? 1 : 0) +
    (urgentOnly ? 1 : 0) +
    (queueMode !== initialQueueMode ? 1 : 0) +
    (qInput.trim() ? 1 : 0)
  const hasActiveFilters = activeFilterCount > 0
  const workflowLabel = workflowFilter
    ? (displayOf(stateList.find((state) => state.id === workflowFilter)?.displayName) ??
      stateList.find((state) => state.id === workflowFilter)?.name ??
      workflowFilter)
    : null
  const visibleActiveCount = items.filter(
    (item) => item.workflowState?.category === 'active',
  ).length
  const sortedItems = useMemo(() => sortFeedbackItems(items, sortMode), [items, sortMode])
  const displayedItems = useMemo(
    () => filterFeedbackItemsByQueueMode(sortedItems, queueMode),
    [queueMode, sortedItems],
  )
  const displayedUrgentCount = displayedItems.filter((item) => item.isUrgent).length
  const displayedActiveCount = displayedItems.filter(
    (item) => item.workflowState?.category === 'active',
  ).length
  const displayedReadyCount = displayedItems.filter(isFeedbackReadyForTriage).length
  const displayedFailedCount = displayedItems.filter(
    (item) => item.enrichmentStatus === 'failed',
  ).length
  const displayedTerminalCount = displayedItems.filter(isTerminalFailure).length
  const displayedPendingAiCount = displayedItems.filter(
    (item) => item.enrichmentStatus === 'pending' || item.enrichmentStatus === 'enriching',
  ).length
  const uniqueSources = new Set(displayedItems.map((item) => item.source).filter(Boolean)).size
  const oldestVisibleAt = displayedItems.reduce<string | null>((oldest, item) => {
    if (!oldest) return item.createdAt
    return new Date(item.createdAt).getTime() < new Date(oldest).getTime() ? item.createdAt : oldest
  }, null)
  const priorityItem =
    displayedItems.find((item) => item.isUrgent) ??
    displayedItems.find((item) => item.workflowState?.category === 'active') ??
    displayedItems[0] ??
    null
  const hasQueueScopedEmpty = items.length > 0 && displayedItems.length === 0

  const itemIds = useMemo(() => displayedItems.map((i) => i.id), [displayedItems])
  const { selected, toggle, toggleAll, clear, isAllSelected } = useRowSelection(itemIds)

  const batchUpdate = useBatchUpdateTags()
  const batchRetry = useBatchRetryEnrichment()
  const batchDelete = useBatchDeleteFeedback()
  const [retryDialogOpen, setRetryDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)

  const handleSearchModeChange = useCallback(
    (mode: FeedbackSearchMode) => {
      if (mode === searchMode) return
      setSearchMode(mode)
      setSemanticResponse(null)
      setSemanticQuery('')
      setSemanticFilterKey('')
      semanticSearch.reset()
    },
    [searchMode, semanticSearch],
  )

  const handleQueryChange = useCallback(
    (value: string) => {
      setQInput(value)
      if (searchMode === 'semantic') {
        semanticSearch.reset()
      }
    },
    [searchMode, semanticSearch],
  )

  const handleSemanticSearch = useCallback(() => {
    const query = qInput.trim()
    if (!query) return
    semanticSearch.mutate(
      {
        q: query,
        limit: 50,
        filter: semanticFilter,
      },
      {
        onSuccess: (response) => {
          setSemanticResponse(response)
          setSemanticQuery(query)
          setSemanticFilterKey(currentSemanticFilterKey)
        },
        onError: () => {
          setSemanticResponse(null)
          setSemanticQuery('')
          setSemanticFilterKey('')
        },
      },
    )
  }, [currentSemanticFilterKey, qInput, semanticFilter, semanticSearch])

  const selectedTerminalFailures = useMemo(() => {
    return items.filter((i) => selected.has(i.id) && isTerminalFailure(i))
  }, [items, selected])

  const removableTags = useMemo(() => {
    const selectedItems = items.filter((i) => selected.has(i.id))
    const tagMap = new Map<string, Tag>()
    for (const item of selectedItems) {
      for (const tag of item.tags ?? []) {
        if (!tagMap.has(tag.id)) tagMap.set(tag.id, tag)
      }
    }
    return Array.from(tagMap.values())
  }, [items, selected])

  const activeFilterChips = useMemo(() => {
    const chips: Array<{
      key: string
      label: string
      value: string
      tone?: 'default' | 'urgent' | 'active'
      onRemove: () => void
    }> = []

    for (const dim of dims) {
      const current = attrFilters[dim.name]
      if (!current || current === '__all') continue
      const tax = dim.taxonomy.find((entry) => entry.value === current)
      chips.push({
        key: `dim:${dim.name}`,
        label: displayOf(dim.displayName) || dim.name,
        value: displayOf(tax?.displayName) || current,
        onRemove: () => setAttrFilters((map) => ({ ...map, [dim.name]: '__all' })),
      })
    }

    if (tagFilter) {
      chips.push({
        key: 'tag',
        label: t('feedback.filter.all_tags'),
        value: tagList.find((tag) => tag.id === tagFilter)?.name ?? tagFilter,
        onRemove: () => setTagFilter(''),
      })
    }

    if (workflowLabel) {
      chips.push({
        key: 'workflow',
        label: t('feedback.focus_items.workflow'),
        value: workflowLabel,
        onRemove: () => setWorkflowFilter(''),
      })
    }

    if (enrichmentFilter) {
      chips.push({
        key: 'enrichment',
        label: t('feedback.filter.enrichment_status'),
        value: enrichmentStatusLabel(enrichmentFilter, t),
        tone: enrichmentFilter === 'failed' ? 'urgent' : 'active',
        onRemove: () => setEnrichmentFilter(''),
      })
    }

    if (qualityFilters.ids?.length) {
      chips.push({
        key: 'quality-ids',
        label: t('feedback.quality_filters.ids'),
        value: t('feedback.quality_filters.ids_value', { count: qualityFilters.ids.length }),
        tone: 'active',
        onRemove: () => setQualityFilters((old) => ({ ...old, ids: undefined })),
      })
    }

    if (qualityFilters.qualitySignal) {
      chips.push({
        key: 'quality-signal',
        label: t('feedback.quality_filters.signal'),
        value: qualitySignalLabel(qualityFilters.qualitySignal, t),
        tone: 'active',
        onRemove: () => setQualityFilters((old) => ({ ...old, qualitySignal: undefined })),
      })
    }

    if (qualityFilters.confidenceLte != null) {
      chips.push({
        key: 'quality-confidence',
        label: t('feedback.quality_filters.confidence'),
        value: t('feedback.quality_filters.confidence_lte', {
          value: Math.round(qualityFilters.confidenceLte * 100),
        }),
        tone: 'active',
        onRemove: () => setQualityFilters((old) => ({ ...old, confidenceLte: undefined })),
      })
    }

    if (urgentOnly) {
      chips.push({
        key: 'urgent',
        label: t('feedback.summary.urgent'),
        value: t('feedback.filter.quick.urgent_active'),
        tone: 'urgent',
        onRemove: () => setUrgentOnly(false),
      })
    }

    if (qInput.trim()) {
      chips.push({
        key: 'search',
        label: t('feedback.focus_items.search'),
        value: qInput.trim(),
        tone: 'active',
        onRemove: () => handleQueryChange(''),
      })
    }

    if (searchMode === 'semantic') {
      chips.push({
        key: 'search-mode',
        label: t('feedback.search.mode_label'),
        value: t('feedback.search.mode.semantic'),
        tone: 'active',
        onRemove: () => handleSearchModeChange('keyword'),
      })
    }

    if (sortMode !== 'newest') {
      chips.push({
        key: 'sort',
        label: t('feedback.sort.label'),
        value: sortModeLabel(sortMode, t),
        tone: 'active',
        onRemove: () => setSortMode('newest'),
      })
    }

    if (queueMode !== initialQueueMode) {
      chips.push({
        key: 'queue',
        label: t('feedback.queue_mode.label'),
        value: queueModeLabel(queueMode, t),
        tone: 'active',
        onRemove: () => setQueueMode(initialQueueMode),
      })
    }

    return chips
  }, [
    attrFilters,
    dims,
    displayOf,
    enrichmentFilter,
    handleQueryChange,
    handleSearchModeChange,
    initialQueueMode,
    qualityFilters,
    queueMode,
    qInput,
    searchMode,
    sortMode,
    tagFilter,
    tagList,
    t,
    urgentOnly,
    workflowLabel,
  ])

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

  const handleBatchRetryEnrichment = () => {
    if (selectedTerminalFailures.length === 0) {
      toast.error(t('feedback.batch.no_terminal_failures'))
      return
    }
    setRetryDialogOpen(true)
  }

  const confirmBatchRetryEnrichment = () => {
    const ids = selectedTerminalFailures.map((f) => f.id)
    batchRetry.mutate(ids, {
      onSuccess: (res) => {
        setRetryDialogOpen(false)
        if (res.failed.length === 0) {
          toast.success(t('feedback.batch.retry_enrichment_success', { count: res.succeeded }))
        } else if (res.succeeded > 0) {
          toast.warning(
            t('feedback.batch.retry_enrichment_partial', {
              succeeded: res.succeeded,
              failed: res.failed.length,
            }),
          )
        } else {
          toast.error(t('feedback.batch.retry_enrichment_failed'))
        }
        clear()
      },
      onError: (err) => {
        setRetryDialogOpen(false)
        toast.error(err instanceof Error ? err.message : t('common.error'))
      },
    })
  }

  const handleBatchDelete = () => {
    if (selected.size === 0) return
    setDeleteDialogOpen(true)
  }

  const confirmBatchDelete = () => {
    const ids = Array.from(selected)
    batchDelete.mutate(ids, {
      onSuccess: (res) => {
        setDeleteDialogOpen(false)
        toast.success(t('feedback.batch.delete_success', { count: res.succeeded }))
        clear()
      },
      onError: (err) => {
        setDeleteDialogOpen(false)
        toast.error(err instanceof Error ? err.message : t('common.error'))
      },
    })
  }

  const clearFilters = () => {
    setAttrFilters({})
    setTagFilter('')
    setWorkflowFilter('')
    setEnrichmentFilter('')
    setQualityFilters({})
    setUrgentOnly(false)
    setQueueMode(initialQueueMode)
    setQInput('')
    setSearchMode('keyword')
    setSemanticResponse(null)
    setSemanticQuery('')
    setSemanticFilterKey('')
    semanticSearch.reset()
  }

  return (
    <div className="space-y-5">
      <section className="rounded-[1.45rem] border border-border/70 bg-[linear-gradient(180deg,rgba(255,252,248,0.9),rgba(255,255,255,0.995)_24%,rgba(249,250,251,0.99))] p-2 shadow-[0_24px_70px_-58px_rgba(15,23,42,0.2)]">
        <div className="relative overflow-hidden rounded-[1.28rem] border border-border/55 bg-[radial-gradient(circle_at_top_left,rgba(255,247,237,0.96),rgba(255,255,255,0.985)_38%),linear-gradient(135deg,rgba(255,255,255,0.985),rgba(248,250,252,0.98))] px-5 py-4.5 sm:px-6 sm:py-5">
          <div className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-primary/30 to-transparent" />
          <div className="pointer-events-none absolute top-0 right-0 h-36 w-36 rounded-full bg-primary/[0.06] blur-3xl" />
          <div className="min-w-0">
            <div className="text-[11px] font-semibold tracking-[0.18em] text-muted-foreground uppercase">
              {t('feedback.eyebrow')}
            </div>
            <div className="mt-2 flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
              <div className="min-w-0">
                <h1 className="text-[2rem] font-semibold tracking-tight text-foreground text-balance sm:text-[2.25rem]">
                  {t('nav.feedback')}
                </h1>
                <p className="mt-2.5 max-w-2xl text-[13.5px] leading-[1.6rem] text-muted-foreground text-pretty">
                  {t('feedback.subtitle')}
                </p>
              </div>
              <div className="flex flex-wrap items-center gap-2 lg:justify-end">
                <WorkbenchFact
                  label={t('feedback.workbench_mode')}
                  value={queueModeLabel(queueMode, t)}
                />
                <WorkbenchFact
                  label={t('feedback.workbench_sort')}
                  value={sortModeLabel(sortMode, t)}
                />
                <WorkbenchFact
                  label={t('feedback.workbench_filters')}
                  value={
                    activeFilterCount > 0
                      ? t('feedback.workbench_filters_active', { count: activeFilterCount })
                      : t('feedback.workbench_filters_idle')
                  }
                />
              </div>
            </div>
            <div className="mt-4 grid gap-2 rounded-[1.15rem] border border-border/60 bg-background/80 p-2.5 shadow-[inset_0_1px_0_rgba(255,255,255,0.65)] sm:grid-cols-2 xl:grid-cols-6">
              <FeedbackStat
                label={t('feedback.summary.visible')}
                value={String(displayedItems.length)}
                hint={t('feedback.queue.visible_hint')}
              />
              <FeedbackStat
                label={t('feedback.summary.urgent')}
                value={String(displayedUrgentCount)}
                tone={displayedUrgentCount > 0 ? 'urgent' : 'default'}
                hint={t('feedback.queue.playbook.urgent_body')}
              />
              <FeedbackStat
                label={t('feedback.row.priority.active')}
                value={String(displayedActiveCount)}
                tone={displayedActiveCount > 0 ? 'active' : 'default'}
                hint={t('feedback.queue.playbook.active_body')}
              />
              <FeedbackStat
                label={t('feedback.queue.failed')}
                value={String(displayedFailedCount)}
                tone={displayedFailedCount > 0 ? 'urgent' : 'default'}
                hint={t('feedback.queue.failed_hint')}
              />
              <FeedbackStat
                label={t('feedback.queue_mode.terminal')}
                value={String(displayedTerminalCount)}
                tone={displayedTerminalCount > 0 ? 'terminal' : 'default'}
                hint={t('feedback.queue.terminal_hint')}
              />
              <FeedbackStat
                label={t('feedback.queue.ready')}
                value={String(displayedReadyCount)}
                tone={displayedReadyCount > 0 ? 'active' : 'default'}
                hint={t('feedback.queue.ready_hint')}
              />
            </div>
          </div>
        </div>

        <Card className="mt-2 overflow-hidden rounded-[1.2rem] border-border/65 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(248,250,252,0.985))] py-0 shadow-[0_16px_36px_-34px_rgba(15,23,42,0.14)]">
          <CardContent className="space-y-3 px-5 py-3.5 sm:px-6">
            <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
              <div className="text-[10px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                {t('feedback.filter_title')}
              </div>
              {!hasActiveFilters && selected.size === 0 ? (
                <div className="text-sm text-muted-foreground">
                  {t('feedback.summary.filters_idle')}
                </div>
              ) : null}
            </div>
            <div className="rounded-[1rem] border border-border/55 bg-background/82 px-3.5 py-3 sm:px-4">
              <FilterBar
                dims={dims}
                attrFilters={attrFilters}
                tagFilter={tagFilter}
                workflowFilter={workflowFilter}
                enrichmentFilter={enrichmentFilter}
                tags={tagList}
                workflowStates={stateList}
                urgentOnly={urgentOnly}
                q={qInput}
                searchMode={searchMode}
                semanticIsLoading={semanticSearch.isPending}
                onAttrChange={(dim, value) => setAttrFilters((m) => ({ ...m, [dim]: value }))}
                onTagChange={setTagFilter}
                onWorkflowChange={setWorkflowFilter}
                onEnrichmentChange={setEnrichmentFilter}
                onUrgentToggle={() => setUrgentOnly((value) => !value)}
                onQ={handleQueryChange}
                onSearchModeChange={handleSearchModeChange}
                onSemanticSearch={handleSemanticSearch}
              />
            </div>
            {searchMode === 'semantic' && (
              <SemanticSearchStatus
                response={isSemanticResponseCurrent ? semanticResponse : null}
                query={qInput.trim()}
                isStale={semanticResponse != null && !isSemanticResponseCurrent}
                isLoading={semanticSearch.isPending}
                errorMessage={
                  semanticSearch.isError
                    ? semanticSearch.error instanceof Error
                      ? semanticSearch.error.message
                      : t('common.error')
                    : ''
                }
                onSearch={handleSemanticSearch}
              />
            )}
            <div className="flex min-h-9 flex-wrap items-center gap-2">
              {activeFilterChips.map((chip) => (
                <ActiveChip
                  key={chip.key}
                  label={chip.label}
                  value={chip.value}
                  tone={chip.tone}
                  onRemove={chip.onRemove}
                />
              ))}
              {selected.size > 0 && (
                <ActiveChip
                  label={t('feedback.focus_items.selection')}
                  value={t('feedback.focus_items.selection_active', { count: selected.size })}
                />
              )}
              {hasActiveFilters && (
                <Button variant="ghost" className="h-9 px-3 text-sm" onClick={clearFilters}>
                  {t('feedback.clear_filters')}
                </Button>
              )}
            </div>
          </CardContent>
        </Card>
      </section>

      <section className="space-y-4">
        {showTerminalWorkbench && (
          <TerminalFailureWorkbenchPanel
            data={terminalWorkbench.data}
            isLoading={terminalWorkbench.isPending}
            isError={terminalWorkbench.isError}
            errorMessage={
              terminalWorkbench.error instanceof Error
                ? terminalWorkbench.error.message
                : t('common.error')
            }
            onRetry={() => void terminalWorkbench.refetch()}
            onOpenFeedback={handleOpenFeedbackDetail}
          />
        )}

        <Card className="gap-0 overflow-hidden rounded-[1.2rem] border-border/75 bg-[linear-gradient(180deg,rgba(255,255,255,0.995),rgba(249,250,251,0.985))] py-0 shadow-[0_28px_72px_-52px_rgba(15,23,42,0.22)]">
          <CardHeader className="border-b border-border/55 bg-[linear-gradient(180deg,rgba(248,250,252,0.82),rgba(255,255,255,0.92))] px-5 py-4 sm:px-6">
            <div className="flex flex-col gap-3 xl:flex-row xl:items-end xl:justify-between">
              <div className="min-w-0 space-y-2">
                <div className="flex flex-wrap items-center gap-2">
                  <div className="text-[10px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                    {t('feedback.feed_title')}
                  </div>
                  <StatusMetaChip value={queueModeLabel(queueMode, t)} />
                  <StatusMetaChip value={sortModeLabel(sortMode, t)} />
                </div>
                <CardTitle className="text-[1.18rem] tracking-tight">
                  {t('feedback.feed_title')}
                </CardTitle>
                <CardDescription className="text-sm leading-[1.35rem]">
                  {t('feedback.feed_description', { count: displayedItems.length })}
                </CardDescription>
              </div>
              <div className="flex flex-col gap-2 xl:items-end">
                <div className="flex flex-wrap items-center gap-2 xl:justify-end">
                  {selected.size > 0 ? (
                    <StatusMetaChip
                      value={t('feedback.focus_items.selection_active', { count: selected.size })}
                    />
                  ) : null}
                  {activeFilterCount > 0 ? (
                    <StatusMetaChip
                      value={t('feedback.workbench_filters_active', { count: activeFilterCount })}
                    />
                  ) : null}
                </div>
                <div className="w-full min-w-[13rem] rounded-full border border-border/60 bg-background/92 p-1 xl:w-56">
                  <Select
                    value={sortMode}
                    onValueChange={(value) => setSortMode(value as FeedbackSortMode)}
                  >
                    <SelectTrigger
                      className="h-9 rounded-full border-0 bg-transparent shadow-none"
                      aria-label={t('feedback.sort.label')}
                    >
                      <SelectValue placeholder={t('feedback.sort.label')} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="newest">{t('feedback.sort.newest')}</SelectItem>
                      <SelectItem value="urgent">{t('feedback.sort.urgent')}</SelectItem>
                      <SelectItem value="active">{t('feedback.sort.active')}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
            </div>
            <div className="mt-3 border-t border-border/50 pt-3">
              <QueueHeaderSummaryStrip
                queueMode={queueMode}
                visibleCount={displayedItems.length}
                readyCount={displayedReadyCount}
                failedCount={displayedFailedCount}
                terminalCount={displayedTerminalCount}
                pendingAiCount={displayedPendingAiCount}
                oldestVisibleAt={oldestVisibleAt}
                priorityItem={priorityItem}
              />
            </div>
          </CardHeader>
          <CardContent className="bg-background px-0">
            {semanticSearch.isPending ? (
              <Loading />
            ) : searchMode === 'semantic' && semanticSearch.isError ? (
              <QueueErrorState
                message={
                  semanticSearch.error instanceof Error
                    ? semanticSearch.error.message
                    : t('common.error')
                }
                onRetry={handleSemanticSearch}
              />
            ) : !isSemanticResponseCurrent && list.isPending ? (
              <Loading />
            ) : !isSemanticResponseCurrent && list.isError ? (
              <QueueErrorState
                message={list.error instanceof Error ? list.error.message : t('common.error')}
                onRetry={() => void list.refetch()}
              />
            ) : items.length > 0 ? (
              <div className="grid gap-0 xl:grid-cols-[minmax(0,1fr)_21.5rem]">
                <div className="min-w-0 border-b border-border/50 xl:border-r xl:border-b-0">
                  <div className="sticky top-0 z-10 border-b border-border/50 bg-background/96 backdrop-blur">
                    {selected.size > 0 && (
                      <div className="border-b border-border/50 px-4 py-3.5 sm:px-5">
                        <SelectionActionBar
                          count={selected.size}
                          availableTags={tagList}
                          removableTags={removableTags}
                          workflowStates={stateList}
                          onBatchAdd={handleBatchAdd}
                          onBatchRemove={handleBatchRemove}
                          onBatchTransition={handleBatchTransition}
                          onBatchDelete={handleBatchDelete}
                          onBatchRetryEnrichment={handleBatchRetryEnrichment}
                          terminalFailureCount={selectedTerminalFailures.length}
                          onCancel={clear}
                        />
                      </div>
                    )}
                    <div className="border-b border-border/50 px-4 py-3.5 sm:px-5">
                      {isSemanticResponseCurrent && semanticResponse?.usedKeywordFallback ? (
                        <SemanticFallbackBanner reason={semanticResponse.fallbackReason} />
                      ) : null}
                      <QueueLaneBanner
                        queueMode={queueMode}
                        urgentOnly={urgentOnly}
                        visibleCount={displayedItems.length}
                        urgentCount={displayedUrgentCount}
                        failedCount={displayedFailedCount}
                        priorityItem={priorityItem}
                        onEnableUrgentScope={() => setUrgentOnly(true)}
                        onOpenPriority={(id) => handleOpenFeedbackDetail(id)}
                      />
                    </div>
                    <div className="px-4 py-3 sm:px-5">
                      <QueueOperatorDeck
                        queueMode={queueMode}
                        sortMode={sortMode}
                        activeFilterCount={activeFilterCount}
                        visibleCount={displayedItems.length}
                        urgentCount={displayedUrgentCount}
                        activeCount={visibleActiveCount}
                        failedCount={displayedFailedCount}
                        terminalCount={displayedTerminalCount}
                        readyCount={displayedReadyCount}
                        pendingAiCount={displayedPendingAiCount}
                        priorityItem={priorityItem}
                        hasActiveFilters={hasActiveFilters}
                        selectedCount={selected.size}
                        isAllSelected={isAllSelected}
                        onToggleAll={toggleAll}
                        onOpenPriority={(id) => handleOpenFeedbackDetail(id)}
                        terminalWorkbenchPriority={terminalWorkbenchPriority}
                        showTerminalWorkbench={showTerminalWorkbench}
                        onClearFilters={clearFilters}
                        onChangeQueueMode={setQueueMode}
                        nextStepLabel={queuePrimaryActionLabel(
                          queueMode,
                          displayedUrgentCount,
                          displayedFailedCount,
                          displayedTerminalCount,
                          t,
                        )}
                      />
                    </div>
                  </div>
                  {hasQueueScopedEmpty ? (
                    <FeedbackSubqueueEmptyState
                      queueMode={queueMode}
                      onResetQueue={() => setQueueMode(initialQueueMode)}
                      onResetAll={clearFilters}
                    />
                  ) : (
                    <FeedbackTable
                      items={displayedItems}
                      dims={dims}
                      allTags={tagList}
                      searchMetaById={isSemanticResponseCurrent ? searchMetaById : undefined}
                      selected={selected}
                      onToggle={toggle}
                      onRowClick={handleOpenFeedbackDetail}
                    />
                  )}
                  {!isSemanticResponseCurrent && list.hasNextPage && (
                    <div className="border-t border-border/50 px-4 py-4 sm:px-5">
                      <div className="flex justify-center">
                        <Button
                          variant="ghost"
                          onClick={() => void list.fetchNextPage()}
                          disabled={list.isFetchingNextPage}
                        >
                          {list.isFetchingNextPage && (
                            <Loader2 className="mr-2 h-3.5 w-3.5 animate-spin" />
                          )}
                          {t('feedback.load_more')}
                        </Button>
                      </div>
                    </div>
                  )}
                </div>

                <QueueRail
                  selectedCount={selected.size}
                  query={qInput.trim()}
                  workflowLabel={workflowLabel}
                  queueMode={queueMode}
                  visibleCount={displayedItems.length}
                  urgentCount={displayedUrgentCount}
                  readyCount={displayedReadyCount}
                  failedCount={displayedFailedCount}
                  terminalCount={displayedTerminalCount}
                  pendingAiCount={displayedPendingAiCount}
                  sourceCount={uniqueSources}
                  oldestVisibleAt={oldestVisibleAt}
                />
              </div>
            ) : isSemanticResponseCurrent ? (
              <SemanticSearchEmptyState onSearch={handleSemanticSearch} onReset={clearFilters} />
            ) : hasActiveFilters ? (
              <FeedbackFilteredEmptyState onReset={clearFilters} />
            ) : (
              <FeedbackEmptyWorkspace
                title={t('feedback.empty_title')}
                description={t('feedback.empty_body')}
              />
            )}
          </CardContent>
        </Card>

        {!showTerminalWorkbench && displayedTerminalCount > 0 && (
          <TerminalFailuresSummaryCard
            terminalCount={displayedTerminalCount}
            failedCount={displayedFailedCount}
            onViewTerminal={() => setQueueMode('terminal')}
            onRetryAll={
              selectedTerminalFailures.length > 0 ? handleBatchRetryEnrichment : undefined
            }
          />
        )}

        <details className="group rounded-[0.95rem] border border-border/45 bg-muted/6">
          <summary className="flex cursor-pointer list-none items-center justify-between px-4 py-3 text-sm font-medium text-foreground">
            <span>{t('feedback.stats.title')}</span>
            <ChevronDown className="size-4 text-muted-foreground transition-transform group-open:rotate-180" />
          </summary>
          <div className="border-t border-border/45 px-4 py-4">
            <div className="grid gap-4 xl:grid-cols-[minmax(0,1.15fr)_minmax(0,0.85fr)]">
              {stats.data && Number(stats.data.total) > 0 && (
                <Card className="rounded-[0.9rem] border-border/40 shadow-none">
                  <CardContent className="px-4 py-3.5">
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
            </div>
          </div>
        </details>
      </section>

      <FeedbackDetailSheet
        id={detailId}
        dims={dims}
        availableTags={tagList}
        workbenchMode={queueMode}
        onOpenChange={(v) => !v && setDetailId(null)}
        restoreFocusRef={detailRestoreFocusRef}
        renderWorkflowTransition={renderWorkflowTransition}
        renderAuditLog={renderAuditLog}
      />

      <RetryEnrichmentDialog
        open={retryDialogOpen}
        count={selectedTerminalFailures.length}
        isLoading={batchRetry.isPending}
        onConfirm={confirmBatchRetryEnrichment}
        onCancel={() => setRetryDialogOpen(false)}
      />

      <DeleteFeedbackDialog
        open={deleteDialogOpen}
        count={selected.size}
        isLoading={batchDelete.isPending}
        onConfirm={confirmBatchDelete}
        onCancel={() => setDeleteDialogOpen(false)}
      />
    </div>
  )
}

function qualityFilterSyncKey(filters: FeedbackPageProps['initialQualityFilters']) {
  if (!filters) return ''
  return [
    filters.ids?.join(',') ?? '',
    filters.confidenceLte ?? '',
    filters.createdFrom ?? '',
    filters.createdTo ?? '',
    filters.enrichedFrom ?? '',
    filters.enrichedTo ?? '',
    filters.qualitySignal ?? '',
  ].join('\x1f')
}

function FilterBar({
  dims,
  attrFilters,
  tagFilter,
  workflowFilter,
  enrichmentFilter,
  tags,
  workflowStates,
  urgentOnly,
  q,
  searchMode,
  semanticIsLoading,
  onAttrChange,
  onTagChange,
  onWorkflowChange,
  onEnrichmentChange,
  onUrgentToggle,
  onQ,
  onSearchModeChange,
  onSemanticSearch,
}: {
  dims: Dimension[]
  attrFilters: Record<string, string>
  tagFilter: string
  workflowFilter: string
  enrichmentFilter: string
  tags: Tag[]
  workflowStates: WorkflowState[]
  urgentOnly: boolean
  q: string
  searchMode: FeedbackSearchMode
  semanticIsLoading: boolean
  onAttrChange: (dim: string, value: string) => void
  onTagChange: (tagId: string) => void
  onWorkflowChange: (stateId: string) => void
  onEnrichmentChange: (status: string) => void
  onUrgentToggle: () => void
  onQ: (v: string) => void
  onSearchModeChange: (mode: FeedbackSearchMode) => void
  onSemanticSearch: () => void
}) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  const activeTags = tags.filter((tag) => !tag.archived)
  const activeStates = workflowStates.filter((s) => !s.archived)

  return (
    <div className="space-y-2.5">
      <form
        className="grid gap-2 lg:grid-cols-[minmax(0,1fr)_auto_auto]"
        onSubmit={(event) => {
          event.preventDefault()
          if (searchMode === 'semantic') onSemanticSearch()
        }}
      >
        <div className="relative">
          <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            placeholder={t('feedback.filter.search_placeholder')}
            value={q}
            onChange={(e) => onQ(e.target.value)}
            className="h-10 bg-background pl-9"
            aria-label={t('feedback.search.input_label')}
          />
        </div>
        <fieldset
          className="grid grid-cols-2 rounded-lg border border-border/60 bg-background/90 p-1"
          aria-label={t('feedback.search.mode_label')}
        >
          <Button
            type="button"
            variant={searchMode === 'keyword' ? 'secondary' : 'ghost'}
            className="h-8 px-3 text-xs"
            aria-pressed={searchMode === 'keyword'}
            onClick={() => onSearchModeChange('keyword')}
          >
            <Search className="h-3.5 w-3.5" />
            {t('feedback.search.mode.keyword')}
          </Button>
          <Button
            type="button"
            variant={searchMode === 'semantic' ? 'secondary' : 'ghost'}
            className="h-8 px-3 text-xs"
            aria-pressed={searchMode === 'semantic'}
            onClick={() => onSearchModeChange('semantic')}
          >
            <Sparkles className="h-3.5 w-3.5" />
            {t('feedback.search.mode.semantic')}
          </Button>
        </fieldset>
        <Button
          type="submit"
          className="h-10"
          disabled={searchMode !== 'semantic' || semanticIsLoading || !q.trim()}
        >
          {semanticIsLoading ? (
            <Loader2 className="h-4 w-4 animate-spin" />
          ) : (
            <Sparkles className="h-4 w-4" />
          )}
          {t('feedback.search.semantic_submit')}
        </Button>
      </form>
      <div className="flex flex-wrap items-center gap-2">
        <QuickScopeButton
          active={!urgentOnly}
          onClick={() => urgentOnly && onUrgentToggle()}
          label={t('feedback.filter.quick.all')}
        />
        <QuickScopeButton
          active={urgentOnly}
          onClick={onUrgentToggle}
          label={t('feedback.filter.quick.urgent')}
        />
      </div>
      <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-5">
        {dims
          .filter((d) => d.kind === 'single' && d.taxonomy.length > 0)
          .map((d) => (
            <Select
              key={d.name}
              value={attrFilters[d.name] || '__all'}
              onValueChange={(v) => onAttrChange(d.name, v)}
            >
              <SelectTrigger
                className="h-10 w-full bg-background"
                aria-label={t('feedback.filter.all', {
                  dim: displayOf(d.displayName) || d.name,
                })}
              >
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
            <SelectTrigger
              className="h-10 w-full bg-background"
              aria-label={t('feedback.filter.all_tags')}
            >
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
            <SelectTrigger
              className="h-10 w-full bg-background"
              aria-label={t('feedback.filter.all_states')}
            >
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
                    {displayOf(s.displayName) || s.name}
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        <Select
          value={enrichmentFilter || '__all'}
          onValueChange={(v) => onEnrichmentChange(v === '__all' ? '' : v)}
        >
          <SelectTrigger
            className="h-10 w-full bg-background"
            aria-label={t('feedback.filter.enrichment_status')}
          >
            <SelectValue placeholder={t('feedback.filter.enrichment_status')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all">{t('feedback.filter.all_enrichment')}</SelectItem>
            <SelectItem value="pending">
              <span className="flex items-center gap-2">
                <span className="inline-block size-2 rounded-full bg-slate-400" />
                {t('feedback.status.pending')}
              </span>
            </SelectItem>
            <SelectItem value="enriching">
              <span className="flex items-center gap-2">
                <span className="inline-block size-2 rounded-full bg-blue-500" />
                {t('feedback.status.enriching')}
              </span>
            </SelectItem>
            <SelectItem value="done">
              <span className="flex items-center gap-2">
                <span className="inline-block size-2 rounded-full bg-emerald-500" />
                {t('feedback.status.done')}
              </span>
            </SelectItem>
            <SelectItem value="failed">
              <span className="flex items-center gap-2">
                <span className="inline-block size-2 rounded-full bg-destructive" />
                {t('feedback.status.failed')}
              </span>
            </SelectItem>
          </SelectContent>
        </Select>
      </div>
    </div>
  )
}

function SemanticSearchStatus({
  response,
  query,
  isStale,
  isLoading,
  errorMessage,
  onSearch,
}: {
  response: SemanticSearchResponse | null
  query: string
  isStale: boolean
  isLoading: boolean
  errorMessage: string
  onSearch: () => void
}) {
  const { t } = useTranslation()

  if (!query) {
    return (
      <div className="rounded-[0.9rem] border border-dashed border-border/60 bg-muted/[0.05] px-3.5 py-3 text-sm text-muted-foreground">
        {t('feedback.search.semantic_idle')}
      </div>
    )
  }

  if (errorMessage) {
    return (
      <div className="flex flex-col gap-2 rounded-[0.9rem] border border-destructive/25 bg-destructive/[0.04] px-3.5 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex min-w-0 items-center gap-2 text-sm text-destructive">
          <AlertCircle className="h-4 w-4 shrink-0" />
          <span className="truncate">{errorMessage}</span>
        </div>
        <Button type="button" size="sm" variant="outline" onClick={onSearch} disabled={isLoading}>
          {t('common.retry')}
        </Button>
      </div>
    )
  }

  if (isLoading) {
    return (
      <div className="inline-flex items-center gap-2 rounded-[0.9rem] border border-border/60 bg-background/86 px-3.5 py-3 text-sm text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" />
        {t('feedback.search.semantic_loading')}
      </div>
    )
  }

  if (isStale) {
    return (
      <div className="flex flex-col gap-2 rounded-[0.9rem] border border-amber-200/75 bg-amber-50/55 px-3.5 py-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="text-sm text-amber-900">{t('feedback.search.semantic_stale')}</div>
        <Button type="button" size="sm" variant="outline" onClick={onSearch}>
          <Sparkles className="h-3.5 w-3.5" />
          {t('feedback.search.semantic_submit')}
        </Button>
      </div>
    )
  }

  if (!response) {
    return (
      <div className="rounded-[0.9rem] border border-dashed border-border/60 bg-muted/[0.05] px-3.5 py-3 text-sm text-muted-foreground">
        {t('feedback.search.semantic_ready')}
      </div>
    )
  }

  const embedded = response.coverage?.totalWithEmbeddings ?? response.totalWithEmbeddings
  const totalLive = response.coverage?.totalLiveFeedback ?? 0
  const fallbackReason = response.fallbackReason
    ? t(`feedback.search.fallback_reason.${response.fallbackReason}`, {
        defaultValue: response.fallbackReason,
      })
    : ''

  return (
    <div className="flex flex-wrap items-center gap-2 rounded-[0.9rem] border border-sky-200/70 bg-sky-50/50 px-3.5 py-3 text-sm text-sky-950">
      <Sparkles className="h-4 w-4" />
      <span>
        {t('feedback.search.semantic_results', {
          count: response.hits.length,
          embedded,
        })}
      </span>
      {totalLive > 0 ? (
        <StatusPill
          tone="muted"
          label={t('feedback.search.semantic_coverage', { embedded, total: totalLive })}
        />
      ) : null}
      {response.usedKeywordFallback ? (
        <StatusPill tone="muted" label={t('feedback.search.keyword_fallback')} />
      ) : response.embeddingModel ? (
        <StatusPill tone="muted" label={response.embeddingModel} />
      ) : null}
      {response.rankingVersion ? (
        <StatusPill
          tone="muted"
          label={t('feedback.search.ranking_version', { version: response.rankingVersion })}
        />
      ) : null}
      {fallbackReason ? <StatusPill tone="muted" label={fallbackReason} /> : null}
    </div>
  )
}

function SemanticFallbackBanner({ reason }: { reason?: string }) {
  const { t } = useTranslation()
  const reasonLabel = reason
    ? t(`feedback.search.fallback_reason.${reason}`, { defaultValue: reason })
    : ''
  return (
    <div className="mb-3 rounded-[1rem] border border-amber-200/75 bg-amber-50/60 px-4 py-3 text-sm text-amber-950">
      <div className="flex items-start gap-2">
        <AlertCircle className="mt-0.5 h-4 w-4 shrink-0" />
        <div>
          <div className="font-medium">{t('feedback.search.keyword_fallback_title')}</div>
          <p className="mt-1 text-xs leading-5 text-amber-900">
            {t('feedback.search.keyword_fallback_body')}
          </p>
          {reasonLabel ? (
            <div className="mt-2 text-[11px] font-medium text-amber-950">{reasonLabel}</div>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function SemanticSearchEmptyState({
  onSearch,
  onReset,
}: {
  onSearch: () => void
  onReset: () => void
}) {
  const { t } = useTranslation()
  return (
    <div className="px-5 py-12 text-center">
      <div className="mx-auto flex h-12 w-12 items-center justify-center rounded-full border border-border/60 bg-muted/20">
        <Sparkles className="h-5 w-5 text-muted-foreground" />
      </div>
      <h3 className="mt-4 text-base font-semibold">{t('feedback.search.no_results_title')}</h3>
      <p className="mx-auto mt-2 max-w-md text-sm leading-6 text-muted-foreground">
        {t('feedback.search.no_results_body')}
      </p>
      <div className="mt-4 flex flex-wrap justify-center gap-2">
        <Button type="button" variant="outline" onClick={onSearch}>
          <Sparkles className="h-4 w-4" />
          {t('feedback.search.semantic_submit')}
        </Button>
        <Button type="button" variant="ghost" onClick={onReset}>
          {t('feedback.clear_filters')}
        </Button>
      </div>
    </div>
  )
}

function QuickScopeButton({
  active,
  onClick,
  label,
}: {
  active: boolean
  onClick: () => void
  label: string
}) {
  return (
    <Button
      type="button"
      variant={active ? 'default' : 'outline'}
      className="h-8 rounded-full px-3 text-xs"
      aria-pressed={active}
      onClick={onClick}
    >
      {label}
    </Button>
  )
}

function SearchMatchBadge({ meta }: { meta: SearchHitMeta }) {
  const { t } = useTranslation()
  const score = meta.matchType === 'keyword' ? meta.keywordScore : meta.similarity
  const percentage = Math.max(0, Math.min(100, Math.round(score * 100)))
  const matchType =
    meta.matchType === 'keyword' || meta.matchType === 'hybrid' || meta.matchType === 'semantic'
      ? meta.matchType
      : 'semantic'
  const title = t('feedback.search.rank_title', {
    matchType: t(`feedback.search.match_type.${matchType}`),
    semanticRank: meta.semanticRank || '-',
    lexicalRank: meta.lexicalRank || '-',
    fusedScore: meta.fusedScore.toFixed(4),
  })

  return (
    <span
      className="inline-flex items-center gap-1 rounded-full border border-sky-200/75 bg-sky-50/70 px-1.5 py-0.5 font-medium text-sky-700"
      title={title}
    >
      <Sparkles className="h-3 w-3" />
      {t(`feedback.search.match_type.${matchType}`)} {percentage}%
    </span>
  )
}

function SearchEvidenceLine({ meta }: { meta: SearchHitMeta }) {
  const { t } = useTranslation()
  const evidence = meta.evidence.find((item) => item.snippet)
  if (!evidence) {
    return null
  }
  return (
    <div className="mt-2 flex min-w-0 items-start gap-1.5 rounded-lg border border-sky-100 bg-sky-50/45 px-2 py-1.5 text-[11.5px] leading-5 text-sky-900">
      <Sparkles className="mt-0.5 h-3 w-3 shrink-0" />
      <span className="shrink-0 font-medium">{t('feedback.search.evidence_label')}</span>
      <span className="min-w-0 line-clamp-2 text-sky-900/85">{evidence.snippet}</span>
    </div>
  )
}

function FeedbackTable({
  items,
  dims,
  allTags,
  searchMetaById,
  selected,
  onToggle,
  onRowClick,
}: {
  items: Feedback[]
  dims: Dimension[]
  allTags: Tag[]
  searchMetaById?: Map<string, SearchHitMeta>
  selected: Set<string>
  onToggle: (id: string) => void
  onRowClick: (id: string, restoreFocusTo: HTMLElement) => void
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
      <div className="space-y-3 px-3 py-3 sm:px-4">
        {items.map((f) => {
          const title = f.enrichedDisplayTitle || f.enrichedTitle || `#${f.id}`
          const isSelected = selected.has(f.id)
          const rowTags = f.tags ?? []
          const searchMeta = searchMetaById?.get(f.id)
          const workflowCategory = f.workflowState?.category ?? ''
          const filledDims = dims.filter((d) => {
            const value = (f.enrichedAttrs as Record<string, unknown> | undefined)?.[d.name]
            if (Array.isArray(value)) return value.length > 0
            return typeof value === 'string' ? value.length > 0 : value != null
          })
          const terminal = isTerminalFailure(f)
          const rowTone = toneForFeedbackRow(f.isUrgent, workflowCategory, isSelected, terminal)
          return (
            <div
              key={f.id}
              className={`grid gap-3 rounded-[1rem] border px-3.5 py-3.5 shadow-[0_18px_40px_-34px_rgba(15,23,42,0.18)] transition-[border-color,background-color,box-shadow,transform] hover:-translate-y-[1px] hover:border-border/85 hover:shadow-[0_22px_46px_-34px_rgba(15,23,42,0.22)] sm:px-4 lg:grid-cols-[1.75rem_minmax(0,1.15fr)_14rem] ${rowTone}`}
            >
              <div className="flex items-start justify-center pt-1">
                <Checkbox
                  checked={isSelected}
                  onCheckedChange={() => onToggle(f.id)}
                  aria-label={t('feedback.table.select_row', { title })}
                />
              </div>
              <button
                type="button"
                className="flex min-h-full min-w-0 flex-col justify-center text-left"
                onClick={(event) => onRowClick(f.id, event.currentTarget)}
              >
                <div className="mb-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-[10.5px] text-muted-foreground">
                  <span
                    className={`inline-flex rounded-full px-1.5 py-0.5 font-medium tracking-[0.04em] ${eyebrowToneForFeedbackRow(f.isUrgent, workflowCategory, terminal)}`}
                  >
                    {labelForFeedbackRow(f.isUrgent, workflowCategory, terminal, t)}
                  </span>
                  <span>#{f.id}</span>
                  {searchMeta ? <SearchMatchBadge meta={searchMeta} /> : null}
                  <span className="text-muted-foreground/50">/</span>
                  <span>
                    {formatDistanceToNow(new Date(f.createdAt), { addSuffix: true, locale: zhCN })}
                  </span>
                </div>
                <div className="flex min-w-0 items-start gap-3">
                  <div className="pt-1">
                    <UrgentDot urgent={f.isUrgent} />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-base font-semibold leading-6 tracking-tight text-foreground">
                      {title}
                    </div>
                    <div className="mt-1.5 line-clamp-2 max-w-3xl text-[13px] leading-[1.4rem] text-muted-foreground">
                      {f.content}
                    </div>
                    {searchMeta ? <SearchEvidenceLine meta={searchMeta} /> : null}
                  </div>
                </div>
                <div className="mt-3 flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                  {f.source ? <StatusMetaChip value={f.source} /> : null}
                  <StatusMetaChip valueNode={<LanguageBadge language={f.language} />} />
                  {filledDims.length === 0 ? (
                    <StatusMetaChip value={t('feedback.row.unclassified_short')} />
                  ) : null}
                  {rowTags.slice(0, 3).map((tag) => (
                    <TagBadgeTooltip key={tag.id} tag={tagLookup.get(tag.id) ?? tag} />
                  ))}
                  {rowTags.length > 3 ? <StatusMetaChip value={`+${rowTags.length - 3}`} /> : null}
                </div>
              </button>
              <aside className="min-w-0 border-t border-border/45 pt-3 lg:border-t-0 lg:border-l lg:pl-3.5 lg:pt-0">
                <div className="flex flex-col gap-2 rounded-[0.9rem] border border-border/65 bg-[linear-gradient(180deg,rgba(250,250,250,0.96),rgba(255,255,255,0.98))] p-2.5 shadow-[0_14px_28px_-30px_rgba(15,23,42,0.18)]">
                  <div className="flex flex-wrap items-center gap-1.5">
                    {f.workflowState ? (
                      <WorkflowStateBadge state={f.workflowState} />
                    ) : (
                      <StatusPill
                        tone={statusToneForFeedback(f)}
                        label={statusLabelForFeedback(f, t)}
                      />
                    )}
                    <StatusPill
                      tone={classificationToneForFeedback(f)}
                      label={classificationLabelForFeedback(f, t)}
                    />
                    {f.enrichmentStatus === 'failed' && (f.enrichmentAttempts ?? 0) > 0 && (
                      <EnrichmentAttemptsBadge
                        attempts={f.enrichmentAttempts ?? 0}
                        isTerminal={isTerminalFailure(f)}
                        nextRetryAt={f.enrichmentNextRetryAt}
                      />
                    )}
                  </div>
                  {filledDims.length > 0 ? (
                    <dl className="grid gap-1.5">
                      {filledDims.slice(0, 2).map((d) => (
                        <div
                          key={d.name}
                          className="rounded-lg border border-border/45 bg-background/92 px-2.5 py-2"
                        >
                          <dt className="mb-1 text-[10px] uppercase tracking-[0.12em] text-muted-foreground">
                            {displayOf(d.displayName) || d.name}
                          </dt>
                          <dd className="min-w-0">
                            <DimensionChips
                              dim={d}
                              value={
                                (f.enrichedAttrs as Record<string, unknown> | undefined)?.[d.name]
                              }
                              className="gap-1 text-xs"
                            />
                          </dd>
                        </div>
                      ))}
                    </dl>
                  ) : (
                    <div className="rounded-lg border border-dashed border-border/55 bg-background/88 px-2.5 py-2 text-xs text-muted-foreground">
                      {t('feedback.row.no_classification_short')}
                    </div>
                  )}
                  {filledDims.length > 2 ? (
                    <div className="text-[11px] text-muted-foreground">
                      +{filledDims.length - 2} {t('feedback.queue.sources')}
                    </div>
                  ) : null}
                </div>
              </aside>
            </div>
          )
        })}
      </div>
    </div>
  )
}

function toneForFeedbackRow(
  urgent: boolean | undefined,
  workflowCategory: string,
  isSelected: boolean,
  isTerminal: boolean,
) {
  if (urgent) {
    return isSelected
      ? 'border-red-200/90 bg-[linear-gradient(135deg,rgba(254,242,242,0.98),rgba(255,255,255,0.995))]'
      : 'border-red-200/85 bg-[linear-gradient(135deg,rgba(254,242,242,0.82),rgba(255,255,255,0.995))]'
  }
  if (isTerminal) {
    return isSelected
      ? 'border-l-4 border-l-destructive border-y-destructive/30 border-r-destructive/30 bg-[linear-gradient(135deg,rgba(254,242,242,0.92),rgba(255,255,255,0.995))]'
      : 'border-l-4 border-l-destructive/70 border-y-destructive/20 border-r-destructive/20 bg-[linear-gradient(135deg,rgba(254,242,242,0.75),rgba(255,255,255,0.995))]'
  }
  if (workflowCategory === 'active') {
    return isSelected
      ? 'border-amber-200/90 bg-[linear-gradient(135deg,rgba(255,251,235,0.98),rgba(255,255,255,0.995))]'
      : 'border-amber-200/85 bg-[linear-gradient(135deg,rgba(255,251,235,0.86),rgba(255,255,255,0.995))]'
  }
  if (workflowCategory === 'closed') {
    return isSelected
      ? 'border-emerald-200/90 bg-[linear-gradient(135deg,rgba(236,253,245,0.98),rgba(255,255,255,0.995))]'
      : 'border-emerald-200/85 bg-[linear-gradient(135deg,rgba(236,253,245,0.84),rgba(255,255,255,0.995))]'
  }
  return isSelected
    ? 'border-slate-200/90 bg-[linear-gradient(135deg,rgba(248,250,252,0.99),rgba(255,255,255,0.995))]'
    : 'border-border/70 bg-[linear-gradient(180deg,rgba(249,250,251,0.96),rgba(255,255,255,0.995))]'
}

function eyebrowToneForFeedbackRow(
  urgent: boolean | undefined,
  workflowCategory: string,
  isTerminal: boolean,
) {
  if (urgent) return 'bg-red-100 text-red-700'
  if (isTerminal) return 'bg-destructive/10 text-destructive'
  if (workflowCategory === 'active') return 'bg-amber-100 text-amber-700'
  if (workflowCategory === 'closed') return 'bg-emerald-100 text-emerald-700'
  return 'bg-muted text-muted-foreground'
}

function labelForFeedbackRow(
  urgent: boolean | undefined,
  workflowCategory: string,
  isTerminal: boolean,
  t: (key: string) => string,
) {
  if (urgent) return t('feedback.row.priority.urgent')
  if (isTerminal) return t('feedback.row.priority.terminal')
  if (workflowCategory === 'active') return t('feedback.row.priority.active')
  if (workflowCategory === 'closed') return t('feedback.row.priority.closed')
  return t('feedback.row.priority.new')
}

function statusToneForFeedback(f: Feedback) {
  if (f.isUrgent) return 'urgent' as const
  if (f.workflowState?.category === 'active') return 'active' as const
  if (f.workflowState?.category === 'closed') return 'closed' as const
  return 'default' as const
}

function statusLabelForFeedback(f: Feedback, t: (key: string) => string) {
  if (f.isUrgent) return t('feedback.row.priority.urgent')
  if (f.workflowState?.category === 'active') return t('feedback.row.priority.active')
  if (f.workflowState?.category === 'closed') return t('feedback.row.priority.closed')
  return t('feedback.row.priority.new')
}

function classificationToneForFeedback(f: Feedback) {
  if (f.enrichmentStatus === 'failed') return 'error' as const
  if (isFeedbackReadyForTriage(f)) return 'success' as const
  if (f.enrichmentStatus === 'enriching' || f.enrichmentStatus === 'pending')
    return 'muted' as const
  return 'muted' as const
}

function classificationLabelForFeedback(f: Feedback, t: (key: string) => string) {
  if (f.enrichmentStatus === 'failed') return t('feedback.row.classification_failed')
  if (isFeedbackReadyForTriage(f)) {
    return t('feedback.row.classification_ready')
  }
  if (f.enrichmentStatus === 'enriching') return t('feedback.row.classification_enriching')
  if (f.enrichmentStatus === 'pending') return t('feedback.row.classification_pending')
  return t('feedback.row.unclassified_short')
}

function hasFilledClassificationAttrs(f: Feedback) {
  const attrs = (f.enrichedAttrs ?? {}) as Record<string, unknown>
  return Object.values(attrs).some((value) => {
    if (Array.isArray(value)) return value.length > 0
    return typeof value === 'string' ? value.length > 0 : value != null
  })
}

function hasUsableAiTitle(f: Feedback) {
  return Boolean(f.enrichedDisplayTitle || f.enrichedTitle)
}

function isFeedbackReadyForTriage(f: Feedback) {
  if (f.enrichmentStatus === 'failed') return false
  if (f.enrichmentStatus === 'pending' || f.enrichmentStatus === 'enriching') return false
  return (
    hasUsableAiTitle(f) || f.classificationConfidence != null || hasFilledClassificationAttrs(f)
  )
}

function FeedbackStat({
  label,
  value,
  tone = 'default',
  hint,
}: {
  label: string
  value: string
  tone?: 'default' | 'urgent' | 'active' | 'terminal'
  hint?: string
}) {
  const toneClass =
    tone === 'terminal'
      ? 'border-red-300/55 bg-red-50/85 text-red-900'
      : tone === 'urgent'
        ? 'border-amber-300/55 bg-amber-50/85 text-amber-900'
        : tone === 'active'
          ? 'border-emerald-300/55 bg-emerald-50/85 text-emerald-900'
          : 'border-border/60 bg-background/88 text-foreground'
  return (
    <div
      className={`rounded-[1.15rem] border px-4 py-3.5 shadow-[0_18px_38px_-34px_rgba(15,23,42,0.24)] ${toneClass}`}
    >
      <div className="text-[11px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
        {label}
      </div>
      <div className="mt-1 text-[1.55rem] font-semibold tracking-tight [font-variant-numeric:tabular-nums]">
        {value}
      </div>
      {hint ? <div className="mt-1 text-xs leading-5 text-muted-foreground">{hint}</div> : null}
    </div>
  )
}

function WorkbenchFact({ label, value }: { label: string; value: string }) {
  return (
    <div className="inline-flex items-center gap-2 rounded-full border border-border/60 bg-background/78 px-3 py-1.5 text-xs">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium text-foreground">{value}</span>
    </div>
  )
}

function QueueLaneBanner({
  queueMode,
  urgentOnly,
  visibleCount,
  urgentCount,
  failedCount,
  priorityItem,
  onEnableUrgentScope,
  onOpenPriority,
}: {
  queueMode: FeedbackQueueMode
  urgentOnly: boolean
  visibleCount: number
  urgentCount: number
  failedCount: number
  priorityItem: Feedback | null
  onEnableUrgentScope: () => void
  onOpenPriority: (id: string) => void
}) {
  const { t } = useTranslation()

  let title = t('feedback.lane.default_title')
  let body = t('feedback.lane.default_body')
  let actionLabel = priorityItem ? t('feedback.lane.default_action') : ''
  let action: (() => void) | null = priorityItem ? () => onOpenPriority(priorityItem.id) : null
  let secondaryActionLabel = ''
  let secondaryAction: (() => void) | null = null
  let toneClass = 'border-border/60 bg-muted/[0.06]'

  if (visibleCount === 0) {
    title = t('feedback.lane.empty_title')
    body = t('feedback.lane.empty_body')
    actionLabel = ''
    action = null
    secondaryActionLabel = ''
    secondaryAction = null
    toneClass = 'border-border/60 bg-slate-50/70'
  } else if (queueMode === 'ready') {
    title = t('feedback.lane.ready_title')
    body = t('feedback.lane.ready_body')
    actionLabel = priorityItem ? t('feedback.lane.ready_action') : ''
    action = priorityItem ? () => onOpenPriority(priorityItem.id) : null
    toneClass = 'border-emerald-200/80 bg-emerald-50/60'
  } else if (queueMode === 'active') {
    title = t('feedback.lane.active_title')
    body = t('feedback.lane.active_body')
    actionLabel = priorityItem ? t('feedback.lane.active_action') : ''
    action = priorityItem ? () => onOpenPriority(priorityItem.id) : null
    toneClass = 'border-amber-200/80 bg-amber-50/65'
  } else if (queueMode === 'failed') {
    title = t('feedback.lane.failure_title')
    body = t('feedback.lane.failure_body')
    actionLabel = priorityItem ? t('feedback.lane.failure_action') : ''
    action = priorityItem ? () => onOpenPriority(priorityItem.id) : null
    toneClass = 'border-orange-200/80 bg-orange-50/60'
  } else if (queueMode === 'urgent') {
    title = t('feedback.lane.urgent_focus_title')
    body = t('feedback.lane.urgent_focus_body')
    actionLabel = priorityItem ? t('feedback.lane.urgent_focus_action') : ''
    action = priorityItem ? () => onOpenPriority(priorityItem.id) : null
    toneClass = 'border-red-200/80 bg-red-50/55'
  } else if (!urgentOnly && urgentCount > 0) {
    title = t('feedback.lane.urgent_title')
    body = t('feedback.lane.urgent_body')
    actionLabel = t('feedback.lane.urgent_action')
    action = onEnableUrgentScope
    secondaryActionLabel = priorityItem ? t('feedback.lane.urgent_secondary_action') : ''
    secondaryAction = priorityItem ? () => onOpenPriority(priorityItem.id) : null
    toneClass = 'border-amber-200/80 bg-amber-50/65'
  } else if (failedCount > 0) {
    title = t('feedback.lane.failure_title')
    body = t('feedback.lane.failure_body')
    actionLabel = priorityItem ? t('feedback.lane.failure_action') : ''
    action = priorityItem ? () => onOpenPriority(priorityItem.id) : null
    secondaryActionLabel = ''
    secondaryAction = null
    toneClass = 'border-orange-200/80 bg-orange-50/60'
  }

  return (
    <div className={`rounded-[1.1rem] border px-4 py-3 ${toneClass}`}>
      <div className="flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between">
        <div className="min-w-0">
          <div className="text-[10px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
            {t('feedback.lane.eyebrow')}
          </div>
          <div className="mt-1 text-sm font-semibold text-foreground">{title}</div>
          <p className="mt-1 line-clamp-2 max-w-2xl text-[13px] leading-[1.35rem] text-muted-foreground">
            {body}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {action ? (
            <Button type="button" size="sm" className="shrink-0" onClick={action}>
              <ArrowRight className="h-3.5 w-3.5" />
              {actionLabel}
            </Button>
          ) : null}
          {secondaryAction ? (
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="shrink-0"
              onClick={secondaryAction}
            >
              {secondaryActionLabel}
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function QueueHeaderSummaryStrip({
  queueMode,
  visibleCount,
  readyCount,
  failedCount,
  terminalCount,
  pendingAiCount,
  oldestVisibleAt,
  priorityItem,
}: {
  queueMode: FeedbackQueueMode
  visibleCount: number
  readyCount: number
  failedCount: number
  terminalCount: number
  pendingAiCount: number
  oldestVisibleAt: string | null
  priorityItem: Feedback | null
}) {
  const { t } = useTranslation()

  const aiSummary =
    terminalCount > 0
      ? {
          label: t('feedback.queue_mode.terminal'),
          value: String(terminalCount),
          tone: 'danger' as const,
        }
      : failedCount > 0
        ? {
            label: t('feedback.queue.failed'),
            value: String(failedCount),
            tone: 'danger' as const,
          }
        : readyCount > 0
          ? {
              label: t('feedback.queue.ready'),
              value: String(readyCount),
              tone: 'success' as const,
            }
          : pendingAiCount > 0
            ? {
                label: t('feedback.queue.pending'),
                value: String(pendingAiCount),
                tone: 'warning' as const,
              }
            : {
                label: t('feedback.queue.visible'),
                value: String(visibleCount),
                tone: 'default' as const,
              }

  return (
    <div className="flex flex-wrap items-center gap-2.5">
      <HeaderSummaryPill
        eyebrow={t('feedback.queue.visible')}
        title={String(visibleCount)}
        body={`${t('feedback.workbench_mode')} · ${queueModeLabel(queueMode, t)}`}
      />
      <HeaderSummaryPill
        eyebrow={t('feedback.detail.ai_state')}
        title={`${aiSummary.label} ${aiSummary.value}`}
        body={
          oldestVisibleAt
            ? `${t('feedback.queue.oldest')} ${formatDistanceToNow(new Date(oldestVisibleAt), {
                addSuffix: true,
                locale: zhCN,
              })}`
            : t('feedback.queue.visible_hint')
        }
        tone={aiSummary.tone}
      />
      <HeaderSummaryPill
        eyebrow={t('feedback.queue.actions.title')}
        title={
          priorityItem
            ? t('feedback.queue.priority_action_with_id', { id: priorityItem.id })
            : t('feedback.queue.priority_action')
        }
        body={
          priorityItem
            ? t('feedback.queue.feedback_id', { id: priorityItem.id })
            : t('feedback.focus_items.selection_idle')
        }
      />
    </div>
  )
}

function QueueModeBar({
  mode,
  urgentCount,
  activeCount,
  failedCount,
  terminalCount,
  readyCount,
  selectedCount,
  isAllSelected,
  onToggleAll,
  variant = 'card',
  onChange,
}: {
  mode: FeedbackQueueMode
  urgentCount: number
  activeCount: number
  failedCount: number
  terminalCount: number
  readyCount: number
  selectedCount: number
  isAllSelected: boolean
  onToggleAll: () => void
  variant?: 'card' | 'embedded'
  onChange: (mode: FeedbackQueueMode) => void
}) {
  const { t } = useTranslation()
  const shellClass =
    variant === 'embedded'
      ? 'px-0 py-0 shadow-none border-0 bg-transparent rounded-none'
      : 'rounded-[1rem] border border-border/60 bg-[linear-gradient(180deg,rgba(255,255,255,0.98),rgba(248,250,252,0.96))] px-3 py-3 shadow-[0_18px_42px_-46px_rgba(15,23,42,0.28)]'

  return (
    <div className={shellClass}>
      {variant === 'embedded' ? (
        <div className="mb-2 flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex flex-wrap items-center gap-2">
            <div className="inline-flex items-center gap-2 rounded-full border border-border/60 bg-background/92 px-2.5 py-1 text-[11px] text-muted-foreground">
              <Checkbox
                checked={isAllSelected}
                onCheckedChange={onToggleAll}
                aria-label={t('feedback.table.select_all')}
              />
              <span>{t('feedback.table.select_all')}</span>
            </div>
            <div className="rounded-full border border-border/60 bg-background/90 px-2.5 py-1 text-[11px] text-muted-foreground">
              {queueModeLabel(mode, t)}
            </div>
          </div>
          {selectedCount > 0 ? (
            <StatusMetaChip
              value={t('feedback.focus_items.selection_active', { count: selectedCount })}
            />
          ) : null}
        </div>
      ) : (
        <div className="mb-2 flex flex-col gap-2 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex items-center gap-3">
            <div className="text-[10px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
              {t('feedback.workbench_mode')}
            </div>
            <div className="inline-flex items-center gap-2 rounded-full border border-border/60 bg-background/92 px-2.5 py-1 text-[11px] text-muted-foreground">
              <Checkbox
                checked={isAllSelected}
                onCheckedChange={onToggleAll}
                aria-label={t('feedback.table.select_all')}
              />
              <span>{t('feedback.table.select_all')}</span>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {selectedCount > 0 ? (
              <StatusMetaChip
                value={t('feedback.focus_items.selection_active', { count: selectedCount })}
              />
            ) : null}
            <div className="rounded-full border border-border/60 bg-background/90 px-2.5 py-1 text-[11px] text-muted-foreground">
              {queueModeLabel(mode, t)}
            </div>
          </div>
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <QueueModeChip
          active={mode === 'all'}
          mode="all"
          label={t('feedback.queue_mode.all')}
          count={null}
          onClick={() => onChange('all')}
        />
        <QueueModeChip
          active={mode === 'urgent'}
          mode="urgent"
          label={t('feedback.queue_mode.urgent')}
          count={urgentCount}
          onClick={() => onChange('urgent')}
        />
        <QueueModeChip
          active={mode === 'active'}
          mode="active"
          label={t('feedback.queue_mode.active')}
          count={activeCount}
          onClick={() => onChange('active')}
        />
        <QueueModeChip
          active={mode === 'failed'}
          mode="failed"
          label={t('feedback.queue_mode.failed')}
          count={failedCount}
          onClick={() => onChange('failed')}
        />
        <QueueModeChip
          active={mode === 'terminal'}
          mode="terminal"
          label={t('feedback.queue_mode.terminal')}
          count={terminalCount}
          onClick={() => onChange('terminal')}
        />
        <QueueModeChip
          active={mode === 'ready'}
          mode="ready"
          label={t('feedback.queue_mode.ready')}
          count={readyCount}
          onClick={() => onChange('ready')}
        />
      </div>
    </div>
  )
}

function QueueActionStrip({
  queueMode,
  visibleCount,
  urgentCount,
  failedCount,
  terminalCount,
  readyCount,
  pendingAiCount,
  priorityItem,
  terminalWorkbenchPriority,
  showTerminalWorkbench,
  hasActiveFilters,
  onOpenPriority,
  onClearFilters,
  variant = 'card',
}: {
  queueMode: FeedbackQueueMode
  visibleCount: number
  urgentCount: number
  failedCount: number
  terminalCount: number
  readyCount: number
  pendingAiCount: number
  priorityItem: Feedback | null
  terminalWorkbenchPriority: ReturnType<typeof selectTerminalFailurePriority> | null
  showTerminalWorkbench: boolean
  hasActiveFilters: boolean
  onOpenPriority: (id: string) => void
  onClearFilters: () => void
  variant?: 'card' | 'embedded'
}) {
  const { t } = useTranslation()
  const primaryActionLabel = queuePrimaryActionLabel(
    queueMode,
    urgentCount,
    failedCount,
    terminalCount,
    t,
  )
  const shouldShowConfigLink = shouldSurfaceRuntimeLink(
    queueMode,
    visibleCount,
    failedCount,
    readyCount,
    pendingAiCount,
  )
  const shouldShowWorkbenchLink =
    showTerminalWorkbench || queueMode === 'terminal' || terminalCount > 0
  const workbenchLink = showTerminalWorkbench
    ? { label: t('feedback.terminal_workbench.back_to_queue'), to: '/feedback' }
    : { label: t('feedback.terminal_workbench.open_workbench'), to: '/feedback/terminal-failures' }
  const shellClass =
    variant === 'embedded'
      ? 'flex h-full flex-col rounded-none border-0 bg-transparent px-0 py-0 shadow-none'
      : 'flex h-full flex-col rounded-[1rem] border border-border/60 bg-[linear-gradient(180deg,rgba(255,255,255,0.99),rgba(248,250,252,0.96))] px-4 py-3.5 shadow-[0_20px_48px_-50px_rgba(15,23,42,0.32)]'

  return (
    <div className={shellClass}>
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <div className="text-[10px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
            {t('feedback.queue.actions.title')}
          </div>
          <p className="mt-1 max-w-2xl text-[13px] leading-[1.45rem] text-muted-foreground">
            {priorityItem
              ? t('feedback.queue.actions.description')
              : t('feedback.queue.actions.empty')}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <StatusPill
            tone={priorityItem ? (queueMode === 'urgent' ? 'urgent' : 'active') : 'muted'}
            label={
              priorityItem
                ? `${t('feedback.queue.actions.title')} #${priorityItem.id}`
                : t('feedback.queue.actions.empty')
            }
          />
          {shouldShowWorkbenchLink ? (
            <Button
              asChild
              type="button"
              size="sm"
              variant="outline"
              className="h-8 rounded-full px-3 text-xs"
            >
              <Link to={workbenchLink.to}>
                {workbenchLink.label}
                <ArrowRight className="size-3.5" />
              </Link>
            </Button>
          ) : null}
        </div>
      </div>
      {terminalWorkbenchPriority ? (
        <div className="mt-3 rounded-[1rem] border border-amber-200/75 bg-amber-50/55 px-3 py-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0">
              <div className="inline-flex items-center gap-2 rounded-full border border-amber-300/70 bg-amber-100/80 px-2.5 py-1 text-[10px] font-semibold tracking-[0.14em] text-amber-800 uppercase">
                <TriangleAlert className="size-3.5" />
                {t('feedback.terminal_workbench.priority_title')}
              </div>
              <div className="mt-2 truncate text-sm font-semibold text-foreground">
                {terminalWorkbenchPriority.cluster.label}
              </div>
              <p className="mt-1 text-xs leading-5 text-muted-foreground text-pretty">
                {t('feedback.terminal_workbench.priority_scope', {
                  dimension: terminalWorkbenchPriority.sectionTitle,
                })}{' '}
                ·{' '}
                {t('feedback.terminal_workbench.count', {
                  count: Number(terminalWorkbenchPriority.cluster.count),
                })}
              </p>
              {terminalWorkbenchPriority.cluster.remediationHint ? (
                <p className="mt-2 text-xs leading-5 text-muted-foreground text-pretty">
                  {terminalWorkbenchPriority.cluster.remediationHint}
                </p>
              ) : null}
            </div>
            {terminalWorkbenchPriority.cluster.sampleFeedbackIds[0] ? (
              <div className="flex shrink-0 flex-wrap items-center gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant="secondary"
                  className="h-9 rounded-full px-3 text-xs"
                  onClick={() =>
                    onOpenPriority(String(terminalWorkbenchPriority.cluster.sampleFeedbackIds[0]))
                  }
                >
                  {t('feedback.terminal_workbench.open_sample_with_id', {
                    id: String(terminalWorkbenchPriority.cluster.sampleFeedbackIds[0]),
                  })}
                </Button>
              </div>
            ) : null}
            {terminalWorkbenchPriority.remediationPath &&
            terminalWorkbenchPriority.remediationLabel ? (
              <div className="flex shrink-0 flex-wrap items-center gap-2">
                <Button
                  asChild
                  type="button"
                  size="sm"
                  variant="outline"
                  className="h-9 rounded-full px-3 text-xs"
                >
                  <Link to={terminalWorkbenchPriority.remediationPath}>
                    {terminalWorkbenchPriority.remediationLabel}
                  </Link>
                </Button>
              </div>
            ) : null}
          </div>
        </div>
      ) : null}
      <div className="mt-auto pt-3">
        <div className="flex flex-wrap items-center gap-2.5">
          <Button
            type="button"
            size="sm"
            className="h-9"
            onClick={() => priorityItem && onOpenPriority(priorityItem.id)}
            disabled={!priorityItem}
          >
            <ArrowRight className="h-3.5 w-3.5" />
            {primaryActionLabel}
          </Button>
          {hasActiveFilters && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              className="h-9"
              onClick={onClearFilters}
            >
              {t('feedback.clear_filters')}
            </Button>
          )}
          {shouldShowConfigLink && (
            <Button asChild size="sm" variant="ghost" className="h-9">
              <Link to="/configuration/enrichment-runtime">
                {t('feedback.queue.actions.fix_runtime')}
              </Link>
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}

function QueueModeChip({
  active,
  mode,
  label,
  count,
  onClick,
}: {
  active: boolean
  mode: FeedbackQueueMode
  label: string
  count: null | number
  onClick: () => void
}) {
  const toneStyles: Record<FeedbackQueueMode, string> = {
    all: 'border-border/70 bg-background text-muted-foreground hover:bg-muted/25',
    urgent: 'border-red-200/80 bg-red-50/45 text-red-700 hover:bg-red-50/70',
    active: 'border-sky-200/80 bg-sky-50/55 text-sky-700 hover:bg-sky-50/80',
    failed: 'border-amber-200/80 bg-amber-50/55 text-amber-700 hover:bg-amber-50/80',
    terminal: 'border-red-300/80 bg-red-50/55 text-red-800 hover:bg-red-50/80',
    ready: 'border-emerald-200/80 bg-emerald-50/55 text-emerald-700 hover:bg-emerald-50/80',
  }
  const toneClass = active
    ? 'border-transparent shadow-[0_16px_32px_-24px_rgba(15,23,42,0.45)]'
    : toneStyles[mode]
  return (
    <Button
      type="button"
      size="sm"
      variant={active ? 'default' : 'outline'}
      className={`h-8 rounded-full px-3 text-xs transition-[transform,box-shadow,border-color,background-color] hover:-translate-y-[1px] ${toneClass}`}
      aria-pressed={active}
      onClick={onClick}
    >
      {label}
      {count != null ? (
        <span
          className={`rounded-full px-1.5 py-0.5 text-[10px] leading-none ${
            active ? 'bg-background/18 text-current' : 'bg-background/75 text-current'
          }`}
        >
          {count}
        </span>
      ) : null}
    </Button>
  )
}

function QueueWorkbenchStrip({
  queueMode,
  sortMode,
  activeFilterCount,
  nextStepLabel,
  variant = 'card',
}: {
  queueMode: FeedbackQueueMode
  sortMode: FeedbackSortMode
  activeFilterCount: number
  nextStepLabel: string
  variant?: 'card' | 'embedded'
}) {
  const { t } = useTranslation()
  const shellClass =
    variant === 'embedded'
      ? 'flex h-full flex-col gap-2.5 rounded-none border-0 bg-transparent px-0 py-0 shadow-none'
      : 'flex h-full flex-col gap-2.5 rounded-[1rem] border border-border/60 bg-[linear-gradient(180deg,rgba(255,255,255,0.99),rgba(248,250,252,0.96))] px-4 py-3 shadow-[0_20px_48px_-50px_rgba(15,23,42,0.32)]'

  return (
    <div className={shellClass}>
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0">
          <div className="text-[10px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
            {t('feedback.workbench_title')}
          </div>
          <p className="mt-1 max-w-2xl text-[13px] leading-[1.35rem] text-muted-foreground">
            {t('feedback.workbench_description')}
          </p>
        </div>
        <div className="rounded-full border border-border/60 bg-background/92 px-3 py-1.5 text-[11px] font-medium text-foreground">
          {t('feedback.workbench_next_step', { step: nextStepLabel })}
        </div>
      </div>
      <div className="mt-auto flex flex-wrap items-center gap-2">
        <ActiveChip
          label={t('feedback.workbench_mode')}
          value={queueModeLabel(queueMode, t)}
          tone={queueMode === 'urgent' ? 'urgent' : queueMode === 'all' ? 'default' : 'active'}
        />
        <ActiveChip
          label={t('feedback.workbench_sort')}
          value={sortModeLabel(sortMode, t)}
          tone={sortMode === 'newest' ? 'default' : 'active'}
        />
        <ActiveChip
          label={t('feedback.workbench_filters')}
          value={
            activeFilterCount > 0
              ? t('feedback.workbench_filters_active', { count: activeFilterCount })
              : t('feedback.workbench_filters_idle')
          }
          tone={activeFilterCount > 0 ? 'active' : 'default'}
        />
      </div>
    </div>
  )
}

function HeaderSummaryPill({
  eyebrow,
  title,
  body,
  tone = 'default',
}: {
  eyebrow: string
  title: string
  body: string
  tone?: 'default' | 'success' | 'danger' | 'warning'
}) {
  const toneClass =
    tone === 'success'
      ? 'border-emerald-200/75 bg-emerald-50/50'
      : tone === 'danger'
        ? 'border-amber-200/75 bg-amber-50/55'
        : tone === 'warning'
          ? 'border-sky-200/75 bg-sky-50/55'
          : 'border-border/60 bg-[linear-gradient(180deg,rgba(255,255,255,0.98),rgba(248,250,252,0.96))]'

  return (
    <div className={`min-w-[11rem] rounded-full border px-3 py-2.5 ${toneClass}`}>
      <div className="text-[10px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
        {eyebrow}
      </div>
      <div className="mt-1 truncate text-sm font-semibold text-foreground">{title}</div>
      <div className="mt-0.5 truncate text-xs leading-5 text-muted-foreground">{body}</div>
    </div>
  )
}

function QueueOperatorDeck({
  queueMode,
  sortMode,
  activeFilterCount,
  visibleCount,
  urgentCount,
  activeCount,
  failedCount,
  terminalCount,
  readyCount,
  pendingAiCount,
  priorityItem,
  hasActiveFilters,
  selectedCount,
  isAllSelected,
  onToggleAll,
  onOpenPriority,
  terminalWorkbenchPriority,
  showTerminalWorkbench,
  onClearFilters,
  onChangeQueueMode,
  nextStepLabel,
}: {
  queueMode: FeedbackQueueMode
  sortMode: FeedbackSortMode
  activeFilterCount: number
  visibleCount: number
  urgentCount: number
  activeCount: number
  failedCount: number
  terminalCount: number
  readyCount: number
  pendingAiCount: number
  priorityItem: Feedback | null
  hasActiveFilters: boolean
  selectedCount: number
  isAllSelected: boolean
  onToggleAll: () => void
  onOpenPriority: (id: string) => void
  terminalWorkbenchPriority: ReturnType<typeof selectTerminalFailurePriority> | null
  showTerminalWorkbench: boolean
  onClearFilters: () => void
  onChangeQueueMode: (mode: FeedbackQueueMode) => void
  nextStepLabel: string
}) {
  return (
    <div className="rounded-[1.05rem] border border-border/60 bg-[linear-gradient(180deg,rgba(255,255,255,0.99),rgba(248,250,252,0.96))] px-4 py-3 shadow-[0_20px_48px_-50px_rgba(15,23,42,0.24)]">
      <div className="grid gap-3 xl:grid-cols-[minmax(0,1.2fr)_minmax(19rem,0.9fr)]">
        <QueueWorkbenchStrip
          queueMode={queueMode}
          sortMode={sortMode}
          activeFilterCount={activeFilterCount}
          nextStepLabel={nextStepLabel}
          variant="embedded"
        />
        <div className="border-t border-border/45 pt-3 xl:border-t-0 xl:border-l xl:pl-4 xl:pt-0">
          <QueueActionStrip
            queueMode={queueMode}
            visibleCount={visibleCount}
            urgentCount={urgentCount}
            failedCount={failedCount}
            terminalCount={terminalCount}
            readyCount={readyCount}
            pendingAiCount={pendingAiCount}
            priorityItem={priorityItem}
            terminalWorkbenchPriority={terminalWorkbenchPriority}
            showTerminalWorkbench={showTerminalWorkbench}
            hasActiveFilters={hasActiveFilters}
            onOpenPriority={onOpenPriority}
            onClearFilters={onClearFilters}
            variant="embedded"
          />
        </div>
      </div>
      <div className="mt-3 border-t border-border/45 pt-3">
        <QueueModeBar
          mode={queueMode}
          urgentCount={urgentCount}
          activeCount={activeCount}
          failedCount={failedCount}
          terminalCount={terminalCount}
          readyCount={readyCount}
          selectedCount={selectedCount}
          isAllSelected={isAllSelected}
          onToggleAll={onToggleAll}
          onChange={onChangeQueueMode}
          variant="embedded"
        />
      </div>
    </div>
  )
}

function QueueRail({
  selectedCount,
  query,
  workflowLabel,
  queueMode,
  visibleCount,
  urgentCount,
  readyCount,
  failedCount,
  terminalCount,
  pendingAiCount,
  sourceCount,
  oldestVisibleAt,
}: {
  selectedCount: number
  query: string
  workflowLabel: string | null
  queueMode: FeedbackQueueMode
  visibleCount: number
  urgentCount: number
  readyCount: number
  failedCount: number
  terminalCount: number
  pendingAiCount: number
  sourceCount: number
  oldestVisibleAt: string | null
}) {
  const { t } = useTranslation()
  const primaryActionLabel = queuePrimaryActionLabel(
    queueMode,
    urgentCount,
    failedCount,
    terminalCount,
    t,
  )
  const railMetrics = buildQueueMetricItems({
    queueMode,
    visibleCount,
    urgentCount,
    readyCount,
    failedCount,
    pendingAiCount,
    sourceCount,
    oldestVisibleAt,
    t,
  })
    .filter((metric) => metric.label !== t('feedback.queue.visible'))
    .slice(0, 3)
  const surfaceTone =
    queueMode === 'urgent'
      ? 'border-red-200/75 bg-red-50/35'
      : queueMode === 'failed'
        ? 'border-amber-200/75 bg-amber-50/35'
        : queueMode === 'active'
          ? 'border-sky-200/75 bg-sky-50/35'
          : queueMode === 'ready'
            ? 'border-emerald-200/75 bg-emerald-50/35'
            : 'border-border/50 bg-background'

  return (
    <aside className="space-y-3.5 bg-[linear-gradient(180deg,rgba(248,250,252,0.55),rgba(255,255,255,0.12))] px-4 py-4 sm:px-5 xl:sticky xl:top-4 xl:self-start xl:px-4">
      <Card
        className={`rounded-[1rem] border-border/65 py-0 shadow-[0_22px_48px_-42px_rgba(15,23,42,0.2)] ${surfaceTone}`}
      >
        <CardContent className="space-y-3 px-4 py-4">
          <div>
            <div className="inline-flex items-center gap-2 text-[11px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
              <Target className="h-3.5 w-3.5" />
              {t('feedback.context_title')}
            </div>
            <p className="mt-2 text-sm leading-6 text-muted-foreground">
              {t('feedback.context_description')}
            </p>
          </div>
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-1">
            <RailSnapshotPill
              label={t('feedback.workbench_mode')}
              value={queueModeLabel(queueMode, t)}
            />
            <RailSnapshotPill label={t('feedback.queue.visible')} value={String(visibleCount)} />
          </div>
          <div className="grid gap-2">
            <RailFact
              label={t('feedback.queue.actions.title')}
              value={primaryActionLabel}
              tone="active"
            />
            <RailFact
              label={t('feedback.focus_items.selection')}
              value={
                selectedCount > 0
                  ? t('feedback.focus_items.selection_active', { count: selectedCount })
                  : t('feedback.focus_items.selection_idle')
              }
              tone={selectedCount > 0 ? 'active' : 'default'}
            />
            <RailFact
              label={t('feedback.focus_items.search')}
              value={query || t('feedback.focus_items.search_idle')}
              tone={query ? 'active' : 'default'}
            />
            <RailFact
              label={t('feedback.focus_items.workflow')}
              value={workflowLabel || t('feedback.focus_items.workflow_idle')}
              tone={workflowLabel ? 'active' : 'default'}
            />
          </div>
        </CardContent>
      </Card>

      <Card className="rounded-[1rem] border-border/60 py-0 shadow-[0_20px_44px_-42px_rgba(15,23,42,0.16)]">
        <CardContent className="space-y-4 px-4 py-4">
          <div className="flex items-start justify-between gap-3">
            <div>
              <div className="inline-flex items-center gap-2 text-[11px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
                <Sparkles className="h-3.5 w-3.5" />
                {t('feedback.queue.title')}
              </div>
              <p className="mt-2 text-sm leading-6 text-muted-foreground">
                {t('feedback.queue.description')}
              </p>
            </div>
            <TriangleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          </div>
          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-1">
            {railMetrics.map((metric) => (
              <QueueMetric
                key={metric.label}
                label={metric.label}
                value={metric.value}
                hint={metric.hint}
                tone={metric.tone}
                icon={metric.icon}
              />
            ))}
          </div>
          <div className="space-y-3 border-t border-border/45 pt-3">
            <div className="text-[11px] font-semibold tracking-[0.14em] text-muted-foreground uppercase">
              {t('feedback.queue.playbook_title')}
            </div>
            <PlaybookItem
              title={
                queueMode === 'urgent'
                  ? t('feedback.queue.playbook.urgent_title')
                  : queueMode === 'active'
                    ? t('feedback.queue.playbook.active_title')
                    : queueMode === 'ready'
                      ? t('feedback.queue.playbook.ready_title')
                      : urgentCount > 0
                        ? t('feedback.queue.playbook.urgent_title')
                        : t('feedback.queue.playbook.ready_title')
              }
              body={
                queueMode === 'urgent'
                  ? t('feedback.queue.playbook.urgent_body')
                  : queueMode === 'active'
                    ? t('feedback.queue.playbook.active_body')
                    : queueMode === 'ready'
                      ? t('feedback.queue.playbook.ready_body')
                      : urgentCount > 0
                        ? t('feedback.queue.playbook.urgent_body')
                        : t('feedback.queue.playbook.ready_body')
              }
            />
            <PlaybookItem
              title={
                queueMode === 'failed' || failedCount > 0
                  ? t('feedback.queue.playbook.failure_title')
                  : t('feedback.queue.playbook.pending_title')
              }
              body={
                queueMode === 'failed' || failedCount > 0
                  ? t('feedback.queue.playbook.failure_body')
                  : t('feedback.queue.playbook.pending_body')
              }
            />
          </div>
        </CardContent>
      </Card>
    </aside>
  )
}

function QueueMetric({
  label,
  value,
  hint,
  tone = 'default',
  icon,
}: {
  label: string
  value: string
  hint: string
  tone?: 'default' | 'success' | 'danger' | 'warning'
  icon?: ReactNode
}) {
  const toneClass =
    tone === 'success'
      ? 'border-emerald-200/70 bg-emerald-50/65'
      : tone === 'danger'
        ? 'border-amber-200/80 bg-amber-50/70'
        : tone === 'warning'
          ? 'border-sky-200/80 bg-sky-50/70'
          : 'border-border/60 bg-background'

  return (
    <div className={`rounded-xl border px-3.5 py-3 ${toneClass}`}>
      <div className="flex items-center justify-between gap-3">
        <div className="text-[11px] font-semibold tracking-[0.12em] text-muted-foreground uppercase">
          {label}
        </div>
        {icon ? <div className="text-muted-foreground">{icon}</div> : null}
      </div>
      <div className="mt-1.5 text-base font-semibold tracking-tight text-foreground">{value}</div>
      <p className="mt-1 text-xs leading-5 text-muted-foreground">{hint}</p>
    </div>
  )
}

function buildQueueMetricItems({
  queueMode,
  visibleCount,
  urgentCount,
  readyCount,
  failedCount,
  pendingAiCount,
  sourceCount,
  oldestVisibleAt,
  t,
}: {
  queueMode: FeedbackQueueMode
  visibleCount: number
  urgentCount: number
  readyCount: number
  failedCount: number
  pendingAiCount: number
  sourceCount: number
  oldestVisibleAt: string | null
  t: (key: string) => string
}) {
  const metrics: Array<{
    icon?: ReactNode
    hint: string
    label: string
    tone?: 'default' | 'success' | 'danger' | 'warning'
    value: string
  }> = [
    {
      icon: <Target className="h-3.5 w-3.5" />,
      hint: queuePostureHint(
        queueMode,
        visibleCount,
        urgentCount,
        failedCount,
        readyCount,
        pendingAiCount,
        t,
      ),
      label: t('feedback.queue.posture'),
      tone: queuePostureTone(
        queueMode,
        visibleCount,
        urgentCount,
        failedCount,
        readyCount,
        pendingAiCount,
      ),
      value: queuePostureLabel(
        queueMode,
        visibleCount,
        urgentCount,
        failedCount,
        readyCount,
        pendingAiCount,
        t,
      ),
    },
    {
      icon: <Sparkles className="h-3.5 w-3.5" />,
      hint: queueAiHealthHint(visibleCount, failedCount, readyCount, pendingAiCount, t),
      label: t('feedback.queue.ai_health'),
      tone: queueAiHealthTone(visibleCount, failedCount, readyCount, pendingAiCount),
      value: queueAiHealthLabel(visibleCount, failedCount, readyCount, pendingAiCount, t),
    },
    {
      hint: t('feedback.queue.visible_hint'),
      label: t('feedback.queue.visible'),
      value: String(visibleCount),
    },
  ]

  if (queueMode === 'failed') {
    metrics.push({
      hint: t('feedback.queue.failed_hint'),
      label: t('feedback.queue.failed'),
      tone: failedCount > 0 ? 'danger' : 'default',
      value: String(failedCount),
    })
  } else if (queueMode === 'ready') {
    metrics.push({
      hint: t('feedback.queue.ready_hint'),
      label: t('feedback.queue.ready'),
      tone: readyCount > 0 ? 'success' : 'default',
      value: String(readyCount),
    })
  } else if (queueMode === 'active') {
    metrics.push(
      {
        hint: t('feedback.queue.failed_hint'),
        label: t('feedback.queue.failed'),
        tone: failedCount > 0 ? 'danger' : 'default',
        value: String(failedCount),
      },
      {
        hint: t('feedback.queue.ready_hint'),
        label: t('feedback.queue.ready'),
        tone: readyCount > 0 ? 'success' : 'default',
        value: String(readyCount),
      },
    )
  } else if (queueMode === 'urgent') {
    metrics.push(
      {
        hint: t('feedback.queue.failed_hint'),
        label: t('feedback.queue.failed'),
        tone: failedCount > 0 ? 'danger' : 'default',
        value: String(failedCount),
      },
      {
        hint: t('feedback.queue.oldest_hint'),
        icon: <Clock3 className="h-3.5 w-3.5" />,
        label: t('feedback.queue.oldest'),
        value: oldestVisibleAt
          ? formatDistanceToNow(new Date(oldestVisibleAt), {
              addSuffix: true,
              locale: zhCN,
            })
          : '—',
      },
    )
  } else {
    metrics.push(
      {
        hint: t('feedback.queue.ready_hint'),
        label: t('feedback.queue.ready'),
        tone: readyCount > 0 ? 'success' : 'default',
        value: String(readyCount),
      },
      {
        hint: t('feedback.queue.failed_hint'),
        label: t('feedback.queue.failed'),
        tone: failedCount > 0 ? 'danger' : 'default',
        value: String(failedCount),
      },
      {
        hint: t('feedback.queue.pending_hint'),
        label: t('feedback.queue.pending'),
        tone: pendingAiCount > 0 ? 'warning' : 'default',
        value: String(pendingAiCount),
      },
    )
  }

  if (queueMode !== 'urgent' && sourceCount > 1) {
    metrics.push({
      hint: t('feedback.queue.sources_hint'),
      label: t('feedback.queue.sources'),
      value: String(sourceCount),
    })
  } else if (oldestVisibleAt) {
    metrics.push({
      hint: t('feedback.queue.oldest_hint'),
      icon: <Clock3 className="h-3.5 w-3.5" />,
      label: t('feedback.queue.oldest'),
      value: formatDistanceToNow(new Date(oldestVisibleAt), {
        addSuffix: true,
        locale: zhCN,
      }),
    })
  }

  return metrics
}

function RailFact({
  label,
  value,
  tone = 'default',
}: {
  label: string
  value: string
  tone?: 'default' | 'active'
}) {
  return (
    <div className="rounded-xl border border-border/60 bg-background px-3 py-2.5 shadow-[inset_0_1px_0_rgba(255,255,255,0.5)]">
      <div className="flex items-center gap-2 text-[11px] font-semibold tracking-[0.12em] text-muted-foreground uppercase">
        <span
          className={`size-1.5 rounded-full ${tone === 'active' ? 'bg-primary/70' : 'bg-border'}`}
        />
        {label}
      </div>
      <div
        className={`mt-1 text-sm leading-5 ${tone === 'active' ? 'text-foreground' : 'text-muted-foreground'}`}
      >
        {value}
      </div>
    </div>
  )
}

function RailSnapshotPill({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-border/60 bg-background/88 px-3 py-2.5 shadow-[inset_0_1px_0_rgba(255,255,255,0.55)]">
      <div className="text-[10px] font-semibold tracking-[0.12em] text-muted-foreground uppercase">
        {label}
      </div>
      <div className="mt-1 truncate text-sm font-medium text-foreground">{value}</div>
    </div>
  )
}

function PlaybookItem({ title, body }: { title: string; body: string }) {
  return (
    <div className="rounded-xl border border-border/50 bg-background px-3.5 py-3">
      <div className="text-sm font-medium text-foreground">{title}</div>
      <p className="mt-1 text-xs leading-[1.35rem] text-muted-foreground">{body}</p>
    </div>
  )
}

function FeedbackEmptyWorkspace({ title, description }: { title: string; description: string }) {
  const { t } = useTranslation()
  return (
    <div className="grid gap-5 px-5 py-8 sm:px-6 lg:grid-cols-[minmax(0,1.45fr)_minmax(18rem,0.9fr)] lg:py-10">
      <div className="rounded-[1.1rem] border border-border/55 bg-[radial-gradient(circle_at_top_left,rgba(255,247,237,0.88),rgba(255,255,255,0.98)_52%)] p-5 shadow-[0_24px_80px_-64px_rgba(194,65,12,0.35)]">
        <div className="flex items-start gap-4">
          <div className="flex size-12 shrink-0 items-center justify-center rounded-2xl border border-primary/15 bg-background/85 text-primary shadow-[0_18px_38px_-30px_rgba(194,65,12,0.45)]">
            <Inbox className="size-5" />
          </div>
          <div className="min-w-0">
            <h3 className="text-lg font-semibold tracking-tight text-foreground">{title}</h3>
            <p className="mt-2 max-w-xl text-sm leading-6 text-muted-foreground text-pretty">
              {description}
            </p>
          </div>
        </div>
        <div className="mt-5 grid gap-3 sm:grid-cols-2">
          <ActionLink
            to="/integrations/api-keys"
            title={t('feedback.empty_actions.api_key_title')}
            body={t('feedback.empty_actions.api_key_body')}
          />
          <ActionLink
            to="/configuration/classification"
            title={t('feedback.empty_actions.classification_title')}
            body={t('feedback.empty_actions.classification_body')}
          />
        </div>
      </div>
      <div className="rounded-[1.1rem] border border-border/55 bg-background p-5">
        <div className="text-[11px] font-semibold tracking-[0.18em] text-muted-foreground uppercase">
          {t('feedback.empty_actions.checklist_eyebrow')}
        </div>
        <div className="mt-3 space-y-3">
          <ChecklistRow
            index="01"
            title={t('feedback.empty_actions.step_ingest_title')}
            body={t('feedback.empty_actions.step_ingest_body')}
          />
          <ChecklistRow
            index="02"
            title={t('feedback.empty_actions.step_classify_title')}
            body={t('feedback.empty_actions.step_classify_body')}
          />
          <ChecklistRow
            index="03"
            title={t('feedback.empty_actions.step_notify_title')}
            body={t('feedback.empty_actions.step_notify_body')}
          />
        </div>
      </div>
    </div>
  )
}

function FeedbackFilteredEmptyState({ onReset }: { onReset: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="px-5 py-8 sm:px-6 lg:py-10">
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1.3fr)_minmax(18rem,0.7fr)]">
        <div className="rounded-[1.1rem] border border-border/55 bg-[linear-gradient(180deg,rgba(255,255,255,0.98),rgba(248,250,252,0.95))] p-5 shadow-[0_24px_80px_-68px_rgba(15,23,42,0.26)]">
          <div className="flex items-start gap-4">
            <div className="flex size-12 shrink-0 items-center justify-center rounded-2xl border border-border/60 bg-muted/12 text-foreground">
              <Search className="size-5" />
            </div>
            <div className="min-w-0">
              <h3 className="text-lg font-semibold tracking-tight text-foreground">
                {t('feedback.filtered_empty_title')}
              </h3>
              <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground text-pretty">
                {t('feedback.filtered_empty_body')}
              </p>
            </div>
          </div>
          <div className="mt-5 flex flex-wrap items-center gap-3">
            <Button onClick={onReset}>{t('feedback.clear_filters')}</Button>
            <div className="text-sm text-muted-foreground">{t('feedback.filtered_empty_hint')}</div>
          </div>
        </div>
        <div className="rounded-[1.1rem] border border-border/55 bg-background p-5">
          <div className="text-[11px] font-semibold tracking-[0.18em] text-muted-foreground uppercase">
            {t('feedback.filtered_empty_playbook_eyebrow')}
          </div>
          <div className="mt-3 space-y-3">
            <ChecklistRow
              index="01"
              title={t('feedback.filtered_empty_step_broaden_title')}
              body={t('feedback.filtered_empty_step_broaden_body')}
            />
            <ChecklistRow
              index="02"
              title={t('feedback.filtered_empty_step_recent_title')}
              body={t('feedback.filtered_empty_step_recent_body')}
            />
            <ChecklistRow
              index="03"
              title={t('feedback.filtered_empty_step_workflow_title')}
              body={t('feedback.filtered_empty_step_workflow_body')}
            />
          </div>
        </div>
      </div>
    </div>
  )
}

function FeedbackSubqueueEmptyState({
  queueMode,
  onResetQueue,
  onResetAll,
}: {
  queueMode: FeedbackQueueMode
  onResetQueue: () => void
  onResetAll: () => void
}) {
  const { t } = useTranslation()

  return (
    <div className="px-5 py-8 sm:px-6 lg:py-10">
      <div className="grid gap-5 lg:grid-cols-[minmax(0,1.3fr)_minmax(18rem,0.7fr)]">
        <div className="rounded-[1.1rem] border border-border/55 bg-[linear-gradient(180deg,rgba(255,255,255,0.98),rgba(248,250,252,0.95))] p-5 shadow-[0_24px_80px_-68px_rgba(15,23,42,0.26)]">
          <div className="flex items-start gap-4">
            <div className="flex size-12 shrink-0 items-center justify-center rounded-2xl border border-border/60 bg-muted/12 text-foreground">
              <Target className="size-5" />
            </div>
            <div className="min-w-0">
              <h3 className="text-lg font-semibold tracking-tight text-foreground">
                {t('feedback.subqueue_empty_title', {
                  queue: queueModeLabel(queueMode, t),
                })}
              </h3>
              <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground text-pretty">
                {t('feedback.subqueue_empty_body')}
              </p>
            </div>
          </div>
          <div className="mt-5 flex flex-wrap items-center gap-3">
            <Button onClick={onResetQueue}>{t('feedback.subqueue_empty_reset_queue')}</Button>
            <Button variant="outline" onClick={onResetAll}>
              {t('feedback.clear_filters')}
            </Button>
            <div className="text-sm text-muted-foreground">{t('feedback.subqueue_empty_hint')}</div>
          </div>
        </div>
        <div className="rounded-[1.1rem] border border-border/55 bg-background p-5">
          <div className="text-[11px] font-semibold tracking-[0.18em] text-muted-foreground uppercase">
            {t('feedback.subqueue_empty_playbook_eyebrow')}
          </div>
          <div className="mt-3 space-y-3">
            <ChecklistRow
              index="01"
              title={t('feedback.subqueue_empty_step_back_title')}
              body={t('feedback.subqueue_empty_step_back_body')}
            />
            <ChecklistRow
              index="02"
              title={t('feedback.subqueue_empty_step_sort_title')}
              body={t('feedback.subqueue_empty_step_sort_body')}
            />
            <ChecklistRow
              index="03"
              title={t('feedback.subqueue_empty_step_expand_title')}
              body={t('feedback.subqueue_empty_step_expand_body')}
            />
          </div>
        </div>
      </div>
    </div>
  )
}

function ActionLink({
  to,
  title,
  body,
}: {
  to: '/integrations/api-keys' | '/configuration/classification'
  title: string
  body: string
}) {
  return (
    <Link
      to={to}
      className="group rounded-xl border border-border/60 bg-background/92 p-4 transition-all hover:-translate-y-0.5 hover:border-primary/35 hover:shadow-[0_22px_40px_-34px_rgba(194,65,12,0.38)]"
    >
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="text-sm font-semibold text-foreground">{title}</div>
          <p className="mt-1 text-sm leading-6 text-muted-foreground text-pretty">{body}</p>
        </div>
        <ArrowRight className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-primary" />
      </div>
    </Link>
  )
}

function ChecklistRow({ index, title, body }: { index: string; title: string; body: string }) {
  return (
    <div className="flex gap-3 rounded-xl border border-border/50 bg-muted/10 p-3.5">
      <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground">
        {index}
      </div>
      <div className="min-w-0">
        <div className="text-sm font-semibold text-foreground">{title}</div>
        <p className="mt-1 text-sm leading-6 text-muted-foreground text-pretty">{body}</p>
      </div>
    </div>
  )
}

function QueueErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="px-5 py-8 sm:px-6 lg:py-10">
      <div className="rounded-[1.1rem] border border-destructive/20 bg-[linear-gradient(180deg,rgba(254,242,242,0.82),rgba(255,255,255,0.96))] p-5 shadow-[0_24px_80px_-66px_rgba(185,28,28,0.42)]">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div className="flex items-start gap-4">
            <div className="flex size-11 shrink-0 items-center justify-center rounded-2xl border border-destructive/20 bg-background/92 text-destructive">
              <AlertCircle className="size-5" />
            </div>
            <div className="min-w-0">
              <h3 className="text-lg font-semibold tracking-tight text-foreground">
                {t('feedback.error_title')}
              </h3>
              <p className="mt-2 max-w-2xl text-sm leading-6 text-muted-foreground text-pretty">
                {t('feedback.error_body')}
              </p>
              <p className="mt-3 rounded-lg border border-destructive/15 bg-background/75 px-3 py-2 font-mono text-xs text-muted-foreground">
                {message}
              </p>
            </div>
          </div>
          <Button variant="outline" onClick={onRetry} className="shrink-0">
            {t('feedback.error_retry')}
          </Button>
        </div>
      </div>
    </div>
  )
}

function ActiveChip({
  label,
  value,
  tone = 'default',
  onRemove,
}: {
  label: string
  value: string
  tone?: 'default' | 'urgent' | 'active'
  onRemove?: () => void
}) {
  return (
    <div
      className={`inline-flex items-center gap-2 rounded-full border px-3 py-1.5 text-sm ${compactStatToneClass(tone)}`}
    >
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium text-foreground">{value}</span>
      {onRemove ? (
        <button
          type="button"
          className="inline-flex size-4 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-background/70 hover:text-foreground"
          aria-label={`${label} ${value}`}
          onClick={onRemove}
        >
          <X className="size-3" />
        </button>
      ) : null}
    </div>
  )
}

function StatusPill({
  tone,
  label,
}: {
  tone: 'default' | 'urgent' | 'active' | 'closed' | 'error' | 'success' | 'muted'
  label: string
}) {
  const className =
    tone === 'urgent'
      ? 'border-red-200 bg-red-50 text-red-700'
      : tone === 'active'
        ? 'border-amber-200 bg-amber-50 text-amber-700'
        : tone === 'closed'
          ? 'border-emerald-200 bg-emerald-50 text-emerald-700'
          : tone === 'error'
            ? 'border-destructive/20 bg-destructive/5 text-destructive'
            : tone === 'success'
              ? 'border-emerald-200 bg-emerald-50/70 text-emerald-700'
              : 'border-border/60 bg-background text-muted-foreground'
  return (
    <span
      className={`inline-flex rounded-full border px-2 py-1 text-[11px] font-medium ${className}`}
    >
      {label}
    </span>
  )
}

function StatusMetaChip({ value, valueNode }: { value?: string; valueNode?: ReactNode }) {
  return (
    <span className="inline-flex items-center rounded-full border border-border/60 bg-background px-2 py-1 text-[11px] text-muted-foreground">
      {valueNode ?? value}
    </span>
  )
}

function compactStatToneClass(tone: 'default' | 'urgent' | 'active') {
  if (tone === 'urgent') return 'border-red-200/75 bg-red-50/45'
  if (tone === 'active') return 'border-amber-200/75 bg-amber-50/40'
  return 'border-border/60 bg-background'
}

function sortModeLabel(mode: FeedbackSortMode, t: (key: string) => string) {
  if (mode === 'urgent') return t('feedback.sort.urgent')
  if (mode === 'active') return t('feedback.sort.active')
  return t('feedback.sort.newest')
}

function queueModeLabel(mode: FeedbackQueueMode, t: (key: string) => string) {
  if (mode === 'urgent') return t('feedback.queue_mode.urgent')
  if (mode === 'active') return t('feedback.queue_mode.active')
  if (mode === 'failed') return t('feedback.queue_mode.failed')
  if (mode === 'terminal') return t('feedback.queue_mode.terminal')
  if (mode === 'ready') return t('feedback.queue_mode.ready')
  return t('feedback.queue_mode.all')
}

function enrichmentStatusLabel(status: string, t: (key: string) => string) {
  if (status === 'pending') return t('feedback.status.pending')
  if (status === 'enriching') return t('feedback.status.enriching')
  if (status === 'done') return t('feedback.status.done')
  if (status === 'failed') return t('feedback.status.failed')
  return status
}

function qualitySignalLabel(signal: string, t: (key: string) => string) {
  if (signal === 'low_confidence') return t('feedback.quality_filters.low_confidence')
  if (signal === 'off_list') return t('feedback.quality_filters.off_list')
  if (signal === 'parse_failure') return t('feedback.quality_filters.parse_failure')
  if (signal === 'terminal_failure') return t('feedback.quality_filters.terminal_failure')
  return signal
}

function queuePrimaryActionLabel(
  queueMode: FeedbackQueueMode,
  urgentCount: number,
  failedCount: number,
  terminalCount: number,
  t: (key: string) => string,
) {
  if (queueMode === 'urgent') return t('feedback.queue.actions.open_urgent')
  if (queueMode === 'active') return t('feedback.queue.actions.open_active')
  if (queueMode === 'failed') return t('feedback.queue.actions.open_failed')
  if (queueMode === 'terminal') return t('feedback.queue.actions.open_terminal')
  if (queueMode === 'ready') return t('feedback.queue.actions.open_ready')
  if (urgentCount > 0) return t('feedback.queue.actions.open_urgent')
  if (terminalCount > 0) return t('feedback.queue.actions.open_terminal')
  if (failedCount > 0) return t('feedback.queue.actions.open_failed')
  return t('feedback.queue.actions.open_priority')
}

function queuePostureTone(
  queueMode: FeedbackQueueMode,
  visibleCount: number,
  urgentCount: number,
  failedCount: number,
  readyCount: number,
  pendingAiCount: number,
) {
  if (visibleCount === 0) return 'default' as const
  if (queueMode === 'urgent') return 'danger' as const
  if (queueMode === 'failed') return 'danger' as const
  if (queueMode === 'active' || queueMode === 'ready') return 'success' as const
  if (urgentCount > 0) return 'danger' as const
  if (failedCount === visibleCount) return 'danger' as const
  if (readyCount > 0) return 'success' as const
  if (pendingAiCount > 0) return 'warning' as const
  return 'default' as const
}

function queuePostureLabel(
  queueMode: FeedbackQueueMode,
  visibleCount: number,
  urgentCount: number,
  failedCount: number,
  readyCount: number,
  pendingAiCount: number,
  t: (key: string) => string,
) {
  if (visibleCount === 0) return t('feedback.queue.posture_empty')
  if (queueMode === 'urgent') return t('feedback.queue.posture_urgent')
  if (queueMode === 'failed') return t('feedback.queue.posture_failure')
  if (queueMode === 'active') return t('feedback.queue.posture_active')
  if (queueMode === 'ready') return t('feedback.queue.posture_ready')
  if (urgentCount > 0) return t('feedback.queue.posture_urgent')
  if (failedCount === visibleCount) return t('feedback.queue.posture_failure')
  if (readyCount === visibleCount) return t('feedback.queue.posture_ready')
  if (pendingAiCount > 0 && readyCount === 0) return t('feedback.queue.posture_pending')
  return t('feedback.queue.posture_mixed')
}

function queuePostureHint(
  queueMode: FeedbackQueueMode,
  visibleCount: number,
  urgentCount: number,
  failedCount: number,
  readyCount: number,
  pendingAiCount: number,
  t: (key: string) => string,
) {
  if (visibleCount === 0) return t('feedback.queue.posture_empty_hint')
  if (queueMode === 'urgent') return t('feedback.queue.posture_urgent_hint')
  if (queueMode === 'failed') return t('feedback.queue.posture_failure_hint')
  if (queueMode === 'active') return t('feedback.queue.posture_active_hint')
  if (queueMode === 'ready') return t('feedback.queue.posture_ready_hint')
  if (urgentCount > 0) return t('feedback.queue.posture_urgent_hint')
  if (failedCount === visibleCount) return t('feedback.queue.posture_failure_hint')
  if (readyCount === visibleCount) return t('feedback.queue.posture_ready_hint')
  if (pendingAiCount > 0 && readyCount === 0) return t('feedback.queue.posture_pending_hint')
  return t('feedback.queue.posture_mixed_hint')
}

function queueAiHealthTone(
  visibleCount: number,
  failedCount: number,
  readyCount: number,
  pendingAiCount: number,
) {
  if (visibleCount === 0) return 'default' as const
  if (failedCount === visibleCount) return 'danger' as const
  if (readyCount === visibleCount) return 'success' as const
  if (pendingAiCount > 0 && failedCount === 0) return 'warning' as const
  if (failedCount > 0) return 'danger' as const
  return 'default' as const
}

function queueAiHealthLabel(
  visibleCount: number,
  failedCount: number,
  readyCount: number,
  pendingAiCount: number,
  t: (key: string) => string,
) {
  if (visibleCount === 0) return t('feedback.queue.ai_health_empty')
  if (failedCount === visibleCount) return t('feedback.queue.ai_health_failed')
  if (readyCount === visibleCount) return t('feedback.queue.ai_health_ready')
  if (pendingAiCount > 0 && failedCount === 0) return t('feedback.queue.ai_health_pending')
  if (failedCount > 0) return t('feedback.queue.ai_health_mixed')
  return t('feedback.queue.ai_health_mixed')
}

function queueAiHealthHint(
  visibleCount: number,
  failedCount: number,
  readyCount: number,
  pendingAiCount: number,
  t: (key: string) => string,
) {
  if (visibleCount === 0) return t('feedback.queue.ai_health_empty_hint')
  if (failedCount === visibleCount) return t('feedback.queue.ai_health_failed_hint')
  if (readyCount === visibleCount) return t('feedback.queue.ai_health_ready_hint')
  if (pendingAiCount > 0 && failedCount === 0) return t('feedback.queue.ai_health_pending_hint')
  return t('feedback.queue.ai_health_mixed_hint')
}

function shouldSurfaceRuntimeLink(
  queueMode: FeedbackQueueMode,
  visibleCount: number,
  failedCount: number,
  readyCount: number,
  pendingAiCount: number,
) {
  if (failedCount === 0) return false
  if (queueMode === 'failed') return true
  if (queueMode !== 'all') return false
  if (visibleCount === 0) return true
  if (failedCount === visibleCount) return true
  return readyCount === 0 && pendingAiCount === 0
}

function sortFeedbackItems(items: Feedback[], mode: FeedbackSortMode) {
  const ranked = [...items]
  ranked.sort((a, b) => compareFeedbackByMode(a, b, mode))
  return ranked
}

function filterFeedbackItemsByQueueMode(items: Feedback[], mode: FeedbackQueueMode) {
  if (mode === 'urgent') return items.filter((item) => item.isUrgent)
  if (mode === 'active') return items.filter((item) => item.workflowState?.category === 'active')
  // 'failed' and 'terminal' are filtered server-side via enrichment_status + terminal_failed_only
  // but we still do client-side filtering for any residual items
  if (mode === 'failed') return items.filter((item) => item.enrichmentStatus === 'failed')
  if (mode === 'terminal') return items.filter(isTerminalFailure)
  if (mode === 'ready') {
    return items.filter(isFeedbackReadyForTriage)
  }
  return items
}

function buildSemanticFilter(
  filters: FeedbackListFilters,
  dims: Dimension[],
): FeedbackFilter | undefined {
  const dimKinds = new Map(dims.map((dim) => [dim.name, dim.kind]))
  const attrs = filters.attrs.map((attr) => ({
    dim: attr.dim,
    value: attr.value,
    multi: dimKinds.get(attr.dim) === 'multi',
  }))

  const filter: FeedbackFilter = {
    attrs,
    urgent: filters.urgent,
    tagId: filters.tag || undefined,
    workflowStateId: filters.workflowState || undefined,
    enrichmentStatus: filters.enrichmentStatus || undefined,
    terminalFailedOnly: filters.terminalFailedOnly,
  }

  if (
    attrs.length === 0 &&
    filter.urgent == null &&
    !filter.tagId &&
    !filter.workflowStateId &&
    !filter.enrichmentStatus &&
    filter.terminalFailedOnly == null
  ) {
    return undefined
  }
  return filter
}

function semanticFilterCacheKey(filter: FeedbackFilter | undefined) {
  return JSON.stringify(filter ?? {})
}

function semanticHitToFeedback(hit: SemanticSearchHit): Feedback | null {
  const feedback = hit.feedback
  if (!feedback?.id) return null
  return {
    ...feedback,
    id: String(feedback.id),
    content: feedback.content ?? '',
    source: feedback.source ?? '',
    type: feedback.type ?? '',
    userId: feedback.userId ?? '',
    pageUrl: feedback.pageUrl ?? '',
    enrichedAttrs: feedback.enrichedAttrs ?? {},
    isUrgent: Boolean(feedback.isUrgent),
    enrichmentStatus: feedback.enrichmentStatus ?? '',
    createdAt: feedback.createdAt ?? '',
    tags: feedback.tags ?? [],
    allowedNextStates: feedback.allowedNextStates ?? [],
  }
}

function compareFeedbackByMode(a: Feedback, b: Feedback, mode: FeedbackSortMode) {
  if (mode === 'urgent') {
    const urgentDelta = Number(b.isUrgent) - Number(a.isUrgent)
    if (urgentDelta !== 0) return urgentDelta
    const activeDelta =
      Number(b.workflowState?.category === 'active') -
      Number(a.workflowState?.category === 'active')
    if (activeDelta !== 0) return activeDelta
  }

  if (mode === 'active') {
    const activeDelta =
      Number(b.workflowState?.category === 'active') -
      Number(a.workflowState?.category === 'active')
    if (activeDelta !== 0) return activeDelta
    const urgentDelta = Number(b.isUrgent) - Number(a.isUrgent)
    if (urgentDelta !== 0) return urgentDelta
  }

  return new Date(b.createdAt).getTime() - new Date(a.createdAt).getTime()
}

function EnrichmentAttemptsBadge({
  attempts,
  isTerminal,
  nextRetryAt,
  errorPreview,
}: {
  attempts: number
  isTerminal: boolean
  nextRetryAt?: string | null
  errorPreview?: string | null
}) {
  const { t } = useTranslation()
  const nextRetryText = nextRetryAt
    ? formatDistanceToNow(new Date(nextRetryAt), { addSuffix: true, locale: zhCN })
    : null
  const titleParts = [
    isTerminal
      ? t('feedback.row.terminal_attempts_hint', { count: attempts })
      : t('feedback.row.retry_attempts_hint', { count: attempts }),
  ]
  if (nextRetryText && !isTerminal) {
    titleParts.push(t('feedback.row.next_retry_hint', { time: nextRetryText }))
  }
  if (errorPreview) {
    titleParts.push(errorPreview.slice(0, 100) + (errorPreview.length > 100 ? '...' : ''))
  }
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${
        isTerminal
          ? 'bg-destructive/10 text-destructive'
          : 'bg-amber-50 text-amber-800 dark:bg-amber-500/20 dark:text-amber-300'
      }`}
      title={titleParts.join('\n')}
    >
      <AlertCircle className="size-2.5" />
      {attempts}/{MAX_ENRICHMENT_ATTEMPTS}
      {nextRetryText && !isTerminal && (
        <span className="ml-0.5 text-[9px] opacity-75">
          <Clock3 className="inline size-2" /> {nextRetryText}
        </span>
      )}
    </span>
  )
}

function TerminalFailuresSummaryCard({
  terminalCount,
  failedCount,
  onViewTerminal,
  onRetryAll,
}: {
  terminalCount: number
  failedCount: number
  onViewTerminal: () => void
  onRetryAll?: () => void
}) {
  const { t } = useTranslation()
  const retryingCount = failedCount - terminalCount

  return (
    <Card className="rounded-[1rem] border-destructive/25 bg-[linear-gradient(135deg,rgba(254,242,242,0.6),rgba(255,255,255,0.98))] py-0 shadow-[0_20px_48px_-42px_rgba(185,28,28,0.25)]">
      <CardContent className="px-5 py-4">
        <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
          <div className="flex items-start gap-4">
            <div className="flex size-11 shrink-0 items-center justify-center rounded-2xl border border-destructive/20 bg-background/90 text-destructive">
              <AlertCircle className="size-5" />
            </div>
            <div className="min-w-0">
              <h2 className="text-base font-semibold text-foreground">
                {t('feedback.terminal_summary.title')}
              </h2>
              <p className="mt-1 text-sm leading-6 text-muted-foreground">
                {t('feedback.terminal_summary.description', { count: terminalCount })}
              </p>
              <div className="mt-2 flex flex-wrap items-center gap-3 text-sm">
                <span className="inline-flex items-center gap-1.5 rounded-full border border-destructive/20 bg-destructive/5 px-2.5 py-1 text-xs font-medium text-destructive">
                  <AlertCircle className="size-3" />
                  {t('feedback.terminal_summary.terminal_count', { count: terminalCount })}
                </span>
                {retryingCount > 0 && (
                  <span className="inline-flex items-center gap-1.5 rounded-full border border-amber-200 bg-amber-50 px-2.5 py-1 text-xs font-medium text-amber-700">
                    <Clock3 className="size-3" />
                    {t('feedback.terminal_summary.retrying_count', { count: retryingCount })}
                  </span>
                )}
              </div>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" size="sm" onClick={onViewTerminal}>
              <Target className="size-3.5" />
              {t('feedback.terminal_summary.view_action')}
            </Button>
            {onRetryAll && (
              <Button type="button" size="sm" variant="outline" onClick={onRetryAll}>
                <RotateCcw className="size-3.5" />
                {t('feedback.terminal_summary.retry_action')}
              </Button>
            )}
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
