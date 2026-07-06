import type {
  ListMCPClientsResponse,
  MCPClient,
  MCPClientDetailResponse,
  MCPClientTool,
  ReplaceMCPToolPoliciesResponse,
  UpdateMCPClientResponse,
} from '../../features/mcp-clients/api/types'
import type {
  ApiKey,
  CreateApiKeyResponse,
  ListApiKeysResponse,
  ListScopePresetsResponse,
  ListScopesResponse,
  ListServiceAccountsResponse,
  ServiceAccount,
} from '../../proto/attune/v1/api_key'
import type {
  AuditLogEntry,
  CreateAuditEvidenceExportResponse,
  GetAuditEvidenceExportResponse,
  ListAuditLogResponse,
} from '../../proto/attune/v1/audit'
import type { EnrichConfig, GetEnrichConfigResponse } from '../../proto/attune/v1/enrich_config'
import {
  type DeleteGdprSubjectResponse,
  type ExportGdprSubjectResponse,
  GdprExportStatus,
  type GdprExportStatusResponse,
  type GdprOperationsResponse,
  GdprRequestStatus,
  GdprRequestType,
  type ListGdprRequestsResponse,
  type RevokeGdprExportResponse,
  type VerifyGdprStepUpResponse,
} from '../../proto/attune/v1/gdpr'
import type {
  Feedback,
  FeedbackDetail,
  GetFeedbackStatsResponse,
  GetTerminalFailureWorkbenchResponse,
  ListFeedbackResponse,
  ReplyDraftWorkflow,
  ReplySendHook,
  ReplySendHookDelivery,
  RetryEnrichmentResponse,
} from '../../proto/attune/v1/ingest'
import {
  type ListDeliveriesResponse,
  type OutboxDelivery,
  OutboxFailureKind,
  type RetryDeliveryResponse,
} from '../../proto/attune/v1/outbox'
import type { GetMeResponse } from '../../proto/attune/v1/session'
import type { ListTagsResponse, Tag } from '../../proto/attune/v1/tag'
import type {
  ListAuditResponse,
  ListStatesResponse,
  WorkflowState,
} from '../../proto/attune/v1/workflow'

export const consoleA11yMe: GetMeResponse = {
  tenant: {
    id: 'tenant-a11y',
    name: 'A11y Tenant',
    slug: 'a11y',
    locale: 'zh-CN',
    timezone: 'Asia/Singapore',
  },
  user: {
    openId: 'user-a11y',
    name: 'A11y Admin',
    role: 'admin',
  },
  csrfToken: 'csrf-a11y',
}

export const consoleA11yAuthProviders = {
  providers: [{ type: 'local' }],
  oidc_only: false,
}

export const consoleA11yWorkflowStates: WorkflowState[] = [
  {
    id: 'state-open',
    name: 'open',
    color: '#2563eb',
    category: 'active',
    position: 1,
    isDefault: true,
    archived: false,
    createdAt: '2026-06-20T00:00:00Z',
    updatedAt: '2026-06-20T00:00:00Z',
    displayName: { entries: { default: 'Open' } },
  },
  {
    id: 'state-done',
    name: 'done',
    color: '#16a34a',
    category: 'terminal',
    position: 2,
    isDefault: false,
    archived: false,
    createdAt: '2026-06-20T00:00:00Z',
    updatedAt: '2026-06-20T00:00:00Z',
    displayName: { entries: { default: 'Done' } },
  },
]

export const consoleA11yWorkflowStatesResponse: ListStatesResponse = {
  states: consoleA11yWorkflowStates,
}

export const consoleA11yTags: Tag[] = [
  {
    id: 'tag-accessibility',
    name: 'accessibility',
    color: '#2563eb',
    description: 'Accessibility signal',
    usageCount: 4,
    archived: false,
    createdBy: 'admin',
    createdAt: '2026-06-20T00:00:00Z',
    updatedAt: '2026-06-20T00:00:00Z',
  },
]

export const consoleA11yTagsResponse: ListTagsResponse = {
  tags: consoleA11yTags,
}

