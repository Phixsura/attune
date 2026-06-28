// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

// ---------------------------------------------------------------------------
// Internal mock stores (white-box; inside package mcpclient)
// ---------------------------------------------------------------------------

type stubClientStore struct {
	client      mcprepo.Client
	revokedID   uuid.UUID
	createErr   error
	getErr      error
	listErr     error
	revokeErr   error
	updateErr   error
	updateCalls int
}

func (s *stubClientStore) ListByTenant(context.Context, string) ([]mcprepo.Client, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return []mcprepo.Client{s.client}, nil
}

func (s *stubClientStore) Create(_ context.Context, _ mcprepo.CreateClientParams) (*mcprepo.Client, error) {
	if s.createErr != nil {
		return nil, s.createErr
	}
	return ptrext.Of(s.client), nil
}

func (s *stubClientStore) GetByID(_ context.Context, id uuid.UUID) (*mcprepo.Client, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if id != s.client.ID {
		return nil, mcprepo.ErrClientNotFound
	}
	return ptrext.Of(s.client), nil
}

func (s *stubClientStore) Revoke(_ context.Context, id uuid.UUID) error {
	s.revokedID = id
	return s.revokeErr
}

func (s *stubClientStore) UpdateGovernance(_ context.Context, p mcprepo.UpdateClientGovernanceParams) (*mcprepo.Client, error) {
	s.updateCalls++
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	s.client.ToolPolicyMode = p.ToolPolicyMode
	s.client.RateLimitRPM = p.RateLimitRPM
	s.client.RateLimitBurst = p.RateLimitBurst
	return ptrext.Of(s.client), nil
}

type stubTokenStore struct {
	grants         []mcprepo.RefreshToken
	revokeErr      error
	revokedGrant   uuid.UUID
	revokedSession uuid.UUID
	revokedClient  uuid.UUID
	listErr        error
}

func (s *stubTokenStore) Revoke(_ context.Context, id uuid.UUID) error {
	s.revokedGrant = id
	return s.revokeErr
}

func (s *stubTokenStore) RevokeByClient(_ context.Context, clientID uuid.UUID) (int64, error) {
	s.revokedClient = clientID
	return 1, nil
}

func (s *stubTokenStore) RevokeBySession(_ context.Context, sessionID uuid.UUID) (int64, error) {
	s.revokedSession = sessionID
	return 1, nil
}

func (s *stubTokenStore) ListByClient(context.Context, uuid.UUID) ([]mcprepo.RefreshToken, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.grants, nil
}

type stubSessionStore struct {
	sessions     []mcprepo.Session
	closedID     uuid.UUID
	closedReason string
	closedBy     string
	closedClient uuid.UUID
	closeErr     error
	getErr       error
	listErr      error
}

func (s *stubSessionStore) CloseByClient(_ context.Context, clientID uuid.UUID) (int64, error) {
	s.closedClient = clientID
	return 1, nil
}

func (s *stubSessionStore) CloseWithReason(_ context.Context, id uuid.UUID, reason, closedBy string) error {
	if s.closeErr != nil {
		return s.closeErr
	}
	s.closedID = id
	s.closedReason = reason
	s.closedBy = closedBy
	return nil
}

func (s *stubSessionStore) GetByID(_ context.Context, id uuid.UUID) (*mcprepo.Session, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	for _, sess := range s.sessions {
		if sess.ID == id {
			return ptrext.Of(sess), nil
		}
	}
	return nil, mcprepo.ErrSessionNotFound
}

func (s *stubSessionStore) ListByClient(context.Context, uuid.UUID) ([]mcprepo.Session, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.sessions, nil
}

type stubToolPolicyStore struct {
	policies     []mcprepo.ToolPolicy
	replacements []mcprepo.UpsertToolPolicyParams
	replaceErr   error
	listErr      error
}

func (s *stubToolPolicyStore) ListByClient(context.Context, uuid.UUID) ([]mcprepo.ToolPolicy, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]mcprepo.ToolPolicy(nil), s.policies...), nil
}

func (s *stubToolPolicyStore) ReplaceByClient(_ context.Context, _ uuid.UUID, policies []mcprepo.UpsertToolPolicyParams) error {
	if s.replaceErr != nil {
		return s.replaceErr
	}
	s.replacements = append([]mcprepo.UpsertToolPolicyParams(nil), policies...)
	s.policies = make([]mcprepo.ToolPolicy, 0, len(policies))
	for _, p := range policies {
		s.policies = append(s.policies, mcprepo.ToolPolicy{
			ClientID:       p.ClientID,
			ToolName:       p.ToolName,
			Effect:         p.Effect,
			RateLimitRPM:   p.RateLimitRPM,
			RateLimitBurst: p.RateLimitBurst,
		})
	}
	return nil
}

type stubAuditRecorder struct {
	events []auditlogsvc.Event
}

