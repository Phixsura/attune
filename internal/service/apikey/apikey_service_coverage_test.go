// SPDX-License-Identifier: Apache-2.0

package apikey

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	apikeyrepo "github.com/Phixsura/attune/internal/repo/apikey"
)

func TestAPIKeysLookupAndIssueMethodsReturnRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newUnreachableAPIKeyService(t)
	keyID := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000001")
	tenantID := "tenant-1"
	raw := validRawAPIKey(t)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "Issue", call: func() error {
			_, _, err := s.Issue(ctx, tenantID, "Production")
			return err
		}},
		{name: "IssueFullWithScopes", call: func() error {
			_, _, err := s.IssueFullWithScopes(ctx, IssueParams{
				TenantID: tenantID, Label: "Production", AllowedCIDRs: []string{"10.0.0.0/8"},
			}, []domain.Scope{domain.ScopeFeedbackRead})
			return err
		}},
		{name: "Lookup", call: func() error {
			_, _, err := s.Lookup(ctx, raw)
			return err
		}},
		{name: "LookupWithScopes", call: func() error {
			_, _, _, err := s.LookupWithScopes(ctx, raw)
			return err
		}},
		{name: "LookupWithScopesAndIP", call: func() error {
			_, _, _, _, err := s.LookupWithScopesAndIP(ctx, raw, "10.0.0.10")
			return err
		}},
		{name: "LookupFull", call: func() error {
			_, err := s.LookupFull(ctx, raw, "10.0.0.10")
			return err
		}},
		{name: "Rotate", call: func() error {
			_, _, err := s.Rotate(ctx, RotateParams{TenantID: tenantID, OldKeyID: keyID, GracePeriod: time.Hour})
			return err
		}},
	} {
		expectAPIKeyServiceError(t, tc.name, tc.call)
	}
}

func TestAPIKeysCorePassThroughMethodsReturnRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newUnreachableAPIKeyService(t)
	keyID := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000002")
	tenantID := "tenant-1"

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "List", call: func() error {
			_, err := s.List(ctx, tenantID)
			return err
		}},
		{name: "Revoke", call: func() error {
			return s.Revoke(ctx, tenantID, keyID)
		}},
		{name: "GetByID", call: func() error {
			_, err := s.GetByID(ctx, tenantID, keyID)
			return err
		}},
		{name: "GetScopes", call: func() error {
			_, err := s.GetScopes(ctx, keyID)
			return err
		}},
		{name: "GetScopesBatch", call: func() error {
			_, err := s.GetScopesBatch(ctx, []uuid.UUID{keyID})
			return err
		}},
		{name: "ExpireGracePeriodKeys", call: func() error {
			_, err := s.ExpireGracePeriodKeys(ctx)
			return err
		}},
		{name: "ListRequestLogs", call: func() error {
			_, err := s.ListRequestLogs(ctx, tenantID, keyID, 10)
			return err
		}},
		{name: "UpdateEnvironment", call: func() error {
			return s.UpdateEnvironment(ctx, tenantID, keyID, string(domain.APIKeyEnvProduction))
		}},
	} {
		expectAPIKeyServiceError(t, tc.name, tc.call)
	}
	s.RecordRequestLog(ctx, apikeyrepo.RequestLogEntry{KeyID: keyID, TenantID: tenantID, Method: "GET", Path: "/v1/feedback"})
}

