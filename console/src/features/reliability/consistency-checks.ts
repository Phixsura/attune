import type { CustomerRequestSummary } from '@/proto/attune/v1/customer_request'
import type { GetFeedbackStatsResponse } from '@/proto/attune/v1/ingest'
import type { RequestNotificationStatusEvidenceItem } from '@/proto/attune/v1/request_notification'
import type { SurveyAnalytics } from '@/proto/attune/v1/survey'
import type { GetUsageResponse } from '@/proto/attune/v1/usage'

export type ConsistencyCheckStatus = 'verified' | 'watch' | 'blocked' | 'needs_data'

export type ConsistencyCheckLaneKey =
  | 'ingest_feedback'
  | 'feedback_request'
  | 'request_notification'
  | 'notification_survey'
  | 'survey_recovery'

export type ConsistencyCheckLane = {
  actionHref: string
  actionLabel: string
  evidence: string
  guardrail: string
  key: ConsistencyCheckLaneKey
  owner: string
  signal: string
  status: ConsistencyCheckStatus
  title: string
}

export type ConsistencyChecks = {
  fingerprint: string
  lanes: ConsistencyCheckLane[]
  summary: string
  totals: Record<ConsistencyCheckStatus, number> & {
    total: number
  }
}

export type ConsistencyChecksInput = {
  customerRequests?: CustomerRequestSummary[]
  dashboardHref: string
  feedbackHref: string
  feedbackStats?: GetFeedbackStatsResponse
  notificationEvidence?: RequestNotificationStatusEvidenceItem[]
  notificationHref: string
  surveyAnalytics?: SurveyAnalytics
  surveyHref: string
  tenantName: string
  usage?: GetUsageResponse
}

export function buildConsistencyChecks(input: ConsistencyChecksInput): ConsistencyChecks {
  const lanes = [
    ingestFeedbackLane(input),
    feedbackRequestLane(input),
    requestNotificationLane(input),
    notificationSurveyLane(input),
    surveyRecoveryLane(input),
  ]
  const totals = {
    blocked: lanes.filter((lane) => lane.status === 'blocked').length,
    needs_data: lanes.filter((lane) => lane.status === 'needs_data').length,
    total: lanes.length,
    verified: lanes.filter((lane) => lane.status === 'verified').length,
    watch: lanes.filter((lane) => lane.status === 'watch').length,
  }

  return {
    fingerprint: `${input.tenantName || 'tenant unknown'} / ${formatNumber(
      parseCount(input.feedbackStats?.total) ?? 0,
    )} feedback / ${formatNumber(input.customerRequests?.length ?? 0)} requests / ${formatNumber(
      input.surveyAnalytics?.completedCount ?? 0,
    )} survey completions`,
    lanes,
    summary: consistencySummary(totals),
    totals,
  }
}

function ingestFeedbackLane(input: ConsistencyChecksInput): ConsistencyCheckLane {
  const ingested = parseCount(input.usage?.total)
  const feedbackTotal = parseCount(input.feedbackStats?.total)
  const usageBuckets = input.usage?.series.length

  return {
    actionHref: input.feedbackHref,
    actionLabel: 'Open feedback records',
    evidence:
      input.usage && input.feedbackStats
        ? `${input.usage.periodStart || 'unknown'} -> ${
            input.usage.periodEnd || 'unknown'
          } / feedback ${input.feedbackStats.periodStart || 'unknown'} -> ${
            input.feedbackStats.periodEnd || 'unknown'
          }`
        : 'usage or feedback aggregate is missing',
    guardrail:
      'Ingest activity and feedback aggregate evidence must both be present before downstream projections are trusted.',
    key: 'ingest_feedback',
    owner: 'Data Pipeline',
    signal:
      ingested !== undefined && feedbackTotal !== undefined && usageBuckets !== undefined
        ? `${formatNumber(ingested)} ingested / ${formatNumber(
            feedbackTotal,
          )} feedback records / ${usageBuckets} usage buckets`
        : 'ingest to feedback evidence missing',
    status: ingestFeedbackStatus(ingested, feedbackTotal, usageBuckets),
    title: 'Ingest -> feedback',
  }
}

