//go:build integration

// SPDX-License-Identifier: Apache-2.0

package externalsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	core "github.com/Phixsura/attune/internal/externalsync"
	githubadapter "github.com/Phixsura/attune/internal/externalsync/adapter/githubissue"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	crrepo "github.com/Phixsura/attune/internal/repo/customerrequest"
	externalsyncrepo "github.com/Phixsura/attune/internal/repo/externalsync"
	"github.com/Phixsura/attune/internal/testdb"
)

func TestRepoConnectionMappingRunLifecycle(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-run")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-run")

	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-run")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	requireDefaultExternalSyncMapping(t, mapping)

	run := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)

	claimed := claimExternalSyncRuns(t, ctx, repository, 1, "worker-1")
	requireClaimedExternalSyncRun(t, claimed, run.ID, 1, "worker-1")
	cursor, err := repository.PrepareRunCursor(ctx, run.ID, "worker-1", tenantID, mapping.ID, externalsyncrepo.StreamDefault)
	if err != nil {
		t.Fatalf("PrepareRunCursor returned error: %v", err)
	}
	if string(cursor) != "{}" {
		t.Fatalf("prepared cursor = %s; want empty object", cursor)
	}
	refreshed, err := repository.RefreshRunClaim(ctx, run.ID, "worker-1")
	if err != nil {
		t.Fatalf("RefreshRunClaim returned error: %v", err)
	}
	requireRowsAffected(t, "RefreshRunClaim", refreshed)
	if _, err := repository.PrepareRunCursor(ctx, run.ID, "wrong-worker", tenantID, mapping.ID, externalsyncrepo.StreamDefault); !errors.Is(err, externalsyncrepo.ErrRunNotFound) {
		t.Fatalf("PrepareRunCursor wrong owner error = %v; want ErrRunNotFound", err)
	}

	affected, err := repository.MarkRunFailed(ctx, run.ID, "worker-1", "http_429", "rate limited", time.Minute, false)
	if err != nil {
		t.Fatalf("MarkRunFailed returned error: %v", err)
	}
	requireRowsAffected(t, "MarkRunFailed", affected)
	retryAfter := time.Now().UTC().Add(2 * time.Minute)
	if err := repository.RecordAttempt(ctx, externalsyncrepo.AttemptInput{
		RunID:             run.ID,
		AttemptNumber:     1,
		Result:            "failed",
		HTTPStatus:        429,
		ProviderRequestID: "github-rate-1",
		RetryAfter:        ptrext.Of(retryAfter),
		ErrorKind:         "rate_limited",
		ErrorMessage:      "secondary rate limit",
	}); err != nil {
		t.Fatalf("RecordAttempt returned error: %v", err)
	}
	if err := repository.RecordAttempt(ctx, externalsyncrepo.AttemptInput{
		RunID:        run.ID,
		Result:       "failed",
		HTTPStatus:   429,
		RetryAfter:   ptrext.Of(retryAfter),
		ErrorKind:    "rate_limited",
		ErrorMessage: "default attempt",
	}); err != nil {
		t.Fatalf("RecordAttempt default fields returned error: %v", err)
	}
	health, err := repository.Health(ctx, tenantID)
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	requireThrottledExternalSyncHealth(t, health)
	_ = claimExternalSyncRuns(t, ctx, repository, 1, "worker-2")

	retried, err := repository.RetryRun(ctx, tenantID, run.ID)
	if err != nil {
		t.Fatalf("RetryRun returned error: %v", err)
	}
	requireQueuedRetryRun(t, ptrext.Indirect(retried))

	claimed = claimExternalSyncRuns(t, ctx, repository, 1, "worker-2")
	requireClaimedExternalSyncRun(t, claimed, run.ID, 2, "worker-2")
	affected, err = repository.MarkRunSucceeded(ctx, run.ID, "worker-2")
	if err != nil {
		t.Fatalf("MarkRunSucceeded returned error: %v", err)
	}
	requireRowsAffected(t, "MarkRunSucceeded", affected)

	health, err = repository.Health(ctx, tenantID)
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	requireCleanExternalSyncHealth(t, health)

	deadRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	claimed = claimExternalSyncRuns(t, ctx, repository, 0, "worker-dead")
	requireClaimedExternalSyncRun(t, claimed, deadRun.ID, 1, "worker-dead")
	affected, err = repository.MarkRunFailed(ctx, deadRun.ID, "worker-dead", "provider_unavailable", "permanent outage", 0, true)
	if err != nil {
		t.Fatalf("MarkRunFailed dead path returned error: %v", err)
	}
	requireRowsAffected(t, "MarkRunFailed dead", affected)
}

type externalSyncManagementFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	repository *externalsyncrepo.Repo
	tenantID   string
	conn       externalsyncrepo.Connection
	other      externalsyncrepo.Connection
	keyID      string
	rotatedKey string
	webhookKey string
}

func newExternalSyncManagementFixture(t *testing.T, slug string) externalSyncManagementFixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, slug)
	keyID := "kid-" + slug
	rotatedKey := keyID + "-rotated"
	webhookKey := keyID + "-webhook"
	insertExternalSyncKey(t, ctx, pool, keyID)
	insertExternalSyncKey(t, ctx, pool, rotatedKey)
	insertExternalSyncKey(t, ctx, pool, webhookKey)

	conn := createExternalSyncConnection(t, ctx, repository, tenantID, keyID)
	other := createExternalSyncConnectionNamed(t, ctx, repository, tenantID, keyID, "GitHub Other")
	return externalSyncManagementFixture{
		ctx:        ctx,
		pool:       pool,
		repository: repository,
		tenantID:   tenantID,
		conn:       conn,
		other:      other,
		keyID:      keyID,
		rotatedKey: rotatedKey,
		webhookKey: webhookKey,
	}
}

func TestRepoConnectionCreateListAndValidation(t *testing.T) {
	fixture := newExternalSyncManagementFixture(t, "external-sync-management-create")

	listed, err := fixture.repository.ListConnections(fixture.ctx, fixture.tenantID)
	if err != nil {
		t.Fatalf("ListConnections returned error: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed connections = %#v; want two rows", listed)
	}
	loaded, err := fixture.repository.GetConnection(fixture.ctx, fixture.tenantID, fixture.conn.ID)
	if err != nil {
		t.Fatalf("GetConnection returned error: %v", err)
	}
	if loaded.ID != fixture.conn.ID || loaded.CredentialKeyID != fixture.keyID {
		t.Fatalf("loaded connection = %#v; want original connection", loaded)
	}
	_, err = fixture.repository.CreateConnection(fixture.ctx, externalsyncrepo.Connection{
		ID:                   uuid.New(),
		TenantID:             fixture.tenantID,
		Provider:             fixture.conn.Provider,
		Name:                 fixture.conn.Name,
		Enabled:              true,
		Status:               externalsyncrepo.ConnectionStatusActive,
		AuthType:             fixture.conn.AuthType,
		ProviderConfig:       []byte("{}"),
		Scopes:               []string{"issues"},
		CredentialKeyID:      fixture.keyID,
		CredentialCiphertext: []byte("ciphertext"),
		CreatedBy:            "admin-duplicate",
		UpdatedBy:            "admin-duplicate",
	})
	if !errors.Is(err, externalsyncrepo.ErrConflict) {
		t.Fatalf("duplicate CreateConnection error = %v; want ErrConflict", err)
	}
	_, err = fixture.repository.CreateConnection(fixture.ctx, externalsyncrepo.Connection{
		ID:                   uuid.New(),
		TenantID:             fixture.tenantID,
		Provider:             "github",
		Name:                 "Bad Key",
		Enabled:              true,
		Status:               externalsyncrepo.ConnectionStatusActive,
		AuthType:             "token",
		ProviderConfig:       []byte("{}"),
		Scopes:               []string{"issues"},
		CredentialKeyID:      "kid-external-sync-management-missing",
		CredentialCiphertext: []byte("ciphertext"),
		CreatedBy:            "admin-bad-key",
		UpdatedBy:            "admin-bad-key",
	})
	if err == nil {
		t.Fatal("CreateConnection with missing key returned nil error")
	}
	_, err = fixture.repository.CreateConnection(fixture.ctx, externalsyncrepo.Connection{
		ID:                      uuid.New(),
		TenantID:                fixture.tenantID,
		Provider:                "github",
		Name:                    "Bad Webhook Key",
		Enabled:                 true,
		Status:                  externalsyncrepo.ConnectionStatusActive,
		AuthType:                "token",
		ProviderConfig:          []byte("{}"),
		Scopes:                  []string{"issues"},
		CredentialKeyID:         fixture.keyID,
		CredentialCiphertext:    []byte("ciphertext"),
		WebhookSecretKeyID:      "kid-external-sync-management-webhook-missing",
		WebhookSecretCiphertext: []byte("webhook-ciphertext"),
		CreatedBy:               "admin-bad-webhook",
		UpdatedBy:               "admin-bad-webhook",
	})
	if err == nil {
		t.Fatal("CreateConnection with missing webhook key returned nil error")
	}
}

func TestRepoConnectionProviderInstallationBindingPersistence(t *testing.T) {
	fixture := newExternalSyncManagementFixture(t, "external-sync-management-bound")
	installation := createExternalProviderInstallation(t, fixture.ctx, fixture.repository, fixture.tenantID)

	bound := createExternalSyncConnectionWithInstallation(t, fixture, installation.ID)
	loaded, err := fixture.repository.GetConnection(fixture.ctx, fixture.tenantID, bound.ID)
	if err != nil {
		t.Fatalf("GetConnection bound returned error: %v", err)
	}
	requireProviderInstallationConnection(t, ptrext.Indirect(loaded), installation.ID)

	listed, err := fixture.repository.ListConnections(fixture.ctx, fixture.tenantID)
	if err != nil {
		t.Fatalf("ListConnections after provider installation returned error: %v", err)
	}
	requireListedProviderInstallationConnection(t, listed, bound.ID, installation.ID)
}

func TestRepoConnectionUpdateProbeAndResumeLifecycle(t *testing.T) {
	fixture := newExternalSyncManagementFixture(t, "external-sync-management-update")

	updated := fixture.conn
	updated.Name = "GitHub Renamed"
	updated.Enabled = false
	updated.BaseURL = "https://api.github.com"
	updated.ProviderConfig = []byte(`{"repo":"acme/app"}`)
	updated.Scopes = []string{"issues", "metadata"}
	updated.CredentialKeyID = fixture.rotatedKey
	updated.CredentialCiphertext = []byte("rotated-ciphertext")
	updated.WebhookSecretKeyID = fixture.webhookKey
	updated.WebhookSecretCiphertext = []byte("webhook-ciphertext")
	updated.UpdatedBy = "admin-2"
	saved, err := fixture.repository.UpdateConnection(fixture.ctx, updated, true, true)
	if err != nil {
		t.Fatalf("UpdateConnection returned error: %v", err)
	}
	if saved.Enabled || saved.Status != externalsyncrepo.ConnectionStatusDisabled ||
		saved.CredentialKeyID != fixture.rotatedKey || saved.WebhookSecretKeyID != fixture.webhookKey {
		t.Fatalf("updated connection = %#v; want disabled rotated connection", saved)
	}
	duplicateUpdate := *saved
	duplicateUpdate.Name = fixture.other.Name
	if _, err := fixture.repository.UpdateConnection(fixture.ctx, duplicateUpdate, false, false); !errors.Is(err, externalsyncrepo.ErrConflict) {
		t.Fatalf("duplicate UpdateConnection error = %v; want ErrConflict", err)
	}
	missingUpdate := *saved
	missingUpdate.ID = uuid.New()
	if _, err := fixture.repository.UpdateConnection(fixture.ctx, missingUpdate, false, false); !errors.Is(err, externalsyncrepo.ErrConnectionNotFound) {
		t.Fatalf("missing UpdateConnection error = %v; want ErrConnectionNotFound", err)
	}
	badCredentialUpdate := *saved
	badCredentialUpdate.CredentialKeyID = "kid-external-sync-management-update-missing"
	if _, err := fixture.repository.UpdateConnection(fixture.ctx, badCredentialUpdate, true, false); err == nil {
		t.Fatal("UpdateConnection with missing credential key returned nil error")
	}
	badWebhookUpdate := *saved
	badWebhookUpdate.WebhookSecretKeyID = "kid-external-sync-management-update-webhook-missing"
	if _, err := fixture.repository.UpdateConnection(fixture.ctx, badWebhookUpdate, false, true); err == nil {
		t.Fatal("UpdateConnection with missing webhook key returned nil error")
	}
}

func TestRepoConnectionProbeAndResumeLifecycle(t *testing.T) {
	fixture := newExternalSyncManagementFixture(t, "external-sync-management-probe")

	failedProbe, err := fixture.repository.UpdateConnectionTestResult(fixture.ctx, fixture.tenantID, fixture.conn.ID, false, strings.Repeat("x", 2100))
	if err != nil {
		t.Fatalf("UpdateConnectionTestResult failed path returned error: %v", err)
	}
	if failedProbe.LastTestStatus != externalsyncrepo.TestStatusFailed || len(failedProbe.LastError) != 2000 {
		t.Fatalf("failed probe = %#v; want truncated failed status", failedProbe)
	}
	okProbe, err := fixture.repository.UpdateConnectionTestResult(fixture.ctx, fixture.tenantID, fixture.conn.ID, true, "")
	if err != nil {
		t.Fatalf("UpdateConnectionTestResult ok path returned error: %v", err)
	}
	if okProbe.LastTestStatus != externalsyncrepo.TestStatusOK {
		t.Fatalf("ok probe status = %q; want ok", okProbe.LastTestStatus)
	}
	if _, err := fixture.repository.UpdateConnectionTestResult(fixture.ctx, fixture.tenantID, uuid.New(), true, ""); !errors.Is(err, externalsyncrepo.ErrConnectionNotFound) {
		t.Fatalf("missing UpdateConnectionTestResult error = %v; want ErrConnectionNotFound", err)
	}
	if _, err := fixture.repository.ResumeConnection(fixture.ctx, fixture.tenantID, uuid.New(), "admin-2"); !errors.Is(err, externalsyncrepo.ErrConnectionNotFound) {
		t.Fatalf("missing ResumeConnection error = %v; want ErrConnectionNotFound", err)
	}
	if _, err := fixture.repository.ResumeConnection(fixture.ctx, fixture.tenantID, fixture.other.ID, "admin-2"); !errors.Is(err, externalsyncrepo.ErrConnectionNotFound) {
		t.Fatalf("active ResumeConnection error = %v; want ErrConnectionNotFound", err)
	}
}

func TestRepoMappingManagementLifecycle(t *testing.T) {
	fixture := newExternalSyncManagementFixture(t, "external-sync-management-mapping")

	allMappings, err := fixture.repository.ListMappings(fixture.ctx, fixture.tenantID, uuid.Nil)
	if err != nil {
		t.Fatalf("ListMappings all returned error: %v", err)
	}
	if len(allMappings) != 2 {
		t.Fatalf("all mappings = %#v; want one mapping per connection", allMappings)
	}
	mapping := firstExternalSyncMapping(t, fixture.ctx, fixture.repository, fixture.tenantID, fixture.conn.ID)
	loadedMapping, err := fixture.repository.GetMapping(fixture.ctx, fixture.tenantID, mapping.ID)
	if err != nil {
		t.Fatalf("GetMapping returned error: %v", err)
	}
	if loadedMapping.ID != mapping.ID {
		t.Fatalf("loaded mapping = %#v; want %s", loadedMapping, mapping.ID)
	}
	resolvedExplicit, err := fixture.repository.ResolveRunMapping(fixture.ctx, fixture.tenantID, fixture.conn.ID, ptrext.Of(mapping.ID))
	if err != nil {
		t.Fatalf("ResolveRunMapping explicit returned error: %v", err)
	}
	resolvedDefault, err := fixture.repository.ResolveRunMapping(fixture.ctx, fixture.tenantID, fixture.conn.ID, nil)
	if err != nil {
		t.Fatalf("ResolveRunMapping default returned error: %v", err)
	}
	if resolvedExplicit.ID != mapping.ID || resolvedDefault.ID != mapping.ID {
		t.Fatalf("resolved mappings explicit=%s default=%s want %s", resolvedExplicit.ID, resolvedDefault.ID, mapping.ID)
	}
	if _, err := fixture.repository.GetMapping(fixture.ctx, fixture.tenantID, uuid.New()); !errors.Is(err, externalsyncrepo.ErrMappingNotFound) {
		t.Fatalf("missing GetMapping error = %v; want ErrMappingNotFound", err)
	}
	if _, err := fixture.repository.ResolveRunMapping(fixture.ctx, fixture.tenantID, uuid.New(), nil); !errors.Is(err, externalsyncrepo.ErrMappingNotFound) {
		t.Fatalf("missing ResolveRunMapping error = %v; want ErrMappingNotFound", err)
	}
	if _, err := fixture.repository.ResolveRunMapping(fixture.ctx, fixture.tenantID, fixture.conn.ID, ptrext.Of(uuid.New())); !errors.Is(err, externalsyncrepo.ErrMappingNotFound) {
		t.Fatalf("missing explicit ResolveRunMapping error = %v; want ErrMappingNotFound", err)
	}
	missingMapping := mapping
	missingMapping.ID = uuid.New()
	if _, err := fixture.repository.UpdateMapping(fixture.ctx, missingMapping); !errors.Is(err, externalsyncrepo.ErrMappingNotFound) {
		t.Fatalf("missing UpdateMapping error = %v; want ErrMappingNotFound", err)
	}
}

