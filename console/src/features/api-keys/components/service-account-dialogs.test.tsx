import { describe, expect, it, vi } from 'vitest'
import { fireEvent, renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'
import { ServiceAccountDeleteDialog } from './service-account-delete-dialog'
import { CreateServiceAccountDialog } from './service-account-dialog'
import { ServiceAccountStatusDialog } from './service-account-status-dialog'

describe('service account dialogs', () => {
  it('submits a trimmed name and omits blank descriptions', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const onOpenChange = vi.fn()
    const { user } = renderWithProviders(
      <CreateServiceAccountDialog
        open={true}
        onOpenChange={onOpenChange}
        onSubmit={onSubmit}
        pending={false}
      />,
    )

    const dialog = screen.getByRole('dialog', { name: '新增服务账号' })
    await user.type(within(dialog).getByLabelText('名称'), '  ci-bot  ')
    await user.type(within(dialog).getByLabelText('说明'), '   ')
    await user.click(within(dialog).getByTestId('create-service-account-submit'))

    await waitFor(() =>
      expect(onSubmit).toHaveBeenCalledWith({ name: 'ci-bot', description: undefined }),
    )
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  })

  it('keeps the create dialog open when submission rejects', async () => {
    const onSubmit = vi.fn().mockRejectedValue(new Error('boom'))
    const onOpenChange = vi.fn()
    const { user } = renderWithProviders(
      <CreateServiceAccountDialog
        open={true}
        onOpenChange={onOpenChange}
        onSubmit={onSubmit}
        pending={false}
      />,
    )

    await user.type(screen.getByLabelText('名称'), 'ci-bot')
    await user.click(screen.getByTestId('create-service-account-submit'))

    await waitFor(() => expect(onSubmit).toHaveBeenCalledOnce())
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it('ignores defensive create submits without a service account name', () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    renderWithProviders(
      <CreateServiceAccountDialog
        open={true}
        onOpenChange={vi.fn()}
        onSubmit={onSubmit}
        pending={false}
      />,
    )

    const form = screen.getByTestId('create-service-account-submit').closest('form')
    expect(form).not.toBeNull()
    fireEvent.submit(form as HTMLFormElement)

    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('disables create controls while pending', () => {
    renderWithProviders(
      <CreateServiceAccountDialog
        open={true}
        onOpenChange={vi.fn()}
        onSubmit={vi.fn()}
        pending={true}
      />,
    )

    expect(screen.getByLabelText('名称')).toBeDisabled()
    expect(screen.getByLabelText('说明')).toBeDisabled()
    expect(screen.getByTestId('create-service-account-submit')).toBeDisabled()
    expect(
      screen.getByTestId('create-service-account-submit').querySelector('.animate-spin'),
    ).toBeInTheDocument()
    expect(screen.getByTestId('create-service-account-cancel')).toBeDisabled()
  })

  it('cancels create dialog directly and resets fields when Radix closes it', async () => {
    const onOpenChange = vi.fn()
    const { user } = renderWithProviders(
      <CreateServiceAccountDialog
        open={true}
        onOpenChange={onOpenChange}
        onSubmit={vi.fn()}
        pending={false}
      />,
    )

    await user.type(screen.getByLabelText('名称'), 'ci-bot')
    await user.type(screen.getByLabelText('说明'), 'temporary bot')
    await user.click(screen.getByTestId('create-service-account-cancel'))
    expect(onOpenChange).toHaveBeenCalledWith(false)

    await user.keyboard('{Escape}')
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    expect(screen.getByLabelText('名称')).toHaveValue('')
    expect(screen.getByLabelText('说明')).toHaveValue('')
  })

  it('cancels and handles rejected delete confirmations without closing', async () => {
    const onOpenChange = vi.fn()
    const onConfirm = vi.fn().mockRejectedValue(new Error('delete failed'))
    const { user } = renderWithProviders(
      <ServiceAccountDeleteDialog
        open={true}
        onOpenChange={onOpenChange}
        serviceAccountName="ci-bot"
        onConfirm={onConfirm}
        pending={false}
      />,
    )

    const dialog = screen.getByRole('alertdialog', { name: '删除服务账号 ci-bot？' })
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)

    await user.click(within(dialog).getByRole('button', { name: '删除' }))
    await waitFor(() => expect(onConfirm).toHaveBeenCalledOnce())
    expect(onOpenChange).toHaveBeenCalledTimes(1)
  })

  it('closes delete dialog after a successful confirmation and shows pending state', async () => {
    const pendingOpenChange = vi.fn()
    renderWithProviders(
      <ServiceAccountDeleteDialog
        open={true}
        onOpenChange={pendingOpenChange}
        serviceAccountName="pending-bot"
        onConfirm={vi.fn()}
        pending={true}
      />,
    )
    const pendingDelete = screen.getByRole('button', { name: '删除' })
    expect(pendingDelete).toBeDisabled()
    expect(pendingDelete.querySelector('.animate-spin')).toBeInTheDocument()

    const onOpenChange = vi.fn()
    const onConfirm = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <ServiceAccountDeleteDialog
        open={true}
        onOpenChange={onOpenChange}
        serviceAccountName="ci-bot"
        onConfirm={onConfirm}
        pending={false}
      />,
    )

    await user.click(
      within(screen.getByRole('alertdialog', { name: '删除服务账号 ci-bot？' })).getByRole(
        'button',
        { name: '删除' },
      ),
    )
    await waitFor(() => expect(onConfirm).toHaveBeenCalledOnce())
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  })

  it('renders pending enable state and handles rejected status confirmations', async () => {
    const pendingOpenChange = vi.fn()
    renderWithProviders(
      <ServiceAccountStatusDialog
        open={true}
        onOpenChange={pendingOpenChange}
        serviceAccountName="deploy-bot"
        nextActive={true}
        onConfirm={vi.fn()}
        pending={true}
      />,
    )
    expect(
      screen.getByRole('alertdialog', { name: '启用服务账号 deploy-bot？' }),
    ).toBeInTheDocument()
    const pendingEnable = screen.getByRole('button', { name: '启用' })
    expect(pendingEnable).toBeDisabled()
    expect(pendingEnable.querySelector('.animate-spin')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '取消' })).toBeDisabled()

    const onOpenChange = vi.fn()
    const onConfirm = vi.fn().mockRejectedValue(new Error('status failed'))
    const { user } = renderWithProviders(
      <ServiceAccountStatusDialog
        open={true}
        onOpenChange={onOpenChange}
        serviceAccountName="ci-bot"
        nextActive={false}
        onConfirm={onConfirm}
        pending={false}
      />,
    )

    const disableDialog = screen.getByRole('alertdialog', { name: '停用服务账号 ci-bot？' })
    await user.click(within(disableDialog).getByRole('button', { name: '停用' }))
    await waitFor(() => expect(onConfirm).toHaveBeenCalledOnce())
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
  })

  it('cancels and closes status dialog after successful confirmation', async () => {
    const onOpenChange = vi.fn()
    const onConfirm = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <ServiceAccountStatusDialog
        open={true}
        onOpenChange={onOpenChange}
        serviceAccountName="ci-bot"
        nextActive={false}
        onConfirm={onConfirm}
        pending={false}
      />,
    )

    const dialog = screen.getByRole('alertdialog', { name: '停用服务账号 ci-bot？' })
    await user.click(within(dialog).getByRole('button', { name: '取消' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
    onOpenChange.mockClear()

    await user.keyboard('{Escape}')
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
    onOpenChange.mockClear()

    await user.click(within(dialog).getByRole('button', { name: '停用' }))
    await waitFor(() => expect(onConfirm).toHaveBeenCalledOnce())
    await waitFor(() => expect(onOpenChange).toHaveBeenCalledWith(false))
  })
})
