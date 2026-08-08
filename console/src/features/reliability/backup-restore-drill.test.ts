import { describe, expect, it } from 'vitest'
import { type BackupRestoreDrillInput, buildBackupRestoreDrill } from './backup-restore-drill'

type CompleteRecoveryLastRun = NonNullable<
  NonNullable<BackupRestoreDrillInput['recovery']>['lastRun']
> &
  Required<
    Pick<
      NonNullable<NonNullable<BackupRestoreDrillInput['recovery']>['lastRun']>,
      'backupRef' | 'durationMs' | 'ranAt' | 'status'
    >
  >

type CompleteRecoveryContext = NonNullable<BackupRestoreDrillInput['recovery']> &
  Required<Pick<NonNullable<BackupRestoreDrillInput['recovery']>, 'lastRun'>> & {
    lastRun: CompleteRecoveryLastRun
  }

type CompleteBackupRestoreDrillInput = BackupRestoreDrillInput & {
  recovery: CompleteRecoveryContext
  release: NonNullable<BackupRestoreDrillInput['release']>
}

function completeInput(): CompleteBackupRestoreDrillInput {
  return {
    dashboardHref: '/d/tenant',
    preflightChecks: [
      {
        name: 'backup catalog',
        category: 'backup',
        status: 'pass',
        message: 'Backup catalog is reachable',
        remediation: '',
      },
      {
        name: 'migration ledger',
        category: 'migration',
        status: 'pass',
        message: 'Migration ledger is current',
        remediation: '',
      },
    ],
    recovery: {
      status: 'pass',
      message: 'Last restore drill passed',
      freshnessWindowSeconds: 604800,
      ageSeconds: 3600,
      lastRun: {
        ranAt: '2026-08-01T09:00:00Z',
        status: 'pass',
        backupRef: 'nightly-backup',
        durationMs: 1234,
      },
    },
    release: {
      serviceVersion: '5d6ea83',
      environment: 'production',
      profile: 'production',
      lifecycleState: 'supported',
      ownerTeam: 'Platform',
      compatibilityRules: [{ key: 'additive', label: 'Additive', description: '' }],
      glossary: [],
      runbookUrl: 'https://github.com/Phixsura/attune/blob/main/docs/private-deploy.md',
      escalationUrl: 'https://github.com/Phixsura/attune/issues/new/choose',
      startedAt: '2026-08-01T09:00:00Z',
    },
    tenantName: 'Tenant One',
  }
}

