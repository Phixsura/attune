import { useInfiniteQuery } from '@tanstack/react-query'
import { format, formatDistanceToNow, isToday, isYesterday, type Locale } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import type { LucideIcon } from 'lucide-react'
import {
  CalendarClock,
  CheckCircle2,
  ChevronDown,
  Clock3,
  Copy,
  Download,
  FileSearch,
  Filter,
  Loader2,
  Search,
  ShieldCheck,
  Sparkles,
  TimerReset,
  X,
} from 'lucide-react'
import { startTransition, useDeferredValue, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { EmptyState } from '@/components/empty-state'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { auditActionOptions } from '@/features/audit-log/actions'
import {
  type AuditLogEntry,
  type AuditLogFilters,
  auditLogInfiniteQuery,
  downloadAuditLogCsv,
} from '@/features/audit-log/api/list-audit-log'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { cn } from '@/lib/utils'

type AuditDraftState = {
  selectedActions: string[]
  targetType: string
  targetId: string
  actorId: string
  fromValue: string
  toValue: string
}

type AuditPreset = {
  id: string
  label: string
  description: string
  values: Partial<AuditDraftState>
}

type AuditHistoryMode = 'push' | 'replace'

type AuditUrlState = {
  filters: AuditLogFilters
  inspectedEntryId: null | string
  localQuery: string
}

type AppliedFilterDescriptor = {
  key: 'actions' | 'actorId' | 'from' | 'localQuery' | 'targetId' | 'targetType' | 'to'
  label: string
}

type InvestigationSignal = {
  count: number
  value: string
}

type CompactSignalDescriptor = {
  key: string
  label: string
  onSelect: (value: string) => void
  signal?: InvestigationSignal
}

type AuditActivityBurst = {
  action: string
  count: number
  key: string
  latestCreatedAt: string
  latestId: string
  oldestCreatedAt: string
  oldestId: string
  paths: string[]
  targetId?: string
  targetType?: string
}

type AuditBurstContext = {
  burst: AuditActivityBurst
  burstItemCount?: number
  burstItemIndex?: number
  burstKey: string
}

type AuditStreamBlock =
  | { burst: AuditActivityBurst; kind: 'burst' }
  | {
      burstItemCount?: number
      burstItemIndex?: number
      burstKey?: string
      item: AuditLogEntry
      kind: 'item'
    }

type AuditStreamGroup = {
  blocks: AuditStreamBlock[]
  items: AuditLogEntry[]
  key: string
  label: string
}

type LocalAuditMatchReason =
  | 'action'
  | 'actor'
  | 'changePath'
  | 'metadata'
  | 'snapshot'
  | 'summary'
  | 'target'

const emptyDraftState: AuditDraftState = {
  selectedActions: [],
  targetType: '',
  targetId: '',
  actorId: '',
  fromValue: '',
  toValue: '',
}

export function AuditLogPage() {
  const { t, i18n } = useTranslation()
  const { can } = usePermissions()
  const canView = can('settings:audit_log:view')
  const locale = i18n.language.startsWith('zh') ? zhCN : undefined
  const initialUrlState = readAuditLogStateFromUrl()

  const [draft, setDraft] = useState<AuditDraftState>(() => filtersToDraft(initialUrlState.filters))
  const [filters, setFilters] = useState<AuditLogFilters>(() => ({
    ...initialUrlState.filters,
    limit: 50,
  }))
  const [localQuery, setLocalQuery] = useState(initialUrlState.localQuery)
  const [selectedEntryId, setSelectedEntryId] = useState<string | null>(
    initialUrlState.inspectedEntryId,
  )
  const [inspectedEntryId, setInspectedEntryId] = useState<string | null>(
    initialUrlState.inspectedEntryId,
  )
  const [isExporting, setIsExporting] = useState(false)
  const [isFilterConsoleExpanded, setIsFilterConsoleExpanded] = useState(false)
  const [isSliceSignalsExpanded, setIsSliceSignalsExpanded] = useState(false)
  const [expandedBurstKeys, setExpandedBurstKeys] = useState<Set<string>>(() => new Set())
  const localQueryInputRef = useRef<HTMLInputElement | null>(null)
  const entryFocusButtonRefs = useRef(new Map<string, HTMLButtonElement>())

  const deferredLocalQuery = useDeferredValue(localQuery.trim().toLowerCase())
  const draftFilters = draftToFilters(draft)

  const query = useInfiniteQuery(auditLogInfiniteQuery(filters))
  const items = query.data?.pages.flatMap((page) => page.items ?? []) ?? []
  const visibleItems = deferredLocalQuery
    ? items.filter((item) => matchesLocalAuditQuery(item, deferredLocalQuery))
    : items
  const latestEntry = items[0]
  const uniqueActions = new Set(items.map((item) => item.action)).size
  const activeFilterCount = countActiveFilters(filters)
  const hasPendingChanges = !areFiltersEqual(draftFilters, filters)
  const filterSummary = describeAppliedFilters(filters, localQuery, t)
  const presets = buildPresets(t)
  const insightItems = deferredLocalQuery ? visibleItems : items
  const insightSignals = buildInvestigationSignals(insightItems)
  const selectedVisibleIndex = selectedEntryId
    ? visibleItems.findIndex((item) => item.id === selectedEntryId)
    : -1
  const selectedEntry =
    visibleItems.find((item) => item.id === selectedEntryId) ??
    items.find((item) => item.id === selectedEntryId) ??
    null
  const inspectedEntry =
    visibleItems.find((item) => item.id === inspectedEntryId) ??
    items.find((item) => item.id === inspectedEntryId) ??
    null
  const detailItems =
    inspectedEntryId && visibleItems.some((item) => item.id === inspectedEntryId)
      ? visibleItems
      : items
  const inspectedEntryIndex = inspectedEntryId
    ? detailItems.findIndex((item) => item.id === inspectedEntryId)
    : -1
  const streamGroups = buildStreamGroups(visibleItems, locale)
  const burstCount = streamGroups.reduce(
    (count, group) => count + group.blocks.filter((block) => block.kind === 'burst').length,
    0,
  )
  const selectedBurstContext = selectedEntryId
    ? findBurstContextForEntry(streamGroups, selectedEntryId)
    : null
  const selectedBurstKeyToExpand =
    selectedBurstContext &&
    typeof selectedBurstContext.burstItemIndex === 'number' &&
    selectedBurstContext.burstItemIndex >= 3
      ? selectedBurstContext.burstKey
      : null
  const selectedBurst = selectedBurstContext?.burst ?? null
  const selectedBurstTargetId = selectedBurst?.targetId ?? null
  const selectedBurstPosition =
    typeof selectedBurstContext?.burstItemIndex === 'number'
      ? selectedBurstContext.burstItemIndex + 1
      : null
  const selectedBurstCanExpand = (selectedBurst?.count ?? 0) > 3
  const selectedBurstExpanded =
    Boolean(selectedBurst && expandedBurstKeys.has(selectedBurst.key)) ||
    selectedBurstKeyToExpand === selectedBurst?.key
  const oldestVisibleEntry = visibleItems[visibleItems.length - 1] ?? null
  const selectedChangeSummary = selectedEntry ? describeAuditChanges(selectedEntry) : null
  const selectedLocalMatchReasons =
    selectedEntry && deferredLocalQuery
      ? describeLocalAuditMatch(selectedEntry, deferredLocalQuery)
      : []
  const selectedHasActionScope = selectedEntry
    ? hasSingleActionFilter(filters, selectedEntry.action)
    : false
  const selectedHasTargetScope = selectedEntry
    ? hasTargetFilter(filters, selectedEntry.targetId)
    : false
  const selectedHasActorScope = selectedEntry
    ? hasActorFilter(filters, selectedEntry.actorId)
    : false
  const selectedBurstHasActionScope = selectedBurst
    ? hasSingleActionFilter(filters, selectedBurst.action)
    : false
  const selectedBurstHasTargetScope = selectedBurstTargetId
    ? hasTargetFilter(filters, selectedBurstTargetId)
    : false
  const hasInvestigationScope = activeFilterCount > 0 || Boolean(deferredLocalQuery)

  useEffect(() => {
    if (visibleItems.length === 0) {
      if (items.length === 0) {
        setSelectedEntryId(null)
      }
      return
    }
    if (!selectedEntryId || !visibleItems.some((item) => item.id === selectedEntryId)) {
      setSelectedEntryId(visibleItems[0]?.id ?? null)
    }
  }, [items.length, selectedEntryId, visibleItems])

  useEffect(() => {
    if (inspectedEntryId && !items.some((item) => item.id === inspectedEntryId)) {
      setInspectedEntryId(null)
    }
  }, [inspectedEntryId, items])

  useEffect(() => {
    if (!selectedEntryId) return
    entryFocusButtonRefs.current.get(selectedEntryId)?.scrollIntoView({
      block: 'nearest',
    })
  }, [selectedEntryId])

  useEffect(() => {
    if (typeof window === 'undefined') return undefined
    const handlePopState = () => {
      const nextState = readAuditLogStateFromUrl()
      setDraft(filtersToDraft(nextState.filters))
      setFilters({ ...nextState.filters, limit: 50 })
      setLocalQuery(nextState.localQuery)
      setInspectedEntryId(nextState.inspectedEntryId)
      setSelectedEntryId(nextState.inspectedEntryId)
    }

    window.addEventListener('popstate', handlePopState)
    return () => window.removeEventListener('popstate', handlePopState)
  }, [])

  useEffect(() => {
    writeAuditLogStateToUrl(
      {
        filters,
        inspectedEntryId,
        localQuery,
      },
      'replace',
    )
  }, [filters, inspectedEntryId, localQuery])

  const handleExport = async () => {
    const exportFilters = {
      actions: filters.actions,
      actorId: filters.actorId,
      from: filters.from,
      targetId: filters.targetId,
      targetType: filters.targetType,
      to: filters.to,
    }
    setIsExporting(true)
    try {
      await downloadAuditLogCsv(exportFilters)
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t('audit_log.export_failed'))
    } finally {
      setIsExporting(false)
    }
  }

  const handleCopy = async (value: string, successMessage = t('common.copied')) => {
    try {
      await navigator.clipboard.writeText(value)
      toast.success(successMessage)
    } catch {
      toast.error(t('audit_log.copy_failed'))
    }
  }

  const handleCopyCurrentView = () => {
    const viewUrl = buildAuditLogUrl({
      filters,
      inspectedEntryId,
      localQuery,
    })
    void handleCopy(viewUrl, t('audit_log.view_copied'))
  }

  const handleClearLocalQuery = () => {
    setLocalQuery('')
    const activeQueryInput = localQueryInputRef.current
    if (activeQueryInput && activeQueryInput === document.activeElement) {
      activeQueryInput.blur()
    }
  }

  const handleApply = () => {
    const nextFilters = { ...draftFilters, limit: 50 }
    setExpandedBurstKeys(new Set())
    writeAuditLogStateToUrl(
      {
        filters: draftFilters,
        inspectedEntryId,
        localQuery,
      },
      'push',
    )
    startTransition(() => {
      setFilters(nextFilters)
    })
  }

  const handleReset = () => {
    setDraft(emptyDraftState)
    setLocalQuery('')
    setExpandedBurstKeys(new Set())
    setSelectedEntryId(null)
    setInspectedEntryId(null)
    writeAuditLogStateToUrl(
      {
        filters: {},
        inspectedEntryId: null,
        localQuery: '',
      },
      'push',
    )
    startTransition(() => {
      setFilters({ limit: 50 })
    })
  }

  const handlePreset = (preset: AuditPreset) => {
    const nextDraft: AuditDraftState = {
      ...emptyDraftState,
      ...preset.values,
      selectedActions: preset.values.selectedActions ?? [],
    }
    setDraft(nextDraft)
    setLocalQuery('')
    setExpandedBurstKeys(new Set())
    setSelectedEntryId(null)
    setInspectedEntryId(null)
    const nextFilters = draftToFilters(nextDraft)
    writeAuditLogStateToUrl(
      {
        filters: nextFilters,
        inspectedEntryId: null,
        localQuery: '',
      },
      'push',
    )
    startTransition(() => {
      setFilters({ ...nextFilters, limit: 50 })
    })
  }

  const handleOpenDetails = (id: string, mode: AuditHistoryMode = 'push') => {
    setSelectedEntryId(id)
    setInspectedEntryId(id)
    writeAuditLogStateToUrl(
      {
        filters,
        inspectedEntryId: id,
        localQuery,
      },
      mode,
    )
  }

  const handleCloseDetails = (mode: AuditHistoryMode = 'push') => {
    if (!inspectedEntryId) return
    setInspectedEntryId(null)
    writeAuditLogStateToUrl(
      {
        filters,
        inspectedEntryId: null,
        localQuery,
      },
      mode,
    )
  }

  const handleMoveSelection = (direction: -1 | 1) => {
    if (visibleItems.length === 0) return
    const currentIndex = selectedEntryId
      ? visibleItems.findIndex((item) => item.id === selectedEntryId)
      : -1
    const fallbackIndex = direction > 0 ? 0 : visibleItems.length - 1
    const nextIndex =
      currentIndex < 0
        ? fallbackIndex
        : Math.min(Math.max(currentIndex + direction, 0), visibleItems.length - 1)
    const nextEntry = visibleItems[nextIndex]
    if (!nextEntry) return
    setSelectedEntryId(nextEntry.id)
  }

  const handleNavigateDetails = (direction: -1 | 1) => {
    if (inspectedEntryIndex < 0) return
    const nextEntry = detailItems[inspectedEntryIndex + direction]
    if (!nextEntry) return
    setSelectedEntryId(nextEntry.id)
    setInspectedEntryId(nextEntry.id)
    writeAuditLogStateToUrl(
      {
        filters,
        inspectedEntryId: nextEntry.id,
        localQuery,
      },
      'replace',
    )
  }

  const handleInvestigate = (
    kind: 'action' | 'actorId' | 'targetId',
    value: string,
    entryId?: string,
  ) => {
    const baseDraft = filtersToDraft(filters)
    const nextDraft: AuditDraftState = {
      ...baseDraft,
      actorId: kind === 'actorId' ? value : baseDraft.actorId,
      selectedActions: kind === 'action' ? [value] : baseDraft.selectedActions,
      targetId: kind === 'targetId' ? value : baseDraft.targetId,
    }
    const nextFilters = draftToFilters(nextDraft)
    setDraft(nextDraft)
    setLocalQuery('')
    setExpandedBurstKeys(new Set())
    setSelectedEntryId(entryId ?? selectedEntryId)
    writeAuditLogStateToUrl(
      {
        filters: nextFilters,
        inspectedEntryId,
        localQuery: '',
      },
      'push',
    )
    startTransition(() => {
      setFilters({ ...nextFilters, limit: 50 })
    })
  }

  const handleRegisterEntryFocusButton = (id: string, node: HTMLButtonElement | null) => {
    if (node) {
      entryFocusButtonRefs.current.set(id, node)
      return
    }
    entryFocusButtonRefs.current.delete(id)
  }

  const toggleBurstExpansion = (burstKey: string) => {
    setExpandedBurstKeys((current) => {
      const next = new Set(current)
      if (next.has(burstKey)) {
        next.delete(burstKey)
      } else {
        next.add(burstKey)
      }
      return next
    })
  }

  const handleSpotlightSignal = (query: string) => {
    const nextQuery = query.trim()
    if (!nextQuery) return
    setLocalQuery(nextQuery)
    setExpandedBurstKeys(new Set())
    localQueryInputRef.current?.focus()
    writeAuditLogStateToUrl(
      {
        filters,
        inspectedEntryId,
        localQuery: nextQuery,
      },
      'push',
    )
  }

  useEffect(() => {
    if (typeof window === 'undefined') return undefined

    const handleKeyDown = (event: KeyboardEvent) => {
      const key = event.key.toLowerCase()
      const target = event.target
      const isTextEntry = isTextEntryTarget(target)
      const hasModifier = event.altKey || event.ctrlKey || event.metaKey

      if (hasModifier) return

      if (key === 'escape') {
        if (inspectedEntryId) {
          event.preventDefault()
          setInspectedEntryId(null)
          writeAuditLogStateToUrl(
            {
              filters,
              inspectedEntryId: null,
              localQuery,
            },
            'push',
          )
          return
        }
        if (localQuery) {
          event.preventDefault()
          setLocalQuery('')
          const activeQueryInput = localQueryInputRef.current
          if (activeQueryInput && activeQueryInput === document.activeElement) {
            activeQueryInput.blur()
          }
        }
        return
      }

      if (isTextEntry) return

      if (key === '/') {
        event.preventDefault()
        localQueryInputRef.current?.focus()
        localQueryInputRef.current?.select()
        return
      }

      if (key === 'enter') {
        if (isCommandSurfaceTarget(target)) return
        if (!selectedEntry) return
        event.preventDefault()
        setSelectedEntryId(selectedEntry.id)
        setInspectedEntryId(selectedEntry.id)
        writeAuditLogStateToUrl(
          {
            filters,
            inspectedEntryId: selectedEntry.id,
            localQuery,
          },
          'push',
        )
        return
      }

      if (key === 'j' || event.key === 'ArrowDown') {
        event.preventDefault()
        if (inspectedEntryId) {
          const nextEntry = detailItems[inspectedEntryIndex + 1]
          if (!nextEntry) return
          setSelectedEntryId(nextEntry.id)
          setInspectedEntryId(nextEntry.id)
          writeAuditLogStateToUrl(
            {
              filters,
              inspectedEntryId: nextEntry.id,
              localQuery,
            },
            'replace',
          )
          return
        }
        if (visibleItems.length === 0) return
        const currentIndex = selectedEntryId
          ? visibleItems.findIndex((item) => item.id === selectedEntryId)
          : -1
        const nextIndex = currentIndex < 0 ? 0 : Math.min(currentIndex + 1, visibleItems.length - 1)
        const nextEntry = visibleItems[nextIndex]
        if (!nextEntry) return
        setSelectedEntryId(nextEntry.id)
        return
      }

      if (key === 'k' || event.key === 'ArrowUp') {
        event.preventDefault()
        if (inspectedEntryId) {
          const nextEntry = detailItems[inspectedEntryIndex - 1]
          if (!nextEntry) return
          setSelectedEntryId(nextEntry.id)
          setInspectedEntryId(nextEntry.id)
          writeAuditLogStateToUrl(
            {
              filters,
              inspectedEntryId: nextEntry.id,
              localQuery,
            },
            'replace',
          )
          return
        }
        if (visibleItems.length === 0) return
        const currentIndex = selectedEntryId
          ? visibleItems.findIndex((item) => item.id === selectedEntryId)
          : -1
        const nextIndex = currentIndex < 0 ? visibleItems.length - 1 : Math.max(currentIndex - 1, 0)
        const nextEntry = visibleItems[nextIndex]
        if (!nextEntry) return
        setSelectedEntryId(nextEntry.id)
      }
    }

    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [
    detailItems,
    filters,
    inspectedEntryId,
    inspectedEntryIndex,
    localQuery,
    selectedEntry,
    selectedEntryId,
    visibleItems,
  ])

  const handleRemoveAppliedFilter = (key: AppliedFilterDescriptor['key']) => {
    const nextDraft = filtersToDraft(filters)
    if (key === 'actions') nextDraft.selectedActions = []
    if (key === 'actorId') nextDraft.actorId = ''
    if (key === 'from') nextDraft.fromValue = ''
    const nextLocalQuery = key === 'localQuery' ? '' : localQuery
    if (key === 'localQuery') setLocalQuery(nextLocalQuery)
    if (key === 'targetId') nextDraft.targetId = ''
    if (key === 'targetType') nextDraft.targetType = ''
    if (key === 'to') nextDraft.toValue = ''

    const nextFilters = draftToFilters(nextDraft)
    setDraft(nextDraft)
    setExpandedBurstKeys(new Set())
    writeAuditLogStateToUrl(
      {
        filters: nextFilters,
        inspectedEntryId,
        localQuery: nextLocalQuery,
      },
      'push',
    )
    startTransition(() => {
      setFilters({ ...nextFilters, limit: 50 })
    })
  }

  const compactSignals: CompactSignalDescriptor[] = [
    {
      key: 'action',
      label: t('audit_log.slice_actions'),
      onSelect: (value) => handleInvestigate('action', value),
      signal: insightSignals.actions[0],
    },
    {
      key: 'actor',
      label: t('audit_log.slice_actors'),
      onSelect: (value) => handleInvestigate('actorId', value),
      signal: insightSignals.actors[0],
    },
    {
      key: 'target',
      label: t('audit_log.slice_targets'),
      onSelect: (value) => handleInvestigate('targetId', value),
      signal: insightSignals.targets[0],
    },
    {
      key: 'path',
      label: t('audit_log.slice_paths'),
      onSelect: handleSpotlightSignal,
      signal: insightSignals.paths[0],
    },
  ]

  if (!canView) {
    return (
      <EmptyState
        icon={FileSearch}
        title={t('audit_log.empty_title')}
        description={t('audit_log.empty_body')}
      />
    )
  }

  return (
    <section className="space-y-5">
      <header className="overflow-hidden rounded-[1.5rem] border border-primary/15 bg-[linear-gradient(135deg,rgba(255,255,255,0.98),rgba(255,248,240,0.96)_46%,rgba(255,238,219,0.88))] shadow-[0_1px_0_rgba(15,23,42,0.03),0_18px_50px_-28px_rgba(234,88,12,0.35)]">
        <div className="space-y-4 px-6 py-5 lg:px-7 lg:py-6">
          <div className="flex flex-col gap-4 xl:flex-row xl:items-start xl:justify-between">
            <div className="space-y-4">
              <div className="inline-flex items-center gap-2 rounded-full border border-primary/20 bg-white/80 px-3 py-1 text-[11px] font-semibold tracking-[0.18em] text-primary uppercase shadow-sm backdrop-blur">
                <ShieldCheck className="h-3.5 w-3.5" />
                {t('audit_log.hero_eyebrow')}
              </div>
              <div className="space-y-2">
                <h1 className="max-w-3xl text-[2rem] font-semibold tracking-[-0.045em] text-foreground [text-wrap:balance] md:text-[2.3rem]">
                  {t('audit_log.title')}
                </h1>
                <p className="max-w-[56ch] text-sm leading-6 text-muted-foreground [text-wrap:pretty]">
                  {t('audit_log.subtitle')}
                </p>
              </div>
              <div className="flex flex-wrap gap-3">
                <Button type="button" disabled={isExporting} onClick={() => void handleExport()}>
                  <Download className="mr-2 h-4 w-4" />
                  {isExporting ? t('audit_log.exporting') : t('audit_log.export')}
                </Button>
                <Button type="button" variant="outline" onClick={handleCopyCurrentView}>
                  <Copy className="mr-2 h-4 w-4" />
                  {t('audit_log.copy_view')}
                </Button>
              </div>
            </div>

            <div className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-white/72 px-3 py-2 text-xs text-muted-foreground shadow-sm backdrop-blur">
              <Sparkles className="h-3.5 w-3.5 text-primary" />
              {t('audit_log.table_help')}
            </div>
          </div>

          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
            <StatCard
              icon={FileSearch}
              label={t('audit_log.stats.loaded')}
              value={String(items.length)}
              hint={t('audit_log.stats.loaded_hint')}
            />
            <StatCard
              icon={Filter}
              label={t('audit_log.stats.filters')}
              value={String(activeFilterCount)}
              hint={
                activeFilterCount > 0
                  ? t('audit_log.stats.filters_active')
                  : t('audit_log.stats.filters_idle')
              }
            />
            <StatCard
              icon={CalendarClock}
              label={t('audit_log.stats.latest')}
              value={
                latestEntry
                  ? formatDistanceToNow(new Date(latestEntry.createdAt), {
                      addSuffix: true,
                      locale,
                    })
                  : t('audit_log.stats.latest_empty')
              }
              hint={
                latestEntry
                  ? formatAbsoluteTimestamp(latestEntry.createdAt, locale)
                  : t('audit_log.stats.latest_hint')
              }
            />
            <StatCard
              icon={ShieldCheck}
              label={t('audit_log.stats.actions')}
              value={String(uniqueActions)}
              hint={t('audit_log.stats.actions_hint')}
            />
          </div>
        </div>
      </header>

      <div className="grid gap-6 xl:grid-cols-[minmax(19rem,22rem)_minmax(0,1fr)]">
        <Card className="overflow-hidden border-border/70 shadow-[0_1px_0_rgba(15,23,42,0.02),0_16px_40px_-30px_rgba(15,23,42,0.24)] xl:sticky xl:top-6 xl:self-start">
          <CardHeader className="border-b border-border/70 bg-[linear-gradient(180deg,rgba(248,250,252,0.9),rgba(255,255,255,0.95))]">
            <div className="flex items-start justify-between gap-3">
              <div className="space-y-3">
                <div className="inline-flex items-center gap-2 text-xs font-semibold tracking-[0.18em] text-muted-foreground uppercase">
                  <Filter className="h-3.5 w-3.5 text-primary" />
                  {t('audit_log.filters_title')}
                </div>
                <CardTitle className="text-xl tracking-[-0.03em]">
                  {t('audit_log.filters_panel_title')}
                </CardTitle>
                <CardDescription className="max-w-md leading-6">
                  {t('audit_log.filters_panel_body')}
                </CardDescription>
              </div>
              <Button
                type="button"
                variant="ghost"
                size="sm"
                className="shrink-0"
                onClick={() => setIsFilterConsoleExpanded((value) => !value)}
                aria-expanded={isFilterConsoleExpanded}
              >
                {isFilterConsoleExpanded
                  ? t('audit_log.collapse_filters')
                  : t('audit_log.expand_filters')}
                <ChevronDown
                  className={cn(
                    'h-4 w-4 transition-transform',
                    isFilterConsoleExpanded && 'rotate-180',
                  )}
                />
              </Button>
            </div>
          </CardHeader>
          <CardContent className="px-6 py-6">
            {isFilterConsoleExpanded ? (
              <div className="space-y-5">
                <div className="space-y-3">
                  <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                    {t('audit_log.presets_title')}
                  </div>
                  <div className="grid gap-2">
                    {presets.map((preset) => (
                      <button
                        key={preset.id}
                        type="button"
                        onClick={() => handlePreset(preset)}
                        className="rounded-2xl border border-border/70 bg-background/90 px-4 py-3 text-left shadow-[inset_0_1px_0_rgba(255,255,255,0.78)] transition hover:border-primary/25 hover:bg-primary/[0.04] focus-visible:border-ring focus-visible:outline-none focus-visible:ring-[3px] focus-visible:ring-ring/40"
                      >
                        <div className="text-sm font-semibold text-foreground">{preset.label}</div>
                        <div className="mt-1 text-xs leading-5 text-muted-foreground">
                          {preset.description}
                        </div>
                      </button>
                    ))}
                  </div>
                </div>

                <FilterField label={t('audit_log.fields.action')} htmlFor="audit-actions-trigger">
                  <AuditActionMultiSelect
                    selected={draft.selectedActions}
                    onChange={(selectedActions) =>
                      setDraft((prev) => ({ ...prev, selectedActions }))
                    }
                    triggerId="audit-actions-trigger"
                  />
                </FilterField>

                <div className="grid gap-5 md:grid-cols-2 xl:grid-cols-1">
                  <FilterField
                    label={t('audit_log.fields.target_type')}
                    htmlFor="audit-target-type"
                  >
                    <Input
                      id="audit-target-type"
                      value={draft.targetType}
                      onChange={(e) =>
                        setDraft((prev) => ({ ...prev, targetType: e.target.value }))
                      }
                      placeholder={t('audit_log.placeholders.target_type')}
                    />
                  </FilterField>

                  <FilterField label={t('audit_log.fields.target_id')} htmlFor="audit-target-id">
                    <Input
                      id="audit-target-id"
                      value={draft.targetId}
                      onChange={(e) => setDraft((prev) => ({ ...prev, targetId: e.target.value }))}
                      placeholder={t('audit_log.placeholders.target_id')}
                    />
                  </FilterField>

                  <FilterField label={t('audit_log.fields.actor_id')} htmlFor="audit-actor-id">
                    <Input
                      id="audit-actor-id"
                      value={draft.actorId}
                      onChange={(e) => setDraft((prev) => ({ ...prev, actorId: e.target.value }))}
                      placeholder={t('audit_log.placeholders.actor_id')}
                    />
                  </FilterField>

                  <FilterField label={t('audit_log.filters_from')} htmlFor="audit-from">
                    <Input
                      id="audit-from"
                      type="datetime-local"
                      value={draft.fromValue}
                      onChange={(e) => setDraft((prev) => ({ ...prev, fromValue: e.target.value }))}
                    />
                  </FilterField>

                  <FilterField label={t('audit_log.filters_to')} htmlFor="audit-to">
                    <Input
                      id="audit-to"
                      type="datetime-local"
                      value={draft.toValue}
                      onChange={(e) => setDraft((prev) => ({ ...prev, toValue: e.target.value }))}
                    />
                  </FilterField>
                </div>

                <div className="rounded-2xl border border-border/60 bg-muted/20 p-3">
                  <div className="mb-2 text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                    {t('audit_log.filters_summary_title')}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <FilterChip active={draft.selectedActions.length > 0}>
                      {draft.selectedActions.length > 0
                        ? t('audit_log.filters_actions_selected', {
                            count: draft.selectedActions.length,
                          })
                        : t('audit_log.filters_actions_placeholder')}
                    </FilterChip>
                    <FilterChip active={Boolean(draft.targetType)}>
                      {draft.targetType || t('audit_log.fields.target_type')}
                    </FilterChip>
                    <FilterChip active={Boolean(draft.targetId)}>
                      {draft.targetId || t('audit_log.fields.target_id')}
                    </FilterChip>
                    <FilterChip active={Boolean(draft.actorId)}>
                      {draft.actorId || t('audit_log.fields.actor_id')}
                    </FilterChip>
                    <FilterChip active={Boolean(draft.fromValue)}>
                      {draft.fromValue || t('audit_log.filters_from')}
                    </FilterChip>
                    <FilterChip active={Boolean(draft.toValue)}>
                      {draft.toValue || t('audit_log.filters_to')}
                    </FilterChip>
                  </div>
                  <div className="mt-3 inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/90 px-3 py-1.5 text-xs text-muted-foreground">
                    {hasPendingChanges ? (
                      <>
                        <Clock3 className="h-3.5 w-3.5 text-primary" />
                        {t('audit_log.pending_changes')}
                      </>
                    ) : (
                      <>
                        <CheckCircle2 className="h-3.5 w-3.5 text-primary" />
                        {t('audit_log.pending_changes_synced')}
                      </>
                    )}
                  </div>
                </div>

                <div className="flex flex-wrap gap-3 pt-1">
                  <Button
                    type="button"
                    className="min-w-32"
                    disabled={!hasPendingChanges}
                    onClick={handleApply}
                  >
                    {t('audit_log.apply')}
                  </Button>
                  <Button type="button" variant="ghost" className="min-w-28" onClick={handleReset}>
                    <TimerReset className="mr-2 h-4 w-4" />
                    {t('audit_log.reset')}
                  </Button>
                </div>
              </div>
            ) : (
              <div className="space-y-4">
                <div className="rounded-2xl border border-border/60 bg-muted/18 px-4 py-3 text-sm leading-6 text-muted-foreground">
                  {activeFilterCount > 0
                    ? t('audit_log.compact_filter_active', { count: activeFilterCount })
                    : t('audit_log.compact_filter_idle')}
                </div>
                <div className="space-y-2">
                  <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                    {t('audit_log.presets_title')}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {presets.map((preset) => (
                      <Button
                        key={preset.id}
                        type="button"
                        variant="outline"
                        size="sm"
                        className="rounded-full"
                        onClick={() => handlePreset(preset)}
                      >
                        {preset.label}
                      </Button>
                    ))}
                  </div>
                </div>
                <div className="flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-border/60 bg-muted/18 px-4 py-3">
                  <div className="inline-flex items-center gap-2 text-xs text-muted-foreground">
                    {hasPendingChanges ? (
                      <>
                        <Clock3 className="h-3.5 w-3.5 text-primary" />
                        {t('audit_log.pending_changes')}
                      </>
                    ) : (
                      <>
                        <CheckCircle2 className="h-3.5 w-3.5 text-primary" />
                        {t('audit_log.pending_changes_synced')}
                      </>
                    )}
                  </div>
                  <div className="flex flex-wrap gap-2">
                    {hasPendingChanges ? (
                      <Button type="button" size="sm" onClick={handleApply}>
                        {t('audit_log.apply')}
                      </Button>
                    ) : null}
                    <Button type="button" variant="ghost" size="sm" onClick={handleReset}>
                      {t('audit_log.reset')}
                    </Button>
                  </div>
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        <Card className="overflow-hidden border-border/70 shadow-[0_1px_0_rgba(15,23,42,0.02),0_16px_40px_-32px_rgba(15,23,42,0.22)]">
          <CardHeader className="border-b border-border/70 bg-[linear-gradient(180deg,rgba(255,255,255,0.98),rgba(248,250,252,0.86))]">
            <div className="flex flex-wrap items-start justify-between gap-4">
              <div className="space-y-2">
                <div className="inline-flex items-center gap-2 text-xs font-semibold tracking-[0.18em] text-muted-foreground uppercase">
                  <Sparkles className="h-3.5 w-3.5 text-primary" />
                  {t('audit_log.table_title')}
                </div>
                <CardTitle className="text-xl tracking-[-0.03em]">
                  {t('audit_log.stream_title')}
                </CardTitle>
                <CardDescription className="max-w-2xl leading-6">
                  {t('audit_log.stream_help')}
                </CardDescription>
              </div>
              <div className="rounded-2xl border border-border/70 bg-white/80 px-4 py-3 shadow-sm">
                <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                  {deferredLocalQuery
                    ? t('audit_log.stream_visible_label')
                    : t('audit_log.stream_count_label')}
                </div>
                <div className="mt-1 text-2xl font-semibold tracking-[-0.04em] text-foreground tabular-nums">
                  {visibleItems.length}
                </div>
                <div className="mt-1 text-xs leading-5 text-muted-foreground">
                  {deferredLocalQuery
                    ? t('audit_log.stream_visible_hint', { total: items.length })
                    : t('audit_log.stream_count_hint')}
                </div>
              </div>
            </div>

            <div className="grid gap-3 pt-4 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-center">
              <div className="relative">
                <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
                <Input
                  ref={localQueryInputRef}
                  value={localQuery}
                  onChange={(e) => setLocalQuery(e.target.value)}
                  className="pl-9 pr-9"
                  placeholder={t('audit_log.local_query_placeholder')}
                />
                {localQuery ? (
                  <button
                    type="button"
                    className="absolute top-1/2 right-2 -translate-y-1/2 rounded-full p-1 text-muted-foreground transition hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                    onClick={handleClearLocalQuery}
                    aria-label={t('audit_log.clear_local_query')}
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                ) : null}
              </div>
              <div className="flex flex-wrap gap-2">
                {filterSummary.length > 0 ? (
                  filterSummary.map((summary) => (
                    <RemovableFilterChip
                      key={summary.key}
                      label={summary.label}
                      onRemove={() => handleRemoveAppliedFilter(summary.key)}
                    />
                  ))
                ) : (
                  <FilterChip active={false}>{t('audit_log.all_filters_idle')}</FilterChip>
                )}
              </div>
            </div>

            {visibleItems.length > 0 ? (
              <div className="pt-4">
                <div className="rounded-2xl border border-border/70 bg-muted/[0.08] px-4 py-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.78)]">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="space-y-2">
                      <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                        {t('audit_log.scope_title')}
                      </div>
                      <div className="text-sm leading-6 text-foreground">
                        {activeFilterCount > 0 || deferredLocalQuery
                          ? t('audit_log.scope_body_active')
                          : t('audit_log.scope_body_idle')}
                      </div>
                    </div>
                    {oldestVisibleEntry && latestEntry ? (
                      <div className="rounded-full border border-border/70 bg-background/88 px-3 py-1.5 text-xs text-muted-foreground tabular-nums">
                        {t('audit_log.scope_range', {
                          from: formatAbsoluteTimestamp(oldestVisibleEntry.createdAt, locale),
                          to: formatAbsoluteTimestamp(latestEntry.createdAt, locale),
                        })}
                      </div>
                    ) : null}
                  </div>
                  <div className="mt-3 flex flex-wrap gap-2">
                    <FilterChip active>
                      {t('audit_log.scope_events', { count: visibleItems.length })}
                    </FilterChip>
                    {burstCount > 0 ? (
                      <FilterChip active={false}>
                        {t('audit_log.scope_bursts', { count: burstCount })}
                      </FilterChip>
                    ) : null}
                    <FilterChip active={Boolean(deferredLocalQuery)}>
                      {deferredLocalQuery
                        ? t('audit_log.scope_local_query', {
                            query: localQuery.trim(),
                          })
                        : t('audit_log.scope_local_idle')}
                    </FilterChip>
                  </div>
                </div>
              </div>
            ) : null}
          </CardHeader>
          <CardContent className="px-0 py-0">
            {query.isPending ? (
              <AuditLogSkeleton />
            ) : query.isError ? (
              <EmptyState
                icon={FileSearch}
                title={t('audit_log.error_title')}
                description={t('audit_log.error_body')}
                action={{ label: t('audit_log.retry'), onClick: () => void query.refetch() }}
                className="px-6 py-16"
              />
            ) : items.length === 0 ? (
              <EmptyState
                icon={FileSearch}
                title={
                  activeFilterCount > 0
                    ? t('audit_log.filtered_empty_title')
                    : t('audit_log.empty_title')
                }
                description={
                  activeFilterCount > 0
                    ? t('audit_log.filtered_empty_body')
                    : t('audit_log.empty_body')
                }
                action={
                  activeFilterCount > 0
                    ? { label: t('audit_log.reset'), onClick: handleReset }
                    : undefined
                }
                className="px-6 py-16"
              />
            ) : visibleItems.length === 0 ? (
              <EmptyState
                icon={Search}
                title={t('audit_log.local_empty_title')}
                description={t('audit_log.local_empty_body')}
                action={{
                  label: t('audit_log.local_empty_action'),
                  onClick: handleClearLocalQuery,
                }}
                className="px-6 py-16"
              />
            ) : (
              <div className="space-y-0">
                <div className="border-b border-border/70 bg-[linear-gradient(180deg,rgba(255,251,247,0.72),rgba(255,255,255,0.94))] px-6 py-4">
                  <div className="grid gap-4 xl:grid-cols-[minmax(0,1.05fr)_minmax(19rem,0.95fr)]">
                    <div className="space-y-4">
                      <div className="rounded-[1.35rem] border border-border/70 bg-white/88 p-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.82)]">
                        <div className="flex flex-wrap items-start justify-between gap-3">
                          <div className="space-y-1">
                            <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                              {t('audit_log.slice_signals_title')}
                            </div>
                            <div className="max-w-[52ch] text-sm leading-6 text-muted-foreground">
                              {deferredLocalQuery
                                ? t('audit_log.slice_signals_body_visible', {
                                    count: insightItems.length,
                                  })
                                : t('audit_log.slice_signals_body_loaded', {
                                    count: insightItems.length,
                                  })}
                            </div>
                          </div>
                          <div className="flex flex-wrap items-center gap-2">
                            <FilterChip active={Boolean(deferredLocalQuery)}>
                              {deferredLocalQuery
                                ? t('audit_log.slice_basis_visible')
                                : t('audit_log.slice_basis_loaded')}
                            </FilterChip>
                            <Button
                              type="button"
                              variant="ghost"
                              size="sm"
                              onClick={() => setIsSliceSignalsExpanded((value) => !value)}
                              aria-expanded={isSliceSignalsExpanded}
                            >
                              {isSliceSignalsExpanded
                                ? t('audit_log.collapse_signals')
                                : t('audit_log.expand_signals')}
                              <ChevronDown
                                className={cn(
                                  'h-4 w-4 transition-transform',
                                  isSliceSignalsExpanded && 'rotate-180',
                                )}
                              />
                            </Button>
                          </div>
                        </div>
                        {isSliceSignalsExpanded ? (
                          <div className="mt-4 grid gap-3 2xl:grid-cols-2">
                            <InvestigationSignalGroup
                              title={t('audit_log.slice_actions')}
                              items={insightSignals.actions}
                              emptyLabel={t('audit_log.slice_none')}
                              onSelect={(value) => handleInvestigate('action', value)}
                            />
                            <InvestigationSignalGroup
                              title={t('audit_log.slice_actors')}
                              items={insightSignals.actors}
                              emptyLabel={t('audit_log.slice_none')}
                              onSelect={(value) => handleInvestigate('actorId', value)}
                            />
                            <InvestigationSignalGroup
                              title={t('audit_log.slice_targets')}
                              items={insightSignals.targets}
                              emptyLabel={t('audit_log.slice_none')}
                              onSelect={(value) => handleInvestigate('targetId', value)}
                            />
                            <InvestigationSignalGroup
                              title={t('audit_log.slice_paths')}
                              items={insightSignals.paths}
                              emptyLabel={t('audit_log.slice_none')}
                              onSelect={handleSpotlightSignal}
                            />
                          </div>
                        ) : (
                          <div className="mt-4 grid gap-3 sm:grid-cols-2 2xl:grid-cols-4">
                            {compactSignals.map((item) => (
                              <CompactInvestigationSignalCard
                                key={item.key}
                                title={item.label}
                                signal={item.signal}
                                emptyLabel={t('audit_log.slice_none')}
                                onSelect={item.onSelect}
                              />
                            ))}
                          </div>
                        )}
                      </div>

                      {selectedBurst ? (
                        <div className="rounded-[1.35rem] border border-border/70 bg-background/92 p-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.76)]">
                          <div className="flex flex-wrap items-start justify-between gap-3">
                            <div className="space-y-1">
                              <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                                {t('audit_log.workspace_burst_title')}
                              </div>
                              <div className="max-w-[50ch] text-sm leading-6 text-muted-foreground">
                                {t('audit_log.workspace_burst_body', {
                                  current: selectedBurstPosition ?? 1,
                                  total: selectedBurst.count,
                                })}
                              </div>
                            </div>
                            <div className="rounded-full border border-border/70 bg-muted/[0.12] px-3 py-1.5 text-xs text-muted-foreground tabular-nums">
                              {t('audit_log.scope_range', {
                                from: formatAbsoluteTimestamp(
                                  selectedBurst.oldestCreatedAt,
                                  locale,
                                ),
                                to: formatAbsoluteTimestamp(selectedBurst.latestCreatedAt, locale),
                              })}
                            </div>
                          </div>
                          <div className="mt-3 flex flex-wrap gap-2">
                            <AuditToken className="border-primary/15 bg-primary/8 text-primary">
                              {selectedBurst.action}
                            </AuditToken>
                            {selectedBurst.targetType ? (
                              <AuditToken>{selectedBurst.targetType}</AuditToken>
                            ) : null}
                            {selectedBurst.targetId ? (
                              <AuditToken>{selectedBurst.targetId}</AuditToken>
                            ) : null}
                          </div>
                          <div className="mt-4 flex flex-wrap gap-2">
                            <Button
                              type="button"
                              variant="outline"
                              onClick={() => setSelectedEntryId(selectedBurst.latestId)}
                              disabled={selectedEntry?.id === selectedBurst.latestId}
                            >
                              {t('audit_log.burst_focus_latest')}
                            </Button>
                            <Button
                              type="button"
                              variant="ghost"
                              onClick={() => setSelectedEntryId(selectedBurst.oldestId)}
                              disabled={selectedEntry?.id === selectedBurst.oldestId}
                            >
                              {t('audit_log.workspace_burst_focus_oldest')}
                            </Button>
                            {selectedBurstCanExpand ? (
                              <Button
                                type="button"
                                variant="ghost"
                                onClick={() => toggleBurstExpansion(selectedBurst.key)}
                              >
                                {selectedBurstExpanded
                                  ? t('audit_log.workspace_burst_collapse_rows')
                                  : t('audit_log.workspace_burst_expand_rows', {
                                      count: selectedBurst.count,
                                    })}
                              </Button>
                            ) : null}
                            {!selectedBurstHasActionScope ? (
                              <Button
                                type="button"
                                variant="ghost"
                                onClick={() =>
                                  handleInvestigate(
                                    'action',
                                    selectedBurst.action,
                                    selectedEntry?.id,
                                  )
                                }
                              >
                                {t('audit_log.workspace_burst_filter_action')}
                              </Button>
                            ) : null}
                            {selectedBurstTargetId && !selectedBurstHasTargetScope ? (
                              <Button
                                type="button"
                                variant="ghost"
                                onClick={() =>
                                  handleInvestigate(
                                    'targetId',
                                    selectedBurstTargetId,
                                    selectedEntry?.id,
                                  )
                                }
                              >
                                {t('audit_log.workspace_burst_filter_target')}
                              </Button>
                            ) : null}
                          </div>
                        </div>
                      ) : null}
                    </div>

                    <div className="rounded-[1.35rem] border border-border/70 bg-muted/[0.05] p-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.78)]">
                      <div className="flex flex-wrap items-start justify-between gap-3">
                        <div className="space-y-1">
                          <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                            {selectedEntry
                              ? t('audit_log.investigation_workspace_title')
                              : t('audit_log.selection_hint_title')}
                          </div>
                          <div className="text-base font-semibold text-foreground">
                            {selectedEntry
                              ? selectedEntry.summary || t('audit_log.summary_fallback')
                              : t('audit_log.selection_hint_title')}
                          </div>
                          <div className="max-w-[54ch] text-sm leading-6 text-muted-foreground">
                            {selectedEntry
                              ? t('audit_log.selection_hint_body', {
                                  action: selectedEntry.action,
                                  actorId: selectedEntry.actorId || t('audit_log.actor_unknown'),
                                })
                              : t('audit_log.selection_idle_body')}
                          </div>
                        </div>
                        {selectedEntry && selectedVisibleIndex >= 0 ? (
                          <FilterChip active>
                            {t('audit_log.focus_position', {
                              current: selectedVisibleIndex + 1,
                              total: visibleItems.length,
                            })}
                          </FilterChip>
                        ) : null}
                      </div>

                      {selectedEntry ? (
                        <>
                          <div className="mt-3 flex flex-wrap gap-2">
                            {selectedBurst && selectedBurstPosition ? (
                              <FilterChip active={false}>
                                {t('audit_log.workspace_burst_position', {
                                  current: selectedBurstPosition,
                                  total: selectedBurst.count,
                                })}
                              </FilterChip>
                            ) : null}
                            <FilterChip active={hasInvestigationScope}>
                              {hasInvestigationScope
                                ? t('audit_log.workspace_scope_active')
                                : t('audit_log.workspace_scope_idle')}
                            </FilterChip>
                          </div>

                          <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-2">
                            <InvestigationFactCard
                              label={t('audit_log.fields.action')}
                              value={selectedEntry.action}
                              mono
                            />
                            <InvestigationFactCard
                              label={t('audit_log.fields.actor')}
                              value={selectedEntry.actorId || t('audit_log.actor_unknown')}
                              hint={selectedEntry.actorType || undefined}
                              mono
                            />
                            <InvestigationFactCard
                              label={t('audit_log.fields.target')}
                              value={selectedEntry.targetId || t('audit_log.target_unknown')}
                              hint={selectedEntry.targetType || undefined}
                              mono
                            />
                            <InvestigationFactCard
                              label={t('audit_log.fields.created_at')}
                              value={formatAbsoluteTimestamp(selectedEntry.createdAt, locale)}
                              hint={formatDistanceToNow(new Date(selectedEntry.createdAt), {
                                addSuffix: true,
                                locale,
                              })}
                              mono
                            />
                          </div>

                          {selectedLocalMatchReasons.length > 0 ? (
                            <div className="mt-4 space-y-2">
                              <div className="text-[11px] font-semibold tracking-[0.16em] text-amber-700 uppercase">
                                {t('audit_log.focus_reasons_title')}
                              </div>
                              <div className="flex flex-wrap gap-2">
                                {selectedLocalMatchReasons.map((reason) => (
                                  <FilterChip key={`${selectedEntry.id}:${reason}`} active={false}>
                                    {t(`audit_log.local_match_${reason}`)}
                                  </FilterChip>
                                ))}
                              </div>
                            </div>
                          ) : null}

                          {selectedChangeSummary ? (
                            <div className="mt-4 space-y-2">
                              <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                                {t('audit_log.focus_paths_title')}
                              </div>
                              <div className="flex flex-wrap gap-2">
                                {selectedChangeSummary.paths.slice(0, 4).map((path) => (
                                  <SpotlightPathChip
                                    key={`focus:${selectedEntry.id}:${path}`}
                                    path={path}
                                    onSpotlight={handleSpotlightSignal}
                                  />
                                ))}
                                {selectedChangeSummary.count > 4 ? (
                                  <FilterChip active>
                                    {t('audit_log.change_more', {
                                      count: selectedChangeSummary.count - 4,
                                    })}
                                  </FilterChip>
                                ) : null}
                              </div>
                            </div>
                          ) : null}

                          <div className="mt-4 grid gap-3 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
                            <div className="space-y-2 rounded-2xl border border-border/70 bg-background/92 px-4 py-3">
                              <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                                {t('audit_log.workspace_browse_title')}
                              </div>
                              <div className="text-xs leading-5 text-muted-foreground">
                                {t('audit_log.shortcuts_inline')}
                              </div>
                              <div className="flex flex-wrap gap-2 pt-1">
                                <Button
                                  type="button"
                                  variant="ghost"
                                  onClick={() => handleMoveSelection(-1)}
                                  disabled={selectedVisibleIndex <= 0}
                                >
                                  {t('audit_log.previous_entry')}
                                </Button>
                                <Button
                                  type="button"
                                  variant="ghost"
                                  onClick={() => handleMoveSelection(1)}
                                  disabled={
                                    selectedVisibleIndex < 0 ||
                                    selectedVisibleIndex >= visibleItems.length - 1
                                  }
                                >
                                  {t('audit_log.next_entry')}
                                </Button>
                                <Button
                                  type="button"
                                  variant="outline"
                                  onClick={() => handleOpenDetails(selectedEntry.id)}
                                >
                                  {t('audit_log.open_details')}
                                </Button>
                              </div>
                            </div>

                            <div className="space-y-2 rounded-2xl border border-border/70 bg-background/92 px-4 py-3">
                              <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                                {t('audit_log.workspace_narrow_title')}
                              </div>
                              <div className="flex flex-wrap gap-2 pt-1">
                                {!selectedHasActionScope ? (
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    onClick={() =>
                                      handleInvestigate(
                                        'action',
                                        selectedEntry.action,
                                        selectedEntry.id,
                                      )
                                    }
                                  >
                                    {t('audit_log.same_action')}
                                  </Button>
                                ) : null}
                                {selectedEntry.targetId && !selectedHasTargetScope ? (
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    onClick={() =>
                                      handleInvestigate(
                                        'targetId',
                                        selectedEntry.targetId,
                                        selectedEntry.id,
                                      )
                                    }
                                  >
                                    {t('audit_log.same_target')}
                                  </Button>
                                ) : null}
                                {selectedEntry.actorId && !selectedHasActorScope ? (
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    onClick={() =>
                                      handleInvestigate(
                                        'actorId',
                                        selectedEntry.actorId,
                                        selectedEntry.id,
                                      )
                                    }
                                  >
                                    {t('audit_log.same_actor')}
                                  </Button>
                                ) : null}
                                {selectedHasActionScope &&
                                selectedHasTargetScope &&
                                selectedHasActorScope ? (
                                  <div className="text-xs leading-5 text-muted-foreground">
                                    {t('audit_log.workspace_narrow_done')}
                                  </div>
                                ) : null}
                              </div>
                            </div>
                          </div>

                          {hasInvestigationScope ? (
                            <div className="mt-4 flex flex-wrap items-center justify-between gap-3 rounded-2xl border border-border/70 bg-background/92 px-4 py-3">
                              <div className="space-y-1">
                                <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                                  {t('audit_log.workspace_scope_title')}
                                </div>
                                <div className="text-xs leading-5 text-muted-foreground">
                                  {t('audit_log.workspace_scope_body')}
                                </div>
                              </div>
                              <div className="flex flex-wrap gap-2">
                                {deferredLocalQuery ? (
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    onClick={handleClearLocalQuery}
                                  >
                                    {t('audit_log.local_empty_action')}
                                  </Button>
                                ) : null}
                                <Button type="button" variant="outline" onClick={handleReset}>
                                  <TimerReset className="mr-2 h-4 w-4" />
                                  {t('audit_log.workspace_scope_reset')}
                                </Button>
                              </div>
                            </div>
                          ) : null}
                        </>
                      ) : null}
                    </div>
                  </div>
                </div>

                <AuditLogStream
                  activeFilters={filters}
                  groups={streamGroups}
                  localQuery={deferredLocalQuery}
                  locale={locale}
                  onCopy={handleCopy}
                  expandedBurstKeys={expandedBurstKeys}
                  onFocus={setSelectedEntryId}
                  onInvestigate={handleInvestigate}
                  onInspect={handleOpenDetails}
                  onRegisterFocusButton={handleRegisterEntryFocusButton}
                  onSpotlightSignal={handleSpotlightSignal}
                  onToggleBurstExpansion={toggleBurstExpansion}
                  selectedEntryId={selectedEntryId}
                  selectedBurstKey={selectedBurstKeyToExpand}
                />

                {query.hasNextPage ? (
                  <div className="border-t border-border/70 px-6 py-5">
                    <Button
                      type="button"
                      variant="outline"
                      className="w-full sm:w-auto"
                      disabled={query.isFetchingNextPage}
                      onClick={() => void query.fetchNextPage()}
                    >
                      {query.isFetchingNextPage ? (
                        <>
                          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                          {t('app.loading')}
                        </>
                      ) : (
                        t('audit_log.load_more')
                      )}
                    </Button>
                  </div>
                ) : null}
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      <Sheet open={Boolean(inspectedEntry)} onOpenChange={(open) => !open && handleCloseDetails()}>
        <SheetContent
          side="right"
          className="w-full overflow-y-auto border-l border-border/70 bg-[linear-gradient(180deg,rgba(255,255,255,0.99),rgba(248,250,252,0.98))] sm:max-w-xl"
        >
          {inspectedEntry ? (
            <AuditDetailsSheet
              item={inspectedEntry}
              locale={locale}
              onCopy={handleCopy}
              onExport={() => void handleExport()}
              exporting={isExporting}
              onInvestigate={handleInvestigate}
              onSpotlightSignal={handleSpotlightSignal}
              currentIndex={inspectedEntryIndex}
              totalCount={detailItems.length}
              onNext={
                inspectedEntryIndex < detailItems.length - 1
                  ? () => handleNavigateDetails(1)
                  : undefined
              }
              onPrevious={inspectedEntryIndex > 0 ? () => handleNavigateDetails(-1) : undefined}
              activeFilters={filters}
            />
          ) : null}
        </SheetContent>
      </Sheet>
    </section>
  )
}

function AuditLogStream({
  activeFilters,
  expandedBurstKeys,
  groups,
  localQuery,
  locale,
  onCopy,
  onFocus,
  onInvestigate,
  onInspect,
  onRegisterFocusButton,
  onSpotlightSignal,
  onToggleBurstExpansion,
  selectedEntryId,
  selectedBurstKey,
}: {
  activeFilters: AuditLogFilters
  expandedBurstKeys: ReadonlySet<string>
  groups: AuditStreamGroup[]
  localQuery: string
  locale?: Locale
  onCopy: (value: string) => void
  onFocus: (id: string) => void
  onInvestigate: (kind: 'action' | 'actorId' | 'targetId', value: string, entryId?: string) => void
  onInspect: (id: string) => void
  onRegisterFocusButton: (id: string, node: HTMLButtonElement | null) => void
  onSpotlightSignal: (query: string) => void
  onToggleBurstExpansion: (burstKey: string) => void
  selectedEntryId: null | string
  selectedBurstKey: null | string
}) {
  const { t } = useTranslation()

  return (
    <div className="divide-y divide-border/70">
      {groups.map((group) => (
        <section key={group.key}>
          <div className="sticky top-0 z-10 border-y border-border/70 bg-background/92 px-6 py-2.5 backdrop-blur">
            <div className="flex items-center gap-2 text-xs font-semibold tracking-[0.16em] text-muted-foreground uppercase">
              <CalendarClock className="h-3.5 w-3.5 text-primary" />
              {group.label}
              <span className="rounded-full bg-primary/8 px-2 py-0.5 text-[11px] tracking-normal text-primary">
                {group.items.length}
              </span>
            </div>
          </div>
          <div className="divide-y divide-border/70">
            {group.blocks.map((block) => {
              if (block.kind === 'burst') {
                const hasSingleActionFilter =
                  normalizeArray(activeFilters.actions).length === 1 &&
                  normalizeArray(activeFilters.actions)[0] === block.burst.action
                const hasTargetFilter =
                  Boolean(block.burst.targetId) && activeFilters.targetId === block.burst.targetId
                const canExpandRows = block.burst.count > 3
                const isExpanded =
                  Boolean(block.burst.key && expandedBurstKeys.has(block.burst.key)) ||
                  selectedBurstKey === block.burst.key
                return (
                  <ActivityBurstCard
                    key={block.burst.key}
                    burst={block.burst}
                    canInvestigateAction={!hasSingleActionFilter}
                    canInvestigateTarget={!hasTargetFilter}
                    canExpandRows={canExpandRows}
                    isExpanded={isExpanded}
                    locale={locale}
                    onFocus={onFocus}
                    onInvestigate={onInvestigate}
                    onSpotlightSignal={onSpotlightSignal}
                    onToggleRows={() => onToggleBurstExpansion(block.burst.key)}
                  />
                )
              }

              const { item } = block
              const changeSummary = describeAuditChanges(item)
              const isSelected = selectedEntryId === item.id
              const localMatchReasons = localQuery ? describeLocalAuditMatch(item, localQuery) : []
              const shouldExpandBurst =
                Boolean(block.burstKey && expandedBurstKeys.has(block.burstKey)) ||
                selectedBurstKey === block.burstKey
              const shouldHideBurstFollower =
                Boolean(block.burstKey) &&
                !shouldExpandBurst &&
                (block.burstItemCount ?? 0) > 3 &&
                (block.burstItemIndex ?? 0) >= 3
              if (shouldHideBurstFollower) {
                return null
              }
              const useCompactBurstRow =
                Boolean(block.burstKey) && !isSelected && localMatchReasons.length === 0
              if (useCompactBurstRow) {
                return (
                  <CompactBurstEntryRow
                    key={item.id}
                    changeSummary={changeSummary}
                    item={item}
                    locale={locale}
                    onFocus={onFocus}
                    onInspect={onInspect}
                    onRegisterFocusButton={onRegisterFocusButton}
                    onSpotlightSignal={onSpotlightSignal}
                  />
                )
              }
              return (
                <article
                  key={item.id}
                  className={cn(
                    'px-6 py-5 transition-colors md:px-7',
                    isSelected ? 'bg-primary/[0.045]' : 'hover:bg-muted/[0.14]',
                  )}
                >
                  <div className="space-y-4">
                    <div className="flex flex-wrap items-start justify-between gap-3">
                      <div className="flex flex-wrap items-center gap-2">
                        <AuditToken className="border-primary/15 bg-primary/8 text-primary">
                          {item.action}
                        </AuditToken>
                        <AuditToken>{item.targetType || t('audit_log.target_unknown')}</AuditToken>
                        <AuditToken>{item.actorType || t('audit_log.actor_unknown')}</AuditToken>
                        {isSelected ? (
                          <AuditToken className="border-amber-200 bg-amber-50 text-amber-700">
                            {t('audit_log.current_focus')}
                          </AuditToken>
                        ) : null}
                        {changeSummary ? (
                          <AuditToken className="border-emerald-200 bg-emerald-50 text-emerald-700">
                            {t('audit_log.change_count', { count: changeSummary.count })}
                          </AuditToken>
                        ) : null}
                      </div>
                      <div className="rounded-full border border-border/70 bg-background/80 px-3 py-1 text-xs text-muted-foreground tabular-nums">
                        {formatDistanceToNow(new Date(item.createdAt), { addSuffix: true, locale })}
                      </div>
                    </div>

                    <button
                      type="button"
                      ref={(node) => onRegisterFocusButton(item.id, node)}
                      onClick={() => onFocus(item.id)}
                      aria-label={t('audit_log.focus_entry_aria', {
                        summary: item.summary || t('audit_log.summary_fallback'),
                      })}
                      className={cn(
                        'w-full rounded-2xl text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
                        isSelected ? 'bg-primary/[0.035]' : 'hover:bg-muted/[0.08]',
                      )}
                    >
                      <div className="space-y-2 rounded-2xl p-2">
                        <div className="text-lg font-semibold tracking-[-0.03em] text-foreground [text-wrap:balance]">
                          {item.summary || t('audit_log.summary_fallback')}
                        </div>
                        <p className="text-sm leading-6 text-muted-foreground">
                          <span className="font-medium text-foreground">
                            {t('audit_log.fields.created_at')}
                          </span>{' '}
                          <span className="font-mono text-[13px]">
                            {formatAbsoluteTimestamp(item.createdAt, locale)}
                          </span>
                          {' · '}
                          <span className="font-medium text-foreground">
                            {t('audit_log.actor_line')}
                          </span>{' '}
                          <span className="font-mono text-[13px]">
                            {item.actorId || t('audit_log.actor_unknown')}
                          </span>
                          {' · '}
                          <span className="font-medium text-foreground">
                            {t('audit_log.target_line')}
                          </span>{' '}
                          <span className="font-mono text-[13px]">
                            {item.targetId || t('audit_log.target_unknown')}
                          </span>
                        </p>
                      </div>
                    </button>

                    {localMatchReasons.length > 0 ? (
                      <div className="rounded-2xl border border-amber-200/80 bg-amber-50/78 px-4 py-3">
                        <div className="text-[11px] font-semibold tracking-[0.16em] text-amber-700 uppercase">
                          {t('audit_log.local_match_title')}
                        </div>
                        <div className="mt-2 flex flex-wrap gap-2">
                          {localMatchReasons.map((reason) => (
                            <FilterChip key={`${item.id}:${reason}`} active={false}>
                              {t(`audit_log.local_match_${reason}`)}
                            </FilterChip>
                          ))}
                        </div>
                      </div>
                    ) : null}

                    {changeSummary ? (
                      <div className="rounded-2xl border border-border/70 bg-muted/[0.14] px-4 py-3">
                        <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                          {t('audit_log.change_summary_title')}
                        </div>
                        <div className="mt-2 flex flex-wrap gap-2">
                          {changeSummary.paths.slice(0, 5).map((path) => (
                            <SpotlightPathChip
                              key={path}
                              path={path}
                              onSpotlight={onSpotlightSignal}
                            />
                          ))}
                          {changeSummary.count > 5 ? (
                            <FilterChip active>
                              {t('audit_log.change_more', { count: changeSummary.count - 5 })}
                            </FilterChip>
                          ) : null}
                        </div>
                      </div>
                    ) : null}

                    <div className="flex flex-wrap gap-2">
                      <Button type="button" variant="outline" onClick={() => onInspect(item.id)}>
                        {t('audit_log.open_details')}
                      </Button>
                      {item.targetId ? (
                        <Button type="button" variant="ghost" onClick={() => onCopy(item.targetId)}>
                          <Copy className="h-4 w-4" />
                          {t('audit_log.copy_target_id')}
                        </Button>
                      ) : null}
                      {item.actorId ? (
                        <Button type="button" variant="ghost" onClick={() => onCopy(item.actorId)}>
                          <Copy className="h-4 w-4" />
                          {t('audit_log.copy_actor_id')}
                        </Button>
                      ) : null}
                    </div>
                  </div>
                </article>
              )
            })}
          </div>
        </section>
      ))}
    </div>
  )
}

function AuditDetailsSheet({
  activeFilters,
  currentIndex,
  item,
  locale,
  onNext,
  onPrevious,
  onCopy,
  onExport,
  exporting,
  onInvestigate,
  onSpotlightSignal,
  totalCount,
}: {
  activeFilters: AuditLogFilters
  currentIndex: number
  item: AuditLogEntry
  locale?: Locale
  onNext?: () => void
  onPrevious?: () => void
  onCopy: (value: string) => void
  onExport: () => void
  exporting: boolean
  onInvestigate: (kind: 'action' | 'actorId' | 'targetId', value: string, entryId?: string) => void
  onSpotlightSignal: (query: string) => void
  totalCount: number
}) {
  const { t } = useTranslation()
  const changeSummary = describeAuditChanges(item)
  const hasActionScope = hasSingleActionFilter(activeFilters, item.action)
  const hasTargetScope = hasTargetFilter(activeFilters, item.targetId)
  const hasActorScope = hasActorFilter(activeFilters, item.actorId)

  return (
    <>
      <SheetHeader className="border-b border-border/70 bg-background/95">
        <div className="flex flex-wrap items-center gap-2">
          <AuditToken className="border-primary/15 bg-primary/8 text-primary">
            {item.action}
          </AuditToken>
          <AuditToken>{item.targetType || t('audit_log.target_unknown')}</AuditToken>
          <AuditToken>{item.actorType || t('audit_log.actor_unknown')}</AuditToken>
        </div>
        <SheetTitle className="text-xl tracking-[-0.03em]">
          {item.summary || t('audit_log.summary_fallback')}
        </SheetTitle>
        <SheetDescription className="leading-6">
          {t('audit_log.details_intro', {
            time: formatAbsoluteTimestamp(item.createdAt, locale),
          })}
        </SheetDescription>
        <div className="flex flex-wrap items-center justify-between gap-3 pt-2">
          <div className="rounded-full border border-border/70 bg-background/88 px-3 py-1 text-xs text-muted-foreground tabular-nums">
            {t('audit_log.entry_position', {
              current: currentIndex + 1,
              total: totalCount,
            })}
          </div>
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={onPrevious}
              disabled={!onPrevious}
            >
              {t('audit_log.previous_entry')}
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={onNext} disabled={!onNext}>
              {t('audit_log.next_entry')}
            </Button>
          </div>
        </div>
        <div className="flex flex-wrap gap-2 pt-2">
          <Button type="button" onClick={onExport} disabled={exporting}>
            <Download className="h-4 w-4" />
            {exporting ? t('audit_log.exporting') : t('audit_log.export')}
          </Button>
          {item.actorId ? (
            <Button type="button" variant="outline" onClick={() => onCopy(item.actorId)}>
              <Copy className="h-4 w-4" />
              {t('audit_log.copy_actor_id')}
            </Button>
          ) : null}
          {item.targetId ? (
            <Button type="button" variant="outline" onClick={() => onCopy(item.targetId)}>
              <Copy className="h-4 w-4" />
              {t('audit_log.copy_target_id')}
            </Button>
          ) : null}
        </div>
      </SheetHeader>

      <div className="space-y-5 px-4 pb-6">
        <div className="rounded-2xl border border-border/70 bg-muted/[0.14] p-4">
          <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
            {t('audit_log.investigate_title')}
          </div>
          <div className="mt-2 text-sm leading-6 text-muted-foreground">
            {t('audit_log.investigate_body')}
          </div>
          <div className="mt-3 flex flex-wrap gap-2">
            {!hasActionScope ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => onInvestigate('action', item.action, item.id)}
              >
                {t('audit_log.same_action')}
              </Button>
            ) : null}
            {item.targetId && !hasTargetScope ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => onInvestigate('targetId', item.targetId, item.id)}
              >
                {t('audit_log.same_target')}
              </Button>
            ) : null}
            {item.actorId && !hasActorScope ? (
              <Button
                type="button"
                variant="outline"
                onClick={() => onInvestigate('actorId', item.actorId, item.id)}
              >
                {t('audit_log.same_actor')}
              </Button>
            ) : null}
          </div>
        </div>

        <div className="grid gap-4 pt-4 sm:grid-cols-2">
          <EntityPanel
            title={t('audit_log.fields.actor')}
            primary={item.actorType || t('audit_log.actor_unknown')}
            secondary={item.actorId || t('audit_log.actor_unknown')}
            auxiliary={item.actorEmail}
            onCopy={item.actorId ? () => onCopy(item.actorId) : undefined}
          />
          <EntityPanel
            title={t('audit_log.fields.target')}
            primary={item.targetType || t('audit_log.target_unknown')}
            secondary={item.targetId || t('audit_log.target_unknown')}
            onCopy={item.targetId ? () => onCopy(item.targetId) : undefined}
          />
        </div>

        <div className="rounded-2xl border border-border/70 bg-muted/[0.14] p-4">
          <div className="mb-3 text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
            {t('audit_log.details_timeline')}
          </div>
          <div className="space-y-2 text-sm">
            <div className="flex items-center justify-between gap-3">
              <span className="text-muted-foreground">{t('audit_log.fields.created_at')}</span>
              <span className="font-medium text-foreground tabular-nums">
                {formatAbsoluteTimestamp(item.createdAt, locale)}
              </span>
            </div>
            <div className="flex items-center justify-between gap-3">
              <span className="text-muted-foreground">{t('audit_log.details_relative_time')}</span>
              <span className="font-medium text-foreground">
                {formatDistanceToNow(new Date(item.createdAt), { addSuffix: true, locale })}
              </span>
            </div>
          </div>
        </div>

        {changeSummary ? (
          <div className="rounded-2xl border border-border/70 bg-muted/[0.14] p-4">
            <div className="mb-3 text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
              {t('audit_log.change_summary_title')}
            </div>
            <div className="mb-3 text-sm font-medium text-foreground">
              {t('audit_log.change_count', { count: changeSummary.count })}
            </div>
            <div className="flex flex-wrap gap-2">
              {changeSummary.paths.map((path) => (
                <SpotlightPathChip key={path} path={path} onSpotlight={onSpotlightSignal} />
              ))}
            </div>
          </div>
        ) : null}

        <RequestMetadata item={item} />

        <div className="grid gap-4">
          <JsonBlock label={t('audit_log.before')} value={item.beforeJson} />
          <JsonBlock label={t('audit_log.after')} value={item.afterJson} />
        </div>
      </div>
    </>
  )
}

function AuditLogSkeleton() {
  return (
    <div className="space-y-0">
      {[0, 1, 2].map((index) => (
        <div key={index} className="space-y-4 border-b border-border/70 px-6 py-5 md:px-7">
          <div className="flex flex-wrap gap-2">
            <Skeleton className="h-6 w-28 rounded-full" />
            <Skeleton className="h-6 w-20 rounded-full" />
            <Skeleton className="h-6 w-16 rounded-full" />
          </div>
          <Skeleton className="h-7 w-72" />
          <Skeleton className="h-5 w-80" />
          <Skeleton className="h-16 w-full rounded-2xl" />
          <div className="flex gap-2">
            <Skeleton className="h-9 w-24" />
            <Skeleton className="h-9 w-24" />
          </div>
        </div>
      ))}
    </div>
  )
}

function FilterField({
  children,
  htmlFor,
  label,
}: {
  children: React.ReactNode
  htmlFor: string
  label: string
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={htmlFor} className="text-[11px] font-semibold tracking-[0.16em] uppercase">
        {label}
      </Label>
      {children}
    </div>
  )
}

function StatCard({
  className,
  hint,
  icon: Icon,
  label,
  value,
}: {
  className?: string
  hint: string
  icon: LucideIcon
  label: string
  value: string
}) {
  return (
    <div
      className={cn(
        'rounded-[1.05rem] border border-white/70 bg-white/84 p-3.5 shadow-[0_8px_24px_-24px_rgba(15,23,42,0.4)] backdrop-blur',
        className,
      )}
    >
      <div className="flex items-center justify-between gap-3">
        <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
          {label}
        </div>
        <div className="rounded-full bg-primary/10 p-1.5 text-primary">
          <Icon className="h-3.5 w-3.5" />
        </div>
      </div>
      <div className="mt-3 text-[1.65rem] font-semibold tracking-[-0.045em] text-foreground [text-wrap:balance]">
        {value}
      </div>
      <div className="mt-1 text-[11px] leading-5 text-muted-foreground">{hint}</div>
    </div>
  )
}

function InvestigationFactCard({
  hint,
  label,
  mono = false,
  value,
}: {
  hint?: string
  label: string
  mono?: boolean
  value: string
}) {
  return (
    <div className="rounded-2xl border border-border/70 bg-background/92 px-3.5 py-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.76)]">
      <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {label}
      </div>
      <div
        className={cn(
          'mt-1.5 break-all text-sm font-medium text-foreground',
          mono && 'font-mono text-[12px]',
        )}
      >
        {value}
      </div>
      {hint ? <div className="mt-1 text-[11px] text-muted-foreground">{hint}</div> : null}
    </div>
  )
}

function AuditActionMultiSelect({
  onChange,
  selected,
  triggerId,
}: {
  onChange: (actions: string[]) => void
  selected: string[]
  triggerId: string
}) {
  const { t } = useTranslation()
  const [query, setQuery] = useState('')
  const deferredQuery = useDeferredValue(query.trim().toLowerCase())
  const summary =
    selected.length === 0
      ? t('audit_log.filters_actions_placeholder')
      : t('audit_log.filters_actions_selected', { count: selected.length })
  const visibleActions = deferredQuery
    ? auditActionOptions.filter((action) => action.toLowerCase().includes(deferredQuery))
    : auditActionOptions

  const toggle = (action: string) => {
    onChange(
      selected.includes(action)
        ? selected.filter((item) => item !== action)
        : [...selected, action].sort(),
    )
  }

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          id={triggerId}
          type="button"
          variant="outline"
          className="h-11 w-full justify-between rounded-xl border-border/70 bg-background text-left shadow-none"
          aria-label={t('audit_log.fields.action')}
        >
          <span className="truncate">{summary}</span>
          <ChevronDown className="ml-2 h-4 w-4 shrink-0" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 rounded-2xl p-3">
        <div className="mb-3 space-y-3">
          <div className="flex items-center justify-between gap-2">
            <div className="text-sm font-medium">{t('audit_log.fields.action')}</div>
            <Button
              type="button"
              variant="ghost"
              size="sm"
              disabled={selected.length === 0}
              onClick={() => onChange([])}
            >
              <X className="mr-1 h-3.5 w-3.5" />
              {t('audit_log.filters_actions_clear')}
            </Button>
          </div>
          <div className="relative">
            <Search className="pointer-events-none absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="pl-9"
              placeholder={t('audit_log.actions_search_placeholder')}
            />
          </div>
        </div>
        <div className="max-h-72 space-y-2 overflow-y-auto pr-1">
          {visibleActions.map((action) => {
            const id = `audit-action-${action.replaceAll('.', '-')}`
            return (
              <label
                key={action}
                htmlFor={id}
                className="flex cursor-pointer items-center gap-3 rounded-xl border border-transparent px-2.5 py-2 transition hover:border-border/70 hover:bg-muted/40"
              >
                <Checkbox
                  id={id}
                  checked={selected.includes(action)}
                  onCheckedChange={() => toggle(action)}
                />
                <span className="font-mono text-xs">{action}</span>
              </label>
            )
          })}
          {visibleActions.length === 0 ? (
            <div className="rounded-xl border border-dashed border-border/70 px-3 py-4 text-center text-xs text-muted-foreground">
              {t('audit_log.actions_search_empty')}
            </div>
          ) : null}
        </div>
      </PopoverContent>
    </Popover>
  )
}