func (s *stubAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	s.events = append(s.events, event)
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testAuth() *session.AuthCtx {
	return ptrext.Of(session.AuthCtx{
		TenantID: "tenant-1",
		UserID:   "admin-user",
		UserType: "admin",
	})
}

func testClient(id uuid.UUID) mcprepo.Client {
	return mcprepo.Client{
		ID:             id,
		TenantID:       "tenant-1",
		Name:           "test-agent",
		RedirectURIs:   []string{"https://example.com/callback"},
		Scopes:         []string{domain.MCPScopeRead, domain.MCPScopeWrite},
		ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
		CreatedAt:      time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		CreatedBy:      "admin",
	}
}

func authReq(method, target string, body *bytes.Buffer) *http.Request {
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest(method, target, nil)
	} else {
		r = httptest.NewRequest(method, target, body)
	}
	return r.WithContext(session.WithAuthCtx(r.Context(), testAuth()))
}

func routeReq(req *http.Request, params map[string]string) *http.Request {
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// ---------------------------------------------------------------------------
// governance.go: Get -- error paths
// ---------------------------------------------------------------------------

func TestGet_InvalidClientID(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{}), nil, nil, nil)

	_, err := h.Get(context.Background(), testAuth(), "not-a-uuid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid client ID")
}

func TestGet_ClientNotFoundForTenant(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	// Client exists but belongs to a different tenant.
	client := testClient(clientID)
	client.TenantID = "other-tenant"
	h := NewHandler(ptrext.Of(stubClientStore{client: client}), nil, nil, nil)

	_, err := h.Get(context.Background(), testAuth(), clientID.String())
	require.Error(t, err)
	assert.True(t, errors.Is(err, mcprepo.ErrClientNotFound))
}

func TestGet_SessionListError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil,
		ptrext.Of(stubSessionStore{listErr: errors.New("db timeout")}),
		nil,
	)

	_, err := h.Get(context.Background(), testAuth(), clientID.String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db timeout")
}

func TestGet_TokenListError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		ptrext.Of(stubTokenStore{listErr: errors.New("token db error")}),
		nil,
		nil,
	)

	_, err := h.Get(context.Background(), testAuth(), clientID.String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token db error")
}

func TestGet_ToolPolicyListError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil,
		nil,
		ptrext.Of(stubToolPolicyStore{listErr: errors.New("policy db error")}),
	)

	_, err := h.Get(context.Background(), testAuth(), clientID.String())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy db error")
}

func TestGet_NilStoresReturnsEmptySlices(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil, // no token store
		nil, // no session store
		nil, // no tool policy store
	)

	resp, err := h.Get(context.Background(), testAuth(), clientID.String())
	require.NoError(t, err)
	assert.Empty(t, resp.Sessions)
	assert.Empty(t, resp.RefreshGrants)
	assert.Nil(t, resp.Connection)
}

// ---------------------------------------------------------------------------
// governance.go: Update -- error paths
// ---------------------------------------------------------------------------

