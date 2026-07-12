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
})
