// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	llmconfigsvc "github.com/Phixsura/attune/internal/service/llmconfig"
)

const testUUID = "550e8400-e29b-41d4-a716-446655440000"

// ── runLLM dispatch — abilities and routes arms ───────────────────────────────

func TestRunLLM_AbilitiesDispatch(t *testing.T) {
	// "abilities list" with a valid UUID reaches withLLMConfigService which
	// calls config.Load(); without ATTUNE_CONFIG the load fails — that is the
	// error path we want to exercise.
	err := runLLM([]string{"abilities", "list", "--channel", testUUID})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLM_RoutesDispatch(t *testing.T) {
	// "routes list" reaches withLLMConfigService directly.
	err := runLLM([]string{"routes", "list"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

// ── runLLMChannels — remaining verb arms ─────────────────────────────────────

func TestRunLLMChannels_ListReachesConfigLoad(t *testing.T) {
	err := runLLMChannels([]string{"list"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMChannels_GetDispatch(t *testing.T) {
	// "get" with a valid UUID reaches withLLMConfigService.
	err := runLLMChannels([]string{"get", "--id", testUUID})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMChannels_CreateDispatch(t *testing.T) {
	err := runLLMChannels([]string{"create", "--name", "test", "--protocol", "openai-compat", "--base-url", "https://example.com"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMChannels_UpdateDispatch(t *testing.T) {
	err := runLLMChannels([]string{"update", "--id", testUUID, "--name", "renamed"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMChannels_DeleteDispatch(t *testing.T) {
	err := runLLMChannels([]string{"delete", "--id", testUUID})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMChannels_TestDispatch(t *testing.T) {
	err := runLLMChannels([]string{"test", "--id", testUUID})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

// ── runLLMChannelGet ──────────────────────────────────────────────────────────

func TestRunLLMChannelGet_ValidUUID_ReachesConfigLoad(t *testing.T) {
	err := runLLMChannelGet([]string{"--id", testUUID})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMChannelGet_FlagParseError(t *testing.T) {
	err := runLLMChannelGet([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
}

// ── runLLMChannelCreate ───────────────────────────────────────────────────────

func TestRunLLMChannelCreate_ValidFlags_ReachesConfigLoad(t *testing.T) {
	err := runLLMChannelCreate([]string{
		"--name", "my-channel",
		"--protocol", "openai-compat",
		"--base-url", "https://api.example.com",
	})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMChannelCreate_FlagParseError(t *testing.T) {
	err := runLLMChannelCreate([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
}

func TestRunLLMChannelCreate_NoFlags_ReachesConfigLoad(t *testing.T) {
	// No flags is valid — the command still reaches withLLMConfigService.
	err := runLLMChannelCreate([]string{})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

// ── runLLMChannelUpdate ───────────────────────────────────────────────────────

func TestRunLLMChannelUpdate_ValidUUID_ReachesConfigLoad(t *testing.T) {
	err := runLLMChannelUpdate([]string{"--id", testUUID, "--name", "renamed"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMChannelUpdate_FlagParseError(t *testing.T) {
	err := runLLMChannelUpdate([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
}

// ── runLLMChannelDelete ───────────────────────────────────────────────────────

func TestRunLLMChannelDelete_ValidUUID_ReachesConfigLoad(t *testing.T) {
	err := runLLMChannelDelete([]string{"--id", testUUID})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMChannelDelete_FlagParseError(t *testing.T) {
	err := runLLMChannelDelete([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
}

// ── runLLMChannelTest ─────────────────────────────────────────────────────────

func TestRunLLMChannelTest_ValidUUID_ReachesConfigLoad(t *testing.T) {
	err := runLLMChannelTest([]string{"--id", testUUID})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMChannelTest_FlagParseError(t *testing.T) {
	err := runLLMChannelTest([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
}

// ── applyChannelFlagOverrides — all flags set ─────────────────────────────────

func TestApplyChannelFlagOverrides_AllFlagsSet(t *testing.T) {
	dst := llmconfigsvc.ChannelInput{
		Name:           "old-name",
		Protocol:       "openai-compat",
		BaseURL:        "https://old.example.com",
		AuthMode:       "none",
		Status:         "disabled",
		Priority:       0,
		Weight:         ptrext.Of(1),
		TimeoutSeconds: ptrext.Of(30),
	}
	src := llmconfigsvc.ChannelInput{
		Name:           "new-name",
		Protocol:       "anthropic",
		BaseURL:        "https://new.example.com",
		AuthMode:       "bearer",
		Status:         "enabled",
		Priority:       5,
		Weight:         ptrext.Of(3),
		TimeoutSeconds: ptrext.Of(90),
	}
	set := map[string]struct{}{
		"name":            {},
		"protocol":        {},
		"base-url":        {},
		"auth-mode":       {},
		"status":          {},
		"priority":        {},
		"weight":          {},
		"timeout-seconds": {},
	}
	got := applyChannelFlagOverrides(dst, src, set)
	if got.Name != "new-name" {
		t.Errorf("Name: got %q, want %q", got.Name, "new-name")
	}
	if got.Protocol != "anthropic" {
		t.Errorf("Protocol: got %q, want %q", got.Protocol, "anthropic")
	}
	if got.BaseURL != "https://new.example.com" {
		t.Errorf("BaseURL: got %q, want %q", got.BaseURL, "https://new.example.com")
	}
	if got.AuthMode != "bearer" {
		t.Errorf("AuthMode: got %q, want %q", got.AuthMode, "bearer")
	}
	if got.Status != "enabled" {
		t.Errorf("Status: got %q, want %q", got.Status, "enabled")
	}
	if got.Priority != 5 {
		t.Errorf("Priority: got %d, want 5", got.Priority)
	}
	if ptrext.Indirect(got.Weight) != 3 {
		t.Errorf("Weight: got %d, want 3", ptrext.Indirect(got.Weight))
	}
	if ptrext.Indirect(got.TimeoutSeconds) != 90 {
		t.Errorf("TimeoutSeconds: got %d, want 90", ptrext.Indirect(got.TimeoutSeconds))
	}
}

// ── runLLMAbilities — remaining verb arms ─────────────────────────────────────

func TestRunLLMAbilities_UpsertDispatch(t *testing.T) {
	err := runLLMAbilities([]string{"upsert", "--channel", testUUID, "--logical-model", "gpt-4", "--provider-model", "gpt-4"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMAbilities_DeleteDispatch(t *testing.T) {
	err := runLLMAbilities([]string{"delete", "--channel", testUUID, "--logical-model", "gpt-4"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

// ── runLLMAbilitiesList ───────────────────────────────────────────────────────

func TestRunLLMAbilitiesList_ValidUUID_ReachesConfigLoad(t *testing.T) {
	err := runLLMAbilitiesList([]string{"--channel", testUUID})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMAbilitiesList_FlagParseError(t *testing.T) {
	err := runLLMAbilitiesList([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
}

// ── runLLMAbilityUpsert ───────────────────────────────────────────────────────

func TestRunLLMAbilityUpsert_ValidUUID_ReachesConfigLoad(t *testing.T) {
	err := runLLMAbilityUpsert([]string{
		"--channel", testUUID,
		"--logical-model", "gpt-4",
		"--provider-model", "gpt-4",
	})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMAbilityUpsert_FlagParseError(t *testing.T) {
	err := runLLMAbilityUpsert([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
}

// ── runLLMAbilityDelete ───────────────────────────────────────────────────────

func TestRunLLMAbilityDelete_ValidUUID_ReachesConfigLoad(t *testing.T) {
	err := runLLMAbilityDelete([]string{
		"--channel", testUUID,
		"--logical-model", "gpt-4",
	})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMAbilityDelete_FlagParseError(t *testing.T) {
	err := runLLMAbilityDelete([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
}

// ── runLLMRoutes — remaining verb arms ───────────────────────────────────────

func TestRunLLMRoutes_ListReachesConfigLoad(t *testing.T) {
	err := runLLMRoutes([]string{"list"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMRoutes_UpsertDispatch(t *testing.T) {
	err := runLLMRoutes([]string{"upsert", "--logical-model", "gpt-4", "--purpose", "enrich"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMRoutes_DeleteDispatch(t *testing.T) {
	err := runLLMRoutes([]string{"delete", "--purpose", "enrich"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

// ── runLLMRouteUpsert ─────────────────────────────────────────────────────────

func TestRunLLMRouteUpsert_ValidFlags_ReachesConfigLoad(t *testing.T) {
	err := runLLMRouteUpsert([]string{
		"--logical-model", "gpt-4",
		"--purpose", "enrich",
	})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMRouteUpsert_NoFlags_ReachesConfigLoad(t *testing.T) {
	// All flags have defaults, so no-flag parse succeeds and reaches config.Load.
	err := runLLMRouteUpsert([]string{})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMRouteUpsert_FlagParseError(t *testing.T) {
	err := runLLMRouteUpsert([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
}

// ── runLLMRouteDelete ─────────────────────────────────────────────────────────

func TestRunLLMRouteDelete_ValidFlags_ReachesConfigLoad(t *testing.T) {
	err := runLLMRouteDelete([]string{"--purpose", "enrich"})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMRouteDelete_NoFlags_ReachesConfigLoad(t *testing.T) {
	// All flags have defaults; reaches config.Load immediately.
	err := runLLMRouteDelete([]string{})
	if err == nil {
		t.Fatal("expected error from config.Load()")
	}
}

func TestRunLLMRouteDelete_FlagParseError(t *testing.T) {
	err := runLLMRouteDelete([]string{"--unknown-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
}
