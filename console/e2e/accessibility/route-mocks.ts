import type { Page, Route } from '@playwright/test'
import type { ServiceAccount } from '../../src/proto/attune/v1/api_key'
import type { AuditLogViewState, SavedAuditLogView } from '../../src/proto/attune/v1/audit'
import {
  type ExternalConnection,
  type ExternalObjectMapping,
  type ExternalObjectSchema,
  ExternalSyncDirection,
  type ExternalSyncEvent,
  ExternalSyncEventSignatureStatus,
  ExternalSyncEventStatus,
  type ExternalSyncHealthResponse,
  type ExternalSyncRun,
  ExternalSyncRunStatus,
  ExternalSyncRunTrigger,
} from '../../src/proto/attune/v1/external_sync'
import type {
  FeedbackDetail,
  ReplyDraftWorkflow,
  ReplySendHook,
  ReplySendHookDelivery,
  ReplySendHookHealth,
} from '../../src/proto/attune/v1/ingest'
import type { Tag } from '../../src/proto/attune/v1/tag'
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
  consoleA11yServiceAccount,
  consoleA11yServiceAccountsList,
  consoleA11yTagsResponse,
  consoleA11yTerminalFeedbackDetail,
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
    auditLogViews: [],
    feedbackDetails: {
      'feedback-101': feedbackDetailWithReplyDraft(
        clone(consoleA11yFeedbackDetail),
        consoleA11yReplyDraftWorkflow,
      ),
      'feedback-201': clone(consoleA11yTerminalFeedbackDetail),
    },
    replyDraftWorkflow: clone(consoleA11yReplyDraftWorkflow),
    replySendHook: clone(consoleA11yReplySendHook),
    replySendHookDeliveries: clone(consoleA11yReplySendHookDeliveries),
    externalSync: createExternalSyncState(),
    serviceAccounts: clone(consoleA11yServiceAccountsList.items),
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

  if (method === 'GET' && path === '/enrich-config') {
    await fulfillJson(route, consoleA11yEnrichConfigResponse)
    return true
  }
  if (method === 'GET' && path === '/tags') {
    await fulfillJson(route, { tags: state.tags })
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
    await fulfillJson(route, feedbackListResponse(state, terminalOnly))
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
  if (method === 'POST' && path.match(/^\/feedback\/[^/]+\/retry-enrichment$/)) {
    if (options.fail?.feedbackRetryEnrichment) {
      await fulfillError(route, options.fail.feedbackRetryEnrichment, 500)
      return true
    }
    await fulfillJson(route, consoleA11yRetryEnrichmentResponse, 202)
    return true
  }
  if (method === 'GET' && path.match(/^\/feedback\/[^/]+$/)) {
    const feedbackId = path.split('/')[2]
    const detail = state.feedbackDetails[feedbackId]
    await fulfillJson(route, detail ? clone(detail) : null, detail ? 200 : 404)
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

  if (method === 'GET' && path === '/api-keys') {
    await fulfillJson(route, consoleA11yApiKeysList)
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
  if (method === 'GET' && path === '/external-sync/connections') {
    await fulfillJson(route, { connections: clone(state.externalSync.connections) })
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
  replyDraftWorkflow: ReplyDraftWorkflow
  replySendHook: ReplySendHook
  replySendHookDeliveries: ReplySendHookDelivery[]
  externalSync: ExternalSyncMockState
  serviceAccounts: ServiceAccount[]
  tags: Tag[]
}

type ExternalSyncMockState = {
  connections: ExternalConnection[]
  events: ExternalSyncEvent[]
  health: ExternalSyncHealthResponse
  mappings: ExternalObjectMapping[]
  runs: ExternalSyncRun[]
  schemasByConnection: Record<string, ExternalObjectSchema[]>
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

function syncReplyDraftFeedbackDetail(state: ApiMockState) {
  const detail = state.feedbackDetails['feedback-101']
  if (!detail) return
  state.feedbackDetails['feedback-101'] = feedbackDetailWithReplyDraft(
    detail,
    state.replyDraftWorkflow,
  )
}

function feedbackListResponse(state: ApiMockState, terminalOnly: boolean) {
  const items = terminalOnly
    ? [state.feedbackDetails['feedback-201']]
    : [state.feedbackDetails['feedback-101'], state.feedbackDetails['feedback-201']]
  return {
    items: items.map((item) => clone(item)),
  }
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
    mappings: [mapping],
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
