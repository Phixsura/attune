import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  requestNotificationDeliveriesQueryKey,
  requestNotificationSenderQueryKey,
  requestNotificationSettingsQueryKey,
  requestNotificationWebhookTargetsQueryKey,
} from '@/features/request-notifications/api/request-notifications'
import {
  booleanPolicyFromSettings,
  canRetry,
  channelLabel,
  channelList,
  errorMessage,
  formatTime,
  numberOrZero,
  optionalValue,
  policyEnabled,
  RequestNotificationsPage,
  statusLabel,
  statusTone,
} from '@/features/request-notifications/components/request-notifications-page'
import type {
  RequestNotificationDelivery,
  RequestNotificationSender,
  RequestNotificationSettings,
  RequestNotificationWebhookTarget,
  RequestSubscriber,
} from '@/proto/attune/v1/request_notification'
import { RequestNotificationChannel } from '@/proto/attune/v1/request_notification'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor } from '@/testing/test-utils'

vi.mock('sonner', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}))

const settingsFixture: RequestNotificationSettings = {
  tenantId: 'tenant-1',
  emailEnabled: true,
  webhookEnabled: true,
  enabledEventTypes: { 'request.status_changed': true },
  statusPolicy: { shipped: true },
  defaultConsentMode: 'explicit_opt_in',
  requirePublicUpdateForStatus: true,
  maxRecipientsWithoutConfirm: 100,
  tenantHourlySendLimit: 1000,
  contactDailySendLimit: 10,
  updatedBy: 'user-1',
  createdAt: '2026-07-16T00:00:00Z',
  updatedAt: '2026-07-16T00:00:00Z',
}

const senderFixture: RequestNotificationSender = {
  id: 'sender-1',
  fromName: 'Attune',
  fromEmailRedacted: 'n***@example.test',
  replyToRedacted: 's***@example.test',
  domain: 'example.test',
  dkimStatus: 'verified',
  spfStatus: 'verified',
  dmarcStatus: 'verified',
  provider: 'email',
  status: 'verified',
  createdAt: '2026-07-16T00:00:00Z',
  updatedAt: '2026-07-16T00:00:00Z',
}

const targetFixture: RequestNotificationWebhookTarget = {
  id: '11111111-1111-1111-1111-111111111111',
  name: 'CRM',
  url: 'https://hooks.example.test/request',
  urlHost: 'hooks.example.test',
  signatureVersion: 'v1',
  eventMask: { 'request.status_changed': true },
  includeRecipientIdentity: true,
  status: 'active',
  createdAt: '2026-07-16T00:00:00Z',
  updatedAt: '2026-07-16T00:00:00Z',
}

const deliveryFixture: RequestNotificationDelivery = {
  id: '42',
  eventId: '22222222-2222-2222-2222-222222222222',
  channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL,
  status: 'failed',
  attempts: 2,
  lastError: 'temporary provider outage',
  failureKind: '',
  deadReason: '',
  retriedBy: '',
  traceId: 'trace-1',
  destinationHash: 'sha256:abc',
  createdAt: '2026-07-16T00:00:00Z',
  manualRetryCount: 0,
}

const subscriberFixture: RequestSubscriber = {
  contactId: '33333333-3333-3333-3333-333333333333',
  displayName: 'Jane Customer',
  organization: 'Example Co',
  emailRedacted: 'j***@example.test',
  consentState: 'opted_in',
  subscriptionStatus: 'active',
  sources: ['voter'],
}

function seededClient() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Number.POSITIVE_INFINITY },
      mutations: { retry: false },
    },
  })
  qc.setQueryData(requestNotificationSettingsQueryKey, settingsFixture)
  qc.setQueryData(requestNotificationSenderQueryKey, senderFixture)
  qc.setQueryData(requestNotificationWebhookTargetsQueryKey, [targetFixture])
  qc.setQueryData([...requestNotificationDeliveriesQueryKey, 25], [deliveryFixture])
  return qc
}

