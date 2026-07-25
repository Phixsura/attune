import { fireEvent } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { CreateInboundSourceDialog } from '@/features/inbound-sources/components/create-dialog'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('CreateInboundSourceDialog', () => {
  it('submits a trimmed webhook create payload', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    await user.type(screen.getByLabelText('名称'), '  Product webhook  ')
    await user.click(screen.getByRole('button', { name: '新建' }))

    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        channel: 'webhook',
        name: 'Product webhook',
        webhookConfig: {},
      })
    })
  })

  it('tests and submits the email create payload with defaults', async () => {
    server.use(
      http.post('/fb/v1/console/inbound/sources/test-connection', async ({ request }) => {
        const body = (await request.json()) as {
          emailConfig?: { host?: string; port?: number; tls?: boolean; username?: string }
        }
        expect(body.emailConfig).toMatchObject({
          host: 'imap.example.com',
          port: 1143,
          tls: false,
          username: 'feedback@example.com',
        })
        return HttpResponse.json({ ok: true, latencyMs: 12 })
      }),
    )
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    await user.click(screen.getByRole('button', { name: /邮箱/ }))
    await user.type(screen.getByLabelText('名称'), 'Support mailbox')
    await user.type(screen.getByLabelText('IMAP 主机'), 'imap.example.com')
    await user.click(screen.getByLabelText(/使用 TLS/))
    fireEvent.change(screen.getByLabelText('端口'), { target: { value: '1143' } })
    await user.type(screen.getByLabelText('用户名'), 'feedback@example.com')
    await user.type(screen.getByLabelText('密码或 App Password'), 'secret')
    await user.clear(screen.getByLabelText('文件夹'))
    await user.click(screen.getByRole('button', { name: '测试连接' }))

    expect(await screen.findByText('连接成功 · 12 ms')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '新建' }))
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith({
        channel: 'email',
        name: 'Support mailbox',
        emailConfig: {
          host: 'imap.example.com',
          port: 1143,
          tls: false,
          username: 'feedback@example.com',
          password: 'secret',
          folder: 'INBOX',
          startFrom: 'now',
          afterIngest: 'mark_seen',
        },
      })
    })
  })

  it('shows the dispatcher error envelope message for malformed test-connection requests', async () => {
    server.use(
      http.post('/fb/v1/console/inbound/sources/test-connection', () =>
        HttpResponse.json(
          {
            code: 'BAD_REQUEST',
            message: 'request body is not valid JSON',
            requestId: 'req-test',
          },
          { status: 400 },
        ),
      ),
    )
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog
        open
        onOpenChange={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        pending={false}
      />,
    )

    await user.click(screen.getByRole('button', { name: /邮箱/ }))
    await user.type(screen.getByLabelText('IMAP 主机'), 'imap.example.com')
    await user.type(screen.getByLabelText('用户名'), 'feedback@example.com')
    await user.type(screen.getByLabelText('密码或 App Password'), 'secret')
    await user.click(screen.getByRole('button', { name: '测试连接' }))

    expect(await screen.findByText('request body is not valid JSON')).toBeInTheDocument()
  })

  it('ignores invalid submit attempts before all required channel fields are present', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    const form = screen.getByRole('button', { name: '新建' }).closest('form')
    expect(form).toBeTruthy()
    if (!form) return

    fireEvent.submit(form)
    expect(onSubmit).not.toHaveBeenCalled()

    await user.type(screen.getByLabelText('名称'), 'Incomplete source')
    await user.click(screen.getByRole('button', { name: /邮箱/ }))
    fireEvent.submit(form)
    expect(onSubmit).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: /Slack/ }))
    fireEvent.submit(form)
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('shows an empty Slack discovery note and keeps channel selection blank', async () => {
    server.use(
      http.post('/fb/v1/console/inbound/sources/slack/discover', () =>
        HttpResponse.json({ channels: [] }),
      ),
    )
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    await user.click(screen.getByRole('button', { name: /Slack/ }))
    await user.type(screen.getByLabelText('Slack Bot Token'), 'xoxb-test-token')
    await user.click(screen.getByRole('button', { name: '发现频道' }))

    expect(await screen.findByText('没有发现可读频道，请检查 token 和 scope。')).toBeInTheDocument()
    expect(screen.getByRole('combobox', { name: '频道' })).toBeDisabled()
  })

  it('surfaces Slack discovery and test failures', async () => {
    server.use(
      http.post('/fb/v1/console/inbound/sources/slack/discover', () =>
        HttpResponse.json(
          { code: 'BAD_REQUEST', message: 'slack token rejected' },
          { status: 400 },
        ),
      ),
      http.post('/fb/v1/console/inbound/sources/test-connection', () =>
        HttpResponse.json(
          { code: 'BAD_REQUEST', message: 'channel not readable' },
          { status: 400 },
        ),
      ),
    )
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog
        open
        onOpenChange={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        pending={false}
      />,
    )

    await user.click(screen.getByRole('button', { name: /Slack/ }))
    await user.type(screen.getByLabelText('Slack Bot Token'), 'xoxb-bad-token')
    await user.click(screen.getByRole('button', { name: '发现频道' }))
    expect(await screen.findByText('slack token rejected')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '测试连接' }))
    expect(await screen.findByText('channel not readable')).toBeInTheDocument()
  })

  it('preserves a selected Slack channel when rediscovery still returns it', async () => {
    let discoverCalls = 0
    server.use(
      http.post('/fb/v1/console/inbound/sources/slack/discover', () => {
        discoverCalls += 1
        return HttpResponse.json({
          channels: [
            {
              id: 'C123456',
              name: 'feedback',
              isPrivate: false,
              isArchived: false,
              isShared: false,
            },
            {
              id: 'C999999',
              name: 'ops',
              isPrivate: true,
              isArchived: false,
              isShared: false,
            },
          ],
        })
      }),
    )
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog
        open
        onOpenChange={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        pending={false}
      />,
    )

    await user.click(screen.getByRole('button', { name: /Slack/ }))
    await user.type(screen.getByLabelText('Slack Bot Token'), 'xoxb-test-token')
    await user.click(screen.getByRole('button', { name: '发现频道' }))
    await screen.findByText('#feedback')
    await user.click(screen.getByRole('combobox', { name: '频道' }))
    await user.click(screen.getByRole('option', { name: '#ops · 私有' }))
    expect(screen.getByRole('combobox', { name: '频道' })).toHaveTextContent('#ops')

    await user.click(screen.getByRole('button', { name: '发现频道' }))
    await waitFor(() => expect(discoverCalls).toBe(2))
    expect(screen.getByRole('combobox', { name: '频道' })).toHaveTextContent('#ops')
  })

  it('discovers a Slack channel and submits the Slack create payload', async () => {
    server.use(
      http.post('/fb/v1/console/inbound/sources/test-connection', async ({ request }) => {
        const body = (await request.json()) as {
          slackConfig?: { botToken?: string; channelId?: string }
        }
        expect(body.slackConfig).toMatchObject({
          botToken: 'xoxb-test-token',
          channelId: 'C999999',
        })
        return HttpResponse.json({ ok: true, latencyMs: 25 })
      }),
      http.post('/fb/v1/console/inbound/sources/slack/discover', async ({ request }) => {
        const body = (await request.json()) as {
          slackConfig?: { botToken?: string; channelId?: string }
        }
        expect(body.slackConfig?.botToken).toBe('xoxb-test-token')
        return HttpResponse.json({
          channels: [
            {
              id: 'C123456',
              name: 'feedback',
              isPrivate: false,
              isArchived: false,
              isShared: false,
            },
            {
              id: 'C999999',
              name: 'private-shared',
              isPrivate: true,
              isArchived: false,
              isShared: true,
            },
          ],
        })
      }),
    )
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    await user.click(screen.getByRole('button', { name: /Slack/ }))
    await user.type(screen.getByLabelText('Slack Bot Token'), 'xoxb-test-token')
    await user.click(screen.getByRole('button', { name: '发现频道' }))
    await screen.findByText('#feedback')
    await user.click(screen.getByRole('combobox', { name: '频道' }))
    await user.click(screen.getByRole('option', { name: '#private-shared · 私有 · 共享' }))
    await user.click(screen.getByRole('button', { name: '测试连接' }))
    expect(await screen.findByText('连接成功 · 25 ms')).toBeInTheDocument()
    await user.type(screen.getByLabelText('名称'), 'Slack Feedback')
    await user.click(screen.getByRole('button', { name: '新建' }))

    expect(onSubmit).toHaveBeenCalledWith({
      channel: 'slack',
      name: 'Slack Feedback',
      slackConfig: {
        botToken: 'xoxb-test-token',
        channelId: 'C999999',
      },
    })
  })

  it('submits a Zendesk API token create payload', async () => {
    server.use(
      http.post('/fb/v1/console/inbound/sources/test-connection', async ({ request }) => {
        const body = (await request.json()) as {
          zendeskConfig?: { subdomain?: string; authMode?: string; email?: string }
        }
        expect(body.zendeskConfig).toMatchObject({
          subdomain: 'mycompany',
          authMode: 'api_token',
          email: 'admin@mycompany.com',
        })
        return HttpResponse.json({ ok: true, latencyMs: 85 })
      }),
    )
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    await user.click(screen.getByRole('button', { name: /Zendesk/ }))
    await user.type(screen.getByLabelText('名称'), 'Support tickets')
    await user.type(screen.getByLabelText('子域名'), 'mycompany')
    await user.type(screen.getByLabelText('管理员邮箱'), 'admin@mycompany.com')
    await user.type(screen.getByLabelText('API Token'), 'zd-token-abc')
    await user.click(screen.getByRole('button', { name: '测试连接' }))

    expect(await screen.findByText(/连接成功/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '新建' }))
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({
          channel: 'zendesk',
          name: 'Support tickets',
          zendeskConfig: expect.objectContaining({
            subdomain: 'mycompany',
            authMode: 'api_token',
            email: 'admin@mycompany.com',
            apiToken: 'zd-token-abc',
          }),
        }),
      )
    })
  })

  it('submits a Zendesk OAuth create payload', async () => {
    server.use(
      http.post('/fb/v1/console/inbound/sources/test-connection', () =>
        HttpResponse.json({ ok: true, latencyMs: 90 }),
      ),
    )
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    await user.click(screen.getByRole('button', { name: /Zendesk/ }))
    await user.type(screen.getByLabelText('名称'), 'Zendesk OAuth')
    await user.type(screen.getByLabelText('子域名'), 'myoauth')
    // Switch from default api_token to OAuth.
    await user.click(screen.getByRole('button', { name: 'OAuth 2.0' }))
    await user.type(screen.getByLabelText('Access Token'), 'access-token-abc')
    await user.click(screen.getByRole('button', { name: '测试连接' }))

    expect(await screen.findByText(/连接成功/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '新建' }))
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({
          channel: 'zendesk',
          name: 'Zendesk OAuth',
          zendeskConfig: expect.objectContaining({
            subdomain: 'myoauth',
            authMode: 'oauth',
            oauthAccessToken: 'access-token-abc',
          }),
        }),
      )
    })
  })

  it('submits an Intercom create payload with region and token', async () => {
    server.use(
      http.post('/fb/v1/console/inbound/sources/test-connection', async ({ request }) => {
        const body = (await request.json()) as {
          intercomConfig?: { region?: string; accessToken?: string }
        }
        expect(body.intercomConfig).toMatchObject({
          region: 'us',
          accessToken: 'ic-token-abc',
        })
        return HttpResponse.json({ ok: true, latencyMs: 42 })
      }),
    )
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    await user.click(screen.getByRole('button', { name: /Intercom/ }))
    await user.type(screen.getByLabelText('名称'), 'Intercom conversations')
    await user.type(screen.getByLabelText('Access Token'), 'ic-token-abc')
    await user.click(screen.getByRole('button', { name: '测试连接' }))

    expect(await screen.findByText(/连接成功/)).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: '新建' }))
    await waitFor(() => {
      expect(onSubmit).toHaveBeenCalledWith(
        expect.objectContaining({
          channel: 'intercom',
          name: 'Intercom conversations',
          intercomConfig: expect.objectContaining({
            region: 'us',
            accessToken: 'ic-token-abc',
            startFrom: 'now',
            maxDetailFetches: 50,
          }),
        }),
      )
    })
  })

  it('keeps the Intercom create button disabled without an access token', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined)
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog open onOpenChange={vi.fn()} onSubmit={onSubmit} pending={false} />,
    )

    await user.click(screen.getByRole('button', { name: /Intercom/ }))
    await user.type(screen.getByLabelText('名称'), 'No token yet')
    expect(screen.getByRole('button', { name: '新建' })).toBeDisabled()
    await user.type(screen.getByLabelText('Access Token'), 'tok')
    expect(screen.getByRole('button', { name: '新建' })).toBeEnabled()
  })

  it('resets transient state when switching back to webhook and when the dialog closes', async () => {
    const onOpenChange = vi.fn()
    const { user } = renderWithProviders(
      <CreateInboundSourceDialog
        open
        onOpenChange={onOpenChange}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        pending={false}
      />,
    )

    await user.click(screen.getByRole('button', { name: /邮箱/ }))
    expect(screen.getByLabelText('IMAP 主机')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: /Webhook/ }))
    expect(screen.queryByLabelText('IMAP 主机')).not.toBeInTheDocument()

    await user.keyboard('{Escape}')
    expect(onOpenChange).toHaveBeenCalledWith(false)
    onOpenChange.mockClear()
    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })
})
