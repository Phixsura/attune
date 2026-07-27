// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"

	core "github.com/Phixsura/attune/internal/externalsync"
	infraMetrics "github.com/Phixsura/attune/internal/infra/metrics"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/externalsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
)

func TestCreateConnectionEncryptsWithConnectionScopedAAD(t *testing.T) {
	store := ptrext.Of(fakeSecretStore{})
	repository := newFakeRepo()
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, store)
	service.SetAuditLogger(audit)

	row, err := service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID:           " tenant-1 ",
		Provider:           "GitHub",
		Name:               " Production GitHub ",
		AuthType:           "TOKEN",
		Credential:         " gh-token ",
		ProviderConfigJSON: `{"enterprise":"acme"}`,
		Scopes:             []string{" issues ", "", "issues", "pull_requests"},
		Enabled:            true,
		Actor:              Actor{ID: "admin-1"},
		AuditActor:         auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("CreateConnection returned error: %v", err)
	}

	if row.Provider != "github" || row.Name != "Production GitHub" || row.AuthType != "token" {
		t.Fatalf("normalized row = provider %q name %q auth %q", row.Provider, row.Name, row.AuthType)
	}
	if !row.Enabled || row.Status != repo.ConnectionStatusActive {
		t.Fatalf("enabled/status = %t/%q; want active enabled connection", row.Enabled, row.Status)
	}
	if !reflect.DeepEqual(row.Scopes, []string{"issues", "pull_requests"}) {
		t.Fatalf("scopes = %#v; want normalized unique scopes", row.Scopes)
	}
	if string(store.encryptPlaintext) != "gh-token" {
		t.Fatalf("encrypted plaintext = %q; want trimmed credential", string(store.encryptPlaintext))
	}
	wantAAD := string(connectionAAD(row.TenantID, row.ID, row.Provider))
	if string(store.encryptAAD) != wantAAD {
		t.Fatalf("encrypt AAD = %q; want %q", string(store.encryptAAD), wantAAD)
	}
	if row.CredentialKeyID != "kid-1" || string(row.CredentialCiphertext) != "ciphertext" {
		t.Fatalf("credential envelope = %q/%q; want fake store envelope", row.CredentialKeyID, string(row.CredentialCiphertext))
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_connection.create" {
		t.Fatalf("audit events = %#v; want one external_connection.create event", audit.events)
	}
	if strings.Contains(fmt.Sprint(audit.events[0].After), "gh-token") {
		t.Fatal("audit event leaked plaintext credential")
	}
}

func TestCreateConnectionEncryptsWebhookSecretWithSeparateAAD(t *testing.T) {
	store := ptrext.Of(fakeSecretStore{})
	repository := newFakeRepo()
	service := New(repository, store)

	row, err := service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID:           "tenant-1",
		Provider:           "github",
		Name:               "GitHub",
		AuthType:           "token",
		Credential:         "gh-token",
		WebhookSecret:      "webhook-secret-123",
		ProviderConfigJSON: "{}",
		Enabled:            true,
		Actor:              Actor{ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("CreateConnection returned error: %v", err)
	}

	if len(store.encryptPlaintexts) != 2 {
		t.Fatalf("encrypt calls = %d; want credential and webhook secret", len(store.encryptPlaintexts))
	}
	if string(store.encryptPlaintexts[1]) != "webhook-secret-123" {
		t.Fatalf("webhook plaintext = %q; want trimmed secret", string(store.encryptPlaintexts[1]))
	}
	wantAAD := string(connectionWebhookSecretAAD(row.TenantID, row.ID, row.Provider))
	if string(store.encryptAADs[1]) != wantAAD {
		t.Fatalf("webhook AAD = %q; want %q", string(store.encryptAADs[1]), wantAAD)
	}
	if row.WebhookSecretKeyID != "kid-1" || len(row.WebhookSecretCiphertext) == 0 {
		t.Fatalf("webhook secret envelope was not persisted: %+v", row)
	}
}

func TestCreateConnectionBindsQualifiedProviderInstallation(t *testing.T) {
	installationID := uuid.New()
	resourceID := uuid.New()
	repository := providerInstallationQualificationRepo(installationID, resourceID, []byte(`{"metadata":"read","issues":"write"}`))
	installation := repository.installations[installationID]
	installation.QualificationStatus = repo.TestStatusOK
	installation.BaseURL = "https://github.enterprise.example"
	repository.installations[installationID] = installation
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	row, err := service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID:               "tenant-1",
		ProviderInstallationID: ptrext.Of(installationID),
		Provider:               "github",
		Name:                   "GitHub App",
		AuthType:               "token",
		Credential:             "gh-token",
		ProviderConfigJSON:     "{}",
		Enabled:                true,
		Actor:                  Actor{ID: "admin-1"},
		AuditActor:             auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("CreateConnection returned error: %v", err)
	}
	if row.ProviderInstallationID == nil || ptrext.Indirect(row.ProviderInstallationID) != installationID {
		t.Fatalf("provider installation id = %v; want %s", row.ProviderInstallationID, installationID)
	}
	if row.BaseURL != "https://github.enterprise.example" {
		t.Fatalf("base URL = %q; want inherited installation base URL", row.BaseURL)
	}
	if string(row.ProviderConfig) != `{"owner":"acme","repo":"app"}` {
		t.Fatalf("provider config = %s; want selected repository-derived owner/repo", string(row.ProviderConfig))
	}
	if len(audit.events) != 1 || !strings.Contains(fmt.Sprint(audit.events[0].After), installationID.String()) {
		t.Fatalf("audit after = %#v; want provider installation id", audit.events)
	}
}

func TestCreateConnectionBlocksUnqualifiedProviderInstallation(t *testing.T) {
	installationID := uuid.New()
	resourceID := uuid.New()
	repository := providerInstallationQualificationRepo(installationID, resourceID, []byte(`{"metadata":"read","issues":"write"}`))
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	_, err := service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID:               "tenant-1",
		ProviderInstallationID: ptrext.Of(installationID),
		Provider:               "github",
		Name:                   "GitHub App",
		AuthType:               "token",
		Credential:             "gh-token",
		ProviderConfigJSON:     "{}",
		Enabled:                true,
		Actor:                  Actor{ID: "admin-1"},
	})
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "must pass qualification") {
		t.Fatalf("CreateConnection error = %v; want qualification validation error", err)
	}
}

func TestCreateConnectionRequiresExplicitConfigForMultipleSelectedInstallationRepositories(t *testing.T) {
	installationID := uuid.New()
	resourceID := uuid.New()
	otherResourceID := uuid.New()
	repository := providerInstallationQualificationRepo(installationID, resourceID, []byte(`{"metadata":"read","issues":"write"}`))
	installation := repository.installations[installationID]
	installation.QualificationStatus = repo.TestStatusWarning
	repository.installations[installationID] = installation
	otherResource := repository.resources[resourceID]
	otherResource.ID = otherResourceID
	otherResource.ResourceKey = "acme/other"
	otherResource.DisplayName = "acme/other"
	repository.resources[otherResourceID] = otherResource
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	_, err := service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID:               "tenant-1",
		ProviderInstallationID: ptrext.Of(installationID),
		Provider:               "github",
		Name:                   "GitHub App",
		AuthType:               "token",
		Credential:             "gh-token",
		ProviderConfigJSON:     "{}",
		Enabled:                true,
		Actor:                  Actor{ID: "admin-1"},
	})
	if !errors.Is(err, ErrValidation) || !strings.Contains(err.Error(), "multiple repositories") {
		t.Fatalf("CreateConnection error = %v; want multiple repository validation error", err)
	}

	row, err := service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID:               "tenant-1",
		ProviderInstallationID: ptrext.Of(installationID),
		Provider:               "github",
		Name:                   "GitHub App",
		AuthType:               "token",
		Credential:             "gh-token",
		ProviderConfigJSON:     `{"owner":"acme","repo":"app"}`,
		Enabled:                true,
		Actor:                  Actor{ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("CreateConnection with explicit config returned error: %v", err)
	}
	if row.ProviderInstallationID == nil || ptrext.Indirect(row.ProviderInstallationID) != installationID {
		t.Fatalf("provider installation id = %v; want %s", row.ProviderInstallationID, installationID)
	}
}

func TestUpdateConnectionRotatesConnectionFields(t *testing.T) {
	result := updateConnectionRotationFixture(t)

	if result.row.Name != "GitHub Issues" {
		t.Fatalf("name = %q; want normalized name", result.row.Name)
	}
	if result.row.Enabled {
		t.Fatalf("enabled = true; want disabled connection")
	}
	if result.row.Status != repo.ConnectionStatusDisabled {
		t.Fatalf("status = %q; want disabled", result.row.Status)
	}
	if result.row.UpdatedBy != "admin-2" {
		t.Fatalf("updated by = %q; want admin-2", result.row.UpdatedBy)
	}
	if !reflect.DeepEqual(result.row.Scopes, []string{"issues", "metadata"}) {
		t.Fatalf("scopes = %#v; want normalized unique scopes", result.row.Scopes)
	}
}

func TestUpdateConnectionRotatesCredentialWebhookAndAudits(t *testing.T) {
	result := updateConnectionRotationFixture(t)

	if !result.repository.updateCredential {
		t.Fatalf("credential update flag = false; want true")
	}
	if !result.repository.updateWebhookSecret {
		t.Fatalf("webhook secret update flag = false; want true")
	}
	if len(result.store.encryptPlaintexts) != 2 {
		t.Fatalf("encrypt calls = %d; want credential and webhook secret", len(result.store.encryptPlaintexts))
	}
	if string(result.store.encryptPlaintexts[0]) != "rotated-token" {
		t.Fatalf("credential plaintext = %q; want trimmed token", string(result.store.encryptPlaintexts[0]))
	}
	if string(result.store.encryptPlaintexts[1]) != "rotated-webhook-secret" {
		t.Fatalf("webhook plaintext = %q; want rotated secret", string(result.store.encryptPlaintexts[1]))
	}
	if string(result.store.encryptAADs[0]) != string(connectionAAD("tenant-1", result.connectionID, "github")) {
		t.Fatalf("credential AAD = %q; want connection scope", string(result.store.encryptAADs[0]))
	}
	if string(result.store.encryptAADs[1]) != string(connectionWebhookSecretAAD("tenant-1", result.connectionID, "github")) {
		t.Fatalf("webhook AAD = %q; want webhook scope", string(result.store.encryptAADs[1]))
	}
	if len(result.audit.events) != 1 {
		t.Fatalf("audit events = %#v; want one event", result.audit.events)
	}
	if result.audit.events[0].Action != "external_connection.update" {
		t.Fatalf("audit action = %q; want external_connection.update", result.audit.events[0].Action)
	}
}

type updateConnectionRotationResult struct {
	connectionID uuid.UUID
	repository   *fakeRepo
	store        *fakeSecretStore
	audit        *fakeAuditRecorder
	row          *repo.Connection
}

func updateConnectionRotationFixture(t *testing.T) updateConnectionRotationResult {
	t.Helper()
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                      connectionID,
		TenantID:                "tenant-1",
		Provider:                "github",
		Name:                    "GitHub",
		Enabled:                 true,
		Status:                  repo.ConnectionStatusActive,
		AuthType:                "token",
		BaseURL:                 "https://api.github.com",
		ProviderConfig:          []byte(`{"repo":"acme/app"}`),
		Scopes:                  []string{"issues"},
		CredentialKeyID:         "old-kid",
		CredentialCiphertext:    []byte("old-ciphertext"),
		WebhookSecretKeyID:      "old-webhook-kid",
		WebhookSecretCiphertext: []byte("old-webhook-ciphertext"),
		CreatedBy:               "admin-1",
		UpdatedBy:               "admin-1",
	}
	store := ptrext.Of(fakeSecretStore{})
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, store)
	service.SetAuditLogger(audit)

	enabled := false
	row, err := service.UpdateConnection(context.Background(), UpdateConnectionInput{
		TenantID:           "tenant-1",
		ID:                 connectionID,
		Name:               ptrext.Of(" GitHub Issues "),
		Enabled:            ptrext.Of(enabled),
		Credential:         ptrext.Of(" rotated-token "),
		WebhookSecret:      ptrext.Of("rotated-webhook-secret"),
		BaseURL:            ptrext.Of(" https://api.github.com "),
		ProviderConfigJSON: ptrext.Of(`{"repo":"acme/app","labels":["attune"]}`),
		Scopes:             []string{" issues ", "metadata", "issues"},
		Actor:              Actor{ID: "admin-2"},
		AuditActor:         auditlogsvc.Actor{Type: "admin", ID: "admin-2"},
	})
	if err != nil {
		t.Fatalf("UpdateConnection returned error: %v", err)
	}
	return updateConnectionRotationResult{
		connectionID: connectionID,
		repository:   repository,
		store:        store,
		audit:        audit,
		row:          row,
	}
}

func TestUpdateConnectionClearsWebhookSecret(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                      connectionID,
		TenantID:                "tenant-1",
		Provider:                "github",
		Name:                    "GitHub",
		Enabled:                 true,
		Status:                  repo.ConnectionStatusActive,
		AuthType:                "token",
		ProviderConfig:          []byte(`{}`),
		CredentialKeyID:         "kid-1",
		CredentialCiphertext:    []byte("ciphertext"),
		WebhookSecretKeyID:      "webhook-kid",
		WebhookSecretCiphertext: []byte("webhook-ciphertext"),
		WebhookSecretSetAt:      ptrext.Of(time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC)),
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	row, err := service.UpdateConnection(context.Background(), UpdateConnectionInput{
		TenantID:      "tenant-1",
		ID:            connectionID,
		WebhookSecret: ptrext.Of("   "),
		Actor:         Actor{ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("UpdateConnection returned error: %v", err)
	}
	if row.WebhookSecretKeyID != "" || row.WebhookSecretCiphertext != nil || row.WebhookSecretSetAt != nil {
		t.Fatalf("webhook fields = key %q ciphertext %v set_at %v; want cleared", row.WebhookSecretKeyID, row.WebhookSecretCiphertext, row.WebhookSecretSetAt)
	}
	if repository.updateCredential || !repository.updateWebhookSecret {
		t.Fatalf("update flags credential=%t webhook=%t; want only webhook update", repository.updateCredential, repository.updateWebhookSecret)
	}
}

func TestDeleteConnectionAuditsAndRemovesConnection(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:       connectionID,
		TenantID: "tenant-1",
		Provider: "github",
		Name:     "GitHub",
		AuthType: "token",
	}
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	err := service.DeleteConnection(context.Background(), "tenant-1", connectionID,
		Actor{ID: "admin-1"}, auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("DeleteConnection returned error: %v", err)
	}
	if _, ok := repository.connections[connectionID]; ok {
		t.Fatalf("connection %s still exists after delete", connectionID)
	}
	if repository.deletedConnectionActor != "admin-1" {
		t.Fatalf("delete actor = %q; want admin-1", repository.deletedConnectionActor)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_connection.delete" {
		t.Fatalf("audit events = %#v; want external_connection.delete", audit.events)
	}
}

func TestTestConnectionRedactsURLAndPersistsProbeFailure(t *testing.T) {
	const providerName = "probe"
	var checked core.Connection
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		check: func(_ context.Context, conn core.Connection) (core.CheckResult, error) {
			checked = conn
			return core.CheckResult{
				OK:    false,
				Error: `GET https://api.example.test/hooks/secret?token=abc denied`,
			}, nil
		},
	}))

	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             providerName,
		Name:                 "Probe",
		Enabled:              true,
		Status:               repo.ConnectionStatusActive,
		AuthType:             "token",
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	store := ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("api-token")})
	service := New(repository, store)

	result, err := service.TestConnection(context.Background(), "tenant-1", connectionID, auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("TestConnection returned error: %v", err)
	}
	if result.OK {
		t.Fatal("TestConnection result OK=true; want persisted failure result")
	}
	if strings.Contains(result.Error, "secret") || strings.Contains(result.Error, "token=abc") {
		t.Fatalf("result error was not redacted: %q", result.Error)
	}
	if !strings.Contains(result.Error, "https://api.example.test") {
		t.Fatalf("result error = %q; want host-only URL redaction", result.Error)
	}
	got := repository.connections[connectionID]
	if got.LastTestStatus != repo.TestStatusFailed || got.LastError != result.Error {
		t.Fatalf("persisted probe state = %q/%q; want failed/%q", got.LastTestStatus, got.LastError, result.Error)
	}
	if string(store.decryptAAD) != string(connectionAAD("tenant-1", connectionID, providerName)) {
		t.Fatalf("decrypt AAD = %q; want connection-scoped AAD", string(store.decryptAAD))
	}
	if string(checked.Credential) != "api-token" {
		t.Fatalf("provider credential = %q; want decrypted token", string(checked.Credential))
	}
}

func TestResumeConnectionAuditsAndClearsQuarantine(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:        connectionID,
		TenantID:  "tenant-1",
		Provider:  "github",
		Name:      "GitHub",
		Enabled:   false,
		Status:    repo.ConnectionStatusQuarantined,
		AuthType:  "token",
		LastError: "provider_unavailable: repeated failures",
	}
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	row, err := service.ResumeConnection(context.Background(), ResumeConnectionInput{
		TenantID:   "tenant-1",
		ID:         connectionID,
		Actor:      Actor{ID: "admin-1"},
		AuditActor: auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("ResumeConnection returned error: %v", err)
	}
	if !row.Enabled || row.Status != repo.ConnectionStatusActive || row.LastError != "" {
		t.Fatalf("resumed row = %#v; want enabled active connection with cleared last error", row)
	}
	if len(repository.resumedConnections) != 1 || repository.resumedConnections[0] != connectionID {
		t.Fatalf("resumed connections = %#v; want %s", repository.resumedConnections, connectionID)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_connection.resume" {
		t.Fatalf("audit events = %#v; want external_connection.resume", audit.events)
	}
}

func TestQualifyConnectionReturnsProviderReadinessChecks(t *testing.T) {
	const providerName = "qualify"
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		check: func(context.Context, core.Connection) (core.CheckResult, error) {
			return core.CheckResult{OK: true, Latency: 12 * time.Millisecond, RequestID: "req-1"}, nil
		},
		discover: func(context.Context, core.Connection) ([]core.ObjectSchema, error) {
			return []core.ObjectSchema{{
				Type:           "issue",
				Fields:         []string{"title", "state"},
				RequiredFields: []string{"title"},
				WritableFields: []string{"title", "state"},
			}}, nil
		},
	}))
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             providerName,
		Name:                 "Qualified",
		Enabled:              true,
		Status:               repo.ConnectionStatusActive,
		AuthType:             "token",
		Scopes:               []string{"issues"},
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("token")}))
	service.SetAuditLogger(audit)

	result, err := service.QualifyConnection(context.Background(), "tenant-1", connectionID, auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("QualifyConnection returned error: %v", err)
	}
	if !result.Ready {
		t.Fatalf("qualification ready = false; checks = %#v", result.Checks)
	}
	gotStatuses := map[string]string{}
	for _, check := range result.Checks {
		gotStatuses[check.Name] = check.Status
		if check.DetailJSON == "" {
			t.Fatalf("check %s has empty detail json", check.Name)
		}
	}
	for _, name := range []string{"provider_registered", "credential_decrypt", "provider_check", "schema_discovery", "schema_metadata", "scope_visibility"} {
		if gotStatuses[name] != QualificationStatusOK {
			t.Fatalf("check statuses = %#v; want %s ok", gotStatuses, name)
		}
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_connection.qualify" {
		t.Fatalf("audit events = %#v; want external_connection.qualify", audit.events)
	}
}

