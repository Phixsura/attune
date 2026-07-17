import { useQuery } from '@tanstack/react-query'
import {
  Bell,
  CheckCircle2,
  Loader2,
  Mail,
  PlayCircle,
  RefreshCcw,
  Send,
  ShieldOff,
  Trash2,
  Users,
  Webhook,
} from 'lucide-react'
import type { FormEvent, ReactNode } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { PageHero, PageHeroMetric } from '@/components/page-hero'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  requestNotificationDeliveriesQuery,
  requestNotificationSenderQuery,
  requestNotificationSettingsQuery,
  requestNotificationWebhookTargetsQuery,
  useCreateRequestNotificationWebhookTarget,
  useDeleteRequestNotificationWebhookTarget,
  useListRequestNotificationSubscribers,
  usePreviewRequestNotification,
  usePublishRequestUpdate,
  useRetryRequestNotificationDelivery,
  useSuppressRequestNotificationSubscriber,
  useTestRequestNotificationWebhookTarget,
  useUpdateRequestNotificationSettings,
  useUpsertRequestNotificationSender,
  useVerifyRequestNotificationSender,
} from '@/features/request-notifications/api/request-notifications'
import { useDocumentTitle } from '@/hooks/use-document-title'
import { cn } from '@/lib/utils'
import {
  RequestNotificationChannel,
  type RequestNotificationDelivery,
  type RequestNotificationSender,
  type RequestNotificationWebhookTarget,
  type RequestSubscriber,
} from '@/proto/attune/v1/request_notification'

type BooleanPolicy = Record<string, boolean>

type PolicyOption = {
  descriptionKey: string
  key: string
  labelKey: string
  testId: string
}

const consentModeValues = new Set(['explicit_opt_in', 'existing_app_consent', 'disabled'])

const eventTypeOptions: PolicyOption[] = [
  {
    key: 'request.status_changed',
    labelKey: 'request_notifications.events.status_changed',
    descriptionKey: 'request_notifications.events.status_changed_help',
    testId: 'rn-event-status-changed',
  },
  {
    key: 'request.shipped',
    labelKey: 'request_notifications.events.shipped',
    descriptionKey: 'request_notifications.events.shipped_help',
    testId: 'rn-event-shipped',
  },
  {
    key: 'request.need_info_direct',
    labelKey: 'request_notifications.events.need_info_direct',
    descriptionKey: 'request_notifications.events.need_info_direct_help',
    testId: 'rn-event-need-info-direct',
  },
  {
    key: 'request.moderator_response',
    labelKey: 'request_notifications.events.moderator_response',
    descriptionKey: 'request_notifications.events.moderator_response_help',
    testId: 'rn-event-moderator-response',
  },
  {
    key: 'changelog.post_published',
    labelKey: 'request_notifications.events.changelog_post_published',
    descriptionKey: 'request_notifications.events.changelog_post_published_help',
    testId: 'rn-event-changelog-post-published',
  },
]

const statusPolicyOptions: PolicyOption[] = [
  {
    key: 'open',
    labelKey: 'request_notifications.request_statuses.open',
    descriptionKey: 'request_notifications.request_statuses.open_help',
    testId: 'rn-status-open',
  },
  {
    key: 'planned',
    labelKey: 'request_notifications.request_statuses.planned',
    descriptionKey: 'request_notifications.request_statuses.planned_help',
    testId: 'rn-status-planned',
  },
  {
    key: 'in_progress',
    labelKey: 'request_notifications.request_statuses.in_progress',
    descriptionKey: 'request_notifications.request_statuses.in_progress_help',
    testId: 'rn-status-in-progress',
  },
  {
    key: 'shipped',
    labelKey: 'request_notifications.request_statuses.shipped',
    descriptionKey: 'request_notifications.request_statuses.shipped_help',
    testId: 'rn-status-shipped',
  },
  {
    key: 'cancelled',
    labelKey: 'request_notifications.request_statuses.cancelled',
    descriptionKey: 'request_notifications.request_statuses.cancelled_help',
    testId: 'rn-status-cancelled',
  },
]

