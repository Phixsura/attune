#!/usr/bin/env node

import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import {
  classifyProviderError,
  mapFields,
  normalizeGitHubIssueWebhook,
  requiredConnectorHooks,
  verifyWebhookSignature,
} from '../integrations/connector-conformance/sdk/connector-sdk.mjs'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const conformanceDir = path.join(root, 'integrations', 'connector-conformance')
const manifestPath = path.join(conformanceDir, 'manifest.json')

const manifest = await readJSON(manifestPath)
validateManifest(manifest)

let fixtureCount = 0
for (const provider of manifest.providers) {
  validateProvider(provider)
  for (const fixtureRef of provider.fixtures) {
    const fixture = await readJSON(path.join(conformanceDir, fixtureRef.path))
    validateFixture(provider, fixtureRef, fixture)
    fixtureCount += 1
  }
  validateRecovery(provider)
}

console.log(
  `connector-conformance: clean (${manifest.providers.length} providers, ${fixtureCount} fixtures, 5 gates)`,
)

async function readJSON(filename) {
  try {
    return JSON.parse(await readFile(filename, 'utf8'))
  } catch (error) {
    throw new Error(`${path.relative(root, filename)} must be valid JSON: ${error.message}`)
  }
}

function validateManifest(value) {
  assertRecord(value, 'manifest')
  assertEqual(value.schemaVersion, 'attune.connector.conformance.v1', 'schemaVersion')
  assertStringArray(value.requiredHooks, 'requiredHooks')
  for (const hook of requiredConnectorHooks) {
    assert(value.requiredHooks.includes(hook), `manifest.requiredHooks missing ${hook}`)
  }
  assert(Array.isArray(value.providers) && value.providers.length > 0, 'manifest.providers empty')
}

function validateProvider(provider) {
  assertRecord(provider, 'provider')
  assertString(provider.id, 'provider.id')
  assertString(provider.displayName, 'provider.displayName')
  assertString(provider.contractVersion, 'provider.contractVersion')
  assertRecord(provider.install, `${provider.id}.install`)
  assertString(provider.install.docs, `${provider.id}.install.docs`)
  assertStringArray(provider.install.credentialTypes, `${provider.id}.install.credentialTypes`)
  assertStringArray(provider.install.scopes, `${provider.id}.install.scopes`)
  assertStringArray(provider.install.webhookEvents, `${provider.id}.install.webhookEvents`)
  assertRecord(provider.webhookSignature, `${provider.id}.webhookSignature`)
  assertEqual(provider.webhookSignature.algorithm, 'sha256', `${provider.id}.webhookSignature`)
  assertString(provider.webhookSignature.header, `${provider.id}.webhookSignature.header`)
  assertString(provider.webhookSignature.secretFixture, `${provider.id}.webhookSignature.secret`)
  assertRecord(provider.fieldMapping, `${provider.id}.fieldMapping`)
  assertStringArray(provider.fieldMapping.requiredLocalFields, `${provider.id}.requiredLocalFields`)
  assertStringArray(
    provider.fieldMapping.requiredExternalFields,
    `${provider.id}.requiredExternalFields`,
  )
  assertRecord(provider.fieldMapping.sampleMapping, `${provider.id}.sampleMapping`)
  assert(
    Array.isArray(provider.fixtures) && provider.fixtures.length >= 3,
    `${provider.id} must include at least 3 fixtures`,
  )
  assert(
    Array.isArray(provider.recovery) && provider.recovery.length >= 4,
    `${provider.id} must include recovery matrix entries`,
  )
}

function validateFixture(provider, fixtureRef, fixture) {
  assertString(fixtureRef.name, `${provider.id}.fixtures.name`)
  assertString(fixtureRef.path, `${provider.id}.fixtures.path`)
  assertEqual(fixtureRef.gate, 'fixture_replay', `${fixtureRef.name}.gate`)
  assertRecord(fixture, fixtureRef.path)
  assertString(fixture.provider, `${fixtureRef.name}.provider`)
  assertString(fixture.rawBody, `${fixtureRef.name}.rawBody`)
  assertRecord(fixture.headers, `${fixtureRef.name}.headers`)
  assertRecord(fixture.expected, `${fixtureRef.name}.expected`)
  assert(
    verifyWebhookSignature({
      algorithm: provider.webhookSignature.algorithm,
      header: provider.webhookSignature.header,
      headers: fixture.headers,
      rawBody: fixture.rawBody,
      secret: provider.webhookSignature.secretFixture,
    }),
    `${fixtureRef.name} signature did not match`,
  )

  const normalized = normalizeGitHubIssueWebhook(fixture)
  assertExpected(fixture.expected, normalized, `${fixtureRef.name}.normalized`)

  const mapped = mapFields(normalized, provider.fieldMapping.sampleMapping)
  for (const field of provider.fieldMapping.requiredLocalFields) {
    assert(mapped[field] !== undefined && mapped[field] !== '', `${fixtureRef.name} missing ${field}`)
  }

  const body = JSON.parse(fixture.rawBody)
  for (const field of provider.fieldMapping.requiredExternalFields) {
    assert(body.issue?.[field] !== undefined, `${fixtureRef.name} missing provider issue.${field}`)
  }
}

function validateRecovery(provider) {
  const seen = new Set()
  for (const scenario of provider.recovery) {
    assertString(scenario.kind, `${provider.id}.recovery.kind`)
    assertNumber(scenario.httpStatus, `${provider.id}.recovery.httpStatus`)
    assertString(scenario.expected, `${provider.id}.recovery.expected`)
    assertEqual(classifyProviderError(scenario), scenario.expected, `${scenario.kind}.recovery`)
    seen.add(scenario.expected)
  }
  for (const expected of ['retry_after', 'reauthorize', 'dead_letter', 'retry']) {
    assert(seen.has(expected), `${provider.id}.recovery missing ${expected}`)
  }
}

function assertExpected(expected, normalized, label) {
  for (const [key, value] of Object.entries(expected)) {
    const actual = normalized[key]
    if (Array.isArray(value)) {
      assert(
        Array.isArray(actual) && value.join('\u0000') === actual.join('\u0000'),
        `${label}.${key} mismatch`,
      )
      continue
    }
    assertEqual(actual, value, `${label}.${key}`)
  }
}

function assertRecord(value, label) {
  assert(typeof value === 'object' && value !== null && !Array.isArray(value), `${label} not object`)
}

function assertString(value, label) {
  assert(typeof value === 'string' && value.length > 0, `${label} not string`)
}

function assertNumber(value, label) {
  assert(typeof value === 'number' && Number.isFinite(value), `${label} not number`)
}

function assertStringArray(value, label) {
  assert(Array.isArray(value) && value.every((item) => typeof item === 'string'), `${label} invalid`)
}

function assertEqual(actual, expected, label) {
  assert(actual === expected, `${label}: expected ${expected}, got ${actual}`)
}

function assert(condition, message) {
  if (!condition) throw new Error(message)
}
