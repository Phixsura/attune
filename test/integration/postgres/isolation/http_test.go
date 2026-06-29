//go:build integration

package isolation

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console"
	"github.com/Phixsura/attune/internal/infra/ratelimit"
)

// httpEnv bundles the test server and two raw API key strings that the
// stubVerifier maps to tenant A / tenant B.
type httpEnv struct {
	Server  *httptest.Server
	APIKeyA string
	APIKeyB string
	Fixture *Fixture
}

// stubVerifier implements apikey.Verifier by mapping two fixed raw-key
// strings to the fixture's two tenants, each with full scopes.
type stubVerifier struct {
	keys map[string]stubIdentity
}

type stubIdentity struct {
	tenantID string
	keyID    uuid.UUID
	scopes   []domain.Scope
}

func (v *stubVerifier) Lookup(_ context.Context, raw string) (string, uuid.UUID, error) {
	id, ok := v.keys[raw]
	if !ok {
		return "", uuid.Nil, domain.ErrInvalidAPIKey
	}
	return id.tenantID, id.keyID, nil
}

func (v *stubVerifier) LookupWithScopes(_ context.Context, raw string) (string, uuid.UUID, []domain.Scope, error) {
	id, ok := v.keys[raw]
	if !ok {
		return "", uuid.Nil, nil, domain.ErrInvalidAPIKey
	}
	return id.tenantID, id.keyID, id.scopes, nil
}

func (v *stubVerifier) LookupWithScopesAndIP(_ context.Context, raw, _ string) (string, uuid.UUID, []domain.Scope, *int, error) {
	id, ok := v.keys[raw]
	if !ok {
		return "", uuid.Nil, nil, nil, domain.ErrInvalidAPIKey
	}
	return id.tenantID, id.keyID, id.scopes, nil, nil
}

// newHTTPEnv creates a real httptest.Server backed by the fixture's
// Postgres pool, with API-key admin routes mounted on a chi router.
func newHTTPEnv(t *testing.T) *httpEnv {
	t.Helper()

	f := NewFixture(t)

	keyA := domain.APIKeyPrefix + "iso_alpha_test_key_00000001"
	keyB := domain.APIKeyPrefix + "iso_bravo_test_key_00000002"

	verifier := &stubVerifier{
		keys: map[string]stubIdentity{
			keyA: {tenantID: f.TenantA.TenantID, keyID: f.TenantA.APIKeyID, scopes: domain.AllScopes},
			keyB: {tenantID: f.TenantB.TenantID, keyID: f.TenantB.APIKeyID, scopes: domain.AllScopes},
		},
	}

	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	console.MountAPIKeyAdminRoutes(r, f.Pool, verifier, 0, ratelimit.NewPerKey(true, nil), console.APIKeyAdminRouteOptions{})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &httpEnv{
		Server:  srv,
		APIKeyA: keyA,
		APIKeyB: keyB,
		Fixture: f,
	}
}

