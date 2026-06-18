package workflowstate

import (
	"context"
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

var (
	ErrNotFound     = errors.New("workflow state not found")
	ErrNameConflict = errors.New("workflow state name conflict")
)

type WorkflowState struct {
	ID       string
	TenantID string
	// Name is the stable machine key (slug, ^[a-z][a-z0-9_]{0,30}$); the
	// human-facing label lives in DisplayName (i18n). Mirrors Dimension.
	Name        string
	DisplayName domain.I18nString
	Color       string
	Category    string
	Position    int
	IsDefault   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ArchivedAt  *time.Time
}

type Transition struct {
	ID          string
	TenantID    string
	FromStateID string
	ToStateID   string
	CreatedAt   time.Time
}

type TransitionEdge struct {
	FromStateID string
	ToStateID   string
}

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

const selectStateCols = `id, tenant_id, name, display_name, color, category, position,
	is_default, created_at, updated_at, archived_at`

const selectStateColsQualified = `s.id, s.tenant_id, s.name, s.display_name, s.color, s.category, s.position,
	s.is_default, s.created_at, s.updated_at, s.archived_at`

func marshalDisplayName(d domain.I18nString) ([]byte, error) {
	if d == nil {
		d = domain.I18nString{}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return nil, fmt.Errorf("marshal display_name: %w", err)
	}
	return b, nil
}

func unmarshalDisplayName(raw []byte) (domain.I18nString, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var d domain.I18nString
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("unmarshal display_name: %w", err)
	}
	return d, nil
}

func scanState(row pgx.Row) (WorkflowState, error) {
	var s WorkflowState
	var displayRaw []byte
	// ptrext:allow scan-target
	err := row.Scan(&s.ID, &s.TenantID, &s.Name, &displayRaw, &s.Color, &s.Category,
		&s.Position, &s.IsDefault, &s.CreatedAt, &s.UpdatedAt, &s.ArchivedAt)
	if err != nil {
		return s, err
	}
	s.DisplayName, err = unmarshalDisplayName(displayRaw)
	return s, err
}

func scanStateRows(rows pgx.Rows) ([]WorkflowState, error) {
	defer rows.Close()
	var out []WorkflowState
	for rows.Next() {
		var s WorkflowState
		var displayRaw []byte
		// ptrext:allow scan-target
		if err := rows.Scan(&s.ID, &s.TenantID, &s.Name, &displayRaw, &s.Color, &s.Category,
			&s.Position, &s.IsDefault, &s.CreatedAt, &s.UpdatedAt, &s.ArchivedAt); err != nil {
			return nil, fmt.Errorf("scan workflow state: %w", err)
		}
		display, err := unmarshalDisplayName(displayRaw)
		if err != nil {
			return nil, err
		}
		s.DisplayName = display
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *Repo) List(ctx context.Context, tenantID string, includeArchived bool) ([]WorkflowState, error) {
	where := "WHERE tenant_id = $1"
	if !includeArchived {
		where += " AND archived_at IS NULL"
	}
	rows, err := r.pool.Query(ctx,
		"SELECT "+selectStateCols+" FROM tenant_workflow_states "+where+" ORDER BY position, created_at",
		tenantID)
	if err != nil {
		logext.Errorf(ctx, "[repo.workflowstate.List] query failed,tenant_id:%s,err:%+v", tenantID, err.Error())
		return nil, fmt.Errorf("list workflow states: %w", err)
	}
	return scanStateRows(rows)
}

func (r *Repo) Get(ctx context.Context, id string) (*WorkflowState, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT "+selectStateCols+" FROM tenant_workflow_states WHERE id = $1", id)
	s, err := scanState(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[repo.workflowstate.Get] query failed,id:%s,err:%+v", id, err.Error())
		return nil, fmt.Errorf("get workflow state: %w", err)
	}
	return ptrext.Of(s), nil
}

func (r *Repo) GetByTenantAndID(ctx context.Context, tenantID, id string) (*WorkflowState, error) {
	row := r.pool.QueryRow(ctx,
		"SELECT "+selectStateCols+" FROM tenant_workflow_states WHERE id = $1 AND tenant_id = $2", id, tenantID)
	s, err := scanState(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		logext.Errorf(ctx, "[repo.workflowstate.GetByTenantAndID] query failed,id:%s,err:%+v", id, err.Error())
		return nil, fmt.Errorf("get workflow state: %w", err)
	}
	return ptrext.Of(s), nil
}

func (r *Repo) Create(ctx context.Context, s WorkflowState) (*WorkflowState, error) {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	dn, mErr := marshalDisplayName(s.DisplayName)
	if mErr != nil {
		return nil, mErr
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO tenant_workflow_states (id, tenant_id, name, display_name, color, category, position, is_default)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING `+selectStateCols,
		s.ID, s.TenantID, s.Name, dn, s.Color, s.Category, s.Position, s.IsDefault)
	created, err := scanState(row)
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return nil, ErrNameConflict
		}
		logext.Errorf(ctx, "[repo.workflowstate.Create] insert failed,tenant_id:%s,err:%+v", s.TenantID, err.Error())
		return nil, fmt.Errorf("create workflow state: %w", err)
	}
	return ptrext.Of(created), nil
}

func (r *Repo) Update(ctx context.Context, s WorkflowState) (*WorkflowState, error) {
	dn, mErr := marshalDisplayName(s.DisplayName)
	if mErr != nil {
		return nil, mErr
	}
	row := r.pool.QueryRow(ctx, `
		UPDATE tenant_workflow_states
		SET display_name = $2, color = $3, position = $4, is_default = $5, updated_at = NOW()
		WHERE id = $1 AND tenant_id = $6 AND archived_at IS NULL
		RETURNING `+selectStateCols,
		s.ID, dn, s.Color, s.Position, s.IsDefault, s.TenantID)
	updated, err := scanState(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		if pgxutil.IsUniqueViolation(err) {
			return nil, ErrNameConflict
		}
		logext.Errorf(ctx, "[repo.workflowstate.Update] update failed,id:%s,err:%+v", s.ID, err.Error())
		return nil, fmt.Errorf("update workflow state: %w", err)
	}
	return ptrext.Of(updated), nil
}

func (r *Repo) Archive(ctx context.Context, tenantID, id string) error {
	ct, err := r.pool.Exec(ctx, `
		UPDATE tenant_workflow_states SET archived_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND archived_at IS NULL`, id, tenantID)
	if err != nil {
		logext.Errorf(ctx, "[repo.workflowstate.Archive] failed,id:%s,err:%+v", id, err.Error())
		return fmt.Errorf("archive workflow state: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repo) DeleteTransitionsForState(ctx context.Context, tenantID, stateID string) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM tenant_workflow_transitions
		WHERE tenant_id = $1 AND (from_state_id = $2 OR to_state_id = $2)`, tenantID, stateID)
	if err != nil {
		logext.Errorf(ctx, "[repo.workflowstate.DeleteTransitionsForState] failed,state_id:%s,err:%+v", stateID, err.Error())
		return fmt.Errorf("delete transitions for state: %w", err)
	}
	return nil
}

func (r *Repo) CountActiveFeedback(ctx context.Context, tenantID, stateID string) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM user_feedback WHERE tenant_id = $1 AND workflow_state_id = $2::uuid",
		tenantID, stateID).Scan(&count) // ptrext:allow scan-target
	if err != nil {
		return 0, fmt.Errorf("count active feedback: %w", err)
	}
	return count, nil
}