function emptyStateClient() {
  const qc = seededClient()
  qc.setQueryData(requestNotificationSettingsQueryKey, {
    ...settingsFixture,
    emailEnabled: false,
    webhookEnabled: false,
  })
  qc.setQueryData(requestNotificationSenderQueryKey, null)
  qc.setQueryData(requestNotificationWebhookTargetsQueryKey, [])
  qc.setQueryData([...requestNotificationDeliveriesQueryKey, 25], [])
  return qc
}

function installRequestHandlers(captures: Record<string, unknown>) {
  server.use(
    http.get('/fb/v1/console/request-notifications/settings', () =>
      HttpResponse.json(settingsFixture),
    ),
    http.put('/fb/v1/console/request-notifications/settings', async ({ request }) => {
      captures.settings = await request.json()
      return HttpResponse.json({ ...settingsFixture, ...(captures.settings as object) })
    }),
    http.get('/fb/v1/console/request-notifications/sender', () => HttpResponse.json(senderFixture)),
    http.put('/fb/v1/console/request-notifications/sender', async ({ request }) => {
      captures.sender = await request.json()
      return HttpResponse.json(senderFixture)
    }),
    http.post('/fb/v1/console/request-notifications/sender:verify', async ({ request }) => {
      captures.verify = await request.json()
      return HttpResponse.json(senderFixture)
    }),
    http.get('/fb/v1/console/request-notifications/webhook-targets', () =>
      HttpResponse.json({ targets: [targetFixture] }),
    ),
    http.post('/fb/v1/console/request-notifications/webhook-targets', async ({ request }) => {
      captures.target = await request.json()
      return HttpResponse.json(targetFixture, { status: 201 })
    }),
    http.post(`/fb/v1/console/request-notifications/webhook-targets/${targetFixture.id}:test`, () =>
      HttpResponse.json({ ok: true, statusCode: 202, latencyMs: '15', message: 'ok' }),
    ),
    http.delete(`/fb/v1/console/request-notifications/webhook-targets/${targetFixture.id}`, () => {
      captures.deletedTarget = targetFixture.id
      return HttpResponse.json({})
    }),
    http.get('/fb/v1/console/request-notifications/deliveries', () =>
      HttpResponse.json({ deliveries: [deliveryFixture] }),
    ),
    http.post('/fb/v1/console/request-notifications/preview', async ({ request }) => {
      captures.preview = await request.json()
      return HttpResponse.json({
        eligibleRecipients: 3,
        excludedRecipients: 1,
        excludedByReason: { email_disabled: 1 },
        emailPayload: { request: { title: 'CSV export' } },
        webhookPayload: { update: { title: 'Shipped' } },
      })
    }),
    http.post('/fb/v1/console/request-notifications/publish', async ({ request }) => {
      captures.publish = await request.json()
      return HttpResponse.json(
        {
          id: '44444444-4444-4444-4444-444444444444',
          eventType: 'REQUEST_NOTIFICATION_EVENT_TYPE_SHIPPED',
          status: 'pending',
          createdAt: '2026-07-16T00:00:00Z',
        },
        { status: 201 },
      )
    }),
    http.post('/fb/v1/console/request-notifications/deliveries/42:retry', () => {
      captures.retry = '42'
      return HttpResponse.json({ ...deliveryFixture, status: 'pending' })
    }),
    http.get(
      '/fb/v1/console/request-notifications/requests/55555555-5555-5555-5555-555555555555/subscribers',
      () => HttpResponse.json({ subscribers: [subscriberFixture] }),
    ),
    http.post(
      `/fb/v1/console/request-notifications/subscribers/${subscriberFixture.contactId}:suppress`,
      async ({ request }) => {
        captures.suppress = await request.json()
        return HttpResponse.json({ ...subscriberFixture, consentState: 'suppressed' })
      },
    ),
  )
}

beforeEach(() => {
  vi.spyOn(window, 'confirm').mockReturnValue(true)
})