// doGet issues a GET to the test server with the given API key and path.
func doGet(t *testing.T, env *httpEnv, rawKey, path string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.Server.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("X-API-Key", rawKey)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

// doPost issues a POST to the test server with the given API key, path, and JSON body.
func doPost(t *testing.T, env *httpEnv, rawKey, path string, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, env.Server.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("X-API-Key", rawKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, respBody
}

// doPatch issues a PATCH to the test server with the given API key, path, and JSON body.
func doPatch(t *testing.T, env *httpEnv, rawKey, path string, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPatch, env.Server.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("X-API-Key", rawKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, respBody
}

// doDelete issues a DELETE to the test server with the given API key and path.
func doDelete(t *testing.T, env *httpEnv, rawKey, path string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, env.Server.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("X-API-Key", rawKey)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, body
}

// doPut issues a PUT to the test server with the given API key, path, and JSON body.
func doPut(t *testing.T, env *httpEnv, rawKey, path string, body string) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, env.Server.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("X-API-Key", rawKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, respBody
}

// containsAny reports whether body contains any of the non-empty needles.
func containsAny(body []byte, needles ...string) bool {
	s := string(body)
	for _, n := range needles {
		if n != "" && strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// TestHTTP_APIKey_CrossTenantListsDenied verifies that every list endpoint
// returns only the requesting tenant's data. Tenant A's key must never see
// tenant B's resource IDs in any list response.
func TestHTTP_APIKey_CrossTenantListsDenied(t *testing.T) {
	env := newHTTPEnv(t)

	// IDs that must never appear in tenant A's list responses.
	forbiddenIDs := []string{
		env.Fixture.TenantB.TenantID,
		env.Fixture.TenantB.TagID.String(),
		env.Fixture.TenantB.WorkflowID,
		env.Fixture.TenantB.APIKeyID.String(),
		env.Fixture.TenantB.NotifyID.String(),
		env.Fixture.TenantB.MCPClientID.String(),
		env.Fixture.TenantB.FeedbackJobID,
		env.Fixture.TenantB.AuditEvidenceID,
	}

	listEndpoints := []struct {
		name string
		path string
	}{
		{"tags", "/tags"},
		{"workflow/states", "/workflow/states"},
		{"workflow/transitions", "/workflow/transitions"},
		{"audit-log", "/audit-log"},
		{"outbox/deliveries", "/outbox/deliveries"},
		{"mcp/clients", "/mcp/clients"},
		{"gdpr/requests", "/gdpr/requests"},
		{"gdpr/operations", "/gdpr/operations"},
	}

	for _, ep := range listEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			status, body := doGet(t, env, env.APIKeyA, ep.path)

			// A 200 is expected for well-formed list calls. If the handler
			// panics (nil service dep), Recoverer turns it into 500 — still
			// fine as long as no tenant B data appears.
			if status == http.StatusOK {
				if containsAny(body, forbiddenIDs...) {
					t.Errorf("list %s leaked tenant B data in 200 response:\n%s", ep.name, string(body))
				}
			} else {
				// Non-200 (e.g. 500 from a nil dependency): verify the
				// error body still doesn't leak tenant B identifiers.
				if containsAny(body, forbiddenIDs...) {
					t.Errorf("list %s leaked tenant B data in %d response:\n%s", ep.name, status, string(body))
				}
			}
		})
	}
}

// TestHTTP_APIKey_CrossTenantGetDenied verifies that fetching a single
// resource belonging to tenant B while authenticated as tenant A either
// returns 404, an empty/error body, or a non-200 status — never the
// other tenant's data.
func TestHTTP_APIKey_CrossTenantGetDenied(t *testing.T) {
	env := newHTTPEnv(t)

	// Build cross-tenant GET requests: tenant A's key, tenant B's resource ID.
	getEndpoints := []struct {
		name string
		path string
	}{
		{"audit-log/evidence/{job_id}", fmt.Sprintf("/audit-log/evidence/%s", env.Fixture.TenantB.AuditEvidenceID)},
		{"mcp/clients/{id}", fmt.Sprintf("/mcp/clients/%s", env.Fixture.TenantB.MCPClientID.String())},
		{"gdpr/exports/{job_id}", fmt.Sprintf("/gdpr/exports/%s", env.Fixture.TenantB.AuditEvidenceID)},
	}

	// IDs that must never appear in tenant A's cross-tenant get responses.
	forbiddenIDs := []string{
		env.Fixture.TenantB.TenantID,
		env.Fixture.TenantB.TagID.String(),
		env.Fixture.TenantB.WorkflowID,
		env.Fixture.TenantB.APIKeyID.String(),
		env.Fixture.TenantB.NotifyID.String(),
		env.Fixture.TenantB.MCPClientID.String(),
		env.Fixture.TenantB.FeedbackJobID,
		env.Fixture.TenantB.AuditEvidenceID,
	}

	for _, ep := range getEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			status, body := doGet(t, env, env.APIKeyA, ep.path)

			if status == http.StatusOK {
				// A 200 for a cross-tenant resource is a breach if the
				// body contains any of tenant B's identifiers.
				if containsAny(body, forbiddenIDs...) {
					t.Errorf("GET %s returned 200 with tenant B data:\n%s", ep.path, string(body))
				}
			}
			// Non-200 (404, 500, etc.) is the expected isolation behavior.
			// Still verify the error body doesn't leak IDs.
			if containsAny(body, forbiddenIDs...) {
				t.Errorf("GET %s leaked tenant B data in %d response:\n%s", ep.path, status, string(body))
			}
		})
	}
}

