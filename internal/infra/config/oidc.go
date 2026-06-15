// SPDX-License-Identifier: Apache-2.0

package config

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/Phixsura/attune/internal/pkg/logext"
)

// RoleMappingEntry defines a single role → groups mapping (evaluated in order).
type RoleMappingEntry struct {
	Role   string   `yaml:"role"`
	Groups []string `yaml:"groups"`
}

// OIDCConfig holds OIDC SSO configuration.
type OIDCConfig struct {
	Enabled            bool               `yaml:"enabled"`
	IssuerURL          string             `yaml:"issuer_url"`
	ClientID           string             `yaml:"client_id"`
	ClientSecret       string             `yaml:"client_secret"`
	RedirectURI        string             `yaml:"redirect_uri"`
	Scopes             []string           `yaml:"scopes"`
	UserClaim          string             `yaml:"user_claim"`
	GroupsClaim        string             `yaml:"groups_claim"`
	ProviderName       string             `yaml:"provider_name"`
	RoleMapping        []RoleMappingEntry `yaml:"role_mapping"`
	AllowedGroups      []string           `yaml:"allowed_groups"`
	OIDCOnly           bool               `yaml:"oidc_only"`
	SkipIssuerCheck    bool               `yaml:"skip_issuer_check"`
	InsecureSkipVerify bool               `yaml:"insecure_skip_verify"`
}

// ApplyDefaults sets default values for optional fields.
func (c *OIDCConfig) ApplyDefaults() {
	if len(c.Scopes) == 0 {
		c.Scopes = []string{"openid", "email", "profile"}
	}
	if c.UserClaim == "" {
		c.UserClaim = "email"
	}
	if c.GroupsClaim == "" {
		c.GroupsClaim = "groups"
	}
	if c.ProviderName == "" {
		c.ProviderName = "SSO"
	}
}

// Validate checks required fields and security constraints.
func (c *OIDCConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	// Required fields
	if c.IssuerURL == "" {
		return errors.New("oidc.issuer_url required when oidc.enabled")
	}
	if c.ClientID == "" {
		return errors.New("oidc.client_id required")
	}
	if c.RedirectURI == "" {
		return errors.New("oidc.redirect_uri required")
	}

	// URL format validation
	issuer, err := url.Parse(c.IssuerURL)
	if err != nil {
		return fmt.Errorf("oidc.issuer_url invalid: %w", err)
	}

	// HTTPS required (except localhost for dev)
	if issuer.Scheme != "https" && !c.InsecureSkipVerify {
		if !isLoopback(issuer.Host) {
			return errors.New("oidc.issuer_url must use HTTPS (set insecure_skip_verify for dev)")
		}
	}

	// Block cloud metadata endpoints (SSRF protection)
	if isCloudMetadataHost(issuer.Host) {
		return errors.New("oidc.issuer_url cannot be a cloud metadata endpoint")
	}

	// Block private IP ranges (SSRF protection)
	if isPrivateHost(issuer.Host) && !c.InsecureSkipVerify {
		return errors.New("oidc.issuer_url cannot be a private IP (set insecure_skip_verify for internal IdP)")
	}

	// RedirectURI validation
	redirect, err := url.Parse(c.RedirectURI)
	if err != nil {
		return fmt.Errorf("oidc.redirect_uri invalid: %w", err)
	}
	if redirect.Scheme != "https" && !isLoopback(redirect.Host) {
		return errors.New("oidc.redirect_uri must use HTTPS (except localhost)")
	}

	// Scopes must include openid
	hasOpenID := false
	for _, s := range c.Scopes {
		if s == "openid" {
			hasOpenID = true
			break
		}
	}
	if !hasOpenID {
		return errors.New("oidc.scopes must include 'openid'")
	}

	// Security warnings (logged, not errors)
	ctx := context.Background()
	if c.InsecureSkipVerify {
		logext.Warnf(ctx, "[config] oidc.insecure_skip_verify=true — TLS verification disabled, not for production")
	}
	if c.SkipIssuerCheck {
		logext.Warnf(ctx, "[config] oidc.skip_issuer_check=true — issuer validation disabled")
	}

	return nil
}