func TestRepoMappingBackfillAndConflictLifecycle(t *testing.T) {
	fixture := newExternalSyncManagementFixture(t, "external-sync-management-backfill")
	mapping := firstExternalSyncMapping(t, fixture.ctx, fixture.repository, fixture.tenantID, fixture.conn.ID)

	backfill, err := fixture.repository.EnqueueBackfill(fixture.ctx, fixture.tenantID, mapping.ID, "admin-2", false)
	if err != nil {
		t.Fatalf("EnqueueBackfill without reset returned error: %v", err)
	}
	if backfill.Run.Trigger != externalsyncrepo.TriggerBackfill || backfill.Mapping.ID != mapping.ID {
		t.Fatalf("backfill result = %#v; want queued backfill for mapping", backfill)
	}
	if _, err := fixture.repository.ResetCursor(fixture.ctx, fixture.tenantID, uuid.New(), "admin-2"); !errors.Is(err, externalsyncrepo.ErrMappingNotFound) {
		t.Fatalf("missing ResetCursor error = %v; want ErrMappingNotFound", err)
	}
	if _, err := fixture.repository.EnqueueBackfill(fixture.ctx, fixture.tenantID, uuid.New(), "admin-2", false); !errors.Is(err, externalsyncrepo.ErrMappingNotFound) {
		t.Fatalf("missing EnqueueBackfill error = %v; want ErrMappingNotFound", err)
	}
	emptyBatch, err := fixture.repository.ResolveConflicts(fixture.ctx, fixture.tenantID, nil, "ignored", "admin-2")
	if err != nil {
		t.Fatalf("empty ResolveConflicts returned error: %v", err)
	}
	if len(emptyBatch.Conflicts) != 0 {
		t.Fatalf("empty ResolveConflicts = %#v; want no rows", emptyBatch)
	}
}

func TestRepoConnectionDeleteLifecycle(t *testing.T) {
	fixture := newExternalSyncManagementFixture(t, "external-sync-management-delete")

	if err := fixture.repository.DeleteConnection(fixture.ctx, fixture.tenantID, fixture.other.ID, "admin-2"); err != nil {
		t.Fatalf("DeleteConnection returned error: %v", err)
	}
	if err := fixture.repository.DeleteConnection(fixture.ctx, fixture.tenantID, fixture.other.ID, "admin-2"); !errors.Is(err, externalsyncrepo.ErrConnectionNotFound) {
		t.Fatalf("second DeleteConnection error = %v; want ErrConnectionNotFound", err)
	}
	if _, err := fixture.repository.GetConnection(fixture.ctx, fixture.tenantID, fixture.other.ID); !errors.Is(err, externalsyncrepo.ErrConnectionNotFound) {
		t.Fatalf("GetConnection deleted error = %v; want ErrConnectionNotFound", err)
	}
}

func TestRepoProviderInstallationCreateListAndGet(t *testing.T) {
	fixture := newProviderInstallationFixture(t, "external-sync-provider-installation-create")

	if fixture.installation.ID != fixture.installationID || len(fixture.resources) != 2 {
		t.Fatalf("created installation=%#v resources=%#v; want installation with two resources",
			fixture.installation, fixture.resources)
	}
	if !json.Valid(fixture.installation.Permissions) || !json.Valid(fixture.resources[0].Permissions) {
		t.Fatalf("created JSON fields are invalid: installation=%s resource=%s",
			fixture.installation.Permissions, fixture.resources[0].Permissions)
	}
	requireDuplicateProviderInstallationConflict(t, fixture)

	listed, err := fixture.repository.ListProviderInstallations(fixture.ctx, fixture.tenantID)
	if err != nil {
		t.Fatalf("ListProviderInstallations returned error: %v", err)
	}
	if len(listed) != 1 || listed[0].ID != fixture.installationID {
		t.Fatalf("listed installations = %#v; want created installation", listed)
	}
	loaded, err := fixture.repository.GetProviderInstallation(fixture.ctx, fixture.tenantID, fixture.installationID)
	if err != nil {
		t.Fatalf("GetProviderInstallation returned error: %v", err)
	}
	if loaded.AccountLogin != "acme" || loaded.ResourceSelection != externalsyncrepo.ResourceSelectionSelected {
		t.Fatalf("loaded installation = %#v; want acme selected installation", loaded)
	}
}

func TestRepoProviderInstallationQualificationLifecycle(t *testing.T) {
	fixture := newProviderInstallationFixture(t, "external-sync-provider-installation-qualification")

	qualified, err := fixture.repository.UpdateProviderInstallationQualification(
		fixture.ctx,
		fixture.tenantID,
		fixture.installationID,
		externalsyncrepo.TestStatusFailed,
		strings.Repeat("x", 2100),
		[]byte(`{"grade":"blocked"}`),
		"admin-2",
	)
	if err != nil {
		t.Fatalf("UpdateProviderInstallationQualification returned error: %v", err)
	}
	requireQualifiedProviderInstallation(t, ptrext.Indirect(qualified))
	if _, err := fixture.repository.UpdateProviderInstallationQualification(fixture.ctx, fixture.tenantID, uuid.New(), externalsyncrepo.TestStatusOK, "", []byte(`{}`), "admin-2"); !errors.Is(err, externalsyncrepo.ErrInstallationNotFound) {
		t.Fatalf("missing UpdateProviderInstallationQualification error = %v; want ErrInstallationNotFound", err)
	}
}

func TestRepoProviderInstallationResourceSelectionLifecycle(t *testing.T) {
	fixture := newProviderInstallationFixture(t, "external-sync-provider-installation-selection")

	selected, err := fixture.repository.SelectProviderInstallationResources(
		fixture.ctx,
		fixture.tenantID,
		fixture.installationID,
		[]uuid.UUID{fixture.resources[1].ID, fixture.resources[1].ID},
		"admin-3",
	)
	if err != nil {
		t.Fatalf("SelectProviderInstallationResources returned error: %v", err)
	}
	requireProviderResourceSelection(t, selected, map[uuid.UUID]bool{
		fixture.resources[0].ID: false,
		fixture.resources[1].ID: true,
	})
	loaded, err := fixture.repository.GetProviderInstallation(fixture.ctx, fixture.tenantID, fixture.installationID)
	if err != nil {
		t.Fatalf("GetProviderInstallation after selection returned error: %v", err)
	}
	if loaded.ResourceSelection != externalsyncrepo.ResourceSelectionSelected || loaded.UpdatedBy != "admin-3" {
		t.Fatalf("selection installation = %#v; want selected by admin-3", loaded)
	}
	if _, err := fixture.repository.SelectProviderInstallationResources(fixture.ctx, fixture.tenantID, fixture.installationID, []uuid.UUID{uuid.New()}, "admin-3"); !errors.Is(err, externalsyncrepo.ErrResourceNotFound) {
		t.Fatalf("missing SelectProviderInstallationResources resource error = %v; want ErrResourceNotFound", err)
	}

	selected, err = fixture.repository.SelectProviderInstallationResources(fixture.ctx, fixture.tenantID, fixture.installationID, nil, "admin-4")
	if err != nil {
		t.Fatalf("SelectProviderInstallationResources none returned error: %v", err)
	}
	requireProviderResourceSelection(t, selected, map[uuid.UUID]bool{
		fixture.resources[0].ID: false,
		fixture.resources[1].ID: false,
	})
	loaded, err = fixture.repository.GetProviderInstallation(fixture.ctx, fixture.tenantID, fixture.installationID)
	if err != nil {
		t.Fatalf("GetProviderInstallation after none selection returned error: %v", err)
	}
	if loaded.ResourceSelection != externalsyncrepo.ResourceSelectionNone {
		t.Fatalf("resource selection = %q; want none", loaded.ResourceSelection)
	}
	if _, err := fixture.repository.ListProviderInstallationResources(fixture.ctx, fixture.tenantID, uuid.New()); !errors.Is(err, externalsyncrepo.ErrInstallationNotFound) {
		t.Fatalf("missing ListProviderInstallationResources error = %v; want ErrInstallationNotFound", err)
	}
}

func TestRepoProviderInstallationDeleteLifecycle(t *testing.T) {
	fixture := newProviderInstallationFixture(t, "external-sync-provider-installation-delete")

	if err := fixture.repository.DeleteProviderInstallation(fixture.ctx, fixture.tenantID, fixture.installationID, "admin-5"); err != nil {
		t.Fatalf("DeleteProviderInstallation returned error: %v", err)
	}
	if err := fixture.repository.DeleteProviderInstallation(fixture.ctx, fixture.tenantID, fixture.installationID, "admin-5"); !errors.Is(err, externalsyncrepo.ErrInstallationNotFound) {
		t.Fatalf("second DeleteProviderInstallation error = %v; want ErrInstallationNotFound", err)
	}
	if _, err := fixture.repository.GetProviderInstallation(fixture.ctx, fixture.tenantID, fixture.installationID); !errors.Is(err, externalsyncrepo.ErrInstallationNotFound) {
		t.Fatalf("GetProviderInstallation deleted error = %v; want ErrInstallationNotFound", err)
	}
	if _, err := fixture.repository.ListProviderInstallationResources(fixture.ctx, fixture.tenantID, fixture.installationID); !errors.Is(err, externalsyncrepo.ErrInstallationNotFound) {
		t.Fatalf("ListProviderInstallationResources deleted error = %v; want ErrInstallationNotFound", err)
	}
}

type providerInstallationFixture struct {
	ctx            context.Context
	repository     *externalsyncrepo.Repo
	tenantID       string
	installationID uuid.UUID
	installation   externalsyncrepo.ProviderInstallation
	resources      []externalsyncrepo.ProviderInstallationResource
}

func newProviderInstallationFixture(t *testing.T, slug string) providerInstallationFixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, slug)
	installationID := uuid.New()
	now := time.Date(2026, 7, 8, 1, 0, 0, 0, time.UTC)

	installation, resources, err := repository.CreateProviderInstallation(ctx, externalsyncrepo.ProviderInstallationWithResources{
		Installation: externalsyncrepo.ProviderInstallation{
			ID:                     installationID,
			TenantID:               tenantID,
			Provider:               "github",
			DisplayName:            "GitHub App",
			InstallationKind:       externalsyncrepo.InstallationKindGitHubApp,
			Status:                 externalsyncrepo.InstallationStatusActive,
			ExternalInstallationID: "12345",
			AccountLogin:           "acme",
			AccountID:              "42",
			AccountURL:             "https://github.com/acme",
			BaseURL:                "https://api.github.com",
			Permissions:            []byte(`{"metadata":"read","issues":"write"}`),
			CapabilityProfile:      []byte(`{}`),
			ResourceSelection:      externalsyncrepo.ResourceSelectionSelected,
			QualificationStatus:    externalsyncrepo.TestStatusUntested,
			CreatedBy:              "admin-1",
			UpdatedBy:              "admin-1",
		},
		Resources: []externalsyncrepo.ProviderInstallationResource{
			{
				ID:                 uuid.New(),
				TenantID:           tenantID,
				InstallationID:     installationID,
				Provider:           "github",
				ResourceType:       externalsyncrepo.ResourceTypeRepository,
				ExternalResourceID: "1001",
				ResourceKey:        "acme/attune",
				DisplayName:        "acme/attune",
				HTMLURL:            "https://github.com/acme/attune",
				Selected:           true,
				Status:             externalsyncrepo.ResourceStatusActive,
				Permissions:        []byte(`{"issues":"write"}`),
				LastSeenAt:         ptrext.Of(now),
			},
			{
				ID:                 uuid.New(),
				TenantID:           tenantID,
				InstallationID:     installationID,
				Provider:           "github",
				ResourceType:       externalsyncrepo.ResourceTypeRepository,
				ExternalResourceID: "1002",
				ResourceKey:        "acme/website",
				DisplayName:        "acme/website",
				HTMLURL:            "https://github.com/acme/website",
				Selected:           true,
				Status:             externalsyncrepo.ResourceStatusActive,
				Permissions:        []byte(`{"issues":"write"}`),
				LastSeenAt:         ptrext.Of(now),
			},
		},
	})
	if err != nil {
		t.Fatalf("CreateProviderInstallation fixture returned error: %v", err)
	}

	return providerInstallationFixture{
		ctx:            ctx,
		repository:     repository,
		tenantID:       tenantID,
		installationID: installationID,
		installation:   ptrext.Indirect(installation),
		resources:      resources,
	}
}

func requireDuplicateProviderInstallationConflict(t *testing.T, fixture providerInstallationFixture) {
	t.Helper()
	_, _, err := fixture.repository.CreateProviderInstallation(fixture.ctx, externalsyncrepo.ProviderInstallationWithResources{
		Installation: externalsyncrepo.ProviderInstallation{
			ID:                     uuid.New(),
			TenantID:               fixture.tenantID,
			Provider:               "github",
			DisplayName:            "GitHub Duplicate",
			InstallationKind:       externalsyncrepo.InstallationKindGitHubApp,
			Status:                 externalsyncrepo.InstallationStatusActive,
			ExternalInstallationID: "12345",
			Permissions:            []byte(`{}`),
			CapabilityProfile:      []byte(`{}`),
			ResourceSelection:      externalsyncrepo.ResourceSelectionNone,
			QualificationStatus:    externalsyncrepo.TestStatusUntested,
			CreatedBy:              "admin-1",
			UpdatedBy:              "admin-1",
		},
	})
	if !errors.Is(err, externalsyncrepo.ErrConflict) {
		t.Fatalf("duplicate CreateProviderInstallation error = %v; want ErrConflict", err)
	}
}

func requireQualifiedProviderInstallation(t *testing.T, installation externalsyncrepo.ProviderInstallation) {
	t.Helper()
	if installation.QualificationStatus != externalsyncrepo.TestStatusFailed {
		t.Fatalf("qualification status = %q; want failed", installation.QualificationStatus)
	}
	if installation.LastQualifiedAt == nil {
		t.Fatal("last qualified at = nil; want timestamp")
	}
	if len(installation.LastError) != 2000 {
		t.Fatalf("last error length = %d; want 2000", len(installation.LastError))
	}
	if installation.UpdatedBy != "admin-2" {
		t.Fatalf("updated by = %q; want admin-2", installation.UpdatedBy)
	}
}

func TestRepoFailureRetryAndConflictResolution(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-conflict")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-conflict")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-conflict")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	run := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)

	failureID := insertExternalSyncRecordFailure(t, ctx, pool, tenantID, run.ID, mapping.ID)

	retriedFailure, err := repository.RetryFailure(ctx, tenantID, failureID, "admin-1")
	if err != nil {
		t.Fatalf("RetryFailure returned error: %v", err)
	}
	requireRetriedExternalSyncFailure(t, ptrext.Indirect(retriedFailure))
	if got := countExternalSyncRetryRuns(t, ctx, pool, tenantID, mapping.ID); got != 1 {
		t.Fatalf("retry runs = %d; want one queued retry run", got)
	}
	if _, err := repository.RetryFailure(ctx, tenantID, failureID, "admin-1"); !errors.Is(err, externalsyncrepo.ErrFailureNotFound) {
		t.Fatalf("second RetryFailure error = %v; want ErrFailureNotFound", err)
	}

	conflictID := insertExternalSyncConflict(t, ctx, pool, tenantID, mapping.ID)

	health, err := repository.Health(ctx, tenantID)
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if health.OpenConflicts != 1 {
		t.Fatalf("open conflicts = %d; want 1", health.OpenConflicts)
	}

	resolved, err := repository.ResolveConflict(ctx, tenantID, conflictID, "local_wins", "admin-1")
	if err != nil {
		t.Fatalf("ResolveConflict returned error: %v", err)
	}
	requireResolvedExternalSyncConflict(t, ptrext.Indirect(resolved))
	if err := repository.RecordAttempt(ctx, externalsyncrepo.AttemptInput{
		RunID:             run.ID,
		AttemptNumber:     1,
		Result:            "failed",
		HTTPStatus:        502,
		ProviderRequestID: "github-retry-1",
		ErrorKind:         "provider_unavailable",
		ErrorMessage:      "temporary outage",
	}); err != nil {
		t.Fatalf("RecordAttempt for detail returned error: %v", err)
	}

	detail, err := repository.GetRunDetail(ctx, tenantID, run.ID)
	if err != nil {
		t.Fatalf("GetRunDetail returned error: %v", err)
	}
	requireExternalSyncRunDetail(t, ptrext.Indirect(detail), failureID, conflictID)

	ignoredID := insertExternalSyncConflict(t, ctx, pool, tenantID, mapping.ID)
	ignored, err := repository.ResolveConflict(ctx, tenantID, ignoredID, "ignored", "admin-1")
	if err != nil {
		t.Fatalf("ResolveConflict ignored returned error: %v", err)
	}
	if ignored.Status != "ignored" || ignored.Resolution != "ignored" {
		t.Fatalf("ignored conflict = %#v; want ignored", ignored)
	}
	if _, err := repository.ResolveConflict(ctx, tenantID, uuid.New(), "local_wins", "admin-1"); !errors.Is(err, externalsyncrepo.ErrConflictNotFound) {
		t.Fatalf("missing ResolveConflict error = %v; want ErrConflictNotFound", err)
	}
	if _, err := repository.RetryRun(ctx, tenantID, uuid.New()); !errors.Is(err, externalsyncrepo.ErrRunNotFound) {
		t.Fatalf("missing RetryRun error = %v; want ErrRunNotFound", err)
	}
}

func TestRepoHealthDetectsDegradedConnections(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-degraded")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-degraded")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-degraded")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)

	insertTerminalExternalSyncRun(t, ctx, pool, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.RunStatusFailed, time.Date(2026, 7, 8, 1, 0, 0, 0, time.UTC))
	insertTerminalExternalSyncRun(t, ctx, pool, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.RunStatusDead, time.Date(2026, 7, 8, 2, 0, 0, 0, time.UTC))
	insertTerminalExternalSyncRun(t, ctx, pool, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.RunStatusFailed, time.Date(2026, 7, 8, 3, 0, 0, 0, time.UTC))

	health, err := repository.Health(ctx, tenantID)
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if health.DegradedConnections != 1 {
		t.Fatalf("degraded connections = %d; want 1", health.DegradedConnections)
	}

	insertTerminalExternalSyncRun(t, ctx, pool, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.RunStatusSucceeded, time.Date(2026, 7, 8, 4, 0, 0, 0, time.UTC))
	health, err = repository.Health(ctx, tenantID)
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if health.DegradedConnections != 0 {
		t.Fatalf("degraded connections after success = %d; want 0", health.DegradedConnections)
	}
}

