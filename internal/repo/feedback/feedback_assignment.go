package feedback

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var ErrAssignmentOwnerNotFound = errors.New("feedback assignment owner not found")

const (
	assignmentEscalationDueSoonWindow = 12 * time.Hour
	defaultAssignmentEscalationLimit  = 25
	maxAssignmentEscalationLimit      = 100
)

type Assignment struct {
	FeedbackID      int64
	OwnerMemberID   *string
	OwnerMemberType string
	OwnerUserID     string
	OwnerEmail      string
	OwnerRole       string
	AssignedAt      *time.Time
	AssignedBy      string
	SLADueAt        *time.Time
	Note            string
}

type AssignmentInput struct {
	OwnerMemberIDSet bool
	OwnerMemberID    *string
	SLADueAtSet      bool
	SLADueAt         *time.Time
	Note             string
	ActorID          string
}

type AssignmentCandidate struct {
	ID                    int64
	CreatedAt             time.Time
	Source                string
	Type                  string
	IsUrgent              bool
	EnrichmentStatus      string
	EnrichmentAttempts    int
	EnrichmentNextRetryAt *time.Time
	WorkflowCategory      string
	HasStableIdentity     bool
	Assignment            Assignment
}

type AssignmentEscalationQueue struct {
	GeneratedAt       time.Time
	OverdueCount      int64
	DueSoonCount      int64
	MissingOwnerCount int64
	MissingSLACount   int64
	Items             []AssignmentEscalation
}

type AssignmentEscalation struct {
	FeedbackID    int64
	Title         string
	Source        string
	Type          string
	IsUrgent      bool
	CreatedAt     time.Time
	Assignment    Assignment
	Reasons       []string
	HoursUntilDue *int
	Priority      string
	Account       AccountContext
}

func (r *FeedbackRepo) FeedbackAssignmentEscalations(
	ctx context.Context,
	tenantID string,
	now time.Time,
	limit int,
) (AssignmentEscalationQueue, error) {
	now = normalizeAssignmentEscalationTime(now)
	limit = normalizeAssignmentEscalationLimit(limit)
	tenantID = strings.TrimSpace(tenantID)
	summary, err := r.feedbackAssignmentEscalationSummary(ctx, tenantID, now)
	if err != nil {
		return AssignmentEscalationQueue{}, err
	}
	items, err := r.feedbackAssignmentEscalationItems(ctx, tenantID, now, limit)
	if err != nil {
		return AssignmentEscalationQueue{}, err
	}
	summary.GeneratedAt = now
	summary.Items = items
	return summary, nil
}

func normalizeAssignmentEscalationTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}

func normalizeAssignmentEscalationLimit(limit int) int {
	if limit <= 0 {
		return defaultAssignmentEscalationLimit
	}
	if limit > maxAssignmentEscalationLimit {
		return maxAssignmentEscalationLimit
	}
	return limit
}

func (r *FeedbackRepo) feedbackAssignmentEscalationSummary(
	ctx context.Context,
	tenantID string,
	now time.Time,
) (AssignmentEscalationQueue, error) {
	row := r.pool.QueryRow(ctx, assignmentEscalationSummarySQL(), tenantID, now)
	var out AssignmentEscalationQueue
	if err := row.Scan(&out.OverdueCount, &out.DueSoonCount, &out.MissingOwnerCount, &out.MissingSLACount); err != nil {
		return AssignmentEscalationQueue{}, fmt.Errorf("feedback assignment escalation summary: %w", err)
	}
	return out, nil
}

func (r *FeedbackRepo) feedbackAssignmentEscalationItems(
	ctx context.Context,
	tenantID string,
	now time.Time,
	limit int,
) ([]AssignmentEscalation, error) {
	rows, err := r.pool.Query(ctx, assignmentEscalationItemsSQL(), tenantID, now, limit)
	if err != nil {
		return nil, fmt.Errorf("feedback assignment escalation items: %w", err)
	}
	defer rows.Close()
	var out []AssignmentEscalation
	for rows.Next() {
		item, scanErr := scanAssignmentEscalation(rows, now)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feedback assignment escalation rows: %w", err)
	}
	return out, nil
}

