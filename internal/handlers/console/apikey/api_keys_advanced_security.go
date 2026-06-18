// ptrext:file-allow proto request/response conversions use raw address-of for optional fields.
package apikey

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/logext"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

// GetRotationSchedule returns the rotation schedule for a key.
func (h *APIKeysHandler) GetRotationSchedule(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetRotationScheduleRequest) (dispatcher.Result[*attunev1.GetRotationScheduleResponse], error) {
	const where = "handlers.console.apikey.GetRotationSchedule"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.GetRotationScheduleResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	schedule, err := h.svc.GetRotationSchedule(ctx, auth.TenantID, keyID)
	if err != nil {
		logext.Errorf(ctx, "[%s] get rotation schedule failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.GetRotationScheduleResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get rotation schedule")
	}

	return dispatcher.OK(ptrext.Of(attunev1.GetRotationScheduleResponse{
		Schedule: toProtoRotationSchedule(schedule),
	}))
}

// CreateRotationSchedule creates a rotation schedule for a key.
func (h *APIKeysHandler) CreateRotationSchedule(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateRotationScheduleRequest) (dispatcher.Result[*attunev1.CreateRotationScheduleResponse], error) {
	const where = "handlers.console.apikey.CreateRotationSchedule"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateRotationScheduleResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	intervalDays := 90
	if req.RotationIntervalDays != nil {
		intervalDays = int(ptrext.Indirect(req.RotationIntervalDays))
	}

	gracePeriodHours := 168
	if req.GracePeriodHours != nil {
		gracePeriodHours = int(ptrext.Indirect(req.GracePeriodHours))
	}

	autoNotify := true
	if req.AutoNotify != nil {
		autoNotify = ptrext.Indirect(req.AutoNotify)
	}

	notifyDaysBefore := []int{30, 7, 1}
	if len(req.GetNotifyDaysBefore()) > 0 {
		notifyDaysBefore = make([]int, len(req.GetNotifyDaysBefore()))
		for i, d := range req.GetNotifyDaysBefore() {
			notifyDaysBefore[i] = int(d)
		}
	}

	schedule, err := h.svc.CreateRotationSchedule(ctx, auth.TenantID, keyID, intervalDays, gracePeriodHours, autoNotify, notifyDaysBefore)
	if err != nil {
		logext.Errorf(ctx, "[%s] create rotation schedule failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.CreateRotationScheduleResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create rotation schedule")
	}

	return dispatcher.Created(ptrext.Of(attunev1.CreateRotationScheduleResponse{
		Schedule: toProtoRotationSchedule(schedule),
	}))
}

// GetUnusedScopes returns unused scopes for a key.
func (h *APIKeysHandler) GetUnusedScopes(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetUnusedScopesRequest) (dispatcher.Result[*attunev1.GetUnusedScopesResponse], error) {
	const where = "handlers.console.apikey.GetUnusedScopes"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.GetUnusedScopesResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	unused, allUsage, err := h.svc.GetUnusedScopes(ctx, auth.TenantID, keyID)
	if err != nil {
		logext.Errorf(ctx, "[%s] get unused scopes failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.GetUnusedScopesResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get unused scopes")
	}

	usageProtos := make([]*attunev1.PermissionUsage, 0, len(allUsage))
	for _, u := range allUsage {
		pu := ptrext.Of(attunev1.PermissionUsage{
			Scope:      u.Scope,
			UsageCount: u.UsageCount,
		})
		if u.FirstUsedAt != nil {
			pu.FirstUsedAt = ptrext.Of(u.FirstUsedAt.UTC().Format(time.RFC3339))
		}
		if u.LastUsedAt != nil {
			pu.LastUsedAt = ptrext.Of(u.LastUsedAt.UTC().Format(time.RFC3339))
		}
		usageProtos = append(usageProtos, pu)
	}

	return dispatcher.OK(ptrext.Of(attunev1.GetUnusedScopesResponse{
		UnusedScopes: unused,
		AllUsage:     usageProtos,
	}))
}

