package apikey

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

const (
	rawKeyHexLen   = 32 // 16 random bytes -> 32 hex chars (after prefix)
	displayPrefLen = 12 // chars shown for identification ("fbk_live_abc")
)

// APIKeys issues and verifies external API keys backed by the
// external_api_keys table. Keys are random 128-bit strings stored as
// deterministic lookup digests; the raw value is shown to the operator
// exactly once at issuance.
type APIKeys struct {
	repo *apikeyrepo.APIKeyRepo
	// touchCache debounces TouchLastUsed: if a key was touched within the
	// last touchInterval we skip the goroutine entirely. Prevents unbounded
	// fan-out under load (many concurrent requests authenticating with the
	// same key would otherwise fire N goroutines/sec, each holding a
	// pgxpool connection).
	touchCache sync.Map // map[uuid.UUID]time.Time (last touch)
}

const touchInterval = 30 * time.Second

func NewAPIKeys(r *apikeyrepo.APIKeyRepo) *APIKeys {
	return ptrext.Of(APIKeys{repo: r})
}

// Issue mints a key for the given tenant and returns the raw value
// (caller must surface it once). tenantID is the TEXT id returned by
// TenantRepo.ResolveSlug or Create (UUID stored as text — see
// migration 001).
func (s *APIKeys) Issue(ctx context.Context, tenantID, label string) (raw string, keyID uuid.UUID, err error) {
	const where = "service.APIKeys.Issue"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,label:%s", where, tenantID, label)
	raw, hash, prefix, err := generate()
	if err != nil {
		logext.Errorf(ctx, "[%s] generate failed,err:%+v", where, err.Error())
		return "", uuid.Nil, err
	}
	keyID, err = s.repo.Insert(ctx, tenantID, hash, prefix, label)
	if err != nil {
		logext.Errorf(ctx, "[%s] repo.Insert failed,tenant_id:%s,err:%+v",
			where, tenantID, err.Error())
		return "", uuid.Nil, err
	}
	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s,prefix:%s",
		where, tenantID, keyID, prefix)
	return raw, keyID, nil
}

// List returns every key (active + revoked) for tenantID, newest first.
func (s *APIKeys) List(ctx context.Context, tenantID string) ([]apikeyrepo.APIKeyListRow, error) {
	return s.repo.ListByTenant(ctx, tenantID)
}

// Revoke soft-deletes the key. Pass-through to repo so the tenant_id
// scope check happens at the SQL boundary.
func (s *APIKeys) Revoke(ctx context.Context, tenantID string, id uuid.UUID) error {
	return s.repo.Revoke(ctx, tenantID, id)
}

// Lookup verifies the raw key, returning its tenant and key ids on
// success. Updates last_used_at asynchronously to avoid blocking the
// request. Returns domain.ErrInvalidAPIKey on every recoverable failure
// (wrong length, hash mismatch, no row, revoked); only unexpected DB
// errors propagate as-is so middleware can map them to 500.
func (s *APIKeys) Lookup(ctx context.Context, raw string) (tenantID string, keyID uuid.UUID, err error) {
	const where = "service.APIKeys.Lookup"
	if len(raw) != len(domain.APIKeyPrefix)+rawKeyHexLen {
		logext.Warnf(ctx, "[%s] reject: bad key length,len:%d", where, len(raw))
		return "", uuid.Nil, domain.ErrInvalidAPIKey
	}
	digest := apiKeyLookupDigest(raw)
	row, err := s.repo.LookupByHash(ctx, digest)
	if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
		logext.Warnf(ctx, "[%s] reject: hash not found", where)
		return "", uuid.Nil, domain.ErrInvalidAPIKey
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] apikey.LookupByHash failed,err:%+v", where, err.Error())
		return "", uuid.Nil, err
	}
	if !hmac.Equal(row.StoredHash, digest) {
		logext.Warnf(ctx, "[%s] reject: hmac mismatch,key_id:%s", where, row.ID)
		return "", uuid.Nil, domain.ErrInvalidAPIKey
	}
	s.touchAsync(row.ID)
	return row.TenantID, row.ID, nil
}

// LookupWithScopes verifies the raw key and returns tenant, key ID, and scopes
// atomically. If scope loading fails, returns domain.ErrInvalidAPIKey (fail-closed).
// Deprecated: Use LookupWithScopesAndIP for IP allowlist support.
func (s *APIKeys) LookupWithScopes(ctx context.Context, raw string) (tenantID string, keyID uuid.UUID, scopes []domain.Scope, err error) {
	return s.LookupWithScopesAndIP(ctx, raw, "")
}

