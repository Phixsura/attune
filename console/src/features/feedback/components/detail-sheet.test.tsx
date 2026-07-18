import userEvent from '@testing-library/user-event'
import type { TFunction } from 'i18next'
import { delay, HttpResponse, http } from 'msw'
import { type ReactNode, useRef, useState } from 'react'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  FeedbackDetailSheet,
  feedbackDetailSheetTestables,
} from '@/features/feedback/components/detail-sheet'
import type { Dimension } from '@/proto/attune/v1/common'
import {
  CustomerRequestDeliveryHealth,
  CustomerRequestImportance,
  CustomerRequestPriority,
  CustomerRequestStatus,
  type CustomerRequestSummary,
} from '@/proto/attune/v1/customer_request'
import type { FeedbackDetail, ReplyDraftWorkflow } from '@/proto/attune/v1/ingest'
import { expectNoA11yViolations } from '@/testing/a11y'
import { server } from '@/testing/mocks/server'
import { fireEvent, renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

vi.mock('@tanstack/react-router', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-router')>('@tanstack/react-router')

  return {
    ...actual,
    Link: ({
      children,
      to,
      search,
      ...props
    }: {
      children: ReactNode
      to?: string
      search?: Record<string, unknown>
    }) => {
      const params = new URLSearchParams()
      if (search) {
        for (const [key, value] of Object.entries(search)) {
          if (value == null) continue
          params.set(key, String(value))
        }
      }
      const qs = params.toString()
      return (
        <a href={`${to ?? '#'}${qs ? `?${qs}` : ''}`} {...props}>
          {children}
        </a>
      )
    },
  }
})

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }))

const dims: Dimension[] = [
  {
    name: 'severity',
    displayName: { entries: { default: 'Severity', 'zh-CN': '严重程度' } },
    kind: 'single',
    taxonomy: [
      { value: 'P0', displayName: { entries: { default: 'P0' } }, examples: [] },
      { value: 'P1', displayName: { entries: { default: 'P1' } }, examples: [] },
    ],
    urgentSet: ['P0'],
    required: false,
    examples: [],
    extractionHint: '',
  },
]

const customerRequestsURL = '/fb/v1/console/customer-requests'

