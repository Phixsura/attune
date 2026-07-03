import { type QueryClient, useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type {
  RegenerateReplyDraftResponse,
  ReplyDraftWorkflowResponse,
  SendReplyDraftResponse,
} from '@/proto/attune/v1/ingest'

const feedbackQueryKey = ['console', 'feedback'] as const
const replySendHookDeliveriesQueryKey = ['console', 'reply-send-hook', 'deliveries'] as const
const replySendHookHealthQueryKey = ['console', 'reply-send-hook', 'health'] as const

function invalidateFeedbackWorkflow(qc: QueryClient) {
  void qc.invalidateQueries({ queryKey: feedbackQueryKey })
}

// useRegenerateReplyDraft re-runs LLM reply-draft generation for one feedback
// row and returns the fresh draft. The mutation settles by invalidating feedback
// caches so the sheet, list, and counters re-read persisted workflow state. The
// draft is operator-facing only — it is never auto-sent.
export function useRegenerateReplyDraft(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      api<RegenerateReplyDraftResponse>(`/fb/v1/console/feedback/${id}/reply-draft/regenerate`, {
        method: 'POST',
        body: {},
      }),
    onSettled: () => {
      invalidateFeedbackWorkflow(qc)
    },
  })
}

export function useUpdateReplyDraft(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ content, expectedRevision }: { content: string; expectedRevision: string }) =>
      api<ReplyDraftWorkflowResponse>(`/fb/v1/console/feedback/${id}/reply-draft/edit`, {
        method: 'POST',
        body: { content, expectedRevision },
      }),
    onSettled: () => {
      invalidateFeedbackWorkflow(qc)
    },
  })
}

export function useApproveReplyDraft(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (expectedRevision: string) =>
      api<ReplyDraftWorkflowResponse>(`/fb/v1/console/feedback/${id}/reply-draft/approve`, {
        method: 'POST',
        body: { expectedRevision },
      }),
    onSettled: () => {
      invalidateFeedbackWorkflow(qc)
    },
  })
}

export function useRejectReplyDraft(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (expectedRevision: string) =>
      api<ReplyDraftWorkflowResponse>(`/fb/v1/console/feedback/${id}/reply-draft/reject`, {
        method: 'POST',
        body: { expectedRevision },
      }),
    onSettled: () => {
      invalidateFeedbackWorkflow(qc)
    },
  })
}

export function useSendReplyDraft(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (expectedRevision: string) => {
      const key = crypto.randomUUID()
      return api<SendReplyDraftResponse>(`/fb/v1/console/feedback/${id}/reply-draft/send`, {
        method: 'POST',
        headers: { 'Idempotency-Key': key },
        body: { expectedRevision },
      })
    },
    onSettled: () => {
      invalidateFeedbackWorkflow(qc)
      void qc.invalidateQueries({ queryKey: replySendHookDeliveriesQueryKey })
      void qc.invalidateQueries({ queryKey: replySendHookHealthQueryKey })
    },
  })
}
