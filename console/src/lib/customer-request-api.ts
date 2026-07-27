import {
  infiniteQueryOptions,
  type QueryClient,
  queryOptions,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  AddCustomerRequestNoteRequest,
  AddCustomerRequestVoteRequest,
  CreateCustomerRequestGitHubIssueRequest,
  CreateCustomerRequestGitHubIssueResponse,
  CreateCustomerRequestRequest,
  CreateCustomerRequestSavedViewRequest,
  CustomerRequestDetail,
  CustomerRequestPriority,
  CustomerRequestSavedViewResponse,
  CustomerRequestScoringSettings,
  CustomerRequestSort,
  CustomerRequestStatus,
  CustomerRequestVisibility,
  DeleteCustomerRequestSavedViewResponse,
  LinkCustomerRequestIssueRequest,
  LinkCustomerToCustomerRequestRequest,
  LinkFeedbackToCustomerRequestRequest,
  ListCustomerRequestSavedViewsResponse,
  ListCustomerRequestsResponse,
  MergeCustomerRequestsRequest,
  PromoteFeedbackToCustomerRequestRequest,
  RecordCustomerRequestIssueSyncRequest,
  SortDirection,
  UpdateCustomerRequestRequest,
  UpdateCustomerRequestSavedViewRequest,
  UpdateCustomerRequestScoringSettingsRequest,
} from '@/proto/attune/v1/customer_request'
import {
  type ExternalConnection,
  type ExternalObjectMapping,
  ExternalSyncDirection,
  type ListExternalConnectionsResponse,
  type ListExternalObjectMappingsResponse,
} from '@/proto/attune/v1/external_sync'

const BASE = '/fb/v1/console/customer-requests'
const EXTERNAL_SYNC_BASE = '/fb/v1/console/external-sync'
const githubIssueRefreshDelaysMs = [750, 1_500, 3_000, 5_000] as const

export interface CustomerRequestFilters {
  q?: string
  status?: CustomerRequestStatus
  priority?: CustomerRequestPriority
  ownerMemberId?: string
  visibility?: CustomerRequestVisibility
  sort?: CustomerRequestSort
  direction?: SortDirection
  feedbackId?: string
  cohortId?: string
}

export const customerRequestKeys = {
  all: ['console', 'customer-requests'] as const,
  list: (filters: CustomerRequestFilters) =>
    ['console', 'customer-requests', 'list', filters] as const,
  detail: (id: string) => ['console', 'customer-requests', 'detail', id] as const,
  githubIssueConnectionOptions: () =>
    ['console', 'customer-requests', 'github-issue-connection-options'] as const,
  githubIssueConnections: () =>
    ['console', 'customer-requests', 'github-issue-connections'] as const,
  scoring: () => ['console', 'customer-requests', 'scoring-settings'] as const,
  savedViews: () => ['console', 'customer-requests', 'saved-views'] as const,
}

export const customerRequestsInfiniteQuery = (filters: CustomerRequestFilters = {}) =>
  infiniteQueryOptions({
    queryKey: customerRequestKeys.list(filters),
    queryFn: ({ pageParam, signal }) => {
      const params = buildListParams(filters)
      params.set('limit', '50')
      if (pageParam) params.set('cursor', pageParam)
      const qs = params.toString()
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      return api<ListCustomerRequestsResponse>(`${BASE}${qs ? `?${qs}` : ''}`, { signal })
    },
    initialPageParam: '' as string,
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
    staleTime: 20_000,
  })

export const customerRequestDetailQuery = (id: string | null) =>
  queryOptions({
    queryKey: customerRequestKeys.detail(id ?? ''),
    enabled: Boolean(id),
    queryFn: ({ signal }) =>
      /* v8 ignore next -- @preserve: defensive fallback branch outside the covered contract path. */
      api<CustomerRequestDetail>(`${BASE}/${encodeURIComponent(id ?? '')}`, { signal }),
    staleTime: 10_000,
  })

export const customerRequestGitHubIssueConnectionsQuery = () =>
  queryOptions({
    queryKey: customerRequestKeys.githubIssueConnections(),
    queryFn: async ({ signal }) =>
      (await loadGitHubIssueConnectionOptions(signal))
        .filter((option) => option.canLink)
        .map((option) => option.connection),
    staleTime: 20_000,
  })

