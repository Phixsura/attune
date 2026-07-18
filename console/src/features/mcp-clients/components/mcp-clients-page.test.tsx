import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  MCPClientsPage,
  mcpClientsPageTestables,
} from '@/features/mcp-clients/components/mcp-clients-page'
import { expectNoA11yViolations } from '@/testing/a11y'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

beforeEach(() => {
  vi.mocked(toast.success).mockClear()
  vi.mocked(toast.error).mockClear()
})

const clientFixture: {
  id: string
  name: string
  redirect_uris: string[]
  scopes: string[]
  tool_policy_mode: 'legacy_allow_all' | 'allow_list'
  rate_limit_rpm: number | null
  rate_limit_burst: number | null
  created_at: string
  created_by: string
} = {
  id: 'client-uuid-1234',
  name: 'claude-code-agent',
  redirect_uris: ['http://localhost:8080/callback'],
  scopes: ['mcp:read', 'mcp:write'],
  tool_policy_mode: 'legacy_allow_all',
  rate_limit_rpm: null,
  rate_limit_burst: null,
  created_at: '2026-06-21T00:00:00Z',
  created_by: 'admin',
}

const baseDetail = {
  client: clientFixture,
  connection: {
    server_url: 'https://attune.example.com/mcp',
    resource_url: 'https://attune.example.com/mcp/v1',
    oauth_issuer: 'https://attune.example.com/mcp/oauth',
    authorization_endpoint: 'https://attune.example.com/mcp/oauth/authorize',
    token_endpoint: 'https://attune.example.com/mcp/oauth/token',
    protected_resource_metadata_url:
      'https://attune.example.com/.well-known/oauth-protected-resource/mcp/v1',
    authorization_server_metadata_url:
      'https://attune.example.com/.well-known/oauth-authorization-server/mcp/oauth',
    openid_configuration_url:
      'https://attune.example.com/.well-known/openid-configuration/mcp/oauth',
    legacy_protected_resource_metadata_url:
      'https://attune.example.com/mcp/.well-known/oauth-protected-resource',
    legacy_authorization_server_metadata_url:
      'https://attune.example.com/mcp/oauth/.well-known/oauth-authorization-server',
    legacy_openid_configuration_url:
      'https://attune.example.com/mcp/oauth/.well-known/openid-configuration',
  },
  sessions: [
    {
      id: 'session-uuid-1111',
      scopes: ['mcp:read', 'mcp:write'],
      last_tool_name: 'list_feedback',
      last_decision: 'allowed',
      last_ip: '127.0.0.1',
      last_user_agent: 'Claude Code',
      last_active_at: '2026-06-24T12:00:00Z',
      created_at: '2026-06-24T10:00:00Z',
      closed_reason: '',
      closed_by: '',
    },
  ],
  refresh_grants: [
    {
      id: 'grant-uuid-2222',
      session_id: 'session-uuid-1111',
      scopes: ['mcp:read'],
      expires_at: '2026-06-25T12:00:00Z',
      created_at: '2026-06-24T10:00:00Z',
    },
  ],
  tools: [
    {
      name: 'list_feedback',
      kind: 'core',
      owner: 'feedback',
      enabled_by_default: true,
      deprecated: true,
      replacement: 'get_feedback',
      aliases: [
        {
          name: 'feedback.list',
          deprecated: true,
          replacement: 'list_feedback',
        },
      ],
      required_scope: 'mcp:read',
      risk: 'read',
      data_class: 'user_content',
      read_only_hint: true,
      destructive_hint: false,
      open_world_hint: false,
      default_rpm: 120,
      default_burst: 20,
      scope_granted: true,
      effective_allowed: true,
      effect: '',
      rate_limit_rpm: null,
      rate_limit_burst: null,
    },
    {
      name: 'update_workflow_state',
      kind: 'core',
      owner: 'workflow',
      enabled_by_default: true,
      deprecated: false,
      replacement: '',
      required_scope: 'mcp:write',
      risk: 'mutate',
      data_class: 'operational',
      read_only_hint: false,
      destructive_hint: false,
      open_world_hint: false,
      default_rpm: 60,
      default_burst: 10,
      scope_granted: true,
      effective_allowed: true,
      effect: '',
      rate_limit_rpm: null,
      rate_limit_burst: null,
    },
  ],
}

