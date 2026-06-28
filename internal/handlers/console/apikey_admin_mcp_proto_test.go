package console

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	consolemcpclient "github.com/Phixsura/attune/internal/handlers/console/mcpclient"
	"github.com/Phixsura/attune/internal/infra/apikey"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
)

type apiKeyMCPProtoClientStore struct {
	client    mcprepo.Client
	nextID    uuid.UUID
	now       time.Time
	revoked   uuid.UUID
	createErr error
}

func (s *apiKeyMCPProtoClientStore) ListByTenant(_ context.Context, tenantID string) ([]mcprepo.Client, error) {
	if s.client.ID == uuid.Nil || s.client.TenantID != tenantID {
		return nil, nil
	}
	return []mcprepo.Client{s.client}, nil
}

func (s *apiKeyMCPProtoClientStore) Create(_ context.Context, p mcprepo.CreateClientParams) (*mcprepo.Client, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	id := s.nextID
	if id == uuid.Nil {
		id = uuid.New()
	}
	createdAt := s.now
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	s.client = mcprepo.Client{
		ID:             id,
		TenantID:       p.TenantID,
		Name:           p.Name,
		RedirectURIs:   append([]string(nil), p.RedirectURIs...),
		Scopes:         append([]string(nil), p.Scopes...),
		ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
		CreatedAt:      createdAt,
		CreatedBy:      p.CreatedBy,
	}
	return ptrext.Of(s.client), nil
}

func (s *apiKeyMCPProtoClientStore) GetByID(_ context.Context, id uuid.UUID) (*mcprepo.Client, error) {
	if s.client.ID != id {
		return nil, mcprepo.ErrClientNotFound
	}
	return ptrext.Of(s.client), nil
}

func (s *apiKeyMCPProtoClientStore) Revoke(_ context.Context, id uuid.UUID) error {
	if s.client.ID != id {
		return mcprepo.ErrClientNotFound
	}
	now := s.now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.revoked = id
	s.client.RevokedAt = ptrext.Of(now)
	return nil
}

func (s *apiKeyMCPProtoClientStore) UpdateGovernance(_ context.Context, p mcprepo.UpdateClientGovernanceParams) (*mcprepo.Client, error) {
	if s.client.ID != p.ID {
		return nil, mcprepo.ErrClientNotFound
	}
	s.client.ToolPolicyMode = p.ToolPolicyMode
	s.client.RateLimitRPM = p.RateLimitRPM
	s.client.RateLimitBurst = p.RateLimitBurst
	return ptrext.Of(s.client), nil
}

func newAPIKeyMCPProtoRouter(handler *consolemcpclient.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(apikey.MiddlewareWithProxies(scopedVerifier{
		tenant: "tenant-1",
		key:    uuid.Nil,
		scopes: []domain.Scope{domain.ScopeMCPClientAdmin},
	}, 0))
	r.Use(withAPIKeySession)
	mountAPIKeyMCPClients(r, handler)
	return r
}

func TestAPIKeyMCPClientsPublicRouteUsesProtoJSONShape(t *testing.T) {
	t.Parallel()

	clientID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	store := ptrext.Of(apiKeyMCPProtoClientStore{
		client: mcprepo.Client{
			ID:             clientID,
			TenantID:       "tenant-1",
			Name:           "agent",
			RedirectURIs:   []string{"https://example.com/callback"},
			Scopes:         []string{domain.MCPScopeRead},
			ToolPolicyMode: domain.MCPToolPolicyModeAllowList,
			CreatedAt:      time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC),
			CreatedBy:      "admin-user",
		},
		nextID: clientID,
		now:    time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC),
	})
	handler := consolemcpclient.NewHandler(store, nil, nil, nil)
	handler.SetConnectionProfile("https://attune.example.com", "")

	router := newAPIKeyMCPProtoRouter(handler)

	req := httptest.NewRequest(http.MethodGet, "/mcp/clients/"+clientID.String(), nil)
	req.Header.Set("X-API-Key", domain.APIKeyPrefix+"mcp-admin")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	body := rec.Body.String()
	require.Contains(t, body, `"redirectUris":["https://example.com/callback"]`)
	require.Contains(t, body, `"toolPolicyMode":"allow_list"`)
	require.Contains(t, body, `"resourceUrl":"https://attune.example.com/mcp/v1"`)
	require.NotContains(t, body, `"redirect_uris"`)
	require.NotContains(t, body, `"tool_policy_mode"`)
	require.NotContains(t, body, `"resource_url"`)
}

