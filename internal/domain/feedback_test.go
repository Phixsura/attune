// SPDX-License-Identifier: Apache-2.0

package domain

import (
	"strings"
	"testing"
)

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
			err := c.in.Validate(nil) // nil-guard → DefaultSourceSet
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}
