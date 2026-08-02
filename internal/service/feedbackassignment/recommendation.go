package feedbackassignment

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
)

const (
	terminalRecommendationAttempts     = 5
	maxRecommendationOperatorNoteRunes = 850
)

type RecommendationInput struct {
	TenantID    string
	FeedbackIDs []int64
	Now         time.Time
}

type RecommendationResult struct {
	TotalMatched    int
	Recommendations []Recommendation
	Failed          []BatchFailure
}

type Recommendation struct {
	FeedbackID               int64
	RuleKey                  string
	RuleName                 string
	OwnerLane                string
	Severity                 string
	SLAHours                 int
	SLADueAt                 *time.Time
	Rationale                string
	AlreadySatisfied         bool
	RecommendedOwnerMemberID *string
	Current                  feedbackrepo.Assignment
}

type ApplyRecommendationInput struct {
	TenantID      string
	FeedbackIDs   []int64
	OwnerMemberID *string
	Note          string
	ActorID       string
	Now           time.Time
}

type ApplyRecommendationResult struct {
	TotalMatched int
	Succeeded    int
	Skipped      int
	Failed       []BatchFailure
	Applied      []Recommendation
}

type assignmentRule struct {
	Key                  string
	Name                 string
	OwnerLane            string
	Severity             string
	SLAHours             int
	Rationale            string
	DefaultOwnerMemberID *string
}

func (s *Service) RecommendBatch(ctx context.Context, input RecommendationInput) (RecommendationResult, error) {
	normalized, err := normalizeRecommendationInput(input)
	if err != nil {
		return RecommendationResult{}, err
	}
	return s.recommendNormalized(ctx, normalized)
}

func (s *Service) ApplyRecommendations(
	ctx context.Context,
	input ApplyRecommendationInput,
) (ApplyRecommendationResult, error) {
	normalized, err := normalizeApplyRecommendationInput(input)
	if err != nil {
		return ApplyRecommendationResult{}, err
	}
	if err := s.validateOwner(ctx, normalized.TenantID, normalized.OwnerMemberID != nil, normalized.OwnerMemberID); err != nil {
		return ApplyRecommendationResult{}, err
	}
	recs, err := s.recommendNormalized(ctx, RecommendationInput{
		TenantID:    normalized.TenantID,
		FeedbackIDs: normalized.FeedbackIDs,
		Now:         normalized.Now,
	})
	if err != nil {
		return ApplyRecommendationResult{}, err
	}
	return s.applyRecommendedAssignments(ctx, normalized, recs)
}

func (s *Service) recommendNormalized(ctx context.Context, input RecommendationInput) (RecommendationResult, error) {
	policy, err := s.loadAssignmentPolicy(ctx, input.TenantID)
	if err != nil {
		return RecommendationResult{}, err
	}
	candidates, err := s.store.AssignmentCandidates(ctx, input.TenantID, input.FeedbackIDs)
	if err != nil {
		return RecommendationResult{}, err
	}
	return recommendationResultFromCandidates(input, candidates, policy), nil
}

func recommendationResultFromCandidates(
	input RecommendationInput,
	candidates []feedbackrepo.AssignmentCandidate,
	policy Policy,
) RecommendationResult {
	byID := assignmentCandidateMap(candidates)
	rules := assignmentRulesFromPolicy(policy)
	result := RecommendationResult{TotalMatched: len(input.FeedbackIDs)}
	for _, id := range input.FeedbackIDs {
		candidate, ok := byID[id]
		if !ok {
			result.Failed = append(result.Failed, recommendationFailure(id, "NOT_FOUND", "feedback not found"))
			continue
		}
		rec, ok := recommendationForCandidate(candidate, input.Now, rules)
		if !ok {
			result.Failed = append(result.Failed, recommendationFailure(id, "NO_RULE", "no assignment recommendation matched"))
			continue
		}
		result.Recommendations = append(result.Recommendations, rec)
	}
	return result
}

