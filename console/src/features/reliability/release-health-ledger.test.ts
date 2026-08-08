import { describe, expect, it } from 'vitest'
import { buildReleaseHealthLedger, type ReleaseHealthLedgerInput } from './release-health-ledger'

type CompleteReleaseHealthLedgerInput = ReleaseHealthLedgerInput &
  Required<
    Pick<
      ReleaseHealthLedgerInput,
      'feedbackStats' | 'notificationEvidence' | 'recovery' | 'release'
    >
  >

function completeInput(): CompleteReleaseHealthLedgerInput {
  return {
    dashboardHref: '/d/tenant',
    escalationHref: '/issues/new',
    feedbackHref: '/feedback?account_key=tenant-1',
    feedbackStats: {
      periodStart: '2026-07-01T00:00:00Z',
      periodEnd: '2026-07-31T23:59:59Z',
      total: '2',
      urgentCount: '1',
      dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
    },
    notificationEvidence: [
      {
        requestStatus: 'shipped',
        expectedCustomers: 4,
        notifiedCustomers: 2,
        failedCustomers: 1,
        suppressedCustomers: 1,
        recoveryPendingCustomers: 1,
        eventCount: 2,
        lastEventAt: '2026-07-16T00:00:00Z',
      },
    ],
    notificationHref: '/integrations/request-notifications',
    readinessStatus: 'warn',
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
      escalationUrl: '/issues/new',
      startedAt: '2026-08-01T09:00:00Z',
    },
  }
}

