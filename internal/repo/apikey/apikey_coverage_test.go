// SPDX-License-Identifier: Apache-2.0

package apikey

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestCoreAPIKeyRepoMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableAPIKeyRepo(t)
	keyID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	tenantID := "tenant-1"
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "Insert", call: func() error {
			_, err := r.Insert(ctx, tenantID, []byte("hash"), "prefix", "Production")
			return err
		}},
		{name: "InsertFull", call: func() error {
			_, err := r.InsertFull(ctx, APIKeyInsertParams{TenantID: tenantID, Hash: []byte("hash"), Prefix: "prefix"})
			return err
		}},
		{name: "InsertWithScopes", call: func() error {
			_, err := r.InsertWithScopes(ctx, tenantID, []byte("hash"), "prefix", "Production", []domain.Scope{domain.ScopeFeedbackRead})
			return err
		}},
		{name: "InsertFullWithScopes", call: func() error {
			_, err := r.InsertFullWithScopes(ctx, APIKeyInsertParams{TenantID: tenantID}, []domain.Scope{domain.ScopeFeedbackRead})
			return err
		}},
		{name: "LookupByHash", call: func() error {
			_, err := r.LookupByHash(ctx, []byte("hash"))
			return err
		}},
		{name: "GetByID", call: func() error {
			_, err := r.GetByID(ctx, tenantID, keyID)
			return err
		}},
		{name: "ListByTenant", call: func() error {
			_, err := r.ListByTenant(ctx, tenantID)
			return err
		}},
		{name: "Revoke", call: func() error {
			return r.Revoke(ctx, tenantID, keyID)
		}},
		{name: "Rotate", call: func() error {
			_, err := r.Rotate(ctx, tenantID, RotateParams{OldKeyID: keyID, GracePeriod: time.Hour}, []domain.Scope{domain.ScopeFeedbackRead})
			return err
		}},
		{name: "ExpireGracePeriodKeys", call: func() error {
			_, err := r.ExpireGracePeriodKeys(ctx)
			return err
		}},
		{name: "InsertRequestLog", call: func() error {
			return r.InsertRequestLog(ctx, RequestLogEntry{KeyID: keyID, TenantID: tenantID, Method: "GET", Path: "/v1/feedback", StatusCode: 200})
		}},
		{name: "ListRequestLogs", call: func() error {
			_, err := r.ListRequestLogs(ctx, tenantID, keyID, 10)
			return err
		}},
		{name: "UpdateEnvironment", call: func() error {
			return r.UpdateEnvironment(ctx, tenantID, keyID, "production")
		}},
		{name: "RecordEvent", call: func() error {
			return r.RecordEvent(ctx, tenantID, keyID, string(domain.APIKeyEventUsed), map[string]any{"at": now.Format(time.RFC3339)})
		}},
		{name: "GetExpiringKeys", call: func() error {
			_, err := r.GetExpiringKeys(ctx, time.Hour)
			return err
		}},
	} {
		expectAPIKeyRepoError(t, tc.name, tc.call)
	}
}

func TestAPIKeyRepoServiceAccountAndBindingMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableAPIKeyRepo(t)
	keyID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	accountID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	tenantID := "tenant-1"

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "CreateServiceAccount", call: func() error {
			_, err := r.CreateServiceAccount(ctx, tenantID, "bot", "automation")
			return err
		}},
		{name: "ListServiceAccounts", call: func() error {
			_, err := r.ListServiceAccounts(ctx, tenantID)
			return err
		}},
		{name: "UpdateServiceAccount", call: func() error {
			_, err := r.UpdateServiceAccount(ctx, tenantID, accountID, false)
			return err
		}},
		{name: "DeleteServiceAccount", call: func() error {
			_, err := r.DeleteServiceAccount(ctx, tenantID, accountID)
			return err
		}},
		{name: "LinkKeyToServiceAccount", call: func() error {
			return r.LinkKeyToServiceAccount(ctx, tenantID, keyID, accountID)
		}},
		{name: "AddResourceBinding", call: func() error {
			return r.AddResourceBinding(ctx, keyID, "project", "project-1")
		}},
		{name: "ListResourceBindings", call: func() error {
			_, err := r.ListResourceBindings(ctx, keyID)
			return err
		}},
		{name: "CheckResourceBinding", call: func() error {
			_, err := r.CheckResourceBinding(ctx, keyID, "project", "project-1")
			return err
		}},
		{name: "HasAnyResourceBindings", call: func() error {
			_, err := r.HasAnyResourceBindings(ctx, keyID)
			return err
		}},
	} {
		expectAPIKeyRepoError(t, tc.name, tc.call)
	}
}

func TestAPIKeyRepoEventAndLeakMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableAPIKeyRepo(t)
	keyID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	leakID := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	tenantID := "tenant-1"

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "CreateEventSubscription", call: func() error {
			_, err := r.CreateEventSubscription(ctx, tenantID, []string{string(domain.APIKeyEventUsed)}, "https://hooks.example.test", "secret")
			return err
		}},
		{name: "ListEventSubscriptions", call: func() error {
			_, err := r.ListEventSubscriptions(ctx, tenantID)
			return err
		}},
		{name: "GetSubscriptionsForEvent", call: func() error {
			_, err := r.GetSubscriptionsForEvent(ctx, tenantID, string(domain.APIKeyEventUsed))
			return err
		}},
		{name: "RecordLeakDetection", call: func() error {
			return r.RecordLeakDetection(ctx, keyID, tenantID, "github", "https://example.test/leak")
		}},
		{name: "GetUnresolvedLeaks", call: func() error {
			_, err := r.GetUnresolvedLeaks(ctx, tenantID)
			return err
		}},
		{name: "ResolveLeakDetection", call: func() error {
			return r.ResolveLeakDetection(ctx, leakID, "rotated")
		}},
	} {
		expectAPIKeyRepoError(t, tc.name, tc.call)
	}
}

func TestAPIKeyRepoRotationAndPermissionMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableAPIKeyRepo(t)
	keyID := uuid.MustParse("10101010-1010-1010-1010-101010101010")
	leakID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	scheduleID := uuid.MustParse("12121212-1212-1212-1212-121212121212")
	tenantID := "tenant-1"
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)

	if err := r.InitializeScopeUsage(ctx, keyID, tenantID, nil); err != nil {
		t.Fatalf("InitializeScopeUsage(empty) error = %v", err)
	}
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "CreateRotationSchedule", call: func() error {
			_, err := r.CreateRotationSchedule(ctx, RotationSchedule{
				TenantID: tenantID, KeyID: keyID, RotationIntervalDays: 30,
				NextRotationAt: now, GracePeriodHours: 24, AutoNotify: true,
				NotifyDaysBefore: []int{14, 7, 1},
			})
			return err
		}},
		{name: "GetRotationSchedule", call: func() error {
			_, err := r.GetRotationSchedule(ctx, keyID)
			return err
		}},
		{name: "GetDueRotations", call: func() error {
			_, err := r.GetDueRotations(ctx)
			return err
		}},
		{name: "MarkRotated", call: func() error {
			return r.MarkRotated(ctx, scheduleID, now.Add(30*24*time.Hour))
		}},
		{name: "RecordScopeUsage", call: func() error {
			return r.RecordScopeUsage(ctx, keyID, tenantID, string(domain.ScopeFeedbackRead))
		}},
		{name: "InitializeScopeUsage", call: func() error {
			return r.InitializeScopeUsage(ctx, keyID, tenantID, []domain.Scope{domain.ScopeFeedbackRead})
		}},
		{name: "GetUnusedScopes", call: func() error {
			_, err := r.GetUnusedScopes(ctx, keyID)
			return err
		}},
		{name: "GetScopeUsage", call: func() error {
			_, err := r.GetScopeUsage(ctx, keyID)
			return err
		}},
		{name: "AutoRevokeLeakedKey", call: func() error {
			return r.AutoRevokeLeakedKey(ctx, keyID, leakID)
		}},
	} {
		expectAPIKeyRepoError(t, tc.name, tc.call)
	}
}