func TestRepoQuarantinesDegradedConnectionAndSkipsClaims(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-quarantine")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-quarantine")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-quarantine")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)

	insertTerminalExternalSyncRun(t, ctx, pool, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.RunStatusFailed, time.Date(2026, 7, 8, 1, 0, 0, 0, time.UTC))
	insertTerminalExternalSyncRun(t, ctx, pool, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.RunStatusDead, time.Date(2026, 7, 8, 2, 0, 0, 0, time.UTC))
	insertTerminalExternalSyncRun(t, ctx, pool, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.RunStatusFailed, time.Date(2026, 7, 8, 3, 0, 0, 0, time.UTC))

	affected, err := repository.QuarantineDegradedConnection(ctx, tenantID, conn.ID, "provider_unavailable: repeated failures")
	if err != nil {
		t.Fatalf("QuarantineDegradedConnection returned error: %v", err)
	}
	requireRowsAffected(t, "QuarantineDegradedConnection", affected)
	requireExternalConnectionQuarantined(t, ctx, pool, tenantID, conn.ID, "provider_unavailable")

	health, err := repository.Health(ctx, tenantID)
	if err != nil {
		t.Fatalf("Health returned error: %v", err)
	}
	if health.QuarantinedConnections != 1 {
		t.Fatalf("quarantined connections = %d; want 1", health.QuarantinedConnections)
	}
	if health.DegradedConnections != 0 {
		t.Fatalf("degraded connections = %d; want 0 after quarantine", health.DegradedConnections)
	}

	run := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	claimed := claimExternalSyncRuns(t, ctx, repository, 1, "worker-quarantine")
	if len(claimed) != 0 {
		t.Fatalf("claimed runs = %#v; want none for quarantined connection run %s", claimed, run.ID)
	}
}

func TestRepoResumeConnectionAndBatchResolveConflicts(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-governance")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-governance")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-governance")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)

	mustExec(t, ctx, pool, `
		UPDATE external_connections
		   SET enabled = FALSE,
		       status = 'quarantined',
		       last_error = 'provider_unavailable: repeated failures'
		 WHERE tenant_id = $1
		   AND id = $2`,
		tenantID, conn.ID)
	resumed, err := repository.ResumeConnection(ctx, tenantID, conn.ID, "admin-1")
	if err != nil {
		t.Fatalf("ResumeConnection returned error: %v", err)
	}
	if !ptrext.Indirect(resumed).Enabled || ptrext.Indirect(resumed).Status != externalsyncrepo.ConnectionStatusActive {
		t.Fatalf("resumed connection = %#v; want enabled active", resumed)
	}
	requireExternalConnectionActive(t, ctx, pool, tenantID, conn.ID)

	conflictID := insertExternalSyncConflict(t, ctx, pool, tenantID, mapping.ID)
	otherID := insertExternalSyncConflict(t, ctx, pool, tenantID, mapping.ID)
	result, err := repository.ResolveConflicts(ctx, tenantID, []uuid.UUID{conflictID, otherID}, "external_wins", "admin-1")
	if err != nil {
		t.Fatalf("ResolveConflicts returned error: %v", err)
	}
	if len(result.Conflicts) != 2 {
		t.Fatalf("resolved conflicts = %#v; want two rows", result.Conflicts)
	}
	requireExternalSyncConflictsResolved(t, ctx, pool, tenantID, []uuid.UUID{conflictID, otherID}, "external_wins")
	ignoredID := insertExternalSyncConflict(t, ctx, pool, tenantID, mapping.ID)
	ignoredResult, err := repository.ResolveConflicts(ctx, tenantID, []uuid.UUID{ignoredID}, "ignored", "admin-1")
	if err != nil {
		t.Fatalf("ResolveConflicts ignored returned error: %v", err)
	}
	if len(ignoredResult.Conflicts) != 1 || ignoredResult.Conflicts[0].Status != "ignored" {
		t.Fatalf("ignored conflicts = %#v; want ignored row", ignoredResult.Conflicts)
	}
}

func TestRepoMetricSnapshotReportsLagAndDeadRuns(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-metrics")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-metrics")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-metrics")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)

	now := time.Now().UTC()
	insertTerminalExternalSyncRun(t, ctx, pool, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.RunStatusDead, now.Add(-10*time.Minute))
	success := insertTerminalExternalSyncRun(t, ctx, pool, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.RunStatusSucceeded, now.Add(-5*time.Minute))
	mustExec(t, ctx, pool, `
		UPDATE external_sync_runs
		   SET finished_at = $2,
		       updated_at = $2
		 WHERE id = $1`,
		success.ID, now.Add(-5*time.Minute))

	snapshot, err := repository.MetricSnapshot(ctx)
	if err != nil {
		t.Fatalf("MetricSnapshot returned error: %v", err)
	}
	var found *externalsyncrepo.MetricPoint
	for i := range snapshot.Points {
		if snapshot.Points[i].Provider == "github" && snapshot.Points[i].ExternalObjectType == "issue" {
			found = &snapshot.Points[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("metric snapshot points = %#v; want github issue point", snapshot.Points)
	}
	if found.DeadRuns != 1 {
		t.Fatalf("dead runs = %d; want 1", found.DeadRuns)
	}
	if found.LagSeconds <= 0 {
		t.Fatalf("lag seconds = %f; want positive lag", found.LagSeconds)
	}
}

func TestRepoListRunsFiltersAndPaginates(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-list-runs")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-list-runs")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-list-runs")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	otherConn := createExternalSyncConnectionNamed(t, ctx, repository, tenantID, "kid-external-sync-list-runs", "GitHub Other")
	otherMapping := firstExternalSyncMapping(t, ctx, repository, tenantID, otherConn.ID)

	oldRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	newRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	otherRun := insertExternalSyncRun(t, ctx, repository, tenantID, otherConn.ID, otherMapping.ID)
	setExternalSyncRunForList(t, ctx, pool, oldRun.ID, externalsyncrepo.RunStatusFailed, time.Date(2026, 7, 8, 1, 0, 0, 0, time.UTC))
	setExternalSyncRunForList(t, ctx, pool, newRun.ID, externalsyncrepo.RunStatusQueued, time.Date(2026, 7, 8, 2, 0, 0, 0, time.UTC))
	setExternalSyncRunForList(t, ctx, pool, otherRun.ID, externalsyncrepo.RunStatusQueued, time.Date(2026, 7, 8, 3, 0, 0, 0, time.UTC))

	firstPage, err := repository.ListRuns(ctx, externalsyncrepo.ListRunsFilter{
		TenantID:     tenantID,
		ConnectionID: ptrext.Of(conn.ID),
		Limit:        1,
	})
	if err != nil {
		t.Fatalf("ListRuns first page returned error: %v", err)
	}
	if len(firstPage.Runs) != 1 || firstPage.Runs[0].ID != newRun.ID || firstPage.NextBeforeID != newRun.ID.String() {
		t.Fatalf("first page = %#v; want newest selected connection run with next cursor", firstPage)
	}

	secondPage, err := repository.ListRuns(ctx, externalsyncrepo.ListRunsFilter{
		TenantID:     tenantID,
		ConnectionID: ptrext.Of(conn.ID),
		BeforeID:     ptrext.Of(newRun.ID),
		Limit:        2,
	})
	if err != nil {
		t.Fatalf("ListRuns second page returned error: %v", err)
	}
	if len(secondPage.Runs) != 1 || secondPage.Runs[0].ID != oldRun.ID || secondPage.NextBeforeID != "" {
		t.Fatalf("second page = %#v; want older selected connection run without next cursor", secondPage)
	}

	failedRuns, err := repository.ListRuns(ctx, externalsyncrepo.ListRunsFilter{
		TenantID:  tenantID,
		MappingID: ptrext.Of(mapping.ID),
		Status:    externalsyncrepo.RunStatusFailed,
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListRuns failed filter returned error: %v", err)
	}
	if len(failedRuns.Runs) != 1 || failedRuns.Runs[0].ID != oldRun.ID {
		t.Fatalf("failed filtered runs = %#v; want old failed run", failedRuns)
	}
	if _, err := repository.ListRuns(ctx, externalsyncrepo.ListRunsFilter{
		TenantID: tenantID,
		BeforeID: ptrext.Of(uuid.New()),
		Limit:    1,
	}); !errors.Is(err, externalsyncrepo.ErrRunNotFound) {
		t.Fatalf("missing ListRuns cursor error = %v; want ErrRunNotFound", err)
	}
}

type externalSyncEventFixture struct {
	ctx        context.Context
	pool       *pgxpool.Pool
	repository *externalsyncrepo.Repo
	tenantID   string
	conn       externalsyncrepo.Connection
	mapping    externalsyncrepo.Mapping
	event      externalsyncrepo.SyncEvent
}

func newExternalSyncEventFixture(t *testing.T, slug string) externalSyncEventFixture {
	t.Helper()
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, slug)
	insertExternalSyncKey(t, ctx, pool, "kid-"+slug)
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-"+slug)
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	return externalSyncEventFixture{
		ctx:        ctx,
		pool:       pool,
		repository: repository,
		tenantID:   tenantID,
		conn:       conn,
		mapping:    mapping,
		event: externalsyncrepo.SyncEvent{
			ID:                uuid.New(),
			TenantID:          tenantID,
			ConnectionID:      conn.ID,
			MappingID:         ptrext.Of(mapping.ID),
			Provider:          "github",
			EventType:         "issues",
			ExternalEventID:   "delivery-1",
			DedupeKey:         "github:issues:delivery-1",
			SignatureStatus:   externalsyncrepo.EventSignatureVerified,
			Status:            externalsyncrepo.EventStatusReceived,
			PayloadDigest:     strings.Repeat("a", 64),
			NormalizedPayload: []byte(`{"action":"opened","issue":42}`),
			ReceivedAt:        time.Date(2026, 7, 8, 2, 5, 0, 0, time.UTC),
		},
	}
}

func hasExternalSyncEvent(events []externalsyncrepo.SyncEvent, id uuid.UUID) bool {
	for _, event := range events {
		if event.ID == id {
			return true
		}
	}
	return false
}

func TestRepoRecordAndListEventLifecycle(t *testing.T) {
	fixture := newExternalSyncEventFixture(t, "external-sync-events-list")

	recorded, err := fixture.repository.RecordEvent(fixture.ctx, fixture.event)
	if err != nil {
		t.Fatalf("RecordEvent returned error: %v", err)
	}
	duplicate := fixture.event
	duplicate.ID = uuid.New()
	duplicate.NormalizedPayload = []byte(`{"action":"edited","issue":42}`)
	recordedAgain, err := fixture.repository.RecordEvent(fixture.ctx, duplicate)
	if err != nil {
		t.Fatalf("duplicate RecordEvent returned error: %v", err)
	}
	if recordedAgain.ID != recorded.ID {
		t.Fatalf("duplicate event id = %s; want original %s", recordedAgain.ID, recorded.ID)
	}
	laterEvent := fixture.event
	laterEvent.ID = uuid.New()
	laterEvent.ExternalEventID = "delivery-2"
	laterEvent.DedupeKey = "github:issues:delivery-2"
	laterEvent.ReceivedAt = fixture.event.ReceivedAt.Add(time.Minute)
	if _, err := fixture.repository.RecordEvent(fixture.ctx, laterEvent); err != nil {
		t.Fatalf("second RecordEvent returned error: %v", err)
	}
	received, err := fixture.repository.ListEvents(fixture.ctx, externalsyncrepo.ListEventsFilter{
		TenantID:     fixture.tenantID,
		ConnectionID: ptrext.Of(fixture.conn.ID),
		Status:       externalsyncrepo.EventStatusReceived,
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("ListEvents returned error: %v", err)
	}
	if len(received.Events) != 2 || !hasExternalSyncEvent(received.Events, recorded.ID) {
		t.Fatalf("received events = %#v; want recorded events", received.Events)
	}
}

func TestRepoEventPaginationAndLookupErrors(t *testing.T) {
	fixture := newExternalSyncEventFixture(t, "external-sync-events-pages")
	if _, err := fixture.repository.RecordEvent(fixture.ctx, fixture.event); err != nil {
		t.Fatalf("RecordEvent returned error: %v", err)
	}
	laterEvent := fixture.event
	laterEvent.ID = uuid.New()
	laterEvent.ExternalEventID = "delivery-2"
	laterEvent.DedupeKey = "github:issues:delivery-2"
	laterEvent.ReceivedAt = fixture.event.ReceivedAt.Add(time.Minute)
	recordedLater, err := fixture.repository.RecordEvent(fixture.ctx, laterEvent)
	if err != nil {
		t.Fatalf("second RecordEvent returned error: %v", err)
	}
	firstPage, err := fixture.repository.ListEvents(fixture.ctx, externalsyncrepo.ListEventsFilter{
		TenantID: fixture.tenantID,
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("ListEvents first page returned error: %v", err)
	}
	if len(firstPage.Events) != 1 || firstPage.NextBeforeID == "" {
		t.Fatalf("first event page = %#v; want one row and before cursor", firstPage)
	}
	secondPage, err := fixture.repository.ListEvents(fixture.ctx, externalsyncrepo.ListEventsFilter{
		TenantID: fixture.tenantID,
		BeforeID: ptrext.Of(uuid.MustParse(
			firstPage.NextBeforeID,
		)),
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListEvents second page returned error: %v", err)
	}
	if len(secondPage.Events) != 1 {
		t.Fatalf("second event page = %#v; want older row", secondPage)
	}
	gotEvent, err := fixture.repository.GetEvent(fixture.ctx, fixture.tenantID, recordedLater.ID)
	if err != nil {
		t.Fatalf("GetEvent returned error: %v", err)
	}
	if gotEvent.ID != recordedLater.ID {
		t.Fatalf("GetEvent = %#v; want %s", gotEvent, recordedLater.ID)
	}
	if _, err := fixture.repository.GetEvent(fixture.ctx, fixture.tenantID, uuid.New()); !errors.Is(err, externalsyncrepo.ErrEventNotFound) {
		t.Fatalf("missing GetEvent error = %v; want ErrEventNotFound", err)
	}
	if _, err := fixture.repository.ListEvents(fixture.ctx, externalsyncrepo.ListEventsFilter{
		TenantID: fixture.tenantID,
		BeforeID: ptrext.Of(uuid.New()),
		Limit:    1,
	}); !errors.Is(err, externalsyncrepo.ErrEventNotFound) {
		t.Fatalf("missing ListEvents cursor error = %v; want ErrEventNotFound", err)
	}
	if _, _, err := fixture.repository.ReplayEvent(fixture.ctx, fixture.tenantID, recordedLater.ID, "admin-1", uuid.New(), externalsyncrepo.DirectionPull); err == nil {
		t.Fatal("ReplayEvent with missing mapping returned nil error")
	}
}

func TestRepoReplayEventLifecycle(t *testing.T) {
	fixture := newExternalSyncEventFixture(t, "external-sync-events-replay")

	recorded, err := fixture.repository.RecordEvent(fixture.ctx, fixture.event)
	if err != nil {
		t.Fatalf("RecordEvent returned error: %v", err)
	}

	replayed, run, err := fixture.repository.ReplayEvent(fixture.ctx, fixture.tenantID, recorded.ID, "admin-1", fixture.mapping.ID, externalsyncrepo.DirectionPull)
	if err != nil {
		t.Fatalf("ReplayEvent returned error: %v", err)
	}
	if replayed.Status != externalsyncrepo.EventStatusReplayed || replayed.RunID == nil || ptrext.Indirect(replayed.RunID) != run.ID {
		t.Fatalf("replayed event = %#v run = %#v; want linked replay run", replayed, run)
	}
	if run.Trigger != externalsyncrepo.TriggerWebhook || run.Direction != externalsyncrepo.DirectionPull {
		t.Fatalf("replay run = %#v; want webhook pull run", run)
	}
	if _, _, err := fixture.repository.ReplayEvent(fixture.ctx, fixture.tenantID, recorded.ID, "admin-1", fixture.mapping.ID, externalsyncrepo.DirectionPull); !errors.Is(err, externalsyncrepo.ErrConflict) {
		t.Fatalf("second ReplayEvent error = %v; want ErrConflict", err)
	}
	if _, _, err := fixture.repository.ReplayEvent(fixture.ctx, fixture.tenantID, uuid.New(), "admin-1", fixture.mapping.ID, externalsyncrepo.DirectionPull); !errors.Is(err, externalsyncrepo.ErrEventNotFound) {
		t.Fatalf("missing ReplayEvent error = %v; want ErrEventNotFound", err)
	}
}

func TestRepoMalformedListRowsReturnScanErrors(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-malformed-lists")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-malformed-lists")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-malformed-lists")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	run := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	event := closedPoolSyncEvent(tenantID, conn.ID, mapping.ID)
	if _, err := repository.RecordEvent(ctx, event); err != nil {
		t.Fatalf("RecordEvent seed returned error: %v", err)
	}
	if _, err := repository.GetRunDetail(ctx, tenantID, uuid.New()); !errors.Is(err, externalsyncrepo.ErrRunNotFound) {
		t.Fatalf("missing GetRunDetail error = %v; want ErrRunNotFound", err)
	}

	mustExec(t, ctx, pool, `ALTER TABLE external_connections ALTER COLUMN created_at TYPE text USING created_at::text`)
	if _, err := repository.ListConnections(ctx, tenantID); err == nil {
		t.Fatal("ListConnections malformed row returned nil error")
	}
	mustExec(t, ctx, pool, `ALTER TABLE external_object_mappings ALTER COLUMN created_at TYPE text USING created_at::text`)
	if _, err := repository.ListMappings(ctx, tenantID, conn.ID); err == nil {
		t.Fatal("ListMappings malformed row returned nil error")
	}
	mustExec(t, ctx, pool, `ALTER TABLE external_sync_runs ALTER COLUMN created_at TYPE text USING created_at::text`)
	if _, err := repository.ListRuns(ctx, externalsyncrepo.ListRunsFilter{TenantID: tenantID}); err == nil {
		t.Fatal("ListRuns malformed row returned nil error")
	}
	mustExec(t, ctx, pool, `ALTER TABLE external_sync_events ALTER COLUMN created_at TYPE text USING created_at::text`)
	if _, err := repository.ListEvents(ctx, externalsyncrepo.ListEventsFilter{TenantID: tenantID}); err == nil {
		t.Fatal("ListEvents malformed row returned nil error")
	}
	if run.ID == uuid.Nil {
		t.Fatal("seed run id is nil")
	}
}

func TestRepoMalformedDetailCollectionsReturnEmptySlices(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-malformed-detail")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-malformed-detail")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-malformed-detail")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	run := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	if err := repository.RecordAttempt(ctx, externalsyncrepo.AttemptInput{RunID: run.ID, Result: "failed"}); err != nil {
		t.Fatalf("RecordAttempt seed returned error: %v", err)
	}
	_ = insertExternalSyncRecordFailure(t, ctx, pool, tenantID, run.ID, mapping.ID)
	_ = insertExternalSyncConflict(t, ctx, pool, tenantID, mapping.ID)

	mustExec(t, ctx, pool, `ALTER TABLE external_sync_attempts ALTER COLUMN started_at TYPE text USING started_at::text`)
	mustExec(t, ctx, pool, `ALTER TABLE external_sync_record_failures ALTER COLUMN created_at TYPE text USING created_at::text`)
	mustExec(t, ctx, pool, `ALTER TABLE external_sync_conflicts ALTER COLUMN created_at TYPE text USING created_at::text`)
	detail, err := repository.GetRunDetail(ctx, tenantID, run.ID)
	if err != nil {
		t.Fatalf("GetRunDetail returned error: %v", err)
	}
	if len(detail.Attempts) != 0 || len(detail.Failures) != 0 || len(detail.Conflicts) != 0 {
		t.Fatalf("malformed detail collections = attempts:%#v failures:%#v conflicts:%#v; want empty slices",
			detail.Attempts, detail.Failures, detail.Conflicts)
	}
}

