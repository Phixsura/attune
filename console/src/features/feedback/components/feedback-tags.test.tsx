import { beforeEach, describe, expect, it, vi } from 'vitest'
import { FeedbackTagSection } from '@/features/feedback/components/feedback-tags'
import type { Tag } from '@/proto/attune/v1/tag'
import { renderWithProviders, screen } from '@/testing/test-utils'

const mocks = vi.hoisted(() => ({
  addMutate: vi.fn(),
  removeMutate: vi.fn(),
  toastError: vi.fn(),
}))

vi.mock('@/features/feedback/api/add-feedback-tag', () => ({
  useAddFeedbackTag: vi.fn(() => ({ mutate: mocks.addMutate })),
}))

vi.mock('@/features/feedback/api/remove-feedback-tag', () => ({
  useRemoveFeedbackTag: vi.fn(() => ({ mutate: mocks.removeMutate })),
}))

vi.mock('sonner', () => ({
  toast: { error: mocks.toastError },
}))

vi.mock('@/components/tag/tag-combobox', () => ({
  TagCombobox: ({
    availableTags,
    onSelect,
    onCreate,
  }: {
    availableTags: Array<{ id: string; name: string }>
    onSelect: (tagId: string) => void
    onCreate?: (name: string) => void
  }) => (
    <div>
      <div data-testid="available-tags">{availableTags.map((tag) => tag.name).join(',')}</div>
      <button type="button" onClick={() => onSelect(availableTags[0]?.id ?? 'missing')}>
        select-first
      </button>
      <button type="button" onClick={() => onCreate?.('fresh tag')}>
        create-fresh
      </button>
    </div>
  ),
}))

vi.mock('@/components/tag/tag-badge-tooltip', () => ({
  TagBadgeTooltip: ({
    tag,
    onRemove,
  }: {
    tag: { id: string; name: string }
    onRemove?: () => void
  }) => (
    <button type="button" onClick={onRemove}>
      remove {tag.name}
    </button>
  ),
}))

function tag(overrides: Partial<Tag>): Tag {
  return {
    id: 'tag-1',
    name: 'bug',
    color: '#ef4444',
    description: '',
    exclusiveScope: '',
    archived: false,
    usageCount: 0,
    createdBy: '',
    createdAt: '',
    updatedAt: '',
    ...overrides,
  }
}

describe('FeedbackTagSection', () => {
  beforeEach(() => {
    mocks.addMutate.mockReset()
    mocks.removeMutate.mockReset()
    mocks.toastError.mockReset()
  })

  it('renders the header and empty state', () => {
    renderWithProviders(<FeedbackTagSection feedbackId="fb-1" tags={[]} availableTags={[]} />)

    expect(screen.getByText('标签')).toBeInTheDocument()
    expect(screen.getByText('暂无标签')).toBeInTheDocument()
  })

  it('hides the header when requested', () => {
    renderWithProviders(
      <FeedbackTagSection feedbackId="fb-1" tags={[]} availableTags={[]} hideHeader />,
    )

    expect(screen.queryByText('标签')).not.toBeInTheDocument()
    expect(screen.getByText('暂无标签')).toBeInTheDocument()
  })

  it('passes only unassigned tags to the picker', () => {
    renderWithProviders(
      <FeedbackTagSection
        feedbackId="fb-1"
        tags={[tag({ id: 'tag-1', name: 'bug' })]}
        availableTags={[
          tag({ id: 'tag-1', name: 'bug' }),
          tag({ id: 'tag-2', name: 'feature', color: '#3b82f6' }),
        ]}
      />,
    )

    expect(screen.getByTestId('available-tags')).toHaveTextContent('feature')
    expect(screen.getByTestId('available-tags')).not.toHaveTextContent('bug')
  })

  it('adds an existing tag and exposes add errors through toast', async () => {
    mocks.addMutate.mockImplementation((_payload, opts) => {
      opts.onError(new Error('add failed'))
    })
    const { user } = renderWithProviders(
      <FeedbackTagSection
        feedbackId="fb-1"
        tags={[]}
        availableTags={[tag({ id: 'tag-2', name: 'feature' })]}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'select-first' }))

    expect(mocks.addMutate).toHaveBeenCalledWith(
      { tagId: 'tag-2' },
      { onError: expect.any(Function) },
    )
    expect(mocks.toastError).toHaveBeenCalledWith('add failed')
  })

  it('creates a new tag and uses the translated fallback for unknown errors', async () => {
    mocks.addMutate.mockImplementation((_payload, opts) => {
      opts.onError('nope')
    })
    const { user } = renderWithProviders(
      <FeedbackTagSection feedbackId="fb-1" tags={[]} availableTags={[]} />,
    )

    await user.click(screen.getByRole('button', { name: 'create-fresh' }))

    expect(mocks.addMutate).toHaveBeenCalledWith(
      { tagName: 'fresh tag' },
      { onError: expect.any(Function) },
    )
    expect(mocks.toastError).toHaveBeenCalledWith('出错了')
  })

  it('removes an assigned tag and reports remove errors', async () => {
    mocks.removeMutate.mockImplementation((_payload, opts) => {
      opts.onError(new Error('remove failed'))
    })
    const { user } = renderWithProviders(
      <FeedbackTagSection
        feedbackId="fb-1"
        tags={[tag({ id: 'tag-1', name: 'bug' })]}
        availableTags={[tag({ id: 'tag-1', name: 'bug' })]}
      />,
    )

    await user.click(screen.getByRole('button', { name: 'remove bug' }))

    expect(mocks.removeMutate).toHaveBeenCalledWith('tag-1', { onError: expect.any(Function) })
    expect(mocks.toastError).toHaveBeenCalledWith('remove failed')
  })
})
