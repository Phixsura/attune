import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { InboundSourcesPage } from '@/features/inbound-sources/components/inbound-sources-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

const baseSources = [
  {
    id: 'src-1',
    tenantId: 'tenant',
    name: 'Main App',
    channel: 'webhook',
    slug: 'main-app',
    enabled: true,
    lastEventAt: '2026-06-21T00:00:00Z',
    lastUid: '0',
    lastError: '',
    createdAt: '2026-06-20T00:00:00Z',
    updatedAt: '2026-06-21T00:00:00Z',
  },
  {
    id: 'src-2',
    tenantId: 'tenant',
    name: 'Support Mailbox',
    channel: 'email',
    slug: 'support-mailbox',
    enabled: false,
    lastEventAt: '',
    lastUid: '0',
    lastError: '',
    createdAt: '2026-06-20T01:00:00Z',
    updatedAt: '2026-06-21T00:00:00Z',
  },
  {
    id: 'src-3',
    tenantId: 'tenant',
    name: 'Slack Feed',
    channel: 'slack',
    slug: 'slack-feed',
    enabled: true,
    lastEventAt: '2026-06-22T00:00:00Z',
    lastUid: '1783852321068324',
    lastError: 'upstream rejected token',
    createdAt: '2026-06-20T02:00:00Z',
    updatedAt: '2026-06-22T00:00:00Z',
  },
]

describe('InboundSourcesPage', () => {
  it('renders hero metrics, governance card, and a selectable source detail panel', async () => {
    server.use(
      http.get('/fb/v1/console/inbound/sources', () =>
        HttpResponse.json({
          items: baseSources,
        }),
      ),
      http.get('/fb/v1/console/inbound/sources/:id', ({ params }) => {
        const source = baseSources.find((item) => item.id === params.id)
        if (!source) {
          throw new Error(`missing source ${String(params.id)}`)
        }
        return HttpResponse.json(source)
      }),
    )

    const { user } = renderWithProviders(<InboundSourcesPage />)

    await waitFor(() => {
      expect(screen.getByText('Main App')).toBeInTheDocument()
    })
    expect(screen.getByText('来源总数')).toBeInTheDocument()
    expect(screen.getByText('入口治理建议')).toBeInTheDocument()
    expect(screen.getByText('源详情')).toBeInTheDocument()
    expect(await screen.findByText('src-1')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Slack Feed' }))

    expect(await screen.findByText('src-3')).toBeInTheDocument()
    expect(await screen.findByText('upstream rejected token')).toBeInTheDocument()
    expect(await screen.findByText('1783852321068324')).toBeInTheDocument()
  })

  it('creates a webhook source, selects it, and reveals the generated secret', async () => {
    let items = [
      {
        id: 'src-1',
        tenantId: 'tenant',
        name: 'Main App',
        channel: 'webhook',
        slug: 'main-app',
        enabled: true,
        lastEventAt: '2026-06-21T00:00:00Z',
        lastUid: '0',
        lastError: '',
        createdAt: '2026-06-20T00:00:00Z',
        updatedAt: '2026-06-21T00:00:00Z',
      },
    ]

    server.use(
      http.get('/fb/v1/console/inbound/sources', () =>
        HttpResponse.json({
          items,
        }),
      ),
      http.get('/fb/v1/console/inbound/sources/:id', ({ params }) => {
        const source = items.find((item) => item.id === params.id)
        if (!source) {
          throw new Error(`missing source ${String(params.id)}`)
        }
        return HttpResponse.json(source)
      }),
      http.post('/fb/v1/console/inbound/sources', async ({ request }) => {
        const body = (await request.json()) as {
          channel?: string
          name?: string
          webhookConfig?: Record<string, never>
        }
        expect(body.channel).toBe('webhook')
        expect(body.name).toBe('Webhook Feedback')
        expect(body.webhookConfig).toEqual({})
        const source = {
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
        }
        items = [...items, source]
        return HttpResponse.json({
          source,
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
    expect(await screen.findByText('src-new')).toBeInTheDocument()
  })
})
