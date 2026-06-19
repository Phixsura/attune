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

  it('escapes ids with special chars in the path', async () => {
    const { fetch, calls } = stubFetch([() => json(200, {})])
    await newClient(fetch).archiveTag('a/b')
    expect(calls[0]?.url).toBe(`${BASE}/v1/tags/a%2Fb`)
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
  it('does NOT retry a non-idempotent POST (createTag)', async () => {
    const { fetch, calls } = stubFetch([() => json(503, { code: 'INTERNAL', message: 'down' })])
    await expect(newClient(fetch).createTag({ name: 'x' })).rejects.toBeInstanceOf(AttuneError)
    expect(calls.length).toBe(1) // no retry
  })

  it('DOES retry an idempotent DELETE (archiveTag)', async () => {
    let n = 0
    const { fetch, calls } = stubFetch([
      () => (++n < 3 ? json(503, { code: 'INTERNAL', message: 'down' }) : json(200, {})),
    ])
    await newClient(fetch).archiveTag('t1')
    expect(calls.length).toBe(3) // retried twice
  })

  it('does NOT retry a non-idempotent POST (createWorkflowState)', async () => {
    const { fetch, calls } = stubFetch([() => json(503, { code: 'INTERNAL', message: 'down' })])
    await expect(
      newClient(fetch).createWorkflowState({
        name: 'triage',
        color: '#3b82f6',
        category: 'active',
        position: 1,
      }),
    ).rejects.toBeInstanceOf(AttuneError)
    expect(calls.length).toBe(1) // no retry — could otherwise duplicate the state
  })

  it('does NOT retry seedWorkflowDefaults (non-idempotent POST)', async () => {
    const { fetch, calls } = stubFetch([() => json(503, { code: 'INTERNAL', message: 'down' })])
    await expect(newClient(fetch).seedWorkflowDefaults()).rejects.toBeInstanceOf(AttuneError)
    expect(calls.length).toBe(1)
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

  it('updateWorkflowState rejects empty id', async () => {
    const { fetch } = stubFetch([() => json(200, {})])
    await expect(newClient(fetch).updateWorkflowState({ id: '' })).rejects.toBeInstanceOf(
      AttuneError,
    )
  })
})
