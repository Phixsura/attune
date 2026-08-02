import { describe, expect, it } from 'vitest'
import { buildIncidentTimeline, type IncidentTimelineInput } from './incident-timeline'

type CompleteIncidentRecoveryLastRun = NonNullable<
  NonNullable<IncidentTimelineInput['recovery']>['lastRun']
> &
  Required<
    Pick<
      NonNullable<NonNullable<IncidentTimelineInput['recovery']>['lastRun']>,
      'backupRef' | 'durationMs' | 'ranAt' | 'status'
    >
  >

type CompleteIncidentRecovery = NonNullable<IncidentTimelineInput['recovery']> &
  Required<Pick<NonNullable<IncidentTimelineInput['recovery']>, 'lastRun'>> & {
    lastRun: CompleteIncidentRecoveryLastRun
  }

type CompleteIncidentTimelineInput = IncidentTimelineInput &
  Required<Pick<IncidentTimelineInput, 'feedbackStats' | 'notificationEvidence' | 'release'>> & {
    recovery: CompleteIncidentRecovery
  }

function completeInput(): CompleteIncidentTimelineInput {
  return {
    activeGdpr: 2,
    dashboardHref: '/d/tenant',
    deadDeliveryCount: 2,
    feedbackHref: '/feedback?account_key=tenant-1',
    feedbackStats: {
      periodStart: '2026-07-01T00:00:00Z',
      periodEnd: '2026-07-31T23:59:59Z',
      total: '2',
      urgentCount: '1',
      dims: [{ dim: 'severity', top: [{ value: 'P0', count: '1' }] }],
    },
    inflightDeadDeliveries: 1,
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
    preflightChecks: [
      {
        name: 'database',
        category: 'database',
        status: 'pass',
        message: 'Database reachable',
        remediation: '',
      },
      {
        name: 'worker',
        category: 'worker',
        status: 'warn',
        message: 'Replay queue needs review',
        remediation: 'Run the replay drill before release.',
      },
    ],
    queuedGdpr: 1,
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
    retryableDeadDeliveries: 1,
    scheduledDeletes: 1,
    tenantName: 'Tenant One',
  }
}

function quietInput(overrides: Partial<IncidentTimelineInput> = {}): IncidentTimelineInput {
  const base = completeInput()
  return {
    ...base,
    activeGdpr: 0,
    deadDeliveryCount: 0,
    feedbackStats: {
      ...base.feedbackStats,
      total: '0',
      urgentCount: '0',
    },
    inflightDeadDeliveries: 0,
    notificationEvidence: [
      {
        ...base.notificationEvidence[0],
        failedCustomers: 0,
        lastEventAt: '',
        notifiedCustomers: 4,
        recoveryPendingCustomers: 0,
      },
    ],
    preflightChecks: [
      {
        name: 'database',
        category: 'database',
        status: 'pass',
        message: 'Database reachable',
        remediation: '',
      },
    ],
    queuedGdpr: 0,
    retryableDeadDeliveries: 0,
    scheduledDeletes: 0,
    ...overrides,
  }
}