func TestCreateProviderInstallationNormalizesResourcesAndAudits(t *testing.T) {
	repository := newFakeRepo()
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	row, resources, err := service.CreateProviderInstallation(context.Background(), CreateProviderInstallationInput{
		TenantID:               " tenant-1 ",
		Provider:               "GitHub",
		DisplayName:            " Production GitHub App ",
		InstallationKind:       repo.InstallationKindGitHubApp,
		ExternalInstallationID: " 12345 ",
		AccountLogin:           " acme ",
		PermissionsJSON:        `{"metadata":"read","issues":"write"}`,
		Resources: []ProviderInstallationResourceInput{{
			ResourceType:    "Repository",
			ResourceKey:     " acme/app ",
			DisplayName:     "",
			HTMLURL:         " https://github.com/acme/app ",
			Selected:        true,
			PermissionsJSON: `{"issues":"write"}`,
		}},
		Actor:      Actor{ID: "admin-1"},
		AuditActor: auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("CreateProviderInstallation returned error: %v", err)
	}
	if row.Provider != "github" || row.DisplayName != "Production GitHub App" ||
		row.InstallationKind != repo.InstallationKindGitHubApp || row.Status != repo.InstallationStatusActive {
		t.Fatalf("installation = %#v; want normalized active GitHub app", row)
	}
	if len(resources) != 1 || resources[0].ResourceType != repo.ResourceTypeRepository ||
		resources[0].ResourceKey != "acme/app" || resources[0].DisplayName != "acme/app" ||
		!resources[0].Selected {
		t.Fatalf("resources = %#v; want normalized selected repository", resources)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_provider_installation.create" {
		t.Fatalf("audit events = %#v; want provider installation create event", audit.events)
	}
}

func TestQualifyProviderInstallationGradesFullGitHubApp(t *testing.T) {
	registerCoreProvider(t, "github", ptrext.Of(fakeProvider{name: "github"}))
	installationID := uuid.New()
	resourceID := uuid.New()
	repository := providerInstallationQualificationRepo(installationID, resourceID, []byte(`{"metadata":"read","issues":"write"}`))
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	result, err := service.QualifyProviderInstallation(context.Background(), "tenant-1", installationID,
		Actor{ID: "admin-1"}, auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("QualifyProviderInstallation returned error: %v", err)
	}
	if !result.Ready || result.Grade != ProviderInstallationGradeFullApp {
		t.Fatalf("qualification = ready %t grade %q checks %#v; want full app", result.Ready, result.Grade, result.Checks)
	}
	updated := repository.installations[installationID]
	if updated.QualificationStatus != QualificationStatusOK ||
		!strings.Contains(string(updated.CapabilityProfile), `"grade":"full_app"`) {
		t.Fatalf("updated installation = %#v profile %s; want ok full_app", updated, string(updated.CapabilityProfile))
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_provider_installation.qualify" {
		t.Fatalf("audit events = %#v; want provider installation qualify event", audit.events)
	}
}

func TestQualifyProviderInstallationBlocksMissingGitHubPermissions(t *testing.T) {
	registerCoreProvider(t, "github", ptrext.Of(fakeProvider{name: "github"}))
	installationID := uuid.New()
	resourceID := uuid.New()
	repository := providerInstallationQualificationRepo(installationID, resourceID, []byte(`{"metadata":"read","issues":"read"}`))
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	result, err := service.QualifyProviderInstallation(context.Background(), "tenant-1", installationID,
		Actor{ID: "admin-1"}, auditlogsvc.Actor{})
	if err != nil {
		t.Fatalf("QualifyProviderInstallation returned error: %v", err)
	}
	if result.Ready || result.Grade != ProviderInstallationGradeBlocked {
		t.Fatalf("qualification = ready %t grade %q; want blocked", result.Ready, result.Grade)
	}
	updated := repository.installations[installationID]
	if updated.QualificationStatus != QualificationStatusFailed ||
		!strings.Contains(updated.LastError, "missing required issue-sync permissions") {
		t.Fatalf("updated status/error = %q/%q; want failed permission error", updated.QualificationStatus, updated.LastError)
	}
}

func TestSelectProviderInstallationResourcesAuditsSelection(t *testing.T) {
	installationID := uuid.New()
	resourceID := uuid.New()
	otherResourceID := uuid.New()
	repository := providerInstallationQualificationRepo(installationID, resourceID, []byte(`{"metadata":"read","issues":"write"}`))
	repository.resources[otherResourceID] = repo.ProviderInstallationResource{
		ID:             otherResourceID,
		TenantID:       "tenant-1",
		InstallationID: installationID,
		Provider:       "github",
		ResourceType:   repo.ResourceTypeRepository,
		ResourceKey:    "acme/other",
		DisplayName:    "acme/other",
		Selected:       true,
		Status:         repo.ResourceStatusActive,
		Permissions:    []byte(`{}`),
	}
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	rows, err := service.SelectProviderInstallationResources(context.Background(), SelectProviderInstallationResourcesInput{
		TenantID:       "tenant-1",
		InstallationID: installationID,
		ResourceIDs:    []uuid.UUID{resourceID, resourceID},
		Actor:          Actor{ID: "admin-1"},
		AuditActor:     auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("SelectProviderInstallationResources returned error: %v", err)
	}
	if len(rows) != 2 || !repository.resources[resourceID].Selected || repository.resources[otherResourceID].Selected {
		t.Fatalf("resource selection = %#v; want only %s selected", repository.resources, resourceID)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_provider_installation.resources_select" {
		t.Fatalf("audit events = %#v; want resources select event", audit.events)
	}
}

func TestProviderInstallationListDeleteAndResourceValidation(t *testing.T) {
	installationID := uuid.New()
	resourceID := uuid.New()
	repository := providerInstallationQualificationRepo(installationID, resourceID, []byte(`{"metadata":"read","issues":"write"}`))
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	listed, err := service.ListProviderInstallations(context.Background(), " tenant-1 ")
	if err != nil {
		t.Fatalf("ListProviderInstallations returned error: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != installationID {
		t.Fatalf("installations = %#v; want seeded installation", listed)
	}
	resources, err := service.ListProviderInstallationResources(context.Background(), " tenant-1 ", installationID)
	if err != nil {
		t.Fatalf("ListProviderInstallationResources returned error: %v", err)
	}
	if len(resources) != 1 || resources[0].ID != resourceID {
		t.Fatalf("resources = %#v; want seeded resource", resources)
	}
	if err := service.DeleteProviderInstallation(context.Background(), "tenant-1", installationID,
		Actor{ID: "  admin-2  "}, auditlogsvc.Actor{Type: "admin", ID: "admin-2"}); err != nil {
		t.Fatalf("DeleteProviderInstallation returned error: %v", err)
	}
	if repository.installations[installationID].Status != repo.InstallationStatusDeleted ||
		repository.resources[resourceID].Selected ||
		repository.resources[resourceID].Status != repo.ResourceStatusRemoved {
		t.Fatalf("deleted installation/resources = %#v / %#v; want deleted and deselected", repository.installations[installationID], repository.resources[resourceID])
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_provider_installation.delete" {
		t.Fatalf("audit events = %#v; want provider installation delete event", audit.events)
	}
	if err := service.DeleteProviderInstallation(context.Background(), "tenant-1", installationID,
		Actor{}, auditlogsvc.Actor{}); err == nil {
		t.Fatal("DeleteProviderInstallation accepted empty actor")
	}
	if _, err := service.SelectProviderInstallationResources(context.Background(), SelectProviderInstallationResourcesInput{
		TenantID:       "tenant-1",
		InstallationID: installationID,
		Actor:          Actor{},
	}); err == nil {
		t.Fatal("SelectProviderInstallationResources accepted empty actor")
	}
}

type createProviderInstallationValidationCase struct {
	name string
	in   CreateProviderInstallationInput
	want string
}

func TestCreateProviderInstallationRequiredValidationBranches(t *testing.T) {
	assertCreateProviderInstallationValidation(t, []createProviderInstallationValidationCase{
		{
			name: "missing tenant",
			in: CreateProviderInstallationInput{
				Provider:          "github",
				DisplayName:       "GitHub",
				InstallationKind:  repo.InstallationKindGitHubApp,
				ResourceSelection: repo.ResourceSelectionAll,
				Actor:             Actor{ID: "admin-1"},
			},
			want: "tenant_id is required",
		},
		{
			name: "invalid provider token",
			in: CreateProviderInstallationInput{
				TenantID:          "tenant-1",
				Provider:          "bad provider",
				DisplayName:       "Bad Provider",
				InstallationKind:  repo.InstallationKindManual,
				ResourceSelection: repo.ResourceSelectionNone,
				Actor:             Actor{ID: "admin-1"},
			},
			want: "provider must match",
		},
		{
			name: "invalid display name",
			in: CreateProviderInstallationInput{
				TenantID:          "tenant-1",
				Provider:          "github",
				DisplayName:       string([]byte{0xff}),
				InstallationKind:  repo.InstallationKindManual,
				ResourceSelection: repo.ResourceSelectionNone,
				Actor:             Actor{ID: "admin-1"},
			},
			want: "display_name",
		},
		{
			name: "invalid kind",
			in: CreateProviderInstallationInput{
				TenantID:          "tenant-1",
				Provider:          "github",
				DisplayName:       "GitHub",
				InstallationKind:  "sidecar",
				ResourceSelection: repo.ResourceSelectionNone,
				Actor:             Actor{ID: "admin-1"},
			},
			want: "installation_kind",
		},
		{
			name: "invalid selection",
			in: CreateProviderInstallationInput{
				TenantID:          "tenant-1",
				Provider:          "github",
				DisplayName:       "GitHub",
				InstallationKind:  repo.InstallationKindManual,
				ResourceSelection: "partial",
				Actor:             Actor{ID: "admin-1"},
			},
			want: "resource_selection",
		},
		{
			name: "missing actor",
			in: CreateProviderInstallationInput{
				TenantID:          "tenant-1",
				Provider:          "github",
				DisplayName:       "GitHub",
				InstallationKind:  repo.InstallationKindManual,
				ResourceSelection: repo.ResourceSelectionNone,
			},
			want: "actor is required",
		},
	})
}

func TestCreateProviderInstallationJSONValidationBranches(t *testing.T) {
	assertCreateProviderInstallationValidation(t, []createProviderInstallationValidationCase{
		{
			name: "bad permissions json",
			in: CreateProviderInstallationInput{
				TenantID:          "tenant-1",
				Provider:          "github",
				DisplayName:       "GitHub",
				InstallationKind:  repo.InstallationKindManual,
				ResourceSelection: repo.ResourceSelectionNone,
				PermissionsJSON:   "{",
				Actor:             Actor{ID: "admin-1"},
			},
			want: "permissions_json",
		},
		{
			name: "bad resource json",
			in: CreateProviderInstallationInput{
				TenantID:          "tenant-1",
				Provider:          "github",
				DisplayName:       "GitHub",
				InstallationKind:  repo.InstallationKindManual,
				ResourceSelection: repo.ResourceSelectionSelected,
				Resources: []ProviderInstallationResourceInput{{
					ResourceType:    repo.ResourceTypeRepository,
					ResourceKey:     "acme/app",
					PermissionsJSON: "{",
				}},
				Actor: Actor{ID: "admin-1"},
			},
			want: "resource.permissions_json",
		},
		{
			name: "bad resource shape",
			in: CreateProviderInstallationInput{
				TenantID:          "tenant-1",
				Provider:          "github",
				DisplayName:       "GitHub",
				InstallationKind:  repo.InstallationKindManual,
				ResourceSelection: repo.ResourceSelectionSelected,
				Resources: []ProviderInstallationResourceInput{{
					ResourceType: "database",
					ResourceKey:  "acme/app",
				}},
				Actor: Actor{ID: "admin-1"},
			},
			want: "resource_type",
		},
	})
}

func assertCreateProviderInstallationValidation(t *testing.T, tests []createProviderInstallationValidationCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := New(newFakeRepo(), ptrext.Of(fakeSecretStore{}))
			_, _, err := service.CreateProviderInstallation(context.Background(), tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CreateProviderInstallation error = %v; want substring %q", err, tt.want)
			}
		})
	}
}

func TestProviderInstallationQualificationFallbackGrades(t *testing.T) {
	registerCoreProvider(t, "github", ptrext.Of(fakeProvider{name: "github"}))
	tests := []struct {
		name         string
		installation repo.ProviderInstallation
		resources    []repo.ProviderInstallationResource
		wantReady    bool
		wantGrade    string
		wantStatus   string
	}{
		{
			name: "github app all resources with boolean permissions",
			installation: repo.ProviderInstallation{
				Provider:               "github",
				InstallationKind:       repo.InstallationKindGitHubApp,
				ExternalInstallationID: "123",
				ResourceSelection:      repo.ResourceSelectionAll,
				Permissions:            []byte(`{"metadata":true,"issues":"admin"}`),
			},
			wantReady:  true,
			wantGrade:  ProviderInstallationGradeFullApp,
			wantStatus: QualificationStatusOK,
		},
		{
			name: "oauth app missing installation id",
			installation: repo.ProviderInstallation{
				Provider:          "github",
				InstallationKind:  repo.InstallationKindOAuthApp,
				ResourceSelection: repo.ResourceSelectionAll,
				Permissions:       []byte(`{"metadata":"read","issues":"write"}`),
			},
			wantReady:  false,
			wantGrade:  ProviderInstallationGradeBlocked,
			wantStatus: QualificationStatusFailed,
		},
		{
			name: "token fallback with provider warnings",
			installation: repo.ProviderInstallation{
				Provider:          "jira",
				InstallationKind:  repo.InstallationKindToken,
				ResourceSelection: repo.ResourceSelectionSelected,
				Permissions:       []byte(`not-json`),
			},
			resources: []repo.ProviderInstallationResource{{
				Selected: true,
				Status:   repo.ResourceStatusActive,
			}},
			wantReady:  false,
			wantGrade:  ProviderInstallationGradeBlocked,
			wantStatus: QualificationStatusFailed,
		},
		{
			name: "manual setup with selected resources",
			installation: repo.ProviderInstallation{
				Provider:          "github",
				InstallationKind:  repo.InstallationKindManual,
				ResourceSelection: repo.ResourceSelectionSelected,
				Permissions:       []byte(`{"metadata":"read","issues":"write"}`),
			},
			resources: []repo.ProviderInstallationResource{{
				Selected: true,
				Status:   repo.ResourceStatusActive,
			}},
			wantReady:  true,
			wantGrade:  ProviderInstallationGradeManualSetup,
			wantStatus: QualificationStatusWarning,
		},
		{
			name: "selected mode without selected resources",
			installation: repo.ProviderInstallation{
				Provider:               "github",
				InstallationKind:       repo.InstallationKindGitHubApp,
				ExternalInstallationID: "123",
				ResourceSelection:      repo.ResourceSelectionSelected,
				Permissions:            []byte(`{"metadata":"read","issues":"write"}`),
			},
			resources: []repo.ProviderInstallationResource{{
				Selected: true,
				Status:   repo.ResourceStatusRemoved,
			}},
			wantReady:  false,
			wantGrade:  ProviderInstallationGradeBlocked,
			wantStatus: QualificationStatusFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := qualifyProviderInstallation(tt.installation, tt.resources)
			status, lastError := providerInstallationQualificationStatus(result)
			if result.Ready != tt.wantReady || result.Grade != tt.wantGrade || status != tt.wantStatus {
				t.Fatalf("qualification = ready %t grade %q status %q error %q checks %#v; want %t/%q/%q",
					result.Ready, result.Grade, status, lastError, result.Checks, tt.wantReady, tt.wantGrade, tt.wantStatus)
			}
			profile := providerInstallationCapabilityProfile(result, tt.resources)
			if !strings.Contains(profile, `"grade":"`+tt.wantGrade+`"`) ||
				!strings.Contains(profile, `"ready":`) {
				t.Fatalf("profile = %s; want grade and readiness", profile)
			}
		})
	}
}

func TestProviderInstallationNormalizationHelpers(t *testing.T) {
	testNormalizeInstallationKind(t)
	testNormalizeResourceSelection(t)
	testNormalizeResourceType(t)
	testNormalizeResourceStatus(t)
	testInstallationStatusForInput(t)
	testPermissionAllowsBranches(t)
}

func testNormalizeInstallationKind(t *testing.T) {
	t.Helper()
	tests := []struct {
		raw  string
		want string
	}{
		{raw: " OAuth_App ", want: repo.InstallationKindOAuthApp},
		{raw: "token", want: repo.InstallationKindToken},
		{raw: "", want: repo.InstallationKindManual},
		{raw: "bad", want: ""},
	}
	for _, tt := range tests {
		if got := normalizeInstallationKind(tt.raw); got != tt.want {
			t.Fatalf("normalizeInstallationKind(%q) = %q; want %q", tt.raw, got, tt.want)
		}
	}
}

func testNormalizeResourceSelection(t *testing.T) {
	t.Helper()
	tests := []struct {
		raw           string
		resourceCount int
		want          string
	}{
		{raw: "", resourceCount: 0, want: repo.ResourceSelectionNone},
		{raw: "", resourceCount: 1, want: repo.ResourceSelectionSelected},
		{raw: "all", resourceCount: 0, want: repo.ResourceSelectionAll},
		{raw: "bad", resourceCount: 0, want: ""},
	}
	for _, tt := range tests {
		if got := normalizeResourceSelection(tt.raw, tt.resourceCount); got != tt.want {
			t.Fatalf("normalizeResourceSelection(%q, %d) = %q; want %q", tt.raw, tt.resourceCount, got, tt.want)
		}
	}
}

func testNormalizeResourceType(t *testing.T) {
	t.Helper()
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "", want: repo.ResourceTypeRepository},
		{raw: "project", want: repo.ResourceTypeProject},
		{raw: "workspace", want: repo.ResourceTypeWorkspace},
		{raw: "organization", want: repo.ResourceTypeOrganization},
		{raw: "database", want: ""},
	}
	for _, tt := range tests {
		if got := normalizeResourceType(tt.raw); got != tt.want {
			t.Fatalf("normalizeResourceType(%q) = %q; want %q", tt.raw, got, tt.want)
		}
	}
}

func testNormalizeResourceStatus(t *testing.T) {
	t.Helper()
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "", want: repo.ResourceStatusActive},
		{raw: "removed", want: repo.ResourceStatusRemoved},
		{raw: "unknown", want: repo.ResourceStatusUnknown},
		{raw: "broken", want: ""},
	}
	for _, tt := range tests {
		if got := normalizeResourceStatus(tt.raw); got != tt.want {
			t.Fatalf("normalizeResourceStatus(%q) = %q; want %q", tt.raw, got, tt.want)
		}
	}
}

func testInstallationStatusForInput(t *testing.T) {
	t.Helper()
	if installationStatusForInput(CreateProviderInstallationInput{
		InstallationKind: repo.InstallationKindGitHubApp,
	}) != repo.InstallationStatusPending {
		t.Fatal("GitHub app without external id should start pending")
	}
	if installationStatusForInput(CreateProviderInstallationInput{
		InstallationKind:       repo.InstallationKindOAuthApp,
		ExternalInstallationID: "oauth-1",
	}) != repo.InstallationStatusActive {
		t.Fatal("OAuth app with external id should start active")
	}
	if installationStatusForInput(CreateProviderInstallationInput{
		InstallationKind: repo.InstallationKindToken,
	}) != repo.InstallationStatusLimited {
		t.Fatal("token installation should start limited")
	}
	if installationStatusForInput(CreateProviderInstallationInput{InstallationKind: "bad"}) != repo.InstallationStatusPending {
		t.Fatal("unknown installation kind should start pending")
	}
}

func testPermissionAllowsBranches(t *testing.T) {
	t.Helper()
	if parsePermissionObject([]byte(`not-json`)) == nil {
		t.Fatal("invalid permission JSON should return an empty object")
	}
	if !permissionAllows(map[string]any{"issues": true}, "issues", "write") ||
		!permissionAllows(map[string]any{"issues": " Write "}, "issues", "write") ||
		permissionAllows(map[string]any{"issues": 1}, "issues", "write") ||
		permissionAllows(map[string]any{}, "issues", "write") {
		t.Fatal("permissionAllows did not cover boolean, string, missing, and unsupported branches")
	}
}

func TestDiscoverConnectionSchemaDecryptsCredentialAndNormalizesSchemas(t *testing.T) {
	const providerName = "schema"
	var discovered core.Connection
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		discover: func(_ context.Context, conn core.Connection) ([]core.ObjectSchema, error) {
			discovered = conn
			return []core.ObjectSchema{
				{
					Type:           " issue ",
					Fields:         []string{"title", "state", "title", ""},
					RequiredFields: []string{" title ", "title", ""},
					WritableFields: []string{" title ", "state", "state"},
				},
				{Type: "", Fields: []string{"ignored"}},
				{Type: "issue", Fields: []string{"duplicate-object"}},
				{Type: "pull_request", Fields: []string{"number"}},
			}, nil
		},
	}))

	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             providerName,
		Name:                 "Schema",
		Enabled:              true,
		Status:               repo.ConnectionStatusActive,
		AuthType:             "token",
		BaseURL:              "https://api.example.test",
		ProviderConfig:       []byte(`{"repo":"app"}`),
		Scopes:               []string{"issues"},
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	store := ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("schema-token")})
	service := New(repository, store)

	schemas, err := service.DiscoverConnectionSchema(context.Background(), "tenant-1", connectionID)
	if err != nil {
		t.Fatalf("DiscoverConnectionSchema returned error: %v", err)
	}
	want := []core.ObjectSchema{
		{
			Type:           "issue",
			Fields:         []string{"title", "state"},
			RequiredFields: []string{"title"},
			WritableFields: []string{"title", "state"},
		},
		{Type: "pull_request", Fields: []string{"number"}, RequiredFields: []string{}, WritableFields: []string{}},
	}
	if !reflect.DeepEqual(schemas, want) {
		t.Fatalf("schemas = %#v; want %#v", schemas, want)
	}
	if string(store.decryptAAD) != string(connectionAAD("tenant-1", connectionID, providerName)) {
		t.Fatalf("decrypt AAD = %q; want connection-scoped AAD", string(store.decryptAAD))
	}
	if string(discovered.Credential) != "schema-token" || string(discovered.ProviderConfig) != `{"repo":"app"}` {
		t.Fatalf("provider connection = %#v; want decrypted credential and config", discovered)
	}
}

func TestPreviewMappingUsesProviderSchemaMetadata(t *testing.T) {
	const providerName = "preview"
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		discover: func(context.Context, core.Connection) ([]core.ObjectSchema, error) {
			return []core.ObjectSchema{{
				Type:           "issue",
				Fields:         []string{"title", "state", "number"},
				RequiredFields: []string{"title"},
				WritableFields: []string{"title", "state"},
			}}, nil
		},
	}))

	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             providerName,
		Name:                 "Preview",
		Enabled:              true,
		Status:               repo.ConnectionStatusActive,
		AuthType:             "token",
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		FieldMapping:       []byte(`{"state":"status"}`),
		StatusMapping:      []byte(`{"open":"new"}`),
		Direction:          repo.DirectionPull,
		Enabled:            true,
	}
	service := New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("preview-token")}))
	override := `{"number":"external_number"}`

	result, err := service.PreviewMapping(context.Background(), PreviewMappingInput{
		TenantID:         " tenant-1 ",
		ID:               mappingID,
		FieldMappingJSON: ptrext.Of(override),
	})
	if err != nil {
		t.Fatalf("PreviewMapping returned error: %v", err)
	}
	if result.Schema.Type != "issue" ||
		!reflect.DeepEqual(result.Schema.RequiredFields, []string{"title"}) ||
		!reflect.DeepEqual(result.Schema.WritableFields, []string{"title", "state"}) {
		t.Fatalf("schema = %#v; want provider metadata", result.Schema)
	}
	if !reflect.DeepEqual(result.Errors, []string{"field_mapping_json must map required provider field title"}) {
		t.Fatalf("errors = %#v; want missing required title", result.Errors)
	}
	if !reflect.DeepEqual(result.Warnings, []string{"field_mapping_json references read-only provider field number"}) {
		t.Fatalf("warnings = %#v; want read-only number warning", result.Warnings)
	}
}