func (r *Repo) CountActiveDefaults(ctx context.Context, tenantID string) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM tenant_workflow_states WHERE tenant_id = $1 AND is_default AND archived_at IS NULL",
		tenantID).Scan(&count) // ptrext:allow scan-target
	if err != nil {
		return 0, fmt.Errorf("count active defaults: %w", err)
	}
	return count, nil
}

func (r *Repo) CheckTransition(ctx context.Context, tenantID, fromID, toID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM tenant_workflow_transitions wt
			JOIN tenant_workflow_states fs ON fs.id = wt.from_state_id
			JOIN tenant_workflow_states ts ON ts.id = wt.to_state_id
			WHERE wt.tenant_id = $1 AND wt.from_state_id = $2::uuid AND wt.to_state_id = $3::uuid
			  AND fs.archived_at IS NULL AND ts.archived_at IS NULL
		)`, tenantID, fromID, toID).Scan(&exists) // ptrext:allow scan-target
	if err != nil {
		return false, fmt.Errorf("check transition: %w", err)
	}
	return exists, nil
}

func (r *Repo) CheckTransitionTx(ctx context.Context, tx pgx.Tx, tenantID, fromID, toID string) (bool, error) {
	var exists bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM tenant_workflow_transitions wt
			JOIN tenant_workflow_states fs ON fs.id = wt.from_state_id
			JOIN tenant_workflow_states ts ON ts.id = wt.to_state_id
			WHERE wt.tenant_id = $1 AND wt.from_state_id = $2::uuid AND wt.to_state_id = $3::uuid
			  AND fs.archived_at IS NULL AND ts.archived_at IS NULL
		)`, tenantID, fromID, toID).Scan(&exists) // ptrext:allow scan-target
	if err != nil {
		return false, fmt.Errorf("check transition tx: %w", err)
	}
	return exists, nil
}

