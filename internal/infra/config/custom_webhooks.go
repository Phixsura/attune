// Package config — custom webhook destination type and validation.
// Split from config.go to keep that file under attune's 300-line cap
// . This file owns the CustomWebhookDest type and the
// validation rules documented in docs/wave-1.2 §4.5.
package config

import (
	"fmt"
	"strings"
)

// CustomWebhookDest is one row's worth of outbound destination config.
// It is upserted into tenant_notify_targets at startup; everything in
// the running service reads from the DB after that, so this struct is
// only touched at boot.
type CustomWebhookDest struct {
	TenantSlug      string `yaml:"tenant_slug" json:"tenant_slug"`
	DestinationType string `yaml:"destination_type" json:"destination_type"` // raw-webhook (default) | slack | lark | discord | github-issue
	Audience        string `yaml:"audience" json:"audience"`                 // pool / radar / all (default pool)
	URL             string `yaml:"url" json:"url"`
	Secret          string `yaml:"secret" json:"secret"`
	TimeoutSeconds  int    `yaml:"timeout_seconds" json:"timeout_seconds"` // 0 → default 10
	Disabled        bool   `yaml:"disabled" json:"disabled"`
}

// validateCustomWebhooks enforces the contract documented in
// docs/wave-1.2 §4.5: tenant_slug + (https url OR loopback http) +
// secret ≥ 16 chars, no duplicate (tenant_slug, audience) keys. Slug
// existence is checked against the tenants table in main.go at
// startup, not here.
// validDestTypes enumerates all implemented destination types. An
// unknown value in config means a typo that would silently drop
// messages (enricher's outboxDestTypes won't match).
var validDestTypes = map[string]bool{
	"raw-webhook":  true,
	"slack":        true,
	"lark":         true,
	"discord":      true,
	"email":        true,
	"github-issue": true,
}

// secretOptionalDestTypes lists destination types where the secret field
// is optional — the URL itself is the authenticating credential.
var secretOptionalDestTypes = map[string]bool{
	"slack":   true,
	"lark":    true,
	"discord": true,
	"teams":   true,
}

func (c *Config) validateCustomWebhooks() error {
	seen := make(map[string]bool)
	for i, w := range c.CustomWebhooks {
		if w.TenantSlug == "" {
			return fmt.Errorf("custom_webhooks[%d]: tenant_slug is required", i)
		}
		if w.URL == "" {
			return fmt.Errorf("custom_webhooks[%d]: url is required", i)
		}
		if !isAllowedWebhookURL(w.URL) {
			return fmt.Errorf("custom_webhooks[%d]: url must be https:// (or http:// for loopback) — got %q", i, w.URL)
		}
		destType := w.DestinationType
		if destType == "" {
			destType = "raw-webhook"
		}
		if !validDestTypes[destType] {
			return fmt.Errorf("custom_webhooks[%d]: destination_type %q is not valid (allowed: raw-webhook, slack, lark, discord, github-issue)", i, destType)
		}
		if !secretOptionalDestTypes[destType] && len(w.Secret) < 16 {
			return fmt.Errorf("custom_webhooks[%d]: secret must be at least 16 chars", i)
		}
		audience := w.Audience
		if audience == "" {
			audience = "pool"
		}
		if audience != "pool" && audience != "radar" && audience != "all" {
			return fmt.Errorf("custom_webhooks[%d]: audience must be pool|radar|all (got %q)", i, audience)
		}
		key := w.TenantSlug + "|" + destType + "|" + audience
		if seen[key] {
			return fmt.Errorf("custom_webhooks[%d]: duplicate (tenant_slug=%q, destination_type=%q, audience=%q)",
				i, w.TenantSlug, destType, audience)
		}
		seen[key] = true
	}
	return nil
}

// isAllowedWebhookURL gates customer-facing webhook URLs. Public-net
// destinations must be TLS; loopback (127.0.0.1 / localhost / [::1])
// may use plain http for internal sidecar / smoke-test wiring.
func isAllowedWebhookURL(u string) bool {
	switch {
	case strings.HasPrefix(u, "https://"):
		return true
	case strings.HasPrefix(u, "http://127.0.0.1"),
		strings.HasPrefix(u, "http://localhost"),
		strings.HasPrefix(u, "http://[::1]"):
		return true
	}
	return false
}
