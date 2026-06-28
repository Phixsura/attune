// SPDX-License-Identifier: Apache-2.0

package gdpr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	"github.com/Phixsura/attune/internal/pkg/workerdrain"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	gdprrepo "github.com/Phixsura/attune/internal/repo/gdpr"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

const (
	DefaultExportTTL        = 24 * time.Hour
	DefaultExportPoll       = 2 * time.Second
	DefaultExportHeartbeat  = 30 * time.Second
	DefaultExportPollLoop   = 5 * time.Second
	DefaultExportExpiryPoll = 1 * time.Minute
)

type exportJobStore interface {
	CreateExportJob(ctx context.Context, tenantID, subjectKey, subjectHash, createdByType, createdBy string) (*gdprrepo.ExportJob, error)
	GetExportJob(ctx context.Context, tenantID, jobID string) (*gdprrepo.ExportJob, error)
	RevokeExportJob(ctx context.Context, tenantID, jobID string) (*gdprrepo.ExportJob, error)
	ClaimNextExportJob(ctx context.Context) (*gdprrepo.ExportJob, error)
	ClaimNextExportJobWithOwner(ctx context.Context, owner string) (*gdprrepo.ExportJob, error)
	HeartbeatExportJob(ctx context.Context, jobID string) error
	HeartbeatExportJobWithOwner(ctx context.Context, jobID, owner string) (int64, error)
	CompleteExportJob(ctx context.Context, jobID, subjectDisplay, archiveFilename string, archive []byte, counts gdprrepo.Counts, expiresAt time.Time) error
	CompleteExportJobWithOwner(ctx context.Context, jobID, owner, subjectDisplay, archiveFilename string, archive []byte, counts gdprrepo.Counts, expiresAt time.Time) (int64, error)
	FailExportJob(ctx context.Context, jobID, errMsg string) error
	FailExportJobWithOwner(ctx context.Context, jobID, owner, errMsg string) (int64, error)
	ExpireReadyExportJobs(ctx context.Context, now time.Time) (int64, error)
	MarkExportJobDownloaded(ctx context.Context, tenantID, jobID string) (*gdprrepo.ExportJob, error)
}

type deleteRequestStore interface {
	ClaimNextDeleteRequest(ctx context.Context, now time.Time) (*gdprrepo.Request, error)
	CompleteDeleteRequest(ctx context.Context, requestID string, counts gdprrepo.Counts) error
	FailDeleteRequest(ctx context.Context, requestID, errMsg string) error
}

type deleteExecutor interface {
	ExecuteDeleteRequest(ctx context.Context, requestID string) (*gdprrepo.DeleteResult, error)
}

func (s *Service) StartExport(ctx context.Context, tenantID, rawSubjectKey string, actor auditlogsvc.Actor) (*attunev1.ExportGdprSubjectResponse, error) {
	subjectKey := stringsTrim(rawSubjectKey)
	if subjectKey == "" {
		return nil, ErrInvalidSubjectKey
	}
	store, ok := s.repo.(exportJobStore)
	if !ok {
		return nil, fmt.Errorf("gdpr export job store is not configured")
	}
	job, err := store.CreateExportJob(ctx, tenantID, subjectKey, subjectkey.Hash(tenantID, subjectKey), actor.Type, actor.ID)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(attunev1.ExportGdprSubjectResponse{
		JobId:             job.ID,
		Status:            exportJobStatusProto(job.Status),
		RetryAfterSeconds: int32(DefaultExportPoll.Seconds()),
	}), nil
}

func (s *Service) GetExportJob(ctx context.Context, tenantID, jobID string) (*attunev1.GdprExportStatusResponse, error) {
	store, ok := s.repo.(exportJobStore)
	if !ok {
		return nil, fmt.Errorf("gdpr export job store is not configured")
	}
	job, err := store.GetExportJob(ctx, tenantID, jobID)
	if err != nil {
		if errors.Is(err, gdprrepo.ErrExportJobNotFound) {
			return nil, ErrExportJobNotFound
		}
		return nil, err
	}
	return jobStatusResponse(job), nil
}

