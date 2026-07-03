import { createFileRoute } from '@tanstack/react-router'
import { replySendHookQuery } from '@/features/reply-send-hook/api/reply-send-hook'
import { ReplySendHookPage } from '@/features/reply-send-hook/components/reply-send-hook-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations/reply-send-hook')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { adminOnly: true }),
  component: ReplySendHookPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(replySendHookQuery()),
})