func TestUpdate_InvalidClientID(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{}), nil, nil, nil)

	_, err := h.Update(context.Background(), testAuth(), "bad-id",
		ptrext.Of(UpdateRequest{ToolPolicyMode: ptrext.Of(domain.MCPToolPolicyModeAllowList)}),
		httptest.NewRequest(http.MethodPatch, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid client ID")
}

func TestUpdate_MissingMutableFields(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(ptrext.Of(stubClientStore{client: testClient(clientID)}), nil, nil, nil)

	_, err := h.Update(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(UpdateRequest{}),
		httptest.NewRequest(http.MethodPatch, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of tool_policy_mode, rate_limit_rpm, or rate_limit_burst is required")
}

func TestUpdate_InvalidToolPolicyMode(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(ptrext.Of(stubClientStore{client: testClient(clientID)}), nil, nil, nil)

	_, err := h.Update(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(UpdateRequest{ToolPolicyMode: ptrext.Of("nonexistent_mode")}),
		httptest.NewRequest(http.MethodPatch, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tool policy mode")
}

func TestUpdate_NegativeRateLimitRPM(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(ptrext.Of(stubClientStore{client: testClient(clientID)}), nil, nil, nil)

	_, err := h.Update(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(UpdateRequest{
			ToolPolicyMode: ptrext.Of(domain.MCPToolPolicyModeAllowList),
			RateLimitRPM:   ptrext.Of(-1),
		}),
		httptest.NewRequest(http.MethodPatch, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be greater than zero")
}

func TestUpdate_NegativeRateLimitBurst(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(ptrext.Of(stubClientStore{client: testClient(clientID)}), nil, nil, nil)

	_, err := h.Update(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(UpdateRequest{
			ToolPolicyMode: ptrext.Of(domain.MCPToolPolicyModeAllowList),
			RateLimitRPM:   ptrext.Of(10),
			RateLimitBurst: ptrext.Of(0),
		}),
		httptest.NewRequest(http.MethodPatch, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be greater than zero")
}

func TestUpdate_ClientLoadError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{getErr: errors.New("db down")}),
		nil, nil, nil,
	)

	_, err := h.Update(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(UpdateRequest{ToolPolicyMode: ptrext.Of(domain.MCPToolPolicyModeAllowList)}),
		httptest.NewRequest(http.MethodPatch, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestUpdate_WithAuditLogger(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	audit := ptrext.Of(stubAuditRecorder{})
	h := NewHandler(ptrext.Of(stubClientStore{client: testClient(clientID)}), nil, nil, nil)
	h.SetAuditLogger(audit)

	_, err := h.Update(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(UpdateRequest{ToolPolicyMode: ptrext.Of(domain.MCPToolPolicyModeAllowList)}),
		httptest.NewRequest(http.MethodPatch, "/", nil))
	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	assert.Equal(t, domain.AuditActionMCPClientUpdate, audit.events[0].Action)
}

func TestUpdate_UpdateGovernanceError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{
			client:    testClient(clientID),
			updateErr: errors.New("update db error"),
		}),
		nil, nil, nil,
	)

	_, err := h.Update(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(UpdateRequest{ToolPolicyMode: ptrext.Of(domain.MCPToolPolicyModeAllowList)}),
		httptest.NewRequest(http.MethodPatch, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "update db error")
}

// ---------------------------------------------------------------------------
// governance.go: ReplaceToolPolicies -- error paths
// ---------------------------------------------------------------------------

func TestReplaceToolPolicies_InvalidClientID(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{}), nil, nil, nil)

	_, err := h.ReplaceToolPolicies(context.Background(), testAuth(), "not-uuid",
		ptrext.Of(ReplaceToolPoliciesRequest{}),
		httptest.NewRequest(http.MethodPut, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid client ID")
}

func TestReplaceToolPolicies_ClientLoadError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{getErr: errors.New("db error")}),
		nil, nil, nil,
	)

	_, err := h.ReplaceToolPolicies(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(ReplaceToolPoliciesRequest{}),
		httptest.NewRequest(http.MethodPut, "/", nil))
	require.Error(t, err)
}

func TestReplaceToolPolicies_NilToolPolicyStore(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil, nil,
		nil, // no tool policy store
	)

	_, err := h.ReplaceToolPolicies(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(ReplaceToolPoliciesRequest{Policies: []ToolPolicyInput{}}),
		httptest.NewRequest(http.MethodPut, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool policy store not configured")
}

func TestReplaceToolPolicies_ReplaceByClientError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil, nil,
		ptrext.Of(stubToolPolicyStore{replaceErr: errors.New("replace failed")}),
	)

	_, err := h.ReplaceToolPolicies(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(ReplaceToolPoliciesRequest{Policies: []ToolPolicyInput{
			{ToolName: "list_feedback", Effect: domain.MCPToolPolicyEffectAllow},
		}}),
		httptest.NewRequest(http.MethodPut, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replace failed")
}

func TestReplaceToolPolicies_ListAfterReplaceError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	// The store succeeds on the first ListByClient (for "before"), then errors
	// on the second call (for "after").
	store := ptrext.Of(stubToolPolicyStore{})
	h := &Handler{
		clients: ptrext.Of(stubClientStore{client: testClient(clientID)}),
		toolPolicies: &countingToolPolicyStore{
			inner:      store,
			failAfterN: 1, // succeed on 1st call (before), fail on 2nd (after)
		},
	}

	_, err := h.ReplaceToolPolicies(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(ReplaceToolPoliciesRequest{Policies: []ToolPolicyInput{
			{ToolName: "list_feedback", Effect: domain.MCPToolPolicyEffectAllow},
		}}),
		httptest.NewRequest(http.MethodPut, "/", nil))
	require.Error(t, err)
}

// countingToolPolicyStore wraps stubToolPolicyStore and fails ListByClient after N calls.
type countingToolPolicyStore struct {
	inner      *stubToolPolicyStore
	failAfterN int
	listCalls  int
}

func (c *countingToolPolicyStore) ListByClient(ctx context.Context, clientID uuid.UUID) ([]mcprepo.ToolPolicy, error) {
	c.listCalls++
	if c.listCalls > c.failAfterN {
		return nil, errors.New("list after replace failed")
	}
	return c.inner.ListByClient(ctx, clientID)
}

func (c *countingToolPolicyStore) ReplaceByClient(ctx context.Context, clientID uuid.UUID, policies []mcprepo.UpsertToolPolicyParams) error {
	return c.inner.ReplaceByClient(ctx, clientID, policies)
}

func TestReplaceToolPolicies_WithAuditLogger(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	audit := ptrext.Of(stubAuditRecorder{})
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil, nil,
		ptrext.Of(stubToolPolicyStore{}),
	)
	h.SetAuditLogger(audit)

	_, err := h.ReplaceToolPolicies(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(ReplaceToolPoliciesRequest{Policies: []ToolPolicyInput{
			{ToolName: "list_feedback", Effect: domain.MCPToolPolicyEffectAllow},
		}}),
		httptest.NewRequest(http.MethodPut, "/", nil))
	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	assert.Equal(t, domain.AuditActionMCPClientToolPolicies, audit.events[0].Action)
}

func TestReplaceToolPolicies_BeforeListError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil, nil,
		ptrext.Of(stubToolPolicyStore{listErr: errors.New("before list fail")}),
	)

	_, err := h.ReplaceToolPolicies(context.Background(), testAuth(), clientID.String(),
		ptrext.Of(ReplaceToolPoliciesRequest{Policies: []ToolPolicyInput{
			{ToolName: "list_feedback", Effect: domain.MCPToolPolicyEffectAllow},
		}}),
		httptest.NewRequest(http.MethodPut, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "before list fail")
}

// ---------------------------------------------------------------------------
// governance.go: RevokeSession -- error paths
// ---------------------------------------------------------------------------

func TestRevokeSession_InvalidClientID(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{}), nil, nil, nil)

	_, err := h.RevokeSession(context.Background(), testAuth(), "bad-id", uuid.New().String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid client ID")
}

func TestRevokeSession_InvalidSessionID(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(ptrext.Of(stubClientStore{client: testClient(clientID)}), nil, nil, nil)

	_, err := h.RevokeSession(context.Background(), testAuth(), clientID.String(), "not-a-uuid",
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid session ID")
}

func TestRevokeSession_NilSessionStore(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil,
		nil, // no session store
		nil,
	)

	_, err := h.RevokeSession(context.Background(), testAuth(), clientID.String(), uuid.New().String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "session store not configured")
}

func TestRevokeSession_SessionNotFound(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil,
		ptrext.Of(stubSessionStore{sessions: nil}),
		nil,
	)

	_, err := h.RevokeSession(context.Background(), testAuth(), clientID.String(), uuid.New().String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.True(t, errors.Is(err, mcprepo.ErrSessionNotFound))
}

func TestRevokeSession_SessionBelongsToDifferentClient(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	sessionID := uuid.New()
	otherClientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil,
		ptrext.Of(stubSessionStore{sessions: []mcprepo.Session{{
			ID:           sessionID,
			ClientID:     otherClientID,
			TenantID:     "tenant-1",
			Scopes:       []string{domain.MCPScopeRead},
			LastActiveAt: time.Now(),
			CreatedAt:    time.Now(),
		}}}),
		nil,
	)

	_, err := h.RevokeSession(context.Background(), testAuth(), clientID.String(), sessionID.String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.True(t, errors.Is(err, mcprepo.ErrSessionNotFound))
}

func TestRevokeSession_CloseWithReasonError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	sessionID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		ptrext.Of(stubTokenStore{}),
		ptrext.Of(stubSessionStore{
			sessions: []mcprepo.Session{{
				ID:           sessionID,
				ClientID:     clientID,
				TenantID:     "tenant-1",
				Scopes:       []string{domain.MCPScopeRead},
				LastActiveAt: time.Now(),
				CreatedAt:    time.Now(),
			}},
			closeErr: errors.New("close failed"),
		}),
		nil,
	)

	_, err := h.RevokeSession(context.Background(), testAuth(), clientID.String(), sessionID.String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "close failed")
}

