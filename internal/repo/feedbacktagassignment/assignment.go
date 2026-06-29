// Package feedbacktagassignment owns the feedback_tag_assignments junction
// table — per-assignment audit trail linking feedback rows to tags (#28).
package feedbacktagassignment

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type Assignment struct {
	FeedbackID int64
	TagID      uuid.UUID
	CreatedBy  string
	CreatedAt  time.Time
}

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

func (r *Repo) Add(ctx context.Context, tenantID string, feedbackID int64, tagID uuid.UUID, createdBy string) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`INSERT INTO feedback_tag_assignments (feedback_id, tag_id, created_by)
		 SELECT $1, $2, $3
		 FROM user_feedback f
		 JOIN tenant_feedback_tags t ON t.id = $2 AND t.tenant_id = $4
		 WHERE f.id = $1 AND f.tenant_id = $4
		 ON CONFLICT (feedback_id, tag_id) DO NOTHING`,
		feedbackID, tagID, createdBy, tenantID,
	)
	if err != nil {
		return false, fmt.Errorf("add tag assignment: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) Remove(ctx context.Context, tenantID string, feedbackID int64, tagID uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM feedback_tag_assignments
		 WHERE feedback_id = $1 AND tag_id = $2
		   AND feedback_id IN (SELECT id FROM user_feedback WHERE tenant_id = $3)`,
		feedbackID, tagID, tenantID,
	)
	if err != nil {
		return false, fmt.Errorf("remove tag assignment: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (r *Repo) RemoveByScopeExcluding(
	ctx context.Context, tenantID string, feedbackID int64, scope string, excludeTagID uuid.UUID,
) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx,
		`DELETE FROM feedback_tag_assignments
		 WHERE feedback_id = $1
		   AND tag_id != $4
		   AND tag_id IN (
		     SELECT id FROM tenant_feedback_tags
		     WHERE exclusive_scope = $2 AND tenant_id = $3
		   )
		   AND feedback_id IN (SELECT id FROM user_feedback WHERE tenant_id = $3)
		 RETURNING tag_id`,
		feedbackID, scope, tenantID, excludeTagID,
	)
	if err != nil {
		return nil, fmt.Errorf("remove by scope: %w", err)
	}
	defer rows.Close()
	var removed []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil { // ptrext:allow scan-target
			return nil, fmt.Errorf("scan removed tag id: %w", err)
		}
		removed = append(removed, id)
	}
	return removed, rows.Err()
}

// TagInfo is a joined view of a tag assignment with its registry row.
type TagInfo struct {
	TagID          uuid.UUID
	Name           string
	Color          string
	Description    string
	ExclusiveScope *string
	UsageCount     int
	Archived       bool
	CreatedBy      string
	TagCreatedAt   time.Time
	TagUpdatedAt   time.Time
	AssignedBy     string
	AssignedAt     time.Time
}

func (r *Repo) ListByFeedback(ctx context.Context, tenantID string, feedbackID int64) ([]TagInfo, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT t.id, t.name, t.color, t.description, t.exclusive_scope,
		        t.usage_count, t.archived_at IS NOT NULL,
		        t.created_by, t.created_at, t.updated_at,
		        a.created_by, a.created_at
		 FROM feedback_tag_assignments a
		 JOIN tenant_feedback_tags t ON t.id = a.tag_id AND t.tenant_id = $2
		 WHERE a.feedback_id = $1
		 ORDER BY a.created_at`,
		feedbackID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list by feedback: %w", err)
	}
	defer rows.Close()
	var out []TagInfo
	for rows.Next() {
		var ti TagInfo
		if err := rows.Scan(
			&ti.TagID, &ti.Name, &ti.Color, &ti.Description, &ti.ExclusiveScope,
			&ti.UsageCount, &ti.Archived,
			&ti.CreatedBy, &ti.TagCreatedAt, &ti.TagUpdatedAt,
			&ti.AssignedBy, &ti.AssignedAt,
		); err != nil {
			return nil, fmt.Errorf("scan tag info: %w", err)
		}
		out = append(out, ti)
	}
	return out, rows.Err()
}

func (r *Repo) ListByFeedbackBatch(ctx context.Context, tenantID string, feedbackIDs []int64) (map[int64][]TagInfo, error) {
	if len(feedbackIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT a.feedback_id,
		        t.id, t.name, t.color, t.description, t.exclusive_scope,
		        t.usage_count, t.archived_at IS NOT NULL,
		        t.created_by, t.created_at, t.updated_at,
		        a.created_by, a.created_at
		 FROM feedback_tag_assignments a
		 JOIN tenant_feedback_tags t ON t.id = a.tag_id AND t.tenant_id = $2
		 WHERE a.feedback_id = ANY($1)
		 ORDER BY a.feedback_id, a.created_at`,
		feedbackIDs, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list by feedback batch: %w", err)
	}
	defer rows.Close()
	out := make(map[int64][]TagInfo)
	for rows.Next() {
		var fbID int64
		var ti TagInfo
		if err := rows.Scan(
			&fbID,
			&ti.TagID, &ti.Name, &ti.Color, &ti.Description, &ti.ExclusiveScope,
			&ti.UsageCount, &ti.Archived,
			&ti.CreatedBy, &ti.TagCreatedAt, &ti.TagUpdatedAt,
			&ti.AssignedBy, &ti.AssignedAt,
		); err != nil {
			return nil, fmt.Errorf("scan batch tag info: %w", err)
		}
		out[fbID] = append(out[fbID], ti)
	}
	return out, rows.Err()
}
