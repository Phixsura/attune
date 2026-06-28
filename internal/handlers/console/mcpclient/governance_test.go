// SPDX-License-Identifier: Apache-2.0

package mcpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/handlers/console/internal/session"
	"github.com/Phixsura/attune/internal/handlers/console/mcpclient"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	mcprepo "github.com/Phixsura/attune/internal/repo/mcp"
)

type fakeClientStore struct {
	client    mcprepo.Client
	revokedID uuid.UUID
	createErr error
}

func (f *fakeClientStore) ListByTenant(context.Context, string) ([]mcprepo.Client, error) {
	return []mcprepo.Client{f.client}, nil
}

func (f *fakeClientStore) Create(context.Context, mcprepo.CreateClientParams) (*mcprepo.Client, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return ptrext.Of(f.client), nil
}

func (f *fakeClientStore) GetByID(_ context.Context, id uuid.UUID) (*mcprepo.Client, error) {
	if id != f.client.ID {
		return nil, mcprepo.ErrClientNotFound
	}
	return ptrext.Of(f.client), nil
}

func (f *fakeClientStore) Revoke(_ context.Context, id uuid.UUID) error {
	f.revokedID = id
	return nil
}

func (f *fakeClientStore) UpdateGovernance(_ context.Context, p mcprepo.UpdateClientGovernanceParams) (*mcprepo.Client, error) {
	f.client.ToolPolicyMode = p.ToolPolicyMode
	f.client.RateLimitRPM = p.RateLimitRPM
	f.client.RateLimitBurst = p.RateLimitBurst
	return ptrext.Of(f.client), nil
}

type fakeTokenStore struct {
	grants         []mcprepo.RefreshToken
	revokedClient  uuid.UUID
	revokedGrant   uuid.UUID
	revokedSession uuid.UUID
}

func (f *fakeTokenStore) Revoke(_ context.Context, id uuid.UUID) error {
	f.revokedGrant = id
	return nil
}

func (f *fakeTokenStore) RevokeByClient(_ context.Context, clientID uuid.UUID) (int64, error) {
	f.revokedClient = clientID
	return 1, nil
}

func (f *fakeTokenStore) RevokeBySession(_ context.Context, sessionID uuid.UUID) (int64, error) {
	f.revokedSession = sessionID
	return 1, nil
}

func (f *fakeTokenStore) ListByClient(context.Context, uuid.UUID) ([]mcprepo.RefreshToken, error) {
	return f.grants, nil
}

type fakeSessionStore struct {
	sessions     []mcprepo.Session
	closedClient uuid.UUID
	closedID     uuid.UUID
	closedReason string
	closedBy     string
}

func (f *fakeSessionStore) CloseByClient(_ context.Context, clientID uuid.UUID) (int64, error) {
	f.closedClient = clientID
	return 1, nil
}

func (f *fakeSessionStore) CloseWithReason(_ context.Context, id uuid.UUID, reason, closedBy string) error {
	f.closedID = id
	f.closedReason = reason
	f.closedBy = closedBy
	return nil
}

func (f *fakeSessionStore) GetByID(_ context.Context, id uuid.UUID) (*mcprepo.Session, error) {
	for _, s := range f.sessions {
		if s.ID == id {
			return ptrext.Of(s), nil
		}
	}
	return nil, mcprepo.ErrSessionNotFound
}

func (f *fakeSessionStore) ListByClient(context.Context, uuid.UUID) ([]mcprepo.Session, error) {
	return f.sessions, nil
}

type fakeToolPolicyStore struct {
	policies     []mcprepo.ToolPolicy
	replacements []mcprepo.UpsertToolPolicyParams
}

func (f *fakeToolPolicyStore) ListByClient(context.Context, uuid.UUID) ([]mcprepo.ToolPolicy, error) {
	return append([]mcprepo.ToolPolicy(nil), f.policies...), nil
}

