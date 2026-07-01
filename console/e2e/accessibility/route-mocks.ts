import type { Page, Route } from '@playwright/test'
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
  unhandledRequests: string[]
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
    unhandledRequests: [],
  }

  await page.route('**/fb/v1/console/**', async (route) => {
    const request = route.request()
    const url = new URL(request.url())
    const method = request.method()
    const path = url.pathname.slice(apiPrefix.length) || '/'

    if (await handleRoute(route, method, path, url, options)) {
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
        : consoleA11yFeedbackDetail
    await fulfillJson(route, detail)
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