func TestRepoApplyErrorBranchesFromSchemaFailures(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-apply-schema-errors")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-apply-schema-errors")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-apply-schema-errors")
	mapping := updateExternalSyncMappingDirection(t, ctx, repository, tenantID, conn.ID, externalsyncrepo.DirectionPush)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "External sync schema errors")
	pushRun := insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.DirectionPush)
	claimed := claimExternalSyncRuns(t, ctx, repository, 1, "worker-schema")
	requireClaimedExternalSyncRun(t, claimed, pushRun.ID, 1, "worker-schema")
	records, err := repository.PreparePushRecords(ctx, claimed[0].ID, "worker-schema", tenantID, mapping.ID, conn.Provider, 100)
	if err != nil {
		t.Fatalf("PreparePushRecords returned error: %v", err)
	}
	if len(records) != 1 || records[0].LocalObjectID != requestID.String() || records[0].ExternalKey != "" {
		t.Fatalf("prepared schema-error records = %#v; want create record for request %s", records, requestID)
	}
	pullRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	pushStatsRun := insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.DirectionPush)
	missingResultRun := insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.DirectionPush)

	mustExec(t, ctx, pool, `ALTER TABLE customer_requests RENAME COLUMN archived_at TO archived_at_broken`)
	if _, err := repository.PreparePushRecords(ctx, claimed[0].ID, "worker-schema", tenantID, mapping.ID, conn.Provider, 10); err == nil {
		t.Fatal("PreparePushRecords with malformed customer_requests returned nil error")
	}
	_, err = repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{
		TenantID:     tenantID,
		RunID:        claimed[0].ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
		Records:      records,
		Results: []externalsyncrepo.PushResult{{
			LocalObjectID:   requestID.String(),
			ExternalKey:     "99",
			ExternalURL:     "https://github.example.test/acme/app/issues/99",
			ExternalVersion: "v1",
		}},
	})
	if err == nil {
		t.Fatal("ApplyPushResult with malformed customer_requests returned nil error")
	}

	mustExec(t, ctx, pool, `ALTER TABLE external_sync_record_failures RENAME COLUMN operation TO operation_broken`)
	if _, err := repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{
		TenantID:     tenantID,
		RunID:        missingResultRun.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
		Records:      records,
	}); err == nil {
		t.Fatal("ApplyPushResult with malformed failures table returned nil error")
	}

	mustExec(t, ctx, pool, `ALTER TABLE external_sync_runs RENAME COLUMN records_seen TO records_seen_broken`)
	if _, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        pullRun.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
		StreamKey:    externalsyncrepo.StreamDefault,
	}); err == nil {
		t.Fatal("ApplyPullResult with malformed runs table returned nil error")
	}
	if _, err := repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{
		TenantID:     tenantID,
		RunID:        pushStatsRun.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
	}); err == nil {
		t.Fatal("ApplyPushResult stats update with malformed runs table returned nil error")
	}
}

func TestRepoClosedPoolReturnsDatabaseErrors(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := uuid.NewString()
	id := uuid.New()
	mappingID := uuid.New()
	pool.Close()

	cases := []struct {
		name string
		run  func() error
	}{
		{"ListConnections", func() error { _, err := repository.ListConnections(ctx, tenantID); return err }},
		{"GetConnection", func() error { _, err := repository.GetConnection(ctx, tenantID, id); return err }},
		{"ListMappings", func() error { _, err := repository.ListMappings(ctx, tenantID, id); return err }},
		{"GetMapping", func() error { _, err := repository.GetMapping(ctx, tenantID, mappingID); return err }},
		{"ResolveRunMapping", func() error {
			_, err := repository.ResolveRunMapping(ctx, tenantID, id, ptrext.Of(mappingID))
			return err
		}},
		{"UpdateMapping", func() error {
			_, err := repository.UpdateMapping(ctx, externalsyncrepo.Mapping{
				ID:              mappingID,
				TenantID:        tenantID,
				Direction:       externalsyncrepo.DirectionPull,
				FieldMapping:    []byte("{}"),
				StatusMapping:   []byte("{}"),
				ConflictPolicy:  "manual",
				TombstonePolicy: "mark_stale",
				Enabled:         true,
			})
			return err
		}},
		{"ResetCursor", func() error { _, err := repository.ResetCursor(ctx, tenantID, mappingID, "admin-1"); return err }},
		{"EnqueueBackfill", func() error {
			_, err := repository.EnqueueBackfill(ctx, tenantID, mappingID, "admin-1", true)
			return err
		}},
		{"InsertRun", func() error {
			_, err := repository.InsertRun(ctx, externalsyncrepo.SyncRun{
				ID:           id,
				TenantID:     tenantID,
				ConnectionID: id,
				MappingID:    ptrext.Of(mappingID),
				Direction:    externalsyncrepo.DirectionPull,
				Trigger:      externalsyncrepo.TriggerManual,
				ActorID:      "admin-1",
			})
			return err
		}},
		{"ListRuns", func() error {
			_, err := repository.ListRuns(ctx, externalsyncrepo.ListRunsFilter{TenantID: tenantID})
			return err
		}},
		{"RecordEvent", func() error {
			_, err := repository.RecordEvent(ctx, closedPoolSyncEvent(tenantID, id, mappingID))
			return err
		}},
		{"ListEvents", func() error {
			_, err := repository.ListEvents(ctx, externalsyncrepo.ListEventsFilter{TenantID: tenantID})
			return err
		}},
		{"GetEvent", func() error { _, err := repository.GetEvent(ctx, tenantID, id); return err }},
		{"ReplayEvent", func() error {
			_, _, err := repository.ReplayEvent(ctx, tenantID, id, "admin-1", mappingID, externalsyncrepo.DirectionPull)
			return err
		}},
		{"RecordTimeline", func() error {
			_, err := repository.RecordTimeline(ctx, externalsyncrepo.RecordTimelineFilter{TenantID: tenantID, MappingID: mappingID})
			return err
		}},
		{"PrepareRunCursor", func() error {
			_, err := repository.PrepareRunCursor(ctx, id, "worker-1", tenantID, mappingID, externalsyncrepo.StreamDefault)
			return err
		}},
		{"ApplyPullResult", func() error {
			_, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{TenantID: tenantID, RunID: id, ConnectionID: id, MappingID: mappingID})
			return err
		}},
		{"PreparePushRecords", func() error {
			_, err := repository.PreparePushRecords(ctx, id, "worker-1", tenantID, mappingID, "github", 10)
			return err
		}},
		{"ApplyPushResult", func() error {
			_, err := repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{TenantID: tenantID, RunID: id, ConnectionID: id, MappingID: mappingID})
			return err
		}},
		{"RecordAttempt", func() error {
			return repository.RecordAttempt(ctx, externalsyncrepo.AttemptInput{RunID: id, Result: "failed"})
		}},
		{"ClaimBatch", func() error { _, err := repository.ClaimBatch(ctx, 1, "worker-1"); return err }},
		{"RefreshRunClaim", func() error { _, err := repository.RefreshRunClaim(ctx, id, "worker-1"); return err }},
		{"MarkRunSucceeded", func() error { _, err := repository.MarkRunSucceeded(ctx, id, "worker-1"); return err }},
		{"MarkRunFailed", func() error {
			_, err := repository.MarkRunFailed(ctx, id, "worker-1", "provider_unavailable", "closed", 0, true)
			return err
		}},
		{"QuarantineDegradedConnection", func() error {
			_, err := repository.QuarantineDegradedConnection(ctx, tenantID, id, "closed")
			return err
		}},
		{"RetryRun", func() error { _, err := repository.RetryRun(ctx, tenantID, id); return err }},
		{"RetryFailure", func() error { _, err := repository.RetryFailure(ctx, tenantID, id, "admin-1"); return err }},
		{"ResolveConflict", func() error {
			_, err := repository.ResolveConflict(ctx, tenantID, id, "local_wins", "admin-1")
			return err
		}},
		{"ResolveConflicts", func() error {
			_, err := repository.ResolveConflicts(ctx, tenantID, []uuid.UUID{id}, "local_wins", "admin-1")
			return err
		}},
		{"Health", func() error { _, err := repository.Health(ctx, tenantID); return err }},
		{"MetricSnapshot", func() error { _, err := repository.MetricSnapshot(ctx); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("closed pool operation returned nil error")
			}
		})
	}
}

func TestRepoApplyPullResultIsIdempotentAndAdvancesCursor(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-apply")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-apply")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-apply")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "External sync idempotency")

	run := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	stats := applyGitHubIssuePullResult(t, ctx, repository, githubIssuePullResultInput{
		tenantID:          tenantID,
		runID:             run.ID,
		connectionID:      conn.ID,
		mappingID:         mapping.ID,
		requestID:         requestID,
		cursorBefore:      []byte(`{"page":1}`),
		cursorAfter:       []byte(`{"page":2}`),
		externalVersion:   "v1",
		externalUpdatedAt: ptrext.Of(time.Date(2026, 7, 8, 4, 5, 6, 0, time.UTC)),
	})
	assertApplyStats(t, stats, externalsyncrepo.ApplyStats{RecordsSeen: 1, RecordsChanged: 1})

	secondRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	stats = applyGitHubIssuePullResult(t, ctx, repository, githubIssuePullResultInput{
		tenantID:        tenantID,
		runID:           secondRun.ID,
		connectionID:    conn.ID,
		mappingID:       mapping.ID,
		requestID:       requestID,
		cursorBefore:    []byte(`{"page":2}`),
		cursorAfter:     []byte(`{"page":2}`),
		externalVersion: "v1",
	})
	assertApplyStats(t, stats, externalsyncrepo.ApplyStats{RecordsSeen: 1})

	if got := countExternalObjectLinks(t, ctx, pool, tenantID, mapping.ID); got != 1 {
		t.Fatalf("external object links = %d; want one idempotent link", got)
	}
	if got := countIssueLinks(t, ctx, pool, tenantID, requestID); got != 1 {
		t.Fatalf("customer request issue links = %d; want one bridged issue link", got)
	}
	assertIssueLinkSyncContext(t, ctx, pool, tenantID, requestID, "ISSUE-1", "open", "octo")
	entries := recordGitHubIssueTimeline(t, ctx, repository, tenantID, mapping.ID, requestID)
	assertTimelineProviderPayload(t, entries, []string{"bug", "customer"}, []string{"octo", "hubot"}, 2)
	if got := externalSyncCursor(t, ctx, pool, tenantID, mapping.ID); got != `{"page": 2}` && got != `{"page":2}` {
		t.Fatalf("cursor = %s; want page 2", got)
	}
	assertApplyPullResultError(t, ctx, repository, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        uuid.New(),
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
	}, externalsyncrepo.ErrRunNotFound)
	assertApplyPullResultError(t, ctx, repository, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        uuid.New(),
		ConnectionID: conn.ID,
		MappingID:    uuid.New(),
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
	}, externalsyncrepo.ErrMappingNotFound)
}

func TestRepoApplyPullResultProjectsDeliveryArtifactChildren(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-delivery-artifact-child")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-delivery-artifact-child")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-delivery-artifact-child")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "External sync delivery artifact")
	run := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	seenAt := time.Date(2026, 7, 8, 5, 30, 0, 0, time.UTC)

	stats, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        run.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
		Records: []externalsyncrepo.PullRecord{{
			LocalObjectID:     requestID.String(),
			ExternalKey:       "ISSUE-1",
			ExternalURL:       "https://github.com/acme/app/issues/1",
			ExternalVersion:   "issue-v1",
			ExternalUpdatedAt: ptrext.Of(seenAt.Add(-time.Hour)),
			Payload:           []byte(`{"title":"Issue one","state":"open"}`),
		}},
		Children: []externalsyncrepo.PullChildRecord{{
			ParentExternalKey: "ISSUE-1",
			Type:              "pull_request",
			ExternalKey:       "acme/app#313",
			ExternalURL:       "https://github.com/acme/app/pull/313",
			ExternalVersion:   seenAt.Format(time.RFC3339),
			ExternalUpdatedAt: ptrext.Of(seenAt),
			Payload: []byte(`{
				"number": 313,
				"title": "Ship delivery graph",
				"state": "open",
				"state_reason": "review_required",
				"assignees": [{"login": "octo"}]
			}`),
		}},
	})
	if err != nil {
		t.Fatalf("ApplyPullResult returned error: %v", err)
	}
	assertApplyStats(t, stats, externalsyncrepo.ApplyStats{RecordsSeen: 2, RecordsChanged: 2})
	assertProjectedDeliveryArtifact(t, ctx, pool, projectedDeliveryArtifactExpectation{
		tenantID:       tenantID,
		requestID:      requestID,
		connectionID:   conn.ID,
		mappingID:      mapping.ID,
		artifactType:   "pull_request",
		relationship:   "implements",
		externalKey:    "acme/app#313",
		externalURL:    "https://github.com/acme/app/pull/313",
		displayKey:     "313",
		title:          "Ship delivery graph",
		status:         "open",
		statusCategory: "review_required",
		stateReason:    "review_required",
		assignee:       "octo",
		syncState:      "synced",
		source:         "external_sync_child",
		lastSeenAt:     seenAt,
		payloadNumber:  "313",
	})
}

func TestGitHubProviderPullDeliveryArtifactsReachCustomerRequestGraph(t *testing.T) {
	core.SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	t.Cleanup(func() { core.SetEgressPolicy(nethardening.Policy{}) })

	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-github-delivery-graph")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-github-delivery-graph")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-github-delivery-graph")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "GitHub delivery graph")
	run := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	server := newGitHubDeliveryArtifactPullServer(t, requestID)
	t.Cleanup(server.Close)

	provider := githubadapter.NewProvider(githubadapter.WithHTTPClient(server.Client()))
	result, err := provider.Pull(ctx, core.PullRequest{
		Connection: core.Connection{
			Provider:       "github",
			ProviderConfig: githubProviderConfig(t, server.URL),
			Credential:     []byte("github-token"),
		},
		Cursor: []byte(`{"updated_since":"2026-07-08T00:00:00Z"}`),
	})
	if err != nil {
		t.Fatalf("GitHub Pull returned error: %v", err)
	}
	stats, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        run.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
		Records:      repoPullRecords(result.Records),
		Children:     repoPullChildren(result.Children),
	})
	if err != nil {
		t.Fatalf("ApplyPullResult returned error: %v", err)
	}
	assertApplyStats(t, stats, externalsyncrepo.ApplyStats{RecordsSeen: 3, RecordsChanged: 3})

	detail, err := crrepo.New(pool).GetDetail(ctx, tenantID, requestID, 50)
	if err != nil {
		t.Fatalf("GetDetail returned error: %v", err)
	}
	if len(detail.DeliveryGraph.Artifacts) != 4 || len(detail.DeliveryGraph.Relationships) != 3 {
		t.Fatalf("delivery graph = %+v, want request plus issue, PR, and commit", detail.DeliveryGraph)
	}
	requireDeliveryGraphArtifact(t, detail.DeliveryGraph, "issue", "7", crrepo.DeliveryHealthSynced)
	requireDeliveryGraphArtifact(t, detail.DeliveryGraph, "pull_request", "acme/app#313", crrepo.DeliveryHealthSynced)
	requireDeliveryGraphArtifact(t, detail.DeliveryGraph, "commit", "acme/app@0123456789abcdef", crrepo.DeliveryHealthSynced)
	if detail.DeliveryGraph.HealthExplanation != "3 linked artifacts: 3 synced." {
		t.Fatalf("delivery graph explanation = %q; want synced artifact rollup", detail.DeliveryGraph.HealthExplanation)
	}
}

