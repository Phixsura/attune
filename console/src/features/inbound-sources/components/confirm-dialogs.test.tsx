import { describe, expect, it, vi } from 'vitest'
import type { InboundSource } from '@/features/inbound-sources/api/list-inbound-sources'
import { DeleteInboundSourceDialog } from '@/features/inbound-sources/components/delete-dialog'
import { RotateConfirmDialog } from '@/features/inbound-sources/components/rotate-dialog'
import { renderWithProviders, screen } from '@/testing/test-utils'

const source: InboundSource = {
  id: 'source-1',
  tenantId: 'tenant-1',
  channel: 'webhook',
  name: 'Production webhook',
  slug: 'production-webhook',
  enabled: true,
  lastUid: '',
  lastError: '',
  createdAt: '2026-07-17T00:00:00Z',
  updatedAt: '2026-07-17T00:00:00Z',
}

describe('inbound source confirm dialogs', () => {
  it('deletes after explicit confirmation', async () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <DeleteInboundSourceDialog
        source={source}
        onCancel={onCancel}
        onConfirm={onConfirm}
        pending={false}
      />,
    )

    expect(screen.getByText('Production webhook')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '确认删除' }))

    expect(onConfirm).toHaveBeenCalledTimes(1)
    expect(onCancel).not.toHaveBeenCalled()
  })

  it('cancels delete and disables actions while pending', async () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <DeleteInboundSourceDialog
        source={source}
        onCancel={onCancel}
        onConfirm={onConfirm}
        pending
      />,
    )

    const cancel = screen.getByRole('button', { name: '取消' })
    const confirm = screen.getByRole('button', { name: '确认删除' })
    expect(cancel).toBeDisabled()
    expect(confirm).toBeDisabled()
    expect(confirm.querySelector('.animate-spin')).toBeInTheDocument()

    await user.keyboard('{Escape}')

    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('keeps delete content unmounted when no source is selected', () => {
    renderWithProviders(
      <DeleteInboundSourceDialog
        source={null}
        onCancel={vi.fn()}
        onConfirm={vi.fn()}
        pending={false}
      />,
    )

    expect(screen.queryByText('Production webhook')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '确认删除' })).not.toBeInTheDocument()
  })

  it('rotates after explicit confirmation', async () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <RotateConfirmDialog
        source={source}
        onCancel={onCancel}
        onConfirm={onConfirm}
        pending={false}
      />,
    )

    expect(screen.getByText('Production webhook')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '轮换' }))

    expect(onConfirm).toHaveBeenCalledTimes(1)
    expect(onCancel).not.toHaveBeenCalled()
  })

  it('cancels rotate and disables actions while pending', async () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <RotateConfirmDialog source={source} onCancel={onCancel} onConfirm={onConfirm} pending />,
    )

    const cancel = screen.getByRole('button', { name: '取消' })
    const confirm = screen.getByRole('button', { name: '轮换' })
    expect(cancel).toBeDisabled()
    expect(confirm).toBeDisabled()
    expect(confirm.querySelector('.animate-spin')).toBeInTheDocument()

    await user.keyboard('{Escape}')

    expect(onCancel).toHaveBeenCalledTimes(1)
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('keeps rotate content unmounted when no source is selected', () => {
    renderWithProviders(
      <RotateConfirmDialog source={null} onCancel={vi.fn()} onConfirm={vi.fn()} pending={false} />,
    )

    expect(screen.queryByText('Production webhook')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '轮换' })).not.toBeInTheDocument()
  })
})
