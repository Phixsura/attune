// SPDX-License-Identifier: Apache-2.0

// Package customerrequest coordinates Customer Request validation, idempotency,
// merge semantics, and audit recording.
package customerrequest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	auditlogrepo "github.com/Phixsura/attune/internal/repo/auditlog"
	repo "github.com/Phixsura/attune/internal/repo/customerrequest"
	externalsyncrepo "github.com/Phixsura/attune/internal/repo/externalsync"
	"github.com/Phixsura/attune/internal/repo/idempotency"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

var (
	ErrValidation          = errors.New("customer request validation failed")
	ErrIdempotencyConflict = errors.New("customer request idempotency conflict")
	ErrRequestInProgress   = errors.New("customer request is already in progress")
	ErrUnsupportedProvider = errors.New("customer request issue provider unsupported")
	ErrInvalidIssueURL     = errors.New("customer request issue url invalid")
)

var (
	idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	currencyPattern       = regexp.MustCompile(`^[A-Z]{3}$`)
)

type AuditEntry struct {
	ID        int64
	Action    string
	ActorType string
	ActorID   string
	Summary   string
	CreatedAt time.Time
}

type DecisionRecord struct {
	AuditID                 int64
	Action                  string
	ActorType               string
	ActorID                 string
	Summary                 string
	CreatedAt               time.Time
	StatusChanged           bool
	OldStatus               repo.Status
	NewStatus               repo.Status
	PriorityChanged         bool
	OldPriority             repo.Priority
	NewPriority             repo.Priority
	OwnerChanged            bool
	OldOwnerMemberID        string
	NewOwnerMemberID        string
	TitleChanged            bool
	DescriptionChanged      bool
	HasDecisionSnapshot     bool
	DecisionScore           int
	DecisionScoreFactors    []repo.DecisionScoreFactor
	DeliveryHealth          repo.DeliveryHealth
	SupportingFeedbackCount int
	CustomerCount           int
	AccountCount            int
	VoteCount               int
	RevenueImpactCents      int64
	RevenueCurrency         string
	DecisionRationale       string
	OwnerMemberID           string
	OwnerDisplay            string
	EvidenceBundleRef       string
	PublicSafeState         string
	PublicSafeReasons       []string
}

type Detail struct {
	Request         repo.Detail
	AuditEntries    []AuditEntry
	DecisionRecords []DecisionRecord
}

type Service struct {
	repo          requestRepo
	idempotency   idempotency.Store
	audit         *auditlogsvc.Service
	notifications notificationSink
	issueCreates  issueCreateRunStore
	surveys       surveySink
}

// requestRepo is the repo surface the service consumes — an interface so
// unit tests can drive multi-step transactions (promote + attribution)
// against fakes; *repo.Repo satisfies it unchanged (PR #122 pattern).
type requestRepo interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	List(ctx context.Context, filter repo.ListFilter) (repo.ListResult, error)
	GetAccountSummary(ctx context.Context, filter repo.ListFilter) (repo.AccountSummary, error)
	GetDetail(ctx context.Context, tenantID string, id uuid.UUID, evidenceLimit int) (*repo.Detail, error)
	GetDetailTx(ctx context.Context, tx pgx.Tx, tenantID string, id uuid.UUID, evidenceLimit int) (*repo.Detail, error)
	GetOwner(ctx context.Context, tenantID string, ownerID uuid.UUID) (*repo.Owner, error)
	GetScoringSettings(ctx context.Context, tenantID string) (repo.ScoringSettings, error)
	UpsertScoringSettingsTx(ctx context.Context, tx pgx.Tx, in repo.ScoringSettingsInput) (repo.ScoringSettings, error)
	CreateTx(ctx context.Context, tx pgx.Tx, in repo.CreateInput) (*repo.Summary, error)
	UpdateTx(ctx context.Context, tx pgx.Tx, in repo.UpdateInput) (*repo.Summary, *repo.Summary, error)
	MergeTx(ctx context.Context, tx pgx.Tx, tenantID string, sourceID, targetID uuid.UUID, actorID string) (repo.MergeResult, error)
	LinkFeedbackTx(ctx context.Context, tx pgx.Tx, in repo.LinkFeedbackInput) error
	UnlinkFeedbackTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID uuid.UUID, feedbackID int64, actorID string) error
	FeedbackSourceMetaTx(ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64) (repo.FeedbackSourceMeta, error)
	LinkCustomerTx(ctx context.Context, tx pgx.Tx, in repo.CustomerLinkInput) (*repo.CustomerLink, error)
	UnlinkCustomerTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID, linkID uuid.UUID, actorID string) (*repo.CustomerLink, error)
	AddVoteTx(ctx context.Context, tx pgx.Tx, in repo.VoteInput) (*repo.Vote, error)
	RemoveVoteTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID, voteID uuid.UUID, actorID string) (*repo.Vote, error)
	AddNoteTx(ctx context.Context, tx pgx.Tx, in repo.NoteInput) (*repo.Note, error)
	DeleteNoteTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID, noteID uuid.UUID, actorID string) (*repo.Note, error)
	LinkIssueTx(ctx context.Context, tx pgx.Tx, in repo.IssueLinkInput) (*repo.IssueLink, error)
	UnlinkIssueTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID, issueLinkID uuid.UUID, actorID string) (*repo.IssueLink, error)
	RecordIssueSyncTx(ctx context.Context, tx pgx.Tx, in repo.IssueSyncInput) (*repo.IssueLink, error)
	BindIssueExternalObjectLinkTx(ctx context.Context, tx pgx.Tx, tenantID string, requestID, issueLinkID, externalObjectLinkID uuid.UUID) (*repo.IssueLink, error)
}

func New(r *repo.Repo, idem idempotency.Store, audit *auditlogsvc.Service) *Service {
	return ptrext.Of(Service{repo: r, idempotency: idem, audit: audit})
}

type notificationSink interface {
	RecordStatusChangeTx(
		ctx context.Context,
		tx pgx.Tx,
		tenantID string,
		requestID uuid.UUID,
		oldStatus string,
		newStatus string,
		actor auditlogsvc.Actor,
	) error
}

type issueCreateRunStore interface {
	ResolveGitHubIssueLinkTarget(
		ctx context.Context,
		in externalsyncrepo.GitHubIssueLinkTargetInput,
	) (*externalsyncrepo.GitHubIssueLinkTarget, error)
	BindManagedGitHubIssueLinkTx(
		ctx context.Context,
		tx pgx.Tx,
		in externalsyncrepo.ManagedGitHubIssueLinkInput,
	) (*externalsyncrepo.ManagedGitHubIssueLinkBinding, error)
	TombstoneLocalIssueExternalLinkTx(
		ctx context.Context,
		tx pgx.Tx,
		tenantID string,
		requestID uuid.UUID,
		externalObjectLinkID uuid.UUID,
	) error
	CreateCustomerRequestIssueRun(
		ctx context.Context,
		in externalsyncrepo.CustomerRequestIssueCreateRunInput,
	) (*externalsyncrepo.CustomerRequestIssueCreateRunResult, error)
	CreateCustomerRequestIssuePullRun(
		ctx context.Context,
		in externalsyncrepo.CustomerRequestIssuePullRunInput,
	) (*externalsyncrepo.CustomerRequestIssuePullRunResult, error)
}

func (s *Service) SetNotificationSink(sink notificationSink) {
	s.notifications = sink
}

func (s *Service) SetIssueCreateRunStore(store issueCreateRunStore) {
	s.issueCreates = store
}

type SurveyRequestResolvedEvent struct {
	TenantID  string
	RequestID uuid.UUID
	OldStatus string
	NewStatus string
	Title     string
	ActorID   string
}

type surveySink interface {
	RecordRequestResolved(ctx context.Context, event SurveyRequestResolvedEvent) (int, error)
}

func (s *Service) SetSurveySink(sink surveySink) {
	s.surveys = sink
}

type ListInput struct {
	TenantID      string
	Query         string
	Statuses      []repo.Status
	Priorities    []repo.Priority
	OwnerMemberID *uuid.UUID
	Visibility    repo.Visibility
	Sort          repo.Sort
	Direction     repo.Direction
	Limit         int
	Cursor        string
	FeedbackID    int64
	CohortID      *string
	AccountKey    string
}

type AccountSummaryInput struct {
	TenantID      string
	Query         string
	Statuses      []repo.Status
	Priorities    []repo.Priority
	OwnerMemberID *uuid.UUID
	Visibility    repo.Visibility
	Sort          repo.Sort
	Direction     repo.Direction
	FeedbackID    int64
	CohortID      *string
	AccountKey    string
	TimelineLimit int
	EventLimit    int
}

type ScoringSettingsInput struct {
	TenantID             string
	PriorityNoneWeight   *int
	PriorityLowWeight    *int
	PriorityMediumWeight *int
	PriorityHighWeight   *int
	PriorityUrgentWeight *int
	FeedbackWeight       *int
	FeedbackCap          *int
	CustomerWeight       *int
	CustomerCap          *int
	AccountWeight        *int
	AccountCap           *int
	VoteWeight           *int
	VoteCap              *int
	RevenueCentsPerPoint *int64
	RevenueCap           *int
	Actor                auditlogsvc.Actor
}

type CreateInput struct {
	TenantID       string
	Title          string
	Description    string
	Status         repo.Status
	Priority       repo.Priority
	OwnerMemberID  *uuid.UUID
	IdempotencyKey string
	Actor          auditlogsvc.Actor
}

type UpdateInput struct {
	TenantID         string
	ID               uuid.UUID
	Title            *string
	Description      *string
	Status           *repo.Status
	Priority         *repo.Priority
	OwnerMemberIDSet bool
	OwnerMemberID    *uuid.UUID
	Actor            auditlogsvc.Actor
}

type PromoteInput struct {
	TenantID       string
	FeedbackIDs    []int64
	Title          string
	Description    string
	Status         repo.Status
	Priority       repo.Priority
	OwnerMemberID  *uuid.UUID
	IdempotencyKey string
	Actor          auditlogsvc.Actor
}

type LinkFeedbackInput struct {
	TenantID   string
	RequestID  uuid.UUID
	FeedbackID int64
	Importance repo.Importance
	Note       string
	Actor      auditlogsvc.Actor
}

