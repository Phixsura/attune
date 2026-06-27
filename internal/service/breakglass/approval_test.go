// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test fixtures with inline struct pointers

package breakglass

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/breakglass"
)

// approvalStubRepo extends stubRepo with approval operations.
type approvalStubRepo struct {
	stubRepo

	getByIDToken domain.BreakGlassToken
	getByIDErr   error

	approveErr    error
	approveCalled bool
	approveID     string
	approveBy     string
}

func (s *approvalStubRepo) GetByID(_ context.Context, _, id string) (domain.BreakGlassToken, error) {
	return s.getByIDToken, s.getByIDErr
}

func (s *approvalStubRepo) Approve(_ context.Context, _, tokenID, approvedBy string) error {
	s.approveCalled = true
	s.approveID = tokenID
	s.approveBy = approvedBy
	return s.approveErr
}

func TestApprove_Success(t *testing.T) {
	t.Parallel()

	now := time.Now()
	repo := &approvalStubRepo{
		getByIDToken: domain.BreakGlassToken{
			ID:                "token-1",
			TenantID:          "tenant-1",
			AdminEmail:        "admin@example.com",
			IssuedBy:          "issuer@example.com",
			RequiresApproval:  true,
			ApprovalExpiresAt: ptrext.Of(now.Add(1 * time.Hour)),
		},
	}
	svc := NewService(repo, newTestConfig())

	err := svc.Approve(context.Background(), ApproveInput{
		TenantID:   "tenant-1",
		TokenID:    "token-1",
		ApprovedBy: "approver@example.com",
	})
	if err != nil {
		t.Fatalf("Approve() err = %v, want nil", err)
	}
	if !repo.approveCalled {
		t.Error("expected repo.Approve to be called")
	}
	if repo.approveID != "token-1" {
		t.Errorf("approveID = %s, want token-1", repo.approveID)
	}
	if repo.approveBy != "approver@example.com" {
		t.Errorf("approveBy = %s, want approver@example.com", repo.approveBy)
	}
}

func TestApprove_NotFound(t *testing.T) {
	t.Parallel()

	repo := &approvalStubRepo{
		getByIDErr: breakglass.ErrNotFound,
	}
	svc := NewService(repo, newTestConfig())

	err := svc.Approve(context.Background(), ApproveInput{
		TenantID:   "tenant-1",
		TokenID:    "nonexistent",
		ApprovedBy: "approver@example.com",
	})
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Errorf("Approve() err = %v, want ErrNotFound", err)
	}
}

func TestApprove_DoesNotRequireApproval(t *testing.T) {
	t.Parallel()

	repo := &approvalStubRepo{
		getByIDToken: domain.BreakGlassToken{
			ID:               "token-1",
			RequiresApproval: false,
		},
	}
	svc := NewService(repo, newTestConfig())

	err := svc.Approve(context.Background(), ApproveInput{
		TenantID:   "tenant-1",
		TokenID:    "token-1",
		ApprovedBy: "approver@example.com",
	})
	if !errors.Is(err, ErrAlreadyApproved) {
		t.Errorf("Approve() err = %v, want ErrAlreadyApproved", err)
	}
}

func TestApprove_AlreadyApproved(t *testing.T) {
	t.Parallel()

	now := time.Now()
	repo := &approvalStubRepo{
		getByIDToken: domain.BreakGlassToken{
			ID:               "token-1",
			RequiresApproval: true,
			ApprovedAt:       ptrext.Of(now),
		},
	}
	svc := NewService(repo, newTestConfig())

	err := svc.Approve(context.Background(), ApproveInput{
		TenantID:   "tenant-1",
		TokenID:    "token-1",
		ApprovedBy: "approver@example.com",
	})
	if !errors.Is(err, ErrAlreadyApproved) {
		t.Errorf("Approve() err = %v, want ErrAlreadyApproved", err)
	}
}

func TestApprove_SelfApproval(t *testing.T) {
	t.Parallel()

	now := time.Now()
	repo := &approvalStubRepo{
		getByIDToken: domain.BreakGlassToken{
			ID:                "token-1",
			IssuedBy:          "admin@example.com",
			RequiresApproval:  true,
			ApprovalExpiresAt: ptrext.Of(now.Add(1 * time.Hour)),
		},
	}
	svc := NewService(repo, newTestConfig())

	err := svc.Approve(context.Background(), ApproveInput{
		TenantID:   "tenant-1",
		TokenID:    "token-1",
		ApprovedBy: "admin@example.com", // same as issuer
	})
	if !errors.Is(err, ErrSelfApproval) {
		t.Errorf("Approve() err = %v, want ErrSelfApproval", err)
	}
}

func TestApprove_ApprovalExpired(t *testing.T) {
	t.Parallel()

	repo := &approvalStubRepo{
		getByIDToken: domain.BreakGlassToken{
			ID:                "token-1",
			IssuedBy:          "issuer@example.com",
			RequiresApproval:  true,
			ApprovalExpiresAt: ptrext.Of(time.Now().Add(-1 * time.Hour)), // expired
		},
	}
	svc := NewService(repo, newTestConfig())

	err := svc.Approve(context.Background(), ApproveInput{
		TenantID:   "tenant-1",
		TokenID:    "token-1",
		ApprovedBy: "approver@example.com",
	})
	if !errors.Is(err, ErrApprovalExpired) {
		t.Errorf("Approve() err = %v, want ErrApprovalExpired", err)
	}
}

