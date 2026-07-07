import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { CustomerRequestsPage } from '@/features/customer-requests/components/customer-requests-page'
import {
  CustomerRequestDeliveryHealth,
  type CustomerRequestDetail,
  CustomerRequestImportance,
  type CustomerRequestOwner,
  CustomerRequestPriority,
  CustomerRequestStatus,
  type CustomerRequestSummary,
  type ListCustomerRequestsResponse,
} from '@/proto/attune/v1/customer_request'
import type { Member } from '@/proto/attune/v1/member'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

const requestID = '11111111-1111-1111-1111-111111111111'
const targetRequestID = '33333333-3333-3333-3333-333333333333'
const baseURL = '/fb/v1/console/customer-requests'

describe('CustomerRequestsPage', () => {
  it('renders customer request rows and opens detail actions', async () => {
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))

    expect(await screen.findByText('关联反馈')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('反馈 ID')).toBeInTheDocument()
    expect(screen.getAllByText('2 条反馈').length).toBeGreaterThan(0)
    expect(screen.getAllByText('1 位客户').length).toBeGreaterThan(0)
    expect(screen.getAllByText(/收入影响/).length).toBeGreaterThan(0)
    expect(screen.getAllByText('决策分 114').length).toBeGreaterThan(0)
    expect(screen.getAllByText('已同步').length).toBeGreaterThan(0)
  })

  it('posts a create request payload from the dialog', async () => {
    const member = sampleMember()
    let payload: Record<string, unknown> | undefined
    mockList({ requests: [] })
    mockMembers([member])
    server.use(
      http.post(baseURL, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(sampleDetail({ title: String(payload.title) }), { status: 201 })
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: '新建需求' }))
    await user.type(screen.getByLabelText('标题'), 'Search result exports')
    await user.type(screen.getByLabelText('描述'), 'Enterprise teams need exports.')
    await user.click(screen.getByRole('combobox', { name: '负责人' }))
    await user.click(await screen.findByRole('option', { name: member.email }))
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      title: 'Search result exports',
      description: 'Enterprise teams need exports.',
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_NONE,
      ownerMemberId: member.id,
    })
    expect(payload?.idempotencyKey).toEqual(expect.stringMatching(/^cr_[A-Za-z0-9_-]+$/))
  })

  it('posts selected feedback ids when promoting feedback', async () => {
    let payload: Record<string, unknown> | undefined
    mockList({ requests: [] })
    server.use(
      http.post(`${baseURL}:promote-feedback`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(sampleDetail({ title: String(payload.title) }), { status: 201 })
      }),
    )

    const { user } = renderWithProviders(
      <CustomerRequestsPage initialPromoteFeedbackIDs={['101', '102']} />,
    )

    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByLabelText('反馈 ID')).toHaveValue('101, 102')
    await user.type(within(dialog).getByLabelText('标题'), 'Batch promote request')
    await user.click(within(dialog).getByRole('button', { name: '提升' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      feedbackIds: ['101', '102'],
      title: 'Batch promote request',
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_NONE,
    })
    expect(payload?.idempotencyKey).toEqual(expect.stringMatching(/^cr_[A-Za-z0-9_-]+$/))
  })

  it('filters the list by feedback id when opened from a feedback context', async () => {
    let feedbackID: string | null = null
    server.use(
      http.get(baseURL, ({ request }) => {
        feedbackID = new URL(request.url).searchParams.get('feedback_id')
        return HttpResponse.json({ requests: [sampleSummary()] })
      }),
    )

    renderWithProviders(<CustomerRequestsPage initialFeedbackID="42" />)

    await screen.findByText('Export bundles')
    expect(feedbackID).toBe('42')
  })

  it('filters the list by owner member id from the toolbar', async () => {
    const owner = sampleOwner()
    mockMembers([sampleMember({ id: owner.id, email: owner.email })])
    const urls: string[] = []
    server.use(
      http.get(baseURL, ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({ requests: [sampleSummary({ owner })] })
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await screen.findByText('Export bundles')
    await user.click(screen.getByRole('combobox', { name: '负责人' }))
    await user.click(await screen.findByRole('option', { name: 'ops@example.com' }))

    await waitFor(() =>
      expect(
        urls.some((url) => new URL(url).searchParams.get('owner_member_id') === owner.id),
      ).toBe(true),
    )
  })

  it('updates owner from the detail drawer', async () => {
    const member = sampleMember()
    let payload: Record<string, unknown> | undefined
    mockMembers([member])
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())
    server.use(
      http.patch(`${baseURL}/${requestID}`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(sampleDetail({ owner: ownerFromMember(member) }))
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('combobox', { name: '负责人' }))
    await user.click(await screen.findByRole('option', { name: member.email }))
    await user.click(within(dialog).getByRole('button', { name: '保存更改' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      id: requestID,
      ownerMemberId: member.id,
    })
  })

  it('clears owner from the detail drawer', async () => {
    const owner = sampleOwner()
    let payload: Record<string, unknown> | undefined
    mockList({ requests: [sampleSummary({ owner })] })
    mockDetail(sampleDetail({ owner }))
    server.use(
      http.patch(`${baseURL}/${requestID}`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(sampleDetail())
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('combobox', { name: '负责人' }))
    await user.click(await screen.findByRole('option', { name: '未分配' }))
    await user.click(within(dialog).getByRole('button', { name: '保存更改' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      id: requestID,
      ownerMemberId: '',
    })
  })

  it('links feedback from the detail drawer', async () => {
    let payload: Record<string, unknown> | undefined
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())
    server.use(
      http.post(`${baseURL}/${requestID}/feedback`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(
          sampleDetail({
            supportingFeedbackCount: 3,
            feedback: [
              {
                feedbackId: '42',
                content: 'Need CSV export',
                source: 'web',
                type: 'bug',
                userId: 'user-42',
                subjectDisplay: 'Ada',
                enrichedTitle: 'Need CSV export',
                importance: CustomerRequestImportance.CUSTOMER_REQUEST_IMPORTANCE_NORMAL,
                note: '',
                linkedBy: 'tester',
                linkedAt: '2026-07-07T00:00:00Z',
                createdAt: '2026-07-07T00:00:00Z',
              },
            ],
          }),
        )
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    await user.type(await screen.findByPlaceholderText('反馈 ID'), '42')
    await user.click(screen.getByRole('button', { name: '添加反馈' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      id: requestID,
      feedbackId: '42',
      importance: CustomerRequestImportance.CUSTOMER_REQUEST_IMPORTANCE_NORMAL,
    })
  })

  it('links a subject-only customer without account profile defaults', async () => {
    let payload: Record<string, unknown> | undefined
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())
    server.use(
      http.post(`${baseURL}/${requestID}/customers`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(sampleDetail())
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getAllByPlaceholderText('客户标识')[0], 'customer-1')
    await user.click(within(dialog).getByRole('button', { name: '添加客户' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      id: requestID,
      subjectKey: 'customer-1',
    })
    expect(payload).not.toHaveProperty('accountRevenueCurrency')
    expect(payload).not.toHaveProperty('accountRevenueCents')
  })

  it('switches the detail drawer to the merge target after merging', async () => {
    let payload: Record<string, unknown> | undefined
    const targetDetail = sampleDetail({
      id: targetRequestID,
      displayNumber: '2',
      displayId: 'CR-2',
      title: 'Merged target',
      duplicateRequestCount: 1,
    })
    mockList({
      requests: [
        sampleSummary(),
        sampleSummary({
          id: targetRequestID,
          displayNumber: '2',
          displayId: 'CR-2',
          title: 'Merged target',
        }),
      ],
    })
    mockDetail(sampleDetail())
    server.use(
      http.get(`${baseURL}/${targetRequestID}`, () => HttpResponse.json(targetDetail)),
      http.post(`${baseURL}/${requestID}:merge`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(targetDetail)
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const sourceDialog = await screen.findByRole('dialog')
    await user.type(within(sourceDialog).getByPlaceholderText('目标客户需求 UUID'), targetRequestID)
    await user.click(within(sourceDialog).getByRole('button', { name: '合并' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      sourceId: requestID,
      targetId: targetRequestID,
    })
    expect(await screen.findByRole('dialog', { name: /CR-2.*Merged target/s })).toBeInTheDocument()
  })
})

function mockList(response: ListCustomerRequestsResponse) {
  server.use(http.get(baseURL, () => HttpResponse.json(response)))
}

function mockDetail(detail: CustomerRequestDetail) {
  server.use(http.get(`${baseURL}/${requestID}`, () => HttpResponse.json(detail)))
}

function mockMembers(members: Member[]) {
  server.use(http.get('/fb/v1/console/members', () => HttpResponse.json({ members })))
}

function sampleMember(overrides: Partial<Member> = {}): Member {
  return {
    id: '22222222-2222-2222-2222-222222222222',
    memberType: 'tenant_user',
    userId: 'ops-user',
    email: 'ops@example.com',
    role: 'member',
    roleSource: 'manual',
    invitedAt: '1783382400',
    acceptedAt: '1783382400',
    ...overrides,
  }
}

function sampleOwner(overrides: Partial<CustomerRequestOwner> = {}): CustomerRequestOwner {
  return {
    id: '22222222-2222-2222-2222-222222222222',
    memberType: 'tenant_user',
    userId: 'ops-user',
    email: 'ops@example.com',
    role: 'member',
    ...overrides,
  }
}

function ownerFromMember(member: Member): CustomerRequestOwner {
  return {
    id: member.id,
    memberType: member.memberType,
    userId: member.userId,
    email: member.email,
    role: member.role,
  }
}

function sampleSummary(overrides: Partial<CustomerRequestSummary> = {}): CustomerRequestSummary {
  return {
    id: requestID,
    displayNumber: '1',
    displayId: 'CR-1',
    title: 'Export bundles',
    status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
    priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
    createdAt: '2026-07-07T00:00:00Z',
    updatedAt: '2026-07-07T01:00:00Z',
    supportingFeedbackCount: 2,
    customerCount: 1,
    accountCount: 1,
    linkedIssueCount: 1,
    voteCount: 3,
    duplicateRequestCount: 0,
    hiddenFeedbackCount: 0,
    revenueImpactCents: '2400000',
    revenueCurrency: 'USD',
    decisionScore: 114,
    decisionScoreExplanation:
      'priority=high feedback=2 customers=1 accounts=1 votes=3 revenue_cents=2400000 delivery_health=synced',
    deliveryHealth: CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_SYNCED,
    syncedIssueCount: 1,
    staleIssueCount: 0,
    failedIssueCount: 0,
    pendingIssueCount: 0,
    manualIssueCount: 0,
    latestFeedbackAt: '2026-07-07T00:30:00Z',
    firstFeedbackAt: '2026-07-07T00:00:00Z',
    ...overrides,
  }
}

function sampleDetail(
  overrides: Partial<CustomerRequestDetail> & Partial<CustomerRequestSummary> = {},
): CustomerRequestDetail {
  const request = sampleSummary(overrides)
  return {
    request,
    description: overrides.description ?? 'Bundle feedback exports for enterprise accounts.',
    feedback: overrides.feedback ?? [],
    issueLinks: overrides.issueLinks ?? [],
    auditEntries: overrides.auditEntries ?? [],
    customers: overrides.customers ?? [],
    votes: overrides.votes ?? [],
    duplicates: overrides.duplicates ?? [],
    accountProfiles: overrides.accountProfiles ?? [
      {
        accountKey: 'acme',
        accountDisplay: 'Acme',
        revenueCents: '2400000',
        revenueCurrency: 'USD',
        tier: 'enterprise',
        sizeSegment: 'mid_market',
        lifecycleStatus: 'active',
        crmProvider: 'salesforce',
        crmExternalId: '001',
        source: 'manual',
        updatedAt: '2026-07-07T00:10:00Z',
      },
    ],
  }
}