func TestResolveConflictRejectsInvalidResolution(t *testing.T) {
	service := New(newFakeRepo(), ptrext.Of(fakeSecretStore{}))

	_, err := service.ResolveConflict(context.Background(), "tenant-1", uuid.New(), "take_both",
		Actor{ID: "admin-1"}, auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("ResolveConflict error = %v; want ErrValidation", err)
	}
}

func TestBatchResolveConflictsDeduplicatesAndAudits(t *testing.T) {
	const providerName = "batch_resolve_metrics"
	conflictID := uuid.New()
	otherID := uuid.New()
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{ID: connectionID, TenantID: "tenant-1", Provider: providerName}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		ExternalObjectType: "issue",
	}
	repository.batchConflictMappingID = mappingID
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)
	beforeMetric := testutil.ToFloat64(infraMetrics.ExternalSyncConflictsTotal.WithLabelValues(providerName, "issue", "external_wins"))

	result, err := service.BatchResolveConflicts(context.Background(), BatchResolveConflictsInput{
		TenantID:   "tenant-1",
		IDs:        []uuid.UUID{conflictID, conflictID, uuid.Nil, otherID},
		Resolution: "external_wins",
		Actor:      Actor{ID: "admin-1"},
		AuditActor: auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("BatchResolveConflicts returned error: %v", err)
	}
	if len(result.Conflicts) != 2 {
		t.Fatalf("resolved conflicts = %#v; want two deduplicated rows", result.Conflicts)
	}
	if len(repository.batchResolveInputs) != 1 {
		t.Fatalf("batch resolve inputs = %#v; want one call", repository.batchResolveInputs)
	}
	got := repository.batchResolveInputs[0]
	if !reflect.DeepEqual(got.ids, []uuid.UUID{conflictID, otherID}) ||
		got.resolution != "external_wins" || got.actor != "admin-1" {
		t.Fatalf("batch resolve input = %#v; want deduplicated ids and resolution", got)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_sync_conflict.resolve" {
		t.Fatalf("audit events = %#v; want conflict resolve audit", audit.events)
	}
	afterMetric := testutil.ToFloat64(infraMetrics.ExternalSyncConflictsTotal.WithLabelValues(providerName, "issue", "external_wins"))
	if delta := afterMetric - beforeMetric; delta != 2 {
		t.Fatalf("conflict metric delta = %v; want 2", delta)
	}
}

func TestRequestRunResolvesDefaultMapping(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPush,
		Enabled:            true,
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	run, err := service.RequestRun(context.Background(), RequestRunInput{
		TenantID:      "tenant-1",
		ConnectionID:  connectionID,
		LocalObjectID: " 22222222-2222-2222-2222-222222222222 ",
		ExternalKey:   " 42 ",
		Actor:         Actor{ID: "admin-1"},
		AuditActor:    auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("RequestRun returned error: %v", err)
	}
	if run.MappingID == nil || ptrext.Indirect(run.MappingID) != mappingID {
		t.Fatalf("run mapping = %v; want default mapping %s", run.MappingID, mappingID)
	}
	if run.Direction != repo.DirectionPush {
		t.Fatalf("run direction = %q; want mapping direction push", run.Direction)
	}
	if len(repository.insertedRuns) != 1 || repository.insertedRuns[0].MappingID == nil ||
		ptrext.Indirect(repository.insertedRuns[0].MappingID) != mappingID {
		t.Fatalf("inserted runs = %#v; want one run with resolved mapping", repository.insertedRuns)
	}
	if got := string(repository.insertedRuns[0].InputMetadata); got != `{"external_key":"42","local_object_id":"22222222-2222-2222-2222-222222222222"}` {
		t.Fatalf("input metadata = %s; want trimmed run selector", got)
	}
}

func TestRequestRunRejectsDirectionOutsideMapping(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPull,
		Enabled:            true,
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	_, err := service.RequestRun(context.Background(), RequestRunInput{
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		MappingID:    ptrext.Of(mappingID),
		Direction:    repo.DirectionPush,
		Actor:        Actor{ID: "admin-1"},
		AuditActor:   auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("RequestRun error = %v; want ErrValidation", err)
	}
	if len(repository.insertedRuns) != 0 {
		t.Fatalf("inserted runs = %#v; want no run inserted", repository.insertedRuns)
	}
}

func TestServiceReadDelegatesReturnRows(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	runID := uuid.New()
	eventID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:       connectionID,
		TenantID: "tenant-1",
		Provider: "github",
		Name:     "GitHub",
		AuthType: "token",
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPull,
		FieldMapping:       []byte(`{"title":"title"}`),
		StatusMapping:      []byte(`{}`),
		Enabled:            true,
	}
	repository.runDetail = ptrext.Of(repo.RunDetail{
		Run: repo.SyncRun{
			ID:           runID,
			TenantID:     "tenant-1",
			ConnectionID: connectionID,
			MappingID:    ptrext.Of(mappingID),
			Direction:    repo.DirectionPull,
			Status:       repo.RunStatusSucceeded,
		},
	})
	repository.events[eventID] = repo.SyncEvent{
		ID:           eventID,
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		Status:       repo.EventStatusReceived,
	}
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	connections, err := service.ListConnections(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ListConnections returned error: %v", err)
	}
	if len(connections) != 1 || connections[0].ID != connectionID {
		t.Fatalf("connections = %#v; want fake connection", connections)
	}

	mappings, err := service.ListMappings(context.Background(), "tenant-1", connectionID)
	if err != nil {
		t.Fatalf("ListMappings returned error: %v", err)
	}
	if len(mappings) != 1 || mappings[0].ID != mappingID {
		t.Fatalf("mappings = %#v; want fake mapping", mappings)
	}

	detail, err := service.GetRunDetail(context.Background(), "tenant-1", runID)
	if err != nil {
		t.Fatalf("GetRunDetail returned error: %v", err)
	}
	if detail.Run.ID != runID {
		t.Fatalf("run detail = %#v; want run %s", detail, runID)
	}

	event, err := service.GetEvent(context.Background(), "tenant-1", eventID)
	if err != nil {
		t.Fatalf("GetEvent returned error: %v", err)
	}
	if event.ID != eventID {
		t.Fatalf("event = %#v; want event %s", event, eventID)
	}
}

func TestServiceUpdateMappingNormalizesAndAudits(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:       connectionID,
		TenantID: "tenant-1",
		Provider: "github",
		Name:     "GitHub",
		AuthType: "token",
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPull,
		FieldMapping:       []byte(`{"title":"title"}`),
		StatusMapping:      []byte(`{}`),
		Enabled:            true,
	}
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	enabled := false
	updated, err := service.UpdateMapping(context.Background(), UpdateMappingInput{
		TenantID:          "tenant-1",
		ID:                mappingID,
		Direction:         repo.DirectionBidirectional,
		FieldMappingJSON:  `{"title":"summary"}`,
		StatusMappingJSON: `{"closed":"done"}`,
		ConflictPolicy:    "",
		TombstonePolicy:   "",
		Enabled:           ptrext.Of(enabled),
		Actor:             Actor{ID: "admin-1"},
		AuditActor:        auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("UpdateMapping returned error: %v", err)
	}
	if updated.Direction != repo.DirectionBidirectional {
		t.Fatalf("direction = %q; want bidirectional", updated.Direction)
	}
	if string(updated.FieldMapping) != `{"title":"summary"}` {
		t.Fatalf("field mapping = %s; want normalized draft", string(updated.FieldMapping))
	}
	if updated.ConflictPolicy != "manual" {
		t.Fatalf("conflict policy = %q; want manual", updated.ConflictPolicy)
	}
	if updated.TombstonePolicy != "mark_stale" {
		t.Fatalf("tombstone policy = %q; want mark_stale", updated.TombstonePolicy)
	}
	if updated.Enabled {
		t.Fatalf("enabled = true; want disabled mapping")
	}
	if len(audit.events) != 1 {
		t.Fatalf("audit events = %#v; want one mapping update event", audit.events)
	}
	if audit.events[0].Action != "external_sync_mapping.update" {
		t.Fatalf("audit action = %q; want mapping update", audit.events[0].Action)
	}
}

func TestResolveConflictAuditsAndRecordsMetric(t *testing.T) {
	const providerName = "resolve_conflict_metric"
	connectionID := uuid.New()
	mappingID := uuid.New()
	conflictID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{ID: connectionID, TenantID: "tenant-1", Provider: providerName}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		ExternalObjectType: "issue",
	}
	repository.batchConflictMappingID = mappingID
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)
	beforeMetric := testutil.ToFloat64(infraMetrics.ExternalSyncConflictsTotal.WithLabelValues(providerName, "issue", "manual_merge"))

	conflict, err := service.ResolveConflict(context.Background(), "tenant-1", conflictID, "manual_merge",
		Actor{ID: "admin-1"}, auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("ResolveConflict returned error: %v", err)
	}
	if conflict.ID != conflictID || conflict.MappingID != mappingID ||
		conflict.Resolution != "manual_merge" || conflict.ResolvedBy != "admin-1" {
		t.Fatalf("conflict = %#v; want resolved conflict on mapping", conflict)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_sync_conflict.resolve" {
		t.Fatalf("audit events = %#v; want conflict resolve audit", audit.events)
	}
	afterMetric := testutil.ToFloat64(infraMetrics.ExternalSyncConflictsTotal.WithLabelValues(providerName, "issue", "manual_merge"))
	if delta := afterMetric - beforeMetric; delta != 1 {
		t.Fatalf("conflict metric delta = %v; want 1", delta)
	}
}

func TestProcessHelpersCoverPartialAndCombinedStats(t *testing.T) {
	err := newProcessRunError("", "", true, errors.New("boom"))
	if err.Error() != "other: boom" {
		t.Fatalf("process run error = %q; want fallback kind and cause message", err.Error())
	}
	kind, retryable, retryAfter, ok := processRunErrorInfo(err)
	if !ok || kind != "other" || !retryable || retryAfter != nil {
		t.Fatalf("processRunErrorInfo = %q/%t/%v/%t; want other retryable", kind, retryable, retryAfter, ok)
	}
	stats := combineStats(
		repo.ApplyStats{RecordsSeen: 1, RecordsChanged: 1, RecordsFailed: 1},
		repo.ApplyStats{RecordsSeen: 2, RecordsChanged: 1, ConflictsCreated: 1},
	)
	if stats.RecordsSeen != 3 || stats.RecordsChanged != 2 ||
		stats.RecordsFailed != 1 || stats.ConflictsCreated != 1 {
		t.Fatalf("combined stats = %#v; want summed counters", stats)
	}
	if processStatus(stats) != repo.RunStatusPartial ||
		processStatus(repo.ApplyStats{RecordsSeen: 1}) != repo.RunStatusSucceeded {
		t.Fatalf("processStatus did not classify partial/succeeded")
	}
	if nonEmpty("   ", "fallback") != "fallback" || nonEmpty(" value ", "fallback") != "value" {
		t.Fatalf("nonEmpty fallback/trim behavior changed")
	}
}

func TestResetCursorClearsPullCursorAndEnqueuesRun(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionBidirectional,
		Enabled:            true,
	}
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	result, err := service.ResetCursor(context.Background(), ResetCursorInput{
		TenantID:   "tenant-1",
		ID:         mappingID,
		Actor:      Actor{ID: "admin-1"},
		AuditActor: auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("ResetCursor returned error: %v", err)
	}
	if result.Mapping.ID != mappingID || result.Run.MappingID == nil ||
		ptrext.Indirect(result.Run.MappingID) != mappingID {
		t.Fatalf("result = %#v; want mapping and queued run for mapping", result)
	}
	if result.Run.Direction != repo.DirectionPull ||
		result.Run.Trigger != repo.TriggerManual ||
		result.Run.ActorID != "admin-1" {
		t.Fatalf("reset run = %#v; want manual pull run for actor", result.Run)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_sync_cursor.reset" {
		t.Fatalf("audit events = %#v; want cursor reset audit", audit.events)
	}
}

func TestResetCursorRejectsPushOnlyMapping(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPush,
		Enabled:            true,
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	_, err := service.ResetCursor(context.Background(), ResetCursorInput{
		TenantID: "tenant-1",
		ID:       mappingID,
		Actor:    Actor{ID: "admin-1"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("ResetCursor error = %v; want ErrValidation", err)
	}
	if len(repository.insertedRuns) != 0 {
		t.Fatalf("inserted runs = %#v; want none for push-only mapping", repository.insertedRuns)
	}
}

func TestRequestBackfillEnqueuesPullRunAndAudits(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionBidirectional,
		Enabled:            true,
	}
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	result, err := service.RequestBackfill(context.Background(), BackfillInput{
		TenantID:    " tenant-1 ",
		ID:          mappingID,
		ResetCursor: true,
		Actor:       Actor{ID: " admin-1 "},
		AuditActor:  auditlogsvc.Actor{Type: "admin", ID: "admin-1"},
	})
	if err != nil {
		t.Fatalf("RequestBackfill returned error: %v", err)
	}
	if result.Mapping.ID != mappingID ||
		result.Run.MappingID == nil ||
		ptrext.Indirect(result.Run.MappingID) != mappingID {
		t.Fatalf("result = %#v; want mapping and queued run", result)
	}
	if result.Run.Direction != repo.DirectionPull ||
		result.Run.Trigger != repo.TriggerBackfill ||
		result.Run.ActorID != "admin-1" ||
		!repository.enqueuedBackfillReset {
		t.Fatalf("run/reset = %#v/%t; want actor-scoped backfill pull reset", result.Run, repository.enqueuedBackfillReset)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_sync_run.backfill" {
		t.Fatalf("audit events = %#v; want backfill audit", audit.events)
	}
}

func TestRequestBackfillRejectsPushOnlyMapping(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPush,
		Enabled:            true,
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	_, err := service.RequestBackfill(context.Background(), BackfillInput{
		TenantID: "tenant-1",
		ID:       mappingID,
		Actor:    Actor{ID: "admin-1"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("RequestBackfill error = %v; want ErrValidation", err)
	}
	if len(repository.insertedRuns) != 0 {
		t.Fatalf("inserted runs = %#v; want none for push-only mapping", repository.insertedRuns)
	}
}

func TestListRunsNormalizesStatusFilter(t *testing.T) {
	repository := newFakeRepo()
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	_, err := service.ListRuns(context.Background(), ListRunsInput{
		TenantID: " tenant-1 ",
		Status:   "EXTERNAL_SYNC_RUN_STATUS_FAILED",
		Limit:    25,
	})
	if err != nil {
		t.Fatalf("ListRuns returned error: %v", err)
	}
	if repository.listRunsFilter.TenantID != "tenant-1" ||
		repository.listRunsFilter.Status != repo.RunStatusFailed ||
		repository.listRunsFilter.Limit != 25 {
		t.Fatalf("list filter = %#v; want normalized tenant/status/limit", repository.listRunsFilter)
	}
}

func TestListRunsRejectsInvalidStatusFilter(t *testing.T) {
	service := New(newFakeRepo(), ptrext.Of(fakeSecretStore{}))

	_, err := service.ListRuns(context.Background(), ListRunsInput{
		TenantID: "tenant-1",
		Status:   "stuck",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("ListRuns error = %v; want ErrValidation", err)
	}
}

func TestRecordTimelineRejectsMissingRecordSelector(t *testing.T) {
	service := New(newFakeRepo(), ptrext.Of(fakeSecretStore{}))

	_, err := service.RecordTimeline(context.Background(), RecordTimelineInput{
		TenantID:  "tenant-1",
		MappingID: uuid.New(),
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("RecordTimeline error = %v; want ErrValidation", err)
	}
}

func TestRecordTimelinePassesTrimmedFilter(t *testing.T) {
	mappingID := uuid.New()
	rows := []repo.RecordTimelineEntry{{
		Kind:          "link",
		OccurredAt:    time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC),
		Status:        "linked",
		LocalObjectID: "cr-1",
		ExternalKey:   "#123",
	}}
	repository := newFakeRepo()
	repository.timelineRows = rows
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	got, err := service.RecordTimeline(context.Background(), RecordTimelineInput{
		TenantID:      " tenant-1 ",
		MappingID:     mappingID,
		LocalObjectID: " cr-1 ",
		Limit:         7,
	})
	if err != nil {
		t.Fatalf("RecordTimeline returned error: %v", err)
	}
	if !reflect.DeepEqual(got, rows) {
		t.Fatalf("rows = %#v; want fake timeline rows", got)
	}
	if repository.recordTimelineFilter.TenantID != "tenant-1" ||
		repository.recordTimelineFilter.MappingID != mappingID ||
		repository.recordTimelineFilter.LocalObjectID != "cr-1" ||
		repository.recordTimelineFilter.Limit != 7 {
		t.Fatalf("filter = %#v; want trimmed selector", repository.recordTimelineFilter)
	}
}

func TestRecordEventNormalizesAndDeduplicates(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:       connectionID,
		TenantID: "tenant-1",
		Provider: "github",
		Name:     "GitHub",
		AuthType: "token",
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPull,
		Enabled:            true,
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	first, err := service.RecordEvent(context.Background(), RecordEventInput{
		TenantID:              "tenant-1",
		ConnectionID:          connectionID,
		MappingID:             ptrext.Of(mappingID),
		EventType:             "issues",
		ExternalEventID:       "delivery-1",
		NormalizedPayloadJSON: `{"action":"opened","issue":42}`,
	})
	if err != nil {
		t.Fatalf("RecordEvent returned error: %v", err)
	}
	second, err := service.RecordEvent(context.Background(), RecordEventInput{
		TenantID:              "tenant-1",
		ConnectionID:          connectionID,
		MappingID:             ptrext.Of(mappingID),
		EventType:             "issues",
		ExternalEventID:       "delivery-1",
		NormalizedPayloadJSON: `{"action":"opened","issue":42}`,
	})
	if err != nil {
		t.Fatalf("RecordEvent duplicate returned error: %v", err)
	}
	if first.ID != second.ID || len(repository.events) != 1 {
		t.Fatalf("events = %#v; want duplicate dedupe to return original event", repository.events)
	}
	if first.Provider != "github" || first.SignatureStatus != repo.EventSignatureVerified ||
		first.Status != repo.EventStatusReceived || first.DedupeKey != "github:issues:delivery-1" {
		t.Fatalf("event = %#v; want normalized verified GitHub delivery", first)
	}
	if len(first.PayloadDigest) != 64 || strings.HasPrefix(first.PayloadDigest, "sha256:") {
		t.Fatalf("payload digest = %q; want raw 64-char hex digest", first.PayloadDigest)
	}
}

func TestRecordEventCapturesFailedSignatureAsTerminalEvent(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:       connectionID,
		TenantID: "tenant-1",
		Provider: "github",
		Name:     "GitHub",
		AuthType: "token",
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	event, err := service.RecordEvent(context.Background(), RecordEventInput{
		TenantID:              "tenant-1",
		ConnectionID:          connectionID,
		EventType:             "issues",
		DedupeKey:             "bad-signature-1",
		SignatureStatus:       repo.EventSignatureFailed,
		NormalizedPayloadJSON: `{}`,
		FailureReason:         "signature mismatch at https://api.example.test/hooks?token=secret",
	})
	if err != nil {
		t.Fatalf("RecordEvent returned error: %v", err)
	}
	if event.Status != repo.EventStatusFailed || event.SignatureStatus != repo.EventSignatureFailed {
		t.Fatalf("event status/signature = %q/%q; want failed signature event", event.Status, event.SignatureStatus)
	}
	if strings.Contains(event.FailureReason, "token=secret") {
		t.Fatalf("failure reason leaked URL secret: %q", event.FailureReason)
	}
}

func TestRecordGitHubWebhookVerifiesSignatureAndStoresRawDigest(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                      connectionID,
		TenantID:                "tenant-1",
		Provider:                "github",
		Name:                    "GitHub",
		AuthType:                "token",
		WebhookSecretKeyID:      "kid-1",
		WebhookSecretCiphertext: []byte("ciphertext"),
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionBidirectional,
		Enabled:            true,
	}
	secret := []byte("webhook-secret-123")
	store := ptrext.Of(fakeSecretStore{decryptPlaintext: secret})
	service := New(repository, store)
	body := []byte(`{"action":"opened","repository":{"full_name":"acme/widget","html_url":"https://github.com/acme/widget"},"issue":{"number":42,"title":"Need SSO","state":"open","html_url":"https://github.com/acme/widget/issues/42"},"comment":{"id":9001,"body":"customer token abc123","html_url":"https://github.com/acme/widget/issues/42#issuecomment-9001"},"sender":{"login":"octo"}}`)

	event, err := service.RecordGitHubWebhook(context.Background(), GitHubWebhookInput{
		TenantID:        "tenant-1",
		ConnectionID:    connectionID,
		EventType:       "issues",
		DeliveryID:      "delivery-1",
		SignatureSHA256: githubSignature(secret, body),
		Body:            body,
	})
	if err != nil {
		t.Fatalf("RecordGitHubWebhook returned error: %v", err)
	}

	if event.SignatureStatus != repo.EventSignatureVerified || event.Status != repo.EventStatusReplayed {
		t.Fatalf("event = %#v; want verified replayed event", event)
	}
	if len(repository.insertedRuns) != 1 || repository.insertedRuns[0].Trigger != repo.TriggerWebhook ||
		repository.insertedRuns[0].ActorID != "github-webhook" {
		t.Fatalf("inserted runs = %#v; want one webhook run", repository.insertedRuns)
	}
	if event.PayloadDigest != eventPayloadDigest(body) {
		t.Fatalf("payload digest = %q; want raw body digest %q", event.PayloadDigest, eventPayloadDigest(body))
	}
	if !strings.Contains(string(event.NormalizedPayload), `"number":42`) ||
		strings.Contains(string(event.NormalizedPayload), "webhook-secret-123") {
		t.Fatalf("normalized payload = %s; want compact issue payload without secrets", string(event.NormalizedPayload))
	}
	if strings.Contains(string(event.NormalizedPayload), "customer token abc123") ||
		!strings.Contains(string(event.NormalizedPayload), `"body_present":true`) ||
		!strings.Contains(string(event.NormalizedPayload), `"body_digest":`) {
		t.Fatalf("normalized payload = %s; want comment body redacted with digest metadata", string(event.NormalizedPayload))
	}
	if string(store.decryptAAD) != string(connectionWebhookSecretAAD("tenant-1", connectionID, "github")) {
		t.Fatalf("decrypt AAD = %q; want webhook-scoped AAD", string(store.decryptAAD))
	}
}

func TestRecordGitHubWebhookRecordsFailedSignature(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                      connectionID,
		TenantID:                "tenant-1",
		Provider:                "github",
		Name:                    "GitHub",
		AuthType:                "token",
		WebhookSecretKeyID:      "kid-1",
		WebhookSecretCiphertext: []byte("ciphertext"),
	}
	service := New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("webhook-secret-123")}))
	body := []byte(`{"zen":"Keep it logically awesome."}`)

	event, err := service.RecordGitHubWebhook(context.Background(), GitHubWebhookInput{
		TenantID:        "tenant-1",
		ConnectionID:    connectionID,
		EventType:       "ping",
		DeliveryID:      "delivery-2",
		SignatureSHA256: "sha256=bad",
		Body:            body,
	})
	if !errors.Is(err, ErrWebhookSignature) {
		t.Fatalf("RecordGitHubWebhook error = %v; want ErrWebhookSignature", err)
	}
	if event == nil || event.Status != repo.EventStatusFailed ||
		event.SignatureStatus != repo.EventSignatureFailed ||
		event.FailureReason == "" {
		t.Fatalf("event = %#v; want failed signature event", event)
	}
}

