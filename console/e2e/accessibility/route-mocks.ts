import type { Page, Route } from '@playwright/test'
import type { ServiceAccount } from '../../src/proto/attune/v1/api_key'
import type { AuditLogViewState, SavedAuditLogView } from '../../src/proto/attune/v1/audit'
import {
  type CreateExternalConnectionRequest,
  type CreateExternalProviderInstallationRequest,
  type ExternalConnection,
  type ExternalObjectMapping,
  type ExternalObjectSchema,
  type ExternalProviderInstallation,
  type ExternalProviderInstallationResource,
  type ExternalProviderInstallationResourceInput,
  ExternalSyncDirection,
  type ExternalSyncEvent,
  ExternalSyncEventSignatureStatus,
  ExternalSyncEventStatus,
  type ExternalSyncHealthResponse,
  type ExternalSyncProvider,
  type ExternalSyncRun,
  ExternalSyncRunStatus,
  ExternalSyncRunTrigger,
} from '../../src/proto/attune/v1/external_sync'
import type {
  CreateInboundSourceResponse,
  DiscoverSlackChannelsResponse,
  InboundSource,
  SlackChannel,
  TestInboundConnectionResponse,
} from '../../src/proto/attune/v1/inbound_source'
import type {
  FeedbackAssignment,
  FeedbackAssignmentEscalationQueue,
  FeedbackAssignmentPolicy,
  FeedbackAssignmentPolicyRevision,
  FeedbackAssignmentPolicyRule,
  FeedbackAssignmentRecommendation,
  FeedbackDetail,
  FeedbackSignalTrace,
  ReplyDraftWorkflow,
  ReplySendHook,
  ReplySendHookDelivery,
  ReplySendHookHealth,
} from '../../src/proto/attune/v1/ingest'
import type { LLMChannel } from '../../src/proto/attune/v1/llm_config'
import type { QualityAction } from '../../src/proto/attune/v1/quality_action'
import type { Tag } from '../../src/proto/attune/v1/tag'
import {
  consoleA11yApiKeyPresets,
  consoleA11yApiKeyScopes,
  consoleA11yApiKeysList,
  consoleA11yAuditEntry,
  consoleA11yAuditEvidenceCreate,
  consoleA11yAuditEvidenceReady,
  consoleA11yAuditLog,
  consoleA11yAuthProviders,
  consoleA11yClassificationQuality,
  consoleA11yClassificationReviewLearning,
  consoleA11yCustomerRequestAccountSummary,
  consoleA11yCustomerRequestDetail,
  consoleA11yCustomerRequestsList,
  consoleA11yEnrichConfigResponse,
  consoleA11yFeedbackAssignmentEscalations,
  consoleA11yFeedbackDetail,
  consoleA11yFeedbackIdentityMerge,
  consoleA11yFeedbackIdentityReview,
  consoleA11yFeedbackIdentitySplit,
  consoleA11yFeedbackIdentitySubjectDetail,
  consoleA11yFeedbackItems,
  consoleA11yFeedbackSignalTrace,
  consoleA11yFeedbackStats,
  consoleA11yFeedbackTriageCommandCenter,
  consoleA11yGdprDelete,
  consoleA11yGdprExportReady,
  consoleA11yGdprExportStart,
  consoleA11yGdprOperations,
  consoleA11yGdprRequests,
  consoleA11yGdprRevoke,
  consoleA11yGdprStepUpVerified,
  consoleA11yIssuedApiKey,
  consoleA11yMcpClientDetail,
  consoleA11yMcpClientsList,
  consoleA11yMcpClientUpdate,
  consoleA11yMcpToolPolicyUpdate,
  consoleA11yMe,
  consoleA11yMembers,
  consoleA11yModerationSubjects,
  consoleA11yNotifyTargets,
  consoleA11yOutboxDeliveries,
  consoleA11yOutboxRetry,
  consoleA11yPortalFeedbackDetail,
  consoleA11yPublicVisibilityPolicy,
  consoleA11yQualityActions,
  consoleA11yReplyDraftWorkflow,
  consoleA11yReplySendHook,
  consoleA11yReplySendHookDeliveries,
  consoleA11yRetryEnrichmentResponse,
  consoleA11ySearchQuality,
  consoleA11yServiceAccount,
  consoleA11yServiceAccountsList,
  consoleA11ySurveyAnalytics,
  consoleA11ySurveyAnalyticsInsights,
  consoleA11ySurveyAnalyticsSegments,
  consoleA11ySurveyAnalyticsTrend,
  consoleA11ySurveyCampaign,
  consoleA11ySurveyCampaignHealth,
  consoleA11ySurveyInvitation,
  consoleA11ySurveyRecipientPreview,
  consoleA11ySurveyResponse,
  consoleA11yTagsResponse,
  consoleA11yTerminalFeedbackDetail,
  consoleA11yTerminalWorkbench,
  consoleA11yWorkflowAudit,
  consoleA11yWorkflowStatesResponse,
} from '../../src/testing/fixtures/console-a11y-fixtures'

const apiPrefix = '/fb/v1/console'

export type ApiMockDiagnostics = {
  inboundSourceRequests: Array<{
    body: unknown
    method: string
    path: string
  }>
  replyDraftRequests: Array<{
    body: unknown
    idempotencyKey?: string
    method: string
    path: string
  }>
  replySendHookRequests: Array<{
    body: unknown
    method: string
    path: string
  }>
  providerInstallationRequests: Array<{
    body: unknown
    method: string
    path: string
  }>
  customerRequestListRequests: string[]
  surveyRequests: Array<{
    body: unknown
    method: string
    path: string
  }>
  externalConnectionRequests: Array<{
    body: unknown
    method: string
    path: string
  }>
  unhandledRequests: string[]
  semanticSearchRequests: unknown[]
}

export type ApiMockFailure =
  | 'apiKeyCreate'
  | 'apiKeyRevoke'
  | 'feedbackRetryEnrichment'
  | 'mcpToolPolicies'
  | 'mcpSessionRevoke'
  | 'mcpGrantRevoke'
  | 'outboxRetry'

export type ApiMockOptions = {
  fail?: Partial<Record<ApiMockFailure, string>>
}

export async function installConsoleApiMocks(
  page: Page,
  options: ApiMockOptions = {},
): Promise<ApiMockDiagnostics> {
  const diagnostics: ApiMockDiagnostics = {
    externalConnectionRequests: [],
    inboundSourceRequests: [],
    providerInstallationRequests: [],
    replyDraftRequests: [],
    replySendHookRequests: [],
    customerRequestListRequests: [],
    surveyRequests: [],
    unhandledRequests: [],
    semanticSearchRequests: [],
  }
  const assignmentPolicy = createAssignmentPolicyState()
  const state: ApiMockState = {
    auditLogViews: [],
    feedbackDetails: {
      'feedback-101': feedbackDetailWithReplyDraft(
        clone(consoleA11yFeedbackDetail),
        consoleA11yReplyDraftWorkflow,
      ),
      'feedback-201': clone(consoleA11yTerminalFeedbackDetail),
      'feedback-301': clone(consoleA11yPortalFeedbackDetail),
    },
    inboundSources: createInboundSourcesState(),
    slackChannels: createSlackChannelsState(),
    replyDraftWorkflow: clone(consoleA11yReplyDraftWorkflow),
    replySendHook: clone(consoleA11yReplySendHook),
    replySendHookDeliveries: clone(consoleA11yReplySendHookDeliveries),
    externalSync: createExternalSyncState(),
    qualityActions: clone(consoleA11yQualityActions),
    serviceAccounts: clone(consoleA11yServiceAccountsList.items),
    assignmentPolicy,
    assignmentPolicyRevisions: [policyRevisionFromAssignmentPolicy(assignmentPolicy)],
    tags: clone(consoleA11yTagsResponse.tags),
  }

  await page.route('**/fb/v1/console/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const method = request.method()
    const path = url.pathname.slice(apiPrefix.length) || '/'

    if (await handleRoute(route, method, path, url, options, diagnostics, state)) {
      return
    }

    diagnostics.unhandledRequests.push(`${method} ${path}`)
    await fulfillJson(route, { message: `Unhandled Console API mock: ${method} ${path}` }, 501)
  })

  return diagnostics
}

