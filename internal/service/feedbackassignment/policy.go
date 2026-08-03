package feedbackassignment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	feedbackrepo "github.com/Phixsura/attune/internal/repo/feedback"
	"github.com/Phixsura/attune/internal/repo/systemsettings"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

const (
	assignmentPolicySettingKey   = "feedback.assignment.policy"
	assignmentPolicyHistoryKey   = "feedback.assignment.policy.history"
	assignmentPolicyHistoryLimit = 20
	maxAssignmentSLAHours        = 720
	maxPolicyNoteRunes           = 500
	maxOwnerLaneRunes            = 64
)

var ownerLanePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

type PolicyStore interface {
	Get(ctx context.Context, tenantID, key string) (string, error)
	Set(ctx context.Context, tenantID, key, value, updatedBy string) error
}

type TransactionalPolicyStore interface {
	SetTx(ctx context.Context, tx pgx.Tx, tenantID, key, value, updatedBy string) error
}

type Policy struct {
	Rules     []PolicyRule
	Version   int
	UpdatedAt *time.Time
	UpdatedBy string
	Note      string
}

type PolicyRule struct {
	RuleKey              string
	RuleName             string
	OwnerLane            string
	Severity             string
	SLAHours             int
	DefaultOwnerMemberID *string
	Enabled              bool
	Rationale            string
}

type UpdatePolicyInput struct {
	TenantID            string
	Rules               []PolicyRule
	Note                string
	Actor               auditlogsvc.Actor
	RestoredFromVersion int
}

type RestorePolicyInput struct {
	TenantID string
	Version  int
	Note     string
	Actor    auditlogsvc.Actor
}

type DryRunPolicyInput struct {
	TenantID    string
	Rules       []PolicyRule
	FeedbackIDs []int64
	Now         time.Time
}

type DryRunPolicyResult struct {
	TotalMatched    int
	Changed         int
	Recommendations []Recommendation
	Failed          []BatchFailure
	Impacts         []PolicyDryRunImpact
}

type PolicyDryRunImpact struct {
	FeedbackID           int64
	CurrentRuleKey       string
	CurrentRuleName      string
	CurrentOwnerLane     string
	CurrentSLAHours      int
	CurrentOwnerMemberID *string
	DraftRuleKey         string
	DraftRuleName        string
	DraftOwnerLane       string
	DraftSLAHours        int
	DraftOwnerMemberID   *string
	Changed              bool
}

type PolicyRevision struct {
	Version   int
	UpdatedAt *time.Time
	UpdatedBy string
	Note      string
	Rules     []PolicyRule
}

type storedPolicyEnvelope struct {
	SchemaVersion int          `json:"schema_version,omitempty"`
	Version       int          `json:"version"`
	UpdatedAt     string       `json:"updated_at,omitempty"`
	UpdatedBy     string       `json:"updated_by,omitempty"`
	Note          string       `json:"note,omitempty"`
	Rules         []PolicyRule `json:"rules"`
}

type storedPolicyHistoryEnvelope struct {
	SchemaVersion int                    `json:"schema_version,omitempty"`
	Revisions     []storedPolicyEnvelope `json:"revisions"`
}

func (s *Service) SetPolicyStore(store PolicyStore) {
	s.policyStore = store
}

func (s *Service) SetAuditLogger(audit *auditlogsvc.Service) {
	s.auditLog = audit
}

func (s *Service) GetPolicy(ctx context.Context, tenantID string) (Policy, error) {
	return s.loadAssignmentPolicy(ctx, tenantID)
}

