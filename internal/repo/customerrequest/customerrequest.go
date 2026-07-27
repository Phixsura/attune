// SPDX-License-Identifier: Apache-2.0

// Package customerrequest owns the Customer Request persistence model.
package customerrequest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/pgxutil"
)

type Status string

const (
	StatusOpen       Status = "open"
	StatusPlanned    Status = "planned"
	StatusInProgress Status = "in_progress"
	StatusShipped    Status = "shipped"
	StatusCancelled  Status = "cancelled"
)

type Priority string

const (
	PriorityNone   Priority = "none"
	PriorityLow    Priority = "low"
	PriorityMedium Priority = "medium"
	PriorityHigh   Priority = "high"
	PriorityUrgent Priority = "urgent"
)

type Importance string

const (
	ImportanceNormal    Importance = "normal"
	ImportanceImportant Importance = "important"
	ImportanceCritical  Importance = "critical"
)

type Visibility string

const (
	VisibilityActive   Visibility = "active"
	VisibilityMerged   Visibility = "merged"
	VisibilityArchived Visibility = "archived"
	VisibilityAll      Visibility = "all"
)

type Sort string

const (
	SortUpdatedAt               Sort = "updated_at"
	SortCustomerCount           Sort = "customer_count"
	SortSupportingFeedbackCount Sort = "supporting_feedback_count"
	SortLatestFeedbackAt        Sort = "latest_feedback_at"
	SortPriority                Sort = "priority"
	SortRevenueImpact           Sort = "revenue_impact"
	SortDecisionScore           Sort = "decision_score"
	SortDeliveryHealth          Sort = "delivery_health"
)

type Direction string

const (
	DirectionAsc  Direction = "asc"
	DirectionDesc Direction = "desc"
)

type IssueSyncState string

const (
	IssueSyncStateManual  IssueSyncState = "manual"
	IssueSyncStatePending IssueSyncState = "pending"
	IssueSyncStateSynced  IssueSyncState = "synced"
	IssueSyncStateStale   IssueSyncState = "stale"
	IssueSyncStateFailed  IssueSyncState = "failed"
)

type DeliveryHealth string

const (
	DeliveryHealthNoLinks DeliveryHealth = "no_links"
	DeliveryHealthManual  DeliveryHealth = "manual"
	DeliveryHealthPending DeliveryHealth = "pending"
	DeliveryHealthSynced  DeliveryHealth = "synced"
	DeliveryHealthStale   DeliveryHealth = "stale"
	DeliveryHealthFailed  DeliveryHealth = "failed"
)

var (
	ErrNotFound         = errors.New("customer request not found")
	ErrConflict         = errors.New("customer request conflict")
	ErrInvalidInput     = errors.New("customer request invalid input")
	ErrFeedbackNotFound = errors.New("feedback not found")
	ErrLinkNotFound     = errors.New("customer request link not found")
	ErrOwnerNotFound    = errors.New("customer request owner not found")
)

type Owner struct {
	ID         uuid.UUID
	MemberType string
	UserID     string
	Email      string
	Role       string
}

type Summary struct {
	ID                       uuid.UUID
	TenantID                 string
	DisplayNumber            int64
	DisplayID                string
	Title                    string
	Description              string
	Status                   Status
	Priority                 Priority
	OwnerMemberID            *uuid.UUID
	Owner                    *Owner
	CreatedBy                string
	UpdatedBy                string
	MergedIntoRequestID      *uuid.UUID
	CreatedAt                time.Time
	UpdatedAt                time.Time
	ArchivedAt               *time.Time
	SupportingFeedbackCount  int
	CustomerCount            int
	AccountCount             int
	LinkedIssueCount         int
	VoteCount                int
	DuplicateRequestCount    int
	HiddenFeedbackCount      int
	RevenueImpactCents       int64
	RevenueCurrency          string
	DecisionScore            int
	DecisionScoreExplanation string
	DeliveryHealth           DeliveryHealth
	DeliveryHealthRank       int
	SyncedIssueCount         int
	StaleIssueCount          int
	FailedIssueCount         int
	PendingIssueCount        int
	ManualIssueCount         int
	FirstFeedbackAt          *time.Time
	LatestFeedbackAt         *time.Time
}

type ScoringSettings struct {
	TenantID             string
	PriorityNoneWeight   int
	PriorityLowWeight    int
	PriorityMediumWeight int
	PriorityHighWeight   int
	PriorityUrgentWeight int
	FeedbackWeight       int
	FeedbackCap          int
	CustomerWeight       int
	CustomerCap          int
	AccountWeight        int
	AccountCap           int
	VoteWeight           int
	VoteCap              int
	RevenueCentsPerPoint int64
	RevenueCap           int
	UpdatedBy            string
	UpdatedAt            time.Time
}

type ScoringSettingsInput struct {
	TenantID             string
	PriorityNoneWeight   int
	PriorityLowWeight    int
	PriorityMediumWeight int
	PriorityHighWeight   int
	PriorityUrgentWeight int
	FeedbackWeight       int
	FeedbackCap          int
	CustomerWeight       int
	CustomerCap          int
	AccountWeight        int
	AccountCap           int
	VoteWeight           int
	VoteCap              int
	RevenueCentsPerPoint int64
	RevenueCap           int
	ActorID              string
}

type FeedbackEvidence struct {
	FeedbackID     int64
	Content        string
	Source         string
	Type           string
	UserID         string
	SubjectDisplay string
	EnrichedTitle  string
	Importance     Importance
	Note           string
	LinkedBy       string
	LinkedAt       time.Time
	CreatedAt      time.Time
}

type IssueLink struct {
	ID                     uuid.UUID
	Provider               string
	ExternalKey            string
	ExternalURL            string
	Title                  string
	Status                 string
	CreatedBy              string
	CreatedAt              time.Time
	UpdatedAt              time.Time
	LastSyncedAt           *time.Time
	SyncState              IssueSyncState
	ExternalStatusCategory string
	ExternalAssignee       string
	ExternalUpdatedAt      *time.Time
	SyncError              string
}

type AccountProfile struct {
	AccountKey      string
	AccountDisplay  string
	RevenueCents    int64
	RevenueCurrency string
	Tier            string
	SizeSegment     string
	LifecycleStatus string
	CRMProvider     string
	CRMExternalID   string
	Source          string
	UpdatedAt       time.Time
}

type CustomerLink struct {
	ID             uuid.UUID
	SubjectKey     string
	SubjectHash    string
	SubjectDisplay string
	AccountKey     string
	AccountDisplay string
	Note           string
	CreatedBy      string
	CreatedAt      time.Time
	AccountProfile *AccountProfile
}

type Vote struct {
	ID             uuid.UUID
	SubjectKey     string
	SubjectHash    string
	SubjectDisplay string
	AccountKey     string
	AccountDisplay string
	Weight         int
	Note           string
	CreatedBy      string
	CreatedAt      time.Time
	AccountProfile *AccountProfile
}

type Note struct {
	ID        uuid.UUID
	Body      string
	CreatedBy string
	CreatedAt time.Time
}

type Duplicate struct {
	ID        uuid.UUID
	DisplayID string
	Title     string
	MergedAt  time.Time
}

type Detail struct {
	Summary         Summary
	Feedback        []FeedbackEvidence
	IssueLinks      []IssueLink
	CustomerLinks   []CustomerLink
	Votes           []Vote
	Notes           []Note
	Duplicates      []Duplicate
	AccountProfiles []AccountProfile
}

type ListFilter struct {
	TenantID      string
	Query         string
	Statuses      []Status
	Priorities    []Priority
	OwnerMemberID *uuid.UUID
	Visibility    Visibility
	Sort          Sort
	Direction     Direction
	Limit         int
	Cursor        string
	FeedbackID    int64
	CohortID      *string
	EvidenceLimit int
}

type ListResult struct {
	Items      []Summary
	NextCursor string
}

type CreateInput struct {
	TenantID      string
	Title         string
	Description   string
	Status        Status
	Priority      Priority
	OwnerMemberID *uuid.UUID
	ActorID       string
}

type UpdateInput struct {
	TenantID         string
	ID               uuid.UUID
	Title            *string
	Description      *string
	Status           *Status
	Priority         *Priority
	OwnerMemberIDSet bool
	OwnerMemberID    *uuid.UUID
	ActorID          string
}

type LinkFeedbackInput struct {
	TenantID   string
	RequestID  uuid.UUID
	FeedbackID int64
	Importance Importance
	Note       string
	ActorID    string
}

type CustomerLinkInput struct {
	TenantID       string
	RequestID      uuid.UUID
	SubjectKey     string
	SubjectHash    string
	SubjectDisplay string
	AccountKey     string
	AccountDisplay string
	Note           string
	ActorID        string
	AccountProfile AccountProfileInput
}

type VoteInput struct {
	TenantID       string
	RequestID      uuid.UUID
	SubjectKey     string
	SubjectHash    string
	SubjectDisplay string
	AccountKey     string
	AccountDisplay string
	Weight         int
	Note           string
	ActorID        string
	AccountProfile AccountProfileInput
}

type NoteInput struct {
	TenantID  string
	RequestID uuid.UUID
	Body      string
	ActorID   string
}

type IssueLinkInput struct {
	TenantID    string
	RequestID   uuid.UUID
	Provider    string
	ExternalKey string
	ExternalURL string
	Title       string
	Status      string
	ActorID     string
}

type AccountProfileInput struct {
	AccountKey      string
	AccountDisplay  string
	RevenueCents    int64
	RevenueCurrency string
	Tier            string
	SizeSegment     string
	LifecycleStatus string
	CRMProvider     string
	CRMExternalID   string
	Source          string
	ActorID         string
}