async function handleRoute(
  route: Route,
  method: string,
  path: string,
  url: URL,
  options: ApiMockOptions,
  diagnostics: ApiMockDiagnostics,
  state: ApiMockState,
) {
  if (method === 'GET' && path === '/me') {
    await fulfillJson(route, consoleA11yMe)
    return true
  }
  if (method === 'GET' && path === '/auth/providers') {
    await fulfillJson(route, consoleA11yAuthProviders)
    return true
  }
  if (method === 'GET' && path === '/auth/sso/mode') {
    await fulfillJson(route, { mode: 'hybrid' })
    return true
  }
  if (method === 'GET' && path === '/auth/breakglass/tokens') {
    await fulfillJson(route, {
      tokens: [
        {
          id: 'bg-token-a11y',
          admin_email: 'admin@example.com',
          expires_at: '2099-07-06T12:30:00Z',
          issued_by: 'user-a11y',
          issued_at: '2026-07-05T11:30:00Z',
          status: 'valid',
          allowed_ips: ['203.0.113.0/24'],
        },
      ],
    })
    return true
  }
  if (method === 'GET' && path === '/auth/breakglass/lockouts') {
    await fulfillJson(route, { lockouts: [] })
    return true
  }

  if (method === 'GET' && path === '/enrich-config') {
    await fulfillJson(route, consoleA11yEnrichConfigResponse)
    return true
  }
  if (method === 'GET' && path === '/classification-quality') {
    await fulfillJson(route, consoleA11yClassificationQuality)
    return true
  }
  if (method === 'GET' && path === '/classification-quality/review-learning') {
    await fulfillJson(route, consoleA11yClassificationReviewLearning)
    return true
  }
  if (method === 'POST' && path === '/classification-quality/reviews') {
    await fulfillJson(route, {
      event: {
        eventId: 'classification-review-e2e',
        feedbackId: '101',
        outcome: 'accepted',
        signalReason: 'low_confidence_rate_spike',
        correctionJson: '{}',
        reviewedAt: '2026-07-30T09:10:00Z',
      },
      learning: consoleA11yClassificationReviewLearning,
    })
    return true
  }
  if (method === 'GET' && path === '/tags') {
    await fulfillJson(route, { tags: state.tags })
    return true
  }
  if (method === 'GET' && path === '/members') {
    await fulfillJson(route, { members: consoleA11yMembers })
    return true
  }
  if (method === 'GET' && path === '/notify-targets') {
    await fulfillJson(route, consoleA11yNotifyTargets)
    return true
  }
  if (method === 'GET' && path === '/clusters') {
    await fulfillJson(route, { items: [], clusteringEnabled: true, totalCount: 0 })
    return true
  }
  if (method === 'GET' && path.startsWith('/clusters/') && path.endsWith('/members')) {
    await fulfillJson(route, { items: [], clusterLabel: '', totalCount: 0 })
    return true
  }
  if (method === 'GET' && path === '/workflow/states') {
    await fulfillJson(route, consoleA11yWorkflowStatesResponse)
    return true
  }
  if (method === 'GET' && path === '/surveys/campaigns') {
    await fulfillJson(route, { campaigns: [consoleA11ySurveyCampaign] })
    return true
  }
  if (method === 'GET' && path === '/surveys/analytics') {
    await fulfillJson(route, consoleA11ySurveyAnalytics)
    return true
  }
  if (method === 'GET' && path === '/surveys/analytics/trend') {
    await fulfillJson(route, { buckets: consoleA11ySurveyAnalyticsTrend })
    return true
  }
  if (method === 'GET' && path === '/surveys/analytics/segments') {
    await fulfillJson(route, { segments: consoleA11ySurveyAnalyticsSegments })
    return true
  }
  if (method === 'GET' && path === '/surveys/analytics/insights') {
    await fulfillJson(route, { insights: consoleA11ySurveyAnalyticsInsights })
    return true
  }
  if (method === 'GET' && path.startsWith('/surveys/campaigns/') && path.endsWith('/health')) {
    await fulfillJson(route, consoleA11ySurveyCampaignHealth)
    return true
  }
  if (method === 'GET' && path === '/surveys/invitations') {
    await fulfillJson(route, { invitations: [consoleA11ySurveyInvitation] })
    return true
  }
  if (
    method === 'GET' &&
    path.match(/^\/surveys\/campaigns\/[^/]+\/nps-runs\/[^/]+\/evidence-exports$/)
  ) {
    await fulfillJson(route, { exports: [] })
    return true
  }
  if (method === 'POST' && path.endsWith('/recipients:preview')) {
    const body = readJsonBody(route)
    diagnostics.surveyRequests.push({ method, path, body })
    await fulfillJson(route, consoleA11ySurveyRecipientPreview)
    return true
  }
  if (method === 'POST' && path.endsWith(':sendTestEmail')) {
    const body = readJsonBody(route)
    diagnostics.surveyRequests.push({ method, path, body })
    await fulfillJson(route, {
      ok: true,
      provider: 'postmark',
      sentAt: '2026-07-30T01:20:00Z',
    })
    return true
  }
  if (method === 'GET' && path === '/surveys/responses') {
    const accountKey = url.searchParams.get('account_key')?.trim()
    const responses =
      accountKey && accountKey !== consoleA11ySurveyResponse.accountContext?.accountKey
        ? []
        : [consoleA11ySurveyResponse]
    await fulfillJson(route, { responses })
    return true
  }
  if (method === 'GET' && path === '/public-visibility/policy') {
    await fulfillJson(route, consoleA11yPublicVisibilityPolicy)
    return true
  }
  if (method === 'GET' && path === '/public-visibility/moderation') {
    await fulfillJson(route, consoleA11yModerationSubjects)
    return true
  }
  if (method === 'GET' && path === '/public-visibility/views') {
    await fulfillJson(route, { views: [] })
    return true
  }
  if (method === 'GET' && path === '/request-notifications/settings') {
    await fulfillJson(route, {
      tenantId: consoleA11yMe.tenant.id,
      emailEnabled: true,
      webhookEnabled: true,
      enabledEventTypes: {
        'request.status_changed': true,
        'request.shipped': true,
      },
      statusPolicy: {
        open: true,
        planned: true,
        in_progress: true,
        shipped: true,
        cancelled: false,
      },
      defaultConsentMode: 'explicit_opt_in',
      requirePublicUpdateForStatus: true,
      maxRecipientsWithoutConfirm: 100,
      tenantHourlySendLimit: 1000,
      contactDailySendLimit: 10,
      updatedBy: consoleA11yMe.user.openId,
      createdAt: '2026-07-16T00:00:00Z',
      updatedAt: '2026-07-16T00:00:00Z',
    })
    return true
  }
  if (method === 'GET' && path === '/request-notifications/sender') {
    await fulfillJson(route, {
      id: 'rn-sender-a11y',
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
    })
    return true
  }
  if (method === 'GET' && path === '/request-notifications/webhook-targets') {
    await fulfillJson(route, {
      targets: [
        {
          id: 'rn-target-a11y',
          name: 'Customer CRM',
          url: 'https://hooks.example.test/request-notifications',
          urlHost: 'hooks.example.test',
          signatureVersion: 'v1',
          eventMask: { 'request.shipped': true },
          includeRecipientIdentity: true,
          status: 'active',
          createdAt: '2026-07-16T00:00:00Z',
          updatedAt: '2026-07-16T00:00:00Z',
        },
      ],
    })
    return true
  }
  if (method === 'GET' && path === '/request-notifications/deliveries') {
    await fulfillJson(route, {
      deliveries: [
        {
          id: 'rn-delivery-a11y',
          eventId: 'rn-event-a11y',
          channel: 'REQUEST_NOTIFICATION_CHANNEL_EMAIL',
          status: 'failed',
          attempts: 2,
          lastError: 'temporary provider outage',
          failureKind: 'provider_5xx',
          deadReason: '',
          retriedBy: '',
          traceId: 'trace-rn-a11y',
          destinationHash: 'sha256:rn-a11y',
          createdAt: '2026-07-16T00:00:00Z',
          manualRetryCount: 0,
        },
      ],
    })
    return true
  }
  if (method === 'GET' && path === '/request-notifications/status-evidence') {
    await fulfillJson(route, {
      items: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 4,
          notifiedCustomers: 2,
          failedCustomers: 1,
          suppressedCustomers: 1,
          recoveryPendingCustomers: 1,
          eventCount: 2,
          lastEventAt: '2026-07-16T00:00:00Z',
        },
      ],
    })
    return true
  }
  if (method === 'POST' && path === '/request-notifications:batch-preview') {
    await fulfillJson(route, {
      totalMatched: 2,
      eligibleRecipients: 4,
      excludedRecipients: 1,
      items: [
        {
          requestId: '11111111-1111-1111-1111-111111111111',
          eligibleRecipients: 4,
          excludedRecipients: 1,
        },
      ],
      failed: [{ requestId: 'bad-request', code: 'validation', message: 'invalid request id' }],
    })
    return true
  }
  if (method === 'POST' && path === '/request-notifications:batch-publish') {
    await fulfillJson(
      route,
      {
        totalMatched: 2,
        succeeded: 1,
        skipped: 0,
        events: [{ id: 'rn-event-batch-a11y', status: 'pending' }],
        failed: [{ requestId: 'bad-request', code: 'validation', message: 'invalid request id' }],
      },
      201,
    )
    return true
  }
  if (method === 'GET' && path === '/inbound/sources') {
    await fulfillJson(route, { items: sortInboundSources(state.inboundSources) })
    return true
  }
  if (method === 'GET' && path.match(/^\/inbound\/sources\/[^/]+\/recent$/)) {
    await fulfillJson(route, { items: [] })
    return true
  }
  if (method === 'GET' && path.match(/^\/feedback\/\d+\/similar$/)) {
    await fulfillJson(route, { items: [], anchor_linked_requests: [] })
    return true
  }
  if (method === 'POST' && path.match(/^\/inbound\/sources\/[^/]+\/sync-now$/)) {
    const id = path.split('/')[3]
    const source = state.inboundSources.find((item) => item.id === id)
    await fulfillJson(route, source ?? {})
    return true
  }
  if (method === 'GET' && path.match(/^\/inbound\/sources\/[^/]+$/)) {
    const id = path.split('/')[3]
    const source = state.inboundSources.find((item) => item.id === id)
    if (!source) {
      await fulfillError(route, `Missing inbound source ${id}`, 404)
      return true
    }
    await fulfillJson(route, clone(source))
    return true
  }
  if (method === 'POST' && path === '/inbound/sources/test-connection') {
    const body = readJsonBody(route) as {
      channel?: string
      emailConfig?: { host?: string }
      slackConfig?: { botToken?: string; channelId?: string }
    } | null
    diagnostics.inboundSourceRequests.push({ method, path, body })
    await fulfillJson(route, buildTestInboundSourceConnectionResponse(state, body))
    return true
  }
  if (method === 'POST' && path === '/inbound/sources/slack/discover') {
    const body = readJsonBody(route) as {
      slackConfig?: { botToken?: string; channelId?: string }
    } | null
    diagnostics.inboundSourceRequests.push({ method, path, body })
    await fulfillJson(route, buildDiscoverSlackChannelsResponse(state, body))
    return true
  }
  if (method === 'POST' && path === '/inbound/sources') {
    const body = readJsonBody(route) as {
      channel?: string
      emailConfig?: Record<string, unknown>
      name?: string
      slackConfig?: { botToken?: string; channelId?: string }
      webhookConfig?: Record<string, never>
    } | null
    diagnostics.inboundSourceRequests.push({ method, path, body })
    const created = buildCreateInboundSourceResponse(state, body)
    if (!created) {
      await fulfillError(route, 'Unsupported inbound source payload', 400)
      return true
    }
    await fulfillJson(route, created, 201)
    return true
  }
  if (method === 'POST' && path.match(/^\/inbound\/sources\/[^/]+\/pause$/)) {
    const id = path.split('/')[3]
    const source = updateInboundSource(state, id, { enabled: false })
    await fulfillJson(route, source)
    return true
  }
  if (method === 'POST' && path.match(/^\/inbound\/sources\/[^/]+\/resume$/)) {
    const id = path.split('/')[3]
    const source = updateInboundSource(state, id, { enabled: true })
    await fulfillJson(route, source)
    return true
  }
  if (method === 'DELETE' && path.match(/^\/inbound\/sources\/[^/]+$/)) {
    const id = path.split('/')[3]
    state.inboundSources = state.inboundSources.filter((source) => source.id !== id)
    await route.fulfill({ status: 204 })
    return true
  }
  if (method === 'POST' && path.match(/^\/inbound\/sources\/[^/]+\/rotate-secret$/)) {
    const id = path.split('/')[3]
    const source = state.inboundSources.find((item) => item.id === id)
    if (!source || source.channel !== 'webhook') {
      await fulfillError(route, 'rotation only supports webhook sources', 400)
      return true
    }
    await fulfillJson(route, {
      secretHex: 'deadbeefcafebabefeedface0000000000000000000000000000000000000000',
      nextEligibleAt: '2026-07-13T00:00:00Z',
    })
    return true
  }

  // Cohort sync: feedback page loads cohort list for filter dropdown (#233)
  if (method === 'GET' && path === '/cohort-sync/cohorts') {
    await fulfillJson(route, { cohorts: [] })
    return true
  }
  if (method === 'GET' && path === '/cohort-sync/health') {
    await fulfillJson(route, {
      source_count: 0,
      active_sources: 0,
      error_sources: 0,
      cohort_count: 0,
      total_active_members: 0,
    })
    return true
  }
  if (method === 'GET' && path === '/cohort-sync/sources') {
    await fulfillJson(route, { sources: [] })
    return true
  }
  if (method === 'GET' && path.startsWith('/cohort-sync/sources/')) {
    await fulfillJson(route, { id: '', provider: '', name: '', status: 'active' })
    return true
  }
  if (method === 'GET' && path.match(/\/cohort-sync\/cohorts\/[^/]+$/)) {
    await fulfillJson(route, { id: '', name: '', member_count: 0 })
    return true
  }
  if (method === 'GET' && path.match(/\/cohort-sync\/cohorts\/[^/]+\/members$/)) {
    await fulfillJson(route, { members: [] })
    return true
  }
  if (method === 'GET' && path.match(/\/cohort-sync\/sources\/[^/]+\/events$/)) {
    await fulfillJson(route, { events: [] })
    return true
  }
  if (method === 'GET' && path.match(/\/cohort-sync\/cohorts\/[^/]+\/runs$/)) {
    await fulfillJson(route, { runs: [] })
    return true
  }

  if (method === 'GET' && path === '/feedback') {
    const terminalOnly = url.searchParams.get('terminal_failed_only') === 'true'
    const source = url.searchParams.get('source')
    const accountKey = url.searchParams.get('account_key')?.trim()
    await fulfillJson(route, feedbackListResponse(state, terminalOnly, source, accountKey))
    return true
  }
  if (method === 'GET' && path === '/feedback/stats') {
    await fulfillJson(route, consoleA11yFeedbackStats)
    return true
  }
  if (method === 'GET' && path === '/usage') {
    await fulfillJson(route, {
      periodStart: '2026-07-01T00:00:00Z',
      periodEnd: '2026-07-31T23:59:59Z',
      total: '72',
      quota: '100',
      series: [{ bucket: '2026-07-01T00:00:00Z', value: '72' }],
    })
    return true
  }
  if (method === 'GET' && path === '/llm-usage') {
    await fulfillJson(route, {
      periodStart: '2026-07-01T00:00:00Z',
      periodEnd: '2026-07-31T23:59:59Z',
      granularity: 'week',
      series: [],
      promptTokens: '12000',
      completionTokens: '4000',
      costUsd: 2.34,
      calls: '20',
      errors: '1',
    })
    return true
  }
  if (method === 'GET' && path === '/feedback/triage-command-center') {
    await fulfillJson(route, consoleA11yFeedbackTriageCommandCenter)
    return true
  }
  if (method === 'GET' && path === '/feedback/assignment/escalations') {
    await fulfillJson(route, assignmentEscalationQueue(state))
    return true
  }
  if (method === 'GET' && path === '/feedback/identity-review') {
    await fulfillJson(route, consoleA11yFeedbackIdentityReview)
    return true
  }
  if (method === 'GET' && path.startsWith('/feedback/identity-review/subjects/')) {
    await fulfillJson(route, consoleA11yFeedbackIdentitySubjectDetail)
    return true
  }
  if (method === 'POST' && path === '/feedback/identity-review/merge') {
    await fulfillJson(route, consoleA11yFeedbackIdentityMerge)
    return true
  }
  if (method === 'POST' && path === '/feedback/identity-review/split') {
    await fulfillJson(route, consoleA11yFeedbackIdentitySplit)
    return true
  }
  if (method === 'POST' && path === '/feedback/search') {
    const body = readJsonBody(route)
    diagnostics.semanticSearchRequests.push(body)
    await fulfillJson(route, semanticSearchResponse(body))
    return true
  }
  if (method === 'GET' && path === '/feedback/search/quality') {
    await fulfillJson(route, consoleA11ySearchQuality)
    return true
  }
  if (method === 'GET' && path === '/quality-actions') {
    await fulfillJson(route, { actions: clone(state.qualityActions) })
    return true
  }
  if (method === 'POST' && path === '/quality-actions/update') {
    const body = readJsonBody(route) as Partial<QualityAction> | null
    const actionKey = body?.actionKey?.trim()
    if (!actionKey) {
      await fulfillError(route, 'quality action key is required', 400)
      return true
    }
    const existing = state.qualityActions.find((action) => action.actionKey === actionKey)
    const now = '2026-07-30T09:10:00Z'
    const status = body?.status ?? existing?.status ?? 'open'
    const updated: QualityAction = {
      actionId: existing?.actionId ?? `qa-action-${state.qualityActions.length + 1}`,
      actionKey,
      signal: body?.signal ?? existing?.signal ?? actionKey,
      status,
      severity: body?.severity ?? existing?.severity ?? 'watch',
      targetPath: body?.targetPath ?? existing?.targetPath ?? '/control-tower',
      metricLabel: body?.metricLabel ?? existing?.metricLabel ?? '',
      metricValue: body?.metricValue ?? existing?.metricValue ?? '',
      recommendationKey: body?.recommendationKey ?? existing?.recommendationKey ?? '',
      evidenceJson: body?.evidenceJson ?? existing?.evidenceJson ?? '{}',
      createdAt: existing?.createdAt ?? now,
      lastSeenAt: now,
      acknowledgedAt: status === 'acknowledged' ? now : (existing?.acknowledgedAt ?? ''),
      resolvedAt: status === 'resolved' ? now : (existing?.resolvedAt ?? ''),
      dismissedAt: status === 'dismissed' ? now : (existing?.dismissedAt ?? ''),
      updatedAt: now,
      updatedBy: consoleA11yMe.user.openId,
    }
    state.qualityActions = [
      updated,
      ...state.qualityActions.filter((action) => action.actionKey !== actionKey),
    ]
    await fulfillJson(route, { action: clone(updated) })
    return true
  }
  if (method === 'GET' && path === '/feedback/terminal-failures') {
    await fulfillJson(route, consoleA11yTerminalWorkbench)
    return true
  }
  if (method === 'GET' && path.match(/^\/feedback\/[^/]+\/audit$/)) {
    await fulfillJson(route, consoleA11yWorkflowAudit)
    return true
  }
  if (method === 'POST' && path.match(/^\/feedback\/[^/]+\/tags$/)) {
    const body = readJsonBody(route) as {
      feedbackId?: string
      tagId?: string
      tagName?: string
      tagColor?: string
    } | null
    const feedbackId = path.split('/')[2]
    const tag = resolveFeedbackTag(state, body)
    if (!tag) {
      await fulfillError(route, 'Tag not found for accessibility gate', 404)
      return true
    }
    const detail = state.feedbackDetails[feedbackId]
    if (detail) {
      detail.tags = upsertTag(detail.tags ?? [], tag)
    }
    await fulfillJson(route, { tag }, 201)
    return true
  }
  if (method === 'DELETE' && path.match(/^\/feedback\/[^/]+\/tags\/[^/]+$/)) {
    const feedbackId = path.split('/')[2]
    const tagId = path.split('/')[4]
    const detail = state.feedbackDetails[feedbackId]
    if (detail) {
      detail.tags = (detail.tags ?? []).filter((tag) => tag.id !== tagId)
    }
    await route.fulfill({ status: 204 })
    return true
  }
  if (method === 'POST' && path === '/feedback/assignment:batch') {
    const body = readJsonBody(route) as {
      feedbackIds?: string[]
      ownerMemberIdSet?: boolean
      ownerMemberId?: string
      slaDueAtSet?: boolean
      slaDueAt?: string
      note?: string
    } | null
    const ids = body?.feedbackIds ?? []
    for (const feedbackId of ids) {
      const detail = state.feedbackDetails[feedbackId]
      if (!detail) continue
      const owner = consoleA11yMembers.find((member) => member.id === body?.ownerMemberId)
      const current = detail.assignment
      detail.assignment = {
        feedbackId,
        owner: body?.ownerMemberIdSet ? assignmentOwner(owner) : current?.owner,
        assignedAt: body?.ownerMemberIdSet ? '2026-06-24T09:15:00Z' : current?.assignedAt,
        assignedBy: 'user-a11y',
        slaDueAt: body?.slaDueAtSet ? body.slaDueAt || undefined : current?.slaDueAt,
        slaStatus: body?.slaDueAtSet && !body.slaDueAt ? 'missing_due_date' : 'on_track',
        note: body?.note ?? current?.note ?? '',
      }
    }
    await fulfillJson(route, {
      totalMatched: ids.length,
      succeeded: ids.length,
      failed: [],
    })
    return true
  }
  if (method === 'POST' && path === '/feedback/transition/batch') {
    const body = readJsonBody(route) as {
      feedbackIds?: string[]
      toStateId?: string
    } | null
    const target = consoleA11yWorkflowStatesResponse.states.find(
      (workflowState) => workflowState.id === body?.toStateId,
    )
    const ids = body?.feedbackIds ?? []
    for (const feedbackId of ids) {
      const detail = state.feedbackDetails[feedbackId]
      if (!detail || !target) continue
      detail.workflowState = target
      detail.allowedNextStates = consoleA11yWorkflowStatesResponse.states.filter(
        (workflowState) => workflowState.id !== target.id,
      )
    }
    await fulfillJson(route, {
      succeeded: target ? ids.length : 0,
      failed: [],
    })
    return true
  }
  if (method === 'POST' && path === '/feedback/assignment:recommend') {
    const body = readJsonBody(route) as { feedbackIds?: string[] } | null
    const ids = body?.feedbackIds ?? []
    const recommendations = ids.flatMap((feedbackId) => {
      const detail = state.feedbackDetails[feedbackId]
      const recommendation = detail
        ? assignmentRecommendation(detail, state.assignmentPolicy)
        : null
      return recommendation ? [recommendation] : []
    })
    await fulfillJson(route, {
      totalMatched: ids.length,
      recommendations,
      failed: ids
        .filter((feedbackId) => !state.feedbackDetails[feedbackId])
        .map((feedbackId) => ({
          feedbackId,
          code: 'NOT_FOUND',
          message: 'feedback not found',
        })),
    })
    return true
  }
  if (method === 'GET' && path === '/feedback/assignment/policy') {
    await fulfillJson(route, state.assignmentPolicy)
    return true
  }
  if (method === 'PUT' && path === '/feedback/assignment/policy') {
    const body = readJsonBody(route) as {
      rules?: FeedbackAssignmentPolicyRule[]
      note?: string
    } | null
    state.assignmentPolicy = {
      rules: body?.rules ?? [],
      version: state.assignmentPolicy.version + 1,
      updatedBy: 'admin@example.com',
      note: body?.note ?? '',
    }
    state.assignmentPolicyRevisions = [
      policyRevisionFromAssignmentPolicy(state.assignmentPolicy),
      ...state.assignmentPolicyRevisions,
    ].slice(0, 20)
    await fulfillJson(route, state.assignmentPolicy)
    return true
  }
  if (method === 'GET' && path === '/feedback/assignment/policy/revisions') {
    await fulfillJson(route, { revisions: state.assignmentPolicyRevisions })
    return true
  }
  if (method === 'POST' && path === '/feedback/assignment/policy:dry-run') {
    const body = readJsonBody(route) as {
      feedbackIds?: string[]
      rules?: FeedbackAssignmentPolicyRule[]
    } | null
    const ids = body?.feedbackIds ?? []
    const draftPolicy: FeedbackAssignmentPolicy = {
      rules: body?.rules ?? [],
      version: state.assignmentPolicy.version + 1,
      updatedBy: 'preview',
      note: '',
    }
    const impacts = ids.flatMap((feedbackId) => {
      const detail = state.feedbackDetails[feedbackId]
      if (!detail) return []
      const current = assignmentRecommendation(detail, state.assignmentPolicy)
      const draft = assignmentRecommendation(detail, draftPolicy)
      if (!current && !draft) return []
      return [assignmentPolicyDryRunImpact(feedbackId, current, draft)]
    })
    const changed = impacts.filter((impact) => impact.changed).length
    await fulfillJson(route, {
      totalMatched: ids.length,
      changed,
      recommendations: ids.flatMap((feedbackId) => {
        const detail = state.feedbackDetails[feedbackId]
        if (!detail) return []
        const recommendation = assignmentRecommendation(detail, draftPolicy)
        return recommendation ? [recommendation] : []
      }),
      failed: ids
        .filter((feedbackId) => !state.feedbackDetails[feedbackId])
        .map((feedbackId) => ({
          feedbackId,
          code: 'NOT_FOUND',
          message: 'feedback not found',
        })),
      impacts,
    })
    return true
  }
  if (method === 'POST' && path === '/feedback/assignment/policy:restore') {
    const body = readJsonBody(route) as { version?: number } | null
    const revision = state.assignmentPolicyRevisions.find((item) => item.version === body?.version)
    const restoredRules = revision?.rules ?? createAssignmentPolicyState().rules
    state.assignmentPolicy = {
      rules: restoredRules,
      version: state.assignmentPolicy.version + 1,
      updatedBy: 'admin@example.com',
      note: `Restored feedback assignment policy version ${body?.version ?? 1}`,
    }
    state.assignmentPolicyRevisions = [
      policyRevisionFromAssignmentPolicy(state.assignmentPolicy),
      ...state.assignmentPolicyRevisions,
    ].slice(0, 20)
    await fulfillJson(route, state.assignmentPolicy)
    return true
  }
  if (method === 'POST' && path === '/feedback/assignment:apply-recommendations') {
    const body = readJsonBody(route) as {
      feedbackIds?: string[]
      ownerMemberId?: string
      note?: string
    } | null
    const ids = body?.feedbackIds ?? []
    const applied: FeedbackAssignmentRecommendation[] = []
    for (const feedbackId of ids) {
      const detail = state.feedbackDetails[feedbackId]
      if (!detail) continue
      const recommendation = assignmentRecommendation(detail, state.assignmentPolicy)
      if (!recommendation) continue
      applied.push(recommendation)
      const defaultOwner = recommendation.recommendedOwnerMemberId
        ? consoleA11yMembers.find((member) => member.id === recommendation.recommendedOwnerMemberId)
        : undefined
      const owner = body?.ownerMemberId
        ? consoleA11yMembers.find((member) => member.id === body.ownerMemberId)
        : defaultOwner
      detail.assignment = {
        feedbackId,
        owner: owner ? assignmentOwner(owner) : detail.assignment?.owner,
        assignedAt: owner ? '2026-06-24T09:15:00Z' : detail.assignment?.assignedAt,
        assignedBy: 'user-a11y',
        slaDueAt: recommendation.recommendedSlaDueAt,
        slaStatus: 'on_track',
        note: `Assignment policy: ${recommendation.ruleName} (${recommendation.ownerLane}, ${recommendation.slaHours}h). ${body?.note ?? ''}`.trim(),
      }
    }
    await fulfillJson(route, {
      totalMatched: ids.length,
      succeeded: applied.length,
      skipped: 0,
      failed: [],
      applied,
    })
    return true
  }
  if (method === 'PATCH' && path.match(/^\/feedback\/[^/]+\/assignment$/)) {
    const feedbackId = path.split('/')[2]
    const body = readJsonBody(route) as {
      ownerMemberId?: string
      slaDueAt?: string
      note?: string
    } | null
    const owner = consoleA11yMembers.find((member) => member.id === body?.ownerMemberId)
    const assignment: FeedbackAssignment = {
      feedbackId,
      owner: owner
        ? {
            memberId: owner.id,
            memberType: owner.memberType,
            userId: owner.userId,
            email: owner.email,
            role: owner.role,
          }
        : undefined,
      assignedAt: owner ? '2026-06-24T09:10:00Z' : undefined,
      assignedBy: 'user-a11y',
      slaDueAt: body?.slaDueAt || undefined,
      slaStatus: body?.slaDueAt ? 'on_track' : 'missing_due_date',
      note: body?.note ?? '',
    }
    const detail = state.feedbackDetails[feedbackId]
    if (detail) {
      detail.assignment = assignment
    }
    await fulfillJson(route, assignment)
    return true
  }
  if (method === 'POST' && path.match(/^\/feedback\/[^/]+\/retry-enrichment$/)) {
    if (options.fail?.feedbackRetryEnrichment) {
      await fulfillError(route, options.fail.feedbackRetryEnrichment, 500)
      return true
    }
    await fulfillJson(route, consoleA11yRetryEnrichmentResponse, 202)
    return true
  }
  if (method === 'GET' && path.match(/^\/feedback\/[^/]+\/signal-trace$/)) {
    const feedbackId = path.split('/')[2]
    await fulfillJson(route, signalTraceForFeedback(feedbackId))
    return true
  }
  if (method === 'GET' && path.match(/^\/feedback\/[^/]+$/)) {
    const feedbackId = path.split('/')[2]
    const detail = state.feedbackDetails[feedbackId]
    await fulfillJson(route, detail ? clone(detail) : null, detail ? 200 : 404)
    return true
  }

  if (method === 'GET' && path === '/customer-requests') {
    diagnostics.customerRequestListRequests.push(url.search)
    const accountKey = url.searchParams.get('account_key')?.trim()
    const requests =
      accountKey && accountKey !== 'acct:acme'
        ? []
        : clone(consoleA11yCustomerRequestsList.requests)
    await fulfillJson(route, { requests })
    return true
  }
  if (method === 'GET' && path === '/customer-requests/account-summary') {
    const accountKey = url.searchParams.get('account_key')?.trim()
    const summary =
      accountKey && accountKey !== 'acct:acme'
        ? {
            accountKey,
            requestCount: 0,
            feedbackCount: 0,
            customerCount: 0,
            voteCount: 0,
            issueCount: 0,
            syncedIssueCount: 0,
            staleIssueCount: 0,
            failedIssueCount: 0,
            pendingIssueCount: 0,
            manualIssueCount: 0,
            revenueImpactCents: '0',
            revenueCurrency: 'USD',
            highPriorityRequestCount: 0,
            shippedRequestCount: 0,
            staleOrFailedIssueCount: 0,
            averageDecisionScore: 0,
            topDecisionScore: 0,
            decisionSignals: [],
            events: [],
            timeline: [],
          }
        : clone(consoleA11yCustomerRequestAccountSummary)
    await fulfillJson(route, summary)
    return true
  }
  if (method === 'GET' && path === '/customer-requests/saved-views') {
    await fulfillJson(route, { views: [] })
    return true
  }
  if (method === 'GET' && path.match(/^\/customer-requests\/[^/]+$/)) {
    const requestID = path.split('/')[2]
    const detail =
      requestID === consoleA11yCustomerRequestDetail.request?.id
        ? clone(consoleA11yCustomerRequestDetail)
        : null
    await fulfillJson(route, detail, detail ? 200 : 404)
    return true
  }

  if (method === 'POST' && path.match(/^\/feedback\/[^/]+\/reply-draft\/edit$/)) {
    const body = readJsonBody(route)
    diagnostics.replyDraftRequests.push({ method, path, body })
    const content =
      typeof (body as { content?: unknown } | null)?.content === 'string'
        ? (body as { content: string }).content
        : state.replyDraftWorkflow.activeText
    state.replyDraftWorkflow = editReplyDraftWorkflow(state.replyDraftWorkflow, content)
    syncReplyDraftFeedbackDetail(state)
    await fulfillJson(route, { workflow: state.replyDraftWorkflow })
    return true
  }
  if (method === 'POST' && path.match(/^\/feedback\/[^/]+\/reply-draft\/approve$/)) {
    const body = readJsonBody(route)
    diagnostics.replyDraftRequests.push({ method, path, body })
    state.replyDraftWorkflow = advanceReplyDraftWorkflow(state.replyDraftWorkflow, 'approved')
    syncReplyDraftFeedbackDetail(state)
    await fulfillJson(route, { workflow: state.replyDraftWorkflow })
    return true
  }
  if (method === 'POST' && path.match(/^\/feedback\/[^/]+\/reply-draft\/reject$/)) {
    const body = readJsonBody(route)
    diagnostics.replyDraftRequests.push({ method, path, body })
    state.replyDraftWorkflow = advanceReplyDraftWorkflow(state.replyDraftWorkflow, 'rejected')
    syncReplyDraftFeedbackDetail(state)
    await fulfillJson(route, { workflow: state.replyDraftWorkflow })
    return true
  }
  if (method === 'POST' && path.match(/^\/feedback\/[^/]+\/reply-draft\/send$/)) {
    const body = readJsonBody(route)
    diagnostics.replyDraftRequests.push({
      method,
      path,
      body,
      idempotencyKey: route.request().headers()['idempotency-key'],
    })
    state.replyDraftWorkflow = advanceReplyDraftWorkflow(state.replyDraftWorkflow, 'sent')
    syncReplyDraftFeedbackDetail(state)
    await fulfillJson(route, { workflow: state.replyDraftWorkflow, fromCache: false })
    return true
  }

  if (method === 'GET' && path === '/system/preflight') {
    await fulfillJson(route, {
      status: 'warn',
      elapsed: '42ms',
      checks: [
        {
          name: 'database',
          category: 'database',
          status: 'pass',
          message: 'Database reachable',
          remediation: '',
        },
        {
          name: 'worker',
          category: 'worker',
          status: 'warn',
          message: 'Replay queue needs review',
          remediation: 'Run the replay drill before release.',
        },
        {
          name: 'secrets:tink_keyset',
          category: 'encryption',
          status: 'pass',
          message: 'Tink keyset parsed and primary key available',
          remediation: '',
        },
        {
          name: 'secrets:decryptability',
          category: 'backup',
          status: 'pass',
          message: 'Managed secret samples decrypted',
          remediation: '',
        },
      ],
    })
    return true
  }
  if (method === 'GET' && path === '/system/recovery') {
    await fulfillJson(route, {
      status: 'pass',
      message: 'Last restore drill passed',
      freshnessWindowSeconds: 604800,
      ageSeconds: 3600,
      lastRun: {
        ranAt: '2026-08-01T09:00:00Z',
        status: 'pass',
        backupRef: 'nightly-backup',
        durationMs: 1234,
      },
    })
    return true
  }
  if (method === 'GET' && path === '/system/release') {
    await fulfillJson(route, {
      serviceVersion: '5d6ea83',
      environment: 'production',
      profile: 'production',
      lifecycleState: 'supported',
      ownerTeam: 'Platform',
      compatibilityRules: [
        { key: 'additive', label: 'Additive' },
        { key: 'breaking', label: 'Breaking' },
      ],
      glossary: [
        { key: 'environment', label: 'Environment' },
        { key: 'owner', label: 'Owner' },
      ],
      runbookUrl: 'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
      escalationUrl: 'https://github.com/Phixsura/attune/issues/new/choose',
      startedAt: '2026-08-01T09:00:00Z',
    })
    return true
  }

  if (method === 'GET' && path === '/api-keys') {
    await fulfillJson(route, consoleA11yApiKeysList)
    return true
  }
  if (method === 'GET' && path === '/llm/channels') {
    await fulfillJson(route, { items: consoleA11yLLMChannels() })
    return true
  }
  if (method === 'GET' && path === '/service-accounts') {
    await fulfillJson(route, { items: state.serviceAccounts })
    return true
  }
  if (method === 'GET' && path === '/api-keys/scopes') {
    await fulfillJson(route, consoleA11yApiKeyScopes)
    return true
  }
  if (method === 'GET' && path === '/api-keys/presets') {
    await fulfillJson(route, consoleA11yApiKeyPresets)
    return true
  }
  if (method === 'POST' && path === '/service-accounts') {
    const body = readJsonBody(route) as { description?: string; name?: string } | null
    const created: ServiceAccount = {
      ...consoleA11yServiceAccount,
      id: `sa-a11y-${state.serviceAccounts.length + 1}`,
      name: body?.name?.trim() || 'new-service-account',
      description: body?.description?.trim() || '',
      isActive: true,
      createdAt: '2026-06-24T09:10:00Z',
      updatedAt: '2026-06-24T09:10:00Z',
    }
    state.serviceAccounts = insertServiceAccount(state.serviceAccounts, created)
    await fulfillJson(route, { serviceAccount: created }, 201)
    return true
  }
  if (method === 'PATCH' && path.match(/^\/service-accounts\/[^/]+$/)) {
    const body = readJsonBody(route) as { isActive?: boolean } | null
    const id = path.split('/')[2]
    const existing =
      state.serviceAccounts.find((account) => account.id === id) ?? consoleA11yServiceAccount
    const updated: ServiceAccount = {
      ...existing,
      id,
      isActive: body?.isActive ?? existing.isActive,
      updatedAt: '2026-06-24T09:11:00Z',
    }
    state.serviceAccounts = insertServiceAccount(
      state.serviceAccounts.filter((account) => account.id !== id),
      updated,
    )
    await fulfillJson(route, updated)
    return true
  }
  if (method === 'DELETE' && path.match(/^\/service-accounts\/[^/]+$/)) {
    const id = path.split('/')[2]
    state.serviceAccounts = state.serviceAccounts.filter((account) => account.id !== id)
    await route.fulfill({ status: 204 })
    return true
  }

  if (method === 'GET' && path === '/audit-log/views') {
    await fulfillJson(route, { items: state.auditLogViews })
    return true
  }
  if (method === 'POST' && path === '/audit-log/views') {
    const body = readJsonBody(route) as { name?: string; state?: AuditLogViewState } | null
    const created: SavedAuditLogView = {
      id: `audit-view-a11y-${state.auditLogViews.length + 1}`,
      name: body?.name?.trim() || 'Untitled view',
      state: body?.state,
      createdAt: '2026-06-24T09:10:00Z',
      updatedAt: '2026-06-24T09:10:00Z',
    }
    state.auditLogViews = upsertSavedAuditView(state.auditLogViews, created)
    await fulfillJson(route, { view: created }, 201)
    return true
  }
  if (method === 'PUT' && path.match(/^\/audit-log\/views\/[^/]+$/)) {
    const body = readJsonBody(route) as { name?: string; state?: AuditLogViewState } | null
    const id = path.split('/')[3]
    const existing = state.auditLogViews.find((view) => view.id === id)
    const updated: SavedAuditLogView = {
      id,
      name: body?.name?.trim() || existing?.name || 'Untitled view',
      state: body?.state ?? existing?.state,
      createdAt: existing?.createdAt ?? '2026-06-24T09:10:00Z',
      updatedAt: '2026-06-24T09:11:00Z',
    }
    state.auditLogViews = upsertSavedAuditView(
      state.auditLogViews.filter((view) => view.id !== id),
      updated,
    )
    await fulfillJson(route, { view: updated })
    return true
  }
  if (method === 'DELETE' && path.match(/^\/audit-log\/views\/[^/]+$/)) {
    const id = path.split('/')[3]
    state.auditLogViews = state.auditLogViews.filter((view) => view.id !== id)
    await route.fulfill({ status: 204 })
    return true
  }

  if (method === 'POST' && path === '/api-keys') {
    if (options.fail?.apiKeyCreate) {
      await fulfillError(route, options.fail.apiKeyCreate, 400)
      return true
    }
    await fulfillJson(route, consoleA11yIssuedApiKey)
    return true
  }
  if (method === 'DELETE' && path.match(/^\/api-keys\/[^/]+$/)) {
    if (options.fail?.apiKeyRevoke) {
      await fulfillError(route, options.fail.apiKeyRevoke, 409)
      return true
    }
    await route.fulfill({ status: 204 })
    return true
  }

  if (method === 'GET' && path === '/mcp/clients') {
    await fulfillJson(route, consoleA11yMcpClientsList)
    return true
  }
  if (method === 'POST' && path === '/mcp/clients') {
    await fulfillJson(route, { client: consoleA11yMcpClientDetail.client })
    return true
  }
  if (method === 'GET' && path.match(/^\/mcp\/clients\/[^/]+$/)) {
    await fulfillJson(route, consoleA11yMcpClientDetail)
    return true
  }
  if (method === 'PATCH' && path.match(/^\/mcp\/clients\/[^/]+$/)) {
    await fulfillJson(route, consoleA11yMcpClientUpdate)
    return true
  }
  if (method === 'DELETE' && path.match(/^\/mcp\/clients\/[^/]+$/)) {
    await route.fulfill({ status: 204 })
    return true
  }
  if (method === 'PUT' && path.match(/^\/mcp\/clients\/[^/]+\/tool-policies$/)) {
    if (options.fail?.mcpToolPolicies) {
      await fulfillError(route, options.fail.mcpToolPolicies, 409)
      return true
    }
    await fulfillJson(route, consoleA11yMcpToolPolicyUpdate)
    return true
  }
  if (method === 'DELETE' && path.match(/^\/mcp\/clients\/[^/]+\/sessions\/[^/]+$/)) {
    if (options.fail?.mcpSessionRevoke) {
      await fulfillError(route, options.fail.mcpSessionRevoke, 409)
      return true
    }
    await route.fulfill({ status: 204 })
    return true
  }
  if (method === 'DELETE' && path.match(/^\/mcp\/clients\/[^/]+\/grants\/[^/]+$/)) {
    if (options.fail?.mcpGrantRevoke) {
      await fulfillError(route, options.fail.mcpGrantRevoke, 409)
      return true
    }
    await route.fulfill({ status: 204 })
    return true
  }

  if (method === 'GET' && path === '/gdpr/operations') {
    await fulfillJson(route, consoleA11yGdprOperations)
    return true
  }
  if (method === 'GET' && path === '/gdpr/requests') {
    const requestType = url.searchParams.get('request_type')
    const items = requestType
      ? consoleA11yGdprRequests.items.filter((item) =>
          item.requestType.toLowerCase().includes(requestType.toLowerCase()),
        )
      : consoleA11yGdprRequests.items
    await fulfillJson(route, { items })
    return true
  }
  if (method === 'POST' && path === '/gdpr/step-up/verify') {
    await fulfillJson(route, consoleA11yGdprStepUpVerified)
    return true
  }
  if (method === 'POST' && path === '/gdpr/export') {
    await fulfillJson(route, consoleA11yGdprExportStart)
    return true
  }
  if (method === 'POST' && path === '/gdpr/delete') {
    await fulfillJson(route, consoleA11yGdprDelete)
    return true
  }
  if (method === 'GET' && path.match(/^\/gdpr\/exports\/[^/]+$/)) {
    await fulfillJson(route, consoleA11yGdprExportReady)
    return true
  }
  if (method === 'POST' && path.match(/^\/gdpr\/exports\/[^/]+\/revoke$/)) {
    await fulfillJson(route, consoleA11yGdprRevoke)
    return true
  }
  if (method === 'GET' && path.match(/^\/gdpr\/exports\/[^/]+\/download$/)) {
    await fulfillBytes(route, 'gdpr-export.zip', 'application/zip')
    return true
  }
  if (method === 'POST' && path.match(/^\/gdpr\/requests\/[^/]+\/cancel$/)) {
    await fulfillJson(route, {
      requestId: 'gdpr-delete-a11y',
      status: 'GDPR_REQUEST_STATUS_CANCELLED',
    })
    return true
  }

  if (method === 'GET' && path === '/audit-log') {
    if (url.searchParams.get('targetType') === 'member') {
      await fulfillJson(route, {
        items: [
          {
            ...consoleA11yAuditEntry,
            action: 'member.update_role',
            id: 'audit-member-a11y',
            summary: 'Updated member role for governance readiness verification.',
            targetId: 'member-a11y-pm',
            targetType: 'member',
          },
        ],
      })
      return true
    }
    if (url.searchParams.get('targetType') === 'public_moderation_subject') {
      await fulfillJson(route, {
        items: [
          {
            ...consoleA11yAuditEntry,
            action: 'moderation.hide',
            id: 'audit-public-moderation-a11y',
            summary: 'Hid public moderation subject for field-level permission verification.',
            targetId: 'moderation-a11y-pending',
            targetType: 'public_moderation_subject',
          },
        ],
      })
      return true
    }
    await fulfillJson(route, consoleA11yAuditLog)
    return true
  }
  if (method === 'GET' && path === '/audit-log/export.csv') {
    await route.fulfill({
      status: 200,
      contentType: 'text/csv',
      headers: {
        'Content-Disposition': 'attachment; filename="audit-log.csv"',
      },
      body: 'id,action,created_at\naudit-1,api_key.create,2026-06-24T09:20:00Z\n',
    })
    return true
  }
  if (method === 'POST' && path === '/audit-log/evidence') {
    await fulfillJson(route, consoleA11yAuditEvidenceCreate)
    return true
  }
  if (method === 'GET' && path.match(/^\/audit-log\/evidence\/[^/]+$/)) {
    await fulfillJson(route, consoleA11yAuditEvidenceReady)
    return true
  }
  if (method === 'GET' && path.match(/^\/audit-log\/evidence\/[^/]+\/download$/)) {
    await fulfillBytes(route, 'audit-evidence.zip', 'application/zip')
    return true
  }

  if (method === 'GET' && path === '/reply-send-hook') {
    await fulfillJson(route, state.replySendHook)
    return true
  }
  if (method === 'GET' && path === '/reply-send-hook/health') {
    await fulfillJson(route, replySendHookHealth(state.replySendHookDeliveries))
    return true
  }
  if (method === 'GET' && path === '/reply-send-hook/deliveries') {
    await fulfillJson(route, { items: state.replySendHookDeliveries })
    return true
  }
  if (method === 'POST' && path === '/reply-send-hook/test') {
    const body = readJsonBody(route)
    diagnostics.replySendHookRequests.push({ method, path, body })
    const attempt = replySendHookDelivery({
      id: `reply-delivery-test-${state.replySendHookDeliveries.length + 1}`,
      status: 'accepted',
      httpStatus: 204,
      attempts: 1,
      error: undefined,
      retryable: false,
    })
    state.replySendHookDeliveries = [attempt, ...state.replySendHookDeliveries]
    await fulfillJson(route, attempt)
    return true
  }
  if (method === 'POST' && path.match(/^\/reply-send-hook\/deliveries\/[^/]+\/redeliver$/)) {
    const body = readJsonBody(route)
    diagnostics.replySendHookRequests.push({ method, path, body })
    const id = path.split('/')[3]
    const existing =
      state.replySendHookDeliveries.find((delivery) => delivery.id === id) ??
      replySendHookDelivery({ id })
    const updated = {
      ...existing,
      status: 'accepted',
      httpStatus: 204,
      attempts: existing.attempts + 1,
      error: undefined,
      retryable: false,
      requestedAt: '2026-06-24T09:14:00Z',
      updatedAt: '2026-06-24T09:14:00Z',
    }
    state.replySendHookDeliveries = [
      updated,
      ...state.replySendHookDeliveries.filter((delivery) => delivery.id !== id),
    ]
    await fulfillJson(route, updated)
    return true
  }
  if (method === 'PUT' && path === '/reply-send-hook') {
    const body = readJsonBody(route)
    diagnostics.replySendHookRequests.push({ method, path, body })
    const request = body as {
      enabled?: boolean
      name?: string
      secret?: string
      url?: string
    } | null
    const urlHost = request?.url ? new URL(request.url).hostname : state.replySendHook.urlHost
    state.replySendHook = {
      ...state.replySendHook,
      enabled: request?.enabled ?? true,
      name: request?.name || 'Support reply bridge',
      urlHost,
      urlFingerprint: 'sha256:replyhookupdateda11yf1ngerprint000000000000',
      updatedBy: 'user-a11y',
      updatedAt: '2026-06-24T09:12:00Z',
    }
    await fulfillJson(route, {
      ...state.replySendHook,
      secretOnce: request?.secret ? undefined : 'generated_reply_secret_a11y_123456',
    })
    return true
  }
  if (method === 'DELETE' && path === '/reply-send-hook') {
    diagnostics.replySendHookRequests.push({ method, path, body: null })
    state.replySendHook = {
      ...state.replySendHook,
      enabled: false,
      disabledAt: '2026-06-24T09:13:00Z',
      updatedBy: 'user-a11y',
      updatedAt: '2026-06-24T09:13:00Z',
    }
    await fulfillJson(route, state.replySendHook)
    return true
  }

  if (method === 'GET' && path === '/external-sync/health') {
    await fulfillJson(route, clone(state.externalSync.health))
    return true
  }
  if (method === 'GET' && path === '/external-sync/providers') {
    await fulfillJson(route, consoleA11yExternalSyncProviders)
    return true
  }
  if (method === 'GET' && path === '/external-sync/provider-installations') {
    await fulfillJson(route, { installations: clone(state.externalSync.installations) })
    return true
  }
  if (method === 'POST' && path === '/external-sync/provider-installations') {
    const body = readJsonBody(route) as CreateExternalProviderInstallationRequest
    diagnostics.providerInstallationRequests.push({ method, path, body })
    const installation = createExternalProviderInstallation(state.externalSync, body)
    await fulfillJson(route, clone(installation), 201)
    return true
  }
  if (method === 'POST' && path.match(/^\/external-sync\/provider-installations\/[^/]+:qualify$/)) {
    diagnostics.providerInstallationRequests.push({ method, path, body: null })
    const installationID = providerInstallationIDFromPath(path)
    const result = qualifyExternalProviderInstallation(state.externalSync, installationID)
    if (!result) {
      await fulfillError(route, `Missing provider installation ${installationID}`, 404)
      return true
    }
    await fulfillJson(route, clone(result))
    return true
  }
  if (method === 'DELETE' && path.match(/^\/external-sync\/provider-installations\/[^/]+$/)) {
    diagnostics.providerInstallationRequests.push({ method, path, body: null })
    const installationID = providerInstallationIDFromPath(path)
    state.externalSync.installations = state.externalSync.installations.filter(
      (installation) => installation.id !== installationID,
    )
    delete state.externalSync.resourcesByInstallation[installationID]
    await route.fulfill({ status: 204 })
    return true
  }
  if (
    method === 'GET' &&
    path.match(/^\/external-sync\/provider-installations\/[^/]+\/resources$/)
  ) {
    const installationID = providerInstallationIDFromPath(path)
    await fulfillJson(route, {
      resources: clone(state.externalSync.resourcesByInstallation[installationID] ?? []),
    })
    return true
  }
  if (
    method === 'POST' &&
    path.match(/^\/external-sync\/provider-installations\/[^/]+\/resources:select$/)
  ) {
    const body = readJsonBody(route) as { resourceIds?: string[] }
    diagnostics.providerInstallationRequests.push({ method, path, body })
    const installationID = providerInstallationIDFromPath(path)
    const resources = selectExternalProviderInstallationResources(
      state.externalSync,
      installationID,
      body?.resourceIds ?? [],
    )
    if (!resources) {
      await fulfillError(route, `Missing provider installation ${installationID}`, 404)
      return true
    }
    await fulfillJson(route, { resources: clone(resources) })
    return true
  }
  if (method === 'GET' && path === '/external-sync/connections') {
    await fulfillJson(route, { connections: clone(state.externalSync.connections) })
    return true
  }
  if (method === 'POST' && path === '/external-sync/connections') {
    const body = readJsonBody(route) as CreateExternalConnectionRequest
    diagnostics.externalConnectionRequests.push({ method, path, body })
    const connection = createExternalConnection(state.externalSync, body)
    await fulfillJson(route, clone(connection), 201)
    return true
  }
  if (method === 'GET' && path.match(/^\/external-sync\/connections\/[^/]+\/schema$/)) {
    const connectionID = path.split('/')[3]
    const schemas = state.externalSync.schemasByConnection[connectionID] ?? []
    await fulfillJson(route, { schemas: clone(schemas) })
    return true
  }
  if (method === 'GET' && path === '/external-sync/mappings') {
    const connectionID = url.searchParams.get('connection_id')
    const mappings = connectionID
      ? state.externalSync.mappings.filter((mapping) => mapping.connectionId === connectionID)
      : state.externalSync.mappings
    await fulfillJson(route, { mappings: clone(mappings) })
    return true
  }
  if (method === 'GET' && path === '/external-sync/runs') {
    const connectionID = url.searchParams.get('connection_id')
    const runs = connectionID
      ? state.externalSync.runs.filter((run) => run.connectionId === connectionID)
      : state.externalSync.runs
    await fulfillJson(route, { runs: clone(runs), nextBeforeId: '' })
    return true
  }
  if (method === 'GET' && path === '/external-sync/events') {
    const connectionID = url.searchParams.get('connection_id')
    const events = connectionID
      ? state.externalSync.events.filter((event) => event.connectionId === connectionID)
      : state.externalSync.events
    await fulfillJson(route, { events: clone(events), nextBeforeId: '' })
    return true
  }

  if (method === 'GET' && path === '/outbox/deliveries') {
    await fulfillJson(route, consoleA11yOutboxDeliveries)
    return true
  }
  if (method === 'POST' && path.match(/^\/outbox\/[^/]+\/retry$/)) {
    if (options.fail?.outboxRetry) {
      await fulfillError(route, options.fail.outboxRetry, 409)
      return true
    }
    await fulfillJson(route, consoleA11yOutboxRetry, 202)
    return true
  }

  return false
}

