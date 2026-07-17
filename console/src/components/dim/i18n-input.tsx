import { Plus, X } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import type { I18nString } from '@/lib/i18n-resolve'

// Stable locale keys we offer by default in the editor. Operators can
// add any BCP 47 tag; this list is just the quick-pick. `default` is
// always present (the resolver's universal fallback).
const COMMON_LOCALES = ['default', 'zh', 'en'] as const

// I18nInput edits one I18nString — typically a Dimension or
// Taxonomy's `displayName`. Each row is `<locale>: <value>`; a "+"
// button adds another locale. The `default` slot is always offered
// so partial-locale maps still resolve to something useful.
export function I18nInput({
  value,
  onChange,
  inline = false,
  placeholder,
  disabled = false,
}: {
  value: I18nString
  onChange: (next: I18nString) => void
  inline?: boolean
  placeholder?: string
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const [extraLocale, setExtraLocale] = useState('')

  const locales = collectLocales(value)
  const setEntry = (locale: string, text: string) => {
    const next: I18nString = { ...value }
    if (text === '') delete next[locale]
    else next[locale] = text
    onChange(next)
  }
  const removeEntry = (locale: string) => {
    const next: I18nString = { ...value }
    delete next[locale]
    onChange(next)
  }
  const addLocale = () => {
    const tag = extraLocale.trim()
    /* v8 ignore next -- @preserve: add-locale control is disabled for blank locale input. */
    if (!tag) return
    if (locales.includes(tag)) {
      setExtraLocale('')
      return
    }
    onChange({ ...value, [tag]: '' })
    setExtraLocale('')
  }

  return (
    <div className={inline ? 'space-y-1' : 'space-y-2'}>
      {locales.map((locale) => (
        <div key={locale} className="flex items-center gap-2">
          <span className="font-mono text-xs text-muted-foreground w-16">{locale}</span>
          <Input
            value={value[locale] ?? ''}
            placeholder={placeholder}
            onChange={(e) => setEntry(locale, e.target.value)}
            className="h-8 text-sm"
            disabled={disabled}
          />
          {locale !== 'default' && !disabled && (
            <Button
              type="button"
              variant="ghost"
              size="sm"
              className="h-7 w-7 p-0"
              onClick={() => removeEntry(locale)}
              aria-label={t('dim.i18n.remove_locale', { locale })}
            >
              <X className="h-3 w-3" />
            </Button>
          )}
        </div>
      ))}
      {!disabled && (
        <div className="flex items-center gap-2">
          <Input
            value={extraLocale}
            placeholder={t('dim.i18n.add_locale_placeholder')}
            onChange={(e) => setExtraLocale(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault()
                addLocale()
              }
            }}
            className="h-8 max-w-[160px] text-xs font-mono"
          />
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={addLocale}
            data-testid="i18n-input-add-locale"
          >
            <Plus className="h-3 w-3 mr-1" />
            {t('dim.i18n.add_locale')}
          </Button>
        </div>
      )}
    </div>
  )
}

// collectLocales always surfaces "default" first, then any keys the
// operator already entered, then the common quick-picks (so they show
// as empty rows ready to fill).
function collectLocales(value: I18nString): string[] {
  const out: string[] = []
  const push = (loc: string) => {
    if (loc && !out.includes(loc)) out.push(loc)
  }
  push('default')
  for (const loc of Object.keys(value)) push(loc)
  for (const loc of COMMON_LOCALES) push(loc)
  return out
}
