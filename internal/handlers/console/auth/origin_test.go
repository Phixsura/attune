// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOriginKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		raw     string
		wantKey string
		wantOK  bool
	}{
		{"https normal", "https://example.com", "https://example.com", true},
		{"https with path", "https://example.com/path/to", "https://example.com", true},
		{"https with explicit 443", "https://example.com:443", "https://example.com", true},
		{"https with custom port", "https://example.com:8443", "https://example.com:8443", true},
		{"http normal", "http://example.com", "http://example.com", true},
		{"http with explicit 80", "http://example.com:80", "http://example.com", true},
		{"http with custom port", "http://example.com:3000", "http://example.com:3000", true},
		{"uppercase scheme", "HTTPS://EXAMPLE.COM", "https://example.com", true},
		{"empty string", "", "", false},
		{"no scheme", "example.com", "", false},
		{"no host", "https://", "", false},
		{"relative path", "/path/to", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := originKey(tt.raw)
			if ok != tt.wantOK {
				t.Errorf("originKey(%q) ok = %v, want %v", tt.raw, ok, tt.wantOK)
			}
			if got != tt.wantKey {
				t.Errorf("originKey(%q) = %q, want %q", tt.raw, got, tt.wantKey)
			}
		})
	}
}

func TestSameOrigin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  string
		want string
		same bool
	}{
		{"exact match", "https://example.com", "https://example.com", true},
		{"with path", "https://example.com/path", "https://example.com", true},
		{"explicit vs implicit port", "https://example.com:443", "https://example.com", true},
		{"different scheme", "http://example.com", "https://example.com", false},
		{"different host", "https://evil.com", "https://example.com", false},
		{"different port", "https://example.com:8443", "https://example.com", false},
		{"empty got", "", "https://example.com", false},
		{"empty want", "https://example.com", "", false},
		{"both empty", "", "", false},
		{"case insensitive", "HTTPS://EXAMPLE.COM", "https://example.com", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sameOrigin(tt.got, tt.want); got != tt.same {
				t.Errorf("sameOrigin(%q, %q) = %v, want %v", tt.got, tt.want, got, tt.same)
			}
		})
	}
}

func TestOriginAllowed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		secFetch  string
		origin    string
		referer   string
		baseURL   string
		wantAllow bool
	}{
		{
			name:      "sec-fetch same-origin",
			secFetch:  "same-origin",
			baseURL:   "https://example.com",
			wantAllow: true,
		},
		{
			name:      "sec-fetch same-site",
			secFetch:  "same-site",
			baseURL:   "https://example.com",
			wantAllow: true,
		},
		{
			name:      "sec-fetch cross-site rejected",
			secFetch:  "cross-site",
			baseURL:   "https://example.com",
			wantAllow: false,
		},
		{
			name:      "same origin header",
			origin:    "https://example.com",
			baseURL:   "https://example.com",
			wantAllow: true,
		},
		{
			name:      "different origin header rejected",
			origin:    "https://evil.com",
			baseURL:   "https://example.com",
			wantAllow: false,
		},
		{
			name:      "referer fallback same origin",
			referer:   "https://example.com/login",
			baseURL:   "https://example.com",
			wantAllow: true,
		},
		{
			name:      "referer fallback different origin",
			referer:   "https://evil.com/attack",
			baseURL:   "https://example.com",
			wantAllow: false,
		},
		{
			name:      "no origin headers allows (curl/server-to-server)",
			baseURL:   "https://example.com",
			wantAllow: true,
		},
		{
			name:      "empty baseURL allows",
			origin:    "https://example.com",
			baseURL:   "",
			wantAllow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/login", nil)
			if tt.secFetch != "" {
				r.Header.Set("Sec-Fetch-Site", tt.secFetch)
			}
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				r.Header.Set("Referer", tt.referer)
			}
			if got := originAllowed(r, tt.baseURL); got != tt.wantAllow {
				t.Errorf("originAllowed() = %v, want %v", got, tt.wantAllow)
			}
		})
	}
}