// isLoopback returns true for localhost addresses (IPv4 and IPv6).
func isLoopback(host string) bool {
	h := normalizeHost(host)

	// String check for "localhost"
	if h == "localhost" {
		return true
	}

	// Parse IP and check loopback
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// isCloudMetadataHost returns true for cloud metadata service addresses.
func isCloudMetadataHost(host string) bool {
	h := normalizeHost(host)

	// Cloud metadata endpoints
	metadataHosts := map[string]bool{
		"169.254.169.254":          true, // AWS/GCP/Azure/DigitalOcean
		"169.254.170.2":            true, // AWS ECS
		"169.254.169.123":          true, // AWS Time sync
		"100.100.100.200":          true, // Alibaba Cloud
		"metadata.google.internal": true, // GCP
	}
	return metadataHosts[h]
}

// isPrivateHost returns true for private/internal IP addresses (IPv4 and IPv6).
// Also blocks DNS rebinding services and internal domain suffixes.
func isPrivateHost(host string) bool {
	h := normalizeHost(host)

	// Check DNS rebinding services (nip.io, xip.io, etc.)
	lowerH := strings.ToLower(h)
	dnsRebindSuffixes := []string{".nip.io", ".xip.io", ".sslip.io", ".localtest.me", ".vcap.me"}
	for _, suffix := range dnsRebindSuffixes {
		if strings.HasSuffix(lowerH, suffix) {
			return true
		}
	}

	// Check internal domain suffixes
	internalSuffixes := []string{".internal", ".local", ".corp", ".cluster.local", ".private", ".lan"}
	for _, suffix := range internalSuffixes {
		if strings.HasSuffix(lowerH, suffix) {
			return true
		}
	}

	// Try parsing as standard IP
	ip := net.ParseIP(h)
	if ip != nil {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return true
		}
		return false
	}

	// Try parsing non-standard IP formats (decimal, hex)
	if nonStdIP := parseNonStandardIP(h); nonStdIP != nil {
		if nonStdIP.IsPrivate() || nonStdIP.IsLoopback() || nonStdIP.IsLinkLocalUnicast() ||
			nonStdIP.IsLinkLocalMulticast() || nonStdIP.IsUnspecified() {
			return true
		}
		return false
	}

	// Fallback string prefix checks for hostnames that might resolve to private IPs
	return strings.HasPrefix(h, "10.") ||
		strings.HasPrefix(h, "172.16.") || strings.HasPrefix(h, "172.17.") ||
		strings.HasPrefix(h, "172.18.") || strings.HasPrefix(h, "172.19.") ||
		strings.HasPrefix(h, "172.20.") || strings.HasPrefix(h, "172.21.") ||
		strings.HasPrefix(h, "172.22.") || strings.HasPrefix(h, "172.23.") ||
		strings.HasPrefix(h, "172.24.") || strings.HasPrefix(h, "172.25.") ||
		strings.HasPrefix(h, "172.26.") || strings.HasPrefix(h, "172.27.") ||
		strings.HasPrefix(h, "172.28.") || strings.HasPrefix(h, "172.29.") ||
		strings.HasPrefix(h, "172.30.") || strings.HasPrefix(h, "172.31.") ||
		strings.HasPrefix(h, "192.168.")
}

// normalizeHost extracts and normalizes the host from host:port format.
func normalizeHost(host string) string {
	// Try to split host:port
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}

	// Handle IPv6 addresses in brackets
	if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
		h = h[1 : len(h)-1]
	}

	// Remove IPv6 zone ID (e.g., fe80::1%eth0 -> fe80::1)
	if idx := strings.Index(h, "%"); idx != -1 {
		h = h[:idx]
	}

	return h
}

// parseNonStandardIP parses decimal (2130706433) and hex (0x7f000001) IP formats.
func parseNonStandardIP(s string) net.IP {
	// Decimal format: 2130706433 -> 127.0.0.1
	if n, err := strconv.ParseUint(s, 10, 32); err == nil {
		return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}

	// Hex format: 0x7f000001 -> 127.0.0.1
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		if n, err := strconv.ParseUint(s[2:], 16, 32); err == nil {
			return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
		}
	}

	return nil
}
