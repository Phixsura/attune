import { HttpResponse, http } from 'msw'
import { toast } from 'sonner'
import { describe, expect, it, vi } from 'vitest'
import { DeadDeliveriesPage } from '@/features/outbox-dead/components/dead-deliveries-page'
import { OutboxFailureKind } from '@/proto/attune/v1/outbox'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const baseDelivery = {
  id: '101',
  feedbackId: 'f-9',
  destinationType: 'raw-webhook',
  destinationTarget: 'https://example.com/hook',
  audience: 'all',
  status: 'dead',
  attempts: 6,
  failureKind: OutboxFailureKind.OUTBOX_FAILURE_KIND_HTTP_5XX,
  httpStatus: 503,
  lastError: 'upstream returned 503',
  deadReason: 'max attempts exhausted',
  traceId: 'trace-9',
  nextRetryAt: '',
  createdAt: '2026-06-18T00:00:00Z',
  deliveredAt: '',
  lastManualRetryAt: '',
  retriedBy: '',
  manualRetryCount: 0,
  inFlight: false,
}

describe('DeadDeliveriesPage', () => {
  it('renders dead rows with readable failure kind + http status', async () => {
    server.use(
      http.get('/fb/v1/console/outbox/deliveries', () =>
        HttpResponse.json({ deliveries: [baseDelivery], nextBeforeId: '0' }),
      ),
    )
    renderWithProviders(<DeadDeliveriesPage />)

    await waitFor(() => {
      expect(screen.getByText('https://example.com/hook')).toBeInTheDocument()
    })
    // failure kind enum → "HTTP 5xx" badge, http code → "HTTP 503" badge.
    expect(screen.getByText('HTTP 5xx')).toBeInTheDocument()
    expect(screen.getByText('HTTP 503')).toBeInTheDocument()
    // dead reason / attempts render.
    expect(screen.getByText('max attempts exhausted')).toBeInTheDocument()
    expect(screen.getByText('6')).toBeInTheDocument()
  })

  it('retry fires the POST and refetches the list', async () => {
    let retried = ''
    let listCalls = 0
    server.use(
      http.get('/fb/v1/console/outbox/deliveries', () => {
        listCalls += 1
        return HttpResponse.json({ deliveries: [baseDelivery], nextBeforeId: '0' })
      }),
      http.post('/fb/v1/console/outbox/:id/retry', ({ params }) => {
        retried = String(params.id)
        return HttpResponse.json(
          { delivery: { ...baseDelivery, status: 'pending' } },
          { status: 202 },
        )
      }),
    )
    const { user } = renderWithProviders(<DeadDeliveriesPage />)

    const retryBtn = await screen.findByRole('button', { name: '重试' })
    const callsBeforeRetry = listCalls
    await user.click(retryBtn)

    await waitFor(() => {
      expect(retried).toBe('101')
    })
    // success toast + the invalidate triggered a refetch of the list.
    await waitFor(() => {
      expect(toast.success).toHaveBeenCalled()
      expect(listCalls).toBeGreaterThan(callsBeforeRetry)
    })
  })

  it('disables retry for in-flight rows', async () => {
    server.use(
      http.get('/fb/v1/console/outbox/deliveries', () =>
        HttpResponse.json({
          deliveries: [{ ...baseDelivery, inFlight: true }],
          nextBeforeId: '0',
        }),
      ),
    )
    renderWithProviders(<DeadDeliveriesPage />)

    const retryBtn = await screen.findByRole('button', { name: '重试' })
    expect(retryBtn).toBeDisabled()
  })

  it('surfaces the server 409 message on a not-retryable row', async () => {
    server.use(
      http.get('/fb/v1/console/outbox/deliveries', () =>
        HttpResponse.json({ deliveries: [baseDelivery], nextBeforeId: '0' }),
      ),
      http.post('/fb/v1/console/outbox/:id/retry', () =>
        HttpResponse.json(
          { code: 'conflict', message: 'delivery is being sent right now' },
          { status: 409 },
        ),
      ),
    )
    const { user } = renderWithProviders(<DeadDeliveriesPage />)

    const retryBtn = await screen.findByRole('button', { name: '重试' })
    await user.click(retryBtn)

    await waitFor(() => {
      expect(toast.error).toHaveBeenCalledWith('delivery is being sent right now')
    })
  })

  it('flipping the filter to failed loads the failed list', async () => {
    server.use(
      http.get('/fb/v1/console/outbox/deliveries', ({ request }) => {
        const url = new URL(request.url)
        const status = url.searchParams.get('status')
        if (status === 'failed') {
          return HttpResponse.json({
            deliveries: [
              {
                ...baseDelivery,
                id: '202',
                status: 'failed',
                destinationTarget: 'https://failed.example.com/hook',
              },
            ],
            nextBeforeId: '0',
          })
        }
        return HttpResponse.json({ deliveries: [], nextBeforeId: '0' })
      }),
    )
    const { user } = renderWithProviders(<DeadDeliveriesPage />)

    // dead is empty → empty state.
    await waitFor(() => {
      expect(screen.getByText('没有死信投递')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '失败' }))
    await waitFor(() => {
      expect(screen.getByText('https://failed.example.com/hook')).toBeInTheDocument()
    })
  })
})

describe('DeliveriesTable failure rendering edge cases', () => {
  it('renders an em-dash when there is no http response and no classification', async () => {
    server.use(
      http.get('/fb/v1/console/outbox/deliveries', () =>
        HttpResponse.json({
          deliveries: [
            {
              ...baseDelivery,
              failureKind: OutboxFailureKind.OUTBOX_FAILURE_KIND_UNSPECIFIED,
              httpStatus: 0,
              deadReason: '',
              lastError: '',
            },
          ],
          nextBeforeId: '0',
        }),
      ),
    )
    renderWithProviders(<DeadDeliveriesPage />)

    const row = await screen.findByRole('row', { name: /raw-webhook/ })
    // No "HTTP" badge anywhere in the failure-kind row.
    expect(within(row).queryByText(/HTTP/)).not.toBeInTheDocument()
  })
})