// LookupWithScopesAndIP verifies the raw key with expiration and IP allowlist checks.
// Returns tenant, key ID, scopes, and rate limit on success.
func (s *APIKeys) LookupWithScopesAndIP(ctx context.Context, raw, clientIP string) (tenantID string, keyID uuid.UUID, scopes []domain.Scope, err error) {
	const where = "service.APIKeys.LookupWithScopesAndIP"
	if len(raw) != len(domain.APIKeyPrefix)+rawKeyHexLen {
		logext.Warnf(ctx, "[%s] reject: bad key length,len:%d", where, len(raw))
		return "", uuid.Nil, nil, domain.ErrInvalidAPIKey
	}
	digest := apiKeyLookupDigest(raw)
	row, err := s.repo.LookupByHash(ctx, digest)
	if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
		logext.Warnf(ctx, "[%s] reject: hash not found", where)
		return "", uuid.Nil, nil, domain.ErrInvalidAPIKey
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] LookupByHash failed,err:%+v", where, err.Error())
		return "", uuid.Nil, nil, err
	}
	if !hmac.Equal(row.StoredHash, digest) {
		logext.Warnf(ctx, "[%s] reject: hmac mismatch,key_id:%s", where, row.ID)
		return "", uuid.Nil, nil, domain.ErrInvalidAPIKey
	}

	if row.ExpiresAt != nil && time.Now().After(ptrext.Indirect(row.ExpiresAt)) {
		logext.Warnf(ctx, "[%s] reject: key expired,key_id:%s,expires_at:%s", where, row.ID, row.ExpiresAt)
		return "", uuid.Nil, nil, domain.ErrAPIKeyExpired
	}

	if len(row.AllowedCIDRs) > 0 && clientIP != "" {
		if !domain.IPAllowedStrings(row.AllowedCIDRs, clientIP) {
			logext.Warnf(ctx, "[%s] reject: IP not allowed,key_id:%s,client_ip:%s", where, row.ID, clientIP)
			return "", uuid.Nil, nil, domain.ErrIPNotAllowed
		}
	}

	scopes, err = s.repo.GetScopes(ctx, row.ID)
	if err != nil {
		logext.Errorf(ctx, "[%s] GetScopes failed,key_id:%s,err:%+v", where, row.ID, err.Error())
		return "", uuid.Nil, nil, domain.ErrInvalidAPIKey
	}

	s.touchAsync(row.ID)
	return row.TenantID, row.ID, scopes, nil
}

// LookupResult contains all key details for verification API.
type LookupResult struct {
	TenantID     string
	KeyID        uuid.UUID
	Scopes       []domain.Scope
	RateLimitRPM *int
	ExpiresAt    *time.Time
}

// LookupFull returns complete key details including rate limit info.
func (s *APIKeys) LookupFull(ctx context.Context, raw, clientIP string) (*LookupResult, error) {
	const where = "service.APIKeys.LookupFull"
	if len(raw) != len(domain.APIKeyPrefix)+rawKeyHexLen {
		logext.Warnf(ctx, "[%s] reject: bad key length,len:%d", where, len(raw))
		return nil, domain.ErrInvalidAPIKey
	}
	digest := apiKeyLookupDigest(raw)
	row, err := s.repo.LookupByHash(ctx, digest)
	if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
		logext.Warnf(ctx, "[%s] reject: hash not found", where)
		return nil, domain.ErrInvalidAPIKey
	}
	if err != nil {
		logext.Errorf(ctx, "[%s] LookupByHash failed,err:%+v", where, err.Error())
		return nil, err
	}
	if !hmac.Equal(row.StoredHash, digest) {
		logext.Warnf(ctx, "[%s] reject: hmac mismatch,key_id:%s", where, row.ID)
		return nil, domain.ErrInvalidAPIKey
	}

	if row.ExpiresAt != nil && time.Now().After(ptrext.Indirect(row.ExpiresAt)) {
		logext.Warnf(ctx, "[%s] reject: key expired,key_id:%s,expires_at:%s", where, row.ID, row.ExpiresAt)
		return nil, domain.ErrAPIKeyExpired
	}

	if len(row.AllowedCIDRs) > 0 && clientIP != "" {
		if !domain.IPAllowedStrings(row.AllowedCIDRs, clientIP) {
			logext.Warnf(ctx, "[%s] reject: IP not allowed,key_id:%s,client_ip:%s", where, row.ID, clientIP)
			return nil, domain.ErrIPNotAllowed
		}
	}

	scopes, err := s.repo.GetScopes(ctx, row.ID)
	if err != nil {
		logext.Errorf(ctx, "[%s] GetScopes failed,key_id:%s,err:%+v", where, row.ID, err.Error())
		return nil, domain.ErrInvalidAPIKey
	}

	s.touchAsync(row.ID)
	return ptrext.Of(LookupResult{
		TenantID:     row.TenantID,
		KeyID:        row.ID,
		Scopes:       scopes,
		RateLimitRPM: row.RateLimitRPM,
		ExpiresAt:    row.ExpiresAt,
	}), nil
}

