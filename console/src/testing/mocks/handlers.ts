import { HttpResponse, http } from 'msw'
import type { ApiKey, CreateApiKeyResponse, ListApiKeysResponse } from '@/proto/attune/v1/api_key'
import type {
  EnrichConfig,
  GetEnrichConfigResponse,
  PreviewEnrichPromptResponse,
  UpdateEnrichConfigResponse,
} from '@/proto/attune/v1/enrich_config'
import type {
  FeedbackDetail,
  GetFeedbackStatsResponse,
  ListFeedbackResponse,
} from '@/proto/attune/v1/ingest'
import type {
  ListNotifyTargetsResponse,
  NotifyTarget,
  TestNotifyTargetResponse,
} from '@/proto/attune/v1/notify_target'
import type { GetMeResponse } from '@/proto/attune/v1/session'

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

// API keys -----------------------------------------------------------------
export const defaultApiKeysList: ListApiKeysResponse = { items: [] }
const defaultIssuedKey: ApiKey = {
  id: 'k-new',
  label: 'fresh',
  keyPrefix: 'sk_test_',
  isActive: true,
  createdAt: '2026-06-07T00:00:00Z',
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

// Enrich config ------------------------------------------------------------
export const defaultEnrichConfig: EnrichConfig = {
  promptTemplate: undefined,
  defaultPromptTemplate: '',
  dimensions: [],
}
export const defaultGetEnrichConfig: GetEnrichConfigResponse = { config: defaultEnrichConfig }
export const defaultPreviewEnrichPrompt: PreviewEnrichPromptResponse = { renderedPrompt: '' }

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
  enrichmentStatus: 'done',
  createdAt: '2026-06-07T00:00:00Z',
  attachments: [],
  enrichedRationale: '',
  enrichedAt: '2026-06-07T00:00:00Z',
  enrichmentError: '',
}
export const defaultFeedbackStats: GetFeedbackStatsResponse = {
  periodStart: '',
  periodEnd: '',
  total: '0',
  dims: [],
  urgentCount: '0',
}

export const handlers = [
  http.get(`${BASE}/me`, () => HttpResponse.json(defaultMe)),

  http.get(`${BASE}/install/start`, ({ request }) => {
    const url = new URL(request.url)
    const redirect = url.searchParams.get('redirect_uri') ?? '/'
    return new HttpResponse(null, { status: 302, headers: { Location: redirect } })
  }),

  http.get(`${BASE}/api-keys`, () => HttpResponse.json(defaultApiKeysList)),
  http.post(`${BASE}/api-keys`, () => HttpResponse.json(defaultCreateApiKey)),
  http.delete(`${BASE}/api-keys/:id`, () => new HttpResponse(null, { status: 204 })),

  http.get(`${BASE}/notify-targets`, () => HttpResponse.json(defaultNotifyTargetsList)),
  http.post(`${BASE}/notify-targets`, () => HttpResponse.json(sampleNotifyTarget)),
  http.patch(`${BASE}/notify-targets/:id`, () => HttpResponse.json(sampleNotifyTarget)),
  http.delete(`${BASE}/notify-targets/:id`, () => new HttpResponse(null, { status: 204 })),
  http.post(`${BASE}/notify-targets/:id/test`, () =>
    HttpResponse.json(defaultTestNotifyTargetResponse),
  ),

  http.get(`${BASE}/enrich-config`, () => HttpResponse.json(defaultGetEnrichConfig)),
  http.put(`${BASE}/enrich-config`, async ({ request }) => {
    // The PUT body is UpdateEnrichConfigRequest = { promptTemplate?, dimensions };
    // the response wraps it in { config } via UpdateEnrichConfigResponse.
    const body = (await request.json()) as { promptTemplate?: string; dimensions?: unknown[] }
    const resp: UpdateEnrichConfigResponse = {
      config: {
        promptTemplate: body.promptTemplate,
        defaultPromptTemplate: '',
        dimensions: (body.dimensions ?? []) as EnrichConfig['dimensions'],
      },
    }
    return HttpResponse.json(resp)
  }),
  http.post(`${BASE}/enrich-config/preview`, () => HttpResponse.json(defaultPreviewEnrichPrompt)),

  http.get(`${BASE}/feedback`, () => HttpResponse.json(defaultFeedbackList)),
  http.get(`${BASE}/feedback/stats`, () => HttpResponse.json(defaultFeedbackStats)),
  http.get(`${BASE}/feedback/:id`, ({ params }) =>
    HttpResponse.json({ ...defaultFeedbackDetail, id: params.id }),
  ),
]