type LinkCustomerInput struct {
	TenantID       string
	RequestID      uuid.UUID
	SubjectKey     string
	SubjectHash    string
	SubjectDisplay string
	AccountKey     string
	AccountDisplay string
	Note           string
	AccountProfile AccountProfileInput
	Actor          auditlogsvc.Actor
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
	AccountProfile AccountProfileInput
	Actor          auditlogsvc.Actor
}

type NoteInput struct {
	TenantID  string
	RequestID uuid.UUID
	Body      string
	Actor     auditlogsvc.Actor
}

type AccountProfileInput struct {
	RevenueCents    *int64
	RevenueCurrency string
	Tier            string
	SizeSegment     string
	LifecycleStatus string
	CRMProvider     string
	CRMExternalID   string
}

type MergeInput struct {
	TenantID       string
	SourceID       uuid.UUID
	TargetID       uuid.UUID
	IdempotencyKey string
	Actor          auditlogsvc.Actor
}

type LinkIssueInput struct {
	TenantID     string
	RequestID    uuid.UUID
	Provider     string
	ExternalURL  string
	ExternalKey  string
	Title        string
	Status       string
	ConnectionID *uuid.UUID
	MappingID    *uuid.UUID
	IssueNumber  string
	Actor        auditlogsvc.Actor
}

type CreateGitHubIssueInput struct {
	TenantID     string
	RequestID    uuid.UUID
	ConnectionID *uuid.UUID
	MappingID    *uuid.UUID
	Actor        auditlogsvc.Actor
}

type CreateGitHubIssueResult struct {
	Detail       *Detail
	RunID        uuid.UUID
	ConnectionID uuid.UUID
	MappingID    uuid.UUID
}

type IssueSyncInput struct {
	TenantID               string
	RequestID              uuid.UUID
	IssueLinkID            uuid.UUID
	SyncState              repo.IssueSyncState
	Status                 string
	ExternalStatusCategory string
	ExternalAssignee       string
	ExternalUpdatedAt      string
	SyncError              string
	Actor                  auditlogsvc.Actor
}

func (s *Service) List(ctx context.Context, in ListInput) (repo.ListResult, error) {
	if in.TenantID == "" {
		return repo.ListResult{}, ErrValidation
	}
	return s.repo.List(ctx, repo.ListFilter{
		TenantID:      in.TenantID,
		Query:         in.Query,
		Statuses:      in.Statuses,
		Priorities:    in.Priorities,
		OwnerMemberID: in.OwnerMemberID,
		Visibility:    defaultVisibility(in.Visibility),
		Sort:          defaultSort(in.Sort),
		Direction:     defaultDirection(in.Direction),
		Limit:         in.Limit,
		Cursor:        in.Cursor,
		FeedbackID:    in.FeedbackID,
		CohortID:      in.CohortID,
		AccountKey:    strings.TrimSpace(in.AccountKey),
	})
}

func (s *Service) GetAccountSummary(ctx context.Context, in AccountSummaryInput) (repo.AccountSummary, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	accountKey := strings.TrimSpace(in.AccountKey)
	if tenantID == "" || accountKey == "" {
		return repo.AccountSummary{}, ErrValidation
	}
	return s.repo.GetAccountSummary(ctx, repo.ListFilter{
		TenantID:      tenantID,
		Query:         in.Query,
		Statuses:      in.Statuses,
		Priorities:    in.Priorities,
		OwnerMemberID: in.OwnerMemberID,
		Visibility:    defaultVisibility(in.Visibility),
		Sort:          defaultSort(in.Sort),
		Direction:     defaultDirection(in.Direction),
		FeedbackID:    in.FeedbackID,
		CohortID:      in.CohortID,
		AccountKey:    accountKey,
		Limit:         in.TimelineLimit,
		EventLimit:    in.EventLimit,
	})
}

func (s *Service) GetScoringSettings(ctx context.Context, tenantID string) (repo.ScoringSettings, error) {
	if strings.TrimSpace(tenantID) == "" {
		return repo.ScoringSettings{}, ErrValidation
	}
	return s.repo.GetScoringSettings(ctx, strings.TrimSpace(tenantID))
}

func (s *Service) UpdateScoringSettings(ctx context.Context, in ScoringSettingsInput) (repo.ScoringSettings, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		return repo.ScoringSettings{}, ErrValidation
	}
	before, err := s.repo.GetScoringSettings(ctx, tenantID)
	if err != nil {
		return repo.ScoringSettings{}, err
	}
	normalized, err := normalizeScoringSettings(in, before)
	if err != nil {
		return repo.ScoringSettings{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return repo.ScoringSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	after, err := s.repo.UpsertScoringSettingsTx(ctx, tx, normalized)
	if err != nil {
		return repo.ScoringSettings{}, err
	}
	if err := s.recordScoringSettingsAuditTx(ctx, tx, normalized.TenantID, in.Actor, before, after); err != nil {
		return repo.ScoringSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return repo.ScoringSettings{}, err
	}
	return after, nil
}

func (s *Service) Get(ctx context.Context, tenantID string, id uuid.UUID, evidenceLimit int) (*Detail, error) {
	return s.detail(ctx, tenantID, id, evidenceLimit)
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*Detail, error) {
	normalized, err := normalizeCreate(in)
	if err != nil {
		return nil, err
	}
	if err := s.validateOwner(ctx, normalized.TenantID, normalized.OwnerMemberID); err != nil {
		return nil, err
	}
	cached, acquired, err := s.acquireIdempotency(ctx, normalized.TenantID, normalized.IdempotencyKey, "create", createIdempotencyPayload(normalized))
	if err != nil || cached != nil {
		return cached, err
	}
	detail, err := s.createInTransaction(ctx, normalized, "customer_request.create", nil)
	return s.completeIdempotency(ctx, normalized.TenantID, normalized.IdempotencyKey, acquired, detail, err)
}

func (s *Service) Update(ctx context.Context, in UpdateInput) (*Detail, error) {
	normalized, err := normalizeUpdate(in)
	if err != nil {
		return nil, err
	}
	if normalized.OwnerMemberIDSet {
		if err := s.validateOwner(ctx, normalized.TenantID, normalized.OwnerMemberID); err != nil {
			return nil, err
		}
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	before, after, err := s.repo.UpdateTx(ctx, tx, repo.UpdateInput{
		TenantID:         normalized.TenantID,
		ID:               normalized.ID,
		Title:            normalized.Title,
		Description:      normalized.Description,
		Status:           normalized.Status,
		Priority:         normalized.Priority,
		OwnerMemberIDSet: normalized.OwnerMemberIDSet,
		OwnerMemberID:    normalized.OwnerMemberID,
		ActorID:          normalized.Actor.ID,
	})
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, normalized.Actor, "customer_request.update", ptrext.Indirect(after),
		"Updated customer request", updateAuditBeforeAfter(ptrext.Indirect(before), ptrext.Indirect(after))); err != nil {
		return nil, err
	}
	beforeSummary := ptrext.Indirect(before)
	afterSummary := ptrext.Indirect(after)
	if s.notifications != nil && beforeSummary.Status != afterSummary.Status {
		if err := s.notifications.RecordStatusChangeTx(ctx, tx, normalized.TenantID, normalized.ID,
			string(beforeSummary.Status), string(afterSummary.Status), normalized.Actor); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.recordSurveyRequestResolved(ctx, normalized.Actor, beforeSummary, afterSummary)
	return s.detail(ctx, normalized.TenantID, normalized.ID, 50)
}

func (s *Service) recordSurveyRequestResolved(
	ctx context.Context,
	actor auditlogsvc.Actor,
	before repo.Summary,
	after repo.Summary,
) {
	const where = "service.customerrequest.recordSurveyRequestResolved"
	if s.surveys == nil || before.Status == after.Status {
		return
	}
	_, err := s.surveys.RecordRequestResolved(ctx, SurveyRequestResolvedEvent{
		TenantID:  after.TenantID,
		RequestID: after.ID,
		OldStatus: string(before.Status),
		NewStatus: string(after.Status),
		Title:     after.Title,
		ActorID:   actor.ID,
	})
	if err != nil {
		logext.Warnf(ctx, "[%s] survey trigger failed,tenant_id:%s,request_id:%s,err:%+v",
			where, after.TenantID, after.ID, err.Error())
	}
}

func (s *Service) PromoteFeedback(ctx context.Context, in PromoteInput) (*Detail, error) {
	normalized, err := normalizePromote(in)
	if err != nil {
		return nil, err
	}
	if err := s.validateOwner(ctx, normalized.TenantID, normalized.OwnerMemberID); err != nil {
		return nil, err
	}
	cached, acquired, err := s.acquireIdempotency(ctx, normalized.TenantID, normalized.IdempotencyKey, "promote", promoteIdempotencyPayload(normalized))
	if err != nil || cached != nil {
		return cached, err
	}
	detail, err := s.promoteInTransaction(ctx, normalized)
	return s.completeIdempotency(ctx, normalized.TenantID, normalized.IdempotencyKey, acquired, detail, err)
}

func (s *Service) LinkFeedback(ctx context.Context, in LinkFeedbackInput) (*Detail, error) {
	normalized, err := normalizeLinkFeedback(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.repo.LinkFeedbackTx(ctx, tx, repo.LinkFeedbackInput{
		TenantID:   normalized.TenantID,
		RequestID:  normalized.RequestID,
		FeedbackID: normalized.FeedbackID,
		Importance: normalized.Importance,
		Note:       normalized.Note,
		ActorID:    normalized.Actor.ID,
	}); err != nil {
		return nil, err
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, normalized.TenantID, normalized.RequestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, normalized.Actor, "customer_request.link_feedback", summary.Summary,
		"Linked feedback to customer request", map[string]any{
			"request_id":  normalized.RequestID.String(),
			"feedback_id": normalized.FeedbackID,
			"importance":  normalized.Importance,
			"note_length": utf8.RuneCountInString(normalized.Note),
		}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, normalized.TenantID, normalized.RequestID, 50)
}

func (s *Service) UnlinkFeedback(ctx context.Context, tenantID string, requestID uuid.UUID, feedbackID int64, actor auditlogsvc.Actor) (*Detail, error) {
	if tenantID == "" || feedbackID <= 0 {
		return nil, ErrValidation
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.repo.UnlinkFeedbackTx(ctx, tx, tenantID, requestID, feedbackID, actor.ID); err != nil {
		return nil, err
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, tenantID, requestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, actor, "customer_request.unlink_feedback", summary.Summary,
		"Unlinked feedback from customer request", map[string]any{
			"request_id":  requestID.String(),
			"feedback_id": feedbackID,
		}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, tenantID, requestID, 50)
}

func (s *Service) LinkCustomer(ctx context.Context, in LinkCustomerInput) (*Detail, error) {
	normalized, err := normalizeCustomerLink(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	link, err := s.repo.LinkCustomerTx(ctx, tx, repo.CustomerLinkInput{
		TenantID:       normalized.TenantID,
		RequestID:      normalized.RequestID,
		SubjectKey:     normalized.SubjectKey,
		SubjectHash:    normalized.SubjectHash,
		SubjectDisplay: normalized.SubjectDisplay,
		AccountKey:     normalized.AccountKey,
		AccountDisplay: normalized.AccountDisplay,
		Note:           normalized.Note,
		ActorID:        normalized.Actor.ID,
		AccountProfile: repo.AccountProfileInput{
			AccountKey:      normalized.AccountKey,
			AccountDisplay:  normalized.AccountDisplay,
			RevenueCents:    accountRevenueCents(normalized.AccountProfile),
			RevenueCurrency: normalized.AccountProfile.RevenueCurrency,
			Tier:            normalized.AccountProfile.Tier,
			SizeSegment:     normalized.AccountProfile.SizeSegment,
			LifecycleStatus: normalized.AccountProfile.LifecycleStatus,
			CRMProvider:     normalized.AccountProfile.CRMProvider,
			CRMExternalID:   normalized.AccountProfile.CRMExternalID,
			Source:          "manual",
			ActorID:         normalized.Actor.ID,
		},
	})
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, normalized.TenantID, normalized.RequestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, normalized.Actor, "customer_request.link_customer", summary.Summary,
		"Linked customer to customer request", customerAuditMetadata(normalized.RequestID, ptrext.Indirect(link))); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, normalized.TenantID, normalized.RequestID, 50)
}

func (s *Service) UnlinkCustomer(ctx context.Context, tenantID string, requestID, linkID uuid.UUID, actor auditlogsvc.Actor) (*Detail, error) {
	if tenantID == "" || requestID == uuid.Nil || linkID == uuid.Nil {
		return nil, ErrValidation
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	link, err := s.repo.UnlinkCustomerTx(ctx, tx, tenantID, requestID, linkID, actor.ID)
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, tenantID, requestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, actor, "customer_request.unlink_customer", summary.Summary,
		"Unlinked customer from customer request", customerAuditMetadata(requestID, ptrext.Indirect(link))); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, tenantID, requestID, 50)
}

func (s *Service) AddVote(ctx context.Context, in VoteInput) (*Detail, error) {
	normalized, err := normalizeVote(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	vote, err := s.repo.AddVoteTx(ctx, tx, repo.VoteInput{
		TenantID:       normalized.TenantID,
		RequestID:      normalized.RequestID,
		SubjectKey:     normalized.SubjectKey,
		SubjectHash:    normalized.SubjectHash,
		SubjectDisplay: normalized.SubjectDisplay,
		AccountKey:     normalized.AccountKey,
		AccountDisplay: normalized.AccountDisplay,
		Weight:         normalized.Weight,
		Note:           normalized.Note,
		ActorID:        normalized.Actor.ID,
		AccountProfile: repo.AccountProfileInput{
			AccountKey:      normalized.AccountKey,
			AccountDisplay:  normalized.AccountDisplay,
			RevenueCents:    accountRevenueCents(normalized.AccountProfile),
			RevenueCurrency: normalized.AccountProfile.RevenueCurrency,
			Tier:            normalized.AccountProfile.Tier,
			SizeSegment:     normalized.AccountProfile.SizeSegment,
			LifecycleStatus: normalized.AccountProfile.LifecycleStatus,
			CRMProvider:     normalized.AccountProfile.CRMProvider,
			CRMExternalID:   normalized.AccountProfile.CRMExternalID,
			Source:          "manual",
			ActorID:         normalized.Actor.ID,
		},
	})
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, normalized.TenantID, normalized.RequestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, normalized.Actor, "customer_request.add_vote", summary.Summary,
		"Added vote to customer request", voteAuditMetadata(normalized.RequestID, ptrext.Indirect(vote))); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, normalized.TenantID, normalized.RequestID, 50)
}

func (s *Service) RemoveVote(ctx context.Context, tenantID string, requestID, voteID uuid.UUID, actor auditlogsvc.Actor) (*Detail, error) {
	if tenantID == "" || requestID == uuid.Nil || voteID == uuid.Nil {
		return nil, ErrValidation
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	vote, err := s.repo.RemoveVoteTx(ctx, tx, tenantID, requestID, voteID, actor.ID)
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, tenantID, requestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, actor, "customer_request.remove_vote", summary.Summary,
		"Removed vote from customer request", voteAuditMetadata(requestID, ptrext.Indirect(vote))); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, tenantID, requestID, 50)
}