func (s *Service) ListPolicyRevisions(ctx context.Context, tenantID string) ([]PolicyRevision, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, ErrValidation
	}
	history, err := s.loadPolicyHistory(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if len(history) > 0 {
		return history, nil
	}
	current, err := s.loadAssignmentPolicy(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if current.Version <= 0 {
		return nil, nil
	}
	return []PolicyRevision{policyRevisionFromPolicy(current)}, nil
}

func (s *Service) UpdatePolicy(ctx context.Context, input UpdatePolicyInput) (Policy, error) {
	normalized, err := normalizeUpdatePolicyInput(input)
	if err != nil {
		return Policy{}, err
	}
	if s.policyStore == nil {
		return Policy{}, fmt.Errorf("%w: policy store not configured", ErrValidation)
	}
	if err := s.validatePolicyOwners(ctx, normalized.TenantID, normalized.Rules); err != nil {
		return Policy{}, err
	}
	before, err := s.loadAssignmentPolicy(ctx, normalized.TenantID)
	if err != nil {
		return Policy{}, err
	}
	after := Policy{
		Rules:     normalized.Rules,
		Version:   nextPolicyVersion(before),
		UpdatedAt: ptrext.Of(time.Now().UTC()),
		UpdatedBy: normalized.Actor.ID,
		Note:      normalized.Note,
	}
	payload, err := json.Marshal(storedPolicyEnvelopeFromPolicy(after))
	if err != nil {
		return Policy{}, fmt.Errorf("marshal feedback assignment policy: %w", err)
	}
	history, err := s.nextPolicyHistory(ctx, normalized.TenantID, before, after)
	if err != nil {
		return Policy{}, err
	}
	historyPayload, err := json.Marshal(storedPolicyHistoryEnvelopeFromRevisions(history))
	if err != nil {
		return Policy{}, fmt.Errorf("marshal feedback assignment policy history: %w", err)
	}
	if err := s.savePolicyAndAudit(ctx, normalized, string(payload), string(historyPayload), before, after); err != nil {
		return Policy{}, err
	}
	return after, nil
}

func (s *Service) RestorePolicy(ctx context.Context, input RestorePolicyInput) (Policy, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.Actor.ID = strings.TrimSpace(input.Actor.ID)
	input.Actor.Type = strings.TrimSpace(input.Actor.Type)
	input.Note = strings.TrimSpace(input.Note)
	if input.Actor.Type == "" {
		input.Actor.Type = "admin"
	}
	if input.TenantID == "" || input.Version <= 0 || input.Actor.ID == "" {
		return Policy{}, ErrValidation
	}
	revisions, err := s.ListPolicyRevisions(ctx, input.TenantID)
	if err != nil {
		return Policy{}, err
	}
	for _, revision := range revisions {
		if revision.Version != input.Version {
			continue
		}
		return s.UpdatePolicy(ctx, UpdatePolicyInput{
			TenantID:            input.TenantID,
			Rules:               revision.Rules,
			Note:                restorePolicyNote(input.Note, input.Version),
			Actor:               input.Actor,
			RestoredFromVersion: input.Version,
		})
	}
	return Policy{}, ErrPolicyRevisionNotFound
}

func (s *Service) DryRunPolicy(ctx context.Context, input DryRunPolicyInput) (DryRunPolicyResult, error) {
	normalized, err := normalizeDryRunPolicyInput(input)
	if err != nil {
		return DryRunPolicyResult{}, err
	}
	if err := s.validatePolicyOwners(ctx, normalized.TenantID, normalized.Rules); err != nil {
		return DryRunPolicyResult{}, err
	}
	currentPolicy, err := s.loadAssignmentPolicy(ctx, normalized.TenantID)
	if err != nil {
		return DryRunPolicyResult{}, err
	}
	candidates, err := s.store.AssignmentCandidates(ctx, normalized.TenantID, normalized.FeedbackIDs)
	if err != nil {
		return DryRunPolicyResult{}, err
	}
	draftPolicy := Policy{Rules: normalized.Rules}
	recommendationInput := RecommendationInput{
		TenantID:    normalized.TenantID,
		FeedbackIDs: normalized.FeedbackIDs,
		Now:         normalized.Now,
	}
	current := recommendationResultFromCandidates(recommendationInput, candidates, currentPolicy)
	draft := recommendationResultFromCandidates(recommendationInput, candidates, draftPolicy)
	impacts, changed := policyDryRunImpacts(normalized.FeedbackIDs, current.Recommendations, draft.Recommendations)
	return DryRunPolicyResult{
		TotalMatched:    draft.TotalMatched,
		Changed:         changed,
		Recommendations: draft.Recommendations,
		Failed:          draft.Failed,
		Impacts:         impacts,
	}, nil
}

func (s *Service) savePolicyAndAudit(
	ctx context.Context,
	input UpdatePolicyInput,
	payload string,
	historyPayload string,
	before Policy,
	after Policy,
) error {
	txStore, hasTxStore := s.policyStore.(TransactionalPolicyStore)
	if s.auditLog != nil && hasTxStore && s.pool != nil {
		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin feedback assignment policy tx: %w", err)
		}
		defer tx.Rollback(ctx) //nolint:errcheck

		if err := txStore.SetTx(
			ctx,
			tx,
			input.TenantID,
			assignmentPolicySettingKey,
			payload,
			input.Actor.ID,
		); err != nil {
			return fmt.Errorf("save feedback assignment policy: %w", err)
		}
		if err := txStore.SetTx(
			ctx,
			tx,
			input.TenantID,
			assignmentPolicyHistoryKey,
			historyPayload,
			input.Actor.ID,
		); err != nil {
			return fmt.Errorf("save feedback assignment policy history: %w", err)
		}
		if err := s.recordPolicyAuditTx(ctx, tx, input, before, after); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit feedback assignment policy tx: %w", err)
		}
		return nil
	}

	if err := s.policyStore.Set(
		ctx,
		input.TenantID,
		assignmentPolicySettingKey,
		payload,
		input.Actor.ID,
	); err != nil {
		return fmt.Errorf("save feedback assignment policy: %w", err)
	}
	if err := s.policyStore.Set(
		ctx,
		input.TenantID,
		assignmentPolicyHistoryKey,
		historyPayload,
		input.Actor.ID,
	); err != nil {
		return fmt.Errorf("save feedback assignment policy history: %w", err)
	}
	if err := s.recordPolicyAudit(ctx, input, before, after); err != nil {
		return err
	}
	return nil
}