func TestAPIKeyRepoSigningAndIdentityMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableAPIKeyRepo(t)
	keyID := uuid.MustParse("13131313-1313-1313-1313-131313131313")
	identityID := uuid.MustParse("14141414-1414-1414-1414-141414141414")
	tenantID := "tenant-1"

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "CreateSigningKey", call: func() error {
			_, err := r.CreateSigningKey(ctx, SigningKey{
				KeyID: keyID, TenantID: tenantID, Algorithm: string(domain.SigningAlgorithmRS256),
				PublicKeyPEM: "pem", KeyFingerprint: "fingerprint",
			})
			return err
		}},
		{name: "GetSigningKeyByFingerprint", call: func() error {
			_, err := r.GetSigningKeyByFingerprint(ctx, "fingerprint")
			return err
		}},
		{name: "ListSigningKeys", call: func() error {
			_, err := r.ListSigningKeys(ctx, keyID)
			return err
		}},
		{name: "CreateManagedIdentity", call: func() error {
			_, err := r.CreateManagedIdentity(ctx, ManagedIdentity{
				TenantID: tenantID, Name: "github", Provider: domain.IdentityProviderGitHubActions,
				ExternalID: "repo:example/app", Audience: "attune", Issuer: "https://token.actions.githubusercontent.com",
				SubjectPattern: "repo:example/app:*", AllowedScopes: []string{string(domain.ScopeFeedbackRead)},
			})
			return err
		}},
		{name: "GetManagedIdentity", call: func() error {
			_, err := r.GetManagedIdentity(ctx, string(domain.IdentityProviderGitHubActions), "repo:example/app")
			return err
		}},
		{name: "ListManagedIdentities", call: func() error {
			_, err := r.ListManagedIdentities(ctx, tenantID)
			return err
		}},
		{name: "GetKeyWithHealthScore", call: func() error {
			_, err := r.GetKeyWithHealthScore(ctx, tenantID, keyID)
			return err
		}},
	} {
		expectAPIKeyRepoError(t, tc.name, tc.call)
	}
	r.TouchManagedIdentity(ctx, identityID)
}

func TestAPIKeyRepoSIEMAndAIAgentMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableAPIKeyRepo(t)
	keyID := uuid.MustParse("15151515-1515-1515-1515-151515151515")
	integrationID := uuid.MustParse("16161616-1616-1616-1616-161616161616")
	tenantID := "tenant-1"

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "CreateSIEMIntegration", call: func() error {
			_, err := r.CreateSIEMIntegration(ctx, SIEMIntegration{
				TenantID: tenantID, Provider: domain.SIEMProviderCustomWebhook, Name: "siem",
				EndpointURL: "https://siem.example.test/events", AuthConfig: json.RawMessage(`{"type":"bearer"}`),
				EventTypes: []string{string(domain.APIKeyEventUsed)}, BatchSize: 100, FlushIntervalSeconds: 30,
			})
			return err
		}},
		{name: "ListSIEMIntegrations", call: func() error {
			_, err := r.ListSIEMIntegrations(ctx, tenantID)
			return err
		}},
		{name: "GetActiveSIEMIntegrations", call: func() error {
			_, err := r.GetActiveSIEMIntegrations(ctx, tenantID, string(domain.APIKeyEventUsed))
			return err
		}},
		{name: "BufferSIEMEvent", call: func() error {
			return r.BufferSIEMEvent(ctx, tenantID, integrationID, string(domain.APIKeyEventUsed), json.RawMessage(`{"ok":true}`))
		}},
		{name: "GetPendingSIEMEvents", call: func() error {
			_, err := r.GetPendingSIEMEvents(ctx, integrationID, 10)
			return err
		}},
		{name: "MarkSIEMEventsSent", call: func() error {
			return r.MarkSIEMEventsSent(ctx, []int64{1, 2, 3})
		}},
		{name: "CreateAIAgentConfig", call: func() error {
			_, err := r.CreateAIAgentConfig(ctx, AIAgentConfig{
				TenantID: tenantID, Name: "agent", AgentType: domain.AIAgentTypeMCPServer,
				AllowedScopes: []string{string(domain.ScopeFeedbackRead)}, MaxTokensPerRequest: 1000,
				MaxRequestsPerMinute: 60, AllowedModels: []string{"gpt-test"},
			})
			return err
		}},
		{name: "ListAIAgentConfigs", call: func() error {
			_, err := r.ListAIAgentConfigs(ctx, tenantID)
			return err
		}},
		{name: "UpdateKeyHealthScore", call: func() error {
			_, err := r.UpdateKeyHealthScore(ctx, keyID)
			return err
		}},
		{name: "GetKeysNeedingHealthUpdate", call: func() error {
			_, err := r.GetKeysNeedingHealthUpdate(ctx, 25)
			return err
		}},
	} {
		expectAPIKeyRepoError(t, tc.name, tc.call)
	}
}

