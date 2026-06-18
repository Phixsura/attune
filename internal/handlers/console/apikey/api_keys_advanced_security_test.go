// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package apikey

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/infra/apikey"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

// advancedSecurityTestService extends fakeAPIKeysService with advanced security methods.
type advancedSecurityTestService struct {
	fakeAPIKeysService

	// Rotation schedule
	rotationSchedule    *apikeyrepo.RotationSchedule
	rotationScheduleErr error

	// Unused scopes
	unusedScopes    []string
	permissionUsage []apikeyrepo.PermissionUsage
	unusedScopesErr error

	// Signing keys
	signingKeys   []apikeyrepo.SigningKey
	signingKeyErr error

	// Managed identities
	managedIdentities  []apikeyrepo.ManagedIdentity
	managedIdentityErr error

	// SIEM integrations
	siemIntegrations   []apikeyrepo.SIEMIntegration
	siemIntegrationErr error

	// AI agent configs
	aiAgentConfigs   []apikeyrepo.AIAgentConfig
	aiAgentConfigErr error

	// Key health
	keyHealth    *attunev1.KeyHealthScore
	keyHealthErr error

	// Capture calls
	capturedKeyID    uuid.UUID
	capturedTenantID string
}

func (s *advancedSecurityTestService) GetRotationSchedule(_ context.Context, tenantID string, keyID uuid.UUID) (*apikeyrepo.RotationSchedule, error) {
	s.capturedTenantID = tenantID
	s.capturedKeyID = keyID
	return s.rotationSchedule, s.rotationScheduleErr
}

func (s *advancedSecurityTestService) CreateRotationSchedule(_ context.Context, tenantID string, keyID uuid.UUID, intervalDays, gracePeriodHours int, autoNotify bool, notifyDaysBefore []int) (*apikeyrepo.RotationSchedule, error) {
	s.capturedTenantID = tenantID
	s.capturedKeyID = keyID
	if s.rotationScheduleErr != nil {
		return nil, s.rotationScheduleErr
	}
	return &apikeyrepo.RotationSchedule{
		ID:                   uuid.New(),
		TenantID:             tenantID,
		KeyID:                keyID,
		RotationIntervalDays: intervalDays,
		GracePeriodHours:     gracePeriodHours,
		AutoNotify:           autoNotify,
		NotifyDaysBefore:     notifyDaysBefore,
		NextRotationAt:       time.Now().Add(time.Duration(intervalDays) * 24 * time.Hour),
		IsActive:             true,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}, nil
}

func (s *advancedSecurityTestService) GetUnusedScopes(_ context.Context, tenantID string, keyID uuid.UUID) ([]string, []apikeyrepo.PermissionUsage, error) {
	s.capturedTenantID = tenantID
	s.capturedKeyID = keyID
	return s.unusedScopes, s.permissionUsage, s.unusedScopesErr
}

func (s *advancedSecurityTestService) ListSigningKeys(_ context.Context, tenantID string, keyID uuid.UUID) ([]apikeyrepo.SigningKey, error) {
	s.capturedTenantID = tenantID
	s.capturedKeyID = keyID
	return s.signingKeys, s.signingKeyErr
}

func (s *advancedSecurityTestService) CreateSigningKey(_ context.Context, tenantID string, keyID uuid.UUID, algorithm, publicKeyPEM, fingerprint string, expiresAt *time.Time) (*apikeyrepo.SigningKey, error) {
	s.capturedTenantID = tenantID
	s.capturedKeyID = keyID
	if s.signingKeyErr != nil {
		return nil, s.signingKeyErr
	}
	return &apikeyrepo.SigningKey{
		ID:             uuid.New(),
		KeyID:          keyID,
		TenantID:       tenantID,
		Algorithm:      algorithm,
		PublicKeyPEM:   publicKeyPEM,
		KeyFingerprint: fingerprint,
		IsActive:       true,
		CreatedAt:      time.Now(),
		ExpiresAt:      expiresAt,
	}, nil
}

func (s *advancedSecurityTestService) ListManagedIdentities(_ context.Context, tenantID string) ([]apikeyrepo.ManagedIdentity, error) {
	s.capturedTenantID = tenantID
	return s.managedIdentities, s.managedIdentityErr
}

