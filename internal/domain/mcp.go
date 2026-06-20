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

// MCP tool audit action constants.
const (
	// Read tools
	AuditActionMCPListFeedback       = "mcp.list_feedback"
	AuditActionMCPGetFeedback        = "mcp.get_feedback"
	AuditActionMCPListWorkflowStates = "mcp.list_workflow_states"
	AuditActionMCPGetWorkflowState   = "mcp.get_workflow_state"
	AuditActionMCPListTags           = "mcp.list_tags"

	// Write tools
	AuditActionMCPUpdateWorkflowState = "mcp.update_workflow_state"
	AuditActionMCPAddTag              = "mcp.add_tag"
	AuditActionMCPRemoveTag           = "mcp.remove_tag"
	AuditActionMCPSetUrgent           = "mcp.set_urgent"

	// Ingest tools
	AuditActionMCPSubmitFeedback = "mcp.submit_feedback"
)
