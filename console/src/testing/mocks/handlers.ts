import { HttpResponse, http } from 'msw'
import type { ApiKey, CreateApiKeyResponse, ListApiKeysResponse } from '@/proto/attune/v1/api_key'
import type { DigestSubscription } from '@/proto/attune/v1/digest_subscription'
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
import type {
  ListNotifyTargetsResponse,
  NotifyTarget,
  TestNotifyTargetResponse,
} from '@/proto/attune/v1/notify_target'
import type { GetMeResponse } from '@/proto/attune/v1/session'
import type { GetLLMUsageResponse } from '@/proto/attune/v1/usage'

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
}
export const defaultFeedbackStats: GetFeedbackStatsResponse = {
  periodStart: '',
  periodEnd: '',
  total: '0',
  dims: [],
  urgentCount: '0',
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
  http.get(`${BASE}/digest-subscription`, () => HttpResponse.json(sampleDigestSubscription)),
  http.put(`${BASE}/digest-subscription`, () => HttpResponse.json(sampleDigestSubscription)),
  http.delete(`${BASE}/digest-subscription`, () => new HttpResponse(null, { status: 204 })),
  http.post(`${BASE}/notify-targets/:id/test`, () =>
    HttpResponse.json(defaultTestNotifyTargetResponse),
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
  http.get(`${BASE}/llm-usage`, () => HttpResponse.json(defaultLLMUsage)),
]
