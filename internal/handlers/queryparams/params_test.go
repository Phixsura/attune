// SPDX-License-Identifier: Apache-2.0

package queryparams

import (
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func request(query string) *http.Request {
	u, _ := url.Parse("http://test/?" + query)
	return &http.Request{URL: u} // ptrext:allow test helper
}

func TestBool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		query    string
		key      string
		def      bool
		expected bool
	}{
		{"", "flag", false, false},
		{"", "flag", true, true},
		{"flag=true", "flag", false, true},
		{"flag=1", "flag", false, true},
		{"flag=yes", "flag", false, true},
		{"flag=false", "flag", true, false},
		{"flag=0", "flag", true, false},
		{"flag=no", "flag", true, false},
		{"flag=invalid", "flag", false, false},
		{"flag=invalid", "flag", true, true},
	}
	for _, tt := range tests {
		result := Bool(request(tt.query), tt.key, tt.def)
		require.Equal(t, tt.expected, result, "query=%q key=%q def=%v", tt.query, tt.key, tt.def)
	}
}

func TestInt(t *testing.T) {
	t.Parallel()
	tests := []struct {
		query          string
		key            string
		def            int
		minVal, maxVal int
		expected       int
	}{
		{"", "n", 10, 0, 100, 10},
		{"n=50", "n", 10, 0, 100, 50},
		{"n=-10", "n", 10, 0, 100, 0},
		{"n=200", "n", 10, 0, 100, 100},
		{"n=invalid", "n", 10, 0, 100, 10},
	}
	for _, tt := range tests {
		result := Int(request(tt.query), tt.key, tt.def, tt.minVal, tt.maxVal)
		require.Equal(t, tt.expected, result, "query=%q", tt.query)
	}
}

func TestString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		query    string
		key      string
		def      string
		maxLen   int
		expected string
	}{
		{"", "s", "default", 100, "default"},
		{"s=hello", "s", "default", 100, "hello"},
		{"s=verylongstring", "s", "default", 5, "default"},
	}
	for _, tt := range tests {
		result := String(request(tt.query), tt.key, tt.def, tt.maxLen)
		require.Equal(t, tt.expected, result, "query=%q", tt.query)
	}
}

func TestEnum(t *testing.T) {
	t.Parallel()
	allowed := []string{"pending", "active", "done"}
	tests := []struct {
		query    string
		key      string
		def      string
		expected string
	}{
		{"", "status", "pending", "pending"},
		{"status=active", "status", "pending", "active"},
		{"status=invalid", "status", "pending", "pending"},
	}
	for _, tt := range tests {
		result := Enum(request(tt.query), tt.key, tt.def, allowed)
		require.Equal(t, tt.expected, result, "query=%q", tt.query)
	}
}

func TestDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		query          string
		key            string
		def            time.Duration
		minVal, maxVal time.Duration
		expected       time.Duration
	}{
		{"", "d", time.Minute, 0, time.Hour, time.Minute},
		{"d=30s", "d", time.Minute, 0, time.Hour, 30 * time.Second},
		{"d=2h", "d", time.Minute, 0, time.Hour, time.Hour},
		{"d=invalid", "d", time.Minute, 0, time.Hour, time.Minute},
	}
	for _, tt := range tests {
		result := Duration(request(tt.query), tt.key, tt.def, tt.minVal, tt.maxVal)
		require.Equal(t, tt.expected, result, "query=%q", tt.query)
	}
}
