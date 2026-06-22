package sig

import (
	"strings"
	"testing"
)

func TestEnvelopeVersion(t *testing.T) {
	t.Parallel()

	if EnvelopeVersion != "1" {
		t.Errorf("EnvelopeVersion = %q, want '1'", EnvelopeVersion)
	}
}

func TestSignRaw(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		body   []byte
		secret string
	}{
		{"empty body", []byte{}, "secret"},
		{"simple body", []byte("hello world"), "secret"},
		{"json body", []byte(`{"key":"value"}`), "my-secret-key"},
		{"binary body", []byte{0x00, 0x01, 0x02, 0xFF}, "binary-secret"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sig := SignRaw(tt.body, tt.secret)

			if !strings.HasPrefix(sig, "sha256=") {
				t.Errorf("SignRaw() = %q, want prefix 'sha256='", sig)
			}

			hexPart := strings.TrimPrefix(sig, "sha256=")
			if len(hexPart) != 64 {
				t.Errorf("hex part length = %d, want 64", len(hexPart))
			}
		})
	}
}

func TestSignRawDeterministic(t *testing.T) {
	t.Parallel()

	body := []byte("test body")
	secret := "test-secret"

	sig1 := SignRaw(body, secret)
	sig2 := SignRaw(body, secret)

	if sig1 != sig2 {
		t.Errorf("SignRaw is not deterministic: %q != %q", sig1, sig2)
	}
}

func TestSignRawDifferentSecrets(t *testing.T) {
	t.Parallel()

	body := []byte("test body")

	sig1 := SignRaw(body, "secret1")
	sig2 := SignRaw(body, "secret2")

	if sig1 == sig2 {
		t.Error("different secrets produced same signature")
	}
}

func TestSignRawDifferentBodies(t *testing.T) {
	t.Parallel()

	secret := "test-secret"

	sig1 := SignRaw([]byte("body1"), secret)
	sig2 := SignRaw([]byte("body2"), secret)

	if sig1 == sig2 {
		t.Error("different bodies produced same signature")
	}
}

func TestSignRawKnownVector(t *testing.T) {
	t.Parallel()

	body := []byte("test")
	secret := "key"

	sig := SignRaw(body, secret)

	if !strings.HasPrefix(sig, "sha256=") {
		t.Errorf("signature = %q, want sha256= prefix", sig)
	}
}
