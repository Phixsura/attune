import type { ApiKey } from '@/features/api-keys/api/list-api-keys'
import type { ServiceAccount } from '@/features/api-keys/api/list-service-accounts'
import type { ScopeInfo, ScopePreset } from '@/proto/attune/v1/api_key'

export type DeveloperApiAdoptionStatus = 'ready' | 'watch' | 'blocked' | 'needs_data'

export type DeveloperApiAdoptionLaneKey =
  | 'openapi_contract'
  | 'node_sdk'
  | 'go_sdk'
  | 'example_sandbox'
  | 'webhook_replay'

export type DeveloperApiAdoptionLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: DeveloperApiAdoptionLaneKey
  owner: string
  signal: string
  status: DeveloperApiAdoptionStatus
  title: string
}

export type DeveloperApiAdoptionKit = {
  fingerprint: string
  lanes: DeveloperApiAdoptionLane[]
  summary: string
  totals: Record<DeveloperApiAdoptionStatus, number> & {
    total: number
  }
}

export type DeveloperApiAdoptionArtifacts = {
  browserIngestExample: boolean
  demoBootstrap: boolean
  demoTestingGuide: boolean
  goExample: boolean
  goSdkE2e: boolean
  goSdkModule: boolean
  nodeExample: boolean
  nodeSdkBrowserSmoke: boolean
  nodeSdkE2e: boolean
  nodeSdkPackage: boolean
  openApiContract: boolean
  openApiGuide: boolean
  replayFixtureCount: number
  replaySmoke: boolean
}

export type DeveloperApiAdoptionKitInput = {
  apiKeys?: ApiKey[]
  artifacts?: DeveloperApiAdoptionArtifacts
  scopes?: ScopeInfo[]
  serviceAccounts?: ServiceAccount[]
  scopePresets?: ScopePreset[]
}

export const defaultDeveloperApiAdoptionArtifacts: DeveloperApiAdoptionArtifacts = {
  browserIngestExample: true,
  demoBootstrap: true,
  demoTestingGuide: true,
  goExample: true,
  goSdkE2e: true,
  goSdkModule: true,
  nodeExample: true,
  nodeSdkBrowserSmoke: true,
  nodeSdkE2e: true,
  nodeSdkPackage: true,
  openApiContract: true,
  openApiGuide: true,
  replayFixtureCount: 4,
  replaySmoke: true,
}

