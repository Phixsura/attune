package service

// enricher_parse.go — pure helpers extracted from enricher.go so the
// main file stays under the listen ≤300-line rule (CLAUDE.md 律 2).
// Everything here is stateless and trivially testable in isolation.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Phixsura/listen/internal/domain"
	"github.com/Phixsura/listen/internal/repo"
)

// buildSnapshot copies the persistence-side row + AI classification into
// the immutable Snapshot value the notifier layer consumes. Copy by
// value so the notifier can outlive any DB tx or HTTP scope.
func buildSnapshot(id int64, row *repo.EnrichInput, e domain.Enriched, at time.Time) domain.Snapshot {
	return domain.Snapshot{
		ID:         id,
		TenantID:   row.TenantID,
		Content:    row.Content,
		Source:     row.Source,
		UserID:     row.UserID,
		Title:      e.Title,
		Kind:       e.Kind,
		Modules:    e.Modules,
		Severity:   e.Severity,
		Priority:   e.Priority,
		EnrichedAt: at,
	}
}

// jsonObjRe pulls the first {...} block out of an LLM reply that
// wrapped its JSON in markdown fences or extra prose. Greedy on
// purpose — for the enrich prompt we expect exactly one object.
var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// parseEnrichJSON validates and decodes the LLM's reply. Returns a
// user-facing error string (the eval CLI reports it back to the
// human auditing accuracy), so keep the messages short and precise.
func parseEnrichJSON(s string) (domain.Enriched, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		m := jsonObjRe.FindString(s)
		if m == "" {
			return domain.Enriched{}, fmt.Errorf("no JSON object in response")
		}
		s = m
	}
	var r domain.Enriched
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return domain.Enriched{}, err
	}
	if r.Title == "" || r.Kind == "" || r.Severity == "" {
		return domain.Enriched{}, fmt.Errorf("missing required fields")
	}
	if !domain.ValidKinds[r.Kind] {
		return domain.Enriched{}, fmt.Errorf("invalid kind: %s", r.Kind)
	}
	if !domain.ValidSeverities[r.Severity] {
		return domain.Enriched{}, fmt.Errorf("invalid severity: %s", r.Severity)
	}
	return r, nil
}

// classifyErrResult buckets classify failures for the duration
// histogram label. Keep small + stable so Grafana queries don't break
// when new error kinds appear.
func classifyErrResult(err error) string {
	if err == nil {
		return "ok"
	}
	msg := err.Error()
	if len(msg) >= 4 && msg[:4] == "llm:" {
		return "llm_err"
	}
	if len(msg) >= 5 && msg[:5] == "parse" {
		return "parse_err"
	}
	return "other_err"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
