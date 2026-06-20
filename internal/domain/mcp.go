// SPDX-License-Identifier: Apache-2.0

package domain

// MCP OAuth scope string constants for use in client validation.
const (
	MCPScopeRead   = "mcp:read"
	MCPScopeWrite  = "mcp:write"
	MCPScopeIngest = "mcp:ingest"
)

// MCP audit action constants.
const (
	AuditActionMCPClientCreate = "mcp_client.create"
	AuditActionMCPClientRevoke = "mcp_client.revoke"
)
