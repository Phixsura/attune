import { describe, expect, it, vi } from 'vitest'
import { AssignmentEscalationQueue } from '@/features/feedback/components/assignment-escalation-queue'
import type { FeedbackAssignmentEscalationQueue } from '@/proto/attune/v1/ingest'
import { renderWithProviders, screen } from '@/testing/test-utils'

const queue: FeedbackAssignmentEscalationQueue = {
  generatedAt: '2026-08-01T12:00:00Z',
  overdueCount: '2',
  dueSoonCount: '1',
  missingOwnerCount: '1',
  missingSlaCount: '1',
  items: [
    {
      feedbackId: '42',
      title: 'Enterprise login regression',
      source: 'portal',
      type: 'bug',
      isUrgent: true,
      createdAt: '2026-07-31T12:00:00Z',
      assignment: {
        feedbackId: '42',
        assignedBy: '',
        slaDueAt: '2026-08-01T10:00:00Z',
        slaStatus: 'overdue',
        note: 'Escalate before renewal call.',
      },
      escalationReasons: ['overdue', 'missing_owner'],
      hoursUntilDue: -2,
      priority: 'critical',
      accountContext: {
        accountKey: 'acct:acme',
        accountDisplay: 'Acme Corp',
        source: 'source_meta',
      },
    },
  ],
}

describe('AssignmentEscalationQueue', () => {
  it('renders SLA escalation metrics and opens a feedback item', async () => {
    const onOpenFeedback = vi.fn()
    const { user } = renderWithProviders(
      <AssignmentEscalationQueue
        data={queue}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={onOpenFeedback}
      />,
    )

    expect(screen.getByText('需要立即处理的责任缺口')).toBeInTheDocument()
    expect(screen.getByText('Enterprise login regression')).toBeInTheDocument()
    expect(screen.getByText('账户 Acme Corp')).toBeInTheDocument()
    expect(screen.getByText('逾期 2 小时')).toBeInTheDocument()
    expect(screen.getAllByText('缺少负责人').length).toBeGreaterThanOrEqual(1)

    await user.click(screen.getByRole('button', { name: '打开反馈 #42' }))
    expect(onOpenFeedback).toHaveBeenCalledWith('42', expect.any(HTMLButtonElement))
  })

  it('renders an accountable empty state', () => {
    renderWithProviders(
      <AssignmentEscalationQueue
        data={{
          generatedAt: '2026-08-01T12:00:00Z',
          overdueCount: '0',
          dueSoonCount: '0',
          missingOwnerCount: '0',
          missingSlaCount: '0',
          items: [],
        }}
        isLoading={false}
        isError={false}
        onRetry={vi.fn()}
        onOpenFeedback={vi.fn()}
      />,
    )

    expect(screen.getByText('当前没有 SLA 升级项')).toBeInTheDocument()
    expect(screen.getByText(/开放反馈都有负责人/)).toBeInTheDocument()
  })
})
