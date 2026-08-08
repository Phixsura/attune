import { createFileRoute } from '@tanstack/react-router'
import {
  surveyAnalyticsQuery,
  surveyCampaignsQuery,
  surveyInvitationsQuery,
  surveyResponsesQuery,
} from '@/features/surveys/api/surveys'
import { SurveysPage } from '@/features/surveys/components/surveys-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations/surveys')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { adminOnly: true }),
  component: SurveysPage,
  loader: async ({ context }) => {
    await Promise.all([
      context.queryClient.ensureQueryData(surveyCampaignsQuery(undefined, 50)),
      context.queryClient.ensureQueryData(surveyAnalyticsQuery()),
      context.queryClient.ensureQueryData(surveyInvitationsQuery({ limit: 25 })),
      context.queryClient.ensureQueryData(
        surveyResponsesQuery({
          limit: 25,
          lowScoreOnly: true,
        }),
      ),
    ])
  },
})
