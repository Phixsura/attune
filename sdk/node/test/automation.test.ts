import { describe, expect, it } from 'vitest'
import { AttuneError, Client, type FetchLike } from '../src/index'
import {
  CustomerRequestPriority,
  CustomerRequestStatus,
} from '../src/proto/attune/v1/customer_request'

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

const json = (status: number, body: unknown): Response =>
  new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })

const noSleep = () => Promise.resolve()
const newClient = (fetch: FetchLike): Client =>
  new Client({ baseURL: BASE, apiKey: KEY, fetch, sleep: noSleep })

describe('webhook subscriptions', () => {
  it('createWebhookSubscription issues POST /v1/hooks', async () => {
    const { fetch, calls } = stubFetch([
      () =>
        json(201, {
          id: 's1',
          target_url: 'https://hooks.zapier.com/x',
          event_types: ['feedback.created'],
          status: 'active',
          consumer: 'zapier',
        }),
    ])
    const res = await newClient(fetch).createWebhookSubscription({
      targetUrl: 'https://hooks.zapier.com/x',
      eventTypes: ['feedback.created'],
      secret: '',
      consumer: 'zapier',
    })
    expect(calls[0]?.url).toBe(`${BASE}/v1/hooks`)
    expect(calls[0]?.init.method).toBe('POST')
    expect(res.id).toBe('s1')
    expect(res.status).toBe('active')
  })

  it('listWebhookSubscriptions issues GET /v1/hooks', async () => {
    const { fetch, calls } = stubFetch([
      () => json(200, { subscriptions: [{ id: 's1', status: 'active' }] }),
    ])
    const res = await newClient(fetch).listWebhookSubscriptions()
    expect(calls[0]?.url).toBe(`${BASE}/v1/hooks`)
    expect(res.subscriptions).toHaveLength(1)
  })

  it('deleteWebhookSubscription issues DELETE /v1/hooks/{id}', async () => {
    const { fetch, calls } = stubFetch([() => new Response(null, { status: 204 })])
    await newClient(fetch).deleteWebhookSubscription('s1')
    expect(calls[0]?.url).toBe(`${BASE}/v1/hooks/s1`)
    expect(calls[0]?.init.method).toBe('DELETE')
  })

  it('deleteWebhookSubscription rejects an invalid id', async () => {
    const { fetch } = stubFetch([() => json(204, {})])
    await expect(newClient(fetch).deleteWebhookSubscription('')).rejects.toBeInstanceOf(AttuneError)
  })

  it('listWebhookSamples issues GET /v1/hooks/samples/{event_type}', async () => {
    const { fetch, calls } = stubFetch([
      () => json(200, { samples: [{ version: '2', event_type: 'feedback.created' }] }),
    ])
    const res = await newClient(fetch).listWebhookSamples('feedback.created')
    expect(calls[0]?.url).toBe(`${BASE}/v1/hooks/samples/feedback.created`)
    expect(res.samples).toHaveLength(1)
  })
})

describe('request automation', () => {
  it('listRequests builds the query string', async () => {
    const { fetch, calls } = stubFetch([() => json(200, { requests: [] })])
    await newClient(fetch).listRequests({ q: 'dark', limit: 5 })
    expect(calls[0]?.url).toBe(`${BASE}/v1/requests?q=dark&limit=5`)
  })

  it('createRequest issues POST /v1/requests', async () => {
    const { fetch, calls } = stubFetch([
      () => json(201, { request: { id: 'r1', title: 'T', status: 1 } }),
    ])
    const res = await newClient(fetch).createRequest({
      title: 'T',
      idempotencyKey: 'zap-recipe-1',
      status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_UNSPECIFIED,
      priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_UNSPECIFIED,
    })
    expect(calls[0]?.url).toBe(`${BASE}/v1/requests`)
    expect(calls[0]?.init.method).toBe('POST')
    expect(res.request?.id).toBe('r1')
  })

  it('updateRequest issues PATCH /v1/requests/{id}', async () => {
    const { fetch, calls } = stubFetch([
      () => json(200, { request: { id: 'r1', title: 'T', status: 3 } }),
    ])
    await newClient(fetch).updateRequest({ id: 'r1' })
    expect(calls[0]?.url).toBe(`${BASE}/v1/requests/r1`)
    expect(calls[0]?.init.method).toBe('PATCH')
  })

  it('addRequestNote issues POST /v1/requests/{id}/notes', async () => {
    const { fetch, calls } = stubFetch([
      () => json(201, { request: { id: 'r1', title: 'T', status: 1 } }),
    ])
    await newClient(fetch).addRequestNote({ id: 'r1', body: 'note', visibility: 'internal' })
    expect(calls[0]?.url).toBe(`${BASE}/v1/requests/r1/notes`)
    const body = JSON.parse(String(calls[0]?.init.body))
    expect(body.visibility).toBe('internal')
  })

  it('updateRequest rejects a missing id', async () => {
    const { fetch } = stubFetch([() => json(200, {})])
    await expect(newClient(fetch).updateRequest({ id: '' })).rejects.toBeInstanceOf(AttuneError)
  })
})