func (s *Service) AddNote(ctx context.Context, in NoteInput) (*Detail, error) {
	normalized, err := normalizeNote(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	note, err := s.repo.AddNoteTx(ctx, tx, repo.NoteInput{
		TenantID:  normalized.TenantID,
		RequestID: normalized.RequestID,
		Body:      normalized.Body,
		ActorID:   normalized.Actor.ID,
	})
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, normalized.TenantID, normalized.RequestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, normalized.Actor, "customer_request.add_note", summary.Summary,
		"Added note to customer request", noteAuditMetadata(normalized.RequestID, ptrext.Indirect(note))); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, normalized.TenantID, normalized.RequestID, 50)
}

func (s *Service) DeleteNote(ctx context.Context, tenantID string, requestID, noteID uuid.UUID, actor auditlogsvc.Actor) (*Detail, error) {
	if tenantID == "" || requestID == uuid.Nil || noteID == uuid.Nil {
		return nil, ErrValidation
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	note, err := s.repo.DeleteNoteTx(ctx, tx, tenantID, requestID, noteID, actor.ID)
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, tenantID, requestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, actor, "customer_request.delete_note", summary.Summary,
		"Deleted note from customer request", noteAuditMetadata(requestID, ptrext.Indirect(note))); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, tenantID, requestID, 50)
}

func (s *Service) Merge(ctx context.Context, in MergeInput) (*Detail, error) {
	if in.TenantID == "" || in.SourceID == uuid.Nil || in.TargetID == uuid.Nil || in.SourceID == in.TargetID {
		return nil, ErrValidation
	}
	if !idempotencyKeyPattern.MatchString(strings.TrimSpace(in.IdempotencyKey)) {
		return nil, ErrValidation
	}
	cached, acquired, err := s.acquireIdempotency(ctx, in.TenantID, in.IdempotencyKey, "merge", mergeIdempotencyPayload(in))
	if err != nil || cached != nil {
		return cached, err
	}
	detail, err := s.mergeInTransaction(ctx, in)
	return s.completeIdempotency(ctx, in.TenantID, in.IdempotencyKey, acquired, detail, err)
}

