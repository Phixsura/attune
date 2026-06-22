import { createFileRoute } from '@tanstack/react-router'
import { ClustersPage } from '@/features/feedback/components/clusters-page'

export const Route = createFileRoute('/_authed/feedback/clusters')({
  component: ClustersPage,
})