func TestRecordGitHubWebhookStoresPingWithoutEnqueue(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                      connectionID,
		TenantID:                "tenant-1",
		Provider:                "github",
		Name:                    "GitHub",
		AuthType:                "token",
		WebhookSecretKeyID:      "kid-1",
		WebhookSecretCiphertext: []byte("ciphertext"),
	}
	secret := []byte("webhook-secret-123")
	service := New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: secret}))
	body := []byte(`{"zen":"Approachable is better than simple."}`)

	event, err := service.RecordGitHubWebhook(context.Background(), GitHubWebhookInput{
		TenantID:        "tenant-1",
		ConnectionID:    connectionID,
		EventType:       "ping",
		DeliveryID:      "delivery-ping",
		SignatureSHA256: githubSignature(secret, body),
		Body:            body,
	})
	if err != nil {
		t.Fatalf("RecordGitHubWebhook returned error: %v", err)
	}
	if event.SignatureStatus != repo.EventSignatureVerified || event.Status != repo.EventStatusReceived {
		t.Fatalf("event = %#v; want verified received ping", event)
	}
	if len(repository.insertedRuns) != 0 {
		t.Fatalf("inserted runs = %#v; want no run for ping", repository.insertedRuns)
	}
}

func TestListEventsNormalizesStatusFilter(t *testing.T) {
	connectionID := uuid.New()
	eventID := uuid.New()
	beforeID := uuid.New()
	repository := newFakeRepo()
	repository.events[eventID] = repo.SyncEvent{
		ID:           eventID,
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		Status:       repo.EventStatusFailed,
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	result, err := service.ListEvents(context.Background(), ListEventsInput{
		TenantID:     " tenant-1 ",
		ConnectionID: ptrext.Of(connectionID),
		Status:       "EXTERNAL_SYNC_EVENT_STATUS_FAILED",
		BeforeID:     ptrext.Of(beforeID),
		Limit:        25,
	})
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if len(result.Events) != 1 || result.Events[0].ID != eventID {
		t.Fatalf("events = %#v; want failed event", result.Events)
	}
	if repository.listEventsFilter.TenantID != "tenant-1" ||
		repository.listEventsFilter.ConnectionID == nil ||
		ptrext.Indirect(repository.listEventsFilter.ConnectionID) != connectionID ||
		repository.listEventsFilter.Status != repo.EventStatusFailed ||
		repository.listEventsFilter.BeforeID == nil ||
		ptrext.Indirect(repository.listEventsFilter.BeforeID) != beforeID ||
		repository.listEventsFilter.Limit != 25 {
		t.Fatalf("list events filter = %#v; want normalized filter", repository.listEventsFilter)
	}
}

func TestListEventsRejectsInvalidStatusFilter(t *testing.T) {
	service := New(newFakeRepo(), ptrext.Of(fakeSecretStore{}))

	_, err := service.ListEvents(context.Background(), ListEventsInput{
		TenantID: "tenant-1",
		Status:   "wedged",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("ListEvents error = %v; want ErrValidation", err)
	}
}

func TestReplayEventEnqueuesWebhookRun(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	eventID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:       connectionID,
		TenantID: "tenant-1",
		Provider: "github",
		Name:     "GitHub",
		AuthType: "token",
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionBidirectional,
		Enabled:            true,
	}
	repository.events[eventID] = repo.SyncEvent{
		ID:              eventID,
		TenantID:        "tenant-1",
		ConnectionID:    connectionID,
		Provider:        "github",
		EventType:       "issues",
		DedupeKey:       "github:issues:delivery-1",
		SignatureStatus: repo.EventSignatureVerified,
		Status:          repo.EventStatusReceived,
		PayloadDigest:   strings.Repeat("a", 64),
	}
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	event, run, err := service.ReplayEvent(context.Background(), "tenant-1", eventID,
		Actor{ID: "admin-1"}, auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("ReplayEvent returned error: %v", err)
	}
	if event.Status != repo.EventStatusReplayed || event.RunID == nil || ptrext.Indirect(event.RunID) != run.ID {
		t.Fatalf("event = %#v run = %#v; want replayed event linked to run", event, run)
	}
	if run.Trigger != repo.TriggerWebhook || run.Direction != repo.DirectionPull ||
		run.MappingID == nil || ptrext.Indirect(run.MappingID) != mappingID {
		t.Fatalf("run = %#v; want webhook-triggered pull run for mapping %s", run, mappingID)
	}
	if len(audit.events) != 1 || audit.events[0].Action != "external_sync_event.replay" {
		t.Fatalf("audit events = %#v; want event replay audit", audit.events)
	}
}

func TestReplayEventRejectsUnverifiedSignature(t *testing.T) {
	connectionID := uuid.New()
	eventID := uuid.New()
	repository := newFakeRepo()
	repository.events[eventID] = repo.SyncEvent{
		ID:              eventID,
		TenantID:        "tenant-1",
		ConnectionID:    connectionID,
		Provider:        "github",
		EventType:       "issues",
		DedupeKey:       "bad-signature",
		SignatureStatus: repo.EventSignatureFailed,
		Status:          repo.EventStatusFailed,
		PayloadDigest:   strings.Repeat("b", 64),
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	_, _, err := service.ReplayEvent(context.Background(), "tenant-1", eventID,
		Actor{ID: "admin-1"}, auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("ReplayEvent error = %v; want ErrValidation", err)
	}
	if len(repository.insertedRuns) != 0 {
		t.Fatalf("inserted runs = %#v; want none", repository.insertedRuns)
	}
}

func TestRetryFailureRunAndHealthDelegateWithAudit(t *testing.T) {
	runID := uuid.New()
	failureID := uuid.New()
	repository := newFakeRepo()
	repository.health = repo.Health{EnabledConnections: 3, DeadRuns: 1}
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)

	run, err := service.RetryRun(context.Background(), "tenant-1", runID,
		Actor{ID: "admin-1"}, auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("RetryRun returned error: %v", err)
	}
	if run.ID != runID || run.Status != repo.RunStatusQueued {
		t.Fatalf("retry run = %#v; want queued run %s", run, runID)
	}

	failure, err := service.RetryFailure(context.Background(), "tenant-1", failureID,
		Actor{ID: "admin-1"}, auditlogsvc.Actor{Type: "admin", ID: "admin-1"})
	if err != nil {
		t.Fatalf("RetryFailure returned error: %v", err)
	}
	if failure.ID != failureID || failure.ResolvedBy != "admin-1" {
		t.Fatalf("retry failure = %#v; want actor-resolved failure", failure)
	}

	health, err := service.Health(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if health.EnabledConnections != 3 || health.DeadRuns != 1 {
		t.Fatalf("health = %#v; want fake health", health)
	}
	if len(audit.events) != 2 ||
		audit.events[0].Action != "external_sync_run.retry" ||
		audit.events[1].Action != "external_sync_failure.retry" {
		t.Fatalf("audit events = %#v; want run and failure retry audits", audit.events)
	}
}

func TestWorkerProcessOnceMarksSuccess(t *testing.T) {
	const providerName = "worker_success"
	pulls := 0
	mappingID := uuid.New()
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		pull: func(_ context.Context, req core.PullRequest) (core.PullResult, error) {
			pulls++
			if string(req.Connection.Credential) != "worker-token" {
				t.Fatalf("pull credential = %q; want worker-token", string(req.Connection.Credential))
			}
			if req.MappingID != mappingID.String() {
				t.Fatalf("pull mapping id = %q; want %s", req.MappingID, mappingID)
			}
			if string(req.Cursor) != `{"page":1}` {
				t.Fatalf("pull cursor = %s; want page 1 cursor", string(req.Cursor))
			}
			if string(req.InputMetadata) != `{"issue_number":42}` {
				t.Fatalf("pull input metadata = %s; want issue hint", string(req.InputMetadata))
			}
			return core.PullResult{
				Records: []core.ExternalRecord{{
					Key:     "ISSUE-1",
					Version: "v1",
					Payload: []byte(`{"title":"Sync me"}`),
				}},
				Children: []core.ExternalChildRecord{{
					ParentKey: "ISSUE-1",
					Type:      repo.ChildTypeComment,
					Key:       "COMMENT-1",
					Version:   "v1",
					Payload:   []byte(`{"body":"Please sync me"}`),
				}},
				NextCursor: []byte(`{"page":2}`),
			}, nil
		},
	}))

	connectionID := uuid.New()
	runID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             providerName,
		Name:                 "Worker",
		AuthType:             "token",
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	repository.cursor = []byte(`{"page":1}`)
	repository.metricSnapshot = repo.MetricSnapshot{Points: []repo.MetricPoint{{
		Provider:           providerName,
		ExternalObjectType: "issue",
		DeadRuns:           2,
		LagSeconds:         37,
	}}}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPull,
		Enabled:            true,
	}
	repository.claimedRuns = []repo.SyncRun{{
		ID:            runID,
		TenantID:      "tenant-1",
		ConnectionID:  connectionID,
		MappingID:     ptrext.Of(mappingID),
		Direction:     repo.DirectionPull,
		Attempts:      1,
		InputMetadata: []byte(`{"issue_number":42}`),
	}}
	worker := NewWorker(New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("worker-token")})))
	worker.Configure(time.Hour, 10, 5)
	beforeSeen := testutil.ToFloat64(infraMetrics.ExternalSyncRecordsTotal.WithLabelValues(providerName, "issue", "pull", "seen"))
	beforeChanged := testutil.ToFloat64(infraMetrics.ExternalSyncRecordsTotal.WithLabelValues(providerName, "issue", "pull", "changed"))

	worker.ProcessOnce(context.Background())

	if pulls != 1 {
		t.Fatalf("pulls = %d; want 1", pulls)
	}
	if !reflect.DeepEqual(repository.succeededRuns, []uuid.UUID{runID}) {
		t.Fatalf("succeeded runs = %#v; want run %s", repository.succeededRuns, runID)
	}
	if len(repository.applyPullInputs) != 1 {
		t.Fatalf("apply pull inputs = %#v; want one applied pull result", repository.applyPullInputs)
	}
	assertWorkerPullApply(t, repository.applyPullInputs[0], mappingID)
	if len(repository.attempts) != 1 || repository.attempts[0].Result != "succeeded" {
		t.Fatalf("attempts = %#v; want one succeeded attempt", repository.attempts)
	}
	if len(repository.failedMarks) != 0 {
		t.Fatalf("failed marks = %#v; want none", repository.failedMarks)
	}
	assertWorkerPullMetrics(t, providerName, beforeSeen, beforeChanged)
}

func assertWorkerPullApply(t *testing.T, gotApply repo.ApplyPullInput, mappingID uuid.UUID) {
	t.Helper()
	if gotApply.MappingID != mappingID || string(gotApply.CursorAfter) != `{"page":2}` ||
		string(gotApply.InputMetadata) != `{"issue_number":42}` ||
		len(gotApply.Records) != 1 || len(gotApply.Children) != 1 {
		t.Fatalf("applied pull = %#v; want mapping, next cursor, input metadata, one record, and one child", gotApply)
	}
}

func assertWorkerPullMetrics(t *testing.T, providerName string, beforeSeen, beforeChanged float64) {
	t.Helper()
	afterSeen := testutil.ToFloat64(infraMetrics.ExternalSyncRecordsTotal.WithLabelValues(providerName, "issue", "pull", "seen"))
	afterChanged := testutil.ToFloat64(infraMetrics.ExternalSyncRecordsTotal.WithLabelValues(providerName, "issue", "pull", "changed"))
	if afterSeen-beforeSeen != 2 || afterChanged-beforeChanged != 2 {
		t.Fatalf("record metrics delta seen=%v changed=%v; want 2/2", afterSeen-beforeSeen, afterChanged-beforeChanged)
	}
	if got := testutil.ToFloat64(infraMetrics.ExternalSyncDeadRuns.WithLabelValues(providerName, "issue")); got != 2 {
		t.Fatalf("dead run gauge = %v; want 2", got)
	}
	if got := testutil.ToFloat64(infraMetrics.ExternalSyncLagSeconds.WithLabelValues(providerName, "issue")); got != 37 {
		t.Fatalf("lag gauge = %v; want 37", got)
	}
}

func TestWorkerProcessOncePushesPreparedLocalRecords(t *testing.T) {
	const providerName = "worker_push"
	mappingID := uuid.New()
	localID := uuid.NewString()
	var pushed core.PushRequest
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		push: func(_ context.Context, req core.PushRequest) (core.PushResult, error) {
			pushed = req
			return core.PushResult{Results: []core.WriteResult{{
				LocalID: localID,
				Key:     "42",
				URL:     "https://github.com/acme/app/issues/42",
				Version: "2026-07-08T12:00:00Z",
			}}}, nil
		},
	}))

	connectionID := uuid.New()
	runID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             providerName,
		Name:                 "Worker",
		AuthType:             "token",
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPush,
		Enabled:            true,
	}
	repository.preparedPushRecords = []repo.PushRecord{{
		LocalObjectID: localID,
		LocalVersion:  "2026-07-08T11:00:00Z",
		Payload:       []byte(`{"title":"CR-1 Sync me"}`),
	}}
	repository.claimedRuns = []repo.SyncRun{{
		ID:           runID,
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		MappingID:    ptrext.Of(mappingID),
		Direction:    repo.DirectionPush,
		Attempts:     1,
	}}

	worker := NewWorker(New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("worker-token")})))
	worker.Configure(time.Hour, 10, 5)
	worker.ProcessOnce(context.Background())

	if pushed.MappingID != mappingID.String() || len(pushed.Records) != 1 {
		t.Fatalf("pushed request = %#v; want one record for mapping %s", pushed, mappingID)
	}
	if pushed.Records[0].ID != localID || string(pushed.Records[0].Payload) != `{"title":"CR-1 Sync me"}` {
		t.Fatalf("pushed record = %#v; want prepared local record", pushed.Records[0])
	}
	if len(repository.applyPushInputs) != 1 {
		t.Fatalf("apply push inputs = %#v; want one applied push result", repository.applyPushInputs)
	}
	gotApply := repository.applyPushInputs[0]
	if gotApply.MappingID != mappingID || len(gotApply.Records) != 1 || len(gotApply.Results) != 1 {
		t.Fatalf("applied push = %#v; want mapping, record, and result", gotApply)
	}
	if gotApply.Results[0].ExternalURL != "https://github.com/acme/app/issues/42" {
		t.Fatalf("push result url = %q; want GitHub issue URL", gotApply.Results[0].ExternalURL)
	}
	if !reflect.DeepEqual(repository.succeededRuns, []uuid.UUID{runID}) {
		t.Fatalf("succeeded runs = %#v; want run %s", repository.succeededRuns, runID)
	}
}

func TestProcessRunBidirectionalCombinesPullAndPush(t *testing.T) {
	const providerName = "process_bidirectional"
	mappingID := uuid.New()
	localID := uuid.NewString()
	var pullCalled bool
	var pushed core.PushRequest
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		pull: func(_ context.Context, req core.PullRequest) (core.PullResult, error) {
			pullCalled = true
			if req.MappingID != mappingID.String() {
				t.Fatalf("pull mapping id = %q; want %s", req.MappingID, mappingID)
			}
			return core.PullResult{
				Records: []core.ExternalRecord{{
					Key:     "ISS-1",
					URL:     "https://github.com/acme/app/issues/1",
					Version: "v1",
					Payload: []byte(`{"title":"Pulled"}`),
				}},
				NextCursor: []byte(`{"page":2}`),
			}, nil
		},
		push: func(_ context.Context, req core.PushRequest) (core.PushResult, error) {
			pushed = req
			return core.PushResult{Results: []core.WriteResult{{
				LocalID: localID,
				Key:     "ISS-2",
				URL:     "https://github.com/acme/app/issues/2",
				Version: "v2",
			}}}, nil
		},
	}))

	connectionID := uuid.New()
	runID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             providerName,
		Name:                 "Bidirectional",
		AuthType:             "token",
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionBidirectional,
		Enabled:            true,
	}
	repository.cursor = []byte(`{"page":1}`)
	repository.preparedPushRecords = []repo.PushRecord{{
		LocalObjectID: localID,
		LocalVersion:  "local-v1",
		Payload:       []byte(`{"title":"Push me"}`),
	}}
	service := New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("process-token")}))

	result, err := service.ProcessRun(context.Background(), repo.SyncRun{
		ID:           runID,
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		MappingID:    ptrext.Of(mappingID),
		Direction:    repo.DirectionBidirectional,
	})
	if err != nil {
		t.Fatalf("ProcessRun returned error: %v", err)
	}
	if !pullCalled || pushed.MappingID != mappingID.String() || len(pushed.Records) != 1 {
		t.Fatalf("pullCalled=%t pushed=%#v; want both directions processed", pullCalled, pushed)
	}
	if result.Status != repo.RunStatusSucceeded || len(result.OperationStats) != 2 {
		t.Fatalf("process result = %#v; want succeeded pull+push stats", result)
	}
	if len(repository.applyPullInputs) != 1 || len(repository.applyPushInputs) != 1 {
		t.Fatalf("apply inputs pull=%#v push=%#v; want both apply paths", repository.applyPullInputs, repository.applyPushInputs)
	}
	if string(repository.applyPullInputs[0].CursorBefore) != `{"page":1}` ||
		string(repository.applyPullInputs[0].CursorAfter) != `{"page":2}` {
		t.Fatalf("pull cursors = %s/%s; want before and next cursor", repository.applyPullInputs[0].CursorBefore, repository.applyPullInputs[0].CursorAfter)
	}
}

func TestWorkerProcessOnceMarksDeadProviderUnavailable(t *testing.T) {
	core.ResetForTest()
	t.Cleanup(restoreCoreNoopProvider)

	connectionID := uuid.New()
	runID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             "missing_provider",
		Name:                 "Missing",
		AuthType:             "token",
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	repository.claimedRuns = []repo.SyncRun{{
		ID:           runID,
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		Direction:    repo.DirectionPull,
		Attempts:     5,
	}}
	worker := NewWorker(New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("worker-token")})))
	worker.Configure(time.Hour, 10, 5)

	worker.ProcessOnce(context.Background())

	if len(repository.succeededRuns) != 0 {
		t.Fatalf("succeeded runs = %#v; want none", repository.succeededRuns)
	}
	if len(repository.failedMarks) != 1 {
		t.Fatalf("failed marks = %#v; want one dead failure", repository.failedMarks)
	}
	got := repository.failedMarks[0]
	if got.id != runID || !got.dead || got.kind != "provider_unavailable" {
		t.Fatalf("failed mark = %#v; want dead provider_unavailable for run %s", got, runID)
	}
	if got.delay != time.Hour {
		t.Fatalf("retry delay = %s; want 1h", got.delay)
	}
	if len(repository.quarantines) != 1 {
		t.Fatalf("quarantines = %#v; want one connection quarantine check", repository.quarantines)
	}
	quarantine := repository.quarantines[0]
	if quarantine.tenantID != "tenant-1" || quarantine.connectionID != connectionID {
		t.Fatalf("quarantine = %#v; want tenant-1 connection %s", quarantine, connectionID)
	}
	if !strings.Contains(quarantine.reason, "provider_unavailable") {
		t.Fatalf("quarantine reason = %q; want provider_unavailable context", quarantine.reason)
	}
}