func (s *Service) loadAssignmentPolicy(ctx context.Context, tenantID string) (Policy, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return Policy{}, ErrValidation
	}
	if s.policyStore == nil {
		return Policy{Rules: defaultAssignmentPolicyRules()}, nil
	}
	raw, err := s.policyStore.Get(ctx, tenantID, assignmentPolicySettingKey)
	if errors.Is(err, systemsettings.ErrNotFound) {
		return Policy{Rules: defaultAssignmentPolicyRules()}, nil
	}
	if err != nil {
		return Policy{}, fmt.Errorf("load feedback assignment policy: %w", err)
	}
	return decodeStoredPolicy(raw)
}

func decodeStoredPolicy(raw string) (Policy, error) {
	var envelope storedPolicyEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return Policy{}, fmt.Errorf("decode feedback assignment policy: %w", err)
	}
	rules, err := mergeAssignmentPolicyRules(envelope.Rules, false)
	if err != nil {
		return Policy{}, err
	}
	version := envelope.Version
	if version <= 0 {
		version = 1
	}
	policy := Policy{
		Rules:     rules,
		Version:   version,
		UpdatedBy: strings.TrimSpace(envelope.UpdatedBy),
		Note:      strings.TrimSpace(envelope.Note),
	}
	if updatedAt, ok := parsePolicyTime(envelope.UpdatedAt); ok {
		policy.UpdatedAt = ptrext.Of(updatedAt)
	}
	return policy, nil
}

func (s *Service) loadPolicyHistory(ctx context.Context, tenantID string) ([]PolicyRevision, error) {
	if s.policyStore == nil {
		return nil, nil
	}
	raw, err := s.policyStore.Get(ctx, tenantID, assignmentPolicyHistoryKey)
	if errors.Is(err, systemsettings.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load feedback assignment policy history: %w", err)
	}
	var envelope storedPolicyHistoryEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return nil, fmt.Errorf("decode feedback assignment policy history: %w", err)
	}
	revisions := make([]PolicyRevision, 0, len(envelope.Revisions))
	for _, stored := range envelope.Revisions {
		policy, err := decodeStoredPolicyEnvelope(stored)
		if err != nil {
			return nil, err
		}
		if policy.Version <= 0 {
			continue
		}
		revisions = append(revisions, policyRevisionFromPolicy(policy))
	}
	sortPolicyRevisions(revisions)
	if len(revisions) > assignmentPolicyHistoryLimit {
		revisions = revisions[:assignmentPolicyHistoryLimit]
	}
	return revisions, nil
}

