//go:build e2e

// Package e2e exercises the published Go SDK against a real, running attune
// server. It is gated behind the `e2e` build tag so `go test ./...` skips it;
// scripts/e2e.sh boots the server + Postgres and runs it with `-tags e2e`.
//
// Required env:
//
//	ATTUNE_E2E_BASE_URL  base URL of the live server (e.g. http://127.0.0.1:8097)
//	ATTUNE_E2E_API_KEY   an ingest:write key
//	ATTUNE_E2E_MARKER    unique content prefix so the harness can find rows in PG
package e2e

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	attune "github.com/Phixsura/attune/sdk/go"
	attunev1 "github.com/Phixsura/attune/sdk/go/attune/v1"
)

// TestE2ERetryAgainstRealServer exercises the SDK's retry path end-to-end: a
// fault-injecting reverse proxy in front of the live server returns 503 for the
// first two attempts (Retry-After: 0 → instant retries), then forwards the third
// to the real server, which actually ingests. Proves transient failures are
// retried and eventually succeed against a real backend (not just a mock).
func TestE2ERetryAgainstRealServer(t *testing.T) {
	base := os.Getenv("ATTUNE_E2E_BASE_URL")
	key := os.Getenv("ATTUNE_E2E_API_KEY")
	marker := os.Getenv("ATTUNE_E2E_MARKER")
	if base == "" || key == "" || marker == "" {
		t.Skip("ATTUNE_E2E_* not set")
	}
	target, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}

	var hits atomic.Int32
	rp := httputil.NewSingleHostReverseProxy(target)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= 2 {
			w.Header().Set("Retry-After", "0") // retry immediately
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		rp.ServeHTTP(w, r) // third attempt → real server
	}))
	defer proxy.Close()

	c, err := attune.New(proxy.URL, key, attune.WithMaxRetries(2))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := c.Ingest(context.Background(), attune.IngestInput{Content: marker + " retry-then-ok"})
	if err != nil {
		t.Fatalf("Ingest should succeed after 2 retries: %v", err)
	}
	if res.ID == "" {
		t.Fatal("empty id after successful retry")
	}
	if got := hits.Load(); got != 3 {
		t.Errorf("server saw %d requests, want 3 (two 503s + one success)", got)
	}
}

func newClient(t *testing.T) (*attune.Client, string) {
	t.Helper()
	base := os.Getenv("ATTUNE_E2E_BASE_URL")
	key := os.Getenv("ATTUNE_E2E_API_KEY")
	marker := os.Getenv("ATTUNE_E2E_MARKER")
	if base == "" || key == "" || marker == "" {
		t.Skip("ATTUNE_E2E_BASE_URL / ATTUNE_E2E_API_KEY / ATTUNE_E2E_MARKER not set")
	}
	c, err := attune.New(base, key, attune.WithTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, marker
}

func TestE2EBasicIngest(t *testing.T) {
	c, marker := newClient(t)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		res, err := c.Ingest(ctx, attune.IngestInput{Content: marker + " basic"})
		if err != nil {
			t.Fatalf("basic ingest %d: %v", i, err)
		}
		if res.ID == "" {
			t.Fatalf("basic ingest %d: empty id", i)
		}
		if res.EnrichmentStatus != "pending" {
			t.Errorf("basic ingest %d: status = %q, want pending", i, res.EnrichmentStatus)
		}
	}
}

func TestE2EFullFieldsIngest(t *testing.T) {
	c, marker := newClient(t)
	res, err := c.Ingest(context.Background(), attune.IngestInput{
		Content:    marker + " full-fields",
		Source:     "web",
		SourceUser: "e2e-user-42",
		PageURL:    "https://app.example.com/settings",
		SourceMeta: map[string]any{"plan": "pro"},
	})
	if err != nil {
		t.Fatalf("full-fields ingest: %v", err)
	}
	if res.ID == "" {
		t.Fatal("full-fields ingest: empty id")
	}
}

