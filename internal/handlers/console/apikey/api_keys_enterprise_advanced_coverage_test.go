// ptrext:file-allow test-fixtures
// SPDX-License-Identifier: Apache-2.0
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
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	apikeysvc "github.com/Phixsura/attune/internal/service/apikey"
)

// richFake embeds the base fakeAPIKeysService and overrides specific methods
// with configurable return values for success/error path testing.
type richFake struct {
	fakeAPIKeysService

	// GetPolicy
	getPolicyResult *apikeyrepo.Policy
	getPolicyErr    error

	// UpsertPolicy
	upsertPolicyErr    error
	upsertPolicyCalled bool

	// ListProjects
	listProjectsResult []apikeyrepo.Project
	listProjectsErr    error

	// CreateProject
	createProjectResult *apikeyrepo.Project
	createProjectErr    error

	// BindKeyToProject
	bindKeyErr error

	// GetKeyTags
	getKeyTagsResult []apikeyrepo.Tag
	getKeyTagsErr    error

	// SetKeyTags
	setKeyTagsErr error

	// SetKeyBudget
	setKeyBudgetErr error

	// CreateTempToken
	createTempTokenResult *apikeyrepo.TempTokenResult
	createTempTokenErr    error

	// ListApprovalRequests
	listApprovalsResult []apikeyrepo.ApprovalRequest
	listApprovalsErr    error

	// CreateApprovalRequest
	createApprovalResult *apikeyrepo.ApprovalRequest
	createApprovalErr    error

	// ReviewApproval
	reviewApprovalResult *apikeyrepo.ApprovalRequest
	reviewApprovalErr    error

	// ListOAuth2Clients
	listOAuth2Result []apikeyrepo.OAuth2Client
	listOAuth2Err    error

	// CreateOAuth2Client
	createOAuth2Result *apikeyrepo.OAuth2Client
	createOAuth2Secret string
	createOAuth2Err    error

	// GetKeyAnalytics / GetTenantAnalytics
	keyAnalyticsResult    []apikeyrepo.AnalyticsHourly
	keyAnalyticsErr       error
	tenantAnalyticsResult []apikeyrepo.AnalyticsHourly
	tenantAnalyticsErr    error

	// ListSecretManagers
	listSecretMgrResult []apikeyrepo.SecretManagerConfig
	listSecretMgrErr    error

	// CreateSecretManager
	createSecretMgrResult *apikeyrepo.SecretManagerConfig
	createSecretMgrErr    error

	// Rotate
	rotateRaw   string
	rotateNewID uuid.UUID
	rotateErr   error

	// ListRequestLogs
	listLogsResult []apikeyrepo.RequestLogRow
	listLogsErr    error

	// UpdateEnvironment
	updateEnvErr error

	// GetExpiringKeys
	expiringKeysResult []apikeyrepo.APIKeyListRow
	expiringKeysErr    error

	// ListServiceAccounts
	listSvcAccountsResult []apikeyrepo.ServiceAccountRow
	listSvcAccountsErr    error

	// CreateServiceAccount
	createSvcAccountID  uuid.UUID
	createSvcAccountErr error

	// ListEventSubscriptions
	listEventSubsResult []apikeyrepo.EventSubscription
	listEventSubsErr    error

	// CreateEventSubscription
	createEventSubID  uuid.UUID
	createEventSubErr error

	// GetUnresolvedLeaks
	leaksResult []apikeyrepo.LeakDetection
	leaksErr    error
}

func (f *richFake) GetPolicy(_ context.Context, _ string) (*apikeyrepo.Policy, error) {
	return f.getPolicyResult, f.getPolicyErr
}

func (f *richFake) UpsertPolicy(_ context.Context, _ apikeyrepo.Policy) error {
	f.upsertPolicyCalled = true
	return f.upsertPolicyErr
}

func (f *richFake) ListProjects(_ context.Context, _ string) ([]apikeyrepo.Project, error) {
	return f.listProjectsResult, f.listProjectsErr
}

func (f *richFake) CreateProject(_ context.Context, _, _, _ string) (*apikeyrepo.Project, error) {
	return f.createProjectResult, f.createProjectErr
}

func (f *richFake) BindKeyToProject(_ context.Context, _ string, _, _ uuid.UUID) error {
	return f.bindKeyErr
}

func (f *richFake) GetKeyTags(_ context.Context, _ string, _ uuid.UUID) ([]apikeyrepo.Tag, error) {
	return f.getKeyTagsResult, f.getKeyTagsErr
}

func (f *richFake) SetKeyTags(_ context.Context, _ string, _ uuid.UUID, _ []apikeyrepo.Tag) error {
	return f.setKeyTagsErr
}

func (f *richFake) SetKeyBudget(_ context.Context, _ string, _ uuid.UUID, _ float64, _ string) error {
	return f.setKeyBudgetErr
}

func (f *richFake) CreateTempToken(_ context.Context, _ string, _ uuid.UUID, _ time.Duration, _ *int, _ string) (*apikeyrepo.TempTokenResult, error) {
	return f.createTempTokenResult, f.createTempTokenErr
}

func (f *richFake) ListApprovalRequests(_ context.Context, _, _ string) ([]apikeyrepo.ApprovalRequest, error) {
	return f.listApprovalsResult, f.listApprovalsErr
}

func (f *richFake) CreateApprovalRequest(_ context.Context, _ apikeyrepo.ApprovalRequest) (*apikeyrepo.ApprovalRequest, error) {
	return f.createApprovalResult, f.createApprovalErr
}

func (f *richFake) ReviewApproval(_ context.Context, _ string, _ uuid.UUID, _ string, _ bool, _ string) (*apikeyrepo.ApprovalRequest, error) {
	return f.reviewApprovalResult, f.reviewApprovalErr
}

func (f *richFake) ListOAuth2Clients(_ context.Context, _ string) ([]apikeyrepo.OAuth2Client, error) {
	return f.listOAuth2Result, f.listOAuth2Err
}

