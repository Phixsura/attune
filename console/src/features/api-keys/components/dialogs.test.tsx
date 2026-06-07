import { describe, expect, it, vi } from 'vitest'
import {
  CreateKeyDialog,
  RevokeKeyDialog,
  SecretKeyDialog,
} from '@/features/api-keys/components/dialogs'
import { renderWithProviders, screen } from '@/testing/test-utils'

// Sonner uses React portals + animation; mock at module boundary so the
// assertion is "toast was called", not "toast DOM eventually appears".
// One documented carve-out from network-boundary mocking (proposal §4-J).
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

describe('CreateKeyDialog', () => {
  it('submit button disabled when label is empty or whitespace', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateKeyDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    const submit = screen.getByTestId('create-key-submit')
    expect(submit).toBeDisabled()
    await user.type(screen.getByRole('textbox'), '   ')
    expect(submit).toBeDisabled()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('calls onSubmit with the trimmed label and clears the input on success', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateKeyDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    const input = screen.getByRole('textbox') as HTMLInputElement
    await user.type(input, '  ci/automation  ')
    await user.click(screen.getByTestId('create-key-submit'))
    expect(onSubmit).toHaveBeenCalledWith('ci/automation')
    await vi.waitFor(() => expect(input.value).toBe(''))
  })

  it('pending=true disables input and action buttons', () => {
    renderWithProviders(
      <CreateKeyDialog open onOpenChange={vi.fn()} onSubmit={vi.fn()} pending={true} />,
    )
    expect(screen.getByRole('textbox')).toBeDisabled()
    expect(screen.getByTestId('create-key-submit')).toBeDisabled()
    expect(screen.getByTestId('create-key-cancel')).toBeDisabled()
  })
})

describe('SecretKeyDialog', () => {
  const issued = {
    key: {
      id: 'k-1',
      label: 'one',
      keyPrefix: 'sk_t_',
      isActive: true,
      createdAt: '2026-06-07T00:00:00Z',
    },
    secret: 'sk_t_the-secret-value',
  }

  it('clicking the copy icon fires navigator.clipboard.writeText(secret)', async () => {
    const { user } = renderWithProviders(<SecretKeyDialog issued={issued} onClose={vi.fn()} />)
    // user-event v14's setup() installs its own (non-vitest) clipboard mock;
    // spy AFTER renderWithProviders to wrap whatever user-event installed.
    const writeSpy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)
    await user.click(screen.getByTestId('secret-copy'))
    expect(writeSpy).toHaveBeenCalledWith(issued.secret)
  })

  it('issued=null renders no secret content', () => {
    renderWithProviders(<SecretKeyDialog issued={null} onClose={vi.fn()} />)
    expect(screen.queryByText(/sk_/)).toBeNull()
  })
})

describe('RevokeKeyDialog', () => {
  const target = {
    id: 'k-1',
    label: 'one',
    keyPrefix: 'sk_p_',
    isActive: true,
    createdAt: '2026-06-07T00:00:00Z',
  }

  it('confirm calls onConfirm', async () => {
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <RevokeKeyDialog target={target} onCancel={vi.fn()} onConfirm={onConfirm} pending={false} />,
    )
    await user.click(screen.getByTestId('revoke-key-confirm'))
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('cancel calls onCancel', async () => {
    const onCancel = vi.fn()
    const { user } = renderWithProviders(
      <RevokeKeyDialog target={target} onCancel={onCancel} onConfirm={vi.fn()} pending={false} />,
    )
    await user.click(screen.getByTestId('revoke-key-cancel'))
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('pending=true disables both action buttons', () => {
    renderWithProviders(
      <RevokeKeyDialog target={target} onCancel={vi.fn()} onConfirm={vi.fn()} pending={true} />,
    )
    expect(screen.getByTestId('revoke-key-confirm')).toBeDisabled()
    expect(screen.getByTestId('revoke-key-cancel')).toBeDisabled()
  })
})
