import { QueryClient } from '@tanstack/react-query'
import { HttpResponse, http } from 'msw'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { auditLogInfiniteQuery, downloadAuditLogCsv } from '@/features/audit-log/api/list-audit-log'
import { server } from '@/testing/mocks/server'

vi.mock('@/lib/blob-download', () => ({
  triggerBlobDownload: vi.fn(),
}))

import { triggerBlobDownload } from '@/lib/blob-download'

afterEach(() => {
  vi.clearAllMocks()
})

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
        actions: ['member.remove', ' ', 'member.invite'],
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

  it('downloads CSV exports with the server filename', async () => {
    let query = ''
    server.use(
      http.get('/fb/v1/console/audit-log/export.csv', ({ request }) => {
        query = new URL(request.url).search
        return new HttpResponse('id,action\n1,login', {
          status: 200,
          headers: {
            'Content-Disposition': 'attachment; filename="audit-log-admin.csv"',
            'Content-Type': 'text/csv',
          },
        })
      }),
    )

    await downloadAuditLogCsv({ actorId: 'user-7', actions: ['login'] })

    expect(query).toContain('actorId=user-7')
    expect(query).toContain('action=login')
    expect(triggerBlobDownload).toHaveBeenCalledTimes(1)
    const [blob, filename] = vi.mocked(triggerBlobDownload).mock.calls[0] ?? []
    expect(blob).toMatchObject({ size: 17, type: 'text/csv' })
    expect(filename).toBe('audit-log-admin.csv')
  })

  it('uses the default filename when the CSV response omits content disposition', async () => {
    server.use(
      http.get(
        '/fb/v1/console/audit-log/export.csv',
        () =>
          new HttpResponse('id,action', { status: 200, headers: { 'Content-Type': 'text/csv' } }),
      ),
    )

    await downloadAuditLogCsv({})

    const [, filename] = vi.mocked(triggerBlobDownload).mock.calls[0] ?? []
    expect(filename).toBe('audit-log.csv')
  })

  it('falls back to HTTP status for empty or non-JSON export errors', async () => {
    server.use(
      http.get(
        '/fb/v1/console/audit-log/export.csv',
        () => new HttpResponse(null, { status: 404 }),
      ),
    )
    await expect(downloadAuditLogCsv({})).rejects.toThrow('HTTP 404')

    server.use(
      http.get('/fb/v1/console/audit-log/export.csv', () =>
        HttpResponse.text('not-json', { status: 500 }),
      ),
    )
    await expect(downloadAuditLogCsv({})).rejects.toThrow('HTTP 500')
  })
})