export function RequestNotificationsPage() {
  const { i18n, t } = useTranslation()
  useDocumentTitle(t('nav.request_notifications'))

  const settingsQuery = useQuery(requestNotificationSettingsQuery())
  const senderQuery = useQuery(requestNotificationSenderQuery())
  const targetsQuery = useQuery(requestNotificationWebhookTargetsQuery())
  const deliveriesQuery = useQuery(requestNotificationDeliveriesQuery(25))

  const settings = settingsQuery.data
  const sender = senderQuery.data ?? null
  const targets = targetsQuery.data ?? []
  const deliveries = deliveriesQuery.data ?? []
  const locale = i18n.resolvedLanguage || i18n.language || undefined

  const updateSettings = useUpdateRequestNotificationSettings()
  const upsertSender = useUpsertRequestNotificationSender()
  const verifySender = useVerifyRequestNotificationSender()
  const createTarget = useCreateRequestNotificationWebhookTarget()
  const deleteTarget = useDeleteRequestNotificationWebhookTarget()
  const testTarget = useTestRequestNotificationWebhookTarget()
  const preview = usePreviewRequestNotification()
  const publish = usePublishRequestUpdate()
  const retryDelivery = useRetryRequestNotificationDelivery()
  const listSubscribers = useListRequestNotificationSubscribers()
  const suppressSubscriber = useSuppressRequestNotificationSubscriber()

  const [emailEnabled, setEmailEnabled] = useState(true)
  const [webhookEnabled, setWebhookEnabled] = useState(true)
  const [consentMode, setConsentMode] = useState('explicit_opt_in')
  const [requirePublicUpdate, setRequirePublicUpdate] = useState(true)
  const [maxRecipientsWithoutConfirm, setMaxRecipientsWithoutConfirm] = useState('500')
  const [tenantHourlySendLimit, setTenantHourlySendLimit] = useState('5000')
  const [contactDailySendLimit, setContactDailySendLimit] = useState('5')
  const [enabledEventTypes, setEnabledEventTypes] = useState<BooleanPolicy>(() =>
    defaultBooleanPolicy(eventTypeOptions),
  )
  const [statusPolicy, setStatusPolicy] = useState<BooleanPolicy>(() =>
    defaultBooleanPolicy(statusPolicyOptions),
  )

  const [senderForm, setSenderForm] = useState({
    fromName: '',
    fromEmail: '',
    replyTo: '',
    provider: 'email',
    providerUrl: '',
    providerSecret: '',
  })

  const [targetForm, setTargetForm] = useState({
    name: '',
    url: '',
    secret: '',
    includeRecipientIdentity: false,
  })

  const [draft, setDraft] = useState({
    requestId: '',
    title: '',
    body: '',
    kind: 'status_change',
    notifySubscribers: true,
    email: true,
    webhook: true,
  })

  const [subscriberRequestId, setSubscriberRequestId] = useState('')
  const [subscribers, setSubscribers] = useState<RequestSubscriber[]>([])
  const [suppressingContactId, setSuppressingContactId] = useState('')

  useEffect(() => {
    if (!settings) return
    setEmailEnabled(settings.emailEnabled)
    setWebhookEnabled(settings.webhookEnabled)
    setConsentMode(normalizeConsentMode(settings.defaultConsentMode))
    setRequirePublicUpdate(settings.requirePublicUpdateForStatus)
    setMaxRecipientsWithoutConfirm(String(settings.maxRecipientsWithoutConfirm || 0))
    setTenantHourlySendLimit(String(settings.tenantHourlySendLimit || 0))
    setContactDailySendLimit(String(settings.contactDailySendLimit || 0))
    setEnabledEventTypes(booleanPolicyFromSettings(settings.enabledEventTypes, eventTypeOptions))
    setStatusPolicy(booleanPolicyFromSettings(settings.statusPolicy, statusPolicyOptions))
  }, [settings])

  useEffect(() => {
    if (!sender) return
    setSenderForm((current) => ({
      ...current,
      fromName: sender.fromName || current.fromName,
      provider: sender.provider || current.provider,
    }))
  }, [sender])

  const failedDeliveries = deliveries.filter((delivery) =>
    ['failed', 'dead'].includes(delivery.status),
  )
  const activeTargets = targets.filter((target) => target.status === 'active')
  const selectedChannels = useMemo(
    () => channelList(draft.email, draft.webhook),
    [draft.email, draft.webhook],
  )
  const canDraft = Boolean(draft.requestId.trim() && draft.title.trim() && draft.body.trim())

  const updateEnabledEventType = (key: string, checked: boolean) => {
    setEnabledEventTypes((current) => ({ ...current, [key]: checked }))
  }

  const updateStatusPolicy = (key: string, checked: boolean) => {
    setStatusPolicy((current) => ({ ...current, [key]: checked }))
  }

  const handleSettingsSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    updateSettings.mutate(
      {
        emailEnabled,
        webhookEnabled,
        enabledEventTypes,
        statusPolicy,
        defaultConsentMode: normalizeConsentMode(consentMode, settings?.defaultConsentMode),
        requirePublicUpdateForStatus: requirePublicUpdate,
        maxRecipientsWithoutConfirm: numberOrZero(maxRecipientsWithoutConfirm),
        tenantHourlySendLimit: numberOrZero(tenantHourlySendLimit),
        contactDailySendLimit: numberOrZero(contactDailySendLimit),
      },
      {
        onSuccess: () => toast.success(t('request_notifications.toast.settings_saved')),
        onError: (err) => toast.error(errorMessage(err, t('common.error'))),
      },
    )
  }

  const handleSenderSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    upsertSender.mutate(
      {
        fromName: senderForm.fromName.trim(),
        fromEmail: senderForm.fromEmail.trim(),
        replyTo: optionalValue(senderForm.replyTo),
        provider: senderForm.provider.trim() || 'email',
        providerUrl: senderForm.providerUrl.trim(),
        providerSecret: optionalValue(senderForm.providerSecret),
      },
      {
        onSuccess: () => {
          setSenderForm((current) => ({
            ...current,
            fromEmail: '',
            providerUrl: '',
            providerSecret: '',
          }))
          toast.success(t('request_notifications.toast.sender_saved'))
        },
        onError: (err) => toast.error(errorMessage(err, t('common.error'))),
      },
    )
  }

  const handleVerifySender = () => {
    const senderId = sender?.id
    /* v8 ignore next -- @preserve: the verify button is disabled unless a sender id is present. */
    if (senderId) {
      verifySender.mutate(senderId, {
        onSuccess: () => toast.success(t('request_notifications.toast.sender_verified')),
        onError: (err) => toast.error(errorMessage(err, t('common.error'))),
      })
    }
  }

  const handleTargetSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    createTarget.mutate(
      {
        name: targetForm.name.trim(),
        url: targetForm.url.trim(),
        secret: optionalValue(targetForm.secret),
        includeRecipientIdentity: targetForm.includeRecipientIdentity,
      },
      {
        onSuccess: () => {
          setTargetForm({
            name: '',
            url: '',
            secret: '',
            includeRecipientIdentity: false,
          })
          toast.success(t('request_notifications.toast.webhook_saved'))
        },
        onError: (err) => toast.error(errorMessage(err, t('common.error'))),
      },
    )
  }

  const handleTestTarget = (target: RequestNotificationWebhookTarget) => {
    testTarget.mutate(target.id, {
      onSuccess: (result) => {
        if (result.ok) {
          toast.success(t('request_notifications.toast.webhook_test_ok'))
          return
        }
        toast.error(result.message || t('request_notifications.toast.webhook_test_failed'))
      },
      onError: (err) => toast.error(errorMessage(err, t('common.error'))),
    })
  }

  const handleDeleteTarget = (target: RequestNotificationWebhookTarget) => {
    if (!window.confirm(t('request_notifications.webhooks.delete_confirm'))) return
    deleteTarget.mutate(target.id, {
      onSuccess: () => toast.success(t('request_notifications.toast.webhook_deleted')),
      onError: (err) => toast.error(errorMessage(err, t('common.error'))),
    })
  }

  const handlePreview = () => {
    preview.mutate(
      {
        update: {
          requestId: draft.requestId.trim(),
          title: draft.title.trim(),
          body: draft.body.trim(),
          kind: draft.kind,
          notifySubscribers: draft.notifySubscribers,
        },
        channels: selectedChannels,
      },
      {
        onError: (err) => toast.error(errorMessage(err, t('common.error'))),
      },
    )
  }

  const handlePublish = () => {
    const threshold = settings?.maxRecipientsWithoutConfirm ?? 0
    const previewRecipients = preview.data?.eligibleRecipients ?? 0
    const needsLargeAudienceConfirm = draft.email && threshold > 0 && previewRecipients > threshold
    const confirmLargeAudience =
      needsLargeAudienceConfirm &&
      window.confirm(
        t('request_notifications.publish.large_audience_confirm', {
          count: previewRecipients,
          limit: threshold,
        }),
      )
    if (needsLargeAudienceConfirm && !confirmLargeAudience) return
    publish.mutate(
      {
        update: {
          requestId: draft.requestId.trim(),
          title: draft.title.trim(),
          body: draft.body.trim(),
          kind: draft.kind,
          notifySubscribers: draft.notifySubscribers,
        },
        channels: selectedChannels,
        confirmLargeAudience,
      },
      {
        onSuccess: () => {
          toast.success(t('request_notifications.toast.published'))
          setDraft((current) => ({ ...current, title: '', body: '' }))
        },
        onError: (err) => toast.error(errorMessage(err, t('common.error'))),
      },
    )
  }

  const handleRetryDelivery = (delivery: RequestNotificationDelivery) => {
    retryDelivery.mutate(delivery.id, {
      onSuccess: () => toast.success(t('request_notifications.toast.delivery_retried')),
      onError: (err) => toast.error(errorMessage(err, t('common.error'))),
    })
  }

  const handleLoadSubscribers = () => {
    const requestId = subscriberRequestId.trim()
    if (!requestId) return
    listSubscribers.mutate(requestId, {
      onSuccess: setSubscribers,
      onError: (err) => toast.error(errorMessage(err, t('common.error'))),
    })
  }

  const handleSuppressSubscriber = (subscriber: RequestSubscriber) => {
    setSuppressingContactId(subscriber.contactId)
    suppressSubscriber.mutate(
      {
        contactId: subscriber.contactId,
        reason: 'operator_suppressed',
      },
      {
        onSuccess: (updated) => {
          setSubscribers((current) =>
            current.map((item) => (item.contactId === updated.contactId ? updated : item)),
          )
          toast.success(t('request_notifications.toast.subscriber_suppressed'))
        },
        onError: (err) => toast.error(errorMessage(err, t('common.error'))),
        onSettled: () => setSuppressingContactId(''),
      },
    )
  }

  return (
    <div className="space-y-6">
      <PageHero
        eyebrow={t('shell.groups.integrations')}
        title={t('nav.request_notifications')}
        subtitle={t('request_notifications.subtitle')}
        actions={
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => {
              settingsQuery.refetch()
              senderQuery.refetch()
              targetsQuery.refetch()
              deliveriesQuery.refetch()
            }}
          >
            <RefreshCcw className="size-4" />
            {t('request_notifications.refresh')}
          </Button>
        }
        metrics={
          <>
            <PageHeroMetric
              label={t('request_notifications.summary.email')}
              value={t(
                `request_notifications.enabled.${settings?.emailEnabled ? 'enabled' : 'disabled'}`,
              )}
              tone={settings?.emailEnabled ? 'active' : 'default'}
            />
            <PageHeroMetric
              label={t('request_notifications.summary.webhook')}
              value={t(
                `request_notifications.enabled.${
                  settings?.webhookEnabled ? 'enabled' : 'disabled'
                }`,
              )}
              tone={settings?.webhookEnabled ? 'active' : 'default'}
            />
            <PageHeroMetric
              label={t('request_notifications.summary.targets')}
              value={`${activeTargets.length}/${targets.length}`}
            />
            <PageHeroMetric
              label={t('request_notifications.summary.failures')}
              value={String(failedDeliveries.length)}
              tone={failedDeliveries.length > 0 ? 'urgent' : 'default'}
            />
          </>
        }
      />

      <div className="grid gap-6 xl:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Bell className="size-4" />
              {t('request_notifications.settings.title')}
            </CardTitle>
            <CardDescription>{t('request_notifications.settings.description')}</CardDescription>
          </CardHeader>
          <CardContent>
            <form className="space-y-4" onSubmit={handleSettingsSubmit}>
              <ToggleRow
                checked={emailEnabled}
                description={t('request_notifications.settings.email_help')}
                label={t('request_notifications.settings.email')}
                onCheckedChange={setEmailEnabled}
                testId="rn-email-enabled"
              />
              <ToggleRow
                checked={webhookEnabled}
                description={t('request_notifications.settings.webhook_help')}
                label={t('request_notifications.settings.webhook')}
                onCheckedChange={setWebhookEnabled}
                testId="rn-webhook-enabled"
              />
              <ToggleRow
                checked={requirePublicUpdate}
                description={t('request_notifications.settings.public_update_help')}
                label={t('request_notifications.settings.public_update')}
                onCheckedChange={setRequirePublicUpdate}
                testId="rn-require-public-update"
              />
              <PolicyToggleGroup
                help={t('request_notifications.settings.event_types_help')}
                onChange={updateEnabledEventType}
                options={eventTypeOptions}
                title={t('request_notifications.settings.event_types')}
                values={enabledEventTypes}
              />
              <PolicyToggleGroup
                help={t('request_notifications.settings.status_policy_help')}
                onChange={updateStatusPolicy}
                options={statusPolicyOptions}
                title={t('request_notifications.settings.status_policy')}
                values={statusPolicy}
              />
              <FormField
                label={t('request_notifications.settings.consent_mode')}
                help={t('request_notifications.settings.consent_mode_help')}
              >
                <Select value={consentMode} onValueChange={setConsentMode}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="explicit_opt_in">
                      {t('request_notifications.consent.explicit_opt_in')}
                    </SelectItem>
                    <SelectItem value="existing_app_consent">
                      {t('request_notifications.consent.existing_app_consent')}
                    </SelectItem>
                    <SelectItem value="disabled">
                      {t('request_notifications.consent.disabled')}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </FormField>
              <div className="grid gap-3 sm:grid-cols-3">
                <FormField label={t('request_notifications.settings.max_unconfirmed')}>
                  <Input
                    data-testid="rn-max-unconfirmed"
                    min="0"
                    type="number"
                    value={maxRecipientsWithoutConfirm}
                    onChange={(event) => setMaxRecipientsWithoutConfirm(event.target.value)}
                  />
                </FormField>
                <FormField label={t('request_notifications.settings.tenant_hourly')}>
                  <Input
                    data-testid="rn-tenant-hourly"
                    min="0"
                    type="number"
                    value={tenantHourlySendLimit}
                    onChange={(event) => setTenantHourlySendLimit(event.target.value)}
                  />
                </FormField>
                <FormField label={t('request_notifications.settings.contact_daily')}>
                  <Input
                    data-testid="rn-contact-daily"
                    min="0"
                    type="number"
                    value={contactDailySendLimit}
                    onChange={(event) => setContactDailySendLimit(event.target.value)}
                  />
                </FormField>
              </div>
              <div className="flex justify-end">
                <Button
                  type="submit"
                  disabled={updateSettings.isPending}
                  data-testid="rn-settings-save"
                >
                  {updateSettings.isPending && <Loader2 className="size-4 animate-spin" />}
                  {t('common.save')}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>

        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Mail className="size-4" />
              {t('request_notifications.sender.title')}
            </CardTitle>
            <CardDescription>{t('request_notifications.sender.description')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <SenderSummary sender={sender} isLoading={senderQuery.isPending} />
            <form className="space-y-4" onSubmit={handleSenderSubmit}>
              <div className="grid gap-3 sm:grid-cols-2">
                <FormField label={t('request_notifications.sender.from_name')}>
                  <Input
                    data-testid="rn-sender-from-name"
                    value={senderForm.fromName}
                    onChange={(event) =>
                      setSenderForm((current) => ({ ...current, fromName: event.target.value }))
                    }
                  />
                </FormField>
                <FormField label={t('request_notifications.sender.from_email')}>
                  <Input
                    data-testid="rn-sender-from-email"
                    placeholder={sender?.fromEmailRedacted || ''}
                    value={senderForm.fromEmail}
                    onChange={(event) =>
                      setSenderForm((current) => ({ ...current, fromEmail: event.target.value }))
                    }
                  />
                </FormField>
                <FormField label={t('request_notifications.sender.reply_to')}>
                  <Input
                    data-testid="rn-sender-reply-to"
                    placeholder={sender?.replyToRedacted || t('common.optional')}
                    value={senderForm.replyTo}
                    onChange={(event) =>
                      setSenderForm((current) => ({ ...current, replyTo: event.target.value }))
                    }
                  />
                </FormField>
                <FormField label={t('request_notifications.sender.provider')}>
                  <Input
                    data-testid="rn-sender-provider"
                    value={senderForm.provider}
                    onChange={(event) =>
                      setSenderForm((current) => ({ ...current, provider: event.target.value }))
                    }
                  />
                </FormField>
              </div>
              <FormField
                label={t('request_notifications.sender.provider_url')}
                help={t('request_notifications.sender.provider_url_help')}
              >
                <Input
                  data-testid="rn-sender-provider-url"
                  value={senderForm.providerUrl}
                  onChange={(event) =>
                    setSenderForm((current) => ({ ...current, providerUrl: event.target.value }))
                  }
                />
              </FormField>
              <FormField label={t('request_notifications.sender.provider_secret')}>
                <Input
                  data-testid="rn-sender-provider-secret"
                  type="password"
                  value={senderForm.providerSecret}
                  onChange={(event) =>
                    setSenderForm((current) => ({
                      ...current,
                      providerSecret: event.target.value,
                    }))
                  }
                />
              </FormField>
              <div className="flex flex-wrap justify-end gap-2">
                <Button
                  type="button"
                  variant="outline"
                  disabled={!sender?.id || verifySender.isPending}
                  onClick={handleVerifySender}
                  data-testid="rn-sender-verify"
                >
                  {verifySender.isPending ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <CheckCircle2 className="size-4" />
                  )}
                  {t('request_notifications.sender.verify')}
                </Button>
                <Button
                  type="submit"
                  disabled={upsertSender.isPending}
                  data-testid="rn-sender-save"
                >
                  {upsertSender.isPending && <Loader2 className="size-4 animate-spin" />}
                  {t('request_notifications.sender.save')}
                </Button>
              </div>
            </form>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(0,0.95fr)_minmax(0,1.05fr)]">
        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Webhook className="size-4" />
              {t('request_notifications.webhooks.title')}
            </CardTitle>
            <CardDescription>{t('request_notifications.webhooks.description')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-5">
            <form className="space-y-4" onSubmit={handleTargetSubmit}>
              <div className="grid gap-3 sm:grid-cols-2">
                <FormField label={t('request_notifications.webhooks.name')}>
                  <Input
                    data-testid="rn-target-name"
                    value={targetForm.name}
                    onChange={(event) =>
                      setTargetForm((current) => ({ ...current, name: event.target.value }))
                    }
                  />
                </FormField>
                <FormField label={t('request_notifications.webhooks.url')}>
                  <Input
                    data-testid="rn-target-url"
                    value={targetForm.url}
                    onChange={(event) =>
                      setTargetForm((current) => ({ ...current, url: event.target.value }))
                    }
                  />
                </FormField>
              </div>
              <FormField label={t('request_notifications.webhooks.secret')}>
                <Input
                  data-testid="rn-target-secret"
                  type="password"
                  value={targetForm.secret}
                  onChange={(event) =>
                    setTargetForm((current) => ({ ...current, secret: event.target.value }))
                  }
                />
              </FormField>
              <ToggleRow
                checked={targetForm.includeRecipientIdentity}
                description={t('request_notifications.webhooks.identity_help')}
                label={t('request_notifications.webhooks.identity')}
                onCheckedChange={(checked) =>
                  setTargetForm((current) => ({
                    ...current,
                    includeRecipientIdentity: checked,
                  }))
                }
                testId="rn-target-include-identity"
              />
              <div className="flex justify-end">
                <Button
                  type="submit"
                  disabled={createTarget.isPending}
                  data-testid="rn-target-create"
                >
                  {createTarget.isPending && <Loader2 className="size-4 animate-spin" />}
                  {t('request_notifications.webhooks.create')}
                </Button>
              </div>
            </form>
            <TargetList
              deletingId={deleteTarget.isPending ? (deleteTarget.variables ?? '') : ''}
              onDelete={handleDeleteTarget}
              onTest={handleTestTarget}
              targets={targets}
              testingId={testTarget.isPending ? (testTarget.variables ?? '') : ''}
            />
          </CardContent>
        </Card>

        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Send className="size-4" />
              {t('request_notifications.publish.title')}
            </CardTitle>
            <CardDescription>{t('request_notifications.publish.description')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_180px]">
              <FormField label={t('request_notifications.publish.request_id')}>
                <Input
                  data-testid="rn-draft-request-id"
                  value={draft.requestId}
                  onChange={(event) =>
                    setDraft((current) => ({ ...current, requestId: event.target.value }))
                  }
                />
              </FormField>
              <FormField label={t('request_notifications.publish.kind')}>
                <Select
                  value={draft.kind}
                  onValueChange={(kind) => setDraft((current) => ({ ...current, kind }))}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="status_change">
                      {t('request_notifications.publish.kind_status')}
                    </SelectItem>
                    <SelectItem value="shipped">
                      {t('request_notifications.publish.kind_shipped')}
                    </SelectItem>
                  </SelectContent>
                </Select>
              </FormField>
            </div>
            <FormField label={t('request_notifications.publish.title_label')}>
              <Input
                data-testid="rn-draft-title"
                value={draft.title}
                onChange={(event) =>
                  setDraft((current) => ({ ...current, title: event.target.value }))
                }
              />
            </FormField>
            <FormField label={t('request_notifications.publish.body')}>
              <textarea
                data-testid="rn-draft-body"
                className="min-h-28 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow] focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
                value={draft.body}
                onChange={(event) =>
                  setDraft((current) => ({ ...current, body: event.target.value }))
                }
              />
            </FormField>
            <div className="grid gap-3 sm:grid-cols-3">
              <ToggleRow
                checked={draft.notifySubscribers}
                compact
                label={t('request_notifications.publish.notify')}
                onCheckedChange={(checked) =>
                  setDraft((current) => ({ ...current, notifySubscribers: checked }))
                }
                testId="rn-draft-notify"
              />
              <ToggleRow
                checked={draft.email}
                compact
                label={t('request_notifications.channels.email')}
                onCheckedChange={(checked) =>
                  setDraft((current) => ({ ...current, email: checked }))
                }
                testId="rn-draft-email"
              />
              <ToggleRow
                checked={draft.webhook}
                compact
                label={t('request_notifications.channels.webhook')}
                onCheckedChange={(checked) =>
                  setDraft((current) => ({ ...current, webhook: checked }))
                }
                testId="rn-draft-webhook"
              />
            </div>
            <div className="flex flex-wrap justify-end gap-2">
              <Button
                type="button"
                variant="outline"
                disabled={!canDraft || preview.isPending}
                onClick={handlePreview}
                data-testid="rn-preview"
              >
                {preview.isPending ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <PlayCircle className="size-4" />
                )}
                {t('request_notifications.publish.preview')}
              </Button>
              <Button
                type="button"
                disabled={!canDraft || publish.isPending}
                onClick={handlePublish}
                data-testid="rn-publish"
              >
                {publish.isPending && <Loader2 className="size-4 animate-spin" />}
                {t('request_notifications.publish.publish')}
              </Button>
            </div>
            {preview.data && (
              <Alert>
                <Bell className="size-4" />
                <AlertTitle>{t('request_notifications.preview.title')}</AlertTitle>
                <AlertDescription className="space-y-3">
                  <div className="grid gap-2 sm:grid-cols-3">
                    <Stat
                      label={t('request_notifications.preview.eligible')}
                      value={preview.data.eligibleRecipients}
                    />
                    <Stat
                      label={t('request_notifications.preview.excluded')}
                      value={preview.data.excludedRecipients}
                    />
                    <Stat
                      label={t('request_notifications.preview.channels')}
                      value={selectedChannels.length}
                    />
                  </div>
                  <pre className="max-h-48 overflow-auto rounded-md bg-muted p-3 text-xs text-muted-foreground">
                    {JSON.stringify(
                      {
                        email: preview.data.emailPayload,
                        webhook: preview.data.webhookPayload,
                        excludedByReason: preview.data.excludedByReason,
                      },
                      null,
                      2,
                    )}
                  </pre>
                </AlertDescription>
              </Alert>
            )}
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-6 xl:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <RefreshCcw className="size-4" />
              {t('request_notifications.deliveries.title')}
            </CardTitle>
            <CardDescription>{t('request_notifications.deliveries.description')}</CardDescription>
          </CardHeader>
          <CardContent>
            <DeliveryTable
              deliveries={deliveries}
              locale={locale}
              onRetry={handleRetryDelivery}
              retryingId={retryDelivery.isPending ? (retryDelivery.variables ?? '') : ''}
            />
          </CardContent>
        </Card>

        <Card className="border-border/60 shadow-none">
          <CardHeader>
            <CardTitle className="flex items-center gap-2 text-base">
              <Users className="size-4" />
              {t('request_notifications.subscribers.title')}
            </CardTitle>
            <CardDescription>{t('request_notifications.subscribers.description')}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex flex-col gap-2 sm:flex-row">
              <Input
                data-testid="rn-subscriber-request-id"
                value={subscriberRequestId}
                onChange={(event) => setSubscriberRequestId(event.target.value)}
                placeholder={t('request_notifications.subscribers.request_placeholder')}
              />
              <Button
                type="button"
                variant="outline"
                disabled={listSubscribers.isPending}
                onClick={handleLoadSubscribers}
                data-testid="rn-subscribers-load"
              >
                {listSubscribers.isPending && <Loader2 className="size-4 animate-spin" />}
                {t('request_notifications.subscribers.load')}
              </Button>
            </div>
            <SubscriberList
              onSuppress={handleSuppressSubscriber}
              subscribers={subscribers}
              suppressingContactId={suppressingContactId}
            />
          </CardContent>
        </Card>
      </div>
    </div>
  )
}

