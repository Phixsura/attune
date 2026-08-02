import { describe, expect, it } from 'vitest'
import {
  CustomerRequestDeliveryHealth,
  CustomerRequestPriority,
  CustomerRequestStatus,
} from '@/proto/attune/v1/customer_request'
import { buildConsistencyChecks, type ConsistencyChecksInput } from './consistency-checks'

type CompleteConsistencyChecksInput = ConsistencyChecksInput &
  Required<
    Pick<
      ConsistencyChecksInput,
      'customerRequests' | 'feedbackStats' | 'notificationEvidence' | 'surveyAnalytics' | 'usage'
    >
  >

function completeInput(): CompleteConsistencyChecksInput {
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
    feedbackHref: '/feedback',
    feedbackStats: {
      periodStart: '2026-07-01T00:00:00Z',
      periodEnd: '2026-07-31T23:59:59Z',
      total: '2',
      urgentCount: '0',
      dims: [],
    },
    notificationEvidence: [
      {
        requestStatus: 'shipped',
        expectedCustomers: 4,
        notifiedCustomers: 4,
        failedCustomers: 0,
        suppressedCustomers: 0,
        recoveryPendingCustomers: 0,
        eventCount: 4,
        lastEventAt: '2026-07-16T00:00:00Z',
      },
    ],
    notificationHref: '/integrations/request-notifications',
    surveyAnalytics: {
      invitationCount: 4,
      deliveredCount: 4,
      suppressedCount: 0,
      completedCount: 2,
      lowScoreCount: 0,
      averageScore: 4.5,
      responseRate: 0.5,
      scoreDistribution: [],
      suppressionReasonDistribution: [],
      averageResponseSeconds: 1200,
      positiveScoreCount: 2,
      positiveScoreRate: 1,
      openLowScoreReviewCount: 0,
      overdueLowScoreReviewCount: 0,
      notStartedCount: 0,
      openedCount: 2,
      expiredCount: 0,
      unassignedLowScoreReviewCount: 0,
      criticalLowScoreReviewCount: 0,
      pendingCustomerContactReviewCount: 0,
      overdueRecoveryQueueCount: 0,
      unassignedRecoveryQueueCount: 0,
      pendingContactRecoveryQueueCount: 0,
      missingRootCauseRecoveryQueueCount: 0,
      missingActionRecoveryQueueCount: 0,
      ownerRecoveryLoads: [],
      pendingDeliveryCount: 0,
      delayedDeliveryCount: 0,
      rejectedDeliveryCount: 0,
    },
    surveyHref: '/integrations/surveys',
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

describe('buildConsistencyChecks', () => {
  it('verifies the ingest, feedback, request, notification, survey, and recovery chain', () => {
    const checks = buildConsistencyChecks(completeInput())

    expect(checks.lanes).toHaveLength(5)
    expect(checks.fingerprint).toBe('Tenant One / 2 feedback / 1 requests / 2 survey completions')
    expect(checks.summary).toBe('data consistency evidence is verified')
    expect(checks.totals).toEqual({
      blocked: 0,
      needs_data: 0,
      total: 5,
      verified: 5,
      watch: 0,
    })
    expect(checks.lanes.find((lane) => lane.key === 'ingest_feedback')).toMatchObject({
      signal: '72 ingested / 2 feedback records / 1 usage buckets',
      status: 'verified',
    })
    expect(checks.lanes.find((lane) => lane.key === 'feedback_request')).toMatchObject({
      signal: '2 supporting feedback / 1 requests / 0 orphaned requests',
      status: 'verified',
    })
    expect(checks.lanes.find((lane) => lane.key === 'request_notification')).toMatchObject({
      signal: '4 notified / 4 expected / 1 request statuses',
      status: 'verified',
    })
  })

  it('surfaces orphaned requests, failed notification evidence, and survey recovery pressure', () => {
    const base = completeInput()
    const request = base.customerRequests?.[0]
    if (!request || !base.surveyAnalytics) {
      throw new Error('completeInput must include request and survey fixtures')
    }
    const checks = buildConsistencyChecks({
      ...base,
      customerRequests: [
        {
          ...request,
          failedIssueCount: 1,
          supportingFeedbackCount: 0,
        },
      ],
      notificationEvidence: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 4,
          notifiedCustomers: 2,
          failedCustomers: 1,
          suppressedCustomers: 0,
          recoveryPendingCustomers: 1,
          eventCount: 4,
          lastEventAt: '2026-07-16T00:00:00Z',
        },
      ],
      surveyAnalytics: {
        ...base.surveyAnalytics,
        lowScoreCount: 1,
        openLowScoreReviewCount: 1,
        overdueLowScoreReviewCount: 1,
        unassignedLowScoreReviewCount: 1,
        pendingCustomerContactReviewCount: 1,
      },
    })

    expect(checks.summary).toBe('1 consistency checks are blocked')
    expect(checks.totals).toEqual({
      blocked: 1,
      needs_data: 0,
      total: 5,
      verified: 2,
      watch: 2,
    })
    expect(checks.lanes.find((lane) => lane.key === 'feedback_request')).toMatchObject({
      status: 'blocked',
    })
    expect(checks.lanes.find((lane) => lane.key === 'request_notification')).toMatchObject({
      signal: '2 notified / 4 expected / 1 request statuses',
      status: 'watch',
    })
    expect(checks.lanes.find((lane) => lane.key === 'survey_recovery')).toMatchObject({
      signal: '1 low-score / 1 open reviews / 1 overdue',
      status: 'watch',
    })
  })

  it('keeps missing consistency evidence visible as data gaps', () => {
    const checks = buildConsistencyChecks({
      dashboardHref: '/d/tenant',
      feedbackHref: '/feedback',
      notificationHref: '/integrations/request-notifications',
      surveyHref: '/integrations/surveys',
      tenantName: 'Tenant One',
    })

    expect(checks.fingerprint).toBe('Tenant One / 0 feedback / 0 requests / 0 survey completions')
    expect(checks.summary).toBe('5 consistency checks need evidence')
    expect(checks.totals).toEqual({
      blocked: 0,
      needs_data: 5,
      total: 5,
      verified: 0,
      watch: 0,
    })
  })

  it('blocks impossible downstream counts across ingest, notification, survey, and recovery', () => {
    const base = completeInput()
    const checks = buildConsistencyChecks({
      ...base,
      feedbackStats: { ...base.feedbackStats, total: '200' },
      notificationEvidence: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 4,
          notifiedCustomers: 5,
          failedCustomers: 0,
          suppressedCustomers: 0,
          recoveryPendingCustomers: 0,
          eventCount: 5,
          lastEventAt: '2026-07-16T00:00:00Z',
        },
      ],
      surveyAnalytics: {
        ...base.surveyAnalytics,
        completedCount: 5,
        deliveredCount: 4,
        lowScoreCount: 1,
        openLowScoreReviewCount: 0,
      },
    })

    expect(checks.summary).toBe('4 consistency checks are blocked')
    expect(checks.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['ingest_feedback', 'blocked'],
      ['feedback_request', 'verified'],
      ['request_notification', 'blocked'],
      ['notification_survey', 'blocked'],
      ['survey_recovery', 'blocked'],
    ])
  })

  it('watches sparse but recoverable consistency signals', () => {
    const base = completeInput()
    const request = base.customerRequests?.[0]
    if (!request || !base.surveyAnalytics) {
      throw new Error('completeInput must include request and survey fixtures')
    }

    const checks = buildConsistencyChecks({
      ...base,
      customerRequests: [
        {
          ...request,
          hiddenFeedbackCount: 3,
          supportingFeedbackCount: 2,
        },
      ],
      feedbackStats: { ...base.feedbackStats, total: '0' },
      notificationEvidence: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 2,
          notifiedCustomers: 1,
          failedCustomers: 0,
          suppressedCustomers: 0,
          recoveryPendingCustomers: 0,
          eventCount: 1,
          lastEventAt: '2026-07-16T00:00:00Z',
        },
      ],
      surveyAnalytics: {
        ...base.surveyAnalytics,
        completedCount: 0,
        delayedDeliveryCount: 1,
        deliveredCount: 0,
        invitationCount: 3,
        rejectedDeliveryCount: 1,
      },
      usage: { ...base.usage, series: [] },
    })

    expect(checks.summary).toBe('3 consistency checks need attention')
    expect(checks.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['ingest_feedback', 'watch'],
      ['feedback_request', 'watch'],
      ['request_notification', 'verified'],
      ['notification_survey', 'watch'],
      ['survey_recovery', 'verified'],
    ])
  })

  it('treats empty or unparsable evidence arrays as needs-data instead of success', () => {
    const base = completeInput()
    const checks = buildConsistencyChecks({
      ...base,
      customerRequests: [],
      feedbackStats: {
        ...base.feedbackStats,
        periodEnd: '',
        periodStart: '',
        total: 'not-a-number',
      },
      notificationEvidence: [],
      tenantName: '',
      usage: { ...base.usage, periodEnd: '', periodStart: '', total: 'not-a-number' },
    })

    expect(checks.fingerprint).toBe(
      'tenant unknown / 0 feedback / 0 requests / 2 survey completions',
    )
    expect(checks.summary).toBe('3 consistency checks need evidence')
    expect(checks.lanes.map((lane) => [lane.key, lane.status])).toEqual([
      ['ingest_feedback', 'needs_data'],
      ['feedback_request', 'needs_data'],
      ['request_notification', 'needs_data'],
      ['notification_survey', 'verified'],
      ['survey_recovery', 'verified'],
    ])
  })

  it('blocks request notification evidence when expected customers have no outcomes', () => {
    const base = completeInput()
    const checks = buildConsistencyChecks({
      ...base,
      notificationEvidence: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 4,
          notifiedCustomers: 0,
          failedCustomers: 0,
          suppressedCustomers: 0,
          recoveryPendingCustomers: 0,
          eventCount: 1,
          lastEventAt: '2026-07-16T00:00:00Z',
        },
      ],
      surveyAnalytics: {
        ...base.surveyAnalytics,
        completedCount: 0,
        deliveredCount: 0,
        invitationCount: 4,
      },
    })

    expect(checks.summary).toBe('1 consistency checks are blocked')
    expect(checks.lanes.find((lane) => lane.key === 'request_notification')).toMatchObject({
      signal: '0 notified / 4 expected / 1 request statuses',
      status: 'blocked',
    })
  })

  it('watches survey delivery gaps after notification evidence exists', () => {
    const base = completeInput()
    const noDelivered = buildConsistencyChecks({
      ...base,
      notificationEvidence: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 0,
          notifiedCustomers: 1,
          failedCustomers: 0,
          suppressedCustomers: 0,
          recoveryPendingCustomers: 0,
          eventCount: 1,
          lastEventAt: '2026-07-16T00:00:00Z',
        },
      ],
      surveyAnalytics: base.surveyAnalytics
        ? {
            ...base.surveyAnalytics,
            completedCount: 0,
            deliveredCount: 0,
            invitationCount: 0,
          }
        : undefined,
    })
    expect(noDelivered.lanes.find((lane) => lane.key === 'notification_survey')).toMatchObject({
      signal: '0 delivered surveys / 0 completed / 1 notified customers',
      status: 'watch',
    })

    const delayed = buildConsistencyChecks({
      ...base,
      notificationEvidence: [
        {
          requestStatus: 'shipped',
          expectedCustomers: 0,
          notifiedCustomers: 0,
          failedCustomers: 0,
          suppressedCustomers: 0,
          recoveryPendingCustomers: 0,
          eventCount: 0,
          lastEventAt: '',
        },
      ],
      surveyAnalytics: base.surveyAnalytics
        ? {
            ...base.surveyAnalytics,
            delayedDeliveryCount: 1,
            rejectedDeliveryCount: 1,
          }
        : undefined,
    })
    expect(delayed.lanes.find((lane) => lane.key === 'notification_survey')).toMatchObject({
      status: 'watch',
    })
  })

  it('accepts numeric aggregate counts from typed API clients', () => {
    const base = completeInput()
    const checks = buildConsistencyChecks({
      ...base,
      feedbackStats: base.feedbackStats ? { ...base.feedbackStats, total: 2 as never } : undefined,
      usage: base.usage ? { ...base.usage, total: 72 as never } : undefined,
    })

    expect(checks.summary).toBe('data consistency evidence is verified')
    expect(checks.lanes.find((lane) => lane.key === 'ingest_feedback')).toMatchObject({
      signal: '72 ingested / 2 feedback records / 1 usage buckets',
      status: 'verified',
    })
  })

  it('separates empty usage buckets from empty request projections', () => {
    const base = completeInput()
    const emptyBuckets = buildConsistencyChecks({
      ...base,
      usage: base.usage ? { ...base.usage, series: [] } : undefined,
    })
    expect(emptyBuckets.lanes.find((lane) => lane.key === 'ingest_feedback')).toMatchObject({
      signal: '72 ingested / 2 feedback records / 0 usage buckets',
      status: 'watch',
    })

    const missingRequests = buildConsistencyChecks({
      ...base,
      customerRequests: [],
      feedbackStats: base.feedbackStats ? { ...base.feedbackStats, total: '2' } : undefined,
    })
    expect(missingRequests.lanes.find((lane) => lane.key === 'feedback_request')).toMatchObject({
      signal: '0 supporting feedback / 0 requests / 0 orphaned requests',
      status: 'watch',
    })

    const emptyProjection = buildConsistencyChecks({
      ...base,
      customerRequests: [],
      feedbackStats: base.feedbackStats ? { ...base.feedbackStats, total: '0' } : undefined,
    })
    expect(emptyProjection.lanes.find((lane) => lane.key === 'feedback_request')).toMatchObject({
      signal: '0 supporting feedback / 0 requests / 0 orphaned requests',
      status: 'needs_data',
    })
  })
})
