import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import type { ReactNode } from 'react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  customerRequestAccountSummaryQuery,
  customerRequestDetailQuery,
  customerRequestGitHubIssueConnectionOptionsQuery,
  customerRequestGitHubIssueConnectionsQuery,
  customerRequestKeys,
  customerRequestSavedViewsQuery,
  customerRequestScoringSettingsQuery,
  customerRequestsInfiniteQuery,
  customerRequestsQuery,
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
import { act, renderHook, waitFor } from '@/testing/test-utils'

const baseURL = '/fb/v1/console/customer-requests'
const requestID = '11111111-1111-1111-1111-111111111111'
const githubIssueRefreshDelaysMs = [750, 1_500, 3_000, 5_000]

afterEach(() => {
  vi.useRealTimers()
})

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

async function advanceGitHubIssueRefreshTimers(count = githubIssueRefreshDelaysMs.length) {
  for (const delayMs of githubIssueRefreshDelaysMs.slice(0, count)) {
    await advanceTimersBy(delayMs)
  }
}

async function advanceTimersBy(delayMs: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(delayMs)
  })
}

describe('customer request API', () => {
  it('classifies GitHub issue connections by link and create capability', async () => {
    const connection = (id: string, provider = 'github', enabled = true, status = 'active') => ({
      id,
      tenantId: 't-1',
      provider,
      name: id,
      enabled,
      status,
      authType: 'token',
      baseUrl: '',
      providerConfigJson: '{"repo_url":"https://github.com/Phixsura/attune"}',
      scopes: ['issues'],
      lastTestedAt: '',
      lastTestStatus: 'ok',
      lastError: '',
      createdBy: 'tester',
      updatedBy: 'tester',
      createdAt: '2026-07-07T00:00:00Z',
      updatedAt: '2026-07-07T00:00:00Z',
      webhookSecretConfigured: true,
    })
    const mapping = (connectionId: string, direction: string, enabled = true) => ({
      id: `mapping-${connectionId}`,
      tenantId: 't-1',
      connectionId,
      localObjectType: 'customer_request',
      externalObjectType: 'issue',
      direction,
      fieldMappingJson: '{}',
      statusMappingJson: '{}',
      conflictPolicy: 'manual',
      tombstonePolicy: 'mark_stale',
      enabled,
      mappingVersion: 1,
      createdAt: '2026-07-07T00:00:00Z',
      updatedAt: '2026-07-07T00:00:00Z',
    })
    const mappings: Record<string, unknown[]> = {
      'github-pull': [mapping('github-pull', 'EXTERNAL_SYNC_DIRECTION_PULL')],
      'github-bidirectional': [
        mapping('github-bidirectional', 'EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL'),
      ],
      'github-push': [mapping('github-push', 'EXTERNAL_SYNC_DIRECTION_PUSH')],
      'github-disabled-mapping': [
        mapping('github-disabled-mapping', 'EXTERNAL_SYNC_DIRECTION_PULL', false),
      ],
    }
    server.use(
      http.get('/fb/v1/console/external-sync/connections', () =>
        HttpResponse.json({
          connections: [
            connection('github-pull'),
            connection('github-bidirectional'),
            connection('github-push'),
            connection('github-disabled-mapping'),
            connection('github-disabled', 'github', false),
            connection('jira-active', 'jira'),
          ],
        }),
      ),
      http.get('/fb/v1/console/external-sync/mappings', ({ request }) => {
        const connectionID = new URL(request.url).searchParams.get('connection_id') ?? ''
        return HttpResponse.json({ mappings: mappings[connectionID] ?? [] })
      }),
    )

    const queryClient = makeQueryClient()
    const got = await queryClient.fetchQuery(customerRequestGitHubIssueConnectionsQuery())
    const options = await queryClient.fetchQuery(customerRequestGitHubIssueConnectionOptionsQuery())

    expect(got.map((item) => item.id)).toEqual(['github-pull', 'github-bidirectional'])
    expect(
      options.map((option) => ({
        id: option.connection.id,
        canLink: option.canLink,
        canCreate: option.canCreate,
      })),
    ).toEqual([
      { id: 'github-pull', canLink: true, canCreate: false },
      { id: 'github-bidirectional', canLink: true, canCreate: true },
      { id: 'github-push', canLink: false, canCreate: true },
    ])
  })

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
        cohortId: 'cohort-vip',
        accountKey: ' acct:acme ',
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
    expect(first.searchParams.get('cohort_id')).toBe('cohort-vip')
    expect(first.searchParams.get('account_key')).toBe('acct:acme')
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

  it('treats a null list cursor as the end of pagination', async () => {
    const urls: string[] = []
    server.use(
      http.get(baseURL, ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({ requests: [], nextCursor: null })
      }),
    )

    await makeQueryClient().fetchInfiniteQuery({
      ...customerRequestsInfiniteQuery(),
      pages: 2,
    })

    expect(urls).toHaveLength(1)
  })

  it('builds a finite snapshot query for reliability consistency checks', async () => {
    let url = ''
    server.use(
      http.get(baseURL, ({ request }) => {
        url = request.url
        return HttpResponse.json({
          requests: [{ id: 'req-1', title: 'Focus restore' }],
        })
      }),
    )

    const items = await makeQueryClient().fetchQuery(
      customerRequestsQuery(
        {
          visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE,
          sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT,
          direction: SortDirection.SORT_DIRECTION_DESC,
        },
        25,
      ),
    )

    const params = new URL(url).searchParams
    expect(params.get('visibility')).toBe(
      CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE,
    )
    expect(params.get('sort')).toBe(CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT)
    expect(params.get('direction')).toBe(SortDirection.SORT_DIRECTION_DESC)
    expect(params.get('limit')).toBe('25')
    expect(items).toEqual([{ id: 'req-1', title: 'Focus restore' }])
  })

  it('returns an empty finite snapshot when the response omits requests', async () => {
    server.use(http.get(baseURL, () => HttpResponse.json({})))

    await expect(makeQueryClient().fetchQuery(customerRequestsQuery({}, 10))).resolves.toEqual([])
  })

  it('fetches authoritative account summaries with the current filters', async () => {
    let url = ''
    server.use(
      http.get(`${baseURL}/account-summary`, ({ request }) => {
        url = request.url
        return HttpResponse.json({
          accountKey: 'acct:acme',
          requestCount: 2,
          feedbackCount: 6,
          customerCount: 3,
          voteCount: 8,
          issueCount: 3,
          revenueImpactCents: '3400000',
          revenueCurrency: 'USD',
          timeline: [],
        })
      }),
    )

    await expect(
      makeQueryClient().fetchQuery(
        customerRequestAccountSummaryQuery({
          q: ' renewal ',
          status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
          priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
          ownerMemberId: 'member-1',
          visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL,
          sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE,
          direction: SortDirection.SORT_DIRECTION_DESC,
          feedbackId: '42',
          accountKey: ' acct:acme ',
        }),
      ),
    ).resolves.toMatchObject({ accountKey: 'acct:acme', requestCount: 2 })

    const params = new URL(url).searchParams
    expect(params.get('account_key')).toBe('acct:acme')
    expect(params.get('q')).toBe('renewal')
    expect(params.get('status')).toBe(CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN)
    expect(params.get('priority')).toBe(CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH)
    expect(params.get('owner_member_id')).toBe('member-1')
    expect(params.get('visibility')).toBe(CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL)
    expect(params.get('sort')).toBe(CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE)
    expect(params.get('direction')).toBe(SortDirection.SORT_DIRECTION_DESC)
    expect(params.get('feedback_id')).toBe('42')
    expect(params.get('timeline_limit')).toBe('5')
    expect(params.get('event_limit')).toBe('12')
    expect(customerRequestAccountSummaryQuery({}).enabled).toBe(false)
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
        createGitHubIssue: useCreateCustomerRequestGitHubIssue(requestID),
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
      connectionId: 'connection-1',
      issueNumber: '212',
      title: 'Customer requests',
      status: 'open',
    })
    await result.current.createGitHubIssue.mutateAsync({
      connectionId: 'connection-1',
      mappingId: 'mapping-1',
    })
    await result.current.unlinkIssue.mutateAsync('issue-link-1')
    await result.current.recordSync.mutateAsync({
      issueLinkId: 'issue-link-1',
      syncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
      status: 'done',
    })

    await waitFor(() => expect(calls).toHaveLength(16))
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
      `POST ${baseURL}/${requestID}/issue-links:create-github`,
      `DELETE ${baseURL}/${requestID}/issue-links/issue-link-1`,
      `POST ${baseURL}/${requestID}/issue-links/issue-link-1:record-sync`,
    ])
    expect(calls[2].body).toEqual({ id: requestID, title: 'Renamed' })
    expect(calls[9].body).toEqual({ id: requestID, body: 'Coordinate with ACME.' })
    expect(calls[12].body).toEqual({
      id: requestID,
      provider: 'github',
      externalUrl: 'https://github.com/Phixsura/attune/issues/212',
      externalKey: '212',
      connectionId: 'connection-1',
      issueNumber: '212',
      title: 'Customer requests',
      status: 'open',
    })
    expect(calls[13].body).toEqual({
      id: requestID,
      connectionId: 'connection-1',
      mappingId: 'mapping-1',
    })
    expect(calls[15].body).toEqual({
      id: requestID,
      issueLinkId: 'issue-link-1',
      syncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
      status: 'done',
    })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: customerRequestKeys.all })
    expect(qc.getQueryData(customerRequestKeys.detail('cached-request'))).toEqual(detail)
  })

  it('refreshes the detail cache after queued GitHub issue creation surfaces a link', async () => {
    vi.useFakeTimers()
    const queued = sampleDetail(requestID)
    const linked = sampleDetailWithGitHubIssue(requestID)
    let detailLoads = 0
    server.use(
      http.post(`${baseURL}/${requestID}/issue-links:create-github`, () =>
        HttpResponse.json({
          detail: queued,
          runId: 'run-1',
          connectionId: 'connection-1',
          mappingId: 'mapping-1',
        }),
      ),
      http.get(`${baseURL}/${requestID}`, () => {
        detailLoads += 1
        return HttpResponse.json(linked)
      }),
    )
    const qc = makeQueryClient()
    const { result } = renderHook(() => useCreateCustomerRequestGitHubIssue(requestID), {
      wrapper: wrapperFor(qc),
    })

    await result.current.mutateAsync({})

    expect(qc.getQueryData(customerRequestKeys.detail(requestID))).toEqual(queued)
    expect(detailLoads).toBe(0)
    await advanceGitHubIssueRefreshTimers(1)
    expect(qc.getQueryData(customerRequestKeys.detail(requestID))).toEqual(linked)
    expect(detailLoads).toBe(1)
  })

  it('keeps polling until a queued GitHub issue has a rendered external URL', async () => {
    vi.useFakeTimers()
    const queued = sampleDetail(requestID)
    const pending = sampleDetailWithIssueLinks(requestID, [
      { provider: 'jira', externalUrl: 'https://jira.example/browse/ATT-7' },
      { provider: 'github', externalUrl: '   ' },
    ])
    const linked = sampleDetailWithGitHubIssue(requestID)
    let detailLoads = 0
    server.use(
      http.post(`${baseURL}/${requestID}/issue-links:create-github`, () =>
        HttpResponse.json({
          detail: queued,
          runId: 'run-1',
          connectionId: 'connection-1',
          mappingId: 'mapping-1',
        }),
      ),
      http.get(`${baseURL}/${requestID}`, () => {
        detailLoads += 1
        return HttpResponse.json(detailLoads === 1 ? pending : linked)
      }),
    )
    const qc = makeQueryClient()
    const { result } = renderHook(() => useCreateCustomerRequestGitHubIssue(requestID), {
      wrapper: wrapperFor(qc),
    })

    await result.current.mutateAsync({})

    await advanceGitHubIssueRefreshTimers(1)
    expect(qc.getQueryData(customerRequestKeys.detail(requestID))).toEqual(pending)
    await advanceTimersBy(githubIssueRefreshDelaysMs[1])
    expect(qc.getQueryData(customerRequestKeys.detail(requestID))).toEqual(linked)
    expect(detailLoads).toBe(2)
  })

  it('invalidates request caches when queued GitHub issue refresh fails', async () => {
    vi.useFakeTimers()
    const queued = sampleDetail(requestID)
    let detailLoads = 0
    server.use(
      http.post(`${baseURL}/${requestID}/issue-links:create-github`, () =>
        HttpResponse.json({
          detail: queued,
          runId: 'run-1',
          connectionId: 'connection-1',
          mappingId: 'mapping-1',
        }),
      ),
      http.get(`${baseURL}/${requestID}`, () => {
        detailLoads += 1
        return HttpResponse.json({ code: 'internal', message: 'failed' }, { status: 500 })
      }),
    )
    const qc = makeQueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(() => useCreateCustomerRequestGitHubIssue(requestID), {
      wrapper: wrapperFor(qc),
    })

    await result.current.mutateAsync({})
    invalidate.mockClear()

    await advanceGitHubIssueRefreshTimers(1)
    expect(detailLoads).toBe(1)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: customerRequestKeys.detail(requestID) })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: customerRequestKeys.all })
  })

  it('invalidates request caches when queued GitHub issue polling is exhausted', async () => {
    vi.useFakeTimers()
    const queued = sampleDetail(requestID)
    const pending = sampleDetailWithIssueLinks(requestID, [{ provider: 'github', externalUrl: '' }])
    let detailLoads = 0
    server.use(
      http.post(`${baseURL}/${requestID}/issue-links:create-github`, () =>
        HttpResponse.json({
          detail: queued,
          runId: 'run-1',
          connectionId: 'connection-1',
          mappingId: 'mapping-1',
        }),
      ),
      http.get(`${baseURL}/${requestID}`, () => {
        detailLoads += 1
        return HttpResponse.json(pending)
      }),
    )
    const qc = makeQueryClient()
    const invalidate = vi.spyOn(qc, 'invalidateQueries')
    const { result } = renderHook(() => useCreateCustomerRequestGitHubIssue(requestID), {
      wrapper: wrapperFor(qc),
    })

    await result.current.mutateAsync({})
    invalidate.mockClear()

    await advanceGitHubIssueRefreshTimers()
    expect(detailLoads).toBe(4)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: customerRequestKeys.detail(requestID) })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: customerRequestKeys.all })
  })

  it('skips queued GitHub issue polling when the mutation response already has a link', async () => {
    vi.useFakeTimers()
    const linked = sampleDetailWithGitHubIssue(requestID)
    let detailLoads = 0
    server.use(
      http.post(`${baseURL}/${requestID}/issue-links:create-github`, () =>
        HttpResponse.json({
          detail: linked,
          runId: 'run-1',
          connectionId: 'connection-1',
          mappingId: 'mapping-1',
        }),
      ),
      http.get(`${baseURL}/${requestID}`, () => {
        detailLoads += 1
        return HttpResponse.json(linked)
      }),
    )
    const qc = makeQueryClient()
    const { result } = renderHook(() => useCreateCustomerRequestGitHubIssue(requestID), {
      wrapper: wrapperFor(qc),
    })

    await result.current.mutateAsync({})
    await advanceGitHubIssueRefreshTimers()

    expect(qc.getQueryData(customerRequestKeys.detail(requestID))).toEqual(linked)
    expect(detailLoads).toBe(0)
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
      decisionScoreFactors: [],
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
    decisionRecords: [],
  }
}

