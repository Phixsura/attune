// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/server"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// ListFeedbackParams is the input for list_feedback.
type ListFeedbackParams struct {
	Cursor          *int64  `json:"cursor,omitempty"`
	Limit           *int    `json:"limit,omitempty"`
	WorkflowStateID *string `json:"workflow_state_id,omitempty"`
	TagID           *string `json:"tag_id,omitempty"`
	Q               *string `json:"q,omitempty"`
	Urgent          *bool   `json:"urgent,omitempty"`
}

// FeedbackItem is the response item for feedback queries.
type FeedbackItem struct {
	ID               int64     `json:"id"`
	Content          string    `json:"content"`
	Source           string    `json:"source"`
	Type             string    `json:"type"`
	UserID           string    `json:"user_id"`
	Language         string    `json:"language,omitempty"`
	PageURL          string    `json:"page_url,omitempty"`
	EnrichedTitle    string    `json:"enriched_title,omitempty"`
	EnrichedAttrs    any       `json:"enriched_attrs,omitempty"`
	IsUrgent         bool      `json:"is_urgent"`
	EnrichmentStatus string    `json:"enrichment_status"`
	CreatedAt        time.Time `json:"created_at"`
	WorkflowStateID  *string   `json:"workflow_state_id,omitempty"`
}

// ListFeedbackResponse is the response for list_feedback.
type ListFeedbackResponse struct {
	Items      []FeedbackItem `json:"items"`
	NextCursor *int64         `json:"next_cursor,omitempty"`
}

func buildListOpts(p ListFeedbackParams) feedback.ConsoleListOpts {
	limit := ptrext.IndirectOr(p.Limit, 50)
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return feedback.ConsoleListOpts{
		Limit:           limit,
		Cursor:          ptrext.Indirect(p.Cursor),
		Q:               ptrext.Indirect(p.Q),
		WorkflowStateID: p.WorkflowStateID,
		TagID:           p.TagID,
		Urgent:          p.Urgent,
	}
}

// RegisterReadTools registers read-only MCP tools.
func RegisterReadTools(d *jsonrpc.Dispatcher, deps *Deps) {
	d.Register("list_feedback", listFeedback(deps))
	d.Register("get_feedback", getFeedback(deps))
	d.Register("list_workflow_states", listWorkflowStates(deps))
	d.Register("get_workflow_state", getWorkflowState(deps))
	d.Register("list_tags", listTags(deps))
}

func listFeedback(deps *Deps) jsonrpc.ToolFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		if !server.HasScope(ctx, "mcp:read") {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "insufficient scope")
		}

		var p ListFeedbackParams
		if len(params) > 0 {
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "invalid params")
			}
		}

		tenantID := server.TenantIDFromContext(ctx)
		if tenantID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "no tenant context")
		}

		opts := buildListOpts(p)

		rows, err := deps.Feedback.ListForConsole(ctx, tenantID, opts)
		if err != nil {
			return nil, err
		}

		items := make([]FeedbackItem, 0, len(rows))
		for _, r := range rows {
			item := FeedbackItem{
				ID:               r.ID,
				Content:          r.Content,
				Source:           r.Source,
				Type:             r.Type,
				UserID:           r.UserID,
				Language:         r.Language,
				PageURL:          r.PageURL,
				EnrichedTitle:    r.EnrichedTitle,
				IsUrgent:         r.IsUrgent,
				EnrichmentStatus: r.EnrichmentStatus,
				CreatedAt:        r.CreatedAt,
				WorkflowStateID:  r.WorkflowStateID,
			}
			if len(r.EnrichedAttrs) > 0 {
				var attrs any
				if json.Unmarshal(r.EnrichedAttrs, &attrs) == nil {
					item.EnrichedAttrs = attrs
				}
			}
			items = append(items, item)
		}

		resp := ListFeedbackResponse{Items: items}
		if len(items) == opts.Limit && len(items) > 0 {
			resp.NextCursor = ptrext.Of(items[len(items)-1].ID)
		}
		return resp, nil
	}
}

// GetFeedbackParams is the input for get_feedback.
type GetFeedbackParams struct {
	ID int64 `json:"id"`
}

// FeedbackDetail is the detailed response for a single feedback.
type FeedbackDetail struct {
	FeedbackItem
	SourceMeta        any        `json:"source_meta,omitempty"`
	Attachments       any        `json:"attachments,omitempty"`
	EnrichmentError   string     `json:"enrichment_error,omitempty"`
	EnrichedAt        *time.Time `json:"enriched_at,omitempty"`
	EnrichedRationale string     `json:"enriched_rationale,omitempty"`
	ReplyDraft        string     `json:"reply_draft,omitempty"`
}