describe('request notification page helpers', () => {
  const translate = (key: string) => key

  it('normalizes channels, inputs, labels, and retry states', () => {
    expect(channelList(true, false)).toEqual([
      RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL,
    ])
    expect(channelList(false, true)).toEqual([
      RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK,
    ])
    expect(channelList(false, false)).toEqual([])
    expect(numberOrZero('42')).toBe(42)
    expect(numberOrZero('-1')).toBe(0)
    expect(numberOrZero('nan')).toBe(0)
    expect(optionalValue('  secret  ')).toBe('secret')
    expect(optionalValue('   ')).toBeUndefined()
    const policy = booleanPolicyFromSettings({ known: false, future: true, invalid: 'yes' }, [
      {
        key: 'known',
        labelKey: 'known',
        descriptionKey: 'known_help',
        testId: 'known',
      },
      {
        key: 'missing',
        labelKey: 'missing',
        descriptionKey: 'missing_help',
        testId: 'missing',
      },
    ])
    expect(policy).toEqual({ known: false, future: true, missing: true })
    expect(policyEnabled(policy, 'known')).toBe(false)
    expect(policyEnabled(policy, 'missing')).toBe(true)
    expect(errorMessage(new Error('boom'), 'fallback')).toBe('boom')
    expect(errorMessage('boom', 'fallback')).toBe('fallback')
    expect(canRetry({ ...deliveryFixture, status: 'failed' })).toBe(true)
    expect(canRetry({ ...deliveryFixture, status: 'dead' })).toBe(true)
    expect(canRetry({ ...deliveryFixture, status: 'delivered' })).toBe(false)
    expect(formatTime('')).toBe('-')
    expect(formatTime('not-time')).toBe('not-time')
    expect(formatTime('2026-07-16T00:00:00Z', 'en-US')).toContain('07')
    expect(statusTone('active')).toContain('emerald')
    expect(statusTone('failed')).toContain('destructive')
    expect(statusTone('pending')).toContain('muted')
    expect(statusLabel(translate, 'verified')).toBe('request_notifications.status.verified')
    expect(statusLabel(translate, 'mystery')).toBe('mystery')
    expect(
      channelLabel(translate, RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL),
    ).toBe('request_notifications.channels.email')
    expect(
      channelLabel(translate, RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK),
    ).toBe('request_notifications.channels.webhook')
    expect(
      channelLabel(translate, RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_UNSPECIFIED),
    ).toBe('request_notifications.channels.unknown')
  })
})

