// SPDX-License-Identifier: Apache-2.0

package outbound

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestContentHashSign_Deterministic(t *testing.T) {
	env := ptrext.Of(Envelope{
		Version:   "2",
		Timestamp: "2026-06-15T10:00:00Z",
		EventType: "feedback.created",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"title":   "Test feedback",
			"content": "Some content",
			"tags":    []any{"bug", "urgent"},
		},
	})

	sig1, err := ContentHashSign(env, "secret123")
	if err != nil {
		t.Fatalf("ContentHashSign: %v", err)
	}

	sig2, err := ContentHashSign(env, "secret123")
	if err != nil {
		t.Fatalf("ContentHashSign: %v", err)
	}

	if sig1 != sig2 {
		t.Errorf("signatures differ: %q vs %q", sig1, sig2)
	}

	if !strings.HasPrefix(sig1, "sha256=") {
		t.Errorf("signature should start with sha256=, got %q", sig1)
	}
}

func TestContentHashSign_FieldOrderIndependent(t *testing.T) {
	env1 := ptrext.Of(Envelope{
		Version:   "2",
		Timestamp: "2026-06-15T10:00:00Z",
		EventType: "feedback.created",
		TenantID:  "tenant-1",
		Feedback: map[string]any{
			"alpha": "first",
			"zebra": "last",
		},
	})

	env2 := ptrext.Of(Envelope{
		TenantID:  "tenant-1",
		Version:   "2",
		EventType: "feedback.created",
		Timestamp: "2026-06-15T10:00:00Z",
		Feedback: map[string]any{
			"zebra": "last",
			"alpha": "first",
		},
	})

	sig1, err := ContentHashSign(env1, "secret")
	if err != nil {
		t.Fatalf("ContentHashSign env1: %v", err)
	}

	sig2, err := ContentHashSign(env2, "secret")
	if err != nil {
		t.Fatalf("ContentHashSign env2: %v", err)
	}

	if sig1 != sig2 {
		t.Errorf("signatures should be identical regardless of field order: %q vs %q", sig1, sig2)
	}
}

func TestContentHashSign_DifferentSecrets(t *testing.T) {
	env := ptrext.Of(Envelope{
		Version:   "2",
		Timestamp: "2026-06-15T10:00:00Z",
		EventType: "feedback.created",
		TenantID:  "tenant-1",
	})

	sig1, _ := ContentHashSign(env, "secret1")
	sig2, _ := ContentHashSign(env, "secret2")

	if sig1 == sig2 {
		t.Error("different secrets should produce different signatures")
	}
}

func TestBytesSign(t *testing.T) {
	body := []byte(`{"key":"value"}`)
	sig := BytesSign(body, "secret")

	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("signature should start with sha256=, got %q", sig)
	}

	sig2 := BytesSign(body, "secret")
	if sig != sig2 {
		t.Error("same input should produce same signature")
	}

	sig3 := BytesSign(body, "different")
	if sig == sig3 {
		t.Error("different secrets should produce different signatures")
	}
}

func TestCanonicalJSON_NestedSorting(t *testing.T) {
	input := map[string]any{
		"zebra": "z",
		"alpha": map[string]any{
			"nested_z": "nz",
			"nested_a": "na",
		},
		"middle": []any{
			map[string]any{"b": 1, "a": 2},
		},
	}

	result, err := canonicalJSON(input)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}

	expected := `{"alpha":{"nested_a":"na","nested_z":"nz"},"middle":[{"a":2,"b":1}],"zebra":"z"}`
	if string(result) != expected {
		t.Errorf("canonicalJSON:\ngot:  %s\nwant: %s", result, expected)
	}
}
