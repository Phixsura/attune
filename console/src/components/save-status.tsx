import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

interface SaveStatusProps {
  dirty: boolean
  saving: boolean
  lastSavedAt: Date | null
}

export function SaveStatus({ dirty, saving, lastSavedAt }: SaveStatusProps) {
  const { t } = useTranslation()

  let content: React.ReactNode = null
  let className = 'text-sm text-muted-foreground'

  if (saving) {
    content = (
      <>
        <Loader2 className="size-3.5 animate-spin" />
        {t('draft.status_saving')}
      </>
    )
  } else if (dirty) {
    className = 'text-sm text-amber-600 dark:text-amber-400'
    content = `● ${t('draft.status_unsaved')}`
  } else if (lastSavedAt) {
    const time = lastSavedAt.toLocaleTimeString(undefined, {
      hour: '2-digit',
      minute: '2-digit',
    })
    content = t('draft.status_saved_at', { time })
  }

  if (!content) return null

  return (
    <span className={`inline-flex items-center gap-1.5 ${className}`} aria-live="polite">
      {content}
    </span>
  )
}
