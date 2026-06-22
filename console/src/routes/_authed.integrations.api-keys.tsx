import { createFileRoute } from '@tanstack/react-router'
import { apiKeysQuery } from '@/features/api-keys/api/list-api-keys'
import { ApiKeysPage } from '@/features/api-keys/components/api-keys-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/integrations/api-keys')({
  beforeLoad: ({ context }) =>
    requireRouteAccess(context, { permission: 'settings:api_keys:view' }),
  component: ApiKeysPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(apiKeysQuery()),
})
