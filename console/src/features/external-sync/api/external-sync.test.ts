import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import {
  deleteExternalConnection,
  ExternalSyncDirection,
  externalSyncConnectionSchemaQuery,
  externalSyncEventsQuery,
  externalSyncProvidersQuery,
  externalSyncRunsQuery,
  retryExternalSyncFailure,
  updateExternalMapping,
} from '@/features/external-sync/api/external-sync'
import { server } from '@/testing/mocks/server'

describe('external sync api helpers', () => {
  it('sends mapping updates and remaining command requests', async () => {
    const calls: Array<{ method: string; url: string; body?: unknown }> = []
    server.use(
      http.put('/fb/v1/console/external-sync/mappings/mapping-1', async ({ request }) => {
        calls.push({
          method: request.method,
          url: new URL(request.url).pathname,
          body: await request.json(),
        })
        return HttpResponse.json({
          id: 'mapping-1',
          tenantId: 'tenant-1',
          connectionId: 'conn-1',
          localObjectType: 'customer_request',
          externalObjectType: 'issue',
          direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
          fieldMappingJson: '{"title":"title"}',
          statusMappingJson: '{}',
          conflictPolicy: 'manual',
          tombstonePolicy: 'mark_stale',
          enabled: true,
          mappingVersion: 2,
          createdAt: '2026-07-08T01:00:00Z',
          updatedAt: '2026-07-08T02:00:00Z',
        })
      }),
      http.delete('/fb/v1/console/external-sync/connections/conn-1', ({ request }) => {
        calls.push({ method: request.method, url: new URL(request.url).pathname })
        return new HttpResponse(null, { status: 204 })
      }),
      http.post('/fb/v1/console/external-sync/failures/failure-1:retry', ({ request }) => {
        calls.push({ method: request.method, url: new URL(request.url).pathname })
        return HttpResponse.json({
          id: 'failure-1',
          tenantId: 'tenant-1',
          runId: 'run-1',
          mappingId: 'mapping-1',
          operation: 'pull',
          localObjectId: 'cr-1',
          externalKey: 'ISSUE-1',
          failureKind: 'validation',
          message: 'bad payload',
          payloadDigest: 'sha256:test',
          retryMode: 'refetch',
          normalizedPayloadJson: '{}',
          retryable: true,
          resolvedAt: '2026-07-08T02:00:00Z',
          resolvedBy: 'admin',
          createdAt: '2026-07-08T01:00:00Z',
        })
      }),
    )

    await updateExternalMapping({
      id: 'mapping-1',
      direction: ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL,
      fieldMappingJson: '{"title":"title"}',
      statusMappingJson: '{}',
      conflictPolicy: 'manual',
      tombstonePolicy: 'mark_stale',
      enabled: true,
    })
    await deleteExternalConnection('conn-1')
    await retryExternalSyncFailure('failure-1')

    expect(calls).toEqual([
      {
        method: 'PUT',
        url: '/fb/v1/console/external-sync/mappings/mapping-1',
        body: {
          direction: 'EXTERNAL_SYNC_DIRECTION_PULL',
          fieldMappingJson: '{"title":"title"}',
          statusMappingJson: '{}',
          conflictPolicy: 'manual',
          tombstonePolicy: 'mark_stale',
          enabled: true,
        },
      },
      { method: 'DELETE', url: '/fb/v1/console/external-sync/connections/conn-1' },
      { method: 'POST', url: '/fb/v1/console/external-sync/failures/failure-1:retry' },
    ])
  })

  it('includes filters and cursors in external sync list queries', async () => {
    const seenQueries: string[] = []
    server.use(
      http.get('/fb/v1/console/external-sync/runs', ({ request }) => {
        seenQueries.push(new URL(request.url).search)
        return HttpResponse.json({ runs: [], nextBeforeId: '' })
      }),
      http.get('/fb/v1/console/external-sync/events', ({ request }) => {
        seenQueries.push(new URL(request.url).search)
        return HttpResponse.json({ events: [], nextBeforeId: '' })
      }),
    )

    const runsQuery = externalSyncRunsQuery(11, {
      connectionId: 'conn-1',
      mappingId: 'mapping-1',
      status: 'failed',
    })
    const eventsQuery = externalSyncEventsQuery(7, {
      connectionId: 'conn-1',
      status: 'dead',
    })
    const unfilteredRunsQuery = externalSyncRunsQuery(5)
    const unfilteredEventsQuery = externalSyncEventsQuery(3)
    const runsQueryFn = runsQuery.queryFn
    const eventsQueryFn = eventsQuery.queryFn
    const unfilteredRunsQueryFn = unfilteredRunsQuery.queryFn
    const unfilteredEventsQueryFn = unfilteredEventsQuery.queryFn
    if (typeof runsQueryFn !== 'function') throw new Error('missing runs queryFn')
    if (typeof eventsQueryFn !== 'function') throw new Error('missing events queryFn')
    if (typeof unfilteredRunsQueryFn !== 'function') throw new Error('missing runs queryFn')
    if (typeof unfilteredEventsQueryFn !== 'function') throw new Error('missing events queryFn')

    await runsQueryFn({
      pageParam: 'run-before',
      signal: undefined,
    } as never)
    await eventsQueryFn({
      pageParam: 'event-before',
      signal: undefined,
    } as never)
    await unfilteredRunsQueryFn({
      pageParam: '',
      signal: undefined,
    } as never)
    await unfilteredEventsQueryFn({
      pageParam: '',
      signal: undefined,
    } as never)

    expect(seenQueries).toEqual([
      '?limit=11&connection_id=conn-1&mapping_id=mapping-1&status=failed&before_id=run-before',
      '?limit=7&connection_id=conn-1&status=dead&before_id=event-before',
      '?limit=5',
      '?limit=3',
    ])
  })

  it('loads the registered external sync providers', async () => {
    const seen: string[] = []
    server.use(
      http.get('/fb/v1/console/external-sync/providers', ({ request }) => {
        seen.push(new URL(request.url).pathname)
        return HttpResponse.json({
          providers: [
            { provider: 'github', display: 'GitHub' },
            { provider: 'jira', display: 'Jira' },
          ],
        })
      }),
    )

    const providersQuery = externalSyncProvidersQuery()
    const providersQueryFn = providersQuery.queryFn
    if (typeof providersQueryFn !== 'function') throw new Error('missing providers queryFn')

    await expect(providersQueryFn({ signal: undefined } as never)).resolves.toEqual([
      { provider: 'github', display: 'GitHub' },
      { provider: 'jira', display: 'Jira' },
    ])
    expect(seen).toEqual(['/fb/v1/console/external-sync/providers'])
  })

  it('short-circuits schema discovery when no connection is selected', async () => {
    const schemaQuery = externalSyncConnectionSchemaQuery()
    const schemaQueryFn = schemaQuery.queryFn
    if (typeof schemaQueryFn !== 'function') throw new Error('missing schema queryFn')

    await expect(schemaQueryFn({ signal: undefined } as never)).resolves.toEqual([])
  })
})
