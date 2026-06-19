// Package repo is the data-access layer. It owns every SQL
// statement that touches user_feedback and exposes only typed methods
// to service/. Handlers and notifiers MUST NOT import this package —
// they go through service.
package feedback

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

// ErrIdempotencyConflict signals that the idempotency key already exists for a
// row whose request fingerprint differs (same key, different body). The service
// maps it to a 409.
var ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")

// FeedbackRepo wraps the user_feedback table. Concurrency-safe: pgxpool
// internally pools connections, so a single shared FeedbackRepo serves
// every caller.
type FeedbackRepo struct {
	pool *pgxpool.Pool
}

const (
	maxEnrichmentAttempts      = 5
	initialEnrichmentBackoff   = time.Minute
	maxEnrichmentBackoff       = 30 * time.Minute
	staleEnrichmentClaimWindow = 5 * time.Minute
)

func NewFeedback(pool *pgxpool.Pool) *FeedbackRepo {
	return ptrext.Of(FeedbackRepo{pool: pool})
}

// Insert creates one row and returns its new id. userID is the composed
// upstream identifier ("ext_<api-key-uuid>:<source-user>"). subjectKey /
// subjectDisplay / subjectHash hold the GDPR-facing canonical subject identity.
func (r *FeedbackRepo) Insert(
	ctx context.Context,
	tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
	in domain.IngestInput,
) (int64, error) {
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
	inboundSourceID := inboundSourceIDFromMeta(in.SourceMeta)
	var id int64
	err := r.pool.QueryRow(
		ctx, `
		INSERT INTO user_feedback
		 (user_id, tenant_id, subject_key, subject_display, subject_hash, type, content, page_url, attachments, source, source_meta, inbound_source_id, workflow_state_id)
		VALUES
		 ($1, $2, $3, $4, $5, 'other', $6, $7, '[]'::jsonb, $8, $9,
		  (SELECT id FROM inbound_sources WHERE id = $10 AND tenant_id = $2 AND channel = $8),
		  (SELECT id FROM tenant_workflow_states WHERE tenant_id = $2 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))
		RETURNING id`,
		userID, tenantID, subjectKey, subjectDisplay, subjectHash,
		in.Content, in.PageURL, in.Source, sourceMetaJSON, inboundSourceID,
	).Scan(&id)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,source:%s,err:%+v",
			where, tenantID, in.Source, err.Error())
		return 0, fmt.Errorf("insert feedback: %w", err)
	}
	return id, nil
}

