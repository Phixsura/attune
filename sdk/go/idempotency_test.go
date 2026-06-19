package attune

import (
	"regexp"
	"testing"
)

// Mirrors the server's accepted shape: 8-64 chars of [A-Za-z0-9_-].
var serverKeyShape = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

func TestNewIdempotencyKeyMatchesServerShape(t *testing.T) {
	if k := newIdempotencyKey(); !serverKeyShape.MatchString(k) {
		t.Errorf("key %q does not match server shape %s", k, serverKeyShape)
	}
}

func TestNewIdempotencyKeyIsUUIDv4(t *testing.T) {
	uuidV4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if k := newIdempotencyKey(); !uuidV4.MatchString(k) {
		t.Errorf("key %q is not a v4 UUID", k)
	}
}

func TestNewIdempotencyKeyUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		k := newIdempotencyKey()
		if seen[k] {
			t.Fatalf("duplicate key generated: %q", k)
		}
		seen[k] = true
	}
}

// TestIdempotencyKeyFallback exercises the crypto-unavailable path: it must still
// produce server-shaped, unique keys (uniqueness guarantee, like the Node SDK).
func TestIdempotencyKeyFallback(t *testing.T) {
	failRead := func([]byte) (int, error) { return 0, errEntropy }
	k1 := idempotencyKey(failRead)
	k2 := idempotencyKey(failRead)
	if !serverKeyShape.MatchString(k1) {
		t.Errorf("fallback key %q does not match server shape", k1)
	}
	if k1 == k2 {
		t.Errorf("fallback keys not unique: %q == %q", k1, k2)
	}
}

var errEntropy = errInsufficientEntropy{}

type errInsufficientEntropy struct{}

func (errInsufficientEntropy) Error() string { return "no entropy" }

func TestHasHeaderControlChar(t *testing.T) {
	if hasHeaderControlChar("good-key_123") {
		t.Error("clean key flagged as having control char")
	}
	for _, bad := range []string{"a\rb", "a\nb", "a\r\nb"} {
		if !hasHeaderControlChar(bad) {
			t.Errorf("%q should be flagged as having a control char", bad)
		}
	}
}