export const consoleA11yEnrichConfig: EnrichConfig = {
  promptTemplate: undefined,
  defaultPromptTemplate: 'Summarize {{content}} with {{dimensions}}.',
  dimensions: [
    {
      name: 'severity',
      displayName: { entries: { default: 'Severity' } },
      kind: 'single',
      taxonomy: [
        { value: 'P0', displayName: { entries: { default: 'P0' } }, examples: [] },
        { value: 'P1', displayName: { entries: { default: 'P1' } }, examples: [] },
        { value: 'P2', displayName: { entries: { default: 'P2' } }, examples: [] },
      ],
      urgentSet: ['P0'],
      required: false,
      examples: [],
      extractionHint: '',
    },
  ],
  promptPolicy: {
    policyId: 'enrich.default',
    policyVersion: '1',
    promptVersion: 'enrich.default@1',
    promptFingerprint: 'sha256:a11y-prompt',
    schemaFingerprint: 'sha256:a11y-schema',
    mode: 'default',
    promptSource: 'built_in',
    templateLanguage: 'en',
    displayLocale: 'zh-CN',
    displayLanguageName: 'Simplified Chinese',
    variables: [],
    outputs: [],
    warnings: [],
  },
  policyConfig: {
    outputLanguagePolicy: 'source_and_display',
    titleMaxChars: 80,
    rationaleMaxChars: 160,
    displayFieldsRequired: true,
    tone: 'concise',
    domainGuidance: '',
  },
  promptVersions: [],
}

export const consoleA11yEnrichConfigResponse: GetEnrichConfigResponse = {
  config: consoleA11yEnrichConfig,
}

export const consoleA11yFeedbackItems: Feedback[] = [
  {
    id: 'feedback-101',
    content: 'Keyboard focus disappears after closing the detail panel.',
    source: 'web',
    type: 'bug',
    userId: 'customer-1',
    pageUrl: 'https://app.example.com/settings',
    enrichedTitle: 'Focus lost after detail close',
    enrichedDisplayTitle: 'Focus lost after detail close',
    enrichedAttrs: { severity: 'P0' },
    isUrgent: true,
    enrichmentStatus: 'done',
    createdAt: '2026-06-24T09:00:00Z',
    language: 'en',
    classificationConfidence: 0.92,
    tags: [consoleA11yTags[0]],
    workflowState: consoleA11yWorkflowStates[0],
    allowedNextStates: [consoleA11yWorkflowStates[1]],
  },
  {
    id: 'feedback-201',
    content: 'Terminal enrichment failure sample with exhausted upstream retries.',
    source: 'web',
    type: 'bug',
    userId: 'customer-2',
    pageUrl: 'https://app.example.com/inbox',
    enrichedTitle: 'Terminal enrichment failure',
    enrichedDisplayTitle: 'Terminal enrichment failure',
    enrichedAttrs: { severity: 'P1' },
    isUrgent: false,
    enrichmentStatus: 'failed',
    enrichmentAttempts: 5,
    enrichmentNextRetryAt: '',
    enrichmentFailureReasonClass: 'llm_err',
    enrichmentFailureModel: 'gpt-4.1',
    enrichmentFailureChannelId: 'channel-primary',
    enrichmentFailureChannelName: 'Primary',
    enrichmentFailureConfigFingerprint: 'sha256:a11y-config',
    enrichmentFailurePromptVersion: 'enrich.default@1',
    createdAt: '2026-06-24T08:00:00Z',
    language: 'en',
    classificationConfidence: 0.64,
    tags: [],
    workflowState: consoleA11yWorkflowStates[0],
    allowedNextStates: [consoleA11yWorkflowStates[1]],
  },
]

export const consoleA11yFeedbackList: ListFeedbackResponse = {
  items: consoleA11yFeedbackItems,
}

export const consoleA11yTerminalFeedbackList: ListFeedbackResponse = {
  items: [consoleA11yFeedbackItems[1]],
}

export const consoleA11yReplyDraftWorkflow: ReplyDraftWorkflow = {
  draftId: 'draft-a11y',
  feedbackId: 'feedback-101',
  cycleNo: 1,
  status: 'suggested',
  activeRevisionId: 'rev-ai-1',
  activeText:
    'Thanks for reporting the focus loss after closing the detail panel. We are investigating the focus restoration path.',
  revisions: [
    {
      id: 'rev-ai-1',
      draftId: 'draft-a11y',
      cycleNo: 1,
      revisionNo: 1,
      origin: 'ai',
      content:
        'Thanks for reporting the focus loss after closing the detail panel. We are investigating the focus restoration path.',
      createdBy: 'llm',
      createdAt: '2026-06-24T09:02:00Z',
    },
  ],
  events: [],
  allowedActions: ['edit', 'approve', 'reject', 'regenerate'],
  blockers: [],
  hookConfigured: true,
  generatedAt: '2026-06-24T09:02:00Z',
  generatedBy: 'llm',
  revision: '1',
  updatedAt: '2026-06-24T09:02:00Z',
}

