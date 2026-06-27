// SPDX-License-Identifier: Apache-2.0

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

// stubRepo is a test double for the Repository interface.
type stubRepo struct {
	issueToken  domain.BreakGlassToken
	issueErr    error
	issueCalled bool
	issueInput  breakglass.NewToken

	listValidTokens []domain.BreakGlassToken
	listValidErr    error

	listAllTokens []domain.BreakGlassToken
	listAllErr    error

	markUsedErr      error
	markUsedCalled   bool
	markUsedID       string
	markUsedClientIP string

	revokeErr    error
	revokeCalled bool
	revokeID     string
	revokeBy     string

	revokeAllResult int64
	revokeAllErr    error

	countValidResult int
	countValidErr    error

	cleanupResult int64
	cleanupErr    error
}

func (s *stubRepo) Issue(_ context.Context, n breakglass.NewToken) (domain.BreakGlassToken, error) {
	s.issueCalled = true
	s.issueInput = n
	return s.issueToken, s.issueErr
}

func (s *stubRepo) ListValidForAdmin(_ context.Context, _, _ string) ([]domain.BreakGlassToken, error) {
	return s.listValidTokens, s.listValidErr
}

func (s *stubRepo) ListAll(_ context.Context, _ string, _ int) ([]domain.BreakGlassToken, error) {
	return s.listAllTokens, s.listAllErr
}

func (s *stubRepo) MarkUsed(_ context.Context, _ string, id, clientIP string) error {
	s.markUsedCalled = true
	s.markUsedID = id
	s.markUsedClientIP = clientIP
	return s.markUsedErr
}

func (s *stubRepo) Revoke(_ context.Context, _ string, id, revokedBy string) error {
	s.revokeCalled = true
	s.revokeID = id
	s.revokeBy = revokedBy
	return s.revokeErr
}

func (s *stubRepo) RevokeAll(_ context.Context, _, _ string) (int64, error) {
	return s.revokeAllResult, s.revokeAllErr
}

func (s *stubRepo) CountValid(_ context.Context, _ string) (int, error) {
	return s.countValidResult, s.countValidErr
}

func (s *stubRepo) Cleanup(_ context.Context, _ time.Time) (int64, error) {
	return s.cleanupResult, s.cleanupErr
}

// newTestConfig returns a config suitable for testing with shorter TTLs.
func newTestConfig() Config {
	return Config{
		DefaultTTL: 10 * time.Minute,
		MaxTTL:     1 * time.Hour,
		MinTTL:     1 * time.Minute,
	}
}

// --- Issue tests ---

func TestIssue_Success(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{
		issueToken: domain.BreakGlassToken{
			ID:         "token-1",
			TenantID:   "tenant-1",
			AdminEmail: "admin@example.com",
			IssuedBy:   "issuer@example.com",
			IssuedAt:   time.Now(),
		},
	})
	svc := NewService(repo, newTestConfig())

	result, err := svc.Issue(context.Background(), IssueInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		TTL:        5 * time.Minute,
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue() err = %v, want nil", err)
	}
	if !repo.issueCalled {
		t.Fatal("expected repo.Issue to be called")
	}
	if result.Token.ID != "token-1" {
		t.Errorf("Token.ID = %s, want token-1", result.Token.ID)
	}
	if result.RawToken == "" {
		t.Error("RawToken is empty, expected a generated token")
	}
	if !hasPrefix(result.RawToken, TokenPrefix) {
		t.Errorf("RawToken = %s, expected prefix %s", result.RawToken, TokenPrefix)
	}
	if result.ExpiresAt.IsZero() {
		t.Error("ExpiresAt is zero")
	}
	// Verify the token hash was passed to the repo
	if repo.issueInput.TokenHash == "" {
		t.Error("TokenHash was not set in repo input")
	}
}