func TestAPIKeyRepoPolicyProjectAndTagMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableAPIKeyRepo(t)
	keyID := uuid.MustParse("17171717-1717-1717-1717-171717171717")
	projectID := uuid.MustParse("18181818-1818-1818-1818-181818181818")
	tenantID := "tenant-1"

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "GetPolicy", call: func() error {
			_, err := r.GetPolicy(ctx, tenantID)
			return err
		}},
		{name: "UpsertPolicy", call: func() error {
			return r.UpsertPolicy(ctx, Policy{TenantID: tenantID, RequireExpiry: true})
		}},
		{name: "CreateProject", call: func() error {
			_, err := r.CreateProject(ctx, tenantID, "project", "production project")
			return err
		}},
		{name: "ListProjects", call: func() error {
			_, err := r.ListProjects(ctx, tenantID)
			return err
		}},
		{name: "BindKeyToProject", call: func() error {
			return r.BindKeyToProject(ctx, tenantID, keyID, projectID)
		}},
		{name: "SetTag", call: func() error {
			return r.SetTag(ctx, keyID, "env", "prod")
		}},
		{name: "GetTags", call: func() error {
			_, err := r.GetTags(ctx, keyID)
			return err
		}},
		{name: "DeleteTag", call: func() error {
			return r.DeleteTag(ctx, keyID, "env")
		}},
		{name: "FindKeysByTag", call: func() error {
			_, err := r.FindKeysByTag(ctx, tenantID, "env", "prod")
			return err
		}},
	} {
		expectAPIKeyRepoError(t, tc.name, tc.call)
	}
}

func TestAPIKeyRepoBudgetApprovalAndTokenMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableAPIKeyRepo(t)
	keyID := uuid.MustParse("19191919-1919-1919-1919-191919191919")
	requestID := uuid.MustParse("20202020-2020-2020-2020-202020202020")
	tokenID := uuid.MustParse("21212121-2121-2121-2121-212121212121")
	tenantID := "tenant-1"
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "SetBudget", call: func() error {
			return r.SetBudget(ctx, tenantID, keyID, 100, ptrext.Of(now))
		}},
		{name: "IncrementBudgetSpent", call: func() error {
			_, _, _, err := r.IncrementBudgetSpent(ctx, keyID, 10)
			return err
		}},
		{name: "CreateApprovalRequest", call: func() error {
			_, err := r.CreateApprovalRequest(ctx, ApprovalRequest{
				TenantID: tenantID, RequesterID: "user-1", RequesterType: "user", KeyLabel: "prod",
				RequestedScopes: []string{string(domain.ScopeFeedbackRead)}, RequestedEnvironment: "production",
				Justification: "deploy", ExpiresAt: now.Add(time.Hour),
			})
			return err
		}},
		{name: "ListPendingApprovals", call: func() error {
			_, err := r.ListPendingApprovals(ctx, tenantID)
			return err
		}},
		{name: "ReviewApproval", call: func() error {
			return r.ReviewApproval(ctx, requestID, "reviewer-1", true, "approved")
		}},
		{name: "CreateTempToken", call: func() error {
			_, err := r.CreateTempToken(ctx, TempToken{
				ParentKeyID: keyID, TenantID: tenantID, TokenHash: []byte("hash"),
				TokenPrefix: "tmp", Purpose: string(domain.TempTokenPurposeCIJob), ExpiresAt: now.Add(time.Hour),
			})
			return err
		}},
		{name: "LookupTempToken", call: func() error {
			_, err := r.LookupTempToken(ctx, []byte("hash"))
			return err
		}},
		{name: "IncrementTempTokenUse", call: func() error {
			_, err := r.IncrementTempTokenUse(ctx, tokenID)
			return err
		}},
		{name: "RevokeTempToken", call: func() error {
			return r.RevokeTempToken(ctx, tokenID)
		}},
	} {
		expectAPIKeyRepoError(t, tc.name, tc.call)
	}
}

