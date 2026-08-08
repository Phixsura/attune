#!/usr/bin/env node

import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const diagnosticsDir = path.join(root, 'integrations', 'upgrade-diagnostics')
const manifestPath = path.join(diagnosticsDir, 'manifest.json')
const requiredChecks = [
  'install_health',
  'permission_boundary',
  'schema_drift',
  'webhook_readiness',
  'fixture_replay',
  'version_compatibility',
]
const requiredActions = [
  'resume_or_reauthorize_connection',
  'review_scopes_before_upgrade',
  'preview_mapping_before_backfill',
  'rotate_secret_and_replay_fixture',
  'refresh_replay_fixture',
  'run_compatibility_migration_plan',
]
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

const manifest = await readJSON(manifestPath)
validateManifest(manifest)

let fixtureCount = 0
for (const fixtureRef of manifest.fixtures) {
  const fixture = await readJSON(path.join(diagnosticsDir, fixtureRef.path))
  validateFixture(fixtureRef, fixture)
  fixtureCount += 1
}

console.log(
  `upgrade-diagnostics: clean (${manifest.diagnosticChecks.length} checks, ${manifest.compatibilityMatrix.length} compatibility rows, ${fixtureCount} fixtures, ${manifest.recoveryPlaybooks.length} recovery playbooks)`,
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
  assertEqual(value.schemaVersion, 'attune.upgrade_diagnostics.v1', 'schemaVersion')
  assert(Array.isArray(value.diagnosticChecks), 'diagnosticChecks invalid')
  assert(Array.isArray(value.compatibilityMatrix), 'compatibilityMatrix invalid')
  assert(Array.isArray(value.recoveryPlaybooks), 'recoveryPlaybooks invalid')
  assert(Array.isArray(value.fixtures), 'fixtures invalid')

  const checks = new Set(value.diagnosticChecks.map((check) => check.id))
  for (const id of requiredChecks) {
    assert(checks.has(id), `diagnosticChecks missing ${id}`)
  }
  const actions = new Set(value.recoveryPlaybooks.map((playbook) => playbook.action))
  for (const action of requiredActions) {
    assert(actions.has(action), `recoveryPlaybooks missing ${action}`)
  }
  const matrixConnectors = new Set(value.compatibilityMatrix.map((row) => row.connector))
  for (const connector of requiredConnectors) {
    assert(matrixConnectors.has(connector), `compatibilityMatrix missing ${connector}`)
  }
  for (const check of value.diagnosticChecks) validateCheck(check, actions)
  for (const row of value.compatibilityMatrix) validateCompatibility(row)
  for (const playbook of value.recoveryPlaybooks) validatePlaybook(playbook)
  assert(value.fixtures.length >= 3, 'fixtures must include at least 3 diagnostic cases')
}

function validateCheck(check, actions) {
  assertRecord(check, 'check')
  assertString(check.id, `${check.id}.id`)
  assertString(check.title, `${check.id}.title`)
  assertString(check.category, `${check.id}.category`)
  assertString(check.evidenceSource, `${check.id}.evidenceSource`)
  assertStringArray(check.failureSignals, `${check.id}.failureSignals`)
  assertString(check.recoveryAction, `${check.id}.recoveryAction`)
  assert(actions.has(check.recoveryAction), `${check.id}.recoveryAction has no playbook`)
  assertString(check.owner, `${check.id}.owner`)
}

function validateCompatibility(row) {
  assertRecord(row, 'compatibility')
  assertString(row.connector, `${row.connector}.connector`)
  assertString(row.currentVersion, `${row.connector}.currentVersion`)
  assertString(row.targetVersion, `${row.connector}.targetVersion`)
  assert(
    row.compatibility === 'non_breaking' || row.compatibility === 'requires_migration',
    `${row.connector}.compatibility invalid`,
  )
  assert(typeof row.rollback === 'boolean', `${row.connector}.rollback invalid`)
  assert(row.compatibility !== 'requires_migration' || row.rollback, `${row.connector} lacks rollback`)
}

function validatePlaybook(playbook) {
  assertRecord(playbook, 'playbook')
  assertString(playbook.action, `${playbook.action}.action`)
  assertStringArray(playbook.steps, `${playbook.action}.steps`)
  assert(playbook.steps.length >= 3, `${playbook.action}.steps too short`)
}

function validateFixture(fixtureRef, fixture) {
  assertString(fixtureRef.name, 'fixture.name')
  assertString(fixtureRef.path, 'fixture.path')
  assertString(fixtureRef.expectedStatus, `${fixtureRef.name}.expectedStatus`)
  assertRecord(fixture, fixtureRef.path)
  assertString(fixture.connector, `${fixtureRef.name}.connector`)
  assertRecord(fixture.input, `${fixtureRef.name}.input`)
  assertRecord(fixture.expected, `${fixtureRef.name}.expected`)
  assertEqual(fixture.expected.status, fixtureRef.expectedStatus, `${fixtureRef.name}.status`)
  assertString(fixture.expected.primaryAction, `${fixtureRef.name}.primaryAction`)
  assertString(fixture.expected.evidence, `${fixtureRef.name}.evidence`)
  assert(requiredActions.includes(fixture.expected.primaryAction), `${fixtureRef.name}.primaryAction invalid`)
  assert(typeof fixture.input.schemaDrift === 'boolean', `${fixtureRef.name}.schemaDrift invalid`)
  assert(typeof fixture.input.rollback === 'boolean', `${fixtureRef.name}.rollback invalid`)
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
