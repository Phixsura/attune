import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import {
  ArrowRight,
  ClipboardList,
  DollarSign,
  ExternalLink,
  GitMerge,
  Loader2,
  Plus,
  RefreshCw,
  Save,
  Search,
  Trash2,
} from 'lucide-react'
import { type ReactNode, useEffect, useMemo, useState } from 'react'
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
  customerRequestDetailQuery,
  customerRequestsInfiniteQuery,
  useAddCustomerRequestVote,
  useCreateCustomerRequest,
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
} from '@/features/customer-requests/api/customer-requests'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { type Member, membersQuery } from '@/lib/members-api'
import { cn } from '@/lib/utils'
import {
  type CustomerRequestAccountProfile,
  type CustomerRequestCustomerLink,
  type CustomerRequestDuplicate,
  type CustomerRequestFeedbackEvidence,
  CustomerRequestImportance,
  type CustomerRequestIssueLink,
  CustomerRequestIssueSyncState,
  type CustomerRequestOwner,
  CustomerRequestPriority,
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

export function CustomerRequestsPage({
  initialPromoteFeedbackIDs = [],
  initialFeedbackID,
}: {
  initialPromoteFeedbackIDs?: string[]
  initialFeedbackID?: string
} = {}) {
  const { t } = useTranslation()
  const permissions = usePermissions()
  const canViewMembers = permissions.can('settings:members:view')
  const members = useQuery({ ...membersQuery(), enabled: canViewMembers })
  const [filters, setFilters] = useState<CustomerRequestFilters>(() => ({
    ...DEFAULT_FILTERS,
    feedbackId: initialFeedbackID,
  }))
  const [selectedID, setSelectedID] = useState<string | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [promoteOpen, setPromoteOpen] = useState(false)
  const initialPromoteKey = initialPromoteFeedbackIDs.join(',')

  useEffect(() => {
    if (initialPromoteKey) setPromoteOpen(true)
  }, [initialPromoteKey])
  useEffect(() => {
    setFilters((current) =>
      current.feedbackId === initialFeedbackID
        ? current
        : { ...current, feedbackId: initialFeedbackID },
    )
  }, [initialFeedbackID])

  const list = useInfiniteQuery(customerRequestsInfiniteQuery(filters))
  const items = useMemo(
    () => list.data?.pages.flatMap((page) => page.requests) ?? [],
    [list.data?.pages],
  )
  const ownerOptions = useMemo(
    () => ownerFilterOptions(items, members.data ?? [], filters.ownerMemberId),
    [filters.ownerMemberId, items, members.data],
  )
  const selected = items.find((item) => item.id === selectedID) ?? null

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
        {permissions.can('customer_request:edit') ? (
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => setPromoteOpen(true)}>
              <ArrowRight className="size-4" />
              {t('customer_requests.promote')}
            </Button>
            <Button onClick={() => setCreateOpen(true)}>
              <Plus className="size-4" />
              {t('customer_requests.create')}
            </Button>
          </div>
        ) : null}
      </section>

      <CustomerRequestToolbar filters={filters} ownerOptions={ownerOptions} onChange={setFilters} />

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
        open={selectedID != null}
        onMerged={setSelectedID}
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
        onOpenChange={setPromoteOpen}
      />
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
  onChange,
}: {
  filters: CustomerRequestFilters
  ownerOptions: OwnerFilterOption[]
  onChange: (filters: CustomerRequestFilters) => void
}) {
  const { t } = useTranslation()
  return (
    <div className="grid gap-3 rounded-md border bg-background p-3 lg:grid-cols-[minmax(15rem,1fr)_repeat(5,11rem)]">
      <div className="relative">
        <Search className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <Input
          aria-label={t('customer_requests.search_placeholder')}
          className="pl-9"
          value={filters.q ?? ''}
          placeholder={t('customer_requests.search_placeholder')}
          onChange={(event) => onChange({ ...filters, q: event.target.value || undefined })}
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
        <SelectTrigger>
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
        <SelectTrigger>
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
        <SelectTrigger>
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
        <SelectTrigger>
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
        </SelectContent>
      </Select>
    </div>
  )
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
  open,
  onMerged,
  onOpenChange,
}: {
  id: string | null
  fallback: CustomerRequestSummary | null
  ownerOptions: OwnerFilterOption[]
  open: boolean
  onMerged: (targetID: string) => void
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
            </div>
            {detail.data.request?.decisionScoreExplanation ? (
              <div className="rounded-md border bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
                {detail.data.request.decisionScoreExplanation}
              </div>
            ) : null}
            {detail.data.description ? (
              <p className="whitespace-pre-wrap text-sm leading-6 text-muted-foreground">
                {detail.data.description}
              </p>
            ) : null}
            {detail.data.request && canEdit ? (
              <RequestEditControls request={detail.data.request} ownerOptions={ownerOptions} />
            ) : null}
            {detail.data.request && canMerge ? (
              <MergeForm sourceID={detail.data.request.id} onMerged={onMerged} />
            ) : null}
            {canEdit ? <FeedbackLinkForm requestID={detail.data.request?.id ?? ''} /> : null}
            {canEdit ? <CustomerLinkForm requestID={detail.data.request?.id ?? ''} /> : null}
            {canEdit ? <VoteForm requestID={detail.data.request?.id ?? ''} /> : null}
            {canEdit ? <IssueLinkForm requestID={detail.data.request?.id ?? ''} /> : null}
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
                    <AccountProfileRow key={item.accountKey} item={item} />
                  ))}
                </div>
              )}
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
  onMerged,
}: {
  sourceID: string
  onMerged: (targetID: string) => void
}) {
  const { t } = useTranslation()
  const merge = useMergeCustomerRequests(sourceID)
  const [targetID, setTargetID] = useState('')
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

function AccountProfileRow({ item }: { item: CustomerRequestAccountProfile }) {
  const { t } = useTranslation()
  return (
    <div className="rounded-md border p-3 text-sm">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <div className="font-medium">{item.accountDisplay || item.accountKey}</div>
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
      {item.updatedAt ? (
        <div className="mt-2 text-xs text-muted-foreground">
          {t('customer_requests.updated', { value: formatDate(item.updatedAt) })}
        </div>
      ) : null}
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

function IssueLinkForm({ requestID }: { requestID: string }) {
  const { t } = useTranslation()
  const permissions = usePermissions()
  const [url, setURL] = useState('')
  const [provider, setProvider] = useState('github')
  const link = useLinkCustomerRequestIssue(requestID)
  if (!permissions.can('customer_request:edit') || !requestID) return null
  return (
    <div className="rounded-md border p-3">
      <div className="grid gap-2 sm:grid-cols-[9rem_minmax(0,1fr)_auto]">
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
          disabled={link.isPending || url.trim().length === 0}
          onClick={() =>
            link.mutate(
              { provider, externalUrl: url },
              {
                onSuccess: () => setURL(''),
                onError: (err) =>
                  toast.error(err instanceof Error ? err.message : t('common.error')),
              },
            )
          }
        >
          {link.isPending ? (
            <Loader2 className="size-4 animate-spin" />
          ) : (
            <Plus className="size-4" />
          )}
          {t('customer_requests.link_issue')}
        </Button>
      </div>
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

function CustomerRequestSkeleton() {
  return (
    <div className="space-y-3">
      <Skeleton className="h-24" />
      <Skeleton className="h-24" />
      <Skeleton className="h-24" />
    </div>
  )
}

function statusLabel(t: (key: string) => string, status: CustomerRequestStatus) {
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

function priorityLabel(t: (key: string) => string, priority: CustomerRequestPriority) {
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

function syncStateLabel(state: CustomerRequestIssueSyncState, t: (key: string) => string) {
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

function supporterLabel(
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

function parseMoneyCents(raw: string) {
  const trimmed = raw.trim()
  if (!trimmed) return undefined
  const parsed = Number(trimmed)
  if (!Number.isInteger(parsed) || parsed < 0) return null
  return parsed
}

function formatMoney(cents: string | number | undefined, currency = 'USD') {
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

function parseFeedbackIDs(raw: string) {
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
