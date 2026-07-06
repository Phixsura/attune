import { formatDistanceToNow } from 'date-fns'
import { zhCN } from 'date-fns/locale'
import type { TFunction } from 'i18next'
import { Bookmark, Loader2, Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import type { SavedAuditLogView } from '@/proto/attune/v1/audit'

export function SavedAuditLogViewsCard({
  deletingViewId,
  errorMessage,
  isLoading,
  onApplyView,
  onDeleteView,
  onSaveAsNew,
  onSaveCurrent,
  selectedViewId,
  selectedViewMatchesCurrent,
  selectedViewName,
  views,
}: {
  deletingViewId: string | null
  errorMessage: string | null
  isLoading: boolean
  onApplyView: (view: SavedAuditLogView) => void
  onDeleteView: (view: SavedAuditLogView) => void
  onSaveAsNew: () => void
  onSaveCurrent: () => void
  selectedViewId: string | null
  selectedViewMatchesCurrent: boolean
  selectedViewName: string | null
  views: SavedAuditLogView[]
}) {
  const { i18n, t } = useTranslation()
  const locale = i18n.language.startsWith('zh') ? zhCN : undefined

  return (
    <Card className="overflow-hidden border-border/70 shadow-[0_1px_0_rgba(15,23,42,0.02),0_16px_40px_-30px_rgba(15,23,42,0.2)]">
      <CardHeader className="border-b border-border/70 px-5 py-3">
        <div className="flex items-center justify-between gap-3">
          <CardTitle className="flex items-center gap-2 text-sm font-medium">
            <Bookmark className="h-3.5 w-3.5 text-primary" />
            {t('audit_log.saved_views_title')}
          </CardTitle>
          <div className="flex flex-wrap items-center gap-2">
            <Button type="button" variant="ghost" size="sm" onClick={onSaveAsNew}>
              <Plus className="mr-2 h-4 w-4" />
              {t('audit_log.saved_views_new')}
            </Button>
            <Button type="button" size="sm" onClick={onSaveCurrent}>
              {t('audit_log.saved_views_save_current')}
            </Button>
          </div>
        </div>
      </CardHeader>
      <CardContent className="space-y-4 px-5 py-4">
        <div className="rounded-2xl border border-border/60 bg-muted/20 px-4 py-3 text-sm">
          <div className="font-medium text-foreground">
            {selectedViewName
              ? t('audit_log.saved_views_current_bound', {
                  name: selectedViewName,
                })
              : t('audit_log.saved_views_current_unbound')}
          </div>
          <div className="mt-1 text-xs leading-5 text-muted-foreground">
            {selectedViewName
              ? selectedViewMatchesCurrent
                ? t('audit_log.saved_views_current_in_sync')
                : t('audit_log.saved_views_current_dirty')
              : t('audit_log.saved_views_current_unbound_hint')}
          </div>
        </div>

        {isLoading ? (
          <div className="flex items-center gap-2 rounded-xl border border-dashed border-border/70 px-4 py-4 text-sm text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin" />
            {t('app.loading')}
          </div>
        ) : errorMessage ? (
          <div className="rounded-xl border border-dashed border-destructive/30 bg-destructive/5 px-4 py-4 text-sm text-destructive">
            {errorMessage}
          </div>
        ) : views.length === 0 ? (
          <div className="rounded-xl border border-dashed border-border/70 px-4 py-4 text-sm text-muted-foreground">
            {t('audit_log.saved_views_empty')}
          </div>
        ) : (
          <div className="space-y-3">
            {views.map((view) => {
              const isSelected = selectedViewId === view.id
              const summary = describeSavedViewState(view, t)
              const updatedAt = view.updatedAt
                ? formatDistanceToNow(new Date(view.updatedAt), {
                    addSuffix: true,
                    locale,
                  })
                : ''
              return (
                <div
                  key={view.id}
                  className="rounded-2xl border border-border/70 bg-background/90 p-3 shadow-[inset_0_1px_0_rgba(255,255,255,0.72)]"
                >
                  <div className="flex items-start justify-between gap-3">
                    <button
                      type="button"
                      onClick={() => onApplyView(view)}
                      className="flex min-w-0 flex-1 flex-col items-start text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
                    >
                      <div className="flex items-center gap-2">
                        <div className="truncate text-sm font-semibold text-foreground">
                          {view.name}
                        </div>
                        {isSelected ? (
                          <span className="rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium tracking-[0.12em] text-primary uppercase">
                            {t('audit_log.saved_views_selected')}
                          </span>
                        ) : null}
                      </div>
                      <div className="mt-1 line-clamp-2 text-xs leading-5 text-muted-foreground">
                        {summary}
                      </div>
                      {updatedAt ? (
                        <div className="mt-1 text-[11px] text-muted-foreground">
                          {t('audit_log.saved_views_updated_at', { time: updatedAt })}
                        </div>
                      ) : null}
                    </button>
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      className="h-8 w-8 shrink-0 rounded-full"
                      disabled={deletingViewId === view.id}
                      onClick={() => onDeleteView(view)}
                      aria-label={t('audit_log.saved_views_delete', { name: view.name })}
                    >
                      {deletingViewId === view.id ? (
                        <Loader2 className="h-4 w-4 animate-spin" />
                      ) : (
                        <Trash2 className="h-4 w-4" />
                      )}
                    </Button>
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function describeSavedViewState(view: SavedAuditLogView, t: TFunction) {
  const state = view.state
  if (!state) return t('audit_log.saved_views_summary_empty')
  const pieces: string[] = []
  const actions = state.actions ?? []
  if (actions.length > 0) {
    pieces.push(t('audit_log.saved_views_summary_actions', { count: actions.length }))
  }
  if (state.actorId) {
    pieces.push(t('audit_log.saved_views_summary_actor_id', { value: state.actorId }))
  } else if (state.actorType) {
    pieces.push(t('audit_log.saved_views_summary_actor_type', { value: state.actorType }))
  }
  if (state.targetType) {
    pieces.push(
      state.targetId
        ? t('audit_log.saved_views_summary_target_with_id', {
            id: state.targetId,
            type: state.targetType,
          })
        : t('audit_log.saved_views_summary_target', { type: state.targetType }),
    )
  }
  if (state.localQuery) {
    pieces.push(t('audit_log.saved_views_summary_local_query', { value: state.localQuery }))
  }
  if (pieces.length === 0) return t('audit_log.saved_views_summary_no_filters')
  return pieces.slice(0, 3).join(' · ')
}
