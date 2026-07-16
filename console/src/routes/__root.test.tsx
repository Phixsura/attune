import { render, screen } from '@testing-library/react'
import type { ComponentType } from 'react'
import { describe, expect, it, vi } from 'vitest'

vi.mock('@tanstack/react-router', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-router')>('@tanstack/react-router')
  return {
    ...actual,
    Outlet: () => <div data-testid="root-outlet" />,
  }
})

import { queryClient, Route as RootRoute } from '@/routes/__root'

describe('__root route', () => {
  it('renders the app providers around the route outlet', () => {
    const Component = RootRoute.options.component as ComponentType

    render(<Component />)

    expect(screen.getByTestId('root-outlet')).toBeInTheDocument()
  })

  it('exports the shared query client defaults used by loaders', () => {
    const defaults = queryClient.getDefaultOptions().queries

    expect(defaults?.refetchOnWindowFocus).toBe(true)
    expect(defaults?.retry).toBe(1)
    expect(defaults?.staleTime).toBe(30_000)
  })
})