type IssueSyncInput struct {
	TenantID               string
	RequestID              uuid.UUID
	IssueLinkID            uuid.UUID
	SyncState              IssueSyncState
	Status                 string
	ExternalStatusCategory string
	ExternalAssignee       string
	ExternalUpdatedAt      *time.Time
	SyncError              string
	ActorID                string
}

type MergeResult struct {
	SourceID                      uuid.UUID
	TargetID                      uuid.UUID
	SourceDisplayID               string
	TargetDisplayID               string
	MovedFeedbackCount            int
	MovedCustomerCount            int
	MovedVoteCount                int
	MovedNoteCount                int
	MovedIssueCount               int
	SkippedDuplicateFeedbackCount int
	SkippedDuplicateCustomerCount int
	SkippedDuplicateVoteCount     int
	SkippedDuplicateIssueCount    int
	AlreadyMergedIntoTarget       bool
}

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

func (r *Repo) Begin(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func DefaultScoringSettings(tenantID string) ScoringSettings {
	return ScoringSettings{
		TenantID:             tenantID,
		PriorityNoneWeight:   0,
		PriorityLowWeight:    20,
		PriorityMediumWeight: 40,
		PriorityHighWeight:   60,
		PriorityUrgentWeight: 80,
		FeedbackWeight:       2,
		FeedbackCap:          80,
		CustomerWeight:       5,
		CustomerCap:          100,
		AccountWeight:        8,
		AccountCap:           120,
		VoteWeight:           4,
		VoteCap:              80,
		RevenueCentsPerPoint: 100000,
		RevenueCap:           100,
		UpdatedBy:            "",
	}
}

func (r *Repo) GetScoringSettings(ctx context.Context, tenantID string) (ScoringSettings, error) {
	out, err := scanScoringSettings(r.pool.QueryRow(ctx, scoringSettingsSelectSQL()+`
		WHERE tenant_id = $1`, tenantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultScoringSettings(tenantID), nil
	}
	if err != nil {
		return ScoringSettings{}, fmt.Errorf("get customer request scoring settings: %w", err)
	}
	return out, nil
}

func (r *Repo) UpsertScoringSettingsTx(
	ctx context.Context,
	tx pgx.Tx,
	in ScoringSettingsInput,
) (ScoringSettings, error) {
	out, err := scanScoringSettings(tx.QueryRow(ctx, `
		INSERT INTO customer_request_scoring_settings (
			tenant_id,
			priority_none_weight,
			priority_low_weight,
			priority_medium_weight,
			priority_high_weight,
			priority_urgent_weight,
			feedback_weight,
			feedback_cap,
			customer_weight,
			customer_cap,
			account_weight,
			account_cap,
			vote_weight,
			vote_cap,
			revenue_cents_per_point,
			revenue_cap,
			updated_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17
		)
		ON CONFLICT (tenant_id) DO UPDATE SET
			priority_none_weight = EXCLUDED.priority_none_weight,
			priority_low_weight = EXCLUDED.priority_low_weight,
			priority_medium_weight = EXCLUDED.priority_medium_weight,
			priority_high_weight = EXCLUDED.priority_high_weight,
			priority_urgent_weight = EXCLUDED.priority_urgent_weight,
			feedback_weight = EXCLUDED.feedback_weight,
			feedback_cap = EXCLUDED.feedback_cap,
			customer_weight = EXCLUDED.customer_weight,
			customer_cap = EXCLUDED.customer_cap,
			account_weight = EXCLUDED.account_weight,
			account_cap = EXCLUDED.account_cap,
			vote_weight = EXCLUDED.vote_weight,
			vote_cap = EXCLUDED.vote_cap,
			revenue_cents_per_point = EXCLUDED.revenue_cents_per_point,
			revenue_cap = EXCLUDED.revenue_cap,
			updated_by = EXCLUDED.updated_by
		RETURNING
			tenant_id,
			priority_none_weight,
			priority_low_weight,
			priority_medium_weight,
			priority_high_weight,
			priority_urgent_weight,
			feedback_weight,
			feedback_cap,
			customer_weight,
			customer_cap,
			account_weight,
			account_cap,
			vote_weight,
			vote_cap,
			revenue_cents_per_point,
			revenue_cap,
			updated_by,
			updated_at`,
		in.TenantID,
		in.PriorityNoneWeight,
		in.PriorityLowWeight,
		in.PriorityMediumWeight,
		in.PriorityHighWeight,
		in.PriorityUrgentWeight,
		in.FeedbackWeight,
		in.FeedbackCap,
		in.CustomerWeight,
		in.CustomerCap,
		in.AccountWeight,
		in.AccountCap,
		in.VoteWeight,
		in.VoteCap,
		in.RevenueCentsPerPoint,
		in.RevenueCap,
		in.ActorID,
	))
	if err != nil {
		return ScoringSettings{}, mapWriteError(err, "upsert customer request scoring settings")
	}
	return out, nil
}

func (r *Repo) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := 0
	if strings.TrimSpace(filter.Cursor) != "" {
		parsed, err := strconv.Atoi(strings.TrimSpace(filter.Cursor))
		if err != nil || parsed < 0 {
			return ListResult{}, ErrInvalidInput
		}
		offset = parsed
	}

	query, args := buildListQuery(filter, limit+1, offset)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return ListResult{}, fmt.Errorf("list customer requests: %w", err)
	}
	defer rows.Close()

	items := make([]Summary, 0, limit)
	for rows.Next() {
		var item Summary
		if err := scanSummary(rows, &item); err != nil { // ptrext:allow scan-target
			return ListResult{}, fmt.Errorf("scan customer request: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return ListResult{}, err
	}

	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = strconv.Itoa(offset + limit)
	}
	return ListResult{Items: items, NextCursor: next}, nil
}

func buildListQuery(filter ListFilter, limit, offset int) (string, []any) {
	args := []any{filter.TenantID}
	clauses := []string{"cr.tenant_id = $1"}
	clauses = appendVisibilityClause(clauses, filter.Visibility)
	if q := strings.TrimSpace(filter.Query); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		clauses = append(clauses, fmt.Sprintf("(LOWER(cr.title) LIKE $%d OR LOWER(cr.display_id) LIKE $%d)", len(args), len(args)))
	}
	if len(filter.Statuses) > 0 {
		values := make([]string, 0, len(filter.Statuses))
		for _, status := range filter.Statuses {
			values = append(values, string(status))
		}
		args = append(args, values)
		clauses = append(clauses, fmt.Sprintf("cr.status = ANY($%d)", len(args)))
	}
	if len(filter.Priorities) > 0 {
		values := make([]string, 0, len(filter.Priorities))
		for _, priority := range filter.Priorities {
			values = append(values, string(priority))
		}
		args = append(args, values)
		clauses = append(clauses, fmt.Sprintf("cr.priority = ANY($%d)", len(args)))
	}
	if filter.OwnerMemberID != nil {
		args = append(args, ptrext.Indirect(filter.OwnerMemberID))
		clauses = append(clauses, fmt.Sprintf("cr.owner_member_id = $%d", len(args)))
	}
	if filter.FeedbackID > 0 {
		args = append(args, filter.FeedbackID)
		clauses = append(clauses, fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM customer_request_feedback_links fl
			WHERE fl.tenant_id = cr.tenant_id
			  AND fl.request_id = cr.id
			  AND fl.feedback_id = $%d
		)`, len(args)))
	}
	if filter.CohortID != nil {
		args = append(args, ptrext.Indirect(filter.CohortID))
		clauses = append(clauses, fmt.Sprintf(`EXISTS (
			SELECT 1
			FROM customer_request_feedback_links fl
			JOIN user_feedback f ON f.tenant_id = fl.tenant_id AND f.id = fl.feedback_id
			JOIN cohort_memberships cm
			  ON cm.tenant_id = f.tenant_id
			  AND cm.external_user_id = f.subject_key
			  AND cm.cohort_id = $%d::uuid
			  AND cm.left_at IS NULL
			WHERE fl.tenant_id = cr.tenant_id
			  AND fl.request_id = cr.id
			  AND f.subject_key <> ''
		)`, len(args)))
	}
	args = append(args, limit, offset)
	return summarySelectSQL() + `
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY ` + orderByClause(filter.Sort, filter.Direction) + `
		LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args)), args
}

func appendVisibilityClause(clauses []string, visibility Visibility) []string {
	switch visibility {
	case VisibilityAll:
		return clauses
	case VisibilityMerged:
		return append(clauses, "cr.merged_into_request_id IS NOT NULL")
	case VisibilityArchived:
		return append(clauses, "cr.archived_at IS NOT NULL AND cr.merged_into_request_id IS NULL")
	default:
		return append(clauses, "cr.archived_at IS NULL AND cr.merged_into_request_id IS NULL")
	}
}

func orderByClause(sort Sort, direction Direction) string {
	dir := "DESC"
	if direction == DirectionAsc {
		dir = "ASC"
	}
	expr := "cr.updated_at"
	switch sort {
	case SortCustomerCount:
		expr = "fc.customer_count"
	case SortSupportingFeedbackCount:
		expr = "fc.supporting_feedback_count"
	case SortLatestFeedbackAt:
		expr = "fc.latest_feedback_at"
	case SortPriority:
		expr = "CASE cr.priority WHEN 'urgent' THEN 4 WHEN 'high' THEN 3 WHEN 'medium' THEN 2 WHEN 'low' THEN 1 ELSE 0 END"
	case SortRevenueImpact:
		expr = "revenue_impact_cents"
	case SortDecisionScore:
		expr = "decision_score"
	case SortDeliveryHealth:
		expr = "delivery_health_rank"
	}
	return expr + " " + dir + " NULLS LAST, cr.id " + dir
}

func (r *Repo) GetDetail(ctx context.Context, tenantID string, id uuid.UUID, evidenceLimit int) (*Detail, error) {
	return loadDetail(ctx, r.pool, tenantID, id, evidenceLimit)
}