// ListSigningKeys returns signing keys for PKCV.
func (h *APIKeysHandler) ListSigningKeys(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.ListSigningKeysRequest) (dispatcher.Result[*attunev1.ListSigningKeysResponse], error) {
	const where = "handlers.console.apikey.ListSigningKeys"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.ListSigningKeysResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	keys, err := h.svc.ListSigningKeys(ctx, auth.TenantID, keyID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list signing keys failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.ListSigningKeysResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list signing keys")
	}

	items := make([]*attunev1.SigningKey, 0, len(keys))
	for _, k := range keys {
		items = append(items, toProtoSigningKey(&k))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListSigningKeysResponse{Items: items}))
}

// CreateSigningKey creates a signing key for PKCV.
func (h *APIKeysHandler) CreateSigningKey(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateSigningKeyRequest) (dispatcher.Result[*attunev1.CreateSigningKeyResponse], error) {
	const where = "handlers.console.apikey.CreateSigningKey"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.CreateSigningKeyResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	algorithm := strings.TrimSpace(req.GetAlgorithm())
	if algorithm == "" {
		return dispatcher.Fail[*attunev1.CreateSigningKeyResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "algorithm is required")
	}

	publicKeyPEM := strings.TrimSpace(req.GetPublicKeyPem())
	if publicKeyPEM == "" {
		return dispatcher.Fail[*attunev1.CreateSigningKeyResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "public_key_pem is required")
	}

	var expiresAt *time.Time
	if e := ptrext.Indirect(req.ExpiresAt); e != "" {
		if parsed, err := time.Parse(time.RFC3339, e); err == nil {
			expiresAt = &parsed
		}
	}

	fingerprint := computeKeyFingerprint(publicKeyPEM)

	signingKey, err := h.svc.CreateSigningKey(ctx, auth.TenantID, keyID, algorithm, publicKeyPEM, fingerprint, expiresAt)
	if err != nil {
		logext.Errorf(ctx, "[%s] create signing key failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.CreateSigningKeyResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create signing key")
	}

	return dispatcher.Created(ptrext.Of(attunev1.CreateSigningKeyResponse{
		Key: toProtoSigningKey(signingKey),
	}))
}

// ListManagedIdentities returns managed identities.
func (h *APIKeysHandler) ListManagedIdentities(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListManagedIdentitiesRequest) (dispatcher.Result[*attunev1.ListManagedIdentitiesResponse], error) {
	const where = "handlers.console.apikey.ListManagedIdentities"
	auth := ctx.Auth

	identities, err := h.svc.ListManagedIdentities(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list managed identities failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListManagedIdentitiesResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list managed identities")
	}

	items := make([]*attunev1.ManagedIdentity, 0, len(identities))
	for _, m := range identities {
		items = append(items, toProtoManagedIdentity(&m))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListManagedIdentitiesResponse{Items: items}))
}

// CreateManagedIdentity creates a managed identity.
func (h *APIKeysHandler) CreateManagedIdentity(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateManagedIdentityRequest) (dispatcher.Result[*attunev1.CreateManagedIdentityResponse], error) {
	const where = "handlers.console.apikey.CreateManagedIdentity"
	auth := ctx.Auth

	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return dispatcher.Fail[*attunev1.CreateManagedIdentityResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_LABEL, "name is required")
	}

	provider := strings.TrimSpace(req.GetProvider())
	if provider == "" {
		return dispatcher.Fail[*attunev1.CreateManagedIdentityResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "provider is required")
	}

	externalID := strings.TrimSpace(req.GetExternalId())
	if externalID == "" {
		return dispatcher.Fail[*attunev1.CreateManagedIdentityResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "external_id is required")
	}

	identity, err := h.svc.CreateManagedIdentity(ctx, auth.TenantID, apikeyrepo.ManagedIdentity{
		TenantID:       auth.TenantID,
		Name:           name,
		Provider:       domain.IdentityProvider(provider),
		ExternalID:     externalID,
		Audience:       ptrext.Indirect(req.Audience),
		Issuer:         ptrext.Indirect(req.Issuer),
		SubjectPattern: ptrext.Indirect(req.SubjectPattern),
		AllowedScopes:  req.GetAllowedScopes(),
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] create managed identity failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateManagedIdentityResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create managed identity")
	}

	return dispatcher.Created(ptrext.Of(attunev1.CreateManagedIdentityResponse{
		Identity: toProtoManagedIdentity(identity),
	}))
}

