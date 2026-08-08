// SPDX-License-Identifier: Apache-2.0
// lint-artifacts:file-allow canonical localized NPS respondent content.

package domain

import "strings"

const (
	// CanonicalNPSLocaleEnglish is the language tag for the shipped English
	// relationship-NPS wording.
	CanonicalNPSLocaleEnglish = "en"
	// CanonicalNPSLocaleSimplifiedChinese is the language tag for the shipped
	// Simplified-Chinese relationship-NPS wording.
	CanonicalNPSLocaleSimplifiedChinese = "zh-CN"
	// NPSContentRevisionV1 is the first immutable Attune relationship-NPS
	// wording. Future wording changes add a new revision; this value is never
	// edited in place.
	NPSContentRevisionV1 = "nps-v1"
	// CurrentNPSContentRevision is the revision assigned to a newly created
	// relationship-NPS campaign.
	CurrentNPSContentRevision = NPSContentRevisionV1
)

// NPSContent is the canonical respondent-facing copy for a relationship-NPS
// campaign. The 0-10 scale and bucket boundaries remain invariant across every
// locale, so measurements retain the same score semantics.
type NPSContent struct {
	Title         string
	Intro         string
	Question      string
	CommentPrompt string
	ThankYou      string
}

// CanonicalNPSLocale returns the language tag of the shipped NPS content. NPS
// has one fixed wording per content language, so an unsupported locale must not
// be retained as page or response metadata for English content. Only the
// generic Chinese tag and explicit Simplified-Chinese script or region variants
// resolve to the shipped Simplified-Chinese content and its matching language
// tag. Traditional-Chinese and other unsupported variants use English until
// their own canonical wording is shipped.
func CanonicalNPSLocale(locale string) string {
	locale = strings.ToLower(strings.TrimSpace(locale))
	if isSimplifiedChineseNPSLocale(locale) {
		return CanonicalNPSLocaleSimplifiedChinese
	}
	return CanonicalNPSLocaleEnglish
}

func isSimplifiedChineseNPSLocale(locale string) bool {
	return locale == "zh" ||
		locale == "zh-cn" || strings.HasPrefix(locale, "zh-cn-") ||
		locale == "zh-sg" || strings.HasPrefix(locale, "zh-sg-") ||
		locale == "zh-hans" || strings.HasPrefix(locale, "zh-hans-")
}

// CanonicalNPSContent returns the current shipped NPS wording for the campaign
// locale. Existing definitions must use CanonicalNPSContentForRevision so their
// wording remains immutable after a newer revision is introduced.
func CanonicalNPSContent(locale string) NPSContent {
	content, _ := CanonicalNPSContentForRevision(locale, CurrentNPSContentRevision)
	return content
}

// CanonicalNPSContentForRevision returns one immutable shipped NPS wording.
// Unknown revisions deliberately do not fall back to current content: displaying
// a different question would silently change a historical measurement.
func CanonicalNPSContentForRevision(locale, revision string) (NPSContent, bool) {
	if strings.TrimSpace(revision) != NPSContentRevisionV1 {
		return NPSContent{}, false
	}
	if CanonicalNPSLocale(locale) == CanonicalNPSLocaleSimplifiedChinese {
		return NPSContent{
			Title:         "产品反馈",
			Intro:         "您的反馈将帮助我们改进。",
			Question:      "您向同事推荐我们的可能性有多大？",
			CommentPrompt: "您给出这个评分的主要原因是什么？",
			ThankYou:      "感谢您的反馈。",
		}, true
	}
	return NPSContent{
		Title:         "Product feedback",
		Intro:         "Your feedback helps us improve.",
		Question:      "How likely are you to recommend us to a colleague?",
		CommentPrompt: "What is the main reason for your score?",
		ThankYou:      "Thanks for your feedback.",
	}, true
}

// NPSContentRevisionFor identifies which shipped revision exactly matches
// content at the canonical locale. It permits legacy snapshots without an
// explicit revision to be recovered without trusting arbitrary text.
func NPSContentRevisionFor(locale string, content NPSContent) (string, bool) {
	canonical, _ := CanonicalNPSContentForRevision(locale, NPSContentRevisionV1)
	if content == canonical {
		return NPSContentRevisionV1, true
	}
	return "", false
}
