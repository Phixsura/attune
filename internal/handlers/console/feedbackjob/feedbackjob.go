// SPDX-License-Identifier: Apache-2.0

// Package feedbackjob provides HTTP handlers for async job operations.
package feedbackjob

import (
	"context"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// jobService defines the service interface for job operations.
type jobService interface {
	GetJobStatus(ctx context.Context, tenantID, jobID string) (*attunev1.JobStatusResponse, error)
	ListJobs(ctx context.Context, tenantID string, status *string, limit int, cursor string) ([]*attunev1.JobStatusResponse, string, error)
	CancelJob(ctx context.Context, tenantID, jobID string) (*attunev1.CancelJobResponse, error)
}

// Handler handles async job HTTP operations.
type Handler struct {
	svc   jobService
	audit auditRecorder
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

// NewHandler creates a new job handler with the provided service.
func NewHandler(svc jobService) *Handler {
	return ptrext.Of(Handler{svc: svc})
}

func (h *Handler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}
