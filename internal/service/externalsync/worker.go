// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/workerdrain"
	repo "github.com/Phixsura/attune/internal/repo/externalsync"
)

const (
	workerName            = "external_sync"
	defaultPoll           = 5 * time.Second
	defaultBatchSize      = 10
	defaultAttempts       = 5
	defaultHeartbeat      = 2 * time.Minute
	maxProviderRetryAfter = 24 * time.Hour
)

type Worker struct {
	service     *Service
	owner       string
	drain       *workerdrain.Drainer
	poll        time.Duration
	batchSize   int
	maxAttempts int
	heartbeat   time.Duration
}

func NewWorker(service *Service) *Worker {
	drain := workerdrain.New(workerName)
	return ptrext.Of(Worker{
		service:     service,
		owner:       workerName + "-" + uuid.NewString(),
		drain:       drain,
		poll:        defaultPoll,
		batchSize:   defaultBatchSize,
		maxAttempts: defaultAttempts,
		heartbeat:   defaultHeartbeat,
	})
}

func (w *Worker) Configure(poll time.Duration, batchSize, maxAttempts int) {
	if poll > 0 {
		w.poll = poll
	}
	if batchSize > 0 {
		w.batchSize = batchSize
	}
	if maxAttempts > 0 {
		w.maxAttempts = maxAttempts
	}
}

func (w *Worker) Run(ctx context.Context) {
	const where = "service.externalsync.Worker.Run"
	logext.Infof(ctx, "[%s] started,owner:%s,poll_interval:%s,batch:%d,max_attempts:%d",
		where, w.owner, w.poll, w.batchSize, w.maxAttempts)
	tick := time.NewTicker(w.poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] stopping", where)
			w.drain.Drain(ctx)
			return
		case <-tick.C:
			w.ProcessOnce(ctx)
		}
	}
}

func (w *Worker) ProcessOnce(ctx context.Context) {
	const where = "service.externalsync.Worker.ProcessOnce"
	runs, err := w.service.repo.ClaimBatch(ctx, w.batchSize, w.owner)
	if err != nil {
		logext.Errorf(ctx, "[%s] claim failed,err:%+v", where, err.Error())
		return
	}
	for _, run := range runs {
		w.drain.Enter()
		w.processRun(ctx, run)
		w.drain.Leave()
	}
}

func (w *Worker) processRun(ctx context.Context, run repo.SyncRun) {
	const where = "service.externalsync.Worker.processRun"
	logext.Infof(ctx, "[%s] start,run_id:%s,tenant_id:%s,provider_connection:%s,attempts:%d",
		where, run.ID.String(), run.TenantID, run.ConnectionID.String(), run.Attempts)
	start := time.Now()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go w.heartbeatRun(runCtx, run.ID, cancel)
	result, err := w.service.ProcessRun(runCtx, run)
	if err != nil {
		w.markFailed(ctx, run, result, err, time.Since(start))
		return
	}
	if n, err := w.service.repo.MarkRunSucceeded(ctx, run.ID, w.owner); err != nil {
		logext.Warnf(ctx, "[%s] mark succeeded failed,run_id:%s,err:%+v", where, run.ID.String(), err.Error())
	} else if n == 0 {
		logext.Warnf(ctx, "[%s] run lease lost before success mark,run_id:%s", where, run.ID.String())
	}
	w.recordRunMetrics(ctx, result, time.Since(start))
	logext.Infof(ctx, "[%s] OK,run_id:%s", where, run.ID.String())
}

func (w *Worker) heartbeatRun(ctx context.Context, runID uuid.UUID, cancel context.CancelFunc) {
	const where = "service.externalsync.Worker.heartbeatRun"
	ticker := time.NewTicker(w.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := w.service.repo.RefreshRunClaim(ctx, runID, w.owner)
			if err != nil {
				logext.Warnf(ctx, "[%s] refresh failed,run_id:%s,err:%+v", where, runID.String(), err.Error())
				continue
			}
			if n == 0 {
				logext.Warnf(ctx, "[%s] run lease lost,run_id:%s", where, runID.String())
				cancel()
				return
			}
		}
	}
}

