// SPDX-License-Identifier: Apache-2.0

package amplitude

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	core "github.com/Phixsura/attune/internal/cohortsync"
)

func init() {
	core.ResetForTest()
	core.Register(providerID, "Amplitude", func() core.Provider {
		return &Adapter{} // ptrext:allow test-init
	})
}

func mustReadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParseWebhook_Create(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := mustReadFixture(t, "create.json")
	payload, err := a.ParseWebhook(body, map[string]string{"x-operation": "create"}, nil)
	if err != nil {
		t.Fatalf("ParseWebhook failed: %v", err)
	}
	if payload.ExternalCohortID != "abc123" {
		t.Errorf("ExternalCohortID = %q, want abc123", payload.ExternalCohortID)
	}
	if payload.CohortName != "Power Users" {
		t.Errorf("CohortName = %q, want Power Users", payload.CohortName)
	}
	if len(payload.Deltas) != 3 {
		t.Fatalf("Deltas len = %d, want 3", len(payload.Deltas))
	}
	for _, d := range payload.Deltas {
		if d.Action != "add" {
			t.Errorf("delta action = %q, want add", d.Action)
		}
	}
	if payload.IsFullSnapshot {
		t.Error("IsFullSnapshot should be false for Amplitude")
	}
}

func TestParseWebhook_Add(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := mustReadFixture(t, "add.json")
	payload, err := a.ParseWebhook(body, map[string]string{"x-operation": "add"}, nil)
	if err != nil {
		t.Fatalf("ParseWebhook failed: %v", err)
	}
	if len(payload.Deltas) != 2 {
		t.Fatalf("Deltas len = %d, want 2", len(payload.Deltas))
	}
	if payload.Deltas[0].ExternalUserID != "user-4" {
		t.Errorf("first user = %q, want user-4", payload.Deltas[0].ExternalUserID)
	}
}

func TestParseWebhook_Remove(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := mustReadFixture(t, "remove.json")
	payload, err := a.ParseWebhook(body, map[string]string{"x-operation": "remove"}, nil)
	if err != nil {
		t.Fatalf("ParseWebhook failed: %v", err)
	}
	if len(payload.Deltas) != 1 {
		t.Fatalf("Deltas len = %d, want 1", len(payload.Deltas))
	}
	if payload.Deltas[0].Action != "remove" {
		t.Errorf("action = %q, want remove", payload.Deltas[0].Action)
	}
}

func TestParseWebhook_FallsBackToBodyOperation(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := mustReadFixture(t, "add.json")
	// No x-operation header — should fall back to body "operation" field
	payload, err := a.ParseWebhook(body, nil, nil)
	if err != nil {
		t.Fatalf("ParseWebhook failed: %v", err)
	}
	if len(payload.Deltas) != 2 {
		t.Errorf("Deltas len = %d, want 2", len(payload.Deltas))
	}
}

func TestParseWebhook_MalformedJSON(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	_, err := a.ParseWebhook([]byte("not json"), nil, nil)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestParseWebhook_EmptyUserIDs(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := []byte(`{"cohort_id":"c1","cohort_name":"C","operation":"add","user_ids":[]}`)
	payload, err := a.ParseWebhook(body, nil, nil)
	if err != nil {
		t.Fatalf("ParseWebhook failed: %v", err)
	}
	if len(payload.Deltas) != 0 {
		t.Errorf("Deltas len = %d, want 0", len(payload.Deltas))
	}
}

func TestParseWebhook_MissingCohortID(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := []byte(`{"cohort_name":"C","operation":"add","user_ids":["u1"]}`)
	_, err := a.ParseWebhook(body, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing cohort_id")
	}
}

func TestParseWebhook_UnknownOperation(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := []byte(`{"cohort_id":"c1","operation":"unknown","user_ids":["u1"]}`)
	_, err := a.ParseWebhook(body, nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
}

func TestProvider(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	if a.Provider() != "amplitude" {
		t.Errorf("Provider() = %q, want amplitude", a.Provider())
	}
}

// ---------------------------------------------------------------------------
// Check
// ---------------------------------------------------------------------------

func TestCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/5/cohorts/list" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	a := &Adapter{client: srv.Client()} // ptrext:allow test-fixture
	conn := core.Connection{
		BaseURL:    srv.URL,
		Credential: []byte("test-api-key"),
	}
	result, err := a.Check(context.Background(), conn)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if !result.OK {
		t.Errorf("Check OK = false, want true")
	}
}

func TestCheck_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	a := &Adapter{client: srv.Client()} // ptrext:allow test-fixture
	conn := core.Connection{
		BaseURL:    srv.URL,
		Credential: []byte("bad-key"),
	}
	result, err := a.Check(context.Background(), conn)
	if err == nil {
		t.Fatal("expected error for 401 response")
	}
	if result.OK {
		t.Error("Check OK = true, want false")
	}
	if !strings.Contains(result.Error, "401") {
		t.Errorf("error %q should mention 401", result.Error)
	}
}