// IssueWithScopes mints a key with specific scopes atomically.
func (s *APIKeys) IssueWithScopes(ctx context.Context, tenantID, label string, scopes []domain.Scope) (raw string, keyID uuid.UUID, err error) {
	return s.IssueFullWithScopes(ctx, IssueParams{
		TenantID: tenantID,
		Label:    label,
	}, scopes)
}

// IssueParams contains all parameters for creating a new API key.
type IssueParams struct {
	TenantID     string
	Label        string
	Description  string
	ExpiresAt    *time.Time
	AllowedCIDRs []string
	RateLimitRPM *int
}

// IssueFullWithScopes mints a key with all security options and scopes atomically.
func (s *APIKeys) IssueFullWithScopes(ctx context.Context, p IssueParams, scopes []domain.Scope) (raw string, keyID uuid.UUID, err error) {
	const where = "service.APIKeys.IssueFullWithScopes"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,label:%s,scopes:%d,expires:%v,cidrs:%d,rate_limit:%v",
		where, p.TenantID, p.Label, len(scopes), p.ExpiresAt != nil, len(p.AllowedCIDRs), p.RateLimitRPM)

	raw, hash, prefix, err := generate()
	if err != nil {
		logext.Errorf(ctx, "[%s] generate failed,err:%+v", where, err.Error())
		return "", uuid.Nil, err
	}
	keyID, err = s.repo.InsertFullWithScopes(ctx, apikeyrepo.APIKeyInsertParams{
		TenantID:     p.TenantID,
		Hash:         hash,
		Prefix:       prefix,
		Label:        p.Label,
		Description:  p.Description,
		ExpiresAt:    p.ExpiresAt,
		AllowedCIDRs: p.AllowedCIDRs,
		RateLimitRPM: p.RateLimitRPM,
	}, scopes)
	if err != nil {
		logext.Errorf(ctx, "[%s] InsertFullWithScopes failed,tenant_id:%s,err:%+v",
			where, p.TenantID, err.Error())
		return "", uuid.Nil, err
	}

	logext.Infof(ctx, "[%s] OK,tenant_id:%s,key_id:%s,prefix:%s,scopes:%d",
		where, p.TenantID, keyID, prefix, len(scopes))
	return raw, keyID, nil
}

// GetByID returns key details by ID (for verify API).
func (s *APIKeys) GetByID(ctx context.Context, tenantID string, keyID uuid.UUID) (*apikeyrepo.APIKeyListRow, error) {
	return s.repo.GetByID(ctx, tenantID, keyID)
}

// GetScopes returns scopes for a given key ID.
func (s *APIKeys) GetScopes(ctx context.Context, keyID uuid.UUID) ([]domain.Scope, error) {
	return s.repo.GetScopes(ctx, keyID)
}

// GetScopesBatch returns scopes for multiple keys in a single query.
func (s *APIKeys) GetScopesBatch(ctx context.Context, keyIDs []uuid.UUID) (map[uuid.UUID][]domain.Scope, error) {
	return s.repo.GetScopesBatch(ctx, keyIDs)
}

// touchAsync debounces s.repo.TouchLastUsed: skips the goroutine if this
// key was touched within touchInterval (30s). Trades small accuracy on
// last_used_at for bounded fan-out under heavy auth load.
func (s *APIKeys) touchAsync(id uuid.UUID) {
	now := time.Now()
	if last, ok := s.touchCache.Load(id); ok {
		if t, _ := last.(time.Time); now.Sub(t) < touchInterval {
			return
		}
	}
	s.touchCache.Store(id, now)
	go s.repo.TouchLastUsed(id)
}

// RotateParams contains parameters for rotating an API key.
type RotateParams struct {
	TenantID    string
	OldKeyID    uuid.UUID
	GracePeriod time.Duration
}

