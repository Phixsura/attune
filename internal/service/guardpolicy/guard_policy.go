// Package guardpolicy owns Console-facing guard policy validation.
package guardpolicy

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/llmclient"
	"github.com/Phixsura/attune/internal/infra/llmguard"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
)

const (
	MaxPolicies         = 50
	MaxRulesPerPolicy   = 50
	MaxPolicyNameBytes  = 120
	MaxTargetValues     = 50
	MaxTargetValueBytes = 120
	MaxReplacementBytes = 64
	MaxPriority         = 10000
)

var (
	ErrPolicyNameRequired = errors.New("guard policy name is required")
	ErrPolicyNameTooLong  = errors.New("guard policy name is too long")
	ErrPolicyLimit        = errors.New("too many guard policies")
	ErrRuleLimit          = errors.New("too many guard policy rules")
	ErrKindInvalid        = errors.New("guard policy kind is invalid")
	ErrGuardInvalid       = errors.New("guard name is invalid")
	ErrStageInvalid       = errors.New("guard stage is invalid")
	ErrActionInvalid      = errors.New("guard action is invalid")
	ErrEntityInvalid      = errors.New("guard entity is invalid")
	ErrTargetInvalid      = errors.New("guard policy target is invalid")
	ErrReplacementInvalid = errors.New("guard policy replacement is invalid")
	ErrPriorityInvalid    = errors.New("guard policy priority is invalid")
)

var (
	safeTokenRE       = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)
	safeReplacementRE = regexp.MustCompile(`^[A-Za-z0-9_{}:<>.-]+$`)
)

