import { describe, expect, it, vi } from 'vitest'
import { SelectionActionBar } from '@/features/feedback/components/selection-action-bar'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('SelectionActionBar', () => {
  const defaultProps = {
    count: 5,
    availableTags: [],
    removableTags: [],
    onBatchAdd: vi.fn(),
    onBatchRemove: vi.fn(),
    onCancel: vi.fn(),
  }

  it('renders null when count is 0', () => {
    const { container } = renderWithProviders(<SelectionActionBar {...defaultProps} count={0} />)
    expect(container.firstChild).toBeNull()
  })

  it('renders selected count', () => {
    renderWithProviders(<SelectionActionBar {...defaultProps} count={5} />)
    expect(screen.getByText('已选 5 条')).toBeInTheDocument()
  })

  it('shows delete button when onBatchDelete provided', async () => {
    const onBatchDelete = vi.fn()
    const { user } = renderWithProviders(
      <SelectionActionBar {...defaultProps} onBatchDelete={onBatchDelete} />,
    )
    const deleteBtn = screen.getByRole('button', { name: /删除/ })
    await user.click(deleteBtn)
    expect(onBatchDelete).toHaveBeenCalledTimes(1)
  })

  it('does not show delete button when onBatchDelete not provided', () => {
    renderWithProviders(<SelectionActionBar {...defaultProps} />)
    expect(screen.queryByRole('button', { name: /删除/ })).not.toBeInTheDocument()
  })

  it('shows loading state with message', () => {
    renderWithProviders(
      <SelectionActionBar {...defaultProps} isLoading loadingMessage="处理中..." />,
    )
    expect(screen.getByText('处理中...')).toBeInTheDocument()
  })

  it('shows default loading message when none provided', () => {
    renderWithProviders(<SelectionActionBar {...defaultProps} isLoading />)
    expect(screen.getByText('加载中…')).toBeInTheDocument()
  })

  it('shows select all when totalMatching > count', async () => {
    const onSelectAll = vi.fn()
    const { user } = renderWithProviders(
      <SelectionActionBar
        {...defaultProps}
        count={5}
        totalMatching={100}
        onSelectAll={onSelectAll}
      />,
    )
    const selectAllBtn = screen.getByText('全选符合条件的 100 条')
    await user.click(selectAllBtn)
    expect(onSelectAll).toHaveBeenCalledTimes(1)
  })

  it('does not show select all when totalMatching equals count', () => {
    renderWithProviders(
      <SelectionActionBar {...defaultProps} count={5} totalMatching={5} onSelectAll={vi.fn()} />,
    )
    expect(screen.queryByText(/全选符合条件/)).not.toBeInTheDocument()
  })

  it('does not show select all when onSelectAll not provided', () => {
    renderWithProviders(<SelectionActionBar {...defaultProps} count={5} totalMatching={100} />)
    expect(screen.queryByText(/全选符合条件/)).not.toBeInTheDocument()
  })

  it('calls onCancel when cancel button clicked', async () => {
    const onCancel = vi.fn()
    const { user } = renderWithProviders(
      <SelectionActionBar {...defaultProps} onCancel={onCancel} />,
    )
    const cancelBtn = screen.getByRole('button', { name: /取消/ })
    await user.click(cancelBtn)
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('shows cancel button in loading state', async () => {
    const onCancel = vi.fn()
    const { user } = renderWithProviders(
      <SelectionActionBar {...defaultProps} isLoading onCancel={onCancel} />,
    )
    const cancelBtn = screen.getByRole('button', { name: /取消/ })
    await user.click(cancelBtn)
    expect(onCancel).toHaveBeenCalledTimes(1)
  })
})
