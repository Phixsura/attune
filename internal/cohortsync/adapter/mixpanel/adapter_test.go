// SPDX-License-Identifier: Apache-2.0

package mixpanel

import (
	"os"
	"path/filepath"
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
