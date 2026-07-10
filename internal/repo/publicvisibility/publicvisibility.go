// SPDX-License-Identifier: Apache-2.0

// Package publicvisibility owns public visibility policy, moderation state, and
// public-safe request projections.
package publicvisibility

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

type AccessMode string

const (
	AccessModeDisabled      AccessMode = "disabled"
	AccessModePublic        AccessMode = "public"
	AccessModeAuthenticated AccessMode = "authenticated"
	AccessModeInviteOnly    AccessMode = "invite_only"
)

type WriteMode string

const (
	WriteModeDisabled   WriteMode = "disabled"
	WriteModeAnonymous  WriteMode = "anonymous"
	WriteModeIdentified WriteMode = "identified"
)

type Surface string

const (
	SurfaceRequest          Surface = "request"
	SurfaceRequestComment   Surface = "request_comment"
	SurfaceRoadmapItem      Surface = "roadmap_item"
	SurfaceChangelogPost    Surface = "changelog_post"
	SurfacePortalSubmission Surface = "portal_submission"
)

type ModerationState string

const (
	ModerationStatePending  ModerationState = "pending"
	ModerationStateApproved ModerationState = "approved"
	ModerationStateRejected ModerationState = "rejected"
	ModerationStateHidden   ModerationState = "hidden"
	ModerationStateSpam     ModerationState = "spam"
)

type IdentityMode string

const (
	IdentityModeAnonymous    IdentityMode = "anonymous"
	IdentityModeDisplayName  IdentityMode = "display_name"
	IdentityModeOrganization IdentityMode = "organization"
)

var (
	ErrNotFound     = errors.New("public visibility not found")
	ErrInvalidInput = errors.New("public visibility invalid input")
)