type Store interface {
	ListForConsole(ctx context.Context, tenantID string) ([]llmguard.Policy, error)
	ReplaceTenantPolicies(ctx context.Context, tenantID, actor string, policies []llmguard.Policy) error
	CreateTenantPolicy(ctx context.Context, tenantID, actor string, policy llmguard.Policy) (llmguard.Policy, error)
	UpdateTenantPolicy(ctx context.Context, tenantID, actor, id string, policy llmguard.Policy) (llmguard.Policy, error)
	DeleteTenantPolicy(ctx context.Context, tenantID, id string) error
	Resolve(ctx context.Context, meta llmclient.GuardMetadata) (llmguard.Plan, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return ptrext.Of(Service{store: store})
}

func (s *Service) List(ctx context.Context, tenantID string) ([]llmguard.Policy, error) {
	return s.store.ListForConsole(ctx, tenantID)
}

func (s *Service) ReplaceTenantPolicies(
	ctx context.Context, tenantID, actor string, policies []llmguard.Policy,
) ([]llmguard.Policy, error) {
	if err := validatePolicies(policies); err != nil {
		return nil, err
	}
	normalized := normalizeTenantPolicies(policies)
	if err := s.store.ReplaceTenantPolicies(ctx, tenantID, actor, normalized); err != nil {
		return nil, err
	}
	return s.store.ListForConsole(ctx, tenantID)
}

func (s *Service) CreatePolicy(
	ctx context.Context, tenantID, actor string, policy llmguard.Policy,
) (llmguard.Policy, error) {
	if err := validatePolicy(policy); err != nil {
		return llmguard.Policy{}, err
	}
	normalized := normalizeTenantPolicy(policy)
	return s.store.CreateTenantPolicy(ctx, tenantID, actor, normalized)
}

func (s *Service) UpdatePolicy(
	ctx context.Context, tenantID, actor, id string, policy llmguard.Policy,
) (llmguard.Policy, error) {
	if err := validatePolicy(policy); err != nil {
		return llmguard.Policy{}, err
	}
	normalized := normalizeTenantPolicy(policy)
	normalized.ID = id
	return s.store.UpdateTenantPolicy(ctx, tenantID, actor, id, normalized)
}

func (s *Service) DeletePolicy(ctx context.Context, tenantID, id string) error {
	return s.store.DeleteTenantPolicy(ctx, tenantID, id)
}

func (s *Service) Resolve(ctx context.Context, meta llmclient.GuardMetadata) (llmguard.Plan, error) {
	if meta.Purpose == "" {
		meta.Purpose = "enrich"
	}
	return s.store.Resolve(ctx, meta)
}

func validatePolicies(policies []llmguard.Policy) error {
	if len(policies) > MaxPolicies {
		return ErrPolicyLimit
	}
	for _, p := range policies {
		if err := validatePolicy(p); err != nil {
			return err
		}
	}
	return nil
}

func validatePolicy(p llmguard.Policy) error {
	name := strings.TrimSpace(p.Name)
	if name == "" {
		return ErrPolicyNameRequired
	}
	if len(name) > MaxPolicyNameBytes {
		return ErrPolicyNameTooLong
	}
	if !validKind(p.Kind) {
		return ErrKindInvalid
	}
	if p.Priority < 0 || p.Priority > MaxPriority {
		return ErrPriorityInvalid
	}
	if err := validateTarget(p.Target); err != nil {
		return err
	}
	if len(p.Rules) > MaxRulesPerPolicy {
		return ErrRuleLimit
	}
	for _, r := range p.Rules {
		if err := validateRule(r); err != nil {
			return err
		}
	}
	return nil
}

func validateRule(r llmguard.Rule) error {
	if r.Guard != "pii" {
		return ErrGuardInvalid
	}
	if !validStage(r.Stage) {
		return ErrStageInvalid
	}
	if !validAction(r.Action) {
		return ErrActionInvalid
	}
	for _, entity := range r.Entities {
		if !validPIIEntity(entity) {
			return ErrEntityInvalid
		}
	}
	if r.Replacement != "" && !validReplacement(r.Replacement) {
		return ErrReplacementInvalid
	}
	return nil
}

func normalizeTenantPolicies(policies []llmguard.Policy) []llmguard.Policy {
	out := make([]llmguard.Policy, 0, len(policies))
	for _, p := range policies {
		out = append(out, normalizeTenantPolicy(p))
	}
	return out
}

func normalizeTenantPolicy(p llmguard.Policy) llmguard.Policy {
	p.Name = strings.TrimSpace(p.Name)
	p.TenantID = ""
	p.Target = normalizeTarget(p.Target)
	if p.Priority == 0 {
		p.Priority = 100
	}
	return p
}

func validateTarget(t llmguard.Target) error {
	if t.TenantID != "" {
		return ErrTargetInvalid
	}
	if !validValueList(t.Channels, domain.ValidSources) {
		return ErrTargetInvalid
	}
	if !validUUIDList(t.SourceIDs) ||
		!validTokenList(t.SourceTags) ||
		!validTokenList(t.Environments) ||
		!validPurposeList(t.Purposes) ||
		!validStageList(t.Stages) {
		return ErrTargetInvalid
	}
	return nil
}

func normalizeTarget(t llmguard.Target) llmguard.Target {
	t.TenantID = ""
	t.Channels = trimStringSlice(t.Channels)
	t.SourceIDs = trimStringSlice(t.SourceIDs)
	t.SourceTags = trimStringSlice(t.SourceTags)
	t.Purposes = trimStringSlice(t.Purposes)
	t.Environments = trimStringSlice(t.Environments)
	return t
}

func validKind(k llmguard.PolicyKind) bool {
	switch k {
	case llmguard.KindBaseline, llmguard.KindDefault, llmguard.KindOverride:
		return true
	default:
		return false
	}
}

func validStage(s llmguard.Stage) bool {
	switch s {
	case llmguard.StageLLMInput, llmguard.StageLLMOutput, llmguard.StageOutbound, llmguard.StageToolCall:
		return true
	default:
		return false
	}
}

func validAction(a llmguard.Action) bool {
	switch a {
	case llmguard.ActionOff, llmguard.ActionAudit, llmguard.ActionRedact, llmguard.ActionBlock:
		return true
	default:
		return false
	}
}

func validValueList(values []string, allowed map[string]bool) bool {
	if len(values) > MaxTargetValues {
		return false
	}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || len(value) > MaxTargetValueBytes || !allowed[value] {
			return false
		}
	}
	return true
}

