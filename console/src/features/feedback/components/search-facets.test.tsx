import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import { SearchFacetBar } from './search-facets'

const dims = [
  { name: 'kind', displayName: 'Kind' },
  { name: 'severity', displayName: 'Severity' },
]

describe('SearchFacetBar', () => {
  it('renders facet badges', () => {
    renderWithProviders(
      <SearchFacetBar
        facets={[{ key: 'kind', label: 'Kind', value: 'bug' }]}
        onAddFacet={vi.fn()}
        onRemoveFacet={vi.fn()}
        availableDims={dims}
      />,
    )
    expect(screen.getByText('bug')).toBeInTheDocument()
    expect(screen.getByText('Kind:')).toBeInTheDocument()
  })

  it('calls onRemoveFacet when X clicked', async () => {
    const onRemove = vi.fn()
    const user = userEvent.setup()
    renderWithProviders(
      <SearchFacetBar
        facets={[{ key: 'kind', label: 'Kind', value: 'bug' }]}
        onAddFacet={vi.fn()}
        onRemoveFacet={onRemove}
        availableDims={dims}
      />,
    )
    const removeBtn = screen.getByText('bug').parentElement?.querySelector('button')
    if (removeBtn) await user.click(removeBtn)
    expect(onRemove).toHaveBeenCalledWith('kind')
  })

  it('hides add button when all dims used', () => {
    renderWithProviders(
      <SearchFacetBar
        facets={[
          { key: 'kind', label: 'Kind', value: 'bug' },
          { key: 'severity', label: 'Severity', value: 'high' },
        ]}
        onAddFacet={vi.fn()}
        onRemoveFacet={vi.fn()}
        availableDims={dims}
      />,
    )
    expect(screen.queryByText('feedback.search.add_facet')).not.toBeInTheDocument()
  })
})
