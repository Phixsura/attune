import { describe, expect, it, vi } from 'vitest'
import { SourcesTable } from '@/features/inbound-sources/components/sources-table'
import { renderWithProviders, screen } from '@/testing/test-utils'

describe('SourcesTable', () => {
  it('renders every channel and state variant', () => {
    renderWithProviders(
      <SourcesTable
        sources={[
          {
            id: 'src-webhook',
            name: 'Webhook Feed',
            channel: 'webhook',
            slug: 'webhook-feed',
            enabled: true,
            lastEventAt: '2026-07-11T12:00:00Z',
            lastUid: '0',
            lastError: '',
            tenantId: 'tenant',
            createdAt: '2026-07-10T12:00:00Z',
            updatedAt: '2026-07-11T12:00:00Z',
          },
          {
            id: 'src-email',
            name: 'Email Feed',
            channel: 'email',
            slug: 'email-feed',
            enabled: false,
            lastEventAt: '',
            lastUid: '0',
            lastError: '',
            tenantId: 'tenant',
            createdAt: '2026-07-10T12:00:00Z',
            updatedAt: '2026-07-11T12:00:00Z',
          },
          {
            id: 'src-slack',
            name: 'Slack Feed',
            channel: 'slack',
            slug: 'slack-feed',
            enabled: true,
            lastEventAt: '2026-07-11T12:00:00Z',
            lastUid: '0',
            lastError: 'upstream rejected token',
            tenantId: 'tenant',
            createdAt: '2026-07-10T12:00:00Z',
            updatedAt: '2026-07-11T12:00:00Z',
          },
          {
            id: 'src-rss',
            name: 'RSS Feed',
            channel: 'rss',
            slug: 'rss-feed',
            enabled: true,
            lastEventAt: '2026-07-11T12:00:00Z',
            lastUid: '0',
            lastError: '',
            tenantId: 'tenant',
            createdAt: '2026-07-10T12:00:00Z',
            updatedAt: '2026-07-11T12:00:00Z',
          },
        ]}
        selectedID="src-slack"
        togglingId="src-email"
        onSelect={vi.fn()}
        onRotate={vi.fn()}
        onPause={vi.fn()}
        onResume={vi.fn()}
        onDelete={vi.fn()}
      />,
    )

    expect(screen.getByText('Webhook Feed')).toBeInTheDocument()
    expect(screen.getByText('Email Feed')).toBeInTheDocument()
    expect(screen.getByText('Slack Feed')).toBeInTheDocument()
    expect(screen.getByText('RSS Feed')).toBeInTheDocument()
    expect(screen.getByText('Webhook')).toBeInTheDocument()
    expect(screen.getByText('邮箱')).toBeInTheDocument()
    expect(screen.getByText('Slack')).toBeInTheDocument()
    expect(screen.getByText('rss')).toBeInTheDocument()
    expect(screen.getByText('异常')).toBeInTheDocument()
    expect(screen.getByText('已暂停')).toBeInTheDocument()
  })
})
