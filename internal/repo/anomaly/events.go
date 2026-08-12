// SPDX-License-Identifier: Apache-2.0

package anomaly

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// Event statuses. open rows are unique per (tenant, slice, direction) via
// the uq_anomaly_events_open partial index.
const (
	EventStatusOpen      = "open"
	EventStatusResolved  = "resolved"
	EventStatusRetracted = "retracted"
)

// Event is one detected anomaly with its lifecycle state.
type Event struct {
	ID              uuid.UUID
	TenantID        string
	SliceType       string
	SliceKey        string
	SliceDisplay    string
	Direction       string
	FirstBucketDate time.Time
	LastBucketDate  time.Time
	Observed        int64
	ExpectedMed     float64
	ExpectedLow     float64
	ExpectedHigh    float64
	ZScore          float64
	Status          string
	QualityActionID *string
	EvidenceJSON    string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ResolvedAt      *time.Time
}

// HitInput carries one detector verdict destined for the event ledger.
type HitInput struct {
	TenantID     string
	SliceType    string
	SliceKey     string
	SliceDisplay string
	Direction    string
	BucketDate   time.Time
	Observed     int64
	ExpectedMed  float64
	ExpectedLow  float64
	ExpectedHigh float64
	Z            float64
	// EvidenceJSON is stored on INSERT only; ongoing hits keep the
	// original evidence (first-occurrence samples and contribution).
	EvidenceJSON string
}

const eventColumns = `
	id, tenant_id, slice_type, slice_key, slice_display, direction,
	first_bucket_date, last_bucket_date, observed,
	expected_med, expected_low, expected_high, z_score,
	status, quality_action_id::text, evidence::text,
	created_at, updated_at, resolved_at`

func scanEvent(row pgx.Row) (Event, error) {
	var e Event
	err := row.Scan(
		&e.ID, &e.TenantID, &e.SliceType, &e.SliceKey, &e.SliceDisplay, &e.Direction,
		&e.FirstBucketDate, &e.LastBucketDate, &e.Observed,
		&e.ExpectedMed, &e.ExpectedLow, &e.ExpectedHigh, &e.ZScore,
		&e.Status, &e.QualityActionID, &e.EvidenceJSON,
		&e.CreatedAt, &e.UpdatedAt, &e.ResolvedAt)
	return e, err
}

// UpsertHit implements the open-row state machine: with no open row for
// (tenant, slice, direction) it INSERTs a fresh event (isNew=true); with
// one it advances last_bucket_date/observed/z (isNew=false). The two-step
// UPDATE-then-INSERT runs in one transaction; the partial unique index
// makes a concurrent double-insert impossible.
func (r *Repo) UpsertHit(ctx context.Context, in HitInput) (Event, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Event{}, false, fmt.Errorf("anomaly upsert hit begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
		UPDATE anomaly_events SET
		  last_bucket_date = $5, observed = $6, expected_med = $7,
		  expected_low = $8, expected_high = $9, z_score = $10, updated_at = NOW()
		WHERE tenant_id = $1 AND slice_type = $2 AND slice_key = $3
		  AND direction = $4 AND status = 'open'
		RETURNING `+eventColumns,
		in.TenantID, in.SliceType, in.SliceKey, in.Direction,
		dateStr(in.BucketDate), in.Observed, in.ExpectedMed,
		in.ExpectedLow, in.ExpectedHigh, in.Z)
	ev, err := scanEvent(row)
	if err == nil {
		return ev, false, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Event{}, false, fmt.Errorf("anomaly upsert hit update: %w", err)
	}

	row = tx.QueryRow(ctx, `
		INSERT INTO anomaly_events
		  (tenant_id, slice_type, slice_key, slice_display, direction,
		   first_bucket_date, last_bucket_date, observed,
		   expected_med, expected_low, expected_high, z_score, evidence)
		VALUES ($1,$2,$3,$4,$5,$6,$6,$7,$8,$9,$10,$11,$12::jsonb)
		RETURNING `+eventColumns,
		in.TenantID, in.SliceType, in.SliceKey, in.SliceDisplay, in.Direction,
		dateStr(in.BucketDate), in.Observed,
		in.ExpectedMed, in.ExpectedLow, in.ExpectedHigh, in.Z,
		nonEmptyJSON(in.EvidenceJSON))
	ev, err = scanEvent(row)
	if err != nil {
		return Event{}, false, fmt.Errorf("anomaly upsert hit insert: %w", err)
	}
	return ev, true, tx.Commit(ctx)
}

func nonEmptyJSON(s string) string {
	if s == "" {
		return "{}"
	}
	return s
}

// SetQualityAction links the event to its control-tower quality action.
func (r *Repo) SetQualityAction(ctx context.Context, eventID uuid.UUID, actionID string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE anomaly_events SET quality_action_id = $2::uuid, updated_at = NOW()
		WHERE id = $1`, eventID, actionID)
	if err != nil {
		return fmt.Errorf("anomaly set quality action: %w", err)
	}
	return nil
}