func TestE2EIdempotencyReplayDedup(t *testing.T) {
	c, marker := newClient(t)
	ctx := context.Background()
	key := "replay-" + marker
	first, err := c.Ingest(ctx, attune.IngestInput{Content: marker + " idem-replay"}, attune.WithIdempotencyKey(key))
	if err != nil {
		t.Fatalf("first replay ingest: %v", err)
	}
	second, err := c.Ingest(ctx, attune.IngestInput{Content: marker + " idem-replay"}, attune.WithIdempotencyKey(key))
	if err != nil {
		t.Fatalf("second replay ingest: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("replay returned different ids: %q vs %q (dedup failed)", first.ID, second.ID)
	}
}

// TestE2EUnauthorizedBadKey verifies the SDK surfaces a real 401 from the live
// server (error-envelope parsing against the actual API, not a mock).
func TestE2EUnauthorizedBadKey(t *testing.T) {
	base := os.Getenv("ATTUNE_E2E_BASE_URL")
	if base == "" {
		t.Skip("ATTUNE_E2E_BASE_URL not set")
	}
	c, err := attune.New(base, "att_sk_definitely_not_a_real_key_000000")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = c.Ingest(context.Background(), attune.IngestInput{Content: "should be rejected"})
	var ae *attune.AttuneError
	if !errors.As(err, &ae) {
		t.Fatalf("want *AttuneError, got %v", err)
	}
	if ae.Status != 401 || ae.Code != attune.CodeUnauthorized {
		t.Errorf("got status=%d code=%s, want 401 UNAUTHORIZED", ae.Status, ae.Code)
	}
}

// TestE2EValidationEmptyContent verifies a real 4xx validation error and that
// the requestId from the server's error envelope is parsed through.
func TestE2EValidationEmptyContent(t *testing.T) {
	c, _ := newClient(t)
	_, err := c.Ingest(context.Background(), attune.IngestInput{Content: ""})
	var ae *attune.AttuneError
	if !errors.As(err, &ae) {
		t.Fatalf("want *AttuneError, got %v", err)
	}
	if ae.Status != 400 {
		t.Errorf("got status=%d code=%s, want 400", ae.Status, ae.Code)
	}
	if ae.RequestID == "" {
		t.Error("expected a non-empty requestId parsed from the server error envelope")
	}
}

// TestE2ETagCRUD exercises the tag CRUD methods over the API-key surface against
// the real server (the e2e key is unrestricted, so it has tags:read/write).
func TestE2ETagCRUD(t *testing.T) {
	c, marker := newClient(t)
	ctx := context.Background()
	name := "sdk-tag-" + marker

	created, err := c.CreateTag(ctx, &attune.CreateTagRequest{Name: name})
	if err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	if created.GetId() == "" || created.GetName() != name {
		t.Fatalf("created tag mismatch: %+v", created)
	}

	list, err := c.ListTags(ctx, false)
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	found := false
	for _, tg := range list.GetTags() {
		if tg.GetId() == created.GetId() {
			found = true
			break
		}
	}
	if !found {
		t.Error("created tag not returned by ListTags")
	}

	// Update replaces fields, so send the full desired state (name + a valid color).
	newName, color := name+"-v2", "#3b82f6"
	upd, err := c.UpdateTag(ctx, &attune.UpdateTagRequest{Id: created.GetId(), Name: &newName, Color: &color})
	if err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	if upd.GetName() != newName {
		t.Errorf("updated name = %q, want %q", upd.GetName(), newName)
	}

	if _, err := c.ArchiveTag(ctx, created.GetId()); err != nil {
		t.Fatalf("ArchiveTag: %v", err)
	}
}

// TestE2EWorkflowCRUD exercises workflow config over the API-key surface against
// the real server: seed defaults (write), then read states + transitions.
func TestE2EWorkflowCRUD(t *testing.T) {
	c, _ := newClient(t)
	ctx := context.Background()

	if _, err := c.SeedWorkflowDefaults(ctx); err != nil {
		t.Fatalf("SeedWorkflowDefaults: %v", err)
	}
	states, err := c.ListWorkflowStates(ctx, false)
	if err != nil {
		t.Fatalf("ListWorkflowStates: %v", err)
	}
	if len(states.GetStates()) == 0 {
		t.Error("expected non-empty workflow states after seeding defaults")
	}
	if _, err := c.ListWorkflowTransitions(ctx); err != nil {
		t.Fatalf("ListWorkflowTransitions: %v", err)
	}

	// A transition referencing a state that doesn't exist in this tenant must be
	// rejected with 400 (the cross-tenant / unknown-state guard), not a 500.
	_, err = c.ReplaceWorkflowTransitions(ctx, &attune.ReplaceTransitionsRequest{
		Transitions: []*attunev1.WorkflowTransitionEdge{
			{FromStateId: "00000000-0000-0000-0000-000000000000", ToStateId: "00000000-0000-0000-0000-000000000001"},
		},
	})
	var ae *attune.AttuneError
	if !errors.As(err, &ae) || ae.Status != 400 {
		t.Errorf("ReplaceWorkflowTransitions with unknown state = %v, want 400", err)
	}
}

// TestE2EWorkflowStateCRUD drives a real Create→Update→Archive of a workflow
// state over the API-key surface (the full state config path, not just seed).
func TestE2EWorkflowStateCRUD(t *testing.T) {
	c, _ := newClient(t)
	ctx := context.Background()

	created, err := c.CreateWorkflowState(ctx, &attune.CreateStateRequest{
		Name:        "sdk_state",
		Color:       "#3b82f6",
		Category:    "active",
		Position:    99,
		DisplayName: &attunev1.I18NString{Entries: map[string]string{"en": "SDK State"}},
	})
	if err != nil {
		t.Fatalf("CreateWorkflowState: %v", err)
	}
	id := created.GetState().GetId()
	if id == "" {
		t.Fatalf("created state has no id: %+v", created.GetState())
	}

	if _, err := c.UpdateWorkflowState(ctx, &attune.UpdateStateRequest{
		Id:          id,
		Color:       strptr("#22c55e"),
		Position:    int32ptr(98),
		DisplayName: &attunev1.I18NString{Entries: map[string]string{"en": "SDK State v2"}},
	}); err != nil {
		t.Fatalf("UpdateWorkflowState: %v", err)
	}

	if _, err := c.ArchiveWorkflowState(ctx, id); err != nil {
		t.Fatalf("ArchiveWorkflowState: %v", err)
	}
}

// TestE2ECrossTenantIsolation verifies a tenant-2 key cannot see or modify
// tenant-1's tags — a core multi-tenant security property.
func TestE2ECrossTenantIsolation(t *testing.T) {
	base := os.Getenv("ATTUNE_E2E_BASE_URL")
	k1 := os.Getenv("ATTUNE_E2E_API_KEY")
	k2 := os.Getenv("ATTUNE_E2E_TENANT2_KEY")
	marker := os.Getenv("ATTUNE_E2E_MARKER")
	if base == "" || k1 == "" || k2 == "" || marker == "" {
		t.Skip("ATTUNE_E2E_TENANT2_KEY not set")
	}
	c1, _ := attune.New(base, k1)
	c2, _ := attune.New(base, k2)
	ctx := context.Background()

	tag, err := c1.CreateTag(ctx, &attune.CreateTagRequest{Name: "iso-" + marker})
	if err != nil {
		t.Fatalf("tenant1 CreateTag: %v", err)
	}

	// tenant2 must NOT see tenant1's tag.
	list2, err := c2.ListTags(ctx, true)
	if err != nil {
		t.Fatalf("tenant2 ListTags: %v", err)
	}
	for _, tg := range list2.GetTags() {
		if tg.GetId() == tag.GetId() {
			t.Fatalf("cross-tenant LEAK: tenant2 sees tenant1's tag %s", tag.GetId())
		}
	}

	// tenant2 trying to archive tenant1's tag must not affect it.
	_, _ = c2.ArchiveTag(ctx, tag.GetId())
	list1, err := c1.ListTags(ctx, false)
	if err != nil {
		t.Fatalf("tenant1 ListTags: %v", err)
	}
	found := false
	for _, tg := range list1.GetTags() {
		if tg.GetId() == tag.GetId() {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("cross-tenant WRITE: tenant2 archived tenant1's tag")
	}
}

func strptr(s string) *string { return &s }
func int32ptr(i int32) *int32 { return &i }

// TestE2EScopeDenied verifies scope gating actually works: a key restricted to
// ingest:write only is FORBIDDEN on tag/workflow writes, but can still ingest.
func TestE2EScopeDenied(t *testing.T) {
	base := os.Getenv("ATTUNE_E2E_BASE_URL")
	rkey := os.Getenv("ATTUNE_E2E_RESTRICTED_KEY")
	if base == "" || rkey == "" {
		t.Skip("ATTUNE_E2E_RESTRICTED_KEY not set")
	}
	c, err := attune.New(base, rkey)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	_, err = c.CreateTag(ctx, &attune.CreateTagRequest{Name: "should-be-denied"})
	var ae *attune.AttuneError
	if !errors.As(err, &ae) || ae.Status != 403 || ae.Code != attune.CodeForbidden {
		t.Errorf("CreateTag with ingest-only key = %v, want 403 FORBIDDEN", err)
	}

	_, err = c.SeedWorkflowDefaults(ctx)
	if !errors.As(err, &ae) || ae.Status != 403 {
		t.Errorf("SeedWorkflowDefaults with ingest-only key = %v, want 403", err)
	}

	// The same key CAN still ingest — its actual scope works.
	if _, err := c.Ingest(ctx, attune.IngestInput{Content: "restricted-key-can-ingest"}); err != nil {
		t.Errorf("ingest:write key should still ingest: %v", err)
	}
}

// TestE2ETagValidationError verifies a real validation error envelope over the
// tag route (empty name → 4xx with code/requestId parsed).
func TestE2ETagValidationError(t *testing.T) {
	c, _ := newClient(t)
	_, err := c.CreateTag(context.Background(), &attune.CreateTagRequest{Name: ""})
	var ae *attune.AttuneError
	if !errors.As(err, &ae) {
		t.Fatalf("want *AttuneError, got %v", err)
	}
	if ae.Status < 400 || ae.Status >= 500 {
		t.Errorf("empty-name CreateTag status = %d, want 4xx", ae.Status)
	}
	if ae.Code == "" {
		t.Error("expected a non-empty error code")
	}
}

func TestE2EConcurrentDedup(t *testing.T) {
	c, marker := newClient(t)
	key := "concurrent-" + marker
	const n = 8
	ids := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := c.Ingest(context.Background(),
				attune.IngestInput{Content: marker + " idem-concurrent"},
				attune.WithIdempotencyKey(key))
			ids[i], errs[i] = res.ID, err
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent ingest %d: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if ids[i] != ids[0] {
			t.Errorf("concurrent ingest returned different ids: %q vs %q (dedup failed)", ids[0], ids[i])
		}
	}
}
