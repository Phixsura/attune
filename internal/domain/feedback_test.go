// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"strings"
	"testing"
)

// TestValidSources_WebhookAndEmail — these two source enums back the first
// two inbound adapters introduced in #66. They must round-trip ValidSources.
func TestValidSources_WebhookAndEmail(t *testing.T) {
	for _, src := range []string{"webhook", "email"} {
		if !ValidSources[src] {
			t.Errorf("ValidSources[%q] = false; want true", src)
		}
	}
}

// TestSourceDisplayName covers all source display name mappings.
func TestSourceDisplayName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   string
	}{
		{"api", "API client"},
		{"webhook", "Webhook"},
		{"email", "Email"},
		{"web", "Web Widget"},
		{"mcp", "MCP"},
		{"other", "Other"},
		{"unknown", "unknown"},
		{"custom-source", "custom-source"},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			t.Parallel()
			if got := SourceDisplayName(tt.source); got != tt.want {
				t.Errorf("SourceDisplayName(%q) = %q; want %q", tt.source, got, tt.want)
			}
		})
	}
}

// TestValidSources_AllKnownSources verifies all expected sources are valid.
func TestValidSources_AllKnownSources(t *testing.T) {
	t.Parallel()

	expected := []string{"api", "webhook", "email", "web", "mcp", "other"}
	for _, src := range expected {
		if !ValidSources[src] {
			t.Errorf("ValidSources[%q] = false; want true", src)
		}
	}

	if ValidSources["nonexistent"] {
		t.Error("ValidSources[\"nonexistent\"] = true; want false")
	}
}

// TestIngestInput_Validate covers the server-side input invariants, including
// adversarial inputs: empty/oversized content, a NUL byte (PostgreSQL TEXT
// cannot store it), and an unknown source.
func TestIngestInput_Validate(t *testing.T) {
	cases := []struct {
		name    string
		in      IngestInput
		wantErr bool
	}{
		{"ok", IngestInput{Content: "hello", Source: "api"}, false},
		{"empty content", IngestInput{Content: "", Source: "api"}, true},
		{"content too long", IngestInput{Content: strings.Repeat("a", MaxContentLen+1), Source: "api"}, true},
		{"null byte", IngestInput{Content: "ab\x00cd", Source: "api"}, true},
		{"unknown source", IngestInput{Content: "hi", Source: "bogus"}, true},
		{"content at cap", IngestInput{Content: strings.Repeat("a", MaxContentLen), Source: "api"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.in.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}
