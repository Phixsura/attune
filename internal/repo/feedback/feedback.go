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
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	pool dbPool
}

// dbPool is the slice of *pgxpool.Pool the repo actually uses — an
// interface so unit tests can inject fakes for paths whose behavior is
// otherwise only reachable on a live database (PR #122 pattern).
type dbPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
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
	return insertFeedback(ctx, r.pool, tenantID, userID, subjectKey, subjectDisplay, subjectHash, in)
}

// InsertTx is the transaction-scoped variant of Insert. Use it when the caller
// needs the feedback row and follow-up writes to commit atomically.
func (r *FeedbackRepo) InsertTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
	in domain.IngestInput,
) (int64, error) {
	return insertFeedback(ctx, tx, tenantID, userID, subjectKey, subjectDisplay, subjectHash, in)
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
	return insertFeedbackIdempotent(ctx, r.pool, tenantID, userID, subjectKey, subjectDisplay, subjectHash, in, idemHash)
}

// InsertIdempotentTx is the transaction-scoped variant of InsertIdempotent.
func (r *FeedbackRepo) InsertIdempotentTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
	in domain.IngestInput,
	idemHash []byte,
) (int64, bool, error) {
	return insertFeedbackIdempotent(ctx, tx, tenantID, userID, subjectKey, subjectDisplay, subjectHash, in, idemHash)
}

