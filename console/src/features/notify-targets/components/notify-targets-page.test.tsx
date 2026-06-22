import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { NotifyTargetsPage } from '@/features/notify-targets/components/notify-targets-page'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

describe('NotifyTargetsPage', () => {
  it('renders hero metrics, governance card, and target rows', async () => {
    server.use(
      http.get('/fb/v1/console/notify-targets', () =>
        HttpResponse.json({
          items: [
            {
              id: 'nt-1',
              destinationType: 'raw-webhook',
              url: 'https://example.com/hook',
              timeoutSeconds: 10,
              disabled: false,
              lastFailureAt: '',
              lastError: '',
            },
            {
              id: 'nt-2',
              destinationType: 'raw-webhook',
              url: 'https://example.com/failing',
              timeoutSeconds: 15,
              disabled: true,
              lastFailureAt: '2026-06-21T00:00:00Z',
              lastError: 'timeout',
            },
          ],
        }),
      ),
    )

    renderWithProviders(<NotifyTargetsPage />)

    await waitFor(() => {
      expect(screen.getByText('https://example.com/hook')).toBeInTheDocument()
    })
    expect(screen.getByText('目标总数')).toBeInTheDocument()
    expect(screen.getByText('投递治理建议')).toBeInTheDocument()
    expect(screen.getByText('https://example.com/failing')).toBeInTheDocument()
  })
})
