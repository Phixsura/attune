import { describe, expect, it } from 'vitest'
import { renderWithProviders, screen } from '@/testing/test-utils'
import { SaveStatus } from './save-status'

describe('SaveStatus', () => {
  it('returns null when clean', () => {
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

  it('saving takes priority over dirty', () => {
    renderWithProviders(<SaveStatus dirty={true} saving={true} lastSavedAt={null} />)
    expect(screen.getByText('保存中…')).toBeInTheDocument()
    expect(screen.queryByText(/未保存/)).not.toBeInTheDocument()
  })

  it('returns null when saved (no persistent timestamp shown)', () => {
    const at = new Date()
    const { container } = renderWithProviders(
      <SaveStatus dirty={false} saving={false} lastSavedAt={at} />,
    )
    expect(container.firstChild).toBeNull()
  })

  it('hides bullet character from screen readers', () => {
    renderWithProviders(<SaveStatus dirty={true} saving={false} lastSavedAt={null} />)
    const bullet = document.querySelector('[aria-hidden="true"]')
    expect(bullet).not.toBeNull()
    expect(bullet?.textContent).toBe('●')
  })
})