// InsertIdempotent inserts the row keyed by in.IdempotencyKey, deduping under
// the partial unique index ux_user_feedback_idempotency (migration 056). A
// concurrent retry's INSERT blocks on the index until the first commits, then
// hits the conflict and reads back the original id — so concurrent retries can
// never create a duplicate row. Returns (id, deduped, err): deduped=true means
// an existing row with a matching request fingerprint was returned (no new
// row, no enrichment). A matching key with a different fingerprint returns
// ErrIdempotencyConflict.
func (r *FeedbackRepo) InsertIdempotent(
	ctx context.Context,
	tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
	in domain.IngestInput,
	idemHash []byte,
) (int64, bool, error) {
	const where = "repo.FeedbackRepo.InsertIdempotent"
	sourceMetaJSON := []byte("{}")
	if in.SourceMeta != nil {
		b, err := json.Marshal(in.SourceMeta)
		if err != nil {
			logext.Errorf(ctx, "[%s] marshal source_meta failed,tenant_id:%s,err:%+v",
				where, tenantID, err.Error())
			return 0, false, fmt.Errorf("marshal source_meta: %w", err)
		}
		sourceMetaJSON = b
	}
	inboundSourceID := inboundSourceIDFromMeta(in.SourceMeta)

	// Bounded retry: the read-back after a conflict can race with a concurrent
	// delete of the conflicting row (e.g. GDPR erasure), which frees the key —
	// re-attempt the insert rather than misreporting a vanished row.
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var id int64
		err := r.pool.QueryRow(
			ctx, `
			INSERT INTO user_feedback
			 (user_id, tenant_id, subject_key, subject_display, subject_hash, type, content, page_url, attachments, source, source_meta, inbound_source_id, workflow_state_id, idempotency_key, idempotency_hash)
			VALUES
			 ($1, $2, $3, $4, $5, 'other', $6, $7, '[]'::jsonb, $8, $9,
			  (SELECT id FROM inbound_sources WHERE id = $10 AND tenant_id = $2 AND channel = $8),
			  (SELECT id FROM tenant_workflow_states WHERE tenant_id = $2 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1),
			  $11, $12)
			ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL
			DO NOTHING
			RETURNING id`,
			userID, tenantID, subjectKey, subjectDisplay, subjectHash,
			in.Content, in.PageURL, in.Source, sourceMetaJSON, inboundSourceID,
			in.IdempotencyKey, idemHash,
		).Scan(&id)
		if err == nil {
			return id, false, nil // fresh insert
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,source:%s,err:%+v",
				where, tenantID, in.Source, err.Error())
			return 0, false, fmt.Errorf("insert feedback (idempotent): %w", err)
		}

		// Conflict: a row with this key already exists. Read it back and compare
		// the request fingerprint to tell a true replay from a key reuse.
		var existingHash []byte
		err = r.pool.QueryRow(
			ctx, `
			SELECT id, idempotency_hash FROM user_feedback
			WHERE tenant_id = $1 AND idempotency_key = $2`,
			tenantID, in.IdempotencyKey,
		).Scan(&id, &existingHash)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // conflicting row vanished between insert and read — retry
		}
		if err != nil {
			logext.Errorf(ctx, "[%s] read existing failed,tenant_id:%s,err:%+v",
				where, tenantID, err.Error())
			return 0, false, fmt.Errorf("read idempotent feedback: %w", err)
		}
		if subtle.ConstantTimeCompare(existingHash, idemHash) != 1 {
			return 0, false, ErrIdempotencyConflict
		}
		return id, true, nil // replay → existing row, no new insert
	}
	return 0, false, fmt.Errorf("insert feedback (idempotent): key contention exceeded %d attempts,tenant_id:%s", maxAttempts, tenantID)
}

// PurgeExpiredIdempotencyKeys clears idempotency_key/idempotency_hash on rows
// older than retention, releasing them from the partial unique index so it stays
// bounded to the recent dedup window (a key is only useful for the brief retry
// window of one ingest). The feedback rows themselves are kept — only the dedup
// tokens are dropped. Returns the number of rows cleared.
func (r *FeedbackRepo) PurgeExpiredIdempotencyKeys(ctx context.Context, retention time.Duration) (int64, error) {
	const where = "repo.FeedbackRepo.PurgeExpiredIdempotencyKeys"
	tag, err := r.pool.Exec(ctx, `
		UPDATE user_feedback
		SET idempotency_key = NULL, idempotency_hash = NULL
		WHERE idempotency_key IS NOT NULL
		  AND created_at < NOW() - make_interval(secs => $1)`,
		retention.Seconds(),
	)
	if err != nil {
		logext.Errorf(ctx, "[%s] purge failed,err:%+v", where, err.Error())
		return 0, fmt.Errorf("purge idempotency keys: %w", err)
	}
	return tag.RowsAffected(), nil
}

