// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbacktag"
	"github.com/Phixsura/attune/internal/repo/workflowstate"
)

// FeedbackReader provides read access to feedback data.
type FeedbackReader interface {
	ListForConsole(ctx context.Context, tenantID string, opts feedback.ConsoleListOpts) ([]feedback.ConsoleListRow, error)
	GetForConsole(ctx context.Context, tenantID string, id int64) (*feedback.ConsoleDetailRow, error)
}

// FeedbackWriter provides write access to feedback data.
type FeedbackWriter interface {
	SetUrgent(ctx context.Context, tenantID string, id int64, urgent bool) error
}

// WorkflowStateReader provides read access to workflow states.
type WorkflowStateReader interface {
	List(ctx context.Context, tenantID string, includeArchived bool) ([]workflowstate.WorkflowState, error)
	GetByTenantAndID(ctx context.Context, tenantID, id string) (*workflowstate.WorkflowState, error)
}

// WorkflowTransitioner provides workflow state transition.
type WorkflowTransitioner interface {
	Transition(ctx context.Context, tenantID string, feedbackID int64, toStateID, byUser, comment string) error
}

// TagReader provides read access to tags.
type TagReader interface {
	List(ctx context.Context, tenantID string, includeArchived bool) ([]feedbacktag.Tag, error)
}

// TagAssigner provides tag assignment operations.
type TagAssigner interface {
	Add(ctx context.Context, tenantID string, feedbackID int64, tagID uuid.UUID, createdBy string) (bool, error)
	Remove(ctx context.Context, tenantID string, feedbackID int64, tagID uuid.UUID) (bool, error)
}

// Ingestor provides feedback ingestion.
type Ingestor interface {
	Ingest(ctx context.Context, tenantID, userID string, in domain.IngestInput) (int64, error)
}

// Deps holds dependencies for MCP tools.
type Deps struct {
	Feedback        FeedbackReader
	FeedbackWriter  FeedbackWriter
	WorkflowState   WorkflowStateReader
	WorkflowTransit WorkflowTransitioner
	Tag             TagReader
	TagAssign       TagAssigner
	Ingestor        Ingestor
}
