import userEvent from '@testing-library/user-event'
import { delay, HttpResponse, http } from 'msw'
import { useState } from 'react'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { FeedbackDetailSheet } from '@/features/feedback/components/detail-sheet'
import type { Dimension } from '@/proto/attune/v1/common'
import { expectNoA11yViolations } from '@/testing/a11y'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

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

describe('FeedbackDetailSheet', () => {
  beforeEach(() => {
    vi.mocked(toast.success).mockClear()
    vi.mocked(toast.error).mockClear()
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: vi.fn().mockResolvedValue(undefined) },
      configurable: true,
    })
  })

  it('id=null renders nothing (sheet closed)', () => {
    renderWithProviders(
      <FeedbackDetailSheet id={null} dims={dims} availableTags={[]} onOpenChange={vi.fn()} />,
    )
    // No urgent dot, no title rendered.
    expect(screen.queryByText(/payment failed/i)).toBeNull()
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

    await userEvent.click(screen.getByTitle('复制'))
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
      return (
        <>
          <button type="button" onClick={() => setId('f-focus')}>
            Open feedback detail
          </button>
          <FeedbackDetailSheet
            id={id}
            dims={dims}
            availableTags={[]}
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
    await userEvent.click(screen.getByRole('button', { name: /确认发送/ }))

    await waitFor(() => expect(requestBody).toMatchObject({ expectedRevision: '7' }))
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
    sourceMeta: null,
    attachments: [],
    enrichmentError: '',
    replyDraftEnabled: true,
    ...over,
  }
}