func (s *Service) LinkIssue(ctx context.Context, in LinkIssueInput) (*Detail, error) {
	prepared, err := s.resolveManagedIssueLinkTarget(ctx, in)
	if err != nil {
		return nil, err
	}
	normalized, err := normalizeIssueInput(prepared)
	if err != nil {
		return nil, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	link, err := s.repo.LinkIssueTx(ctx, tx, repo.IssueLinkInput{
		TenantID:    normalized.TenantID,
		RequestID:   normalized.RequestID,
		Provider:    normalized.Provider,
		ExternalKey: normalized.ExternalKey,
		ExternalURL: normalized.ExternalURL,
		Title:       normalized.Title,
		Status:      normalized.Status,
		MappingID:   normalized.MappingID,
		ActorID:     normalized.Actor.ID,
	})
	if err != nil {
		return nil, err
	}
	pullTarget, boundLink, err := s.bindManagedIssueLinkTx(ctx, tx, normalized, ptrext.Indirect(link))
	if err != nil {
		return nil, err
	}
	if boundLink != nil {
		link = boundLink
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, normalized.TenantID, normalized.RequestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, normalized.Actor, "customer_request.link_issue", summary.Summary,
		"Linked issue to customer request", issueAuditMetadata(normalized.RequestID, ptrext.Indirect(link))); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	s.enqueueManagedIssuePull(ctx, normalized, pullTarget)
	return s.detail(ctx, normalized.TenantID, normalized.RequestID, 50)
}

func (s *Service) resolveManagedIssueLinkTarget(ctx context.Context, in LinkIssueInput) (LinkIssueInput, error) {
	in.IssueNumber = strings.TrimSpace(in.IssueNumber)
	if in.IssueNumber == "" {
		return in, nil
	}
	if strings.TrimSpace(in.ExternalURL) != "" {
		return LinkIssueInput{}, ErrValidation
	}
	if in.ConnectionID == nil {
		return LinkIssueInput{}, ErrValidation
	}
	if strings.TrimSpace(in.Provider) != "" && !strings.EqualFold(strings.TrimSpace(in.Provider), "github") {
		return LinkIssueInput{}, ErrUnsupportedProvider
	}
	if s.issueCreates == nil {
		return LinkIssueInput{}, ErrValidation
	}
	target, err := s.issueCreates.ResolveGitHubIssueLinkTarget(ctx, externalsyncrepo.GitHubIssueLinkTargetInput{
		TenantID:     in.TenantID,
		ConnectionID: ptrext.Indirect(in.ConnectionID),
		MappingID:    in.MappingID,
		IssueNumber:  in.IssueNumber,
	})
	if err != nil {
		return LinkIssueInput{}, mapManagedIssueLinkError(err)
	}
	in.Provider = "github"
	in.ExternalURL = target.ExternalURL
	if strings.TrimSpace(in.ExternalKey) == "" {
		in.ExternalKey = target.ExternalKey
	}
	if strings.TrimSpace(in.Title) == "" {
		in.Title = target.Title
	}
	if strings.TrimSpace(in.Status) == "" {
		in.Status = target.Status
	}
	in.MappingID = ptrext.Of(target.MappingID)
	in.IssueNumber = target.ExternalSyncKey
	return in, nil
}

func (s *Service) bindManagedIssueLinkTx(
	ctx context.Context,
	tx pgx.Tx,
	in LinkIssueInput,
	link repo.IssueLink,
) (*externalsyncrepo.ManagedIssueSyncTarget, *repo.IssueLink, error) {
	if s.issueCreates == nil {
		return nil, nil, nil
	}
	binding, err := s.issueCreates.BindManagedGitHubIssueLinkTx(ctx, tx, externalsyncrepo.ManagedGitHubIssueLinkInput{
		TenantID:    in.TenantID,
		RequestID:   in.RequestID,
		Provider:    in.Provider,
		ExternalKey: in.ExternalKey,
		ExternalURL: in.ExternalURL,
		MappingID:   in.MappingID,
	})
	if err != nil || binding == nil {
		return nil, nil, mapManagedIssueLinkError(err)
	}
	updated, err := s.repo.BindIssueExternalObjectLinkTx(ctx, tx, in.TenantID, in.RequestID, link.ID, binding.ExternalObjectLinkID)
	if err != nil {
		return nil, nil, err
	}
	return ptrext.Of(externalsyncrepo.ManagedIssueSyncTarget{
		ConnectionID: binding.ConnectionID,
		MappingID:    binding.MappingID,
		ExternalKey:  binding.ExternalKey,
	}), updated, nil
}

func (s *Service) enqueueManagedIssuePull(ctx context.Context, in LinkIssueInput, target *externalsyncrepo.ManagedIssueSyncTarget) {
	if s.issueCreates == nil || !strings.EqualFold(in.Provider, "github") || target == nil {
		return
	}
	managed := ptrext.Indirect(target)
	if managed.ConnectionID == uuid.Nil || managed.MappingID == uuid.Nil ||
		strings.TrimSpace(managed.ExternalKey) == "" {
		return
	}
	_, err := s.issueCreates.CreateCustomerRequestIssuePullRun(ctx, externalsyncrepo.CustomerRequestIssuePullRunInput{
		TenantID:     in.TenantID,
		RequestID:    in.RequestID,
		ConnectionID: managed.ConnectionID,
		MappingID:    managed.MappingID,
		ExternalKey:  managed.ExternalKey,
		ActorID:      in.Actor.ID,
	})
	if err != nil {
		logext.Warnf(ctx, "[customer_request.link_issue] enqueue managed issue pull failed,tenant_id:%s,request_id:%s,mapping_id:%s,external_key:%s,err:%s",
			in.TenantID, in.RequestID.String(), managed.MappingID.String(), managed.ExternalKey, err.Error())
	}
}

func (s *Service) CreateGitHubIssue(ctx context.Context, in CreateGitHubIssueInput) (*CreateGitHubIssueResult, error) {
	if in.TenantID == "" || in.RequestID == uuid.Nil || in.Actor.ID == "" || s.issueCreates == nil {
		return nil, ErrValidation
	}
	current, err := s.detail(ctx, in.TenantID, in.RequestID, 0)
	if err != nil {
		return nil, err
	}
	if hasGitHubIssueLink(current) {
		return nil, repo.ErrConflict
	}
	result, err := s.issueCreates.CreateCustomerRequestIssueRun(ctx, externalsyncrepo.CustomerRequestIssueCreateRunInput{
		TenantID:     in.TenantID,
		RequestID:    in.RequestID,
		ConnectionID: in.ConnectionID,
		MappingID:    in.MappingID,
		ActorID:      in.Actor.ID,
	})
	if err != nil {
		return nil, mapIssueCreateRunError(err)
	}
	if err := s.recordAudit(ctx, in.Actor, "customer_request.create_github_issue", current.Request.Summary,
		"Queued GitHub issue creation for customer request",
		map[string]any{
			"request_id":    in.RequestID.String(),
			"connection_id": result.Mapping.ConnectionID.String(),
			"mapping_id":    result.Mapping.ID.String(),
			"run_id":        result.Run.ID.String(),
		}); err != nil {
		logext.Warnf(ctx, "[customer_request.create_github_issue] record audit failed,tenant_id:%s,request_id:%s,run_id:%s,err:%s",
			in.TenantID, in.RequestID.String(), result.Run.ID.String(), err.Error())
	}
	detail, err := s.detail(ctx, in.TenantID, in.RequestID, 50)
	if err != nil {
		return nil, err
	}
	return ptrext.Of(CreateGitHubIssueResult{
		Detail:       detail,
		RunID:        result.Run.ID,
		ConnectionID: result.Mapping.ConnectionID,
		MappingID:    result.Mapping.ID,
	}), nil
}

func (s *Service) UnlinkIssue(ctx context.Context, tenantID string, requestID, issueLinkID uuid.UUID, actor auditlogsvc.Actor) (*Detail, error) {
	if tenantID == "" || requestID == uuid.Nil || issueLinkID == uuid.Nil {
		return nil, ErrValidation
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	link, err := s.repo.UnlinkIssueTx(ctx, tx, tenantID, requestID, issueLinkID, actor.ID)
	if err != nil {
		return nil, err
	}
	if err := s.tombstoneManagedIssueLinkTx(ctx, tx, tenantID, requestID, ptrext.Indirect(link)); err != nil {
		return nil, err
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, tenantID, requestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, actor, "customer_request.unlink_issue", summary.Summary,
		"Unlinked issue from customer request", issueAuditMetadata(requestID, ptrext.Indirect(link))); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, tenantID, requestID, 50)
}

func (s *Service) tombstoneManagedIssueLinkTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	requestID uuid.UUID,
	link repo.IssueLink,
) error {
	if s.issueCreates == nil || link.ExternalObjectLinkID == nil {
		return nil
	}
	err := s.issueCreates.TombstoneLocalIssueExternalLinkTx(ctx, tx, tenantID, requestID, ptrext.Indirect(link.ExternalObjectLinkID))
	return mapManagedIssueLinkError(err)
}

func (s *Service) RecordIssueSync(ctx context.Context, in IssueSyncInput) (*Detail, error) {
	normalized, err := normalizeIssueSync(in)
	if err != nil {
		return nil, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	link, err := s.repo.RecordIssueSyncTx(ctx, tx, repo.IssueSyncInput{
		TenantID:               normalized.TenantID,
		RequestID:              normalized.RequestID,
		IssueLinkID:            normalized.IssueLinkID,
		SyncState:              normalized.SyncState,
		Status:                 normalized.Status,
		ExternalStatusCategory: normalized.ExternalStatusCategory,
		ExternalAssignee:       normalized.ExternalAssignee,
		ExternalUpdatedAt:      parseOptionalTime(normalized.ExternalUpdatedAt),
		SyncError:              normalized.SyncError,
		ActorID:                normalized.Actor.ID,
	})
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.GetDetailTx(ctx, tx, normalized.TenantID, normalized.RequestID, 0)
	if err != nil {
		return nil, err
	}
	if err := s.recordAuditTx(ctx, tx, normalized.Actor, "customer_request.record_issue_sync", summary.Summary,
		"Recorded issue sync state", issueAuditMetadata(normalized.RequestID, ptrext.Indirect(link))); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, normalized.TenantID, normalized.RequestID, 50)
}

func (s *Service) createInTransaction(ctx context.Context, in CreateInput, action string, extra map[string]any) (*Detail, error) {
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	created, err := s.repo.CreateTx(ctx, tx, repo.CreateInput{
		TenantID:      in.TenantID,
		Title:         in.Title,
		Description:   in.Description,
		Status:        in.Status,
		Priority:      in.Priority,
		OwnerMemberID: in.OwnerMemberID,
		ActorID:       in.Actor.ID,
	})
	if err != nil {
		return nil, err
	}
	after := createAuditMetadata(ptrext.Indirect(created), in.IdempotencyKey)
	for k, v := range extra {
		after[k] = v
	}
	if err := s.recordAuditTx(ctx, tx, in.Actor, action, ptrext.Indirect(created),
		createAuditSummary(action, ptrext.Indirect(created)), after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, in.TenantID, created.ID, 50)
}

func (s *Service) promoteInTransaction(ctx context.Context, in PromoteInput) (*Detail, error) {
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	created, err := s.repo.CreateTx(ctx, tx, repo.CreateInput{
		TenantID:      in.TenantID,
		Title:         in.Title,
		Description:   in.Description,
		Status:        in.Status,
		Priority:      in.Priority,
		OwnerMemberID: in.OwnerMemberID,
		ActorID:       in.Actor.ID,
	})
	if err != nil {
		return nil, err
	}
	for _, feedbackID := range in.FeedbackIDs {
		if err := s.repo.LinkFeedbackTx(ctx, tx, repo.LinkFeedbackInput{
			TenantID:   in.TenantID,
			RequestID:  created.ID,
			FeedbackID: feedbackID,
			Importance: repo.ImportanceNormal,
			ActorID:    in.Actor.ID,
		}); err != nil {
			return nil, err
		}
	}
	// Auto-attribution: inbound-channel feedback carries contact/company
	// identity in source_meta — link those customers so the promoted
	// request already knows who asked and what they are worth.
	autoLinked := s.autoLinkCustomersTx(ctx, tx, in.TenantID, created.ID, in.FeedbackIDs, in.Actor.ID)
	createdDetail, err := s.repo.GetDetailTx(ctx, tx, in.TenantID, created.ID, 0)
	if err != nil {
		return nil, err
	}
	auditSummary := createdDetail.Summary
	after := createAuditMetadata(auditSummary, in.IdempotencyKey)
	after["feedback_ids"] = in.FeedbackIDs
	after["feedback_count"] = len(in.FeedbackIDs)
	if autoLinked > 0 {
		after["auto_linked_customers"] = autoLinked
	}
	if err := s.recordAuditTx(ctx, tx, in.Actor, "customer_request.promote_feedback", auditSummary,
		"Promoted feedback to customer request", after); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, in.TenantID, created.ID, 50)
}