export interface CustomerRequestGitHubIssueConnectionOption {
  connection: ExternalConnection
  canLink: boolean
  canCreate: boolean
}

export const customerRequestGitHubIssueConnectionOptionsQuery = () =>
  queryOptions({
    queryKey: customerRequestKeys.githubIssueConnectionOptions(),
    queryFn: ({ signal }) => loadGitHubIssueConnectionOptions(signal),
    staleTime: 20_000,
  })

async function loadGitHubIssueConnectionOptions(
  signal?: AbortSignal,
): Promise<CustomerRequestGitHubIssueConnectionOption[]> {
  const resp = await api<ListExternalConnectionsResponse>(`${EXTERNAL_SYNC_BASE}/connections`, {
    signal,
  })
  const githubConnections = resp.connections.filter(
    (connection) =>
      connection.provider === 'github' && connection.enabled && connection.status === 'active',
  )
  const checked = await Promise.all(
    githubConnections.map(async (connection) => {
      const mappings = await api<ListExternalObjectMappingsResponse>(
        `${EXTERNAL_SYNC_BASE}/mappings?connection_id=${encodeURIComponent(connection.id)}`,
        { signal },
      )
      const canLink = mappings.mappings.some(isPullCapableGitHubIssueMapping)
      const canCreate = mappings.mappings.some(isPushCapableGitHubIssueMapping)
      return { connection, canLink, canCreate }
    }),
  )
  return checked.filter((option) => option.canLink || option.canCreate)
}

function isPullCapableGitHubIssueMapping(mapping: ExternalObjectMapping) {
  return (
    mapping.enabled &&
    mapping.localObjectType === 'customer_request' &&
    mapping.externalObjectType === 'issue' &&
    (mapping.direction === ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL ||
      mapping.direction === ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL)
  )
}

function isPushCapableGitHubIssueMapping(mapping: ExternalObjectMapping) {
  return (
    mapping.enabled &&
    mapping.localObjectType === 'customer_request' &&
    mapping.externalObjectType === 'issue' &&
    (mapping.direction === ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PUSH ||
      mapping.direction === ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL)
  )
}

export const customerRequestScoringSettingsQuery = () =>
  queryOptions({
    queryKey: customerRequestKeys.scoring(),
    queryFn: ({ signal }) =>
      api<CustomerRequestScoringSettings>(`${BASE}/scoring-settings`, { signal }),
    staleTime: 30_000,
  })

export const customerRequestSavedViewsQuery = () =>
  queryOptions({
    queryKey: customerRequestKeys.savedViews(),
    queryFn: ({ signal }) =>
      api<ListCustomerRequestSavedViewsResponse>(`${BASE}/saved-views`, { signal }),
    staleTime: 15_000,
  })

export function useCreateCustomerRequest() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: CreateCustomerRequestRequest) =>
      api<CustomerRequestDetail>(BASE, { method: 'POST', body }),
    onSuccess: (detail) => {
      void qc.invalidateQueries({ queryKey: customerRequestKeys.all })
      if (detail.request?.id) {
        qc.setQueryData(customerRequestKeys.detail(detail.request.id), detail)
      }
    },
  })
}

export function usePromoteFeedbackToCustomerRequest() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: PromoteFeedbackToCustomerRequestRequest) =>
      api<CustomerRequestDetail>(`${BASE}:promote-feedback`, { method: 'POST', body }),
    onSuccess: (detail) => {
      void qc.invalidateQueries({ queryKey: customerRequestKeys.all })
      // Link state feeds the recurrence card's dedup guard — refresh it
      // so an already-promoted cluster stops offering a second promote.
      void qc.invalidateQueries({ queryKey: ['console', 'feedback', 'similar'] })
      if (detail.request?.id) {
        qc.setQueryData(customerRequestKeys.detail(detail.request.id), detail)
      }
    },
  })
}

