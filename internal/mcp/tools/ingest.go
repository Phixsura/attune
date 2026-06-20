// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/mcp/jsonrpc"
	"github.com/Phixsura/attune/internal/mcp/server"
)

// RegisterIngestTools registers ingest MCP tools.
func RegisterIngestTools(d *jsonrpc.Dispatcher, deps *Deps) {
	d.Register("submit_feedback", submitFeedback(deps))
}

// SubmitFeedbackParams is the input for submit_feedback.
type SubmitFeedbackParams struct {
	Content        string         `json:"content"`
	Source         string         `json:"source,omitempty"`
	PageURL        string         `json:"page_url,omitempty"`
	UserID         string         `json:"user_id,omitempty"`
	SourceMeta     map[string]any `json:"source_meta,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
}

// SubmitFeedbackResponse is the response for submit_feedback.
type SubmitFeedbackResponse struct {
	FeedbackID int64 `json:"feedback_id"`
}

func submitFeedback(deps *Deps) jsonrpc.ToolFunc {
	return func(ctx context.Context, params json.RawMessage) (any, error) {
		if !server.HasScope(ctx, "mcp:ingest") {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "insufficient scope")
		}

		var p SubmitFeedbackParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "invalid params")
		}
		if p.Content == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidParams, "content is required")
		}

		tenantID := server.TenantIDFromContext(ctx)
		if tenantID == "" {
			return nil, jsonrpc.NewToolError(jsonrpc.CodeInvalidRequest, "no tenant context")
		}

		source := p.Source
		if source == "" {
			source = "mcp"
		}

		userID := p.UserID
		if userID == "" {
			userID = mcpPrincipal(ctx)
		}

		input := domain.IngestInput{
			Content:        p.Content,
			Source:         source,
			PageURL:        p.PageURL,
			SourceMeta:     p.SourceMeta,
			IdempotencyKey: p.IdempotencyKey,
		}

		id, err := deps.Ingestor.Ingest(ctx, tenantID, userID, input)
		if err != nil {
			return nil, err
		}

		return SubmitFeedbackResponse{FeedbackID: id}, nil
	}
}
