package domain

import (
	"encoding/json"
	"testing"
)

// I18nString.Resolve edge cases beyond the basic happy path covered in
// dimension_test.go. The resolver is the only thing that interprets the
// per-locale map at runtime; any inconsistency between the SPA hook and
// the Go-side helper is a user-facing bug.

func TestI18nString_LocaleChainPicksMostSpecificFirst(t *testing.T) {
	m := I18nString{"zh-Hant": "繁中", "zh": "中", "en": "EN", "default": "X"}
	if got := m.Resolve([]string{"zh-Hant", "zh", "en"}); got != "繁中" {
		t.Errorf("specific tag should win, got %q", got)
	}
}

func TestI18nString_ChainSkipsBlankEntriesAndContinues(t *testing.T) {
	m := I18nString{"zh": "", "en": "E", "default": "D"}
	if got := m.Resolve([]string{"zh", "en"}); got != "E" {
		t.Errorf("blank locale should advance the chain, got %q", got)
	}
}

func TestI18nString_LastResortIsAnyNonEmpty(t *testing.T) {
	// Resolver of last resort: walk map for any non-blank when neither
	// preferred nor default match. Output is one of "Foo" / "Bar".
	m := I18nString{"ja": "Foo", "ko": "Bar"}
	got := m.Resolve([]string{"zh", "en"})
	if got != "Foo" && got != "Bar" {
		t.Errorf("expected one of the leftover entries, got %q", got)
	}
}

func TestI18nString_JSONRoundTrip(t *testing.T) {
	in := I18nString{"default": "X", "zh": "中文"}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out I18nString
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["zh"] != "中文" {
		t.Errorf("unicode lost: %v", out)
	}
}

func TestI18nString_PartialMapStillResolves(t *testing.T) {
	// A tenant who only fills the default locale — every locale
	// resolves to that one entry.
	m := I18nString{"default": "Only"}
	for _, loc := range []string{"en", "ja", "zh-Hant"} {
		if got := m.Resolve([]string{loc}); got != "Only" {
			t.Errorf("locale %q should fall back to default 'Only', got %q", loc, got)
		}
	}
}

func TestI18nString_FallbackLocaleConstantStable(t *testing.T) {
	// Sanity: the "default" sentinel is the contract — both SPA and Go
	// side must agree on it. A regression here would silently break
	// existing operator configs.
	if FallbackLocale != "default" {
		t.Errorf("FallbackLocale changed to %q — coordinate with SPA i18n-resolve.ts", FallbackLocale)
	}
}
