// SPDX-License-Identifier: Apache-2.0

// Package embedding provides repository methods for embedding tasks and
// feedback embedding columns.
package embedding

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/taskoutbox"
)

// VecToString converts a []float32 to pgvector's text format: "[1.0,2.0,3.0]".
func VecToString(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

var ErrTaskNotFound = errors.New("embedding task not found")

// ErrNoTask aliases the generic queue sentinel so existing callers keep
// using embedding.ErrNoTask.
var ErrNoTask = taskoutbox.ErrNoTask

// TaskRepo wraps the embedding_task outbox table. The generic claim/retry
// machinery lives in taskoutbox.Queue; embedding-specific enqueue and
// vector/cluster methods stay here.
type TaskRepo struct {
	pool *pgxpool.Pool
	q    *taskoutbox.Queue
}

func NewTaskRepo(pool *pgxpool.Pool) *TaskRepo {
	return ptrext.Of(TaskRepo{
		pool: pool,
		q:    taskoutbox.New(pool, "embedding_task", "clustering_enabled"),
	})
}

// Task aliases the generic outbox row so existing callers keep using
// embedding.Task.
type Task = taskoutbox.Task

// CreateTask inserts a new embedding task for a feedback item.
func (r *TaskRepo) CreateTask(ctx context.Context, feedbackID int64, tenantID string) (int64, error) {
	var id int64
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO embedding_task (feedback_id, tenant_id, status)
		VALUES ($1, $2, 'pending')
		ON CONFLICT (feedback_id) DO NOTHING
		RETURNING id`,
		feedbackID, tenantID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("create embedding task: %w", err)
	}
	return id, nil
}

// CreateTaskTx inserts a new embedding task within a transaction.
// Only creates the task if clustering is enabled for the tenant.
func (r *TaskRepo) CreateTaskTx(ctx context.Context, tx pgx.Tx, feedbackID int64, tenantID string) error {
	_, err := tx.Exec(
		ctx, `
		INSERT INTO embedding_task (feedback_id, tenant_id, status)
		SELECT $1, $2, 'pending'
		WHERE EXISTS (
			SELECT 1 FROM tenants
			WHERE id = $2 AND clustering_enabled = TRUE
		)
		ON CONFLICT (feedback_id) DO NOTHING`,
		feedbackID, tenantID,
	)
	if err != nil {
		return fmt.Errorf("create embedding task tx: %w", err)
	}
	return nil
}

// BackfillTasks queues unembedded feedback for embedding processing.
// If force is true, re-queues already embedded feedback.
// Returns the number of tasks created.
func (r *TaskRepo) BackfillTasks(ctx context.Context, tenantID string, batchSize int, force bool) (int64, error) {
	whereClause := "embedding IS NULL"
	if force {
		whereClause = "TRUE"
	}
	result, err := r.pool.Exec(ctx, fmt.Sprintf(
		`
		INSERT INTO embedding_task (feedback_id, tenant_id, status)
		SELECT id, tenant_id, 'pending'
		FROM user_feedback
		WHERE tenant_id = $1
		  AND enrichment_status = 'done'
		  AND %s
		  AND NOT EXISTS (
		      SELECT 1 FROM embedding_task et WHERE et.feedback_id = user_feedback.id
		  )
		LIMIT $2
		ON CONFLICT (feedback_id) DO NOTHING`,
		whereClause,
	), tenantID, batchSize)
	if err != nil {
		return 0, fmt.Errorf("backfill embedding tasks: %w", err)
	}
	return result.RowsAffected(), nil
}

// TryClaim atomically claims one eligible embedding task. Delegates to the
// generic outbox queue (gated on tenants.clustering_enabled).
func (r *TaskRepo) TryClaim(ctx context.Context, staleDuration time.Duration) (*Task, error) {
	return r.q.TryClaim(ctx, staleDuration)
}

// MarkDone marks a task completed.
func (r *TaskRepo) MarkDone(ctx context.Context, taskID int64) error {
	return r.q.MarkDone(ctx, taskID)
}

// MarkFailed records a failure with retry backoff.
func (r *TaskRepo) MarkFailed(ctx context.Context, taskID int64, lastErr error, maxAttempts int) error {
	return r.q.MarkFailed(ctx, taskID, lastErr, maxAttempts)
}

// ResetStaleClaims recovers tasks stuck in processing.
func (r *TaskRepo) ResetStaleClaims(ctx context.Context, staleDuration time.Duration) (int64, error) {
	return r.q.ResetStaleClaims(ctx, staleDuration)
}

// QueueDepth returns outstanding tasks for a tenant.
func (r *TaskRepo) QueueDepth(ctx context.Context, tenantID string) (int64, error) {
	return r.q.QueueDepth(ctx, tenantID)
}

// FeedbackEmbedding holds embedding data for a feedback item.
type FeedbackEmbedding struct {
	Embedding      []float32
	EmbeddingModel string
	EmbeddingDims  int
	ClusterID      uuid.UUID
}

// UpdateEmbedding stores the embedding and cluster assignment atomically.
func (r *TaskRepo) UpdateEmbedding(
	ctx context.Context,
	feedbackID int64,
	emb FeedbackEmbedding,
) error {
	_, err := r.pool.Exec(
		ctx, `
		UPDATE user_feedback
		SET embedding = $2::vector,
		    embedding_model = $3,
		    embedding_dims = $4,
		    embedded_at = NOW(),
		    cluster_id = $5,
		    cluster_assigned_at = NOW()
		WHERE id = $1`,
		feedbackID, VecToString(emb.Embedding), emb.EmbeddingModel, emb.EmbeddingDims, emb.ClusterID,
	)
	if err != nil {
		return fmt.Errorf("update feedback embedding: %w", err)
	}
	return nil
}

// FindSimilarOpts are options for FindSimilar queries.
type FindSimilarOpts struct {
	TenantID       string
	Embedding      []float32
	EmbeddingModel string
	Threshold      float64
	RecencyDays    int
	EfSearch       int
}

// SimilarRow is the result of a similarity search.
type SimilarRow struct {
	ID         int64
	ClusterID  uuid.UUID
	Similarity float64
}

// FindSimilar finds the most similar feedback with an existing cluster.
// Uses iterative-scan pattern to handle pgvector's post-filter behavior.
// Runs in a transaction to ensure SET LOCAL ef_search takes effect.
func (r *TaskRepo) FindSimilar(ctx context.Context, opts FindSimilarOpts) (*SimilarRow, error) {
	if opts.EfSearch == 0 {
		opts.EfSearch = 200
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin find similar tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, fmt.Sprintf("SET LOCAL hnsw.ef_search = %d", opts.EfSearch))
	if err != nil {
		return nil, fmt.Errorf("set ef_search: %w", err)
	}

	row := tx.QueryRow(
		ctx, `
		SELECT id, cluster_id, 1 - (embedding <=> $2::vector) AS similarity
		FROM user_feedback
		WHERE tenant_id = $1
		  AND embedding IS NOT NULL
		  AND cluster_id IS NOT NULL
		  AND embedding_model = $5
		  AND created_at > NOW() - make_interval(days => $4)
		  AND 1 - (embedding <=> $2::vector) > $3
		ORDER BY embedding <=> $2::vector
		LIMIT 1`,
		opts.TenantID, VecToString(opts.Embedding), opts.Threshold, opts.RecencyDays, opts.EmbeddingModel,
	)

	var s SimilarRow
	if err := row.Scan(&s.ID, &s.ClusterID, &s.Similarity); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find similar: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit find similar tx: %w", err)
	}
	return ptrext.Of(s), nil
}

// GetFeedbackContent retrieves the content field for embedding generation.
func (r *TaskRepo) GetFeedbackContent(ctx context.Context, feedbackID int64) (string, error) {
	var content string
	err := r.pool.QueryRow(
		ctx, `
		SELECT content FROM user_feedback WHERE id = $1`,
		feedbackID,
	).Scan(&content)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrTaskNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get feedback content: %w", err)
	}
	return content, nil
}

// GetClusterInfo returns info about a cluster.
type ClusterInfo struct {
	Count int
	Label string
}

func (r *TaskRepo) GetClusterInfo(ctx context.Context, tenantID string, clusterID uuid.UUID) (*ClusterInfo, error) {
	var info ClusterInfo
	err := r.pool.QueryRow(
		ctx, `
		SELECT COUNT(*), COALESCE(MAX(cluster_label), '')
		FROM user_feedback
		WHERE tenant_id = $1 AND cluster_id = $2`,
		tenantID, clusterID,
	).Scan(&info.Count, &info.Label)
	if err != nil {
		return nil, fmt.Errorf("get cluster info: %w", err)
	}
	return ptrext.Of(info), nil
}

// GetClusterTitles returns sample enriched titles from a cluster.
func (r *TaskRepo) GetClusterTitles(ctx context.Context, tenantID string, clusterID uuid.UUID, limit int) ([]string, error) {
	rows, err := r.pool.Query(
		ctx, `
		SELECT COALESCE(enriched_title, LEFT(content, 100))
		FROM user_feedback
		WHERE tenant_id = $1 AND cluster_id = $2
		  AND (enriched_title IS NOT NULL OR content <> '')
		ORDER BY created_at DESC
		LIMIT $3`,
		tenantID, clusterID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get cluster titles: %w", err)
	}
	defer rows.Close()

	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			return nil, err
		}
		titles = append(titles, title)
	}
	return titles, rows.Err()
}

// UpdateClusterLabel sets the cluster label on all feedback in the cluster.
func (r *TaskRepo) UpdateClusterLabel(ctx context.Context, tenantID string, clusterID uuid.UUID, label string) error {
	_, err := r.pool.Exec(
		ctx, `
		UPDATE user_feedback
		SET cluster_label = $3
		WHERE tenant_id = $1 AND cluster_id = $2`,
		tenantID, clusterID, label,
	)
	if err != nil {
		return fmt.Errorf("update cluster label: %w", err)
	}
	return nil
}

// ClusterListOpts are options for ListClusters.
type ClusterListOpts struct {
	RecencyDays int
	MinCount    int
	Limit       int
	Cursor      string // keyset cursor: "unix_ts:uuid"
	Sort        string // "count" or "latest_at" (default)
	Query       string // search in cluster label
}

// ClusterSummary is a condensed view of a cluster.
type ClusterSummary struct {
	ClusterID   uuid.UUID
	Count       int
	LatestAt    time.Time
	Label       string
	SampleTitle string
}

// ClusterListResult holds cluster list with pagination metadata.
type ClusterListResult struct {
	Items      []ClusterSummary
	NextCursor string
	TotalCount int
}

// ListClusters returns cluster summaries for a tenant with cursor pagination.
func (r *TaskRepo) ListClusters(ctx context.Context, tenantID string, opts ClusterListOpts) (*ClusterListResult, error) {
	opts = normalizeClusterOpts(opts)
	cursorTime, cursorID, err := parseClusterCursor(opts.Cursor)
	if err != nil {
		return nil, err
	}

	totalCount, err := r.countClusters(ctx, tenantID, opts)
	if err != nil {
		return nil, err
	}

	out, err := r.queryClusters(ctx, tenantID, opts, cursorTime, cursorID)
	if err != nil {
		return nil, err
	}

	var nextCursor string
	if len(out) > opts.Limit {
		out = out[:opts.Limit]
		last := out[len(out)-1]
		nextCursor = fmt.Sprintf("%d:%s", last.LatestAt.UnixNano(), last.ClusterID.String())
	}

	return ptrext.Of(ClusterListResult{Items: out, NextCursor: nextCursor, TotalCount: totalCount}), nil
}

func normalizeClusterOpts(opts ClusterListOpts) ClusterListOpts {
	if opts.RecencyDays == 0 {
		opts.RecencyDays = 30
	}
	if opts.MinCount == 0 {
		opts.MinCount = 2
	}
	if opts.Limit == 0 {
		opts.Limit = 50
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}
	if opts.Sort != "count" && opts.Sort != "latest_at" {
		opts.Sort = "latest_at"
	}
	return opts
}

func parseClusterCursor(cursor string) (time.Time, uuid.UUID, error) {
	if cursor == "" {
		return time.Time{}, uuid.Nil, nil
	}
	parts := strings.SplitN(cursor, ":", 2)
	if len(parts) != 2 {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid cursor format")
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid cursor timestamp: %w", err)
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return time.Time{}, uuid.Nil, fmt.Errorf("invalid cursor id: %w", err)
	}
	return time.Unix(0, nanos), id, nil
}

func (r *TaskRepo) countClusters(ctx context.Context, tenantID string, opts ClusterListOpts) (int, error) {
	var count int
	if opts.Query != "" {
		err := r.pool.QueryRow(ctx, `
			SELECT COUNT(*) FROM (
				SELECT cluster_id FROM user_feedback
				WHERE tenant_id = $1 AND cluster_id IS NOT NULL
				  AND created_at > NOW() - make_interval(days => $2)
				GROUP BY cluster_id
				HAVING COUNT(*) >= $3 AND COALESCE(MAX(cluster_label), '') ILIKE '%' || $4 || '%'
			) sub`, tenantID, opts.RecencyDays, opts.MinCount, opts.Query).Scan(&count)
		return count, err
	}
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM (
			SELECT cluster_id FROM user_feedback
			WHERE tenant_id = $1 AND cluster_id IS NOT NULL
			  AND created_at > NOW() - make_interval(days => $2)
			GROUP BY cluster_id HAVING COUNT(*) >= $3
		) sub`, tenantID, opts.RecencyDays, opts.MinCount).Scan(&count)
	return count, err
}

