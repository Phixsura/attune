import { describe, expect, it, vi } from 'vitest'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'
import { CreateMCPClientDialog, RevokeMCPClientDialog } from './dialogs'

describe('CreateMCPClientDialog', () => {
  it('normalizes redirect URIs, toggles scopes, and resets after successful submit', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateMCPClientDialog
        open={true}
        onOpenChange={vi.fn()}
        onSubmit={onSubmit}
        pending={false}
      />,
    )

    await user.type(screen.getByLabelText('客户端名称'), '  claude-code-agent  ')
    await user.type(
      screen.getByLabelText('重定向 URI'),
      ' http://localhost:8080/callback, http://localhost:8080/callback\nhttps://vscode.dev/redirect ',
    )
    await user.click(screen.getByRole('checkbox', { name: /Write/ }))
    await user.click(screen.getByRole('checkbox', { name: /Read/ }))
    await user.click(screen.getByRole('button', { name: '+ 注册客户端' }))

    expect(onSubmit).toHaveBeenCalledWith({
      name: 'claude-code-agent',
      redirect_uris: ['http://localhost:8080/callback', 'https://vscode.dev/redirect'],
      scopes: ['mcp:write'],
    })
    await waitFor(() => {
      expect(screen.getByLabelText('客户端名称')).toHaveValue('')
      expect(screen.getByLabelText('重定向 URI')).toHaveValue('')
      expect(screen.getByRole('checkbox', { name: /Read/ })).toBeChecked()
    })
  })

  it('keeps entered values when submit rejects and closes through cancel', async () => {
    const onOpenChange = vi.fn()
    const onSubmit = vi.fn().mockRejectedValue(new Error('nope'))
    const { user } = renderWithProviders(
      <CreateMCPClientDialog
        open={true}
        onOpenChange={onOpenChange}
        onSubmit={onSubmit}
        pending={false}
      />,
    )

    await user.type(screen.getByLabelText('客户端名称'), 'agent')
    await user.type(screen.getByLabelText('重定向 URI'), 'http://localhost:8080/callback')
    await user.click(screen.getByRole('button', { name: '+ 注册客户端' }))

    expect(onSubmit).toHaveBeenCalledOnce()
    expect(screen.getByLabelText('客户端名称')).toHaveValue('agent')

    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })
})

describe('RevokeMCPClientDialog', () => {
  const target = {
    id: 'client-1',
    name: 'claude-code-agent',
    redirect_uris: ['http://localhost:8080/callback'],
    scopes: ['mcp:read'],
    tool_policy_mode: 'legacy_allow_all' as const,
    rate_limit_rpm: null,
    rate_limit_burst: null,
    created_at: '2026-06-21T00:00:00Z',
    created_by: 'admin',
  }

  it('confirms, cancels, and renders pending state', async () => {
    const onCancel = vi.fn()
    const onConfirm = vi.fn()
    const { rerender, user } = renderWithProviders(
      <RevokeMCPClientDialog
        target={target}
        onCancel={onCancel}
        onConfirm={onConfirm}
        pending={false}
      />,
    )

    await user.click(screen.getByRole('button', { name: '撤销' }))
    expect(onConfirm).toHaveBeenCalledOnce()

    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(onCancel).toHaveBeenCalledOnce()

    rerender(
      <RevokeMCPClientDialog
        target={target}
        onCancel={onCancel}
        onConfirm={onConfirm}
        pending={true}
      />,
    )
    expect(screen.getByRole('button', { name: '撤销' })).toBeDisabled()
    expect(
      screen.getByRole('button', { name: '撤销' }).querySelector('.animate-spin'),
    ).not.toBeNull()
  })
})
