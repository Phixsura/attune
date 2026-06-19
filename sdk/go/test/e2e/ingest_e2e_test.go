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
	"os"
	"sync"
	"testing"
	"time"

	attune "github.com/Phixsura/attune/sdk/go"
)

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
