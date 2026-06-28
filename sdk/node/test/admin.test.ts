import { describe, expect, it } from 'vitest'
import { AttuneError, Client, type FetchLike } from '../src/index'

const BASE = 'https://attune.example.test'
const KEY = 'ak_test_key'

function stubFetch(responses: Array<() => Response | Promise<Response>>): {
  fetch: FetchLike
  calls: Array<{ url: string; init: RequestInit }>
} {
  const calls: Array<{ url: string; init: RequestInit }> = []
  let i = 0
  const fetch: FetchLike = (url, init) => {
    calls.push({ url, init })
    const next = responses[Math.min(i, responses.length - 1)]
    i += 1
    if (!next) throw new Error('no stubbed response')
    return Promise.resolve(next())
  }
  return { fetch, calls }
}

const json = (status: number, body: unknown, headers?: Record<string, string>): Response =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', ...headers },
  })

const binary = (
  status: number,
  body: string | Uint8Array,
  headers?: Record<string, string>,
): Response =>
  new Response(body as BodyInit, {
    status,
    headers,
  })

const noSleep = () => Promise.resolve()
const newClient = (fetch: FetchLike, overrides = {}): Client =>
  new Client({ baseURL: BASE, apiKey: KEY, fetch, sleep: noSleep, ...overrides })

describe('tags', () => {
  it('listTags issues GET /v1/tags and parses the response', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { tags: [{ id: 't1', name: 'bug' }] })])
    const res = await newClient(fetch).listTags()
    expect(calls[0]?.init.method).toBe('GET')
    expect(calls[0]?.url).toBe(`${BASE}/v1/tags`)
    expect(calls[0]?.init.body).toBeUndefined()
    expect(res.tags[0]?.name).toBe('bug')
  })

  it('listTags includeArchived adds the query param', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { tags: [] })])
    await newClient(fetch).listTags({ includeArchived: true })
    expect(calls[0]?.url).toBe(`${BASE}/v1/tags?include_archived=true`)
  })

  it('rejects invalid tag list options before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { tags: [] })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listTags('bad')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'tag list options must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listTags({ includeArchived: 'false' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'includeArchived must be a boolean',
    })
    expect(calls).toHaveLength(0)
  })

  it('createTag POSTs /v1/tags with the body', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: 't9', name: 'bug' })])
    const tag = await newClient(fetch).createTag({ name: 'bug', color: '#ef4444' })
    expect(calls[0]?.init.method).toBe('POST')
    expect(calls[0]?.url).toBe(`${BASE}/v1/tags`)
    expect(JSON.parse(calls[0]?.init.body as string)).toMatchObject({
      name: 'bug',
      color: '#ef4444',
    })
    expect(tag.id).toBe('t9')
  })

  it('rejects a non-object tag request before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: 't9', name: 'bug' })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.createTag(null)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'tag request must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.updateTag(undefined)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'tag request must be an object',
    })
    expect(calls).toHaveLength(0)
  })

  it('rejects malformed per-request options before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: 't9', name: 'bug' })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.createTag({ name: 'bug' }, 'bad')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'request options must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.archiveTag('t9', { signal: 'bad' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'signal must be an AbortSignal',
    })
    expect(calls).toHaveLength(0)
  })

  it('updateTag rejects empty id and PATCHes /v1/tags/{id}', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { id: 't9', name: 'x' })])
    const c = newClient(fetch)
    await expect(c.updateTag({ id: '' })).rejects.toBeInstanceOf(AttuneError)
    await c.updateTag({ id: 't9', name: 'x' })
    expect(calls[0]?.init.method).toBe('PATCH')
    expect(calls[0]?.url).toBe(`${BASE}/v1/tags/t9`)
  })

  it('archiveTag DELETEs /v1/tags/{id} and rejects empty id', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    const c = newClient(fetch)
    await expect(c.archiveTag('')).rejects.toBeInstanceOf(AttuneError)
    await c.archiveTag('t9')
    expect(calls[0]?.init.method).toBe('DELETE')
    expect(calls[0]?.url).toBe(`${BASE}/v1/tags/t9`)
  })

  it('escapes an allowed special char in the path', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    await newClient(fetch).archiveTag('a b') // space is allowed but must be escaped
    expect(calls[0]?.url).toBe(`${BASE}/v1/tags/a%20b`)
  })

  it('rejects dot-segment / slash ids before sending (no path walking)', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    const c = newClient(fetch)
    for (const bad of ['.', '..', 'a/b', '../admin']) {
      await expect(c.archiveTag(bad)).rejects.toBeInstanceOf(AttuneError)
      await expect(c.updateTag({ id: bad })).rejects.toBeInstanceOf(AttuneError)
      await expect(c.archiveWorkflowState(bad)).rejects.toBeInstanceOf(AttuneError)
      await expect(c.updateWorkflowState({ id: bad })).rejects.toBeInstanceOf(AttuneError)
    }
    expect(calls).toHaveLength(0) // never hit the network
  })

  it('rejects non-string path ids before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.archiveTag(123)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'tag id is invalid',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.archiveWorkflowState(123)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'state id is invalid',
    })
    expect(calls).toHaveLength(0)
  })

  it('surfaces a FORBIDDEN error envelope', async () => {
    const { fetch } = stubFetch([
      () =>
        json(403, { code: 'FORBIDDEN', message: 'missing scope: tags:write', requestId: 'rq7' }),
    ])
    await expect(newClient(fetch).createTag({ name: 'x' })).rejects.toMatchObject({
      code: 'FORBIDDEN',
      status: 403,
      requestId: 'rq7',
    })
  })
})

