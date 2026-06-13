import userEvent from '@testing-library/user-event'
import { delay, HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { FeedbackDetailSheet } from '@/features/feedback/components/detail-sheet'
import type { Dimension } from '@/proto/attune/v1/common'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

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
        HttpResponse.json(
          draftRow('f-3', { replyDraft: 'Sorry to hear that — we are investigating.' }),
        ),
      ),
    )
    renderWithProviders(<FeedbackDetailSheet id="f-3" dims={dims} onOpenChange={vi.fn()} />)
    await waitFor(() => expect(screen.getByText(/Sorry to hear that/i)).toBeInTheDocument())
    expect(screen.getByText('回复草稿')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /复制/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /重新生成/ })).toBeInTheDocument()
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
    renderWithProviders(<FeedbackDetailSheet id="f-4" dims={dims} onOpenChange={vi.fn()} />)
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
    renderWithProviders(<FeedbackDetailSheet id="f-6" dims={dims} onOpenChange={vi.fn()} />)
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
    renderWithProviders(<FeedbackDetailSheet id="f-7" dims={dims} onOpenChange={vi.fn()} />)
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
    renderWithProviders(<FeedbackDetailSheet id="f-8" dims={dims} onOpenChange={vi.fn()} />)
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
    renderWithProviders(<FeedbackDetailSheet id="f-9" dims={dims} onOpenChange={vi.fn()} />)
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
    renderWithProviders(<FeedbackDetailSheet id="f-10" dims={dims} onOpenChange={vi.fn()} />)
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
    renderWithProviders(<FeedbackDetailSheet id="f-11" dims={dims} onOpenChange={vi.fn()} />)
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
    renderWithProviders(<FeedbackDetailSheet id="f-12" dims={dims} onOpenChange={vi.fn()} />)
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
    renderWithProviders(<FeedbackDetailSheet id="f-13" dims={dims} onOpenChange={vi.fn()} />)
    await waitFor(() => expect(screen.getByText('old')).toBeInTheDocument())
    await userEvent.click(screen.getByRole('button', { name: /重新生成/ }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('操作过于频繁，请稍候再试'))
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
