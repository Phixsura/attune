#!/usr/bin/env node
import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'

const root = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..')
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
  if (!source) return {}
  try {
    return JSON.parse(source)
  } catch (err) {
    failures.push(`invalid JSON ${file}: ${err.message}`)
    return {}
  }
}

function requireContains(file, needle, label) {
  const source = read(file)
  if (source && !source.includes(needle)) {
    failures.push(`${file}: missing ${label} (${needle})`)
  }
}

function requireValue(value, label) {
  if (!value) failures.push(label)
}

const sharedClientSurface = [
  ['ingest', 'Ingest'],
  ['listTags', 'ListTags'],
  ['createTag', 'CreateTag'],
  ['updateTag', 'UpdateTag'],
  ['archiveTag', 'ArchiveTag'],
  ['listWorkflowStates', 'ListWorkflowStates'],
  ['createWorkflowState', 'CreateWorkflowState'],
  ['updateWorkflowState', 'UpdateWorkflowState'],
  ['archiveWorkflowState', 'ArchiveWorkflowState'],
  ['listWorkflowTransitions', 'ListWorkflowTransitions'],
  ['replaceWorkflowTransitions', 'ReplaceWorkflowTransitions'],
  ['seedWorkflowDefaults', 'SeedWorkflowDefaults'],
  ['listAuditLog', 'ListAuditLog'],
  ['exportAuditLogCSV', 'ExportAuditLogCSV'],
  ['createAuditEvidenceExport', 'CreateAuditEvidenceExport'],
  ['getAuditEvidenceExport', 'GetAuditEvidenceExport'],
  ['downloadAuditEvidenceExport', 'DownloadAuditEvidenceExport'],
  ['exportGdprSubject', 'ExportGdprSubject'],
  ['getGdprExport', 'GetGdprExport'],
  ['downloadGdprExport', 'DownloadGdprExport'],
  ['revokeGdprExport', 'RevokeGdprExport'],
  ['deleteGdprSubject', 'DeleteGdprSubject'],
  ['cancelGdprRequest', 'CancelGdprRequest'],
  ['listGdprRequests', 'ListGdprRequests'],
  ['getGdprOperations', 'GetGdprOperations'],
  ['listOutboxDeliveries', 'ListOutboxDeliveries'],
  ['retryOutboxDelivery', 'RetryOutboxDelivery'],
  ['listMCPClients', 'ListMCPClients'],
  ['createMCPClient', 'CreateMCPClient'],
  ['getMCPClient', 'GetMCPClient'],
  ['revokeMCPClient', 'RevokeMCPClient'],
  ['updateMCPClient', 'UpdateMCPClient'],
  ['replaceMCPClientToolPolicies', 'ReplaceMCPClientToolPolicies'],
  ['revokeMCPRefreshGrant', 'RevokeMCPRefreshGrant'],
  ['revokeMCPSession', 'RevokeMCPSession'],
]

function checkSharedClientSurface() {
  const nodeClient = read('sdk/node/src/client.ts')
  const goFiles = [
    'sdk/go/client.go',
    'sdk/go/tags.go',
    'sdk/go/workflow.go',
    'sdk/go/audit.go',
    'sdk/go/gdpr.go',
    'sdk/go/outbox.go',
    'sdk/go/mcp_clients.go',
  ]
    .map(read)
    .join('\n')

  for (const [nodeMethod, goMethod] of sharedClientSurface) {
    requireValue(
      new RegExp(`\\n\\s{2}${nodeMethod}\\(`).test(nodeClient),
      `sdk/node/src/client.ts: missing shared Client.${nodeMethod}()`,
    )
    requireValue(
      new RegExp(`func \\(c \\*Client\\) ${goMethod}\\(`).test(goFiles),
      `sdk/go: missing shared Client.${goMethod}()`,
    )
  }
}

function checkErrorContractParity() {
  requireContains('sdk/node/src/index.ts', 'export { AttuneError, TransportErrorCode }', 'Node error exports')
  requireContains('sdk/node/src/index.ts', 'ErrorResponse', 'Node ErrorResponse type export')
  requireContains('sdk/node/src/index.ts', 'ErrorCode', 'Node ErrorCode enum export')
  requireContains('sdk/go/wire.go', 'type (\n\t// IngestRequest', 'Go wire type aliases')
  requireContains('sdk/go/wire.go', 'ErrorResponse = attunev1.ErrorResponse', 'Go ErrorResponse alias')
  requireContains('sdk/go/wire.go', 'ErrorCode = attunev1.ErrorCode', 'Go ErrorCode alias')
  requireContains('sdk/go/wire.go', 'TransportErrorCode = struct', 'Go transport error group')
  requireContains('sdk/go/errors.go', 'CodeIdempotencyConflict', 'Go idempotency conflict code')
  requireContains('sdk/node/test/client.test.ts', 'AttuneError', 'Node error unit coverage')
  requireContains('sdk/go/errors_test.go', 'CodeIdempotencyConflict', 'Go error unit coverage')
}

