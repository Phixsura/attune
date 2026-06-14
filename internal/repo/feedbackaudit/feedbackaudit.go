package feedbackaudit

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type Entry struct {
	ID         int64
	TenantID   string
	FeedbackID int64
	EntityType string
	FieldName  string
	OldValue   *string
	NewValue   *string
	Comment    string
	ChangedBy  string
	CreatedAt  time.Time
}

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

func (r *Repo) WriteTx(ctx context.Context, tx pgx.Tx, e Entry) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO feedback_audit_log
			(tenant_id, feedback_id, entity_type, field_name, old_value, new_value, comment, changed_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.TenantID, e.FeedbackID, e.EntityType, e.FieldName,
		e.OldValue, e.NewValue, e.Comment, e.ChangedBy)
	if err != nil {
		logext.Errorf(ctx, "[repo.feedbackaudit.WriteTx] insert failed,feedback_id:%d,err:%+v",
			e.FeedbackID, err.Error())
		return fmt.Errorf("write audit entry: %w", err)
	}
	return nil
}

func (r *Repo) Write(ctx context.Context, e Entry) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO feedback_audit_log
			(tenant_id, feedback_id, entity_type, field_name, old_value, new_value, comment, changed_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.TenantID, e.FeedbackID, e.EntityType, e.FieldName,
		e.OldValue, e.NewValue, e.Comment, e.ChangedBy)
	if err != nil {
		logext.Errorf(ctx, "[repo.feedbackaudit.Write] insert failed,feedback_id:%d,err:%+v",
			e.FeedbackID, err.Error())
		return fmt.Errorf("write audit entry: %w", err)
	}
	return nil
}

const selectCols = `id, tenant_id, feedback_id, entity_type, field_name,
	old_value, new_value, comment, changed_by, created_at`

func (r *Repo) List(ctx context.Context, feedbackID int64, cursor string, limit int) ([]Entry, string, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	args := []any{feedbackID}
	where := "WHERE feedback_id = $1"
	if cursor != "" {
		cursorID, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor: %w", err)
		}
		args = append(args, cursorID)
		where += fmt.Sprintf(" AND id < $%d", len(args))
	}
	args = append(args, limit+1)
	query := "SELECT " + selectCols + " FROM feedback_audit_log " +
		where + fmt.Sprintf(" ORDER BY id DESC LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		logext.Errorf(ctx, "[repo.feedbackaudit.List] query failed,feedback_id:%d,err:%+v",
			feedbackID, err.Error())
		return nil, "", fmt.Errorf("list audit entries: %w", err)
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var e Entry
		// ptrext:allow scan-target
		if err := rows.Scan(&e.ID, &e.TenantID, &e.FeedbackID, &e.EntityType, &e.FieldName,
			&e.OldValue, &e.NewValue, &e.Comment, &e.ChangedBy, &e.CreatedAt); err != nil {
			return nil, "", fmt.Errorf("scan audit entry: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var nextCursor string
	if len(out) > limit {
		nextCursor = strconv.FormatInt(out[limit].ID, 10)
		out = out[:limit]
	}
	return out, nextCursor, nil
}
