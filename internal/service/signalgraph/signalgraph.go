// SPDX-License-Identifier: Apache-2.0

// Package signalgraph coordinates durable identity graph mutations.
package signalgraph

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/signalgraph"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

var (
	ErrValidation       = errors.New("signal graph validation failed")
	ErrFeedbackNotFound = repo.ErrFeedbackNotFound
	ErrIdentityNotFound = repo.ErrIdentityNotFound
	ErrSubjectNotFound  = repo.ErrSubjectNotFound
	ErrConflict         = repo.ErrConflict
)

type repository interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	MergeIdentityReviewTx(ctx context.Context, tx pgx.Tx, in repo.MergeIdentityReviewInput) (repo.MergeIdentityReviewResult, error)
	SplitIdentityReviewTx(ctx context.Context, tx pgx.Tx, in repo.SplitIdentityReviewInput) (repo.SplitIdentityReviewResult, error)
	ListRecentMerges(ctx context.Context, tenantID string, limit int) ([]repo.RecentMerge, error)
	ListSubjectRoster(ctx context.Context, tenantID string, limit int) (repo.SubjectRoster, error)
	SubjectDetail(ctx context.Context, tenantID string, subjectID uuid.UUID, eventLimit int) (repo.SubjectDetail, error)
}

type Service struct {
	repo  repository
	audit *auditlogsvc.Service
}

func New(r repository, audit *auditlogsvc.Service) *Service {
	return ptrext.Of(Service{repo: r, audit: audit})
}

type MergeIdentityReviewInput struct {
	TenantID      string
	IdentityKind  string
	IdentityValue string
	FeedbackIDs   []int64
	Note          string
	Actor         auditlogsvc.Actor
}

type MergeIdentityReviewResult struct {
	Subject        repo.Subject
	EvidenceCount  int
	CreatedSubject bool
}

type SplitIdentityReviewInput struct {
	TenantID      string
	SubjectID     string
	IdentityKind  string
	IdentityValue string
	Note          string
	Actor         auditlogsvc.Actor
}

type SplitIdentityReviewResult struct {
	Subject       repo.Subject
	EvidenceCount int
}

