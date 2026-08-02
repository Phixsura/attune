import type { SurveyAnalytics } from '@/proto/attune/v1/survey'

export function recoveryReadinessScore(analytics?: SurveyAnalytics) {
  if (!analytics || analytics.openLowScoreReviewCount === 0) return 100
  const penalty =
    analytics.overdueLowScoreReviewCount * 22 +
    analytics.criticalLowScoreReviewCount * 18 +
    analytics.unassignedLowScoreReviewCount * 14 +
    analytics.pendingCustomerContactReviewCount * 10 +
    analytics.missingRootCauseRecoveryQueueCount * 7 +
    analytics.missingActionRecoveryQueueCount * 5
  return Math.max(0, 100 - penalty)
}
