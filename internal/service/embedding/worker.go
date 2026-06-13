// SPDX-License-Identifier: Apache-2.0

// Package embedding provides the embedding worker that processes the
// embedding_task outbox and assigns feedback to semantic clusters.
package embedding

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/embedding"
	"github.com/Phixsura/attune/internal/repo/llmaudit"
	"github.com/Phixsura/attune/internal/service/llmrouter"
)

// Worker processes embedding tasks from the outbox.
type Worker struct {
	taskRepo  *embedding.TaskRepo
	router    *llmrouter.Router
	llmClient llmclient.LLMClient
	auditRepo *llmaudit.Repo

	pollInterval  time.Duration
	staleDuration time.Duration
	maxAttempts   int
	dimensions    int
	threshold     float64
	recencyDays   int
}

// NewWorker creates an embedding worker.
func NewWorker(
	taskRepo *embedding.TaskRepo,
	router *llmrouter.Router,
	llmClient llmclient.LLMClient,
	auditRepo *llmaudit.Repo,
) *Worker {
	return ptrext.Of(Worker{
		taskRepo:      taskRepo,
		router:        router,
		llmClient:     llmClient,
		auditRepo:     auditRepo,
		pollInterval:  5 * time.Second,
		staleDuration: 5 * time.Minute,
		maxAttempts:   5,
		dimensions:    256,
		threshold:     0.85,
		recencyDays:   30,
	})
}

// Configure overrides defaults.
func (w *Worker) Configure(pollInterval time.Duration, dimensions, maxAttempts int, threshold float64) {
	if pollInterval > 0 {
		w.pollInterval = pollInterval
	}
	if dimensions > 0 {
		w.dimensions = dimensions
	}
	if maxAttempts > 0 {
		w.maxAttempts = maxAttempts
	}
	if threshold > 0 {
		w.threshold = threshold
	}
}

// Run loops on ctx until cancellation.
func (w *Worker) Run(ctx context.Context) {
	const where = "embedding.Worker.Run"
	if reset, err := w.taskRepo.ResetStaleClaims(ctx, w.staleDuration); err != nil {
		logext.Warnf(ctx, "[%s] reset stale claims failed,err:%+v", where, err.Error())
	} else if reset > 0 {
		logext.Infof(ctx, "[%s] reset stale claims,count:%d", where, reset)
	}
	logext.Infof(ctx, "[%s] embedding worker started,poll_interval:%s,dims:%d,threshold:%.2f",
		where, w.pollInterval, w.dimensions, w.threshold)

	tick := time.NewTicker(w.pollInterval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			logext.Infof(ctx, "[%s] embedding worker stopping", where)
			return
		case <-tick.C:
			w.ProcessOnce(ctx)
		}
	}
}

// ProcessOnce processes one task. Exposed for testing.
func (w *Worker) ProcessOnce(ctx context.Context) {
	const where = "embedding.Worker.ProcessOnce"
	task, err := w.taskRepo.TryClaim(ctx, w.staleDuration)
	if err != nil {
		if !errors.Is(err, embedding.ErrNoTask) {
			logext.Errorf(ctx, "[%s] try claim failed,err:%+v", where, err.Error())
		}
		return
	}
	w.processTask(ctx, task)
}

