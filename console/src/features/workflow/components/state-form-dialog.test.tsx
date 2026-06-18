import { describe, expect, it, vi } from 'vitest'
import type { WorkflowState } from '@/proto/attune/v1/workflow'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'
import { StateFormDialog } from './state-form-dialog'

const existingState: WorkflowState = {
  id: 'ws-1',
  name: 'open',
  displayName: { entries: { default: 'Open', zh: '待处理' } },
  color: '#3b82f6',
  category: 'open',
  position: 0,
  isDefault: true,
  archived: false,
  createdAt: '2026-06-14T00:00:00Z',
  updatedAt: '2026-06-14T00:00:00Z',
}

// Label text for the machine-key input (workflow.state_dialog.key).
const KEY_LABEL = 'Key（稳定标识）'

describe('StateFormDialog', () => {
  it('does not render content when closed', () => {
    renderWithProviders(
      <StateFormDialog open={false} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('renders create mode with empty key and editable display name', () => {
    renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    expect(screen.getByText('新建状态')).toBeInTheDocument()
    expect(screen.getByText('为反馈工作流添加一个新的处理阶段。')).toBeInTheDocument()
    const keyInput = screen.getByLabelText(KEY_LABEL)
    expect(keyInput).toHaveValue('')
    expect(keyInput).not.toHaveAttribute('readonly')
    // The multi-locale display-name editor exposes default/zh/en rows.
    expect(screen.getByText('default')).toBeInTheDocument()
    expect(screen.getByText('zh')).toBeInTheDocument()
    expect(screen.getByText('en')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '新建' })).toBeInTheDocument()
  })

  it('renders edit mode with locked key and pre-filled display name', () => {
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
    const keyInput = screen.getByLabelText(KEY_LABEL)
    expect(keyInput).toHaveValue('open')
    // Key is immutable on edit.
    expect(keyInput).toHaveAttribute('readonly')
    // Display-name rows are pre-filled from state.displayName.entries.
    expect(screen.getByDisplayValue('Open')).toBeInTheDocument()
    expect(screen.getByDisplayValue('待处理')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存' })).toBeInTheDocument()
  })

  it('shows the key-locked hint in edit mode', () => {
    renderWithProviders(
      <StateFormDialog
        open={true}
        state={existingState}
        pending={false}
        onOpenChange={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )
    expect(screen.getByText('创建后标识不可更改；如需改名请编辑显示名称。')).toBeInTheDocument()
    expect(screen.getByText('创建后分类不可更改。')).toBeInTheDocument()
  })

  it('shows the key help hint in create mode', () => {
    renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    expect(screen.getByText('小写英文 + 数字 + 下划线，创建后不可改')).toBeInTheDocument()
    expect(screen.queryByText('创建后分类不可更改。')).not.toBeInTheDocument()
  })

  it('disables submit when the key is empty (create)', () => {
    renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    expect(screen.getByRole('button', { name: '新建' })).toBeDisabled()
  })

  it('disables submit when the key is not a valid slug (create)', async () => {
    const { user } = renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    const keyInput = screen.getByLabelText(KEY_LABEL)
    // Uppercase + space are not allowed by ^[a-z][a-z0-9_]{0,30}$.
    await user.type(keyInput, 'Bad Key')
    expect(screen.getByRole('button', { name: '新建' })).toBeDisabled()
  })

  it('enables submit once the key is a valid slug (create)', async () => {
    const { user } = renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    const keyInput = screen.getByLabelText(KEY_LABEL)
    await user.type(keyInput, 'in_progress')
    expect(screen.getByRole('button', { name: '新建' })).not.toBeDisabled()
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

  it('create posts the trimmed key and the display-name map', async () => {
    const onSubmit = vi.fn()
    const { user } = renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={onSubmit} />,
    )

    const keyInput = screen.getByLabelText(KEY_LABEL)
    await user.type(keyInput, '  in_progress  ')

    // Fill the default-locale display name. The default row is the first
    // textbox after the key input.
    const textboxes = screen.getAllByRole('textbox') as HTMLInputElement[]
    const defaultRow = textboxes.find((el) => el.id !== 'state-key' && el.value === '')
    expect(defaultRow).toBeDefined()
    if (defaultRow) await user.type(defaultRow, 'In Progress')

    await user.click(screen.getByRole('button', { name: '新建' }))
    expect(onSubmit).toHaveBeenCalledTimes(1)
    const arg = onSubmit.mock.calls[0][0]
    expect(arg.name).toBe('in_progress')
    expect(arg.color).toBe('#3b82f6')
    expect(arg.category).toBe('open')
    expect(arg.displayName.default).toBe('In Progress')
  })

  it('edit submits the edited display name and selected color, no key', async () => {
    const onSubmit = vi.fn()
    const { user } = renderWithProviders(
      <StateFormDialog
        open={true}
        state={existingState}
        pending={false}
        onOpenChange={vi.fn()}
        onSubmit={onSubmit}
      />,
    )

    // Switch the color to red (#ef4444).
    await user.click(screen.getByRole('button', { name: '#ef4444' }))
    await user.click(screen.getByRole('button', { name: '保存' }))

    expect(onSubmit).toHaveBeenCalledTimes(1)
    const arg = onSubmit.mock.calls[0][0]
    expect(arg.name).toBe('open')
    expect(arg.color).toBe('#ef4444')
    expect(arg.displayName).toEqual({ default: 'Open', zh: '待处理' })
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
    const colorButtons = screen.getAllByRole('button', { name: /^#/ })
    expect(colorButtons).toHaveLength(9)
  })

  it('selecting a different color submits it (create)', async () => {
    const onSubmit = vi.fn()
    const { user } = renderWithProviders(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={onSubmit} />,
    )

    await user.type(screen.getByLabelText(KEY_LABEL), 'test')
    await user.click(screen.getByRole('button', { name: '#ef4444' }))
    await user.click(screen.getByRole('button', { name: '新建' }))

    expect(onSubmit.mock.calls[0][0].color).toBe('#ef4444')
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
    expect(screen.getByLabelText(KEY_LABEL)).toHaveValue('open')

    // Close and reopen without a state (create mode).
    rerender(
      <StateFormDialog open={false} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    rerender(
      <StateFormDialog open={true} pending={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} />,
    )
    await waitFor(() => {
      expect(screen.getByLabelText(KEY_LABEL)).toHaveValue('')
    })
    // The pre-filled display value is gone too.
    expect(screen.queryByDisplayValue('待处理')).not.toBeInTheDocument()
  })

  it('category select is disabled in edit mode', () => {
    renderWithProviders(
      <StateFormDialog
        open={true}
        state={existingState}
        pending={false}
        onOpenChange={vi.fn()}
        onSubmit={vi.fn()}
      />,
    )
    const dialog = screen.getByRole('dialog')
    const combobox = within(dialog).getByRole('combobox')
    expect(combobox).toBeDisabled()
  })
})
