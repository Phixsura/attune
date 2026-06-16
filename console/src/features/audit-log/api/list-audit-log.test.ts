import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { auditLogInfiniteQuery, downloadAuditLogCsv } from '@/features/audit-log/api/list-audit-log'
import { server } from '@/testing/mocks/server'

describe('auditLogInfiniteQuery', () => {
  it('returns audit log items from response', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log', () =>
        HttpResponse.json({
          items: [
            {
              id: '1',
              actorType: 'admin',
              actorId: 'user-1',
              action: 'api_key.create',
              targetType: 'api_key',
              targetId: 'key-1',
              summary: 'Created API key',
              createdAt: '2026-06-16T10:00:00Z',
            },
          ],
        }),
      ),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const page = await qc.fetchInfiniteQuery(auditLogInfiniteQuery({ limit: 100 }))
    expect(page.pages).toHaveLength(1)
    expect(page.pages[0].items).toHaveLength(1)
    expect(page.pages[0].items[0]).toMatchObject({ action: 'api_key.create', targetId: 'key-1' })
  })

  it('returns empty array when items is missing', async () => {
    server.use(http.get('/fb/v1/console/audit-log', () => HttpResponse.json({})))
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    const page = await qc.fetchInfiniteQuery(auditLogInfiniteQuery({}))
    expect(page.pages[0].items ?? []).toEqual([])
  })

  it('round-trips active filters into query params', async () => {
    let actorId = ''
    let targetType = ''
    let from = ''
    let actions: string[] = []
    server.use(
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        const url = new URL(request.url)
        actorId = url.searchParams.get('actorId') ?? ''
        targetType = url.searchParams.get('targetType') ?? ''
        from = url.searchParams.get('from') ?? ''
        actions = url.searchParams.getAll('action')
        return HttpResponse.json({ items: [] })
      }),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await qc.fetchInfiniteQuery(
      auditLogInfiniteQuery({
        actions: ['member.remove', 'member.invite'],
        actorId: 'user-7',
        from: '2026-06-16T00:00:00Z',
        targetType: 'member',
      }),
    )

    expect(actorId).toBe('user-7')
    expect(targetType).toBe('member')
    expect(from).toBe('2026-06-16T00:00:00Z')
    expect(actions).toEqual(['member.remove', 'member.invite'])
  })

  it('passes page cursor on subsequent pages', async () => {
    let cursor = ''
    server.use(
      http.get('/fb/v1/console/audit-log', ({ request }) => {
        cursor = new URL(request.url).searchParams.get('cursor') ?? ''
        return HttpResponse.json({
          items: [],
          nextCursor: cursor ? null : '1718539200000000000:42',
        })
      }),
    )
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })

    await qc.fetchInfiniteQuery({ ...auditLogInfiniteQuery({ limit: 2 }), pages: 2 })

    expect(cursor).toBe('1718539200000000000:42')
  })

  it('surfaces structured export errors', async () => {
    server.use(
      http.get('/fb/v1/console/audit-log/export.csv', () =>
        HttpResponse.json({ message: 'forbidden' }, { status: 403 }),
      ),
    )

    await expect(downloadAuditLogCsv({ actorId: 'user-7' })).rejects.toThrow('forbidden')
  })
})
