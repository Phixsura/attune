import { describe, expect, it } from 'vitest'
import { renderWithProviders, screen } from '@/testing/test-utils'
import { WorkflowStateBadge } from './workflow-state-badge'

describe('WorkflowStateBadge', () => {
  it('renders the state name', () => {
    renderWithProviders(<WorkflowStateBadge name="Open" color="#3b82f6" category="open" />)
    expect(screen.getByText('Open')).toBeInTheDocument()
  })

  it('renders the color dot with correct color', () => {
    const { container } = renderWithProviders(
      <WorkflowStateBadge name="Open" color="#3b82f6" category="open" />,
    )
    const dot = container.querySelector('[aria-hidden]')
    expect(dot).toHaveStyle({ backgroundColor: '#3b82f6' })
  })

  it('applies open category styles', () => {
    renderWithProviders(<WorkflowStateBadge name="New" color="#3b82f6" category="open" />)
    const badge = screen.getByText('New').closest('span')
    expect(badge?.className).toContain('blue')
  })

  it('applies active category styles', () => {
    renderWithProviders(<WorkflowStateBadge name="In Progress" color="#f59e0b" category="active" />)
    const badge = screen.getByText('In Progress').closest('span')
    expect(badge?.className).toContain('amber')
  })

  it('applies closed category styles', () => {
    renderWithProviders(<WorkflowStateBadge name="Done" color="#10b981" category="closed" />)
    const badge = screen.getByText('Done').closest('span')
    expect(badge?.className).toContain('emerald')
  })

  it('falls back to muted styles for unknown category', () => {
    renderWithProviders(<WorkflowStateBadge name="Unknown" color="#999" category="other" />)
    const badge = screen.getByText('Unknown').closest('span')
    expect(badge?.className).toContain('muted')
  })

  it('accepts additional className', () => {
    renderWithProviders(
      <WorkflowStateBadge name="Open" color="#3b82f6" category="open" className="ml-2" />,
    )
    const badge = screen.getByText('Open').closest('span')
    expect(badge?.className).toContain('ml-2')
  })
})