export function useUpdateCustomerRequest(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Omit<UpdateCustomerRequestRequest, 'id'>) =>
      api<CustomerRequestDetail>(`${BASE}/${encodeURIComponent(id)}`, {
        method: 'PATCH',
        body: { id, ...body },
      }),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useLinkCustomerRequestFeedback(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Omit<LinkFeedbackToCustomerRequestRequest, 'id'>) =>
      api<CustomerRequestDetail>(`${BASE}/${encodeURIComponent(id)}/feedback`, {
        method: 'POST',
        body: { id, ...body },
      }),
    onSuccess: (detail) => {
      updateCustomerRequestCache(qc, detail)
      // Refresh the recurrence card's dedup state (anchor/neighbor
      // linked_requests) so the link action flips to its linked state.
      void qc.invalidateQueries({ queryKey: ['console', 'feedback', 'similar'] })
    },
  })
}

export function useUnlinkCustomerRequestFeedback(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (feedbackId: string) =>
      api<CustomerRequestDetail>(
        `${BASE}/${encodeURIComponent(id)}/feedback/${encodeURIComponent(feedbackId)}`,
        { method: 'DELETE' },
      ),
    onSuccess: (detail) => {
      updateCustomerRequestCache(qc, detail)
      // Symmetric with link/promote: unlinking frees the anchor, so the
      // recurrence card must flip back out of its terminal state.
      void qc.invalidateQueries({ queryKey: ['console', 'feedback', 'similar'] })
    },
  })
}

export function useLinkCustomerRequestCustomer(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Omit<LinkCustomerToCustomerRequestRequest, 'id'>) =>
      api<CustomerRequestDetail>(`${BASE}/${encodeURIComponent(id)}/customers`, {
        method: 'POST',
        body: { id, ...body },
      }),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useUnlinkCustomerRequestCustomer(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (customerLinkId: string) =>
      api<CustomerRequestDetail>(
        `${BASE}/${encodeURIComponent(id)}/customers/${encodeURIComponent(customerLinkId)}`,
        { method: 'DELETE' },
      ),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useAddCustomerRequestVote(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Omit<AddCustomerRequestVoteRequest, 'id'>) =>
      api<CustomerRequestDetail>(`${BASE}/${encodeURIComponent(id)}/votes`, {
        method: 'POST',
        body: { id, ...body },
      }),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useRemoveCustomerRequestVote(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (voteId: string) =>
      api<CustomerRequestDetail>(
        `${BASE}/${encodeURIComponent(id)}/votes/${encodeURIComponent(voteId)}`,
        { method: 'DELETE' },
      ),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useAddCustomerRequestNote(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Omit<AddCustomerRequestNoteRequest, 'id'>) =>
      api<CustomerRequestDetail>(`${BASE}/${encodeURIComponent(id)}/notes`, {
        method: 'POST',
        body: { id, ...body },
      }),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useDeleteCustomerRequestNote(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (noteId: string) =>
      api<CustomerRequestDetail>(
        `${BASE}/${encodeURIComponent(id)}/notes/${encodeURIComponent(noteId)}`,
        { method: 'DELETE' },
      ),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useMergeCustomerRequests(sourceID: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Omit<MergeCustomerRequestsRequest, 'sourceId'>) =>
      api<CustomerRequestDetail>(`${BASE}/${encodeURIComponent(sourceID)}:merge`, {
        method: 'POST',
        body: { sourceId: sourceID, ...body },
      }),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useLinkCustomerRequestIssue(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Omit<LinkCustomerRequestIssueRequest, 'id'>) =>
      api<CustomerRequestDetail>(`${BASE}/${encodeURIComponent(id)}/issue-links`, {
        method: 'POST',
        body: { id, ...body },
      }),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useCreateCustomerRequestGitHubIssue(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Omit<CreateCustomerRequestGitHubIssueRequest, 'id'> = {}) =>
      api<CreateCustomerRequestGitHubIssueResponse>(
        `${BASE}/${encodeURIComponent(id)}/issue-links:create-github`,
        {
          method: 'POST',
          body: { id, ...body },
        },
      ),
    onSuccess: (response) => {
      if (response.detail) updateCustomerRequestCache(qc, response.detail)
      if (response.runId) {
        void refreshGitHubIssueDetailAfterRun(qc, id, response.detail).catch(() => {
          void qc.invalidateQueries({ queryKey: customerRequestKeys.detail(id) })
          void qc.invalidateQueries({ queryKey: customerRequestKeys.all })
        })
      }
    },
  })
}