function ToggleRow({
  checked,
  compact,
  description,
  label,
  onCheckedChange,
  testId,
}: {
  checked: boolean
  compact?: boolean
  description?: string
  label: string
  onCheckedChange: (checked: boolean) => void
  testId?: string
}) {
  return (
    <div
      className={cn(
        'flex cursor-pointer items-start gap-3 rounded-md border border-border/70 p-3',
        compact && 'items-center py-2',
      )}
    >
      <Checkbox
        checked={checked}
        data-testid={testId}
        onCheckedChange={(value) => onCheckedChange(value === true)}
      />
      <span className="min-w-0 space-y-0.5">
        <span className="block text-sm font-medium text-foreground">{label}</span>
        {description && <span className="block text-xs text-muted-foreground">{description}</span>}
      </span>
    </div>
  )
}

function PolicyToggleGroup({
  help,
  onChange,
  options,
  title,
  values,
}: {
  help: string
  onChange: (key: string, checked: boolean) => void
  options: PolicyOption[]
  title: string
  values: BooleanPolicy
}) {
  const { t } = useTranslation()
  return (
    <div className="space-y-2 rounded-md border border-border/70 p-3">
      <div className="space-y-0.5">
        <Label>{title}</Label>
        <p className="text-xs text-muted-foreground">{help}</p>
      </div>
      <div className="grid gap-2 sm:grid-cols-2">
        {options.map((option) => (
          <ToggleRow
            checked={policyEnabled(values, option.key)}
            compact
            description={t(option.descriptionKey)}
            key={option.key}
            label={t(option.labelKey)}
            onCheckedChange={(checked) => onChange(option.key, checked)}
            testId={option.testId}
          />
        ))}
      </div>
    </div>
  )
}

