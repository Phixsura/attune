package database

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// DuplicatePrefix represents a numeric prefix shared by multiple migration files.
type DuplicatePrefix struct {
	Prefix int      `json:"prefix"`
	Files  []string `json:"files"`
}

// ErrDuplicatePrefix is returned when multiple migrations share a numeric prefix.
type ErrDuplicatePrefix struct {
	Duplicates []DuplicatePrefix
}

func (e ErrDuplicatePrefix) Error() string {
	var sb strings.Builder
	sb.WriteString("duplicate migration prefixes detected:\n")
	for _, d := range e.Duplicates {
		fmt.Fprintf(&sb, "  %03d: %s\n", d.Prefix, strings.Join(d.Files, ", "))
	}
	sb.WriteString("\nRenumber migrations to ensure unique prefixes.")
	return sb.String()
}

// DetectDuplicatePrefixes checks for numeric prefix collisions.
// Returns nil if no duplicates found.
func DetectDuplicatePrefixes(names []string) error {
	prefixes := make(map[int][]string) // prefix -> filenames
	for _, name := range names {
		parts := strings.SplitN(name, "_", 2)
		if len(parts) < 2 {
			continue // Skip malformed names
		}
		prefix, err := strconv.Atoi(parts[0])
		if err != nil {
			continue // Skip non-numeric prefixes
		}
		prefixes[prefix] = append(prefixes[prefix], name)
	}

	var dups []DuplicatePrefix
	for prefix, files := range prefixes {
		if len(files) > 1 {
			sort.Strings(files)
			dups = append(dups, DuplicatePrefix{Prefix: prefix, Files: files})
		}
	}

	if len(dups) > 0 {
		sort.Slice(dups, func(i, j int) bool { return dups[i].Prefix < dups[j].Prefix })
		return ErrDuplicatePrefix{Duplicates: dups}
	}
	return nil
}

// NoTxViolation represents a no-transaction migration that violates requirements.
type NoTxViolation struct {
	Filename string `json:"filename"`
	Reason   string `json:"reason"`
}

// ErrNoTxViolation is returned when no-transaction migrations are malformed.
type ErrNoTxViolation struct {
	Violations []NoTxViolation
}

func (e ErrNoTxViolation) Error() string {
	var sb strings.Builder
	sb.WriteString("no-transaction migration violations:\n")
	for _, v := range e.Violations {
		fmt.Fprintf(&sb, "  %s: %s\n", v.Filename, v.Reason)
	}
	sb.WriteString("\nNo-transaction migrations must be single-statement and idempotent.")
	return sb.String()
}

// VerifyNoTxDirectives checks that no-transaction migrations are properly formatted.
// They must be single-statement and contain idempotency guards.
func VerifyNoTxDirectives(names []string) error {
	var violations []NoTxViolation

	for _, name := range names {
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			continue
		}

		if !isNoTxMigration(body) {
			continue
		}

		// Check for multiple statements (count semicolons outside comments)
		semicolons := countSemicolons(body)
		if semicolons > 1 {
			violations = append(violations, NoTxViolation{
				Filename: name,
				Reason:   fmt.Sprintf("multiple statements (%d semicolons)", semicolons),
			})
			continue
		}

		// Check for idempotency guard
		bodyLower := bytes.ToLower(body)
		hasIfExists := bytes.Contains(bodyLower, []byte("if exists")) ||
			bytes.Contains(bodyLower, []byte("if not exists"))
		hasConcurrently := bytes.Contains(bodyLower, []byte("concurrently"))

		if !hasIfExists && !hasConcurrently {
			violations = append(violations, NoTxViolation{
				Filename: name,
				Reason:   "no idempotency guard (IF NOT EXISTS / IF EXISTS / CONCURRENTLY)",
			})
		}
	}

	if len(violations) > 0 {
		return ErrNoTxViolation{Violations: violations}
	}
	return nil
}

// countSemicolons counts semicolons outside SQL comments.
func countSemicolons(body []byte) int {
	count := 0
	inLineComment := false

	lines := bytes.Split(body, []byte("\n"))
	for _, line := range lines {
		trimmed := bytes.TrimSpace(line)
		if bytes.HasPrefix(trimmed, []byte("--")) {
			continue // Skip comment lines
		}
		// Count semicolons in non-comment lines
		for _, b := range line {
			if b == ';' && !inLineComment {
				count++
			}
		}
	}
	return count
}

// LoadMigrationNames returns the sorted list of embedded migration filenames.
func LoadMigrationNames() ([]string, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
