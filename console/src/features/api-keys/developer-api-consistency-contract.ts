export type DeveloperApiConsistencyStatus = 'verified' | 'watch' | 'blocked' | 'needs_data'

export type DeveloperApiConsistencyLaneKey =
  | 'pagination_contract'
  | 'filter_contract'
  | 'sort_contract'
  | 'error_envelope'
  | 'idempotency_contract'
  | 'sdk_wire_semantics'

export type DeveloperApiConsistencyLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: DeveloperApiConsistencyLaneKey
  owner: string
  signal: string
  status: DeveloperApiConsistencyStatus
  title: string
}

export type DeveloperApiConsistencyContract = {
  fingerprint: string
  lanes: DeveloperApiConsistencyLane[]
  summary: string
  totals: Record<DeveloperApiConsistencyStatus, number> & {
    total: number
  }
}

export type DeveloperApiConsistencyArtifacts = {
  apiConsistencyVerifier: boolean
  consolePaginationSurfaces: number
  cursorPaginationSurfaces: number
  expectedConsolePaginationSurfaces: number
  expectedFilterSurfaces: number
  expectedPaginationSurfaces: number
  expectedSdkPagers: number
  expectedSortSurfaces: number
  filterSurfaces: number
  goErrorEnvelope: boolean
  goIdempotencyCoverage: boolean
  goQueryValidation: boolean
  goSdkPagers: number
  goWireQueryCoverage: boolean
  idempotencyHeaderOpenApi: boolean
  nodeErrorEnvelope: boolean
  nodeIdempotencyCoverage: boolean
  nodeQueryValidation: boolean
  nodeSdkPagers: number
  nodeWireQueryCoverage: boolean
  openApiErrorEnvelope: boolean
  openApiSortEnum: boolean
  publicPaginationSurfaces: number
  sdkSortEnum: boolean
  sortSurfaces: number
}

export const defaultDeveloperApiConsistencyArtifacts: DeveloperApiConsistencyArtifacts = {
  apiConsistencyVerifier: true,
  consolePaginationSurfaces: 3,
  cursorPaginationSurfaces: 2,
  expectedConsolePaginationSurfaces: 3,
  expectedFilterSurfaces: 3,
  expectedPaginationSurfaces: 3,
  expectedSdkPagers: 3,
  expectedSortSurfaces: 3,
  filterSurfaces: 3,
  goErrorEnvelope: true,
  goIdempotencyCoverage: true,
  goQueryValidation: true,
  goSdkPagers: 3,
  goWireQueryCoverage: true,
  idempotencyHeaderOpenApi: true,
  nodeErrorEnvelope: true,
  nodeIdempotencyCoverage: true,
  nodeQueryValidation: true,
  nodeSdkPagers: 3,
  nodeWireQueryCoverage: true,
  openApiErrorEnvelope: true,
  openApiSortEnum: true,
  publicPaginationSurfaces: 3,
  sdkSortEnum: true,
  sortSurfaces: 3,
}

export type DeveloperApiConsistencyContractInput = {
  artifacts?: DeveloperApiConsistencyArtifacts
}