func (r *Repo) GetDetailTx(ctx context.Context, tx pgx.Tx, tenantID string, id uuid.UUID, evidenceLimit int) (*Detail, error) {
	return loadDetail(ctx, tx, tenantID, id, evidenceLimit)
}

func loadDetail(ctx context.Context, db queryer, tenantID string, id uuid.UUID, evidenceLimit int) (*Detail, error) {
	if evidenceLimit <= 0 {
		evidenceLimit = 50
	}
	if evidenceLimit > 100 {
		evidenceLimit = 100
	}
	summary, err := loadSummary(ctx, db, tenantID, id)
	if err != nil {
		return nil, err
	}
	feedback, err := listEvidence(ctx, db, tenantID, id, evidenceLimit)
	if err != nil {
		return nil, err
	}
	issues, err := listIssueLinks(ctx, db, tenantID, id)
	if err != nil {
		return nil, err
	}
	customers, err := listCustomerLinks(ctx, db, tenantID, id)
	if err != nil {
		return nil, err
	}
	votes, err := listVotes(ctx, db, tenantID, id)
	if err != nil {
		return nil, err
	}
	notes, err := listNotes(ctx, db, tenantID, id)
	if err != nil {
		return nil, err
	}
	duplicates, err := listDuplicates(ctx, db, tenantID, id)
	if err != nil {
		return nil, err
	}
	accountProfiles := collectAccountProfiles(customers, votes)
	return ptrext.Of(Detail{
		Summary:         ptrext.Indirect(summary),
		Feedback:        feedback,
		IssueLinks:      issues,
		CustomerLinks:   customers,
		Votes:           votes,
		Notes:           notes,
		Duplicates:      duplicates,
		AccountProfiles: accountProfiles,
	}), nil
}

func loadSummary(ctx context.Context, db queryer, tenantID string, id uuid.UUID) (*Summary, error) {
	var out Summary
	err := scanSummary(db.QueryRow(ctx, summarySelectSQL()+`
		WHERE cr.tenant_id = $1 AND cr.id = $2`, tenantID, id), &out) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get customer request: %w", err)
	}
	return ptrext.Of(out), nil
}

func (r *Repo) CreateTx(ctx context.Context, tx pgx.Tx, in CreateInput) (*Summary, error) {
	number, displayID, err := allocateDisplayID(ctx, tx, in.TenantID)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	err = tx.QueryRow(
		ctx, `
		INSERT INTO customer_requests (
			tenant_id, display_number, display_id, title, description, status, priority,
			owner_member_id, created_by, updated_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
		RETURNING id`,
		in.TenantID, number, displayID, in.Title, in.Description, in.Status, in.Priority,
		in.OwnerMemberID, in.ActorID,
	).Scan(&id)
	if err != nil {
		return nil, mapWriteError(err, "create customer request")
	}
	return loadSummary(ctx, tx, in.TenantID, id)
}

