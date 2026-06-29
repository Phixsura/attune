import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import { ResponseTemplatesMenu } from './response-templates-menu'

describe('ResponseTemplatesMenu', () => {
  it('renders trigger button with count', () => {
    renderWithProviders(
      <ResponseTemplatesMenu
        templates={[
          { id: '1', name: 'T1', content: 'c1', createdAt: '' },
          { id: '2', name: 'T2', content: 'c2', createdAt: '' },
        ]}
        onInsert={vi.fn()}
        onSave={vi.fn()}
        onDelete={vi.fn()}
        currentContent="test"
      />,
    )
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('calls onInsert when template clicked', async () => {
    const onInsert = vi.fn()
    const user = userEvent.setup()
    renderWithProviders(
      <ResponseTemplatesMenu
        templates={[{ id: '1', name: 'Greeting', content: 'Hello!', createdAt: '' }]}
        onInsert={onInsert}
        onSave={vi.fn()}
        onDelete={vi.fn()}
        currentContent="test"
      />,
    )
    await user.click(screen.getByRole('button'))
    await user.click(screen.getByText('Greeting'))
    expect(onInsert).toHaveBeenCalledWith('Hello!')
  })
})
