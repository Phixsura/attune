#!/usr/bin/env node
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'

const root = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..')
const base = 'integrations/import-export-workbench'
const failures = []

function read(file) {
  const abs = path.join(root, file)
  if (!fs.existsSync(abs)) {
    failures.push(`missing file ${file}`)
    return ''
  }
  return fs.readFileSync(abs, 'utf8')
}

function readJSON(file) {
  const source = read(file)
  if (!source) return null
  try {
    return JSON.parse(source)
  } catch (err) {
    failures.push(`invalid JSON ${file}: ${err.message}`)
    return null
  }
}

function requireValue(value, label) {
  if (!value) failures.push(label)
}

function fixturePath(file) {
  return `${base}/${file}`
}

function csvHeaders(file) {
  const source = read(file).trim()
  if (!source) return []
  return source.split(/\r?\n/)[0].split(',').map((header) => header.trim())
}

const manifest = readJSON(`${base}/manifest.json`)
const dryRun = manifest?.dryRun?.path ? readJSON(fixturePath(manifest.dryRun.path)) : null

function checkManifest() {
  requireValue(manifest?.id === 'attune.import_export.workbench.v1', 'manifest id mismatch')
  requireValue(manifest?.formats?.includes('csv'), 'manifest missing CSV format')
  requireValue(manifest?.formats?.includes('json'), 'manifest missing JSON format')
  requireValue(manifest?.templates?.length >= 4, 'manifest must define at least four templates')
  requireValue(
    manifest?.templates?.some((template) => template.direction === 'import'),
    'manifest missing import template',
  )
  requireValue(
    manifest?.templates?.some((template) => template.direction === 'export'),
    'manifest missing export template',
  )
  for (const template of manifest?.templates ?? []) {
    requireValue(template.id && template.direction && template.format && template.path, `template ${template.id ?? '<missing>'} is incomplete`)
    read(fixturePath(template.path))
  }
}

function checkSchemaAndMapping() {
  const required = manifest?.schema?.requiredFields ?? []
  const fields = manifest?.schema?.fields ?? []
  const mapping = manifest?.fieldMapping ?? {}
  requireValue(fields.length >= 8, 'schema preview must include at least eight fields')
  requireValue(required.length >= 4, 'schema preview must include at least four required fields')
  for (const field of required) {
    requireValue(
      fields.some((candidate) => candidate.name === field && candidate.required === true),
      `required field ${field} missing from schema preview`,
    )
    requireValue(Boolean(mapping[field]), `required field ${field} missing mapping`)
  }
}

function checkFixtures() {
  const required = manifest?.schema?.requiredFields ?? []
  const mapping = manifest?.fieldMapping ?? {}
  const csvImport = `${base}/fixtures/feedback-import.csv`
  const jsonImport = readJSON(`${base}/fixtures/customer-request-import.json`)
  const headers = csvHeaders(csvImport)
  for (const field of required) {
    requireValue(headers.includes(mapping[field]), `CSV fixture missing mapped header for ${field}`)
  }
  requireValue(Array.isArray(jsonImport) && jsonImport.length >= 3, 'JSON import fixture needs at least three sample rows')
  for (const row of jsonImport ?? []) {
    for (const field of required) {
      requireValue(row[field] !== undefined && row[field] !== '', `JSON fixture row missing ${field}`)
    }
  }
}

function checkDryRunAndRecovery() {
  const actions = new Set((dryRun?.rows ?? []).map((row) => row.action))
  for (const action of manifest?.dryRun?.minimumActions ?? []) {
    requireValue(actions.has(action), `dry-run fixture missing ${action} row action`)
  }
  const recoveryCodes = new Set((manifest?.recovery?.classes ?? []).map((item) => item.code))
  const recoveryActions = new Set((manifest?.recovery?.classes ?? []).map((item) => item.action))
  requireValue(recoveryCodes.size >= 4, 'recovery matrix must define at least four error classes')
  for (const row of dryRun?.rows ?? []) {
    if (row.action !== 'reject') continue
    requireValue(recoveryCodes.has(row.error_code), `dry-run reject row uses unknown error code ${row.error_code}`)
    requireValue(
      recoveryActions.has(row.recovery_action),
      `dry-run reject row uses unknown recovery action ${row.recovery_action}`,
    )
  }
}

function checkGovernance() {
  const scopes = manifest?.governance?.requiredScopes ?? []
  const auditEvents = manifest?.governance?.auditEvents ?? []
  requireValue(scopes.length >= 3, 'governance must define at least three required scopes')
  requireValue(auditEvents.includes('import.dry_run'), 'governance missing import.dry_run audit event')
  requireValue(auditEvents.includes('import.commit'), 'governance missing import.commit audit event')
  requireValue(auditEvents.includes('export.download'), 'governance missing export.download audit event')
  requireValue(manifest?.governance?.piiRedaction === true, 'governance must require PII redaction')
}

checkManifest()
checkSchemaAndMapping()
checkFixtures()
checkDryRunAndRecovery()
checkGovernance()

if (failures.length > 0) {
  for (const failure of failures) {
    console.error(`ERROR ${failure}`)
  }
  process.exit(1)
}

console.log(
  `import-export-workbench: clean (${manifest.formats.length} formats, ${manifest.templates.length} templates, ${manifest.schema.requiredFields.length} required mappings, ${dryRun.rows.length} dry-run rows, ${manifest.recovery.classes.length} recovery classes)`,
)
