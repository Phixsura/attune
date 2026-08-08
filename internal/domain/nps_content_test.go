// SPDX-License-Identifier: Apache-2.0

package domain

import "testing"

func TestCanonicalNPSLocaleMatchesShippedContent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		locale string
		want   string
	}{
		{locale: "", want: CanonicalNPSLocaleEnglish},
		{locale: "en-US", want: CanonicalNPSLocaleEnglish},
		{locale: "fr-FR", want: CanonicalNPSLocaleEnglish},
		{locale: "zh", want: CanonicalNPSLocaleSimplifiedChinese},
		{locale: "zh-CN", want: CanonicalNPSLocaleSimplifiedChinese},
		{locale: "zh-CN-u-ca-chinese", want: CanonicalNPSLocaleSimplifiedChinese},
		{locale: "zh-SG", want: CanonicalNPSLocaleSimplifiedChinese},
		{locale: "zh-Hans", want: CanonicalNPSLocaleSimplifiedChinese},
		{locale: "zh-Hans-CN", want: CanonicalNPSLocaleSimplifiedChinese},
		{locale: "zh-TW", want: CanonicalNPSLocaleEnglish},
		{locale: "zh-HK", want: CanonicalNPSLocaleEnglish},
		{locale: "zh-Hant", want: CanonicalNPSLocaleEnglish},
	} {
		t.Run(tc.locale, func(t *testing.T) {
			if got := CanonicalNPSLocale(tc.locale); got != tc.want {
				t.Fatalf("CanonicalNPSLocale(%q) = %q, want %q", tc.locale, got, tc.want)
			}
		})
	}

	if got := CanonicalNPSContent("fr-FR").Question; got != "How likely are you to recommend us to a colleague?" {
		t.Fatalf("French fallback question = %q", got)
	}
	if got := CanonicalNPSContent("zh-Hans").Question; got != "您向同事推荐我们的可能性有多大？" {
		t.Fatalf("Simplified-Chinese canonical question = %q", got)
	}
	if got := CanonicalNPSContent("zh-TW").Question; got != "How likely are you to recommend us to a colleague?" {
		t.Fatalf("Traditional-Chinese fallback question = %q", got)
	}
}

func TestCanonicalNPSContentRevisionIsImmutableAndRecognizable(t *testing.T) {
	t.Parallel()

	content, ok := CanonicalNPSContentForRevision("zh-CN", NPSContentRevisionV1)
	if !ok || content.Question != "您向同事推荐我们的可能性有多大？" {
		t.Fatalf("CanonicalNPSContentForRevision() = %#v, %t", content, ok)
	}
	if revision, ok := NPSContentRevisionFor("zh-CN", content); !ok || revision != NPSContentRevisionV1 {
		t.Fatalf("NPSContentRevisionFor() = %q, %t", revision, ok)
	}
	if _, ok := CanonicalNPSContentForRevision("en", "nps-v999"); ok {
		t.Fatal("unknown NPS content revision unexpectedly resolved")
	}
	if _, ok := NPSContentRevisionFor("en", NPSContent{Question: "Different question"}); ok {
		t.Fatal("arbitrary NPS content unexpectedly matched a shipped revision")
	}
}
