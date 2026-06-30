import { useQuery } from '@tanstack/react-query'
import { FeedbackPage } from '@/features/feedback/components/feedback-page'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { tagsQuery } from '@/features/tags/api/list-tags'
import { workflowStatesQuery } from '@/features/workflow/api/list-states'
import { useBatchTransitionFeedback } from '@/features/workflow/api/transition-feedback'
import { AuditTimeline } from '@/features/workflow/components/audit-timeline'
import { WorkflowTransitionSelect } from '@/features/workflow/components/workflow-transition-select'

type FeedbackRoutePageProps = {
  initialQueueMode?: 'all' | 'urgent' | 'active' | 'failed' | 'terminal' | 'ready'
  showTerminalWorkbench?: boolean
}

export function FeedbackRoutePage({
  initialQueueMode = 'all',
  showTerminalWorkbench = false,
}: FeedbackRoutePageProps = {}) {
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
      initialQueueMode={initialQueueMode}
      showTerminalWorkbench={showTerminalWorkbench}
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