func newGitHubDeliveryArtifactPullServer(t *testing.T, requestID uuid.UUID) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/acme/app/issues", func(w http.ResponseWriter, r *http.Request) {
		assertGitHubPullRequest(t, r, "github-token")
		if got := r.URL.Query().Get("since"); got != "2026-07-08T00:00:00Z" {
			t.Fatalf("issues since query = %q; want cursor value", got)
		}
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Fatalf("issues per_page = %q; want 100", got)
		}
		writeJSON(t, w, []map[string]any{{
			"number":       7,
			"html_url":     "https://github.com/acme/app/issues/7",
			"title":        "Delivery issue",
			"state":        "open",
			"state_reason": nil,
			"locked":       false,
			"updated_at":   "2026-07-08T10:00:00Z",
			"closed_at":    nil,
			"comments":     0,
			"body":         "<!-- attune:customer_request_id=" + requestID.String() + " -->",
		}})
	})
	mux.HandleFunc("/repos/acme/app/issues/7/timeline", func(w http.ResponseWriter, r *http.Request) {
		assertGitHubPullRequest(t, r, "github-token")
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Fatalf("timeline per_page = %q; want 100", got)
		}
		writeJSON(t, w, []map[string]any{
			{
				"event":      "cross-referenced",
				"created_at": "2026-07-08T10:08:00Z",
				"actor":      map[string]any{"login": "hubot"},
				"source": map[string]any{
					"type": "pull_request",
					"issue": map[string]any{
						"number":       313,
						"html_url":     "https://github.com/acme/app/pull/313",
						"title":        "Implement delivery graph",
						"state":        "open",
						"state_reason": "review_required",
						"assignee":     map[string]any{"login": "octo"},
						"assignees":    []map[string]any{{"login": "octo"}},
						"updated_at":   "2026-07-08T10:07:00Z",
						"pull_request": map[string]any{"url": "https://api.github.com/repos/acme/app/pulls/313"},
					},
				},
			},
			{
				"event":      "closed",
				"commit_id":  "0123456789abcdef",
				"commit_url": "https://api.github.com/repos/acme/app/commits/0123456789abcdef",
				"created_at": "2026-07-08T10:09:00Z",
				"actor":      map[string]any{"login": "octo"},
			},
		})
	})
	return httptest.NewServer(mux)
}

func assertGitHubPullRequest(t *testing.T, r *http.Request, token string) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+token {
		t.Fatalf("authorization header = %q; want bearer token", got)
	}
	if got := r.Header.Get("Accept"); got == "" {
		t.Fatal("accept header is empty; want GitHub media type")
	}
	if got := r.Header.Get("User-Agent"); got == "" {
		t.Fatal("user-agent header is empty")
	}
}

func githubProviderConfig(t *testing.T, apiBaseURL string) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]string{
		"owner":        "acme",
		"repo":         "app",
		"api_base_url": apiBaseURL,
	})
	if err != nil {
		t.Fatalf("marshal github provider config: %v", err)
	}
	return payload
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("write json response: %v", err)
	}
}

func repoPullRecords(records []core.ExternalRecord) []externalsyncrepo.PullRecord {
	out := make([]externalsyncrepo.PullRecord, 0, len(records))
	for _, record := range records {
		out = append(out, externalsyncrepo.PullRecord{
			LocalObjectID:     record.LocalObjectID,
			ExternalKey:       record.Key,
			ExternalURL:       record.URL,
			ExternalVersion:   record.Version,
			ExternalUpdatedAt: ptrext.Of(record.UpdatedAt),
			Deleted:           record.Deleted,
			Payload:           record.Payload,
		})
	}
	return out
}

func repoPullChildren(children []core.ExternalChildRecord) []externalsyncrepo.PullChildRecord {
	out := make([]externalsyncrepo.PullChildRecord, 0, len(children))
	for _, child := range children {
		out = append(out, externalsyncrepo.PullChildRecord{
			ParentExternalKey: child.ParentKey,
			Type:              child.Type,
			ExternalKey:       child.Key,
			ExternalURL:       child.URL,
			ExternalVersion:   child.Version,
			ExternalUpdatedAt: ptrext.Of(child.UpdatedAt),
			Deleted:           child.Deleted,
			Payload:           child.Payload,
		})
	}
	return out
}

func requireDeliveryGraphArtifact(
	t *testing.T,
	graph crrepo.DeliveryGraph,
	artifactType string,
	externalKey string,
	wantHealth crrepo.DeliveryHealth,
) {
	t.Helper()
	for _, artifact := range graph.Artifacts {
		if artifact.ArtifactType == artifactType && artifact.ExternalKey == externalKey {
			if artifact.Health != wantHealth {
				t.Fatalf("artifact %s/%s health = %q; want %q", artifactType, externalKey, artifact.Health, wantHealth)
			}
			return
		}
	}
	t.Fatalf("delivery graph artifacts = %+v; want %s/%s", graph.Artifacts, artifactType, externalKey)
}

func TestRepoResetCursorClearsCursorAndEnqueuesPullRun(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-reset-cursor")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-reset-cursor")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-reset-cursor")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)

	if _, err := pool.Exec(ctx, `
		INSERT INTO external_sync_cursors
		 (tenant_id, mapping_id, stream_key, cursor, high_watermark)
		VALUES ($1, $2, 'default', '{"page":5}'::jsonb, NOW())`,
		tenantID, mapping.ID); err != nil {
		t.Fatalf("seed external sync cursor: %v", err)
	}

	result, err := repository.ResetCursor(ctx, tenantID, mapping.ID, "admin-1")
	if err != nil {
		t.Fatalf("ResetCursor returned error: %v", err)
	}
	if result.Mapping.ID != mapping.ID {
		t.Fatalf("result mapping = %s; want %s", result.Mapping.ID, mapping.ID)
	}
	requireResetCursorRun(t, result.Run, mapping.ID)
	requireResetCursorRow(t, ctx, pool, tenantID, mapping.ID, "admin-1")
	if got := countExternalSyncManualPullRuns(t, ctx, pool, tenantID, mapping.ID); got != 1 {
		t.Fatalf("manual pull runs = %d; want one reset recovery run", got)
	}
}

func TestRepoEnqueueBackfillResetsCursorAndQueuesRun(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-backfill")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-backfill")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-backfill")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)

	mustExec(t, ctx, pool, `
		INSERT INTO external_sync_cursors
		 (tenant_id, mapping_id, stream_key, cursor, high_watermark)
		VALUES ($1, $2, 'default', '{"page":9}'::jsonb, NOW())`,
		tenantID, mapping.ID)

	result, err := repository.EnqueueBackfill(ctx, tenantID, mapping.ID, "admin-1", true)
	if err != nil {
		t.Fatalf("EnqueueBackfill returned error: %v", err)
	}
	if result.Mapping.ID != mapping.ID {
		t.Fatalf("result mapping = %s; want %s", result.Mapping.ID, mapping.ID)
	}
	requireBackfillRun(t, result.Run, mapping.ID)
	requireResetCursorRow(t, ctx, pool, tenantID, mapping.ID, "admin-1")
	if got := countExternalSyncBackfillRuns(t, ctx, pool, tenantID, mapping.ID); got != 1 {
		t.Fatalf("backfill runs = %d; want one queued backfill run", got)
	}
}

func TestRepoRecordTimelineListsRecordLedger(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-timeline")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-timeline")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-timeline")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	run := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "Timeline request")

	insertExternalObjectLink(t, ctx, pool, tenantID, mapping.ID, requestID.String(), "ISSUE-1")
	_ = insertExternalSyncRecordFailure(t, ctx, pool, tenantID, run.ID, mapping.ID)
	_ = insertExternalSyncConflict(t, ctx, pool, tenantID, mapping.ID)

	entries, err := repository.RecordTimeline(ctx, externalsyncrepo.RecordTimelineFilter{
		TenantID:    tenantID,
		MappingID:   mapping.ID,
		ExternalKey: "ISSUE-1",
		Limit:       10,
	})
	if err != nil {
		t.Fatalf("RecordTimeline returned error: %v", err)
	}
	requireTimelineKinds(t, entries, "link", "failure", "conflict")
}

func TestRepoApplyPullResultRevivesTombstonedExternalLink(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-revive")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-revive")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-revive")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "External sync revive")

	createRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	if _, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        createRun.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
		CursorAfter:  []byte(`{"page":1}`),
		Records: []externalsyncrepo.PullRecord{{
			LocalObjectID:   requestID.String(),
			ExternalKey:     "ISSUE-REVIVE",
			ExternalURL:     "https://github.example.test/org/repo/issues/9",
			ExternalVersion: "v1",
			Payload:         []byte(`{"title":"Revive me","status":"open"}`),
		}},
	}); err != nil {
		t.Fatalf("create ApplyPullResult returned error: %v", err)
	}

	deleteRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	if _, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        deleteRun.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
		CursorAfter:  []byte(`{"page":2}`),
		Records: []externalsyncrepo.PullRecord{{
			ExternalKey: "ISSUE-REVIVE",
			Deleted:     true,
			Payload:     []byte(`{"title":"Revive me"}`),
		}},
	}); err != nil {
		t.Fatalf("delete ApplyPullResult returned error: %v", err)
	}
	if got := externalObjectLinkState(t, ctx, pool, tenantID, mapping.ID, "ISSUE-REVIVE"); got != externalsyncrepo.SyncStateDeleted {
		t.Fatalf("link state after delete = %q; want deleted", got)
	}
	if got := issueLinkState(t, ctx, pool, tenantID, requestID, "ISSUE-REVIVE"); got != "stale" {
		t.Fatalf("issue link state after delete = %q; want stale", got)
	}

	reviveRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	stats, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        reviveRun.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
		CursorAfter:  []byte(`{"page":3}`),
		Records: []externalsyncrepo.PullRecord{{
			LocalObjectID:   requestID.String(),
			ExternalKey:     "ISSUE-REVIVE",
			ExternalURL:     "https://github.example.test/org/repo/issues/9",
			ExternalVersion: "v2",
			Payload:         []byte(`{"title":"Revive me again","status":"open"}`),
		}},
	})
	if err != nil {
		t.Fatalf("revive ApplyPullResult returned error: %v", err)
	}
	if stats.RecordsSeen != 1 || stats.RecordsChanged != 1 || stats.RecordsFailed != 0 {
		t.Fatalf("revive stats = %#v; want one changed record", stats)
	}
	if got := countExternalObjectLinks(t, ctx, pool, tenantID, mapping.ID); got != 1 {
		t.Fatalf("external object links = %d; want one revived link", got)
	}
	if got := externalObjectLinkState(t, ctx, pool, tenantID, mapping.ID, "ISSUE-REVIVE"); got != externalsyncrepo.SyncStateSynced {
		t.Fatalf("link state after revive = %q; want synced", got)
	}
	if got := issueLinkState(t, ctx, pool, tenantID, requestID, "ISSUE-REVIVE"); got != "synced" {
		t.Fatalf("issue link state after revive = %q; want synced", got)
	}
	deletedAt := sql.NullTime{}
	reason := ptrext.Of("")
	if err := pool.QueryRow(ctx, `
		SELECT external_deleted_at, tombstone_reason
		  FROM external_object_links
		 WHERE tenant_id = $1 AND mapping_id = $2 AND external_key = 'ISSUE-REVIVE'`,
		tenantID, mapping.ID).Scan(&deletedAt, reason); err != nil { // ptrext:allow scan-out-param
		t.Fatalf("read revived tombstone fields: %v", err)
	}
	if deletedAt.Valid || ptrext.Indirect(reason) != "" {
		t.Fatalf("revived tombstone = deleted:%t reason:%q; want cleared", deletedAt.Valid, ptrext.Indirect(reason))
	}
}

func TestRepoApplyPullResultSkipsLocallyTombstonedExternalLink(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-local-tombstone")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-local-tombstone")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-local-tombstone")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "External sync local tombstone")

	createRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	if _, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        createRun.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
		CursorAfter:  []byte(`{"page":1}`),
		Records: []externalsyncrepo.PullRecord{{
			LocalObjectID:   requestID.String(),
			ExternalKey:     "ISSUE-LOCAL-TOMBSTONE",
			ExternalURL:     "https://github.example.test/org/repo/issues/10",
			ExternalVersion: "v1",
			Payload:         []byte(`{"title":"Do not revive","status":"open"}`),
		}},
	}); err != nil {
		t.Fatalf("create ApplyPullResult returned error: %v", err)
	}
	if got := countIssueLinks(t, ctx, pool, tenantID, requestID); got != 1 {
		t.Fatalf("issue links before local tombstone = %d, want one", got)
	}

	mustExec(t, ctx, pool, `
		UPDATE external_object_links
		   SET local_deleted_at = NOW(),
		       sync_state = 'deleted',
		       tombstone_reason = 'local_unlinked'
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND external_key = 'ISSUE-LOCAL-TOMBSTONE'`,
		tenantID, mapping.ID)
	mustExec(t, ctx, pool, `
		DELETE FROM customer_request_issue_links
		 WHERE tenant_id = $1
		   AND request_id = $2
		   AND external_key = 'ISSUE-LOCAL-TOMBSTONE'`,
		tenantID, requestID)

	pullRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	stats, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        pullRun.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
		CursorAfter:  []byte(`{"page":2}`),
		Records: []externalsyncrepo.PullRecord{{
			LocalObjectID:   requestID.String(),
			ExternalKey:     "ISSUE-LOCAL-TOMBSTONE",
			ExternalURL:     "https://github.example.test/org/repo/issues/10",
			ExternalVersion: "v2",
			Payload:         []byte(`{"title":"Do not revive again","status":"open"}`),
		}},
	})
	if err != nil {
		t.Fatalf("local tombstone ApplyPullResult returned error: %v", err)
	}
	if stats.RecordsSeen != 1 || stats.RecordsChanged != 0 || stats.RecordsFailed != 0 {
		t.Fatalf("local tombstone stats = %#v; want seen without changed or failed", stats)
	}
	if got := countIssueLinks(t, ctx, pool, tenantID, requestID); got != 0 {
		t.Fatalf("issue links after local tombstone pull = %d, want zero", got)
	}
	var state, reason string
	var localDeleted, externalDeleted bool
	if err := pool.QueryRow(ctx, `
		SELECT sync_state,
		       tombstone_reason,
		       local_deleted_at IS NOT NULL,
		       external_deleted_at IS NOT NULL
		  FROM external_object_links
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND external_key = 'ISSUE-LOCAL-TOMBSTONE'`,
		tenantID, mapping.ID).Scan(&state, &reason, &localDeleted, &externalDeleted); err != nil {
		t.Fatalf("read local tombstone state: %v", err)
	}
	if state != externalsyncrepo.SyncStateDeleted || reason != "local_unlinked" || !localDeleted || externalDeleted {
		t.Fatalf("local tombstone state=%q reason=%q local_deleted=%t external_deleted=%t; want local tombstone",
			state, reason, localDeleted, externalDeleted)
	}
}

func TestRepoApplyPullResultCreatesConflictForPendingLocalLink(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-apply-conflict")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-apply-conflict")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-apply-conflict")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "External sync conflict")
	run := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)

	_, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        run.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
		CursorAfter:  []byte(`{"page":1}`),
		Records: []externalsyncrepo.PullRecord{{
			LocalObjectID:   requestID.String(),
			ExternalKey:     "ISSUE-2",
			ExternalVersion: "v1",
			Payload:         []byte(`{"title":"Issue two","status":"open"}`),
		}},
	})
	if err != nil {
		t.Fatalf("initial ApplyPullResult returned error: %v", err)
	}
	mustExec(t, ctx, pool, `
		UPDATE external_object_links
		   SET sync_state = 'pending'
		 WHERE tenant_id = $1 AND mapping_id = $2 AND external_key = 'ISSUE-2'`,
		tenantID, mapping.ID)

	conflictRun := insertExternalSyncRun(t, ctx, repository, tenantID, conn.ID, mapping.ID)
	stats, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     tenantID,
		RunID:        conflictRun.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
		CursorAfter:  []byte(`{"page":2}`),
		Records: []externalsyncrepo.PullRecord{{
			LocalObjectID:   requestID.String(),
			ExternalKey:     "ISSUE-2",
			ExternalVersion: "v2",
			Payload:         []byte(`{"title":"Issue two","status":"closed"}`),
		}},
	})
	if err != nil {
		t.Fatalf("conflict ApplyPullResult returned error: %v", err)
	}
	if stats.ConflictsCreated != 1 || stats.RecordsChanged != 0 {
		t.Fatalf("conflict stats = %#v; want one conflict and no changed record", stats)
	}
	if got := countExternalSyncConflicts(t, ctx, pool, tenantID, mapping.ID); got != 1 {
		t.Fatalf("open conflicts = %d; want one conflict row", got)
	}
	if got := externalObjectLinkState(t, ctx, pool, tenantID, mapping.ID, "ISSUE-2"); got != externalsyncrepo.SyncStateConflict {
		t.Fatalf("link state = %q; want conflict", got)
	}
}