func (r *TaskRepo) queryClusters(ctx context.Context, tenantID string, opts ClusterListOpts, cursorTime time.Time, cursorID uuid.UUID) ([]ClusterSummary, error) {
	orderBy := "MAX(created_at) DESC, cluster_id DESC"
	if opts.Sort == "count" {
		orderBy = "COUNT(*) DESC, cluster_id DESC"
	}

	args := []any{tenantID, opts.RecencyDays, opts.MinCount, opts.Limit + 1}
	cursorClause := ""
	if !cursorTime.IsZero() {
		cursorClause = "AND (MAX(created_at), cluster_id) < ($5, $6)"
		args = append(args, cursorTime, cursorID)
	}
	labelFilter := ""
	if opts.Query != "" {
		labelFilter = fmt.Sprintf("AND COALESCE(MAX(cluster_label), '') ILIKE '%%' || $%d || '%%'", len(args)+1)
		args = append(args, opts.Query)
	}

	query := fmt.Sprintf(`
		SELECT cluster_id, COUNT(*) AS count, MAX(created_at) AS latest_at,
			COALESCE(MAX(cluster_label), (SELECT COALESCE(enriched_title, LEFT(content, 100))
				FROM user_feedback f2 WHERE f2.cluster_id = f.cluster_id ORDER BY f2.created_at DESC LIMIT 1)) AS label,
			(SELECT COALESCE(enriched_title, LEFT(content, 100))
				FROM user_feedback f3 WHERE f3.cluster_id = f.cluster_id ORDER BY f3.created_at DESC LIMIT 1) AS sample_title
		FROM user_feedback f
		WHERE tenant_id = $1 AND cluster_id IS NOT NULL AND created_at > NOW() - make_interval(days => $2)
		GROUP BY cluster_id HAVING COUNT(*) >= $3 %s %s ORDER BY %s LIMIT $4`,
		cursorClause, labelFilter, orderBy)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list clusters: %w", err)
	}
	defer rows.Close()

	var out []ClusterSummary
	for rows.Next() {
		var s ClusterSummary
		var label, sampleTitle *string
		if err := rows.Scan(&s.ClusterID, &s.Count, &s.LatestAt, &label, &sampleTitle); err != nil {
			return nil, fmt.Errorf("scan cluster: %w", err)
		}
		s.Label, s.SampleTitle = ptrext.Indirect(label), ptrext.Indirect(sampleTitle)
		out = append(out, s)
	}
	return out, rows.Err()
}

