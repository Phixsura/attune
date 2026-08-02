import type {
  ExternalConnection,
  ExternalObjectMapping,
  ExternalObjectSchema,
  ExternalSyncEvent,
  ExternalSyncHealthResponse,
  ExternalSyncRun,
} from '@/features/external-sync/api/external-sync'

export type ConnectorConformanceStatus = 'verified' | 'watch' | 'blocked' | 'needs_data'

export type ConnectorConformanceLaneKey =
  | 'connector_manifest'
  | 'fixture_replay'
  | 'webhook_signature'
  | 'field_mapping'
  | 'error_recovery'

export type ConnectorConformanceLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: ConnectorConformanceLaneKey
  owner: string
  signal: string
  status: ConnectorConformanceStatus
  title: string
}

export type ConnectorConformanceGate = {
  fingerprint: string
  lanes: ConnectorConformanceLane[]
  summary: string
  totals: Record<ConnectorConformanceStatus, number> & {
    total: number
  }
}

export type ConnectorConformanceArtifacts = {
  conformanceVerifier: boolean
  connectorSdk: boolean
  expectedFixtures: number
  expectedProviders: number
  expectedRequiredHooks: number
  fieldMappingContract: boolean
  fixtureReplaySuite: boolean
  fixtures: number
  manifest: boolean
  providers: number
  recoveryMatrix: boolean
  requiredHooks: number
  requiredSchemaFields: string[]
  signatureVerifier: boolean
}

export type ConnectorConformanceGateInput = {
  artifacts?: ConnectorConformanceArtifacts
  connections?: ExternalConnection[]
  events?: ExternalSyncEvent[]
  health?: ExternalSyncHealthResponse
  mappings?: ExternalObjectMapping[]
  runs?: ExternalSyncRun[]
  schemas?: ExternalObjectSchema[]
}

export const defaultConnectorConformanceArtifacts: ConnectorConformanceArtifacts = {
  conformanceVerifier: true,
  connectorSdk: true,
  expectedFixtures: 3,
  expectedProviders: 1,
  expectedRequiredHooks: 6,
  fieldMappingContract: true,
  fixtureReplaySuite: true,
  fixtures: 3,
  manifest: true,
  providers: 1,
  recoveryMatrix: true,
  requiredHooks: 6,
  requiredSchemaFields: ['number', 'title', 'state', 'labels', 'updated_at'],
  signatureVerifier: true,
}