func (s *advancedSecurityTestService) CreateManagedIdentity(_ context.Context, tenantID string, m apikeyrepo.ManagedIdentity) (*apikeyrepo.ManagedIdentity, error) {
	s.capturedTenantID = tenantID
	if s.managedIdentityErr != nil {
		return nil, s.managedIdentityErr
	}
	m.ID = uuid.New()
	m.TenantID = tenantID
	m.IsActive = true
	m.CreatedAt = time.Now()
	m.UpdatedAt = time.Now()
	return &m, nil
}

func (s *advancedSecurityTestService) ListSIEMIntegrations(_ context.Context, tenantID string) ([]apikeyrepo.SIEMIntegration, error) {
	s.capturedTenantID = tenantID
	return s.siemIntegrations, s.siemIntegrationErr
}

func (s *advancedSecurityTestService) CreateSIEMIntegration(_ context.Context, tenantID string, si apikeyrepo.SIEMIntegration) (*apikeyrepo.SIEMIntegration, error) {
	s.capturedTenantID = tenantID
	if s.siemIntegrationErr != nil {
		return nil, s.siemIntegrationErr
	}
	si.ID = uuid.New()
	si.TenantID = tenantID
	si.IsActive = true
	si.CreatedAt = time.Now()
	si.UpdatedAt = time.Now()
	return &si, nil
}

func (s *advancedSecurityTestService) ListAIAgentConfigs(_ context.Context, tenantID string) ([]apikeyrepo.AIAgentConfig, error) {
	s.capturedTenantID = tenantID
	return s.aiAgentConfigs, s.aiAgentConfigErr
}

func (s *advancedSecurityTestService) CreateAIAgentConfig(_ context.Context, tenantID string, c apikeyrepo.AIAgentConfig) (*apikeyrepo.AIAgentConfig, error) {
	s.capturedTenantID = tenantID
	if s.aiAgentConfigErr != nil {
		return nil, s.aiAgentConfigErr
	}
	c.ID = uuid.New()
	c.TenantID = tenantID
	c.IsActive = true
	c.CreatedAt = time.Now()
	c.UpdatedAt = time.Now()
	return &c, nil
}

func (s *advancedSecurityTestService) GetKeyHealth(_ context.Context, tenantID string, keyID uuid.UUID) (*attunev1.KeyHealthScore, error) {
	s.capturedTenantID = tenantID
	s.capturedKeyID = keyID
	return s.keyHealth, s.keyHealthErr
}

func advancedTestRequestContext() *dispatcher.RequestContext[*session.AuthCtx] {
	return &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    &session.AuthCtx{TenantID: "tenant-adv-1"},
	}
}

// ========== Rotation Schedule Tests ==========