// ClusterMember is a feedback item within a cluster.
type ClusterMember struct {
	ID            int64
	Content       string
	EnrichedTitle string
	Source        string
	CreatedAt     time.Time
	Similarity    float64
}

// ClusterMembersOpts are options for GetClusterMembers.
type ClusterMembersOpts struct {
	Limit  int
	Cursor string // keyset cursor: "unix_nanos:id"
}

// ClusterMembersResult holds cluster members and metadata.
type ClusterMembersResult struct {
	Items      []ClusterMember
	Label      string
	TotalCount int
	NextCursor string
}

// GetClusterMembers returns feedback items in a cluster with cursor pagination.
func (r *TaskRepo) GetClusterMembers(ctx context.Context, tenantID string, clusterID uuid.UUID, opts ClusterMembersOpts) (*ClusterMembersResult, error) {
	opts = normalizeMemberOpts(opts)

	var result ClusterMembersResult
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(MAX(cluster_label), '') FROM user_feedback WHERE tenant_id = $1 AND cluster_id = $2`,
		tenantID, clusterID).Scan(&result.TotalCount, &result.Label)
	if err != nil {
		return nil, fmt.Errorf("get cluster metadata: %w", err)
	}

	cursorTime, cursorID, err := parseMemberCursor(opts.Cursor)
	if err != nil {
		return nil, err
	}

	result.Items, err = r.queryClusterMembers(ctx, tenantID, clusterID, opts.Limit, cursorTime, cursorID)
	if err != nil {
		return nil, err
	}

	if len(result.Items) > opts.Limit {
		result.Items = result.Items[:opts.Limit]
		last := result.Items[len(result.Items)-1]
		result.NextCursor = fmt.Sprintf("%d:%d", last.CreatedAt.UnixNano(), last.ID)
	}
	return ptrext.Of(result), nil
}

func normalizeMemberOpts(opts ClusterMembersOpts) ClusterMembersOpts {
	if opts.Limit == 0 {
		opts.Limit = 50
	}
	if opts.Limit > 500 {
		opts.Limit = 500
	}
	return opts
}

func parseMemberCursor(cursor string) (time.Time, int64, error) {
	if cursor == "" {
		return time.Time{}, 0, nil
	}
	parts := strings.SplitN(cursor, ":", 2)
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("invalid cursor format")
	}
	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid cursor timestamp: %w", err)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid cursor id: %w", err)
	}
	return time.Unix(0, nanos), id, nil
}

func (r *TaskRepo) queryClusterMembers(ctx context.Context, tenantID string, clusterID uuid.UUID, limit int, cursorTime time.Time, cursorID int64) ([]ClusterMember, error) {
	var refEmbedding string
	_ = r.pool.QueryRow(ctx, `SELECT embedding::text FROM user_feedback WHERE tenant_id = $1 AND cluster_id = $2 AND embedding IS NOT NULL ORDER BY created_at ASC LIMIT 1`,
		tenantID, clusterID).Scan(&refEmbedding)

	args := []any{tenantID, clusterID, limit + 1}
	cursorClause := ""
	if !cursorTime.IsZero() {
		cursorClause = fmt.Sprintf("AND (created_at, id) < ($%d, $%d)", len(args)+1, len(args)+2)
		args = append(args, cursorTime, cursorID)
	}
	simExpr := "0::float8"
	if refEmbedding != "" {
		simExpr = fmt.Sprintf("CASE WHEN embedding IS NOT NULL THEN 1 - (embedding <=> $%d::vector) ELSE 0 END", len(args)+1)
		args = append(args, refEmbedding)
	}

	query := fmt.Sprintf(`SELECT id, content, COALESCE(enriched_title, ''), source, created_at, %s AS similarity
		FROM user_feedback WHERE tenant_id = $1 AND cluster_id = $2 %s ORDER BY created_at DESC, id DESC LIMIT $3`, simExpr, cursorClause)

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get cluster members: %w", err)
	}
	defer rows.Close()

	var out []ClusterMember
	for rows.Next() {
		var m ClusterMember
		if err := rows.Scan(&m.ID, &m.Content, &m.EnrichedTitle, &m.Source, &m.CreatedAt, &m.Similarity); err != nil {
			return nil, fmt.Errorf("scan cluster member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// IsClusteringEnabled checks if clustering is enabled for a tenant.
func (r *TaskRepo) IsClusteringEnabled(ctx context.Context, tenantID string) (bool, error) {
	var enabled bool
	err := r.pool.QueryRow(
		ctx, `
		SELECT clustering_enabled FROM tenants WHERE id = $1`,
		tenantID,
	).Scan(&enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check clustering enabled: %w", err)
	}
	return enabled, nil
}

// ClusteringConfig holds tenant-specific clustering settings.
type ClusteringConfig struct {
	Enabled   bool
	Threshold float64
}

// GetClusteringConfig returns clustering settings for a tenant.
func (r *TaskRepo) GetClusteringConfig(ctx context.Context, tenantID string) (ClusteringConfig, error) {
	var cfg ClusteringConfig
	err := r.pool.QueryRow(
		ctx, `
		SELECT clustering_enabled, clustering_threshold FROM tenants WHERE id = $1`,
		tenantID,
	).Scan(&cfg.Enabled, &cfg.Threshold)
	if errors.Is(err, pgx.ErrNoRows) {
		return ClusteringConfig{Threshold: 0.75}, nil
	}
	if err != nil {
		return ClusteringConfig{}, fmt.Errorf("get clustering config: %w", err)
	}
	return cfg, nil
}