func (s *Service) DownloadExport(ctx context.Context, tenantID, jobID string) (*ExportBundle, error) {
	store, ok := s.repo.(exportJobStore)
	if !ok {
		return nil, fmt.Errorf("gdpr export job store is not configured")
	}
	job, err := store.MarkExportJobDownloaded(ctx, tenantID, jobID)
	if err != nil {
		switch {
		case errors.Is(err, gdprrepo.ErrExportJobNotDownloadable):
			return nil, ErrExportJobNotDownloadable
		case errors.Is(err, gdprrepo.ErrExportJobNotFound):
			return nil, ErrExportJobNotFound
		default:
			return nil, err
		}
	}
	return ptrext.Of(ExportBundle{
		Filename: job.ArchiveFilename,
		Data:     job.Archive,
		Counts:   job.Counts,
	}), nil
}

func (s *Service) RevokeExport(ctx context.Context, tenantID, jobID string, actor auditlogsvc.Actor) (*attunev1.RevokeGdprExportResponse, error) {
	store, ok := s.repo.(exportJobStore)
	if !ok {
		return nil, fmt.Errorf("gdpr export job store is not configured")
	}
	job, err := store.RevokeExportJob(ctx, tenantID, jobID)
	if err != nil {
		switch {
		case errors.Is(err, gdprrepo.ErrExportJobNotRevocable):
			return nil, ErrExportJobNotRevocable
		case errors.Is(err, gdprrepo.ErrExportJobNotFound):
			return nil, ErrExportJobNotFound
		default:
			return nil, err
		}
	}
	if s.audit != nil {
		if err := s.audit.Record(ctx, auditlogsvc.Event{
			TenantID:   tenantID,
			Actor:      actor,
			Action:     "gdpr.export.revoked",
			TargetType: "gdpr_subject",
			TargetID:   job.SubjectHash,
			Summary:    "Revoked GDPR export archive",
			After: map[string]any{
				"job_id":       job.ID,
				"subject_hash": job.SubjectHash,
				"revoked_at":   time.Now().UTC().Format(time.RFC3339),
			},
		}); err != nil {
			return nil, err
		}
	}
	return ptrext.Of(attunev1.RevokeGdprExportResponse{
		JobId:         job.ID,
		Status:        attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_REVOKED,
		RequestStatus: attunev1.GdprRequestStatus_GDPR_REQUEST_STATUS_REVOKED,
	}), nil
}

var (
	ErrExportJobNotFound        = errors.New("gdpr export job not found")
	ErrExportJobNotDownloadable = errors.New("gdpr export job not downloadable")
	ErrExportJobNotRevocable    = errors.New("gdpr export job not revocable")
)

type Worker struct {
	repo           repo
	jobStore       exportJobStore
	deleteStore    deleteRequestStore
	deleteExecutor deleteExecutor
	audit          auditRecorder
	exportTTL      time.Duration

	// owner uniquely identifies this worker instance for fencing.
	owner string

	// drain tracks in-flight work for graceful shutdown.
	drain *workerdrain.Drainer
}

type WorkerOption func(*Worker)

func WithWorkerExportTTL(ttl time.Duration) WorkerOption {
	return func(w *Worker) {
		if ttl > 0 {
			w.exportTTL = ttl
		}
	}
}

func NewWorker(repo repo, jobStore exportJobStore, audit auditRecorder, opts ...WorkerOption) *Worker {
	deleteStore, _ := jobStore.(deleteRequestStore)
	deleteExecutor, _ := repo.(deleteExecutor)
	d := workerdrain.New("gdpr")
	d.SetTimeout(30 * time.Second)
	worker := ptrext.Of(Worker{
		repo:           repo,
		jobStore:       jobStore,
		deleteStore:    deleteStore,
		deleteExecutor: deleteExecutor,
		audit:          audit,
		exportTTL:      DefaultExportTTL,
		owner:          "gdpr-" + uuid.NewString(),
		drain:          d,
	})
	for _, opt := range opts {
		opt(worker)
	}
	return worker
}

