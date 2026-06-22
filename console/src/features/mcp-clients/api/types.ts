// MCP OAuth client types - matches internal/handlers/console/mcpclient

export interface MCPClient {
  id: string
  name: string
  redirect_uris: string[]
  scopes: string[]
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