// ListSIEMIntegrations returns SIEM integrations.
func (h *APIKeysHandler) ListSIEMIntegrations(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListSIEMIntegrationsRequest) (dispatcher.Result[*attunev1.ListSIEMIntegrationsResponse], error) {
	const where = "handlers.console.apikey.ListSIEMIntegrations"
	auth := ctx.Auth

	integrations, err := h.svc.ListSIEMIntegrations(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list siem integrations failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListSIEMIntegrationsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list siem integrations")
	}

	items := make([]*attunev1.SIEMIntegration, 0, len(integrations))
	for _, s := range integrations {
		items = append(items, toProtoSIEMIntegration(&s))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListSIEMIntegrationsResponse{Items: items}))
}

// CreateSIEMIntegration creates a SIEM integration.
func (h *APIKeysHandler) CreateSIEMIntegration(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateSIEMIntegrationRequest) (dispatcher.Result[*attunev1.CreateSIEMIntegrationResponse], error) {
	const where = "handlers.console.apikey.CreateSIEMIntegration"
	auth := ctx.Auth

	provider := strings.TrimSpace(req.GetProvider())
	if provider == "" {
		return dispatcher.Fail[*attunev1.CreateSIEMIntegrationResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "provider is required")
	}

	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return dispatcher.Fail[*attunev1.CreateSIEMIntegrationResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_LABEL, "name is required")
	}

	endpointURL := strings.TrimSpace(req.GetEndpointUrl())
	if endpointURL == "" {
		return dispatcher.Fail[*attunev1.CreateSIEMIntegrationResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "endpoint_url is required")
	}

	authConfigJSON, _ := json.Marshal(req.GetAuthConfig())

	batchSize := 100
	if req.BatchSize != nil {
		batchSize = int(ptrext.Indirect(req.BatchSize))
	}

	flushInterval := 60
	if req.FlushIntervalSeconds != nil {
		flushInterval = int(ptrext.Indirect(req.FlushIntervalSeconds))
	}

	eventTypes := req.GetEventTypes()
	if len(eventTypes) == 0 {
		eventTypes = []string{
			"key.created", "key.rotated", "key.revoked", "key.expired",
			"key.leaked", "auth.success", "auth.failure", "scope.denied",
		}
	}

	integration, err := h.svc.CreateSIEMIntegration(ctx, auth.TenantID, apikeyrepo.SIEMIntegration{
		TenantID:             auth.TenantID,
		Provider:             domain.SIEMProvider(provider),
		Name:                 name,
		EndpointURL:          endpointURL,
		AuthConfig:           authConfigJSON,
		EventTypes:           eventTypes,
		BatchSize:            batchSize,
		FlushIntervalSeconds: flushInterval,
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] create siem integration failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateSIEMIntegrationResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create siem integration")
	}

	return dispatcher.Created(ptrext.Of(attunev1.CreateSIEMIntegrationResponse{
		Integration: toProtoSIEMIntegration(integration),
	}))
}

// ListAIAgentConfigs returns AI agent configurations.
func (h *APIKeysHandler) ListAIAgentConfigs(ctx *dispatcher.RequestContext[*session.AuthCtx], _ *attunev1.ListAIAgentConfigsRequest) (dispatcher.Result[*attunev1.ListAIAgentConfigsResponse], error) {
	const where = "handlers.console.apikey.ListAIAgentConfigs"
	auth := ctx.Auth

	configs, err := h.svc.ListAIAgentConfigs(ctx, auth.TenantID)
	if err != nil {
		logext.Errorf(ctx, "[%s] list ai agent configs failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.ListAIAgentConfigsResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to list ai agent configs")
	}

	items := make([]*attunev1.AIAgentConfig, 0, len(configs))
	for _, c := range configs {
		items = append(items, toProtoAIAgentConfig(&c))
	}

	return dispatcher.OK(ptrext.Of(attunev1.ListAIAgentConfigsResponse{Items: items}))
}

