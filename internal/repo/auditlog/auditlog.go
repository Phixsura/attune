// SPDX-License-Identifier: Apache-2.0

package auditlog

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

type Entry struct {
	ID             int64
	TenantID       string
	ActorType      string
	ActorID        string
	ActorEmail     string
	ActorIP        string
	ActorUserAgent string
	Action         string
	TargetType     string
	TargetID       string
	Summary        string
	BeforeJSON     json.RawMessage
	AfterJSON      json.RawMessage
	CreatedAt      time.Time
}

type ListFilter struct {
	TenantID   string
	Actions    []string
	ActorType  string
	ActorID    string
	TargetType string
	TargetID   string
	From       *time.Time
	To         *time.Time
	Limit      int
	Cursor     string
	Unbounded  bool
}

type ListResult struct {
	Items      []Entry
	NextCursor string
}

func (r *Repo) Insert(ctx context.Context, entry Entry) error {
	const q = `
		INSERT INTO audit_log (
			tenant_id, actor_type, actor_id, actor_email, actor_ip, actor_user_agent,
			action, target_type, target_id, summary, before_json, after_json
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	_, err := r.pool.Exec(
		ctx,
		q,
		entry.TenantID,
		entry.ActorType,
		entry.ActorID,
		entry.ActorEmail,
		entry.ActorIP,
		entry.ActorUserAgent,
		entry.Action,
		entry.TargetType,
		entry.TargetID,
		entry.Summary,
		nullableJSON(entry.BeforeJSON),
		nullableJSON(entry.AfterJSON),
	)
	return err
}

func (r *Repo) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	limit := boundedLimit(filter)
	query, args, err := buildListQuery(filter, limit)
	if err != nil {
		return ListResult{}, err
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	items, err := scanEntries(rows)
	if err != nil {
		return ListResult{}, err
	}
	return paginateListResult(items, limit, filter.Unbounded), nil
}

func (r *Repo) PruneBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT set_config('attune.audit_prune', 'on', true)`); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('attune.audit_prune_before', $1, true)`, cutoff.UTC().Format(time.RFC3339Nano)); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM audit_log WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func nullableJSON(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

func boundedLimit(filter ListFilter) int {
	if filter.Unbounded {
		return filter.Limit
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	return limit
}

func buildListQuery(filter ListFilter, limit int) (string, []any, error) {
	query := `
		SELECT id, tenant_id, actor_type, actor_id, actor_email, actor_ip, actor_user_agent,
		       action, target_type, target_id, summary, before_json, after_json, created_at
		FROM audit_log
		WHERE tenant_id = $1`
	args := []any{filter.TenantID}
	query, args = appendActionFilter(query, args, filter.Actions)
	query, args = appendExactFilter(query, args, "actor_type", filter.ActorType)
	query, args = appendExactFilter(query, args, "actor_id", filter.ActorID)
	query, args = appendExactFilter(query, args, "target_type", filter.TargetType)
	query, args = appendExactFilter(query, args, "target_id", filter.TargetID)
	query, args = appendTimeFilter(query, args, "created_at >= ", filter.From)
	query, args = appendTimeFilter(query, args, "created_at <= ", filter.To)
	return appendListOrderAndLimit(query, args, filter, limit)
}

func appendActionFilter(query string, args []any, actions []string) (string, []any) {
	if len(actions) == 0 {
		return query, args
	}
	placeholders := make([]string, 0, len(actions))
	for _, action := range actions {
		args = append(args, action)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	query += fmt.Sprintf(" AND action IN (%s)", strings.Join(placeholders, ", "))
	return query, args
}

func appendExactFilter(query string, args []any, column, value string) (string, []any) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return query, args
	}
	args = append(args, trimmed)
	query += fmt.Sprintf(" AND %s = $%d", column, len(args))
	return query, args
}

func appendTimeFilter(query string, args []any, clause string, value *time.Time) (string, []any) {
	if value == nil {
		return query, args
	}
	args = append(args, ptrext.Indirect(value))
	query += fmt.Sprintf(" AND %s$%d", clause, len(args))
	return query, args
}

func appendListOrderAndLimit(query string, args []any, filter ListFilter, limit int) (string, []any, error) {
	if !filter.Unbounded && strings.TrimSpace(filter.Cursor) != "" {
		cursorTime, cursorID, err := ParseCursor(filter.Cursor)
		if err != nil {
			return "", nil, err
		}
		args = append(args, cursorTime, cursorID)
		query += fmt.Sprintf(" AND (created_at, id) < ($%d, $%d)", len(args)-1, len(args))
	}
	query += " ORDER BY created_at DESC, id DESC"
	if filter.Unbounded {
		return query, args, nil
	}
	args = append(args, limit+1)
	query += fmt.Sprintf(" LIMIT $%d", len(args))
	return query, args, nil
}

func scanEntries(rows pgxRows) ([]Entry, error) {
	var items []Entry
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

type pgxRows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanEntry(rows pgxRows) (Entry, error) {
	var entry Entry
	err := rows.Scan(
		&entry.ID,
		&entry.TenantID,
		&entry.ActorType,
		&entry.ActorID,
		&entry.ActorEmail,
		&entry.ActorIP,
		&entry.ActorUserAgent,
		&entry.Action,
		&entry.TargetType,
		&entry.TargetID,
		&entry.Summary,
		&entry.BeforeJSON,
		&entry.AfterJSON,
		&entry.CreatedAt,
	)
	return entry, err
}

func paginateListResult(items []Entry, limit int, unbounded bool) ListResult {
	if unbounded || len(items) <= limit {
		return ListResult{Items: items}
	}
	return ListResult{
		Items:      items[:limit],
		NextCursor: formatCursor(items[limit-1]),
	}
}

func formatCursor(entry Entry) string {
	return fmt.Sprintf("%d:%d", entry.CreatedAt.UTC().UnixNano(), entry.ID)
}

func ParseCursor(raw string) (time.Time, int64, error) {
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 {
		return time.Time{}, 0, fmt.Errorf("invalid audit cursor format")
	}
	unixNanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid audit cursor timestamp: %w", err)
	}
	id, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("invalid audit cursor id: %w", err)
	}
	return time.Unix(0, unixNanos).UTC(), id, nil
}
