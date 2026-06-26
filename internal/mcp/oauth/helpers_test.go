// SPDX-License-Identifier: Apache-2.0

package oauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestParseScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty string", "", nil},
		{"single scope", "read", []string{"read"}},
		{"multiple scopes", "read write ingest", []string{"read", "write", "ingest"}},
		{"extra whitespace", "  read   write  ", []string{"read", "write"}},
		{"tabs and newlines", "read\twrite\ningest", []string{"read", "write", "ingest"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseScopes(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseScopes(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseScopes(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestScopesAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		requested []string
		allowed   []string
		want      bool
	}{
		{"empty requested", []string{}, []string{"read", "write"}, true},
		{"all allowed", []string{"read", "write"}, []string{"read", "write", "ingest"}, true},
		{"subset allowed", []string{"read"}, []string{"read", "write"}, true},
		{"one not allowed", []string{"read", "admin"}, []string{"read", "write"}, false},
		{"none allowed", []string{"admin"}, []string{"read", "write"}, false},
		{"empty allowed", []string{"read"}, []string{}, false},
		{"both empty", []string{}, []string{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scopesAllowed(tt.requested, tt.allowed)
			if got != tt.want {
				t.Errorf("scopesAllowed(%v, %v) = %v, want %v", tt.requested, tt.allowed, got, tt.want)
			}
		})
	}
}

func TestHashToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			"empty",
			"",
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			"simple",
			"abc",
			"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
		{
			"token like",
			"at_12345678901234567890123456789012",
			"79ca5c391f6d25a1c39e66102f8bfdef5a1c3b68c8e3f5a7e4e8e1f3c4d7a9b2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hashToken(tt.token)
			if tt.name == "token like" {
				if len(got) != 64 {
					t.Errorf("hashToken(%q) length = %d, want 64", tt.token, len(got))
				}
				return
			}
			if got != tt.want {
				t.Errorf("hashToken(%q) = %q, want %q", tt.token, got, tt.want)
			}
		})
	}
}

func TestAudienceContains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		aud    jwt.ClaimStrings
		target string
		want   bool
	}{
		{"found", jwt.ClaimStrings{"a", "b", "c"}, "b", true},
		{"not found", jwt.ClaimStrings{"a", "b", "c"}, "d", false},
		{"empty audience", jwt.ClaimStrings{}, "a", false},
		{"single match", jwt.ClaimStrings{"target"}, "target", true},
		{"nil audience", nil, "a", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := audienceContains(tt.aud, tt.target)
			if got != tt.want {
				t.Errorf("audienceContains(%v, %q) = %v, want %v", tt.aud, tt.target, got, tt.want)
			}
		})
	}
}

func TestGenerateRandomString(t *testing.T) {
	t.Parallel()
	a := generateRandomString()
	b := generateRandomString()
	if a == b {
		t.Fatal("two calls should produce different strings")
	}
	if len(a) < 40 {
		t.Fatalf("expected base64 of 32 bytes (~43 chars), got %d", len(a))
	}
}

func TestErrorRedirect_InvalidClient(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/authorize", nil)
	errorRedirect(w, r, "https://example.com/callback", "state1", ErrInvalidClient)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid client, got %d", w.Code)
	}
}

func TestErrorRedirect_InvalidRedirectURI(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/authorize", nil)
	errorRedirect(w, r, "https://example.com/callback", "", ErrInvalidRedirectURI)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid redirect, got %d", w.Code)
	}
}

func TestErrorRedirect_EmptyRedirectURI(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/authorize", nil)
	errorRedirect(w, r, "", "s", ErrPKCERequired)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty redirect, got %d", w.Code)
	}
}

func TestErrorRedirect_Redirect(t *testing.T) {
	t.Parallel()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/authorize", nil)
	errorRedirect(w, r, "https://example.com/callback", "mystate", ErrInvalidScope)
	if w.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc == "" {
		t.Fatal("missing Location header")
	}
}

func TestMapAuthError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err      error
		wantCode string
	}{
		{ErrInvalidRequest, "invalid_request"},
		{ErrInvalidClient, "invalid_request"},
		{ErrInvalidRedirectURI, "invalid_request"},
		{ErrPKCERequired, "invalid_request"},
		{ErrInvalidScope, "invalid_scope"},
		{ErrInvalidTarget, "invalid_target"},
		{errors.New("unknown"), "access_denied"},
	}
	for _, c := range cases {
		code, desc := mapAuthError(c.err)
		if code != c.wantCode {
			t.Errorf("mapAuthError(%v) code = %q, want %q", c.err, code, c.wantCode)
		}
		if desc == "" {
			t.Errorf("mapAuthError(%v) desc is empty", c.err)
		}
	}
}
