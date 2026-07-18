import { ArrowRight, Loader2, MessageSquareText, Route, Sparkles } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { WorkflowStateBadge } from '@/components/workflow/workflow-state-badge'
import { useTransitionFeedback } from '@/features/workflow/api/transition-feedback'
import { useDisplayName } from '@/lib/i18n-resolve'
import type { WorkflowState } from '@/proto/attune/v1/workflow'

export function WorkflowTransitionSelect({
  feedbackId,
  currentState,
  allowedNext,
}: {
  feedbackId: string
  currentState?: WorkflowState
  allowedNext: WorkflowState[]
}) {
  const { t } = useTranslation()
  const displayOf = useDisplayName()
  const transition = useTransitionFeedback()
  const [toStateId, setToStateId] = useState<string>('')
  const [comment, setComment] = useState('')
  const selectedState = allowedNext.find((state) => state.id === toStateId)

  const handleTransition = () => {
    /* v8 ignore next -- @preserve: transition button is disabled until a target state is selected. */
    if (!toStateId) return
    transition.mutate(
      { feedbackId: Number(feedbackId), toStateId, comment: comment.trim() },
      {
        onSuccess: () => {
          toast.success(t('workflow.transition_success'))
          setToStateId('')
          setComment('')
        },
        onError: (err) => toast.error(err instanceof Error ? err.message : t('common.error')),
      },
    )
  }

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2">
        <StatePanel
          icon={<Sparkles className="h-3.5 w-3.5" />}
          label={t('workflow.current_state')}
          value={
            currentState ? (
              <WorkflowStateBadge state={currentState} />
            ) : (
              <span className="text-xs text-muted-foreground">{t('workflow.no_states')}</span>
            )
          }
          hint={
            currentState
              ? t('workflow.transition_current_hint')
              : t('workflow.transition_missing_current_hint')
          }
        />
        <StatePanel
          icon={<Route className="h-3.5 w-3.5" />}
          label={t('workflow.transition_next_state')}
          value={
            selectedState ? (
              <WorkflowStateBadge state={selectedState} />
            ) : (
              <span className="text-xs text-muted-foreground">
                {allowedNext.length > 0
                  ? t('workflow.transition_placeholder')
                  : t('workflow.transition_no_routes')}
              </span>
            )
          }
          hint={
            allowedNext.length > 0
              ? t('workflow.transition_panel_hint', { count: allowedNext.length })
              : t('workflow.transition_no_routes_hint')
          }
        />
      </div>

      {allowedNext.length > 0 && (
        <div className="rounded-xl border border-border/60 bg-muted/[0.08] p-4">
          <div className="mb-3 flex items-center gap-2">
            <div className="flex h-7 w-7 items-center justify-center rounded-full bg-primary/10 text-primary">
              <ArrowRight className="h-3.5 w-3.5" />
            </div>
            <div className="min-w-0">
              <p className="text-sm font-medium">{t('workflow.transition_button')}</p>
              <p className="text-xs text-muted-foreground">{t('workflow.transition_ready_hint')}</p>
            </div>
          </div>

          <div className="space-y-3">
            <Select value={toStateId} onValueChange={setToStateId}>
              <SelectTrigger
                aria-label={t('workflow.transition_next_state')}
                className="h-10 rounded-lg bg-background text-sm"
              >
                <SelectValue placeholder={t('workflow.transition_placeholder')} />
              </SelectTrigger>
              <SelectContent>
                {allowedNext.map((s) => (
                  <SelectItem key={s.id} value={s.id}>
                    <span className="flex items-center gap-2">
                      <span
                        className="inline-block size-2 rounded-full"
                        style={{ backgroundColor: s.color }}
                      />
                      {displayOf(s.displayName) || s.name}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <label className="block space-y-2">
              <span className="inline-flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                <MessageSquareText className="h-3.5 w-3.5" />
                {t('workflow.transition_reason')}
              </span>
              <textarea
                className="min-h-24 w-full resize-y rounded-lg border border-input bg-background px-3 py-2 text-sm shadow-xs outline-none transition-[color,box-shadow] placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
                placeholder={t('workflow.transition_comment')}
                aria-label={t('workflow.transition_reason')}
                value={comment}
                onChange={(e) => setComment(e.target.value)}
                maxLength={500}
              />
            </label>

            <div className="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
              <p className="text-xs text-muted-foreground">
                {selectedState
                  ? t('workflow.transition_selected_hint', {
                      state: displayOf(selectedState.displayName) || selectedState.name,
                    })
                  : t('workflow.transition_comment_hint')}
              </p>
              <div className="flex items-center gap-3 sm:shrink-0">
                <span className="text-[11px] text-muted-foreground">{comment.length}/500</span>
                <Button
                  size="sm"
                  className="h-9 min-w-28"
                  disabled={!toStateId || transition.isPending}
                  onClick={handleTransition}
                >
                  {transition.isPending ? (
                    <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />
                  ) : (
                    <ArrowRight className="mr-1.5 h-3.5 w-3.5" />
                  )}
                  {t('workflow.transition_button')}
                </Button>
              </div>
            </div>
          </div>
        </div>
      )}

      {!currentState && allowedNext.length === 0 && (
        <div className="rounded-xl border border-dashed border-border/70 bg-muted/[0.08] px-4 py-4">
          <p className="text-sm font-medium">{t('workflow.no_states')}</p>
          <p className="mt-1 text-xs leading-6 text-muted-foreground">
            {t('workflow.transition_missing_current_hint')}
          </p>
        </div>
      )}

      {currentState && allowedNext.length === 0 && (
        <div className="rounded-xl border border-dashed border-border/70 bg-background px-4 py-4">
          <p className="text-sm font-medium">{t('workflow.transition_no_routes')}</p>
          <p className="mt-1 text-xs leading-6 text-muted-foreground">
            {t('workflow.transition_no_routes_hint')}
          </p>
        </div>
      )}
    </div>
  )
}

function StatePanel({
  icon,
  label,
  value,
  hint,
}: {
  icon: React.ReactNode
  label: string
  value: React.ReactNode
  hint: string
}) {
  return (
    <div className="rounded-xl border border-border/60 bg-background px-4 py-3">
      <div className="flex items-start gap-3">
        <div className="mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
          {icon}
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-[11px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
            {label}
          </p>
          <div className="mt-2">{value}</div>
          <p className="mt-2 text-xs leading-5 text-muted-foreground">{hint}</p>
        </div>
      </div>
    </div>
  )
}
