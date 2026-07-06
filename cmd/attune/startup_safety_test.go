// SPDX-License-Identifier: Apache-2.0
// ptrext:file-allow test fixtures use config pointers.
package main

import (
	"context"
	"testing"

	"github.com/Phixsura/attune/internal/infra/config"
)

func TestValidateConfigSafety_RejectsProductionHTTP(t *testing.T) {
	t.Parallel()

	err := validateConfigSafety(context.Background(), &config.Config{
		Profile:           config.ProfileProduction,
		ConsoleBaseURL:    "http://console.example.com",
		ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
	})
	if err == nil {
		t.Fatal("expected production HTTP base_url to be rejected")
	}
}

func TestValidateConfigSafety_RejectsMalformedConsoleBaseURL(t *testing.T) {
	t.Parallel()

	err := validateConfigSafety(context.Background(), &config.Config{
		ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
		ConsoleBaseURL:    "://bad",
	})
	if err == nil {
		t.Fatal("expected malformed console.base_url to be rejected")
	}
}

func TestValidateConfigSafety_AllowsDevHTTP(t *testing.T) {
	t.Parallel()

	err := validateConfigSafety(context.Background(), &config.Config{
		Profile:           config.ProfileDev,
		ConsoleBaseURL:    "http://localhost:8080",
		ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
	})
	if err != nil {
		t.Fatalf("validateConfigSafety() err = %v; want nil", err)
	}
}

func TestValidateConfigSafety_RejectsProductionPrivateEgress(t *testing.T) {
	t.Parallel()

	err := validateConfigSafety(context.Background(), &config.Config{
		Profile:           config.ProfileProduction,
		ConsoleBaseURL:    "https://console.example.com",
		ConsoleSessionKey: "this-is-a-sufficiently-long-session-key-for-testing",
		Security: config.SecurityConfig{
			AllowPrivateEgress: true,
		},
	})
	if err == nil {
		t.Fatal("expected production private egress to be rejected")
	}
}

func TestValidateBootstrapSafety_SkipsWhenConsoleDisabled(t *testing.T) {
	t.Parallel()

	if err := validateBootstrapSafety(context.Background(), &config.Config{}, nil); err != nil {
		t.Fatalf("validateBootstrapSafety() err = %v; want nil", err)
	}
}
