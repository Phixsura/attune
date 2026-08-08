// SPDX-License-Identifier: Apache-2.0

import { HttpResponse, http } from 'msw'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { BreakGlassLockout, BreakGlassToken } from '@/features/security/api/breakglass'
import { SecurityPage } from '@/features/security/components/security-page'
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
import type { PreflightReportResponse } from '@/proto/attune/v1/system'
import { server } from '@/testing/mocks/server'
import { renderWithProviders, screen, waitFor, within } from '@/testing/test-utils'

const toastError = vi.fn()
const toastSuccess = vi.fn()
vi.mock('sonner', () => ({
  toast: {
    error: (...args: unknown[]) => toastError(...args),
    success: (...args: unknown[]) => toastSuccess(...args),
  },
}))

const activeToken: BreakGlassToken = {
  id: 'token-1',
  admin_email: 'admin@example.com',
  expires_at: '2099-07-06T12:30:00Z',
  issued_by: 'issuer-1',
  issued_at: '2026-07-05T11:30:00Z',
  status: 'valid',
  allowed_ips: ['203.0.113.0/24'],
}

const sampleLockout: BreakGlassLockout = {
  ip: '203.0.113.10',
  locked_until: '2026-07-05T15:00:00Z',
  remaining_mins: 11,
  attempts: 5,
}

const governanceMembers: Member[] = [
  {
    id: 'member-admin-1',
    memberType: 'oidc_user',
    userId: 'admin-1',
    email: 'admin-1@example.com',
    role: 'admin',
    roleSource: 'idp',
    invitedAt: '0',
    acceptedAt: '1',
  },
  {
    id: 'member-admin-2',
    memberType: 'oidc_user',
    userId: 'admin-2',
    email: 'admin-2@example.com',
    role: 'admin',
    roleSource: 'idp',
    invitedAt: '0',
    acceptedAt: '1',
  },
  {
    id: 'member-viewer-1',
    memberType: 'oidc_user',
    userId: 'viewer-1',
    email: 'viewer@example.com',
    role: 'viewer',
    roleSource: 'idp',
    invitedAt: '0',
    acceptedAt: '1',
  },
  {
    id: 'member-member-1',
    memberType: 'oidc_user',
    userId: 'member-1',
    email: 'member@example.com',
    role: 'member',
    roleSource: 'idp',
    invitedAt: '0',
    acceptedAt: '1',
  },
]

const governanceAuditEntries: AuditLogEntry[] = [
  {
    id: 'audit-member-role',
    actorType: 'admin',
    actorId: 'admin-1',
    actorEmail: 'admin-1@example.com',
    actorIp: '127.0.0.1',
    actorUserAgent: 'vitest',
    action: 'member.update_role',
    targetType: 'member',
    targetId: 'member-viewer-1',
    summary: 'Updated member role',
    beforeJson: '{}',
    afterJson: '{}',
    createdAt: '2026-07-01T00:00:00Z',
  },
]

const fieldPermissionsAuditEntries: AuditLogEntry[] = [
  {
    id: 'audit-public-moderation',
    actorType: 'admin',
    actorId: 'admin-1',
    actorEmail: 'admin-1@example.com',
    actorIp: '127.0.0.1',
    actorUserAgent: 'vitest',
    action: 'moderation.hide',
    targetType: 'public_moderation_subject',
    targetId: 'moderation-sensitive',
    summary: 'Hid public moderation subject',
    beforeJson: '{}',
    afterJson: '{}',
    createdAt: '2026-07-01T00:10:00Z',
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
  portalSubmissionForm: undefined,
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
    id: 'moderation-pending',
    tenantId: 'tenant-1',
    surface: PublicSurface.PUBLIC_SURFACE_REQUEST,
    subjectId: 'request-public',
    state: ModerationState.MODERATION_STATE_PENDING,
    reasonCode: '',
    reasonNote: '',
    submittedByDisplay: 'Ada Lovelace',
    reviewedBy: '',
    createdAt: '2026-07-01T00:01:00Z',
    updatedAt: '2026-07-01T00:01:00Z',
  },
  {
    id: 'moderation-approved',
    tenantId: 'tenant-1',
    surface: PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
    subjectId: 'comment-public',
    state: ModerationState.MODERATION_STATE_APPROVED,
    reasonCode: 'safe',
    reasonNote: '',
    submittedByDisplay: 'Grace Hopper',
    reviewedBy: 'admin-1',
    reviewedAt: '2026-07-01T00:04:00Z',
    createdAt: '2026-07-01T00:02:00Z',
    updatedAt: '2026-07-01T00:04:00Z',
  },
]

