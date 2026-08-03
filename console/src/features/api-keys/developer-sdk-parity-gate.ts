import type { ApiKey } from '@/features/api-keys/api/list-api-keys'

export type DeveloperSdkParityStatus = 'verified' | 'watch' | 'blocked' | 'needs_data'

export type DeveloperSdkParityLaneKey =
  | 'management_surface'
  | 'error_contract'
  | 'retry_idempotency'
  | 'browser_boundary'
  | 'release_artifacts'

export type DeveloperSdkParityLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: DeveloperSdkParityLaneKey
  owner: string
  signal: string
  status: DeveloperSdkParityStatus
  title: string
}

export type DeveloperSdkParityGate = {
  fingerprint: string
  lanes: DeveloperSdkParityLane[]
  summary: string
  totals: Record<DeveloperSdkParityStatus, number> & {
    total: number
  }
}

export type DeveloperSdkParityArtifacts = {
  apiVersionPinned: boolean
  browserExample: boolean
  errorContractExports: boolean
  expectedSharedClientMethods: number
  goClientMethods: number
  goLiveE2e: boolean
  goModule: boolean
  idempotencyCoverage: boolean
  nodeBrowserSmoke: boolean
  nodeClientMethods: number
  nodeLiveE2e: boolean
  nodePackageExports: boolean
  nodePackedTypes: boolean
  openApiErrorEnvelope: boolean
  packedArtifactInstallSmoke: boolean
  retryPolicyExports: boolean
  sdkParityVerifier: boolean
  serverOnlyManagementWarning: boolean
  sharedClientMethods: number
  transportErrors: boolean
}

export type DeveloperSdkParityGateInput = {
  apiKeys?: ApiKey[]
  artifacts?: DeveloperSdkParityArtifacts
}

export const defaultDeveloperSdkParityArtifacts: DeveloperSdkParityArtifacts = {
  apiVersionPinned: true,
  browserExample: true,
  errorContractExports: true,
  expectedSharedClientMethods: 35,
  goClientMethods: 35,
  goLiveE2e: true,
  goModule: true,
  idempotencyCoverage: true,
  nodeBrowserSmoke: true,
  nodeClientMethods: 35,
  nodeLiveE2e: true,
  nodePackageExports: true,
  nodePackedTypes: true,
  openApiErrorEnvelope: true,
  packedArtifactInstallSmoke: true,
  retryPolicyExports: true,
  sdkParityVerifier: true,
  serverOnlyManagementWarning: true,
  sharedClientMethods: 35,
  transportErrors: true,
}

