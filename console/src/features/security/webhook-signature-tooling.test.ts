import { describe, expect, it } from 'vitest'
import type { ExternalConnection, ExternalSyncEvent } from '@/proto/attune/v1/external_sync'
import {
  ExternalSyncEventSignatureStatus,
  ExternalSyncEventStatus,
} from '@/proto/attune/v1/external_sync'
import type { InboundSource } from '@/proto/attune/v1/inbound_source'
import type { ReplySendHook, ReplySendHookHealth } from '@/proto/attune/v1/ingest'
import {
  RequestNotificationChannel,
  type RequestNotificationDelivery,
  type RequestNotificationSettings,
  type RequestNotificationWebhookTarget,
} from '@/proto/attune/v1/request_notification'
import { buildWebhookSignatureTooling } from './webhook-signature-tooling'

type CompleteReplySendHookHealth = ReplySendHookHealth &
  Required<Pick<ReplySendHookHealth, 'latestDelivery' | 'latestProblem'>>

const inboundSources: InboundSource[] = [
  {
    channel: 'webhook',
    createdAt: '2026-06-01T00:00:00Z',
    enabled: true,
    id: 'src-webhook',
    lastError: '',
    lastEventAt: '2026-07-01T00:00:00Z',
    lastUid: '0',
    name: 'Product webhook',
    slug: 'product-webhook',
    tenantId: 'tenant-1',
    updatedAt: '2026-07-01T00:00:00Z',
  },
]

const replySendHook: ReplySendHook = {
  createdAt: '2026-06-01T00:00:00Z',
  enabled: true,
  id: 'hook-1',
  name: 'Reply hook',
  updatedAt: '2026-07-01T00:00:00Z',
  urlFingerprint: 'sha256:reply',
  urlHost: 'hooks.example.com',
}

const replySendHookHealth: CompleteReplySendHookHealth = {
  accepted: '0',
  dead: '0',
  failed: '1',
  latestDelivery: {
    attempts: 1,
    createdAt: '2026-07-01T00:01:00Z',
    error: 'receiver returned 500',
    eventType: 'reply.test',
    hookFingerprint: 'sha256:reply',
    hookHost: 'hooks.example.com',
    hookId: 'hook-1',
    httpStatus: 500,
    id: 'reply-delivery-1',
    idempotencyKey: 'reply-test-1',
    maxAttempts: 8,
    requestedAt: '2026-07-01T00:01:00Z',
    requestedBy: 'admin-1',
    requestedByType: 'admin',
    retryable: true,
    status: 'failed',
    updatedAt: '2026-07-01T00:01:00Z',
  },
  latestProblem: {
    attempts: 1,
    createdAt: '2026-07-01T00:01:00Z',
    error: 'receiver returned 500',
    eventType: 'reply.test',
    hookFingerprint: 'sha256:reply',
    hookHost: 'hooks.example.com',
    hookId: 'hook-1',
    httpStatus: 500,
    id: 'reply-delivery-1',
    idempotencyKey: 'reply-test-1',
    maxAttempts: 8,
    requestedAt: '2026-07-01T00:01:00Z',
    requestedBy: 'admin-1',
    requestedByType: 'admin',
    retryable: true,
    status: 'failed',
    updatedAt: '2026-07-01T00:01:00Z',
  },
  pending: '0',
  retryable: '1',
  total: '1',
}

const requestNotificationSettings: RequestNotificationSettings = {
  contactDailySendLimit: 10,
  createdAt: '2026-07-16T00:00:00Z',
  defaultConsentMode: 'explicit_opt_in',
  emailEnabled: true,
  enabledEventTypes: { 'request.status_changed': true },
  maxRecipientsWithoutConfirm: 100,
  requirePublicUpdateForStatus: true,
  statusPolicy: { shipped: true },
  tenantHourlySendLimit: 1000,
  tenantId: 'tenant-1',
  updatedAt: '2026-07-16T00:00:00Z',
  updatedBy: 'admin-1',
  webhookEnabled: true,
}

const requestNotificationWebhookTargets: RequestNotificationWebhookTarget[] = [
  {
    createdAt: '2026-07-16T00:00:00Z',
    eventMask: { 'request.shipped': true },
    id: 'rn-target-1',
    includeRecipientIdentity: true,
    name: 'Customer CRM',
    signatureVersion: 'v1',
    status: 'active',
    updatedAt: '2026-07-16T00:00:00Z',
    url: 'https://hooks.example.test/request-notifications',
    urlHost: 'hooks.example.test',
  },
]

