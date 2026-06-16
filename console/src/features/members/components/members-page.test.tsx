// SPDX-License-Identifier: Apache-2.0

import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { MembersPage } from '@/features/members/components/members-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

// Mock usePermissions hook
vi.mock('@/features/session/hooks/use-permissions', () => ({
  usePermissions: () => ({
    role: 'admin',
    userId: 'current-user',
    isAdmin: true,
    isMember: false,
    isViewer: false,
    can: () => true,
    canView: () => true,
    canEdit: () => true,
    canManage: () => true,
    canDelete: () => true,
  }),
}))

// i18n translations are in zh-CN
const TEXT = {
  invite: '邀请成员',
  loading: '加载中…',
  pending: '待确认',
  noMembers: '暂无成员',
}

function setupMembersResponse(members: unknown[]) {
  server.use(http.get('/fb/v1/console/members', () => HttpResponse.json({ members })))
}

const mockMembers = [
  {
    id: 'm1',
    memberType: 'admin',
    userId: 'admin-user',
    email: 'admin@example.com',
    role: 'admin',
    roleSource: 'bootstrap',
    invitedAt: '1700000000',
    acceptedAt: '1700000001',
  },
  {
    id: 'm2',
    memberType: 'oidc_user',
    userId: 'member-user',
    email: 'member@example.com',
    role: 'member',
    roleSource: 'idp',
    invitedAt: '1700000000',
    acceptedAt: '1700000002',
  },
  {
    id: 'm3',
    memberType: 'invite',
    userId: '',
    email: 'pending@example.com',
    role: 'viewer',
    roleSource: 'manual',
    invitedAt: '1700000000',
    acceptedAt: '0',
  },
]

describe('MembersPage', () => {
  it('renders member list with emails', async () => {
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })
    expect(screen.getByText('member@example.com')).toBeInTheDocument()
    expect(screen.getByText('pending@example.com')).toBeInTheDocument()
  })

  it('shows pending badge for invite members', async () => {
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('pending@example.com')).toBeInTheDocument()
    })
    // The pending member should show pending text (待确认)
    const pendingTexts = screen.getAllByText(TEXT.pending)
    expect(pendingTexts.length).toBeGreaterThan(0)
  })

  it('shows invite button for admin users', async () => {
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })
    // Admin should see the invite button (邀请成员)
    expect(screen.getByText(TEXT.invite)).toBeInTheDocument()
  })

  it('shows loading state initially', () => {
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    // Should show loading indicator before data loads (加载中)
    expect(screen.getByText(TEXT.loading)).toBeInTheDocument()
  })

  it('opens invite dialog when invite button clicked', async () => {
    setupMembersResponse(mockMembers)
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByText(TEXT.invite))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })

  it('displays member types correctly', async () => {
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    // Check member type badges are present
    expect(screen.getByText('oidc_user')).toBeInTheDocument()
    expect(screen.getByText('invite')).toBeInTheDocument()
  })
})

describe('MembersPage invite dialog', () => {
  it('opens dialog and shows email input', async () => {
    setupMembersResponse(mockMembers)
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByText(TEXT.invite))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    // Dialog should have email input
    expect(screen.getByPlaceholderText('user@example.com')).toBeInTheDocument()
  })

  it('closes dialog on cancel', async () => {
    setupMembersResponse(mockMembers)
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByText(TEXT.invite))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    // Click cancel button (取消)
    await user.click(screen.getByText('取消'))

    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })
  })
})
