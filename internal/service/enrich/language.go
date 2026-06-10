package enrich

import (
	"strings"
	"unicode"
)

const (
	LanguageUnknown  = "unknown"
	LanguageChinese  = "zh"
	LanguageEnglish  = "en"
	LanguageJapanese = "ja"

	languageDetectorVersion = "rule-v1"
)

// DetectLanguage returns a conservative ISO-639-1-style code for the source
// feedback text. The detector is intentionally dependency-free and cheap:
// enough signal returns zh/en/ja, ambiguous or mostly non-linguistic text
// returns unknown rather than overfitting short rows.
func DetectLanguage(text string) string {
	counts := countLanguageScripts(stripURLTokens(text))
	return classifyLanguageCounts(counts)
}

type languageCounts struct {
	han     int
	hira    int
	kata    int
	latin   int
	letters int
}

func countLanguageScripts(text string) languageCounts {
	var counts languageCounts
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han):
			counts.han++
			counts.letters++
		case unicode.In(r, unicode.Hiragana):
			counts.hira++
			counts.letters++
		case unicode.In(r, unicode.Katakana):
			counts.kata++
			counts.letters++
		case unicode.In(r, unicode.Latin) && unicode.IsLetter(r):
			counts.latin++
			counts.letters++
		case unicode.IsLetter(r):
			counts.letters++
		}
	}
	return counts
}

func classifyLanguageCounts(counts languageCounts) string {
	if counts.letters < 2 {
		return LanguageUnknown
	}
	kana := counts.hira + counts.kata
	if kana > 0 {
		return LanguageJapanese
	}
	if counts.han > 0 {
		return classifyHanLatinMix(counts.han, counts.latin)
	}
	if counts.latin >= 3 && counts.latin*2 >= counts.letters {
		return LanguageEnglish
	}
	return LanguageUnknown
}

func classifyHanLatinMix(han, latin int) string {
	switch {
	case latin == 0:
		return LanguageChinese
	case han >= latin:
		return LanguageChinese
	case latin >= han*3 && latin >= 3:
		return LanguageEnglish
	default:
		return LanguageUnknown
	}
}

func stripURLTokens(text string) string {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return text
	}
	kept := make([]string, 0, len(fields))
	for _, field := range fields {
		lower := strings.ToLower(strings.Trim(field, ".,;:!?()[]{}<>\"'"))
		if strings.HasPrefix(lower, "http://") ||
			strings.HasPrefix(lower, "https://") ||
			strings.HasPrefix(lower, "www.") {
			continue
		}
		kept = append(kept, field)
	}
	return strings.Join(kept, " ")
}

func normalizeLanguage(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexAny(code, "-_"); i >= 0 {
		code = code[:i]
	}
	switch code {
	case LanguageChinese, LanguageEnglish, LanguageJapanese:
		return code
	default:
		return LanguageUnknown
	}
}

func displayLanguageForLocale(locale string) string {
	switch normalizeLanguage(locale) {
	case LanguageChinese:
		return LanguageChinese
	case LanguageJapanese:
		return LanguageJapanese
	case LanguageEnglish:
		return LanguageEnglish
	default:
		return LanguageEnglish
	}
}

func displayLocaleForTenantLocale(locale string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return LanguageEnglish
	}
	return locale
}

func sameLanguage(sourceLanguage, displayLocale string) bool {
	source := normalizeLanguage(sourceLanguage)
	display := displayLanguageForLocale(displayLocale)
	return source != LanguageUnknown && display != LanguageUnknown && source == display
}

func promptLanguageFor(code string) string {
	if normalizeLanguage(code) == LanguageChinese {
		return LanguageChinese
	}
	return LanguageEnglish
}

func detectRowLanguage(content string) string {
	return DetectLanguage(content)
}