type ApiMockState = {
  auditLogViews: SavedAuditLogView[]
  feedbackDetails: Record<string, FeedbackDetail>
  inboundSources: InboundSource[]
  slackChannels: SlackChannel[]
  replyDraftWorkflow: ReplyDraftWorkflow
  replySendHook: ReplySendHook
  replySendHookDeliveries: ReplySendHookDelivery[]
  externalSync: ExternalSyncMockState
  qualityActions: QualityAction[]
  serviceAccounts: ServiceAccount[]
  assignmentPolicy: FeedbackAssignmentPolicy
  assignmentPolicyRevisions: FeedbackAssignmentPolicyRevision[]
  tags: Tag[]
}

const consoleA11yExternalSyncProviders: ExternalSyncProvider[] = [
  {
    provider: 'github',
    display: 'GitHub',
  },
  {
    provider: 'jira',
    display: 'Jira',
  },
]

type ExternalSyncMockState = {
  connections: ExternalConnection[]
  events: ExternalSyncEvent[]
  health: ExternalSyncHealthResponse
  installations: ExternalProviderInstallation[]
  mappings: ExternalObjectMapping[]
  resourcesByInstallation: Record<string, ExternalProviderInstallationResource[]>
  runs: ExternalSyncRun[]
  schemasByConnection: Record<string, ExternalObjectSchema[]>
}