function RequestMetadata({ item }: { item: AuditLogEntry }) {
  const { t } = useTranslation()
  const metadata = [
    [t('audit_log.fields.actor_email'), item.actorEmail],
    [t('audit_log.fields.actor_ip'), item.actorIp],
    [t('audit_log.fields.actor_user_agent'), item.actorUserAgent],
  ].filter(([, value]) => value)
  if (metadata.length === 0) return null

  return (
    <div className="rounded-2xl border border-border/70 bg-muted/[0.14] p-4">
      <div className="mb-3 text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {t('audit_log.request_metadata')}
      </div>
      <dl className="space-y-3 text-xs">
        {metadata.map(([label, value]) => (
          <div key={label} className="grid gap-1 sm:grid-cols-[132px_1fr] sm:items-start">
            <dt className="text-muted-foreground">{label}</dt>
            <dd className="break-all font-mono">{value}</dd>
          </div>
        ))}
      </dl>
    </div>
  )
}

function JsonBlock({ label, value }: { label: string; value?: string }) {
  if (!value) return null

  return (
    <div className="rounded-2xl border border-border/70 bg-muted/[0.14] p-4">
      <div className="mb-2 text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {label}
      </div>
      <pre className="overflow-x-auto rounded-xl bg-background/92 p-3 text-xs leading-6 text-foreground shadow-[inset_0_1px_0_rgba(255,255,255,0.7)]">
        {formatJson(value)}
      </pre>
    </div>
  )
}

