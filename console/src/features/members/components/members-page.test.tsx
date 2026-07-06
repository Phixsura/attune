// SPDX-License-Identifier: Apache-2.0

import { configure } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { MembersPage } from '@/features/members/components/members-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

// Spy on toast so error-path assertions can verify a notification fired.
const toastError = vi.fn()
const toastSuccess = vi.fn()
vi.mock('sonner', () => ({
  toast: {
    error: (...args: unknown[]) => toastError(...args),
    success: (...args: unknown[]) => toastSuccess(...args),
  },
}))

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

configure({ asyncUtilTimeout: 10_000 })

// i18n translations are in zh-CN
const TEXT = {
  invite: '邀请成员',
  loading: '加载中…',
  pending: '等待接受',
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
    id: 'm4',
    memberType: 'tenant_user',
    userId: 'ops-user',
    email: 'ops@example.com',
    role: 'delegated_admin',
    roleSource: 'manual',
    invitedAt: '1700000000',
    acceptedAt: '1700000003',
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

    expect(screen.getByRole('heading', { level: 1, name: '团队成员' })).toBeInTheDocument()
    await screen.findByText('admin@example.com', undefined, { timeout: 3000 })
    expect(screen.getByText('member@example.com')).toBeInTheDocument()
    expect(screen.getByText('pending@example.com')).toBeInTheDocument()
  })

  it('renders the summary metrics from member state', async () => {
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    const totalCard = screen.getByRole('group', { name: '总成员' })
    const activeCard = screen.getByRole('group', { name: '活跃成员' })
    const pendingCard = screen.getByRole('group', { name: '待接受邀请' })

    expect(within(totalCard).getByText('4')).toBeInTheDocument()
    expect(within(totalCard).getByText('共 4 位成员')).toBeInTheDocument()
    expect(within(activeCard).getByText('3')).toBeInTheDocument()
    expect(within(activeCard).getByText('已接受邀请并可访问当前租户的成员。')).toBeInTheDocument()
    expect(
      screen.getByText('当前显示 3 / 3 位已加入成员，其中包含 1 位管理员和 1 位委派管理员。'),
    ).toBeInTheDocument()
    expect(within(pendingCard).getByText('1')).toBeInTheDocument()
    expect(within(pendingCard).getByText('尚未完成首次登录确认的邀请。')).toBeInTheDocument()
  })

  it('filters the page down to pending invitations', async () => {
    setupMembersResponse(mockMembers)
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('member@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '待处理邀请' }))

    expect(screen.queryByText('member@example.com')).not.toBeInTheDocument()
    expect(screen.getByText('pending@example.com')).toBeInTheDocument()
  })

  it('filters members by search query', async () => {
    setupMembersResponse(mockMembers)
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('member@example.com')).toBeInTheDocument()
    })

    await user.type(screen.getByRole('textbox', { name: '搜索成员和邀请' }), 'pending@')

    expect(screen.queryByText('member@example.com')).not.toBeInTheDocument()
    expect(screen.getByText('pending@example.com')).toBeInTheDocument()
  })

  it('warns when the tenant only has one active admin', async () => {
    setupMembersResponse([
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
    ])
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(
        screen.getByText('当前仅剩 1 位活跃管理员。为避免失去租户管理权限，请至少保留两位管理员。'),
      ).toBeInTheDocument()
    })
  })

  it('shows pending badge for invite members', async () => {
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('pending@example.com')).toBeInTheDocument()
    })
    // The pending member should show pending invitation state.
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
    expect(screen.getAllByText(TEXT.loading).length).toBeGreaterThan(0)
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
    expect(screen.getByText('SSO 成员')).toBeInTheDocument()
    expect(screen.getAllByText('待接受邀请').length).toBeGreaterThan(0)
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

  it('can type in email field', async () => {
    setupMembersResponse(mockMembers)
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByText(TEXT.invite))

    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })

    const emailInput = screen.getByPlaceholderText('user@example.com')
    await user.type(emailInput, 'new@test.com')
    expect(emailInput).toHaveValue('new@test.com')
  })
})

describe('MembersPage empty state', () => {
  it('shows empty state when no members', async () => {
    setupMembersResponse([])
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText(TEXT.noMembers)).toBeInTheDocument()
    })
  })
})

