import { describe, expect, it } from 'vitest'
import type { WorkflowState } from '@/proto/attune/v1/workflow'
import { renderWithProviders, screen } from '@/testing/test-utils'
import { WorkflowStateBadge } from './workflow-state-badge'

// makeState builds a WorkflowState with a slug `name` (machine key) and
// optional per-locale displayName, matching the split-name contract.
function makeState(over: Partial<WorkflowState> = {}): WorkflowState {
  return {
    id: 'ws-1',
    name: 'open',
    color: '#3b82f6',
    category: 'open',
    position: 0,
    isDefault: false,
    archived: false,
    createdAt: '2026-06-14T00:00:00Z',
    updatedAt: '2026-06-14T00:00:00Z',
    ...over,
  }
}

describe('WorkflowStateBadge', () => {
  it('resolves and renders the localized display name', () => {
    renderWithProviders(
      <WorkflowStateBadge
        state={makeState({ name: 'open', displayName: { entries: { default: 'Open' } } })}
      />,
    )
    expect(screen.getByText('Open')).toBeInTheDocument()
  })

  it('prefers the active-locale display name over the default entry', () => {
    // Test i18n defaults to zh-CN, so the resolver chain is [zh-CN, zh, en].
    renderWithProviders(
      <WorkflowStateBadge
        state={makeState({ displayName: { entries: { default: 'Open', zh: '待处理' } } })}
      />,
    )
    expect(screen.getByText('待处理')).toBeInTheDocument()
  })

  it('falls back to the machine key when displayName is empty', () => {
    renderWithProviders(<WorkflowStateBadge state={makeState({ name: 'in_progress' })} />)
    expect(screen.getByText('in_progress')).toBeInTheDocument()
  })

  it('falls back to the machine key when displayName entries are blank', () => {
    renderWithProviders(
      <WorkflowStateBadge
        state={makeState({ name: 'done', displayName: { entries: { default: '' } } })}
      />,
    )
    expect(screen.getByText('done')).toBeInTheDocument()
  })

  it('renders the color dot with correct color', () => {
    const { container } = renderWithProviders(
      <WorkflowStateBadge state={makeState({ color: '#3b82f6' })} />,
    )
    const dot = container.querySelector('[aria-hidden]')
    expect(dot).toHaveStyle({ backgroundColor: '#3b82f6' })
  })

  it('applies open category styles', () => {
    renderWithProviders(
      <WorkflowStateBadge
        state={makeState({ category: 'open', displayName: { entries: { default: 'New' } } })}
      />,
    )
    const badge = screen.getByText('New').closest('span')
    expect(badge?.className).toContain('blue')
  })

  it('applies active category styles', () => {
    renderWithProviders(
      <WorkflowStateBadge
        state={makeState({
          category: 'active',
          color: '#f59e0b',
          displayName: { entries: { default: 'In Progress' } },
        })}
      />,
    )
    const badge = screen.getByText('In Progress').closest('span')
    expect(badge?.className).toContain('amber')
  })

  it('applies closed category styles', () => {
    renderWithProviders(
      <WorkflowStateBadge
        state={makeState({
          category: 'closed',
          color: '#10b981',
          displayName: { entries: { default: 'Done' } },
        })}
      />,
    )
    const badge = screen.getByText('Done').closest('span')
    expect(badge?.className).toContain('emerald')
  })

  it('falls back to muted styles for unknown category', () => {
    renderWithProviders(
      <WorkflowStateBadge
        state={makeState({
          category: 'other',
          color: '#999',
          displayName: { entries: { default: 'Unknown' } },
        })}
      />,
    )
    const badge = screen.getByText('Unknown').closest('span')
    expect(badge?.className).toContain('muted')
  })

  it('accepts additional className', () => {
    renderWithProviders(
      <WorkflowStateBadge
        state={makeState({ displayName: { entries: { default: 'Open' } } })}
        className="ml-2"
      />,
    )
    const badge = screen.getByText('Open').closest('span')
    expect(badge?.className).toContain('ml-2')
  })
})