// TestHTTP_APIKey_CrossTenantWritesDenied verifies that tenant A's API key
// cannot modify (POST/PATCH/DELETE) tenant B's resources.
func TestHTTP_APIKey_CrossTenantWritesDenied(t *testing.T) {
	env := newHTTPEnv(t)

	// Attempts to modify tenant B's resources using tenant A's key.
	writeEndpoints := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		// Tags: update + archive cross-tenant
		{"PATCH /tags/{id}", "PATCH",
			fmt.Sprintf("/tags/%s", env.Fixture.TenantB.TagID),
			`{"name":"hijacked","color":"#aabbcc"}`},
		{"DELETE /tags/{id}", "DELETE",
			fmt.Sprintf("/tags/%s", env.Fixture.TenantB.TagID), ""},

		// Workflow: update + archive cross-tenant
		{"PATCH /workflow/states/{id}", "PATCH",
			fmt.Sprintf("/workflow/states/%s", env.Fixture.TenantB.WorkflowID),
			`{"name":"hijacked"}`},
		{"DELETE /workflow/states/{id}", "DELETE",
			fmt.Sprintf("/workflow/states/%s", env.Fixture.TenantB.WorkflowID), ""},

		// Outbox: retry cross-tenant
		{"POST /outbox/{id}/retry", "POST",
			fmt.Sprintf("/outbox/%d/retry", env.Fixture.TenantB.OutboxID), ""},

		// MCP clients: update + revoke cross-tenant
		{"PATCH /mcp/clients/{id}", "PATCH",
			fmt.Sprintf("/mcp/clients/%s", env.Fixture.TenantB.MCPClientID),
			`{"name":"hijacked"}`},
		{"DELETE /mcp/clients/{id}", "DELETE",
			fmt.Sprintf("/mcp/clients/%s", env.Fixture.TenantB.MCPClientID), ""},
	}

	forbiddenIDs := []string{
		env.Fixture.TenantB.TenantID,
		env.Fixture.TenantB.TagID.String(),
		env.Fixture.TenantB.WorkflowID,
		env.Fixture.TenantB.MCPClientID.String(),
	}

	for _, ep := range writeEndpoints {
		t.Run(ep.name, func(t *testing.T) {
			var status int
			var body []byte

			switch ep.method {
			case "PATCH":
				status, body = doPatch(t, env, env.APIKeyA, ep.path, ep.body)
			case "DELETE":
				status, body = doDelete(t, env, env.APIKeyA, ep.path)
			case "POST":
				status, body = doPost(t, env, env.APIKeyA, ep.path, ep.body)
			case "PUT":
				status, body = doPut(t, env, env.APIKeyA, ep.path, ep.body)
			}

			// Cross-tenant writes: expect 404/4xx/5xx — never 200 with tenant B data.
			if containsAny(body, forbiddenIDs...) {
				t.Errorf("%s leaked tenant B data in %d response:\n%s",
					ep.name, status, string(body))
			}

			if status == http.StatusOK {
				t.Errorf("ISOLATION BREACH: %s returned 200 for cross-tenant write", ep.name)
			}
		})
	}

	// After all cross-tenant writes, verify tenant B's resources are unmodified.
	verifyTenantBIntegrity(t, env)
}

// verifyTenantBIntegrity checks that tenant B's resources still exist after
// cross-tenant write attempts. A 404 would indicate a successful cross-tenant
// deletion; 200 or 500 (nil dep) are both acceptable.
func verifyTenantBIntegrity(t *testing.T, env *httpEnv) {
	t.Helper()

	t.Run("verify_mcp_client_intact", func(t *testing.T) {
		status, _ := doGet(t, env, env.APIKeyB, fmt.Sprintf("/mcp/clients/%s", env.Fixture.TenantB.MCPClientID))
		if status == http.StatusNotFound {
			t.Errorf("tenant B's MCP client was deleted by cross-tenant write attempt")
		}
	})

	t.Run("verify_tags_list_intact", func(t *testing.T) {
		status, body := doGet(t, env, env.APIKeyB, "/tags")
		if status == http.StatusOK && !containsAny(body, env.Fixture.TenantB.TagID.String()) {
			t.Errorf("tenant B's tag %s missing from list after cross-tenant write attempt", env.Fixture.TenantB.TagID)
		}
	})

	t.Run("verify_workflow_list_intact", func(t *testing.T) {
		status, body := doGet(t, env, env.APIKeyB, "/workflow/states")
		if status == http.StatusOK && !containsAny(body, env.Fixture.TenantB.WorkflowID) {
			t.Errorf("tenant B's workflow state %s missing from list after cross-tenant write attempt", env.Fixture.TenantB.WorkflowID)
		}
	})
}
