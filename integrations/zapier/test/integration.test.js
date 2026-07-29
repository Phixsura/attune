'use strict'

// Integration tests via the zapier-platform-core test harness + nock mocks.
// Set ATTUNE_LIVE_BASE_URL (+ ATTUNE_LIVE_API_KEY) to run the same calls
// against a real local attune instead of mocks.

const assert = require('node:assert/strict')
const { test } = require('node:test')

const zapier = require('zapier-platform-core')
const nock = require('nock')

const App = require('../index')
const samples = require('../samples')

const appTester = zapier.createAppTester(App)
zapier.tools.env.inject()

const LIVE = process.env.ATTUNE_LIVE_BASE_URL || ''
const BASE = LIVE || 'https://attune.example.test'
const authData = {
  base_url: BASE,
  api_key: process.env.ATTUNE_LIVE_API_KEY || 'fbk_live_test',
}
const mocked = LIVE === ''

test('auth test hits /v1/auth/verify and labels the connection', async () => {
  if (mocked) {
    nock(BASE).get('/v1/auth/verify').reply(200, {
      valid: true,
      label: 'zapier-key',
      tenant_display_name: 'Acme Inc',
      scopes: ['hooks:manage'],
    })
  }
  const result = await appTester(App.authentication.test, { authData })
  assert.equal(result.valid, true)
})

test('subscribe stores the hook and returns its id', async () => {
  if (mocked) {
    nock(BASE)
      .post('/v1/hooks', (body) => {
        assert.equal(body.target_url, 'https://hooks.zapier.com/h/1')
        assert.deepEqual(body.event_types, ['feedback.created'])
        assert.equal(body.consumer, 'zapier')
        return true
      })
      .reply(201, { id: 'sub-1', status: 'active' })
  }
  const result = await appTester(App.triggers.new_feedback.operation.performSubscribe, {
    authData,
    targetUrl: 'https://hooks.zapier.com/h/1',
  })
  assert.ok(result.id, 'subscribe must return an id for bundle.subscribeData')
  if (mocked) assert.equal(result.id, 'sub-1')

  if (!mocked) {
    // Live mode: clean up the hook we just created.
    await appTester(App.triggers.new_feedback.operation.performUnsubscribe, {
      authData,
      subscribeData: { id: result.id },
    })
  }
})

test('unsubscribe deletes the stored hook', { skip: !mocked }, async () => {
  nock(BASE).delete('/v1/hooks/sub-1').reply(204)
  await appTester(App.triggers.new_feedback.operation.performUnsubscribe, {
    authData,
    subscribeData: { id: 'sub-1' },
  })
})

test('perform reshapes the live envelope with a dedup id', async () => {
  const results = await appTester(App.triggers.urgent_feedback.operation.perform, {
    authData,
    cleanedRequest: samples.feedbackUrgent,
  })
  assert.equal(results.length, 1)
  assert.equal(results[0].id, '12345-feedback.urgent')
  assert.equal(results[0].event_type, 'feedback.urgent')
  assert.equal(results[0].feedback.enriched.is_urgent, true)
})

test('performList output is schema-identical to perform output (T004)', async () => {
  if (mocked) {
    nock(BASE)
      .get('/v1/hooks/samples/request.status_changed')
      .reply(200, { samples: [JSON.parse(JSON.stringify(samples.requestStatusChanged))] })
  }
  const [listed] = await appTester(App.triggers.request_status_changed.operation.performList, {
    authData,
  })
  const [performed] = await appTester(App.triggers.request_status_changed.operation.perform, {
    authData,
    cleanedRequest: samples.requestStatusChanged,
  })
  assert.ok(listed, 'performList must return at least one item')
  const listedKeys = Object.keys(listed).sort()
  const performedKeys = Object.keys(performed).sort()
  for (const k of listedKeys) {
    assert.ok(performedKeys.includes(k), `performList key ${k} missing from live shape`)
  }
})

test('every trigger static sample matches its event type and carries id', () => {
  for (const [key, trigger] of Object.entries(App.triggers)) {
    const sample = trigger.operation.sample
    assert.ok(sample.id, `${key} sample needs a dedup id`)
    assert.equal(sample.version, '2')
    assert.ok(sample.event_type, `${key} sample needs event_type`)
    assert.ok(sample.feedback || sample.request, `${key} sample needs an entity object`)
  }
})

test('create_feedback posts to /v1/feedback/ingest', { skip: !mocked }, async () => {
  nock(BASE)
    .post('/v1/feedback/ingest', (body) => {
      assert.equal(body.content, 'From a Zap')
      assert.equal(body.source, 'api')
      return true
    })
    .reply(200, { id: 777, status: 'accepted' })
  const result = await appTester(App.creates.create_feedback.operation.perform, {
    authData,
    inputData: { content: 'From a Zap' },
  })
  assert.equal(result.id, 777)
})

test('update_request PATCHes status', { skip: !mocked }, async () => {
  nock(BASE)
    .patch('/v1/requests/req-1', (body) => {
      assert.equal(body.status, 'CUSTOMER_REQUEST_STATUS_SHIPPED')
      return true
    })
    .reply(200, { request: { id: 'req-1', status: 'CUSTOMER_REQUEST_STATUS_SHIPPED' } })
  const result = await appTester(App.creates.update_request.operation.perform, {
    authData,
    inputData: { request_id: 'req-1', status: 'CUSTOMER_REQUEST_STATUS_SHIPPED' },
  })
  assert.equal(result.request.id, 'req-1')
})

test('add_tag posts the tag name', { skip: !mocked }, async () => {
  nock(BASE)
    .post('/v1/feedback/42/tags', (body) => {
      assert.equal(body.tag_name, 'bug')
      return true
    })
    .reply(200, { tag: { id: 't1', name: 'bug' } })
  const result = await appTester(App.creates.add_tag.operation.perform, {
    authData,
    inputData: { feedback_id: 42, tag_name: 'bug' },
  })
  assert.equal(result.tag.name, 'bug')
})

test('add_note defaults to internal visibility', { skip: !mocked }, async () => {
  nock(BASE)
    .post('/v1/requests/req-1/notes', (body) => {
      assert.equal(body.visibility, 'internal')
      assert.equal(body.body, 'synced from Zendesk')
      return true
    })
    .reply(201, { request: { id: 'req-1' } })
  const result = await appTester(App.creates.add_note.operation.perform, {
    authData,
    inputData: { request_id: 'req-1', body: 'synced from Zendesk' },
  })
  assert.equal(result.request.id, 'req-1')
})

test('401 from the API propagates as an auth error', { skip: !mocked }, async () => {
  nock(BASE).get('/v1/auth/verify').reply(401, {
    code: 'UNAUTHORIZED',
    message: 'invalid api key',
  })
  await assert.rejects(appTester(App.authentication.test, { authData }))
})
