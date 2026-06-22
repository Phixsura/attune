import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authed/inbound-sources')({
  beforeLoad: () => {
    throw redirect({ to: '/integrations/inbound-sources' })
  },
})