func TestAPIKeyRepoOAuthSecretAndAnalyticsMethodsReturnPoolErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableAPIKeyRepo(t)
	keyID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	requestID := uuid.MustParse("23232323-2323-2323-2323-232323232323")
	tenantID := "tenant-1"
	now := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{name: "CreateOAuth2Client", call: func() error {
			_, err := r.CreateOAuth2Client(ctx, OAuth2Client{
				TenantID: tenantID, ClientID: "client-1", ClientSecretHash: []byte("hash"),
				Name: "client", Description: "m2m", RedirectURIs: []string{"https://app.example.test/callback"},
				AllowedScopes: []string{string(domain.ScopeFeedbackRead)},
			})
			return err
		}},
		{name: "LookupOAuth2Client", call: func() error {
			_, err := r.LookupOAuth2Client(ctx, "client-1")
			return err
		}},
		{name: "ListOAuth2Clients", call: func() error {
			_, err := r.ListOAuth2Clients(ctx, tenantID)
			return err
		}},
		{name: "CreateSecretManagerConfig", call: func() error {
			_, err := r.CreateSecretManagerConfig(ctx, SecretManagerConfig{
				TenantID: tenantID, ManagerType: domain.SecretManagerVault, Name: "vault",
				Config: json.RawMessage(`{"addr":"https://vault.example.test"}`),
			})
			return err
		}},
		{name: "ListSecretManagerConfigs", call: func() error {
			_, err := r.ListSecretManagerConfigs(ctx, tenantID)
			return err
		}},
		{name: "GetAnalytics", call: func() error {
			_, err := r.GetAnalytics(ctx, keyID, now.Add(-time.Hour), now)
			return err
		}},
		{name: "GetTenantAnalytics", call: func() error {
			_, err := r.GetTenantAnalytics(ctx, tenantID, now.Add(-time.Hour), now)
			return err
		}},
		{name: "AggregateAnalytics", call: func() error {
			return r.AggregateAnalytics(ctx)
		}},
		{name: "ListApprovalsByStatus", call: func() error {
			_, err := r.ListApprovalsByStatus(ctx, tenantID, string(domain.ApprovalStatusPending))
			return err
		}},
		{name: "GetApprovalRequest", call: func() error {
			_, err := r.GetApprovalRequest(ctx, requestID)
			return err
		}},
	} {
		expectAPIKeyRepoError(t, tc.name, tc.call)
	}
}

func TestAPIKeyRepoScopeMethodsHandleEmptyAndPoolErrorCases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := newUnreachableAPIKeyRepo(t)
	keyID := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")

	batch, err := r.GetScopesBatch(ctx, nil)
	if err != nil {
		t.Fatalf("GetScopesBatch(empty) error = %v", err)
	}
	if len(batch) != 0 {
		t.Fatalf("GetScopesBatch(empty) = %+v, want empty map", batch)
	}
	expectAPIKeyRepoError(t, "GetScopes", func() error {
		_, err := r.GetScopes(ctx, keyID)
		return err
	})
	expectAPIKeyRepoError(t, "GetScopesBatch", func() error {
		_, err := r.GetScopesBatch(ctx, []uuid.UUID{keyID})
		return err
	})
}

func TestAPIKeyRepoFireAndForgetTouchesDoNotPanicOnPoolErrors(t *testing.T) {
	r := newUnreachableAPIKeyRepo(t)
	r.TouchLastUsed(uuid.MustParse("99999999-9999-9999-9999-999999999999"))
}

func newUnreachableAPIKeyRepo(t *testing.T) *APIKeyRepo {
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
	return NewAPIKey(pool)
}

func expectAPIKeyRepoError(t *testing.T, name string, call func() error) {
	t.Helper()
	if err := call(); err == nil {
		t.Fatalf("%s() error = nil, want pool error", name)
	}
}
