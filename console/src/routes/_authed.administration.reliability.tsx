import { createFileRoute } from '@tanstack/react-router'
import { apiKeysQuery } from '@/features/api-keys/api/list-api-keys'
import { gdprOperationsQuery } from '@/features/gdpr/api/gdpr-control'
import { mcpClientsQuery } from '@/features/mcp-clients/api/list-mcp-clients'
import { deliveriesQuery } from '@/features/outbox-dead/api/list-deliveries'
import { recoveryContextQuery } from '@/features/reliability/api/get-recovery-context'
import { releaseContextQuery } from '@/features/reliability/api/get-release-context'
import { authModeQuery } from '@/features/security/api/auth-mode'
import { meQuery } from '@/features/session/api/get-me'
import { preflightQuery } from '@/features/system-readiness/api/get-preflight'
import { ReliabilityRoutePage } from '@/routes/-reliability-route-page'
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
    ]),
})
