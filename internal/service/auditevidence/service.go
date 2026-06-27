// SPDX-License-Identifier: Apache-2.0

// Package auditevidence implements tamper-evident audit evidence export.
// It produces signed, hash-chained ZIP archives containing audit log entries.
package auditevidence

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	aerepo "github.com/Phixsura/attune/internal/repo/auditevidence"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

var (
	ErrJobNotFound        = errors.New("audit evidence export job not found")
	ErrJobNotDownloadable = errors.New("audit evidence export job not downloadable")
)

type jobStore interface {
	CreateJob(ctx context.Context, tenantID, createdBy string, filterJSON json.RawMessage) (*aerepo.ExportJob, error)
	GetJob(ctx context.Context, tenantID, jobID string) (*aerepo.ExportJob, error)
	ClaimNextJob(ctx context.Context, owner string) (*aerepo.ExportJob, error)
	HeartbeatJob(ctx context.Context, jobID, owner string) (int64, error)
	CompleteJob(ctx context.Context, jobID, owner, archiveFilename string, archive []byte, totalEvents int, expiresAt time.Time) (int64, error)
	FailJob(ctx context.Context, jobID, owner, errMsg string) (int64, error)
	ExpireJobs(ctx context.Context, now time.Time) (int64, error)
	RequeueStaleJobs(ctx context.Context, staleThreshold time.Duration) (int64, error)
	MarkDownloaded(ctx context.Context, tenantID, jobID string) (*aerepo.ExportJob, error)
}

type auditLogLister interface {
	List(ctx context.Context, filter auditlogsvc.ListFilter) (auditlogrepo.ListResult, error)
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

type Service struct {
	jobs      jobStore
	auditLogs auditLogLister
	audit     auditRecorder
	exportTTL time.Duration
}

type Option func(*Service)

func WithExportTTL(ttl time.Duration) Option {
	return func(s *Service) {
		if ttl > 0 {
			s.exportTTL = ttl
		}
	}
}

func New(jobs jobStore, auditLogs auditLogLister, audit auditRecorder, opts ...Option) *Service {
	svc := ptrext.Of(Service{
		jobs:      jobs,
		auditLogs: auditLogs,
		audit:     audit,
		exportTTL: 72 * time.Hour,
	})
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

type ExportFilter struct {
	Actions    []string   `json:"actions,omitempty"`
	ActorType  string     `json:"actor_type,omitempty"`
	ActorID    string     `json:"actor_id,omitempty"`
	TargetType string     `json:"target_type,omitempty"`
	TargetID   string     `json:"target_id,omitempty"`
	From       *time.Time `json:"from,omitempty"`
	To         *time.Time `json:"to,omitempty"`
}

type StartExportResult struct {
	JobID  string
	Status string
}

func (s *Service) StartExport(ctx context.Context, tenantID, createdBy string, filter ExportFilter) (*StartExportResult, error) {
	filterJSON, err := json.Marshal(filter)
	if err != nil {
		return nil, err
	}
	job, err := s.jobs.CreateJob(ctx, tenantID, createdBy, filterJSON)
	if err != nil {
		return nil, err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, auditlogsvc.Event{
			TenantID:   tenantID,
			Actor:      auditlogsvc.Actor{Type: "admin", ID: createdBy},
			Action:     "audit_evidence.create",
			TargetType: "audit_evidence_export",
			TargetID:   job.ID,
			Summary:    "Created audit evidence export job",
			After: map[string]any{
				"job_id": job.ID,
				"filter": filter,
			},
		})
	}
	return ptrext.Of(StartExportResult{
		JobID:  job.ID,
		Status: string(job.Status),
	}), nil
}

func (s *Service) GetJob(ctx context.Context, tenantID, jobID string) (*aerepo.ExportJob, error) {
	job, err := s.jobs.GetJob(ctx, tenantID, jobID)
	if err != nil {
		if errors.Is(err, aerepo.ErrJobNotFound) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	return job, nil
}

type DownloadBundle struct {
	Filename string
	Data     []byte
}

func (s *Service) Download(ctx context.Context, tenantID, jobID string, actor auditlogsvc.Actor) (*DownloadBundle, error) {
	job, err := s.jobs.MarkDownloaded(ctx, tenantID, jobID)
	if err != nil {
		if errors.Is(err, aerepo.ErrJobNotDownloadable) {
			return nil, ErrJobNotDownloadable
		}
		if errors.Is(err, aerepo.ErrJobNotFound) {
			return nil, ErrJobNotFound
		}
		return nil, err
	}
	if s.audit != nil {
		_ = s.audit.Record(ctx, auditlogsvc.Event{
			TenantID:   tenantID,
			Actor:      actor,
			Action:     "audit_evidence.download",
			TargetType: "audit_evidence_export",
			TargetID:   job.ID,
			Summary:    "Downloaded audit evidence export archive",
			After: map[string]any{
				"job_id":   job.ID,
				"filename": job.ArchiveFilename,
			},
		})
	}
	return ptrext.Of(DownloadBundle{
		Filename: job.ArchiveFilename,
		Data:     job.Archive,
	}), nil
}
