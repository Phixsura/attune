import { describe, expect, it } from 'vitest'
import {
  buildDeveloperImportExportWorkbench,
  type DeveloperImportExportArtifacts,
  defaultDeveloperImportExportArtifacts,
} from '@/features/api-keys/developer-import-export-workbench'

describe('buildDeveloperImportExportWorkbench', () => {
  it('verifies the import/export workbench evidence', () => {
    const workbench = buildDeveloperImportExportWorkbench()

    expect(workbench.fingerprint).toBe(
      '2/2 formats / 4 templates / 4/4 required mappings / dry-run 37 create 2 update 1 reject / 4 recovery classes / verifier on',
    )
    expect(workbench.summary).toBe('developer import/export workbench is verified')
    expect(workbench.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      total: 6,
      verified: 6,
      watch: 0,
    })
    expect(workbench.lanes.map((lane) => [lane.key, lane.status, lane.signal])).toEqual([
      ['template_catalog', 'verified', 'CSV + JSON / 2 import templates / 2 export templates'],
      ['schema_preview', 'verified', '8 fields / 4 required / 3 samples'],
      ['field_mapping', 'verified', '8 mapped fields / 4 required tracked'],
      ['dry_run_diff', 'verified', '37 create / 2 update / 1 reject'],
      ['error_recovery', 'verified', 'quarantine / map_status / merge_or_skip / request_scope'],
      [
        'governance_audit',
        'verified',
        'feedback:write / customer_request:write / audit:read / import + export events',
      ],
    ])
    expect(workbench.templates).toHaveLength(4)
    expect(workbench.dryRunRows).toHaveLength(3)
  })

  it('blocks when required CSV or JSON templates are missing', () => {
    const artifacts: DeveloperImportExportArtifacts = {
      ...defaultDeveloperImportExportArtifacts,
      formats: ['csv'],
      templates: defaultDeveloperImportExportArtifacts.templates.slice(0, 2),
    }

    const workbench = buildDeveloperImportExportWorkbench({ artifacts })

    expect(workbench.summary).toBe('1 import/export lanes are blocked')
    expect(workbench.lanes.find((lane) => lane.key === 'template_catalog')).toMatchObject({
      status: 'blocked',
    })
  })

  it('blocks when the verifier is off and labels an empty format catalog', () => {
    const artifacts: DeveloperImportExportArtifacts = {
      ...defaultDeveloperImportExportArtifacts,
      formats: [],
      importExportVerifier: false,
    }

    const workbench = buildDeveloperImportExportWorkbench({ artifacts })

    expect(workbench.summary).toBe('1 import/export lanes are blocked')
    expect(workbench.lanes.find((lane) => lane.key === 'template_catalog')).toMatchObject({
      signal: 'no formats / 2 import templates / 2 export templates',
      status: 'blocked',
    })
  })

  it('watches catalogs that have formats but no import or export templates', () => {
    const artifacts: DeveloperImportExportArtifacts = {
      ...defaultDeveloperImportExportArtifacts,
      exportTemplateCount: 0,
      importTemplateCount: 0,
    }

    const workbench = buildDeveloperImportExportWorkbench({ artifacts })

    expect(workbench.summary).toBe('1 import/export lanes need hardening')
    expect(workbench.lanes.find((lane) => lane.key === 'template_catalog')).toMatchObject({
      evidence: '4/4 templates / import 0 / export 0 / verifier available',
      status: 'watch',
    })
  })

  it('blocks schema preview when field coverage or sample evidence is absent', () => {
    const missingPreview = buildDeveloperImportExportWorkbench({
      artifacts: {
        ...defaultDeveloperImportExportArtifacts,
        schemaPreview: false,
      },
    })
    expect(missingPreview.lanes.find((lane) => lane.key === 'schema_preview')).toMatchObject({
      evidence: 'schema preview missing / 8 fields / 3 sample rows',
      status: 'blocked',
    })

    const blocked = buildDeveloperImportExportWorkbench({
      artifacts: {
        ...defaultDeveloperImportExportArtifacts,
        schemaFieldCount: 2,
      },
    })
    expect(blocked.lanes.find((lane) => lane.key === 'schema_preview')).toMatchObject({
      status: 'blocked',
    })

    const watched = buildDeveloperImportExportWorkbench({
      artifacts: {
        ...defaultDeveloperImportExportArtifacts,
        sampleRows: 0,
      },
    })
    expect(watched.summary).toBe('1 import/export lanes need hardening')
    expect(watched.lanes.find((lane) => lane.key === 'schema_preview')).toMatchObject({
      signal: '8 fields / 4 required / 0 samples',
      status: 'watch',
    })
  })

  it('keeps field mapping on watch when a required field is only suggested', () => {
    const artifacts: DeveloperImportExportArtifacts = {
      ...defaultDeveloperImportExportArtifacts,
      fieldMappingRows: defaultDeveloperImportExportArtifacts.fieldMappingRows.map((row) =>
        row.localField === 'status' ? { ...row, status: 'suggested' } : row,
      ),
    }

    const workbench = buildDeveloperImportExportWorkbench({ artifacts })

    expect(workbench.summary).toBe('1 import/export lanes need hardening')
    expect(workbench.lanes.find((lane) => lane.key === 'field_mapping')).toMatchObject({
      evidence: '3/4 required mapped / 1 suggested / 0 drifted',
      status: 'watch',
    })
  })

  it('blocks field mapping when required rows are missing, drifted, or under-counted', () => {
    const artifacts: DeveloperImportExportArtifacts = {
      ...defaultDeveloperImportExportArtifacts,
      fieldMappingRows: defaultDeveloperImportExportArtifacts.fieldMappingRows.map((row) =>
        row.localField === 'status'
          ? { ...row, status: 'drift' }
          : row.localField === 'source'
            ? { ...row, status: 'missing' }
            : row,
      ),
    }

    const workbench = buildDeveloperImportExportWorkbench({ artifacts })

    expect(workbench.summary).toBe('1 import/export lanes are blocked')
    expect(workbench.lanes.find((lane) => lane.key === 'field_mapping')).toMatchObject({
      evidence: '2/4 required mapped / 0 suggested / 1 drifted',
      status: 'blocked',
    })

    const underCounted = buildDeveloperImportExportWorkbench({
      artifacts: {
        ...defaultDeveloperImportExportArtifacts,
        expectedRequiredMappings: 5,
      },
    })
    expect(underCounted.lanes.find((lane) => lane.key === 'field_mapping')).toMatchObject({
      evidence: '4/5 required mapped / 0 suggested / 0 drifted',
      status: 'blocked',
    })
  })

  it('blocks dry-run diff when preview rows are unavailable', () => {
    const artifacts: DeveloperImportExportArtifacts = {
      ...defaultDeveloperImportExportArtifacts,
      dryRunPreview: false,
      dryRunRows: [],
    }

    const workbench = buildDeveloperImportExportWorkbench({ artifacts })

    expect(workbench.summary).toBe('1 import/export lanes are blocked')
    expect(workbench.lanes.find((lane) => lane.key === 'dry_run_diff')).toMatchObject({
      evidence: 'preview missing / 0 sample rows / 1 rejects classified',
      status: 'blocked',
    })
  })

  it('blocks rejected dry-run rows when no recovery matrix is present', () => {
    const artifacts: DeveloperImportExportArtifacts = {
      ...defaultDeveloperImportExportArtifacts,
      recoveryClassCount: 0,
      recoveryPlaybook: false,
      rollbackPlan: false,
    }

    const workbench = buildDeveloperImportExportWorkbench({ artifacts })

    expect(workbench.summary).toBe('2 import/export lanes are blocked')
    expect(workbench.lanes.find((lane) => lane.key === 'dry_run_diff')).toMatchObject({
      status: 'blocked',
    })
    expect(workbench.lanes.find((lane) => lane.key === 'error_recovery')).toMatchObject({
      status: 'blocked',
    })
  })

  it('watches recovery when playbook or rollback evidence is missing without reject pressure', () => {
    const artifacts: DeveloperImportExportArtifacts = {
      ...defaultDeveloperImportExportArtifacts,
      dryRunRejectRows: 0,
      recoveryPlaybook: false,
      rollbackPlan: false,
    }

    const workbench = buildDeveloperImportExportWorkbench({ artifacts })

    expect(workbench.summary).toBe('1 import/export lanes need hardening')
    expect(workbench.lanes.find((lane) => lane.key === 'error_recovery')).toMatchObject({
      evidence: '4/4 recovery classes / playbook missing / rollback missing',
      status: 'watch',
    })
  })

  it('blocks governance when scopes or audit events fall below contract', () => {
    const artifacts: DeveloperImportExportArtifacts = {
      ...defaultDeveloperImportExportArtifacts,
      auditEventCount: 2,
      requiredScopes: 2,
    }

    const workbench = buildDeveloperImportExportWorkbench({ artifacts })

    expect(workbench.summary).toBe('1 import/export lanes are blocked')
    expect(workbench.lanes.find((lane) => lane.key === 'governance_audit')).toMatchObject({
      evidence: '2/3 scopes / 2/3 audit events / redaction on',
      status: 'blocked',
    })

    const auditOnly = buildDeveloperImportExportWorkbench({
      artifacts: {
        ...defaultDeveloperImportExportArtifacts,
        auditEventCount: 2,
      },
    })
    expect(auditOnly.lanes.find((lane) => lane.key === 'governance_audit')).toMatchObject({
      evidence: '3/3 scopes / 2/3 audit events / redaction on',
      status: 'blocked',
    })
  })

  it('keeps governance on watch when redaction evidence is missing', () => {
    const artifacts: DeveloperImportExportArtifacts = {
      ...defaultDeveloperImportExportArtifacts,
      piiRedaction: false,
    }

    const workbench = buildDeveloperImportExportWorkbench({ artifacts })

    expect(workbench.summary).toBe('1 import/export lanes need hardening')
    expect(workbench.lanes.find((lane) => lane.key === 'governance_audit')).toMatchObject({
      evidence: '3/3 scopes / 3/3 audit events / redaction off',
      status: 'watch',
    })
  })
})
