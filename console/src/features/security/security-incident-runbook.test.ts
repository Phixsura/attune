import { describe, expect, it } from 'vitest'
import type { ApiKey } from '@/proto/attune/v1/api_key'
import type { AuditLogEntry } from '@/proto/attune/v1/audit'
import type { ExternalConnection, ExternalSyncEvent } from '@/proto/attune/v1/external_sync'
import {
  ExternalSyncEventSignatureStatus,
  ExternalSyncEventStatus,
} from '@/proto/attune/v1/external_sync'
import type { GdprOperationsResponse } from '@/proto/attune/v1/gdpr'
import type { InboundSource } from '@/proto/attune/v1/inbound_source'
import type { ReplySendHook, ReplySendHookHealth } from '@/proto/attune/v1/ingest'
import type { LLMChannel } from '@/proto/attune/v1/llm_config'
import type { Member } from '@/proto/attune/v1/member'
import type { NotifyTarget } from '@/proto/attune/v1/notify_target'
import {
  ModerationState,
  type ModerationSubject,
  PublicAccessMode,
  PublicIdentityMode,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
} from '@/proto/attune/v1/public_visibility'
import {
  RequestNotificationChannel,
  type RequestNotificationDelivery,
  type RequestNotificationSettings,
  type RequestNotificationWebhookTarget,
} from '@/proto/attune/v1/request_notification'
import type { PreflightCheckResult } from '@/proto/attune/v1/system'
import type { AuthModeResponse } from './api/auth-mode'
import type { BreakGlassToken } from './api/breakglass'
import {
  buildSecurityIncidentRunbook,
  type SecurityIncidentRunbookInput,
} from './security-incident-runbook'

const apiKeys: ApiKey[] = [
  {
    allowedCidrs: ['203.0.113.0/24'],
    createdAt: '2026-01-01T00:00:00Z',
    environment: 'production',
    expiresAt: '2026-08-15T00:00:00Z',
    id: 'key-new',
    isActive: true,
    keyPrefix: 'att_live_new',
    label: 'production ingest',
    lastUsedAt: '2026-07-01T00:00:00Z',
    rateLimitRpm: 120,
    scopes: ['ingest:write'],
    usageCount: '42',
  },
  {
    allowedCidrs: [],
    createdAt: '2025-01-01T00:00:00Z',
    environment: 'production',
    gracePeriodEndsAt: '2026-07-02T00:00:00Z',
    id: 'key-old',
    isActive: true,
    keyPrefix: 'att_live_old',
    label: 'legacy ingest',
    lastUsedAt: '2026-06-20T00:00:00Z',
    scopes: ['feedback:read'],
    usageCount: '5',
  },
]

const llmChannels: LLMChannel[] = [
  {
    authMode: 'bearer',
    baseUrl: 'https://api.openai.com/v1',
    createdAt: '2026-06-01T00:00:00Z',
    credentialKeyId: 'tink-key-1',
    hasApiKey: true,
    id: 'llm-1',
    lastError: '',
    lastTestStatus: 'pass',
    lastTestedAt: '2026-07-01T00:00:00Z',
    name: 'OpenAI production',
    priority: 1,
    protocol: 'openai-responses',
    status: 'enabled',
    timeoutSeconds: 30,
    updatedAt: '2026-07-01T00:00:00Z',
    weight: 1,
  },
]

const preflightChecks: PreflightCheckResult[] = [
  {
    category: 'encryption',
    message: 'Tink keyset parsed and primary key available',
    name: 'secrets:tink_keyset',
    remediation: '',
    status: 'pass',
  },
  {
    category: 'backup',
    message: 'Managed secret samples decrypted',
    name: 'secrets:decryptability',
    remediation: '',
    status: 'pass',
  },
]

const tokens: BreakGlassToken[] = [
  {
    admin_email: 'admin@example.com',
    allowed_ips: ['203.0.113.0/24'],
    expires_at: '2099-07-06T12:30:00Z',
    id: 'token-1',
    issued_at: '2026-07-05T11:30:00Z',
    issued_by: 'issuer-1',
    status: 'valid',
  },
]

const authMode: AuthModeResponse = { mode: 'hybrid' }

