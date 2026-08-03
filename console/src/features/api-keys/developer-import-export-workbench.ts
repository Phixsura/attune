export type DeveloperImportExportStatus = 'verified' | 'watch' | 'blocked' | 'needs_data'

export type DeveloperImportExportLaneKey =
  | 'template_catalog'
  | 'schema_preview'
  | 'field_mapping'
  | 'dry_run_diff'
  | 'error_recovery'
  | 'governance_audit'

export type DeveloperImportExportLane = {
  actionLabel: string
  evidence: string
  guardrail: string
  key: DeveloperImportExportLaneKey
  owner: string
  signal: string
  status: DeveloperImportExportStatus
  title: string
}

export type DeveloperImportExportTemplate = {
  direction: 'import' | 'export'
  format: 'csv' | 'json'
  id: string
  object: string
  status: 'ready' | 'watch' | 'blocked'
}

export type DeveloperImportExportMappingRow = {
  localField: string
  providerField: string
  required: boolean
  status: 'mapped' | 'suggested' | 'missing' | 'drift'
}

export type DeveloperImportExportDryRunRow = {
  action: 'create' | 'update' | 'reject'
  evidence: string
  recoveryAction: string
  row: number
  status: 'ready' | 'watch' | 'blocked'
}

export type DeveloperImportExportWorkbench = {
  dryRunRows: DeveloperImportExportDryRunRow[]
  fingerprint: string
  lanes: DeveloperImportExportLane[]
  mappingRows: DeveloperImportExportMappingRow[]
  summary: string
  templates: DeveloperImportExportTemplate[]
  totals: Record<DeveloperImportExportStatus, number> & {
    total: number
  }
}

export type DeveloperImportExportArtifacts = {
  auditEventCount: number
  dryRunCreateRows: number
  dryRunPreview: boolean
  dryRunRejectRows: number
  dryRunRows: DeveloperImportExportDryRunRow[]
  dryRunUpdateRows: number
  expectedAuditEvents: number
  expectedFormats: number
  expectedRecoveryClasses: number
  expectedRequiredMappings: number
  expectedScopes: number
  expectedTemplates: number
  exportTemplateCount: number
  fieldMappingRows: DeveloperImportExportMappingRow[]
  formats: Array<'csv' | 'json'>
  importExportVerifier: boolean
  importTemplateCount: number
  piiRedaction: boolean
  recoveryClassCount: number
  recoveryPlaybook: boolean
  requiredScopes: number
  rollbackPlan: boolean
  sampleRows: number
  schemaFieldCount: number
  schemaPreview: boolean
  templates: DeveloperImportExportTemplate[]
}

export type DeveloperImportExportWorkbenchInput = {
  artifacts?: DeveloperImportExportArtifacts
}

