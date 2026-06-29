import { screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders } from '@/testing/test-utils'
import type { PortalSettings } from './portal-config'
import { PortalConfig } from './portal-config'

const SETTINGS: PortalSettings = {
  enabled: true,
  slug: 'acme',
  brandColor: '#3b82f6',
  welcomeMessage: 'Share your feedback!',
}

describe('PortalConfig', () => {
  it('renders form with settings', () => {
    renderWithProviders(<PortalConfig settings={SETTINGS} />)
    expect(screen.getByDisplayValue('acme')).toBeInTheDocument()
    expect(screen.getByDisplayValue('Share your feedback!')).toBeInTheDocument()
  })

  it('shows portal URL when enabled', () => {
    renderWithProviders(<PortalConfig settings={SETTINGS} />)
    expect(screen.getByText('/portal/acme')).toBeInTheDocument()
  })

  it('calls onSave on submit', async () => {
    const onSave = vi.fn()
    renderWithProviders(<PortalConfig settings={SETTINGS} onSave={onSave} />)
    await userEvent.click(screen.getByRole('button', { name: '保存' }))
    expect(onSave).toHaveBeenCalledWith(SETTINGS)
  })
})
