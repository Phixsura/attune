// SPDX-License-Identifier: Apache-2.0

// Package summarize provides feedback corpus summarization.
package summarize

import (
	"fmt"
	"strings"
)

// FeedbackItem is the minimal input for summarization.
type FeedbackItem struct {
	ID       int64
	Content  string
	Kind     string
	Severity string
}

// CorpusSummary holds the structured summary output.
type CorpusSummary struct {
	TotalItems   int
	TopThemes    []ThemeSummary
	SeverityDist map[string]int
	KindDist     map[string]int
	Prompt       string
}

// ThemeSummary groups related items under a theme label.
type ThemeSummary struct {
	Label string
	Count int
	IDs   []int64
}

// BuildSummarizationPrompt constructs an LLM prompt for corpus summarization.
func BuildSummarizationPrompt(items []FeedbackItem) string {
	var b strings.Builder
	b.WriteString("Summarize the following user feedback items. ")
	b.WriteString("Identify the top 5 recurring themes, group items by theme, ")
	b.WriteString("and provide a brief executive summary.\n\n")

	for _, item := range items {
		fmt.Fprintf(&b, "- [#%d] (%s/%s) %s\n", item.ID, item.Kind, item.Severity, truncate(item.Content, 200))
	}

	b.WriteString("\nProvide output as JSON with keys: themes (array of {label, count, ids}), executive_summary (string).")
	return b.String()
}

// ComputeDistributions computes kind and severity distributions.
func ComputeDistributions(items []FeedbackItem) (kindDist, severityDist map[string]int) {
	kindDist = make(map[string]int)
	severityDist = make(map[string]int)
	for _, item := range items {
		kindDist[item.Kind]++
		severityDist[item.Severity]++
	}
	return kindDist, severityDist
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
