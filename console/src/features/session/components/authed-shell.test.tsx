import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { AuthedShell } from '@/features/session/components/authed-shell'
import { expectNoA11yViolations } from '@/testing/a11y'
import { renderWithProviders, screen } from '@/testing/test-utils'

vi.mock('@tanstack/react-router', async () => {
  const actual =
    await vi.importActual<typeof import('@tanstack/react-router')>('@tanstack/react-router')
  return {
    ...actual,
    Link: ({
      children,
      className,
      onClick,
      to,
    }: {
      children: ReactNode
      className?: string
      onClick?: () => void
      to?: string
    }) => (
      <a href={to ?? '#'} className={className} onClick={onClick}>
        {children}
      </a>
    ),
    useNavigate: () => vi.fn(),
    useRouterState: ({
      select,
    }: {
      select: (state: { location: { pathname: string } }) => string
    }) => select({ location: { pathname: '/administration/gdpr' } }),
  }
})

vi.mock('@/features/session/api/logout', () => ({
  useLogout: () => ({
    mutate: vi.fn(),
    isPending: false,
  }),
}))

vi.mock('@/features/session/hooks/use-permissions', () => ({
  usePermissions: () => ({
    can: () => true,
    isAdmin: true,
    role: 'admin',
  }),
}))

vi.mock('@/features/session/components/theme-toggle', () => ({
  ThemeToggle: ({ label }: { label: string }) => <button type="button">{label}</button>,
}))

vi.mock('@/features/session/components/auth/role-badge', () => ({
  RoleBadge: ({ className, role }: { className?: string; role?: string }) => (
    <span className={className}>{role}</span>
  ),
}))

describe('AuthedShell', () => {
  it('renders the desktop sidebar as its own scroll container', () => {
    renderWithProviders(
      <AuthedShell
        me={{
          tenant: {
            id: 'tenant-1',
            name: 'Test Tenant',
            slug: 'test-tenant',
            locale: 'zh-CN',
            timezone: 'Asia/Shanghai',
          },
          user: {
            openId: 'user-1',
            name: 'admin@test.com',
            role: 'admin',
          },
          csrfToken: 'csrf-token',
        }}
      >
        <div>content</div>
      </AuthedShell>,
    )

    const sidebarScroll = screen.getByTestId('shell-sidebar-scroll')
    expect(sidebarScroll).toHaveClass('overflow-y-auto')
    expect(sidebarScroll).toHaveClass('overscroll-contain')
    expect(sidebarScroll.className).toContain('max-h-[calc(100dvh-7rem)]')
  })

  it('exposes a skip link and main-content target', async () => {
    const { container, user } = renderWithProviders(
      <AuthedShell
        me={{
          tenant: {
            id: 'tenant-1',
            name: 'Test Tenant',
            slug: 'test-tenant',
            locale: 'zh-CN',
            timezone: 'Asia/Shanghai',
          },
          user: {
            openId: 'user-1',
            name: 'admin@test.com',
            role: 'admin',
          },
          csrfToken: 'csrf-token',
        }}
      >
        <h1>content</h1>
      </AuthedShell>,
    )

    expect(screen.getByRole('link', { name: '跳到主要内容' })).toHaveAttribute(
      'href',
      '#main-content',
    )
    const skipLink = screen.getByRole('link', { name: '跳到主要内容' })
    const mainContent = document.getElementById('main-content')
    expect(mainContent).toHaveAttribute('tabindex', '-1')

    await user.click(skipLink)

    expect(window.location.hash).toBe('#main-content')
    expect(document.activeElement).toBe(mainContent)

    window.history.replaceState(null, '', '/')
    skipLink.focus()
    await user.keyboard('{Enter}')

    expect(window.location.hash).toBe('#main-content')
    expect(document.activeElement).toBe(mainContent)
    await expectNoA11yViolations(container)
  })

  it('keeps low-priority account chrome from widening the mobile header', () => {
    renderWithProviders(
      <AuthedShell
        me={{
          tenant: {
            id: 'tenant-1',
            name: 'Test Tenant',
            slug: 'test-tenant',
            locale: 'zh-CN',
            timezone: 'Asia/Shanghai',
          },
          user: {
            openId: 'user-1',
            name: 'admin@test.com',
            role: 'admin',
          },
          csrfToken: 'csrf-token',
        }}
      >
        <div>content</div>
      </AuthedShell>,
    )

    expect(screen.getByText('Test Tenant').parentElement).toHaveClass('flex-1')
    expect(screen.getByText('admin')).toHaveClass('hidden')
    expect(screen.getByText('admin')).toHaveClass('sm:inline-flex')
    expect(screen.getByText('admin@test.com')).toHaveClass('sr-only')
    expect(screen.getByText('admin@test.com')).toHaveClass('sm:not-sr-only')
    expect(screen.getByRole('button', { name: 'admin@test.com' })).toHaveClass('px-2')
  })
})
