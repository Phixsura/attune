import { HttpResponse, http } from 'msw'
import type {
  ListLockoutsResponse,
  ListTokensResponse,
  RevokeAllResponse,
} from '@/features/security/api/breakglass'
import type {
  ApiKey,
  CreateApiKeyResponse,
  ListApiKeysResponse,
  ListScopePresetsResponse,
  ListScopesResponse,
  ListServiceAccountsResponse,
} from '@/proto/attune/v1/api_key'
import type {
  GetClassificationQualityResponse,
  GetClassificationReviewLearningResponse,
} from '@/proto/attune/v1/classification_quality'
import type { DigestSubscription } from '@/proto/attune/v1/digest_subscription'
import type {
  EnrichConfig,
  GetEnrichConfigResponse,
  PreviewEnrichPromptResponse,
  UpdateEnrichConfigResponse,
} from '@/proto/attune/v1/enrich_config'
import type {
  ListExternalProviderInstallationResourcesResponse,
  ListExternalProviderInstallationsResponse,
  ListExternalSyncProvidersResponse,
  SelectExternalProviderInstallationResourcesResponse,
} from '@/proto/attune/v1/external_sync'
import type { InboundSource } from '@/proto/attune/v1/inbound_source'
import type {
  FeedbackAssignment,
  FeedbackAssignmentEscalationQueue,
  FeedbackAssignmentPolicy,
  FeedbackDetail,
  FeedbackIdentitySubjectDetail,
  FeedbackSignalTrace,
  FeedbackTriageCommandCenter,
  GetFeedbackIdentityReviewResponse,
  GetFeedbackStatsResponse,
  ListFeedbackResponse,
  ListReplySendHookDeliveriesResponse,
  MergeFeedbackIdentityReviewResponse,
  ReplySendHook,
  ReplySendHookDelivery,
  ReplySendHookHealth,
  SplitFeedbackIdentityReviewResponse,
} from '@/proto/attune/v1/ingest'
import type {
  ListLLMChannelAbilitiesResponse,
  ListLLMChannelModelsResponse,
  ListLLMChannelsResponse,
  ListLLMRoutesResponse,
  LLMChannel,
  LLMChannelAbility,
  LLMProviderModel,
  LLMRoute,
  TestLLMChannelResponse,
} from '@/proto/attune/v1/llm_config'
import type { ListMembersResponse } from '@/proto/attune/v1/member'
import type {
  ListNotifyTargetsResponse,
  NotifyTarget,
  TestNotifyTargetResponse,
} from '@/proto/attune/v1/notify_target'
import {
  type ListDeliveriesResponse,
  OutboxFailureKind,
  type RetryDeliveryResponse,
} from '@/proto/attune/v1/outbox'
import type { RequestNotificationStatusEvidenceResponse } from '@/proto/attune/v1/request_notification'
import type { GetSearchQualityResponse, SemanticSearchResponse } from '@/proto/attune/v1/search'
import type { GetMeResponse } from '@/proto/attune/v1/session'
import {
  type PreviewSurveyRecipientsResponse,
  type SurveyAnalytics,
  type SurveyAnalyticsInsight,
  SurveyAnalyticsInsightSeverity,
  type SurveyAnalyticsSegment,
  SurveyAnalyticsSegmentDimension,
  type SurveyAnalyticsTrendBucket,
  type SurveyCampaign,
  type SurveyCampaignHealth,
  SurveyCampaignHealthCheckStatus,
  SurveyCampaignHealthStatus,
  SurveyCampaignStatus,
  SurveyDedupePolicy,
  SurveyDeliveryStatus,
  SurveyDistributionMode,
  type SurveyInvitation,
  type SurveyLowScoreReview,
  SurveyLowScoreReviewStatus,
  SurveyLowScoreSeverity,
  SurveyRecoveryNotificationStatus,
  SurveyRecoverySlaStatus,
  type SurveyResponse,
  SurveyResponseStatus,
  SurveySuppressionStatus,
  SurveyTriggerEvent,
  SurveyType,
} from '@/proto/attune/v1/survey'
import type { GetLLMUsageResponse, GetUsageResponse } from '@/proto/attune/v1/usage'
import type { ListAuditResponse, ListStatesResponse } from '@/proto/attune/v1/workflow'

// Forward-friendly default handlers for every /fb/v1/console/* endpoint.
// Per §4-G of the design proposal: cover the full surface so adding a
// new endpoint in production immediately triggers a clear "unhandled
// request" failure if the matching test hasn't added a mock.
//
// Every default export is typed against its ts-proto generated type.
// If a proto shape changes, this file fails to compile — the test
// fixture drift discovered during the first iteration of this PR
// (#3 in the self-review) is no longer possible.
//
// Per-test overrides use `server.use(http.<verb>(...))` for case-
// specific shapes (error envelopes, paginated cursors, 401s, etc.).

const BASE = '/fb/v1/console'

// Session ------------------------------------------------------------------
export const defaultMe: GetMeResponse = {
  tenant: {
    id: 't-1',
    name: 'Default Tenant',
    slug: 'default',
    locale: 'zh-CN',
    timezone: 'UTC',
  },
  user: {
    openId: 'u-1',
    name: 'Tester',
    role: 'admin',
  },
  csrfToken: 'csrf-test-token',
}
export const defaultMembersList: ListMembersResponse = { members: [] }

// API keys -----------------------------------------------------------------
export const defaultApiKeysList: ListApiKeysResponse = { items: [] }
export const defaultServiceAccountsList: ListServiceAccountsResponse = { items: [] }
export const defaultApiKeyScopes: ListScopesResponse = { scopes: [] }
export const defaultApiKeyPresets: ListScopePresetsResponse = { presets: [] }
const defaultIssuedKey: ApiKey = {
  id: 'k-new',
  label: 'fresh',
  keyPrefix: 'sk_test_',
  isActive: true,
  createdAt: '2026-06-07T00:00:00Z',
  scopes: [],
  allowedCidrs: [],
  usageCount: '0',
  environment: '',
}
export const defaultCreateApiKey: CreateApiKeyResponse = {
  key: defaultIssuedKey,
  secret: 'sk_test_secret_value_redacted_in_real_envs',
}

// Notify targets -----------------------------------------------------------
export const defaultNotifyTargetsList: ListNotifyTargetsResponse = { items: [] }
const sampleNotifyTarget: NotifyTarget = {
  id: 'nt-new',
  destinationType: 'raw-webhook',
  audience: 'all',
  url: 'https://example.com/hook',
  timeoutSeconds: 10,
  disabled: false,
  createdAt: '2026-06-07T00:00:00Z',
  lastError: '',
}
const defaultTestNotifyTargetResponse: TestNotifyTargetResponse = { ok: true, statusCode: 200 }

// Outbox dead-letter queue --------------------------------------------------
export const defaultDeliveriesList: ListDeliveriesResponse = {
  deliveries: [],
  nextBeforeId: '0',
}
const defaultExternalSyncProviders: ListExternalSyncProvidersResponse = {
  providers: [
    { provider: 'github', display: 'GitHub' },
    { provider: 'jira', display: 'Jira' },
  ],
}
const defaultExternalProviderInstallations: ListExternalProviderInstallationsResponse = {
  installations: [],
}
const defaultExternalProviderInstallationResources: ListExternalProviderInstallationResourcesResponse =
  { resources: [] }
const defaultSelectExternalProviderInstallationResources: SelectExternalProviderInstallationResourcesResponse =
  { resources: [] }
