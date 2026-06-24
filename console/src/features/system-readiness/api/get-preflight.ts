import { queryOptions } from '@tanstack/react-query'
import { api } from '@/lib/api-client'

export type PreflightStatus = 'pass' | 'warn' | 'fail' | 'skipped'

export type PreflightCategory =
  | 'config'
  | 'database'
  | 'migration'
  | 'backup'
  | 'encryption'
  | 'auth'
  | 'metrics'
  | 'worker'

export interface PreflightCheckResult {
  name: string
  category: PreflightCategory
  status: PreflightStatus
  message: string
  remediation?: string
}

export interface PreflightReport {
  status: PreflightStatus
  checks: PreflightCheckResult[]
  elapsed: string
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
