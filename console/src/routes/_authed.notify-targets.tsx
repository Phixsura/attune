import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authed/notify-targets')({
  beforeLoad: () => {
    throw redirect({ to: '/integrations/notify-targets' })
  },
})
