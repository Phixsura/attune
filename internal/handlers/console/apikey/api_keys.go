package apikey

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	"github.com/Phixsura/attune/internal/service/apikey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type apiKeysService interface {
	List(ctx context.Context, tenantID string) ([]apikeyrepo.APIKeyListRow, error)
	Issue(ctx context.Context, tenantID, label string) (raw string, keyID uuid.UUID, err error)
	IssueWithScopes(ctx context.Context, tenantID, label string, scopes []domain.Scope) (raw string, keyID uuid.UUID, err error)
	IssueFullWithScopes(ctx context.Context, p apikey.IssueParams, scopes []domain.Scope) (raw string, keyID uuid.UUID, err error)
	GetScopes(ctx context.Context, keyID uuid.UUID) ([]domain.Scope, error)
	GetScopesBatch(ctx context.Context, keyIDs []uuid.UUID) (map[uuid.UUID][]domain.Scope, error)
	GetByID(ctx context.Context, tenantID string, keyID uuid.UUID) (*apikeyrepo.APIKeyListRow, error)
	Revoke(ctx context.Context, tenantID string, id uuid.UUID) error
	Rotate(ctx context.Context, p apikey.RotateParams) (newRaw string, newKeyID uuid.UUID, err error)
	ListRequestLogs(ctx context.Context, tenantID string, keyID uuid.UUID, limit int) ([]apikeyrepo.RequestLogRow, error)
	UpdateEnvironment(ctx context.Context, tenantID string, keyID uuid.UUID, env string) error
	GetExpiringKeys(ctx context.Context, within time.Duration) ([]apikeyrepo.APIKeyListRow, error)
	CreateServiceAccount(ctx context.Context, tenantID, name, description string) (uuid.UUID, error)
	ListServiceAccounts(ctx context.Context, tenantID string) ([]apikeyrepo.ServiceAccountRow, error)
	CreateEventSubscription(ctx context.Context, tenantID string, eventTypes []string, webhookURL, secret string) (uuid.UUID, error)
	ListEventSubscriptions(ctx context.Context, tenantID string) ([]apikeyrepo.EventSubscription, error)
	GetUnresolvedLeaks(ctx context.Context, tenantID string) ([]apikeyrepo.LeakDetection, error)

	// Enterprise features
	GetPolicy(ctx context.Context, tenantID string) (*apikeyrepo.Policy, error)
	UpsertPolicy(ctx context.Context, policy apikeyrepo.Policy) error
	ListProjects(ctx context.Context, tenantID string) ([]apikeyrepo.Project, error)
	CreateProject(ctx context.Context, tenantID, name, description string) (*apikeyrepo.Project, error)
	BindKeyToProject(ctx context.Context, tenantID string, keyID, projectID uuid.UUID) error
	GetKeyTags(ctx context.Context, tenantID string, keyID uuid.UUID) ([]apikeyrepo.Tag, error)
	SetKeyTags(ctx context.Context, tenantID string, keyID uuid.UUID, tags []apikeyrepo.Tag) error
	SetKeyBudget(ctx context.Context, tenantID string, keyID uuid.UUID, limitUSD float64, overageAction string) error
	CreateTempToken(ctx context.Context, tenantID string, parentKeyID uuid.UUID, expiresIn time.Duration, maxUses *int, purpose string) (*apikeyrepo.TempTokenResult, error)
	ListApprovalRequests(ctx context.Context, tenantID, status string) ([]apikeyrepo.ApprovalRequest, error)
	CreateApprovalRequest(ctx context.Context, req apikeyrepo.ApprovalRequest) (*apikeyrepo.ApprovalRequest, error)
	ReviewApproval(ctx context.Context, tenantID string, requestID uuid.UUID, reviewerID string, approve bool, notes string) (*apikeyrepo.ApprovalRequest, error)
	ListOAuth2Clients(ctx context.Context, tenantID string) ([]apikeyrepo.OAuth2Client, error)
	CreateOAuth2Client(ctx context.Context, tenantID, name, description string, redirectURIs, allowedScopes []string) (*apikeyrepo.OAuth2Client, string, error)
	GetKeyAnalytics(ctx context.Context, tenantID string, keyID uuid.UUID, start, end time.Time) ([]apikeyrepo.AnalyticsHourly, error)
	GetTenantAnalytics(ctx context.Context, tenantID string, start, end time.Time) ([]apikeyrepo.AnalyticsHourly, error)
	ListSecretManagers(ctx context.Context, tenantID string) ([]apikeyrepo.SecretManagerConfig, error)
	CreateSecretManager(ctx context.Context, tenantID, managerType, name string, config map[string]string) (*apikeyrepo.SecretManagerConfig, error)

	// Advanced security features
	GetRotationSchedule(ctx context.Context, tenantID string, keyID uuid.UUID) (*apikeyrepo.RotationSchedule, error)
	CreateRotationSchedule(ctx context.Context, tenantID string, keyID uuid.UUID, intervalDays, gracePeriodHours int, autoNotify bool, notifyDaysBefore []int) (*apikeyrepo.RotationSchedule, error)
	GetUnusedScopes(ctx context.Context, tenantID string, keyID uuid.UUID) ([]string, []apikeyrepo.PermissionUsage, error)
	ListSigningKeys(ctx context.Context, tenantID string, keyID uuid.UUID) ([]apikeyrepo.SigningKey, error)
	CreateSigningKey(ctx context.Context, tenantID string, keyID uuid.UUID, algorithm, publicKeyPEM, fingerprint string, expiresAt *time.Time) (*apikeyrepo.SigningKey, error)
	ListManagedIdentities(ctx context.Context, tenantID string) ([]apikeyrepo.ManagedIdentity, error)
	CreateManagedIdentity(ctx context.Context, tenantID string, m apikeyrepo.ManagedIdentity) (*apikeyrepo.ManagedIdentity, error)
	ListSIEMIntegrations(ctx context.Context, tenantID string) ([]apikeyrepo.SIEMIntegration, error)
	CreateSIEMIntegration(ctx context.Context, tenantID string, s apikeyrepo.SIEMIntegration) (*apikeyrepo.SIEMIntegration, error)
	ListAIAgentConfigs(ctx context.Context, tenantID string) ([]apikeyrepo.AIAgentConfig, error)
	CreateAIAgentConfig(ctx context.Context, tenantID string, c apikeyrepo.AIAgentConfig) (*apikeyrepo.AIAgentConfig, error)
	GetKeyHealth(ctx context.Context, tenantID string, keyID uuid.UUID) (*attunev1.KeyHealthScore, error)
}

