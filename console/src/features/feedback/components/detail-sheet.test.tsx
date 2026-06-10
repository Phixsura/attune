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
          enrichedRationale: 'AI rationale',
          enrichedAttrs: { severity: 'P0' },
          enrichedAt: '2026-06-07T10:30:00Z',
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
    await waitFor(() => expect(screen.getByText('Payment failed')).toBeInTheDocument())
    expect(screen.getByText(/payment failed at checkout/i)).toBeInTheDocument()
    expect(screen.getByText(/AI rationale/i)).toBeInTheDocument()
  })
})
