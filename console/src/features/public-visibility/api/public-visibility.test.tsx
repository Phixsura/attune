import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { setCsrfToken } from '@/lib/api-client'
import {
  ModerationState,
  PublicAccessMode,
  PublicIdentityMode,
  type PublicRequestPublication,
  PublicSurface,
  type PublicVisibilityPolicy,
  PublicWriteMode,
} from '@/proto/attune/v1/public_visibility'
import { server } from '@/testing/mocks/server'
import {
  approveModerationSubject,
  getPublicRequestProfile,
  hideModerationSubject,
  markModerationSubjectSpam,
  moderationSubjectsQuery,
  publicVisibilityPolicyQuery,
  publicVisibilityQueryKeys,
  rejectModerationSubject,
  restoreModerationSubject,
  updatePublicVisibilityPolicy,
  upsertPublicRequestProfile,
} from './public-visibility'

const policyFixture: PublicVisibilityPolicy = {
  tenantId: 'tenant-1',
  portalAccessMode: PublicAccessMode.PUBLIC_ACCESS_MODE_PUBLIC,
  searchIndexingEnabled: true,
  requestsEnabled: true,
  commentsEnabled: false,
  roadmapEnabled: true,
  changelogEnabled: false,
  submissionWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_IDENTIFIED,
  commentWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_DISABLED,
  voteWriteMode: PublicWriteMode.PUBLIC_WRITE_MODE_ANONYMOUS,
  defaultRequestState: ModerationState.MODERATION_STATE_PENDING,
  defaultCommentState: ModerationState.MODERATION_STATE_PENDING,
  submitterIdentityMode: PublicIdentityMode.PUBLIC_IDENTITY_MODE_DISPLAY_NAME,
  showVoteCount: true,
  showCommentCount: false,
  showSubmitterDisplay: true,
  hidePublicTimestamps: false,
  updatedBy: 'admin-1',
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

const subjectFixture = {
  id: 'moderation-1',
  tenantId: 'tenant-1',
  surface: PublicSurface.PUBLIC_SURFACE_REQUEST,
  subjectId: 'profile-1',
  state: ModerationState.MODERATION_STATE_PENDING,
  reasonCode: '',
  reasonNote: '',
  submittedByDisplay: '',
  reviewedBy: '',
  createdAt: '2026-07-10T00:00:00Z',
  updatedAt: '2026-07-10T00:00:00Z',
}

const publicationFixture: PublicRequestPublication = {
  profile: {
    id: 'profile-1',
    tenantId: 'tenant-1',
    requestId: 'request-1',
    publicSlug: 'billing-export',
    publicTitle: 'Billing export',
    publicSummary: 'Customers can export billing data.',
    publicState: 'planned',
    roadmapColumn: 'Next',
    includedInPortal: true,
    includedInRoadmap: true,
    updatedBy: 'admin-1',
    createdAt: '2026-07-10T00:00:00Z',
    updatedAt: '2026-07-10T00:00:00Z',
  },
  moderation: subjectFixture,
}

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

beforeEach(() => {
  setCsrfToken('csrf-test-token')
})

afterEach(() => {
  setCsrfToken(null)
})

describe('public visibility API client', () => {
  it('builds stable query keys and unwraps policy and moderation list responses', async () => {
    const seen: Array<{ path: string; query: string }> = []
    server.use(
      http.get('/fb/v1/console/public-visibility/policy', ({ request }) => {
        const url = new URL(request.url)
        seen.push({ path: url.pathname, query: url.search })
        return HttpResponse.json(policyFixture)
      }),
      http.get('/fb/v1/console/public-visibility/moderation', ({ request }) => {
        const url = new URL(request.url)
        seen.push({ path: url.pathname, query: url.search })
        return HttpResponse.json({ subjects: [subjectFixture], nextCursor: 'next' })
      }),
    )

    const qc = makeQueryClient()

    await expect(qc.fetchQuery(publicVisibilityPolicyQuery())).resolves.toEqual(policyFixture)
    await expect(qc.fetchQuery(moderationSubjectsQuery())).resolves.toEqual([subjectFixture])
    expect(publicVisibilityQueryKeys.policy()).toEqual(['console', 'public-visibility', 'policy'])
    expect(publicVisibilityQueryKeys.moderation()).toEqual([
      'console',
      'public-visibility',
      'moderation',
    ])
    expect(publicVisibilityQueryKeys.requestProfile('request-1')).toEqual([
      'console',
      'public-visibility',
      'request-profile',
      'request-1',
    ])
    expect(seen).toEqual([
      { path: '/fb/v1/console/public-visibility/policy', query: '' },
      { path: '/fb/v1/console/public-visibility/moderation', query: '?limit=50' },
    ])
  })

  it('sends policy and request profile mutations to encoded console endpoints', async () => {
    const requests: Array<{
      method: string
      path: string
      body?: unknown
      csrf: string | null
    }> = []
    server.use(
      http.put('/fb/v1/console/public-visibility/policy', async ({ request }) => {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
          csrf: request.headers.get('x-csrf-token'),
        })
        return HttpResponse.json(policyFixture)
      }),
      http.get('/fb/v1/console/public-visibility/requests/:requestId/profile', ({ request }) => {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          csrf: request.headers.get('x-csrf-token'),
        })
        return HttpResponse.json(publicationFixture)
      }),
      http.put(
        '/fb/v1/console/public-visibility/requests/:requestId/profile',
        async ({ request }) => {
          requests.push({
            method: request.method,
            path: new URL(request.url).pathname,
            body: await request.json(),
            csrf: request.headers.get('x-csrf-token'),
          })
          return HttpResponse.json(publicationFixture)
        },
      ),
    )

    await expect(updatePublicVisibilityPolicy(policyFixture)).resolves.toEqual(policyFixture)
    await expect(getPublicRequestProfile('request / one')).resolves.toEqual(publicationFixture)
    await expect(
      upsertPublicRequestProfile({
        requestId: 'request / one',
        publicSlug: 'billing-export',
        publicTitle: 'Billing export',
        publicSummary: 'Customers can export billing data.',
        publicState: 'planned',
        roadmapColumn: 'Next',
        includedInPortal: true,
        includedInRoadmap: true,
        submittedByDisplay: 'Jane',
      }),
    ).resolves.toEqual(publicationFixture)

    expect(requests).toEqual([
      {
        method: 'PUT',
        path: '/fb/v1/console/public-visibility/policy',
        body: policyFixture,
        csrf: 'csrf-test-token',
      },
      {
        method: 'GET',
        path: '/fb/v1/console/public-visibility/requests/request%20%2F%20one/profile',
        csrf: null,
      },
      {
        method: 'PUT',
        path: '/fb/v1/console/public-visibility/requests/request%20%2F%20one/profile',
        body: {
          requestId: 'request / one',
          publicSlug: 'billing-export',
          publicTitle: 'Billing export',
          publicSummary: 'Customers can export billing data.',
          publicState: 'planned',
          roadmapColumn: 'Next',
          includedInPortal: true,
          includedInRoadmap: true,
          submittedByDisplay: 'Jane',
        },
        csrf: 'csrf-test-token',
      },
    ])
  })

  it('posts moderation actions without leaking the subject id into action bodies', async () => {
    const requests: Array<{ method: string; path: string; body: unknown }> = []
    const recordAction = async (request: Request) => {
      requests.push({
        method: request.method,
        path: new URL(request.url).pathname,
        body: await request.json(),
      })
      return HttpResponse.json(subjectFixture)
    }
    server.use(
      http.post('/fb/v1/console/public-visibility/moderation/:id\\:approve', ({ request }) =>
        recordAction(request),
      ),
      http.post('/fb/v1/console/public-visibility/moderation/:id\\:reject', ({ request }) =>
        recordAction(request),
      ),
      http.post('/fb/v1/console/public-visibility/moderation/:id\\:hide', ({ request }) =>
        recordAction(request),
      ),
      http.post('/fb/v1/console/public-visibility/moderation/:id\\:mark-spam', ({ request }) =>
        recordAction(request),
      ),
      http.post('/fb/v1/console/public-visibility/moderation/:id\\:restore', ({ request }) =>
        recordAction(request),
      ),
    )

    await approveModerationSubject({
      id: 'moderation-1',
      reasonCode: 'operator.approved',
      reasonNote: 'safe to publish',
    })
    await rejectModerationSubject({
      id: 'moderation-2',
      reasonCode: 'policy.sensitive',
      reasonNote: 'contains private detail',
    })
    await hideModerationSubject({
      id: 'moderation-3',
      reasonCode: 'operator.hidden',
      reasonNote: '',
    })
    await markModerationSubjectSpam({
      id: 'moderation-4',
      reasonCode: 'abuse.spam',
      reasonNote: 'repeated submission',
    })
    await restoreModerationSubject({
      id: 'moderation-5',
    } as Parameters<typeof restoreModerationSubject>[0])

    expect(requests).toEqual([
      {
        method: 'POST',
        path: '/fb/v1/console/public-visibility/moderation/moderation-1:approve',
        body: { reasonCode: 'operator.approved', reasonNote: 'safe to publish' },
      },
      {
        method: 'POST',
        path: '/fb/v1/console/public-visibility/moderation/moderation-2:reject',
        body: { reasonCode: 'policy.sensitive', reasonNote: 'contains private detail' },
      },
      {
        method: 'POST',
        path: '/fb/v1/console/public-visibility/moderation/moderation-3:hide',
        body: { reasonCode: 'operator.hidden', reasonNote: '' },
      },
      {
        method: 'POST',
        path: '/fb/v1/console/public-visibility/moderation/moderation-4:mark-spam',
        body: { reasonCode: 'abuse.spam', reasonNote: 'repeated submission' },
      },
      {
        method: 'POST',
        path: '/fb/v1/console/public-visibility/moderation/moderation-5:restore',
        body: { reasonCode: '', reasonNote: '' },
      },
    ])
  })
})
