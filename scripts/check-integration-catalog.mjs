#!/usr/bin/env node

import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const catalogDir = path.join(root, 'integrations', 'integration-catalog')
const manifestPath = path.join(catalogDir, 'manifest.json')
const requiredConnectors = [
  'jira',
  'github',
  'intercom',
  'zendesk',
  'salesforce',
  'hubspot',
  'custom-webhook',
  'csv',
]
const allowedInstallStatuses = new Set(['available', 'installed', 'beta', 'requires_setup'])
const allowedHealthBadges = new Set(['healthy', 'ready', 'watch', 'degraded'])
const allowedCompatibility = new Set(['non_breaking', 'requires_migration'])
const requiredAuditEvents = ['integration.install', 'integration.health_check', 'integration.replay']

const manifest = await readJSON(manifestPath)
validateManifest(manifest)

let replayFixtureCount = 0
let permissionMapCount = 0
let installStateCount = 0
let upgradePathCount = 0

for (const connector of manifest.connectors) {
  validateConnector(connector)
  const fixture = await readJSON(path.join(catalogDir, connector.sampleReplay.path))
  validateReplayFixture(connector, fixture)
  replayFixtureCount += 1
  permissionMapCount += 1
  installStateCount += 1
  upgradePathCount += 1
}

console.log(
  `integration-catalog: clean (${manifest.connectors.length} connectors, ${installStateCount} install states, ${permissionMapCount} permission maps, ${replayFixtureCount} replay fixtures, ${upgradePathCount} upgrade paths)`,
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
  assertEqual(value.schemaVersion, 'attune.integration_catalog.v1', 'schemaVersion')
  assert(Array.isArray(value.connectors), 'manifest.connectors invalid')
  const ids = new Set(value.connectors.map((connector) => connector.id))
  for (const id of requiredConnectors) {
    assert(ids.has(id), `manifest.connectors missing ${id}`)
  }
  assertEqual(ids.size, value.connectors.length, 'connector ids must be unique')
}

function validateConnector(connector) {
  assertRecord(connector, 'connector')
  assertString(connector.id, `${connector.id}.id`)
  assertString(connector.displayName, `${connector.id}.displayName`)
  assertString(connector.category, `${connector.id}.category`)
  assertString(connector.description, `${connector.id}.description`)
  assertString(connector.owner, `${connector.id}.owner`)
  assertString(connector.supportTier, `${connector.id}.supportTier`)

  assertRecord(connector.install, `${connector.id}.install`)
  assert(allowedInstallStatuses.has(connector.install.status), `${connector.id}.install.status invalid`)
  assertString(connector.install.docs, `${connector.id}.install.docs`)
  assertStringArray(connector.install.authTypes, `${connector.id}.install.authTypes`)
  assertStringArray(connector.install.setupChecks, `${connector.id}.install.setupChecks`)
  assert(connector.install.setupChecks.length >= 3, `${connector.id}.install.setupChecks too short`)

  assertRecord(connector.permissions, `${connector.id}.permissions`)
  assertStringArray(connector.permissions.scopes, `${connector.id}.permissions.scopes`)
  assert(connector.permissions.scopes.length > 0, `${connector.id}.permissions.scopes empty`)
  assertEqual(connector.permissions.leastPrivilege, true, `${connector.id}.permissions.leastPrivilege`)
  assertStringArray(connector.permissions.dataClasses, `${connector.id}.permissions.dataClasses`)

  assertRecord(connector.health, `${connector.id}.health`)
  assert(allowedHealthBadges.has(connector.health.badge), `${connector.id}.health.badge invalid`)
  assertStringArray(connector.health.signals, `${connector.id}.health.signals`)
  assert(connector.health.signals.length >= 3, `${connector.id}.health.signals too short`)
  assertString(connector.health.slo, `${connector.id}.health.slo`)

  assertRecord(connector.sampleReplay, `${connector.id}.sampleReplay`)
  assertString(connector.sampleReplay.path, `${connector.id}.sampleReplay.path`)
  assertString(connector.sampleReplay.event, `${connector.id}.sampleReplay.event`)
  assertString(connector.sampleReplay.normalizedType, `${connector.id}.sampleReplay.normalizedType`)
  assertString(connector.sampleReplay.expectedAction, `${connector.id}.sampleReplay.expectedAction`)

  assertRecord(connector.upgrade, `${connector.id}.upgrade`)
  assertString(connector.upgrade.currentVersion, `${connector.id}.upgrade.currentVersion`)
  assertString(connector.upgrade.path, `${connector.id}.upgrade.path`)
  assert(allowedCompatibility.has(connector.upgrade.compatibility), `${connector.id}.upgrade invalid`)
  assert(typeof connector.upgrade.rollback === 'boolean', `${connector.id}.upgrade.rollback invalid`)
  assert(
    connector.upgrade.compatibility !== 'requires_migration' || connector.upgrade.rollback,
    `${connector.id}.upgrade requires rollback for migrations`,
  )

  assertStringArray(connector.auditEvents, `${connector.id}.auditEvents`)
  for (const event of requiredAuditEvents) {
    assert(connector.auditEvents.includes(event), `${connector.id}.auditEvents missing ${event}`)
  }
}

function validateReplayFixture(connector, fixture) {
  assertRecord(fixture, `${connector.id}.fixture`)
  assertEqual(fixture.provider, connector.id, `${connector.id}.fixture.provider`)
  assertEqual(fixture.eventType, connector.sampleReplay.event, `${connector.id}.fixture.eventType`)
  assertRecord(fixture.rawPayload, `${connector.id}.fixture.rawPayload`)
  assertRecord(fixture.normalized, `${connector.id}.fixture.normalized`)
  assertEqual(
    fixture.normalized.type,
    connector.sampleReplay.normalizedType,
    `${connector.id}.fixture.normalized.type`,
  )
  assertEqual(
    fixture.normalized.action,
    connector.sampleReplay.expectedAction,
    `${connector.id}.fixture.normalized.action`,
  )
  assertString(fixture.normalized.externalId, `${connector.id}.fixture.normalized.externalId`)
  assertString(fixture.normalized.title, `${connector.id}.fixture.normalized.title`)
  assertString(fixture.normalized.source, `${connector.id}.fixture.normalized.source`)
}

function assertRecord(value, label) {
  assert(typeof value === 'object' && value !== null && !Array.isArray(value), `${label} not object`)
}

function assertString(value, label) {
  assert(typeof value === 'string' && value.length > 0, `${label} not string`)
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
