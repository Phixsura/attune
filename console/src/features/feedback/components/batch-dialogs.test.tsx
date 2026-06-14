import { describe, expect, it, vi } from 'vitest'
import { BatchConfirmDialog } from '@/features/feedback/components/batch-confirm-dialog'
import { BatchDeleteDialog } from '@/features/feedback/components/batch-delete-dialog'
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
