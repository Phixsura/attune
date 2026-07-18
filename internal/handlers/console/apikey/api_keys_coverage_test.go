// ptrext:file-allow test-fixtures
package apikey

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/dispatcher"
	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	attunev1 "github.com/Phixsura/attune/internal/proto/attune/v1"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

type noopAPIKeyAuditRecorder struct{}

func (noopAPIKeyAuditRecorder) Record(context.Context, auditlogsvc.Event) error {
	return nil
}

func TestAPIKeysHandlerConstructorAndAuditLogger(t *testing.T) {
	t.Parallel()

	h := NewAPIKeysHandler(nil)
	require.NotNil(t, h)
	require.Nil(t, h.audit)

	recorder := noopAPIKeyAuditRecorder{}
	h.SetAuditLogger(recorder)
	require.Equal(t, recorder, h.audit)
}

// ---------- toProtoPolicy ----------

func TestToProtoPolicy_Nil(t *testing.T) {
	t.Parallel()

	got := toProtoPolicy(nil)
	require.Nil(t, got)
}

func TestToProtoPolicy_AllOptionalFieldsSet(t *testing.T) {
	t.Parallel()

	p := apikeyrepo.Policy{
		ID:                       uuid.New(),
		TenantID:                 "t-1",
		MaxExpiryDays:            ptrext.Of(90),
		RequireExpiry:            true,
		RequireIPAllowlist:       true,
		RequireDescription:       true,
		MaxKeysPerServiceAccount: ptrext.Of(5),
		AllowedEnvironments:      []string{"production", "staging"},
		RequireMFAForCreate:      true,
		RequireApprovalForProd:   true,
		AutoRevokeUnusedDays:     ptrext.Of(30),
	}

	got := toProtoPolicy(&p)

	require.NotNil(t, got)
	require.True(t, got.GetRequireExpiry())
	require.True(t, got.GetRequireIpAllowlist())
	require.True(t, got.GetRequireDescription())
	require.True(t, got.GetRequireMfaForCreate())
	require.True(t, got.GetRequireApprovalForProd())
	require.Equal(t, []string{"production", "staging"}, got.GetAllowedEnvironments())
	require.Equal(t, int32(90), got.GetMaxExpiryDays())
	require.Equal(t, int32(5), got.GetMaxKeysPerServiceAccount())
	require.Equal(t, int32(30), got.GetAutoRevokeUnusedDays())
}

func TestToProtoPolicy_NoOptionalFields(t *testing.T) {
	t.Parallel()

	p := apikeyrepo.Policy{
		TenantID:                 "t-1",
		RequireExpiry:            false,
		MaxExpiryDays:            nil,
		MaxKeysPerServiceAccount: nil,
		AutoRevokeUnusedDays:     nil,
	}

	got := toProtoPolicy(&p)

	require.NotNil(t, got)
	require.False(t, got.GetRequireExpiry())
	require.Equal(t, int32(0), got.GetMaxExpiryDays())
	require.Equal(t, int32(0), got.GetMaxKeysPerServiceAccount())
	require.Equal(t, int32(0), got.GetAutoRevokeUnusedDays())
}

// ---------- toProtoApprovalRequest ----------

func TestToProtoApprovalRequest_Nil(t *testing.T) {
	t.Parallel()

	got := toProtoApprovalRequest(nil)
	require.Nil(t, got)
}

func TestToProtoApprovalRequest_AllFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	expires := created.Add(7 * 24 * time.Hour)
	reviewed := created.Add(2 * time.Hour)

	r := apikeyrepo.ApprovalRequest{
		ID:                   id,
		TenantID:             "t-1",
		RequesterID:          "user-1",
		RequesterType:        "admin",
		KeyLabel:             "prod-key",
		KeyDescription:       "production use",
		RequestedScopes:      []string{"ingest:write", "feedback:read"},
		RequestedEnvironment: "production",
		RequestedExpiryDays:  ptrext.Of(90),
		Justification:        "need for deployment",
		Status:               domain.ApprovalStatus("approved"),
		ReviewerID:           "reviewer-1",
		ReviewerNotes:        "looks good",
		CreatedAt:            created,
		ReviewedAt:           ptrext.Of(reviewed),
		ExpiresAt:            expires,
	}

	got := toProtoApprovalRequest(&r)

	require.NotNil(t, got)
	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, "user-1", got.GetRequesterId())
	require.Equal(t, "admin", got.GetRequesterType())
	require.Equal(t, "prod-key", got.GetKeyLabel())
	require.Equal(t, "production use", got.GetKeyDescription())
	require.Equal(t, []string{"ingest:write", "feedback:read"}, got.GetRequestedScopes())
	require.Equal(t, "production", got.GetRequestedEnvironment())
	require.Equal(t, int32(90), got.GetRequestedExpiryDays())
	require.Equal(t, "need for deployment", got.GetJustification())
	require.Equal(t, "approved", got.GetStatus())
	require.Equal(t, "reviewer-1", got.GetReviewerId())
	require.Equal(t, "looks good", got.GetReviewerNotes())
	require.Equal(t, created.Format(time.RFC3339), got.GetCreatedAt())
	require.Equal(t, reviewed.Format(time.RFC3339), got.GetReviewedAt())
	require.Equal(t, expires.Format(time.RFC3339), got.GetExpiresAt())
}

