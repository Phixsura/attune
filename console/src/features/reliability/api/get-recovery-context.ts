import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { PreflightStatus } from '@/lib/preflight'
import type {
  RecoveryContextLastRun as SystemRecoveryContextLastRun,
  RecoveryContextResponse as SystemRecoveryContextResponse,
} from '@/proto/attune/v1/system'

export interface RecoveryContextLastRun
  extends Omit<SystemRecoveryContextLastRun, 'status' | 'durationMs'> {
  status: PreflightStatus
  durationMs: number
}

export interface RecoveryContextResponse
  extends Omit<
    SystemRecoveryContextResponse,
    'status' | 'freshnessWindowSeconds' | 'ageSeconds' | 'lastRun'
  > {
  status: PreflightStatus
  freshnessWindowSeconds: number
  ageSeconds?: number
  lastRun?: RecoveryContextLastRun
}

export const recoveryContextQuery = () =>
  queryOptions({
    queryKey: ['console', 'system', 'recovery'],
    queryFn: async ({ signal }) => {
      return api<RecoveryContextResponse>('/fb/v1/console/system/recovery', { signal })
    },
    staleTime: 5 * 60 * 1000,
    refetchOnWindowFocus: false,
  })