const complianceGdprOperations: GdprOperationsResponse = {
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

const complianceNotifyTargets: NotifyTarget[] = [
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

const rotationApiKeys: ApiKey[] = [
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

const rotationInboundSources: InboundSource[] = [
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

const rotationLlmChannels: LLMChannel[] = [
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

const rotationReplySendHook: ReplySendHook = {
  createdAt: '2026-06-01T00:00:00Z',
  enabled: true,
  id: 'hook-1',
  name: 'Reply hook',
  updatedAt: '2026-07-01T00:00:00Z',
  urlFingerprint: 'sha256:reply',
  urlHost: 'hooks.example.com',
}

const rotationReplySendHookHealth: ReplySendHookHealth = {
  accepted: '1',
  dead: '0',
  failed: '0',
  pending: '0',
  retryable: '0',
  total: '1',
}

const rotationPreflight: PreflightReportResponse = {
  checks: [
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
  ],
  elapsed: '12ms',
  status: 'pass',
}

const signatureRequestNotificationSettings: RequestNotificationSettings = {
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

const signatureRequestNotificationWebhookTargets: RequestNotificationWebhookTarget[] = [
  {
    createdAt: '2026-07-16T00:00:00Z',
    eventMask: { 'request.shipped': true },
    id: 'rn-target-security',
    includeRecipientIdentity: true,
    name: 'Customer CRM',
    signatureVersion: 'v1',
    status: 'active',
    updatedAt: '2026-07-16T00:00:00Z',
    url: 'https://hooks.example.test/request-notifications',
    urlHost: 'hooks.example.test',
  },
]

const signatureRequestNotificationDeliveries: RequestNotificationDelivery[] = [
  {
    attempts: 2,
    channel: RequestNotificationChannel.REQUEST_NOTIFICATION_CHANNEL_EMAIL,
    createdAt: '2026-07-16T00:00:00Z',
    deadReason: '',
    destinationHash: 'sha256:rn-email',
    eventId: 'rn-event-security',
    failureKind: 'provider_5xx',
    id: 'rn-delivery-security',
    lastError: 'temporary provider outage',
    manualRetryCount: 0,
    retriedBy: '',
    status: 'failed',
    traceId: 'trace-rn-security',
  },
]

const signatureExternalSyncConnections: ExternalConnection[] = [
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

const signatureExternalSyncEvents: ExternalSyncEvent[] = [
  {
    connectionId: 'external-sync-connection-active',
    createdAt: '2026-07-08T03:02:00Z',
    dedupeKey: 'github:evt-security',
    eventType: 'issues.edited',
    externalEventId: 'evt-security',
    failureReason: 'mapping was disabled when the webhook arrived',
    id: 'external-sync-event-security',
    mappingId: 'external-sync-mapping-security',
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

function makeToken(overrides: Partial<BreakGlassToken> = {}): BreakGlassToken {
  return {
    ...activeToken,
    ...overrides,
  }
}

function apiFailure(message: string, status = 500) {
  return HttpResponse.json({ code: 'FAILED', message }, { status })
}

function setupSecurityResponses({
  tokens = [activeToken],
  lockouts = [sampleLockout],
  members = governanceMembers,
  auditEntries = governanceAuditEntries,
  fieldAuditEntries = fieldPermissionsAuditEntries,
  mode = 'hybrid',
  moderation = moderationSubjects,
  policy = publicVisibilityPolicy,
  gdprOperations = complianceGdprOperations,
  notifyTargets = complianceNotifyTargets,
  apiKeys = rotationApiKeys,
  inboundSources = rotationInboundSources,
  llmChannels = rotationLlmChannels,
  replySendHook = rotationReplySendHook,
  replySendHookHealth = rotationReplySendHookHealth,
  preflight = rotationPreflight,
  requestNotificationSettings = signatureRequestNotificationSettings,
  requestNotificationWebhookTargets = signatureRequestNotificationWebhookTargets,
  requestNotificationDeliveries = signatureRequestNotificationDeliveries,
  externalSyncConnections = signatureExternalSyncConnections,
  externalSyncEvents = signatureExternalSyncEvents,
}: {
  auditEntries?: AuditLogEntry[]
  apiKeys?: ApiKey[]
  externalSyncConnections?: ExternalConnection[]
  externalSyncEvents?: ExternalSyncEvent[]
  fieldAuditEntries?: AuditLogEntry[]
  gdprOperations?: GdprOperationsResponse
  inboundSources?: InboundSource[]
  llmChannels?: LLMChannel[]
  members?: Member[]
  tokens?: BreakGlassToken[]
  lockouts?: BreakGlassLockout[]
  mode?: 'hybrid' | 'sso_only'
  moderation?: ModerationSubject[]
  notifyTargets?: NotifyTarget[]
  preflight?: PreflightReportResponse
  policy?: PublicVisibilityPolicy
  replySendHook?: ReplySendHook | null
  replySendHookHealth?: ReplySendHookHealth
  requestNotificationDeliveries?: RequestNotificationDelivery[]
  requestNotificationSettings?: RequestNotificationSettings
  requestNotificationWebhookTargets?: RequestNotificationWebhookTarget[]
} = {}) {
  let currentTokens = [...tokens]
  let currentLockouts = [...lockouts]

  server.use(
    http.get('/fb/v1/console/auth/sso/mode', () => HttpResponse.json({ mode })),
    http.get('/fb/v1/console/members', () => HttpResponse.json({ members })),
    http.get('/fb/v1/console/audit-log', ({ request }) => {
      const url = new URL(request.url)
      if (url.searchParams.get('targetType') === 'public_moderation_subject') {
        return HttpResponse.json({ items: fieldAuditEntries })
      }
      return HttpResponse.json({ items: auditEntries })
    }),
    http.get('/fb/v1/console/public-visibility/policy', () => HttpResponse.json(policy)),
    http.get('/fb/v1/console/public-visibility/moderation', () =>
      HttpResponse.json({ subjects: moderation }),
    ),
    http.get('/fb/v1/console/gdpr/operations', () => HttpResponse.json(gdprOperations)),
    http.get('/fb/v1/console/notify-targets', () => HttpResponse.json({ items: notifyTargets })),
    http.get('/fb/v1/console/api-keys', () => HttpResponse.json({ items: apiKeys })),
    http.get('/fb/v1/console/inbound/sources', () => HttpResponse.json({ items: inboundSources })),
    http.get('/fb/v1/console/llm/channels', () => HttpResponse.json({ items: llmChannels })),
    http.get('/fb/v1/console/reply-send-hook', () =>
      replySendHook
        ? HttpResponse.json(replySendHook)
        : HttpResponse.json(
            { code: 'NOT_FOUND', message: 'reply hook not configured' },
            { status: 404 },
          ),
    ),
    http.get('/fb/v1/console/reply-send-hook/health', () => HttpResponse.json(replySendHookHealth)),
    http.get('/fb/v1/console/system/preflight', () => HttpResponse.json(preflight)),
    http.get('/fb/v1/console/request-notifications/settings', () =>
      HttpResponse.json(requestNotificationSettings),
    ),
    http.get('/fb/v1/console/request-notifications/webhook-targets', () =>
      HttpResponse.json({ targets: requestNotificationWebhookTargets }),
    ),
    http.get('/fb/v1/console/request-notifications/deliveries', () =>
      HttpResponse.json({ deliveries: requestNotificationDeliveries }),
    ),
    http.get('/fb/v1/console/external-sync/connections', () =>
      HttpResponse.json({ connections: externalSyncConnections }),
    ),
    http.get('/fb/v1/console/external-sync/events', () =>
      HttpResponse.json({ events: externalSyncEvents, nextBeforeId: '' }),
    ),
    http.post('/fb/v1/console/auth/breakglass/issue', async ({ request }) => {
      const body = (await request.json()) as {
        admin_email: string
        ttl_minutes: number
        allowed_ips?: string[]
      }
      const issuedToken: BreakGlassToken = {
        id: `token-${currentTokens.length + 1}`,
        admin_email: body.admin_email,
        expires_at: '2099-07-06T12:30:00Z',
        issued_by: 'issuer-1',
        issued_at: '2026-07-05T11:30:00Z',
        status: 'valid',
        allowed_ips: body.allowed_ips ?? [],
      }
      currentTokens = [...currentTokens, issuedToken]
      return HttpResponse.json({
        token: issuedToken,
        raw_token: 'bg-issued-token',
        expires_at: issuedToken.expires_at,
      })
    }),
    http.get('/fb/v1/console/auth/breakglass/tokens', () =>
      HttpResponse.json({ tokens: currentTokens }),
    ),
    http.get('/fb/v1/console/auth/breakglass/lockouts', () =>
      HttpResponse.json({ lockouts: currentLockouts }),
    ),
    http.post('/fb/v1/console/auth/breakglass/tokens/revoke-all', () => {
      const revoked = currentTokens.filter(
        (token) => token.status === 'valid' || token.status === 'expiring',
      ).length
      currentTokens = []
      return HttpResponse.json({ revoked })
    }),
    http.post('/fb/v1/console/auth/breakglass/tokens/:tokenId/revoke', ({ params }) => {
      currentTokens = currentTokens.map((token) =>
        token.id === String(params.tokenId)
          ? {
              ...token,
              revoked_at: '2026-07-05T12:05:00Z',
              revoked_by: 'issuer-1',
              status: 'revoked',
            }
          : token,
      )
      return new HttpResponse(null, { status: 204 })
    }),
    http.post('/fb/v1/console/auth/breakglass/lockouts/:ip/unlock', ({ params }) => {
      currentLockouts = currentLockouts.filter((row) => row.ip !== String(params.ip))
      return new HttpResponse(null, { status: 204 })
    }),
  )

  return {
    apiKeys,
    externalSyncConnections,
    externalSyncEvents,
    fieldPermissionsAuditEntries: fieldAuditEntries,
    gdprOperations,
    governanceAuditEntries: auditEntries,
    inboundSources,
    llmChannels,
    members,
    moderationSubjects: moderation,
    notifyTargets,
    preflightChecks: preflight?.checks,
    publicVisibilityPolicy: policy,
    replySendHook,
    replySendHookHealth,
    requestNotificationDeliveries,
    requestNotificationSettings,
    requestNotificationWebhookTargets,
  }
}

function renderSecurityPage(options?: Parameters<typeof setupSecurityResponses>[0]) {
  return renderWithProviders(<SecurityPage evidence={setupSecurityResponses(options)} />)
}

describe('SecurityPage', () => {
  beforeEach(() => {
    toastError.mockClear()
    toastSuccess.mockClear()
  })

  it('renders break-glass tokens and lockout monitoring', async () => {
    renderSecurityPage()

    await waitFor(
      () => {
        expect(screen.getByText('admin@example.com')).toBeInTheDocument()
      },
      { timeout: 10_000 },
    )

    expect(screen.getByText('203.0.113.10')).toBeInTheDocument()
    expect(screen.getByText('5 次失败')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '撤销全部活跃令牌' })).toBeEnabled()
    expect(screen.getByText('Break-Glass 锁定')).toBeInTheDocument()
    expect(screen.getByText('治理 / RBAC 就绪度')).toBeInTheDocument()
    await waitFor(() => {
      expect(
        screen.getByText('hybrid / 4 active members / 2 admins / 1 member audit events'),
      ).toBeInTheDocument()
    })
    expect(screen.getByText('1 governance checks need attention')).toBeInTheDocument()
    expect(screen.getByText('hybrid / 1 active break-glass / 1 lockouts')).toBeInTheDocument()
    expect(
      screen.getByText('4 IdP-managed / 4 active members / 0 pending invites'),
    ).toBeInTheDocument()
    expect(screen.getByText('3 roles represented / 4 active members')).toBeInTheDocument()
    expect(screen.getByText('2 active admins')).toBeInTheDocument()
    expect(screen.getByText('1 member audit events / 1 actors')).toBeInTheDocument()
    expect(screen.getByText('字段级权限台账')).toBeInTheDocument()
    await waitFor(() => {
      expect(
        screen.getByText('4 roles / public / 2 moderation subjects / 1 moderation audit events'),
      ).toBeInTheDocument()
    })
    expect(screen.getByText('3 field-level permission checks need attention')).toBeInTheDocument()
    expect(screen.getByText('4 roles / 2 policy editors / viewer 3 grants')).toBeInTheDocument()
    expect(
      screen.getByText('public / search off / request pending / comment pending'),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        'submission identified / comments disabled / votes anonymous / identity display_name',
      ),
    ).toBeInTheDocument()
    expect(screen.getByText('1 pending / 1 approved / 0 blocked / 2 subjects')).toBeInTheDocument()
    expect(screen.getByText('1 moderation audit events / 1 actors')).toBeInTheDocument()
    expect(screen.getByText('合规包证据')).toBeInTheDocument()
    await waitFor(() => {
      expect(
        screen.getByText(
          'hybrid / 4 active members / 4 public surfaces / 1 outbound targets / 2 audit events',
        ),
      ).toBeInTheDocument()
    })
    expect(screen.getByText('1 compliance package checks are blocked')).toBeInTheDocument()
    expect(screen.getByText('hybrid / 2 admins / 3 roles / 1 break-glass')).toBeInTheDocument()
    expect(
      screen.getByText('4 public surfaces / identity display_name / moderation 2 subjects'),
    ).toBeInTheDocument()
    expect(screen.getByText('2 audit events / 1 actors / 2 action types')).toBeInTheDocument()
    expect(screen.getByText('audit 30d / legal hold off / 1 scheduled deletes')).toBeInTheDocument()
    expect(screen.getByText('1 enabled outbound / 0 failing / 1 HTTPS')).toBeInTheDocument()
    expect(screen.getByText('密钥轮换就绪度')).toBeInTheDocument()
    await waitFor(() => {
      expect(
        screen.getByText(
          '2 active API keys / 1 webhook sources / 1 managed LLM keys / 2 outbound targets / 2 keyset checks',
        ),
      ).toBeInTheDocument()
    })
    expect(screen.getByText('1 key rotation checks need attention')).toBeInTheDocument()
    expect(screen.getByText('2 keyset checks / 2 passing / 0 warning')).toBeInTheDocument()
    expect(
      screen.getByText('2 active keys / 1 expiring / 1 in grace / 1 never expires'),
    ).toBeInTheDocument()
    expect(screen.getByText('1 webhook sources / 1 enabled / 0 failing')).toBeInTheDocument()
    expect(
      screen.getByText('1 notify targets / reply hook on / 0 delivery failures'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('1 LLM channels / 1 managed keys / 1 tested / 0 failing'),
    ).toBeInTheDocument()
    expect(screen.getByText('Webhook signature 验证工具')).toBeInTheDocument()
    await waitFor(() => {
      expect(
        screen.getByText(
          '1 inbound webhooks / reply hook on / 1 request webhooks / 1 external sync secrets / 0 signature failures',
        ),
      ).toBeInTheDocument()
    })
    expect(screen.getByText('3 webhook signature checks need attention')).toBeInTheDocument()
    expect(screen.getByText('1 inbound webhooks / 1 enabled / 0 failing')).toBeInTheDocument()
    expect(
      screen.getByText('reply hook on / fingerprint on / 1 deliveries / 0 failing'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('1 request webhooks / 1 signed / 0 tested / 0 webhook failures'),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        '2 connections / 1 webhook secrets / 1 verified events / 0 signature failures',
      ),
    ).toBeInTheDocument()
    expect(
      screen.getByText('1 signature-path failures / 1 diagnostics / 1 replay paths'),
    ).toBeInTheDocument()
    expect(screen.getByText('安全事件响应 runbook')).toBeInTheDocument()
    await waitFor(() => {
      expect(
        screen.getByText(
          '2 API keys / 0 signature failures / 2 admins / 4 public surfaces / 1 notification failures',
        ),
      ).toBeInTheDocument()
    })
    expect(screen.getByText('5 security incident lanes need rehearsal')).toBeInTheDocument()
    expect(
      screen.getByText('2 API keys / 1 managed LLM keys / 2 keyset checks / 1 break-glass'),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        '0 signature failures / 0 reply failures / 0 request webhook failures / 1 external failures',
      ),
    ).toBeInTheDocument()
    expect(
      screen.getByText('hybrid / 2 admins / 4 IdP-managed / 1 member audit events'),
    ).toBeInTheDocument()
    expect(
      screen.getByText('4 public surfaces / 1 pending / legal hold off / 1 scheduled deletes'),
    ).toBeInTheDocument()
    expect(
      screen.getByText(
        '1 outbound targets / 1 request failures / 0 reply failures / 0 target failures',
      ),
    ).toBeInTheDocument()
  })

  it('surfaces governance continuity and access-review gaps', async () => {
    renderSecurityPage({
      auditEntries: [],
      lockouts: [],
      members: [
        {
          id: 'member-admin-1',
          memberType: 'oidc_user',
          userId: 'admin-1',
          email: 'admin-1@example.com',
          role: 'admin',
          roleSource: 'manual',
          invitedAt: '0',
          acceptedAt: '1',
        },
        {
          id: 'member-member-1',
          memberType: 'oidc_user',
          userId: 'member-1',
          email: 'member-1@example.com',
          role: 'member',
          roleSource: 'manual',
          invitedAt: '0',
          acceptedAt: '1',
        },
      ],
      mode: 'sso_only',
      tokens: [],
    })

    await waitFor(() => {
      expect(screen.getByText('治理 / RBAC 就绪度')).toBeInTheDocument()
    })
    await waitFor(() => {
      expect(
        screen.getByText('sso_only / 2 active members / 1 admins / 0 member audit events'),
      ).toBeInTheDocument()
    })
    expect(screen.getByText('2 governance readiness checks are blocked')).toBeInTheDocument()
    expect(screen.getByText('sso_only / 0 active break-glass / 0 lockouts')).toBeInTheDocument()
    expect(
      screen.getByText('0 IdP-managed / 2 active members / 0 pending invites'),
    ).toBeInTheDocument()
    expect(screen.getByText('2 roles represented / 2 active members')).toBeInTheDocument()
    expect(screen.getByText('1 active admins')).toBeInTheDocument()
    expect(screen.getByText('0 member audit events / 0 actors')).toBeInTheDocument()
  })

  it('covers issue, copy, revoke-all, and cutover failure flows', async () => {
    const evidence = setupSecurityResponses({ tokens: [], lockouts: [], mode: 'hybrid' })
    server.use(
      http.post('/fb/v1/console/auth/sso/cutover', () =>
        HttpResponse.json({
          success: false,
          message: 'SSO cutover blocked by preflight checks',
          preflight: {
            status: 'fail',
            checks: [
              {
                name: 'sso:oidc_enabled',
                status: 'pass',
                message: 'OIDC configured',
              },
              {
                name: 'sso:redirect_uri_match',
                status: 'fail',
                message: 'redirect mismatch',
                remediation: 'Fix redirect URI',
              },
            ],
          },
        }),
      ),
    )

    const writeSpy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined)
    const { user } = renderWithProviders(<SecurityPage evidence={evidence} />)

    await waitFor(() => {
      expect(screen.getByText('暂无令牌')).toBeInTheDocument()
    })
    expect(screen.getByText('暂无锁定 IP')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '撤销全部活跃令牌' })).toBeDisabled()

    const issueButtons = screen.getAllByRole('button', { name: '签发令牌' })
    await user.click(issueButtons[issueButtons.length - 1])
    const issueDialog = await screen.findByRole('dialog')
    await user.type(within(issueDialog).getByLabelText('管理员邮箱'), 'ops@example.com')
    await user.type(within(issueDialog).getByLabelText(/IP 白名单/), '203.0.113.0/24')
    await user.click(within(issueDialog).getByRole('button', { name: '签发令牌' }))

    const tokenDialog = await screen.findByRole('dialog')
    await user.click(within(tokenDialog).getAllByRole('button')[0])
    expect(writeSpy).toHaveBeenCalledWith(expect.stringContaining('bg-issued-token'))
    expect(toastSuccess).toHaveBeenCalledWith('已复制')

    await user.click(within(tokenDialog).getByRole('button', { name: '完成' }))
    await waitFor(() => {
      expect(screen.getByText('ops@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '撤销全部活跃令牌' }))
    const revokeDialog = await screen.findByRole('dialog')
    await user.click(within(revokeDialog).getByRole('button', { name: '撤销全部活跃令牌' }))

    await waitFor(() => {
      expect(screen.getByText('暂无令牌')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '强制 SSO' }))
    const cutoverDialog = await screen.findByRole('dialog')
    expect(within(cutoverDialog).getByText('无活跃令牌')).toBeInTheDocument()
    await user.click(within(cutoverDialog).getByRole('button', { name: '切换至 SSO' }))

    await waitFor(() => {
      expect(screen.getByText('预检失败')).toBeInTheDocument()
    })
    expect(screen.getByText('OIDC configured')).toBeInTheDocument()
    expect(screen.getByText('redirect mismatch')).toBeInTheDocument()

    await user.click(within(cutoverDialog).getByRole('checkbox', { name: '跳过 break-glass 检查' }))
    await user.keyboard('{Escape}')
    await waitFor(() => {
      expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '强制 SSO' }))
    const resetDialog = await screen.findByRole('dialog')
    expect(within(resetDialog).queryByText('预检失败')).not.toBeInTheDocument()
    expect(
      within(resetDialog).getByRole('checkbox', { name: '跳过 break-glass 检查' }),
    ).not.toBeChecked()
    await user.click(within(resetDialog).getByRole('button', { name: '取消' }))
  })

  it('switches back to hybrid mode from the SSO-only action path', async () => {
    const evidence = setupSecurityResponses({ tokens: [], lockouts: [], mode: 'sso_only' })
    let currentMode: 'hybrid' | 'sso_only' = 'sso_only'
    server.use(
      http.get('/fb/v1/console/auth/sso/mode', () => HttpResponse.json({ mode: currentMode })),
      http.post('/fb/v1/console/auth/sso/fallback', () => {
        currentMode = 'hybrid'
        return HttpResponse.json({ success: true, message: 'Switched to hybrid mode' })
      }),
    )

    const { user } = renderWithProviders(<SecurityPage evidence={evidence} />)

    await waitFor(() => {
      expect(screen.getByText('仅 SSO')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '启用密码登录' }))
    const fallbackDialog = await screen.findByRole('dialog')
    await user.click(within(fallbackDialog).getByRole('button', { name: '启用密码' }))

    await waitFor(() => {
      expect(screen.getByText('混合模式')).toBeInTheDocument()
    })
  })

  it('switches to SSO-only mode after a successful cutover', async () => {
    let currentMode: 'hybrid' | 'sso_only' = 'hybrid'
    let cutoverRequest: { skip_breakglass_check?: boolean } = {}
    const evidence = setupSecurityResponses({ lockouts: [], mode: currentMode })
    server.use(
      http.get('/fb/v1/console/auth/sso/mode', () => HttpResponse.json({ mode: currentMode })),
      http.post('/fb/v1/console/auth/sso/cutover', async ({ request }) => {
        cutoverRequest = (await request.json()) as { skip_breakglass_check?: boolean }
        currentMode = 'sso_only'
        return HttpResponse.json({ success: true, message: 'Switched to SSO-only mode' })
      }),
    )

    const { user } = renderWithProviders(<SecurityPage evidence={evidence} />)

    await waitFor(() => {
      expect(screen.getByText('混合模式')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '强制 SSO' }))
    const cutoverDialog = await screen.findByRole('dialog')
    await user.click(within(cutoverDialog).getByRole('checkbox', { name: '跳过 break-glass 检查' }))
    await user.click(within(cutoverDialog).getByRole('button', { name: '切换至 SSO' }))

    await waitFor(() => {
      expect(toastSuccess).toHaveBeenCalledWith('已切换至仅 SSO 模式')
    })
    expect(cutoverRequest.skip_breakglass_check).toBe(true)
    await waitFor(() => {
      expect(screen.getByText('仅 SSO')).toBeInTheDocument()
    })
  })

  it('paginates and revokes a single break-glass token', async () => {
    const tokens = Array.from({ length: 12 }, (_, index) =>
      makeToken({
        id: `token-${index + 1}`,
        admin_email: `pager-${index + 1}@example.com`,
      }),
    )
    const { user } = renderSecurityPage({ tokens, lockouts: [] })

    await waitFor(() => {
      expect(screen.getByText('pager-1@example.com')).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: '上一页' })).toBeDisabled()
    expect(screen.getByRole('button', { name: '下一页' })).toBeEnabled()

    await user.click(screen.getByRole('button', { name: '下一页' }))
    await waitFor(() => {
      expect(screen.getByText('pager-11@example.com')).toBeInTheDocument()
    })
    expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: '上一页' }))
    await waitFor(() => {
      expect(screen.getByText('pager-1@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '撤销令牌 pager-1@example.com' }))
    await waitFor(() => {
      expect(toastSuccess).toHaveBeenCalledWith('令牌已撤销')
    })
    await waitFor(() => {
      expect(
        screen.queryByRole('button', { name: '撤销令牌 pager-1@example.com' }),
      ).not.toBeInTheDocument()
    })
  })

  it('sorts break-glass lockouts by soonest unlock time', async () => {
    renderSecurityPage({
      lockouts: [
        {
          ip: '203.0.113.20',
          locked_until: '2026-07-05T16:00:00Z',
          remaining_mins: 70,
          attempts: 6,
        },
        {
          ip: '203.0.113.11',
          locked_until: '2026-07-05T14:00:00Z',
          remaining_mins: 10,
          attempts: 4,
        },
      ],
    })

    await screen.findByText('203.0.113.11', {}, { timeout: 10_000 })

    const lockoutRows = screen
      .getAllByRole('row')
      .filter((row) => row.textContent?.includes('203.0.113.'))
    expect(lockoutRows.map((row) => row.textContent)).toEqual([
      expect.stringContaining('203.0.113.11'),
      expect.stringContaining('203.0.113.20'),
    ])
  })

  it('revokes all active tokens from the security page', async () => {
    const { user } = renderSecurityPage()

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '撤销全部活跃令牌' }))

    const dialog = await screen.findByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: '撤销全部活跃令牌' }))

    await waitFor(() => {
      expect(screen.queryByText('admin@example.com')).not.toBeInTheDocument()
    })
    expect(screen.getByText('暂无令牌')).toBeInTheDocument()
  })

  it('keeps issue and revoke-all dialogs open when mutations fail', async () => {
    const evidence = setupSecurityResponses()
    server.use(
      http.post('/fb/v1/console/auth/breakglass/issue', () => apiFailure('issue denied')),
      http.post('/fb/v1/console/auth/breakglass/tokens/revoke-all', () =>
        apiFailure('revoke all denied'),
      ),
    )
    const { user } = renderWithProviders(<SecurityPage evidence={evidence} />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '签发令牌' }))
    const issueDialog = await screen.findByRole('dialog')
    await user.type(within(issueDialog).getByLabelText('管理员邮箱'), 'ops@example.com')
    await user.click(within(issueDialog).getByRole('button', { name: '签发令牌' }))

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('issue denied')
    })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.click(within(issueDialog).getByRole('button', { name: '取消' }))

    await user.click(screen.getByRole('button', { name: '撤销全部活跃令牌' }))
    const revokeDialog = await screen.findByRole('dialog')
    await user.click(within(revokeDialog).getByRole('button', { name: '撤销全部活跃令牌' }))

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('revoke all denied')
    })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.click(within(revokeDialog).getByRole('button', { name: '取消' }))
  })

  it('shows errors when single-token revoke and lockout unlock fail', async () => {
    const evidence = setupSecurityResponses()
    server.use(
      http.post('/fb/v1/console/auth/breakglass/tokens/:tokenId/revoke', () =>
        apiFailure('single revoke denied'),
      ),
      http.post('/fb/v1/console/auth/breakglass/lockouts/:ip/unlock', () =>
        apiFailure('unlock denied'),
      ),
    )
    const { user } = renderWithProviders(<SecurityPage evidence={evidence} />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '撤销令牌 admin@example.com' }))
    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('single revoke denied')
    })

    await user.click(screen.getByRole('button', { name: '解除锁定' }))
    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('unlock denied')
    })
  })

  it('surfaces cutover errors when the cutover request cannot be parsed', async () => {
    const evidence = setupSecurityResponses({ lockouts: [] })
    server.use(
      http.post(
        '/fb/v1/console/auth/sso/cutover',
        () => new HttpResponse('not-json', { status: 500 }),
      ),
    )
    const { user } = renderWithProviders(<SecurityPage evidence={evidence} />)

    await waitFor(() => {
      expect(screen.getByText('admin@example.com')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '强制 SSO' }))
    const cutoverDialog = await screen.findByRole('dialog')
    await user.click(within(cutoverDialog).getByRole('button', { name: '切换至 SSO' }))

    await waitFor(() => {
      expect(toastError).toHaveBeenCalled()
    })
  })

  it('keeps the fallback dialog open when switching back to hybrid fails', async () => {
    const evidence = setupSecurityResponses({ tokens: [], lockouts: [], mode: 'sso_only' })
    server.use(http.post('/fb/v1/console/auth/sso/fallback', () => apiFailure('fallback denied')))
    const { user } = renderWithProviders(<SecurityPage evidence={evidence} />)

    await waitFor(() => {
      expect(screen.getByText('仅 SSO')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '启用密码登录' }))
    const fallbackDialog = await screen.findByRole('dialog')
    await user.click(within(fallbackDialog).getByRole('button', { name: '启用密码' }))

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledWith('fallback denied')
    })
    expect(screen.getByRole('dialog')).toBeInTheDocument()
    await user.click(within(fallbackDialog).getByRole('button', { name: '取消' }))
  })

  it('unlocks a locked IP from the lockout table', async () => {
    const { user } = renderSecurityPage()

    await waitFor(() => {
      expect(screen.getByText('203.0.113.10')).toBeInTheDocument()
    })

    await user.click(screen.getByRole('button', { name: '解除锁定' }))

    await waitFor(() => {
      expect(screen.queryByText('203.0.113.10')).not.toBeInTheDocument()
    })
    expect(screen.getByText('暂无锁定 IP')).toBeInTheDocument()
  })
})