func TestCheck_NetworkError(t *testing.T) {
	a := &Adapter{client: http.DefaultClient} // ptrext:allow test-fixture
	conn := core.Connection{
		BaseURL:    "http://127.0.0.1:1", // nothing listening
		Credential: []byte("key"),
	}
	result, err := a.Check(context.Background(), conn)
	if err == nil {
		t.Fatal("expected error for unreachable server")
	}
	if result.OK {
		t.Error("Check OK = true, want false for network error")
	}
}

// ---------------------------------------------------------------------------
// splitPullCredential
// ---------------------------------------------------------------------------

func TestSplitPullCredential_Valid(t *testing.T) {
	key, secret, err := splitPullCredential([]byte("mykey:mysecret"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "mykey" {
		t.Errorf("key = %q, want mykey", key)
	}
	if secret != "mysecret" {
		t.Errorf("secret = %q, want mysecret", secret)
	}
}

func TestSplitPullCredential_Invalid(t *testing.T) {
	cases := []string{
		"nocolon",
		":nosecret", // empty key
		"nokey:",    // empty secret
		"",          // empty string
	}
	for _, tc := range cases {
		_, _, err := splitPullCredential([]byte(tc))
		if err == nil {
			t.Errorf("splitPullCredential(%q) should return error", tc)
		}
	}
}

// ---------------------------------------------------------------------------
// PullCohort
// ---------------------------------------------------------------------------

func TestPullCohort_Success(t *testing.T) {
	requestID := "req-abc-123"
	cohortID := "cohort-42"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// Step 1: request export
		case r.URL.Path == fmt.Sprintf("/api/5/cohorts/request/%s", cohortID):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"request_id": requestID})

		// Step 2: poll status
		case r.URL.Path == fmt.Sprintf("/api/5/cohorts/request/%s/status", requestID):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"async_status": "COMPLETE"})

		// Step 3: download file
		case r.URL.Path == fmt.Sprintf("/api/5/cohorts/request/%s/file", requestID):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode([]string{"user-a", "user-b", "user-c"})

		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	a := &Adapter{client: srv.Client()} // ptrext:allow test-fixture
	conn := core.Connection{
		BaseURL:    srv.URL,
		Credential: []byte("apikey:secret"),
	}
	payload, err := a.PullCohort(context.Background(), conn, cohortID)
	if err != nil {
		t.Fatalf("PullCohort returned error: %v", err)
	}
	if payload.ExternalCohortID != cohortID {
		t.Errorf("ExternalCohortID = %q, want %q", payload.ExternalCohortID, cohortID)
	}
	if !payload.IsFullSnapshot {
		t.Error("IsFullSnapshot should be true for pull")
	}
	if len(payload.Deltas) != 3 {
		t.Fatalf("Deltas len = %d, want 3", len(payload.Deltas))
	}
	wantUsers := []string{"user-a", "user-b", "user-c"}
	for i, d := range payload.Deltas {
		if d.ExternalUserID != wantUsers[i] {
			t.Errorf("Deltas[%d].ExternalUserID = %q, want %q", i, d.ExternalUserID, wantUsers[i])
		}
		if d.Action != "add" {
			t.Errorf("Deltas[%d].Action = %q, want add", i, d.Action)
		}
	}
}

func TestPullCohort_ExportFailed(t *testing.T) {
	cohortID := "cohort-fail"
	requestID := "req-fail"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == fmt.Sprintf("/api/5/cohorts/request/%s", cohortID):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"request_id": requestID})

		case r.URL.Path == fmt.Sprintf("/api/5/cohorts/request/%s/status", requestID):
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]string{"async_status": "FAILED"})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	a := &Adapter{client: srv.Client()} // ptrext:allow test-fixture
	conn := core.Connection{
		BaseURL:    srv.URL,
		Credential: []byte("apikey:secret"),
	}
	_, err := a.PullCohort(context.Background(), conn, cohortID)
	if err == nil {
		t.Fatal("expected error when export status is FAILED")
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Errorf("error %q should mention 'failed'", err.Error())
	}
}

func TestPullCohort_BadCredential(t *testing.T) {
	a := &Adapter{client: http.DefaultClient} // ptrext:allow test-fixture
	conn := core.Connection{
		BaseURL:    "http://127.0.0.1:1",
		Credential: []byte("nocolon"),
	}
	_, err := a.PullCohort(context.Background(), conn, "c1")
	if err == nil {
		t.Fatal("expected error for bad credential format")
	}
	if !strings.Contains(err.Error(), "api_key:secret_key") {
		t.Errorf("error %q should mention expected format", err.Error())
	}
}