func decodeStoredPolicyEnvelope(envelope storedPolicyEnvelope) (Policy, error) {
	payload, err := json.Marshal(envelope)
	if err != nil {
		return Policy{}, fmt.Errorf("marshal feedback assignment policy envelope: %w", err)
	}
	return decodeStoredPolicy(string(payload))
}

func (s *Service) nextPolicyHistory(
	ctx context.Context,
	tenantID string,
	before Policy,
	after Policy,
) ([]PolicyRevision, error) {
	revisions, err := s.loadPolicyHistory(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if before.Version > 0 {
		revisions = upsertPolicyRevision(revisions, policyRevisionFromPolicy(before))
	}
	revisions = upsertPolicyRevision(revisions, policyRevisionFromPolicy(after))
	sortPolicyRevisions(revisions)
	if len(revisions) > assignmentPolicyHistoryLimit {
		revisions = revisions[:assignmentPolicyHistoryLimit]
	}
	return revisions, nil
}

func normalizeDryRunPolicyInput(input DryRunPolicyInput) (DryRunPolicyInput, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.FeedbackIDs = uniquePositiveFeedbackIDs(input.FeedbackIDs)
	if input.Now.IsZero() {
		input.Now = time.Now().UTC()
	}
	input.Now = input.Now.UTC()
	if input.TenantID == "" || len(input.FeedbackIDs) == 0 || len(input.Rules) == 0 {
		return DryRunPolicyInput{}, ErrValidation
	}
	if len(input.FeedbackIDs) > MaxBatchSize {
		return DryRunPolicyInput{}, fmt.Errorf("%w: too many feedback ids", ErrValidation)
	}
	rules, err := mergeAssignmentPolicyRules(input.Rules, true)
	if err != nil {
		return DryRunPolicyInput{}, err
	}
	input.Rules = rules
	return input, nil
}

func nextPolicyVersion(before Policy) int {
	if before.Version <= 0 {
		return 1
	}
	return before.Version + 1
}

func restorePolicyNote(note string, version int) string {
	if strings.TrimSpace(note) == "" {
		return fmt.Sprintf("Restored feedback assignment policy version %d", version)
	}
	return strings.TrimSpace(note)
}

func policyRevisionFromPolicy(policy Policy) PolicyRevision {
	return PolicyRevision{
		Version:   policy.Version,
		UpdatedAt: policy.UpdatedAt,
		UpdatedBy: policy.UpdatedBy,
		Note:      policy.Note,
		Rules:     policy.Rules,
	}
}

func upsertPolicyRevision(revisions []PolicyRevision, revision PolicyRevision) []PolicyRevision {
	out := make([]PolicyRevision, 0, len(revisions)+1)
	out = append(out, revision)
	for _, item := range revisions {
		if item.Version == revision.Version {
			continue
		}
		out = append(out, item)
	}
	return out
}

func sortPolicyRevisions(revisions []PolicyRevision) {
	sort.SliceStable(revisions, func(i int, j int) bool {
		return revisions[i].Version > revisions[j].Version
	})
}

func storedPolicyEnvelopeFromPolicy(policy Policy) storedPolicyEnvelope {
	envelope := storedPolicyEnvelope{
		SchemaVersion: 1,
		Version:       policy.Version,
		UpdatedBy:     strings.TrimSpace(policy.UpdatedBy),
		Note:          strings.TrimSpace(policy.Note),
		Rules:         policy.Rules,
	}
	if policy.UpdatedAt != nil {
		envelope.UpdatedAt = policy.UpdatedAt.UTC().Format(time.RFC3339)
	}
	return envelope
}

func storedPolicyHistoryEnvelopeFromRevisions(revisions []PolicyRevision) storedPolicyHistoryEnvelope {
	out := make([]storedPolicyEnvelope, 0, len(revisions))
	for _, revision := range revisions {
		out = append(out, storedPolicyEnvelopeFromPolicy(Policy{
			Rules:     revision.Rules,
			Version:   revision.Version,
			UpdatedAt: revision.UpdatedAt,
			UpdatedBy: revision.UpdatedBy,
			Note:      revision.Note,
		}))
	}
	return storedPolicyHistoryEnvelope{SchemaVersion: 1, Revisions: out}
}

func parsePolicyTime(value string) (time.Time, bool) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func normalizeUpdatePolicyInput(input UpdatePolicyInput) (UpdatePolicyInput, error) {
	input.TenantID = strings.TrimSpace(input.TenantID)
	input.Actor.ID = strings.TrimSpace(input.Actor.ID)
	input.Actor.Type = strings.TrimSpace(input.Actor.Type)
	input.Note = strings.TrimSpace(input.Note)
	if input.Actor.Type == "" {
		input.Actor.Type = "admin"
	}
	if input.TenantID == "" || input.Actor.ID == "" || len(input.Rules) == 0 {
		return UpdatePolicyInput{}, ErrValidation
	}
	if len([]rune(input.Note)) > maxPolicyNoteRunes {
		return UpdatePolicyInput{}, fmt.Errorf("%w: note too long", ErrValidation)
	}
	rules, err := mergeAssignmentPolicyRules(input.Rules, true)
	if err != nil {
		return UpdatePolicyInput{}, err
	}
	input.Rules = rules
	return input, nil
}

func mergeAssignmentPolicyRules(input []PolicyRule, strict bool) ([]PolicyRule, error) {
	defaults := defaultAssignmentPolicyRules()
	merged := make(map[string]PolicyRule, len(defaults))
	for _, rule := range defaults {
		merged[rule.RuleKey] = rule
	}
	for _, item := range input {
		key := strings.TrimSpace(item.RuleKey)
		current, ok := merged[key]
		if !ok {
			if strict {
				return nil, fmt.Errorf("%w: unknown policy rule", ErrValidation)
			}
			continue
		}
		updated, err := overlayPolicyRule(current, item, strict)
		if err != nil {
			return nil, err
		}
		merged[key] = updated
	}
	out := make([]PolicyRule, 0, len(defaults))
	for _, rule := range defaults {
		out = append(out, merged[rule.RuleKey])
	}
	return out, nil
}

func overlayPolicyRule(base PolicyRule, input PolicyRule, strict bool) (PolicyRule, error) {
	out := base
	out.Enabled = input.Enabled
	ownerLane := strings.TrimSpace(input.OwnerLane)
	if ownerLane == "" && !strict {
		ownerLane = base.OwnerLane
	}
	if !ownerLanePattern.MatchString(ownerLane) {
		return PolicyRule{}, fmt.Errorf("%w: invalid owner lane", ErrValidation)
	}
	out.OwnerLane = ownerLane
	slaHours := input.SLAHours
	if slaHours == 0 && !strict {
		slaHours = base.SLAHours
	}
	if slaHours < 1 || slaHours > maxAssignmentSLAHours {
		return PolicyRule{}, fmt.Errorf("%w: invalid SLA hours", ErrValidation)
	}
	out.SLAHours = slaHours
	out.DefaultOwnerMemberID = normalizePolicyOwner(input.DefaultOwnerMemberID)
	return out, nil
}

func normalizePolicyOwner(value *string) *string {
	trimmed := strings.TrimSpace(ptrext.IndirectOr(value, ""))
	if trimmed == "" {
		return nil
	}
	return ptrext.Of(trimmed)
}

func (s *Service) validatePolicyOwners(ctx context.Context, tenantID string, rules []PolicyRule) error {
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		if rule.DefaultOwnerMemberID == nil {
			continue
		}
		ownerID := ptrext.Indirect(rule.DefaultOwnerMemberID)
		if _, ok := seen[ownerID]; ok {
			continue
		}
		seen[ownerID] = struct{}{}
		if err := s.store.ValidateAssignmentOwner(ctx, tenantID, ownerID); err != nil {
			if errors.Is(err, feedbackrepo.ErrAssignmentOwnerNotFound) {
				return ErrOwnerNotFound
			}
			return err
		}
	}
	return nil
}