// APIKeysHandler serves /fb/v1/console/api-keys. All three operations
// scope to the session's tenant — see auth.RequireSession middleware
// which writes TenantID to context before this handler runs.
type APIKeysHandler struct {
	svc   apiKeysService
	audit auditRecorder
}

type auditRecorder interface {
	Record(ctx context.Context, event auditlogsvc.Event) error
}

func NewAPIKeysHandler(svc *apikey.APIKeys) *APIKeysHandler {
	return ptrext.Of(APIKeysHandler{svc: svc})
}

func (h *APIKeysHandler) SetAuditLogger(audit auditRecorder) {
	h.audit = audit
}

func toProtoAPIKey(row apikeyrepo.APIKeyListRow, scopes []domain.Scope) *attunev1.ApiKey {
	scopeStrs := make([]string, len(scopes))
	for i, s := range scopes {
		scopeStrs[i] = string(s)
	}
	k := ptrext.Of(attunev1.ApiKey{
		Id:           row.ID.String(),
		KeyPrefix:    row.KeyPrefix,
		Label:        row.Label,
		IsActive:     row.IsActive,
		CreatedAt:    row.CreatedAt.UTC().Format(time.RFC3339),
		Scopes:       scopeStrs,
		UsageCount:   row.UsageCount,
		AllowedCidrs: row.AllowedCIDRs,
	})
	if row.Description != "" {
		k.Description = ptrext.Of(row.Description)
	}
	if row.LastUsedAt != nil {
		k.LastUsedAt = ptrext.Of(row.LastUsedAt.UTC().Format(time.RFC3339))
	}
	if row.RevokedAt != nil {
		k.RevokedAt = ptrext.Of(row.RevokedAt.UTC().Format(time.RFC3339))
	}
	if row.ExpiresAt != nil {
		k.ExpiresAt = ptrext.Of(row.ExpiresAt.UTC().Format(time.RFC3339))
	}
	if row.RateLimitRPM != nil {
		k.RateLimitRpm = ptrext.Of(int32(ptrext.Indirect(row.RateLimitRPM)))
	}
	return k
}

func auditActor(auth *session.AuthCtx, req *http.Request) auditlogsvc.Actor {
	actorType := auth.UserType
	if actorType == "" {
		actorType = "admin"
	}
	return auditlogsvc.ActorFromRequest(actorType, auth.UserID, req)
}