export function buildDeveloperApiAdoptionKit(
  input: DeveloperApiAdoptionKitInput,
): DeveloperApiAdoptionKit {
  const artifacts = input.artifacts ?? defaultDeveloperApiAdoptionArtifacts
  const lanes = [
    openApiContractLane(input, artifacts),
    nodeSdkLane(input, artifacts),
    goSdkLane(input, artifacts),
    exampleSandboxLane(input, artifacts),
    webhookReplayLane(input, artifacts),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    ready: lanes.filter((lane) => lane.status === 'ready').length,
    total: lanes.length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${input.scopes?.length ?? 0} scopes / ${input.scopePresets?.length ?? 0} presets / ${
      activeApiKeys(input.apiKeys).length
    } active keys / ${
      activeServiceAccounts(input.serviceAccounts).length
    } service accounts / ${verifiedArtifactCount(artifacts)}/${artifactCount} artifacts verified`,
    lanes,
    summary: developerApiAdoptionSummary(totals),
    totals,
  }
}

function openApiContractLane(
  input: DeveloperApiAdoptionKitInput,
  artifacts: DeveloperApiAdoptionArtifacts,
): DeveloperApiAdoptionLane {
  const active = activeApiKeys(input.apiKeys)
  const used = active.filter((key) => Boolean(key.lastUsedAt)).length

  return {
    actionHref: 'https://github.com/Phixsura/attune/blob/main/docs/openapi/openapi.yaml',
    actionLabel: 'Open API contract',
    evidence:
      input.scopes && input.scopePresets && input.apiKeys
        ? `${input.scopes.length} scopes / ${input.scopePresets.length} presets / OpenAPI ${available(
            artifacts.openApiContract,
          )} / guide ${available(artifacts.openApiGuide)}`
        : 'OpenAPI contract inventory evidence is missing',
    guardrail:
      'External teams need generated OpenAPI, stable scope metadata, preset templates, and live key usage evidence before they can adopt the API safely.',
    key: 'openapi_contract',
    owner: 'Developer Platform',
    signal:
      input.scopes && input.scopePresets && input.apiKeys
        ? `${input.scopes.length} scopes / ${input.scopePresets.length} presets / ${active.length} active keys / ${used} used`
        : 'API contract evidence missing',
    status: openApiContractStatus(input, artifacts),
    title: 'OpenAPI contract inventory',
  }
}

function nodeSdkLane(
  input: DeveloperApiAdoptionKitInput,
  artifacts: DeveloperApiAdoptionArtifacts,
): DeveloperApiAdoptionLane {
  const automationIdentities = activeAutomationIdentities(input.apiKeys, input.serviceAccounts)

  return {
    actionHref: 'https://github.com/Phixsura/attune/tree/main/sdk/node',
    actionLabel: 'Review Node SDK',
    evidence:
      input.apiKeys && input.serviceAccounts
        ? `package ${available(artifacts.nodeSdkPackage)} / e2e ${available(
            artifacts.nodeSdkE2e,
          )} / browser smoke ${available(
            artifacts.nodeSdkBrowserSmoke,
          )} / ${automationIdentities} automation identities`
        : 'Node SDK adoption evidence is missing',
    guardrail:
      'The Node SDK path needs a packaged client, live e2e verification, browser smoke coverage, examples, and automation identity evidence.',
    key: 'node_sdk',
    owner: 'Developer Platform',
    signal:
      input.apiKeys && input.serviceAccounts
        ? `${artifacts.nodeExample ? 1 : 0} Node examples / e2e ${onOff(
            artifacts.nodeSdkE2e,
          )} / browser smoke ${onOff(artifacts.nodeSdkBrowserSmoke)} / ${automationIdentities} automation identities`
        : 'Node SDK evidence missing',
    status: nodeSdkStatus(input, artifacts),
    title: 'Node SDK adoption path',
  }
}

function goSdkLane(
  input: DeveloperApiAdoptionKitInput,
  artifacts: DeveloperApiAdoptionArtifacts,
): DeveloperApiAdoptionLane {
  const active = activeApiKeys(input.apiKeys)

  return {
    actionHref: 'https://github.com/Phixsura/attune/tree/main/sdk/go',
    actionLabel: 'Review Go SDK',
    evidence: input.apiKeys
      ? `module ${available(artifacts.goSdkModule)} / e2e ${available(
          artifacts.goSdkE2e,
        )} / example ${available(artifacts.goExample)} / ${active.length} active keys`
      : 'Go SDK adoption evidence is missing',
    guardrail:
      'The Go SDK path needs a module consumer surface, live e2e verification, an installable example, and at least one active key for local verification.',
    key: 'go_sdk',
    owner: 'Developer Platform',
    signal: input.apiKeys
      ? `${artifacts.goExample ? 1 : 0} Go examples / e2e ${onOff(
          artifacts.goSdkE2e,
        )} / ${active.length} active keys`
      : 'Go SDK evidence missing',
    status: goSdkStatus(input, artifacts),
    title: 'Go SDK adoption path',
  }
}

function exampleSandboxLane(
  input: DeveloperApiAdoptionKitInput,
  artifacts: DeveloperApiAdoptionArtifacts,
): DeveloperApiAdoptionLane {
  const active = activeApiKeys(input.apiKeys)
  const serviceAccounts = activeServiceAccounts(input.serviceAccounts)
  const ingestOnlyPreset =
    input.scopePresets?.some((preset) => preset.id === 'ingest_only') ?? false

  return {
    actionHref:
      'https://github.com/Phixsura/attune/blob/main/docs/testing.md#developer-parity-loop--demo-workspace',
    actionLabel: 'Open sandbox guide',
    evidence:
      input.apiKeys && input.serviceAccounts && input.scopePresets
        ? `demo bootstrap ${available(artifacts.demoBootstrap)} / testing guide ${available(
            artifacts.demoTestingGuide,
          )} / ${serviceAccounts.length} service accounts / ingest preset ${available(
            ingestOnlyPreset,
          )}`
        : 'sandbox adoption evidence is missing',
    guardrail:
      'A fresh developer should have deterministic demo bootstrap, reset documentation, example apps, and least-privilege presets before judging product value.',
    key: 'example_sandbox',
    owner: 'Developer Experience',
    signal:
      input.apiKeys && input.serviceAccounts && input.scopePresets
        ? `${active.length} active keys / ${serviceAccounts.length} service accounts / ${
            input.scopePresets.length
          } presets / demo bootstrap ${onOff(artifacts.demoBootstrap)}`
        : 'sandbox evidence missing',
    status: exampleSandboxStatus(input, artifacts),
    title: 'Example app and sandbox path',
  }
}

function webhookReplayLane(
  input: DeveloperApiAdoptionKitInput,
  artifacts: DeveloperApiAdoptionArtifacts,
): DeveloperApiAdoptionLane {
  const active = activeApiKeys(input.apiKeys)

  return {
    actionHref:
      'https://github.com/Phixsura/attune/tree/main/docs/proposals/2026/07/assets-zapier-e2e',
    actionLabel: 'Review replay assets',
    evidence: input.apiKeys
      ? `${artifacts.replayFixtureCount} replay fixtures / replay smoke ${available(
          artifacts.replaySmoke,
        )} / browser ingest ${available(artifacts.browserIngestExample)} / ${active.length} active keys`
      : 'webhook replay evidence is missing',
    guardrail:
      'Webhook adoption needs replayable payload evidence, browser-safe ingest examples, smoke coverage, and a working key before external integrators debug failures.',
    key: 'webhook_replay',
    owner: 'Developer Platform + Integrations',
    signal: input.apiKeys
      ? `${artifacts.replayFixtureCount} replay fixtures / replay smoke ${onOff(
          artifacts.replaySmoke,
        )} / browser ingest ${onOff(artifacts.browserIngestExample)}`
      : 'webhook replay evidence missing',
    status: webhookReplayStatus(input, artifacts),
    title: 'Webhook replay playground',
  }
}

function openApiContractStatus(
  input: DeveloperApiAdoptionKitInput,
  artifacts: DeveloperApiAdoptionArtifacts,
): DeveloperApiAdoptionStatus {
  if (!input.scopes || !input.scopePresets || !input.apiKeys) return 'needs_data'
  if (!artifacts.openApiContract || input.scopes.length === 0 || input.scopePresets.length === 0) {
    return 'blocked'
  }
  if (!artifacts.openApiGuide || activeApiKeys(input.apiKeys).length === 0) return 'watch'
  return 'ready'
}

function nodeSdkStatus(
  input: DeveloperApiAdoptionKitInput,
  artifacts: DeveloperApiAdoptionArtifacts,
): DeveloperApiAdoptionStatus {
  if (!input.apiKeys || !input.serviceAccounts) return 'needs_data'
  if (!artifacts.nodeSdkPackage || !artifacts.nodeSdkE2e) return 'blocked'
  if (
    !artifacts.nodeExample ||
    !artifacts.nodeSdkBrowserSmoke ||
    activeAutomationIdentities(input.apiKeys, input.serviceAccounts) === 0
  ) {
    return 'watch'
  }
  return 'ready'
}

function goSdkStatus(
  input: DeveloperApiAdoptionKitInput,
  artifacts: DeveloperApiAdoptionArtifacts,
): DeveloperApiAdoptionStatus {
  if (!input.apiKeys) return 'needs_data'
  if (!artifacts.goSdkModule || !artifacts.goSdkE2e) return 'blocked'
  if (!artifacts.goExample || activeApiKeys(input.apiKeys).length === 0) return 'watch'
  return 'ready'
}

function exampleSandboxStatus(
  input: DeveloperApiAdoptionKitInput,
  artifacts: DeveloperApiAdoptionArtifacts,
): DeveloperApiAdoptionStatus {
  if (!input.apiKeys || !input.serviceAccounts || !input.scopePresets) return 'needs_data'
  const ingestOnlyPreset = input.scopePresets.some((preset) => preset.id === 'ingest_only')
  if (!artifacts.demoBootstrap || !artifacts.demoTestingGuide) return 'blocked'
  if (
    !ingestOnlyPreset ||
    activeApiKeys(input.apiKeys).length === 0 ||
    activeServiceAccounts(input.serviceAccounts).length === 0
  ) {
    return 'watch'
  }
  return 'ready'
}

function webhookReplayStatus(
  input: DeveloperApiAdoptionKitInput,
  artifacts: DeveloperApiAdoptionArtifacts,
): DeveloperApiAdoptionStatus {
  if (!input.apiKeys) return 'needs_data'
  if (artifacts.replayFixtureCount === 0 || !artifacts.replaySmoke) return 'blocked'
  if (!artifacts.browserIngestExample || activeApiKeys(input.apiKeys).length === 0) return 'watch'
  return 'ready'
}

function developerApiAdoptionSummary(totals: DeveloperApiAdoptionKit['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} developer adoption lanes are blocked`
  if (totals.needs_data > 0) return `${totals.needs_data} developer adoption lanes need evidence`
  if (totals.watch > 0) return `${totals.watch} developer adoption lanes need hardening`
  return 'developer API adoption kit evidence is ready'
}

function activeApiKeys(keys: ApiKey[] | undefined): ApiKey[] {
  return (keys ?? []).filter((key) => key.isActive)
}

function activeServiceAccounts(accounts: ServiceAccount[] | undefined): ServiceAccount[] {
  return (accounts ?? []).filter((account) => account.isActive)
}

function activeAutomationIdentities(
  keys: ApiKey[] | undefined,
  accounts: ServiceAccount[] | undefined,
): number {
  const activeAccountIds = new Set(activeServiceAccounts(accounts).map((account) => account.id))
  return activeApiKeys(keys).filter(
    (key) => key.serviceAccountId && activeAccountIds.has(key.serviceAccountId),
  ).length
}

const artifactCount = 14

function verifiedArtifactCount(artifacts: DeveloperApiAdoptionArtifacts): number {
  return [
    artifacts.browserIngestExample,
    artifacts.demoBootstrap,
    artifacts.demoTestingGuide,
    artifacts.goExample,
    artifacts.goSdkE2e,
    artifacts.goSdkModule,
    artifacts.nodeExample,
    artifacts.nodeSdkBrowserSmoke,
    artifacts.nodeSdkE2e,
    artifacts.nodeSdkPackage,
    artifacts.openApiContract,
    artifacts.openApiGuide,
    artifacts.replayFixtureCount > 0,
    artifacts.replaySmoke,
  ].filter(Boolean).length
}

function available(value: boolean): string {
  return value ? 'available' : 'missing'
}

function onOff(value: boolean): string {
  return value ? 'on' : 'off'
}
