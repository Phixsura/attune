import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { RegenerateReplyDraftResponse } from '@/proto/attune/v1/ingest'

// useRegenerateReplyDraft re-runs LLM reply-draft generation for one feedback
// row and returns the fresh draft. On success it invalidates the detail query
// so the sheet re-reads the persisted draft. The draft is operator-facing only
// — it is never auto-sent.
export function useRegenerateReplyDraft(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () =>
      api<RegenerateReplyDraftResponse>(`/fb/v1/console/feedback/${id}/reply-draft/regenerate`, {
        method: 'POST',
        body: {},
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['console', 'feedback', 'detail', id] })
    },
  })
}
