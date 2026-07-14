import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { setCsrfToken } from '@/lib/api-client'
import { PublicSurface, type PublicVisibilityViewState } from '@/proto/attune/v1/public_visibility'
import { server } from '@/testing/mocks/server'
import {
  createPublicVisibilitySavedView,
  deletePublicVisibilitySavedView,
  publicVisibilitySavedViewsQuery,
  updatePublicVisibilitySavedView,
} from './saved-views'

beforeEach(() => {
  setCsrfToken('csrf-test-token')
})

afterEach(() => {
  setCsrfToken(null)
})

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

describe('public visibility saved views API', () => {
  it('fetches saved views with a stable query key', async () => {
    const seen: Array<{ path: string; method: string }> = []
    server.use(
      http.get('/fb/v1/console/public-visibility/views', ({ request }) => {
        seen.push({ path: new URL(request.url).pathname, method: request.method })
        return HttpResponse.json({
          views: [
            {
              id: 'view-1',
              name: 'Pending requests',
              state: {
                queueView: 'pending',
                surfaces: [PublicSurface.PUBLIC_SURFACE_REQUEST],
              },
              createdAt: '2026-07-10T00:00:00Z',
              updatedAt: '2026-07-10T00:00:00Z',
            },
          ],
        })
      }),
    )

    const qc = makeQueryClient()
    await expect(qc.fetchQuery(publicVisibilitySavedViewsQuery())).resolves.toEqual({
      views: [
        {
          id: 'view-1',
          name: 'Pending requests',
          state: {
            queueView: 'pending',
            surfaces: [PublicSurface.PUBLIC_SURFACE_REQUEST],
          },
          createdAt: '2026-07-10T00:00:00Z',
          updatedAt: '2026-07-10T00:00:00Z',
        },
      ],
    })
    expect(publicVisibilitySavedViewsQuery().queryKey).toEqual([
      'console',
      'public-visibility',
      'views',
    ])
    expect(seen).toEqual([{ path: '/fb/v1/console/public-visibility/views', method: 'GET' }])
  })

  it('creates, updates, and deletes saved views through the encoded console endpoints', async () => {
    const requests: Array<{
      method: string
      path: string
      body?: unknown
      csrf: string | null
    }> = []
    const state: PublicVisibilityViewState = {
      queueView: 'blocked',
      surfaces: [
        PublicSurface.PUBLIC_SURFACE_REQUEST_COMMENT,
        PublicSurface.PUBLIC_SURFACE_REQUEST,
      ],
    }
    server.use(
      http.post('/fb/v1/console/public-visibility/views', async ({ request }) => {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
          csrf: request.headers.get('x-csrf-token'),
        })
        return HttpResponse.json({
          view: {
            id: 'view-1',
            name: 'Pending requests',
            state,
            createdAt: '2026-07-10T00:00:00Z',
            updatedAt: '2026-07-10T00:00:00Z',
          },
        })
      }),
      http.put('/fb/v1/console/public-visibility/views/:id', async ({ request }) => {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          body: await request.json(),
          csrf: request.headers.get('x-csrf-token'),
        })
        return HttpResponse.json({
          view: {
            id: 'view-1',
            name: 'Blocked requests',
            state: {
              queueView: 'blocked',
              surfaces: [PublicSurface.PUBLIC_SURFACE_REQUEST],
            },
            createdAt: '2026-07-10T00:00:00Z',
            updatedAt: '2026-07-10T00:00:00Z',
          },
        })
      }),
      http.delete('/fb/v1/console/public-visibility/views/:id', ({ request }) => {
        requests.push({
          method: request.method,
          path: new URL(request.url).pathname,
          csrf: request.headers.get('x-csrf-token'),
        })
        return HttpResponse.json({})
      }),
    )

    await expect(
      createPublicVisibilitySavedView({
        name: 'Pending requests',
        state,
      }),
    ).resolves.toEqual({
      view: {
        id: 'view-1',
        name: 'Pending requests',
        state,
        createdAt: '2026-07-10T00:00:00Z',
        updatedAt: '2026-07-10T00:00:00Z',
      },
    })
    await expect(
      updatePublicVisibilitySavedView({
        id: 'view-1',
        name: 'Blocked requests',
        state: {
          queueView: 'blocked',
          surfaces: [PublicSurface.PUBLIC_SURFACE_REQUEST],
        },
      }),
    ).resolves.toEqual({
      view: {
        id: 'view-1',
        name: 'Blocked requests',
        state: {
          queueView: 'blocked',
          surfaces: [PublicSurface.PUBLIC_SURFACE_REQUEST],
        },
        createdAt: '2026-07-10T00:00:00Z',
        updatedAt: '2026-07-10T00:00:00Z',
      },
    })
    await expect(deletePublicVisibilitySavedView('view-1')).resolves.toEqual({})

    expect(requests).toEqual([
      {
        method: 'POST',
        path: '/fb/v1/console/public-visibility/views',
        body: {
          name: 'Pending requests',
          state,
        },
        csrf: 'csrf-test-token',
      },
      {
        method: 'PUT',
        path: '/fb/v1/console/public-visibility/views/view-1',
        body: {
          name: 'Blocked requests',
          state: {
            queueView: 'blocked',
            surfaces: [PublicSurface.PUBLIC_SURFACE_REQUEST],
          },
        },
        csrf: 'csrf-test-token',
      },
      {
        method: 'DELETE',
        path: '/fb/v1/console/public-visibility/views/view-1',
        csrf: 'csrf-test-token',
      },
    ])
  })
})
