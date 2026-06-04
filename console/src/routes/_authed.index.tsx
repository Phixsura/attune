import { createFileRoute, redirect } from '@tanstack/react-router'

// "/" inside the authed layout bounces to /feedback as the default
// landing page. /usage might make more sense later for admins; we'll
// revisit when we have data to suggest one over the other.

export const Route = createFileRoute('/_authed/')({
  beforeLoad: () => {
    throw redirect({ to: '/feedback' })
  },
})
