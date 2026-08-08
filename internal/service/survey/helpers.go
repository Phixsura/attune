// SPDX-License-Identifier: Apache-2.0

package survey

import (
	"strconv"
	"strings"

	"github.com/Phixsura/attune/internal/domain"
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

func defaultContent(surveyType string, locales ...string) map[string]any {
	if surveyType == repo.TypeNPS {
		locale := ""
		if len(locales) > 0 {
			locale = locales[0]
		}
		content, _ := npsContentForRevision(locale, domain.CurrentNPSContentRevision)
		return content
	}
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

func npsContentForRevision(locale, revision string) (map[string]any, bool) {
	content, ok := domain.CanonicalNPSContentForRevision(locale, revision)
	if !ok {
		return nil, false
	}
	return map[string]any{
		"title":          content.Title,
		"intro":          content.Intro,
		"question":       content.Question,
		"comment_prompt": content.CommentPrompt,
		"thank_you":      content.ThankYou,
	}, true
}

func npsContentRevisionFor(locale string, content map[string]any) string {
	revision, ok := domain.NPSContentRevisionFor(locale, domain.NPSContent{
		Title:         snapshotString(content, "title"),
		Intro:         snapshotString(content, "intro"),
		Question:      snapshotString(content, "question"),
		CommentPrompt: snapshotString(content, "comment_prompt"),
		ThankYou:      snapshotString(content, "thank_you"),
	})
	if !ok {
		return domain.CurrentNPSContentRevision
	}
	return revision
}

func publicText(content map[string]any, key string) string {
	value, ok := content[key].(string)
	if !ok {
		return ""
	}
	return value
}

func campaignSnapshot(c repo.Campaign) map[string]any {
	snapshot := map[string]any{
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
	if c.SurveyType == repo.TypeNPS {
		snapshot["nps_content_revision"] = npsContentRevisionFor(c.Locale, c.Content)
	}
	return snapshot
}

// campaignFromInvitationSnapshot returns the definition promised to a recipient.
// The current campaign remains the authority for whether the invitation is active.
func campaignFromInvitationSnapshot(current repo.Campaign, invitation repo.Invitation) (repo.Campaign, error) {
	if current.SurveyType == repo.TypeNPS {
		current.Locale = domain.CanonicalNPSLocale(current.Locale)
		content, ok := npsContentForRevision(current.Locale, npsContentRevisionFor(current.Locale, current.Content))
		if !ok {
			return repo.Campaign{}, ErrDisabled
		}
		current.Content = content
	}
	snapshot := invitation.CampaignSnapshot
	surveyType := normalizeSurveyType(snapshotString(snapshot, "survey_type"))
	content := nestedMap(snapshot, "content")
	lowScoreThreshold, ok := invitationSnapshotInt(snapshot, "low_score_threshold")
	if surveyType == "" || len(content) == 0 || !ok || validateLowScoreThreshold(surveyType, lowScoreThreshold) != nil {
		return current, nil
	}

	result := current
	if name := snapshotString(snapshot, "name"); name != "" {
		result.Name = name
	}
	result.SurveyType = surveyType
	result.Content = normalizeObject(content)
	result.Locale = normalizedLocale(snapshotString(snapshot, "locale"), current.Locale)
	if result.SurveyType == repo.TypeNPS {
		result.Locale = domain.CanonicalNPSLocale(result.Locale)
		revision := snapshotString(snapshot, "nps_content_revision")
		if revision == "" {
			revision = npsContentRevisionFor(result.Locale, content)
		}
		canonicalContent, ok := npsContentForRevision(result.Locale, revision)
		if !ok {
			return repo.Campaign{}, ErrDisabled
		}
		result.Content = canonicalContent
	}
	result.ContentVersion = invitation.CampaignContentVersion
	result.LowScoreThreshold = lowScoreThreshold
	return result, nil
}

func invitationSnapshotInt(snapshot map[string]any, key string) (int, bool) {
	value := snapshotString(snapshot, key)
	parsed, err := strconv.Atoi(value)
	return parsed, err == nil
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
	if surveyType == repo.TypeNPS {
		return 0, 10
	}
	if surveyType == repo.TypeCES {
		return 1, 7
	}
	return 1, 5
}