function EntityPanel({
  auxiliary,
  onCopy,
  primary,
  secondary,
  title,
}: {
  auxiliary?: string
  onCopy?: () => void
  primary: string
  secondary: string
  title: string
}) {
  const { t } = useTranslation()

  return (
    <div className="rounded-2xl border border-border/70 bg-muted/[0.14] p-4">
      <div className="flex items-center justify-between gap-3">
        <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
          {title}
        </div>
        {onCopy ? (
          <Button type="button" variant="ghost" size="xs" onClick={onCopy}>
            <Copy className="h-3.5 w-3.5" />
            {t('common.copy')}
          </Button>
        ) : null}
      </div>
      <div className="mt-3 text-sm font-medium text-foreground">{primary}</div>
      <div className="mt-1 break-all font-mono text-xs text-muted-foreground">{secondary}</div>
      {auxiliary ? (
        <div className="mt-2 break-all text-xs text-muted-foreground">{auxiliary}</div>
      ) : null}
    </div>
  )
}

function FilterChip({ active, children }: { active: boolean; children: React.ReactNode }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border px-3 py-1 text-xs transition-colors',
        active
          ? 'border-primary/18 bg-primary/9 text-primary'
          : 'border-border/60 bg-background/80 text-muted-foreground',
      )}
    >
      {children}
    </span>
  )
}