// CreateAIAgentConfig creates an AI agent configuration.
func (h *APIKeysHandler) CreateAIAgentConfig(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.CreateAIAgentConfigRequest) (dispatcher.Result[*attunev1.CreateAIAgentConfigResponse], error) {
	const where = "handlers.console.apikey.CreateAIAgentConfig"
	auth := ctx.Auth

	name := strings.TrimSpace(req.GetName())
	if name == "" {
		return dispatcher.Fail[*attunev1.CreateAIAgentConfigResponse](http.StatusBadRequest, attunev1.ErrorCode_MISSING_LABEL, "name is required")
	}

	agentType := strings.TrimSpace(req.GetAgentType())
	if agentType == "" {
		return dispatcher.Fail[*attunev1.CreateAIAgentConfigResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_REQUEST, "agent_type is required")
	}

	maxTokens := 100000
	if req.MaxTokensPerRequest != nil {
		maxTokens = int(ptrext.Indirect(req.MaxTokensPerRequest))
	}

	maxRequests := 60
	if req.MaxRequestsPerMinute != nil {
		maxRequests = int(ptrext.Indirect(req.MaxRequestsPerMinute))
	}

	requireApproval := false
	if req.RequireHumanApproval != nil {
		requireApproval = ptrext.Indirect(req.RequireHumanApproval)
	}

	approvalTimeout := 300
	if req.ApprovalTimeoutSeconds != nil {
		approvalTimeout = int(ptrext.Indirect(req.ApprovalTimeoutSeconds))
	}

	auditAll := true
	if req.AuditAllRequests != nil {
		auditAll = ptrext.Indirect(req.AuditAllRequests)
	}

	config, err := h.svc.CreateAIAgentConfig(ctx, auth.TenantID, apikeyrepo.AIAgentConfig{
		TenantID:             auth.TenantID,
		Name:                 name,
		AgentType:            domain.AIAgentType(agentType),
		AllowedScopes:        req.GetAllowedScopes(),
		MaxTokensPerRequest:  maxTokens,
		MaxRequestsPerMinute: maxRequests,
		AllowedModels:        req.GetAllowedModels(),
		RequireHumanApproval: requireApproval,
		ApprovalTimeoutSecs:  approvalTimeout,
		AuditAllRequests:     auditAll,
	})
	if err != nil {
		logext.Errorf(ctx, "[%s] create ai agent config failed,tenant_id:%s,err:%+v", where, auth.TenantID, err.Error())
		return dispatcher.Fail[*attunev1.CreateAIAgentConfigResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to create ai agent config")
	}

	return dispatcher.Created(ptrext.Of(attunev1.CreateAIAgentConfigResponse{
		Config: toProtoAIAgentConfig(config),
	}))
}

// GetKeyHealth returns the health score for a key.
func (h *APIKeysHandler) GetKeyHealth(ctx *dispatcher.RequestContext[*session.AuthCtx], req *attunev1.GetKeyHealthRequest) (dispatcher.Result[*attunev1.GetKeyHealthResponse], error) {
	const where = "handlers.console.apikey.GetKeyHealth"
	auth := ctx.Auth

	keyID, err := uuid.Parse(req.GetId())
	if err != nil {
		return dispatcher.Fail[*attunev1.GetKeyHealthResponse](http.StatusBadRequest, attunev1.ErrorCode_BAD_ID, "invalid key id")
	}

	health, err := h.svc.GetKeyHealth(ctx, auth.TenantID, keyID)
	if err != nil {
		if errors.Is(err, apikeyrepo.ErrAPIKeyNotFound) {
			return dispatcher.Fail[*attunev1.GetKeyHealthResponse](http.StatusNotFound, attunev1.ErrorCode_NOT_FOUND, "api key not found")
		}
		logext.Errorf(ctx, "[%s] get key health failed,tenant_id:%s,key_id:%s,err:%+v", where, auth.TenantID, keyID, err.Error())
		return dispatcher.Fail[*attunev1.GetKeyHealthResponse](http.StatusInternalServerError, attunev1.ErrorCode_INTERNAL, "failed to get key health")
	}

	return dispatcher.OK(ptrext.Of(attunev1.GetKeyHealthResponse{
		Health: health,
	}))
}

func computeKeyFingerprint(publicKeyPEM string) string {
	hash := sha256.Sum256([]byte(publicKeyPEM))
	return hex.EncodeToString(hash[:])
}

func toProtoRotationSchedule(s *apikeyrepo.RotationSchedule) *attunev1.RotationSchedule {
	if s == nil {
		return nil
	}
	notifyDays := make([]int32, len(s.NotifyDaysBefore))
	for i, d := range s.NotifyDaysBefore {
		notifyDays[i] = int32(d)
	}
	schedule := ptrext.Of(attunev1.RotationSchedule{
		Id:                   s.ID.String(),
		KeyId:                s.KeyID.String(),
		RotationIntervalDays: int32(s.RotationIntervalDays),
		NextRotationAt:       s.NextRotationAt.UTC().Format(time.RFC3339),
		GracePeriodHours:     int32(s.GracePeriodHours),
		AutoNotify:           s.AutoNotify,
		NotifyDaysBefore:     notifyDays,
		IsActive:             s.IsActive,
		CreatedAt:            s.CreatedAt.UTC().Format(time.RFC3339),
	})
	if s.LastRotatedAt != nil {
		schedule.LastRotatedAt = ptrext.Of(s.LastRotatedAt.UTC().Format(time.RFC3339))
	}
	return schedule
}

func toProtoSigningKey(k *apikeyrepo.SigningKey) *attunev1.SigningKey {
	if k == nil {
		return nil
	}
	sk := ptrext.Of(attunev1.SigningKey{
		Id:             k.ID.String(),
		KeyId:          k.KeyID.String(),
		Algorithm:      k.Algorithm,
		PublicKeyPem:   k.PublicKeyPEM,
		KeyFingerprint: k.KeyFingerprint,
		IsActive:       k.IsActive,
		CreatedAt:      k.CreatedAt.UTC().Format(time.RFC3339),
	})
	if k.ExpiresAt != nil {
		sk.ExpiresAt = ptrext.Of(k.ExpiresAt.UTC().Format(time.RFC3339))
	}
	return sk
}

func toProtoManagedIdentity(m *apikeyrepo.ManagedIdentity) *attunev1.ManagedIdentity {
	if m == nil {
		return nil
	}
	mi := ptrext.Of(attunev1.ManagedIdentity{
		Id:            m.ID.String(),
		Name:          m.Name,
		Provider:      string(m.Provider),
		ExternalId:    m.ExternalID,
		AllowedScopes: m.AllowedScopes,
		IsActive:      m.IsActive,
		CreatedAt:     m.CreatedAt.UTC().Format(time.RFC3339),
	})
	if m.Audience != "" {
		mi.Audience = ptrext.Of(m.Audience)
	}
	if m.Issuer != "" {
		mi.Issuer = ptrext.Of(m.Issuer)
	}
	if m.SubjectPattern != "" {
		mi.SubjectPattern = ptrext.Of(m.SubjectPattern)
	}
	if m.LastUsedAt != nil {
		mi.LastUsedAt = ptrext.Of(m.LastUsedAt.UTC().Format(time.RFC3339))
	}
	return mi
}

func toProtoSIEMIntegration(s *apikeyrepo.SIEMIntegration) *attunev1.SIEMIntegration {
	if s == nil {
		return nil
	}
	si := ptrext.Of(attunev1.SIEMIntegration{
		Id:                   s.ID.String(),
		Provider:             string(s.Provider),
		Name:                 s.Name,
		EndpointUrl:          s.EndpointURL,
		EventTypes:           s.EventTypes,
		BatchSize:            int32(s.BatchSize),
		FlushIntervalSeconds: int32(s.FlushIntervalSeconds),
		IsActive:             s.IsActive,
		CreatedAt:            s.CreatedAt.UTC().Format(time.RFC3339),
	})
	if s.LastSentAt != nil {
		si.LastSentAt = ptrext.Of(s.LastSentAt.UTC().Format(time.RFC3339))
	}
	if s.LastError != "" {
		si.LastError = ptrext.Of(s.LastError)
	}
	return si
}

func toProtoAIAgentConfig(c *apikeyrepo.AIAgentConfig) *attunev1.AIAgentConfig {
	if c == nil {
		return nil
	}
	return ptrext.Of(attunev1.AIAgentConfig{
		Id:                     c.ID.String(),
		Name:                   c.Name,
		AgentType:              string(c.AgentType),
		AllowedScopes:          c.AllowedScopes,
		MaxTokensPerRequest:    int32(c.MaxTokensPerRequest),
		MaxRequestsPerMinute:   int32(c.MaxRequestsPerMinute),
		AllowedModels:          c.AllowedModels,
		RequireHumanApproval:   c.RequireHumanApproval,
		ApprovalTimeoutSeconds: int32(c.ApprovalTimeoutSecs),
		AuditAllRequests:       c.AuditAllRequests,
		IsActive:               c.IsActive,
		CreatedAt:              c.CreatedAt.UTC().Format(time.RFC3339),
	})
}
