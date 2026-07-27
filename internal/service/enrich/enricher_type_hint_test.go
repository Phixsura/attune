// SPDX-License-Identifier: Apache-2.0

package enrich

import (
	"strings"
	"testing"
)

func TestRenderPrompt_TypeHint(t *testing.T) {
	t.Parallel()
	cfg := ClassifyConfig{
		TypeHint: "bug_report",
		Language: "en",
	}
	prompt := renderPrompt(cfg, "My printer is on fire.")
	if !strings.Contains(prompt, "bug_report") {
		t.Error("expected type hint 'bug_report' in prompt")
	}
	if !strings.Contains(prompt, "pre-classified") {
		t.Error("expected 'pre-classified' phrasing in prompt")
	}
}

func TestRenderPrompt_NoTypeHint(t *testing.T) {
	t.Parallel()
	cfg := ClassifyConfig{
		Language: "en",
	}
	prompt := renderPrompt(cfg, "My printer is on fire.")
	if strings.Contains(prompt, "pre-classified") {
		t.Error("should not have type hint when TypeHint is empty")
	}
}
