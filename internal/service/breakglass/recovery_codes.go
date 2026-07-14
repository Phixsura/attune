// SPDX-License-Identifier: Apache-2.0

package breakglass

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"github.com/ProtonMail/bcrypt"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/breakglass"
)

// RecoveryCodePrefix is prepended to all recovery codes.
const RecoveryCodePrefix = "rc_"

// DefaultRecoveryCodeCount is how many codes to generate.
const DefaultRecoveryCodeCount = 10

// RecoveryCodeRepository defines the data-access contract for recovery codes.
type RecoveryCodeRepository interface {
	Create(ctx context.Context, n breakglass.NewRecoveryCode) (domain.BreakGlassRecoveryCode, error)
	ListValid(ctx context.Context, tenantID string) ([]domain.BreakGlassRecoveryCode, error)
	ListAll(ctx context.Context, tenantID string, limit int) ([]domain.BreakGlassRecoveryCode, error)
	MarkUsed(ctx context.Context, tenantID, id, clientIP string) error
	Revoke(ctx context.Context, tenantID, id, revokedBy string) error
	RevokeAll(ctx context.Context, tenantID, revokedBy string) (int64, error)
	CountValid(ctx context.Context, tenantID string) (int, error)
}

// RecoveryCodeService manages break-glass recovery codes.
type RecoveryCodeService struct {
	repo RecoveryCodeRepository
}

// NewRecoveryCodeService creates a new recovery code service.
func NewRecoveryCodeService(repo RecoveryCodeRepository) *RecoveryCodeService {
	return ptrext.Of(RecoveryCodeService{repo: repo})
}

// GenerateCodesInput is the input for generating recovery codes.
type GenerateCodesInput struct {
	TenantID  string
	Count     int
	CreatedBy string
}

// GenerateCodesResult is the result of generating recovery codes.
type GenerateCodesResult struct {
	Codes    []domain.BreakGlassRecoveryCode
	RawCodes []string // Plain text codes (only returned once!)
}

// GenerateCodes creates a new set of recovery codes.
// Returns the codes with their plain text values - these are only shown once!
func (s *RecoveryCodeService) GenerateCodes(ctx context.Context, input GenerateCodesInput) (GenerateCodesResult, error) {
	const where = "breakglass.RecoveryCodeService.GenerateCodes"

	count := input.Count
	if count <= 0 {
		count = DefaultRecoveryCodeCount
	}
	if count > 20 {
		count = 20 // Cap at 20 codes
	}

	result := GenerateCodesResult{
		Codes:    make([]domain.BreakGlassRecoveryCode, 0, count),
		RawCodes: make([]string, 0, count),
	}

	for i := 1; i <= count; i++ {
		rawCode, err := generateRecoveryCode()
		if err != nil {
			return GenerateCodesResult{}, fmt.Errorf("generate code: %w", err)
		}

		hash, err := hashRecoveryCode(rawCode)
		if err != nil {
			return GenerateCodesResult{}, fmt.Errorf("hash code: %w", err)
		}

		code, err := s.repo.Create(ctx, breakglass.NewRecoveryCode{
			TenantID:  input.TenantID,
			CodeHash:  hash,
			Label:     fmt.Sprintf("Code %d", i),
			CreatedBy: input.CreatedBy,
		})
		if err != nil {
			return GenerateCodesResult{}, err
		}

		result.Codes = append(result.Codes, code)
		result.RawCodes = append(result.RawCodes, rawCode)
	}

	logext.Infof(ctx, "[%s] generated %d recovery codes,tenant:%s,created_by:%s",
		where, count, input.TenantID, input.CreatedBy)

	return result, nil
}

// ValidateCode validates a recovery code and marks it as used.
func (s *RecoveryCodeService) ValidateCode(ctx context.Context, tenantID, rawCode, clientIP string) (domain.BreakGlassRecoveryCode, error) {
	const where = "breakglass.RecoveryCodeService.ValidateCode"

	if !strings.HasPrefix(rawCode, RecoveryCodePrefix) {
		return domain.BreakGlassRecoveryCode{}, breakglass.ErrCodeNotFound
	}

	codes, err := s.repo.ListValid(ctx, tenantID)
	if err != nil {
		return domain.BreakGlassRecoveryCode{}, err
	}

	for _, c := range codes {
		if verifyRecoveryCode(rawCode, c.CodeHash) {
			if err := s.repo.MarkUsed(ctx, tenantID, c.ID, clientIP); err != nil {
				if errors.Is(err, breakglass.ErrCodeAlreadyUsed) {
					logext.Warnf(ctx, "[%s] code race: already used,id:%s", where, c.ID)
					continue
				}
				return domain.BreakGlassRecoveryCode{}, err
			}
			logext.Infof(ctx, "[%s] recovery code validated and consumed,id:%s,label:%s,ip:%s",
				where, c.ID, c.Label, clientIP)
			return c, nil
		}
	}

	logext.Warnf(ctx, "[%s] no valid recovery code found", where)
	return domain.BreakGlassRecoveryCode{}, breakglass.ErrCodeNotFound
}

// List returns all codes for a tenant.
func (s *RecoveryCodeService) List(ctx context.Context, tenantID string) ([]domain.BreakGlassRecoveryCode, error) {
	return s.repo.ListAll(ctx, tenantID, 100)
}

// CountValid returns the number of valid codes for a tenant.
func (s *RecoveryCodeService) CountValid(ctx context.Context, tenantID string) (int, error) {
	return s.repo.CountValid(ctx, tenantID)
}

// Revoke revokes a specific recovery code.
func (s *RecoveryCodeService) Revoke(ctx context.Context, tenantID, codeID, revokedBy string) error {
	const where = "breakglass.RecoveryCodeService.Revoke"
	if err := s.repo.Revoke(ctx, tenantID, codeID, revokedBy); err != nil {
		return err
	}
	logext.Infof(ctx, "[%s] recovery code revoked,id:%s,revoked_by:%s", where, codeID, revokedBy)
	return nil
}

// RevokeAll revokes all valid codes for a tenant.
func (s *RecoveryCodeService) RevokeAll(ctx context.Context, tenantID, revokedBy string) (int64, error) {
	const where = "breakglass.RecoveryCodeService.RevokeAll"
	n, err := s.repo.RevokeAll(ctx, tenantID, revokedBy)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		logext.Infof(ctx, "[%s] revoked %d recovery codes,tenant:%s,revoked_by:%s", where, n, tenantID, revokedBy)
	}
	return n, nil
}

// generateRecoveryCode creates a random recovery code.
// Format: rc_XXXX-XXXX-XXXX (12 chars, grouped by 4)
func generateRecoveryCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// Use base32 without padding for readability
	encoded := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
	// Take first 12 chars, format as XXXX-XXXX-XXXX
	if len(encoded) < 12 {
		encoded = encoded + "000000000000"
	}
	formatted := fmt.Sprintf("%s-%s-%s", encoded[0:4], encoded[4:8], encoded[8:12])
	return RecoveryCodePrefix + formatted, nil
}

// hashRecoveryCode hashes a recovery code for storage.
func hashRecoveryCode(code string) (string, error) {
	return bcrypt.Hash(code)
}

// verifyRecoveryCode checks if a code matches a hash.
func verifyRecoveryCode(code, hash string) bool {
	return bcrypt.Match(code, hash)
}