func getFeedback(deps *Deps) jsonrpc.ToolFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		if !server.HasScope(ctx, "mcp:read") {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "insufficient scope")
		}

		var p GetFeedbackParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "invalid params")
		}
		if p.ID == 0 {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "id is required")
		}

		tenantID := server.TenantIDFromContext(ctx)
		if tenantID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "no tenant context")
		}

		row, err := deps.Feedback.GetForConsole(ctx, tenantID, p.ID)
		if errors.Is(err, feedback.ErrFeedbackNotFound) {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "feedback not found")
		}
		if err != nil {
			return nil, err
		}

		detail := FeedbackDetail{
			FeedbackItem: FeedbackItem{
				ID:               row.ID,
				Content:          row.Content,
				Source:           row.Source,
				Type:             row.Type,
				UserID:           row.UserID,
				Language:         row.Language,
				PageURL:          row.PageURL,
				EnrichedTitle:    row.EnrichedTitle,
				IsUrgent:         row.IsUrgent,
				EnrichmentStatus: row.EnrichmentStatus,
				CreatedAt:        row.CreatedAt,
				WorkflowStateID:  row.WorkflowStateID,
			},
			EnrichmentError:   row.EnrichmentError,
			EnrichedAt:        row.EnrichedAt,
			EnrichedRationale: row.EnrichedRationale,
			ReplyDraft:        row.ReplyDraft,
		}

		if len(row.EnrichedAttrs) > 0 {
			var attrs any
			if json.Unmarshal(row.EnrichedAttrs, &attrs) == nil {
				detail.EnrichedAttrs = attrs
			}
		}
		if len(row.SourceMeta) > 0 {
			var meta any
			if json.Unmarshal(row.SourceMeta, &meta) == nil {
				detail.SourceMeta = meta
			}
		}
		if len(row.Attachments) > 0 {
			var att any
			if json.Unmarshal(row.Attachments, &att) == nil {
				detail.Attachments = att
			}
		}

		return detail, nil
	}
}

// WorkflowStateItem is the response item for workflow state queries.
type WorkflowStateItem struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	DisplayName map[string]string `json:"display_name"`
	Color       string            `json:"color"`
	Category    string            `json:"category"`
	Position    int               `json:"position"`
	IsDefault   bool              `json:"is_default"`
}

func listWorkflowStates(deps *Deps) jsonrpc.ToolFunc {
	return func(ctx context.Context, _ json.RawMessage) (any, error) {
		if !server.HasScope(ctx, "mcp:read") {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "insufficient scope")
		}

		tenantID := server.TenantIDFromContext(ctx)
		if tenantID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "no tenant context")
		}

		rows, err := deps.WorkflowState.List(ctx, tenantID, false)
		if err != nil {
			return nil, err
		}

		items := make([]WorkflowStateItem, 0, len(rows))
		for _, r := range rows {
			items = append(items, WorkflowStateItem{
				ID:          r.ID,
				Name:        r.Name,
				DisplayName: r.DisplayName,
				Color:       r.Color,
				Category:    r.Category,
				Position:    r.Position,
				IsDefault:   r.IsDefault,
			})
		}
		return map[string]any{"items": items}, nil
	}
}

// GetWorkflowStateParams is the input for get_workflow_state.
type GetWorkflowStateParams struct {
	ID string `json:"id"`
}

func getWorkflowState(deps *Deps) jsonrpc.ToolFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		if !server.HasScope(ctx, "mcp:read") {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "insufficient scope")
		}

		var p GetWorkflowStateParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "invalid params")
		}
		if p.ID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "id is required")
		}

		tenantID := server.TenantIDFromContext(ctx)
		if tenantID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "no tenant context")
		}

		row, err := deps.WorkflowState.GetByTenantAndID(ctx, tenantID, p.ID)
		if err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "workflow state not found")
		}

		return WorkflowStateItem{
			ID:          row.ID,
			Name:        row.Name,
			DisplayName: row.DisplayName,
			Color:       row.Color,
			Category:    row.Category,
			Position:    row.Position,
			IsDefault:   row.IsDefault,
		}, nil
	}
}

// TagItem is the response item for tag queries.
type TagItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
	UsageCount  int    `json:"usage_count"`
}

func listTags(deps *Deps) jsonrpc.ToolFunc {
	return func(ctx context.Context, _ json.RawMessage) (any, error) {
		if !server.HasScope(ctx, "mcp:read") {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "insufficient scope")
		}

		tenantID := server.TenantIDFromContext(ctx)
		if tenantID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "no tenant context")
		}

		rows, err := deps.Tag.List(ctx, tenantID, false)
		if err != nil {
			return nil, err
		}

		items := make([]TagItem, 0, len(rows))
		for _, r := range rows {
			items = append(items, TagItem{
				ID:          r.ID.String(),
				Name:        r.Name,
				Color:       r.Color,
				Description: r.Description,
				UsageCount:  r.UsageCount,
			})
		}
		return map[string]any{"items": items}, nil
	}
}