describe('buildBackupRestoreDrill', () => {
  it('verifies backup freshness, restore execution, migration, ownership, and remediation', () => {
    const drill = buildBackupRestoreDrill(completeInput())

    expect(drill.lanes).toHaveLength(5)
    expect(drill.fingerprint).toBe('Tenant One / nightly-backup / supported')
    expect(drill.summary).toBe('backup and restore evidence is verified')
    expect(drill.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      total: 5,
      verified: 5,
      watch: 0,
    })
    expect(drill.lanes.find((lane) => lane.key === 'backup_freshness')).toMatchObject({
      signal: 'backup=nightly-backup / age=1h / window=7d',
      status: 'verified',
    })
    expect(drill.lanes.find((lane) => lane.key === 'restore_execution')).toMatchObject({
      signal: 'pass restore / 1.2s',
      status: 'verified',
    })
    expect(drill.lanes.find((lane) => lane.key === 'migration_readiness')).toMatchObject({
      signal: 'supported / 1 compatibility rules',
      status: 'verified',
    })
  })

  it('blocks the drill when restore or migration evidence is unsafe', () => {
    const base = completeInput()
    const drill = buildBackupRestoreDrill({
      ...base,
      preflightChecks: [
        {
          name: 'restore validation',
          category: 'backup',
          status: 'fail',
          message: 'Restore validation failed',
          remediation: 'Run restore validation against the latest backup.',
        },
      ],
      recovery: {
        ...base.recovery,
        status: 'fail',
        message: 'Restore failed',
        ageSeconds: 900000,
      },
      release: { ...base.release, lifecycleState: 'blocked' },
    })

    expect(drill.summary).toBe('4 backup / restore lanes are blocked')
    expect(drill.totals.blocked).toBe(4)
    expect(drill.lanes.find((lane) => lane.key === 'backup_freshness')).toMatchObject({
      status: 'blocked',
    })
    expect(drill.lanes.find((lane) => lane.key === 'migration_readiness')).toMatchObject({
      status: 'blocked',
    })
    expect(drill.lanes.find((lane) => lane.key === 'remediation_path')).toMatchObject({
      signal: 'Run restore validation against the latest backup.',
      status: 'blocked',
    })
  })

  it('keeps missing backup and restore evidence visible as data gaps', () => {
    const drill = buildBackupRestoreDrill({
      dashboardHref: '/d/tenant',
      tenantName: 'Tenant One',
    })

    expect(drill.fingerprint).toBe('Tenant One / backup unknown / state unknown')
    expect(drill.summary).toBe('5 backup / restore lanes need evidence')
    expect(drill.totals).toEqual({
      blocked: 0,
      needs_data: 5,
      total: 5,
      verified: 0,
      watch: 0,
    })
  })

  it('watches stale restore and migration evidence with an incomplete runbook path', () => {
    const base = completeInput()
    const drill = buildBackupRestoreDrill({
      ...base,
      preflightChecks: [
        {
          name: 'restore rehearsal',
          category: 'release',
          status: 'warn',
          message: 'Restore rehearsal should run before migration.',
          remediation: 'Schedule a restore rehearsal.',
        },
      ],
      recovery: base.recovery
        ? {
            ...base.recovery,
            status: 'warn',
            ageSeconds: 45,
            freshnessWindowSeconds: 90,
            lastRun: {
              ...base.recovery.lastRun,
              status: 'warn',
              durationMs: 65_000,
            },
          }
        : undefined,
      release: {
        ...base.release,
        escalationUrl: '',
        lifecycleState: 'migrating',
        runbookUrl: '',
      },
    })

    expect(drill.summary).toBe('5 backup / restore lanes need attention')
    expect(drill.totals).toMatchObject({ blocked: 0, needs_data: 0, verified: 0, watch: 5 })
    expect(drill.lanes.find((lane) => lane.key === 'backup_freshness')).toMatchObject({
      signal: 'backup=nightly-backup / age=45s / window=2m',
      status: 'watch',
    })
    expect(drill.lanes.find((lane) => lane.key === 'restore_execution')).toMatchObject({
      signal: 'warn restore / 1.1m',
      status: 'watch',
    })
    expect(drill.lanes.find((lane) => lane.key === 'migration_readiness')).toMatchObject({
      signal: 'migrating / 1 compatibility rules',
      status: 'watch',
    })
    expect(drill.lanes.find((lane) => lane.key === 'runbook_ownership')).toMatchObject({
      evidence: 'owner=Platform / runbook=missing / escalation=missing',
      status: 'watch',
    })
    expect(drill.lanes.find((lane) => lane.key === 'remediation_path')).toMatchObject({
      signal: 'Schedule a restore rehearsal.',
      status: 'watch',
    })
  })

  it('keeps malformed restore evidence in needs-data instead of treating skipped checks as safe', () => {
    const base = completeInput()
    const drill = buildBackupRestoreDrill({
      ...base,
      preflightChecks: [],
      recovery: {
        status: 'skipped',
        message: '',
        freshnessWindowSeconds: 0,
        lastRun: {
          ranAt: '',
          status: 'skipped',
          backupRef: '',
          durationMs: 0,
        },
      },
      release: {
        ...base.release,
        compatibilityRules: [],
        lifecycleState: '',
        ownerTeam: '',
      },
    })

    expect(drill.summary).toBe('4 backup / restore lanes need evidence')
    expect(drill.totals).toMatchObject({ blocked: 0, needs_data: 4, verified: 1, watch: 0 })
    expect(drill.lanes.find((lane) => lane.key === 'backup_freshness')).toMatchObject({
      evidence: 'no recovery message / status=skipped',
      signal: 'backup freshness evidence missing',
      status: 'needs_data',
    })
    expect(drill.lanes.find((lane) => lane.key === 'restore_execution')).toMatchObject({
      signal: 'skipped restore / 0ms',
      status: 'needs_data',
    })
    expect(drill.lanes.find((lane) => lane.key === 'migration_readiness')).toMatchObject({
      signal: 'unknown / 0 compatibility rules',
      status: 'needs_data',
    })
    expect(drill.lanes.find((lane) => lane.key === 'runbook_ownership')).toMatchObject({
      signal: 'owner=missing / runbook=present / escalation=present',
      status: 'needs_data',
    })
  })

  it.each([
    [500, '500ms'],
    [9_500, '9.5s'],
    [12_000, '12s'],
    [600_000, '10m'],
    [7_200_000, '2.0h'],
    [43_200_000, '12h'],
    [172_800_000, '2d'],
  ])('formats restore durations at %sms as %s', (durationMs, label) => {
    const base = completeInput()
    const drill = buildBackupRestoreDrill({
      ...base,
      recovery: base.recovery
        ? {
            ...base.recovery,
            lastRun: {
              ...base.recovery.lastRun,
              durationMs,
            },
          }
        : undefined,
    })

    expect(drill.lanes.find((lane) => lane.key === 'restore_execution')?.signal).toBe(
      `pass restore / ${label}`,
    )
  })

  it('keeps skipped restore statuses in needs-data even when the evidence shape is complete', () => {
    const base = completeInput()
    const drill = buildBackupRestoreDrill({
      ...base,
      recovery: base.recovery
        ? {
            ...base.recovery,
            status: 'skipped',
            lastRun: {
              ...base.recovery.lastRun,
              status: 'skipped',
            },
          }
        : undefined,
    })

    expect(drill.summary).toBe('2 backup / restore lanes need evidence')
    expect(drill.lanes.find((lane) => lane.key === 'backup_freshness')).toMatchObject({
      status: 'needs_data',
    })
    expect(drill.lanes.find((lane) => lane.key === 'restore_execution')).toMatchObject({
      status: 'needs_data',
    })
  })

  it('blocks stale backups even when the latest restore run passed', () => {
    const base = completeInput()
    const drill = buildBackupRestoreDrill({
      ...base,
      recovery: base.recovery
        ? {
            ...base.recovery,
            ageSeconds: 120,
            freshnessWindowSeconds: 60,
          }
        : undefined,
      tenantName: '',
    })

    expect(drill.fingerprint).toBe('tenant unknown / nightly-backup / supported')
    expect(drill.summary).toBe('1 backup / restore lanes are blocked')
    expect(drill.lanes.find((lane) => lane.key === 'backup_freshness')).toMatchObject({
      signal: 'backup=nightly-backup / age=2m / window=1m',
      status: 'blocked',
    })
  })
})
