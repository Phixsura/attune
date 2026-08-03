// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

const (
	defaultEscalationDueHours = 8
	maxEscalationDueHours     = 24 * 14

	escalationReasonCritical = "critical_recovery"
	escalationReasonHighRisk = "high_risk_recovery"

	defaultRecoveryAutomationLimit = 10
	maxRecoveryAutomationLimit     = 50
	recoveryAutomationActor        = "system:survey-recovery-worker"
	recoveryAutomationMarker       = "automation=survey_recovery_worker"
)

func (s *Service) EscalateLowScoreReviews(ctx context.Context, in EscalationInput) (EscalationResult, error) {
	responseIDs, dueHours, note, err := validateEscalationInput(in)
	if err != nil {
		return EscalationResult{}, err
	}
	out := EscalationResult{
		Reviews:   make([]repo.LowScoreReview, 0, len(responseIDs)),
		Decisions: make([]EscalationDecision, 0, len(responseIDs)),
	}
	for _, responseID := range responseIDs {
		review, decision, err := s.escalateLowScoreReview(ctx, in, responseID, dueHours, note)
		if err != nil {
			return EscalationResult{}, err
		}
		out.Reviews = append(out.Reviews, review)
		out.Decisions = append(out.Decisions, decision)
	}
	return out, nil
}

func (s *Service) ProcessRecoveryAutomation(
	ctx context.Context,
	in RecoveryAutomationInput,
) (RecoveryAutomationResult, error) {
	limit, owner, dueHours, err := validateRecoveryAutomationInput(in)
	if err != nil {
		return RecoveryAutomationResult{}, err
	}
	claimed, err := s.repo.ClaimLowScoreReviewsForRecoveryAutomation(ctx, limit, owner)
	if err != nil {
		return RecoveryAutomationResult{}, mapRepoError(err)
	}
	out := RecoveryAutomationResult{
		Claimed:   len(claimed),
		Reviews:   make([]repo.LowScoreReview, 0, len(claimed)),
		Decisions: make([]EscalationDecision, 0, len(claimed)),
	}
	for _, review := range claimed {
		item, decision, ok, err := s.processClaimedRecoveryAutomation(ctx, review, dueHours, owner)
		if err != nil {
			return RecoveryAutomationResult{}, err
		}
		if !ok {
			out.Skipped++
			continue
		}
		out.Escalated++
		out.Reviews = append(out.Reviews, item)
		out.Decisions = append(out.Decisions, decision)
		if s.enqueueRecoveryNotification(ctx, item, decision) {
			out.NotificationsEnqueued++
		} else {
			out.NotificationsSkipped++
		}
	}
	return out, nil
}

func validateEscalationInput(in EscalationInput) ([]uuid.UUID, int, string, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.ActorID) == "" {
		return nil, 0, "", ErrValidation
	}
	responseIDs, err := dedupeUUIDs(in.ResponseIDs, maxBatchLowScoreReviews)
	if err != nil {
		return nil, 0, "", err
	}
	dueHours, err := normalizedEscalationDueHours(in.DueInHours)
	if err != nil {
		return nil, 0, "", err
	}
	return responseIDs, dueHours, boundedString(strings.TrimSpace(in.Note), 240), nil
}

func normalizedEscalationDueHours(value int) (int, error) {
	if value == 0 {
		return defaultEscalationDueHours, nil
	}
	if value < 1 || value > maxEscalationDueHours {
		return 0, ErrValidation
	}
	return value, nil
}

func validateRecoveryAutomationInput(in RecoveryAutomationInput) (int, string, int, error) {
	limit := in.Limit
	if limit == 0 {
		limit = defaultRecoveryAutomationLimit
	}
	if limit < 0 || limit > maxRecoveryAutomationLimit {
		return 0, "", 0, ErrValidation
	}
	owner := strings.TrimSpace(in.Owner)
	if owner == "" {
		owner = recoveryAutomationActor
	}
	dueHours, err := normalizedEscalationDueHours(in.DueInHours)
	if err != nil {
		return 0, "", 0, err
	}
	return limit, boundedString(owner, 256), dueHours, nil
}

func (s *Service) escalateLowScoreReview(
	ctx context.Context,
	in EscalationInput,
	responseID uuid.UUID,
	dueHours int,
	note string,
) (repo.LowScoreReview, EscalationDecision, error) {
	current, err := s.repo.GetLowScoreReview(ctx, strings.TrimSpace(in.TenantID), responseID)
	if err != nil {
		return repo.LowScoreReview{}, EscalationDecision{}, mapRepoError(err)
	}
	if current.Status == repo.ReviewResolved || current.Status == repo.ReviewDismissed {
		return repo.LowScoreReview{}, EscalationDecision{}, ErrConflict
	}
	now := s.now().UTC()
	next := escalationReview(current, now, dueHours, note, in.ActorID)
	decision := escalationDecision(current, next, now)
	item, err := s.repo.UpdateLowScoreReview(ctx, next)
	if err != nil {
		return repo.LowScoreReview{}, EscalationDecision{}, mapRepoError(err)
	}
	return item, decision, nil
}

