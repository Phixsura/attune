import { QueryClient } from '@tanstack/react-query'
import { delay, HttpResponse, http } from 'msw'
import { toast } from 'sonner'
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
  normalizeConsentMode,
  numberOrZero,
  optionalValue,
  policyEnabled,
  RequestNotificationsPage,
  statusLabel,
  statusTone,
} from '@/features/request-notifications/components/request-notifications-page'
import i18n from '@/i18n'
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

const targetFallbackFixture: RequestNotificationWebhookTarget = {
  ...targetFixture,
  id: '11111111-1111-1111-1111-222222222222',
  name: 'Fallback target',
  url: 'https://fallback.example.test/request',
  urlHost: '',
  status: '',
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

const deliveryDeadFixture: RequestNotificationDelivery = {
  ...deliveryFixture,
  id: '43',
  channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK,
  status: 'dead',
  attempts: 3,
  lastError: '',
  deadReason: 'permanent bounce',
  createdAt: '',
}

const deliveryQuietFixture: RequestNotificationDelivery = {
  ...deliveryFixture,
  id: '44',
  status: 'delivered',
  attempts: 1,
  lastError: '',
  deadReason: '',
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

const organizationSubscriberFixture: RequestSubscriber = {
  ...subscriberFixture,
  contactId: '33333333-3333-3333-3333-444444444444',
  displayName: '',
  organization: 'Example Org',
  emailRedacted: 'o***@example.test',
  consentState: '',
  subscriptionStatus: 'pending',
}

const anonymousSubscriberFixture: RequestSubscriber = {
  ...subscriberFixture,
  contactId: '33333333-3333-3333-3333-555555555555',
  displayName: '',
  organization: '',
  emailRedacted: '-',
  consentState: '',
  subscriptionStatus: 'pending',
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
  qc.setQueryData(requestNotificationWebhookTargetsQueryKey, [targetFixture, targetFallbackFixture])
  qc.setQueryData(
    [...requestNotificationDeliveriesQueryKey, 25],
    [deliveryFixture, deliveryDeadFixture, deliveryQuietFixture],
  )
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
      HttpResponse.json({ targets: [targetFixture, targetFallbackFixture] }),
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
      HttpResponse.json({ deliveries: [deliveryFixture, deliveryDeadFixture] }),
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
      () =>
        HttpResponse.json({
          subscribers: [
            subscriberFixture,
            organizationSubscriberFixture,
            anonymousSubscriberFixture,
          ],
        }),
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

function withI18nLocale(resolvedLanguage: string, language: string) {
  const originalResolvedLanguage = i18n.resolvedLanguage
  const originalLanguage = i18n.language
  Object.defineProperty(i18n, 'resolvedLanguage', {
    configurable: true,
    value: resolvedLanguage,
    writable: true,
  })
  Object.defineProperty(i18n, 'language', {
    configurable: true,
    value: language,
    writable: true,
  })
  return () => {
    Object.defineProperty(i18n, 'resolvedLanguage', {
      configurable: true,
      value: originalResolvedLanguage,
      writable: true,
    })
    Object.defineProperty(i18n, 'language', {
      configurable: true,
      value: originalLanguage,
      writable: true,
    })
  }
}

beforeEach(() => {
  vi.spyOn(window, 'confirm').mockReturnValue(true)
  vi.mocked(toast.success).mockClear()
  vi.mocked(toast.error).mockClear()
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
    expect(normalizeConsentMode('disabled')).toBe('disabled')
    expect(normalizeConsentMode('', 'existing_app_consent')).toBe('existing_app_consent')
    expect(normalizeConsentMode('unknown')).toBe('explicit_opt_in')
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
    expect(
      booleanPolicyFromSettings(undefined, [
        {
          key: 'fallback',
          labelKey: 'fallback',
          descriptionKey: 'fallback_help',
          testId: 'fallback',
        },
      ]),
    ).toEqual({ fallback: true })
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

  it('renders with language and undefined locale fallbacks', async () => {
    let restore = withI18nLocale('', 'en-US')
    const languageFallback = renderWithProviders(<RequestNotificationsPage />, {
      queryClient: emptyStateClient(),
    })
    expect(await screen.findByTestId('rn-deliveries-empty')).toBeInTheDocument()
    languageFallback.unmount()
    restore()

    restore = withI18nLocale('', '')
    const undefinedFallback = renderWithProviders(<RequestNotificationsPage />, {
      queryClient: emptyStateClient(),
    })
    expect(await screen.findByTestId('rn-deliveries-empty')).toBeInTheDocument()
    undefinedFallback.unmount()
    restore()
  })

  it('renders loading and API fallback shapes from fresh queries', async () => {
    server.use(
      http.get('/fb/v1/console/request-notifications/settings', () =>
        HttpResponse.json({
          ...settingsFixture,
          defaultConsentMode: '',
          maxRecipientsWithoutConfirm: 0,
          tenantHourlySendLimit: 0,
          contactDailySendLimit: 0,
          enabledEventTypes: {},
          statusPolicy: {},
        }),
      ),
      http.get('/fb/v1/console/request-notifications/sender', async () => {
        await delay(350)
        return HttpResponse.json(senderFixture)
      }),
      http.get('/fb/v1/console/request-notifications/webhook-targets', () =>
        HttpResponse.json({ targets: [targetFallbackFixture] }),
      ),
      http.get('/fb/v1/console/request-notifications/deliveries', () =>
        HttpResponse.json({ deliveries: [deliveryDeadFixture] }),
      ),
      http.get('/fb/v1/console/request-notifications/requests/bad-request/subscribers', () =>
        HttpResponse.json({ message: 'cannot load subscribers' }, { status: 500 }),
      ),
    )
    const { user } = renderWithProviders(<RequestNotificationsPage />)

    expect(screen.getByTestId('rn-sender-loading')).toBeInTheDocument()
    expect(await screen.findByText('Fallback target')).toBeInTheDocument()
    expect(screen.getByText('permanent bounce')).toBeInTheDocument()

    await user.type(screen.getByTestId('rn-subscriber-request-id'), 'bad-request')
    await user.click(screen.getByTestId('rn-subscribers-load'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot load subscribers'))
  })

  it('normalizes malformed consent mode values before saving settings', async () => {
    const captures: Record<string, unknown> = {}
    installRequestHandlers(captures)
    server.use(
      http.get('/fb/v1/console/request-notifications/settings', () =>
        HttpResponse.json({
          ...settingsFixture,
          defaultConsentMode: 'legacy_mode',
        }),
      ),
    )
    const { user } = renderWithProviders(<RequestNotificationsPage />)

    expect(await screen.findByText('CRM')).toBeInTheDocument()
    await user.click(screen.getByTestId('rn-settings-save'))

    await waitFor(() =>
      expect(captures.settings).toMatchObject({
        defaultConsentMode: 'explicit_opt_in',
      }),
    )
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
    await user.click(screen.getAllByRole('combobox')[0])
    await user.click(await screen.findByRole('option', { name: '沿用应用同意' }))
    await user.clear(screen.getByTestId('rn-max-unconfirmed'))
    await user.type(screen.getByTestId('rn-max-unconfirmed'), '250')
    await user.click(screen.getByTestId('rn-settings-save'))
    await waitFor(() => expect(captures.settings).toMatchObject({ emailEnabled: false }))
    expect(captures.settings).toMatchObject({
      defaultConsentMode: 'existing_app_consent',
      maxRecipientsWithoutConfirm: 250,
    })
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
  })

  it('renders sender and subscriber fallback values', async () => {
    installRequestHandlers({})
    const qc = seededClient()
    qc.setQueryData(requestNotificationSenderQueryKey, {
      ...senderFixture,
      fromName: '',
      fromEmailRedacted: '',
      domain: '',
      provider: '',
      status: '',
    })
    const { user } = renderWithProviders(<RequestNotificationsPage />, {
      queryClient: qc,
    })

    expect(await screen.findByText('CRM')).toBeInTheDocument()
    expect(screen.getAllByText('-').length).toBeGreaterThanOrEqual(3)
    expect(screen.getByTestId('rn-sender-from-name')).toHaveValue('')

    await user.type(
      screen.getByTestId('rn-subscriber-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.click(screen.getByTestId('rn-subscribers-load'))

    expect(await screen.findByText('Jane Customer')).toBeInTheDocument()
    expect(screen.getByText('Example Org')).toBeInTheDocument()
    expect(screen.getAllByText('-').length).toBeGreaterThanOrEqual(4)
  })

  it('publishes while settings and preview data are still absent', async () => {
    const captures: Record<string, unknown> = {}
    installRequestHandlers(captures)
    server.use(
      http.get('/fb/v1/console/request-notifications/settings', async () => {
        await delay(5000)
        return HttpResponse.json(settingsFixture)
      }),
    )
    const { user } = renderWithProviders(<RequestNotificationsPage />)

    await user.type(
      screen.getByTestId('rn-draft-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.type(screen.getByTestId('rn-draft-title'), 'Shipped')
    await user.type(screen.getByTestId('rn-draft-body'), 'CSV export is now available.')
    await user.click(screen.getByTestId('rn-publish'))

    await waitFor(() =>
      expect(captures.publish).toMatchObject({
        confirmLargeAudience: false,
      }),
    )
  })

  it('previews, publishes, retries deliveries, and suppresses subscribers', async () => {
    const captures: Record<string, unknown> = {}
    installRequestHandlers(captures)
    const { user } = renderWithProviders(<RequestNotificationsPage />, {
      queryClient: seededClient(),
    })

    expect(await screen.findByText('CRM')).toBeInTheDocument()

    await user.type(
      screen.getByTestId('rn-draft-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.click(screen.getAllByRole('combobox')[1])
    await user.click(await screen.findByRole('option', { name: '已发货' }))
    await user.type(screen.getByTestId('rn-draft-title'), 'Shipped')
    await user.type(screen.getByTestId('rn-draft-body'), 'CSV export is now available.')
    await user.click(screen.getByTestId('rn-draft-webhook'))
    await user.click(screen.getByTestId('rn-preview'))
    await waitFor(() =>
      expect(captures.preview).toMatchObject({
        channels: ['REQUEST_NOTIFICATION_CHANNEL_EMAIL'],
        update: { kind: 'shipped' },
      }),
    )
    await waitFor(() => expect(screen.getAllByText('3').length).toBeGreaterThan(0))
    expect(
      await screen.findByText(
        (content, element) =>
          element?.tagName.toLowerCase() === 'pre' && content.includes('CSV export'),
      ),
    ).toHaveClass('max-w-full', 'whitespace-pre-wrap', 'break-words')
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

  it('requires confirmation before publishing large email audiences', async () => {
    const captures: Record<string, unknown> = {}
    installRequestHandlers(captures)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValueOnce(false).mockReturnValueOnce(true)
    const { user } = renderWithProviders(<RequestNotificationsPage />, {
      queryClient: seededClient(),
    })

    await user.type(
      screen.getByTestId('rn-draft-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.type(screen.getByTestId('rn-draft-title'), 'Shipped')
    await user.type(screen.getByTestId('rn-draft-body'), 'CSV export is now available.')
    await user.clear(screen.getByTestId('rn-max-unconfirmed'))
    await user.type(screen.getByTestId('rn-max-unconfirmed'), '1')
    await user.click(screen.getByTestId('rn-settings-save'))
    await waitFor(() => expect(captures.settings).toMatchObject({ maxRecipientsWithoutConfirm: 1 }))

    await user.click(screen.getByTestId('rn-preview'))
    await waitFor(() => expect(screen.getAllByText('3').length).toBeGreaterThan(0))
    await user.click(screen.getByTestId('rn-publish'))
    expect(captures.publish).toBeUndefined()

    await user.click(screen.getByTestId('rn-publish'))
    await waitFor(() => expect(captures.publish).toMatchObject({ confirmLargeAudience: true }))
    expect(confirm).toHaveBeenCalledTimes(2)
  })

  it('handles webhook test failures and cancelled deletes', async () => {
    const captures: Record<string, unknown> = {}
    installRequestHandlers(captures)
    server.use(
      http.post(
        `/fb/v1/console/request-notifications/webhook-targets/${targetFixture.id}:test`,
        () => HttpResponse.json({ ok: false, latencyMs: '10', message: 'signature rejected' }),
      ),
    )
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { user } = renderWithProviders(<RequestNotificationsPage />, {
      queryClient: seededClient(),
    })

    await screen.findByText('CRM')
    await user.click(screen.getByTestId(`rn-target-test-${targetFixture.id}`))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('signature rejected'))
    server.use(
      http.post(
        `/fb/v1/console/request-notifications/webhook-targets/${targetFixture.id}:test`,
        () => HttpResponse.json({ ok: false, latencyMs: '10', message: '' }),
      ),
    )
    await user.click(screen.getByTestId(`rn-target-test-${targetFixture.id}`))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('Webhook 测试失败'))
    await user.click(screen.getByTestId(`rn-target-delete-${targetFixture.id}`))
    expect(captures.deletedTarget).toBeUndefined()
  })

  it('shows pending indicators for long-running operator actions', async () => {
    const captures: Record<string, unknown> = {}
    installRequestHandlers(captures)
    server.use(
      http.post('/fb/v1/console/request-notifications/sender:verify', async () => {
        await delay(350)
        return HttpResponse.json(senderFixture)
      }),
      http.post('/fb/v1/console/request-notifications/webhook-targets', async () => {
        await delay(350)
        return HttpResponse.json(targetFixture, { status: 201 })
      }),
      http.post(
        `/fb/v1/console/request-notifications/webhook-targets/${targetFixture.id}:test`,
        async () => {
          await delay(350)
          return HttpResponse.json({ ok: true })
        },
      ),
      http.delete(
        `/fb/v1/console/request-notifications/webhook-targets/${targetFixture.id}`,
        async () => {
          await delay(350)
          return HttpResponse.json({})
        },
      ),
      http.post('/fb/v1/console/request-notifications/preview', async () => {
        await delay(350)
        return HttpResponse.json({ eligibleRecipients: 0 })
      }),
      http.post('/fb/v1/console/request-notifications/publish', async () => {
        await delay(350)
        return HttpResponse.json({ id: 'event-1' })
      }),
      http.post('/fb/v1/console/request-notifications/deliveries/42:retry', async () => {
        await delay(350)
        return HttpResponse.json(deliveryFixture)
      }),
      http.post(
        `/fb/v1/console/request-notifications/subscribers/${subscriberFixture.contactId}:suppress`,
        async () => {
          await delay(350)
          return HttpResponse.json({ ...subscriberFixture, consentState: 'suppressed' })
        },
      ),
    )
    const { user } = renderWithProviders(<RequestNotificationsPage />, {
      queryClient: seededClient(),
    })

    await screen.findByText('CRM')

    await user.click(screen.getByTestId('rn-sender-verify'))
    await waitFor(() =>
      expect(
        screen.getByTestId('rn-sender-verify').querySelector('.animate-spin'),
      ).toBeInTheDocument(),
    )

    await user.type(screen.getByTestId('rn-target-name'), 'Escalations')
    await user.type(screen.getByTestId('rn-target-url'), 'https://hooks.example.test/escalations')
    await user.click(screen.getByTestId('rn-target-create'))
    await waitFor(() =>
      expect(
        screen.getByTestId('rn-target-create').querySelector('.animate-spin'),
      ).toBeInTheDocument(),
    )

    await user.click(screen.getByTestId(`rn-target-test-${targetFixture.id}`))
    await waitFor(() =>
      expect(
        screen.getByTestId(`rn-target-test-${targetFixture.id}`).querySelector('.animate-spin'),
      ).toBeInTheDocument(),
    )

    await user.click(screen.getByTestId(`rn-target-delete-${targetFixture.id}`))
    await waitFor(() =>
      expect(
        screen.getByTestId(`rn-target-delete-${targetFixture.id}`).querySelector('.animate-spin'),
      ).toBeInTheDocument(),
    )

    await user.type(
      screen.getByTestId('rn-draft-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.type(screen.getByTestId('rn-draft-title'), 'Shipped')
    await user.type(screen.getByTestId('rn-draft-body'), 'CSV export is now available.')

    await user.click(screen.getByTestId('rn-preview'))
    await waitFor(() =>
      expect(screen.getByTestId('rn-preview').querySelector('.animate-spin')).toBeInTheDocument(),
    )

    await user.click(screen.getByTestId('rn-publish'))
    await waitFor(() =>
      expect(screen.getByTestId('rn-publish').querySelector('.animate-spin')).toBeInTheDocument(),
    )

    await user.click(screen.getByTestId('rn-delivery-retry-42'))
    await waitFor(() =>
      expect(
        screen.getByTestId('rn-delivery-retry-42').querySelector('.animate-spin'),
      ).toBeInTheDocument(),
    )

    await user.type(
      screen.getByTestId('rn-subscriber-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.click(screen.getByTestId('rn-subscribers-load'))
    expect(await screen.findByText('Jane Customer')).toBeInTheDocument()
    await user.click(screen.getByTestId(`rn-subscriber-suppress-${subscriberFixture.contactId}`))
    await waitFor(() =>
      expect(
        screen
          .getByTestId(`rn-subscriber-suppress-${subscriberFixture.contactId}`)
          .querySelector('.animate-spin'),
      ).toBeInTheDocument(),
    )
  })

  it('surfaces mutation errors through toast fallbacks', async () => {
    server.use(
      http.get('/fb/v1/console/request-notifications/settings', () =>
        HttpResponse.json(settingsFixture),
      ),
      http.get('/fb/v1/console/request-notifications/sender', () => HttpResponse.json(null)),
      http.get('/fb/v1/console/request-notifications/webhook-targets', () =>
        HttpResponse.json({ targets: [] }),
      ),
      http.get('/fb/v1/console/request-notifications/deliveries', () =>
        HttpResponse.json({ deliveries: [deliveryQuietFixture] }),
      ),
      http.put('/fb/v1/console/request-notifications/settings', () =>
        HttpResponse.json({ message: 'cannot save settings' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/request-notifications/preview', () =>
        HttpResponse.json({ message: 'cannot preview' }, { status: 500 }),
      ),
    )
    const { user } = renderWithProviders(<RequestNotificationsPage />, {
      queryClient: seededClient(),
    })

    await user.click(await screen.findByTestId('rn-settings-save'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot save settings'))
    expect(screen.getAllByText('-').length).toBeGreaterThan(0)

    await user.type(
      screen.getByTestId('rn-draft-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.type(screen.getByTestId('rn-draft-title'), 'Shipped')
    await user.type(screen.getByTestId('rn-draft-body'), 'CSV export is now available.')
    await user.click(screen.getByTestId('rn-preview'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot preview'))
  })

  it('surfaces remaining operator mutation errors and secondary form branches', async () => {
    const captures: Record<string, unknown> = {}
    installRequestHandlers(captures)
    server.use(
      http.put('/fb/v1/console/request-notifications/sender', () =>
        HttpResponse.json({ message: 'cannot save sender' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/request-notifications/sender:verify', () =>
        HttpResponse.json({ message: 'cannot verify sender' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/request-notifications/webhook-targets', () =>
        HttpResponse.json({ message: 'cannot create target' }, { status: 500 }),
      ),
      http.post(
        `/fb/v1/console/request-notifications/webhook-targets/${targetFixture.id}:test`,
        () => HttpResponse.json({ message: 'cannot test target' }, { status: 500 }),
      ),
      http.delete(`/fb/v1/console/request-notifications/webhook-targets/${targetFixture.id}`, () =>
        HttpResponse.json({ message: 'cannot delete target' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/request-notifications/publish', () =>
        HttpResponse.json({ message: 'cannot publish' }, { status: 500 }),
      ),
      http.post('/fb/v1/console/request-notifications/deliveries/42:retry', () =>
        HttpResponse.json({ message: 'cannot retry delivery' }, { status: 500 }),
      ),
      http.post(
        `/fb/v1/console/request-notifications/subscribers/${subscriberFixture.contactId}:suppress`,
        () => HttpResponse.json({ message: 'cannot suppress subscriber' }, { status: 500 }),
      ),
    )
    const { user } = renderWithProviders(<RequestNotificationsPage />, {
      queryClient: seededClient(),
    })

    await screen.findByText('CRM')
    await user.click(screen.getByRole('button', { name: /刷新/ }))

    await user.clear(screen.getByTestId('rn-tenant-hourly'))
    await user.type(screen.getByTestId('rn-tenant-hourly'), '2400')
    await user.clear(screen.getByTestId('rn-contact-daily'))
    await user.type(screen.getByTestId('rn-contact-daily'), '7')
    await user.click(screen.getByTestId('rn-settings-save'))
    await waitFor(() =>
      expect(captures.settings).toMatchObject({
        tenantHourlySendLimit: 2400,
        contactDailySendLimit: 7,
      }),
    )

    await user.clear(screen.getByTestId('rn-sender-from-name'))
    await user.type(screen.getByTestId('rn-sender-from-name'), 'Product Updates')
    await user.type(screen.getByTestId('rn-sender-reply-to'), 'support@example.test')
    await user.clear(screen.getByTestId('rn-sender-provider'))
    await user.type(screen.getByTestId('rn-sender-from-email'), 'notify@example.test')
    await user.click(screen.getByTestId('rn-sender-save'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot save sender'))
    await user.click(screen.getByTestId('rn-sender-verify'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot verify sender'))

    await user.type(screen.getByTestId('rn-target-name'), 'Escalations')
    await user.type(screen.getByTestId('rn-target-url'), 'https://hooks.example.test/escalations')
    await user.click(screen.getByTestId('rn-target-create'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot create target'))
    await user.click(screen.getByTestId(`rn-target-test-${targetFixture.id}`))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot test target'))
    await user.click(screen.getByTestId(`rn-target-delete-${targetFixture.id}`))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot delete target'))

    await user.type(
      screen.getByTestId('rn-draft-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.type(screen.getByTestId('rn-draft-title'), 'Shipped')
    await user.type(screen.getByTestId('rn-draft-body'), 'CSV export is now available.')
    await user.click(screen.getByTestId('rn-draft-notify'))
    await user.click(screen.getByTestId('rn-draft-email'))
    await user.click(screen.getByTestId('rn-preview'))
    await waitFor(() =>
      expect(captures.preview).toMatchObject({
        channels: ['REQUEST_NOTIFICATION_CHANNEL_WEBHOOK'],
      }),
    )
    await user.click(screen.getByTestId('rn-publish'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot publish'))

    await user.click(screen.getByTestId('rn-delivery-retry-42'))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot retry delivery'))

    await user.click(screen.getByTestId('rn-subscribers-load'))
    expect(screen.queryByText('Jane Customer')).not.toBeInTheDocument()
    await user.type(
      screen.getByTestId('rn-subscriber-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.click(screen.getByTestId('rn-subscribers-load'))
    expect(await screen.findByText('Jane Customer')).toBeInTheDocument()
    await user.click(screen.getByTestId(`rn-subscriber-suppress-${subscriberFixture.contactId}`))
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith('cannot suppress subscriber'))
  }, 60_000)

  it('shows pending indicators for settings, sender, subscriber loading, and id fallbacks', async () => {
    const undefinedTarget = {
      ...targetFixture,
      id: undefined as unknown as string,
      name: 'No identifier target',
    }
    const undefinedDelivery = {
      ...deliveryFixture,
      id: undefined as unknown as string,
      lastError: '',
      deadReason: '',
    }
    server.use(
      http.get('/fb/v1/console/request-notifications/settings', () =>
        HttpResponse.json(settingsFixture),
      ),
      http.get('/fb/v1/console/request-notifications/sender', () =>
        HttpResponse.json(senderFixture),
      ),
      http.get('/fb/v1/console/request-notifications/webhook-targets', () =>
        HttpResponse.json({ targets: [undefinedTarget] }),
      ),
      http.get('/fb/v1/console/request-notifications/deliveries', () =>
        HttpResponse.json({ deliveries: [undefinedDelivery] }),
      ),
      http.put('/fb/v1/console/request-notifications/settings', async () => {
        await delay(350)
        return HttpResponse.json(settingsFixture)
      }),
      http.put('/fb/v1/console/request-notifications/sender', async () => {
        await delay(350)
        return HttpResponse.json(senderFixture)
      }),
      http.post('/fb/v1/console/request-notifications/webhook-targets/undefined:test', async () => {
        await delay(350)
        return HttpResponse.json({ ok: true })
      }),
      http.delete('/fb/v1/console/request-notifications/webhook-targets/undefined', async () => {
        await delay(350)
        return HttpResponse.json({})
      }),
      http.post('/fb/v1/console/request-notifications/deliveries/undefined:retry', async () => {
        await delay(350)
        return HttpResponse.json(undefinedDelivery)
      }),
      http.get(
        '/fb/v1/console/request-notifications/requests/55555555-5555-5555-5555-555555555555/subscribers',
        async () => {
          await delay(350)
          return HttpResponse.json({ subscribers: [subscriberFixture] })
        },
      ),
    )
    const { user } = renderWithProviders(<RequestNotificationsPage />)

    expect(await screen.findByText('No identifier target')).toBeInTheDocument()

    await user.click(screen.getByTestId('rn-settings-save'))
    await waitFor(() =>
      expect(
        screen.getByTestId('rn-settings-save').querySelector('.animate-spin'),
      ).toBeInTheDocument(),
    )

    await user.type(screen.getByTestId('rn-sender-from-email'), 'notify@example.test')
    await user.click(screen.getByTestId('rn-sender-save'))
    await waitFor(() =>
      expect(
        screen.getByTestId('rn-sender-save').querySelector('.animate-spin'),
      ).toBeInTheDocument(),
    )

    await user.click(screen.getByTestId('rn-target-test-undefined'))
    await delay(25)
    expect(screen.getByTestId('rn-target-test-undefined')).toBeEnabled()

    await user.click(screen.getByTestId('rn-target-delete-undefined'))
    await delay(25)
    expect(screen.getByTestId('rn-target-delete-undefined')).toBeEnabled()

    await user.click(screen.getByTestId('rn-delivery-retry-undefined'))
    await delay(25)
    expect(screen.getByTestId('rn-delivery-retry-undefined')).toBeEnabled()

    await user.type(
      screen.getByTestId('rn-subscriber-request-id'),
      '55555555-5555-5555-5555-555555555555',
    )
    await user.click(screen.getByTestId('rn-subscribers-load'))
    await waitFor(() =>
      expect(
        screen.getByTestId('rn-subscribers-load').querySelector('.animate-spin'),
      ).toBeInTheDocument(),
    )
  })
})
