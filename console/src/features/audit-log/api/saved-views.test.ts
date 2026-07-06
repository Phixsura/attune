import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import {
  createSavedAuditLogView,
  deleteSavedAuditLogView,
  savedAuditLogViewsQuery,
  updateSavedAuditLogView,
} from '@/features/audit-log/api/saved-views'
import { server } from '@/testing/mocks/server'

describe('savedAuditLogViewsQuery', () => {
  it('loads saved investigation views', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log/views', () =>
        HttpResponse.json({
          items: [
            {
              id: 'view-1',
              name: '成员删除排查',
              state: {
                actions: ['member.remove'],
                actorType: '',
                actorId: 'user-1',
                targetType: 'member',
                targetId: 'member-42',
                from: '',
                to: '',
                localQuery: 'playwright',
              },
              createdAt: '2026-06-16T10:00:00Z',
              updatedAt: '2026-06-16T10:05:00Z',
            },
          ],
        }),
      ),
    )

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const result = await qc.fetchQuery(savedAuditLogViewsQuery())

    expect(result.items).toHaveLength(1)
    expect(result.items[0]).toMatchObject({
      id: 'view-1',
      name: '成员删除排查',
    })
  })
})

describe('saved audit log view mutations', () => {
  it('creates a saved view with the expected payload', async () => {
    let payload: unknown
    server.use(
      http.post('/fb/v1/console/audit-log/views', async ({ request }) => {
        payload = await request.json()
        return HttpResponse.json({
          view: {
            id: 'view-1',
            name: '成员删除排查',
            state: {
              actions: ['member.remove'],
              actorType: '',
              actorId: 'user-1',
              targetType: 'member',
              targetId: 'member-42',
              from: '',
              to: '',
              localQuery: 'playwright',
            },
            createdAt: '2026-06-16T10:00:00Z',
            updatedAt: '2026-06-16T10:00:00Z',
          },
        })
      }),
    )

    const response = await createSavedAuditLogView({
      name: '成员删除排查',
      state: {
        actions: ['member.remove'],
        actorType: '',
        actorId: 'user-1',
        targetType: 'member',
        targetId: 'member-42',
        from: '',
        to: '',
        localQuery: 'playwright',
      },
    })

    expect(payload).toEqual({
      name: '成员删除排查',
      state: {
        actions: ['member.remove'],
        actorType: '',
        actorId: 'user-1',
        targetType: 'member',
        targetId: 'member-42',
        from: '',
        to: '',
        localQuery: 'playwright',
      },
    })
    expect(response.view?.id).toBe('view-1')
  })

  it('updates a saved view at the expected path', async () => {
    let payload: unknown
    server.use(
      http.put('/fb/v1/console/audit-log/views/view-1', async ({ request }) => {
        payload = await request.json()
        return HttpResponse.json({
          view: {
            id: 'view-1',
            name: '成员删除排查 - v2',
            state: {
              actions: ['member.remove'],
              actorType: '',
              actorId: 'user-2',
              targetType: 'member',
              targetId: 'member-42',
              from: '',
              to: '',
              localQuery: 'playwright',
            },
            createdAt: '2026-06-16T10:00:00Z',
            updatedAt: '2026-06-16T10:10:00Z',
          },
        })
      }),
    )

    const response = await updateSavedAuditLogView({
      id: 'view-1',
      name: '成员删除排查 - v2',
      state: {
        actions: ['member.remove'],
        actorType: '',
        actorId: 'user-2',
        targetType: 'member',
        targetId: 'member-42',
        from: '',
        to: '',
        localQuery: 'playwright',
      },
    })

    expect(payload).toEqual({
      name: '成员删除排查 - v2',
      state: {
        actions: ['member.remove'],
        actorType: '',
        actorId: 'user-2',
        targetType: 'member',
        targetId: 'member-42',
        from: '',
        to: '',
        localQuery: 'playwright',
      },
    })
    expect(response.view?.name).toBe('成员删除排查 - v2')
  })

  it('deletes a saved view by id', async () => {
    server.use(http.delete('/fb/v1/console/audit-log/views/view-1', () => HttpResponse.json({})))

    await expect(deleteSavedAuditLogView('view-1')).resolves.toEqual({})
  })
})