func (s *Service) recordPolicyAudit(ctx context.Context, input UpdatePolicyInput, before Policy, after Policy) error {
	if s.auditLog == nil {
		return nil
	}
	return s.auditLog.Record(ctx, policyAuditEvent(input, before, after))
}

func (s *Service) recordPolicyAuditTx(
	ctx context.Context,
	tx pgx.Tx,
	input UpdatePolicyInput,
	before Policy,
	after Policy,
) error {
	if s.auditLog == nil {
		return nil
	}
	return s.auditLog.RecordTx(ctx, tx, policyAuditEvent(input, before, after))
}

func policyAuditEvent(input UpdatePolicyInput, before Policy, after Policy) auditlogsvc.Event {
	afterFields := policyAuditFields(after)
	if input.RestoredFromVersion > 0 {
		afterFields["restored_from_version"] = input.RestoredFromVersion
	}
	return auditlogsvc.Event{
		TenantID:   input.TenantID,
		Actor:      input.Actor,
		Action:     policyAuditAction(input),
		TargetType: "feedback_assignment_policy",
		TargetID:   input.TenantID,
		Summary:    policyAuditSummary(input),
		Before:     policyAuditFields(before),
		After:      afterFields,
	}
}

func policyAuditAction(input UpdatePolicyInput) string {
	if input.RestoredFromVersion > 0 {
		return "feedback_assignment.policy_restore"
	}
	return "feedback_assignment.policy_update"
}

