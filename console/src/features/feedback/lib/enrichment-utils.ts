import type { FeedbackDetail } from '@/features/feedback/api/get-feedback-detail'
import type { Feedback } from '@/features/feedback/api/list-feedback-infinite'

export const MAX_ENRICHMENT_ATTEMPTS = 5

export function isTerminalFailure(
  item: Pick<
    Feedback | FeedbackDetail,
    'enrichmentStatus' | 'enrichmentAttempts' | 'enrichmentNextRetryAt'
  >,
): boolean {
  return (
    item.enrichmentStatus === 'failed' &&
    (item.enrichmentAttempts ?? 0) >= MAX_ENRICHMENT_ATTEMPTS &&
    !item.enrichmentNextRetryAt
  )
}
