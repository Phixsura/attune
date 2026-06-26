// SPDX-License-Identifier: Apache-2.0

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
)

func TestIsPrivateHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		host   string
		expect bool
	}{
		// IPv4 private ranges
		{"IPv4 10.x", "10.0.0.1", true},
		{"IPv4 172.16.x", "172.16.0.1", true},
		{"IPv4 172.31.x", "172.31.255.255", true},
		{"IPv4 192.168.x", "192.168.1.1", true},
		{"IPv4 loopback", "127.0.0.1", true},
		{"IPv4 public", "8.8.8.8", false},
		{"IPv4 with port", "192.168.1.1:8080", true},

		// IPv6 private ranges
		{"IPv6 loopback", "::1", true},
		{"IPv6 link-local", "fe80::1", true},
		{"IPv6 unique-local fc00", "fc00::1", true},
		{"IPv6 unique-local fd00", "fd00::1", true},
		{"IPv6 public", "2001:4860:4860::8888", false},
		{"IPv6 in brackets", "[fe80::1]", true},
		{"IPv6 in brackets with port", "[::1]:8080", true},

		// Hostnames (string prefix fallback)
		{"hostname public", "example.com", false},
		{"hostname local", "localhost", false}, // isLoopback catches this, not isPrivateHost

		// DNS rebinding services (SSRF bypass attempts)
		{"nip.io rebinding", "127.0.0.1.nip.io", true},
		{"xip.io rebinding", "192.168.1.1.xip.io", true},
		{"sslip.io rebinding", "10.0.0.1.sslip.io", true},
		{"localtest.me rebinding", "app.localtest.me", true},
		{"vcap.me rebinding", "anything.vcap.me", true},

		// Internal domain suffixes
		{"internal suffix", "idp.internal", true},
		{"local suffix", "service.local", true},
		{"corp suffix", "auth.corp", true},
		{"k8s cluster.local", "oidc.default.svc.cluster.local", true},
		{"private suffix", "app.private", true},
		{"lan suffix", "router.lan", true},

		// Non-standard IP formats (SSRF bypass attempts)
		{"decimal loopback", "2130706433", true}, // 127.0.0.1
		{"hex loopback", "0x7f000001", true},     // 127.0.0.1
		{"decimal 10.x", "167772161", true},      // 10.0.0.1
		{"hex 192.168.1.1", "0xC0A80101", true},  // 192.168.1.1
		{"decimal public", "134744072", false},   // 8.8.8.8
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, isPrivateHost(tt.host), "host: %s", tt.host)
		})
	}
}

func TestIsCloudMetadataHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		host   string
		expect bool
	}{
		{"AWS metadata", "169.254.169.254", true},
		{"AWS metadata with port", "169.254.169.254:80", true},
		{"GCP metadata", "metadata.google.internal", true},
		{"ECS metadata", "169.254.170.2", true},
		{"AWS time sync", "169.254.169.123", true},
		{"Alibaba Cloud metadata", "100.100.100.200", true},
		{"normal host", "example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, isCloudMetadataHost(tt.host), "host: %s", tt.host)
		})
	}
}

func TestIsLoopback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		host   string
		expect bool
	}{
		{"localhost", "localhost", true},
		{"localhost with port", "localhost:8080", true},
		{"IPv4 loopback", "127.0.0.1", true},
		{"IPv4 loopback with port", "127.0.0.1:3000", true},
		{"IPv6 loopback", "::1", true},
		{"IPv6 loopback in brackets", "[::1]", true},
		{"IPv6 loopback with port", "[::1]:8080", true},
		{"public domain", "example.com", false},
		{"public IPv4", "8.8.8.8", false},
		{"private IPv4", "192.168.1.1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, isLoopback(tt.host), "host: %s", tt.host)
		})
	}
}

func TestNormalizeHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"plain host", "example.com", "example.com"},
		{"host with port", "example.com:443", "example.com"},
		{"IPv4", "192.168.1.1", "192.168.1.1"},
		{"IPv4 with port", "192.168.1.1:8080", "192.168.1.1"},
		{"IPv6 in brackets", "[::1]", "::1"},
		{"IPv6 with port", "[::1]:8080", "::1"},
		{"IPv6 with zone", "fe80::1%eth0", "fe80::1"},
		{"localhost", "localhost", "localhost"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expect, normalizeHost(tt.input), "input: %s", tt.input)
		})
	}
}

func TestParseNonStandardIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		expectNil bool
		expectIP  string
	}{
		{"decimal 127.0.0.1", "2130706433", false, "127.0.0.1"},
		{"hex 127.0.0.1", "0x7f000001", false, "127.0.0.1"},
		{"hex uppercase", "0X7F000001", false, "127.0.0.1"},
		{"decimal 10.0.0.1", "167772161", false, "10.0.0.1"},
		{"invalid decimal", "not-a-number", true, ""},
		{"invalid hex", "0xGGGGGGGG", true, ""},
		{"normal IP", "192.168.1.1", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := parseNonStandardIP(tt.input)
			if tt.expectNil {
				assert.Nil(t, ip, "input: %s", tt.input)
			} else {
				assert.NotNil(t, ip, "input: %s", tt.input)
				assert.Equal(t, tt.expectIP, ip.String(), "input: %s", tt.input)
			}
		})
	}
}

func TestOIDCConfig_Validate(t *testing.T) {
	t.Parallel()

	valid := OIDCConfig{
		Enabled:     true,
		IssuerURL:   "https://accounts.google.com",
		ClientID:    "my-client",
		RedirectURI: "https://app.example.com/callback",
		Scopes:      []string{"openid", "email"},
	}

	t.Run("disabled skips validation", func(t *testing.T) {
		t.Parallel()
		cfg := OIDCConfig{Enabled: false}
		assert.NoError(t, cfg.Validate())
	})

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		assert.NoError(t, cfg.Validate())
	})

	t.Run("missing issuer", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.IssuerURL = ""
		assert.ErrorIs(t, cfg.Validate(), ErrOIDCIssuerRequired)
	})

	t.Run("missing client id", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.ClientID = ""
		assert.ErrorIs(t, cfg.Validate(), ErrOIDCClientIDRequired)
	})

	t.Run("missing redirect uri", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.RedirectURI = ""
		assert.ErrorIs(t, cfg.Validate(), ErrOIDCRedirectRequired)
	})

	t.Run("http issuer rejected", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.IssuerURL = "http://idp.example.com"
		assert.ErrorIs(t, cfg.Validate(), ErrOIDCIssuerNotHTTPS)
	})

	t.Run("http issuer allowed with insecure skip verify", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.IssuerURL = "http://idp.example.com"
		cfg.InsecureSkipVerify = true
		assert.NoError(t, cfg.Validate())
	})

	t.Run("http issuer allowed for localhost", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.IssuerURL = "http://localhost:8080"
		assert.NoError(t, cfg.Validate())
	})

	t.Run("cloud metadata issuer rejected", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.IssuerURL = "https://169.254.169.254"
		assert.ErrorIs(t, cfg.Validate(), ErrOIDCIssuerMetadata)
	})

	t.Run("private ip issuer rejected", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.IssuerURL = "https://192.168.1.1"
		assert.ErrorIs(t, cfg.Validate(), ErrOIDCIssuerPrivate)
	})

	t.Run("private ip issuer allowed with insecure skip verify", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.IssuerURL = "https://192.168.1.1"
		cfg.InsecureSkipVerify = true
		assert.NoError(t, cfg.Validate())
	})

	t.Run("http redirect rejected", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.RedirectURI = "http://app.example.com/callback"
		assert.Error(t, cfg.Validate())
	})

	t.Run("http redirect allowed for localhost", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.RedirectURI = "http://localhost:3000/callback"
		assert.NoError(t, cfg.Validate())
	})

	t.Run("scopes without openid rejected", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.Scopes = []string{"email", "profile"}
		assert.Error(t, cfg.Validate())
	})

	t.Run("security warnings logged", func(t *testing.T) {
		t.Parallel()
		cfg := valid
		cfg.InsecureSkipVerify = true
		cfg.SkipIssuerCheck = true
		assert.NoError(t, cfg.Validate())
	})
}

func TestOIDCConfig_ApplyDefaults(t *testing.T) {
	t.Parallel()

	t.Run("empty config gets defaults", func(t *testing.T) {
		cfg := ptrext.Of(OIDCConfig{})
		cfg.ApplyDefaults()
		assert.Equal(t, []string{"openid", "email", "profile"}, cfg.Scopes)
		assert.Equal(t, "email", cfg.UserClaim)
		assert.Equal(t, "groups", cfg.GroupsClaim)
		assert.Equal(t, "SSO", cfg.ProviderName)
	})

	t.Run("existing values preserved", func(t *testing.T) {
		cfg := ptrext.Of(OIDCConfig{
			Scopes:       []string{"openid", "custom"},
			UserClaim:    "sub",
			GroupsClaim:  "roles",
			ProviderName: "Okta",
		})
		cfg.ApplyDefaults()
		assert.Equal(t, []string{"openid", "custom"}, cfg.Scopes)
		assert.Equal(t, "sub", cfg.UserClaim)
		assert.Equal(t, "roles", cfg.GroupsClaim)
		assert.Equal(t, "Okta", cfg.ProviderName)
	})
}
