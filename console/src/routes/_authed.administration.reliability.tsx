import { createFileRoute } from '@tanstack/react-router'
import { apiKeysQuery } from '@/features/api-keys/api/list-api-keys'
import { customerRequestsQuery } from '@/features/customer-requests/api/customer-requests'
import { feedbackStatsQuery } from '@/features/feedback/api/get-feedback-stats'
import { gdprOperationsQuery } from '@/features/gdpr/api/gdpr-control'
import { mcpClientsQuery } from '@/features/mcp-clients/api/list-mcp-clients'
import { deliveriesQuery } from '@/features/outbox-dead/api/list-deliveries'
import { recoveryContextQuery } from '@/features/reliability/api/get-recovery-context'
import { releaseContextQuery } from '@/features/reliability/api/get-release-context'
import { requestNotificationStatusEvidenceQuery } from '@/features/request-notifications/api/request-notifications'
import { authModeQuery } from '@/features/security/api/auth-mode'
import { meQuery } from '@/features/session/api/get-me'
import { surveyAnalyticsQuery } from '@/features/surveys/api/surveys'
import { preflightQuery } from '@/features/system-readiness/api/get-preflight'
import { llmUsageQuery } from '@/features/usage/api/get-llm-usage'
import { usageQuery } from '@/features/usage/api/get-usage'
import {
  ReliabilityRoutePage,
  reliabilityCustomerRequestFilters,
} from '@/routes/-reliability-route-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/administration/reliability')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { adminOnly: true }),
  component: ReliabilityRoutePage,
  loader: async ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(meQuery()),
      context.queryClient.ensureQueryData(authModeQuery()),
      context.queryClient.ensureQueryData(preflightQuery()),
      context.queryClient.ensureQueryData(apiKeysQuery()),
      context.queryClient.ensureQueryData(mcpClientsQuery()),
      context.queryClient.ensureQueryData(gdprOperationsQuery()),
      context.queryClient.ensureQueryData(deliveriesQuery('dead')),
      context.queryClient.ensureQueryData(recoveryContextQuery()),
      context.queryClient.ensureQueryData(releaseContextQuery()),
      context.queryClient.ensureQueryData(feedbackStatsQuery()),
      context.queryClient.ensureQueryData(requestNotificationStatusEvidenceQuery()),
      context.queryClient.ensureQueryData(usageQuery()),
      context.queryClient.ensureQueryData(llmUsageQuery({ granularity: 'week', range: 'now-30d' })),
      context.queryClient.ensureQueryData(customerRequestsQuery(reliabilityCustomerRequestFilters)),
      context.queryClient.ensureQueryData(surveyAnalyticsQuery()),
    ]),
})