function createInboundSourcesState(): InboundSource[] {
  const tenantId = consoleA11yMe.tenant.id
  return [
    {
      id: 'src-webhook-a11y',
      tenantId,
      channel: 'webhook',
      name: 'Website Feedback',
      slug: 'website-feedback',
      enabled: true,
      lastEventAt: '2026-07-11T12:00:00Z',
      lastUid: '0',
      lastError: '',
      createdAt: '2026-07-10T12:00:00Z',
      updatedAt: '2026-07-11T12:00:00Z',
    },
    {
      id: 'src-email-a11y',
      tenantId,
      channel: 'email',
      name: 'Support Mailbox',
      slug: 'support-mailbox',
      enabled: false,
      lastEventAt: '',
      lastUid: '0',
      lastError: '',
      createdAt: '2026-07-10T13:00:00Z',
      updatedAt: '2026-07-11T12:00:00Z',
    },
    {
      id: 'src-slack-a11y',
      tenantId,
      channel: 'slack',
      name: 'Slack Mock Source',
      slug: 'slack-mock-source',
      enabled: true,
      lastEventAt: '2026-07-11T15:00:00Z',
      lastUid: '1783852321068324',
      lastError: 'slack auth.test: invalid_auth',
      createdAt: '2026-07-10T14:00:00Z',
      updatedAt: '2026-07-11T15:00:00Z',
    },
  ]
}