// Rotate creates a new key and puts the old key in grace period.
// During grace period, both keys are valid.
func (s *APIKeys) Rotate(ctx context.Context, p RotateParams) (newRaw string, newKeyID uuid.UUID, err error) {
	const where = "service.APIKeys.Rotate"
	logext.Infof(ctx, "[%s] start,tenant_id:%s,old_key_id:%s,grace_period:%s",
		where, p.TenantID, p.OldKeyID, p.GracePeriod)

	scopes, err := s.repo.GetScopes(ctx, p.OldKeyID)
	if err != nil {
		logext.Errorf(ctx, "[%s] GetScopes failed,err:%+v", where, err.Error())
		return "", uuid.Nil, err
	}

	newRaw, hash, prefix, err := generate()
	if err != nil {
		logext.Errorf(ctx, "[%s] generate failed,err:%+v", where, err.Error())
		return "", uuid.Nil, err
	}

	newKeyID, err = s.repo.Rotate(ctx, p.TenantID, apikeyrepo.RotateParams{
		OldKeyID:    p.OldKeyID,
		NewHash:     hash,
		NewPrefix:   prefix,
		GracePeriod: p.GracePeriod,
	}, scopes)
	if err != nil {
		logext.Errorf(ctx, "[%s] Rotate failed,err:%+v", where, err.Error())
		return "", uuid.Nil, err
	}

	_ = s.repo.RecordEvent(ctx, p.TenantID, p.OldKeyID, string(domain.APIKeyEventRotated), map[string]any{
		"new_key_id":   newKeyID.String(),
		"grace_period": p.GracePeriod.String(),
	})

	logext.Infof(ctx, "[%s] OK,old_key_id:%s,new_key_id:%s", where, p.OldKeyID, newKeyID)
	return newRaw, newKeyID, nil
}

// ExpireGracePeriodKeys marks keys as inactive after their grace period ends.
func (s *APIKeys) ExpireGracePeriodKeys(ctx context.Context) (int64, error) {
	return s.repo.ExpireGracePeriodKeys(ctx)
}

// RecordRequestLog records an API request for audit purposes.
func (s *APIKeys) RecordRequestLog(ctx context.Context, e apikeyrepo.RequestLogEntry) {
	if err := s.repo.InsertRequestLog(ctx, e); err != nil {
		logext.Warnf(ctx, "[service.APIKeys.RecordRequestLog] failed,key_id:%s,err:%+v", e.KeyID, err.Error())
	}
}

// ListRequestLogs returns recent request logs for a key.
func (s *APIKeys) ListRequestLogs(ctx context.Context, tenantID string, keyID uuid.UUID, limit int) ([]apikeyrepo.RequestLogRow, error) {
	return s.repo.ListRequestLogs(ctx, tenantID, keyID, limit)
}

// UpdateEnvironment changes the environment tag for a key.
func (s *APIKeys) UpdateEnvironment(ctx context.Context, tenantID string, keyID uuid.UUID, env string) error {
	if !domain.IsValidEnvironment(env) {
		return fmt.Errorf("invalid environment: %s", env)
	}
	return s.repo.UpdateEnvironment(ctx, tenantID, keyID, env)
}

// CreateServiceAccount creates a new service account.
func (s *APIKeys) CreateServiceAccount(ctx context.Context, tenantID, name, description string) (uuid.UUID, error) {
	return s.repo.CreateServiceAccount(ctx, tenantID, name, description)
}

// ListServiceAccounts returns all service accounts for a tenant.
func (s *APIKeys) ListServiceAccounts(ctx context.Context, tenantID string) ([]apikeyrepo.ServiceAccountRow, error) {
	return s.repo.ListServiceAccounts(ctx, tenantID)
}

// LinkKeyToServiceAccount associates an API key with a service account.
func (s *APIKeys) LinkKeyToServiceAccount(ctx context.Context, tenantID string, keyID, serviceAccountID uuid.UUID) error {
	return s.repo.LinkKeyToServiceAccount(ctx, tenantID, keyID, serviceAccountID)
}

// AddResourceBinding binds a key to a specific resource.
func (s *APIKeys) AddResourceBinding(ctx context.Context, keyID uuid.UUID, resourceType, resourceID string) error {
	return s.repo.AddResourceBinding(ctx, keyID, resourceType, resourceID)
}

// ListResourceBindings returns all resource bindings for a key.
func (s *APIKeys) ListResourceBindings(ctx context.Context, keyID uuid.UUID) ([]apikeyrepo.ResourceBinding, error) {
	return s.repo.ListResourceBindings(ctx, keyID)
}

// CheckResourceAccess checks if a key has access to a resource.
// Returns true if key has no bindings (unrestricted) or has a matching binding.
func (s *APIKeys) CheckResourceAccess(ctx context.Context, keyID uuid.UUID, resourceType, resourceID string) (bool, error) {
	hasBindings, err := s.repo.HasAnyResourceBindings(ctx, keyID)
	if err != nil {
		return false, err
	}
	if !hasBindings {
		return true, nil
	}
	return s.repo.CheckResourceBinding(ctx, keyID, resourceType, resourceID)
}

