// SPDX-License-Identifier: Apache-2.0

// Package publicvisibility owns public visibility policy, moderation state, and
// public-safe request projections.
package publicvisibility

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

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
	PortalSubmissionForm  PortalSubmissionForm
	UpdatedBy             string
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type PortalSubmissionFieldKind string

const (
	PortalSubmissionFieldKindText        PortalSubmissionFieldKind = "text"
	PortalSubmissionFieldKindTextarea    PortalSubmissionFieldKind = "textarea"
	PortalSubmissionFieldKindSelect      PortalSubmissionFieldKind = "select"
	PortalSubmissionFieldKindMultiSelect PortalSubmissionFieldKind = "multiselect"
	PortalSubmissionFieldKindBoolean     PortalSubmissionFieldKind = "boolean"
)

type PortalSubmissionField struct {
	Key         string                    `json:"key"`
	Label       string                    `json:"label"`
	Kind        PortalSubmissionFieldKind `json:"kind"`
	Required    bool                      `json:"required"`
	Options     []string                  `json:"options,omitempty"`
	Placeholder string                    `json:"placeholder,omitempty"`
}

type PortalSubmissionForm struct {
	Headline          string                  `json:"headline"`
	Description       string                  `json:"description"`
	Acknowledgement   string                  `json:"acknowledgement"`
	SubmitButtonLabel string                  `json:"submit_button_label"`
	ShowPageURL       bool                    `json:"show_page_url"`
	Fields            []PortalSubmissionField `json:"fields,omitempty"`
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
	ViewerHasVoted      bool
}

type PublicRequestListFilter struct {
	TenantSlug        string
	Roadmap           bool
	Query             string
	SimilarityText    string
	ExcludePublicSlug string
	Sort              string
	State             string
	RoadmapColumn     string
	OnlyVotedByViewer bool
	OnlyWithComments  bool
	Limit             int
	Cursor            string
	ViewerSubjectKey  string
}

type PublicRequestListCandidate struct {
	Profile           RequestProfile
	Moderation        ModerationSubject
	VoteCount         int
	CommentCount      int
	SubmitterDisplay  string
	CustomerRequestID uuid.UUID
	ViewerHasVoted    bool
}

type PublicRequestComment struct {
	ID                 uuid.UUID
	Body               string
	SubmittedByDisplay string
	State              ModerationState
	CreatedAt          time.Time
}