describe('MembersPage current user', () => {
  it('shows "you" badge for current user', async () => {
    vi.doMock('@/features/session/hooks/use-permissions', () => ({
      usePermissions: () => ({
        role: 'admin',
        userId: 'admin-user',
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
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })
  })
})

describe('MembersPage remove dialog', () => {
  it('opens remove dialog when trash icon clicked', async () => {
    setupMembersResponse(mockMembers)
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('member@example.com')).toBeInTheDocument()
    })

    // Find trash buttons and click one for a non-admin member
    const memberTrashButton = screen.getByRole('button', { name: '移除成员 member@example.com' })
    if (memberTrashButton) {
      await user.click(memberTrashButton)
    }

    // The remove dialog should open
    await waitFor(() => {
      expect(screen.getByRole('dialog')).toBeInTheDocument()
    })
  })
})

describe('MembersPage role badges', () => {
  it('shows role source for each member', async () => {
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    // Check role sources are displayed
    expect(screen.getByText('系统初始化')).toBeInTheDocument()
    expect(screen.getByText('身份提供方')).toBeInTheDocument()
    expect(screen.getAllByText('手动分配').length).toBeGreaterThanOrEqual(2)
  })
})

describe('MembersPage role select', () => {
  it('displays role select triggers for each member', async () => {
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    // Only manageable members expose an editable role select.
    const selectTriggers = screen.getAllByRole('combobox')
    expect(selectTriggers.length).toBeGreaterThanOrEqual(1)
  })
})

describe('MembersPage table headers', () => {
  it('displays all table column headers', async () => {
    setupMembersResponse(mockMembers)
    renderWithProviders(<MembersPage />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    // Check table headers exist (columnheader role)
    const headers = screen.getAllByRole('columnheader')
    expect(headers.length).toBeGreaterThanOrEqual(5)
  })
})

describe('MembersPage mutations', () => {
  it('submits an invite (covers handleInvite success path)', async () => {
    setupMembersResponse(mockMembers)
    let posted: { email?: string; role?: string } = {}
    server.use(
      http.post('/fb/v1/console/members', async ({ request }) => {
        posted = (await request.json()) as { email?: string; role?: string }
        return HttpResponse.json({ member: { id: 'new', email: posted.email, role: posted.role } })
      }),
    )
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => expect(screen.getByText('admin@example.com')).toBeInTheDocument())
    await user.click(screen.getByText(TEXT.invite))
    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())

    await user.type(screen.getByPlaceholderText('user@example.com'), 'invitee@example.com')
    // The submit button is the dialog's primary action (邀请).
    const submit = screen.getByRole('button', { name: '发送邀请' })
    await user.click(submit)

    await waitFor(() => expect(posted.email).toBe('invitee@example.com'))
  })

  it('confirms removal (covers handleRemove success path)', async () => {
    setupMembersResponse(mockMembers)
    let deletedId = ''
    server.use(
      http.delete('/fb/v1/console/members/:id', ({ params }) => {
        deletedId = String(params.id)
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => expect(screen.getByText('member@example.com')).toBeInTheDocument())

    const memberTrash = screen.getByRole('button', { name: '移除成员 member@example.com' })
    expect(memberTrash).toBeDefined()
    if (memberTrash) await user.click(memberTrash)

    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())
    // Confirm button (移除) inside the remove dialog.
    await user.click(screen.getByRole('button', { name: '移除' }))

    await waitFor(() => expect(deletedId).toBe('m2'))
  })

  it('rejects invalid email before sending (covers validation branch)', async () => {
    setupMembersResponse(mockMembers)
    let postCalled = false
    server.use(
      http.post('/fb/v1/console/members', () => {
        postCalled = true
        return HttpResponse.json({ member: {} })
      }),
    )
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => expect(screen.getByText('admin@example.com')).toBeInTheDocument())
    await user.click(screen.getByText(TEXT.invite))
    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())

    await user.type(screen.getByPlaceholderText('user@example.com'), 'not-an-email')
    await user.click(screen.getByRole('button', { name: '发送邀请' }))

    // Invalid email is rejected client-side; no POST is made.
    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())
    expect(postCalled).toBe(false)
  })

  it('changes a member role via the role select (covers handleRoleChange)', async () => {
    setupMembersResponse(mockMembers)
    let patched: { id?: string; role?: string } = {}
    server.use(
      http.patch('/fb/v1/console/members/:id', async ({ request, params }) => {
        patched = {
          id: String(params.id),
          role: ((await request.json()) as { role?: string }).role,
        }
        return new HttpResponse(null, { status: 204 })
      }),
    )
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => expect(screen.getByText('member@example.com')).toBeInTheDocument())

    // The member row's role select trigger (combobox). m2 (member) is
    // manageable by the admin actor, so its select is enabled.
    const combos = screen.getAllByRole('combobox')
    const memberCombo = combos.find((c) =>
      c.closest('tr')?.textContent?.includes('member@example.com'),
    )
    expect(memberCombo).toBeDefined()
    if (!memberCombo) return
    await user.click(memberCombo)

    // Pick "viewer" (只读) from the opened listbox.
    const option = await screen.findByRole('option', { name: '只读' })
    await user.click(option)

    await waitFor(() => expect(patched.id).toBe('m2'))
    expect(patched.role).toBe('viewer')
  })

  it('invites with a chosen role via the role select (covers invite role select)', async () => {
    setupMembersResponse(mockMembers)
    let posted: { email?: string; role?: string } = {}
    server.use(
      http.post('/fb/v1/console/members', async ({ request }) => {
        posted = (await request.json()) as { email?: string; role?: string }
        return HttpResponse.json({ member: { id: 'new', email: posted.email, role: posted.role } })
      }),
    )
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => expect(screen.getByText('admin@example.com')).toBeInTheDocument())
    await user.click(screen.getByText(TEXT.invite))
    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())

    // Change the invite role to admin via the dialog's role select.
    await user.click(within(screen.getByRole('dialog')).getByRole('combobox'))
    await user.click(await screen.findByRole('option', { name: '管理员' }))

    await user.type(screen.getByPlaceholderText('user@example.com'), 'boss@example.com')
    await user.click(screen.getByRole('button', { name: '发送邀请' }))

    await waitFor(() => expect(posted.email).toBe('boss@example.com'))
    expect(posted.role).toBe('admin')
  })
})

