// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/server"
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

		byUser := mcpPrincipal(ctx)
		err := deps.WorkflowTransit.Transition(ctx, tenantID, p.FeedbackID, p.StateID, byUser, p.Comment)
		if err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, err.Error())
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

		byUser := mcpPrincipal(ctx)
		added, err := deps.TagAssign.Add(ctx, tenantID, p.FeedbackID, tagUUID, byUser)
		if err != nil {
			return nil, err
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
			return nil, err
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
			return nil, err
		}

		return map[string]any{
			"success":     true,
			"feedback_id": p.FeedbackID,
			"urgent":      p.Urgent,
		}, nil
	}
}

func mcpPrincipal(ctx context.Context) string {
	clientID := server.ClientIDFromContext(ctx)
	sessionID := server.SessionIDFromContext(ctx)
	return fmt.Sprintf("mcp:%s:%s", clientID.String(), sessionID.String())
}