function sampleDetailWithIssueLinks(
  id: string,
  links: Array<{ provider: string; externalUrl: string }>,
): CustomerRequestDetail {
  const detail = sampleDetail(id)
  return {
    ...detail,
    request: detail.request
      ? {
          ...detail.request,
          linkedIssueCount: 1,
          syncedIssueCount: 1,
          deliveryHealth: CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_SYNCED,
        }
      : undefined,
    issueLinks: links.map((link, index) => ({
      id: `issue-link-${index + 1}`,
      provider: link.provider,
      externalKey: String(702 + index),
      externalUrl: link.externalUrl,
      title: `${link.provider} #${702 + index}`,
      status: 'open',
      createdBy: 'tester',
      createdAt: '2026-07-07T00:00:00Z',
      updatedAt: '2026-07-07T00:00:00Z',
      lastSyncedAt: '2026-07-07T00:00:00Z',
      syncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
      externalStatusCategory: 'open',
      externalAssignee: '',
      externalUpdatedAt: '2026-07-07T00:00:00Z',
      syncError: '',
    })),
  }
}

function sampleDetailWithGitHubIssue(id: string): CustomerRequestDetail {
  return sampleDetailWithIssueLinks(id, [
    { provider: 'github', externalUrl: 'https://github.com/acme/app/issues/702' },
  ])
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