func policyAuditSummary(input UpdatePolicyInput) string {
	note := strings.TrimSpace(input.Note)
	if input.RestoredFromVersion > 0 {
		base := fmt.Sprintf("Restored feedback assignment policy version %d", input.RestoredFromVersion)
		if note == "" || note == base {
			return base
		}
		return base + ": " + note
	}
	if note == "" {
		return "Updated feedback assignment policy"
	}
	return "Updated feedback assignment policy: " + note
}

func policyAuditFields(policy Policy) map[string]any {
	rules := make([]map[string]any, 0, len(policy.Rules))
	for _, rule := range policy.Rules {
		item := map[string]any{
			"rule_key":    rule.RuleKey,
			"owner_lane":  rule.OwnerLane,
			"severity":    rule.Severity,
			"sla_hours":   rule.SLAHours,
			"enabled":     rule.Enabled,
			"rule_name":   rule.RuleName,
			"has_default": rule.DefaultOwnerMemberID != nil,
		}
		if rule.DefaultOwnerMemberID != nil {
			item["default_owner_member_id"] = ptrext.Indirect(rule.DefaultOwnerMemberID)
		}
		rules = append(rules, item)
	}
	return map[string]any{"rules": rules}
}

func assignmentRulesFromPolicy(policy Policy) map[string]assignmentRule {
	out := make(map[string]assignmentRule, len(policy.Rules))
	for _, rule := range policy.Rules {
		if !rule.Enabled {
			continue
		}
		out[rule.RuleKey] = assignmentRule{
			Key:                  rule.RuleKey,
			Name:                 rule.RuleName,
			OwnerLane:            rule.OwnerLane,
			Severity:             rule.Severity,
			SLAHours:             rule.SLAHours,
			Rationale:            rule.Rationale,
			DefaultOwnerMemberID: rule.DefaultOwnerMemberID,
		}
	}
	return out
}

func defaultAssignmentPolicyRules() []PolicyRule {
	rules := defaultAssignmentRules()
	out := make([]PolicyRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, policyRuleFromAssignmentRule(rule))
	}
	return out
}

func policyRuleFromAssignmentRule(rule assignmentRule) PolicyRule {
	return PolicyRule{
		RuleKey:              rule.Key,
		RuleName:             rule.Name,
		OwnerLane:            rule.OwnerLane,
		Severity:             rule.Severity,
		SLAHours:             rule.SLAHours,
		DefaultOwnerMemberID: rule.DefaultOwnerMemberID,
		Enabled:              true,
		Rationale:            rule.Rationale,
	}
}