func (f *richFake) CreateOAuth2Client(_ context.Context, _, _, _ string, _, _ []string) (*apikeyrepo.OAuth2Client, string, error) {
	return f.createOAuth2Result, f.createOAuth2Secret, f.createOAuth2Err
}

func (f *richFake) GetKeyAnalytics(_ context.Context, _ string, _ uuid.UUID, _, _ time.Time) ([]apikeyrepo.AnalyticsHourly, error) {
	return f.keyAnalyticsResult, f.keyAnalyticsErr
}

func (f *richFake) GetTenantAnalytics(_ context.Context, _ string, _, _ time.Time) ([]apikeyrepo.AnalyticsHourly, error) {
	return f.tenantAnalyticsResult, f.tenantAnalyticsErr
}

func (f *richFake) ListSecretManagers(_ context.Context, _ string) ([]apikeyrepo.SecretManagerConfig, error) {
	return f.listSecretMgrResult, f.listSecretMgrErr
}

func (f *richFake) CreateSecretManager(_ context.Context, _, _, _ string, _ map[string]string) (*apikeyrepo.SecretManagerConfig, error) {
	return f.createSecretMgrResult, f.createSecretMgrErr
}

func (f *richFake) Rotate(_ context.Context, _ apikeysvc.RotateParams) (string, uuid.UUID, error) {
	return f.rotateRaw, f.rotateNewID, f.rotateErr
}

func (f *richFake) ListRequestLogs(_ context.Context, _ string, _ uuid.UUID, _ int) ([]apikeyrepo.RequestLogRow, error) {
	return f.listLogsResult, f.listLogsErr
}

func (f *richFake) UpdateEnvironment(_ context.Context, _ string, _ uuid.UUID, _ string) error {
	return f.updateEnvErr
}

func (f *richFake) GetExpiringKeys(_ context.Context, _ time.Duration) ([]apikeyrepo.APIKeyListRow, error) {
	return f.expiringKeysResult, f.expiringKeysErr
}

func (f *richFake) ListServiceAccounts(_ context.Context, _ string) ([]apikeyrepo.ServiceAccountRow, error) {
	return f.listSvcAccountsResult, f.listSvcAccountsErr
}

func (f *richFake) CreateServiceAccount(_ context.Context, _, _, _ string) (uuid.UUID, error) {
	return f.createSvcAccountID, f.createSvcAccountErr
}

func (f *richFake) ListEventSubscriptions(_ context.Context, _ string) ([]apikeyrepo.EventSubscription, error) {
	return f.listEventSubsResult, f.listEventSubsErr
}

func (f *richFake) CreateEventSubscription(_ context.Context, _ string, _ []string, _, _ string) (uuid.UUID, error) {
	return f.createEventSubID, f.createEventSubErr
}

func (f *richFake) GetUnresolvedLeaks(_ context.Context, _ string) ([]apikeyrepo.LeakDetection, error) {
	return f.leaksResult, f.leaksErr
}

// testRequestContextWithUserID returns a RequestContext that also carries a UserID,
// which is needed by handlers that reference auth.UserID (e.g. CreateApprovalRequest).
func testRequestContextWithUserID() *dispatcher.RequestContext[*session.AuthCtx] {
	return &dispatcher.RequestContext[*session.AuthCtx]{
		Context: context.Background(),
		Auth:    &session.AuthCtx{TenantID: "tenant-1", UserID: "user-1"},
	}
}

var errSentinel = errors.New("service failure")

// refTime is a fixed reference time for deterministic assertions.
var refTime = time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)

// ---------- GetPolicy ----------

