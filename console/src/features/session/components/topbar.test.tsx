import type { ReactNode } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { SessionMe } from '@/features/session/api/get-me'
import { TopBar } from '@/features/session/components/topbar'
import i18n from '@/i18n'
import { renderWithProviders, screen } from '@/testing/test-utils'

const mocks = vi.hoisted(() => ({
  can: vi.fn(),
  isAdmin: true,
  logoutIsPending: false,
  mutate: vi.fn(),
  navigate: vi.fn(),
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
      to,
    }: {
      children: ReactNode
      className?: string
      to?: string
    }) => (
      <a href={to ?? '#'} className={className}>
        {children}
      </a>
    ),
    useNavigate: () => mocks.navigate,
  }
})

vi.mock('@/features/session/api/logout', () => ({
  useLogout: () => ({
    isPending: mocks.logoutIsPending,
    mutate: mocks.mutate,
  }),
}))

vi.mock('@/features/session/hooks/use-permissions', () => ({
  usePermissions: () => ({
    can: mocks.can,
    isAdmin: mocks.isAdmin,
    role: mocks.role,
  }),
}))

vi.mock('@/features/session/components/auth/role-badge', () => ({
  RoleBadge: ({ role }: { role?: string }) => <span>{role}</span>,
}))

const me: SessionMe = {
  tenant: {
    id: 'tenant-1',
    name: 'Acme Tenant',
    slug: 'acme',
    locale: 'zh-CN',
    timezone: 'Asia/Shanghai',
  },
  user: {
    openId: 'user-1',
    name: 'Ada Lovelace',
    role: 'admin',
  },
  csrfToken: 'csrf-token',
}

describe('TopBar', () => {
  beforeEach(() => {
    mocks.can.mockReset()
    mocks.can.mockReturnValue(true)
    mocks.isAdmin = true
    mocks.logoutIsPending = false
    mocks.mutate.mockReset()
    mocks.navigate.mockReset()
    mocks.role = 'admin'
  })

  it('renders admin navigation and dispatches menu actions', async () => {
    const { user } = renderWithProviders(<TopBar me={me} />)

    expect(screen.getByText('Acme Tenant')).toBeInTheDocument()
    expect(screen.getByText('Ada Lovelace')).toBeInTheDocument()
    expect(screen.getByText(i18n.t('nav.llm_config'))).toBeInTheDocument()
    expect(screen.getByText(i18n.t('nav.outbox_dead'))).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Ada Lovelace/ }))
    await user.click(screen.getByText(i18n.t('auth.change_password.menu')))

    expect(mocks.navigate).toHaveBeenCalledWith({ to: '/change-password' })

    await user.click(screen.getByRole('button', { name: /Ada Lovelace/ }))
    await user.click(screen.getByText(i18n.t('auth.logout')))

    expect(mocks.mutate).toHaveBeenCalledWith(
      undefined,
      expect.objectContaining({ onSuccess: expect.any(Function) }),
    )
  })

  it('hides admin-only links and disables logout while pending', async () => {
    mocks.can.mockReturnValue(false)
    mocks.isAdmin = false
    mocks.logoutIsPending = true
    mocks.role = 'member'

    const { user } = renderWithProviders(<TopBar me={me} />)

    expect(screen.queryByText(i18n.t('nav.llm_config'))).not.toBeInTheDocument()
    expect(screen.queryByText(i18n.t('nav.outbox_dead'))).not.toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: /Ada Lovelace/ }))

    expect(screen.queryByText(i18n.t('auth.change_password.menu'))).not.toBeInTheDocument()
    expect(screen.getByText(i18n.t('auth.logout'))).toHaveAttribute('aria-disabled', 'true')
  })
})