function FormField({
  children,
  help,
  label,
}: {
  children: ReactNode
  help?: string
  label: string
}) {
  return (
    <div className="space-y-1.5">
      <Label>{label}</Label>
      {children}
      {help && <p className="text-xs text-muted-foreground">{help}</p>}
    </div>
  )
}

function SenderSummary({
  isLoading,
  sender,
}: {
  isLoading: boolean
  sender: RequestNotificationSender | null
}) {
  const { t } = useTranslation()
  if (isLoading) {
    return (
      <div
        className="flex items-center gap-2 rounded-md border border-border/70 p-3 text-sm text-muted-foreground"
        data-testid="rn-sender-loading"
      >
        <Loader2 className="size-4 animate-spin" />
        {t('common.loading')}
      </div>
    )
  }
  if (!sender) {
    return (
      <Alert data-testid="rn-sender-empty">
        <Mail className="size-4" />
        <AlertTitle>{t('request_notifications.sender.empty_title')}</AlertTitle>
        <AlertDescription>{t('request_notifications.sender.empty_body')}</AlertDescription>
      </Alert>
    )
  }
  return (
    <div className="grid gap-2 rounded-md border border-border/70 p-3 text-sm sm:grid-cols-2">
      <SummaryItem
        label={t('request_notifications.sender.current_from')}
        value={sender.fromEmailRedacted}
      />
      <SummaryItem label={t('request_notifications.sender.current_domain')} value={sender.domain} />
      <SummaryItem
        label={t('request_notifications.sender.current_provider')}
        value={sender.provider}
      />
      <SummaryItem
        label={t('request_notifications.sender.current_status')}
        value={statusLabel(t, sender.status)}
      />
    </div>
  )
}