func assignmentEscalationSummarySQL() string {
	return `
		WITH scope AS (
			SELECT uf.owner_member_id, uf.feedback_sla_due_at
			FROM user_feedback uf
			LEFT JOIN tenant_workflow_states ws
			  ON ws.tenant_id = uf.tenant_id AND ws.id = uf.workflow_state_id
			WHERE uf.tenant_id = $1
			  AND uf.deleted_at IS NULL
			  AND COALESCE(ws.category, 'open') <> 'closed'
		)
		SELECT
			COUNT(*) FILTER (WHERE feedback_sla_due_at < $2::timestamptz)::bigint,
			COUNT(*) FILTER (
				WHERE feedback_sla_due_at >= $2::timestamptz
				  AND feedback_sla_due_at < $2::timestamptz + INTERVAL '12 hours'
			)::bigint,
			COUNT(*) FILTER (WHERE owner_member_id IS NULL)::bigint,
			COUNT(*) FILTER (WHERE feedback_sla_due_at IS NULL)::bigint
		FROM scope`
}

func assignmentEscalationItemsSQL() string {
	return `
		SELECT
			uf.id,
			COALESCE(NULLIF(uf.enriched_display_title, ''), NULLIF(uf.enriched_title, ''), LEFT(uf.content, 96)),
			COALESCE(uf.source, ''),
			COALESCE(uf.type, ''),
			uf.is_urgent,
			uf.created_at,
			uf.owner_member_id::text,
			uf.owner_assigned_at,
			COALESCE(uf.owner_assigned_by, ''),
			uf.feedback_sla_due_at,
			COALESCE(uf.owner_assignment_note, ''),
			COALESCE(tm.member_type, ''),
			COALESCE(tm.user_id, ''),
			COALESCE(tm.email, ''),
			COALESCE(tm.role, ''),
			COALESCE(` + feedbackAccountKeySQL("uf") + `, ''),
			COALESCE(` + feedbackAccountDisplaySQL("uf") + `, ''),
			` + feedbackAccountSourceSQL("uf") + `
		FROM user_feedback uf
		LEFT JOIN tenant_workflow_states ws
		  ON ws.tenant_id = uf.tenant_id AND ws.id = uf.workflow_state_id
		LEFT JOIN tenant_members tm ON tm.id = uf.owner_member_id
		WHERE uf.tenant_id = $1
		  AND uf.deleted_at IS NULL
		  AND COALESCE(ws.category, 'open') <> 'closed'
		  AND (
		       uf.owner_member_id IS NULL
		    OR uf.feedback_sla_due_at IS NULL
		    OR uf.feedback_sla_due_at < $2::timestamptz + INTERVAL '12 hours'
		  )
		ORDER BY
		  CASE
		    WHEN uf.feedback_sla_due_at < $2::timestamptz THEN 0
		    WHEN uf.owner_member_id IS NULL THEN 1
		    WHEN uf.feedback_sla_due_at IS NULL THEN 2
		    WHEN uf.feedback_sla_due_at < $2::timestamptz + INTERVAL '12 hours' THEN 3
		    ELSE 4
		  END,
		  uf.is_urgent DESC,
		  uf.feedback_sla_due_at ASC NULLS LAST,
		  uf.created_at ASC,
		  uf.id ASC
		LIMIT $3`
}

func scanAssignmentEscalation(row assignmentScanner, now time.Time) (AssignmentEscalation, error) {
	var item AssignmentEscalation
	var ownerID sql.NullString  // ptrext:allow scan-target
	var assignedAt sql.NullTime // ptrext:allow scan-target
	var slaDueAt sql.NullTime   // ptrext:allow scan-target
	err := row.Scan(
		&item.FeedbackID,
		&item.Title,
		&item.Source,
		&item.Type,
		&item.IsUrgent,
		&item.CreatedAt,
		&ownerID,
		&assignedAt,
		&item.Assignment.AssignedBy,
		&slaDueAt,
		&item.Assignment.Note,
		&item.Assignment.OwnerMemberType,
		&item.Assignment.OwnerUserID,
		&item.Assignment.OwnerEmail,
		&item.Assignment.OwnerRole,
		&item.Account.AccountKey,
		&item.Account.AccountDisplay,
		&item.Account.Source,
	)
	if err != nil {
		return AssignmentEscalation{}, fmt.Errorf("scan feedback assignment escalation: %w", err)
	}
	item.Assignment = hydrateAssignmentTimes(item.Assignment, item.FeedbackID, ownerID, assignedAt, slaDueAt)
	item.Reasons = assignmentEscalationReasons(item.Assignment, now)
	item.HoursUntilDue = assignmentEscalationHoursUntilDue(item.Assignment.SLADueAt, now)
	item.Priority = assignmentEscalationPriority(item.Reasons)
	return item, nil
}

