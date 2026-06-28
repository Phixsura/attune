// SPDX-License-Identifier: Apache-2.0

package auditevidence

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/workerdrain"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

const (
	DefaultPollInterval      = 5 * time.Second
	DefaultHeartbeat         = 30 * time.Second
	DefaultExpiryPoll        = 1 * time.Minute
	DefaultExportTTL         = 72 * time.Hour
	DefaultMaxEventsLimit    = 100000
	DefaultStaleJobThreshold = 5 * time.Minute
)

type Worker struct {
	jobs      jobStore
	auditLogs auditLogLister
	audit     auditRecorder
	exportTTL time.Duration
	signKey   []byte
	owner     string
	drain     *workerdrain.Drainer
}

type WorkerOption func(*Worker)

func WithWorkerExportTTL(ttl time.Duration) WorkerOption {
	return func(w *Worker) {
		if ttl > 0 {
			w.exportTTL = ttl
		}
	}
}

func WithWorkerSigningKey(key []byte) WorkerOption {
	return func(w *Worker) {
		w.signKey = key
	}
}

func NewWorker(jobs jobStore, auditLogs auditLogLister, audit auditRecorder, opts ...WorkerOption) *Worker {
	d := workerdrain.New("audit-evidence")
	d.SetTimeout(30 * time.Second)
	w := ptrext.Of(Worker{
		jobs:      jobs,
		auditLogs: auditLogs,
		audit:     audit,
		exportTTL: DefaultExportTTL,
		owner:     "ae-" + uuid.NewString(),
		drain:     d,
	})
	for _, opt := range opts {
		opt(w)
	}
	return w
}

func (w *Worker) Run(ctx context.Context) {
	const where = "service.auditevidence.Worker.Run"
	logext.Infof(ctx, "[%s] started,owner:%s", where, w.owner)
	poll := time.NewTicker(DefaultPollInterval)
	expiry := time.NewTicker(DefaultExpiryPoll)
	defer poll.Stop()
	defer expiry.Stop()

	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] stopping,waiting for in-flight work", where)
			w.drain.Drain(ctx)
			return
		case <-poll.C:
			if err := w.processNext(ctx); err != nil {
				logext.Warnf(ctx, "[%s] process failed,err:%+v", where, err.Error())
			}
		case <-expiry.C:
			if n, err := w.jobs.ExpireJobs(ctx, time.Now()); err != nil {
				logext.Warnf(ctx, "[%s] expire failed,err:%+v", where, err.Error())
			} else if n > 0 {
				logext.Infof(ctx, "[%s] expired %d evidence exports", where, n)
				metrics.AuditEvidenceExportsTotal.WithLabelValues("system", "expired").Add(float64(n))
				w.auditExpiry(ctx, n)
			}
			if n, err := w.jobs.RequeueStaleJobs(ctx, DefaultStaleJobThreshold); err != nil {
				logext.Warnf(ctx, "[%s] requeue stale failed,err:%+v", where, err.Error())
			} else if n > 0 {
				logext.Infof(ctx, "[%s] requeued %d stale evidence jobs", where, n)
			}
		}
	}
}

