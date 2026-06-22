import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authed/clusters')({
  beforeLoad: () => {
    throw redirect({ to: '/feedback/clusters' })
  },
})
