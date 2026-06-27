// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test fixtures with inline struct pointers

package breakglass

import (
	"context"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/breakglass"
	"github.com/Phixsura/attune/internal/service/securityalert"
)

type mockWarnRepo struct {
	sent       map[string]bool
	markSentID string
}

func newMockWarnRepo() *mockWarnRepo {
	return &mockWarnRepo{sent: make(map[string]bool)}
}

func (m *mockWarnRepo) HasSent(_ context.Context, tokenID string, warnType breakglass.ExpiryWarningType) (bool, error) {
	return m.sent[tokenID+string(warnType)], nil
}

func (m *mockWarnRepo) MarkSent(_ context.Context, tokenID string, warnType breakglass.ExpiryWarningType) error {
	m.markSentID = tokenID
	m.sent[tokenID+string(warnType)] = true
	return nil
}

func TestExpiryWarningService_CheckAndWarn_NoTokens(t *testing.T) {
	t.Parallel()

	tokenRepo := &stubRepo{listAllTokens: []domain.BreakGlassToken{}}
	svc := NewExpiryWarningService(tokenRepo, nil, nil)

	result, err := svc.CheckAndWarn(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("CheckAndWarn() err = %v, want nil", err)
	}
	if result.Warnings24h != 0 || result.Warnings1h != 0 {
		t.Errorf("Warnings = %d/%d, want 0/0", result.Warnings24h, result.Warnings1h)
	}
}

func TestExpiryWarningService_CheckAndWarn_SkipsUsedTokens(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tokenRepo := &stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{ID: "used", ExpiresAt: now.Add(30 * time.Minute), UsedAt: ptrext.Of(now)},
			{ID: "revoked", ExpiresAt: now.Add(30 * time.Minute), RevokedAt: ptrext.Of(now)},
			{ID: "expired", ExpiresAt: now.Add(-1 * time.Hour)},
		},
	}
	svc := NewExpiryWarningService(tokenRepo, nil, nil)

	result, err := svc.CheckAndWarn(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("CheckAndWarn() err = %v, want nil", err)
	}
	if result.Warnings24h != 0 || result.Warnings1h != 0 {
		t.Errorf("Warnings = %d/%d, want 0/0", result.Warnings24h, result.Warnings1h)
	}
}

func TestExpiryWarningService_CheckAndWarn_Warns1h(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tokenRepo := &stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{ID: "expiring-soon", AdminEmail: "admin@example.com", TenantID: "tenant-1", ExpiresAt: now.Add(30 * time.Minute)},
		},
	}
	warnRepo := newMockWarnRepo()
	alertSvc := securityalert.NewService("")
	svc := NewExpiryWarningService(tokenRepo, warnRepo, alertSvc)

	result, err := svc.CheckAndWarn(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("CheckAndWarn() err = %v, want nil", err)
	}
	if result.Warnings1h != 1 {
		t.Errorf("Warnings1h = %d, want 1", result.Warnings1h)
	}
	if warnRepo.markSentID != "expiring-soon" {
		t.Errorf("markSentID = %s, want expiring-soon", warnRepo.markSentID)
	}
}

func TestExpiryWarningService_CheckAndWarn_Warns24h(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tokenRepo := &stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{ID: "expiring-24h", AdminEmail: "admin@example.com", TenantID: "tenant-1", ExpiresAt: now.Add(12 * time.Hour)},
		},
	}
	warnRepo := newMockWarnRepo()
	alertSvc := securityalert.NewService("")
	svc := NewExpiryWarningService(tokenRepo, warnRepo, alertSvc)

	result, err := svc.CheckAndWarn(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("CheckAndWarn() err = %v, want nil", err)
	}
	if result.Warnings24h != 1 {
		t.Errorf("Warnings24h = %d, want 1", result.Warnings24h)
	}
}

func TestExpiryWarningService_CheckAndWarn_AlreadySentReturnsSuccess(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tokenRepo := &stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{ID: "expiring-soon", AdminEmail: "admin@example.com", TenantID: "tenant-1", ExpiresAt: now.Add(30 * time.Minute)},
		},
	}
	warnRepo := newMockWarnRepo()
	warnRepo.sent["expiring-soon"+string(breakglass.ExpiryWarning1h)] = true
	alertSvc := securityalert.NewService("")
	svc := NewExpiryWarningService(tokenRepo, warnRepo, alertSvc)

	result, err := svc.CheckAndWarn(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("CheckAndWarn() err = %v, want nil", err)
	}
	// Note: counter increments even for already-sent warnings as sendWarningIfNeeded returns nil
	if result.Warnings1h != 1 {
		t.Errorf("Warnings1h = %d, want 1", result.Warnings1h)
	}
}

func TestExpiryWarningService_GetExpiringTokens(t *testing.T) {
	t.Parallel()

	now := time.Now()
	tokenRepo := &stubRepo{
		listAllTokens: []domain.BreakGlassToken{
			{ID: "expiring-soon", ExpiresAt: now.Add(30 * time.Minute)},
			{ID: "not-expiring", ExpiresAt: now.Add(48 * time.Hour)},
			{ID: "used", ExpiresAt: now.Add(30 * time.Minute), UsedAt: ptrext.Of(now)},
		},
	}
	svc := NewExpiryWarningService(tokenRepo, nil, nil)

	tokens, err := svc.GetExpiringTokens(context.Background(), "tenant-1", 1*time.Hour)
	if err != nil {
		t.Fatalf("GetExpiringTokens() err = %v, want nil", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("len(tokens) = %d, want 1", len(tokens))
	}
	if tokens[0].ID != "expiring-soon" {
		t.Errorf("tokens[0].ID = %s, want expiring-soon", tokens[0].ID)
	}
}

func TestFormatExpiryWarning_Minutes(t *testing.T) {
	t.Parallel()

	token := domain.BreakGlassToken{AdminEmail: "admin@example.com"}
	result := formatExpiryMinutes(token, 45)
	if result == "" {
		t.Error("formatExpiryMinutes returned empty string")
	}
}

func TestFormatExpiryWarning_Hours(t *testing.T) {
	t.Parallel()

	token := domain.BreakGlassToken{AdminEmail: "admin@example.com"}
	result := formatExpiryHours(token, 12)
	if result == "" {
		t.Error("formatExpiryHours returned empty string")
	}
}