func TestIssue_DefaultTTL(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()
	repo := ptrext.Of(stubRepo{issueToken: domain.BreakGlassToken{ID: "token-1"}})
	svc := NewService(repo, cfg)

	result, err := svc.Issue(context.Background(), IssueInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		TTL:        0, // should use default
		IssuedBy:   "issuer@example.com",
	})
	if err != nil {
		t.Fatalf("Issue() err = %v, want nil", err)
	}
	// The expires_at should be approximately DefaultTTL from now
	expectedExpiry := time.Now().Add(cfg.DefaultTTL)
	diff := result.ExpiresAt.Sub(expectedExpiry)
	if diff < -2*time.Second || diff > 2*time.Second {
		t.Errorf("ExpiresAt = %v, expected near %v (diff: %v)", result.ExpiresAt, expectedExpiry, diff)
	}
}

func TestIssue_TTLBelowMinimum(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()
	repo := ptrext.Of(stubRepo{})
	svc := NewService(repo, cfg)

	_, err := svc.Issue(context.Background(), IssueInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		TTL:        30 * time.Second, // below MinTTL of 1 minute
		IssuedBy:   "issuer@example.com",
	})
	if err == nil {
		t.Fatal("Issue() err = nil, want TTL below minimum error")
	}
	if repo.issueCalled {
		t.Error("repo.Issue should not be called for invalid TTL")
	}
}

func TestIssue_TTLExceedsMaximum(t *testing.T) {
	t.Parallel()

	cfg := newTestConfig()
	repo := ptrext.Of(stubRepo{})
	svc := NewService(repo, cfg)

	_, err := svc.Issue(context.Background(), IssueInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		TTL:        25 * time.Hour, // exceeds MaxTTL of 1 hour
		IssuedBy:   "issuer@example.com",
	})
	if err == nil {
		t.Fatal("Issue() err = nil, want TTL exceeds maximum error")
	}
	if repo.issueCalled {
		t.Error("repo.Issue should not be called for invalid TTL")
	}
}

func TestIssue_RepoError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database error")
	repo := ptrext.Of(stubRepo{issueErr: wantErr})
	svc := NewService(repo, newTestConfig())

	_, err := svc.Issue(context.Background(), IssueInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		TTL:        5 * time.Minute,
		IssuedBy:   "issuer@example.com",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Issue() err = %v, want %v", err, wantErr)
	}
}

// --- Validate tests ---

func TestValidate_Success(t *testing.T) {
	t.Parallel()

	// Generate a real token and hash for testing
	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)

	repo := ptrext.Of(stubRepo{
		listValidTokens: []domain.BreakGlassToken{
			{
				ID:         "token-1",
				TenantID:   "tenant-1",
				AdminEmail: "admin@example.com",
				TokenHash:  hash,
				ExpiresAt:  time.Now().Add(10 * time.Minute),
			},
		},
	})
	svc := NewService(repo, newTestConfig())

	result, err := svc.Validate(context.Background(), ValidateInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		RawToken:   rawToken,
	})
	if err != nil {
		t.Fatalf("Validate() err = %v, want nil", err)
	}
	if result.ID != "token-1" {
		t.Errorf("Token.ID = %s, want token-1", result.ID)
	}
	if !repo.markUsedCalled {
		t.Error("expected MarkUsed to be called")
	}
	if repo.markUsedID != "token-1" {
		t.Errorf("MarkUsed called with id = %s, want token-1", repo.markUsedID)
	}
}

func TestValidate_InvalidToken(t *testing.T) {
	t.Parallel()

	// Create a hash for a different token
	differentToken, _ := generateToken()
	hash, _ := hashToken(differentToken)

	repo := ptrext.Of(stubRepo{
		listValidTokens: []domain.BreakGlassToken{
			{
				ID:        "token-1",
				TokenHash: hash,
				ExpiresAt: time.Now().Add(10 * time.Minute),
			},
		},
	})
	svc := NewService(repo, newTestConfig())

	_, err := svc.Validate(context.Background(), ValidateInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		RawToken:   "bg_wrongtoken",
	})
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Fatalf("Validate() err = %v, want ErrNotFound", err)
	}
	if repo.markUsedCalled {
		t.Error("MarkUsed should not be called for invalid token")
	}
}

func TestValidate_NoTokensExist(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{listValidTokens: nil})
	svc := NewService(repo, newTestConfig())

	_, err := svc.Validate(context.Background(), ValidateInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		RawToken:   "bg_sometoken",
	})
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Fatalf("Validate() err = %v, want ErrNotFound", err)
	}
}

