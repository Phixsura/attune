import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import type { FeedbackListFilters } from '@/features/feedback/api/list-feedback-infinite'
import { FeedbackPage } from '@/features/feedback/components/feedback-page'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { tagsQuery } from '@/features/tags/api/list-tags'
import { workflowStatesQuery } from '@/features/workflow/api/list-states'
import { useBatchTransitionFeedback } from '@/features/workflow/api/transition-feedback'
import { AuditTimeline } from '@/features/workflow/components/audit-timeline'
import { WorkflowTransitionSelect } from '@/features/workflow/components/workflow-transition-select'
import { useDocumentTitle } from '@/hooks/use-document-title'

type FeedbackRoutePageProps = {
  initialQueueMode?: 'all' | 'urgent' | 'active' | 'failed' | 'terminal' | 'ready'
  initialSourceFilter?: string
  initialTypeFilter?: string
  showTerminalWorkbench?: boolean
  titleKey?: string
  subtitleKey?: string
  initialQualityFilters?: Pick<
    FeedbackListFilters,
    | 'ids'
    | 'confidenceLte'
    | 'createdFrom'
    | 'createdTo'
    | 'enrichedFrom'
    | 'enrichedTo'
    | 'qualitySignal'
  >
}

export function FeedbackRoutePage({
  initialQueueMode = 'all',
  initialSourceFilter = '',
  initialTypeFilter = '',
  showTerminalWorkbench = false,
  titleKey,
  subtitleKey,
  initialQualityFilters,
}: FeedbackRoutePageProps = {}) {
  const { t } = useTranslation()
  const resolvedTitleKey =
    titleKey ?? (initialQueueMode === 'terminal' ? 'nav.terminal_failures' : 'nav.feedback')
  useDocumentTitle(t(resolvedTitleKey))
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
      initialSourceFilter={initialSourceFilter}
      initialTypeFilter={initialTypeFilter}
      initialQualityFilters={initialQualityFilters}
      showTerminalWorkbench={showTerminalWorkbench}
      titleKey={resolvedTitleKey}
      subtitleKey={subtitleKey}
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
