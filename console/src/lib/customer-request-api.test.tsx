import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import {
  customerRequestDetailQuery,
  customerRequestKeys,
  customerRequestSavedViewsQuery,
  customerRequestScoringSettingsQuery,
  customerRequestsInfiniteQuery,
  useAddCustomerRequestNote,
  useAddCustomerRequestVote,
  useCreateCustomerRequest,
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
} from '@/lib/customer-request-api'
import {
  CustomerRequestDeliveryHealth,
  type CustomerRequestDetail,
  CustomerRequestImportance,
  CustomerRequestIssueSyncState,
  CustomerRequestPriority,
  type CustomerRequestScoringSettings,
  CustomerRequestSort,
  CustomerRequestStatus,
  CustomerRequestVisibility,
  SortDirection,
} from '@/proto/attune/v1/customer_request'
import { server } from '@/testing/mocks/server'
import { renderHook, waitFor } from '@/testing/test-utils'

const baseURL = '/fb/v1/console/customer-requests'
const requestID = '11111111-1111-1111-1111-111111111111'

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function wrapperFor(queryClient: QueryClient) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  )
}

describe('customer request API', () => {
  it('builds list query params for filters and pagination', async () => {
    const urls: string[] = []
    server.use(
      http.get(baseURL, ({ request }) => {
        urls.push(request.url)
        const cursor = new URL(request.url).searchParams.get('cursor')
        return HttpResponse.json({ requests: [], nextCursor: cursor ? undefined : 'next-page' })
      }),
    )

    await makeQueryClient().fetchInfiniteQuery({
      ...customerRequestsInfiniteQuery({
        q: '  exports  ',
        status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
        priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
        ownerMemberId: 'member-1',
        visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL,
        sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE,
        direction: SortDirection.SORT_DIRECTION_DESC,
        feedbackId: '42',
      }),
      pages: 2,
    })

    expect(urls).toHaveLength(2)
    const first = new URL(urls[0])
    expect(first.searchParams.get('q')).toBe('exports')
    expect(first.searchParams.get('status')).toBe(
      CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
    )
    expect(first.searchParams.get('priority')).toBe(
      CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
    )
    expect(first.searchParams.get('owner_member_id')).toBe('member-1')
    expect(first.searchParams.get('visibility')).toBe(
      CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL,
    )
    expect(first.searchParams.get('sort')).toBe(
      CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE,
    )
    expect(first.searchParams.get('direction')).toBe(SortDirection.SORT_DIRECTION_DESC)
    expect(first.searchParams.get('feedback_id')).toBe('42')
    expect(first.searchParams.get('limit')).toBe('50')
    expect(first.searchParams.has('cursor')).toBe(false)
    expect(new URL(urls[1]).searchParams.get('cursor')).toBe('next-page')
  })

  it('keeps empty list filters out of the query string', async () => {
    let url = ''
    server.use(
      http.get(baseURL, ({ request }) => {
        url = request.url
        return HttpResponse.json({ requests: [] })
      }),
    )

    await makeQueryClient().fetchInfiniteQuery(customerRequestsInfiniteQuery({ q: '   ' }))

    const params = new URL(url).searchParams
    expect(params.get('limit')).toBe('50')
    expect(params.has('q')).toBe(false)
    expect(params.has('status')).toBe(false)
  })

  it('fetches details only when an id is present', async () => {
    let path = ''
    const detail = sampleDetail(requestID)
    server.use(
      http.get(`${baseURL}/${requestID}`, ({ request }) => {
        path = new URL(request.url).pathname
        return HttpResponse.json(detail)
      }),
    )

    const disabled = customerRequestDetailQuery(null)
    expect(disabled.enabled).toBe(false)
    expect(disabled.queryKey).toEqual(customerRequestKeys.detail(''))

    await expect(
      makeQueryClient().fetchQuery(customerRequestDetailQuery(requestID)),
    ).resolves.toEqual(detail)
    expect(path).toBe(`${baseURL}/${requestID}`)
  })

  it('fetches and updates scoring settings', async () => {
    let payload: Record<string, unknown> | undefined
    server.use(
      http.get(`${baseURL}/scoring-settings`, () => HttpResponse.json(sampleScoringSettings())),
      http.put(`${baseURL}/scoring-settings`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(
          sampleScoringSettings({
            feedbackWeight: Number(payload.feedbackWeight),
            revenueCentsPerPoint: String(payload.revenueCentsPerPoint),
          }),
        )
      }),
    )
    const qc = makeQueryClient()

    await expect(qc.fetchQuery(customerRequestScoringSettingsQuery())).resolves.toMatchObject({
      feedbackWeight: 2,
      revenueCentsPerPoint: '100000',
    })

    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(() => useUpdateCustomerRequestScoringSettings(), {
      wrapper: wrapperFor(qc),
    })
    await result.current.mutateAsync({
      feedbackWeight: 9,
      revenueCentsPerPoint: '250000',
    })

    expect(payload).toEqual({
      feedbackWeight: 9,
      revenueCentsPerPoint: '250000',
    })
    expect(qc.getQueryData(customerRequestKeys.scoring())).toMatchObject({
      feedbackWeight: 9,
      revenueCentsPerPoint: '250000',
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: customerRequestKeys.all })
  })

  it('fetches and mutates saved views', async () => {
    const calls: Array<{ method: string; path: string; body?: unknown }> = []
    server.use(
      http.get(`${baseURL}/saved-views`, () =>
        HttpResponse.json({
          views: [
            {
              id: 'view-1',
              name: 'Scoreboard',
              state: {
                q: 'renewal',
                status: [CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN],
                priority: [CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH],
                sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE,
                direction: SortDirection.SORT_DIRECTION_DESC,
              },
              createdAt: '2026-07-08T00:00:00Z',
              updatedAt: '2026-07-08T00:00:00Z',
            },
          ],
        }),
      ),
      http.post(`${baseURL}/saved-views`, async ({ request }) => {
        calls.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
        })
        return HttpResponse.json({ view: { id: 'view-created', name: 'New', state: {} } })
      }),
      http.put(`${baseURL}/saved-views/view-1`, async ({ request }) => {
        calls.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
        })
        return HttpResponse.json({ view: { id: 'view-1', name: 'Updated', state: {} } })
      }),
      http.delete(`${baseURL}/saved-views/view-1`, ({ request }) => {
        calls.push({ method: request.method, path: new URL(request.url).pathname })
        return HttpResponse.json({})
      }),
    )
    const qc = makeQueryClient()
    await expect(qc.fetchQuery(customerRequestSavedViewsQuery())).resolves.toMatchObject({
      views: [{ id: 'view-1', name: 'Scoreboard' }],
    })

    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(
      () => ({
        create: useCreateCustomerRequestSavedView(),
        update: useUpdateCustomerRequestSavedView(),
        remove: useDeleteCustomerRequestSavedView(),
      }),
      { wrapper: wrapperFor(qc) },
    )

    await result.current.create.mutateAsync({
      name: 'New',
      state: {
        q: 'exports',
        status: [],
        priority: [],
        visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE,
        sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_REVENUE_IMPACT,
        direction: SortDirection.SORT_DIRECTION_DESC,
      },
    })
    await result.current.update.mutateAsync({
      id: 'view-1',
      name: 'Updated',
      state: {
        q: '',
        status: [],
        priority: [],
        visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL,
        sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT,
        direction: SortDirection.SORT_DIRECTION_DESC,
      },
    })
    await result.current.remove.mutateAsync('view-1')

    expect(calls).toEqual([
      {
        method: 'POST',
        path: `${baseURL}/saved-views`,
        body: {
          name: 'New',
          state: {
            q: 'exports',
            status: [],
            priority: [],
            visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE,
            sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_REVENUE_IMPACT,
            direction: SortDirection.SORT_DIRECTION_DESC,
          },
        },
      },
      {
        method: 'PUT',
        path: `${baseURL}/saved-views/view-1`,
        body: {
          id: 'view-1',
          name: 'Updated',
          state: {
            q: '',
            status: [],
            priority: [],
            visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL,
            sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT,
            direction: SortDirection.SORT_DIRECTION_DESC,
          },
        },
      },
      { method: 'DELETE', path: `${baseURL}/saved-views/view-1` },
    ])
    expect(invalidate).toHaveBeenCalledWith({ queryKey: customerRequestKeys.savedViews() })
  })

  it('posts mutating actions and refreshes customer request caches', async () => {
    const calls: Array<{ method: string; path: string; body?: unknown }> = []
    const detail = sampleDetail('cached-request')
    server.use(
      http.all(/\/fb\/v1\/console\/customer-requests.*/, async ({ request }) => {
        calls.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body:
            request.method === 'GET' || request.method === 'DELETE'
              ? undefined
              : await request.json(),
        })
        return HttpResponse.json(detail)
      }),
    )
    const qc = makeQueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(
      () => ({
        create: useCreateCustomerRequest(),
        promote: usePromoteFeedbackToCustomerRequest(),
        update: useUpdateCustomerRequest(requestID),
        linkFeedback: useLinkCustomerRequestFeedback(requestID),
        unlinkFeedback: useUnlinkCustomerRequestFeedback(requestID),
        linkCustomer: useLinkCustomerRequestCustomer(requestID),
        unlinkCustomer: useUnlinkCustomerRequestCustomer(requestID),
        addVote: useAddCustomerRequestVote(requestID),
        removeVote: useRemoveCustomerRequestVote(requestID),
        addNote: useAddCustomerRequestNote(requestID),
        deleteNote: useDeleteCustomerRequestNote(requestID),
        merge: useMergeCustomerRequests(requestID),
        linkIssue: useLinkCustomerRequestIssue(requestID),
        unlinkIssue: useUnlinkCustomerRequestIssue(requestID),
        recordSync: useRecordCustomerRequestIssueSync(requestID),
      }),
      { wrapper: wrapperFor(qc) },
    )

    await result.current.create.mutateAsync({
      title: 'Export bundles',
      description: 'CSV exports',
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
      ownerMemberId: 'member-1',
      idempotencyKey: 'create-key',
    })
    await result.current.promote.mutateAsync({
      feedbackIds: ['101', '102'],
      title: 'Promoted request',
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_MEDIUM,
      idempotencyKey: 'promote-key',
    })
    await result.current.update.mutateAsync({ title: 'Renamed' })
    await result.current.linkFeedback.mutateAsync({
      feedbackId: '42',
      importance: CustomerRequestImportance.CUSTOMER_REQUEST_IMPORTANCE_IMPORTANT,
      note: 'strong signal',
    })
    await result.current.unlinkFeedback.mutateAsync('42')
    await result.current.linkCustomer.mutateAsync({
      subjectKey: 'subject-1',
      accountKey: 'acme',
      accountRevenueCents: '120000',
    })
    await result.current.unlinkCustomer.mutateAsync('customer-link-1')
    await result.current.addVote.mutateAsync({ subjectKey: 'subject-1', weight: 3 })
    await result.current.removeVote.mutateAsync('vote-1')
    await result.current.addNote.mutateAsync({ body: 'Coordinate with ACME.' })
    await result.current.deleteNote.mutateAsync('note-1')
    await result.current.merge.mutateAsync({ targetId: 'target-1', idempotencyKey: 'merge-key' })
    await result.current.linkIssue.mutateAsync({
      provider: 'github',
      externalUrl: 'https://github.com/Phixsura/attune/issues/212',
      externalKey: '212',
      title: 'Customer requests',
      status: 'open',
    })
    await result.current.unlinkIssue.mutateAsync('issue-link-1')
    await result.current.recordSync.mutateAsync({
      issueLinkId: 'issue-link-1',
      syncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
      status: 'done',
    })

    await waitFor(() => expect(calls).toHaveLength(15))
    expect(calls.map((call) => `${call.method} ${call.path}`)).toEqual([
      `POST ${baseURL}`,
      `POST ${baseURL}:promote-feedback`,
      `PATCH ${baseURL}/${requestID}`,
      `POST ${baseURL}/${requestID}/feedback`,
      `DELETE ${baseURL}/${requestID}/feedback/42`,
      `POST ${baseURL}/${requestID}/customers`,
      `DELETE ${baseURL}/${requestID}/customers/customer-link-1`,
      `POST ${baseURL}/${requestID}/votes`,
      `DELETE ${baseURL}/${requestID}/votes/vote-1`,
      `POST ${baseURL}/${requestID}/notes`,
      `DELETE ${baseURL}/${requestID}/notes/note-1`,
      `POST ${baseURL}/${requestID}:merge`,
      `POST ${baseURL}/${requestID}/issue-links`,
      `DELETE ${baseURL}/${requestID}/issue-links/issue-link-1`,
      `POST ${baseURL}/${requestID}/issue-links/issue-link-1:record-sync`,
    ])
    expect(calls[2].body).toEqual({ id: requestID, title: 'Renamed' })
    expect(calls[9].body).toEqual({ id: requestID, body: 'Coordinate with ACME.' })
    expect(calls[14].body).toEqual({
      id: requestID,
      issueLinkId: 'issue-link-1',
      syncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
      status: 'done',
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: customerRequestKeys.all })
    expect(qc.getQueryData(customerRequestKeys.detail('cached-request'))).toEqual(detail)
  })

  it('does not cache mutation responses without a request id', async () => {
    server.use(
      http.post(baseURL, () => HttpResponse.json({})),
      http.post(`${baseURL}:promote-feedback`, () => HttpResponse.json({})),
      http.patch(`${baseURL}/${requestID}`, () => HttpResponse.json({})),
    )
    const qc = makeQueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(
      () => ({
        create: useCreateCustomerRequest(),
        promote: usePromoteFeedbackToCustomerRequest(),
        update: useUpdateCustomerRequest(requestID),
      }),
      { wrapper: wrapperFor(qc) },
    )

    await result.current.create.mutateAsync({
      title: 'Export bundles',
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
      idempotencyKey: 'create-key',
    })
    await result.current.promote.mutateAsync({
      feedbackIds: ['101'],
      title: 'Promoted request',
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_MEDIUM,
      idempotencyKey: 'promote-key',
    })
    await result.current.update.mutateAsync({ title: 'Renamed' })

    expect(invalidate).toHaveBeenCalledWith({ queryKey: customerRequestKeys.all })
    expect(qc.getQueryData(customerRequestKeys.detail(requestID))).toBeUndefined()
  })
})