func assignmentEscalationReasons(assignment Assignment, now time.Time) []string {
	reasons := []string{}
	if assignment.SLADueAt != nil {
		dueAt := assignment.SLADueAt.UTC()
		if dueAt.Before(now) {
			reasons = append(reasons, "overdue")
		} else if dueAt.Before(now.Add(assignmentEscalationDueSoonWindow)) {
			reasons = append(reasons, "due_soon")
		}
	} else {
		reasons = append(reasons, "missing_sla")
	}
	if assignment.OwnerMemberID == nil {
		reasons = append(reasons, "missing_owner")
	}
	return reasons
}

func assignmentEscalationHoursUntilDue(dueAt *time.Time, now time.Time) *int {
	if dueAt == nil {
		return nil
	}
	hours := int(dueAt.UTC().Sub(now).Hours())
	return ptrext.Of(hours)
}

func assignmentEscalationPriority(reasons []string) string {
	for _, reason := range reasons {
		switch reason {
		case "overdue":
			return "critical"
		case "missing_owner", "missing_sla":
			return "high"
		case "due_soon":
			return "medium"
		}
	}
	return "low"
}

func hydrateAssignmentTimes(
	assignment Assignment,
	feedbackID int64,
	ownerID sql.NullString,
	assignedAt sql.NullTime,
	slaDueAt sql.NullTime,
) Assignment {
	assignment.FeedbackID = feedbackID
	if ownerID.Valid && ownerID.String != "" {
		assignment.OwnerMemberID = ptrext.Of(ownerID.String)
	}
	if assignedAt.Valid {
		assignment.AssignedAt = ptrext.Of(assignedAt.Time)
	}
	if slaDueAt.Valid {
		assignment.SLADueAt = ptrext.Of(slaDueAt.Time)
	}
	return assignment
}

func (r *FeedbackRepo) AssignmentForUpdate(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	feedbackID int64,
) (Assignment, error) {
	row := tx.QueryRow(ctx, assignmentSelectQuery()+`
		WHERE uf.tenant_id = $1 AND uf.id = $2
		FOR UPDATE OF uf`,
		strings.TrimSpace(tenantID),
		feedbackID,
	)
	return scanAssignment(row)
}

func (r *FeedbackRepo) AssignFeedbackTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	feedbackID int64,
	input AssignmentInput,
) (Assignment, error) {
	ownerValue := sql.NullString{}
	if input.OwnerMemberID != nil {
		ownerValue = sql.NullString{String: ptrext.Indirect(input.OwnerMemberID), Valid: true}
	}
	_, err := tx.Exec(ctx, `
		UPDATE user_feedback
		SET owner_member_id = CASE WHEN $3 THEN NULLIF($4, '')::uuid ELSE owner_member_id END,
		    owner_assigned_at = CASE
		        WHEN $3 AND NULLIF($4, '') IS NOT NULL THEN NOW()
		        WHEN $3 THEN NULL
		        ELSE owner_assigned_at
		    END,
		    owner_assigned_by = CASE WHEN $3 THEN $8 ELSE owner_assigned_by END,
		    feedback_sla_due_at = CASE WHEN $5 THEN $6 ELSE feedback_sla_due_at END,
		    owner_assignment_note = $7
		WHERE tenant_id = $1 AND id = $2`,
		strings.TrimSpace(tenantID),
		feedbackID,
		input.OwnerMemberIDSet,
		ownerValue,
		input.SLADueAtSet,
		input.SLADueAt,
		input.Note,
		input.ActorID,
	)
	if err != nil {
		return Assignment{}, fmt.Errorf("assign feedback: %w", err)
	}
	return r.AssignmentForUpdate(ctx, tx, tenantID, feedbackID)
}

