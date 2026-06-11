// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	llmconfigsvc "github.com/Phixsura/attune/internal/service/llmconfig"
)

func TestLLMSubcommandsRequireVerb(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]string) error
	}{
		{name: "channels", fn: runLLMChannels},
		{name: "abilities", fn: runLLMAbilities},
		{name: "routes", fn: runLLMRoutes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.fn(nil); err == nil {
				t.Fatal("expected usage error")
			}
		})
	}
}

func TestApplyChannelFlagOverridesKeepsUnvisitedValues(t *testing.T) {
	existing := llmconfigsvc.ChannelInput{
		Name:           "existing",
		Protocol:       "openai-compat",
		BaseURL:        "https://old.example.com",
		AuthMode:       "none",
		Status:         "disabled",
		Priority:       7,
		Weight:         ptrext.Of(3),
		TimeoutSeconds: ptrext.Of(45),
	}
	parsed := llmconfigsvc.ChannelInput{
		Name:           "new",
		Protocol:       "gemini",
		BaseURL:        "https://new.example.com",
		AuthMode:       "bearer",
		Status:         "enabled",
		Priority:       9,
		Weight:         ptrext.Of(1),
		TimeoutSeconds: ptrext.Of(60),
	}
	got := applyChannelFlagOverrides(existing, parsed, map[string]struct{}{
		"name":     {},
		"priority": {},
	})
	if got.Name != "new" || got.Priority != 9 {
		t.Fatalf("visited fields not applied: %+v", got)
	}
	if got.Protocol != "openai-compat" ||
		got.BaseURL != "https://old.example.com" ||
		got.AuthMode != "none" ||
		got.Status != "disabled" ||
		ptrext.Indirect(got.Weight) != 3 ||
		ptrext.Indirect(got.TimeoutSeconds) != 45 {
		t.Fatalf("unvisited fields changed: %+v", got)
	}
}