// CreateEventSubscription creates a new event webhook subscription.
func (s *APIKeys) CreateEventSubscription(ctx context.Context, tenantID string, eventTypes []string, webhookURL, secret string) (uuid.UUID, error) {
	return s.repo.CreateEventSubscription(ctx, tenantID, eventTypes, webhookURL, secret)
}

// ListEventSubscriptions returns all event subscriptions for a tenant.
func (s *APIKeys) ListEventSubscriptions(ctx context.Context, tenantID string) ([]apikeyrepo.EventSubscription, error) {
	return s.repo.ListEventSubscriptions(ctx, tenantID)
}

// GetSubscriptionsForEvent returns active subscriptions that include the given event type.
func (s *APIKeys) GetSubscriptionsForEvent(ctx context.Context, tenantID, eventType string) ([]apikeyrepo.EventSubscription, error) {
	return s.repo.GetSubscriptionsForEvent(ctx, tenantID, eventType)
}

// RecordEvent records an API key lifecycle event.
func (s *APIKeys) RecordEvent(ctx context.Context, tenantID string, keyID uuid.UUID, eventType string, payload map[string]any) error {
	return s.repo.RecordEvent(ctx, tenantID, keyID, eventType, payload)
}

// GetExpiringKeys returns keys expiring within the given duration.
func (s *APIKeys) GetExpiringKeys(ctx context.Context, within time.Duration) ([]apikeyrepo.APIKeyListRow, error) {
	return s.repo.GetExpiringKeys(ctx, within)
}

// RecordLeakDetection records a detected key leak.
func (s *APIKeys) RecordLeakDetection(ctx context.Context, keyID uuid.UUID, tenantID, source, sourceURL string) error {
	return s.repo.RecordLeakDetection(ctx, keyID, tenantID, source, sourceURL)
}

// GetUnresolvedLeaks returns unresolved leak detections for a tenant.
func (s *APIKeys) GetUnresolvedLeaks(ctx context.Context, tenantID string) ([]apikeyrepo.LeakDetection, error) {
	return s.repo.GetUnresolvedLeaks(ctx, tenantID)
}

// ResolveLeakDetection marks a leak as resolved.
func (s *APIKeys) ResolveLeakDetection(ctx context.Context, leakID uuid.UUID, resolution string) error {
	return s.repo.ResolveLeakDetection(ctx, leakID, resolution)
}

// generate is the random-key + lookup-digest + display-prefix construction.
// Lives here rather than in repo because the raw value never leaves service;
// repo only sees the digest.
func generate() (raw string, hash []byte, prefix string, err error) {
	buf := make([]byte, rawKeyHexLen/2)
	if _, err = rand.Read(buf); err != nil {
		return "", nil, "", fmt.Errorf("rand: %w", err)
	}
	raw = domain.APIKeyPrefix + hex.EncodeToString(buf)
	hash = apiKeyLookupDigest(raw)
	if len(raw) >= displayPrefLen {
		prefix = raw[:displayPrefLen]
	} else {
		prefix = raw
	}
	return raw, hash, prefix, nil
}