func (s *Service) mergeInTransaction(ctx context.Context, in MergeInput) (*Detail, error) {
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := s.repo.MergeTx(ctx, tx, in.TenantID, in.SourceID, in.TargetID, in.Actor.ID)
	if err != nil {
		return nil, err
	}
	if !result.AlreadyMergedIntoTarget {
		if err := s.recordMergeAuditTx(ctx, tx, in.TenantID, in.Actor, result); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.detail(ctx, in.TenantID, in.TargetID, 50)
}

func (s *Service) validateOwner(ctx context.Context, tenantID string, ownerID *uuid.UUID) error {
	if ownerID == nil {
		return nil
	}
	_, err := s.repo.GetOwner(ctx, tenantID, ptrext.Indirect(ownerID))
	return err
}

func normalizeCreate(in CreateInput) (CreateInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.Status = defaultStatus(in.Status)
	in.Priority = defaultPriority(in.Priority)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	if err := validateCreateFields(in.TenantID, in.Title, in.Description, in.Status, in.Priority, in.IdempotencyKey); err != nil {
		return CreateInput{}, err
	}
	return in, nil
}

func normalizePromote(in PromoteInput) (PromoteInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Description = strings.TrimSpace(in.Description)
	in.Status = defaultStatus(in.Status)
	in.Priority = defaultPriority(in.Priority)
	in.IdempotencyKey = strings.TrimSpace(in.IdempotencyKey)
	ids := dedupeFeedbackIDs(in.FeedbackIDs)
	if len(ids) == 0 || len(ids) > 100 {
		return PromoteInput{}, ErrValidation
	}
	in.FeedbackIDs = ids
	if err := validateCreateFields(in.TenantID, in.Title, in.Description, in.Status, in.Priority, in.IdempotencyKey); err != nil {
		return PromoteInput{}, err
	}
	return in, nil
}

func normalizeScoringSettings(
	in ScoringSettingsInput,
	current repo.ScoringSettings,
) (repo.ScoringSettingsInput, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	if tenantID == "" {
		return repo.ScoringSettingsInput{}, ErrValidation
	}
	out := repo.ScoringSettingsInput{
		TenantID:             tenantID,
		PriorityNoneWeight:   patchInt(current.PriorityNoneWeight, in.PriorityNoneWeight),
		PriorityLowWeight:    patchInt(current.PriorityLowWeight, in.PriorityLowWeight),
		PriorityMediumWeight: patchInt(current.PriorityMediumWeight, in.PriorityMediumWeight),
		PriorityHighWeight:   patchInt(current.PriorityHighWeight, in.PriorityHighWeight),
		PriorityUrgentWeight: patchInt(current.PriorityUrgentWeight, in.PriorityUrgentWeight),
		FeedbackWeight:       patchInt(current.FeedbackWeight, in.FeedbackWeight),
		FeedbackCap:          patchInt(current.FeedbackCap, in.FeedbackCap),
		CustomerWeight:       patchInt(current.CustomerWeight, in.CustomerWeight),
		CustomerCap:          patchInt(current.CustomerCap, in.CustomerCap),
		AccountWeight:        patchInt(current.AccountWeight, in.AccountWeight),
		AccountCap:           patchInt(current.AccountCap, in.AccountCap),
		VoteWeight:           patchInt(current.VoteWeight, in.VoteWeight),
		VoteCap:              patchInt(current.VoteCap, in.VoteCap),
		RevenueCentsPerPoint: patchInt64(current.RevenueCentsPerPoint, in.RevenueCentsPerPoint),
		RevenueCap:           patchInt(current.RevenueCap, in.RevenueCap),
		ActorID:              strings.TrimSpace(in.Actor.ID),
	}
	if out.ActorID == "" {
		out.ActorID = "system"
	}
	for _, check := range scoringWeightChecks(out) {
		if !validWeight(check.value, check.limit) {
			return repo.ScoringSettingsInput{}, ErrValidation
		}
	}
	if out.RevenueCentsPerPoint <= 0 || out.RevenueCentsPerPoint > 100000000000 {
		return repo.ScoringSettingsInput{}, ErrValidation
	}
	return out, nil
}

func scoringWeightChecks(in repo.ScoringSettingsInput) []struct {
	value int
	limit int
} {
	return []struct {
		value int
		limit int
	}{
		{value: in.PriorityNoneWeight, limit: 10000},
		{value: in.PriorityLowWeight, limit: 10000},
		{value: in.PriorityMediumWeight, limit: 10000},
		{value: in.PriorityHighWeight, limit: 10000},
		{value: in.PriorityUrgentWeight, limit: 10000},
		{value: in.FeedbackWeight, limit: 1000},
		{value: in.FeedbackCap, limit: 10000},
		{value: in.CustomerWeight, limit: 1000},
		{value: in.CustomerCap, limit: 10000},
		{value: in.AccountWeight, limit: 1000},
		{value: in.AccountCap, limit: 10000},
		{value: in.VoteWeight, limit: 1000},
		{value: in.VoteCap, limit: 10000},
		{value: in.RevenueCap, limit: 10000},
	}
}

func patchInt(current int, next *int) int {
	if next == nil {
		return current
	}
	return ptrext.Indirect(next)
}

func patchInt64(current int64, next *int64) int64 {
	if next == nil {
		return current
	}
	return ptrext.Indirect(next)
}

func validWeight(value, limit int) bool {
	return value >= 0 && value <= limit
}

func validateCreateFields(tenantID, title, description string, status repo.Status, priority repo.Priority, key string) error {
	if tenantID == "" || utf8.RuneCountInString(title) == 0 || utf8.RuneCountInString(title) > 200 {
		return ErrValidation
	}
	if utf8.RuneCountInString(description) > 10000 {
		return ErrValidation
	}
	if !validStatus(status) || !validPriority(priority) {
		return ErrValidation
	}
	if !idempotencyKeyPattern.MatchString(key) {
		return ErrValidation
	}
	return nil
}

func normalizeUpdate(in UpdateInput) (UpdateInput, error) {
	if in.TenantID == "" || in.ID == uuid.Nil {
		return UpdateInput{}, ErrValidation
	}
	if in.Title != nil {
		value := strings.TrimSpace(ptrext.Indirect(in.Title))
		if utf8.RuneCountInString(value) == 0 || utf8.RuneCountInString(value) > 200 {
			return UpdateInput{}, ErrValidation
		}
		in.Title = ptrext.Of(value)
	}
	if in.Description != nil {
		value := strings.TrimSpace(ptrext.Indirect(in.Description))
		if utf8.RuneCountInString(value) > 10000 {
			return UpdateInput{}, ErrValidation
		}
		in.Description = ptrext.Of(value)
	}
	if in.Status != nil && !validStatus(ptrext.Indirect(in.Status)) {
		return UpdateInput{}, ErrValidation
	}
	if in.Priority != nil && !validPriority(ptrext.Indirect(in.Priority)) {
		return UpdateInput{}, ErrValidation
	}
	return in, nil
}

func normalizeLinkFeedback(in LinkFeedbackInput) (LinkFeedbackInput, error) {
	if in.TenantID == "" || in.RequestID == uuid.Nil || in.FeedbackID <= 0 {
		return LinkFeedbackInput{}, ErrValidation
	}
	if in.Importance == "" {
		in.Importance = repo.ImportanceNormal
	}
	in.Note = strings.TrimSpace(in.Note)
	if !validImportance(in.Importance) || utf8.RuneCountInString(strings.TrimSpace(in.Note)) > 5000 {
		return LinkFeedbackInput{}, ErrValidation
	}
	return in, nil
}

func normalizeCustomerLink(in LinkCustomerInput) (LinkCustomerInput, error) {
	in.SubjectKey = strings.TrimSpace(in.SubjectKey)
	in.SubjectHash = strings.TrimSpace(in.SubjectHash)
	in.SubjectDisplay = strings.TrimSpace(in.SubjectDisplay)
	in.AccountKey = strings.TrimSpace(in.AccountKey)
	in.AccountDisplay = strings.TrimSpace(in.AccountDisplay)
	in.Note = strings.TrimSpace(in.Note)
	if in.TenantID == "" || in.RequestID == uuid.Nil {
		return LinkCustomerInput{}, ErrValidation
	}
	if in.SubjectKey == "" && in.SubjectHash == "" && in.AccountKey == "" {
		return LinkCustomerInput{}, ErrValidation
	}
	if !validSupporterFields(in.SubjectKey, in.SubjectHash, in.SubjectDisplay, in.AccountKey, in.AccountDisplay, in.Note) {
		return LinkCustomerInput{}, ErrValidation
	}
	profile, err := normalizeAccountProfile(in.AccountKey, in.AccountProfile)
	if err != nil {
		return LinkCustomerInput{}, err
	}
	in.AccountProfile = profile
	return in, nil
}

func normalizeVote(in VoteInput) (VoteInput, error) {
	in.SubjectKey = strings.TrimSpace(in.SubjectKey)
	in.SubjectHash = strings.TrimSpace(in.SubjectHash)
	in.SubjectDisplay = strings.TrimSpace(in.SubjectDisplay)
	in.AccountKey = strings.TrimSpace(in.AccountKey)
	in.AccountDisplay = strings.TrimSpace(in.AccountDisplay)
	in.Note = strings.TrimSpace(in.Note)
	if in.TenantID == "" || in.RequestID == uuid.Nil {
		return VoteInput{}, ErrValidation
	}
	if in.SubjectKey == "" && in.SubjectHash == "" && in.AccountKey == "" {
		return VoteInput{}, ErrValidation
	}
	if in.Weight == 0 {
		in.Weight = 1
	}
	if in.Weight < 1 || in.Weight > 100 {
		return VoteInput{}, ErrValidation
	}
	if !validSupporterFields(in.SubjectKey, in.SubjectHash, in.SubjectDisplay, in.AccountKey, in.AccountDisplay, in.Note) {
		return VoteInput{}, ErrValidation
	}
	profile, err := normalizeAccountProfile(in.AccountKey, in.AccountProfile)
	if err != nil {
		return VoteInput{}, err
	}
	in.AccountProfile = profile
	return in, nil
}

func normalizeNote(in NoteInput) (NoteInput, error) {
	in.Body = strings.TrimSpace(in.Body)
	if in.TenantID == "" || in.RequestID == uuid.Nil {
		return NoteInput{}, ErrValidation
	}
	if utf8.RuneCountInString(in.Body) == 0 || utf8.RuneCountInString(in.Body) > 5000 {
		return NoteInput{}, ErrValidation
	}
	return in, nil
}

func normalizeAccountProfile(accountKey string, in AccountProfileInput) (AccountProfileInput, error) {
	in.RevenueCurrency = strings.ToUpper(strings.TrimSpace(in.RevenueCurrency))
	in.Tier = strings.TrimSpace(in.Tier)
	in.SizeSegment = strings.TrimSpace(in.SizeSegment)
	in.LifecycleStatus = strings.TrimSpace(in.LifecycleStatus)
	in.CRMProvider = strings.TrimSpace(in.CRMProvider)
	in.CRMExternalID = strings.TrimSpace(in.CRMExternalID)
	if strings.TrimSpace(accountKey) == "" {
		if hasAccountProfileInput(in) {
			return AccountProfileInput{}, ErrValidation
		}
		return AccountProfileInput{}, nil
	}
	if in.RevenueCurrency == "" {
		in.RevenueCurrency = "USD"
	}
	if in.RevenueCents != nil && ptrext.Indirect(in.RevenueCents) < 0 {
		return AccountProfileInput{}, ErrValidation
	}
	if !currencyPattern.MatchString(in.RevenueCurrency) {
		return AccountProfileInput{}, ErrValidation
	}
	if utf8.RuneCountInString(in.Tier) > 120 ||
		utf8.RuneCountInString(in.SizeSegment) > 120 ||
		utf8.RuneCountInString(in.LifecycleStatus) > 120 ||
		utf8.RuneCountInString(in.CRMProvider) > 120 ||
		utf8.RuneCountInString(in.CRMExternalID) > 512 {
		return AccountProfileInput{}, ErrValidation
	}
	return in, nil
}

func hasAccountProfileInput(in AccountProfileInput) bool {
	return in.RevenueCents != nil ||
		strings.TrimSpace(in.RevenueCurrency) != "" ||
		strings.TrimSpace(in.Tier) != "" ||
		strings.TrimSpace(in.SizeSegment) != "" ||
		strings.TrimSpace(in.LifecycleStatus) != "" ||
		strings.TrimSpace(in.CRMProvider) != "" ||
		strings.TrimSpace(in.CRMExternalID) != ""
}

func accountRevenueCents(in AccountProfileInput) int64 {
	if in.RevenueCents == nil {
		return 0
	}
	return ptrext.Indirect(in.RevenueCents)
}

func validSupporterFields(subjectKey, subjectHash, subjectDisplay, accountKey, accountDisplay, note string) bool {
	return utf8.RuneCountInString(subjectKey) <= 512 &&
		utf8.RuneCountInString(subjectHash) <= 128 &&
		utf8.RuneCountInString(subjectDisplay) <= 500 &&
		utf8.RuneCountInString(accountKey) <= 512 &&
		utf8.RuneCountInString(accountDisplay) <= 500 &&
		utf8.RuneCountInString(note) <= 5000
}

func normalizeIssueInput(in LinkIssueInput) (LinkIssueInput, error) {
	in.Provider = strings.ToLower(strings.TrimSpace(in.Provider))
	in.ExternalURL = strings.TrimSpace(in.ExternalURL)
	in.ExternalKey = strings.TrimSpace(in.ExternalKey)
	in.Title = strings.TrimSpace(in.Title)
	in.Status = strings.TrimSpace(in.Status)
	if in.TenantID == "" || in.RequestID == uuid.Nil {
		return LinkIssueInput{}, ErrValidation
	}
	if !validProvider(in.Provider) {
		return LinkIssueInput{}, ErrUnsupportedProvider
	}
	parsed, err := url.Parse(in.ExternalURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return LinkIssueInput{}, ErrInvalidIssueURL
	}
	if in.ExternalKey == "" {
		in.ExternalKey = deriveExternalKey(in.Provider, parsed)
	}
	if utf8.RuneCountInString(in.ExternalKey) == 0 || utf8.RuneCountInString(in.ExternalKey) > 512 {
		return LinkIssueInput{}, ErrValidation
	}
	if utf8.RuneCountInString(in.ExternalURL) > 2048 || utf8.RuneCountInString(in.Title) > 500 || utf8.RuneCountInString(in.Status) > 120 {
		return LinkIssueInput{}, ErrValidation
	}
	return in, nil
}

func normalizeIssueSync(in IssueSyncInput) (IssueSyncInput, error) {
	in.Status = strings.TrimSpace(in.Status)
	in.ExternalStatusCategory = strings.TrimSpace(in.ExternalStatusCategory)
	in.ExternalAssignee = strings.TrimSpace(in.ExternalAssignee)
	in.ExternalUpdatedAt = strings.TrimSpace(in.ExternalUpdatedAt)
	in.SyncError = strings.TrimSpace(in.SyncError)
	if in.TenantID == "" || in.RequestID == uuid.Nil || in.IssueLinkID == uuid.Nil {
		return IssueSyncInput{}, ErrValidation
	}
	if in.SyncState == "" {
		in.SyncState = repo.IssueSyncStateSynced
	}
	if !validIssueSyncState(in.SyncState) {
		return IssueSyncInput{}, ErrValidation
	}
	if in.ExternalUpdatedAt != "" {
		if _, err := time.Parse(time.RFC3339, in.ExternalUpdatedAt); err != nil {
			return IssueSyncInput{}, ErrValidation
		}
	}
	if utf8.RuneCountInString(in.Status) > 120 ||
		utf8.RuneCountInString(in.ExternalStatusCategory) > 120 ||
		utf8.RuneCountInString(in.ExternalAssignee) > 500 ||
		utf8.RuneCountInString(in.SyncError) > 2000 {
		return IssueSyncInput{}, ErrValidation
	}
	return in, nil
}

func hasGitHubIssueLink(detail *Detail) bool {
	if detail == nil {
		return false
	}
	for _, link := range detail.Request.IssueLinks {
		if strings.EqualFold(link.Provider, "github") {
			return true
		}
	}
	return false
}

func mapIssueCreateRunError(err error) error {
	switch {
	case errors.Is(err, externalsyncrepo.ErrMappingNotFound), errors.Is(err, externalsyncrepo.ErrConflict):
		return repo.ErrConflict
	case errors.Is(err, externalsyncrepo.ErrLocalObjectNotFound):
		return repo.ErrNotFound
	default:
		return err
	}
}

func mapManagedIssueLinkError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, externalsyncrepo.ErrInvalidInput):
		return repo.ErrInvalidInput
	case errors.Is(err, externalsyncrepo.ErrMappingNotFound), errors.Is(err, externalsyncrepo.ErrConflict):
		return repo.ErrConflict
	case errors.Is(err, externalsyncrepo.ErrLocalObjectNotFound):
		return repo.ErrNotFound
	default:
		return err
	}
}

