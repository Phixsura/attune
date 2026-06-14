import { ArrowRight, Loader2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { WorkflowStateBadge } from '@/components/workflow/workflow-state-badge'
import { useTransitionFeedback } from '@/features/workflow/api/transition-feedback'
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
  const transition = useTransitionFeedback()
  const [toStateId, setToStateId] = useState<string>('')
  const [comment, setComment] = useState('')

  const handleTransition = () => {
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
    <div className="space-y-3">
      {currentState && (
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">{t('workflow.current_state')}:</span>
          <WorkflowStateBadge
            name={currentState.name}
            color={currentState.color}
            category={currentState.category}
          />
        </div>
      )}

      {allowedNext.length > 0 && (
        <div className="flex items-end gap-2">
          <div className="min-w-0 flex-1 space-y-1.5">
            <Select value={toStateId} onValueChange={setToStateId}>
              <SelectTrigger className="h-8 text-xs">
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
                      {s.name}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              className="h-8 text-xs"
              placeholder={t('workflow.transition_comment')}
              value={comment}
              onChange={(e) => setComment(e.target.value)}
              maxLength={500}
            />
          </div>
          <Button
            size="sm"
            className="h-8"
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
      )}

      {!currentState && allowedNext.length === 0 && (
        <p className="text-xs text-muted-foreground">{t('workflow.no_states')}</p>
      )}
    </div>
  )
}
