// ptrext:file-allow test fixtures use handler pointers and proto request captures.
package apikey

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// ---------- parseExpiry ----------

func TestParseExpiry(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		wantNil bool
		check   func(t *testing.T, got *time.Time)
	}{
		{
			name:  "7d",
			input: "7d",
			check: func(t *testing.T, got *time.Time) {
				require.InDelta(t, 7*24, time.Until(ptrext.Indirect(got)).Hours(), 1)
			},
		},
		{
			name:  "30d",
			input: "30d",
			check: func(t *testing.T, got *time.Time) {
				require.InDelta(t, 30*24, time.Until(ptrext.Indirect(got)).Hours(), 1)
			},
		},
		{
			name:  "90d",
			input: "90d",
			check: func(t *testing.T, got *time.Time) {
				require.InDelta(t, 90*24, time.Until(ptrext.Indirect(got)).Hours(), 1)
			},
		},
		{
			name:  "1y",
			input: "1y",
			check: func(t *testing.T, got *time.Time) {
				require.InDelta(t, 365*24, time.Until(ptrext.Indirect(got)).Hours(), 1)
			},
		},
		{
			name:  "RFC3339",
			input: "2030-01-01T00:00:00Z",
			check: func(t *testing.T, got *time.Time) {
				want, _ := time.Parse(time.RFC3339, "2030-01-01T00:00:00Z")
				require.True(t, got.Equal(want))
			},
		},
		{
			name:    "invalid string",
			input:   "invalid",
			wantNil: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := parseExpiry(tt.input)
			if tt.wantNil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			tt.check(t, got)
		})
	}
}

// ---------- parseCreateScopes ----------

func TestParseCreateScopes_EmptyDefaultsToFullAccess(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	scopes, err := h.parseCreateScopes(ctx, &attunev1.CreateApiKeyRequest{
		Label:  "test",
		Scopes: nil,
	})

	require.NoError(t, err)
	require.Equal(t, GetFullAccessScopes(), scopes)
}

func TestParseCreateScopes_Deduplication(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	scopes, err := h.parseCreateScopes(ctx, &attunev1.CreateApiKeyRequest{
		Label:  "test",
		Scopes: []string{"ingest:write", "feedback:read", "ingest:write", "feedback:read"},
	})

	require.NoError(t, err)
	require.Len(t, scopes, 2)
	require.Equal(t, domain.ScopeIngestWrite, scopes[0])
	require.Equal(t, domain.ScopeFeedbackRead, scopes[1])
}