const members: Member[] = [
  {
    acceptedAt: '1',
    email: 'admin-1@example.com',
    id: 'member-admin-1',
    invitedAt: '0',
    memberType: 'oidc_user',
    role: 'admin',
    roleSource: 'idp',
    userId: 'admin-1',
  },
  {
    acceptedAt: '1',
    email: 'admin-2@example.com',
    id: 'member-admin-2',
    invitedAt: '0',
    memberType: 'oidc_user',
    role: 'admin',
    roleSource: 'idp',
    userId: 'admin-2',
  },
  {
    acceptedAt: '1',
    email: 'viewer@example.com',
    id: 'member-viewer-1',
    invitedAt: '0',
    memberType: 'oidc_user',
    role: 'viewer',
    roleSource: 'idp',
    userId: 'viewer-1',
  },
]

const auditEntries: AuditLogEntry[] = [
  {
    action: 'member.update_role',
    actorEmail: 'admin-1@example.com',
    actorId: 'admin-1',
    actorIp: '127.0.0.1',
    actorType: 'admin',
    actorUserAgent: 'vitest',
    afterJson: '{}',
    beforeJson: '{}',
    createdAt: '2026-07-01T00:00:00Z',
    id: 'audit-member-role',
    summary: 'Updated member role',
    targetId: 'member-viewer-1',
    targetType: 'member',
  },
]

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

