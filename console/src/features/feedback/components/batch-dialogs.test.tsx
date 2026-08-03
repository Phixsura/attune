import { describe, expect, it, vi } from 'vitest'
import { AssignmentRecommendationDialog } from '@/features/feedback/components/assignment-recommendation-dialog'
import { BatchAssignmentDialog } from '@/features/feedback/components/batch-assignment-dialog'
import { BatchConfirmDialog } from '@/features/feedback/components/batch-confirm-dialog'
import { BatchDeleteDialog } from '@/features/feedback/components/batch-delete-dialog'
import { DeleteFeedbackDialog } from '@/features/feedback/components/delete-feedback-dialog'
import { RetryEnrichmentDialog } from '@/features/feedback/components/retry-enrichment-dialog'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('BatchDeleteDialog', () => {
  const defaultProps = {
    open: true,
    onOpenChange: vi.fn(),
    count: 5,
    isHardDelete: false,
    onConfirm: vi.fn(),
  }

  it('shows soft delete message by default', () => {
    renderWithProviders(<BatchDeleteDialog {...defaultProps} />)
    expect(screen.getByText('删除反馈')).toBeInTheDocument()
    expect(screen.getByText('确定要删除这 5 条反馈吗？此操作可撤销。')).toBeInTheDocument()
  })

  it('shows hard delete warning when isHardDelete', () => {
    renderWithProviders(<BatchDeleteDialog {...defaultProps} isHardDelete />)
    expect(screen.getByText('永久删除反馈')).toBeInTheDocument()
    expect(screen.getByText('确定要永久删除这 5 条反馈吗？此操作不可撤销。')).toBeInTheDocument()
  })

  it('calls onConfirm when confirmed', async () => {
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <BatchDeleteDialog {...defaultProps} onConfirm={onConfirm} />,
    )
    const confirmBtn = screen.getByTestId('batch-delete-confirm')
    await user.click(confirmBtn)
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('disables buttons when loading', () => {
    renderWithProviders(<BatchDeleteDialog {...defaultProps} isLoading />)
    const confirmBtn = screen.getByTestId('batch-delete-confirm')
    const cancelBtn = screen.getByRole('button', { name: /取消/ })
    expect(confirmBtn).toBeDisabled()
    expect(cancelBtn).toBeDisabled()
  })

  it('shows deleting text when loading', () => {
    renderWithProviders(<BatchDeleteDialog {...defaultProps} isLoading />)
    expect(screen.getByText('删除中…')).toBeInTheDocument()
  })

  it('does not render content when closed', () => {
    renderWithProviders(<BatchDeleteDialog {...defaultProps} open={false} />)
    expect(screen.queryByText('删除反馈')).not.toBeInTheDocument()
  })
})

describe('BatchConfirmDialog', () => {
  const defaultProps = {
    open: true,
    onOpenChange: vi.fn(),
    operation: 'tag' as const,
    matchedCount: 5,
    onConfirm: vi.fn(),
  }

  it('shows count mismatch warning when counts differ', () => {
    renderWithProviders(
      <BatchConfirmDialog {...defaultProps} matchedCount={10} expectedCount={5} />,
    )
    expect(
      screen.getByText('注意：服务端匹配到 10 条，与预期的 5 条不符。请重新确认筛选条件。'),
    ).toBeInTheDocument()
  })

  it('disables confirm button when counts mismatch', () => {
    renderWithProviders(
      <BatchConfirmDialog {...defaultProps} matchedCount={10} expectedCount={5} />,
    )
    const confirmBtn = screen.getByTestId('batch-confirm-action')
    expect(confirmBtn).toBeDisabled()
  })

  it('enables confirm button when counts match', () => {
    renderWithProviders(<BatchConfirmDialog {...defaultProps} matchedCount={5} expectedCount={5} />)
    const confirmBtn = screen.getByTestId('batch-confirm-action')
    expect(confirmBtn).not.toBeDisabled()
  })

  it('shows tag operation title', () => {
    renderWithProviders(<BatchConfirmDialog {...defaultProps} operation="tag" />)
    expect(screen.getByText('批量修改标签')).toBeInTheDocument()
  })

  it('shows workflow operation title', () => {
    renderWithProviders(<BatchConfirmDialog {...defaultProps} operation="workflow" />)
    expect(screen.getByText('批量流转状态')).toBeInTheDocument()
  })

  it('shows delete operation title', () => {
    renderWithProviders(<BatchConfirmDialog {...defaultProps} operation="delete" />)
    expect(screen.getByText('批量删除反馈')).toBeInTheDocument()
  })

  it('calls onConfirm when confirmed', async () => {
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <BatchConfirmDialog {...defaultProps} onConfirm={onConfirm} />,
    )
    const confirmBtn = screen.getByTestId('batch-confirm-action')
    await user.click(confirmBtn)
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('disables buttons when loading', () => {
    renderWithProviders(<BatchConfirmDialog {...defaultProps} isLoading />)
    const confirmBtn = screen.getByTestId('batch-confirm-action')
    const cancelBtn = screen.getByRole('button', { name: /取消/ })
    expect(confirmBtn).toBeDisabled()
    expect(cancelBtn).toBeDisabled()
  })

  it('shows processing text when loading', () => {
    renderWithProviders(<BatchConfirmDialog {...defaultProps} isLoading />)
    expect(screen.getByText('处理中…')).toBeInTheDocument()
  })

  it('does not render content when closed', () => {
    renderWithProviders(<BatchConfirmDialog {...defaultProps} open={false} />)
    expect(screen.queryByText('批量修改标签')).not.toBeInTheDocument()
  })
})

describe('BatchAssignmentDialog', () => {
  const defaultProps = {
    open: true,
    count: 2,
    members: [
      {
        id: 'member-1',
        memberType: 'tenant_user',
        userId: 'owner-1',
        email: 'owner@example.com',
        role: 'member',
        roleSource: 'manual',
        invitedAt: '0',
        acceptedAt: '1',
      },
    ],
    isMembersLoading: false,
    isLoading: false,
    onConfirm: vi.fn(),
    onCancel: vi.fn(),
  }

  it('keeps apply disabled until a bulk assignment intent is selected', () => {
    renderWithProviders(<BatchAssignmentDialog {...defaultProps} />)
    expect(screen.getByRole('button', { name: '应用分派' })).toBeDisabled()
  })

  it('submits owner, SLA, and handoff note changes', async () => {
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <BatchAssignmentDialog {...defaultProps} onConfirm={onConfirm} />,
    )

    await user.click(screen.getByLabelText('负责人'))
    await user.click(screen.getByRole('option', { name: 'owner@example.com' }))
    await user.click(screen.getByLabelText('SLA'))
    await user.click(screen.getByRole('option', { name: '设置 SLA' }))
    await user.type(screen.getByLabelText('截止时间'), '2026-08-02T09:30')
    await user.type(screen.getByLabelText('交接备注'), 'Enterprise escalation')
    await user.click(screen.getByRole('button', { name: '应用分派' }))

    expect(onConfirm).toHaveBeenCalledWith({
      ownerMemberIdSet: true,
      ownerMemberId: 'member-1',
      slaDueAtSet: true,
      slaDueAt: expect.stringMatching(/^2026-08-02T.*Z$/),
      note: 'Enterprise escalation',
    })
  })
})

describe('AssignmentRecommendationDialog', () => {
  const defaultProps = {
    open: true,
    count: 2,
    response: {
      totalMatched: 2,
      recommendations: [
        {
          feedbackId: '101',
          ruleKey: 'urgent_open',
          ruleName: 'Urgent open feedback',
          ownerLane: 'support_triage',
          severity: 'critical',
          slaHours: 24,
          recommendedSlaDueAt: '2026-08-02T09:30:00Z',
          rationale: 'urgent',
          alreadySatisfied: false,
        },
      ],
      failed: [],
    },
    members: [
      {
        id: 'member-1',
        memberType: 'tenant_user',
        userId: 'owner-1',
        email: 'owner@example.com',
        role: 'member',
        roleSource: 'manual',
        invitedAt: '0',
        acceptedAt: '1',
      },
    ],
    isMembersLoading: false,
    isPreviewLoading: false,
    isApplying: false,
    onConfirm: vi.fn(),
    onCancel: vi.fn(),
  }

  it('keeps apply disabled when no recommendations match', () => {
    renderWithProviders(
      <AssignmentRecommendationDialog
        {...defaultProps}
        response={{ totalMatched: 1, recommendations: [], failed: [] }}
      />,
    )
    expect(screen.getByRole('button', { name: '应用建议' })).toBeDisabled()
  })

  it('submits owner override and policy note', async () => {
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <AssignmentRecommendationDialog {...defaultProps} onConfirm={onConfirm} />,
    )

    await user.click(screen.getByLabelText('实际负责人'))
    await user.click(screen.getByRole('option', { name: 'owner@example.com' }))
    await user.type(screen.getByLabelText('应用备注'), 'Policy sweep')
    await user.click(screen.getByRole('button', { name: '应用建议' }))

    expect(onConfirm).toHaveBeenCalledWith({
      ownerMemberId: 'member-1',
      note: 'Policy sweep',
    })
  })
})