describe('retry safety (bug fix)', () => {
  it('retries createTag with an auto-generated idempotency key', async () => {
    const { fetch, calls } = stubFetch([() => json(503, { code: 'INTERNAL', message: 'down' })])
    await expect(newClient(fetch).createTag({ name: 'x' })).rejects.toBeInstanceOf(AttuneError)
    expect(calls.length).toBe(3)
    expect(calls[0]?.init.headers).toBeDefined()
  })

  it('DOES retry an idempotent DELETE (archiveTag)', async () => {
    let n = 0
    const { fetch, calls } = stubFetch([
      () => (++n < 3 ? json(503, { code: 'INTERNAL', message: 'down' }) : json(200, {})),
    ])
    await newClient(fetch).archiveTag('t1')
    expect(calls.length).toBe(3) // retried twice
  })

  it('retries createWorkflowState with an auto-generated idempotency key', async () => {
    const { fetch, calls } = stubFetch([() => json(503, { code: 'INTERNAL', message: 'down' })])
    await expect(
      newClient(fetch).createWorkflowState({
        name: 'triage',
        color: '#3b82f6',
        category: 'active',
        position: 1,
      }),
    ).rejects.toBeInstanceOf(AttuneError)
    expect(calls.length).toBe(3)
  })

  it('retries seedWorkflowDefaults with an auto-generated idempotency key', async () => {
    const { fetch, calls } = stubFetch([() => json(503, { code: 'INTERNAL', message: 'down' })])
    await expect(newClient(fetch).seedWorkflowDefaults()).rejects.toBeInstanceOf(AttuneError)
    expect(calls.length).toBe(3)
  })

  it('DOES retry replaceWorkflowTransitions (idempotent PUT)', async () => {
    let n = 0
    const { fetch, calls } = stubFetch([
      () => (++n < 3 ? json(503, { code: 'INTERNAL', message: 'down' }) : json(200, {})),
    ])
    await newClient(fetch).replaceWorkflowTransitions({ transitions: [] })
    expect(calls.length).toBe(3) // retried twice
  })
})

