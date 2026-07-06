import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'
import type { PreflightStatus as SharedPreflightStatus } from '@/lib/preflight'
import type {
  PreflightCheckResult as SystemPreflightCheckResult,
  PreflightReportResponse as SystemPreflightReportResponse,
} from '@/proto/attune/v1/system'

export type PreflightStatus = SharedPreflightStatus
export type PreflightCategory =
  | 'config'
  | 'database'
  | 'migration'
  | 'backup'
  | 'encryption'
  | 'auth'
  | 'metrics'
  | 'worker'

export interface PreflightCheckResult
  extends Omit<SystemPreflightCheckResult, 'category' | 'status'> {
  category: PreflightCategory
  status: PreflightStatus
}

export interface PreflightReport extends Omit<SystemPreflightReportResponse, 'status' | 'checks'> {
  status: PreflightStatus
  checks: PreflightCheckResult[]
}

export const preflightQuery = () =>
  queryOptions({
    queryKey: ['console', 'system', 'preflight'],
    queryFn: async ({ signal }) => {
      return api<PreflightReport>('/fb/v1/console/system/preflight', { signal })
    },
    staleTime: 0,
    refetchOnWindowFocus: false,
  })
