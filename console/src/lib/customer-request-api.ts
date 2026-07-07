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
  CreateCustomerRequestRequest,
  CustomerRequestDetail,
  CustomerRequestPriority,
  CustomerRequestSort,
  CustomerRequestStatus,
  CustomerRequestVisibility,
  LinkCustomerRequestIssueRequest,
  LinkCustomerToCustomerRequestRequest,
  LinkFeedbackToCustomerRequestRequest,
  ListCustomerRequestsResponse,
  MergeCustomerRequestsRequest,
  PromoteFeedbackToCustomerRequestRequest,
  RecordCustomerRequestIssueSyncRequest,
  SortDirection,
  UpdateCustomerRequestRequest,
} from '@/proto/attune/v1/customer_request'

const BASE = '/fb/v1/console/customer-requests'

export interface CustomerRequestFilters {
  q?: string
  status?: CustomerRequestStatus
  priority?: CustomerRequestPriority
  ownerMemberId?: string
  visibility?: CustomerRequestVisibility
  sort?: CustomerRequestSort
  direction?: SortDirection
  feedbackId?: string
}

export const customerRequestKeys = {
  all: ['console', 'customer-requests'] as const,
  list: (filters: CustomerRequestFilters) =>
    ['console', 'customer-requests', 'list', filters] as const,
  detail: (id: string) => ['console', 'customer-requests', 'detail', id] as const,
}

export const customerRequestsInfiniteQuery = (filters: CustomerRequestFilters = {}) =>
  infiniteQueryOptions({
    queryKey: customerRequestKeys.list(filters),
    queryFn: ({ pageParam, signal }) => {
      const params = buildListParams(filters)
      params.set('limit', '50')
      if (pageParam) params.set('cursor', pageParam)
      const qs = params.toString()
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
      api<CustomerRequestDetail>(`${BASE}/${encodeURIComponent(id ?? '')}`, { signal }),
    staleTime: 10_000,
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
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
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
    onSuccess: (detail) => updateCustomerRequestCache(qc, detail),
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

function updateCustomerRequestCache(qc: QueryClient, detail: CustomerRequestDetail) {
  void qc.invalidateQueries({ queryKey: customerRequestKeys.all })
  if (detail.request?.id) {
    qc.setQueryData(customerRequestKeys.detail(detail.request.id), detail)
  }
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
  return params
}