func TestRevokeSession_NilTokenStoreStillCloses(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	sessionID := uuid.New()
	sessions := ptrext.Of(stubSessionStore{sessions: []mcprepo.Session{{
		ID:           sessionID,
		ClientID:     clientID,
		TenantID:     "tenant-1",
		Scopes:       []string{domain.MCPScopeRead},
		LastActiveAt: time.Now(),
		CreatedAt:    time.Now(),
	}}})
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil, // no token store
		sessions,
		nil,
	)

	_, err := h.RevokeSession(context.Background(), testAuth(), clientID.String(), sessionID.String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.NoError(t, err)
	assert.Equal(t, sessionID, sessions.closedID)
}

func TestRevokeSession_WithAuditLogger(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	sessionID := uuid.New()
	closedAt := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	audit := ptrext.Of(stubAuditRecorder{})
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		ptrext.Of(stubTokenStore{}),
		ptrext.Of(stubSessionStore{sessions: []mcprepo.Session{{
			ID:           sessionID,
			ClientID:     clientID,
			TenantID:     "tenant-1",
			Scopes:       []string{domain.MCPScopeRead},
			LastActiveAt: time.Now(),
			CreatedAt:    time.Now(),
			ClosedAt:     ptrext.Of(closedAt),
		}}}),
		nil,
	)
	h.SetAuditLogger(audit)

	_, err := h.RevokeSession(context.Background(), testAuth(), clientID.String(), sessionID.String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	assert.Equal(t, domain.AuditActionMCPSessionRevoke, audit.events[0].Action)
}

// ---------------------------------------------------------------------------
// governance.go: RevokeRefreshGrant -- error paths
// ---------------------------------------------------------------------------

func TestRevokeRefreshGrant_InvalidClientID(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{}), nil, nil, nil)

	_, err := h.RevokeRefreshGrant(context.Background(), testAuth(), "bad", uuid.New().String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid client ID")
}