function createSlackChannelsState(): SlackChannel[] {
  return [
    { id: 'C-GENERAL', name: 'general', isPrivate: false, isArchived: false, isShared: false },
    {
      id: 'C-PRODUCT',
      name: 'product-feedback',
      isPrivate: false,
      isArchived: false,
      isShared: true,
    },
    { id: 'C-SUPPORT', name: 'support', isPrivate: true, isArchived: false, isShared: false },
  ]
}

function createAssignmentPolicyState(): FeedbackAssignmentPolicy {
  return {
    version: 1,
    updatedBy: 'system',
    note: 'Default assignment policy',
    rules: [
      assignmentPolicyRule('urgent_open', 'Urgent open feedback', 'support_triage', 'critical', 24),
      assignmentPolicyRule('terminal_failures', 'Terminal AI failures', 'ai_ops', 'high', 48),
      assignmentPolicyRule('stalled_active', 'Active work at risk', 'product_owner', 'high', 168),
      assignmentPolicyRule('identity_debt', 'Identity evidence debt', 'data_quality', 'medium', 96),
      assignmentPolicyRule('untriaged', 'Untriaged intake', 'triage_dri', 'high', 72),
    ],
  }
}

function policyRevisionFromAssignmentPolicy(
  policy: FeedbackAssignmentPolicy,
): FeedbackAssignmentPolicyRevision {
  return {
    version: policy.version,
    updatedAt: policy.updatedAt,
    updatedBy: policy.updatedBy,
    note: policy.note,
    rules: clone(policy.rules),
  }
}