func TestRetryAfterDelayUsesBoundedProviderHint(t *testing.T) {
	now := time.Date(2026, 7, 8, 3, 0, 0, 0, time.UTC)
	future := now.Add(5 * time.Minute)
	short := now.Add(10 * time.Second)
	past := now.Add(-time.Minute)
	huge := now.Add(48 * time.Hour)

	tests := []struct {
		name       string
		retryAfter *time.Time
		fallback   time.Duration
		want       time.Duration
	}{
		{name: "nil uses fallback", fallback: 30 * time.Second, want: 30 * time.Second},
		{name: "future uses provider delay", retryAfter: ptrext.Of(future), fallback: 30 * time.Second, want: 5 * time.Minute},
		{name: "shorter provider delay keeps fallback", retryAfter: ptrext.Of(short), fallback: 30 * time.Second, want: 30 * time.Second},
		{name: "past provider delay keeps fallback", retryAfter: ptrext.Of(past), fallback: 30 * time.Second, want: 30 * time.Second},
		{name: "huge provider delay is capped", retryAfter: ptrext.Of(huge), fallback: time.Hour, want: maxProviderRetryAfter},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryAfterDelay(now, tt.retryAfter, tt.fallback); got != tt.want {
				t.Fatalf("retryAfterDelay = %s; want %s", got, tt.want)
			}
		})
	}
}

func TestWorkerProcessOnceMarksDeadDirectionMismatch(t *testing.T) {
	const providerName = "worker_direction_mismatch"
	providerCalls := 0
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		pull: func(context.Context, core.PullRequest) (core.PullResult, error) {
			providerCalls++
			return core.PullResult{}, nil
		},
		push: func(context.Context, core.PushRequest) (core.PushResult, error) {
			providerCalls++
			return core.PushResult{}, nil
		},
	}))

	connectionID := uuid.New()
	mappingID := uuid.New()
	runID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             providerName,
		Name:                 "Worker",
		AuthType:             "token",
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPull,
		Enabled:            true,
	}
	repository.claimedRuns = []repo.SyncRun{{
		ID:           runID,
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		MappingID:    ptrext.Of(mappingID),
		Direction:    repo.DirectionPush,
		Attempts:     1,
	}}
	worker := NewWorker(New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("worker-token")})))
	worker.Configure(time.Hour, 10, 5)

	worker.ProcessOnce(context.Background())

	if providerCalls != 0 {
		t.Fatalf("provider calls = %d; want none", providerCalls)
	}
	if len(repository.succeededRuns) != 0 {
		t.Fatalf("succeeded runs = %#v; want none", repository.succeededRuns)
	}
	if len(repository.applyPullInputs) != 0 || len(repository.applyPushInputs) != 0 {
		t.Fatalf("apply inputs = pull %#v push %#v; want none", repository.applyPullInputs, repository.applyPushInputs)
	}
	if len(repository.failedMarks) != 1 {
		t.Fatalf("failed marks = %#v; want one validation failure", repository.failedMarks)
	}
	got := repository.failedMarks[0]
	if got.id != runID || !got.dead || got.kind != "validation_error" || got.delay != 0 {
		t.Fatalf("failed mark = %#v; want terminal validation failure for run %s", got, runID)
	}
	if !strings.Contains(got.message, "not allowed by mapping direction") {
		t.Fatalf("failure message = %q; want direction mismatch explanation", got.message)
	}
}

func TestWorkerProcessOncePersistsProviderErrorKind(t *testing.T) {
	tests := []struct {
		name      string
		kind      string
		retryable bool
		wantDead  bool
		wantDelay time.Duration
	}{
		{
			name:      "retryable rate limit",
			kind:      "rate_limited",
			retryable: true,
			wantDelay: 30 * time.Second,
		},
		{
			name:     "non retryable auth failure",
			kind:     "auth_failed",
			wantDead: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			providerName := "worker_provider_error_" + tt.kind
			providerErr := errors.New("provider failed")
			retryAfter := time.Date(2026, 7, 8, 3, 4, 5, 0, time.UTC)
			registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
				name: providerName,
				pull: func(context.Context, core.PullRequest) (core.PullResult, error) {
					return core.PullResult{}, providerErr
				},
				classify: func(error) core.SyncError {
					return core.SyncError{
						Kind:              tt.kind,
						Message:           "classified provider failure",
						ProviderRequestID: "provider-request-1",
						RetryAfter:        ptrext.Of(retryAfter),
						Retryable:         tt.retryable,
					}
				},
			}))

			connectionID := uuid.New()
			mappingID := uuid.New()
			runID := uuid.New()
			repository := newFakeRepo()
			repository.connections[connectionID] = repo.Connection{
				ID:                   connectionID,
				TenantID:             "tenant-1",
				Provider:             providerName,
				Name:                 "Worker",
				AuthType:             "token",
				CredentialKeyID:      "kid-1",
				CredentialCiphertext: []byte("ciphertext"),
			}
			repository.mappings[mappingID] = repo.Mapping{
				ID:                 mappingID,
				TenantID:           "tenant-1",
				ConnectionID:       connectionID,
				LocalObjectType:    "customer_request",
				ExternalObjectType: "issue",
				Direction:          repo.DirectionPull,
				Enabled:            true,
			}
			repository.claimedRuns = []repo.SyncRun{{
				ID:           runID,
				TenantID:     "tenant-1",
				ConnectionID: connectionID,
				MappingID:    ptrext.Of(mappingID),
				Direction:    repo.DirectionPull,
				Attempts:     1,
			}}
			worker := NewWorker(New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("worker-token")})))
			worker.Configure(time.Hour, 10, 5)

			worker.ProcessOnce(context.Background())

			if len(repository.failedMarks) != 1 {
				t.Fatalf("failed marks = %#v; want one provider failure", repository.failedMarks)
			}
			got := repository.failedMarks[0]
			if got.id != runID || got.kind != tt.kind || got.dead != tt.wantDead || got.delay != tt.wantDelay {
				t.Fatalf("failed mark = %#v; want kind %q dead %t delay %s", got, tt.kind, tt.wantDead, tt.wantDelay)
			}
			if len(repository.attempts) != 1 || repository.attempts[0].ErrorKind != tt.kind {
				t.Fatalf("attempts = %#v; want provider error kind %q", repository.attempts, tt.kind)
			}
			if repository.attempts[0].ProviderRequestID != "provider-request-1" ||
				repository.attempts[0].RetryAfter == nil ||
				!ptrext.Indirect(repository.attempts[0].RetryAfter).Equal(retryAfter) {
				t.Fatalf("attempt diagnostics = %#v; want provider request id and retry-after", repository.attempts[0])
			}
		})
	}
}

func TestWorkerProcessOnceHonorsProviderRetryAfter(t *testing.T) {
	const providerName = "worker_provider_retry_after"
	providerErr := errors.New("provider rate limited")
	retryAfter := time.Now().UTC().Add(5 * time.Minute)
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		pull: func(context.Context, core.PullRequest) (core.PullResult, error) {
			return core.PullResult{}, providerErr
		},
		classify: func(error) core.SyncError {
			return core.SyncError{
				Kind:       "rate_limited",
				Message:    "secondary rate limit",
				RetryAfter: ptrext.Of(retryAfter),
				Retryable:  true,
			}
		},
	}))

	connectionID := uuid.New()
	mappingID := uuid.New()
	runID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             providerName,
		Name:                 "Worker",
		AuthType:             "token",
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPull,
		Enabled:            true,
	}
	repository.claimedRuns = []repo.SyncRun{{
		ID:           runID,
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		MappingID:    ptrext.Of(mappingID),
		Direction:    repo.DirectionPull,
		Attempts:     1,
	}}
	worker := NewWorker(New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("worker-token")})))
	worker.Configure(time.Hour, 10, 5)

	worker.ProcessOnce(context.Background())

	if len(repository.failedMarks) != 1 {
		t.Fatalf("failed marks = %#v; want one provider failure", repository.failedMarks)
	}
	got := repository.failedMarks[0]
	if got.delay < 4*time.Minute || got.delay > 6*time.Minute {
		t.Fatalf("retry delay = %s; want provider retry-after around 5m", got.delay)
	}
}

func TestWorkerHeartbeatCancelsLostRunClaim(t *testing.T) {
	const providerName = "worker_lost_claim"
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		pull: func(ctx context.Context, _ core.PullRequest) (core.PullResult, error) {
			<-ctx.Done()
			return core.PullResult{}, ctx.Err()
		},
		classify: func(error) core.SyncError {
			return core.SyncError{Kind: "provider_unavailable", Message: "run claim lost", Retryable: true}
		},
	}))

	connectionID := uuid.New()
	mappingID := uuid.New()
	runID := uuid.New()
	repository := newFakeRepo()
	repository.refreshRunClaimRows = 0
	repository.connections[connectionID] = repo.Connection{
		ID:                   connectionID,
		TenantID:             "tenant-1",
		Provider:             providerName,
		Name:                 "Worker",
		AuthType:             "token",
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
	}
	repository.mappings[mappingID] = repo.Mapping{
		ID:                 mappingID,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          repo.DirectionPull,
		Enabled:            true,
	}
	repository.claimedRuns = []repo.SyncRun{{
		ID:           runID,
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		MappingID:    ptrext.Of(mappingID),
		Direction:    repo.DirectionPull,
		Attempts:     1,
	}}
	worker := NewWorker(New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("worker-token")})))
	worker.Configure(time.Hour, 10, 5)
	worker.heartbeat = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan struct{})

	go func() {
		worker.ProcessOnce(ctx)
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("ProcessOnce did not stop after lost run claim")
	}
	if len(repository.refreshedRunClaims) == 0 || repository.refreshedRunClaims[0] != runID {
		t.Fatalf("refreshed run claims = %#v; want heartbeat for run %s", repository.refreshedRunClaims, runID)
	}
	if len(repository.failedMarks) != 1 {
		t.Fatalf("failed marks = %#v; want one failure after lost claim cancellation", repository.failedMarks)
	}
	got := repository.failedMarks[0]
	if got.kind != "provider_unavailable" || got.dead {
		t.Fatalf("failed mark = %#v; want retryable provider_unavailable after lost claim", got)
	}
}

func TestConnectionProbeAndSchemaFailureBranches(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", "missing_provider")
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	result, err := service.TestConnection(context.Background(), "tenant-1", connectionID, auditlogsvc.Actor{})
	if !errors.Is(err, core.ErrProviderUnavailable) || result.OK {
		t.Fatalf("missing provider TestConnection = %+v err=%v; want unavailable failure", result, err)
	}
	if repository.connections[connectionID].LastTestStatus != repo.TestStatusFailed {
		t.Fatalf("last test status = %q; want failed", repository.connections[connectionID].LastTestStatus)
	}
	if _, err := service.DiscoverConnectionSchema(context.Background(), "tenant-1", connectionID); !errors.Is(err, core.ErrProviderUnavailable) {
		t.Fatalf("DiscoverConnectionSchema missing provider error = %v; want unavailable", err)
	}
}

func TestConnectionProbeDecryptAndProviderErrors(t *testing.T) {
	const providerName = "probe_errors"
	probeErr := errors.New("GET https://api.example.test/hooks/secret?token=abc failed")
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		check: func(context.Context, core.Connection) (core.CheckResult, error) {
			return core.CheckResult{}, probeErr
		},
		classify: func(error) core.SyncError {
			return core.SyncError{Kind: "provider_unavailable", Message: probeErr.Error(), Retryable: true}
		},
		discover: func(context.Context, core.Connection) ([]core.ObjectSchema, error) {
			return nil, errors.New("schema failed at https://api.example.test/schema?token=abc")
		},
	}))
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", providerName)
	service := New(repository, ptrext.Of(fakeSecretStore{decryptErr: errors.New("decrypt failed")}))
	if _, err := service.TestConnection(context.Background(), "tenant-1", connectionID, auditlogsvc.Actor{}); err == nil {
		t.Fatal("TestConnection with decrypt failure returned nil error")
	}

	service = New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("token")}))
	result, err := service.TestConnection(context.Background(), "tenant-1", connectionID, auditlogsvc.Actor{})
	if err == nil || result.OK || strings.Contains(result.Error, "token=abc") {
		t.Fatalf("provider error result = %+v err=%v; want redacted probe error", result, err)
	}
	if _, err := service.DiscoverConnectionSchema(context.Background(), "tenant-1", connectionID); !errors.Is(err, ErrValidation) {
		t.Fatalf("DiscoverConnectionSchema provider error = %v; want validation", err)
	}
}

func TestQualifyConnectionFailureAndWarningBranches(t *testing.T) {
	missingProviderID := uuid.New()
	repository := newFakeRepo()
	repository.connections[missingProviderID] = serviceTestConnection(missingProviderID, "tenant-1", "missing_provider")
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	result, err := service.QualifyConnection(context.Background(), " tenant-1 ", missingProviderID, auditlogsvc.Actor{})
	if err != nil || result.Ready || checkStatus(result, "provider_registered") != QualificationStatusFailed {
		t.Fatalf("missing provider qualification = %+v err=%v", result, err)
	}

	decryptID := uuid.New()
	const decryptProvider = "qualify_decrypt"
	registerCoreProvider(t, decryptProvider, ptrext.Of(fakeProvider{name: decryptProvider}))
	repository.connections[decryptID] = serviceTestConnection(decryptID, "tenant-1", decryptProvider)
	service = New(repository, ptrext.Of(fakeSecretStore{decryptErr: errors.New("decrypt failed")}))
	result, err = service.QualifyConnection(context.Background(), "tenant-1", decryptID, auditlogsvc.Actor{})
	if err != nil || result.Ready || checkStatus(result, "credential_decrypt") != QualificationStatusFailed {
		t.Fatalf("decrypt qualification = %+v err=%v", result, err)
	}
}

func TestQualifyConnectionProviderCheckAndSchemaWarnings(t *testing.T) {
	const providerName = "qualify_branches"
	providerErr := errors.New("provider check failed")
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		check: func(context.Context, core.Connection) (core.CheckResult, error) {
			return core.CheckResult{OK: false}, providerErr
		},
		discover: func(context.Context, core.Connection) ([]core.ObjectSchema, error) {
			return []core.ObjectSchema{
				{Type: "issue"},
				{Type: "task", Fields: []string{"title"}},
			}, nil
		},
		classify: func(error) core.SyncError {
			return core.SyncError{Kind: "provider_unavailable", Message: "classified check failure"}
		},
	}))
	connectionID := uuid.New()
	repository := newFakeRepo()
	conn := serviceTestConnection(connectionID, "tenant-1", providerName)
	conn.Scopes = nil
	repository.connections[connectionID] = conn
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	result, err := service.QualifyConnection(context.Background(), "tenant-1", connectionID, auditlogsvc.Actor{})
	if err != nil || result.Ready {
		t.Fatalf("qualification = %+v err=%v; want not ready", result, err)
	}
	if checkStatus(result, "provider_check") != QualificationStatusFailed ||
		checkStatus(result, "schema_metadata") != QualificationStatusFailed ||
		checkStatus(result, "scope_visibility") != QualificationStatusWarning {
		t.Fatalf("qualification checks = %#v", result.Checks)
	}
}

func TestCreateAndUpdateValidationBranches(t *testing.T) {
	service := New(newFakeRepo(), ptrext.Of(fakeSecretStore{}))
	_, err := service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID: "tenant-1", Provider: "github", Name: "GitHub", AuthType: "token", Credential: "token",
		WebhookSecret: "short", ProviderConfigJSON: `{}`, Actor: Actor{ID: "admin-1"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("short webhook secret error = %v; want validation", err)
	}
	_, err = service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID: "tenant-1", Provider: "github", Name: "GitHub", AuthType: "token", Credential: "token",
		ProviderConfigJSON: `[1]`, Actor: Actor{ID: "admin-1"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("array provider config error = %v; want validation", err)
	}
	_, err = service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID: "tenant-1", Provider: "github", Name: "GitHub", AuthType: "token", Credential: "token",
		ProviderConfigJSON: `{}`, Actor: Actor{},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("missing actor error = %v; want validation", err)
	}
}

