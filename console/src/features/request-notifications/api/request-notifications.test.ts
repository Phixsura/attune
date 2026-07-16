import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { createElement, type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { RequestNotificationChannel } from '@/proto/attune/v1/request_notification'
import { server } from '@/testing/mocks/server'
import {
  requestNotificationDeliveriesQuery,
  requestNotificationSenderQuery,
  requestNotificationSettingsQuery,
  requestNotificationWebhookTargetsQuery,
  requestNotificationWebhookTargetsQueryKey,
  useListRequestNotificationSubscribers,
  useSuppressRequestNotificationSubscriber,
  useUpdateRequestNotificationWebhookTarget,
} from './request-notifications'

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function wrapperFor(qc: QueryClient) {
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children)
}

describe('request notification API client', () => {
  it('fetches settings, targets, and deliveries from console endpoints', async () => {
    const seen: Array<{ path: string; query: string }> = []
    server.use(
      http.get('/fb/v1/console/request-notifications/settings', ({ request }) => {
        const url = new URL(request.url)
        seen.push({ path: url.pathname, query: url.search })
        return HttpResponse.json({
          tenantId: 'tenant-1',
          emailEnabled: true,
          webhookEnabled: false,
          defaultConsentMode: 'explicit_opt_in',
          requirePublicUpdateForStatus: true,
          maxRecipientsWithoutConfirm: 50,
          tenantHourlySendLimit: 500,
          contactDailySendLimit: 5,
          updatedBy: 'user-1',
          createdAt: '2026-07-16T00:00:00Z',
          updatedAt: '2026-07-16T00:00:00Z',
        })
      }),
      http.get('/fb/v1/console/request-notifications/webhook-targets', ({ request }) => {
        const url = new URL(request.url)
        seen.push({ path: url.pathname, query: url.search })
        return HttpResponse.json({
          targets: [
            {
              id: 'target-1',
              name: 'CRM',
              url: 'https://hooks.example.test/notify',
              urlHost: 'hooks.example.test',
              signatureVersion: 'v1',
              includeRecipientIdentity: false,
              status: 'active',
              createdAt: '2026-07-16T00:00:00Z',
              updatedAt: '2026-07-16T00:00:00Z',
            },
          ],
        })
      }),
      http.get('/fb/v1/console/request-notifications/deliveries', ({ request }) => {
        const url = new URL(request.url)
        seen.push({ path: url.pathname, query: url.search })
        return HttpResponse.json({
          deliveries: [
            {
              id: '42',
              eventId: 'event-1',
              channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL,
              status: 'failed',
              attempts: 1,
              lastError: 'temporary',
              destinationHash: 'sha256:abc',
              traceId: 'trace-1',
              createdAt: '2026-07-16T00:00:00Z',
              manualRetryCount: 0,
            },
          ],
        })
      }),
    )

    const qc = makeQueryClient()
    await expect(qc.fetchQuery(requestNotificationSettingsQuery())).resolves.toMatchObject({
      emailEnabled: true,
    })
    await expect(qc.fetchQuery(requestNotificationWebhookTargetsQuery())).resolves.toHaveLength(1)
    await expect(qc.fetchQuery(requestNotificationDeliveriesQuery(10))).resolves.toHaveLength(1)
    expect(seen).toEqual([
      { path: '/fb/v1/console/request-notifications/settings', query: '' },
      { path: '/fb/v1/console/request-notifications/webhook-targets', query: '' },
      { path: '/fb/v1/console/request-notifications/deliveries', query: '?limit=10' },
    ])
  })

  it('defaults missing list payloads to empty arrays', async () => {
    server.use(
      http.get('/fb/v1/console/request-notifications/webhook-targets', () => HttpResponse.json({})),
      http.get('/fb/v1/console/request-notifications/deliveries', () => HttpResponse.json({})),
    )
    const qc = makeQueryClient()

    await expect(qc.fetchQuery(requestNotificationWebhookTargetsQuery())).resolves.toEqual([])
    await expect(qc.fetchQuery(requestNotificationDeliveriesQuery())).resolves.toEqual([])
  })

  it('treats a missing sender as unconfigured while preserving other errors', async () => {
    server.use(
      http.get('/fb/v1/console/request-notifications/sender', () =>
        HttpResponse.json({ code: 'NOT_FOUND' }, { status: 404 }),
      ),
    )
    await expect(makeQueryClient().fetchQuery(requestNotificationSenderQuery())).resolves.toBeNull()

    server.use(
      http.get('/fb/v1/console/request-notifications/sender', () =>
        HttpResponse.json({ message: 'boom' }, { status: 500 }),
      ),
    )
    await expect(
      makeQueryClient().fetchQuery(requestNotificationSenderQuery()),
    ).rejects.toMatchObject({
      status: 500,
    })
  })

  it('patches webhook targets and invalidates the target list', async () => {
    let seenPath = ''
    let seenBody: unknown
    server.use(
      http.patch(
        '/fb/v1/console/request-notifications/webhook-targets/:id',
        async ({ params, request }) => {
          seenPath = new URL(request.url).pathname
          seenBody = await request.json()
          return HttpResponse.json({
            id: String(params.id),
            name: 'CRM',
            urlHost: 'hooks.example.test',
            signatureVersion: 'v1',
            includeRecipientIdentity: true,
            status: 'disabled',
          })
        },
      ),
    )
    const qc = makeQueryClient()
    qc.setQueryData(requestNotificationWebhookTargetsQueryKey, [{ id: 'target/slash' }])
    const { result } = renderHook(() => useUpdateRequestNotificationWebhookTarget(), {
      wrapper: wrapperFor(qc),
    })

    result.current.mutate({
      id: 'target/slash',
      name: 'CRM',
      status: 'disabled',
    })
    await waitFor(() => expect(result.current.isSuccess).toBe(true))

    expect(seenPath).toBe('/fb/v1/console/request-notifications/webhook-targets/target%2Fslash')
    expect(seenBody).toMatchObject({ id: 'target/slash', status: 'disabled' })
    expect(qc.getQueryState(requestNotificationWebhookTargetsQueryKey)?.isInvalidated).toBe(true)
  })

  it('lists and suppresses subscribers through mutation hooks', async () => {
    const calls: Array<{ path: string; body?: unknown }> = []
    server.use(
      http.get(
        '/fb/v1/console/request-notifications/requests/:requestId/subscribers',
        ({ request }) => {
          calls.push({ path: new URL(request.url).pathname })
          return HttpResponse.json({})
        },
      ),
      http.post(
        /\/fb\/v1\/console\/request-notifications\/subscribers\/[^/]+:suppress$/,
        async ({ request }) => {
          calls.push({ path: new URL(request.url).pathname, body: await request.json() })
          return HttpResponse.json({
            contactId: 'contact/slash',
            displayName: 'Jane',
            consentState: 'suppressed',
            subscriptionStatus: 'suppressed',
          })
        },
      ),
    )
    const qc = makeQueryClient()
    const list = renderHook(() => useListRequestNotificationSubscribers(), {
      wrapper: wrapperFor(qc),
    })
    const suppress = renderHook(() => useSuppressRequestNotificationSubscriber(), {
      wrapper: wrapperFor(qc),
    })

    list.result.current.mutate('request/slash')
    await waitFor(() => expect(list.result.current.isSuccess).toBe(true))
    expect(list.result.current.data).toEqual([])

    suppress.result.current.mutate({ contactId: 'contact/slash', reason: 'manual' })
    await waitFor(() => expect(suppress.result.current.isSuccess).toBe(true))
    expect(suppress.result.current.data?.consentState).toBe('suppressed')
    expect(calls).toEqual([
      { path: '/fb/v1/console/request-notifications/requests/request%2Fslash/subscribers' },
      {
        path: '/fb/v1/console/request-notifications/subscribers/contact%2Fslash:suppress',
        body: { reason: 'manual' },
      },
    ])
  })
})