func (w *Worker) markFailed(ctx context.Context, run repo.SyncRun, result ProcessResult, err error, elapsed time.Duration) {
	const where = "service.externalsync.Worker.markFailed"
	dead := run.Attempts >= w.maxAttempts
	delay := backoff(run.Attempts)
	kind, retryable, retryAfter, classified := processRunErrorInfo(err)
	if kind == "" {
		kind = "other"
	}
	switch {
	case errors.Is(err, ErrProviderUnavailable):
		kind = "provider_unavailable"
	case errors.Is(err, ErrValidation):
		kind = "validation_error"
		dead = true
		delay = 0
	case classified && !retryable:
		dead = true
		delay = 0
	}
	if !dead {
		delay = retryAfterDelay(time.Now(), retryAfter, delay)
	}
	if n, markErr := w.service.repo.MarkRunFailed(ctx, run.ID, w.owner, kind, err.Error(), delay, dead); markErr != nil {
		logext.Warnf(ctx, "[%s] mark failed failed,run_id:%s,err:%+v", where, run.ID.String(), markErr.Error())
	} else if n == 0 {
		logext.Warnf(ctx, "[%s] run lease lost before failure mark,run_id:%s", where, run.ID.String())
	} else if quarantined, quarantineErr := w.service.repo.QuarantineDegradedConnection(
		ctx,
		run.TenantID,
		run.ConnectionID,
		kind+": "+err.Error(),
	); quarantineErr != nil {
		logext.Warnf(ctx, "[%s] quarantine check failed,run_id:%s,connection_id:%s,err:%+v",
			where, run.ID.String(), run.ConnectionID.String(), quarantineErr.Error())
	} else if quarantined > 0 {
		logext.Warnf(ctx, "[%s] connection quarantined,run_id:%s,connection_id:%s,reason:%s",
			where, run.ID.String(), run.ConnectionID.String(), kind)
	}
	status := "failed"
	if dead {
		status = "dead"
	}
	result.Status = status
	w.recordRunMetrics(ctx, result, elapsed)
	logext.Warnf(ctx, "[%s] failed,run_id:%s,dead:%t,err:%s", where, run.ID.String(), dead, err.Error())
}

func (w *Worker) recordRunMetrics(ctx context.Context, result ProcessResult, elapsed time.Duration) {
	provider := metricLabel(result.Provider)
	objectType := metricLabel(result.ExternalObjectType)
	status := metricLabel(result.Status)
	metrics.ExternalSyncRunsTotal.WithLabelValues(provider, objectType, status).Inc()
	metrics.ExternalSyncRunDuration.WithLabelValues(provider, objectType, status).Observe(elapsed.Seconds())
	for _, operation := range result.OperationStats {
		recordOperationMetrics(provider, objectType, operation)
	}
	w.refreshSnapshotMetrics(ctx)
}

func recordOperationMetrics(provider, objectType string, operation ProcessOperationStats) {
	name := metricLabel(operation.Operation)
	addCounter(metrics.ExternalSyncRecordsTotal.WithLabelValues(provider, objectType, name, "seen"), operation.Stats.RecordsSeen)
	addCounter(metrics.ExternalSyncRecordsTotal.WithLabelValues(provider, objectType, name, "changed"), operation.Stats.RecordsChanged)
	addCounter(metrics.ExternalSyncRecordsTotal.WithLabelValues(provider, objectType, name, "failed"), operation.Stats.RecordsFailed)
	addCounter(metrics.ExternalSyncConflictsTotal.WithLabelValues(provider, objectType, "open"), operation.Stats.ConflictsCreated)
}

func (w *Worker) refreshSnapshotMetrics(ctx context.Context) {
	const where = "service.externalsync.Worker.refreshSnapshotMetrics"
	snapshot, err := w.service.repo.MetricSnapshot(ctx)
	if err != nil {
		logext.Warnf(ctx, "[%s] snapshot failed,err:%+v", where, err.Error())
		return
	}
	for _, point := range snapshot.Points {
		provider := metricLabel(point.Provider)
		objectType := metricLabel(point.ExternalObjectType)
		metrics.ExternalSyncDeadRuns.WithLabelValues(provider, objectType).Set(float64(point.DeadRuns))
		metrics.ExternalSyncLagSeconds.WithLabelValues(provider, objectType).Set(nonNegative(point.LagSeconds))
	}
}

func addCounter(counter interface{ Add(float64) }, value int) {
	if value > 0 {
		counter.Add(float64(value))
	}
}

func nonNegative(value float64) float64 {
	if value < 0 {
		return 0
	}
	return value
}

func metricLabel(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func backoff(attempts int) time.Duration {
	switch {
	case attempts <= 1:
		return 30 * time.Second
	case attempts == 2:
		return 2 * time.Minute
	case attempts == 3:
		return 10 * time.Minute
	default:
		return time.Hour
	}
}

func retryAfterDelay(now time.Time, retryAfter *time.Time, fallback time.Duration) time.Duration {
	if retryAfter == nil {
		return fallback
	}
	delay := ceilDuration(retryAfter.Sub(now), time.Second)
	if delay <= fallback {
		return fallback
	}
	if delay > maxProviderRetryAfter {
		return maxProviderRetryAfter
	}
	return delay
}

func ceilDuration(value, unit time.Duration) time.Duration {
	if value <= 0 || unit <= 0 {
		return 0
	}
	return ((value + unit - 1) / unit) * unit
}
