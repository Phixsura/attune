import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { MCPClientsPage } from '@/features/mcp-clients/components/mcp-clients-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const clientFixture = {
  id: 'client-uuid-1234',
  name: 'claude-code-agent',
  redirect_uris: ['http://localhost:8080/callback'],
  scopes: ['mcp:read', 'mcp:write'],
  created_at: '2026-06-21T00:00:00Z',
  created_by: 'admin',
}

describe('MCPClientsPage user flow', () => {
  it('renders existing clients, creates a new client, and revokes a client', async () => {
    let createBody: unknown
    let deletedId = ''
    server.use(
      http.get('/fb/v1/console/mcp/clients', () => HttpResponse.json({ clients: [clientFixture] })),
      http.post('/fb/v1/console/mcp/clients', async ({ request }) => {
        createBody = await request.json()
        return HttpResponse.json({
          client: {
            ...clientFixture,
            id: 'client-uuid-5678',
            name: 'cursor-agent',
            scopes: ['mcp:read'],
          },
        })
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
    expect(screen.getByText('client-u…')).toBeInTheDocument()
    expect(screen.getByText('mcp:read')).toBeInTheDocument()
    expect(screen.getByText('mcp:write')).toBeInTheDocument()
    expect(screen.getByText('可用')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '+ 注册客户端' }))
    const createDialog = screen.getByRole('dialog', { name: '注册 MCP 客户端' })
    await user.type(within(createDialog).getByLabelText('客户端名称'), 'cursor-agent')
    await user.type(
      within(createDialog).getByLabelText('重定向 URI'),
      'http://localhost:3000/callback',
    )
    await user.click(within(createDialog).getByRole('button', { name: '+ 注册客户端' }))
    await waitFor(() => {
      const parsed = typeof createBody === 'string' ? JSON.parse(createBody) : createBody
      expect(parsed).toEqual({
        name: 'cursor-agent',
        redirect_uris: ['http://localhost:3000/callback'],
        scopes: ['mcp:read'],
      })
    })

    await user.click(screen.getByRole('button', { name: '撤销' }))
    const revokeDialog = screen.getByRole('dialog', { name: '撤销此客户端？' })
    await user.click(within(revokeDialog).getByRole('button', { name: '撤销' }))
    await waitFor(() => {
      expect(deletedId).toBe('client-uuid-1234')
    })
  })

  it('shows empty state when no clients exist', async () => {
    server.use(http.get('/fb/v1/console/mcp/clients', () => HttpResponse.json({ clients: [] })))

    renderWithProviders(<MCPClientsPage />)

    await waitFor(() => {
      expect(screen.getByText('还没有 MCP 客户端')).toBeInTheDocument()
    })
  })
})