describe('mcpClientsPageTestables', () => {
  it('formats local helper output and parse fallbacks', () => {
    expect(mcpClientsPageTestables.emptyDraft()).toEqual({
      effect: '',
      rateLimitBurst: '',
      rateLimitRPM: '',
    })
    expect(mcpClientsPageTestables.parseNullableInt('')).toBeNull()
    expect(mcpClientsPageTestables.parseNullableInt(' 42 ')).toBe(42)
    expect(mcpClientsPageTestables.parseNullableInt('bad')).toBeNull()
    expect(mcpClientsPageTestables.truncateMiddle('short-id')).toBe('short-id')
    expect(mcpClientsPageTestables.truncateMiddle('client-uuid-1234567890')).toBe('client-u…7890')
    expect(mcpClientsPageTestables.truncateAgent('')).toBe('-')
    expect(mcpClientsPageTestables.truncateAgent('Claude Code')).toBe('Claude Code')
    expect(mcpClientsPageTestables.truncateAgent('A'.repeat(32))).toBe(`${'A'.repeat(25)}…`)
    expect(mcpClientsPageTestables.formatPolicyMode('allow_list', (key) => key)).toBe(
      'mcp_clients.governance.mode.allow_list',
    )
    expect(mcpClientsPageTestables.formatPolicyMode('legacy_allow_all', (key) => key)).toBe(
      'mcp_clients.governance.mode.legacy_allow_all',
    )
  })
})

