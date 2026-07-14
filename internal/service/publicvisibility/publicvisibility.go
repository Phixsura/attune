// SPDX-License-Identifier: Apache-2.0

// Package publicvisibility coordinates public policy, moderation transitions,
// audit recording, and public-safe projections.
package publicvisibility

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/pkg/subjectkey"
	repo "github.com/Phixsura/attune/internal/repo/publicvisibility"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

var (
	publicSlugPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,159}$`)
	reasonCodePattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,79}$`)
	portalFieldKeyPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	portalFieldReservedKeys = map[string]struct{}{
		"title":           {},
		"details":         {},
		"page_url":        {},
		"display_name":    {},
		"organization":    {},
		"kind":            {},
		"honeypot":        {},
		"idempotency_key": {},
	}
	reasonRequiredFor = map[ModerationAction]struct{}{
		ActionReject:   {},
		ActionHide:     {},
		ActionMarkSpam: {},
	}
)

var (
	ErrValidation        = errors.New("public visibility validation failed")
	ErrNotFound          = errors.New("public visibility not found")
	ErrDisabled          = errors.New("public visibility disabled")
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
	ListPublicRequestComments(ctx context.Context, tenantSlug string, publicSlug string, viewerSubjectKey string) ([]repo.PublicRequestComment, error)
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
	CreateModerationSubjectTx(ctx context.Context, tx pgx.Tx, subject repo.ModerationSubject) (*repo.ModerationSubject, error)
	GetPublicRequestCandidate(ctx context.Context, tenantSlug string, publicSlug string, viewerSubjectKey string) (*repo.PublicRequestCandidate, error)
	ListPublicRequestCandidates(ctx context.Context, filter repo.PublicRequestListFilter) (repo.PublicRequestListResult, error)
	AddPublicRequestVoteTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID, subjectKey string, subjectHash string, subjectDisplay string, createdBy string) error
	RemovePublicRequestVoteTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID, subjectKey string) error
	AddPublicRequestCommentTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID, subjectKey string, subjectHash string, subjectDisplay string, body string, createdBy string) (*repo.PublicRequestComment, error)
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
	PortalSubmissionForm  repo.PortalSubmissionForm
	RoadmapStatusMappings []repo.RoadmapStatusMapping
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
	CommentItems     []repo.PublicRequestComment
	SimilarRequests  []PublicRequest
	SubmitterDisplay string
	ViewerHasVoted   bool
	CanComment       bool
	NoIndex          bool
}

