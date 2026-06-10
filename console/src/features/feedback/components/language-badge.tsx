import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

export function LanguageBadge({ language, className }: { language?: string; className?: string }) {
  const { t } = useTranslation()
  const code = normalizeLanguage(language)
  const label = t(`feedback.language.${code}`, {
    defaultValue: code === 'unknown' ? t('feedback.language.unknown') : code.toUpperCase(),
  })
  const title = t('feedback.language.source_title', { language: label })
  return (
    <span
      className={cn(
        'inline-flex h-6 min-w-9 items-center justify-center rounded border border-border bg-muted/40 px-2 text-[11px] font-medium uppercase text-muted-foreground',
        className,
      )}
      title={title}
    >
      {code === 'unknown' ? '-' : code}
    </span>
  )
}

export function languagesDiffer(source?: string, displayLocale?: string) {
  const src = normalizeLanguage(source)
  const dst = normalizeLanguage(displayLocale)
  return src !== 'unknown' && dst !== 'unknown' && src !== dst
}

function normalizeLanguage(language?: string) {
  const code = (language || '').trim().toLowerCase().split(/[-_]/)[0]
  return code || 'unknown'
}
