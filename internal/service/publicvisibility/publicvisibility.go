// SPDX-License-Identifier: Apache-2.0

// Package publicvisibility coordinates public policy, moderation transitions,
// audit recording, and public-safe projections.
package publicvisibility

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

var (
	publicSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,159}$`)
	reasonCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,79}$`)
	reasonRequiredFor = map[ModerationAction]struct{}{
		ActionReject:   {},
		ActionHide:     {},
		ActionMarkSpam: {},
	}
)

var (
	ErrValidation        = errors.New("public visibility validation failed")
	ErrNotFound          = errors.New("public visibility not found")
	ErrInvalidTransition = errors.New("public visibility moderation transition invalid")
)

type Service struct {
	repo  repository
	audit *auditlogsvc.Service
}

func New(r *repo.Repo, audit *auditlogsvc.Service) *Service {
	return ptrext.Of(Service{repo: r, audit: audit})
}

type repository interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	GetPolicy(ctx context.Context, tenantID string) (*repo.Policy, error)
	ListSubjects(ctx context.Context, filter repo.ListFilter) (repo.ListResult, error)
	GetRequestPublication(ctx context.Context, tenantID string, requestID uuid.UUID) (*repo.RequestPublication, error)
	UpsertPolicyTx(ctx context.Context, tx pgx.Tx, policy repo.Policy) (*repo.Policy, error)
	UpsertRequestPublicationTx(
		ctx context.Context,
		tx pgx.Tx,
		profile repo.RequestProfile,
		defaultState repo.ModerationState,
		submittedByDisplay string,
		submittedByFingerprint string,
	) (*repo.RequestPublication, error)
	GetSubjectForUpdateTx(ctx context.Context, tx pgx.Tx, tenantID string, id uuid.UUID) (*repo.ModerationSubject, error)
	UpdateSubjectStateTx(
		ctx context.Context,
		tx pgx.Tx,
		tenantID string,
		id uuid.UUID,
		state repo.ModerationState,
		reasonCode string,
		reasonNote string,
		reviewedBy string,
		reviewedAt time.Time,
	) (*repo.ModerationSubject, error)
	GetPublicRequestCandidate(ctx context.Context, tenantSlug string, publicSlug string) (*repo.PublicRequestCandidate, error)
}

type UpdatePolicyInput struct {
	TenantID              string
	PortalAccessMode      repo.AccessMode
	SearchIndexingEnabled bool
	RequestsEnabled       bool
	CommentsEnabled       bool
	RoadmapEnabled        bool
	ChangelogEnabled      bool
	SubmissionWriteMode   repo.WriteMode
	CommentWriteMode      repo.WriteMode
	VoteWriteMode         repo.WriteMode
	DefaultRequestState   repo.ModerationState
	DefaultCommentState   repo.ModerationState
	SubmitterIdentityMode repo.IdentityMode
	ShowVoteCount         bool
	ShowCommentCount      bool
	ShowSubmitterDisplay  bool
	HidePublicTimestamps  bool
	Actor                 auditlogsvc.Actor
}

type ListModerationInput struct {
	TenantID string
	Surfaces []repo.Surface
	States   []repo.ModerationState
	Limit    int
	Cursor   string
}

type UpsertRequestProfileInput struct {
	TenantID               string
	RequestID              uuid.UUID
	PublicSlug             string
	PublicTitle            string
	PublicSummary          string
	PublicState            string
	RoadmapColumn          string
	IncludedInPortal       bool
	IncludedInRoadmap      bool
	SubmittedByDisplay     string
	SubmittedByFingerprint string
	Actor                  auditlogsvc.Actor
}

type ModerationAction string

const (
	ActionApprove  ModerationAction = "approve"
	ActionReject   ModerationAction = "reject"
	ActionHide     ModerationAction = "hide"
	ActionMarkSpam ModerationAction = "mark_spam"
	ActionRestore  ModerationAction = "restore"
)

type ModerateInput struct {
	TenantID   string
	ID         uuid.UUID
	Action     ModerationAction
	ReasonCode string
	ReasonNote string
	Actor      auditlogsvc.Actor
}

type PublicRequest struct {
	Summary          repo.RequestProfile
	Policy           repo.Policy
	Votes            int
	Comments         int
	SubmitterDisplay string
	NoIndex          bool
}