function CompactInvestigationSignalCard({
  emptyLabel,
  onSelect,
  signal,
  title,
}: {
  emptyLabel: string
  onSelect: (value: string) => void
  signal?: InvestigationSignal
  title: string
}) {
  return (
    <div className="rounded-2xl border border-border/70 bg-white/88 p-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.8)]">
      <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {title}
      </div>
      {signal ? (
        <button
          type="button"
          onClick={() => onSelect(signal.value)}
          aria-label={`${signal.value} ${signal.count}`}
          className="mt-2 inline-flex max-w-full items-center gap-2 rounded-full border border-border/70 bg-background/92 px-2.5 py-1.5 text-left text-xs text-foreground transition hover:border-primary/30 hover:bg-primary/[0.05] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        >
          <span className="max-w-[11rem] truncate font-mono text-[11px]">{signal.value}</span>
          <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[11px] font-semibold text-primary tabular-nums">
            {signal.count}
          </span>
        </button>
      ) : (
        <div className="mt-2 text-xs leading-5 text-muted-foreground">{emptyLabel}</div>
      )}
    </div>
  )
}

function ActivityBurstCard({
  burst,
  canInvestigateAction,
  canInvestigateTarget,
  canExpandRows,
  isExpanded,
  locale,
  onFocus,
  onInvestigate,
  onSpotlightSignal,
  onToggleRows,
}: {
  burst: AuditActivityBurst
  canInvestigateAction: boolean
  canInvestigateTarget: boolean
  canExpandRows: boolean
  isExpanded: boolean
  locale?: Locale
  onFocus: (id: string) => void
  onInvestigate: (kind: 'action' | 'actorId' | 'targetId', value: string, entryId?: string) => void
  onSpotlightSignal: (query: string) => void
  onToggleRows: () => void
}) {
  const { t } = useTranslation()
  const targetId = burst.targetId
  const hiddenCount = Math.max(burst.count - 3, 0)

  return (
    <div className="border-b border-border/70 bg-[linear-gradient(180deg,rgba(255,247,237,0.78),rgba(255,255,255,0.94))] px-6 py-4 md:px-7">
      <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_auto] xl:items-start">
        <div className="space-y-3">
          <div className="inline-flex items-center rounded-full border border-primary/18 bg-primary/9 px-3 py-1 text-[11px] font-semibold tracking-[0.16em] text-primary uppercase">
            {t('audit_log.burst_title', { count: burst.count })}
          </div>
          <div className="text-sm leading-6 text-muted-foreground">
            {burst.targetId
              ? t('audit_log.burst_body_targeted', {
                  action: burst.action,
                  from: formatAbsoluteTimestamp(burst.oldestCreatedAt, locale),
                  target: burst.targetId,
                  to: formatAbsoluteTimestamp(burst.latestCreatedAt, locale),
                })
              : t('audit_log.burst_body_generic', {
                  action: burst.action,
                  from: formatAbsoluteTimestamp(burst.oldestCreatedAt, locale),
                  to: formatAbsoluteTimestamp(burst.latestCreatedAt, locale),
                })}
          </div>
          <div className="flex flex-wrap gap-2">
            <AuditToken className="border-primary/15 bg-primary/8 text-primary">
              {burst.action}
            </AuditToken>
            {burst.targetType ? <AuditToken>{burst.targetType}</AuditToken> : null}
            {burst.targetId ? <AuditToken>{burst.targetId}</AuditToken> : null}
          </div>
          {burst.paths.length > 0 ? (
            <div className="space-y-2">
              <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
                {t('audit_log.burst_paths_title')}
              </div>
              <div className="flex flex-wrap gap-2">
                {burst.paths.map((path) => (
                  <SpotlightPathChip
                    key={`${burst.key}:${path}`}
                    path={path}
                    onSpotlight={onSpotlightSignal}
                  />
                ))}
              </div>
            </div>
          ) : null}
          {canExpandRows ? (
            <div className="rounded-2xl border border-border/60 bg-background/86 px-3 py-2 text-xs leading-5 text-muted-foreground">
              {isExpanded
                ? t('audit_log.burst_expanded_hint', { count: burst.count })
                : t('audit_log.burst_collapsed_hint', { count: hiddenCount })}
            </div>
          ) : null}
        </div>

        <div className="flex flex-wrap gap-2 xl:justify-end">
          <Button type="button" variant="outline" onClick={() => onFocus(burst.latestId)}>
            {t('audit_log.burst_focus_latest')}
          </Button>
          {canExpandRows ? (
            <Button type="button" variant="ghost" onClick={onToggleRows}>
              {isExpanded
                ? t('audit_log.burst_collapse_rows')
                : t('audit_log.burst_expand_rows', { count: burst.count })}
            </Button>
          ) : null}
          {canInvestigateAction ? (
            <Button
              type="button"
              variant="ghost"
              onClick={() => onInvestigate('action', burst.action, burst.latestId)}
            >
              {t('audit_log.burst_filter_action')}
            </Button>
          ) : null}
          {targetId && canInvestigateTarget ? (
            <Button
              type="button"
              variant="ghost"
              onClick={() => onInvestigate('targetId', targetId, burst.latestId)}
            >
              {t('audit_log.burst_filter_target')}
            </Button>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function CompactBurstEntryRow({
  changeSummary,
  item,
  locale,
  onFocus,
  onInspect,
  onRegisterFocusButton,
  onSpotlightSignal,
}: {
  changeSummary: null | ReturnType<typeof describeAuditChanges>
  item: AuditLogEntry
  locale?: Locale
  onFocus: (id: string) => void
  onInspect: (id: string) => void
  onRegisterFocusButton: (id: string, node: HTMLButtonElement | null) => void
  onSpotlightSignal: (query: string) => void
}) {
  const { t } = useTranslation()

  return (
    <article className="px-6 py-3 md:px-7">
      <div className="rounded-2xl border border-border/60 bg-background/88 px-3 py-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.75)] transition hover:border-primary/20 hover:bg-primary/[0.02]">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <button
            type="button"
            ref={(node) => onRegisterFocusButton(item.id, node)}
            onClick={() => onFocus(item.id)}
            aria-label={t('audit_log.focus_entry_aria', {
              summary: item.summary || t('audit_log.summary_fallback'),
            })}
            className="min-w-0 flex-1 rounded-xl p-1 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
          >
            <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
              <span className="font-mono tabular-nums">
                {formatAbsoluteTimestamp(item.createdAt, locale)}
              </span>
              <span>
                {formatDistanceToNow(new Date(item.createdAt), { addSuffix: true, locale })}
              </span>
              {changeSummary ? (
                <FilterChip active={false}>
                  {t('audit_log.compact_change_count', { count: changeSummary.count })}
                </FilterChip>
              ) : null}
            </div>
            <div className="mt-2 text-sm font-medium text-foreground">
              {item.summary || t('audit_log.summary_fallback')}
            </div>
            <div className="mt-1 text-xs leading-5 text-muted-foreground">
              {t('audit_log.actor_line')} {item.actorId || t('audit_log.actor_unknown')}
              {' · '}
              {t('audit_log.target_line')} {item.targetId || t('audit_log.target_unknown')}
            </div>
          </button>
          <div className="flex flex-wrap items-center justify-end gap-2">
            {changeSummary?.paths.slice(0, 2).map((path) => (
              <SpotlightPathChip
                key={`${item.id}:${path}`}
                path={path}
                onSpotlight={onSpotlightSignal}
              />
            ))}
            <Button type="button" size="sm" variant="ghost" onClick={() => onInspect(item.id)}>
              {t('audit_log.open_details')}
            </Button>
          </div>
        </div>
      </div>
    </article>
  )
}

function SpotlightPathChip({
  onSpotlight,
  path,
}: {
  onSpotlight: (query: string) => void
  path: string
}) {
  const { t } = useTranslation()

  return (
    <button
      type="button"
      onClick={() => onSpotlight(path)}
      aria-label={t('audit_log.spotlight_path', { path })}
      className="inline-flex items-center rounded-full border border-primary/18 bg-primary/9 px-3 py-1 text-xs text-primary transition hover:border-primary/30 hover:bg-primary/[0.14] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
    >
      {path}
    </button>
  )
}

function InvestigationSignalGroup({
  emptyLabel,
  items,
  onSelect,
  title,
}: {
  emptyLabel: string
  items: InvestigationSignal[]
  onSelect: (value: string) => void
  title: string
}) {
  return (
    <div className="rounded-2xl border border-border/70 bg-white/88 p-4 shadow-[inset_0_1px_0_rgba(255,255,255,0.8)]">
      <div className="text-[11px] font-semibold tracking-[0.16em] text-muted-foreground uppercase">
        {title}
      </div>
      {items.length > 0 ? (
        <div className="mt-3 flex flex-wrap gap-2">
          {items.map((item) => (
            <button
              key={`${title}:${item.value}`}
              type="button"
              onClick={() => onSelect(item.value)}
              aria-label={`${item.value} ${item.count}`}
              className="inline-flex items-center gap-2 rounded-full border border-border/70 bg-background/92 px-3 py-1.5 text-left text-xs text-foreground transition hover:border-primary/30 hover:bg-primary/[0.05] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
            >
              <span className="max-w-[16rem] truncate font-mono text-[11px]">{item.value}</span>
              <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[11px] font-semibold text-primary tabular-nums">
                {item.count}
              </span>
            </button>
          ))}
        </div>
      ) : (
        <div className="mt-3 text-xs leading-5 text-muted-foreground">{emptyLabel}</div>
      )}
    </div>
  )
}

function RemovableFilterChip({ label, onRemove }: { label: string; onRemove: () => void }) {
  const { t } = useTranslation()

  return (
    <span className="inline-flex items-center gap-1 rounded-full border border-primary/18 bg-primary/9 px-2.5 py-1 text-xs text-primary">
      <span>{label}</span>
      <button
        type="button"
        className="rounded-full p-0.5 text-primary transition hover:bg-primary/12 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        onClick={onRemove}
        aria-label={t('audit_log.remove_filter', { label })}
      >
        <X className="h-3 w-3" />
      </button>
    </span>
  )
}

function AuditToken({ children, className }: { children: React.ReactNode; className?: string }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full border border-border/70 bg-muted/22 px-3 py-1 font-mono text-[11px] tracking-[0.02em] text-muted-foreground',
        className,
      )}
    >
      {children}
    </span>
  )
}

