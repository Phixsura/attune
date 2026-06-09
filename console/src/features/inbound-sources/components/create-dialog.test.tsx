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
})