func TestParseCreateScopes_InvalidScope(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	_, err := h.parseCreateScopes(ctx, &attunev1.CreateApiKeyRequest{
		Label:  "test",
		Scopes: []string{"bogus:scope"},
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_REQUEST, got.Code)
}

func TestParseCreateScopes_APIKeyAdminForbidden(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	_, err := h.parseCreateScopes(ctx, &attunev1.CreateApiKeyRequest{
		Label:  "test",
		Scopes: []string{"ingest:write", "apikey:admin"},
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusForbidden, got.Status)
	require.Equal(t, attunev1.ErrorCode_FORBIDDEN, got.Code)
}

func TestParseCreateScopes_ValidSingle(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	scopes, err := h.parseCreateScopes(ctx, &attunev1.CreateApiKeyRequest{
		Label:  "test",
		Scopes: []string{"ingest:write"},
	})

	require.NoError(t, err)
	require.Len(t, scopes, 1)
	require.Equal(t, domain.ScopeIngestWrite, scopes[0])
}

// ---------- parseCreateCIDRs ----------

func TestParseCreateCIDRs_NilReturnsNil(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	cidrs, err := h.parseCreateCIDRs(ctx, &attunev1.CreateApiKeyRequest{
		Label:        "test",
		AllowedCidrs: nil,
	})

	require.NoError(t, err)
	require.Nil(t, cidrs)
}

func TestParseCreateCIDRs_EmptyReturnsNil(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	cidrs, err := h.parseCreateCIDRs(ctx, &attunev1.CreateApiKeyRequest{
		Label:        "test",
		AllowedCidrs: []string{},
	})

	require.NoError(t, err)
	require.Nil(t, cidrs)
}

func TestParseCreateCIDRs_ValidCIDRs(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	cidrs, err := h.parseCreateCIDRs(ctx, &attunev1.CreateApiKeyRequest{
		Label:        "test",
		AllowedCidrs: []string{"10.0.0.0/8", "192.168.1.0/24"},
	})

	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.0/8", "192.168.1.0/24"}, cidrs)
}

func TestParseCreateCIDRs_InvalidCIDR(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	_, err := h.parseCreateCIDRs(ctx, &attunev1.CreateApiKeyRequest{
		Label:        "test",
		AllowedCidrs: []string{"not-a-cidr"},
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_BAD_REQUEST, got.Code)
	require.Contains(t, got.Message, "invalid CIDR")
}

// ---------- parseCreateRateLimit ----------

func TestParseCreateRateLimit_NilReturnsNil(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	rpm, err := h.parseCreateRateLimit(ctx, &attunev1.CreateApiKeyRequest{
		Label:        "test",
		RateLimitRpm: nil,
	})

	require.NoError(t, err)
	require.Nil(t, rpm)
}

func TestParseCreateRateLimit_ValidValue(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	rpm, err := h.parseCreateRateLimit(ctx, &attunev1.CreateApiKeyRequest{
		Label:        "test",
		RateLimitRpm: ptrext.Of(int32(500)),
	})

	require.NoError(t, err)
	require.NotNil(t, rpm)
	require.Equal(t, 500, ptrext.Indirect(rpm))
}

func TestParseCreateRateLimit_BoundaryValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   int32
		wantErr bool
	}{
		{"min valid", 1, false},
		{"max valid", 100000, false},
		{"too low", 0, true},
		{"negative", -1, true},
		{"too high", 100001, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
			ctx := testRequestContext()

			rpm, err := h.parseCreateRateLimit(ctx, &attunev1.CreateApiKeyRequest{
				Label:        "test",
				RateLimitRpm: ptrext.Of(tt.value),
			})

			if tt.wantErr {
				var got *dispatcher.Error
				require.ErrorAs(t, err, &got)
				require.Equal(t, http.StatusBadRequest, got.Status)
			} else {
				require.NoError(t, err)
				require.Equal(t, int(tt.value), ptrext.Indirect(rpm))
			}
		})
	}
}

// ---------- findCreatedKey ----------

func TestFindCreatedKey_Found(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	other := uuid.New()
	svc := &fakeAPIKeysService{
		listRows: []apikeyrepo.APIKeyListRow{
			{ID: other, Label: "Other"},
			{ID: id, Label: "Target", KeyPrefix: "fbk_live_abc"},
		},
	}
	h := &APIKeysHandler{svc: svc}
	ctx := testRequestContext()

	row := h.findCreatedKey(ctx, "tenant-1", id, "test")

	require.Equal(t, id, row.ID)
	require.Equal(t, "Target", row.Label)
	require.Equal(t, "fbk_live_abc", row.KeyPrefix)
}

func TestFindCreatedKey_NotFound(t *testing.T) {
	t.Parallel()

	svc := &fakeAPIKeysService{
		listRows: []apikeyrepo.APIKeyListRow{
			{ID: uuid.New(), Label: "Other"},
		},
	}
	h := &APIKeysHandler{svc: svc}
	ctx := testRequestContext()

	row := h.findCreatedKey(ctx, "tenant-1", uuid.New(), "test")

	require.Equal(t, uuid.Nil, row.ID)
	require.Empty(t, row.Label)
}