func TestRevokeRefreshGrant_InvalidGrantID(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(ptrext.Of(stubClientStore{client: testClient(clientID)}), nil, nil, nil)

	_, err := h.RevokeRefreshGrant(context.Background(), testAuth(), clientID.String(), "bad",
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid grant ID")
}

func TestRevokeRefreshGrant_NilTokenStore(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil, // no token store
		nil, nil,
	)

	_, err := h.RevokeRefreshGrant(context.Background(), testAuth(), clientID.String(), uuid.New().String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token store not configured")
}

func TestRevokeRefreshGrant_GrantNotFound(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		ptrext.Of(stubTokenStore{grants: nil}),
		nil, nil,
	)

	_, err := h.RevokeRefreshGrant(context.Background(), testAuth(), clientID.String(), uuid.New().String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.True(t, errors.Is(err, mcprepo.ErrRefreshTokenNotFound))
}

func TestRevokeRefreshGrant_TokenListError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		ptrext.Of(stubTokenStore{listErr: errors.New("token list failed")}),
		nil, nil,
	)

	_, err := h.RevokeRefreshGrant(context.Background(), testAuth(), clientID.String(), uuid.New().String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token list failed")
}

func TestRevokeRefreshGrant_RevokeError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	grantID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		ptrext.Of(stubTokenStore{
			grants: []mcprepo.RefreshToken{{
				ID:        grantID,
				ClientID:  clientID,
				SessionID: uuid.New(),
				Scopes:    []string{domain.MCPScopeRead},
				ExpiresAt: time.Now().Add(time.Hour),
				CreatedAt: time.Now(),
			}},
			revokeErr: errors.New("revoke failed"),
		}),
		nil, nil,
	)

	_, err := h.RevokeRefreshGrant(context.Background(), testAuth(), clientID.String(), grantID.String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "revoke failed")
}

func TestRevokeRefreshGrant_WithAuditLogger(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	grantID := uuid.New()
	audit := ptrext.Of(stubAuditRecorder{})
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		ptrext.Of(stubTokenStore{grants: []mcprepo.RefreshToken{{
			ID:        grantID,
			ClientID:  clientID,
			SessionID: uuid.New(),
			Scopes:    []string{domain.MCPScopeRead},
			ExpiresAt: time.Now().Add(time.Hour),
			CreatedAt: time.Now(),
		}}}),
		nil, nil,
	)
	h.SetAuditLogger(audit)

	_, err := h.RevokeRefreshGrant(context.Background(), testAuth(), clientID.String(), grantID.String(),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.NoError(t, err)
	require.Len(t, audit.events, 1)
	assert.Equal(t, domain.AuditActionMCPRefreshGrantRevoke, audit.events[0].Action)
}

// ---------------------------------------------------------------------------
// governance.go: Serve* HTTP handlers -- error paths
// ---------------------------------------------------------------------------

func TestServeGet_Error(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{getErr: errors.New("db fail")}), nil, nil, nil)

	req := routeReq(authReq(http.MethodGet, "/", nil), map[string]string{"id": uuid.New().String()})
	rec := httptest.NewRecorder()
	h.ServeGet(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServeUpdate_InvalidJSON(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{}), nil, nil, nil)

	req := routeReq(
		authReq(http.MethodPatch, "/", bytes.NewBufferString("{")),
		map[string]string{"id": uuid.New().String()},
	)
	rec := httptest.NewRecorder()
	h.ServeUpdate(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "validation_error", body["code"])
}

func TestServeUpdate_Error(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{getErr: errors.New("db fail")}), nil, nil, nil)

	req := routeReq(
		authReq(http.MethodPatch, "/", bytes.NewBufferString(`{"tool_policy_mode":"allow_list"}`)),
		map[string]string{"id": uuid.New().String()},
	)
	rec := httptest.NewRecorder()
	h.ServeUpdate(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServeReplaceToolPolicies_InvalidJSON(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{}), nil, nil, nil)

	req := routeReq(
		authReq(http.MethodPut, "/", bytes.NewBufferString("not-json")),
		map[string]string{"id": uuid.New().String()},
	)
	rec := httptest.NewRecorder()
	h.ServeReplaceToolPolicies(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServeReplaceToolPolicies_Error(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{getErr: errors.New("db fail")}), nil, nil, nil)

	req := routeReq(
		authReq(http.MethodPut, "/", bytes.NewBufferString(`{"policies":[]}`)),
		map[string]string{"id": uuid.New().String()},
	)
	rec := httptest.NewRecorder()
	h.ServeReplaceToolPolicies(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServeRevokeSession_Error(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{getErr: errors.New("db fail")}), nil, nil, nil)

	req := routeReq(
		authReq(http.MethodDelete, "/", nil),
		map[string]string{"id": uuid.New().String(), "session_id": uuid.New().String()},
	)
	rec := httptest.NewRecorder()
	h.ServeRevokeSession(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestServeRevokeRefreshGrant_Error(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{getErr: errors.New("db fail")}), nil, nil, nil)

	req := routeReq(
		authReq(http.MethodDelete, "/", nil),
		map[string]string{"id": uuid.New().String(), "grant_id": uuid.New().String()},
	)
	rec := httptest.NewRecorder()
	h.ServeRevokeRefreshGrant(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ---------------------------------------------------------------------------
// handler.go: writeError -- sentinel error paths
// ---------------------------------------------------------------------------

func TestWriteError_SessionNotFound(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	writeError(rec, mcprepo.ErrSessionNotFound)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "not_found", body["code"])
	assert.Contains(t, body["message"], "session not found")
}