function assignmentPolicyRule(
  ruleKey: string,
  ruleName: string,
  ownerLane: string,
  severity: string,
  slaHours: number,
): FeedbackAssignmentPolicyRule {
  return {
    ruleKey,
    ruleName,
    ownerLane,
    severity,
    slaHours,
    enabled: true,
    rationale: 'Mocked assignment policy rule.',
  }
}

function sortInboundSources(sources: InboundSource[]): InboundSource[] {
  return clone(sources).sort((left, right) => {
    if (left.channel !== right.channel) {
      return left.channel.localeCompare(right.channel)
    }
    return left.name.localeCompare(right.name)
  })
}

function buildTestInboundSourceConnectionResponse(
  state: ApiMockState,
  body: {
    channel?: string
    emailConfig?: { host?: string }
    slackConfig?: { botToken?: string; channelId?: string }
    zendeskConfig?: { subdomain?: string }
    intercomConfig?: { region?: string; accessToken?: string }
  } | null,
): TestInboundConnectionResponse {
  if (body?.channel === 'slack') {
    const token = body.slackConfig?.botToken?.trim() ?? ''
    if (!token) {
      return { ok: false, error: 'slack_config.bot_token must not be empty' }
    }
    const channelID = body.slackConfig?.channelId?.trim() ?? ''
    if (channelID && !state.slackChannels.some((channel) => channel.id === channelID)) {
      return { ok: false, error: `unknown slack channel ${channelID}` }
    }
    return { ok: true, latencyMs: '2' }
  }
  if (body?.channel === 'email') {
    const host = body.emailConfig?.host?.trim() ?? ''
    if (!host) {
      return { ok: false, error: 'email_config.host must not be empty' }
    }
    return { ok: true, latencyMs: '5' }
  }
  if (body?.channel === 'zendesk') {
    const subdomain = body.zendeskConfig?.subdomain?.trim() ?? ''
    if (!subdomain) {
      return { ok: false, error: 'zendesk_config.subdomain must not be empty' }
    }
    return { ok: true, latencyMs: '8' }
  }
  if (body?.channel === 'intercom') {
    const token = body.intercomConfig?.accessToken?.trim() ?? ''
    if (!token) {
      return { ok: false, error: 'intercom_config.access_token must not be empty' }
    }
    return { ok: true, latencyMs: '6' }
  }
  return { ok: false, error: 'unsupported channel' }
}

function buildDiscoverSlackChannelsResponse(
  state: ApiMockState,
  body: {
    slackConfig?: { botToken?: string; channelId?: string }
  } | null,
): DiscoverSlackChannelsResponse {
  const token = body?.slackConfig?.botToken?.trim() ?? ''
  if (!token) {
    return { channels: [] }
  }
  return { channels: clone(state.slackChannels) }
}

function buildCreateInboundSourceResponse(
  state: ApiMockState,
  body: {
    channel?: string
    emailConfig?: Record<string, unknown>
    name?: string
    slackConfig?: { botToken?: string; channelId?: string }
    webhookConfig?: Record<string, never>
  } | null,
): CreateInboundSourceResponse | null {
  const channel = body?.channel?.trim() ?? ''
  const name = body?.name?.trim() ?? ''
  if (!channel || !name) return null
  if (channel === 'slack') {
    const token = body.slackConfig?.botToken?.trim() ?? ''
    const channelID = body.slackConfig?.channelId?.trim() ?? ''
    if (!token || !channelID) return null
    const selected =
      state.slackChannels.find((item) => item.id === channelID) ?? state.slackChannels[0]
    if (!selected) return null
    const source = createInboundSourceRow(state, {
      channel,
      name,
      lastError: '',
      lastEventAt: '',
      lastUid: '0',
      slug: slugifyInboundSourceName(name),
    })
    state.inboundSources = upsertInboundSource(state.inboundSources, source)
    return { source: clone(source) }
  }
  if (channel === 'webhook') {
    const source = createInboundSourceRow(state, {
      channel,
      name,
      lastError: '',
      lastEventAt: '',
      lastUid: '0',
      slug: slugifyInboundSourceName(name),
    })
    state.inboundSources = upsertInboundSource(state.inboundSources, source)
    return {
      source: clone(source),
      webhookSecretReveal: {
        url: `https://hooks.example.com/inbound/${source.slug}`,
        secretHex: 'deadbeefcafebabefeedface0000000000000000000000000000000000000000',
        curlExample: `curl -X POST https://hooks.example.com/inbound/${source.slug}`,
      },
    }
  }
  if (channel === 'email' || channel === 'zendesk' || channel === 'intercom') {
    const source = createInboundSourceRow(state, {
      channel,
      name,
      lastError: '',
      lastEventAt: '',
      lastUid: '0',
      slug: slugifyInboundSourceName(name),
      enabled: true,
    })
    state.inboundSources = upsertInboundSource(state.inboundSources, source)
    return { source: clone(source) }
  }
  return null
}

function createInboundSourceRow(
  state: ApiMockState,
  input: {
    channel: string
    enabled?: boolean
    lastError: string
    lastEventAt: string
    lastUid: string
    name: string
    slug: string
  },
): InboundSource {
  const now = '2026-07-12T10:00:00Z'
  return {
    id: `src-${state.inboundSources.length + 1}-a11y`,
    tenantId: consoleA11yMe.tenant.id,
    channel: input.channel,
    name: input.name,
    slug: input.slug,
    enabled: input.enabled ?? true,
    lastEventAt: input.lastEventAt,
    lastUid: input.lastUid,
    lastError: input.lastError,
    createdAt: now,
    updatedAt: now,
  }
}

function upsertInboundSource(items: InboundSource[], source: InboundSource): InboundSource[] {
  return [...items.filter((item) => item.id !== source.id), source]
}

function updateInboundSource(
  state: ApiMockState,
  id: string,
  patch: Partial<Pick<InboundSource, 'enabled' | 'lastError' | 'lastEventAt' | 'lastUid'>>,
) {
  const current = state.inboundSources.find((source) => source.id === id)
  if (!current) {
    throw new Error(`missing inbound source ${id}`)
  }
  const updated: InboundSource = {
    ...current,
    ...patch,
    updatedAt: '2026-07-12T10:01:00Z',
  }
  state.inboundSources = upsertInboundSource(
    state.inboundSources.filter((source) => source.id !== id),
    updated,
  )
  return clone(updated)
}

function slugifyInboundSourceName(value: string) {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 200)
}

function feedbackDetailWithReplyDraft(
  base: FeedbackDetail,
  workflow: ReplyDraftWorkflow,
): FeedbackDetail {
  return {
    ...base,
    replyDraft: workflow.activeText,
    replyDraftGeneratedAt: workflow.generatedAt ?? base.replyDraftGeneratedAt,
    replyDraftWorkflow: workflow,
  }
}

function signalTraceForFeedback(feedbackId: string): FeedbackSignalTrace {
  return {
    ...clone(consoleA11yFeedbackSignalTrace),
    feedbackId,
    signalTraceId: `trace-${feedbackId}`,
    events: consoleA11yFeedbackSignalTrace.events.map((event) => ({
      ...event,
      traceId: event.traceId ? `trace-${feedbackId}` : event.traceId,
    })),
  }
}

function syncReplyDraftFeedbackDetail(state: ApiMockState) {
  const detail = state.feedbackDetails['feedback-101']
  if (!detail) return
  state.feedbackDetails['feedback-101'] = feedbackDetailWithReplyDraft(
    detail,
    state.replyDraftWorkflow,
  )
}

function feedbackListResponse(
  state: ApiMockState,
  terminalOnly: boolean,
  source?: string | null,
  accountKey?: string,
) {
  const items = [
    state.feedbackDetails['feedback-101'],
    state.feedbackDetails['feedback-201'],
    state.feedbackDetails['feedback-301'],
  ].filter((item): item is FeedbackDetail => Boolean(item))

  const scopedItems = source
    ? items.filter((item) => item.source === source)
    : items.filter((item) => item.source !== 'portal')

  const accountScopedItems = accountKey
    ? scopedItems.filter((item) => item.accountContext?.accountKey === accountKey)
    : scopedItems

  const filteredItems = terminalOnly
    ? accountScopedItems.filter(
        (item) => item.enrichmentStatus === 'failed' && (item.enrichmentAttempts ?? 0) >= 5,
      )
    : accountScopedItems

  return {
    items: filteredItems.map((item) => clone(item)),
  }
}

function assignmentEscalationQueue(state: ApiMockState): FeedbackAssignmentEscalationQueue {
  const generatedAt = consoleA11yFeedbackAssignmentEscalations.generatedAt
  const now = new Date(generatedAt)
  const source = Object.values(state.feedbackDetails).filter(
    (item) => item.workflowState?.category !== 'closed',
  )
  const items = source
    .flatMap((item) => {
      const reasons = assignmentEscalationReasons(item, now)
      if (reasons.length === 0) return []
      const dueAt = item.assignment?.slaDueAt ? new Date(item.assignment.slaDueAt) : null
      const hoursUntilDue =
        dueAt && !Number.isNaN(dueAt.getTime())
          ? Math.trunc((dueAt.getTime() - now.getTime()) / 3_600_000)
          : undefined
      return [
        {
          feedbackId: item.id,
          title: item.enrichedDisplayTitle || item.enrichedTitle || item.content.slice(0, 96),
          source: item.source,
          type: item.type,
          isUrgent: item.isUrgent,
          createdAt: item.createdAt,
          assignment: {
            feedbackId: item.id,
            ...(item.assignment ?? {}),
            assignedBy: item.assignment?.assignedBy ?? '',
            note: item.assignment?.note ?? '',
            slaStatus: assignmentSlaStatus(item, now),
          },
          escalationReasons: reasons,
          hoursUntilDue,
          priority: assignmentEscalationPriority(reasons),
          accountContext: item.accountContext,
        },
      ]
    })
    .sort(assignmentEscalationSort)

  return {
    generatedAt,
    overdueCount: String(items.filter((item) => item.escalationReasons.includes('overdue')).length),
    dueSoonCount: String(
      items.filter((item) => item.escalationReasons.includes('due_soon')).length,
    ),
    missingOwnerCount: String(
      items.filter((item) => item.escalationReasons.includes('missing_owner')).length,
    ),
    missingSlaCount: String(
      items.filter((item) => item.escalationReasons.includes('missing_sla')).length,
    ),
    items,
  }
}

function assignmentEscalationReasons(item: FeedbackDetail, now: Date) {
  const reasons: string[] = []
  const dueAt = item.assignment?.slaDueAt ? new Date(item.assignment.slaDueAt) : null
  if (!dueAt || Number.isNaN(dueAt.getTime())) {
    reasons.push('missing_sla')
  } else if (dueAt.getTime() < now.getTime()) {
    reasons.push('overdue')
  } else if (dueAt.getTime() < now.getTime() + 12 * 3_600_000) {
    reasons.push('due_soon')
  }
  if (!item.assignment?.owner) {
    reasons.push('missing_owner')
  }
  return reasons
}