describe('buildIncidentTimeline', () => {
  it('reconstructs incident start, detection, impact, mitigation, recovery, and notification phases', () => {
    const timeline = buildIncidentTimeline(completeInput())

    expect(timeline.events).toHaveLength(6)
    expect(timeline.fingerprint).toBe('Tenant One / 5d6ea83 / supported')
    expect(timeline.summary).toBe('4 incident timeline phases need attention')
    expect(timeline.totals).toEqual({
      attention: 4,
      blocked: 0,
      needs_data: 0,
      recovered: 1,
      verified: 1,
      total: 6,
    })
    expect(timeline.events.find((event) => event.phase === 'detection')).toMatchObject({
      signal: 'worker: Replay queue needs review',
      status: 'attention',
    })
    expect(timeline.events.find((event) => event.phase === 'impact')).toMatchObject({
      signal: '1 urgent / 2 total feedback',
      status: 'attention',
    })
    expect(timeline.events.find((event) => event.phase === 'customer_notification')).toMatchObject({
      signal: '1 failed / 1 recovery pending customers',
      status: 'attention',
    })
  })

  it('blocks the incident timeline when customer impact and recovery evidence are unsafe', () => {
    const base = completeInput()
    const release = base.release
    const recovery = base.recovery
    const feedbackStats = base.feedbackStats
    if (!release || !recovery || !feedbackStats) {
      throw new Error('completeInput must include release, recovery, and feedback fixtures')
    }

    const timeline = buildIncidentTimeline({
      ...base,
      activeGdpr: 1,
      deadDeliveryCount: 5,
      feedbackStats: {
        ...feedbackStats,
        total: '10',
        urgentCount: '5',
      },
      notificationEvidence: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 8,
          notifiedCustomers: 1,
          failedCustomers: 3,
          suppressedCustomers: 0,
          recoveryPendingCustomers: 2,
          eventCount: 6,
          lastEventAt: '2026-07-16T00:00:00Z',
        },
      ],
      preflightChecks: [
        {
          name: 'database',
          category: 'database',
          status: 'fail',
          message: 'Database unreachable',
          remediation: 'Fail over to the standby database.',
        },
      ],
      recovery: { ...recovery, status: 'fail', message: 'Restore failed' },
      release: { ...release, lifecycleState: 'blocked' },
      retryableDeadDeliveries: 2,
      scheduledDeletes: 1,
    })

    expect(timeline.summary).toBe('6 incident timeline phases are blocked')
    expect(timeline.totals.blocked).toBe(6)
    expect(timeline.events.every((event) => event.status === 'blocked')).toBe(true)
  })

  it('keeps missing incident reconstruction inputs visible as data gaps', () => {
    const timeline = buildIncidentTimeline({
      dashboardHref: '/d/tenant',
      feedbackHref: '/feedback',
      notificationHref: '/integrations/request-notifications',
      tenantName: 'Tenant One',
    })

    expect(timeline.fingerprint).toBe('Tenant One / release unknown / state unknown')
    expect(timeline.summary).toBe('6 incident timeline phases need data')
    expect(timeline.totals).toEqual({
      attention: 0,
      blocked: 0,
      needs_data: 6,
      recovered: 0,
      verified: 0,
      total: 6,
    })
  })

  it('marks the incident timeline fully verified after recovery clears pressure', () => {
    const timeline = buildIncidentTimeline(quietInput())

    expect(timeline.summary).toBe('incident timeline is fully verified')
    expect(timeline.totals).toEqual({
      attention: 0,
      blocked: 0,
      needs_data: 0,
      recovered: 1,
      verified: 5,
      total: 6,
    })
    expect(timeline.events.find((event) => event.phase === 'detection')).toMatchObject({
      signal: 'no readiness issue detected',
      status: 'verified',
    })
    expect(timeline.events.find((event) => event.phase === 'customer_notification')).toMatchObject({
      occurredAtLabel: 'notification window unknown',
      signal: '0 failed / 0 recovery pending customers',
      status: 'verified',
    })
  })

  it('keeps deprecated release starts in attention until migration completes', () => {
    const timeline = buildIncidentTimeline(
      quietInput({
        release: {
          ...completeInput().release,
          lifecycleState: 'deprecated',
        },
      }),
    )

    expect(timeline.summary).toBe('1 incident timeline phases need attention')
    expect(timeline.events.find((event) => event.phase === 'start')).toMatchObject({
      signal: '5d6ea83 / deprecated',
      status: 'attention',
    })
  })

  it('keeps restore warnings actionable with remediation evidence', () => {
    const timeline = buildIncidentTimeline(
      quietInput({
        recovery: {
          ...completeInput().recovery,
          message: 'Restore drill is stale',
          remediation: 'run restore drill',
          status: 'warn',
        },
      }),
    )

    expect(timeline.summary).toBe('1 incident timeline phases need attention')
    expect(timeline.events.find((event) => event.phase === 'recovery')).toMatchObject({
      evidence:
        'status=warn / freshness=604800s / age=3600s / backup=nightly-backup / duration=1234ms / remediation=run restore drill',
      status: 'attention',
    })
  })

  it('treats skipped recovery and incomplete release metadata as data gaps', () => {
    const timeline = buildIncidentTimeline(
      quietInput({
        recovery: {
          ...completeInput().recovery,
          status: 'skipped',
        },
        release: {
          ...completeInput().release,
          environment: '',
          lifecycleState: '',
          ownerTeam: '',
          serviceVersion: '',
        },
      }),
    )

    expect(timeline.summary).toBe('2 incident timeline phases need data')
    expect(timeline.events.find((event) => event.phase === 'start')).toMatchObject({
      status: 'needs_data',
    })
    expect(timeline.events.find((event) => event.phase === 'recovery')).toMatchObject({
      status: 'needs_data',
    })
  })

  it('treats malformed feedback counters as missing impact evidence', () => {
    const timeline = buildIncidentTimeline(
      quietInput({
        feedbackStats: {
          ...completeInput().feedbackStats,
          total: ' ',
          urgentCount: 'not-a-number',
        },
      }),
    )

    expect(timeline.summary).toBe('1 incident timeline phases need data')
    expect(timeline.events.find((event) => event.phase === 'impact')).toMatchObject({
      signal: 'feedback impact unknown',
      status: 'needs_data',
    })
  })

  it('verifies non-urgent impact windows with positive feedback volume', () => {
    const timeline = buildIncidentTimeline(
      quietInput({
        feedbackStats: {
          ...completeInput().feedbackStats,
          total: '10',
          urgentCount: '0',
        },
      }),
    )

    expect(timeline.summary).toBe('incident timeline is fully verified')
    expect(timeline.events.find((event) => event.phase === 'impact')).toMatchObject({
      signal: '0 urgent / 10 total feedback',
      status: 'verified',
    })
  })
})