func allocateDisplayID(ctx context.Context, tx pgx.Tx, tenantID string) (int64, string, error) {
	if _, err := tx.Exec(ctx, `
		INSERT INTO customer_request_counters (tenant_id, next_number)
		VALUES ($1, 1)
		ON CONFLICT (tenant_id) DO NOTHING`, tenantID); err != nil {
		return 0, "", fmt.Errorf("ensure customer request counter: %w", err)
	}
	var next int64
	if err := tx.QueryRow(ctx, `
		SELECT next_number
		FROM customer_request_counters
		WHERE tenant_id = $1
		FOR UPDATE`, tenantID).Scan(&next); err != nil {
		return 0, "", fmt.Errorf("lock customer request counter: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE customer_request_counters
		SET next_number = next_number + 1
		WHERE tenant_id = $1`, tenantID); err != nil {
		return 0, "", fmt.Errorf("advance customer request counter: %w", err)
	}
	return next, fmt.Sprintf("CR-%d", next), nil
}

func (r *Repo) UpdateTx(ctx context.Context, tx pgx.Tx, in UpdateInput) (*Summary, *Summary, error) {
	before, err := loadSummary(ctx, tx, in.TenantID, in.ID)
	if err != nil {
		return nil, nil, err
	}
	if before.ArchivedAt != nil || before.MergedIntoRequestID != nil {
		return nil, nil, ErrConflict
	}
	title := before.Title
	if in.Title != nil {
		title = ptrext.Indirect(in.Title)
	}
	description := before.Description
	if in.Description != nil {
		description = ptrext.Indirect(in.Description)
	}
	status := before.Status
	if in.Status != nil {
		status = ptrext.Indirect(in.Status)
	}
	priority := before.Priority
	if in.Priority != nil {
		priority = ptrext.Indirect(in.Priority)
	}
	owner := before.OwnerMemberID
	if in.OwnerMemberIDSet {
		owner = in.OwnerMemberID
	}
	if _, err := tx.Exec(
		ctx, `
		UPDATE customer_requests
		SET title = $3,
		    description = $4,
		    status = $5,
		    priority = $6,
		    owner_member_id = $7,
		    updated_by = $8
		WHERE tenant_id = $1 AND id = $2`,
		in.TenantID, in.ID, title, description, status, priority, owner, in.ActorID,
	); err != nil {
		return nil, nil, mapWriteError(err, "update customer request")
	}
	after, err := loadSummary(ctx, tx, in.TenantID, in.ID)
	if err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

func (r *Repo) LinkFeedbackTx(ctx context.Context, tx pgx.Tx, in LinkFeedbackInput) error {
	err := tx.QueryRow(
		ctx, `
		INSERT INTO customer_request_feedback_links (
			tenant_id, request_id, feedback_id, importance, note, created_by
		)
		SELECT $1, cr.id, uf.id, $4, $5, $6
		FROM customer_requests cr
		JOIN user_feedback uf
		  ON uf.tenant_id = cr.tenant_id
		 AND uf.id = $3
		 AND uf.deleted_at IS NULL
		WHERE cr.tenant_id = $1
		  AND cr.id = $2
		  AND cr.archived_at IS NULL
		ON CONFLICT (request_id, feedback_id)
		DO UPDATE SET importance = EXCLUDED.importance, note = EXCLUDED.note
		RETURNING feedback_id`,
		in.TenantID, in.RequestID, in.FeedbackID, in.Importance, in.Note, in.ActorID,
	).Scan(&in.FeedbackID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrFeedbackNotFound
	}
	if err != nil {
		return mapWriteError(err, "link feedback")
	}
	return touchRequestTx(ctx, tx, in.TenantID, in.RequestID, in.ActorID)
}

func (r *Repo) UnlinkFeedbackTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID, feedbackID int64, actorID string) error {
	tag, err := tx.Exec(
		ctx, `
		DELETE FROM customer_request_feedback_links
		WHERE tenant_id = $1 AND request_id = $2 AND feedback_id = $3`,
		tenantID, requestID, feedbackID,
	)
	if err != nil {
		return fmt.Errorf("unlink feedback: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrLinkNotFound
	}
	return touchRequestTx(ctx, tx, tenantID, requestID, actorID)
}

func (r *Repo) LinkCustomerTx(ctx context.Context, tx pgx.Tx, in CustomerLinkInput) (*CustomerLink, error) {
	if err := upsertAccountProfileTx(ctx, tx, in.TenantID, in.AccountProfile); err != nil {
		return nil, err
	}
	var id uuid.UUID
	err := tx.QueryRow(
		ctx, `
		INSERT INTO customer_request_customer_links (
			tenant_id, request_id, subject_key, subject_hash, subject_display,
			account_key, account_display, note, created_by
		)
		SELECT $1, cr.id, $3, $4, $5, $6, $7, $8, $9
		FROM customer_requests cr
		WHERE cr.tenant_id = $1
		  AND cr.id = $2
		  AND cr.archived_at IS NULL
		ON CONFLICT (tenant_id, request_id, subject_hash, subject_key, account_key)
		DO UPDATE SET
		  subject_display = EXCLUDED.subject_display,
		  account_display = EXCLUDED.account_display,
		  note = EXCLUDED.note
		RETURNING id`,
		in.TenantID, in.RequestID, in.SubjectKey, in.SubjectHash, in.SubjectDisplay,
		in.AccountKey, in.AccountDisplay, in.Note, in.ActorID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := loadSummary(ctx, tx, in.TenantID, in.RequestID); getErr != nil {
			return nil, getErr
		}
		return nil, ErrConflict
	}
	if err != nil {
		return nil, mapWriteError(err, "link customer")
	}
	if err := touchRequestTx(ctx, tx, in.TenantID, in.RequestID, in.ActorID); err != nil {
		return nil, err
	}
	return getCustomerLink(ctx, tx, in.TenantID, in.RequestID, id)
}

func (r *Repo) UnlinkCustomerTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID, linkID uuid.UUID, actorID string) (*CustomerLink, error) {
	link, err := getCustomerLink(ctx, tx, tenantID, requestID, linkID)
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(
		ctx, `
		DELETE FROM customer_request_customer_links
		WHERE tenant_id = $1 AND request_id = $2 AND id = $3`,
		tenantID, requestID, linkID,
	)
	if err != nil {
		return nil, fmt.Errorf("unlink customer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrLinkNotFound
	}
	if err := touchRequestTx(ctx, tx, tenantID, requestID, actorID); err != nil {
		return nil, err
	}
	return link, nil
}

func (r *Repo) AddVoteTx(ctx context.Context, tx pgx.Tx, in VoteInput) (*Vote, error) {
	if err := upsertAccountProfileTx(ctx, tx, in.TenantID, in.AccountProfile); err != nil {
		return nil, err
	}
	var id uuid.UUID
	err := tx.QueryRow(
		ctx, `
		INSERT INTO customer_request_votes (
			tenant_id, request_id, subject_key, subject_hash, subject_display,
			account_key, account_display, weight, note, created_by
		)
		SELECT $1, cr.id, $3, $4, $5, $6, $7, $8, $9, $10
		FROM customer_requests cr
		WHERE cr.tenant_id = $1
		  AND cr.id = $2
		  AND cr.archived_at IS NULL
		ON CONFLICT (tenant_id, request_id, subject_hash, subject_key, account_key)
		DO UPDATE SET
		  subject_display = EXCLUDED.subject_display,
		  account_display = EXCLUDED.account_display,
		  weight = EXCLUDED.weight,
		  note = EXCLUDED.note
		RETURNING id`,
		in.TenantID, in.RequestID, in.SubjectKey, in.SubjectHash, in.SubjectDisplay,
		in.AccountKey, in.AccountDisplay, in.Weight, in.Note, in.ActorID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := loadSummary(ctx, tx, in.TenantID, in.RequestID); getErr != nil {
			return nil, getErr
		}
		return nil, ErrConflict
	}
	if err != nil {
		return nil, mapWriteError(err, "add vote")
	}
	if err := touchRequestTx(ctx, tx, in.TenantID, in.RequestID, in.ActorID); err != nil {
		return nil, err
	}
	return getVote(ctx, tx, in.TenantID, in.RequestID, id)
}

func (r *Repo) RemoveVoteTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID, voteID uuid.UUID, actorID string) (*Vote, error) {
	vote, err := getVote(ctx, tx, tenantID, requestID, voteID)
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(
		ctx, `
		DELETE FROM customer_request_votes
		WHERE tenant_id = $1 AND request_id = $2 AND id = $3`,
		tenantID, requestID, voteID,
	)
	if err != nil {
		return nil, fmt.Errorf("remove vote: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrLinkNotFound
	}
	if err := touchRequestTx(ctx, tx, tenantID, requestID, actorID); err != nil {
		return nil, err
	}
	return vote, nil
}

func (r *Repo) AddNoteTx(ctx context.Context, tx pgx.Tx, in NoteInput) (*Note, error) {
	var id uuid.UUID
	err := tx.QueryRow(
		ctx, `
		INSERT INTO customer_request_notes (
			tenant_id, request_id, body, created_by
		)
		SELECT $1, cr.id, $3, $4
		FROM customer_requests cr
		WHERE cr.tenant_id = $1
		  AND cr.id = $2
		  AND cr.archived_at IS NULL
		RETURNING id`,
		in.TenantID, in.RequestID, in.Body, in.ActorID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := loadSummary(ctx, tx, in.TenantID, in.RequestID); getErr != nil {
			return nil, getErr
		}
		return nil, ErrConflict
	}
	if err != nil {
		return nil, mapWriteError(err, "add note")
	}
	if err := touchRequestTx(ctx, tx, in.TenantID, in.RequestID, in.ActorID); err != nil {
		return nil, err
	}
	return getNote(ctx, tx, in.TenantID, in.RequestID, id)
}

func (r *Repo) DeleteNoteTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID, noteID uuid.UUID, actorID string) (*Note, error) {
	var note Note
	err := tx.QueryRow(
		ctx, `
		WITH deleted AS (
			DELETE FROM customer_request_notes
			WHERE tenant_id = $1 AND request_id = $2 AND id = $3
			RETURNING id, body, created_by, created_at
		)
		SELECT id, body, created_by, created_at FROM deleted`,
		tenantID, requestID, noteID,
	).Scan(&note.ID, &note.Body, &note.CreatedBy, &note.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("delete note: %w", err)
	}
	if err := touchRequestTx(ctx, tx, tenantID, requestID, actorID); err != nil {
		return nil, err
	}
	return ptrext.Of(note), nil
}

func (r *Repo) LinkIssueTx(ctx context.Context, tx pgx.Tx, in IssueLinkInput) (*IssueLink, error) {
	var id uuid.UUID
	err := tx.QueryRow(
		ctx, `
		INSERT INTO customer_request_issue_links (
			tenant_id, request_id, provider, external_key, external_url, title, status, created_by
		)
		SELECT $1, cr.id, $3, $4, $5, $6, $7, $8
		FROM customer_requests cr
		WHERE cr.tenant_id = $1
		  AND cr.id = $2
		  AND cr.archived_at IS NULL
		ON CONFLICT (tenant_id, request_id, provider, external_key) DO NOTHING
		RETURNING id`,
		in.TenantID, in.RequestID, in.Provider, in.ExternalKey, in.ExternalURL, in.Title, in.Status, in.ActorID,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := loadSummary(ctx, tx, in.TenantID, in.RequestID); getErr != nil {
			return nil, getErr
		}
		return nil, ErrConflict
	}
	if err != nil {
		return nil, mapWriteError(err, "link issue")
	}
	if err := touchRequestTx(ctx, tx, in.TenantID, in.RequestID, in.ActorID); err != nil {
		return nil, err
	}
	return getIssueLink(ctx, tx, in.TenantID, in.RequestID, id)
}

func (r *Repo) UnlinkIssueTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID, issueLinkID uuid.UUID, actorID string) (*IssueLink, error) {
	link, err := getIssueLink(ctx, tx, tenantID, requestID, issueLinkID)
	if err != nil {
		return nil, err
	}
	tag, err := tx.Exec(
		ctx, `
		DELETE FROM customer_request_issue_links
		WHERE tenant_id = $1 AND request_id = $2 AND id = $3`,
		tenantID, requestID, issueLinkID,
	)
	if err != nil {
		return nil, fmt.Errorf("unlink issue: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrLinkNotFound
	}
	if err := touchRequestTx(ctx, tx, tenantID, requestID, actorID); err != nil {
		return nil, err
	}
	return link, nil
}

func (r *Repo) RecordIssueSyncTx(ctx context.Context, tx pgx.Tx, in IssueSyncInput) (*IssueLink, error) {
	tag, err := tx.Exec(
		ctx, `
		UPDATE customer_request_issue_links
		SET status = $4,
		    sync_state = $5,
		    external_status_category = $6,
		    external_assignee = $7,
		    external_updated_at = $8,
		    sync_error = $9,
		    last_synced_at = NOW()
		WHERE tenant_id = $1 AND request_id = $2 AND id = $3`,
		in.TenantID,
		in.RequestID,
		in.IssueLinkID,
		in.Status,
		in.SyncState,
		in.ExternalStatusCategory,
		in.ExternalAssignee,
		in.ExternalUpdatedAt,
		in.SyncError,
	)
	if err != nil {
		return nil, fmt.Errorf("record issue sync: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrLinkNotFound
	}
	if err := touchRequestTx(ctx, tx, in.TenantID, in.RequestID, in.ActorID); err != nil {
		return nil, err
	}
	return getIssueLink(ctx, tx, in.TenantID, in.RequestID, in.IssueLinkID)
}

func (r *Repo) MergeTx(ctx context.Context, tx pgx.Tx, tenantID string, sourceID, targetID uuid.UUID, actorID string) (MergeResult, error) {
	if sourceID == targetID {
		return MergeResult{}, ErrConflict
	}
	source, target, err := lockMergeRequests(ctx, tx, tenantID, sourceID, targetID)
	if err != nil {
		return MergeResult{}, err
	}
	result := MergeResult{
		SourceID:        sourceID,
		TargetID:        targetID,
		SourceDisplayID: source.DisplayID,
		TargetDisplayID: target.DisplayID,
	}
	if source.MergedIntoRequestID != nil {
		if ptrext.Indirect(source.MergedIntoRequestID) == targetID {
			result.AlreadyMergedIntoTarget = true
			return result, nil
		}
		return MergeResult{}, ErrConflict
	}
	if source.ArchivedAt != nil || target.ArchivedAt != nil || target.MergedIntoRequestID != nil {
		return MergeResult{}, ErrConflict
	}

	backlinks, err := copyMergeBacklinks(ctx, tx, tenantID, sourceID, targetID, actorID)
	if err != nil {
		return MergeResult{}, err
	}
	if err := touchRequestTx(ctx, tx, tenantID, targetID, actorID); err != nil {
		return MergeResult{}, err
	}

	if _, err := tx.Exec(
		ctx, `
		UPDATE customer_requests
		SET merged_into_request_id = $3,
		    archived_at = NOW(),
		    updated_by = $4
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, sourceID, targetID, actorID,
	); err != nil {
		return MergeResult{}, fmt.Errorf("mark customer request merged: %w", err)
	}

	result.MovedFeedbackCount = backlinks.MovedFeedback
	result.MovedCustomerCount = backlinks.MovedCustomers
	result.MovedVoteCount = backlinks.MovedVotes
	result.MovedNoteCount = backlinks.MovedNotes
	result.MovedIssueCount = backlinks.MovedIssues
	result.SkippedDuplicateFeedbackCount = backlinks.SourceFeedback - backlinks.MovedFeedback
	result.SkippedDuplicateCustomerCount = backlinks.SourceCustomers - backlinks.MovedCustomers
	result.SkippedDuplicateVoteCount = backlinks.SourceVotes - backlinks.MovedVotes
	result.SkippedDuplicateIssueCount = backlinks.SourceIssues - backlinks.MovedIssues
	return result, nil
}

type mergeBacklinkCounts struct {
	SourceFeedback  int
	MovedFeedback   int
	SourceCustomers int
	MovedCustomers  int
	SourceVotes     int
	MovedVotes      int
	SourceNotes     int
	MovedNotes      int
	SourceIssues    int
	MovedIssues     int
}

func copyMergeBacklinks(ctx context.Context, tx pgx.Tx, tenantID string, sourceID, targetID uuid.UUID, actorID string) (mergeBacklinkCounts, error) {
	var out mergeBacklinkCounts
	var err error
	out.SourceFeedback, err = countFeedbackLinks(ctx, tx, tenantID, sourceID)
	if err != nil {
		return mergeBacklinkCounts{}, err
	}
	out.MovedFeedback, err = copyFeedbackLinks(ctx, tx, tenantID, sourceID, targetID, actorID)
	if err != nil {
		return mergeBacklinkCounts{}, err
	}
	out.SourceCustomers, err = countCustomerLinks(ctx, tx, tenantID, sourceID)
	if err != nil {
		return mergeBacklinkCounts{}, err
	}
	out.MovedCustomers, err = copyCustomerLinks(ctx, tx, tenantID, sourceID, targetID, actorID)
	if err != nil {
		return mergeBacklinkCounts{}, err
	}
	out.SourceVotes, err = countVotes(ctx, tx, tenantID, sourceID)
	if err != nil {
		return mergeBacklinkCounts{}, err
	}
	out.MovedVotes, err = copyVotes(ctx, tx, tenantID, sourceID, targetID, actorID)
	if err != nil {
		return mergeBacklinkCounts{}, err
	}
	out.SourceNotes, err = countNotes(ctx, tx, tenantID, sourceID)
	if err != nil {
		return mergeBacklinkCounts{}, err
	}
	out.MovedNotes, err = copyNotes(ctx, tx, tenantID, sourceID, targetID)
	if err != nil {
		return mergeBacklinkCounts{}, err
	}
	out.SourceIssues, err = countIssueLinks(ctx, tx, tenantID, sourceID)
	if err != nil {
		return mergeBacklinkCounts{}, err
	}
	out.MovedIssues, err = copyIssueLinks(ctx, tx, tenantID, sourceID, targetID, actorID)
	if err != nil {
		return mergeBacklinkCounts{}, err
	}
	return out, nil
}

func (r *Repo) GetOwner(ctx context.Context, tenantID string, ownerID uuid.UUID) (*Owner, error) {
	owner, err := scanOwner(r.pool.QueryRow(ctx, `
		SELECT id::text, member_type, user_id, COALESCE(email, ''), role
		FROM tenant_members
		WHERE tenant_id = $1 AND id = $2`, tenantID, ownerID)) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOwnerNotFound
	}
	return owner, err
}

