// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"strings"

	"github.com/Phixsura/attune/internal/pkg/ptrext"
	repo "github.com/Phixsura/attune/internal/repo/survey"
)

func normalizeContent(surveyType string, content map[string]any) map[string]any {
	out := defaultContent(normalizeSurveyType(surveyType))
	for key, value := range content {
		if text, ok := value.(string); ok {
			out[key] = boundedString(strings.TrimSpace(text), 1000)
			continue
		}
		out[key] = value
	}
	return out
}

func defaultContent(surveyType string) map[string]any {
	if surveyType == repo.TypeCES {
		return map[string]any{
			"title":          "Resolution feedback",
			"intro":          "Your feedback helps us improve.",
			"question":       "How easy was it to get this resolved?",
			"comment_prompt": "What made this easy or difficult?",
			"thank_you":      "Thanks for your feedback.",
		}
	}
	return map[string]any{
		"title":          "Resolution feedback",
		"intro":          "Your feedback helps us improve.",
		"question":       "How satisfied are you with the resolution?",
		"comment_prompt": "What could we improve?",
		"thank_you":      "Thanks for your feedback.",
	}
}

func publicText(content map[string]any, key string) string {
	value, ok := content[key].(string)
	if !ok {
		return ""
	}
	return value
}

func campaignSnapshot(c repo.Campaign) map[string]any {
	return map[string]any{
		"campaign_id":                      c.ID.String(),
		"name":                             c.Name,
		"survey_type":                      c.SurveyType,
		"trigger_event":                    c.TriggerEvent,
		"distribution_mode":                c.DistributionMode,
		"dedupe_policy":                    c.DedupePolicy,
		"content_version":                  c.ContentVersion,
		"content":                          normalizeObject(c.Content),
		"locale":                           c.Locale,
		"low_score_threshold":              c.LowScoreThreshold,
		"expires_after_days":               c.ExpiresAfterDays,
		"sampling_percent":                 c.SamplingPercent,
		"min_days_between_contact":         c.MinDaysBetweenContact,
		"max_daily_invitations":            c.MaxDailyInvitations,
		"require_recent_customer_activity": c.RequireRecentCustomerActivity,
		"recent_activity_days":             c.RecentActivityDays,
		"suppress_auto_resolved":           c.SuppressAutoResolved,
	}
}

func normalizeObject(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(value))
	for key, item := range value {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		out[key] = item
	}
	return out
}

func normalizedLocale(raw string, fallback string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = strings.TrimSpace(fallback)
	}
	if raw == "" {
		return "en"
	}
	if len(raw) > 35 {
		return raw[:35]
	}
	return raw
}

func boundedString(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func intOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return ptrext.Indirect(value)
}

func numberOr(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return ptrext.Indirect(value)
}

func boolOr(value *bool, fallback bool) bool {
	if value == nil {
		return fallback
	}
	return ptrext.Indirect(value)
}

func validateScore(surveyType string, score int) error {
	minScore, maxScore := ScoreRange(surveyType)
	if score < minScore || score > maxScore {
		return ErrValidation
	}
	return nil
}

func validateLowScoreThreshold(surveyType string, threshold int) error {
	minScore, maxScore := ScoreRange(surveyType)
	if threshold < minScore || threshold > maxScore {
		return ErrValidation
	}
	return nil
}

func ScoreRange(surveyType string) (int, int) {
	if surveyType == repo.TypeCES {
		return 1, 7
	}
	return 1, 5
}
