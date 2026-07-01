import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'

// Loading is the spinner + "Loading…" line shown while a list/table is
// fetching. Three route pages had identical local copies (#66 review
// M4) — this file collapses them. The text comes from the shared
// `app.loading` i18n key.
export function Loading({ className }: { className?: string }) {
  const { t } = useTranslation()
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn('flex items-center justify-center py-8 text-muted-foreground', className)}
    >
      <Loader2 aria-hidden="true" className="mr-2 h-4 w-4 animate-spin" />
      {t('app.loading')}
    </div>
  )
}
