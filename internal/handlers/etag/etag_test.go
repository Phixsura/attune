// SPDX-License-Identifier: Apache-2.0

package etag

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerate(t *testing.T) {
	t.Parallel()
	content := []byte("hello world")
	etag := Generate(content)

	require.True(t, len(etag) > 0)
	require.True(t, etag[0] == '"')
	require.True(t, etag[len(etag)-1] == '"')

	// Same content should produce same ETag
	etag2 := Generate(content)
	require.Equal(t, etag, etag2)

	// Different content should produce different ETag
	etag3 := Generate([]byte("different"))
	require.NotEqual(t, etag, etag3)
}

func TestGenerateWeak(t *testing.T) {
	t.Parallel()
	content := []byte("hello world")
	etag := GenerateWeak(content)

	require.True(t, len(etag) > 0)
	require.Contains(t, etag, "W/")
}

func TestMatch(t *testing.T) {
	t.Parallel()
	etag := Generate([]byte("content"))

	tests := []struct {
		name        string
		ifNoneMatch string
		expected    bool
	}{
		{"empty header", "", false},
		{"exact match", etag, true},
		{"wildcard", "*", true},
		{"no match", `"different"`, false},
		{"multiple with match", `"other", ` + etag + `, "another"`, true},
		{"multiple no match", `"a", "b", "c"`, false},
		{"weak comparison", `W/` + etag, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.ifNoneMatch != "" {
				req.Header.Set("If-None-Match", tt.ifNoneMatch)
			}
			result := Match(req, etag)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchStrong(t *testing.T) {
	t.Parallel()
	etag := Generate([]byte("content"))
	weakEtag := GenerateWeak([]byte("content"))

	tests := []struct {
		name     string
		ifMatch  string
		etag     string
		expected bool
	}{
		{"empty header", "", etag, true},
		{"exact match", etag, etag, true},
		{"wildcard", "*", etag, true},
		{"no match", `"different"`, etag, false},
		{"weak etag rejected", weakEtag, weakEtag, false},
		{"weak in header skipped", "W/" + etag, etag, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/", nil)
			if tt.ifMatch != "" {
				req.Header.Set("If-Match", tt.ifMatch)
			}
			result := MatchStrong(req, tt.etag)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSetHeader(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	etag := Generate([]byte("content"))

	SetHeader(rec, etag)

	require.Equal(t, etag, rec.Header().Get("ETag"))
}

func TestNotModified(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	NotModified(rec)
	require.Equal(t, http.StatusNotModified, rec.Code)
}

func TestPreconditionFailed(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	PreconditionFailed(rec)
	require.Equal(t, http.StatusPreconditionFailed, rec.Code)
}