func (s *Service) GetPolicy(ctx context.Context, tenantID string) (repo.Policy, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return repo.Policy{}, ErrValidation
	}
	policy, err := s.repo.GetPolicy(ctx, tenantID)
	if errors.Is(err, repo.ErrNotFound) {
		return defaultPolicy(tenantID), nil
	}
	if err != nil {
		return repo.Policy{}, err
	}
	return ptrext.Indirect(policy), nil
}

func (s *Service) UpdatePolicy(ctx context.Context, in UpdatePolicyInput) (repo.Policy, error) {
	normalized, err := normalizePolicyInput(in)
	if err != nil {
		return repo.Policy{}, err
	}
	before, err := s.GetPolicy(ctx, normalized.TenantID)
	if err != nil {
		return repo.Policy{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return repo.Policy{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	after, err := s.repo.UpsertPolicyTx(ctx, tx, normalized)
	if err != nil {
		return repo.Policy{}, err
	}
	if err := s.recordPolicyAuditTx(ctx, tx, in.Actor, before, ptrext.Indirect(after)); err != nil {
		return repo.Policy{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.Policy{}, err
	}
	return ptrext.Indirect(after), nil
}

func (s *Service) ListModeration(ctx context.Context, in ListModerationInput) (repo.ListResult, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		return repo.ListResult{}, ErrValidation
	}
	return s.repo.ListSubjects(ctx, repo.ListFilter{
		TenantID: tenantID,
		Surfaces: in.Surfaces,
		States:   in.States,
		Limit:    in.Limit,
		Cursor:   strings.TrimSpace(in.Cursor),
	})
}

func (s *Service) GetRequestPublication(ctx context.Context, tenantID string, requestID uuid.UUID) (repo.RequestPublication, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" || requestID == uuid.Nil {
		return repo.RequestPublication{}, ErrValidation
	}
	publication, err := s.repo.GetRequestPublication(ctx, tenantID, requestID)
	if errors.Is(err, repo.ErrNotFound) {
		return repo.RequestPublication{}, ErrNotFound
	}
	if err != nil {
		return repo.RequestPublication{}, err
	}
	return ptrext.Indirect(publication), nil
}

func (s *Service) UpsertRequestProfile(ctx context.Context, in UpsertRequestProfileInput) (repo.RequestPublication, error) {
	normalized, err := normalizeRequestProfileInput(in)
	if err != nil {
		return repo.RequestPublication{}, err
	}
	policy, err := s.GetPolicy(ctx, normalized.TenantID)
	if err != nil {
		return repo.RequestPublication{}, err
	}
	before, err := s.repo.GetRequestPublication(ctx, normalized.TenantID, normalized.RequestID)
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return repo.RequestPublication{}, err
	}
	submittedByDisplay := bounded(strings.TrimSpace(in.SubmittedByDisplay), 200)
	submittedByFingerprint := bounded(strings.TrimSpace(in.SubmittedByFingerprint), 128)
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return repo.RequestPublication{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	after, err := s.repo.UpsertRequestPublicationTx(ctx, tx, normalized,
		policy.DefaultRequestState, submittedByDisplay, submittedByFingerprint)
	if errors.Is(err, repo.ErrNotFound) {
		return repo.RequestPublication{}, ErrNotFound
	}
	if errors.Is(err, repo.ErrInvalidInput) {
		return repo.RequestPublication{}, ErrValidation
	}
	if err != nil {
		return repo.RequestPublication{}, err
	}
	if err := s.recordRequestProfileAuditTx(ctx, tx, in.Actor, before, ptrext.Indirect(after)); err != nil {
		return repo.RequestPublication{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.RequestPublication{}, err
	}
	return ptrext.Indirect(after), nil
}

func (s *Service) Moderate(ctx context.Context, in ModerateInput) (repo.ModerationSubject, error) {
	normalized, err := normalizeModerateInput(in)
	if err != nil {
		return repo.ModerationSubject{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return repo.ModerationSubject{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, err := s.repo.GetSubjectForUpdateTx(ctx, tx, normalized.TenantID, normalized.ID)
	if errors.Is(err, repo.ErrNotFound) {
		return repo.ModerationSubject{}, ErrNotFound
	}
	if err != nil {
		return repo.ModerationSubject{}, err
	}
	next, err := nextState(before.State, normalized.Action)
	if err != nil {
		return repo.ModerationSubject{}, err
	}
	after, err := s.repo.UpdateSubjectStateTx(ctx, tx, normalized.TenantID, normalized.ID, next,
		normalized.ReasonCode, normalized.ReasonNote, actorID(normalized.Actor), time.Now().UTC())
	if errors.Is(err, repo.ErrNotFound) {
		return repo.ModerationSubject{}, ErrNotFound
	}
	if err != nil {
		return repo.ModerationSubject{}, err
	}
	if err := s.recordModerationAuditTx(ctx, tx, normalized.Actor, normalized.Action, ptrext.Indirect(before), ptrext.Indirect(after)); err != nil {
		return repo.ModerationSubject{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.ModerationSubject{}, err
	}
	return ptrext.Indirect(after), nil
}

func (s *Service) GetPublicRequest(ctx context.Context, tenantSlug string, publicSlug string) (PublicRequest, error) {
	tenantSlug = strings.TrimSpace(tenantSlug)
	publicSlug = strings.TrimSpace(publicSlug)
	if tenantSlug == "" || publicSlug == "" {
		return PublicRequest{}, ErrNotFound
	}
	candidate, err := s.repo.GetPublicRequestCandidate(ctx, tenantSlug, publicSlug)
	if errors.Is(err, repo.ErrNotFound) {
		return PublicRequest{}, ErrNotFound
	}
	if err != nil {
		return PublicRequest{}, err
	}
	if !publicRequestVisible(ptrext.Indirect(candidate)) {
		return PublicRequest{}, ErrNotFound
	}
	return publicRequestFromCandidate(ptrext.Indirect(candidate)), nil
}

func defaultPolicy(tenantID string) repo.Policy {
	return repo.Policy{
		TenantID:              tenantID,
		PortalAccessMode:      repo.AccessModeDisabled,
		SubmissionWriteMode:   repo.WriteModeDisabled,
		CommentWriteMode:      repo.WriteModeDisabled,
		VoteWriteMode:         repo.WriteModeDisabled,
		DefaultRequestState:   repo.ModerationStatePending,
		DefaultCommentState:   repo.ModerationStatePending,
		SubmitterIdentityMode: repo.IdentityModeAnonymous,
		ShowVoteCount:         true,
		ShowCommentCount:      true,
	}
}

func normalizePolicyInput(in UpdatePolicyInput) (repo.Policy, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		return repo.Policy{}, ErrValidation
	}
	if actorID(in.Actor) == "" {
		return repo.Policy{}, ErrValidation
	}
	policy := repo.Policy{
		TenantID:              tenantID,
		PortalAccessMode:      in.PortalAccessMode,
		SearchIndexingEnabled: in.SearchIndexingEnabled,
		RequestsEnabled:       in.RequestsEnabled,
		CommentsEnabled:       in.CommentsEnabled,
		RoadmapEnabled:        in.RoadmapEnabled,
		ChangelogEnabled:      in.ChangelogEnabled,
		SubmissionWriteMode:   in.SubmissionWriteMode,
		CommentWriteMode:      in.CommentWriteMode,
		VoteWriteMode:         in.VoteWriteMode,
		DefaultRequestState:   in.DefaultRequestState,
		DefaultCommentState:   in.DefaultCommentState,
		SubmitterIdentityMode: in.SubmitterIdentityMode,
		ShowVoteCount:         in.ShowVoteCount,
		ShowCommentCount:      in.ShowCommentCount,
		ShowSubmitterDisplay:  in.ShowSubmitterDisplay,
		HidePublicTimestamps:  in.HidePublicTimestamps,
		UpdatedBy:             actorID(in.Actor),
	}
	if !validAccessMode(policy.PortalAccessMode) || !validWriteMode(policy.SubmissionWriteMode) ||
		!validWriteMode(policy.CommentWriteMode) || !validWriteMode(policy.VoteWriteMode) ||
		!validDefaultState(policy.DefaultRequestState) || !validDefaultState(policy.DefaultCommentState) ||
		!validIdentityMode(policy.SubmitterIdentityMode) {
		return repo.Policy{}, ErrValidation
	}
	return policy, nil
}

func normalizeModerateInput(in ModerateInput) (ModerateInput, error) {
	in.TenantID = strings.TrimSpace(in.TenantID)
	in.ReasonCode = strings.ToLower(strings.TrimSpace(in.ReasonCode))
	in.ReasonNote = bounded(strings.TrimSpace(in.ReasonNote), 1000)
	if in.TenantID == "" || in.ID == uuid.Nil || in.Action == "" || actorID(in.Actor) == "" {
		return ModerateInput{}, ErrValidation
	}
	switch in.Action {
	case ActionApprove, ActionReject, ActionHide, ActionMarkSpam, ActionRestore:
		if _, required := reasonRequiredFor[in.Action]; required && in.ReasonCode == "" {
			return ModerateInput{}, ErrValidation
		}
		if in.ReasonCode != "" && !reasonCodePattern.MatchString(in.ReasonCode) {
			return ModerateInput{}, ErrValidation
		}
		return in, nil
	default:
		return ModerateInput{}, ErrValidation
	}
}

func normalizeRequestProfileInput(in UpsertRequestProfileInput) (repo.RequestProfile, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	slug := strings.ToLower(strings.TrimSpace(in.PublicSlug))
	title := strings.TrimSpace(in.PublicTitle)
	summary := strings.TrimSpace(in.PublicSummary)
	state := strings.TrimSpace(in.PublicState)
	column := strings.TrimSpace(in.RoadmapColumn)
	if tenantID == "" || in.RequestID == uuid.Nil || actorID(in.Actor) == "" {
		return repo.RequestProfile{}, ErrValidation
	}
	if !publicSlugPattern.MatchString(slug) || title == "" || tooLong(title, 200) ||
		tooLong(summary, 2000) || tooLong(state, 80) || tooLong(column, 80) {
		return repo.RequestProfile{}, ErrValidation
	}
	return repo.RequestProfile{
		TenantID:          tenantID,
		RequestID:         in.RequestID,
		PublicSlug:        slug,
		PublicTitle:       title,
		PublicSummary:     summary,
		PublicState:       state,
		RoadmapColumn:     column,
		IncludedInPortal:  in.IncludedInPortal,
		IncludedInRoadmap: in.IncludedInRoadmap,
		UpdatedBy:         actorID(in.Actor),
	}, nil
}

func nextState(current repo.ModerationState, action ModerationAction) (repo.ModerationState, error) {
	switch action {
	case ActionApprove:
		if current == repo.ModerationStatePending || current == repo.ModerationStateRejected {
			return repo.ModerationStateApproved, nil
		}
	case ActionReject:
		if current == repo.ModerationStatePending {
			return repo.ModerationStateRejected, nil
		}
	case ActionHide:
		if current == repo.ModerationStateApproved {
			return repo.ModerationStateHidden, nil
		}
	case ActionMarkSpam:
		if current != repo.ModerationStateSpam {
			return repo.ModerationStateSpam, nil
		}
	case ActionRestore:
		if current == repo.ModerationStateHidden {
			return repo.ModerationStateApproved, nil
		}
		if current == repo.ModerationStateRejected || current == repo.ModerationStateSpam {
			return repo.ModerationStatePending, nil
		}
	}
	return "", ErrInvalidTransition
}

func publicRequestVisible(candidate repo.PublicRequestCandidate) bool {
	return candidate.Policy.PortalAccessMode == repo.AccessModePublic &&
		candidate.Policy.RequestsEnabled &&
		candidate.Profile.IncludedInPortal &&
		candidate.Moderation.State == repo.ModerationStateApproved &&
		candidate.CustomerRequestLive
}

func publicRequestFromCandidate(candidate repo.PublicRequestCandidate) PublicRequest {
	return PublicRequest{
		Summary:          candidate.Profile,
		Policy:           candidate.Policy,
		Votes:            candidate.VoteCount,
		Comments:         candidate.CommentCount,
		SubmitterDisplay: publicSubmitterDisplay(candidate.Policy, candidate.SubmitterDisplay),
		NoIndex:          !candidate.Policy.SearchIndexingEnabled,
	}
}

func publicSubmitterDisplay(policy repo.Policy, display string) string {
	if !policy.ShowSubmitterDisplay || policy.SubmitterIdentityMode == repo.IdentityModeAnonymous {
		return ""
	}
	return display
}

func validAccessMode(mode repo.AccessMode) bool {
	return mode == repo.AccessModeDisabled || mode == repo.AccessModePublic
}

func validWriteMode(mode repo.WriteMode) bool {
	return mode == repo.WriteModeDisabled || mode == repo.WriteModeAnonymous || mode == repo.WriteModeIdentified
}

func validDefaultState(state repo.ModerationState) bool {
	return state == repo.ModerationStatePending || state == repo.ModerationStateApproved
}

func validIdentityMode(mode repo.IdentityMode) bool {
	return mode == repo.IdentityModeAnonymous || mode == repo.IdentityModeDisplayName || mode == repo.IdentityModeOrganization
}

func actorID(actor auditlogsvc.Actor) string {
	return strings.TrimSpace(actor.ID)
}

func actorForAudit(actor auditlogsvc.Actor) auditlogsvc.Actor {
	if strings.TrimSpace(actor.Type) == "" {
		actor.Type = "admin"
	}
	if strings.TrimSpace(actor.ID) == "" {
		actor.ID = "system"
	}
	return actor
}

func bounded(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func tooLong(value string, limit int) bool {
	return utf8.RuneCountInString(value) > limit
}

func (s *Service) recordPolicyAuditTx(ctx context.Context, tx pgx.Tx, actor auditlogsvc.Actor, before repo.Policy, after repo.Policy) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   after.TenantID,
		Actor:      actorForAudit(actor),
		Action:     "public_policy.update",
		TargetType: "public_visibility_policy",
		TargetID:   after.TenantID,
		Summary:    "Updated public visibility policy",
		Before:     policyAuditFields(before),
		After:      policyAuditFields(after),
	})
}

func (s *Service) recordRequestProfileAuditTx(
	ctx context.Context,
	tx pgx.Tx,
	actor auditlogsvc.Actor,
	before *repo.RequestPublication,
	after repo.RequestPublication,
) error {
	if s.audit == nil {
		return nil
	}
	var beforeFields map[string]any
	if before != nil {
		beforeFields = requestProfileAuditFields(ptrext.Indirect(before).Profile)
	}
	return s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   after.Profile.TenantID,
		Actor:      actorForAudit(actor),
		Action:     "public_request_profile.upsert",
		TargetType: "public_request_profile",
		TargetID:   after.Profile.ID.String(),
		Summary:    "Updated public request profile",
		Before:     beforeFields,
		After:      requestProfileAuditFields(after.Profile),
	})
}

func (s *Service) recordModerationAuditTx(
	ctx context.Context,
	tx pgx.Tx,
	actor auditlogsvc.Actor,
	action ModerationAction,
	before repo.ModerationSubject,
	after repo.ModerationSubject,
) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   after.TenantID,
		Actor:      actorForAudit(actor),
		Action:     "moderation." + string(action),
		TargetType: "public_moderation_subject",
		TargetID:   after.ID.String(),
		Summary:    fmt.Sprintf("Changed %s moderation state from %s to %s", after.Surface, before.State, after.State),
		Before:     moderationAuditFields(before),
		After:      moderationAuditFields(after),
	})
}

