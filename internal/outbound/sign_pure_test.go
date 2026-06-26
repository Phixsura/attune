// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test-fixtures

package outbound

import (
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestContentHashSign_NilFeedbackMap(t *testing.T) {
	t.Parallel()

	env := ptrext.Of(Envelope{
		Version:   "2",
		Timestamp: "2026-06-20T00:00:00Z",
		EventType: "test",
		TenantID:  "t1",
		Feedback:  nil,
	})

	sig, err := ContentHashSign(env, "secret")
	if err != nil {
		t.Fatalf("ContentHashSign with nil Feedback: %v", err)
	}
	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("signature = %q, want sha256= prefix", sig)
	}
}

func TestContentHashSign_EmptySecret(t *testing.T) {
	t.Parallel()

	env := ptrext.Of(Envelope{
		Version:   "2",
		EventType: "test",
	})

	sig, err := ContentHashSign(env, "")
	if err != nil {
		t.Fatalf("ContentHashSign with empty secret: %v", err)
	}
	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("signature = %q, want sha256= prefix", sig)
	}
}

func TestContentHashSign_DifferentEnvelopes(t *testing.T) {
	t.Parallel()

	env1 := ptrext.Of(Envelope{Version: "2", EventType: "test", TenantID: "t1"})
	env2 := ptrext.Of(Envelope{Version: "2", EventType: "test", TenantID: "t2"})

	sig1, err := ContentHashSign(env1, "secret")
	if err != nil {
		t.Fatalf("ContentHashSign env1: %v", err)
	}
	sig2, err := ContentHashSign(env2, "secret")
	if err != nil {
		t.Fatalf("ContentHashSign env2: %v", err)
	}

	if sig1 == sig2 {
		t.Error("different envelopes should produce different signatures")
	}
}

func TestBytesSign_EmptyBody(t *testing.T) {
	t.Parallel()

	sig := BytesSign([]byte{}, "secret")
	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("signature = %q, want sha256= prefix", sig)
	}

	// Same empty body, same secret => same signature.
	sig2 := BytesSign([]byte{}, "secret")
	if sig != sig2 {
		t.Errorf("same empty body should produce same signature: %q vs %q", sig, sig2)
	}
}

func TestBytesSign_EmptySecret(t *testing.T) {
	t.Parallel()

	sig := BytesSign([]byte("body"), "")
	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("signature = %q, want sha256= prefix", sig)
	}

	hexPart := strings.TrimPrefix(sig, "sha256=")
	if len(hexPart) != 64 {
		t.Errorf("hex part length = %d, want 64", len(hexPart))
	}
}

func TestSignatureVersionConstants(t *testing.T) {
	t.Parallel()

	// These values are stored in the database. Verify they remain stable.
	if SignatureVersionContentHash != "v2-content-hash" {
		t.Errorf("SignatureVersionContentHash = %q, want %q", SignatureVersionContentHash, "v2-content-hash")
	}
	if SignatureVersionBytes != "v2-bytes" {
		t.Errorf("SignatureVersionBytes = %q, want %q", SignatureVersionBytes, "v2-bytes")
	}
}

func TestCanonicalJSON_Primitives(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		input any
		want  string
	}{
		{"string", "hello", `"hello"`},
		{"number", 42.0, "42"},
		{"boolean", true, "true"},
		{"null", nil, "null"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := canonicalJSON(tc.input)
			if err != nil {
				t.Fatalf("canonicalJSON(%v): %v", tc.input, err)
			}
			if string(got) != tc.want {
				t.Errorf("canonicalJSON(%v) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestCanonicalJSON_EmptyMap(t *testing.T) {
	t.Parallel()

	got, err := canonicalJSON(map[string]any{})
	if err != nil {
		t.Fatalf("canonicalJSON({}): %v", err)
	}
	if string(got) != "{}" {
		t.Errorf("canonicalJSON({}) = %q, want %q", got, "{}")
	}
}

func TestCanonicalJSON_EmptyArray(t *testing.T) {
	t.Parallel()

	got, err := canonicalJSON([]any{})
	if err != nil {
		t.Fatalf("canonicalJSON([]): %v", err)
	}
	if string(got) != "[]" {
		t.Errorf("canonicalJSON([]) = %q, want %q", got, "[]")
	}
}

func TestCanonicalJSON_DeeplyNested(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"c": map[string]any{
			"b": map[string]any{
				"a": 1,
			},
		},
		"a": "first",
	}
	got, err := canonicalJSON(input)
	if err != nil {
		t.Fatalf("canonicalJSON: %v", err)
	}
	want := `{"a":"first","c":{"b":{"a":1}}}`
	if string(got) != want {
		t.Errorf("canonicalJSON =\n  got:  %s\n  want: %s", got, want)
	}
}

func TestCanonicalJSON_MarshalError(t *testing.T) {
	t.Parallel()
	type bad struct {
		Ch chan int `json:"ch"`
	}
	_, err := canonicalJSON(bad{Ch: make(chan int)})
	if err == nil {
		t.Fatal("expected error for unmarshalable type")
	}
}

func TestSortKeys_NilValue(t *testing.T) {
	t.Parallel()

	got := sortKeys(nil)
	if got != nil {
		t.Errorf("sortKeys(nil) = %v, want nil", got)
	}
}

func TestSortKeys_StringValue(t *testing.T) {
	t.Parallel()

	got := sortKeys("hello")
	if got != "hello" {
		t.Errorf("sortKeys(string) = %v, want %q", got, "hello")
	}
}

func TestSortKeys_NumberValue(t *testing.T) {
	t.Parallel()

	got := sortKeys(42.0)
	if got != 42.0 {
		t.Errorf("sortKeys(42.0) = %v, want 42.0", got)
	}
}

func TestSortKeys_ArrayWithMaps(t *testing.T) {
	t.Parallel()

	input := []any{
		map[string]any{"z": 1, "a": 2},
		"plain",
		map[string]any{"m": 3},
	}
	got := sortKeys(input)
	arr, ok := got.([]any)
	if !ok {
		t.Fatalf("sortKeys returned %T, want []any", got)
	}
	if len(arr) != 3 {
		t.Fatalf("len = %d, want 3", len(arr))
	}
	// First element should be a sorted map.
	m, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("arr[0] = %T, want map[string]any", arr[0])
	}
	if m["a"] != 2 || m["z"] != 1 {
		t.Errorf("arr[0] = %v, want map with a=2, z=1", m)
	}
	// Second element should pass through as string.
	if arr[1] != "plain" {
		t.Errorf("arr[1] = %v, want %q", arr[1], "plain")
	}
}