describe('RequestNotificationsPage', () => {
  it('renders empty configuration and list states without enabling draft actions', async () => {
    renderWithProviders(<RequestNotificationsPage />, {
      queryClient: emptyStateClient(),
    })

    expect(await screen.findByTestId('rn-sender-empty')).toBeInTheDocument()
    expect(screen.getByTestId('rn-targets-empty')).toBeInTheDocument()
    expect(screen.getByTestId('rn-deliveries-empty')).toBeInTheDocument()
    expect(screen.getByTestId('rn-subscribers-empty')).toBeInTheDocument()
    expect(screen.getByTestId('rn-preview')).toBeDisabled()
    expect(screen.getByTestId('rn-publish')).toBeDisabled()
    expect(screen.getByTestId('rn-sender-verify')).toBeDisabled()
  })

  it('renders loaded notification state and drives the main operator mutations', async () => {
    const captures: Record<string, unknown> = {}
    installRequestHandlers(captures)
    const { user } = renderWithProviders(<RequestNotificationsPage />, {
      queryClient: seededClient(),
    })

    expect(await screen.findByText('CRM')).toBeInTheDocument()
    expect(screen.getByText('temporary provider outage')).toBeInTheDocument()
    expect(screen.getByText('n***@example.test')).toBeInTheDocument()

    await user.click(screen.getByTestId('rn-email-enabled'))
    await user.click(screen.getByTestId('rn-event-shipped'))
    await user.click(screen.getByTestId('rn-status-in-progress'))
    await user.clear(screen.getByTestId('rn-max-unconfirmed'))
    await user.type(screen.getByTestId('rn-max-unconfirmed'), '250')
    await user.click(screen.getByTestId('rn-settings-save'))
    await waitFor(() => expect(captures.settings).toMatchObject({ emailEnabled: false }))
    expect(captures.settings).toMatchObject({ maxRecipientsWithoutConfirm: 250 })
    expect(captures.settings).toMatchObject({
      enabledEventTypes: {
        'request.status_changed': true,
        'request.shipped': false,
        'request.need_info_direct': true,
        'request.moderator_response': true,
        'changelog.post_published': true,
      },
      statusPolicy: {
        open: true,
        planned: true,
        in_progress: false,
        shipped: true,
        cancelled: true,
      },
    })

    await user.clear(screen.getByTestId('rn-sender-from-email'))
    await user.type(screen.getByTestId('rn-sender-from-email'), 'notify@example.test')
    await user.clear(screen.getByTestId('rn-sender-provider-url'))
    await user.type(screen.getByTestId('rn-sender-provider-url'), 'https://email.example.test/send')
    await user.clear(screen.getByTestId('rn-sender-provider-secret'))
    await user.type(screen.getByTestId('rn-sender-provider-secret'), 'secret')
    await user.click(screen.getByTestId('rn-sender-save'))
    await waitFor(() => expect(captures.sender).toMatchObject({ fromEmail: 'notify@example.test' }))
    await user.click(screen.getByTestId('rn-sender-verify'))
    await waitFor(() => expect(captures.verify).toEqual({ id: 'sender-1' }))

    await user.type(screen.getByTestId('rn-target-name'), 'Support hub')
    await user.type(screen.getByTestId('rn-target-url'), 'https://hooks.example.test/support')
    await user.type(screen.getByTestId('rn-target-secret'), 'target-secret')
    await user.click(screen.getByTestId('rn-target-include-identity'))
    await user.click(screen.getByTestId('rn-target-create'))
    await waitFor(() => expect(captures.target).toMatchObject({ name: 'Support hub' }))
    expect(captures.target).toMatchObject({ includeRecipientIdentity: true })
    await user.click(screen.getByTestId(`rn-target-test-${targetFixture.id}`))
    await user.click(screen.getByTestId(`rn-target-delete-${targetFixture.id}`))
    await waitFor(() => expect(captures.deletedTarget).toBe(targetFixture.id))

    await user.type(
      screen.getByTestId('rn-draft-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.type(screen.getByTestId('rn-draft-title'), 'Shipped')
    await user.type(screen.getByTestId('rn-draft-body'), 'CSV export is now available.')
    await user.click(screen.getByTestId('rn-draft-webhook'))
    await user.click(screen.getByTestId('rn-preview'))
    await waitFor(() =>
      expect(captures.preview).toMatchObject({ channels: ['REQUEST_NOTIFICATION_CHANNEL_EMAIL'] }),
    )
    expect(await screen.findByText('3')).toBeInTheDocument()
    await user.click(screen.getByTestId('rn-publish'))
    await waitFor(() =>
      expect(captures.publish).toMatchObject({ channels: ['REQUEST_NOTIFICATION_CHANNEL_EMAIL'] }),
    )

    await user.click(screen.getByTestId('rn-delivery-retry-42'))
    await waitFor(() => expect(captures.retry).toBe('42'))

    await user.type(
      screen.getByTestId('rn-subscriber-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.click(screen.getByTestId('rn-subscribers-load'))
    expect(await screen.findByText('Jane Customer')).toBeInTheDocument()
    await user.click(screen.getByTestId(`rn-subscriber-suppress-${subscriberFixture.contactId}`))
    await waitFor(() => expect(captures.suppress).toEqual({ reason: 'operator_suppressed' }))
  })
})