func escalationReview(
	current repo.LowScoreReview,
	now time.Time,
	dueHours int,
	note string,
	actorID string,
) repo.LowScoreReview {
	next := current
	next.Status = repo.ReviewInReview
	next.Severity = repo.SeverityCritical
	dueAt := escalationDueAt(current, now, dueHours)
	next.DueAt = ptrext.Of(dueAt)
	next.ActionTaken = escalationActionTaken(current, dueAt, now, note)
	next.UpdatedBy = strings.TrimSpace(actorID)
	return next
}

func escalationDueAt(review repo.LowScoreReview, now time.Time, dueHours int) time.Time {
	dueAt := now.Add(time.Duration(dueHours) * time.Hour)
	if review.DueAt != nil && ptrext.Indirect(review.DueAt).Before(dueAt) {
		return ptrext.Indirect(review.DueAt)
	}
	return dueAt
}

func escalationActionTaken(review repo.LowScoreReview, dueAt time.Time, now time.Time, note string) string {
	reason := escalationReason(review, now)
	action := fmt.Sprintf("Escalated recovery: reason=%s; severity=%s; due_at=%s.",
		reason,
		repo.SeverityCritical,
		dueAt.UTC().Format(time.RFC3339),
	)
	if note != "" {
		action += " Note: " + note
	}
	return appendRecoveryAction(review.ActionTaken, action)
}

func appendRecoveryAction(existing string, action string) string {
	existing = strings.TrimSpace(existing)
	action = strings.TrimSpace(action)
	if existing == "" {
		return boundedString(action, 5000)
	}
	if len(existing)+1+len(action) <= 5000 {
		return existing + "\n" + action
	}
	if len(action) >= 5000 {
		return boundedString(action, 5000)
	}
	keepExisting := 5000 - len(action) - 1
	return boundedString(existing, keepExisting) + "\n" + action
}

func escalationDecision(current repo.LowScoreReview, next repo.LowScoreReview, now time.Time) EscalationDecision {
	dueAt := ptrext.Indirect(next.DueAt)
	return EscalationDecision{
		ResponseID:       current.ResponseID,
		PreviousSeverity: current.Severity,
		Severity:         next.Severity,
		PreviousDueAt:    current.DueAt,
		DueAt:            dueAt,
		OwnerMissing:     next.OwnerMemberID == nil,
		DueAtChanged:     current.DueAt == nil || !ptrext.Indirect(current.DueAt).Equal(dueAt),
		Reason:           escalationReason(current, now),
		ActionTaken:      next.ActionTaken,
	}
}

func escalationReason(review repo.LowScoreReview, now time.Time) string {
	if review.DueAt != nil && ptrext.Indirect(review.DueAt).Before(now) {
		return repo.RecoveryBlockerOverdue
	}
	if review.Severity == repo.SeverityCritical {
		return escalationReasonCritical
	}
	if review.OwnerMemberID == nil {
		return repo.RecoveryBlockerOwner
	}
	if review.DueAt == nil {
		return repo.RecoveryBlockerDue
	}
	return escalationReasonHighRisk
}

func (s *Service) processClaimedRecoveryAutomation(
	ctx context.Context,
	current repo.LowScoreReview,
	dueHours int,
	owner string,
) (repo.LowScoreReview, EscalationDecision, bool, error) {
	now := s.now().UTC()
	if !reviewNeedsRecoveryAutomation(current, now) {
		recordRecoveryAutomation(current.TenantID, "skipped", "not_eligible")
		return repo.LowScoreReview{}, EscalationDecision{}, false, nil
	}
	note := recoveryAutomationNote(current, now)
	next := escalationReview(current, now, dueHours, note, owner)
	decision := escalationDecision(current, next, now)
	item, err := s.repo.UpdateLowScoreReview(ctx, next)
	if err != nil {
		recordRecoveryAutomation(current.TenantID, "error", "update_failed")
		return repo.LowScoreReview{}, EscalationDecision{}, false, mapRepoError(err)
	}
	recordRecoveryAutomation(item.TenantID, "escalated", decision.Reason)
	return item, decision, true, nil
}

