import { api } from '@/lib/api-client'

export interface BreakGlassToken {
  id: string
  admin_email: string
  expires_at: string
  used_at?: string
  used_from_ip?: string
  issued_by: string
  issued_at: string
  revoked_at?: string
  revoked_by?: string
  allowed_ips?: string[]
  status: string
}

export interface RevokeAllResponse {
  revoked: number
}

export interface BreakGlassLockout {
  ip: string
  locked_until: string
  remaining_mins: number
  attempts: number
}

export interface ListLockoutsResponse {
  lockouts: BreakGlassLockout[]
}

export interface ListTokensResponse {
  tokens: BreakGlassToken[]
}

export interface IssueTokenRequest {
  admin_email: string
  ttl_minutes: number
  allowed_ips?: string[]
}

export interface IssueTokenResponse {
  token: BreakGlassToken
  raw_token: string
  expires_at: string
}

export async function listBreakGlassTokens(): Promise<ListTokensResponse> {
  return api<ListTokensResponse>('/fb/v1/console/auth/breakglass/tokens')
}

export async function issueBreakGlassToken(req: IssueTokenRequest): Promise<IssueTokenResponse> {
  return api<IssueTokenResponse>('/fb/v1/console/auth/breakglass/issue', {
    method: 'POST',
    body: req,
  })
}

export async function revokeBreakGlassToken(tokenId: string): Promise<void> {
  await api(`/fb/v1/console/auth/breakglass/tokens/${tokenId}/revoke`, {
    method: 'POST',
  })
}

export async function revokeAllBreakGlassTokens(): Promise<RevokeAllResponse> {
  return api<RevokeAllResponse>('/fb/v1/console/auth/breakglass/tokens/revoke-all', {
    method: 'POST',
  })
}

export async function listBreakGlassLockouts(): Promise<ListLockoutsResponse> {
  return api<ListLockoutsResponse>('/fb/v1/console/auth/breakglass/lockouts')
}

export async function unlockBreakGlassLockout(ip: string): Promise<void> {
  await api(`/fb/v1/console/auth/breakglass/lockouts/${encodeURIComponent(ip)}/unlock`, {
    method: 'POST',
  })
}
