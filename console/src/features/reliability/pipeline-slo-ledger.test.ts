import { describe, expect, it } from 'vitest'
import {
  CustomerRequestDeliveryHealth,
  CustomerRequestPriority,
  CustomerRequestStatus,
} from '@/proto/attune/v1/customer_request'
import type { PipelineSloLedgerInput } from './pipeline-slo-ledger'
import { buildPipelineSloLedger } from './pipeline-slo-ledger'

type CompletePipelineSloLedgerInput = PipelineSloLedgerInput &
  Required<Pick<PipelineSloLedgerInput, 'customerRequests' | 'llmUsage' | 'usage'>>

function completeInput(): CompletePipelineSloLedgerInput {
  return {
    customerRequests: [
      {
        id: 'req-1',
        displayId: 'CR-1',
        displayNumber: '1',
        title: 'Keyboard focus restore',
        status: CustomerRequestStatus.CUSTOMER_REQUEST_STATUS_OPEN,
        priority: CustomerRequestPriority.CUSTOMER_REQUEST_PRIORITY_HIGH,
        supportingFeedbackCount: 2,
        customerCount: 1,
        linkedIssueCount: 1,
        hiddenFeedbackCount: 0,
        firstFeedbackAt: '2026-07-01T00:00:00Z',
        latestFeedbackAt: '2026-07-02T00:00:00Z',
        createdAt: '2026-07-01T00:00:00Z',
        updatedAt: '2026-07-02T00:00:00Z',
        accountCount: 1,
        voteCount: 1,
        duplicateRequestCount: 0,
        revenueImpactCents: '100000',
        revenueCurrency: 'USD',
        decisionScore: 42,
        decisionScoreExplanation: 'feedback=2 delivery_health=synced',
        deliveryHealth: CustomerRequestDeliveryHealth.CUSTOMER_REQUEST_DELIVERY_HEALTH_SYNCED,
        syncedIssueCount: 1,
        staleIssueCount: 0,
        failedIssueCount: 0,
        pendingIssueCount: 0,
        manualIssueCount: 0,
        decisionScoreFactors: [],
      },
    ],
    dashboardHref: '/d/tenant',
    deadDeliveryCount: 0,
    feedbackHref: '/feedback',
    inflightDeadDeliveries: 0,
    llmUsage: {
      periodStart: '2026-07-01T00:00:00Z',
      periodEnd: '2026-07-31T23:59:59Z',
      granularity: 'week',
      series: [
        {
          bucket: '2026-07-01T00:00:00Z',
          tenantId: 'tenant-1',
          modelId: 'gpt-5-mini',
          promptTokens: '1000',
          completionTokens: '500',
          costUsd: 1.23,
          calls: '20',
          errors: '0',
        },
      ],
      promptTokens: '1000',
      completionTokens: '500',
      costUsd: 1.23,
      calls: '20',
      errors: '0',
    },
    preflightChecks: [
      { name: 'database', category: 'database', status: 'pass', message: 'ok' },
      { name: 'migrations', category: 'migration', status: 'pass', message: 'ok' },
      { name: 'worker', category: 'worker', status: 'pass', message: 'ok' },
      { name: 'metrics', category: 'metrics', status: 'pass', message: 'ok' },
    ],
    readinessStatus: 'pass',
    releaseLifecycleState: 'supported',
    retryableDeadDeliveries: 0,
    tenantName: 'Tenant One',
    usage: {
      periodStart: '2026-07-01T00:00:00Z',
      periodEnd: '2026-07-31T23:59:59Z',
      total: '72',
      quota: '100',
      series: [{ bucket: '2026-07-01T00:00:00Z', value: '72' }],
    },
  }
}

