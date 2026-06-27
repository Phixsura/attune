import { useTranslation } from 'react-i18next'

interface DraftBannerProps {
  variant: 'recovery' | 'conflict'
  draftAgeMs?: number | null
  onDiscard: () => void
  onKeep: () => void
}

export function DraftBanner({ variant, onDiscard, onKeep }: DraftBannerProps) {
  const { t } = useTranslation()
  const isRecovery = variant === 'recovery'
  const accentClass = isRecovery ? 'border-l-amber-500' : 'border-l-destructive'

  return (
    <div
      role="alert"
      className={`flex items-center gap-3 border-l-2 py-2 pl-3 pr-2 text-sm ${accentClass}`}
    >
      <span className="flex-1 text-muted-foreground">
        {isRecovery ? t('draft.banner_recovery_title') : t('draft.banner_conflict_title')}
      </span>
      <button
        type="button"
        className="shrink-0 text-xs text-muted-foreground hover:text-foreground"
        onClick={onDiscard}
      >
        {isRecovery ? t('draft.recovered_discard') : t('draft.server_changed_reload')}
      </button>
      <button
        type="button"
        className="shrink-0 text-xs font-medium text-foreground hover:underline"
        onClick={onKeep}
      >
        {isRecovery ? t('draft.banner_keep') : t('draft.banner_keep_mine')}
      </button>
    </div>
  )
}