func TestRepoPrepareAndApplyPushResult(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-push")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-push")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-push")
	mapping := updateExternalSyncMappingDirection(t, ctx, repository, tenantID, conn.ID, externalsyncrepo.DirectionPush)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "External sync push")

	run := insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.DirectionPush)
	claimed := claimExternalSyncRuns(t, ctx, repository, 1, "worker-push")
	requireClaimedExternalSyncRun(t, claimed, run.ID, 1, "worker-push")

	records := prepareExternalSyncPushRecords(t, ctx, repository, claimed[0].ID, tenantID, mapping.ID, conn.Provider)
	requirePreparedCreatePushRecord(t, records, requestID)
	defaultLimitRecords, err := repository.PreparePushRecords(ctx, claimed[0].ID, "worker-push", tenantID, mapping.ID, conn.Provider, 0)
	if err != nil {
		t.Fatalf("PreparePushRecords default limit returned error: %v", err)
	}
	requirePreparedCreatePushRecord(t, defaultLimitRecords, requestID)
	if _, err := repository.PreparePushRecords(ctx, claimed[0].ID, "wrong-worker", tenantID, mapping.ID, conn.Provider, 10); !errors.Is(err, externalsyncrepo.ErrRunNotFound) {
		t.Fatalf("PreparePushRecords wrong owner error = %v; want ErrRunNotFound", err)
	}
	if _, err := repository.PreparePushRecords(ctx, claimed[0].ID, "worker-push", tenantID, uuid.New(), conn.Provider, 10); !errors.Is(err, externalsyncrepo.ErrMappingNotFound) {
		t.Fatalf("PreparePushRecords missing mapping error = %v; want ErrMappingNotFound", err)
	}
	stats, err := repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{
		TenantID:     tenantID,
		RunID:        claimed[0].ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
		Records:      records,
		Results: []externalsyncrepo.PushResult{{
			LocalObjectID:   requestID.String(),
			ExternalKey:     "42",
			ExternalURL:     "https://github.com/acme/app/issues/42",
			ExternalVersion: "2026-07-08T12:00:00Z",
		}},
	})
	if err != nil {
		t.Fatalf("ApplyPushResult returned error: %v", err)
	}
	requirePushApplyStats(t, stats)
	if got := countExternalObjectLinks(t, ctx, pool, tenantID, mapping.ID); got != 1 {
		t.Fatalf("external object links = %d; want one pushed link", got)
	}
	if got := countIssueLinks(t, ctx, pool, tenantID, requestID); got != 1 {
		t.Fatalf("customer request issue links = %d; want one synced issue link", got)
	}
	assertIssueLinkSynced(t, ctx, pool, tenantID, requestID, "42", "https://github.com/acme/app/issues/42")

	records = prepareExternalSyncPushRecords(t, ctx, repository, claimed[0].ID, tenantID, mapping.ID, conn.Provider)
	requireNoPushRecords(t, records, "after synced push")

	time.Sleep(5 * time.Millisecond)
	mustExec(t, ctx, pool, `
		UPDATE customer_requests
		   SET title = 'External sync push updated',
		       updated_by = 'admin-2'
		 WHERE tenant_id = $1 AND id = $2`, tenantID, requestID)
	records = prepareExternalSyncPushRecords(t, ctx, repository, claimed[0].ID, tenantID, mapping.ID, conn.Provider)
	requirePreparedUpdatePushRecord(t, records)
	missingResultRun := insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.DirectionPush)
	stats, err = repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{
		TenantID:     tenantID,
		RunID:        missingResultRun.ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
		Records:      records,
	})
	if err != nil {
		t.Fatalf("ApplyPushResult missing result returned error: %v", err)
	}
	if stats.RecordsFailed != len(records) || stats.RecordsChanged != 0 {
		t.Fatalf("missing result stats = %#v; want failed records", stats)
	}
	if _, err := repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{
		TenantID:     tenantID,
		RunID:        uuid.New(),
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
	}); !errors.Is(err, externalsyncrepo.ErrRunNotFound) {
		t.Fatalf("missing ApplyPushResult run error = %v; want ErrRunNotFound", err)
	}
	if _, err := repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{
		TenantID:     tenantID,
		RunID:        missingResultRun.ID,
		ConnectionID: conn.ID,
		MappingID:    uuid.New(),
		Provider:     conn.Provider,
	}); !errors.Is(err, externalsyncrepo.ErrMappingNotFound) {
		t.Fatalf("missing ApplyPushResult mapping error = %v; want ErrMappingNotFound", err)
	}
}

func TestRepoPreparePushRecordsSkipsLocallyTombstonedIssueLink(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-push-local-tombstone")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-push-local-tombstone")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-push-local-tombstone")
	mapping := updateExternalSyncMappingDirection(t, ctx, repository, tenantID, conn.ID, externalsyncrepo.DirectionPush)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "External sync push")

	createRun := insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.DirectionPush)
	claimed := claimExternalSyncRuns(t, ctx, repository, 1, "worker-push")
	requireClaimedExternalSyncRun(t, claimed, createRun.ID, 1, "worker-push")
	records := prepareExternalSyncPushRecords(t, ctx, repository, claimed[0].ID, tenantID, mapping.ID, conn.Provider)
	requirePreparedCreatePushRecord(t, records, requestID)
	if _, err := repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{
		TenantID:     tenantID,
		RunID:        claimed[0].ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
		Records:      records,
		Results: []externalsyncrepo.PushResult{{
			LocalObjectID:   requestID.String(),
			ExternalKey:     "42",
			ExternalURL:     "https://github.com/acme/app/issues/42",
			ExternalVersion: "2026-07-08T12:00:00Z",
		}},
	}); err != nil {
		t.Fatalf("ApplyPushResult returned error: %v", err)
	}

	mustExec(t, ctx, pool, `
		UPDATE external_object_links
		   SET local_deleted_at = NOW(),
		       sync_state = 'deleted',
		       tombstone_reason = 'local_unlinked'
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND local_object_id = $3::text`,
		tenantID, mapping.ID, requestID.String())
	mustExec(t, ctx, pool, `
		DELETE FROM customer_request_issue_links
		 WHERE tenant_id = $1
		   AND request_id = $2`,
		tenantID, requestID)
	mustExec(t, ctx, pool, `
		INSERT INTO external_object_links
		 (id, tenant_id, mapping_id, local_object_type, local_object_id,
		  external_object_type, external_key, external_url, sync_state,
		  local_deleted_at, tombstone_reason)
		VALUES ($1, $2, $3, 'customer_request', $4, 'issue', '43',
		        'https://github.com/acme/app/issues/43', 'deleted',
		        NOW(), 'local_unlinked')`,
		uuid.New(), tenantID, mapping.ID, requestID.String())

	scheduledRun := insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.DirectionPush)
	claimed = claimExternalSyncRuns(t, ctx, repository, 1, "worker-push")
	requireClaimedExternalSyncRun(t, claimed, scheduledRun.ID, 1, "worker-push")
	records = prepareExternalSyncPushRecords(t, ctx, repository, claimed[0].ID, tenantID, mapping.ID, conn.Provider)
	requireNoPushRecords(t, records, "after local tombstone")
	if _, err := repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{
		TenantID:     tenantID,
		RunID:        claimed[0].ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
	}); err != nil {
		t.Fatalf("ApplyPushResult empty scheduled run returned error: %v", err)
	}

	broadCreateRun := insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.DirectionPush)
	mustExec(t, ctx, pool, `
		UPDATE external_sync_runs
		   SET input_metadata = '{"source":"customer_request_issue_create"}'::jsonb
		 WHERE tenant_id = $1
		   AND id = $2`,
		tenantID, broadCreateRun.ID)
	claimed = claimExternalSyncRuns(t, ctx, repository, 1, "worker-push")
	requireClaimedExternalSyncRun(t, claimed, broadCreateRun.ID, 1, "worker-push")
	records = prepareExternalSyncPushRecords(t, ctx, repository, claimed[0].ID, tenantID, mapping.ID, conn.Provider)
	requireNoPushRecords(t, records, "after broad create metadata without local object id")

	explicit, err := repository.CreateCustomerRequestIssueRun(ctx, externalsyncrepo.CustomerRequestIssueCreateRunInput{
		TenantID:  tenantID,
		RequestID: requestID,
		MappingID: ptrext.Of(mapping.ID),
		ActorID:   "admin-1",
	})
	if err != nil {
		t.Fatalf("CreateCustomerRequestIssueRun after local tombstone returned error: %v", err)
	}
	claimed = claimExternalSyncRuns(t, ctx, repository, 1, "worker-push")
	requireClaimedExternalSyncRun(t, claimed, ptrext.Indirect(explicit).Run.ID, 1, "worker-push")
	records = prepareExternalSyncPushRecords(t, ctx, repository, claimed[0].ID, tenantID, mapping.ID, conn.Provider)
	requirePreparedCreatePushRecord(t, records, requestID)
}

func TestRepoApplyPushResultKeepsIssueLinkWhenProviderReturnsMetadataWithError(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-push-partial")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-push-partial")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-push-partial")
	mapping := updateExternalSyncMappingDirection(t, ctx, repository, tenantID, conn.ID, externalsyncrepo.DirectionPush)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "External sync push")
	run := insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.DirectionPush)
	claimed := claimExternalSyncRuns(t, ctx, repository, 1, "worker-push")
	requireClaimedExternalSyncRun(t, claimed, run.ID, 1, "worker-push")

	records := prepareExternalSyncPushRecords(t, ctx, repository, claimed[0].ID, tenantID, mapping.ID, conn.Provider)
	requirePreparedCreatePushRecord(t, records, requestID)
	stats, err := repository.ApplyPushResult(ctx, externalsyncrepo.ApplyPushInput{
		TenantID:     tenantID,
		RunID:        claimed[0].ID,
		ConnectionID: conn.ID,
		MappingID:    mapping.ID,
		Provider:     conn.Provider,
		Records:      records,
		Results: []externalsyncrepo.PushResult{{
			LocalObjectID:   requestID.String(),
			ExternalKey:     "42",
			ExternalURL:     "https://github.com/acme/app/issues/42",
			ExternalVersion: "2026-07-08T12:00:00Z",
			ErrorKind:       "provider_error",
			ErrorMessage:    "managed comment write failed",
			Retryable:       true,
		}},
	})
	if err != nil {
		t.Fatalf("ApplyPushResult returned error: %v", err)
	}
	if stats.RecordsSeen != 1 || stats.RecordsChanged != 1 || stats.RecordsFailed != 1 {
		t.Fatalf("partial push stats = %#v; want changed link plus recorded failure", stats)
	}
	if got := countExternalObjectLinks(t, ctx, pool, tenantID, mapping.ID); got != 1 {
		t.Fatalf("external object links = %d; want linked issue metadata", got)
	}
	assertIssueLinkSynced(t, ctx, pool, tenantID, requestID, "42", "https://github.com/acme/app/issues/42")
	if got := countExternalSyncPushFailures(t, ctx, pool, tenantID, claimed[0].ID, "42"); got != 1 {
		t.Fatalf("push failures = %d; want provider error failure row", got)
	}
}

func TestRepoCreateCustomerRequestIssueRunQueuesSinglePushRun(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-create-issue")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-create-issue")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-create-issue")
	mapping := updateExternalSyncMappingDirection(t, ctx, repository, tenantID, conn.ID, externalsyncrepo.DirectionBidirectional)
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "Create GitHub issue")

	result, err := repository.CreateCustomerRequestIssueRun(ctx, externalsyncrepo.CustomerRequestIssueCreateRunInput{
		TenantID:  tenantID,
		RequestID: requestID,
		ActorID:   "admin-1",
	})
	if err != nil {
		t.Fatalf("CreateCustomerRequestIssueRun returned error: %v", err)
	}
	if result.Mapping.ID != mapping.ID ||
		result.Run.Direction != externalsyncrepo.DirectionPush ||
		result.Run.Trigger != externalsyncrepo.TriggerManual ||
		result.Run.Status != externalsyncrepo.RunStatusQueued {
		t.Fatalf("create issue result = %#v; want queued manual push run for selected mapping", result)
	}
	requireCreateIssueRunMetadata(t, result.Run, requestID)

	repeated, err := repository.CreateCustomerRequestIssueRun(ctx, externalsyncrepo.CustomerRequestIssueCreateRunInput{
		TenantID:  tenantID,
		RequestID: requestID,
		ActorID:   "admin-1",
	})
	if err != nil {
		t.Fatalf("repeated CreateCustomerRequestIssueRun returned error: %v", err)
	}
	if repeated.Run.ID != result.Run.ID {
		t.Fatalf("repeated run id = %s, want existing queued run %s", repeated.Run.ID, result.Run.ID)
	}

	linkedRequestID := insertExternalSyncCustomerRequestWithNumber(t, ctx, pool, tenantID, 2, "Already linked")
	insertExternalSyncGitHubIssueLink(t, ctx, pool, tenantID, linkedRequestID)
	_, err = repository.CreateCustomerRequestIssueRun(ctx, externalsyncrepo.CustomerRequestIssueCreateRunInput{
		TenantID:  tenantID,
		RequestID: linkedRequestID,
		ActorID:   "admin-1",
	})
	if !errors.Is(err, externalsyncrepo.ErrConflict) {
		t.Fatalf("linked CreateCustomerRequestIssueRun error = %v; want ErrConflict", err)
	}
}

func TestRepoCreateCustomerRequestIssueRunRequiresUniquePushMapping(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-create-issue-mapping")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-create-issue-mapping")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-create-issue-mapping")
	requestID := insertExternalSyncCustomerRequest(t, ctx, pool, tenantID, "Missing push mapping")

	_, err := repository.CreateCustomerRequestIssueRun(ctx, externalsyncrepo.CustomerRequestIssueCreateRunInput{
		TenantID:  tenantID,
		RequestID: requestID,
		ActorID:   "admin-1",
	})
	if !errors.Is(err, externalsyncrepo.ErrMappingNotFound) {
		t.Fatalf("pull-only CreateCustomerRequestIssueRun error = %v; want ErrMappingNotFound", err)
	}

	mapping := updateExternalSyncMappingDirection(t, ctx, repository, tenantID, conn.ID, externalsyncrepo.DirectionPush)
	other := createExternalSyncConnectionNamed(t, ctx, repository, tenantID, "kid-external-sync-create-issue-mapping", "GitHub Secondary")
	otherMapping := updateExternalSyncMappingDirection(t, ctx, repository, tenantID, other.ID, externalsyncrepo.DirectionPush)
	_, err = repository.CreateCustomerRequestIssueRun(ctx, externalsyncrepo.CustomerRequestIssueCreateRunInput{
		TenantID:  tenantID,
		RequestID: requestID,
		ActorID:   "admin-1",
	})
	if !errors.Is(err, externalsyncrepo.ErrConflict) {
		t.Fatalf("ambiguous CreateCustomerRequestIssueRun error = %v; want ErrConflict", err)
	}
	selected, err := repository.CreateCustomerRequestIssueRun(ctx, externalsyncrepo.CustomerRequestIssueCreateRunInput{
		TenantID:  tenantID,
		RequestID: requestID,
		MappingID: ptrext.Of(mapping.ID),
		ActorID:   "admin-1",
	})
	if err != nil {
		t.Fatalf("selected CreateCustomerRequestIssueRun returned error: %v", err)
	}
	if selected.Mapping.ID != mapping.ID {
		t.Fatalf("selected mapping = %s, want %s", selected.Mapping.ID, mapping.ID)
	}
	_, err = repository.CreateCustomerRequestIssueRun(ctx, externalsyncrepo.CustomerRequestIssueCreateRunInput{
		TenantID:  tenantID,
		RequestID: requestID,
		MappingID: ptrext.Of(otherMapping.ID),
		ActorID:   "admin-1",
	})
	if !errors.Is(err, externalsyncrepo.ErrConflict) {
		t.Fatalf("concurrent other mapping CreateCustomerRequestIssueRun error = %v; want ErrConflict", err)
	}
}

func TestRepoPreparePushRecordsSkipsUnsupportedMapping(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)
	repository := externalsyncrepo.New(pool)
	tenantID := insertExternalSyncTenant(t, ctx, pool, "external-sync-push-unsupported")
	insertExternalSyncKey(t, ctx, pool, "kid-external-sync-push-unsupported")
	conn := createExternalSyncConnection(t, ctx, repository, tenantID, "kid-external-sync-push-unsupported")
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, conn.ID)
	mustExec(t, ctx, pool, `
		ALTER TABLE external_object_mappings
		DROP CONSTRAINT chk_external_object_mappings_local_type`)
	mustExec(t, ctx, pool, `
		UPDATE external_object_mappings
		   SET local_object_type = 'other_object'
		 WHERE tenant_id = $1
		   AND id = $2`, tenantID, mapping.ID)
	run := insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, conn.ID, mapping.ID, externalsyncrepo.DirectionPush)
	records, err := repository.PreparePushRecords(ctx, run.ID, "", tenantID, mapping.ID, conn.Provider, 10)
	if err != nil {
		t.Fatalf("PreparePushRecords unsupported mapping returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("unsupported push records = %#v; want none", records)
	}
}

