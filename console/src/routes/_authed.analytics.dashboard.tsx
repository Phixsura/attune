import { createFileRoute } from '@tanstack/react-router'
import { feedbackStatsQuery } from '@/features/feedback/api/get-feedback-stats'
import { AnalyticsDashboard } from '@/features/feedback/components/analytics-dashboard'
import { enrichConfigQuery } from '@/features/settings/api/get-enrich-config'
import { usageQuery } from '@/features/usage/api/get-usage'

export const Route = createFileRoute('/_authed/analytics/dashboard')({
  component: AnalyticsDashboard,
  loader: ({ context }) =>
    Promise.all([
      context.queryClient.ensureQueryData(feedbackStatsQuery()),
      context.queryClient.ensureQueryData(enrichConfigQuery()),
      context.queryClient.ensureQueryData(usageQuery()),
    ]),
})