describe('MCPClientsPage user flow', () => {
  it('updates governance, saves tool policies, and revokes a session', async () => {
    let patchBody: unknown
    let toolPolicyBody: unknown
    let revokedSessionId = ''
    let revokedGrantId = ''
    let detail = JSON.parse(JSON.stringify(baseDetail)) as typeof baseDetail

    server.use(
      http.get('/fb/v1/console/mcp/clients', () => HttpResponse.json({ clients: [detail.client] })),
      http.get('/fb/v1/console/mcp/clients/:id', ({ params }) => {
        if (String(params.id) !== detail.client.id) {
          return new HttpResponse(null, { status: 404 })
        }
        return HttpResponse.json(detail)
      }),
      http.patch('/fb/v1/console/mcp/clients/:id', async ({ request }) => {
        patchBody = await request.json()
        const body = patchBody as {
          tool_policy_mode: 'legacy_allow_all' | 'allow_list'
          rate_limit_rpm: number | null
          rate_limit_burst: number | null
        }
        detail = {
          ...detail,
          client: {
            ...detail.client,
            tool_policy_mode: body.tool_policy_mode,
            rate_limit_rpm: body.rate_limit_rpm,
            rate_limit_burst: body.rate_limit_burst,
          },
        }
        return HttpResponse.json({ client: detail.client })
      }),
      http.put('/fb/v1/console/mcp/clients/:id/tool-policies', async ({ request }) => {
        toolPolicyBody = await request.json()
        return HttpResponse.json({ tools: detail.tools })
      }),
      http.delete('/fb/v1/console/mcp/clients/:id/sessions/:sessionId', ({ params }) => {
        revokedSessionId = String(params.sessionId)
        return new HttpResponse(null, { status: 204 })
      }),
      http.delete('/fb/v1/console/mcp/clients/:id/grants/:grantId', ({ params }) => {
        revokedGrantId = String(params.grantId)
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const { user } = renderWithProviders(<MCPClientsPage />)

    await waitFor(() => {
      expect(screen.getByText('claude-code-agent')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.getByText('默认治理策略')).toBeInTheDocument()
    })
    expect(
      screen
        .getAllByText('MCP 客户端')
        .find((node) => node.getAttribute('data-slot') === 'card-title')
        ?.closest('[data-slot="card"]'),
    ).toHaveClass('min-w-0')
    expect(screen.getByText('连接工作台')).toBeInTheDocument()
    expect(screen.getByText('连接工作台').closest('[data-slot="card"]')).toHaveClass('min-w-0')
    expect(screen.getByText('https://attune.example.com/mcp')).toBeInTheDocument()
    expect(screen.getByText(/claude mcp add --transport http/)).toBeInTheDocument()
    expect(screen.getByText('当前只支持交互式 OAuth 宿主')).toBeInTheDocument()
    expect(screen.getByText(/当前不支持 client_credentials 等无头授权流/)).toBeInTheDocument()
    expect(screen.getByText('当前客户端已满足此模板')).toBeInTheDocument()
    expect(screen.getAllByText('已弃用').length).toBeGreaterThanOrEqual(2)
    expect(screen.getByText('建议迁移到 get_feedback')).toBeInTheDocument()

    await user.click(screen.getByRole('combobox', { name: '宿主模板' }))
    await user.click(await screen.findByRole('option', { name: 'Cursor' }))
    await waitFor(() => {
      expect(screen.getByText('使用前还需要补充 redirect URI')).toBeInTheDocument()
    })
    expect(
      screen.getAllByText('https://www.cursor.com/agents/mcp/oauth/callback').length,
    ).toBeGreaterThan(0)
    expect(screen.getByText(/"CLIENT_ID": "client-uuid-1234"/)).toBeInTheDocument()

    await user.click(screen.getByRole('combobox', { name: '工具授权模式' }))
    await user.click(await screen.findByRole('option', { name: '白名单模式' }))
    await user.clear(screen.getByLabelText('每分钟请求数'))
    await user.type(screen.getByLabelText('每分钟请求数'), '90')
    await user.clear(screen.getByLabelText('突发上限'))
    await user.type(screen.getByLabelText('突发上限'), '12')
    await user.click(screen.getByRole('button', { name: '保存治理设置' }))

    await waitFor(() => {
      expect(patchBody).toEqual({
        tool_policy_mode: 'allow_list',
        rate_limit_rpm: 90,
        rate_limit_burst: 12,
      })
    })

    const toolRow = screen
      .getAllByText('list_feedback')
      .map((node) => node.closest('tr'))
      .find((row) => row && within(row).queryByRole('checkbox'))
    expect(toolRow).toBeTruthy()
    expect(screen.getByText('feedback.list')).toBeInTheDocument()
    expect(screen.getByText('兼容别名')).toBeInTheDocument()
    if (!toolRow) return
    await user.click(within(toolRow).getByRole('checkbox', { name: '允许工具 list_feedback' }))
    await user.type(
      within(toolRow).getByRole('textbox', { name: '设置工具 list_feedback 的每分钟请求数' }),
      '30',
    )
    await user.type(
      within(toolRow).getByRole('textbox', { name: '设置工具 list_feedback 的突发上限' }),
      '6',
    )
    await user.click(screen.getByRole('button', { name: '保存工具策略' }))

    await waitFor(() => {
      expect(toolPolicyBody).toEqual({
        policies: [
          {
            tool_name: 'list_feedback',
            effect: 'allow',
            rate_limit_rpm: 30,
            rate_limit_burst: 6,
          },
        ],
      })
    })

    await user.click(screen.getByRole('button', { name: '终止' }))
    await waitFor(() => {
      expect(revokedSessionId).toBe('session-uuid-1111')
    })

    await user.click(screen.getByRole('button', { name: '撤销授权' }))
    await waitFor(() => {
      expect(revokedGrantId).toBe('grant-uuid-2222')
    })
  })

  it('selects a client and edits tool policy controls without pointer-only row behavior', async () => {
    const secondClient = {
      ...clientFixture,
      id: 'client-uuid-9999',
      name: 'vscode-agent',
      redirect_uris: ['http://127.0.0.1:33418'],
      scopes: ['mcp:read'],
    }

    server.use(
      http.get('/fb/v1/console/mcp/clients', () =>
        HttpResponse.json({ clients: [clientFixture, secondClient] }),
      ),
      http.get('/fb/v1/console/mcp/clients/:id', ({ params }) => {
        const id = String(params.id)
        return HttpResponse.json({
          ...baseDetail,
          client: id === secondClient.id ? secondClient : clientFixture,
        })
      }),
      http.put('/fb/v1/console/mcp/clients/:id/tool-policies', () =>
        HttpResponse.json({ tools: baseDetail.tools }),
      ),
    )

    const { container, user } = renderWithProviders(<MCPClientsPage />)

    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: '选择 MCP 客户端 vscode-agent' }),
      ).toBeInTheDocument()
    })

    screen.getByRole('button', { name: '选择 MCP 客户端 vscode-agent' }).focus()
    await user.keyboard('[Enter]')

    await waitFor(() => {
      expect(screen.getByText('client-uuid-9999')).toBeInTheDocument()
    })

    expect(screen.getByRole('checkbox', { name: '阻止工具 list_feedback' })).toBeInTheDocument()
    expect(
      screen.getByRole('textbox', { name: '设置工具 list_feedback 的每分钟请求数' }),
    ).toBeInTheDocument()
    expect(
      screen.getByRole('textbox', { name: '设置工具 list_feedback 的突发上限' }),
    ).toBeInTheDocument()
    await expectNoA11yViolations(container)
  })

  it('creates a new client and revokes a client', async () => {
    let createBody: unknown
    let deletedId = ''
    let clients = [clientFixture]
    let createdClient:
      | (typeof clientFixture & { id: string; name: string; scopes: string[] })
      | null = null
    server.use(
      http.get('/fb/v1/console/mcp/clients', () => HttpResponse.json({ clients })),
      http.get('/fb/v1/console/mcp/clients/:id', ({ params }) => {
        const id = String(params.id)
        if (createdClient && id === createdClient.id) {
          return HttpResponse.json({
            ...baseDetail,
            client: createdClient,
            sessions: [],
            refresh_grants: [],
          })
        }
        return HttpResponse.json(baseDetail)
      }),
      http.post('/fb/v1/console/mcp/clients', async ({ request }) => {
        createBody = await request.json()
        createdClient = {
          ...clientFixture,
          id: 'client-uuid-5678',
          name: 'vscode-agent',
          redirect_uris: ['http://127.0.0.1:33418', 'https://vscode.dev/redirect'],
          scopes: ['mcp:read'],
        }
        clients = [createdClient, ...clients]
        return HttpResponse.json({ client: createdClient })
      }),
      http.delete('/fb/v1/console/mcp/clients/:id', ({ params }) => {
        deletedId = String(params.id)
        return new HttpResponse(null, { status: 204 })
      }),
    )

    const { user } = renderWithProviders(<MCPClientsPage />)

    await waitFor(() => {
      expect(screen.getByText('claude-code-agent')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '+ 注册客户端' }))
    const createDialog = screen.getByRole('dialog', { name: '注册 MCP 客户端' })
    await user.type(within(createDialog).getByLabelText('客户端名称'), 'vscode-agent')
    await user.type(
      within(createDialog).getByLabelText('重定向 URI'),
      'http://127.0.0.1:33418\nhttps://vscode.dev/redirect',
    )
    await user.click(within(createDialog).getByRole('button', { name: '+ 注册客户端' }))

    await waitFor(() => {
      const parsed = typeof createBody === 'string' ? JSON.parse(createBody) : createBody
      expect(parsed).toEqual({
        name: 'vscode-agent',
        redirect_uris: ['http://127.0.0.1:33418', 'https://vscode.dev/redirect'],
        scopes: ['mcp:read'],
      })
    })
    await waitFor(() => {
      expect(screen.getByText('client-uuid-5678')).toBeInTheDocument()
    })
    expect(screen.getAllByText('http://127.0.0.1:33418').length).toBeGreaterThan(0)
    expect(screen.getAllByText('https://vscode.dev/redirect').length).toBeGreaterThan(0)

    await user.click(screen.getByRole('combobox', { name: '宿主模板' }))
    await user.click(await screen.findByRole('option', { name: 'VS Code' }))
    await waitFor(() => {
      expect(screen.getByText('当前客户端已满足此模板')).toBeInTheDocument()
    })

    const createdRow = screen
      .getAllByText('vscode-agent')
      .map((node) => node.closest('tr'))
      .find((row) => row && within(row).queryByRole('button', { name: '撤销客户端 vscode-agent' }))
    expect(createdRow).toBeTruthy()
    if (!createdRow) return
    const revokeButton = within(createdRow).getByRole('button', { name: '撤销客户端 vscode-agent' })
    revokeButton.focus()
    await user.keyboard('[Enter]')
    const revokeDialog = screen.getByRole('dialog', { name: '撤销此客户端？' })
    expect(revokeDialog).toBeInTheDocument()
    await expectNoA11yViolations(document.body)
    await user.keyboard('[Escape]')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
    expect(revokeButton).toHaveFocus()

    await user.click(revokeButton)
    const confirmDialog = screen.getByRole('dialog', { name: '撤销此客户端？' })
    await user.click(within(confirmDialog).getByRole('button', { name: '撤销' }))
    await waitFor(() => {
      expect(deletedId).toBe('client-uuid-5678')
    })
  })

  it('keeps the create dialog open and surfaces create failures', async () => {
    server.use(
      http.get('/fb/v1/console/mcp/clients', () => HttpResponse.json({ clients: [clientFixture] })),
      http.get('/fb/v1/console/mcp/clients/:id', () => HttpResponse.json(baseDetail)),
      http.post('/fb/v1/console/mcp/clients', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'create failed' }, { status: 500 }),
      ),
    )

    const { user } = renderWithProviders(<MCPClientsPage />)

    await waitFor(() => {
      expect(screen.getByText('claude-code-agent')).toBeInTheDocument()
    })
    await user.click(screen.getByRole('button', { name: '+ 注册客户端' }))
    const createDialog = screen.getByRole('dialog', { name: '注册 MCP 客户端' })
    await user.type(within(createDialog).getByLabelText('客户端名称'), 'bad-agent')
    await user.type(within(createDialog).getByLabelText('重定向 URI'), 'http://127.0.0.1:33418')
    await user.click(within(createDialog).getByRole('button', { name: '+ 注册客户端' }))

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('create failed'))
    expect(screen.getByRole('dialog', { name: '注册 MCP 客户端' })).toBeInTheDocument()
    expect(within(createDialog).getByLabelText('客户端名称')).toHaveValue('bad-agent')
  })

  it('surfaces governance, tool, session, grant, and client revoke failures', async () => {
    server.use(
      http.get('/fb/v1/console/mcp/clients', () => HttpResponse.json({ clients: [clientFixture] })),
      http.get('/fb/v1/console/mcp/clients/:id', () => HttpResponse.json(baseDetail)),
      http.patch('/fb/v1/console/mcp/clients/:id', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'governance failed' }, { status: 500 }),
      ),
      http.put('/fb/v1/console/mcp/clients/:id/tool-policies', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'tool policy failed' }, { status: 500 }),
      ),
      http.delete('/fb/v1/console/mcp/clients/:id/sessions/:sessionId', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'session revoke failed' }, { status: 500 }),
      ),
      http.delete('/fb/v1/console/mcp/clients/:id/grants/:grantId', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'grant revoke failed' }, { status: 500 }),
      ),
      http.delete('/fb/v1/console/mcp/clients/:id', () =>
        HttpResponse.json({ code: 'INTERNAL', message: 'client revoke failed' }, { status: 500 }),
      ),
    )

    const { user } = renderWithProviders(<MCPClientsPage />)

    await waitFor(() => {
      expect(screen.getByText('claude-code-agent')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.getByText('默认治理策略')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '保存治理设置' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('governance failed'))

    vi.mocked(toast.error).mockClear()
    await user.click(screen.getByRole('button', { name: '保存工具策略' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('tool policy failed'))

    vi.mocked(toast.error).mockClear()
    await user.click(screen.getByRole('button', { name: '终止' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('session revoke failed'))

    vi.mocked(toast.error).mockClear()
    await user.click(screen.getByRole('button', { name: '撤销授权' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('grant revoke failed'))

    vi.mocked(toast.error).mockClear()
    await user.click(screen.getByRole('button', { name: '撤销客户端 claude-code-agent' }))
    const revokeDialog = screen.getByRole('dialog', { name: '撤销此客户端？' })
    await user.click(within(revokeDialog).getByRole('button', { name: '撤销' }))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('client revoke failed'))
  })

  it('treats revoked clients as read-only in the detail workspace', async () => {
    const revokedClient = {
      ...clientFixture,
      id: 'client-uuid-revoked',
      name: 'revoked-agent',
      revoked_at: '2026-06-25T00:00:00Z',
    }

    server.use(
      http.get('/fb/v1/console/mcp/clients', () => HttpResponse.json({ clients: [revokedClient] })),
      http.get('/fb/v1/console/mcp/clients/:id', () =>
        HttpResponse.json({
          ...baseDetail,
          client: revokedClient,
        }),
      ),
    )

    renderWithProviders(<MCPClientsPage />)

    await waitFor(() => {
      expect(screen.getByText('revoked-agent')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(screen.getByText('该客户端已撤销')).toBeInTheDocument()
    })

    expect(screen.queryByText('当前客户端已满足此模板')).not.toBeInTheDocument()
    expect(
      screen.getByText(
        '已撤销客户端不会再生成可直接使用的宿主配置。请先注册替换客户端，再复制新的模板片段。',
      ),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        '该客户端已经失效，无法继续修改默认治理策略。若要调整治理，请先注册替换客户端。',
      ),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        '该客户端已经失效，无法继续修改工具策略。若要调整授权规则，请先注册替换客户端。',
      ),
    ).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '保存治理设置' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '保存工具策略' })).toBeDisabled()
    expect(screen.getByRole('textbox', { name: '每分钟请求数' })).toBeDisabled()
  })

  it('shows empty state when no clients exist', async () => {
    server.use(http.get('/fb/v1/console/mcp/clients', () => HttpResponse.json({ clients: [] })))

    const { user } = renderWithProviders(<MCPClientsPage />)

    await waitFor(() => {
      expect(screen.getByText('还没有 MCP 客户端')).toBeInTheDocument()
    })

    const createButtons = screen.getAllByRole('button', { name: '+ 注册客户端' })
    await user.click(createButtons.at(-1) ?? createButtons[0])
    expect(screen.getByRole('dialog', { name: '注册 MCP 客户端' })).toBeInTheDocument()
  })
})
