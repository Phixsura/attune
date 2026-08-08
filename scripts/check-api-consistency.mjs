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

function requireContains(file, needle, label) {
  const source = read(file)
  if (source && !source.includes(needle)) {
    failures.push(`${file}: missing ${label} (${needle})`)
  }
}

function requireRegex(file, pattern, label) {
  const source = read(file)
  if (source && !pattern.test(source)) {
    failures.push(`${file}: missing ${label} (${pattern})`)
  }
}

function requireValue(value, label) {
  if (!value) failures.push(label)
}

const openApi = read('docs/openapi/openapi.yaml')

const contractSurfaces = [
  {
    name: 'audit log',
    paths: ['/fb/v1/console/audit-log', '/v1/audit-log'],
    params: [
      'action',
      'actions',
      'actorType',
      'actorId',
      'targetType',
      'targetId',
      'from',
      'to',
      'limit',
      'cursor',
    ],
    responsePattern: /ListAuditLogResponse:[\s\S]*?nextCursor:/,
  },
  {
    name: 'GDPR requests',
    paths: ['/fb/v1/console/gdpr/requests', '/v1/gdpr/requests'],
    params: ['cursor', 'limit', 'requestType'],
    responsePattern: /ListGdprRequestsResponse:[\s\S]*?nextCursor:/,
  },
  {
    name: 'outbox deliveries',
    paths: ['/fb/v1/console/outbox/deliveries', '/v1/outbox/deliveries'],
    params: ['status', 'limit', 'beforeId'],
    responsePattern: /ListDeliveriesResponse:[\s\S]*?nextBeforeId:/,
  },
]

