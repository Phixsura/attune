import { describe, expect, it, vi } from 'vitest'
import type { Tag } from '@/proto/attune/v1/tag'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { TagCombobox } from './tag-combobox'

const tags: Tag[] = [
  {
    id: 't1',
    name: 'bug',
    color: '#ef4444',
    description: '',
    exclusiveScope: '',
    archived: false,
    usageCount: 3,
    createdBy: '',
    createdAt: '',
    updatedAt: '',
  },
  {
    id: 't2',
    name: 'feature',
    color: '#3b82f6',
    description: '',
    exclusiveScope: 'priority',
    archived: false,
    usageCount: 1,
    createdBy: '',
    createdAt: '',
    updatedAt: '',
  },
  {
    id: 't3',
    name: 'urgent',
    color: '#f97316',
    description: '',
    exclusiveScope: '',
    archived: false,
    usageCount: 0,
    createdBy: '',
    createdAt: '',
    updatedAt: '',
  },
]

describe('TagCombobox', () => {
  it('opens popover on trigger click and shows all tags', async () => {
    const onSelect = vi.fn()
    const { user } = renderWithProviders(<TagCombobox availableTags={tags} onSelect={onSelect} />)

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    expect(screen.getAllByRole('option')).toHaveLength(3)

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    })
  })

  it('filters tags by search query', async () => {
    const onSelect = vi.fn()
    const { user } = renderWithProviders(<TagCombobox availableTags={tags} onSelect={onSelect} />)

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })
    await user.type(screen.getByRole('combobox'), 'bug')
    expect(screen.getAllByRole('option')).toHaveLength(1)
    expect(screen.getByText('bug')).toBeInTheDocument()

    await user.keyboard('{Enter}')
    expect(onSelect).toHaveBeenCalledWith('t1')
  })

  it('calls onSelect when clicking a tag option', async () => {
    const onSelect = vi.fn()
    const { user } = renderWithProviders(<TagCombobox availableTags={tags} onSelect={onSelect} />)

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })
    await user.click(screen.getByText('feature'))
    expect(onSelect).toHaveBeenCalledWith('t2')
  })

  it('shows create option when no exact match and onCreate provided', async () => {
    const onSelect = vi.fn()
    const onCreate = vi.fn()
    const { user } = renderWithProviders(
      <TagCombobox availableTags={tags} onSelect={onSelect} onCreate={onCreate} />,
    )

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })
    await user.type(screen.getByRole('combobox'), 'newlabel')
    const options = screen.getAllByRole('option')
    expect(options[options.length - 1]).toHaveAttribute('id', expect.stringContaining('create'))
  })

  it('hides create option when the exact tag exists outside the selectable list', async () => {
    const onSelect = vi.fn()
    const onCreate = vi.fn()
    const { user } = renderWithProviders(
      <TagCombobox
        availableTags={[tags[1], tags[2]]}
        allTags={tags}
        onSelect={onSelect}
        onCreate={onCreate}
      />,
    )

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })
    await user.type(screen.getByRole('combobox'), 'bug')

    expect(screen.getByText('标签「bug」已添加到当前反馈')).toBeInTheDocument()
    expect(screen.queryByText('创建「bug」')).not.toBeInTheDocument()

    await user.keyboard('{Enter}')
    expect(onCreate).not.toHaveBeenCalled()
  })

  it('navigates options with ArrowDown/ArrowUp', async () => {
    const onSelect = vi.fn()
    const { user } = renderWithProviders(<TagCombobox availableTags={tags} onSelect={onSelect} />)

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })

    const input = screen.getByRole('combobox')
    await user.keyboard('{ArrowDown}')
    expect(input).toHaveAttribute('aria-activedescendant', expect.stringContaining('t1'))

    await user.keyboard('{ArrowDown}')
    expect(input).toHaveAttribute('aria-activedescendant', expect.stringContaining('t2'))

    await user.keyboard('{ArrowUp}')
    expect(input).toHaveAttribute('aria-activedescendant', expect.stringContaining('t1'))

    await user.keyboard('{Home}')
    expect(input).toHaveAttribute('aria-activedescendant', expect.stringContaining('t1'))

    await user.keyboard('{End}')
    expect(input).toHaveAttribute('aria-activedescendant', expect.stringContaining('t3'))

    await user.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByRole('combobox')).not.toBeInTheDocument()
    })
  })

  it('keeps keyboard navigation inert when there are no selectable options', async () => {
    const onSelect = vi.fn()
    const { user } = renderWithProviders(<TagCombobox availableTags={tags} onSelect={onSelect} />)

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })
    const input = screen.getByRole('combobox')
    await user.type(input, 'missing')
    expect(screen.getByText('无匹配标签')).toBeInTheDocument()

    await user.keyboard('{ArrowDown}{ArrowUp}{Home}{End}{Enter}')

    expect(input).not.toHaveAttribute('aria-activedescendant')
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('selects active option on Enter', async () => {
    const onSelect = vi.fn()
    const { user } = renderWithProviders(<TagCombobox availableTags={tags} onSelect={onSelect} />)

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })

    await user.keyboard('{ArrowDown}{ArrowDown}{Enter}')
    expect(onSelect).toHaveBeenCalledWith('t2')
  })

  it('calls onCreate on Enter when create option is active', async () => {
    const onSelect = vi.fn()
    const onCreate = vi.fn()
    const { user } = renderWithProviders(
      <TagCombobox availableTags={tags} onSelect={onSelect} onCreate={onCreate} />,
    )

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })
    await user.type(screen.getByRole('combobox'), 'newone')
    await user.keyboard('{End}{Enter}')
    expect(onCreate).toHaveBeenCalledWith('newone')
  })

  it('calls onCreate on Enter when a new tag is typed', async () => {
    const onSelect = vi.fn()
    const onCreate = vi.fn()
    const { user } = renderWithProviders(
      <TagCombobox availableTags={tags} onSelect={onSelect} onCreate={onCreate} />,
    )

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })
    await user.type(screen.getByRole('combobox'), 'freshlabel')
    await user.keyboard('{Enter}')
    expect(onCreate).toHaveBeenCalledWith('freshlabel')
    expect(onSelect).not.toHaveBeenCalled()
  })

  it('displays exclusive scope badge on tag options', async () => {
    const onSelect = vi.fn()
    const { user } = renderWithProviders(<TagCombobox availableTags={tags} onSelect={onSelect} />)

    await user.click(screen.getByRole('button'))
    await waitFor(() => {
      expect(screen.getByRole('combobox')).toBeInTheDocument()
    })
    expect(screen.getByText('priority')).toBeInTheDocument()
  })
})