function assignmentSlaStatus(item: FeedbackDetail, now: Date) {
  const dueAt = item.assignment?.slaDueAt ? new Date(item.assignment.slaDueAt) : null
  if (!dueAt || Number.isNaN(dueAt.getTime())) return 'missing_due_date'
  if (dueAt.getTime() < now.getTime()) return 'overdue'
  if (dueAt.getTime() < now.getTime() + 12 * 3_600_000) return 'due_soon'
  return 'on_track'
}

function assignmentEscalationPriority(reasons: string[]) {
  if (reasons.includes('overdue')) return 'critical'
  if (reasons.includes('missing_owner') || reasons.includes('missing_sla')) return 'high'
  if (reasons.includes('due_soon')) return 'medium'
  return 'low'
}

function assignmentEscalationSort(
  left: FeedbackAssignmentEscalationQueue['items'][number],
  right: FeedbackAssignmentEscalationQueue['items'][number],
) {
  const rank = (item: FeedbackAssignmentEscalationQueue['items'][number]) => {
    if (item.escalationReasons.includes('overdue')) return 0
    if (item.escalationReasons.includes('missing_owner')) return 1
    if (item.escalationReasons.includes('missing_sla')) return 2
    if (item.escalationReasons.includes('due_soon')) return 3
    return 4
  }
  const rankDelta = rank(left) - rank(right)
  if (rankDelta !== 0) return rankDelta
  if (left.isUrgent !== right.isUrgent) return left.isUrgent ? -1 : 1
  return left.feedbackId.localeCompare(right.feedbackId)
}

function resolveFeedbackTag(
  state: ApiMockState,
  body: {
    feedbackId?: string
    tagId?: string
    tagName?: string
    tagColor?: string
  } | null,
) {
  const tagId = body?.tagId?.trim()
  if (tagId) {
    return state.tags.find((tag) => tag.id === tagId) ?? null
  }

  const tagName = body?.tagName?.trim()
  if (!tagName) return null

  const existing = state.tags.find((tag) => tag.name.toLowerCase() === tagName.toLowerCase())
  if (existing) return existing

  const created: Tag = {
    id: `tag-a11y-${state.tags.length + 1}`,
    name: tagName,
    color: body?.tagColor?.trim() || '#6b7280',
    description: '',
    exclusiveScope: '',
    usageCount: 0,
    archived: false,
    createdBy: 'user-a11y',
    createdAt: '2026-06-24T09:10:00Z',
    updatedAt: '2026-06-24T09:10:00Z',
  }
  state.tags = upsertTag(state.tags, created)
  return created
}

function upsertTag(items: Tag[], tag: Tag): Tag[] {
  return [...items.filter((item) => item.id !== tag.id), tag]
}

function editReplyDraftWorkflow(workflow: ReplyDraftWorkflow, content: string): ReplyDraftWorkflow {
  const revisionNo = nextRevisionNo(workflow)
  const revision = {
    id: `rev-human-${revisionNo}`,
    draftId: workflow.draftId,
    cycleNo: workflow.cycleNo,
    revisionNo,
    origin: 'human',
    content,
    createdBy: 'user-a11y',
    createdAt: '2026-06-24T09:10:00Z',
  }
  return {
    ...workflow,
    status: 'edited',
    activeRevisionId: revision.id,
    activeText: content,
    revisions: [revision, ...workflow.revisions],
    allowedActions: ['edit', 'approve', 'reject', 'regenerate'],
    editedAt: revision.createdAt,
    editedBy: 'user-a11y',
    revision: incrementRevision(workflow.revision),
    updatedAt: revision.createdAt,
  }
}

function advanceReplyDraftWorkflow(
  workflow: ReplyDraftWorkflow,
  status: 'approved' | 'rejected' | 'sent',
): ReplyDraftWorkflow {
  const at =
    status === 'approved'
      ? '2026-06-24T09:11:00Z'
      : status === 'sent'
        ? '2026-06-24T09:12:00Z'
        : '2026-06-24T09:13:00Z'
  const activeRevisionId = workflow.activeRevisionId ?? workflow.revisions[0]?.id
  if (status === 'approved') {
    return {
      ...workflow,
      status,
      approvedRevisionId: activeRevisionId,
      allowedActions: ['edit', 'send', 'reject', 'regenerate'],
      approvedAt: at,
      approvedBy: 'user-a11y',
      revision: incrementRevision(workflow.revision),
      updatedAt: at,
    }
  }
  if (status === 'sent') {
    return {
      ...workflow,
      status,
      sentRevisionId: activeRevisionId,
      allowedActions: [],
      sentAt: at,
      sentBy: 'user-a11y',
      externalDeliveryStatus: 'delivered',
      externalMessageId: 'browser-a11y-message',
      revision: incrementRevision(workflow.revision),
      updatedAt: at,
    }
  }
  return {
    ...workflow,
    status,
    allowedActions: ['regenerate'],
    rejectedAt: at,
    rejectedBy: 'user-a11y',
    revision: incrementRevision(workflow.revision),
    updatedAt: at,
  }
}

function nextRevisionNo(workflow: ReplyDraftWorkflow) {
  return Math.max(0, ...workflow.revisions.map((revision) => revision.revisionNo)) + 1
}

function incrementRevision(revision: string) {
  const parsed = Number(revision)
  return Number.isFinite(parsed) ? String(parsed + 1) : revision
}

function replySendHookDelivery(
  override: Partial<ReplySendHookDelivery> = {},
): ReplySendHookDelivery {
  return {
    id: 'reply-delivery-a11y',
    hookId: 'reply-hook-a11y',
    hookHost: 'hooks.example.com',
    hookFingerprint: 'sha256:2e8bb7f6b3c0a11y9d84e5c24219f4266fded6c4',
    eventType: 'reply.test',
    status: 'failed',
    idempotencyKey: 'reply_test_a11y',
    httpStatus: 500,
    attempts: 1,
    maxAttempts: 8,
    error: 'receiver returned 500',
    requestedByType: 'admin',
    requestedBy: 'user-a11y',
    requestedAt: '2026-06-24T09:05:00Z',
    createdAt: '2026-06-24T09:05:00Z',
    updatedAt: '2026-06-24T09:05:00Z',
    retryable: true,
    ...override,
  }
}

function replySendHookHealth(deliveries: ReplySendHookDelivery[]): ReplySendHookHealth {
  const latestProblem = deliveries.find(
    (delivery) => delivery.status === 'failed' || delivery.status === 'dead',
  )
  return {
    accepted: String(deliveries.filter((delivery) => delivery.status === 'accepted').length),
    dead: String(deliveries.filter((delivery) => delivery.status === 'dead').length),
    failed: String(deliveries.filter((delivery) => delivery.status === 'failed').length),
    latestDelivery: deliveries[0],
    latestProblem,
    pending: String(deliveries.filter((delivery) => delivery.status === 'pending').length),
    retryable: String(deliveries.filter((delivery) => delivery.retryable).length),
    total: String(deliveries.length),
  }
}

function consoleA11yLLMChannels(): LLMChannel[] {
  return [
    {
      authMode: 'bearer',
      baseUrl: 'https://api.openai.com/v1',
      createdAt: '2026-06-24T08:00:00Z',
      credentialKeyId: 'tink-key-a11y',
      hasApiKey: true,
      id: 'llm-a11y',
      lastError: '',
      lastTestStatus: 'pass',
      lastTestedAt: '2026-07-11T12:00:00Z',
      name: 'Primary LLM',
      priority: 1,
      protocol: 'openai-responses',
      status: 'enabled',
      timeoutSeconds: 30,
      updatedAt: '2026-07-11T12:00:00Z',
      weight: 1,
    },
  ]
}

function createExternalSyncState(): ExternalSyncMockState {
  const activeConnection: ExternalConnection = {
    id: 'external-sync-conn-a11y-active',
    tenantId: 'tenant-a11y',
    provider: 'github',
    name: 'GitHub Issues A11y',
    enabled: true,
    status: 'active',
    authType: 'token',
    baseUrl: '',
    providerInstallationId: '',
    providerConfigJson: '{"owner":"acme","repo":"console"}',
    scopes: ['issues'],
    lastTestedAt: '2026-07-08T02:00:00Z',
    lastTestStatus: 'ok',
    lastError: '',
    createdBy: 'user-a11y',
    updatedBy: 'user-a11y',
    createdAt: '2026-07-08T01:00:00Z',
    updatedAt: '2026-07-08T02:00:00Z',
    webhookSecretConfigured: true,
  }
  const quarantinedConnection: ExternalConnection = {
    id: 'external-sync-conn-a11y-quarantined',
    tenantId: 'tenant-a11y',
    provider: 'github',
    name: 'GitHub Issues Quarantined',
    enabled: false,
    status: 'quarantined',
    authType: 'token',
    baseUrl: '',
    providerInstallationId: '',
    providerConfigJson: '{"owner":"acme","repo":"legacy"}',
    scopes: ['issues'],
    lastTestedAt: '2026-07-08T03:00:00Z',
    lastTestStatus: 'failed',
    lastError: 'provider throttled after repeated 429 responses',
    createdBy: 'user-a11y',
    updatedBy: 'user-a11y',
    createdAt: '2026-07-08T01:30:00Z',
    updatedAt: '2026-07-08T03:00:00Z',
    webhookSecretConfigured: false,
  }
  const mapping: ExternalObjectMapping = {
    id: 'external-sync-mapping-a11y',
    tenantId: 'tenant-a11y',
    connectionId: activeConnection.id,
    localObjectType: 'customer_request',
    externalObjectType: 'issue',
    direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL,
    fieldMappingJson: '{"title":"title","status":"state","tags":"labels"}',
    statusMappingJson: '{"open":"open","done":"closed"}',
    conflictPolicy: 'manual',
    tombstonePolicy: 'mark_stale',
    enabled: true,
    mappingVersion: 3,
    createdAt: '2026-07-08T01:05:00Z',
    updatedAt: '2026-07-08T02:05:00Z',
  }
  const run: ExternalSyncRun = {
    id: 'external-sync-run-a11y-failed',
    tenantId: 'tenant-a11y',
    connectionId: activeConnection.id,
    mappingId: mapping.id,
    direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL,
    trigger: ExternalSyncRunTrigger.EXTERNAL_SYNC_RUN_TRIGGER_MANUAL,
    status: ExternalSyncRunStatus.EXTERNAL_SYNC_RUN_STATUS_FAILED,
    attempts: 3,
    nextRetryAt: '2026-07-08T03:10:00Z',
    startedAt: '2026-07-08T03:00:00Z',
    finishedAt: '2026-07-08T03:01:00Z',
    cursorBeforeJson: '{"since":"2026-07-08T02:00:00Z"}',
    cursorAfterJson: '{}',
    recordsSeen: 12,
    recordsChanged: 4,
    recordsFailed: 1,
    conflictsCreated: 1,
    errorKind: 'provider_throttled',
    errorMessage: 'GitHub returned Retry-After for issue sync',
    actorId: 'user-a11y',
    createdAt: '2026-07-08T03:00:00Z',
    updatedAt: '2026-07-08T03:01:00Z',
    inFlight: false,
  }
  const event: ExternalSyncEvent = {
    id: 'external-sync-event-a11y-failed',
    tenantId: 'tenant-a11y',
    connectionId: activeConnection.id,
    mappingId: mapping.id,
    provider: 'github',
    eventType: 'issues.edited',
    externalEventId: 'evt-a11y-214',
    dedupeKey: 'github:evt-a11y-214',
    signatureStatus: ExternalSyncEventSignatureStatus.EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_VERIFIED,
    status: ExternalSyncEventStatus.EXTERNAL_SYNC_EVENT_STATUS_FAILED,
    payloadDigest: 'sha256:a11yeventdigest000000000000000000000000000000000000000000',
    normalizedPayloadJson: '{"action":"edited","issue":{"number":214}}',
    receivedAt: '2026-07-08T03:02:00Z',
    replayedAt: '',
    replayedBy: '',
    runId: '',
    failureReason: 'mapping was disabled when the webhook arrived',
    createdAt: '2026-07-08T03:02:00Z',
    updatedAt: '2026-07-08T03:02:00Z',
  }

  return {
    connections: [activeConnection, quarantinedConnection],
    events: [event],
    health: {
      activeRuns: 0,
      deadRuns: 1,
      degradedConnections: 1,
      delayedRetryRuns: 1,
      disabledConnections: 1,
      enabledConnections: 1,
      failingConnections: 1,
      newestRetryAfter: '2026-07-08T03:10:00Z',
      newestSuccessfulRunAt: '2026-07-08T02:45:00Z',
      openConflicts: 1,
      providerUnavailableRuns: 0,
      quarantinedConnections: 1,
      retryableRuns: 1,
      staleConnections: 0,
      throttledRuns: 1,
      unauthorizedRuns: 0,
    },
    installations: [],
    mappings: [mapping],
    resourcesByInstallation: {},
    runs: [run],
    schemasByConnection: {
      [activeConnection.id]: [
        {
          type: 'issue',
          fields: ['number', 'title', 'state', 'labels', 'updated_at'],
          requiredFields: ['title'],
          writableFields: ['title', 'state', 'labels'],
        },
      ],
      [quarantinedConnection.id]: [
        {
          type: 'issue',
          fields: ['number', 'title', 'state', 'labels', 'updated_at'],
          requiredFields: ['title'],
          writableFields: ['title', 'state', 'labels'],
        },
      ],
    },
  }
}