describe('FeedbackDetailSheet', () => {
  beforeEach(() => {
    vi.mocked(toast.success).mockClear()
    vi.mocked(toast.error).mockClear()
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
      configurable: true,
    })
    server.use(http.get(customerRequestsURL, () => HttpResponse.json({ requests: [] })))
  })

  it('id=null renders nothing (sheet closed)', () => {
    renderWithProviders(
      <FeedbackDetailSheet id={null} dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    // No urgent dot, no title rendered.
    expect(screen.queryByText(/payment failed/i)).toBeNull()
  })

  it('covers detail helper state matrices and defensive parsing', () => {
    const t = ((key: string) => key) as TFunction
    const keyT = (key: string) => key
    const base = feedbackRow('helper')

    expect(
      feedbackDetailSheetTestables.customerRequestStatusLabel(
        CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_PLANNED,
        t,
      ),
    ).toBe('customer_requests.statuses.planned')
    expect(
      feedbackDetailSheetTestables.customerRequestStatusLabel(
        CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_IN_PROGRESS,
        t,
      ),
    ).toBe('customer_requests.statuses.in_progress')
    expect(
      feedbackDetailSheetTestables.customerRequestStatusLabel(
        CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_SHIPPED,
        t,
      ),
    ).toBe('customer_requests.statuses.shipped')
    expect(
      feedbackDetailSheetTestables.customerRequestStatusLabel(
        CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_CANCELLED,
        t,
      ),
    ).toBe('customer_requests.statuses.cancelled')
    expect(
      feedbackDetailSheetTestables.customerRequestStatusLabel(
        99 as unknown as CustomerRequestStatus,
        t,
      ),
    ).toBe('customer_requests.statuses.open')

    expect(
      feedbackDetailSheetTestables.customerRequestPriorityLabel(
        CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_LOW,
        t,
      ),
    ).toBe('customer_requests.priorities.low')
    expect(
      feedbackDetailSheetTestables.customerRequestPriorityLabel(
        CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_MEDIUM,
        t,
      ),
    ).toBe('customer_requests.priorities.medium')
    expect(
      feedbackDetailSheetTestables.customerRequestPriorityLabel(
        CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
        t,
      ),
    ).toBe('customer_requests.priorities.high')
    expect(
      feedbackDetailSheetTestables.customerRequestPriorityLabel(
        CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_URGENT,
        t,
      ),
    ).toBe('customer_requests.priorities.urgent')
    expect(
      feedbackDetailSheetTestables.customerRequestPriorityLabel(
        99 as unknown as CustomerRequestPriority,
        t,
      ),
    ).toBe('customer_requests.priorities.none')

    expect(
      feedbackDetailSheetTestables.detailSummaryState(
        { ...base, enrichmentError: 'failed' },
        false,
        keyT,
      ),
    ).toEqual({ tone: 'error', label: 'feedback.row.classification_failed' })
    expect(feedbackDetailSheetTestables.detailSummaryState(base, true, keyT)).toEqual({
      tone: 'success',
      label: 'feedback.row.classification_ready',
    })
    expect(
      feedbackDetailSheetTestables.detailSummaryState(
        { ...base, enrichmentStatus: 'enriching' },
        false,
        keyT,
      ),
    ).toEqual({ tone: 'muted', label: 'feedback.row.classification_enriching' })
    expect(
      feedbackDetailSheetTestables.detailSummaryState(
        { ...base, enrichmentStatus: 'pending' },
        false,
        keyT,
      ),
    ).toEqual({ tone: 'muted', label: 'feedback.row.classification_pending' })

    expect(feedbackDetailSheetTestables.workbenchModeLabel('urgent', keyT)).toBe(
      'feedback.queue_mode.urgent',
    )
    expect(feedbackDetailSheetTestables.workbenchModeLabel('active', keyT)).toBe(
      'feedback.queue_mode.active',
    )
    expect(feedbackDetailSheetTestables.workbenchModeLabel('failed', keyT)).toBe(
      'feedback.queue_mode.failed',
    )
    expect(feedbackDetailSheetTestables.workbenchModeLabel('terminal', keyT)).toBe(
      'feedback.queue_mode.terminal',
    )
    expect(feedbackDetailSheetTestables.workbenchModeLabel('ready', keyT)).toBe(
      'feedback.queue_mode.ready',
    )
    expect(feedbackDetailSheetTestables.workbenchModeLabel('all', keyT)).toBe(
      'feedback.queue_mode.all',
    )
    expect(feedbackDetailSheetTestables.detailWorkbenchCue('all', false, keyT)).toBeNull()
    expect(feedbackDetailSheetTestables.detailWorkbenchCue('urgent', false, keyT)).toMatchObject({
      tone: 'danger',
      title: 'feedback.detail.workbench_urgent_title',
    })
    expect(feedbackDetailSheetTestables.detailWorkbenchCue('ready', true, keyT)).toMatchObject({
      tone: 'success',
      body: 'feedback.detail.workbench_ready_body',
    })
    expect(feedbackDetailSheetTestables.detailWorkbenchCue('ready', false, keyT)).toMatchObject({
      tone: 'warning',
      body: 'feedback.detail.workbench_ready_missing_body',
    })
    expect(
      feedbackDetailSheetTestables.detailWorkbenchCue('unknown' as never, false, keyT),
    ).toBeNull()

    expect(
      feedbackDetailSheetTestables.terminalFailureSnapshotPresent({
        ...base,
        enrichmentFailureModel: 'gpt-test',
      }),
    ).toBe(true)
    expect(feedbackDetailSheetTestables.terminalFailureSnapshotPresent(base)).toBe(false)
    expect(feedbackDetailSheetTestables.terminalFailureReasonClassLabel('llm_err', keyT)).toBe(
      'feedback.detail.failure_reason_class_llm',
    )
    expect(feedbackDetailSheetTestables.terminalFailureReasonClassLabel('parse_err', keyT)).toBe(
      'feedback.detail.failure_reason_class_parse',
    )
    expect(feedbackDetailSheetTestables.terminalFailureReasonClassLabel('other_err', keyT)).toBe(
      'feedback.detail.failure_reason_class_other',
    )
    expect(feedbackDetailSheetTestables.terminalFailureReasonClassLabel('', keyT)).toBe('—')

    expect(feedbackDetailSheetTestables.isPositiveIntString(' 42 ')).toBe(true)
    expect(feedbackDetailSheetTestables.isPositiveIntString('0')).toBe(false)
    expect(feedbackDetailSheetTestables.relativeTime('')).toBeNull()
    expect(feedbackDetailSheetTestables.relativeTime('not-a-date')).toBeNull()

    const workflow: ReplyDraftWorkflow = {
      draftId: 'draft-1',
      feedbackId: 'helper',
      cycleNo: 1,
      status: 'sent',
      activeRevisionId: 'rev-2',
      approvedRevisionId: 'rev-2',
      sentRevisionId: 'rev-2',
      activeText: 'Human reply',
      allowedActions: [],
      blockers: [],
      hookConfigured: true,
      revision: '3',
      updatedAt: '2026-07-03T10:00:00Z',
      revisions: [
        {
          id: 'rev-1',
          draftId: 'draft-1',
          cycleNo: 1,
          revisionNo: 1,
          origin: 'ai',
          content: 'AI reply',
          createdBy: 'assistant',
          createdAt: '2026-07-03T09:55:00Z',
        },
        {
          id: 'rev-2',
          draftId: 'draft-1',
          cycleNo: 1,
          revisionNo: 2,
          origin: 'human',
          content: 'Human reply',
          createdBy: 'member-1',
          createdAt: '2026-07-03T10:00:00Z',
        },
      ],
      events: [
        {
          id: 'evt-1',
          draftId: 'draft-1',
          eventType: 'sent',
          actorType: 'user',
          actorId: 'member-1',
          blocker: '',
          createdAt: '2026-07-03T10:05:00Z',
        },
      ],
    }

    expect(feedbackDetailSheetTestables.isCompleteReplyDraftWorkflow(workflow)).toBe(true)
    expect(feedbackDetailSheetTestables.isCompleteReplyDraftWorkflow(undefined)).toBe(false)
    expect(feedbackDetailSheetTestables.latestRevisionByOrigin(workflow, 'ai')?.id).toBe('rev-1')
    expect(feedbackDetailSheetTestables.revisionByID(workflow, 'rev-2')?.origin).toBe('human')
    expect(feedbackDetailSheetTestables.revisionByID(workflow, undefined)).toBeUndefined()
    expect(feedbackDetailSheetTestables.replyDraftTimelineItems(workflow)[0].key).toBe('evt-1')
    expect(feedbackDetailSheetTestables.isReplyDraftHardBlocker('send_hook_changed')).toBe(true)
    expect(feedbackDetailSheetTestables.isReplyDraftHardBlocker('send_failed')).toBe(false)

    expect(
      feedbackDetailSheetTestables.portalSubmissionMeta({
        portal_submission: {
          kind: 'general',
          title: '  Hello  ',
          details: '  Details  ',
          custom_fields: { z: 1, a: null },
        },
      }),
    ).toMatchObject({
      kind: 'general',
      title: 'Hello',
      details: 'Details',
      customFields: { z: 1, a: null },
    })
    expect(feedbackDetailSheetTestables.portalSubmissionMeta({ portal_submission: {} })).toBeNull()
    expect(feedbackDetailSheetTestables.portalSubmissionEntries({ b: 2, a: 1 })).toEqual([
      ['a', 1],
      ['b', 2],
    ])
    expect(feedbackDetailSheetTestables.portalSubmissionKindLabel('general', t)).toBe(
      'feedback.type.general',
    )
    expect(feedbackDetailSheetTestables.portalSubmissionKindLabel('', t)).toBe('—')
    expect(feedbackDetailSheetTestables.portalSubmissionContactFieldLabel('display_name', t)).toBe(
      'feedback.detail.portal_submission_display_name',
    )
    expect(feedbackDetailSheetTestables.portalSubmissionContactFieldLabel('organization', t)).toBe(
      'feedback.detail.portal_submission_organization',
    )
    expect(feedbackDetailSheetTestables.portalSubmissionContactFieldLabel('email', t)).toBe('email')
    expect(feedbackDetailSheetTestables.portalSubmissionValueNode(null)).toBe('—')
    expect(feedbackDetailSheetTestables.portalSubmissionValueNode('   ')).toBe('—')
    expect(feedbackDetailSheetTestables.portalSubmissionValueNode(7)).toBe('7')
    expect(feedbackDetailSheetTestables.portalSubmissionValueNode(Symbol('x'))).toBe('Symbol(x)')
    const circular: Record<string, unknown> = {}
    circular.self = circular
    expect(feedbackDetailSheetTestables.portalSubmissionValueNode(circular)).toBe('—')
    expect(feedbackDetailSheetTestables.portalSubmissionText(42)).toBe('')
    expect(feedbackDetailSheetTestables.portalSubmissionText('  value  ', true)).toBe('value')
    expect(feedbackDetailSheetTestables.isPortalRecord({ ok: true })).toBe(true)
    expect(feedbackDetailSheetTestables.isPortalRecord(['nope'])).toBe(false)
  })

  it('id set → renders feedback fields after the detail query resolves', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-1',
          content: 'payment failed at checkout',
          enrichedTitle: 'Payment failed',
          enrichedDisplayTitle: '支付失败',
          enrichedRationale: 'AI rationale',
          enrichedDisplayRationale: 'AI 中文解读',
          enrichedDisplayLocale: 'zh',
          enrichedAttrs: { severity: 'P0' },
          enrichedAt: '2026-06-07T10:30:00Z',
          language: 'en',
          isUrgent: true,
          source: 'web',
          userId: 'u-1',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: null,
          attachments: [],
          enrichmentError: '',
        }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-1" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    // Title, raw content, AI rationale all appear once the query resolves.
    await waitFor(() => expect(screen.getAllByText('支付失败').length).toBeGreaterThanOrEqual(1))
    expect(screen.getByText(/payment failed at checkout/i)).toBeInTheDocument()
    expect(screen.getByText(/AI 中文解读/i)).toBeInTheDocument()
    expect(screen.getByText(/AI rationale/i)).toBeInTheDocument()
    expect(screen.getAllByTitle('原文语言：英文').length).toBeGreaterThanOrEqual(1)
  })

  it('treats array-valued dimensions as classification signal', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          feedbackRow('array-attrs', {
            enrichedRationale: '',
            enrichedDisplayRationale: '',
            enrichedAttrs: { severity: ['P0'] },
            classificationConfidence: undefined,
          }),
        ),
      ),
    )

    renderWithProviders(
      <FeedbackDetailSheet
        id="array-attrs"
        dims={dims}
        availableTags={[]}
        onOpenChange={vi.fn()}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('分类已就绪').length).toBeGreaterThanOrEqual(1))
    expect(screen.queryByText('暂未分类')).not.toBeInTheDocument()
  })

  it('hides customer-request linking controls when the operator lacks edit permission', async () => {
    server.use(
      http.get('/fb/v1/console/me', () =>
        HttpResponse.json({
          tenant: {
            id: 'tenant-1',
            name: 'Tenant',
            slug: 'tenant',
            locale: 'zh-CN',
            timezone: 'UTC',
          },
          user: { openId: 'viewer-1', name: 'Viewer', role: 'viewer' },
          csrfToken: 'csrf-test-token',
        }),
      ),
      http.get('/fb/v1/console/feedback/:id', () => HttpResponse.json(feedbackRow('42'))),
    )

    renderWithProviders(
      <FeedbackDetailSheet id="42" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    await waitFor(() => expect(screen.getAllByText('Need CSV export').length).toBeGreaterThan(0))
    expect(screen.getByText('关联客户需求')).toBeInTheDocument()
    expect(screen.queryByLabelText('搜索客户需求')).not.toBeInTheDocument()
  })

  it('hides customer-request links completely when the operator cannot view them', async () => {
    server.use(
      http.get('/fb/v1/console/me', () =>
        HttpResponse.json({
          tenant: {
            id: 'tenant-1',
            name: 'Tenant',
            slug: 'tenant',
            locale: 'zh-CN',
            timezone: 'UTC',
          },
          user: { openId: 'guest-1', name: 'Guest', role: 'guest' },
          csrfToken: 'csrf-test-token',
        }),
      ),
      http.get('/fb/v1/console/feedback/:id', () => HttpResponse.json(feedbackRow('42'))),
    )

    renderWithProviders(
      <FeedbackDetailSheet id="42" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    await waitFor(() => expect(screen.getAllByText('Need CSV export').length).toBeGreaterThan(0))
    await waitFor(() => expect(screen.queryByText('关联客户需求')).not.toBeInTheDocument())
  })

  it('renders structured portal submission evidence when source meta contains portal data', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          feedbackRow('401', {
            source: 'portal',
            userId: 'portal_user',
            sourceMeta: {
              portal_submission: {
                kind: 'bug',
                title: 'Login fails',
                details: 'After SSO redirect\nThe form resets.',
                page_url: 'https://app.example.com/login',
                private_contact: {
                  display_name: 'Ada Lovelace',
                  organization: 'Acme',
                },
                custom_fields: {
                  severity: 'high',
                  components: ['ui', 'api'],
                  consent: true,
                },
                user_agent: 'PortalTest/1.0',
              },
            },
          }),
        ),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="portal-1" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    const portalHeading = await screen.findByRole('heading', { name: '门户投稿' })
    const portalSection = portalHeading.closest('section')
    expect(portalSection).not.toBeNull()
    const portal = within(portalSection as HTMLElement)

    expect(portal.getByText('缺陷')).toBeInTheDocument()
    expect(portal.getByText('Login fails')).toBeInTheDocument()
    expect(portal.getByText(/After SSO redirect/)).toBeInTheDocument()
    expect(portal.getByRole('link', { name: 'https://app.example.com/login' })).toHaveAttribute(
      'href',
      'https://app.example.com/login',
    )
    expect(portal.getByText('显示名')).toBeInTheDocument()
    expect(portal.getByText('Ada Lovelace')).toBeInTheDocument()
    expect(portal.getByText('组织')).toBeInTheDocument()
    expect(portal.getByText('Acme')).toBeInTheDocument()
    expect(portal.getByText('severity')).toBeInTheDocument()
    expect(portal.getByText('high')).toBeInTheDocument()
    expect(portal.getByText('components')).toBeInTheDocument()
    expect(portal.getByText(/"ui"/, { selector: 'pre' })).toBeInTheDocument()
    expect(portal.getByText(/"api"/, { selector: 'pre' })).toBeInTheDocument()
    expect(portal.getByText('consent')).toBeInTheDocument()
    expect(portal.getByText('true')).toBeInTheDocument()
    expect(portal.getByText('PortalTest/1.0')).toBeInTheDocument()
    await waitFor(() =>
      expect(portal.getByRole('link', { name: '转为客户需求' })).toHaveAttribute(
        'href',
        '/feedback/customer-requests?promote_feedback_ids=401&feedback_id=401',
      ),
    )
  })

  it('renders HTML-like portal submission text as escaped text nodes', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          feedbackRow('402', {
            source: 'portal',
            userId: 'portal_user',
            sourceMeta: {
              portal_submission: {
                kind: 'request',
                title: '<img src=x onerror="window.__portalXssTitle=1">',
                details: 'line1\n<svg onload="window.__portalXssDetails=1"></svg>',
                page_url: 'https://app.example.com/login?next=%3Ctag%3E',
                private_contact: {
                  display_name: '<b>Ada</b>',
                },
                custom_fields: {
                  note: '<em>xss</em>',
                  flags: ['<script>alert(1)</script>'],
                },
                user_agent: 'PortalTest/1.0',
              },
            },
          }),
        ),
      ),
    )

    renderWithProviders(
      <FeedbackDetailSheet id="402" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    const portalHeading = await screen.findByRole('heading', { name: '门户投稿' })
    const portalSection = portalHeading.closest('section')
    expect(portalSection).not.toBeNull()

    const portal = within(portalSection as HTMLElement)
    const portalHTML = (portalSection as HTMLElement).innerHTML
    expect(portalHTML).toContain('&lt;img src=x onerror="window.__portalXssTitle=1"&gt;')
    expect(portalHTML).toContain('&lt;svg onload="window.__portalXssDetails=1"&gt;&lt;/svg&gt;')
    expect(portal.getByText('<b>Ada</b>')).toBeInTheDocument()
    expect(portal.getByText('<em>xss</em>')).toBeInTheDocument()
    expect(portal.getByText(/<script>alert\(1\)<\/script>/)).toBeInTheDocument()
    expect(portal.getByText('PortalTest/1.0')).toBeInTheDocument()
    expect(portal.queryByRole('img')).not.toBeInTheDocument()
  })

  it('renders linked customer requests and unlinks them from the feedback detail', async () => {
    const linkedRequest = customerRequestSummary()
    let unlinked = false
    server.use(
      http.get('/fb/v1/console/feedback/:id', () => HttpResponse.json(feedbackRow('42'))),
      http.get(customerRequestsURL, ({ request }) => {
        const url = new URL(request.url)
        if (url.searchParams.get('feedback_id') === '42') {
          return HttpResponse.json({ requests: unlinked ? [] : [linkedRequest] })
        }
        return HttpResponse.json({ requests: [] })
      }),
      http.delete(`${customerRequestsURL}/${linkedRequest.id}/feedback/42`, () => {
        unlinked = true
        return HttpResponse.json(customerRequestDetail(linkedRequest))
      }),
    )

    renderWithProviders(
      <FeedbackDetailSheet id="42" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    await waitFor(() => expect(screen.getByText('关联客户需求')).toBeInTheDocument())
    expect(await screen.findByText('CR-9')).toBeInTheDocument()
    expect(screen.getByText('Export bundles')).toBeInTheDocument()

    await userEvent.click(screen.getByRole('button', { name: '解除关联' }))

    await waitFor(() => expect(unlinked).toBe(true))
    await waitFor(() => expect(screen.getByText('这条反馈还没有关联客户需求')).toBeInTheDocument())
  })

  it('links the current feedback to a searched customer request', async () => {
    const candidate = customerRequestSummary()
    let payload: Record<string, unknown> | null = null
    let linked = false
    server.use(
      http.get('/fb/v1/console/feedback/:id', () => HttpResponse.json(feedbackRow('42'))),
      http.get(customerRequestsURL, ({ request }) => {
        const url = new URL(request.url)
        if (url.searchParams.get('feedback_id') === '42') {
          return HttpResponse.json({ requests: linked ? [candidate] : [] })
        }
        return HttpResponse.json({ requests: [candidate] })
      }),
      http.post(`${customerRequestsURL}/${candidate.id}/feedback`, async ({ request }) => {
        payload = (await request.json()) as Record<string, unknown>
        linked = true
        return HttpResponse.json(customerRequestDetail(candidate))
      }),
    )

    renderWithProviders(
      <FeedbackDetailSheet id="42" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    await userEvent.type(await screen.findByLabelText('搜索客户需求'), 'Export')
    await userEvent.click(await screen.findByRole('button', { name: /CR-9.*Export bundles/s }))
    await userEvent.click(screen.getByRole('button', { name: '关联需求' }))

    await waitFor(() => expect(payload).not.toBeNull())
    expect(payload).toMatchObject({
      id: candidate.id,
      feedbackId: '42',
      importance: CustomerRequestImportance.CUSTOMER_REQUEST_IMPORTANCE_NORMAL,
    })
  })

  it('ignores customer-request link submit when no request is selected', async () => {
    const candidate = customerRequestSummary()
    let posted = false
    server.use(
      http.get('/fb/v1/console/feedback/:id', () => HttpResponse.json(feedbackRow('42'))),
      http.get(customerRequestsURL, ({ request }) => {
        const url = new URL(request.url)
        if (url.searchParams.get('feedback_id') === '42') {
          return HttpResponse.json({ requests: [] })
        }
        return HttpResponse.json({ requests: [candidate] })
      }),
      http.post(`${customerRequestsURL}/${candidate.id}/feedback`, () => {
        posted = true
        return HttpResponse.json(customerRequestDetail(candidate))
      }),
    )

    renderWithProviders(
      <FeedbackDetailSheet id="42" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    const search = await screen.findByLabelText('搜索客户需求')
    await userEvent.type(search, 'Export')
    const form = screen.getByRole('button', { name: /关联需求/ }).closest('form')
    expect(form).not.toBeNull()
    fireEvent.submit(form as HTMLFormElement)

    await waitFor(() => expect(screen.getByRole('button', { name: /关联需求/ })).toBeDisabled())
    expect(posted).toBe(false)
  })

  it('shows linked-request load errors and retries the query', async () => {
    const linkedRequest = customerRequestSummary()
    let failLinkedLoad = true
    server.use(
      http.get('/fb/v1/console/feedback/:id', () => HttpResponse.json(feedbackRow('42'))),
      http.get(customerRequestsURL, ({ request }) => {
        const url = new URL(request.url)
        if (url.searchParams.get('feedback_id') === '42') {
          if (failLinkedLoad) {
            failLinkedLoad = false
            return HttpResponse.json({ message: 'linked load failed' }, { status: 500 })
          }
          return HttpResponse.json({ requests: [linkedRequest] })
        }
        return HttpResponse.json({ requests: [] })
      }),
    )

    renderWithProviders(
      <FeedbackDetailSheet id="42" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    await waitFor(() => expect(screen.getByText('客户需求加载失败')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: '重试' }))
    await waitFor(() => expect(screen.getByText('CR-9')).toBeInTheDocument())
  })

  it('shows an error toast when linking a customer request fails', async () => {
    const candidate = customerRequestSummary()
    server.use(
      http.get('/fb/v1/console/feedback/:id', () => HttpResponse.json(feedbackRow('42'))),
      http.get(customerRequestsURL, ({ request }) => {
        const url = new URL(request.url)
        if (url.searchParams.get('feedback_id') === '42') {
          return HttpResponse.json({ requests: [] })
        }
        return HttpResponse.json({ requests: [candidate] })
      }),
      http.post(`${customerRequestsURL}/${candidate.id}/feedback`, () =>
        HttpResponse.json({ message: 'link denied' }, { status: 500 }),
      ),
    )

    renderWithProviders(
      <FeedbackDetailSheet id="42" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    await userEvent.type(await screen.findByLabelText('搜索客户需求'), 'Export')
    await userEvent.click(await screen.findByRole('button', { name: /CR-9.*Export bundles/s }))
    await userEvent.click(screen.getByRole('button', { name: '关联需求' }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('link denied'))
  })

  it('shows an error toast when unlinking a customer request fails', async () => {
    const linkedRequest = customerRequestSummary()
    server.use(
      http.get('/fb/v1/console/feedback/:id', () => HttpResponse.json(feedbackRow('42'))),
      http.get(customerRequestsURL, ({ request }) => {
        const url = new URL(request.url)
        if (url.searchParams.get('feedback_id') === '42') {
          return HttpResponse.json({ requests: [linkedRequest] })
        }
        return HttpResponse.json({ requests: [] })
      }),
      http.delete(`${customerRequestsURL}/${linkedRequest.id}/feedback/42`, () =>
        HttpResponse.json({ message: 'unlink denied' }, { status: 500 }),
      ),
    )

    renderWithProviders(
      <FeedbackDetailSheet id="42" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    await waitFor(() => expect(screen.getByText('CR-9')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: '解除关联' }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('unlink denied'))
  })

  it('does not show a source-language rationale block for same-language rows', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-2',
          content: '支付失败',
          enrichedTitle: '支付失败',
          enrichedDisplayTitle: '支付失败',
          enrichedRationale: '原文侧：支付流程受阻',
          enrichedDisplayRationale: '展示侧：支付流程受阻',
          enrichedDisplayLocale: 'zh-CN',
          enrichedAttrs: { severity: 'P0' },
          enrichedAt: '2026-06-07T10:30:00Z',
          language: 'zh',
          isUrgent: true,
          source: 'web',
          userId: 'u-1',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: null,
          attachments: [],
          enrichmentError: '',
        }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-2" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('展示侧：支付流程受阻')).toBeInTheDocument())
    expect(screen.queryByText('原语言解读')).toBeNull()
    expect(screen.queryByText('原文侧：支付流程受阻')).toBeNull()
  })

  it('renders a failure status banner when enrichment failed before classification landed', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-2b',
          content: '支付失败',
          enrichedTitle: '',
          enrichedDisplayTitle: '',
          enrichedRationale: '',
          enrichedDisplayRationale: '',
          enrichedDisplayLocale: 'zh-CN',
          enrichedAttrs: {},
          enrichedAt: '',
          language: 'zh',
          isUrgent: false,
          source: 'web',
          userId: 'u-1',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: null,
          attachments: [],
          enrichmentError: 'llm: llm_not_configured',
          classificationConfidence: undefined,
        }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-2b" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() =>
      expect(screen.getByText('这条反馈暂时没有可用的 AI 富化结果')).toBeInTheDocument(),
    )
    expect(screen.getAllByText('llm: llm_not_configured').length).toBeGreaterThanOrEqual(1)
  })

  it('renders terminal failure snapshot details and copies the error message', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-terminal',
          content: 'payment failed at checkout',
          enrichedTitle: '',
          enrichedDisplayTitle: '',
          enrichedRationale: '',
          enrichedDisplayRationale: '',
          enrichedDisplayLocale: 'zh',
          enrichedAttrs: {},
          enrichedAt: '',
          language: 'en',
          isUrgent: false,
          source: 'web',
          userId: 'u-1',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: { ticket: 'T-1' },
          attachments: [{ name: 'receipt.png', size: 1234 }],
          enrichmentError: 'LLM provider timeout',
          enrichmentStatus: 'failed',
          enrichmentAttempts: 5,
          enrichmentFailureReasonClass: 'mystery_reason',
          enrichmentFailureModel: 'gpt-4.1',
          enrichmentFailureChannelName: 'openai',
          enrichmentFailureChannelId: 'ch-1',
          enrichmentFailureConfigFingerprint: 'abc123',
          enrichmentFailurePromptVersion: 'v7',
          replyDraftEnabled: false,
        }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-terminal"
        dims={dims}
        availableTags={[]}
        workbenchMode="terminal"
        onOpenChange={vi.fn()}
      />,
    )
    await waitFor(() => expect(screen.getByText(/LLM provider timeout/)).toBeInTheDocument())
    expect(screen.getByText('mystery_reason')).toBeInTheDocument()
    expect(screen.getByText('gpt-4.1')).toBeInTheDocument()
    expect(screen.getByText('openai')).toBeInTheDocument()
    expect(screen.getByText('ch-1')).toBeInTheDocument()
    expect(screen.getByText('abc123')).toBeInTheDocument()
    expect(screen.getByText('v7')).toBeInTheDocument()
    expect(
      screen.getAllByText((_, node) => node?.textContent?.includes('"ticket": "T-1"') ?? false)
        .length,
    ).toBeGreaterThan(0)
    expect(
      screen.getAllByText(
        (_, node) => node?.textContent?.includes('"name": "receipt.png"') ?? false,
      ).length,
    ).toBeGreaterThan(0)

    const setTimeoutSpy = vi.spyOn(window, 'setTimeout').mockImplementation(((
      handler: TimerHandler,
    ) => {
      if (typeof handler === 'function') handler()
      return 1
    }) as typeof window.setTimeout)
    try {
      await userEvent.click(screen.getByTitle('复制'))
      expect(setTimeoutSpy).toHaveBeenCalledWith(expect.any(Function), 1500)
    } finally {
      setTimeoutSpy.mockRestore()
    }
  })

  it('closes the detail sheet when the visible close button is used', async () => {
    const onOpenChange = vi.fn()
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-close',
          content: 'payment failed at checkout',
          enrichedTitle: 'Payment failed',
          enrichedDisplayTitle: '支付失败',
          enrichedRationale: 'AI rationale',
          enrichedDisplayRationale: 'AI 中文解读',
          enrichedDisplayLocale: 'zh',
          enrichedAttrs: { severity: 'P0' },
          enrichedAt: '2026-06-07T10:30:00Z',
          language: 'en',
          isUrgent: true,
          source: 'web',
          userId: 'u-1',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: null,
          attachments: [],
          enrichmentError: '',
        }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-close"
        dims={dims}
        availableTags={[]}
        onOpenChange={onOpenChange}
      />,
    )
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('restores focus to the opener after Escape closes the controlled sheet', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-focus',
          content: 'payment failed at checkout',
          enrichedTitle: 'Payment failed',
          enrichedDisplayTitle: '支付失败',
          enrichedRationale: 'AI rationale',
          enrichedDisplayRationale: 'AI 中文解读',
          enrichedDisplayLocale: 'zh',
          enrichedAttrs: { severity: 'P0' },
          enrichedAt: '2026-06-07T10:30:00Z',
          language: 'en',
          isUrgent: true,
          source: 'web',
          userId: 'u-1',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: null,
          attachments: [],
          enrichmentError: '',
        }),
      ),
    )

    function SheetHarness() {
      const [id, setId] = useState<string | null>(null)
      const openerRef = useRef<HTMLButtonElement>(null)
      return (
        <>
          <button ref={openerRef} type="button" onClick={() => setId('f-focus')}>
            Open feedback detail
          </button>
          <FeedbackDetailSheet
            id={id}
            dims={dims}
            availableTags={[]}
            restoreFocusRef={openerRef}
            onOpenChange={(open) => setId(open ? 'f-focus' : null)}
          />
        </>
      )
    }

    const { user } = renderWithProviders(<SheetHarness />)
    const opener = screen.getByRole('button', { name: 'Open feedback detail' })

    opener.focus()
    await user.keyboard('[Enter]')

    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())
    await waitFor(() => expect(screen.getAllByText('支付失败').length).toBeGreaterThanOrEqual(1))
    await expectNoA11yViolations(document.body)

    await user.keyboard('[Escape]')

    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(opener).toHaveFocus()
  })

  it('shows workbench guidance for active queue context', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-active',
          content: 'payment failed at checkout',
          enrichedTitle: 'Payment failed',
          enrichedDisplayTitle: '支付失败',
          enrichedRationale: 'AI rationale',
          enrichedDisplayRationale: 'AI 中文解读',
          enrichedDisplayLocale: 'zh',
          enrichedAttrs: { severity: 'P0' },
          enrichedAt: '2026-06-07T10:30:00Z',
          language: 'en',
          isUrgent: false,
          source: 'web',
          userId: 'u-1',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: null,
          attachments: [],
          enrichmentError: '',
          workflowState: {
            id: 'ws-1',
            name: 'in_progress',
            displayName: { entries: { default: 'In Progress', zh: '处理中' } },
            color: '#f59e0b',
            category: 'active',
            position: 1,
            isDefault: false,
            archived: false,
            createdAt: '2026-06-07T00:00:00Z',
            updatedAt: '2026-06-07T00:00:00Z',
          },
        }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-active"
        dims={dims}
        availableTags={[]}
        workbenchMode="active"
        onOpenChange={vi.fn()}
      />,
    )
    await waitFor(() => expect(screen.getByText('先推进状态流转')).toBeInTheDocument())
    expect(screen.getByText('当前工作面：处理中')).toBeInTheDocument()
  })

  it('shows failure workbench guidance for failed queue context', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-failed',
          content: 'payment failed at checkout',
          enrichedTitle: '',
          enrichedDisplayTitle: '',
          enrichedRationale: '',
          enrichedDisplayRationale: '',
          enrichedDisplayLocale: 'zh',
          enrichedAttrs: {},
          enrichedAt: '',
          language: 'en',
          isUrgent: false,
          source: 'web',
          userId: 'u-1',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: null,
          attachments: [],
          enrichmentError: 'llm: failed',
        }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-failed"
        dims={dims}
        availableTags={[]}
        workbenchMode="failed"
        onOpenChange={vi.fn()}
      />,
    )
    await waitFor(() => expect(screen.getByText('先判断是个例还是配置问题')).toBeInTheDocument())
    expect(screen.getByText('当前工作面：富化失败')).toBeInTheDocument()
  })

  it('shows the reply draft with copy + regenerate when present', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-3', { replyDraft: 'Sorry to hear that — we are investigating.' }),
        ),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-3" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText(/Sorry to hear that/i)).toBeInTheDocument())
    expect(screen.getByText('回复草稿')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /复制/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /重新生成/ })).toBeInTheDocument()
  })

  it('shows workflow status, controlled-send actions, and revision history', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-workflow', {
            replyDraft: 'legacy projection',
            replyDraftWorkflow: {
              draftId: 'draft-1',
              feedbackId: 'f-workflow',
              cycleNo: 1,
              status: 'approved',
              activeRevisionId: 'rev-2',
              approvedRevisionId: 'rev-2',
              activeText: 'Human edited reply',
              allowedActions: ['edit', 'send', 'reject', 'regenerate'],
              blockers: [],
              hookConfigured: true,
              revision: '3',
              updatedAt: '2026-07-03T10:00:00Z',
              revisions: [
                {
                  id: 'rev-2',
                  draftId: 'draft-1',
                  cycleNo: 1,
                  revisionNo: 2,
                  origin: 'human',
                  content: 'Human edited reply',
                  createdBy: 'member-1',
                  createdAt: '2026-07-03T10:00:00Z',
                },
                {
                  id: 'rev-1',
                  draftId: 'draft-1',
                  cycleNo: 1,
                  revisionNo: 1,
                  origin: 'ai',
                  content: 'AI suggested reply',
                  createdBy: 'assistant',
                  createdAt: '2026-07-03T09:55:00Z',
                },
              ],
              events: [],
            },
          }),
        ),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-workflow" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    await waitFor(() =>
      expect(screen.getAllByText('Human edited reply').length).toBeGreaterThanOrEqual(1),
    )
    expect(screen.getByText('已批准')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /编辑/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /发送/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /拒绝/ })).toBeInTheDocument()
    expect(screen.getByText('AI 建议')).toBeInTheDocument()
    expect(screen.getByText('人工编辑')).toBeInTheDocument()
    expect(screen.getByText('编辑历史')).toBeInTheDocument()
    expect(screen.getByText('发送检查')).toBeInTheDocument()
    expect(screen.getByText('证据')).toBeInTheDocument()
    expect(screen.getByText('变更对比')).toBeInTheDocument()
    expect(screen.getByText(/人工\s+v2/)).toBeInTheDocument()
    expect(screen.getByText(/AI\s+v1/)).toBeInTheDocument()
    expect(screen.getAllByText('AI suggested reply').length).toBeGreaterThanOrEqual(1)
  })

  it('opens a send preflight before posting the approved reply', async () => {
    let requestBody: Record<string, unknown> | null = null
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-preflight', {
            source: 'web',
            userId: 'user-42',
            replyDraft: 'legacy projection',
            replyDraftWorkflow: {
              draftId: 'draft-1',
              feedbackId: 'f-preflight',
              cycleNo: 1,
              status: 'approved',
              activeRevisionId: 'rev-2',
              approvedRevisionId: 'rev-2',
              activeText: 'Human edited reply ready to send',
              allowedActions: ['edit', 'send', 'reject', 'regenerate'],
              blockers: [],
              hookConfigured: true,
              approvedBy: 'member-1',
              revision: '7',
              updatedAt: '2026-07-03T10:00:00Z',
              revisions: [
                {
                  id: 'rev-2',
                  draftId: 'draft-1',
                  cycleNo: 1,
                  revisionNo: 2,
                  origin: 'human',
                  content: 'Human edited reply ready to send',
                  createdBy: 'member-1',
                  createdAt: '2026-07-03T10:00:00Z',
                },
                {
                  id: 'rev-1',
                  draftId: 'draft-1',
                  cycleNo: 1,
                  revisionNo: 1,
                  origin: 'ai',
                  content: 'AI suggested reply',
                  createdBy: 'assistant',
                  createdAt: '2026-07-03T09:55:00Z',
                },
              ],
              events: [],
            },
          }),
        ),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/send', async ({ request }) => {
        requestBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({
          workflow: {
            draftId: 'draft-1',
            feedbackId: 'f-preflight',
            cycleNo: 1,
            status: 'sent',
            activeRevisionId: 'rev-2',
            approvedRevisionId: 'rev-2',
            sentRevisionId: 'rev-2',
            activeText: 'Human edited reply ready to send',
            allowedActions: [],
            blockers: [],
            hookConfigured: true,
            sentAt: '2026-07-03T10:05:00Z',
            sentBy: 'member-1',
            revision: '8',
            updatedAt: '2026-07-03T10:05:00Z',
            revisions: [],
            events: [],
          },
          fromCache: false,
        })
      }),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-preflight"
        dims={dims}
        availableTags={[]}
        onOpenChange={vi.fn()}
      />,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: '发送' })).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: '发送' }))
    expect(requestBody).toBeNull()
    const preflight = await screen.findByRole('dialog', { name: '确认发送回复' })
    expect(screen.getByText('最终发送文本')).toBeInTheDocument()
    expect(within(preflight).getByText('Human edited reply ready to send')).toBeInTheDocument()
    await userEvent.click(within(preflight).getByRole('button', { name: '取消' }))
    await waitFor(() => expect(screen.queryByRole('dialog', { name: '确认发送回复' })).toBeNull())
    await userEvent.click(screen.getByRole('button', { name: '发送' }))
    await userEvent.click(screen.getByRole('button', { name: /确认发送/ }))

    await waitFor(() => expect(requestBody).toMatchObject({ expectedRevision: '7' }))
  })

  it('surfaces reply draft edit, approve, reject, and send failures', async () => {
    const workflow = {
      draftId: 'draft-1',
      feedbackId: 'f-draft-errors',
      cycleNo: 1,
      status: 'approved',
      activeRevisionId: 'rev-2',
      approvedRevisionId: 'rev-2',
      activeText: 'Human edited reply',
      allowedActions: ['edit', 'approve', 'send', 'reject', 'regenerate'],
      blockers: [],
      hookConfigured: true,
      approvedBy: 'member-1',
      revision: '13',
      updatedAt: '2026-07-03T10:00:00Z',
      revisions: [
        {
          id: 'rev-2',
          draftId: 'draft-1',
          cycleNo: 1,
          revisionNo: 2,
          origin: 'human',
          content: 'Human edited reply',
          createdBy: 'member-1',
          createdAt: '2026-07-03T10:00:00Z',
        },
        {
          id: 'rev-1',
          draftId: 'draft-1',
          cycleNo: 1,
          revisionNo: 1,
          origin: 'ai',
          content: 'AI suggested reply',
          createdBy: 'assistant',
          createdAt: '2026-07-03T09:55:00Z',
        },
      ],
      events: [],
    }

    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-draft-errors', {
            replyDraft: 'legacy projection',
            replyDraftWorkflow: workflow,
          }),
        ),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/edit', () =>
        HttpResponse.json({ message: 'edit denied' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/approve', () =>
        HttpResponse.json({ message: 'approve denied' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/reject', () =>
        HttpResponse.json({ message: 'reject denied' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/send', () =>
        HttpResponse.json({ message: 'send blocked' }, { status: 409 }),
      ),
    )

    renderWithProviders(
      <FeedbackDetailSheet
        id="f-draft-errors"
        dims={dims}
        availableTags={[]}
        onOpenChange={vi.fn()}
      />,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: /编辑/ })).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /编辑/ }))
    const editor = screen.getByRole('textbox')
    await userEvent.clear(editor)
    await userEvent.type(editor, 'Edited but rejected by server')
    await userEvent.click(screen.getByRole('button', { name: /保存/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('保存失败'))
    await userEvent.click(screen.getByRole('button', { name: /取消/ }))

    await userEvent.click(screen.getByRole('button', { name: /批准/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('批准失败'))

    await userEvent.click(screen.getByRole('button', { name: /拒绝/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('拒绝失败'))

    await userEvent.click(screen.getByRole('button', { name: '发送' }))
    await userEvent.click(await screen.findByRole('button', { name: /确认发送/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('当前草稿暂不可发送'))
  })

  it('labels sent text separately from the AI suggestion and human edit', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-sent', {
            replyDraft: 'legacy projection',
            replyDraftWorkflow: {
              draftId: 'draft-1',
              feedbackId: 'f-sent',
              cycleNo: 1,
              status: 'sent',
              activeRevisionId: 'rev-2',
              approvedRevisionId: 'rev-2',
              sentRevisionId: 'rev-2',
              activeText: 'Human edited reply',
              allowedActions: ['regenerate'],
              blockers: [],
              hookConfigured: true,
              revision: '4',
              updatedAt: '2026-07-03T10:05:00Z',
              revisions: [
                {
                  id: 'rev-2',
                  draftId: 'draft-1',
                  cycleNo: 1,
                  revisionNo: 2,
                  origin: 'human',
                  content: 'Human edited reply',
                  createdBy: 'member-1',
                  createdAt: '2026-07-03T10:00:00Z',
                },
                {
                  id: 'rev-1',
                  draftId: 'draft-1',
                  cycleNo: 1,
                  revisionNo: 1,
                  origin: 'ai',
                  content: 'AI suggested reply',
                  createdBy: 'assistant',
                  createdAt: '2026-07-03T09:55:00Z',
                },
              ],
              events: [],
            },
          }),
        ),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-sent" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )

    await waitFor(() => expect(screen.getByText('已发送文本')).toBeInTheDocument())
    expect(screen.getByText('AI 建议')).toBeInTheDocument()
    expect(screen.getByText('人工编辑')).toBeInTheDocument()
    expect(screen.getAllByText('已发送').length).toBeGreaterThanOrEqual(1)
  })

  it('shows send failure as a retryable workflow blocker', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-send-failed', {
            replyDraft: 'legacy projection',
            replyDraftWorkflow: {
              draftId: 'draft-1',
              feedbackId: 'f-send-failed',
              cycleNo: 1,
              status: 'send_failed',
              activeRevisionId: 'rev-2',
              approvedRevisionId: 'rev-2',
              activeText: 'Human edited reply',
              allowedActions: ['edit', 'send', 'reject', 'regenerate'],
              blockers: ['send_failed'],
              hookConfigured: true,
              externalDeliveryStatus: 'failed',
              revision: '5',
              updatedAt: '2026-07-03T10:07:00Z',
              revisions: [
                {
                  id: 'rev-2',
                  draftId: 'draft-1',
                  cycleNo: 1,
                  revisionNo: 2,
                  origin: 'human',
                  content: 'Human edited reply',
                  createdBy: 'member-1',
                  createdAt: '2026-07-03T10:00:00Z',
                },
                {
                  id: 'rev-1',
                  draftId: 'draft-1',
                  cycleNo: 1,
                  revisionNo: 1,
                  origin: 'ai',
                  content: 'AI suggested reply',
                  createdBy: 'assistant',
                  createdAt: '2026-07-03T09:55:00Z',
                },
              ],
              events: [],
            },
          }),
        ),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-send-failed"
        dims={dims}
        availableTags={[]}
        onOpenChange={vi.fn()}
      />,
    )

    await waitFor(() => expect(screen.getAllByText('发送失败').length).toBeGreaterThanOrEqual(1))
    expect(screen.getByText('上次发送失败，请重试或编辑后再发送')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /发送/ })).toBeInTheDocument()
  })

  it('shows send pending as a locked workflow state', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-send-pending', {
            replyDraftWorkflow: {
              draftId: 'draft-1',
              feedbackId: 'f-send-pending',
              cycleNo: 1,
              status: 'send_pending',
              activeRevisionId: 'rev-2',
              approvedRevisionId: 'rev-2',
              activeText: 'Human edited reply',
              allowedActions: [],
              blockers: ['send_in_progress'],
              hookConfigured: true,
              revision: '6',
              updatedAt: '2026-07-03T10:08:00Z',
              revisions: [
                {
                  id: 'rev-2',
                  draftId: 'draft-1',
                  cycleNo: 1,
                  revisionNo: 2,
                  origin: 'human',
                  content: 'Human edited reply',
                  createdBy: 'member-1',
                  createdAt: '2026-07-03T10:00:00Z',
                },
              ],
              events: [],
            },
          }),
        ),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-send-pending"
        dims={dims}
        availableTags={[]}
        onOpenChange={vi.fn()}
      />,
    )

    await waitFor(() => expect(screen.getByText('发送中')).toBeInTheDocument())
    expect(screen.getByText('回复正在发送中')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /发送/ })).toBeNull()
    expect(screen.queryByRole('button', { name: /重新生成|生成草稿/ })).toBeNull()
  })

  it('sends the current workflow revision when saving a human edit', async () => {
    let requestBody: Record<string, unknown> | null = null
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-edit-revision', {
            replyDraftWorkflow: {
              draftId: 'draft-1',
              feedbackId: 'f-edit-revision',
              cycleNo: 1,
              status: 'edited',
              activeRevisionId: 'rev-2',
              activeText: 'Human edited reply',
              allowedActions: ['edit', 'approve', 'reject', 'regenerate'],
              blockers: [],
              hookConfigured: true,
              revision: '11',
              updatedAt: '2026-07-03T10:00:00Z',
              revisions: [
                {
                  id: 'rev-2',
                  draftId: 'draft-1',
                  cycleNo: 1,
                  revisionNo: 2,
                  origin: 'human',
                  content: 'Human edited reply',
                  createdBy: 'member-1',
                  createdAt: '2026-07-03T10:00:00Z',
                },
              ],
              events: [],
            },
          }),
        ),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/edit', async ({ request }) => {
        requestBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({ workflow: { revision: '12' } })
      }),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-edit-revision"
        dims={dims}
        availableTags={[]}
        onOpenChange={vi.fn()}
      />,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: /编辑/ })).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /编辑/ }))
    const editor = screen.getByRole('textbox')
    await userEvent.clear(editor)
    await userEvent.type(editor, 'Updated human edit')
    await userEvent.click(screen.getByRole('button', { name: /保存/ }))

    await waitFor(() =>
      expect(requestBody).toMatchObject({
        content: 'Updated human edit',
        expectedRevision: '11',
      }),
    )
  })

  it('uses the latest successful workflow when actions happen back to back', async () => {
    let editBody: Record<string, unknown> | null = null
    const baseWorkflow = {
      draftId: 'draft-1',
      feedbackId: 'f-back-to-back',
      cycleNo: 1,
      activeRevisionId: 'rev-1',
      activeText: 'AI suggested reply',
      blockers: [],
      hookConfigured: true,
      revisions: [
        {
          id: 'rev-1',
          draftId: 'draft-1',
          cycleNo: 1,
          revisionNo: 1,
          origin: 'ai',
          content: 'AI suggested reply',
          createdBy: 'assistant',
          createdAt: '2026-07-03T09:55:00Z',
        },
      ],
      events: [],
    }
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-back-to-back', {
            replyDraftWorkflow: {
              ...baseWorkflow,
              status: 'suggested',
              allowedActions: ['edit', 'approve', 'reject', 'regenerate'],
              revision: '1',
              updatedAt: '2026-07-03T10:00:00Z',
            },
          }),
        ),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/approve', () =>
        HttpResponse.json({
          workflow: {
            ...baseWorkflow,
            status: 'approved',
            approvedRevisionId: 'rev-1',
            allowedActions: ['edit', 'send', 'reject', 'regenerate'],
            revision: '2',
            updatedAt: '2026-07-03T10:01:00Z',
          },
        }),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/edit', async ({ request }) => {
        editBody = (await request.json()) as Record<string, unknown>
        return HttpResponse.json({
          workflow: {
            ...baseWorkflow,
            status: 'edited',
            activeRevisionId: 'rev-2',
            activeText: 'Second edit after approval',
            allowedActions: ['edit', 'approve', 'reject', 'regenerate'],
            revision: '3',
            updatedAt: '2026-07-03T10:02:00Z',
            revisions: [
              {
                id: 'rev-2',
                draftId: 'draft-1',
                cycleNo: 1,
                revisionNo: 2,
                origin: 'human',
                content: 'Second edit after approval',
                createdBy: 'member-1',
                createdAt: '2026-07-03T10:02:00Z',
              },
              ...baseWorkflow.revisions,
            ],
            events: [],
          },
        })
      }),
    )

    renderWithProviders(
      <FeedbackDetailSheet
        id="f-back-to-back"
        dims={dims}
        availableTags={[]}
        onOpenChange={vi.fn()}
      />,
    )

    await waitFor(() => expect(screen.getByRole('button', { name: /批准/ })).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /批准/ }))
    await waitFor(() => expect(screen.getAllByText('已批准').length).toBeGreaterThanOrEqual(1))
    await userEvent.click(screen.getByRole('button', { name: /编辑/ }))
    const editor = screen.getByRole('textbox')
    await userEvent.clear(editor)
    await userEvent.type(editor, 'Second edit after approval')
    await userEvent.click(screen.getByRole('button', { name: /保存/ }))

    await waitFor(() =>
      expect(editBody).toMatchObject({
        content: 'Second edit after approval',
        expectedRevision: '2',
      }),
    )
    expect(
      (await screen.findAllByText('Second edit after approval')).length,
    ).toBeGreaterThanOrEqual(1)
    expect(screen.getAllByText('已编辑').length).toBeGreaterThanOrEqual(1)
  })

  it('regenerate updates the draft via the invalidated detail refetch', async () => {
    let posted = false
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(draftRow('f-4', { replyDraft: posted ? 'new draft' : 'old draft' })),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/regenerate', () => {
        posted = true
        return HttpResponse.json({
          replyDraft: 'new draft',
          replyDraftGeneratedAt: '2026-06-09T11:00:00Z',
        })
      }),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-4" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('old draft')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /重新生成/ }))
    // 'new draft' must come from the GET refetch (which now returns it),
    // proving invalidateQueries fired with the right key — not just the
    // mutation response.
    await waitFor(() => expect(screen.getByText('new draft')).toBeInTheDocument())
  })

  it('uses the workflow returned by a successful regenerate immediately', async () => {
    const baseWorkflow = {
      draftId: 'draft-regen',
      feedbackId: 'f-regenerate-workflow',
      cycleNo: 1,
      status: 'suggested',
      activeRevisionId: 'rev-1',
      activeText: 'old workflow draft',
      allowedActions: ['edit', 'approve', 'reject', 'regenerate'],
      blockers: [],
      hookConfigured: true,
      revision: '1',
      updatedAt: '2026-07-03T10:00:00Z',
      revisions: [
        {
          id: 'rev-1',
          draftId: 'draft-regen',
          cycleNo: 1,
          revisionNo: 1,
          origin: 'ai',
          content: 'old workflow draft',
          createdBy: 'assistant',
          createdAt: '2026-07-03T10:00:00Z',
        },
      ],
      events: [],
    }
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-regenerate-workflow', {
            replyDraftWorkflow: baseWorkflow,
          }),
        ),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/regenerate', () =>
        HttpResponse.json({
          workflow: {
            ...baseWorkflow,
            activeRevisionId: 'rev-2',
            activeText: 'regenerated workflow draft',
            revision: '2',
            updatedAt: '2026-07-03T10:01:00Z',
            revisions: [
              {
                id: 'rev-2',
                draftId: 'draft-regen',
                cycleNo: 1,
                revisionNo: 2,
                origin: 'ai',
                content: 'regenerated workflow draft',
                createdBy: 'assistant',
                createdAt: '2026-07-03T10:01:00Z',
              },
              ...baseWorkflow.revisions,
            ],
          },
          fromCache: false,
        }),
      ),
    )

    renderWithProviders(
      <FeedbackDetailSheet
        id="f-regenerate-workflow"
        dims={dims}
        availableTags={[]}
        onOpenChange={vi.fn()}
      />,
    )

    await waitFor(() =>
      expect(screen.getAllByText('old workflow draft').length).toBeGreaterThanOrEqual(1),
    )
    await userEvent.click(screen.getByRole('button', { name: /重新生成/ }))

    await waitFor(() =>
      expect(screen.getAllByText('regenerated workflow draft').length).toBeGreaterThanOrEqual(1),
    )
    expect(screen.getByText('rev 2')).toBeInTheDocument()
  })

  it('uses the workflow returned by a successful reject immediately', async () => {
    const baseWorkflow = {
      draftId: 'draft-reject',
      feedbackId: 'f-reject-workflow',
      cycleNo: 1,
      status: 'suggested',
      activeRevisionId: 'rev-1',
      activeText: 'rejectable workflow draft',
      allowedActions: ['edit', 'approve', 'reject', 'regenerate'],
      blockers: [],
      hookConfigured: true,
      revision: '1',
      updatedAt: '2026-07-03T10:00:00Z',
      revisions: [
        {
          id: 'rev-1',
          draftId: 'draft-reject',
          cycleNo: 1,
          revisionNo: 1,
          origin: 'ai',
          content: 'rejectable workflow draft',
          createdBy: 'assistant',
          createdAt: '2026-07-03T10:00:00Z',
        },
      ],
      events: [],
    }
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-reject-workflow', {
            replyDraftWorkflow: baseWorkflow,
          }),
        ),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/reject', () =>
        HttpResponse.json({
          workflow: {
            ...baseWorkflow,
            status: 'rejected',
            allowedActions: ['regenerate'],
            revision: '2',
            updatedAt: '2026-07-03T10:01:00Z',
          },
          fromCache: false,
        }),
      ),
    )

    renderWithProviders(
      <FeedbackDetailSheet
        id="f-reject-workflow"
        dims={dims}
        availableTags={[]}
        onOpenChange={vi.fn()}
      />,
    )

    await waitFor(() =>
      expect(screen.getAllByText('rejectable workflow draft').length).toBeGreaterThanOrEqual(1),
    )
    await userEvent.click(screen.getByRole('button', { name: /拒绝/ }))

    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('草稿已拒绝'))
    expect(screen.getByText('已拒绝')).toBeInTheDocument()
    expect(screen.getByText('rev 2')).toBeInTheDocument()
  })

  it('enabled-but-empty draft offers a Generate entry point and no copy button', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(draftRow('f-6', { replyDraft: '' })),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-6" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('回复草稿')).toBeInTheDocument())
    expect(screen.getByText('暂无草稿')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /生成草稿/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /复制/ })).toBeNull()
  })

  it('hides the draft section when reply-draft is disabled for the tenant', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(draftRow('f-7', { replyDraftEnabled: false, replyDraft: '' })),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-7" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('原始内容')).toBeInTheDocument())
    expect(screen.queryByText('回复草稿')).toBeNull()
  })

  it('copy toasts success only via the clipboard write resolving', async () => {
    // jsdom + userEvent install a resolving clipboard stub, so we assert the
    // success path runs through .then (success is not toasted unconditionally,
    // which was the bug). The failure path (.catch → error toast) is exercised
    // by the live e2e run and held by onCopy's then/catch structure.
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(draftRow('f-8', { replyDraft: 'a draft to copy' })),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-8" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('a draft to copy')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /复制/ }))
    await waitFor(() => expect(toast.success).toHaveBeenCalledWith('草稿已复制到剪贴板'))
  })

  it('shows a copy error toast when clipboard write fails', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(draftRow('f-copy-fail', { replyDraft: 'cannot copy this' })),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-copy-fail"
        dims={dims}
        availableTags={[]}
        onOpenChange={vi.fn()}
      />,
    )
    await waitFor(() => expect(screen.getByText('cannot copy this')).toBeInTheDocument())
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValueOnce(
      new Error('clipboard unavailable'),
    )
    await userEvent.click(screen.getByRole('button', { name: /复制/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('复制失败'))
  })

  it('regenerate failure shows an error toast', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(draftRow('f-9', { replyDraft: 'old' })),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/regenerate', () =>
        HttpResponse.json({ code: 'BAD_GATEWAY', message: 'failed' }, { status: 502 }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-9" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('old')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /重新生成/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('重新生成失败'))
  })

  it('shows a skeleton and disables the buttons while regenerating', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(draftRow('f-10', { replyDraft: 'old draft' })),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/regenerate', async () => {
        await delay('infinite') // hold the request open so the pending UI stays
        return HttpResponse.json({ replyDraft: 'unused', replyDraftGeneratedAt: '' })
      }),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-10" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('old draft')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /重新生成/ }))
    // Pending: the draft body becomes a skeleton and both buttons disable.
    await waitFor(() =>
      expect(document.querySelector('[data-slot="skeleton"]')).toBeInTheDocument(),
    )
    expect(screen.queryByText('old draft')).toBeNull()
    expect(screen.getByRole('button', { name: /复制/ })).toBeDisabled()
    expect(screen.getByRole('button', { name: /重新生成/ })).toBeDisabled()
  })

  it('swaps the copy button to a copied confirmation after copying', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(draftRow('f-11', { replyDraft: 'copy me' })),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-11" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('copy me')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /复制/ }))
    await waitFor(() => expect(screen.getByRole('button', { name: /已复制/ })).toBeInTheDocument())
  })

  it('resets the copied confirmation when the copy timer fires', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(draftRow('f-11b', { replyDraft: 'copy then reset' })),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-11b" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('copy then reset')).toBeInTheDocument())
    const setTimeoutSpy = vi.spyOn(window, 'setTimeout').mockImplementation(((
      handler: TimerHandler,
    ) => {
      if (typeof handler === 'function') handler()
      return 1
    }) as typeof window.setTimeout)
    try {
      await userEvent.click(screen.getByRole('button', { name: /复制/ }))
      expect(setTimeoutSpy).toHaveBeenCalledWith(expect.any(Function), 1500)
    } finally {
      setTimeoutSpy.mockRestore()
    }
    await waitFor(() => expect(screen.getByRole('button', { name: /^复制$/ })).toBeInTheDocument())
  })

  it('renders the generated-at provenance when the draft carries a timestamp', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(
          draftRow('f-12', {
            replyDraft: 'drafted',
            replyDraftGeneratedAt: '2026-06-13T10:00:00Z',
          }),
        ),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-12" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('drafted')).toBeInTheDocument())
    expect(screen.getByText(/生成于/)).toBeInTheDocument()
  })

  it('regenerate cooldown (429) shows the distinct cooldown toast', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json(draftRow('f-13', { replyDraft: 'old' })),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/regenerate', () =>
        HttpResponse.json({ code: 'RATE_LIMITED', message: 'cooldown' }, { status: 429 }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet id="f-13" dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    await waitFor(() => expect(screen.getByText('old')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /重新生成/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('操作过于频繁，请稍候再试'))
  })
})

