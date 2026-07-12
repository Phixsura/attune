import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { InboundSourcesPage } from '@/features/inbound-sources/components/inbound-sources-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('InboundSourcesPage', () => {
  it('renders hero metrics, governance card, and source rows', async () => {
    server.use(
      http.get('/fb/v1/console/inbound/sources', () =>
        HttpResponse.json({
          items: [
            {
              id: 'src-1',
              name: 'Main App',
              channel: 'webhook',
              slug: 'main-app',
              enabled: true,
              lastEventAt: '2026-06-21T00:00:00Z',
              lastError: '',
            },
            {
              id: 'src-2',
              name: 'Support Mailbox',
              channel: 'email',
              slug: 'support-mailbox',
              enabled: false,
              lastEventAt: '',
              lastError: '',
            },
            {
              id: 'src-3',
              name: 'Slack Feed',
              channel: 'slack',
              slug: 'slack-feed',
              enabled: true,
              lastEventAt: '2026-06-22T00:00:00Z',
              lastError: '',
            },
          ],
        }),
      ),
    )

    renderWithProviders(<InboundSourcesPage />)

    await waitFor(() => {
      expect(screen.getByText('Main App')).toBeInTheDocument()
    })
    expect(screen.getByText('来源总数')).toBeInTheDocument()
    expect(screen.getByText('入口治理建议')).toBeInTheDocument()
    expect(screen.getByText('Support Mailbox')).toBeInTheDocument()
    expect(screen.getByText('Slack Feed')).toBeInTheDocument()
    expect(screen.getByText('Slack')).toBeInTheDocument()
  })

  it('creates a webhook source and reveals the generated secret', async () => {
    server.use(
      http.get('/fb/v1/console/inbound/sources', () =>
        HttpResponse.json({
          items: [
            {
              id: 'src-1',
              name: 'Main App',
              channel: 'webhook',
              slug: 'main-app',
              enabled: true,
              lastEventAt: '2026-06-21T00:00:00Z',
              lastError: '',
            },
          ],
        }),
      ),
      http.post('/fb/v1/console/inbound/sources', async ({ request }) => {
        const body = (await request.json()) as {
          channel?: string
          name?: string
          webhookConfig?: Record<string, never>
        }
        expect(body.channel).toBe('webhook')
        expect(body.name).toBe('Webhook Feedback')
        expect(body.webhookConfig).toEqual({})
        return HttpResponse.json({
          source: {
            id: 'src-new',
            tenantId: 'tenant',
            channel: 'webhook',
            name: 'Webhook Feedback',
            slug: 'webhook-feedback',
            enabled: true,
            lastEventAt: '',
            lastUid: '0',
            lastError: '',
            createdAt: '2026-07-12T00:00:00Z',
            updatedAt: '2026-07-12T00:00:00Z',
          },
          webhookSecretReveal: {
            url: 'https://hooks.example.com/inbound/webhook-feedback',
            secretHex: 'deadbeef',
            curlExample: 'curl -X POST https://hooks.example.com/inbound/webhook-feedback',
          },
        })
      }),
    )

    const { user } = renderWithProviders(<InboundSourcesPage />)

    await waitFor(() => {
      expect(screen.getByText('Main App')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '+ 添加入站源' }))
    await user.type(screen.getByLabelText('名称'), 'Webhook Feedback')
    await user.click(screen.getByRole('button', { name: '新建' }))

    expect(await screen.findByText('Webhook URL')).toBeInTheDocument()
    expect(screen.getByText('deadbeef')).toBeInTheDocument()
  })
})
