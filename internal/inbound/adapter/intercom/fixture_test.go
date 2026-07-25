// SPDX-License-Identifier: Apache-2.0

package intercom

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/inbound"
	"github.com/Phixsura/attune/internal/inbound/inboundtest"
	"github.com/Phixsura/attune/internal/pkg/nethardening"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

// loadFixture reads a testdata JSON file.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("load fixture %s: %v", name, err)
	}
	return data
}

// newFixtureServer serves Intercom API responses from testdata files.
func newFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/me":
			fmt.Fprint(w, `{"type":"admin","email":"ops@acme.com","app":{"type":"app","id_code":"ws-fixture","name":"Acme","region":"US"}}`)
		case r.URL.Path == "/conversations/search" && r.Method == http.MethodPost:
			w.Write(loadFixture(t, "conversations_search.json")) //nolint:errcheck
		case strings.HasPrefix(r.URL.Path, "/conversations/"):
			id := strings.TrimPrefix(r.URL.Path, "/conversations/")
			data, err := os.ReadFile("testdata/conversation_" + id + ".json")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Write(data) //nolint:errcheck
		case r.URL.Path == "/companies/co-9":
			fmt.Fprint(w, `{"type":"company","id":"co-9","name":"Customer Co","monthly_spend":1200,"size":85,"industry":"Software","plan":{"type":"plan","id":"p1","name":"Pro"}}`)
		case r.URL.Path == "/contacts/search" && r.Method == http.MethodPost:
			w.Write(loadFixture(t, "contacts_search.json")) //nolint:errcheck
		default:
			http.NotFound(w, r)
		}
	}))
}

// setupTestEgress allows loopback and sets the test override so host
// validation (*.intercom.io) is skipped for httptest servers.
func setupTestEgress(t *testing.T, srvURL string) {
	t.Helper()
	SetEgressPolicy(nethardening.Policy{AllowLoopback: true})
	SetAPIBaseURL(srvURL)
	t.Cleanup(func() {
		SetEgressPolicy(nethardening.Policy{})
		SetAPIBaseURL("")
	})
}

// fixtureHarness bundles the shared state assembled by setupFixturePipeline.
type fixtureHarness struct {
	a          *adapter
	src        inbound.Source
	sources    *fakeSourcesWithConfig
	ingestFake *inboundtest.FakeIngest
	configBlob []byte
}

func setupFixturePipeline(t *testing.T) fixtureHarness {
	t.Helper()
	srv := newFixtureServer(t)
	t.Cleanup(srv.Close)
	setupTestEgress(t, srv.URL)

	fixedTime := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	origNow := nowFn
	nowFn = func() time.Time { return fixedTime }
	t.Cleanup(func() { nowFn = origNow })

	secrets := inboundtest.FakeSecrets{}
	tokenEnc, _ := secrets.Encrypt([]byte("fixture-token"))
	cfg := Config{
		Version:              ConfigVersion,
		Region:               "us",
		AccessTokenEncrypted: tokenEnc,
		WorkspaceID:          "ws-fixture",
		StartFrom:            "full",
	}
	configBlob := buildTestConfig(t, cfg, secrets)

	sources := newFakeSourcesWithConfig()
	ingestFake := &inboundtest.FakeIngest{} // ptrext:allow mutex-identity
	deps := inbound.Deps{
		Mux:     ptrext.Of(inboundtest.FakeMux{}),
		Ingest:  inbound.IngestFunc(ingestFake.Ingest),
		Sources: sources,
		Secrets: secrets,
		Metrics: ptrext.Of(inboundtest.FakeMetrics{}),
	}

	src := inbound.Source{
		ID: "fixture-src", TenantID: "fixture-tenant", Channel: channelName,
		Name: "Fixture Intercom", Slug: "fixture-intercom",
		Config: configBlob, Enabled: true,
	}
	sources.Put("t1", src)

	a := NewAdapter().(*adapter)
	a.deps = deps
	return fixtureHarness{a: a, src: src, sources: sources, ingestFake: ingestFake, configBlob: configBlob}
}

// TestFixture_FullPipeline verifies the complete httptest → adapter →
// IngestInput data flow using real JSON fixture files. This is the E2E
// proof that Intercom conversations generate feedback items with correct
// support metadata (issue #230 acceptance criteria).
func TestFixture_FullPipeline(t *testing.T) {
	h := setupFixturePipeline(t)
	h.a.pollSource(context.Background(), h.src)

	if len(h.ingestFake.Calls) != 2 {
		t.Fatalf("expected 2 ingested conversations, got %d", len(h.ingestFake.Calls))
	}
	verifyFixtureConversation501(t, h.ingestFake.Calls[0].In)
	verifyFixtureConversation502(t, h.ingestFake.Calls[1].In)
	verifyFixtureWatermarkAndReplay(t, h)
}

