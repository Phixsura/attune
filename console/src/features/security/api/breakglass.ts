import { api } from '@/lib/api-client'

export interface BreakGlassToken {
  id: string
  admin_email: string
  expires_at: string
  used_at?: string
  issued_by: string
  issued_at: string
  revoked_at?: string
  revoked_by?: string
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