function buildPresets(t: (key: string) => string): AuditPreset[] {
  return [
    {
      id: 'recent-24h',
      label: t('audit_log.presets.recent_24h'),
      description: t('audit_log.presets.recent_24h_body'),
      values: { fromValue: toLocalDateTimeInput(Date.now() - 24 * 60 * 60 * 1000) },
    },
    {
      id: 'member-access',
      label: t('audit_log.presets.member_access'),
      description: t('audit_log.presets.member_access_body'),
      values: {
        selectedActions: ['member.invite', 'member.remove', 'member.update_role'],
      },
    },
    {
      id: 'integration-changes',
      label: t('audit_log.presets.integration_changes'),
      description: t('audit_log.presets.integration_changes_body'),
      values: {
        selectedActions: [
          'api_key.create',
          'api_key.revoke',
          'digest_subscription.upsert',
          'digest_subscription.delete',
          'inbound_source.create',
          'inbound_source.delete',
          'inbound_source.pause',
          'inbound_source.resume',
          'inbound_source.rotate_secret',
          'notify_target.create',
          'notify_target.delete',
          'notify_target.update',
        ],
      },
    },
    {
      id: 'ai-runtime',
      label: t('audit_log.presets.ai_runtime'),
      description: t('audit_log.presets.ai_runtime_body'),
      values: {
        selectedActions: [
          'enrich_config.update',
          'enrich_config.activate_version',
          'llm_ability.upsert',
          'llm_ability.delete',
          'llm_channel.create',
          'llm_channel.update',
          'llm_channel.delete',
          'llm_channel.test',
          'llm_route.upsert',
          'llm_route.delete',
        ],
      },
    },
  ]
}

