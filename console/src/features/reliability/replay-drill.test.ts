import { describe, expect, it } from 'vitest'
import { buildReplayDrill } from './replay-drill'

describe('buildReplayDrill', () => {
  it('maps every reliability SLO to an executable replay lane', () => {
    const drill = buildReplayDrill({
      activeGdpr: 0,
      dashboardHref: '/d/tenant',
      deadDeliveryCount: 0,
      inflightDeadDeliveries: 0,
      queuedGdpr: 0,
      readinessStatus: 'pass',
      recoveryStatus: 'pass',
      releaseLifecycleState: 'supported',
      retryableDeadDeliveries: 0,
      scheduledDeletes: 0,
      tenantName: 'Tenant One',
    })

    expect(drill.lanes).toHaveLength(7)
    expect(drill.totals).toEqual({ attention: 0, blocked: 0, ready: 7, total: 7 })
    expect(drill.lanes.map((lane) => lane.entryHref)).toEqual(
      expect.arrayContaining([
        '/feedback?account_key=tenant-one',
        '/feedback/terminal-failures',
        '/administration/dead-deliveries',
        '/administration/gdpr',
      ]),
    )
  })

  it('marks replay lanes that need operator attention', () => {
    const drill = buildReplayDrill({
      activeGdpr: 2,
      dashboardHref: '/d/tenant',
      deadDeliveryCount: 3,
      inflightDeadDeliveries: 1,
      queuedGdpr: 1,
      readinessStatus: 'warn',
      recoveryStatus: 'pass',
      releaseLifecycleState: 'supported',
      retryableDeadDeliveries: 2,
      scheduledDeletes: 1,
      tenantName: 'Tenant One',
    })

    expect(drill.totals.attention).toBeGreaterThanOrEqual(4)
    expect(drill.lanes.find((lane) => lane.key === 'outbox_delivery')).toMatchObject({
      signal: '2 retryable / 1 in-flight / 3 dead',
      status: 'attention',
    })
    expect(drill.lanes.find((lane) => lane.key === 'gdpr_job')).toMatchObject({
      signal: '1 queued / 2 active / 1 scheduled delete',
      status: 'attention',
    })
  })

  it('blocks the drill when the release lifecycle is blocked', () => {
    const drill = buildReplayDrill({
      activeGdpr: 0,
      dashboardHref: '/d/tenant',
      deadDeliveryCount: 0,
      inflightDeadDeliveries: 0,
      queuedGdpr: 0,
      readinessStatus: 'pass',
      recoveryStatus: 'pass',
      releaseLifecycleState: 'blocked',
      retryableDeadDeliveries: 0,
      scheduledDeletes: 0,
      tenantName: 'Tenant One',
    })

    expect(drill.totals).toEqual({ attention: 0, blocked: 7, ready: 0, total: 7 })
  })

  it('blocks failed readiness and recovery drills while falling back for blank tenant names', () => {
    const drill = buildReplayDrill({
      activeGdpr: 0,
      dashboardHref: '/d/tenant',
      deadDeliveryCount: 0,
      inflightDeadDeliveries: 0,
      queuedGdpr: 0,
      readinessStatus: 'fail',
      recoveryStatus: 'fail',
      releaseLifecycleState: 'supported',
      retryableDeadDeliveries: 0,
      scheduledDeletes: 0,
      tenantName: ' ',
    })

    expect(drill.totals).toEqual({ attention: 0, blocked: 3, ready: 4, total: 7 })
    expect(drill.lanes.find((lane) => lane.key === 'ingest_service')).toMatchObject({
      entryHref: '/feedback',
      status: 'blocked',
    })
    expect(drill.lanes.find((lane) => lane.key === 'gdpr_job')).toMatchObject({
      status: 'blocked',
    })
  })
})