export const consoleA11yFeedbackDetail: FeedbackDetail = {
  ...consoleA11yFeedbackItems[0],
  attachments: [],
  enrichmentError: '',
  enrichedAt: '2026-06-24T09:01:00Z',
  enrichedRationale: 'The report describes a focus-management regression.',
  enrichedDisplayRationale: 'The report describes a focus-management regression.',
  sourceMeta: { browser: 'Chromium' },
  replyDraft: consoleA11yReplyDraftWorkflow.activeText,
  replyDraftGeneratedAt: '2026-06-24T09:02:00Z',
  replyDraftEnabled: true,
  replyDraftWorkflow: consoleA11yReplyDraftWorkflow,
}

export const consoleA11yTerminalFeedbackDetail: FeedbackDetail = {
  ...consoleA11yFeedbackItems[1],
  attachments: [],
  enrichmentError: 'llm: upstream exhausted',
  enrichedAt: '',
  enrichedRationale: 'Terminal failure sample.',
  enrichedDisplayRationale: 'Terminal failure sample.',
  sourceMeta: { browser: 'Chromium' },
  replyDraftEnabled: false,
}

export const consoleA11yFeedbackStats: GetFeedbackStatsResponse = {
  periodStart: '2026-06-01T00:00:00Z',
  periodEnd: '2026-06-30T23:59:59Z',
  total: '2',
  urgentCount: '1',
  dims: [
    {
      dim: 'severity',
      top: [
        { value: 'P0', count: '1' },
        { value: 'P1', count: '1' },
      ],
    },
  ],
}

export const consoleA11yTerminalWorkbench: GetTerminalFailureWorkbenchResponse = {
  periodStart: '2026-06-01T00:00:00Z',
  periodEnd: '2026-06-30T23:59:59Z',
  totalTerminalFailures: '1',
  oldestCreatedAt: '2026-06-24T08:00:00Z',
  reasonClassClusters: [
    {
      key: 'llm_err',
      label: 'LLM upstream error',
      count: '1',
      oldestCreatedAt: '2026-06-24T08:00:00Z',
      newestCreatedAt: '2026-06-24T08:00:00Z',
      sampleFeedbackIds: ['feedback-201'],
      remediationHint: 'Check the provider channel and retry once stable.',
    },
  ],
  modelChannelClusters: [],
  configFingerprintClusters: [],
  ageBucketClusters: [],
}

export const consoleA11yRetryEnrichmentResponse: RetryEnrichmentResponse = {
  id: 'feedback-201',
  enrichmentStatus: 'failed',
  enrichmentAttempts: 0,
  enrichmentNextRetryAt: '2026-06-24T09:05:00Z',
}

export const consoleA11yWorkflowAudit: ListAuditResponse = {
  entries: [],
}

export const consoleA11yApiKey: ApiKey = {
  id: 'key-a11y',
  label: 'ci-accessibility',
  keyPrefix: 'ak_live_a11y',
  isActive: true,
  createdAt: '2026-06-21T00:00:00Z',
  lastUsedAt: '2026-06-24T07:00:00Z',
  scopes: ['feedback:read', 'ingest:write'],
  allowedCidrs: [],
  usageCount: '14',
  environment: 'production',
}

export const consoleA11yApiKeysList: ListApiKeysResponse = {
  items: [consoleA11yApiKey],
}

export const consoleA11yIssuedApiKey: CreateApiKeyResponse = {
  key: {
    ...consoleA11yApiKey,
    id: 'key-new-a11y',
    label: 'new-accessibility-key',
    keyPrefix: 'ak_live_new',
    createdAt: '2026-06-24T09:10:00Z',
    lastUsedAt: undefined,
    usageCount: '0',
  },
  secret: 'ak_live_secret_visible_once',
}

export const consoleA11yServiceAccount: ServiceAccount = {
  id: 'sa-a11y',
  name: 'ci-bot',
  description: 'deployment pipeline',
  isActive: true,
  createdAt: '2026-06-21T00:00:00Z',
  updatedAt: '2026-06-24T07:00:00Z',
}

export const consoleA11yServiceAccountsList: ListServiceAccountsResponse = {
  items: [],
}

