// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

const (
	assignmentReasonUnassigned = "unassigned_low_score"
	assignmentReasonOverdue    = "overdue_escalation"
	assignmentReasonCritical   = "critical_same_day"
	assignmentReasonBalance    = "load_rebalance"

	maxAssignmentDueHours = 24 * 14
)

type assignmentOwner struct {
	ID            uuid.UUID
	WorkloadScore int
}

func (s *Service) AssignLowScoreReviews(ctx context.Context, in AssignmentInput) (AssignmentResult, error) {
	responseIDs, ownerIDs, dueHours, err := validateAssignmentInput(in)
	if err != nil {
		return AssignmentResult{}, err
	}
	analytics, err := s.repo.Analytics(ctx, repo.AnalyticsFilter{TenantID: strings.TrimSpace(in.TenantID)})
	if err != nil {
		return AssignmentResult{}, mapRepoError(err)
	}
	owners := assignmentOwners(ownerIDs, analytics.OwnerRecoveryLoads)
	out := AssignmentResult{
		Reviews:   make([]repo.LowScoreReview, 0, len(responseIDs)),
		Decisions: make([]AssignmentDecision, 0, len(responseIDs)),
	}
	for _, responseID := range responseIDs {
		review, decision, err := s.assignLowScoreReview(ctx, in, responseID, dueHours, owners)
		if err != nil {
			return AssignmentResult{}, err
		}
		out.Reviews = append(out.Reviews, review)
		out.Decisions = append(out.Decisions, decision)
	}
	return out, nil
}

func validateAssignmentInput(in AssignmentInput) ([]uuid.UUID, []uuid.UUID, int, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ActorID) == "" {
		return nil, nil, 0, ErrValidation
	}
	responseIDs, err := dedupeUUIDs(in.ResponseIDs, maxBatchLowScoreReviews)
	if err != nil {
		return nil, nil, 0, err
	}
	ownerIDs, err := dedupeUUIDs(in.CandidateOwnerMemberIDs, maxBatchLowScoreReviews)
	if err != nil {
		return nil, nil, 0, err
	}
	dueHours, err := normalizedAssignmentDueHours(in.DueInHours)
	if err != nil {
		return nil, nil, 0, err
	}
	return responseIDs, ownerIDs, dueHours, nil
}

