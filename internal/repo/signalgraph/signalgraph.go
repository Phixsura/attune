// SPDX-License-Identifier: Apache-2.0

// Package signalgraph owns the durable customer signal identity graph.
package signalgraph

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var (
	ErrFeedbackNotFound = errors.New("signal graph feedback evidence not found")
	ErrIdentityNotFound = errors.New("signal graph identity not found")
	ErrSubjectNotFound  = errors.New("signal graph subject not found")
	ErrConflict         = errors.New("signal graph identity conflict")
)

type Repo struct {
	pool pool
}

func New(pool *pgxpool.Pool) *Repo {
	return ptrext.Of(Repo{pool: pool})
}

type pool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (r *Repo) Begin(ctx context.Context) (pgx.Tx, error) {
	return r.pool.Begin(ctx)
}

type Subject struct {
	ID                   uuid.UUID
	TenantID             string
	DisplayName          string
	PrimaryIdentityKind  string
	PrimaryIdentityValue string
	Status               string
	IdentityCount        int
	EvidenceCount        int
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

type MergeIdentityReviewInput struct {
	TenantID                string
	ActorID                 string
	IdentityKind            string
	IdentityValue           string
	IdentityValueNormalized string
	FeedbackIDs             []int64
	Note                    string
}

type MergeIdentityReviewResult struct {
	Subject        Subject
	EvidenceCount  int
	CreatedSubject bool
}

type SplitIdentityReviewInput struct {
	TenantID                string
	ActorID                 string
	SubjectID               uuid.UUID
	IdentityKind            string
	IdentityValue           string
	IdentityValueNormalized string
	Note                    string
}

type SplitIdentityReviewResult struct {
	Subject       Subject
	EvidenceCount int
}

type RecentMerge struct {
	EventID       uuid.UUID
	Subject       Subject
	IdentityKind  string
	IdentityValue string
	FeedbackIDs   []int64
	EvidenceCount int
	CreatedBy     string
	CreatedAt     time.Time
}

type SubjectRoster struct {
	ActiveSubjectCount  int
	ActiveIdentityCount int
	EvidenceCount       int
	Subjects            []Subject
}

type SubjectDetail struct {
	Subject    Subject
	Identities []SubjectIdentity
	Events     []SubjectEvent
}

type SubjectIdentity struct {
	ID               uuid.UUID
	Kind             string
	Value            string
	Source           string
	Confidence       string
	EvidenceCount    int
	FirstFeedbackID  int64
	LatestFeedbackID int64
	Revoked          bool
	RevokedAt        sql.NullTime
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type SubjectEvent struct {
	ID            uuid.UUID
	Action        string
	IdentityKind  string
	IdentityValue string
	FeedbackIDs   []int64
	Evidence      []SubjectEventEvidence
	EvidenceCount int
	Note          string
	CreatedBy     string
	CreatedAt     time.Time
}

type SubjectEventEvidence struct {
	ID         int64
	Source     string
	UserID     string
	Content    string
	SourceMeta []byte
	CreatedAt  time.Time
}

type feedbackEvidenceStats struct {
	Count            int
	FirstFeedbackID  sql.NullInt64
	LatestFeedbackID sql.NullInt64
}

type activeIdentity struct {
	Value         string
	EvidenceCount int
	FeedbackIDs   []int64
}

func (r *Repo) MergeIdentityReviewTx(
	ctx context.Context,
	tx pgx.Tx,
	in MergeIdentityReviewInput,
) (MergeIdentityReviewResult, error) {
	stats, err := r.feedbackEvidenceStatsTx(ctx, tx, in.TenantID, in.FeedbackIDs)
	if err != nil {
		return MergeIdentityReviewResult{}, err
	}
	if stats.Count != len(in.FeedbackIDs) {
		return MergeIdentityReviewResult{}, ErrFeedbackNotFound
	}
	subject, created, err := r.subjectForIdentityTx(ctx, tx, in)
	if err != nil {
		return MergeIdentityReviewResult{}, err
	}
	if err := r.upsertIdentityTx(ctx, tx, in, subject.ID, stats); err != nil {
		return MergeIdentityReviewResult{}, err
	}
	if err := r.insertMergeEventTx(ctx, tx, in, subject.ID, stats.Count); err != nil {
		return MergeIdentityReviewResult{}, err
	}
	summary, err := r.subjectSummaryTx(ctx, tx, in.TenantID, subject.ID)
	if err != nil {
		return MergeIdentityReviewResult{}, err
	}
	return MergeIdentityReviewResult{
		Subject:        summary,
		EvidenceCount:  stats.Count,
		CreatedSubject: created,
	}, nil
}

func (r *Repo) SplitIdentityReviewTx(
	ctx context.Context,
	tx pgx.Tx,
	in SplitIdentityReviewInput,
) (SplitIdentityReviewResult, error) {
	identity, err := r.revokeIdentityTx(ctx, tx, in)
	if err != nil {
		return SplitIdentityReviewResult{}, err
	}
	if err := r.insertSplitEventTx(ctx, tx, in, identity); err != nil {
		return SplitIdentityReviewResult{}, err
	}
	if err := r.refreshSubjectPrimaryIdentityTx(ctx, tx, in.TenantID, in.SubjectID, in.ActorID); err != nil {
		return SplitIdentityReviewResult{}, err
	}
	subject, err := r.subjectSummaryTx(ctx, tx, in.TenantID, in.SubjectID)
	if err != nil {
		return SplitIdentityReviewResult{}, err
	}
	return SplitIdentityReviewResult{Subject: subject, EvidenceCount: identity.EvidenceCount}, nil
}

func (r *Repo) ListRecentMerges(ctx context.Context, tenantID string, limit int) ([]RecentMerge, error) {
	const q = `
		SELECT
			e.id,
			e.identity_kind,
			e.identity_value,
			e.feedback_ids,
			e.evidence_count,
			e.created_by,
			e.created_at,
			s.id,
			s.tenant_id,
			s.display_name,
			s.primary_identity_kind,
			s.primary_identity_value,
			s.status,
			COUNT(i.id),
			COALESCE(SUM(i.evidence_count), 0),
			s.created_at,
			s.updated_at
		FROM signal_subject_merge_events e
		JOIN signal_subjects s ON s.tenant_id = e.tenant_id AND s.id = e.subject_id
		LEFT JOIN signal_subject_identities i
		  ON i.tenant_id = s.tenant_id
		 AND i.subject_id = s.id
		 AND i.revoked_at IS NULL
		WHERE e.tenant_id = $1
		  AND e.action = 'review_merge'
		  AND EXISTS (
			SELECT 1
			FROM signal_subject_identities active
			WHERE active.tenant_id = e.tenant_id
			  AND active.subject_id = e.subject_id
			  AND active.kind = e.identity_kind
			  AND active.value_normalized = e.identity_value_normalized
			  AND active.revoked_at IS NULL
		  )
		GROUP BY e.id, e.identity_kind, e.identity_value, e.feedback_ids, e.evidence_count,
		         e.created_by, e.created_at, s.id, s.tenant_id, s.display_name,
		         s.primary_identity_kind, s.primary_identity_value, s.status,
		         s.created_at, s.updated_at
		ORDER BY e.created_at DESC, e.id DESC
		LIMIT $2`
	rows, err := r.pool.Query(ctx, q, tenantID, boundedRecentMergeLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("query recent signal subject merges: %w", err)
	}
	defer rows.Close()
	items := make([]RecentMerge, 0)
	for rows.Next() {
		var item RecentMerge
		err := rows.Scan(
			&item.EventID,                      // ptrext:allow - pgx Scan requires destination addresses.
			&item.IdentityKind,                 // ptrext:allow - pgx Scan requires destination addresses.
			&item.IdentityValue,                // ptrext:allow - pgx Scan requires destination addresses.
			&item.FeedbackIDs,                  // ptrext:allow - pgx Scan requires destination addresses.
			&item.EvidenceCount,                // ptrext:allow - pgx Scan requires destination addresses.
			&item.CreatedBy,                    // ptrext:allow - pgx Scan requires destination addresses.
			&item.CreatedAt,                    // ptrext:allow - pgx Scan requires destination addresses.
			&item.Subject.ID,                   // ptrext:allow - pgx Scan requires destination addresses.
			&item.Subject.TenantID,             // ptrext:allow - pgx Scan requires destination addresses.
			&item.Subject.DisplayName,          // ptrext:allow - pgx Scan requires destination addresses.
			&item.Subject.PrimaryIdentityKind,  // ptrext:allow - pgx Scan requires destination addresses.
			&item.Subject.PrimaryIdentityValue, // ptrext:allow - pgx Scan requires destination addresses.
			&item.Subject.Status,               // ptrext:allow - pgx Scan requires destination addresses.
			&item.Subject.IdentityCount,        // ptrext:allow - pgx Scan requires destination addresses.
			&item.Subject.EvidenceCount,        // ptrext:allow - pgx Scan requires destination addresses.
			&item.Subject.CreatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
			&item.Subject.UpdatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
		)
		if err != nil {
			return nil, fmt.Errorf("scan recent signal subject merge: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recent signal subject merges: %w", err)
	}
	return items, nil
}

func (r *Repo) ListSubjectRoster(ctx context.Context, tenantID string, limit int) (SubjectRoster, error) {
	const totalsQ = `
		SELECT
			COUNT(DISTINCT s.id),
			COUNT(i.id),
			COALESCE(SUM(i.evidence_count), 0)
		FROM signal_subjects s
		LEFT JOIN signal_subject_identities i
		  ON i.tenant_id = s.tenant_id
		 AND i.subject_id = s.id
		 AND i.revoked_at IS NULL
		WHERE s.tenant_id = $1 AND s.status = 'active'`
	var roster SubjectRoster
	err := r.pool.QueryRow(ctx, totalsQ, tenantID).Scan(
		&roster.ActiveSubjectCount,  // ptrext:allow - pgx Scan requires destination addresses.
		&roster.ActiveIdentityCount, // ptrext:allow - pgx Scan requires destination addresses.
		&roster.EvidenceCount,       // ptrext:allow - pgx Scan requires destination addresses.
	)
	if err != nil {
		return SubjectRoster{}, fmt.Errorf("query signal subject roster totals: %w", err)
	}

	const subjectsQ = `
		SELECT
			s.id,
			s.tenant_id,
			s.display_name,
			s.primary_identity_kind,
			s.primary_identity_value,
			s.status,
			COUNT(i.id),
			COALESCE(SUM(i.evidence_count), 0),
			s.created_at,
			s.updated_at
		FROM signal_subjects s
		LEFT JOIN signal_subject_identities i
		  ON i.tenant_id = s.tenant_id
		 AND i.subject_id = s.id
		 AND i.revoked_at IS NULL
		WHERE s.tenant_id = $1 AND s.status = 'active'
		GROUP BY s.id, s.tenant_id, s.display_name, s.primary_identity_kind,
		         s.primary_identity_value, s.status, s.created_at, s.updated_at
		ORDER BY COALESCE(SUM(i.evidence_count), 0) DESC, COUNT(i.id) DESC, s.updated_at DESC, s.id DESC
		LIMIT $2`
	rows, err := r.pool.Query(ctx, subjectsQ, tenantID, boundedSubjectRosterLimit(limit))
	if err != nil {
		return SubjectRoster{}, fmt.Errorf("query signal subject roster: %w", err)
	}
	defer rows.Close()
	roster.Subjects = make([]Subject, 0)
	for rows.Next() {
		var subject Subject
		err := rows.Scan(
			&subject.ID,                   // ptrext:allow - pgx Scan requires destination addresses.
			&subject.TenantID,             // ptrext:allow - pgx Scan requires destination addresses.
			&subject.DisplayName,          // ptrext:allow - pgx Scan requires destination addresses.
			&subject.PrimaryIdentityKind,  // ptrext:allow - pgx Scan requires destination addresses.
			&subject.PrimaryIdentityValue, // ptrext:allow - pgx Scan requires destination addresses.
			&subject.Status,               // ptrext:allow - pgx Scan requires destination addresses.
			&subject.IdentityCount,        // ptrext:allow - pgx Scan requires destination addresses.
			&subject.EvidenceCount,        // ptrext:allow - pgx Scan requires destination addresses.
			&subject.CreatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
			&subject.UpdatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
		)
		if err != nil {
			return SubjectRoster{}, fmt.Errorf("scan signal subject roster: %w", err)
		}
		roster.Subjects = append(roster.Subjects, subject)
	}
	if err := rows.Err(); err != nil {
		return SubjectRoster{}, fmt.Errorf("iterate signal subject roster: %w", err)
	}
	return roster, nil
}

func (r *Repo) SubjectDetail(ctx context.Context, tenantID string, subjectID uuid.UUID, eventLimit int) (SubjectDetail, error) {
	subject, err := r.subjectSummary(ctx, tenantID, subjectID)
	if err != nil {
		return SubjectDetail{}, err
	}
	identities, err := r.subjectIdentities(ctx, tenantID, subjectID)
	if err != nil {
		return SubjectDetail{}, err
	}
	events, err := r.subjectEvents(ctx, tenantID, subjectID, eventLimit)
	if err != nil {
		return SubjectDetail{}, err
	}
	events, err = r.attachSubjectEventEvidence(ctx, tenantID, events)
	if err != nil {
		return SubjectDetail{}, err
	}
	return SubjectDetail{
		Subject:    subject,
		Identities: identities,
		Events:     events,
	}, nil
}

func (r *Repo) subjectSummary(ctx context.Context, tenantID string, subjectID uuid.UUID) (Subject, error) {
	const q = `
		SELECT
			s.id,
			s.tenant_id,
			s.display_name,
			s.primary_identity_kind,
			s.primary_identity_value,
			s.status,
			COUNT(i.id),
			COALESCE(SUM(i.evidence_count), 0),
			s.created_at,
			s.updated_at
		FROM signal_subjects s
		LEFT JOIN signal_subject_identities i
		  ON i.tenant_id = s.tenant_id
		 AND i.subject_id = s.id
		 AND i.revoked_at IS NULL
		WHERE s.tenant_id = $1 AND s.id = $2
		GROUP BY s.id, s.tenant_id, s.display_name, s.primary_identity_kind,
		         s.primary_identity_value, s.status, s.created_at, s.updated_at`
	var subject Subject
	err := r.pool.QueryRow(ctx, q, tenantID, subjectID).Scan(
		&subject.ID,                   // ptrext:allow - pgx Scan requires destination addresses.
		&subject.TenantID,             // ptrext:allow - pgx Scan requires destination addresses.
		&subject.DisplayName,          // ptrext:allow - pgx Scan requires destination addresses.
		&subject.PrimaryIdentityKind,  // ptrext:allow - pgx Scan requires destination addresses.
		&subject.PrimaryIdentityValue, // ptrext:allow - pgx Scan requires destination addresses.
		&subject.Status,               // ptrext:allow - pgx Scan requires destination addresses.
		&subject.IdentityCount,        // ptrext:allow - pgx Scan requires destination addresses.
		&subject.EvidenceCount,        // ptrext:allow - pgx Scan requires destination addresses.
		&subject.CreatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
		&subject.UpdatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subject{}, ErrSubjectNotFound
	}
	if err != nil {
		return Subject{}, fmt.Errorf("query signal subject detail summary: %w", err)
	}
	return subject, nil
}

func (r *Repo) subjectIdentities(
	ctx context.Context,
	tenantID string,
	subjectID uuid.UUID,
) ([]SubjectIdentity, error) {
	const q = `
		SELECT
			id,
			kind,
			value,
			source,
			confidence,
			evidence_count,
			COALESCE(first_feedback_id, 0),
			COALESCE(latest_feedback_id, 0),
			revoked_at IS NOT NULL,
			revoked_at,
			created_at,
			updated_at
		FROM signal_subject_identities
		WHERE tenant_id = $1 AND subject_id = $2
		ORDER BY revoked_at IS NOT NULL ASC, evidence_count DESC, updated_at DESC, id DESC`
	rows, err := r.pool.Query(ctx, q, tenantID, subjectID)
	if err != nil {
		return nil, fmt.Errorf("query signal subject identities: %w", err)
	}
	defer rows.Close()
	items := make([]SubjectIdentity, 0)
	for rows.Next() {
		var item SubjectIdentity
		err := rows.Scan(
			&item.ID,               // ptrext:allow - pgx Scan requires destination addresses.
			&item.Kind,             // ptrext:allow - pgx Scan requires destination addresses.
			&item.Value,            // ptrext:allow - pgx Scan requires destination addresses.
			&item.Source,           // ptrext:allow - pgx Scan requires destination addresses.
			&item.Confidence,       // ptrext:allow - pgx Scan requires destination addresses.
			&item.EvidenceCount,    // ptrext:allow - pgx Scan requires destination addresses.
			&item.FirstFeedbackID,  // ptrext:allow - pgx Scan requires destination addresses.
			&item.LatestFeedbackID, // ptrext:allow - pgx Scan requires destination addresses.
			&item.Revoked,          // ptrext:allow - pgx Scan requires destination addresses.
			&item.RevokedAt,        // ptrext:allow - pgx Scan requires destination addresses.
			&item.CreatedAt,        // ptrext:allow - pgx Scan requires destination addresses.
			&item.UpdatedAt,        // ptrext:allow - pgx Scan requires destination addresses.
		)
		if err != nil {
			return nil, fmt.Errorf("scan signal subject identity: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate signal subject identities: %w", err)
	}
	return items, nil
}

func (r *Repo) subjectEvents(
	ctx context.Context,
	tenantID string,
	subjectID uuid.UUID,
	limit int,
) ([]SubjectEvent, error) {
	const q = `
		SELECT id, action, identity_kind, identity_value, feedback_ids,
		       evidence_count, note, created_by, created_at
		FROM signal_subject_merge_events
		WHERE tenant_id = $1 AND subject_id = $2
		ORDER BY created_at DESC, id DESC
		LIMIT $3`
	rows, err := r.pool.Query(ctx, q, tenantID, subjectID, boundedSubjectEventLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("query signal subject events: %w", err)
	}
	defer rows.Close()
	items := make([]SubjectEvent, 0)
	for rows.Next() {
		var item SubjectEvent
		err := rows.Scan(
			&item.ID,            // ptrext:allow - pgx Scan requires destination addresses.
			&item.Action,        // ptrext:allow - pgx Scan requires destination addresses.
			&item.IdentityKind,  // ptrext:allow - pgx Scan requires destination addresses.
			&item.IdentityValue, // ptrext:allow - pgx Scan requires destination addresses.
			&item.FeedbackIDs,   // ptrext:allow - pgx Scan requires destination addresses.
			&item.EvidenceCount, // ptrext:allow - pgx Scan requires destination addresses.
			&item.Note,          // ptrext:allow - pgx Scan requires destination addresses.
			&item.CreatedBy,     // ptrext:allow - pgx Scan requires destination addresses.
			&item.CreatedAt,     // ptrext:allow - pgx Scan requires destination addresses.
		)
		if err != nil {
			return nil, fmt.Errorf("scan signal subject event: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate signal subject events: %w", err)
	}
	return items, nil
}

func (r *Repo) attachSubjectEventEvidence(
	ctx context.Context,
	tenantID string,
	events []SubjectEvent,
) ([]SubjectEvent, error) {
	ids := subjectEventEvidenceIDs(events)
	if len(ids) == 0 {
		return events, nil
	}
	byID, err := r.subjectEventEvidenceRows(ctx, tenantID, ids)
	if err != nil {
		return nil, err
	}
	for i := range events {
		for _, id := range limitedPositiveIDs(events[i].FeedbackIDs, boundedSubjectEventEvidenceLimit(0)) {
			if evidence, ok := byID[id]; ok {
				events[i].Evidence = append(events[i].Evidence, evidence)
			}
		}
	}
	return events, nil
}

func subjectEventEvidenceIDs(events []SubjectEvent) []int64 {
	seen := make(map[int64]struct{})
	ids := make([]int64, 0)
	for _, event := range events {
		for _, id := range limitedPositiveIDs(event.FeedbackIDs, boundedSubjectEventEvidenceLimit(0)) {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

func (r *Repo) subjectEventEvidenceRows(
	ctx context.Context,
	tenantID string,
	ids []int64,
) (map[int64]SubjectEventEvidence, error) {
	const q = `
		SELECT id, source, user_id, content, source_meta, created_at
		FROM user_feedback
		WHERE tenant_id = $1 AND id = ANY($2)`
	rows, err := r.pool.Query(ctx, q, tenantID, ids)
	if err != nil {
		return nil, fmt.Errorf("query signal subject event evidence: %w", err)
	}
	defer rows.Close()
	out := make(map[int64]SubjectEventEvidence, len(ids))
	for rows.Next() {
		var item SubjectEventEvidence
		err := rows.Scan(
			&item.ID,         // ptrext:allow - pgx Scan requires destination addresses.
			&item.Source,     // ptrext:allow - pgx Scan requires destination addresses.
			&item.UserID,     // ptrext:allow - pgx Scan requires destination addresses.
			&item.Content,    // ptrext:allow - pgx Scan requires destination addresses.
			&item.SourceMeta, // ptrext:allow - pgx Scan requires destination addresses.
			&item.CreatedAt,  // ptrext:allow - pgx Scan requires destination addresses.
		)
		if err != nil {
			return nil, fmt.Errorf("scan signal subject event evidence: %w", err)
		}
		out[item.ID] = item
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate signal subject event evidence: %w", err)
	}
	return out, nil
}

func (r *Repo) feedbackEvidenceStatsTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	feedbackIDs []int64,
) (feedbackEvidenceStats, error) {
	const q = `
		WITH requested AS (
			SELECT unnest($2::bigint[]) AS id
		)
		SELECT
			COUNT(DISTINCT uf.id),
			(array_agg(uf.id ORDER BY uf.created_at ASC, uf.id ASC))[1],
			(array_agg(uf.id ORDER BY uf.created_at DESC, uf.id DESC))[1]
		FROM requested req
		JOIN user_feedback uf ON uf.tenant_id = $1 AND uf.id = req.id`
	var stats feedbackEvidenceStats
	err := tx.QueryRow(ctx, q, tenantID, feedbackIDs).Scan(
		&stats.Count,            // ptrext:allow - pgx Scan requires destination addresses.
		&stats.FirstFeedbackID,  // ptrext:allow - pgx Scan requires destination addresses.
		&stats.LatestFeedbackID, // ptrext:allow - pgx Scan requires destination addresses.
	)
	if err != nil {
		return feedbackEvidenceStats{}, fmt.Errorf("query signal graph feedback evidence: %w", err)
	}
	return stats, nil
}

func (r *Repo) subjectForIdentityTx(
	ctx context.Context,
	tx pgx.Tx,
	in MergeIdentityReviewInput,
) (Subject, bool, error) {
	subject, err := r.findSubjectByIdentityTx(ctx, tx, in.TenantID, in.IdentityKind, in.IdentityValueNormalized)
	if err == nil {
		if err := r.touchSubjectTx(ctx, tx, in.TenantID, subject.ID, in.ActorID); err != nil {
			return Subject{}, false, err
		}
		return subject, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Subject{}, false, err
	}
	created, err := r.createSubjectTx(ctx, tx, in)
	if err != nil {
		return Subject{}, false, err
	}
	return created, true, nil
}

func (r *Repo) findSubjectByIdentityTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	kind string,
	normalizedValue string,
) (Subject, error) {
	const q = `
		SELECT
			s.id,
			s.tenant_id,
			s.display_name,
			s.primary_identity_kind,
			s.primary_identity_value,
			s.status,
			s.created_at,
			s.updated_at
		FROM signal_subject_identities i
		JOIN signal_subjects s ON s.tenant_id = i.tenant_id AND s.id = i.subject_id
		WHERE i.tenant_id = $1
		  AND i.kind = $2
		  AND i.value_normalized = $3
		  AND i.revoked_at IS NULL
		  AND s.status = 'active'
		LIMIT 1`
	var subject Subject
	err := tx.QueryRow(ctx, q, tenantID, kind, normalizedValue).Scan(
		&subject.ID,                   // ptrext:allow - pgx Scan requires destination addresses.
		&subject.TenantID,             // ptrext:allow - pgx Scan requires destination addresses.
		&subject.DisplayName,          // ptrext:allow - pgx Scan requires destination addresses.
		&subject.PrimaryIdentityKind,  // ptrext:allow - pgx Scan requires destination addresses.
		&subject.PrimaryIdentityValue, // ptrext:allow - pgx Scan requires destination addresses.
		&subject.Status,               // ptrext:allow - pgx Scan requires destination addresses.
		&subject.CreatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
		&subject.UpdatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
	)
	if err != nil {
		return Subject{}, err
	}
	return subject, nil
}

func (r *Repo) createSubjectTx(ctx context.Context, tx pgx.Tx, in MergeIdentityReviewInput) (Subject, error) {
	const q = `
		INSERT INTO signal_subjects (
			tenant_id, display_name, primary_identity_kind, primary_identity_value, created_by, updated_by
		) VALUES ($1, $2, $3, $4, $5, $5)
		RETURNING id, tenant_id, display_name, primary_identity_kind, primary_identity_value, status, created_at, updated_at`
	displayName := in.IdentityValue
	var subject Subject
	err := tx.QueryRow(ctx, q, in.TenantID, displayName, in.IdentityKind, in.IdentityValue, in.ActorID).Scan(
		&subject.ID,                   // ptrext:allow - pgx Scan requires destination addresses.
		&subject.TenantID,             // ptrext:allow - pgx Scan requires destination addresses.
		&subject.DisplayName,          // ptrext:allow - pgx Scan requires destination addresses.
		&subject.PrimaryIdentityKind,  // ptrext:allow - pgx Scan requires destination addresses.
		&subject.PrimaryIdentityValue, // ptrext:allow - pgx Scan requires destination addresses.
		&subject.Status,               // ptrext:allow - pgx Scan requires destination addresses.
		&subject.CreatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
		&subject.UpdatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
	)
	if err != nil {
		return Subject{}, fmt.Errorf("create signal subject: %w", err)
	}
	return subject, nil
}

func (r *Repo) touchSubjectTx(ctx context.Context, tx pgx.Tx, tenantID string, subjectID uuid.UUID, actorID string) error {
	const q = `
		UPDATE signal_subjects
		SET updated_by = $3
		WHERE tenant_id = $1 AND id = $2 AND status = 'active'`
	if _, err := tx.Exec(ctx, q, tenantID, subjectID, actorID); err != nil {
		return fmt.Errorf("touch signal subject: %w", err)
	}
	return nil
}

func (r *Repo) upsertIdentityTx(
	ctx context.Context,
	tx pgx.Tx,
	in MergeIdentityReviewInput,
	subjectID uuid.UUID,
	stats feedbackEvidenceStats,
) error {
	const q = `
		INSERT INTO signal_subject_identities (
			tenant_id, subject_id, kind, value, value_normalized, source, confidence,
			first_feedback_id, latest_feedback_id, evidence_count, created_by, updated_by
		) VALUES ($1, $2, $3, $4, $5, 'review', 'reviewed', $6, $7, $8, $9, $9)
		ON CONFLICT (tenant_id, kind, value_normalized) WHERE revoked_at IS NULL DO UPDATE SET
			latest_feedback_id = EXCLUDED.latest_feedback_id,
			evidence_count = GREATEST(signal_subject_identities.evidence_count, EXCLUDED.evidence_count),
			updated_by = EXCLUDED.updated_by
		WHERE signal_subject_identities.subject_id = EXCLUDED.subject_id
		RETURNING subject_id`
	var returnedSubjectID uuid.UUID
	err := tx.QueryRow(
		ctx,
		q,
		in.TenantID,
		subjectID,
		in.IdentityKind,
		in.IdentityValue,
		in.IdentityValueNormalized,
		nullableInt64(stats.FirstFeedbackID),
		nullableInt64(stats.LatestFeedbackID),
		stats.Count,
		in.ActorID,
	).Scan(&returnedSubjectID) // ptrext:allow - pgx Scan requires destination addresses.
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrConflict
	}
	if err != nil {
		return fmt.Errorf("upsert signal subject identity: %w", err)
	}
	return nil
}

func (r *Repo) insertMergeEventTx(
	ctx context.Context,
	tx pgx.Tx,
	in MergeIdentityReviewInput,
	subjectID uuid.UUID,
	evidenceCount int,
) error {
	const q = `
		INSERT INTO signal_subject_merge_events (
			tenant_id, subject_id, action, identity_kind, identity_value, identity_value_normalized,
			feedback_ids, evidence_count, note, created_by
		) VALUES ($1, $2, 'review_merge', $3, $4, $5, $6, $7, $8, $9)`
	if _, err := tx.Exec(
		ctx,
		q,
		in.TenantID,
		subjectID,
		in.IdentityKind,
		in.IdentityValue,
		in.IdentityValueNormalized,
		in.FeedbackIDs,
		evidenceCount,
		in.Note,
		in.ActorID,
	); err != nil {
		return fmt.Errorf("insert signal subject merge event: %w", err)
	}
	return nil
}

func (r *Repo) revokeIdentityTx(ctx context.Context, tx pgx.Tx, in SplitIdentityReviewInput) (activeIdentity, error) {
	const q = `
		WITH latest_merge AS (
			SELECT feedback_ids
			FROM signal_subject_merge_events
			WHERE tenant_id = $1
			  AND subject_id = $2
			  AND identity_kind = $3
			  AND identity_value_normalized = $4
			  AND action = 'review_merge'
			ORDER BY created_at DESC, id DESC
			LIMIT 1
		),
		revoked AS (
			UPDATE signal_subject_identities
			SET revoked_at = NOW(), updated_by = $5
			WHERE tenant_id = $1
			  AND subject_id = $2
			  AND kind = $3
			  AND value_normalized = $4
			  AND revoked_at IS NULL
			RETURNING value, evidence_count
		)
		SELECT revoked.value, revoked.evidence_count, COALESCE(latest_merge.feedback_ids, '{}'::bigint[])
		FROM revoked
		LEFT JOIN latest_merge ON true`
	var identity activeIdentity
	err := tx.QueryRow(
		ctx,
		q,
		in.TenantID,
		in.SubjectID,
		in.IdentityKind,
		in.IdentityValueNormalized,
		in.ActorID,
	).Scan(
		&identity.Value,         // ptrext:allow - pgx Scan requires destination addresses.
		&identity.EvidenceCount, // ptrext:allow - pgx Scan requires destination addresses.
		&identity.FeedbackIDs,   // ptrext:allow - pgx Scan requires destination addresses.
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return activeIdentity{}, ErrIdentityNotFound
	}
	if err != nil {
		return activeIdentity{}, fmt.Errorf("revoke signal subject identity: %w", err)
	}
	return identity, nil
}

func (r *Repo) insertSplitEventTx(
	ctx context.Context,
	tx pgx.Tx,
	in SplitIdentityReviewInput,
	identity activeIdentity,
) error {
	const q = `
		INSERT INTO signal_subject_merge_events (
			tenant_id, subject_id, action, identity_kind, identity_value, identity_value_normalized,
			feedback_ids, evidence_count, note, created_by
		) VALUES ($1, $2, 'split', $3, $4, $5, $6, $7, $8, $9)`
	value := in.IdentityValue
	if value == "" {
		value = identity.Value
	}
	if _, err := tx.Exec(
		ctx,
		q,
		in.TenantID,
		in.SubjectID,
		in.IdentityKind,
		value,
		in.IdentityValueNormalized,
		identity.FeedbackIDs,
		identity.EvidenceCount,
		in.Note,
		in.ActorID,
	); err != nil {
		return fmt.Errorf("insert signal subject split event: %w", err)
	}
	return nil
}

func (r *Repo) refreshSubjectPrimaryIdentityTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	subjectID uuid.UUID,
	actorID string,
) error {
	const q = `
		WITH next_identity AS (
			SELECT kind, value
			FROM signal_subject_identities
			WHERE tenant_id = $1
			  AND subject_id = $2
			  AND revoked_at IS NULL
			ORDER BY evidence_count DESC, updated_at DESC, id DESC
			LIMIT 1
		)
		UPDATE signal_subjects s
		SET
			primary_identity_kind = COALESCE((SELECT kind FROM next_identity), ''),
			primary_identity_value = COALESCE((SELECT value FROM next_identity), ''),
			updated_by = $3
		WHERE s.tenant_id = $1 AND s.id = $2 AND s.status = 'active'`
	if _, err := tx.Exec(ctx, q, tenantID, subjectID, actorID); err != nil {
		return fmt.Errorf("refresh signal subject primary identity: %w", err)
	}
	return nil
}

func (r *Repo) subjectSummaryTx(ctx context.Context, tx pgx.Tx, tenantID string, subjectID uuid.UUID) (Subject, error) {
	const q = `
		SELECT
			s.id,
			s.tenant_id,
			s.display_name,
			s.primary_identity_kind,
			s.primary_identity_value,
			s.status,
			COUNT(i.id),
			COALESCE(SUM(i.evidence_count), 0),
			s.created_at,
			s.updated_at
		FROM signal_subjects s
		LEFT JOIN signal_subject_identities i
		  ON i.tenant_id = s.tenant_id
		 AND i.subject_id = s.id
		 AND i.revoked_at IS NULL
		WHERE s.tenant_id = $1 AND s.id = $2
		GROUP BY s.id, s.tenant_id, s.display_name, s.primary_identity_kind,
		         s.primary_identity_value, s.status, s.created_at, s.updated_at`
	var subject Subject
	err := tx.QueryRow(ctx, q, tenantID, subjectID).Scan(
		&subject.ID,                   // ptrext:allow - pgx Scan requires destination addresses.
		&subject.TenantID,             // ptrext:allow - pgx Scan requires destination addresses.
		&subject.DisplayName,          // ptrext:allow - pgx Scan requires destination addresses.
		&subject.PrimaryIdentityKind,  // ptrext:allow - pgx Scan requires destination addresses.
		&subject.PrimaryIdentityValue, // ptrext:allow - pgx Scan requires destination addresses.
		&subject.Status,               // ptrext:allow - pgx Scan requires destination addresses.
		&subject.IdentityCount,        // ptrext:allow - pgx Scan requires destination addresses.
		&subject.EvidenceCount,        // ptrext:allow - pgx Scan requires destination addresses.
		&subject.CreatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
		&subject.UpdatedAt,            // ptrext:allow - pgx Scan requires destination addresses.
	)
	if err != nil {
		return Subject{}, fmt.Errorf("query signal subject summary: %w", err)
	}
	return subject, nil
}

func boundedRecentMergeLimit(limit int) int {
	if limit <= 0 {
		return 10
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func boundedSubjectRosterLimit(limit int) int {
	if limit <= 0 {
		return 6
	}
	if limit > 50 {
		return 50
	}
	return limit
}

func boundedSubjectEventLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func boundedSubjectEventEvidenceLimit(limit int) int {
	if limit <= 0 {
		return 5
	}
	if limit > 10 {
		return 10
	}
	return limit
}

func limitedPositiveIDs(ids []int64, limit int) []int64 {
	if len(ids) == 0 || limit <= 0 {
		return nil
	}
	out := make([]int64, 0, limit)
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		out = append(out, id)
		if len(out) == limit {
			break
		}
	}
	return out
}

func nullableInt64(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}

func NormalizeIdentityValue(kind string, value string) string {
	trimmed := strings.TrimSpace(value)
	if kind == "email" {
		return strings.ToLower(trimmed)
	}
	return trimmed
}
