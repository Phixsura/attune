// ptrext:file-allow test fixtures use handler/config pointers and json decoding.
package system

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/infra/config"
)

func TestReleaseHandler_ReturnsRuntimeMetadata(t *testing.T) {
	h := NewReleaseHandler(&config.Config{
		Profile: config.ProfileProduction,
		Observability: config.ObservabilityConfig{
			ServiceVersion: "5d6ea83",
			Environment:    "production",
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/system/release", nil)
	h.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q; want application/json", ct)
	}

	var info ReleaseInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if info.ServiceVersion != "5d6ea83" {
		t.Fatalf("ServiceVersion = %q; want %q", info.ServiceVersion, "5d6ea83")
	}
	if info.Environment != "production" {
		t.Fatalf("Environment = %q; want production", info.Environment)
	}
	if info.Profile != config.ProfileProduction {
		t.Fatalf("Profile = %q; want production", info.Profile)
	}
	if info.LifecycleState != string(domain.LifecycleStateSupported) {
		t.Fatalf("LifecycleState = %q; want %q", info.LifecycleState, domain.LifecycleStateSupported)
	}
	if info.OwnerTeam != defaultReleaseOwnerTeam {
		t.Fatalf("OwnerTeam = %q; want %q", info.OwnerTeam, defaultReleaseOwnerTeam)
	}
	if len(info.CompatibilityRules) != len(domain.CompatibilityRules()) {
		t.Fatalf("CompatibilityRules length = %d; want %d", len(info.CompatibilityRules), len(domain.CompatibilityRules()))
	}
	if info.CompatibilityRules[0].Key != string(domain.CompatibilityRuleAdditive) {
		t.Fatalf("CompatibilityRules[0].Key = %q; want %q", info.CompatibilityRules[0].Key, domain.CompatibilityRuleAdditive)
	}
	if len(info.Glossary) != len(domain.PlatformGlossary()) {
		t.Fatalf("Glossary length = %d; want %d", len(info.Glossary), len(domain.PlatformGlossary()))
	}
	if info.Glossary[0].Key != string(domain.PlatformTermEnvironment) {
		t.Fatalf("Glossary[0].Key = %q; want %q", info.Glossary[0].Key, domain.PlatformTermEnvironment)
	}
	if info.RunbookURL != defaultReleaseRunbookURL {
		t.Fatalf("RunbookURL = %q; want %q", info.RunbookURL, defaultReleaseRunbookURL)
	}
	if info.EscalationURL != defaultReleaseEscalation {
		t.Fatalf("EscalationURL = %q; want %q", info.EscalationURL, defaultReleaseEscalation)
	}
	if _, err := time.Parse(time.RFC3339Nano, info.StartedAt); err != nil {
		t.Fatalf("StartedAt = %q; want RFC3339 timestamp: %v", info.StartedAt, err)
	}
}

func TestReleaseHandler_BlocksDevBuildInProduction(t *testing.T) {
	h := NewReleaseHandler(&config.Config{
		Profile: config.ProfileProduction,
		Observability: config.ObservabilityConfig{
			ServiceVersion: "dev",
			Environment:    "production",
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/system/release", nil)
	h.ServeHTTP(rec, req)

	var info ReleaseInfo
	if err := json.NewDecoder(rec.Body).Decode(&info); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if info.LifecycleState != string(domain.LifecycleStateBlocked) {
		t.Fatalf("LifecycleState = %q; want %q", info.LifecycleState, domain.LifecycleStateBlocked)
	}
}