const replySendHookHealth: ReplySendHookHealth = {
  accepted: '1',
  dead: '0',
  failed: '0',
  pending: '0',
  retryable: '0',
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
    id: 'rn-target-1',
    includeRecipientIdentity: true,
    name: 'Customer CRM',
    signatureVersion: 'v1',
    status: 'active',
    updatedAt: '2026-07-16T00:00:00Z',
    url: 'https://hooks.example.test/request-notifications',
    urlHost: 'hooks.example.test',
    verifiedAt: '2026-07-16T00:10:00Z',
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

const publicVisibilityPolicy: PublicVisibilityPolicy = {
  changelogEnabled: true,
  commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
  commentsEnabled: true,
  createdAt: '2026-07-01T00:00:00Z',
  defaultCommentState: ModerationState.MODERATION_STATE_PENDING,
  defaultRequestState: ModerationState.MODERATION_STATE_PENDING,
  hidePublicTimestamps: false,
  portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC,
  requestsEnabled: true,
  roadmapEnabled: true,
  roadmapStatusMapping: [],
  searchIndexingEnabled: false,
  showCommentCount: true,
  showSubmitterDisplay: true,
  showVoteCount: true,
  submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
  submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME,
  tenantId: 'tenant-1',
  updatedAt: '2026-07-01T00:05:00Z',
  updatedBy: 'admin-1',
  voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
}

const moderationSubjects: ModerationSubject[] = [
  {
    createdAt: '2026-07-01T00:01:00Z',
    id: 'moderation-pending',
    reasonCode: '',
    reasonNote: '',
    reviewedBy: '',
    state: ModerationState.MODERATION_STATE_PENDING,
    subjectId: 'request-public',
    submittedByDisplay: 'Ada Lovelace',
    surface: PublicSurface.PUBLIC_SURFACE_REQUEST,
    tenantId: 'tenant-1',
    updatedAt: '2026-07-01T00:01:00Z',
  },
]

const gdprOperations: GdprOperationsResponse = {
  activeRequestCount: 1,
  auditPruneIntervalSeconds: 3_600,
  auditRetentionDays: 30,
  backupsMayRetainUntilRotation: true,
  deleteGraceWindowSeconds: 3_600,
  exportTtlSeconds: 86_400,
  hashedAuditResidue: true,
  legalHoldSupported: false,
  nextExportExpiryAt: '2026-06-25T09:00:00Z',
  queuedRequestCount: 1,
  readyExportCount: 1,
  scheduledDeleteCount: 1,
  stepUp: {
    method: 'password',
    passwordAllowed: true,
    satisfied: false,
    ttlSeconds: 900,
  },
}

const notifyTargets: NotifyTarget[] = [
  {
    audience: 'all',
    createdAt: '2026-07-01T00:00:00Z',
    destinationType: 'raw-webhook',
    disabled: false,
    id: 'notify-1',
    lastError: '',
    timeoutSeconds: 10,
    url: 'https://example.com/hook',
  },
]

function baseInput(overrides: Partial<SecurityIncidentRunbookInput> = {}) {
  return {
    apiKeys,
    auditEntries,
    authMode,
    externalSyncConnections,
    externalSyncEvents,
    gdprOperations,
    inboundSources,
    llmChannels,
    lockouts: [],
    members,
    moderationSubjects,
    notifyTargets,
    preflightChecks,
    publicVisibilityPolicy,
    replySendHook,
    replySendHookHealth,
    requestNotificationDeliveries,
    requestNotificationSettings,
    requestNotificationWebhookTargets,
    tokens,
    ...overrides,
  }
}

function readyInput(overrides: Partial<SecurityIncidentRunbookInput> = {}) {
  return baseInput({
    apiKeys: [apiKeys[0]],
    authMode: { mode: 'sso_only' },
    externalSyncEvents: [
      {
        ...externalSyncEvents[0],
        failureReason: '',
        signatureStatus:
          ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED,
        status: ExternalSyncEventStatus.EXTERNAL_SYNC_EVENT_STATUS_RECEIVED,
      },
    ],
    gdprOperations: {
      ...gdprOperations,
      backupsMayRetainUntilRotation: false,
      legalHoldSupported: true,
      queuedRequestCount: 0,
      readyExportCount: 0,
      scheduledDeleteCount: 0,
    },
    moderationSubjects: [],
    publicVisibilityPolicy: {
      ...publicVisibilityPolicy,
      changelogEnabled: false,
      commentsEnabled: false,
      hidePublicTimestamps: true,
      requestsEnabled: false,
      roadmapEnabled: false,
      showCommentCount: false,
      showSubmitterDisplay: false,
      showVoteCount: false,
    },
    requestNotificationDeliveries: [],
    ...overrides,
  })
}

describe('buildSecurityIncidentRunbook', () => {
  it('joins Credential compromise, webhook, access, privacy, and notification incident evidence', () => {
    const runbook = buildSecurityIncidentRunbook(baseInput())

    expect(runbook.fingerprint).toBe(
      '2 API keys / 0 signature failures / 2 admins / 4 public surfaces / 1 notification failures',
    )
    expect(runbook.summary).toBe('5 security incident lanes need rehearsal')
    expect(runbook.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 0, watch: 5 })
    expect(runbook.lanes.find((lane) => lane.key === 'credential_compromise')).toMatchObject({
      signal: '2 API keys / 1 managed LLM keys / 2 keyset checks / 1 break-glass',
      status: 'watch',
    })
    expect(runbook.lanes.find((lane) => lane.key === 'webhook_signature_incident')).toMatchObject({
      signal:
        '0 signature failures / 0 reply failures / 0 request webhook failures / 1 external failures',
      status: 'watch',
    })
    expect(runbook.lanes.find((lane) => lane.key === 'access_identity_incident')).toMatchObject({
      signal: 'hybrid / 2 admins / 3 IdP-managed / 1 member audit events',
      status: 'watch',
    })
    expect(runbook.lanes.find((lane) => lane.key === 'public_privacy_incident')).toMatchObject({
      signal: '4 public surfaces / 1 pending / legal hold off / 1 scheduled deletes',
      status: 'watch',
    })
    expect(
      runbook.lanes.find((lane) => lane.key === 'customer_notification_recovery'),
    ).toMatchObject({
      signal: '1 outbound targets / 1 request failures / 0 reply failures / 0 target failures',
      status: 'watch',
    })
  })

  it('blocks runbook lanes for unrecoverable incident gaps', () => {
    const runbook = buildSecurityIncidentRunbook(
      baseInput({
        authMode: { mode: 'sso_only' },
        externalSyncEvents: [
          {
            ...externalSyncEvents[0],
            failureReason: 'signature mismatch',
            signatureStatus:
              ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_FAILED,
          },
        ],
        notifyTargets: [],
        preflightChecks: [{ ...preflightChecks[0], status: 'fail' }],
        publicVisibilityPolicy: {
          ...publicVisibilityPolicy,
          defaultRequestState: ModerationState.MODERATION_STATE_APPROVED,
        },
        requestNotificationSettings: {
          ...requestNotificationSettings,
          emailEnabled: false,
          webhookEnabled: false,
        },
        requestNotificationWebhookTargets: [
          {
            ...requestNotificationWebhookTargets[0],
            signatureVersion: '',
          },
        ],
        tokens: [],
      }),
    )

    expect(runbook.summary).toBe('5 security incident runbook lanes are blocked')
    expect(runbook.lanes.map((lane) => lane.status)).toEqual([
      'blocked',
      'blocked',
      'blocked',
      'blocked',
      'blocked',
    ])
  })

  it('keeps every lane at needs-data when incident evidence is absent', () => {
    const runbook = buildSecurityIncidentRunbook({})

    expect(runbook.summary).toBe('5 security incident lanes need evidence')
    expect(runbook.totals).toMatchObject({ blocked: 0, needs_data: 5, ready: 0, watch: 0 })
    expect(runbook.lanes.every((lane) => lane.status === 'needs_data')).toBe(true)
  })

  it('marks the incident runbook ready when every rehearsal artifact is present', () => {
    const runbook = buildSecurityIncidentRunbook(readyInput())

    expect(runbook.fingerprint).toBe(
      '1 API keys / 0 signature failures / 2 admins / 0 public surfaces / 0 notification failures',
    )
    expect(runbook.summary).toBe('security incident runbook evidence is ready')
    expect(runbook.totals).toMatchObject({ blocked: 0, needs_data: 0, ready: 5, watch: 0 })
    expect(runbook.lanes.map((lane) => lane.status)).toEqual([
      'ready',
      'ready',
      'ready',
      'ready',
      'ready',
    ])
  })

  it('blocks credential response when active API keys have no scopes', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        apiKeys: [
          {
            ...apiKeys[0],
            scopes: [],
          },
        ],
      }),
    )

    expect(runbook.summary).toBe('1 security incident runbook lanes are blocked')
    expect(runbook.lanes.find((lane) => lane.key === 'credential_compromise')).toMatchObject({
      status: 'blocked',
    })
  })

  it('blocks credential response when enabled LLM channels need missing keys', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        llmChannels: [
          {
            ...llmChannels[0],
            credentialKeyId: '',
            hasApiKey: false,
          },
        ],
      }),
    )

    expect(runbook.summary).toBe('1 security incident runbook lanes are blocked')
    expect(runbook.lanes.find((lane) => lane.key === 'credential_compromise')).toMatchObject({
      status: 'blocked',
    })
  })

  it('watches credential response when secret checks warn or LLM tests are stale', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        llmChannels: [
          {
            ...llmChannels[0],
            lastTestedAt: '',
          },
        ],
        preflightChecks: [{ ...preflightChecks[0], status: 'warn' }],
      }),
    )

    expect(runbook.summary).toBe('1 security incident lanes need rehearsal')
    expect(runbook.lanes.find((lane) => lane.key === 'credential_compromise')).toMatchObject({
      status: 'watch',
    })
  })

  it('blocks webhook incidents for inbound webhook failures', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        inboundSources: [
          {
            ...inboundSources[0],
            lastError: 'signature mismatch',
          },
        ],
      }),
    )

    expect(runbook.summary).toBe('1 security incident runbook lanes are blocked')
    expect(runbook.lanes.find((lane) => lane.key === 'webhook_signature_incident')).toMatchObject({
      signal:
        '0 signature failures / 0 reply failures / 0 request webhook failures / 0 external failures',
      status: 'blocked',
    })
  })

  it('blocks webhook incidents when an enabled external connection lacks a secret', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        externalSyncConnections: [
          {
            ...externalSyncConnections[0],
            webhookSecretConfigured: false,
          },
        ],
      }),
    )

    expect(runbook.summary).toBe('1 security incident runbook lanes are blocked')
    expect(runbook.lanes.find((lane) => lane.key === 'webhook_signature_incident')).toMatchObject({
      status: 'blocked',
    })
  })

  it('watches webhook incidents for retryable hooks and untested request targets', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        replySendHookHealth: {
          ...replySendHookHealth,
          retryable: '1',
        },
        requestNotificationWebhookTargets: [
          {
            ...requestNotificationWebhookTargets[0],
            verifiedAt: undefined,
          },
        ],
        requestNotificationDeliveries: [
          {
            ...requestNotificationDeliveries[0],
            channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK,
            destinationHash: 'sha256:rn-webhook',
            id: 'rn-delivery-webhook-1',
          },
          {
            ...requestNotificationDeliveries[0],
            channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK,
            deadReason: 'max attempts exceeded',
            id: 'rn-delivery-webhook-dead',
            lastError: '',
            status: 'dead',
          },
          {
            ...requestNotificationDeliveries[0],
            channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK,
            id: 'rn-delivery-webhook-last-error',
            status: 'accepted',
          },
          {
            ...requestNotificationDeliveries[0],
            channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_WEBHOOK,
            deadReason: 'operator stopped retries',
            id: 'rn-delivery-webhook-dead-reason',
            lastError: '',
            status: 'accepted',
          },
          {
            ...requestNotificationDeliveries[0],
            channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL,
            deadReason: '',
            id: 'rn-delivery-email-filtered',
            lastError: '',
            status: 'failed',
          },
        ],
      }),
    )

    expect(runbook.summary).toBe('2 security incident lanes need rehearsal')
    expect(runbook.lanes.find((lane) => lane.key === 'webhook_signature_incident')).toMatchObject({
      status: 'watch',
    })
    expect(
      runbook.lanes.find((lane) => lane.key === 'customer_notification_recovery'),
    ).toMatchObject({
      status: 'watch',
    })
  })

  it('blocks access incidents when no active admin remains', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        members: [
          {
            ...members[2],
            role: 'viewer',
          },
        ],
      }),
    )

    expect(runbook.summary).toBe('1 security incident runbook lanes are blocked')
    expect(runbook.lanes.find((lane) => lane.key === 'access_identity_incident')).toMatchObject({
      signal: 'sso_only / 0 admins / 1 IdP-managed / 1 member audit events',
      status: 'blocked',
    })
  })

  it('watches access incidents when IdP ownership or audit evidence is thin', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        auditEntries: [],
        members: [
          members[0],
          members[1],
          {
            ...members[2],
            roleSource: 'local',
          },
        ],
      }),
    )

    expect(runbook.summary).toBe('1 security incident lanes need rehearsal')
    expect(runbook.lanes.find((lane) => lane.key === 'access_identity_incident')).toMatchObject({
      signal: 'sso_only / 2 admins / 2 IdP-managed / 0 member audit events',
      status: 'watch',
    })
  })

  it('blocks public privacy incidents for terminal moderation subjects', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        moderationSubjects: [
          {
            ...moderationSubjects[0],
            state: ModerationState.MODERATION_STATE_REJECTED,
          },
        ],
      }),
    )

    expect(runbook.summary).toBe('1 security incident runbook lanes are blocked')
    expect(runbook.lanes.find((lane) => lane.key === 'public_privacy_incident')).toMatchObject({
      signal: '0 public surfaces / 0 pending / legal hold on / 0 scheduled deletes',
      status: 'blocked',
    })
  })

  it('watches private privacy incidents when legal hold support is absent', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        gdprOperations: {
          ...gdprOperations,
          backupsMayRetainUntilRotation: false,
          legalHoldSupported: false,
          readyExportCount: 0,
          scheduledDeleteCount: 0,
        },
      }),
    )

    expect(runbook.summary).toBe('1 security incident lanes need rehearsal')
    expect(runbook.lanes.find((lane) => lane.key === 'public_privacy_incident')).toMatchObject({
      signal: '0 public surfaces / 0 pending / legal hold off / 0 scheduled deletes',
      status: 'watch',
    })
  })

  it('blocks notification recovery when outbound targets are not HTTPS', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        notifyTargets: [
          {
            ...notifyTargets[0],
            url: 'http://example.com/hook',
          },
        ],
      }),
    )

    expect(runbook.summary).toBe('1 security incident runbook lanes are blocked')
    expect(
      runbook.lanes.find((lane) => lane.key === 'customer_notification_recovery'),
    ).toMatchObject({
      signal: '1 outbound targets / 0 request failures / 0 reply failures / 0 target failures',
      status: 'blocked',
    })
  })

  it('watches notification recovery when outbound targets are failing', () => {
    const runbook = buildSecurityIncidentRunbook(
      readyInput({
        notifyTargets: [
          {
            ...notifyTargets[0],
            lastFailureAt: '2026-07-20T00:00:00Z',
          },
        ],
      }),
    )

    expect(runbook.summary).toBe('1 security incident lanes need rehearsal')
    expect(
      runbook.lanes.find((lane) => lane.key === 'customer_notification_recovery'),
    ).toMatchObject({
      signal: '1 outbound targets / 0 request failures / 0 reply failures / 1 target failures',
      status: 'watch',
    })
  })
})