func TestAPIKeysServiceAccountResourceAndEventMethodsReturnRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newUnreachableAPIKeyService(t)
	keyID := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000003")
	accountID := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000004")
	leakID := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000005")
	tenantID := "tenant-1"

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "CreateServiceAccount", call: func() error {
			_, err := s.CreateServiceAccount(ctx, tenantID, "bot", "automation")
			return err
		}},
		{name: "ListServiceAccounts", call: func() error {
			_, err := s.ListServiceAccounts(ctx, tenantID)
			return err
		}},
		{name: "UpdateServiceAccount", call: func() error {
			_, err := s.UpdateServiceAccount(ctx, tenantID, accountID, true)
			return err
		}},
		{name: "DeleteServiceAccount", call: func() error {
			_, err := s.DeleteServiceAccount(ctx, tenantID, accountID)
			return err
		}},
		{name: "LinkKeyToServiceAccount", call: func() error {
			return s.LinkKeyToServiceAccount(ctx, tenantID, keyID, accountID)
		}},
		{name: "AddResourceBinding", call: func() error {
			return s.AddResourceBinding(ctx, keyID, "project", "project-1")
		}},
		{name: "ListResourceBindings", call: func() error {
			_, err := s.ListResourceBindings(ctx, keyID)
			return err
		}},
		{name: "CheckResourceAccess", call: func() error {
			_, err := s.CheckResourceAccess(ctx, keyID, "project", "project-1")
			return err
		}},
		{name: "CreateEventSubscription", call: func() error {
			_, err := s.CreateEventSubscription(ctx, tenantID, []string{string(domain.APIKeyEventUsed)}, "https://hooks.example.test", "secret")
			return err
		}},
		{name: "ListEventSubscriptions", call: func() error {
			_, err := s.ListEventSubscriptions(ctx, tenantID)
			return err
		}},
		{name: "GetSubscriptionsForEvent", call: func() error {
			_, err := s.GetSubscriptionsForEvent(ctx, tenantID, string(domain.APIKeyEventUsed))
			return err
		}},
		{name: "RecordEvent", call: func() error {
			return s.RecordEvent(ctx, tenantID, keyID, string(domain.APIKeyEventUsed), map[string]any{"path": "/v1/feedback"})
		}},
		{name: "GetExpiringKeys", call: func() error {
			_, err := s.GetExpiringKeys(ctx, time.Hour)
			return err
		}},
		{name: "RecordLeakDetection", call: func() error {
			return s.RecordLeakDetection(ctx, keyID, tenantID, "github", "https://example.test/leak")
		}},
		{name: "GetUnresolvedLeaks", call: func() error {
			_, err := s.GetUnresolvedLeaks(ctx, tenantID)
			return err
		}},
		{name: "ResolveLeakDetection", call: func() error {
			return s.ResolveLeakDetection(ctx, leakID, "rotated")
		}},
	} {
		expectAPIKeyServiceError(t, tc.name, tc.call)
	}
}

func TestAPIKeysPolicyProjectAndTokenMethodsReturnRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newUnreachableAPIKeyService(t)
	keyID := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000006")
	projectID := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000007")
	requestID := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000008")
	tenantID := "tenant-1"

	if err := s.SetKeyTags(ctx, tenantID, keyID, nil); err != nil {
		t.Fatalf("SetKeyTags(empty) error = %v", err)
	}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "GetPolicy", call: func() error {
			_, err := s.GetPolicy(ctx, tenantID)
			return err
		}},
		{name: "UpsertPolicy", call: func() error {
			return s.UpsertPolicy(ctx, apikeyrepo.Policy{TenantID: tenantID, RequireExpiry: true})
		}},
		{name: "ListProjects", call: func() error {
			_, err := s.ListProjects(ctx, tenantID)
			return err
		}},
		{name: "CreateProject", call: func() error {
			_, err := s.CreateProject(ctx, tenantID, "project", "production project")
			return err
		}},
		{name: "BindKeyToProject", call: func() error {
			return s.BindKeyToProject(ctx, tenantID, keyID, projectID)
		}},
		{name: "GetKeyTags", call: func() error {
			_, err := s.GetKeyTags(ctx, tenantID, keyID)
			return err
		}},
		{name: "SetKeyTags", call: func() error {
			return s.SetKeyTags(ctx, tenantID, keyID, []apikeyrepo.Tag{{Key: "env", Value: "prod"}})
		}},
		{name: "SetKeyBudget", call: func() error {
			return s.SetKeyBudget(ctx, tenantID, keyID, 100, string(domain.BudgetOverageBlock))
		}},
		{name: "CreateTempToken", call: func() error {
			_, err := s.CreateTempToken(ctx, tenantID, keyID, time.Hour, nil, string(domain.TempTokenPurposeCIJob))
			return err
		}},
		{name: "ListApprovalRequestsPending", call: func() error {
			_, err := s.ListApprovalRequests(ctx, tenantID, "")
			return err
		}},
		{name: "ListApprovalRequestsByStatus", call: func() error {
			_, err := s.ListApprovalRequests(ctx, tenantID, string(domain.ApprovalStatusApproved))
			return err
		}},
		{name: "CreateApprovalRequest", call: func() error {
			_, err := s.CreateApprovalRequest(ctx, apikeyrepo.ApprovalRequest{TenantID: tenantID, RequesterID: "user-1"})
			return err
		}},
		{name: "ReviewApproval", call: func() error {
			_, err := s.ReviewApproval(ctx, tenantID, requestID, "reviewer-1", true, "approved")
			return err
		}},
	} {
		expectAPIKeyServiceError(t, tc.name, tc.call)
	}
}

