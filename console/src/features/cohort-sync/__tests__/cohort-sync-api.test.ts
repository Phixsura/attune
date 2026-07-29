import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { server } from '@/testing/mocks/server'
import {
  cohortSyncHealthQuery,
  cohortSyncKeys,
  createCohortSource,
  deleteCohortSource,
  getCohort,
  getCohortSource,
  listCohortMembersQuery,
  listCohortSourcesQuery,
  listCohortSyncEventsQuery,
  listCohortSyncRunsQuery,
  listCohortsQuery,
  syncCohort,
  testCohortSource,
  updateCohort,
  updateCohortSource,
} from '../api/cohort-sync'

describe('cohort-sync API client', () => {
  it('listCohortSourcesQuery returns sources', async () => {
    server.use(
      http.get('/fb/v1/console/cohort-sync/sources', () =>
        HttpResponse.json({ sources: [{ id: 's1', name: 'Test' }] }),
      ),
    )
    const opts = listCohortSourcesQuery()
    expect(opts.queryKey).toEqual(cohortSyncKeys.sources())
    expect(opts.staleTime).toBe(20_000)
    const result = await opts.queryFn!({ signal: new AbortController().signal } as never)
    expect(result).toHaveLength(1)
    expect(result[0].name).toBe('Test')
  })

  it('getCohortSource returns a source', async () => {
    server.use(
      http.get('/fb/v1/console/cohort-sync/sources/s1', () =>
        HttpResponse.json({ id: 's1', name: 'Source' }),
      ),
    )
    const result = await getCohortSource('s1')
    expect(result.id).toBe('s1')
  })

  it('createCohortSource posts a new source', async () => {
    server.use(
      http.post('/fb/v1/console/cohort-sync/sources', () =>
        HttpResponse.json({ id: 's2', name: 'New' }),
      ),
    )
    const result = await createCohortSource({
      provider: 'amplitude',
      name: 'New',
      authType: 'api_key',
      credential: 'key',
      enabled: true,
    })
    expect(result.id).toBe('s2')
  })

  it('updateCohortSource patches a source', async () => {
    server.use(
      http.patch('/fb/v1/console/cohort-sync/sources/s1', () =>
        HttpResponse.json({ id: 's1', name: 'Updated' }),
      ),
    )
    const result = await updateCohortSource('s1', { name: 'Updated' })
    expect(result.name).toBe('Updated')
  })

  it('deleteCohortSource deletes a source', async () => {
    server.use(
      http.delete(
        '/fb/v1/console/cohort-sync/sources/s1',
        () => new HttpResponse(null, { status: 204 }),
      ),
    )
    await expect(deleteCohortSource('s1')).resolves.toBeUndefined()
  })

  it('testCohortSource returns test result', async () => {
    server.use(
      http.post('/fb/v1/console/cohort-sync/sources/s1:test', () =>
        HttpResponse.json({ ok: true, error: '' }),
      ),
    )
    const result = await testCohortSource('s1')
    expect(result.ok).toBe(true)
  })

  it('listCohortsQuery returns cohorts', async () => {
    server.use(
      http.get('/fb/v1/console/cohort-sync/cohorts', () =>
        HttpResponse.json({ cohorts: [{ id: 'c1', name: 'Cohort' }] }),
      ),
    )
    const opts = listCohortsQuery()
    const result = await opts.queryFn!({ signal: new AbortController().signal } as never)
    expect(result).toHaveLength(1)
  })

  it('getCohort returns a cohort', async () => {
    server.use(
      http.get('/fb/v1/console/cohort-sync/cohorts/c1', () =>
        HttpResponse.json({ id: 'c1', name: 'Cohort' }),
      ),
    )
    const result = await getCohort('c1')
    expect(result.id).toBe('c1')
  })

  it('updateCohort patches a cohort', async () => {
    server.use(
      http.patch('/fb/v1/console/cohort-sync/cohorts/c1', () =>
        HttpResponse.json({ id: 'c1', name: 'Renamed' }),
      ),
    )
    const result = await updateCohort('c1', { name: 'Renamed' })
    expect(result.name).toBe('Renamed')
  })

  it('syncCohort triggers sync', async () => {
    server.use(
      http.post('/fb/v1/console/cohort-sync/cohorts/c1:sync', () =>
        HttpResponse.json({ run: { id: 'r1', status: 'succeeded' } }),
      ),
    )
    const result = await syncCohort('c1')
    expect(result.run?.status).toBe('succeeded')
  })

  it('listCohortMembersQuery returns members', async () => {
    server.use(
      http.get('/fb/v1/console/cohort-sync/cohorts/c1/members', () =>
        HttpResponse.json({ members: [{ id: 'm1' }] }),
      ),
    )
    const opts = listCohortMembersQuery('c1')
    const result = await opts.queryFn!({ signal: new AbortController().signal } as never)
    expect(result).toHaveLength(1)
  })

  it('listCohortSyncEventsQuery returns events', async () => {
    server.use(
      http.get('/fb/v1/console/cohort-sync/sources/s1/events', () =>
        HttpResponse.json({ events: [{ id: 'e1' }] }),
      ),
    )
    const opts = listCohortSyncEventsQuery('s1')
    const result = await opts.queryFn!({ signal: new AbortController().signal } as never)
    expect(result).toHaveLength(1)
  })

  it('listCohortSyncRunsQuery returns runs', async () => {
    server.use(
      http.get('/fb/v1/console/cohort-sync/cohorts/c1/runs', () =>
        HttpResponse.json({ runs: [{ id: 'r1' }] }),
      ),
    )
    const opts = listCohortSyncRunsQuery('c1')
    const result = await opts.queryFn!({ signal: new AbortController().signal } as never)
    expect(result).toHaveLength(1)
  })

  it('cohortSyncHealthQuery returns health', async () => {
    server.use(
      http.get('/fb/v1/console/cohort-sync/health', () =>
        HttpResponse.json({ sourceCount: 2, activeSources: 1 }),
      ),
    )
    const opts = cohortSyncHealthQuery()
    const result = await opts.queryFn!({ signal: new AbortController().signal } as never)
    expect(result.sourceCount).toBe(2)
  })

  it('query key factory produces correct keys', () => {
    expect(cohortSyncKeys.all).toEqual(['cohort-sync'])
    expect(cohortSyncKeys.sources()).toEqual(['cohort-sync', 'sources'])
    expect(cohortSyncKeys.cohorts('src-1')).toEqual(['cohort-sync', 'cohorts', 'src-1'])
    expect(cohortSyncKeys.members('c1')).toEqual(['cohort-sync', 'members', 'c1'])
    expect(cohortSyncKeys.runs('c1')).toEqual(['cohort-sync', 'runs', 'c1'])
    expect(cohortSyncKeys.events('s1')).toEqual(['cohort-sync', 'events', 's1'])
    expect(cohortSyncKeys.health()).toEqual(['cohort-sync', 'health'])
  })
})
