import { HttpResponse, http } from 'msw'

// Forward-friendly default handlers for every /fb/v1/console/* endpoint.
// Per §4-G of the design proposal: cover the full surface so adding a
// new endpoint in production immediately triggers a clear "unhandled
// request" failure if the matching test hasn't added a mock.
//
// Tests override these defaults with `server.use(http.<verb>(...))` for
// per-case shapes (error envelopes, paginated cursors, empty lists,
// 401s, etc.). The defaults here just need to be shape-correct enough
// to keep happy-path renders from blowing up on `JSON.parse`.

const BASE = '/fb/v1/console'

// Session ------------------------------------------------------------------
export const defaultMe = {
  tenant: { id: 't-1', name: 'Default Tenant', slug: 'default' },
  user: { id: 'u-1', email: 'tester@example.com', displayName: 'Tester' },
  csrfToken: 'csrf-test-token',
}

// API keys -----------------------------------------------------------------
// Field naming follows ListApiKeysResponse / CreateApiKeyResponse —
// list returns { items }, create wraps the new key under { key, secret }.
export const defaultApiKeysList = { items: [] as unknown[] }
export const defaultCreateApiKey = {
  key: {
    id: 'k-new',
    label: 'fresh',
    keyPrefix: 'sk_test_',
    createdAt: '2026-06-07T00:00:00Z',
    lastUsedAt: '',
    isActive: true,
  },
  secret: 'sk_test_secret_value_redacted_in_real_envs',
}

// Notify targets -----------------------------------------------------------
// Field naming follows ListNotifyTargetsResponse (`items`).
export const defaultNotifyTargetsList = { items: [] as unknown[] }

// Enrich config ------------------------------------------------------------
export const defaultEnrichConfig = {
  promptTemplate: '',
  defaultPromptTemplate: '',
  dimensions: [] as unknown[],
}
export const defaultGetEnrichConfig = { config: defaultEnrichConfig }
export const defaultPreviewEnrichPrompt = { renderedPrompt: '' }

// Feedback -----------------------------------------------------------------
export const defaultFeedbackList = { items: [] as unknown[], nextCursor: null as string | null }
export const defaultFeedbackDetail = {
  id: 'f-1',
  content: 'sample feedback content',
  enrichedTitle: 'Sample',
  enrichedRationale: '',
  enrichedAttrs: {} as Record<string, unknown>,
  enrichedAt: '2026-06-07T00:00:00Z',
  isUrgent: false,
  source: 'web',
  userId: '',
  pageUrl: '',
  createdAt: '2026-06-07T00:00:00Z',
  sourceMeta: null as Record<string, unknown> | null,
  attachments: [] as unknown[],
  enrichmentError: '',
}
export const defaultFeedbackStats = { total: 0, dims: [] as unknown[] }

export const handlers = [
  http.get(`${BASE}/me`, () => HttpResponse.json(defaultMe)),

  http.get(`${BASE}/install/start`, ({ request }) => {
    // Echo redirect_uri back for redirect tests.
    const url = new URL(request.url)
    const redirect = url.searchParams.get('redirect_uri') ?? '/'
    return new HttpResponse(null, { status: 302, headers: { Location: redirect } })
  }),

  http.get(`${BASE}/api-keys`, () => HttpResponse.json(defaultApiKeysList)),
  http.post(`${BASE}/api-keys`, () => HttpResponse.json(defaultCreateApiKey)),
  http.delete(`${BASE}/api-keys/:id`, () => new HttpResponse(null, { status: 204 })),

  http.get(`${BASE}/notify-targets`, () => HttpResponse.json(defaultNotifyTargetsList)),
  http.post(`${BASE}/notify-targets`, async ({ request }) =>
    HttpResponse.json({ id: 'nt-new', ...((await request.json()) as object) }),
  ),
  http.patch(`${BASE}/notify-targets/:id`, async ({ request, params }) =>
    HttpResponse.json({ id: params.id, ...((await request.json()) as object) }),
  ),
  http.delete(`${BASE}/notify-targets/:id`, () => new HttpResponse(null, { status: 204 })),
  http.post(`${BASE}/notify-targets/:id/test`, () => HttpResponse.json({ ok: true })),

  http.get(`${BASE}/enrich-config`, () => HttpResponse.json(defaultGetEnrichConfig)),
  http.put(`${BASE}/enrich-config`, async ({ request }) => {
    // The PUT body is UpdateEnrichConfigRequest = { promptTemplate?, dimensions };
    // the response wraps it in { config } via UpdateEnrichConfigResponse.
    const body = (await request.json()) as { promptTemplate?: string; dimensions?: unknown[] }
    return HttpResponse.json({
      config: {
        promptTemplate: body.promptTemplate ?? '',
        defaultPromptTemplate: '',
        dimensions: body.dimensions ?? [],
      },
    })
  }),
  http.post(`${BASE}/enrich-config/preview`, () => HttpResponse.json(defaultPreviewEnrichPrompt)),

  http.get(`${BASE}/feedback`, () => HttpResponse.json(defaultFeedbackList)),
  http.get(`${BASE}/feedback/stats`, () => HttpResponse.json(defaultFeedbackStats)),
  http.get(`${BASE}/feedback/:id`, ({ params }) =>
    HttpResponse.json({ ...defaultFeedbackDetail, id: params.id }),
  ),
]
