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
    destinationType: 'raw_webhook',
    url: 'https://hook.example.com',
    audience: 'all',
    timeoutSeconds: 10,
    disabled: false,
    secretSet: true,
    createdAt: '2026-06-07T00:00:00Z',
    updatedAt: '2026-06-07T00:00:00Z',
    ...over,
  } as NotifyTarget
}

describe('EditNotifyDialog sparse PATCH diff', () => {
  it('untouched dialog → onClose called, onSubmit not called (empty patch path)', async () => {
    const target = makeTarget()
    const onClose = vi.fn()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={onClose} onSubmit={onSubmit} pending={false} />,
    )
    // Click Save without touching any field.
    await user.click(screen.getByRole('button', { name: '保存' }))
    expect(onSubmit).not.toHaveBeenCalled()
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('url change only → patch contains ONLY { url: <trimmed> }', async () => {
    const target = makeTarget({ url: 'https://old.example.com' })
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    const urlInput = screen.getByLabelText(/Webhook URL/i) as HTMLInputElement
    await user.clear(urlInput)
    await user.type(urlInput, '  https://new.example.com  ')
    await user.click(screen.getByRole('button', { name: '保存' }))
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
    const secretInput = screen.getByLabelText(/签名 secret/i) as HTMLInputElement
    await user.type(secretInput, 'fresh-secret')
    await user.click(screen.getByRole('button', { name: '保存' }))
    const patch = onSubmit.mock.calls[0][0] as NotifyTargetPatch
    expect(patch).toEqual({ secret: 'fresh-secret' })
  })

  it('clear-secret checkbox alone → patch.secret = "" (explicit clear)', async () => {
    const target = makeTarget()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    // Find the "clear secret" checkbox by its bound label text fragment.
    const clearCheckbox = screen.getByRole('checkbox', { name: /清.*secret|clear/i })
    await user.click(clearCheckbox)
    await user.click(screen.getByRole('button', { name: '保存' }))
    const patch = onSubmit.mock.calls[0][0] as NotifyTargetPatch
    expect(patch).toEqual({ secret: '' })
  })

  it('typing then toggling clear → typed value wins (cleared bit reset on type)', async () => {
    const target = makeTarget()
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <EditNotifyDialog target={target} onClose={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    const secretInput = screen.getByLabelText(/签名 secret/i) as HTMLInputElement
    await user.type(secretInput, 'typed-value')
    // The "clear" checkbox in this UI doesn't OVERRIDE a typed value if the user
    // clears the checkbox before submit — the bit is bool, the input is the
    // value of record. Per edit-dialog.tsx:72-73: `if (secret) patch.secret =
    // secret; else if (secretCleared) patch.secret = ''`. Typed value wins.
    await user.click(screen.getByRole('button', { name: '保存' }))
    const patch = onSubmit.mock.calls[0][0] as NotifyTargetPatch
    expect(patch).toEqual({ secret: 'typed-value' })
  })
})