func TestAPIKeyMCPClientsPublicRouteAcceptsProtoJSONAndReturnsStandardErrors(t *testing.T) {
	t.Parallel()

	store := ptrext.Of(apiKeyMCPProtoClientStore{
		nextID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		now:    time.Date(2026, 6, 28, 12, 30, 0, 0, time.UTC),
	})
	handler := consolemcpclient.NewHandler(store, nil, nil, nil)
	router := newAPIKeyMCPProtoRouter(handler)

	t.Run("create uses lowerCamelCase public request and response fields", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp/clients", strings.NewReader(
			`{"name":"agent","redirectUris":["https://example.com/callback"],"scopes":["mcp:read"]}`,
		))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", domain.APIKeyPrefix+"mcp-admin")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusCreated, rec.Code)
		require.Contains(t, rec.Body.String(), `"redirectUris":["https://example.com/callback"]`)
		require.NotContains(t, rec.Body.String(), `"redirect_uris"`)
	})

	t.Run("validation failures return the shared error envelope", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp/clients", strings.NewReader(
			`{"name":"agent","redirectUris":[],"scopes":["mcp:read"]}`,
		))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", domain.APIKeyPrefix+"mcp-admin")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, "VALIDATION", body["code"])
		require.NotEqual(t, "validation_error", body["code"])
	})

	t.Run("blank client names return validation instead of a database 500", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp/clients", strings.NewReader(
			`{"name":"   ","redirectUris":["https://example.com/callback"],"scopes":["mcp:read"]}`,
		))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", domain.APIKeyPrefix+"mcp-admin")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, "VALIDATION", body["code"])
		require.Contains(t, body["message"], "name is required")
	})

	t.Run("duplicate client names return conflict instead of internal error", func(t *testing.T) {
		dupStore := ptrext.Of(apiKeyMCPProtoClientStore{
			createErr: mcprepo.ErrClientNameConflict,
		})
		dupHandler := consolemcpclient.NewHandler(dupStore, nil, nil, nil)
		dupRouter := newAPIKeyMCPProtoRouter(dupHandler)

		req := httptest.NewRequest(http.MethodPost, "/mcp/clients", strings.NewReader(
			`{"name":"agent","redirectUris":["https://example.com/callback"],"scopes":["mcp:read"]}`,
		))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", domain.APIKeyPrefix+"mcp-admin")
		rec := httptest.NewRecorder()

		dupRouter.ServeHTTP(rec, req)

		require.Equal(t, http.StatusConflict, rec.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, "CONFLICT", body["code"])
		require.Equal(t, "client name already exists", body["message"])
	})

	t.Run("rate-only update preserves omitted governance fields on the public route", func(t *testing.T) {
		existingClientID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		rpm := 20
		burst := 6
		updateStore := ptrext.Of(apiKeyMCPProtoClientStore{
			client: mcprepo.Client{
				ID:             existingClientID,
				TenantID:       "tenant-1",
				Name:           "agent",
				RedirectURIs:   []string{"https://example.com/callback"},
				Scopes:         []string{domain.MCPScopeRead},
				ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
				RateLimitRPM:   ptrext.Of(rpm),
				RateLimitBurst: ptrext.Of(burst),
				CreatedAt:      time.Date(2026, 6, 28, 12, 30, 0, 0, time.UTC),
				CreatedBy:      "admin-user",
			},
			now: time.Date(2026, 6, 28, 12, 30, 0, 0, time.UTC),
		})
		updateHandler := consolemcpclient.NewHandler(updateStore, nil, nil, nil)
		updateRouter := newAPIKeyMCPProtoRouter(updateHandler)

		req := httptest.NewRequest(http.MethodPatch, "/mcp/clients/"+existingClientID.String(), strings.NewReader(`{"rateLimitRpm":45}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", domain.APIKeyPrefix+"mcp-admin")
		rec := httptest.NewRecorder()

		updateRouter.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), `"toolPolicyMode":"legacy_allow_all"`)
		require.Contains(t, rec.Body.String(), `"rateLimitRpm":45`)
		require.Contains(t, rec.Body.String(), `"rateLimitBurst":6`)
	})

	t.Run("empty patch returns validation instead of a silent no-op", func(t *testing.T) {
		existingClientID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		updateStore := ptrext.Of(apiKeyMCPProtoClientStore{
			client: mcprepo.Client{
				ID:             existingClientID,
				TenantID:       "tenant-1",
				Name:           "agent",
				RedirectURIs:   []string{"https://example.com/callback"},
				Scopes:         []string{domain.MCPScopeRead},
				ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
				CreatedAt:      time.Date(2026, 6, 28, 12, 30, 0, 0, time.UTC),
				CreatedBy:      "admin-user",
			},
			now: time.Date(2026, 6, 28, 12, 30, 0, 0, time.UTC),
		})
		updateHandler := consolemcpclient.NewHandler(updateStore, nil, nil, nil)
		updateRouter := newAPIKeyMCPProtoRouter(updateHandler)

		req := httptest.NewRequest(http.MethodPatch, "/mcp/clients/"+existingClientID.String(), strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", domain.APIKeyPrefix+"mcp-admin")
		rec := httptest.NewRecorder()

		updateRouter.ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, "VALIDATION", body["code"])
		require.Contains(t, body["message"], "at least one of tool_policy_mode, rate_limit_rpm, or rate_limit_burst is required")
	})

	t.Run("missing policies does not silently clear all tool policies", func(t *testing.T) {
		existingClientID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
		replaceStore := ptrext.Of(apiKeyMCPProtoClientStore{
			client: mcprepo.Client{
				ID:             existingClientID,
				TenantID:       "tenant-1",
				Name:           "agent",
				RedirectURIs:   []string{"https://example.com/callback"},
				Scopes:         []string{domain.MCPScopeRead},
				ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
				CreatedAt:      time.Date(2026, 6, 28, 12, 30, 0, 0, time.UTC),
				CreatedBy:      "admin-user",
			},
			now: time.Date(2026, 6, 28, 12, 30, 0, 0, time.UTC),
		})
		replaceHandler := consolemcpclient.NewHandler(
			replaceStore,
			nil,
			nil,
			nil,
		)
		replaceRouter := newAPIKeyMCPProtoRouter(replaceHandler)

		req := httptest.NewRequest(http.MethodPut, "/mcp/clients/"+existingClientID.String()+"/tool-policies", strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", domain.APIKeyPrefix+"mcp-admin")
		rec := httptest.NewRecorder()

		replaceRouter.ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, "VALIDATION", body["code"])
		require.Contains(t, body["message"], "use [] to clear all tool policies")
	})

	t.Run("hostless redirect URIs are rejected on the public route", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp/clients", strings.NewReader(
			`{"name":"agent","redirectUris":["https:callback"],"scopes":["mcp:read"]}`,
		))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", domain.APIKeyPrefix+"mcp-admin")
		rec := httptest.NewRecorder()

		router.ServeHTTP(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)

		var body map[string]any
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Equal(t, "VALIDATION", body["code"])
		require.Contains(t, body["message"], "absolute URI with a host")
	})
}