export const consoleA11yApiKeyScopes: ListScopesResponse = {
  scopes: [
    {
      scope: 'feedback:read',
      resource: 'feedback',
      action: 'read',
      description: 'Read feedback',
      implies: [],
    },
    {
      scope: 'ingest:write',
      resource: 'ingest',
      action: 'write',
      description: 'Submit feedback',
      implies: [],
    },
  ],
}

export const consoleA11yApiKeyPresets: ListScopePresetsResponse = {
  presets: [
    {
      id: 'full_access',
      name: 'Full access',
      description: 'All available console API scopes.',
      scopes: ['feedback:read', 'ingest:write'],
    },
    {
      id: 'ingest_only',
      name: 'Ingest only',
      description: 'Submit feedback without read access.',
      scopes: ['ingest:write'],
    },
  ],
}

export const consoleA11yAuditEntry: AuditLogEntry = {
  id: 'audit-1',
  actorType: 'admin',
  actorId: 'user-a11y',
  actorEmail: 'admin@example.com',
  actorIp: '127.0.0.1',
  actorUserAgent: 'Playwright',
  action: 'api_key.create',
  targetType: 'api_key',
  targetId: 'key-a11y',
  summary: 'Created API key for browser accessibility verification.',
  beforeJson: '{}',
  afterJson: '{"label":"ci-accessibility"}',
  createdAt: '2026-06-24T09:20:00Z',
}

export const consoleA11yAuditLog: ListAuditLogResponse = {
  items: [consoleA11yAuditEntry],
}

export const consoleA11yAuditEvidenceCreate: CreateAuditEvidenceExportResponse = {
  jobId: 'audit-evidence-a11y',
  status: 'queued',
  retryAfterSeconds: 1,
}

export const consoleA11yAuditEvidenceReady: GetAuditEvidenceExportResponse = {
  jobId: 'audit-evidence-a11y',
  status: 'completed',
  totalEvents: 1,
  createdAt: '2026-06-24T09:20:00Z',
  completedAt: '2026-06-24T09:21:00Z',
  expiresAt: '2026-06-25T09:21:00Z',
  downloadPath: '/fb/v1/console/audit-log/evidence/audit-evidence-a11y/download',
  archiveFilename: 'audit-evidence-a11y.zip',
  retryAfterSeconds: 1,
}

export const consoleA11yReplySendHook: ReplySendHook = {
  id: 'reply-hook-a11y',
  name: 'Support reply bridge',
  enabled: true,
  urlHost: 'hooks.example.com',
  urlFingerprint: 'sha256:2e8bb7f6b3c0a11y9d84e5c24219f4266fded6c4',
  createdBy: 'user-a11y',
  updatedBy: 'user-a11y',
  createdAt: '2026-06-24T09:00:00Z',
  updatedAt: '2026-06-24T09:00:00Z',
}

export const consoleA11yReplySendHookDeliveries: ReplySendHookDelivery[] = [
  {
    id: 'reply-delivery-a11y-failed',
    hookId: 'reply-hook-a11y',
    hookHost: 'hooks.example.com',
    hookFingerprint: 'sha256:2e8bb7f6b3c0a11y9d84e5c24219f4266fded6c4',
    eventType: 'reply.test',
    status: 'failed',
    idempotencyKey: 'reply_test_a11y_failed',
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
  },
]

export const consoleA11yMcpClient: MCPClient = {
  id: 'client-a11y',
  name: 'browser-a11y-agent',
  redirect_uris: ['http://localhost:39123/callback'],
  scopes: ['mcp:read', 'mcp:write'],
  tool_policy_mode: 'legacy_allow_all',
  rate_limit_rpm: null,
  rate_limit_burst: null,
  created_at: '2026-06-21T00:00:00Z',
  created_by: 'admin',
}

export const consoleA11yMcpTools: MCPClientTool[] = [
  {
    name: 'list_feedback',
    kind: 'core',
    owner: 'feedback',
    enabled_by_default: true,
    deprecated: true,
    replacement: 'get_feedback',
    aliases: [
      {
        name: 'feedback.list',
        deprecated: true,
        replacement: 'list_feedback',
      },
    ],
    required_scope: 'mcp:read',
    risk: 'read',
    data_class: 'user_content',
    read_only_hint: true,
    destructive_hint: false,
    open_world_hint: false,
    default_rpm: 120,
    default_burst: 20,
    scope_granted: true,
    effective_allowed: true,
    effect: '',
    rate_limit_rpm: null,
    rate_limit_burst: null,
  },
  {
    name: 'update_workflow_state',
    kind: 'core',
    owner: 'workflow',
    enabled_by_default: true,
    deprecated: false,
    replacement: '',
    required_scope: 'mcp:write',
    risk: 'mutate',
    data_class: 'operational',
    read_only_hint: false,
    destructive_hint: false,
    open_world_hint: false,
    default_rpm: 60,
    default_burst: 10,
    scope_granted: true,
    effective_allowed: true,
    effect: '',
    rate_limit_rpm: null,
    rate_limit_burst: null,
  },
]

