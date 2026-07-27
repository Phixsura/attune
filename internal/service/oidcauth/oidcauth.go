// SPDX-License-Identifier: Apache-2.0

// Package oidcauth provides OIDC authentication business logic.
package oidcauth

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"golang.org/x/oauth2"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/config"
	"github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

var tracer = otel.Tracer("attune/oidcauth")

const (
	maxGroupNameLen      = 256
	maxGroupsCount       = 100
	discoveryTimeout     = 30 * time.Second
	tokenExchangeTimeout = 15 * time.Second
	userInfoTimeout      = 10 * time.Second
)

// otelHTTPClient returns an HTTP client instrumented with OpenTelemetry.
var otelHTTPClient = ptrext.Of(http.Client{
	Transport: otelhttp.NewTransport(http.DefaultTransport),
})

// UserStore defines persistence operations for OIDC users.
type UserStore interface {
	GetByExternalID(ctx context.Context, provider, externalID string) (domain.OIDCUser, error)
	Upsert(ctx context.Context, u domain.OIDCUser) (domain.OIDCUser, error)
}

// TenantResolver finds the default tenant for new users.
type TenantResolver interface {
	FirstActiveID(ctx context.Context) (string, error)
}

// MembershipStore syncs OIDC users into tenant-scoped RBAC membership.
type MembershipStore interface {
	EnsureOIDCMember(ctx context.Context, tenantID, userID string, role domain.Role) error
}

// Service handles OIDC authentication business logic.
type Service struct {
	cfg         *config.OIDCConfig
	provider    *oidc.Provider
	verifier    *oidc.IDTokenVerifier
	oauth2Cfg   *oauth2.Config
	users       UserStore
	tenants     TenantResolver
	memberships MembershipStore
}

// NewService initializes OIDC provider via discovery.
func NewService(ctx context.Context, cfg *config.OIDCConfig, users UserStore, tenants TenantResolver, memberships MembershipStore) (*Service, error) {
	const where = "oidcauth.NewService"

	if !cfg.Enabled {
		return nil, nil
	}

	// Discovery with timeout and OTel-instrumented HTTP client
	discoveryCtx, cancel := context.WithTimeout(ctx, discoveryTimeout)
	defer cancel()

	// Inject OTel-instrumented HTTP client for all OIDC/OAuth2 operations
	discoveryCtx = oidc.ClientContext(discoveryCtx, otelHTTPClient)

	if cfg.InsecureSkipVerify {
		discoveryCtx = oidc.InsecureIssuerURLContext(discoveryCtx, cfg.IssuerURL)
	}

	provider, err := oidc.NewProvider(discoveryCtx, cfg.IssuerURL)
	if err != nil {
		logext.Errorf(ctx, "[%s] discovery failed,issuer:%s,err:%s", where, cfg.IssuerURL, err.Error())
		return nil, fmt.Errorf("oidc discovery failed: %w", err)
	}

	logext.Infof(ctx, "[%s] provider initialized,issuer:%s", where, cfg.IssuerURL)

	verifier := provider.Verifier(ptrext.Of(oidc.Config{
		ClientID:        cfg.ClientID,
		SkipIssuerCheck: cfg.SkipIssuerCheck,
	}))

	oauth2Cfg := ptrext.Of(oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURI,
		Endpoint:     provider.Endpoint(),
		Scopes:       cfg.Scopes,
	})

	return ptrext.Of(Service{
		cfg:         cfg,
		provider:    provider,
		verifier:    verifier,
		oauth2Cfg:   oauth2Cfg,
		users:       users,
		tenants:     tenants,
		memberships: memberships,
	}), nil
}

// AuthCodeURL generates the authorization URL with PKCE and nonce.
func (s *Service) AuthCodeURL(state, verifier, nonce string) string {
	return s.oauth2Cfg.AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", nonce),
	)
}

