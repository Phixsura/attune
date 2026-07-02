import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authed/classification-quality')({
  beforeLoad: () => {
    throw redirect({ to: '/analytics/classification-quality' })
  },
})