func (w *Worker) processTask(ctx context.Context, task *embedding.Task) {
	const where = "embedding.Worker.processTask"
	start := time.Now()

	content, err := w.taskRepo.GetFeedbackContent(ctx, task.FeedbackID)
	if err != nil {
		logext.Errorf(ctx, "[%s] get content failed,task_id:%d,err:%+v", where, task.ID, err.Error())
		_ = w.taskRepo.MarkFailed(ctx, task.ID, err, w.maxAttempts)
		return
	}

	resp, err := w.router.Embed(ctx, llmrouter.EmbeddingRequest{
		TenantID:   task.TenantID,
		Input:      []string{content},
		Dimensions: w.dimensions,
		UserID:     fmt.Sprintf("feedback:%d", task.FeedbackID),
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] embed failed,task_id:%d,err:%+v", where, task.ID, err.Error())
		_ = w.taskRepo.MarkFailed(ctx, task.ID, err, w.maxAttempts)
		metrics.EmbedErrors.WithLabelValues(task.TenantID, "embed_api").Inc()
		return
	}
	if len(resp.Embeddings) == 0 || len(resp.Embeddings[0]) == 0 {
		err := fmt.Errorf("empty embedding response")
		logext.Errorf(ctx, "[%s] %s,task_id:%d", where, err.Error(), task.ID)
		_ = w.taskRepo.MarkFailed(ctx, task.ID, err, w.maxAttempts)
		return
	}

	emb := resp.Embeddings[0]
	model := resp.Route.ProviderModel

	cfg, err := w.taskRepo.GetClusteringConfig(ctx, task.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] get clustering config failed,task_id:%d,err:%+v", where, task.ID, err.Error())
		_ = w.taskRepo.MarkFailed(ctx, task.ID, err, w.maxAttempts)
		return
	}
	threshold := cfg.Threshold
	if threshold <= 0 {
		threshold = w.threshold // fallback to worker default
	}

	similar, err := w.taskRepo.FindSimilar(ctx, embedding.FindSimilarOpts{
		TenantID:       task.TenantID,
		Embedding:      emb,
		EmbeddingModel: model,
		Threshold:      threshold,
		RecencyDays:    w.recencyDays,
		EfSearch:       200,
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] find similar failed,task_id:%d,err:%+v", where, task.ID, err.Error())
		_ = w.taskRepo.MarkFailed(ctx, task.ID, err, w.maxAttempts)
		return
	}

	var clusterID uuid.UUID
	isNewCluster := false
	if similar != nil {
		clusterID = similar.ClusterID
	} else {
		clusterID = uuid.New()
		isNewCluster = true
	}

	if err := w.taskRepo.UpdateEmbedding(ctx, task.FeedbackID, embedding.FeedbackEmbedding{
		Embedding:      emb,
		EmbeddingModel: model,
		EmbeddingDims:  len(emb),
		ClusterID:      clusterID,
	}); err != nil {
		logext.Errorf(ctx, "[%s] update embedding failed,task_id:%d,err:%+v", where, task.ID, err.Error())
		_ = w.taskRepo.MarkFailed(ctx, task.ID, err, w.maxAttempts)
		return
	}

	if !isNewCluster {
		w.maybeGenerateClusterLabel(ctx, task.TenantID, clusterID)
	}

	if err := w.taskRepo.MarkDone(ctx, task.ID); err != nil {
		logext.Errorf(ctx, "[%s] mark done failed,task_id:%d,err:%+v", where, task.ID, err.Error())
	}

	w.writeAudit(ctx, task, resp, start)

	clusterType := "existing"
	if isNewCluster {
		clusterType = "new"
	}
	metrics.EmbedClusterAssignments.WithLabelValues(task.TenantID, clusterType).Inc()
	metrics.EmbedDuration.WithLabelValues(task.TenantID).Observe(time.Since(start).Seconds())

	logext.Infof(ctx, "[%s] OK,task_id:%d,feedback_id:%d,cluster_id:%s,is_new:%t,latency_ms:%d",
		where, task.ID, task.FeedbackID, clusterID, isNewCluster, time.Since(start).Milliseconds())
}

func (w *Worker) maybeGenerateClusterLabel(ctx context.Context, tenantID string, clusterID uuid.UUID) {
	const where = "embedding.Worker.maybeGenerateClusterLabel"
	info, err := w.taskRepo.GetClusterInfo(ctx, tenantID, clusterID)
	if err != nil || info.Count < 3 || info.Label != "" {
		return
	}

	titles, err := w.taskRepo.GetClusterTitles(ctx, tenantID, clusterID, 5)
	if err != nil || len(titles) < 3 {
		return
	}

	label, err := w.generateLabel(ctx, tenantID, titles)
	if err != nil {
		logext.Warnf(ctx, "[%s] generate label failed,cluster_id:%s,err:%+v", where, clusterID, err.Error())
		return
	}

	if err := w.taskRepo.UpdateClusterLabel(ctx, tenantID, clusterID, label); err != nil {
		logext.Warnf(ctx, "[%s] update label failed,cluster_id:%s,err:%+v", where, clusterID, err.Error())
	}
}

func (w *Worker) generateLabel(ctx context.Context, tenantID string, titles []string) (string, error) {
	if w.router == nil {
		return "", fmt.Errorf("llm router not configured")
	}

	prompt := fmt.Sprintf(`Below are sample feedback titles from the same cluster.
Generate a short label (3-5 words) summarizing the common theme.
Return ONLY the label, no explanation.

Sample titles:
%s`, strings.Join(titles, "\n"))

	resp, err := w.router.Complete(ctx, llmclient.CompletionRequest{
		Prompt: prompt,
		Guard: llmclient.GuardMetadata{
			TenantID: tenantID,
			Purpose:  "cluster_label",
		},
	})
	if err != nil {
		return "", fmt.Errorf("llm complete: %w", err)
	}

	label := strings.TrimSpace(resp.Text)
	if len(label) > 50 {
		label = label[:50]
	}
	return label, nil
}

func (w *Worker) writeAudit(ctx context.Context, task *embedding.Task, resp llmrouter.EmbeddingResponse, start time.Time) {
	if w.auditRepo == nil {
		return
	}
	_ = w.auditRepo.Insert(ctx, llmaudit.Row{
		TenantID:        task.TenantID,
		FeedbackID:      task.FeedbackID,
		Purpose:         "embed",
		ModelID:         resp.Route.ProviderModel,
		PromptTokens:    resp.Usage.PromptTokens,
		ChannelID:       resp.Route.ChannelID,
		ProviderModelID: resp.Route.ProviderModel,
		LLMProtocol:     resp.Route.Protocol,
		LatencyMS:       int(time.Since(start).Milliseconds()),
		Status:          "ok",
	})
}