const sampleDelivery = {
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
const defaultRetryDeliveryResponse: RetryDeliveryResponse = {
  delivery: { ...sampleDelivery, status: 'pending', attempts: 0 },
}

// Reply send hook -----------------------------------------------------------
const defaultReplySendHook: ReplySendHook = {
  id: 'reply-hook-default',
  name: 'Support reply bridge',
  enabled: true,
  urlHost: 'hooks.example.com',
  urlFingerprint: 'sha256:mock-reply-hook',
  createdBy: 'u-1',
  updatedBy: 'u-1',
  createdAt: '2026-06-24T09:00:00Z',
  updatedAt: '2026-06-24T09:00:00Z',
}

const defaultReplySendHookDelivery: ReplySendHookDelivery = {
  id: 'reply-delivery-default',
  hookId: defaultReplySendHook.id,
  hookHost: defaultReplySendHook.urlHost,
  hookFingerprint: defaultReplySendHook.urlFingerprint,
  eventType: 'reply.test',
  status: 'failed',
  idempotencyKey: 'reply_test_default',
  httpStatus: 500,
  attempts: 1,
  maxAttempts: 8,
  error: 'receiver returned 500',
  requestedByType: 'admin',
  requestedBy: 'u-1',
  requestedAt: '2026-06-24T09:05:00Z',
  createdAt: '2026-06-24T09:05:00Z',
  updatedAt: '2026-06-24T09:05:00Z',
  retryable: true,
}

const defaultReplySendHookDeliveries: ListReplySendHookDeliveriesResponse = {
  items: [defaultReplySendHookDelivery],
}

const defaultReplySendHookHealth: ReplySendHookHealth = {
  accepted: '0',
  dead: '0',
  failed: '1',
  latestDelivery: defaultReplySendHookDelivery,
  latestProblem: defaultReplySendHookDelivery,
  pending: '0',
  retryable: '1',
  total: '1',
}

function acceptedReplySendHookDelivery(
  delivery: ReplySendHookDelivery = defaultReplySendHookDelivery,
): ReplySendHookDelivery {
  return {
    ...delivery,
    status: 'accepted',
    httpStatus: 204,
    attempts: delivery.attempts + 1,
    error: undefined,
    completedAt: '2026-06-24T09:06:00Z',
    updatedAt: '2026-06-24T09:06:00Z',
    retryable: false,
  }
}

// LLM config ---------------------------------------------------------------
const sampleLLMChannel: LLMChannel = {
  id: '11111111-1111-1111-1111-111111111111',
  name: 'Primary',
  protocol: 'openai-compat',
  baseUrl: 'http://localhost:11434',
  authMode: 'bearer',
  hasApiKey: true,
  credentialKeyId: '123',
  status: 'enabled',
  priority: 100,
  weight: 1,
  timeoutSeconds: 60,
  createdAt: '2026-06-11T00:00:00Z',
  updatedAt: '2026-06-11T00:00:00Z',
  lastTestStatus: '',
  lastError: '',
}
const sampleLLMAbility: LLMChannelAbility = {
  id: '22222222-2222-2222-2222-222222222222',
  channelId: sampleLLMChannel.id,
  logicalModel: 'enrich-default',
  providerModel: 'gpt-4o-mini',
  enabled: true,
  priority: 100,
  weight: 1,
  createdAt: '2026-06-11T00:00:00Z',
  updatedAt: '2026-06-11T00:00:00Z',
}
const sampleLLMModels: LLMProviderModel[] = [
  { id: 'gpt-4o-mini', displayName: 'GPT 4o mini', ownedBy: 'openai' },
  { id: 'gpt-4.1-mini', displayName: 'GPT 4.1 mini', ownedBy: 'openai' },
]
const sampleLLMRoute: LLMRoute = {
  id: '33333333-3333-3333-3333-333333333333',
  tenantId: '',
  purpose: 'enrich',
  logicalModel: 'enrich-default',
  enabled: true,
  createdAt: '2026-06-11T00:00:00Z',
  updatedAt: '2026-06-11T00:00:00Z',
}
export const defaultLLMChannelsList: ListLLMChannelsResponse = { items: [sampleLLMChannel] }
export const defaultLLMAbilitiesList: ListLLMChannelAbilitiesResponse = {
  items: [sampleLLMAbility],
}
export const defaultLLMModelsList: ListLLMChannelModelsResponse = { items: sampleLLMModels }
export const defaultLLMRoutesList: ListLLMRoutesResponse = { items: [sampleLLMRoute] }
const defaultTestLLMChannelResponse: TestLLMChannelResponse = {
  ok: true,
  providerModel: 'gpt-4o-mini',
  text: 'attune-ok',
  inputTokens: 3,
  outputTokens: 2,
  latencyMs: '42',
  channel: sampleLLMChannel,
}

// Enrich config ------------------------------------------------------------
export const defaultEnrichConfig: EnrichConfig = {
  promptTemplate: undefined,
  defaultPromptTemplate: 'DEFAULT {{content}} {{dimensions}}',
  dimensions: [],
  promptPolicy: defaultPromptPolicy('built_in'),
  policyConfig: {
    outputLanguagePolicy: 'source_and_display',
    titleMaxChars: 30,
    rationaleMaxChars: 30,
    displayFieldsRequired: true,
    tone: 'concise',
    domainGuidance: '',
  },
  promptVersions: [
    {
      id: '11111111-2222-3333-4444-555555555555',
      promptVersion: 'enrich.default@1',
      promptFingerprint: 'sha256:mock-default-prompt',
      schemaFingerprint: 'sha256:mock-schema',
      policyId: 'enrich.default',
      policyVersion: '1',
      mode: 'default',
      promptSource: 'built_in',
      createdAt: '2026-06-21T00:00:00Z',
      isActive: true,
      hasTemplate: false,
      dimensionsCount: 0,
      dimensions: [],
      policyConfig: {
        outputLanguagePolicy: 'source_and_display',
        titleMaxChars: 30,
        rationaleMaxChars: 30,
        displayFieldsRequired: true,
        tone: 'concise',
        domainGuidance: '',
      },
      warnings: [],
    },
  ],
}

function defaultPromptPolicy(source: 'built_in' | 'custom_template') {
  if (source === 'custom_template') {
    return {
      policyId: 'enrich.legacy_custom_template',
      policyVersion: 'sha256:mock',
      promptVersion: 'enrich.legacy_custom_template@sha256:mock',
      promptFingerprint: 'sha256:mock-custom-prompt',
      schemaFingerprint: 'sha256:mock-schema',
      mode: 'legacy_custom_override',
      promptSource: 'custom_template',
      templateLanguage: 'custom',
      displayLocale: 'zh-CN',
      displayLanguageName: 'Simplified Chinese',
      variables: [],
      outputs: [],
      warnings: [],
    }
  }
  return {
    policyId: 'enrich.default',
    policyVersion: '1',
    promptVersion: 'enrich.default@1',
    promptFingerprint: 'sha256:mock-default-prompt',
    schemaFingerprint: 'sha256:mock-schema',
    mode: 'default',
    promptSource: 'built_in',
    templateLanguage: 'en',
    displayLocale: 'zh-CN',
    displayLanguageName: 'Simplified Chinese',
    variables: [],
    outputs: [],
    warnings: [],
  }
}
export const defaultGetEnrichConfig: GetEnrichConfigResponse = { config: defaultEnrichConfig }
export const defaultPreviewEnrichPrompt: PreviewEnrichPromptResponse = {
  renderedPrompt: '',
  promptPolicy: defaultEnrichConfig.promptPolicy,
}

// Feedback -----------------------------------------------------------------
export const defaultFeedbackList: ListFeedbackResponse = { items: [], nextCursor: undefined }
export const defaultFeedbackDetail: FeedbackDetail = {
  id: 'f-1',
  content: 'sample feedback content',
  source: 'web',
  type: 'feedback',
  userId: '',
  pageUrl: '',
  enrichedTitle: 'Sample',
  enrichedAttrs: {},
  isUrgent: false,
  replyDraftEnabled: false,
  enrichmentStatus: 'done',
  createdAt: '2026-06-07T00:00:00Z',
  classificationConfidence: 0.82,
  attachments: [],
  enrichedRationale: '',
  enrichedAt: '2026-06-07T00:00:00Z',
  enrichmentError: '',
  tags: [],
  allowedNextStates: [],
  accountContext: {
    accountKey: 'acct:sample',
    accountDisplay: 'Sample Account',
    source: 'source_meta',
  },
  assignment: {
    feedbackId: 'f-1',
    assignedBy: '',
    slaStatus: 'missing_due_date',
    note: '',
  },
}
export const defaultFeedbackSignalTrace: FeedbackSignalTrace = {
  feedbackId: 'f-1',
  signalTraceId: 'trace-f-1',
  source: 'web',
  terminalStatus: 'completed',
  complete: true,
  missingStages: [],
  generatedAt: '2026-06-07T00:05:00Z',
  stages: [
    {
      key: 'source_event',
      label: 'Source event',
      status: 'completed',
      eventCount: 1,
      lastEventAt: '2026-06-07T00:00:00Z',
    },
    {
      key: 'enrichment',
      label: 'AI enrichment',
      status: 'completed',
      eventCount: 2,
      lastEventAt: '2026-06-07T00:01:00Z',
    },
    {
      key: 'request',
      label: 'Customer request',
      status: 'completed',
      eventCount: 1,
      lastEventAt: '2026-06-07T00:02:00Z',
    },
    {
      key: 'notification',
      label: 'Customer notification',
      status: 'completed',
      eventCount: 1,
      lastEventAt: '2026-06-07T00:03:00Z',
    },
    {
      key: 'survey',
      label: 'Survey follow-up',
      status: 'completed',
      eventCount: 1,
      lastEventAt: '2026-06-07T00:04:00Z',
    },
  ],
  events: [
    {
      stage: 'source_event',
      kind: 'source_captured',
      status: 'completed',
      traceId: 'trace-f-1',
      summary: 'Feedback source event captured',
      occurredAt: '2026-06-07T00:00:00Z',
      metadata: { source: 'web' },
    },
    {
      stage: 'enrichment',
      kind: 'llm_call',
      status: 'completed',
      traceId: 'trace-f-1',
      summary: 'LLM call recorded for enrichment',
      occurredAt: '2026-06-07T00:01:00Z',
      metadata: { model_id: 'gpt-4o-mini', purpose: 'enrichment' },
    },
  ],
}
export const defaultFeedbackStats: GetFeedbackStatsResponse = {
  periodStart: '',
  periodEnd: '',
  total: '0',
  dims: [],
  urgentCount: '0',
}
export const defaultRequestNotificationStatusEvidence: RequestNotificationStatusEvidenceResponse = {
  items: [],
}
export const defaultFeedbackTriageCommandCenter: FeedbackTriageCommandCenter = {
  generatedAt: '2026-08-01T00:00:00Z',
  openCount: '0',
  activeCount: '0',
  closedCount: '0',
  urgentOpenCount: '0',
  terminalFailureCount: '0',
  identityDebtCount: '0',
  overdueCount: '0',
  dueSoonCount: '0',
  lanes: [],
}
export const defaultFeedbackAssignmentEscalations: FeedbackAssignmentEscalationQueue = {
  generatedAt: '2026-08-01T00:00:00Z',
  overdueCount: '1',
  dueSoonCount: '1',
  missingOwnerCount: '1',
  missingSlaCount: '1',
  items: [
    {
      feedbackId: 'f-1',
      title: 'Sample assignment risk',
      source: 'web',
      type: 'bug',
      isUrgent: true,
      createdAt: '2026-07-31T09:00:00Z',
      assignment: {
        feedbackId: 'f-1',
        assignedBy: '',
        slaDueAt: '2026-07-31T21:00:00Z',
        slaStatus: 'overdue',
        note: 'Mocked escalation queue sample.',
      },
      escalationReasons: ['overdue', 'missing_owner'],
      hoursUntilDue: -3,
      priority: 'critical',
      accountContext: {
        accountKey: 'acct:sample',
        accountDisplay: 'Sample Account',
        source: 'source_meta',
      },
    },
  ],
}
export const defaultFeedbackAssignmentPolicy: FeedbackAssignmentPolicy = {
  version: 1,
  updatedBy: 'system',
  note: 'Default assignment policy',
  rules: [
    {
      ruleKey: 'urgent_open',
      ruleName: 'Urgent open feedback',
      ownerLane: 'support_triage',
      severity: 'critical',
      slaHours: 24,
      enabled: true,
      rationale:
        'Urgent open feedback should be confirmed and assigned before the next business cycle.',
    },
    {
      ruleKey: 'terminal_failures',
      ruleName: 'Terminal AI failures',
      ownerLane: 'ai_ops',
      severity: 'high',
      slaHours: 48,
      enabled: true,
      rationale:
        'Terminal enrichment failures need operator review before retrying or changing model configuration.',
    },
    {
      ruleKey: 'stalled_active',
      ruleName: 'Active work at risk',
      ownerLane: 'product_owner',
      severity: 'high',
      slaHours: 168,
      enabled: true,
      rationale:
        'Active feedback should keep an explicit deadline so committed work does not disappear.',
    },
    {
      ruleKey: 'identity_debt',
      ruleName: 'Identity evidence debt',
      ownerLane: 'data_quality',
      severity: 'medium',
      slaHours: 96,
      enabled: true,
      rationale:
        'Feedback without stable identity evidence should be repaired before merging demand or notifying customers.',
    },
    {
      ruleKey: 'untriaged',
      ruleName: 'Untriaged intake',
      ownerLane: 'triage_dri',
      severity: 'high',
      slaHours: 72,
      enabled: true,
      rationale:
        'Open intake should get an owner lane and deadline before promotion or closure decisions.',
    },
  ],
}
let feedbackAssignmentPolicyState: FeedbackAssignmentPolicy = structuredClone(
  defaultFeedbackAssignmentPolicy,
)
export const defaultFeedbackIdentityReview: GetFeedbackIdentityReviewResponse = {
  summary: {
    scannedFeedbackCount: 0,
    mergeCandidateCount: 0,
    needsEvidenceCount: 0,
    strongCandidateCount: 0,
    weakFeedbackCount: 0,
  },
  mergeCandidates: [],
  needsEvidence: [],
  recentMerges: [],
  subjectRoster: {
    activeSubjectCount: 0,
    activeIdentityCount: 0,
    evidenceCount: 0,
    subjects: [],
  },
}
export const defaultFeedbackIdentityMerge: MergeFeedbackIdentityReviewResponse = {
  subject: {
    id: 'subject-default',
    displayName: 'ada@example.com',
    primaryIdentityKind: 'email',
    primaryIdentityValue: 'ada@example.com',
    status: 'active',
    identityCount: 1,
    evidenceCount: 2,
    createdAt: '2026-08-01T00:00:00Z',
    updatedAt: '2026-08-01T00:00:00Z',
  },
  evidenceCount: 2,
  createdSubject: true,
  action: 'signal_subject.merge',
}
export const defaultFeedbackIdentitySplit: SplitFeedbackIdentityReviewResponse = {
  subject: {
    id: 'subject-default',
    displayName: 'ada@example.com',
    primaryIdentityKind: '',
    primaryIdentityValue: '',
    status: 'active',
    identityCount: 0,
    evidenceCount: 0,
    createdAt: '2026-08-01T00:00:00Z',
    updatedAt: '2026-08-01T00:00:00Z',
  },
  evidenceCount: 2,
  action: 'signal_subject.split',
}
export const defaultFeedbackIdentitySubjectDetail: FeedbackIdentitySubjectDetail = {
  subject: defaultFeedbackIdentityMerge.subject,
  identities: [
    {
      id: 'identity-default-email',
      kind: 'email',
      value: 'ada@example.com',
      source: 'review',
      confidence: 'reviewed',
      evidenceCount: 2,
      firstFeedbackId: '201',
      latestFeedbackId: '202',
      revoked: false,
      revokedAt: '',
      createdAt: '2026-08-01T00:00:00Z',
      updatedAt: '2026-08-01T00:00:00Z',
    },
  ],
  events: [
    {
      id: 'event-default-merge',
      action: 'review_merge',
      identityKind: 'email',
      identityValue: 'ada@example.com',
      evidenceCount: 2,
      feedbackIds: ['201', '202'],
      evidence: [
        {
          feedbackId: '201',
          source: 'web',
          sourceUser: 'web-1',
          createdAt: '2026-08-01T00:00:00Z',
          excerpt: 'Ada reported the same checkout failure from the web portal.',
          keys: [{ kind: 'email', value: 'ada@example.com', source: 'source_meta.email' }],
        },
      ],
      note: 'reviewed',
      createdBy: 'user-1',
      createdAt: '2026-08-01T00:00:00Z',
    },
  ],
}
export const defaultInboundSources: InboundSource[] = [
  {
    id: 'source-default-webhook',
    tenantId: 't-1',
    channel: 'webhook',
    name: 'Default webhook',
    slug: 'default-webhook',
    enabled: true,
    lastEventAt: '2026-07-10T12:00:00Z',
    lastUid: '0',
    lastError: '',
    createdAt: '2026-07-10T12:00:00Z',
    updatedAt: '2026-07-10T12:00:00Z',
  },
]
export const defaultSemanticSearchResponse: SemanticSearchResponse = {
  hits: [],
  embeddingModel: '',
  totalWithEmbeddings: 0,
  usedKeywordFallback: false,
  rankingVersion: 'rrf.pgfts.v1.k60',
  coverage: undefined,
  runId: '11111111-2222-3333-4444-555555555555',
}
export const defaultLLMUsage: GetLLMUsageResponse = {
  periodStart: '2026-06-01T00:00:00Z',
  periodEnd: '2026-06-10T00:00:00Z',
  granularity: 'week',
  series: [],
  promptTokens: '0',
  completionTokens: '0',
  costUsd: 0,
  calls: '0',
  errors: '0',
}
export const defaultUsage: GetUsageResponse = {
  periodStart: '2026-07-01T00:00:00Z',
  periodEnd: '2026-07-31T23:59:59Z',
  total: '72',
  quota: '100',
  series: [{ bucket: '2026-07-01T00:00:00Z', value: '72' }],
}
export const defaultClassificationQuality: GetClassificationQualityResponse = {
  generatedAt: '2026-07-02T00:00:00Z',
  dataThrough: '2026-07-02T00:00:00Z',
  rollupLagSeconds: '0',
  currentFrom: '2026-06-25T00:00:00Z',
  currentTo: '2026-07-02T00:00:00Z',
  baselineFrom: '2026-05-28T00:00:00Z',
  baselineTo: '2026-06-25T00:00:00Z',
  bucketWidth: 'day',
  summary: {
    classificationEvents: '0',
    failedAttempts: '0',
    averageConfidence: 0,
    lowConfidenceRate: 0,
    offListRate: 0,
    unknownDimensionRate: 0,
    parseFailureRate: 0,
    terminalFailureRate: 0,
    worstSeverity: 'normal',
  },
  series: [],
  dimensions: [],
  warnings: [],
  samples: [],
}
export const defaultClassificationReviewLearning: GetClassificationReviewLearningResponse = {
  generatedAt: '2026-07-02T00:00:00Z',
  currentFrom: '2026-06-25T00:00:00Z',
  currentTo: '2026-07-02T00:00:00Z',
  totalReviews: '0',
  accepted: '0',
  edited: '0',
  dismissed: '0',
  trainingCandidateCount: '0',
  reviewedFeedbackCount: '0',
  classifiedFeedbackCount: '0',
  reviewCoverageRate: 0,
  reasonBuckets: [],
  recentEvents: [],
}
export const defaultSearchQuality: GetSearchQualityResponse = {
  generatedAt: '2026-07-02T00:00:00Z',
  currentFrom: '2026-06-25T00:00:00Z',
  currentTo: '2026-07-02T00:00:00Z',
  bucketWidth: 'day',
  summary: {
    queryCount: '0',
    zeroResultCount: '0',
    zeroResultRate: 0,
    fallbackCount: '0',
    fallbackRate: 0,
    clickCount: '0',
    clickThroughRate: 0,
    averageResultCount: 0,
    p95LatencyMs: '0',
    worstSeverity: 'normal',
  },
  series: [],
  queries: [],
  zeroResultQueries: [],
  fallbackBreakdown: [],
  indexHealth: {
    totalLiveFeedback: 0,
    totalWithEmbeddings: 0,
    coverageRatio: 1,
    embeddingModel: '',
    missingFeedbackCount: '0',
  },
  rankingVersions: [
    {
      rankingVersion: 'rrf.pgfts.v1.k60',
      status: 'active',
      trafficPercent: 100,
      notes: 'Current production ranker',
      updatedAt: '',
    },
  ],
}

export const defaultWorkflowStates: ListStatesResponse = { states: [] }
export const defaultWorkflowAudit: ListAuditResponse = { entries: [] }

const sampleDigestSubscription: DigestSubscription = {
  enabled: true,
  frequency: 'daily',
  sendHour: 9,
  llmMinFeedback: 6,
  sendOnEmpty: false,
  nextRunAt: '2026-06-14T09:00:00Z',
  lastRunAt: '2026-06-13T09:00:00Z',
  createdAt: '2026-06-07T00:00:00Z',
  updatedAt: '2026-06-13T21:00:00Z',
  clusteringEnabled: false,
}

export const sampleSurveyCampaign: SurveyCampaign = {
  id: 'survey-campaign-1',
  name: 'Resolution CSAT',
  surveyType: SurveyType.SURVEY_TYPE_CSAT,
  status: SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ACTIVE,
  triggerEvent: SurveyTriggerEvent.SURVEY_TRIGGER_EVENT_WORKFLOW_TRANSITION,
  distributionMode: SurveyDistributionMode.SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL,
  dedupePolicy: SurveyDedupePolicy.SURVEY_DEDUPE_POLICY_ONE_PER_RESOLUTION,
  triggerFilter: { workflow_category: 'closed' },
  content: {
    title: 'Satisfaction check',
    question: 'How satisfied are you with this resolution?',
  },
  locale: 'zh-CN',
  contentVersion: 1,
  samplingPercent: 100,
  minDaysBetweenContact: 14,
  expiresAfterDays: 14,
  maxDailyInvitations: 500,
  lowScoreThreshold: 3,
  requireRecentCustomerActivity: false,
  recentActivityDays: 30,
  suppressAutoResolved: true,
  createdBy: 'u-1',
  updatedBy: 'u-1',
  createdAt: '2026-07-30T00:00:00Z',
  updatedAt: '2026-07-30T00:00:00Z',
}

export const sampleSurveyInvitation: SurveyInvitation = {
  id: 'survey-invitation-1',
  campaignId: sampleSurveyCampaign.id,
  sourceType: 'feedback',
  sourceId: '101',
  distributionMode: SurveyDistributionMode.SURVEY_DISTRIBUTION_MODE_CONTACT_EMAIL,
  deliveryStatus: SurveyDeliveryStatus.SURVEY_DELIVERY_STATUS_ACCEPTED,
  responseStatus: SurveyResponseStatus.SURVEY_RESPONSE_STATUS_NOT_STARTED,
  suppressionStatus: SurveySuppressionStatus.SURVEY_SUPPRESSION_STATUS_NOT_SUPPRESSED,
  suppressionReason: '',
  campaignSnapshot: {},
  recipientSnapshot: { account: { key: 'acct:acme', name: 'Acme Corp' } },
  deliveryRetryable: false,
  publicUrl: '/surveys/token-1',
  expiresAt: '2026-08-13T00:00:00Z',
  createdAt: '2026-07-30T00:00:00Z',
  updatedAt: '2026-07-30T00:00:00Z',
}

export const sampleSurveyRecipientPreview: PreviewSurveyRecipientsResponse = {
  campaignId: sampleSurveyCampaign.id,
  triggerMatched: true,
  sampleIncluded: true,
  matchedCount: 2,
  eligibleCount: 1,
  suppressedCount: 1,
  deliveryReady: true,
  deliveryBlocker: '',
  recipients: [
    {
      sourceType: 'workflow_transition',
      sourceId: '101',
      contactId: '33333333-3333-3333-3333-333333333333',
      channel: 'email',
      displayName: 'Ada Lovelace',
      subjectDisplay: 'Ada',
      eligible: true,
      suppressionReason: '',
      recipientSnapshot: {},
      lastActivityAt: '2026-07-30T00:00:00Z',
    },
    {
      sourceType: 'workflow_transition',
      sourceId: '102',
      contactId: '44444444-4444-4444-4444-444444444444',
      channel: 'email',
      displayName: 'Grace Hopper',
      subjectDisplay: 'Grace',
      eligible: false,
      suppressionReason: 'contact_cooldown',
      recipientSnapshot: {},
      lastActivityAt: '2026-07-29T00:00:00Z',
    },
  ],
  suppressionReasonDistribution: [{ reason: 'contact_cooldown', count: 1 }],
}

export const sampleSurveyLowScoreReview: SurveyLowScoreReview = {
  responseId: 'survey-response-1',
  status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_OPEN,
  severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH,
  ownerMemberId: '',
  rootCause: '',
  actionTaken: '',
  customerContacted: false,
  dueAt: '2026-07-29T00:10:00Z',
  updatedBy: '',
  createdAt: '2026-07-30T00:10:00Z',
  updatedAt: '2026-07-30T00:10:00Z',
  slaStatus: SurveyRecoverySlaStatus.SURVEY_RECOVERY_SLA_STATUS_OVERDUE,
  blockerReason: 'overdue_sla',
  nextBestAction: 'resolve_overdue',
  riskScore: 95,
  recoveryNotificationStatus:
    SurveyRecoveryNotificationStatus.SURVEY_RECOVERY_NOTIFICATION_STATUS_UNSPECIFIED,
  recoveryNotificationReason: '',
  recoveryNotificationLastError: '',
}

export const sampleSurveyResponse: SurveyResponse = {
  id: sampleSurveyLowScoreReview.responseId,
  campaignId: sampleSurveyCampaign.id,
  invitationId: sampleSurveyInvitation.id,
  sourceType: 'feedback',
  sourceId: '101',
  score: 2,
  comment: 'The fix helped, but it took too many messages.',
  locale: 'zh-CN',
  lowScore: true,
  submittedAt: '2026-07-30T00:10:00Z',
  accountContext: {
    accountKey: 'acct:acme',
    accountDisplay: 'Acme Corp',
    source: 'recipient_snapshot',
  },
  lowScoreReview: sampleSurveyLowScoreReview,
}

export const defaultSurveyAnalytics: SurveyAnalytics = {
  invitationCount: 3,
  deliveredCount: 2,
  suppressedCount: 1,
  notStartedCount: 1,
  openedCount: 0,
  expiredCount: 0,
  pendingDeliveryCount: 1,
  delayedDeliveryCount: 0,
  rejectedDeliveryCount: 0,
  completedCount: 1,
  lowScoreCount: 1,
  positiveScoreCount: 1,
  openLowScoreReviewCount: 1,
  overdueLowScoreReviewCount: 1,
  unassignedLowScoreReviewCount: 1,
  criticalLowScoreReviewCount: 0,
  pendingCustomerContactReviewCount: 1,
  oldestOpenLowScoreReviewDueAt: '2026-07-30T01:00:00Z',
  overdueRecoveryQueueCount: 1,
  unassignedRecoveryQueueCount: 0,
  pendingContactRecoveryQueueCount: 0,
  missingRootCauseRecoveryQueueCount: 0,
  missingActionRecoveryQueueCount: 0,
  averageScore: 2,
  responseRate: 1 / 3,
  positiveScoreRate: 1,
  averageResponseSeconds: 7200,
  scoreDistribution: [{ score: 2, count: 1 }],
  suppressionReasonDistribution: [{ reason: 'contact_cooldown', count: 1 }],
  ownerRecoveryLoads: [
    {
      ownerMemberId: '22222222-2222-2222-2222-222222222222',
      openCount: 3,
      overdueCount: 1,
      dueSoonCount: 1,
      criticalCount: 1,
      pendingContactCount: 2,
      oldestOpenDueAt: '2026-07-30T01:00:00Z',
      workloadScore: 91,
    },
  ],
}

export const defaultSurveyAnalyticsTrend: SurveyAnalyticsTrendBucket[] = [
  {
    date: '2026-07-28',
    invitationCount: 2,
    deliveredCount: 2,
    suppressedCount: 0,
    completedCount: 1,
    lowScoreCount: 1,
    positiveScoreCount: 0,
    averageScore: 2,
    responseRate: 0.5,
    notStartedCount: 1,
    openedCount: 0,
    expiredCount: 0,
  },
  {
    date: '2026-07-29',
    invitationCount: 3,
    deliveredCount: 2,
    suppressedCount: 1,
    completedCount: 1,
    lowScoreCount: 0,
    positiveScoreCount: 1,
    averageScore: 5,
    responseRate: 1 / 3,
    notStartedCount: 1,
    openedCount: 1,
    expiredCount: 0,
  },
  {
    date: '2026-07-30',
    invitationCount: 3,
    deliveredCount: 2,
    suppressedCount: 1,
    completedCount: 1,
    lowScoreCount: 1,
    positiveScoreCount: 1,
    averageScore: 2,
    responseRate: 1 / 3,
    notStartedCount: 1,
    openedCount: 0,
    expiredCount: 0,
  },
]

export const defaultSurveyAnalyticsSegments: SurveyAnalyticsSegment[] = [
  {
    dimension: SurveyAnalyticsSegmentDimension.SURVEY_ANALYTICS_SEGMENT_DIMENSION_SOURCE_TYPE,
    key: 'feedback',
    label: 'feedback',
    invitationCount: 3,
    deliveredCount: 2,
    suppressedCount: 1,
    completedCount: 1,
    lowScoreCount: 1,
    positiveScoreCount: 0,
    expiredCount: 1,
    averageScore: 2,
    responseRate: 1 / 3,
    lowScoreRate: 1,
    positiveScoreRate: 0,
    suppressionRate: 1 / 3,
    averageResponseSeconds: 7200,
    attentionScore: 6,
  },
]

export const defaultSurveyAnalyticsInsights: SurveyAnalyticsInsight[] = [
  {
    id: 'survey-overdue-low-score-reviews',
    severity: SurveyAnalyticsInsightSeverity.SURVEY_ANALYTICS_INSIGHT_SEVERITY_CRITICAL,
    title: 'Low-score reviews are overdue',
    summary: 'Customer recovery is blocked by overdue low-score follow-up work.',
    metric: 'overdue_low_score_review_count',
    value: 1,
    threshold: 1,
    segmentDimension:
      SurveyAnalyticsSegmentDimension.SURVEY_ANALYTICS_SEGMENT_DIMENSION_UNSPECIFIED,
    recommendedAction: 'Assign owners and resolve overdue low-score reviews.',
    rank: 1,
  },
]

export const defaultSurveyCampaignHealth: SurveyCampaignHealth = {
  campaignId: sampleSurveyCampaign.id,
  status: SurveyCampaignHealthStatus.SURVEY_CAMPAIGN_HEALTH_STATUS_BLOCKED,
  readinessScore: 65,
  generatedAt: '2026-07-30T13:00:00Z',
  funnel: {
    invitationCount: 3,
    pendingCount: 1,
    delayedCount: 0,
    deliveredCount: 2,
    openedCount: 0,
    completedCount: 1,
    suppressedCount: 1,
    expiredCount: 0,
    rejectedCount: 0,
    lowScoreCount: 1,
    openLowScoreReviewCount: 1,
    overdueLowScoreReviewCount: 1,
    deliveryRate: 2 / 3,
    openRate: 0,
    responseRate: 1 / 3,
    suppressionRate: 1 / 3,
    expiredRate: 0,
    recoveryOverdueRate: 1,
  },
  checks: [
    {
      id: 'campaign-status',
      status: SurveyCampaignHealthCheckStatus.SURVEY_CAMPAIGN_HEALTH_CHECK_STATUS_PASS,
      title: 'Campaign is active',
      summary: 'The campaign can receive trigger events and create invitations.',
      recommendedAction: 'Keep campaign ownership and content current.',
      evidence: 'status=active',
    },
    {
      id: 'recovery-queue',
      status: SurveyCampaignHealthCheckStatus.SURVEY_CAMPAIGN_HEALTH_CHECK_STATUS_FAIL,
      title: 'Customer recovery is overdue',
      summary: 'Low-score responses have active follow-up work past its due date.',
      recommendedAction: 'Assign owners and resolve overdue low-score reviews.',
      evidence: 'overdue_reviews=1 open_reviews=1',
    },
  ],
  suppressionReasonDistribution: [{ reason: 'contact_cooldown', count: 1 }],
}

// Auth providers (login page SSO detection)
export const defaultAuthProviders = { providers: [{ type: 'local' }], oidc_only: false }
export const defaultAuthMode = { mode: 'hybrid' as const }
export const defaultBreakGlassTokens: ListTokensResponse = { tokens: [] }
export const defaultBreakGlassLockouts: ListLockoutsResponse = { lockouts: [] }
export const defaultBreakGlassRevokeAll: RevokeAllResponse = { revoked: 0 }

export const handlers = [
  http.get(`${BASE}/me`, () => HttpResponse.json(defaultMe)),
  http.get(`${BASE}/members`, () => HttpResponse.json(defaultMembersList)),
  http.get(`${BASE}/auth/providers`, () => HttpResponse.json(defaultAuthProviders)),
  http.get(`${BASE}/auth/sso/mode`, () => HttpResponse.json(defaultAuthMode)),
  http.post(`${BASE}/auth/sso/cutover`, () =>
    HttpResponse.json({ success: true, message: 'Switched to SSO-only mode' }),
  ),
  http.post(`${BASE}/auth/sso/fallback`, () =>
    HttpResponse.json({ success: true, message: 'Switched to hybrid mode' }),
  ),
  http.get(`${BASE}/auth/breakglass/tokens`, () => HttpResponse.json(defaultBreakGlassTokens)),
  http.post(`${BASE}/auth/breakglass/issue`, () =>
    HttpResponse.json({
      token: {
        id: 'bg-token-1',
        admin_email: 'admin@example.com',
        expires_at: '2026-07-06T12:00:00Z',
        issued_by: 'u-1',
        issued_at: '2026-07-06T11:30:00Z',
        status: 'valid',
        allowed_ips: [],
      },
      raw_token: 'bg_sample_token',
      expires_at: '2026-07-06T12:00:00Z',
    }),
  ),
  http.post(`${BASE}/auth/breakglass/tokens/revoke-all`, () =>
    HttpResponse.json(defaultBreakGlassRevokeAll),
  ),
  http.post(
    `${BASE}/auth/breakglass/tokens/:id/revoke`,
    () => new HttpResponse(null, { status: 204 }),
  ),
  http.get(`${BASE}/auth/breakglass/lockouts`, () => HttpResponse.json(defaultBreakGlassLockouts)),
  http.post(
    `${BASE}/auth/breakglass/lockouts/:ip/unlock`,
    () => new HttpResponse(null, { status: 204 }),
  ),

  http.get(`${BASE}/install/start`, ({ request }) => {
    const url = new URL(request.url)
    const redirect = url.searchParams.get('redirect_uri') ?? '/'
    return new HttpResponse(null, { status: 302, headers: { Location: redirect } })
  }),

  http.get(`${BASE}/api-keys`, () => HttpResponse.json(defaultApiKeysList)),
  http.get(`${BASE}/service-accounts`, () => HttpResponse.json(defaultServiceAccountsList)),
  http.get(`${BASE}/api-keys/scopes`, () => HttpResponse.json(defaultApiKeyScopes)),
  http.get(`${BASE}/api-keys/presets`, () => HttpResponse.json(defaultApiKeyPresets)),
  http.post(`${BASE}/api-keys`, () => HttpResponse.json(defaultCreateApiKey)),
  http.delete(`${BASE}/api-keys/:id`, () => new HttpResponse(null, { status: 204 })),

  http.get(`${BASE}/notify-targets`, () => HttpResponse.json(defaultNotifyTargetsList)),
  http.post(`${BASE}/notify-targets`, () => HttpResponse.json(sampleNotifyTarget)),
  http.patch(`${BASE}/notify-targets/:id`, () => HttpResponse.json(sampleNotifyTarget)),
  http.delete(`${BASE}/notify-targets/:id`, () => new HttpResponse(null, { status: 204 })),
  http.get(`${BASE}/digest-subscription`, () => HttpResponse.json(sampleDigestSubscription)),
  http.put(`${BASE}/digest-subscription`, () => HttpResponse.json(sampleDigestSubscription)),
  http.delete(`${BASE}/digest-subscription`, () => new HttpResponse(null, { status: 204 })),
  http.post(`${BASE}/notify-targets/:id/test`, () =>
    HttpResponse.json(defaultTestNotifyTargetResponse),
  ),

  http.get(`${BASE}/surveys/campaigns`, () =>
    HttpResponse.json({ campaigns: [sampleSurveyCampaign] }),
  ),
  http.post(`${BASE}/surveys/campaigns`, async ({ request }) => {
    const body = (await request.json()) as Partial<SurveyCampaign>
    return HttpResponse.json(
      {
        ...sampleSurveyCampaign,
        ...body,
        id: 'survey-campaign-new',
        contentVersion: 1,
        createdAt: '2026-07-30T01:00:00Z',
        updatedAt: '2026-07-30T01:00:00Z',
      },
      { status: 201 },
    )
  }),
  http.patch(`${BASE}/surveys/campaigns/:id`, async ({ params, request }) => {
    const body = (await request.json()) as Partial<SurveyCampaign>
    return HttpResponse.json({
      ...sampleSurveyCampaign,
      ...body,
      id: String(params.id),
      updatedAt: '2026-07-30T01:10:00Z',
    })
  }),
  http.post(`${BASE}/surveys/campaigns/:id\\:archive`, ({ params }) =>
    HttpResponse.json({
      ...sampleSurveyCampaign,
      id: String(params.id),
      status: SurveyCampaignStatus.SURVEY_CAMPAIGN_STATUS_ARCHIVED,
      archivedAt: '2026-07-30T01:15:00Z',
    }),
  ),
  http.post(`${BASE}/surveys/campaigns/:id/hosted-links`, ({ params }) =>
    HttpResponse.json(
      {
        ...sampleSurveyInvitation,
        id: 'survey-invitation-new',
        campaignId: String(params.id),
        publicUrl: '/surveys/token-new',
      },
      { status: 201 },
    ),
  ),
  http.post(`${BASE}/surveys/campaigns/:id/recipients:preview`, ({ params }) =>
    HttpResponse.json({
      ...sampleSurveyRecipientPreview,
      campaignId: String(params.id),
    }),
  ),
  http.post(`${BASE}/surveys/campaigns/:id\\:sendTestEmail`, () =>
    HttpResponse.json({
      ok: true,
      provider: 'postmark',
      sentAt: '2026-07-30T01:20:00Z',
    }),
  ),
  http.get(`${BASE}/surveys/campaigns/:id/health`, ({ params }) =>
    HttpResponse.json({
      ...defaultSurveyCampaignHealth,
      campaignId: String(params.id),
    }),
  ),
  http.get(`${BASE}/surveys/invitations`, () =>
    HttpResponse.json({ invitations: [sampleSurveyInvitation] }),
  ),
  http.get(`${BASE}/surveys/responses`, () =>
    HttpResponse.json({ responses: [sampleSurveyResponse] }),
  ),
  http.get(`${BASE}/surveys/analytics`, () => HttpResponse.json(defaultSurveyAnalytics)),
  http.get(`${BASE}/surveys/analytics/trend`, () =>
    HttpResponse.json({ buckets: defaultSurveyAnalyticsTrend }),
  ),
  http.get(`${BASE}/surveys/analytics/segments`, () =>
    HttpResponse.json({ segments: defaultSurveyAnalyticsSegments }),
  ),
  http.get(`${BASE}/surveys/analytics/insights`, () =>
    HttpResponse.json({ insights: defaultSurveyAnalyticsInsights }),
  ),
  http.patch(`${BASE}/surveys/responses/:id/low-score-review`, ({ params }) =>
    HttpResponse.json({
      ...sampleSurveyLowScoreReview,
      responseId: String(params.id),
      status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_RESOLVED,
      updatedAt: '2026-07-30T01:20:00Z',
    }),
  ),
  http.post(`${BASE}/surveys/responses/low-score-reviews:assign`, () =>
    HttpResponse.json({
      reviews: [sampleSurveyLowScoreReview],
      decisions: [
        {
          responseId: sampleSurveyLowScoreReview.responseId,
          ownerMemberId: '22222222-2222-2222-2222-222222222222',
          dueAt: '2026-07-31T09:00:00Z',
          severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH,
          escalated: false,
          reason: 'load_rebalance',
          workloadScoreBefore: 20,
          workloadScoreAfter: 43,
        },
      ],
    }),
  ),
  http.post(`${BASE}/surveys/responses/low-score-reviews:escalate`, () =>
    HttpResponse.json({
      reviews: [
        {
          ...sampleSurveyLowScoreReview,
          status: SurveyLowScoreReviewStatus.SURVEY_LOW_SCORE_REVIEW_STATUS_IN_REVIEW,
          severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
        },
      ],
      decisions: [
        {
          responseId: sampleSurveyLowScoreReview.responseId,
          previousSeverity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_HIGH,
          severity: SurveyLowScoreSeverity.SURVEY_LOW_SCORE_SEVERITY_CRITICAL,
          previousDueAt: '2026-07-30T01:00:00Z',
          dueAt: '2026-07-30T01:00:00Z',
          ownerMissing: false,
          dueAtChanged: false,
          reason: 'overdue_sla',
          actionTaken: 'Escalated recovery: reason=overdue_sla; severity=critical.',
        },
      ],
    }),
  ),

  http.get(`${BASE}/outbox/deliveries`, () => HttpResponse.json(defaultDeliveriesList)),
  http.post(`${BASE}/outbox/:id/retry`, () =>
    HttpResponse.json(defaultRetryDeliveryResponse, { status: 202 }),
  ),

  http.get(`${BASE}/external-sync/providers`, () =>
    HttpResponse.json(defaultExternalSyncProviders),
  ),
  http.get(`${BASE}/external-sync/provider-installations`, () =>
    HttpResponse.json(defaultExternalProviderInstallations),
  ),
  http.post(`${BASE}/external-sync/provider-installations`, async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>
    return HttpResponse.json({
      id: 'pi-new',
      tenantId: defaultMe.tenant?.id ?? 't-1',
      provider: String(body.provider ?? 'github'),
      displayName: String(body.displayName ?? 'GitHub App'),
      installationKind: String(body.installationKind ?? 'github_app'),
      status: 'pending',
      externalInstallationId: String(body.externalInstallationId ?? ''),
      accountLogin: String(body.accountLogin ?? ''),
      accountId: String(body.accountId ?? ''),
      accountUrl: String(body.accountUrl ?? ''),
      baseUrl: String(body.baseUrl ?? ''),
      permissionsJson: String(body.permissionsJson ?? '{}'),
      capabilityProfileJson: String(body.capabilityProfileJson ?? '{}'),
      resourceSelection: String(body.resourceSelection ?? 'none'),
      qualificationStatus: 'untested',
      lastQualifiedAt: '',
      lastError: '',
      createdBy: defaultMe.user?.openId ?? 'u-1',
      updatedBy: defaultMe.user?.openId ?? 'u-1',
      createdAt: '2026-07-08T01:00:00Z',
      updatedAt: '2026-07-08T01:00:00Z',
    })
  }),
  http.delete(
    `${BASE}/external-sync/provider-installations/:id`,
    () => new HttpResponse(null, { status: 204 }),
  ),
  http.post(
    /\/fb\/v1\/console\/external-sync\/provider-installations\/([^/]+):qualify$/,
    ({ request }) => {
      const installationId =
        new URL(request.url).pathname.match(
          /\/external-sync\/provider-installations\/([^/]+):qualify$/,
        )?.[1] ?? 'pi-1'
      return HttpResponse.json({
        installationId: decodeURIComponent(installationId),
        ready: false,
        grade: 'blocked',
        checks: [],
      })
    },
  ),
  http.get(`${BASE}/external-sync/provider-installations/:id/resources`, () =>
    HttpResponse.json(defaultExternalProviderInstallationResources),
  ),
  http.post(
    /\/fb\/v1\/console\/external-sync\/provider-installations\/[^/]+\/resources:select$/,
    () => HttpResponse.json(defaultSelectExternalProviderInstallationResources),
  ),

  http.get(`${BASE}/reply-send-hook`, () => HttpResponse.json(defaultReplySendHook)),
  http.put(`${BASE}/reply-send-hook`, () => HttpResponse.json(defaultReplySendHook)),
  http.delete(`${BASE}/reply-send-hook`, () =>
    HttpResponse.json({ ...defaultReplySendHook, enabled: false }),
  ),
  http.get(`${BASE}/reply-send-hook/deliveries`, () =>
    HttpResponse.json(defaultReplySendHookDeliveries),
  ),
  http.get(`${BASE}/reply-send-hook/health`, () => HttpResponse.json(defaultReplySendHookHealth)),
  http.post(`${BASE}/reply-send-hook/test`, () =>
    HttpResponse.json(acceptedReplySendHookDelivery()),
  ),
  http.post(`${BASE}/reply-send-hook/deliveries/:id/redeliver`, ({ params }) =>
    HttpResponse.json(
      acceptedReplySendHookDelivery({
        ...defaultReplySendHookDelivery,
        id: String(params.id),
      }),
    ),
  ),

  http.get(`${BASE}/llm/channels`, () => HttpResponse.json(defaultLLMChannelsList)),
  http.post(`${BASE}/llm/channels`, () => HttpResponse.json(sampleLLMChannel)),
  http.get(`${BASE}/llm/channels/:id`, ({ params }) =>
    HttpResponse.json({ ...sampleLLMChannel, id: String(params.id) }),
  ),
  http.patch(`${BASE}/llm/channels/:id`, ({ params }) =>
    HttpResponse.json({ ...sampleLLMChannel, id: String(params.id) }),
  ),
  http.delete(`${BASE}/llm/channels/:id`, () => new HttpResponse(null, { status: 204 })),
  http.post(`${BASE}/llm/channels/:id/test`, () =>
    HttpResponse.json(defaultTestLLMChannelResponse),
  ),
  http.get(`${BASE}/llm/channels/:id/models`, () => HttpResponse.json(defaultLLMModelsList)),
  http.get(`${BASE}/llm/channels/:id/abilities`, () => HttpResponse.json(defaultLLMAbilitiesList)),
  http.put(`${BASE}/llm/channels/:id/abilities`, () => HttpResponse.json(sampleLLMAbility)),
  http.post(
    `${BASE}/llm/channels/:id/abilities/delete`,
    () => new HttpResponse(null, { status: 204 }),
  ),
  http.get(`${BASE}/llm/routes`, () => HttpResponse.json(defaultLLMRoutesList)),
  http.put(`${BASE}/llm/routes`, () => HttpResponse.json(sampleLLMRoute)),
  http.post(`${BASE}/llm/routes/delete`, () => new HttpResponse(null, { status: 204 })),

  http.get(`${BASE}/enrich-config`, () => HttpResponse.json(defaultGetEnrichConfig)),
  http.get(`${BASE}/enrich-config/versions`, () =>
    HttpResponse.json({
      versions: defaultEnrichConfig.promptVersions,
    }),
  ),
  http.put(`${BASE}/enrich-config`, async ({ request }) => {
    // The PUT body is UpdateEnrichConfigRequest = { promptTemplate?, dimensions };
    // the response wraps it in { config } via UpdateEnrichConfigResponse.
    const body = (await request.json()) as {
      promptTemplate?: string
      dimensions?: unknown[]
      policyConfig?: EnrichConfig['policyConfig']
    }
    const resp: UpdateEnrichConfigResponse = {
      config: {
        promptTemplate: body.promptTemplate,
        defaultPromptTemplate: defaultEnrichConfig.defaultPromptTemplate,
        dimensions: (body.dimensions ?? []) as EnrichConfig['dimensions'],
        promptPolicy: defaultPromptPolicy(body.promptTemplate ? 'custom_template' : 'built_in'),
        promptVersions: [
          {
            id: '22222222-3333-4444-5555-666666666666',
            promptVersion: body.promptTemplate
              ? 'enrich.legacy_custom_template@sha256:mock'
              : 'enrich.default@1',
            promptFingerprint: body.promptTemplate
              ? 'sha256:mock-custom-prompt'
              : 'sha256:mock-default-prompt',
            schemaFingerprint: 'sha256:mock-schema',
            policyId: body.promptTemplate ? 'enrich.legacy_custom_template' : 'enrich.default',
            policyVersion: body.promptTemplate ? 'sha256:mock' : '1',
            mode: body.promptTemplate ? 'legacy_custom_override' : 'default',
            promptSource: body.promptTemplate ? 'custom_template' : 'built_in',
            createdAt: '2026-06-21T00:01:00Z',
            isActive: true,
            hasTemplate: Boolean(body.promptTemplate),
            dimensionsCount: body.dimensions?.length ?? 0,
            promptTemplate: body.promptTemplate,
            dimensions: (body.dimensions ?? []) as EnrichConfig['dimensions'],
            policyConfig: body.policyConfig,
            warnings: [],
          },
        ],
      },
    }
    return HttpResponse.json(resp)
  }),
  http.post(`${BASE}/enrich-config/versions/:id\\:activate`, ({ params }) =>
    HttpResponse.json({
      config: {
        ...defaultEnrichConfig,
        promptVersions: defaultEnrichConfig.promptVersions.map((version) => ({
          ...version,
          isActive: version.id === params.id,
        })),
      },
    }),
  ),
  http.post(`${BASE}/enrich-config/preview`, async ({ request }) => {
    const body = (await request.json()) as { promptTemplate?: string }
    return HttpResponse.json({
      ...defaultPreviewEnrichPrompt,
      promptPolicy: defaultPromptPolicy(body.promptTemplate ? 'custom_template' : 'built_in'),
    })
  }),
  http.post(`${BASE}/enrich-config/eval-suggestions\\:analyze`, () =>
    HttpResponse.json({
      coverage: {},
      candidates: [],
      recommendations: [],
    }),
  ),

  http.get(`${BASE}/feedback`, () => HttpResponse.json(defaultFeedbackList)),
  http.post(`${BASE}/feedback/search`, () => HttpResponse.json(defaultSemanticSearchResponse)),
  http.get(`${BASE}/feedback/search/quality`, () => HttpResponse.json(defaultSearchQuality)),
  http.post(`${BASE}/feedback/search/events`, () => HttpResponse.json({})),
  http.get(`${BASE}/feedback/stats`, () => HttpResponse.json(defaultFeedbackStats)),
  http.get(`${BASE}/request-notifications/status-evidence`, () =>
    HttpResponse.json(defaultRequestNotificationStatusEvidence),
  ),
  http.get(`${BASE}/feedback/triage-command-center`, () =>
    HttpResponse.json(defaultFeedbackTriageCommandCenter),
  ),
  http.get(`${BASE}/feedback/assignment/escalations`, () =>
    HttpResponse.json(defaultFeedbackAssignmentEscalations),
  ),
  http.get(`${BASE}/feedback/assignment/policy`, () =>
    HttpResponse.json(feedbackAssignmentPolicyState),
  ),
  http.put(`${BASE}/feedback/assignment/policy`, async ({ request }) => {
    const body = (await request.json()) as { rules?: FeedbackAssignmentPolicy['rules'] }
    feedbackAssignmentPolicyState = {
      version: feedbackAssignmentPolicyState.version + 1,
      updatedBy: 'admin-1',
      note: 'Policy updated from mock',
      rules: body.rules ?? [],
    }
    return HttpResponse.json(feedbackAssignmentPolicyState)
  }),
  http.get(`${BASE}/feedback/assignment/policy/revisions`, () =>
    HttpResponse.json({ revisions: [feedbackAssignmentPolicyState] }),
  ),
  http.post(`${BASE}/feedback/assignment/policy:dry-run`, async ({ request }) => {
    const body = (await request.json()) as {
      rules?: FeedbackAssignmentPolicy['rules']
      feedbackIds?: string[]
    }
    const draftRule = body.rules?.[0]
    const currentRule = feedbackAssignmentPolicyState.rules[0]
    return HttpResponse.json({
      totalMatched: body.feedbackIds?.length ?? 0,
      changed: draftRule && currentRule?.slaHours !== draftRule.slaHours ? 1 : 0,
      recommendations: [],
      failed: [],
      impacts: (body.feedbackIds ?? []).slice(0, 1).map((feedbackId) => ({
        feedbackId,
        currentRuleKey: currentRule?.ruleKey ?? '',
        currentRuleName: currentRule?.ruleName ?? '',
        currentOwnerLane: currentRule?.ownerLane ?? '',
        currentSlaHours: currentRule?.slaHours ?? 0,
        draftRuleKey: draftRule?.ruleKey ?? '',
        draftRuleName: draftRule?.ruleName ?? '',
        draftOwnerLane: draftRule?.ownerLane ?? '',
        draftSlaHours: draftRule?.slaHours ?? 0,
        changed: Boolean(draftRule && currentRule?.slaHours !== draftRule.slaHours),
      })),
    })
  }),
  http.post(`${BASE}/feedback/assignment/policy:restore`, async ({ request }) => {
    const body = (await request.json()) as { version?: number }
    feedbackAssignmentPolicyState = {
      ...defaultFeedbackAssignmentPolicy,
      version: feedbackAssignmentPolicyState.version + 1,
      updatedBy: 'admin-1',
      note: `Restored version ${body.version ?? 1}`,
    }
    return HttpResponse.json(feedbackAssignmentPolicyState)
  }),
  http.get(`${BASE}/feedback/identity-review`, () =>
    HttpResponse.json(defaultFeedbackIdentityReview),
  ),
  http.get(`${BASE}/feedback/identity-review/subjects/:subjectId`, () =>
    HttpResponse.json(defaultFeedbackIdentitySubjectDetail),
  ),
  http.post(`${BASE}/feedback/identity-review/merge`, () =>
    HttpResponse.json(defaultFeedbackIdentityMerge),
  ),
  http.post(`${BASE}/feedback/identity-review/split`, () =>
    HttpResponse.json(defaultFeedbackIdentitySplit),
  ),
  http.post(`${BASE}/feedback/assignment\\:batch`, async ({ request }) => {
    const body = (await request.json()) as { feedbackIds?: string[] }
    const count = body.feedbackIds?.length ?? 0
    return HttpResponse.json({ totalMatched: count, succeeded: count, failed: [] })
  }),
  http.post(`${BASE}/feedback/transition/batch`, async ({ request }) => {
    const body = (await request.json()) as { feedbackIds?: string[] }
    const count = body.feedbackIds?.length ?? 0
    return HttpResponse.json({ succeeded: count, failed: [] })
  }),
  http.post(`${BASE}/feedback/assignment\\:recommend`, async ({ request }) => {
    const body = (await request.json()) as { feedbackIds?: string[] }
    const ids = body.feedbackIds ?? []
    return HttpResponse.json({
      totalMatched: ids.length,
      recommendations: ids.map((feedbackId) => ({
        feedbackId,
        ruleKey: 'urgent_open',
        ruleName: 'Urgent open feedback',
        ownerLane: 'support_triage',
        severity: 'critical',
        slaHours: 24,
        recommendedSlaDueAt: '2026-08-02T09:30:00Z',
        rationale:
          'Urgent open feedback should be confirmed and assigned before the next business cycle.',
        alreadySatisfied: false,
      })),
      failed: [],
    })
  }),
  http.post(`${BASE}/feedback/assignment\\:apply-recommendations`, async ({ request }) => {
    const body = (await request.json()) as { feedbackIds?: string[] }
    const count = body.feedbackIds?.length ?? 0
    return HttpResponse.json({
      totalMatched: count,
      succeeded: count,
      skipped: 0,
      failed: [],
      applied: [],
    })
  }),
  http.get(`${BASE}/inbound/sources`, () => HttpResponse.json({ items: defaultInboundSources })),
  http.get(`${BASE}/feedback/:id/signal-trace`, ({ params }) =>
    HttpResponse.json({
      ...defaultFeedbackSignalTrace,
      feedbackId: String(params.id),
      signalTraceId: `trace-${String(params.id)}`,
    }),
  ),
  http.get(`${BASE}/feedback/:id`, ({ params }) =>
    HttpResponse.json({ ...defaultFeedbackDetail, id: params.id }),
  ),
  http.patch(`${BASE}/feedback/:id/assignment`, async ({ params, request }) => {
    const body = (await request.json()) as {
      ownerMemberId?: string
      slaDueAt?: string
      note?: string
    }
    const owner = defaultMembersList.members.find((member) => member.id === body.ownerMemberId)
    const assignment: FeedbackAssignment = {
      feedbackId: String(params.id),
      owner: owner
        ? {
            memberId: owner.id,
            memberType: owner.memberType,
            userId: owner.userId,
            email: owner.email,
            role: owner.role,
          }
        : undefined,
      assignedAt: body.ownerMemberId ? '2026-08-01T09:00:00Z' : undefined,
      assignedBy: 'u-1',
      slaDueAt: body.slaDueAt || undefined,
      slaStatus: body.slaDueAt ? 'on_track' : 'missing_due_date',
      note: body.note ?? '',
    }
    return HttpResponse.json(assignment)
  }),
  http.get(`${BASE}/workflow/states`, () => HttpResponse.json(defaultWorkflowStates)),
  http.get(`${BASE}/feedback/:id/audit`, () => HttpResponse.json(defaultWorkflowAudit)),
  http.get(`${BASE}/usage`, () => HttpResponse.json(defaultUsage)),
  http.get(`${BASE}/llm-usage`, () => HttpResponse.json(defaultLLMUsage)),
  http.get(`${BASE}/classification-quality`, () => HttpResponse.json(defaultClassificationQuality)),
  http.get(`${BASE}/classification-quality/review-learning`, () =>
    HttpResponse.json(defaultClassificationReviewLearning),
  ),
  http.post(`${BASE}/classification-quality/reviews`, async ({ request }) => {
    const body = (await request.json()) as {
      feedbackId?: string
      outcome?: string
      signalReason?: string
      correctionJson?: string
      note?: string
    }
    return HttpResponse.json({
      event: {
        eventId: 'classification-review-default',
        feedbackId: body.feedbackId ?? '0',
        outcome: body.outcome ?? 'accepted',
        signalReason: body.signalReason ?? '',
        correctionJson: body.correctionJson ?? '{}',
        note: body.note ?? '',
        reviewedAt: '2026-07-02T09:00:00Z',
      },
      learning: {
        ...defaultClassificationReviewLearning,
        totalReviews: '1',
        accepted: body.outcome === 'accepted' ? '1' : '0',
        edited: body.outcome === 'edited' ? '1' : '0',
        dismissed: body.outcome === 'dismissed' ? '1' : '0',
        trainingCandidateCount: body.outcome === 'accepted' ? '0' : '1',
      },
    })
  }),
]