const requestNotificationDeliveries: RequestNotificationDelivery[] = [
  {
    attempts: 2,
    channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL,
    createdAt: '2026-07-16T00:00:00Z',
    deadReason: '',
    destinationHash: 'sha256:rn-email',
    eventId: 'rn-event-1',
    failureKind: 'provider_5xx',
    id: 'rn-delivery-email-1',
    lastError: 'temporary provider outage',
    manualRetryCount: 0,
    retriedBy: '',
    status: 'failed',
    traceId: 'trace-rn-email',
  },
]

const externalSyncConnections: ExternalConnection[] = [
  {
    authType: 'oauth',
    baseUrl: 'https://api.github.com',
    createdAt: '2026-07-08T01:00:00Z',
    createdBy: 'admin-1',
    enabled: true,
    id: 'external-sync-connection-active',
    lastError: '',
    lastTestStatus: 'pass',
    lastTestedAt: '2026-07-08T02:00:00Z',
    name: 'GitHub issues',
    provider: 'github',
    providerInstallationId: 'provider-installation-github',
    providerConfigJson: '{}',
    scopes: ['issues:read'],
    status: 'active',
    tenantId: 'tenant-1',
    updatedAt: '2026-07-08T02:00:00Z',
    updatedBy: 'admin-1',
    webhookSecretConfigured: true,
  },
  {
    authType: 'api_key',
    baseUrl: 'https://api.example.com',
    createdAt: '2026-07-08T01:00:00Z',
    createdBy: 'admin-1',
    enabled: false,
    id: 'external-sync-connection-disabled',
    lastError: 'provider disabled',
    lastTestStatus: 'fail',
    lastTestedAt: '2026-07-08T02:00:00Z',
    name: 'Disabled sync',
    provider: 'jira',
    providerInstallationId: 'provider-installation-jira',
    providerConfigJson: '{}',
    scopes: ['issues:read'],
    status: 'quarantined',
    tenantId: 'tenant-1',
    updatedAt: '2026-07-08T02:00:00Z',
    updatedBy: 'admin-1',
    webhookSecretConfigured: false,
  },
]

const externalSyncEvents: ExternalSyncEvent[] = [
  {
    connectionId: 'external-sync-connection-active',
    createdAt: '2026-07-08T03:02:00Z',
    dedupeKey: 'github:evt-214',
    eventType: 'issues.edited',
    externalEventId: 'evt-214',
    failureReason: 'mapping was disabled when the webhook arrived',
    id: 'external-sync-event-failed',
    mappingId: 'external-sync-mapping-1',
    normalizedPayloadJson: '{"action":"edited"}',
    payloadDigest: 'sha256:eventdigest',
    provider: 'github',
    receivedAt: '2026-07-08T03:02:00Z',
    replayedAt: '',
    replayedBy: '',
    runId: '',
    signatureStatus: ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED,
    status: ExternalSyncEventStatus.EXTERNAL_SYNC_EVENT_STATUS_FAILED,
    tenantId: 'tenant-1',
    updatedAt: '2026-07-08T03:02:00Z',
  },
]

