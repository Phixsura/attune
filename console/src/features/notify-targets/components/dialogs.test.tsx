import { describe, expect, it, vi } from 'vitest'
import type { NotifyTargetCreate } from '@/features/notify-targets/api/create-notify-target'
import type { NotifyTarget } from '@/features/notify-targets/api/list-notify-targets'
import {
  CreateNotifyDialog,
  DeleteNotifyDialog,
} from '@/features/notify-targets/components/dialogs'
import { fireEvent, renderWithProviders, screen } from '@/testing/test-utils'

// CreateNotifyDialog builds the POST body; the audience select is the
// digest delivery path's entry point (a target with audience=digest is
// what the digest worker resolves). DeleteNotifyDialog gates its confirm
// on target !== null. Source under test: dialogs.tsx.

describe('CreateNotifyDialog', () => {
  it('fills url and submits with the server-default audience=all', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateNotifyDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    await user.type(screen.getByTestId('create-notify-url'), 'https://hook.example.com')
    await user.click(screen.getByTestId('create-notify-submit'))
    expect(onSubmit).toHaveBeenCalledTimes(1)
    expect(onSubmit.mock.calls[0][0] as NotifyTargetCreate).toEqual({
      destinationType: 'raw-webhook',
      url: 'https://hook.example.com',
      audience: 'all',
      timeoutSeconds: 10,
      disabled: false,
    })
  })

  it('selecting audience=digest carries the digest channel into the create body', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateNotifyDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    // Click the audience select (second combobox, after destination type)
    const comboboxes = screen.getAllByRole('combobox')
    await user.click(comboboxes[1]) // audience is the second select
    await user.click(screen.getByRole('option', { name: 'digest' }))
    await user.type(screen.getByTestId('create-notify-url'), 'https://hook.example.com')
    await user.click(screen.getByTestId('create-notify-submit'))
    expect(onSubmit).toHaveBeenCalledTimes(1)
    expect((onSubmit.mock.calls[0][0] as NotifyTargetCreate).audience).toBe('digest')
  })

  it('submits selected destination metadata with trimmed url, secret, and timeout', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateNotifyDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    const comboboxes = screen.getAllByRole('combobox')
    await user.click(comboboxes[0])
    await user.click(screen.getByRole('option', { name: 'Slack' }))
    expect(screen.getByText('Slack incoming webhook URL')).toBeInTheDocument()

    const secretInput = document.querySelector<HTMLInputElement>('#nt-secret')
    const timeoutInput = document.querySelector<HTMLInputElement>('#nt-timeout')
    expect(secretInput).not.toBeNull()
    expect(timeoutInput).not.toBeNull()

    fireEvent.change(screen.getByTestId('create-notify-url'), {
      target: { value: '  https://hooks.slack.test/TOKEN  ' },
    })
    fireEvent.change(secretInput as HTMLInputElement, { target: { value: '  not-used  ' } })
    fireEvent.change(timeoutInput as HTMLInputElement, { target: { value: '30' } })
    fireEvent.submit(screen.getByTestId('create-notify-submit').closest('form') as HTMLFormElement)

    expect(onSubmit).toHaveBeenCalledWith({
      destinationType: 'slack',
      url: 'https://hooks.slack.test/TOKEN',
      audience: 'all',
      timeoutSeconds: 30,
      disabled: false,
      secret: 'not-used',
    })
  })

  it('keeps submit disabled for blank urls and disables controls while pending', () => {
    const { rerender } = renderWithProviders(
      <CreateNotifyDialog open onOpenChange={vi.fn()} onSubmit={vi.fn()} pending={false} />,
    )

    expect(screen.getByTestId('create-notify-submit')).toBeDisabled()

    rerender(<CreateNotifyDialog open onOpenChange={vi.fn()} onSubmit={vi.fn()} pending />)
    expect(screen.getByTestId('create-notify-url')).toBeDisabled()
    expect(screen.getByTestId('create-notify-submit')).toBeDisabled()
  })

  it('does not mount the form while closed', () => {
    renderWithProviders(
      <CreateNotifyDialog open={false} onOpenChange={vi.fn()} onSubmit={vi.fn()} pending={false} />,
    )
    expect(screen.queryByTestId('create-notify-url')).not.toBeInTheDocument()
  })
})

describe('DeleteNotifyDialog', () => {
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

  it('confirm click fires onConfirm exactly once', async () => {
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <DeleteNotifyDialog
        target={makeTarget()}
        onCancel={vi.fn()}
        onConfirm={onConfirm}
        pending={false}
      />,
    )
    await user.click(screen.getByTestId('delete-notify-confirm'))
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('cancel click closes the dialog and pending disables destructive action', async () => {
    const onCancel = vi.fn()
    const { user, rerender } = renderWithProviders(
      <DeleteNotifyDialog
        target={makeTarget()}
        onCancel={onCancel}
        onConfirm={vi.fn()}
        pending={false}
      />,
    )

    await user.click(screen.getByRole('button', { name: /Cancel|取消/ }))
    expect(onCancel).toHaveBeenCalledTimes(1)

    rerender(
      <DeleteNotifyDialog target={makeTarget()} onCancel={onCancel} onConfirm={vi.fn()} pending />,
    )
    expect(screen.getByTestId('delete-notify-confirm')).toBeDisabled()
  })

  it('renders nothing when target is null', () => {
    renderWithProviders(
      <DeleteNotifyDialog target={null} onCancel={vi.fn()} onConfirm={vi.fn()} pending={false} />,
    )
    expect(screen.queryByTestId('delete-notify-confirm')).not.toBeInTheDocument()
  })
})