func TestToProtoApprovalRequest_NoOptionalFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	expires := created.Add(7 * 24 * time.Hour)

	r := apikeyrepo.ApprovalRequest{
		ID:                  id,
		TenantID:            "t-1",
		RequesterID:         "user-1",
		RequesterType:       "admin",
		KeyLabel:            "test-key",
		RequestedExpiryDays: nil,
		ReviewerID:          "",
		ReviewerNotes:       "",
		ReviewedAt:          nil,
		CreatedAt:           created,
		ExpiresAt:           expires,
	}

	got := toProtoApprovalRequest(&r)

	require.NotNil(t, got)
	require.Equal(t, int32(0), got.GetRequestedExpiryDays())
	require.Empty(t, got.GetReviewerId())
	require.Empty(t, got.GetReviewerNotes())
	require.Empty(t, got.GetReviewedAt())
}

// ---------- toProtoAnalytics ----------

func TestToProtoAnalytics_Nil(t *testing.T) {
	t.Parallel()

	got := toProtoAnalytics(nil)
	require.Nil(t, got)
}

func TestToProtoAnalytics_ZeroRequestCount(t *testing.T) {
	t.Parallel()

	hour := time.Date(2026, 6, 20, 14, 0, 0, 0, time.UTC)
	a := apikeyrepo.AnalyticsHourly{
		KeyID:          uuid.New(),
		TenantID:       "t-1",
		Hour:           hour,
		RequestCount:   0,
		ErrorCount:     0,
		TotalLatencyMs: 0,
		Status2xx:      0,
		Status4xx:      0,
		Status5xx:      0,
	}

	got := toProtoAnalytics(&a)

	require.NotNil(t, got)
	require.Equal(t, hour.Format(time.RFC3339), got.GetHour())
	require.Equal(t, int64(0), got.GetRequestCount())
	require.Equal(t, int64(0), got.GetErrorCount())
	require.Equal(t, int64(0), got.GetAvgLatencyMs())
	require.Equal(t, int32(0), got.GetStatus_2Xx())
	require.Equal(t, int32(0), got.GetStatus_4Xx())
	require.Equal(t, int32(0), got.GetStatus_5Xx())
}

func TestToProtoAnalytics_NonZeroRequestCount(t *testing.T) {
	t.Parallel()

	hour := time.Date(2026, 6, 20, 14, 0, 0, 0, time.UTC)
	a := apikeyrepo.AnalyticsHourly{
		KeyID:          uuid.New(),
		TenantID:       "t-1",
		Hour:           hour,
		RequestCount:   100,
		ErrorCount:     5,
		TotalLatencyMs: 5000,
		Status2xx:      90,
		Status4xx:      7,
		Status5xx:      3,
	}

	got := toProtoAnalytics(&a)

	require.NotNil(t, got)
	require.Equal(t, int64(100), got.GetRequestCount())
	require.Equal(t, int64(5), got.GetErrorCount())
	require.Equal(t, int64(50), got.GetAvgLatencyMs()) // 5000 / 100
	require.Equal(t, int32(90), got.GetStatus_2Xx())
	require.Equal(t, int32(7), got.GetStatus_4Xx())
	require.Equal(t, int32(3), got.GetStatus_5Xx())
}

// ---------- parseGracePeriod ----------