describe('buildReleaseHealthLedger', () => {
  it('correlates release version, lifecycle, recovery, feedback, and notification evidence', () => {
    const ledger = buildReleaseHealthLedger(completeInput())

    expect(ledger.entries).toHaveLength(5)
    expect(ledger.releaseFingerprint).toBe('5d6ea83 / production / supported')
    expect(ledger.summary).toBe('2 release-health signals need attention')
    expect(ledger.totals).toEqual({
      attention: 2,
      blocked: 0,
      needs_data: 0,
      ready: 3,
      total: 5,
    })
    expect(ledger.entries.find((entry) => entry.key === 'feedback_pressure')).toMatchObject({
      signal: '1 urgent / 2 total feedback',
      status: 'attention',
    })
    expect(ledger.entries.find((entry) => entry.key === 'notification_failures')).toMatchObject({
      signal: '1 failed / 1 recovery pending customers',
      status: 'attention',
    })
  })

  it('blocks release health when release lifecycle or recovery evidence is blocked', () => {
    const base = completeInput()
    const recovery = base.recovery
    const release = base.release
    if (!recovery || !release) {
      throw new Error('completeInput must include release and recovery fixtures')
    }
    const ledger = buildReleaseHealthLedger({
      ...base,
      recovery: { ...recovery, status: 'fail', message: 'Restore failed' },
      release: { ...release, lifecycleState: 'blocked' },
    })

    expect(ledger.totals.blocked).toBeGreaterThanOrEqual(3)
    expect(ledger.entries.find((entry) => entry.key === 'runtime_version')).toMatchObject({
      status: 'blocked',
    })
    expect(ledger.entries.find((entry) => entry.key === 'restore_drill')).toMatchObject({
      status: 'blocked',
    })
  })

  it('keeps missing release-health inputs visible as data gaps', () => {
    const ledger = buildReleaseHealthLedger({
      dashboardHref: '/d/tenant',
      feedbackHref: '/feedback',
      notificationHref: '/integrations/request-notifications',
    })

    expect(ledger.releaseFingerprint).toBe('release unknown')
    expect(ledger.summary).toBe('5 release-health signals need data')
    expect(ledger.totals).toEqual({
      attention: 0,
      blocked: 0,
      needs_data: 5,
      ready: 0,
      total: 5,
    })
  })

  it('marks release health ready when pressure and notification residue are clear', () => {
    const base = completeInput()
    const ledger = buildReleaseHealthLedger({
      ...base,
      feedbackStats: {
        ...base.feedbackStats,
        total: '0',
        urgentCount: '0',
      },
      notificationEvidence: [
        {
          ...base.notificationEvidence[0],
          failedCustomers: 0,
          recoveryPendingCustomers: 0,
        },
      ],
      readinessStatus: undefined,
    })

    expect(ledger.summary).toBe('release health is ready')
    expect(ledger.totals).toEqual({
      attention: 0,
      blocked: 0,
      needs_data: 0,
      ready: 5,
      total: 5,
    })
    expect(ledger.entries.find((entry) => entry.key === 'feedback_pressure')).toMatchObject({
      signal: '0 urgent / 0 total feedback',
      status: 'ready',
    })
  })

  it('keeps non-production supported releases in attention', () => {
    const base = completeInput()
    const ledger = buildReleaseHealthLedger({
      ...base,
      feedbackStats: {
        ...base.feedbackStats,
        urgentCount: '0',
      },
      notificationEvidence: [
        {
          ...base.notificationEvidence[0],
          failedCustomers: 0,
          recoveryPendingCustomers: 0,
        },
      ],
      release: {
        ...base.release,
        environment: 'staging',
      },
    })

    expect(ledger.summary).toBe('1 release-health signals need attention')
    expect(ledger.entries.find((entry) => entry.key === 'runtime_version')).toMatchObject({
      signal: '5d6ea83 / staging / Platform',
      status: 'attention',
    })
  })

  it.each([
    'deprecated',
    'migrating',
    'recovering',
  ])('keeps %s lifecycle releases in attention', (lifecycleState) => {
    const base = completeInput()
    const ledger = buildReleaseHealthLedger({
      ...base,
      feedbackStats: {
        ...base.feedbackStats,
        urgentCount: '0',
      },
      notificationEvidence: [
        {
          ...base.notificationEvidence[0],
          failedCustomers: 0,
          recoveryPendingCustomers: 0,
        },
      ],
      release: {
        ...base.release,
        lifecycleState,
      },
    })

    expect(ledger.summary).toBe('1 release-health signals need attention')
    expect(ledger.entries.find((entry) => entry.key === 'lifecycle_gate')).toMatchObject({
      signal: lifecycleState,
      status: 'attention',
    })
  })

  it('keeps restore warnings actionable with remediation evidence', () => {
    const base = completeInput()
    const ledger = buildReleaseHealthLedger({
      ...base,
      feedbackStats: {
        ...base.feedbackStats,
        urgentCount: '0',
      },
      notificationEvidence: [
        {
          ...base.notificationEvidence[0],
          failedCustomers: 0,
          recoveryPendingCustomers: 0,
        },
      ],
      recovery: {
        ...base.recovery,
        message: 'Restore drill is stale',
        remediation: 'run backup restore drill',
        status: 'warn',
      },
    })

    expect(ledger.summary).toBe('1 release-health signals need attention')
    expect(ledger.entries.find((entry) => entry.key === 'restore_drill')).toMatchObject({
      evidence:
        'freshness=604800s / age=3600s / backup=nightly-backup / duration=1234ms / remediation=run backup restore drill',
      status: 'attention',
    })
  })

  it('treats skipped restore drills and incomplete runtime metadata as data gaps', () => {
    const base = completeInput()
    const ledger = buildReleaseHealthLedger({
      ...base,
      feedbackStats: {
        ...base.feedbackStats,
        urgentCount: '0',
      },
      notificationEvidence: [
        {
          ...base.notificationEvidence[0],
          failedCustomers: 0,
          recoveryPendingCustomers: 0,
        },
      ],
      recovery: {
        ...base.recovery,
        status: 'skipped',
      },
      release: {
        ...base.release,
        ownerTeam: ' ',
      },
    })

    expect(ledger.summary).toBe('2 release-health signals need data')
    expect(ledger.entries.find((entry) => entry.key === 'runtime_version')).toMatchObject({
      status: 'needs_data',
    })
    expect(ledger.entries.find((entry) => entry.key === 'restore_drill')).toMatchObject({
      status: 'needs_data',
    })
  })

  it('blocks release health when feedback and notification pressure cross thresholds', () => {
    const base = completeInput()
    const ledger = buildReleaseHealthLedger({
      ...base,
      feedbackStats: {
        ...base.feedbackStats,
        total: '10',
        urgentCount: '5',
      },
      notificationEvidence: [
        {
          ...base.notificationEvidence[0],
          failedCustomers: 3,
          recoveryPendingCustomers: 2,
        },
      ],
    })

    expect(ledger.summary).toBe('2 blocked release-health signals')
    expect(ledger.entries.find((entry) => entry.key === 'feedback_pressure')).toMatchObject({
      signal: '5 urgent / 10 total feedback',
      status: 'blocked',
    })
    expect(ledger.entries.find((entry) => entry.key === 'notification_failures')).toMatchObject({
      signal: '3 failed / 2 recovery pending customers',
      status: 'blocked',
    })
  })

  it('treats blank or malformed feedback counters as missing data', () => {
    const base = completeInput()
    const ledger = buildReleaseHealthLedger({
      ...base,
      feedbackStats: {
        ...base.feedbackStats,
        total: ' ',
        urgentCount: 'not-a-number',
      },
      notificationEvidence: [
        {
          ...base.notificationEvidence[0],
          failedCustomers: 0,
          recoveryPendingCustomers: 0,
        },
      ],
    })

    expect(ledger.summary).toBe('1 release-health signals need data')
    expect(ledger.entries.find((entry) => entry.key === 'feedback_pressure')).toMatchObject({
      signal: 'feedback pressure unknown',
      status: 'needs_data',
    })
  })
})