func TestAPIKeysSecurityIntegrationMethodsReturnRepoErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := newUnreachableAPIKeyService(t)
	keyID := uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000009")
	tenantID := "tenant-1"
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "ListOAuth2Clients", call: func() error {
			_, err := s.ListOAuth2Clients(ctx, tenantID)
			return err
		}},
		{name: "CreateOAuth2Client", call: func() error {
			_, _, err := s.CreateOAuth2Client(ctx, tenantID, "client", "m2m", []string{"https://app.example.test/callback"}, []string{string(domain.ScopeFeedbackRead)})
			return err
		}},
		{name: "GetKeyAnalytics", call: func() error {
			_, err := s.GetKeyAnalytics(ctx, tenantID, keyID, now.Add(-time.Hour), now)
			return err
		}},
		{name: "GetTenantAnalytics", call: func() error {
			_, err := s.GetTenantAnalytics(ctx, tenantID, now.Add(-time.Hour), now)
			return err
		}},
		{name: "ListSecretManagers", call: func() error {
			_, err := s.ListSecretManagers(ctx, tenantID)
			return err
		}},
		{name: "CreateSecretManager", call: func() error {
			_, err := s.CreateSecretManager(ctx, tenantID, string(domain.SecretManagerVault), "vault", map[string]string{"addr": "https://vault.example.test"})
			return err
		}},
		{name: "GetRotationSchedule", call: func() error {
			_, err := s.GetRotationSchedule(ctx, tenantID, keyID)
			return err
		}},
		{name: "CreateRotationSchedule", call: func() error {
			_, err := s.CreateRotationSchedule(ctx, tenantID, keyID, 30, 24, true, []int{14, 7, 1})
			return err
		}},
		{name: "GetUnusedScopes", call: func() error {
			_, _, err := s.GetUnusedScopes(ctx, tenantID, keyID)
			return err
		}},
		{name: "ListSigningKeys", call: func() error {
			_, err := s.ListSigningKeys(ctx, tenantID, keyID)
			return err
		}},
		{name: "CreateSigningKey", call: func() error {
			_, err := s.CreateSigningKey(ctx, tenantID, keyID, string(domain.SigningAlgorithmRS256), "pem", "fingerprint", nil)
			return err
		}},
		{name: "ListManagedIdentities", call: func() error {
			_, err := s.ListManagedIdentities(ctx, tenantID)
			return err
		}},
		{name: "CreateManagedIdentity", call: func() error {
			_, err := s.CreateManagedIdentity(ctx, tenantID, apikeyrepo.ManagedIdentity{
				TenantID: tenantID, Name: "github", Provider: domain.IdentityProviderGitHubActions, ExternalID: "repo:example/app",
			})
			return err
		}},
		{name: "ListSIEMIntegrations", call: func() error {
			_, err := s.ListSIEMIntegrations(ctx, tenantID)
			return err
		}},
		{name: "CreateSIEMIntegration", call: func() error {
			_, err := s.CreateSIEMIntegration(ctx, tenantID, apikeyrepo.SIEMIntegration{
				TenantID: tenantID, Provider: domain.SIEMProviderCustomWebhook, Name: "siem", EndpointURL: "https://siem.example.test/events",
			})
			return err
		}},
		{name: "ListAIAgentConfigs", call: func() error {
			_, err := s.ListAIAgentConfigs(ctx, tenantID)
			return err
		}},
		{name: "CreateAIAgentConfig", call: func() error {
			_, err := s.CreateAIAgentConfig(ctx, tenantID, apikeyrepo.AIAgentConfig{
				TenantID: tenantID, Name: "agent", AgentType: domain.AIAgentTypeMCPServer,
			})
			return err
		}},
		{name: "GetKeyHealth", call: func() error {
			_, err := s.GetKeyHealth(ctx, tenantID, keyID)
			return err
		}},
	} {
		expectAPIKeyServiceError(t, tc.name, tc.call)
	}
}

func newUnreachableAPIKeyService(t *testing.T) *APIKeys {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://attune:attune@127.0.0.1:1/attune?sslmode=disable")
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 25 * time.Millisecond
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)
	return NewAPIKeys(apikeyrepo.NewAPIKey(pool))
}

func validRawAPIKey(t *testing.T) string {
	t.Helper()
	raw, _, _, err := generate()
	if err != nil {
		t.Fatalf("generate() error = %v", err)
	}
	return raw
}

func expectAPIKeyServiceError(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want repo error", name)
	}
}
