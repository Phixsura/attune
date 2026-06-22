import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/_authed/api-keys')({
  beforeLoad: () => {
    throw redirect({ to: '/integrations/api-keys' })
  },
})