func TestApprove_TokenAlreadyUsed(t *testing.T) {
	t.Parallel()

	now := time.Now()
	repo := &approvalStubRepo{
		getByIDToken: domain.BreakGlassToken{
			ID:                "token-1",
			IssuedBy:          "issuer@example.com",
			RequiresApproval:  true,
			ApprovalExpiresAt: ptrext.Of(now.Add(1 * time.Hour)),
			UsedAt:            ptrext.Of(now),
		},
	}
	svc := NewService(repo, newTestConfig())

	err := svc.Approve(context.Background(), ApproveInput{
		TenantID:   "tenant-1",
		TokenID:    "token-1",
		ApprovedBy: "approver@example.com",
	})
	if !errors.Is(err, breakglass.ErrAlreadyUsed) {
		t.Errorf("Approve() err = %v, want ErrAlreadyUsed", err)
	}
}

func TestApprove_TokenRevoked(t *testing.T) {
	t.Parallel()

	now := time.Now()
	repo := &approvalStubRepo{
		getByIDToken: domain.BreakGlassToken{
			ID:                "token-1",
			IssuedBy:          "issuer@example.com",
			RequiresApproval:  true,
			ApprovalExpiresAt: ptrext.Of(now.Add(1 * time.Hour)),
			RevokedAt:         ptrext.Of(now),
		},
	}
	svc := NewService(repo, newTestConfig())

	err := svc.Approve(context.Background(), ApproveInput{
		TenantID:   "tenant-1",
		TokenID:    "token-1",
		ApprovedBy: "approver@example.com",
	})
	if !errors.Is(err, breakglass.ErrAlreadyRevoked) {
		t.Errorf("Approve() err = %v, want ErrAlreadyRevoked", err)
	}
}

func TestListPendingApproval_Empty(t *testing.T) {
	t.Parallel()

	repo := &approvalStubRepo{
		stubRepo: stubRepo{
			listAllTokens: []domain.BreakGlassToken{},
		},
	}
	svc := NewService(repo, newTestConfig())

	tokens, err := svc.ListPendingApproval(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ListPendingApproval() err = %v, want nil", err)
	}
	if len(tokens) != 0 {
		t.Errorf("len(tokens) = %d, want 0", len(tokens))
	}
}

func TestListPendingApproval_FiltersPending(t *testing.T) {
	t.Parallel()

	now := time.Now()
	repo := &approvalStubRepo{
		stubRepo: stubRepo{
			listAllTokens: []domain.BreakGlassToken{
				{ID: "pending-1", RequiresApproval: true, ApprovalExpiresAt: ptrext.Of(now.Add(1 * time.Hour))},
				{ID: "approved-1", RequiresApproval: true, ApprovedAt: ptrext.Of(now)},
				{ID: "no-approval", RequiresApproval: false},
				{ID: "expired-approval", RequiresApproval: true, ApprovalExpiresAt: ptrext.Of(now.Add(-1 * time.Hour))},
				{ID: "used", RequiresApproval: true, ApprovalExpiresAt: ptrext.Of(now.Add(1 * time.Hour)), UsedAt: ptrext.Of(now)},
			},
		},
	}
	svc := NewService(repo, newTestConfig())

	tokens, err := svc.ListPendingApproval(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ListPendingApproval() err = %v, want nil", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("len(tokens) = %d, want 1", len(tokens))
	}
	if tokens[0].ID != "pending-1" {
		t.Errorf("tokens[0].ID = %s, want pending-1", tokens[0].ID)
	}
}

func TestIssueWithApproval_RequiresJustification(t *testing.T) {
	t.Parallel()

	repo := &approvalStubRepo{}
	svc := NewService(repo, newTestConfig())

	_, err := svc.IssueWithApproval(context.Background(), IssueWithApprovalInput{
		IssueInput: IssueInput{
			TenantID:   "tenant-1",
			AdminEmail: "admin@example.com",
			TTL:        5 * time.Minute,
			IssuedBy:   "issuer@example.com",
		},
		RequiresApproval: true,
		Justification:    "", // empty
	})
	if err == nil {
		t.Error("IssueWithApproval() err = nil, want justification required error")
	}
}

func TestIssueWithApproval_Success(t *testing.T) {
	t.Parallel()

	repo := &approvalStubRepo{
		stubRepo: stubRepo{
			issueToken: domain.BreakGlassToken{
				ID:         "token-1",
				TenantID:   "tenant-1",
				AdminEmail: "admin@example.com",
			},
		},
	}
	svc := NewService(repo, newTestConfig())

	result, err := svc.IssueWithApproval(context.Background(), IssueWithApprovalInput{
		IssueInput: IssueInput{
			TenantID:   "tenant-1",
			AdminEmail: "admin@example.com",
			TTL:        5 * time.Minute,
			IssuedBy:   "issuer@example.com",
		},
		RequiresApproval: true,
		Justification:    "Emergency access needed",
	})
	if err != nil {
		t.Fatalf("IssueWithApproval() err = %v, want nil", err)
	}
	if result.Token.ID != "token-1" {
		t.Errorf("Token.ID = %s, want token-1", result.Token.ID)
	}
}
