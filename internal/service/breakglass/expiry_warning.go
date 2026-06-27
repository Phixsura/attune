// SPDX-License-Identifier: Apache-2.0

package breakglass

import (
	"context"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/breakglass"
	"github.com/Phixsura/attune/internal/service/securityalert"
)

// ExpiryWarningRepository tracks sent warnings to avoid duplicates.
type ExpiryWarningRepository interface {
	HasSent(ctx context.Context, tokenID string, warningType breakglass.ExpiryWarningType) (bool, error)
	MarkSent(ctx context.Context, tokenID string, warningType breakglass.ExpiryWarningType) error
}

// ExpiryWarningService checks for expiring tokens and sends alerts.
type ExpiryWarningService struct {
	tokenRepo Repository
	warnRepo  ExpiryWarningRepository
	alertSvc  *securityalert.Service
}

// NewExpiryWarningService creates a new expiry warning service.
func NewExpiryWarningService(
	tokenRepo Repository,
	warnRepo ExpiryWarningRepository,
	alertSvc *securityalert.Service,
) *ExpiryWarningService {
	return ptrext.Of(ExpiryWarningService{
		tokenRepo: tokenRepo,
		warnRepo:  warnRepo,
		alertSvc:  alertSvc,
	})
}

// ExpiryCheckResult contains the results of an expiry check run.
type ExpiryCheckResult struct {
	Warnings24h int
	Warnings1h  int
	Expired     int
}

// CheckAndWarn checks all valid tokens and sends warnings for those about to expire.
// This should be called periodically (e.g., every 15 minutes).
func (s *ExpiryWarningService) CheckAndWarn(ctx context.Context, tenantID string) (ExpiryCheckResult, error) {
	const where = "breakglass.ExpiryWarningService.CheckAndWarn"

	tokens, err := s.tokenRepo.ListAll(ctx, tenantID, 1000)
	if err != nil {
		return ExpiryCheckResult{}, err
	}

	now := time.Now()
	result := ExpiryCheckResult{}

	for _, t := range tokens {
		// Skip used, revoked, or already expired tokens
		if t.UsedAt != nil || t.RevokedAt != nil || t.ExpiresAt.Before(now) {
			continue
		}

		timeUntilExpiry := t.ExpiresAt.Sub(now)

		// Check for 1h warning (takes precedence over 24h)
		if timeUntilExpiry <= 1*time.Hour {
			if err := s.sendWarningIfNeeded(ctx, t, breakglass.ExpiryWarning1h); err == nil {
				result.Warnings1h++
			}
		} else if timeUntilExpiry <= 24*time.Hour {
			if err := s.sendWarningIfNeeded(ctx, t, breakglass.ExpiryWarning24h); err == nil {
				result.Warnings24h++
			}
		}
	}

	if result.Warnings24h > 0 || result.Warnings1h > 0 {
		logext.Infof(ctx, "[%s] sent expiry warnings,tenant:%s,24h:%d,1h:%d",
			where, tenantID, result.Warnings24h, result.Warnings1h)
	}

	return result, nil
}

func (s *ExpiryWarningService) sendWarningIfNeeded(ctx context.Context, token domain.BreakGlassToken, warnType breakglass.ExpiryWarningType) error {
	const where = "breakglass.ExpiryWarningService.sendWarningIfNeeded"

	// Check if we already sent this warning
	if s.warnRepo != nil {
		sent, err := s.warnRepo.HasSent(ctx, token.ID, warnType)
		if err != nil {
			logext.Warnf(ctx, "[%s] failed to check warning status,id:%s,err:%s", where, token.ID, err.Error())
			return err
		}
		if sent {
			return nil // Already sent, skip
		}
	}

	// Send alert
	if s.alertSvc != nil {
		timeLeft := time.Until(token.ExpiresAt)
		alert := securityalert.Alert{
			Type:     securityalert.AlertType("breakglass.expiring"),
			TenantID: token.TenantID,
			Actor:    "system",
			Summary:  formatExpiryWarning(token, warnType, timeLeft),
			Details: map[string]string{
				"token_id":    token.ID,
				"admin_email": token.AdminEmail,
				"expires_at":  token.ExpiresAt.Format(time.RFC3339),
				"warning":     string(warnType),
			},
		}
		s.alertSvc.Send(ctx, alert)
	}

	// Mark as sent
	if s.warnRepo != nil {
		if err := s.warnRepo.MarkSent(ctx, token.ID, warnType); err != nil {
			logext.Warnf(ctx, "[%s] failed to mark warning sent,id:%s,err:%s", where, token.ID, err.Error())
		}
	}

	return nil
}

func formatExpiryWarning(token domain.BreakGlassToken, warnType breakglass.ExpiryWarningType, timeLeft time.Duration) string {
	minutes := int(timeLeft.Minutes())
	if minutes < 60 {
		return formatExpiryMinutes(token, minutes)
	}
	hours := minutes / 60
	return formatExpiryHours(token, hours)
}

func formatExpiryMinutes(token domain.BreakGlassToken, minutes int) string {
	return "Break-glass token for " + token.AdminEmail + " expires in " + string(rune('0'+minutes/10)) + string(rune('0'+minutes%10)) + " minutes"
}

func formatExpiryHours(token domain.BreakGlassToken, hours int) string {
	return "Break-glass token for " + token.AdminEmail + " expires in " + string(rune('0'+hours/10)) + string(rune('0'+hours%10)) + " hours"
}

// GetExpiringTokens returns tokens that will expire within the given duration.
func (s *ExpiryWarningService) GetExpiringTokens(ctx context.Context, tenantID string, within time.Duration) ([]domain.BreakGlassToken, error) {
	tokens, err := s.tokenRepo.ListAll(ctx, tenantID, 1000)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	expiringSoon := make([]domain.BreakGlassToken, 0)

	for _, t := range tokens {
		if t.UsedAt != nil || t.RevokedAt != nil || t.ExpiresAt.Before(now) {
			continue
		}
		if t.ExpiresAt.Sub(now) <= within {
			expiringSoon = append(expiringSoon, t)
		}
	}

	return expiringSoon, nil
}