describe('buildPipelineSloLedger', () => {
  it('keeps ingest, enrich, outbox, and sync SLO evidence ready', () => {
    const ledger = buildPipelineSloLedger(completeInput())

    expect(ledger.fingerprint).toBe('Tenant One / 72 ingested / 20 enrich calls / 1 sync rows')
    expect(ledger.summary).toBe('pipeline SLO evidence is ready')
    expect(ledger.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      ready: 4,
      total: 4,
      watch: 0,
    })
    expect(ledger.lanes.find((lane) => lane.key === 'ingest')).toMatchObject({
      burnSignal: '72 ingested / 1 buckets / 72.0% quota used',
      status: 'ready',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'sync')).toMatchObject({
      burnSignal: '1 synced / 0 stale / 0 failed / 0 pending / 0 manual',
      status: 'ready',
    })
  })

  it('surfaces enrichment, outbox, and sync pressure without hiding release gates', () => {
    const base = completeInput()
    const request = base.customerRequests?.[0]
    if (!request || !base.llmUsage) {
      throw new Error('completeInput must include request and LLM usage fixtures')
    }
    const ledger = buildPipelineSloLedger({
      ...base,
      customerRequests: [
        {
          ...request,
          failedIssueCount: 1,
          syncedIssueCount: 0,
        },
      ],
      deadDeliveryCount: 2,
      inflightDeadDeliveries: 1,
      llmUsage: {
        ...base.llmUsage,
        calls: '20',
        errors: '1',
        series: [{ ...base.llmUsage.series[0], errors: '1' }],
      },
      retryableDeadDeliveries: 1,
    })

    expect(ledger.summary).toBe('1 pipeline SLOs are blocked')
    expect(ledger.totals).toEqual({
      blocked: 1,
      needs_data: 0,
      ready: 1,
      total: 4,
      watch: 2,
    })
    expect(ledger.lanes.find((lane) => lane.key === 'enrich')).toMatchObject({
      burnSignal: '20 calls / 1 errors / 5.0% error rate',
      status: 'watch',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'outbox')).toMatchObject({
      burnSignal: '1 retryable / 1 in-flight / 2 dead',
      status: 'watch',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'sync')).toMatchObject({
      status: 'blocked',
    })
  })

  it('keeps missing pipeline evidence visible as data gaps', () => {
    const ledger = buildPipelineSloLedger({
      dashboardHref: '/d/tenant',
      feedbackHref: '/feedback',
      tenantName: '',
    })

    expect(ledger.fingerprint).toBe('tenant unknown / 0 ingested / 0 enrich calls / 0 sync rows')
    expect(ledger.summary).toBe('4 pipeline SLOs need evidence')
    expect(ledger.totals).toEqual({
      blocked: 0,
      needs_data: 4,
      ready: 0,
      total: 4,
      watch: 0,
    })
  })

  it('blocks every pipeline SLO behind a blocked release lifecycle gate', () => {
    const ledger = buildPipelineSloLedger({
      ...completeInput(),
      releaseLifecycleState: 'blocked',
    })

    expect(ledger.summary).toBe('4 pipeline SLOs are blocked')
    expect(ledger.totals).toEqual({
      blocked: 4,
      needs_data: 0,
      ready: 0,
      total: 4,
      watch: 0,
    })
    expect(ledger.lanes.every((lane) => lane.releaseGate.endsWith('/ release blocked'))).toBe(true)
  })

  it('blocks ingest when readiness fails', () => {
    const ledger = buildPipelineSloLedger({
      ...completeInput(),
      readinessStatus: 'fail',
    })

    expect(ledger.summary).toBe('1 pipeline SLOs are blocked')
    expect(ledger.lanes.find((lane) => lane.key === 'ingest')).toMatchObject({
      status: 'blocked',
    })
  })

  it('watches zero ingest and zero enrichment traffic with unknown burn rates', () => {
    const base = completeInput()
    const ledger = buildPipelineSloLedger({
      ...base,
      llmUsage: {
        ...base.llmUsage,
        calls: '0',
        errors: '0',
        series: [],
      },
      readinessStatus: 'warn',
      usage: {
        ...base.usage,
        quota: '0',
        series: [],
        total: '0',
      },
    })

    expect(ledger.summary).toBe('2 pipeline SLOs need attention')
    expect(ledger.lanes.find((lane) => lane.key === 'ingest')).toMatchObject({
      burnSignal: '0 ingested / 0 buckets / unknown quota used',
      status: 'watch',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'enrich')).toMatchObject({
      burnSignal: '0 calls / 0 errors / unknown error rate',
      status: 'watch',
    })
  })

  it('blocks enrichment fast-burn errors above the objective', () => {
    const base = completeInput()
    const ledger = buildPipelineSloLedger({
      ...base,
      llmUsage: {
        ...base.llmUsage,
        calls: '20',
        errors: '2',
      },
    })

    expect(ledger.summary).toBe('1 pipeline SLOs are blocked')
    expect(ledger.lanes.find((lane) => lane.key === 'enrich')).toMatchObject({
      burnSignal: '20 calls / 2 errors / 10.0% error rate',
      status: 'blocked',
    })
  })

  it('blocks dead letters when no retryable recovery path remains', () => {
    const ledger = buildPipelineSloLedger({
      ...completeInput(),
      deadDeliveryCount: 2,
      retryableDeadDeliveries: 0,
    })

    expect(ledger.summary).toBe('1 pipeline SLOs are blocked')
    expect(ledger.lanes.find((lane) => lane.key === 'outbox')).toMatchObject({
      burnSignal: '0 retryable / 0 in-flight / 2 dead',
      status: 'blocked',
    })
  })

  it('watches sync drift before it becomes a failed projection', () => {
    const base = completeInput()
    const request = base.customerRequests?.[0]
    if (!request) {
      throw new Error('completeInput must include request fixtures')
    }
    const ledger = buildPipelineSloLedger({
      ...base,
      customerRequests: [
        {
          ...request,
          pendingIssueCount: 1,
          staleIssueCount: 1,
          syncedIssueCount: 0,
        },
      ],
    })

    expect(ledger.summary).toBe('1 pipeline SLOs need attention')
    expect(ledger.lanes.find((lane) => lane.key === 'sync')).toMatchObject({
      burnSignal: '0 synced / 1 stale / 0 failed / 1 pending / 0 manual',
      status: 'watch',
    })
  })

  it('treats malformed counters as missing evidence', () => {
    const base = completeInput()
    const ledger = buildPipelineSloLedger({
      ...base,
      llmUsage: {
        ...base.llmUsage,
        calls: 'not-a-number',
      },
      usage: {
        ...base.usage,
        total: 'not-a-number',
      },
    })

    expect(ledger.fingerprint).toBe('Tenant One / 0 ingested / 0 enrich calls / 1 sync rows')
    expect(ledger.summary).toBe('2 pipeline SLOs need evidence')
    expect(ledger.lanes.find((lane) => lane.key === 'ingest')).toMatchObject({
      burnSignal: 'ingest burn signal missing',
      status: 'needs_data',
    })
    expect(ledger.lanes.find((lane) => lane.key === 'enrich')).toMatchObject({
      burnSignal: 'enrichment burn signal missing',
      status: 'needs_data',
    })
  })
})