func (f *fakeToolPolicyStore) ReplaceByClient(_ context.Context, _ uuid.UUID, policies []mcprepo.UpsertToolPolicyParams) error {
	f.replacements = append([]mcprepo.UpsertToolPolicyParams(nil), policies...)
	f.policies = make([]mcprepo.ToolPolicy, 0, len(policies))
	for _, p := range policies {
		f.policies = append(f.policies, mcprepo.ToolPolicy{
			ClientID:       p.ClientID,
			ToolName:       p.ToolName,
			Effect:         p.Effect,
			RateLimitRPM:   p.RateLimitRPM,
			RateLimitBurst: p.RateLimitBurst,
		})
	}
	return nil
}

func TestHandlerGetReturnsGovernanceDetail(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	sessionID := uuid.New()
	handler := mcpclient.NewHandler(
		ptrext.Of(fakeClientStore{client: mcprepo.Client{
			ID:             clientID,
			TenantID:       "tenant-1",
			Name:           "agent",
			RedirectURIs:   []string{"https://example.com/callback"},
			Scopes:         []string{domain.MCPScopeRead, domain.MCPScopeWrite},
			ToolPolicyMode: domain.MCPToolPolicyModeAllowList,
			CreatedAt:      time.Now(),
			CreatedBy:      "admin",
		}}),
		ptrext.Of(fakeTokenStore{grants: []mcprepo.RefreshToken{{
			ID:        uuid.New(),
			ClientID:  clientID,
			SessionID: sessionID,
			Scopes:    []string{domain.MCPScopeRead},
			ExpiresAt: time.Now().Add(time.Hour),
			CreatedAt: time.Now(),
		}}}),
		ptrext.Of(fakeSessionStore{sessions: []mcprepo.Session{{
			ID:           sessionID,
			ClientID:     clientID,
			TenantID:     "tenant-1",
			Scopes:       []string{domain.MCPScopeRead},
			LastToolName: "list_feedback",
			LastDecision: "allowed",
			LastActiveAt: time.Now(),
			CreatedAt:    time.Now(),
		}}}),
		ptrext.Of(fakeToolPolicyStore{policies: []mcprepo.ToolPolicy{{
			ClientID: clientID,
			ToolName: "list_feedback",
			Effect:   domain.MCPToolPolicyEffectAllow,
		}}}),
	)
	handler.SetConnectionProfile("https://attune.example.com", "")

	resp, err := handler.Get(context.Background(), ptrext.Of(session.AuthCtx{TenantID: "tenant-1"}), clientID.String())
	require.NoError(t, err)
	require.Equal(t, clientID.String(), resp.Client.ID)
	require.Len(t, resp.Sessions, 1)
	require.Len(t, resp.RefreshGrants, 1)
	require.NotEmpty(t, resp.Tools)
	require.NotNil(t, resp.Connection)
	require.Equal(t, "https://attune.example.com/mcp", resp.Connection.ServerURL)
	require.Equal(t, "https://attune.example.com/mcp/v1", resp.Connection.ResourceURL)
	require.Equal(t, "https://attune.example.com/mcp/oauth", resp.Connection.OAuthIssuer)
	require.Equal(t, "https://attune.example.com/.well-known/oauth-protected-resource/mcp/v1", resp.Connection.ProtectedResourceMetadataURL)
	require.Equal(t, "https://attune.example.com/.well-known/oauth-authorization-server/mcp/oauth", resp.Connection.AuthorizationServerMetadataURL)
	require.Equal(t, "https://attune.example.com/mcp/.well-known/oauth-protected-resource", resp.Connection.LegacyProtectedResourceMetadataURL)

	var found bool
	for _, tool := range resp.Tools {
		if tool.Name == "list_feedback" {
			found = true
			require.Equal(t, domain.MCPToolPolicyEffectAllow, tool.Effect)
			require.True(t, tool.EffectiveAllowed)
		}
	}
	require.True(t, found)
}

