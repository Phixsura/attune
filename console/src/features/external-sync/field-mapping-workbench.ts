import {
  type ExternalConnection,
  type ExternalObjectMapping,
  type ExternalObjectSchema,
  ExternalSyncDirection,
  type ExternalSyncHealthResponse,
  type ExternalSyncRun,
} from '@/features/external-sync/api/external-sync'

export type FieldMappingWorkbenchStatus = 'verified' | 'watch' | 'blocked' | 'needs_data'

export type FieldMappingWorkbenchLaneKey =
  | 'schema_diff'
  | 'required_mapping'
  | 'status_mapping'
  | 'preview_backfill'
  | 'rollback_recovery'

export type FieldMappingWorkbenchLane = {
  actionLabel: string
  detail: string
  evidence: string
  key: FieldMappingWorkbenchLaneKey
  owner: string
  signal: string
  status: FieldMappingWorkbenchStatus
  title: string
}

export type FieldMappingWorkbenchRowStatus = 'mapped' | 'suggested' | 'missing' | 'drift'

export type FieldMappingWorkbenchRow = {
  evidence: string
  localField: string
  providerField: string
  required: boolean
  status: FieldMappingWorkbenchRowStatus
  suggestion: string
}

export type FieldMappingWorkbench = {
  fingerprint: string
  lanes: FieldMappingWorkbenchLane[]
  mappingRows: FieldMappingWorkbenchRow[]
  summary: string
  totals: Record<FieldMappingWorkbenchStatus, number> & {
    total: number
  }
}

export type FieldMappingWorkbenchArtifacts = {
  backfillImpactPreview: boolean
  conflictPolicyControl: boolean
  recommendedLocalFields: string[]
  requiredLocalFields: string[]
  requiredProviderFields: string[]
  requiredStatusValues: string[]
  resetCursorRecovery: boolean
  samplePreview: boolean
  schemaDiffDetector: boolean
  suggestions: Record<string, string>
  tombstonePolicyControl: boolean
}

export type FieldMappingWorkbenchInput = {
  artifacts?: FieldMappingWorkbenchArtifacts
  connection?: ExternalConnection | null
  health?: ExternalSyncHealthResponse
  mapping?: ExternalObjectMapping | null
  mappings?: ExternalObjectMapping[]
  runs?: ExternalSyncRun[]
  schemas?: ExternalObjectSchema[]
}

export const defaultFieldMappingWorkbenchArtifacts: FieldMappingWorkbenchArtifacts = {
  backfillImpactPreview: true,
  conflictPolicyControl: true,
  recommendedLocalFields: ['description', 'tags', 'external_key'],
  requiredLocalFields: ['title', 'status'],
  requiredProviderFields: ['title', 'state'],
  requiredStatusValues: ['open', 'done'],
  resetCursorRecovery: true,
  samplePreview: true,
  schemaDiffDetector: true,
  suggestions: {
    description: 'body',
    external_key: 'number',
    source_url: 'html_url',
    status: 'state',
    tags: 'labels',
    title: 'title',
    updated_at: 'updated_at',
  },
  tombstonePolicyControl: true,
}

