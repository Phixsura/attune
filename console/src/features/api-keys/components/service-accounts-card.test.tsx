import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { ServiceAccountsCard } from '@/features/api-keys/components/service-accounts-card'
import { expectNoA11yViolations } from '@/testing/a11y'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

describe('ServiceAccountsCard', () => {
  it('renders service accounts and toggles active state', async () => {
    let createBody: unknown
    let updateBodies: unknown[] = []
    let deleteIds: string[] = []
    let serviceAccounts = [
      {
        id: 'sa-1',
        name: 'ci-bot',
        description: 'deployment pipeline',
        isActive: true,
        createdAt: '2026-06-07T00:00:00Z',
        updatedAt: '2026-06-08T00:00:00Z',
      },
    ]

    server.use(
      http.get('/fb/v1/console/service-accounts', () =>
        HttpResponse.json({ items: serviceAccounts }),
      ),
      http.post('/fb/v1/console/service-accounts', async ({ request }) => {
        createBody = await request.json()
        const created = {
          id: 'sa-2',
          name: 'deploy-bot',
          description: 'deployment pipeline',
          isActive: true,
          createdAt: '2026-06-09T00:00:00Z',
          updatedAt: '2026-06-10T00:00:00Z',
        }
        serviceAccounts = [...serviceAccounts, created]
        return HttpResponse.json({ serviceAccount: created }, { status: 201 })
      }),
      http.patch('/fb/v1/console/service-accounts/:id', async ({ params, request }) => {
        const body = await request.json()
        updateBodies = [...updateBodies, { id: params.id, body }]
        const nextActive = (body as { isActive: boolean }).isActive
        const updated = {
          ...serviceAccounts[0],
          isActive: nextActive,
          updatedAt: nextActive ? '2026-06-11T00:00:00Z' : '2026-06-12T00:00:00Z',
        }
        serviceAccounts = [updated, ...serviceAccounts.slice(1)]
        return HttpResponse.json(updated)
      }),
      http.delete('/fb/v1/console/service-accounts/:id', ({ params }) => {
        deleteIds = [...deleteIds, String(params.id)]
        serviceAccounts = serviceAccounts.filter((account) => account.id !== params.id)
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const { container, user } = renderWithProviders(<ServiceAccountsCard canEdit={true} />)

    await waitFor(() => {
      expect(screen.getByText('ci-bot')).toBeInTheDocument()
    })
    expect(screen.getByText('服务账号目录')).toBeInTheDocument()
    expect(screen.getByText('统一查看 1 个服务账号，其中 1 个启用中。')).toBeInTheDocument()
    expect(screen.getByRole('table', { name: '服务账号列表' })).toBeInTheDocument()
    expect(screen.getByText('启用')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '停用服务账号 ci-bot' })).toBeInTheDocument()
    await expectNoA11yViolations(container)

    await user.click(screen.getByRole('button', { name: '停用服务账号 ci-bot' }))
    const disableDialog = screen.getByRole('alertdialog', { name: '停用服务账号 ci-bot？' })
    expect(within(disableDialog).getByRole('button', { name: '停用' })).toBeEnabled()
    await user.click(within(disableDialog).getByRole('button', { name: '停用' }))

    await waitFor(() => {
      expect(updateBodies).toEqual([{ id: 'sa-1', body: { isActive: false } }])
    })
    await waitFor(() => {
      expect(screen.getByText('统一查看 1 个服务账号，其中 0 个启用中。')).toBeInTheDocument()
    })
    expect(screen.getByText('停用')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '启用服务账号 ci-bot' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '启用服务账号 ci-bot' }))
    const enableDialog = screen.getByRole('alertdialog', { name: '启用服务账号 ci-bot？' })
    expect(within(enableDialog).getByRole('button', { name: '启用' })).toBeEnabled()
    await user.click(within(enableDialog).getByRole('button', { name: '启用' }))

    await waitFor(() => {
      expect(updateBodies).toEqual([
        { id: 'sa-1', body: { isActive: false } },
        { id: 'sa-1', body: { isActive: true } },
      ])
    })
    await waitFor(() => {
      expect(screen.getByText('统一查看 1 个服务账号，其中 1 个启用中。')).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: '停用服务账号 ci-bot' })).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '新增服务账号' }))
    const createDialog = screen.getByRole('dialog', { name: '新增服务账号' })
    expect(within(createDialog).getByLabelText('名称')).toHaveFocus()

    await user.type(within(createDialog).getByLabelText('名称'), '  deploy-bot  ')
    await user.type(within(createDialog).getByLabelText('说明'), '  deployment pipeline  ')
    await user.click(within(createDialog).getByTestId('create-service-account-submit'))

    await waitFor(() => {
      expect(createBody).toEqual({
        name: 'deploy-bot',
        description: 'deployment pipeline',
      })
    })
    await waitFor(() => {
      expect(screen.getByText('deploy-bot')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.getByText('统一查看 2 个服务账号，其中 2 个启用中。')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.queryByRole('dialog', { name: '新增服务账号' })).not.toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.getByRole('button', { name: '删除服务账号 ci-bot' })).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '删除服务账号 ci-bot' }))
    const deleteDialog = screen.getByRole('alertdialog', { name: '删除服务账号 ci-bot？' })
    expect(within(deleteDialog).getByRole('button', { name: '删除' })).toBeEnabled()
    await user.click(within(deleteDialog).getByRole('button', { name: '删除' }))

    await waitFor(() => {
      expect(deleteIds).toEqual(['sa-1'])
    })
    await waitFor(() => {
      expect(screen.getByText('统一查看 1 个服务账号，其中 1 个启用中。')).toBeInTheDocument()
    })
    expect(screen.queryByText('ci-bot')).not.toBeInTheDocument()
    expect(screen.getByText('deploy-bot')).toBeInTheDocument()
  })

  it('renders the empty state when no service accounts exist', async () => {
    server.use(http.get('/fb/v1/console/service-accounts', () => HttpResponse.json({ items: [] })))

    const { container } = renderWithProviders(<ServiceAccountsCard canEdit={true} />)

    await waitFor(() => {
      expect(screen.getByText('还没有服务账号')).toBeInTheDocument()
    })
    expect(screen.getByText('先创建一个服务账号，把自动化凭证和人类登录分开。')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: '新增服务账号' })).toHaveLength(2)

    await expectNoA11yViolations(container)
  })

  it('renders a retryable error state when loading fails', async () => {
    let attempts = 0
    server.use(
      http.get('/fb/v1/console/service-accounts', () => {
        attempts += 1
        return attempts === 1
          ? new HttpResponse(null, { status: 500 })
          : HttpResponse.json({ items: [] })
      }),
    )

    const { user } = renderWithProviders(<ServiceAccountsCard canEdit={true} />)

    await waitFor(() => {
      expect(screen.getByText('服务账号列表暂时无法加载')).toBeInTheDocument()
    })
    expect(
      screen.getByText('请稍后重试；如果问题持续，确认当前账号是否具备管理服务账号的权限。'),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '重试' })).toBeEnabled()

    await user.click(screen.getByRole('button', { name: '重试' }))

    await waitFor(() => {
      expect(screen.getByText('还没有服务账号')).toBeInTheDocument()
    })
    expect(attempts).toBe(2)
  })

  it('does not mount the panel for non-editors', () => {
    const { container } = renderWithProviders(<ServiceAccountsCard canEdit={false} />)

    expect(container).toBeEmptyDOMElement()
  })
})