func insertExternalSyncTenant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, slug string) string {
	t.Helper()
	id := uuid.NewString()
	mustExec(t, ctx, pool, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`, id, slug, slug)
	return id
}

func insertExternalSyncKey(t *testing.T, ctx context.Context, pool *pgxpool.Pool, keyID string) {
	t.Helper()
	mustExec(t, ctx, pool, `
		INSERT INTO secret_key_registry
		 (key_id, primary_key, status, type_url, output_prefix_type, key_material_type,
		  fingerprint_sha256, fingerprint_version)
		VALUES ($1, FALSE, 'ENABLED', 'type.googleapis.com/google.crypto.tink.AesGcmKey',
		        'TINK', 'SYMMETRIC', 'fixture', 1)`,
		keyID)
}

func closedPoolSyncEvent(tenantID string, connectionID, mappingID uuid.UUID) externalsyncrepo.SyncEvent {
	return externalsyncrepo.SyncEvent{
		ID:                uuid.New(),
		TenantID:          tenantID,
		ConnectionID:      connectionID,
		MappingID:         ptrext.Of(mappingID),
		Provider:          "github",
		EventType:         "issues",
		ExternalEventID:   "delivery-closed",
		DedupeKey:         "github:issues:delivery-closed",
		SignatureStatus:   externalsyncrepo.EventSignatureVerified,
		Status:            externalsyncrepo.EventStatusReceived,
		PayloadDigest:     strings.Repeat("c", 64),
		NormalizedPayload: []byte(`{"action":"opened"}`),
		ReceivedAt:        time.Now().UTC(),
	}
}

func createExternalSyncConnection(t *testing.T, ctx context.Context, repository *externalsyncrepo.Repo, tenantID, keyID string) externalsyncrepo.Connection {
	t.Helper()
	return createExternalSyncConnectionNamed(t, ctx, repository, tenantID, keyID, "GitHub")
}

func createExternalSyncConnectionNamed(t *testing.T, ctx context.Context, repository *externalsyncrepo.Repo, tenantID, keyID, name string) externalsyncrepo.Connection {
	t.Helper()
	conn, err := repository.CreateConnection(ctx, externalsyncrepo.Connection{
		ID:                   uuid.New(),
		TenantID:             tenantID,
		Provider:             "github",
		Name:                 name,
		Enabled:              true,
		Status:               externalsyncrepo.ConnectionStatusActive,
		AuthType:             "token",
		ProviderConfig:       []byte("{}"),
		Scopes:               []string{"issues"},
		CredentialKeyID:      keyID,
		CredentialCiphertext: []byte("ciphertext"),
		CreatedBy:            "admin-1",
		UpdatedBy:            "admin-1",
	})
	if err != nil {
		t.Fatalf("CreateConnection returned error: %v", err)
	}
	return ptrext.Indirect(conn)
}

func createExternalSyncConnectionWithInstallation(t *testing.T, fixture externalSyncManagementFixture, installationID uuid.UUID) externalsyncrepo.Connection {
	t.Helper()
	conn, err := fixture.repository.CreateConnection(fixture.ctx, externalsyncrepo.Connection{
		ID:                     uuid.New(),
		TenantID:               fixture.tenantID,
		ProviderInstallationID: ptrext.Of(installationID),
		Provider:               "github",
		Name:                   "GitHub Bound",
		Enabled:                true,
		Status:                 externalsyncrepo.ConnectionStatusActive,
		AuthType:               "token",
		ProviderConfig:         []byte("{}"),
		Scopes:                 []string{"issues"},
		CredentialKeyID:        fixture.keyID,
		CredentialCiphertext:   []byte("ciphertext"),
		CreatedBy:              "admin-bound",
		UpdatedBy:              "admin-bound",
	})
	if err != nil {
		t.Fatalf("CreateConnection with provider installation returned error: %v", err)
	}
	requireProviderInstallationConnection(t, ptrext.Indirect(conn), installationID)
	return ptrext.Indirect(conn)
}

func createExternalProviderInstallation(t *testing.T, ctx context.Context, repository *externalsyncrepo.Repo, tenantID string) externalsyncrepo.ProviderInstallation {
	t.Helper()
	id := uuid.New()
	row, _, err := repository.CreateProviderInstallation(ctx, externalsyncrepo.ProviderInstallationWithResources{
		Installation: externalsyncrepo.ProviderInstallation{
			ID:                     id,
			TenantID:               tenantID,
			Provider:               "github",
			DisplayName:            "GitHub App",
			InstallationKind:       externalsyncrepo.InstallationKindGitHubApp,
			Status:                 externalsyncrepo.InstallationStatusActive,
			ExternalInstallationID: id.String(),
			AccountLogin:           "acme",
			AccountURL:             "https://github.com/acme",
			BaseURL:                "https://api.github.com",
			Permissions:            []byte(`{"metadata":"read","issues":"write"}`),
			CapabilityProfile:      []byte(`{}`),
			ResourceSelection:      externalsyncrepo.ResourceSelectionNone,
			QualificationStatus:    externalsyncrepo.TestStatusOK,
			CreatedBy:              "admin-installation",
			UpdatedBy:              "admin-installation",
		},
	})
	if err != nil {
		t.Fatalf("CreateProviderInstallation helper returned error: %v", err)
	}
	return ptrext.Indirect(row)
}

func requireProviderInstallationConnection(t *testing.T, conn externalsyncrepo.Connection, want uuid.UUID) {
	t.Helper()
	if conn.ProviderInstallationID == nil || ptrext.Indirect(conn.ProviderInstallationID) != want {
		t.Fatalf("connection provider installation id = %v; want %s", conn.ProviderInstallationID, want)
	}
}

func requireListedProviderInstallationConnection(t *testing.T, rows []externalsyncrepo.Connection, id, want uuid.UUID) {
	t.Helper()
	for _, row := range rows {
		if row.ID == id {
			requireProviderInstallationConnection(t, row, want)
			return
		}
	}
	t.Fatalf("ListConnections did not include bound connection %s", id)
}

func setExternalSyncRunForList(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id uuid.UUID, status string, createdAt time.Time) {
	t.Helper()
	mustExec(t, ctx, pool, `
		UPDATE external_sync_runs
		   SET status = $2,
		       created_at = $3,
		       updated_at = $3
		 WHERE id = $1`, id, status, createdAt)
}

func firstExternalSyncMapping(t *testing.T, ctx context.Context, repository *externalsyncrepo.Repo, tenantID string, connectionID uuid.UUID) externalsyncrepo.Mapping {
	t.Helper()
	mappings, err := repository.ListMappings(ctx, tenantID, connectionID)
	if err != nil {
		t.Fatalf("ListMappings returned error: %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("mapping count = %d; want one mapping", len(mappings))
	}
	return mappings[0]
}

func requireDefaultExternalSyncMapping(t *testing.T, mapping externalsyncrepo.Mapping) {
	t.Helper()
	if mapping.LocalObjectType != "customer_request" {
		t.Fatalf("mapping local object = %q; want customer_request", mapping.LocalObjectType)
	}
	if mapping.ExternalObjectType != "issue" {
		t.Fatalf("mapping external object = %q; want issue", mapping.ExternalObjectType)
	}
	if mapping.Direction != externalsyncrepo.DirectionPull {
		t.Fatalf("mapping direction = %q; want pull", mapping.Direction)
	}
}

func updateExternalSyncMappingDirection(
	t *testing.T,
	ctx context.Context,
	repository *externalsyncrepo.Repo,
	tenantID string,
	connectionID uuid.UUID,
	direction string,
) externalsyncrepo.Mapping {
	t.Helper()
	mapping := firstExternalSyncMapping(t, ctx, repository, tenantID, connectionID)
	mapping.Direction = direction
	updated, err := repository.UpdateMapping(ctx, mapping)
	if err != nil {
		t.Fatalf("UpdateMapping returned error: %v", err)
	}
	return ptrext.Indirect(updated)
}

func insertExternalSyncRun(t *testing.T, ctx context.Context, repository *externalsyncrepo.Repo, tenantID string, connectionID, mappingID uuid.UUID) externalsyncrepo.SyncRun {
	t.Helper()
	return insertExternalSyncRunWithDirection(t, ctx, repository, tenantID, connectionID, mappingID, externalsyncrepo.DirectionPull)
}

func insertTerminalExternalSyncRun(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	repository *externalsyncrepo.Repo,
	tenantID string,
	connectionID uuid.UUID,
	mappingID uuid.UUID,
	status string,
	createdAt time.Time,
) externalsyncrepo.SyncRun {
	t.Helper()
	run := insertExternalSyncRun(t, ctx, repository, tenantID, connectionID, mappingID)
	setExternalSyncRunForList(t, ctx, pool, run.ID, status, createdAt)
	return run
}

func insertExternalSyncRunWithDirection(
	t *testing.T,
	ctx context.Context,
	repository *externalsyncrepo.Repo,
	tenantID string,
	connectionID uuid.UUID,
	mappingID uuid.UUID,
	direction string,
) externalsyncrepo.SyncRun {
	t.Helper()
	run, err := repository.InsertRun(ctx, externalsyncrepo.SyncRun{
		ID:           uuid.New(),
		TenantID:     tenantID,
		ConnectionID: connectionID,
		MappingID:    ptrext.Of(mappingID),
		Direction:    direction,
		Trigger:      externalsyncrepo.TriggerManual,
		ActorID:      "admin-1",
	})
	if err != nil {
		t.Fatalf("InsertRun returned error: %v", err)
	}
	return ptrext.Indirect(run)
}

func claimExternalSyncRuns(t *testing.T, ctx context.Context, repository *externalsyncrepo.Repo, n int, owner string) []externalsyncrepo.SyncRun {
	t.Helper()
	claimed, err := repository.ClaimBatch(ctx, n, owner)
	if err != nil {
		t.Fatalf("ClaimBatch returned error: %v", err)
	}
	return claimed
}

func requireClaimedExternalSyncRun(t *testing.T, claimed []externalsyncrepo.SyncRun, runID uuid.UUID, attempts int, owner string) {
	t.Helper()
	if len(claimed) != 1 {
		t.Fatalf("claimed runs = %#v; want one run", claimed)
	}
	row := claimed[0]
	if row.ID != runID {
		t.Fatalf("claimed run id = %s; want %s", row.ID, runID)
	}
	if row.Status != externalsyncrepo.RunStatusRunning {
		t.Fatalf("claimed run status = %q; want running", row.Status)
	}
	if row.Attempts != attempts || row.ClaimedBy != owner {
		t.Fatalf("claimed run = %#v; want attempt %d claimed by %s", row, attempts, owner)
	}
}

func requireRowsAffected(t *testing.T, operation string, affected int64) {
	t.Helper()
	if affected != 1 {
		t.Fatalf("%s affected = %d; want 1", operation, affected)
	}
}

func requireProviderResourceSelection(t *testing.T, resources []externalsyncrepo.ProviderInstallationResource, want map[uuid.UUID]bool) {
	t.Helper()
	if len(resources) != len(want) {
		t.Fatalf("resources = %#v; want %d rows", resources, len(want))
	}
	for _, resource := range resources {
		selected, ok := want[resource.ID]
		if !ok {
			t.Fatalf("unexpected resource row = %#v", resource)
		}
		if resource.Selected != selected {
			t.Fatalf("resource %s selected = %v; want %v", resource.ID, resource.Selected, selected)
		}
	}
}

func requireQueuedRetryRun(t *testing.T, run externalsyncrepo.SyncRun) {
	t.Helper()
	if run.Status != externalsyncrepo.RunStatusQueued {
		t.Fatalf("retried run status = %q; want queued", run.Status)
	}
	if run.Trigger != externalsyncrepo.TriggerRetry {
		t.Fatalf("retried run trigger = %q; want retry", run.Trigger)
	}
}

func requireCleanExternalSyncHealth(t *testing.T, health externalsyncrepo.Health) {
	t.Helper()
	if health.EnabledConnections != 1 {
		t.Fatalf("enabled connections = %d; want 1", health.EnabledConnections)
	}
	if health.RetryableRuns != 0 ||
		health.DeadRuns != 0 ||
		health.ThrottledRuns != 0 ||
		health.DelayedRetryRuns != 0 ||
		health.NewestSuccessfulRunAt == nil {
		t.Fatalf("health = %#v; want no retryable/dead/throttled runs and newest successful run", health)
	}
}

func requireThrottledExternalSyncHealth(t *testing.T, health externalsyncrepo.Health) {
	t.Helper()
	if health.ThrottledRuns != 1 || health.DelayedRetryRuns != 1 || health.NewestRetryAfter == nil {
		t.Fatalf("health = %#v; want throttled delayed retry", health)
	}
	if health.UnauthorizedRuns != 0 || health.ProviderUnavailableRuns != 0 {
		t.Fatalf("provider breakdown = %#v; want only throttled", health)
	}
}

func requireExternalConnectionQuarantined(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	connectionID uuid.UUID,
	reasonPart string,
) {
	t.Helper()
	var enabled bool
	var status string
	var lastError sql.NullString
	if err := pool.QueryRow(ctx, `
		SELECT enabled, status, last_error
		  FROM external_connections
		 WHERE tenant_id = $1
		   AND id = $2`,
		tenantID, connectionID).Scan(&enabled, &status, &lastError); err != nil {
		t.Fatalf("read quarantined external connection: %v", err)
	}
	if enabled || status != externalsyncrepo.ConnectionStatusQuarantined {
		t.Fatalf("connection enabled=%t status=%q; want disabled quarantined", enabled, status)
	}
	if !lastError.Valid || !strings.Contains(lastError.String, reasonPart) {
		t.Fatalf("connection last_error = %q; want reason containing %q", lastError.String, reasonPart)
	}
}

func requireExternalConnectionActive(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	connectionID uuid.UUID,
) {
	t.Helper()
	var enabled bool
	var status string
	var lastError sql.NullString
	if err := pool.QueryRow(ctx, `
		SELECT enabled, status, last_error
		  FROM external_connections
		 WHERE tenant_id = $1
		   AND id = $2`,
		tenantID, connectionID).Scan(&enabled, &status, &lastError); err != nil {
		t.Fatalf("read resumed external connection: %v", err)
	}
	if !enabled || status != externalsyncrepo.ConnectionStatusActive || lastError.String != "" {
		t.Fatalf("connection enabled=%t status=%q last_error=%q; want active with cleared error",
			enabled, status, lastError.String)
	}
}

func requireExternalSyncConflictsResolved(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	ids []uuid.UUID,
	resolution string,
) {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		  FROM external_sync_conflicts
		 WHERE tenant_id = $1
		   AND id = ANY($2)
		   AND status = 'resolved'
		   AND resolution = $3
		   AND resolved_by = 'admin-1'
		   AND resolved_at IS NOT NULL`,
		tenantID, ids, resolution).Scan(&count); err != nil {
		t.Fatalf("count resolved external sync conflicts: %v", err)
	}
	if count != len(ids) {
		t.Fatalf("resolved conflicts = %d; want %d", count, len(ids))
	}
}

func insertExternalSyncRecordFailure(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, runID, mappingID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	mustExec(t, ctx, pool, `
		INSERT INTO external_sync_record_failures
		 (id, tenant_id, run_id, mapping_id, operation, external_key, failure_kind,
		  message, payload_digest, retry_mode, normalized_payload, retryable)
		VALUES ($1, $2, $3, $4, 'pull', 'ISSUE-1', 'http_429',
		        'rate limited', 'sha256:abc', 'refetch', '{"key":"ISSUE-1"}'::jsonb, TRUE)`,
		id, tenantID, runID, mappingID)
	return id
}

func insertExternalSyncConflict(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, mappingID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	mustExec(t, ctx, pool, `
		INSERT INTO external_sync_conflicts
		 (id, tenant_id, mapping_id, local_object_id, external_key, conflict_kind,
		  local_snapshot, external_snapshot)
		VALUES ($1, $2, $3, 'cr-1', 'ISSUE-1', 'version_mismatch',
		        '{"status":"open"}'::jsonb, '{"status":"closed"}'::jsonb)`,
		id, tenantID, mappingID)
	return id
}

func insertExternalObjectLink(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	mappingID uuid.UUID,
	localObjectID string,
	externalKey string,
) uuid.UUID {
	t.Helper()
	id := uuid.New()
	mustExec(t, ctx, pool, `
		INSERT INTO external_object_links
		 (id, tenant_id, mapping_id, local_object_type, local_object_id,
		  external_object_type, external_key, external_url, external_version,
		  sync_state, last_synced_at)
		VALUES ($1, $2, $3, 'customer_request', $4,
		        'issue', $5, 'https://github.example.test/org/repo/issues/1', 'v1',
		        'synced', NOW())`,
		id, tenantID, mappingID, localObjectID, externalKey)
	return id
}

func requireRetriedExternalSyncFailure(t *testing.T, failure externalsyncrepo.RecordFailure) {
	t.Helper()
	if failure.ResolvedAt == nil {
		t.Fatalf("retried failure resolved_at = nil; want timestamp")
	}
	if failure.ResolvedBy != "admin-1" || !failure.Retryable {
		t.Fatalf("retried failure = %#v; want admin-1 retryable failure", failure)
	}
}

func requireResolvedExternalSyncConflict(t *testing.T, conflict externalsyncrepo.ConflictRow) {
	t.Helper()
	if conflict.Status != "resolved" {
		t.Fatalf("resolved conflict status = %q; want resolved", conflict.Status)
	}
	if conflict.Resolution != "local_wins" || conflict.ResolvedAt == nil {
		t.Fatalf("resolved conflict = %#v; want local_wins with timestamp", conflict)
	}
}

func requireExternalSyncRunDetail(t *testing.T, detail externalsyncrepo.RunDetail, failureID, conflictID uuid.UUID) {
	t.Helper()
	if len(detail.Attempts) != 1 || detail.Attempts[0].AttemptNumber != 1 ||
		detail.Attempts[0].ProviderRequestID != "github-retry-1" {
		t.Fatalf("detail attempts = %#v; want recorded provider attempt", detail.Attempts)
	}
	if len(detail.Failures) != 1 || detail.Failures[0].ID != failureID {
		t.Fatalf("detail failures = %#v; want retry failure row", detail.Failures)
	}
	if len(detail.Conflicts) != 1 || detail.Conflicts[0].ID != conflictID {
		t.Fatalf("detail conflicts = %#v; want conflict row for mapping", detail.Conflicts)
	}
}

func requireResetCursorRun(t *testing.T, run externalsyncrepo.SyncRun, mappingID uuid.UUID) {
	t.Helper()
	if run.MappingID == nil {
		t.Fatalf("reset run mapping is nil; want %s", mappingID)
	}
	if ptrext.Indirect(run.MappingID) != mappingID ||
		run.Direction != externalsyncrepo.DirectionPull ||
		run.Trigger != externalsyncrepo.TriggerManual ||
		run.Status != externalsyncrepo.RunStatusQueued ||
		run.ActorID != "admin-1" {
		t.Fatalf("reset run = %#v; want queued manual pull run", run)
	}
}

func requireBackfillRun(t *testing.T, run externalsyncrepo.SyncRun, mappingID uuid.UUID) {
	t.Helper()
	if run.MappingID == nil {
		t.Fatalf("backfill run mapping is nil; want %s", mappingID)
	}
	if ptrext.Indirect(run.MappingID) != mappingID ||
		run.Direction != externalsyncrepo.DirectionPull ||
		run.Trigger != externalsyncrepo.TriggerBackfill ||
		run.Status != externalsyncrepo.RunStatusQueued ||
		run.ActorID != "admin-1" {
		t.Fatalf("backfill run = %#v; want queued backfill pull run", run)
	}
}

func requireResetCursorRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, mappingID uuid.UUID, actor string) {
	t.Helper()
	cursor := ptrext.Of("")
	resetBy := ptrext.Of("")
	resetAtSet := ptrext.Of(false)
	highWatermarkCleared := ptrext.Of(false)
	if err := pool.QueryRow(ctx, `
		SELECT cursor::text,
		       reset_requested_by,
		       reset_requested_at IS NOT NULL,
		       high_watermark IS NULL
		  FROM external_sync_cursors
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND stream_key = 'default'`,
		tenantID, mappingID).Scan(cursor, resetBy, resetAtSet, highWatermarkCleared); err != nil {
		t.Fatalf("read reset cursor: %v", err)
	}
	if ptrext.Indirect(cursor) != "{}" ||
		ptrext.Indirect(resetBy) != actor ||
		!ptrext.Indirect(resetAtSet) ||
		!ptrext.Indirect(highWatermarkCleared) {
		t.Fatalf("cursor reset row = cursor %s by %s at %t high watermark cleared %t",
			ptrext.Indirect(cursor), ptrext.Indirect(resetBy),
			ptrext.Indirect(resetAtSet), ptrext.Indirect(highWatermarkCleared))
	}
}