type Policy struct {
	TenantID              string
	PortalAccessMode      AccessMode
	SearchIndexingEnabled bool
	RequestsEnabled       bool
	CommentsEnabled       bool
	RoadmapEnabled        bool
	ChangelogEnabled      bool
	SubmissionWriteMode   WriteMode
	CommentWriteMode      WriteMode
	VoteWriteMode         WriteMode
	DefaultRequestState   ModerationState
	DefaultCommentState   ModerationState
	SubmitterIdentityMode IdentityMode
	ShowVoteCount         bool
	ShowCommentCount      bool
	ShowSubmitterDisplay  bool
	HidePublicTimestamps  bool
	UpdatedBy             string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type ModerationSubject struct {
	ID                     uuid.UUID
	TenantID               string
	Surface                Surface
	SubjectID              string
	State                  ModerationState
	ReasonCode             string
	ReasonNote             string
	SubmittedByDisplay     string
	SubmittedByFingerprint string
	ReviewedBy             string
	ReviewedAt             *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type RequestProfile struct {
	ID                uuid.UUID
	TenantID          string
	RequestID         uuid.UUID
	PublicSlug        string
	PublicTitle       string
	PublicSummary     string
	PublicState       string
	RoadmapColumn     string
	IncludedInPortal  bool
	IncludedInRoadmap bool
	PublishedAt       *time.Time
	UpdatedBy         string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type PublicRequestCandidate struct {
	Policy              Policy
	Profile             RequestProfile
	Moderation          ModerationSubject
	VoteCount           int
	CommentCount        int
	SubmitterDisplay    string
	CustomerRequestID   uuid.UUID
	CustomerRequestLive bool
}

type RequestPublication struct {
	Profile    RequestProfile
	Moderation ModerationSubject
}

type ListFilter struct {
	TenantID string
	Surfaces []Surface
	States   []ModerationState
	Limit    int
	Cursor   string
}

type ListResult struct {
	Items      []ModerationSubject
	NextCursor string
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

func (r *Repo) GetPolicy(ctx context.Context, tenantID string) (*Policy, error) {
	return loadPolicy(ctx, r.pool, tenantID)
}

func (r *Repo) ResolveTenantIDBySlug(ctx context.Context, slug string) (string, error) {
	var tenantID string
	err := r.pool.QueryRow(ctx, `
		SELECT id
		FROM tenants
		WHERE slug = $1
		  AND is_active = TRUE
		LIMIT 1`, slug).Scan(&tenantID) // ptrext:allow scan-target
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return tenantID, err
}

func (r *Repo) UpsertPolicyTx(ctx context.Context, tx pgx.Tx, policy Policy) (*Policy, error) {
	q := `
		INSERT INTO public_visibility_policies (
			tenant_id, portal_access_mode, search_indexing_enabled,
			requests_enabled, comments_enabled, roadmap_enabled, changelog_enabled,
			submission_write_mode, comment_write_mode, vote_write_mode,
			default_request_state, default_comment_state, submitter_identity_mode,
			show_vote_count, show_comment_count, show_submitter_display,
			hide_public_timestamps, updated_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18
		)
		ON CONFLICT (tenant_id) DO UPDATE SET
			portal_access_mode = EXCLUDED.portal_access_mode,
			search_indexing_enabled = EXCLUDED.search_indexing_enabled,
			requests_enabled = EXCLUDED.requests_enabled,
			comments_enabled = EXCLUDED.comments_enabled,
			roadmap_enabled = EXCLUDED.roadmap_enabled,
			changelog_enabled = EXCLUDED.changelog_enabled,
			submission_write_mode = EXCLUDED.submission_write_mode,
			comment_write_mode = EXCLUDED.comment_write_mode,
			vote_write_mode = EXCLUDED.vote_write_mode,
			default_request_state = EXCLUDED.default_request_state,
			default_comment_state = EXCLUDED.default_comment_state,
			submitter_identity_mode = EXCLUDED.submitter_identity_mode,
			show_vote_count = EXCLUDED.show_vote_count,
			show_comment_count = EXCLUDED.show_comment_count,
			show_submitter_display = EXCLUDED.show_submitter_display,
			hide_public_timestamps = EXCLUDED.hide_public_timestamps,
			updated_by = EXCLUDED.updated_by
		RETURNING ` + policyColumns()
	policyOut, err := scanPolicy(tx.QueryRow(ctx, q,
		policy.TenantID,
		policy.PortalAccessMode,
		policy.SearchIndexingEnabled,
		policy.RequestsEnabled,
		policy.CommentsEnabled,
		policy.RoadmapEnabled,
		policy.ChangelogEnabled,
		policy.SubmissionWriteMode,
		policy.CommentWriteMode,
		policy.VoteWriteMode,
		policy.DefaultRequestState,
		policy.DefaultCommentState,
		policy.SubmitterIdentityMode,
		policy.ShowVoteCount,
		policy.ShowCommentCount,
		policy.ShowSubmitterDisplay,
		policy.HidePublicTimestamps,
		policy.UpdatedBy,
	))
	if err != nil {
		return nil, mapWriteError(err)
	}
	return policyOut, nil
}

func (r *Repo) ListSubjects(ctx context.Context, filter ListFilter) (ListResult, error) {
	limit := boundedLimit(filter.Limit)
	offset, err := parseCursor(filter.Cursor)
	if err != nil {
		return ListResult{}, err
	}
	args := []any{filter.TenantID}
	clauses := []string{"tenant_id = $1"}
	if len(filter.Surfaces) > 0 {
		values := make([]string, 0, len(filter.Surfaces))
		for _, surface := range filter.Surfaces {
			values = append(values, string(surface))
		}
		args = append(args, values)
		clauses = append(clauses, fmt.Sprintf("surface = ANY($%d)", len(args)))
	}
	if len(filter.States) > 0 {
		values := make([]string, 0, len(filter.States))
		for _, state := range filter.States {
			values = append(values, string(state))
		}
		args = append(args, values)
		clauses = append(clauses, fmt.Sprintf("state = ANY($%d)", len(args)))
	}
	args = append(args, limit+1, offset)
	q := `SELECT ` + subjectColumns() + `
		FROM public_moderation_subjects
		WHERE ` + strings.Join(clauses, " AND ") + `
		ORDER BY created_at DESC, id DESC
		LIMIT $` + strconv.Itoa(len(args)-1) + ` OFFSET $` + strconv.Itoa(len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()
	items, err := scanSubjects(rows)
	if err != nil {
		return ListResult{}, err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = strconv.Itoa(offset + limit)
	}
	return ListResult{Items: items, NextCursor: next}, nil
}

func (r *Repo) GetSubject(ctx context.Context, tenantID string, id uuid.UUID) (*ModerationSubject, error) {
	return loadSubject(ctx, r.pool, tenantID, id, false)
}

func (r *Repo) GetSubjectForUpdateTx(ctx context.Context, tx pgx.Tx, tenantID string, id uuid.UUID) (*ModerationSubject, error) {
	return loadSubject(ctx, tx, tenantID, id, true)
}

func (r *Repo) UpdateSubjectStateTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	id uuid.UUID,
	state ModerationState,
	reasonCode string,
	reasonNote string,
	reviewedBy string,
	reviewedAt time.Time,
) (*ModerationSubject, error) {
	q := `
		UPDATE public_moderation_subjects
		SET state = $3,
		    reason_code = $4,
		    reason_note = $5,
		    reviewed_by = $6,
		    reviewed_at = $7
		WHERE tenant_id = $1 AND id = $2
		RETURNING ` + subjectColumns()
	subject, err := scanSubject(tx.QueryRow(ctx, q, tenantID, id, state, reasonCode, reasonNote, reviewedBy, reviewedAt))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return subject, err
}

func (r *Repo) CreateModerationSubjectTx(ctx context.Context, tx pgx.Tx, subject ModerationSubject) (*ModerationSubject, error) {
	q := `
		INSERT INTO public_moderation_subjects (
			tenant_id, surface, subject_id, state, submitted_by_display,
			submitted_by_fingerprint
		) VALUES (
			$1, $2, $3, $4, $5, $6
		)
		RETURNING ` + subjectColumns()
	saved, err := scanSubject(tx.QueryRow(ctx, q,
		subject.TenantID,
		subject.Surface,
		subject.SubjectID,
		subject.State,
		subject.SubmittedByDisplay,
		subject.SubmittedByFingerprint,
	))
	if err != nil {
		return nil, mapWriteError(err)
	}
	return saved, nil
}

func (r *Repo) GetRequestPublication(ctx context.Context, tenantID string, requestID uuid.UUID) (*RequestPublication, error) {
	q := `
		SELECT
			` + prefixedProfileColumns("prp") + `,
			` + prefixedSubjectColumns("pms") + `
		FROM public_request_profiles prp
		JOIN public_moderation_subjects pms
		  ON pms.tenant_id = prp.tenant_id
		 AND pms.surface = 'request'
		 AND pms.subject_id = prp.id::text
		WHERE prp.tenant_id = $1
		  AND prp.request_id = $2
		LIMIT 1`
	var out RequestPublication
	targets := profileScanTargets(&out.Profile)                       // ptrext:allow scan-target
	targets = append(targets, subjectScanTargets(&out.Moderation)...) // ptrext:allow scan-target
	err := r.pool.QueryRow(ctx, q, tenantID, requestID).Scan(targets...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return ptrext.Of(out), nil
}

func (r *Repo) UpsertRequestPublicationTx(
	ctx context.Context,
	tx pgx.Tx,
	profile RequestProfile,
	defaultState ModerationState,
	submittedByDisplay string,
	submittedByFingerprint string,
) (*RequestPublication, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM customer_requests
			WHERE tenant_id = $1
			  AND id = $2
			  AND archived_at IS NULL
			  AND merged_into_request_id IS NULL
		)`, profile.TenantID, profile.RequestID).Scan(&exists); err != nil { // ptrext:allow scan-target
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}

	upsertProfile := `
		INSERT INTO public_request_profiles (
			tenant_id, request_id, public_slug, public_title, public_summary,
			public_state, roadmap_column, included_in_portal, included_in_roadmap,
			published_at, updated_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9,
			CASE WHEN $8 OR $9 THEN NOW() ELSE NULL END,
			$10
		)
		ON CONFLICT (tenant_id, request_id) DO UPDATE SET
			public_slug = EXCLUDED.public_slug,
			public_title = EXCLUDED.public_title,
			public_summary = EXCLUDED.public_summary,
			public_state = EXCLUDED.public_state,
			roadmap_column = EXCLUDED.roadmap_column,
			included_in_portal = EXCLUDED.included_in_portal,
			included_in_roadmap = EXCLUDED.included_in_roadmap,
			published_at = CASE
				WHEN EXCLUDED.included_in_portal OR EXCLUDED.included_in_roadmap
					THEN COALESCE(public_request_profiles.published_at, NOW())
				ELSE NULL
			END,
			updated_by = EXCLUDED.updated_by
		RETURNING ` + profileColumns()
	savedProfile, err := scanProfile(tx.QueryRow(ctx, upsertProfile,
		profile.TenantID,
		profile.RequestID,
		profile.PublicSlug,
		profile.PublicTitle,
		profile.PublicSummary,
		profile.PublicState,
		profile.RoadmapColumn,
		profile.IncludedInPortal,
		profile.IncludedInRoadmap,
		profile.UpdatedBy,
	))
	if err != nil {
		return nil, mapWriteError(err)
	}

	upsertSubject := `
		INSERT INTO public_moderation_subjects (
			tenant_id, surface, subject_id, state, submitted_by_display,
			submitted_by_fingerprint
		) VALUES (
			$1, 'request', $2, $3, $4, $5
		)
		ON CONFLICT (tenant_id, surface, subject_id) DO UPDATE SET
			submitted_by_display = EXCLUDED.submitted_by_display,
			submitted_by_fingerprint = EXCLUDED.submitted_by_fingerprint
		RETURNING ` + subjectColumns()
	subject, err := scanSubject(tx.QueryRow(ctx, upsertSubject,
		savedProfile.TenantID,
		savedProfile.ID.String(),
		defaultState,
		submittedByDisplay,
		submittedByFingerprint,
	))
	if err != nil {
		return nil, mapWriteError(err)
	}
	return ptrext.Of(RequestPublication{
		Profile:    ptrext.Indirect(savedProfile),
		Moderation: ptrext.Indirect(subject),
	}), nil
}

func (r *Repo) GetPublicRequestCandidate(ctx context.Context, tenantSlug string, publicSlug string) (*PublicRequestCandidate, error) {
	q := `
		SELECT
			` + prefixedPolicyColumns("pol") + `,
			` + prefixedProfileColumns("prp") + `,
			` + prefixedSubjectColumns("pms") + `,
			COALESCE(votes.vote_count, 0),
			0::bigint AS comment_count,
			cr.id,
			(cr.archived_at IS NULL AND cr.merged_into_request_id IS NULL) AS request_live
		FROM tenants t
		JOIN public_visibility_policies pol ON pol.tenant_id = t.id
		JOIN public_request_profiles prp ON prp.tenant_id = t.id
		JOIN public_moderation_subjects pms
		  ON pms.tenant_id = prp.tenant_id
		 AND pms.surface = 'request'
		 AND pms.subject_id = prp.id::text
		JOIN customer_requests cr
		  ON cr.tenant_id = prp.tenant_id
		 AND cr.id = prp.request_id
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::bigint AS vote_count
			FROM customer_request_votes v
			WHERE v.tenant_id = prp.tenant_id
			  AND v.request_id = prp.request_id
		) votes ON true
		WHERE t.slug = $1
		  AND t.is_active = TRUE
		  AND prp.public_slug = $2
		LIMIT 1`
	var out PublicRequestCandidate
	var votes int64
	var comments int64
	targets := policyScanTargets(&out.Policy)                         // ptrext:allow scan-target
	targets = append(targets, profileScanTargets(&out.Profile)...)    // ptrext:allow scan-target
	targets = append(targets, subjectScanTargets(&out.Moderation)...) // ptrext:allow scan-target
	targets = append(targets,
		&votes, &comments, &out.CustomerRequestID, &out.CustomerRequestLive, // ptrext:allow scan-target
	)
	err := r.pool.QueryRow(ctx, q, tenantSlug, publicSlug).Scan(targets...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.VoteCount = int(votes)
	out.CommentCount = int(comments)
	out.SubmitterDisplay = out.Moderation.SubmittedByDisplay
	return ptrext.Of(out), nil
}

type queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type rowScanner interface {
	Scan(dest ...any) error
}

func loadPolicy(ctx context.Context, db queryer, tenantID string) (*Policy, error) {
	q := `SELECT ` + policyColumns() + ` FROM public_visibility_policies WHERE tenant_id = $1`
	policy, err := scanPolicy(db.QueryRow(ctx, q, tenantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return policy, err
}

func loadSubject(ctx context.Context, db queryer, tenantID string, id uuid.UUID, forUpdate bool) (*ModerationSubject, error) {
	q := `SELECT ` + subjectColumns() + ` FROM public_moderation_subjects WHERE tenant_id = $1 AND id = $2`
	if forUpdate {
		q += ` FOR UPDATE`
	}
	subject, err := scanSubject(db.QueryRow(ctx, q, tenantID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return subject, err
}

func policyColumns() string {
	return strings.Join([]string{
		"tenant_id",
		"portal_access_mode",
		"search_indexing_enabled",
		"requests_enabled",
		"comments_enabled",
		"roadmap_enabled",
		"changelog_enabled",
		"submission_write_mode",
		"comment_write_mode",
		"vote_write_mode",
		"default_request_state",
		"default_comment_state",
		"submitter_identity_mode",
		"show_vote_count",
		"show_comment_count",
		"show_submitter_display",
		"hide_public_timestamps",
		"updated_by",
		"created_at",
		"updated_at",
	}, ", ")
}

func subjectColumns() string {
	return strings.Join([]string{
		"id",
		"tenant_id",
		"surface",
		"subject_id",
		"state",
		"reason_code",
		"reason_note",
		"submitted_by_display",
		"submitted_by_fingerprint",
		"reviewed_by",
		"reviewed_at",
		"created_at",
		"updated_at",
	}, ", ")
}

func profileColumns() string {
	return strings.Join([]string{
		"id",
		"tenant_id",
		"request_id",
		"public_slug",
		"public_title",
		"public_summary",
		"public_state",
		"roadmap_column",
		"included_in_portal",
		"included_in_roadmap",
		"published_at",
		"updated_by",
		"created_at",
		"updated_at",
	}, ", ")
}

func prefixedPolicyColumns(alias string) string {
	return prefixColumns(alias, policyColumns())
}

func prefixedSubjectColumns(alias string) string {
	return prefixColumns(alias, subjectColumns())
}

func prefixedProfileColumns(alias string) string {
	return prefixColumns(alias, profileColumns())
}

func prefixColumns(alias string, columns string) string {
	parts := strings.Split(columns, ", ")
	for i, part := range parts {
		parts[i] = alias + "." + part
	}
	return strings.Join(parts, ", ")
}

func scanPolicy(row rowScanner) (*Policy, error) {
	var policy Policy
	if err := row.Scan(policyScanTargets(&policy)...); err != nil { // ptrext:allow scan-target
		return nil, err
	}
	return ptrext.Of(policy), nil
}

func policyScanTargets(policy *Policy, extra ...any) []any {
	targets := []any{
		&policy.TenantID, &policy.PortalAccessMode, &policy.SearchIndexingEnabled, // ptrext:allow scan-target
		&policy.RequestsEnabled, &policy.CommentsEnabled, &policy.RoadmapEnabled, // ptrext:allow scan-target
		&policy.ChangelogEnabled, &policy.SubmissionWriteMode, &policy.CommentWriteMode, // ptrext:allow scan-target
		&policy.VoteWriteMode, &policy.DefaultRequestState, &policy.DefaultCommentState, // ptrext:allow scan-target
		&policy.SubmitterIdentityMode, &policy.ShowVoteCount, &policy.ShowCommentCount, // ptrext:allow scan-target
		&policy.ShowSubmitterDisplay, &policy.HidePublicTimestamps, &policy.UpdatedBy, // ptrext:allow scan-target
		&policy.CreatedAt, &policy.UpdatedAt, // ptrext:allow scan-target
	}
	return append(targets, extra...)
}

func scanSubject(row rowScanner) (*ModerationSubject, error) {
	var subject ModerationSubject
	if err := row.Scan(subjectScanTargets(&subject)...); err != nil { // ptrext:allow scan-target
		return nil, err
	}
	return ptrext.Of(subject), nil
}

func subjectScanTargets(subject *ModerationSubject, extra ...any) []any {
	targets := []any{
		&subject.ID, &subject.TenantID, &subject.Surface, &subject.SubjectID, // ptrext:allow scan-target
		&subject.State, &subject.ReasonCode, &subject.ReasonNote, // ptrext:allow scan-target
		&subject.SubmittedByDisplay, &subject.SubmittedByFingerprint, &subject.ReviewedBy, // ptrext:allow scan-target
		&subject.ReviewedAt, &subject.CreatedAt, &subject.UpdatedAt, // ptrext:allow scan-target
	}
	return append(targets, extra...)
}

func scanProfile(row rowScanner) (*RequestProfile, error) {
	var profile RequestProfile
	if err := row.Scan(profileScanTargets(&profile)...); err != nil { // ptrext:allow scan-target
		return nil, err
	}
	return ptrext.Of(profile), nil
}

func profileScanTargets(profile *RequestProfile, extra ...any) []any {
	targets := []any{
		&profile.ID, &profile.TenantID, &profile.RequestID, &profile.PublicSlug, // ptrext:allow scan-target
		&profile.PublicTitle, &profile.PublicSummary, &profile.PublicState, // ptrext:allow scan-target
		&profile.RoadmapColumn, &profile.IncludedInPortal, &profile.IncludedInRoadmap, // ptrext:allow scan-target
		&profile.PublishedAt, &profile.UpdatedBy, &profile.CreatedAt, &profile.UpdatedAt, // ptrext:allow scan-target
	}
	return append(targets, extra...)
}

func scanSubjects(rows pgx.Rows) ([]ModerationSubject, error) {
	var out []ModerationSubject
	for rows.Next() {
		var subject ModerationSubject
		if err := rows.Scan(subjectScanTargets(&subject)...); err != nil { // ptrext:allow scan-target
			return nil, err
		}
		out = append(out, subject)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func boundedLimit(limit int) int {
	if limit <= 0 {
		return 50
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func parseCursor(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(trimmed)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("%w: invalid cursor", ErrInvalidInput)
	}
	return offset, nil
}

func mapWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505", "23514", "23503":
			return fmt.Errorf("%w: %s", ErrInvalidInput, pgErr.ConstraintName)
		}
	}
	return err
}
