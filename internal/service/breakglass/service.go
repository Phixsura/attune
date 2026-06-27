// SPDX-License-Identifier: Apache-2.0

// Package breakglass provides break-glass token issuance and validation
// for emergency admin access (#158).
package breakglass

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/breakglass"
)

// Repository defines the data-access contract for break-glass tokens.
type Repository interface {
	Issue(ctx context.Context, n breakglass.NewToken) (domain.BreakGlassToken, error)
	ListValidForAdmin(ctx context.Context, tenantID, adminEmail string) ([]domain.BreakGlassToken, error)
	ListAll(ctx context.Context, tenantID string, limit int) ([]domain.BreakGlassToken, error)
	MarkUsed(ctx context.Context, tenantID, id, clientIP string) error
	Revoke(ctx context.Context, tenantID, id, revokedBy string) error
	RevokeAll(ctx context.Context, tenantID, revokedBy string) (int64, error)
	CountValid(ctx context.Context, tenantID string) (int, error)
	Cleanup(ctx context.Context, cutoff time.Time) (int64, error)
}

const (
	// TokenPrefix distinguishes break-glass tokens in logs.
	TokenPrefix = "bg_"
	// tokenBytes is the number of random bytes in a token (32 bytes = 256 bits).
	tokenBytes = 32
	// bcryptCost is the bcrypt cost factor for token hashing.
	bcryptCost = 12
)

// dummyHash is a pre-computed bcrypt hash used to prevent timing attacks.
// When no valid token is found, we still run bcrypt.CompareHashAndPassword
// against this dummy hash to normalize response time.
// #nosec G101 -- not a credential, intentional dummy value for timing normalization
var dummyHash = []byte("$2a$12$000000000000000000000uGWpQ7c0AqnqcP6EehRw.4.FBgm6KKSK")

// Config holds break-glass service configuration.
type Config struct {
	DefaultTTL time.Duration
	MaxTTL     time.Duration
	MinTTL     time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		DefaultTTL: 30 * time.Minute,
		MaxTTL:     24 * time.Hour,
		MinTTL:     5 * time.Minute,
	}
}

// Service handles break-glass token operations.
type Service struct {
	repo Repository
	cfg  Config
}

// NewService creates a new break-glass service.
func NewService(repo Repository, cfg Config) *Service {
	return ptrext.Of(Service{repo: repo, cfg: cfg})
}

// IssueInput is the input for issuing a break-glass token.
type IssueInput struct {
	TenantID   string
	AdminEmail string
	TTL        time.Duration
	IssuedBy   string
	AllowedIPs []string // CIDR ranges that can use this token; nil = no restriction
}

// IssueResult is the result of issuing a break-glass token.
type IssueResult struct {
	Token     domain.BreakGlassToken
	RawToken  string // plaintext token, shown only once
	LoginURL  string // convenience URL for login
	ExpiresAt time.Time
}

// Issue creates a new break-glass token.
func (s *Service) Issue(ctx context.Context, input IssueInput) (IssueResult, error) {
	const where = "breakglass.Service.Issue"

	ttl := input.TTL
	if ttl <= 0 {
		ttl = s.cfg.DefaultTTL
	}
	if ttl < s.cfg.MinTTL {
		return IssueResult{}, fmt.Errorf("TTL %v is below minimum %v", ttl, s.cfg.MinTTL)
	}
	if ttl > s.cfg.MaxTTL {
		return IssueResult{}, fmt.Errorf("TTL %v exceeds maximum %v", ttl, s.cfg.MaxTTL)
	}

	rawToken, err := generateToken()
	if err != nil {
		logext.Errorf(ctx, "[%s] token generation failed,err:%s", where, err.Error())
		return IssueResult{}, fmt.Errorf("token generation failed: %w", err)
	}

	hash, err := hashToken(rawToken)
	if err != nil {
		logext.Errorf(ctx, "[%s] token hashing failed,err:%s", where, err.Error())
		return IssueResult{}, fmt.Errorf("token hashing failed: %w", err)
	}

	expiresAt := time.Now().Add(ttl)

	token, err := s.repo.Issue(ctx, breakglass.NewToken{
		TenantID:   input.TenantID,
		AdminEmail: input.AdminEmail,
		TokenHash:  hash,
		ExpiresAt:  expiresAt,
		IssuedBy:   input.IssuedBy,
		AllowedIPs: input.AllowedIPs,
	})
	if err != nil {
		return IssueResult{}, err
	}

	ipInfo := "any"
	if len(input.AllowedIPs) > 0 {
		ipInfo = fmt.Sprintf("%v", input.AllowedIPs)
	}
	logext.Infof(ctx, "[%s] issued token,id:%s,admin:%s,expires:%s,issued_by:%s,allowed_ips:%s",
		where, token.ID, input.AdminEmail, expiresAt.Format(time.RFC3339), input.IssuedBy, ipInfo)

	return IssueResult{
		Token:     token,
		RawToken:  rawToken,
		ExpiresAt: expiresAt,
	}, nil
}

// ValidateInput is the input for validating a break-glass token.
type ValidateInput struct {
	TenantID   string
	AdminEmail string
	RawToken   string
	ClientIP   string // checked against allowed IPs; recorded in audit
}

