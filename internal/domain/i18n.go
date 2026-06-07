package domain

// I18nString is a per-locale display map. The key `"default"` is the
// universal fallback used when the requested locale is missing. Locale
// keys follow BCP 47 (e.g. "zh", "en", "ja", "zh-Hant"). At least one
// non-empty entry is expected; an empty map resolves to "".
//
// The backend never resolves an I18nString — handlers return the full
// map and the SPA decides which entry to render based on the user's
// locale chain. This keeps locale logic in a single place (the SPA)
// and lets the same wire payload serve all callers.
type I18nString map[string]string

// FallbackLocale is the literal locale key reserved as the universal
// fallback inside an I18nString. Distinct from any BCP 47 tag.
const FallbackLocale = "default"

// Resolve picks the best display string for the caller's preferred
// locale chain. Walks `preferred` in order, then the `"default"` key,
// then any remaining non-empty entry. Returns "" only when the map
// is fully empty.
//
// The non-deterministic last-resort path (range over map) is fine: it
// only fires when both the user-preferred locales and `"default"` are
// absent, which is itself a partial-config edge case the resolver is
// allowed to handle pragmatically.
func (i I18nString) Resolve(preferred []string) string {
	if len(i) == 0 {
		return ""
	}
	for _, loc := range preferred {
		if v, ok := i[loc]; ok && v != "" {
			return v
		}
	}
	if v, ok := i[FallbackLocale]; ok && v != "" {
		return v
	}
	for _, v := range i {
		if v != "" {
			return v
		}
	}
	return ""
}

// NonEmpty reports whether at least one entry has a non-empty value.
// Used at validation time — a Dimension or Taxonomy with a fully empty
// DisplayName is a rejection.
func (i I18nString) NonEmpty() bool {
	for _, v := range i {
		if v != "" {
			return true
		}
	}
	return false
}
