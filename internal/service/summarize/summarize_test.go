// SPDX-License-Identifier: Apache-2.0

package summarize

import (
	"strings"
	"testing"
)

func TestBuildSummarizationPrompt(t *testing.T) {
	items := []FeedbackItem{
		{ID: 1, Content: "Login is slow", Kind: "bug", Severity: "high"},
		{ID: 2, Content: "Add dark mode", Kind: "feature_request", Severity: "low"},
		{ID: 3, Content: "Dashboard crashes", Kind: "bug", Severity: "critical"},
	}
	prompt := BuildSummarizationPrompt(items)

	if !strings.Contains(prompt, "#1") {
		t.Error("prompt missing item #1")
	}
	if !strings.Contains(prompt, "dark mode") {
		t.Error("prompt missing content")
	}
	if !strings.Contains(prompt, "JSON") {
		t.Error("prompt missing output format instruction")
	}
}

func TestComputeDistributions(t *testing.T) {
	items := []FeedbackItem{
		{Kind: "bug", Severity: "high"},
		{Kind: "bug", Severity: "low"},
		{Kind: "feature_request", Severity: "high"},
	}
	kindDist, sevDist := ComputeDistributions(items)

	if kindDist["bug"] != 2 {
		t.Errorf("bug count = %d, want 2", kindDist["bug"])
	}
	if kindDist["feature_request"] != 1 {
		t.Errorf("feature_request count = %d, want 1", kindDist["feature_request"])
	}
	if sevDist["high"] != 2 {
		t.Errorf("high severity = %d, want 2", sevDist["high"])
	}
}

func TestTruncate(t *testing.T) {
	short := "hello"
	if truncate(short, 10) != "hello" {
		t.Error("should not truncate short string")
	}
	long := strings.Repeat("a", 300)
	result := truncate(long, 200)
	if len(result) != 203 {
		t.Errorf("truncated len = %d, want 203", len(result))
	}
}