function feedbackRequestLane(input: ConsistencyChecksInput): ConsistencyCheckLane {
  const requests = input.customerRequests
  const feedbackTotal = parseCount(input.feedbackStats?.total)
  const supporting = requests?.reduce((sum, request) => sum + request.supportingFeedbackCount, 0)
  const hidden = requests?.reduce((sum, request) => sum + request.hiddenFeedbackCount, 0)
  const orphaned = requests?.filter((request) => request.supportingFeedbackCount === 0).length
  const issueProblems = requests?.reduce(
    (sum, request) => sum + request.failedIssueCount + request.staleIssueCount,
    0,
  )

  return {
    actionHref: '/feedback/customer-requests',
    actionLabel: 'Open customer requests',
    /* v8 ignore next -- @preserve: present request projections produce numeric reduce totals; fallback is malformed-fixture defense. */
    evidence: requests
      ? `${requests.length} request rows / ${formatNumber(
          supporting ?? 0,
        )} supporting feedback / ${formatNumber(hidden ?? 0)} hidden feedback`
      : 'customer request projection is missing',
    guardrail:
      'Every request projection should retain supporting feedback and expose stale or failed issue links.',
    key: 'feedback_request',
    owner: 'Product Ops',
    /* v8 ignore next -- @preserve: present request projections produce numeric reduce totals; fallback is malformed-fixture defense. */
    signal:
      requests && feedbackTotal !== undefined
        ? `${formatNumber(supporting ?? 0)} supporting feedback / ${formatNumber(
            requests.length,
          )} requests / ${formatNumber(orphaned ?? 0)} orphaned requests`
        : 'feedback to request evidence missing',
    status: feedbackRequestStatus(
      feedbackTotal,
      requests?.length,
      supporting,
      hidden,
      orphaned,
      issueProblems,
    ),
    title: 'Feedback -> request',
  }
}

function requestNotificationLane(input: ConsistencyChecksInput): ConsistencyCheckLane {
  const evidence = input.notificationEvidence
  const expected = sumNotification(evidence, 'expectedCustomers')
  const notified = sumNotification(evidence, 'notifiedCustomers')
  const failed = sumNotification(evidence, 'failedCustomers')
  const recoveryPending = sumNotification(evidence, 'recoveryPendingCustomers')
  const eventCount = sumNotification(evidence, 'eventCount')

  return {
    actionHref: input.notificationHref,
    actionLabel: 'Open notification evidence',
    evidence: evidence
      ? `${formatNumber(eventCount)} events / ${formatNumber(failed)} failed / ${formatNumber(
          recoveryPending,
        )} recovery pending`
      : 'request notification status evidence is missing',
    guardrail:
      'Request state changes should retain expected, notified, failed, and recovery-pending customer counts.',
    key: 'request_notification',
    owner: 'Customer Success',
    signal: evidence
      ? `${formatNumber(notified)} notified / ${formatNumber(expected)} expected / ${formatNumber(
          evidence.length,
        )} request statuses`
      : 'request to notification evidence missing',
    status: requestNotificationStatus(evidence, expected, notified, failed, recoveryPending),
    title: 'Request -> notification',
  }
}

function notificationSurveyLane(input: ConsistencyChecksInput): ConsistencyCheckLane {
  const survey = input.surveyAnalytics
  const notified = sumNotification(input.notificationEvidence, 'notifiedCustomers')
  const expected = sumNotification(input.notificationEvidence, 'expectedCustomers')
  const hasNotification = Boolean(input.notificationEvidence)

  return {
    actionHref: input.surveyHref,
    actionLabel: 'Open survey analytics',
    evidence: survey
      ? `${formatNumber(survey.invitationCount)} invitations / ${formatNumber(
          survey.suppressedCount,
        )} suppressed / ${formatNumber(survey.expiredCount)} expired`
      : 'survey analytics are missing',
    guardrail:
      'Post-resolution surveys should not exceed upstream notification evidence or complete more responses than were delivered.',
    key: 'notification_survey',
    owner: 'Customer Success',
    signal: survey
      ? `${formatNumber(survey.deliveredCount)} delivered surveys / ${formatNumber(
          survey.completedCount,
        )} completed / ${formatNumber(notified)} notified customers`
      : 'notification to survey evidence missing',
    status: notificationSurveyStatus(survey, hasNotification, expected, notified),
    title: 'Notification -> survey',
  }
}

function surveyRecoveryLane(input: ConsistencyChecksInput): ConsistencyCheckLane {
  const survey = input.surveyAnalytics

  return {
    actionHref: input.surveyHref,
    actionLabel: 'Open recovery queue',
    evidence: survey
      ? `${formatNumber(survey.unassignedLowScoreReviewCount)} unassigned / ${formatNumber(
          survey.pendingCustomerContactReviewCount,
        )} pending contact / oldest due=${survey.oldestOpenLowScoreReviewDueAt || 'unknown'}`
      : 'survey recovery analytics are missing',
    guardrail:
      'Every low-score survey should keep a recovery review, owner, SLA, and customer-contact state.',
    key: 'survey_recovery',
    owner: 'Customer Success',
    signal: survey
      ? `${formatNumber(survey.lowScoreCount)} low-score / ${formatNumber(
          survey.openLowScoreReviewCount,
        )} open reviews / ${formatNumber(survey.overdueLowScoreReviewCount)} overdue`
      : 'survey recovery evidence missing',
    status: surveyRecoveryStatus(survey),
    title: 'Survey recovery queue',
  }
}

