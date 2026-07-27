//go:build integration

// SPDX-License-Identifier: Apache-2.0

// ptrext:file-allow test-fixtures

package cohortsync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Phixsura/attune/internal/cohortsync"
	_ "github.com/Phixsura/attune/internal/cohortsync/adapter/amplitude"
	_ "github.com/Phixsura/attune/internal/cohortsync/adapter/mixpanel"
	"github.com/Phixsura/attune/internal/handlers/cohortsyncwebhook"
	"github.com/Phixsura/attune/internal/infra/secretstore"
	cohortsyncrepo "github.com/Phixsura/attune/internal/repo/cohortsync"
	cohortsyncservice "github.com/Phixsura/attune/internal/service/cohortsync"
	auditlogsvc "github.com/Phixsura/attune/internal/service/auditlog"
	"github.com/Phixsura/attune/internal/testdb"
)

// TestCohortSyncEndToEnd exercises the full vertical slice: webhook HTTP →
// adapter parse → service delta → Postgres upsert → feedback filter query.
func TestCohortSyncEndToEnd(t *testing.T) {
	ctx := context.Background()
	pool := testdb.NewPool(t)

	// Generate a fresh Tink keyset for credential encryption.
	keysetJSON, err := secretstore.GenerateAES256GCMKeysetJSON()
	if err != nil {
		t.Fatalf("generate keyset: %v", err)
	}
	store, err := secretstore.NewTinkStoreFromJSON(keysetJSON)
	if err != nil {
		t.Fatalf("new tink store: %v", err)
	}

	// Create tenant.
	tenantID := uuid.NewString()
	mustExec(t, ctx, pool, `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $3)`,
		tenantID, "cohort-e2e", "Cohort E2E")

	// Register the Tink primary key in secret_key_registry so SQL constraints pass.
	mustExec(t, ctx, pool, `
		INSERT INTO secret_key_registry
		 (key_id, primary_key, status, type_url, output_prefix_type, key_material_type,
		  fingerprint_sha256, fingerprint_version)
		VALUES ($1, TRUE, 'ENABLED', 'type.googleapis.com/google.crypto.tink.AesGcmKey',
		        'TINK', 'SYMMETRIC', 'e2e-fixture', 1)
		ON CONFLICT (key_id) DO NOTHING`,
		store.PrimaryKeyID())

	// Seed feedback rows with distinct source_user values.
	// user_id is the composed form, subject_key is the raw upstream identifier.
	for _, user := range []string{"user-1", "user-2", "user-3"} {
		composedUID := "ext_" + uuid.NewString() + ":" + user
		mustExec(t, ctx, pool, `
			INSERT INTO user_feedback (tenant_id, content, source, user_id, subject_key, subject_display)
			VALUES ($1, $2, 'api', $3, $4, $4)`,
			tenantID, "feedback from "+user, composedUID, user)
	}

	// Build the service + handler.
	repo := cohortsyncrepo.New(pool)
	svc := cohortsyncservice.New(repo, store)

	// Create a cohort source via the service (encrypts credential).
	credential := "test-api-key-" + uuid.NewString()[:8]
	source, err := svc.CreateSource(ctx, cohortsyncservice.CreateSourceInput{
		TenantID:   tenantID,
		Provider:   "amplitude",
		Name:       "Test Amplitude",
		AuthType:   "api_key",
		Credential: credential,
		Enabled:    true,
		Actor:      cohortsyncservice.Actor{Type: "admin", ID: "e2e"},
		AuditActor: auditlogsvc.Actor{Type: "admin", ID: "e2e"},
	})
	if err != nil {
		t.Fatalf("CreateSource: %v", err)
	}
	t.Logf("created source: id=%s provider=%s", source.ID, source.Provider)

	// Boot the webhook handler.
	handler := cohortsyncwebhook.NewHandler(svc)
	mux := chi.NewMux()
	mux.Mount("/v1/cohort-sync", handler.Routes())
	server := httptest.NewServer(mux)
	defer server.Close()

	baseURL := server.URL + "/v1/cohort-sync"

	// === Step 1: Amplitude create ===
	t.Run("amplitude_create", func(t *testing.T) {
		body := fmt.Sprintf(`{
			"cohort_id": "power-users",
			"cohort_name": "Power Users",
			"operation": "create",
			"user_ids": ["user-1", "user-2"],
			"user_id_type": "BY_ID"
		}`)
		resp := doWebhook(t, baseURL+"/amplitude/"+tenantID+"/"+source.ID.String()+"/create",
			credential, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("create: status=%d body=%s", resp.StatusCode, readBody(t, resp))
		}

		// Verify cohort was created.
		var cohortName string
		err := pool.QueryRow(ctx, `
			SELECT name FROM cohorts
			WHERE tenant_id = $1 AND external_cohort_id = 'power-users'`,
			tenantID).Scan(&cohortName)
		if err != nil {
			t.Fatalf("cohort not found: %v", err)
		}
		if cohortName != "Power Users" {
			t.Errorf("cohort name = %q, want Power Users", cohortName)
		}

		// Verify 2 active memberships.
		var memberCount int
		pool.QueryRow(ctx, `
			SELECT count(*) FROM cohort_memberships
			WHERE tenant_id = $1 AND left_at IS NULL`,
			tenantID).Scan(&memberCount)
		if memberCount != 2 {
			t.Errorf("active members = %d, want 2", memberCount)
		}
	})

	// === Step 2: Amplitude add ===
	t.Run("amplitude_add", func(t *testing.T) {
		body := `{
			"cohort_id": "power-users",
			"cohort_name": "Power Users",
			"operation": "add",
			"user_ids": ["user-3"],
			"user_id_type": "BY_ID"
		}`
		resp := doWebhook(t, baseURL+"/amplitude/"+tenantID+"/"+source.ID.String()+"/add",
			credential, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("add: status=%d body=%s", resp.StatusCode, readBody(t, resp))
		}

		var memberCount int
		pool.QueryRow(ctx, `
			SELECT count(*) FROM cohort_memberships
			WHERE tenant_id = $1 AND left_at IS NULL`,
			tenantID).Scan(&memberCount)
		if memberCount != 3 {
			t.Errorf("active members after add = %d, want 3", memberCount)
		}
	})

	// === Step 3: Amplitude remove ===
	t.Run("amplitude_remove", func(t *testing.T) {
		body := `{
			"cohort_id": "power-users",
			"cohort_name": "Power Users",
			"operation": "remove",
			"user_ids": ["user-1"],
			"user_id_type": "BY_ID"
		}`
		resp := doWebhook(t, baseURL+"/amplitude/"+tenantID+"/"+source.ID.String()+"/remove",
			credential, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("remove: status=%d body=%s", resp.StatusCode, readBody(t, resp))
		}

		// user-1 should be departed.
		var leftAt *string
		pool.QueryRow(ctx, `
			SELECT left_at::text FROM cohort_memberships
			WHERE tenant_id = $1 AND external_user_id = 'user-1'`,
			tenantID).Scan(&leftAt)
		if leftAt == nil {
			t.Error("user-1 should have left_at set after remove")
		}

		// user-2 and user-3 should still be active.
		var activeCount int
		pool.QueryRow(ctx, `
			SELECT count(*) FROM cohort_memberships
			WHERE tenant_id = $1 AND left_at IS NULL`,
			tenantID).Scan(&activeCount)
		if activeCount != 2 {
			t.Errorf("active members after remove = %d, want 2 (user-2, user-3)", activeCount)
		}
	})

	// === Step 4: Feedback filter by cohort ===
	t.Run("feedback_filter_by_cohort", func(t *testing.T) {
		// Get the cohort ID.
		var cohortID uuid.UUID
		err := pool.QueryRow(ctx, `
			SELECT id FROM cohorts
			WHERE tenant_id = $1 AND external_cohort_id = 'power-users'`,
			tenantID).Scan(&cohortID)
		if err != nil {
			t.Fatalf("get cohort ID: %v", err)
		}

		// Query feedback filtered by cohort — should return user-2 and user-3 only.
		rows, err := pool.Query(ctx, `
			SELECT f.subject_key
			FROM user_feedback f
			WHERE f.tenant_id = $1
			  AND f.subject_key <> ''
			  AND EXISTS (
			    SELECT 1 FROM cohort_memberships cm
			    WHERE cm.tenant_id = f.tenant_id
			      AND cm.cohort_id = $2
			      AND cm.external_user_id = f.subject_key
			      AND cm.left_at IS NULL
			  )
			ORDER BY f.subject_key`, tenantID, cohortID)
		if err != nil {
			t.Fatalf("feedback filter query: %v", err)
		}
		defer rows.Close()

		var matched []string
		for rows.Next() {
			var sk string
			if err := rows.Scan(&sk); err != nil {
				t.Fatalf("scan: %v", err)
			}
			matched = append(matched, sk)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows err: %v", err)
		}

		if len(matched) != 2 {
			t.Fatalf("matched feedback = %v, want [user-2, user-3]", matched)
		}
		if matched[0] != "user-2" || matched[1] != "user-3" {
			t.Errorf("matched = %v, want [user-2, user-3]", matched)
		}
	})

	// === Step 5: Mixpanel members (full snapshot) ===
	t.Run("mixpanel_full_snapshot", func(t *testing.T) {
		// Create a Mixpanel source.
		mpSource, err := svc.CreateSource(ctx, cohortsyncservice.CreateSourceInput{
			TenantID:   tenantID,
			Provider:   "mixpanel",
			Name:       "Test Mixpanel",
			AuthType:   "api_key",
			Credential: credential,
			Enabled:    true,
			Actor:      cohortsyncservice.Actor{Type: "admin", ID: "e2e"},
			AuditActor: auditlogsvc.Actor{Type: "admin", ID: "e2e"},
		})
		if err != nil {
			t.Fatalf("CreateSource mixpanel: %v", err)
		}

		body := `{
			"action": "members",
			"cohort_id": "enterprise",
			"cohort_name": "Enterprise",
			"members": [
				{"mixpanel_distinct_id": "user-2", "email": "u2@test.com", "first_name": "User", "last_name": "Two"},
				{"mixpanel_distinct_id": "user-3", "email": "u3@test.com", "first_name": "User", "last_name": "Three"}
			]
		}`
		resp := doWebhook(t, baseURL+"/mixpanel/"+tenantID+"/"+mpSource.ID.String(),
			credential, body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("mixpanel members: status=%d body=%s", resp.StatusCode, readBody(t, resp))
		}

		// Verify enterprise cohort has 2 members.
		var mpCount int
		pool.QueryRow(ctx, `
			SELECT count(*) FROM cohort_memberships cm
			JOIN cohorts c ON c.id = cm.cohort_id
			WHERE cm.tenant_id = $1
			  AND c.external_cohort_id = 'enterprise'
			  AND cm.left_at IS NULL`, tenantID).Scan(&mpCount)
		if mpCount != 2 {
			t.Errorf("enterprise cohort active members = %d, want 2", mpCount)
		}
	})

	// === Step 6: Auth failure ===
	t.Run("auth_failure", func(t *testing.T) {
		body := `{"cohort_id":"x","operation":"add","user_ids":["u1"]}`
		resp := doWebhook(t, baseURL+"/amplitude/"+tenantID+"/"+source.ID.String()+"/add",
			"wrong-key", body)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("auth failure: status=%d, want 401", resp.StatusCode)
		}
	})

	// === Step 7: Sync run history ===
	t.Run("sync_runs_recorded", func(t *testing.T) {
		var runCount int
		pool.QueryRow(ctx, `
			SELECT count(*) FROM cohort_sync_runs
			WHERE tenant_id = $1`, tenantID).Scan(&runCount)
		if runCount < 3 {
			t.Errorf("sync runs = %d, want >= 3 (create + add + remove)", runCount)
		}
	})

	// === Step 8: Verify registered adapters ===
	t.Run("adapters_registered", func(t *testing.T) {
		entries := cohortsync.Providers()
		if len(entries) < 2 {
			t.Fatalf("registered providers = %d, want >= 2", len(entries))
		}
		found := map[string]bool{}
		for _, e := range entries {
			found[e.Provider] = true
		}
		if !found["amplitude"] {
			t.Error("amplitude adapter not registered")
		}
		if !found["mixpanel"] {
			t.Error("mixpanel adapter not registered")
		}
	})
}

// ---------- helpers ----------

func doWebhook(t *testing.T, url, credential, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(credential, "")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(raw)
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec fixture SQL: %v", err)
	}
}

// Suppress unused import warning for JSON.
var _ = json.Marshal
