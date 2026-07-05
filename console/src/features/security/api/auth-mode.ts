import { queryOptions } from '@tanstack/react-query'
import { api, getCsrfToken } from '@/lib/api-client'

export interface AuthModeResponse {
  mode: 'hybrid' | 'sso_only'
}

export interface CutoverResponse {
  success: boolean
  message: string
  preflight?: {
    status: string
    checks: Array<{
      name: string
      status: string
      message: string
      remediation?: string
    }>
  }
}

export interface FallbackResponse {
  success: boolean
  message: string
}

export async function getAuthMode(): Promise<AuthModeResponse> {
  return api<AuthModeResponse>('/fb/v1/console/auth/sso/mode')
}

export function authModeQuery() {
  return queryOptions({
    queryKey: ['auth-mode'] as const,
    queryFn: getAuthMode,
    staleTime: 30_000,
  })
}

export async function cutoverToSSO(skipBreakglassCheck = false): Promise<CutoverResponse> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  const csrf = getCsrfToken()
  if (csrf) {
    headers['X-CSRF-Token'] = csrf
  }
  const res = await fetch('/fb/v1/console/auth/sso/cutover', {
    method: 'POST',
    headers,
    body: JSON.stringify({ skip_breakglass_check: skipBreakglassCheck }),
    credentials: 'include',
  })
  const data = await res.json()
  return data as CutoverResponse
}

export async function fallbackToHybrid(): Promise<FallbackResponse> {
  return api<FallbackResponse>('/fb/v1/console/auth/sso/fallback', {
    method: 'POST',
  })
}
