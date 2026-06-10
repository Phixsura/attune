import { createFileRoute } from '@tanstack/react-router'
import { inboundSourcesQuery } from '@/features/inbound-sources/api/list-inbound-sources'
import { InboundSourcesPage } from '@/features/inbound-sources/components/inbound-sources-page'

export const Route = createFileRoute('/_authed/inbound-sources')({
  component: InboundSourcesPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(inboundSourcesQuery()),
})
