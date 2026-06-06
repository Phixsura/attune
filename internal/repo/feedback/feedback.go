// Package repo is the data-access layer (律 8). It owns every SQL
// statement that touches user_feedback and exposes only typed methods
// to service/. Handlers and notifiers MUST NOT import this package —
// they go through service.
package feedback

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/logext"
)

// FeedbackRepo wraps the user_feedback table. Concurrency-safe: pgxpool
// internally pools connections, so a single shared FeedbackRepo serves
// every caller.
type FeedbackRepo struct {
	pool *pgxpool.Pool
}

func NewFeedback(pool *pgxpool.Pool) *FeedbackRepo {
	return &FeedbackRepo{pool: pool}
}

// Insert creates one row and returns its new id. userID is the composed
// upstream identifier ("ext_<api-key-uuid>:<source-user>"); the caller
// (service.Ingestor) is responsible for building it.
func (r *FeedbackRepo) Insert(ctx context.Context, tenantID, userID string, in domain.IngestInput) (int64, error) {
	const where = "repo.FeedbackRepo.Insert"
	sourceMetaJSON := []byte("{}")
	if in.SourceMeta != nil {
		b, err := json.Marshal(in.SourceMeta)
		if err != nil {
			logext.Errorf(ctx, "[%s] marshal source_meta failed,tenant_id:%s,err:%+v",
				where, tenantID, err.Error())
			return 0, fmt.Errorf("marshal source_meta: %w", err)
		}
		sourceMetaJSON = b
	}
	var id int64
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO user_feedback
		  (user_id, tenant_id, type, content, page_url, attachments, source, source_meta)
		VALUES
		  ($1, $2, 'other', $3, $4, '[]'::jsonb, $5, $6)
		RETURNING id`,
		userID, tenantID, in.Content, in.PageURL, in.Source, sourceMetaJSON,
	).Scan(&id)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,source:%s,err:%+v",
			where, tenantID, in.Source, err.Error())
		return 0, fmt.Errorf("insert feedback: %w", err)
	}
	return id, nil
}

// TryClaim atomically transitions a row into 'enriching' if it's eligible:
// either still pending/failed, or stuck in 'enriching' past 5 minutes.
// Returns true iff this caller now owns the row.
func (r *FeedbackRepo) TryClaim(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE user_feedback
		SET enrichment_status = 'enriching',
		    enrichment_claimed_at = NOW(),
		    enrichment_error = NULL
		WHERE id = $1
		  AND (enrichment_status IN ('pending','failed')
		       OR (enrichment_status = 'enriching'
		           AND enrichment_claimed_at < NOW() - INTERVAL '5 minutes'))`, id)
	if err != nil {
		return false, fmt.Errorf("claim feedback %d: %w", id, err)
	}
	return tag.RowsAffected() == 1, nil
}

// EnrichInput is the row data the enricher needs to call the LLM.
// Returned by LoadForEnrich after a successful TryClaim.
type EnrichInput struct {
	Content  string
	Source   string
	UserID   string
	TenantID string
}

// LoadForEnrich returns the columns the LLM prompt and the downstream
// Snapshot need. Assumes the caller just claimed the row.
func (r *FeedbackRepo) LoadForEnrich(ctx context.Context, id int64) (*EnrichInput, error) {
	var in EnrichInput
	err := r.pool.QueryRow(
		ctx,
		`SELECT content, source, user_id, tenant_id FROM user_feedback WHERE id=$1`, id,
	).Scan(&in.Content, &in.Source, &in.UserID, &in.TenantID)
	if err != nil {
		return nil, fmt.Errorf("load feedback %d: %w", id, err)
	}
	return &in, nil
}

// markDoneSQL is the body of MarkDone[/Tx]. Extracted so both flavors
// stay in lockstep — the only difference is who executes it.
const markDoneSQL = `
	UPDATE user_feedback
	SET enriched_title = $1,
	    enriched_kind = $2,
	    enriched_modules = $3,
	    enriched_severity = $4,
	    enriched_priority = $5,
	    enrichment_status = 'done',
	    enrichment_error = NULL,
	    enriched_at = NOW()
	WHERE id = $6`

