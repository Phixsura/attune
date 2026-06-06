import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api, setCsrfToken } from '@/lib/api-client'

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>('/fb/v1/console/logout', { method: 'POST' }),
    onSettled: () => {
      setCsrfToken(null)
      qc.removeQueries({ queryKey: ['console', 'me'] })
    },
  })
}