func reviewNeedsRecoveryAutomation(review repo.LowScoreReview, now time.Time) bool {
	if review.Status == repo.ReviewResolved || review.Status == repo.ReviewDismissed {
		return false
	}
	if strings.Contains(review.ActionTaken, recoveryAutomationMarker) {
		return false
	}
	return review.DueAt == nil ||
		ptrext.Indirect(review.DueAt).Before(now) ||
		review.Severity == repo.SeverityCritical ||
		recoveryOwnerGapExpired(review, now)
}

func recoveryOwnerGapExpired(review repo.LowScoreReview, now time.Time) bool {
	return review.OwnerMemberID == nil && !review.CreatedAt.IsZero() && !review.CreatedAt.After(now.Add(-24*time.Hour))
}

func recoveryAutomationNote(review repo.LowScoreReview, now time.Time) string {
	reason := escalationReason(review, now)
	return recoveryAutomationMarker + "; trigger=" + reason
}

func recordRecoveryAutomation(tenantID string, result string, reason string) {
	metrics.SurveyRecoveryAutomationTotal.WithLabelValues(
		strings.TrimSpace(tenantID),
		strings.TrimSpace(result),
		strings.TrimSpace(reason),
	).Inc()
}

func (s *Service) enqueueRecoveryNotification(
	ctx context.Context,
	review repo.LowScoreReview,
	decision EscalationDecision,
) bool {
	if review.OwnerMemberID == nil {
		recordRecoveryNotification(review.TenantID, "skipped", "owner_missing")
		return false
	}
	details, err := s.repo.RecoveryNotificationContext(ctx, review.TenantID, review.ResponseID)
	if err != nil {
		recordRecoveryNotification(review.TenantID, "skipped", "owner_unavailable")
		return false
	}
	if !usableRecoveryOwnerEmail(details.Owner.Email) {
		recordRecoveryNotification(review.TenantID, "skipped", "owner_email_missing")
		return false
	}
	_, created, err := s.repo.EnsureRecoveryNotification(ctx, repo.RecoveryNotificationInput{
		TenantID:        review.TenantID,
		ResponseID:      review.ResponseID,
		OwnerMemberID:   details.Owner.ID,
		Reason:          decision.Reason,
		DestinationHash: repo.DestinationHash(details.Owner.Email),
		Payload:         s.recoveryNotificationPayload(details, decision),
	})
	if err != nil {
		recordRecoveryNotification(review.TenantID, "error", "enqueue_failed")
		return false
	}
	if !created {
		recordRecoveryNotification(review.TenantID, "skipped", "duplicate")
		return false
	}
	recordRecoveryNotification(review.TenantID, "enqueued", decision.Reason)
	return true
}

func usableRecoveryOwnerEmail(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" {
		return false
	}
	parsed, err := mail.ParseAddress(email)
	return err == nil && strings.EqualFold(strings.TrimSpace(parsed.Address), email)
}

func (s *Service) recoveryNotificationPayload(
	details repo.RecoveryNotificationContext,
	decision EscalationDecision,
) map[string]any {
	survey := map[string]any{
		"kind":          "low_score_recovery",
		"campaign_id":   details.CampaignID.String(),
		"campaign_name": details.CampaignName,
		"survey_type":   details.SurveyType,
		"response_id":   details.ResponseID.String(),
		"score":         details.Score,
		"comment":       boundedString(details.Comment, 1000),
		"source_type":   details.SourceType,
		"source_id":     details.SourceID,
		"status":        details.ReviewStatus,
		"severity":      details.Severity,
		"reason":        decision.Reason,
		"submitted_at":  details.SubmittedAt.UTC().Format(time.RFC3339),
	}
	if details.RequestID != nil {
		survey["request_id"] = ptrext.Indirect(details.RequestID).String()
	}
	if !decision.DueAt.IsZero() {
		survey["due_at"] = decision.DueAt.UTC().Format(time.RFC3339)
	}
	if s.publicBase != "" {
		survey["console_url"] = s.publicBase + "/integrations/surveys"
	}
	return map[string]any{
		"version":    "1",
		"timestamp":  s.now().UTC().Format(time.RFC3339Nano),
		"event_id":   details.ResponseID.String(),
		"event_type": surveyRecoveryEscalationEventType,
		"tenant_id":  details.TenantID,
		"survey":     survey,
		"recipient": map[string]any{
			"owner_member_id": details.Owner.ID.String(),
			"display":         details.Owner.DisplayName,
			"email":           redactedSurveyEmail(details.Owner.Email),
		},
	}
}

func recordRecoveryNotification(tenantID string, result string, reason string) {
	metrics.SurveyRecoveryNotificationTotal.WithLabelValues(
		strings.TrimSpace(tenantID),
		strings.TrimSpace(result),
		strings.TrimSpace(reason),
	).Inc()
}
