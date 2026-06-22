import { createFileRoute } from '@tanstack/react-router'
import { usePermissions } from '@/features/session/hooks/use-permissions'
import { enrichmentRuntimeQuery } from '@/features/settings/api/enrichment-runtime'
import { EnrichmentRuntimePage } from '@/features/settings/components/enrichment-runtime-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/configuration/enrichment-runtime')({
  beforeLoad: ({ context }) =>
    requireRouteAccess(context, { permission: 'settings:enrichment_runtime:view' }),
  component: EnrichmentRuntimeRoutePage,
  loader: ({ context }) => context.queryClient.ensureQueryData(enrichmentRuntimeQuery()),
})

function EnrichmentRuntimeRoutePage() {
  const { can } = usePermissions()
  return <EnrichmentRuntimePage canEdit={can('settings:enrichment_runtime:edit')} />
}
