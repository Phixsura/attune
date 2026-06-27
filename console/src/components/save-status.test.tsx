import { describe, expect, it } from 'vitest'
import { renderWithProviders, screen } from '@/testing/test-utils'
import { SaveStatus } from './save-status'

describe('SaveStatus', () => {
  it('returns null when clean and never saved', () => {
    const { container } = renderWithProviders(
      <SaveStatus dirty={false} saving={false} lastSavedAt={null} />,
    )
    expect(container.firstChild).toBeNull()
  })

  it('shows saving spinner', () => {
    renderWithProviders(<SaveStatus dirty={false} saving={true} lastSavedAt={null} />)
    expect(screen.getByText('保存中…')).toBeInTheDocument()
  })

  it('shows unsaved indicator when dirty', () => {
    renderWithProviders(<SaveStatus dirty={true} saving={false} lastSavedAt={null} />)
    expect(screen.getByText(/未保存/)).toBeInTheDocument()
  })

  it('shows saved-at timestamp', () => {
    const at = new Date(2026, 5, 27, 14, 30)
    renderWithProviders(<SaveStatus dirty={false} saving={false} lastSavedAt={at} />)
    expect(screen.getByText(/已保存于/)).toBeInTheDocument()
  })

  it('saving takes priority over dirty', () => {
    renderWithProviders(<SaveStatus dirty={true} saving={true} lastSavedAt={null} />)
    expect(screen.getByText('保存中…')).toBeInTheDocument()
    expect(screen.queryByText(/未保存/)).not.toBeInTheDocument()
  })

  it('dirty takes priority over lastSavedAt', () => {
    const at = new Date()
    renderWithProviders(<SaveStatus dirty={true} saving={false} lastSavedAt={at} />)
    expect(screen.getByText(/未保存/)).toBeInTheDocument()
    expect(screen.queryByText(/已保存于/)).not.toBeInTheDocument()
  })

  it('hides bullet character from screen readers', () => {
    renderWithProviders(<SaveStatus dirty={true} saving={false} lastSavedAt={null} />)
    const bullet = document.querySelector('[aria-hidden="true"]')
    expect(bullet).not.toBeNull()
    expect(bullet?.textContent).toBe('●')
  })

  it('has aria-live on all states', () => {
    const { rerender } = renderWithProviders(
      <SaveStatus dirty={true} saving={false} lastSavedAt={null} />,
    )
    expect(screen.getByText(/未保存/).closest('[aria-live]')).not.toBeNull()

    rerender(<SaveStatus dirty={false} saving={true} lastSavedAt={null} />)
    expect(screen.getByText('保存中…').closest('[aria-live]')).not.toBeNull()
  })
})