function draftToFilters(draft: AuditDraftState): AuditLogFilters {
  return {
    actions: draft.selectedActions.length > 0 ? draft.selectedActions : undefined,
    actorId: draft.actorId.trim() || undefined,
    from: localDateTimeToISO(draft.fromValue),
    targetId: draft.targetId.trim() || undefined,
    targetType: draft.targetType.trim() || undefined,
    to: localDateTimeToISO(draft.toValue),
  }
}

function filtersToDraft(filters: AuditLogFilters): AuditDraftState {
  return {
    selectedActions: filters.actions ?? [],
    targetType: filters.targetType ?? '',
    targetId: filters.targetId ?? '',
    actorId: filters.actorId ?? '',
    fromValue: isoToLocalDateTimeInput(filters.from),
    toValue: isoToLocalDateTimeInput(filters.to),
  }
}

function areFiltersEqual(left: AuditLogFilters, right: AuditLogFilters) {
  return (
    normalizeArray(left.actions).join('|') === normalizeArray(right.actions).join('|') &&
    (left.actorId ?? '') === (right.actorId ?? '') &&
    (left.from ?? '') === (right.from ?? '') &&
    (left.targetId ?? '') === (right.targetId ?? '') &&
    (left.targetType ?? '') === (right.targetType ?? '') &&
    (left.to ?? '') === (right.to ?? '')
  )
}