func validUUIDList(values []string) bool {
	if len(values) > MaxTargetValues {
		return false
	}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || len(value) > MaxTargetValueBytes {
			return false
		}
		if _, err := uuid.Parse(value); err != nil {
			return false
		}
	}
	return true
}

func validTokenList(values []string) bool {
	if len(values) > MaxTargetValues {
		return false
	}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || len(value) > MaxTargetValueBytes || !safeTokenRE.MatchString(value) {
			return false
		}
	}
	return true
}

func validPurposeList(values []string) bool {
	if len(values) > MaxTargetValues {
		return false
	}
	for _, raw := range values {
		switch strings.TrimSpace(raw) {
		case "enrich", "digest", "reply_draft", "eval", "outbound":
		default:
			return false
		}
	}
	return true
}

func validStageList(values []llmguard.Stage) bool {
	if len(values) > MaxTargetValues {
		return false
	}
	for _, value := range values {
		if !validStage(value) {
			return false
		}
	}
	return true
}

func validReplacement(replacement string) bool {
	return len(replacement) <= MaxReplacementBytes && safeReplacementRE.MatchString(replacement)
}

func validPIIEntity(entity string) bool {
	switch entity {
	case "email", "phone", "cn_mobile", "cn_id", "credit_card":
		return true
	default:
		return false
	}
}

func ErrToCode(err error) attunev1.ErrorCode {
	switch {
	case errors.Is(err, ErrPolicyNameRequired),
		errors.Is(err, ErrPolicyNameTooLong),
		errors.Is(err, ErrPolicyLimit),
		errors.Is(err, ErrRuleLimit),
		errors.Is(err, ErrKindInvalid),
		errors.Is(err, ErrGuardInvalid),
		errors.Is(err, ErrStageInvalid),
		errors.Is(err, ErrActionInvalid),
		errors.Is(err, ErrEntityInvalid),
		errors.Is(err, ErrTargetInvalid),
		errors.Is(err, ErrReplacementInvalid),
		errors.Is(err, ErrPriorityInvalid):
		return attunev1.ErrorCode_VALIDATION
	default:
		return attunev1.ErrorCode_ERROR_CODE_UNSPECIFIED
	}
}

func ErrToMessage(err error) string {
	switch {
	case errors.Is(err, ErrPolicyNameRequired):
		return "guard policy name is required"
	case errors.Is(err, ErrPolicyNameTooLong):
		return fmt.Sprintf("guard policy name must be at most %d bytes", MaxPolicyNameBytes)
	case errors.Is(err, ErrPolicyLimit):
		return fmt.Sprintf("at most %d guard policies are allowed", MaxPolicies)
	case errors.Is(err, ErrRuleLimit):
		return fmt.Sprintf("at most %d rules are allowed per guard policy", MaxRulesPerPolicy)
	case errors.Is(err, ErrKindInvalid):
		return "guard policy kind must be baseline, default, or override"
	case errors.Is(err, ErrGuardInvalid):
		return "guard policy guard must be pii"
	case errors.Is(err, ErrStageInvalid):
		return "guard policy stage is invalid"
	case errors.Is(err, ErrActionInvalid):
		return "guard policy action must be off, audit, redact, or block"
	case errors.Is(err, ErrEntityInvalid):
		return "guard policy entity is invalid"
	case errors.Is(err, ErrTargetInvalid):
		return "guard policy target is invalid"
	case errors.Is(err, ErrReplacementInvalid):
		return fmt.Sprintf("guard policy replacement must be at most %d bytes and use safe template characters", MaxReplacementBytes)
	case errors.Is(err, ErrPriorityInvalid):
		return fmt.Sprintf("guard policy priority must be between 0 and %d", MaxPriority)
	default:
		return ""
	}
}

func trimStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, value := range in {
		out = append(out, strings.TrimSpace(value))
	}
	return out
}
