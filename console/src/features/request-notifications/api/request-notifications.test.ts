import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { createElement, type ReactNode } from 'react'
import { describe, expect, it } from 'vitest'
import { RequestNotificationChannel } from '@/proto/attune/v1/request_notification'
import { server } from '@/testing/mocks/server'
import {
  requestNotificationDeliveriesQuery,
  requestNotificationDeliveriesQueryKey,
  requestNotificationSenderQuery,
  requestNotificationSenderQueryKey,
  requestNotificationSettingsQuery,
  requestNotificationSettingsQueryKey,
  requestNotificationWebhookTargetsQuery,
  requestNotificationWebhookTargetsQueryKey,
  useCreateRequestNotificationWebhookTarget,
  useDeleteRequestNotificationWebhookTarget,
  useListRequestNotificationSubscribers,
  usePreviewRequestNotification,
  usePublishRequestUpdate,
  useRecordRequestNotificationProviderEvent,
  useRetryRequestNotificationDelivery,
  useSuppressRequestNotificationSubscriber,
  useTestRequestNotificationWebhookTarget,
  useUpdateRequestNotificationSettings,
  useUpdateRequestNotificationWebhookTarget,
  useUpsertRequestNotificationSender,
  useVerifyRequestNotificationSender,
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

  it('records provider suppression events through the console endpoint', async () => {
    let seenPath = ''
    let seenBody: unknown
    server.use(
      http.post(
        '/fb/v1/console/request-notifications/provider-events:suppress',
        async ({ request }) => {
          seenPath = new URL(request.url).pathname
          seenBody = await request.json()
          return HttpResponse.json({
            contactId: 'contact-1',
            emailRedacted: 'j***@example.test',
            consentState: 'suppressed',
            subscriptionStatus: 'suppressed',
          })
        },
      ),
    )
    const hook = renderHook(() => useRecordRequestNotificationProviderEvent(), {
      wrapper: wrapperFor(makeQueryClient()),
    })

    hook.result.current.mutate({
      email: 'jane@example.test',
      eventType: 'bounce',
      reason: '550 mailbox unavailable',
      provider: 'postmark',
      providerMessageId: 'msg-1',
    })
    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true))

    expect(seenPath).toBe('/fb/v1/console/request-notifications/provider-events:suppress')
    expect(seenBody).toMatchObject({
      email: 'jane@example.test',
      eventType: 'bounce',
      providerMessageId: 'msg-1',
    })
  })

  it('mutates notification settings and sender configuration with cache updates', async () => {
    const calls: Array<{ path: string; body?: unknown }> = []
    server.use(
      http.put('/fb/v1/console/request-notifications/settings', async ({ request }) => {
        calls.push({ path: new URL(request.url).pathname, body: await request.json() })
        return HttpResponse.json({
          tenantId: 'tenant-1',
          emailEnabled: false,
          webhookEnabled: true,
          defaultConsentMode: 'explicit_opt_in',
        })
      }),
      http.put('/fb/v1/console/request-notifications/sender', async ({ request }) => {
        calls.push({ path: new URL(request.url).pathname, body: await request.json() })
        return HttpResponse.json({
          id: 'sender-1',
          fromName: 'Attune',
          fromEmailRedacted: 'n***@example.test',
          provider: 'email',
          status: 'pending',
        })
      }),
      http.post('/fb/v1/console/request-notifications/sender:verify', async ({ request }) => {
        calls.push({ path: new URL(request.url).pathname, body: await request.json() })
        return HttpResponse.json({
          id: 'sender-1',
          fromName: 'Attune',
          fromEmailRedacted: 'n***@example.test',
          provider: 'email',
          status: 'verified',
        })
      }),
    )

    const qc = makeQueryClient()
    qc.setQueryData(requestNotificationSettingsQueryKey, { tenantId: 'tenant-1' })
    qc.setQueryData(requestNotificationSenderQueryKey, { id: 'sender-1' })

    const updateSettings = renderHook(() => useUpdateRequestNotificationSettings(), {
      wrapper: wrapperFor(qc),
    })
    const upsertSender = renderHook(() => useUpsertRequestNotificationSender(), {
      wrapper: wrapperFor(qc),
    })
    const verifySender = renderHook(() => useVerifyRequestNotificationSender(), {
      wrapper: wrapperFor(qc),
    })

    updateSettings.result.current.mutate({ emailEnabled: false, webhookEnabled: true })
    await waitFor(() => expect(updateSettings.result.current.isSuccess).toBe(true))
    expect(qc.getQueryData(requestNotificationSettingsQueryKey)).toMatchObject({
      emailEnabled: false,
    })

    upsertSender.result.current.mutate({
      fromName: 'Attune',
      fromEmail: 'notify@example.test',
      provider: 'email',
      providerUrl: 'https://mail.example.test/send',
    })
    await waitFor(() => expect(upsertSender.result.current.isSuccess).toBe(true))

    verifySender.result.current.mutate('sender-1')
    await waitFor(() => expect(verifySender.result.current.isSuccess).toBe(true))

    expect(calls).toEqual([
      {
        path: '/fb/v1/console/request-notifications/settings',
        body: { emailEnabled: false, webhookEnabled: true },
      },
      {
        path: '/fb/v1/console/request-notifications/sender',
        body: {
          fromName: 'Attune',
          fromEmail: 'notify@example.test',
          provider: 'email',
          providerUrl: 'https://mail.example.test/send',
        },
      },
      {
        path: '/fb/v1/console/request-notifications/sender:verify',
        body: { id: 'sender-1' },
      },
    ])
    expect(qc.getQueryState(requestNotificationSettingsQueryKey)?.isInvalidated).toBe(true)
    expect(qc.getQueryState(requestNotificationSenderQueryKey)?.isInvalidated).toBe(true)
  })

  it('mutates webhook targets and delivery workflows with encoded identifiers', async () => {
    const calls: Array<{ path: string; body?: unknown }> = []
    server.use(
      http.post('/fb/v1/console/request-notifications/webhook-targets', async ({ request }) => {
        calls.push({ path: new URL(request.url).pathname, body: await request.json() })
        return HttpResponse.json({
          id: 'target/slash',
          name: 'CRM',
          urlHost: 'hooks.example.test',
          signatureVersion: 'v1',
          includeRecipientIdentity: true,
          status: 'active',
        })
      }),
      http.delete(
        '/fb/v1/console/request-notifications/webhook-targets/:id',
        ({ params, request }) => {
          calls.push({ path: new URL(request.url).pathname, body: { id: params.id } })
          return HttpResponse.json({})
        },
      ),
      http.post(
        /\/fb\/v1\/console\/request-notifications\/webhook-targets\/[^/]+:test$/,
        ({ request }) => {
          calls.push({ path: new URL(request.url).pathname })
          return HttpResponse.json({ ok: true, statusCode: 202, latencyMs: '18' })
        },
      ),
      http.post('/fb/v1/console/request-notifications/preview', async ({ request }) => {
        calls.push({ path: new URL(request.url).pathname, body: await request.json() })
        return HttpResponse.json({ eligibleRecipients: 1, excludedRecipients: 0 })
      }),
      http.post('/fb/v1/console/request-notifications/publish', async ({ request }) => {
        calls.push({ path: new URL(request.url).pathname, body: await request.json() })
        return HttpResponse.json({ id: 'event-1', status: 'pending' }, { status: 201 })
      }),
      http.post(
        /\/fb\/v1\/console\/request-notifications\/deliveries\/[^/]+:retry$/,
        ({ request }) => {
          calls.push({ path: new URL(request.url).pathname })
          return HttpResponse.json({ id: 'delivery/slash', status: 'pending' })
        },
      ),
    )

    const qc = makeQueryClient()
    qc.setQueryData(requestNotificationWebhookTargetsQueryKey, [{ id: 'target/slash' }])
    qc.setQueryData([...requestNotificationDeliveriesQueryKey, 25], [{ id: 'delivery/slash' }])

    const createTarget = renderHook(() => useCreateRequestNotificationWebhookTarget(), {
      wrapper: wrapperFor(qc),
    })
    const deleteTarget = renderHook(() => useDeleteRequestNotificationWebhookTarget(), {
      wrapper: wrapperFor(qc),
    })
    const testTarget = renderHook(() => useTestRequestNotificationWebhookTarget(), {
      wrapper: wrapperFor(qc),
    })
    const preview = renderHook(() => usePreviewRequestNotification(), {
      wrapper: wrapperFor(qc),
    })
    const publish = renderHook(() => usePublishRequestUpdate(), {
      wrapper: wrapperFor(qc),
    })
    const retry = renderHook(() => useRetryRequestNotificationDelivery(), {
      wrapper: wrapperFor(qc),
    })

    createTarget.result.current.mutate({
      name: 'CRM',
      url: 'https://hooks.example.test/request',
      includeRecipientIdentity: true,
    })
    await waitFor(() => expect(createTarget.result.current.isSuccess).toBe(true))

    deleteTarget.result.current.mutate('target/slash')
    await waitFor(() => expect(deleteTarget.result.current.isSuccess).toBe(true))

    testTarget.result.current.mutate('target/slash')
    await waitFor(() => expect(testTarget.result.current.isSuccess).toBe(true))

    preview.result.current.mutate({
      update: {
        requestId: 'request/slash',
        title: 'Shipped',
        body: 'Done',
        kind: 'status_change',
        notifySubscribers: true,
      },
      channels: [RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL],
    })
    await waitFor(() => expect(preview.result.current.isSuccess).toBe(true))

    publish.result.current.mutate({
      update: {
        requestId: 'request/slash',
        title: 'Shipped',
        body: 'Done',
        kind: 'status_change',
        notifySubscribers: true,
      },
      channels: [RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL],
      confirmLargeAudience: true,
    })
    await waitFor(() => expect(publish.result.current.isSuccess).toBe(true))

    retry.result.current.mutate('delivery/slash')
    await waitFor(() => expect(retry.result.current.isSuccess).toBe(true))

    expect(calls).toEqual([
      {
        path: '/fb/v1/console/request-notifications/webhook-targets',
        body: {
          name: 'CRM',
          url: 'https://hooks.example.test/request',
          includeRecipientIdentity: true,
        },
      },
      {
        path: '/fb/v1/console/request-notifications/webhook-targets/target%2Fslash',
        body: { id: 'target/slash' },
      },
      { path: '/fb/v1/console/request-notifications/webhook-targets/target%2Fslash:test' },
      {
        path: '/fb/v1/console/request-notifications/preview',
        body: {
          update: {
            requestId: 'request/slash',
            title: 'Shipped',
            body: 'Done',
            kind: 'status_change',
            notifySubscribers: true,
          },
          channels: ['REQUEST_NOTIFICATION_CHANNEL_EMAIL'],
        },
      },
      {
        path: '/fb/v1/console/request-notifications/publish',
        body: {
          update: {
            requestId: 'request/slash',
            title: 'Shipped',
            body: 'Done',
            kind: 'status_change',
            notifySubscribers: true,
          },
          channels: ['REQUEST_NOTIFICATION_CHANNEL_EMAIL'],
          confirmLargeAudience: true,
        },
      },
      { path: '/fb/v1/console/request-notifications/deliveries/delivery%2Fslash:retry' },
    ])
    expect(qc.getQueryState(requestNotificationWebhookTargetsQueryKey)?.isInvalidated).toBe(true)
    expect(qc.getQueryState([...requestNotificationDeliveriesQueryKey, 25])?.isInvalidated).toBe(
      true,
    )
  })
})