export function buildDeveloperSdkParityGate(
  input: DeveloperSdkParityGateInput,
): DeveloperSdkParityGate {
  const artifacts = input.artifacts ?? defaultDeveloperSdkParityArtifacts
  const lanes = [
    managementSurfaceLane(artifacts),
    errorContractLane(artifacts),
    retryIdempotencyLane(artifacts),
    browserBoundaryLane(input, artifacts),
    releaseArtifactsLane(artifacts),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    total: lanes.length,
    verified: lanes.filter((lane) => lane.status === 'verified').length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${artifacts.sharedClientMethods}/${artifacts.expectedSharedClientMethods} shared methods / verifier ${onOff(
      artifacts.sdkParityVerifier,
    )} / ${browserSafeKeys(input.apiKeys).length} browser-safe keys / ${releaseGateCount(
      artifacts,
    )}/${releaseGateTotal} release gates`,
    lanes,
    summary: developerSdkParitySummary(totals),
    totals,
  }
}

function managementSurfaceLane(artifacts: DeveloperSdkParityArtifacts): DeveloperSdkParityLane {
  const drift =
    Math.abs(artifacts.nodeClientMethods - artifacts.goClientMethods) +
    Math.abs(artifacts.expectedSharedClientMethods - artifacts.sharedClientMethods)

  return {
    actionHref: 'https://github.com/Phixsura/attune/blob/main/scripts/check-sdk-parity.mjs',
    actionLabel: 'Run parity verifier',
    evidence: `${artifacts.sharedClientMethods} expected shared methods / Node ${artifacts.nodeClientMethods} / Go ${artifacts.goClientMethods} / verifier ${available(
      artifacts.sdkParityVerifier,
    )}`,
    guardrail:
      'Node and Go clients must expose the same public management surface before external teams can rely on either SDK interchangeably.',
    key: 'management_surface',
    owner: 'Developer Platform',
    signal: `${artifacts.sharedClientMethods} shared methods / Node ${artifacts.nodeClientMethods} / Go ${artifacts.goClientMethods} / drift ${drift}`,
    status: managementSurfaceStatus(artifacts),
    title: 'Public management surface parity',
  }
}

function errorContractLane(artifacts: DeveloperSdkParityArtifacts): DeveloperSdkParityLane {
  return {
    actionHref:
      'https://github.com/Phixsura/attune/blob/main/docs/openapi/README.md#error-envelope',
    actionLabel: 'Review error contract',
    evidence: `ErrorResponse ${available(artifacts.openApiErrorEnvelope)} / SDK exports ${available(
      artifacts.errorContractExports,
    )} / transport errors ${available(artifacts.transportErrors)}`,
    guardrail:
      'Both SDKs must expose the same server error envelope, generated error enum, and transport error categories so retries and support tooling stay deterministic.',
    key: 'error_contract',
    owner: 'Developer Platform + Support',
    signal: `ErrorResponse + ErrorCode / AttuneError + TransportErrorCode / envelope ${available(
      artifacts.openApiErrorEnvelope,
    )}`,
    status: errorContractStatus(artifacts),
    title: 'Error contract parity',
  }
}

function retryIdempotencyLane(artifacts: DeveloperSdkParityArtifacts): DeveloperSdkParityLane {
  return {
    actionHref: 'https://github.com/Phixsura/attune/tree/main/sdk',
    actionLabel: 'Review retry tests',
    evidence: `retry exports ${available(artifacts.retryPolicyExports)} / idempotency tests ${available(
      artifacts.idempotencyCoverage,
    )} / API version ${available(artifacts.apiVersionPinned)}`,
    guardrail:
      'Transient retry rules, Retry-After handling, stable Idempotency-Key generation, and API-version pinning must remain lockstep across SDKs.',
    key: 'retry_idempotency',
    owner: 'Developer Platform + Reliability',
    signal: `408/429/5xx / Retry-After / idempotency ${available(
      artifacts.idempotencyCoverage,
    )} / API version pinned ${onOff(artifacts.apiVersionPinned)}`,
    status: retryIdempotencyStatus(artifacts),
    title: 'Retry and idempotency parity',
  }
}

function browserBoundaryLane(
  input: DeveloperSdkParityGateInput,
  artifacts: DeveloperSdkParityArtifacts,
): DeveloperSdkParityLane {
  const safeKeys = browserSafeKeys(input.apiKeys)
  const managementKeys = managementScopedKeys(input.apiKeys)

  return {
    actionHref: 'https://github.com/Phixsura/attune/tree/main/sdk/node/examples/browser-ingest',
    actionLabel: 'Review browser example',
    evidence: input.apiKeys
      ? `browser smoke ${available(artifacts.nodeBrowserSmoke)} / example ${available(
          artifacts.browserExample,
        )} / warning ${available(artifacts.serverOnlyManagementWarning)} / ${safeKeys.length} ingest-only active keys`
      : 'browser key-safety evidence is missing',
    guardrail:
      'Only ingest:write keys may be browser-publishable; management scopes must stay server-side and the shipped package must prove this boundary in a real browser.',
    key: 'browser_boundary',
    owner: 'Developer Platform + Security',
    signal: input.apiKeys
      ? `${safeKeys.length} browser-safe keys / ${managementKeys.length} management scoped keys / browser smoke ${onOff(
          artifacts.nodeBrowserSmoke,
        )}`
      : 'browser key evidence missing',
    status: browserBoundaryStatus(input, artifacts),
    title: 'Browser-safe usage boundary',
  }
}

function releaseArtifactsLane(artifacts: DeveloperSdkParityArtifacts): DeveloperSdkParityLane {
  return {
    actionHref: 'https://github.com/Phixsura/attune/tree/main/sdk',
    actionLabel: 'Review SDK release artifacts',
    evidence: `npm exports ${available(artifacts.nodePackageExports)} / d.ts ${available(
      artifacts.nodePackedTypes,
    )} / Go module ${available(artifacts.goModule)} / packed smoke ${available(
      artifacts.packedArtifactInstallSmoke,
    )}`,
    guardrail:
      'Release artifacts must prove installability through package metadata, generated types, packed artifact smoke, and live e2e scripts for both SDKs.',
    key: 'release_artifacts',
    owner: 'Developer Platform + Release',
    signal: `npm ESM+CJS+types / Go module / live e2e ${liveE2eCount(artifacts)}/2 / packed smoke ${onOff(
      artifacts.packedArtifactInstallSmoke,
    )}`,
    status: releaseArtifactsStatus(artifacts),
    title: 'Release artifact parity',
  }
}

function managementSurfaceStatus(artifacts: DeveloperSdkParityArtifacts): DeveloperSdkParityStatus {
  if (!artifacts.sdkParityVerifier) return 'blocked'
  if (
    artifacts.sharedClientMethods < artifacts.expectedSharedClientMethods ||
    artifacts.nodeClientMethods !== artifacts.goClientMethods ||
    artifacts.sharedClientMethods !== artifacts.nodeClientMethods
  ) {
    return 'blocked'
  }
  return 'verified'
}

function errorContractStatus(artifacts: DeveloperSdkParityArtifacts): DeveloperSdkParityStatus {
  if (!artifacts.openApiErrorEnvelope || !artifacts.errorContractExports) return 'blocked'
  if (!artifacts.transportErrors) return 'watch'
  return 'verified'
}

function retryIdempotencyStatus(artifacts: DeveloperSdkParityArtifacts): DeveloperSdkParityStatus {
  if (!artifacts.retryPolicyExports || !artifacts.idempotencyCoverage) return 'blocked'
  if (!artifacts.apiVersionPinned) return 'watch'
  return 'verified'
}

function browserBoundaryStatus(
  input: DeveloperSdkParityGateInput,
  artifacts: DeveloperSdkParityArtifacts,
): DeveloperSdkParityStatus {
  if (!input.apiKeys) return 'needs_data'
  if (!artifacts.browserExample || !artifacts.nodeBrowserSmoke) return 'blocked'
  if (!artifacts.serverOnlyManagementWarning || browserSafeKeys(input.apiKeys).length === 0) {
    return 'watch'
  }
  return 'verified'
}

function releaseArtifactsStatus(artifacts: DeveloperSdkParityArtifacts): DeveloperSdkParityStatus {
  if (!artifacts.nodePackageExports || !artifacts.nodePackedTypes || !artifacts.goModule) {
    return 'blocked'
  }
  if (!artifacts.nodeLiveE2e || !artifacts.goLiveE2e || !artifacts.packedArtifactInstallSmoke) {
    return 'watch'
  }
  return 'verified'
}

function developerSdkParitySummary(totals: DeveloperSdkParityGate['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} SDK parity lanes are blocked`
  if (totals.needs_data > 0)
    return `${totals.needs_data} SDK parity lanes need browser key evidence`
  if (totals.watch > 0) return `${totals.watch} SDK parity lanes need hardening`
  return 'Node/Go SDK parity gate is verified'
}

function browserSafeKeys(keys: ApiKey[] | undefined): ApiKey[] {
  return activeKeys(keys).filter((key) => isBrowserSafeScopeSet(key.scopes))
}

function managementScopedKeys(keys: ApiKey[] | undefined): ApiKey[] {
  return activeKeys(keys).filter((key) => key.scopes.some((scope) => scope !== 'ingest:write'))
}

function activeKeys(keys: ApiKey[] | undefined): ApiKey[] {
  return (keys ?? []).filter((key) => key.isActive)
}

function isBrowserSafeScopeSet(scopes: string[]): boolean {
  return scopes.length === 1 && scopes[0] === 'ingest:write'
}

const releaseGateTotal = 6

function releaseGateCount(artifacts: DeveloperSdkParityArtifacts): number {
  return [
    artifacts.nodePackageExports,
    artifacts.nodePackedTypes,
    artifacts.goModule,
    artifacts.nodeLiveE2e,
    artifacts.goLiveE2e,
    artifacts.packedArtifactInstallSmoke,
  ].filter(Boolean).length
}

function liveE2eCount(artifacts: DeveloperSdkParityArtifacts): number {
  return [artifacts.nodeLiveE2e, artifacts.goLiveE2e].filter(Boolean).length
}

function available(value: boolean): string {
  return value ? 'available' : 'missing'
}

function onOff(value: boolean): string {
  return value ? 'on' : 'off'
}
