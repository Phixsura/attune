package feedbackassignment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/feedbackaudit"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

var (
	ErrValidation             = errors.New("feedback assignment validation failed")
	ErrNotFound               = errors.New("feedback not found")
	ErrOwnerNotFound          = errors.New("feedback assignment owner not found")
	ErrPolicyRevisionNotFound = errors.New("feedback assignment policy revision not found")
)

const MaxBatchSize = 100

type Input struct {
	TenantID         string
	FeedbackID       int64
	OwnerMemberIDSet bool
	OwnerMemberID    *string
	SLADueAtSet      bool
	SLADueAt         *time.Time
	Note             string
	ActorID          string
}

type BatchInput struct {
	TenantID         string
	FeedbackIDs      []int64
	OwnerMemberIDSet bool
	OwnerMemberID    *string
	SLADueAtSet      bool
	SLADueAt         *time.Time
	Note             string
	ActorID          string
}

type BatchResult struct {
	TotalMatched int
	Succeeded    int
	Failed       []BatchFailure
}

type BatchFailure struct {
	FeedbackID int64
	Code       string
	Message    string
}

type Store interface {
	AssignmentForUpdate(ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64) (feedbackrepo.Assignment, error)
	AssignmentCandidates(ctx context.Context, tenantID string, feedbackIDs []int64) ([]feedbackrepo.AssignmentCandidate, error)
	AssignFeedbackTx(ctx context.Context, tx pgx.Tx, tenantID string, feedbackID int64, input feedbackrepo.AssignmentInput) (feedbackrepo.Assignment, error)
	ValidateAssignmentOwner(ctx context.Context, tenantID string, ownerMemberID string) error
}

type AuditWriter interface {
	WriteTx(ctx context.Context, tx pgx.Tx, e feedbackaudit.Entry) error
}

type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	store       Store
	audits      AuditWriter
	pool        TxBeginner
	policyStore PolicyStore
	auditLog    *auditlogsvc.Service
}

func New(store Store, audits AuditWriter, pool TxBeginner) *Service {
	return ptrext.Of(Service{store: store, audits: audits, pool: pool})
}