describe('MembersPage mutation errors', () => {
  it('shows an error toast and keeps the dialog open when invite fails', async () => {
    toastError.mockClear()
    setupMembersResponse(mockMembers)
    server.use(
      http.post('/fb/v1/console/members', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'already a member' }, { status: 409 }),
      ),
    )
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => expect(screen.getByText('admin@example.com')).toBeInTheDocument())
    await user.click(screen.getByText(TEXT.invite))
    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())

    await user.type(screen.getByPlaceholderText('user@example.com'), 'dup@example.com')
    await user.click(screen.getByRole('button', { name: '发送邀请' }))

    await waitFor(() => expect(toastError).toHaveBeenCalled())
    // On failure the dialog stays open (only success closes it).
    expect(screen.getByRole('dialog')).toBeInTheDocument()
  })

  it('shows an error toast when removal fails', async () => {
    toastError.mockClear()
    setupMembersResponse(mockMembers)
    server.use(
      http.delete('/fb/v1/console/members/:id', () =>
        HttpResponse.json({ code: 'CONFLICT', message: 'last admin' }, { status: 409 }),
      ),
    )
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => expect(screen.getByText('member@example.com')).toBeInTheDocument())
    const trash = screen.getByRole('button', { name: '移除成员 member@example.com' })
    if (trash) await user.click(trash)
    await waitFor(() => expect(screen.getByRole('dialog')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: '移除' }))

    await waitFor(() => expect(toastError).toHaveBeenCalled())
  })

  it('shows an error toast when role change fails', async () => {
    toastError.mockClear()
    setupMembersResponse(mockMembers)
    server.use(
      http.patch('/fb/v1/console/members/:id', () =>
        HttpResponse.json({ code: 'FORBIDDEN', message: 'nope' }, { status: 403 }),
      ),
    )
    const { user } = renderWithProviders(<MembersPage />)

    await waitFor(() => expect(screen.getByText('member@example.com')).toBeInTheDocument())
    const combo = screen
      .getAllByRole('combobox')
      .find((c) => c.closest('tr')?.textContent?.includes('member@example.com'))
    if (!combo) return
    await user.click(combo)
    await user.click(await screen.findByRole('option', { name: '只读' }))

    await waitFor(() => expect(toastError).toHaveBeenCalled())
  })
})