func policyAuditFields(policy repo.Policy) map[string]any {
	return map[string]any{
		"portal_access_mode":      policy.PortalAccessMode,
		"search_indexing_enabled": policy.SearchIndexingEnabled,
		"requests_enabled":        policy.RequestsEnabled,
		"comments_enabled":        policy.CommentsEnabled,
		"roadmap_enabled":         policy.RoadmapEnabled,
		"changelog_enabled":       policy.ChangelogEnabled,
		"submission_write_mode":   policy.SubmissionWriteMode,
		"comment_write_mode":      policy.CommentWriteMode,
		"vote_write_mode":         policy.VoteWriteMode,
		"default_request_state":   policy.DefaultRequestState,
		"default_comment_state":   policy.DefaultCommentState,
		"submitter_identity_mode": policy.SubmitterIdentityMode,
		"show_vote_count":         policy.ShowVoteCount,
		"show_comment_count":      policy.ShowCommentCount,
		"show_submitter_display":  policy.ShowSubmitterDisplay,
		"hide_public_timestamps":  policy.HidePublicTimestamps,
	}
}

func moderationAuditFields(subject repo.ModerationSubject) map[string]any {
	return map[string]any{
		"surface":     subject.Surface,
		"subject_id":  subject.SubjectID,
		"state":       subject.State,
		"reason_code": subject.ReasonCode,
	}
}

func requestProfileAuditFields(profile repo.RequestProfile) map[string]any {
	return map[string]any{
		"request_id":            profile.RequestID.String(),
		"public_slug":           profile.PublicSlug,
		"public_title_length":   utf8.RuneCountInString(profile.PublicTitle),
		"public_summary_length": utf8.RuneCountInString(profile.PublicSummary),
		"public_state":          profile.PublicState,
		"roadmap_column":        profile.RoadmapColumn,
		"included_in_portal":    profile.IncludedInPortal,
		"included_in_roadmap":   profile.IncludedInRoadmap,
	}
}