func apiKeyLookupDigest(raw string) []byte {
	// The input is a 128-bit CSPRNG API token used for deterministic DB
	// lookup, not a low-entropy password. Preimage resistance of SHA-256 is
	// sufficient for this indexed lookup digest.

	// codeql[go/weak-sensitive-data-hashing]
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// GetPolicy returns the org-level API key policy.
func (s *APIKeys) GetPolicy(ctx context.Context, tenantID string) (*apikeyrepo.Policy, error) {
	return s.repo.GetPolicy(ctx, tenantID)
}

// UpsertPolicy creates or updates the org-level policy.
func (s *APIKeys) UpsertPolicy(ctx context.Context, policy apikeyrepo.Policy) error {
	return s.repo.UpsertPolicy(ctx, policy)
}

// ListProjects returns all projects for a tenant.
func (s *APIKeys) ListProjects(ctx context.Context, tenantID string) ([]apikeyrepo.Project, error) {
	return s.repo.ListProjects(ctx, tenantID)
}

// CreateProject creates a new project.
func (s *APIKeys) CreateProject(ctx context.Context, tenantID, name, description string) (*apikeyrepo.Project, error) {
	id, err := s.repo.CreateProject(ctx, tenantID, name, description)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return ptrext.Of(apikeyrepo.Project{
		ID:          id,
		TenantID:    tenantID,
		Name:        name,
		Description: description,
		IsActive:    true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}), nil
}

// BindKeyToProject binds an API key to a project.
func (s *APIKeys) BindKeyToProject(ctx context.Context, tenantID string, keyID, projectID uuid.UUID) error {
	return s.repo.BindKeyToProject(ctx, tenantID, keyID, projectID)
}

// GetKeyTags returns tags for an API key.
func (s *APIKeys) GetKeyTags(ctx context.Context, tenantID string, keyID uuid.UUID) ([]apikeyrepo.Tag, error) {
	return s.repo.GetTags(ctx, keyID)
}

// SetKeyTags sets tags for an API key.
func (s *APIKeys) SetKeyTags(ctx context.Context, tenantID string, keyID uuid.UUID, tags []apikeyrepo.Tag) error {
	for _, t := range tags {
		if err := s.repo.SetTag(ctx, keyID, t.Key, t.Value); err != nil {
			return err
		}
	}
	return nil
}

// SetKeyBudget sets budget for an API key.
func (s *APIKeys) SetKeyBudget(ctx context.Context, tenantID string, keyID uuid.UUID, limitUSD float64, overageAction string) error {
	return s.repo.SetBudget(ctx, tenantID, keyID, limitUSD, nil)
}

// TempTokenResult contains the result of creating a temporary token.
type TempTokenResult struct {
	Token       string
	TokenPrefix string
	ExpiresAt   time.Time
}

// CreateTempToken creates a temporary token from a parent key.
func (s *APIKeys) CreateTempToken(ctx context.Context, tenantID string, parentKeyID uuid.UUID, expiresIn time.Duration, maxUses *int, purpose string) (*apikeyrepo.TempTokenResult, error) {
	const where = "service.APIKeys.CreateTempToken"

	raw, hash, prefix, err := generate()
	if err != nil {
		logext.Errorf(ctx, "[%s] generate failed,err:%+v", where, err.Error())
		return nil, err
	}

	expiresAt := time.Now().Add(expiresIn)
	_, err = s.repo.CreateTempToken(ctx, apikeyrepo.TempToken{
		ParentKeyID: parentKeyID,
		TenantID:    tenantID,
		TokenHash:   hash,
		TokenPrefix: prefix,
		Purpose:     purpose,
		ExpiresAt:   expiresAt,
		MaxUses:     maxUses,
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] CreateTempToken failed,err:%+v", where, err.Error())
		return nil, err
	}

	return ptrext.Of(apikeyrepo.TempTokenResult{
		Token:       raw,
		TokenPrefix: prefix,
		ExpiresAt:   expiresAt,
	}), nil
}

// ListApprovalRequests returns approval requests for a tenant.
func (s *APIKeys) ListApprovalRequests(ctx context.Context, tenantID, status string) ([]apikeyrepo.ApprovalRequest, error) {
	if status == "" || status == "pending" {
		return s.repo.ListPendingApprovals(ctx, tenantID)
	}
	return s.repo.ListApprovalsByStatus(ctx, tenantID, status)
}

// CreateApprovalRequest creates a new approval request.
func (s *APIKeys) CreateApprovalRequest(ctx context.Context, req apikeyrepo.ApprovalRequest) (*apikeyrepo.ApprovalRequest, error) {
	id, err := s.repo.CreateApprovalRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	req.ID = id
	req.Status = "pending"
	req.CreatedAt = time.Now()
	req.ExpiresAt = time.Now().Add(7 * 24 * time.Hour)
	return ptrext.Of(req), nil
}

// ReviewApproval approves or rejects an approval request.
func (s *APIKeys) ReviewApproval(ctx context.Context, tenantID string, requestID uuid.UUID, reviewerID string, approve bool, notes string) (*apikeyrepo.ApprovalRequest, error) {
	if err := s.repo.ReviewApproval(ctx, requestID, reviewerID, approve, notes); err != nil {
		return nil, err
	}
	return s.repo.GetApprovalRequest(ctx, requestID)
}

// ListOAuth2Clients returns OAuth2 clients for a tenant.
func (s *APIKeys) ListOAuth2Clients(ctx context.Context, tenantID string) ([]apikeyrepo.OAuth2Client, error) {
	return s.repo.ListOAuth2Clients(ctx, tenantID)
}

// CreateOAuth2Client creates a new OAuth2 client and returns the client secret.
func (s *APIKeys) CreateOAuth2Client(ctx context.Context, tenantID, name, description string, redirectURIs, allowedScopes []string) (*apikeyrepo.OAuth2Client, string, error) {
	const where = "service.APIKeys.CreateOAuth2Client"

	clientID := "client_" + uuid.New().String()[:8]
	raw, hash, _, err := generate()
	if err != nil {
		logext.Errorf(ctx, "[%s] generate failed,err:%+v", where, err.Error())
		return nil, "", err
	}

	client := apikeyrepo.OAuth2Client{
		TenantID:         tenantID,
		ClientID:         clientID,
		ClientSecretHash: hash,
		Name:             name,
		Description:      description,
		RedirectURIs:     redirectURIs,
		AllowedScopes:    allowedScopes,
		IsActive:         true,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	id, err := s.repo.CreateOAuth2Client(ctx, client)
	if err != nil {
		logext.Errorf(ctx, "[%s] CreateOAuth2Client failed,err:%+v", where, err.Error())
		return nil, "", err
	}
	client.ID = id

	return ptrext.Of(client), raw, nil
}

// GetKeyAnalytics returns hourly analytics for a key.
func (s *APIKeys) GetKeyAnalytics(ctx context.Context, tenantID string, keyID uuid.UUID, start, end time.Time) ([]apikeyrepo.AnalyticsHourly, error) {
	return s.repo.GetAnalytics(ctx, keyID, start, end)
}

// GetTenantAnalytics returns hourly analytics for all keys in a tenant.
func (s *APIKeys) GetTenantAnalytics(ctx context.Context, tenantID string, start, end time.Time) ([]apikeyrepo.AnalyticsHourly, error) {
	return s.repo.GetTenantAnalytics(ctx, tenantID, start, end)
}

// ListSecretManagers returns secret manager configs for a tenant.
func (s *APIKeys) ListSecretManagers(ctx context.Context, tenantID string) ([]apikeyrepo.SecretManagerConfig, error) {
	return s.repo.ListSecretManagerConfigs(ctx, tenantID)
}

// CreateSecretManager creates a new secret manager config.
func (s *APIKeys) CreateSecretManager(ctx context.Context, tenantID, managerType, name string, config map[string]string) (*apikeyrepo.SecretManagerConfig, error) {
	const where = "service.APIKeys.CreateSecretManager"

	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	c := apikeyrepo.SecretManagerConfig{
		TenantID:    tenantID,
		ManagerType: domain.SecretManagerType(managerType),
		Name:        name,
		Config:      configJSON,
		IsActive:    true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	id, err := s.repo.CreateSecretManagerConfig(ctx, c)
	if err != nil {
		logext.Errorf(ctx, "[%s] CreateSecretManagerConfig failed,err:%+v", where, err.Error())
		return nil, err
	}
	c.ID = id

	return ptrext.Of(c), nil
}

// GetRotationSchedule returns the rotation schedule for a key.
func (s *APIKeys) GetRotationSchedule(ctx context.Context, tenantID string, keyID uuid.UUID) (*apikeyrepo.RotationSchedule, error) {
	return s.repo.GetRotationSchedule(ctx, keyID)
}

// CreateRotationSchedule creates a rotation schedule for a key.
func (s *APIKeys) CreateRotationSchedule(ctx context.Context, tenantID string, keyID uuid.UUID, intervalDays, gracePeriodHours int, autoNotify bool, notifyDaysBefore []int) (*apikeyrepo.RotationSchedule, error) {
	nextRotation := time.Now().Add(time.Duration(intervalDays) * 24 * time.Hour)
	_, err := s.repo.CreateRotationSchedule(ctx, apikeyrepo.RotationSchedule{
		TenantID:             tenantID,
		KeyID:                keyID,
		RotationIntervalDays: intervalDays,
		NextRotationAt:       nextRotation,
		GracePeriodHours:     gracePeriodHours,
		AutoNotify:           autoNotify,
		NotifyDaysBefore:     notifyDaysBefore,
		IsActive:             true,
	})
	if err != nil {
		return nil, err
	}
	return s.repo.GetRotationSchedule(ctx, keyID)
}

// GetUnusedScopes returns unused scopes for a key.
func (s *APIKeys) GetUnusedScopes(ctx context.Context, tenantID string, keyID uuid.UUID) ([]string, []apikeyrepo.PermissionUsage, error) {
	unused, err := s.repo.GetUnusedScopes(ctx, keyID)
	if err != nil {
		return nil, nil, err
	}
	allUsage, err := s.repo.GetScopeUsage(ctx, keyID)
	if err != nil {
		return nil, nil, err
	}
	return unused, allUsage, nil
}

// ListSigningKeys returns signing keys for a key.
func (s *APIKeys) ListSigningKeys(ctx context.Context, tenantID string, keyID uuid.UUID) ([]apikeyrepo.SigningKey, error) {
	return s.repo.ListSigningKeys(ctx, keyID)
}

// CreateSigningKey creates a signing key for PKCV.
func (s *APIKeys) CreateSigningKey(ctx context.Context, tenantID string, keyID uuid.UUID, algorithm, publicKeyPEM, fingerprint string, expiresAt *time.Time) (*apikeyrepo.SigningKey, error) {
	id, err := s.repo.CreateSigningKey(ctx, apikeyrepo.SigningKey{
		KeyID:          keyID,
		TenantID:       tenantID,
		Algorithm:      algorithm,
		PublicKeyPEM:   publicKeyPEM,
		KeyFingerprint: fingerprint,
		IsActive:       true,
		ExpiresAt:      expiresAt,
	})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	return ptrext.Of(apikeyrepo.SigningKey{
		ID:             id,
		KeyID:          keyID,
		TenantID:       tenantID,
		Algorithm:      algorithm,
		PublicKeyPEM:   publicKeyPEM,
		KeyFingerprint: fingerprint,
		IsActive:       true,
		CreatedAt:      now,
		ExpiresAt:      expiresAt,
	}), nil
}

// ListManagedIdentities returns managed identities.
func (s *APIKeys) ListManagedIdentities(ctx context.Context, tenantID string) ([]apikeyrepo.ManagedIdentity, error) {
	return s.repo.ListManagedIdentities(ctx, tenantID)
}

// CreateManagedIdentity creates a managed identity.
func (s *APIKeys) CreateManagedIdentity(ctx context.Context, tenantID string, m apikeyrepo.ManagedIdentity) (*apikeyrepo.ManagedIdentity, error) {
	id, err := s.repo.CreateManagedIdentity(ctx, m)
	if err != nil {
		return nil, err
	}
	m.ID = id
	m.IsActive = true
	m.CreatedAt = time.Now()
	m.UpdatedAt = time.Now()
	return ptrext.Of(m), nil
}

// ListSIEMIntegrations returns SIEM integrations.
func (s *APIKeys) ListSIEMIntegrations(ctx context.Context, tenantID string) ([]apikeyrepo.SIEMIntegration, error) {
	return s.repo.ListSIEMIntegrations(ctx, tenantID)
}

// CreateSIEMIntegration creates a SIEM integration.
func (s *APIKeys) CreateSIEMIntegration(ctx context.Context, tenantID string, si apikeyrepo.SIEMIntegration) (*apikeyrepo.SIEMIntegration, error) {
	id, err := s.repo.CreateSIEMIntegration(ctx, si)
	if err != nil {
		return nil, err
	}
	si.ID = id
	si.IsActive = true
	si.CreatedAt = time.Now()
	si.UpdatedAt = time.Now()
	return ptrext.Of(si), nil
}

// ListAIAgentConfigs returns AI agent configurations.
func (s *APIKeys) ListAIAgentConfigs(ctx context.Context, tenantID string) ([]apikeyrepo.AIAgentConfig, error) {
	return s.repo.ListAIAgentConfigs(ctx, tenantID)
}

// CreateAIAgentConfig creates an AI agent configuration.
func (s *APIKeys) CreateAIAgentConfig(ctx context.Context, tenantID string, c apikeyrepo.AIAgentConfig) (*apikeyrepo.AIAgentConfig, error) {
	id, err := s.repo.CreateAIAgentConfig(ctx, c)
	if err != nil {
		return nil, err
	}
	c.ID = id
	c.IsActive = true
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	return ptrext.Of(c), nil
}

// GetKeyHealth returns the health score for a key.
func (s *APIKeys) GetKeyHealth(ctx context.Context, tenantID string, keyID uuid.UUID) (*attunev1.KeyHealthScore, error) {
	score, err := s.repo.UpdateKeyHealthScore(ctx, keyID)
	if err != nil {
		return nil, err
	}

	row, err := s.repo.GetByID(ctx, tenantID, keyID)
	if err != nil {
		return nil, err
	}

	unused, err := s.repo.GetUnusedScopes(ctx, keyID)
	if err != nil {
		return nil, err
	}

	daysSinceRotation := int(time.Since(row.CreatedAt).Hours() / 24)

	return ptrext.Of(attunev1.KeyHealthScore{
		Score:             int32(score),
		HasExpiry:         row.ExpiresAt != nil,
		HasIpRestriction:  len(row.AllowedCIDRs) > 0,
		HasRateLimit:      row.RateLimitRPM != nil,
		UnusedScopeCount:  int32(len(unused)),
		DaysSinceRotation: int32(daysSinceRotation),
	}), nil
}
