import { describe, expect, it, vi } from 'vitest'
import {
  CreateKeyDialog,
  RevokeKeyDialog,
  SecretKeyDialog,
} from '@/features/api-keys/components/dialogs'
import { renderWithProviders, screen } from '@/testing/test-utils'

// Sonner uses React portals + animation. Mock at module boundary so the
// assertion is "toast was called", not "toast DOM eventually appears".
// This is the one carve-out from the network-boundary mock principle
// (proposal §4-J Tier 2 / §6 Risks).
vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

// "Action buttons" — buttons we own — excludes Radix Dialog's auto-
// rendered close button (`[data-slot="dialog-close"]`).
const actionButtons = () =>
  screen.getAllByRole('button').filter((b) => b.getAttribute('data-slot') !== 'dialog-close')

describe('CreateKeyDialog', () => {
  it('Create button disabled while label is empty or whitespace', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateKeyDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )
    const submit = screen.getByRole('button', { name: '新建' })
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
    await user.click(screen.getByRole('button', { name: '新建' }))
    expect(onSubmit).toHaveBeenCalledWith('ci/automation')
    await vi.waitFor(() => expect(input.value).toBe(''))
  })

  it('pending=true disables input and action buttons', () => {
    renderWithProviders(
      <CreateKeyDialog open onOpenChange={vi.fn()} onSubmit={vi.fn()} pending={true} />,
    )
    expect(screen.getByRole('textbox')).toBeDisabled()
    for (const btn of actionButtons()) expect(btn).toBeDisabled()
  })
})

describe('SecretKeyDialog', () => {
  const issued = {
    id: 'k-1',
    label: 'one',
    keyPrefix: 'sk_t_',
    secret: 'sk_t_the-secret-value',
    createdAt: 'now',
  }

  it('clicking the copy icon fires navigator.clipboard.writeText(secret)', async () => {
    // user-event v14's setup() installs its own clipboard mock that
    // is NOT a vitest spy. Spy AFTER renderWithProviders (which calls
    // userEvent.setup) so we wrap whatever user-event installed.
    const { user } = renderWithProviders(<SecretKeyDialog issued={issued} onClose={vi.fn()} />)
    const writeSpy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)

    const buttons = actionButtons()
    const copyBtn = buttons.find((b) => b.querySelector('svg') && b.textContent === '')
    expect(copyBtn).toBeDefined()
    if (copyBtn) await user.click(copyBtn)
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
    createdAt: 'now',
    lastUsedAt: '',
    isActive: true,
  }

  it('confirm calls onConfirm', async () => {
    const onConfirm = vi.fn()
    const { user } = renderWithProviders(
      <RevokeKeyDialog target={target} onCancel={vi.fn()} onConfirm={onConfirm} pending={false} />,
    )
    await user.click(screen.getByRole('button', { name: '撤销' }))
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('cancel calls onCancel', async () => {
    const onCancel = vi.fn()
    const { user } = renderWithProviders(
      <RevokeKeyDialog target={target} onCancel={onCancel} onConfirm={vi.fn()} pending={false} />,
    )
    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(onCancel).toHaveBeenCalledTimes(1)
  })

  it('pending=true disables both action buttons', () => {
    renderWithProviders(
      <RevokeKeyDialog target={target} onCancel={vi.fn()} onConfirm={vi.fn()} pending={true} />,
    )
    for (const btn of actionButtons()) expect(btn).toBeDisabled()
  })
})