func dedupeFeedbackIDs(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func defaultStatus(status repo.Status) repo.Status {
	if status == "" {
		return repo.StatusOpen
	}
	return status
}

func defaultPriority(priority repo.Priority) repo.Priority {
	if priority == "" {
		return repo.PriorityNone
	}
	return priority
}

func defaultVisibility(visibility repo.Visibility) repo.Visibility {
	if visibility == "" {
		return repo.VisibilityActive
	}
	return visibility
}

func defaultSort(sort repo.Sort) repo.Sort {
	if sort == "" {
		return repo.SortUpdatedAt
	}
	return sort
}

func defaultDirection(direction repo.Direction) repo.Direction {
	if direction == "" {
		return repo.DirectionDesc
	}
	return direction
}

func validStatus(status repo.Status) bool {
	switch status {
	case repo.StatusOpen, repo.StatusPlanned, repo.StatusInProgress, repo.StatusShipped, repo.StatusCancelled:
		return true
	default:
		return false
	}
}

func validPriority(priority repo.Priority) bool {
	switch priority {
	case repo.PriorityNone, repo.PriorityLow, repo.PriorityMedium, repo.PriorityHigh, repo.PriorityUrgent:
		return true
	default:
		return false
	}
}

func validImportance(importance repo.Importance) bool {
	switch importance {
	case repo.ImportanceNormal, repo.ImportanceImportant, repo.ImportanceCritical:
		return true
	default:
		return false
	}
}

func validProvider(provider string) bool {
	switch provider {
	case "github", "jira", "linear", "other":
		return true
	default:
		return false
	}
}

func validIssueSyncState(state repo.IssueSyncState) bool {
	switch state {
	case repo.IssueSyncStateManual, repo.IssueSyncStatePending, repo.IssueSyncStateSynced, repo.IssueSyncStateStale, repo.IssueSyncStateFailed:
		return true
	default:
		return false
	}
}

func parseOptionalTime(raw string) *time.Time {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil
	}
	return ptrext.Of(parsed)
}

func deriveExternalKey(provider string, parsed *url.URL) string {
	path := strings.Trim(parsed.Path, "/")
	switch provider {
	case "github":
		parts := strings.Split(path, "/")
		if len(parts) >= 4 && parts[2] == "issues" {
			return parts[0] + "/" + parts[1] + "#" + parts[3]
		}
	case "jira", "linear":
		if path != "" {
			parts := strings.Split(path, "/")
			return parts[len(parts)-1]
		}
	}
	return parsed.String()
}

func (s *Service) acquireIdempotency(ctx context.Context, tenantID, key, operation string, payload any) (*Detail, bool, error) {
	if s.idempotency == nil {
		return nil, false, nil
	}
	hash, err := hashPayload(operation, payload)
	if err != nil {
		return nil, false, err
	}
	record, acquired, err := s.idempotency.Acquire(ctx, tenantID, key, hash, 0)
	if errors.Is(err, idempotency.ErrHashMismatch) {
		return nil, false, ErrIdempotencyConflict
	}
	if errors.Is(err, idempotency.ErrExpired) {
		if _, deleteErr := s.idempotency.DeleteExpired(ctx, tenantID, key); deleteErr != nil {
			return nil, false, deleteErr
		}
		record, acquired, err = s.idempotency.Acquire(ctx, tenantID, key, hash, 0)
		if errors.Is(err, idempotency.ErrHashMismatch) {
			return nil, false, ErrIdempotencyConflict
		}
		if err != nil {
			return nil, false, err
		}
		if acquired {
			return nil, true, nil
		}
	}
	if err != nil {
		return nil, false, err
	}
	if acquired {
		return nil, true, nil
	}
	if record.Status == idempotency.StatusPending {
		return nil, false, ErrRequestInProgress
	}
	if record.Status == idempotency.StatusCompleted && len(record.ResponseBody) > 0 {
		var cached Detail
		if err := json.Unmarshal(record.ResponseBody, &cached); err != nil {
			return nil, false, err
		}
		return ptrext.Of(cached), false, nil
	}
	return nil, true, nil
}