// ExchangeAndVerify exchanges code for tokens and verifies the ID token.
func (s *Service) ExchangeAndVerify(ctx context.Context, code, verifier, expectedNonce string) (*domain.OIDCClaims, error) {
	const where = "oidcauth.Service.ExchangeAndVerify"

	ctx, span := tracer.Start(ctx, "oidc.exchange_and_verify")
	defer span.End()

	// Token exchange with timeout and OTel-instrumented HTTP client
	exchangeCtx, cancel := context.WithTimeout(ctx, tokenExchangeTimeout)
	defer cancel()
	exchangeCtx = oidc.ClientContext(exchangeCtx, otelHTTPClient)

	exchangeStart := time.Now()
	_, exchangeSpan := tracer.Start(exchangeCtx, "oidc.token_exchange")

	token, err := s.oauth2Cfg.Exchange(
		exchangeCtx,
		code,
		oauth2.VerifierOption(verifier),
	)

	metrics.OIDCTokenExchangeDuration.Observe(time.Since(exchangeStart).Seconds())
	exchangeSpan.End()

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "token exchange failed")
		metrics.OIDCLoginTotal.WithLabelValues("token_exchange_failed").Inc()
		logext.Errorf(ctx, "[%s] token exchange failed,err:%s", where, err.Error())
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}

	// Extract ID token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		metrics.OIDCLoginTotal.WithLabelValues("no_id_token").Inc()
		logext.Errorf(ctx, "[%s] no id_token in response", where)
		return nil, errors.New("no id_token in response")
	}

	// Verify ID token
	_, verifySpan := tracer.Start(ctx, "oidc.verify_id_token")
	idToken, err := s.verifier.Verify(ctx, rawIDToken)
	verifySpan.End()

	if err != nil {
		span.RecordError(err)
		metrics.OIDCLoginTotal.WithLabelValues("id_token_invalid").Inc()
		logext.Errorf(ctx, "[%s] id_token verification failed,err:%s", where, err.Error())
		return nil, fmt.Errorf("id_token verification failed: %w", err)
	}

	// Verify nonce (replay protection) - constant-time comparison
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(expectedNonce)) != 1 {
		metrics.OIDCLoginTotal.WithLabelValues("nonce_mismatch").Inc()
		logext.Warnf(ctx, "[%s] nonce mismatch", where)
		return nil, errors.New("nonce mismatch")
	}

	// Extract claims
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		logext.Errorf(ctx, "[%s] claims extraction failed,err:%s", where, err.Error())
		return nil, fmt.Errorf("claims extraction failed: %w", err)
	}

	oidcClaims, err := s.extractClaims(ctx, claims, token)
	if err != nil {
		metrics.OIDCLoginTotal.WithLabelValues("claims_invalid").Inc()
		return nil, err
	}

	span.SetAttributes(
		attribute.String("oidc.subject", oidcClaims.Subject),
		attribute.Int("oidc.groups_count", len(oidcClaims.Groups)),
	)

	return oidcClaims, nil
}

// ValidateState compares states using constant-time comparison.
func (s *Service) ValidateState(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// CheckAllowedGroups verifies user is in at least one allowed group.
func (s *Service) CheckAllowedGroups(groups []string) bool {
	if len(s.cfg.AllowedGroups) == 0 {
		return true // no restriction
	}

	groupSet := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		groupSet[g] = struct{}{}
	}

	for _, allowed := range s.cfg.AllowedGroups {
		if _, ok := groupSet[allowed]; ok {
			return true
		}
	}
	return false
}

// MapRole determines user role from groups (ordered matching).
func (s *Service) MapRole(groups []string) string {
	groupSet := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		groupSet[g] = struct{}{}
	}

	// Ordered evaluation
	for _, entry := range s.cfg.RoleMapping {
		for _, g := range entry.Groups {
			if _, ok := groupSet[g]; ok {
				metrics.OIDCRoleMappingTotal.WithLabelValues(entry.Role).Inc()
				return entry.Role
			}
		}
	}

	metrics.OIDCRoleMappingTotal.WithLabelValues("member").Inc()
	return "member" // default
}

// FindOrCreateUser upserts OIDC user and returns the user with ID.
func (s *Service) FindOrCreateUser(ctx context.Context, claims *domain.OIDCClaims, role string) (domain.OIDCUser, error) {
	const where = "oidcauth.Service.FindOrCreateUser"

	user := domain.OIDCUser{
		Provider:    "oidc",
		ExternalID:  claims.Subject,
		Email:       claims.Email,
		DisplayName: claims.Name,
		Role:        role,
		Groups:      claims.Groups,
	}

	result, err := s.users.Upsert(ctx, user)
	if err != nil {
		logext.Errorf(ctx, "[%s] upsert failed,sub:%s,err:%s", where, claims.Subject, err.Error())
		return domain.OIDCUser{}, err
	}

	return result, nil
}

