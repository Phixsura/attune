// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/server"
	"github.com/Phixsura/attune/internal/pkg/logext"
)

// RegisterWriteTools registers write MCP tools.
func RegisterWriteTools(d *jsonrpc.Dispatcher, deps *Deps) {
	d.Register("update_workflow_state", updateWorkflowState(deps))
	d.Register("add_tag", addTag(deps))
	d.Register("remove_tag", removeTag(deps))
	d.Register("set_urgent", setUrgent(deps))
}

// UpdateWorkflowStateParams is the input for update_workflow_state.
type UpdateWorkflowStateParams struct {
	FeedbackID int64  `json:"feedback_id"`
	StateID    string `json:"state_id"`
	Comment    string `json:"comment,omitempty"`
}

func updateWorkflowState(deps *Deps) jsonrpc.ToolFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		if !server.HasScope(ctx, "mcp:write") {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "insufficient scope")
		}

		var p UpdateWorkflowStateParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "invalid params")
		}
		if p.FeedbackID == 0 {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "feedback_id is required")
		}
		if p.StateID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "state_id is required")
		}

		tenantID := server.TenantIDFromContext(ctx)
		if tenantID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "no tenant context")
		}

		byUser := MCPPrincipal(ctx)
		err := deps.WorkflowTransit.Transition(ctx, tenantID, p.FeedbackID, p.StateID, byUser, p.Comment)
		if err != nil {
			logext.Warnf(ctx, "mcp tool update_workflow_state failed: %v", err)
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "workflow transition failed")
		}

		if deps.Audit != nil {
			deps.Audit.Record(ctx, AuditEvent{ //nolint:errcheck
				TenantID:   tenantID,
				Actor:      byUser,
				Action:     domain.AuditActionMCPUpdateWorkflowState,
				TargetType: "feedback",
				TargetID:   fmt.Sprintf("%d", p.FeedbackID),
				Summary:    fmt.Sprintf("Set workflow state to %s via MCP", p.StateID),
				After:      map[string]any{"state_id": p.StateID, "comment": p.Comment},
			})
		}

		return map[string]any{
			"success":     true,
			"feedback_id": p.FeedbackID,
			"state_id":    p.StateID,
		}, nil
	}
}

// AddTagParams is the input for add_tag.
type AddTagParams struct {
	FeedbackID int64  `json:"feedback_id"`
	TagID      string `json:"tag_id"`
}

func addTag(deps *Deps) jsonrpc.ToolFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		if !server.HasScope(ctx, "mcp:write") {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "insufficient scope")
		}

		var p AddTagParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "invalid params")
		}
		if p.FeedbackID == 0 {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "feedback_id is required")
		}
		if p.TagID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "tag_id is required")
		}

		tagUUID, err := uuid.Parse(p.TagID)
		if err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "invalid tag_id format")
		}

		tenantID := server.TenantIDFromContext(ctx)
		if tenantID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "no tenant context")
		}

		byUser := MCPPrincipal(ctx)
		added, err := deps.TagAssign.Add(ctx, tenantID, p.FeedbackID, tagUUID, byUser)
		if err != nil {
			logext.Warnf(ctx, "mcp tool add_tag failed: %v", err)
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInternalError, "failed to add tag")
		}

		if deps.Audit != nil && added {
			deps.Audit.Record(ctx, AuditEvent{ //nolint:errcheck
				TenantID:   tenantID,
				Actor:      byUser,
				Action:     domain.AuditActionMCPAddTag,
				TargetType: "feedback",
				TargetID:   fmt.Sprintf("%d", p.FeedbackID),
				Summary:    fmt.Sprintf("Added tag %s via MCP", p.TagID),
				After:      map[string]any{"tag_id": p.TagID},
			})
		}

		return map[string]any{
			"success":     true,
			"feedback_id": p.FeedbackID,
			"tag_id":      p.TagID,
			"added":       added,
		}, nil
	}
}

// RemoveTagParams is the input for remove_tag.
type RemoveTagParams struct {
	FeedbackID int64  `json:"feedback_id"`
	TagID      string `json:"tag_id"`
}

func removeTag(deps *Deps) jsonrpc.ToolFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		if !server.HasScope(ctx, "mcp:write") {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "insufficient scope")
		}

		var p RemoveTagParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "invalid params")
		}
		if p.FeedbackID == 0 {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "feedback_id is required")
		}
		if p.TagID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "tag_id is required")
		}

		tagUUID, err := uuid.Parse(p.TagID)
		if err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "invalid tag_id format")
		}

		tenantID := server.TenantIDFromContext(ctx)
		if tenantID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "no tenant context")
		}

		removed, err := deps.TagAssign.Remove(ctx, tenantID, p.FeedbackID, tagUUID)
		if err != nil {
			logext.Warnf(ctx, "mcp tool remove_tag failed: %v", err)
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInternalError, "failed to remove tag")
		}

		if deps.Audit != nil && removed {
			byUser := MCPPrincipal(ctx)
			deps.Audit.Record(ctx, AuditEvent{ //nolint:errcheck
				TenantID:   tenantID,
				Actor:      byUser,
				Action:     domain.AuditActionMCPRemoveTag,
				TargetType: "feedback",
				TargetID:   fmt.Sprintf("%d", p.FeedbackID),
				Summary:    fmt.Sprintf("Removed tag %s via MCP", p.TagID),
				Before:     map[string]any{"tag_id": p.TagID},
			})
		}

		return map[string]any{
			"success":     true,
			"feedback_id": p.FeedbackID,
			"tag_id":      p.TagID,
			"removed":     removed,
		}, nil
	}
}

// SetUrgentParams is the input for set_urgent.
type SetUrgentParams struct {
	FeedbackID int64 `json:"feedback_id"`
	Urgent     bool  `json:"urgent"`
}

func setUrgent(deps *Deps) jsonrpc.ToolFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		if !server.HasScope(ctx, "mcp:write") {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "insufficient scope")
		}

		var p SetUrgentParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "invalid params")
		}
		if p.FeedbackID == 0 {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "feedback_id is required")
		}

		tenantID := server.TenantIDFromContext(ctx)
		if tenantID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "no tenant context")
		}

		err := deps.FeedbackWriter.SetUrgent(ctx, tenantID, p.FeedbackID, p.Urgent)
		if err != nil {
			logext.Warnf(ctx, "mcp tool set_urgent failed: %v", err)
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInternalError, "failed to set urgent flag")
		}

		if deps.Audit != nil {
			byUser := MCPPrincipal(ctx)
			deps.Audit.Record(ctx, AuditEvent{ //nolint:errcheck
				TenantID:   tenantID,
				Actor:      byUser,
				Action:     domain.AuditActionMCPSetUrgent,
				TargetType: "feedback",
				TargetID:   fmt.Sprintf("%d", p.FeedbackID),
				Summary:    fmt.Sprintf("Set urgent=%v via MCP", p.Urgent),
				After:      map[string]any{"urgent": p.Urgent},
			})
		}

		return map[string]any{
			"success":     true,
			"feedback_id": p.FeedbackID,
			"urgent":      p.Urgent,
		}, nil
	}
}