// verifyFixtureConversation501: full thread, note excluded, roles tagged.
func verifyFixtureConversation501(t *testing.T, first domain.IngestInput) {
	t.Helper()
	if first.Source != "intercom" {
		t.Errorf("Source = %q", first.Source)
	}
	if first.SourceUser != "alice@customer.com" {
		t.Errorf("SourceUser = %q", first.SourceUser)
	}
	for _, want := range []string{
		"Cannot export dashboard",
		"[customer] The PDF export button is greyed out",
		"[agent] Thanks for reporting!",
		"[customer] Chrome. Also happens in Safari.",
	} {
		if !strings.Contains(first.Content, want) {
			t.Errorf("content missing %q: %q", want, first.Content)
		}
	}
	if strings.Contains(first.Content, "plan-gating bug") {
		t.Errorf("internal note leaked into content: %q", first.Content)
	}
	if first.Type != "bug_report" {
		t.Errorf("Type = %q, want bug_report (priority hint)", first.Type)
	}
	if first.PageURL != "https://app.intercom.com/a/inbox/ws-fixture/inbox/conversation/501" {
		t.Errorf("PageURL = %q", first.PageURL)
	}
	if first.IdempotencyKey != "intercom_ws-fixture_501_1753400000" {
		t.Errorf("IdempotencyKey = %q", first.IdempotencyKey)
	}
	verifyFixtureMeta501(t, first.SourceMeta)
}

func verifyFixtureMeta501(t *testing.T, meta map[string]any) {
	t.Helper()
	wantEqual := map[string]any{
		"intercom_contact_external_id":    "cust-70",
		"intercom_company_name":           "Customer Co",
		"intercom_state":                  "open",
		"intercom_customer_message_count": 2,
		"intercom_agent_message_count":    1,
	}
	for k, want := range wantEqual {
		if meta[k] != want {
			t.Errorf("%s = %v, want %v", k, meta[k], want)
		}
	}
	if !strings.Contains(meta["intercom_tags"].(string), "export") {
		t.Errorf("tags = %v", meta["intercom_tags"])
	}
	if !strings.Contains(meta["intercom_custom_attributes"].(string), "EXPORT_403") {
		t.Errorf("custom_attributes = %v", meta["intercom_custom_attributes"])
	}
	// Company profile resolved via GET /companies/{id} (revenue context).
	if meta["intercom_company_monthly_spend"] != 1200 {
		t.Errorf("company_monthly_spend = %v", meta["intercom_company_monthly_spend"])
	}
	if meta["intercom_company_plan"] != "Pro" {
		t.Errorf("company_plan = %v", meta["intercom_company_plan"])
	}
	if meta["intercom_source_url"] != "https://app.customer.com/dashboards/42" {
		t.Errorf("source_url = %v", meta["intercom_source_url"])
	}
}

// verifyFixtureConversation502: subject fallback, Fin bot tagged, AI flag.
func verifyFixtureConversation502(t *testing.T, second domain.IngestInput) {
	t.Helper()
	if !strings.Contains(second.Content, "Feature request: dark mode") {
		t.Errorf("subject fallback missing: %q", second.Content)
	}
	if !strings.Contains(second.Content, "[bot] I can pass this to the product team") {
		t.Errorf("bot tagging missing: %q", second.Content)
	}
	if second.SourceMeta["intercom_ai_agent_participated"] != true {
		t.Errorf("ai_agent_participated = %v", second.SourceMeta["intercom_ai_agent_participated"])
	}
	if second.SourceMeta["intercom_ai_resolution_state"] != "escalated" {
		t.Errorf("ai_resolution_state = %v", second.SourceMeta["intercom_ai_resolution_state"])
	}
	if second.SourceMeta["intercom_ai_rating"] != 2 {
		t.Errorf("ai_rating = %v", second.SourceMeta["intercom_ai_rating"])
	}
	// Fin escalated → complaint hint.
	if second.Type != "complaint" {
		t.Errorf("Type = %q, want complaint (Fin escalated)", second.Type)
	}
}

// verifyFixtureWatermarkAndReplay: watermark advanced; replay ingests 0.
func verifyFixtureWatermarkAndReplay(t *testing.T, h fixtureHarness) {
	t.Helper()
	stored, err := h.sources.Get(context.Background(), "fixture-src")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.State.LastUID != 1753410000 {
		t.Errorf("LastUID = %d, want 1753410000", stored.State.LastUID)
	}

	// Replay: same fixtures re-polled — the client-side watermark filter
	// must skip everything (replay-safe dedupe acceptance criterion).
	before := len(h.ingestFake.Calls)
	stored.Config = h.configBlob
	h.a.pollSource(context.Background(), stored)
	if len(h.ingestFake.Calls) != before {
		t.Errorf("replay ingested %d new rows, want 0 (watermark filter)", len(h.ingestFake.Calls)-before)
	}
}
