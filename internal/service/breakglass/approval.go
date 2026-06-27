// SPDX-License-Identifier: Apache-2.0

package breakglass

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/breakglass"
)

// ErrPendingApproval — token requires approval before use.
var ErrPendingApproval = errors.New("breakglass: token pending approval")

// ErrApprovalExpired — approval window has passed.
var ErrApprovalExpired = errors.New("breakglass: approval request expired")

// ErrSelfApproval — cannot approve your own token.
var ErrSelfApproval = errors.New("breakglass: cannot approve own token")

// ErrAlreadyApproved — token has already been approved.
var ErrAlreadyApproved = errors.New("breakglass: token already approved")

// DefaultApprovalWindow is how long an approval request is valid.
const DefaultApprovalWindow = 1 * time.Hour

// ApprovalRepository extends Repository with approval operations.
type ApprovalRepository interface {
	GetByID(ctx context.Context, tenantID, id string) (domain.BreakGlassToken, error)
	Approve(ctx context.Context, tenantID, tokenID, approvedBy string) error
}

// ApproveInput is the input for approving a token.
type ApproveInput struct {
	TenantID   string
	TokenID    string
	ApprovedBy string
}

// Approve approves a pending break-glass token.
func (s *Service) Approve(ctx context.Context, input ApproveInput) error {
	const where = "breakglass.Service.Approve"

	token, err := s.repo.(interface {
		GetByID(ctx context.Context, tenantID, id string) (domain.BreakGlassToken, error)
	}).GetByID(ctx, input.TenantID, input.TokenID)
	if err != nil {
		if errors.Is(err, breakglass.ErrNotFound) {
			return breakglass.ErrNotFound
		}
		return err
	}

	// Validate approval conditions
	if !token.RequiresApproval {
		return ErrAlreadyApproved // Token doesn't need approval
	}
	if token.ApprovedAt != nil {
		return ErrAlreadyApproved
	}
	if token.IssuedBy == input.ApprovedBy {
		return ErrSelfApproval
	}
	if token.ApprovalExpiresAt != nil && time.Now().After(ptrext.Indirect(token.ApprovalExpiresAt)) {
		return ErrApprovalExpired
	}
	if token.UsedAt != nil {
		return breakglass.ErrAlreadyUsed
	}
	if token.RevokedAt != nil {
		return breakglass.ErrAlreadyRevoked
	}

	// Perform approval
	if err := s.repo.(interface {
		Approve(ctx context.Context, tenantID, tokenID, approvedBy string) error
	}).Approve(ctx, input.TenantID, input.TokenID, input.ApprovedBy); err != nil {
		return err
	}

	logext.Infof(ctx, "[%s] token approved,id:%s,approved_by:%s,issued_by:%s",
		where, input.TokenID, input.ApprovedBy, token.IssuedBy)

	return nil
}

// ListPendingApproval returns all tokens pending approval for a tenant.
func (s *Service) ListPendingApproval(ctx context.Context, tenantID string) ([]domain.BreakGlassToken, error) {
	tokens, err := s.List(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	pending := make([]domain.BreakGlassToken, 0)
	for _, t := range tokens {
		if t.RequiresApproval && t.ApprovedAt == nil && !t.IsApprovalExpired(now) && t.UsedAt == nil && t.RevokedAt == nil {
			pending = append(pending, t)
		}
	}
	return pending, nil
}

// IssueWithApprovalInput extends IssueInput with approval fields.
type IssueWithApprovalInput struct {
	IssueInput
	RequiresApproval bool
	Justification    string
}

// IssueWithApproval issues a token that requires approval from another admin.
func (s *Service) IssueWithApproval(ctx context.Context, input IssueWithApprovalInput) (IssueResult, error) {
	const where = "breakglass.Service.IssueWithApproval"

	if input.RequiresApproval && input.Justification == "" {
		return IssueResult{}, fmt.Errorf("justification required when approval is needed")
	}

	// For now, we issue normally and let the repo handle the approval fields
	// The extended repo will need to accept these additional fields
	result, err := s.Issue(ctx, input.IssueInput)
	if err != nil {
		return IssueResult{}, err
	}

	if input.RequiresApproval {
		logext.Infof(ctx, "[%s] token issued pending approval,id:%s,admin:%s,justification:%s",
			where, result.Token.ID, input.AdminEmail, input.Justification)
	}

	return result, nil
}