type PublicRequestList struct {
	Requests   []PublicRequest
	Policy     repo.Policy
	NoIndex    bool
	NextCursor string
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

func (s *Service) GetPublicRequest(ctx context.Context, tenantSlug string, publicSlug string, visitorID string) (PublicRequest, error) {
	tenantSlug = strings.TrimSpace(tenantSlug)
	publicSlug = strings.TrimSpace(publicSlug)
	if tenantSlug == "" || publicSlug == "" {
		return PublicRequest{}, ErrNotFound
	}
	candidate, err := s.repo.GetPublicRequestCandidate(ctx, tenantSlug, publicSlug, portalVisitorSubjectKey(visitorID))
	if errors.Is(err, repo.ErrNotFound) {
		return PublicRequest{}, ErrNotFound
	}
	if err != nil {
		return PublicRequest{}, err
	}
	if !publicRequestVisible(ptrext.Indirect(candidate)) {
		return PublicRequest{}, ErrNotFound
	}
	request := publicRequestFromCandidate(ptrext.Indirect(candidate))
	if publicCommentsVisible(request.Policy) {
		comments, err := s.repo.ListPublicRequestComments(ctx, tenantSlug, publicSlug, portalVisitorSubjectKey(visitorID))
		if errors.Is(err, repo.ErrNotFound) {
			return PublicRequest{}, ErrNotFound
		}
		if err != nil {
			return PublicRequest{}, err
		}
		request.CommentItems = comments
	}
	similar, err := s.listSimilarPublicRequests(ctx, tenantSlug, request.Summary.PublicTitle, publicSlug, visitorID)
	if err != nil {
		return PublicRequest{}, err
	}
	request.SimilarRequests = similar
	request.CanComment = publicCommentWriteEnabled(request.Policy)
	return request, nil
}

func (s *Service) ListPublicRequests(ctx context.Context, tenantSlug string, limit int, cursor string, query string, sort string, state string, roadmap string, onlyVotedByViewer bool, onlyWithComments bool, visitorID string) (PublicRequestList, error) {
	return s.listPublicRequests(ctx, tenantSlug, limit, cursor, false, query, sort, state, roadmap, onlyVotedByViewer, onlyWithComments, visitorID)
}

func (s *Service) ListPublicRoadmap(ctx context.Context, tenantSlug string, limit int, cursor string, query string, sort string, state string, roadmap string, onlyVotedByViewer bool, onlyWithComments bool, visitorID string) (PublicRequestList, error) {
	return s.listPublicRequests(ctx, tenantSlug, limit, cursor, true, query, sort, state, roadmap, onlyVotedByViewer, onlyWithComments, visitorID)
}

func (s *Service) listPublicRequests(
	ctx context.Context,
	tenantSlug string,
	limit int,
	cursor string,
	roadmap bool,
	query string,
	sort string,
	state string,
	roadmapColumn string,
	onlyVotedByViewer bool,
	onlyWithComments bool,
	visitorID string,
) (PublicRequestList, error) {
	tenantSlug = strings.TrimSpace(tenantSlug)
	if tenantSlug == "" {
		return PublicRequestList{}, ErrNotFound
	}
	result, err := s.repo.ListPublicRequestCandidates(ctx, repo.PublicRequestListFilter{
		TenantSlug:        tenantSlug,
		Roadmap:           roadmap,
		Query:             strings.TrimSpace(query),
		Sort:              strings.TrimSpace(sort),
		State:             strings.TrimSpace(state),
		RoadmapColumn:     strings.TrimSpace(roadmapColumn),
		OnlyVotedByViewer: onlyVotedByViewer,
		OnlyWithComments:  onlyWithComments,
		Limit:             limit,
		Cursor:            strings.TrimSpace(cursor),
		ViewerSubjectKey:  portalVisitorSubjectKey(visitorID),
	})
	if errors.Is(err, repo.ErrNotFound) {
		return PublicRequestList{}, ErrNotFound
	}
	if errors.Is(err, repo.ErrInvalidInput) {
		return PublicRequestList{}, ErrValidation
	}
	if err != nil {
		return PublicRequestList{}, err
	}
	if !publicRequestListVisible(result.Policy, roadmap) {
		return PublicRequestList{}, ErrNotFound
	}
	requests := make([]PublicRequest, 0, len(result.Items))
	for _, item := range result.Items {
		requests = append(requests, publicRequestFromListCandidate(result.Policy, item))
	}
	return PublicRequestList{
		Requests:   requests,
		Policy:     result.Policy,
		NoIndex:    !result.Policy.SearchIndexingEnabled,
		NextCursor: result.NextCursor,
	}, nil
}

func (s *Service) listSimilarPublicRequests(
	ctx context.Context,
	tenantSlug string,
	title string,
	currentSlug string,
	visitorID string,
) ([]PublicRequest, error) {
	tenantSlug = strings.TrimSpace(tenantSlug)
	title = strings.TrimSpace(title)
	currentSlug = strings.TrimSpace(currentSlug)
	if tenantSlug == "" || title == "" {
		return nil, nil
	}
	result, err := s.repo.ListPublicRequestCandidates(ctx, repo.PublicRequestListFilter{
		TenantSlug:        tenantSlug,
		SimilarityText:    title,
		ExcludePublicSlug: currentSlug,
		Sort:              "top",
		Limit:             4,
		ViewerSubjectKey:  portalVisitorSubjectKey(visitorID),
	})
	if errors.Is(err, repo.ErrNotFound) {
		return nil, nil
	}
	if errors.Is(err, repo.ErrInvalidInput) {
		return nil, ErrValidation
	}
	if err != nil {
		return nil, err
	}
	similar := make([]PublicRequest, 0, len(result.Items))
	for _, item := range result.Items {
		similar = append(similar, publicRequestFromListCandidate(result.Policy, item))
	}
	return similar, nil
}

func (s *Service) VotePublicRequest(ctx context.Context, tenantSlug string, publicSlug string, visitorID string, actor auditlogsvc.Actor) (PublicRequest, error) {
	target, subjectKey, subjectHash, err := s.resolvePublicVoteTarget(ctx, tenantSlug, publicSlug, visitorID)
	if err != nil {
		return PublicRequest{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return PublicRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.repo.AddPublicRequestVoteTx(ctx, tx, target.Policy.TenantID, target.CustomerRequestID, subjectKey, subjectHash, portalVisitorSubjectDisplay(), actorID(actor)); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return PublicRequest{}, ErrNotFound
		}
		return PublicRequest{}, err
	}
	if err := s.recordPortalVoteAuditTx(ctx, tx, actor, "customer_request.add_vote", target, subjectKey, subjectHash); err != nil {
		return PublicRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PublicRequest{}, err
	}
	return s.GetPublicRequest(ctx, tenantSlug, publicSlug, visitorID)
}

func (s *Service) CreatePublicRequestComment(ctx context.Context, tenantSlug string, publicSlug string, visitorID string, body string, actor auditlogsvc.Actor) (PublicRequest, error) {
	target, subjectKey, subjectHash, trimmedBody, err := s.resolvePublicCommentTarget(ctx, tenantSlug, publicSlug, visitorID, body)
	if err != nil {
		return PublicRequest{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return PublicRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	comment, err := s.repo.AddPublicRequestCommentTx(
		ctx,
		tx,
		target.Policy.TenantID,
		target.CustomerRequestID,
		subjectKey,
		subjectHash,
		portalVisitorSubjectDisplay(),
		trimmedBody,
		actorID(actor),
	)
	if errors.Is(err, repo.ErrNotFound) {
		return PublicRequest{}, ErrNotFound
	}
	if err != nil {
		return PublicRequest{}, err
	}
	subject, err := s.repo.CreateModerationSubjectTx(ctx, tx, repo.ModerationSubject{
		TenantID:               target.Policy.TenantID,
		Surface:                repo.SurfaceRequestComment,
		SubjectID:              comment.ID.String(),
		State:                  target.Policy.DefaultCommentState,
		SubmittedByDisplay:     comment.SubmittedByDisplay,
		SubmittedByFingerprint: subjectHash,
	})
	if err != nil {
		return PublicRequest{}, err
	}
	if err := s.recordPortalCommentAuditTx(ctx, tx, actor, target, subjectKey, subjectHash, comment, ptrext.Indirect(subject)); err != nil {
		return PublicRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PublicRequest{}, err
	}
	return s.GetPublicRequest(ctx, tenantSlug, publicSlug, visitorID)
}

func (s *Service) UnvotePublicRequest(ctx context.Context, tenantSlug string, publicSlug string, visitorID string, actor auditlogsvc.Actor) (PublicRequest, error) {
	target, subjectKey, subjectHash, err := s.resolvePublicVoteTarget(ctx, tenantSlug, publicSlug, visitorID)
	if err != nil {
		return PublicRequest{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return PublicRequest{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.repo.RemovePublicRequestVoteTx(ctx, tx, target.Policy.TenantID, target.CustomerRequestID, subjectKey); err != nil {
		return PublicRequest{}, err
	}
	if err := s.recordPortalVoteAuditTx(ctx, tx, actor, "customer_request.remove_vote", target, subjectKey, subjectHash); err != nil {
		return PublicRequest{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return PublicRequest{}, err
	}
	return s.GetPublicRequest(ctx, tenantSlug, publicSlug, visitorID)
}

func (s *Service) resolvePublicVoteTarget(ctx context.Context, tenantSlug string, publicSlug string, visitorID string) (repo.PublicRequestCandidate, string, string, error) {
	visitorSubjectKey := portalVisitorSubjectKey(visitorID)
	if visitorSubjectKey == "" {
		return repo.PublicRequestCandidate{}, "", "", ErrValidation
	}
	candidate, err := s.repo.GetPublicRequestCandidate(ctx, tenantSlug, publicSlug, visitorSubjectKey)
	if errors.Is(err, repo.ErrNotFound) {
		return repo.PublicRequestCandidate{}, "", "", ErrNotFound
	}
	if err != nil {
		return repo.PublicRequestCandidate{}, "", "", err
	}
	if !publicRequestVisible(ptrext.Indirect(candidate)) {
		return repo.PublicRequestCandidate{}, "", "", ErrNotFound
	}
	if candidate.Policy.VoteWriteMode == repo.WriteModeDisabled {
		return repo.PublicRequestCandidate{}, "", "", ErrDisabled
	}
	subjectHash := subjectkey.Hash(candidate.Policy.TenantID, visitorSubjectKey)
	return ptrext.Indirect(candidate), visitorSubjectKey, subjectHash, nil
}

func (s *Service) resolvePublicCommentTarget(ctx context.Context, tenantSlug string, publicSlug string, visitorID string, body string) (repo.PublicRequestCandidate, string, string, string, error) {
	visitorSubjectKey := portalVisitorSubjectKey(visitorID)
	if visitorSubjectKey == "" {
		return repo.PublicRequestCandidate{}, "", "", "", ErrValidation
	}
	body = strings.TrimSpace(body)
	if body == "" || tooLong(body, 5000) {
		return repo.PublicRequestCandidate{}, "", "", "", ErrValidation
	}
	candidate, err := s.repo.GetPublicRequestCandidate(ctx, tenantSlug, publicSlug, visitorSubjectKey)
	if errors.Is(err, repo.ErrNotFound) {
		return repo.PublicRequestCandidate{}, "", "", "", ErrNotFound
	}
	if err != nil {
		return repo.PublicRequestCandidate{}, "", "", "", err
	}
	if !publicRequestVisible(ptrext.Indirect(candidate)) {
		return repo.PublicRequestCandidate{}, "", "", "", ErrNotFound
	}
	if !publicCommentWriteEnabled(candidate.Policy) {
		return repo.PublicRequestCandidate{}, "", "", "", ErrDisabled
	}
	subjectHash := subjectkey.Hash(candidate.Policy.TenantID, visitorSubjectKey)
	return ptrext.Indirect(candidate), visitorSubjectKey, subjectHash, body, nil
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
		PortalSubmissionForm:  defaultPortalSubmissionForm(),
		RoadmapStatusMappings: defaultRoadmapStatusMappings(),
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
		PortalSubmissionForm:  in.PortalSubmissionForm,
		RoadmapStatusMappings: in.RoadmapStatusMappings,
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
	form, err := normalizePortalSubmissionForm(policy.PortalSubmissionForm)
	if err != nil {
		return repo.Policy{}, err
	}
	policy.PortalSubmissionForm = form
	roadmapMappings, err := normalizeRoadmapStatusMappings(policy.RoadmapStatusMappings)
	if err != nil {
		return repo.Policy{}, err
	}
	policy.RoadmapStatusMappings = roadmapMappings
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
		tooLong(summary, 2000) || tooLong(state, 80) {
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

var roadmapStatusRanks = map[string]int{
	"open":        0,
	"planned":     1,
	"in_progress": 2,
	"shipped":     3,
	"cancelled":   4,
}

func defaultRoadmapStatusMappings() []repo.RoadmapStatusMapping {
	return []repo.RoadmapStatusMapping{
		{Status: "open", Label: "under consideration", Order: 1, Included: true},
		{Status: "planned", Label: "planned", Order: 2, Included: true},
		{Status: "in_progress", Label: "in progress", Order: 3, Included: true},
		{Status: "shipped", Label: "shipped", Order: 4, Included: true},
		{Status: "cancelled", Label: "cancelled", Order: 5, Included: false},
	}
}

func normalizeRoadmapStatusMappings(mappings []repo.RoadmapStatusMapping) ([]repo.RoadmapStatusMapping, error) {
	if len(mappings) == 0 {
		return defaultRoadmapStatusMappings(), nil
	}
	normalized := make([]repo.RoadmapStatusMapping, 0, len(mappings))
	seenStatuses := make(map[string]struct{}, len(mappings))
	seenLabels := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		status := strings.ToLower(strings.TrimSpace(mapping.Status))
		label := strings.TrimSpace(mapping.Label)
		if status == "" || label == "" || !roadmapStatusKnown(status) || mapping.Order <= 0 {
			return nil, ErrValidation
		}
		if _, dup := seenStatuses[status]; dup {
			return nil, ErrValidation
		}
		labelKey := strings.ToLower(label)
		if _, dup := seenLabels[labelKey]; dup {
			return nil, ErrValidation
		}
		seenStatuses[status] = struct{}{}
		seenLabels[labelKey] = struct{}{}
		normalized = append(normalized, repo.RoadmapStatusMapping{
			Status:   status,
			Label:    bounded(label, 80),
			Order:    mapping.Order,
			Included: mapping.Included,
		})
	}
	if len(normalized) != len(defaultRoadmapStatusMappings()) {
		return nil, ErrValidation
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		if normalized[i].Order == normalized[j].Order {
			return roadmapStatusRank(normalized[i].Status) < roadmapStatusRank(normalized[j].Status)
		}
		return normalized[i].Order < normalized[j].Order
	})
	for i := range normalized {
		normalized[i].Order = i + 1
	}
	return normalized, nil
}

func roadmapStatusKnown(status string) bool {
	_, ok := roadmapStatusRanks[status]
	return ok
}

func roadmapStatusRank(status string) int {
	if rank, ok := roadmapStatusRanks[status]; ok {
		return rank
	}
	return len(roadmapStatusRanks)
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

func publicRequestListVisible(policy repo.Policy, roadmap bool) bool {
	if policy.PortalAccessMode != repo.AccessModePublic {
		return false
	}
	if roadmap {
		return policy.RoadmapEnabled
	}
	return policy.RequestsEnabled
}

func publicRequestFromCandidate(candidate repo.PublicRequestCandidate) PublicRequest {
	return PublicRequest{
		Summary:          candidate.Profile,
		Policy:           candidate.Policy,
		Votes:            candidate.VoteCount,
		Comments:         candidate.CommentCount,
		SubmitterDisplay: publicSubmitterDisplay(candidate.Policy, candidate.SubmitterDisplay),
		ViewerHasVoted:   candidate.ViewerHasVoted,
		CanComment:       publicCommentWriteEnabled(candidate.Policy),
		NoIndex:          !candidate.Policy.SearchIndexingEnabled,
	}
}

func publicRequestFromListCandidate(policy repo.Policy, candidate repo.PublicRequestListCandidate) PublicRequest {
	return PublicRequest{
		Summary:          candidate.Profile,
		Policy:           policy,
		Votes:            candidate.VoteCount,
		Comments:         candidate.CommentCount,
		SubmitterDisplay: publicSubmitterDisplay(policy, candidate.SubmitterDisplay),
		ViewerHasVoted:   candidate.ViewerHasVoted,
		CanComment:       publicCommentWriteEnabled(policy),
		NoIndex:          !policy.SearchIndexingEnabled,
	}
}

func portalVisitorSubjectKey(visitorID string) string {
	visitorID = strings.TrimSpace(visitorID)
	if visitorID == "" {
		return ""
	}
	return "portal:" + visitorID
}

func portalVisitorSubjectDisplay() string {
	return "Portal visitor"
}

func publicSubmitterDisplay(policy repo.Policy, display string) string {
	if !policy.ShowSubmitterDisplay || policy.SubmitterIdentityMode == repo.IdentityModeAnonymous {
		return ""
	}
	return display
}

func publicCommentWriteEnabled(policy repo.Policy) bool {
	return policy.CommentsEnabled && policy.CommentWriteMode != repo.WriteModeDisabled
}

func publicCommentsVisible(policy repo.Policy) bool {
	return policy.CommentsEnabled
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

func (s *Service) recordPortalVoteAuditTx(
	ctx context.Context,
	tx pgx.Tx,
	actor auditlogsvc.Actor,
	action string,
	candidate repo.PublicRequestCandidate,
	subjectKey string,
	subjectHash string,
) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   candidate.Policy.TenantID,
		Actor:      actorForAudit(actor),
		Action:     action,
		TargetType: "customer_request",
		TargetID:   candidate.CustomerRequestID.String(),
		Summary:    strings.TrimSpace(strings.ReplaceAll(action, "_", " ")),
		Before: map[string]any{
			"request_id": candidate.Profile.RequestID.String(),
		},
		After: map[string]any{
			"request_id":    candidate.Profile.RequestID.String(),
			"subject_key":   subjectKey,
			"subject_hash":  subjectHash,
			"vote_source":   "portal",
			"vote_count":    candidate.VoteCount,
			"viewer_voted":  candidate.ViewerHasVoted,
			"public_slug":   candidate.Profile.PublicSlug,
			"moderation_id": candidate.Moderation.ID.String(),
		},
	})
}

func (s *Service) recordPortalCommentAuditTx(
	ctx context.Context,
	tx pgx.Tx,
	actor auditlogsvc.Actor,
	candidate repo.PublicRequestCandidate,
	subjectKey string,
	subjectHash string,
	comment *repo.PublicRequestComment,
	moderation repo.ModerationSubject,
) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   candidate.Policy.TenantID,
		Actor:      actorForAudit(actor),
		Action:     "customer_request.add_comment",
		TargetType: "customer_request_comment",
		TargetID:   comment.ID.String(),
		Summary:    "Added public request comment",
		Before: map[string]any{
			"request_id": candidate.Profile.RequestID.String(),
		},
		After: map[string]any{
			"request_id":     candidate.Profile.RequestID.String(),
			"subject_key":    subjectKey,
			"subject_hash":   subjectHash,
			"comment_length": utf8.RuneCountInString(comment.Body),
			"public_slug":    candidate.Profile.PublicSlug,
			"moderation_id":  moderation.ID.String(),
			"state":          moderation.State,
		},
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
		"portal_access_mode":              policy.PortalAccessMode,
		"search_indexing_enabled":         policy.SearchIndexingEnabled,
		"requests_enabled":                policy.RequestsEnabled,
		"comments_enabled":                policy.CommentsEnabled,
		"roadmap_enabled":                 policy.RoadmapEnabled,
		"changelog_enabled":               policy.ChangelogEnabled,
		"submission_write_mode":           policy.SubmissionWriteMode,
		"comment_write_mode":              policy.CommentWriteMode,
		"vote_write_mode":                 policy.VoteWriteMode,
		"default_request_state":           policy.DefaultRequestState,
		"default_comment_state":           policy.DefaultCommentState,
		"submitter_identity_mode":         policy.SubmitterIdentityMode,
		"show_vote_count":                 policy.ShowVoteCount,
		"show_comment_count":              policy.ShowCommentCount,
		"show_submitter_display":          policy.ShowSubmitterDisplay,
		"hide_public_timestamps":          policy.HidePublicTimestamps,
		"roadmap_status_mapping_count":    len(policy.RoadmapStatusMappings),
		"portal_submission_headline":      policy.PortalSubmissionForm.Headline,
		"portal_submission_field_count":   len(policy.PortalSubmissionForm.Fields),
		"portal_submission_show_page_url": policy.PortalSubmissionForm.ShowPageURL,
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
		"roadmap_order":         profile.RoadmapOrder,
		"roadmap_visible":       profile.RoadmapVisible,
		"included_in_portal":    profile.IncludedInPortal,
		"included_in_roadmap":   profile.IncludedInRoadmap,
	}
}

func defaultPortalSubmissionForm() repo.PortalSubmissionForm {
	return repo.PortalSubmissionForm{
		Headline:          "Send feedback",
		Description:       "Share bugs, ideas, or anything blocking your work.",
		Acknowledgement:   "Thanks. We will review your submission.",
		SubmitButtonLabel: "Submit feedback",
		ShowPageURL:       true,
	}
}

func normalizePortalSubmissionForm(form repo.PortalSubmissionForm) (repo.PortalSubmissionForm, error) {
	form = normalizePortalSubmissionFormText(form)
	if len(form.Fields) == 0 {
		form.Fields = nil
		return form, nil
	}
	if len(form.Fields) > 8 {
		return repo.PortalSubmissionForm{}, ErrValidation
	}
	fields, err := normalizePortalSubmissionFormFields(form.Fields)
	if err != nil {
		return repo.PortalSubmissionForm{}, err
	}
	form.Fields = fields
	return form, nil
}

func normalizePortalSubmissionFormText(form repo.PortalSubmissionForm) repo.PortalSubmissionForm {
	form.Headline = bounded(strings.TrimSpace(form.Headline), 120)
	form.Description = bounded(strings.TrimSpace(form.Description), 1000)
	form.Acknowledgement = bounded(strings.TrimSpace(form.Acknowledgement), 500)
	form.SubmitButtonLabel = bounded(strings.TrimSpace(form.SubmitButtonLabel), 80)
	return form
}

func normalizePortalSubmissionFormFields(fields []repo.PortalSubmissionField) ([]repo.PortalSubmissionField, error) {
	seen := make(map[string]struct{}, len(fields))
	out := make([]repo.PortalSubmissionField, 0, len(fields))
	for i := range fields {
		field, err := normalizePortalSubmissionFormField(fields[i], seen)
		if err != nil {
			return nil, err
		}
		out = append(out, field)
	}
	return out, nil
}

func normalizePortalSubmissionFormField(field repo.PortalSubmissionField, seen map[string]struct{}) (repo.PortalSubmissionField, error) {
	field.Key = strings.ToLower(strings.TrimSpace(field.Key))
	field.Label = bounded(strings.TrimSpace(field.Label), 120)
	field.Placeholder = bounded(strings.TrimSpace(field.Placeholder), 160)
	if field.Key == "" || !portalFieldKeyPattern.MatchString(field.Key) || field.Label == "" {
		return repo.PortalSubmissionField{}, ErrValidation
	}
	if _, reserved := portalFieldReservedKeys[field.Key]; reserved {
		return repo.PortalSubmissionField{}, ErrValidation
	}
	if _, dup := seen[field.Key]; dup {
		return repo.PortalSubmissionField{}, ErrValidation
	}
	seen[field.Key] = struct{}{}
	if !portalSubmissionFieldKindValid(field.Kind) {
		return repo.PortalSubmissionField{}, ErrValidation
	}
	if err := normalizePortalSubmissionFormFieldOptions(ptrext.Of(field)); err != nil {
		return repo.PortalSubmissionField{}, err
	}
	return field, nil
}

func normalizePortalSubmissionFormFieldOptions(field *repo.PortalSubmissionField) error {
	if field.Kind == repo.PortalSubmissionFieldKindBoolean {
		field.Options = nil
		return nil
	}
	if field.Kind != repo.PortalSubmissionFieldKindSelect && field.Kind != repo.PortalSubmissionFieldKindMultiSelect {
		if len(field.Options) > 0 {
			return ErrValidation
		}
		return nil
	}
	if len(field.Options) == 0 || len(field.Options) > 12 {
		return ErrValidation
	}
	for i := range field.Options {
		field.Options[i] = bounded(strings.TrimSpace(field.Options[i]), 80)
		if field.Options[i] == "" {
			return ErrValidation
		}
	}
	return nil
}

func portalSubmissionFieldKindValid(kind repo.PortalSubmissionFieldKind) bool {
	switch kind {
	case repo.PortalSubmissionFieldKindText,
		repo.PortalSubmissionFieldKindTextarea,
		repo.PortalSubmissionFieldKindSelect,
		repo.PortalSubmissionFieldKindMultiSelect,
		repo.PortalSubmissionFieldKindBoolean:
		return true
	default:
		return false
	}
}