export function buildDeveloperApiConsistencyContract(
  input: DeveloperApiConsistencyContractInput = {},
): DeveloperApiConsistencyContract {
  const artifacts = input.artifacts ?? defaultDeveloperApiConsistencyArtifacts
  const lanes = [
    paginationContractLane(artifacts),
    filterContractLane(artifacts),
    sortContractLane(artifacts),
    errorEnvelopeLane(artifacts),
    idempotencyContractLane(artifacts),
    sdkWireSemanticsLane(artifacts),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    total: lanes.length,
    verified: lanes.filter((lane) => lane.status === 'verified').length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${artifacts.publicPaginationSurfaces}/${artifacts.expectedPaginationSurfaces} public pagination surfaces / ${artifacts.consolePaginationSurfaces}/${artifacts.expectedConsolePaginationSurfaces} console mirrors / ${artifacts.filterSurfaces}/${artifacts.expectedFilterSurfaces} filters / ${artifacts.sortSurfaces}/${artifacts.expectedSortSurfaces} sort enums / verifier ${onOff(artifacts.apiConsistencyVerifier)}`,
    lanes,
    summary: developerApiConsistencySummary(totals),
    totals,
  }
}

function paginationContractLane(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyLane {
  return {
    actionHref: 'https://github.com/Phixsura/attune/blob/main/scripts/check-api-consistency.mjs',
    actionLabel: 'Run consistency verifier',
    evidence: `public ${artifacts.publicPaginationSurfaces}/${artifacts.expectedPaginationSurfaces} / console ${artifacts.consolePaginationSurfaces}/${artifacts.expectedConsolePaginationSurfaces} / Node pagers ${artifacts.nodeSdkPagers}/${artifacts.expectedSdkPagers} / Go pagers ${artifacts.goSdkPagers}/${artifacts.expectedSdkPagers}`,
    guardrail:
      'Audit log, GDPR requests, and outbox deliveries must keep cursor or keyset pagination identical across OpenAPI, console mirrors, and SDK pagers.',
    key: 'pagination_contract',
    owner: 'Developer Platform + API',
    signal: `${artifacts.cursorPaginationSurfaces} cursor surfaces / 1 before_id surface / nextCursor + nextBeforeId`,
    status: paginationContractStatus(artifacts),
    title: 'Pagination contract',
  }
}

function filterContractLane(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyLane {
  return {
    actionHref: 'https://github.com/Phixsura/attune/blob/main/docs/openapi/openapi.yaml',
    actionLabel: 'Review filter contract',
    evidence: `filters ${artifacts.filterSurfaces}/${artifacts.expectedFilterSurfaces} / Node validation ${available(artifacts.nodeQueryValidation)} / Go validation ${available(artifacts.goQueryValidation)}`,
    guardrail:
      'Action, actor, target, time-window, GDPR request type, and outbox status filters need typed validation and stable wire names before integrators automate them.',
    key: 'filter_contract',
    owner: 'Developer Platform + Integrations',
    signal: 'audit actions + actor/target/time / request_type / status[]',
    status: filterContractStatus(artifacts),
    title: 'Filter semantics',
  }
}

function sortContractLane(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyLane {
  return {
    actionHref:
      'https://github.com/Phixsura/attune/blob/main/proto/attune/v1/customer_request.proto',
    actionLabel: 'Review sort enums',
    evidence: `sort surfaces ${artifacts.sortSurfaces}/${artifacts.expectedSortSurfaces} / OpenAPI enum ${available(artifacts.openApiSortEnum)} / generated SDK enum ${available(artifacts.sdkSortEnum)}`,
    guardrail:
      'Customer Request list, saved view, and account summary sorting must remain enum-backed so dashboards and exports cannot silently reinterpret rank order.',
    key: 'sort_contract',
    owner: 'Developer Platform + Console',
    signal: 'CustomerRequestSort + SortDirection / decision score / delivery health',
    status: sortContractStatus(artifacts),
    title: 'Sort enum contract',
  }
}

function errorEnvelopeLane(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyLane {
  return {
    actionHref:
      'https://github.com/Phixsura/attune/blob/main/docs/openapi/README.md#error-envelope',
    actionLabel: 'Review error envelope',
    evidence: `OpenAPI ${available(artifacts.openApiErrorEnvelope)} / Node ${available(
      artifacts.nodeErrorEnvelope,
    )} / Go ${available(artifacts.goErrorEnvelope)}`,
    guardrail:
      'Every non-2xx response must continue to parse into the shared ErrorResponse envelope so retry, support, and telemetry code can classify failures deterministically.',
    key: 'error_envelope',
    owner: 'Developer Platform + Support',
    signal: 'ErrorResponse code/message/requestId across OpenAPI, Node, and Go',
    status: errorEnvelopeStatus(artifacts),
    title: 'Error envelope',
  }
}

function idempotencyContractLane(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyLane {
  return {
    actionHref: 'https://github.com/Phixsura/attune/tree/main/sdk',
    actionLabel: 'Review idempotency tests',
    evidence: `OpenAPI header ${available(
      artifacts.idempotencyHeaderOpenApi,
    )} / Node coverage ${available(artifacts.nodeIdempotencyCoverage)} / Go coverage ${available(
      artifacts.goIdempotencyCoverage,
    )}`,
    guardrail:
      'Management writes must keep stable Idempotency-Key behavior across generated contracts and both SDKs so safe retries cannot create duplicate work.',
    key: 'idempotency_contract',
    owner: 'Developer Platform + Reliability',
    signal: `Idempotency-Key / management POST coverage ${coverageCount([
      artifacts.nodeIdempotencyCoverage,
      artifacts.goIdempotencyCoverage,
    ])}/2`,
    status: idempotencyContractStatus(artifacts),
    title: 'Idempotency semantics',
  }
}

function sdkWireSemanticsLane(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyLane {
  return {
    actionHref: 'https://github.com/Phixsura/attune/tree/main/sdk',
    actionLabel: 'Review SDK query fixtures',
    evidence: `Node wire tests ${available(artifacts.nodeWireQueryCoverage)} / Go wire tests ${available(
      artifacts.goWireQueryCoverage,
    )} / verifier ${available(artifacts.apiConsistencyVerifier)}`,
    guardrail:
      'SDK tests must pin repeated array filters, snake-case wire aliases, positive limits, cursor pagination, and before_id pagination before API examples ship.',
    key: 'sdk_wire_semantics',
    owner: 'Developer Platform',
    signal: 'actions[]=repeat / request_type / before_id / positive limit validators',
    status: sdkWireSemanticsStatus(artifacts),
    title: 'SDK wire semantics',
  }
}

function paginationContractStatus(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyStatus {
  if (!artifacts.apiConsistencyVerifier) return 'blocked'
  if (
    artifacts.publicPaginationSurfaces < artifacts.expectedPaginationSurfaces ||
    artifacts.consolePaginationSurfaces < artifacts.expectedConsolePaginationSurfaces ||
    artifacts.nodeSdkPagers < artifacts.expectedSdkPagers ||
    artifacts.goSdkPagers < artifacts.expectedSdkPagers
  ) {
    return 'blocked'
  }
  return 'verified'
}

function filterContractStatus(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyStatus {
  if (artifacts.filterSurfaces < artifacts.expectedFilterSurfaces) return 'blocked'
  if (!artifacts.nodeQueryValidation || !artifacts.goQueryValidation) return 'watch'
  return 'verified'
}

function sortContractStatus(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyStatus {
  if (artifacts.sortSurfaces < artifacts.expectedSortSurfaces || !artifacts.openApiSortEnum) {
    return 'blocked'
  }
  if (!artifacts.sdkSortEnum) return 'watch'
  return 'verified'
}

function errorEnvelopeStatus(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyStatus {
  if (!artifacts.openApiErrorEnvelope) return 'blocked'
  if (!artifacts.nodeErrorEnvelope || !artifacts.goErrorEnvelope) return 'watch'
  return 'verified'
}

function idempotencyContractStatus(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyStatus {
  if (!artifacts.idempotencyHeaderOpenApi) return 'blocked'
  if (!artifacts.nodeIdempotencyCoverage || !artifacts.goIdempotencyCoverage) return 'watch'
  return 'verified'
}

function sdkWireSemanticsStatus(
  artifacts: DeveloperApiConsistencyArtifacts,
): DeveloperApiConsistencyStatus {
  if (!artifacts.apiConsistencyVerifier) return 'blocked'
  if (!artifacts.nodeWireQueryCoverage || !artifacts.goWireQueryCoverage) return 'blocked'
  return 'verified'
}

function developerApiConsistencySummary(totals: DeveloperApiConsistencyContract['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} API consistency lanes are blocked`
  /* v8 ignore next -- @preserve: API consistency lanes never emit needs_data; kept for shared status-union exhaustiveness. */
  if (totals.needs_data > 0) return `${totals.needs_data} API consistency lanes need evidence`
  if (totals.watch > 0) return `${totals.watch} API consistency lanes need hardening`
  return 'developer API consistency contract is verified'
}

function coverageCount(values: boolean[]): number {
  return values.filter(Boolean).length
}

function available(value: boolean): string {
  return value ? 'available' : 'missing'
}

function onOff(value: boolean): string {
  return value ? 'on' : 'off'
}
