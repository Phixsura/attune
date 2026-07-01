import { describe, expect, it, vi } from 'vitest'
import { SearchResults } from '@/features/feedback/components/search-results'
import { SemanticSearchBar } from '@/features/feedback/components/semantic-search-bar'
import type { SemanticSearchHit } from '@/proto/attune/v1/search'
import { expectNoA11yViolations } from '@/testing/a11y'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('SemanticSearchBar', () => {
  const defaultProps = {
    onSearch: vi.fn(),
  }

  it('submits query on form submit', async () => {
    const onSearch = vi.fn()
    const { user } = renderWithProviders(<SemanticSearchBar onSearch={onSearch} />)
    const input = screen.getByPlaceholderText('搜索反馈内容...')
    await user.type(input, 'test query')
    const submitBtn = screen.getByRole('button', { name: /语义搜索/ })
    await user.click(submitBtn)
    expect(onSearch).toHaveBeenCalledWith('test query')
  })

  it('trims whitespace from query', async () => {
    const onSearch = vi.fn()
    const { user } = renderWithProviders(<SemanticSearchBar onSearch={onSearch} />)
    const input = screen.getByPlaceholderText('搜索反馈内容...')
    await user.type(input, '  spaced query  ')
    const submitBtn = screen.getByRole('button', { name: /语义搜索/ })
    await user.click(submitBtn)
    expect(onSearch).toHaveBeenCalledWith('spaced query')
  })

  it('clears query on clear click', async () => {
    const onClear = vi.fn()
    const { user } = renderWithProviders(
      <SemanticSearchBar {...defaultProps} onClear={onClear} defaultValue="existing" />,
    )
    const input = screen.getByDisplayValue('existing')
    expect(input).toBeInTheDocument()
    const clearBtn = screen.getByRole('button', { name: '清除搜索内容' })
    await user.click(clearBtn)
    expect(onClear).toHaveBeenCalledTimes(1)
  })

  it('has no automated accessibility violations in the populated state', async () => {
    const { container } = renderWithProviders(
      <SemanticSearchBar {...defaultProps} defaultValue="existing" />,
    )

    expect(screen.getByRole('textbox', { name: '搜索反馈内容' })).toBeInTheDocument()
    await expectNoA11yViolations(container)
  })

  it('disables submit when empty', () => {
    renderWithProviders(<SemanticSearchBar {...defaultProps} />)
    const submitBtn = screen.getByRole('button', { name: /语义搜索/ })
    expect(submitBtn).toBeDisabled()
  })

  it('disables submit when only whitespace', async () => {
    const { user } = renderWithProviders(<SemanticSearchBar {...defaultProps} />)
    const input = screen.getByPlaceholderText('搜索反馈内容...')
    await user.type(input, '   ')
    const submitBtn = screen.getByRole('button', { name: /语义搜索/ })
    expect(submitBtn).toBeDisabled()
  })

  it('enables submit when has content', async () => {
    const { user } = renderWithProviders(<SemanticSearchBar {...defaultProps} />)
    const input = screen.getByPlaceholderText('搜索反馈内容...')
    await user.type(input, 'test')
    const submitBtn = screen.getByRole('button', { name: /语义搜索/ })
    expect(submitBtn).not.toBeDisabled()
  })

  it('disables input when loading', () => {
    renderWithProviders(<SemanticSearchBar {...defaultProps} isLoading />)
    const input = screen.getByPlaceholderText('搜索反馈内容...')
    expect(input).toBeDisabled()
  })

  it('uses custom placeholder', () => {
    renderWithProviders(<SemanticSearchBar {...defaultProps} placeholder="Custom placeholder" />)
    expect(screen.getByPlaceholderText('Custom placeholder')).toBeInTheDocument()
  })

  it('uses default value', () => {
    renderWithProviders(<SemanticSearchBar {...defaultProps} defaultValue="initial query" />)
    expect(screen.getByDisplayValue('initial query')).toBeInTheDocument()
  })
})