func TestUpdateConnectionRejectsInvalidSecretAndShape(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", "github")
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	blankCredential := " "
	_, err := service.UpdateConnection(context.Background(), UpdateConnectionInput{
		TenantID: "tenant-1", ID: connectionID, Credential: ptrext.Of(blankCredential), Actor: Actor{ID: "admin-1"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("blank credential error = %v; want validation", err)
	}
	badName := strings.Repeat("x", 201)
	_, err = service.UpdateConnection(context.Background(), UpdateConnectionInput{
		TenantID: "tenant-1", ID: connectionID, Name: ptrext.Of(badName), Actor: Actor{ID: "admin-1"},
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("bad name error = %v; want validation", err)
	}
}

func TestMappingPreviewAndNormalizationBranches(t *testing.T) {
	if _, err := normalizeMapping(UpdateMappingInput{Direction: "sideways", FieldMappingJSON: `{}`, StatusMappingJSON: `{}`}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeMapping invalid direction error = %v; want validation", err)
	}
	if _, err := normalizeMapping(UpdateMappingInput{Direction: repo.DirectionPull, FieldMappingJSON: `{`, StatusMappingJSON: `{}`}); !errors.Is(err, ErrValidation) {
		t.Fatalf("normalizeMapping bad field JSON error = %v; want validation", err)
	}
	errorsOut, warnings := validateMappingPreview(`{"unknown":"x"}`, `{`, core.ObjectSchema{
		Type:           "issue",
		Fields:         []string{"title"},
		RequiredFields: []string{"title"},
		WritableFields: []string{"title"},
	})
	if len(errorsOut) == 0 || len(warnings) != 0 {
		t.Fatalf("preview errors=%#v warnings=%#v; want parse error only", errorsOut, warnings)
	}
	if schema := schemaForObject(nil, "issue"); schema.Type != "issue" {
		t.Fatalf("schema fallback = %#v; want issue type", schema)
	}
	if mappingAllowsRunDirection("bad", repo.DirectionPull) {
		t.Fatal("invalid mapping direction allowed pull run")
	}
}

func TestWebhookEventValidationBranches(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", "github")
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	webhookInputs := []GitHubWebhookInput{
		{ConnectionID: connectionID, EventType: "issues", DeliveryID: "d-1"},
		{TenantID: "tenant-1", EventType: "issues", DeliveryID: "d-1"},
		{TenantID: "tenant-1", ConnectionID: connectionID, DeliveryID: "d-1"},
		{TenantID: "tenant-1", ConnectionID: connectionID, EventType: "issues"},
	}
	for _, input := range webhookInputs {
		if _, err := service.RecordGitHubWebhook(context.Background(), input); !errors.Is(err, ErrValidation) {
			t.Fatalf("RecordGitHubWebhook(%+v) error = %v; want validation", input, err)
		}
	}

	wrongProviderID := uuid.New()
	repository.connections[wrongProviderID] = serviceTestConnection(wrongProviderID, "tenant-1", "zendesk")
	_, err := service.RecordGitHubWebhook(context.Background(), GitHubWebhookInput{
		TenantID: "tenant-1", ConnectionID: wrongProviderID, EventType: "issues", DeliveryID: "d-1",
	})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("wrong provider webhook error = %v; want validation", err)
	}
}

func TestWebhookSignatureAndPayloadHelpers(t *testing.T) {
	body := []byte(`{"zen":"hi"}`)
	if verifyGitHubSignatureSHA256("", []byte("secret"), body) ||
		verifyGitHubSignatureSHA256("sha256=bad", []byte("secret"), body) ||
		verifyGitHubSignatureSHA256(githubSignature(nil, body), nil, body) {
		t.Fatal("invalid GitHub signatures verified")
	}
	payload := normalizeGitHubWebhookPayload("ping", "delivery-1", []byte(`{`))
	if !strings.Contains(payload, "invalid_json") {
		t.Fatalf("invalid JSON payload = %s; want parse marker", payload)
	}
	payload = normalizeGitHubWebhookPayload("issues", "delivery-2", []byte(`{"repository":{"full_name":"acme/app","secret":"drop"},"issue":{"number":1,"user":{"login":"octo","secret":"drop"}},"sender":{"login":"octo"}}`))
	if !strings.Contains(payload, `"full_name":"acme/app"`) || strings.Contains(payload, "secret") {
		t.Fatalf("normalized webhook payload = %s; want selected public fields", payload)
	}
}

func TestRecordEventValidationBranches(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", "github")
	repository.mappings[mappingID] = serviceTestMapping(mappingID, connectionID, repo.DirectionPull, true)
	service := New(repository, ptrext.Of(fakeSecretStore{}))

	cases := []RecordEventInput{
		{TenantID: "tenant-1", ConnectionID: connectionID, EventType: "", NormalizedPayloadJSON: `{}`},
		{TenantID: "tenant-1", ConnectionID: connectionID, EventType: "issues", NormalizedPayloadJSON: `{}`, PayloadDigest: "bad"},
		{TenantID: "tenant-1", ConnectionID: connectionID, EventType: "issues", NormalizedPayloadJSON: `{}`, SignatureStatus: "unknown"},
		{TenantID: "tenant-1", ConnectionID: connectionID, EventType: "issues", NormalizedPayloadJSON: `{`, SignatureStatus: repo.EventSignatureVerified},
	}
	for _, input := range cases {
		if _, err := service.RecordEvent(context.Background(), input); !errors.Is(err, ErrValidation) {
			t.Fatalf("RecordEvent(%+v) error = %v; want validation", input, err)
		}
	}
	event, err := service.RecordEvent(context.Background(), RecordEventInput{
		TenantID: "tenant-1", ConnectionID: connectionID, MappingID: ptrext.Of(mappingID),
		EventType: "issues", DedupeKey: strings.Repeat("x", 600), SignatureStatus: repo.EventSignatureNotRequired,
		NormalizedPayloadJSON: `{}`,
	})
	if err != nil {
		t.Fatalf("RecordEvent returned error: %v", err)
	}
	if event.SignatureStatus != repo.EventSignatureNotRequired || len(event.DedupeKey) != 512 {
		t.Fatalf("event = %#v; want not_required signature and truncated dedupe key", event)
	}
}

func TestReplayAndBatchValidationBranches(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	eventID := uuid.New()
	repository := newFakeRepo()
	repository.mappings[mappingID] = serviceTestMapping(mappingID, connectionID, repo.DirectionPush, true)
	repository.events[eventID] = repo.SyncEvent{
		ID: eventID, TenantID: "tenant-1", ConnectionID: connectionID,
		MappingID: ptrext.Of(mappingID), SignatureStatus: repo.EventSignatureVerified, Status: repo.EventStatusReceived,
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	_, _, err := service.ReplayEvent(context.Background(), "tenant-1", eventID, Actor{ID: "admin-1"}, auditlogsvc.Actor{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("ReplayEvent push-only mapping error = %v; want validation", err)
	}

	repository.events[eventID] = repo.SyncEvent{ID: eventID, TenantID: "tenant-1", ConnectionID: connectionID, SignatureStatus: repo.EventSignatureVerified, Status: repo.EventStatusReplayed}
	_, _, err = service.ReplayEvent(context.Background(), "tenant-1", eventID, Actor{ID: "admin-1"}, auditlogsvc.Actor{})
	if !errors.Is(err, repo.ErrConflict) {
		t.Fatalf("ReplayEvent replayed status error = %v; want conflict", err)
	}

	assertBatchResolveValidation(t, service, BatchResolveConflictsInput{TenantID: "tenant-1", IDs: []uuid.UUID{uuid.New()}, Resolution: "local_wins"})
	assertBatchResolveValidation(t, service, BatchResolveConflictsInput{TenantID: "tenant-1", IDs: []uuid.UUID{uuid.New()}, Resolution: "bad", Actor: Actor{ID: "admin-1"}})
	assertBatchResolveValidation(t, service, BatchResolveConflictsInput{TenantID: "tenant-1", Resolution: "local_wins", Actor: Actor{ID: "admin-1"}})
	many := make([]uuid.UUID, 51)
	for i := range many {
		many[i] = uuid.New()
	}
	assertBatchResolveValidation(t, service, BatchResolveConflictsInput{TenantID: "tenant-1", IDs: many, Resolution: "local_wins", Actor: Actor{ID: "admin-1"}})
}

func TestProcessApplyFailureBranches(t *testing.T) {
	pullErr := errors.New("apply pull failed at https://api.example.test/pull?token=abc")
	repository, service, run := processFailureFixture(t, "process_pull_apply")
	repository.applyPullErr = pullErr
	result, err := service.ProcessRun(context.Background(), run)
	if err == nil || result.Status != repo.RunStatusSucceeded ||
		len(repository.attempts) != 1 || repository.attempts[0].ErrorKind != "apply_failed" ||
		strings.Contains(repository.attempts[0].ErrorMessage, "token=abc") {
		t.Fatalf("pull apply failure result=%+v err=%v attempts=%#v", result, err, repository.attempts)
	}

	pushErr := errors.New("apply push failed")
	repository, service, run = processFailureFixture(t, "process_push_apply")
	run.Direction = repo.DirectionPush
	repository.mappings[ptrext.Indirect(run.MappingID)] = serviceTestMapping(ptrext.Indirect(run.MappingID), run.ConnectionID, repo.DirectionPush, true)
	repository.preparedPushRecords = []repo.PushRecord{{LocalObjectID: uuid.NewString(), Payload: []byte(`{"title":"Bug"}`)}}
	repository.applyPushErr = pushErr
	if _, err = service.ProcessRun(context.Background(), run); err == nil ||
		len(repository.attempts) != 1 || repository.attempts[0].ErrorKind != "apply_failed" {
		t.Fatalf("push apply failure err=%v attempts=%#v", err, repository.attempts)
	}
}

func TestProcessPushPrepareAndResultMappingBranches(t *testing.T) {
	repository, service, run := processFailureFixture(t, "process_push_prepare")
	run.Direction = repo.DirectionPush
	repository.mappings[ptrext.Indirect(run.MappingID)] = serviceTestMapping(ptrext.Indirect(run.MappingID), run.ConnectionID, repo.DirectionPush, true)
	repository.preparePushErr = errors.New("prepare failed")
	if _, err := service.ProcessRun(context.Background(), run); err == nil {
		t.Fatal("ProcessRun push prepare failure returned nil error")
	}
	results := pushResultsToRepo([]core.WriteResult{{
		LocalID: "cr-1",
		Error:   ptrext.Of(core.SyncError{Message: "failed"}),
	}})
	if len(results) != 1 || results[0].ErrorKind != "provider_error" {
		t.Fatalf("pushResultsToRepo = %#v; want provider_error fallback", results)
	}
}

func TestWorkerRunAndUtilityBranches(t *testing.T) {
	repository := newFakeRepo()
	repository.claimErr = errors.New("claim failed")
	worker := NewWorker(New(repository, ptrext.Of(fakeSecretStore{})))
	worker.ProcessOnce(context.Background())

	repository.metricSnapshotErr = errors.New("snapshot failed")
	worker.recordRunMetrics(context.Background(), ProcessResult{}, 0)
	if nonNegative(-1) != 0 || nonNegative(2.5) != 2.5 {
		t.Fatal("nonNegative returned unexpected values")
	}
	if backoff(0) != 30*time.Second || backoff(2) != 2*time.Minute ||
		backoff(3) != 10*time.Minute || backoff(4) != time.Hour {
		t.Fatal("backoff returned unexpected schedule")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	worker.Run(ctx)
}

func TestAuditHelperNilBranches(t *testing.T) {
	if connectionAudit(nil) != nil || mappingAudit(nil) != nil || runAudit(nil) != nil ||
		cursorResetAudit(nil) != nil || backfillAudit(nil, false) != nil ||
		failureAudit(nil) != nil || conflictAudit(nil) != nil || eventAudit(nil) != nil {
		t.Fatal("nil audit helpers should return nil")
	}
	if eventReplayAudit(nil, nil) == nil {
		t.Fatal("eventReplayAudit should return an empty map for nil event")
	}
	if truncateString("abcdef", 3) != "abc" || truncateString("abc", 3) != "abc" {
		t.Fatal("truncateString returned unexpected value")
	}
	redacted := redact("failed at https://api.example.test/path?token=abc.")
	if strings.Contains(redacted, "token=abc") || !strings.HasSuffix(redacted, ".") {
		t.Fatalf("redact = %q; want secret removed with punctuation retained", redacted)
	}
}

func TestServiceRepositoryErrorPropagationBranches(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	baseConn := serviceTestConnection(connectionID, "tenant-1", "github")
	baseMapping := serviceTestMapping(mappingID, connectionID, repo.DirectionPull, true)
	repoErr := errors.New("repo failed")

	t.Run("connection mutations", func(t *testing.T) {
		repository := newFakeRepo()
		repository.connections[connectionID] = baseConn
		repository.createConnectionErr = repoErr
		service := New(repository, ptrext.Of(fakeSecretStore{}))
		_, err := service.CreateConnection(context.Background(), CreateConnectionInput{
			TenantID: "tenant-1", Provider: "github", Name: "GitHub", AuthType: "token",
			Credential: "token", ProviderConfigJSON: `{}`, Actor: Actor{ID: "admin-1"},
		})
		if !errors.Is(err, repoErr) {
			t.Fatalf("CreateConnection error = %v; want repoErr", err)
		}
		repository.updateConnectionErr = repoErr
		_, err = service.UpdateConnection(context.Background(), UpdateConnectionInput{
			TenantID: "tenant-1", ID: connectionID, Name: ptrext.Of("GitHub"), Actor: Actor{ID: "admin-1"},
		})
		if !errors.Is(err, repoErr) {
			t.Fatalf("UpdateConnection error = %v; want repoErr", err)
		}
		repository.deleteConnectionErr = repoErr
		if err = service.DeleteConnection(context.Background(), "tenant-1", connectionID, Actor{ID: "admin-1"}, auditlogsvc.Actor{}); !errors.Is(err, repoErr) {
			t.Fatalf("DeleteConnection error = %v; want repoErr", err)
		}
	})

	t.Run("mapping and run mutations", func(t *testing.T) {
		repository := newFakeRepo()
		repository.connections[connectionID] = baseConn
		repository.mappings[mappingID] = baseMapping
		service := New(repository, ptrext.Of(fakeSecretStore{}))
		repository.resetCursorErr = repoErr
		_, err := service.ResetCursor(context.Background(), ResetCursorInput{TenantID: "tenant-1", ID: mappingID, Actor: Actor{ID: "admin-1"}})
		if !errors.Is(err, repoErr) {
			t.Fatalf("ResetCursor error = %v; want repoErr", err)
		}
		repository.enqueueBackfillErr = repoErr
		_, err = service.RequestBackfill(context.Background(), BackfillInput{TenantID: "tenant-1", ID: mappingID, Actor: Actor{ID: "admin-1"}})
		if !errors.Is(err, repoErr) {
			t.Fatalf("RequestBackfill error = %v; want repoErr", err)
		}
		repository.insertRunErr = repoErr
		_, err = service.RequestRun(context.Background(), RequestRunInput{TenantID: "tenant-1", ConnectionID: connectionID, MappingID: ptrext.Of(mappingID), Actor: Actor{ID: "admin-1"}})
		if !errors.Is(err, repoErr) {
			t.Fatalf("RequestRun error = %v; want repoErr", err)
		}
	})
}

func TestServiceEventAndRetryErrorPropagationBranches(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	eventID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", "github")
	repository.mappings[mappingID] = serviceTestMapping(mappingID, connectionID, repo.DirectionPull, true)
	repository.events[eventID] = repo.SyncEvent{
		ID: eventID, TenantID: "tenant-1", ConnectionID: connectionID,
		MappingID: ptrext.Of(mappingID), SignatureStatus: repo.EventSignatureVerified, Status: repo.EventStatusReceived,
	}
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	repoErr := errors.New("repo failed")

	repository.recordEventErr = repoErr
	if _, err := service.RecordEvent(context.Background(), RecordEventInput{
		TenantID: "tenant-1", ConnectionID: connectionID, EventType: "issues", NormalizedPayloadJSON: `{}`,
	}); !errors.Is(err, repoErr) {
		t.Fatalf("RecordEvent error = %v; want repoErr", err)
	}
	repository.recordEventErr = nil
	repository.replayEventErr = repoErr
	if _, _, err := service.ReplayEvent(context.Background(), "tenant-1", eventID, Actor{ID: "admin-1"}, auditlogsvc.Actor{}); !errors.Is(err, repoErr) {
		t.Fatalf("ReplayEvent error = %v; want repoErr", err)
	}
	repository.retryRunErr = repoErr
	if _, err := service.RetryRun(context.Background(), "tenant-1", uuid.New(), Actor{}, auditlogsvc.Actor{}); !errors.Is(err, repoErr) {
		t.Fatalf("RetryRun error = %v; want repoErr", err)
	}
	repository.retryFailureErr = repoErr
	if _, err := service.RetryFailure(context.Background(), "tenant-1", uuid.New(), Actor{}, auditlogsvc.Actor{}); !errors.Is(err, repoErr) {
		t.Fatalf("RetryFailure error = %v; want repoErr", err)
	}
	repository.resolveConflictErr = repoErr
	if _, err := service.ResolveConflict(context.Background(), "tenant-1", uuid.New(), "local_wins", Actor{}, auditlogsvc.Actor{}); !errors.Is(err, repoErr) {
		t.Fatalf("ResolveConflict error = %v; want repoErr", err)
	}
	repository.resolveConflictsErr = repoErr
	if _, err := service.BatchResolveConflicts(context.Background(), BatchResolveConflictsInput{
		TenantID: "tenant-1", IDs: []uuid.UUID{uuid.New()}, Resolution: "local_wins", Actor: Actor{ID: "admin-1"},
	}); !errors.Is(err, repoErr) {
		t.Fatalf("BatchResolveConflicts error = %v; want repoErr", err)
	}
}

func TestServiceValidationAndHelperRemainingBranches(t *testing.T) {
	if err := validateConnectionShape("GitHub", "GitHub", "token", `{}`); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid provider shape error = %v; want validation", err)
	}
	if err := validateConnectionShape("github", "GitHub", "session", `{}`); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid auth type error = %v; want validation", err)
	}
	if err := validateConnectionShape("github", "GitHub", "token", `{`); !errors.Is(err, ErrValidation) {
		t.Fatalf("invalid config JSON error = %v; want validation", err)
	}
	if got, err := normalizeJSONObject("", "field"); err != nil || got != "{}" {
		t.Fatalf("blank normalizeJSONObject = %q err=%v; want empty object", got, err)
	}
	if _, err := normalizeJSONObject(strings.Repeat("x", 32*1024+1), "field"); !errors.Is(err, ErrValidation) {
		t.Fatalf("large normalizeJSONObject error = %v; want validation", err)
	}
	if _, err := normalizeMapping(UpdateMappingInput{Direction: repo.DirectionPull, FieldMappingJSON: `{}`, StatusMappingJSON: `{`}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad status mapping error = %v; want validation", err)
	}
	if normalizeRunStatus("") != "" || normalizeEventStatus("") != "" {
		t.Fatal("blank status normalizers should return empty filters")
	}
	if got := normalizeEventDedupeKey("", "github", "issues", "", strings.Repeat("a", 64)); !strings.Contains(got, strings.Repeat("a", 64)) {
		t.Fatalf("digest dedupe key = %q; want digest fallback", got)
	}
}

func TestServiceRemainingWebhookAndProcessBranches(t *testing.T) {
	connectionID := uuid.New()
	repository := newFakeRepo()
	conn := serviceTestConnection(connectionID, "tenant-1", "github")
	conn.WebhookSecretKeyID = "kid-1"
	conn.WebhookSecretCiphertext = []byte("ciphertext")
	repository.connections[connectionID] = conn
	service := New(repository, ptrext.Of(fakeSecretStore{decryptErr: errors.New("decrypt failed")}))
	_, err := service.RecordGitHubWebhook(context.Background(), GitHubWebhookInput{
		TenantID: "tenant-1", ConnectionID: connectionID, EventType: "issues", DeliveryID: "d-1",
	})
	if err == nil {
		t.Fatal("RecordGitHubWebhook decrypt error returned nil")
	}

	runErr := processRunError{kind: "validation"}
	if runErr.Error() != "validation" {
		t.Fatalf("processRunError without message = %q; want kind", runErr.Error())
	}
	records := pullRecordsToRepo([]core.ExternalRecord{{Key: "ISS-1", UpdatedAt: time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC)}})
	if len(records) != 1 || records[0].ExternalUpdatedAt == nil {
		t.Fatalf("pullRecordsToRepo = %#v; want updated timestamp pointer", records)
	}
}

func TestProcessRunRemainingErrorBranches(t *testing.T) {
	repository, service, run := processFailureFixture(t, "process_remaining")
	repository.prepareRunCursorErr = errors.New("cursor failed")
	if _, err := service.ProcessRun(context.Background(), run); err == nil {
		t.Fatal("ProcessRun cursor preparation failure returned nil error")
	}

	repository, service, run = processFailureFixture(t, "process_stream_key")
	providerName := repository.connections[run.ConnectionID].Provider
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		pull: func(context.Context, core.PullRequest) (core.PullResult, error) {
			return core.PullResult{StreamKey: "custom", Records: []core.ExternalRecord{{Key: "ISS-1"}}}, nil
		},
	}))
	if _, err := service.ProcessRun(context.Background(), run); err != nil {
		t.Fatalf("ProcessRun with custom stream key returned error: %v", err)
	}
	if repository.applyPullInputs[0].StreamKey != "custom" {
		t.Fatalf("stream key = %q; want custom", repository.applyPullInputs[0].StreamKey)
	}

	repository, service, run = processFailureFixture(t, "process_missing")
	delete(repository.connections, run.ConnectionID)
	if _, err := service.ProcessRun(context.Background(), run); !errors.Is(err, repo.ErrConnectionNotFound) {
		t.Fatalf("ProcessRun missing connection error = %v; want not found", err)
	}
}

func TestWorkerRemainingFailureBranches(t *testing.T) {
	repository, service, run := processFailureFixture(t, "worker_mark_branches")
	repository.claimedRuns = []repo.SyncRun{run}
	repository.markSucceededRows = -1
	worker := NewWorker(service)
	worker.Configure(time.Hour, 10, 5)
	worker.ProcessOnce(context.Background())
	if len(repository.succeededRuns) != 1 {
		t.Fatalf("succeeded runs = %#v; want attempted success mark", repository.succeededRuns)
	}

	repository, service, run = processFailureFixture(t, "worker_failure_mark")
	repository.claimedRuns = []repo.SyncRun{run}
	repository.markFailedErr = errors.New("mark failed")
	registerCoreProvider(t, repository.connections[run.ConnectionID].Provider, ptrext.Of(fakeProvider{
		name: repository.connections[run.ConnectionID].Provider,
		pull: func(context.Context, core.PullRequest) (core.PullResult, error) {
			return core.PullResult{}, errors.New("provider failed")
		},
	}))
	worker = NewWorker(service)
	worker.Configure(time.Hour, 10, 5)
	worker.ProcessOnce(context.Background())

	repository, service, run = processFailureFixture(t, "worker_quarantine_error")
	repository.claimedRuns = []repo.SyncRun{run}
	repository.quarantineErr = errors.New("quarantine failed")
	registerCoreProvider(t, repository.connections[run.ConnectionID].Provider, ptrext.Of(fakeProvider{
		name: repository.connections[run.ConnectionID].Provider,
		pull: func(context.Context, core.PullRequest) (core.PullResult, error) {
			return core.PullResult{}, errors.New("provider failed")
		},
	}))
	worker = NewWorker(service)
	worker.Configure(time.Hour, 10, 5)
	worker.ProcessOnce(context.Background())
}

func TestServiceStoreErrorBranches(t *testing.T) {
	encryptErr := errors.New("encrypt failed")
	service := New(newFakeRepo(), ptrext.Of(fakeSecretStore{encryptErr: encryptErr}))
	_, err := service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID: "tenant-1", Provider: "github", Name: "GitHub", AuthType: "token",
		Credential: "token", ProviderConfigJSON: `{}`, Actor: Actor{ID: "admin-1"},
	})
	if !errors.Is(err, encryptErr) {
		t.Fatalf("credential encrypt error = %v; want encryptErr", err)
	}
	_, err = service.CreateConnection(context.Background(), CreateConnectionInput{
		TenantID: "tenant-1", Provider: "github", Name: "GitHub", AuthType: "token",
		Credential: "token", WebhookSecret: "webhook-secret-123", ProviderConfigJSON: `{}`, Actor: Actor{ID: "admin-1"},
	})
	if !errors.Is(err, encryptErr) {
		t.Fatalf("webhook encrypt error = %v; want encryptErr", err)
	}

	connectionID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", "github")
	service = New(repository, ptrext.Of(fakeSecretStore{encryptErr: encryptErr}))
	secret := "webhook-secret-123"
	_, err = service.UpdateConnection(context.Background(), UpdateConnectionInput{
		TenantID: "tenant-1", ID: connectionID, Credential: ptrext.Of("token"), Actor: Actor{ID: "admin-1"},
	})
	if !errors.Is(err, encryptErr) {
		t.Fatalf("update credential encrypt error = %v; want encryptErr", err)
	}
	_, err = service.UpdateConnection(context.Background(), UpdateConnectionInput{
		TenantID: "tenant-1", ID: connectionID, WebhookSecret: ptrext.Of(secret), Actor: Actor{ID: "admin-1"},
	})
	if !errors.Is(err, encryptErr) {
		t.Fatalf("update webhook encrypt error = %v; want encryptErr", err)
	}
}

