import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { CustomerRequestsPage } from '@/features/customer-requests/components/customer-requests-page'
import {
  CustomerRequestAccountEventKind,
  CustomerRequestAccountSignalKind,
  CustomerRequestAccountSignalSeverity,
  type CustomerRequestAccountSummary,
  CustomerRequestDecisionPublicSafeState,
  CustomerRequestDecisionScoreFactorKind,
  type CustomerRequestDeliveryGraph,
  CustomerRequestDeliveryHealth,
  type CustomerRequestDetail,
  CustomerRequestEvidenceConfidence,
  CustomerRequestEvidenceQualityReason,
  CustomerRequestImportance,
  type CustomerRequestIssueLink,
  CustomerRequestIssueSyncState,
  type CustomerRequestNote,
  type CustomerRequestOwner,
  CustomerRequestPriority,
  type CustomerRequestScoringSettings,
  CustomerRequestSort,
  CustomerRequestStatus,
  type CustomerRequestSummary,
  CustomerRequestVisibility,
  type ListCustomerRequestsResponse,
  SortDirection,
} from '@/proto/attune/v1/customer_request'
import type { Member } from '@/proto/attune/v1/member'
import { defaultMe } from '@/testing/mocks/handlers'
import { server } from '@/testing/mocks/server'
import { fireEvent, renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

const requestID = '11111111-1111-1111-1111-111111111111'
const noteID = '44444444-4444-4444-4444-444444444444'
const targetRequestID = '33333333-3333-3333-3333-333333333333'
const baseURL = '/fb/v1/console/customer-requests'

describe('CustomerRequestsPage', () => {
  it('opens the detail drawer from an initial request id deep link', async () => {
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())

    renderWithProviders(<CustomerRequestsPage initialRequestID={requestID} />)

    expect(await screen.findByText('关联反馈')).toBeInTheDocument()
    expect(screen.getByPlaceholderText('反馈 ID')).toBeInTheDocument()
    expect(screen.getAllByText('证据质量 85 · 高').length).toBeGreaterThan(0)
    expect(screen.getByText('多来源证据')).toBeInTheDocument()
  })

  it('shows evidence quality gaps for low-confidence requests', async () => {
    const weak = sampleSummary({
      supportingFeedbackCount: 1,
      customerCount: 1,
      accountCount: 0,
      linkedIssueCount: 0,
      evidenceQuality: {
        score: 15,
        confidence: CustomerRequestEvidenceConfidence.CUSTOMER_REQUEST_EVIDENCE_CONFIDENCE_LOW,
        evidenceCount: 1,
        sourceCount: 1,
        customerCount: 1,
        accountCount: 0,
        latestEvidenceAt: '2026-01-01T00:00:00Z',
        stale: true,
        lowConfidence: true,
        gapReasons: [
          CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_LOW_FEEDBACK_VOLUME,
          CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_SINGLE_CUSTOMER,
          CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_NO_ACCOUNT_CONTEXT,
          CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_STALE_EVIDENCE,
          CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_NO_DELIVERY_LINK,
        ],
        strengths: [],
      },
    })
    mockList({ requests: [weak] })
    mockDetail(sampleDetail(weak))

    renderWithProviders(<CustomerRequestsPage initialRequestID={requestID} />)

    expect(await screen.findByText('证据质量 15 · 低')).toBeInTheDocument()
    expect(screen.getByText('低信心')).toBeInTheDocument()
    expect(screen.getByText('证据过期')).toBeInTheDocument()
    expect(screen.getByText('反馈量不足')).toBeInTheDocument()
    expect(screen.getByText('缺少账户上下文')).toBeInTheDocument()
  })

  it('pre-fills the merge target from an initial deep link', async () => {
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())

    renderWithProviders(
      <CustomerRequestsPage initialRequestID={requestID} initialMergeTargetID={targetRequestID} />,
    )

    expect(await screen.findByPlaceholderText('目标客户需求 UUID')).toHaveValue(targetRequestID)
  })

  it('loads account-scoped customer request deep links', async () => {
    const urls: string[] = []
    const summaryUrls: string[] = []
    const accountRequests = [
      sampleSummary(),
      sampleSummary({
        id: targetRequestID,
        displayId: 'CR-2',
        displayNumber: '2',
        title: 'Renewal blocker',
        supportingFeedbackCount: 4,
        customerCount: 2,
        linkedIssueCount: 2,
        voteCount: 5,
        revenueImpactCents: '1000000',
        decisionScore: 80,
        syncedIssueCount: 0,
        staleIssueCount: 1,
        failedIssueCount: 1,
        pendingIssueCount: 0,
        deliveryHealth: CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_FAILED,
      }),
    ]
    server.use(
      http.get(baseURL, ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({ requests: [accountRequests[0]] })
      }),
      http.get(`${baseURL}/account-summary`, ({ request }) => {
        summaryUrls.push(request.url)
        return HttpResponse.json(sampleAccountSummary({ timeline: accountRequests }))
      }),
      http.get(`${baseURL}/${targetRequestID}`, () =>
        HttpResponse.json(
          sampleDetail({
            id: targetRequestID,
            displayId: 'CR-2',
            displayNumber: '2',
            title: 'Renewal blocker',
          }),
        ),
      ),
      http.get(`${baseURL}/saved-views`, () => HttpResponse.json({ views: [] })),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage initialAccountKey="acct:acme" />)

    await waitFor(() =>
      expect(urls.some((url) => new URL(url).searchParams.get('account_key') === 'acct:acme')).toBe(
        true,
      ),
    )
    await waitFor(() =>
      expect(
        summaryUrls.some(
          (url) =>
            new URL(url).searchParams.get('account_key') === 'acct:acme' &&
            new URL(url).searchParams.get('timeline_limit') === '5',
        ),
      ).toBe(true),
    )
    expect(screen.getByLabelText('账户标识')).toHaveValue('acct:acme')
    const accountOverview = await screen.findByTestId('customer-request-account-overview')
    expect(within(accountOverview).getByText('账户信号概览')).toBeInTheDocument()
    expect(screen.getByText('Acme Corp 需求组合')).toBeInTheDocument()
    expect(screen.getByText('enterprise · mid_market · active · salesforce')).toBeInTheDocument()
    expect(screen.getByText('2 个需求')).toBeInTheDocument()
    expect(screen.getByText('6 条反馈')).toBeInTheDocument()
    expect(screen.getByText('3 位客户')).toBeInTheDocument()
    expect(screen.getByText('8 票')).toBeInTheDocument()
    expect(screen.getByText('3 个交付引用')).toBeInTheDocument()
    expect(screen.getByText('收入影响 $34,000')).toBeInTheDocument()
    expect(screen.getByText('平均决策分 83')).toBeInTheDocument()
    expect(screen.getByText('最高决策分 126')).toBeInTheDocument()
    expect(screen.getByText('1 高优先级')).toBeInTheDocument()
    expect(screen.getByText('0 已发布')).toBeInTheDocument()
    expect(screen.getByText('1 个交付风险')).toBeInTheDocument()
    expect(screen.getByText('1 已同步 · 1 过期 · 1 失败 · 0 待同步 · 0 手动')).toBeInTheDocument()
    expect(screen.getByText('决策信号')).toBeInTheDocument()
    expect(screen.getByText('1 个交付风险需要恢复')).toBeInTheDocument()
    expect(screen.getByText('1 个高优先级需求，最高分 126')).toBeInTheDocument()
    expect(screen.getByText('收入影响 $34,000，平均分 83')).toBeInTheDocument()
    expect(screen.getByText('17 条客户证据，平均分 83')).toBeInTheDocument()
    expect(screen.getByText('账户证据时间线')).toBeInTheDocument()
    expect(screen.getByText('反馈关联')).toBeInTheDocument()
    expect(screen.getByText('Renewal owner reported export failure.')).toBeInTheDocument()
    expect(
      screen.getByText('对象 Ada Lovelace · 来源 portal · 操作人 operator · 反馈 #42'),
    ).toBeInTheDocument()
    expect(screen.getByText('交付同步')).toBeInTheDocument()
    expect(screen.getByText('ATT-236')).toBeInTheDocument()
    expect(screen.getByText('内部备注')).toBeInTheDocument()
    expect(screen.getByText('请求时间线')).toBeInTheDocument()
    expect(within(accountOverview).getAllByText('Renewal blocker').length).toBeGreaterThan(0)

    await user.click(
      within(accountOverview).getByRole('button', { name: /反馈关联.*CR-2.*Renewal blocker/s }),
    )
    expect(
      await screen.findByRole('dialog', { name: /CR-2.*Renewal blocker/s }),
    ).toBeInTheDocument()
  })

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
    expect(screen.getByText('决策分解释')).toBeInTheDocument()
    expect(screen.getByText('计分贡献 114')).toBeInTheDocument()
    expect(screen.getByText('反馈证据')).toBeInTheDocument()
    expect(screen.getByText('2 × 权重 2 · 上限 80')).toBeInTheDocument()
    expect(screen.getByText('收入影响')).toBeInTheDocument()
    expect(screen.getByText('$25,000 · 每 $1,000 计 1 分 · 上限 100')).toBeInTheDocument()
    expect(screen.getByText('交付健康')).toBeInTheDocument()
    expect(screen.getByText('1 个交付引用 · 不计入分数')).toBeInTheDocument()
    expect(screen.getByText('上下文')).toBeInTheDocument()
    expect(screen.getByText('决策记录')).toBeInTheDocument()
    expect(screen.getByText('Updated customer request')).toBeInTheDocument()
    expect(
      screen.getByText((text) => text.includes('pm-1') && text.includes('2026')),
    ).toBeInTheDocument()
    expect(screen.getByText('负责人 owner@example.com')).toBeInTheDocument()
    expect(
      screen.getByText('原因 priority=high feedback=2 customers=1 accounts=1'),
    ).toBeInTheDocument()
    expect(
      screen.getByText(`证据包 customer-request/${requestID}/evidence/CR-1`),
    ).toBeInTheDocument()
    expect(screen.getByText('公开安全 需复核')).toBeInTheDocument()
    expect(screen.getByText('收入上下文')).toBeInTheDocument()
    expect(screen.getByText('状态 Open → Planned')).toBeInTheDocument()
    expect(screen.getByText('优先级 High → Urgent')).toBeInTheDocument()
    expect(screen.getByText('描述变更')).toBeInTheDocument()
    expect(screen.getAllByText('已同步').length).toBeGreaterThan(0)

    await user.click(screen.getByRole('button', { name: 'Close' }))
    await waitFor(() => expect(screen.queryByPlaceholderText('反馈 ID')).not.toBeInTheDocument())
  })

  it('filters the request list from an account profile in the detail drawer', async () => {
    const urls: string[] = []
    server.use(
      http.get(baseURL, ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({ requests: [sampleSummary()] })
      }),
      http.get(`${baseURL}/account-summary`, ({ request }) =>
        HttpResponse.json(
          sampleAccountSummary({
            accountKey: new URL(request.url).searchParams.get('account_key') ?? 'acme',
            accountProfile: {
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
          }),
        ),
      ),
      http.get(`${baseURL}/saved-views`, () => HttpResponse.json({ views: [] })),
    )
    mockDetail(sampleDetail())

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '查看 Acme 的客户需求' }))

    await waitFor(() =>
      expect(urls.some((url) => new URL(url).searchParams.get('account_key') === 'acme')).toBe(
        true,
      ),
    )
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(screen.getByLabelText('账户标识')).toHaveValue('acme')
    expect(await screen.findByText('账户信号概览')).toBeInTheDocument()
    expect(screen.getByText('Acme 需求组合')).toBeInTheDocument()
  })

  it('retries after the list query fails', async () => {
    let calls = 0
    server.use(
      http.get(baseURL, () => {
        calls += 1
        return calls === 1
          ? HttpResponse.json({ message: 'list failed' }, { status: 500 })
          : HttpResponse.json({ requests: [] })
      }),
      http.get(`${baseURL}/saved-views`, () => HttpResponse.json({ views: [] })),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    expect(await screen.findByText('客户需求加载失败')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '重试' }))

    expect(await screen.findByText('还没有客户需求')).toBeInTheDocument()
    expect(calls).toBeGreaterThanOrEqual(2)
  })

  it('opens the promote dialog from the toolbar and ignores invalid feedback ids', async () => {
    let promoted = false
    mockList({ requests: [] })
    server.use(
      http.post(`${baseURL}:promote-feedback`, () => {
        promoted = true
        return HttpResponse.json(sampleDetail(), { status: 201 })
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: '从反馈提升' }))
    const dialog = await screen.findByRole('dialog', { name: '从反馈提升为客户需求' })
    await user.type(within(dialog).getByLabelText('反馈 ID'), 'not-a-number')
    await user.type(within(dialog).getByLabelText('标题'), 'Invalid promote')
    await user.click(within(dialog).getByRole('button', { name: '提升' }))

    expect(promoted).toBe(false)
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
  })

  it('changes toolbar status, priority, visibility, and sort filters', async () => {
    const urls: string[] = []
    server.use(
      http.get(baseURL, ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({ requests: [sampleSummary()] })
      }),
      http.get(`${baseURL}/account-summary`, ({ request }) =>
        HttpResponse.json(
          sampleAccountSummary({
            accountKey: new URL(request.url).searchParams.get('account_key') ?? 'acct:acme',
          }),
        ),
      ),
      http.get(`${baseURL}/saved-views`, () => HttpResponse.json({ views: [] })),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await screen.findByText('Export bundles')
    await user.type(screen.getByLabelText('账户标识'), 'acct:acme')
    await waitFor(() =>
      expect(urls.some((url) => new URL(url).searchParams.get('account_key') === 'acct:acme')).toBe(
        true,
      ),
    )

    const toolbarCombos = () => screen.getAllByRole('combobox').slice(1)

    await user.click(toolbarCombos()[0])
    await user.click(await screen.findByRole('option', { name: 'Planned' }))
    await waitFor(() =>
      expect(
        urls.some(
          (url) =>
            new URL(url).searchParams.get('status') ===
            CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED,
        ),
      ).toBe(true),
    )

    await user.click(toolbarCombos()[1])
    await user.click(await screen.findByRole('option', { name: 'High' }))
    await waitFor(() =>
      expect(
        urls.some(
          (url) =>
            new URL(url).searchParams.get('priority') ===
            CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
        ),
      ).toBe(true),
    )

    await user.click(toolbarCombos()[3])
    await user.click(await screen.findByRole('option', { name: 'All' }))
    await waitFor(() =>
      expect(
        urls.some(
          (url) =>
            new URL(url).searchParams.get('visibility') ===
            CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL,
        ),
      ).toBe(true),
    )

    await user.click(toolbarCombos()[4])
    await user.click(await screen.findByRole('option', { name: '决策分' }))
    await waitFor(() =>
      expect(
        urls.some(
          (url) =>
            new URL(url).searchParams.get('sort') ===
            CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE,
        ),
      ).toBe(true),
    )
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
    const dialog = await screen.findByRole('dialog', { name: '新建客户需求' })
    await user.type(within(dialog).getByLabelText('标题'), 'Search result exports')
    await user.type(within(dialog).getByLabelText('描述'), 'Enterprise teams need exports.')
    await user.click(within(dialog).getAllByRole('combobox')[0])
    await user.click(await screen.findByRole('option', { name: 'High' }))
    await user.click(within(dialog).getByRole('combobox', { name: '负责人' }))
    await user.click(await screen.findByRole('option', { name: member.email }))
    await user.click(within(dialog).getByRole('button', { name: '保存' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      title: 'Search result exports',
      description: 'Enterprise teams need exports.',
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
      ownerMemberId: member.id,
    })
    expect(payload?.idempotencyKey).toEqual(expect.stringMatching(/^cr_[A-Za-z0-9_-]+$/))
  })

  it('keeps the create dialog open when creating a request fails', async () => {
    let attempts = 0
    mockList({ requests: [] })
    server.use(
      http.post(baseURL, () => {
        attempts += 1
        return HttpResponse.json({ message: 'create failed' }, { status: 500 })
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: '新建需求' }))
    await user.type(screen.getByLabelText('标题'), 'Search result exports')
    await user.click(screen.getByRole('button', { name: '保存' }))

    await waitFor(() => expect(attempts).toBe(1))
    expect(screen.getByRole('dialog', { name: '新建客户需求' })).toBeInTheDocument()
  })

  it('updates scoring settings from the settings dialog', async () => {
    let payload: Record<string, unknown> | undefined
    mockList({ requests: [] })
    server.use(
      http.get(`${baseURL}/scoring-settings`, () => HttpResponse.json(sampleScoringSettings())),
      http.put(`${baseURL}/scoring-settings`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(
          sampleScoringSettings({
            feedbackWeight: Number(payload.feedbackWeight),
            revenueCentsPerPoint: String(payload.revenueCentsPerPoint),
            updatedBy: 'tester',
          }),
        )
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: '评分设置' }))
    const dialog = await screen.findByRole('dialog', { name: '评分设置' })
    expect(await within(dialog).findByLabelText('反馈权重')).toHaveValue(2)
    await user.clear(within(dialog).getByLabelText('None'))
    await user.type(within(dialog).getByLabelText('None'), '11')
    await user.clear(within(dialog).getByLabelText('反馈权重'))
    await user.type(within(dialog).getByLabelText('反馈权重'), '9')
    await user.clear(within(dialog).getByLabelText('每分收入金额'))
    await user.type(within(dialog).getByLabelText('每分收入金额'), '250000')
    await user.click(within(dialog).getByRole('button', { name: '保存' }))

    await waitFor(() =>
      expect(payload).toMatchObject({
        priorityNoneWeight: 11,
        feedbackWeight: 9,
        revenueCentsPerPoint: '250000',
      }),
    )
  })

  it('keeps scoring settings open when saving fails', async () => {
    let attempts = 0
    mockList({ requests: [] })
    server.use(
      http.get(`${baseURL}/scoring-settings`, () => HttpResponse.json(sampleScoringSettings())),
      http.put(`${baseURL}/scoring-settings`, () => {
        attempts += 1
        return HttpResponse.json({ message: 'scoring failed' }, { status: 500 })
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: '评分设置' }))
    const dialog = await screen.findByRole('dialog', { name: '评分设置' })
    await user.click(within(dialog).getByRole('button', { name: '保存' }))

    await waitFor(() => expect(attempts).toBe(1))
    expect(screen.getByRole('dialog', { name: '评分设置' })).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    await waitFor(() =>
      expect(screen.queryByRole('dialog', { name: '评分设置' })).not.toBeInTheDocument(),
    )
  })

  it('applies, updates, deletes, and creates saved views', async () => {
    const urls: string[] = []
    const writes: Array<{ method: string; path: string; body?: unknown }> = []
    server.use(
      http.get(baseURL, ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({ requests: [] })
      }),
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
                visibility: CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ALL,
                sort: CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE,
                direction: SortDirection.SORT_DIRECTION_DESC,
                accountKey: 'acct:acme',
              },
              createdAt: '2026-07-08T00:00:00Z',
              updatedAt: '2026-07-08T00:00:00Z',
            },
          ],
        }),
      ),
      http.put(`${baseURL}/saved-views/view-1`, async ({ request }) => {
        writes.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
        })
        return HttpResponse.json({ view: { id: 'view-1', name: 'Scoreboard updated', state: {} } })
      }),
      http.delete(`${baseURL}/saved-views/view-1`, ({ request }) => {
        writes.push({ method: request.method, path: new URL(request.url).pathname })
        return HttpResponse.json({})
      }),
      http.post(`${baseURL}/saved-views`, async ({ request }) => {
        writes.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
        })
        return HttpResponse.json({ view: { id: 'view-created', name: 'New planning', state: {} } })
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    const savedViewSelect = await screen.findByRole('combobox', { name: '保存视图' })
    await waitFor(() => expect(savedViewSelect).not.toBeDisabled())
    await user.click(savedViewSelect)
    await user.click(await screen.findByRole('option', { name: 'Scoreboard' }))

    await waitFor(() => {
      const params = new URL(urls.at(-1) ?? '').searchParams
      expect(params.get('q')).toBe('renewal')
      expect(params.get('sort')).toBe(CustomerRequestSort.CUSTOMER_REQUEST_SORT_DECISION_SCORE)
      expect(params.get('account_key')).toBe('acct:acme')
    })

    await user.click(screen.getByRole('button', { name: '保存视图' }))
    const updateDialog = await screen.findByRole('dialog', { name: '保存客户需求视图' })
    const nameInput = within(updateDialog).getByLabelText('视图名称')
    await user.clear(nameInput)
    await user.type(nameInput, 'Scoreboard updated')
    await user.click(within(updateDialog).getByRole('button', { name: '保存' }))

    await waitFor(() =>
      expect(writes).toContainEqual({
        method: 'PUT',
        path: `${baseURL}/saved-views/view-1`,
        body: expect.objectContaining({
          id: 'view-1',
          name: 'Scoreboard updated',
          state: expect.objectContaining({ accountKey: 'acct:acme' }),
        }),
      }),
    )

    await user.click(screen.getByRole('button', { name: '删除保存视图' }))
    await waitFor(() =>
      expect(writes).toContainEqual({
        method: 'DELETE',
        path: `${baseURL}/saved-views/view-1`,
      }),
    )

    await user.click(screen.getByRole('button', { name: '保存视图' }))
    const createDialog = await screen.findByRole('dialog', { name: '保存客户需求视图' })
    await user.type(within(createDialog).getByLabelText('视图名称'), 'New planning')
    await user.click(within(createDialog).getByRole('button', { name: '保存' }))

    await waitFor(() =>
      expect(writes).toContainEqual({
        method: 'POST',
        path: `${baseURL}/saved-views`,
        body: expect.objectContaining({ name: 'New planning' }),
      }),
    )
  })

  it('handles saved view defaults and mutation errors', async () => {
    const urls: string[] = []
    const writes: Array<{ method: string; path: string; body?: unknown }> = []
    server.use(
      http.get(baseURL, ({ request }) => {
        urls.push(request.url)
        return HttpResponse.json({ requests: [] })
      }),
      http.get(`${baseURL}/saved-views`, () =>
        HttpResponse.json({
          views: [
            {
              id: 'view-defaults',
              name: 'Defaults',
              state: {},
              createdAt: '2026-07-08T00:00:00Z',
              updatedAt: '2026-07-08T00:00:00Z',
            },
          ],
        }),
      ),
      http.put(`${baseURL}/saved-views/view-defaults`, async ({ request }) => {
        writes.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
        })
        return HttpResponse.json({ message: 'save failed' }, { status: 500 })
      }),
      http.delete(`${baseURL}/saved-views/view-defaults`, ({ request }) => {
        writes.push({ method: request.method, path: new URL(request.url).pathname })
        return HttpResponse.json({ message: 'delete failed' }, { status: 500 })
      }),
      http.post(`${baseURL}/saved-views`, async ({ request }) => {
        writes.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
        })
        return HttpResponse.json({ view: { name: 'Defaults copy', state: {} } })
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    const savedViewSelect = await screen.findByRole('combobox', { name: '保存视图' })
    await waitFor(() => expect(savedViewSelect).not.toBeDisabled())
    await user.click(savedViewSelect)
    await user.click(await screen.findByRole('option', { name: 'Defaults' }))

    await waitFor(() => {
      const params = new URL(urls.at(-1) ?? '').searchParams
      expect(params.get('visibility')).toBe(
        CustomerRequestVisibility.CUSTOMER_REQUEST_VISIBILITY_ACTIVE,
      )
      expect(params.get('sort')).toBe(CustomerRequestSort.CUSTOMER_REQUEST_SORT_UPDATED_AT)
      expect(params.get('direction')).toBe(SortDirection.SORT_DIRECTION_DESC)
      expect(params.has('q')).toBe(false)
    })

    await user.click(screen.getByRole('button', { name: '保存视图' }))
    const updateDialog = await screen.findByRole('dialog', { name: '保存客户需求视图' })
    await user.click(within(updateDialog).getByRole('button', { name: '保存' }))
    await waitFor(() =>
      expect(writes).toContainEqual({
        method: 'PUT',
        path: `${baseURL}/saved-views/view-defaults`,
        body: expect.objectContaining({ id: 'view-defaults', name: 'Defaults' }),
      }),
    )

    await user.click(within(updateDialog).getByRole('button', { name: '取消' }))
    await user.click(screen.getByRole('button', { name: '删除保存视图' }))
    await waitFor(() =>
      expect(writes).toContainEqual({
        method: 'DELETE',
        path: `${baseURL}/saved-views/view-defaults`,
      }),
    )

    await user.click(savedViewSelect)
    await user.click(await screen.findByRole('option', { name: '当前筛选' }))
    await user.click(screen.getByRole('button', { name: '保存视图' }))
    const createDialog = await screen.findByRole('dialog', { name: '保存客户需求视图' })
    await user.type(within(createDialog).getByLabelText('视图名称'), 'Defaults copy')
    await user.click(within(createDialog).getByRole('button', { name: '保存' }))

    await waitFor(() =>
      expect(writes).toContainEqual({
        method: 'POST',
        path: `${baseURL}/saved-views`,
        body: expect.objectContaining({ name: 'Defaults copy' }),
      }),
    )
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

  it('keeps the promote dialog open when promoting feedback fails', async () => {
    let attempts = 0
    mockList({ requests: [] })
    server.use(
      http.post(`${baseURL}:promote-feedback`, () => {
        attempts += 1
        return HttpResponse.json({ message: 'promote failed' }, { status: 500 })
      }),
    )

    const { user } = renderWithProviders(
      <CustomerRequestsPage initialPromoteFeedbackIDs={['101']} />,
    )

    const dialog = await screen.findByRole('dialog', { name: '从反馈提升为客户需求' })
    await user.type(within(dialog).getByLabelText('标题'), 'Failed promotion')
    await user.click(within(dialog).getByRole('button', { name: '提升' }))

    await waitFor(() => expect(attempts).toBe(1))
    expect(screen.getByRole('dialog', { name: '从反馈提升为客户需求' })).toBeInTheDocument()
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

  it('renders status, priority, delivery health variants and loads another page', async () => {
    const urls: string[] = []
    server.use(
      http.get(baseURL, ({ request }) => {
        urls.push(request.url)
        const cursor = new URL(request.url).searchParams.get('cursor')
        return HttpResponse.json(
          cursor
            ? {
                requests: [
                  sampleSummary({
                    id: 'request-manual',
                    displayNumber: '6',
                    displayId: 'CR-6',
                    title: 'Manual handoff',
                    status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_CANCELLED,
                    priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_NONE,
                    deliveryHealth:
                      CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_MANUAL,
                  }),
                ],
              }
            : {
                requests: [
                  sampleSummary({
                    id: 'request-planned',
                    displayNumber: '2',
                    displayId: 'CR-2',
                    title: 'Planned rollout',
                    status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED,
                    priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_LOW,
                    deliveryHealth:
                      CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_FAILED,
                  }),
                  sampleSummary({
                    id: 'request-progress',
                    displayNumber: '3',
                    displayId: 'CR-3',
                    title: 'In progress sync',
                    status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_IN_PROGRESS,
                    priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_MEDIUM,
                    deliveryHealth:
                      CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_STALE,
                  }),
                  sampleSummary({
                    id: 'request-shipped',
                    displayNumber: '4',
                    displayId: 'CR-4',
                    title: 'Shipped handoff',
                    status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_SHIPPED,
                    priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT,
                    deliveryHealth:
                      CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_PENDING,
                  }),
                  sampleSummary({
                    id: 'request-open',
                    displayNumber: '5',
                    displayId: 'CR-5',
                    title: 'No links yet',
                    deliveryHealth:
                      CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_NO_LINKS,
                  }),
                ],
                nextCursor: 'next-page',
              },
        )
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await screen.findByText('Planned rollout')
    expect(screen.getByText('In progress sync')).toBeInTheDocument()
    expect(screen.getByText('Shipped handoff')).toBeInTheDocument()
    expect(screen.getByText('No links yet')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /加载中/ }))

    await screen.findByText('Manual handoff')
    await user.type(screen.getByLabelText('搜索标题或 CR 编号'), 'rollout')
    await waitFor(() =>
      expect(urls.some((url) => new URL(url).searchParams.get('q') === 'rollout')).toBe(true),
    )
    expect(urls.some((url) => new URL(url).searchParams.get('cursor') === 'next-page')).toBe(true)
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
    await user.click(within(dialog).getByRole('combobox', { name: '优先级' }))
    await user.click(await screen.findByRole('option', { name: 'Urgent' }))
    await user.click(within(dialog).getByRole('button', { name: '保存更改' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      id: requestID,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT,
      ownerMemberId: member.id,
    })
  })

  it('keeps detail status edits available when update fails', async () => {
    let attempts = 0
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())
    server.use(
      http.patch(`${baseURL}/${requestID}`, () => {
        attempts += 1
        return HttpResponse.json({ message: 'update failed' }, { status: 500 })
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('combobox', { name: '状态' }))
    await user.click(await screen.findByRole('option', { name: 'Planned' }))
    await user.click(within(dialog).getByRole('button', { name: '保存更改' }))

    await waitFor(() => expect(attempts).toBe(1))
    expect(within(dialog).getByRole('button', { name: '保存更改' })).toBeInTheDocument()
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

  it('links a customer with account profile fields', async () => {
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
    await user.type(within(dialog).getAllByPlaceholderText('客户标识')[0], 'buyer-1')
    await user.type(within(dialog).getAllByPlaceholderText('客户名称')[0], 'Buyer One')
    await user.type(within(dialog).getAllByPlaceholderText('账户标识')[0], 'acme')
    await user.type(within(dialog).getAllByPlaceholderText('账户名称')[0], 'Acme')
    await user.type(within(dialog).getAllByPlaceholderText('收入分')[0], '12345')
    await user.clear(within(dialog).getAllByPlaceholderText('币种')[0])
    await user.type(within(dialog).getAllByPlaceholderText('币种')[0], 'eur')
    await user.type(within(dialog).getAllByPlaceholderText('层级')[0], 'enterprise')
    await user.type(within(dialog).getAllByPlaceholderText('规模')[0], 'mid')
    await user.type(within(dialog).getAllByPlaceholderText('生命周期')[0], 'active')
    await user.click(within(dialog).getByRole('button', { name: '添加客户' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      id: requestID,
      subjectKey: 'buyer-1',
      subjectDisplay: 'Buyer One',
      accountKey: 'acme',
      accountDisplay: 'Acme',
      accountRevenueCents: '12345',
      accountRevenueCurrency: 'EUR',
      accountTier: 'enterprise',
      accountSizeSegment: 'mid',
      accountLifecycleStatus: 'active',
    })
  })

  it('adds a vote with account profile fields', async () => {
    let payload: Record<string, unknown> | undefined
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())
    server.use(
      http.post(`${baseURL}/${requestID}/votes`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(sampleDetail())
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getAllByPlaceholderText('客户标识')[1], 'voter-1')
    await user.type(within(dialog).getAllByPlaceholderText('客户名称')[1], 'Vote Owner')
    await user.type(within(dialog).getAllByPlaceholderText('账户标识')[1], 'globex')
    await user.type(within(dialog).getAllByPlaceholderText('账户名称')[1], 'Globex')
    await user.clear(within(dialog).getByPlaceholderText('权重'))
    await user.type(within(dialog).getByPlaceholderText('权重'), '7')
    await user.type(within(dialog).getAllByPlaceholderText('收入分')[1], '98765')
    await user.clear(within(dialog).getAllByPlaceholderText('币种')[1])
    await user.type(within(dialog).getAllByPlaceholderText('币种')[1], 'gbp')
    await user.type(within(dialog).getAllByPlaceholderText('层级')[1], 'strategic')
    await user.type(within(dialog).getAllByPlaceholderText('规模')[1], 'enterprise')
    await user.type(within(dialog).getAllByPlaceholderText('生命周期')[1], 'renewal')
    await user.click(within(dialog).getByRole('button', { name: '添加投票' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      id: requestID,
      subjectKey: 'voter-1',
      subjectDisplay: 'Vote Owner',
      accountKey: 'globex',
      accountDisplay: 'Globex',
      weight: 7,
      accountRevenueCents: '98765',
      accountRevenueCurrency: 'GBP',
      accountTier: 'strategic',
      accountSizeSegment: 'enterprise',
      accountLifecycleStatus: 'renewal',
    })
  })

  it('adds an internal note from the detail drawer', async () => {
    let payload: Record<string, unknown> | undefined
    let detailResponse = sampleDetail()
    mockList({ requests: [sampleSummary()] })
    server.use(
      http.get(`${baseURL}/${requestID}`, () => HttpResponse.json(detailResponse)),
      http.post(`${baseURL}/${requestID}/notes`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        detailResponse = sampleDetail({
          notes: [sampleNote({ body: String(payload.body) })],
        })
        return HttpResponse.json(detailResponse)
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    await user.type(within(dialog).getByLabelText('备注内容'), 'Prioritize after ACME review.')
    await user.click(within(dialog).getByRole('button', { name: '添加备注' }))

    await waitFor(() => expect(payload).toBeDefined())
    expect(payload).toMatchObject({
      id: requestID,
      body: 'Prioritize after ACME review.',
    })
    expect(await within(dialog).findByText('Prioritize after ACME review.')).toBeInTheDocument()
  })

  it('ignores empty note submissions from the detail drawer', async () => {
    let posted = false
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())
    server.use(
      http.post(`${baseURL}/${requestID}/notes`, () => {
        posted = true
        return HttpResponse.json(sampleDetail())
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    const form = within(dialog).getByLabelText('备注内容').closest('form') as HTMLFormElement

    fireEvent.submit(form)

    await waitFor(() => expect(posted).toBe(false))
  })

  it('deletes an internal note from the detail drawer', async () => {
    let deleted = false
    let detailResponse = sampleDetail({ notes: [sampleNote()] })
    mockList({ requests: [sampleSummary()] })
    server.use(
      http.get(`${baseURL}/${requestID}`, () => HttpResponse.json(detailResponse)),
      http.delete(`${baseURL}/${requestID}/notes/${noteID}`, () => {
        deleted = true
        detailResponse = sampleDetail({ notes: [] })
        return HttpResponse.json(detailResponse)
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText('Prioritize after ACME review.')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: '删除备注' }))

    await waitFor(() => expect(deleted).toBe(true))
    await waitFor(() =>
      expect(within(dialog).queryByText('Prioritize after ACME review.')).not.toBeInTheDocument(),
    )
  })

  it('links an external issue and clears the issue URL field', async () => {
    let payload: Record<string, unknown> | undefined
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())
    server.use(
      http.post(`${baseURL}/${requestID}/issue-links`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(sampleDetail())
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    await user.type(
      within(dialog).getByPlaceholderText('Issue URL'),
      'https://github.com/Phixsura/attune/issues/224',
    )
    await user.click(within(dialog).getByRole('button', { name: '添加引用' }))

    await waitFor(() =>
      expect(payload).toMatchObject({
        id: requestID,
        provider: 'github',
        externalUrl: 'https://github.com/Phixsura/attune/issues/224',
      }),
    )
    await waitFor(() => expect(within(dialog).getByPlaceholderText('Issue URL')).toHaveValue(''))
  })

  it('links a GitHub issue by managed connection and issue number', async () => {
    let payload: Record<string, unknown> | undefined
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())
    server.use(
      http.post(`${baseURL}/${requestID}/issue-links`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(sampleDetail())
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText('GitHub Managed')).toBeInTheDocument()
    await user.type(within(dialog).getByPlaceholderText('Issue 编号'), '224')
    await user.click(within(dialog).getByRole('button', { name: '添加引用' }))

    await waitFor(() =>
      expect(payload).toMatchObject({
        id: requestID,
        provider: 'github',
        externalUrl: '',
        connectionId: 'connection-github',
        issueNumber: '224',
      }),
    )
    await waitFor(() => expect(within(dialog).getByPlaceholderText('Issue 编号')).toHaveValue(''))
  })

  it('queues GitHub issue creation from the detail drawer', async () => {
    let payload: Record<string, unknown> | undefined
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())
    server.use(
      http.post(`${baseURL}/${requestID}/issue-links:create-github`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({
          detail: sampleDetail(),
          runId: 'run-1',
          connectionId: 'connection-1',
          mappingId: 'mapping-1',
        })
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    const createButton = within(dialog).getByRole('button', { name: '创建 GitHub Issue' })
    await waitFor(() => expect(createButton).toBeEnabled())
    await user.click(createButton)

    await waitFor(() =>
      expect(payload).toEqual({ id: requestID, connectionId: 'connection-github' }),
    )
  })

  it('disables GitHub issue creation for pull-only managed mappings', async () => {
    let called = false
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail(), { mappingDirection: 'EXTERNAL_SYNC_DIRECTION_PULL' })
    server.use(
      http.post(`${baseURL}/${requestID}/issue-links:create-github`, () => {
        called = true
        return HttpResponse.json({ message: 'unexpected create' }, { status: 500 })
      }),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    const createButton = await within(dialog).findByRole('button', {
      name: '创建 GitHub Issue',
    })

    await waitFor(() => expect(createButton).toBeDisabled())
    expect(called).toBe(false)
  })

  it('hides GitHub issue creation when a GitHub issue is already linked', async () => {
    const detail = sampleDetail({ issueLinks: [sampleGitHubIssueLink()] })
    mockList({ requests: [detail.request ?? sampleSummary()] })
    mockDetail(detail)

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')

    expect((await within(dialog).findAllByText('GitHub #212')).length).toBeGreaterThan(0)
    expect(
      within(dialog).queryByRole('button', { name: '创建 GitHub Issue' }),
    ).not.toBeInTheDocument()
  })

  it('hides issue linking controls for read-only users', async () => {
    server.use(
      http.get('/fb/v1/console/me', () =>
        HttpResponse.json({
          ...defaultMe,
          user: { ...defaultMe.user, role: 'viewer' },
        }),
      ),
    )
    mockList({ requests: [sampleSummary()] })
    mockDetail(sampleDetail())

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText('关联反馈')).toBeInTheDocument()

    await waitFor(() =>
      expect(within(dialog).queryByPlaceholderText('Issue URL')).not.toBeInTheDocument(),
    )
    expect(
      within(dialog).queryByRole('button', { name: '创建 GitHub Issue' }),
    ).not.toBeInTheDocument()
  })

  it('renders populated detail sections and removes linked records', async () => {
    const detail = sampleDetail({
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT,
      deliveryHealth: CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_FAILED,
      feedback: [
        {
          feedbackId: '42',
          content: 'Raw export request',
          source: 'web',
          type: 'feature',
          userId: 'user-42',
          subjectDisplay: 'Ada',
          enrichedTitle: 'Need enterprise CSV export',
          importance: CustomerRequestImportance.CUSTOMER_REQUEST_IMPORTANCE_CRITICAL,
          note: 'from renewal call',
          linkedBy: 'tester',
          linkedAt: '2026-07-07T00:05:00Z',
          createdAt: '2026-07-07T00:00:00Z',
        },
      ],
      customers: [
        {
          id: 'customer-link-1',
          subjectKey: 'subject-1',
          subjectHash: '',
          subjectDisplay: 'Acme buyer',
          accountKey: 'acme',
          accountDisplay: 'Acme',
          note: 'renewal blocker',
          createdBy: 'tester',
          createdAt: '2026-07-07T00:10:00Z',
        },
      ],
      votes: [
        {
          id: 'vote-1',
          subjectKey: 'subject-1',
          subjectHash: '',
          subjectDisplay: 'Vote champion',
          accountKey: 'acme',
          accountDisplay: 'Acme',
          weight: 4,
          note: 'exec sponsor',
          createdBy: 'tester',
          createdAt: '2026-07-07T00:20:00Z',
        },
      ],
      issueLinks: [
        {
          id: 'issue-link-1',
          provider: 'github',
          externalKey: '212',
          externalUrl: 'https://github.com/Phixsura/attune/issues/212',
          title: 'GitHub #212',
          status: 'open',
          createdBy: 'tester',
          createdAt: '2026-07-07T00:25:00Z',
          updatedAt: '2026-07-07T00:25:00Z',
          lastSyncedAt: '2026-07-07T00:30:00Z',
          syncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_FAILED,
          externalStatusCategory: 'in_progress',
          externalAssignee: 'ops@example.com',
          externalUpdatedAt: '2026-07-07T00:30:00Z',
          syncError: 'rate limited',
        },
      ],
      duplicates: [
        {
          id: 'duplicate-1',
          displayId: 'CR-9',
          title: 'Older duplicate',
          mergedAt: '2026-07-07T00:35:00Z',
        },
      ],
      auditEntries: [
        {
          id: 'audit-1',
          action: 'created',
          actorType: 'user',
          actorId: 'tester',
          summary: 'Request created',
          createdAt: '2026-07-07T00:40:00Z',
        },
      ],
      notes: [sampleNote()],
      accountProfiles: [
        {
          accountKey: 'acme',
          accountDisplay: 'Acme Corp',
          revenueCents: '1250000',
          revenueCurrency: 'USD',
          tier: 'enterprise',
          sizeSegment: 'mid_market',
          lifecycleStatus: 'active',
          crmProvider: 'salesforce',
          crmExternalId: '001',
          source: 'manual',
          updatedAt: '2026-07-07T00:45:00Z',
        },
      ],
    })
    const deletes: string[] = []
    let syncPayload: Record<string, unknown> | undefined
    mockList({ requests: [detail.request ?? sampleSummary()] })
    mockDetail(detail)
    server.use(
      http.delete(`${baseURL}/${requestID}/feedback/42`, () => {
        deletes.push('feedback')
        return HttpResponse.json(detail)
      }),
      http.delete(`${baseURL}/${requestID}/customers/customer-link-1`, () => {
        deletes.push('customer')
        return HttpResponse.json(detail)
      }),
      http.delete(`${baseURL}/${requestID}/votes/vote-1`, () => {
        deletes.push('vote')
        return HttpResponse.json(detail)
      }),
      http.delete(`${baseURL}/${requestID}/issue-links/issue-link-1`, () => {
        deletes.push('issue')
        return HttpResponse.json(detail)
      }),
      http.post(
        `${baseURL}/${requestID}/issue-links/issue-link-1:record-sync`,
        async ({ request }) => {
          syncPayload = (await request.json()) as Record<string, unknown>
          return HttpResponse.json(detail)
        },
      ),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')
    expect(await within(dialog).findByText('Need enterprise CSV export')).toBeInTheDocument()
    expect(within(dialog).getByText('Acme buyer')).toBeInTheDocument()
    expect(within(dialog).getByText('Vote champion')).toBeInTheDocument()
    expect(within(dialog).getByText('交付图谱')).toBeInTheDocument()
    expect(within(dialog).getByText('2 个交付节点 · 1 条关系')).toBeInTheDocument()
    expect(within(dialog).getByText('1 linked artifacts: 1 failed.')).toBeInTheDocument()
    expect(within(dialog).getAllByText('GitHub #212')).toHaveLength(2)
    expect(within(dialog).getAllByText('rate limited')).toHaveLength(2)
    expect(within(dialog).getByText('Acme Corp')).toBeInTheDocument()
    expect(within(dialog).getByText('CR-9')).toBeInTheDocument()
    expect(within(dialog).getByText('Request created')).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: '记录同步' }))
    await waitFor(() => expect(syncPayload).toBeDefined())
    expect(syncPayload).toMatchObject({
      id: requestID,
      issueLinkId: 'issue-link-1',
      syncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
      status: 'open',
    })

    await user.click(within(dialog).getByRole('button', { name: '移除引用' }))
    await user.click(within(dialog).getByRole('button', { name: '移除投票' }))
    await user.click(within(dialog).getByRole('button', { name: '移除客户' }))
    await user.click(within(dialog).getByRole('button', { name: '移除反馈' }))

    await waitFor(() => expect(deletes).toEqual(['issue', 'vote', 'customer', 'feedback']))
  })

  it('keeps detail actions available when mutations fail', async () => {
    const detail = sampleDetail({
      feedback: [
        {
          feedbackId: '42',
          content: 'Raw export request',
          source: 'web',
          type: 'feature',
          userId: 'user-42',
          subjectDisplay: 'Ada',
          enrichedTitle: 'Need enterprise CSV export',
          importance: CustomerRequestImportance.CUSTOMER_REQUEST_IMPORTANCE_CRITICAL,
          note: '',
          linkedBy: 'tester',
          linkedAt: '2026-07-07T00:05:00Z',
          createdAt: '2026-07-07T00:00:00Z',
        },
      ],
      customers: [
        {
          id: 'customer-link-1',
          subjectKey: 'subject-1',
          subjectHash: '',
          subjectDisplay: 'Acme buyer',
          accountKey: 'acme',
          accountDisplay: 'Acme',
          note: '',
          createdBy: 'tester',
          createdAt: '2026-07-07T00:10:00Z',
        },
      ],
      votes: [
        {
          id: 'vote-1',
          subjectKey: 'subject-1',
          subjectHash: '',
          subjectDisplay: 'Vote champion',
          accountKey: 'acme',
          accountDisplay: 'Acme',
          weight: 4,
          note: '',
          createdBy: 'tester',
          createdAt: '2026-07-07T00:20:00Z',
        },
      ],
      notes: [sampleNote()],
      issueLinks: [
        {
          id: 'issue-link-1',
          provider: 'jira',
          externalKey: 'ATT-212',
          externalUrl: 'https://example.atlassian.net/browse/ATT-212',
          title: 'Jira ATT-212',
          status: 'open',
          createdBy: 'tester',
          createdAt: '2026-07-07T00:25:00Z',
          updatedAt: '2026-07-07T00:25:00Z',
          lastSyncedAt: '2026-07-07T00:30:00Z',
          syncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_STALE,
          externalStatusCategory: 'in_progress',
          externalAssignee: 'ops@example.com',
          externalUpdatedAt: '2026-07-07T00:30:00Z',
          syncError: '',
        },
      ],
    })
    const failures: string[] = []
    const fail = (name: string) => {
      failures.push(name)
      return HttpResponse.json({ code: 'INTERNAL', message: `${name} failed` }, { status: 500 })
    }
    mockList({ requests: [detail.request ?? sampleSummary()] })
    mockDetail(detail)
    server.use(
      http.post(`${baseURL}/${requestID}:merge`, () => fail('merge')),
      http.post(`${baseURL}/${requestID}/feedback`, () => fail('feedback')),
      http.post(`${baseURL}/${requestID}/customers`, () => fail('customer')),
      http.post(`${baseURL}/${requestID}/votes`, () => fail('vote')),
      http.post(`${baseURL}/${requestID}/notes`, () => fail('note')),
      http.post(`${baseURL}/${requestID}/issue-links`, () => fail('issue-link')),
      http.post(`${baseURL}/${requestID}/issue-links:create-github`, () =>
        fail('create-github-issue'),
      ),
      http.post(`${baseURL}/${requestID}/issue-links/issue-link-1:record-sync`, () =>
        fail('record-sync'),
      ),
      http.delete(`${baseURL}/${requestID}/feedback/42`, () => fail('unlink-feedback')),
      http.delete(`${baseURL}/${requestID}/customers/customer-link-1`, () =>
        fail('unlink-customer'),
      ),
      http.delete(`${baseURL}/${requestID}/votes/vote-1`, () => fail('remove-vote')),
      http.delete(`${baseURL}/${requestID}/notes/${noteID}`, () => fail('delete-note')),
      http.delete(`${baseURL}/${requestID}/issue-links/issue-link-1`, () => fail('unlink-issue')),
    )

    const { user } = renderWithProviders(<CustomerRequestsPage />)

    await user.click(await screen.findByRole('button', { name: /CR-1.*Export bundles/s }))
    const dialog = await screen.findByRole('dialog')

    await user.type(within(dialog).getByPlaceholderText('目标客户需求 UUID'), targetRequestID)
    await user.click(within(dialog).getByRole('button', { name: '合并' }))
    await waitFor(() => expect(failures).toContain('merge'))

    await user.type(within(dialog).getByPlaceholderText('反馈 ID'), '42')
    await user.click(within(dialog).getByRole('button', { name: '添加反馈' }))
    await waitFor(() => expect(failures).toContain('feedback'))

    await user.type(within(dialog).getAllByPlaceholderText('客户标识')[0], 'buyer-1')
    await user.click(within(dialog).getByRole('button', { name: '添加客户' }))
    await waitFor(() => expect(failures).toContain('customer'))

    await user.type(within(dialog).getAllByPlaceholderText('客户标识')[1], 'voter-1')
    await user.click(within(dialog).getByRole('button', { name: '添加投票' }))
    await waitFor(() => expect(failures).toContain('vote'))

    await user.type(within(dialog).getByLabelText('备注内容'), 'failed note')
    await user.click(within(dialog).getByRole('button', { name: '添加备注' }))
    await waitFor(() => expect(failures).toContain('note'))

    await user.type(
      within(dialog).getByPlaceholderText('Issue URL'),
      'https://github.com/Phixsura/attune/issues/212',
    )
    await user.click(within(dialog).getByRole('button', { name: '添加引用' }))
    await waitFor(() => expect(failures).toContain('issue-link'))

    await user.click(within(dialog).getByRole('button', { name: '创建 GitHub Issue' }))
    await waitFor(() => expect(failures).toContain('create-github-issue'))

    await user.click(within(dialog).getByRole('button', { name: '记录同步' }))
    await waitFor(() => expect(failures).toContain('record-sync'))
    await user.click(within(dialog).getByRole('button', { name: '移除引用' }))
    await waitFor(() => expect(failures).toContain('unlink-issue'))
    await user.click(within(dialog).getByRole('button', { name: '删除备注' }))
    await waitFor(() => expect(failures).toContain('delete-note'))
    await user.click(within(dialog).getByRole('button', { name: '移除投票' }))
    await waitFor(() => expect(failures).toContain('remove-vote'))
    await user.click(within(dialog).getByRole('button', { name: '移除客户' }))
    await waitFor(() => expect(failures).toContain('unlink-customer'))
    await user.click(within(dialog).getByRole('button', { name: '移除反馈' }))
    await waitFor(() => expect(failures).toContain('unlink-feedback'))
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
  server.use(
    http.get(baseURL, () => HttpResponse.json(response)),
    http.get(`${baseURL}/saved-views`, () => HttpResponse.json({ views: [] })),
  )
}

function mockDetail(detail: CustomerRequestDetail, options: { mappingDirection?: string } = {}) {
  server.use(
    http.get(`${baseURL}/${requestID}`, () => HttpResponse.json(detail)),
    http.get('/fb/v1/console/external-sync/connections', () =>
      HttpResponse.json({
        connections: [
          {
            id: 'connection-github',
            tenantId: 't-1',
            provider: 'github',
            name: 'GitHub Managed',
            enabled: true,
            status: 'active',
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
          },
        ],
      }),
    ),
    http.get('/fb/v1/console/external-sync/mappings', () =>
      HttpResponse.json({
        mappings: [
          {
            id: 'mapping-github',
            tenantId: 't-1',
            connectionId: 'connection-github',
            localObjectType: 'customer_request',
            externalObjectType: 'issue',
            direction: options.mappingDirection ?? 'EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL',
            fieldMappingJson: '{}',
            statusMappingJson: '{}',
            conflictPolicy: 'manual',
            tombstonePolicy: 'mark_stale',
            enabled: true,
            mappingVersion: 1,
            createdAt: '2026-07-07T00:00:00Z',
            updatedAt: '2026-07-07T00:00:00Z',
          },
        ],
      }),
    ),
  )
}

function mockMembers(members: Member[]) {
  server.use(http.get('/fb/v1/console/members', () => HttpResponse.json({ members })))
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

function sampleGitHubIssueLink(
  overrides: Partial<CustomerRequestIssueLink> = {},
): CustomerRequestIssueLink {
  return {
    id: 'issue-link-1',
    provider: 'github',
    externalKey: '212',
    externalUrl: 'https://github.com/Phixsura/attune/issues/212',
    title: 'GitHub #212',
    status: 'open',
    createdBy: 'tester',
    createdAt: '2026-07-07T00:25:00Z',
    updatedAt: '2026-07-07T00:25:00Z',
    lastSyncedAt: '2026-07-07T00:30:00Z',
    syncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
    externalStatusCategory: 'in_progress',
    externalAssignee: 'ops@example.com',
    externalUpdatedAt: '2026-07-07T00:30:00Z',
    syncError: '',
    ...overrides,
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
    revenueImpactCents: '2500000',
    revenueCurrency: 'USD',
    decisionScore: 114,
    decisionScoreExplanation:
      'priority=high feedback=2 customers=1 accounts=1 votes=3 revenue_cents=2500000 delivery_health=synced',
    decisionScoreFactors: [
      {
        kind: CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_PRIORITY,
        rawCount: 1,
        rawValueCents: '0',
        weight: 60,
        cap: 0,
        unitCents: '0',
        contribution: 60,
        capped: false,
        contributesToScore: true,
      },
      {
        kind: CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_FEEDBACK,
        rawCount: 2,
        rawValueCents: '0',
        weight: 2,
        cap: 80,
        unitCents: '0',
        contribution: 4,
        capped: false,
        contributesToScore: true,
      },
      {
        kind: CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_CUSTOMERS,
        rawCount: 1,
        rawValueCents: '0',
        weight: 5,
        cap: 100,
        unitCents: '0',
        contribution: 5,
        capped: false,
        contributesToScore: true,
      },
      {
        kind: CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_ACCOUNTS,
        rawCount: 1,
        rawValueCents: '0',
        weight: 8,
        cap: 120,
        unitCents: '0',
        contribution: 8,
        capped: false,
        contributesToScore: true,
      },
      {
        kind: CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_VOTES,
        rawCount: 3,
        rawValueCents: '0',
        weight: 4,
        cap: 80,
        unitCents: '0',
        contribution: 12,
        capped: false,
        contributesToScore: true,
      },
      {
        kind: CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_REVENUE,
        rawCount: 0,
        rawValueCents: '2500000',
        weight: 0,
        cap: 100,
        unitCents: '100000',
        contribution: 25,
        capped: false,
        contributesToScore: true,
      },
      {
        kind: CustomerRequestDecisionScoreFactorKind.CUSTOMER_REQUEST_DECISION_SCORE_FACTOR_KIND_DELIVERY_HEALTH,
        rawCount: 1,
        rawValueCents: '0',
        weight: 0,
        cap: 0,
        unitCents: '0',
        contribution: 0,
        capped: false,
        contributesToScore: false,
      },
    ],
    evidenceQuality: {
      score: 85,
      confidence: CustomerRequestEvidenceConfidence.CUSTOMER_REQUEST_EVIDENCE_CONFIDENCE_HIGH,
      evidenceCount: 3,
      sourceCount: 2,
      customerCount: 3,
      accountCount: 1,
      latestEvidenceAt: '2026-07-07T00:30:00Z',
      stale: false,
      lowConfidence: false,
      gapReasons: [],
      strengths: [
        CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_SUPPORTING_FEEDBACK,
        CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_MULTI_SOURCE,
        CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_MULTI_CUSTOMER,
        CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_ACCOUNT_CONTEXT,
        CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_FRESH_EVIDENCE,
        CustomerRequestEvidenceQualityReason.CUSTOMER_REQUEST_EVIDENCE_QUALITY_REASON_DELIVERY_LINKED,
      ],
    },
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

function sampleAccountSummary(
  overrides: Partial<CustomerRequestAccountSummary> = {},
): CustomerRequestAccountSummary {
  return {
    accountKey: 'acct:acme',
    accountProfile: {
      accountKey: 'acct:acme',
      accountDisplay: 'Acme Corp',
      revenueCents: '2400000',
      revenueCurrency: 'USD',
      tier: 'enterprise',
      sizeSegment: 'mid_market',
      lifecycleStatus: 'active',
      crmProvider: 'salesforce',
      crmExternalId: '001-acme',
      source: 'manual',
      updatedAt: '2026-07-07T00:10:00Z',
    },
    requestCount: 2,
    feedbackCount: 6,
    customerCount: 3,
    voteCount: 8,
    issueCount: 3,
    syncedIssueCount: 1,
    staleIssueCount: 1,
    failedIssueCount: 1,
    pendingIssueCount: 0,
    manualIssueCount: 0,
    revenueImpactCents: '3400000',
    revenueCurrency: 'USD',
    highPriorityRequestCount: 1,
    shippedRequestCount: 0,
    staleOrFailedIssueCount: 1,
    averageDecisionScore: 83,
    topDecisionScore: 126,
    decisionSignals: [
      {
        kind: CustomerRequestAccountSignalKind.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_KIND_DELIVERY_RISK,
        severity:
          CustomerRequestAccountSignalSeverity.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_SEVERITY_CRITICAL,
        count: 1,
        valueCents: '0',
        score: 1,
      },
      {
        kind: CustomerRequestAccountSignalKind.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_KIND_HIGH_PRIORITY_DEMAND,
        severity:
          CustomerRequestAccountSignalSeverity.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_SEVERITY_WARNING,
        count: 1,
        valueCents: '0',
        score: 126,
      },
      {
        kind: CustomerRequestAccountSignalKind.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_KIND_REVENUE_IMPACT,
        severity:
          CustomerRequestAccountSignalSeverity.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_SEVERITY_INFO,
        count: 2,
        valueCents: '3400000',
        score: 83,
      },
      {
        kind: CustomerRequestAccountSignalKind.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_KIND_EVIDENCE_BREADTH,
        severity:
          CustomerRequestAccountSignalSeverity.CUSTOMER_REQUEST_ACCOUNT_SIGNAL_SEVERITY_INFO,
        count: 17,
        valueCents: '0',
        score: 83,
      },
    ],
    events: [
      {
        kind: CustomerRequestAccountEventKind.CUSTOMER_REQUEST_ACCOUNT_EVENT_KIND_FEEDBACK_LINKED,
        requestId: targetRequestID,
        requestDisplayId: 'CR-2',
        requestTitle: 'Renewal blocker',
        occurredAt: '2026-07-07T00:20:00Z',
        actorId: 'operator',
        subjectDisplay: 'Ada Lovelace',
        source: 'portal',
        description: 'Renewal owner reported export failure.',
        feedbackId: '42',
        issueProvider: '',
        issueKey: '',
        issueUrl: '',
        issueSyncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_UNSPECIFIED,
      },
      {
        kind: CustomerRequestAccountEventKind.CUSTOMER_REQUEST_ACCOUNT_EVENT_KIND_ISSUE_SYNCED,
        requestId: targetRequestID,
        requestDisplayId: 'CR-2',
        requestTitle: 'Renewal blocker',
        occurredAt: '2026-07-07T00:19:00Z',
        actorId: 'sync-worker',
        subjectDisplay: 'enterprise-team',
        source: 'github',
        description: 'failed',
        feedbackId: '0',
        issueProvider: 'github',
        issueKey: 'ATT-236',
        issueUrl: 'https://github.example/attune/issues/236',
        issueSyncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_FAILED,
      },
      {
        kind: CustomerRequestAccountEventKind.CUSTOMER_REQUEST_ACCOUNT_EVENT_KIND_NOTE_ADDED,
        requestId: requestID,
        requestDisplayId: 'CR-1',
        requestTitle: 'Export bundle',
        occurredAt: '2026-07-07T00:18:00Z',
        actorId: 'cs-lead',
        subjectDisplay: '',
        source: 'note',
        description: 'CS marked this account as renewal-sensitive.',
        feedbackId: '0',
        issueProvider: '',
        issueKey: '',
        issueUrl: '',
        issueSyncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_UNSPECIFIED,
      },
    ],
    timeline: [sampleSummary()],
    ...overrides,
  }
}

function sampleDetail(
  overrides: Partial<CustomerRequestDetail> & Partial<CustomerRequestSummary> = {},
): CustomerRequestDetail {
  const request = sampleSummary(overrides)
  const issueLinks = overrides.issueLinks ?? []
  return {
    request,
    description: overrides.description ?? 'Bundle feedback exports for enterprise accounts.',
    feedback: overrides.feedback ?? [],
    issueLinks,
    deliveryGraph: overrides.deliveryGraph ?? sampleDeliveryGraph(request, issueLinks),
    auditEntries: overrides.auditEntries ?? [],
    customers: overrides.customers ?? [],
    votes: overrides.votes ?? [],
    notes: overrides.notes ?? [],
    duplicates: overrides.duplicates ?? [],
    decisionRecords: overrides.decisionRecords ?? [sampleDecisionRecord(request)],
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

function sampleDeliveryGraph(
  request: CustomerRequestSummary,
  issueLinks: CustomerRequestIssueLink[],
): CustomerRequestDeliveryGraph {
  const rootID = `request:${request.id}`
  const issueArtifacts = issueLinks.map((issue) => ({
    id: `issue_link:${issue.id}`,
    provider: issue.provider,
    artifactType: 'issue',
    externalKey: issue.externalKey,
    externalUrl: issue.externalUrl,
    title: issue.title,
    status: issue.status,
    statusCategory: issue.externalStatusCategory,
    assignee: issue.externalAssignee,
    syncState: issue.syncState,
    health: deliveryHealthForSyncState(issue.syncState),
    lastSeenAt: issue.externalUpdatedAt || issue.lastSyncedAt || issue.updatedAt,
    source: 'customer_request_issue_link',
    syncError: issue.syncError,
  }))
  return {
    artifacts: [
      {
        id: rootID,
        provider: 'attune',
        artifactType: 'customer_request',
        externalKey: request.displayId,
        externalUrl: '',
        title: request.title,
        status: request.status,
        statusCategory: '',
        assignee: request.owner?.email ?? '',
        syncState: CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_UNSPECIFIED,
        health: request.deliveryHealth,
        lastSeenAt: request.updatedAt,
        source: 'customer_request',
        syncError: '',
      },
      ...issueArtifacts,
    ],
    relationships: issueLinks.map((issue) => ({
      id: `rel:${rootID}:issue_link:${issue.id}`,
      sourceArtifactId: rootID,
      targetArtifactId: `issue_link:${issue.id}`,
      relationshipType: 'tracked_by',
      provider: issue.provider,
      createdAt: issue.createdAt,
    })),
    health: request.deliveryHealth,
    healthExplanation: deliveryGraphHealthExplanation(issueLinks),
    updatedAt: issueArtifacts[issueArtifacts.length - 1]?.lastSeenAt ?? request.updatedAt,
  }
}

function deliveryHealthForSyncState(state: CustomerRequestIssueSyncState) {
  switch (state) {
    case CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_FAILED:
      return CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_FAILED
    case CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_STALE:
      return CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_STALE
    case CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED:
      return CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_SYNCED
    case CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_MANUAL:
      return CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_MANUAL
    default:
      return CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_PENDING
  }
}

function deliveryGraphHealthExplanation(issueLinks: CustomerRequestIssueLink[]) {
  if (issueLinks.length === 0) return 'No delivery artifacts are linked.'
  const parts = [
    issueStateCount(
      issueLinks,
      CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_SYNCED,
      'synced',
    ),
    issueStateCount(
      issueLinks,
      CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_STALE,
      'stale',
    ),
    issueStateCount(
      issueLinks,
      CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_FAILED,
      'failed',
    ),
    issueStateCount(
      issueLinks,
      CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_PENDING,
      'pending',
    ),
    issueStateCount(
      issueLinks,
      CustomerRequestIssueSyncState.CUSTOMER_REQUEST_ISSUE_SYNC_STATE_MANUAL,
      'manual',
    ),
  ].filter(Boolean)
  if (parts.length === 0) return `${issueLinks.length} linked artifacts.`
  return `${issueLinks.length} linked artifacts: ${parts.join(', ')}.`
}

function issueStateCount(
  issueLinks: CustomerRequestIssueLink[],
  state: CustomerRequestIssueSyncState,
  label: string,
) {
  const count = issueLinks.filter((issue) => issue.syncState === state).length
  return count ? `${count} ${label}` : ''
}

function sampleDecisionRecord(request: CustomerRequestSummary) {
  return {
    auditId: '1',
    action: 'customer_request.update',
    actorType: 'admin',
    actorId: 'pm-1',
    summary: 'Updated customer request',
    createdAt: '2026-07-07T01:05:00Z',
    statusChanged: true,
    oldStatus: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
    newStatus: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED,
    priorityChanged: true,
    oldPriority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
    newPriority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT,
    ownerChanged: false,
    oldOwnerMemberId: '',
    newOwnerMemberId: '',
    titleChanged: false,
    descriptionChanged: true,
    hasDecisionSnapshot: true,
    decisionScore: request.decisionScore,
    decisionScoreFactors: request.decisionScoreFactors,
    deliveryHealth: request.deliveryHealth,
    supportingFeedbackCount: request.supportingFeedbackCount,
    customerCount: request.customerCount,
    accountCount: request.accountCount,
    voteCount: request.voteCount,
    revenueImpactCents: request.revenueImpactCents,
    revenueCurrency: request.revenueCurrency,
    decisionRationale: 'priority=high feedback=2 customers=1 accounts=1',
    ownerMemberId: '22222222-2222-2222-2222-222222222222',
    ownerDisplay: 'owner@example.com',
    evidenceBundleRef: `customer-request/${request.id}/evidence/${request.displayId}`,
    publicSafeState:
      CustomerRequestDecisionPublicSafeState.CUSTOMER_REQUEST_DECISION_PUBLIC_SAFE_STATE_NEEDS_REVIEW,
    publicSafeReasons: ['revenue_context'],
  }
}

function sampleNote(overrides: Partial<CustomerRequestNote> = {}): CustomerRequestNote {
  return {
    id: noteID,
    body: 'Prioritize after ACME review.',
    createdBy: 'operator@example.com',
    createdAt: '2026-07-07T00:15:00Z',
    ...overrides,
  }
}
