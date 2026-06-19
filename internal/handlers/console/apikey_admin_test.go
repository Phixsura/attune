package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/apikey"
)

// scopedVerifier is a stub apikey.Verifier that authenticates every request as a
// fixed tenant/key carrying a fixed scope set, so a test can drive the scope
// gate without a database or real key store.
type scopedVerifier struct {
	tenant string
	key    uuid.UUID
	scopes []domain.Scope
}

func (v scopedVerifier) Lookup(context.Context, string) (string, uuid.UUID, error) {
	return v.tenant, v.key, nil
}

func (v scopedVerifier) LookupWithScopes(context.Context, string) (string, uuid.UUID, []domain.Scope, error) {
	return v.tenant, v.key, v.scopes, nil
}

func (v scopedVerifier) LookupWithScopesAndIP(context.Context, string, string) (string, uuid.UUID, []domain.Scope, *int, error) {
	return v.tenant, v.key, v.scopes, nil, nil
}

// TestApikeyToSessionMapsIdentity verifies the apikey→session adapter maps the
// authenticated key identity onto the AuthCtx the console handlers consume, with
// the actor recorded as "apikey:<keyID>".
func TestApikeyToSessionMapsIdentity(t *testing.T) {
	t.Parallel()
	keyID := "11111111-1111-1111-1111-111111111111"
	ctx := apikey.WithAuthForTest(context.Background(), "tenant-7", keyID, []domain.Scope{domain.ScopeTagsWrite})
	req := httptest.NewRequest(http.MethodGet, "/tags", nil).WithContext(ctx)

	auth, err := apikeyToSession(req)
	require.NoError(t, err)
	require.Equal(t, "tenant-7", auth.TenantID)
	require.Equal(t, "apikey:"+keyID, auth.UserID)
	require.Equal(t, "admin", auth.UserType)
}

// TestApikeyToSessionFailsClosedWithoutAuth verifies the adapter fails closed
// (401, no panic) if ever reached without the API-key middleware having run.
func TestApikeyToSessionFailsClosedWithoutAuth(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/tags", nil) // bare context, no auth
	auth, err := apikeyToSession(req)
	require.Nil(t, auth)
	require.Error(t, err)
}

// TestAPIKeyAdminRoutesScopeGated drives every mounted tag/workflow route with a
// key that holds only ingest:write and asserts each is rejected at the scope
// gate (403 FORBIDDEN) before any handler — and therefore any DB — is reached.
// A nil pool is safe precisely because the request never gets past RequireScope.
func TestAPIKeyAdminRoutesScopeGated(t *testing.T) {
	t.Parallel()
	v := scopedVerifier{tenant: "tenant-1", key: uuid.Nil, scopes: []domain.Scope{domain.ScopeIngestWrite}}
	r := chi.NewRouter()
	MountAPIKeyAdminRoutes(r, nil, v, 0)

	cases := []struct {
		name, method, path string
	}{
		{"list tags needs tags:read", http.MethodGet, "/tags"},
		{"create tag needs tags:write", http.MethodPost, "/tags"},
		{"update tag needs tags:write", http.MethodPatch, "/tags/t1"},
		{"archive tag needs tags:write", http.MethodDelete, "/tags/t1"},
		{"list states needs workflow:read", http.MethodGet, "/workflow/states"},
		{"create state needs workflow:write", http.MethodPost, "/workflow/states"},
		{"update state needs workflow:write", http.MethodPatch, "/workflow/states/s1"},
		{"archive state needs workflow:write", http.MethodDelete, "/workflow/states/s1"},
		{"list transitions needs workflow:read", http.MethodGet, "/workflow/transitions"},
		{"replace transitions needs workflow:write", http.MethodPut, "/workflow/transitions"},
		{"seed needs workflow:write", http.MethodPost, "/workflow/seed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("X-API-Key", domain.APIKeyPrefix+"restricted")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusForbidden, w.Code)
			var body struct {
				Code string `json:"code"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.Equal(t, "FORBIDDEN", body.Code)
		})
	}
}

// TestAPIKeyAdminRoutesRejectMissingKey verifies the mounted group is behind the
// API-key middleware: a request with no key is rejected 401 before any handler.
func TestAPIKeyAdminRoutesRejectMissingKey(t *testing.T) {
	t.Parallel()
	v := scopedVerifier{tenant: "tenant-1", key: uuid.Nil, scopes: domain.AllScopes}
	r := chi.NewRouter()
	MountAPIKeyAdminRoutes(r, nil, v, 0)

	req := httptest.NewRequest(http.MethodGet, "/tags", nil) // no X-API-Key
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}
