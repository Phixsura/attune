// MCP OAuth client types - matches internal/handlers/console/mcpclient

export interface MCPClient {
  id: string
  name: string
  redirect_uris: string[]
  scopes: string[]
  tool_policy_mode: 'legacy_allow_all' | 'allow_list'
  rate_limit_rpm?: number | null
  rate_limit_burst?: number | null
  created_at: string
  created_by: string
  revoked_at?: string
}

export interface ListMCPClientsResponse {
  clients: MCPClient[]
}

export interface CreateMCPClientRequest {
  name: string
  redirect_uris: string[]
  scopes: string[]
}

export interface CreateMCPClientResponse {
  client: MCPClient
}

export interface MCPSession {
  id: string
  scopes: string[]
  last_tool_name: string
  last_decision: string
  last_ip: string
  last_user_agent: string
  last_active_at: string
  created_at: string
  closed_reason: string
  closed_by: string
  closed_at?: string
}

export interface MCPRefreshGrant {
  id: string
  session_id: string
  scopes: string[]
  expires_at: string
  created_at: string
}

export interface MCPClientTool {
  name: string
  required_scope: string
  risk: string
  data_class: string
  read_only_hint: boolean
  destructive_hint: boolean
  open_world_hint: boolean
  default_rpm: number
  default_burst: number
  scope_granted: boolean
  effective_allowed: boolean
  effect: '' | 'allow' | 'deny'
  rate_limit_rpm?: number | null
  rate_limit_burst?: number | null
}

export interface MCPConnectionProfile {
  server_url: string
  resource_url: string
  oauth_issuer: string
  authorization_endpoint: string
  token_endpoint: string
  protected_resource_metadata_url: string
  authorization_server_metadata_url: string
  openid_configuration_url: string
  legacy_protected_resource_metadata_url: string
  legacy_authorization_server_metadata_url: string
  legacy_openid_configuration_url: string
}

export interface MCPClientDetailResponse {
  client: MCPClient
  sessions: MCPSession[]
  refresh_grants: MCPRefreshGrant[]
  tools: MCPClientTool[]
  connection?: MCPConnectionProfile
}

export interface UpdateMCPClientRequest {
  tool_policy_mode: 'legacy_allow_all' | 'allow_list'
  rate_limit_rpm: number | null
  rate_limit_burst: number | null
}

export interface UpdateMCPClientResponse {
  client: MCPClient
}

export interface ReplaceMCPToolPoliciesRequest {
  policies: Array<{
    tool_name: string
    effect: 'allow' | 'deny'
    rate_limit_rpm: number | null
    rate_limit_burst: number | null
  }>
}

export interface ReplaceMCPToolPoliciesResponse {
  tools: MCPClientTool[]
}