export const defaultDeveloperImportExportArtifacts: DeveloperImportExportArtifacts = {
  auditEventCount: 3,
  dryRunCreateRows: 37,
  dryRunPreview: true,
  dryRunRejectRows: 1,
  dryRunRows: [
    {
      action: 'create',
      evidence: 'row 1 / new external id / customer request create',
      recoveryAction: 'commit',
      row: 1,
      status: 'ready',
    },
    {
      action: 'update',
      evidence: 'row 2 / matching external id / evidence timestamp newer',
      recoveryAction: 'review diff',
      row: 2,
      status: 'ready',
    },
    {
      action: 'reject',
      evidence: 'row 3 / invalid enum / source state needs mapping',
      recoveryAction: 'map_status',
      row: 3,
      status: 'watch',
    },
  ],
  dryRunUpdateRows: 2,
  expectedAuditEvents: 3,
  expectedFormats: 2,
  expectedRecoveryClasses: 4,
  expectedRequiredMappings: 4,
  expectedScopes: 3,
  expectedTemplates: 4,
  exportTemplateCount: 2,
  fieldMappingRows: [
    { localField: 'external_id', providerField: 'External ID', required: true, status: 'mapped' },
    { localField: 'title', providerField: 'Title', required: true, status: 'mapped' },
    { localField: 'status', providerField: 'State', required: true, status: 'mapped' },
    { localField: 'source', providerField: 'Source', required: true, status: 'mapped' },
    { localField: 'account_key', providerField: 'Account Key', required: false, status: 'mapped' },
    {
      localField: 'revenue_impact_cents',
      providerField: 'Revenue Impact Cents',
      required: false,
      status: 'mapped',
    },
    {
      localField: 'evidence_url',
      providerField: 'Evidence URL',
      required: false,
      status: 'mapped',
    },
    { localField: 'tags', providerField: 'Tags', required: false, status: 'mapped' },
  ],
  formats: ['csv', 'json'],
  importExportVerifier: true,
  importTemplateCount: 2,
  piiRedaction: true,
  recoveryClassCount: 4,
  recoveryPlaybook: true,
  requiredScopes: 3,
  rollbackPlan: true,
  sampleRows: 3,
  schemaFieldCount: 8,
  schemaPreview: true,
  templates: [
    {
      direction: 'import',
      format: 'csv',
      id: 'feedback-import',
      object: 'feedback',
      status: 'ready',
    },
    {
      direction: 'import',
      format: 'json',
      id: 'customer-request-import',
      object: 'customer_request',
      status: 'ready',
    },
    {
      direction: 'export',
      format: 'json',
      id: 'decision-evidence-export',
      object: 'decision_evidence',
      status: 'ready',
    },
    {
      direction: 'export',
      format: 'csv',
      id: 'audit-log-export',
      object: 'audit_log',
      status: 'ready',
    },
  ],
}

export function buildDeveloperImportExportWorkbench(
  input: DeveloperImportExportWorkbenchInput = {},
): DeveloperImportExportWorkbench {
  const artifacts = input.artifacts ?? defaultDeveloperImportExportArtifacts
  const lanes = [
    templateCatalogLane(artifacts),
    schemaPreviewLane(artifacts),
    fieldMappingLane(artifacts),
    dryRunDiffLane(artifacts),
    errorRecoveryLane(artifacts),
    governanceAuditLane(artifacts),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    total: lanes.length,
    verified: lanes.filter((lane) => lane.status === 'verified').length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }
  const requiredMapped = requiredMappings(artifacts).filter((row) => row.status === 'mapped').length

  return {
    dryRunRows: artifacts.dryRunRows,
    fingerprint: `${artifacts.formats.length}/${artifacts.expectedFormats} formats / ${
      artifacts.templates.length
    } templates / ${requiredMapped}/${artifacts.expectedRequiredMappings} required mappings / dry-run ${
      artifacts.dryRunCreateRows
    } create ${artifacts.dryRunUpdateRows} update ${artifacts.dryRunRejectRows} reject / ${
      artifacts.recoveryClassCount
    } recovery classes / verifier ${onOff(artifacts.importExportVerifier)}`,
    lanes,
    mappingRows: artifacts.fieldMappingRows,
    summary: developerImportExportSummary(totals),
    templates: artifacts.templates,
    totals,
  }
}

function templateCatalogLane(artifacts: DeveloperImportExportArtifacts): DeveloperImportExportLane {
  return {
    actionLabel: 'Review templates',
    evidence: `${artifacts.templates.length}/${artifacts.expectedTemplates} templates / import ${artifacts.importTemplateCount} / export ${artifacts.exportTemplateCount} / verifier ${available(
      artifacts.importExportVerifier,
    )}`,
    guardrail:
      'CSV and JSON templates must cover both import and export paths before operators can move data without inventing one-off spreadsheets.',
    key: 'template_catalog',
    owner: 'Developer Platform + Product Ops',
    signal: `${formatLabel(artifacts.formats)} / ${artifacts.importTemplateCount} import templates / ${artifacts.exportTemplateCount} export templates`,
    status: templateCatalogStatus(artifacts),
    title: 'Template catalog',
  }
}