describe('retry enrichment', () => {
  it('shows retry button for terminal failures and triggers success toast', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-term',
          content: 'terminal failure content',
          enrichedTitle: '',
          enrichedAttrs: {},
          language: 'en',
          isUrgent: false,
          source: 'web',
          userId: 'u-1',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: null,
          attachments: [],
          enrichmentStatus: 'failed',
          enrichmentError: 'LLM provider timeout',
          enrichmentAttempts: 5,
          enrichmentNextRetryAt: null,
        }),
      ),
      http.post('/fb/v1/console/feedback/:id/retry-enrichment', () =>
        HttpResponse.json({
          id: 'f-term',
          enrichmentStatus: 'failed',
          enrichmentAttempts: 0,
          enrichmentNextRetryAt: '2026-06-22T10:00:00Z',
        }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-term"
        dims={dims}
        availableTags={[]}
        workbenchMode="terminal"
        onOpenChange={vi.fn()}
      />,
    )
    await waitFor(() => expect(screen.getByText(/LLM provider timeout/)).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /重试/ }))
    await waitFor(() => expect(toast.success).toHaveBeenCalled())
  })

  it('shows error toast when retry fails', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-fail',
          content: 'retry fail content',
          enrichedTitle: '',
          enrichedAttrs: {},
          language: 'en',
          isUrgent: false,
          source: 'web',
          userId: 'u-1',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: null,
          attachments: [],
          enrichmentStatus: 'failed',
          enrichmentError: 'API error',
          enrichmentAttempts: 5,
          enrichmentNextRetryAt: null,
        }),
      ),
      http.post('/fb/v1/console/feedback/:id/retry-enrichment', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'server error' }, { status: 500 }),
      ),
    )
    renderWithProviders(
      <FeedbackDetailSheet
        id="f-fail"
        dims={dims}
        availableTags={[]}
        workbenchMode="terminal"
        onOpenChange={vi.fn()}
      />,
    )
    await waitFor(() => expect(screen.getByText(/API error/)).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /重试/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalled())
  })
})

