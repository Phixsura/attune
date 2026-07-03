import type { Page, Route } from '@playwright/test'
import type {
  FeedbackDetail,
  ReplyDraftWorkflow,
  ReplySendHook,
  ReplySendHookDelivery,
  ReplySendHookHealth,
} from '../../src/proto/attune/v1/ingest'
import {
  consoleA11yApiKeyPresets,
  consoleA11yApiKeyScopes,
  consoleA11yApiKeysList,
  consoleA11yAuditEvidenceCreate,
  consoleA11yAuditEvidenceReady,
  consoleA11yAuditLog,
  consoleA11yAuthProviders,
  consoleA11yEnrichConfigResponse,
  consoleA11yFeedbackDetail,
  consoleA11yFeedbackItems,
  consoleA11yFeedbackList,
  consoleA11yFeedbackStats,
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
  consoleA11yOutboxDeliveries,
  consoleA11yOutboxRetry,
  consoleA11yReplyDraftWorkflow,
  consoleA11yReplySendHook,
  consoleA11yReplySendHookDeliveries,
  consoleA11yRetryEnrichmentResponse,
  consoleA11yTagsResponse,
  consoleA11yTerminalFeedbackDetail,
  consoleA11yTerminalFeedbackList,
  consoleA11yTerminalWorkbench,
  consoleA11yWorkflowAudit,
  consoleA11yWorkflowStatesResponse,
} from '../../src/testing/fixtures/console-a11y-fixtures'

const apiPrefix = '/fb/v1/console'

export type ApiMockDiagnostics = {
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
    replyDraftRequests: [],
    replySendHookRequests: [],
    unhandledRequests: [],
    semanticSearchRequests: [],
  }
  const state: ApiMockState = {
    replyDraftWorkflow: clone(consoleA11yReplyDraftWorkflow),
    replySendHook: clone(consoleA11yReplySendHook),
    replySendHookDeliveries: clone(consoleA11yReplySendHookDeliveries),
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

  if (method === 'GET' && path === '/enrich-config') {
    await fulfillJson(route, consoleA11yEnrichConfigResponse)
    return true
  }
  if (method === 'GET' && path === '/tags') {
    await fulfillJson(route, consoleA11yTagsResponse)
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

  if (method === 'GET' && path === '/feedback') {
    const terminalOnly = url.searchParams.get('terminal_failed_only') === 'true'
    await fulfillJson(
      route,
      terminalOnly ? consoleA11yTerminalFeedbackList : consoleA11yFeedbackList,
    )
    return true
  }
  if (method === 'GET' && path === '/feedback/stats') {
    await fulfillJson(route, consoleA11yFeedbackStats)
    return true
  }
  if (method === 'POST' && path === '/feedback/search') {
    const body = readJsonBody(route)
    diagnostics.semanticSearchRequests.push(body)
    await fulfillJson(route, semanticSearchResponse(body))
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
  if (method === 'POST' && path.match(/^\/feedback\/[^/]+\/retry-enrichment$/)) {
    if (options.fail?.feedbackRetryEnrichment) {
      await fulfillError(route, options.fail.feedbackRetryEnrichment, 500)
      return true
    }
    await fulfillJson(route, consoleA11yRetryEnrichmentResponse, 202)
    return true
  }
  if (method === 'GET' && path.match(/^\/feedback\/[^/]+$/)) {
    const detail =
      path === '/feedback/feedback-201'
        ? consoleA11yTerminalFeedbackDetail
        : feedbackDetailWithReplyDraft(state.replyDraftWorkflow)
    await fulfillJson(route, detail)
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
    await fulfillJson(route, { workflow: state.replyDraftWorkflow })
    return true
  }
  if (method === 'POST' && path.match(/^\/feedback\/[^/]+\/reply-draft\/approve$/)) {
    const body = readJsonBody(route)
    diagnostics.replyDraftRequests.push({ method, path, body })
    state.replyDraftWorkflow = advanceReplyDraftWorkflow(state.replyDraftWorkflow, 'approved')
    await fulfillJson(route, { workflow: state.replyDraftWorkflow })
    return true
  }
  if (method === 'POST' && path.match(/^\/feedback\/[^/]+\/reply-draft\/reject$/)) {
    const body = readJsonBody(route)
    diagnostics.replyDraftRequests.push({ method, path, body })
    state.replyDraftWorkflow = advanceReplyDraftWorkflow(state.replyDraftWorkflow, 'rejected')
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
    await fulfillJson(route, { workflow: state.replyDraftWorkflow, fromCache: false })
    return true
  }

  if (method === 'GET' && path === '/api-keys') {
    await fulfillJson(route, consoleA11yApiKeysList)
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
  replyDraftWorkflow: ReplyDraftWorkflow
  replySendHook: ReplySendHook
  replySendHookDeliveries: ReplySendHookDelivery[]
}

function feedbackDetailWithReplyDraft(workflow: ReplyDraftWorkflow): FeedbackDetail {
  return {
    ...consoleA11yFeedbackDetail,
    replyDraft: workflow.activeText,
    replyDraftGeneratedAt: workflow.generatedAt ?? consoleA11yFeedbackDetail.replyDraftGeneratedAt,
    replyDraftWorkflow: workflow,
  }
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

function clone<T>(value: T): T {
  return JSON.parse(JSON.stringify(value)) as T
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
