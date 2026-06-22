import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { AuthedShell } from '@/features/session/components/authed-shell'
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
  RoleBadge: ({ role }: { role?: string }) => <span>{role}</span>,
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
})