func TestEnterpriseGetPolicy_Success(t *testing.T) {
	t.Parallel()

	policy := &apikeyrepo.Policy{
		ID:            uuid.New(),
		TenantID:      "tenant-1",
		RequireExpiry: true,
		MaxExpiryDays: ptrext.Of(90),
	}
	svc := &richFake{getPolicyResult: policy}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetPolicy(testRequestContext(), &attunev1.GetPolicyRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.True(t, result.Body.GetPolicy().GetRequireExpiry())
	require.Equal(t, int32(90), result.Body.GetPolicy().GetMaxExpiryDays())
}

func TestEnterpriseGetPolicy_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{getPolicyErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.GetPolicy(testRequestContext(), &attunev1.GetPolicyRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

// ---------- UpdatePolicy ----------

func TestEnterpriseUpdatePolicy_SuccessWithAllOptionalFields(t *testing.T) {
	t.Parallel()

	svc := &richFake{}
	h := &APIKeysHandler{svc: svc}

	result, err := h.UpdatePolicy(testRequestContext(), &attunev1.UpdatePolicyRequest{
		Policy: ptrext.Of(attunev1.ApiKeyPolicy{
			RequireExpiry:            true,
			RequireIpAllowlist:       true,
			RequireDescription:       true,
			MaxExpiryDays:            ptrext.Of(int32(60)),
			MaxKeysPerServiceAccount: ptrext.Of(int32(10)),
			AutoRevokeUnusedDays:     ptrext.Of(int32(45)),
			AllowedEnvironments:      []string{"production", "staging"},
			RequireMfaForCreate:      true,
			RequireApprovalForProd:   true,
		}),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.True(t, svc.upsertPolicyCalled)

	p := result.Body.GetPolicy()
	require.True(t, p.GetRequireExpiry())
	require.True(t, p.GetRequireIpAllowlist())
	require.True(t, p.GetRequireDescription())
	require.Equal(t, int32(60), p.GetMaxExpiryDays())
	require.Equal(t, int32(10), p.GetMaxKeysPerServiceAccount())
	require.Equal(t, int32(45), p.GetAutoRevokeUnusedDays())
	require.Equal(t, []string{"production", "staging"}, p.GetAllowedEnvironments())
	require.True(t, p.GetRequireMfaForCreate())
	require.True(t, p.GetRequireApprovalForProd())
}

func TestEnterpriseUpdatePolicy_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{upsertPolicyErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.UpdatePolicy(testRequestContext(), &attunev1.UpdatePolicyRequest{
		Policy: ptrext.Of(attunev1.ApiKeyPolicy{RequireExpiry: true}),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

// ---------- ListProjects ----------

func TestEnterpriseListProjects_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{
		listProjectsResult: []apikeyrepo.Project{
			{
				ID:          id,
				TenantID:    "tenant-1",
				Name:        "Backend",
				Description: "backend services",
				IsActive:    true,
				CreatedAt:   refTime,
				UpdatedAt:   refTime,
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListProjects(testRequestContext(), &attunev1.ListProjectsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, id.String(), result.Body.GetItems()[0].GetId())
	require.Equal(t, "Backend", result.Body.GetItems()[0].GetName())
}

func TestEnterpriseListProjects_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{listProjectsErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListProjects(testRequestContext(), &attunev1.ListProjectsRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- CreateProject ----------

func TestEnterpriseCreateProject_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{
		createProjectResult: &apikeyrepo.Project{
			ID:          id,
			TenantID:    "tenant-1",
			Name:        "Frontend",
			Description: "frontend apps",
			IsActive:    true,
			CreatedAt:   refTime,
			UpdatedAt:   refTime,
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateProject(testRequestContext(), &attunev1.CreateProjectRequest{
		Name:        "Frontend",
		Description: ptrext.Of("frontend apps"),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, id.String(), result.Body.GetProject().GetId())
	require.Equal(t, "Frontend", result.Body.GetProject().GetName())
}

func TestEnterpriseCreateProject_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{createProjectErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateProject(testRequestContext(), &attunev1.CreateProjectRequest{Name: "Test"})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- BindKeyToProject ----------

func TestEnterpriseBindKeyToProject_Success(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &richFake{
		fakeAPIKeysService: fakeAPIKeysService{
			listRows: []apikeyrepo.APIKeyListRow{{
				ID:        keyID,
				KeyPrefix: "fbk_live_abc",
				Label:     "Bound",
				IsActive:  true,
				CreatedAt: refTime,
			}},
			scopes: []domain.Scope{domain.ScopeIngestWrite},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.BindKeyToProject(testRequestContext(), &attunev1.BindKeyToProjectRequest{
		Id:        keyID.String(),
		ProjectId: uuid.New().String(),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, keyID.String(), result.Body.GetKey().GetId())
}

func TestEnterpriseBindKeyToProject_KeyNotFound(t *testing.T) {
	t.Parallel()

	svc := &richFake{bindKeyErr: apikeyrepo.ErrAPIKeyNotFound}
	h := &APIKeysHandler{svc: svc}

	_, err := h.BindKeyToProject(testRequestContext(), &attunev1.BindKeyToProjectRequest{
		Id:        uuid.New().String(),
		ProjectId: uuid.New().String(),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusNotFound, got.Status)
	require.Equal(t, attunev1.ErrorCode_NOT_FOUND, got.Code)
}

func TestEnterpriseBindKeyToProject_GeneralError(t *testing.T) {
	t.Parallel()

	svc := &richFake{bindKeyErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.BindKeyToProject(testRequestContext(), &attunev1.BindKeyToProjectRequest{
		Id:        uuid.New().String(),
		ProjectId: uuid.New().String(),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- GetKeyTags ----------

func TestEnterpriseGetKeyTags_Success(t *testing.T) {
	t.Parallel()

	svc := &richFake{
		getKeyTagsResult: []apikeyrepo.Tag{
			{Key: "env", Value: "prod"},
			{Key: "team", Value: "platform"},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetKeyTags(testRequestContext(), &attunev1.GetKeyTagsRequest{Id: uuid.New().String()})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetTags(), 2)
	require.Equal(t, "env", result.Body.GetTags()[0].GetKey())
	require.Equal(t, "prod", result.Body.GetTags()[0].GetValue())
}

func TestEnterpriseGetKeyTags_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{getKeyTagsErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.GetKeyTags(testRequestContext(), &attunev1.GetKeyTagsRequest{Id: uuid.New().String()})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- SetKeyTags ----------

func TestEnterpriseSetKeyTags_Success(t *testing.T) {
	t.Parallel()

	svc := &richFake{}
	h := &APIKeysHandler{svc: svc}

	result, err := h.SetKeyTags(testRequestContext(), &attunev1.SetKeyTagsRequest{
		Id: uuid.New().String(),
		Tags: []*attunev1.ApiKeyTag{
			{Key: "env", Value: "staging"},
		},
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetTags(), 1)
	require.Equal(t, "env", result.Body.GetTags()[0].GetKey())
	require.Equal(t, "staging", result.Body.GetTags()[0].GetValue())
}

func TestEnterpriseSetKeyTags_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{setKeyTagsErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.SetKeyTags(testRequestContext(), &attunev1.SetKeyTagsRequest{
		Id:   uuid.New().String(),
		Tags: []*attunev1.ApiKeyTag{{Key: "k", Value: "v"}},
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- SetKeyBudget ----------

func TestEnterpriseSetKeyBudget_Success(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &richFake{
		fakeAPIKeysService: fakeAPIKeysService{
			listRows: []apikeyrepo.APIKeyListRow{{
				ID:        keyID,
				KeyPrefix: "fbk_live_bgt",
				Label:     "Budget Key",
				IsActive:  true,
				CreatedAt: refTime,
			}},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.SetKeyBudget(testRequestContext(), &attunev1.SetKeyBudgetRequest{
		Id:             keyID.String(),
		BudgetLimitUsd: "250.50",
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, keyID.String(), result.Body.GetKey().GetId())
}

func TestEnterpriseSetKeyBudget_KeyNotFound(t *testing.T) {
	t.Parallel()

	svc := &richFake{setKeyBudgetErr: apikeyrepo.ErrAPIKeyNotFound}
	h := &APIKeysHandler{svc: svc}

	_, err := h.SetKeyBudget(testRequestContext(), &attunev1.SetKeyBudgetRequest{
		Id:             uuid.New().String(),
		BudgetLimitUsd: "100.00",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusNotFound, got.Status)
	require.Equal(t, attunev1.ErrorCode_NOT_FOUND, got.Code)
}

func TestEnterpriseSetKeyBudget_GeneralError(t *testing.T) {
	t.Parallel()

	svc := &richFake{setKeyBudgetErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.SetKeyBudget(testRequestContext(), &attunev1.SetKeyBudgetRequest{
		Id:             uuid.New().String(),
		BudgetLimitUsd: "100.00",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- CreateTempToken ----------

func TestEnterpriseCreateTempToken_SuccessWithMaxUses(t *testing.T) {
	t.Parallel()

	expiresAt := refTime.Add(2 * time.Hour)
	svc := &richFake{
		createTempTokenResult: &apikeyrepo.TempTokenResult{
			Token:       "tmp_token_value",
			TokenPrefix: "tmp_",
			ExpiresAt:   expiresAt,
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateTempToken(testRequestContext(), &attunev1.CreateTempTokenRequest{
		Id:        uuid.New().String(),
		ExpiresIn: "2h",
		MaxUses:   ptrext.Of(int32(5)),
		Purpose:   ptrext.Of("ci_pipeline"),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, "tmp_token_value", result.Body.GetToken())
	require.Equal(t, "tmp_", result.Body.GetTokenPrefix())
	require.Equal(t, expiresAt.UTC().Format(time.RFC3339), result.Body.GetExpiresAt())
	require.Equal(t, int32(5), result.Body.GetMaxUses())
}

func TestEnterpriseCreateTempToken_DefaultExpiryAndPurpose(t *testing.T) {
	t.Parallel()

	expiresAt := refTime.Add(time.Hour)
	svc := &richFake{
		createTempTokenResult: &apikeyrepo.TempTokenResult{
			Token:       "tmp_default",
			TokenPrefix: "tmp_",
			ExpiresAt:   expiresAt,
		},
	}
	h := &APIKeysHandler{svc: svc}

	// No ExpiresIn, no MaxUses, no Purpose -- uses defaults (1h, nil, "one_time").
	result, err := h.CreateTempToken(testRequestContext(), &attunev1.CreateTempTokenRequest{
		Id: uuid.New().String(),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, "tmp_default", result.Body.GetToken())
	require.Equal(t, int32(0), result.Body.GetMaxUses()) // maxUses nil => not set in response
}

func TestEnterpriseCreateTempToken_KeyNotFound(t *testing.T) {
	t.Parallel()

	svc := &richFake{createTempTokenErr: apikeyrepo.ErrAPIKeyNotFound}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateTempToken(testRequestContext(), &attunev1.CreateTempTokenRequest{
		Id: uuid.New().String(),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusNotFound, got.Status)
	require.Equal(t, attunev1.ErrorCode_NOT_FOUND, got.Code)
}

func TestEnterpriseCreateTempToken_GeneralError(t *testing.T) {
	t.Parallel()

	svc := &richFake{createTempTokenErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateTempToken(testRequestContext(), &attunev1.CreateTempTokenRequest{
		Id: uuid.New().String(),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

func TestEnterpriseCreateTempToken_InvalidExpiresIn(t *testing.T) {
	t.Parallel()

	svc := &richFake{}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateTempToken(testRequestContext(), &attunev1.CreateTempTokenRequest{
		Id:        uuid.New().String(),
		ExpiresIn: "invalid",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_REQUEST, got.Code)
}

// ---------- ListApprovalRequests ----------

func TestEnterpriseListApprovalRequests_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{
		listApprovalsResult: []apikeyrepo.ApprovalRequest{
			{
				ID:            id,
				TenantID:      "tenant-1",
				RequesterID:   "user-1",
				RequesterType: "admin",
				KeyLabel:      "prod-key",
				Status:        "pending",
				CreatedAt:     refTime,
				ExpiresAt:     refTime.Add(7 * 24 * time.Hour),
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListApprovalRequests(testRequestContext(), &attunev1.ListApprovalRequestsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, id.String(), result.Body.GetItems()[0].GetId())
	require.Equal(t, "pending", result.Body.GetItems()[0].GetStatus())
}

func TestEnterpriseListApprovalRequests_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{listApprovalsErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListApprovalRequests(testRequestContext(), &attunev1.ListApprovalRequestsRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- CreateApprovalRequest ----------

func TestEnterpriseCreateApprovalRequest_SuccessWithExpiryDays(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{
		createApprovalResult: &apikeyrepo.ApprovalRequest{
			ID:                  id,
			TenantID:            "tenant-1",
			RequesterID:         "user-1",
			RequesterType:       "admin",
			KeyLabel:            "new-key",
			RequestedExpiryDays: ptrext.Of(30),
			Status:              "pending",
			CreatedAt:           refTime,
			ExpiresAt:           refTime.Add(7 * 24 * time.Hour),
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateApprovalRequest(testRequestContextWithUserID(), &attunev1.CreateApprovalRequestRequest{
		KeyLabel:            "new-key",
		RequestedExpiryDays: ptrext.Of(int32(30)),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, id.String(), result.Body.GetRequest().GetId())
	require.Equal(t, int32(30), result.Body.GetRequest().GetRequestedExpiryDays())
}

func TestEnterpriseCreateApprovalRequest_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{createApprovalErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateApprovalRequest(testRequestContextWithUserID(), &attunev1.CreateApprovalRequestRequest{
		KeyLabel: "fail-key",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- ReviewApproval ----------

func TestEnterpriseReviewApproval_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	reviewed := refTime.Add(time.Hour)
	svc := &richFake{
		reviewApprovalResult: &apikeyrepo.ApprovalRequest{
			ID:            id,
			TenantID:      "tenant-1",
			RequesterID:   "user-1",
			RequesterType: "admin",
			KeyLabel:      "reviewed-key",
			Status:        "approved",
			ReviewerID:    "reviewer-1",
			ReviewedAt:    ptrext.Of(reviewed),
			CreatedAt:     refTime,
			ExpiresAt:     refTime.Add(7 * 24 * time.Hour),
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ReviewApproval(testRequestContextWithUserID(), &attunev1.ReviewApprovalRequest{
		Id:      id.String(),
		Approve: true,
		Notes:   ptrext.Of("approved for production"),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, "approved", result.Body.GetRequest().GetStatus())
}

func TestEnterpriseReviewApproval_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{reviewApprovalErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ReviewApproval(testRequestContextWithUserID(), &attunev1.ReviewApprovalRequest{
		Id:      uuid.New().String(),
		Approve: false,
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- ListOAuth2Clients ----------

func TestEnterpriseListOAuth2Clients_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{
		listOAuth2Result: []apikeyrepo.OAuth2Client{
			{
				ID:            id,
				TenantID:      "tenant-1",
				ClientID:      "client-abc",
				Name:          "CI Client",
				Description:   "CI/CD pipeline",
				RedirectURIs:  []string{"https://ci.example.com/callback"},
				AllowedScopes: []string{"ingest:write"},
				IsActive:      true,
				CreatedAt:     refTime,
				UpdatedAt:     refTime,
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListOAuth2Clients(testRequestContext(), &attunev1.ListOAuth2ClientsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	c := result.Body.GetItems()[0]
	require.Equal(t, id.String(), c.GetId())
	require.Equal(t, "client-abc", c.GetClientId())
	require.Equal(t, "CI Client", c.GetName())
	require.Equal(t, []string{"https://ci.example.com/callback"}, c.GetRedirectUris())
}

func TestEnterpriseListOAuth2Clients_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{listOAuth2Err: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListOAuth2Clients(testRequestContext(), &attunev1.ListOAuth2ClientsRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- CreateOAuth2Client ----------

func TestEnterpriseCreateOAuth2Client_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{
		createOAuth2Result: &apikeyrepo.OAuth2Client{
			ID:            id,
			TenantID:      "tenant-1",
			ClientID:      "client-new",
			Name:          "New Client",
			Description:   "test client",
			RedirectURIs:  []string{"https://app.example.com/cb"},
			AllowedScopes: []string{"feedback:read"},
			IsActive:      true,
			CreatedAt:     refTime,
			UpdatedAt:     refTime,
		},
		createOAuth2Secret: "secret-xyz-123",
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateOAuth2Client(testRequestContext(), &attunev1.CreateOAuth2ClientRequest{
		Name:          "New Client",
		Description:   ptrext.Of("test client"),
		RedirectUris:  []string{"https://app.example.com/cb"},
		AllowedScopes: []string{"feedback:read"},
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, id.String(), result.Body.GetClient().GetId())
	require.Equal(t, "secret-xyz-123", result.Body.GetClientSecret())
}

func TestEnterpriseCreateOAuth2Client_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{createOAuth2Err: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateOAuth2Client(testRequestContext(), &attunev1.CreateOAuth2ClientRequest{
		Name: "Fail Client",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- GetKeyAnalytics ----------

func TestEnterpriseGetKeyAnalytics_Success(t *testing.T) {
	t.Parallel()

	hour := time.Date(2026, 6, 20, 14, 0, 0, 0, time.UTC)
	svc := &richFake{
		keyAnalyticsResult: []apikeyrepo.AnalyticsHourly{
			{
				KeyID:          uuid.New(),
				TenantID:       "tenant-1",
				Hour:           hour,
				RequestCount:   200,
				ErrorCount:     10,
				TotalLatencyMs: 4000,
				Status2xx:      180,
				Status4xx:      15,
				Status5xx:      5,
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetKeyAnalytics(testRequestContext(), &attunev1.GetKeyAnalyticsRequest{
		Id: uuid.New().String(),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, int64(200), result.Body.GetItems()[0].GetRequestCount())
}

func TestEnterpriseGetKeyAnalytics_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{keyAnalyticsErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.GetKeyAnalytics(testRequestContext(), &attunev1.GetKeyAnalyticsRequest{
		Id: uuid.New().String(),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

func TestEnterpriseGetKeyAnalytics_CustomTimeRange(t *testing.T) {
	t.Parallel()

	svc := &richFake{keyAnalyticsResult: []apikeyrepo.AnalyticsHourly{}}
	h := &APIKeysHandler{svc: svc}

	start := refTime.Format(time.RFC3339)
	end := refTime.Add(24 * time.Hour).Format(time.RFC3339)

	result, err := h.GetKeyAnalytics(testRequestContext(), &attunev1.GetKeyAnalyticsRequest{
		Id:    uuid.New().String(),
		Start: ptrext.Of(start),
		End:   ptrext.Of(end),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Empty(t, result.Body.GetItems())
}

func TestEnterpriseGetKeyAnalytics_InvalidTimeIgnored(t *testing.T) {
	t.Parallel()

	svc := &richFake{keyAnalyticsResult: []apikeyrepo.AnalyticsHourly{}}
	h := &APIKeysHandler{svc: svc}

	// Invalid time format is silently ignored and defaults are used.
	result, err := h.GetKeyAnalytics(testRequestContext(), &attunev1.GetKeyAnalyticsRequest{
		Id:    uuid.New().String(),
		Start: ptrext.Of("not-a-time"),
		End:   ptrext.Of("also-bad"),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
}

// ---------- GetTenantAnalytics ----------

func TestEnterpriseGetTenantAnalytics_Success(t *testing.T) {
	t.Parallel()

	hour := time.Date(2026, 6, 20, 14, 0, 0, 0, time.UTC)
	svc := &richFake{
		tenantAnalyticsResult: []apikeyrepo.AnalyticsHourly{
			{
				KeyID:          uuid.New(),
				TenantID:       "tenant-1",
				Hour:           hour,
				RequestCount:   500,
				ErrorCount:     25,
				TotalLatencyMs: 10000,
				Status2xx:      460,
				Status4xx:      30,
				Status5xx:      10,
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.GetTenantAnalytics(testRequestContext(), &attunev1.GetTenantAnalyticsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, int64(500), result.Body.GetItems()[0].GetRequestCount())
}

func TestEnterpriseGetTenantAnalytics_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{tenantAnalyticsErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.GetTenantAnalytics(testRequestContext(), &attunev1.GetTenantAnalyticsRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

func TestEnterpriseGetTenantAnalytics_CustomTimeRange(t *testing.T) {
	t.Parallel()

	svc := &richFake{tenantAnalyticsResult: []apikeyrepo.AnalyticsHourly{}}
	h := &APIKeysHandler{svc: svc}

	start := refTime.Format(time.RFC3339)
	end := refTime.Add(48 * time.Hour).Format(time.RFC3339)

	result, err := h.GetTenantAnalytics(testRequestContext(), &attunev1.GetTenantAnalyticsRequest{
		Start: ptrext.Of(start),
		End:   ptrext.Of(end),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
}

// ---------- ListSecretManagers ----------

func TestEnterpriseListSecretManagers_SuccessWithSyncFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	syncAt := refTime.Add(time.Hour)
	svc := &richFake{
		listSecretMgrResult: []apikeyrepo.SecretManagerConfig{
			{
				ID:             id,
				TenantID:       "tenant-1",
				ManagerType:    domain.SecretManagerVault,
				Name:           "Corp Vault",
				IsActive:       true,
				LastSyncAt:     ptrext.Of(syncAt),
				LastSyncStatus: "success",
				CreatedAt:      refTime,
				UpdatedAt:      refTime,
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListSecretManagers(testRequestContext(), &attunev1.ListSecretManagersRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	item := result.Body.GetItems()[0]
	require.Equal(t, id.String(), item.GetId())
	require.Equal(t, "hashicorp_vault", item.GetManagerType())
	require.Equal(t, syncAt.UTC().Format(time.RFC3339), item.GetLastSyncAt())
	require.Equal(t, "success", item.GetLastSyncStatus())
}

func TestEnterpriseListSecretManagers_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{listSecretMgrErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListSecretManagers(testRequestContext(), &attunev1.ListSecretManagersRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- CreateSecretManager ----------

func TestEnterpriseCreateSecretManager_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{
		createSecretMgrResult: &apikeyrepo.SecretManagerConfig{
			ID:          id,
			TenantID:    "tenant-1",
			ManagerType: domain.SecretManagerAWSSecretsManager,
			Name:        "AWS SM",
			IsActive:    true,
			CreatedAt:   refTime,
			UpdatedAt:   refTime,
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateSecretManager(testRequestContext(), &attunev1.CreateSecretManagerRequest{
		ManagerType: "aws_secrets_manager",
		Name:        "AWS SM",
		Config:      map[string]string{"region": "us-east-1"},
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, id.String(), result.Body.GetConfig().GetId())
	require.Equal(t, "aws_secrets_manager", result.Body.GetConfig().GetManagerType())
}

func TestEnterpriseCreateSecretManager_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{createSecretMgrErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateSecretManager(testRequestContext(), &attunev1.CreateSecretManagerRequest{
		ManagerType: "vault",
		Name:        "Fail",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- Rotate ----------

func TestAdvancedRotate_SuccessNoGracePeriod(t *testing.T) {
	t.Parallel()

	oldKeyID := uuid.New()
	newKeyID := uuid.New()
	svc := &richFake{
		rotateRaw:   "fbk_live_new_secret",
		rotateNewID: newKeyID,
		fakeAPIKeysService: fakeAPIKeysService{
			listRows: []apikeyrepo.APIKeyListRow{
				{
					ID:        oldKeyID,
					KeyPrefix: "fbk_live_old",
					Label:     "Old Key",
					IsActive:  false,
					CreatedAt: refTime,
				},
				{
					ID:        newKeyID,
					KeyPrefix: "fbk_live_new",
					Label:     "New Key",
					IsActive:  true,
					CreatedAt: refTime.Add(time.Minute),
				},
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.Rotate(testRequestContext(), &attunev1.RotateApiKeyRequest{
		Id: oldKeyID.String(),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, "fbk_live_new_secret", result.Body.GetSecret())
	require.NotEmpty(t, result.Body.GetGracePeriodEndsAt())
	require.NotNil(t, result.Body.GetOldKey())
	require.NotNil(t, result.Body.GetNewKey())
}

func TestAdvancedRotate_SuccessWithGracePeriod(t *testing.T) {
	t.Parallel()

	oldKeyID := uuid.New()
	newKeyID := uuid.New()
	svc := &richFake{
		rotateRaw:   "fbk_live_rotated",
		rotateNewID: newKeyID,
		fakeAPIKeysService: fakeAPIKeysService{
			listRows: []apikeyrepo.APIKeyListRow{
				{ID: oldKeyID, KeyPrefix: "fbk_old", Label: "Old", IsActive: false, CreatedAt: refTime},
				{ID: newKeyID, KeyPrefix: "fbk_new", Label: "New", IsActive: true, CreatedAt: refTime},
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.Rotate(testRequestContext(), &attunev1.RotateApiKeyRequest{
		Id:          oldKeyID.String(),
		GracePeriod: ptrext.Of("48h"),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, "fbk_live_rotated", result.Body.GetSecret())
}

func TestAdvancedRotate_KeyNotFound(t *testing.T) {
	t.Parallel()

	svc := &richFake{rotateErr: apikeyrepo.ErrAPIKeyNotFound}
	h := &APIKeysHandler{svc: svc}

	_, err := h.Rotate(testRequestContext(), &attunev1.RotateApiKeyRequest{
		Id: uuid.New().String(),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusNotFound, got.Status)
	require.Equal(t, attunev1.ErrorCode_NOT_FOUND, got.Code)
}

func TestAdvancedRotate_GeneralError(t *testing.T) {
	t.Parallel()

	svc := &richFake{rotateErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.Rotate(testRequestContext(), &attunev1.RotateApiKeyRequest{
		Id: uuid.New().String(),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
	require.Equal(t, attunev1.ErrorCode_INTERNAL, got.Code)
}

func TestAdvancedRotate_InvalidGracePeriod(t *testing.T) {
	t.Parallel()

	svc := &richFake{}
	h := &APIKeysHandler{svc: svc}

	_, err := h.Rotate(testRequestContext(), &attunev1.RotateApiKeyRequest{
		Id:          uuid.New().String(),
		GracePeriod: ptrext.Of("xyz"),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_REQUEST, got.Code)
}

func TestAdvancedRotate_BadKeyID(t *testing.T) {
	t.Parallel()

	svc := &richFake{}
	h := &APIKeysHandler{svc: svc}

	_, err := h.Rotate(testRequestContext(), &attunev1.RotateApiKeyRequest{Id: "not-uuid"})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

// ---------- ListLogs ----------

func TestAdvancedListLogs_Success(t *testing.T) {
	t.Parallel()

	ts := refTime.Add(2 * time.Hour)
	svc := &richFake{
		listLogsResult: []apikeyrepo.RequestLogRow{
			{
				ID:         1,
				KeyID:      uuid.New(),
				Timestamp:  ts,
				Method:     "POST",
				Path:       "/v1/feedback/ingest",
				StatusCode: 200,
				ClientIP:   "10.0.0.1",
				UserAgent:  "attune-sdk/1.0",
				LatencyMs:  42,
				RequestID:  "req-abc",
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListLogs(testRequestContext(), &attunev1.ListApiKeyLogsRequest{
		Id: uuid.New().String(),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetLogs(), 1)
	logEntry := result.Body.GetLogs()[0]
	require.Equal(t, "POST", logEntry.GetMethod())
	require.Equal(t, "/v1/feedback/ingest", logEntry.GetPath())
	require.Equal(t, int32(200), logEntry.GetStatusCode())
	require.Equal(t, "10.0.0.1", logEntry.GetClientIp())
	require.Equal(t, int32(42), logEntry.GetLatencyMs())
}

func TestAdvancedListLogs_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{listLogsErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListLogs(testRequestContext(), &attunev1.ListApiKeyLogsRequest{
		Id: uuid.New().String(),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

func TestAdvancedListLogs_LimitClampingBelowOne(t *testing.T) {
	t.Parallel()

	svc := &richFake{listLogsResult: []apikeyrepo.RequestLogRow{}}
	h := &APIKeysHandler{svc: svc}

	// Limit 0 should clamp to 1 (below minimum).
	result, err := h.ListLogs(testRequestContext(), &attunev1.ListApiKeyLogsRequest{
		Id:    uuid.New().String(),
		Limit: ptrext.Of(int32(0)),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
}

func TestAdvancedListLogs_LimitClampingAboveMax(t *testing.T) {
	t.Parallel()

	svc := &richFake{listLogsResult: []apikeyrepo.RequestLogRow{}}
	h := &APIKeysHandler{svc: svc}

	// Limit 5000 should clamp to 1000 (above maximum).
	result, err := h.ListLogs(testRequestContext(), &attunev1.ListApiKeyLogsRequest{
		Id:    uuid.New().String(),
		Limit: ptrext.Of(int32(5000)),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
}

// ---------- UpdateEnvironment ----------

func TestAdvancedUpdateEnvironment_Success(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	svc := &richFake{
		fakeAPIKeysService: fakeAPIKeysService{
			listRows: []apikeyrepo.APIKeyListRow{{
				ID:        keyID,
				KeyPrefix: "fbk_live_env",
				Label:     "Env Key",
				IsActive:  true,
				CreatedAt: refTime,
			}},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.UpdateEnvironment(testRequestContext(), &attunev1.UpdateApiKeyEnvironmentRequest{
		Id:          keyID.String(),
		Environment: "staging",
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Equal(t, keyID.String(), result.Body.GetKey().GetId())
}

func TestAdvancedUpdateEnvironment_NotFound(t *testing.T) {
	t.Parallel()

	svc := &richFake{updateEnvErr: apikeyrepo.ErrAPIKeyNotFound}
	h := &APIKeysHandler{svc: svc}

	_, err := h.UpdateEnvironment(testRequestContext(), &attunev1.UpdateApiKeyEnvironmentRequest{
		Id:          uuid.New().String(),
		Environment: "production",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusNotFound, got.Status)
	require.Equal(t, attunev1.ErrorCode_NOT_FOUND, got.Code)
}

func TestAdvancedUpdateEnvironment_InvalidEnvironment(t *testing.T) {
	t.Parallel()

	svc := &richFake{}
	h := &APIKeysHandler{svc: svc}

	_, err := h.UpdateEnvironment(testRequestContext(), &attunev1.UpdateApiKeyEnvironmentRequest{
		Id:          uuid.New().String(),
		Environment: "invalid-env",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_REQUEST, got.Code)
}

// ---------- ListExpiring ----------

func TestAdvancedListExpiring_Success(t *testing.T) {
	t.Parallel()

	keyID := uuid.New()
	expires := refTime.Add(3 * 24 * time.Hour)
	svc := &richFake{
		expiringKeysResult: []apikeyrepo.APIKeyListRow{
			{
				ID:        keyID,
				KeyPrefix: "fbk_live_exp",
				Label:     "Expiring",
				IsActive:  true,
				CreatedAt: refTime,
				ExpiresAt: ptrext.Of(expires),
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListExpiring(testRequestContext(), &attunev1.ListExpiringApiKeysRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, keyID.String(), result.Body.GetItems()[0].GetId())
}

func TestAdvancedListExpiring_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{expiringKeysErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListExpiring(testRequestContext(), &attunev1.ListExpiringApiKeysRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

func TestAdvancedListExpiring_InvalidWithin(t *testing.T) {
	t.Parallel()

	svc := &richFake{}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListExpiring(testRequestContext(), &attunev1.ListExpiringApiKeysRequest{
		Within: ptrext.Of("bad-duration"),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_REQUEST, got.Code)
}

// ---------- ListServiceAccounts ----------

func TestAdvancedListServiceAccounts_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{
		listSvcAccountsResult: []apikeyrepo.ServiceAccountRow{
			{
				ID:          id,
				TenantID:    "tenant-1",
				Name:        "CI Bot",
				Description: "CI/CD service account",
				IsActive:    true,
				CreatedAt:   refTime,
				UpdatedAt:   refTime,
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListServiceAccounts(testRequestContext(), &attunev1.ListServiceAccountsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, id.String(), result.Body.GetItems()[0].GetId())
	require.Equal(t, "CI Bot", result.Body.GetItems()[0].GetName())
}

func TestAdvancedListServiceAccounts_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{listSvcAccountsErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListServiceAccounts(testRequestContext(), &attunev1.ListServiceAccountsRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- CreateServiceAccount ----------

func TestAdvancedCreateServiceAccount_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{createSvcAccountID: id}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateServiceAccount(testRequestContext(), &attunev1.CreateServiceAccountRequest{
		Name:        "Deploy Bot",
		Description: ptrext.Of("deployment automation"),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, id.String(), result.Body.GetServiceAccount().GetId())
	require.Equal(t, "Deploy Bot", result.Body.GetServiceAccount().GetName())
	require.True(t, result.Body.GetServiceAccount().GetIsActive())
}

func TestAdvancedCreateServiceAccount_EmptyName(t *testing.T) {
	t.Parallel()

	svc := &richFake{}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateServiceAccount(testRequestContext(), &attunev1.CreateServiceAccountRequest{
		Name: "  ",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_MISSING_LABEL, got.Code)
}

func TestAdvancedCreateServiceAccount_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{createSvcAccountErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateServiceAccount(testRequestContext(), &attunev1.CreateServiceAccountRequest{
		Name: "Fail Bot",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- ListEventSubscriptions ----------

func TestAdvancedListEventSubscriptions_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{
		listEventSubsResult: []apikeyrepo.EventSubscription{
			{
				ID:         id,
				TenantID:   "tenant-1",
				EventTypes: []string{"key.created", "key.revoked"},
				WebhookURL: "https://hooks.example.com/keys",
				IsActive:   true,
				CreatedAt:  refTime,
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListEventSubscriptions(testRequestContext(), &attunev1.ListEventSubscriptionsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	require.Equal(t, id.String(), result.Body.GetItems()[0].GetId())
	require.Equal(t, []string{"key.created", "key.revoked"}, result.Body.GetItems()[0].GetEventTypes())
}

func TestAdvancedListEventSubscriptions_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{listEventSubsErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListEventSubscriptions(testRequestContext(), &attunev1.ListEventSubscriptionsRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- CreateEventSubscription ----------

func TestAdvancedCreateEventSubscription_Success(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	svc := &richFake{createEventSubID: id}
	h := &APIKeysHandler{svc: svc}

	result, err := h.CreateEventSubscription(testRequestContext(), &attunev1.CreateEventSubscriptionRequest{
		EventTypes: []string{"key.rotated"},
		WebhookUrl: "https://hooks.example.com/events",
		Secret:     ptrext.Of("webhook-secret"),
	})

	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, result.Status)
	require.Equal(t, id.String(), result.Body.GetSubscription().GetId())
	require.Equal(t, []string{"key.rotated"}, result.Body.GetSubscription().GetEventTypes())
	require.True(t, result.Body.GetSubscription().GetIsActive())
}

func TestAdvancedCreateEventSubscription_EmptyTypes(t *testing.T) {
	t.Parallel()

	svc := &richFake{}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateEventSubscription(testRequestContext(), &attunev1.CreateEventSubscriptionRequest{
		EventTypes: []string{},
		WebhookUrl: "https://hooks.example.com",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_REQUEST, got.Code)
}

func TestAdvancedCreateEventSubscription_EmptyURL(t *testing.T) {
	t.Parallel()

	svc := &richFake{}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateEventSubscription(testRequestContext(), &attunev1.CreateEventSubscriptionRequest{
		EventTypes: []string{"key.created"},
		WebhookUrl: "",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_REQUEST, got.Code)
}

func TestAdvancedCreateEventSubscription_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{createEventSubErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.CreateEventSubscription(testRequestContext(), &attunev1.CreateEventSubscriptionRequest{
		EventTypes: []string{"key.created"},
		WebhookUrl: "https://hooks.example.com",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}

// ---------- ListLeakDetections ----------

func TestAdvancedListLeakDetections_SuccessWithResolvedAt(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	keyID := uuid.New()
	resolvedAt := refTime.Add(24 * time.Hour)
	svc := &richFake{
		leaksResult: []apikeyrepo.LeakDetection{
			{
				ID:         id,
				KeyID:      keyID,
				TenantID:   "tenant-1",
				Source:     "github",
				SourceURL:  "https://github.com/org/repo/blob/main/config.yaml",
				DetectedAt: refTime,
				ResolvedAt: ptrext.Of(resolvedAt),
				Resolution: "key_revoked",
			},
		},
	}
	h := &APIKeysHandler{svc: svc}

	result, err := h.ListLeakDetections(testRequestContext(), &attunev1.ListLeakDetectionsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetItems(), 1)
	item := result.Body.GetItems()[0]
	require.Equal(t, id.String(), item.GetId())
	require.Equal(t, keyID.String(), item.GetKeyId())
	require.Equal(t, "github", item.GetSource())
	require.Equal(t, resolvedAt.UTC().Format(time.RFC3339), item.GetResolvedAt())
	require.Equal(t, "key_revoked", item.GetResolution())
}

func TestAdvancedListLeakDetections_ServiceError(t *testing.T) {
	t.Parallel()

	svc := &richFake{leaksErr: errSentinel}
	h := &APIKeysHandler{svc: svc}

	_, err := h.ListLeakDetections(testRequestContext(), &attunev1.ListLeakDetectionsRequest{})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusInternalServerError, got.Status)
}