func lockMergeRequests(ctx context.Context, tx pgx.Tx, tenantID string, sourceID, targetID uuid.UUID) (lockRow, lockRow, error) {
	firstID, secondID := sourceID, targetID
	sourceIsFirst := true
	if firstID.String() > secondID.String() {
		firstID, secondID = secondID, firstID
		sourceIsFirst = false
	}
	first, err := lockRequest(ctx, tx, tenantID, firstID)
	if err != nil {
		return lockRow{}, lockRow{}, err
	}
	second, err := lockRequest(ctx, tx, tenantID, secondID)
	if err != nil {
		return lockRow{}, lockRow{}, err
	}
	if sourceIsFirst {
		return first, second, nil
	}
	return second, first, nil
}

type lockRow struct {
	DisplayID           string
	MergedIntoRequestID *uuid.UUID
	ArchivedAt          *time.Time
}

func lockRequest(ctx context.Context, tx pgx.Tx, tenantID string, id uuid.UUID) (lockRow, error) {
	var row lockRow
	var merged sql.NullString
	err := tx.QueryRow(ctx, `
		SELECT display_id, merged_into_request_id::text, archived_at
		FROM customer_requests
		WHERE tenant_id = $1 AND id = $2
		FOR UPDATE`, tenantID, id).Scan(&row.DisplayID, &merged, &row.ArchivedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return lockRow{}, ErrNotFound
	}
	if err != nil {
		return lockRow{}, fmt.Errorf("lock customer request: %w", err)
	}
	if merged.Valid {
		parsed, parseErr := uuid.Parse(merged.String)
		if parseErr != nil {
			return lockRow{}, parseErr
		}
		row.MergedIntoRequestID = ptrext.Of(parsed)
	}
	return row, nil
}

func countFeedbackLinks(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM customer_request_feedback_links
		WHERE tenant_id = $1 AND request_id = $2`, tenantID, requestID).Scan(&count)
	return count, err
}

func copyFeedbackLinks(ctx context.Context, tx pgx.Tx, tenantID string, sourceID, targetID uuid.UUID, actorID string) (int, error) {
	var count int
	err := tx.QueryRow(
		ctx, `
		WITH moved AS (
			INSERT INTO customer_request_feedback_links (
				tenant_id, request_id, feedback_id, importance, note, created_by, created_at
			)
			SELECT tenant_id, $3, feedback_id, importance, note, $4, NOW()
			FROM customer_request_feedback_links
			WHERE tenant_id = $1 AND request_id = $2
			ON CONFLICT (request_id, feedback_id) DO NOTHING
			RETURNING feedback_id
		)
		SELECT COUNT(*)::int FROM moved`,
		tenantID, sourceID, targetID, actorID,
	).Scan(&count)
	return count, err
}

func countCustomerLinks(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM customer_request_customer_links
		WHERE tenant_id = $1 AND request_id = $2`, tenantID, requestID).Scan(&count)
	return count, err
}

func copyCustomerLinks(ctx context.Context, tx pgx.Tx, tenantID string, sourceID, targetID uuid.UUID, actorID string) (int, error) {
	var count int
	err := tx.QueryRow(
		ctx, `
		WITH moved AS (
			INSERT INTO customer_request_customer_links (
				tenant_id, request_id, subject_key, subject_hash, subject_display,
				account_key, account_display, note, created_by, created_at
			)
			SELECT tenant_id, $3, subject_key, subject_hash, subject_display,
			       account_key, account_display, note, $4, NOW()
			FROM customer_request_customer_links
			WHERE tenant_id = $1 AND request_id = $2
			ON CONFLICT (tenant_id, request_id, subject_hash, subject_key, account_key) DO NOTHING
			RETURNING id
		)
		SELECT COUNT(*)::int FROM moved`,
		tenantID, sourceID, targetID, actorID,
	).Scan(&count)
	return count, err
}

func countVotes(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM customer_request_votes
		WHERE tenant_id = $1 AND request_id = $2`, tenantID, requestID).Scan(&count)
	return count, err
}

func copyVotes(ctx context.Context, tx pgx.Tx, tenantID string, sourceID, targetID uuid.UUID, actorID string) (int, error) {
	var count int
	err := tx.QueryRow(
		ctx, `
		WITH moved AS (
			INSERT INTO customer_request_votes (
				tenant_id, request_id, subject_key, subject_hash, subject_display,
				account_key, account_display, weight, note, created_by, created_at
			)
			SELECT tenant_id, $3, subject_key, subject_hash, subject_display,
			       account_key, account_display, weight, note, $4, NOW()
			FROM customer_request_votes
			WHERE tenant_id = $1 AND request_id = $2
			ON CONFLICT (tenant_id, request_id, subject_hash, subject_key, account_key) DO NOTHING
			RETURNING id
		)
		SELECT COUNT(*)::int FROM moved`,
		tenantID, sourceID, targetID, actorID,
	).Scan(&count)
	return count, err
}

func countNotes(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM customer_request_notes
		WHERE tenant_id = $1 AND request_id = $2`, tenantID, requestID).Scan(&count)
	return count, err
}

func copyNotes(ctx context.Context, tx pgx.Tx, tenantID string, sourceID, targetID uuid.UUID) (int, error) {
	var count int
	err := tx.QueryRow(
		ctx, `
		WITH moved AS (
			INSERT INTO customer_request_notes (
				tenant_id, request_id, body, created_by, created_at
			)
			SELECT tenant_id, $3, body, created_by, created_at
			FROM customer_request_notes
			WHERE tenant_id = $1 AND request_id = $2
			RETURNING id
		)
		SELECT COUNT(*)::int FROM moved`,
		tenantID, sourceID, targetID,
	).Scan(&count)
	return count, err
}