describe('workflow', () => {
  const cases: [string, (c: Client) => Promise<unknown>, string, string][] = [
    ['listWorkflowStates', (c) => c.listWorkflowStates(), 'GET', '/v1/workflow/states'],
    ['seedWorkflowDefaults', (c) => c.seedWorkflowDefaults(), 'POST', '/v1/workflow/seed'],
    [
      'listWorkflowTransitions',
      (c) => c.listWorkflowTransitions(),
      'GET',
      '/v1/workflow/transitions',
    ],
    [
      'createWorkflowState',
      (c) =>
        c.createWorkflowState({
          name: 'triage',
          color: '#3b82f6',
          category: 'active',
          position: 1,
        }),
      'POST',
      '/v1/workflow/states',
    ],
    [
      'replaceWorkflowTransitions',
      (c) => c.replaceWorkflowTransitions({ transitions: [] }),
      'PUT',
      '/v1/workflow/transitions',
    ],
    [
      'archiveWorkflowState',
      (c) => c.archiveWorkflowState('s1'),
      'DELETE',
      '/v1/workflow/states/s1',
    ],
  ]
  for (const [name, call, method, path] of cases) {
    it(`${name} → ${method} ${path}`, async () => {
      const { fetch, calls } = stubFetch([() => json(200, {})])
      await call(newClient(fetch))
      const c0 = calls[0]
      if (!c0) throw new Error('no request recorded')
      expect(c0.init.method).toBe(method)
      expect(c0.url).toBe(`${BASE}${path}`)
    })
  }

  it('listWorkflowStates includeArchived adds the query param', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { states: [] })])
    await newClient(fetch).listWorkflowStates({ includeArchived: true })
    expect(calls[0]?.url).toBe(`${BASE}/v1/workflow/states?include_archived=true`)
  })

  it('rejects invalid workflow list options before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { states: [] })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listWorkflowStates('bad')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'workflow state list options must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listWorkflowStates({ includeArchived: 'false' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'includeArchived must be a boolean',
    })
    expect(calls).toHaveLength(0)
  })

  it('updateWorkflowState rejects empty id', async () => {
    const { fetch } = stubFetch([() => json(200, {})])
    await expect(newClient(fetch).updateWorkflowState({ id: '' })).rejects.toBeInstanceOf(
      AttuneError,
    )
  })

  it('rejects a non-object workflow request before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.createWorkflowState(null)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'state request must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.updateWorkflowState(undefined)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'state request must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.replaceWorkflowTransitions(null)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'workflow transition request must be an object',
    })
    expect(calls).toHaveLength(0)
  })

  it('rejects malformed workflow options before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listWorkflowTransitions('bad')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'request options must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.seedWorkflowDefaults({ signal: 'bad' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'signal must be an AbortSignal',
    })
    expect(calls).toHaveLength(0)
  })
})