func (w *Worker) processNext(ctx context.Context) error {
	job, err := w.jobs.ClaimNextJob(ctx, w.owner)
	if err != nil || job == nil {
		return err
	}

	w.drain.Enter()
	defer w.drain.Leave()

	start := time.Now()

	jobCtx, cancelJob := context.WithCancel(ctx)
	defer cancelJob()

	go w.heartbeat(jobCtx, job.ID, cancelJob)

	entries, err := w.fetchAllEntries(jobCtx, job.TenantID, job.FilterJSON)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logext.Warnf(ctx, "[auditevidence.Worker.processNext] job aborted (lease lost),job_id:%s", job.ID)
			return nil
		}
		metrics.AuditEvidenceExportsTotal.WithLabelValues(job.TenantID, "failed").Inc()
		_, _ = w.jobs.FailJob(ctx, job.ID, w.owner, err.Error())
		return err
	}

	archive, err := buildArchive(archiveParams{
		jobID:         job.ID,
		tenantID:      job.TenantID,
		createdByType: job.CreatedByType,
		createdBy:     job.CreatedBy,
		filterJSON:    job.FilterJSON,
		entries:       entries,
		signKey:       w.signKey,
	})
	if err != nil {
		metrics.AuditEvidenceExportsTotal.WithLabelValues(job.TenantID, "failed").Inc()
		_, _ = w.jobs.FailJob(ctx, job.ID, w.owner, err.Error())
		return err
	}

	if n, err := w.jobs.CompleteJob(
		ctx,
		job.ID,
		w.owner,
		archive.filename,
		archive.data,
		archive.totalEvents,
		time.Now().UTC().Add(w.exportTTL),
	); err != nil {
		return err
	} else if n == 0 {
		logext.Warnf(ctx, "[auditevidence.Worker.processNext] job re-claimed,job_id:%s", job.ID)
		return nil
	}

	metrics.AuditEvidenceExportsTotal.WithLabelValues(job.TenantID, "completed").Inc()
	metrics.AuditEvidenceExportDuration.WithLabelValues(job.TenantID).Observe(time.Since(start).Seconds())
	metrics.AuditEvidenceExportSizeBytes.WithLabelValues(job.TenantID).Observe(float64(len(archive.data)))

	return nil
}

func (w *Worker) fetchAllEntries(ctx context.Context, tenantID string, filterJSON json.RawMessage) ([]auditlogrepo.Entry, error) {
	var f ExportFilter
	if len(filterJSON) > 0 {
		if err := json.Unmarshal(filterJSON, &f); err != nil {
			return nil, err
		}
	}

	var all []auditlogrepo.Entry
	cursor := ""
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		result, err := w.auditLogs.List(ctx, auditlogsvc.ListFilter{
			TenantID:   tenantID,
			Actions:    f.Actions,
			ActorType:  f.ActorType,
			ActorID:    f.ActorID,
			TargetType: f.TargetType,
			TargetID:   f.TargetID,
			From:       f.From,
			To:         f.To,
			Limit:      500,
			Cursor:     cursor,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, result.Items...)
		if result.NextCursor == "" || len(all) >= DefaultMaxEventsLimit {
			break
		}
		cursor = result.NextCursor
	}
	return all, nil
}

func (w *Worker) heartbeat(ctx context.Context, jobID string, cancelJob context.CancelFunc) {
	const where = "auditevidence.Worker.heartbeat"
	ticker := time.NewTicker(DefaultHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hbCtx, hbCancel := context.WithTimeout(context.Background(), 10*time.Second)
			n, err := w.jobs.HeartbeatJob(hbCtx, jobID, w.owner)
			hbCancel()
			if err != nil {
				logext.Warnf(ctx, "[%s] heartbeat failed,job_id:%s,err:%+v", where, jobID, err.Error())
			} else if n == 0 {
				logext.Warnf(ctx, "[%s] job re-claimed,job_id:%s", where, jobID)
				cancelJob()
				return
			}
		}
	}
}

func normalizeStoredActorType(actorType, actorID string) string {
	trimmedType := strings.TrimSpace(actorType)
	if trimmedType != "" {
		return trimmedType
	}
	if strings.HasPrefix(strings.TrimSpace(actorID), "apikey:") {
		return "api_key"
	}
	return "admin"
}

func (w *Worker) auditExpiry(ctx context.Context, count int64) {
	if w.audit == nil {
		return
	}
	_ = w.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   "system",
		Actor:      auditlogsvc.Actor{Type: "system", ID: "audit-evidence-worker"},
		Action:     "audit_evidence.expire",
		TargetType: "audit_evidence_export",
		Summary:    "Expired audit evidence export archives",
		After: map[string]any{
			"expired_count": count,
		},
	})
}

func (w *Worker) ProcessNextForTest(ctx context.Context) error {
	return w.processNext(ctx)
}
