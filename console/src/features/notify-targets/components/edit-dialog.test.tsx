import { describe, expect, it, vi } from 'vitest'
import type { NotifyTarget } from '@/features/notify-targets/api/list-notify-targets'
import type { NotifyTargetPatch } from '@/features/notify-targets/api/update-notify-target'
import { EditNotifyDialog } from '@/features/notify-targets/components/edit-dialog'
import { renderWithProviders, screen } from '@/testing/test-utils'

// The single highest-value component test: the sparse-PATCH diff that
// decides which fields to send to PATCH /notify-targets/:id. A
// regression here can silently overwrite tenant config — exactly the
// kind of "passes type-check, breaks production" bug the suite is for.
// Source under test: edit-dialog.tsx:65-79.

function makeTarget(over: Partial<NotifyTarget> = {}): NotifyTarget {
  return {
    id: 'nt-1',
    destinationType: 'raw-webhook',
    url: 'https://hook.example.com',
    audience: 'all',
    timeoutSeconds: 10,
    disabled: false,
    createdAt: '2026-06-07T00:00:00Z',
    lastError: '',
    ...over,
  }
}

describe('EditNotifyDialog sparse PATCH diff', () => {
  it('untouched dialog → onClose called, onSubmit not called (empty patch path)', async () => {
    const target = makeTarget()
    const onClose = vi.fn()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={onClose} onSubmit={onSubmit} pending={false} />,
    )
    await user.click(screen.getByTestId('edit-notify-save'))
    expect(onSubmit).not.toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('url change only → patch contains ONLY { url: <trimmed> }', async () => {
    const target = makeTarget({ url: 'https://old.example.com' })
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    const urlInput = screen.getByTestId('edit-notify-url') as HTMLInputElement
    await user.clear(urlInput)
    await user.type(urlInput, '  https://new.example.com  ')
    await user.click(screen.getByTestId('edit-notify-save'))
    expect(onSubmit).toHaveBeenCalledTimes(1)
    const patch = onSubmit.mock.calls[0][0] as NotifyTargetPatch
    expect(patch).toEqual({ url: 'https://new.example.com' })
  })

  it('typing a new secret → patch.secret = <value>', async () => {
    const target = makeTarget()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    const secretInput = screen.getByTestId('edit-notify-secret') as HTMLInputElement
    await user.type(secretInput, 'fresh-secret')
    await user.click(screen.getByTestId('edit-notify-save'))
    const patch = onSubmit.mock.calls[0][0] as NotifyTargetPatch
    expect(patch).toEqual({ secret: 'fresh-secret' })
  })

  it('clear-secret checkbox alone → patch.secret = "" (explicit clear)', async () => {
    const target = makeTarget()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    await user.click(screen.getByTestId('edit-notify-secret-clear'))
    await user.click(screen.getByTestId('edit-notify-save'))
    const patch = onSubmit.mock.calls[0][0] as NotifyTargetPatch
    expect(patch).toEqual({ secret: '' })
  })

  it('timeout change → patch contains ONLY { timeoutSeconds }', async () => {
    const target = makeTarget({ timeoutSeconds: 10 })
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    const timeoutInput = screen.getByTestId('edit-notify-timeout') as HTMLInputElement
    await user.clear(timeoutInput)
    await user.type(timeoutInput, '30')
    await user.click(screen.getByTestId('edit-notify-save'))
    const patch = onSubmit.mock.calls[0][0] as NotifyTargetPatch
    expect(patch).toEqual({ timeoutSeconds: 30 })
  })

  it('disabled toggle → patch contains ONLY { disabled: true }', async () => {
    const target = makeTarget({ disabled: false })
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    await user.click(screen.getByTestId('edit-notify-disabled'))
    await user.click(screen.getByTestId('edit-notify-save'))
    const patch = onSubmit.mock.calls[0][0] as NotifyTargetPatch
    expect(patch).toEqual({ disabled: true })
  })

  it('audience change → patch contains ONLY { audience }', async () => {
    const target = makeTarget({ audience: 'all' })
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    await user.click(screen.getByRole('combobox'))
    await user.click(screen.getByRole('option', { name: 'digest' }))
    await user.click(screen.getByTestId('edit-notify-save'))

    const patch = onSubmit.mock.calls[0][0] as NotifyTargetPatch
    expect(patch).toEqual({ audience: 'digest' })
  })

  it('calls onClose when the dialog requests close', async () => {
    const target = makeTarget()
    const onClose = vi.fn()
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={onClose} onSubmit={vi.fn()} pending={false} />,
    )

    await user.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('clear-then-type resets the cleared bit → patch.secret = typed value (not empty)', async () => {
    // Real "typed wins over cleared" requires CLEARING FIRST, then typing —
    // because the input's onChange handler resets secretCleared on every
    // keystroke (edit-dialog.tsx:135). This is the only sequence that
    // exercises the priority of the `if (secret)` branch over the
    // `else if (secretCleared)` branch (edit-dialog.tsx:72-73).
    const target = makeTarget()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    // Step 1: toggle clear ON → secretCleared=true, secret stays ''.
    await user.click(screen.getByTestId('edit-notify-secret-clear'))
    // Step 2: toggle OFF so the secret input is no longer disabled (the
    // cleared bit gates disabled at edit-dialog.tsx:137).
    await user.click(screen.getByTestId('edit-notify-secret-clear'))
    // Step 3: type → onChange runs setSecret(value) AND setSecretCleared(false).
    const secretInput = screen.getByTestId('edit-notify-secret') as HTMLInputElement
    await user.type(secretInput, 'rotation')
    await user.click(screen.getByTestId('edit-notify-save'))
    const patch = onSubmit.mock.calls[0][0] as NotifyTargetPatch
    expect(patch).toEqual({ secret: 'rotation' })
  })
})