describe('RetryEnrichmentDialog', () => {
  const defaultProps = {
    open: true,
    count: 3,
    isLoading: false,
    onConfirm: vi.fn(),
    onCancel: vi.fn(),
  }

  it('shows retry title and count', () => {
    renderWithProviders(<RetryEnrichmentDialog {...defaultProps} />)
    expect(screen.getByText('重试 AI 富化')).toBeInTheDocument()
  })

  it('calls onConfirm when retry clicked', async () => {
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <RetryEnrichmentDialog {...defaultProps} onConfirm={onConfirm} />,
    )
    const retryBtn = screen.getByRole('button', { name: /重试/ })
    await user.click(retryBtn)
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('calls onCancel when cancel clicked', async () => {
    const onCancel = vi.fn()
    const { user } = renderWithProviders(
      <RetryEnrichmentDialog {...defaultProps} onCancel={onCancel} />,
    )
    const cancelBtn = screen.getByRole('button', { name: /取消/ })
    await user.click(cancelBtn)
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('disables buttons when loading', () => {
    renderWithProviders(<RetryEnrichmentDialog {...defaultProps} isLoading />)
    const retryBtn = screen.getByRole('button', { name: /处理中/ })
    const cancelBtn = screen.getByRole('button', { name: /取消/ })
    expect(retryBtn).toBeDisabled()
    expect(cancelBtn).toBeDisabled()
  })

  it('shows processing text when loading', () => {
    renderWithProviders(<RetryEnrichmentDialog {...defaultProps} isLoading />)
    expect(screen.getByText(/处理中/)).toBeInTheDocument()
  })

  it('does not render content when closed', () => {
    renderWithProviders(<RetryEnrichmentDialog {...defaultProps} open={false} />)
    expect(screen.queryByText('重试 AI 富化')).not.toBeInTheDocument()
  })
})

describe('DeleteFeedbackDialog', () => {
  const defaultProps = {
    open: true,
    count: 5,
    isLoading: false,
    onConfirm: vi.fn(),
    onCancel: vi.fn(),
  }

  it('shows delete title and warning', () => {
    renderWithProviders(<DeleteFeedbackDialog {...defaultProps} />)
    expect(screen.getByText('永久删除反馈')).toBeInTheDocument()
  })

  it('calls onConfirm when delete clicked', async () => {
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <DeleteFeedbackDialog {...defaultProps} onConfirm={onConfirm} />,
    )
    const deleteBtn = screen.getByRole('button', { name: /删除$/ })
    await user.click(deleteBtn)
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('calls onCancel when cancel clicked', async () => {
    const onCancel = vi.fn()
    const { user } = renderWithProviders(
      <DeleteFeedbackDialog {...defaultProps} onCancel={onCancel} />,
    )
    const cancelBtn = screen.getByRole('button', { name: /取消/ })
    await user.click(cancelBtn)
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('disables buttons when loading', () => {
    renderWithProviders(<DeleteFeedbackDialog {...defaultProps} isLoading />)
    const deleteBtn = screen.getByRole('button', { name: /删除中/ })
    const cancelBtn = screen.getByRole('button', { name: /取消/ })
    expect(deleteBtn).toBeDisabled()
    expect(cancelBtn).toBeDisabled()
  })

  it('shows deleting text when loading', () => {
    renderWithProviders(<DeleteFeedbackDialog {...defaultProps} isLoading />)
    expect(screen.getByText(/删除中/)).toBeInTheDocument()
  })

  it('does not render content when closed', () => {
    renderWithProviders(<DeleteFeedbackDialog {...defaultProps} open={false} />)
    expect(screen.queryByText('永久删除反馈')).not.toBeInTheDocument()
  })
})