// MarkDone persists the LLM classification and flips the row to 'done'.
// Caller computed Priority from Severity already. Single-statement; no
// outer tx needed. Use MarkDoneTx when this UPDATE must be atomic with
// other writes (e.g. outbox insertion).
func (r *FeedbackRepo) MarkDone(ctx context.Context, id int64, e domain.Enriched) error {
	modulesJSON, _ := json.Marshal(e.Modules)
	if _, err := r.pool.Exec(
		ctx, markDoneSQL,
		e.Title, e.Kind, modulesJSON, e.Severity, e.Priority, id,
	); err != nil {
		return fmt.Errorf("update enrichment row %d: %w", id, err)
	}
	return nil
}

// MarkDoneTx is MarkDone inside a caller-supplied transaction. The
// enricher uses this to flip user_feedback + INSERT outbox in one
// atomic step (律 7 — model is "feedback is enriched IFF outbox is
// queued", anything else is undefined state).
func (r *FeedbackRepo) MarkDoneTx(ctx context.Context, tx pgx.Tx, id int64, e domain.Enriched) error {
	modulesJSON, _ := json.Marshal(e.Modules)
	if _, err := tx.Exec(
		ctx, markDoneSQL,
		e.Title, e.Kind, modulesJSON, e.Severity, e.Priority, id,
	); err != nil {
		return fmt.Errorf("update enrichment row %d (tx): %w", id, err)
	}
	return nil
}

// BeginTx opens a transaction on the underlying pool. Exposed here
// (rather than pool directly) so service-layer code can stay decoupled
// from pgxpool.
func (r *FeedbackRepo) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

// MarkFailed records an enrichment error without changing the row's
// payload. Best-effort: errors here only surface in logs.
func (r *FeedbackRepo) MarkFailed(ctx context.Context, id int64, errMsg string) {
	const where = "repo.FeedbackRepo.MarkFailed"
	if _, err := r.pool.Exec(ctx,
		`UPDATE user_feedback SET enrichment_status='failed', enrichment_error=$1 WHERE id=$2`,
		truncate(errMsg, 1000), id); err != nil {
		logext.Errorf(ctx, "[%s] update failed,id:%d,err:%+v", where, id, err.Error())
	}
}

// ListPending returns up to n ids that need enrichment, ordered by
// arrival time. Includes stuck 'enriching' rows past the 5-min stale
// threshold so a crashed worker doesn't strand them.
func (r *FeedbackRepo) ListPending(ctx context.Context, n int) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id FROM user_feedback
		WHERE enrichment_status IN ('pending','failed')
		   OR (enrichment_status = 'enriching'
		       AND enrichment_claimed_at < NOW() - INTERVAL '5 minutes')
		ORDER BY created_at ASC
		LIMIT $1`, n)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan pending: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SampleRow is one historical enriched row, returned by SampleEnriched
// for the eval CLI. modules is the JSONB array materialized into Go.
type SampleRow struct {
	ID       int64
	TenantID string
	Content  string
	Kind     string
	Severity string
	Modules  []string
	Priority float64
}

// SampleEnriched returns up to n randomly-sampled rows that completed
// enrichment after `since`. Used by `attune eval` to feed re-run /
// human-label workflows. Order is random per call (ORDER BY RANDOM())
// — fine for a 50-row sample; Wave 3 may switch to TABLESAMPLE if
// the table grows past a few million rows.
func (r *FeedbackRepo) SampleEnriched(ctx context.Context, since time.Time, n int) ([]SampleRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, content,
		       COALESCE(enriched_kind,''),
		       COALESCE(enriched_severity,''),
		       COALESCE(enriched_modules, '[]'::jsonb),
		       COALESCE(enriched_priority, 0)
		  FROM user_feedback
		 WHERE enrichment_status = 'done'
		   AND enriched_at >= $1
		 ORDER BY RANDOM()
		 LIMIT $2`, since, n)
	if err != nil {
		return nil, fmt.Errorf("sample enriched: %w", err)
	}
	defer rows.Close()
	var out []SampleRow
	for rows.Next() {
		var s SampleRow
		var modulesRaw []byte
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.Content,
			&s.Kind, &s.Severity, &modulesRaw, &s.Priority,
		); err != nil {
			return nil, fmt.Errorf("scan sample row: %w", err)
		}
		if len(modulesRaw) > 0 {
			_ = json.Unmarshal(modulesRaw, &s.Modules)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