func hashPayload(operation string, payload any) ([]byte, error) {
	data, err := json.Marshal(struct {
		Operation string
		Payload   any
	}{Operation: operation, Payload: payload})
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(data)
	return sum[:], nil
}

func createIdempotencyPayload(in CreateInput) any {
	return struct {
		TenantID      string
		Title         string
		Description   string
		Status        repo.Status
		Priority      repo.Priority
		OwnerMemberID string
	}{
		TenantID:      in.TenantID,
		Title:         in.Title,
		Description:   in.Description,
		Status:        in.Status,
		Priority:      in.Priority,
		OwnerMemberID: uuidPtrString(in.OwnerMemberID),
	}
}

func promoteIdempotencyPayload(in PromoteInput) any {
	return struct {
		TenantID      string
		FeedbackIDs   []int64
		Title         string
		Description   string
		Status        repo.Status
		Priority      repo.Priority
		OwnerMemberID string
	}{
		TenantID:      in.TenantID,
		FeedbackIDs:   in.FeedbackIDs,
		Title:         in.Title,
		Description:   in.Description,
		Status:        in.Status,
		Priority:      in.Priority,
		OwnerMemberID: uuidPtrString(in.OwnerMemberID),
	}
}

func mergeIdempotencyPayload(in MergeInput) any {
	return struct {
		TenantID string
		SourceID string
		TargetID string
	}{
		TenantID: in.TenantID,
		SourceID: in.SourceID.String(),
		TargetID: in.TargetID.String(),
	}
}

func (s *Service) completeIdempotency(ctx context.Context, tenantID, key string, acquired bool, detail *Detail, opErr error) (*Detail, error) {
	if s.idempotency == nil || !acquired {
		return detail, opErr
	}
	if opErr != nil {
		_ = s.idempotency.Fail(ctx, tenantID, key)
		return nil, opErr
	}
	body, err := json.Marshal(detail)
	if err != nil {
		_ = s.idempotency.Fail(ctx, tenantID, key)
		return nil, err
	}
	if err := s.idempotency.Complete(ctx, tenantID, key, 200, body); err != nil {
		return nil, err
	}
	return detail, nil
}

func (s *Service) detail(ctx context.Context, tenantID string, id uuid.UUID, evidenceLimit int) (*Detail, error) {
	detail, err := s.repo.GetDetail(ctx, tenantID, id, evidenceLimit)
	if err != nil {
		return nil, err
	}
	out := ptrext.Of(Detail{Request: ptrext.Indirect(detail)})
	if s.audit == nil {
		return out, nil
	}
	result, err := s.audit.List(ctx, auditlogsvc.ListFilter{
		TenantID:   tenantID,
		TargetType: "customer_request",
		TargetID:   id.String(),
		Limit:      20,
	})
	if err != nil {
		return nil, err
	}
	out.AuditEntries = make([]AuditEntry, 0, len(result.Items))
	out.DecisionRecords = make([]DecisionRecord, 0, len(result.Items))
	for _, item := range result.Items {
		out.AuditEntries = append(out.AuditEntries, AuditEntry{
			ID:        item.ID,
			Action:    item.Action,
			ActorType: item.ActorType,
			ActorID:   item.ActorID,
			Summary:   item.Summary,
			CreatedAt: item.CreatedAt,
		})
		if record, ok := decisionRecordFromAudit(item); ok {
			out.DecisionRecords = append(out.DecisionRecords, record)
		}
	}
	return out, nil
}

func (s *Service) recordAuditTx(
	ctx context.Context,
	tx pgx.Tx,
	actor auditlogsvc.Actor,
	action string,
	target repo.Summary,
	summary string,
	after any,
) error {
	if s.audit == nil {
		return nil
	}
	if actor.Type == "" {
		actor.Type = "admin"
	}
	if actor.ID == "" {
		actor.ID = "system"
	}
	return s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   target.TenantID,
		Actor:      actor,
		Action:     action,
		TargetType: "customer_request",
		TargetID:   target.ID.String(),
		Summary:    summary,
		After:      withDecisionSnapshot(after, target),
	})
}

func (s *Service) recordAudit(
	ctx context.Context,
	actor auditlogsvc.Actor,
	action string,
	target repo.Summary,
	summary string,
	after any,
) error {
	if s.audit == nil {
		return nil
	}
	return s.audit.Record(ctx, auditlogsvc.Event{
		TenantID:   target.TenantID,
		Actor:      actor,
		Action:     action,
		TargetType: "customer_request",
		TargetID:   target.ID.String(),
		Summary:    summary,
		After:      after,
	})
}

func (s *Service) recordMergeAuditTx(ctx context.Context, tx pgx.Tx, tenantID string, actor auditlogsvc.Actor, result repo.MergeResult) error {
	if s.audit == nil {
		return nil
	}
	if actor.Type == "" {
		actor.Type = "admin"
	}
	if actor.ID == "" {
		actor.ID = "system"
	}
	return s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   tenantID,
		Actor:      actor,
		Action:     "customer_request.merge",
		TargetType: "customer_request",
		TargetID:   result.TargetID.String(),
		Summary:    fmt.Sprintf("Merged %s into %s", result.SourceDisplayID, result.TargetDisplayID),
		After: map[string]any{
			"source_request_id":                result.SourceID.String(),
			"target_request_id":                result.TargetID.String(),
			"moved_feedback_count":             result.MovedFeedbackCount,
			"moved_customer_count":             result.MovedCustomerCount,
			"moved_vote_count":                 result.MovedVoteCount,
			"moved_note_count":                 result.MovedNoteCount,
			"moved_issue_count":                result.MovedIssueCount,
			"skipped_duplicate_feedback_count": result.SkippedDuplicateFeedbackCount,
			"skipped_duplicate_customer_count": result.SkippedDuplicateCustomerCount,
			"skipped_duplicate_vote_count":     result.SkippedDuplicateVoteCount,
			"skipped_duplicate_issue_count":    result.SkippedDuplicateIssueCount,
		},
	})
}

func (s *Service) recordScoringSettingsAuditTx(
	ctx context.Context,
	tx pgx.Tx,
	tenantID string,
	actor auditlogsvc.Actor,
	before repo.ScoringSettings,
	after repo.ScoringSettings,
) error {
	if s.audit == nil {
		return nil
	}
	if actor.Type == "" {
		actor.Type = "admin"
	}
	return s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   tenantID,
		Actor:      actor,
		Action:     "customer_request.update_scoring_settings",
		TargetType: "customer_request_scoring_settings",
		TargetID:   tenantID,
		Summary:    "Updated customer request scoring settings",
		Before:     scoringSettingsAuditFields(before),
		After:      scoringSettingsAuditFields(after),
	})
}

func scoringSettingsAuditFields(settings repo.ScoringSettings) map[string]any {
	return map[string]any{
		"priority_none_weight":    settings.PriorityNoneWeight,
		"priority_low_weight":     settings.PriorityLowWeight,
		"priority_medium_weight":  settings.PriorityMediumWeight,
		"priority_high_weight":    settings.PriorityHighWeight,
		"priority_urgent_weight":  settings.PriorityUrgentWeight,
		"feedback_weight":         settings.FeedbackWeight,
		"feedback_cap":            settings.FeedbackCap,
		"customer_weight":         settings.CustomerWeight,
		"customer_cap":            settings.CustomerCap,
		"account_weight":          settings.AccountWeight,
		"account_cap":             settings.AccountCap,
		"vote_weight":             settings.VoteWeight,
		"vote_cap":                settings.VoteCap,
		"revenue_cents_per_point": settings.RevenueCentsPerPoint,
		"revenue_cap":             settings.RevenueCap,
	}
}

func withDecisionSnapshot(after any, summary repo.Summary) map[string]any {
	payload := auditPayloadFromAny(after)
	for key, value := range decisionSnapshotAuditFields(summary) {
		payload[key] = value
	}
	return payload
}

func auditPayloadFromAny(value any) map[string]any {
	out := make(map[string]any)
	if value == nil {
		return out
	}
	typed, ok := value.(map[string]any)
	if !ok {
		out["metadata"] = value
		return out
	}
	for key, item := range typed {
		out[key] = item
	}
	return out
}

func decisionSnapshotAuditFields(summary repo.Summary) map[string]any {
	return map[string]any{
		"decision_score":            summary.DecisionScore,
		"decision_score_factors":    decisionScoreFactorAuditFields(summary.DecisionScoreFactors),
		"delivery_health":           summary.DeliveryHealth,
		"supporting_feedback_count": summary.SupportingFeedbackCount,
		"customer_count":            summary.CustomerCount,
		"account_count":             summary.AccountCount,
		"vote_count":                summary.VoteCount,
		"revenue_impact_cents":      summary.RevenueImpactCents,
		"revenue_currency":          summary.RevenueCurrency,
		"decision_rationale":        decisionRationale(summary),
		"owner_member_id":           uuidPtrString(summary.OwnerMemberID),
		"owner_display":             decisionOwnerDisplay(summary),
		"evidence_bundle_ref":       decisionEvidenceBundleRef(summary),
		"public_safe_state":         decisionPublicSafeState(summary),
		"public_safe_reasons":       decisionPublicSafeReasons(summary),
	}
}

func decisionRationale(summary repo.Summary) string {
	if summary.DecisionScoreExplanation != "" {
		return summary.DecisionScoreExplanation
	}
	return fmt.Sprintf(
		"score=%d feedback=%d customers=%d accounts=%d delivery_health=%s",
		summary.DecisionScore,
		summary.SupportingFeedbackCount,
		summary.CustomerCount,
		summary.AccountCount,
		summary.DeliveryHealth,
	)
}

func decisionOwnerDisplay(summary repo.Summary) string {
	if summary.Owner == nil {
		return ""
	}
	owner := ptrext.Indirect(summary.Owner)
	if strings.TrimSpace(owner.Email) != "" {
		return strings.TrimSpace(owner.Email)
	}
	if strings.TrimSpace(owner.UserID) != "" {
		return strings.TrimSpace(owner.UserID)
	}
	if strings.TrimSpace(owner.Role) != "" {
		return strings.TrimSpace(owner.Role)
	}
	return owner.ID.String()
}

func decisionEvidenceBundleRef(summary repo.Summary) string {
	if summary.ID == uuid.Nil {
		return ""
	}
	displayID := strings.TrimSpace(summary.DisplayID)
	if displayID == "" {
		displayID = summary.ID.String()
	}
	return fmt.Sprintf("customer-request/%s/evidence/%s", summary.ID.String(), displayID)
}