func TestGetRotationSchedule_Success(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	scheduleID := uuid.New()
	nextRotation := time.Now().Add(90 * 24 * time.Hour)

	svc := &advancedSecurityTestService{
		rotationSchedule: &apikeyrepo.RotationSchedule{
			ID:                   scheduleID,
			TenantID:             "tenant-adv-1",
			KeyID:                keyID,
			RotationIntervalDays: 90,
			GracePeriodHours:     24,
			AutoNotify:           true,
			NotifyDaysBefore:     []int{7, 3, 1},
			NextRotationAt:       nextRotation,
			IsActive:             true,
			CreatedAt:            time.Now(),
			UpdatedAt:            time.Now(),
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetRotationSchedule(advancedTestRequestContext(), &attunev1.GetRotationScheduleRequest{Id: keyID.String()})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, keyID, svc.capturedKeyID)
	require.Equal(t, "tenant-adv-1", svc.capturedTenantID)
	require.Equal(t, int32(90), result.Body.GetSchedule().GetRotationIntervalDays())
	require.Equal(t, int32(24), result.Body.GetSchedule().GetGracePeriodHours())
	require.True(t, result.Body.GetSchedule().GetAutoNotify())
}

func TestGetRotationSchedule_NotFound(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{rotationSchedule: nil}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetRotationSchedule(advancedTestRequestContext(), &attunev1.GetRotationScheduleRequest{Id: keyID.String()})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Nil(t, result.Body.GetSchedule())
}

func TestGetRotationSchedule_BadID(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.GetRotationSchedule(advancedTestRequestContext(), &attunev1.GetRotationScheduleRequest{Id: "not-a-uuid"})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

func TestGetRotationSchedule_ServiceError(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{rotationScheduleErr: errors.New("db connection failed")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.GetRotationSchedule(advancedTestRequestContext(), &attunev1.GetRotationScheduleRequest{Id: keyID.String()})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

func TestCreateRotationSchedule_Success(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	intervalDays := int32(90)
	gracePeriod := int32(48)
	autoNotify := true
	result, err := h.CreateRotationSchedule(advancedTestRequestContext(), &attunev1.CreateRotationScheduleRequest{
		Id:                   keyID.String(),
		RotationIntervalDays: &intervalDays,
		GracePeriodHours:     &gracePeriod,
		AutoNotify:           &autoNotify,
		NotifyDaysBefore:     []int32{14, 7, 1},
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, keyID, svc.capturedKeyID)
}

func TestCreateRotationSchedule_BadID(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}
	intervalDays := int32(30)

	_, err := h.CreateRotationSchedule(advancedTestRequestContext(), &attunev1.CreateRotationScheduleRequest{
		Id:                   "invalid",
		RotationIntervalDays: &intervalDays,
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

// ========== Unused Scopes Tests ==========

func TestGetUnusedScopes_Success(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	now := time.Now()
	svc := &advancedSecurityTestService{
		unusedScopes: []string{"ingest:write", "feedback:delete"},
		permissionUsage: []apikeyrepo.PermissionUsage{
			{Scope: "feedback:read", UsageCount: 100, LastUsedAt: &now},
			{Scope: "ingest:write", UsageCount: 0, LastUsedAt: nil},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetUnusedScopes(advancedTestRequestContext(), &attunev1.GetUnusedScopesRequest{Id: keyID.String()})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, []string{"ingest:write", "feedback:delete"}, result.Body.GetUnusedScopes())
	require.Len(t, result.Body.GetAllUsage(), 2)
}

func TestGetUnusedScopes_Empty(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{
		unusedScopes:    []string{},
		permissionUsage: []apikeyrepo.PermissionUsage{},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetUnusedScopes(advancedTestRequestContext(), &attunev1.GetUnusedScopesRequest{Id: keyID.String()})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Empty(t, result.Body.GetUnusedScopes())
}

func TestGetUnusedScopes_BadID(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.GetUnusedScopes(advancedTestRequestContext(), &attunev1.GetUnusedScopesRequest{Id: ""})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
}

// ========== Signing Keys Tests ==========

func TestListSigningKeys_Success(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	signingKeyID := uuid.New()
	svc := &advancedSecurityTestService{
		signingKeys: []apikeyrepo.SigningKey{
			{
				ID:             signingKeyID,
				KeyID:          keyID,
				TenantID:       "tenant-adv-1",
				Algorithm:      "RS256",
				PublicKeyPEM:   "-----BEGIN PUBLIC KEY-----\nMIIB...\n-----END PUBLIC KEY-----",
				KeyFingerprint: "SHA256:abc123",
				IsActive:       true,
				CreatedAt:      time.Now(),
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListSigningKeys(advancedTestRequestContext(), &attunev1.ListSigningKeysRequest{Id: keyID.String()})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, "RS256", result.Body.GetItems()[0].GetAlgorithm())
	require.Equal(t, "SHA256:abc123", result.Body.GetItems()[0].GetKeyFingerprint())
}

func TestListSigningKeys_Empty(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{signingKeys: []apikeyrepo.SigningKey{}}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListSigningKeys(advancedTestRequestContext(), &attunev1.ListSigningKeysRequest{Id: keyID.String()})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Empty(t, result.Body.GetItems())
}

func TestCreateSigningKey_Success(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	publicKey := "-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA...\n-----END PUBLIC KEY-----"
	result, err := h.CreateSigningKey(advancedTestRequestContext(), &attunev1.CreateSigningKeyRequest{
		Id:           keyID.String(),
		Algorithm:    "RS256",
		PublicKeyPem: publicKey,
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, "RS256", result.Body.GetKey().GetAlgorithm())
	require.NotEmpty(t, result.Body.GetKey().GetKeyFingerprint())
}

func TestCreateSigningKey_BadID(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateSigningKey(advancedTestRequestContext(), &attunev1.CreateSigningKeyRequest{
		Id:           "bad",
		Algorithm:    "RS256",
		PublicKeyPem: "key",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
}

func TestCreateSigningKey_MissingAlgorithm(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateSigningKey(advancedTestRequestContext(), &attunev1.CreateSigningKeyRequest{
		Id:           keyID.String(),
		Algorithm:    "",
		PublicKeyPem: "key",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
}

func TestCreateSigningKey_MissingPublicKey(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateSigningKey(advancedTestRequestContext(), &attunev1.CreateSigningKeyRequest{
		Id:           keyID.String(),
		Algorithm:    "RS256",
		PublicKeyPem: "",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
}

// ========== Managed Identities Tests ==========

func TestListManagedIdentities_Success(t *testing.T) {
	t.Parallel()

	identityID := uuid.New()
	svc := &advancedSecurityTestService{
		managedIdentities: []apikeyrepo.ManagedIdentity{
			{
				ID:            identityID,
				TenantID:      "tenant-adv-1",
				Name:          "prod-api-identity",
				Provider:      domain.IdentityProviderAzureAD,
				ExternalID:    "/subscriptions/xxx/resourceGroups/yyy/providers/Microsoft.ManagedIdentity/userAssignedIdentities/zzz",
				AllowedScopes: []string{"ingest:write", "feedback:read"},
				IsActive:      true,
				CreatedAt:     time.Now(),
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListManagedIdentities(advancedTestRequestContext(), &attunev1.ListManagedIdentitiesRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, "azure_ad", result.Body.GetItems()[0].GetProvider())
	require.Equal(t, "prod-api-identity", result.Body.GetItems()[0].GetName())
}

func TestListManagedIdentities_Empty(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{managedIdentities: []apikeyrepo.ManagedIdentity{}}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListManagedIdentities(advancedTestRequestContext(), &attunev1.ListManagedIdentitiesRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Empty(t, result.Body.GetItems())
}

func TestCreateManagedIdentity_Success(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateManagedIdentity(advancedTestRequestContext(), &attunev1.CreateManagedIdentityRequest{
		Provider:      "gcp",
		Name:          "GCP Service Account",
		ExternalId:    "sa@project.iam.gserviceaccount.com",
		AllowedScopes: []string{"feedback:read"},
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, "gcp", result.Body.GetIdentity().GetProvider())
	require.Equal(t, "GCP Service Account", result.Body.GetIdentity().GetName())
}

func TestCreateManagedIdentity_MissingProvider(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateManagedIdentity(advancedTestRequestContext(), &attunev1.CreateManagedIdentityRequest{
		Provider:   "",
		ExternalId: "some-id",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
}

func TestCreateManagedIdentity_MissingExternalID(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateManagedIdentity(advancedTestRequestContext(), &attunev1.CreateManagedIdentityRequest{
		Provider:   "azure",
		ExternalId: "",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
}

func TestCreateManagedIdentity_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{managedIdentityErr: errors.New("duplicate identity")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateManagedIdentity(advancedTestRequestContext(), &attunev1.CreateManagedIdentityRequest{
		Provider:   "azure",
		ExternalId: "id",
		Name:       "test",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ========== SIEM Integrations Tests ==========

func TestListSIEMIntegrations_Success(t *testing.T) {
	t.Parallel()

	integrationID := uuid.New()
	svc := &advancedSecurityTestService{
		siemIntegrations: []apikeyrepo.SIEMIntegration{
			{
				ID:          integrationID,
				TenantID:    "tenant-adv-1",
				Provider:    domain.SIEMProviderSplunk,
				Name:        "Splunk HEC",
				EndpointURL: "https://splunk.example.com:8088/services/collector",
				EventTypes:  []string{"api_key.created", "api_key.revoked"},
				IsActive:    true,
				CreatedAt:   time.Now(),
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListSIEMIntegrations(advancedTestRequestContext(), &attunev1.ListSIEMIntegrationsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, "splunk", result.Body.GetItems()[0].GetProvider())
	require.Equal(t, "Splunk HEC", result.Body.GetItems()[0].GetName())
}

func TestListSIEMIntegrations_Empty(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{siemIntegrations: []apikeyrepo.SIEMIntegration{}}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListSIEMIntegrations(advancedTestRequestContext(), &attunev1.ListSIEMIntegrationsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Empty(t, result.Body.GetItems())
}

func TestCreateSIEMIntegration_Success(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateSIEMIntegration(advancedTestRequestContext(), &attunev1.CreateSIEMIntegrationRequest{
		Provider:    "datadog",
		Name:        "Datadog SIEM",
		EndpointUrl: "https://http-intake.logs.datadoghq.com/v1/input",
		EventTypes:  []string{"api_key.created", "api_key.rotated", "api_key.revoked"},
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, "datadog", result.Body.GetIntegration().GetProvider())
	require.Equal(t, "Datadog SIEM", result.Body.GetIntegration().GetName())
}

func TestCreateSIEMIntegration_MissingProvider(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateSIEMIntegration(advancedTestRequestContext(), &attunev1.CreateSIEMIntegrationRequest{
		Provider:    "",
		Name:        "Test",
		EndpointUrl: "https://example.com",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
}

func TestCreateSIEMIntegration_MissingEndpoint(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateSIEMIntegration(advancedTestRequestContext(), &attunev1.CreateSIEMIntegrationRequest{
		Provider:    "splunk",
		Name:        "Test",
		EndpointUrl: "",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
}

func TestCreateSIEMIntegration_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{siemIntegrationErr: errors.New("connection test failed")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateSIEMIntegration(advancedTestRequestContext(), &attunev1.CreateSIEMIntegrationRequest{
		Provider:    "splunk",
		Name:        "Test",
		EndpointUrl: "https://example.com",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ========== AI Agent Configs Tests ==========

func TestListAIAgentConfigs_Success(t *testing.T) {
	t.Parallel()

	agentID := uuid.New()
	svc := &advancedSecurityTestService{
		aiAgentConfigs: []apikeyrepo.AIAgentConfig{
			{
				ID:                   agentID,
				TenantID:             "tenant-adv-1",
				AgentType:            domain.AIAgentTypeMCPServer,
				Name:                 "MCP Agent",
				AllowedScopes:        []string{"feedback:read", "feedback:write"},
				MaxTokensPerRequest:  100000,
				MaxRequestsPerMinute: 1000,
				IsActive:             true,
				CreatedAt:            time.Now(),
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListAIAgentConfigs(advancedTestRequestContext(), &attunev1.ListAIAgentConfigsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, "mcp_server", result.Body.GetItems()[0].GetAgentType())
	require.Equal(t, "MCP Agent", result.Body.GetItems()[0].GetName())
}

func TestListAIAgentConfigs_Empty(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{aiAgentConfigs: []apikeyrepo.AIAgentConfig{}}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListAIAgentConfigs(advancedTestRequestContext(), &attunev1.ListAIAgentConfigsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Empty(t, result.Body.GetItems())
}

func TestCreateAIAgentConfig_Success(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateAIAgentConfig(advancedTestRequestContext(), &attunev1.CreateAIAgentConfigRequest{
		AgentType:     "openai_function",
		Name:          "GPT Agent",
		AllowedScopes: []string{"feedback:read"},
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, "openai_function", result.Body.GetConfig().GetAgentType())
	require.Equal(t, "GPT Agent", result.Body.GetConfig().GetName())
}

func TestCreateAIAgentConfig_MissingAgentType(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateAIAgentConfig(advancedTestRequestContext(), &attunev1.CreateAIAgentConfigRequest{
		AgentType: "",
		Name:      "Test",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
}

func TestCreateAIAgentConfig_MissingName(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateAIAgentConfig(advancedTestRequestContext(), &attunev1.CreateAIAgentConfigRequest{
		AgentType: "claude",
		Name:      "",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
}

func TestCreateAIAgentConfig_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{aiAgentConfigErr: errors.New("quota exceeded")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateAIAgentConfig(advancedTestRequestContext(), &attunev1.CreateAIAgentConfigRequest{
		AgentType: "claude",
		Name:      "Test",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ========== Key Health Tests ==========

func TestGetKeyHealth_Success(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{
		keyHealth: &attunev1.KeyHealthScore{
			Score:             85,
			HasExpiry:         true,
			HasIpRestriction:  true,
			HasRateLimit:      true,
			UnusedScopeCount:  2,
			DaysSinceRotation: 30,
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetKeyHealth(advancedTestRequestContext(), &attunev1.GetKeyHealthRequest{Id: keyID.String()})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, int32(85), result.Body.GetHealth().GetScore())
	require.True(t, result.Body.GetHealth().GetHasExpiry())
	require.True(t, result.Body.GetHealth().GetHasIpRestriction())
	require.True(t, result.Body.GetHealth().GetHasRateLimit())
	require.Equal(t, int32(2), result.Body.GetHealth().GetUnusedScopeCount())
	require.Equal(t, int32(30), result.Body.GetHealth().GetDaysSinceRotation())
}

func TestGetKeyHealth_LowScore(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{
		keyHealth: &attunev1.KeyHealthScore{
			Score:             25,
			HasExpiry:         false,
			HasIpRestriction:  false,
			HasRateLimit:      false,
			UnusedScopeCount:  10,
			DaysSinceRotation: 365,
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetKeyHealth(advancedTestRequestContext(), &attunev1.GetKeyHealthRequest{Id: keyID.String()})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, int32(25), result.Body.GetHealth().GetScore())
	require.False(t, result.Body.GetHealth().GetHasExpiry())
}

func TestGetKeyHealth_BadID(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.GetKeyHealth(advancedTestRequestContext(), &attunev1.GetKeyHealthRequest{Id: "invalid-uuid"})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

func TestGetKeyHealth_NotFound(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{keyHealthErr: apikeyrepo.ErrAPIKeyNotFound}
	h := &APIKeysHandler{svc: svc}

	_, err := h.GetKeyHealth(advancedTestRequestContext(), &attunev1.GetKeyHealthRequest{Id: keyID.String()})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusNotFound, got.Status)
	require.Equal(t, attunev1.ErrorCode_NOT_FOUND, got.Code)
}

func TestGetKeyHealth_ServiceError(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{keyHealthErr: errors.New("db error")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.GetKeyHealth(advancedTestRequestContext(), &attunev1.GetKeyHealthRequest{Id: keyID.String()})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ========== Browser Detection Tests ==========

func TestIsBrowserUserAgent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		ua        string
		isBrowser bool
	}{
		// Browser User-Agents
		{"Chrome", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36", true},
		{"Firefox", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:89.0) Gecko/20100101 Firefox/89.0", true},
		{"Safari", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.1.1 Safari/605.1.15", true},
		{"Edge", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36 Edg/91.0.864.59", true},
		{"Opera", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36 OPR/77.0.4054.254", true},
		{"IE11", "Mozilla/5.0 (Windows NT 10.0; WOW64; Trident/7.0; rv:11.0) like Gecko", true},
		{"IE10", "Mozilla/5.0 (compatible; MSIE 10.0; Windows NT 6.1; Trident/6.0)", true},

		// Non-browser User-Agents
		{"curl", "curl/7.68.0", false},
		{"Python requests", "python-requests/2.25.1", false},
		{"Go http", "Go-http-client/1.1", false},
		{"Node fetch", "node-fetch/1.0", false},
		{"Custom SDK", "attune-sdk/1.0.0", false},
		{"Empty", "", false},
		{"Postman", "PostmanRuntime/7.28.0", false},
		{"wget", "Wget/1.21", false},
		{"httpie", "HTTPie/2.4.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := apikey.IsBrowserUserAgent(tt.ua)
			require.Equal(t, tt.isBrowser, result, "expected IsBrowserUserAgent(%q) = %v", tt.ua, tt.isBrowser)
		})
	}
}
