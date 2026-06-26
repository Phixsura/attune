package logext

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSafeURLForLog(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"valid_https", "https://example.com/path?q=1", "https://example.com"},
		{"valid_http", "http://localhost:8080/api", "http://localhost:8080"},
		{"no_scheme", "example.com", "invalid"},
		{"no_host", "file:///tmp/foo", "invalid"},
		{"with_port", "https://api.example.com:443/v1", "https://api.example.com:443"},
		{"whitespace_padded", "  https://example.com  ", "https://example.com"},
		{"invalid_url", "://bad", "invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, SafeURLForLog(tt.raw))
		})
	}
}