// EnsureMembership syncs the OIDC user into tenant_members before a session is issued.
func (s *Service) EnsureMembership(ctx context.Context, tenantID, userID, role string) error {
	const where = "oidcauth.Service.EnsureMembership"

	if s.memberships == nil {
		return nil
	}
	if tenantID == "" {
		return errors.New("oidc membership tenant_id is required")
	}
	if userID == "" {
		return errors.New("oidc membership user_id is required")
	}

	if err := s.memberships.EnsureOIDCMember(ctx, tenantID, userID, domain.ParseRole(role)); err != nil {
		logext.Errorf(ctx, "[%s] ensure member failed,tenant_id:%s,user_id:%s,err:%s",
			where, tenantID, userID, err.Error())
		return err
	}
	return nil
}

// ResolveDefaultTenant returns the first active tenant ID.
func (s *Service) ResolveDefaultTenant(ctx context.Context) string {
	const where = "oidcauth.Service.ResolveDefaultTenant"

	if s.tenants == nil {
		return ""
	}

	id, err := s.tenants.FirstActiveID(ctx)
	if err != nil {
		logext.Warnf(ctx, "[%s] FirstActiveID failed,err:%s", where, err.Error())
		return ""
	}
	return id
}

// ProviderName returns the configured display name for the IdP.
func (s *Service) ProviderName() string {
	return s.cfg.ProviderName
}

// OIDCOnly returns whether local login should be hidden.
func (s *Service) OIDCOnly() bool {
	return s.cfg.OIDCOnly
}

// extractClaims parses ID token claims into domain struct.
func (s *Service) extractClaims(ctx context.Context, claims map[string]any, token *oauth2.Token) (*domain.OIDCClaims, error) {
	result := ptrext.Of(domain.OIDCClaims{})

	// Subject (required)
	if sub, ok := claims["sub"].(string); ok && sub != "" {
		result.Subject = sub
	} else if _, exists := claims["sub"]; exists {
		return nil, errors.New("sub claim is not a string")
	} else {
		return nil, errors.New("missing sub claim")
	}

	// User identifier (configurable claim)
	if val, ok := claims[s.cfg.UserClaim].(string); ok && val != "" {
		result.Email = val
	} else {
		return nil, fmt.Errorf("missing or invalid %s claim", s.cfg.UserClaim)
	}

	// Display name
	if name, ok := claims["name"].(string); ok {
		result.Name = name
	} else if name, ok := claims["preferred_username"].(string); ok {
		result.Name = name
	} else {
		result.Name = result.Email
	}

	// Groups (may be in ID token or require userinfo call)
	result.Groups = s.extractGroups(ctx, claims, token)

	return result, nil
}

// extractGroups gets groups from ID token or userinfo endpoint.
func (s *Service) extractGroups(ctx context.Context, claims map[string]any, token *oauth2.Token) []string {
	const where = "oidcauth.Service.extractGroups"

	// Try ID token first
	if groups := extractStringSlice(claims, s.cfg.GroupsClaim); len(groups) > 0 {
		return sanitizeGroups(groups)
	}

	// Fallback to userinfo endpoint with timeout and OTel-instrumented HTTP client
	userinfoCtx, cancel := context.WithTimeout(ctx, userInfoTimeout)
	defer cancel()
	userinfoCtx = oidc.ClientContext(userinfoCtx, otelHTTPClient)

	_, span := tracer.Start(userinfoCtx, "oidc.userinfo")
	defer span.End()

	userInfo, err := s.provider.UserInfo(userinfoCtx, oauth2.StaticTokenSource(token))
	if err != nil {
		logext.Warnf(ctx, "[%s] userinfo fallback failed,err:%s", where, err.Error())
		return nil
	}

	var userClaims map[string]any
	if err := userInfo.Claims(&userClaims); err != nil {
		logext.Warnf(ctx, "[%s] userinfo claims parse failed,err:%s", where, err.Error())
		return nil
	}

	return sanitizeGroups(extractStringSlice(userClaims, s.cfg.GroupsClaim))
}

// extractStringSlice extracts string array from claims.
func extractStringSlice(claims map[string]any, key string) []string {
	val, ok := claims[key]
	if !ok {
		return nil
	}

	switch v := val.(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case string:
		return strings.Fields(v)
	default:
		return nil
	}
}

// sanitizeGroups enforces length and count limits.
func sanitizeGroups(groups []string) []string {
	result := make([]string, 0, len(groups))
	for i, g := range groups {
		if i >= maxGroupsCount {
			break
		}
		if len(g) <= maxGroupNameLen {
			result = append(result, g)
		}
	}
	return result
}
