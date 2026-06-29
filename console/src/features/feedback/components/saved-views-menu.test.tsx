import { describe, expect, it, vi } from 'vitest'
import { SavedViewsMenu } from '@/features/feedback/components/saved-views-menu'
import type { SavedView, SavedViewFilters } from '@/features/feedback/hooks/use-saved-views'
import { renderWithProviders, screen, userEvent } from '@/testing/test-utils'

const emptyFilters: SavedViewFilters = {
  attrFilters: {},
  tagFilter: '',
  workflowFilter: '',
  enrichmentFilter: '',
  urgentOnly: false,
  queueMode: 'all',
  sortMode: 'newest',
  q: '',
}

const sampleView: SavedView = {
  id: 'v-1',
  name: 'Urgent only',
  filters: { ...emptyFilters, urgentOnly: true },
  createdAt: '2026-06-29T00:00:00Z',
}

describe('SavedViewsMenu', () => {
  it('renders the trigger button with view count', () => {
    renderWithProviders(
      <SavedViewsMenu
        views={[sampleView]}
        onSave={vi.fn()}
        onLoad={vi.fn()}
        onRemove={vi.fn()}
        currentFilters={emptyFilters}
      />,
    )
    expect(screen.getByText('视图')).toBeInTheDocument()
    expect(screen.getByText('1')).toBeInTheDocument()
  })

  it('calls onLoad when clicking a saved view', async () => {
    const onLoad = vi.fn()
    const user = userEvent.setup()
    renderWithProviders(
      <SavedViewsMenu
        views={[sampleView]}
        onSave={vi.fn()}
        onLoad={onLoad}
        onRemove={vi.fn()}
        currentFilters={emptyFilters}
      />,
    )
    await user.click(screen.getByText('视图'))
    await user.click(screen.getByText('Urgent only'))
    expect(onLoad).toHaveBeenCalledWith(sampleView)
  })
})