func dedupeUUIDs(ids []uuid.UUID, maxCount int) ([]uuid.UUID, error) {
	if len(ids) == 0 || len(ids) > maxCount {
		return nil, ErrValidation
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return nil, ErrValidation
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func normalizedAssignmentDueHours(value int) (int, error) {
	if value == 0 {
		return 0, nil
	}
	if value < 1 || value > maxAssignmentDueHours {
		return 0, ErrValidation
	}
	return value, nil
}

func assignmentOwners(ownerIDs []uuid.UUID, loads []repo.RecoveryOwnerLoad) []assignmentOwner {
	scoreByOwner := make(map[uuid.UUID]int, len(loads))
	for _, load := range loads {
		scoreByOwner[load.OwnerMemberID] = load.WorkloadScore
	}
	out := make([]assignmentOwner, 0, len(ownerIDs))
	for _, id := range ownerIDs {
		out = append(out, assignmentOwner{ID: id, WorkloadScore: scoreByOwner[id]})
	}
	return out
}

func (s *Service) assignLowScoreReview(
	ctx context.Context,
	in AssignmentInput,
	responseID uuid.UUID,
	dueHours int,
	owners []assignmentOwner,
) (repo.LowScoreReview, AssignmentDecision, error) {
	current, err := s.repo.GetLowScoreReview(ctx, strings.TrimSpace(in.TenantID), responseID)
	if err != nil {
		return repo.LowScoreReview{}, AssignmentDecision{}, mapRepoError(err)
	}
	if current.Status == repo.ReviewResolved || current.Status == repo.ReviewDismissed {
		return repo.LowScoreReview{}, AssignmentDecision{}, ErrConflict
	}
	ownerIndex := selectAssignmentOwner(owners)
	dueAt := assignmentDueAt(current, s.now(), dueHours)
	next := assignmentReview(current, owners[ownerIndex].ID, dueAt, in.ActorID)
	decision := assignmentDecision(current, next, owners[ownerIndex].WorkloadScore, s.now())
	owners[ownerIndex].WorkloadScore = decision.WorkloadScoreAfter
	item, err := s.repo.UpdateLowScoreReview(ctx, next)
	if err != nil {
		return repo.LowScoreReview{}, AssignmentDecision{}, mapRepoError(err)
	}
	return item, decision, nil
}

func selectAssignmentOwner(owners []assignmentOwner) int {
	best := 0
	for idx := 1; idx < len(owners); idx++ {
		if assignmentOwnerLess(owners[idx], owners[best]) {
			best = idx
		}
	}
	return best
}

func assignmentOwnerLess(left assignmentOwner, right assignmentOwner) bool {
	if left.WorkloadScore != right.WorkloadScore {
		return left.WorkloadScore < right.WorkloadScore
	}
	return left.ID.String() < right.ID.String()
}

func assignmentDueAt(review repo.LowScoreReview, now time.Time, dueHours int) time.Time {
	dueAt := now.Add(lowScoreSLA(review.Severity))
	if dueHours > 0 {
		dueAt = now.Add(time.Duration(dueHours) * time.Hour)
	}
	if review.DueAt != nil && ptrext.Indirect(review.DueAt).Before(dueAt) {
		return ptrext.Indirect(review.DueAt)
	}
	return dueAt
}

func assignmentReview(current repo.LowScoreReview, ownerID uuid.UUID, dueAt time.Time, actorID string) repo.LowScoreReview {
	next := current
	next.Status = repo.ReviewInReview
	next.OwnerMemberID = ptrext.Of(ownerID)
	next.DueAt = ptrext.Of(dueAt)
	next.UpdatedBy = strings.TrimSpace(actorID)
	return next
}

func assignmentDecision(
	current repo.LowScoreReview,
	next repo.LowScoreReview,
	scoreBefore int,
	now time.Time,
) AssignmentDecision {
	increment := assignmentWorkloadIncrement(next, now)
	return AssignmentDecision{
		ResponseID:            current.ResponseID,
		OwnerMemberID:         ptrext.Indirect(next.OwnerMemberID),
		PreviousOwnerMemberID: current.OwnerMemberID,
		DueAt:                 ptrext.Indirect(next.DueAt),
		Severity:              next.Severity,
		Escalated:             assignmentEscalated(current, now),
		Reason:                assignmentReason(current, now),
		WorkloadScoreBefore:   scoreBefore,
		WorkloadScoreAfter:    scoreBefore + increment,
	}
}

func assignmentWorkloadIncrement(review repo.LowScoreReview, now time.Time) int {
	score := 3
	dueAt := ptrext.Indirect(review.DueAt)
	if dueAt.Before(now) {
		score += 30
	} else if dueAt.Before(now.Add(24 * time.Hour)) {
		score += 12
	}
	if review.Severity == repo.SeverityCritical {
		score += 20
	}
	if !review.CustomerContacted {
		score += 8
	}
	return score
}

func assignmentEscalated(review repo.LowScoreReview, now time.Time) bool {
	return assignmentReason(review, now) == assignmentReasonOverdue ||
		assignmentReason(review, now) == assignmentReasonCritical
}

func assignmentReason(review repo.LowScoreReview, now time.Time) string {
	if review.DueAt != nil && ptrext.Indirect(review.DueAt).Before(now) {
		return assignmentReasonOverdue
	}
	if review.Severity == repo.SeverityCritical {
		return assignmentReasonCritical
	}
	if review.OwnerMemberID == nil {
		return assignmentReasonUnassigned
	}
	return assignmentReasonBalance
}
