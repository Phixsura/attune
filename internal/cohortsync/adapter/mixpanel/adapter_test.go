// SPDX-License-Identifier: Apache-2.0

package mixpanel

import (
	"context"
	"encoding/json"
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
	core.Register(providerID, "Mixpanel", func() core.Provider {
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

func TestParseWebhook_Members(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := mustReadFixture(t, "members.json")
	payload, err := a.ParseWebhook(body, nil, nil)
	if err != nil {
		t.Fatalf("ParseWebhook failed: %v", err)
	}
	if payload.ExternalCohortID != "cohort-xyz" {
		t.Errorf("ExternalCohortID = %q, want cohort-xyz", payload.ExternalCohortID)
	}
	if payload.CohortName != "Enterprise Accounts" {
		t.Errorf("CohortName = %q", payload.CohortName)
	}
	if !payload.IsFullSnapshot {
		t.Error("IsFullSnapshot should be true for members action")
	}
	if len(payload.Deltas) != 2 {
		t.Fatalf("Deltas len = %d, want 2", len(payload.Deltas))
	}
	d := payload.Deltas[0]
	if d.ExternalUserID != "uid-1" {
		t.Errorf("ExternalUserID = %q, want uid-1", d.ExternalUserID)
	}
	if d.Email != "alice@example.com" {
		t.Errorf("Email = %q", d.Email)
	}
	if d.DisplayName != "Alice Smith" {
		t.Errorf("DisplayName = %q, want Alice Smith", d.DisplayName)
	}
	if d.Action != "add" {
		t.Errorf("Action = %q, want add", d.Action)
	}
}

func TestParseWebhook_AddMembers(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := mustReadFixture(t, "add_members.json")
	payload, err := a.ParseWebhook(body, nil, nil)
	if err != nil {
		t.Fatalf("ParseWebhook failed: %v", err)
	}
	if payload.IsFullSnapshot {
		t.Error("IsFullSnapshot should be false for add_members")
	}
	if len(payload.Deltas) != 1 {
		t.Fatalf("Deltas len = %d, want 1", len(payload.Deltas))
	}
	if payload.Deltas[0].Action != "add" {
		t.Errorf("Action = %q, want add", payload.Deltas[0].Action)
	}
}

func TestParseWebhook_RemoveMembers(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := mustReadFixture(t, "remove_members.json")
	payload, err := a.ParseWebhook(body, nil, nil)
	if err != nil {
		t.Fatalf("ParseWebhook failed: %v", err)
	}
	if payload.IsFullSnapshot {
		t.Error("IsFullSnapshot should be false for remove_members")
	}
	if len(payload.Deltas) != 1 {
		t.Fatalf("Deltas len = %d, want 1", len(payload.Deltas))
	}
	if payload.Deltas[0].Action != "remove" {
		t.Errorf("Action = %q, want remove", payload.Deltas[0].Action)
	}
}

func TestParseWebhook_MalformedJSON(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	_, err := a.ParseWebhook([]byte("bad"), nil, nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseWebhook_UnknownAction(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := []byte(`{"action":"update","cohort_id":"c1","members":[]}`)
	_, err := a.ParseWebhook(body, nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestParseWebhook_MissingCohortID(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := []byte(`{"action":"members","members":[]}`)
	_, err := a.ParseWebhook(body, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing cohort_id")
	}
}

func TestParseWebhook_SkipsEmptyDistinctID(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	body := []byte(`{"action":"add_members","cohort_id":"c1","members":[{"mixpanel_distinct_id":"","email":"x@y.com"}]}`)
	payload, err := a.ParseWebhook(body, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(payload.Deltas) != 0 {
		t.Errorf("expected 0 deltas for empty distinct_id, got %d", len(payload.Deltas))
	}
}

func TestBuildDisplayName(t *testing.T) {
	cases := []struct {
		first, last, want string
	}{
		{"Alice", "Smith", "Alice Smith"},
		{"Alice", "", "Alice"},
		{"", "Smith", "Smith"},
		{"", "", ""},
		{"  Bob  ", "  Jones  ", "Bob Jones"},
	}
	for _, tc := range cases {
		got := buildDisplayName(tc.first, tc.last)
		if got != tc.want {
			t.Errorf("buildDisplayName(%q, %q) = %q, want %q", tc.first, tc.last, got, tc.want)
		}
	}
}

func TestProvider(t *testing.T) {
	a := &Adapter{} // ptrext:allow test-fixture
	if a.Provider() != "mixpanel" {
		t.Errorf("Provider() = %q, want mixpanel", a.Provider())
	}
}

// ---------------------------------------------------------------------------
// Check
// ---------------------------------------------------------------------------

func TestCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/2.0/engage" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		if r.URL.Query().Get("page_size") != "0" {
			t.Errorf("page_size = %q, want 0", r.URL.Query().Get("page_size"))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[],"total":0}`))
	}))
	defer srv.Close()

	a := &Adapter{client: srv.Client()} // ptrext:allow test-fixture
	conn := core.Connection{
		BaseURL:    srv.URL,
		Credential: []byte("test-sa-secret"),
	}
	result, err := a.Check(context.Background(), conn)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if !result.OK {
		t.Error("Check OK = false, want true")
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

// ---------------------------------------------------------------------------
// PullCohort
// ---------------------------------------------------------------------------

func TestPullCohort_Success(t *testing.T) {
	cohortID := "12345"
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/2.0/engage" {
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		callCount++
		w.WriteHeader(http.StatusOK)

		switch callCount {
		case 1:
			// First page: return 2 results, total=3
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []map[string]interface{}{
					{"$distinct_id": "u1", "$properties": map[string]string{"$email": "a@b.com", "$first_name": "Alice", "$last_name": "Smith"}},
					{"$distinct_id": "u2", "$properties": map[string]string{"$email": "c@d.com", "$first_name": "Bob", "$last_name": ""}},
				},
				"total":      3,
				"session_id": "sess-abc",
			})
		case 2:
			// Second page: return 1 result to satisfy total
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"results": []map[string]interface{}{
					{"$distinct_id": "u3", "$properties": map[string]string{"$email": "", "$first_name": "", "$last_name": "Jones"}},
				},
				"total":      3,
				"session_id": "sess-abc",
			})
		default:
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"results":    []interface{}{},
				"total":      3,
				"session_id": "sess-abc",
			})
		}
	}))
	defer srv.Close()

	a := &Adapter{client: srv.Client()} // ptrext:allow test-fixture
	conn := core.Connection{
		BaseURL:    srv.URL,
		Credential: []byte("user:secret"),
	}
	payload, err := a.PullCohort(context.Background(), conn, cohortID)
	if err != nil {
		t.Fatalf("PullCohort returned error: %v", err)
	}
	if payload.ExternalCohortID != cohortID {
		t.Errorf("ExternalCohortID = %q, want %q", payload.ExternalCohortID, cohortID)
	}
	if !payload.IsFullSnapshot {
		t.Error("IsFullSnapshot should be true")
	}
	if len(payload.Deltas) != 3 {
		t.Fatalf("Deltas len = %d, want 3", len(payload.Deltas))
	}

	// Verify first user
	if payload.Deltas[0].ExternalUserID != "u1" {
		t.Errorf("Deltas[0].ExternalUserID = %q, want u1", payload.Deltas[0].ExternalUserID)
	}
	if payload.Deltas[0].Email != "a@b.com" {
		t.Errorf("Deltas[0].Email = %q, want a@b.com", payload.Deltas[0].Email)
	}
	if payload.Deltas[0].DisplayName != "Alice Smith" {
		t.Errorf("Deltas[0].DisplayName = %q, want Alice Smith", payload.Deltas[0].DisplayName)
	}
	if payload.Deltas[0].Action != "add" {
		t.Errorf("Deltas[0].Action = %q, want add", payload.Deltas[0].Action)
	}

	// Verify third user (last name only)
	if payload.Deltas[2].DisplayName != "Jones" {
		t.Errorf("Deltas[2].DisplayName = %q, want Jones", payload.Deltas[2].DisplayName)
	}
}

func TestPullCohort_BadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := &Adapter{client: srv.Client()} // ptrext:allow test-fixture
	conn := core.Connection{
		BaseURL:    srv.URL,
		Credential: []byte("user:secret"),
	}
	_, err := a.PullCohort(context.Background(), conn, "c1")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q should mention 500", err.Error())
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
	if !strings.Contains(err.Error(), "username:secret") {
		t.Errorf("error %q should mention expected format", err.Error())
	}
}

// ---------------------------------------------------------------------------
// personsToDelta
// ---------------------------------------------------------------------------

func TestPersonsToDelta(t *testing.T) {
	persons := []engagePerson{
		{
			DistinctID: "uid-1",
			Properties: engagePersonProperties{Email: "a@b.com", FirstName: "Alice", LastName: "Smith"},
		},
		{
			DistinctID: "uid-2",
			Properties: engagePersonProperties{Email: "", FirstName: "", LastName: ""},
		},
		{
			DistinctID: "", // should be skipped
			Properties: engagePersonProperties{Email: "skip@me.com"},
		},
	}
	deltas := personsToDelta(persons)
	if len(deltas) != 2 {
		t.Fatalf("len(deltas) = %d, want 2 (empty distinct_id skipped)", len(deltas))
	}
	if deltas[0].ExternalUserID != "uid-1" {
		t.Errorf("deltas[0].ExternalUserID = %q, want uid-1", deltas[0].ExternalUserID)
	}
	if deltas[0].Email != "a@b.com" {
		t.Errorf("deltas[0].Email = %q, want a@b.com", deltas[0].Email)
	}
	if deltas[0].DisplayName != "Alice Smith" {
		t.Errorf("deltas[0].DisplayName = %q, want Alice Smith", deltas[0].DisplayName)
	}
	if deltas[0].Action != "add" {
		t.Errorf("deltas[0].Action = %q, want add", deltas[0].Action)
	}
	// Second user: no name fields → empty display name
	if deltas[1].DisplayName != "" {
		t.Errorf("deltas[1].DisplayName = %q, want empty", deltas[1].DisplayName)
	}
}