function schemaPreviewLane(artifacts: DeveloperImportExportArtifacts): DeveloperImportExportLane {
  return {
    actionLabel: 'Preview schema',
    evidence: `schema preview ${available(artifacts.schemaPreview)} / ${artifacts.schemaFieldCount} fields / ${artifacts.sampleRows} sample rows`,
    guardrail:
      'Operators need field types, required flags, and sample values before they can trust a bulk import or export mapping.',
    key: 'schema_preview',
    owner: 'Console + Integrations',
    signal: `${artifacts.schemaFieldCount} fields / ${artifacts.expectedRequiredMappings} required / ${artifacts.sampleRows} samples`,
    status: schemaPreviewStatus(artifacts),
    title: 'Schema preview',
  }
}

function fieldMappingLane(artifacts: DeveloperImportExportArtifacts): DeveloperImportExportLane {
  const required = requiredMappings(artifacts)
  const requiredMapped = required.filter((row) => row.status === 'mapped').length
  const drifted = artifacts.fieldMappingRows.filter((row) => row.status === 'drift').length
  const suggested = artifacts.fieldMappingRows.filter((row) => row.status === 'suggested').length
  return {
    actionLabel: 'Resolve mappings',
    evidence: `${requiredMapped}/${artifacts.expectedRequiredMappings} required mapped / ${suggested} suggested / ${drifted} drifted`,
    guardrail:
      'Required local fields must be mapped explicitly; suggestions help setup speed but do not count as saved coverage.',
    key: 'field_mapping',
    owner: 'Product Ops',
    signal: `${artifacts.fieldMappingRows.length} mapped fields / ${required.length} required tracked`,
    status: fieldMappingStatus(artifacts),
    title: 'Field mapping',
  }
}

function dryRunDiffLane(artifacts: DeveloperImportExportArtifacts): DeveloperImportExportLane {
  return {
    actionLabel: 'Open dry-run diff',
    evidence: `preview ${available(artifacts.dryRunPreview)} / ${artifacts.dryRunRows.length} sample rows / ${artifacts.dryRunRejectRows} rejects classified`,
    guardrail:
      'Bulk import and export flows need a dry-run diff that separates creates, updates, skips, rejects, and recoverable errors before commit.',
    key: 'dry_run_diff',
    owner: 'Console + Reliability',
    signal: `${artifacts.dryRunCreateRows} create / ${artifacts.dryRunUpdateRows} update / ${artifacts.dryRunRejectRows} reject`,
    status: dryRunDiffStatus(artifacts),
    title: 'Dry-run diff',
  }
}

function errorRecoveryLane(artifacts: DeveloperImportExportArtifacts): DeveloperImportExportLane {
  return {
    actionLabel: 'Review recovery',
    evidence: `${artifacts.recoveryClassCount}/${artifacts.expectedRecoveryClasses} recovery classes / playbook ${available(
      artifacts.recoveryPlaybook,
    )} / rollback ${available(artifacts.rollbackPlan)}`,
    guardrail:
      'Rejected rows, invalid enums, duplicates, missing permissions, and bad evidence links need visible recovery paths instead of opaque failed uploads.',
    key: 'error_recovery',
    owner: 'Reliability + Support',
    signal: 'quarantine / map_status / merge_or_skip / request_scope',
    status: errorRecoveryStatus(artifacts),
    title: 'Error recovery',
  }
}

function governanceAuditLane(artifacts: DeveloperImportExportArtifacts): DeveloperImportExportLane {
  return {
    actionLabel: 'Review audit evidence',
    evidence: `${artifacts.requiredScopes}/${artifacts.expectedScopes} scopes / ${artifacts.auditEventCount}/${artifacts.expectedAuditEvents} audit events / redaction ${onOff(
      artifacts.piiRedaction,
    )}`,
    guardrail:
      'Import/export needs permission boundaries, dry-run and commit audit events, export download trails, and PII redaction before bulk workflows are safe.',
    key: 'governance_audit',
    owner: 'Security + Developer Platform',
    signal: 'feedback:write / customer_request:write / audit:read / import + export events',
    status: governanceAuditStatus(artifacts),
    title: 'Governance and audit',
  }
}