func TestParseGracePeriod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "hours", input: "1h", want: time.Hour},
		{name: "multiple hours", input: "24h", want: 24 * time.Hour},
		{name: "days", input: "7d", want: 7 * 24 * time.Hour},
		{name: "single day", input: "1d", want: 24 * time.Hour},
		{name: "go duration with minutes", input: "2h30m", want: 2*time.Hour + 30*time.Minute},
		{name: "zero hours returns zero duration", input: "0h", want: 0},
		{name: "zero days returns zero duration", input: "0d", want: 0},
		{name: "non-numeric hours", input: "abch", wantErr: true},
		{name: "non-numeric days", input: "abcd", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
		{name: "whitespace hours", input: "  3h  ", want: 3 * time.Hour},
		{name: "uppercase trimmed", input: "  5D  ", want: 5 * 24 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseGracePeriod(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// ---------- toProtoAPIKeyFromRow ----------

func TestToProtoAPIKeyFromRow_Nil(t *testing.T) {
	t.Parallel()

	got := toProtoAPIKeyFromRow(nil, nil)
	require.Nil(t, got)
}

func TestToProtoAPIKeyFromRow_AllOptionalFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)
	lastUsed := created.Add(2 * time.Hour)
	revoked := created.Add(48 * time.Hour)
	expires := created.Add(90 * 24 * time.Hour)

	row := apikeyrepo.APIKeyListRow{
		ID:           id,
		KeyPrefix:    "fbk_live_full",
		Label:        "Full Row",
		Description:  "a key with all fields",
		IsActive:     false,
		CreatedAt:    created,
		LastUsedAt:   ptrext.Of(lastUsed),
		RevokedAt:    ptrext.Of(revoked),
		ExpiresAt:    ptrext.Of(expires),
		UsageCount:   42,
		AllowedCIDRs: []string{"10.0.0.0/8", "192.168.0.0/16"},
		RateLimitRPM: ptrext.Of(500),
	}
	scopes := []domain.Scope{domain.ScopeIngestWrite, domain.ScopeFeedbackRead}

	got := toProtoAPIKeyFromRow(&row, scopes)

	require.NotNil(t, got)
	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, "fbk_live_full", got.GetKeyPrefix())
	require.Equal(t, "Full Row", got.GetLabel())
	require.Equal(t, "a key with all fields", got.GetDescription())
	require.False(t, got.GetIsActive())
	require.Equal(t, created.Format(time.RFC3339), got.GetCreatedAt())
	require.Equal(t, lastUsed.Format(time.RFC3339), got.GetLastUsedAt())
	require.Equal(t, revoked.Format(time.RFC3339), got.GetRevokedAt())
	require.Equal(t, expires.Format(time.RFC3339), got.GetExpiresAt())
	require.Equal(t, int64(42), got.GetUsageCount())
	require.Equal(t, []string{"10.0.0.0/8", "192.168.0.0/16"}, got.GetAllowedCidrs())
	require.Equal(t, int32(500), got.GetRateLimitRpm())
	require.Equal(t, []string{"ingest:write", "feedback:read"}, got.GetScopes())
	require.Equal(t, "production", got.GetEnvironment())
}

func TestToProtoAPIKeyFromRow_MinimalFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	created := time.Date(2026, 6, 20, 10, 0, 0, 0, time.UTC)

	row := apikeyrepo.APIKeyListRow{
		ID:        id,
		KeyPrefix: "fbk_live_min",
		Label:     "Minimal",
		IsActive:  true,
		CreatedAt: created,
	}

	got := toProtoAPIKeyFromRow(&row, nil)

	require.NotNil(t, got)
	require.Equal(t, id.String(), got.GetId())
	require.Equal(t, "Minimal", got.GetLabel())
	require.True(t, got.GetIsActive())
	require.Empty(t, got.GetLastUsedAt())
	require.Empty(t, got.GetRevokedAt())
	require.Empty(t, got.GetExpiresAt())
	require.Equal(t, int32(0), got.GetRateLimitRpm())
	require.Empty(t, got.GetScopes())
}

// ---------- ListScopes handler ----------

func TestListScopes_ReturnsNonEmpty(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}

	result, err := h.ListScopes(testRequestContext(), &attunev1.ListScopesRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.NotEmpty(t, result.Body.GetScopes())

	var withDescription int
	for _, s := range result.Body.GetScopes() {
		require.NotEmpty(t, s.GetScope(), "scope string must not be empty")
		require.NotEmpty(t, s.GetResource(), "resource must not be empty")
		require.NotEmpty(t, s.GetAction(), "action must not be empty")
		if s.GetDescription() != "" {
			withDescription++
		}
	}
	// Most scopes have descriptions; at least the core set should.
	require.Greater(t, withDescription, 10)
}

// ---------- ListScopePresets handler ----------