function pathBlock(pathName) {
  const marker = `    ${pathName}:`
  const start = openApi.indexOf(marker)
  if (start < 0) return ''
  const rest = openApi.slice(start + marker.length)
  const next = rest.search(/\n    \//)
  return next < 0 ? rest : rest.slice(0, next)
}

function checkOpenApiPaginationAndFilters() {
  for (const surface of contractSurfaces) {
    for (const pathName of surface.paths) {
      const block = pathBlock(pathName)
      requireValue(block, `docs/openapi/openapi.yaml: missing ${surface.name} path ${pathName}`)
      for (const param of surface.params) {
        requireValue(
          block.includes(`- name: ${param}`),
          `docs/openapi/openapi.yaml: ${pathName} missing ${surface.name} query parameter ${param}`,
        )
      }
      requireValue(
        block.includes("$ref: '#/components/schemas/ErrorResponse'"),
        `docs/openapi/openapi.yaml: ${pathName} missing shared ErrorResponse envelope`,
      )
      if (pathName.startsWith('/v1/')) {
        requireValue(
          block.includes('- name: X-Attune-Api-Version'),
          `docs/openapi/openapi.yaml: ${pathName} missing API version header`,
        )
      }
    }
    requireValue(
      surface.responsePattern.test(openApi),
      `docs/openapi/openapi.yaml: ${surface.name} response missing pagination cursor field`,
    )
  }
}

function checkSortEnumCoverage() {
  requireRegex(
    'docs/openapi/openapi.yaml',
    /- name: sort[\s\S]*?CUSTOMER_REQUEST_SORT_DECISION_SCORE[\s\S]*?- name: direction[\s\S]*?SORT_DIRECTION_DESC/,
    'Customer Request sort and direction OpenAPI enums',
  )
  requireContains(
    'proto/attune/v1/customer_request.proto',
    'enum CustomerRequestSort',
    'CustomerRequestSort proto enum',
  )
  requireContains(
    'proto/attune/v1/customer_request.proto',
    'CUSTOMER_REQUEST_SORT_DECISION_SCORE',
    'decision score sort enum value',
  )
  requireContains(
    'proto/attune/v1/customer_request.proto',
    'enum SortDirection',
    'SortDirection proto enum',
  )
  requireContains(
    'console/src/proto/attune/v1/customer_request.ts',
    'export enum CustomerRequestSort',
    'Console generated CustomerRequestSort',
  )
  requireContains(
    'sdk/node/src/proto/attune/v1/customer_request.ts',
    'export enum SortDirection',
    'Node SDK generated SortDirection',
  )
  requireContains(
    'internal/proto/attune/v1/customer_request.pb.go',
    'CustomerRequestSort_CUSTOMER_REQUEST_SORT_DECISION_SCORE',
    'Go generated CustomerRequestSort',
  )
}

function checkSdkWireSemantics() {
  requireContains('sdk/node/src/client.ts', 'async *iterateAuditLog', 'Node audit cursor pager')
  requireContains('sdk/node/src/client.ts', 'async *iterateGdprRequests', 'Node GDPR cursor pager')
  requireContains('sdk/node/src/client.ts', 'async *iterateOutboxDeliveries', 'Node outbox before_id pager')
  requireContains('sdk/node/src/client.ts', "query.append('actions', item)", 'Node repeated audit actions filter')
  requireContains('sdk/node/src/client.ts', "setQueryValue(query, 'request_type', requestType)", 'Node GDPR request_type wire name')
  requireContains('sdk/node/src/client.ts', "setQueryValue(query, 'before_id', beforeId)", 'Node outbox before_id wire name')
  requireContains('sdk/node/src/client.ts', 'validateOptionalPositiveInt32', 'Node positive limit validation')
  requireContains('sdk/node/src/client.ts', 'validateOptionalOutboxStatuses', 'Node outbox status validation')

  requireContains('sdk/go/audit.go', 'type AuditLogPager struct', 'Go audit cursor pager')
  requireContains('sdk/go/gdpr.go', 'type GdprRequestPager struct', 'Go GDPR cursor pager')
  requireContains('sdk/go/outbox.go', 'type OutboxDeliveryPager struct', 'Go outbox before_id pager')
  requireContains('sdk/go/audit.go', 'query.Add("actions", action)', 'Go repeated audit actions filter')
  requireContains('sdk/go/gdpr.go', 'query.Set("request_type", req.GetRequestType())', 'Go GDPR request_type wire name')
  requireContains('sdk/go/outbox.go', 'query.Set("before_id", strconv.FormatInt(req.GetBeforeId(), 10))', 'Go outbox before_id wire name')
  requireContains('sdk/go/query.go', 'validateNonNegativeProtoInt32', 'Go positive limit validation')
  requireContains('sdk/go/query.go', 'normalizeOutboxStatuses', 'Go outbox status validation')

  requireContains(
    'sdk/node/test/admin.test.ts',
    '/v1/audit-log?actions=tag.create&actions=tag.archive',
    'Node audit repeated action wire test',
  )
  requireContains(
    'sdk/node/test/admin.test.ts',
    '/v1/gdpr/requests?cursor=c1&limit=10&request_type=export',
    'Node GDPR request_type wire test',
  )
  requireContains(
    'sdk/node/test/admin.test.ts',
    '/v1/outbox/deliveries?status=dead&status=failed&limit=20&before_id=99',
    'Node outbox before_id wire test',
  )
  requireContains(
    'sdk/go/admin_test.go',
    'actions=tag.create&actions=tag.archive',
    'Go audit repeated action wire test',
  )
  requireContains(
    'sdk/go/admin_test.go',
    'cursor=c1&limit=10&request_type=export',
    'Go GDPR request_type wire test',
  )
  requireContains(
    'sdk/go/admin_test.go',
    'before_id=99&limit=20&status=dead&status=failed',
    'Go outbox before_id wire test',
  )
}

function checkErrorAndIdempotencySemantics() {
  requireContains('docs/openapi/openapi.yaml', 'ErrorResponse:', 'OpenAPI ErrorResponse schema')
  requireContains('sdk/node/src/client.ts', 'function parseErrorBody', 'Node ErrorResponse parser')
  requireContains('sdk/go/errors.go', 'errorFromResponse', 'Go ErrorResponse parser')
  requireContains('sdk/node/src/index.ts', 'ErrorResponse', 'Node ErrorResponse export')
  requireContains('sdk/go/wire.go', 'ErrorResponse = attunev1.ErrorResponse', 'Go ErrorResponse alias')
  requireContains('docs/openapi/openapi.yaml', '- name: Idempotency-Key', 'OpenAPI Idempotency-Key header')
  requireContains('sdk/node/src/client.ts', "headers['Idempotency-Key'] = idempotencyKey", 'Node Idempotency-Key header')
  requireContains('sdk/go/client.go', 'req.Header.Set("Idempotency-Key", key)', 'Go Idempotency-Key header')
  requireContains(
    'sdk/node/test/admin.test.ts',
    'auto-generated idempotency key',
    'Node management idempotency coverage',
  )
  requireContains(
    'sdk/go/admin_test.go',
    'Management POSTs auto-generate idempotency keys',
    'Go management idempotency coverage',
  )
}

checkOpenApiPaginationAndFilters()
checkSortEnumCoverage()
checkSdkWireSemantics()
checkErrorAndIdempotencySemantics()

if (failures.length > 0) {
  for (const failure of failures) {
    console.error(`ERROR ${failure}`)
  }
  process.exit(1)
}

console.log(
  `api-consistency: clean (${contractSurfaces.length} paginated public surfaces, console mirrors, filter/sort/error/idempotency semantics verified)`,
)
