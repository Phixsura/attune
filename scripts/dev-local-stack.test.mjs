import assert from 'node:assert/strict'
import test from 'node:test'

import { DEFAULTS, parseArgs, renderConfig, usage } from './dev-local-stack.mjs'

test('parseArgs returns stable local defaults', () => {
  const options = parseArgs([])

  assert.equal(options.adminEmail, DEFAULTS.adminEmail)
  assert.equal(options.adminPassword, DEFAULTS.adminPassword)
  assert.equal(options.buildConsole, true)
  assert.equal(options.bootstrapDemo, true)
  assert.equal(options.dbPort, DEFAULTS.preferredDbPort)
  assert.equal(options.serverPort, undefined)
  assert.equal(options.tenantSlug, DEFAULTS.tenantSlug)
})

test('parseArgs accepts explicit ports and disabled setup steps', () => {
  const options = parseArgs([
    '--server-port',
    '18090',
    '--db-port',
    '15433',
    '--tenant',
    'acme',
    '--tenant-name',
    'Acme',
    '--admin-email',
    'ops@example.com',
    '--admin-password',
    'local-password',
    '--no-console-build',
    '--no-demo-bootstrap',
    '--keep-workdir',
  ])

  assert.equal(options.serverPort, 18090)
  assert.equal(options.dbPort, 15433)
  assert.equal(options.tenantSlug, 'acme')
  assert.equal(options.tenantName, 'Acme')
  assert.equal(options.adminEmail, 'ops@example.com')
  assert.equal(options.adminPassword, 'local-password')
  assert.equal(options.buildConsole, false)
  assert.equal(options.bootstrapDemo, false)
  assert.equal(options.keepWorkdir, true)
})

test('parseArgs rejects invalid options', () => {
  assert.throws(() => parseArgs(['--server-port', '0']), /integer port/)
  assert.throws(() => parseArgs(['--db-port', 'not-a-port']), /integer port/)
  assert.throws(() => parseArgs(['--tenant']), /requires a value/)
  assert.throws(() => parseArgs(['--wat']), /unknown option/)
})

test('renderConfig emits current config shape', () => {
  const config = renderConfig({
    baseURL: 'http://127.0.0.1:18090',
    consoleAdmin: { email: 'ops@example.com', password: 'local-password' },
    consoleSessionKey: 'a'.repeat(64),
    dsn: 'postgres://attune@127.0.0.1:15432/attune?sslmode=disable',
    keyset: '{"primaryKeyId":1,"key":[]}',
    serverPort: 18090,
  })

  assert.match(config, /profile: dev/)
  assert.match(config, /port: 18090/)
  assert.match(config, /url: "postgres:\/\/attune@127\.0\.0\.1:15432\/attune\?sslmode=disable"/)
  assert.match(config, /base_url: "http:\/\/127\.0\.0\.1:18090"/)
  assert.match(config, /session_key: "aaaaaaaa/)
  assert.match(config, /email: "ops@example\.com"/)
  assert.match(config, /password: "local-password"/)
  assert.match(config, /tink_keyset: \|/)
})

test('usage documents the failed-fetch-safe entrypoint', () => {
  const text = usage()

  assert.match(text, /Starts a disposable local Attune stack/)
  assert.match(text, /--server-port/)
  assert.match(text, /--no-console-build/)
})
