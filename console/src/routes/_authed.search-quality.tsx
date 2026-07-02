import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authed/search-quality')({
  beforeLoad: () => {
    throw redirect({ to: '/analytics/search-quality' })
  },
})