func TestValidate_RaceCondition_ErrAlreadyUsed(t *testing.T) {
	t.Parallel()

	// Generate a token
	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)

	// Repo that simulates a race: MarkUsed returns ErrAlreadyUsed
	repo := ptrext.Of(stubRepo{
		listValidTokens: []domain.BreakGlassToken{
			{ID: "token-1", TokenHash: hash, ExpiresAt: time.Now().Add(10 * time.Minute)},
		},
		markUsedErr: breakglass.ErrAlreadyUsed,
	})

	svc := NewService(repo, newTestConfig())

	// Try to validate with the token, but it gets marked as used by another
	// request (race condition). The service should continue to next token
	// or return ErrNotFound if no more tokens.
	_, err := svc.Validate(context.Background(), ValidateInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		RawToken:   rawToken,
	})
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Fatalf("Validate() err = %v, want ErrNotFound (race condition)", err)
	}
	if !repo.markUsedCalled {
		t.Error("expected MarkUsed to be called")
	}
}

func TestValidate_ListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database error")
	repo := ptrext.Of(stubRepo{listValidErr: wantErr})
	svc := NewService(repo, newTestConfig())

	_, err := svc.Validate(context.Background(), ValidateInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		RawToken:   "bg_token",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Validate() err = %v, want %v", err, wantErr)
	}
}

func TestValidate_MarkUsedError(t *testing.T) {
	t.Parallel()

	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)
	wantErr := errors.New("database error")

	repo := ptrext.Of(stubRepo{
		listValidTokens: []domain.BreakGlassToken{
			{ID: "token-1", TokenHash: hash, ExpiresAt: time.Now().Add(10 * time.Minute)},
		},
		markUsedErr: wantErr,
	})
	svc := NewService(repo, newTestConfig())

	_, err := svc.Validate(context.Background(), ValidateInput{
		TenantID:   "tenant-1",
		AdminEmail: "admin@example.com",
		RawToken:   rawToken,
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Validate() err = %v, want %v", err, wantErr)
	}
}

// --- ValidateByToken tests ---

func TestValidateByToken_Success(t *testing.T) {
	t.Parallel()

	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)

	repo := ptrext.Of(stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{
				ID:         "token-1",
				TenantID:   "tenant-1",
				AdminEmail: "admin@example.com",
				TokenHash:  hash,
				ExpiresAt:  time.Now().Add(10 * time.Minute),
			},
		},
	})
	svc := NewService(repo, newTestConfig())

	result, err := svc.ValidateByToken(context.Background(), "tenant-1", rawToken, "192.168.1.100")
	if err != nil {
		t.Fatalf("ValidateByToken() err = %v, want nil", err)
	}
	if result.ID != "token-1" {
		t.Errorf("Token.ID = %s, want token-1", result.ID)
	}
	if result.AdminEmail != "admin@example.com" {
		t.Errorf("AdminEmail = %s, want admin@example.com", result.AdminEmail)
	}
	if !repo.markUsedCalled {
		t.Error("expected MarkUsed to be called")
	}
}

func TestValidateByToken_ExpiredToken(t *testing.T) {
	t.Parallel()

	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)

	repo := ptrext.Of(stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{
				ID:        "token-1",
				TokenHash: hash,
				ExpiresAt: time.Now().Add(-1 * time.Hour), // expired
			},
		},
	})
	svc := NewService(repo, newTestConfig())

	_, err := svc.ValidateByToken(context.Background(), "tenant-1", rawToken, "192.168.1.100")
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Fatalf("ValidateByToken() err = %v, want ErrNotFound (expired)", err)
	}
	if repo.markUsedCalled {
		t.Error("MarkUsed should not be called for expired token")
	}
}