func decisionPublicSafeState(summary repo.Summary) string {
	if summary.ArchivedAt != nil || summary.MergedIntoRequestID != nil || summary.Status == repo.StatusCancelled {
		return "internal_only"
	}
	if len(decisionPublicSafeReasons(summary)) > 0 {
		return "needs_review"
	}
	return "public_safe"
}

func decisionPublicSafeReasons(summary repo.Summary) []string {
	reasons := make([]string, 0, 4)
	if summary.HiddenFeedbackCount > 0 {
		reasons = append(reasons, "hidden_feedback")
	}
	if summary.RevenueImpactCents > 0 {
		reasons = append(reasons, "revenue_context")
	}
	if summary.SupportingFeedbackCount == 0 {
		reasons = append(reasons, "missing_evidence")
	}
	if summary.DeliveryHealth == repo.DeliveryHealthFailed {
		reasons = append(reasons, "failed_delivery_link")
	}
	return reasons
}

func decisionScoreFactorAuditFields(factors []repo.DecisionScoreFactor) []map[string]any {
	out := make([]map[string]any, 0, len(factors))
	for _, factor := range factors {
		out = append(out, map[string]any{
			"kind":                 factor.Kind,
			"raw_count":            factor.RawCount,
			"raw_value_cents":      factor.RawValueCents,
			"weight":               factor.Weight,
			"cap":                  factor.Cap,
			"unit_cents":           factor.UnitCents,
			"contribution":         factor.Contribution,
			"capped":               factor.Capped,
			"contributes_to_score": factor.ContributesToScore,
		})
	}
	return out
}

func decisionRecordFromAudit(entry auditlogrepo.Entry) (DecisionRecord, bool) {
	if entry.TargetType != "customer_request" || !strings.HasPrefix(entry.Action, "customer_request.") {
		return DecisionRecord{}, false
	}
	record := DecisionRecord{
		AuditID:   entry.ID,
		Action:    entry.Action,
		ActorType: entry.ActorType,
		ActorID:   entry.ActorID,
		Summary:   entry.Summary,
		CreatedAt: entry.CreatedAt,
	}
	payload := auditPayloadFromJSON(entry.AfterJSON)
	record = applyDecisionRecordChanges(payload, record)
	record = applyDecisionRecordSnapshot(payload, record)
	return record, true
}

func auditPayloadFromJSON(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func applyDecisionRecordChanges(payload map[string]any, record DecisionRecord) DecisionRecord {
	oldStatus, hasOldStatus := auditString(payload, "old_status")
	newStatus, hasNewStatus := auditString(payload, "new_status")
	record.StatusChanged = (hasOldStatus || hasNewStatus) && oldStatus != newStatus
	record.OldStatus = repo.Status(oldStatus)
	record.NewStatus = repo.Status(newStatus)
	oldPriority, hasOldPriority := auditString(payload, "old_priority")
	newPriority, hasNewPriority := auditString(payload, "new_priority")
	record.PriorityChanged = (hasOldPriority || hasNewPriority) && oldPriority != newPriority
	record.OldPriority = repo.Priority(oldPriority)
	record.NewPriority = repo.Priority(newPriority)
	oldOwner, hasOldOwner := auditString(payload, "old_owner_member_id")
	newOwner, hasNewOwner := auditString(payload, "new_owner_member_id")
	record.OldOwnerMemberID = oldOwner
	record.NewOwnerMemberID = newOwner
	record.OwnerChanged = (hasOldOwner || hasNewOwner) && oldOwner != newOwner
	record.TitleChanged, _ = auditBool(payload, "title_changed")
	record.DescriptionChanged, _ = auditBool(payload, "description_changed")
	return record
}

func applyDecisionRecordSnapshot(payload map[string]any, record DecisionRecord) DecisionRecord {
	score, ok := auditInt(payload, "decision_score")
	record.HasDecisionSnapshot = ok
	record.DecisionScore = score
	record.DecisionScoreFactors = auditDecisionScoreFactors(payload["decision_score_factors"])
	deliveryHealth, _ := auditString(payload, "delivery_health")
	record.DeliveryHealth = repo.DeliveryHealth(deliveryHealth)
	record.SupportingFeedbackCount, _ = auditInt(payload, "supporting_feedback_count")
	record.CustomerCount, _ = auditInt(payload, "customer_count")
	record.AccountCount, _ = auditInt(payload, "account_count")
	record.VoteCount, _ = auditInt(payload, "vote_count")
	record.RevenueImpactCents, _ = auditInt64(payload, "revenue_impact_cents")
	record.RevenueCurrency, _ = auditString(payload, "revenue_currency")
	record.DecisionRationale, _ = auditString(payload, "decision_rationale")
	record.OwnerMemberID, _ = auditString(payload, "owner_member_id")
	record.OwnerDisplay, _ = auditString(payload, "owner_display")
	record.EvidenceBundleRef, _ = auditString(payload, "evidence_bundle_ref")
	record.PublicSafeState, _ = auditString(payload, "public_safe_state")
	record.PublicSafeReasons = auditStringList(payload["public_safe_reasons"])
	return record
}

func auditStringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func auditDecisionScoreFactors(value any) []repo.DecisionScoreFactor {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]repo.DecisionScoreFactor, 0, len(items))
	for _, item := range items {
		if factor, ok := auditDecisionScoreFactor(item); ok {
			out = append(out, factor)
		}
	}
	return out
}

func auditDecisionScoreFactor(value any) (repo.DecisionScoreFactor, bool) {
	payload, ok := value.(map[string]any)
	if !ok {
		return repo.DecisionScoreFactor{}, false
	}
	kind, _ := auditString(payload, "kind")
	factor := repo.DecisionScoreFactor{Kind: repo.DecisionScoreFactorKind(kind)}
	factor.RawCount, _ = auditInt(payload, "raw_count")
	factor.RawValueCents, _ = auditInt64(payload, "raw_value_cents")
	factor.Weight, _ = auditInt(payload, "weight")
	factor.Cap, _ = auditInt(payload, "cap")
	factor.UnitCents, _ = auditInt64(payload, "unit_cents")
	factor.Contribution, _ = auditInt(payload, "contribution")
	factor.Capped, _ = auditBool(payload, "capped")
	factor.ContributesToScore, _ = auditBool(payload, "contributes_to_score")
	return factor, factor.Kind != ""
}

func auditString(payload map[string]any, key string) (string, bool) {
	value, ok := payload[key]
	if !ok {
		return "", false
	}
	if value == nil {
		return "", true
	}
	typed, ok := value.(string)
	if ok {
		return typed, true
	}
	return fmt.Sprint(value), true
}

func auditBool(payload map[string]any, key string) (bool, bool) {
	value, ok := payload[key]
	if !ok {
		return false, false
	}
	typed, ok := value.(bool)
	return typed, ok
}

func auditInt(payload map[string]any, key string) (int, bool) {
	value, ok := payload[key]
	if !ok {
		return 0, false
	}
	asInt, ok := auditInt64Value(value)
	return int(asInt), ok
}

func auditInt64(payload map[string]any, key string) (int64, bool) {
	value, ok := payload[key]
	if !ok {
		return 0, false
	}
	return auditInt64Value(value)
}

func auditInt64Value(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		return int64(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func createAuditMetadata(summary repo.Summary, idempotencyKey string) map[string]any {
	hash := sha256.Sum256([]byte(idempotencyKey))
	return map[string]any{
		"request_id":           summary.ID.String(),
		"display_id":           summary.DisplayID,
		"title_length":         utf8.RuneCountInString(summary.Title),
		"status":               summary.Status,
		"priority":             summary.Priority,
		"owner_member_id":      uuidPtrString(summary.OwnerMemberID),
		"idempotency_key_hash": fmt.Sprintf("%x", hash[:8]),
	}
}

func createAuditSummary(action string, summary repo.Summary) string {
	if action == "customer_request.promote_feedback" {
		return "Promoted feedback to customer request"
	}
	if summary.DisplayID == "" {
		return "Created customer request"
	}
	return fmt.Sprintf("Created customer request %s", summary.DisplayID)
}

func updateAuditBeforeAfter(before, after repo.Summary) map[string]any {
	return map[string]any{
		"request_id":          after.ID.String(),
		"old_status":          before.Status,
		"new_status":          after.Status,
		"old_priority":        before.Priority,
		"new_priority":        after.Priority,
		"old_owner_member_id": uuidPtrString(before.OwnerMemberID),
		"new_owner_member_id": uuidPtrString(after.OwnerMemberID),
		"title_changed":       before.Title != after.Title,
		"description_changed": before.Description != after.Description,
	}
}

func issueAuditMetadata(requestID uuid.UUID, link repo.IssueLink) map[string]any {
	host := ""
	if parsed, err := url.Parse(link.ExternalURL); err == nil {
		host = parsed.Host
	}
	return map[string]any{
		"request_id":    requestID.String(),
		"issue_link_id": link.ID.String(),
		"provider":      link.Provider,
		"external_key":  link.ExternalKey,
		"url_host":      host,
	}
}

func customerAuditMetadata(requestID uuid.UUID, link repo.CustomerLink) map[string]any {
	return map[string]any{
		"request_id":       requestID.String(),
		"customer_link_id": link.ID.String(),
		"subject_key_set":  link.SubjectKey != "",
		"subject_hash_set": link.SubjectHash != "",
		"account_key_set":  link.AccountKey != "",
		"note_length":      utf8.RuneCountInString(link.Note),
	}
}

func voteAuditMetadata(requestID uuid.UUID, vote repo.Vote) map[string]any {
	return map[string]any{
		"request_id":       requestID.String(),
		"vote_id":          vote.ID.String(),
		"subject_key_set":  vote.SubjectKey != "",
		"subject_hash_set": vote.SubjectHash != "",
		"account_key_set":  vote.AccountKey != "",
		"weight":           vote.Weight,
		"note_length":      utf8.RuneCountInString(vote.Note),
	}
}

func noteAuditMetadata(requestID uuid.UUID, note repo.Note) map[string]any {
	return map[string]any{
		"request_id":  requestID.String(),
		"note_id":     note.ID.String(),
		"body_length": utf8.RuneCountInString(note.Body),
	}
}

func uuidPtrString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return ptrext.Indirect(id).String()
}
