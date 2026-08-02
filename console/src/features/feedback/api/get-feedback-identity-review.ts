import { queryOptions, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  FeedbackIdentitySubjectDetail,
  GetFeedbackIdentityReviewResponse,
  MergeFeedbackIdentityReviewRequest,
  MergeFeedbackIdentityReviewResponse,
  SplitFeedbackIdentityReviewRequest,
  SplitFeedbackIdentityReviewResponse,
} from '@/proto/attune/v1/ingest'

export type FeedbackIdentityReview = GetFeedbackIdentityReviewResponse
export type FeedbackIdentityMergeRequest = MergeFeedbackIdentityReviewRequest
export type FeedbackIdentityMergeResponse = MergeFeedbackIdentityReviewResponse
export type FeedbackIdentitySplitRequest = SplitFeedbackIdentityReviewRequest
export type FeedbackIdentitySplitResponse = SplitFeedbackIdentityReviewResponse
export type FeedbackIdentitySubject = FeedbackIdentitySubjectDetail

export const feedbackIdentityReviewQuery = () =>
  queryOptions({
    queryKey: ['console', 'feedback', 'identity-review'],
    queryFn: ({ signal }) =>
      api<FeedbackIdentityReview>('/fb/v1/console/feedback/identity-review', { signal }),
    staleTime: 60_000,
  })

export const feedbackIdentitySubjectQuery = (subjectId: string) =>
  queryOptions({
    queryKey: ['console', 'feedback', 'identity-review', 'subjects', subjectId],
    queryFn: ({ signal }) =>
      api<FeedbackIdentitySubject>(
        `/fb/v1/console/feedback/identity-review/subjects/${encodeURIComponent(subjectId)}`,
        { signal },
      ),
    enabled: subjectId.length > 0,
    staleTime: 60_000,
  })

export function useMergeFeedbackIdentityReview() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: FeedbackIdentityMergeRequest) =>
      api<FeedbackIdentityMergeResponse>('/fb/v1/console/feedback/identity-review/merge', {
        method: 'POST',
        body,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['console', 'feedback', 'identity-review'] })
    },
  })
}

export function useSplitFeedbackIdentityReview() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (body: FeedbackIdentitySplitRequest) =>
      api<FeedbackIdentitySplitResponse>('/fb/v1/console/feedback/identity-review/split', {
        method: 'POST',
        body,
      }),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ['console', 'feedback', 'identity-review'] })
    },
  })
}