func TestHandlerUpdateChangesClientGovernance(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	clients := ptrext.Of(fakeClientStore{client: mcprepo.Client{
		ID:             clientID,
		TenantID:       "tenant-1",
		Name:           "agent",
		RedirectURIs:   []string{"https://example.com/callback"},
		Scopes:         []string{domain.MCPScopeRead},
		ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
		CreatedAt:      time.Now(),
		CreatedBy:      "admin",
	}})
	handler := mcpclient.NewHandler(clients, nil, nil, nil)

	rpm := 45
	burst := 9
	resp, err := handler.Update(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin", UserType: "admin"}),
		clientID.String(),
		ptrext.Of(mcpclient.UpdateRequest{
			ToolPolicyMode: ptrext.Of(domain.MCPToolPolicyModeAllowList),
			RateLimitRPM:   ptrext.Of(rpm),
			RateLimitBurst: ptrext.Of(burst),
		}),
		httptest.NewRequest(http.MethodPatch, "/", nil),
	)
	require.NoError(t, err)
	require.Equal(t, domain.MCPToolPolicyModeAllowList, resp.Client.ToolPolicyMode)
	require.NotNil(t, clients.client.RateLimitRPM)
	require.Equal(t, 45, ptrext.IndirectOr(clients.client.RateLimitRPM, 0))
}

func TestHandlerUpdatePreservesOmittedFields(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	existingBurst := 9
	clients := ptrext.Of(fakeClientStore{client: mcprepo.Client{
		ID:             clientID,
		TenantID:       "tenant-1",
		Name:           "agent",
		RedirectURIs:   []string{"https://example.com/callback"},
		Scopes:         []string{domain.MCPScopeRead},
		ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
		RateLimitBurst: ptrext.Of(existingBurst),
		CreatedAt:      time.Now(),
		CreatedBy:      "admin",
	}})
	handler := mcpclient.NewHandler(clients, nil, nil, nil)

	rpm := 45
	resp, err := handler.Update(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin", UserType: "admin"}),
		clientID.String(),
		ptrext.Of(mcpclient.UpdateRequest{
			RateLimitRPM: ptrext.Of(rpm),
		}),
		httptest.NewRequest(http.MethodPatch, "/", nil),
	)
	require.NoError(t, err)
	require.Equal(t, domain.MCPToolPolicyModeLegacyAllowAll, resp.Client.ToolPolicyMode)
	require.NotNil(t, clients.client.RateLimitRPM)
	require.Equal(t, 45, ptrext.IndirectOr(clients.client.RateLimitRPM, 0))
	require.NotNil(t, clients.client.RateLimitBurst)
	require.Equal(t, existingBurst, ptrext.IndirectOr(clients.client.RateLimitBurst, 0))
}

func TestHandlerUpdateRejectsRevokedClient(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	revokedAt := time.Now()
	handler := mcpclient.NewHandler(ptrext.Of(fakeClientStore{client: mcprepo.Client{
		ID:             clientID,
		TenantID:       "tenant-1",
		Name:           "agent",
		RedirectURIs:   []string{"https://example.com/callback"},
		Scopes:         []string{domain.MCPScopeRead},
		ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
		CreatedAt:      time.Now(),
		CreatedBy:      "admin",
		RevokedAt:      ptrext.Of(revokedAt),
	}}), nil, nil, nil)

	_, err := handler.Update(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin", UserType: "admin"}),
		clientID.String(),
		ptrext.Of(mcpclient.UpdateRequest{ToolPolicyMode: ptrext.Of(domain.MCPToolPolicyModeAllowList)}),
		httptest.NewRequest(http.MethodPatch, "/", nil),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "client is revoked")
}

func TestHandlerReplaceToolPoliciesRejectsOutOfScopeTool(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	handler := mcpclient.NewHandler(
		ptrext.Of(fakeClientStore{client: mcprepo.Client{
			ID:             clientID,
			TenantID:       "tenant-1",
			Name:           "agent",
			RedirectURIs:   []string{"https://example.com/callback"},
			Scopes:         []string{domain.MCPScopeRead},
			ToolPolicyMode: domain.MCPToolPolicyModeAllowList,
			CreatedAt:      time.Now(),
			CreatedBy:      "admin",
		}}),
		nil,
		nil,
		ptrext.Of(fakeToolPolicyStore{}),
	)

	_, err := handler.ReplaceToolPolicies(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1"}),
		clientID.String(),
		ptrext.Of(mcpclient.ReplaceToolPoliciesRequest{
			Policies: []mcpclient.ToolPolicyInput{{
				ToolName: "update_workflow_state",
				Effect:   domain.MCPToolPolicyEffectAllow,
			}},
		}),
		httptest.NewRequest(http.MethodPut, "/", nil),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "tool scope not granted")
}

