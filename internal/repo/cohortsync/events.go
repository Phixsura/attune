// SPDX-License-Identifier: Apache-2.0

package cohortsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ErrDuplicateEvent is returned when a webhook delivery has already been processed.
var ErrDuplicateEvent = errors.New("duplicate cohort sync event")

// RecordEvent inserts a webhook event for dedup. Returns ErrDuplicateEvent if
// the dedupe_key already exists for this source.
func (r *Repo) RecordEvent(ctx context.Context, in SyncEvent) (*SyncEvent, error) {
	row := in
	if row.ID == uuid.Nil {
		row.ID = uuid.New()
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO cohort_sync_events (id, tenant_id, cohort_source_id, provider,
		       event_type, dedupe_key, status, payload_digest, members_count)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (tenant_id, cohort_source_id, dedupe_key) DO NOTHING
		RETURNING created_at`,
		row.ID, row.TenantID, row.CohortSourceID, row.Provider,
		row.EventType, row.DedupeKey, row.Status, row.PayloadDigest, row.MembersCount,
	).Scan(&row.CreatedAt) // ptrext:allow scan-out-param
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrDuplicateEvent
	}
	if err != nil {
		return nil, fmt.Errorf("record cohort sync event: %w", err)
	}
	return ptrext.Of(row), nil
}

// UpdateEventStatus updates the status and optional run_id of a recorded event.
func (r *Repo) UpdateEventStatus(ctx context.Context, id uuid.UUID, status string, runID *uuid.UUID, failureReason string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE cohort_sync_events
		   SET status = $2, run_id = $3, failure_reason = $4
		 WHERE id = $1`, id, status, runID, failureReason)
	if err != nil {
		return fmt.Errorf("update event status: %w", err)
	}
	return nil
}

// ListEvents returns recent events for a source.
func (r *Repo) ListEvents(ctx context.Context, tenantID string, sourceID uuid.UUID, limit int) ([]SyncEvent, error) {
	if limit <= 0 || limit > defaultLimit {
		limit = defaultLimit
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, cohort_source_id, provider, event_type, dedupe_key,
		       status, payload_digest, members_count, failure_reason, run_id,
		       received_at, created_at
		  FROM cohort_sync_events
		 WHERE tenant_id = $1 AND cohort_source_id = $2
		 ORDER BY created_at DESC
		 LIMIT $3`, tenantID, sourceID, limit)
	if err != nil {
		return nil, fmt.Errorf("list cohort sync events: %w", err)
	}
	defer rows.Close()
	var out []SyncEvent
	for rows.Next() {
		var e SyncEvent
		if err := rows.Scan(
			&e.ID, &e.TenantID, &e.CohortSourceID, &e.Provider,
			&e.EventType, &e.DedupeKey, &e.Status, &e.PayloadDigest,
			&e.MembersCount, &e.FailureReason, &e.RunID,
			&e.ReceivedAt, &e.CreatedAt,
		); err != nil { // ptrext:allow scan-out-param
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventPayloadDigest computes a SHA-256 digest for dedup keying.
func EventPayloadDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
