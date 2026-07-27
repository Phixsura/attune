// SPDX-License-Identifier: Apache-2.0

package cohortsync

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// ApplyInput is the input for an atomic membership delta or full-snapshot operation.
type ApplyInput struct {
	TenantID     string
	CohortID     uuid.UUID
	SourceID     uuid.UUID
	Trigger      string
	Members      []MembershipUpsert
	RemoveIDs    []string  // for incremental remove deltas
	StaleTTLDays int       // for MarkDeparted
	OlderThan    time.Time // for full-snapshot reconciliation (zero = skip MarkDeparted)
	IsSnapshot   bool      // true = run MarkDeparted after upsert
}

// ApplyResult is the outcome of an atomic apply operation.
type ApplyResult struct {
	Run          SyncRun
	MembersAdded int
	Removed      int64
	MemberCount  int
}

// ApplyMembershipDelta atomically applies a membership delta or full snapshot.
// All operations run in a single database transaction: if any step fails,
// the entire operation rolls back with no partial state.
func (r *Repo) ApplyMembershipDelta(ctx context.Context, in ApplyInput) (ApplyResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("begin apply tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 1. Insert sync run.
	var run SyncRun
	runID := uuid.New()
	err = tx.QueryRow(ctx, `
		INSERT INTO cohort_sync_runs (id, tenant_id, cohort_id, trigger, status)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING started_at, created_at`,
		runID, in.TenantID, in.CohortID, in.Trigger, "running",
	).Scan(&run.StartedAt, &run.CreatedAt) // ptrext:allow scan-out-param
	if err != nil {
		return ApplyResult{}, fmt.Errorf("insert run: %w", err)
	}
	run.ID = runID
	run.TenantID = in.TenantID
	run.CohortID = in.CohortID
	run.Trigger = in.Trigger
	run.Status = "running"

	// 2. Upsert memberships.
	var added int
	if len(in.Members) > 0 {
		batch := pgx.Batch{}
		for _, m := range in.Members {
			props := m.UserProperties
			if len(props) == 0 {
				props = []byte("{}")
			}
			batch.Queue(`
				INSERT INTO cohort_memberships (id, tenant_id, cohort_id, external_user_id, email,
				       display_name, user_properties)
				VALUES (gen_random_uuid(), $1, $2, $3, $4, $5, $6)
				ON CONFLICT (tenant_id, cohort_id, external_user_id)
				DO UPDATE SET email = EXCLUDED.email, display_name = EXCLUDED.display_name,
				       user_properties = EXCLUDED.user_properties,
				       left_at = NULL, expires_at = NULL, last_seen_at = NOW()`,
				in.TenantID, in.CohortID, m.ExternalUserID, m.Email, m.DisplayName, props)
		}
		br := tx.SendBatch(ctx, &batch) // ptrext:allow batch-send
		for range in.Members {
			if _, batchErr := br.Exec(); batchErr != nil {
				_ = br.Close()
				return ApplyResult{}, fmt.Errorf("upsert membership: %w", batchErr)
			}
			added++
		}
		if closeErr := br.Close(); closeErr != nil {
			return ApplyResult{}, fmt.Errorf("close upsert batch: %w", closeErr)
		}
	}

	// 3. Mark departed (remove deltas or full-snapshot reconciliation).
	var removed int64
	if len(in.RemoveIDs) > 0 {
		tag, err := tx.Exec(ctx, `
			UPDATE cohort_memberships
			   SET left_at = NOW(),
			       expires_at = NOW() + make_interval(days => $4)
			 WHERE tenant_id = $1
			   AND cohort_id = $2
			   AND left_at IS NULL
			   AND external_user_id = ANY($3)`,
			in.TenantID, in.CohortID, in.RemoveIDs, in.StaleTTLDays)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("mark members departed: %w", err)
		}
		removed = tag.RowsAffected()
	}
	if in.IsSnapshot && !in.OlderThan.IsZero() {
		tag, err := tx.Exec(ctx, `
			UPDATE cohort_memberships
			   SET left_at = NOW(),
			       expires_at = NOW() + make_interval(days => $4)
			 WHERE tenant_id = $1
			   AND cohort_id = $2
			   AND left_at IS NULL
			   AND last_seen_at < $3`,
			in.TenantID, in.CohortID, in.OlderThan, in.StaleTTLDays)
		if err != nil {
			return ApplyResult{}, fmt.Errorf("mark snapshot departed: %w", err)
		}
		removed = tag.RowsAffected()
	}

	// 4. Count active members.
	var memberCount int
	err = tx.QueryRow(ctx, `
		SELECT count(*)
		  FROM cohort_memberships
		 WHERE tenant_id = $1 AND cohort_id = $2 AND left_at IS NULL`,
		in.TenantID, in.CohortID).Scan(&memberCount) // ptrext:allow scan-out-param
	if err != nil {
		return ApplyResult{}, fmt.Errorf("count active: %w", err)
	}

	// 5. Update cohort sync result.
	if _, err := tx.Exec(ctx, `
		UPDATE cohorts
		   SET member_count = $3, last_synced_at = NOW(), last_error = '', updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2`,
		in.TenantID, in.CohortID, memberCount); err != nil {
		return ApplyResult{}, fmt.Errorf("update cohort: %w", err)
	}

	// 6. Update source sync status.
	if _, err := tx.Exec(ctx, `
		UPDATE cohort_sources
		   SET last_sync_at = NOW(), last_error = '', status = 'active', updated_at = NOW()
		 WHERE tenant_id = $1 AND id = $2`,
		in.TenantID, in.SourceID); err != nil {
		return ApplyResult{}, fmt.Errorf("update source status: %w", err)
	}

	// 7. Finish run as succeeded.
	if _, err := tx.Exec(ctx, `
		UPDATE cohort_sync_runs
		   SET status = 'succeeded', members_added = $2, members_removed = $3,
		       members_total = $4, finished_at = NOW()
		 WHERE id = $1`,
		run.ID, added, removed, memberCount); err != nil {
		return ApplyResult{}, fmt.Errorf("finish run: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ApplyResult{}, fmt.Errorf("commit apply tx: %w", err)
	}

	run.Status = "succeeded"
	run.MembersAdded = added
	run.MembersRemoved = int(removed)
	run.MembersTotal = memberCount
	run.FinishedAt = ptrext.Of(time.Now())

	return ApplyResult{
		Run:          run,
		MembersAdded: added,
		Removed:      removed,
		MemberCount:  memberCount,
	}, nil
}