function templateCatalogStatus(
  artifacts: DeveloperImportExportArtifacts,
): DeveloperImportExportStatus {
  if (!artifacts.importExportVerifier) return 'blocked'
  if (
    !artifacts.formats.includes('csv') ||
    !artifacts.formats.includes('json') ||
    artifacts.templates.length < artifacts.expectedTemplates
  ) {
    return 'blocked'
  }
  if (artifacts.importTemplateCount === 0 || artifacts.exportTemplateCount === 0) return 'watch'
  return 'verified'
}

function schemaPreviewStatus(
  artifacts: DeveloperImportExportArtifacts,
): DeveloperImportExportStatus {
  if (!artifacts.schemaPreview) return 'blocked'
  if (artifacts.schemaFieldCount < artifacts.expectedRequiredMappings) return 'blocked'
  if (artifacts.sampleRows === 0) return 'watch'
  return 'verified'
}

function fieldMappingStatus(
  artifacts: DeveloperImportExportArtifacts,
): DeveloperImportExportStatus {
  const required = requiredMappings(artifacts)
  if (required.length < artifacts.expectedRequiredMappings) return 'blocked'
  if (required.some((row) => row.status === 'missing' || row.status === 'drift')) return 'blocked'
  if (required.some((row) => row.status === 'suggested')) return 'watch'
  return 'verified'
}

function dryRunDiffStatus(artifacts: DeveloperImportExportArtifacts): DeveloperImportExportStatus {
  if (!artifacts.dryRunPreview || artifacts.dryRunRows.length === 0) return 'blocked'
  if (
    artifacts.dryRunRejectRows > 0 &&
    (!artifacts.recoveryPlaybook ||
      artifacts.recoveryClassCount < artifacts.expectedRecoveryClasses)
  ) {
    return 'blocked'
  }
  return 'verified'
}

function errorRecoveryStatus(
  artifacts: DeveloperImportExportArtifacts,
): DeveloperImportExportStatus {
  if (artifacts.recoveryClassCount < artifacts.expectedRecoveryClasses) return 'blocked'
  if (!artifacts.recoveryPlaybook || !artifacts.rollbackPlan) return 'watch'
  return 'verified'
}

function governanceAuditStatus(
  artifacts: DeveloperImportExportArtifacts,
): DeveloperImportExportStatus {
  if (artifacts.requiredScopes < artifacts.expectedScopes) return 'blocked'
  if (artifacts.auditEventCount < artifacts.expectedAuditEvents) return 'blocked'
  if (!artifacts.piiRedaction) return 'watch'
  return 'verified'
}

function developerImportExportSummary(totals: DeveloperImportExportWorkbench['totals']): string {
  if (totals.blocked > 0) return `${totals.blocked} import/export lanes are blocked`
  /* v8 ignore next -- @preserve: no import/export lane emits needs_data; kept for status-union exhaustiveness. */
  if (totals.needs_data > 0) return `${totals.needs_data} import/export lanes need evidence`
  if (totals.watch > 0) return `${totals.watch} import/export lanes need hardening`
  return 'developer import/export workbench is verified'
}

function requiredMappings(
  artifacts: DeveloperImportExportArtifacts,
): DeveloperImportExportMappingRow[] {
  return artifacts.fieldMappingRows.filter((row) => row.required)
}

function formatLabel(formats: string[]): string {
  if (formats.length === 0) return 'no formats'
  return formats.map((format) => format.toUpperCase()).join(' + ')
}

function available(value: boolean): string {
  return value ? 'available' : 'missing'
}

function onOff(value: boolean): string {
  return value ? 'on' : 'off'
}
