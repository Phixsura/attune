import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

interface SaveStatusProps {
  dirty: boolean
  saving: boolean
  lastSavedAt?: Date | null
}

export function SaveStatus({ dirty, saving }: SaveStatusProps) {
  const { t } = useTranslation()

  if (saving) {
    return (
      <span
        className="inline-flex items-center gap-1.5 text-xs text-muted-foreground"
        aria-live="polite"
      >
        <Loader2 className="size-3 animate-spin" />
        {t('draft.status_saving')}
      </span>
    )
  }

  if (dirty) {
    return (
      <span className="text-xs text-muted-foreground" aria-live="polite">
        <span aria-hidden="true">●</span> {t('draft.status_unsaved')}
      </span>
    )
  }

  return null
}