export function useUnlinkCustomerRequestIssue(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (issueLinkId: string) =>
      api<CustomerRequestDetail>(
        `${BASE}/${encodeURIComponent(id)}/issue-links/${encodeURIComponent(issueLinkId)}`,
        { method: 'DELETE' },
      ),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useRecordCustomerRequestIssueSync(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: Omit<RecordCustomerRequestIssueSyncRequest, 'id'>) =>
      api<CustomerRequestDetail>(
        `${BASE}/${encodeURIComponent(id)}/issue-links/${encodeURIComponent(body.issueLinkId)}:record-sync`,
        {
          method: 'POST',
          body: { id, ...body },
        },
      ),
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
  })
}

export function useUpdateCustomerRequestScoringSettings() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpdateCustomerRequestScoringSettingsRequest) =>
      api<CustomerRequestScoringSettings>(`${BASE}/scoring-settings`, {
        method: 'PUT',
        body,
      }),
    onSuccess: (settings) => {
      qc.setQueryData(customerRequestKeys.scoring(), settings)
      void qc.invalidateQueries({ queryKey: customerRequestKeys.all })
    },
  })
}

export function useCreateCustomerRequestSavedView() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: CreateCustomerRequestSavedViewRequest) =>
      api<CustomerRequestSavedViewResponse>(`${BASE}/saved-views`, {
        method: 'POST',
        body,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: customerRequestKeys.savedViews() })
    },
  })
}

export function useUpdateCustomerRequestSavedView() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (body: UpdateCustomerRequestSavedViewRequest) =>
      api<CustomerRequestSavedViewResponse>(`${BASE}/saved-views/${encodeURIComponent(body.id)}`, {
        method: 'PUT',
        body,
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: customerRequestKeys.savedViews() })
    },
  })
}

export function useDeleteCustomerRequestSavedView() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      api<DeleteCustomerRequestSavedViewResponse>(`${BASE}/saved-views/${encodeURIComponent(id)}`, {
        method: 'DELETE',
      }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: customerRequestKeys.savedViews() })
    },
  })
}

function updateCustomerRequestCache(qc: QueryClient, detail: CustomerRequestDetail) {
  void qc.invalidateQueries({ queryKey: customerRequestKeys.all })
  if (detail.request?.id) {
    qc.setQueryData(customerRequestKeys.detail(detail.request.id), detail)
  }
}

async function refreshGitHubIssueDetailAfterRun(
  qc: QueryClient,
  id: string,
  initial?: CustomerRequestDetail,
) {
  if (!id || hasRenderedGitHubIssueLink(initial)) return

  for (const delayMs of githubIssueRefreshDelaysMs) {
    await sleep(delayMs)
    const detail = await api<CustomerRequestDetail>(`${BASE}/${encodeURIComponent(id)}`)
    updateCustomerRequestCache(qc, detail)
    if (hasRenderedGitHubIssueLink(detail)) return
  }

  void qc.invalidateQueries({ queryKey: customerRequestKeys.detail(id) })
  void qc.invalidateQueries({ queryKey: customerRequestKeys.all })
}

function hasRenderedGitHubIssueLink(detail?: CustomerRequestDetail) {
  return Boolean(
    detail?.issueLinks.some((link) => link.provider === 'github' && link.externalUrl.trim() !== ''),
  )
}

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function buildListParams(filters: CustomerRequestFilters) {
  const params = new URLSearchParams()
  if (filters.q?.trim()) params.set('q', filters.q.trim())
  if (filters.status) params.set('status', filters.status)
  if (filters.priority) params.set('priority', filters.priority)
  if (filters.ownerMemberId) params.set('owner_member_id', filters.ownerMemberId)
  if (filters.visibility) params.set('visibility', filters.visibility)
  if (filters.sort) params.set('sort', filters.sort)
  if (filters.direction) params.set('direction', filters.direction)
  if (filters.feedbackId) params.set('feedback_id', filters.feedbackId)
  if (filters.cohortId) params.set('cohort_id', filters.cohortId)
  return params
}
