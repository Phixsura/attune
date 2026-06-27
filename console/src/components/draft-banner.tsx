import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'

function formatAge(
  ms: number | null,
  t: (key: string, opts?: Record<string, unknown>) => string,
): string {
  if (ms === null) return ''
  const minutes = Math.floor(ms / 60_000)
  if (minutes < 1) return t('draft.age_seconds')
  if (minutes < 60) return t('draft.age_minutes', { count: minutes })
  const hours = Math.floor(minutes / 60)
  return t('draft.age_hours', { count: hours })
}

interface DraftBannerProps {
  variant: 'recovery' | 'conflict'
  draftAgeMs?: number | null
  onDiscard: () => void
  onKeep: () => void
}

export function DraftBanner({ variant, draftAgeMs, onDiscard, onKeep }: DraftBannerProps) {
  const { t } = useTranslation()
  const isRecovery = variant === 'recovery'
  const age = formatAge(draftAgeMs ?? null, t)

  return (
    <div
      role="alert"
      className="flex flex-col gap-3 rounded-lg border border-border bg-muted/50 px-4 py-3 text-sm sm:flex-row sm:items-center"
    >
      <p className="flex-1">
        <span className="font-medium">
          {isRecovery ? t('draft.banner_recovery_title') : t('draft.banner_conflict_title')}
        </span>
        <span className="ml-1 text-muted-foreground">
          {isRecovery
            ? age
              ? t('draft.banner_recovery_body', { age })
              : t('draft.banner_recovery_body_no_age')
            : t('draft.banner_conflict_body')}
        </span>
      </p>
      <div className="flex shrink-0 gap-2">
        <Button type="button" variant="outline" size="sm" onClick={onDiscard}>
          {isRecovery ? t('draft.recovered_discard') : t('draft.server_changed_reload')}
        </Button>
        <Button type="button" size="sm" onClick={onKeep}>
          {isRecovery ? t('draft.banner_keep') : t('draft.banner_keep_mine')}
        </Button>
      </div>
    </div>
  )
}