describe('buildWebhookSignatureTooling', () => {
  it('joins signature, fingerprint, test, and diagnostic evidence into one decision', () => {
    const tooling = buildWebhookSignatureTooling({
      externalSyncConnections,
      externalSyncEvents,
      inboundSources,
      replySendHook,
      replySendHookHealth,
      requestNotificationDeliveries,
      requestNotificationSettings,
      requestNotificationWebhookTargets,
    })

    expect(tooling.fingerprint).toBe(
      '1 inbound webhooks / reply hook on / 1 request webhooks / 1 external sync secrets / 0 signature failures',
    )
    expect(tooling.summary).toBe('4 webhook signature checks need attention')
    expect(tooling.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 1, watch: 4 })
    expect(tooling.lanes.find((lane) => lane.key === 'inbound_webhook_signature')).toMatchObject({
      signal: '1 inbound webhooks / 1 enabled / 0 failing',
      status: 'ready',
    })
    expect(tooling.lanes.find((lane) => lane.key === 'reply_hook_fingerprint')).toMatchObject({
      signal: 'reply hook on / fingerprint on / 1 deliveries / 1 failing',
      status: 'watch',
    })
    expect(tooling.lanes.find((lane) => lane.key === 'request_notification_webhook')).toMatchObject(
      {
        signal: '1 request webhooks / 1 signed / 0 tested / 0 webhook failures',
        status: 'watch',
      },
    )
    expect(tooling.lanes.find((lane) => lane.key === 'external_sync_signature')).toMatchObject({
      signal: '2 connections / 1 webhook secrets / 1 verified events / 0 signature failures',
      status: 'watch',
    })
    expect(tooling.lanes.find((lane) => lane.key === 'failure_diagnostics')).toMatchObject({
      signal: '2 signature-path failures / 2 diagnostics / 2 replay paths',
      status: 'watch',
    })
  })

  it('blocks missing request signatures and explicit external signature failures', () => {
    const tooling = buildWebhookSignatureTooling({
      externalSyncConnections,
      externalSyncEvents: [
        {
          ...externalSyncEvents[0],
          failureReason: 'signature mismatch',
          signatureStatus:
            ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_FAILED,
        },
      ],
      inboundSources,
      replySendHook,
      replySendHookHealth,
      requestNotificationDeliveries,
      requestNotificationSettings,
      requestNotificationWebhookTargets: [
        {
          ...requestNotificationWebhookTargets[0],
          signatureVersion: '',
        },
      ],
    })

    expect(tooling.summary).toBe('2 webhook signature checks are blocked')
    expect(tooling.lanes.find((lane) => lane.key === 'request_notification_webhook')).toMatchObject(
      { status: 'blocked' },
    )
    expect(tooling.lanes.find((lane) => lane.key === 'external_sync_signature')).toMatchObject({
      signal: '2 connections / 1 webhook secrets / 0 verified events / 1 signature failures',
      status: 'blocked',
    })
  })

  it('keeps every lane at needs-data when signature-path evidence is absent', () => {
    const tooling = buildWebhookSignatureTooling({})

    expect(tooling.summary).toBe('5 webhook signature checks need evidence')
    expect(tooling.totals).toMatchObject({ blocked: 0, needs_data: 5, ready: 0, watch: 0 })
    expect(tooling.lanes.every((lane) => lane.status === 'needs_data')).toBe(true)
  })

  it('marks a fully exercised signature estate ready', () => {
    const tooling = buildWebhookSignatureTooling({
      externalSyncConnections: [externalSyncConnections[0]],
      externalSyncEvents: [
        {
          ...externalSyncEvents[0],
          failureReason: '',
          payloadDigest: 'sha256:verified',
          signatureStatus:
            ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED,
          status: ExternalSyncEventStatus.EXTERNAL_SYNC_EVENT_STATUS_RECEIVED,
        },
      ],
      inboundSources,
      replySendHook,
      replySendHookHealth: {
        ...replySendHookHealth,
        accepted: '2',
        dead: '0',
        failed: '0',
        latestDelivery: {
          ...replySendHookHealth.latestDelivery,
          error: '',
          retryable: false,
          status: 'accepted',
        },
        latestProblem: undefined,
        retryable: '0',
        total: '2',
      },
      requestNotificationDeliveries: [],
      requestNotificationSettings,
      requestNotificationWebhookTargets: [
        {
          ...requestNotificationWebhookTargets[0],
          lastTestedAt: '2026-07-16T00:05:00Z',
        },
      ],
    })

    expect(tooling.summary).toBe('All webhook signature checks are ready')
    expect(tooling.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 5, watch: 0 })
    expect(tooling.lanes.map((lane) => lane.status)).toEqual([
      'ready',
      'ready',
      'ready',
      'ready',
      'ready',
    ])
  })

  it('watches intentionally disabled or unconfigured webhook surfaces', () => {
    const tooling = buildWebhookSignatureTooling({
      externalSyncConnections: [],
      externalSyncEvents: [],
      inboundSources: [
        {
          ...inboundSources[0],
          channel: 'email',
        },
      ],
      replySendHook: {
        ...replySendHook,
        enabled: false,
      },
      replySendHookHealth: {
        ...replySendHookHealth,
        accepted: '0',
        dead: '0',
        failed: '0',
        latestDelivery: undefined,
        latestProblem: undefined,
        retryable: '0',
        total: '0',
      },
      requestNotificationDeliveries: [],
      requestNotificationSettings: {
        ...requestNotificationSettings,
        webhookEnabled: false,
      },
      requestNotificationWebhookTargets: [],
    })

    expect(tooling.summary).toBe('4 webhook signature checks need attention')
    expect(tooling.lanes.find((lane) => lane.key === 'inbound_webhook_signature')).toMatchObject({
      signal: '0 inbound webhooks / 0 enabled / 0 failing',
      status: 'watch',
    })
    expect(tooling.lanes.find((lane) => lane.key === 'reply_hook_fingerprint')).toMatchObject({
      signal: 'reply hook off / fingerprint on / 0 deliveries / 0 failing',
      status: 'watch',
    })
    expect(tooling.lanes.find((lane) => lane.key === 'request_notification_webhook')).toMatchObject(
      {
        evidence: 'webhook off / 0 active / identity none / 0 failing deliveries',
        status: 'watch',
      },
    )
    expect(tooling.lanes.find((lane) => lane.key === 'external_sync_signature')).toMatchObject({
      signal: '0 connections / 0 webhook secrets / 0 verified events / 0 signature failures',
      status: 'watch',
    })
  })

  it('blocks every unsigned path when diagnostics cannot prove the failure', () => {
    const tooling = buildWebhookSignatureTooling({
      externalSyncConnections: [
        {
          ...externalSyncConnections[0],
          webhookSecretConfigured: false,
        },
      ],
      externalSyncEvents: [
        {
          ...externalSyncEvents[0],
          failureReason: '',
          payloadDigest: '',
          signatureStatus:
            ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED,
          status: ExternalSyncEventStatus.EXTERNAL_SYNC_EVENT_STATUS_RECEIVED,
        },
      ],
      inboundSources: [
        {
          ...inboundSources[0],
          lastError: 'signature mismatch',
        },
      ],
      replySendHook: {
        ...replySendHook,
        urlFingerprint: '',
      },
      replySendHookHealth: {
        ...replySendHookHealth,
        dead: '0',
        failed: '0',
        latestProblem: undefined,
        retryable: '0',
      },
      requestNotificationDeliveries: [
        {
          attempts: 8,
          channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK,
          createdAt: '2026-07-16T00:00:00Z',
          deadReason: 'signature expired',
          destinationHash: '',
          eventId: 'rn-event-dead',
          failureKind: 'signature_mismatch',
          id: 'rn-delivery-dead',
          lastError: '',
          manualRetryCount: 0,
          retriedBy: '',
          status: 'dead',
          traceId: '',
        },
      ],
      requestNotificationSettings,
      requestNotificationWebhookTargets: [
        {
          ...requestNotificationWebhookTargets[0],
          signatureVersion: '',
        },
      ],
    })

    expect(tooling.summary).toBe('5 webhook signature checks are blocked')
    expect(tooling.lanes.find((lane) => lane.key === 'inbound_webhook_signature')).toMatchObject({
      status: 'blocked',
    })
    expect(tooling.lanes.find((lane) => lane.key === 'reply_hook_fingerprint')).toMatchObject({
      status: 'blocked',
    })
    expect(tooling.lanes.find((lane) => lane.key === 'request_notification_webhook')).toMatchObject(
      { status: 'blocked' },
    )
    expect(tooling.lanes.find((lane) => lane.key === 'external_sync_signature')).toMatchObject({
      status: 'blocked',
    })
    expect(tooling.lanes.find((lane) => lane.key === 'failure_diagnostics')).toMatchObject({
      signal: '2 signature-path failures / 1 diagnostics / 1 replay paths',
      status: 'blocked',
    })
  })

  it('watches untested but signed paths without blocking rollout', () => {
    const tooling = buildWebhookSignatureTooling({
      externalSyncConnections: [externalSyncConnections[0]],
      externalSyncEvents: [],
      inboundSources: [
        {
          ...inboundSources[0],
          lastEventAt: '',
        },
      ],
      replySendHook,
      replySendHookHealth: {
        ...replySendHookHealth,
        accepted: '0',
        dead: '0',
        failed: '0',
        latestDelivery: undefined,
        latestProblem: undefined,
        retryable: '0',
        total: '0',
      },
      requestNotificationDeliveries: [],
      requestNotificationSettings,
      requestNotificationWebhookTargets: [
        {
          ...requestNotificationWebhookTargets[0],
          includeRecipientIdentity: false,
          lastTestedAt: undefined,
          verifiedAt: undefined,
        },
      ],
    })

    expect(tooling.summary).toBe('4 webhook signature checks need attention')
    expect(tooling.lanes.find((lane) => lane.key === 'inbound_webhook_signature')).toMatchObject({
      status: 'watch',
    })
    expect(tooling.lanes.find((lane) => lane.key === 'reply_hook_fingerprint')).toMatchObject({
      signal: 'reply hook on / fingerprint on / 0 deliveries / 0 failing',
      status: 'watch',
    })
    expect(tooling.lanes.find((lane) => lane.key === 'request_notification_webhook')).toMatchObject(
      {
        evidence: 'webhook on / 1 active / identity redacted / 0 failing deliveries',
        status: 'watch',
      },
    )
    expect(tooling.lanes.find((lane) => lane.key === 'external_sync_signature')).toMatchObject({
      status: 'watch',
    })
  })

  it('keeps diagnosable webhook delivery failures in watch instead of blocked', () => {
    const tooling = buildWebhookSignatureTooling({
      externalSyncConnections: [externalSyncConnections[0]],
      externalSyncEvents: [
        {
          ...externalSyncEvents[0],
          failureReason: 'mapping was disabled when the webhook arrived',
          payloadDigest: 'sha256:eventdigest',
          signatureStatus:
            ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED,
          status: ExternalSyncEventStatus.EXTERNAL_SYNC_EVENT_STATUS_FAILED,
        },
      ],
      inboundSources,
      replySendHook,
      replySendHookHealth: {
        ...replySendHookHealth,
        latestProblem: {
          ...replySendHookHealth.latestProblem,
          error: '',
          hookFingerprint: 'sha256:reply',
          retryable: false,
        },
      },
      requestNotificationDeliveries: [
        {
          attempts: 1,
          channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK,
          createdAt: '2026-07-16T00:00:00Z',
          deadReason: '',
          destinationHash: 'sha256:webhook',
          eventId: 'rn-event-webhook',
          failureKind: 'provider_5xx',
          id: 'rn-delivery-webhook',
          lastError: '',
          manualRetryCount: 0,
          retriedBy: '',
          status: 'failed',
          traceId: 'trace-webhook',
        },
      ],
      requestNotificationSettings,
      requestNotificationWebhookTargets: [
        {
          ...requestNotificationWebhookTargets[0],
          lastTestedAt: '2026-07-16T00:05:00Z',
        },
      ],
    })

    expect(tooling.summary).toBe('4 webhook signature checks need attention')
    expect(tooling.lanes.find((lane) => lane.key === 'request_notification_webhook')).toMatchObject(
      {
        signal: '1 request webhooks / 1 signed / 1 tested / 1 webhook failures',
        status: 'watch',
      },
    )
    expect(tooling.lanes.find((lane) => lane.key === 'failure_diagnostics')).toMatchObject({
      signal: '3 signature-path failures / 3 diagnostics / 2 replay paths',
      status: 'watch',
    })
  })

  it('treats malformed delivery counters as zero when assessing reply hooks', () => {
    const tooling = buildWebhookSignatureTooling({
      externalSyncConnections: [externalSyncConnections[0]],
      externalSyncEvents: [
        {
          ...externalSyncEvents[0],
          failureReason: '',
          signatureStatus:
            ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED,
          status: ExternalSyncEventStatus.EXTERNAL_SYNC_EVENT_STATUS_RECEIVED,
        },
      ],
      inboundSources,
      replySendHook,
      replySendHookHealth: {
        ...replySendHookHealth,
        accepted: 'NaN',
        dead: 'NaN',
        failed: 'NaN',
        latestDelivery: undefined,
        latestProblem: undefined,
        retryable: 'NaN',
        total: 'NaN',
      },
      requestNotificationDeliveries: [],
      requestNotificationSettings,
      requestNotificationWebhookTargets: [
        {
          ...requestNotificationWebhookTargets[0],
          verifiedAt: '2026-07-16T00:06:00Z',
        },
      ],
    })

    expect(tooling.summary).toBe('1 webhook signature checks need attention')
    expect(tooling.lanes.find((lane) => lane.key === 'reply_hook_fingerprint')).toMatchObject({
      signal: 'reply hook on / fingerprint on / 0 deliveries / 0 failing',
      status: 'watch',
    })
  })
})