func countIssueLinks(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM customer_request_issue_links
		WHERE tenant_id = $1 AND request_id = $2`, tenantID, requestID).Scan(&count)
	return count, err
}

func copyIssueLinks(ctx context.Context, tx pgx.Tx, tenantID string, sourceID, targetID uuid.UUID, actorID string) (int, error) {
	var count int
	err := tx.QueryRow(
		ctx, `
		WITH moved AS (
			INSERT INTO customer_request_issue_links (
				tenant_id, request_id, provider, external_key, external_url, title, status,
				created_by, last_synced_at, sync_state, external_status_category,
				external_assignee, external_updated_at, sync_error
			)
			SELECT tenant_id, $3, provider, external_key, external_url, title, status,
			       $4, last_synced_at, sync_state, external_status_category,
			       external_assignee, external_updated_at, sync_error
			FROM customer_request_issue_links
			WHERE tenant_id = $1 AND request_id = $2
			ON CONFLICT (tenant_id, request_id, provider, external_key) DO NOTHING
			RETURNING id
		)
		SELECT COUNT(*)::int FROM moved`,
		tenantID, sourceID, targetID, actorID,
	).Scan(&count)
	return count, err
}

func upsertAccountProfileTx(ctx context.Context, tx pgx.Tx, tenantID string, in AccountProfileInput) error {
	if strings.TrimSpace(in.AccountKey) == "" {
		return nil
	}
	source := strings.TrimSpace(in.Source)
	if source == "" {
		source = "manual"
	}
	currency := strings.TrimSpace(in.RevenueCurrency)
	if currency == "" {
		currency = "USD"
	}
	_, err := tx.Exec(
		ctx, `
		INSERT INTO customer_request_accounts (
			tenant_id,
			account_key,
			account_display,
			revenue_cents,
			revenue_currency,
			tier,
			size_segment,
			lifecycle_status,
			crm_provider,
			crm_external_id,
			source,
			created_by,
			updated_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
		ON CONFLICT (tenant_id, account_key)
		DO UPDATE SET
			account_display = COALESCE(NULLIF(EXCLUDED.account_display, ''), customer_request_accounts.account_display),
			revenue_cents = CASE
				WHEN EXCLUDED.revenue_cents > 0 THEN EXCLUDED.revenue_cents
				ELSE customer_request_accounts.revenue_cents
			END,
			revenue_currency = COALESCE(NULLIF(EXCLUDED.revenue_currency, ''), customer_request_accounts.revenue_currency),
			tier = COALESCE(NULLIF(EXCLUDED.tier, ''), customer_request_accounts.tier),
			size_segment = COALESCE(NULLIF(EXCLUDED.size_segment, ''), customer_request_accounts.size_segment),
			lifecycle_status = COALESCE(NULLIF(EXCLUDED.lifecycle_status, ''), customer_request_accounts.lifecycle_status),
			crm_provider = COALESCE(NULLIF(EXCLUDED.crm_provider, ''), customer_request_accounts.crm_provider),
			crm_external_id = COALESCE(NULLIF(EXCLUDED.crm_external_id, ''), customer_request_accounts.crm_external_id),
			source = EXCLUDED.source,
			updated_by = EXCLUDED.updated_by`,
		tenantID,
		strings.TrimSpace(in.AccountKey),
		strings.TrimSpace(in.AccountDisplay),
		in.RevenueCents,
		currency,
		strings.TrimSpace(in.Tier),
		strings.TrimSpace(in.SizeSegment),
		strings.TrimSpace(in.LifecycleStatus),
		strings.TrimSpace(in.CRMProvider),
		strings.TrimSpace(in.CRMExternalID),
		source,
		in.ActorID,
	)
	if err != nil {
		return mapWriteError(err, "upsert customer request account")
	}
	return nil
}

func touchRequestTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID, actorID string) error {
	tag, err := tx.Exec(
		ctx, `
		UPDATE customer_requests
		SET updated_by = $3
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, requestID, actorID,
	)
	if err != nil {
		return fmt.Errorf("touch customer request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const summarySelectSQLText = `
			SELECT
				cr.id,
				cr.tenant_id,
			cr.display_number,
			cr.display_id,
			cr.title,
			cr.description,
			cr.status,
			cr.priority,
			cr.owner_member_id::text,
			cr.created_by,
			cr.updated_by,
			cr.merged_into_request_id::text,
			cr.created_at,
			cr.updated_at,
			cr.archived_at,
			tm.id::text,
			tm.member_type,
			tm.user_id,
			COALESCE(tm.email, ''),
			tm.role,
			COALESCE(fc.supporting_feedback_count, 0),
			COALESCE(sc.customer_count, 0),
			COALESCE(sc.account_count, 0),
			COALESCE(ic.linked_issue_count, 0),
			COALESCE(vc.vote_count, 0),
			COALESCE(dc.duplicate_request_count, 0),
			COALESCE(fc.hidden_feedback_count, 0),
			COALESCE(ai.revenue_impact_cents, 0),
			COALESCE(ai.revenue_currency, 'USD'),
			(
				CASE cr.priority
					WHEN 'urgent' THEN COALESCE(css.priority_urgent_weight, 80)
					WHEN 'high' THEN COALESCE(css.priority_high_weight, 60)
					WHEN 'medium' THEN COALESCE(css.priority_medium_weight, 40)
					WHEN 'low' THEN COALESCE(css.priority_low_weight, 20)
					ELSE COALESCE(css.priority_none_weight, 0)
				END
				+ LEAST(
					COALESCE(fc.supporting_feedback_count, 0) * COALESCE(css.feedback_weight, 2),
					COALESCE(css.feedback_cap, 80)
				)
				+ LEAST(
					COALESCE(sc.customer_count, 0) * COALESCE(css.customer_weight, 5),
					COALESCE(css.customer_cap, 100)
				)
				+ LEAST(
					COALESCE(sc.account_count, 0) * COALESCE(css.account_weight, 8),
					COALESCE(css.account_cap, 120)
				)
				+ LEAST(
					COALESCE(vc.vote_count, 0) * COALESCE(css.vote_weight, 4),
					COALESCE(css.vote_cap, 80)
				)
				+ LEAST(
					(COALESCE(ai.revenue_impact_cents, 0) / COALESCE(NULLIF(css.revenue_cents_per_point, 0), 100000))::int,
					COALESCE(css.revenue_cap, 100)
				)
			)::int AS decision_score,
			CASE
				WHEN COALESCE(ic.linked_issue_count, 0) = 0 THEN 'no_links'
				WHEN COALESCE(ic.failed_issue_count, 0) > 0 THEN 'failed'
				WHEN COALESCE(ic.stale_issue_count, 0) > 0 THEN 'stale'
				WHEN COALESCE(ic.pending_issue_count, 0) > 0 THEN 'pending'
				WHEN COALESCE(ic.synced_issue_count, 0) = COALESCE(ic.linked_issue_count, 0) THEN 'synced'
				ELSE 'manual'
			END AS delivery_health,
			CASE
				WHEN COALESCE(ic.failed_issue_count, 0) > 0 THEN 5
				WHEN COALESCE(ic.stale_issue_count, 0) > 0 THEN 4
				WHEN COALESCE(ic.pending_issue_count, 0) > 0 THEN 3
				WHEN COALESCE(ic.linked_issue_count, 0) = 0 THEN 0
				WHEN COALESCE(ic.synced_issue_count, 0) = COALESCE(ic.linked_issue_count, 0) THEN 1
				ELSE 2
			END AS delivery_health_rank,
			COALESCE(ic.synced_issue_count, 0),
			COALESCE(ic.stale_issue_count, 0),
			COALESCE(ic.failed_issue_count, 0),
			COALESCE(ic.pending_issue_count, 0),
			COALESCE(ic.manual_issue_count, 0),
			fc.first_feedback_at,
			fc.latest_feedback_at
		FROM customer_requests cr
		LEFT JOIN tenant_members tm
		  ON tm.tenant_id = cr.tenant_id
		 AND tm.id = cr.owner_member_id
		LEFT JOIN customer_request_scoring_settings css
		  ON css.tenant_id = cr.tenant_id
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*) FILTER (WHERE uf.id IS NOT NULL AND uf.deleted_at IS NULL)::int AS supporting_feedback_count,
				MIN(uf.created_at) FILTER (WHERE uf.id IS NOT NULL AND uf.deleted_at IS NULL) AS first_feedback_at,
				MAX(uf.created_at) FILTER (WHERE uf.id IS NOT NULL AND uf.deleted_at IS NULL) AS latest_feedback_at,
				COUNT(*) FILTER (WHERE uf.id IS NULL OR uf.deleted_at IS NOT NULL)::int AS hidden_feedback_count
			FROM customer_request_feedback_links l
			LEFT JOIN user_feedback uf
			  ON uf.tenant_id = l.tenant_id
			 AND uf.id = l.feedback_id
			WHERE l.tenant_id = cr.tenant_id
			  AND l.request_id = cr.id
		) fc ON TRUE
		LEFT JOIN LATERAL (
			SELECT
				COUNT(DISTINCT identity) FILTER (WHERE identity <> '')::int AS customer_count,
				COUNT(DISTINCT account_key) FILTER (WHERE account_key <> '')::int AS account_count
			FROM (
				SELECT
					COALESCE(NULLIF(uf.subject_hash, ''), NULLIF(uf.subject_key, ''), NULLIF(uf.user_id, ''), '') AS identity,
					'' AS account_key
				FROM customer_request_feedback_links l
				JOIN user_feedback uf
				  ON uf.tenant_id = l.tenant_id
				 AND uf.id = l.feedback_id
				 AND uf.deleted_at IS NULL
				WHERE l.tenant_id = cr.tenant_id
				  AND l.request_id = cr.id
				UNION ALL
				SELECT
					COALESCE(NULLIF(cl.subject_hash, ''), NULLIF(cl.subject_key, ''), NULLIF(cl.account_key, ''), '') AS identity,
					COALESCE(NULLIF(cl.account_key, ''), '') AS account_key
				FROM customer_request_customer_links cl
				WHERE cl.tenant_id = cr.tenant_id
				  AND cl.request_id = cr.id
				UNION ALL
				SELECT
					COALESCE(NULLIF(v.subject_hash, ''), NULLIF(v.subject_key, ''), NULLIF(v.account_key, ''), '') AS identity,
					COALESCE(NULLIF(v.account_key, ''), '') AS account_key
				FROM customer_request_votes v
				WHERE v.tenant_id = cr.tenant_id
				  AND v.request_id = cr.id
			) supporters
		) sc ON TRUE
		LEFT JOIN LATERAL (
			SELECT
				COUNT(*)::int AS linked_issue_count,
				COUNT(*) FILTER (WHERE il.sync_state = 'synced')::int AS synced_issue_count,
				COUNT(*) FILTER (WHERE il.sync_state = 'stale')::int AS stale_issue_count,
				COUNT(*) FILTER (WHERE il.sync_state = 'failed')::int AS failed_issue_count,
				COUNT(*) FILTER (WHERE il.sync_state = 'pending')::int AS pending_issue_count,
				COUNT(*) FILTER (WHERE il.sync_state = 'manual')::int AS manual_issue_count
			FROM customer_request_issue_links il
			WHERE il.tenant_id = cr.tenant_id
			  AND il.request_id = cr.id
		) ic ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS vote_count
			FROM customer_request_votes v
			WHERE v.tenant_id = cr.tenant_id
			  AND v.request_id = cr.id
		) vc ON TRUE
		LEFT JOIN LATERAL (
			SELECT
				COALESCE(SUM(ca.revenue_cents), 0)::bigint AS revenue_impact_cents,
				COALESCE(MIN(NULLIF(ca.revenue_currency, '')), 'USD') AS revenue_currency
			FROM (
				SELECT DISTINCT account_key
				FROM customer_request_customer_links cl
				WHERE cl.tenant_id = cr.tenant_id
				  AND cl.request_id = cr.id
				  AND cl.account_key <> ''
				UNION
				SELECT DISTINCT account_key
				FROM customer_request_votes v
				WHERE v.tenant_id = cr.tenant_id
				  AND v.request_id = cr.id
				  AND v.account_key <> ''
			) accounts
			JOIN customer_request_accounts ca
			  ON ca.tenant_id = cr.tenant_id
			 AND ca.account_key = accounts.account_key
		) ai ON TRUE
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS duplicate_request_count
				FROM customer_requests dup
				WHERE dup.tenant_id = cr.tenant_id
				  AND dup.merged_into_request_id = cr.id
			) dc ON TRUE`

func summarySelectSQL() string {
	return summarySelectSQLText
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSummary(row scanner, out *Summary) error { // ptrext:allow scan-target
	var ownerMemberID, mergedInto sql.NullString
	var ownerID, ownerType, ownerUserID, ownerEmail, ownerRole sql.NullString
	var status, priority, deliveryHealth string
	err := row.Scan(
		&out.ID,
		&out.TenantID,
		&out.DisplayNumber,
		&out.DisplayID,
		&out.Title,
		&out.Description,
		&status,
		&priority,
		&ownerMemberID,
		&out.CreatedBy,
		&out.UpdatedBy,
		&mergedInto,
		&out.CreatedAt,
		&out.UpdatedAt,
		&out.ArchivedAt,
		&ownerID,
		&ownerType,
		&ownerUserID,
		&ownerEmail,
		&ownerRole,
		&out.SupportingFeedbackCount,
		&out.CustomerCount,
		&out.AccountCount,
		&out.LinkedIssueCount,
		&out.VoteCount,
		&out.DuplicateRequestCount,
		&out.HiddenFeedbackCount,
		&out.RevenueImpactCents,
		&out.RevenueCurrency,
		&out.DecisionScore,
		&deliveryHealth,
		&out.DeliveryHealthRank,
		&out.SyncedIssueCount,
		&out.StaleIssueCount,
		&out.FailedIssueCount,
		&out.PendingIssueCount,
		&out.ManualIssueCount,
		&out.FirstFeedbackAt,
		&out.LatestFeedbackAt,
	)
	if err != nil {
		return err
	}
	out.Status = Status(status)
	out.Priority = Priority(priority)
	out.DeliveryHealth = DeliveryHealth(deliveryHealth)
	out.DecisionScoreExplanation = decisionScoreExplanation(out)
	if ownerMemberID.Valid {
		parsed, parseErr := uuid.Parse(ownerMemberID.String)
		if parseErr != nil {
			return parseErr
		}
		out.OwnerMemberID = ptrext.Of(parsed)
	}
	if mergedInto.Valid {
		parsed, parseErr := uuid.Parse(mergedInto.String)
		if parseErr != nil {
			return parseErr
		}
		out.MergedIntoRequestID = ptrext.Of(parsed)
	}
	if ownerID.Valid {
		parsed, parseErr := uuid.Parse(ownerID.String)
		if parseErr != nil {
			return parseErr
		}
		out.Owner = ptrext.Of(Owner{
			ID:         parsed,
			MemberType: ownerType.String,
			UserID:     ownerUserID.String,
			Email:      ownerEmail.String,
			Role:       ownerRole.String,
		})
	}
	return nil
}

func scoringSettingsSelectSQL() string {
	return `
		SELECT
			tenant_id,
			priority_none_weight,
			priority_low_weight,
			priority_medium_weight,
			priority_high_weight,
			priority_urgent_weight,
			feedback_weight,
			feedback_cap,
			customer_weight,
			customer_cap,
			account_weight,
			account_cap,
			vote_weight,
			vote_cap,
			revenue_cents_per_point,
			revenue_cap,
			updated_by,
			updated_at
		FROM customer_request_scoring_settings`
}

func scanScoringSettings(row scanner) (ScoringSettings, error) { // ptrext:allow scan-target
	var out ScoringSettings
	err := row.Scan(
		&out.TenantID,
		&out.PriorityNoneWeight,
		&out.PriorityLowWeight,
		&out.PriorityMediumWeight,
		&out.PriorityHighWeight,
		&out.PriorityUrgentWeight,
		&out.FeedbackWeight,
		&out.FeedbackCap,
		&out.CustomerWeight,
		&out.CustomerCap,
		&out.AccountWeight,
		&out.AccountCap,
		&out.VoteWeight,
		&out.VoteCap,
		&out.RevenueCentsPerPoint,
		&out.RevenueCap,
		&out.UpdatedBy,
		&out.UpdatedAt,
	)
	return out, err
}

func listEvidence(ctx context.Context, db queryer, tenantID string, requestID uuid.UUID, limit int) ([]FeedbackEvidence, error) {
	rows, err := db.Query(ctx, `
		SELECT
			l.feedback_id,
			uf.content,
			uf.source,
			uf.type,
			uf.user_id,
			uf.subject_display,
			COALESCE(uf.enriched_title, ''),
			l.importance,
			l.note,
			l.created_by,
			l.created_at,
			uf.created_at
		FROM customer_request_feedback_links l
		JOIN user_feedback uf
		  ON uf.tenant_id = l.tenant_id
		 AND uf.id = l.feedback_id
		 AND uf.deleted_at IS NULL
		WHERE l.tenant_id = $1 AND l.request_id = $2
		ORDER BY l.created_at DESC, l.feedback_id DESC
		LIMIT $3`, tenantID, requestID, limit)
	if err != nil {
		return nil, fmt.Errorf("list customer request evidence: %w", err)
	}
	defer rows.Close()
	var out []FeedbackEvidence
	for rows.Next() {
		var item FeedbackEvidence
		var importance string
		if err := rows.Scan(
			&item.FeedbackID,
			&item.Content,
			&item.Source,
			&item.Type,
			&item.UserID,
			&item.SubjectDisplay,
			&item.EnrichedTitle,
			&importance,
			&item.Note,
			&item.LinkedBy,
			&item.LinkedAt,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		item.Importance = Importance(importance)
		out = append(out, item)
	}
	return out, rows.Err()
}

func listCustomerLinks(ctx context.Context, db queryer, tenantID string, requestID uuid.UUID) ([]CustomerLink, error) {
	rows, err := db.Query(ctx, `
		SELECT
			cl.id,
			cl.subject_key,
			cl.subject_hash,
			cl.subject_display,
			cl.account_key,
			cl.account_display,
			cl.note,
			cl.created_by,
			cl.created_at,
			ca.account_key,
			ca.account_display,
			ca.revenue_cents,
			ca.revenue_currency,
			ca.tier,
			ca.size_segment,
			ca.lifecycle_status,
			ca.crm_provider,
			ca.crm_external_id,
			ca.source,
			ca.updated_at
		FROM customer_request_customer_links cl
		LEFT JOIN customer_request_accounts ca
		  ON ca.tenant_id = cl.tenant_id
		 AND ca.account_key = cl.account_key
		WHERE cl.tenant_id = $1 AND cl.request_id = $2
		ORDER BY cl.created_at DESC, cl.id DESC`, tenantID, requestID)
	if err != nil {
		return nil, fmt.Errorf("list customer request customer links: %w", err)
	}
	defer rows.Close()
	var out []CustomerLink
	for rows.Next() {
		var item CustomerLink
		if err := scanCustomerLink(rows, &item); err != nil { // ptrext:allow scan-target
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func getCustomerLink(ctx context.Context, db queryer, tenantID string, requestID, linkID uuid.UUID) (*CustomerLink, error) {
	var out CustomerLink
	err := scanCustomerLink(db.QueryRow(
		ctx, `
		SELECT
			cl.id,
			cl.subject_key,
			cl.subject_hash,
			cl.subject_display,
			cl.account_key,
			cl.account_display,
			cl.note,
			cl.created_by,
			cl.created_at,
			ca.account_key,
			ca.account_display,
			ca.revenue_cents,
			ca.revenue_currency,
			ca.tier,
			ca.size_segment,
			ca.lifecycle_status,
			ca.crm_provider,
			ca.crm_external_id,
			ca.source,
			ca.updated_at
		FROM customer_request_customer_links cl
		LEFT JOIN customer_request_accounts ca
		  ON ca.tenant_id = cl.tenant_id
		 AND ca.account_key = cl.account_key
		WHERE cl.tenant_id = $1 AND cl.request_id = $2 AND cl.id = $3`,
		tenantID, requestID, linkID,
	), &out) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(out), nil
}

func scanCustomerLink(row scanner, out *CustomerLink) error { // ptrext:allow scan-target
	var profile accountProfileScan
	err := row.Scan(
		&out.ID,
		&out.SubjectKey,
		&out.SubjectHash,
		&out.SubjectDisplay,
		&out.AccountKey,
		&out.AccountDisplay,
		&out.Note,
		&out.CreatedBy,
		&out.CreatedAt,
		&profile.AccountKey,
		&profile.AccountDisplay,
		&profile.RevenueCents,
		&profile.RevenueCurrency,
		&profile.Tier,
		&profile.SizeSegment,
		&profile.LifecycleStatus,
		&profile.CRMProvider,
		&profile.CRMExternalID,
		&profile.Source,
		&profile.UpdatedAt,
	)
	if err != nil {
		return err
	}
	out.AccountProfile = profile.toProfile()
	return nil
}

func listVotes(ctx context.Context, db queryer, tenantID string, requestID uuid.UUID) ([]Vote, error) {
	rows, err := db.Query(ctx, `
		SELECT
			v.id,
			v.subject_key,
			v.subject_hash,
			v.subject_display,
			v.account_key,
			v.account_display,
			v.weight,
			v.note,
			v.created_by,
			v.created_at,
			ca.account_key,
			ca.account_display,
			ca.revenue_cents,
			ca.revenue_currency,
			ca.tier,
			ca.size_segment,
			ca.lifecycle_status,
			ca.crm_provider,
			ca.crm_external_id,
			ca.source,
			ca.updated_at
		FROM customer_request_votes v
		LEFT JOIN customer_request_accounts ca
		  ON ca.tenant_id = v.tenant_id
		 AND ca.account_key = v.account_key
		WHERE v.tenant_id = $1 AND v.request_id = $2
		ORDER BY v.created_at DESC, v.id DESC`, tenantID, requestID)
	if err != nil {
		return nil, fmt.Errorf("list customer request votes: %w", err)
	}
	defer rows.Close()
	var out []Vote
	for rows.Next() {
		var item Vote
		if err := scanVote(rows, &item); err != nil { // ptrext:allow scan-target
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func getVote(ctx context.Context, db queryer, tenantID string, requestID, voteID uuid.UUID) (*Vote, error) {
	var out Vote
	err := scanVote(db.QueryRow(
		ctx, `
		SELECT
			v.id,
			v.subject_key,
			v.subject_hash,
			v.subject_display,
			v.account_key,
			v.account_display,
			v.weight,
			v.note,
			v.created_by,
			v.created_at,
			ca.account_key,
			ca.account_display,
			ca.revenue_cents,
			ca.revenue_currency,
			ca.tier,
			ca.size_segment,
			ca.lifecycle_status,
			ca.crm_provider,
			ca.crm_external_id,
			ca.source,
			ca.updated_at
		FROM customer_request_votes v
		LEFT JOIN customer_request_accounts ca
		  ON ca.tenant_id = v.tenant_id
		 AND ca.account_key = v.account_key
		WHERE v.tenant_id = $1 AND v.request_id = $2 AND v.id = $3`,
		tenantID, requestID, voteID,
	), &out) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(out), nil
}

func scanVote(row scanner, out *Vote) error { // ptrext:allow scan-target
	var profile accountProfileScan
	err := row.Scan(
		&out.ID,
		&out.SubjectKey,
		&out.SubjectHash,
		&out.SubjectDisplay,
		&out.AccountKey,
		&out.AccountDisplay,
		&out.Weight,
		&out.Note,
		&out.CreatedBy,
		&out.CreatedAt,
		&profile.AccountKey,
		&profile.AccountDisplay,
		&profile.RevenueCents,
		&profile.RevenueCurrency,
		&profile.Tier,
		&profile.SizeSegment,
		&profile.LifecycleStatus,
		&profile.CRMProvider,
		&profile.CRMExternalID,
		&profile.Source,
		&profile.UpdatedAt,
	)
	if err != nil {
		return err
	}
	out.AccountProfile = profile.toProfile()
	return nil
}

func listNotes(ctx context.Context, db queryer, tenantID string, requestID uuid.UUID) ([]Note, error) {
	rows, err := db.Query(ctx, `
		SELECT id, body, created_by, created_at
		FROM customer_request_notes
		WHERE tenant_id = $1 AND request_id = $2
		ORDER BY created_at DESC, id DESC`, tenantID, requestID)
	if err != nil {
		return nil, fmt.Errorf("list customer request notes: %w", err)
	}
	defer rows.Close()
	var out []Note
	for rows.Next() {
		var item Note
		if err := rows.Scan(&item.ID, &item.Body, &item.CreatedBy, &item.CreatedAt); err != nil { // ptrext:allow scan-target
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func getNote(ctx context.Context, db queryer, tenantID string, requestID, noteID uuid.UUID) (*Note, error) {
	var out Note
	err := db.QueryRow(
		ctx, `
		SELECT id, body, created_by, created_at
		FROM customer_request_notes
		WHERE tenant_id = $1 AND request_id = $2 AND id = $3`,
		tenantID, requestID, noteID,
	).Scan(&out.ID, &out.Body, &out.CreatedBy, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(out), nil
}

func listDuplicates(ctx context.Context, db queryer, tenantID string, requestID uuid.UUID) ([]Duplicate, error) {
	rows, err := db.Query(ctx, `
		SELECT id, display_id, title, archived_at
		FROM customer_requests
		WHERE tenant_id = $1 AND merged_into_request_id = $2
		ORDER BY archived_at DESC, id DESC`, tenantID, requestID)
	if err != nil {
		return nil, fmt.Errorf("list customer request duplicates: %w", err)
	}
	defer rows.Close()
	var out []Duplicate
	for rows.Next() {
		var item Duplicate
		if err := rows.Scan(&item.ID, &item.DisplayID, &item.Title, &item.MergedAt); err != nil { // ptrext:allow scan-target
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func listIssueLinks(ctx context.Context, db queryer, tenantID string, requestID uuid.UUID) ([]IssueLink, error) {
	rows, err := db.Query(ctx, `
		SELECT
			id,
			provider,
			external_key,
			external_url,
			title,
			status,
			created_by,
			created_at,
			updated_at,
			last_synced_at,
			sync_state,
			external_status_category,
			external_assignee,
			external_updated_at,
			sync_error
		FROM customer_request_issue_links
		WHERE tenant_id = $1 AND request_id = $2
		ORDER BY created_at DESC, id DESC`, tenantID, requestID)
	if err != nil {
		return nil, fmt.Errorf("list customer request issue links: %w", err)
	}
	defer rows.Close()
	var out []IssueLink
	for rows.Next() {
		var item IssueLink
		if err := scanIssueLink(rows, &item); err != nil { // ptrext:allow scan-target
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func getIssueLink(ctx context.Context, db queryer, tenantID string, requestID, issueLinkID uuid.UUID) (*IssueLink, error) {
	var out IssueLink
	err := scanIssueLink(db.QueryRow(
		ctx, `
		SELECT
			id,
			provider,
			external_key,
			external_url,
			title,
			status,
			created_by,
			created_at,
			updated_at,
			last_synced_at,
			sync_state,
			external_status_category,
			external_assignee,
			external_updated_at,
			sync_error
		FROM customer_request_issue_links
		WHERE tenant_id = $1 AND request_id = $2 AND id = $3`,
		tenantID, requestID, issueLinkID,
	), &out) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLinkNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(out), nil
}

func scanIssueLink(row scanner, out *IssueLink) error { // ptrext:allow scan-target
	var syncState string
	err := row.Scan(
		&out.ID,
		&out.Provider,
		&out.ExternalKey,
		&out.ExternalURL,
		&out.Title,
		&out.Status,
		&out.CreatedBy,
		&out.CreatedAt,
		&out.UpdatedAt,
		&out.LastSyncedAt,
		&syncState,
		&out.ExternalStatusCategory,
		&out.ExternalAssignee,
		&out.ExternalUpdatedAt,
		&out.SyncError,
	)
	if err != nil {
		return err
	}
	out.SyncState = IssueSyncState(syncState)
	return nil
}

type accountProfileScan struct {
	AccountKey      sql.NullString
	AccountDisplay  sql.NullString
	RevenueCents    sql.NullInt64
	RevenueCurrency sql.NullString
	Tier            sql.NullString
	SizeSegment     sql.NullString
	LifecycleStatus sql.NullString
	CRMProvider     sql.NullString
	CRMExternalID   sql.NullString
	Source          sql.NullString
	UpdatedAt       sql.NullTime
}

func (s accountProfileScan) toProfile() *AccountProfile {
	if !s.AccountKey.Valid {
		return nil
	}
	return ptrext.Of(AccountProfile{
		AccountKey:      s.AccountKey.String,
		AccountDisplay:  s.AccountDisplay.String,
		RevenueCents:    s.RevenueCents.Int64,
		RevenueCurrency: s.RevenueCurrency.String,
		Tier:            s.Tier.String,
		SizeSegment:     s.SizeSegment.String,
		LifecycleStatus: s.LifecycleStatus.String,
		CRMProvider:     s.CRMProvider.String,
		CRMExternalID:   s.CRMExternalID.String,
		Source:          s.Source.String,
		UpdatedAt:       s.UpdatedAt.Time,
	})
}

func collectAccountProfiles(customers []CustomerLink, votes []Vote) []AccountProfile {
	seen := make(map[string]struct{})
	out := make([]AccountProfile, 0)
	for _, item := range customers {
		if item.AccountProfile == nil {
			continue
		}
		profile := ptrext.Indirect(item.AccountProfile)
		if _, ok := seen[profile.AccountKey]; ok {
			continue
		}
		seen[profile.AccountKey] = struct{}{}
		out = append(out, profile)
	}
	for _, item := range votes {
		if item.AccountProfile == nil {
			continue
		}
		profile := ptrext.Indirect(item.AccountProfile)
		if _, ok := seen[profile.AccountKey]; ok {
			continue
		}
		seen[profile.AccountKey] = struct{}{}
		out = append(out, profile)
	}
	return out
}

func decisionScoreExplanation(summary *Summary) string {
	if summary == nil {
		return ""
	}
	return fmt.Sprintf(
		"priority=%s feedback=%d customers=%d accounts=%d votes=%d revenue_cents=%d delivery_health=%s",
		summary.Priority,
		summary.SupportingFeedbackCount,
		summary.CustomerCount,
		summary.AccountCount,
		summary.VoteCount,
		summary.RevenueImpactCents,
		summary.DeliveryHealth,
	)
}

func scanOwner(row scanner) (*Owner, error) { // ptrext:allow scan-target
	var rawID string
	var owner Owner
	err := row.Scan(&rawID, &owner.MemberType, &owner.UserID, &owner.Email, &owner.Role)
	if err != nil {
		return nil, err
	}
	parsed, err := uuid.Parse(rawID)
	if err != nil {
		return nil, err
	}
	owner.ID = parsed
	return ptrext.Of(owner), nil
}

func mapWriteError(err error, op string) error {
	if pgxutil.IsCheckViolation(err) {
		return ErrInvalidInput
	}
	if pgxutil.IsUniqueViolation(err) {
		return ErrConflict
	}
	if pgxutil.IsForeignKeyViolation(err) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", op, err)
}
