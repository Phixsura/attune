import { describe, expect, it, vi } from 'vitest'
import type { Tag } from '@/proto/attune/v1/tag'
import { renderWithProviders, screen } from '@/testing/test-utils'
import { TagBadgeTooltip } from './tag-badge-tooltip'

const baseTag: Tag = {
  id: 't1',
  name: 'regression',
  color: '#ef4444',
  description: '',
  exclusiveScope: '',
  archived: false,
  usageCount: 0,
  createdBy: '',
  createdAt: '',
  updatedAt: '',
}

describe('TagBadgeTooltip', () => {
  it('renders the tag name', () => {
    renderWithProviders(<TagBadgeTooltip tag={baseTag} />)
    expect(screen.getByText('regression')).toBeInTheDocument()
  })

  it('renders the color dot with correct color', () => {
    const { container } = renderWithProviders(<TagBadgeTooltip tag={baseTag} />)
    const dot = container.querySelector('[aria-hidden]')
    expect(dot).toHaveStyle({ backgroundColor: '#ef4444' })
  })

  it('renders without crashing when description is present', () => {
    const tag = { ...baseTag, description: 'Marks regressions' }
    renderWithProviders(<TagBadgeTooltip tag={tag} />)
    expect(screen.getByText('regression')).toBeInTheDocument()
  })

  it('renders without crashing when exclusiveScope is present', () => {
    const tag = { ...baseTag, exclusiveScope: 'severity' }
    renderWithProviders(<TagBadgeTooltip tag={tag} />)
    expect(screen.getByText('regression')).toBeInTheDocument()
  })

  it('renders without crashing when usageCount > 0', () => {
    const tag = { ...baseTag, usageCount: 5 }
    renderWithProviders(<TagBadgeTooltip tag={tag} />)
    expect(screen.getByText('regression')).toBeInTheDocument()
  })

  it('shows remove button when onRemove is provided', () => {
    const onRemove = vi.fn()
    renderWithProviders(<TagBadgeTooltip tag={baseTag} onRemove={onRemove} />)
    const removeBtn = screen.getByRole('button')
    expect(removeBtn).toBeInTheDocument()
  })

  it('calls onRemove when remove button is clicked', async () => {
    const onRemove = vi.fn()
    const { user } = renderWithProviders(<TagBadgeTooltip tag={baseTag} onRemove={onRemove} />)
    await user.click(screen.getByRole('button'))
    expect(onRemove).toHaveBeenCalledOnce()
  })

  it('does not show remove button when onRemove is not provided', () => {
    renderWithProviders(<TagBadgeTooltip tag={baseTag} />)
    expect(screen.queryByRole('button')).toBeNull()
  })
})
