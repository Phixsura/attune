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

// OIDC configuration errors.
var (
	ErrOIDCIssuerRequired   = errors.New("oidc.issuer_url required when oidc.enabled")
	ErrOIDCClientIDRequired = errors.New("oidc.client_id required")
	ErrOIDCRedirectRequired = errors.New("oidc.redirect_uri required")
	ErrOIDCIssuerNotHTTPS   = errors.New("oidc.issuer_url must use HTTPS (set insecure_skip_verify for dev)")
	ErrOIDCIssuerMetadata   = errors.New("oidc.issuer_url cannot be a cloud metadata endpoint")
	ErrOIDCIssuerPrivate    = errors.New("oidc.issuer_url cannot be a private IP (set insecure_skip_verify for internal IdP)")
	ErrOIDCRedirectNotHTTPS = errors.New("oidc.redirect_uri must use HTTPS in production")
	ErrOIDCScopeOpenID      = errors.New("oidc.scopes must include 'openid'")
	ErrOIDCScopeEmail       = errors.New("oidc.scopes should include 'email' for user identification")
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
	if err := c.validateRequired(); err != nil {
		return err
	}
	if err := c.validateIssuerURL(); err != nil {
		return err
	}
	if err := c.validateRedirectURI(); err != nil {
		return err
	}
	if err := c.validateScopes(); err != nil {
		return err
	}
	c.logSecurityWarnings()
	return nil
}

func (c *OIDCConfig) validateRequired() error {
	if c.IssuerURL == "" {
		return ErrOIDCIssuerRequired
	}
	if c.ClientID == "" {
		return ErrOIDCClientIDRequired
	}
	if c.RedirectURI == "" {
		return ErrOIDCRedirectRequired
	}
	return nil
}

func (c *OIDCConfig) validateIssuerURL() error {
	issuer, err := url.Parse(c.IssuerURL)
	if err != nil {
		return fmt.Errorf("oidc.issuer_url invalid: %w", err)
	}
	if issuer.Scheme != "https" && !c.InsecureSkipVerify && !isLoopback(issuer.Host) {
		return ErrOIDCIssuerNotHTTPS
	}
	if isCloudMetadataHost(issuer.Host) {
		return ErrOIDCIssuerMetadata
	}
	if isPrivateHost(issuer.Host) && !c.InsecureSkipVerify {
		return ErrOIDCIssuerPrivate
	}
	return nil
}

func (c *OIDCConfig) validateRedirectURI() error {
	redirect, err := url.Parse(c.RedirectURI)
	if err != nil {
		return fmt.Errorf("oidc.redirect_uri invalid: %w", err)
	}
	if redirect.Scheme != "https" && !isLoopback(redirect.Host) {
		return errors.New("oidc.redirect_uri must use HTTPS (except localhost)")
	}
	return nil
}

func (c *OIDCConfig) validateScopes() error {
	for _, s := range c.Scopes {
		if s == "openid" {
			return nil
		}
	}
	return errors.New("oidc.scopes must include 'openid'")
}

func (c *OIDCConfig) logSecurityWarnings() {
	ctx := context.Background()
	if c.InsecureSkipVerify {
		logext.Warnf(ctx, "[config] oidc.insecure_skip_verify=true — TLS verification disabled, not for production")
	}
	if c.SkipIssuerCheck {
		logext.Warnf(ctx, "[config] oidc.skip_issuer_check=true — issuer validation disabled")
	}
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
	lowerH := strings.ToLower(h)

	if isDNSRebindingService(lowerH) || isInternalDomain(lowerH) {
		return true
	}
	return isPrivateIP(h)
}

func isDNSRebindingService(host string) bool {
	suffixes := []string{".nip.io", ".xip.io", ".sslip.io", ".localtest.me", ".vcap.me"}
	for _, s := range suffixes {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return false
}

func isInternalDomain(host string) bool {
	suffixes := []string{".internal", ".local", ".corp", ".cluster.local", ".private", ".lan"}
	for _, s := range suffixes {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return false
}

func isPrivateIP(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return isPrivateOrReservedIP(ip)
	}
	if ip := parseNonStandardIP(host); ip != nil {
		return isPrivateOrReservedIP(ip)
	}
	return hasPrivateIPPrefix(host)
}

func isPrivateOrReservedIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func hasPrivateIPPrefix(host string) bool {
	if strings.HasPrefix(host, "10.") || strings.HasPrefix(host, "192.168.") {
		return true
	}
	for i := 16; i <= 31; i++ {
		if strings.HasPrefix(host, fmt.Sprintf("172.%d.", i)) {
			return true
		}
	}
	return false
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