func TestValidateByToken_AlreadyUsedToken(t *testing.T) {
	t.Parallel()

	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)
	usedAt := time.Now().Add(-5 * time.Minute)

	repo := ptrext.Of(stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{
				ID:        "token-1",
				TokenHash: hash,
				ExpiresAt: time.Now().Add(10 * time.Minute),
				UsedAt:    ptrext.Of(usedAt), // already used
			},
		},
	})
	svc := NewService(repo, newTestConfig())

	_, err := svc.ValidateByToken(context.Background(), "tenant-1", rawToken, "192.168.1.100")
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Fatalf("ValidateByToken() err = %v, want ErrNotFound (already used)", err)
	}
	if repo.markUsedCalled {
		t.Error("MarkUsed should not be called for already used token")
	}
}

func TestValidateByToken_RevokedToken(t *testing.T) {
	t.Parallel()

	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)
	revokedAt := time.Now().Add(-5 * time.Minute)

	repo := ptrext.Of(stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{
				ID:        "token-1",
				TokenHash: hash,
				ExpiresAt: time.Now().Add(10 * time.Minute),
				RevokedAt: ptrext.Of(revokedAt), // revoked
			},
		},
	})
	svc := NewService(repo, newTestConfig())

	_, err := svc.ValidateByToken(context.Background(), "tenant-1", rawToken, "192.168.1.100")
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Fatalf("ValidateByToken() err = %v, want ErrNotFound (revoked)", err)
	}
	if repo.markUsedCalled {
		t.Error("MarkUsed should not be called for revoked token")
	}
}

func TestValidateByToken_RaceCondition(t *testing.T) {
	t.Parallel()

	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)

	repo := ptrext.Of(stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{
				ID:        "token-1",
				TokenHash: hash,
				ExpiresAt: time.Now().Add(10 * time.Minute),
			},
		},
		markUsedErr: breakglass.ErrAlreadyUsed, // simulate race
	})
	svc := NewService(repo, newTestConfig())

	_, err := svc.ValidateByToken(context.Background(), "tenant-1", rawToken, "192.168.1.100")
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Fatalf("ValidateByToken() err = %v, want ErrNotFound (race)", err)
	}
}

func TestValidateByToken_ListError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database error")
	repo := ptrext.Of(stubRepo{listAllErr: wantErr})
	svc := NewService(repo, newTestConfig())

	_, err := svc.ValidateByToken(context.Background(), "tenant-1", "bg_token", "192.168.1.100")
	if !errors.Is(err, wantErr) {
		t.Fatalf("ValidateByToken() err = %v, want %v", err, wantErr)
	}
}

func TestValidateByToken_IPNotAllowed(t *testing.T) {
	t.Parallel()

	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)

	repo := ptrext.Of(stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{
				ID:         "token-1",
				AdminEmail: "admin@example.com",
				TokenHash:  hash,
				ExpiresAt:  time.Now().Add(10 * time.Minute),
				AllowedIPs: []string{"10.0.0.0/8"}, // only 10.x.x.x allowed
			},
		},
	})
	svc := NewService(repo, newTestConfig())

	// Try from a disallowed IP
	_, err := svc.ValidateByToken(context.Background(), "tenant-1", rawToken, "192.168.1.100")
	if !errors.Is(err, breakglass.ErrIPNotAllowed) {
		t.Fatalf("ValidateByToken() err = %v, want ErrIPNotAllowed", err)
	}
	if repo.markUsedCalled {
		t.Error("MarkUsed should not be called when IP is not allowed")
	}
}

func TestValidateByToken_IPAllowed(t *testing.T) {
	t.Parallel()

	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)

	repo := ptrext.Of(stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{
				ID:         "token-1",
				AdminEmail: "admin@example.com",
				TokenHash:  hash,
				ExpiresAt:  time.Now().Add(10 * time.Minute),
				AllowedIPs: []string{"192.168.0.0/16"}, // 192.168.x.x allowed
			},
		},
	})
	svc := NewService(repo, newTestConfig())

	// Try from an allowed IP
	result, err := svc.ValidateByToken(context.Background(), "tenant-1", rawToken, "192.168.1.100")
	if err != nil {
		t.Fatalf("ValidateByToken() err = %v, want nil", err)
	}
	if result.ID != "token-1" {
		t.Errorf("Token.ID = %s, want token-1", result.ID)
	}
	if !repo.markUsedCalled {
		t.Error("MarkUsed should be called when IP is allowed")
	}
	if repo.markUsedClientIP != "192.168.1.100" {
		t.Errorf("MarkUsed clientIP = %s, want 192.168.1.100", repo.markUsedClientIP)
	}
}

