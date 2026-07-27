// SPDX-License-Identifier: Apache-2.0

package amplitude

import (
	"os"
	"path/filepath"
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