function ingestFeedbackStatus(
  ingested: number | undefined,
  feedbackTotal: number | undefined,
  usageBuckets: number | undefined,
): ConsistencyCheckStatus {
  if (ingested === undefined || feedbackTotal === undefined || usageBuckets === undefined) {
    return 'needs_data'
  }
  if (feedbackTotal > ingested) return 'blocked'
  if (ingested > 0 && feedbackTotal === 0) return 'watch'
  if (usageBuckets === 0) return 'watch'
  return 'verified'
}

function feedbackRequestStatus(
  feedbackTotal: number | undefined,
  requestCount: number | undefined,
  supporting: number | undefined,
  hidden: number | undefined,
  orphaned: number | undefined,
  issueProblems: number | undefined,
): ConsistencyCheckStatus {
  if (
    feedbackTotal === undefined ||
    requestCount === undefined ||
    supporting === undefined ||
    hidden === undefined ||
    orphaned === undefined ||
    issueProblems === undefined
  ) {
    return 'needs_data'
  }
  if (orphaned > 0 || issueProblems > 0) return 'blocked'
  if (feedbackTotal > 0 && requestCount === 0) return 'watch'
  if (hidden > supporting) return 'watch'
  return supporting > 0 ? 'verified' : 'needs_data'
}

function requestNotificationStatus(
  evidence: RequestNotificationStatusEvidenceItem[] | undefined,
  expected: number,
  notified: number,
  failed: number,
  recoveryPending: number,
): ConsistencyCheckStatus {
  if (!evidence) return 'needs_data'
  if (expected > 0 && notified + failed + recoveryPending === 0) return 'blocked'
  if (notified > expected) return 'blocked'
  if (failed > 0 || recoveryPending > 0) return 'watch'
  return evidence.length > 0 ? 'verified' : 'needs_data'
}

function notificationSurveyStatus(
  survey: SurveyAnalytics | undefined,
  hasNotification: boolean,
  expected: number,
  notified: number,
): ConsistencyCheckStatus {
  if (!survey || !hasNotification) return 'needs_data'
  if (survey.completedCount > survey.deliveredCount) return 'blocked'
  if (expected > 0 && survey.invitationCount > expected) return 'watch'
  if (notified > 0 && survey.deliveredCount === 0) return 'watch'
  if (survey.rejectedDeliveryCount > 0 || survey.delayedDeliveryCount > 0) return 'watch'
  return 'verified'
}

function surveyRecoveryStatus(survey: SurveyAnalytics | undefined): ConsistencyCheckStatus {
  if (!survey) return 'needs_data'
  if (survey.lowScoreCount > 0 && survey.openLowScoreReviewCount === 0) return 'blocked'
  if (
    survey.overdueLowScoreReviewCount > 0 ||
    survey.unassignedLowScoreReviewCount > 0 ||
    survey.pendingCustomerContactReviewCount > 0
  ) {
    return 'watch'
  }
  return 'verified'
}

function consistencySummary(totals: Record<ConsistencyCheckStatus, number> & { total: number }) {
  if (totals.blocked > 0) {
    return `${totals.blocked} consistency checks are blocked`
  }
  if (totals.needs_data > 0) {
    return `${totals.needs_data} consistency checks need evidence`
  }
  if (totals.watch > 0) {
    return `${totals.watch} consistency checks need attention`
  }
  return 'data consistency evidence is verified'
}

function sumNotification(
  evidence: RequestNotificationStatusEvidenceItem[] | undefined,
  key:
    | 'eventCount'
    | 'expectedCustomers'
    | 'failedCustomers'
    | 'notifiedCustomers'
    | 'recoveryPendingCustomers',
) {
  return evidence?.reduce((sum, item) => sum + item[key], 0) ?? 0
}

function parseCount(value: string | number | undefined): number | undefined {
  if (value === undefined) return undefined
  const parsed = typeof value === 'number' ? value : Number.parseInt(value, 10)
  return Number.isFinite(parsed) ? parsed : undefined
}

function formatNumber(value: number) {
  return new Intl.NumberFormat('en-US').format(value)
}