func TestServiceMissingResourceBranches(t *testing.T) {
	service := New(newFakeRepo(), ptrext.Of(fakeSecretStore{}))
	connectionID := uuid.New()
	mappingID := uuid.New()
	if _, err := service.UpdateConnection(context.Background(), UpdateConnectionInput{TenantID: "tenant-1", ID: connectionID}); !errors.Is(err, repo.ErrConnectionNotFound) {
		t.Fatalf("UpdateConnection missing error = %v; want connection not found", err)
	}
	if err := service.DeleteConnection(context.Background(), "tenant-1", connectionID, Actor{}, auditlogsvc.Actor{}); !errors.Is(err, repo.ErrConnectionNotFound) {
		t.Fatalf("DeleteConnection missing error = %v; want connection not found", err)
	}
	if _, err := service.ResumeConnection(context.Background(), ResumeConnectionInput{TenantID: "tenant-1", ID: connectionID, Actor: Actor{ID: "admin-1"}}); !errors.Is(err, repo.ErrConnectionNotFound) {
		t.Fatalf("ResumeConnection missing error = %v; want connection not found", err)
	}
	if _, err := service.TestConnection(context.Background(), "tenant-1", connectionID, auditlogsvc.Actor{}); !errors.Is(err, repo.ErrConnectionNotFound) {
		t.Fatalf("TestConnection missing error = %v; want connection not found", err)
	}
	if _, err := service.QualifyConnection(context.Background(), "tenant-1", connectionID, auditlogsvc.Actor{}); !errors.Is(err, repo.ErrConnectionNotFound) {
		t.Fatalf("QualifyConnection missing error = %v; want connection not found", err)
	}
	if _, err := service.DiscoverConnectionSchema(context.Background(), "tenant-1", connectionID); !errors.Is(err, repo.ErrConnectionNotFound) {
		t.Fatalf("DiscoverConnectionSchema missing error = %v; want connection not found", err)
	}
	if _, err := service.ResetCursor(context.Background(), ResetCursorInput{TenantID: "tenant-1", ID: mappingID, Actor: Actor{ID: "admin-1"}}); !errors.Is(err, repo.ErrMappingNotFound) {
		t.Fatalf("ResetCursor missing mapping error = %v; want mapping not found", err)
	}
}

