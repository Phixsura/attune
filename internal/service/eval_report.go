package service

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// FormatReport renders a markdown report from an EvalReport. Output
// matches design doc v0.4 §4.6.
func FormatReport(rep *EvalReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# listen eval report · %s\n\n", rep.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "- mode: %s\n- label source: %s\n", rep.Mode, rep.LabelSource)
	if !rep.Since.IsZero() {
		fmt.Fprintf(&b, "- since: %s\n", rep.Since.Format(time.RFC3339))
	}
	fmt.Fprintf(&b, "- sample size: %d\n", rep.SampleSize)
	if rep.LLMCostYuan > 0 {
		fmt.Fprintf(&b, "- LLM cost (est): ¥%.2f\n", rep.LLMCostYuan)
	}
	if rep.SampleSize == 0 {
		b.WriteString("\n(no rows sampled — widen --since or check enrichment_status='done' rows exist)\n")
		return b.String()
	}
	kindPct := 100.0 * float64(rep.KindMatches) / float64(rep.SampleSize)
	sevPct := 100.0 * float64(rep.SevMatches) / float64(rep.SampleSize)
	moduleAvg := rep.ModuleSumIoU / float64(rep.SampleSize)
	b.WriteString("\n## agreement\n\n")
	b.WriteString("| dimension | match rate | red line |\n")
	b.WriteString("|-----------|-----------|---------|\n")
	fmt.Fprintf(&b, "| kind | %.1f%% (%d/%d) | 80%% |\n", kindPct, rep.KindMatches, rep.SampleSize)
	fmt.Fprintf(&b, "| severity | %.1f%% (%d/%d) | 80%% |\n", sevPct, rep.SevMatches, rep.SampleSize)
	fmt.Fprintf(&b, "| module Jaccard | avg %.2f | 0.75 |\n", moduleAvg)
	if len(rep.Mismatches) == 0 {
		return b.String()
	}
	b.WriteString("\n## top 10 mismatches\n\n")
	b.WriteString("| id | old kind | new kind | old sev | new sev | content |\n")
	b.WriteString("|----|---------|---------|--------|--------|--------|\n")
	for i, m := range rep.Mismatches {
		if i >= 10 {
			break
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s |\n",
			m.FeedbackID, m.OldKind, m.NewKind, m.OldSeverity, m.NewSeverity,
			truncateForReport(m.Content, 60))
	}
	return b.String()
}

func sortMismatches(ms []Mismatch) {
	// Sort by "worst" first: lower Jaccard means more module divergence.
	sort.Slice(ms, func(i, j int) bool {
		return ms[i].ModuleJaccard < ms[j].ModuleJaccard
	})
}

func headerIndex(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[strings.TrimSpace(h)] = i
	}
	return m
}

func safeColumn(rec []string, idx map[string]int, col string) string {
	if i, ok := idx[col]; ok && i < len(rec) {
		return rec[i]
	}
	return ""
}

func splitModules(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "|")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func truncateForReport(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
