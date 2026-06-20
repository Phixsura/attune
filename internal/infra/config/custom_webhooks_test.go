// ptrext:file-allow test fixtures construct Config values inline.
package config

import (
	"strings"
	"testing"
)

func TestValidateCustomWebhooks_SlackSkipsSecretRequirement(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		CustomWebhooks: []CustomWebhookDest{{
			TenantSlug:      "demo",
			DestinationType: "slack",
			Audience:        "pool",
			URL:             "https://hooks.slack.com/services/T00/B00/xxx",
		}},
	}
	if err := cfg.validateCustomWebhooks(); err != nil {
		t.Fatalf("slack without secret should pass: %v", err)
	}
}

func TestValidateCustomWebhooks_RawWebhookRequiresSecret(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		CustomWebhooks: []CustomWebhookDest{{
			TenantSlug:      "demo",
			DestinationType: "raw-webhook",
			Audience:        "pool",
			URL:             "https://example.com/hook",
			Secret:          "short",
		}},
	}
	err := cfg.validateCustomWebhooks()
	if err == nil {
		t.Fatal("raw-webhook with short secret should fail")
	}
	if !strings.Contains(err.Error(), "secret must be at least 16") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCustomWebhooks_DefaultDestTypeRequiresSecret(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		CustomWebhooks: []CustomWebhookDest{{
			TenantSlug: "demo",
			Audience:   "pool",
			URL:        "https://example.com/hook",
		}},
	}
	err := cfg.validateCustomWebhooks()
	if err == nil {
		t.Fatal("empty destination_type (defaults to raw-webhook) with no secret should fail")
	}
	if !strings.Contains(err.Error(), "secret must be at least 16") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCustomWebhooks_DedupIncludesDestType(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		CustomWebhooks: []CustomWebhookDest{
			{
				TenantSlug:      "demo",
				DestinationType: "raw-webhook",
				Audience:        "pool",
				URL:             "https://example.com/hook",
				Secret:          "0123456789abcdef",
			},
			{
				TenantSlug:      "demo",
				DestinationType: "slack",
				Audience:        "pool",
				URL:             "https://hooks.slack.com/services/T00/B00/xxx",
			},
		},
	}
	if err := cfg.validateCustomWebhooks(); err != nil {
		t.Fatalf("same tenant+audience but different dest_type should pass: %v", err)
	}
}

func TestValidateCustomWebhooks_InvalidDestTypeRejected(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		CustomWebhooks: []CustomWebhookDest{{
			TenantSlug:      "demo",
			DestinationType: "slakc",
			Audience:        "pool",
			URL:             "https://example.com/hook",
		}},
	}
	err := cfg.validateCustomWebhooks()
	if err == nil {
		t.Fatal("invalid destination_type should fail")
	}
	if !strings.Contains(err.Error(), "not valid") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCustomWebhooks_LarkSkipsSecretRequirement(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		CustomWebhooks: []CustomWebhookDest{{
			TenantSlug:      "demo",
			DestinationType: "lark",
			Audience:        "pool",
			URL:             "https://open.feishu.cn/open-apis/bot/v2/hook/xxx",
		}},
	}
	if err := cfg.validateCustomWebhooks(); err != nil {
		t.Fatalf("lark without secret should pass: %v", err)
	}
}

func TestValidateCustomWebhooks_GitHubIssueRequiresSecret(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		CustomWebhooks: []CustomWebhookDest{{
			TenantSlug:      "demo",
			DestinationType: "github-issue",
			Audience:        "pool",
			URL:             "https://api.github.com/repos/org/repo/issues",
			Secret:          "short",
		}},
	}
	err := cfg.validateCustomWebhooks()
	if err == nil {
		t.Fatal("github-issue with short secret should fail")
	}
	if !strings.Contains(err.Error(), "secret must be at least 16") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateCustomWebhooks_DuplicateSameDestType(t *testing.T) {
	t.Parallel()
	cfg := &Config{
		CustomWebhooks: []CustomWebhookDest{
			{
				TenantSlug:      "demo",
				DestinationType: "slack",
				Audience:        "pool",
				URL:             "https://hooks.slack.com/services/T00/B00/xxx",
			},
			{
				TenantSlug:      "demo",
				DestinationType: "slack",
				Audience:        "pool",
				URL:             "https://hooks.slack.com/services/T00/B00/yyy",
			},
		},
	}
	err := cfg.validateCustomWebhooks()
	if err == nil {
		t.Fatal("same tenant+dest_type+audience should be duplicate")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "destination_type") {
		t.Fatalf("error should mention destination_type: %v", err)
	}
}
