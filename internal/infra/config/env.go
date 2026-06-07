package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Phixsura/attune/internal/logext"
)

// applyEnvOverrides mutates yc in place, letting env vars trump YAML.
// Kept in its own file because the override tables grow with every new
// config field and would otherwise inflate config.go past the attune
// 300-line discipline.
//
// FEEDBACK_API_CUSTOM_WEBHOOKS is a JSON array of CustomWebhookDest
// objects; it fully replaces (not merges with) any yaml entries.
func applyEnvOverrides(yc *yamlConfig) error {
	for _, o := range []struct {
		env string
		dst *string
	}{
		{"FEEDBACK_API_DATABASE_URL", &yc.DatabaseURL},
		{"FEEDBACK_API_LLM_PROTOCOL", &yc.LLMProtocol},
		{"FEEDBACK_API_LLM_OPENAI_BASE_URL", &yc.LLMOpenAIBaseURL},
		{"FEEDBACK_API_LLM_OPENAI_API_KEY", &yc.LLMOpenAIAPIKey},
		{"FEEDBACK_API_LLM_MODEL", &yc.LLMModel},
		{"FEEDBACK_API_LARK_SIGNING_SECRET", &yc.LarkSigningSecret},
		{"FEEDBACK_API_LARK_VERIFICATION_TOKEN", &yc.LarkVerificationToken},
		{"FEEDBACK_API_LARK_DEFAULT_TENANT_SLUG", &yc.LarkDefaultTenantSlug},
		{"FEEDBACK_API_FEEDBACK_POOL_WEBHOOK_URL", &yc.FeedbackPoolWebhookURL},
		{"FEEDBACK_API_FEEDBACK_POOL_WEBHOOK_SECRET", &yc.FeedbackPoolWebhookSecret},
		{"FEEDBACK_API_DEV_RADAR_WEBHOOK_URL", &yc.DevRadarWebhookURL},
		{"FEEDBACK_API_DEV_RADAR_WEBHOOK_SECRET", &yc.DevRadarWebhookSecret},
		{"FEEDBACK_API_LARK_APP_ID", &yc.LarkAppID},
		{"FEEDBACK_API_LARK_APP_SECRET", &yc.LarkAppSecret},
		{"FEEDBACK_API_CONSOLE_SESSION_KEY", &yc.ConsoleSessionKey},
		{"FEEDBACK_API_CONSOLE_BASE_URL", &yc.ConsoleBaseURL},
	} {
		if v := os.Getenv(o.env); v != "" {
			*o.dst = v
		}
	}
	if raw := os.Getenv("FEEDBACK_API_CUSTOM_WEBHOOKS"); raw != "" {
		const where = "config.applyEnvOverrides"
		var parsed []CustomWebhookDest
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			logext.Errorf(context.Background(),
				"[%s] FEEDBACK_API_CUSTOM_WEBHOOKS parse failed,err:%+v",
				where, err.Error())
			return fmt.Errorf("FEEDBACK_API_CUSTOM_WEBHOOKS: %w", err)
		}
		yc.CustomWebhooks = parsed
	}
	// Boolean env flags — accept "1", "true", "yes" (case-insensitive).
	for _, b := range []struct {
		env string
		dst *bool
	}{
		{"FEEDBACK_API_CONSOLE_INSECURE_COOKIES", &yc.ConsoleInsecureCookies},
		{"FEEDBACK_API_CONSOLE_DEV_LOGIN", &yc.ConsoleDevLogin},
		{"FEEDBACK_API_RATE_LIMIT_DISABLED", &yc.RateLimitDisabled},
	} {
		if v := os.Getenv(b.env); v != "" {
			switch strings.ToLower(v) {
			case "1", "true", "yes", "on":
				*b.dst = true
			}
		}
	}
	// Int env overrides — bad values silently ignored (yaml/default wins).
	for _, n := range []struct {
		env string
		dst *int
	}{
		{"FEEDBACK_API_RATE_LIMIT_PER_MINUTE", &yc.RateLimitPerMinute},
		{"FEEDBACK_API_RATE_LIMIT_BURST", &yc.RateLimitBurst},
	} {
		if v := os.Getenv(n.env); v != "" {
			if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
				*n.dst = parsed
			}
		}
	}
	return nil
}