func TestWriteError_RefreshTokenNotFound(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	writeError(rec, mcprepo.ErrRefreshTokenNotFound)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "not_found", body["code"])
	assert.Contains(t, body["message"], "refresh grant not found")
}

func TestWriteError_ClientNotFound(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	writeError(rec, mcprepo.ErrClientNotFound)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "not_found", body["code"])
}

func TestWriteError_InternalError(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	writeError(rec, errors.New("some unexpected error"))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "internal_error", body["code"])
}

// ---------------------------------------------------------------------------
// handler.go: ServeList -- error path
// ---------------------------------------------------------------------------

func TestServeList_Error(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{listErr: errors.New("list fail")}), nil, nil, nil)

	rec := httptest.NewRecorder()
	h.ServeList(rec, authReq(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ---------------------------------------------------------------------------
// handler.go: ServeCreate -- success path (covers audit branch)
// ---------------------------------------------------------------------------

func TestServeCreate_Success(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	audit := ptrext.Of(stubAuditRecorder{})
	h := NewHandler(
		ptrext.Of(stubClientStore{client: testClient(clientID)}),
		nil, nil, nil,
	)
	h.SetAuditLogger(audit)

	body := `{"name":"agent","redirect_uris":["https://example.com/cb"],"scopes":["mcp:read"]}`
	req := authReq(http.MethodPost, "/", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.ServeCreate(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	require.Len(t, audit.events, 1)
	assert.Equal(t, domain.AuditActionMCPClientCreate, audit.events[0].Action)
}

func TestServeCreate_StoreError(t *testing.T) {
	t.Parallel()
	h := NewHandler(
		ptrext.Of(stubClientStore{createErr: errors.New("create fail")}),
		nil, nil, nil,
	)

	body := `{"name":"agent","redirect_uris":["https://example.com/cb"],"scopes":["mcp:read"]}`
	req := authReq(http.MethodPost, "/", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.ServeCreate(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// ---------------------------------------------------------------------------
// handler.go: List -- error path
// ---------------------------------------------------------------------------

func TestList_Error(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{listErr: errors.New("db fail")}), nil, nil, nil)

	_, err := h.List(context.Background(), testAuth(), ptrext.Of(ListRequest{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db fail")
}

// ---------------------------------------------------------------------------
// handler.go: Revoke -- error paths
// ---------------------------------------------------------------------------

func TestRevoke_StoreError(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	h := NewHandler(
		ptrext.Of(stubClientStore{
			client:    testClient(clientID),
			revokeErr: errors.New("revoke db error"),
		}),
		nil, nil, nil,
	)

	_, err := h.Revoke(context.Background(), testAuth(),
		ptrext.Of(RevokeRequest{ID: clientID.String()}),
		httptest.NewRequest(http.MethodDelete, "/", nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "revoke db error")
}

// ---------------------------------------------------------------------------
// handler.go: toDTO -- RevokedAt path
// ---------------------------------------------------------------------------

func TestToDTO_WithRevokedAt(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	c := mcprepo.Client{
		ID:           uuid.New(),
		TenantID:     "t",
		Name:         "c",
		RedirectURIs: []string{"https://example.com/cb"},
		Scopes:       []string{domain.MCPScopeRead},
		CreatedAt:    now,
		CreatedBy:    "admin",
		RevokedAt:    ptrext.Of(now),
	}

	dto := toDTO(c)
	require.NotNil(t, dto.RevokedAt)
	assert.Equal(t, "2026-06-15T12:00:00Z", ptrext.Indirect(dto.RevokedAt))
}

// ---------------------------------------------------------------------------
// handler.go: loadClientForTenant -- tenant mismatch
// ---------------------------------------------------------------------------

func TestLoadClientForTenant_TenantMismatch(t *testing.T) {
	t.Parallel()
	clientID := uuid.New()
	client := testClient(clientID)
	client.TenantID = "other-tenant"
	h := NewHandler(ptrext.Of(stubClientStore{client: client}), nil, nil, nil)

	_, err := h.loadClientForTenant(context.Background(), "tenant-1", clientID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, mcprepo.ErrClientNotFound))
}

// ---------------------------------------------------------------------------
// governance.go: toSessionDTO -- ClosedAt path
// ---------------------------------------------------------------------------

func TestToSessionDTO_WithClosedAt(t *testing.T) {
	t.Parallel()
	closedAt := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	s := mcprepo.Session{
		ID:           uuid.New(),
		ClientID:     uuid.New(),
		TenantID:     "t",
		Scopes:       []string{domain.MCPScopeRead},
		LastToolName: "list_feedback",
		LastDecision: "allowed",
		LastIP:       "127.0.0.1",
		LastActiveAt: time.Now(),
		CreatedAt:    time.Now(),
		ClosedAt:     ptrext.Of(closedAt),
		ClosedReason: "expired",
		ClosedBy:     "system",
	}

	dto := toSessionDTO(s)
	require.NotNil(t, dto.ClosedAt)
	assert.Equal(t, "2026-03-01T10:00:00Z", ptrext.Indirect(dto.ClosedAt))
	assert.Equal(t, "expired", dto.ClosedReason)
	assert.Equal(t, "system", dto.ClosedBy)
}

// ---------------------------------------------------------------------------
// governance.go: validateToolPolicyMode -- valid modes
// ---------------------------------------------------------------------------

func TestValidateToolPolicyMode(t *testing.T) {
	t.Parallel()

	t.Run("legacy_allow_all", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateToolPolicyMode(domain.MCPToolPolicyModeLegacyAllowAll))
	})

	t.Run("allow_list", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateToolPolicyMode(domain.MCPToolPolicyModeAllowList))
	})

	t.Run("invalid", func(t *testing.T) {
		t.Parallel()
		err := validateToolPolicyMode("bogus")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid tool policy mode")
	})
}

// ---------------------------------------------------------------------------
// governance.go: validateOptionalPositiveInt
// ---------------------------------------------------------------------------

func TestValidateOptionalPositiveInt(t *testing.T) {
	t.Parallel()

	t.Run("nil is valid", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateOptionalPositiveInt("field", nil))
	})

	t.Run("positive is valid", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validateOptionalPositiveInt("field", ptrext.Of(5)))
	})

	t.Run("zero is invalid", func(t *testing.T) {
		t.Parallel()
		err := validateOptionalPositiveInt("field", ptrext.Of(0))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be greater than zero")
	})

	t.Run("negative is invalid", func(t *testing.T) {
		t.Parallel()
		err := validateOptionalPositiveInt("field", ptrext.Of(-3))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be greater than zero")
	})
}

