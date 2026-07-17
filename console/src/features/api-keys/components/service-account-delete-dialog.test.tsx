import { useRef, useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { ServiceAccountDeleteDialog } from '@/features/api-keys/components/service-account-delete-dialog'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('ServiceAccountDeleteDialog', () => {
  it('confirms deletion and closes after the promise resolves', async () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined)
    const onOpenChange = vi.fn()
    const { user } = renderWithProviders(
      <ServiceAccountDeleteDialog
        open
        onOpenChange={onOpenChange}
        serviceAccountName="ci-bot"
        onConfirm={onConfirm}
        pending={false}
      />,
    )

    expect(screen.getByRole('alertdialog', { name: '删除服务账号 ci-bot？' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '删除' }))

    await waitFor(() => expect(onConfirm).toHaveBeenCalledTimes(1))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('keeps the dialog open when deletion fails', async () => {
    const onConfirm = vi.fn().mockRejectedValue(new Error('delete failed'))
    const onOpenChange = vi.fn()
    const { user } = renderWithProviders(
      <ServiceAccountDeleteDialog
        open
        onOpenChange={onOpenChange}
        serviceAccountName="ci-bot"
        onConfirm={onConfirm}
        pending={false}
      />,
    )

    await user.click(screen.getByRole('button', { name: '删除' }))

    await waitFor(() => expect(onConfirm).toHaveBeenCalledTimes(1))
    expect(onOpenChange).not.toHaveBeenCalledWith(false)
    expect(screen.getByRole('alertdialog', { name: '删除服务账号 ci-bot？' })).toBeInTheDocument()
  })

  it('reports dialog close changes and disables actions while pending', async () => {
    const onOpenChange = vi.fn()
    const { user } = renderWithProviders(
      <ServiceAccountDeleteDialog
        open
        onOpenChange={onOpenChange}
        serviceAccountName="ci-bot"
        onConfirm={vi.fn()}
        pending
      />,
    )

    expect(screen.getByRole('button', { name: '取消' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '删除' })).toBeDisabled()
    await user.keyboard('{Escape}')
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('can close without restoring focus when focus restoration is disabled', async () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <StatefulDeleteDialog
        onConfirm={onConfirm}
        restoreFocusOnClose={false}
        serviceAccountName="ci-bot"
      />,
    )

    await user.click(screen.getByRole('button', { name: '取消' }))

    await waitFor(() =>
      expect(
        screen.queryByRole('alertdialog', { name: '删除服务账号 ci-bot？' }),
      ).not.toBeInTheDocument(),
    )
  })

  it('restores focus to the provided trigger after closing', async () => {
    const onConfirm = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <StatefulDeleteDialog onConfirm={onConfirm} serviceAccountName="ci-bot" />,
    )

    await user.click(screen.getByRole('button', { name: '取消' }))

    await waitFor(() =>
      expect(
        screen.queryByRole('alertdialog', { name: '删除服务账号 ci-bot？' }),
      ).not.toBeInTheDocument(),
    )
    await waitFor(() => expect(screen.getByTestId('restore-target')).toHaveFocus())
  })
})

function StatefulDeleteDialog({
  onConfirm,
  restoreFocusOnClose = true,
  serviceAccountName,
}: {
  onConfirm: () => Promise<unknown>
  restoreFocusOnClose?: boolean
  serviceAccountName: string
}) {
  const [open, setOpen] = useState(true)
  const restoreFocusRef = useRef<HTMLButtonElement | null>(null)

  return (
    <>
      <button ref={restoreFocusRef} type="button" data-testid="restore-target">
        Trigger
      </button>
      <ServiceAccountDeleteDialog
        open={open}
        onOpenChange={setOpen}
        serviceAccountName={serviceAccountName}
        onConfirm={onConfirm}
        pending={false}
        restoreFocusRef={restoreFocusRef}
        restoreFocusOnClose={restoreFocusOnClose}
      />
    </>
  )
}
