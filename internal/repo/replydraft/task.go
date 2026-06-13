// SPDX-License-Identifier: Apache-2.0

// Package replydraft provides repository methods for the reply_draft_task
// outbox and the reply_draft column on user_feedback.
package replydraft

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/taskoutbox"
)

// ErrNotFound means the feedback row to draft for does not exist.
var ErrNotFound = errors.New("feedback not found for draft")

// Task aliases the generic outbox row so callers use replydraft.Task.
type Task = taskoutbox.Task

// ErrNoTask re-exports the generic queue sentinel.
var ErrNoTask = taskoutbox.ErrNoTask

// DraftTaskRepo wraps the reply_draft_task outbox plus the reply_draft column
// writes on user_feedback. Claim/retry machinery is delegated to the generic
// taskoutbox.Queue (gated on tenants.reply_draft_enabled).
type DraftTaskRepo struct {
	pool *pgxpool.Pool
	q    *taskoutbox.Queue
}

func NewDraftTaskRepo(pool *pgxpool.Pool) *DraftTaskRepo {
	return ptrext.Of(DraftTaskRepo{
		pool: pool,
		q:    taskoutbox.New(pool, "reply_draft_task", "reply_draft_enabled"),
	})
}

// CreateTaskTx enqueues a draft task inside the enrich tx, but only when the
// tenant opted in AND classification confidence clears the per-tenant
// threshold. A nil confidence is admitted only when the threshold is 0 (no
// self-rating → don't spend tokens once a gate is set).
func (r *DraftTaskRepo) CreateTaskTx(ctx context.Context, tx pgx.Tx, feedbackID int64, tenantID string, confidence *float64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO reply_draft_task (feedback_id, tenant_id, status)
		SELECT $1, $2, 'pending'
		WHERE EXISTS (
			SELECT 1 FROM tenants
			WHERE id = $2 AND reply_draft_enabled = TRUE
			  AND (reply_draft_min_confidence = 0
			       OR ($3::float8 IS NOT NULL AND $3 >= reply_draft_min_confidence))
		)
		ON CONFLICT (feedback_id) DO NOTHING`,
		feedbackID, tenantID, confidence)
	if err != nil {
		return fmt.Errorf("create reply draft task: %w", err)
	}
	return nil
}

// TryClaim claims one eligible draft task (gated on reply_draft_enabled).
func (r *DraftTaskRepo) TryClaim(ctx context.Context, staleDuration time.Duration) (*Task, error) {
	return r.q.TryClaim(ctx, staleDuration)
}

// MarkDone marks a task completed.
func (r *DraftTaskRepo) MarkDone(ctx context.Context, taskID int64) error {
	return r.q.MarkDone(ctx, taskID)
}

// MarkFailed records a failure with retry backoff.
func (r *DraftTaskRepo) MarkFailed(ctx context.Context, taskID int64, lastErr error, maxAttempts int) error {
	return r.q.MarkFailed(ctx, taskID, lastErr, maxAttempts)
}

// ResetStaleClaims recovers tasks stuck in processing.
func (r *DraftTaskRepo) ResetStaleClaims(ctx context.Context, staleDuration time.Duration) (int64, error) {
	return r.q.ResetStaleClaims(ctx, staleDuration)
}

// QueueDepth returns outstanding tasks for a tenant.
func (r *DraftTaskRepo) QueueDepth(ctx context.Context, tenantID string) (int64, error) {
	return r.q.QueueDepth(ctx, tenantID)
}

// DraftInput is everything renderDraftPrompt needs to build the prompt.
type DraftInput struct {
	Content        string
	EnrichedTitle  string
	Language       string
	Attrs          map[string]any // decoded enriched_attrs; e.g. Attrs["sentiment"]
	TenantID       string
	PromptTemplate string // per-tenant override; "" → default template
}

// LoadForDraft loads the content + enriched fields + the tenant's optional
// prompt override for one feedback row.
func (r *DraftTaskRepo) LoadForDraft(ctx context.Context, feedbackID int64, tenantID string) (*DraftInput, error) {
	var (
		in        DraftInput
		attrsJSON []byte
	)
	err := r.pool.QueryRow(ctx, `
		SELECT f.content,
		       COALESCE(f.enriched_title, ''),
		       COALESCE(f.enriched_attrs, '{}'::jsonb),
		       COALESCE(f.language, ''),
		       f.tenant_id,
		       COALESCE(t.reply_draft_prompt_template, '')
		FROM user_feedback f
		JOIN tenants t ON t.id = f.tenant_id
		WHERE f.id = $1 AND f.tenant_id = $2`,
		feedbackID, tenantID,
	).Scan(&in.Content, &in.EnrichedTitle, &attrsJSON, &in.Language, &in.TenantID, &in.PromptTemplate)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load for draft: %w", err)
	}
	if len(attrsJSON) > 0 {
		if err := json.Unmarshal(attrsJSON, &in.Attrs); err != nil {
			return nil, fmt.Errorf("decode enriched_attrs: %w", err)
		}
	}
	return ptrext.Of(in), nil
}

// UpdateReplyDraft overwrites the draft column (tenant-scoped) and returns the
// DB-stamped generation time. Returns ErrNotFound when the row is not owned by
// the tenant, so a missing tenant scope can never write another tenant's row.
func (r *DraftTaskRepo) UpdateReplyDraft(ctx context.Context, feedbackID int64, tenantID, draft string) (time.Time, error) {
	var generatedAt time.Time
	err := r.pool.QueryRow(ctx, `
		UPDATE user_feedback
		SET reply_draft = $3, reply_draft_generated_at = NOW()
		WHERE id = $1 AND tenant_id = $2
		RETURNING reply_draft_generated_at`,
		feedbackID, tenantID, draft,
	).Scan(&generatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, ErrNotFound
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("update reply draft: %w", err)
	}
	return generatedAt, nil
}

// DraftPrecheck returns the enrichment status and the tenant's reply-draft
// opt-in flag for a tenant-scoped feedback row — the inputs to the Regenerate
// endpoint's guards. Returns ErrNotFound when the row is not owned by the tenant.
func (r *DraftTaskRepo) DraftPrecheck(ctx context.Context, feedbackID int64, tenantID string) (enrichmentStatus string, replyDraftEnabled bool, err error) {
	err = r.pool.QueryRow(ctx, `
		SELECT f.enrichment_status, t.reply_draft_enabled
		FROM user_feedback f
		JOIN tenants t ON t.id = f.tenant_id
		WHERE f.id = $1 AND f.tenant_id = $2`,
		feedbackID, tenantID,
	).Scan(&enrichmentStatus, &replyDraftEnabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ErrNotFound
	}
	if err != nil {
		return "", false, fmt.Errorf("draft precheck: %w", err)
	}
	return enrichmentStatus, replyDraftEnabled, nil
}