func (s *Service) applyRecommendedAssignments(
	ctx context.Context,
	input ApplyRecommendationInput,
	recs RecommendationResult,
) (ApplyRecommendationResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ApplyRecommendationResult{}, fmt.Errorf("begin recommended assignment tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	result := ApplyRecommendationResult{TotalMatched: recs.TotalMatched, Failed: recs.Failed}
	ownerValidation := map[string]error{}
	for _, rec := range recs.Recommendations {
		if shouldSkipRecommendation(rec, input) {
			result.Skipped++
			continue
		}
		item := recommendedAssignmentInput(input, rec)
		if err := s.validateRecommendedOwner(ctx, input.TenantID, item.OwnerMemberID, ownerValidation); err != nil {
			if errors.Is(err, ErrOwnerNotFound) {
				result.Failed = append(result.Failed, recommendationFailure(rec.FeedbackID, "OWNER_NOT_FOUND", "owner member not found"))
				continue
			}
			return ApplyRecommendationResult{}, err
		}
		if _, err := s.assignBatchItem(ctx, tx, item, rec.FeedbackID); err != nil {
			if errors.Is(err, ErrNotFound) {
				result.Failed = append(result.Failed, recommendationFailure(rec.FeedbackID, "NOT_FOUND", "feedback not found"))
				continue
			}
			return ApplyRecommendationResult{}, err
		}
		result.Succeeded++
		result.Applied = append(result.Applied, rec)
	}
	if err := tx.Commit(ctx); err != nil {
		return ApplyRecommendationResult{}, fmt.Errorf("commit recommended assignment tx: %w", err)
	}
	return result, nil
}

func normalizeRecommendationInput(input RecommendationInput) (RecommendationInput, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.FeedbackIDs = uniquePositiveFeedbackIDs(input.FeedbackIDs)
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	input.Now = input.Now.UTC()
	if input.TenantID == "" || len(input.FeedbackIDs) == 0 {
		return RecommendationInput{}, ErrValidation
	}
	if len(input.FeedbackIDs) > MaxBatchSize {
		return RecommendationInput{}, fmt.Errorf("%w: too many feedback ids", ErrValidation)
	}
	return input, nil
}

func normalizeApplyRecommendationInput(input ApplyRecommendationInput) (ApplyRecommendationInput, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.Note = strings.TrimSpace(input.Note)
	input.FeedbackIDs = uniquePositiveFeedbackIDs(input.FeedbackIDs)
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	input.Now = input.Now.UTC()
	if input.TenantID == "" || input.ActorID == "" || len(input.FeedbackIDs) == 0 {
		return ApplyRecommendationInput{}, ErrValidation
	}
	if len(input.FeedbackIDs) > MaxBatchSize {
		return ApplyRecommendationInput{}, fmt.Errorf("%w: too many feedback ids", ErrValidation)
	}
	if len([]rune(input.Note)) > maxRecommendationOperatorNoteRunes {
		return ApplyRecommendationInput{}, fmt.Errorf("%w: note too long", ErrValidation)
	}
	if input.OwnerMemberID != nil {
		ownerID := strings.TrimSpace(ptrext.Indirect(input.OwnerMemberID))
		input.OwnerMemberID = ptrext.Of(ownerID)
	}
	return input, nil
}

func assignmentCandidateMap(candidates []feedbackrepo.AssignmentCandidate) map[int64]feedbackrepo.AssignmentCandidate {
	out := make(map[int64]feedbackrepo.AssignmentCandidate, len(candidates))
	for _, item := range candidates {
		out[item.ID] = item
	}
	return out
}

func recommendationForCandidate(
	candidate feedbackrepo.AssignmentCandidate,
	now time.Time,
	rules map[string]assignmentRule,
) (Recommendation, bool) {
	rule, ok := assignmentRuleForCandidate(candidate, rules)
	if !ok {
		return Recommendation{}, false
	}
	dueAt := candidate.CreatedAt.UTC().Add(time.Duration(rule.SLAHours) * time.Hour)
	rec := Recommendation{
		FeedbackID:               candidate.ID,
		RuleKey:                  rule.Key,
		RuleName:                 rule.Name,
		OwnerLane:                rule.OwnerLane,
		Severity:                 rule.Severity,
		SLAHours:                 rule.SLAHours,
		SLADueAt:                 ptrext.Of(dueAt),
		Rationale:                recommendationRationale(rule, candidate, now),
		AlreadySatisfied:         assignmentAlreadySatisfied(candidate.Assignment, dueAt),
		RecommendedOwnerMemberID: rule.DefaultOwnerMemberID,
		Current:                  candidate.Assignment,
	}
	return rec, true
}

func assignmentRuleForCandidate(
	candidate feedbackrepo.AssignmentCandidate,
	rules map[string]assignmentRule,
) (assignmentRule, bool) {
	closed := candidate.WorkflowCategory == "closed"
	if candidate.IsUrgent && !closed {
		return lookupAssignmentRule(rules, "urgent_open")
	}
	if isTerminalCandidate(candidate) {
		return lookupAssignmentRule(rules, "terminal_failures")
	}
	if candidate.WorkflowCategory == "active" {
		return lookupAssignmentRule(rules, "stalled_active")
	}
	if !closed && !candidate.HasStableIdentity {
		return lookupAssignmentRule(rules, "identity_debt")
	}
	if candidate.WorkflowCategory == "" || candidate.WorkflowCategory == "open" {
		return lookupAssignmentRule(rules, "untriaged")
	}
	return assignmentRule{}, false
}

func lookupAssignmentRule(rules map[string]assignmentRule, key string) (assignmentRule, bool) {
	rule, ok := rules[key]
	return rule, ok
}

func isTerminalCandidate(candidate feedbackrepo.AssignmentCandidate) bool {
	return candidate.EnrichmentStatus == "failed" &&
		candidate.EnrichmentAttempts >= terminalRecommendationAttempts &&
		candidate.EnrichmentNextRetryAt == nil
}

func assignmentAlreadySatisfied(current feedbackrepo.Assignment, recommendedDueAt time.Time) bool {
	return current.SLADueAt != nil && !current.SLADueAt.UTC().After(recommendedDueAt.UTC())
}

func shouldSkipRecommendation(rec Recommendation, input ApplyRecommendationInput) bool {
	return rec.AlreadySatisfied && recommendationOwner(input, rec) == nil && input.Note == ""
}

func recommendedAssignmentInput(input ApplyRecommendationInput, rec Recommendation) BatchInput {
	ownerID := recommendationOwner(input, rec)
	return BatchInput{
		TenantID:         input.TenantID,
		FeedbackIDs:      []int64{rec.FeedbackID},
		OwnerMemberIDSet: ownerID != nil,
		OwnerMemberID:    ownerID,
		SLADueAtSet:      rec.SLADueAt != nil,
		SLADueAt:         rec.SLADueAt,
		Note:             recommendedAssignmentNote(rec, input.Note),
		ActorID:          input.ActorID,
	}
}

func recommendationOwner(input ApplyRecommendationInput, rec Recommendation) *string {
	if input.OwnerMemberID != nil {
		return input.OwnerMemberID
	}
	return rec.RecommendedOwnerMemberID
}

func (s *Service) validateRecommendedOwner(
	ctx context.Context,
	tenantID string,
	ownerID *string,
	cache map[string]error,
) error {
	if ownerID == nil {
		return nil
	}
	key := ptrext.Indirect(ownerID)
	err, ok := cache[key]
	if ok {
		return err
	}
	err = s.validateOwner(ctx, tenantID, true, ownerID)
	cache[key] = err
	return err
}

func recommendedAssignmentNote(rec Recommendation, operatorNote string) string {
	base := fmt.Sprintf("Assignment policy: %s (%s, %dh).", rec.RuleName, rec.OwnerLane, rec.SLAHours)
	if operatorNote == "" {
		return base
	}
	return base + " " + operatorNote
}

func recommendationRationale(
	rule assignmentRule,
	candidate feedbackrepo.AssignmentCandidate,
	now time.Time,
) string {
	ageHours := int(now.Sub(candidate.CreatedAt.UTC()).Hours())
	if ageHours < 0 {
		ageHours = 0
	}
	return fmt.Sprintf("%s Source=%s type=%s age=%dh.", rule.Rationale, candidate.Source, candidate.Type, ageHours)
}

func recommendationFailure(feedbackID int64, code string, message string) BatchFailure {
	return BatchFailure{FeedbackID: feedbackID, Code: code, Message: message}
}

func policyDryRunImpacts(
	feedbackIDs []int64,
	current []Recommendation,
	draft []Recommendation,
) ([]PolicyDryRunImpact, int) {
	currentByID := recommendationMap(current)
	draftByID := recommendationMap(draft)
	out := make([]PolicyDryRunImpact, 0, len(feedbackIDs))
	changed := 0
	for _, feedbackID := range feedbackIDs {
		currentRec, hasCurrent := currentByID[feedbackID]
		draftRec, hasDraft := draftByID[feedbackID]
		if !hasCurrent && !hasDraft {
			continue
		}
		impact := PolicyDryRunImpact{FeedbackID: feedbackID}
		if hasCurrent {
			impact = dryRunImpactWithCurrent(impact, currentRec)
		}
		if hasDraft {
			impact = dryRunImpactWithDraft(impact, draftRec)
		}
		impact.Changed = !recommendationsEqual(currentRec, hasCurrent, draftRec, hasDraft)
		if impact.Changed {
			changed++
		}
		out = append(out, impact)
	}
	return out, changed
}

func recommendationMap(items []Recommendation) map[int64]Recommendation {
	out := make(map[int64]Recommendation, len(items))
	for _, item := range items {
		out[item.FeedbackID] = item
	}
	return out
}

func dryRunImpactWithCurrent(impact PolicyDryRunImpact, rec Recommendation) PolicyDryRunImpact {
	impact.CurrentRuleKey = rec.RuleKey
	impact.CurrentRuleName = rec.RuleName
	impact.CurrentOwnerLane = rec.OwnerLane
	impact.CurrentSLAHours = rec.SLAHours
	impact.CurrentOwnerMemberID = rec.RecommendedOwnerMemberID
	return impact
}

func dryRunImpactWithDraft(impact PolicyDryRunImpact, rec Recommendation) PolicyDryRunImpact {
	impact.DraftRuleKey = rec.RuleKey
	impact.DraftRuleName = rec.RuleName
	impact.DraftOwnerLane = rec.OwnerLane
	impact.DraftSLAHours = rec.SLAHours
	impact.DraftOwnerMemberID = rec.RecommendedOwnerMemberID
	return impact
}

func recommendationsEqual(
	current Recommendation,
	hasCurrent bool,
	draft Recommendation,
	hasDraft bool,
) bool {
	if hasCurrent != hasDraft {
		return false
	}
	if !hasCurrent {
		return true
	}
	return current.RuleKey == draft.RuleKey &&
		current.OwnerLane == draft.OwnerLane &&
		current.SLAHours == draft.SLAHours &&
		stringPtrEqual(current.RecommendedOwnerMemberID, draft.RecommendedOwnerMemberID)
}

func stringPtrEqual(left *string, right *string) bool {
	return ptrext.IndirectOr(left, "") == ptrext.IndirectOr(right, "")
}

func urgentOpenRule() assignmentRule {
	return assignmentRule{
		Key:       "urgent_open",
		Name:      "Urgent open feedback",
		OwnerLane: "support_triage",
		Severity:  "critical",
		SLAHours:  24,
		Rationale: "Urgent open feedback should be confirmed and assigned before the next business cycle.",
	}
}

func terminalFailureRule() assignmentRule {
	return assignmentRule{
		Key:       "terminal_failures",
		Name:      "Terminal AI failures",
		OwnerLane: "ai_ops",
		Severity:  "high",
		SLAHours:  48,
		Rationale: "Terminal enrichment failures need operator review before retrying or changing model configuration.",
	}
}

func stalledActiveRule() assignmentRule {
	return assignmentRule{
		Key:       "stalled_active",
		Name:      "Active work at risk",
		OwnerLane: "product_owner",
		Severity:  "high",
		SLAHours:  168,
		Rationale: "Active feedback should keep an explicit deadline so committed work does not disappear.",
	}
}

func identityDebtRule() assignmentRule {
	return assignmentRule{
		Key:       "identity_debt",
		Name:      "Identity evidence debt",
		OwnerLane: "data_quality",
		Severity:  "medium",
		SLAHours:  96,
		Rationale: "Feedback without stable identity evidence should be repaired before merging demand or notifying customers.",
	}
}

func untriagedRule() assignmentRule {
	return assignmentRule{
		Key:       "untriaged",
		Name:      "Untriaged intake",
		OwnerLane: "triage_dri",
		Severity:  "high",
		SLAHours:  72,
		Rationale: "Open intake should get an owner lane and deadline before promotion or closure decisions.",
	}
}

func defaultAssignmentRules() []assignmentRule {
	return []assignmentRule{
		urgentOpenRule(),
		terminalFailureRule(),
		stalledActiveRule(),
		identityDebtRule(),
		untriagedRule(),
	}
}