// ---------------------------------------------------------------------------
// governance.go: validateToolPolicyInputs -- edge cases
// ---------------------------------------------------------------------------

func TestValidateToolPolicyInputs_EmptyToolName(t *testing.T) {
	t.Parallel()
	err := validateToolPolicyInputs([]string{domain.MCPScopeRead}, []ToolPolicyInput{
		{ToolName: "", Effect: domain.MCPToolPolicyEffectAllow},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool_name is required")
}

func TestValidateToolPolicyInputs_DuplicateToolName(t *testing.T) {
	t.Parallel()
	err := validateToolPolicyInputs(
		[]string{domain.MCPScopeRead},
		[]ToolPolicyInput{
			{ToolName: "list_feedback", Effect: domain.MCPToolPolicyEffectAllow},
			{ToolName: "list_feedback", Effect: domain.MCPToolPolicyEffectDeny},
		},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate tool_name")
}

func TestValidateToolPolicyInputs_UnknownTool(t *testing.T) {
	t.Parallel()
	err := validateToolPolicyInputs([]string{domain.MCPScopeRead}, []ToolPolicyInput{
		{ToolName: "nonexistent_tool_xyz", Effect: domain.MCPToolPolicyEffectAllow},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool")
}

func TestValidateToolPolicyInputs_InvalidEffect(t *testing.T) {
	t.Parallel()
	err := validateToolPolicyInputs([]string{domain.MCPScopeRead}, []ToolPolicyInput{
		{ToolName: "list_feedback", Effect: "invalid_effect"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid effect")
}

func TestValidateToolPolicyInputs_NegativeRateLimits(t *testing.T) {
	t.Parallel()

	t.Run("negative rpm", func(t *testing.T) {
		t.Parallel()
		err := validateToolPolicyInputs([]string{domain.MCPScopeRead}, []ToolPolicyInput{
			{ToolName: "list_feedback", Effect: domain.MCPToolPolicyEffectAllow, RateLimitRPM: ptrext.Of(-1)},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be greater than zero")
	})

	t.Run("negative burst", func(t *testing.T) {
		t.Parallel()
		err := validateToolPolicyInputs([]string{domain.MCPScopeRead}, []ToolPolicyInput{
			{ToolName: "list_feedback", Effect: domain.MCPToolPolicyEffectAllow, RateLimitBurst: ptrext.Of(-1)},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be greater than zero")
	})
}

// ---------------------------------------------------------------------------
// governance.go: scopeCovers -- write covers read
// ---------------------------------------------------------------------------

func TestScopeCovers(t *testing.T) {
	t.Parallel()

	t.Run("exact match", func(t *testing.T) {
		t.Parallel()
		assert.True(t, scopeCovers([]string{domain.MCPScopeRead}, domain.MCPScopeRead))
	})

	t.Run("write covers read", func(t *testing.T) {
		t.Parallel()
		assert.True(t, scopeCovers([]string{domain.MCPScopeWrite}, domain.MCPScopeRead))
	})

	t.Run("read does not cover write", func(t *testing.T) {
		t.Parallel()
		assert.False(t, scopeCovers([]string{domain.MCPScopeRead}, domain.MCPScopeWrite))
	})

	t.Run("no scopes", func(t *testing.T) {
		t.Parallel()
		assert.False(t, scopeCovers(nil, domain.MCPScopeRead))
	})
}

// ---------------------------------------------------------------------------
// governance.go: toolEffectiveAllowed
// ---------------------------------------------------------------------------

func TestToolEffectiveAllowed(t *testing.T) {
	t.Parallel()

	t.Run("deny always denies", func(t *testing.T) {
		t.Parallel()
		assert.False(t, toolEffectiveAllowed(domain.MCPToolPolicyModeLegacyAllowAll, true, domain.MCPToolPolicyEffectDeny))
	})

	t.Run("allow_list mode without override denies", func(t *testing.T) {
		t.Parallel()
		assert.False(t, toolEffectiveAllowed(domain.MCPToolPolicyModeAllowList, false, ""))
	})

	t.Run("allow_list mode with allow override allows", func(t *testing.T) {
		t.Parallel()
		assert.True(t, toolEffectiveAllowed(domain.MCPToolPolicyModeAllowList, true, domain.MCPToolPolicyEffectAllow))
	})

	t.Run("legacy_allow_all mode without override allows", func(t *testing.T) {
		t.Parallel()
		assert.True(t, toolEffectiveAllowed(domain.MCPToolPolicyModeLegacyAllowAll, false, ""))
	})

	t.Run("legacy_allow_all mode with allow override allows", func(t *testing.T) {
		t.Parallel()
		assert.True(t, toolEffectiveAllowed(domain.MCPToolPolicyModeLegacyAllowAll, true, domain.MCPToolPolicyEffectAllow))
	})
}

// ---------------------------------------------------------------------------
// connection.go: newConnectionProfile -- edge cases
// ---------------------------------------------------------------------------

func TestNewConnectionProfile_EmptyBaseURL(t *testing.T) {
	t.Parallel()
	p := newConnectionProfile("", "")
	assert.Nil(t, p)
}

func TestNewConnectionProfile_EmptyBaseURLWhitespace(t *testing.T) {
	t.Parallel()
	p := newConnectionProfile("   ", "")
	assert.Nil(t, p)
}

func TestNewConnectionProfile_DefaultIssuer(t *testing.T) {
	t.Parallel()
	p := newConnectionProfile("https://example.com", "")
	require.NotNil(t, p)
	assert.Equal(t, "https://example.com/mcp/oauth", p.OAuthIssuer)
	assert.Equal(t, "https://example.com/mcp", p.ServerURL)
	assert.Equal(t, "https://example.com/mcp/v1", p.ResourceURL)
}

func TestNewConnectionProfile_CustomIssuer(t *testing.T) {
	t.Parallel()
	p := newConnectionProfile("https://example.com", "https://auth.example.com/oauth")
	require.NotNil(t, p)
	assert.Equal(t, "https://auth.example.com/oauth", p.OAuthIssuer)
}

func TestNewConnectionProfile_TrailingSlash(t *testing.T) {
	t.Parallel()
	p := newConnectionProfile("https://example.com/", "https://auth.example.com/oauth/")
	require.NotNil(t, p)
	assert.Equal(t, "https://example.com/mcp", p.ServerURL)
	assert.Equal(t, "https://auth.example.com/oauth", p.OAuthIssuer)
}

func TestNewConnectionProfile_LegacyURLs(t *testing.T) {
	t.Parallel()
	p := newConnectionProfile("https://example.com", "")
	require.NotNil(t, p)
	assert.Equal(t, "https://example.com/mcp/.well-known/oauth-protected-resource", p.LegacyProtectedResourceMetadataURL)
	assert.Equal(t, "https://example.com/mcp/oauth/.well-known/oauth-authorization-server", p.LegacyAuthorizationServerMetadataURL)
	assert.Equal(t, "https://example.com/mcp/oauth/.well-known/openid-configuration", p.LegacyOpenIDConfigurationURL)
}

// ---------------------------------------------------------------------------
// handler.go: ServeRevoke -- validation error (invalid UUID)
// ---------------------------------------------------------------------------

func TestServeRevoke_ValidationError(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{}), nil, nil, nil)

	req := routeReq(
		authReq(http.MethodDelete, "/", nil),
		map[string]string{"id": "not-a-uuid"},
	)
	rec := httptest.NewRecorder()
	h.ServeRevoke(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// ---------------------------------------------------------------------------
// governance.go: listToolPolicies -- nil store returns nil
// ---------------------------------------------------------------------------

func TestListToolPolicies_NilStore(t *testing.T) {
	t.Parallel()
	h := NewHandler(ptrext.Of(stubClientStore{}), nil, nil, nil)

	policies, err := h.listToolPolicies(context.Background(), uuid.New())
	require.NoError(t, err)
	assert.Nil(t, policies)
}
