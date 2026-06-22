import { useQuery } from '@tanstack/react-query'
import { FeedbackPage } from '@/features/feedback/components/feedback-page'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { tagsQuery } from '@/features/tags/api/list-tags'
import { workflowStatesQuery } from '@/features/workflow/api/list-states'
import { useBatchTransitionFeedback } from '@/features/workflow/api/transition-feedback'
import { AuditTimeline } from '@/features/workflow/components/audit-timeline'
import { WorkflowTransitionSelect } from '@/features/workflow/components/workflow-transition-select'

export function FeedbackRoutePage() {
  const config = useQuery(enrichConfigQuery())
  const allTags = useQuery(tagsQuery())
  const allStates = useQuery(workflowStatesQuery())
  const batchTransition = useBatchTransitionFeedback()

  return (
    <FeedbackPage
      dims={config.data?.dimensions ?? []}
      tagList={allTags.data ?? []}
      stateList={allStates.data ?? []}
      batchTransition={batchTransition}
      renderWorkflowTransition={(data) => (
        <WorkflowTransitionSelect
          feedbackId={String(data.id)}
          currentState={data.workflowState}
          allowedNext={data.allowedNextStates ?? []}
        />
      )}
      renderAuditLog={(data) => <AuditTimeline feedbackId={String(data.id)} />}
    />
  )
}