function normalizeArray(values?: string[]) {
  return [...(values ?? [])].sort()
}

function hasSingleActionFilter(filters: AuditLogFilters, action: string) {
  const actions = normalizeArray(filters.actions)
  return actions.length === 1 && actions[0] === action
}

function hasTargetFilter(filters: AuditLogFilters, targetId?: string) {
  return Boolean(targetId) && filters.targetId === targetId
}

function hasActorFilter(filters: AuditLogFilters, actorId?: string) {
  return Boolean(actorId) && filters.actorId === actorId
}

function countActiveFilters(filters: AuditLogFilters) {
  return [
    filters.actions?.length ?? 0,
    filters.actorId ? 1 : 0,
    filters.from ? 1 : 0,
    filters.targetId ? 1 : 0,
    filters.targetType ? 1 : 0,
    filters.to ? 1 : 0,
  ].reduce((sum, count) => sum + (count > 0 ? 1 : 0), 0)
}

function describeAppliedFilters(
  filters: AuditLogFilters,
  localQuery: string,
  t: (key: string, options?: Record<string, unknown>) => string,
) {
  const summary: AppliedFilterDescriptor[] = []
  if (filters.actions?.length) {
    summary.push({
      key: 'actions',
      label: t('audit_log.filters_actions_selected', { count: filters.actions.length }),
    })
  }
  if (filters.targetType)
    summary.push({
      key: 'targetType',
      label: `${t('audit_log.fields.target_type')}: ${filters.targetType}`,
    })
  if (filters.targetId)
    summary.push({
      key: 'targetId',
      label: `${t('audit_log.fields.target_id')}: ${filters.targetId}`,
    })
  if (filters.actorId)
    summary.push({
      key: 'actorId',
      label: `${t('audit_log.fields.actor_id')}: ${filters.actorId}`,
    })
  if (filters.from)
    summary.push({
      key: 'from',
      label: `${t('audit_log.filters_from')}: ${formatDateChip(filters.from)}`,
    })
  if (filters.to)
    summary.push({
      key: 'to',
      label: `${t('audit_log.filters_to')}: ${formatDateChip(filters.to)}`,
    })
  if (localQuery.trim())
    summary.push({
      key: 'localQuery',
      label: `${t('audit_log.local_query_label')}: ${localQuery.trim()}`,
    })
  return summary
}