func (r *FeedbackRepo) ValidateAssignmentOwner(
	ctx context.Context,
	tenantID string,
	ownerMemberID string,
) error {
	if _, err := uuid.Parse(strings.TrimSpace(ownerMemberID)); err != nil {
		return fmt.Errorf("%w: invalid owner id", ErrAssignmentOwnerNotFound)
	}
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM tenant_members
			WHERE tenant_id = $1
			  AND id = $2::uuid
			  AND member_type <> 'invite'
			  AND role IN ('admin', 'member')
		)`,
		strings.TrimSpace(tenantID),
		strings.TrimSpace(ownerMemberID),
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("validate feedback assignment owner: %w", err)
	}
	if !exists {
		return ErrAssignmentOwnerNotFound
	}
	return nil
}

func (r *FeedbackRepo) AssignmentCandidates(
	ctx context.Context,
	tenantID string,
	feedbackIDs []int64,
) ([]AssignmentCandidate, error) {
	ids := uniquePositiveIDs(feedbackIDs, 100)
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, assignmentCandidatesQuery(), strings.TrimSpace(tenantID), ids)
	if err != nil {
		return nil, fmt.Errorf("feedback assignment candidates: %w", err)
	}
	defer rows.Close()
	var out []AssignmentCandidate
	for rows.Next() {
		row, scanErr := scanAssignmentCandidate(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feedback assignment candidates rows: %w", err)
	}
	return out, nil
}

func assignmentCandidatesQuery() string {
	return `
		SELECT uf.id,
		       uf.created_at,
		       COALESCE(uf.source, ''),
		       COALESCE(uf.type, ''),
		       uf.is_urgent,
		       COALESCE(uf.enrichment_status, ''),
		       uf.enrichment_attempts,
		       uf.enrichment_next_retry_at,
		       COALESCE(ws.category, ''),
		       ` + feedbackStableIdentityPredicate() + `,
		       uf.owner_member_id::text,
		       uf.owner_assigned_at,
		       COALESCE(uf.owner_assigned_by, ''),
		       uf.feedback_sla_due_at,
		       COALESCE(uf.owner_assignment_note, ''),
		       COALESCE(tm.member_type, ''),
		       COALESCE(tm.user_id, ''),
		       COALESCE(tm.email, ''),
		       COALESCE(tm.role, '')
		FROM user_feedback uf
		LEFT JOIN tenant_workflow_states ws
		  ON ws.tenant_id = uf.tenant_id AND ws.id = uf.workflow_state_id
		LEFT JOIN tenant_members tm ON tm.id = uf.owner_member_id
		WHERE uf.tenant_id = $1
		  AND uf.id = ANY($2)
		  AND uf.deleted_at IS NULL
		ORDER BY array_position($2::bigint[], uf.id), uf.id`
}

func scanAssignmentCandidate(row assignmentScanner) (AssignmentCandidate, error) {
	var item AssignmentCandidate
	var ownerID sql.NullString  // ptrext:allow scan-target
	var assignedAt sql.NullTime // ptrext:allow scan-target
	var nextRetry sql.NullTime  // ptrext:allow scan-target
	var slaDueAt sql.NullTime   // ptrext:allow scan-target
	err := row.Scan(
		&item.ID,
		&item.CreatedAt,
		&item.Source,
		&item.Type,
		&item.IsUrgent,
		&item.EnrichmentStatus,
		&item.EnrichmentAttempts,
		&nextRetry,
		&item.WorkflowCategory,
		&item.HasStableIdentity,
		&ownerID,
		&assignedAt,
		&item.Assignment.AssignedBy,
		&slaDueAt,
		&item.Assignment.Note,
		&item.Assignment.OwnerMemberType,
		&item.Assignment.OwnerUserID,
		&item.Assignment.OwnerEmail,
		&item.Assignment.OwnerRole,
	)
	if err != nil {
		return AssignmentCandidate{}, fmt.Errorf("scan feedback assignment candidate: %w", err)
	}
	if nextRetry.Valid {
		item.EnrichmentNextRetryAt = ptrext.Of(nextRetry.Time)
	}
	item.Assignment = hydrateAssignmentTimes(item.Assignment, item.ID, ownerID, assignedAt, slaDueAt)
	return item, nil
}

func assignmentSelectQuery() string {
	return `
		SELECT uf.id,
		       uf.owner_member_id::text,
		       uf.owner_assigned_at,
		       COALESCE(uf.owner_assigned_by, ''),
		       uf.feedback_sla_due_at,
		       COALESCE(uf.owner_assignment_note, ''),
		       COALESCE(tm.member_type, ''),
		       COALESCE(tm.user_id, ''),
		       COALESCE(tm.email, ''),
		       COALESCE(tm.role, '')
		FROM user_feedback uf
		LEFT JOIN tenant_members tm ON tm.id = uf.owner_member_id`
}

type assignmentScanner interface {
	Scan(dest ...any) error
}

func scanAssignment(row assignmentScanner) (Assignment, error) {
	var item Assignment
	var ownerID sql.NullString  // ptrext:allow scan-target
	var assignedAt sql.NullTime // ptrext:allow scan-target
	var slaDueAt sql.NullTime   // ptrext:allow scan-target
	err := row.Scan(
		&item.FeedbackID,
		&ownerID,
		&assignedAt,
		&item.AssignedBy,
		&slaDueAt,
		&item.Note,
		&item.OwnerMemberType,
		&item.OwnerUserID,
		&item.OwnerEmail,
		&item.OwnerRole,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Assignment{}, ErrFeedbackNotFound
	}
	if err != nil {
		return Assignment{}, fmt.Errorf("scan feedback assignment: %w", err)
	}
	item = hydrateAssignmentTimes(item, item.FeedbackID, ownerID, assignedAt, slaDueAt)
	return item, nil
}