function TargetList({
  deletingId,
  onDelete,
  onTest,
  targets,
  testingId,
}: {
  deletingId: string
  onDelete: (target: RequestNotificationWebhookTarget) => void
  onTest: (target: RequestNotificationWebhookTarget) => void
  targets: RequestNotificationWebhookTarget[]
  testingId: string
}) {
  const { t } = useTranslation()
  if (targets.length === 0) {
    return (
      <div
        className="rounded-md border border-dashed border-border/80 p-4 text-sm text-muted-foreground"
        data-testid="rn-targets-empty"
      >
        {t('request_notifications.webhooks.empty')}
      </div>
    )
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('request_notifications.webhooks.name')}</TableHead>
          <TableHead>{t('request_notifications.webhooks.host')}</TableHead>
          <TableHead>{t('request_notifications.webhooks.status')}</TableHead>
          <TableHead className="text-right">{t('request_notifications.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {targets.map((target) => (
          <TableRow key={target.id}>
            <TableCell className="font-medium">{target.name}</TableCell>
            <TableCell>{target.urlHost || '-'}</TableCell>
            <TableCell>
              <StatusPill status={target.status} />
            </TableCell>
            <TableCell className="text-right">
              <div className="inline-flex gap-1">
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t('request_notifications.webhooks.test')}
                  disabled={testingId === target.id}
                  onClick={() => onTest(target)}
                  data-testid={`rn-target-test-${target.id}`}
                >
                  {testingId === target.id ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <PlayCircle className="size-4" />
                  )}
                </Button>
                <Button
                  type="button"
                  variant="ghost"
                  size="icon-sm"
                  aria-label={t('common.delete')}
                  disabled={deletingId === target.id}
                  onClick={() => onDelete(target)}
                  data-testid={`rn-target-delete-${target.id}`}
                >
                  {deletingId === target.id ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <Trash2 className="size-4" />
                  )}
                </Button>
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function DeliveryTable({
  deliveries,
  locale,
  onRetry,
  retryingId,
}: {
  deliveries: RequestNotificationDelivery[]
  locale?: string
  onRetry: (delivery: RequestNotificationDelivery) => void
  retryingId: string
}) {
  const { t } = useTranslation()
  if (deliveries.length === 0) {
    return (
      <div
        className="rounded-md border border-dashed border-border/80 p-4 text-sm text-muted-foreground"
        data-testid="rn-deliveries-empty"
      >
        {t('request_notifications.deliveries.empty')}
      </div>
    )
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('request_notifications.deliveries.channel')}</TableHead>
          <TableHead>{t('request_notifications.deliveries.status')}</TableHead>
          <TableHead>{t('request_notifications.deliveries.attempts')}</TableHead>
          <TableHead>{t('request_notifications.deliveries.time')}</TableHead>
          <TableHead>{t('request_notifications.deliveries.error')}</TableHead>
          <TableHead className="text-right">{t('request_notifications.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {deliveries.map((delivery) => (
          <TableRow key={delivery.id}>
            <TableCell>{channelLabel(t, delivery.channel)}</TableCell>
            <TableCell>
              <StatusPill status={delivery.status} />
            </TableCell>
            <TableCell>{delivery.attempts}</TableCell>
            <TableCell>{formatTime(delivery.createdAt, locale)}</TableCell>
            <TableCell className="max-w-[18rem] truncate text-muted-foreground">
              {delivery.lastError || delivery.deadReason || '-'}
            </TableCell>
            <TableCell className="text-right">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t('common.retry')}
                disabled={!canRetry(delivery) || retryingId === delivery.id}
                onClick={() => onRetry(delivery)}
                data-testid={`rn-delivery-retry-${delivery.id}`}
              >
                {retryingId === delivery.id ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <RefreshCcw className="size-4" />
                )}
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function SubscriberList({
  onSuppress,
  subscribers,
  suppressingContactId,
}: {
  onSuppress: (subscriber: RequestSubscriber) => void
  subscribers: RequestSubscriber[]
  suppressingContactId: string
}) {
  const { t } = useTranslation()
  if (subscribers.length === 0) {
    return (
      <div
        className="rounded-md border border-dashed border-border/80 p-4 text-sm text-muted-foreground"
        data-testid="rn-subscribers-empty"
      >
        {t('request_notifications.subscribers.empty')}
      </div>
    )
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('request_notifications.subscribers.person')}</TableHead>
          <TableHead>{t('request_notifications.subscribers.email')}</TableHead>
          <TableHead>{t('request_notifications.subscribers.status')}</TableHead>
          <TableHead className="text-right">{t('request_notifications.actions')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {subscribers.map((subscriber) => (
          <TableRow key={subscriber.contactId}>
            <TableCell className="font-medium">
              {subscriber.displayName || subscriber.organization || '-'}
            </TableCell>
            <TableCell>{subscriber.emailRedacted}</TableCell>
            <TableCell>
              <StatusPill status={subscriber.consentState || subscriber.subscriptionStatus} />
            </TableCell>
            <TableCell className="text-right">
              <Button
                type="button"
                variant="ghost"
                size="icon-sm"
                aria-label={t('request_notifications.subscribers.suppress')}
                disabled={suppressingContactId === subscriber.contactId}
                onClick={() => onSuppress(subscriber)}
                data-testid={`rn-subscriber-suppress-${subscriber.contactId}`}
              >
                {suppressingContactId === subscriber.contactId ? (
                  <Loader2 className="size-4 animate-spin" />
                ) : (
                  <ShieldOff className="size-4" />
                )}
              </Button>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function SummaryItem({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="truncate font-medium">{value || '-'}</div>
    </div>
  )
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-md border border-border/70 p-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="text-lg font-semibold tabular-nums">{value}</div>
    </div>
  )
}

function StatusPill({ status }: { status: string }) {
  const { t } = useTranslation()
  const normalized = status || 'unknown'
  return (
    <span
      className={cn(
        'inline-flex rounded-md border px-2 py-0.5 text-xs font-medium',
        statusTone(normalized),
      )}
    >
      {statusLabel(t, normalized)}
    </span>
  )
}

export function channelList(email: boolean, webhook: boolean) {
  const channels: RequestNotificationChannel[] = []
  if (email) {
    channels.push(RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL)
  }
  if (webhook) {
    channels.push(RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK)
  }
  return channels
}

export function defaultBooleanPolicy(options: PolicyOption[]): BooleanPolicy {
  return Object.fromEntries(options.map((option) => [option.key, true]))
}

export function booleanPolicyFromSettings(
  values: { [key: string]: unknown } | undefined,
  options: PolicyOption[],
): BooleanPolicy {
  const out: BooleanPolicy = {}
  for (const [key, value] of Object.entries(values ?? {})) {
    if (typeof value === 'boolean') {
      out[key] = value
    }
  }
  for (const option of options) {
    if (out[option.key] === undefined) {
      out[option.key] = true
    }
  }
  return out
}

export function normalizeConsentMode(value: string | undefined, fallback?: string) {
  const trimmed = (value ?? '').trim()
  if (consentModeValues.has(trimmed)) return trimmed
  const fallbackValue = (fallback ?? '').trim()
  if (consentModeValues.has(fallbackValue)) return fallbackValue
  return 'explicit_opt_in'
}

export function policyEnabled(values: BooleanPolicy, key: string) {
  return values[key] !== false
}

export function numberOrZero(value: string) {
  const parsed = Number.parseInt(value, 10)
  if (Number.isNaN(parsed) || parsed < 0) return 0
  return parsed
}

export function optionalValue(value: string) {
  const trimmed = value.trim()
  return trimmed || undefined
}

export function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback
}

export function canRetry(delivery: RequestNotificationDelivery) {
  return delivery.status === 'failed' || delivery.status === 'dead'
}

export function formatTime(value: string, locale?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString(locale, {
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    month: '2-digit',
  })
}

export function statusTone(status: string) {
  if (['active', 'delivered', 'verified', 'opted_in'].includes(status)) {
    return 'border-emerald-300 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950 dark:text-emerald-200'
  }
  if (['failed', 'dead', 'suppressed', 'disabled', 'opted_out'].includes(status)) {
    return 'border-destructive/30 bg-destructive/10 text-destructive'
  }
  return 'border-border bg-muted text-muted-foreground'
}

export function statusLabel(t: (key: string) => string, status: string) {
  const labels: Record<string, string> = {
    active: t('request_notifications.status.active'),
    dead: t('request_notifications.status.dead'),
    delivered: t('request_notifications.status.delivered'),
    disabled: t('request_notifications.status.disabled'),
    failed: t('request_notifications.status.failed'),
    opted_in: t('request_notifications.status.opted_in'),
    opted_out: t('request_notifications.status.opted_out'),
    pending: t('request_notifications.status.pending'),
    suppressed: t('request_notifications.status.suppressed'),
    unverified: t('request_notifications.status.unverified'),
    verified: t('request_notifications.status.verified'),
  }
  return labels[status] ?? status
}

export function channelLabel(t: (key: string) => string, channel: RequestNotificationChannel) {
  switch (channel) {
    case RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL:
      return t('request_notifications.channels.email')
    case RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK:
      return t('request_notifications.channels.webhook')
    default:
      return t('request_notifications.channels.unknown')
  }
}