// PurgeExpiredIdempotencyKeys clears idempotency_key/idempotency_hash on rows
// older than retention, releasing them from the partial unique index so it stays
// bounded to the recent dedup window (a key is only useful for the brief retry
// window of one ingest). The feedback rows themselves are kept — only the dedup
// tokens are dropped. Returns the number of rows cleared.
func (r *FeedbackRepo) PurgeExpiredIdempotencyKeys(ctx context.Context, retention time.Duration) (int64, error) {
	const where = "repo.FeedbackRepo.PurgeExpiredIdempotencyKeys"
	tag, err := r.pool.Exec(
		ctx, `
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

func insertFeedback(
	ctx context.Context,
	db queryer,
	tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
	in domain.IngestInput,
) (int64, error) {
	const where = "repo.FeedbackRepo.Insert"
	sourceMetaJSON, err := marshalSourceMeta(ctx, where, tenantID, in.SourceMeta)
	if err != nil {
		return 0, err
	}
	inboundSourceID := inboundSourceIDFromMeta(in.SourceMeta)
	var id int64
	err = db.QueryRow(
		ctx, `
		INSERT INTO user_feedback
		 (user_id, tenant_id, subject_key, subject_display, subject_hash, type, content, page_url, attachments, source, source_meta, signal_trace_id, inbound_source_id, workflow_state_id)
		VALUES
		 ($1, $2, $3, $4, $5, $6, $7, $8, '[]'::jsonb, $9, $10, $11,
		  (SELECT id FROM inbound_sources WHERE id = $12 AND tenant_id = $2 AND channel = $9),
		  (SELECT id FROM tenant_workflow_states WHERE tenant_id = $2 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1))
		RETURNING id`,
		userID, tenantID, subjectKey, subjectDisplay, subjectHash, feedbackType(in),
		in.Content, in.PageURL, in.Source, sourceMetaJSON, requiredSignalTraceID(in.SignalTraceID), inboundSourceID,
	).Scan(&id)
	if err != nil {
		logext.Errorf(ctx, "[%s] insert failed,tenant_id:%s,source:%s,err:%+v",
			where, tenantID, in.Source, err.Error())
		return 0, fmt.Errorf("insert feedback: %w", err)
	}
	return id, nil
}

func insertFeedbackIdempotent(
	ctx context.Context,
	db queryer,
	tenantID, userID, subjectKey, subjectDisplay, subjectHash string,
	in domain.IngestInput,
	idemHash []byte,
) (int64, bool, error) {
	const where = "repo.FeedbackRepo.InsertIdempotent"
	sourceMetaJSON, err := marshalSourceMeta(ctx, where, tenantID, in.SourceMeta)
	if err != nil {
		return 0, false, err
	}
	inboundSourceID := inboundSourceIDFromMeta(in.SourceMeta)

	// Bounded retry: the read-back after a conflict can race with a concurrent
	// delete of the conflicting row (e.g. GDPR erasure), which frees the key —
	// re-attempt the insert rather than misreporting a vanished row.
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		var id int64
		err := db.QueryRow(
			ctx, `
			INSERT INTO user_feedback
			 (user_id, tenant_id, subject_key, subject_display, subject_hash, type, content, page_url, attachments, source, source_meta, signal_trace_id, inbound_source_id, workflow_state_id, idempotency_key, idempotency_hash)
			VALUES
			 ($1, $2, $3, $4, $5, $6, $7, $8, '[]'::jsonb, $9, $10, $11,
			  (SELECT id FROM inbound_sources WHERE id = $12 AND tenant_id = $2 AND channel = $9),
			  (SELECT id FROM tenant_workflow_states WHERE tenant_id = $2 AND is_default AND archived_at IS NULL ORDER BY position LIMIT 1),
			  $13, $14)
			ON CONFLICT (tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL
			DO NOTHING
			RETURNING id`,
			userID, tenantID, subjectKey, subjectDisplay, subjectHash, feedbackType(in),
			in.Content, in.PageURL, in.Source, sourceMetaJSON, requiredSignalTraceID(in.SignalTraceID), inboundSourceID,
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
		err = db.QueryRow(
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

func marshalSourceMeta(
	ctx context.Context,
	where string,
	tenantID string,
	sourceMeta map[string]any,
) ([]byte, error) {
	sourceMetaJSON := []byte("{}")
	if sourceMeta == nil {
		return sourceMetaJSON, nil
	}
	b, err := json.Marshal(sourceMeta)
	if err != nil {
		logext.Errorf(ctx, "[%s] marshal source_meta failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return nil, fmt.Errorf("marshal source_meta: %w", err)
	}
	return b, nil
}

func feedbackType(in domain.IngestInput) string {
	kind := strings.ToLower(strings.TrimSpace(in.Type))
	if kind == "" {
		return "other"
	}
	return kind
}

// TryClaim atomically transitions a row into 'enriching' if it's eligible:
// pending, retryable failed after backoff, or stuck in 'enriching' past the
// stale claim window.
// Returns true iff this caller now owns the row.
func (r *FeedbackRepo) TryClaim(ctx context.Context, id int64) (bool, error) {
	return r.TryClaimWithOwner(ctx, id, "")
}

// TryClaimWithOwner is like TryClaim but also sets enrichment_claimed_by.
// This enables fencing so MarkDone/MarkFailed only succeed if this worker
// still holds the claim.
func (r *FeedbackRepo) TryClaimWithOwner(ctx context.Context, id int64, owner string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE user_feedback
		SET enrichment_status = 'enriching',
		 enrichment_claimed_at = NOW(),
		 enrichment_claimed_by = $4,
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
		id, maxEnrichmentAttempts, int(staleEnrichmentClaimWindow.Seconds()), owner)
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
	Type            string
	UserID          string
	Language        string
	DisplayLocale   string
	TenantID        string
	CreatedAt       time.Time
	InboundSourceID string
	SourceTags      []string
	PromptTemplate  *string
	Dimensions      domain.DimensionSet
	PromptPolicy    domain.EnrichPromptPolicyConfig
	PromptVersionID string
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
		policyRaw     []byte
		inboundSource *uuid.UUID
	)
	err := r.pool.QueryRow(
		ctx,
		`SELECT uf.content, uf.source, COALESCE(uf.type, ''), uf.user_id, COALESCE(uf.language, ''),
		 COALESCE(t.locale, 'en'), uf.tenant_id, uf.created_at,
			 uf.inbound_source_id, COALESCE(src.tags, '{}'::text[]),
			 t.enrich_prompt_template, t.enrich_dimensions, t.enrich_prompt_policy,
			 COALESCE(t.active_enrich_prompt_version_id::text, '')
		 FROM user_feedback uf
		 LEFT JOIN tenants t ON t.id = uf.tenant_id
		 LEFT JOIN inbound_sources src ON src.id = uf.inbound_source_id
		 WHERE uf.id = $1`, id,
	).Scan(&in.Content, &in.Source, &in.Type, &in.UserID, &in.Language, &in.DisplayLocale, &in.TenantID, &in.CreatedAt,
		&inboundSource, &in.SourceTags, &in.PromptTemplate, &dimsRaw, &policyRaw, &in.PromptVersionID)
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
	in.PromptPolicy = domain.DefaultEnrichPromptPolicyConfig()
	if len(policyRaw) > 0 {
		if err := json.Unmarshal(policyRaw, &in.PromptPolicy); err != nil {
			return nil, fmt.Errorf("decode enrich prompt policy for %d: %w", id, err)
		}
		in.PromptPolicy = domain.NormalizeEnrichPromptPolicyConfig(in.PromptPolicy)
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

var signalTraceMetaKeys = []string{
	"signal_trace_id",
	"signalTraceId",
	"trace_id", // lint-slog:allow rule-2
	"traceId",
	"event_id",
	"eventId",
	"source_event_id",
	"sourceEventId",
	"external_event_id",
	"externalEventId",
}

const maxSignalTraceIDLen = 160

// SignalTraceIDFromSourceMeta resolves the durable business trace attached to a
// feedback signal. Upstream source/event identifiers win so retries and
// cross-system records converge; callers provide a request/background fallback.
func SignalTraceIDFromSourceMeta(meta map[string]any, fallback string) string {
	for _, key := range signalTraceMetaKeys {
		if id := normalizeSignalTraceID(metaString(meta, key)); id != "" {
			return id
		}
	}
	return normalizeSignalTraceID(fallback)
}

func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	raw, ok := meta[key]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		return ""
	}
}

func normalizeSignalTraceID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" {
		return ""
	}
	id = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, id)
	return pgxutil.Truncate(id, maxSignalTraceIDLen)
}

func requiredSignalTraceID(raw string) string {
	if id := normalizeSignalTraceID(raw); id != "" {
		return id
	}
	return uuid.NewString()
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
		 enrichment_failure_reason_class = NULL,
		 enrichment_failure_model = NULL,
		 enrichment_failure_channel_id = NULL,
		 enrichment_failure_channel_name = NULL,
		 enrichment_failure_config_fingerprint = NULL,
		 enrichment_failure_prompt_version = NULL,
		 enrichment_claimed_at = NULL,
		 enrichment_claimed_by = NULL,
		 enriched_at = NOW()
		WHERE id = $10`

// markDoneWithOwnerSQL adds claimed_by fencing to markDoneSQL.
const markDoneWithOwnerSQL = `
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
		 enrichment_failure_reason_class = NULL,
		 enrichment_failure_model = NULL,
		 enrichment_failure_channel_id = NULL,
		 enrichment_failure_channel_name = NULL,
		 enrichment_failure_config_fingerprint = NULL,
		 enrichment_failure_prompt_version = NULL,
		 enrichment_claimed_at = NULL,
		 enrichment_claimed_by = NULL,
		 enriched_at = NOW()
		WHERE id = $10 AND enrichment_claimed_by = $11`

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

// MarkDoneTxWithOwner is like MarkDoneTx but includes claimed_by fencing.
// Returns rows affected (0 if the row was re-claimed by another worker).
func (r *FeedbackRepo) MarkDoneTxWithOwner(ctx context.Context, tx pgx.Tx, id int64, owner string, e domain.Enriched, metas ...EnrichmentMetadata) (int64, error) {
	attrsJSON, err := marshalAttrs(e.Attrs)
	if err != nil {
		return 0, err
	}
	meta := mergeEnrichmentMetadata(metas)
	tag, err := tx.Exec(
		ctx, markDoneWithOwnerSQL,
		e.Title, attrsJSON, e.IsUrgent, e.Rationale, meta.Language,
		e.DisplayTitle, e.DisplayRationale, meta.DisplayLocale,
		e.ClassificationConfidence, id, owner,
	)
	if err != nil {
		return 0, fmt.Errorf("update enrichment row %d (tx): %w", id, err)
	}
	return tag.RowsAffected(), nil
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
func (r *FeedbackRepo) MarkFailed(ctx context.Context, id int64, errMsg string, snapshots ...EnrichmentFailureSnapshot) (terminal bool, tenant string) {
	return r.MarkFailedWithOwner(ctx, id, "", errMsg, snapshots...)
}

// MarkFailedWithOwner is like MarkFailed but includes claimed_by fencing.
// If owner is non-empty, only updates if enrichment_claimed_by matches.
func (r *FeedbackRepo) MarkFailedWithOwner(ctx context.Context, id int64, owner, errMsg string, snapshots ...EnrichmentFailureSnapshot) (terminal bool, tenant string) {
	const where = "repo.FeedbackRepo.MarkFailedWithOwner"
	snapshot := EnrichmentFailureSnapshot{}
	if len(snapshots) > 0 {
		snapshot = snapshots[0]
	}
	sql, args := markFailedQuery(id, owner, errMsg, snapshot)
	if err := r.pool.QueryRow(ctx, sql, args...).Scan(&terminal, &tenant); err != nil {
		// Row gone (concurrent erase / terminal transition) is benign — don't
		// log it as an error; only real DB failures are error-worthy.
		if !errors.Is(err, pgx.ErrNoRows) {
			logext.Errorf(ctx, "[%s] update failed,id:%d,err:%+v", where, id, err.Error())
		}
		return false, ""
	}
	return terminal, tenant
}

func markFailedQuery(id int64, owner, errMsg string, snapshot EnrichmentFailureSnapshot) (string, []any) {
	if owner == "" {
		return markFailedWithoutOwnerQuery(id, errMsg, snapshot)
	}
	return markFailedWithOwnerQuery(id, owner, errMsg, snapshot)
}

func markFailedWithoutOwnerQuery(id int64, errMsg string, snapshot EnrichmentFailureSnapshot) (string, []any) {
	return `WITH updated AS (
	    UPDATE user_feedback
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
	        enrichment_failure_reason_class = NULLIF($6, ''),
	        enrichment_failure_model = NULLIF($7, ''),
	        enrichment_failure_channel_id = NULLIF($8, ''),
	        enrichment_failure_channel_name = NULLIF($9, ''),
	        enrichment_failure_config_fingerprint = NULLIF($10, ''),
	        enrichment_failure_prompt_version = NULLIF($11, ''),
	        enrichment_claimed_at = NULL,
	        enrichment_claimed_by = NULL
	    WHERE id = $2
	    RETURNING id, tenant_id, source, enrichment_attempts,
	              enrichment_attempts >= $3 AS terminal
	),
	inserted AS (
	    INSERT INTO classification_quality_failure_events
	     (tenant_id, feedback_id, event_kind, reason_class, logical_model,
	      provider_model, channel_id, channel_name, source, attempts, terminal)
	    SELECT tenant_id, id, 'attempt_failed', $6,
	           COALESCE(NULLIF($12, ''), NULLIF($7, ''), ''),
	           COALESCE(NULLIF($13, ''), NULLIF($7, ''), ''),
	           COALESCE(NULLIF($8, ''), ''),
	           COALESCE(NULLIF($9, ''), ''),
	           COALESCE(NULLIF(source, ''), ''),
	           enrichment_attempts,
	           terminal
	      FROM updated
	    RETURNING terminal, tenant_id
	)
	SELECT terminal, tenant_id FROM inserted`, []any{
			pgxutil.Truncate(errMsg, 1000), id, maxEnrichmentAttempts,
			int(initialEnrichmentBackoff.Seconds()), int(maxEnrichmentBackoff.Seconds()),
			normalizeRepoFailureReasonClass(snapshot.ReasonClass), snapshot.Model, snapshot.ChannelID,
			snapshot.ChannelName, snapshot.ConfigFingerprint, snapshot.PromptVersion,
			snapshot.LogicalModel, snapshot.ProviderModel,
		}
}

func markFailedWithOwnerQuery(id int64, owner, errMsg string, snapshot EnrichmentFailureSnapshot) (string, []any) {
	return `WITH updated AS (
	    UPDATE user_feedback
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
	        enrichment_failure_reason_class = NULLIF($7, ''),
	        enrichment_failure_model = NULLIF($8, ''),
	        enrichment_failure_channel_id = NULLIF($9, ''),
	        enrichment_failure_channel_name = NULLIF($10, ''),
	        enrichment_failure_config_fingerprint = NULLIF($11, ''),
	        enrichment_failure_prompt_version = NULLIF($12, ''),
	        enrichment_claimed_at = NULL,
	        enrichment_claimed_by = NULL
	    WHERE id = $2 AND enrichment_claimed_by = $6
	    RETURNING id, tenant_id, source, enrichment_attempts,
	              enrichment_attempts >= $3 AS terminal
	),
	inserted AS (
	    INSERT INTO classification_quality_failure_events
	     (tenant_id, feedback_id, event_kind, reason_class, logical_model,
	      provider_model, channel_id, channel_name, source, attempts, terminal)
	    SELECT tenant_id, id, 'attempt_failed', $7,
	           COALESCE(NULLIF($13, ''), NULLIF($8, ''), ''),
	           COALESCE(NULLIF($14, ''), NULLIF($8, ''), ''),
	           COALESCE(NULLIF($9, ''), ''),
	           COALESCE(NULLIF($10, ''), ''),
	           COALESCE(NULLIF(source, ''), ''),
	           enrichment_attempts,
	           terminal
	      FROM updated
	    RETURNING terminal, tenant_id
	)
	SELECT terminal, tenant_id FROM inserted`, []any{
			pgxutil.Truncate(errMsg, 1000), id, maxEnrichmentAttempts,
			int(initialEnrichmentBackoff.Seconds()), int(maxEnrichmentBackoff.Seconds()), owner,
			normalizeRepoFailureReasonClass(snapshot.ReasonClass), snapshot.Model, snapshot.ChannelID,
			snapshot.ChannelName, snapshot.ConfigFingerprint, snapshot.PromptVersion,
			snapshot.LogicalModel, snapshot.ProviderModel,
		}
}

func normalizeRepoFailureReasonClass(class string) string {
	switch strings.TrimSpace(class) {
	case "llm_err", "parse_err", "other_err":
		return strings.TrimSpace(class)
	default:
		return "other_err"
	}
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

// SampleEnrichedByTenant returns a random sample of rows with completed
// enrichment after `since` for a specific tenant. Used by Console API
// eval-suggestions endpoint to ensure tenant isolation.
func (r *FeedbackRepo) SampleEnrichedByTenant(ctx context.Context, tenantID string, since time.Time, n int) ([]SampleRow, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, content,
		 COALESCE(enriched_attrs, '{}'::jsonb),
		 is_urgent
		 FROM user_feedback
		 WHERE tenant_id = $1
		 AND enrichment_status = 'done'
		 AND enriched_at >= $2
		 ORDER BY RANDOM()
		 LIMIT $3`, tenantID, since, n)
	if err != nil {
		return nil, fmt.Errorf("sample enriched by tenant: %w", err)
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

// SetUrgent updates the is_urgent flag on a feedback row.
func (r *FeedbackRepo) SetUrgent(ctx context.Context, tenantID string, id int64, urgent bool) error {
	tag, err := r.pool.Exec(
		ctx,
		`UPDATE user_feedback SET is_urgent = $3, updated_at = NOW()
		 WHERE id = $1 AND tenant_id = $2`,
		id, tenantID, urgent,
	)
	if err != nil {
		return fmt.Errorf("set urgent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("feedback not found")
	}
	return nil
}

// truncate moved to internal/repo/pgxutil.Truncate (single canonical
// helper imported by every repo subpackage).

// RetryResult holds the result of a successful RetryEnrichment call.
type RetryResult struct {
	ID          int64
	Status      string
	Attempts    int
	NextRetryAt *time.Time
}

// ErrInvalidStateForRetry signals that the row is not in a failed state and
// cannot be retried.
var ErrInvalidStateForRetry = errors.New("invalid state for retry: row must be in failed status")

// RetryEnrichment resets a terminal-failed row so it can be re-enriched. It sets
// enrichment_attempts=0, enrichment_next_retry_at=NOW(), and clears enrichment_error.
// Returns ErrFeedbackNotFound if the row doesn't exist or belongs to a different tenant.
// Returns ErrInvalidStateForRetry if the row is not in failed status.
func (r *FeedbackRepo) RetryEnrichment(ctx context.Context, tenantID string, id int64) (*RetryResult, error) {
	const where = "repo.FeedbackRepo.RetryEnrichment"
	var result RetryResult
	var nextRetry time.Time
	err := r.pool.QueryRow(ctx, `
		UPDATE user_feedback
		SET enrichment_attempts = 0,
		    enrichment_next_retry_at = NOW(),
		    enrichment_error = NULL,
		    updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND enrichment_status = 'failed'
		RETURNING id, enrichment_status, enrichment_attempts, enrichment_next_retry_at
	`, id, tenantID).Scan(&result.ID, &result.Status, &result.Attempts, &nextRetry)

	if errors.Is(err, pgx.ErrNoRows) {
		// Could be: not found, wrong tenant, or not in failed status.
		// Check if the row exists at all to return the right error.
		var status string
		checkErr := r.pool.QueryRow(ctx,
			`SELECT enrichment_status FROM user_feedback WHERE id = $1 AND tenant_id = $2`,
			id, tenantID).Scan(&status)
		if errors.Is(checkErr, pgx.ErrNoRows) {
			return nil, ErrFeedbackNotFound
		}
		if checkErr != nil {
			logext.Errorf(ctx, "[%s] check row failed,tenant_id:%s,id:%d,err:%+v",
				where, tenantID, id, checkErr.Error())
			return nil, fmt.Errorf("retry enrichment: %w", checkErr)
		}
		// Row exists but not in failed status
		return nil, ErrInvalidStateForRetry
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] update failed,tenant_id:%s,id:%d,err:%+v",
			where, tenantID, id, err.Error())
		return nil, fmt.Errorf("retry enrichment: %w", err)
	}
	result.NextRetryAt = ptrext.Of(nextRetry)
	return ptrext.Of(result), nil
}