function sampleDetail(id: string): CustomerRequestDetail {
  return {
    request: {
      id,
      displayId: 'CR-1',
      displayNumber: '1',
      title: 'Export bundles',
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
      supportingFeedbackCount: 0,
      customerCount: 0,
      linkedIssueCount: 0,
      hiddenFeedbackCount: 0,
      firstFeedbackAt: '',
      latestFeedbackAt: '',
      createdAt: '2026-07-07T00:00:00Z',
      updatedAt: '2026-07-07T00:00:00Z',
      accountCount: 0,
      voteCount: 0,
      duplicateRequestCount: 0,
      revenueImpactCents: '0',
      revenueCurrency: 'USD',
      decisionScore: 0,
      decisionScoreExplanation: '',
      deliveryHealth: CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_NO_LINKS,
      syncedIssueCount: 0,
      staleIssueCount: 0,
      failedIssueCount: 0,
      pendingIssueCount: 0,
      manualIssueCount: 0,
    },
    description: 'CSV exports',
    feedback: [],
    issueLinks: [],
    auditEntries: [],
    customers: [],
    votes: [],
    duplicates: [],
    accountProfiles: [],
    notes: [],
  }
}

function sampleScoringSettings(
  overrides: Partial<CustomerRequestScoringSettings> = {},
): CustomerRequestScoringSettings {
  return {
    tenantId: 't-1',
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
    updatedBy: '',
    updatedAt: '',
    ...overrides,
  }
}