func TestValidateByToken_NoIPRestriction(t *testing.T) {
	t.Parallel()

	rawToken, _ := generateToken()
	hash, _ := hashToken(rawToken)

	repo := ptrext.Of(stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{
				ID:         "token-1",
				AdminEmail: "admin@example.com",
				TokenHash:  hash,
				ExpiresAt:  time.Now().Add(10 * time.Minute),
				AllowedIPs: nil, // no restriction
			},
		},
	})
	svc := NewService(repo, newTestConfig())

	// Any IP should work
	result, err := svc.ValidateByToken(context.Background(), "tenant-1", rawToken, "1.2.3.4")
	if err != nil {
		t.Fatalf("ValidateByToken() err = %v, want nil", err)
	}
	if result.ID != "token-1" {
		t.Errorf("Token.ID = %s, want token-1", result.ID)
	}
}

// --- Revoke tests ---

func TestRevoke_Success(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{})
	svc := NewService(repo, newTestConfig())

	err := svc.Revoke(context.Background(), "tenant-1", "token-1", "revoker@example.com")
	if err != nil {
		t.Fatalf("Revoke() err = %v, want nil", err)
	}
	if !repo.revokeCalled {
		t.Error("expected Revoke to be called")
	}
	if repo.revokeID != "token-1" {
		t.Errorf("Revoke called with id = %s, want token-1", repo.revokeID)
	}
	if repo.revokeBy != "revoker@example.com" {
		t.Errorf("Revoke called with revokedBy = %s, want revoker@example.com", repo.revokeBy)
	}
}

func TestRevoke_NotFound(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{revokeErr: breakglass.ErrNotFound})
	svc := NewService(repo, newTestConfig())

	err := svc.Revoke(context.Background(), "tenant-1", "nonexistent", "revoker@example.com")
	if !errors.Is(err, breakglass.ErrNotFound) {
		t.Fatalf("Revoke() err = %v, want ErrNotFound", err)
	}
}

func TestRevoke_AlreadyRevoked(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{revokeErr: breakglass.ErrAlreadyRevoked})
	svc := NewService(repo, newTestConfig())

	err := svc.Revoke(context.Background(), "tenant-1", "token-1", "revoker@example.com")
	if !errors.Is(err, breakglass.ErrAlreadyRevoked) {
		t.Fatalf("Revoke() err = %v, want ErrAlreadyRevoked", err)
	}
}

func TestRevoke_AlreadyUsed(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{revokeErr: breakglass.ErrAlreadyUsed})
	svc := NewService(repo, newTestConfig())

	err := svc.Revoke(context.Background(), "tenant-1", "token-1", "revoker@example.com")
	if !errors.Is(err, breakglass.ErrAlreadyUsed) {
		t.Fatalf("Revoke() err = %v, want ErrAlreadyUsed", err)
	}
}

// --- List tests ---

func TestList_Success(t *testing.T) {
	t.Parallel()

	tokens := []domain.BreakGlassToken{
		{ID: "token-1", AdminEmail: "admin1@example.com"},
		{ID: "token-2", AdminEmail: "admin2@example.com"},
	}
	repo := ptrext.Of(stubRepo{listAllTokens: tokens})
	svc := NewService(repo, newTestConfig())

	result, err := svc.List(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("List() err = %v, want nil", err)
	}
	if len(result) != 2 {
		t.Errorf("List() returned %d tokens, want 2", len(result))
	}
}

func TestList_Empty(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{listAllTokens: nil})
	svc := NewService(repo, newTestConfig())

	result, err := svc.List(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("List() err = %v, want nil", err)
	}
	if len(result) != 0 {
		t.Errorf("List() returned %d tokens, want 0", len(result))
	}
}

func TestList_Error(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database error")
	repo := ptrext.Of(stubRepo{listAllErr: wantErr})
	svc := NewService(repo, newTestConfig())

	_, err := svc.List(context.Background(), "tenant-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("List() err = %v, want %v", err, wantErr)
	}
}