func (s *Service) Assign(ctx context.Context, input Input) (feedbackrepo.Assignment, error) {
	normalized, err := normalize(input)
	if err != nil {
		return feedbackrepo.Assignment{}, err
	}
	if normalized.OwnerMemberIDSet && normalized.OwnerMemberID != nil {
		if err := s.store.ValidateAssignmentOwner(ctx, normalized.TenantID, ptrext.Indirect(normalized.OwnerMemberID)); err != nil {
			if errors.Is(err, feedbackrepo.ErrAssignmentOwnerNotFound) {
				return feedbackrepo.Assignment{}, ErrOwnerNotFound
			}
			return feedbackrepo.Assignment{}, err
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return feedbackrepo.Assignment{}, fmt.Errorf("begin assignment tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	before, err := s.store.AssignmentForUpdate(ctx, tx, normalized.TenantID, normalized.FeedbackID)
	if err != nil {
		return feedbackrepo.Assignment{}, mapStoreError(err)
	}
	after, err := s.store.AssignFeedbackTx(ctx, tx, normalized.TenantID, normalized.FeedbackID, feedbackrepo.AssignmentInput{
		OwnerMemberIDSet: normalized.OwnerMemberIDSet,
		OwnerMemberID:    normalized.OwnerMemberID,
		SLADueAtSet:      normalized.SLADueAtSet,
		SLADueAt:         normalized.SLADueAt,
		Note:             normalized.Note,
		ActorID:          normalized.ActorID,
	})
	if err != nil {
		return feedbackrepo.Assignment{}, mapStoreError(err)
	}
	if err := s.writeAuditEntries(ctx, tx, normalized, before, after); err != nil {
		return feedbackrepo.Assignment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return feedbackrepo.Assignment{}, fmt.Errorf("commit assignment tx: %w", err)
	}
	return after, nil
}

func (s *Service) AssignBatch(ctx context.Context, input BatchInput) (BatchResult, error) {
	normalized, err := normalizeBatch(input)
	if err != nil {
		return BatchResult{}, err
	}
	if err := s.validateOwner(ctx, normalized.TenantID, normalized.OwnerMemberIDSet, normalized.OwnerMemberID); err != nil {
		return BatchResult{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return BatchResult{}, fmt.Errorf("begin batch assignment tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	result := BatchResult{TotalMatched: len(normalized.FeedbackIDs)}
	for _, feedbackID := range normalized.FeedbackIDs {
		applied, err := s.assignBatchItem(ctx, tx, normalized, feedbackID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				result.Failed = append(result.Failed, BatchFailure{
					FeedbackID: feedbackID,
					Code:       "NOT_FOUND",
					Message:    "feedback not found",
				})
				continue
			}
			return BatchResult{}, err
		}
		if applied {
			result.Succeeded++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchResult{}, fmt.Errorf("commit batch assignment tx: %w", err)
	}
	return result, nil
}

func normalize(input Input) (Input, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Note = strings.TrimSpace(input.Note)
	if input.TenantID == "" || input.FeedbackID <= 0 || input.ActorID == "" {
		return Input{}, ErrValidation
	}
	if len([]rune(input.Note)) > 1000 {
		return Input{}, fmt.Errorf("%w: note too long", ErrValidation)
	}
	if input.OwnerMemberIDSet {
		ownerID := strings.TrimSpace(ptrext.IndirectOr(input.OwnerMemberID, ""))
		if ownerID == "" {
			input.OwnerMemberID = nil
		} else {
			input.OwnerMemberID = ptrext.Of(ownerID)
		}
	}
	if input.SLADueAtSet && input.SLADueAt != nil {
		input.SLADueAt = ptrext.Of(input.SLADueAt.UTC())
	}
	return input, nil
}

func normalizeBatch(input BatchInput) (BatchInput, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Note = strings.TrimSpace(input.Note)
	input.FeedbackIDs = uniquePositiveFeedbackIDs(input.FeedbackIDs)
	if input.TenantID == "" || input.ActorID == "" || len(input.FeedbackIDs) == 0 {
		return BatchInput{}, ErrValidation
	}
	if len(input.FeedbackIDs) > MaxBatchSize {
		return BatchInput{}, fmt.Errorf("%w: too many feedback ids", ErrValidation)
	}
	if len([]rune(input.Note)) > 1000 {
		return BatchInput{}, fmt.Errorf("%w: note too long", ErrValidation)
	}
	if !input.OwnerMemberIDSet && !input.SLADueAtSet && input.Note == "" {
		return BatchInput{}, fmt.Errorf("%w: assignment change required", ErrValidation)
	}
	if input.OwnerMemberIDSet {
		ownerID := strings.TrimSpace(ptrext.IndirectOr(input.OwnerMemberID, ""))
		if ownerID == "" {
			input.OwnerMemberID = nil
		} else {
			input.OwnerMemberID = ptrext.Of(ownerID)
		}
	}
	if input.SLADueAtSet && input.SLADueAt != nil {
		input.SLADueAt = ptrext.Of(input.SLADueAt.UTC())
	}
	return input, nil
}

func uniquePositiveFeedbackIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
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

func (s *Service) validateOwner(
	ctx context.Context,
	tenantID string,
	ownerMemberIDSet bool,
	ownerMemberID *string,
) error {
	if !ownerMemberIDSet || ownerMemberID == nil {
		return nil
	}
	if err := s.store.ValidateAssignmentOwner(ctx, tenantID, ptrext.Indirect(ownerMemberID)); err != nil {
		if errors.Is(err, feedbackrepo.ErrAssignmentOwnerNotFound) {
			return ErrOwnerNotFound
		}
		return err
	}
	return nil
}

func (s *Service) assignBatchItem(
	ctx context.Context,
	tx pgx.Tx,
	input BatchInput,
	feedbackID int64,
) (bool, error) {
	before, err := s.store.AssignmentForUpdate(ctx, tx, input.TenantID, feedbackID)
	if err != nil {
		return false, mapStoreError(err)
	}
	after, err := s.store.AssignFeedbackTx(ctx, tx, input.TenantID, feedbackID, feedbackrepo.AssignmentInput{
		OwnerMemberIDSet: input.OwnerMemberIDSet,
		OwnerMemberID:    input.OwnerMemberID,
		SLADueAtSet:      input.SLADueAtSet,
		SLADueAt:         input.SLADueAt,
		Note:             input.Note,
		ActorID:          input.ActorID,
	})
	if err != nil {
		return false, mapStoreError(err)
	}
	auditInput := Input{
		TenantID:         input.TenantID,
		FeedbackID:       feedbackID,
		OwnerMemberIDSet: input.OwnerMemberIDSet,
		OwnerMemberID:    input.OwnerMemberID,
		SLADueAtSet:      input.SLADueAtSet,
		SLADueAt:         input.SLADueAt,
		Note:             input.Note,
		ActorID:          input.ActorID,
	}
	if err := s.writeAuditEntries(ctx, tx, auditInput, before, after); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) writeAuditEntries(
	ctx context.Context,
	tx pgx.Tx,
	input Input,
	before feedbackrepo.Assignment,
	after feedbackrepo.Assignment,
) error {
	if s.audits == nil {
		return nil
	}
	entries := assignmentAuditEntries(input, before, after)
	for _, entry := range entries {
		if err := s.audits.WriteTx(ctx, tx, entry); err != nil {
			return fmt.Errorf("write assignment audit: %w", err)
		}
	}
	return nil
}

func assignmentAuditEntries(
	input Input,
	before feedbackrepo.Assignment,
	after feedbackrepo.Assignment,
) []feedbackaudit.Entry {
	base := feedbackaudit.Entry{
		TenantID:   input.TenantID,
		FeedbackID: input.FeedbackID,
		EntityType: "feedback_assignment",
		Comment:    input.Note,
		ChangedBy:  input.ActorID,
	}
	var entries []feedbackaudit.Entry
	if ptrString(before.OwnerMemberID) != ptrString(after.OwnerMemberID) {
		item := base
		item.FieldName = "owner_member_id"
		item.OldValue = before.OwnerMemberID
		item.NewValue = after.OwnerMemberID
		entries = append(entries, item)
	}
	if timePtrString(before.SLADueAt) != timePtrString(after.SLADueAt) {
		item := base
		item.FieldName = "feedback_sla_due_at"
		item.OldValue = rfc3339Ptr(before.SLADueAt)
		item.NewValue = rfc3339Ptr(after.SLADueAt)
		entries = append(entries, item)
	}
	if before.Note != after.Note {
		item := base
		item.FieldName = "owner_assignment_note"
		item.OldValue = nullable(before.Note)
		item.NewValue = nullable(after.Note)
		entries = append(entries, item)
	}
	if len(entries) == 0 {
		item := base
		item.FieldName = "feedback_assignment"
		item.OldValue = nil
		item.NewValue = nullable("reviewed")
		entries = append(entries, item)
	}
	return entries
}

func mapStoreError(err error) error {
	if errors.Is(err, feedbackrepo.ErrFeedbackNotFound) {
		return ErrNotFound
	}
	if errors.Is(err, feedbackrepo.ErrAssignmentOwnerNotFound) {
		return ErrOwnerNotFound
	}
	return err
}

func ptrString(value *string) string {
	return strings.TrimSpace(ptrext.IndirectOr(value, ""))
}

func timePtrString(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func rfc3339Ptr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	return ptrext.Of(value.UTC().Format(time.RFC3339))
}

func nullable(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return ptrext.Of(value)
}
