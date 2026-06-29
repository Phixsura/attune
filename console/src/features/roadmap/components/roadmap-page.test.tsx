import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import type { RoadmapItem } from './roadmap-page'
import { RoadmapPage } from './roadmap-page'

const ITEMS: RoadmapItem[] = [
  { id: '1', title: 'API v2', description: 'New API version', status: 'planned', votes: 12 },
  { id: '2', title: 'Dark mode', description: '', status: 'in_progress', votes: 8 },
  { id: '3', title: 'SSO', description: 'Enterprise SSO', status: 'completed', votes: 20 },
]

describe('RoadmapPage', () => {
  it('renders kanban columns with items', () => {
    renderWithProviders(
      <RoadmapPage items={ITEMS} onAdd={vi.fn()} onRemove={vi.fn()} onVote={vi.fn()} />,
    )
    expect(screen.getByText('API v2')).toBeInTheDocument()
    expect(screen.getByText('Dark mode')).toBeInTheDocument()
    expect(screen.getByText('SSO')).toBeInTheDocument()
  })

  it('shows empty state', () => {
    renderWithProviders(
      <RoadmapPage items={[]} onAdd={vi.fn()} onRemove={vi.fn()} onVote={vi.fn()} />,
    )
    expect(screen.getByText('暂无路线图条目')).toBeInTheDocument()
  })

  it('calls onVote when vote button clicked', async () => {
    const onVote = vi.fn()
    renderWithProviders(
      <RoadmapPage items={ITEMS} onAdd={vi.fn()} onRemove={vi.fn()} onVote={onVote} />,
    )
    const voteButtons = screen.getAllByRole('button').filter((b) => b.textContent?.includes('12'))
    await userEvent.click(voteButtons[0])
    expect(onVote).toHaveBeenCalledWith('1')
  })
})