type PublicRequestListResult struct {
	Policy     Policy
	Items      []PublicRequestListCandidate
	NextCursor string
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
			hide_public_timestamps, portal_submission_form, updated_by
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19
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
			portal_submission_form = EXCLUDED.portal_submission_form,
			updated_by = EXCLUDED.updated_by
		RETURNING ` + policyColumns()
	formJSON, err := json.Marshal(policy.PortalSubmissionForm)
	if err != nil {
		return nil, fmt.Errorf("marshal portal submission form: %w", err)
	}
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
		formJSON,
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

func (r *Repo) GetPublicRequestCandidate(ctx context.Context, tenantSlug string, publicSlug string, viewerSubjectKey string) (*PublicRequestCandidate, error) {
	q := `
		SELECT
			` + prefixedPolicyColumns("pol") + `,
			` + prefixedProfileColumns("prp") + `,
			` + prefixedSubjectColumns("pms") + `,
			COALESCE(votes.vote_count, 0),
			CASE
			  WHEN $3 <> '' THEN EXISTS(
			    SELECT 1
			    FROM customer_request_votes vv
			    WHERE vv.tenant_id = prp.tenant_id
			      AND vv.request_id = prp.request_id
			      AND vv.subject_key = $3
			  )
			  ELSE FALSE
			END,
			COALESCE(comments.comment_count, 0),
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
			  AND v.subject_key LIKE 'portal:%%'
		) votes ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::bigint AS comment_count
			FROM customer_request_comments c
			JOIN public_moderation_subjects cps
			  ON cps.tenant_id = c.tenant_id
			 AND cps.surface = 'request_comment'
			 AND cps.subject_id = c.id::text
			 AND cps.state = 'approved'
			WHERE c.tenant_id = prp.tenant_id
			  AND c.request_id = prp.request_id
		) comments ON true
		WHERE t.slug = $1
		  AND t.is_active = TRUE
		  AND prp.public_slug = $2
		LIMIT 1`
	var out PublicRequestCandidate
	var votes int64
	var viewerHasVoted bool
	var comments int64
	var formJSON []byte
	targets := policyScanTargets(&out.Policy, &formJSON)              // ptrext:allow scan-target
	targets = append(targets, profileScanTargets(&out.Profile)...)    // ptrext:allow scan-target
	targets = append(targets, subjectScanTargets(&out.Moderation)...) // ptrext:allow scan-target
	targets = append(targets,
		&votes, &viewerHasVoted, &comments, &out.CustomerRequestID, &out.CustomerRequestLive, // ptrext:allow scan-target
	)
	err := r.pool.QueryRow(ctx, q, tenantSlug, publicSlug, viewerSubjectKey).Scan(targets...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	out.VoteCount = int(votes)
	out.CommentCount = int(comments)
	out.ViewerHasVoted = viewerHasVoted
	out.SubmitterDisplay = out.Moderation.SubmittedByDisplay
	return ptrext.Of(out), nil
}

var publicBoardListCandidatesQuery = `
		SELECT
			` + prefixedProfileColumns("prp") + `,
			` + prefixedSubjectColumns("pms") + `,
			COALESCE(votes.vote_count, 0),
			CASE
			  WHEN $4 <> '' THEN EXISTS(
			    SELECT 1
			    FROM customer_request_votes vv
			    WHERE vv.tenant_id = prp.tenant_id
			      AND vv.request_id = prp.request_id
			      AND vv.subject_key = $4
			  )
			  ELSE FALSE
			END,
			COALESCE(comments.comment_count, 0),
			cr.id
		FROM public_request_profiles prp
		JOIN public_moderation_subjects pms
		  ON pms.tenant_id = prp.tenant_id
		 AND pms.surface = 'request'
		 AND pms.subject_id = prp.id::text
		 AND pms.state = 'approved'
		JOIN customer_requests cr
		  ON cr.tenant_id = prp.tenant_id
		 AND cr.id = prp.request_id
		 AND cr.archived_at IS NULL
		 AND cr.merged_into_request_id IS NULL
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::bigint AS vote_count
			FROM customer_request_votes v
			WHERE v.tenant_id = prp.tenant_id
			  AND v.request_id = prp.request_id
			  AND v.subject_key LIKE 'portal:%%'
		) votes ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::bigint AS comment_count
			FROM customer_request_comments c
			JOIN public_moderation_subjects cps
			  ON cps.tenant_id = c.tenant_id
			 AND cps.surface = 'request_comment'
			 AND cps.subject_id = c.id::text
			 AND cps.state = 'approved'
			WHERE c.tenant_id = prp.tenant_id
			  AND c.request_id = prp.request_id
		) comments ON true
		WHERE prp.tenant_id = $1
		  AND %s%s%s%s%s%s%s
		ORDER BY %s
		LIMIT $2 OFFSET $3`

func (r *Repo) ListPublicRequestCandidates(ctx context.Context, filter PublicRequestListFilter) (PublicRequestListResult, error) {
	limit := boundedLimit(filter.Limit)
	offset, err := parseCursor(filter.Cursor)
	if err != nil {
		return PublicRequestListResult{}, err
	}
	tenantID, err := r.ResolveTenantIDBySlug(ctx, filter.TenantSlug)
	if err != nil {
		return PublicRequestListResult{}, err
	}
	policy, err := r.GetPolicy(ctx, tenantID)
	if err != nil {
		return PublicRequestListResult{}, err
	}

	includedClause := "prp.included_in_portal = TRUE"
	orderBy := publicBoardOrderByClause(filter.Sort, filter.Roadmap)
	if filter.Roadmap {
		includedClause = "prp.included_in_roadmap = TRUE"
	}
	args := []any{tenantID, limit + 1, offset, filter.ViewerSubjectKey}
	stateClause, args := publicBoardContainsClause("prp.public_state", filter.State, args)
	roadmapClause, args := publicBoardContainsClause("prp.roadmap_column", filter.RoadmapColumn, args)
	excludeClause, args := publicBoardExcludeClause(filter.ExcludePublicSlug, args)
	voteClause, args := publicBoardViewerVoteClause(filter.OnlyVotedByViewer, filter.ViewerSubjectKey, args)
	commentClause := ""
	if filter.OnlyWithComments {
		commentClause = "\n		  AND COALESCE(comments.comment_count, 0) > 0"
	}
	var searchClause string
	if strings.TrimSpace(filter.SimilarityText) != "" {
		searchClause, args = publicBoardSimilarityClause(filter.SimilarityText, args)
	} else {
		searchClause, args = publicBoardSearchClause(filter.Query, args)
	}
	q := fmt.Sprintf(
		publicBoardListCandidatesQuery,
		includedClause,
		excludeClause,
		stateClause,
		roadmapClause,
		voteClause,
		commentClause,
		searchClause,
		orderBy,
	)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return PublicRequestListResult{}, err
	}
	defer rows.Close()
	items, err := scanPublicRequestListCandidates(rows)
	if err != nil {
		return PublicRequestListResult{}, err
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = strconv.Itoa(offset + limit)
	}
	return PublicRequestListResult{
		Policy:     ptrext.Indirect(policy),
		Items:      items,
		NextCursor: next,
	}, nil
}

func publicBoardContainsClause(column string, value string, args []any) (string, []any) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", args
	}
	args = append(args, "%"+trimmed+"%")
	return fmt.Sprintf(`
		  AND %s ILIKE $%d`, column, len(args)), args
}

func publicBoardSearchClause(query string, args []any) (string, []any) {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return "", args
	}
	args = append(args, "%"+trimmed+"%")
	searchArg := len(args)
	clause := fmt.Sprintf(`
		  AND (
		    prp.public_title ILIKE $%[1]d
		    OR prp.public_summary ILIKE $%[1]d
		    OR EXISTS(
		      SELECT 1
		      FROM customer_request_comments c
		      JOIN public_moderation_subjects cps
		        ON cps.tenant_id = c.tenant_id
		       AND cps.surface = 'request_comment'
		       AND cps.subject_id = c.id::text
		       AND cps.state = 'approved'
		      WHERE c.tenant_id = prp.tenant_id
		        AND c.request_id = prp.request_id
		        AND c.body ILIKE $%[1]d
		    )
	)`, searchArg)
	return clause, args
}

func publicBoardSimilarityClause(text string, args []any) (string, []any) {
	terms := publicBoardSearchTerms(text)
	if len(terms) == 0 {
		return "\n		  AND FALSE", args
	}
	clauses := make([]string, 0, len(terms))
	for _, term := range terms {
		args = append(args, "%"+term+"%")
		termArg := len(args)
		clauses = append(clauses, fmt.Sprintf(`
		  (
		    prp.public_title ILIKE $%[1]d
		    OR prp.public_summary ILIKE $%[1]d
		    OR EXISTS(
		      SELECT 1
		      FROM customer_request_comments c
		      JOIN public_moderation_subjects cps
		        ON cps.tenant_id = c.tenant_id
		       AND cps.surface = 'request_comment'
		       AND cps.subject_id = c.id::text
		       AND cps.state = 'approved'
		      WHERE c.tenant_id = prp.tenant_id
		        AND c.request_id = prp.request_id
		        AND c.body ILIKE $%[1]d
		    )
		  )`, termArg))
	}
	return "\n		  AND (" + strings.Join(clauses, "\n		    OR ") + ")", args
}

func publicBoardSearchTerms(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	seen := map[string]struct{}{}
	terms := make([]string, 0, 6)
	for _, raw := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		term := strings.TrimSpace(raw)
		if len(term) < 3 {
			continue
		}
		if _, ok := seen[term]; ok {
			continue
		}
		seen[term] = struct{}{}
		terms = append(terms, term)
		if len(terms) == 6 {
			break
		}
	}
	return terms
}

func publicBoardExcludeClause(slug string, args []any) (string, []any) {
	trimmed := strings.TrimSpace(slug)
	if trimmed == "" {
		return "", args
	}
	args = append(args, trimmed)
	return fmt.Sprintf("\n		  AND prp.public_slug <> $%d", len(args)), args
}

func publicBoardViewerVoteClause(onlyVotedByViewer bool, viewerSubjectKey string, args []any) (string, []any) {
	if !onlyVotedByViewer {
		return "", args
	}
	trimmed := strings.TrimSpace(viewerSubjectKey)
	if trimmed == "" {
		return "\n		  AND FALSE", args
	}
	args = append(args, trimmed)
	return fmt.Sprintf(`
		  AND EXISTS(
		    SELECT 1
		    FROM customer_request_votes vv
		    WHERE vv.tenant_id = prp.tenant_id
		      AND vv.request_id = prp.request_id
		      AND vv.subject_key = $%d
		  )`, len(args)), args
}

func publicBoardOrderByClause(sort string, roadmap bool) string {
	prefix := ""
	if roadmap {
		prefix = "LOWER(NULLIF(prp.roadmap_column, '')) ASC NULLS LAST, "
	}
	switch normalizePublicBoardSort(sort) {
	case "recent":
		return prefix + "prp.updated_at DESC, COALESCE(votes.vote_count, 0) DESC, prp.id DESC"
	default:
		return prefix + "COALESCE(votes.vote_count, 0) DESC, prp.updated_at DESC, prp.id DESC"
	}
}

func normalizePublicBoardSort(sort string) string {
	switch strings.ToLower(strings.TrimSpace(sort)) {
	case "recent", "new", "newest", "latest", "activity":
		return "recent"
	default:
		return "top"
	}
}

func (r *Repo) AddPublicRequestVoteTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	subjectKey string,
	subjectHash string,
	subjectDisplay string,
	createdBy string,
) error {
	var id uuid.UUID
	err := tx.QueryRow(
		ctx, `
		INSERT INTO customer_request_votes (
			tenant_id, request_id, subject_key, subject_hash, subject_display,
			account_key, account_display, weight, note, created_by
		)
		SELECT $1, cr.id, $3, $4, $5, '', '', 1, '', $6
		FROM customer_requests cr
		WHERE cr.tenant_id = $1
		  AND cr.id = $2
		  AND cr.archived_at IS NULL
		ON CONFLICT (tenant_id, request_id, subject_hash, subject_key, account_key)
		DO UPDATE SET
		  subject_display = EXCLUDED.subject_display,
		  weight = EXCLUDED.weight,
		  note = EXCLUDED.note,
		  created_by = EXCLUDED.created_by,
		  created_at = NOW()
		RETURNING id`,
		tenantID, requestID, subjectKey, subjectHash, subjectDisplay, createdBy,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return mapWriteError(err)
	}
	return nil
}

func (r *Repo) RemovePublicRequestVoteTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	subjectKey string,
) error {
	_, err := tx.Exec(
		ctx, `
		DELETE FROM customer_request_votes
		WHERE tenant_id = $1
		  AND request_id = $2
		  AND subject_key = $3`,
		tenantID, requestID, subjectKey,
	)
	if err != nil {
		return fmt.Errorf("remove public vote: %w", err)
	}
	return nil
}

func (r *Repo) AddPublicRequestCommentTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	subjectKey string,
	subjectHash string,
	subjectDisplay string,
	body string,
	createdBy string,
) (*PublicRequestComment, error) {
	var comment PublicRequestComment
	err := tx.QueryRow(
		ctx, `
		INSERT INTO customer_request_comments (
			tenant_id, request_id, body, subject_key, subject_hash, subject_display, created_by
		)
		SELECT $1, cr.id, $3, $4, $5, $6, $7
		FROM customer_requests cr
		WHERE cr.tenant_id = $1
		  AND cr.id = $2
		  AND cr.archived_at IS NULL
		  AND cr.merged_into_request_id IS NULL
		RETURNING id, body, subject_display, created_at`,
		tenantID, requestID, body, subjectKey, subjectHash, subjectDisplay, createdBy,
	).Scan(&comment.ID, &comment.Body, &comment.SubmittedByDisplay, &comment.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, mapWriteError(err)
	}
	return ptrext.Of(comment), nil
}

func (r *Repo) ListPublicRequestComments(
	ctx context.Context,
	tenantSlug string,
	publicSlug string,
	viewerSubjectKey string,
) ([]PublicRequestComment, error) {
	tenantID, err := r.ResolveTenantIDBySlug(ctx, tenantSlug)
	if err != nil {
		return nil, err
	}
	q := `
		SELECT
			c.id,
			c.body,
			c.subject_display,
			pms.state,
			c.created_at
		FROM public_request_profiles prp
		JOIN public_moderation_subjects pms_req
		  ON pms_req.tenant_id = prp.tenant_id
		 AND pms_req.surface = 'request'
		 AND pms_req.subject_id = prp.id::text
		 AND pms_req.state = 'approved'
		JOIN customer_requests cr
		  ON cr.tenant_id = prp.tenant_id
		 AND cr.id = prp.request_id
		 AND cr.archived_at IS NULL
		 AND cr.merged_into_request_id IS NULL
		JOIN customer_request_comments c
		  ON c.tenant_id = prp.tenant_id
		 AND c.request_id = prp.request_id
		JOIN public_moderation_subjects pms
		  ON pms.tenant_id = c.tenant_id
		 AND pms.surface = 'request_comment'
		 AND pms.subject_id = c.id::text
		WHERE prp.tenant_id = $1
		  AND prp.public_slug = $2
		  AND (
		    pms.state = 'approved'
		    OR ($3 <> '' AND c.subject_key = $3)
		  )
		ORDER BY c.created_at ASC, c.id ASC`
	rows, err := r.pool.Query(ctx, q, tenantID, publicSlug, viewerSubjectKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublicRequestComment
	for rows.Next() {
		var comment PublicRequestComment
		if err := rows.Scan(&comment.ID, &comment.Body, &comment.SubmittedByDisplay, &comment.State, &comment.CreatedAt); err != nil { // ptrext:allow scan-target
			return nil, err
		}
		out = append(out, comment)
	}
	return out, rows.Err()
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
		"portal_submission_form",
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
	var formJSON []byte
	if err := row.Scan(policyScanTargets(&policy, &formJSON)...); err != nil { // ptrext:allow scan-target
		return nil, err
	}
	if len(formJSON) > 0 {
		if err := json.Unmarshal(formJSON, &policy.PortalSubmissionForm); err != nil {
			return nil, fmt.Errorf("unmarshal portal submission form: %w", err)
		}
	}
	return ptrext.Of(policy), nil
}

func policyScanTargets(policy *Policy, formJSON *[]byte, extra ...any) []any {
	targets := []any{
		&policy.TenantID, &policy.PortalAccessMode, &policy.SearchIndexingEnabled, // ptrext:allow scan-target
		&policy.RequestsEnabled, &policy.CommentsEnabled, &policy.RoadmapEnabled, // ptrext:allow scan-target
		&policy.ChangelogEnabled, &policy.SubmissionWriteMode, &policy.CommentWriteMode, // ptrext:allow scan-target
		&policy.VoteWriteMode, &policy.DefaultRequestState, &policy.DefaultCommentState, // ptrext:allow scan-target
		&policy.SubmitterIdentityMode, &policy.ShowVoteCount, &policy.ShowCommentCount, // ptrext:allow scan-target
		&policy.ShowSubmitterDisplay, &policy.HidePublicTimestamps, formJSON, &policy.UpdatedBy, // ptrext:allow scan-target
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

func scanPublicRequestListCandidates(rows pgx.Rows) ([]PublicRequestListCandidate, error) {
	var out []PublicRequestListCandidate
	for rows.Next() {
		var candidate PublicRequestListCandidate
		var votes int64
		var viewerHasVoted bool
		var comments int64
		targets := profileScanTargets(&candidate.Profile)                                           // ptrext:allow scan-target
		targets = append(targets, subjectScanTargets(&candidate.Moderation)...)                     // ptrext:allow scan-target
		targets = append(targets, &votes, &viewerHasVoted, &comments, &candidate.CustomerRequestID) // ptrext:allow scan-target
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		candidate.VoteCount = int(votes)
		candidate.CommentCount = int(comments)
		candidate.ViewerHasVoted = viewerHasVoted
		candidate.SubmitterDisplay = candidate.Moderation.SubmittedByDisplay
		out = append(out, candidate)
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
