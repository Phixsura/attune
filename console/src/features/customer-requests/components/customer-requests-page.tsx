import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import {
  ArrowRight,
  Bookmark,
  Building2,
  ClipboardList,
  DollarSign,
  ExternalLink,
  GitBranch,
  Github,
  GitMerge,
  Loader2,
  Plus,
  RefreshCw,
  Save,
  Search,
  SlidersHorizontal,
  Trash2,
} from 'lucide-react'
import { type ReactNode, useEffect, useId, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
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
import {
  type CustomerRequestFilters,
  customerRequestAccountSummaryQuery,
  customerRequestDetailQuery,
  customerRequestGitHubIssueConnectionOptionsQuery,
  customerRequestSavedViewsQuery,
  customerRequestScoringSettingsQuery,
  customerRequestsInfiniteQuery,
  useAddCustomerRequestNote,
  useAddCustomerRequestVote,
  useCreateCustomerRequest,
  useCreateCustomerRequestGitHubIssue,
  useCreateCustomerRequestSavedView,
  useDeleteCustomerRequestNote,
  useDeleteCustomerRequestSavedView,
  useLinkCustomerRequestCustomer,
  useLinkCustomerRequestFeedback,
  useLinkCustomerRequestIssue,
  useMergeCustomerRequests,
  usePromoteFeedbackToCustomerRequest,
  useRecordCustomerRequestIssueSync,
  useRemoveCustomerRequestVote,
  useUnlinkCustomerRequestCustomer,
  useUnlinkCustomerRequestFeedback,
  useUnlinkCustomerRequestIssue,
  useUpdateCustomerRequest,
  useUpdateCustomerRequestSavedView,
  useUpdateCustomerRequestScoringSettings,
} from '@/features/customer-requests/api/customer-requests'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { useDocumentTitle } from '@/hooks/use-document-title'
import { api } from '@/lib/api-client'
import { type Member, membersQuery } from '@/lib/members-api'
import { cn } from '@/lib/utils'
import {
  type CustomerRequestAccountDecisionSignal,
  type CustomerRequestAccountEvent,
  CustomerRequestAccountEventKind,
  type CustomerRequestAccountProfile,
  CustomerRequestAccountSignalKind,
  CustomerRequestAccountSignalSeverity,
  type CustomerRequestAccountSummary,
  type CustomerRequestCustomerLink,
  CustomerRequestDecisionPublicSafeState,
  type CustomerRequestDecisionRecord,
  type CustomerRequestDecisionScoreFactor,
  CustomerRequestDecisionScoreFactorKind,
  type CustomerRequestDeliveryArtifact,
  type CustomerRequestDeliveryGraph,
  CustomerRequestDeliveryHealth,
  type CustomerRequestDuplicate,
  CustomerRequestEvidenceConfidence,
  type CustomerRequestEvidenceQuality,
  CustomerRequestEvidenceQualityReason,
  type CustomerRequestFeedbackEvidence,
  CustomerRequestImportance,
  type CustomerRequestIssueLink,
  CustomerRequestIssueSyncState,
  type CustomerRequestNote,
  type CustomerRequestOwner,
  CustomerRequestPriority,
  type CustomerRequestSavedView,
  type CustomerRequestSavedViewState,
  type CustomerRequestScoringSettings,
  CustomerRequestSort,
  CustomerRequestStatus,
  type CustomerRequestSummary,
  CustomerRequestVisibility,
  type CustomerRequestVote,
  SortDirection,
} from '@/proto/attune/v1/customer_request'

const DEFAULT_FILTERS: CustomerRequestFilters = {
  visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE,
  sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT,
  direction: SortDirection.SORT_DIRECTION_DESC,
}

interface OwnerFilterOption {
  id: string
  label: string
}

type TranslateFn = ReturnType<typeof useTranslation>['t']

export function CustomerRequestsPage({
  initialPromoteFeedbackIDs = [],
  initialFeedbackID,
  initialAccountKey,
  initialRequestID,
  initialMergeTargetID,
  onAccountKeyInspect,
  onPromoteClose,
}: {
  initialPromoteFeedbackIDs?: string[]
  initialFeedbackID?: string
  initialAccountKey?: string
  initialRequestID?: string
  initialMergeTargetID?: string
  onAccountKeyInspect?: (accountKey: string) => void
  onPromoteClose?: () => void
} = {}) {
  const { t } = useTranslation()
  useDocumentTitle(t('customer_requests.title'))
  const permissions = usePermissions()
  const canViewMembers = permissions.can('settings:members:view')
  const canConfigure = permissions.can('customer_request:configure')
  const canEdit = permissions.can('customer_request:edit')
  const members = useQuery({ ...membersQuery(), enabled: canViewMembers })
  // Inline cohort query to avoid cross-feature import (same pattern as feedback-page).
  const cohortList = useQuery({
    queryKey: ['cohort-sync', 'cohorts'],
    queryFn: ({ signal }) =>
      api<{ cohorts?: Array<{ id: string; name: string }> }>('/fb/v1/console/cohort-sync/cohorts', {
        signal,
      })
        .then((r) => r.cohorts ?? [])
        .catch(() => []),
    staleTime: 30_000,
  })
  const [filters, setFilters] = useState<CustomerRequestFilters>(() => ({
    ...DEFAULT_FILTERS,
    feedbackId: initialFeedbackID,
    accountKey: initialAccountKey,
  }))
  const [selectedID, setSelectedID] = useState<string | null>(() => initialRequestID ?? null)
  const [createOpen, setCreateOpen] = useState(false)
  const [promoteOpen, setPromoteOpen] = useState(false)
  const [scoringOpen, setScoringOpen] = useState(false)
  const initialPromoteKey = initialPromoteFeedbackIDs.join(',')

  useEffect(() => {
    if (!initialPromoteKey || permissions.isPending) return
    if (canEdit) {
      setPromoteOpen(true)
      return
    }
    onPromoteClose?.()
  }, [canEdit, initialPromoteKey, onPromoteClose, permissions.isPending])
  useEffect(() => {
    setFilters((current) =>
      current.feedbackId === initialFeedbackID
        ? current
        : { ...current, feedbackId: initialFeedbackID },
    )
  }, [initialFeedbackID])
  useEffect(() => {
    setFilters((current) =>
      current.accountKey === initialAccountKey
        ? current
        : { ...current, accountKey: initialAccountKey },
    )
  }, [initialAccountKey])
  useEffect(() => {
    setSelectedID(initialRequestID ?? null)
  }, [initialRequestID])

  const list = useInfiniteQuery(customerRequestsInfiniteQuery(filters))
  const accountSummary = useQuery(customerRequestAccountSummaryQuery(filters))
  const items = useMemo(
    () => list.data?.pages.flatMap((page) => page.requests) ?? [],
    [list.data?.pages],
  )
  const activeAccountKey = filters.accountKey?.trim()
  const ownerOptions = useMemo(
    () => ownerFilterOptions(items, members.data ?? [], filters.ownerMemberId),
    [filters.ownerMemberId, items, members.data],
  )
  const selected = items.find((item) => item.id === selectedID) ?? null
  const inspectAccountRequests = (accountKey: string) => {
    const normalized = accountKey.trim()
    if (!normalized) return
    setFilters({ ...DEFAULT_FILTERS, accountKey: normalized })
    setSelectedID(null)
    onAccountKeyInspect?.(normalized)
  }

  return (
    <div className="space-y-5">
      <section className="flex flex-col gap-4 border-b pb-5 lg:flex-row lg:items-end lg:justify-between">
        <div className="max-w-3xl space-y-2">
          <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
            <ClipboardList className="size-4" />
            {t('nav.customer_requests')}
          </div>
          <h1 className="text-3xl font-semibold tracking-normal">{t('customer_requests.title')}</h1>
          <p className="text-sm text-muted-foreground">{t('customer_requests.subtitle')}</p>
        </div>
        {canConfigure || canEdit ? (
          <div className="flex flex-wrap gap-2">
            {canConfigure ? (
              <Button variant="outline" onClick={() => setScoringOpen(true)}>
                <SlidersHorizontal className="size-4" />
                {t('customer_requests.scoring_settings')}
              </Button>
            ) : null}
            {canEdit ? (
              <>
                <Button variant="outline" onClick={() => setPromoteOpen(true)}>
                  <ArrowRight className="size-4" />
                  {t('customer_requests.promote')}
                </Button>
                <Button onClick={() => setCreateOpen(true)}>
                  <Plus className="size-4" />
                  {t('customer_requests.create')}
                </Button>
              </>
            ) : null}
          </div>
        ) : null}
      </section>

      <CustomerRequestSavedViewsBar filters={filters} onApply={setFilters} />

      <CustomerRequestToolbar
        filters={filters}
        ownerOptions={ownerOptions}
        cohorts={(cohortList.data ?? []).map((c) => ({ id: c.id, name: c.name }))}
        onChange={setFilters}
      />

      {activeAccountKey ? (
        <AccountSignalOverview
          accountKey={activeAccountKey}
          error={accountSummary.error}
          isPending={accountSummary.isPending}
          model={accountSummary.data}
          onOpenRequest={setSelectedID}
        />
      ) : null}

      {list.isPending ? (
        <CustomerRequestSkeleton />
      ) : list.isError ? (
        <Card>
          <CardContent className="flex items-center justify-between gap-4 p-6">
            <div>
              <p className="font-medium">{t('customer_requests.load_failed')}</p>
              <p className="text-sm text-muted-foreground">{list.error?.message}</p>
            </div>
            <Button variant="outline" onClick={() => void list.refetch()}>
              <RefreshCw className="size-4" />
              {t('customer_requests.retry')}
            </Button>
          </CardContent>
        </Card>
      ) : items.length === 0 ? (
        <Card>
          <CardContent className="grid min-h-60 place-items-center p-8 text-center">
            <div className="space-y-2">
              <ClipboardList className="mx-auto size-8 text-muted-foreground" />
              <p className="font-medium">{t('customer_requests.empty')}</p>
              <p className="max-w-md text-sm text-muted-foreground">
                {t('customer_requests.empty_hint')}
              </p>
            </div>
          </CardContent>
        </Card>
      ) : (
        <div className="grid gap-3">
          {items.map((item) => (
            <CustomerRequestRow
              key={item.id}
              item={item}
              ownerLabel={item.owner ? ownerLabel(item.owner) : undefined}
              onOpen={() => setSelectedID(item.id)}
            />
          ))}
          {list.hasNextPage ? (
            <Button
              variant="outline"
              className="justify-self-center"
              disabled={list.isFetchingNextPage}
              onClick={() => void list.fetchNextPage()}
            >
              {list.isFetchingNextPage ? <Loader2 className="size-4 animate-spin" /> : null}
              {t('common.loading')}
            </Button>
          ) : null}
        </div>
      )}

      <CustomerRequestDetailSheet
        id={selectedID}
        fallback={selected}
        ownerOptions={ownerOptions}
        initialMergeTargetID={initialMergeTargetID}
        open={selectedID != null}
        onMerged={setSelectedID}
        onInspectAccount={inspectAccountRequests}
        onOpenChange={(open) => {
          if (!open) setSelectedID(null)
        }}
      />
      <CustomerRequestFormDialog
        open={createOpen}
        mode="create"
        ownerOptions={ownerOptions}
        onOpenChange={setCreateOpen}
      />
      <CustomerRequestFormDialog
        open={promoteOpen}
        mode="promote"
        initialFeedbackIDs={initialPromoteFeedbackIDs}
        ownerOptions={ownerOptions}
        onOpenChange={(open) => {
          setPromoteOpen(open)
          if (!open) onPromoteClose?.()
        }}
      />
      {canConfigure ? (
        <ScoringSettingsDialog open={scoringOpen} onOpenChange={setScoringOpen} />
      ) : null}
    </div>
  )
}

function ownerFilterOptions(
  items: CustomerRequestSummary[],
  members: Member[],
  selectedID?: string,
) {
  const owners = new Map<string, OwnerFilterOption>()
  for (const member of members) {
    if (member.memberType === 'invite') continue
    owners.set(member.id, {
      id: member.id,
      label: memberLabel(member),
    })
  }
  for (const item of items) {
    if (!item.owner?.id) continue
    if (owners.has(item.owner.id)) continue
    owners.set(item.owner.id, {
      id: item.owner.id,
      label: ownerLabel(item.owner),
    })
  }
  if (selectedID && !owners.has(selectedID)) {
    owners.set(selectedID, { id: selectedID, label: selectedID })
  }
  return Array.from(owners.values()).sort((a, b) => a.label.localeCompare(b.label))
}

function ownerLabel(owner: CustomerRequestOwner) {
  return owner.email || owner.userId || owner.id
}

function memberLabel(member: Member) {
  return member.email || member.userId || member.id
}

function CustomerRequestToolbar({
  filters,
  ownerOptions,
  cohorts,
  onChange,
}: {
  filters: CustomerRequestFilters
  ownerOptions: OwnerFilterOption[]
  cohorts: Array<{ id: string; name: string }>
  onChange: (filters: CustomerRequestFilters) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="grid min-w-0 gap-3 rounded-md border bg-background p-3 sm:grid-cols-2 xl:grid-cols-4">
      <div className="relative min-w-0">
        <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          aria-label={t('customer_requests.search_placeholder')}
          className="pl-9"
          value={filters.q ?? ''}
          placeholder={t('customer_requests.search_placeholder')}
          onChange={(event) => onChange({ ...filters, q: event.target.value || undefined })}
        />
      </div>
      <div className="relative min-w-0">
        <Building2 className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          aria-label={t('customer_requests.account_filter')}
          className="pl-9"
          value={filters.accountKey ?? ''}
          placeholder={t('customer_requests.account_filter_placeholder')}
          onChange={(event) =>
            onChange({ ...filters, accountKey: event.target.value || undefined })
          }
        />
      </div>
      <Select
        value={filters.status ?? 'all'}
        onValueChange={(value) =>
          onChange({
            ...filters,
            status: value === 'all' ? undefined : (value as CustomerRequestStatus),
          })
        }
      >
        <SelectTrigger aria-label={t('customer_requests.status')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{t('customer_requests.statuses.all')}</SelectItem>
          <SelectItem value={CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN}>
            {t('customer_requests.statuses.open')}
          </SelectItem>
          <SelectItem value={CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED}>
            {t('customer_requests.statuses.planned')}
          </SelectItem>
          <SelectItem value={CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_IN_PROGRESS}>
            {t('customer_requests.statuses.in_progress')}
          </SelectItem>
          <SelectItem value={CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_SHIPPED}>
            {t('customer_requests.statuses.shipped')}
          </SelectItem>
          <SelectItem value={CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_CANCELLED}>
            {t('customer_requests.statuses.cancelled')}
          </SelectItem>
        </SelectContent>
      </Select>
      <Select
        value={filters.priority ?? 'all'}
        onValueChange={(value) =>
          onChange({
            ...filters,
            priority: value === 'all' ? undefined : (value as CustomerRequestPriority),
          })
        }
      >
        <SelectTrigger aria-label={t('customer_requests.priority')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{t('customer_requests.priorities.all')}</SelectItem>
          <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_NONE}>
            {t('customer_requests.priorities.none')}
          </SelectItem>
          <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_LOW}>
            {t('customer_requests.priorities.low')}
          </SelectItem>
          <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_MEDIUM}>
            {t('customer_requests.priorities.medium')}
          </SelectItem>
          <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH}>
            {t('customer_requests.priorities.high')}
          </SelectItem>
          <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT}>
            {t('customer_requests.priorities.urgent')}
          </SelectItem>
        </SelectContent>
      </Select>
      <Select
        value={filters.ownerMemberId ?? 'all'}
        onValueChange={(value) =>
          onChange({
            ...filters,
            ownerMemberId: value === 'all' ? undefined : value,
          })
        }
      >
        <SelectTrigger aria-label={t('customer_requests.owner_filter')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="all">{t('customer_requests.owners.all')}</SelectItem>
          {ownerOptions.map((owner) => (
            <SelectItem key={owner.id} value={owner.id}>
              {owner.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Select
        value={filters.visibility ?? CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE}
        onValueChange={(value) =>
          onChange({ ...filters, visibility: value as CustomerRequestVisibility })
        }
      >
        <SelectTrigger aria-label={t('customer_requests.visibility')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE}>
            {t('customer_requests.visibilities.active')}
          </SelectItem>
          <SelectItem value={CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_MERGED}>
            {t('customer_requests.visibilities.merged')}
          </SelectItem>
          <SelectItem value={CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ARCHIVED}>
            {t('customer_requests.visibilities.archived')}
          </SelectItem>
          <SelectItem value={CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL}>
            {t('customer_requests.visibilities.all')}
          </SelectItem>
        </SelectContent>
      </Select>
      <Select
        value={filters.sort ?? CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT}
        onValueChange={(value) => onChange({ ...filters, sort: value as CustomerRequestSort })}
      >
        <SelectTrigger aria-label={t('customer_requests.sort')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT}>
            {t('customer_requests.sorts.updated_at')}
          </SelectItem>
          <SelectItem value={CustomerRequestSort.CUSTOMER_REQUEST_SORT_CUSTOMER_COUNT}>
            {t('customer_requests.sorts.customer_count')}
          </SelectItem>
          <SelectItem value={CustomerRequestSort.CUSTOMER_REQUEST_SORT_SUPPORTING_FEEDBACK_COUNT}>
            {t('customer_requests.sorts.supporting_feedback_count')}
          </SelectItem>
          <SelectItem value={CustomerRequestSort.CUSTOMER_REQUEST_SORT_LATEST_FEEDBACK_AT}>
            {t('customer_requests.sorts.latest_feedback_at')}
          </SelectItem>
          <SelectItem value={CustomerRequestSort.CUSTOMER_REQUEST_SORT_PRIORITY}>
            {t('customer_requests.sorts.priority')}
          </SelectItem>
          <SelectItem value={CustomerRequestSort.CUSTOMER_REQUEST_SORT_REVENUE_IMPACT}>
            {t('customer_requests.sorts.revenue_impact')}
          </SelectItem>
          <SelectItem value={CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE}>
            {t('customer_requests.sorts.decision_score')}
          </SelectItem>
          <SelectItem value={CustomerRequestSort.CUSTOMER_REQUEST_SORT_DELIVERY_HEALTH}>
            {t('customer_requests.sorts.delivery_health')}
          </SelectItem>
        </SelectContent>
      </Select>
      {cohorts.length > 0 && (
        <Select
          value={filters.cohortId ?? '__all'}
          onValueChange={(v) => onChange({ ...filters, cohortId: v === '__all' ? undefined : v })}
        >
          <SelectTrigger aria-label={t('cohort_sync.filter.all')}>
            <SelectValue placeholder={t('cohort_sync.filter.all')} />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="__all">{t('cohort_sync.filter.all')}</SelectItem>
            {cohorts.map((c) => (
              <SelectItem key={c.id} value={c.id}>
                {c.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )}
    </div>
  )
}

function AccountSignalOverview({
  accountKey,
  error,
  isPending,
  model,
  onOpenRequest,
}: {
  accountKey: string
  error: Error | null
  isPending: boolean
  model?: CustomerRequestAccountSummary
  onOpenRequest: (id: string) => void
}) {
  const { t } = useTranslation()
  const scope = model?.accountProfile?.accountDisplay || model?.accountKey || accountKey
  const decisionSignals = model?.decisionSignals ?? []
  const events = model?.events ?? []
  const timeline = model?.timeline ?? []
  return (
    <section
      data-testid="customer-request-account-overview"
      className="space-y-4 rounded-md border bg-background p-4"
    >
      <div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div className="min-w-0 space-y-1">
          <div className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
            <Building2 className="size-4" />
            {t('customer_requests.account_overview_title')}
          </div>
          <h2 className="break-words text-xl font-semibold tracking-normal">
            {t('customer_requests.account_overview_scope', { value: scope })}
          </h2>
        </div>
        {model ? (
          <div className="flex items-center gap-1 self-start rounded bg-muted px-2 py-1 text-sm font-medium">
            <DollarSign className="size-4" />
            {t('customer_requests.account_overview_revenue_metric', {
              value: formatMoney(model.revenueImpactCents, model.revenueCurrency),
            })}
          </div>
        ) : null}
      </div>

      {isPending ? <AccountOverviewSkeleton /> : null}

      {!isPending && error ? (
        <div className="rounded-md border border-destructive/30 bg-destructive/5 p-3 text-sm text-destructive">
          {t('customer_requests.load_failed')}: {error.message}
        </div>
      ) : null}

      {!isPending && !error && model ? (
        <>
          {model.accountProfile ? <AccountProfileSummary profile={model.accountProfile} /> : null}

          <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
            <AccountOverviewMetric
              label={t('customer_requests.account_overview_request_metric', {
                count: model.requestCount,
              })}
            />
            <AccountOverviewMetric
              label={t('customer_requests.account_overview_feedback_metric', {
                count: model.feedbackCount,
              })}
            />
            <AccountOverviewMetric
              label={t('customer_requests.account_overview_customer_metric', {
                count: model.customerCount,
              })}
            />
            <AccountOverviewMetric
              label={t('customer_requests.account_overview_vote_metric', {
                count: model.voteCount,
              })}
            />
            <AccountOverviewMetric
              label={t('customer_requests.issue_count', {
                count: model.issueCount,
              })}
            />
            <AccountOverviewMetric
              label={t('customer_requests.account_overview_average_score_metric', {
                count: model.averageDecisionScore,
              })}
            />
            <AccountOverviewMetric
              label={t('customer_requests.account_overview_top_score_metric', {
                count: model.topDecisionScore,
              })}
            />
            <AccountOverviewMetric
              label={t('customer_requests.account_overview_high_priority_metric', {
                count: model.highPriorityRequestCount,
              })}
            />
            <AccountOverviewMetric
              label={t('customer_requests.account_overview_shipped_metric', {
                count: model.shippedRequestCount,
              })}
            />
            <AccountOverviewMetric
              label={t('customer_requests.account_overview_delivery_risk_metric', {
                count: model.staleOrFailedIssueCount,
              })}
            />
          </div>

          <div className="rounded-md bg-muted/40 p-3 text-sm">
            {t('customer_requests.account_overview_delivery_metric', {
              failed: model.failedIssueCount,
              manual: model.manualIssueCount,
              pending: model.pendingIssueCount,
              stale: model.staleIssueCount,
              synced: model.syncedIssueCount,
            })}
          </div>

          <AccountDecisionSignals currency={model.revenueCurrency} signals={decisionSignals} />

          <AccountEventTimeline events={events} onOpenRequest={onOpenRequest} />

          <div className="space-y-2">
            <div className="flex items-center gap-2 text-sm font-medium">
              <ClipboardList className="size-4" />
              {t('customer_requests.account_overview_timeline')}
            </div>
            {timeline.length === 0 ? (
              <p className="text-sm text-muted-foreground">
                {t('customer_requests.account_overview_empty')}
              </p>
            ) : (
              <div className="divide-y rounded-md border">
                {timeline.map((item) => (
                  <button
                    key={item.id}
                    type="button"
                    className="grid w-full gap-2 p-3 text-left text-sm transition hover:bg-muted/40 md:grid-cols-[auto_minmax(0,1fr)_auto]"
                    onClick={() => onOpenRequest(item.id)}
                  >
                    <span className="font-mono text-xs text-muted-foreground">
                      {item.displayId}
                    </span>
                    <div className="min-w-0">
                      <div className="truncate font-medium">{item.title}</div>
                      <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                        <span>{statusLabel(t, item.status)}</span>
                        <span>{priorityLabel(t, item.priority)}</span>
                        <span>
                          {t('customer_requests.decision_score', { count: item.decisionScore })}
                        </span>
                      </div>
                    </div>
                    <span className="text-xs text-muted-foreground">
                      {t('customer_requests.updated', { value: formatDate(item.updatedAt) })}
                    </span>
                  </button>
                ))}
              </div>
            )}
          </div>
        </>
      ) : null}
    </section>
  )
}

function AccountOverviewMetric({ label }: { label: string }) {
  return <div className="rounded-md border bg-muted/20 px-3 py-2 text-sm font-medium">{label}</div>
}

function AccountEventTimeline({
  events,
  onOpenRequest,
}: {
  events: CustomerRequestAccountEvent[]
  onOpenRequest: (id: string) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-sm font-medium">
        <ClipboardList className="size-4" />
        {t('customer_requests.account_overview_events')}
      </div>
      {events.length === 0 ? (
        <p className="text-sm text-muted-foreground">
          {t('customer_requests.account_events_empty')}
        </p>
      ) : (
        <div className="divide-y rounded-md border">
          {events.map((event) => (
            <div
              key={`${event.kind}:${event.requestId}:${event.occurredAt}:${event.feedbackId}:${event.issueKey}`}
              className="grid gap-2 p-3 text-sm md:grid-cols-[minmax(0,1fr)_auto]"
            >
              <button
                type="button"
                className="min-w-0 text-left"
                onClick={() => onOpenRequest(event.requestId)}
              >
                <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                  <span className="rounded bg-muted px-2 py-0.5 text-xs font-medium">
                    {accountEventKindLabel(t, event.kind)}
                  </span>
                  <span className="font-mono text-xs text-muted-foreground">
                    {event.requestDisplayId}
                  </span>
                  <span className="font-medium">{event.requestTitle}</span>
                </div>
                {event.description ? (
                  <div className="mt-1 line-clamp-2 text-muted-foreground">{event.description}</div>
                ) : null}
                <AccountEventMeta event={event} />
              </button>
              <div className="text-xs text-muted-foreground md:text-right">
                <div>{formatDate(event.occurredAt)}</div>
                {event.issueUrl ? (
                  <a
                    className="mt-1 inline-flex items-center gap-1 text-foreground underline-offset-4 hover:underline"
                    href={event.issueUrl}
                    target="_blank"
                    rel="noreferrer"
                  >
                    <ExternalLink className="size-3" />
                    {event.issueKey}
                  </a>
                ) : null}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

function AccountEventMeta({ event }: { event: CustomerRequestAccountEvent }) {
  const { t } = useTranslation()
  const parts = [
    event.subjectDisplay
      ? t('customer_requests.account_event_subject', { value: event.subjectDisplay })
      : '',
    event.source ? t('customer_requests.account_event_source', { value: event.source }) : '',
    event.actorId ? t('customer_requests.account_event_actor', { value: event.actorId }) : '',
    event.feedbackId && event.feedbackId !== '0'
      ? t('customer_requests.account_event_feedback', { value: event.feedbackId })
      : '',
    event.issueKey && !event.issueUrl
      ? t('customer_requests.account_event_issue', { value: event.issueKey })
      : '',
  ].filter(Boolean)
  if (parts.length === 0) return null
  return <div className="mt-1 text-xs text-muted-foreground">{parts.join(' · ')}</div>
}

function accountEventKindLabel(
  t: (key: string, options?: Record<string, unknown>) => string,
  kind: CustomerRequestAccountEventKind,
) {
  switch (kind) {
    case CustomerRequestAccountEventKind.CUSTOMER_REQUEST_ACCOUNT_EVENT_KIND_REQUEST_CREATED:
      return t('customer_requests.account_event_request_created')
    case CustomerRequestAccountEventKind.CUSTOMER_REQUEST_ACCOUNT_EVENT_KIND_FEEDBACK_LINKED:
      return t('customer_requests.account_event_feedback_linked')
    case CustomerRequestAccountEventKind.CUSTOMER_REQUEST_ACCOUNT_EVENT_KIND_CUSTOMER_LINKED:
      return t('customer_requests.account_event_customer_linked')
    case CustomerRequestAccountEventKind.CUSTOMER_REQUEST_ACCOUNT_EVENT_KIND_VOTE_ADDED:
      return t('customer_requests.account_event_vote_added')
    case CustomerRequestAccountEventKind.CUSTOMER_REQUEST_ACCOUNT_EVENT_KIND_ISSUE_LINKED:
      return t('customer_requests.account_event_issue_linked')
    case CustomerRequestAccountEventKind.CUSTOMER_REQUEST_ACCOUNT_EVENT_KIND_ISSUE_SYNCED:
      return t('customer_requests.account_event_issue_synced')
    case CustomerRequestAccountEventKind.CUSTOMER_REQUEST_ACCOUNT_EVENT_KIND_NOTE_ADDED:
      return t('customer_requests.account_event_note_added')
    default:
      return t('customer_requests.account_event_unknown')
  }
}

function AccountDecisionSignals({
  currency,
  signals,
}: {
  currency: string
  signals: CustomerRequestAccountDecisionSignal[]
}) {
  const { t } = useTranslation()
  if (signals.length === 0) return null
  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 text-sm font-medium">
        <SlidersHorizontal className="size-4" />
        {t('customer_requests.account_overview_decision_signals')}
      </div>
      <div className="grid gap-2 md:grid-cols-2">
        {signals.map((signal) => (
          <div
            key={`${signal.kind}:${signal.count}:${signal.score}:${signal.valueCents}`}
            className={cn(
              'rounded-md border px-3 py-2 text-sm',
              accountSignalSeverityClass(signal.severity),
            )}
          >
            <div className="flex items-center justify-between gap-3">
              <span className="font-medium">{accountDecisionSignalLabel(t, signal, currency)}</span>
              <span className="shrink-0 text-xs font-medium">
                {accountSignalSeverityLabel(t, signal.severity)}
              </span>
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

function accountDecisionSignalLabel(
  t: (key: string, options?: Record<string, unknown>) => string,
  signal: CustomerRequestAccountDecisionSignal,
  currency: string,
) {
  switch (signal.kind) {
    case CustomerRequestAccountSignalKind.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_KIND_DELIVERY_RISK:
      return t('customer_requests.account_signal_delivery_risk', { count: signal.count })
    case CustomerRequestAccountSignalKind.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_KIND_HIGH_PRIORITY_DEMAND:
      return t('customer_requests.account_signal_high_priority', {
        count: signal.count,
        score: signal.score,
      })
    case CustomerRequestAccountSignalKind.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_KIND_REVENUE_IMPACT:
      return t('customer_requests.account_signal_revenue', {
        score: signal.score,
        value: formatMoney(signal.valueCents, currency),
      })
    case CustomerRequestAccountSignalKind.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_KIND_EVIDENCE_BREADTH:
      return t('customer_requests.account_signal_evidence_breadth', {
        count: signal.count,
        score: signal.score,
      })
    case CustomerRequestAccountSignalKind.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_KIND_EVIDENCE_GAP:
      return t('customer_requests.account_signal_evidence_gap', { count: signal.count })
    case CustomerRequestAccountSignalKind.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_KIND_SHIPPED_OUTCOME:
      return t('customer_requests.account_signal_shipped', { count: signal.count })
    default:
      return t('customer_requests.account_signal_unknown')
  }
}

function accountSignalSeverityLabel(
  t: (key: string, options?: Record<string, unknown>) => string,
  severity: CustomerRequestAccountSignalSeverity,
) {
  switch (severity) {
    case CustomerRequestAccountSignalSeverity.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_SEVERITY_CRITICAL:
      return t('customer_requests.account_signal_severity_critical')
    case CustomerRequestAccountSignalSeverity.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_SEVERITY_WARNING:
      return t('customer_requests.account_signal_severity_warning')
    case CustomerRequestAccountSignalSeverity.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_SEVERITY_POSITIVE:
      return t('customer_requests.account_signal_severity_positive')
    default:
      return t('customer_requests.account_signal_severity_info')
  }
}

function accountSignalSeverityClass(severity: CustomerRequestAccountSignalSeverity) {
  switch (severity) {
    case CustomerRequestAccountSignalSeverity.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_SEVERITY_CRITICAL:
      return 'border-destructive/40 bg-destructive/5 text-destructive'
    case CustomerRequestAccountSignalSeverity.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_SEVERITY_WARNING:
      return 'border-amber-300 bg-amber-50 text-amber-950'
    case CustomerRequestAccountSignalSeverity.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_SEVERITY_POSITIVE:
      return 'border-emerald-300 bg-emerald-50 text-emerald-950'
    default:
      return 'border-border bg-muted/20 text-foreground'
  }
}

function AccountOverviewSkeleton() {
  return (
    <div className="space-y-3">
      <div className="grid gap-2 sm:grid-cols-2 xl:grid-cols-4">
        <Skeleton className="h-10" />
        <Skeleton className="h-10" />
        <Skeleton className="h-10" />
        <Skeleton className="h-10" />
      </div>
      <Skeleton className="h-14" />
    </div>
  )
}

function AccountProfileSummary({ profile }: { profile: CustomerRequestAccountProfile }) {
  const { t } = useTranslation()
  const meta = [
    profile.tier,
    profile.sizeSegment,
    profile.lifecycleStatus,
    profile.crmProvider,
  ].filter(Boolean)
  return (
    <div className="rounded-md border bg-muted/20 p-3 text-sm">
      <div className="font-medium">{profile.accountDisplay || profile.accountKey}</div>
      {meta.length > 0 ? (
        <div className="mt-1 text-xs text-muted-foreground">
          {t('customer_requests.account_overview_profile_meta', {
            value: meta.join(' · '),
          })}
        </div>
      ) : null}
    </div>
  )
}

const SAVED_VIEW_NONE = '__current__'

function CustomerRequestSavedViewsBar({
  filters,
  onApply,
}: {
  filters: CustomerRequestFilters
  onApply: (filters: CustomerRequestFilters) => void
}) {
  const { t } = useTranslation()
  const inputID = useId()
  const viewsQuery = useQuery(customerRequestSavedViewsQuery())
  const create = useCreateCustomerRequestSavedView()
  const update = useUpdateCustomerRequestSavedView()
  const remove = useDeleteCustomerRequestSavedView()
  const [selectedID, setSelectedID] = useState('')
  const [saveOpen, setSaveOpen] = useState(false)
  const [name, setName] = useState('')
  const views = viewsQuery.data?.views ?? []
  const selected = views.find((view) => view.id === selectedID) ?? null
  const isSaving = create.isPending || update.isPending

  const openSaveDialog = () => {
    setName(selected?.name ?? '')
    setSaveOpen(true)
  }

  const saveCurrent = () => {
    const trimmedName = name.trim()
    /* v8 ignore next -- @preserve: the save button is disabled until the view name is non-empty. */
    if (!trimmedName) return
    const state = filtersToSavedViewState(filters)
    const handlers = {
      onSuccess: (response: { view?: CustomerRequestSavedView }) => {
        if (response.view?.id) setSelectedID(response.view.id)
        setSaveOpen(false)
      },
      onError: (err: unknown) =>
        toast.error(err instanceof Error ? err.message : t('common.error')),
    }
    if (selected) {
      update.mutate({ id: selected.id, name: trimmedName, state }, handlers)
      return
    }
    create.mutate({ name: trimmedName, state }, handlers)
  }

  return (
    <div className="flex flex-col gap-2 rounded-md border bg-background p-3 md:flex-row md:items-center md:justify-between">
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <Bookmark className="size-4 shrink-0 text-muted-foreground" />
        <Select
          value={selectedID || SAVED_VIEW_NONE}
          disabled={viewsQuery.isPending}
          onValueChange={(value) => {
            if (value === SAVED_VIEW_NONE) {
              setSelectedID('')
              return
            }
            const view = views.find((item) => item.id === value)
            /* v8 ignore next -- @preserve: select options are rendered from the current saved-view list. */
            if (!view) return
            setSelectedID(view.id)
            onApply(savedViewStateToFilters(view.state))
          }}
        >
          <SelectTrigger
            className="min-w-0 md:w-72"
            aria-label={t('customer_requests.saved_views_label')}
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={SAVED_VIEW_NONE}>
              {t('customer_requests.saved_views_current')}
            </SelectItem>
            {views.length === 0 ? (
              <SelectItem value="__empty__" disabled>
                {t('customer_requests.saved_views_empty')}
              </SelectItem>
            ) : null}
            {views.map((view) => (
              <SelectItem key={view.id} value={view.id}>
                {view.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={openSaveDialog}>
          <Save className="size-4" />
          {t('customer_requests.saved_views_save')}
        </Button>
        <Button
          variant="ghost"
          size="icon"
          disabled={!selected || remove.isPending}
          aria-label={t('customer_requests.saved_views_delete')}
          onClick={() => {
            /* v8 ignore next -- @preserve: the delete button is disabled until a saved view is selected. */
            if (!selected) return
            remove.mutate(selected.id, {
              onSuccess: () => setSelectedID(''),
              onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
            })
          }}
        >
          {remove.isPending ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Trash2 className="size-4" />
          )}
        </Button>
      </div>
      <Dialog open={saveOpen} onOpenChange={setSaveOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('customer_requests.saved_views_save_title')}</DialogTitle>
            <DialogDescription className="sr-only">
              {t('customer_requests.saved_views_save')}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor={inputID}>{t('customer_requests.saved_views_name')}</Label>
            <Input
              id={inputID}
              value={name}
              maxLength={80}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setSaveOpen(false)}>
              {t('common.cancel')}
            </Button>
            <Button disabled={isSaving || name.trim().length === 0} onClick={saveCurrent}>
              {isSaving ? <Loader2 className="size-4 animate-spin" /> : null}
              {t('common.save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function filtersToSavedViewState(filters: CustomerRequestFilters): CustomerRequestSavedViewState {
  return {
    q: filters.q?.trim() ?? '',
    status: filters.status ? [filters.status] : [],
    priority: filters.priority ? [filters.priority] : [],
    ownerMemberId: filters.ownerMemberId,
    visibility: filters.visibility ?? CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE,
    sort: filters.sort ?? CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT,
    direction: filters.direction ?? SortDirection.SORT_DIRECTION_DESC,
    feedbackId: filters.feedbackId,
    accountKey: filters.accountKey?.trim() || undefined,
  }
}

function savedViewStateToFilters(state?: CustomerRequestSavedViewState): CustomerRequestFilters {
  if (!state) return { ...DEFAULT_FILTERS }
  return {
    q: state.q || undefined,
    status: (state.status ?? [])[0],
    priority: (state.priority ?? [])[0],
    ownerMemberId: state.ownerMemberId,
    visibility: state.visibility || CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE,
    sort: state.sort || CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT,
    direction: state.direction || SortDirection.SORT_DIRECTION_DESC,
    feedbackId: state.feedbackId,
    accountKey: state.accountKey,
  }
}

type ScoringFormState = {
  priorityNoneWeight: number
  priorityLowWeight: number
  priorityMediumWeight: number
  priorityHighWeight: number
  priorityUrgentWeight: number
  feedbackWeight: number
  feedbackCap: number
  customerWeight: number
  customerCap: number
  accountWeight: number
  accountCap: number
  voteWeight: number
  voteCap: number
  revenueCentsPerPoint: string
  revenueCap: number
}

const DEFAULT_SCORING_FORM: ScoringFormState = {
  priorityNoneWeight: 0,
  priorityLowWeight: 20,
  priorityMediumWeight: 40,
  priorityHighWeight: 60,
  priorityUrgentWeight: 80,
  feedbackWeight: 2,
  feedbackCap: 80,
  customerWeight: 5,
  customerCap: 100,
  accountWeight: 8,
  accountCap: 120,
  voteWeight: 4,
  voteCap: 80,
  revenueCentsPerPoint: '100000',
  revenueCap: 100,
}

const PRIORITY_SCORING_FIELDS = [
  'priorityNoneWeight',
  'priorityLowWeight',
  'priorityMediumWeight',
  'priorityHighWeight',
  'priorityUrgentWeight',
] as const

const SIGNAL_SCORING_FIELDS = [
  'feedbackWeight',
  'feedbackCap',
  'customerWeight',
  'customerCap',
  'accountWeight',
  'accountCap',
  'voteWeight',
  'voteCap',
  'revenueCap',
] as const

function ScoringSettingsDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const settings = useQuery({ ...customerRequestScoringSettingsQuery(), enabled: open })
  const update = useUpdateCustomerRequestScoringSettings()
  const [form, setForm] = useState<ScoringFormState>(DEFAULT_SCORING_FORM)

  useEffect(() => {
    if (settings.data) setForm(scoringSettingsToForm(settings.data))
  }, [settings.data])

  const setNumber = (field: keyof ScoringFormState, value: string) => {
    setForm((current) => ({
      ...current,
      [field]: field === 'revenueCentsPerPoint' ? value : normalizeInteger(value),
    }))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t('customer_requests.scoring_settings')}</DialogTitle>
          <DialogDescription className="sr-only">
            {t('customer_requests.scoring_settings_description')}
          </DialogDescription>
        </DialogHeader>
        {settings.isPending ? (
          <div className="grid gap-3 sm:grid-cols-2">
            <Skeleton className="h-16" />
            <Skeleton className="h-16" />
            <Skeleton className="h-16" />
            <Skeleton className="h-16" />
          </div>
        ) : settings.isError ? (
          <div className="rounded-md border p-4 text-sm text-destructive">
            {settings.error?.message}
          </div>
        ) : (
          <div className="space-y-5">
            <ScoringFieldGrid title={t('customer_requests.scoring_priority_weights')}>
              {PRIORITY_SCORING_FIELDS.map((field) => (
                <ScoringNumberInput
                  key={field}
                  label={t(`customer_requests.scoring_fields.${field}`)}
                  value={form[field]}
                  onChange={(value) => setNumber(field, value)}
                />
              ))}
            </ScoringFieldGrid>
            <ScoringFieldGrid title={t('customer_requests.scoring_signal_weights')}>
              {SIGNAL_SCORING_FIELDS.map((field) => (
                <ScoringNumberInput
                  key={field}
                  label={t(`customer_requests.scoring_fields.${field}`)}
                  value={form[field]}
                  onChange={(value) => setNumber(field, value)}
                />
              ))}
              <ScoringNumberInput
                label={t('customer_requests.scoring_fields.revenueCentsPerPoint')}
                value={form.revenueCentsPerPoint}
                onChange={(value) => setNumber('revenueCentsPerPoint', value)}
              />
            </ScoringFieldGrid>
            {settings.data?.updatedAt ? (
              <p className="text-xs text-muted-foreground">
                {t('customer_requests.scoring_updated', {
                  actor: settings.data.updatedBy || t('customer_requests.owner_unassigned'),
                  value: formatDate(settings.data.updatedAt),
                })}
              </p>
            ) : null}
          </div>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button
            disabled={settings.isPending || settings.isError || update.isPending}
            onClick={() =>
              update.mutate(scoringFormToRequest(form), {
                onSuccess: () => {
                  toast.success(t('customer_requests.scoring_saved'))
                  onOpenChange(false)
                },
                onError: (err) =>
                  toast.error(err instanceof Error ? err.message : t('common.error')),
              })
            }
          >
            {update.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Save className="size-4" />
            )}
            {t('common.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ScoringFieldGrid({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-3">
      <h2 className="text-sm font-medium">{title}</h2>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{children}</div>
    </section>
  )
}

function ScoringNumberInput({
  label,
  value,
  onChange,
}: {
  label: string
  value: number | string
  onChange: (value: string) => void
}) {
  const id = useId()
  return (
    <div className="space-y-1 text-sm">
      <Label htmlFor={id} className="text-muted-foreground">
        {label}
      </Label>
      <Input
        id={id}
        min={0}
        inputMode="numeric"
        type="number"
        value={value}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  )
}

function scoringSettingsToForm(settings: CustomerRequestScoringSettings): ScoringFormState {
  return {
    priorityNoneWeight: settings.priorityNoneWeight,
    priorityLowWeight: settings.priorityLowWeight,
    priorityMediumWeight: settings.priorityMediumWeight,
    priorityHighWeight: settings.priorityHighWeight,
    priorityUrgentWeight: settings.priorityUrgentWeight,
    feedbackWeight: settings.feedbackWeight,
    feedbackCap: settings.feedbackCap,
    customerWeight: settings.customerWeight,
    customerCap: settings.customerCap,
    accountWeight: settings.accountWeight,
    accountCap: settings.accountCap,
    voteWeight: settings.voteWeight,
    voteCap: settings.voteCap,
    revenueCentsPerPoint: settings.revenueCentsPerPoint || '100000',
    revenueCap: settings.revenueCap,
  }
}

function scoringFormToRequest(form: ScoringFormState) {
  return {
    priorityNoneWeight: form.priorityNoneWeight,
    priorityLowWeight: form.priorityLowWeight,
    priorityMediumWeight: form.priorityMediumWeight,
    priorityHighWeight: form.priorityHighWeight,
    priorityUrgentWeight: form.priorityUrgentWeight,
    feedbackWeight: form.feedbackWeight,
    feedbackCap: form.feedbackCap,
    customerWeight: form.customerWeight,
    customerCap: form.customerCap,
    accountWeight: form.accountWeight,
    accountCap: form.accountCap,
    voteWeight: form.voteWeight,
    voteCap: form.voteCap,
    revenueCentsPerPoint: normalizeIntegerString(form.revenueCentsPerPoint, '100000'),
    revenueCap: form.revenueCap,
  }
}

function normalizeInteger(value: string) {
  const parsed = Number.parseInt(value, 10)
  if (!Number.isFinite(parsed) || parsed < 0) return 0
  return parsed
}

function normalizeIntegerString(value: string, fallback: string) {
  const trimmed = value.trim()
  if (!/^\d+$/.test(trimmed)) return fallback
  return trimmed === '0' ? fallback : trimmed
}

function CustomerRequestRow({
  item,
  ownerLabel,
  onOpen,
}: {
  item: CustomerRequestSummary
  ownerLabel?: string
  onOpen: () => void
}) {
  const { t } = useTranslation()
  return (
    <button
      type="button"
      className="grid gap-3 rounded-md border bg-background p-4 text-left transition hover:border-primary/40 hover:bg-muted/30 lg:grid-cols-[minmax(0,1fr)_auto]"
      onClick={onOpen}
    >
      <div className="min-w-0 space-y-2">
        <div className="flex flex-wrap items-center gap-2">
          <span className="rounded border px-2 py-0.5 font-mono text-xs text-muted-foreground">
            {item.displayId}
          </span>
          <StatusPill status={item.status} />
          <PriorityPill priority={item.priority} />
          <DeliveryHealthPill health={item.deliveryHealth} />
        </div>
        <h2 className="truncate text-base font-semibold">{item.title}</h2>
        <div className="flex flex-wrap gap-x-4 gap-y-1 text-sm text-muted-foreground">
          <span>
            {t('customer_requests.feedback_count', { count: item.supportingFeedbackCount })}
          </span>
          <span>{t('customer_requests.customer_count', { count: item.customerCount })}</span>
          <span>{t('customer_requests.vote_count', { count: item.voteCount })}</span>
          <span>{t('customer_requests.issue_count', { count: item.linkedIssueCount })}</span>
          <span>
            {t('customer_requests.revenue_impact', {
              value: formatMoney(item.revenueImpactCents, item.revenueCurrency),
            })}
          </span>
          <span>{t('customer_requests.decision_score', { count: item.decisionScore })}</span>
          <EvidenceQualityBadge quality={item.evidenceQuality} />
          <span>
            {t('customer_requests.issue_sync_counts', {
              synced: item.syncedIssueCount,
              stale: item.staleIssueCount,
              failed: item.failedIssueCount,
              pending: item.pendingIssueCount,
            })}
          </span>
          {item.duplicateRequestCount > 0 ? (
            <span>
              {t('customer_requests.duplicate_count', { count: item.duplicateRequestCount })}
            </span>
          ) : null}
          {item.hiddenFeedbackCount > 0 ? (
            <span>
              {t('customer_requests.hidden_feedback_count', { count: item.hiddenFeedbackCount })}
            </span>
          ) : null}
          <span>
            {t('customer_requests.owner_value', {
              value: ownerLabel ?? t('customer_requests.owner_unassigned'),
            })}
          </span>
        </div>
      </div>
      <div className="flex items-center justify-between gap-3 text-sm text-muted-foreground lg:min-w-56 lg:justify-end">
        <span>{t('customer_requests.updated', { value: formatDate(item.updatedAt) })}</span>
        <ArrowRight className="size-4" />
      </div>
    </button>
  )
}

function CustomerRequestDetailSheet({
  id,
  fallback,
  ownerOptions,
  initialMergeTargetID,
  open,
  onMerged,
  onInspectAccount,
  onOpenChange,
}: {
  id: string | null
  fallback: CustomerRequestSummary | null
  ownerOptions: OwnerFilterOption[]
  initialMergeTargetID?: string
  open: boolean
  onMerged: (targetID: string) => void
  onInspectAccount: (accountKey: string) => void
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const permissions = usePermissions()
  const detail = useQuery(customerRequestDetailQuery(id))
  const current = detail.data?.request ?? fallback
  const canEdit = permissions.can('customer_request:edit')
  const canMerge = permissions.can('customer_request:merge')
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-3xl">
        <SheetHeader>
          <SheetTitle className="flex flex-wrap items-center gap-2">
            {current?.displayId ? (
              <span className="rounded border px-2 py-0.5 font-mono text-xs text-muted-foreground">
                {current.displayId}
              </span>
            ) : null}
            {current?.title ?? t('app.loading')}
          </SheetTitle>
          <SheetDescription className="sr-only">{t('customer_requests.subtitle')}</SheetDescription>
        </SheetHeader>
        {detail.isPending ? (
          <div className="mt-6 space-y-3">
            <Skeleton className="h-24" />
            <Skeleton className="h-48" />
          </div>
        ) : detail.isError ? (
          <div className="mt-6 rounded-md border p-4 text-sm text-destructive">
            {detail.error?.message}
          </div>
        ) : detail.data ? (
          <div className="mt-6 space-y-5">
            <div className="grid gap-3 rounded-md border p-4 sm:grid-cols-2 lg:grid-cols-3">
              <Metric
                label={t('customer_requests.feedback_count', {
                  count: detail.data.request?.supportingFeedbackCount ?? 0,
                })}
              />
              <Metric
                label={t('customer_requests.customer_count', {
                  count: detail.data.request?.customerCount ?? 0,
                })}
              />
              <Metric
                label={t('customer_requests.account_count', {
                  count: detail.data.request?.accountCount ?? 0,
                })}
              />
              <Metric
                label={t('customer_requests.vote_count', {
                  count: detail.data.request?.voteCount ?? 0,
                })}
              />
              <Metric
                label={t('customer_requests.issue_count', {
                  count: detail.data.request?.linkedIssueCount ?? 0,
                })}
              />
              <Metric
                label={t('customer_requests.duplicate_count', {
                  count: detail.data.request?.duplicateRequestCount ?? 0,
                })}
              />
              <Metric
                label={t('customer_requests.revenue_impact', {
                  value: formatMoney(
                    detail.data.request?.revenueImpactCents,
                    detail.data.request?.revenueCurrency,
                  ),
                })}
              />
              <Metric
                label={t('customer_requests.decision_score', {
                  count: detail.data.request?.decisionScore ?? 0,
                })}
              />
              <Metric
                label={t('customer_requests.evidence_quality_score', {
                  score: detail.data.request?.evidenceQuality?.score ?? 0,
                  confidence: evidenceConfidenceLabel(
                    t,
                    detail.data.request?.evidenceQuality?.confidence,
                  ),
                })}
              />
              <Metric
                label={t('customer_requests.delivery_health', {
                  value: deliveryHealthLabel(detail.data.request?.deliveryHealth, t),
                })}
              />
              <Metric
                label={t('customer_requests.issue_sync_counts', {
                  synced: detail.data.request?.syncedIssueCount ?? 0,
                  stale: detail.data.request?.staleIssueCount ?? 0,
                  failed: detail.data.request?.failedIssueCount ?? 0,
                  pending: detail.data.request?.pendingIssueCount ?? 0,
                })}
              />
            </div>
            <DecisionScoreBreakdown request={detail.data.request} />
            <EvidenceQualityPanel quality={detail.data.request?.evidenceQuality} />
            {detail.data.description ? (
              <p className="whitespace-pre-wrap text-sm leading-6 text-muted-foreground">
                {detail.data.description}
              </p>
            ) : null}
            {detail.data.request && canEdit ? (
              <RequestEditControls request={detail.data.request} ownerOptions={ownerOptions} />
            ) : null}
            {detail.data.request && canMerge ? (
              <MergeForm
                sourceID={detail.data.request.id}
                initialTargetID={initialMergeTargetID}
                onMerged={onMerged}
              />
            ) : null}
            {canEdit ? <FeedbackLinkForm requestID={detail.data.request?.id ?? ''} /> : null}
            {canEdit ? <CustomerLinkForm requestID={detail.data.request?.id ?? ''} /> : null}
            {canEdit ? <VoteForm requestID={detail.data.request?.id ?? ''} /> : null}
            {canEdit ? (
              <IssueLinkForm
                requestID={detail.data.request?.id ?? ''}
                hasGitHubIssueLink={detail.data.issueLinks.some(isGitHubIssueLink)}
              />
            ) : null}
            <DetailSection title={t('customer_requests.notes')}>
              {canEdit ? <NoteForm requestID={detail.data.request?.id ?? ''} /> : null}
              {(detail.data.notes ?? []).length === 0 ? (
                <EmptyLine text={t('customer_requests.no_notes')} />
              ) : (
                <div className="space-y-2">
                  {(detail.data.notes ?? []).map((item) => (
                    <NoteRow
                      key={item.id}
                      requestID={detail.data.request?.id ?? ''}
                      item={item}
                      canEdit={canEdit}
                    />
                  ))}
                </div>
              )}
            </DetailSection>
            <DetailSection title={t('customer_requests.linked_feedback')}>
              {detail.data.feedback.length === 0 ? (
                <EmptyLine text={t('customer_requests.no_feedback')} />
              ) : (
                <div className="space-y-2">
                  {detail.data.feedback.map((item) => (
                    <FeedbackEvidenceRow
                      key={item.feedbackId}
                      requestID={detail.data.request?.id ?? ''}
                      item={item}
                      canEdit={canEdit}
                    />
                  ))}
                </div>
              )}
            </DetailSection>
            <DetailSection title={t('customer_requests.customers')}>
              {detail.data.customers.length === 0 ? (
                <EmptyLine text={t('customer_requests.no_customers')} />
              ) : (
                <div className="space-y-2">
                  {detail.data.customers.map((item) => (
                    <CustomerLinkRow
                      key={item.id}
                      requestID={detail.data.request?.id ?? ''}
                      item={item}
                      canEdit={canEdit}
                    />
                  ))}
                </div>
              )}
            </DetailSection>
            <DetailSection title={t('customer_requests.votes')}>
              {detail.data.votes.length === 0 ? (
                <EmptyLine text={t('customer_requests.no_votes')} />
              ) : (
                <div className="space-y-2">
                  {detail.data.votes.map((item) => (
                    <VoteRow
                      key={item.id}
                      requestID={detail.data.request?.id ?? ''}
                      item={item}
                      canEdit={canEdit}
                    />
                  ))}
                </div>
              )}
            </DetailSection>
            <DetailSection title={t('customer_requests.accounts')}>
              {detail.data.accountProfiles.length === 0 ? (
                <EmptyLine text={t('customer_requests.no_accounts')} />
              ) : (
                <div className="space-y-2">
                  {detail.data.accountProfiles.map((item) => (
                    <AccountProfileRow
                      key={item.accountKey}
                      item={item}
                      onInspect={onInspectAccount}
                    />
                  ))}
                </div>
              )}
            </DetailSection>
            <DetailSection title={t('customer_requests.delivery_graph')}>
              <DeliveryGraphPanel graph={detail.data.deliveryGraph} />
            </DetailSection>
            <DetailSection title={t('customer_requests.delivery_links')}>
              {detail.data.issueLinks.length === 0 ? (
                <EmptyLine text={t('customer_requests.no_issues')} />
              ) : (
                <div className="space-y-2">
                  {detail.data.issueLinks.map((item) => (
                    <IssueLinkRow
                      key={item.id}
                      requestID={detail.data.request?.id ?? ''}
                      item={item}
                      canEdit={canEdit}
                    />
                  ))}
                </div>
              )}
            </DetailSection>
            <DetailSection title={t('customer_requests.duplicates')}>
              {detail.data.duplicates.length === 0 ? (
                <EmptyLine text={t('customer_requests.no_duplicates')} />
              ) : (
                <div className="space-y-2">
                  {detail.data.duplicates.map((item) => (
                    <DuplicateRow key={item.id} item={item} />
                  ))}
                </div>
              )}
            </DetailSection>
            <DecisionRecordsSection
              records={detail.data.decisionRecords ?? []}
              currency={detail.data.request?.revenueCurrency ?? 'USD'}
            />
            <DetailSection title={t('customer_requests.audit')}>
              {detail.data.auditEntries.length === 0 ? (
                <EmptyLine text={t('customer_requests.no_audit')} />
              ) : (
                <div className="space-y-2">
                  {detail.data.auditEntries.map((item) => (
                    <div key={item.id} className="rounded-md border p-3 text-sm">
                      <div className="font-medium">{item.summary || item.action}</div>
                      <div className="text-xs text-muted-foreground">
                        {formatDate(item.createdAt)}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </DetailSection>
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  )
}

function RequestEditControls({
  request,
  ownerOptions,
}: {
  request: CustomerRequestSummary
  ownerOptions: OwnerFilterOption[]
}) {
  const { t } = useTranslation()
  const update = useUpdateCustomerRequest(request.id)
  const [status, setStatus] = useState(request.status)
  const [priority, setPriority] = useState(request.priority)
  const [ownerMemberID, setOwnerMemberID] = useState(request.owner?.id ?? 'unassigned')

  useEffect(() => {
    setStatus(request.status)
    setPriority(request.priority)
    setOwnerMemberID(request.owner?.id ?? 'unassigned')
  }, [request.owner?.id, request.priority, request.status])

  const currentOwnerMemberID = request.owner?.id ?? 'unassigned'
  const changed =
    status !== request.status ||
    priority !== request.priority ||
    ownerMemberID !== currentOwnerMemberID
  return (
    <div className="grid gap-2 rounded-md border p-3 sm:grid-cols-[1fr_1fr_1fr_auto]">
      <Select value={status} onValueChange={(value) => setStatus(value as CustomerRequestStatus)}>
        <SelectTrigger aria-label={t('customer_requests.status')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN}>
            {t('customer_requests.statuses.open')}
          </SelectItem>
          <SelectItem value={CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED}>
            {t('customer_requests.statuses.planned')}
          </SelectItem>
          <SelectItem value={CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_IN_PROGRESS}>
            {t('customer_requests.statuses.in_progress')}
          </SelectItem>
          <SelectItem value={CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_SHIPPED}>
            {t('customer_requests.statuses.shipped')}
          </SelectItem>
          <SelectItem value={CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_CANCELLED}>
            {t('customer_requests.statuses.cancelled')}
          </SelectItem>
        </SelectContent>
      </Select>
      <Select
        value={priority}
        onValueChange={(value) => setPriority(value as CustomerRequestPriority)}
      >
        <SelectTrigger aria-label={t('customer_requests.priority')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_NONE}>
            {t('customer_requests.priorities.none')}
          </SelectItem>
          <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_LOW}>
            {t('customer_requests.priorities.low')}
          </SelectItem>
          <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_MEDIUM}>
            {t('customer_requests.priorities.medium')}
          </SelectItem>
          <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH}>
            {t('customer_requests.priorities.high')}
          </SelectItem>
          <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT}>
            {t('customer_requests.priorities.urgent')}
          </SelectItem>
        </SelectContent>
      </Select>
      <Select value={ownerMemberID} onValueChange={setOwnerMemberID}>
        <SelectTrigger aria-label={t('customer_requests.owner_filter')}>
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="unassigned">{t('customer_requests.owner_unassigned')}</SelectItem>
          {ownerOptions.map((owner) => (
            <SelectItem key={owner.id} value={owner.id}>
              {owner.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button
        disabled={!changed || update.isPending}
        onClick={() =>
          update.mutate(
            {
              status,
              priority,
              ownerMemberId: ownerMemberID === 'unassigned' ? '' : ownerMemberID,
            },
            {
              onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
            },
          )
        }
      >
        {update.isPending ? (
          <Loader2 className="size-4 animate-spin" />
        ) : (
          <Save className="size-4" />
        )}
        {t('customer_requests.save_changes')}
      </Button>
    </div>
  )
}

function MergeForm({
  sourceID,
  initialTargetID,
  onMerged,
}: {
  sourceID: string
  initialTargetID?: string
  onMerged: (targetID: string) => void
}) {
  const { t } = useTranslation()
  const merge = useMergeCustomerRequests(sourceID)
  const [targetID, setTargetID] = useState(() => initialTargetID ?? '')
  useEffect(() => {
    setTargetID(initialTargetID ?? '')
  }, [initialTargetID])
  const normalizedTargetID = targetID.trim()
  return (
    <div className="grid gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_auto]">
      <Input
        value={targetID}
        placeholder={t('customer_requests.merge_target_placeholder')}
        onChange={(event) => setTargetID(event.target.value)}
      />
      <Button
        variant="outline"
        disabled={
          merge.isPending || normalizedTargetID.length === 0 || normalizedTargetID === sourceID
        }
        onClick={() =>
          merge.mutate(
            { targetId: normalizedTargetID, idempotencyKey: makeIdempotencyKey() },
            {
              onSuccess: (detail) => {
                setTargetID('')
                if (detail.request?.id) onMerged(detail.request.id)
              },
              onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
            },
          )
        }
      >
        {merge.isPending ? (
          <Loader2 className="size-4 animate-spin" />
        ) : (
          <GitMerge className="size-4" />
        )}
        {t('customer_requests.merge')}
      </Button>
    </div>
  )
}

function FeedbackLinkForm({ requestID }: { requestID: string }) {
  const { t } = useTranslation()
  const [feedbackID, setFeedbackID] = useState('')
  const link = useLinkCustomerRequestFeedback(requestID)
  const normalizedFeedbackID = parseFeedbackIDs(feedbackID)[0] ?? ''
  /* v8 ignore next -- @preserve: parent detail views only mount this form with a selected request. */
  if (!requestID) return null
  return (
    <div className="grid gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_auto]">
      <Input
        value={feedbackID}
        placeholder={t('customer_requests.feedback_id_placeholder')}
        onChange={(event) => setFeedbackID(event.target.value)}
      />
      <Button
        disabled={link.isPending || normalizedFeedbackID.length === 0}
        onClick={() =>
          link.mutate(
            {
              feedbackId: normalizedFeedbackID,
              importance: CustomerRequestImportance.CUSTOMER_REQUEST_IMPORTANCE_NORMAL,
            },
            {
              onSuccess: () => setFeedbackID(''),
              onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
            },
          )
        }
      >
        {link.isPending ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
        {t('customer_requests.link_feedback')}
      </Button>
    </div>
  )
}

function CustomerLinkForm({ requestID }: { requestID: string }) {
  const { t } = useTranslation()
  const [subjectKey, setSubjectKey] = useState('')
  const [subjectDisplay, setSubjectDisplay] = useState('')
  const [accountKey, setAccountKey] = useState('')
  const [accountDisplay, setAccountDisplay] = useState('')
  const [revenueCents, setRevenueCents] = useState('')
  const [currency, setCurrency] = useState('USD')
  const [tier, setTier] = useState('')
  const [sizeSegment, setSizeSegment] = useState('')
  const [lifecycleStatus, setLifecycleStatus] = useState('')
  const link = useLinkCustomerRequestCustomer(requestID)
  const normalizedSubjectKey = subjectKey.trim()
  const normalizedAccountKey = accountKey.trim()
  const hasAccountProfile = normalizedAccountKey.length > 0
  const parsedRevenue = parseMoneyCents(revenueCents)
  /* v8 ignore next -- @preserve: parent detail views only mount this form with a selected request. */
  if (!requestID) return null
  return (
    <div className="rounded-md border p-3">
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
        <Input
          value={subjectKey}
          placeholder={t('customer_requests.subject_key_placeholder')}
          onChange={(event) => setSubjectKey(event.target.value)}
        />
        <Input
          value={subjectDisplay}
          placeholder={t('customer_requests.subject_display_placeholder')}
          onChange={(event) => setSubjectDisplay(event.target.value)}
        />
        <Input
          value={accountKey}
          placeholder={t('customer_requests.account_key_placeholder')}
          onChange={(event) => setAccountKey(event.target.value)}
        />
        <Input
          value={accountDisplay}
          placeholder={t('customer_requests.account_display_placeholder')}
          onChange={(event) => setAccountDisplay(event.target.value)}
        />
        <Input
          value={revenueCents}
          inputMode="numeric"
          placeholder={t('customer_requests.revenue_cents_placeholder')}
          onChange={(event) => setRevenueCents(event.target.value)}
        />
        <Input
          value={currency}
          maxLength={3}
          placeholder={t('customer_requests.currency_placeholder')}
          onChange={(event) => setCurrency(event.target.value.toUpperCase())}
        />
        <Input
          value={tier}
          placeholder={t('customer_requests.tier_placeholder')}
          onChange={(event) => setTier(event.target.value)}
        />
        <Input
          value={sizeSegment}
          placeholder={t('customer_requests.size_segment_placeholder')}
          onChange={(event) => setSizeSegment(event.target.value)}
        />
        <Input
          value={lifecycleStatus}
          placeholder={t('customer_requests.lifecycle_placeholder')}
          onChange={(event) => setLifecycleStatus(event.target.value)}
        />
      </div>
      <Button
        className="mt-2"
        disabled={
          link.isPending ||
          (normalizedSubjectKey.length === 0 && normalizedAccountKey.length === 0) ||
          parsedRevenue === null
        }
        onClick={() =>
          link.mutate(
            {
              subjectKey: normalizedSubjectKey,
              subjectDisplay: subjectDisplay.trim(),
              accountKey: normalizedAccountKey,
              accountDisplay: accountDisplay.trim(),
              accountRevenueCents:
                hasAccountProfile && parsedRevenue !== undefined
                  ? String(parsedRevenue)
                  : undefined,
              accountRevenueCurrency: hasAccountProfile ? currency.trim() || undefined : undefined,
              accountTier: hasAccountProfile ? tier.trim() || undefined : undefined,
              accountSizeSegment: hasAccountProfile ? sizeSegment.trim() || undefined : undefined,
              accountLifecycleStatus: hasAccountProfile
                ? lifecycleStatus.trim() || undefined
                : undefined,
            },
            {
              onSuccess: () => {
                setSubjectKey('')
                setSubjectDisplay('')
                setAccountKey('')
                setAccountDisplay('')
                setRevenueCents('')
                setCurrency('USD')
                setTier('')
                setSizeSegment('')
                setLifecycleStatus('')
              },
              onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
            },
          )
        }
      >
        {link.isPending ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
        {t('customer_requests.link_customer')}
      </Button>
    </div>
  )
}

function VoteForm({ requestID }: { requestID: string }) {
  const { t } = useTranslation()
  const [subjectKey, setSubjectKey] = useState('')
  const [subjectDisplay, setSubjectDisplay] = useState('')
  const [accountKey, setAccountKey] = useState('')
  const [accountDisplay, setAccountDisplay] = useState('')
  const [weight, setWeight] = useState('1')
  const [revenueCents, setRevenueCents] = useState('')
  const [currency, setCurrency] = useState('USD')
  const [tier, setTier] = useState('')
  const [sizeSegment, setSizeSegment] = useState('')
  const [lifecycleStatus, setLifecycleStatus] = useState('')
  const add = useAddCustomerRequestVote(requestID)
  const normalizedSubjectKey = subjectKey.trim()
  const normalizedAccountKey = accountKey.trim()
  const hasAccountProfile = normalizedAccountKey.length > 0
  const parsedWeight = Number(weight)
  const parsedRevenue = parseMoneyCents(revenueCents)
  /* v8 ignore next -- @preserve: parent detail views only mount this form with a selected request. */
  if (!requestID) return null
  return (
    <div className="rounded-md border p-3">
      <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-5">
        <Input
          value={subjectKey}
          placeholder={t('customer_requests.subject_key_placeholder')}
          onChange={(event) => setSubjectKey(event.target.value)}
        />
        <Input
          value={subjectDisplay}
          placeholder={t('customer_requests.subject_display_placeholder')}
          onChange={(event) => setSubjectDisplay(event.target.value)}
        />
        <Input
          value={accountKey}
          placeholder={t('customer_requests.account_key_placeholder')}
          onChange={(event) => setAccountKey(event.target.value)}
        />
        <Input
          value={accountDisplay}
          placeholder={t('customer_requests.account_display_placeholder')}
          onChange={(event) => setAccountDisplay(event.target.value)}
        />
        <Input
          value={weight}
          inputMode="numeric"
          placeholder={t('customer_requests.vote_weight')}
          onChange={(event) => setWeight(event.target.value)}
        />
        <Input
          value={revenueCents}
          inputMode="numeric"
          placeholder={t('customer_requests.revenue_cents_placeholder')}
          onChange={(event) => setRevenueCents(event.target.value)}
        />
        <Input
          value={currency}
          maxLength={3}
          placeholder={t('customer_requests.currency_placeholder')}
          onChange={(event) => setCurrency(event.target.value.toUpperCase())}
        />
        <Input
          value={tier}
          placeholder={t('customer_requests.tier_placeholder')}
          onChange={(event) => setTier(event.target.value)}
        />
        <Input
          value={sizeSegment}
          placeholder={t('customer_requests.size_segment_placeholder')}
          onChange={(event) => setSizeSegment(event.target.value)}
        />
        <Input
          value={lifecycleStatus}
          placeholder={t('customer_requests.lifecycle_placeholder')}
          onChange={(event) => setLifecycleStatus(event.target.value)}
        />
      </div>
      <Button
        className="mt-2"
        disabled={
          add.isPending ||
          (normalizedSubjectKey.length === 0 && normalizedAccountKey.length === 0) ||
          !Number.isInteger(parsedWeight) ||
          parsedWeight < 1 ||
          parsedWeight > 100 ||
          parsedRevenue === null
        }
        onClick={() =>
          add.mutate(
            {
              subjectKey: normalizedSubjectKey,
              subjectDisplay: subjectDisplay.trim(),
              accountKey: normalizedAccountKey,
              accountDisplay: accountDisplay.trim(),
              weight: parsedWeight,
              accountRevenueCents:
                hasAccountProfile && parsedRevenue !== undefined
                  ? String(parsedRevenue)
                  : undefined,
              accountRevenueCurrency: hasAccountProfile ? currency.trim() || undefined : undefined,
              accountTier: hasAccountProfile ? tier.trim() || undefined : undefined,
              accountSizeSegment: hasAccountProfile ? sizeSegment.trim() || undefined : undefined,
              accountLifecycleStatus: hasAccountProfile
                ? lifecycleStatus.trim() || undefined
                : undefined,
            },
            {
              onSuccess: () => {
                setSubjectKey('')
                setSubjectDisplay('')
                setAccountKey('')
                setAccountDisplay('')
                setWeight('1')
                setRevenueCents('')
                setCurrency('USD')
                setTier('')
                setSizeSegment('')
                setLifecycleStatus('')
              },
              onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
            },
          )
        }
      >
        {add.isPending ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
        {t('customer_requests.add_vote')}
      </Button>
    </div>
  )
}

function NoteForm({ requestID }: { requestID: string }) {
  const { t } = useTranslation()
  const [body, setBody] = useState('')
  const add = useAddCustomerRequestNote(requestID)
  const normalizedBody = body.trim()
  /* v8 ignore next -- @preserve: parent detail views only mount this form with a selected request. */
  if (!requestID) return null
  return (
    <form
      className="grid gap-2 rounded-md border p-3 sm:grid-cols-[minmax(0,1fr)_auto]"
      onSubmit={(event) => {
        event.preventDefault()
        if (!normalizedBody) return
        add.mutate(
          { body: normalizedBody },
          {
            onSuccess: () => setBody(''),
            onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
          },
        )
      }}
    >
      <textarea
        aria-label={t('customer_requests.note_body')}
        className="min-h-20 w-full rounded-md border bg-background px-3 py-2 text-sm outline-none ring-offset-background focus-visible:ring-2 focus-visible:ring-ring"
        maxLength={5000}
        value={body}
        placeholder={t('customer_requests.note_placeholder')}
        onChange={(event) => setBody(event.target.value)}
      />
      <Button type="submit" disabled={add.isPending || normalizedBody.length === 0}>
        {add.isPending ? <Loader2 className="size-4 animate-spin" /> : <Plus className="size-4" />}
        {t('customer_requests.add_note')}
      </Button>
    </form>
  )
}

export function CustomerRequestFormDialog({
  open,
  mode,
  initialFeedbackIDs,
  ownerOptions,
  onOpenChange,
}: {
  open: boolean
  mode: 'create' | 'promote'
  initialFeedbackIDs?: string[]
  ownerOptions?: OwnerFilterOption[]
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  const create = useCreateCustomerRequest()
  const promote = usePromoteFeedbackToCustomerRequest()
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [feedbackIDs, setFeedbackIDs] = useState('')
  const [priority, setPriority] = useState(CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_NONE)
  const [ownerMemberID, setOwnerMemberID] = useState('unassigned')
  const pending = create.isPending || promote.isPending
  const owners = ownerOptions ?? []

  useEffect(() => {
    if (open && mode === 'promote' && initialFeedbackIDs?.length) {
      setFeedbackIDs(initialFeedbackIDs.join(', '))
    }
  }, [initialFeedbackIDs, mode, open])

  const submit = () => {
    const idempotencyKey = makeIdempotencyKey()
    if (mode === 'promote') {
      const ids = parseFeedbackIDs(feedbackIDs)
      if (ids.length === 0) return
      promote.mutate(
        {
          feedbackIds: ids,
          title,
          description,
          status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
          priority,
          ownerMemberId: ownerMemberID === 'unassigned' ? undefined : ownerMemberID,
          idempotencyKey,
        },
        {
          onSuccess: () => {
            reset()
            onOpenChange(false)
          },
          onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
        },
      )
      return
    }
    create.mutate(
      {
        title,
        description,
        status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
        priority,
        ownerMemberId: ownerMemberID === 'unassigned' ? undefined : ownerMemberID,
        idempotencyKey,
      },
      {
        onSuccess: () => {
          reset()
          onOpenChange(false)
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  const reset = () => {
    setTitle('')
    setDescription('')
    setFeedbackIDs('')
    setPriority(CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_NONE)
    setOwnerMemberID('unassigned')
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {mode === 'promote'
              ? t('customer_requests.promote_dialog_title')
              : t('customer_requests.create_dialog_title')}
          </DialogTitle>
          <DialogDescription className="sr-only">
            {t('customer_requests.subtitle')}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          {mode === 'promote' ? (
            <div className="space-y-2">
              <Label htmlFor="customer-request-feedback-ids">
                {t('customer_requests.feedback_ids')}
              </Label>
              <Input
                id="customer-request-feedback-ids"
                value={feedbackIDs}
                placeholder={t('customer_requests.feedback_ids_placeholder')}
                onChange={(event) => setFeedbackIDs(event.target.value)}
              />
            </div>
          ) : null}
          <div className="space-y-2">
            <Label htmlFor="customer-request-title">{t('customer_requests.title_label')}</Label>
            <Input
              id="customer-request-title"
              value={title}
              onChange={(event) => setTitle(event.target.value)}
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="customer-request-description">
              {t('customer_requests.description_label')}
            </Label>
            <textarea
              id="customer-request-description"
              className="min-h-28 w-full rounded-md border bg-background px-3 py-2 text-sm outline-none ring-offset-background focus-visible:ring-2 focus-visible:ring-ring"
              value={description}
              onChange={(event) => setDescription(event.target.value)}
            />
          </div>
          <Select
            value={priority}
            onValueChange={(value) => setPriority(value as CustomerRequestPriority)}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_NONE}>
                {t('customer_requests.priorities.none')}
              </SelectItem>
              <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_LOW}>
                {t('customer_requests.priorities.low')}
              </SelectItem>
              <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_MEDIUM}>
                {t('customer_requests.priorities.medium')}
              </SelectItem>
              <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH}>
                {t('customer_requests.priorities.high')}
              </SelectItem>
              <SelectItem value={CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT}>
                {t('customer_requests.priorities.urgent')}
              </SelectItem>
            </SelectContent>
          </Select>
          {owners.length > 0 ? (
            <div className="space-y-2">
              <Label>{t('customer_requests.owner_filter')}</Label>
              <Select value={ownerMemberID} onValueChange={setOwnerMemberID}>
                <SelectTrigger aria-label={t('customer_requests.owner_filter')}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="unassigned">
                    {t('customer_requests.owner_unassigned')}
                  </SelectItem>
                  {owners.map((owner) => (
                    <SelectItem key={owner.id} value={owner.id}>
                      {owner.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t('common.cancel')}
          </Button>
          <Button disabled={pending || title.trim().length === 0} onClick={submit}>
            {pending ? <Loader2 className="size-4 animate-spin" /> : null}
            {mode === 'promote' ? t('customer_requests.actions.promote') : t('common.save')}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function FeedbackEvidenceRow({
  requestID,
  item,
  canEdit,
}: {
  requestID: string
  item: CustomerRequestFeedbackEvidence
  canEdit: boolean
}) {
  const { t } = useTranslation()
  const unlink = useUnlinkCustomerRequestFeedback(requestID)
  return (
    <div className="rounded-md border p-3">
      <div className="mb-1 flex items-center justify-between gap-3 text-xs text-muted-foreground">
        <span>#{item.feedbackId}</span>
        <span>{formatDate(item.createdAt)}</span>
      </div>
      <div className="flex items-start justify-between gap-3">
        <p className="line-clamp-3 text-sm">{item.enrichedTitle || item.content}</p>
        {canEdit ? (
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('customer_requests.unlink_feedback')}
            disabled={unlink.isPending}
            onClick={() =>
              unlink.mutate(item.feedbackId, {
                onError: (err) =>
                  toast.error(err instanceof Error ? err.message : t('common.error')),
              })
            }
          >
            {unlink.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Trash2 className="size-4" />
            )}
          </Button>
        ) : null}
      </div>
    </div>
  )
}

function CustomerLinkRow({
  requestID,
  item,
  canEdit,
}: {
  requestID: string
  item: CustomerRequestCustomerLink
  canEdit: boolean
}) {
  const { t } = useTranslation()
  const unlink = useUnlinkCustomerRequestCustomer(requestID)
  const account = item.accountDisplay || item.accountKey
  return (
    <div className="rounded-md border p-3 text-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <div className="font-medium">{supporterLabel(item)}</div>
          {account ? <div className="text-muted-foreground">{account}</div> : null}
          {item.note ? <p className="text-muted-foreground">{item.note}</p> : null}
          <div className="text-xs text-muted-foreground">{formatDate(item.createdAt)}</div>
        </div>
        {canEdit ? (
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('customer_requests.unlink_customer')}
            disabled={unlink.isPending}
            onClick={() =>
              unlink.mutate(item.id, {
                onError: (err) =>
                  toast.error(err instanceof Error ? err.message : t('common.error')),
              })
            }
          >
            {unlink.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Trash2 className="size-4" />
            )}
          </Button>
        ) : null}
      </div>
    </div>
  )
}

function VoteRow({
  requestID,
  item,
  canEdit,
}: {
  requestID: string
  item: CustomerRequestVote
  canEdit: boolean
}) {
  const { t } = useTranslation()
  const remove = useRemoveCustomerRequestVote(requestID)
  const account = item.accountDisplay || item.accountKey
  return (
    <div className="rounded-md border p-3 text-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{supporterLabel(item)}</span>
            <span className="rounded bg-muted px-2 py-0.5 text-xs">
              {t('customer_requests.vote_weight_value', { count: item.weight })}
            </span>
          </div>
          {account ? <div className="text-muted-foreground">{account}</div> : null}
          {item.note ? <p className="text-muted-foreground">{item.note}</p> : null}
          <div className="text-xs text-muted-foreground">{formatDate(item.createdAt)}</div>
        </div>
        {canEdit ? (
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('customer_requests.remove_vote')}
            disabled={remove.isPending}
            onClick={() =>
              remove.mutate(item.id, {
                onError: (err) =>
                  toast.error(err instanceof Error ? err.message : t('common.error')),
              })
            }
          >
            {remove.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Trash2 className="size-4" />
            )}
          </Button>
        ) : null}
      </div>
    </div>
  )
}

function NoteRow({
  requestID,
  item,
  canEdit,
}: {
  requestID: string
  item: CustomerRequestNote
  canEdit: boolean
}) {
  const { t } = useTranslation()
  const remove = useDeleteCustomerRequestNote(requestID)
  return (
    <div className="rounded-md border p-3 text-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <p className="whitespace-pre-wrap break-words">{item.body}</p>
          <div className="text-xs text-muted-foreground">
            {[item.createdBy, formatDate(item.createdAt)].filter(Boolean).join(' · ')}
          </div>
        </div>
        {canEdit ? (
          <Button
            variant="ghost"
            size="icon"
            aria-label={t('customer_requests.delete_note')}
            disabled={remove.isPending}
            onClick={() =>
              remove.mutate(item.id, {
                onError: (err) =>
                  toast.error(err instanceof Error ? err.message : t('common.error')),
              })
            }
          >
            {remove.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Trash2 className="size-4" />
            )}
          </Button>
        ) : null}
      </div>
    </div>
  )
}

function AccountProfileRow({
  item,
  onInspect,
}: {
  item: CustomerRequestAccountProfile
  onInspect: (accountKey: string) => void
}) {
  const { t } = useTranslation()
  const accountLabel = item.accountDisplay || item.accountKey
  return (
    <div className="rounded-md border p-3 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <div className="font-medium">{accountLabel}</div>
          <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span>{item.accountKey}</span>
            {item.tier ? <span>{item.tier}</span> : null}
            {item.sizeSegment ? <span>{item.sizeSegment}</span> : null}
            {item.lifecycleStatus ? <span>{item.lifecycleStatus}</span> : null}
          </div>
          {item.crmProvider || item.crmExternalId ? (
            <div className="text-xs text-muted-foreground">
              {[item.crmProvider, item.crmExternalId].filter(Boolean).join(' · ')}
            </div>
          ) : null}
        </div>
        <div className="flex items-center gap-1 rounded bg-muted px-2 py-1 text-xs font-medium">
          <DollarSign className="size-3.5" />
          {formatMoney(item.revenueCents, item.revenueCurrency)}
        </div>
      </div>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="mt-3"
        aria-label={t('customer_requests.account_view_requests_for', { value: accountLabel })}
        onClick={() => onInspect(item.accountKey)}
      >
        <Search className="size-3.5" />
        {t('customer_requests.account_view_requests')}
      </Button>
      {item.updatedAt ? (
        <div className="mt-2 text-xs text-muted-foreground">
          {t('customer_requests.updated', { value: formatDate(item.updatedAt) })}
        </div>
      ) : null}
    </div>
  )
}

function DeliveryGraphPanel({ graph }: { graph?: CustomerRequestDeliveryGraph }) {
  const { t } = useTranslation()
  const artifacts = graph?.artifacts ?? []
  const relationships = graph?.relationships ?? []
  const externalArtifacts = artifacts.filter((item) => item.artifactType !== 'customer_request')
  if (!graph || externalArtifacts.length === 0) {
    return <EmptyLine text={t('customer_requests.delivery_graph_empty')} />
  }
  return (
    <div className="rounded-md border text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3 p-3">
        <div className="min-w-0 space-y-1">
          <div className="flex items-center gap-2 font-medium">
            <GitBranch className="size-4 text-muted-foreground" />
            <span>{deliveryHealthLabel(graph.health, t)}</span>
          </div>
          <p className="text-xs text-muted-foreground">
            {graph.healthExplanation ||
              t('customer_requests.delivery_graph_summary', {
                count: artifacts.length,
                relationshipCount: relationships.length,
              })}
          </p>
        </div>
        <div className="rounded bg-muted px-2 py-1 text-xs font-medium text-muted-foreground">
          {t('customer_requests.delivery_graph_summary', {
            count: artifacts.length,
            relationshipCount: relationships.length,
          })}
        </div>
      </div>
      <div className="border-t">
        {externalArtifacts.map((artifact) => (
          <DeliveryArtifactRow key={artifact.id} artifact={artifact} />
        ))}
      </div>
    </div>
  )
}

function DeliveryArtifactRow({ artifact }: { artifact: CustomerRequestDeliveryArtifact }) {
  const { t } = useTranslation()
  const label = artifact.title || artifact.externalKey || artifact.externalUrl || artifact.id
  return (
    <div className="grid gap-2 border-t p-3 first:border-t-0 sm:grid-cols-[minmax(0,1fr)_auto]">
      <div className="min-w-0 space-y-1">
        {artifact.externalUrl ? (
          <a
            className="flex min-w-0 items-center gap-2 font-medium hover:underline"
            href={artifact.externalUrl}
            target="_blank"
            rel="noreferrer"
          >
            <span className="min-w-0 truncate">{label}</span>
            <ExternalLink className="size-4 shrink-0 text-muted-foreground" />
          </a>
        ) : (
          <div className="truncate font-medium">{label}</div>
        )}
        <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
          <span className="rounded bg-muted px-1.5 py-0.5 font-mono">{artifact.provider}</span>
          <span>{artifact.artifactType}</span>
          {artifact.externalKey ? <span>{artifact.externalKey}</span> : null}
          {artifact.status ? <span>{artifact.status}</span> : null}
          {artifact.statusCategory ? <span>{artifact.statusCategory}</span> : null}
          {artifact.assignee ? <span>{artifact.assignee}</span> : null}
          <span>{syncStateLabel(artifact.syncState, t)}</span>
          {artifact.lastSeenAt ? (
            <span>
              {t('customer_requests.delivery_artifact_last_seen', {
                value: formatDate(artifact.lastSeenAt),
              })}
            </span>
          ) : null}
        </div>
        {artifact.syncError ? (
          <p className="text-xs text-destructive">{artifact.syncError}</p>
        ) : null}
      </div>
      <div
        className={cn(
          'h-fit rounded border px-2 py-1 text-xs font-medium',
          deliveryHealthClassName(artifact.health),
        )}
      >
        {deliveryHealthLabel(artifact.health, t)}
      </div>
    </div>
  )
}

function DuplicateRow({ item }: { item: CustomerRequestDuplicate }) {
  return (
    <div className="rounded-md border p-3 text-sm">
      <div className="flex flex-wrap items-center gap-2">
        <span className="rounded border px-2 py-0.5 font-mono text-xs text-muted-foreground">
          {item.displayId}
        </span>
        <span className="font-medium">{item.title}</span>
      </div>
      <div className="mt-1 text-xs text-muted-foreground">{formatDate(item.mergedAt)}</div>
    </div>
  )
}

function isGitHubIssueLink(item: CustomerRequestIssueLink) {
  return item.provider.toLowerCase() === 'github'
}

function IssueLinkForm({
  requestID,
  hasGitHubIssueLink,
}: {
  requestID: string
  hasGitHubIssueLink: boolean
}) {
  const { t } = useTranslation()
  const permissions = usePermissions()
  const [url, setURL] = useState('')
  const [provider, setProvider] = useState('github')
  const [connectionID, setConnectionID] = useState('')
  const [issueNumber, setIssueNumber] = useState('')
  const link = useLinkCustomerRequestIssue(requestID)
  const createGitHubIssue = useCreateCustomerRequestGitHubIssue(requestID)
  const githubConnectionOptionsQuery = useQuery(customerRequestGitHubIssueConnectionOptionsQuery())
  const githubConnectionOptions = githubConnectionOptionsQuery.data ?? []
  const githubConnections = githubConnectionOptions.map((option) => option.connection)
  const selectedGitHubConnectionOption = githubConnectionOptions.find(
    (option) => option.connection.id === connectionID,
  )
  const canUseManagedGitHubLink =
    provider === 'github' &&
    url.trim() === '' &&
    selectedGitHubConnectionOption?.canLink === true &&
    issueNumber.trim() !== ''
  const canLinkIssue = url.trim() !== '' || canUseManagedGitHubLink
  const canCreateGitHubIssue = selectedGitHubConnectionOption?.canCreate === true
  useEffect(() => {
    if (provider !== 'github' || connectionID !== '' || githubConnectionOptions.length === 0) {
      return
    }
    const preferred =
      githubConnectionOptions.find((option) => option.canLink && option.canCreate) ??
      githubConnectionOptions.find((option) => option.canLink) ??
      githubConnectionOptions.find((option) => option.canCreate)
    setConnectionID(preferred?.connection.id ?? '')
  }, [connectionID, githubConnectionOptions, provider])
  /* v8 ignore next -- @preserve: the issue-link form is hidden unless edit permission and request id exist. */
  if (!permissions.can('customer_request:edit') || !requestID) return null
  const handleLinkIssue = () => {
    const trimmedURL = url.trim()
    const body =
      trimmedURL === ''
        ? {
            provider: 'github',
            externalUrl: '',
            connectionId: connectionID,
            issueNumber: issueNumber.trim(),
          }
        : { provider, externalUrl: trimmedURL }
    link.mutate(body, {
      onSuccess: () => {
        setURL('')
        setIssueNumber('')
      },
      onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
    })
  }
  return (
    <div className="rounded-md border p-3">
      <div
        className={cn(
          'grid gap-2',
          hasGitHubIssueLink
            ? 'sm:grid-cols-[9rem_minmax(0,1fr)_auto]'
            : 'sm:grid-cols-[9rem_minmax(0,1fr)_auto_auto]',
        )}
      >
        <Select value={provider} onValueChange={setProvider}>
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="github">GitHub</SelectItem>
            <SelectItem value="jira">Jira</SelectItem>
            <SelectItem value="linear">Linear</SelectItem>
            <SelectItem value="other">Other</SelectItem>
          </SelectContent>
        </Select>
        <Input
          value={url}
          placeholder={t('customer_requests.issue_url')}
          onChange={(event) => setURL(event.target.value)}
        />
        <Button
          disabled={link.isPending || createGitHubIssue.isPending || !canLinkIssue}
          onClick={handleLinkIssue}
        >
          {link.isPending ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Plus className="size-4" />
          )}
          {t('customer_requests.link_issue')}
        </Button>
        {hasGitHubIssueLink ? null : (
          <Button
            variant="secondary"
            disabled={link.isPending || createGitHubIssue.isPending || !canCreateGitHubIssue}
            onClick={() =>
              createGitHubIssue.mutate(
                { connectionId: connectionID },
                {
                  onSuccess: () => toast.success(t('customer_requests.create_github_issue_queued')),
                  onError: (err) =>
                    toast.error(err instanceof Error ? err.message : t('common.error')),
                },
              )
            }
          >
            {createGitHubIssue.isPending ? (
              <Loader2 className="size-4 animate-spin" />
            ) : (
              <Github className="size-4" />
            )}
            {t('customer_requests.create_github_issue')}
          </Button>
        )}
      </div>
      {provider === 'github' ? (
        <div className="mt-2 grid gap-2 sm:grid-cols-[minmax(0,1fr)_9rem]">
          <Select value={connectionID} onValueChange={setConnectionID}>
            <SelectTrigger>
              <SelectValue placeholder={t('customer_requests.github_connection')} />
            </SelectTrigger>
            <SelectContent>
              {githubConnectionOptionsQuery.isLoading ? (
                <SelectItem disabled value="__loading">
                  {t('customer_requests.github_connection_loading')}
                </SelectItem>
              ) : null}
              {!githubConnectionOptionsQuery.isLoading && githubConnections.length === 0 ? (
                <SelectItem disabled value="__empty">
                  {t('customer_requests.no_github_connections')}
                </SelectItem>
              ) : null}
              {githubConnections.map((connection) => (
                <SelectItem key={connection.id} value={connection.id}>
                  {connection.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            value={issueNumber}
            inputMode="numeric"
            placeholder={t('customer_requests.issue_number')}
            onChange={(event) => setIssueNumber(event.target.value)}
          />
        </div>
      ) : null}
    </div>
  )
}

function IssueLinkRow({
  requestID,
  item,
  canEdit,
}: {
  requestID: string
  item: CustomerRequestIssueLink
  canEdit: boolean
}) {
  const { t } = useTranslation()
  const unlink = useUnlinkCustomerRequestIssue(requestID)
  const recordSync = useRecordCustomerRequestIssueSync(requestID)
  return (
    <div className="rounded-md border p-3 text-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <a
            className="flex min-w-0 items-center gap-2 hover:underline"
            href={item.externalUrl}
            target="_blank"
            rel="noreferrer"
          >
            <span className="min-w-0 truncate">
              {item.title || item.externalKey || item.externalUrl}
            </span>
            <ExternalLink className="size-4 shrink-0 text-muted-foreground" />
          </a>
          <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span>{syncStateLabel(item.syncState, t)}</span>
            {item.status ? <span>{item.status}</span> : null}
            {item.externalStatusCategory ? <span>{item.externalStatusCategory}</span> : null}
            {item.externalAssignee ? <span>{item.externalAssignee}</span> : null}
            {item.lastSyncedAt ? (
              <span>
                {t('customer_requests.last_synced', { value: formatDate(item.lastSyncedAt) })}
              </span>
            ) : null}
          </div>
          {item.syncError ? <p className="text-xs text-destructive">{item.syncError}</p> : null}
        </div>
        {canEdit ? (
          <div className="flex shrink-0 items-center gap-1">
            <Button
              variant="ghost"
              size="sm"
              disabled={recordSync.isPending}
              onClick={() =>
                recordSync.mutate(
                  {
                    issueLinkId: item.id,
                    syncState:
                      CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
                    status: item.status,
                  },
                  {
                    onError: (err) =>
                      toast.error(err instanceof Error ? err.message : t('common.error')),
                  },
                )
              }
            >
              {recordSync.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <RefreshCw className="size-4" />
              )}
              {t('customer_requests.record_sync')}
            </Button>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t('customer_requests.unlink_issue')}
              disabled={unlink.isPending}
              onClick={() =>
                unlink.mutate(item.id, {
                  onError: (err) =>
                    toast.error(err instanceof Error ? err.message : t('common.error')),
                })
              }
            >
              {unlink.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Trash2 className="size-4" />
              )}
            </Button>
          </div>
        ) : null}
      </div>
    </div>
  )
}

function DecisionRecordsSection({
  records,
  currency,
}: {
  records: CustomerRequestDecisionRecord[]
  currency: string
}) {
  const { t } = useTranslation()
  return (
    <DetailSection title={t('customer_requests.decision_records')}>
      {records.length === 0 ? (
        <EmptyLine text={t('customer_requests.no_decision_records')} />
      ) : (
        <div className="space-y-2">
          {records.map((record) => (
            <DecisionRecordRow key={record.auditId} record={record} currency={currency} />
          ))}
        </div>
      )}
    </DetailSection>
  )
}

function DecisionRecordRow({
  record,
  currency,
}: {
  record: CustomerRequestDecisionRecord
  currency: string
}) {
  const { t } = useTranslation()
  return (
    <article className="space-y-3 rounded-md border p-3 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <div className="font-medium">
            {record.summary || decisionRecordActionLabel(t, record)}
          </div>
          <div className="text-xs text-muted-foreground">
            {t('customer_requests.decision_record_meta', {
              actor: record.actorId || record.actorType || t('customer_requests.actor_unknown'),
              value: formatDate(record.createdAt),
            })}
          </div>
          {record.ownerDisplay || record.ownerMemberId ? (
            <div className="mt-1 text-xs text-muted-foreground">
              {t('customer_requests.decision_record_owner', {
                value: record.ownerDisplay || record.ownerMemberId,
              })}
            </div>
          ) : null}
        </div>
        {record.hasDecisionSnapshot ? (
          <span className="rounded border px-2 py-0.5 font-mono text-xs tabular-nums text-muted-foreground">
            {t('customer_requests.decision_score', { count: record.decisionScore })}
          </span>
        ) : null}
      </div>
      <DecisionRecordChanges record={record} />
      {record.hasDecisionSnapshot ? (
        <div className="space-y-2">
          {record.decisionRationale ? (
            <p className="rounded bg-muted/40 px-3 py-2 text-xs leading-5 text-muted-foreground">
              {t('customer_requests.decision_record_rationale', {
                value: record.decisionRationale,
              })}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span>
              {t('customer_requests.feedback_count', {
                count: record.supportingFeedbackCount,
              })}
            </span>
            <span>{t('customer_requests.customer_count', { count: record.customerCount })}</span>
            <span>{t('customer_requests.account_count', { count: record.accountCount })}</span>
            <span>{t('customer_requests.vote_count', { count: record.voteCount })}</span>
            <span>
              {t('customer_requests.revenue_impact', {
                value: formatMoney(record.revenueImpactCents, record.revenueCurrency || currency),
              })}
            </span>
            <span>
              {t('customer_requests.delivery_health', {
                value: deliveryHealthLabel(record.deliveryHealth, t),
              })}
            </span>
            {record.evidenceBundleRef ? (
              <span>
                {t('customer_requests.decision_record_evidence_bundle', {
                  value: record.evidenceBundleRef,
                })}
              </span>
            ) : null}
            <span>
              {t('customer_requests.decision_record_public_safe', {
                value: decisionPublicSafeStateLabel(t, record.publicSafeState),
              })}
            </span>
          </div>
          {(record.publicSafeReasons ?? []).length > 0 ? (
            <div className="flex flex-wrap gap-1.5">
              {(record.publicSafeReasons ?? []).map((reason) => (
                <span
                  key={reason}
                  className="rounded border border-amber-200 bg-amber-50 px-2 py-0.5 text-xs text-amber-900"
                >
                  {decisionPublicSafeReasonLabel(t, reason)}
                </span>
              ))}
            </div>
          ) : null}
          <div className="flex flex-wrap gap-1.5">
            {record.decisionScoreFactors
              .filter((factor) => factor.contributesToScore)
              .slice(0, 4)
              .map((factor) => (
                <span
                  key={factor.kind}
                  className="rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground"
                >
                  {decisionFactorLabel(t, factor.kind)} +{factor.contribution}
                </span>
              ))}
          </div>
        </div>
      ) : (
        <div className="text-xs text-muted-foreground">
          {t('customer_requests.decision_record_no_snapshot')}
        </div>
      )}
    </article>
  )
}

function DecisionRecordChanges({ record }: { record: CustomerRequestDecisionRecord }) {
  const { t } = useTranslation()
  const changes = decisionRecordChanges(record, t)
  if (changes.length === 0) {
    return null
  }
  return (
    <div className="flex flex-wrap gap-1.5">
      {changes.map((change) => (
        <span key={change} className="rounded bg-muted px-2 py-0.5 text-xs text-muted-foreground">
          {change}
        </span>
      ))}
    </div>
  )
}

function decisionRecordChanges(record: CustomerRequestDecisionRecord, t: TranslateFn): string[] {
  const changes: string[] = []
  if (record.statusChanged) {
    changes.push(
      t('customer_requests.decision_record_status_changed', {
        old: statusLabel(t, record.oldStatus),
        next: statusLabel(t, record.newStatus),
      }),
    )
  }
  if (record.priorityChanged) {
    changes.push(
      t('customer_requests.decision_record_priority_changed', {
        old: priorityLabel(t, record.oldPriority),
        next: priorityLabel(t, record.newPriority),
      }),
    )
  }
  if (record.ownerChanged) changes.push(t('customer_requests.decision_record_owner_changed'))
  if (record.titleChanged) changes.push(t('customer_requests.decision_record_title_changed'))
  if (record.descriptionChanged) {
    changes.push(t('customer_requests.decision_record_description_changed'))
  }
  return changes
}

function decisionRecordActionLabel(t: TranslateFn, record: CustomerRequestDecisionRecord): string {
  const actionKey = record.action.split('.').pop() ?? record.action
  return t('customer_requests.decision_record_action', { value: actionKey })
}

function decisionPublicSafeStateLabel(
  t: TranslateFn,
  state?: CustomerRequestDecisionPublicSafeState,
): string {
  switch (state) {
    case CustomerRequestDecisionPublicSafeState.CUSTOMER_REQUEST_DECISION_PUBLIC_SAFE_STATE_PUBLIC_SAFE:
      return t('customer_requests.decision_public_safe_states.public_safe')
    case CustomerRequestDecisionPublicSafeState.CUSTOMER_REQUEST_DECISION_PUBLIC_SAFE_STATE_NEEDS_REVIEW:
      return t('customer_requests.decision_public_safe_states.needs_review')
    case CustomerRequestDecisionPublicSafeState.CUSTOMER_REQUEST_DECISION_PUBLIC_SAFE_STATE_INTERNAL_ONLY:
      return t('customer_requests.decision_public_safe_states.internal_only')
    default:
      return t('customer_requests.decision_public_safe_states.unknown')
  }
}

function decisionPublicSafeReasonLabel(t: TranslateFn, reason: string): string {
  switch (reason) {
    case 'hidden_feedback':
      return t('customer_requests.decision_public_safe_reasons.hidden_feedback')
    case 'revenue_context':
      return t('customer_requests.decision_public_safe_reasons.revenue_context')
    case 'missing_evidence':
      return t('customer_requests.decision_public_safe_reasons.missing_evidence')
    case 'failed_delivery_link':
      return t('customer_requests.decision_public_safe_reasons.failed_delivery_link')
    default:
      return t('customer_requests.decision_public_safe_reasons.unknown', { value: reason })
  }
}

function EvidenceQualityBadge({ quality }: { quality?: CustomerRequestEvidenceQuality }) {
  const { t } = useTranslation()
  if (!quality) {
    return null
  }
  return (
    <span className={cn('rounded px-2 py-0.5 text-xs', evidenceQualityTone(quality))}>
      {t('customer_requests.evidence_quality_score', {
        score: quality.score,
        confidence: evidenceConfidenceLabel(t, quality.confidence),
      })}
    </span>
  )
}

function EvidenceQualityPanel({ quality }: { quality?: CustomerRequestEvidenceQuality }) {
  const { t } = useTranslation()
  if (!quality) {
    return null
  }
  const latest = quality.latestEvidenceAt
    ? formatDate(quality.latestEvidenceAt)
    : t('customer_requests.evidence_quality_no_latest')
  return (
    <section className="space-y-3 rounded-md border bg-muted/10 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h3 className="text-sm font-semibold">{t('customer_requests.evidence_quality_title')}</h3>
          <p className="mt-1 text-xs text-muted-foreground">
            {t('customer_requests.evidence_quality_summary', {
              score: quality.score,
              confidence: evidenceConfidenceLabel(t, quality.confidence),
              latest,
            })}
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          {quality.lowConfidence ? (
            <span className="rounded bg-rose-100 px-2 py-1 text-xs text-rose-900">
              {t('customer_requests.evidence_quality_low_confidence')}
            </span>
          ) : null}
          {quality.stale ? (
            <span className="rounded bg-amber-100 px-2 py-1 text-xs text-amber-900">
              {t('customer_requests.evidence_quality_stale')}
            </span>
          ) : null}
        </div>
      </div>
      <div className="grid gap-2 sm:grid-cols-4">
        <Metric
          label={t('customer_requests.evidence_quality_evidence_count', {
            count: quality.evidenceCount,
          })}
        />
        <Metric
          label={t('customer_requests.evidence_quality_source_count', {
            count: quality.sourceCount,
          })}
        />
        <Metric
          label={t('customer_requests.evidence_quality_customer_count', {
            count: quality.customerCount,
          })}
        />
        <Metric
          label={t('customer_requests.evidence_quality_account_count', {
            count: quality.accountCount,
          })}
        />
      </div>
      <EvidenceReasonList
        title={t('customer_requests.evidence_quality_strengths')}
        reasons={quality.strengths}
        empty={t('customer_requests.evidence_quality_no_strengths')}
      />
      <EvidenceReasonList
        title={t('customer_requests.evidence_quality_gaps')}
        reasons={quality.gapReasons}
        empty={t('customer_requests.evidence_quality_no_gaps')}
      />
    </section>
  )
}

function EvidenceReasonList({
  title,
  reasons,
  empty,
}: {
  title: string
  reasons: CustomerRequestEvidenceQualityReason[]
  empty: string
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-2">
      <div className="text-xs font-medium uppercase text-muted-foreground">{title}</div>
      {reasons.length === 0 ? (
        <p className="text-sm text-muted-foreground">{empty}</p>
      ) : (
        <div className="flex flex-wrap gap-2">
          {reasons.map((reason) => (
            <span key={reason} className="rounded border bg-background px-2 py-1 text-xs">
              {evidenceReasonLabel(t, reason)}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}

function evidenceQualityTone(quality: CustomerRequestEvidenceQuality): string {
  if (
    quality.lowConfidence ||
    quality.confidence ===
      CustomerRequestEvidenceConfidence.CUSTOMER_REQUEST_EVIDENCE_CONFIDENCE_LOW
  ) {
    return 'bg-rose-100 text-rose-900'
  }
  if (
    quality.stale ||
    quality.confidence ===
      CustomerRequestEvidenceConfidence.CUSTOMER_REQUEST_EVIDENCE_CONFIDENCE_MEDIUM
  ) {
    return 'bg-amber-100 text-amber-900'
  }
  return 'bg-emerald-100 text-emerald-900'
}

function evidenceConfidenceLabel(
  t: TranslateFn,
  confidence?: CustomerRequestEvidenceConfidence,
): string {
  switch (confidence) {
    case CustomerRequestEvidenceConfidence.CUSTOMER_REQUEST_EVIDENCE_CONFIDENCE_HIGH:
      return t('customer_requests.evidence_confidence.high')
    case CustomerRequestEvidenceConfidence.CUSTOMER_REQUEST_EVIDENCE_CONFIDENCE_MEDIUM:
      return t('customer_requests.evidence_confidence.medium')
    case CustomerRequestEvidenceConfidence.CUSTOMER_REQUEST_EVIDENCE_CONFIDENCE_LOW:
      return t('customer_requests.evidence_confidence.low')
    default:
      return t('customer_requests.evidence_confidence.unknown')
  }
}

function evidenceReasonLabel(t: TranslateFn, reason: CustomerRequestEvidenceQualityReason): string {
  switch (reason) {
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_NO_SUPPORTING_FEEDBACK:
      return t('customer_requests.evidence_reasons.no_supporting_feedback')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_LOW_FEEDBACK_VOLUME:
      return t('customer_requests.evidence_reasons.low_feedback_volume')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_SINGLE_CUSTOMER:
      return t('customer_requests.evidence_reasons.single_customer')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_NO_ACCOUNT_CONTEXT:
      return t('customer_requests.evidence_reasons.no_account_context')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_STALE_EVIDENCE:
      return t('customer_requests.evidence_reasons.stale_evidence')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_NO_DELIVERY_LINK:
      return t('customer_requests.evidence_reasons.no_delivery_link')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_HIDDEN_FEEDBACK:
      return t('customer_requests.evidence_reasons.hidden_feedback')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_SUPPORTING_FEEDBACK:
      return t('customer_requests.evidence_reasons.supporting_feedback')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_MULTI_CUSTOMER:
      return t('customer_requests.evidence_reasons.multi_customer')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_ACCOUNT_CONTEXT:
      return t('customer_requests.evidence_reasons.account_context')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_FRESH_EVIDENCE:
      return t('customer_requests.evidence_reasons.fresh_evidence')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_DELIVERY_LINKED:
      return t('customer_requests.evidence_reasons.delivery_linked')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_SINGLE_SOURCE:
      return t('customer_requests.evidence_reasons.single_source')
    case CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_MULTI_SOURCE:
      return t('customer_requests.evidence_reasons.multi_source')
    default:
      return t('customer_requests.evidence_reasons.unknown')
  }
}

function DecisionScoreBreakdown({ request }: { request?: CustomerRequestSummary }) {
  const { t } = useTranslation()
  const factors = request?.decisionScoreFactors ?? []
  if (factors.length === 0) {
    return null
  }
  const total = factors
    .filter((factor) => factor.contributesToScore)
    .reduce((sum, factor) => sum + factor.contribution, 0)
  return (
    <section className="space-y-3 rounded-md border bg-muted/10 p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="text-sm font-semibold">{t('customer_requests.decision_breakdown')}</h3>
        <span className="font-mono text-xs text-muted-foreground">
          {t('customer_requests.decision_breakdown_total', { count: total })}
        </span>
      </div>
      <div className="grid gap-2 sm:grid-cols-2">
        {factors.map((factor) => (
          <DecisionScoreFactorRow
            key={factor.kind}
            factor={factor}
            currency={request?.revenueCurrency ?? 'USD'}
          />
        ))}
      </div>
    </section>
  )
}

function DecisionScoreFactorRow({
  factor,
  currency,
}: {
  factor: CustomerRequestDecisionScoreFactor
  currency: string
}) {
  const { t } = useTranslation()
  const contribution = factor.contributesToScore
    ? t('customer_requests.decision_factor_contribution', { count: factor.contribution })
    : t('customer_requests.decision_factor_context')
  return (
    <div className="rounded border bg-background px-3 py-2 text-sm">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="font-medium">{decisionFactorLabel(t, factor.kind)}</div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {decisionFactorFormula(t, factor, currency)}
          </div>
        </div>
        <div className="shrink-0 text-right">
          <div className="font-mono text-sm font-semibold tabular-nums">{contribution}</div>
          {factor.capped ? (
            <div className="mt-1 rounded bg-amber-100 px-1.5 py-0.5 text-[11px] text-amber-900">
              {t('customer_requests.decision_factor_capped')}
            </div>
          ) : null}
        </div>
      </div>
    </div>
  )
}

function decisionFactorLabel(t: TranslateFn, kind: CustomerRequestDecisionScoreFactorKind): string {
  switch (kind) {
    case CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_PRIORITY:
      return t('customer_requests.decision_factor_priority')
    case CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_FEEDBACK:
      return t('customer_requests.decision_factor_feedback')
    case CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_CUSTOMERS:
      return t('customer_requests.decision_factor_customers')
    case CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_ACCOUNTS:
      return t('customer_requests.decision_factor_accounts')
    case CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_VOTES:
      return t('customer_requests.decision_factor_votes')
    case CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_REVENUE:
      return t('customer_requests.decision_factor_revenue')
    case CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_DELIVERY_HEALTH:
      return t('customer_requests.decision_factor_delivery')
    default:
      return t('customer_requests.decision_factor_unknown')
  }
}

function decisionFactorFormula(
  t: TranslateFn,
  factor: CustomerRequestDecisionScoreFactor,
  currency: string,
): string {
  switch (factor.kind) {
    case CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_PRIORITY:
      return t('customer_requests.decision_factor_priority_formula', { weight: factor.weight })
    case CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_REVENUE:
      return t('customer_requests.decision_factor_revenue_formula', {
        value: formatMoney(factor.rawValueCents, currency),
        unit: formatMoney(factor.unitCents, currency),
        cap: factor.cap,
      })
    case CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_DELIVERY_HEALTH:
      return t('customer_requests.decision_factor_delivery_formula', {
        count: factor.rawCount,
      })
    default:
      return t('customer_requests.decision_factor_count_formula', {
        count: factor.rawCount,
        weight: factor.weight,
        cap: factor.cap,
      })
  }
}

function DetailSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-2">
      <h3 className="text-sm font-semibold">{title}</h3>
      {children}
    </section>
  )
}

function Metric({ label }: { label: string }) {
  return <div className="text-sm font-medium">{label}</div>
}

function EmptyLine({ text }: { text: string }) {
  return (
    <div className="rounded-md border border-dashed p-4 text-sm text-muted-foreground">{text}</div>
  )
}

function StatusPill({ status }: { status: CustomerRequestStatus }) {
  const { t } = useTranslation()
  return <span className="rounded bg-muted px-2 py-0.5 text-xs">{statusLabel(t, status)}</span>
}

function PriorityPill({ priority }: { priority: CustomerRequestPriority }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'rounded px-2 py-0.5 text-xs',
        priority === CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT
          ? 'bg-destructive text-destructive-foreground'
          : 'bg-muted',
      )}
    >
      {priorityLabel(t, priority)}
    </span>
  )
}

function DeliveryHealthPill({ health }: { health: CustomerRequestDeliveryHealth }) {
  const { t } = useTranslation()
  return (
    <span
      className={cn(
        'rounded px-2 py-0.5 text-xs',
        health === CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_FAILED
          ? 'bg-destructive text-destructive-foreground'
          : health === CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_STALE
            ? 'bg-amber-100 text-amber-900'
            : health === CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_PENDING
              ? 'bg-sky-100 text-sky-900'
              : 'bg-muted',
      )}
    >
      {deliveryHealthLabel(health, t)}
    </span>
  )
}

function CustomerRequestSkeleton() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-24" />
      <Skeleton className="h-24" />
      <Skeleton className="h-24" />
    </div>
  )
}

export function statusLabel(t: (key: string) => string, status: CustomerRequestStatus) {
  switch (status) {
    case CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED:
      return t('customer_requests.statuses.planned')
    case CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_IN_PROGRESS:
      return t('customer_requests.statuses.in_progress')
    case CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_SHIPPED:
      return t('customer_requests.statuses.shipped')
    case CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_CANCELLED:
      return t('customer_requests.statuses.cancelled')
    default:
      return t('customer_requests.statuses.open')
  }
}

export function priorityLabel(t: (key: string) => string, priority: CustomerRequestPriority) {
  switch (priority) {
    case CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_LOW:
      return t('customer_requests.priorities.low')
    case CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_MEDIUM:
      return t('customer_requests.priorities.medium')
    case CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH:
      return t('customer_requests.priorities.high')
    case CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT:
      return t('customer_requests.priorities.urgent')
    default:
      return t('customer_requests.priorities.none')
  }
}

export function deliveryHealthLabel(
  health: CustomerRequestDeliveryHealth | undefined,
  t: (key: string) => string,
) {
  switch (health) {
    case CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_FAILED:
      return t('customer_requests.delivery_health_states.failed')
    case CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_STALE:
      return t('customer_requests.delivery_health_states.stale')
    case CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_PENDING:
      return t('customer_requests.delivery_health_states.pending')
    case CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_SYNCED:
      return t('customer_requests.delivery_health_states.synced')
    case CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_MANUAL:
      return t('customer_requests.delivery_health_states.manual')
    default:
      return t('customer_requests.delivery_health_states.no_links')
  }
}

function deliveryHealthClassName(health: CustomerRequestDeliveryHealth | undefined) {
  switch (health) {
    case CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_FAILED:
      return 'border-destructive/40 bg-destructive/10 text-destructive'
    case CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_STALE:
      return 'border-amber-500/40 bg-amber-500/10 text-amber-700'
    case CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_PENDING:
      return 'border-sky-500/40 bg-sky-500/10 text-sky-700'
    case CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_SYNCED:
      return 'border-emerald-500/40 bg-emerald-500/10 text-emerald-700'
    default:
      return 'border-border bg-muted text-muted-foreground'
  }
}

export function syncStateLabel(state: CustomerRequestIssueSyncState, t: (key: string) => string) {
  switch (state) {
    case CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_PENDING:
      return t('customer_requests.sync_states.pending')
    case CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED:
      return t('customer_requests.sync_states.synced')
    case CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_STALE:
      return t('customer_requests.sync_states.stale')
    case CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_FAILED:
      return t('customer_requests.sync_states.failed')
    default:
      return t('customer_requests.sync_states.manual')
  }
}

export function supporterLabel(
  item: Pick<
    CustomerRequestCustomerLink | CustomerRequestVote,
    'accountDisplay' | 'accountKey' | 'subjectDisplay' | 'subjectHash' | 'subjectKey'
  >,
) {
  return (
    item.subjectDisplay ||
    item.subjectKey ||
    item.subjectHash ||
    item.accountDisplay ||
    item.accountKey
  )
}

export function parseMoneyCents(raw: string) {
  const trimmed = raw.trim()
  if (!trimmed) return undefined
  const parsed = Number(trimmed)
  if (!Number.isInteger(parsed) || parsed < 0) return null
  return parsed
}

export function formatMoney(cents: string | number | undefined, currency = 'USD') {
  const value = Number(cents ?? 0) / 100
  if (!Number.isFinite(value)) return `${currency} 0`
  try {
    return new Intl.NumberFormat(undefined, {
      style: 'currency',
      currency: currency || 'USD',
      maximumFractionDigits: value >= 1000 ? 0 : 2,
    }).format(value)
  } catch {
    return `${currency || 'USD'} ${value.toFixed(value >= 1000 ? 0 : 2)}`
  }
}

export function parseFeedbackIDs(raw: string) {
  return raw
    .split(',')
    .map((id) => id.trim())
    .filter(Boolean)
    .map((id) => Number(id))
    .filter((id) => Number.isInteger(id) && id > 0)
    .map((id) => String(id))
}

function makeIdempotencyKey() {
  if (globalThis.crypto?.randomUUID) {
    return `cr_${globalThis.crypto.randomUUID().replaceAll('-', '').slice(0, 24)}`
  }
  return `cr_${Math.random().toString(36).slice(2, 18)}`
}

function formatDate(value: string | undefined) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export const customerRequestPageTestables = {
  DEFAULT_FILTERS,
  DEFAULT_SCORING_FORM,
  filtersToSavedViewState,
  formatDate,
  makeIdempotencyKey,
  memberLabel,
  normalizeInteger,
  normalizeIntegerString,
  ownerFilterOptions,
  ownerLabel,
  savedViewStateToFilters,
  scoringFormToRequest,
  scoringSettingsToForm,
}