describe('SearchResults', () => {
  const makeHit = (id: string, similarity: number, matchType?: string): SemanticSearchHit => ({
    feedback: {
      id,
      content: `Content for ${id}`,
      enrichedTitle: `Title for ${id}`,
      source: 'test',
      type: 'feedback',
      userId: 'user-1',
      pageUrl: '',
      isUrgent: false,
      enrichmentStatus: 'done',
      createdAt: '2026-06-15T00:00:00Z',
      tags: [],
      allowedNextStates: [],
    },
    similarity,
    keywordScore: 0,
    matchType: matchType ?? '',
  })

  it('shows no results message when empty', () => {
    renderWithProviders(<SearchResults results={[]} />)
    expect(screen.getByText('未找到匹配的反馈')).toBeInTheDocument()
  })

  it('shows keyword fallback badge when used', () => {
    const results = [makeHit('1', 0.8)]
    renderWithProviders(<SearchResults results={results} usedKeywordFallback />)
    expect(screen.getByText('使用关键词搜索')).toBeInTheDocument()
  })

  it('does not show keyword fallback badge when not used', () => {
    const results = [makeHit('1', 0.8)]
    renderWithProviders(<SearchResults results={results} usedKeywordFallback={false} />)
    expect(screen.queryByText('使用关键词搜索')).not.toBeInTheDocument()
  })

  it('renders results with similarity percentage', () => {
    const results = [makeHit('1', 0.85)]
    renderWithProviders(<SearchResults results={results} />)
    expect(screen.getByText('Title for 1')).toBeInTheDocument()
    expect(screen.getByText('Content for 1')).toBeInTheDocument()
    expect(screen.getByText('85%')).toBeInTheDocument()
  })

  it('renders multiple results', () => {
    const results = [makeHit('1', 0.9), makeHit('2', 0.7), makeHit('3', 0.5)]
    renderWithProviders(<SearchResults results={results} />)
    expect(screen.getByText('Title for 1')).toBeInTheDocument()
    expect(screen.getByText('Title for 2')).toBeInTheDocument()
    expect(screen.getByText('Title for 3')).toBeInTheDocument()
  })

  it('calls onSelectFeedback when result clicked', async () => {
    const onSelect = vi.fn()
    const results = [makeHit('fb-123', 0.8)]
    const { user } = renderWithProviders(
      <SearchResults results={results} onSelectFeedback={onSelect} />,
    )
    await user.click(screen.getByText('Title for fb-123'))
    expect(onSelect).toHaveBeenCalledWith('fb-123')
  })

  it('shows untitled when enrichedTitle is empty', () => {
    const results: SemanticSearchHit[] = [
      {
        feedback: {
          id: '1',
          content: 'Some content',
          enrichedTitle: '',
          source: 'test',
          type: 'feedback',
          userId: 'user-1',
          pageUrl: '',
          isUrgent: false,
          enrichmentStatus: 'done',
          createdAt: '2026-06-15T00:00:00Z',
          tags: [],
          allowedNextStates: [],
        },
        similarity: 0.8,
        keywordScore: 0,
        matchType: '',
      },
    ]
    renderWithProviders(<SearchResults results={results} />)
    expect(screen.getByText('无标题')).toBeInTheDocument()
  })

  it('applies high similarity styling for >= 80%', () => {
    const results = [makeHit('1', 0.85)]
    renderWithProviders(<SearchResults results={results} />)
    const badge = screen.getByText('85%')
    expect(badge).toHaveClass('text-green-700')
  })

  it('applies medium similarity styling for >= 60%', () => {
    const results = [makeHit('1', 0.65)]
    renderWithProviders(<SearchResults results={results} />)
    const badge = screen.getByText('65%')
    expect(badge).toHaveClass('text-amber-700')
  })

  it('applies low similarity styling for < 60%', () => {
    const results = [makeHit('1', 0.45)]
    renderWithProviders(<SearchResults results={results} />)
    const badge = screen.getByText('45%')
    expect(badge).toHaveClass('text-muted-foreground')
  })
})