function checkRetryAndIdempotencyParity() {
  requireContains('sdk/node/src/index.ts', 'backoffDelay, isRetryable, parseRetryAfter', 'Node retry exports')
  requireContains('sdk/go/retry.go', 'func IsRetryable', 'Go retry export')
  requireContains('sdk/go/retry.go', 'func BackoffDelay', 'Go backoff export')
  requireContains('sdk/go/retry.go', 'func ParseRetryAfter', 'Go Retry-After export')
  requireContains('sdk/node/src/client.ts', "headers['Idempotency-Key'] = idempotencyKey", 'Node Idempotency-Key header')
  requireContains('sdk/go/client.go', 'req.Header.Set("Idempotency-Key", key)', 'Go Idempotency-Key header')
  requireContains('sdk/node/src/client.ts', 'resolveIdempotencyKey(validatedOpts?.idempotencyKey)', 'Node management POST idempotency')
  requireContains('sdk/go/management_http.go', 'resolveRetryablePOSTKey', 'Go management POST idempotency')
  requireContains('sdk/node/test/retry.test.ts', 'parseRetryAfter', 'Node retry unit coverage')
  requireContains('sdk/go/retry_test.go', 'Retry-After', 'Go retry unit coverage')
  requireContains('sdk/node/test/client.test.ts', 'reuses the SAME key across retries', 'Node stable idempotency coverage')
  requireContains('sdk/go/client_test.go', 'idempotency key changed across retries', 'Go stable idempotency coverage')
  requireContains('sdk/go/admin_test.go', 'Management POSTs auto-generate idempotency keys', 'Go management idempotency coverage')
  requireContains('sdk/node/test/admin.test.ts', 'auto-generated idempotency key', 'Node management idempotency coverage')
}

function checkBrowserBoundary() {
  requireContains('sdk/node/README.md', 'Only ship an `ingest:write`', 'browser key-safety warning')
  requireContains('sdk/node/README.md', 'Never put a broader-scope key in client-side code.', 'management key browser boundary')
  requireContains('sdk/node/examples/browser-ingest/main.ts', "source: 'web'", 'browser ingest example')
  requireContains('sdk/node/scripts/browser-smoke.mjs', 'Browser smoke', 'browser smoke example path')
  requireContains('sdk/node/src/client.ts', 'shouldSendUserAgentHeader', 'browser-safe User-Agent branch')
  requireContains('sdk/node/src/client.ts', 'X-Attune-Api-Version', 'browser-safe API version header')
}

function checkReleaseArtifactParity() {
  const pkg = readJSON('sdk/node/package.json')
  requireValue(pkg.name === '@phixsura/attune', 'sdk/node/package.json: package name mismatch')
  requireValue(pkg.type === 'module', 'sdk/node/package.json: package must be ESM-first')
  requireValue(pkg.sideEffects === false, 'sdk/node/package.json: package must declare sideEffects false')
  requireValue(pkg.exports?.['.']?.import?.types === './dist/index.d.ts', 'sdk/node/package.json: missing ESM types export')
  requireValue(pkg.exports?.['.']?.require?.default === './dist/index.cjs', 'sdk/node/package.json: missing CJS export')
  requireValue(pkg.files?.includes('dist'), 'sdk/node/package.json: dist not included in published files')
  requireContains('sdk/node/dist/index.d.ts', 'declare class Client', 'Node packed d.ts')
  requireContains('sdk/node/dist/index.cjs', 'exports.Client', 'Node packed CommonJS entry')
  requireContains('sdk/node/scripts/e2e.sh', 'ATTUNE_E2E_BASE_URL', 'Node live e2e entrypoint')
  requireContains('sdk/go/go.mod', 'module github.com/Phixsura/attune/sdk/go', 'Go SDK module path')
  requireContains('sdk/go/scripts/e2e.sh', 'ATTUNE_E2E_BASE_URL', 'Go live e2e entrypoint')
  requireContains('sdk/go/README.md', 'go get github.com/Phixsura/attune/sdk/go@latest', 'Go install docs')
}

checkSharedClientSurface()
checkErrorContractParity()
checkRetryAndIdempotencyParity()
checkBrowserBoundary()
checkReleaseArtifactParity()

if (failures.length > 0) {
  for (const failure of failures) {
    console.error(`ERROR ${failure}`)
  }
  process.exit(1)
}

console.log(
  `sdk-parity: clean (${sharedClientSurface.length} shared client methods, error/retry/idempotency/browser/release gates verified)`,
)