function createExternalProviderInstallation(
  state: ExternalSyncMockState,
  body: CreateExternalProviderInstallationRequest,
): ExternalProviderInstallation {
  const createdAt = '2026-07-22T08:00:00Z'
  const installation: ExternalProviderInstallation = {
    id: `external-sync-provider-installation-${state.installations.length + 1}`,
    tenantId: consoleA11yMe.tenant.id,
    provider: body.provider,
    displayName: body.displayName,
    installationKind: body.installationKind,
    status: 'active',
    externalInstallationId: body.externalInstallationId,
    accountLogin: body.accountLogin,
    accountId: body.accountId,
    accountUrl: body.accountUrl,
    baseUrl: body.baseUrl,
    permissionsJson: body.permissionsJson,
    capabilityProfileJson: body.capabilityProfileJson || '{}',
    resourceSelection: body.resourceSelection,
    qualificationStatus: 'untested',
    lastQualifiedAt: '',
    lastError: '',
    createdBy: 'user-a11y',
    updatedBy: 'user-a11y',
    createdAt,
    updatedAt: createdAt,
  }
  state.installations = [...state.installations, installation]
  state.resourcesByInstallation[installation.id] = body.resources.map((resource, index) =>
    createExternalProviderInstallationResource(installation, resource, index),
  )
  return installation
}

function createExternalProviderInstallationResource(
  installation: ExternalProviderInstallation,
  input: ExternalProviderInstallationResourceInput,
  index: number,
): ExternalProviderInstallationResource {
  return {
    id: `${installation.id}-resource-${index + 1}`,
    tenantId: installation.tenantId,
    installationId: installation.id,
    provider: installation.provider,
    resourceType: input.resourceType,
    externalResourceId: input.externalResourceId,
    resourceKey: input.resourceKey,
    displayName: input.displayName,
    htmlUrl: input.htmlUrl,
    selected: input.selected,
    status: input.status,
    permissionsJson: input.permissionsJson,
    lastSeenAt: '2026-07-22T08:00:00Z',
    createdAt: '2026-07-22T08:00:00Z',
    updatedAt: '2026-07-22T08:00:00Z',
  }
}

function createExternalConnection(
  state: ExternalSyncMockState,
  body: CreateExternalConnectionRequest,
): ExternalConnection {
  const createdAt = '2026-07-22T08:02:00Z'
  const enabled = body.enabled ?? true
  const connection: ExternalConnection = {
    id: `external-sync-conn-a11y-created-${state.connections.length + 1}`,
    tenantId: consoleA11yMe.tenant.id,
    provider: body.provider,
    name: body.name,
    enabled,
    status: enabled ? 'active' : 'disabled',
    authType: body.authType,
    baseUrl: body.baseUrl,
    providerInstallationId: body.providerInstallationId,
    providerConfigJson: body.providerConfigJson,
    scopes: body.scopes,
    lastTestedAt: '',
    lastTestStatus: 'untested',
    lastError: '',
    createdBy: 'user-a11y',
    updatedBy: 'user-a11y',
    createdAt,
    updatedAt: createdAt,
    webhookSecretConfigured: body.webhookSecret.length > 0,
  }
  state.connections = [...state.connections, connection]
  return connection
}

function qualifyExternalProviderInstallation(state: ExternalSyncMockState, installationID: string) {
  const installation = state.installations.find((item) => item.id === installationID)
  if (!installation) return null

  const resources = state.resourcesByInstallation[installationID] ?? []
  const selectedCount = resources.filter((resource) => resource.selected).length
  const permissionProfileOK =
    installation.permissionsJson.includes('"metadata":"read"') &&
    installation.permissionsJson.includes('"issues":"write"')
  const ready =
    installation.provider === 'github' &&
    installation.externalInstallationId !== '' &&
    permissionProfileOK &&
    selectedCount > 0
  const grade = ready ? 'full_app' : 'blocked'
  const status = ready ? 'ok' : 'failed'
  const lastQualifiedAt = '2026-07-22T08:01:00Z'
  const updatedInstallation: ExternalProviderInstallation = {
    ...installation,
    capabilityProfileJson: JSON.stringify({
      grade,
      ready,
      check_counts: ready ? { ok: 3 } : { failed: 1, ok: 2 },
      resource_count: resources.length,
      selected_resources: selectedCount,
    }),
    qualificationStatus: status,
    lastQualifiedAt,
    lastError: ready ? '' : 'Installation is not ready for issue sync',
    updatedAt: lastQualifiedAt,
  }
  state.installations = state.installations.map((item) =>
    item.id === installationID ? updatedInstallation : item,
  )
  return {
    installationId: installationID,
    ready,
    grade,
    installation: updatedInstallation,
    checks: [
      {
        name: 'provider_registered',
        status: 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_OK',
        summary: 'Provider adapter is registered',
        detailJson: '{}',
      },
      {
        name: 'permission_profile',
        status: permissionProfileOK
          ? 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_OK'
          : 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_FAILED',
        summary: permissionProfileOK
          ? 'GitHub installation exposes required issue-sync permissions'
          : 'GitHub installation is missing required issue-sync permissions',
        detailJson: '{}',
      },
      {
        name: 'resource_selection',
        status:
          selectedCount > 0
            ? 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_OK'
            : 'EXTERNAL_SYNC_QUALIFICATION_CHECK_STATUS_FAILED',
        summary:
          selectedCount > 0
            ? 'Installation has selected provider resources'
            : 'No provider resources are selected for sync',
        detailJson: JSON.stringify({ selected_resources: selectedCount }),
      },
    ],
  }
}

function selectExternalProviderInstallationResources(
  state: ExternalSyncMockState,
  installationID: string,
  resourceIDs: string[],
) {
  const resources = state.resourcesByInstallation[installationID]
  if (!resources) return null
  const selected = new Set(resourceIDs)
  const updatedResources = resources.map((resource) => ({
    ...resource,
    selected: selected.has(resource.id),
    updatedAt: '2026-07-22T08:02:00Z',
  }))
  state.resourcesByInstallation[installationID] = updatedResources
  state.installations = state.installations.map((installation) =>
    installation.id === installationID
      ? {
          ...installation,
          resourceSelection: selected.size > 0 ? 'selected' : 'none',
          updatedAt: '2026-07-22T08:02:00Z',
        }
      : installation,
  )
  return updatedResources
}

function providerInstallationIDFromPath(path: string) {
  return path.split('/')[3].split(':')[0]
}

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
}

function insertServiceAccount(items: ServiceAccount[], account: ServiceAccount): ServiceAccount[] {
  return [...items.filter((item) => item.id !== account.id), account].sort((a, b) =>
    a.name.localeCompare(b.name),
  )
}

function upsertSavedAuditView(
  items: SavedAuditLogView[],
  view: SavedAuditLogView,
): SavedAuditLogView[] {
  return [view, ...items.filter((item) => item.id !== view.id)]
}

function readJsonBody(route: Route) {
  try {
    return route.request().postDataJSON()
  } catch {
    return null
  }
}

function semanticSearchResponse(body: unknown) {
  const request = body as { q?: string; filter?: { terminalFailedOnly?: boolean } } | null
  const terminalOnly = request?.filter?.terminalFailedOnly === true
  const fallback = request?.q?.toLowerCase().includes('fallback') ?? false
  const feedback = terminalOnly ? consoleA11yFeedbackItems[1] : consoleA11yFeedbackItems[0]
  return {
    hits: [
      {
        feedback,
        similarity: fallback ? 0 : 0.91,
        keywordScore: fallback ? 0.88 : 0.74,
        matchType: fallback ? 'keyword' : terminalOnly ? 'semantic' : 'hybrid',
        semanticRank: fallback ? 0 : 1,
        lexicalRank: fallback ? 1 : terminalOnly ? 0 : 1,
        fusedScore: fallback ? 0.0049 : 0.0163,
        evidence: [
          {
            field: terminalOnly ? 'title' : 'content',
            snippet: terminalOnly
              ? 'Terminal enrichment failure sample with exhausted upstream retries.'
              : 'Keyboard focus disappears after closing the detail panel.',
            reason: fallback ? 'lexical_match' : 'semantic_match',
          },
        ],
        rankingSignals: fallback
          ? ['lexical', 'rrf', 'keyword_fallback']
          : terminalOnly
            ? ['semantic', 'rrf']
            : ['semantic', 'lexical', 'rrf'],
      },
    ],
    embeddingModel: fallback ? '' : 'text-embedding-3-small',
    totalWithEmbeddings: 2,
    usedKeywordFallback: fallback,
    fallbackReason: fallback ? 'embedding_generation_failed' : undefined,
    rankingVersion: 'rrf.pgfts.v1.k60',
    coverage: {
      totalLiveFeedback: 2,
      totalWithEmbeddings: 2,
      embeddingModel: fallback ? '' : 'text-embedding-3-small',
    },
  }
}

function assignmentOwner(
  member: (typeof consoleA11yMembers)[number] | undefined,
): FeedbackAssignment['owner'] {
  if (!member) {
    return undefined
  }
  return {
    memberId: member.id,
    memberType: member.memberType,
    userId: member.userId,
    email: member.email,
    role: member.role,
  }
}

function assignmentRecommendation(
  detail: FeedbackDetail,
  policy: FeedbackAssignmentPolicy,
): FeedbackAssignmentRecommendation | null {
  const urgent = detail.isUrgent
  const ruleKey = urgent ? 'urgent_open' : 'untriaged'
  const fallback = urgent
    ? assignmentPolicyRule('urgent_open', 'Urgent open feedback', 'support_triage', 'critical', 24)
    : assignmentPolicyRule('untriaged', 'Untriaged intake', 'triage_dri', 'high', 72)
  const configuredRule = policy.rules.find((item) => item.ruleKey === ruleKey)
  if (configuredRule && !configuredRule.enabled) {
    return null
  }
  const rule = configuredRule ?? fallback
  return {
    feedbackId: String(detail.id),
    ruleKey,
    ruleName: rule.ruleName,
    ownerLane: rule.ownerLane,
    severity: rule.severity,
    slaHours: rule.slaHours,
    recommendedSlaDueAt: recommendedDueAt(rule.slaHours),
    rationale: 'Mocked assignment policy recommendation.',
    alreadySatisfied: false,
    currentAssignment: detail.assignment,
    recommendedOwnerMemberId: rule.defaultOwnerMemberId,
  }
}

function assignmentPolicyDryRunImpact(
  feedbackId: string,
  current: FeedbackAssignmentRecommendation | null,
  draft: FeedbackAssignmentRecommendation | null,
) {
  return {
    feedbackId,
    currentRuleKey: current?.ruleKey ?? '',
    currentRuleName: current?.ruleName ?? '',
    currentOwnerLane: current?.ownerLane ?? '',
    currentSlaHours: current?.slaHours ?? 0,
    currentOwnerMemberId: current?.recommendedOwnerMemberId,
    draftRuleKey: draft?.ruleKey ?? '',
    draftRuleName: draft?.ruleName ?? '',
    draftOwnerLane: draft?.ownerLane ?? '',
    draftSlaHours: draft?.slaHours ?? 0,
    draftOwnerMemberId: draft?.recommendedOwnerMemberId,
    changed:
      current?.ruleKey !== draft?.ruleKey ||
      current?.ownerLane !== draft?.ownerLane ||
      current?.slaHours !== draft?.slaHours ||
      current?.recommendedOwnerMemberId !== draft?.recommendedOwnerMemberId,
  }
}

function recommendedDueAt(slaHours: number): string {
  const dueAt = new Date('2026-08-01T09:30:00Z')
  dueAt.setUTCHours(dueAt.getUTCHours() + slaHours)
  return dueAt.toISOString().replace('.000Z', 'Z')
}

async function fulfillJson(route: Route, payload: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    body: JSON.stringify(payload),
  })
}

async function fulfillError(route: Route, message: string, status: number) {
  await fulfillJson(
    route,
    {
      code: 'A11Y_TEST_FAILURE',
      message,
      requestId: 'req-a11y-failure',
    },
    status,
  )
}

async function fulfillBytes(route: Route, filename: string, contentType: string) {
  await route.fulfill({
    status: 200,
    contentType,
    headers: {
      'Content-Disposition': `attachment; filename="${filename}"`,
    },
    body: Buffer.from([1, 2, 3, 4]),
  })
}
