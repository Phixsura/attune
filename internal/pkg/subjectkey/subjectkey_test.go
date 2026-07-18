package subjectkey

import "testing"

func TestNormalize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sourceUser  string
		legacyID    string
		wantKey     string
		wantDisplay string
	}{
		{name: "prefers source user", sourceUser: "alice@example.com", legacyID: "ext_1:bob@example.com", wantKey: "alice@example.com", wantDisplay: "alice@example.com"},
		{name: "trims source user", sourceUser: " alice@example.com ", legacyID: "ext_1:bob@example.com", wantKey: "alice@example.com", wantDisplay: "alice@example.com"},
		{name: "parses legacy composed id", legacyID: "ext_1:bob@example.com", wantKey: "bob@example.com", wantDisplay: "bob@example.com"},
		{name: "trims parsed legacy composed id", legacyID: " ext_1: bob@example.com ", wantKey: "bob@example.com", wantDisplay: "bob@example.com"},
		{name: "opaque legacy id fallback", legacyID: "legacy-user-42", wantKey: "legacy-user-42", wantDisplay: "legacy-user-42"},
		{name: "legacy ext id without subject falls back to raw id", legacyID: "ext_1", wantKey: "ext_1", wantDisplay: "ext_1"},
		{name: "blank identifiers return empty subject", sourceUser: " ", legacyID: " ", wantKey: "", wantDisplay: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotKey, gotDisplay := Normalize(tt.sourceUser, tt.legacyID)
			if gotKey != tt.wantKey || gotDisplay != tt.wantDisplay {
				t.Fatalf("Normalize(%q, %q) = (%q, %q), want (%q, %q)", tt.sourceUser, tt.legacyID, gotKey, gotDisplay, tt.wantKey, tt.wantDisplay)
			}
		})
	}
}

func TestHash(t *testing.T) {
	t.Parallel()

	first := Hash("tenant-a", "alice@example.com")
	second := Hash("tenant-a", "alice@example.com")
	otherTenant := Hash("tenant-b", "alice@example.com")

	if first == "" {
		t.Fatal("Hash returned empty string")
	}
	if first != second {
		t.Fatal("Hash should be deterministic")
	}
	if first == otherTenant {
		t.Fatal("Hash should be tenant-scoped")
	}
}