// ListOpenEvents returns all open events for reconciliation.
func (r *Repo) ListOpenEvents(ctx context.Context, tenantID string) ([]Event, error) {
	return r.listEvents(ctx, tenantID, EventStatusOpen, "", 0)
}

// ListEvents returns events filtered by status ("" = all), newest first.
func (r *Repo) ListEvents(ctx context.Context, tenantID, status string, limit int) ([]Event, error) {
	return r.listEvents(ctx, tenantID, status, "", limit)
}

// ListEventsBySliceType additionally filters by slice type in SQL so the
// limit applies AFTER filtering (an in-memory filter would silently drop
// matching events past the limit).
func (r *Repo) ListEventsBySliceType(ctx context.Context, tenantID, status, sliceType string, limit int) ([]Event, error) {
	return r.listEvents(ctx, tenantID, status, sliceType, limit)
}

func (r *Repo) listEvents(ctx context.Context, tenantID, status, sliceType string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `
		SELECT `+eventColumns+` FROM anomaly_events
		WHERE tenant_id = $1 AND ($2 = '' OR status = $2)
		  AND ($4 = '' OR slice_type = $4)
		ORDER BY last_bucket_date DESC, created_at DESC
		LIMIT $3`, tenantID, status, limit, sliceType)
	if err != nil {
		return nil, fmt.Errorf("anomaly list events: %w", err)
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetEvent fetches one event scoped to the tenant.
func (r *Repo) GetEvent(ctx context.Context, tenantID string, id uuid.UUID) (*Event, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT `+eventColumns+` FROM anomaly_events
		WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	e, err := scanEvent(row)
	if err != nil {
		return nil, fmt.Errorf("anomaly get event: %w", err)
	}
	return ptrext.Of(e), nil
}

// ResolveEvent transitions open → resolved.
func (r *Repo) ResolveEvent(ctx context.Context, tenantID string, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE anomaly_events
		SET status = 'resolved', resolved_at = NOW(), updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND status = 'open'`, tenantID, id)
	if err != nil {
		return fmt.Errorf("anomaly resolve event: %w", err)
	}
	return nil
}

// RetractEvent transitions open|resolved → retracted (data correction).
func (r *Repo) RetractEvent(ctx context.Context, tenantID string, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE anomaly_events
		SET status = 'retracted', updated_at = NOW()
		WHERE tenant_id = $1 AND id = $2 AND status IN ('open','resolved')`, tenantID, id)
	if err != nil {
		return fmt.Errorf("anomaly retract event: %w", err)
	}
	return nil
}

// DigestAnomaly is the digest-facing projection of one open event.
type DigestAnomaly struct {
	SliceDisplay string
	Direction    string
	Observed     int64
	ExpectedMed  float64
	EventID      string
}

// OpenDigestAnomaliesInWindow lists open events whose last bucket falls in
// [from, to) — the digest section source. Only tenants whose anomaly
// notify_mode is 'digest' get the section: 'immediate' tenants were
// already webhooked (a digest repeat is double delivery) and 'off'
// tenants explicitly opted out of anomaly notifications. Tenants without
// a config row default to 'immediate' and are therefore excluded.
func (r *Repo) OpenDigestAnomaliesInWindow(
	ctx context.Context, tenantID string, from, to time.Time,
) ([]DigestAnomaly, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.slice_display, e.direction, e.observed, e.expected_med, e.id::text
		FROM anomaly_events e
		JOIN tenant_anomaly_configs c ON c.tenant_id = e.tenant_id
		WHERE e.tenant_id = $1 AND e.status = 'open'
		  AND c.notify_mode = 'digest'
		  AND e.last_bucket_date >= $2::date AND e.last_bucket_date < $3::date
		ORDER BY ABS(e.z_score) DESC
		LIMIT 10`, tenantID, dateStr(from), dateStr(to))
	if err != nil {
		return nil, fmt.Errorf("anomaly digest window: %w", err)
	}
	defer rows.Close()
	var out []DigestAnomaly
	for rows.Next() {
		var d DigestAnomaly
		if err := rows.Scan(&d.SliceDisplay, &d.Direction, &d.Observed, &d.ExpectedMed, &d.EventID); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// FilterLiveFeedbackIDs returns the subset of ids whose feedback rows still
// exist for the tenant — evidence samples must never leak GDPR-deleted ids.
func (r *Repo) FilterLiveFeedbackIDs(ctx context.Context, tenantID string, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id FROM user_feedback
		WHERE tenant_id = $1 AND id = ANY($2)
		ORDER BY id DESC`, tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("anomaly live feedback ids: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetEvidence writes the evidence document for one event. Split from
// UpsertHit so the worker can skip the (multi-query) contribution
// computation entirely for ongoing hits, which never store new evidence.
func (r *Repo) SetEvidence(ctx context.Context, eventID uuid.UUID, evidenceJSON string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE anomaly_events SET evidence = $2::jsonb, updated_at = NOW()
		WHERE id = $1`, eventID, nonEmptyJSON(evidenceJSON))
	if err != nil {
		return fmt.Errorf("anomaly set evidence: %w", err)
	}
	return nil
}
