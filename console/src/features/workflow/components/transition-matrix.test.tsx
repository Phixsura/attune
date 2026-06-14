import { describe, expect, it, vi } from 'vitest'
import type { WorkflowState, WorkflowTransition } from '@/proto/attune/v1/workflow'
import { renderWithProviders, screen } from '@/testing/test-utils'
import { TransitionMatrix } from './transition-matrix'

const states: WorkflowState[] = [
  {
    id: 'ws-1',
    name: 'Open',
    color: '#3b82f6',
    category: 'open',
    position: 0,
    isDefault: true,
    archived: false,
    createdAt: '2026-06-14T00:00:00Z',
    updatedAt: '2026-06-14T00:00:00Z',
  },
  {
    id: 'ws-2',
    name: 'In Progress',
    color: '#f59e0b',
    category: 'active',
    position: 1,
    isDefault: false,
    archived: false,
    createdAt: '2026-06-14T00:00:00Z',
    updatedAt: '2026-06-14T00:00:00Z',
  },
  {
    id: 'ws-3',
    name: 'Done',
    color: '#22c55e',
    category: 'closed',
    position: 2,
    isDefault: false,
    archived: false,
    createdAt: '2026-06-14T00:00:00Z',
    updatedAt: '2026-06-14T00:00:00Z',
  },
]

const archivedState: WorkflowState = {
  id: 'ws-4',
  name: 'Archived State',
  color: '#6b7280',
  category: 'closed',
  position: 3,
  isDefault: false,
  archived: true,
  createdAt: '2026-06-14T00:00:00Z',
  updatedAt: '2026-06-14T00:00:00Z',
}

const transitions: WorkflowTransition[] = [
  { id: 'wt-1', fromStateId: 'ws-1', toStateId: 'ws-2' },
  { id: 'wt-2', fromStateId: 'ws-2', toStateId: 'ws-3' },
]

describe('TransitionMatrix', () => {
  it('renders nothing when all states are archived', () => {
    const { container } = renderWithProviders(
      <TransitionMatrix
        states={[archivedState]}
        transitions={[]}
        saving={false}
        onSave={vi.fn()}
      />,
    )
    expect(container.innerHTML).toBe('')
  })

  it('renders column and row headers for active states', () => {
    renderWithProviders(
      <TransitionMatrix states={states} transitions={[]} saving={false} onSave={vi.fn()} />,
    )
    // Each state name should appear in both row and column headers
    const openBadges = screen.getAllByText('Open')
    expect(openBadges.length).toBeGreaterThanOrEqual(2)
    const progressBadges = screen.getAllByText('In Progress')
    expect(progressBadges.length).toBeGreaterThanOrEqual(2)
  })

  it('renders dashes on the diagonal (self-transitions)', () => {
    renderWithProviders(
      <TransitionMatrix states={states} transitions={[]} saving={false} onSave={vi.fn()} />,
    )
    const dashes = screen.getAllByText('—')
    expect(dashes).toHaveLength(states.length)
  })

  it('checks existing transitions', () => {
    renderWithProviders(
      <TransitionMatrix
        states={states}
        transitions={transitions}
        saving={false}
        onSave={vi.fn()}
      />,
    )
    const checkbox = screen.getByRole('checkbox', { name: 'Open → In Progress' })
    expect(checkbox).toBeChecked()
    const checkbox2 = screen.getByRole('checkbox', { name: 'In Progress → Done' })
    expect(checkbox2).toBeChecked()
    const checkbox3 = screen.getByRole('checkbox', { name: 'Open → Done' })
    expect(checkbox3).not.toBeChecked()
  })

  it('save button is disabled when no changes', () => {
    renderWithProviders(
      <TransitionMatrix
        states={states}
        transitions={transitions}
        saving={false}
        onSave={vi.fn()}
      />,
    )
    const saveBtn = screen.getByRole('button', { name: /保存规则/ })
    expect(saveBtn).toBeDisabled()
  })

  it('toggling a checkbox enables save and calls onSave with edges', async () => {
    const onSave = vi.fn()
    const { user } = renderWithProviders(
      <TransitionMatrix states={states} transitions={transitions} saving={false} onSave={onSave} />,
    )

    // Toggle Open -> Done (currently unchecked)
    const checkbox = screen.getByRole('checkbox', { name: 'Open → Done' })
    await user.click(checkbox)

    const saveBtn = screen.getByRole('button', { name: /保存规则/ })
    expect(saveBtn).not.toBeDisabled()

    await user.click(saveBtn)
    expect(onSave).toHaveBeenCalledTimes(1)
    const edges = onSave.mock.calls[0][0]
    // Should include the original two transitions plus the newly toggled one
    expect(edges).toHaveLength(3)
    expect(edges).toContainEqual({ fromStateId: 'ws-1', toStateId: 'ws-2' })
    expect(edges).toContainEqual({ fromStateId: 'ws-2', toStateId: 'ws-3' })
    expect(edges).toContainEqual({ fromStateId: 'ws-1', toStateId: 'ws-3' })
  })

  it('unchecking a transition marks dirty and removes edge on save', async () => {
    const onSave = vi.fn()
    const { user } = renderWithProviders(
      <TransitionMatrix states={states} transitions={transitions} saving={false} onSave={onSave} />,
    )

    // Uncheck Open -> In Progress
    const checkbox = screen.getByRole('checkbox', { name: 'Open → In Progress' })
    await user.click(checkbox)

    const saveBtn = screen.getByRole('button', { name: /保存规则/ })
    await user.click(saveBtn)

    const edges = onSave.mock.calls[0][0]
    expect(edges).toHaveLength(1)
    expect(edges).toContainEqual({ fromStateId: 'ws-2', toStateId: 'ws-3' })
  })

  it('save button is disabled while saving', () => {
    renderWithProviders(
      <TransitionMatrix states={states} transitions={[]} saving={true} onSave={vi.fn()} />,
    )
    const saveBtn = screen.getByRole('button', { name: /保存规则/ })
    expect(saveBtn).toBeDisabled()
  })

  it('excludes archived states from the matrix', () => {
    renderWithProviders(
      <TransitionMatrix
        states={[...states, archivedState]}
        transitions={[]}
        saving={false}
        onSave={vi.fn()}
      />,
    )
    expect(screen.queryByText('Archived State')).not.toBeInTheDocument()
  })
})
