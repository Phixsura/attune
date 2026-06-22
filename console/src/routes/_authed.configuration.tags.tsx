import { createFileRoute } from '@tanstack/react-router'
import { tagsQuery } from '@/features/tags/api/list-tags'
import { TagsPage } from '@/features/tags/components/tags-page'
import { requireRouteAccess } from '@/routes/-route-access'

export const Route = createFileRoute('/_authed/configuration/tags')({
  beforeLoad: ({ context }) => requireRouteAccess(context, { permission: 'settings:tags:view' }),
  component: TagsPage,
  loader: ({ context }) => context.queryClient.ensureQueryData(tagsQuery()),
})