func TestServiceMappingGuardBranches(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", "github")
	repository.mappings[mappingID] = serviceTestMapping(mappingID, connectionID, repo.DirectionPull, false)
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	if _, err := service.ResetCursor(context.Background(), ResetCursorInput{TenantID: "tenant-1", ID: mappingID, Actor: Actor{}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("ResetCursor missing actor error = %v; want validation", err)
	}
	if _, err := service.ResetCursor(context.Background(), ResetCursorInput{TenantID: "tenant-1", ID: mappingID, Actor: Actor{ID: "admin-1"}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("ResetCursor disabled mapping error = %v; want validation", err)
	}
	if _, err := service.RequestBackfill(context.Background(), BackfillInput{TenantID: "tenant-1", ID: mappingID, Actor: Actor{}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("RequestBackfill missing actor error = %v; want validation", err)
	}
	if _, err := service.RequestBackfill(context.Background(), BackfillInput{TenantID: "tenant-1", ID: mappingID, Actor: Actor{ID: "admin-1"}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("RequestBackfill disabled mapping error = %v; want validation", err)
	}
	if _, err := service.RecordTimeline(context.Background(), RecordTimelineInput{TenantID: "tenant-1"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("RecordTimeline missing mapping error = %v; want validation", err)
	}
}

func TestServiceSchemaDiscoveryRemainingBranches(t *testing.T) {
	const emptyProvider = "schema_empty"
	registerCoreProvider(t, emptyProvider, ptrext.Of(fakeProvider{
		name: emptyProvider,
		discover: func(context.Context, core.Connection) ([]core.ObjectSchema, error) {
			return nil, nil
		},
	}))
	connectionID := uuid.New()
	repository := newFakeRepo()
	conn := serviceTestConnection(connectionID, "tenant-1", emptyProvider)
	conn.Scopes = nil
	repository.connections[connectionID] = conn
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	result, err := service.QualifyConnection(context.Background(), "tenant-1", connectionID, auditlogsvc.Actor{})
	if err != nil || checkStatus(result, "schema_discovery") != QualificationStatusWarning {
		t.Fatalf("empty schema qualification = %+v err=%v; want warning", result, err)
	}

	const failingProvider = "schema_discover_fail"
	registerCoreProvider(t, failingProvider, ptrext.Of(fakeProvider{
		name: failingProvider,
		discover: func(context.Context, core.Connection) ([]core.ObjectSchema, error) {
			return nil, errors.New("discover failed")
		},
		classify: func(error) core.SyncError {
			return core.SyncError{Kind: "provider_error", HTTPStatus: 503}
		},
	}))
	connectionID = uuid.New()
	repository = newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", failingProvider)
	service = New(repository, ptrext.Of(fakeSecretStore{}))
	result, err = service.QualifyConnection(context.Background(), "tenant-1", connectionID, auditlogsvc.Actor{})
	if err != nil || checkStatus(result, "schema_discovery") != QualificationStatusFailed {
		t.Fatalf("failing schema qualification = %+v err=%v; want failed discovery", result, err)
	}
}

func TestServiceMappingPreviewRemainingBranches(t *testing.T) {
	connectionID := uuid.New()
	mappingID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", "missing_provider")
	repository.mappings[mappingID] = serviceTestMapping(mappingID, connectionID, repo.DirectionPull, true)
	service := New(repository, ptrext.Of(fakeSecretStore{}))
	if _, err := service.PreviewMapping(context.Background(), PreviewMappingInput{TenantID: "tenant-1", ID: uuid.New()}); !errors.Is(err, repo.ErrMappingNotFound) {
		t.Fatalf("PreviewMapping missing error = %v; want mapping not found", err)
	}
	if _, err := service.PreviewMapping(context.Background(), PreviewMappingInput{
		TenantID: "tenant-1", ID: mappingID, StatusMappingJSON: ptrext.Of(`{"open":"new"}`),
	}); !errors.Is(err, core.ErrProviderUnavailable) {
		t.Fatalf("PreviewMapping schema error = %v; want provider unavailable", err)
	}
	repository.updateMappingErr = errors.New("update mapping failed")
	if _, err := service.UpdateMapping(context.Background(), UpdateMappingInput{
		TenantID: "tenant-1", ID: mappingID, Direction: repo.DirectionPull, FieldMappingJSON: `{}`, StatusMappingJSON: `{}`,
	}); err == nil {
		t.Fatal("UpdateMapping repo error returned nil")
	}
}

func TestServiceProcessRemainingBranches(t *testing.T) {
	_, service, run := processFailureFixture(t, "process_decrypt")
	service.store = ptrext.Of(fakeSecretStore{decryptErr: errors.New("decrypt failed")})
	if _, err := service.ProcessRun(context.Background(), run); err == nil {
		t.Fatal("ProcessRun decrypt failure returned nil")
	}
	repository, service, run := processFailureFixture(t, "process_mapping_missing")
	delete(repository.mappings, ptrext.Indirect(run.MappingID))
	if _, err := service.ProcessRun(context.Background(), run); !errors.Is(err, repo.ErrMappingNotFound) {
		t.Fatalf("ProcessRun mapping error = %v; want mapping not found", err)
	}
	repository, service, run = processFailureFixture(t, "process_push_provider_error")
	run.Direction = repo.DirectionPush
	repository.mappings[ptrext.Indirect(run.MappingID)] = serviceTestMapping(ptrext.Indirect(run.MappingID), run.ConnectionID, repo.DirectionPush, true)
	registerCoreProvider(t, repository.connections[run.ConnectionID].Provider, ptrext.Of(fakeProvider{
		name: repository.connections[run.ConnectionID].Provider,
		push: func(context.Context, core.PushRequest) (core.PushResult, error) {
			return core.PushResult{}, errors.New("provider failed")
		},
		classify: func(error) core.SyncError {
			return core.SyncError{}
		},
	}))
	if _, err := service.ProcessRun(context.Background(), run); err == nil ||
		repository.attempts[0].ErrorKind != "provider_error" ||
		repository.attempts[0].ErrorMessage != "provider failed" {
		t.Fatalf("ProcessRun push provider error=%v attempts=%#v", err, repository.attempts)
	}
}

func TestServiceRecordDefaultActorBranch(t *testing.T) {
	audit := ptrext.Of(fakeAuditRecorder{})
	service := New(newFakeRepo(), ptrext.Of(fakeSecretStore{}))
	service.SetAuditLogger(audit)
	service.record(context.Background(), auditlogsvc.Actor{ID: "admin-1"}, "tenant-1", "action", "target", "id", "summary", nil, nil)
	if len(audit.events) != 1 || audit.events[0].Actor.Type != "admin" {
		t.Fatalf("audit events = %#v; want default admin actor type", audit.events)
	}
}

func TestWorkerRunTickAndHeartbeatErrorBranches(t *testing.T) {
	repository := newFakeRepo()
	repository.claimErr = errors.New("claim failed")
	worker := NewWorker(New(repository, ptrext.Of(fakeSecretStore{})))
	worker.Configure(time.Millisecond, 1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		worker.Run(ctx)
		close(done)
	}()
	time.Sleep(3 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after cancellation")
	}

	repository = newFakeRepo()
	repository.refreshRunClaimRows = 1
	worker = NewWorker(New(repository, ptrext.Of(fakeSecretStore{})))
	worker.heartbeat = time.Millisecond
	runCtx, runCancel := context.WithCancel(context.Background())
	go worker.heartbeatRun(runCtx, uuid.New(), runCancel)
	time.Sleep(3 * time.Millisecond)
	runCancel()
}

func serviceTestConnection(id uuid.UUID, tenantID, provider string) repo.Connection {
	return repo.Connection{
		ID:                   id,
		TenantID:             tenantID,
		Provider:             provider,
		Name:                 "GitHub",
		Enabled:              true,
		Status:               repo.ConnectionStatusActive,
		AuthType:             "token",
		ProviderConfig:       []byte(`{}`),
		CredentialKeyID:      "kid-1",
		CredentialCiphertext: []byte("ciphertext"),
		Scopes:               []string{"issues"},
	}
}

func serviceTestMapping(id, connectionID uuid.UUID, direction string, enabled bool) repo.Mapping {
	return repo.Mapping{
		ID:                 id,
		TenantID:           "tenant-1",
		ConnectionID:       connectionID,
		LocalObjectType:    "customer_request",
		ExternalObjectType: "issue",
		Direction:          direction,
		Enabled:            enabled,
	}
}

func checkStatus(result QualificationResult, name string) string {
	for _, check := range result.Checks {
		if check.Name == name {
			return check.Status
		}
	}
	return ""
}

func assertBatchResolveValidation(t *testing.T, service *Service, in BatchResolveConflictsInput) {
	t.Helper()
	if _, err := service.BatchResolveConflicts(context.Background(), in); !errors.Is(err, ErrValidation) {
		t.Fatalf("BatchResolveConflicts(%+v) error = %v; want validation", in, err)
	}
}

func processFailureFixture(t *testing.T, providerName string) (*fakeRepo, *Service, repo.SyncRun) {
	t.Helper()
	registerCoreProvider(t, providerName, ptrext.Of(fakeProvider{
		name: providerName,
		pull: func(context.Context, core.PullRequest) (core.PullResult, error) {
			return core.PullResult{Records: []core.ExternalRecord{{Key: "ISS-1", Payload: []byte(`{"title":"Bug"}`)}}}, nil
		},
		push: func(context.Context, core.PushRequest) (core.PushResult, error) {
			return core.PushResult{Results: []core.WriteResult{{LocalID: "cr-1", Key: "ISS-1"}}}, nil
		},
	}))
	connectionID := uuid.New()
	mappingID := uuid.New()
	runID := uuid.New()
	repository := newFakeRepo()
	repository.connections[connectionID] = serviceTestConnection(connectionID, "tenant-1", providerName)
	repository.mappings[mappingID] = serviceTestMapping(mappingID, connectionID, repo.DirectionPull, true)
	run := repo.SyncRun{
		ID:           runID,
		TenantID:     "tenant-1",
		ConnectionID: connectionID,
		MappingID:    ptrext.Of(mappingID),
		Direction:    repo.DirectionPull,
		Attempts:     1,
		ClaimedBy:    "worker-1",
	}
	return repository, New(repository, ptrext.Of(fakeSecretStore{decryptPlaintext: []byte("token")})), run
}

type fakeSecretStore struct {
	encryptPlaintext  []byte
	encryptAAD        []byte
	encryptPlaintexts [][]byte
	encryptAADs       [][]byte
	decryptPlaintext  []byte
	decryptAAD        []byte
	encryptErr        error
	decryptErr        error
}

func (s *fakeSecretStore) EncryptValue(plaintext, aad []byte) (secretstore.EncryptedValue, error) {
	s.encryptPlaintext = append([]byte(nil), plaintext...)
	s.encryptAAD = append([]byte(nil), aad...)
	s.encryptPlaintexts = append(s.encryptPlaintexts, append([]byte(nil), plaintext...))
	s.encryptAADs = append(s.encryptAADs, append([]byte(nil), aad...))
	if s.encryptErr != nil {
		return secretstore.EncryptedValue{}, s.encryptErr
	}
	return secretstore.EncryptedValue{KeyID: "kid-1", Ciphertext: []byte("ciphertext")}, nil
}

func (s *fakeSecretStore) DecryptValue(_ secretstore.EncryptedValue, aad []byte) ([]byte, error) {
	s.decryptAAD = append([]byte(nil), aad...)
	if s.decryptErr != nil {
		return nil, s.decryptErr
	}
	if s.decryptPlaintext == nil {
		return []byte("plaintext"), nil
	}
	return append([]byte(nil), s.decryptPlaintext...), nil
}

func githubSignature(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type fakeAuditRecorder struct {
	events []auditlogsvc.Event
}

func (r *fakeAuditRecorder) Record(_ context.Context, event auditlogsvc.Event) error {
	r.events = append(r.events, event)
	return nil
}

type fakeProvider struct {
	name     string
	check    func(context.Context, core.Connection) (core.CheckResult, error)
	discover func(context.Context, core.Connection) ([]core.ObjectSchema, error)
	pull     func(context.Context, core.PullRequest) (core.PullResult, error)
	push     func(context.Context, core.PushRequest) (core.PushResult, error)
	classify func(error) core.SyncError
}

func (p *fakeProvider) Provider() string { return p.name }

func (p *fakeProvider) Check(ctx context.Context, conn core.Connection) (core.CheckResult, error) {
	if p.check != nil {
		return p.check(ctx, conn)
	}
	return core.CheckResult{OK: true}, nil
}

func (p *fakeProvider) Discover(ctx context.Context, conn core.Connection) ([]core.ObjectSchema, error) {
	if p.discover != nil {
		return p.discover(ctx, conn)
	}
	return []core.ObjectSchema{{Type: "issue"}}, nil
}

func (p *fakeProvider) Pull(ctx context.Context, req core.PullRequest) (core.PullResult, error) {
	if p.pull != nil {
		return p.pull(ctx, req)
	}
	return core.PullResult{}, nil
}

func (p *fakeProvider) Push(ctx context.Context, req core.PushRequest) (core.PushResult, error) {
	if p.push != nil {
		return p.push(ctx, req)
	}
	return core.PushResult{}, nil
}

func (p *fakeProvider) ClassifyError(err error) core.SyncError {
	if p.classify != nil {
		return p.classify(err)
	}
	if err == nil {
		return core.SyncError{}
	}
	return core.SyncError{Kind: "other", Message: err.Error()}
}

func registerCoreProvider(t *testing.T, provider string, implementation core.Provider) {
	t.Helper()
	core.ResetForTest()
	core.Register(provider, provider, func() core.Provider { return implementation })
	t.Cleanup(restoreCoreNoopProvider)
}

func restoreCoreNoopProvider() {
	core.ResetForTest()
	core.Register("noop", "No-op", func() core.Provider { return core.NoopProvider{} })
}

func providerInstallationQualificationRepo(installationID, resourceID uuid.UUID, permissions []byte) *fakeRepo {
	repository := newFakeRepo()
	repository.installations[installationID] = repo.ProviderInstallation{
		ID:                     installationID,
		TenantID:               "tenant-1",
		Provider:               "github",
		DisplayName:            "GitHub App",
		InstallationKind:       repo.InstallationKindGitHubApp,
		Status:                 repo.InstallationStatusActive,
		ExternalInstallationID: "12345",
		AccountLogin:           "acme",
		Permissions:            permissions,
		CapabilityProfile:      []byte(`{}`),
		ResourceSelection:      repo.ResourceSelectionSelected,
		QualificationStatus:    repo.TestStatusUntested,
		CreatedBy:              "admin-1",
		UpdatedBy:              "admin-1",
	}
	repository.resources[resourceID] = repo.ProviderInstallationResource{
		ID:             resourceID,
		TenantID:       "tenant-1",
		InstallationID: installationID,
		Provider:       "github",
		ResourceType:   repo.ResourceTypeRepository,
		ResourceKey:    "acme/app",
		DisplayName:    "acme/app",
		Selected:       true,
		Status:         repo.ResourceStatusActive,
		Permissions:    []byte(`{}`),
	}
	return repository
}

type fakeRepo struct {
	connections            map[uuid.UUID]repo.Connection
	mappings               map[uuid.UUID]repo.Mapping
	installations          map[uuid.UUID]repo.ProviderInstallation
	resources              map[uuid.UUID]repo.ProviderInstallationResource
	cursor                 []byte
	claimedRuns            []repo.SyncRun
	insertedRuns           []repo.SyncRun
	updateCredential       bool
	updateWebhookSecret    bool
	deletedConnectionActor string
	runDetail              *repo.RunDetail
	listRunsFilter         repo.ListRunsFilter
	enqueuedBackfillReset  bool
	recordTimelineFilter   repo.RecordTimelineFilter
	timelineRows           []repo.RecordTimelineEntry
	events                 map[uuid.UUID]repo.SyncEvent
	listEventsFilter       repo.ListEventsFilter
	applyPullInputs        []repo.ApplyPullInput
	preparedPushRecords    []repo.PushRecord
	applyPushInputs        []repo.ApplyPushInput
	attempts               []repo.AttemptInput
	succeededRuns          []uuid.UUID
	failedMarks            []failedMark
	quarantines            []quarantineMark
	resumedConnections     []uuid.UUID
	batchResolveInputs     []batchResolveInput
	batchConflictMappingID uuid.UUID
	health                 repo.Health
	metricSnapshot         repo.MetricSnapshot
	refreshedRunClaims     []uuid.UUID
	refreshRunClaimRows    int64
	claimErr               error
	createConnectionErr    error
	updateConnectionErr    error
	deleteConnectionErr    error
	resumeConnectionErr    error
	createInstallationErr  error
	deleteInstallationErr  error
	selectResourcesErr     error
	resetCursorErr         error
	enqueueBackfillErr     error
	insertRunErr           error
	updateMappingErr       error
	recordEventErr         error
	enqueueEventErr        error
	replayEventErr         error
	retryRunErr            error
	retryFailureErr        error
	resolveConflictErr     error
	resolveConflictsErr    error
	prepareRunCursorErr    error
	preparePushErr         error
	applyPullErr           error
	applyPushErr           error
	markSucceededRows      int64
	markSucceededErr       error
	markFailedRows         int64
	markFailedErr          error
	quarantineErr          error
	metricSnapshotErr      error
}

type failedMark struct {
	id      uuid.UUID
	owner   string
	kind    string
	message string
	delay   time.Duration
	dead    bool
}

type quarantineMark struct {
	tenantID     string
	connectionID uuid.UUID
	reason       string
}

type batchResolveInput struct {
	tenantID   string
	ids        []uuid.UUID
	resolution string
	actor      string
}

func newFakeRepo() *fakeRepo {
	return ptrext.Of(fakeRepo{
		connections:         map[uuid.UUID]repo.Connection{},
		mappings:            map[uuid.UUID]repo.Mapping{},
		installations:       map[uuid.UUID]repo.ProviderInstallation{},
		resources:           map[uuid.UUID]repo.ProviderInstallationResource{},
		events:              map[uuid.UUID]repo.SyncEvent{},
		cursor:              []byte("{}"),
		refreshRunClaimRows: 1,
	})
}

func (r *fakeRepo) ListConnections(context.Context, string) ([]repo.Connection, error) {
	out := make([]repo.Connection, 0, len(r.connections))
	for _, conn := range r.connections {
		out = append(out, conn)
	}
	return out, nil
}

func (r *fakeRepo) ListProviderInstallations(_ context.Context, tenantID string) ([]repo.ProviderInstallation, error) {
	out := make([]repo.ProviderInstallation, 0, len(r.installations))
	for _, installation := range r.installations {
		if installation.TenantID == tenantID {
			out = append(out, installation)
		}
	}
	return out, nil
}

func (r *fakeRepo) GetProviderInstallation(_ context.Context, tenantID string, id uuid.UUID) (*repo.ProviderInstallation, error) {
	installation, ok := r.installations[id]
	if !ok || installation.TenantID != tenantID {
		return nil, repo.ErrInstallationNotFound
	}
	return ptrext.Of(installation), nil
}

func (r *fakeRepo) CreateProviderInstallation(_ context.Context, in repo.ProviderInstallationWithResources) (*repo.ProviderInstallation, []repo.ProviderInstallationResource, error) {
	if r.createInstallationErr != nil {
		return nil, nil, r.createInstallationErr
	}
	now := time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC)
	if in.Installation.CreatedAt.IsZero() {
		in.Installation.CreatedAt = now
	}
	if in.Installation.UpdatedAt.IsZero() {
		in.Installation.UpdatedAt = now
	}
	r.installations[in.Installation.ID] = in.Installation
	for _, resource := range in.Resources {
		if resource.CreatedAt.IsZero() {
			resource.CreatedAt = now
		}
		if resource.UpdatedAt.IsZero() {
			resource.UpdatedAt = now
		}
		r.resources[resource.ID] = resource
	}
	return ptrext.Of(in.Installation), append([]repo.ProviderInstallationResource(nil), in.Resources...), nil
}

func (r *fakeRepo) UpdateProviderInstallationQualification(_ context.Context, tenantID string, id uuid.UUID, status string, lastError string, capabilityProfile []byte, actor string) (*repo.ProviderInstallation, error) {
	installation, ok := r.installations[id]
	if !ok || installation.TenantID != tenantID {
		return nil, repo.ErrInstallationNotFound
	}
	now := time.Date(2026, 7, 8, 2, 3, 4, 0, time.UTC)
	installation.QualificationStatus = status
	installation.LastQualifiedAt = ptrext.Of(now)
	installation.LastError = lastError
	installation.CapabilityProfile = append([]byte(nil), capabilityProfile...)
	installation.UpdatedBy = actor
	installation.UpdatedAt = now
	r.installations[id] = installation
	return ptrext.Of(installation), nil
}

func (r *fakeRepo) DeleteProviderInstallation(_ context.Context, tenantID string, id uuid.UUID, actor string) error {
	if r.deleteInstallationErr != nil {
		return r.deleteInstallationErr
	}
	installation, ok := r.installations[id]
	if !ok || installation.TenantID != tenantID {
		return repo.ErrInstallationNotFound
	}
	installation.Status = repo.InstallationStatusDeleted
	installation.UpdatedBy = actor
	r.installations[id] = installation
	for resourceID, resource := range r.resources {
		if resource.TenantID == tenantID && resource.InstallationID == id {
			resource.Selected = false
			resource.Status = repo.ResourceStatusRemoved
			r.resources[resourceID] = resource
		}
	}
	return nil
}

func (r *fakeRepo) ListProviderInstallationResources(_ context.Context, tenantID string, installationID uuid.UUID) ([]repo.ProviderInstallationResource, error) {
	if _, err := r.GetProviderInstallation(context.Background(), tenantID, installationID); err != nil {
		return nil, err
	}
	out := make([]repo.ProviderInstallationResource, 0, len(r.resources))
	for _, resource := range r.resources {
		if resource.TenantID == tenantID && resource.InstallationID == installationID {
			out = append(out, resource)
		}
	}
	return out, nil
}

func (r *fakeRepo) SelectProviderInstallationResources(_ context.Context, tenantID string, installationID uuid.UUID, resourceIDs []uuid.UUID, actor string) ([]repo.ProviderInstallationResource, error) {
	if r.selectResourcesErr != nil {
		return nil, r.selectResourcesErr
	}
	installation, ok := r.installations[installationID]
	if !ok || installation.TenantID != tenantID {
		return nil, repo.ErrInstallationNotFound
	}
	selected := map[uuid.UUID]bool{}
	for _, id := range resourceIDs {
		selected[id] = true
	}
	for id, resource := range r.resources {
		if resource.TenantID == tenantID && resource.InstallationID == installationID {
			resource.Selected = selected[id]
			r.resources[id] = resource
		}
	}
	installation.UpdatedBy = actor
	if len(selected) == 0 {
		installation.ResourceSelection = repo.ResourceSelectionNone
	} else {
		installation.ResourceSelection = repo.ResourceSelectionSelected
	}
	r.installations[installationID] = installation
	return r.ListProviderInstallationResources(context.Background(), tenantID, installationID)
}

func (r *fakeRepo) GetConnection(_ context.Context, tenantID string, id uuid.UUID) (*repo.Connection, error) {
	conn, ok := r.connections[id]
	if !ok || conn.TenantID != tenantID {
		return nil, repo.ErrConnectionNotFound
	}
	return ptrext.Of(conn), nil
}

func (r *fakeRepo) CreateConnection(_ context.Context, in repo.Connection) (*repo.Connection, error) {
	if r.createConnectionErr != nil {
		return nil, r.createConnectionErr
	}
	now := time.Date(2026, 7, 8, 1, 2, 3, 0, time.UTC)
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	if in.UpdatedAt.IsZero() {
		in.UpdatedAt = now
	}
	r.connections[in.ID] = in
	return ptrext.Of(in), nil
}

func (r *fakeRepo) UpdateConnection(_ context.Context, in repo.Connection, updateCredential, updateWebhookSecret bool) (*repo.Connection, error) {
	if r.updateConnectionErr != nil {
		return nil, r.updateConnectionErr
	}
	r.updateCredential = updateCredential
	r.updateWebhookSecret = updateWebhookSecret
	r.connections[in.ID] = in
	return ptrext.Of(in), nil
}

func (r *fakeRepo) DeleteConnection(_ context.Context, tenantID string, id uuid.UUID, actor string) error {
	if r.deleteConnectionErr != nil {
		return r.deleteConnectionErr
	}
	conn, ok := r.connections[id]
	if !ok || conn.TenantID != tenantID {
		return repo.ErrConnectionNotFound
	}
	r.deletedConnectionActor = actor
	delete(r.connections, id)
	return nil
}

func (r *fakeRepo) UpdateConnectionTestResult(_ context.Context, tenantID string, id uuid.UUID, ok bool, lastError string) (*repo.Connection, error) {
	conn, exists := r.connections[id]
	if !exists || conn.TenantID != tenantID {
		return nil, repo.ErrConnectionNotFound
	}
	now := time.Date(2026, 7, 8, 2, 3, 4, 0, time.UTC)
	conn.LastTestedAt = ptrext.Of(now)
	conn.LastError = lastError
	if ok {
		conn.LastTestStatus = repo.TestStatusOK
	} else {
		conn.LastTestStatus = repo.TestStatusFailed
	}
	r.connections[id] = conn
	return ptrext.Of(conn), nil
}

func (r *fakeRepo) ResumeConnection(_ context.Context, tenantID string, id uuid.UUID, actor string) (*repo.Connection, error) {
	if r.resumeConnectionErr != nil {
		return nil, r.resumeConnectionErr
	}
	conn, exists := r.connections[id]
	if !exists || conn.TenantID != tenantID {
		return nil, repo.ErrConnectionNotFound
	}
	conn.Enabled = true
	conn.Status = repo.ConnectionStatusActive
	conn.LastError = ""
	conn.UpdatedBy = actor
	r.connections[id] = conn
	r.resumedConnections = append(r.resumedConnections, id)
	return ptrext.Of(conn), nil
}

func (r *fakeRepo) ListMappings(_ context.Context, tenantID string, connectionID uuid.UUID) ([]repo.Mapping, error) {
	out := []repo.Mapping{}
	for _, mapping := range r.mappings {
		if mapping.TenantID != tenantID {
			continue
		}
		if connectionID != uuid.Nil && mapping.ConnectionID != connectionID {
			continue
		}
		out = append(out, mapping)
	}
	return out, nil
}

func (r *fakeRepo) GetMapping(_ context.Context, tenantID string, id uuid.UUID) (*repo.Mapping, error) {
	mapping, ok := r.mappings[id]
	if !ok || mapping.TenantID != tenantID {
		return nil, repo.ErrMappingNotFound
	}
	return ptrext.Of(mapping), nil
}

func (r *fakeRepo) ResolveRunMapping(_ context.Context, tenantID string, connectionID uuid.UUID, mappingID *uuid.UUID) (*repo.Mapping, error) {
	if mappingID != nil {
		mapping, ok := r.mappings[ptrext.Indirect(mappingID)]
		if ok && mapping.TenantID == tenantID && mapping.ConnectionID == connectionID && mapping.Enabled {
			return ptrext.Of(mapping), nil
		}
		return nil, repo.ErrMappingNotFound
	}
	for _, mapping := range r.mappings {
		if mapping.TenantID == tenantID && mapping.ConnectionID == connectionID && mapping.Enabled {
			return ptrext.Of(mapping), nil
		}
	}
	return nil, repo.ErrMappingNotFound
}

func (r *fakeRepo) UpdateMapping(_ context.Context, in repo.Mapping) (*repo.Mapping, error) {
	if r.updateMappingErr != nil {
		return nil, r.updateMappingErr
	}
	return ptrext.Of(in), nil
}

func (r *fakeRepo) ResetCursor(_ context.Context, tenantID string, mappingID uuid.UUID, actor string) (*repo.ResetCursorResult, error) {
	if r.resetCursorErr != nil {
		return nil, r.resetCursorErr
	}
	mapping, ok := r.mappings[mappingID]
	if !ok || mapping.TenantID != tenantID {
		return nil, repo.ErrMappingNotFound
	}
	run := repo.SyncRun{
		ID:           uuid.New(),
		TenantID:     tenantID,
		ConnectionID: mapping.ConnectionID,
		MappingID:    ptrext.Of(mapping.ID),
		Direction:    repo.DirectionPull,
		Trigger:      repo.TriggerManual,
		Status:       repo.RunStatusQueued,
		ActorID:      actor,
	}
	r.insertedRuns = append(r.insertedRuns, run)
	return ptrext.Of(repo.ResetCursorResult{Mapping: mapping, Run: run}), nil
}

func (r *fakeRepo) EnqueueBackfill(_ context.Context, tenantID string, mappingID uuid.UUID, actor string, resetCursor bool) (*repo.BackfillResult, error) {
	if r.enqueueBackfillErr != nil {
		return nil, r.enqueueBackfillErr
	}
	mapping, ok := r.mappings[mappingID]
	if !ok || mapping.TenantID != tenantID {
		return nil, repo.ErrMappingNotFound
	}
	run := repo.SyncRun{
		ID:           uuid.New(),
		TenantID:     tenantID,
		ConnectionID: mapping.ConnectionID,
		MappingID:    ptrext.Of(mapping.ID),
		Direction:    repo.DirectionPull,
		Trigger:      repo.TriggerBackfill,
		Status:       repo.RunStatusQueued,
		ActorID:      actor,
	}
	r.enqueuedBackfillReset = resetCursor
	r.insertedRuns = append(r.insertedRuns, run)
	return ptrext.Of(repo.BackfillResult{Mapping: mapping, Run: run}), nil
}

func (r *fakeRepo) InsertRun(_ context.Context, run repo.SyncRun) (*repo.SyncRun, error) {
	if r.insertRunErr != nil {
		return nil, r.insertRunErr
	}
	r.insertedRuns = append(r.insertedRuns, run)
	return ptrext.Of(run), nil
}

func (r *fakeRepo) ListRuns(_ context.Context, filter repo.ListRunsFilter) (repo.ListRunsResult, error) {
	r.listRunsFilter = filter
	return repo.ListRunsResult{}, nil
}

func (r *fakeRepo) GetRunDetail(_ context.Context, _ string, id uuid.UUID) (*repo.RunDetail, error) {
	if r.runDetail == nil || r.runDetail.Run.ID != id {
		return nil, repo.ErrRunNotFound
	}
	return r.runDetail, nil
}

func (r *fakeRepo) RecordTimeline(_ context.Context, filter repo.RecordTimelineFilter) ([]repo.RecordTimelineEntry, error) {
	r.recordTimelineFilter = filter
	return append([]repo.RecordTimelineEntry(nil), r.timelineRows...), nil
}

func (r *fakeRepo) RecordEvent(_ context.Context, in repo.SyncEvent) (*repo.SyncEvent, error) {
	if r.recordEventErr != nil {
		return nil, r.recordEventErr
	}
	for _, event := range r.events {
		if event.TenantID == in.TenantID && event.ConnectionID == in.ConnectionID && event.DedupeKey == in.DedupeKey {
			return ptrext.Of(event), nil
		}
	}
	r.events[in.ID] = in
	return ptrext.Of(in), nil
}

func (r *fakeRepo) ListEvents(_ context.Context, filter repo.ListEventsFilter) (repo.ListEventsResult, error) {
	r.listEventsFilter = filter
	out := make([]repo.SyncEvent, 0, len(r.events))
	for _, event := range r.events {
		if event.TenantID != filter.TenantID {
			continue
		}
		if filter.ConnectionID != nil && event.ConnectionID != ptrext.Indirect(filter.ConnectionID) {
			continue
		}
		if filter.Status != "" && event.Status != filter.Status {
			continue
		}
		out = append(out, event)
	}
	return repo.ListEventsResult{Events: out}, nil
}

func (r *fakeRepo) GetEvent(_ context.Context, tenantID string, id uuid.UUID) (*repo.SyncEvent, error) {
	event, ok := r.events[id]
	if !ok || event.TenantID != tenantID {
		return nil, repo.ErrEventNotFound
	}
	return ptrext.Of(event), nil
}

func (r *fakeRepo) ReplayEvent(_ context.Context, tenantID string, id uuid.UUID, actor string, mappingID uuid.UUID, direction string) (*repo.SyncEvent, *repo.SyncRun, error) {
	if r.replayEventErr != nil {
		return nil, nil, r.replayEventErr
	}
	event, ok := r.events[id]
	if !ok || event.TenantID != tenantID {
		return nil, nil, repo.ErrEventNotFound
	}
	if event.RunID != nil || event.Status == repo.EventStatusReplayed {
		return nil, nil, repo.ErrConflict
	}
	run := repo.SyncRun{
		ID:           uuid.New(),
		TenantID:     tenantID,
		ConnectionID: event.ConnectionID,
		MappingID:    ptrext.Of(mappingID),
		Direction:    direction,
		Trigger:      repo.TriggerWebhook,
		Status:       repo.RunStatusQueued,
		ActorID:      actor,
	}
	event.Status = repo.EventStatusReplayed
	event.ReplayedAt = ptrext.Of(time.Date(2026, 7, 8, 6, 7, 8, 0, time.UTC))
	event.ReplayedBy = actor
	event.RunID = ptrext.Of(run.ID)
	r.events[id] = event
	r.insertedRuns = append(r.insertedRuns, run)
	return ptrext.Of(event), ptrext.Of(run), nil
}

func (r *fakeRepo) EnqueueEventRun(_ context.Context, tenantID string, id uuid.UUID, actor string) (*repo.SyncEvent, *repo.SyncRun, error) {
	if r.enqueueEventErr != nil {
		return nil, nil, r.enqueueEventErr
	}
	event, ok := r.events[id]
	if !ok || event.TenantID != tenantID {
		return nil, nil, repo.ErrEventNotFound
	}
	if event.RunID != nil || event.Status != repo.EventStatusReceived {
		return ptrext.Of(event), nil, nil
	}
	mapping, ok := r.issuePullMapping(tenantID, event.ConnectionID, event.MappingID)
	if !ok {
		event.Status = repo.EventStatusIgnored
		event.FailureReason = "no enabled pull issue mapping"
		r.events[id] = event
		return ptrext.Of(event), nil, nil
	}
	run := repo.SyncRun{
		ID:            uuid.New(),
		TenantID:      tenantID,
		ConnectionID:  event.ConnectionID,
		MappingID:     ptrext.Of(mapping.ID),
		Direction:     repo.DirectionPull,
		Trigger:       repo.TriggerWebhook,
		Status:        repo.RunStatusQueued,
		ActorID:       actor,
		InputMetadata: []byte(`{"issue_number":42}`),
	}
	event.MappingID = ptrext.Of(mapping.ID)
	event.Status = repo.EventStatusReplayed
	event.ReplayedAt = ptrext.Of(time.Date(2026, 7, 8, 6, 7, 8, 0, time.UTC))
	event.ReplayedBy = actor
	event.RunID = ptrext.Of(run.ID)
	r.events[id] = event
	r.insertedRuns = append(r.insertedRuns, run)
	return ptrext.Of(event), ptrext.Of(run), nil
}

func (r *fakeRepo) issuePullMapping(tenantID string, connectionID uuid.UUID, mappingID *uuid.UUID) (repo.Mapping, bool) {
	for _, mapping := range r.mappings {
		if mapping.TenantID != tenantID || mapping.ConnectionID != connectionID || !mapping.Enabled {
			continue
		}
		if mapping.LocalObjectType != "customer_request" || mapping.ExternalObjectType != "issue" {
			continue
		}
		if mapping.Direction != repo.DirectionPull && mapping.Direction != repo.DirectionBidirectional {
			continue
		}
		if mappingID != nil && mapping.ID != ptrext.Indirect(mappingID) {
			continue
		}
		return mapping, true
	}
	return repo.Mapping{}, false
}

func (r *fakeRepo) ClaimBatch(context.Context, int, string) ([]repo.SyncRun, error) {
	if r.claimErr != nil {
		return nil, r.claimErr
	}
	out := append([]repo.SyncRun(nil), r.claimedRuns...)
	r.claimedRuns = nil
	return out, nil
}

func (r *fakeRepo) RefreshRunClaim(_ context.Context, id uuid.UUID, _ string) (int64, error) {
	r.refreshedRunClaims = append(r.refreshedRunClaims, id)
	return r.refreshRunClaimRows, nil
}

func (r *fakeRepo) PrepareRunCursor(context.Context, uuid.UUID, string, string, uuid.UUID, string) ([]byte, error) {
	if r.prepareRunCursorErr != nil {
		return nil, r.prepareRunCursorErr
	}
	return append([]byte(nil), r.cursor...), nil
}

func (r *fakeRepo) ApplyPullResult(_ context.Context, in repo.ApplyPullInput) (repo.ApplyStats, error) {
	r.applyPullInputs = append(r.applyPullInputs, in)
	if r.applyPullErr != nil {
		return repo.ApplyStats{}, r.applyPullErr
	}
	records := len(in.Records) + len(in.Children)
	return repo.ApplyStats{RecordsSeen: records, RecordsChanged: records}, nil
}

func (r *fakeRepo) PreparePushRecords(context.Context, uuid.UUID, string, string, uuid.UUID, string, int) ([]repo.PushRecord, error) {
	if r.preparePushErr != nil {
		return nil, r.preparePushErr
	}
	return append([]repo.PushRecord(nil), r.preparedPushRecords...), nil
}

func (r *fakeRepo) ApplyPushResult(_ context.Context, in repo.ApplyPushInput) (repo.ApplyStats, error) {
	r.applyPushInputs = append(r.applyPushInputs, in)
	if r.applyPushErr != nil {
		return repo.ApplyStats{}, r.applyPushErr
	}
	return repo.ApplyStats{RecordsSeen: len(in.Records), RecordsChanged: len(in.Results)}, nil
}

func (r *fakeRepo) RecordAttempt(_ context.Context, in repo.AttemptInput) error {
	r.attempts = append(r.attempts, in)
	return nil
}

func (r *fakeRepo) MarkRunSucceeded(_ context.Context, id uuid.UUID, _ string) (int64, error) {
	if r.markSucceededErr != nil {
		return 0, r.markSucceededErr
	}
	r.succeededRuns = append(r.succeededRuns, id)
	if r.markSucceededRows != 0 {
		return r.markSucceededRows, nil
	}
	return 1, nil
}

func (r *fakeRepo) MarkRunFailed(_ context.Context, id uuid.UUID, owner, kind, message string, nextDelay time.Duration, dead bool) (int64, error) {
	if r.markFailedErr != nil {
		return 0, r.markFailedErr
	}
	r.failedMarks = append(r.failedMarks, failedMark{
		id:      id,
		owner:   owner,
		kind:    kind,
		message: message,
		delay:   nextDelay,
		dead:    dead,
	})
	if r.markFailedRows != 0 {
		return r.markFailedRows, nil
	}
	return 1, nil
}

func (r *fakeRepo) QuarantineDegradedConnection(_ context.Context, tenantID string, connectionID uuid.UUID, reason string) (int64, error) {
	if r.quarantineErr != nil {
		return 0, r.quarantineErr
	}
	r.quarantines = append(r.quarantines, quarantineMark{
		tenantID:     tenantID,
		connectionID: connectionID,
		reason:       reason,
	})
	return 1, nil
}

func (r *fakeRepo) RetryRun(_ context.Context, _ string, id uuid.UUID) (*repo.SyncRun, error) {
	if r.retryRunErr != nil {
		return nil, r.retryRunErr
	}
	return ptrext.Of(repo.SyncRun{ID: id, Status: repo.RunStatusQueued}), nil
}

func (r *fakeRepo) RetryFailure(_ context.Context, tenantID string, id uuid.UUID, actor string) (*repo.RecordFailure, error) {
	if r.retryFailureErr != nil {
		return nil, r.retryFailureErr
	}
	return ptrext.Of(repo.RecordFailure{ID: id, TenantID: tenantID, ResolvedBy: actor}), nil
}

func (r *fakeRepo) ResolveConflict(_ context.Context, tenantID string, id uuid.UUID, resolution, actor string) (*repo.ConflictRow, error) {
	if r.resolveConflictErr != nil {
		return nil, r.resolveConflictErr
	}
	return ptrext.Of(repo.ConflictRow{
		ID:          id,
		TenantID:    tenantID,
		MappingID:   r.batchConflictMappingID,
		ExternalKey: "ISS-1",
		Status:      "resolved",
		Resolution:  resolution,
		ResolvedBy:  actor,
	}), nil
}

func (r *fakeRepo) ResolveConflicts(_ context.Context, tenantID string, ids []uuid.UUID, resolution, actor string) (repo.BatchResolveConflictsResult, error) {
	if r.resolveConflictsErr != nil {
		return repo.BatchResolveConflictsResult{}, r.resolveConflictsErr
	}
	r.batchResolveInputs = append(r.batchResolveInputs, batchResolveInput{
		tenantID:   tenantID,
		ids:        append([]uuid.UUID(nil), ids...),
		resolution: resolution,
		actor:      actor,
	})
	rows := make([]repo.ConflictRow, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, repo.ConflictRow{
			ID:         id,
			TenantID:   tenantID,
			MappingID:  r.batchConflictMappingID,
			Status:     "resolved",
			Resolution: resolution,
			ResolvedBy: actor,
		})
	}
	return repo.BatchResolveConflictsResult{Conflicts: rows}, nil
}

func (r *fakeRepo) Health(context.Context, string) (repo.Health, error) {
	return r.health, nil
}

func (r *fakeRepo) MetricSnapshot(context.Context) (repo.MetricSnapshot, error) {
	if r.metricSnapshotErr != nil {
		return repo.MetricSnapshot{}, r.metricSnapshotErr
	}
	return r.metricSnapshot, nil
}
