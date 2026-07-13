import { HttpResponse, http } from 'msw'
import { describe, expect, it, vi } from 'vitest'
import { CreateInboundSourceDialog } from '@/features/inbound-sources/components/create-dialog'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('CreateInboundSourceDialog', () => {
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

  it('discovers a Slack channel and submits the Slack create payload', async () => {
    server.use(
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
    await user.click(screen.getByRole('option', { name: '#feedback' }))
    await user.type(screen.getByLabelText('名称'), 'Slack Feedback')
    await user.click(screen.getByRole('button', { name: '新建' }))

    expect(onSubmit).toHaveBeenCalledWith({
      channel: 'slack',
      name: 'Slack Feedback',
      slackConfig: {
        botToken: 'xoxb-test-token',
        channelId: 'C123456',
      },
    })
  })
})