func (r *Repo) ArchiveWithTransitions(ctx context.Context, tenantID, stateID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `
		DELETE FROM tenant_workflow_transitions
		WHERE tenant_id = $1 AND (from_state_id = $2 OR to_state_id = $2)`,
		tenantID, stateID); err != nil {
		return fmt.Errorf("delete transitions for state: %w", err)
	}

	ct, err := tx.Exec(ctx, `
		UPDATE tenant_workflow_states SET archived_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND tenant_id = $2 AND archived_at IS NULL`, stateID, tenantID)
	if err != nil {
		return fmt.Errorf("archive workflow state: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}

	return tx.Commit(ctx)
}

func (r *Repo) AllowedNext(ctx context.Context, tenantID, fromID string) ([]WorkflowState, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+selectStateColsQualified+` FROM tenant_workflow_states s
		JOIN tenant_workflow_transitions wt ON wt.to_state_id = s.id
		WHERE wt.tenant_id = $1 AND wt.from_state_id = $2::uuid
		  AND s.archived_at IS NULL
		ORDER BY s.position, s.created_at`, tenantID, fromID)
	if err != nil {
		logext.Errorf(ctx, "[repo.workflowstate.AllowedNext] query failed,from:%s,err:%+v", fromID, err.Error())
		return nil, fmt.Errorf("allowed next states: %w", err)
	}
	return scanStateRows(rows)
}

func (r *Repo) ListTransitions(ctx context.Context, tenantID string) ([]Transition, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, from_state_id, to_state_id, created_at
		FROM tenant_workflow_transitions
		WHERE tenant_id = $1
		ORDER BY created_at`, tenantID)
	if err != nil {
		logext.Errorf(ctx, "[repo.workflowstate.ListTransitions] query failed,tenant_id:%s,err:%+v", tenantID, err.Error())
		return nil, fmt.Errorf("list transitions: %w", err)
	}
	defer rows.Close()
	var out []Transition
	for rows.Next() {
		var t Transition
		// ptrext:allow scan-target
		if err := rows.Scan(&t.ID, &t.TenantID, &t.FromStateID, &t.ToStateID, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan transition: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *Repo) ReplaceTransitions(ctx context.Context, tenantID string, edges []TransitionEdge) ([]Transition, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, "DELETE FROM tenant_workflow_transitions WHERE tenant_id = $1", tenantID); err != nil {
		return nil, fmt.Errorf("delete old transitions: %w", err)
	}

	var out []Transition
	for _, e := range edges {
		var t Transition
		// ptrext:allow scan-target
		err := tx.QueryRow(ctx, `
			INSERT INTO tenant_workflow_transitions (tenant_id, from_state_id, to_state_id)
			VALUES ($1, $2::uuid, $3::uuid)
			RETURNING id, tenant_id, from_state_id, to_state_id, created_at`,
			tenantID, e.FromStateID, e.ToStateID,
		).Scan(&t.ID, &t.TenantID, &t.FromStateID, &t.ToStateID, &t.CreatedAt)
		if err != nil {
			logext.Errorf(ctx, "[repo.workflowstate.ReplaceTransitions] insert failed,from:%s,to:%s,err:%+v",
				e.FromStateID, e.ToStateID, err.Error())
			return nil, fmt.Errorf("insert transition: %w", err)
		}
		out = append(out, t)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit transitions: %w", err)
	}
	return out, nil
}

func (r *Repo) UpsertStateReturningID(ctx context.Context, tx pgx.Tx, s WorkflowState) (string, error) {
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	dn, mErr := marshalDisplayName(s.DisplayName)
	if mErr != nil {
		return "", mErr
	}
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO tenant_workflow_states (id, tenant_id, name, display_name, color, category, position, is_default)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (tenant_id, name) WHERE archived_at IS NULL
		DO UPDATE SET display_name = EXCLUDED.display_name
		RETURNING id`,
		s.ID, s.TenantID, s.Name, dn, s.Color, s.Category, s.Position, s.IsDefault,
	).Scan(&id) // ptrext:allow scan-target
	if err != nil {
		return "", fmt.Errorf("upsert state: %w", err)
	}
	return id, nil
}

func (r *Repo) InsertTransitionIgnoreConflict(ctx context.Context, tx pgx.Tx, tenantID, fromID, toID string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tenant_workflow_transitions (tenant_id, from_state_id, to_state_id)
		VALUES ($1, $2::uuid, $3::uuid)
		ON CONFLICT (tenant_id, from_state_id, to_state_id) DO NOTHING`,
		tenantID, fromID, toID)
	if err != nil {
		return fmt.Errorf("insert transition: %w", err)
	}
	return nil
}

func (r *Repo) GetCurrentStateForUpdate(ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64) (*string, error) {
	var stateID *string
	err := tx.QueryRow(ctx,
		"SELECT workflow_state_id FROM user_feedback WHERE id = $1 AND tenant_id = $2 FOR UPDATE",
		feedbackID, tenantID).Scan(&stateID) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get current state for update: %w", err)
	}
	return stateID, nil
}

func (r *Repo) SetFeedbackState(ctx context.Context, tx pgx.Tx, feedbackID int64, stateID string) error {
	_, err := tx.Exec(ctx,
		"UPDATE user_feedback SET workflow_state_id = $1::uuid, workflow_updated_at = NOW() WHERE id = $2",
		stateID, feedbackID)
	if err != nil {
		return fmt.Errorf("set feedback state: %w", err)
	}
	return nil
}