// TryClaim atomically transitions a row into 'enriching' if it's eligible:
// pending, retryable failed after backoff, or stuck in 'enriching' past the
// stale claim window.
// Returns true iff this caller now owns the row.
func (r *FeedbackRepo) TryClaim(ctx context.Context, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE user_feedback
		SET enrichment_status = 'enriching',
		 enrichment_claimed_at = NOW(),
		 enrichment_error = NULL,
		 enrichment_next_retry_at = NULL
		WHERE id = $1
		 AND (
		  enrichment_status = 'pending'
		 OR (enrichment_status = 'failed'
		     AND enrichment_attempts < $2
		     AND (enrichment_next_retry_at IS NULL OR enrichment_next_retry_at <= NOW()))
		 OR (enrichment_status = 'enriching'
		     AND enrichment_claimed_at < NOW() - make_interval(secs => $3))
		 )`,
		id, maxEnrichmentAttempts, int(staleEnrichmentClaimWindow.Seconds()))
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
	Content         string
	Source          string
	UserID          string
	Language        string
	DisplayLocale   string
	TenantID        string
	CreatedAt       time.Time
	InboundSourceID string
	SourceTags      []string
	PromptTemplate  *string
	Dimensions      domain.DimensionSet
}

// LoadForEnrich returns the columns the LLM prompt and the downstream
// Snapshot need. Assumes the caller just claimed the row.
//
// LEFT JOIN tenants pulls the per-tenant enricher override in the same
// query — saves a round-trip and means the enricher never needs to
// import the tenant repo.
func (r *FeedbackRepo) LoadForEnrich(ctx context.Context, id int64) (*EnrichInput, error) {
	var (
		in            EnrichInput
		dimsRaw       []byte
		inboundSource *uuid.UUID
	)
	err := r.pool.QueryRow(
		ctx,
		`SELECT uf.content, uf.source, uf.user_id, COALESCE(uf.language, ''),
		 COALESCE(t.locale, 'en'), uf.tenant_id, uf.created_at,
			 uf.inbound_source_id, COALESCE(src.tags, '{}'::text[]),
		 t.enrich_prompt_template, t.enrich_dimensions
		 FROM user_feedback uf
		 LEFT JOIN tenants t ON t.id = uf.tenant_id
		 LEFT JOIN inbound_sources src ON src.id = uf.inbound_source_id
		 WHERE uf.id = $1`, id,
	).Scan(&in.Content, &in.Source, &in.UserID, &in.Language, &in.DisplayLocale, &in.TenantID, &in.CreatedAt,
		&inboundSource, &in.SourceTags, &in.PromptTemplate, &dimsRaw)
	if err != nil {
		return nil, fmt.Errorf("load feedback %d: %w", id, err)
	}
	if inboundSource != nil {
		in.InboundSourceID = inboundSource.String()
	}
	if len(dimsRaw) > 0 {
		if err := json.Unmarshal(dimsRaw, &in.Dimensions); err != nil {
			return nil, fmt.Errorf("decode enrich dimensions for %d: %w", id, err)
		}
	}
	return ptrext.Of(in), nil
}

func inboundSourceIDFromMeta(meta map[string]any) *uuid.UUID {
	if meta == nil {
		return nil
	}
	raw, ok := meta[domain.SourceMetaInboundSourceID].(string)
	if !ok || raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return ptrext.Of(id)
}

type EnrichmentMetadata struct {
	Language      string
	DisplayLocale string
}

// markDoneSQL is the body of MarkDone[/Tx]. Extracted so both flavors
// stay in lockstep — the only difference is who executes it.
const markDoneSQL = `
		UPDATE user_feedback
		SET enriched_title = $1,
		 enriched_attrs = $2::jsonb,
		 is_urgent = $3,
		 enriched_rationale = $4,
		 language = NULLIF($5, ''),
		 enriched_display_title = NULLIF($6, ''),
		 enriched_display_rationale = NULLIF($7, ''),
		 enriched_display_locale = NULLIF($8, ''),
		 classification_confidence = $9,
		 enrichment_status = 'done',
		 enrichment_error = NULL,
		 enrichment_attempts = 0,
		 enrichment_next_retry_at = NULL,
		 enrichment_claimed_at = NULL,
		 enriched_at = NOW()
		WHERE id = $10`

// MarkDone persists the LLM classification and flips the row to 'done'.
// Single-statement; no outer tx needed. Use MarkDoneTx when this
// UPDATE must be atomic with other writes (e.g. outbox insertion).
func (r *FeedbackRepo) MarkDone(ctx context.Context, id int64, e domain.Enriched, metas ...EnrichmentMetadata) error {
	attrsJSON, err := marshalAttrs(e.Attrs)
	if err != nil {
		return err
	}
	meta := mergeEnrichmentMetadata(metas)
	if _, err := r.pool.Exec(
		ctx, markDoneSQL,
		e.Title, attrsJSON, e.IsUrgent, e.Rationale, meta.Language,
		e.DisplayTitle, e.DisplayRationale, meta.DisplayLocale,
		e.ClassificationConfidence, id,
	); err != nil {
		return fmt.Errorf("update enrichment row %d: %w", id, err)
	}
	return nil
}

// MarkDoneTx is MarkDone inside a caller-supplied transaction. The
// enricher uses this to flip user_feedback + INSERT outbox in one
// atomic step (model is "feedback is enriched IFF outbox is queued",
// anything else is undefined state).
func (r *FeedbackRepo) MarkDoneTx(ctx context.Context, tx pgx.Tx, id int64, e domain.Enriched, metas ...EnrichmentMetadata) error {
	attrsJSON, err := marshalAttrs(e.Attrs)
	if err != nil {
		return err
	}
	meta := mergeEnrichmentMetadata(metas)
	if _, err := tx.Exec(
		ctx, markDoneSQL,
		e.Title, attrsJSON, e.IsUrgent, e.Rationale, meta.Language,
		e.DisplayTitle, e.DisplayRationale, meta.DisplayLocale,
		e.ClassificationConfidence, id,
	); err != nil {
		return fmt.Errorf("update enrichment row %d (tx): %w", id, err)
	}
	return nil
}

func mergeEnrichmentMetadata(metas []EnrichmentMetadata) EnrichmentMetadata {
	var out EnrichmentMetadata
	for _, meta := range metas {
		if meta.Language != "" {
			out.Language = meta.Language
		}
		if meta.DisplayLocale != "" {
			out.DisplayLocale = meta.DisplayLocale
		}
	}
	return out
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

// MarkFailed records an enrichment error and schedules exponential retry.
// Attempts stop after maxEnrichmentAttempts; the row remains visible as
// status=failed with no next retry so operators can inspect/remediate.
// Best-effort: errors here only surface in logs.
//
// It returns whether this attempt was terminal (retries now exhausted) and the
// row's tenant, so the caller can record attune_enrichment_terminal_failures_total
// (#64). On a DB error it returns (false, "").
func (r *FeedbackRepo) MarkFailed(ctx context.Context, id int64, errMsg string) (terminal bool, tenant string) {
	const where = "repo.FeedbackRepo.MarkFailed"
	if err := r.pool.QueryRow(
		ctx,
		`UPDATE user_feedback
		    SET enrichment_status = 'failed',
		        enrichment_error = $1,
		        enrichment_attempts = enrichment_attempts + 1,
		        enrichment_next_retry_at = CASE
		          WHEN enrichment_attempts + 1 >= $3 THEN NULL
		          ELSE NOW() + make_interval(secs => LEAST(
		            $4 * CAST(POWER(2, LEAST(enrichment_attempts, 10)) AS INTEGER),
		            $5
		          ))
		        END,
		        enrichment_claimed_at = NULL
		  WHERE id = $2
		  RETURNING enrichment_attempts >= $3, tenant_id`,
		pgxutil.Truncate(errMsg, 1000), id, maxEnrichmentAttempts,
		int(initialEnrichmentBackoff.Seconds()), int(maxEnrichmentBackoff.Seconds()),
	).Scan(&terminal, &tenant); err != nil {
		// Row gone (concurrent erase / terminal transition) is benign — don't
		// log it as an error; only real DB failures are error-worthy.
		if !errors.Is(err, pgx.ErrNoRows) {
			logext.Errorf(ctx, "[%s] update failed,id:%d,err:%+v", where, id, err.Error())
		}
		return false, ""
	}
	return terminal, tenant
}

// ListPending returns up to n ids that need enrichment, ordered by
// arrival time. Includes stuck 'enriching' rows past the 5-min stale
// threshold so a crashed worker doesn't strand them.
func (r *FeedbackRepo) ListPending(ctx context.Context, n int) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id FROM user_feedback
		WHERE enrichment_status = 'pending'
		 OR (enrichment_status = 'failed'
		     AND enrichment_attempts < $2
		     AND (enrichment_next_retry_at IS NULL OR enrichment_next_retry_at <= NOW()))
		 OR (enrichment_status = 'enriching'
		     AND enrichment_claimed_at < NOW() - make_interval(secs => $3))
		ORDER BY created_at ASC
		LIMIT $1`, n, maxEnrichmentAttempts, int(staleEnrichmentClaimWindow.Seconds()))
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