describe('audit log', () => {
  it('listAuditLog builds the expected query string', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { items: [] })])
    await newClient(fetch).listAuditLog({
      actions: ['tag.create', 'tag.archive'],
      actorType: 'api_key',
      actorId: 'apikey:123',
      targetType: 'tag',
      targetId: 't1',
      from: '2026-06-01T00:00:00Z',
      to: '2026-06-30T00:00:00Z',
      limit: 25,
      cursor: 'next-page',
    })
    expect(calls[0]?.init.method).toBe('GET')
    expect(calls[0]?.url).toBe(
      `${BASE}/v1/audit-log?actions=tag.create&actions=tag.archive&actor_type=api_key&actor_id=apikey%3A123&target_type=tag&target_id=t1&from=2026-06-01T00%3A00%3A00Z&to=2026-06-30T00%3A00%3A00Z&limit=25&cursor=next-page`,
    )
  })

  it('exportAuditLogCSV returns binary bytes and filename', async () => {
    const { fetch, calls } = stubFetch([
      () =>
        binary(200, 'id,action\n1,tag.create\n', {
          'content-type': 'text/csv; charset=utf-8',
          'content-disposition': 'attachment; filename="audit-log.csv"',
        }),
    ])
    const resp = await newClient(fetch).exportAuditLogCSV({ action: 'tag.create' })
    expect(calls[0]?.url).toBe(`${BASE}/v1/audit-log/export.csv?action=tag.create`)
    expect(resp.filename).toBe('audit-log.csv')
    expect(new TextDecoder().decode(resp.data)).toContain('tag.create')
  })

  it('createAuditEvidenceExport retries with an idempotency key', async () => {
    let n = 0
    const { fetch, calls } = stubFetch([
      () =>
        ++n < 3
          ? json(503, { code: 'INTERNAL', message: 'down' })
          : json(200, { jobId: 'job-1', status: 'queued', retryAfterSeconds: 2 }),
    ])
    const resp = await newClient(fetch).createAuditEvidenceExport({
      from: '2026-06-01T00:00:00Z',
      to: '2026-06-30T00:00:00Z',
      actions: [],
      actorType: '',
      actorId: '',
      targetType: '',
      targetId: '',
    })
    expect(resp.jobId).toBe('job-1')
    expect(calls.length).toBe(3)
  })

  it('rejects a non-object audit log query before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { items: [] })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listAuditLog(null)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'audit log query must be an object',
    })
    expect(calls).toHaveLength(0)
  })

  it('rejects non-array audit actions before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { items: [] })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listAuditLog({ actions: 'tag.create' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'audit log actions must be an array of strings',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listAuditLog({ actions: ['tag.create', 1] })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'audit log actions must be an array of strings',
    })
    expect(calls).toHaveLength(0)
  })

  it('rejects malformed audit log scalar query values before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { items: [] })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listAuditLog({ action: 7 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'audit log action must be a string',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listAuditLog({ from: { bad: true } })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'audit log from must be a string',
    })
    await expect(c.listAuditLog({ limit: 1.5 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'audit log limit must be a positive integer',
    })
    await expect(c.listAuditLog({ limit: 0 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'audit log limit must be a positive integer',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listAuditLog({ cursor: 123 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'audit log cursor must be a string',
    })
    expect(calls).toHaveLength(0)
  })
})