func (s *Service) MergeIdentityReview(
	ctx context.Context,
	in MergeIdentityReviewInput,
) (MergeIdentityReviewResult, error) {
	normalized, err := normalizeMergeIdentityReview(in)
	if err != nil {
		return MergeIdentityReviewResult{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return MergeIdentityReviewResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := s.repo.MergeIdentityReviewTx(ctx, tx, normalized)
	if err != nil {
		return MergeIdentityReviewResult{}, err
	}
	if err := s.recordMergeAuditTx(ctx, tx, normalized, in.Actor, result); err != nil {
		return MergeIdentityReviewResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return MergeIdentityReviewResult{}, err
	}
	return MergeIdentityReviewResult{
		Subject:        result.Subject,
		EvidenceCount:  result.EvidenceCount,
		CreatedSubject: result.CreatedSubject,
	}, nil
}

func (s *Service) SplitIdentityReview(
	ctx context.Context,
	in SplitIdentityReviewInput,
) (SplitIdentityReviewResult, error) {
	normalized, err := normalizeSplitIdentityReview(in)
	if err != nil {
		return SplitIdentityReviewResult{}, err
	}
	tx, err := s.repo.Begin(ctx)
	if err != nil {
		return SplitIdentityReviewResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := s.repo.SplitIdentityReviewTx(ctx, tx, normalized)
	if err != nil {
		return SplitIdentityReviewResult{}, err
	}
	if err := s.recordSplitAuditTx(ctx, tx, normalized, in.Actor, result); err != nil {
		return SplitIdentityReviewResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return SplitIdentityReviewResult{}, err
	}
	return SplitIdentityReviewResult{Subject: result.Subject, EvidenceCount: result.EvidenceCount}, nil
}

func (s *Service) RecentIdentityMerges(ctx context.Context, tenantID string, limit int) ([]repo.RecentMerge, error) {
	trimmed := strings.TrimSpace(tenantID)
	if trimmed == "" {
		return nil, ErrValidation
	}
	return s.repo.ListRecentMerges(ctx, trimmed, limit)
}

func (s *Service) SubjectRoster(ctx context.Context, tenantID string, limit int) (repo.SubjectRoster, error) {
	trimmed := strings.TrimSpace(tenantID)
	if trimmed == "" {
		return repo.SubjectRoster{}, ErrValidation
	}
	return s.repo.ListSubjectRoster(ctx, trimmed, limit)
}

func (s *Service) SubjectDetail(
	ctx context.Context,
	tenantID string,
	subjectID string,
	eventLimit int,
) (repo.SubjectDetail, error) {
	trimmedTenant := strings.TrimSpace(tenantID)
	parsedSubjectID, err := uuid.Parse(strings.TrimSpace(subjectID))
	if err != nil {
		return repo.SubjectDetail{}, ErrValidation
	}
	if trimmedTenant == "" {
		return repo.SubjectDetail{}, ErrValidation
	}
	return s.repo.SubjectDetail(ctx, trimmedTenant, parsedSubjectID, eventLimit)
}

func normalizeMergeIdentityReview(in MergeIdentityReviewInput) (repo.MergeIdentityReviewInput, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	kind := strings.ToLower(strings.TrimSpace(in.IdentityKind))
	value := strings.TrimSpace(in.IdentityValue)
	note := strings.TrimSpace(in.Note)
	feedbackIDs := uniquePositiveFeedbackIDs(in.FeedbackIDs)
	if tenantID == "" || value == "" || !stableIdentityKind(kind) || len(feedbackIDs) < 2 || len(feedbackIDs) > 50 {
		return repo.MergeIdentityReviewInput{}, ErrValidation
	}
	if utf8.RuneCountInString(value) > 512 || utf8.RuneCountInString(note) > 1000 {
		return repo.MergeIdentityReviewInput{}, ErrValidation
	}
	actorID := strings.TrimSpace(in.Actor.ID)
	if actorID == "" {
		actorID = "system"
	}
	return repo.MergeIdentityReviewInput{
		TenantID:                tenantID,
		ActorID:                 actorID,
		IdentityKind:            kind,
		IdentityValue:           value,
		IdentityValueNormalized: repo.NormalizeIdentityValue(kind, value),
		FeedbackIDs:             feedbackIDs,
		Note:                    note,
	}, nil
}

func normalizeSplitIdentityReview(in SplitIdentityReviewInput) (repo.SplitIdentityReviewInput, error) {
	tenantID := strings.TrimSpace(in.TenantID)
	kind := strings.ToLower(strings.TrimSpace(in.IdentityKind))
	value := strings.TrimSpace(in.IdentityValue)
	note := strings.TrimSpace(in.Note)
	subjectID, err := uuid.Parse(strings.TrimSpace(in.SubjectID))
	if err != nil {
		return repo.SplitIdentityReviewInput{}, ErrValidation
	}
	if tenantID == "" || value == "" || !stableIdentityKind(kind) {
		return repo.SplitIdentityReviewInput{}, ErrValidation
	}
	if utf8.RuneCountInString(value) > 512 || utf8.RuneCountInString(note) > 1000 {
		return repo.SplitIdentityReviewInput{}, ErrValidation
	}
	actorID := strings.TrimSpace(in.Actor.ID)
	if actorID == "" {
		actorID = "system"
	}
	return repo.SplitIdentityReviewInput{
		TenantID:                tenantID,
		ActorID:                 actorID,
		SubjectID:               subjectID,
		IdentityKind:            kind,
		IdentityValue:           value,
		IdentityValueNormalized: repo.NormalizeIdentityValue(kind, value),
		Note:                    note,
	}, nil
}

func uniquePositiveFeedbackIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
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
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func stableIdentityKind(kind string) bool {
	switch kind {
	case "email", "external_id", "source_contact_id", "crm_id", "support_id":
		return true
	default:
		return false
	}
}

func (s *Service) recordMergeAuditTx(
	ctx context.Context,
	tx pgx.Tx,
	in repo.MergeIdentityReviewInput,
	actor auditlogsvc.Actor,
	result repo.MergeIdentityReviewResult,
) error {
	if s.audit == nil {
		return nil
	}
	if actor.Type == "" {
		actor.Type = "admin"
	}
	if actor.ID == "" {
		actor.ID = in.ActorID
	}
	return s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   in.TenantID,
		Actor:      actor,
		Action:     "signal_subject.merge",
		TargetType: "signal_subject",
		TargetID:   result.Subject.ID.String(),
		Summary:    fmt.Sprintf("Merged %d feedback rows into signal subject", result.EvidenceCount),
		After: map[string]any{
			"identity_kind":       in.IdentityKind,
			"identity_value_hash": identityValueHash(in.IdentityKind, in.IdentityValueNormalized),
			"feedback_count":      len(in.FeedbackIDs),
			"evidence_count":      result.EvidenceCount,
			"created_subject":     result.CreatedSubject,
			"identity_count":      result.Subject.IdentityCount,
		},
	})
}

func (s *Service) recordSplitAuditTx(
	ctx context.Context,
	tx pgx.Tx,
	in repo.SplitIdentityReviewInput,
	actor auditlogsvc.Actor,
	result repo.SplitIdentityReviewResult,
) error {
	if s.audit == nil {
		return nil
	}
	if actor.Type == "" {
		actor.Type = "admin"
	}
	if actor.ID == "" {
		actor.ID = in.ActorID
	}
	return s.audit.RecordTx(ctx, tx, auditlogsvc.Event{
		TenantID:   in.TenantID,
		Actor:      actor,
		Action:     "signal_subject.split",
		TargetType: "signal_subject",
		TargetID:   result.Subject.ID.String(),
		Summary:    "Split identity from signal subject",
		After: map[string]any{
			"identity_kind":       in.IdentityKind,
			"identity_value_hash": identityValueHash(in.IdentityKind, in.IdentityValueNormalized),
			"evidence_count":      result.EvidenceCount,
			"identity_count":      result.Subject.IdentityCount,
		},
	})
}

func identityValueHash(kind string, normalizedValue string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + normalizedValue))
	return fmt.Sprintf("%x", sum)
}