func TestHandlerReplaceToolPoliciesRejectsRevokedClient(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	revokedAt := time.Now()
	handler := mcpclient.NewHandler(
		ptrext.Of(fakeClientStore{client: mcprepo.Client{
			ID:             clientID,
			TenantID:       "tenant-1",
			Name:           "agent",
			RedirectURIs:   []string{"https://example.com/callback"},
			Scopes:         []string{domain.MCPScopeRead},
			ToolPolicyMode: domain.MCPToolPolicyModeAllowList,
			CreatedAt:      time.Now(),
			CreatedBy:      "admin",
			RevokedAt:      ptrext.Of(revokedAt),
		}}),
		nil,
		nil,
		ptrext.Of(fakeToolPolicyStore{}),
	)

	_, err := handler.ReplaceToolPolicies(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1"}),
		clientID.String(),
		ptrext.Of(mcpclient.ReplaceToolPoliciesRequest{
			Policies: []mcpclient.ToolPolicyInput{{
				ToolName: "list_feedback",
				Effect:   domain.MCPToolPolicyEffectAllow,
			}},
		}),
		httptest.NewRequest(http.MethodPut, "/", nil),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "client is revoked")
}

func TestHandlerRevokeSessionClosesOneSessionAndRevokesItsGrant(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	sessionID := uuid.New()
	tokens := ptrext.Of(fakeTokenStore{})
	sessions := ptrext.Of(fakeSessionStore{sessions: []mcprepo.Session{{
		ID:        sessionID,
		ClientID:  clientID,
		TenantID:  "tenant-1",
		Scopes:    []string{domain.MCPScopeRead},
		CreatedAt: time.Now(),
	}}})
	handler := mcpclient.NewHandler(
		ptrext.Of(fakeClientStore{client: mcprepo.Client{
			ID:             clientID,
			TenantID:       "tenant-1",
			Name:           "agent",
			RedirectURIs:   []string{"https://example.com/callback"},
			Scopes:         []string{domain.MCPScopeRead},
			ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
			CreatedAt:      time.Now(),
			CreatedBy:      "admin",
		}}),
		tokens,
		sessions,
		nil,
	)

	_, err := handler.RevokeSession(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-user", UserType: "admin"}),
		clientID.String(),
		sessionID.String(),
		httptest.NewRequest(http.MethodDelete, "/", nil),
	)
	require.NoError(t, err)
	require.Equal(t, sessionID, tokens.revokedSession)
	require.Equal(t, sessionID, sessions.closedID)
	require.Equal(t, "admin_revoked", sessions.closedReason)
	require.Equal(t, "admin-user", sessions.closedBy)
}

func TestHandlerRevokeRefreshGrantRevokesOnlyTargetGrant(t *testing.T) {
	t.Parallel()

	clientID := uuid.New()
	grantID := uuid.New()
	tokens := ptrext.Of(fakeTokenStore{grants: []mcprepo.RefreshToken{{
		ID:        grantID,
		ClientID:  clientID,
		SessionID: uuid.New(),
		Scopes:    []string{domain.MCPScopeRead},
		ExpiresAt: time.Now().Add(time.Hour),
		CreatedAt: time.Now(),
	}}})
	handler := mcpclient.NewHandler(
		ptrext.Of(fakeClientStore{client: mcprepo.Client{
			ID:             clientID,
			TenantID:       "tenant-1",
			Name:           "agent",
			RedirectURIs:   []string{"https://example.com/callback"},
			Scopes:         []string{domain.MCPScopeRead},
			ToolPolicyMode: domain.MCPToolPolicyModeLegacyAllowAll,
			CreatedAt:      time.Now(),
			CreatedBy:      "admin",
		}}),
		tokens,
		nil,
		nil,
	)

	_, err := handler.RevokeRefreshGrant(
		context.Background(),
		ptrext.Of(session.AuthCtx{TenantID: "tenant-1", UserID: "admin-user", UserType: "admin"}),
		clientID.String(),
		grantID.String(),
		httptest.NewRequest(http.MethodDelete, "/", nil),
	)
	require.NoError(t, err)
	require.Equal(t, grantID, tokens.revokedGrant)
	require.Equal(t, uuid.Nil, tokens.revokedSession)
}
