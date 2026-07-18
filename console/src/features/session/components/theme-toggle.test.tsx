import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ThemeToggle } from '@/features/session/components/theme-toggle'
import { renderWithProviders, screen } from '@/testing/test-utils'

const setThemeMock = vi.fn()
let resolvedTheme = 'light'

vi.mock('next-themes', () => ({
  useTheme: () => ({
    resolvedTheme,
    setTheme: setThemeMock,
  }),
}))

describe('ThemeToggle', () => {
  beforeEach(() => {
    resolvedTheme = 'light'
    setThemeMock.mockReset()
  })

  it('switches from light to dark', async () => {
    const { user } = renderWithProviders(<ThemeToggle label="Toggle theme" />)

    await user.click(screen.getByRole('button', { name: 'Toggle theme' }))

    expect(setThemeMock).toHaveBeenCalledWith('dark')
  })

  it('switches from dark to light after mounting', async () => {
    resolvedTheme = 'dark'
    const { user } = renderWithProviders(<ThemeToggle label="Toggle theme" />)

    await user.click(screen.getByRole('button', { name: 'Toggle theme' }))

    expect(setThemeMock).toHaveBeenCalledWith('light')
  })
})