export const consoleA11yMcpClientsList: ListMCPClientsResponse = {
  clients: [consoleA11yMcpClient],
}

export const consoleA11yMcpClientDetail: MCPClientDetailResponse = {
  client: consoleA11yMcpClient,
  sessions: [
    {
      id: 'session-a11y',
      scopes: ['mcp:read', 'mcp:write'],
      last_tool_name: 'list_feedback',
      last_decision: 'allowed',
      last_ip: '127.0.0.1',
      last_user_agent: 'Playwright',
      last_active_at: '2026-06-24T09:00:00Z',
      created_at: '2026-06-24T08:00:00Z',
      closed_reason: '',
      closed_by: '',
    },
  ],
  refresh_grants: [
    {
      id: 'grant-a11y',
      session_id: 'session-a11y',
      scopes: ['mcp:read'],
      expires_at: '2026-06-25T08:00:00Z',
      created_at: '2026-06-24T08:00:00Z',
    },
  ],
  tools: consoleA11yMcpTools,
  connection: {
    server_url: 'https://attune.example.com/mcp',
    resource_url: 'https://attune.example.com/mcp/v1',
    oauth_issuer: 'https://attune.example.com/mcp/oauth',
    authorization_endpoint: 'https://attune.example.com/mcp/oauth/authorize',
    token_endpoint: 'https://attune.example.com/mcp/oauth/token',
    protected_resource_metadata_url:
      'https://attune.example.com/.well-known/oauth-protected-resource/mcp/v1',
    authorization_server_metadata_url:
      'https://attune.example.com/.well-known/oauth-authorization-server/mcp/oauth',
    openid_configuration_url:
      'https://attune.example.com/.well-known/openid-configuration/mcp/oauth',
    legacy_protected_resource_metadata_url:
      'https://attune.example.com/mcp/.well-known/oauth-protected-resource',
    legacy_authorization_server_metadata_url:
      'https://attune.example.com/mcp/oauth/.well-known/oauth-authorization-server',
    legacy_openid_configuration_url:
      'https://attune.example.com/mcp/oauth/.well-known/openid-configuration',
  },
}

export const consoleA11yMcpClientUpdate: UpdateMCPClientResponse = {
  client: {
    ...consoleA11yMcpClient,
    tool_policy_mode: 'allow_list',
  },
}

export const consoleA11yMcpToolPolicyUpdate: ReplaceMCPToolPoliciesResponse = {
  tools: consoleA11yMcpTools.map((tool) => ({
    ...tool,
    effect: tool.name === 'list_feedback' ? 'allow' : tool.effect,
  })),
}

export const consoleA11yGdprOperations: GdprOperationsResponse = {
  stepUp: {
    satisfied: false,
    passwordAllowed: true,
    method: 'password',
    ttlSeconds: 900,
  },
  exportTtlSeconds: 86_400,
  auditRetentionDays: 30,
  auditPruneIntervalSeconds: 3_600,
  queuedRequestCount: 1,
  activeRequestCount: 1,
  readyExportCount: 1,
  nextExportExpiryAt: '2026-06-25T09:00:00Z',
  hashedAuditResidue: true,
  backupsMayRetainUntilRotation: true,
  legalHoldSupported: false,
  deleteGraceWindowSeconds: 3_600,
  scheduledDeleteCount: 1,
}