func TestListScopePresets_ReturnsPresets(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}

	result, err := h.ListScopePresets(testRequestContext(), &attunev1.ListScopePresetsRequest{})

	require.NoError(t, err)
	require.Equal(t, http.StatusOK, result.Status)
	require.Len(t, result.Body.GetPresets(), len(presetsData))

	// Verify the ingest_only preset exists and is correct.
	var found bool
	for _, p := range result.Body.GetPresets() {
		if p.GetId() == "ingest_only" {
			found = true
			require.Equal(t, "Ingest Only", p.GetName())
			require.Equal(t, "SDK/webhook submission only", p.GetDescription())
			require.Equal(t, []string{string(domain.ScopeIngestWrite)}, p.GetScopes())
			break
		}
	}
	require.True(t, found, "ingest_only preset must be present")
}

// ---------- Enterprise handler validation paths ----------

func newEnterpriseTestHandler() *APIKeysHandler {
	return &APIKeysHandler{svc: &fakeAPIKeysService{}}
}

func TestCreateProject_EmptyName(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.CreateProject(testRequestContext(), &attunev1.CreateProjectRequest{Name: ""})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_MISSING_LABEL, got.Code)
}

func TestBindKeyToProject_InvalidKeyID(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.BindKeyToProject(testRequestContext(), &attunev1.BindKeyToProjectRequest{
		Id:        "not-a-uuid",
		ProjectId: uuid.New().String(),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

func TestBindKeyToProject_InvalidProjectID(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.BindKeyToProject(testRequestContext(), &attunev1.BindKeyToProjectRequest{
		Id:        uuid.New().String(),
		ProjectId: "not-a-uuid",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

func TestGetKeyTags_InvalidID(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.GetKeyTags(testRequestContext(), &attunev1.GetKeyTagsRequest{Id: "bad"})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

func TestSetKeyTags_InvalidID(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.SetKeyTags(testRequestContext(), &attunev1.SetKeyTagsRequest{Id: "bad"})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

func TestSetKeyBudget_Validation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		id       string
		budget   string
		wantCode attunev1.ErrorCode
	}{
		{
			name:     "invalid UUID",
			id:       "bad-id",
			budget:   "100.00",
			wantCode: attunev1.ErrorCode_BAD_ID,
		},
		{
			name:     "empty budget",
			id:       uuid.New().String(),
			budget:   "",
			wantCode: attunev1.ErrorCode_BAD_REQUEST,
		},
		{
			name:     "negative budget",
			id:       uuid.New().String(),
			budget:   "-10.00",
			wantCode: attunev1.ErrorCode_BAD_REQUEST,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newEnterpriseTestHandler()

			_, err := h.SetKeyBudget(testRequestContext(), &attunev1.SetKeyBudgetRequest{
				Id:             tt.id,
				BudgetLimitUsd: tt.budget,
			})

			var got *dispatcher.Error
			require.ErrorAs(t, err, &got)
			require.Equal(t, http.StatusBadRequest, got.Status)
			require.Equal(t, tt.wantCode, got.Code)
		})
	}
}

func TestCreateTempToken_InvalidID(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.CreateTempToken(testRequestContext(), &attunev1.CreateTempTokenRequest{Id: "bad"})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

func TestReviewApproval_InvalidID(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.ReviewApproval(testRequestContext(), &attunev1.ReviewApprovalRequest{Id: "bad"})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_ID, got.Code)
}

func TestCreateOAuth2Client_EmptyName(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.CreateOAuth2Client(testRequestContext(), &attunev1.CreateOAuth2ClientRequest{Name: ""})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_MISSING_LABEL, got.Code)
}

func TestCreateApprovalRequest_EmptyLabel(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.CreateApprovalRequest(testRequestContext(), &attunev1.CreateApprovalRequestRequest{KeyLabel: ""})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_MISSING_LABEL, got.Code)
}

func TestCreateSecretManager_EmptyType(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.CreateSecretManager(testRequestContext(), &attunev1.CreateSecretManagerRequest{
		ManagerType: "",
		Name:        "vault",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_REQUEST, got.Code)
}

func TestCreateSecretManager_EmptyName(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.CreateSecretManager(testRequestContext(), &attunev1.CreateSecretManagerRequest{
		ManagerType: "vault",
		Name:        "",
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_MISSING_LABEL, got.Code)
}

func TestUpdatePolicy_NilPolicy(t *testing.T) {
	t.Parallel()

	h := newEnterpriseTestHandler()

	_, err := h.UpdatePolicy(testRequestContext(), &attunev1.UpdatePolicyRequest{Policy: nil})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_REQUEST, got.Code)
}