// --- CountValid tests ---

func TestCountValid_Success(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{countValidResult: 5})
	svc := NewService(repo, newTestConfig())

	count, err := svc.CountValid(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("CountValid() err = %v, want nil", err)
	}
	if count != 5 {
		t.Errorf("CountValid() = %d, want 5", count)
	}
}

func TestCountValid_Zero(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{countValidResult: 0})
	svc := NewService(repo, newTestConfig())

	count, err := svc.CountValid(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("CountValid() err = %v, want nil", err)
	}
	if count != 0 {
		t.Errorf("CountValid() = %d, want 0", count)
	}
}

func TestCountValid_Error(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database error")
	repo := ptrext.Of(stubRepo{countValidErr: wantErr})
	svc := NewService(repo, newTestConfig())

	_, err := svc.CountValid(context.Background(), "tenant-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("CountValid() err = %v, want %v", err, wantErr)
	}
}

// --- Cleanup tests ---

func TestCleanup_Success(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{cleanupResult: 10})
	svc := NewService(repo, newTestConfig())

	count, err := svc.Cleanup(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup() err = %v, want nil", err)
	}
	if count != 10 {
		t.Errorf("Cleanup() = %d, want 10", count)
	}
}

func TestCleanup_NoExpiredTokens(t *testing.T) {
	t.Parallel()

	repo := ptrext.Of(stubRepo{cleanupResult: 0})
	svc := NewService(repo, newTestConfig())

	count, err := svc.Cleanup(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("Cleanup() err = %v, want nil", err)
	}
	if count != 0 {
		t.Errorf("Cleanup() = %d, want 0", count)
	}
}

func TestCleanup_Error(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database error")
	repo := ptrext.Of(stubRepo{cleanupErr: wantErr})
	svc := NewService(repo, newTestConfig())

	_, err := svc.Cleanup(context.Background(), 24*time.Hour)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Cleanup() err = %v, want %v", err, wantErr)
	}
}

// --- Token generation tests ---

func TestGenerateToken_Format(t *testing.T) {
	t.Parallel()

	token, err := generateToken()
	if err != nil {
		t.Fatalf("generateToken() err = %v", err)
	}
	if !hasPrefix(token, TokenPrefix) {
		t.Errorf("token = %s, expected prefix %s", token, TokenPrefix)
	}
	// Token should be bg_ + 43 chars (32 bytes base64url encoded)
	expectedLen := len(TokenPrefix) + 43
	if len(token) != expectedLen {
		t.Errorf("token length = %d, want %d", len(token), expectedLen)
	}
}

func TestGenerateToken_Uniqueness(t *testing.T) {
	t.Parallel()

	tokens := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token, err := generateToken()
		if err != nil {
			t.Fatalf("generateToken() err = %v", err)
		}
		if tokens[token] {
			t.Fatalf("duplicate token generated: %s", token)
		}
		tokens[token] = true
	}
}

func TestHashAndVerifyToken(t *testing.T) {
	t.Parallel()

	token, _ := generateToken()
	hash, err := hashToken(token)
	if err != nil {
		t.Fatalf("hashToken() err = %v", err)
	}
	if hash == "" {
		t.Error("hash is empty")
	}
	if hash == token {
		t.Error("hash should not equal token")
	}

	// Verify correct token
	if !verifyToken(token, hash) {
		t.Error("verifyToken() = false for correct token")
	}

	// Verify incorrect token
	if verifyToken("wrong-token", hash) {
		t.Error("verifyToken() = true for wrong token")
	}
}

// --- Config tests ---

func TestDefaultConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultConfig()
	if cfg.DefaultTTL != 30*time.Minute {
		t.Errorf("DefaultTTL = %v, want 30m", cfg.DefaultTTL)
	}
	if cfg.MaxTTL != 24*time.Hour {
		t.Errorf("MaxTTL = %v, want 24h", cfg.MaxTTL)
	}
	if cfg.MinTTL != 5*time.Minute {
		t.Errorf("MinTTL = %v, want 5m", cfg.MinTTL)
	}
}

// --- Helpers ---

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