describe('gdpr', () => {
  const cases: [string, (c: Client) => Promise<unknown>, string, string][] = [
    [
      'exportGdprSubject',
      (c) => c.exportGdprSubject({ subjectKey: 'user:1' }),
      'POST',
      '/v1/gdpr/export',
    ],
    ['getGdprExport', (c) => c.getGdprExport('job-1'), 'GET', '/v1/gdpr/exports/job-1'],
    [
      'revokeGdprExport',
      (c) => c.revokeGdprExport('job-1'),
      'POST',
      '/v1/gdpr/exports/job-1/revoke',
    ],
    [
      'deleteGdprSubject',
      (c) => c.deleteGdprSubject({ subjectKey: 'user:1' }),
      'POST',
      '/v1/gdpr/delete',
    ],
    [
      'cancelGdprRequest',
      (c) => c.cancelGdprRequest('req-1'),
      'POST',
      '/v1/gdpr/requests/req-1/cancel',
    ],
    ['getGdprOperations', (c) => c.getGdprOperations(), 'GET', '/v1/gdpr/operations'],
  ]

  for (const [name, call, method, path] of cases) {
    it(`${name} → ${method} ${path}`, async () => {
      const { fetch, calls } = stubFetch([() => json(200, {})])
      await call(newClient(fetch))
      expect(calls[0]?.init.method).toBe(method)
      expect(calls[0]?.url).toBe(`${BASE}${path}`)
    })
  }

  it('listGdprRequests adds cursor/limit/type query params', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { items: [] })])
    await newClient(fetch).listGdprRequests({ cursor: 'c1', limit: 10, requestType: 'export' })
    expect(calls[0]?.url).toBe(`${BASE}/v1/gdpr/requests?cursor=c1&limit=10&request_type=export`)
  })

  it('retries exportGdprSubject with an auto-generated idempotency key', async () => {
    const { fetch, calls } = stubFetch([() => json(503, { code: 'INTERNAL', message: 'down' })])
    await expect(
      newClient(fetch).exportGdprSubject({ subjectKey: 'user:1' }),
    ).rejects.toBeInstanceOf(AttuneError)
    expect(calls.length).toBe(3)
  })

  it('downloadGdprExport returns binary bytes and filename', async () => {
    const { fetch, calls } = stubFetch([
      () =>
        binary(200, 'zipdata', {
          'content-type': 'application/zip',
          'content-disposition': 'attachment; filename="gdpr-export.zip"',
        }),
    ])
    const resp = await newClient(fetch).downloadGdprExport('job-1')
    expect(calls[0]?.url).toBe(`${BASE}/v1/gdpr/exports/job-1/download`)
    expect(resp.contentType).toBe('application/zip')
    expect(resp.filename).toBe('gdpr-export.zip')
    expect(new TextDecoder().decode(resp.data)).toBe('zipdata')
  })

  it('iterateGdprRequests walks every page', async () => {
    const { fetch } = stubFetch([
      () => json(200, { items: [{ requestId: 'req-1' }], nextCursor: 'c2' }),
      () => json(200, { items: [{ requestId: 'req-2' }] }),
    ])
    const items: string[] = []
    for await (const item of newClient(fetch).iterateGdprRequests()) {
      items.push(item.requestId)
    }
    expect(items).toEqual(['req-1', 'req-2'])
  })

  it('rejects non-object gdpr request/query payloads before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { items: [] })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.exportGdprSubject(null)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'gdpr export request must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.deleteGdprSubject(undefined)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'gdpr delete request must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listGdprRequests(null)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'gdpr request query must be an object',
    })
    expect(calls).toHaveLength(0)
  })

  it('rejects malformed gdpr options before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { items: [] })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.getGdprOperations('bad')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'request options must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.getGdprExport('job-1', { signal: 'bad' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'signal must be an AbortSignal',
    })
    expect(calls).toHaveLength(0)
  })

  it('rejects malformed gdpr ids and query scalars before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { items: [] })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.getGdprExport(123)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'gdpr export job id is invalid',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.cancelGdprRequest({ bad: true })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'gdpr request id is invalid',
    })
    await expect(c.listGdprRequests({ limit: 1.5 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'gdpr request limit must be a positive integer',
    })
    await expect(c.listGdprRequests({ limit: -1 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'gdpr request limit must be a positive integer',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listGdprRequests({ requestType: 7 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'gdpr request type must be "export" or "delete"',
    })
    await expect(c.listGdprRequests({ requestType: 'typo' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'gdpr request type must be "export" or "delete"',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listGdprRequests({ cursor: { bad: true } })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'gdpr request cursor must be a string',
    })
    expect(calls).toHaveLength(0)
  })
})

describe('outbox', () => {
  it('listOutboxDeliveries builds the expected query string', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { deliveries: [], nextBeforeId: '0' })])
    await newClient(fetch).listOutboxDeliveries({
      status: [' dead ', 'failed', 'dead'],
      limit: 20,
      beforeId: '99',
    })
    expect(calls[0]?.init.method).toBe('GET')
    expect(calls[0]?.url).toBe(
      `${BASE}/v1/outbox/deliveries?status=dead&status=failed&limit=20&before_id=99`,
    )
  })

  it('retryOutboxDelivery POSTs the retry path and rejects invalid ids', async () => {
    const { fetch, calls } = stubFetch([() => json(202, { delivery: { id: '1' } })])
    const c = newClient(fetch)
    await expect(c.retryOutboxDelivery('')).rejects.toBeInstanceOf(AttuneError)
    await c.retryOutboxDelivery('1')
    expect(calls[0]?.init.method).toBe('POST')
    expect(calls[0]?.url).toBe(`${BASE}/v1/outbox/1/retry`)
  })

  it('rejects invalid outbox list options before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { deliveries: [], nextBeforeId: '0' })])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listOutboxDeliveries('bad')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'outbox delivery list options must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listOutboxDeliveries({ status: 'dead' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'outbox statuses must be an array of strings',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listOutboxDeliveries({ status: ['dead', 1] })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'outbox statuses must be an array of strings',
    })
    await expect(c.listOutboxDeliveries({ status: ['bogus'] })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'outbox status must be one of "pending", "delivered", "failed", or "dead"',
    })
    await expect(c.listOutboxDeliveries({ status: [''] })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'outbox status must be one of "pending", "delivered", "failed", or "dead"',
    })
    await expect(c.listOutboxDeliveries({ limit: 1.5 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'outbox delivery limit must be a positive integer',
    })
    await expect(c.listOutboxDeliveries({ limit: 0 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'outbox delivery limit must be a positive integer',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listOutboxDeliveries({ beforeId: 99 })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'beforeId must be a non-negative int64 string',
    })
    await expect(c.listOutboxDeliveries({ beforeId: 'not-an-int' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'beforeId must be a non-negative int64 string',
    })
    await expect(c.listOutboxDeliveries({ beforeId: '-99' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'beforeId must be a non-negative int64 string',
    })
    await expect(
      c.listOutboxDeliveries({ beforeId: '92233720368547758070' }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'beforeId must be a non-negative int64 string',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.retryOutboxDelivery(123)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'delivery id is invalid',
    })
    await expect(c.retryOutboxDelivery('0')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'delivery id is invalid',
    })
    await expect(c.retryOutboxDelivery('abc')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'delivery id is invalid',
    })
    await expect(c.retryOutboxDelivery('92233720368547758070')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'delivery id is invalid',
    })
    expect(calls).toHaveLength(0)
  })
})

