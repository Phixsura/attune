package enrich

// enricher_parse.go — pure helpers extracted from enricher.go so the
// main file stays under the attune no-grab-bag-files guidance.
// Everything here is stateless and trivially testable in isolation.

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Phixsura/attune/internal/domain"
	"github.com/Phixsura/attune/internal/pkg/ptrext"
	"github.com/Phixsura/attune/internal/repo/feedback"
)

// buildSnapshot copies the persistence-side row + AI classification into
// the immutable Snapshot value the notifier layer consumes. Copy by
// value so the notifier can outlive any DB tx or HTTP scope.
func buildSnapshot(id int64, row *feedback.EnrichInput, e domain.Enriched, at time.Time) domain.Snapshot {
	return domain.Snapshot{
		ID:                       id,
		TenantID:                 row.TenantID,
		Content:                  row.Content,
		Source:                   row.Source,
		UserID:                   row.UserID,
		Language:                 row.Language,
		DisplayLocale:            snapshotDisplayLocale(row.DisplayLocale, e),
		Title:                    e.Title,
		DisplayTitle:             e.DisplayTitle,
		Attrs:                    e.Attrs,
		IsUrgent:                 e.IsUrgent,
		Rationale:                e.Rationale,
		DisplayRationale:         e.DisplayRationale,
		ClassificationConfidence: e.ClassificationConfidence,
		SubmittedAt:              row.CreatedAt, // #82: actual user submission time, not enrichment time
		EnrichedAt:               at,
	}
}

func snapshotDisplayLocale(displayLocale string, e domain.Enriched) string {
	if !hasDisplayOutput(e) {
		return ""
	}
	return displayLocaleForTenantLocale(displayLocale)
}

func hasDisplayOutput(e domain.Enriched) bool {
	return e.DisplayTitle != "" || e.DisplayRationale != ""
}

func normalizeEnrichedDisplay(e domain.Enriched, sourceLanguage, displayLocale string) domain.Enriched {
	if !sameLanguage(sourceLanguage, displayLocale) {
		return e
	}
	if e.DisplayTitle == "" {
		e.DisplayTitle = e.Title
	}
	if e.DisplayRationale == "" {
		e.DisplayRationale = e.Rationale
	}
	return e
}

// jsonObjRe pulls the first {...} block out of an LLM reply that
// wrapped its JSON in markdown fences or extra prose. Greedy on
// purpose — for the enrich prompt we expect exactly one object.
var jsonObjRe = regexp.MustCompile(`(?s)\{.*\}`)

// markdownFenceRe strips markdown code fences (case-insensitive, any
// language tag) so the LLM's ```json / ```JSON / ``` are all handled.
var markdownFenceRe = regexp.MustCompile("(?is)^```(?:\\w*)\\s*\n?|```\\s*$")

// parseEnrichJSON decodes the LLM's reply into the dimensions-driven
// Enriched shape. The LLM emits a flat object whose top-level keys
// are `title`, `rationale`, and one entry per configured Dimension.
// We pull `title` and `rationale` out by name and treat everything
// else as the raw attrs map — gate (2) downstream filters those
// entries against the tenant's DimensionSet so unknown / off-list
// keys are dropped without rejecting the whole row.
func parseEnrichJSON(s string) (domain.Enriched, error) {
	s = strings.TrimSpace(s)
	s = markdownFenceRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") {
		m := jsonObjRe.FindString(s)
		if m == "" {
			return domain.Enriched{}, fmt.Errorf("no JSON object in response")
		}
		s = m
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return domain.Enriched{}, err
	}
	title, _ := raw["title"].(string)
	if title == "" {
		return domain.Enriched{}, fmt.Errorf("missing required field: title")
	}
	rationale, _ := raw["rationale"].(string)
	displayTitle, _ := raw["display_title"].(string)
	displayRationale, _ := raw["display_rationale"].(string)
	confidence := parseClassificationConfidence(raw["classification_confidence"])
	delete(raw, "title")
	delete(raw, "rationale")
	delete(raw, "display_title")
	delete(raw, "display_rationale")
	delete(raw, "classification_confidence")
	return domain.Enriched{
		Title:                    title,
		DisplayTitle:             displayTitle,
		Rationale:                rationale,
		DisplayRationale:         displayRationale,
		ClassificationConfidence: confidence,
		Attrs:                    raw,
	}, nil
}

func parseClassificationConfidence(v any) *float64 {
	var n float64
	switch x := v.(type) {
	case nil:
		return nil
	case float64:
		n = x
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return nil
		}
		n = parsed
	default:
		return nil
	}
	if math.IsNaN(n) || math.IsInf(n, 0) {
		return nil
	}
	if n < 0 {
		n = 0
	}
	if n > 1 {
		n = 1
	}
	return ptrext.Of(n)
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
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}