// draftRow builds a feedback detail JSON with reply-draft enabled by default.
function draftRow(id: string, over: Record<string, unknown> = {}) {
  return {
    id,
    content: 'app crashes on login',
    enrichedTitle: 'Crash',
    enrichedAttrs: {},
    language: 'en',
    isUrgent: false,
    source: 'web',
    userId: 'u-1',
    pageUrl: '',
    createdAt: '2026-06-07T10:00:00Z',
    sourceMeta: undefined,
    attachments: [],
    enrichmentError: '',
    replyDraftEnabled: true,
    ...over,
  }
}

function feedbackRow(id: string, over: Partial<FeedbackDetail> = {}): FeedbackDetail {
  return {
    id,
    content: 'Need CSV export',
    type: 'feature',
    enrichedTitle: 'CSV export',
    enrichedDisplayTitle: 'CSV export',
    enrichedRationale: 'Enterprise customers need export bundles.',
    enrichedDisplayRationale: 'Enterprise customers need export bundles.',
    enrichedDisplayLocale: 'en',
    enrichedAttrs: { severity: 'P1' },
    enrichedAt: '2026-07-07T10:30:00Z',
    language: 'en',
    isUrgent: false,
    source: 'web',
    userId: 'u-42',
    pageUrl: '',
    createdAt: '2026-07-07T10:00:00Z',
    sourceMeta: undefined,
    attachments: [],
    enrichmentError: '',
    enrichmentStatus: 'done',
    replyDraftEnabled: true,
    tags: [],
    allowedNextStates: [],
    ...over,
  }
}

