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
          port: 993,
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
          port: 993,
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
})