func TestFindCreatedKey_ListError(t *testing.T) {
	t.Parallel()

	svc := &fakeAPIKeysService{listErr: errors.New("db down")}
	h := &APIKeysHandler{svc: svc}
	ctx := testRequestContext()

	row := h.findCreatedKey(ctx, "tenant-1", uuid.New(), "test")

	require.Equal(t, uuid.Nil, row.ID)
}

// ---------- recordCreateAudit ----------

type fakeAuditRecorder struct {
	recorded []auditlogsvc.Event
	err      error
}

func (f *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	f.recorded = append(f.recorded, event)
	return f.err
}

func TestRecordCreateAudit_NilAuditSkips(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}, audit: nil}
	ctx := testRequestContext()

	err := h.recordCreateAudit(ctx, ctx.Auth, uuid.New(), apikeyrepo.APIKeyListRow{}, apikeysvc.IssueParams{})

	require.NoError(t, err)
}

func testRequestContextWithRequest() *dispatcher.RequestContext[*session.AuthCtx] {
	req := httptest.NewRequest(http.MethodPost, "/fb/v1/console/api-keys", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	// We need to build a RequestContext that has a request attached.
	// Since the struct has unexported fields for response/request, we use
	// the simpler test context and accept that Request() will be nil.
	return &dispatcher.RequestContext[*session.AuthCtx]{
		Context: req.Context(),
		Auth: &session.AuthCtx{
			TenantID: "tenant-1",
			UserID:   "user-1",
			UserType: "admin",
		},
	}
}

func TestRecordCreateAudit_BasicFields(t *testing.T) {
	t.Parallel()

	recorder := &fakeAuditRecorder{}
	h := &APIKeysHandler{svc: &fakeAPIKeysService{}, audit: recorder}
	ctx := testRequestContextWithRequest()

	keyID := uuid.New()
	row := apikeyrepo.APIKeyListRow{
		ID:        keyID,
		Label:     "My Key",
		KeyPrefix: "fbk_live_xyz",
		IsActive:  true,
	}
	params := apikeysvc.IssueParams{
		TenantID: "tenant-1",
		Label:    "My Key",
	}

	err := h.recordCreateAudit(ctx, ctx.Auth, keyID, row, params)

	require.NoError(t, err)
	require.Len(t, recorder.recorded, 1)

	event := recorder.recorded[0]
	require.Equal(t, "tenant-1", event.TenantID)
	require.Equal(t, "api_key.create", event.Action)
	require.Equal(t, "api_key", event.TargetType)
	require.Equal(t, keyID.String(), event.TargetID)
	require.Equal(t, "Created API key", event.Summary)

	after := event.After.(map[string]any)
	require.Equal(t, keyID.String(), after["id"])
	require.Equal(t, "My Key", after["label"])
	require.Equal(t, "fbk_live_xyz", after["key_prefix"])
	require.Equal(t, true, after["is_active"])
}

func TestRecordCreateAudit_WithOptionalFields(t *testing.T) {
	t.Parallel()

	recorder := &fakeAuditRecorder{}
	h := &APIKeysHandler{svc: &fakeAPIKeysService{}, audit: recorder}
	ctx := testRequestContextWithRequest()

	keyID := uuid.New()
	expiresAt := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	row := apikeyrepo.APIKeyListRow{
		ID:        keyID,
		Label:     "Expiring Key",
		KeyPrefix: "fbk_live_exp",
		IsActive:  true,
	}
	params := apikeysvc.IssueParams{
		TenantID:     "tenant-1",
		Label:        "Expiring Key",
		ExpiresAt:    ptrext.Of(expiresAt),
		AllowedCIDRs: []string{"10.0.0.0/8"},
		RateLimitRPM: ptrext.Of(1000),
	}

	err := h.recordCreateAudit(ctx, ctx.Auth, keyID, row, params)

	require.NoError(t, err)
	require.Len(t, recorder.recorded, 1)

	after := recorder.recorded[0].After.(map[string]any)
	require.Equal(t, expiresAt.Format(time.RFC3339), after["expires_at"])
	require.Equal(t, []string{"10.0.0.0/8"}, after["allowed_cidrs"])
	require.Equal(t, 1000, after["rate_limit_rpm"])
}

func TestRecordCreateAudit_RecorderError(t *testing.T) {
	t.Parallel()

	recorder := &fakeAuditRecorder{err: errors.New("audit write failed")}
	h := &APIKeysHandler{svc: &fakeAPIKeysService{}, audit: recorder}
	ctx := testRequestContextWithRequest()

	err := h.recordCreateAudit(ctx, ctx.Auth, uuid.New(), apikeyrepo.APIKeyListRow{}, apikeysvc.IssueParams{})

	require.Error(t, err)
}

// ---------- validateCreateRequest ----------

func TestValidateCreateRequest_LabelTooLong(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	_, _, err := h.validateCreateRequest(ctx, &attunev1.CreateApiKeyRequest{
		Label: strings.Repeat("x", 201),
	})

	var got *dispatcher.Error
	require.ErrorAs(t, err, &got)
	require.Equal(t, http.StatusBadRequest, got.Status)
	require.Equal(t, attunev1.ErrorCode_LABEL_TOO_LONG, got.Code)
}

func TestValidateCreateRequest_ExpiresIn_Never(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	params, scopes, err := h.validateCreateRequest(ctx, &attunev1.CreateApiKeyRequest{
		Label:     "test",
		ExpiresIn: ptrext.Of("never"),
	})

	require.NoError(t, err)
	require.Nil(t, params.ExpiresAt)
	require.NotEmpty(t, scopes)
}

func TestValidateCreateRequest_ExpiresIn_Duration(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	params, _, err := h.validateCreateRequest(ctx, &attunev1.CreateApiKeyRequest{
		Label:     "test",
		ExpiresIn: ptrext.Of("30d"),
	})

	require.NoError(t, err)
	require.NotNil(t, params.ExpiresAt)
	require.InDelta(t, 30*24, time.Until(ptrext.Indirect(params.ExpiresAt)).Hours(), 1)
}

func TestValidateCreateRequest_TrimsDescription(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	params, _, err := h.validateCreateRequest(ctx, &attunev1.CreateApiKeyRequest{
		Label:       "test",
		Description: ptrext.Of("  my description  "),
	})

	require.NoError(t, err)
	require.Equal(t, "my description", params.Description)
}

func TestValidateCreateRequest_WithCIDRsAndRateLimit(t *testing.T) {
	t.Parallel()

	h := &APIKeysHandler{svc: &fakeAPIKeysService{}}
	ctx := testRequestContext()

	params, _, err := h.validateCreateRequest(ctx, &attunev1.CreateApiKeyRequest{
		Label:        "test",
		AllowedCidrs: []string{"10.0.0.0/8"},
		RateLimitRpm: ptrext.Of(int32(500)),
	})

	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.0/8"}, params.AllowedCIDRs)
	require.NotNil(t, params.RateLimitRPM)
	require.Equal(t, 500, ptrext.Indirect(params.RateLimitRPM))
}

// ---------- auditActor ----------

func TestAuditActor_DefaultsToAdmin(t *testing.T) {
	t.Parallel()

	auth := &session.AuthCtx{
		TenantID: "t1",
		UserID:   "u1",
		UserType: "",
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)

	actor := auditActor(auth, req)

	require.Equal(t, "admin", actor.Type)
	require.Equal(t, "u1", actor.ID)
}

func TestAuditActor_PreservesExplicitType(t *testing.T) {
	t.Parallel()

	auth := &session.AuthCtx{
		TenantID: "t1",
		UserID:   "u1",
		UserType: "oidc",
	}
	req := httptest.NewRequest(http.MethodPost, "/", nil)

	actor := auditActor(auth, req)

	require.Equal(t, "oidc", actor.Type)
	require.Equal(t, "u1", actor.ID)
}