func (w *Worker) Run(ctx context.Context) {
	const where = "service.gdpr.Worker.Run"
	logext.Infof(ctx, "[%s] started,owner:%s,poll_interval:%s", where, w.owner, DefaultExportPollLoop)
	poll := time.NewTicker(DefaultExportPollLoop)
	expiry := time.NewTicker(DefaultExportExpiryPoll)
	defer poll.Stop()
	defer expiry.Stop()

	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] stopping,waiting for in-flight work", where)
			w.drain.Drain(ctx)
			return
		case <-poll.C:
			if err := w.processNextExport(ctx); err != nil {
				logext.Warnf(ctx, "[%s] process failed,err:%+v", where, err.Error())
			}
			if err := w.processNextDelete(ctx); err != nil {
				logext.Warnf(ctx, "[%s] delete failed,err:%+v", where, err.Error())
			}
		case <-expiry.C:
			if _, err := w.jobStore.ExpireReadyExportJobs(ctx, time.Now()); err != nil {
				logext.Warnf(ctx, "[%s] expire failed,err:%+v", where, err.Error())
			}
		}
	}
}

func (w *Worker) processNextExport(ctx context.Context) error {
	job, err := w.jobStore.ClaimNextExportJobWithOwner(ctx, w.owner)
	if err != nil || job == nil {
		return err
	}

	// Track in-flight work for graceful shutdown drain.
	w.drain.Enter()
	defer w.drain.Leave()

	// Create cancellable context for lease lost early exit.
	jobCtx, cancelJob := context.WithCancel(ctx)
	defer cancelJob()

	go w.heartbeat(ctx, job.ID, cancelJob)

	data, err := w.repo.Export(jobCtx, job.TenantID, job.SubjectKey)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logext.Warnf(ctx, "[gdpr.Worker.processNextExport] job aborted (lease lost),job_id:%s", job.ID)
			return nil
		}
		_, _ = w.jobStore.FailExportJobWithOwner(ctx, job.ID, w.owner, err.Error())
		return err
	}
	archive, err := buildZIP(job.TenantID, data)
	if err != nil {
		_, _ = w.jobStore.FailExportJobWithOwner(ctx, job.ID, w.owner, err.Error())
		return err
	}
	if n, err := w.jobStore.CompleteExportJobWithOwner(
		ctx,
		job.ID,
		w.owner,
		data.SubjectDisplay,
		zipFilename(job.SubjectKey, data.GeneratedAt),
		archive,
		data.Counts,
		time.Now().UTC().Add(w.exportTTL),
	); err != nil {
		return err
	} else if n == 0 {
		logext.Warnf(ctx, "[gdpr.Worker.processNextExport] job re-claimed by another worker,job_id:%s", job.ID)
		return nil
	}
	if w.audit != nil {
		if err := w.audit.Record(ctx, auditlogsvc.Event{
			TenantID:   job.TenantID,
			Actor:      storedActor(job.CreatedByType, job.CreatedBy),
			Action:     "gdpr.export",
			TargetType: "gdpr_subject",
			TargetID:   job.SubjectHash,
			Summary:    "Exported GDPR bundle for subject",
			After: map[string]any{
				"subject_hash":         job.SubjectHash,
				"feedback_count":       data.Counts.FeedbackCount,
				"tag_assignment_count": data.Counts.TagAssignmentCount,
				"feedback_audit_count": data.Counts.FeedbackAuditCount,
				"llm_audit_count":      data.Counts.LLMAuditCount,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) processNextDelete(ctx context.Context) error {
	if w.deleteStore == nil || w.deleteExecutor == nil {
		return nil
	}
	req, err := w.deleteStore.ClaimNextDeleteRequest(ctx, time.Now())
	if err != nil || req == nil {
		return err
	}
	result, err := w.deleteExecutor.ExecuteDeleteRequest(ctx, req.ID)
	if err != nil {
		_ = w.deleteStore.FailDeleteRequest(ctx, req.ID, err.Error())
		return err
	}
	if err := w.deleteStore.CompleteDeleteRequest(ctx, req.ID, result.Counts); err != nil {
		return err
	}
	if w.audit != nil {
		if err := w.audit.Record(ctx, auditlogsvc.Event{
			TenantID:   req.TenantID,
			Actor:      storedActor(req.CreatedByType, req.CreatedBy),
			Action:     "gdpr.delete",
			TargetType: "gdpr_subject",
			TargetID:   req.SubjectHash,
			Summary:    "Deleted GDPR subject data",
			After: map[string]any{
				"request_id":           req.ID,
				"subject_hash":         req.SubjectHash,
				"feedback_count":       result.Counts.FeedbackCount,
				"tag_assignment_count": result.Counts.TagAssignmentCount,
				"feedback_audit_count": result.Counts.FeedbackAuditCount,
				"llm_audit_count":      result.Counts.LLMAuditCount,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func storedActor(actorType, actorID string) auditlogsvc.Actor {
	normalizedType := stringsTrim(actorType)
	normalizedID := stringsTrim(actorID)
	if normalizedType == "" && normalizedID != "" {
		if strings.HasPrefix(normalizedID, "apikey:") {
			normalizedType = "api_key"
		} else {
			normalizedType = "admin"
		}
	}
	return auditlogsvc.Actor{Type: normalizedType, ID: normalizedID}
}

func (w *Worker) heartbeat(ctx context.Context, jobID string, cancelJob context.CancelFunc) {
	const where = "gdpr.Worker.heartbeat"
	ticker := time.NewTicker(DefaultExportHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Use background context to avoid racing with parent cancellation.
			n, err := w.jobStore.HeartbeatExportJobWithOwner(context.Background(), jobID, w.owner)
			if err != nil {
				logext.Warnf(ctx, "[%s] heartbeat failed,job_id:%s,err:%+v", where, jobID, err.Error())
			} else if n == 0 {
				logext.Warnf(ctx, "[%s] job re-claimed by another worker,job_id:%s — aborting", where, jobID)
				cancelJob() // Abort processing early
				return
			}
		}
	}
}

func jobStatusResponse(job *gdprrepo.ExportJob) *attunev1.GdprExportStatusResponse {
	resp := ptrext.Of(attunev1.GdprExportStatusResponse{
		JobId:              job.ID,
		SubjectKey:         job.SubjectKey,
		SubjectDisplay:     job.SubjectDisplay,
		Status:             exportJobStatusProto(job.Status),
		RetryAfterSeconds:  int32(DefaultExportPoll.Seconds()),
		FeedbackCount:      int32(job.Counts.FeedbackCount),
		TagAssignmentCount: int32(job.Counts.TagAssignmentCount),
		FeedbackAuditCount: int32(job.Counts.FeedbackAuditCount),
		LlmAuditCount:      int32(job.Counts.LLMAuditCount),
		CreatedAt:          job.CreatedAt.UTC().Format(time.RFC3339),
	})
	if job.StartedAt != nil {
		resp.StartedAt = ptrext.Of(job.StartedAt.UTC().Format(time.RFC3339))
	}
	if job.CompletedAt != nil {
		resp.CompletedAt = ptrext.Of(job.CompletedAt.UTC().Format(time.RFC3339))
	}
	if job.ExpiresAt != nil {
		resp.ExpiresAt = ptrext.Of(job.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if job.DownloadedAt != nil {
		resp.DownloadedAt = ptrext.Of(job.DownloadedAt.UTC().Format(time.RFC3339))
	}
	if job.RevokedAt != nil {
		resp.RevokedAt = ptrext.Of(job.RevokedAt.UTC().Format(time.RFC3339))
	}
	if job.ArchiveFilename != "" {
		resp.ArchiveFilename = ptrext.Of(job.ArchiveFilename)
	}
	if job.Error != "" {
		resp.Error = ptrext.Of(job.Error)
	}
	if job.Status == gdprrepo.ExportJobCompleted && job.ExpiresAt != nil && job.ExpiresAt.After(time.Now().UTC()) {
		resp.DownloadPath = ptrext.Of("/fb/v1/console/gdpr/exports/" + job.ID + "/download")
	}
	return resp
}

func exportJobStatusProto(status gdprrepo.ExportJobStatus) attunev1.GdprExportStatus {
	switch status {
	case gdprrepo.ExportJobQueued:
		return attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_QUEUED
	case gdprrepo.ExportJobRunning:
		return attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_RUNNING
	case gdprrepo.ExportJobCompleted:
		return attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_COMPLETED
	case gdprrepo.ExportJobFailed:
		return attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_FAILED
	case gdprrepo.ExportJobExpired:
		return attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_EXPIRED
	case gdprrepo.ExportJobRevoked:
		return attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_REVOKED
	default:
		return attunev1.GdprExportStatus_GDPR_EXPORT_STATUS_UNSPECIFIED
	}
}

func stringsTrim(v string) string {
	return strings.TrimSpace(v)
}