function buildInvestigationSignals(items: AuditLogEntry[]) {
  return {
    actions: countTopSignals(items.map((item) => item.action)),
    actors: countTopSignals(items.map((item) => item.actorId)),
    paths: countTopSignals(
      items.flatMap((item) => {
        const changeSummary = describeAuditChanges(item)
        return changeSummary?.paths ?? []
      }),
      4,
    ),
    targets: countTopSignals(items.map((item) => item.targetId)),
  }
}

function countTopSignals(values: Array<string | undefined>, limit = 3): InvestigationSignal[] {
  const counts = new Map<string, number>()
  for (const rawValue of values) {
    const value = rawValue?.trim()
    if (!value) continue
    counts.set(value, (counts.get(value) ?? 0) + 1)
  }

  return [...counts.entries()]
    .sort((left, right) => right[1] - left[1] || left[0].localeCompare(right[0]))
    .slice(0, limit)
    .map(([value, count]) => ({ value, count }))
}

function readAuditLogStateFromUrl(): AuditUrlState {
  if (typeof window === 'undefined') {
    return {
      filters: {},
      inspectedEntryId: null,
      localQuery: '',
    }
  }

  const params = new URLSearchParams(window.location.search)
  const repeatedActions = params.getAll('action')
  const csvActions = (params.get('actions') ?? '')
    .split(',')
    .map((value) => value.trim())
    .filter(Boolean)
  const actions = [...new Set([...repeatedActions, ...csvActions])]

  return {
    filters: {
      actions: actions.length > 0 ? actions : undefined,
      actorId: emptyToUndefined(params.get('actorId')),
      from: emptyToUndefined(params.get('from')),
      targetId: emptyToUndefined(params.get('targetId')),
      targetType: emptyToUndefined(params.get('targetType')),
      to: emptyToUndefined(params.get('to')),
    },
    inspectedEntryId: emptyToUndefined(params.get('entry')) ?? null,
    localQuery: params.get('q')?.trim() ?? '',
  }
}

function writeAuditLogStateToUrl(state: AuditUrlState, mode: AuditHistoryMode) {
  if (typeof window === 'undefined') return

  const nextUrl = buildAuditLogUrl(state)
  const currentUrl = `${window.location.pathname}${window.location.search}${window.location.hash}`
  if (nextUrl === currentUrl) return

  if (mode === 'push') {
    window.history.pushState({}, '', nextUrl)
    return
  }
  window.history.replaceState({}, '', nextUrl)
}

function buildAuditLogUrl(state: AuditUrlState) {
  const search = buildAuditLogSearchParams(state).toString()
  const path =
    typeof window === 'undefined' ? '/administration/audit-log' : window.location.pathname
  return search ? `${path}?${search}` : path
}

function buildAuditLogSearchParams({ filters, inspectedEntryId, localQuery }: AuditUrlState) {
  const params = new URLSearchParams()
  for (const action of filters.actions ?? []) {
    const trimmed = action.trim()
    if (trimmed) params.append('action', trimmed)
  }
  if (filters.actorId) params.set('actorId', filters.actorId)
  if (filters.from) params.set('from', filters.from)
  if (filters.targetId) params.set('targetId', filters.targetId)
  if (filters.targetType) params.set('targetType', filters.targetType)
  if (filters.to) params.set('to', filters.to)
  if (localQuery.trim()) params.set('q', localQuery.trim())
  if (inspectedEntryId) params.set('entry', inspectedEntryId)
  return params
}

function buildStreamGroups(items: AuditLogEntry[], locale?: Locale): AuditStreamGroup[] {
  return groupEntriesByDay(items, locale).map((group) => ({
    ...group,
    blocks: buildStreamBlocks(group.items),
  }))
}

function groupEntriesByDay(items: AuditLogEntry[], locale?: Locale) {
  const groups = new Map<string, { key: string; label: string; items: AuditLogEntry[] }>()
  for (const item of items) {
    const date = new Date(item.createdAt)
    const key = format(date, 'yyyy-MM-dd')
    if (!groups.has(key)) {
      groups.set(key, {
        key,
        label: formatDayLabel(date, locale),
        items: [],
      })
    }
    groups.get(key)?.items.push(item)
  }
  return [...groups.values()]
}

function buildStreamBlocks(items: AuditLogEntry[]): AuditStreamBlock[] {
  const blocks: AuditStreamBlock[] = []
  let index = 0

  while (index < items.length) {
    const signature = buildBurstSignature(items[index])
    let end = index + 1
    while (end < items.length && buildBurstSignature(items[end]) === signature) {
      end += 1
    }

    const slice = items.slice(index, end)
    const burst = slice.length >= 2 ? createActivityBurst(slice) : null
    if (burst) {
      blocks.push({
        kind: 'burst',
        burst,
      })
    }

    slice.forEach((item, burstItemIndex) => {
      blocks.push({
        burstItemCount: burst?.count,
        burstItemIndex: burst ? burstItemIndex : undefined,
        burstKey: burst?.key,
        kind: 'item',
        item,
      })
    })

    index = end
  }

  return blocks
}

function findBurstContextForEntry(
  groups: AuditStreamGroup[],
  entryId: string,
): AuditBurstContext | null {
  for (const group of groups) {
    const bursts = new Map<string, AuditActivityBurst>()
    for (const block of group.blocks) {
      if (block.kind === 'burst') {
        bursts.set(block.burst.key, block.burst)
        continue
      }
      if (block.kind === 'item' && block.item.id === entryId) {
        if (!block.burstKey) {
          return null
        }
        const burst = bursts.get(block.burstKey)
        if (!burst) {
          return null
        }
        return {
          burst,
          burstItemCount: block.burstItemCount,
          burstItemIndex: block.burstItemIndex,
          burstKey: block.burstKey,
        }
      }
    }
  }
  return null
}

function buildBurstSignature(item: AuditLogEntry) {
  const action = item.action.trim().toLowerCase()
  const targetType = item.targetType?.trim().toLowerCase() ?? ''
  const targetId = item.targetId?.trim().toLowerCase() ?? ''

  if (targetType || targetId) return `target:${action}|${targetType}|${targetId}`

  const summary = item.summary?.trim().toLowerCase() ?? ''
  if (summary) return `summary:${action}|${summary}`

  return `action:${action}`
}

function createActivityBurst(items: AuditLogEntry[]): AuditActivityBurst {
  const latest = items[0]
  const oldest = items[items.length - 1]
  const paths = [
    ...new Set(items.flatMap((item) => describeAuditChanges(item)?.paths ?? [])),
  ].slice(0, 4)

  return {
    action: latest.action,
    count: items.length,
    key: items.map((item) => item.id).join('|'),
    latestCreatedAt: latest.createdAt,
    latestId: latest.id,
    oldestCreatedAt: oldest.createdAt,
    oldestId: oldest.id,
    paths,
    targetId: latest.targetId,
    targetType: latest.targetType,
  }
}

function formatDayLabel(value: Date, locale?: Locale) {
  if (isToday(value)) return locale === zhCN ? '今天' : 'Today'
  if (isYesterday(value)) return locale === zhCN ? '昨天' : 'Yesterday'
  return format(value, 'yyyy-MM-dd EEEE', { locale })
}

function matchesLocalAuditQuery(item: AuditLogEntry, query: string) {
  const changeSummary = describeAuditChanges(item)
  const haystack = [
    item.action,
    item.actorEmail,
    item.actorId,
    item.actorIp,
    item.actorType,
    item.actorUserAgent,
    item.summary,
    item.targetId,
    item.targetType,
    item.beforeJson,
    item.afterJson,
    changeSummary?.paths.join(' '),
  ]
    .join(' ')
    .toLowerCase()

  return haystack.includes(query)
}

function describeLocalAuditMatch(item: AuditLogEntry, query: string): LocalAuditMatchReason[] {
  const reasons: LocalAuditMatchReason[] = []
  const changeSummary = describeAuditChanges(item)
  const includes = (values: Array<string | undefined>) =>
    values.join(' ').toLowerCase().includes(query)

  if (includes([item.summary])) reasons.push('summary')
  if (includes([item.action])) reasons.push('action')
  if (includes([item.actorId, item.actorType, item.actorEmail])) reasons.push('actor')
  if (includes([item.targetId, item.targetType])) reasons.push('target')
  if (includes([item.actorIp, item.actorUserAgent])) reasons.push('metadata')
  if (changeSummary?.paths.some((path) => path.toLowerCase().includes(query))) {
    reasons.push('changePath')
  }
  if (includes([item.beforeJson, item.afterJson])) reasons.push('snapshot')

  return reasons
}

function describeAuditChanges(item: AuditLogEntry) {
  const beforeValue = safeParseJson(item.beforeJson)
  const afterValue = safeParseJson(item.afterJson)
  if (beforeValue === undefined && afterValue === undefined) return null
  const paths = collectChangedPaths(beforeValue, afterValue)
  if (paths.length === 0) return null
  return { count: paths.length, paths: paths.slice(0, 12) }
}

function safeParseJson(raw?: string) {
  if (!raw) return undefined
  try {
    return JSON.parse(raw) as unknown
  } catch {
    return raw
  }
}

function collectChangedPaths(before: unknown, after: unknown, basePath = ''): string[] {
  if (isEqualValue(before, after)) return []

  if (isPlainObject(before) && isPlainObject(after)) {
    const keys = [...new Set([...Object.keys(before), ...Object.keys(after)])].sort()
    const paths = keys.flatMap((key) =>
      collectChangedPaths(before[key], after[key], basePath ? `${basePath}.${key}` : key),
    )
    return paths.length > 0 ? paths : [basePath || 'value']
  }

  if (Array.isArray(before) && Array.isArray(after)) {
    const size = Math.max(before.length, after.length)
    const paths: string[] = []
    for (let index = 0; index < size; index += 1) {
      paths.push(...collectChangedPaths(before[index], after[index], `${basePath}[${index}]`))
    }
    return paths.length > 0 ? paths : [basePath || 'items']
  }

  return [basePath || 'value']
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function isEqualValue(left: unknown, right: unknown): boolean {
  if (left === right) return true
  if (Array.isArray(left) && Array.isArray(right)) {
    return (
      left.length === right.length &&
      left.every((value, index) => isEqualValue(value, right[index]))
    )
  }
  if (isPlainObject(left) && isPlainObject(right)) {
    const leftKeys = Object.keys(left)
    const rightKeys = Object.keys(right)
    return (
      leftKeys.length === rightKeys.length &&
      leftKeys.every((key) => isEqualValue(left[key], right[key]))
    )
  }
  return false
}

function formatJson(raw: string) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function formatAbsoluteTimestamp(raw: string, locale?: Locale) {
  return format(new Date(raw), 'yyyy-MM-dd HH:mm:ss', { locale })
}

function formatDateChip(raw: string) {
  return format(new Date(raw), 'MM-dd HH:mm')
}

function localDateTimeToISO(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return undefined
  const parsed = new Date(trimmed)
  if (Number.isNaN(parsed.getTime())) return undefined
  return parsed.toISOString()
}

function toLocalDateTimeInput(value: number) {
  const date = new Date(value)
  const offsetDate = new Date(date.getTime() - date.getTimezoneOffset() * 60_000)
  return offsetDate.toISOString().slice(0, 16)
}

function isoToLocalDateTimeInput(value?: string) {
  if (!value) return ''
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return ''
  return toLocalDateTimeInput(parsed.getTime())
}

function emptyToUndefined(value: null | string) {
  const trimmed = value?.trim()
  return trimmed ? trimmed : undefined
}

function isCommandSurfaceTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false
  const tagName = target.tagName.toLowerCase()
  return tagName === 'a' || tagName === 'button'
}

function isTextEntryTarget(target: EventTarget | null) {
  if (!(target instanceof HTMLElement)) return false
  const tagName = target.tagName.toLowerCase()
  return (
    target.isContentEditable ||
    tagName === 'input' ||
    tagName === 'select' ||
    tagName === 'textarea'
  )
}
