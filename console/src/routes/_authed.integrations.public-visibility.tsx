import { createFileRoute } from '@tanstack/react-router'
import { moderationSubjectsQuery } from '@/features/public-visibility/api/public-visibility'
import { PublicVisibilityPage } from '@/features/public-visibility/components/public-visibility-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations/public-visibility')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { permission: 'moderation:view' }),
  component: PublicVisibilityPage,
  loader: async ({ context }) => {
    await context.queryClient.ensureQueryData(moderationSubjectsQuery())
  },
})
