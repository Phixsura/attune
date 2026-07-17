import { toast } from 'sonner'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { MCPClient, MCPConnectionProfile } from '@/features/mcp-clients/api/types'
import { ConnectionWorkspaceCard } from '@/features/mcp-clients/components/connection-workspace-card'
import { act, renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}))

afterEach(() => {
  vi.useRealTimers()
  vi.clearAllMocks()
})

const client: MCPClient = {
  id: 'client-123',
  name: 'Claude Pilot',
  redirect_uris: ['http://localhost:8080/callback'],
  scopes: ['feedback:read', 'feedback:write'],
  tool_policy_mode: 'allow_list',
  rate_limit_rpm: 60,
  rate_limit_burst: 10,
  created_at: '2026-07-01T00:00:00Z',
  created_by: 'admin-1',
}

const connection: MCPConnectionProfile = {
  server_url: 'https://mcp.example.test/mcp/v1',
  resource_url: 'https://mcp.example.test/mcp/v1',
  oauth_issuer: 'https://mcp.example.test',
  authorization_endpoint: 'https://mcp.example.test/oauth/authorize',
  token_endpoint: 'https://mcp.example.test/oauth/token',
  protected_resource_metadata_url: 'https://mcp.example.test/.well-known/oauth-protected-resource',
  authorization_server_metadata_url:
    'https://mcp.example.test/.well-known/oauth-authorization-server',
  openid_configuration_url: 'https://mcp.example.test/.well-known/openid-configuration',
  legacy_protected_resource_metadata_url:
    'https://mcp.example.test/.well-known/oauth-protected-resource/mcp',
  legacy_authorization_server_metadata_url:
    'https://mcp.example.test/.well-known/oauth-authorization-server/mcp',
  legacy_openid_configuration_url: 'https://mcp.example.test/.well-known/openid-configuration/mcp',
}

describe('ConnectionWorkspaceCard', () => {
  it('shows an unavailable state when connection metadata is absent', () => {
    renderWithProviders(<ConnectionWorkspaceCard client={client} />)

    expect(screen.getByText('连接资料暂不可用')).toBeInTheDocument()
    expect(
      screen.getByText('当前服务尚未暴露 MCP 公网地址，暂时无法生成宿主配置模板。'),
    ).toBeInTheDocument()
  })

  it('copies endpoints and switches to the curl diagnostic template', async () => {
    const writeSpy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <ConnectionWorkspaceCard client={client} connection={connection} />,
    )

    expect(screen.getByText('当前客户端已满足此模板')).toBeInTheDocument()
    await user.click(screen.getAllByRole('button', { name: '复制' })[0])
    await waitFor(() => expect(writeSpy).toHaveBeenCalledWith(connection.server_url))

    await user.click(screen.getByRole('combobox', { name: '宿主模板' }))
    await user.click(screen.getByRole('option', { name: 'Curl 诊断' }))

    expect(
      screen.getByText('这个模板只用于手工探测公开面，不要求额外 redirect URI。'),
    ).toBeInTheDocument()
    expect(
      (screen.getByLabelText('复制到宿主配置文件或终端命令即可使用。') as HTMLTextAreaElement)
        .value,
    ).toContain("curl -fsSL 'https://mcp.example.test/.well-known/oauth-protected-resource'")
  })

  it('copies template snippets and clears the copied state after the timeout', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const writeSpy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <ConnectionWorkspaceCard client={client} connection={connection} />,
    )

    const snippetCopyButton = screen.getAllByRole('button', { name: '复制' }).at(-1) as HTMLElement
    await user.click(snippetCopyButton)

    await waitFor(() => expect(writeSpy).toHaveBeenCalledWith(expect.stringContaining('claude')))
    expect(snippetCopyButton).toHaveTextContent('已复制')

    act(() => {
      vi.advanceTimersByTime(1500)
    })

    await waitFor(() => expect(snippetCopyButton).toHaveTextContent('复制'))
    vi.useRealTimers()
  })

  it('shows a copy failure toast when clipboard write fails', async () => {
    vi.spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('denied'))
    const { user } = renderWithProviders(
      <ConnectionWorkspaceCard client={client} connection={connection} />,
    )

    await user.click(screen.getAllByRole('button', { name: '复制' })[0])

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('复制失败'))
  })

  it('shows revoked copy guidance instead of a snippet textarea', () => {
    renderWithProviders(
      <ConnectionWorkspaceCard
        client={{ ...client, revoked_at: '2026-07-02T00:00:00Z' }}
        connection={connection}
      />,
    )

    expect(screen.getByText('该客户端已撤销')).toBeInTheDocument()
    expect(
      screen.getByText(
        '已撤销客户端不会再生成可直接使用的宿主配置。请先注册替换客户端，再复制新的模板片段。',
      ),
    ).toBeInTheDocument()
    expect(
      screen.queryByLabelText('复制到宿主配置文件或终端命令即可使用。'),
    ).not.toBeInTheDocument()
  })
})
