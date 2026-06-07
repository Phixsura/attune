// Package repo is the data-access layer. It owns every SQL
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
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

// FeedbackRepo wraps the user_feedback table. Concurrency-safe: pgxpool
// internally pools connections, so a single shared FeedbackRepo serves
// every caller.
type FeedbackRepo struct {
	pool *pgxpool.Pool
}

func NewFeedback(pool *pgxpool.Pool) *FeedbackRepo {
	return ptrext.Of(FeedbackRepo{pool: pool})
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
//
// PromptTemplate and Dimensions come from the per-tenant override on
// the tenants row (#10 → E3 proposal). PromptTemplate may be nil
// (fall back to the built-in default). Dimensions may be empty (the
// LLM still emits title + rationale but no per-dim values).
//
// CreatedAt is the user's actual submission time, surfaced through
// Snapshot.SubmittedAt into outbound envelopes (#82) so consumers see
// the real timeline instead of an enrichment-delayed timestamp.
type EnrichInput struct {
	Content        string
	Source         string
	UserID         string
	TenantID       string
	CreatedAt      time.Time
	PromptTemplate *string
	Dimensions     domain.DimensionSet
}

// LoadForEnrich returns the columns the LLM prompt and the downstream
// Snapshot need. Assumes the caller just claimed the row.
//
// LEFT JOIN tenants pulls the per-tenant enricher override in the same
// query — saves a round-trip and means the enricher never needs to
// import the tenant repo.
func (r *FeedbackRepo) LoadForEnrich(ctx context.Context, id int64) (*EnrichInput, error) {
	var (
		in      EnrichInput
		dimsRaw []byte
	)
	err := r.pool.QueryRow(
		ctx,
		`SELECT uf.content, uf.source, uf.user_id, uf.tenant_id, uf.created_at,
		 t.enrich_prompt_template, t.enrich_dimensions
		 FROM user_feedback uf
		 LEFT JOIN tenants t ON t.id = uf.tenant_id
		 WHERE uf.id = $1`, id,
	).Scan(&in.Content, &in.Source, &in.UserID, &in.TenantID, &in.CreatedAt,
		&in.PromptTemplate, &dimsRaw)
	if err != nil {
		return nil, fmt.Errorf("load feedback %d: %w", id, err)
	}
	if len(dimsRaw) > 0 {
		if err := json.Unmarshal(dimsRaw, &in.Dimensions); err != nil {
			return nil, fmt.Errorf("decode enrich dimensions for %d: %w", id, err)
		}
	}
	return ptrext.Of(in), nil
}

// markDoneSQL is the body of MarkDone[/Tx]. Extracted so both flavors
// stay in lockstep — the only difference is who executes it.
const markDoneSQL = `
	UPDATE user_feedback
	SET enriched_title = $1,
	 enriched_attrs = $2::jsonb,
	 is_urgent = $3,
	 enriched_rationale = $4,
	 enrichment_status = 'done',
	 enrichment_error = NULL,
	 enriched_at = NOW()
	WHERE id = $5`

// MarkDone persists the LLM classification and flips the row to 'done'.
// Single-statement; no outer tx needed. Use MarkDoneTx when this
// UPDATE must be atomic with other writes (e.g. outbox insertion).
func (r *FeedbackRepo) MarkDone(ctx context.Context, id int64, e domain.Enriched) error {
	attrsJSON, err := marshalAttrs(e.Attrs)
	if err != nil {
		return err
	}
	if _, err := r.pool.Exec(
		ctx, markDoneSQL,
		e.Title, attrsJSON, e.IsUrgent, e.Rationale, id,
	); err != nil {
		return fmt.Errorf("update enrichment row %d: %w", id, err)
	}
	return nil
}

// MarkDoneTx is MarkDone inside a caller-supplied transaction. The
// enricher uses this to flip user_feedback + INSERT outbox in one
// atomic step (model is "feedback is enriched IFF outbox is queued",
// anything else is undefined state).
func (r *FeedbackRepo) MarkDoneTx(ctx context.Context, tx pgx.Tx, id int64, e domain.Enriched) error {
	attrsJSON, err := marshalAttrs(e.Attrs)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(
		ctx, markDoneSQL,
		e.Title, attrsJSON, e.IsUrgent, e.Rationale, id,
	); err != nil {
		return fmt.Errorf("update enrichment row %d (tx): %w", id, err)
	}
	return nil
}

// MaxAttrsBytes is the upper bound on a single row's enriched_attrs
// JSONB payload before MarkDone refuses the write. The default 32 KiB
// fits any sensible per-tenant dim set (the OSS seed comes in under
// 1 KiB) and shields the table from rogue clients that try to stuff
// thousands of labels into one row. Operators with genuinely larger
// taxonomies can bump this constant; the schema's hard cap is 1 GiB
// (Postgres JSONB max).
const MaxAttrsBytes = 32 * 1024

// ErrAttrsTooLarge signals MarkDone refused the payload. Surfaced
// through the enricher as a parse_err-style failure so the row goes to
// status=failed instead of poisoning the table with truncated data.
var ErrAttrsTooLarge = fmt.Errorf("enriched_attrs exceeds %d bytes", MaxAttrsBytes)

// marshalAttrs canonicalizes a nil Attrs map to an empty JSONB object
// so the DB column is never NULL nor "null" — keeps GIN containment
// queries and scan paths uniform — and enforces the per-row size cap.
func marshalAttrs(a map[string]any) ([]byte, error) {
	if a == nil {
		return []byte("{}"), nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("marshal enriched_attrs: %w", err)
	}
	if len(b) > MaxAttrsBytes {
		return nil, fmt.Errorf("%w (got %d)", ErrAttrsTooLarge, len(b))
	}
	return b, nil
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
		pgxutil.Truncate(errMsg, 1000), id); err != nil {
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
// for the `attune eval` subcommand.
type SampleRow struct {
	ID       int64
	TenantID string
	Content  string
	Attrs    map[string]any // decoded from enriched_attrs JSONB
	IsUrgent bool
}

// SampleEnriched returns up to n randomly-sampled rows that completed
// enrichment after `since`. Used by `attune eval` to feed re-run /
// human-label workflows. Order is random per call (ORDER BY RANDOM())
// — fine for a 50-row sample.
func (r *FeedbackRepo) SampleEnriched(ctx context.Context, since time.Time, n int) ([]SampleRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, content,
		 COALESCE(enriched_attrs, '{}'::jsonb),
		 is_urgent
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
		var (
			s        SampleRow
			attrsRaw []byte
		)
		if err := rows.Scan(
			&s.ID, &s.TenantID, &s.Content,
			&attrsRaw, &s.IsUrgent,
		); err != nil {
			return nil, fmt.Errorf("scan sample row: %w", err)
		}
		if len(attrsRaw) > 0 {
			if err := json.Unmarshal(attrsRaw, &s.Attrs); err != nil {
				return nil, fmt.Errorf("decode sample attrs: %w", err)
			}
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// truncate moved to internal/repo/pgxutil.Truncate (single canonical
// helper imported by every repo subpackage).
