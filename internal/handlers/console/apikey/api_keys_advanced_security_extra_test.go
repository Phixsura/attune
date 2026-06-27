// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package apikey

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

// ---------- ListSigningKeys error paths ----------

func TestListSigningKeys_BadID(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.ListSigningKeys(advancedTestRequestContext(), &attunev1.ListSigningKeysRequest{Id: "bad-uuid"})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

func TestListSigningKeys_ServiceError(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{signingKeyErr: errors.New("db error")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListSigningKeys(advancedTestRequestContext(), &attunev1.ListSigningKeysRequest{Id: keyID.String()})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

// ---------- CreateRotationSchedule with defaults ----------

func TestCreateRotationSchedule_DefaultValues(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	// No optional fields set: should use defaults
	result, err := h.CreateRotationSchedule(advancedTestRequestContext(), &attunev1.CreateRotationScheduleRequest{
		Id: keyID.String(),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, keyID, svc.capturedKeyID)
	// Defaults: intervalDays=90, gracePeriodHours=168, autoNotify=true, notifyDaysBefore=[30,7,1]
	require.Equal(t, int32(90), result.Body.GetSchedule().GetRotationIntervalDays())
	require.Equal(t, int32(168), result.Body.GetSchedule().GetGracePeriodHours())
	require.True(t, result.Body.GetSchedule().GetAutoNotify())
	require.Equal(t, []int32{30, 7, 1}, result.Body.GetSchedule().GetNotifyDaysBefore())
}

func TestCreateRotationSchedule_ServiceError(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{rotationScheduleErr: errors.New("db error")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateRotationSchedule(advancedTestRequestContext(), &attunev1.CreateRotationScheduleRequest{
		Id: keyID.String(),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

// ---------- GetUnusedScopes service error ----------

func TestGetUnusedScopes_ServiceError(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{unusedScopesErr: errors.New("db error")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.GetUnusedScopes(advancedTestRequestContext(), &attunev1.GetUnusedScopesRequest{Id: keyID.String()})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

// ---------- CreateSigningKey service error ----------

func TestCreateSigningKey_ServiceError(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{signingKeyErr: errors.New("db error")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateSigningKey(advancedTestRequestContext(), &attunev1.CreateSigningKeyRequest{
		Id:           keyID.String(),
		Algorithm:    "RS256",
		PublicKeyPem: "-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

// ---------- CreateSigningKey with expiry ----------

func TestCreateSigningKey_WithExpiry(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateSigningKey(advancedTestRequestContext(), &attunev1.CreateSigningKeyRequest{
		Id:           keyID.String(),
		Algorithm:    "ES256",
		PublicKeyPem: "-----BEGIN PUBLIC KEY-----\ntest\n-----END PUBLIC KEY-----",
		ExpiresAt:    ptrext.Of("2030-01-01T00:00:00Z"),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.NotEmpty(t, result.Body.GetKey().GetExpiresAt())
}

// ---------- ListManagedIdentities service error ----------

func TestListManagedIdentities_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{managedIdentityErr: errors.New("db error")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListManagedIdentities(advancedTestRequestContext(), &attunev1.ListManagedIdentitiesRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

// ---------- ListSIEMIntegrations service error ----------

func TestListSIEMIntegrations_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{siemIntegrationErr: errors.New("db error")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListSIEMIntegrations(advancedTestRequestContext(), &attunev1.ListSIEMIntegrationsRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

// ---------- ListAIAgentConfigs service error ----------

func TestListAIAgentConfigs_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{aiAgentConfigErr: errors.New("db error")}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListAIAgentConfigs(advancedTestRequestContext(), &attunev1.ListAIAgentConfigsRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

// ---------- CreateManagedIdentity missing name ----------

func TestCreateManagedIdentity_MissingName(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateManagedIdentity(advancedTestRequestContext(), &attunev1.CreateManagedIdentityRequest{
		Name:       "",
		Provider:   "azure",
		ExternalId: "some-id",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_MISSING_LABEL, got.Code)
}

// ---------- CreateSIEMIntegration missing name ----------

func TestCreateSIEMIntegration_MissingName(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &advancedSecurityTestService{}}

	_, err := h.CreateSIEMIntegration(advancedTestRequestContext(), &attunev1.CreateSIEMIntegrationRequest{
		Provider:    "splunk",
		Name:        "",
		EndpointUrl: "https://example.com",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_MISSING_LABEL, got.Code)
}

// ---------- CreateSIEMIntegration default event types ----------

func TestCreateSIEMIntegration_DefaultEventTypes(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateSIEMIntegration(advancedTestRequestContext(), &attunev1.CreateSIEMIntegrationRequest{
		Provider:    "splunk",
		Name:        "Test SIEM",
		EndpointUrl: "https://splunk.example.com",
		EventTypes:  nil, // should use defaults
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	// Default event types include key lifecycle and auth events
	eventTypes := result.Body.GetIntegration().GetEventTypes()
	require.Contains(t, eventTypes, "key.created")
	require.Contains(t, eventTypes, "key.revoked")
	require.Contains(t, eventTypes, "auth.success")
	require.Contains(t, eventTypes, "auth.failure")
}

// ---------- CreateSIEMIntegration with custom batch/flush ----------

func TestCreateSIEMIntegration_CustomBatchFlush(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	batchSize := int32(500)
	flushInterval := int32(120)
	result, err := h.CreateSIEMIntegration(advancedTestRequestContext(), &attunev1.CreateSIEMIntegrationRequest{
		Provider:             "datadog",
		Name:                 "Custom SIEM",
		EndpointUrl:          "https://example.com",
		EventTypes:           []string{"key.created"},
		BatchSize:            &batchSize,
		FlushIntervalSeconds: &flushInterval,
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, int32(500), result.Body.GetIntegration().GetBatchSize())
	require.Equal(t, int32(120), result.Body.GetIntegration().GetFlushIntervalSeconds())
}

// ---------- CreateAIAgentConfig with custom values ----------

func TestCreateAIAgentConfig_CustomValues(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	maxTokens := int32(50000)
	maxRequests := int32(30)
	requireApproval := true
	approvalTimeout := int32(900)
	auditAll := false
	result, err := h.CreateAIAgentConfig(advancedTestRequestContext(), &attunev1.CreateAIAgentConfigRequest{
		AgentType:              "custom_bot",
		Name:                   "Custom Agent",
		AllowedScopes:          []string{"feedback:read"},
		MaxTokensPerRequest:    &maxTokens,
		MaxRequestsPerMinute:   &maxRequests,
		AllowedModels:          []string{"claude-3"},
		RequireHumanApproval:   &requireApproval,
		ApprovalTimeoutSeconds: &approvalTimeout,
		AuditAllRequests:       &auditAll,
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, int32(50000), result.Body.GetConfig().GetMaxTokensPerRequest())
	require.Equal(t, int32(30), result.Body.GetConfig().GetMaxRequestsPerMinute())
	require.True(t, result.Body.GetConfig().GetRequireHumanApproval())
	require.Equal(t, int32(900), result.Body.GetConfig().GetApprovalTimeoutSeconds())
	require.False(t, result.Body.GetConfig().GetAuditAllRequests())
}

// ---------- CreateAIAgentConfig default values ----------

func TestCreateAIAgentConfig_DefaultValues(t *testing.T) {
	t.Parallel()

	svc := &advancedSecurityTestService{}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateAIAgentConfig(advancedTestRequestContext(), &attunev1.CreateAIAgentConfigRequest{
		AgentType: "mcp_server",
		Name:      "Default Agent",
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	// Defaults: maxTokens=100000, maxRequests=60, requireApproval=false, approvalTimeout=300, auditAll=true
	require.Equal(t, int32(100000), result.Body.GetConfig().GetMaxTokensPerRequest())
	require.Equal(t, int32(60), result.Body.GetConfig().GetMaxRequestsPerMinute())
	require.False(t, result.Body.GetConfig().GetRequireHumanApproval())
	require.Equal(t, int32(300), result.Body.GetConfig().GetApprovalTimeoutSeconds())
	require.True(t, result.Body.GetConfig().GetAuditAllRequests())
}

// ---------- GetUnusedScopes with FirstUsedAt ----------

func TestGetUnusedScopes_WithFirstUsedAt(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	now := fixedTime()
	firstUsed := now.Add(-48 * 24 * 365)
	svc := &advancedSecurityTestService{
		unusedScopes: []string{},
		permissionUsage: []apikeyrepo.PermissionUsage{
			{
				Scope:       "feedback:read",
				UsageCount:  100,
				FirstUsedAt: ptrext.Of(firstUsed),
				LastUsedAt:  ptrext.Of(now),
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetUnusedScopes(advancedTestRequestContext(), &attunev1.GetUnusedScopesRequest{Id: keyID.String()})

	require.NoError(t, err)
	require.Len(t, result.Body.GetAllUsage(), 1)
	require.NotEmpty(t, result.Body.GetAllUsage()[0].GetFirstUsedAt())
	require.NotEmpty(t, result.Body.GetAllUsage()[0].GetLastUsedAt())
}

func fixedTime() time.Time {
	return time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
}