// Validate checks if a token is valid for login and marks it as used.
func (s *Service) Validate(ctx context.Context, input ValidateInput) (domain.BreakGlassToken, error) {
	const where = "breakglass.Service.Validate"

	tokens, err := s.repo.ListValidForAdmin(ctx, input.TenantID, input.AdminEmail)
	if err != nil {
		return domain.BreakGlassToken{}, err
	}

	for _, t := range tokens {
		if verifyToken(input.RawToken, t.TokenHash) {
			// Check IP whitelist before consuming the token.
			if !t.IsIPAllowed(input.ClientIP) {
				logext.Warnf(ctx, "[%s] IP not allowed,id:%s,client_ip:%s,allowed:%v", where, t.ID, input.ClientIP, t.AllowedIPs)
				return domain.BreakGlassToken{}, breakglass.ErrIPNotAllowed
			}

			if err := s.repo.MarkUsed(ctx, input.TenantID, t.ID, input.ClientIP); err != nil {
				if errors.Is(err, breakglass.ErrAlreadyUsed) {
					logext.Warnf(ctx, "[%s] token race: already used,id:%s", where, t.ID)
					continue
				}
				return domain.BreakGlassToken{}, err
			}
			logext.Infof(ctx, "[%s] token validated and consumed,id:%s,admin:%s,ip:%s", where, t.ID, input.AdminEmail, input.ClientIP)
			return t, nil
		}
	}

	logext.Warnf(ctx, "[%s] no valid token found,admin:%s", where, input.AdminEmail)
	return domain.BreakGlassToken{}, breakglass.ErrNotFound
}

// ValidateByToken validates a token without knowing the admin email upfront.
// Used for the break-glass login endpoint where only the token is provided.
// The clientIP is checked against the token's allowed IP list and recorded.
func (s *Service) ValidateByToken(ctx context.Context, tenantID, rawToken, clientIP string) (domain.BreakGlassToken, error) {
	const where = "breakglass.Service.ValidateByToken"

	tokens, err := s.repo.ListAll(ctx, tenantID, 1000)
	if err != nil {
		return domain.BreakGlassToken{}, err
	}

	now := time.Now()
	for _, t := range tokens {
		if !t.IsValid(now) {
			continue
		}
		if verifyToken(rawToken, t.TokenHash) {
			// Check IP whitelist before consuming the token.
			if !t.IsIPAllowed(clientIP) {
				logext.Warnf(ctx, "[%s] IP not allowed,id:%s,client_ip:%s,allowed:%v", where, t.ID, clientIP, t.AllowedIPs)
				return domain.BreakGlassToken{}, breakglass.ErrIPNotAllowed
			}

			if err := s.repo.MarkUsed(ctx, tenantID, t.ID, clientIP); err != nil {
				if errors.Is(err, breakglass.ErrAlreadyUsed) {
					logext.Warnf(ctx, "[%s] token race: already used,id:%s", where, t.ID)
					continue
				}
				return domain.BreakGlassToken{}, err
			}
			logext.Infof(ctx, "[%s] token validated and consumed,id:%s,admin:%s,ip:%s", where, t.ID, t.AdminEmail, clientIP)
			return t, nil
		}
	}

	// Timing normalization: always run bcrypt comparison even when no valid
	// token is found, to prevent timing side-channel attacks that could reveal
	// whether valid tokens exist for a tenant.
	_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(rawToken))

	logext.Warnf(ctx, "[%s] no valid token found", where)
	return domain.BreakGlassToken{}, breakglass.ErrNotFound
}

// Revoke revokes a token by ID.
func (s *Service) Revoke(ctx context.Context, tenantID, tokenID, revokedBy string) error {
	const where = "breakglass.Service.Revoke"
	if err := s.repo.Revoke(ctx, tenantID, tokenID, revokedBy); err != nil {
		return err
	}
	logext.Infof(ctx, "[%s] token revoked,id:%s,revoked_by:%s", where, tokenID, revokedBy)
	return nil
}

// RevokeAll revokes all valid tokens for a tenant. Returns the number revoked.
func (s *Service) RevokeAll(ctx context.Context, tenantID, revokedBy string) (int64, error) {
	const where = "breakglass.Service.RevokeAll"
	n, err := s.repo.RevokeAll(ctx, tenantID, revokedBy)
	if err != nil {
		return 0, err
	}
	if n > 0 {
		logext.Infof(ctx, "[%s] revoked %d tokens,tenant:%s,revoked_by:%s", where, n, tenantID, revokedBy)
	}
	return n, nil
}

// List returns all tokens for a tenant.
func (s *Service) List(ctx context.Context, tenantID string) ([]domain.BreakGlassToken, error) {
	return s.repo.ListAll(ctx, tenantID, 100)
}

// CountValid returns the count of valid tokens for a tenant.
func (s *Service) CountValid(ctx context.Context, tenantID string) (int, error) {
	return s.repo.CountValid(ctx, tenantID)
}

// Cleanup removes expired tokens older than the cutoff.
func (s *Service) Cleanup(ctx context.Context, cutoff time.Duration) (int64, error) {
	const where = "breakglass.Service.Cleanup"
	n, err := s.repo.Cleanup(ctx, time.Now().Add(-cutoff))
	if err != nil {
		return 0, err
	}
	if n > 0 {
		logext.Infof(ctx, "[%s] cleaned up %d expired tokens", where, n)
	}
	return n, nil
}

// generateToken creates a new random token with the bg_ prefix.
func generateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// hashToken creates a bcrypt hash of a token.
func hashToken(token string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(token), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// verifyToken checks if a token matches a hash.
func verifyToken(token, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(token)) == nil
}
