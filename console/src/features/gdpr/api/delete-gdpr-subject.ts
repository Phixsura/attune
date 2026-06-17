import { useMutation } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { DeleteGdprSubjectResponse } from '@/proto/attune/v1/gdpr'

export function useDeleteGdprSubject() {
  return useMutation({
    mutationFn: (subjectKey: string) =>
      api<DeleteGdprSubjectResponse>('/fb/v1/console/gdpr/delete', {
        method: 'POST',
        body: { subjectKey },
      }),
  })
}
