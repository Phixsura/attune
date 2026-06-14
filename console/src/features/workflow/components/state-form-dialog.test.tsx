import { describe, expect, it, vi } from 'vitest'
import type { WorkflowState } from '@/proto/attune/v1/workflow'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { StateFormDialog } from './state-form-dialog'

const existingState: WorkflowState = {
  id: 'ws-1',
  name: 'Open',
  color: '#3b82f6',
  category: 'open',
  position: 0,
  isDefault: true,
  archived: false,
  createdAt: '2026-06-14T00:00:00Z',
  updatedAt: '2026-06-14T00:00:00Z',
}

describe('StateFormDialog', () => {
  it('does not render content when closed', () => {
    renderWithProviders(
      <StateFormDialog open={false} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('renders create mode with empty fields', () => {
    renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    expect(screen.getByText('新建状态')).toBeInTheDocument()
    expect(screen.getByText('为反馈工作流添加一个新的处理阶段。')).toBeInTheDocument()
    const nameInput = screen.getByLabelText('名称')
    expect(nameInput).toHaveValue('')
    expect(screen.getByRole('button', { name: '新建' })).toBeInTheDocument()
  })

  it('renders edit mode with pre-filled fields', () => {
    renderWithProviders(
      <StateFormDialog
        open={true}
        state={existingState}
        pending={false}
        onOpenChange={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByText('编辑状态')).toBeInTheDocument()
    expect(screen.getByText('修改状态的名称或颜色。')).toBeInTheDocument()
    const nameInput = screen.getByLabelText('名称')
    expect(nameInput).toHaveValue('Open')
    expect(screen.getByRole('button', { name: '保存' })).toBeInTheDocument()
  })

  it('shows category-locked hint in edit mode', () => {
    renderWithProviders(
      <StateFormDialog
        open={true}
        state={existingState}
        pending={false}
        onOpenChange={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByText('创建后分类不可更改。')).toBeInTheDocument()
  })

  it('does not show category-locked hint in create mode', () => {
    renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    expect(screen.queryByText('创建后分类不可更改。')).not.toBeInTheDocument()
  })

  it('submit button is disabled when name is empty', () => {
    renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    expect(screen.getByRole('button', { name: '新建' })).toBeDisabled()
  })

  it('submit button is disabled while pending', () => {
    renderWithProviders(
      <StateFormDialog
        open={true}
        state={existingState}
        pending={true}
        onOpenChange={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByRole('button', { name: '保存' })).toBeDisabled()
  })

  it('calls onSubmit with trimmed name on form submit', async () => {
    const onSubmit = vi.fn()
    const { user } = renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={onSubmit} />,
    )

    const nameInput = screen.getByLabelText('名称')
    await user.type(nameInput, '  New State  ')

    await user.click(screen.getByRole('button', { name: '新建' }))
    expect(onSubmit).toHaveBeenCalledWith({
      name: 'New State',
      color: '#3b82f6',
      category: 'open',
    })
  })

  it('calls onOpenChange(false) when cancel is clicked', async () => {
    const onOpenChange = vi.fn()
    const { user } = renderWithProviders(
      <StateFormDialog
        open={true}
        pending={false}
        onOpenChange={onOpenChange}
        onSubmit={vi.fn()}
      />,
    )

    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('renders color palette buttons', () => {
    renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    // 9 color palette buttons
    const colorButtons = screen.getAllByRole('button', { name: /^#/ })
    expect(colorButtons).toHaveLength(9)
  })

  it('selecting a different color submits it', async () => {
    const onSubmit = vi.fn()
    const { user } = renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={onSubmit} />,
    )

    const nameInput = screen.getByLabelText('名称')
    await user.type(nameInput, 'Test')

    // Click the red color (#ef4444)
    await user.click(screen.getByRole('button', { name: '#ef4444' }))

    await user.click(screen.getByRole('button', { name: '新建' }))
    expect(onSubmit).toHaveBeenCalledWith({
      name: 'Test',
      color: '#ef4444',
      category: 'open',
    })
  })

  it('resets fields when dialog reopens', async () => {
    const { rerender } = renderWithProviders(
      <StateFormDialog
        open={true}
        state={existingState}
        pending={false}
        onOpenChange={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByLabelText('名称')).toHaveValue('Open')

    // Close and reopen without a state (create mode)
    rerender(
      <StateFormDialog open={false} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    rerender(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    await waitFor(() => {
      expect(screen.getByLabelText('名称')).toHaveValue('')
    })
  })
})