export function buildConnectorConformanceGate(
  input: ConnectorConformanceGateInput,
): ConnectorConformanceGate {
  const artifacts = input.artifacts ?? defaultConnectorConformanceArtifacts
  const lanes = [
    connectorManifestLane(input, artifacts),
    fixtureReplayLane(input, artifacts),
    webhookSignatureLane(input, artifacts),
    fieldMappingLane(input, artifacts),
    errorRecoveryLane(input, artifacts),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    total: lanes.length,
    verified: lanes.filter((lane) => lane.status === 'verified').length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${artifacts.providers}/${artifacts.expectedProviders} providers / ${artifacts.fixtures}/${artifacts.expectedFixtures} fixtures / ${artifacts.requiredHooks}/${artifacts.expectedRequiredHooks} hooks / ${activeConnections(input.connections).length} live connectors / ${verifiedSignatureEvents(input.events).length} verified signatures`,
    lanes,
    summary: connectorConformanceSummary(totals),
    totals,
  }
}

function connectorManifestLane(
  input: ConnectorConformanceGateInput,
  artifacts: ConnectorConformanceArtifacts,
): ConnectorConformanceLane {
  return {
    actionHref: 'https://github.com/Phixsura/attune/tree/main/integrations/connector-conformance',
    actionLabel: 'Review connector manifest',
    evidence: `SDK ${available(artifacts.connectorSdk)} / manifest ${available(
      artifacts.manifest,
    )} / ${artifacts.providers}/${artifacts.expectedProviders} providers / ${artifacts.requiredHooks}/${artifacts.expectedRequiredHooks} hooks`,
    guardrail:
      'Every connector must declare install metadata, credential modes, webhook events, and required lifecycle hooks before it can be listed as platform-ready.',
    key: 'connector_manifest',
    owner: 'Developer Platform + Integrations',
    signal: `${activeConnections(input.connections).length} active live connectors / ${configuredWebhookSecrets(
      input.connections,
    )} webhook secrets configured`,
    status: connectorManifestStatus(artifacts),
    title: 'Connector manifest and SDK contract',
  }
}

function fixtureReplayLane(
  input: ConnectorConformanceGateInput,
  artifacts: ConnectorConformanceArtifacts,
): ConnectorConformanceLane {
  const replayed = replayedEvents(input.events).length
  return {
    actionHref:
      'https://github.com/Phixsura/attune/tree/main/integrations/connector-conformance/fixtures',
    actionLabel: 'Review replay fixtures',
    evidence: `fixture suite ${available(artifacts.fixtureReplaySuite)} / verifier ${available(
      artifacts.conformanceVerifier,
    )} / ${artifacts.fixtures}/${artifacts.expectedFixtures} signed fixtures`,
    guardrail:
      'Provider payloads must replay deterministically into normalized Attune records with stable dedupe keys, titles, statuses, labels, and external keys.',
    key: 'fixture_replay',
    owner: 'Developer Platform + QA',
    signal: `${(input.events ?? []).length} received events / ${replayed} replayed in tenant ledger`,
    status: fixtureReplayStatus(artifacts),
    title: 'Fixture replay normalization',
  }
}

function webhookSignatureLane(
  input: ConnectorConformanceGateInput,
  artifacts: ConnectorConformanceArtifacts,
): ConnectorConformanceLane {
  const verified = verifiedSignatureEvents(input.events).length
  const failed = failedSignatureEvents(input.events).length
  return {
    actionHref:
      'https://github.com/Phixsura/attune/blob/main/scripts/check-connector-conformance.mjs',
    actionLabel: 'Run signature gate',
    evidence: `signature verifier ${available(
      artifacts.signatureVerifier,
    )} / ${configuredWebhookSecrets(input.connections)} webhook secrets / ${verified} verified events / ${failed} failed signatures`,
    guardrail:
      'Webhook deliveries must prove HMAC verification before replay and keep failed signatures visible as unreplayable evidence.',
    key: 'webhook_signature',
    owner: 'Security + Integrations',
    signal: `${verified} verified / ${failed} failed / ${configuredWebhookSecrets(
      input.connections,
    )} configured secrets`,
    status: webhookSignatureStatus(input, artifacts),
    title: 'Webhook signature evidence',
  }
}

function fieldMappingLane(
  input: ConnectorConformanceGateInput,
  artifacts: ConnectorConformanceArtifacts,
): ConnectorConformanceLane {
  const enabledMappings = enabledObjectMappings(input.mappings)
  const mappingProblems = liveMappingProblems(input, artifacts)
  return {
    actionHref: 'https://github.com/Phixsura/attune/blob/main/docs/external-sync-adapters.md',
    actionLabel: 'Review mapping contract',
    evidence: `mapping contract ${available(
      artifacts.fieldMappingContract,
    )} / ${enabledMappings.length} enabled mappings / ${(input.schemas ?? []).length} provider schemas / ${mappingProblems.length} schema problems`,
    guardrail:
      'Field mapping must map enabled Attune objects to provider schema fields, catch schema drift, and remain previewable before backfill or push.',
    key: 'field_mapping',
    owner: 'Integrations + Console',
    signal: `${mappedFieldCount(enabledMappings)} mapped fields / ${schemaFieldCount(
      input.schemas,
    )} provider fields / ${mappingProblems.length} problems`,
    status: fieldMappingStatus(input, artifacts, mappingProblems),
    title: 'Field mapping contract',
  }
}

function errorRecoveryLane(
  input: ConnectorConformanceGateInput,
  artifacts: ConnectorConformanceArtifacts,
): ConnectorConformanceLane {
  const retryable =
    (input.health?.retryableRuns ?? 0) +
    (input.health?.delayedRetryRuns ?? 0) +
    retryableRuns(input.runs).length
  const quarantined =
    input.health?.quarantinedConnections ?? quarantinedConnections(input.connections).length
  return {
    actionHref: 'https://github.com/Phixsura/attune/tree/main/integrations/connector-conformance',
    actionLabel: 'Review recovery matrix',
    evidence: `recovery matrix ${available(artifacts.recoveryMatrix)} / ${retryable} retryable signals / ${quarantined} quarantined connectors / ${
      input.health?.providerUnavailableRuns ?? providerUnavailableRuns(input.runs)
    } provider outages`,
    guardrail:
      'Rate limits, unauthorized credentials, validation failures, and provider outages must route to deterministic retry, reauthorize, dead-letter, or manual recovery paths.',
    key: 'error_recovery',
    owner: 'Reliability + Integrations',
    signal: `${retryable} retryable / ${quarantined} quarantined / ${
      input.health?.unauthorizedRuns ?? unauthorizedRuns(input.runs)
    } unauthorized / ${input.health?.throttledRuns ?? throttledRuns(input.runs)} throttled`,
    status: errorRecoveryStatus(input, artifacts),
    title: 'Error recovery conformance',
  }
}

function connectorManifestStatus(
  artifacts: ConnectorConformanceArtifacts,
): ConnectorConformanceStatus {
  if (!artifacts.connectorSdk || !artifacts.manifest) return 'blocked'
  if (
    artifacts.providers < artifacts.expectedProviders ||
    artifacts.requiredHooks < artifacts.expectedRequiredHooks
  ) {
    return 'blocked'
  }
  return 'verified'
}

function fixtureReplayStatus(artifacts: ConnectorConformanceArtifacts): ConnectorConformanceStatus {
  if (!artifacts.conformanceVerifier || !artifacts.fixtureReplaySuite) return 'blocked'
  if (artifacts.fixtures < artifacts.expectedFixtures) return 'blocked'
  return 'verified'
}

function webhookSignatureStatus(
  input: ConnectorConformanceGateInput,
  artifacts: ConnectorConformanceArtifacts,
): ConnectorConformanceStatus {
  if (!artifacts.signatureVerifier) return 'blocked'
  if (!input.connections || input.connections.length === 0) return 'needs_data'
  if (failedSignatureEvents(input.events).length > 0) return 'blocked'
  if (configuredWebhookSecrets(input.connections) === 0 || !input.events) return 'watch'
  if (verifiedSignatureEvents(input.events).length === 0) return 'watch'
  return 'verified'
}

function fieldMappingStatus(
  input: ConnectorConformanceGateInput,
  artifacts: ConnectorConformanceArtifacts,
  mappingProblems: string[],
): ConnectorConformanceStatus {
  if (!artifacts.fieldMappingContract) return 'blocked'
  if (
    !input.mappings ||
    input.mappings.length === 0 ||
    !input.schemas ||
    input.schemas.length === 0
  ) {
    return 'needs_data'
  }
  if (mappingProblems.length > 0) return 'blocked'
  if (enabledObjectMappings(input.mappings).length === 0 || mappedFieldCount(input.mappings) < 2) {
    return 'watch'
  }
  return 'verified'
}

function errorRecoveryStatus(
  input: ConnectorConformanceGateInput,
  artifacts: ConnectorConformanceArtifacts,
): ConnectorConformanceStatus {
  if (!artifacts.recoveryMatrix) return 'blocked'
  if (!input.health && !input.runs && !input.connections) return 'needs_data'
  /* v8 ignore next -- @preserve: partial live-health payloads normalize missing counters to zero. */
  if ((input.health?.deadRuns ?? 0) > 0 && (input.health?.retryableRuns ?? 0) === 0) return 'watch'
  return 'verified'
}

function connectorConformanceSummary(totals: ConnectorConformanceGate['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} connector conformance lanes are blocked`
  if (totals.needs_data > 0)
    return `${totals.needs_data} connector conformance lanes need live tenant evidence`
  if (totals.watch > 0) return `${totals.watch} connector conformance lanes need hardening`
  return 'connector conformance is verified'
}

function activeConnections(connections: ExternalConnection[] | undefined): ExternalConnection[] {
  return (connections ?? []).filter(
    (connection) => connection.enabled && connection.status !== 'deleted',
  )
}

function quarantinedConnections(
  connections: ExternalConnection[] | undefined,
): ExternalConnection[] {
  return (connections ?? []).filter((connection) => connection.status === 'quarantined')
}

function configuredWebhookSecrets(connections: ExternalConnection[] | undefined): number {
  return (connections ?? []).filter((connection) => connection.webhookSecretConfigured).length
}

function enabledObjectMappings(
  mappings: ExternalObjectMapping[] | undefined,
): ExternalObjectMapping[] {
  return (mappings ?? []).filter((mapping) => mapping.enabled)
}

function verifiedSignatureEvents(events: ExternalSyncEvent[] | undefined): ExternalSyncEvent[] {
  return (events ?? []).filter((event) => signatureStatus(event) === 'verified')
}

function failedSignatureEvents(events: ExternalSyncEvent[] | undefined): ExternalSyncEvent[] {
  return (events ?? []).filter((event) => {
    const status = signatureStatus(event)
    return status === 'failed' || status === 'invalid' || status === 'mismatch'
  })
}

function replayedEvents(events: ExternalSyncEvent[] | undefined): ExternalSyncEvent[] {
  return (events ?? []).filter((event) => Boolean(event.replayedAt || event.runId))
}

function signatureStatus(event: ExternalSyncEvent): string {
  return String(event.signatureStatus)
    .replace('EXTERNAL_SYNC_EVENT_SIGNATURE_STATUS_', '')
    .toLowerCase()
}

function liveMappingProblems(
  input: ConnectorConformanceGateInput,
  artifacts: ConnectorConformanceArtifacts,
): string[] {
  const schemaFields = new Set((input.schemas ?? []).flatMap((schema) => schema.fields))
  const problems: string[] = []
  for (const field of artifacts.requiredSchemaFields) {
    if (schemaFields.size > 0 && !schemaFields.has(field)) {
      problems.push(`schema:${field}`)
    }
  }
  for (const mapping of enabledObjectMappings(input.mappings)) {
    const parsed = parseMapping(mapping.fieldMappingJson)
    if (!parsed) {
      problems.push(`mapping:${mapping.id}:invalid-json`)
      continue
    }
    for (const externalField of mappingFieldCandidates(parsed)) {
      if (schemaFields.size > 0 && !schemaFields.has(externalField)) {
        problems.push(`mapping:${mapping.id}:${externalField}`)
      }
    }
  }
  return problems
}

function parseMapping(raw: string): Record<string, unknown> | null {
  try {
    const parsed: unknown = JSON.parse(raw || '{}')
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return null
    return parsed as Record<string, unknown>
  } catch {
    return null
  }
}

function mappingFieldCandidates(mapping: Record<string, unknown>): string[] {
  const candidates = new Set<string>()
  for (const value of Object.values(mapping)) {
    collectFieldCandidate(candidates, value)
  }
  return Array.from(candidates)
}

function collectFieldCandidate(candidates: Set<string>, value: unknown) {
  if (typeof value === 'string' && /^[A-Za-z_][A-Za-z0-9_.-]*$/.test(value)) {
    candidates.add(value)
    return
  }
  if (Array.isArray(value)) {
    for (const item of value) {
      collectFieldCandidate(candidates, item)
    }
    return
  }
  if (typeof value === 'object' && value !== null) {
    for (const item of Object.values(value)) {
      collectFieldCandidate(candidates, item)
    }
  }
}

function mappedFieldCount(mappings: ExternalObjectMapping[] | undefined): number {
  return enabledObjectMappings(mappings).reduce((sum, mapping) => {
    const parsed = parseMapping(mapping.fieldMappingJson)
    return sum + (parsed ? mappingFieldCandidates(parsed).length : 0)
  }, 0)
}

function schemaFieldCount(schemas: ExternalObjectSchema[] | undefined): number {
  return (schemas ?? []).reduce((sum, schema) => sum + schema.fields.length, 0)
}

function retryableRuns(runs: ExternalSyncRun[] | undefined): ExternalSyncRun[] {
  return (runs ?? []).filter((run) => Boolean(run.nextRetryAt))
}

function providerUnavailableRuns(runs: ExternalSyncRun[] | undefined): number {
  return (runs ?? []).filter((run) => run.errorKind === 'provider_unavailable').length
}

function unauthorizedRuns(runs: ExternalSyncRun[] | undefined): number {
  return (runs ?? []).filter((run) => run.errorKind === 'unauthorized').length
}

function throttledRuns(runs: ExternalSyncRun[] | undefined): number {
  return (runs ?? []).filter((run) => run.errorKind === 'provider_throttled').length
}

function available(value: boolean): string {
  return value ? 'available' : 'missing'
}