export function buildFieldMappingWorkbench(
  input: FieldMappingWorkbenchInput,
): FieldMappingWorkbench {
  const artifacts = input.artifacts ?? defaultFieldMappingWorkbenchArtifacts
  const mapping = input.mapping ?? input.mappings?.[0] ?? null
  const schema = schemaForMapping(mapping, input.schemas)
  const parsedFieldMapping = parseJSONRecord(mapping?.fieldMappingJson ?? '')
  const parsedStatusMapping = parseJSONRecord(mapping?.statusMappingJson ?? '')
  const rows = fieldMappingRows(parsedFieldMapping.value, schema, artifacts)
  const schemaProblems = schemaDiffProblems(schema, artifacts)
  const driftRows = rows.filter((row) => row.status === 'drift')
  const missingRequiredRows = rows.filter((row) => row.required && row.status !== 'mapped')
  const lanes = [
    schemaDiffLane(input, schema, schemaProblems, artifacts),
    requiredMappingLane(mapping, parsedFieldMapping, rows, artifacts),
    statusMappingLane(mapping, parsedStatusMapping, artifacts),
    previewBackfillLane(mapping, parsedFieldMapping, schema, rows, artifacts),
    rollbackRecoveryLane(input, mapping, artifacts),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    total: lanes.length,
    verified: lanes.filter((lane) => lane.status === 'verified').length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${input.connection?.name ?? 'No connection'} / ${
      mapping?.id ?? 'no mapping'
    } / ${artifacts.requiredLocalFields.length - missingRequiredRows.length}/${
      artifacts.requiredLocalFields.length
    } required fields / ${schema?.fields.length ?? 0} provider fields / ${
      schemaProblems.length + driftRows.length
    } drift risks`,
    lanes,
    mappingRows: rows,
    summary: fieldMappingWorkbenchSummary(totals),
    totals,
  }
}

function schemaDiffLane(
  input: FieldMappingWorkbenchInput,
  schema: ExternalObjectSchema | null,
  schemaProblems: string[],
  artifacts: FieldMappingWorkbenchArtifacts,
): FieldMappingWorkbenchLane {
  return {
    actionLabel: 'Review schema diff',
    detail:
      'Provider schema discovery must surface required and writable fields before operators can save or backfill mappings.',
    evidence: `schema diff ${available(artifacts.schemaDiffDetector)} / ${schema?.fields.length ?? 0} fields / ${schema?.writableFields.length ?? 0} writable / ${schemaProblems.length} required missing`,
    key: 'schema_diff',
    owner: 'Integrations',
    signal: `${input.connection?.provider ?? 'no provider'} / ${schema?.type ?? 'no schema'} / ${(input.schemas ?? []).length} discovered schemas`,
    status: schemaDiffStatus(schema, schemaProblems, artifacts),
    title: 'Provider schema diff',
  }
}

function requiredMappingLane(
  mapping: ExternalObjectMapping | null,
  parsed: ParsedJSONRecord,
  rows: FieldMappingWorkbenchRow[],
  artifacts: FieldMappingWorkbenchArtifacts,
): FieldMappingWorkbenchLane {
  const mappedRequired = rows.filter((row) => row.required && row.status === 'mapped').length
  const suggestedRequired = rows.filter((row) => row.required && row.status === 'suggested').length
  const drifted = rows.filter((row) => row.status === 'drift').length
  return {
    actionLabel: 'Resolve required mappings',
    detail:
      'Required Attune fields should map to provider fields explicitly, with suggestions visible but not counted as saved coverage.',
    evidence: `${mappedRequired}/${artifacts.requiredLocalFields.length} required mapped / ${suggestedRequired} suggested / ${drifted} drifted / JSON ${parsed.error ? 'invalid' : 'valid'}`,
    key: 'required_mapping',
    owner: 'Console + Product Ops',
    signal: `${mappedFieldCount(parsed.value)} saved fields / ${rows.length} tracked fields / mapping ${mapping?.mappingVersion ?? 0}`,
    status: requiredMappingStatus(mapping, parsed, rows),
    title: 'Required field coverage',
  }
}

function statusMappingLane(
  mapping: ExternalObjectMapping | null,
  parsed: ParsedJSONRecord,
  artifacts: FieldMappingWorkbenchArtifacts,
): FieldMappingWorkbenchLane {
  const mappedStatuses = artifacts.requiredStatusValues.filter((status) =>
    Object.hasOwn(parsed.value ?? {}, status),
  )
  return {
    actionLabel: 'Review status mapping',
    detail:
      'Local lifecycle states need explicit provider-state mappings so shipped, cancelled, and reopened records do not drift silently.',
    evidence: `${mappedStatuses.length}/${artifacts.requiredStatusValues.length} required statuses / JSON ${parsed.error ? 'invalid' : 'valid'} / conflict ${mapping?.conflictPolicy || 'unset'}`,
    key: 'status_mapping',
    owner: 'Product Ops + Integrations',
    signal: `statuses ${mappedStatuses.join(', ') || 'none'} / tombstone ${
      mapping?.tombstonePolicy || 'unset'
    }`,
    status: statusMappingStatus(mapping, parsed, artifacts),
    title: 'Status and lifecycle mapping',
  }
}

function previewBackfillLane(
  mapping: ExternalObjectMapping | null,
  parsed: ParsedJSONRecord,
  schema: ExternalObjectSchema | null,
  rows: FieldMappingWorkbenchRow[],
  artifacts: FieldMappingWorkbenchArtifacts,
): FieldMappingWorkbenchLane {
  const canBackfill = mapping ? mappingAllowsPull(mapping.direction) : false
  return {
    actionLabel: 'Run preview before backfill',
    detail:
      'Operators need sample preview, reset-cursor, and backfill impact controls before changing a live connector mapping.',
    evidence: `preview ${available(artifacts.samplePreview)} / impact ${available(
      artifacts.backfillImpactPreview,
    )} / reset ${available(artifacts.resetCursorRecovery)} / backfill ${
      canBackfill ? 'available' : 'not available'
    }`,
    key: 'preview_backfill',
    owner: 'Console + Reliability',
    signal: `${schema?.type ?? 'no schema'} / ${rows.filter((row) => row.status === 'mapped').length} mapped fields / enabled ${mapping?.enabled ? 'yes' : 'no'}`,
    status: previewBackfillStatus(mapping, parsed, schema, rows, artifacts),
    title: 'Preview and backfill safety',
  }
}

function rollbackRecoveryLane(
  input: FieldMappingWorkbenchInput,
  mapping: ExternalObjectMapping | null,
  artifacts: FieldMappingWorkbenchArtifacts,
): FieldMappingWorkbenchLane {
  const failedRecords = (input.runs ?? []).reduce((sum, run) => sum + run.recordsFailed, 0)
  const conflicts =
    input.health?.openConflicts ??
    (input.runs ?? []).reduce((sum, run) => sum + run.conflictsCreated, 0)
  return {
    actionLabel: 'Review rollback path',
    detail:
      'Every live mapping change needs conflict policy, tombstone behavior, reset cursor, and failure recovery evidence.',
    evidence: `conflict ${mapping?.conflictPolicy || 'unset'} / tombstone ${
      mapping?.tombstonePolicy || 'unset'
    } / reset ${available(artifacts.resetCursorRecovery)} / ${failedRecords} failed records / ${conflicts} conflicts`,
    key: 'rollback_recovery',
    owner: 'Reliability + Integrations',
    signal: `mapping v${mapping?.mappingVersion ?? 0} / ${failedRecords} failed / ${conflicts} conflicts`,
    status: rollbackRecoveryStatus(mapping, failedRecords, conflicts, artifacts),
    title: 'Rollback and recovery path',
  }
}

function schemaDiffStatus(
  schema: ExternalObjectSchema | null,
  schemaProblems: string[],
  artifacts: FieldMappingWorkbenchArtifacts,
): FieldMappingWorkbenchStatus {
  if (!artifacts.schemaDiffDetector) return 'blocked'
  if (!schema) return 'needs_data'
  if (schemaProblems.length > 0) return 'blocked'
  return 'verified'
}

function requiredMappingStatus(
  mapping: ExternalObjectMapping | null,
  parsed: ParsedJSONRecord,
  rows: FieldMappingWorkbenchRow[],
): FieldMappingWorkbenchStatus {
  if (!mapping) return 'needs_data'
  if (parsed.error) return 'blocked'
  if (rows.some((row) => row.required && row.status !== 'mapped')) return 'blocked'
  if (rows.some((row) => row.status === 'drift')) return 'blocked'
  return 'verified'
}

function statusMappingStatus(
  mapping: ExternalObjectMapping | null,
  parsed: ParsedJSONRecord,
  artifacts: FieldMappingWorkbenchArtifacts,
): FieldMappingWorkbenchStatus {
  if (!mapping) return 'needs_data'
  if (parsed.error) return 'blocked'
  const mappedStatuses = artifacts.requiredStatusValues.filter((status) =>
    /* v8 ignore next -- @preserve: parsed non-error records carry an object value; empty fallback guards malformed fixtures. */
    Object.hasOwn(parsed.value ?? {}, status),
  )
  if (mappedStatuses.length < artifacts.requiredStatusValues.length) return 'blocked'
  return 'verified'
}

function previewBackfillStatus(
  mapping: ExternalObjectMapping | null,
  parsed: ParsedJSONRecord,
  schema: ExternalObjectSchema | null,
  rows: FieldMappingWorkbenchRow[],
  artifacts: FieldMappingWorkbenchArtifacts,
): FieldMappingWorkbenchStatus {
  if (
    !artifacts.samplePreview ||
    !artifacts.backfillImpactPreview ||
    !artifacts.resetCursorRecovery
  ) {
    return 'blocked'
  }
  if (!mapping || !schema) return 'needs_data'
  if (parsed.error || rows.some((row) => row.required && row.status !== 'mapped')) return 'blocked'
  if (!mapping.enabled || !mappingAllowsPull(mapping.direction)) return 'watch'
  return 'verified'
}

function rollbackRecoveryStatus(
  mapping: ExternalObjectMapping | null,
  failedRecords: number,
  conflicts: number,
  artifacts: FieldMappingWorkbenchArtifacts,
): FieldMappingWorkbenchStatus {
  if (!artifacts.conflictPolicyControl || !artifacts.tombstonePolicyControl) return 'blocked'
  if (!mapping) return 'needs_data'
  if (!mapping.conflictPolicy || !mapping.tombstonePolicy) return 'blocked'
  if (failedRecords > 0 || conflicts > 0) return 'watch'
  return 'verified'
}

function schemaForMapping(
  mapping: ExternalObjectMapping | null,
  schemas: ExternalObjectSchema[] | undefined,
): ExternalObjectSchema | null {
  if (!mapping) return schemas?.[0] ?? null
  return (
    schemas?.find((schema) => schema.type === mapping.externalObjectType) ?? schemas?.[0] ?? null
  )
}

function fieldMappingRows(
  mapping: Record<string, unknown> | null,
  schema: ExternalObjectSchema | null,
  artifacts: FieldMappingWorkbenchArtifacts,
): FieldMappingWorkbenchRow[] {
  const schemaFields = new Set(schema?.fields ?? [])
  const fields = unique([...artifacts.requiredLocalFields, ...artifacts.recommendedLocalFields])
  return fields.map((localField) => {
    const required = artifacts.requiredLocalFields.includes(localField)
    const mapped = stringValue(mapping?.[localField])
    const suggestion = artifacts.suggestions[localField] ?? ''
    const providerField = mapped || suggestion
    const providerAvailable = Boolean(providerField && schemaFields.has(providerField))
    const status = fieldRowStatus({ mapped, providerAvailable, required, schema, suggestion })
    return {
      evidence: rowEvidence({ mapped, providerAvailable, required, schema, suggestion }),
      localField,
      providerField: providerField || 'unmapped',
      required,
      status,
      suggestion: suggestion || 'none',
    }
  })
}

function fieldRowStatus({
  mapped,
  providerAvailable,
  required,
  schema,
  suggestion,
}: {
  mapped: string
  providerAvailable: boolean
  required: boolean
  schema: ExternalObjectSchema | null
  suggestion: string
}): FieldMappingWorkbenchRowStatus {
  if (mapped && !providerAvailable && schema) return 'drift'
  if (mapped) return 'mapped'
  if (suggestion && (!schema || providerAvailable)) return 'suggested'
  return required ? 'missing' : 'missing'
}

function rowEvidence({
  mapped,
  providerAvailable,
  required,
  schema,
  suggestion,
}: {
  mapped: string
  providerAvailable: boolean
  required: boolean
  schema: ExternalObjectSchema | null
  suggestion: string
}): string {
  if (mapped && providerAvailable) return 'saved mapping matches provider schema'
  if (mapped && schema) return 'saved mapping references a missing provider field'
  if (suggestion && (!schema || providerAvailable)) return 'suggestion is available from schema'
  return required ? 'required field is not mapped' : 'optional field is not mapped'
}

function schemaDiffProblems(
  schema: ExternalObjectSchema | null,
  artifacts: FieldMappingWorkbenchArtifacts,
): string[] {
  if (!schema) return []
  const fields = new Set(schema.fields)
  return artifacts.requiredProviderFields.filter((field) => !fields.has(field))
}

type ParsedJSONRecord = {
  error: boolean
  value: Record<string, unknown> | null
}

function parseJSONRecord(raw: string): ParsedJSONRecord {
  const trimmed = raw.trim()
  if (!trimmed) return { error: false, value: {} }
  try {
    const parsed: unknown = JSON.parse(trimmed)
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
      return { error: true, value: null }
    }
    return { error: false, value: parsed as Record<string, unknown> }
  } catch {
    return { error: true, value: null }
  }
}

function mappedFieldCount(mapping: Record<string, unknown> | null): number {
  return Object.values(mapping ?? {}).filter((value) => stringValue(value).length > 0).length
}

function mappingAllowsPull(direction: ExternalSyncDirection): boolean {
  return (
    direction === ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_PULL ||
    direction === ExternalSyncDirection.EXTERNAL_SYNC_DIRECTION_BIDIRECTIONAL
  )
}

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function unique(values: string[]): string[] {
  return Array.from(new Set(values))
}

function available(value: boolean): string {
  return value ? 'available' : 'missing'
}

function fieldMappingWorkbenchSummary(totals: FieldMappingWorkbench['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} field mapping lanes are blocked`
  if (totals.needs_data > 0)
    return `${totals.needs_data} field mapping lanes need live mapping evidence`
  if (totals.watch > 0) return `${totals.watch} field mapping lanes need hardening`
  return 'field mapping workbench is verified'
}
