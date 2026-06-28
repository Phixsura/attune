// SPDX-License-Identifier: Apache-2.0

package mcpclient

import (
	"net/url"
	"strings"
	"testing"

	"github.com/Phixsura/attune/internal/domain"
)

func TestValidateScopes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		scopes  []string
		wantErr bool
		errMsg  string
	}{
		{"empty scopes", []string{}, false, ""},
		{"read only", []string{domain.MCPScopeRead}, false, ""},
		{"write only", []string{domain.MCPScopeWrite}, false, ""},
		{"ingest only", []string{domain.MCPScopeIngest}, false, ""},
		{"all valid", []string{domain.MCPScopeRead, domain.MCPScopeWrite, domain.MCPScopeIngest}, false, ""},
		{"invalid scope", []string{"admin"}, true, "invalid scope"},
		{"mixed valid invalid", []string{domain.MCPScopeRead, "invalid"}, true, "invalid scope"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScopes(tt.scopes)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateScopes(%v) = nil, want error containing %q", tt.scopes, tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateScopes(%v) error = %q, want containing %q", tt.scopes, err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("validateScopes(%v) = %v, want nil", tt.scopes, err)
			}
		})
	}
}

func TestValidateClientName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		wantErr bool
		errMsg  string
	}{
		{"simple", "agent", false, ""},
		{"trimmed allowed", "  ops-agent  ", false, ""},
		{"empty rejected", "", true, "name is required"},
		{"whitespace rejected", "   ", true, "name is required"},
		{"too long rejected", strings.Repeat("x", 129), true, "name exceeds 128 characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateClientName(strings.TrimSpace(tt.value))
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateClientName(%q) = nil, want error containing %q", tt.value, tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateClientName(%q) error = %q, want containing %q", tt.value, err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("validateClientName(%q) = %v, want nil", tt.value, err)
			}
		})
	}
}

func TestIsLoopbackURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		uri  string
		want bool
	}{
		{"localhost", "http://localhost:8080/callback", true},
		{"localhost no port", "http://localhost/callback", true},
		{"localhost uppercase", "http://LOCALHOST/callback", true},
		{"127.0.0.1", "http://127.0.0.1:3000/callback", true},
		{"ipv6 loopback", "http://::1:8080/callback", true},
		{"ipv6 bracketed", "http://[::1]:8080/callback", true},
		{"external host", "http://example.com/callback", false},
		{"internal host", "http://internal.example.com/callback", false},
		{"localhost subdomain", "http://localhost.example.com/callback", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.uri)
			if err != nil {
				t.Fatalf("url.Parse(%q) error = %v", tt.uri, err)
			}
			got := isLoopbackURI(u)
			if got != tt.want {
				t.Errorf("isLoopbackURI(%q) = %v, want %v", tt.uri, got, tt.want)
			}
		})
	}
}

func TestValidateRedirectURIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		uris    []string
		wantErr bool
		errMsg  string
	}{
		{"empty", []string{}, false, ""},
		{"https valid", []string{"https://example.com/callback"}, false, ""},
		{"localhost http", []string{"http://localhost:8080/callback"}, false, ""},
		{"127.0.0.1 http", []string{"http://127.0.0.1/callback"}, false, ""},
		{"http non-localhost", []string{"http://example.com/callback"}, true, "must use HTTPS"},
		{"fragment rejected", []string{"https://example.com/callback#frag"}, true, "fragment not allowed"},
		{"javascript scheme", []string{"javascript:alert(1)"}, true, "dangerous URI scheme"},
		{"data scheme", []string{"data:text/html,<script>"}, true, "dangerous URI scheme"},
		{"file scheme", []string{"file:///etc/passwd"}, true, "dangerous URI scheme"},
		{"vbscript scheme", []string{"vbscript:msgbox(1)"}, true, "dangerous URI scheme"},
		{"invalid url", []string{"://not-a-url"}, true, "invalid URL"},
		{"hostless https opaque", []string{"https:callback"}, true, "absolute URI with a host"},
		{"hostless https path", []string{"https:///callback"}, true, "absolute URI with a host"},
		{"scheme-relative loopback", []string{"//localhost/callback"}, true, "absolute URI with a host"},
		{"multiple valid", []string{"https://a.com/cb", "http://localhost/cb"}, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRedirectURIs(tt.uris)
			if tt.wantErr {
				if err == nil {
					t.Errorf("validateRedirectURIs(%v) = nil, want error containing %q", tt.uris, tt.errMsg)
				} else if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateRedirectURIs(%v) error = %q, want containing %q", tt.uris, err.Error(), tt.errMsg)
				}
			} else if err != nil {
				t.Errorf("validateRedirectURIs(%v) = %v, want nil", tt.uris, err)
			}
		})
	}
}
