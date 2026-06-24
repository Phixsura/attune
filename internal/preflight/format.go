package preflight

import (
	"fmt"
	"io"
	"strings"
)

// StatusIcon returns a terminal-friendly status indicator.
func StatusIcon(s Status) string {
	switch s {
	case StatusPass:
		return "\033[32m✓\033[0m" // green
	case StatusWarn:
		return "\033[33m!\033[0m" // yellow
	case StatusFail:
		return "\033[31m✗\033[0m" // red
	case StatusSkipped:
		return "\033[90m-\033[0m" // gray
	default:
		return "?"
	}
}

// StatusLabel returns an uppercase status label with ANSI color.
func StatusLabel(s Status) string {
	switch s {
	case StatusPass:
		return "\033[32mPASS\033[0m"
	case StatusWarn:
		return "\033[33mWARN\033[0m"
	case StatusFail:
		return "\033[31mFAIL\033[0m"
	default:
		return strings.ToUpper(string(s))
	}
}

// FormatText writes a human-readable report to w.
func FormatText(w io.Writer, report Report) {
	fmt.Fprintln(w, "attune preflight — production readiness report")
	fmt.Fprintln(w)

	currentCat := Category("")
	for _, r := range report.Checks {
		if r.Category != currentCat {
			currentCat = r.Category
			fmt.Fprintf(w, " %s\n", categoryTitle(currentCat))
		}
		fmt.Fprintf(w, "  %s %-28s %s\n", StatusIcon(r.Status), r.Name, r.Message)
		if r.Remediation != "" && (r.Status == StatusFail || r.Status == StatusWarn) {
			fmt.Fprintf(w, "    → %s\n", r.Remediation)
		}
	}

	pass, warn, fail, skipped := countByStatus(report.Checks)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Overall: %s (%d checks, %d passed, %d warnings, %d failures, %d skipped, %s)\n",
		StatusLabel(report.Status), len(report.Checks), pass, warn, fail, skipped, report.Elapsed)
}

func categoryTitle(c Category) string {
	switch c {
	case CategoryConfig:
		return "Config"
	case CategoryDatabase:
		return "Database"
	case CategoryMigration:
		return "Migration"
	case CategoryBackup:
		return "Backup"
	case CategoryEncryption:
		return "Encryption"
	case CategoryAuth:
		return "Auth"
	case CategoryMetrics:
		return "Metrics"
	case CategoryWorker:
		return "Worker"
	default:
		return string(c)
	}
}

func countByStatus(results []Result) (pass, warn, fail, skipped int) {
	for _, r := range results {
		switch r.Status {
		case StatusPass:
			pass++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		case StatusSkipped:
			skipped++
		}
	}
	return
}
