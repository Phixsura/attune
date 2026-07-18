import { useQuery } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export type JobStatus = 'queued' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface Job {
  jobId: string
  status: JobStatus
  total: number
  progress: number
  result?: unknown
  error?: string
}

export function useJobStatus(jobId: string | null, options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: ['job', jobId],
    queryFn: async ({ signal }) => {
      /* v8 ignore next -- @preserve: query is disabled until a job id exists. */
      if (!jobId) return null
      return api<Job>(`/fb/v1/console/jobs/${jobId}`, { signal })
    },
    enabled: !!jobId && options?.enabled !== false,
    refetchInterval: (query) => {
      const data = query.state.data
      if (!data) return false
      // Poll every 2s while running, stop when terminal
      if (data.status === 'queued' || data.status === 'running') {
        return 2000
      }
      return false
    },
  })
}