func requireTimelineKinds(t *testing.T, entries []externalsyncrepo.RecordTimelineEntry, kinds ...string) {
	t.Helper()
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.Kind] = true
		if entry.OccurredAt.IsZero() || entry.Summary == "" || len(entry.Detail) == 0 {
			t.Fatalf("timeline entry = %#v; want occurred_at, summary, and detail", entry)
		}
	}
	for _, kind := range kinds {
		if !seen[kind] {
			t.Fatalf("timeline entries = %#v; missing kind %q", entries, kind)
		}
	}
}

func prepareExternalSyncPushRecords(
	t *testing.T,
	ctx context.Context,
	repository *externalsyncrepo.Repo,
	runID uuid.UUID,
	tenantID string,
	mappingID uuid.UUID,
	provider string,
) []externalsyncrepo.PushRecord {
	t.Helper()
	records, err := repository.PreparePushRecords(ctx, runID, "worker-push", tenantID, mappingID, provider, 100)
	if err != nil {
		t.Fatalf("PreparePushRecords returned error: %v", err)
	}
	return records
}

func requirePreparedCreatePushRecord(t *testing.T, records []externalsyncrepo.PushRecord, requestID uuid.UUID) {
	t.Helper()
	if len(records) != 1 {
		t.Fatalf("prepared records = %#v; want one create push record", records)
	}
	if records[0].LocalObjectID != requestID.String() || records[0].ExternalKey != "" {
		t.Fatalf("prepared record = %#v; want create record for request %s", records[0], requestID)
	}
	if got := payloadStringForTest(t, records[0].Payload, "title"); got != "CR-1 External sync push" {
		t.Fatalf("push title payload = %q; want CR display title", got)
	}
}

func requirePushApplyStats(t *testing.T, stats externalsyncrepo.ApplyStats) {
	t.Helper()
	if stats.RecordsSeen != 1 || stats.RecordsChanged != 1 || stats.RecordsFailed != 0 {
		t.Fatalf("push stats = %#v; want one changed record", stats)
	}
}

func requireNoPushRecords(t *testing.T, records []externalsyncrepo.PushRecord, where string) {
	t.Helper()
	if len(records) != 0 {
		t.Fatalf("prepared records %s = %#v; want none", where, records)
	}
}

func requirePreparedUpdatePushRecord(t *testing.T, records []externalsyncrepo.PushRecord) {
	t.Helper()
	if len(records) != 1 {
		t.Fatalf("prepared records after local update = %#v; want one update record", records)
	}
	if records[0].ExternalKey != "42" {
		t.Fatalf("prepared update external key = %q; want 42", records[0].ExternalKey)
	}
}

func insertExternalSyncCustomerRequest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID, title string) uuid.UUID {
	t.Helper()
	return insertExternalSyncCustomerRequestWithNumber(t, ctx, pool, tenantID, 1, title)
}

func insertExternalSyncCustomerRequestWithNumber(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	tenantID string,
	displayNumber int64,
	title string,
) uuid.UUID {
	t.Helper()
	id := ptrext.Of(uuid.New())
	mustExec(t, ctx, pool, `
		INSERT INTO customer_requests
		 (id, tenant_id, display_number, display_id, title, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, 'admin-1', 'admin-1')`,
		ptrext.Indirect(id), tenantID, displayNumber, fmt.Sprintf("CR-%d", displayNumber), title)
	return ptrext.Indirect(id)
}

func insertExternalSyncGitHubIssueLink(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, requestID uuid.UUID) {
	t.Helper()
	mustExec(t, ctx, pool, `
		INSERT INTO customer_request_issue_links
		 (tenant_id, request_id, provider, external_key, external_url, title, status, created_by)
		VALUES ($1, $2, 'github', '42', 'https://github.com/acme/app/issues/42',
		        'Existing issue', 'open', 'admin-1')`,
		tenantID, requestID)
}

func requireCreateIssueRunMetadata(t *testing.T, run externalsyncrepo.SyncRun, requestID uuid.UUID) {
	t.Helper()
	var metadata map[string]string
	if err := json.Unmarshal(run.InputMetadata, &metadata); err != nil {
		t.Fatalf("unmarshal create issue run metadata: %v", err)
	}
	if metadata["local_object_id"] != requestID.String() ||
		metadata["source"] != "customer_request_issue_create" {
		t.Fatalf("create issue run metadata = %#v; want request selector", metadata)
	}
}

func countExternalObjectLinks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, mappingID uuid.UUID) int {
	t.Helper()
	count := ptrext.Of(0)
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		  FROM external_object_links
		 WHERE tenant_id = $1 AND mapping_id = $2`, tenantID, mappingID).Scan(count); err != nil {
		t.Fatalf("count external object links: %v", err)
	}
	return ptrext.Indirect(count)
}

func countIssueLinks(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, requestID uuid.UUID) int {
	t.Helper()
	count := ptrext.Of(0)
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		  FROM customer_request_issue_links
		 WHERE tenant_id = $1 AND request_id = $2`, tenantID, requestID).Scan(count); err != nil {
		t.Fatalf("count customer request issue links: %v", err)
	}
	return ptrext.Indirect(count)
}

func externalSyncCursor(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, mappingID uuid.UUID) string {
	t.Helper()
	cursor := ptrext.Of("")
	if err := pool.QueryRow(ctx, `
		SELECT cursor::text
		  FROM external_sync_cursors
		 WHERE tenant_id = $1 AND mapping_id = $2 AND stream_key = 'default'`,
		tenantID, mappingID).Scan(cursor); err != nil {
		t.Fatalf("read external sync cursor: %v", err)
	}
	return ptrext.Indirect(cursor)
}

func countExternalSyncConflicts(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, mappingID uuid.UUID) int {
	t.Helper()
	count := ptrext.Of(0)
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		  FROM external_sync_conflicts
		 WHERE tenant_id = $1 AND mapping_id = $2 AND status = 'open'`,
		tenantID, mappingID).Scan(count); err != nil {
		t.Fatalf("count external sync conflicts: %v", err)
	}
	return ptrext.Indirect(count)
}

func countExternalSyncRetryRuns(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, mappingID uuid.UUID) int {
	t.Helper()
	count := ptrext.Of(0)
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		  FROM external_sync_runs
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND trigger = 'retry'
		   AND status = 'queued'`,
		tenantID, mappingID).Scan(count); err != nil {
		t.Fatalf("count external sync retry runs: %v", err)
	}
	return ptrext.Indirect(count)
}

func countExternalSyncPushFailures(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, runID uuid.UUID, externalKey string) int {
	t.Helper()
	count := ptrext.Of(0)
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		  FROM external_sync_record_failures
		 WHERE tenant_id = $1
		   AND run_id = $2
		   AND operation = 'push'
		   AND external_key = $3`,
		tenantID, runID, externalKey).Scan(count); err != nil {
		t.Fatalf("count external sync push failures: %v", err)
	}
	return ptrext.Indirect(count)
}

func countExternalSyncManualPullRuns(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, mappingID uuid.UUID) int {
	t.Helper()
	count := ptrext.Of(0)
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		  FROM external_sync_runs
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND trigger = 'manual'
		   AND direction = 'pull'
		   AND status = 'queued'`,
		tenantID, mappingID).Scan(count); err != nil {
		t.Fatalf("count external sync manual pull runs: %v", err)
	}
	return ptrext.Indirect(count)
}

func countExternalSyncBackfillRuns(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, mappingID uuid.UUID) int {
	t.Helper()
	count := ptrext.Of(0)
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		  FROM external_sync_runs
		 WHERE tenant_id = $1
		   AND mapping_id = $2
		   AND trigger = 'backfill'
		   AND direction = 'pull'
		   AND status = 'queued'`,
		tenantID, mappingID).Scan(count); err != nil {
		t.Fatalf("count external sync backfill runs: %v", err)
	}
	return ptrext.Indirect(count)
}

type githubIssuePullResultInput struct {
	tenantID          string
	runID             uuid.UUID
	connectionID      uuid.UUID
	mappingID         uuid.UUID
	requestID         uuid.UUID
	cursorBefore      []byte
	cursorAfter       []byte
	externalVersion   string
	externalUpdatedAt *time.Time
}

func applyGitHubIssuePullResult(
	t *testing.T,
	ctx context.Context,
	repository *externalsyncrepo.Repo,
	in githubIssuePullResultInput,
) externalsyncrepo.ApplyStats {
	t.Helper()
	stats, err := repository.ApplyPullResult(ctx, externalsyncrepo.ApplyPullInput{
		TenantID:     in.tenantID,
		RunID:        in.runID,
		ConnectionID: in.connectionID,
		MappingID:    in.mappingID,
		Provider:     "github",
		StreamKey:    externalsyncrepo.StreamDefault,
		CursorBefore: in.cursorBefore,
		CursorAfter:  in.cursorAfter,
		Records: []externalsyncrepo.PullRecord{{
			LocalObjectID:     in.requestID.String(),
			ExternalKey:       "ISSUE-1",
			ExternalURL:       "https://github.example.test/org/repo/issues/1",
			ExternalVersion:   in.externalVersion,
			ExternalUpdatedAt: in.externalUpdatedAt,
			Payload:           []byte(`{"title":"Issue one","state":"open","assignee":"octo","assignees":["octo","hubot"],"labels":["bug","customer"],"comments":2}`),
		}},
	})
	if err != nil {
		t.Fatalf("ApplyPullResult returned error: %v", err)
	}
	return stats
}

func assertApplyStats(t *testing.T, got externalsyncrepo.ApplyStats, want externalsyncrepo.ApplyStats) {
	t.Helper()
	if got != want {
		t.Fatalf("apply stats = %#v; want %#v", got, want)
	}
}

type projectedDeliveryArtifactExpectation struct {
	tenantID       string
	requestID      uuid.UUID
	connectionID   uuid.UUID
	mappingID      uuid.UUID
	artifactType   string
	relationship   string
	externalKey    string
	externalURL    string
	displayKey     string
	title          string
	status         string
	statusCategory string
	stateReason    string
	assignee       string
	syncState      string
	source         string
	lastSeenAt     time.Time
	payloadNumber  string
}

func assertProjectedDeliveryArtifact(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	want projectedDeliveryArtifactExpectation,
) {
	t.Helper()
	var connectionID, mappingID string
	var hasExternalObjectLink bool
	var artifactType, relationship, externalKey, externalURL, displayKey string
	var title, status, statusCategory, stateReason, assignee, syncState, source string
	var lastSeenAt time.Time
	var payloadNumber sql.NullString
	if err := pool.QueryRow(ctx, `
		SELECT connection_id::text,
		       mapping_id::text,
		       external_object_link_id IS NOT NULL,
		       artifact_type,
		       relationship,
		       external_key,
		       external_url,
		       display_key,
		       title,
		       status,
		       status_category,
		       state_reason,
		       assignee,
		       sync_state,
		       source,
		       last_seen_at,
		       payload->>'number'
		  FROM customer_request_delivery_artifacts
		 WHERE tenant_id = $1
		   AND request_id = $2
		   AND provider = 'github'
		   AND artifact_type = $3
		   AND external_key = $4
		   AND deleted_at IS NULL`,
		want.tenantID, want.requestID, want.artifactType, want.externalKey).Scan(
		&connectionID,
		&mappingID,
		&hasExternalObjectLink,
		&artifactType,
		&relationship,
		&externalKey,
		&externalURL,
		&displayKey,
		&title,
		&status,
		&statusCategory,
		&stateReason,
		&assignee,
		&syncState,
		&source,
		&lastSeenAt,
		&payloadNumber,
	); err != nil {
		t.Fatalf("read projected delivery artifact: %v", err)
	}
	got := projectedDeliveryArtifactExpectation{
		connectionID:   uuid.MustParse(connectionID),
		mappingID:      uuid.MustParse(mappingID),
		artifactType:   artifactType,
		relationship:   relationship,
		externalKey:    externalKey,
		externalURL:    externalURL,
		displayKey:     displayKey,
		title:          title,
		status:         status,
		statusCategory: statusCategory,
		stateReason:    stateReason,
		assignee:       assignee,
		syncState:      syncState,
		source:         source,
		lastSeenAt:     lastSeenAt.UTC(),
		payloadNumber:  payloadNumber.String,
	}
	want.tenantID = ""
	want.requestID = uuid.Nil
	want.lastSeenAt = want.lastSeenAt.UTC()
	if got != want || !hasExternalObjectLink {
		t.Fatalf("projected delivery artifact = %+v external_link=%t; want %+v with external link", got, hasExternalObjectLink, want)
	}
}

func recordGitHubIssueTimeline(
	t *testing.T,
	ctx context.Context,
	repository *externalsyncrepo.Repo,
	tenantID string,
	mappingID uuid.UUID,
	requestID uuid.UUID,
) []externalsyncrepo.RecordTimelineEntry {
	t.Helper()
	entries, err := repository.RecordTimeline(ctx, externalsyncrepo.RecordTimelineFilter{
		TenantID:      tenantID,
		MappingID:     mappingID,
		LocalObjectID: requestID.String(),
		ExternalKey:   "ISSUE-1",
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("RecordTimeline after pull returned error: %v", err)
	}
	return entries
}

func assertApplyPullResultError(
	t *testing.T,
	ctx context.Context,
	repository *externalsyncrepo.Repo,
	in externalsyncrepo.ApplyPullInput,
	want error,
) {
	t.Helper()
	if _, err := repository.ApplyPullResult(ctx, in); !errors.Is(err, want) {
		t.Fatalf("ApplyPullResult error = %v; want %v", err, want)
	}
}

func assertIssueLinkSynced(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, requestID uuid.UUID, externalKey, externalURL string) {
	t.Helper()
	gotKey := ptrext.Of("")
	gotURL := ptrext.Of("")
	gotState := ptrext.Of("")
	if err := pool.QueryRow(ctx, `
		SELECT external_key, external_url, sync_state
		  FROM customer_request_issue_links
		 WHERE tenant_id = $1 AND request_id = $2`, tenantID, requestID).Scan(gotKey, gotURL, gotState); err != nil {
		t.Fatalf("read issue link: %v", err)
	}
	if ptrext.Indirect(gotKey) != externalKey || ptrext.Indirect(gotURL) != externalURL || ptrext.Indirect(gotState) != "synced" {
		t.Fatalf("issue link = key:%q url:%q state:%q; want %q/%q/synced",
			ptrext.Indirect(gotKey), ptrext.Indirect(gotURL), ptrext.Indirect(gotState), externalKey, externalURL)
	}
}

func assertIssueLinkSyncContext(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, requestID uuid.UUID, externalKey, category, assignee string) {
	t.Helper()
	gotCategory := ptrext.Of("")
	gotAssignee := ptrext.Of("")
	if err := pool.QueryRow(ctx, `
		SELECT external_status_category, external_assignee
		  FROM customer_request_issue_links
		 WHERE tenant_id = $1 AND request_id = $2 AND external_key = $3`,
		tenantID, requestID, externalKey).Scan(gotCategory, gotAssignee); err != nil {
		t.Fatalf("read issue link sync context: %v", err)
	}
	if ptrext.Indirect(gotCategory) != category || ptrext.Indirect(gotAssignee) != assignee {
		t.Fatalf("issue link context = category:%q assignee:%q; want %q/%q",
			ptrext.Indirect(gotCategory), ptrext.Indirect(gotAssignee), category, assignee)
	}
}

func assertTimelineProviderPayload(
	t *testing.T,
	entries []externalsyncrepo.RecordTimelineEntry,
	labels []string,
	assignees []string,
	comments float64,
) {
	t.Helper()
	for _, entry := range entries {
		if entry.Kind != "link" {
			continue
		}
		var detail map[string]any
		if err := json.Unmarshal(entry.Detail, &detail); err != nil {
			t.Fatalf("unmarshal timeline detail: %v", err)
		}
		payload, ok := detail["provider_payload"].(map[string]any)
		if !ok {
			t.Fatalf("timeline detail = %#v; missing provider_payload", detail)
		}
		if !stringArrayMatches(payload["labels"], labels) ||
			!stringArrayMatches(payload["assignees"], assignees) ||
			payload["comments"] != comments {
			t.Fatalf("timeline provider payload = %#v; want labels %v assignees %v comments %.0f",
				payload, labels, assignees, comments)
		}
		return
	}
	t.Fatalf("timeline entries = %#v; missing link provider payload", entries)
}

func stringArrayMatches(value any, want []string) bool {
	items, ok := value.([]any)
	if !ok || len(items) != len(want) {
		return false
	}
	for i, item := range items {
		if item != want[i] {
			return false
		}
	}
	return true
}

func issueLinkState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, requestID uuid.UUID, externalKey string) string {
	t.Helper()
	state := ptrext.Of("")
	if err := pool.QueryRow(ctx, `
		SELECT sync_state
		  FROM customer_request_issue_links
		 WHERE tenant_id = $1 AND request_id = $2 AND external_key = $3`,
		tenantID, requestID, externalKey).Scan(state); err != nil {
		t.Fatalf("read issue link state: %v", err)
	}
	return ptrext.Indirect(state)
}

func payloadStringForTest(t *testing.T, payload []byte, key string) string {
	t.Helper()
	values := map[string]any{}
	if err := json.Unmarshal(payload, &values); err != nil { // ptrext:allow unmarshal-out-param
		t.Fatalf("decode payload: %v", err)
	}
	value, _ := values[key].(string)
	return value
}

func externalObjectLinkState(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tenantID string, mappingID uuid.UUID, externalKey string) string {
	t.Helper()
	state := ptrext.Of("")
	if err := pool.QueryRow(ctx, `
		SELECT sync_state
		  FROM external_object_links
		 WHERE tenant_id = $1 AND mapping_id = $2 AND external_key = $3`,
		tenantID, mappingID, externalKey).Scan(state); err != nil {
		t.Fatalf("read external object link state: %v", err)
	}
	return ptrext.Indirect(state)
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec fixture SQL: %v", err)
	}
}
