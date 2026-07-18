import type { MouseEvent, ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AuthedShell } from '@/features/session/components/authed-shell'
import { expectNoA11yViolations } from '@/testing/a11y'
import { renderWithProviders, screen } from '@/testing/test-utils'

const routerMock = vi.hoisted(() => ({
  navigate: vi.fn(),
  pathname: '/administration/gdpr',
}))
const logoutMock = vi.hoisted(() => ({
  isPending: false,
  mutate: vi.fn(),
}))
const permissionsMock = vi.hoisted(() => ({
  can: vi.fn(() => true),
  isAdmin: true,
  role: 'admin',
}))

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
      <a
        href={to ?? '#'}
        className={className}
        onClick={(event: MouseEvent<HTMLAnchorElement>) => {
          event.preventDefault()
          onClick?.()
        }}
      >
        {children}
      </a>
    ),
    useNavigate: () => routerMock.navigate,
    useRouterState: ({
      select,
    }: {
      select: (state: { location: { pathname: string } }) => string
    }) => select({ location: { pathname: routerMock.pathname } }),
  }
})

vi.mock('@/features/session/api/logout', () => ({
  useLogout: () => logoutMock,
}))

vi.mock('@/features/session/hooks/use-permissions', () => ({
  usePermissions: () => permissionsMock,
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
  const me = {
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
  }

  beforeEach(() => {
    window.history.replaceState(null, '', '/')
    routerMock.navigate.mockClear()
    routerMock.pathname = '/administration/gdpr'
    logoutMock.isPending = false
    logoutMock.mutate.mockClear()
    permissionsMock.can.mockClear()
    permissionsMock.can.mockImplementation(() => true)
    permissionsMock.isAdmin = true
    permissionsMock.role = 'admin'
  })

  it('renders the desktop sidebar as its own scroll container', () => {
    renderWithProviders(
      <AuthedShell me={me}>
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
      <AuthedShell me={me}>
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
    await user.keyboard('{Space}')
    expect(window.location.hash).toBe('')

    await user.keyboard('{Enter}')

    expect(window.location.hash).toBe('#main-content')
    expect(document.activeElement).toBe(mainContent)

    const pushSpy = vi.spyOn(window.history, 'pushState')
    await user.click(skipLink)
    expect(pushSpy).not.toHaveBeenCalled()
    pushSpy.mockRestore()
    await expectNoA11yViolations(container)
  })

  it('does not change location when the skip target is missing', async () => {
    const { user } = renderWithProviders(
      <AuthedShell me={me}>
        <h1>content</h1>
      </AuthedShell>,
    )
    document.getElementById('main-content')?.remove()

    await user.click(screen.getByRole('link', { name: '跳到主要内容' }))

    expect(window.location.hash).toBe('')
  })

  it('keeps low-priority account chrome from widening the mobile header', () => {
    renderWithProviders(
      <AuthedShell me={me}>
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

  it('routes admin account menu actions through navigation and logout mutation', async () => {
    const { user } = renderWithProviders(
      <AuthedShell me={me}>
        <div>content</div>
      </AuthedShell>,
    )

    await user.click(screen.getByRole('button', { name: 'admin@test.com' }))
    await user.click(await screen.findByRole('menuitem', { name: /修改密码/ }))

    expect(routerMock.navigate).toHaveBeenCalledWith({ to: '/change-password' })

    await user.click(screen.getAllByRole('link', { name: '反馈' })[0])

    await user.click(screen.getByRole('button', { name: 'admin@test.com' }))
    await user.click(await screen.findByRole('menuitem', { name: /退出登录/ }))

    expect(logoutMock.mutate).toHaveBeenCalledWith(
      undefined,
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    )
    expect(logoutMock.mutate).toHaveBeenCalledTimes(1)
  })

  it('falls back to the feedback group label when the current path has no section match', () => {
    routerMock.pathname = '/unknown'

    renderWithProviders(
      <AuthedShell me={me}>
        <div>content</div>
      </AuthedShell>,
    )

    expect(screen.getAllByText('反馈工作台')).not.toHaveLength(0)
  })
})