function customerRequestSummary(
  overrides: Partial<CustomerRequestSummary> = {},
): CustomerRequestSummary {
  return {
    id: '11111111-1111-1111-1111-111111111111',
    displayNumber: '9',
    displayId: 'CR-9',
    title: 'Export bundles',
    status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
    priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
    createdAt: '2026-07-07T00:00:00Z',
    updatedAt: '2026-07-07T01:00:00Z',
    firstFeedbackAt: '2026-07-07T00:10:00Z',
    latestFeedbackAt: '2026-07-07T00:10:00Z',
    supportingFeedbackCount: 1,
    customerCount: 1,
    accountCount: 1,
    linkedIssueCount: 0,
    voteCount: 0,
    duplicateRequestCount: 0,
    hiddenFeedbackCount: 0,
    revenueImpactCents: '0',
    revenueCurrency: 'USD',
    decisionScore: 67,
    decisionScoreExplanation:
      'priority=high feedback=1 customers=1 accounts=1 votes=0 revenue_cents=0 delivery_health=no_links',
    deliveryHealth: CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_NO_LINKS,
    syncedIssueCount: 0,
    staleIssueCount: 0,
    failedIssueCount: 0,
    pendingIssueCount: 0,
    manualIssueCount: 0,
    ...overrides,
  }
}

function customerRequestDetail(request: CustomerRequestSummary) {
  return {
    request,
    description: 'Bundle customer evidence for delivery planning.',
    feedback: [],
    issueLinks: [],
    auditEntries: [],
    customers: [],
    votes: [],
    notes: [],
    duplicates: [],
    accountProfiles: [],
  }
}