describe('mcp client governance', () => {
  const clientId = '11111111-1111-1111-1111-111111111111'
  const grantId = '22222222-2222-2222-2222-222222222222'
  const sessionId = '33333333-3333-3333-3333-333333333333'

  const cases: [string, (c: Client) => Promise<unknown>, string, string][] = [
    ['listMCPClients', (c) => c.listMCPClients(), 'GET', '/v1/mcp/clients'],
    [
      'createMCPClient',
      (c) =>
        c.createMCPClient({
          name: 'agent',
          redirectUris: ['https://example.com/callback'],
          scopes: ['mcp:read'],
        }),
      'POST',
      '/v1/mcp/clients',
    ],
    ['getMCPClient', (c) => c.getMCPClient(clientId), 'GET', `/v1/mcp/clients/${clientId}`],
    [
      'revokeMCPClient',
      (c) => c.revokeMCPClient(clientId),
      'DELETE',
      `/v1/mcp/clients/${clientId}`,
    ],
    [
      'updateMCPClient',
      (c) => c.updateMCPClient({ id: clientId, toolPolicyMode: 'legacy_allow_all' }),
      'PATCH',
      `/v1/mcp/clients/${clientId}`,
    ],
    [
      'replaceMCPClientToolPolicies',
      (c) => c.replaceMCPClientToolPolicies({ id: clientId, policies: [] }),
      'PUT',
      `/v1/mcp/clients/${clientId}/tool-policies`,
    ],
    [
      'revokeMCPRefreshGrant',
      (c) => c.revokeMCPRefreshGrant(clientId, grantId),
      'DELETE',
      `/v1/mcp/clients/${clientId}/grants/${grantId}`,
    ],
    [
      'revokeMCPSession',
      (c) => c.revokeMCPSession(clientId, sessionId),
      'DELETE',
      `/v1/mcp/clients/${clientId}/sessions/${sessionId}`,
    ],
  ]

  for (const [name, call, method, path] of cases) {
    it(`${name} → ${method} ${path}`, async () => {
      const { fetch, calls } = stubFetch([() => json(200, {})])
      await call(newClient(fetch))
      expect(calls[0]?.init.method).toBe(method)
      expect(calls[0]?.url).toBe(`${BASE}${path}`)
    })
  }

  it('rejects invalid client identifiers before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    const c = newClient(fetch)
    await expect(c.getMCPClient('')).rejects.toBeInstanceOf(AttuneError)
    await expect(c.getMCPClient('client-1')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp client id is invalid',
    })
    await expect(c.updateMCPClient({ id: '' })).rejects.toBeInstanceOf(AttuneError)
    await expect(c.updateMCPClient({ id: 'client-1' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp client id is invalid',
    })
    await expect(c.replaceMCPClientToolPolicies({ id: '', policies: [] })).rejects.toBeInstanceOf(
      AttuneError,
    )
    await expect(
      c.replaceMCPClientToolPolicies({ id: 'client-1', policies: [] }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp client id is invalid',
    })
    await expect(c.revokeMCPRefreshGrant(clientId, '')).rejects.toBeInstanceOf(AttuneError)
    await expect(c.revokeMCPRefreshGrant(clientId, 'grant-1')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp refresh grant id is invalid',
    })
    await expect(c.revokeMCPSession(clientId, '')).rejects.toBeInstanceOf(AttuneError)
    await expect(c.revokeMCPSession(clientId, 'session-1')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp session id is invalid',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.getMCPClient(123)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp client id is invalid',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.revokeMCPRefreshGrant(clientId, 123)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp refresh grant id is invalid',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.revokeMCPSession(123, sessionId)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp client id is invalid',
    })
    expect(calls).toHaveLength(0)
  })

  it('accepts 204 No Content for revoke-style MCP routes', async () => {
    const { fetch } = stubFetch([
      () => new Response(null, { status: 204 }),
      () => new Response(null, { status: 204 }),
      () => new Response(null, { status: 204 }),
    ])
    const c = newClient(fetch)
    await expect(c.revokeMCPClient(clientId)).resolves.toEqual({})
    await expect(c.revokeMCPRefreshGrant(clientId, grantId)).resolves.toEqual({})
    await expect(c.revokeMCPSession(clientId, sessionId)).resolves.toEqual({})
  })

  it('rejects non-object MCP request payloads before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.createMCPClient(null)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp client request must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.updateMCPClient(undefined)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp client request must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.replaceMCPClientToolPolicies(null)).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp tool policy request must be an object',
    })
    expect(calls).toHaveLength(0)
  })

  it('rejects malformed MCP governance payloads before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    const c = newClient(fetch)

    await expect(
      c.createMCPClient({
        name: '   ',
        redirectUris: ['https://example.com/callback'],
        scopes: ['mcp:read'],
      }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp client name is required',
    })
    await expect(
      c.createMCPClient({
        name: 'x'.repeat(129),
        redirectUris: ['https://example.com/callback'],
        scopes: ['mcp:read'],
      }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp client name must be at most 128 characters',
    })
    await expect(
      c.createMCPClient({ name: 'agent', redirectUris: [], scopes: ['mcp:read'] }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'redirectUris must contain at least one URI',
    })
    await expect(
      c.createMCPClient({
        name: 'agent',
        redirectUris: ['https://example.com/callback#frag'],
        scopes: ['mcp:read'],
      }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'fragment not allowed in redirect URI',
    })
    await expect(
      c.createMCPClient({
        name: 'agent',
        redirectUris: ['http://example.com/callback'],
        scopes: ['mcp:read'],
      }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'redirect URI must use HTTPS (except for localhost)',
    })
    await expect(
      c.createMCPClient({
        name: 'agent',
        redirectUris: ['https://example.com/callback'],
        scopes: ['mcp:admin'],
      }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp scope must be one of "mcp:read", "mcp:write", or "mcp:ingest"',
    })
    await expect(c.updateMCPClient({ id: clientId })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message:
        'mcp update request must include at least one of toolPolicyMode, rateLimitRpm, or rateLimitBurst',
    })
    await expect(
      c.updateMCPClient({ id: clientId, toolPolicyMode: 'allow_all' }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp toolPolicyMode must be "legacy_allow_all" or "allow_list"',
    })
    await expect(
      c.updateMCPClient({ id: clientId, toolPolicyMode: 'allow_list', rateLimitRpm: 0 }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp rateLimitRpm must be a positive integer',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.replaceMCPClientToolPolicies({ id: clientId })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp policies must be provided; use [] to clear all policies',
    })
    await expect(
      c.replaceMCPClientToolPolicies({
        id: clientId,
        policies: [{ toolName: '   ', effect: 'allow' }],
      }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp tool policy toolName is required',
    })
    await expect(
      c.replaceMCPClientToolPolicies({
        id: clientId,
        policies: [
          { toolName: 'list_tags', effect: 'allow' },
          { toolName: 'list_tags', effect: 'deny' },
        ],
      }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'duplicate mcp tool policy toolName: list_tags',
    })
    await expect(
      c.replaceMCPClientToolPolicies({
        id: clientId,
        policies: [{ toolName: 'list_tags', effect: 'block' }],
      }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp tool policy effect must be "allow" or "deny"',
    })
    await expect(
      c.replaceMCPClientToolPolicies({
        id: clientId,
        policies: [{ toolName: 'list_tags', effect: 'allow', rateLimitBurst: 0 }],
      }),
    ).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'mcp tool policy rateLimitBurst must be a positive integer',
    })
    expect(calls).toHaveLength(0)
  })

  it('normalizes MCP governance payloads before sending', async () => {
    const { fetch, calls } = stubFetch([
      () => json(200, {}),
      () => json(200, {}),
      () => json(200, {}),
      () => json(200, {}),
    ])
    const c = newClient(fetch)

    await c.createMCPClient({
      name: ' ops-agent ',
      redirectUris: ['https://example.com/callback'],
      scopes: ['mcp:read'],
    })
    await c.updateMCPClient({
      id: clientId,
      toolPolicyMode: ' legacy_allow_all ',
      rateLimitRpm: 5,
      rateLimitBurst: 7,
    })
    await c.updateMCPClient({
      id: clientId,
      rateLimitRpm: 9,
    })
    await c.replaceMCPClientToolPolicies({
      id: clientId,
      policies: [
        { toolName: ' list_tags ', effect: ' allow ', rateLimitRpm: 3, rateLimitBurst: 4 },
      ],
    })

    expect(JSON.parse(calls[0]?.init.body as string)).toMatchObject({
      name: 'ops-agent',
      redirectUris: ['https://example.com/callback'],
      scopes: ['mcp:read'],
    })
    expect(JSON.parse(calls[1]?.init.body as string)).toMatchObject({
      id: clientId,
      toolPolicyMode: 'legacy_allow_all',
      rateLimitRpm: 5,
      rateLimitBurst: 7,
    })
    expect(JSON.parse(calls[2]?.init.body as string)).toMatchObject({
      id: clientId,
      rateLimitRpm: 9,
    })
    expect(JSON.parse(calls[2]?.init.body as string)).not.toHaveProperty('toolPolicyMode')
    expect(JSON.parse(calls[2]?.init.body as string)).not.toHaveProperty('rateLimitBurst')
    expect(JSON.parse(calls[3]?.init.body as string)).toMatchObject({
      id: clientId,
      policies: [{ toolName: 'list_tags', effect: 'allow', rateLimitRpm: 3, rateLimitBurst: 4 }],
    })
  })

  it('rejects malformed MCP options before sending', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    const c = newClient(fetch)
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listMCPClients('bad')).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'request options must be an object',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.revokeMCPClient(clientId, { signal: 'bad' })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'signal must be an AbortSignal',
    })
    // @ts-expect-error deliberate JS-style misuse
    await expect(c.listMCPClients({ signal: { aborted: false } })).rejects.toMatchObject({
      name: 'AttuneError',
      code: 'BAD_REQUEST',
      message: 'signal must be an AbortSignal',
    })
    expect(calls).toHaveLength(0)
  })
})
