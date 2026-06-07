package eval

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/domain"
)

// FormatReport renders a Markdown report from an EvalReport — one row
// per Dimension with the appropriate metric (match rate for single,
// average IoU for multi).
func FormatReport(rep *EvalReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# attune eval report · %s\n\n", rep.GeneratedAt.Format(time.RFC3339))
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
	b.WriteString("\n## agreement (per dimension)\n\n")
	b.WriteString("| dimension | kind | metric | red line |\n")
	b.WriteString("|-----------|------|--------|---------|\n")
	for _, name := range sortedDimNames(rep.Dims) {
		score := rep.Dims[name]
		switch score.Kind {
		case domain.DimSingle:
			pct := 100.0 * float64(score.Match) / float64(score.Total)
			fmt.Fprintf(&b, "| %s | single | match rate %.1f%% (%d/%d) | 80%% |\n",
				name, pct, score.Match, score.Total)
		case domain.DimMulti:
			avg := score.SumIoU / float64(score.Total)
			fmt.Fprintf(&b, "| %s | multi | avg IoU %.2f | 0.75 |\n", name, avg)
		}
	}
	if len(rep.Mismatches) == 0 {
		return b.String()
	}
	b.WriteString("\n## top 10 mismatches\n\n")
	b.WriteString("| id | dim diffs | content |\n")
	b.WriteString("|----|----------|--------|\n")
	for i, m := range rep.Mismatches {
		if i >= 10 {
			break
		}
		fmt.Fprintf(&b, "| %d | %s | %s |\n",
			m.FeedbackID, summarizeDiffs(m.Diffs), truncateForReport(m.Content, 60))
	}
	return b.String()
}

func summarizeDiffs(diffs map[string]DimDiff) string {
	if len(diffs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(diffs))
	for _, name := range sortedDiffNames(diffs) {
		d := diffs[name]
		switch d.Kind {
		case domain.DimSingle:
			parts = append(parts, fmt.Sprintf("%s: %v→%v", name, d.Old, d.New))
		case domain.DimMulti:
			parts = append(parts, fmt.Sprintf("%s: IoU %.2f", name, d.IoU))
		}
	}
	return strings.Join(parts, "; ")
}

func sortedDimNames(scores map[string]DimScore) []string {
	out := make([]string, 0, len(scores))
	for k := range scores {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedDiffNames(diffs map[string]DimDiff) []string {
	out := make([]string, 0, len(diffs))
	for k := range diffs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func truncateForReport(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
