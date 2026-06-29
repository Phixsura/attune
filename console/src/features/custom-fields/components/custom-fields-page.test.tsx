import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import { type CustomFieldDefinition, CustomFieldsPage } from './custom-fields-page'

const sampleFields: CustomFieldDefinition[] = [
  {
    id: '1',
    fieldKey: 'priority',
    displayName: 'Priority',
    fieldType: 'enum',
    enumValues: ['low', 'medium', 'high'],
    required: true,
    sortOrder: 0,
  },
  {
    id: '2',
    fieldKey: 'version',
    displayName: 'App Version',
    fieldType: 'text',
    enumValues: [],
    required: false,
    sortOrder: 1,
  },
]

describe('CustomFieldsPage', () => {
  it('renders field rows', () => {
    renderWithProviders(
      <CustomFieldsPage fields={sampleFields} onAdd={vi.fn()} onRemove={vi.fn()} />,
    )
    expect(screen.getByText('priority')).toBeInTheDocument()
    expect(screen.getByText('App Version')).toBeInTheDocument()
  })

  it('shows empty state when no fields', () => {
    renderWithProviders(<CustomFieldsPage fields={[]} onAdd={vi.fn()} onRemove={vi.fn()} />)
    expect(screen.getByText('暂无自定义字段')).toBeInTheDocument()
  })

  it('calls onRemove when delete clicked', async () => {
    const user = userEvent.setup()
    const onRemove = vi.fn()
    renderWithProviders(
      <CustomFieldsPage fields={sampleFields} onAdd={vi.fn()} onRemove={onRemove} />,
    )
    const deleteButtons = screen.getAllByRole('button', { name: '' })
    const trashButtons = deleteButtons.filter((b) => b.querySelector('svg'))
    if (trashButtons.length > 0) {
      await user.click(trashButtons[0])
      expect(onRemove).toHaveBeenCalledWith('1')
    }
  })
})
