import { useTranslation } from 'react-i18next'

// I18nString mirrors the proto-side per-locale display map. The
// generated ts-proto type lives at `@/proto/attune/v1/common`, but it
// resolves to the same shape — we keep this alias here so feature
// modules import a stable name.
export type I18nString = { [locale: string]: string }

// FALLBACK_LOCALE matches the Go-side domain.FallbackLocale: the
// universal fallback key inside an I18nString.
const FALLBACK_LOCALE = 'default'

// resolveI18n picks the best display for a given locale chain.
//
//   1. Walk `preferred` in order; take the first non-empty hit.
//   2. Fall back to the "default" key.
//   3. As a last resort, return any non-empty entry.
//
// Returns "" only when the map is empty or entirely blank.
export function resolveI18n(
  m: I18nString | { entries?: I18nString } | null | undefined,
  preferred: readonly string[],
): string {
  const entries = unwrap(m)
  if (!entries) return ''
  for (const loc of preferred) {
    const v = entries[loc]
    if (v) return v
  }
  const fallback = entries[FALLBACK_LOCALE]
  if (fallback) return fallback
  for (const v of Object.values(entries)) {
    if (v) return v
  }
  return ''
}

// useDisplayName builds the locale chain from i18next's current
// language and walks the resolver. The chain always includes the
// base form of the BCP 47 tag (e.g. `zh-CN` → `[zh-CN, zh, en]`),
// then "default" via the resolver fallback.
//
// Returns a stable callable so callers can pass it to any number of
// I18nString fields without re-binding on every render.
export function useDisplayName(): (
  m: I18nString | { entries?: I18nString } | null | undefined,
) => string {
  const { i18n } = useTranslation()
  const chain = buildLocaleChain(i18n.language)
  return (m) => resolveI18n(m, chain)
}

function buildLocaleChain(language: string): string[] {
  const out: string[] = []
  const push = (loc: string) => {
    if (loc && !out.includes(loc)) out.push(loc)
  }
  push(language)
  const dash = language.indexOf('-')
  if (dash > 0) push(language.slice(0, dash))
  push('en')
  return out
}

// Some callers receive the proto wrapper `{ entries: { ... } }`
// (generated when `I18nString` is a message), others receive the raw
// map (Go side serializes the map directly). Accept both shapes so
// migrating between them doesn't require call-site changes.
function unwrap(m: I18nString | { entries?: I18nString } | null | undefined): I18nString | null {
  if (!m) return null
  if (typeof m === 'object' && 'entries' in m && m.entries) return m.entries
  return m as I18nString
}