export const consoleA11yGdprRequests: ListGdprRequestsResponse = {
  items: [
    {
      requestId: 'gdpr-export-a11y',
      requestType: GdprRequestType.GDPR_REQUEST_TYPE_EXPORT,
      status: GdprRequestStatus.GDPR_REQUEST_STATUS_COMPLETED,
      subjectKey: 'customer-a11y@example.com',
      subjectDisplay: 'customer-a11y@example.com',
      createdBy: 'admin',
      feedbackCount: 2,
      tagAssignmentCount: 1,
      feedbackAuditCount: 2,
      llmAuditCount: 1,
      outboxCount: 0,
      createdAt: '2026-06-24T09:00:00Z',
      completedAt: '2026-06-24T09:01:00Z',
      expiresAt: '2026-06-25T09:01:00Z',
      archiveFilename: 'customer-a11y-export.zip',
    },
    {
      requestId: 'gdpr-delete-a11y',
      requestType: GdprRequestType.GDPR_REQUEST_TYPE_DELETE,
      status: GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED,
      subjectKey: 'delete-a11y@example.com',
      subjectDisplay: 'delete-a11y@example.com',
      createdBy: 'admin',
      feedbackCount: 1,
      tagAssignmentCount: 0,
      feedbackAuditCount: 1,
      llmAuditCount: 0,
      outboxCount: 1,
      createdAt: '2026-06-24T08:30:00Z',
      executeAfter: '2026-06-24T10:30:00Z',
    },
  ],
}

export const consoleA11yGdprStepUpVerified: VerifyGdprStepUpResponse = {
  stepUp: {
    satisfied: true,
    passwordAllowed: true,
    method: 'password',
    ttlSeconds: 900,
    verifiedAt: '2026-06-24T09:10:00Z',
    expiresAt: '2026-06-24T09:25:00Z',
  },
}

export const consoleA11yGdprExportStart: ExportGdprSubjectResponse = {
  jobId: 'gdpr-job-a11y',
  status: GdprExportStatus.GDPR_EXPORT_STATUS_QUEUED,
  retryAfterSeconds: 1,
}

export const consoleA11yGdprExportReady: GdprExportStatusResponse = {
  jobId: 'gdpr-job-a11y',
  subjectKey: 'customer-a11y@example.com',
  subjectDisplay: 'customer-a11y@example.com',
  status: GdprExportStatus.GDPR_EXPORT_STATUS_COMPLETED,
  retryAfterSeconds: 1,
  downloadPath: '/fb/v1/console/gdpr/exports/gdpr-job-a11y/download',
  archiveFilename: 'customer-a11y-export.zip',
  feedbackCount: 2,
  tagAssignmentCount: 1,
  feedbackAuditCount: 2,
  llmAuditCount: 1,
  createdAt: '2026-06-24T09:10:00Z',
  completedAt: '2026-06-24T09:11:00Z',
  expiresAt: '2026-06-25T09:11:00Z',
}

export const consoleA11yGdprDelete: DeleteGdprSubjectResponse = {
  requestId: 'gdpr-delete-new-a11y',
  status: GdprRequestStatus.GDPR_REQUEST_STATUS_SCHEDULED,
  executeAfter: '2026-06-24T10:30:00Z',
  subjectKey: 'delete-a11y@example.com',
  feedbackCount: 1,
  tagAssignmentCount: 0,
  feedbackAuditCount: 1,
  llmAuditCount: 0,
  outboxCount: 1,
}

export const consoleA11yGdprRevoke: RevokeGdprExportResponse = {
  jobId: 'gdpr-job-a11y',
  status: GdprExportStatus.GDPR_EXPORT_STATUS_REVOKED,
  requestStatus: GdprRequestStatus.GDPR_REQUEST_STATUS_REVOKED,
}

export const consoleA11yOutboxDelivery: OutboxDelivery = {
  id: 'delivery-a11y',
  feedbackId: 'feedback-201',
  destinationType: 'raw-webhook',
  destinationTarget: 'https://example.com/hooks/feedback',
  audience: 'all',
  status: 'dead',
  attempts: 6,
  failureKind: OutboxFailureKind.OUTBOX_FAILURE_KIND_HTTP_5XX,
  httpStatus: 503,
  lastError: 'upstream returned 503',
  deadReason: 'max attempts exhausted',
  traceId: 'trace-a11y',
  nextRetryAt: '',
  createdAt: '2026-06-24T08:00:00Z',
  deliveredAt: '',
  lastManualRetryAt: '',
  retriedBy: '',
  manualRetryCount: 0,
  inFlight: false,
}

export const consoleA11yOutboxDeliveries: ListDeliveriesResponse = {
  deliveries: [consoleA11yOutboxDelivery],
  nextBeforeId: '0',
}

export const consoleA11yOutboxRetry: RetryDeliveryResponse = {
  delivery: {
    ...consoleA11yOutboxDelivery,
    status: 'pending',
    attempts: 0,
    lastManualRetryAt: '2026-06-24T09:20:00Z',
  },
}
