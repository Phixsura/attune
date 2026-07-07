import { describe, expect, it, vi } from 'vitest'
import { SelectionActionBar } from '@/features/feedback/components/selection-action-bar'
import type { WorkflowState } from '@/proto/attune/v1/workflow'
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

  it('shows customer request promotion when handler is provided', async () => {
    const onPromoteToCustomerRequest = vi.fn()
    const { user } = renderWithProviders(
      <SelectionActionBar
        {...defaultProps}
        onPromoteToCustomerRequest={onPromoteToCustomerRequest}
      />,
    )
    await user.click(screen.getByRole('button', { name: /从反馈提升/ }))
    expect(onPromoteToCustomerRequest).toHaveBeenCalledTimes(1)
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

  describe('workflow transition', () => {
    const workflowStates: WorkflowState[] = [
      {
        id: 'state-1',
        name: 'Open',
        displayName: { entries: { en: 'Open', 'zh-CN': '待处理' } },
        color: '#22c55e',
        category: 'active',
        position: 0,
        isDefault: true,
        archived: false,
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
      },
      {
        id: 'state-2',
        name: 'Closed',
        displayName: { entries: { en: 'Closed', 'zh-CN': '已关闭' } },
        color: '#ef4444',
        category: 'done',
        position: 1,
        isDefault: false,
        archived: false,
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
      },
      {
        id: 'state-3',
        name: 'Archived',
        displayName: { entries: { en: 'Archived' } },
        color: '#gray',
        category: 'done',
        position: 2,
        isDefault: false,
        archived: true,
        createdAt: '2026-01-01T00:00:00Z',
        updatedAt: '2026-01-01T00:00:00Z',
      },
    ]

    it('shows transition select when workflowStates and onBatchTransition provided', () => {
      renderWithProviders(
        <SelectionActionBar
          {...defaultProps}
          workflowStates={workflowStates}
          onBatchTransition={vi.fn()}
        />,
      )
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })

    it('does not show transition select when workflowStates empty', () => {
      renderWithProviders(
        <SelectionActionBar {...defaultProps} workflowStates={[]} onBatchTransition={vi.fn()} />,
      )
      expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    })

    it('does not show transition select when onBatchTransition not provided', () => {
      renderWithProviders(<SelectionActionBar {...defaultProps} workflowStates={workflowStates} />)
      expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    })

    it('filters out archived states', () => {
      renderWithProviders(
        <SelectionActionBar
          {...defaultProps}
          workflowStates={workflowStates}
          onBatchTransition={vi.fn()}
        />,
      )
      expect(screen.queryByText('Archived')).not.toBeInTheDocument()
    })

    it('falls back to state name when displayName is empty', async () => {
      const statesWithEmptyDisplay: WorkflowState[] = [
        {
          id: 'state-x',
          name: 'FallbackName',
          displayName: { entries: {} },
          color: '#000',
          category: 'active',
          position: 0,
          isDefault: false,
          archived: false,
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z',
        },
      ]
      const { user } = renderWithProviders(
        <SelectionActionBar
          {...defaultProps}
          workflowStates={statesWithEmptyDisplay}
          onBatchTransition={vi.fn()}
        />,
      )
      await user.click(screen.getByRole('combobox'))
      expect(screen.getByText('FallbackName')).toBeInTheDocument()
    })
  })

  describe('tag operations', () => {
    it('shows remove tag button when removableTags has items', () => {
      const removableTags = [
        {
          id: 'tag-1',
          name: 'Bug',
          color: '#ff0000',
          description: '',
          usageCount: 0,
          archived: false,
          createdBy: 'user-1',
          createdAt: '2026-01-01T00:00:00Z',
          updatedAt: '2026-01-01T00:00:00Z',
        },
      ]
      renderWithProviders(<SelectionActionBar {...defaultProps} removableTags={removableTags} />)
      expect(screen.getByRole('button', { name: /移除标签/ })).toBeInTheDocument()
    })

    it('does not show remove tag button when removableTags is empty', () => {
      renderWithProviders(<SelectionActionBar {...defaultProps} removableTags={[]} />)
      expect(screen.queryByRole('button', { name: /移除标签/ })).not.toBeInTheDocument()
    })
  })

  describe('retry enrichment', () => {
    it('shows retry button when onBatchRetryEnrichment provided and terminalFailureCount > 0', async () => {
      const onBatchRetryEnrichment = vi.fn()
      const { user } = renderWithProviders(
        <SelectionActionBar
          {...defaultProps}
          onBatchRetryEnrichment={onBatchRetryEnrichment}
          terminalFailureCount={3}
        />,
      )
      const retryBtn = screen.getByRole('button', { name: /重试/ })
      await user.click(retryBtn)
      expect(onBatchRetryEnrichment).toHaveBeenCalledTimes(1)
    })

    it('does not show retry button when terminalFailureCount is 0', () => {
      renderWithProviders(
        <SelectionActionBar
          {...defaultProps}
          onBatchRetryEnrichment={vi.fn()}
          terminalFailureCount={0}
        />,
      )
      expect(screen.queryByRole('button', { name: /重试/ })).not.toBeInTheDocument()
    })

    it('does not show retry button when onBatchRetryEnrichment not provided', () => {
      renderWithProviders(<SelectionActionBar {...defaultProps} terminalFailureCount={3} />)
      expect(screen.queryByRole('button', { name: /重试/ })).not.toBeInTheDocument()
    })

    it('shows count badge when terminalFailureCount differs from total count', () => {
      renderWithProviders(
        <SelectionActionBar
          {...defaultProps}
          count={10}
          onBatchRetryEnrichment={vi.fn()}
          terminalFailureCount={3}
        />,
      )
      expect(screen.getByText('3')).toBeInTheDocument()
    })

    it('does not show count badge when terminalFailureCount equals total count', () => {
      renderWithProviders(
        <SelectionActionBar
          {...defaultProps}
          count={5}
          onBatchRetryEnrichment={vi.fn()}
          terminalFailureCount={5}
        />,
      )
      const retryBtn = screen.getByRole('button', { name: /重试/ })
      expect(retryBtn).toBeInTheDocument()
      expect(screen.queryByText('5')).not.toBeInTheDocument()
    })
  })
})
