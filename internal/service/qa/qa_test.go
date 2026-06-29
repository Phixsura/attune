// SPDX-License-Identifier: Apache-2.0

package qa

import (
	"strings"
	"testing"
)

func TestBuildRAGPrompt(t *testing.T) {
	items := []RetrievedItem{
		{FeedbackID: 1, Content: "Login is broken since yesterday", Kind: "bug", Severity: "high", Score: 0.95},
		{FeedbackID: 2, Content: "Would like dark mode", Kind: "feature", Severity: "low", Score: 0.80},
	}
	prompt := BuildRAGPrompt("What are the most reported bugs?", items)
	if !strings.Contains(prompt, "Login is broken") {
		t.Error("prompt should contain feedback content")
	}
	if !strings.Contains(prompt, "What are the most reported bugs?") {
		t.Error("prompt should contain the question")
	}
	if !strings.Contains(prompt, "[bug/high]") {
		t.Error("prompt should contain kind/severity labels")
	}
}

func TestBuildRAGPrompt_TruncatesLongContent(t *testing.T) {
	long := strings.Repeat("x", 500)
	items := []RetrievedItem{{FeedbackID: 1, Content: long, Kind: "bug", Severity: "high"}}
	prompt := BuildRAGPrompt("test?", items)
	if strings.Contains(prompt, strings.Repeat("x", 500)) {
		t.Error("long content should be truncated")
	}
	if !strings.Contains(prompt, "...") {
		t.Error("truncated content should end with ...")
	}
}

func TestBuildRAGPrompt_MaxItems(t *testing.T) {
	items := make([]RetrievedItem, 15)
	for i := range items {
		items[i] = RetrievedItem{Content: "item", Kind: "bug", Severity: "low"}
	}
	prompt := BuildRAGPrompt("q?", items)
	count := strings.Count(prompt, "[bug/low]")
	if count > 10 {
		t.Errorf("should cap at 10 items, found %d", count)
	}
}

func TestExtractSourceIDs(t *testing.T) {
	items := []RetrievedItem{
		{FeedbackID: 1}, {FeedbackID: 2}, {FeedbackID: 1}, {FeedbackID: 3},
	}
	ids := ExtractSourceIDs(items)
	if len(ids) != 3 {
		t.Errorf("got %d unique IDs, want 3", len(ids))
	}
}

func TestExtractSourceIDs_Empty(t *testing.T) {
	ids := ExtractSourceIDs(nil)
	if ids != nil {
		t.Error("empty input should return nil")
	}
}
