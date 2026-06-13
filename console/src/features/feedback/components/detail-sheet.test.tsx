import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { FeedbackDetailSheet } from '@/features/feedback/components/detail-sheet'
import type { Dimension } from '@/proto/attune/v1/common'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

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
  it('id=null renders nothing (sheet closed)', () => {
    renderWithProviders(<FeedbackDetailSheet id={null} dims={dims} onOpenChange={vi.fn()} />)
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
    renderWithProviders(<FeedbackDetailSheet id="f-1" dims={dims} onOpenChange={vi.fn()} />)
    // Title, raw content, AI rationale all appear once the query resolves.
    await waitFor(() => expect(screen.getByText('支付失败')).toBeInTheDocument())
    expect(screen.getByText(/payment failed at checkout/i)).toBeInTheDocument()
    expect(screen.getByText(/AI 中文解读/i)).toBeInTheDocument()
    expect(screen.getByText(/AI rationale/i)).toBeInTheDocument()
    expect(screen.getByTitle('原文语言：英文')).toBeInTheDocument()
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
    renderWithProviders(<FeedbackDetailSheet id="f-2" dims={dims} onOpenChange={vi.fn()} />)
    await waitFor(() => expect(screen.getByText('展示侧：支付流程受阻')).toBeInTheDocument())
    expect(screen.queryByText('原语言解读')).toBeNull()
    expect(screen.queryByText('原文侧：支付流程受阻')).toBeNull()
  })

  it('shows the reply draft with copy + regenerate when present', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-3',
          content: 'app crashes on login',
          enrichedTitle: 'Crash',
          enrichedAttrs: {},
          replyDraft: 'Sorry to hear that — we are investigating and will update you shortly.',
          replyDraftGeneratedAt: '2026-06-09T10:00:00Z',
          language: 'en',
          isUrgent: false,
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
    renderWithProviders(<FeedbackDetailSheet id="f-3" dims={dims} onOpenChange={vi.fn()} />)
    await waitFor(() => expect(screen.getByText(/Sorry to hear that/i)).toBeInTheDocument())
    expect(screen.getByText('回复草稿')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /复制/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /重新生成/ })).toBeInTheDocument()
  })

  it('regenerate posts and shows the new draft', async () => {
    server.use(
      http.get('/fb/v1/console/feedback/:id', () =>
        HttpResponse.json({
          id: 'f-4',
          content: 'x',
          enrichedAttrs: {},
          replyDraft: 'old draft',
          language: 'en',
          isUrgent: false,
          source: 'web',
          userId: '',
          pageUrl: '',
          createdAt: '2026-06-07T10:00:00Z',
          sourceMeta: null,
          attachments: [],
          enrichmentError: '',
        }),
      ),
      http.post('/fb/v1/console/feedback/:id/reply-draft/regenerate', () =>
        HttpResponse.json({
          replyDraft: 'new draft',
          replyDraftGeneratedAt: '2026-06-09T11:00:00Z',
        }),
      ),
    )
    renderWithProviders(<FeedbackDetailSheet id="f-4" dims={dims} onOpenChange={vi.fn()} />)
    await waitFor(() => expect(screen.getByText('old draft')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /重新生成/ }))
    await waitFor(() => expect(screen.getByText('new draft')).toBeInTheDocument())
  })
})
