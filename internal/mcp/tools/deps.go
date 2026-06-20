// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"

	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
)

// FeedbackReader provides read access to feedback data.
type FeedbackReader interface {
	ListForConsole(ctx context.Context, tenantID string, opts feedback.ConsoleListOpts) ([]feedback.ConsoleListRow, error)
	GetForConsole(ctx context.Context, tenantID string, id int64) (*feedback.ConsoleDetailRow, error)
}

// WorkflowStateReader provides read access to workflow states.
type WorkflowStateReader interface {
	List(ctx context.Context, tenantID string, includeArchived bool) ([]workflowstate.WorkflowState, error)
	GetByTenantAndID(ctx context.Context, tenantID, id string) (*workflowstate.WorkflowState, error)
}

// TagReader provides read access to tags.
type TagReader interface {
	List(ctx context.Context, tenantID string, includeArchived bool) ([]feedbacktag.Tag, error)
}

// Deps holds dependencies for MCP tools.
type Deps struct {
	Feedback      FeedbackReader
	WorkflowState WorkflowStateReader
	Tag           TagReader
}
